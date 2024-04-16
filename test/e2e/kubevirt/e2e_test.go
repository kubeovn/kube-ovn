package kubevirt

import (
	"context"
	"flag"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	v1 "kubevirt.io/api/core/v1"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

const image = "quay.io/kubevirt/cirros-container-disk-demo:latest"

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

	var vmName, namespaceName string
	var podClient *framework.PodClient
	var vmClient *framework.VMClient
	ginkgo.BeforeEach(func() {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12.")

		namespaceName = f.Namespace.Name
		vmName = "vm-" + framework.RandomSuffix()
		podClient = f.PodClientNS(namespaceName)
		vmClient = f.VMClientNS(namespaceName)

		ginkgo.By("Creating vm " + vmName)
		vm := framework.MakeVM(vmName, image, "small", true)
		_ = vmClient.CreateSync(vm)
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting vm " + vmName)
		vmClient.DeleteSync(vmName)
	})

	framework.ConformanceIt("should be able to keep pod ips after vm pod is deleted", func() {
		ginkgo.By("Getting pod of vm " + vmName)
		labelSelector := fmt.Sprintf("%s=%s", v1.VirtualMachineNameLabel, vmName)
		podList, err := podClient.List(context.TODO(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(podList.Items, 1)

		ginkgo.By("Validating pod annotations")
		pod := &podList.Items[0]
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.VMAnnotation, vmName)
		ips := pod.Status.PodIPs

		ginkgo.By("Deleting pod " + pod.Name)
		podClient.DeleteSync(pod.Name)
		framework.ExpectNoError(err)

		ginkgo.By("Waiting for vm " + vmName + " to be ready")
		err = vmClient.WaitToBeReady(vmName, 2*time.Minute)
		framework.ExpectNoError(err)

		ginkgo.By("Getting pod of vm " + vmName)
		podList, err = podClient.List(context.TODO(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(podList.Items, 1)

		ginkgo.By("Validating new pod annotations")
		pod = &podList.Items[0]
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.VMAnnotation, vmName)

		ginkgo.By("Checking whether pod ips are changed")
		framework.ExpectConsistOf(ips, pod.Status.PodIPs)
	})

	framework.ConformanceIt("should be able to keep pod ips after the vm is restarted", func() {
		ginkgo.By("Getting pod of vm " + vmName)
		labelSelector := fmt.Sprintf("%s=%s", v1.VirtualMachineNameLabel, vmName)
		podList, err := podClient.List(context.TODO(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(podList.Items, 1)

		ginkgo.By("Validating pod annotations")
		pod := &podList.Items[0]
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.VMAnnotation, vmName)
		ips := pod.Status.PodIPs

		ginkgo.By("Stopping vm " + vmName)
		vmClient.StopSync(vmName)
		framework.ExpectNoError(err)

		ginkgo.By("Starting vm " + vmName)
		vmClient.StartSync(vmName)
		framework.ExpectNoError(err)

		ginkgo.By("Getting pod of vm " + vmName)
		podList, err = podClient.List(context.TODO(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(podList.Items, 1)

		ginkgo.By("Validating new pod annotations")
		pod = &podList.Items[0]
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.VMAnnotation, vmName)

		ginkgo.By("Checking whether pod ips are changed")
		framework.ExpectConsistOf(ips, pod.Status.PodIPs)
	})
})
