package kubevirt

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	"k8s.io/utils/ptr"
	v1 "kubevirt.io/api/core/v1"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var image = "quay.io/kubevirt/cirros-container-disk-demo:v1.7.2"

func init() {
	if env := os.Getenv("KUBEVIRT_CONTAINERDISK_IMAGE"); env != "" {
		image = env
	}

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
		vm := framework.MakeVM(vmName, image, "small", ptr.To(v1.RunStrategyAlways))
		_ = vmClient.CreateSync(vm)
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting vm " + vmName)
		vmClient.DeleteSync(vmName)

		// Wait for the VM's IP CRD to be fully cleaned up before deleting subnet
		portName := ovs.PodNameToPortName(vmName, namespaceName, util.OvnProvider)
		ginkgo.By("Waiting for IP " + portName + " to be cleaned up")
		err := ipClient.WaitToDisappear(portName, time.Second, 2*time.Minute)
		framework.ExpectNoError(err)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
	})

	framework.ConformanceIt("should be able to keep pod ips after vm pod is deleted", func() {
		ginkgo.By("Getting pod of vm " + vmName)
		labelSelector := fmt.Sprintf("%s=%s", v1.VirtualMachineInstanceIDLabel, vmName)
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
		labelSelector := fmt.Sprintf("%s=%s", v1.VirtualMachineInstanceIDLabel, vmName)
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

		portName := ovs.PodNameToPortName(vmName, namespaceName, util.OvnProvider)
		ginkgo.By("Check ip resource " + portName)
		// the ip should exist after vm is stopped
		oldVMIP := ipClient.Get(portName)
		framework.ExpectNil(oldVMIP.DeletionTimestamp)
		ginkgo.By("Starting vm " + vmName)
		vmClient.StartSync(vmName)

		// new ip name is the same as the old one
		ginkgo.By("Check ip resource " + portName)
		newVMIP := ipClient.Get(portName)
		framework.ExpectEqual(oldVMIP.Spec, newVMIP.Spec)

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

	framework.ConformanceIt("should be able to handle vm restart when subnet changes before the vm is stopped", func() {
		// create a vm within a namespace, the namespace has no subnet, so the vm use ovn-default subnet
		// create a subnet in the namespace later, the vm should use its own subnet
		// stop the vm, the vm should delete the vm ip, because of the namespace only has one subnet but not ovn-default
		// start the vm, the vm should use the namespace owned subnet
		ginkgo.By("Creating subnet " + subnetName)
		cidr := framework.RandomCIDR(f.ClusterIPFamily)
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		_ = subnetClient.CreateSync(subnet)

		ginkgo.By("Getting pod of vm " + vmName)
		labelSelector := fmt.Sprintf("%s=%s", v1.VirtualMachineInstanceIDLabel, vmName)
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

		// the ip is deleted
		portName := ovs.PodNameToPortName(vmName, namespaceName, util.OvnProvider)
		err = ipClient.WaitToDisappear(portName, time.Second, 2*time.Minute)
		framework.ExpectNoError(err)

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
		framework.ExpectNotEqual(ips, pod.Status.PodIPs)

		ginkgo.By("Checking external-ids of LSP " + portName)
		cmd := "ovn-nbctl --format=list --data=bare --no-heading --columns=external_ids list Logical_Switch_Port " + portName
		output, _, err := framework.NBExec(cmd)
		framework.ExpectNoError(err)
		framework.ExpectContainElement(strings.Fields(string(output)), "ls="+subnetName)
	})

	framework.ConformanceIt("should be able to handle vm restart when subnet changes after the vm is stopped", func() {
		ginkgo.By("Getting pod of vm " + vmName)

		labelSelector := fmt.Sprintf("%s=%s", v1.VirtualMachineInstanceIDLabel, vmName)
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

		ginkgo.By("Creating subnet " + subnetName)
		cidr := framework.RandomCIDR(f.ClusterIPFamily)
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		_ = subnetClient.CreateSync(subnet)

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
		framework.ExpectNotEqual(ips, pod.Status.PodIPs)

		portName := ovs.PodNameToPortName(vmName, namespaceName, util.OvnProvider)
		ginkgo.By("Checking external-ids of LSP " + portName)
		cmd := "ovn-nbctl --format=list --data=bare --no-heading --columns=external_ids list Logical_Switch_Port " + portName
		output, _, err := framework.NBExec(cmd)
		framework.ExpectNoError(err)
		framework.ExpectContainElement(strings.Fields(string(output)), "ls="+subnetName)
	})

	framework.ConformanceIt("restart vm should be able to change vm subnet after deleting the old ip", func() {
		// case: test change vm subnet after stop vm and delete old ip
		// stop vm, delete the ip.
		// create new subnet in the namespace.
		// make sure ip changed after vm started
		ginkgo.By("Getting pod of vm " + vmName)
		labelSelector := fmt.Sprintf("%s=%s", v1.VirtualMachineInstanceIDLabel, vmName)
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

		// make sure the vm ip is still exist
		portName := ovs.PodNameToPortName(vmName, namespaceName, util.OvnProvider)
		oldVMIP := ipClient.Get(portName)
		framework.ExpectNotEmpty(oldVMIP.Spec.IPAddress)
		ipClient.DeleteSync(portName)
		// delete old ip to create the same name ip in other subnet

		ginkgo.By("Creating subnet " + subnetName)
		cidr := framework.RandomCIDR(f.ClusterIPFamily)
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		subnet = subnetClient.CreateSync(subnet)
		ginkgo.By("Updating vm " + vmName + " to use new subnet " + subnet.Name)

		// the vm should use the new subnet in the namespace
		ginkgo.By("Starting vm " + vmName)
		vmClient.StartSync(vmName)
		// new ip name is the same as the old one
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

		ginkgo.By("Checking external-ids of LSP " + portName)
		cmd := "ovn-nbctl --format=list --data=bare --no-heading --columns=external_ids list Logical_Switch_Port " + portName
		output, _, err := framework.NBExec(cmd)
		framework.ExpectNoError(err)
		framework.ExpectContainElement(strings.Fields(string(output)), "ls="+subnetName)
	})
})

var _ = framework.Describe("[group:kubevirt]", func() {
	f := framework.NewDefaultFramework("kubevirt-multus")

	var vmName, namespaceName string
	var subnetNameA, subnetNameB, nadNameA, nadNameB string
	var subnetClient *framework.SubnetClient
	var nadClient *framework.NetworkAttachmentDefinitionClient
	var podClient *framework.PodClient
	var vmClient *framework.VMClient
	var ipClient *framework.IPClient

	ginkgo.BeforeEach(func() {
		f.SkipVersionPriorTo(1, 14, "This feature was introduced in v1.14.")

		namespaceName = f.Namespace.Name
		vmName = "vm-" + framework.RandomSuffix()
		subnetNameA = "subnet-a-" + framework.RandomSuffix()
		subnetNameB = "subnet-b-" + framework.RandomSuffix()
		nadNameA = "nad-a-" + framework.RandomSuffix()
		nadNameB = "nad-b-" + framework.RandomSuffix()
		subnetClient = f.SubnetClient()
		nadClient = f.NetworkAttachmentDefinitionClientNS(namespaceName)
		podClient = f.PodClientNS(namespaceName)
		vmClient = f.VMClientNS(namespaceName)
		ipClient = f.IPClient()
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting vm " + vmName)
		vmClient.DeleteSync(vmName)

		ginkgo.By("Deleting NADs")
		nadClient.Delete(nadNameA)
		nadClient.Delete(nadNameB)

		ginkgo.By("Deleting subnets")
		subnetClient.DeleteSync(subnetNameA)
		subnetClient.DeleteSync(subnetNameB)
	})

	framework.ConformanceIt("should keep attachment network ip after vm pod is deleted", func() {
		providerA := fmt.Sprintf("%s.%s.ovn", nadNameA, namespaceName)

		ginkgo.By("Creating subnet " + subnetNameA)
		cidrA := framework.RandomCIDR(f.ClusterIPFamily)
		subnetA := framework.MakeSubnet(subnetNameA, "", cidrA, "", "", "", nil, nil, nil)
		subnetA.Spec.Provider = providerA
		_ = subnetClient.CreateSync(subnetA)

		ginkgo.By("Creating NAD " + nadNameA)
		nadA := framework.MakeOVNNetworkAttachmentDefinition(nadNameA, namespaceName, providerA, nil)
		_ = nadClient.Create(nadA)

		ginkgo.By("Creating vm " + vmName + " with multus network " + nadNameA)
		vm := framework.MakeVMWithMultusNetwork(vmName, image, "small", ptr.To(v1.RunStrategyAlways), nadNameA)
		_ = vmClient.CreateSync(vm)

		ginkgo.By("Getting pod of vm " + vmName)
		labelSelector := fmt.Sprintf("%s=%s", v1.VirtualMachineInstanceIDLabel, vmName)
		podList, err := podClient.List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(podList.Items, 1)

		pod := &podList.Items[0]
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")

		ginkgo.By("Checking attachment IP exists")
		attachPortName := ovs.PodNameToPortName(vmName, namespaceName, providerA)
		attachIP := ipClient.Get(attachPortName)
		framework.ExpectNotEmpty(attachIP.Spec.IPAddress)
		oldAttachIPAddr := attachIP.Spec.IPAddress

		ginkgo.By("Deleting pod " + pod.Name)
		podClient.DeleteSync(pod.Name)

		ginkgo.By("Waiting for vm " + vmName + " to be ready")
		err = vmClient.WaitToBeReady(vmName, 2*time.Minute)
		framework.ExpectNoError(err)

		ginkgo.By("Checking attachment IP is preserved")
		newAttachIP := ipClient.Get(attachPortName)
		framework.ExpectEqual(oldAttachIPAddr, newAttachIP.Spec.IPAddress)
	})

	// This test exercises the stop→patch NAD→start workflow. The old pod deletion
	// is processed before the NAD patch, so stale attachment IPs are cleaned up
	// during new pod creation (cleanStaleVMAttachmentIPs in reconcileAllocateSubnets).
	framework.ConformanceIt("should release old attachment ip and allocate new one when VM NAD is changed", func() {
		f.SkipVersionPriorTo(1, 16, "This feature was introduced in v1.16.")
		providerA := fmt.Sprintf("%s.%s.ovn", nadNameA, namespaceName)
		providerB := fmt.Sprintf("%s.%s.ovn", nadNameB, namespaceName)

		ginkgo.By("Creating subnets")
		cidrA := framework.RandomCIDR(f.ClusterIPFamily)
		subnetA := framework.MakeSubnet(subnetNameA, "", cidrA, "", "", "", nil, nil, nil)
		subnetA.Spec.Provider = providerA
		_ = subnetClient.CreateSync(subnetA)

		cidrB := framework.RandomCIDR(f.ClusterIPFamily)
		subnetB := framework.MakeSubnet(subnetNameB, "", cidrB, "", "", "", nil, nil, nil)
		subnetB.Spec.Provider = providerB
		_ = subnetClient.CreateSync(subnetB)

		ginkgo.By("Creating NADs")
		nadA := framework.MakeOVNNetworkAttachmentDefinition(nadNameA, namespaceName, providerA, nil)
		_ = nadClient.Create(nadA)
		nadB := framework.MakeOVNNetworkAttachmentDefinition(nadNameB, namespaceName, providerB, nil)
		_ = nadClient.Create(nadB)

		ginkgo.By("Creating vm " + vmName + " with multus network " + nadNameA)
		vm := framework.MakeVMWithMultusNetwork(vmName, image, "small", ptr.To(v1.RunStrategyAlways), nadNameA)
		_ = vmClient.CreateSync(vm)

		ginkgo.By("Getting pod of vm " + vmName)
		labelSelector := fmt.Sprintf("%s=%s", v1.VirtualMachineInstanceIDLabel, vmName)
		podList, err := podClient.List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(podList.Items, 1)

		ginkgo.By("Recording IPs")
		pod := &podList.Items[0]
		primaryIPs := pod.Status.PodIPs

		attachPortNameA := ovs.PodNameToPortName(vmName, namespaceName, providerA)
		attachIPA := ipClient.Get(attachPortNameA)
		framework.ExpectNotEmpty(attachIPA.Spec.IPAddress)

		ginkgo.By("Stopping vm " + vmName)
		vmClient.StopSync(vmName)

		ginkgo.By("Patching vm " + vmName + " to switch NAD from " + nadNameA + " to " + nadNameB)
		patchData, err := json.Marshal([]map[string]any{
			{
				"op":    "replace",
				"path":  "/spec/template/spec/networks/1/multus/networkName",
				"value": nadNameB,
			},
		})
		framework.ExpectNoError(err)
		vmClient.Patch(vmName, types.JSONPatchType, patchData)

		ginkgo.By("Starting vm " + vmName)
		vmClient.StartSync(vmName)

		ginkgo.By("Verifying old attachment IP is released")
		err = ipClient.WaitToDisappear(attachPortNameA, time.Second, 2*time.Minute)
		framework.ExpectNoError(err)

		ginkgo.By("Verifying new attachment IP is allocated")
		attachPortNameB := ovs.PodNameToPortName(vmName, namespaceName, providerB)
		framework.ExpectTrue(ipClient.WaitToBeReady(attachPortNameB, 2*time.Minute))

		ginkgo.By("Verifying primary network IP is preserved")
		podList, err = podClient.List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(podList.Items, 1)
		framework.ExpectNotEmpty(podList.Items[0].Status.PodIPs)
		framework.ExpectEqual(primaryIPs, podList.Items[0].Status.PodIPs)
	})
})
