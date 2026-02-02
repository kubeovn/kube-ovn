package ha

import (
	"context"
	"flag"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	"k8s.io/utils/ptr"
	"k8s.io/utils/set"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega/format"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	// Register flags.
	config.CopyFlags(config.Flags, flag.CommandLine)
	k8sframework.RegisterCommonFlags(flag.CommandLine)
	k8sframework.RegisterClusterFlags(flag.CommandLine)
}

func getFileInfo(node *kind.Node, filePath string) (string, string) {
	ginkgo.GinkgoHelper()

	stdout, stderr, err := node.Exec("sh", "-c", fmt.Sprintf(`if [ -f %s ]; then stat --format "%%W %%s" "%s"; else echo 0 0; fi`, filePath, filePath))
	framework.ExpectNoError(err, fmt.Sprintf("failed to get file size of %q on node %s: stdout = %q, stderr = %q", filePath, node.Name(), stdout, stderr))
	fields := strings.Fields(string(stdout))
	framework.ExpectHaveLen(fields, 2, "unexpected output of stat command on file %q on node %s: stdout = %q, stderr = %q", filePath, node.Name(), stdout, stderr)
	return fields[0], fields[1]
}

func dbFilePath(db string) string {
	return fmt.Sprintf("/etc/ovn/ovn%s_db.db", db)
}

func dbFileHostPath(db string) string {
	return fmt.Sprintf("/etc/origin/ovn/ovn%s_db.db", db)
}

var dbNames = map[string]string{
	"nb": ovnnb.DatabaseName,
	"sb": ovnsb.DatabaseName,
}

func cmdClusterStatus(db string) string {
	return fmt.Sprintf("ovn-appctl -t /var/run/ovn/ovn%s_db.ctl cluster/status %s", db, dbNames[db])
}

type clusterStatus struct {
	ClusterID string
	ServerID  string
	Address   string
	Servers   map[string]string // sid -> address
}

// parseClusterStatus parses the output of `ovn-appctl cluster/status` command.
// Example output:
//
//	3e81
//	Name: OVN_Northbound
//	Cluster ID: 6be2 (6be21888-7f5e-48c9-b1d6-3a30571d6a02)
//	Server ID: 3e81 (3e816124-8972-4046-a267-517b9fe7443c)
//	Address: tcp:[172.18.0.2]:6643
//	Status: cluster member
//	Role: follower
//	Term: 3
//	Leader: 68d9
//	Vote: 68d9
//
//	Election timer: 5000
//	Log: [2, 141]
//	Entries not yet committed: 0
//	Entries not yet applied: 0
//	Connections: ->0000 ->0000 <-68d9 <-007f
//	Disconnections: 963
//	Servers:
//	    3e81 (3e81 at tcp:[172.18.0.2]:6643) (self)
//	    68d9 (68d9 at tcp:[172.18.0.4]:6643) last msg 1543 ms ago
//	    007f (007f at tcp:[172.18.0.3]:6643) last msg 4959739 ms ago
func parseClusterStatus(s string) *clusterStatus {
	ginkgo.GinkgoHelper()

	status := &clusterStatus{
		Servers: make(map[string]string),
	}
	for line := range strings.SplitSeq(s, "\n") {
		fields := strings.Fields(line)
		switch {
		case strings.HasPrefix(line, "Cluster ID:"):
			status.ClusterID = fields[len(fields)-2]
		case strings.HasPrefix(line, "Server ID:"):
			status.ServerID = fields[len(fields)-2]
		case strings.HasPrefix(line, "Address:"):
			status.Address = fields[len(fields)-1]
		case slices.Contains(fields, "at"):
			status.Servers[fields[0]] = strings.TrimSuffix(fields[slices.Index(fields, "at")+1], ")")
		}
	}
	framework.ExpectNotEmpty(status.ClusterID, "parsing cluster ID from cluster status:\n%s", s)
	framework.ExpectNotEmpty(status.ServerID, "parsing server ID from cluster status:\n%s", s)
	framework.ExpectNotEmpty(status.Address, "parsing address from cluster status:\n%s", s)
	framework.ExpectHaveKeyWithValue(status.Servers, status.ServerID, status.Address, "parsing servers from cluster status:\n%s", s)

	return status
}

func getDbSidsFromClusterStatus(f *framework.Framework, deploy *appsv1.Deployment) map[string]set.Set[string] {
	ginkgo.GinkgoHelper()

	ginkgo.By("Getting pods of deployment " + deploy.Name)
	deployClient := f.DeploymentClientNS(deploy.Namespace)
	pods, err := deployClient.GetAllPods(deploy)
	framework.ExpectNoError(err)
	framework.ExpectHaveLen(pods.Items, int(*deploy.Spec.Replicas))

	expectedServerCount := len(pods.Items)
	dbServers := make(map[string]map[string]string)

	// Wait for cluster to converge with retry logic
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, 60*time.Second, true, func(_ context.Context) (bool, error) {
		// Refresh pods list in case of changes
		pods, err = deployClient.GetAllPods(deploy)
		if err != nil {
			return false, nil // Retry on error
		}
		if len(pods.Items) != int(*deploy.Spec.Replicas) {
			return false, nil // Wait for pods to be ready
		}

		// Clear previous state for retry
		dbServers = make(map[string]map[string]string)

		for _, db := range [...]string{"nb", "sb"} {
			ginkgo.By("Getting ovn" + db + " db server ids on all ovn-central pods")
			for pod := range slices.Values(pods.Items) {
				stdout, stderr, err := framework.ExecShellInPod(context.Background(), f, pod.Namespace, pod.Name, cmdClusterStatus(db))
				if err != nil {
					framework.Logf("Failed to get ovn%s db status in pod %s, will retry: stdout = %q, stderr = %q, err = %v", db, pod.Name, stdout, stderr, err)
					return false, nil // Retry on error
				}
				status := parseClusterStatus(stdout)

				// Check if cluster has converged (all servers are visible)
				if len(status.Servers) != expectedServerCount {
					framework.Logf("Cluster %s not converged yet in pod %s: expected %d servers, got %d, will retry",
						db, pod.Name, expectedServerCount, len(status.Servers))
					return false, nil // Retry until cluster converges
				}

				if len(dbServers[db]) == 0 {
					dbServers[db] = maps.Clone(status.Servers)
				} else if !maps.Equal(status.Servers, dbServers[db]) {
					framework.Logf("Inconsistent servers in ovn%s db status across pods, will retry", db)
					return false, nil // Retry until consistent
				}
			}
		}
		return true, nil
	})
	framework.ExpectNoError(err, "timeout waiting for OVN cluster to converge")

	framework.Logf("ovn db servers from cluster status:\n%s", format.Object(dbServers, 2))

	dbSids := make(map[string]set.Set[string], len(dbServers))
	for db, servers := range dbServers {
		dbSids[db] = set.New(slices.Collect(maps.Keys(servers))...)
	}
	framework.Logf("ovn db server ids from cluster status:\n%s", format.Object(dbSids, 2))
	return dbSids
}

func TestE2E(t *testing.T) {
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)
	e2e.RunE2ETests(t)
}

func getDbSids(f *framework.Framework, deploy *appsv1.Deployment) map[string]set.Set[string] {
	ginkgo.GinkgoHelper()

	ginkgo.By("Getting pods of deployment " + deploy.Name)
	deployClient := f.DeploymentClientNS(deploy.Namespace)
	pods, err := deployClient.GetAllPods(deploy)
	framework.ExpectNoError(err)
	framework.ExpectHaveLen(pods.Items, int(*deploy.Spec.Replicas))

	dbSids := make(map[string]set.Set[string])
	for _, db := range [...]string{"nb", "sb"} {
		dbSids[db] = set.New[string]()
		cmd := fmt.Sprintf(`ovsdb-tool db-sid %q`, dbFilePath(db))
		for pod := range slices.Values(pods.Items) {
			ginkgo.By("Getting ovn" + db + " db server id from pod " + pod.Name)
			stdout, stderr, err := framework.ExecShellInPod(context.Background(), f, pod.Namespace, pod.Name, cmd)
			framework.ExpectNoError(err, fmt.Sprintf("failed to get ovn%s db server id from pod %s: stdout = %q, stderr = %q", db, pod.Name, stdout, stderr))
			dbSids[db].Insert(strings.TrimSpace(stdout)[:4])
		}
		framework.ExpectHaveLen(dbSids[db], int(*deploy.Spec.Replicas), "unexpected number of ovn%s db server ids from pods of deployment %s", db, deploy.Name)
	}
	framework.Logf("ovn db server ids from db files:\n%s", format.Object(dbSids, 2))

	return dbSids
}

func corruptAndRecover(f *framework.Framework, deploy *appsv1.Deployment, dbFile, dbFileHost, corruptCmd string, kindNodes map[string]*kind.Node) {
	ginkgo.GinkgoHelper()

	replicas := *deploy.Spec.Replicas

	ginkgo.By("Getting ovn-central pods")
	deployClient := f.DeploymentClientNS(deploy.Namespace)
	pods, err := deployClient.GetAllPods(deploy)
	framework.ExpectNoError(err)
	framework.ExpectHaveLen(pods.Items, int(replicas))

	checkCmd := "ovsdb-tool check-cluster " + dbFile
	for _, pod := range pods.Items {
		ginkgo.By("Checking whether db file " + dbFile + " on node " + pod.Spec.NodeName + " is healthy")
		stdout, stderr, err := framework.ExecShellInPod(context.Background(), f, pod.Namespace, pod.Name, checkCmd)
		framework.ExpectNoError(err, fmt.Sprintf("failed to check db file %q: stdout = %q, stderr = %q", dbFile, stdout, stderr))
	}

	nodes := set.New[string]()
	for _, pod := range pods.Items {
		nodes.Insert(pod.Spec.NodeName)
	}

	ginkgo.By("Scaling down deployment ovn-central to " + strconv.Itoa(int(replicas-1)))
	deployClient.SetScale(deploy.Name, replicas-1)

	ginkgo.By("Waiting for an ovn-central pod to be deleted")
	framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
		pods, err := deployClient.GetAllPods(deploy)
		if err != nil {
			return false, err
		}
		return len(pods.Items) == int(replicas-1), nil
	}, "")

	ginkgo.By("Waiting for deployment ovn-central to be ready")
	deployClient.RolloutStatus(deploy.Name)

	ginkgo.By("Getting ovn-central pods after scale down")
	pods, err = deployClient.GetAllPods(deploy)
	framework.ExpectNoError(err)
	framework.ExpectHaveLen(pods.Items, int(replicas-1))

	newNodes := set.New[string]()
	for _, pod := range pods.Items {
		newNodes.Insert(pod.Spec.NodeName)
	}

	ginkgo.By("Getting the node where ovn-central pod is removed")
	diffNodes := nodes.Difference(newNodes)
	framework.ExpectHaveLen(diffNodes, 1)
	nodeName := diffNodes.UnsortedList()[0]
	framework.Logf("node with ovn-central pod removed: %s", nodeName)

	ginkgo.By("Getting file info of db file " + dbFileHost + " on node " + nodeName + " before corruption")
	birth, size := getFileInfo(kindNodes[nodeName], dbFileHost)
	framework.Logf("file info of db file %s on node %s before corruption: birth time = %s, size = %s",
		dbFileHost, nodeName, birth, size)

	ginkgo.By("Corrupting db file " + dbFileHost + " on node " + nodeName)
	framework.ExpectHaveKey(kindNodes, nodeName, "getting kind node by name")
	node := kindNodes[nodeName]
	stdout, stderr, err := node.Exec("bash", "-exc", corruptCmd)
	framework.ExpectNoError(err, fmt.Sprintf("failed to corrupt db file %q: stdout = %q, stderr = %q", dbFileHost, stdout, stderr))

	ginkgo.By("Getting file size of db file " + dbFileHost + " on node " + nodeName + " after corruption")
	_, newSize := getFileInfo(node, dbFileHost)
	framework.Logf("file size of db file %s on node %s after corruption: %s", dbFileHost, nodeName, newSize)
	framework.ExpectNotEqual(newSize, size, "file size of db file %q on node %s should change after corruption", dbFileHost, nodeName)

	ginkgo.By("Scaling up deployment ovn-central to " + strconv.Itoa(int(replicas)))
	deployClient.SetScale(deploy.Name, replicas)

	ginkgo.By("Waiting for deployment ovn-central to be ready")
	deployClient.RolloutStatus(deploy.Name)

	ginkgo.By("Getting ovn-central pods after scale up")
	pods, err = deployClient.GetAllPods(deploy)
	framework.ExpectNoError(err)
	framework.ExpectHaveLen(pods.Items, int(replicas))

	ginkgo.By("Checking db files on all ovn-central pods are healthy after corruption recovery")
	newNodes.Clear()
	for pod := range slices.Values(pods.Items) {
		newNodes.Insert(pod.Spec.NodeName)
		ginkgo.By("Checking whether db file " + dbFile + " on node " + pod.Spec.NodeName + " is healthy")
		stdout, stderr, err := framework.ExecShellInPod(context.Background(), f, pod.Namespace, pod.Name, checkCmd)
		framework.ExpectNoError(err, fmt.Sprintf("failed to check db file %q: stdout = %q, stderr = %q", dbFile, stdout, stderr))
	}
	framework.ExpectEqual(newNodes, nodes, "the set of nodes hosting ovn-central pods should be the same as before")

	ginkgo.By("Getting birth time of db file " + dbFileHost + " on node " + nodeName + " after corruption recovery")
	newBirth, _ := getFileInfo(node, dbFileHost)
	framework.Logf("birth time of db file %s on node %s after corruption recovery: %s", dbFileHost, nodeName, newBirth)
	framework.ExpectNotEqual(newBirth, birth, "birth time of db file %q on node %s should change after corruption recovery", dbFileHost, nodeName)
}

var _ = framework.Describe("[group:ha]", func() {
	f := framework.NewDefaultFramework("ha")
	f.SkipNamespaceCreation = true
	kindNodes := make(map[string]*kind.Node)
	var clusterName string
	var skip bool

	ginkgo.BeforeEach(func() {
		if skip {
			ginkgo.Skip("OVN database HA spec only runs on kind clusters")
		}

		if clusterName == "" {
			ginkgo.By("Getting k8s nodes")
			k8sNodes, err := e2enode.GetReadySchedulableNodes(context.Background(), f.ClientSet)
			framework.ExpectNoError(err)

			cluster, ok := kind.IsKindProvided(k8sNodes.Items[0].Spec.ProviderID)
			if !ok {
				skip = true
				ginkgo.Skip("OVN database HA spec only runs on kind clusters")
			}
			clusterName = cluster

			ginkgo.By("Getting kind nodes")
			nodes, err := kind.ListNodes(clusterName, "")
			framework.ExpectNoError(err, "getting nodes in kind cluster")
			framework.ExpectNotEmpty(nodes)
			for node := range slices.Values(nodes) {
				kindNodes[node.Name()] = ptr.To(node)
			}
		}
	})

	framework.DisruptiveIt("ovn db should recover automatically from db file corruption", func() {
		f.SkipVersionPriorTo(1, 11, "This feature was introduced in v1.11")

		ginkgo.By("Getting deployment ovn-central")
		deployClient := f.DeploymentClientNS(framework.KubeOvnNamespace)
		deploy := deployClient.Get("ovn-central")
		replicas := *deploy.Spec.Replicas
		framework.ExpectNotZero(replicas)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Restoring deployment ovn-central replicas to " + strconv.Itoa(int(replicas)))
			deployClient.SetScale(deploy.Name, replicas)
		})

		ginkgo.By("Waiting for deployment ovn-central to be ready")
		deployClient.RolloutStatus(deploy.Name)

		ginkgo.By("Getting ovn db server ids from db files")
		dbSids := getDbSids(f, deploy)

		ginkgo.By("Getting ovn db server ids from cluster status")
		clusterDbSids := getDbSidsFromClusterStatus(f, deploy)

		ginkgo.By("Comparing ovn db server ids from db files and from cluster status")
		if !maps.EqualFunc(dbSids, clusterDbSids, func(a, b set.Set[string]) bool {
			return a.Equal(b)
		}) {
			framework.Failf("ovn db server ids from cluster status do not match those from db files:\nfrom db files:\n%s\nfrom cluster status:\n%s",
				format.Object(dbSids, 2), format.Object(clusterDbSids, 2))
		}

		for _, db := range [...]string{"nb", "sb"} {
			dbFile := dbFilePath(db)
			dbFileHost := dbFileHostPath(db)
			testcases := map[string]bool{
				// Truncate the db file to a larger size or a smaller size
				fmt.Sprintf(`truncate --no-create --size=+$((5+$RANDOM%%5)) "%s"`, dbFileHost): true,
				fmt.Sprintf(`truncate --no-create --size=-$((5+$RANDOM%%5)) "%s"`, dbFileHost): true,
				// The following two corruption methods only work for OVN >= 1.14
				fmt.Sprintf(`: > "%s"`, dbFileHost):   !f.VersionPriorTo(1, 14),
				fmt.Sprintf(`rm -f "%s"`, dbFileHost): !f.VersionPriorTo(1, 14),
			}
			for cmd, runSpec := range testcases {
				if runSpec {
					corruptAndRecover(f, deploy, dbFile, dbFileHost, cmd, kindNodes)
				}
			}
		}

		ginkgo.By("Checking whether server ids are preserved after corruption recovery")
		newDbSids := getDbSids(f, deploy)
		if !maps.EqualFunc(dbSids, newDbSids, func(a, b set.Set[string]) bool {
			return a.Equal(b)
		}) {
			framework.Failf("ovn db server ids changed after corruption recovery:\nbefore:\n%s\nafter:\n%s",
				format.Object(dbSids, 2), format.Object(newDbSids, 2))
		}

		ginkgo.By("Getting ovn db server ids from cluster status")
		clusterDbSids = getDbSidsFromClusterStatus(f, deploy)

		ginkgo.By("Checking whether server ids from cluster status match those from db files after corruption recovery")
		if !maps.EqualFunc(dbSids, clusterDbSids, func(a, b set.Set[string]) bool {
			return a.Equal(b)
		}) {
			framework.Failf("ovn db server ids from cluster status do not match those from db files:\nfrom db files:\n%s\nfrom cluster status:\n%s",
				format.Object(dbSids, 2), format.Object(clusterDbSids, 2))
		}
	})
})
