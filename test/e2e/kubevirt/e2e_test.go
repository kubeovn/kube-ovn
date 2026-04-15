package kubevirt

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	"k8s.io/utils/ptr"
	v1 "kubevirt.io/api/core/v1"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var image = "quay.io/kubevirt/cirros-container-disk-demo:v1.8.1"

func getVMPod(podClient *framework.PodClient, vmName string) *corev1.Pod {
	ginkgo.GinkgoHelper()
	labelSelector := fmt.Sprintf("%s=%s", v1.VirtualMachineInstanceIDLabel, vmName)
	var result *corev1.Pod
	gomega.Eventually(func(g gomega.Gomega) {
		podList, err := podClient.List(context.TODO(), metav1.ListOptions{
			LabelSelector: labelSelector,
			FieldSelector: "status.phase=Running",
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(podList.Items).To(gomega.HaveLen(1),
			"expected exactly 1 running pod for vm %s, got %d", vmName, len(podList.Items))
		result = &podList.Items[0]
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(gomega.Succeed())
	return result
}

func expectVMAnnotations(pod *corev1.Pod, vmName string) {
	ginkgo.GinkgoHelper()
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")
	framework.ExpectHaveKeyWithValue(pod.Annotations, util.VMAnnotation, vmName)
}

func expectLSPMigrationCleanup(portName string) {
	ginkgo.GinkgoHelper()
	cmd := "ovn-nbctl --format=csv --data=bare --no-heading --columns=options list Logical_Switch_Port " + portName
	output, _, err := framework.NBExec(cmd)
	framework.ExpectNoError(err)
	outputStr := string(output)
	framework.ExpectNotContainSubstring(outputStr, "activation-strategy")
	if strings.Contains(outputStr, "requested-chassis") {
		scanner := bufio.NewScanner(strings.NewReader(outputStr))
		scanner.Split(bufio.ScanWords)
		for scanner.Scan() {
			if chassisValue, ok := strings.CutPrefix(scanner.Text(), "requested-chassis="); ok {
				framework.ExpectNotContainSubstring(chassisValue, ",")
			}
		}
		if err := scanner.Err(); err != nil {
			framework.ExpectNoError(err)
		}
	}
}

func parsePingStats(stdout string) (transmitted, received, lost int) {
	ginkgo.GinkgoHelper()
	re := regexp.MustCompile(`(\d+) packets transmitted, (\d+) packets received`)
	matches := re.FindStringSubmatch(stdout)
	framework.ExpectNotEmpty(matches, "failed to parse ping statistics from output")
	var err error
	transmitted, err = strconv.Atoi(matches[1])
	framework.ExpectNoError(err)
	received, err = strconv.Atoi(matches[2])
	framework.ExpectNoError(err)
	lost = transmitted - received
	return transmitted, received, lost
}

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
		f.SkipVersionPriorTo(1, 14, "This feature was introduced in v1.14.")

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
		pod := getVMPod(podClient, vmName)

		ginkgo.By("Validating pod annotations")
		expectVMAnnotations(pod, vmName)
		ips := pod.Status.PodIPs

		ginkgo.By("Deleting pod " + pod.Name)
		podClient.DeleteSync(pod.Name)

		ginkgo.By("Waiting for vm " + vmName + " to be ready")
		err := vmClient.WaitToBeReady(vmName, 2*time.Minute)
		framework.ExpectNoError(err)

		ginkgo.By("Getting pod of vm " + vmName)
		pod = getVMPod(podClient, vmName)

		ginkgo.By("Validating new pod annotations")
		expectVMAnnotations(pod, vmName)

		ginkgo.By("Checking whether pod ips are changed")
		framework.ExpectEqual(ips, pod.Status.PodIPs)
	})

	framework.ConformanceIt("should be able to keep pod ips after the vm is restarted", func() {
		ginkgo.By("Getting pod of vm " + vmName)
		pod := getVMPod(podClient, vmName)

		ginkgo.By("Validating pod annotations")
		expectVMAnnotations(pod, vmName)
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
		pod = getVMPod(podClient, vmName)

		ginkgo.By("Validating new pod annotations")
		expectVMAnnotations(pod, vmName)

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
		pod := getVMPod(podClient, vmName)

		ginkgo.By("Validating pod annotations")
		expectVMAnnotations(pod, vmName)
		ips := pod.Status.PodIPs

		ginkgo.By("Stopping vm " + vmName)
		vmClient.StopSync(vmName)

		// the ip is deleted
		portName := ovs.PodNameToPortName(vmName, namespaceName, util.OvnProvider)
		err := ipClient.WaitToDisappear(portName, time.Second, 2*time.Minute)
		framework.ExpectNoError(err)

		ginkgo.By("Starting vm " + vmName)
		vmClient.StartSync(vmName)

		ginkgo.By("Getting pod of vm " + vmName)
		pod = getVMPod(podClient, vmName)

		ginkgo.By("Validating new pod annotations")
		expectVMAnnotations(pod, vmName)

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
		pod := getVMPod(podClient, vmName)

		ginkgo.By("Validating pod annotations")
		expectVMAnnotations(pod, vmName)
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
		pod = getVMPod(podClient, vmName)

		ginkgo.By("Validating new pod annotations")
		expectVMAnnotations(pod, vmName)

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
		pod := getVMPod(podClient, vmName)

		ginkgo.By("Validating pod annotations")
		expectVMAnnotations(pod, vmName)
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
		pod = getVMPod(podClient, vmName)

		ginkgo.By("Validating new pod annotations")
		expectVMAnnotations(pod, vmName)
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
		pod := getVMPod(podClient, vmName)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")

		ginkgo.By("Checking attachment IP exists")
		attachPortName := ovs.PodNameToPortName(vmName, namespaceName, providerA)
		attachIP := ipClient.Get(attachPortName)
		framework.ExpectNotEmpty(attachIP.Spec.IPAddress)
		oldAttachIPAddr := attachIP.Spec.IPAddress

		ginkgo.By("Deleting pod " + pod.Name)
		podClient.DeleteSync(pod.Name)

		ginkgo.By("Waiting for vm " + vmName + " to be ready")
		err := vmClient.WaitToBeReady(vmName, 2*time.Minute)
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
		pod := getVMPod(podClient, vmName)

		ginkgo.By("Recording IPs")
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
		pod = getVMPod(podClient, vmName)
		framework.ExpectNotEmpty(pod.Status.PodIPs)
		framework.ExpectEqual(primaryIPs, pod.Status.PodIPs)
	})
})

var _ = framework.Describe("[group:kubevirt]", func() {
	f := framework.NewDefaultFramework("kubevirt-migrations")

	var vmName, namespaceName string
	var podClient *framework.PodClient
	var vmClient *framework.VMClient
	var ipClient *framework.IPClient
	var migrationClient *framework.VMIMigrationClient

	ginkgo.BeforeEach(func() {
		f.SkipVersionPriorTo(1, 14, "Live migration e2e tests require v1.14 or later.")

		nodes, err := e2enode.GetReadySchedulableNodes(context.TODO(), f.ClientSet)
		framework.ExpectNoError(err)
		if len(nodes.Items) < 2 {
			ginkgo.Skip("live migration requires at least 2 schedulable nodes")
		}

		namespaceName = f.Namespace.Name
		vmName = "vm-" + framework.RandomSuffix()
		podClient = f.PodClientNS(namespaceName)
		vmClient = f.VMClientNS(namespaceName)
		ipClient = f.IPClient()
		migrationClient = f.VMIMigrationClientNS(namespaceName)
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting vm " + vmName)
		vmClient.DeleteSync(vmName)

		portName := ovs.PodNameToPortName(vmName, namespaceName, util.OvnProvider)
		ginkgo.By("Waiting for IP " + portName + " to be cleaned up")
		err := ipClient.WaitToDisappear(portName, time.Second, 2*time.Minute)
		framework.ExpectNoError(err)
	})

	ginkgo.Context("with bridge interface", func() {
		ginkgo.BeforeEach(func() {
			ginkgo.By("Creating live-migratable bridge vm " + vmName)
			vm := framework.MakeVMLiveMigratableBridge(vmName, image, "small")
			_ = vmClient.CreateSync(vm)
		})

		framework.ConformanceIt("should keep pod ip and mac unchanged after live migration", func() {
			ginkgo.By("Getting pod of vm " + vmName)
			pod := getVMPod(podClient, vmName)
			origNode := pod.Spec.NodeName

			ginkgo.By("Validating pod annotations")
			expectVMAnnotations(pod, vmName)
			origIPs := pod.Status.PodIPs
			origMAC := pod.Annotations[util.MacAddressAnnotation]
			framework.ExpectMAC(origMAC)

			migrationName := "mig-" + framework.RandomSuffix()
			ginkgo.By("Creating migration " + migrationName + " for vm " + vmName)
			migration := framework.MakeVMIMigration(migrationName, vmName)
			migration = migrationClient.Create(migration)

			ginkgo.By("Waiting for migration to succeed")
			err := migrationClient.WaitForPhase(migration.Name, v1.MigrationSucceeded, 5*time.Minute)
			framework.ExpectNoError(err)

			ginkgo.By("Waiting for vm " + vmName + " to be ready")
			err = vmClient.WaitToBeReady(vmName, 2*time.Minute)
			framework.ExpectNoError(err)

			ginkgo.By("Getting pod of vm " + vmName + " after migration")
			pod = getVMPod(podClient, vmName)

			ginkgo.By("Checking that the vm moved to a different node")
			framework.ExpectNotEqual(pod.Spec.NodeName, origNode)

			ginkgo.By("Validating pod annotations after migration")
			expectVMAnnotations(pod, vmName)

			ginkgo.By("Checking that IP and MAC are unchanged")
			framework.ExpectEqual(pod.Status.PodIPs, origIPs)
			framework.ExpectEqual(pod.Annotations[util.MacAddressAnnotation], origMAC)
		})

		framework.ConformanceIt("should clean up OVN LSP migrate options after successful migration", func() {
			portName := ovs.PodNameToPortName(vmName, namespaceName, util.OvnProvider)

			migrationName := "mig-" + framework.RandomSuffix()
			ginkgo.By("Creating migration " + migrationName + " for vm " + vmName)
			migration := framework.MakeVMIMigration(migrationName, vmName)
			migration = migrationClient.Create(migration)

			ginkgo.By("Waiting for migration to succeed")
			err := migrationClient.WaitForPhase(migration.Name, v1.MigrationSucceeded, 5*time.Minute)
			framework.ExpectNoError(err)

			ginkgo.By("Waiting for vm " + vmName + " to be ready")
			err = vmClient.WaitToBeReady(vmName, 2*time.Minute)
			framework.ExpectNoError(err)

			ginkgo.By("Checking OVN LSP options for " + portName)
			expectLSPMigrationCleanup(portName)
		})

		framework.ConformanceIt("should preserve IP CRD resource through live migration", func() {
			portName := ovs.PodNameToPortName(vmName, namespaceName, util.OvnProvider)

			ginkgo.By("Getting pod of vm " + vmName)
			pod := getVMPod(podClient, vmName)
			origNode := pod.Spec.NodeName

			ginkgo.By("Getting IP CRD " + portName + " before migration")
			origIP := ipClient.Get(portName)
			framework.ExpectNotEmpty(origIP.Spec.IPAddress)
			origUID := origIP.UID
			framework.ExpectEqual(origIP.Spec.NodeName, origNode)

			migrationName := "mig-" + framework.RandomSuffix()
			ginkgo.By("Creating migration " + migrationName + " for vm " + vmName)
			migration := framework.MakeVMIMigration(migrationName, vmName)
			migration = migrationClient.Create(migration)

			ginkgo.By("Waiting for migration to succeed")
			err := migrationClient.WaitForPhase(migration.Name, v1.MigrationSucceeded, 5*time.Minute)
			framework.ExpectNoError(err)

			ginkgo.By("Waiting for vm " + vmName + " to be ready")
			err = vmClient.WaitToBeReady(vmName, 2*time.Minute)
			framework.ExpectNoError(err)

			ginkgo.By("Getting pod of vm " + vmName + " after migration")
			pod = getVMPod(podClient, vmName)
			newNode := pod.Spec.NodeName
			framework.ExpectNotEqual(newNode, origNode)

			ginkgo.By("Getting IP CRD " + portName + " after migration")
			gomega.Eventually(func(g gomega.Gomega) {
				newIP := ipClient.Get(portName)
				g.Expect(string(newIP.UID)).To(gomega.Equal(string(origUID)), "IP CRD should not be recreated")
				g.Expect(newIP.Spec.IPAddress).To(gomega.Equal(origIP.Spec.IPAddress))
				g.Expect(newIP.Spec.MacAddress).To(gomega.Equal(origIP.Spec.MacAddress))
				g.Expect(newIP.Spec.Subnet).To(gomega.Equal(origIP.Spec.Subnet))
				g.Expect(newIP.Spec.PodName).To(gomega.Equal(origIP.Spec.PodName))
				g.Expect(newIP.Spec.NodeName).To(gomega.Equal(newNode),
					"IP CRD NodeName should be updated to %s", newNode)
			}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(gomega.Succeed())
		})

		framework.ConformanceIt("should preserve network identity when migration is aborted", func() {
			ginkgo.By("Getting pod of vm " + vmName)
			pod := getVMPod(podClient, vmName)
			origIPs := pod.Status.PodIPs
			origMAC := pod.Annotations[util.MacAddressAnnotation]
			framework.ExpectMAC(origMAC)

			migrationName := "mig-" + framework.RandomSuffix()
			ginkgo.By("Creating migration " + migrationName + " for vm " + vmName)
			migration := framework.MakeVMIMigration(migrationName, vmName)
			migration = migrationClient.Create(migration)

			ginkgo.By("Aborting migration " + migrationName)
			migrationClient.Delete(migration.Name)

			ginkgo.By("Getting pod of vm " + vmName + " after canceled migration")
			pod = getVMPod(podClient, vmName)

			ginkgo.By("Checking that IP and MAC are unchanged regardless of migration outcome")
			framework.ExpectEqual(pod.Status.PodIPs, origIPs)
			framework.ExpectEqual(pod.Annotations[util.MacAddressAnnotation], origMAC)

			portName := ovs.PodNameToPortName(vmName, namespaceName, util.OvnProvider)
			ginkgo.By("Checking OVN LSP options for " + portName)
			expectLSPMigrationCleanup(portName)
		})

		framework.ConformanceIt("should maintain network connectivity through multiple sequential migrations", func() {
			ginkgo.By("Getting pod of vm " + vmName)
			pod := getVMPod(podClient, vmName)
			vmIP := pod.Status.PodIPs[0].IP
			origIPs := pod.Status.PodIPs
			origMAC := pod.Annotations[util.MacAddressAnnotation]
			framework.ExpectMAC(origMAC)
			framework.Logf("VM pod IP: %s, node: %s", vmIP, pod.Spec.NodeName)

			portName := ovs.PodNameToPortName(vmName, namespaceName, util.OvnProvider)
			origIP := ipClient.Get(portName)
			origUID := origIP.UID

			proberName := "prober-" + framework.RandomSuffix()
			ginkgo.By("Creating prober pod " + proberName)
			proberPod := framework.MakePod(namespaceName, proberName, nil, nil, framework.AgnhostImage, nil, []string{"pause"})
			_ = podClient.CreateSync(proberPod)
			ginkgo.DeferCleanup(func() {
				ginkgo.By("Deleting prober pod " + proberName)
				podClient.DeleteSync(proberName)
			})

			var stdout string
			var err error
			ginkgo.By("Verifying initial connectivity from prober to VM")
			gomega.Eventually(func() error {
				stdout, _, err = framework.ExecShellInPod(context.TODO(), f, namespaceName, proberName, fmt.Sprintf("ping -c 3 -W 2 %s", vmIP))
				return err
			}).WithTimeout(60*time.Second).WithPolling(5*time.Second).Should(gomega.Succeed(), "initial connectivity check failed")
			framework.Logf("Initial ping output:\n%s", stdout)

			maxAcceptableLoss := 5

			const migrationCount = 3
			for i := 1; i <= migrationCount; i++ {
				prevNode := getVMPod(podClient, vmName).Spec.NodeName
				migrationName := fmt.Sprintf("mig-%d-%s", i, framework.RandomSuffix())
				ginkgo.By(fmt.Sprintf("[migration %d/%d] Creating migration %s (non-blocking)", i, migrationCount, migrationName))
				migration := framework.MakeVMIMigration(migrationName, vmName)
				_ = migrationClient.Create(migration)

				ginkgo.By(fmt.Sprintf("[migration %d/%d] Running continuous ping during migration", i, migrationCount))
				pingCmd := fmt.Sprintf("ping -c 400 -i 0.1 -w 60 %s 2>&1 || true", vmIP)
				stdout, _, err = framework.ExecShellInPod(context.TODO(), f, namespaceName, proberName, pingCmd)
				framework.ExpectNoError(err)

				transmitted, received, lost := parsePingStats(stdout)
				framework.Logf("[migration %d/%d] Ping: %d transmitted, %d received, %d lost", i, migrationCount, transmitted, received, lost)

				ginkgo.By(fmt.Sprintf("[migration %d/%d] Verifying migration succeeded", i, migrationCount))
				err = migrationClient.WaitForPhase(migrationName, v1.MigrationSucceeded, 5*time.Minute)
				framework.ExpectNoError(err)

				ginkgo.By(fmt.Sprintf("[migration %d/%d] Asserting packet loss (%d) within threshold (%d)", i, migrationCount, lost, maxAcceptableLoss))
				gomega.Expect(lost).To(gomega.BeNumerically("<=", maxAcceptableLoss),
					"migration %d/%d: expected at most %d lost packets, but lost %d out of %d", i, migrationCount, maxAcceptableLoss, lost, transmitted)

				ginkgo.By(fmt.Sprintf("[migration %d/%d] Waiting for vm to be ready", i, migrationCount))
				err = vmClient.WaitToBeReady(vmName, 2*time.Minute)
				framework.ExpectNoError(err)

				pod = getVMPod(podClient, vmName)
				ginkgo.By(fmt.Sprintf("[migration %d/%d] Checking node changed from %s to %s", i, migrationCount, prevNode, pod.Spec.NodeName))
				framework.ExpectNotEqual(pod.Spec.NodeName, prevNode)

				ginkgo.By(fmt.Sprintf("[migration %d/%d] Checking IP and MAC unchanged", i, migrationCount))
				framework.ExpectEqual(pod.Status.PodIPs, origIPs)
				framework.ExpectEqual(pod.Annotations[util.MacAddressAnnotation], origMAC)

				ginkgo.By(fmt.Sprintf("[migration %d/%d] Checking annotations", i, migrationCount))
				expectVMAnnotations(pod, vmName)

				ginkgo.By(fmt.Sprintf("[migration %d/%d] Checking IP CRD preserved", i, migrationCount))
				gomega.Eventually(func(g gomega.Gomega) {
					currentIP := ipClient.Get(portName)
					g.Expect(string(currentIP.UID)).To(gomega.Equal(string(origUID)))
					g.Expect(currentIP.Spec.IPAddress).To(gomega.Equal(origIP.Spec.IPAddress))
					g.Expect(currentIP.Spec.MacAddress).To(gomega.Equal(origIP.Spec.MacAddress))
					g.Expect(currentIP.Spec.NodeName).To(gomega.Equal(pod.Spec.NodeName),
						"IP CRD NodeName should be updated to %s", pod.Spec.NodeName)
				}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(gomega.Succeed())

				ginkgo.By(fmt.Sprintf("[migration %d/%d] Checking OVN LSP cleanup", i, migrationCount))
				expectLSPMigrationCleanup(portName)

				framework.Logf("[migration %d/%d] PASSED — node: %s, IP: %v, MAC: %s, lost packets: %d", i, migrationCount, pod.Spec.NodeName, pod.Status.PodIPs, origMAC, lost)
			}

			ginkgo.By("Verifying post-migration connectivity")
			stdout, _, err = framework.ExecShellInPod(context.TODO(), f, namespaceName, proberName, fmt.Sprintf("ping -c 3 -W 2 %s", vmIP))
			framework.ExpectNoError(err, "post-migration connectivity check failed")
			framework.Logf("Post-migration ping output:\n%s", stdout)
		})
	})

	ginkgo.Context("with masquerade interface", func() {
		ginkgo.BeforeEach(func() {
			ginkgo.By("Creating live-migratable masquerade vm " + vmName)
			vm := framework.MakeVMLiveMigratable(vmName, image, "small")
			_ = vmClient.CreateSync(vm)
		})

		framework.ConformanceIt("should maintain network connectivity during live migration with masquerade interface", func() {
			ginkgo.By("Getting pod of vm " + vmName)
			pod := getVMPod(podClient, vmName)
			vmIP := pod.Status.PodIPs[0].IP
			framework.Logf("VM pod IP: %s, node: %s", vmIP, pod.Spec.NodeName)

			proberName := "prober-" + framework.RandomSuffix()
			ginkgo.By("Creating prober pod " + proberName)
			proberPod := framework.MakePod(namespaceName, proberName, nil, nil, framework.AgnhostImage, nil, []string{"pause"})
			_ = podClient.CreateSync(proberPod)
			ginkgo.DeferCleanup(func() {
				ginkgo.By("Deleting prober pod " + proberName)
				podClient.DeleteSync(proberName)
			})

			var stdout string
			var err error
			ginkgo.By("Verifying initial connectivity from prober to VM")
			gomega.Eventually(func() error {
				stdout, _, err = framework.ExecShellInPod(context.TODO(), f, namespaceName, proberName, fmt.Sprintf("ping -c 3 -W 2 %s", vmIP))
				return err
			}).WithTimeout(60*time.Second).WithPolling(5*time.Second).Should(gomega.Succeed(), "initial connectivity check failed")
			framework.Logf("Initial ping output:\n%s", stdout)

			migrationName := "mig-" + framework.RandomSuffix()
			ginkgo.By("Creating migration " + migrationName + " for vm " + vmName + " (non-blocking)")
			migration := framework.MakeVMIMigration(migrationName, vmName)
			_ = migrationClient.Create(migration)

			ginkgo.By("Running continuous ping from prober to VM " + vmIP + " during migration")
			pingCmd := fmt.Sprintf("ping -c 400 -i 0.1 -w 60 %s 2>&1 || true", vmIP)
			stdout, _, err = framework.ExecShellInPod(context.TODO(), f, namespaceName, proberName, pingCmd)
			framework.ExpectNoError(err)
			framework.Logf("Continuous ping output:\n%s", stdout)

			ginkgo.By("Parsing ping statistics for packet loss")
			transmitted, received, lost := parsePingStats(stdout)
			framework.Logf("Ping results: %d transmitted, %d received, %d lost", transmitted, received, lost)

			ginkgo.By("Verifying migration succeeded")
			err = migrationClient.WaitForPhase(migrationName, v1.MigrationSucceeded, 5*time.Minute)
			framework.ExpectNoError(err)

			maxAcceptableLoss := 5
			ginkgo.By(fmt.Sprintf("Asserting packet loss (%d) is within acceptable threshold (%d)", lost, maxAcceptableLoss))
			gomega.Expect(lost).To(gomega.BeNumerically("<=", maxAcceptableLoss),
				"expected at most %d lost packets (0.5s downtime at 0.1s interval), but lost %d out of %d", maxAcceptableLoss, lost, transmitted)

			ginkgo.By("Verifying post-migration connectivity")
			stdout, _, err = framework.ExecShellInPod(context.TODO(), f, namespaceName, proberName, fmt.Sprintf("ping -c 3 -W 2 %s", vmIP))
			framework.ExpectNoError(err, "post-migration connectivity check failed")
			framework.Logf("Post-migration ping output:\n%s", stdout)
		})
	})
})

var _ = framework.Describe("[group:kubevirt]", func() {
	f := framework.NewDefaultFramework("kubevirt-multus-migrations")

	var vmName, namespaceName, subnetName, nadName string
	var podClient *framework.PodClient
	var vmClient *framework.VMClient
	var ipClient *framework.IPClient
	var subnetClient *framework.SubnetClient
	var nadClient *framework.NetworkAttachmentDefinitionClient
	var migrationClient *framework.VMIMigrationClient

	ginkgo.BeforeEach(func() {
		f.SkipVersionPriorTo(1, 14, "Multus live migration e2e tests require v1.14 or later.")

		nodes, err := e2enode.GetReadySchedulableNodes(context.TODO(), f.ClientSet)
		framework.ExpectNoError(err)
		if len(nodes.Items) < 2 {
			ginkgo.Skip("live migration requires at least 2 schedulable nodes")
		}

		namespaceName = f.Namespace.Name

		vmName = "vm-" + framework.RandomSuffix()
		subnetName = "subnet-" + framework.RandomSuffix()
		nadName = "nad-" + framework.RandomSuffix()
		podClient = f.PodClientNS(namespaceName)
		vmClient = f.VMClientNS(namespaceName)
		ipClient = f.IPClient()
		subnetClient = f.SubnetClient()
		nadClient = f.NetworkAttachmentDefinitionClientNS(namespaceName)
		migrationClient = f.VMIMigrationClientNS(namespaceName)
	})

	ginkgo.AfterEach(func() {
		if vmName == "" {
			return
		}

		ginkgo.By("Deleting vm " + vmName)
		vmClient.DeleteSync(vmName)

		portName := ovs.PodNameToPortName(vmName, namespaceName, util.OvnProvider)
		ginkgo.By("Waiting for IP " + portName + " to be cleaned up")
		err := ipClient.WaitToDisappear(portName, time.Second, 2*time.Minute)
		framework.ExpectNoError(err)

		ginkgo.By("Deleting NAD " + nadName)
		nadClient.Delete(nadName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
	})

	framework.ConformanceIt("should preserve all NICs through live migration of a multi-NIC vm", func() {
		provider := fmt.Sprintf("%s.%s.%s", nadName, namespaceName, util.OvnProvider)

		ginkgo.By("Creating secondary subnet " + subnetName)
		cidr := framework.RandomCIDR(f.ClusterIPFamily)
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", provider, nil, nil, []string{namespaceName})
		_ = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating NAD " + nadName)
		nad := framework.MakeOVNNetworkAttachmentDefinition(nadName, namespaceName, provider, nil)
		_ = nadClient.Create(nad)

		ginkgo.By("Creating multi-NIC live-migratable vm " + vmName)
		nadFullName := fmt.Sprintf("%s/%s", namespaceName, nadName)
		vm := framework.MakeVMLiveMigratableMultiNIC(vmName, image, "small", nadFullName)
		_ = vmClient.CreateSync(vm)

		ginkgo.By("Getting pod of vm " + vmName)
		pod := getVMPod(podClient, vmName)
		origNode := pod.Spec.NodeName

		ginkgo.By("Checking default NIC annotations")
		origIPs := pod.Status.PodIPs
		origMAC := pod.Annotations[util.MacAddressAnnotation]
		framework.ExpectMAC(origMAC)

		ginkgo.By("Checking secondary NIC annotations")
		secondaryIPKey := fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)
		secondaryMACKey := fmt.Sprintf(util.MacAddressAnnotationTemplate, provider)
		origSecondaryIP := pod.Annotations[secondaryIPKey]
		origSecondaryMAC := pod.Annotations[secondaryMACKey]
		framework.ExpectNotEmpty(origSecondaryIP, "secondary IP should be allocated")
		framework.ExpectMAC(origSecondaryMAC)
		framework.Logf("Before migration — node: %s, default IP: %v, MAC: %s, secondary IP: %s, MAC: %s",
			origNode, origIPs, origMAC, origSecondaryIP, origSecondaryMAC)

		migrationName := "mig-" + framework.RandomSuffix()
		ginkgo.By("Creating migration " + migrationName + " for vm " + vmName)
		migration := framework.MakeVMIMigration(migrationName, vmName)
		migration = migrationClient.Create(migration)

		ginkgo.By("Waiting for migration to succeed")
		err := migrationClient.WaitForPhase(migration.Name, v1.MigrationSucceeded, 5*time.Minute)
		framework.ExpectNoError(err)

		ginkgo.By("Waiting for vm " + vmName + " to be ready")
		err = vmClient.WaitToBeReady(vmName, 2*time.Minute)
		framework.ExpectNoError(err)

		ginkgo.By("Getting pod of vm " + vmName + " after migration")
		pod = getVMPod(podClient, vmName)
		framework.Logf("After migration — node: %s", pod.Spec.NodeName)

		ginkgo.By("Checking that the vm moved to a different node")
		framework.ExpectNotEqual(pod.Spec.NodeName, origNode)

		ginkgo.By("Checking default NIC IP and MAC are unchanged")
		framework.ExpectEqual(pod.Status.PodIPs, origIPs)
		framework.ExpectEqual(pod.Annotations[util.MacAddressAnnotation], origMAC)

		ginkgo.By("Checking secondary NIC IP and MAC are unchanged")
		framework.ExpectEqual(pod.Annotations[secondaryIPKey], origSecondaryIP)
		framework.ExpectEqual(pod.Annotations[secondaryMACKey], origSecondaryMAC)

		ginkgo.By("Checking default NIC OVN LSP cleanup")
		portName := ovs.PodNameToPortName(vmName, namespaceName, util.OvnProvider)
		expectLSPMigrationCleanup(portName)

		ginkgo.By("Checking secondary NIC OVN LSP cleanup")
		secondaryPortName := ovs.PodNameToPortName(vmName, namespaceName, provider)
		expectLSPMigrationCleanup(secondaryPortName)

		framework.Logf("After migration — default IP: %v, MAC: %s, secondary IP: %s, MAC: %s",
			pod.Status.PodIPs, pod.Annotations[util.MacAddressAnnotation],
			pod.Annotations[secondaryIPKey], pod.Annotations[secondaryMACKey])
	})
})
