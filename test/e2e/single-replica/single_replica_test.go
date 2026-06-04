package single_replica

import (
	"context"
	"flag"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	config.CopyFlags(config.Flags, flag.CommandLine)
	k8sframework.RegisterCommonFlags(flag.CommandLine)
	k8sframework.RegisterClusterFlags(flag.CommandLine)
}

func TestE2E(t *testing.T) {
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)
	e2e.RunE2ETests(t)
}

// isSingleReplicaMode reports whether the cluster was installed with
// OVN_CENTRAL_MODE=single. The ovn-nb Service has its `ovn-nb-leader: "true"`
// selector dropped in single mode, which is a reliable, install-method-agnostic
// signal.
func isSingleReplicaMode(f *framework.Framework) bool {
	ginkgo.GinkgoHelper()

	svc, err := f.ClientSet.CoreV1().Services(framework.KubeOvnNamespace).
		Get(context.TODO(), "ovn-nb", metav1.GetOptions{})
	framework.ExpectNoError(err, "getting Service kube-system/ovn-nb")
	_, hasLeaderSelector := svc.Spec.Selector["ovn-nb-leader"]
	return !hasLeaderSelector
}

var _ = framework.Describe("[group:single-replica]", func() {
	f := framework.NewDefaultFramework("single-replica")

	ginkgo.BeforeEach(func() {
		f.SkipVersionPriorTo(1, 18, "Single-replica ovn-central mode was introduced in v1.18")
		if !isSingleReplicaMode(f) {
			ginkgo.Skip("ovn-central is not deployed in single-replica mode; skipping")
		}
	})

	ginkgo.It("should deploy ovn-central as a single-replica Deployment backed by a PVC", func() {
		ginkgo.By("Getting Deployment kube-system/ovn-central")
		deployClient := f.DeploymentClientNS(framework.KubeOvnNamespace)
		deploy := deployClient.Get("ovn-central")

		ginkgo.By("Verifying replicas == 1")
		framework.ExpectNotNil(deploy.Spec.Replicas)
		gomega.Expect(*deploy.Spec.Replicas).To(gomega.BeEquivalentTo(1))

		ginkgo.By("Verifying strategy is Recreate")
		gomega.Expect(deploy.Spec.Strategy.Type).To(gomega.Equal(appsv1.RecreateDeploymentStrategyType))

		ginkgo.By("Verifying the host-config-ovn volume is backed by PVC ovn-central-data")
		var found bool
		for _, v := range deploy.Spec.Template.Spec.Volumes {
			if v.Name != "host-config-ovn" {
				continue
			}
			found = true
			framework.ExpectNotNil(v.PersistentVolumeClaim, "host-config-ovn volume should be a PVC in single-replica mode, not hostPath")
			gomega.Expect(v.PersistentVolumeClaim.ClaimName).To(gomega.Equal("ovn-central-data"))
		}
		gomega.Expect(found).To(gomega.BeTrue(), "host-config-ovn volume must be present on the Deployment")

		ginkgo.By("Verifying PVC kube-system/ovn-central-data is Bound")
		pvc, err := f.ClientSet.CoreV1().PersistentVolumeClaims(framework.KubeOvnNamespace).
			Get(context.TODO(), "ovn-central-data", metav1.GetOptions{})
		framework.ExpectNoError(err)
		gomega.Expect(string(pvc.Status.Phase)).To(gomega.Equal("Bound"))
	})

	ginkgo.It("should expose ovn-nb / ovn-sb / ovn-northd via leader-less Services", func() {
		for _, name := range []string{"ovn-nb", "ovn-sb", "ovn-northd"} {
			ginkgo.By("Inspecting Service kube-system/" + name)
			svc, err := f.ClientSet.CoreV1().Services(framework.KubeOvnNamespace).
				Get(context.TODO(), name, metav1.GetOptions{})
			framework.ExpectNoError(err)
			gomega.Expect(svc.Spec.Selector).To(gomega.HaveKeyWithValue("app", "ovn-central"))
			// In single-replica mode the leader-label selectors are dropped so
			// the single pod is always the Service's endpoint.
			gomega.Expect(svc.Spec.Selector).NotTo(gomega.HaveKey("ovn-nb-leader"))
			gomega.Expect(svc.Spec.Selector).NotTo(gomega.HaveKey("ovn-sb-leader"))
			gomega.Expect(svc.Spec.Selector).NotTo(gomega.HaveKey("ovn-northd-leader"))
		}
	})

	ginkgo.It("should keep the ovn-central pod labelled as leader for all three databases", func() {
		ginkgo.By("Listing pods of ovn-central")
		pods, err := f.ClientSet.CoreV1().Pods(framework.KubeOvnNamespace).
			List(context.TODO(), metav1.ListOptions{LabelSelector: "app=ovn-central"})
		framework.ExpectNoError(err)
		gomega.Expect(pods.Items).To(gomega.HaveLen(1), "single-replica mode must have exactly one ovn-central pod")

		ginkgo.By("Waiting for the leader-checker to apply ovn-*-leader=true labels")
		pod := pods.Items[0]
		framework.WaitUntil(2*time.Second, time.Minute, func(ctx context.Context) (bool, error) {
			p, err := f.ClientSet.CoreV1().Pods(framework.KubeOvnNamespace).Get(ctx, pod.Name, metav1.GetOptions{})
			if err != nil {
				return false, nil
			}
			for _, label := range []string{"ovn-nb-leader", "ovn-sb-leader", "ovn-northd-leader"} {
				if p.Labels[label] != "true" {
					return false, nil
				}
			}
			return true, nil
		}, "pod "+pod.Name+" to be labelled as leader for all three databases")
	})

	ginkgo.It("should support basic subnet+pod networking", func() {
		subnetClient := f.SubnetClient()
		podClient := f.PodClient()
		suffix := framework.RandomSuffix()
		subnetName := "single-replica-subnet-" + suffix
		podName := "single-replica-pod-" + suffix
		cidr := framework.RandomCIDR(f.ClusterIPFamily)

		ginkgo.By("Creating subnet " + subnetName)
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		_ = subnetClient.CreateSync(subnet)
		ginkgo.DeferCleanup(func() { subnetClient.DeleteSync(subnetName) })

		ginkgo.By("Creating pod " + podName + " on the new subnet")
		annotations := map[string]string{util.LogicalSwitchAnnotation: subnetName}
		pod := framework.MakePod(f.Namespace.Name, podName, nil, annotations, framework.AgnhostImage, nil, nil)
		_ = podClient.CreateSync(pod)
		ginkgo.DeferCleanup(func() { podClient.DeleteSync(podName) })

		ginkgo.By("Verifying pod got an IP from the new subnet")
		pod = podClient.GetPod(podName)
		gomega.Expect(pod.Annotations[util.AllocatedAnnotation]).To(gomega.Equal("true"))
		gomega.Expect(pod.Annotations[util.IPAddressAnnotation]).NotTo(gomega.BeEmpty())
	})

	framework.DisruptiveIt("should recover after the ovn-central pod is deleted", func() {
		ginkgo.By("Recording the current ovn-central pod")
		deployClient := f.DeploymentClientNS(framework.KubeOvnNamespace)
		deploy := deployClient.Get("ovn-central")
		oldPods, err := deployClient.GetAllPods(deploy)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(oldPods.Items, 1)
		oldPodName := oldPods.Items[0].Name

		ginkgo.By("Deleting pod " + oldPodName)
		err = f.ClientSet.CoreV1().Pods(framework.KubeOvnNamespace).
			Delete(context.TODO(), oldPodName, metav1.DeleteOptions{})
		framework.ExpectNoError(err)

		ginkgo.By("Waiting for a fresh ovn-central pod to become Ready")
		deployClient.RolloutStatus("ovn-central")
		newPods, err := deployClient.GetAllPods(deploy)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(newPods.Items, 1)
		gomega.Expect(newPods.Items[0].Name).NotTo(gomega.Equal(oldPodName), "a new pod should be created after deletion")

		ginkgo.By("Verifying the OVN DB is still functional after recovery")
		subnetClient := f.SubnetClient()
		podClient := f.PodClient()
		suffix := framework.RandomSuffix()
		subnetName := "single-replica-recover-" + suffix
		podName := "single-replica-recover-pod-" + suffix
		cidr := framework.RandomCIDR(f.ClusterIPFamily)

		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		_ = subnetClient.CreateSync(subnet)
		ginkgo.DeferCleanup(func() { subnetClient.DeleteSync(subnetName) })

		annotations := map[string]string{util.LogicalSwitchAnnotation: subnetName}
		pod := framework.MakePod(f.Namespace.Name, podName, nil, annotations, framework.AgnhostImage, nil, nil)
		_ = podClient.CreateSync(pod)
		ginkgo.DeferCleanup(func() { podClient.DeleteSync(podName) })

		pod = podClient.GetPod(podName)
		gomega.Expect(pod.Annotations[util.AllocatedAnnotation]).To(gomega.Equal("true"))
		gomega.Expect(pod.Annotations[util.IPAddressAnnotation]).NotTo(gomega.BeEmpty())
	})
})
