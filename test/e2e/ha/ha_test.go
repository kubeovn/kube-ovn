package ha

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"testing"
	"time"

	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	// Register flags.
	config.CopyFlags(config.Flags, flag.CommandLine)
	k8sframework.RegisterCommonFlags(flag.CommandLine)
	k8sframework.RegisterClusterFlags(flag.CommandLine)
}

func TestE2E(t *testing.T) {
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)
	e2e.RunE2ETests(t)
}

var _ = framework.Describe("[group:ha]", func() {
	f := framework.NewDefaultFramework("ha")
	f.SkipNamespaceCreation = true

	framework.DisruptiveIt("ovn db should recover automatically from db file corruption", func() {
		f.SkipVersionPriorTo(1, 11, "This feature was introduced in v1.11")

		ginkgo.By("Getting daemonset ovs-ovn")
		dsClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
		ds := dsClient.Get("ovs-ovn")

		ginkgo.By("Getting deployment ovn-central")
		deployClient := f.DeploymentClientNS(framework.KubeOvnNamespace)
		deploy := deployClient.Get("ovn-central")
		replicas := *deploy.Spec.Replicas
		framework.ExpectNotZero(replicas)

		ginkgo.By("Ensuring deployment ovn-central is ready")
		deployClient.RolloutStatus(deploy.Name)

		ginkgo.By("Getting nodes running deployment ovn-central")
		deployClient.RolloutStatus(deploy.Name)
		pods, err := deployClient.GetPods(deploy)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(pods.Items, int(replicas))
		nodes := make([]string, 0, int(replicas))
		for _, pod := range pods.Items {
			nodes = append(nodes, pod.Spec.NodeName)
		}

		ginkgo.By("Setting size of deployment ovn-central to 0")
		deployClient.SetScale(deploy.Name, 0)

		ginkgo.By("Waiting for ovn-central pods to disappear")
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			pods, err := deployClient.GetAllPods(deploy)
			if err != nil {
				return false, err
			}
			return len(pods.Items) == 0, nil
		}, "")

		db := "/etc/ovn/ovnnb_db.db"
		checkCmd := fmt.Sprintf("ovsdb-tool check-cluster " + db)
		corruptCmd := fmt.Sprintf(`bash -c 'dd if=/dev/zero of="%s" bs=1 count=$((10+$RANDOM%%10)) seek=$(stat -c %%s "%s")'`, db, db)
		for _, node := range nodes {
			ginkgo.By("Getting ovs-ovn pod running on node " + node)
			pod, err := dsClient.GetPodOnNode(ds, node)
			framework.ExpectNoError(err)

			ginkgo.By("Ensuring db file " + db + " on node " + node + " is ok")
			stdout, stderr, err := framework.ExecShellInPod(context.Background(), f, pod.Namespace, pod.Name, checkCmd)
			framework.ExpectNoError(err, fmt.Sprintf("failed to check db file %q: stdout = %q, stderr = %q", db, stdout, stderr))
			ginkgo.By("Corrupting db file " + db + " on node " + node)
			stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, pod.Namespace, pod.Name, corruptCmd)
			framework.ExpectNoError(err, fmt.Sprintf("failed to corrupt db file %q: stdout = %q, stderr = %q", db, stdout, stderr))
			ginkgo.By("Ensuring db file " + db + " on node " + node + " is corrupted")
			stdout, stderr, err = framework.ExecShellInPod(context.Background(), f, pod.Namespace, pod.Name, checkCmd)
			framework.ExpectError(err)
			framework.Logf("command output: stdout = %q, stderr = %q", stdout, stderr)
		}

		ginkgo.By("Setting size of deployment ovn-central to " + strconv.Itoa(int(replicas)))
		deployClient.SetScale(deploy.Name, replicas)

		ginkgo.By("Waiting for deployment ovn-central to be ready")
		deployClient.RolloutStatus(deploy.Name)
	})
})
