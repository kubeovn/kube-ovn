package ha

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
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

func raftHeaderFileHostPath(db string) string {
	return fmt.Sprintf("/etc/origin/ovn/ovn%s_db.hdr", db)
}

func copyDbFiles(node *kind.Node, dir string) error {
	var files []string
	for _, db := range [...]string{"nb", "sb"} {
		files = append(files, dbFileHostPath(db), raftHeaderFileHostPath(db))
	}
	var failures []error
	for _, file := range files {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		output, err := exec.CommandContext(ctx, "docker", "cp", node.Name()+":"+file, filepath.Join(dir, filepath.Base(file))).CombinedOutput()
		cancel()
		if err != nil {
			failures = append(failures, fmt.Errorf("copy %s from node %s: %w: %s", file, node.Name(), err, output))
		}
	}
	return errors.Join(failures...)
}

type failedCheckCluster struct {
	podName string
	command string
	err     error
	stdout  string
	stderr  string
}

func saveDbFailureDiagnostics(pods []corev1.Pod, kindNodes map[string]*kind.Node, failureMessage string, failedCheck *failedCheckCluster, run func(corev1.Pod, string) (string, string, error)) (string, error) {
	baseDir := os.Getenv("HA_E2E_DIAGNOSTICS_DIR")
	if baseDir == "" {
		baseDir = filepath.Join(os.TempDir(), "kube-ovn-ha-e2e-diagnostics")
	}
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(baseDir, "db-failure-")
	if err != nil {
		return "", err
	}

	var failures []error
	if err := os.WriteFile(filepath.Join(dir, "failure.txt"), []byte(failureMessage+"\n"), 0o600); err != nil {
		failures = append(failures, fmt.Errorf("save failure message: %w", err))
	}
	if failedCheck != nil {
		result := fmt.Sprintf("pod: %s\ncommand: %s\nerror: %v\nstdout:\n%s\nstderr:\n%s\n", failedCheck.podName, failedCheck.command, failedCheck.err, failedCheck.stdout, failedCheck.stderr)
		if err := os.WriteFile(filepath.Join(dir, "failed-check-cluster.txt"), []byte(result), 0o600); err != nil {
			failures = append(failures, fmt.Errorf("save last check-cluster result: %w", err))
		}
	}
	for nodeName, node := range kindNodes {
		nodeDir := filepath.Join(dir, nodeName)
		if err := os.MkdirAll(nodeDir, 0o700); err != nil {
			failures = append(failures, fmt.Errorf("create directory for node %s: %w", nodeName, err))
			continue
		}
		if err := copyDbFiles(node, nodeDir); err != nil {
			failures = append(failures, err)
		}
	}

	for _, pod := range pods {
		podDir := filepath.Join(dir, pod.Spec.NodeName, pod.Name)
		if err := os.MkdirAll(podDir, 0o700); err != nil {
			failures = append(failures, fmt.Errorf("create directory for pod %s: %w", pod.Name, err))
			continue
		}
		for _, db := range [...]string{"nb", "sb"} {
			stdout, stderr, err := run(pod, "ovsdb-tool check-cluster "+dbFilePath(db))
			result := fmt.Sprintf("pod: %s\ncommand: ovsdb-tool check-cluster %s\nerror: %v\nstdout:\n%s\nstderr:\n%s\n", pod.Name, dbFilePath(db), err, stdout, stderr)
			if err != nil {
				failures = append(failures, fmt.Errorf("check cluster for pod %s: %w", pod.Name, err))
			}
			if err := os.WriteFile(filepath.Join(podDir, db+"-check-cluster.txt"), []byte(result), 0o600); err != nil {
				failures = append(failures, fmt.Errorf("save check-cluster output for pod %s: %w", pod.Name, err))
			}
			stdout, stderr, err = run(pod, cmdClusterStatus(db))
			result = fmt.Sprintf("pod: %s\ncommand: %s\nerror: %v\nstdout:\n%s\nstderr:\n%s\n", pod.Name, cmdClusterStatus(db), err, stdout, stderr)
			if err != nil {
				failures = append(failures, fmt.Errorf("cluster status for pod %s: %w", pod.Name, err))
			}
			if err := os.WriteFile(filepath.Join(podDir, db+"-cluster-status.txt"), []byte(result), 0o600); err != nil {
				failures = append(failures, fmt.Errorf("save cluster status for pod %s: %w", pod.Name, err))
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "snapshot-errors.txt"), []byte(fmt.Sprint(errors.Join(failures...))), 0o600); err != nil {
		failures = append(failures, fmt.Errorf("save snapshot errors: %w", err))
	}
	return dir, errors.Join(failures...)
}

var dbNames = map[string]string{
	"nb": ovnnb.DatabaseName,
	"sb": ovnsb.DatabaseName,
}

func cmdClusterStatus(db string) string {
	return fmt.Sprintf("ovn-appctl -t /var/run/ovn/ovn%s_db.ctl cluster/status %s", db, dbNames[db])
}

func getCentralDeployment(client *framework.DeploymentClient) *appsv1.Deployment {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	deploy, err := client.GetContext(ctx, "ovn-central")
	framework.ExpectNoError(err)
	return deploy
}

func getCentralPods(client *framework.DeploymentClient, deploy *appsv1.Deployment) (*corev1.PodList, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.GetAllPodsContext(ctx, deploy)
}

func setCentralReplicas(client *framework.DeploymentClient, name string, replicas int32) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	framework.ExpectNoError(client.SetScaleContext(ctx, name, replicas))
}

func clusterStatusHasAllServers(output string, expected int) bool {
	status := ""
	serverCount := 0
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		switch {
		case strings.HasPrefix(line, "Status:"):
			status = strings.TrimSpace(strings.TrimPrefix(line, "Status:"))
		case slices.Contains(fields, "at"):
			serverCount++
		}
	}
	return status == "cluster member" && serverCount == expected
}

func waitForDbRecovery(f *framework.Framework, pods []corev1.Pod) {
	ginkgo.GinkgoHelper()

	expected := len(pods)
	for pod := range slices.Values(pods) {
		for _, db := range [...]string{"nb", "sb"} {
			ginkgo.By("Waiting for ovn" + db + " db cluster on node " + pod.Spec.NodeName + " to show all servers")
			framework.WaitUntil(time.Second, 60*time.Second, func(ctx context.Context) (bool, error) {
				stdout, _, err := framework.ExecShellInPod(ctx, f, pod.Namespace, pod.Name, cmdClusterStatus(db))
				if err != nil {
					return false, nil
				}
				return clusterStatusHasAllServers(stdout, expected), nil
			}, fmt.Sprintf("ovn%s db cluster on node %s to show %d servers", db, pod.Spec.NodeName, expected))
		}
	}
}

func waitForCentralLeaders(f *framework.Framework) {
	ginkgo.GinkgoHelper()

	ginkgo.By("Waiting for OVN leaders to remain ready after database recovery")
	var readySince time.Time
	framework.WaitUntil(time.Second, 2*time.Minute, func(ctx context.Context) (bool, error) {
		pods, err := f.ClientSet.CoreV1().Pods(framework.KubeOvnNamespace).List(ctx, metav1.ListOptions{LabelSelector: "app=ovn-central"})
		if err != nil {
			readySince = time.Time{}
			framework.Logf("listing ovn-central pods while waiting for leaders: %v", err)
			return false, nil
		}

		leaders := map[string]int{"ovn-nb-leader": 0, "ovn-sb-leader": 0, "ovn-northd-leader": 0}
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
				continue
			}
			for _, container := range pod.Status.ContainerStatuses {
				if container.Name != "ovn-central" || container.State.Running == nil || !container.Ready {
					continue
				}
				for label := range leaders {
					if pod.Labels[label] == "true" {
						leaders[label]++
					}
				}
			}
		}
		for _, count := range leaders {
			if count != 1 {
				readySince = time.Time{}
				return false, nil
			}
		}
		if readySince.IsZero() {
			readySince = time.Now()
		}
		return time.Since(readySince) >= 10*time.Second, nil
	}, "one ready NB, SB, and northd leader for 10 consecutive seconds")
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
	pods, err := getCentralPods(deployClient, deploy)
	framework.ExpectNoError(err)
	framework.ExpectHaveLen(pods.Items, int(*deploy.Spec.Replicas))

	expectedCount := len(pods.Items)
	dbServers := make(map[string]map[string]string)
	for _, db := range [...]string{"nb", "sb"} {
		ginkgo.By("Waiting for ovn" + db + " db cluster to show all servers on every ovn-central pod")
		for pod := range slices.Values(pods.Items) {
			var lastStdout, lastStderr string
			framework.WaitUntil(time.Second, 30*time.Second, func(ctx context.Context) (bool, error) {
				stdout, stderr, err := framework.ExecShellInPod(ctx, f, pod.Namespace, pod.Name, cmdClusterStatus(db))
				if err != nil {
					return false, nil
				}
				lastStdout, lastStderr = stdout, stderr
				var count int
				for line := range strings.SplitSeq(stdout, "\n") {
					if slices.Contains(strings.Fields(line), "at") {
						count++
					}
				}
				return count == expectedCount, nil
			}, fmt.Sprintf("ovn%s db on pod %s to show %d servers", db, pod.Name, expectedCount))

			status := parseClusterStatus(lastStdout)
			framework.ExpectHaveLen(status.Servers, expectedCount, "unexpected number of servers in ovn%s db status in pod %s: stdout = %q, stderr = %q", db, pod.Name, lastStdout, lastStderr)
			if len(dbServers[db]) == 0 {
				dbServers[db] = maps.Clone(status.Servers)
			} else {
				framework.ExpectEqual(status.Servers, dbServers[db], "inconsistent servers in ovn%s db status in pod %s: stdout = %q, stderr = %q", db, pod.Name, lastStdout, lastStderr)
			}
		}
	}
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
	pods, err := getCentralPods(deployClient, deploy)
	framework.ExpectNoError(err)
	framework.ExpectHaveLen(pods.Items, int(*deploy.Spec.Replicas))

	dbSids := make(map[string]set.Set[string])
	for _, db := range [...]string{"nb", "sb"} {
		dbSids[db] = set.New[string]()
		cmd := fmt.Sprintf(`ovsdb-tool db-sid %q`, dbFilePath(db))
		for pod := range slices.Values(pods.Items) {
			ginkgo.By("Getting ovn" + db + " db server id from pod " + pod.Name)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			stdout, stderr, err := framework.ExecShellInPod(ctx, f, pod.Namespace, pod.Name, cmd)
			cancel()
			framework.ExpectNoError(err, fmt.Sprintf("failed to get ovn%s db server id from pod %s: stdout = %q, stderr = %q", db, pod.Name, stdout, stderr))
			dbSids[db].Insert(strings.TrimSpace(stdout)[:4])
		}
		framework.ExpectHaveLen(dbSids[db], int(*deploy.Spec.Replicas), "unexpected number of ovn%s db server ids from pods of deployment %s", db, deploy.Name)
	}
	framework.Logf("ovn db server ids from db files:\n%s", format.Object(dbSids, 2))

	return dbSids
}

func corruptAndRecover(f *framework.Framework, deploy *appsv1.Deployment, dbFile, dbFileHost, corruptCmd string, automaticRecoveryExpected bool, kindNodes map[string]*kind.Node, lastFailedCheck **failedCheckCluster) {
	ginkgo.GinkgoHelper()

	replicas := *deploy.Spec.Replicas

	ginkgo.By("Getting ovn-central pods")
	deployClient := f.DeploymentClientNS(deploy.Namespace)
	pods, err := getCentralPods(deployClient, deploy)
	framework.ExpectNoError(err)
	framework.ExpectHaveLen(pods.Items, int(replicas))

	checkCmd := "ovsdb-tool check-cluster " + dbFile
	for _, pod := range pods.Items {
		ginkgo.By("Waiting for db file " + dbFile + " on node " + pod.Spec.NodeName + " to be healthy before corruption")
		var lastStdout, lastStderr string
		var lastErr error
		err := wait.PollUntilContextTimeout(context.Background(), time.Second, 30*time.Second, false, func(ctx context.Context) (bool, error) {
			lastStdout, lastStderr, lastErr = framework.ExecShellInContainerContext(ctx, f, pod.Namespace, pod.Name, "ovn-central", checkCmd)
			return lastErr == nil, nil
		})
		if err != nil {
			*lastFailedCheck = &failedCheckCluster{podName: pod.Name, command: checkCmd, err: lastErr, stdout: lastStdout, stderr: lastStderr}
			framework.Failf("db file %s on pod %s did not become healthy: wait error = %v, last check error = %v, stdout = %q, stderr = %q",
				dbFile, pod.Name, err, lastErr, lastStdout, lastStderr)
		}
	}

	nodes := set.New[string]()
	for _, pod := range pods.Items {
		nodes.Insert(pod.Spec.NodeName)
	}

	ginkgo.By("Scaling down deployment ovn-central to " + strconv.Itoa(int(replicas-1)))
	setCentralReplicas(deployClient, deploy.Name, replicas-1)

	ginkgo.By("Waiting for an ovn-central pod to be deleted")
	framework.WaitUntil(time.Second, 30*time.Second, func(ctx context.Context) (bool, error) {
		pods, err := deployClient.GetAllPodsContext(ctx, deploy)
		if err != nil {
			return false, err
		}
		return len(pods.Items) == int(replicas-1), nil
	}, "")

	ginkgo.By("Waiting for deployment ovn-central to be ready")
	deployClient.RolloutStatus(deploy.Name)

	ginkgo.By("Getting ovn-central pods after scale down")
	pods, err = getCentralPods(deployClient, deploy)
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
	dbKind := strings.TrimPrefix(strings.TrimSuffix(filepath.Base(dbFile), "_db.db"), "ovn")
	headerFileHost := raftHeaderFileHostPath(dbKind)
	dbBackupFileHost := dbFileHost + ".kube-ovn-ha-backup"
	headerBackupFileHost := headerFileHost + ".kube-ovn-ha-backup"
	stdout, stderr, err := node.Exec("cp", dbFileHost, dbBackupFileHost)
	framework.ExpectNoError(err, fmt.Sprintf("failed to back up db file %q: stdout = %q, stderr = %q", dbFileHost, stdout, stderr))
	stdout, stderr, err = node.Exec("cp", headerFileHost, headerBackupFileHost)
	framework.ExpectNoError(err, fmt.Sprintf("failed to back up raft header %q: stdout = %q, stderr = %q", headerFileHost, stdout, stderr))
	stdout, stderr, err = node.Exec("bash", "-exc", corruptCmd)
	framework.ExpectNoError(err, fmt.Sprintf("failed to corrupt db file %q: stdout = %q, stderr = %q", dbFileHost, stdout, stderr))

	ginkgo.By("Getting file size of db file " + dbFileHost + " on node " + nodeName + " after corruption")
	corruptedBirth, corruptedSize := getFileInfo(node, dbFileHost)
	framework.Logf("file info of db file %s on node %s after corruption: birth time = %s, size = %s", dbFileHost, nodeName, corruptedBirth, corruptedSize)
	framework.ExpectNotEqual(corruptedSize, size, "file size of db file %q on node %s should change after corruption", dbFileHost, nodeName)

	ginkgo.By("Scaling up deployment ovn-central to " + strconv.Itoa(int(replicas)))
	setCentralReplicas(deployClient, deploy.Name, replicas)

	if automaticRecoveryExpected {
		ginkgo.By("Waiting for deployment ovn-central to be ready")
		deployClient.RolloutStatus(deploy.Name)
	} else {
		ginkgo.By("Waiting for the corrupted ovn-central member to restart without rewriting its database")
		framework.WaitUntil(time.Second, 60*time.Second, func(_ context.Context) (bool, error) {
			pods, err := getCentralPods(deployClient, deploy)
			if err != nil || len(pods.Items) != int(replicas) {
				return false, err
			}
			for _, pod := range pods.Items {
				if pod.Spec.NodeName != nodeName {
					continue
				}
				for _, status := range pod.Status.ContainerStatuses {
					if status.Name == "ovn-central" && status.RestartCount > 0 {
						return true, nil
					}
				}
			}
			return false, nil
		}, "corrupted ovn-central member to restart")
		gotBirth, gotSize := getFileInfo(node, dbFileHost)
		framework.ExpectEqual(gotBirth, corruptedBirth, "corrupted database birth time must remain unchanged")
		framework.ExpectEqual(gotSize, corruptedSize, "corrupted database size must remain unchanged")
	}

	if !automaticRecoveryExpected {
		ginkgo.By("Restoring the original database and raft header")
		stdout, stderr, err = node.Exec("cp", dbBackupFileHost, dbFileHost)
		framework.ExpectNoError(err, fmt.Sprintf("failed to restore db file %q: stdout = %q, stderr = %q", dbFileHost, stdout, stderr))
		stdout, stderr, err = node.Exec("cp", headerBackupFileHost, headerFileHost)
		framework.ExpectNoError(err, fmt.Sprintf("failed to restore raft header %q: stdout = %q, stderr = %q", headerFileHost, stdout, stderr))
	}

	ginkgo.By("Removing temporary recovery backups")
	stdout, stderr, err = node.Exec("rm", "-f", dbBackupFileHost, headerBackupFileHost)
	framework.ExpectNoError(err, fmt.Sprintf("failed to remove recovery backups: stdout = %q, stderr = %q", stdout, stderr))

	ginkgo.By("Waiting for deployment ovn-central to be ready after restoring the database")
	deployClient.RolloutStatus(deploy.Name)

	ginkgo.By("Getting ovn-central pods after scale up")
	pods, err = getCentralPods(deployClient, deploy)
	framework.ExpectNoError(err)
	framework.ExpectHaveLen(pods.Items, int(replicas))

	ginkgo.By("Checking db files on all ovn-central pods are healthy after database recovery")
	newNodes.Clear()
	for pod := range slices.Values(pods.Items) {
		newNodes.Insert(pod.Spec.NodeName)
	}
	waitForDbRecovery(f, pods.Items)
	framework.ExpectEqual(newNodes, nodes, "the set of nodes hosting ovn-central pods should be the same as before")

	ginkgo.By("Getting birth time of db file " + dbFileHost + " after database recovery")
	newBirth, _ := getFileInfo(node, dbFileHost)
	framework.Logf("birth time of db file %s on node %s after database recovery: %s", dbFileHost, nodeName, newBirth)
	if automaticRecoveryExpected {
		framework.ExpectNotEqual(newBirth, birth, "birth time of db file %q on node %s should change after automatic recovery", dbFileHost, nodeName)
	} else {
		framework.ExpectEqual(newBirth, birth, "restoring the original database should restore its birth time")
	}
}

var _ = framework.SerialDescribe("[group:ha]", func() {
	f := framework.NewDefaultFramework("ha")
	f.SkipNamespaceCreation = true
	kindNodes := make(map[string]*kind.Node)
	var clusterName string
	var skip bool
	var lastFailedCheck *failedCheckCluster
	var diagnosticsSaved bool

	ginkgo.BeforeEach(func() {
		lastFailedCheck = nil
		diagnosticsSaved = false
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
				kindNodes[node.Name()] = new(node)
			}
		}
	})
	captureFailure := func(report ginkgo.SpecReport, phase string) {
		if !report.Failed() || skip || diagnosticsSaved {
			return
		}
		failureMessage := fmt.Sprintf("capture phase: %s\n%s", phase, report.Failure.Message)
		var pods []corev1.Pod
		if f.ClientSet != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			list, err := f.ClientSet.CoreV1().Pods(framework.KubeOvnNamespace).List(ctx, metav1.ListOptions{LabelSelector: "app=ovn-central"})
			cancel()
			if err != nil {
				failureMessage += fmt.Sprintf("\nlist ovn-central pods: %v", err)
			} else {
				pods = list.Items
			}
		}
		dir, snapshotErr := saveDbFailureDiagnostics(pods, kindNodes, failureMessage, lastFailedCheck, func(pod corev1.Pod, command string) (string, string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return framework.ExecShellInContainerContext(ctx, f, pod.Namespace, pod.Name, "ovn-central", command)
		})
		diagnosticsSaved = dir != ""
		framework.Logf("HA E2E failure diagnostics: directory = %q, snapshot errors = %v", dir, snapshotErr)
	}
	ginkgo.JustAfterEach(func() {
		captureFailure(ginkgo.CurrentSpecReport(), "before cleanup")
	})
	ginkgo.ReportAfterEach(func(report ginkgo.SpecReport) {
		captureFailure(report, "after cleanup")
	})

	registerKubectlKoHA(f, kindNodes)

	framework.DisruptiveIt("ovn db should preserve invalid db files instead of recovering automatically", func() {
		f.SkipVersionPriorTo(1, 11, "This feature was introduced in v1.11")

		ginkgo.By("Getting deployment ovn-central")
		deployClient := f.DeploymentClientNS(framework.KubeOvnNamespace)
		deploy := getCentralDeployment(deployClient)
		replicas := *deploy.Spec.Replicas
		framework.ExpectNotZero(replicas)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Restoring deployment ovn-central replicas to " + strconv.Itoa(int(replicas)))
			setCentralReplicas(deployClient, deploy.Name, replicas)
			deployClient.RolloutStatus(deploy.Name)
			waitForCentralLeaders(f)
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
					automaticRecoveryExpected := strings.Contains(cmd, `rm -f`)
					corruptAndRecover(f, deploy, dbFile, dbFileHost, cmd, automaticRecoveryExpected, kindNodes, &lastFailedCheck)
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

	framework.DisruptiveIt("ovn db should recover automatically when the db is missing and the raft header is invalid", func() {
		f.SkipVersionPriorTo(1, 15, "This feature was introduced in v1.15")

		ginkgo.By("Getting deployment ovn-central")
		deployClient := f.DeploymentClientNS(framework.KubeOvnNamespace)
		deploy := getCentralDeployment(deployClient)
		replicas := *deploy.Spec.Replicas
		framework.ExpectNotZero(replicas)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Restoring deployment ovn-central replicas to " + strconv.Itoa(int(replicas)))
			setCentralReplicas(deployClient, deploy.Name, replicas)
			deployClient.RolloutStatus(deploy.Name)
			waitForCentralLeaders(f)
		})

		ginkgo.By("Waiting for deployment ovn-central to be ready")
		deployClient.RolloutStatus(deploy.Name)

		const zeroClusterID = "00000000-0000-0000-0000-000000000000"
		dbFile := dbFilePath("nb")
		dbFileHost := dbFileHostPath("nb")
		hdrFileHost := raftHeaderFileHostPath("nb")
		poisonCmd := fmt.Sprintf(`
for i in $(seq 1 30); do
    [ -s %[1]q ] && break
    sleep 1
done
test -s %[1]q
sed -i -E 's/"cluster_id": "[^"]+"/"cluster_id": "%[2]s"/' %[1]q
grep -q '"cluster_id": "%[2]s"' %[1]q
rm -f %[3]q
`, hdrFileHost, zeroClusterID, dbFileHost)

		corruptAndRecover(f, deploy, dbFile, dbFileHost, poisonCmd, true, kindNodes, &lastFailedCheck)

		ginkgo.By("Checking every ovn-central pod sees a converged three-member OVN DB cluster")
		clusterDbSids := getDbSidsFromClusterStatus(f, deploy)
		framework.ExpectHaveLen(clusterDbSids["nb"], int(replicas), "unexpected number of ovnnb db servers after invalid raft header recovery")
	})
})
