package external_endpoint

import (
	"context"
	"flag"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
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

func TestE2E(t *testing.T) {
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)
	e2e.RunE2ETests(t)
}

// endpointNIC stands in for the NIC of a VM on a hypervisor: an OVS internal port
// in its own network namespace, bound to the endpoint's logical switch port through
// external_ids:iface-id, exactly what libvirt does for <virtualport type='openvswitch'>.
// The port is created through the node's ovs-ovn pod and the network namespace on the
// kind node itself, which runs as root.
type endpointNIC struct {
	node   kind.Node
	ovsPod *corev1.Pod
	netns  string
	dev    string
}

func (n *endpointNIC) nodeExec(cmd string) error {
	_, stderr, err := n.node.Exec("sh", "-c", cmd)
	if err != nil {
		return fmt.Errorf("%q on node %s: %w: %s", cmd, n.node.Name(), err, stderr)
	}
	return nil
}

func plugEndpointNIC(f *framework.Framework, node *corev1.Node, ep *apiv1.ExternalEndpoint, subnet *apiv1.Subnet) *endpointNIC {
	ginkgo.GinkgoHelper()

	cluster, ok := kind.IsKindProvided(node.Spec.ProviderID)
	if !ok {
		ginkgo.Skip("plugging an endpoint NIC requires a kind cluster")
	}
	kindNodes, err := kind.ListNodes(cluster, "")
	framework.ExpectNoError(err)
	idx := slices.IndexFunc(kindNodes, func(n kind.Node) bool { return n.Name() == node.Name })
	framework.ExpectNotEqual(idx, -1, "kind node %s not found", node.Name)

	daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
	ds := daemonSetClient.Get("ovs-ovn")
	ovsPod, err := daemonSetClient.GetPodOnNode(ds, node.Name)
	framework.ExpectNoError(err)

	suffix := framework.RandomSuffix()
	nic := &endpointNIC{node: kindNodes[idx], ovsPod: ovsPod, netns: "eep-" + suffix, dev: "eep" + suffix}
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Unplugging the endpoint NIC " + nic.dev + " on node " + node.Name)
		_, _ = e2epodoutput.RunHostCmd(ovsPod.Namespace, ovsPod.Name, "ovs-vsctl --if-exists del-port br-int "+nic.dev)
		_ = nic.nodeExec("ip netns del " + nic.netns)
	})

	ginkgo.By("Plugging an OVS internal port with iface-id " + ep.Status.InterfaceID + " on node " + node.Name)
	framework.ExpectNoError(nic.nodeExec("ip netns add " + nic.netns))
	_, err = e2epodoutput.RunHostCmd(ovsPod.Namespace, ovsPod.Name,
		fmt.Sprintf("ovs-vsctl add-port br-int %s -- set interface %s type=internal external_ids:iface-id=%s", nic.dev, nic.dev, ep.Status.InterfaceID))
	framework.ExpectNoError(err)
	cmds := []string{
		fmt.Sprintf("ip link set %s netns %s", nic.dev, nic.netns),
		fmt.Sprintf("ip netns exec %s ip link set %s address %s mtu 1300 up", nic.netns, nic.dev, ep.Status.MacAddress),
	}
	for ip := range strings.SplitSeq(util.GetStringIP(ep.Status.V4IP, ep.Status.V6IP), ",") {
		cmds = append(cmds, fmt.Sprintf("ip netns exec %s ip addr add %s/%s dev %s nodad", nic.netns, ip, prefixLength(subnet.Spec.CIDRBlock, ip), nic.dev))
	}
	for _, cmd := range cmds {
		framework.ExpectNoError(nic.nodeExec(cmd))
	}
	return nic
}

// prefixLength returns the prefix length of the subnet cidr of the family of ip.
func prefixLength(cidrBlock, ip string) string {
	for cidr := range strings.SplitSeq(cidrBlock, ",") {
		if util.CheckProtocol(cidr) == util.CheckProtocol(ip) {
			return strings.Split(cidr, "/")[1]
		}
	}
	return ""
}

func pingCmd(ip string) string {
	if util.CheckProtocol(ip) == apiv1.ProtocolIPv6 {
		return "ping -6 -c 3 -W 2 " + ip
	}
	return "ping -c 3 -W 2 " + ip
}

func lspField(name, column string) string {
	ginkgo.GinkgoHelper()
	stdout, _, err := framework.NBExec(fmt.Sprintf("ovn-nbctl --data=bare --no-heading --columns=%s find Logical_Switch_Port name=%s", column, name))
	framework.ExpectNoError(err)
	return strings.TrimSpace(string(stdout))
}

func epCondition(ep *apiv1.ExternalEndpoint, condType string) *metav1.Condition {
	return meta.FindStatusCondition(ep.Status.Conditions, condType)
}

var _ = framework.SerialDescribe("[group:external-endpoint]", func() {
	f := framework.NewDefaultFramework("external-endpoint")

	var vpcClient *framework.VpcClient
	var subnetClient *framework.SubnetClient
	var podClient *framework.PodClient
	var epClient *framework.ExternalEndpointClient
	var namespaceName, vpcName, subnetName string
	var subnet *apiv1.Subnet
	var nodes []corev1.Node

	ginkgo.BeforeEach(func() {
		f.SkipVersionPriorTo(1, 17, "ExternalEndpoint was introduced in v1.17")
		deploy, err := f.ClientSet.AppsV1().Deployments(framework.KubeOvnNamespace).Get(context.Background(), "kube-ovn-controller", metav1.GetOptions{})
		framework.ExpectNoError(err)
		if !slices.Contains(deploy.Spec.Template.Spec.Containers[0].Args, "--enable-external-endpoint=true") {
			ginkgo.Skip("kube-ovn-controller does not run with --enable-external-endpoint=true")
		}

		namespaceName = f.Namespace.Name
		vpcClient = f.VpcClient()
		subnetClient = f.SubnetClient()
		podClient = f.PodClient()
		epClient = f.ExternalEndpointClient()

		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), f.ClientSet)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)
		nodes = nodeList.Items

		vpcName = "vpc-" + framework.RandomSuffix()
		ginkgo.By("Creating vpc " + vpcName)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting vpc " + vpcName)
			vpcClient.DeleteSync(vpcName)
		})
		vpcClient.CreateSync(framework.MakeVpc(vpcName, "", false, false, []string{namespaceName}))

		subnetName = "subnet-" + framework.RandomSuffix()
		cidr := framework.RandomCIDR(f.ClusterIPFamily)
		ginkgo.By("Creating subnet " + subnetName + " with cidr " + cidr)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting subnet " + subnetName)
			subnetClient.DeleteSync(subnetName)
		})
		s := framework.MakeSubnet(subnetName, "", cidr, "", vpcName, "", nil, nil, []string{namespaceName})
		s.Spec.EnableDHCP = true
		subnet = subnetClient.CreateSync(s)
	})

	framework.ConformanceIt("should give an external NIC the network service of a pod NIC", func() {
		chassis := nodes[0].Name
		epName := "ep-" + framework.RandomSuffix()
		ginkgo.By("Creating external endpoint " + epName + " pinned to chassis " + chassis)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting external endpoint " + epName)
			epClient.DeleteSync(epName)
		})
		ep := epClient.CreateSync(framework.MakeExternalEndpoint(namespaceName, epName, subnetName, chassis, util.GenerateMac(), ""))

		ginkgo.By("Checking the allocated address and the status")
		framework.ExpectEqual(ep.Status.InterfaceID, string(ep.UID))
		epIPs := strings.Split(util.GetStringIP(ep.Status.V4IP, ep.Status.V6IP), ",")
		for _, ip := range epIPs {
			framework.ExpectTrue(util.CIDRContainIP(subnet.Spec.CIDRBlock, ip), "%s is not in %s", ip, subnet.Spec.CIDRBlock)
		}
		framework.ExpectEqual(ep.Spec.MacAddress, ep.Status.MacAddress)
		framework.ExpectFalse(meta.IsStatusConditionTrue(ep.Status.Conditions, apiv1.ExternalEndpointConditionBound))

		ginkgo.By("Checking the IP CR of the endpoint")
		ipCR := f.IPClient().Get(ep.Status.InterfaceID)
		framework.ExpectEqual(ipCR.Spec.PodType, util.KindExternalEndpoint)
		framework.ExpectEqual(ipCR.Spec.PodName, epName)
		framework.ExpectEqual(ipCR.Spec.Namespace, namespaceName)
		framework.ExpectEqual(ipCR.Labels[util.ExternalEndpointUIDLabel], string(ep.UID))
		framework.ExpectEmpty(ipCR.OwnerReferences)

		ginkgo.By("Checking the logical switch port of the endpoint")
		addresses := strings.Join(append([]string{ep.Status.MacAddress}, epIPs...), " ")
		framework.ExpectEqual(lspField(ep.Status.InterfaceID, "addresses"), addresses)
		framework.ExpectEqual(lspField(ep.Status.InterfaceID, "port_security"), addresses)
		externalIDs := lspField(ep.Status.InterfaceID, "external_ids")
		framework.ExpectContainSubstring(externalIDs, "external_endpoint="+namespaceName+"/"+epName)
		framework.ExpectContainSubstring(externalIDs, "vendor="+util.CniTypeName)
		framework.ExpectNotContainSubstring(externalIDs, "pod=")
		framework.ExpectContainSubstring(lspField(ep.Status.InterfaceID, "options"), "requested-chassis="+chassis)
		if subnet.Status.DHCPv4OptionsUUID != "" && ep.Status.V4IP != "" {
			framework.ExpectEqual(lspField(ep.Status.InterfaceID, "dhcpv4_options"), subnet.Status.DHCPv4OptionsUUID)
		}

		ginkgo.By("Plugging the endpoint NIC in on chassis " + chassis)
		nic := plugEndpointNIC(f, &nodes[0], ep, subnet)
		ep = epClient.WaitUntil(epName, func(e *apiv1.ExternalEndpoint) (bool, error) {
			return e.Status.Up && meta.IsStatusConditionTrue(e.Status.Conditions, apiv1.ExternalEndpointConditionBound), nil
		}, "Bound", 2*time.Second, time.Minute)

		podName := "pod-" + framework.RandomSuffix()
		ginkgo.By("Creating pod " + podName + " in subnet " + subnetName)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting pod " + podName)
			podClient.DeleteSync(podName)
		})
		annotations := map[string]string{util.LogicalSwitchAnnotation: subnetName}
		pod := podClient.CreateSync(framework.MakePod(namespaceName, podName, nil, annotations, f.KubeOVNImage, []string{"sleep", "infinity"}, nil))
		podIPs := util.PodIPs(*pod)

		for _, ip := range epIPs {
			ginkgo.By("Checking pod -> endpoint " + ip)
			framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
				_, _, err := framework.ExecShellInPod(context.Background(), f, namespaceName, podName, pingCmd(ip))
				return err == nil, nil
			}, "pod reaches the endpoint")
		}
		if ep.Status.V4IP != "" {
			// the endpoint answers with its own address, which port security allows; an
			// extra address on the same NIC is not in port_security, so OVN drops the ARP
			// replies (and any IP traffic) sourced from it
			spoofed := ""
			for ip := range strings.SplitSeq(framework.RandomIPs(subnet.Spec.CIDRBlock, ",", 3), ",") {
				if util.CheckProtocol(ip) == apiv1.ProtocolIPv4 && ip != ep.Status.V4IP && !slices.Contains(podIPs, ip) {
					spoofed = ip
					break
				}
			}
			framework.ExpectNotEmpty(spoofed)
			ginkgo.By("Checking that port security drops traffic of the unassigned address " + spoofed)
			framework.ExpectNoError(nic.nodeExec(fmt.Sprintf("ip netns exec %s ip addr add %s/32 dev %s", nic.netns, spoofed, nic.dev)))
			_, _, err := framework.ExecShellInPod(context.Background(), f, namespaceName, podName, pingCmd(spoofed))
			framework.ExpectError(err, "an address that is not in port_security must not be reachable")
			_, _, err = framework.ExecShellInPod(context.Background(), f, namespaceName, podName, pingCmd(ep.Status.V4IP))
			framework.ExpectNoError(err, "the assigned address is still reachable")
		}

		ginkgo.By("Deleting the logical switch port out of band and waiting for it to be repaired")
		_, _, err := framework.NBExec("ovn-nbctl lsp-del " + ep.Status.InterfaceID)
		framework.ExpectNoError(err)
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			return lspField(ep.Status.InterfaceID, "name") == ep.Status.InterfaceID, nil
		}, "the logical switch port is recreated")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			_, _, err := framework.ExecShellInPod(context.Background(), f, namespaceName, podName, pingCmd(epIPs[0]))
			return err == nil, nil
		}, "traffic recovers after the repair")

		if len(nodes) > 1 {
			ginkgo.By("Moving the chassis pin to another node: the port must be released")
			modified := ep.DeepCopy()
			modified.Spec.Chassis = nodes[1].Name
			ep = epClient.Patch(ep, modified)
			ep = epClient.WaitUntil(epName, func(e *apiv1.ExternalEndpoint) (bool, error) {
				return !e.Status.Up, nil
			}, "unbound", 2*time.Second, time.Minute)

			ginkgo.By("Moving the chassis pin back: the port must be bound again")
			modified = ep.DeepCopy()
			modified.Spec.Chassis = chassis
			ep = epClient.Patch(ep, modified)
			ep = epClient.WaitUntil(epName, func(e *apiv1.ExternalEndpoint) (bool, error) {
				return e.Status.Up, nil
			}, "bound again", 2*time.Second, time.Minute)
		}

		ginkgo.By("Deleting the external endpoint")
		epClient.DeleteSync(epName)
		framework.ExpectEmpty(lspField(ep.Status.InterfaceID, "name"))
		_, err = f.KubeOVNClientSet.KubeovnV1().IPs().Get(context.Background(), ep.Status.InterfaceID, metav1.GetOptions{})
		framework.ExpectTrue(k8serrors.IsNotFound(err), "the IP CR must be deleted with the endpoint")
		_, _, err = framework.ExecShellInPod(context.Background(), f, namespaceName, podName, pingCmd(epIPs[0]))
		framework.ExpectError(err, "the endpoint must be unreachable after its deletion")

		ginkgo.By("Checking that the address is free again")
		reuseName := "ep-" + framework.RandomSuffix()
		ginkgo.DeferCleanup(func() {
			epClient.DeleteSync(reuseName)
		})
		reuse := epClient.CreateSync(framework.MakeExternalEndpoint(namespaceName, reuseName, subnetName, chassis, "", strings.Join(epIPs, ",")))
		framework.ExpectEqual(util.GetStringIP(reuse.Status.V4IP, reuse.Status.V6IP), strings.Join(epIPs, ","))
	})

	framework.ConformanceIt("should reject endpoints it cannot serve", func() {
		chassis := nodes[0].Name

		ginkgo.By("Rejecting an endpoint without spec.chassis in the schema")
		_, err := f.KubeOVNClientSet.KubeovnV1().ExternalEndpoints(namespaceName).Create(context.Background(),
			framework.MakeExternalEndpoint(namespaceName, "ep-"+framework.RandomSuffix(), subnetName, "", "", ""), metav1.CreateOptions{})
		framework.ExpectError(err)
		framework.ExpectContainSubstring(err.Error(), "spec.chassis")

		failsWith := func(name, reason, message string) {
			ginkgo.GinkgoHelper()
			epClient.WaitUntil(name, func(e *apiv1.ExternalEndpoint) (bool, error) {
				cond := epCondition(e, apiv1.ExternalEndpointConditionReady)
				return cond != nil && cond.Status == metav1.ConditionFalse && cond.Reason == reason && strings.Contains(cond.Message, message), nil
			}, "Ready=False/"+reason, 2*time.Second, time.Minute)
		}

		outOfRange := "ep-" + framework.RandomSuffix()
		ginkgo.By("Rejecting a static address outside of the subnet")
		ginkgo.DeferCleanup(func() { epClient.DeleteSync(outOfRange) })
		addr := "192.0.2.10"
		if f.IsIPv6() {
			addr = "2001:db8::10"
		}
		epClient.Create(framework.MakeExternalEndpoint(namespaceName, outOfRange, subnetName, chassis, "", addr))
		failsWith(outOfRange, "AcquireAddressFailed", "")
		framework.ExpectEmpty(epClient.Get(outOfRange).Status.V4IP + epClient.Get(outOfRange).Status.V6IP)

		podName := "pod-" + framework.RandomSuffix()
		ginkgo.By("Rejecting the address of an existing pod")
		ginkgo.DeferCleanup(func() { podClient.DeleteSync(podName) })
		annotations := map[string]string{util.LogicalSwitchAnnotation: subnetName}
		pod := podClient.CreateSync(framework.MakePod(namespaceName, podName, nil, annotations, f.KubeOVNImage, []string{"sleep", "infinity"}, nil))
		conflict := "ep-" + framework.RandomSuffix()
		ginkgo.DeferCleanup(func() { epClient.DeleteSync(conflict) })
		epClient.Create(framework.MakeExternalEndpoint(namespaceName, conflict, subnetName, chassis, "", strings.Join(util.PodIPs(*pod), ",")))
		failsWith(conflict, "AcquireAddressFailed", "AddressConflict")

		otherNamespace := "eep-other-" + framework.RandomSuffix()
		ginkgo.By("Rejecting an endpoint in a namespace the subnet is not bound to")
		_, err = f.ClientSet.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{Name: otherNamespace}, metav1.CreateOptions{})
		framework.ExpectNoError(err)
		ginkgo.DeferCleanup(func() {
			f.ExternalEndpointClientNS(otherNamespace).DeleteSync("intruder")
			framework.ExpectNoError(f.ClientSet.CoreV1().Namespaces().Delete(context.Background(), otherNamespace, metav1.DeleteOptions{}))
		})
		otherClient := f.ExternalEndpointClientNS(otherNamespace)
		otherClient.Create(framework.MakeExternalEndpoint(otherNamespace, "intruder", subnetName, chassis, "", ""))
		otherClient.WaitUntil("intruder", func(e *apiv1.ExternalEndpoint) (bool, error) {
			cond := epCondition(e, apiv1.ExternalEndpointConditionReady)
			return cond != nil && cond.Reason == "NamespaceNotAllowed", nil
		}, "Ready=False/NamespaceNotAllowed", 2*time.Second, time.Minute)
	})

	framework.ConformanceIt("should not finish deleting a subnet while an endpoint uses it", func() {
		epName := "ep-" + framework.RandomSuffix()
		ginkgo.By("Creating external endpoint " + epName)
		ep := epClient.CreateSync(framework.MakeExternalEndpoint(namespaceName, epName, subnetName, nodes[0].Name, "", ""))

		ginkgo.By("Deleting subnet " + subnetName + " while the endpoint exists")
		framework.ExpectNoError(f.KubeOVNClientSet.KubeovnV1().Subnets().Delete(context.Background(), subnetName, metav1.DeleteOptions{}))
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			s, err := f.KubeOVNClientSet.KubeovnV1().Subnets().Get(context.Background(), subnetName, metav1.GetOptions{})
			return err == nil && !s.DeletionTimestamp.IsZero(), nil
		}, "the subnet is terminating")
		time.Sleep(10 * time.Second)
		framework.ExpectEqual(lspField(ep.Status.InterfaceID, "name"), ep.Status.InterfaceID)
		_, err := f.KubeOVNClientSet.KubeovnV1().Subnets().Get(context.Background(), subnetName, metav1.GetOptions{})
		framework.ExpectNoError(err, "the subnet must wait for the endpoint")

		ginkgo.By("Deleting the endpoint lets the subnet go")
		epClient.DeleteSync(epName)
		framework.ExpectNoError(subnetClient.WaitToDisappear(subnetName, 2*time.Second, 2*time.Minute))
	})
})
