package kubevirt

import (
	"context"
	"flag"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
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

var _ = framework.Describe("[group:kubevirt]", func() {
	f := framework.NewDefaultFramework("kubevirt")

	var podClient *framework.PodClient
	ginkgo.BeforeEach(func() {
		podClient = f.PodClientNS("default")
	})

	framework.ConformanceIt("Kubevirt vm pod should keep ip", func() {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12.")

		ginkgo.By("Get kubevirt vm pod")
		podList, err := podClient.List(context.TODO(), metav1.ListOptions{
			LabelSelector: "vm.kubevirt.io/name=testvm",
		})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(podList.Items, 1)

		ginkgo.By("Validating pod annotations")
		pod := &podList.Items[0]
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, "ovn.kubernetes.io/virtualmachine", "testvm")
		ipAddr := pod.Annotations[util.IPAddressAnnotation]

		ginkgo.By("Deleting pod " + pod.Name)
		podClient.DeleteSync(pod.Name)
		framework.ExpectNoError(err)

		ginkgo.By("Check kubevirt vm pod after rebuild")
		podList, err = podClient.List(context.TODO(), metav1.ListOptions{
			LabelSelector: "vm.kubevirt.io/name=testvm",
		})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(podList.Items, 1)

		pod = &podList.Items[0]
		ginkgo.By("Waiting for pod " + pod.Name + " to be running")
		podClient.WaitForRunning(pod.Name)

		ginkgo.By("Validating new pod annotations")
		pod = podClient.GetPod(pod.Name)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, "ovn.kubernetes.io/virtualmachine", "testvm")

		ginkgo.By("Check vm pod ip unchanged" + pod.Name)
		ipNewAddr := pod.Annotations[util.IPAddressAnnotation]
		framework.ExpectEqual(ipAddr, ipNewAddr)
	})
})
