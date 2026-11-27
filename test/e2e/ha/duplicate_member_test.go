package ha

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/set"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

func readCentralDBSID(f *framework.Framework, pod corev1.Pod, db string) string {
	ginkgo.GinkgoHelper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stdout, stderr, err := framework.ExecShellInContainerContext(ctx, f, pod.Namespace, pod.Name, "ovn-central", fmt.Sprintf("ovsdb-tool db-sid %q", dbFilePath(db)))
	framework.ExpectNoError(err, "failed to read ovn%s SID from pod %s: stdout=%q stderr=%q", db, pod.Name, stdout, stderr)
	sid := strings.TrimSpace(stdout)
	framework.ExpectNotEmpty(sid, "empty ovn%s SID from pod %s", db, pod.Name)
	return sid[:4]
}

func removeCentralDatabaseFiles(node *kind.Node) {
	ginkgo.GinkgoHelper()
	for _, db := range []string{"nb", "sb"} {
		for _, path := range []string{dbFileHostPath(db), raftHeaderFileHostPath(db)} {
			stdout, stderr, err := node.Exec("rm", "-f", path)
			framework.ExpectNoError(err, "failed to remove %s: stdout=%s stderr=%s", path, stdout, stderr)
		}
	}
}

func waitForReadyCentralPods(f *framework.Framework, deploy *appsv1.Deployment, expected int) {
	ginkgo.GinkgoHelper()
	client := f.DeploymentClientNS(deploy.Namespace)
	framework.WaitUntil(time.Second, 2*time.Minute, func(ctx context.Context) (bool, error) {
		pods, err := getCentralPods(client, deploy)
		if err != nil {
			return false, err
		}
		ready := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning {
				continue
			}
			for _, status := range pod.Status.ContainerStatuses {
				if status.Name == "ovn-central" && status.Ready {
					ready++
					break
				}
			}
		}
		return ready == expected, nil
	}, fmt.Sprintf("%d ready ovn-central pods", expected))
}

func registerDuplicateMemberHA(f *framework.Framework, nodes map[string]*kind.Node) {
	framework.DisruptiveIt("ovn db should remove stale duplicate members after database directory loss", func() {
		f.SkipVersionPriorTo(1, 15, "Static duplicate Raft member cleanup is supported from v1.15")

		client := f.DeploymentClientNS(framework.KubeOvnNamespace)
		deploy := getCentralDeployment(client)
		replicas := *deploy.Spec.Replicas
		if replicas < 3 {
			ginkgo.Skip("duplicate Raft member recovery requires at least three central replicas")
		}
		client.RolloutStatus(deploy.Name)
		waitForCentralLeaders(f)

		pods, err := getCentralPods(client, deploy)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(pods.Items, int(replicas))
		oldSIDs := make(map[string]map[string]string, len(pods.Items))
		originalNodes := set.New[string]()
		for _, pod := range pods.Items {
			framework.ExpectHaveKey(nodes, pod.Spec.NodeName)
			originalNodes.Insert(pod.Spec.NodeName)
			oldSIDs[pod.Spec.NodeName] = map[string]string{
				"nb": readCentralDBSID(f, pod, "nb"),
				"sb": readCentralDBSID(f, pod, "sb"),
			}
		}

		ginkgo.DeferCleanup(func() {
			setCentralReplicas(client, deploy.Name, replicas)
			client.RolloutStatus(deploy.Name)
			waitForCentralLeaders(f)
		})

		ginkgo.By("Stopping one central member and removing its database files and Raft headers")
		setCentralReplicas(client, deploy.Name, replicas-1)
		framework.WaitUntil(time.Second, time.Minute, func(ctx context.Context) (bool, error) {
			current, err := getCentralPods(client, deploy)
			return err == nil && len(current.Items) == int(replicas-1), err
		}, "one ovn-central pod to stop")
		client.RolloutStatus(deploy.Name)
		remaining, err := getCentralPods(client, deploy)
		framework.ExpectNoError(err)
		currentNodes := set.New[string]()
		for _, pod := range remaining.Items {
			currentNodes.Insert(pod.Spec.NodeName)
		}
		removed := originalNodes.Difference(currentNodes)
		framework.ExpectHaveLen(removed, 1, "exactly one central node must be removed")
		removedNode := removed.UnsortedList()[0]
		removeCentralDatabaseFiles(nodes[removedNode])

		ginkgo.By("Starting the member with an empty database directory")
		setCentralReplicas(client, deploy.Name, replicas)
		client.RolloutStatus(deploy.Name)
		current, err := getCentralPods(client, deploy)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(current.Items, int(replicas))

		ginkgo.By("Waiting for the old duplicate NB/SB SIDs to be removed")
		clusterSIDs := getDbSidsFromClusterStatus(f, deploy)
		for _, db := range []string{"nb", "sb"} {
			if clusterSIDs[db].Has(oldSIDs[removedNode][db]) {
				framework.Failf("stale %s SID %s on removed node %s was not kicked", db, oldSIDs[removedNode][db], removedNode)
			}
		}

		ginkgo.By("Scaling down a different central member after duplicate cleanup")
		setCentralReplicas(client, deploy.Name, replicas-1)
		framework.WaitUntil(time.Second, time.Minute, func(ctx context.Context) (bool, error) {
			remaining, err := getCentralPods(client, deploy)
			return err == nil && len(remaining.Items) == int(replicas-1), err
		}, "one central member to remain unavailable")
		waitForReadyCentralPods(f, deploy, int(replicas-1))
		waitForCentralLeaders(f)
	})
}
