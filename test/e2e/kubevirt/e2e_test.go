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

	"github.com/kubeovn/kube-ovn/pkg/ovs"
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

	var vmName, subnetName, namespaceName string
	var subnetClient *framework.SubnetClient
	var podClient *framework.PodClient
	var vmClient *framework.VMClient
	var ipClient *framework.IPClient
	ginkgo.BeforeEach(func() {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12.")

		namespaceName = f.Namespace.Name
		vmName = "vm-" + framework.RandomSuffix()
		subnetName = "subnet-" + framework.RandomSuffix()
		subnetClient = f.SubnetClient()
		podClient = f.PodClientNS(namespaceName)
		vmClient = f.VMClientNS(namespaceName)
		ipClient = f.IPClient()

		ginkgo.By("Creating vm " + vmName)
		vm := framework.MakeVM(vmName, image, "small", true)
		_ = vmClient.CreateSync(vm)
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting vm " + vmName)
		vmClient.DeleteSync(vmName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
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
		framework.ExpectEqual(ips, pod.Status.PodIPs)
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

		ginkgo.By("Starting vm " + vmName)
		vmClient.StartSync(vmName)

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
		framework.ExpectEqual(ips, pod.Status.PodIPs)
	})

	framework.ConformanceIt("should be able to change vm subnet after deleting the old ip", func() {
		ginkgo.By("Creating subnet " + subnetName)
		cidr := framework.RandomCIDR(f.ClusterIPFamily)
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		subnet = subnetClient.CreateSync(subnet)

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

		ginkgo.By("Stopping vm " + vmName)
		vmClient.StopSync(vmName)
		portName := ovs.PodNameToPortName(vmName, namespaceName, subnet.Spec.Provider)
		oldVMIP := ipClient.Get(portName)
		framework.ExpectNotEmpty(oldVMIP.Spec.IPAddress)
		ginkgo.By("Updating vm " + vmName + " to use new subnet " + subnetName)
		// delete old ip, lsp, ipam
		ipClient.DeleteSync(portName)

		vm := vmClient.Get(vmName).DeepCopy()
		vm.Spec.Template.ObjectMeta.Annotations[util.LogicalSwitchAnnotation] = subnetName
		vmClient.UpdateSync(vm)

		ginkgo.By("Starting vm " + vmName)
		vmClient.StartSync(vmName)
		// new ip is the same name as the old one
		newVMIP := ipClient.Get(portName)
		framework.ExpectNotEmpty(newVMIP.Spec.IPAddress)

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
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnetName)

		ginkgo.By("Checking whether pod ips are changed")
		framework.ExpectNotEqual(newVMIP.Spec.IPAddress, oldVMIP.Spec.IPAddress)
	})
})
