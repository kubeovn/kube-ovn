package kubevirt

import (
	"context"
	"flag"
	"os"
	"path/filepath"
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
	if k8sframework.TestContext.KubeConfig == "" {
		k8sframework.TestContext.KubeConfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
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
		f.SkipVersionPriorTo(1, 12, "Only test kubevirt vm keep ip in master")

		ginkgo.By("Get kubevirt vm pod")
		podList, err := podClient.List(context.TODO(), metav1.ListOptions{
			LabelSelector: "vm.kubevirt.io/name=testvm",
		})
		framework.ExpectNoError(err)
		framework.ExpectEqual(len(podList.Items), 1)

		ginkgo.By("Validating pod annotations")
		pod := podList.Items[0]
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, "ovn.kubernetes.io/virtualmachine", "testvm")
		ipAddr := pod.Annotations[util.IpAddressAnnotation]

		ginkgo.By("Deleting pod " + pod.Name)
		podClient.DeleteSync(pod.Name)
		framework.ExpectNoError(err)

		ginkgo.By("Check kubevirt vm pod after rebuild")
		podList, err = podClient.List(context.TODO(), metav1.ListOptions{
			LabelSelector: "vm.kubevirt.io/name=testvm",
		})
		framework.ExpectNoError(err)
		framework.ExpectEqual(len(podList.Items), 1)

		ginkgo.By("Validating new pod annotations")
		pod = podList.Items[0]
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, "ovn.kubernetes.io/virtualmachine", "testvm")

		ginkgo.By("Check vm pod ip unchanged" + pod.Name)
		ipNewAddr := pod.Annotations[util.IpAddressAnnotation]
		framework.ExpectEqual(ipAddr, ipNewAddr)
	})
})
