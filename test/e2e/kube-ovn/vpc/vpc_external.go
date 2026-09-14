package vpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/moby/moby/api/types/network"
	"github.com/onsi/ginkgo/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iproute"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

// extDefaultSubnet must match the cluster's --external-gateway-switch flag value.
const extDefaultSubnet = "external"

func makeProviderNetwork(name string, linkMap map[string]*iproute.Link) *kubeovnv1.ProviderNetwork {
	var defaultInterface string
	customInterfaces := make(map[string][]string)
	for node, link := range linkMap {
		if !strings.ContainsRune(node, '-') {
			continue
		}
		if defaultInterface == "" {
			defaultInterface = link.IfName
		} else if link.IfName != defaultInterface {
			customInterfaces[link.IfName] = append(customInterfaces[link.IfName], node)
		}
	}
	return framework.MakeProviderNetwork(name, false, defaultInterface, customInterfaces, nil)
}

// setNodeGWLabel adds the external gateway label to a node, or removes it when add is false.
func setNodeGWLabel(cs clientset.Interface, nodeName string, add bool) {
	if add {
		setNodeGWLabelValue(cs, nodeName, "true")
		return
	}
	setNodeGWLabelValue(cs, nodeName, "")
}

// setNodeGWLabelValue sets the external gateway label of a node to the given value; an empty value
// removes the label.
func setNodeGWLabelValue(cs clientset.Interface, nodeName, value string) {
	ginkgo.GinkgoHelper()
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		node, err := cs.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		updated := node.DeepCopy()
		if updated.Labels == nil {
			updated.Labels = make(map[string]string)
		}
		if value == "" {
			delete(updated.Labels, util.ExGatewayLabel)
		} else {
			updated.Labels[util.ExGatewayLabel] = value
		}
		_, err = cs.CoreV1().Nodes().Update(context.Background(), updated, metav1.UpdateOptions{})
		return err
	})
	framework.ExpectNoError(err)
}

// gwLabelState captures the external gateway label of a node so that the suite can restore it
// verbatim. kube-ovn sets the label to "false" (instead of removing it) when a node stops being an
// external gateway node, so restoring a boolean would turn such a node back into a gateway.
type gwLabelState struct {
	value   string
	present bool
}

// gwLabelStateOf returns the external gateway label state of the given labels.
func gwLabelStateOf(labels map[string]string) gwLabelState {
	value, present := labels[util.ExGatewayLabel]
	return gwLabelState{value: value, present: present}
}

// gwLabelStateOfNode returns the external gateway label state of the named node.
func gwLabelStateOfNode(cs clientset.Interface, nodeName string) gwLabelState {
	ginkgo.GinkgoHelper()
	node, err := cs.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	framework.ExpectNoError(err)
	return gwLabelStateOf(node.Labels)
}

// restoreValue returns the value the label has to be set to; an empty value removes the label.
func (s gwLabelState) restoreValue() string {
	if !s.present {
		return ""
	}
	return s.value
}

// restore brings the external gateway label of the named node back to its original state.
func (s gwLabelState) restore(cs clientset.Interface, nodeName string) {
	setNodeGWLabelValue(cs, nodeName, s.restoreValue())
}

// countGWNodes returns the number of nodes that already carry the external gateway label and are
// not managed by this suite. The chassis of these nodes stay on the VPC external LRPs during the
// whole test, so expectations have to be relative to them.
func countGWNodes(cs clientset.Interface, ignored ...string) int {
	ginkgo.GinkgoHelper()
	nodes, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	framework.ExpectNoError(err)

	skip := make(map[string]struct{}, len(ignored))
	for _, name := range ignored {
		skip[name] = struct{}{}
	}
	count := 0
	for i := range nodes.Items {
		node := &nodes.Items[i]
		if _, ok := skip[node.Name]; ok {
			continue
		}
		if node.Labels[util.ExGatewayLabel] == "true" {
			count++
		}
	}
	return count
}

// pickNodesWithoutGWLabel returns up to count ready schedulable nodes that do not carry the
// external gateway label, so the suite never reconfigures a cluster that already has an external
// gateway configured.
func pickNodesWithoutGWLabel(cs clientset.Interface, count int) []string {
	ginkgo.GinkgoHelper()
	nodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
	framework.ExpectNoError(err)

	names := make([]string, 0, count)
	for i := range nodes.Items {
		node := &nodes.Items[i]
		if node.Labels[util.ExGatewayLabel] == "true" {
			continue
		}
		names = append(names, node.Name)
		if len(names) == count {
			break
		}
	}
	return names
}

// deleteOvnEipBestEffort deletes an OVN EIP and waits for it to disappear. A timeout is only
// logged: deleting the VPC already releases its LRP EIP, and slow garbage collection must not
// fail the spec. Only use it for the shared default external subnet, whose subnet cleanup is
// best-effort as well; suite-owned subnets delete their EIPs with DeleteSync so that the subnet
// they depend on is removed deterministically.
func deleteOvnEipBestEffort(client *framework.OvnEipClient, name string) {
	ginkgo.GinkgoHelper()
	client.Delete(name)

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := client.OvnEipInterface.Get(context.Background(), name, metav1.GetOptions{}); apierrors.IsNotFound(err) {
			return
		}
		time.Sleep(time.Second)
	}
	framework.Logf("OVN EIP %s still exists after cleanup, leaving it to the controller", name)
}

// waitSubnetGoneBestEffort waits until the subnet disappears and reports the outcome without
// failing the spec.
func waitSubnetGoneBestEffort(client *framework.SubnetClient, name string) bool {
	ginkgo.GinkgoHelper()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := client.SubnetInterface.Get(context.Background(), name, metav1.GetOptions{}); apierrors.IsNotFound(err) {
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}

// ensureDefaultExternalSubnet makes the cluster wide default external gateway subnet available
// and reports whether this suite created it (and therefore owns its cleanup). The subnet is
// shared with other suites (e.g. [group:rlr]), so an existing subnet is reused and a leftover
// subnet that is still terminating is waited for instead of failing the spec.
func ensureDefaultExternalSubnet(f *framework.Framework, clusterName, suffix, dockerNetName, pnName, vlanPrefix string) (bool, func()) {
	ginkgo.GinkgoHelper()

	subnetClient := f.SubnetClient()
	for {
		subnet, err := subnetClient.SubnetInterface.Get(context.Background(), extDefaultSubnet, metav1.GetOptions{})
		switch {
		case err == nil && subnet.DeletionTimestamp.IsZero():
			framework.ExpectTrue(subnetClient.WaitToBeReady(extDefaultSubnet, 2*time.Minute),
				"wait for the existing subnet %s to become ready", extDefaultSubnet)
			return false, nil
		case err == nil:
			ginkgo.By("Waiting for the leftover subnet " + extDefaultSubnet + " to disappear")
			if !waitSubnetGoneBestEffort(subnetClient, extDefaultSubnet) {
				framework.Failf("subnet %s is stuck in Terminating, a previous run left it behind", extDefaultSubnet)
			}
		case apierrors.IsNotFound(err):
			ginkgo.By("Setting up main docker network for default external subnet")
			linkMap, disconnect := connectDockerNetwork(f, clusterName, dockerNetName)
			pn := makeProviderNetwork(pnName, linkMap)
			// A previous run intentionally keeps the provider network when the shared subnet is
			// stuck terminating; the subnet is gone now, so reuse the leftover instead of failing
			// CreateSync with AlreadyExists.
			switch _, getErr := f.ProviderNetworkClient().ProviderNetworkInterface.Get(context.Background(), pnName, metav1.GetOptions{}); {
			case getErr == nil:
				framework.ExpectTrue(f.ProviderNetworkClient().WaitToBeReady(pnName, 2*time.Minute),
					"wait for the retained provider network %s to become ready", pnName)
			case apierrors.IsNotFound(getErr):
				_ = f.ProviderNetworkClient().CreateSync(pn)
			default:
				framework.ExpectNoError(getErr)
			}
			vlan := framework.MakeVlan(vlanPrefix+"-"+suffix, pnName, 0)
			_ = f.VlanClient().Create(vlan)
			cidr, gw, excludeIPs := dockerSubnetCIDR(f, dockerNetName)
			_ = subnetClient.CreateSync(framework.MakeSubnet(extDefaultSubnet, vlan.Name, cidr, gw, "", "", excludeIPs, nil, nil))
			return true, disconnect
		default:
			framework.ExpectNoError(err)
		}
	}
}

// lrpChassisList returns the chassis names registered on the given LRP's gateway chassis.
// It queries Gateway_Chassis by ExternalID rather than lrp-gateway-chassis-list because
// some OVN versions return rc=1 for an LRP that has no chassis entries yet.
func lrpChassisList(vpcName, subnetName string) ([]string, error) {
	lrp := vpcName + "-" + subnetName
	cmd := "ovn-nbctl --bare --columns=name find Gateway_Chassis external_ids:lrp=" + lrp
	out, _, err := framework.NBExec(cmd)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(out)) == "" {
		return nil, nil
	}
	var result []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}
	return result, nil
}

func lrpExists(vpcName, subnetName string) (bool, error) {
	lrp := vpcName + "-" + subnetName
	cmd := "ovn-nbctl --bare --columns=name find Logical_Router_Port name=" + lrp
	out, _, err := framework.NBExec(cmd)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func waitLRPChassisCount(vpcName, subnetName string, count int) {
	ginkgo.GinkgoHelper()
	framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		list, err := lrpChassisList(vpcName, subnetName)
		if err != nil {
			return false, err
		}
		return len(list) == count, nil
	}, fmt.Sprintf("LRP %s-%s has %d chassis", vpcName, subnetName, count))
}

func waitLRPPresent(vpcName, subnetName string) {
	ginkgo.GinkgoHelper()
	framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		return lrpExists(vpcName, subnetName)
	}, fmt.Sprintf("LRP %s-%s exists in OVN NB", vpcName, subnetName))
}

func waitLRPAbsent(vpcName, subnetName string) {
	ginkgo.GinkgoHelper()
	framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		exists, err := lrpExists(vpcName, subnetName)
		if err != nil {
			return false, err
		}
		return !exists, nil
	}, fmt.Sprintf("LRP %s-%s is absent from OVN NB", vpcName, subnetName))
}

// connectDockerNetwork creates the docker network (if absent), connects all Kind nodes, and
// returns the per-node link map. The returned function disconnects nodes on cleanup.
func connectDockerNetwork(f *framework.Framework, clusterName, netName string) (map[string]*iproute.Link, func()) {
	ginkgo.GinkgoHelper()

	net, err := docker.NetworkCreate(netName, f.HasIPv6(), true)
	framework.ExpectNoError(err, "creating docker network "+netName)

	nodes, err := kind.ListNodes(clusterName, "")
	framework.ExpectNoError(err)
	framework.ExpectNotEmpty(nodes)

	framework.ExpectNoError(kind.NetworkConnect(net.ID, nodes), "connecting nodes to "+netName)

	nodes, err = kind.ListNodes(clusterName, "")
	framework.ExpectNoError(err)

	linkMap := make(map[string]*iproute.Link, len(nodes))
	for _, node := range nodes {
		links, err := node.ListLinks()
		framework.ExpectNoError(err)
		for _, link := range links {
			if link.Address == node.NetworkSettings.Networks[netName].MacAddress.String() {
				linkMap[node.ID] = &link
				break
			}
		}
		framework.ExpectHaveKey(linkMap, node.ID)
		linkMap[node.Name()] = linkMap[node.ID]
	}

	cleanup := func() {
		inspected, err := docker.NetworkInspect(netName)
		if err != nil {
			return
		}
		nodes, err := kind.ListNodes(clusterName, "")
		if err != nil {
			return
		}
		_ = kind.NetworkDisconnect(inspected.ID, nodes)
		_ = docker.NetworkRemove(inspected.ID)
	}
	return linkMap, cleanup
}

// orderedDockerSubnet joins the enabled Docker IPAM subnets and gateways with IPv4 first.
// Kube-OVN dual-stack subnets must keep the IPv4 CIDR before the IPv6 one (the IPAM assigns
// cidrs[0] to IPv4), while the Docker IPAM config order is not guaranteed. [group:rlr] does the
// same ordering when it creates the shared external subnet.
func orderedDockerSubnet(configs []network.IPAMConfig, hasIPv4, hasIPv6 bool) (cidr, gw string) {
	var cidrV4, cidrV6, gwV4, gwV6 string
	for _, cfg := range configs {
		switch util.CheckProtocol(cfg.Subnet.String()) {
		case kubeovnv1.ProtocolIPv4:
			if hasIPv4 {
				cidrV4, gwV4 = cfg.Subnet.String(), cfg.Gateway.String()
			}
		case kubeovnv1.ProtocolIPv6:
			if hasIPv6 {
				cidrV6, gwV6 = cfg.Subnet.String(), cfg.Gateway.String()
			}
		}
	}

	cidrParts := make([]string, 0, 2)
	gwParts := make([]string, 0, 2)
	if cidrV4 != "" {
		cidrParts = append(cidrParts, cidrV4)
		gwParts = append(gwParts, gwV4)
	}
	if cidrV6 != "" {
		cidrParts = append(cidrParts, cidrV6)
		gwParts = append(gwParts, gwV6)
	}
	return strings.Join(cidrParts, ","), strings.Join(gwParts, ",")
}

// dockerSubnetCIDR extracts IPv4/IPv6 CIDR, gateway, and container exclude-IPs from a docker network.
func dockerSubnetCIDR(f *framework.Framework, netName string) (cidr, gw string, excludeIPs []string) {
	ginkgo.GinkgoHelper()
	net, err := docker.NetworkInspect(netName)
	framework.ExpectNoError(err)

	cidr, gw = orderedDockerSubnet(net.IPAM.Config, f.HasIPv4(), f.HasIPv6())
	for _, c := range net.Containers {
		if c.IPv4Address.IsValid() && f.HasIPv4() {
			excludeIPs = append(excludeIPs, c.IPv4Address.Addr().String())
		}
		if c.IPv6Address.IsValid() && f.HasIPv6() {
			excludeIPs = append(excludeIPs, c.IPv6Address.Addr().String())
		}
	}
	return cidr, gw, excludeIPs
}

// patchVPCExternal updates EnableExternal and ExtraExternalSubnets on a VPC and waits for ready.
func patchVPCExternal(vpcClient *framework.VpcClient, vpcName string, enable bool, extras []string) {
	ginkgo.GinkgoHelper()
	cur := vpcClient.Get(vpcName)
	mod := cur.DeepCopy()
	mod.Spec.EnableExternal = enable
	mod.Spec.ExtraExternalSubnets = extras
	vpcClient.PatchSync(cur, mod, 2*time.Minute)
}

// Serial: both specs create, use and delete the cluster wide default external gateway subnet,
// so they must never run in parallel with each other or with other specs.
var _ = framework.SerialDescribe("[group:vpc-external]", func() {
	f := framework.NewDefaultFramework("vpc-external")

	var (
		skip        bool
		cs          clientset.Interface
		clusterName string
	)

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		if clusterName == "" {
			k8sNodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
			framework.ExpectNoError(err)
			cluster, ok := kind.IsKindProvided(k8sNodes.Items[0].Spec.ProviderID)
			if !ok {
				skip = true
				ginkgo.Skip("vpc-external e2e only runs on Kind clusters")
			}
			clusterName = cluster
		}
	})

	// =========================================================
	// Test 1: GW node label add/remove syncs chassis on all VPC LRPs
	// =========================================================
	ginkgo.Context("GW node label lifecycle", func() {
		const (
			dockerNetMain  = "kube-ovn-vpc-gw-main"
			dockerNetExtra = "kube-ovn-vpc-gw-extra"
		)

		var (
			suffix          string
			node1, node2    string
			vpc1Name        string
			vpc2Name        string
			extraSubnetName string

			createdDefaultInfra bool
			disconnectMain      func()
			disconnectExtra     func()
			origNode1GW         gwLabelState
			origNode2GW         gwLabelState
			gwBase              int

			providerNetworkClient *framework.ProviderNetworkClient
			vlanClient            *framework.VlanClient
			subnetClient          *framework.SubnetClient
			vpcClient             *framework.VpcClient
			ovnEipClient          *framework.OvnEipClient
		)

		ginkgo.BeforeEach(func() {
			if skip {
				ginkgo.Skip("vpc-external e2e only runs on Kind clusters")
			}

			suffix = framework.RandomSuffix()
			extraSubnetName = "vpc-gw-extra-" + suffix
			vpc1Name = "vpc-gw1-" + suffix
			vpc2Name = "vpc-gw2-" + suffix

			providerNetworkClient = f.ProviderNetworkClient()
			vlanClient = f.VlanClient()
			subnetClient = f.SubnetClient()
			vpcClient = f.VpcClient()
			ovnEipClient = f.OvnEipClient()

			nodes := pickNodesWithoutGWLabel(cs, 2)
			if len(nodes) < 2 {
				ginkgo.Skip("GW node lifecycle test requires at least 2 schedulable nodes without an external gateway label")
			}
			node1, node2 = nodes[0], nodes[1]
			origNode1GW, origNode2GW = gwLabelStateOfNode(cs, node1), gwLabelStateOfNode(cs, node2)
			// Chassis of pre-existing external gateway nodes stay on the VPC LRPs during the whole
			// test, so every expectation has to be relative to them.
			gwBase = countGWNodes(cs, node1, node2)

			// Setup main docker network → provider + VLAN + "external" subnet. The subnet is shared
			// with other suites (e.g. [group:rlr]), so it is reused when it already exists.
			createdDefaultInfra, disconnectMain = ensureDefaultExternalSubnet(f, clusterName, suffix,
				dockerNetMain, "pn-gw-main", "vlan-gw-main")

			// Setup extra docker network → provider + VLAN + extra subnet for VPC2.
			ginkgo.By("Setting up extra docker network for extra external subnet")
			extraLinkMap, disconn2 := connectDockerNetwork(f, clusterName, dockerNetExtra)
			disconnectExtra = disconn2
			pnExtra := makeProviderNetwork("pn-gw-extra", extraLinkMap)
			_ = providerNetworkClient.CreateSync(pnExtra)
			vlanExtra := framework.MakeVlan("vlan-gw-extra-"+suffix, "pn-gw-extra", 0)
			_ = vlanClient.Create(vlanExtra)
			cidr2, gw2, excl2 := dockerSubnetCIDR(f, dockerNetExtra)
			subExtra := framework.MakeSubnet(extraSubnetName, vlanExtra.Name, cidr2, gw2, "", "", excl2, nil, nil)
			_ = subnetClient.CreateSync(subExtra)

			// Label node1 as GW before creating VPCs (controller requires at least one GW node).
			ginkgo.By("Labeling node1 as external GW node")
			setNodeGWLabel(cs, node1, true)

			// Create VPC1: EnableExternal=true, uses default "external" subnet.
			ginkgo.By("Creating VPC1 with EnableExternal=true")
			vpc1 := framework.MakeVpc(vpc1Name, "", true, false, nil)
			_ = vpcClient.CreateSync(vpc1)

			// Create VPC2: EnableExternal=true, uses extraSubnetName via ExtraExternalSubnets.
			ginkgo.By("Creating VPC2 with ExtraExternalSubnets=[" + extraSubnetName + "]")
			vpc2 := framework.MakeVpc(vpc2Name, "", true, false, nil)
			vpc2.Spec.ExtraExternalSubnets = []string{extraSubnetName}
			_ = vpcClient.CreateSync(vpc2)

			// Wait for both LRPs to appear in OVN NB.
			ginkgo.By("Waiting for VPC LRPs to be created")
			waitLRPPresent(vpc1Name, extDefaultSubnet)
			waitLRPPresent(vpc2Name, extraSubnetName)
		})

		ginkgo.AfterEach(func() {
			// Restore the external gateway labels of the nodes managed by this suite instead of
			// removing them unconditionally, so a pre-existing configuration is not clobbered.
			if node1 != "" {
				origNode1GW.restore(cs, node1)
			}
			if node2 != "" {
				origNode2GW.restore(cs, node2)
			}

			// Delete the VPCs before their auto-created LRP EIPs: while a VPC still has
			// enableExternal=true the controller re-allocates the LRP EIP as fast as the test
			// deletes it, so the EIP would never converge (see [group:rlr]). Deleting the VPC
			// releases the external connection, after which the LRP EIP can be removed for good.
			vpcClient.DeleteSync(vpc1Name)
			vpcClient.DeleteSync(vpc2Name)
			deleteOvnEipBestEffort(ovnEipClient, vpc1Name+"-"+extDefaultSubnet)
			// The extra subnet is owned by this suite and deleted below, so its LRP EIP must be
			// gone first; wait for it instead of leaving it to the best-effort default path.
			ovnEipClient.DeleteSync(vpc2Name + "-" + extraSubnetName)

			subnetClient.DeleteSync(extraSubnetName)
			vlanClient.Delete("vlan-gw-extra-" + suffix)
			providerNetworkClient.DeleteSync("pn-gw-extra")
			if createdDefaultInfra {
				// The default external subnet is shared with other suites: remove it only after
				// every LRP EIP allocated from it is gone, and never fail the spec on cleanup.
				subnetClient.Delete(extDefaultSubnet)
				if waitSubnetGoneBestEffort(subnetClient, extDefaultSubnet) {
					vlanClient.Delete("vlan-gw-main-" + suffix)
					providerNetworkClient.DeleteSync("pn-gw-main")
				} else {
					framework.Logf("subnet %s was not removed, keeping its provider network, vlan and docker network for the next run", extDefaultSubnet)
					disconnectMain = nil
				}
			}

			if disconnectExtra != nil {
				disconnectExtra()
			}
			if disconnectMain != nil {
				disconnectMain()
			}
		})

		framework.ConformanceIt("should sync gateway chassis to all VPC LRPs when GW label is added or removed", func() {
			f.SkipVersionPriorTo(1, 17, "VPC external LRP chassis reconciliation was introduced in v1.17")

			ginkgo.By("Step 1: Verify initial state — node1 chassis on both LRPs")
			waitLRPChassisCount(vpc1Name, extDefaultSubnet, gwBase+1)
			waitLRPChassisCount(vpc2Name, extraSubnetName, gwBase+1)

			ginkgo.By("Step 2: Label node2 as GW — both chassis appear on both LRPs")
			setNodeGWLabel(cs, node2, true)
			waitLRPChassisCount(vpc1Name, extDefaultSubnet, gwBase+2)
			waitLRPChassisCount(vpc2Name, extraSubnetName, gwBase+2)

			ginkgo.By("Step 3: Remove GW label from node1 — only node2 chassis remains")
			setNodeGWLabel(cs, node1, false)
			waitLRPChassisCount(vpc1Name, extDefaultSubnet, gwBase+1)
			waitLRPChassisCount(vpc2Name, extraSubnetName, gwBase+1)

			ginkgo.By("Step 4: Remove GW label from node2 — only pre-existing external gateways remain")
			setNodeGWLabel(cs, node2, false)
			waitLRPChassisCount(vpc1Name, extDefaultSubnet, gwBase)
			waitLRPChassisCount(vpc2Name, extraSubnetName, gwBase)

			ginkgo.By("Step 5: Re-label node1 — chassis restored on both LRPs")
			setNodeGWLabel(cs, node1, true)
			waitLRPChassisCount(vpc1Name, extDefaultSubnet, gwBase+1)
			waitLRPChassisCount(vpc2Name, extraSubnetName, gwBase+1)
		})
	})

	// =========================================================
	// Test 2: VPC external subnet changes manage LRPs and chassis
	// =========================================================
	ginkgo.Context("VPC external subnets lifecycle", func() {
		const (
			dockerNetMain   = "kube-ovn-vpc-sub-main"
			dockerNetExtra1 = "kube-ovn-vpc-sub-extra1"
			dockerNetExtra2 = "kube-ovn-vpc-sub-extra2"
		)

		var (
			suffix                string
			node1                 string
			vpcName               string
			extra1SubnetName      string
			extra2SubnetName      string
			createdDefaultInfra   bool
			disconnectMain        func()
			disconnectExtra1      func()
			disconnectExtra2      func()
			origNode1GW           gwLabelState
			gwBase                int
			providerNetworkClient *framework.ProviderNetworkClient
			vlanClient            *framework.VlanClient
			subnetClient          *framework.SubnetClient
			vpcClient             *framework.VpcClient
			ovnEipClient          *framework.OvnEipClient
		)

		ginkgo.BeforeEach(func() {
			if skip {
				ginkgo.Skip("vpc-external e2e only runs on Kind clusters")
			}

			suffix = framework.RandomSuffix()
			vpcName = "vpc-sub-" + suffix
			extra1SubnetName = "vpc-sub-extra1-" + suffix
			extra2SubnetName = "vpc-sub-extra2-" + suffix

			providerNetworkClient = f.ProviderNetworkClient()
			vlanClient = f.VlanClient()
			subnetClient = f.SubnetClient()
			vpcClient = f.VpcClient()
			ovnEipClient = f.OvnEipClient()

			nodes := pickNodesWithoutGWLabel(cs, 1)
			if len(nodes) < 1 {
				ginkgo.Skip("VPC external subnet lifecycle test requires a schedulable node without an external gateway label")
			}
			node1 = nodes[0]
			origNode1GW = gwLabelStateOfNode(cs, node1)
			// Chassis of pre-existing external gateway nodes stay on the VPC LRPs during the whole
			// test, so every expectation has to be relative to them.
			gwBase = countGWNodes(cs, node1)

			// Setup main docker network → "external" subnet. The subnet is shared with other suites
			// (e.g. [group:rlr]), so it is reused when it already exists.
			createdDefaultInfra, disconnectMain = ensureDefaultExternalSubnet(f, clusterName, suffix,
				dockerNetMain, "pn-sub-main", "vlan-sub-main")

			// Setup docker network for extra1 subnet.
			ginkgo.By("Setting up extra1 docker network")
			extra1LinkMap, disconn2 := connectDockerNetwork(f, clusterName, dockerNetExtra1)
			disconnectExtra1 = disconn2
			pnE1 := makeProviderNetwork("pn-sub-ext1", extra1LinkMap)
			_ = providerNetworkClient.CreateSync(pnE1)
			vlanE1 := framework.MakeVlan("vlan-sub-extra1-"+suffix, "pn-sub-ext1", 0)
			_ = vlanClient.Create(vlanE1)
			cidr1, gw1, excl1 := dockerSubnetCIDR(f, dockerNetExtra1)
			subE1 := framework.MakeSubnet(extra1SubnetName, vlanE1.Name, cidr1, gw1, "", "", excl1, nil, nil)
			_ = subnetClient.CreateSync(subE1)

			// Setup docker network for extra2 subnet.
			ginkgo.By("Setting up extra2 docker network")
			extra2LinkMap, disconn3 := connectDockerNetwork(f, clusterName, dockerNetExtra2)
			disconnectExtra2 = disconn3
			pnE2 := makeProviderNetwork("pn-sub-ext2", extra2LinkMap)
			_ = providerNetworkClient.CreateSync(pnE2)
			vlanE2 := framework.MakeVlan("vlan-sub-extra2-"+suffix, "pn-sub-ext2", 0)
			_ = vlanClient.Create(vlanE2)
			cidr2, gw2, excl2 := dockerSubnetCIDR(f, dockerNetExtra2)
			subE2 := framework.MakeSubnet(extra2SubnetName, vlanE2.Name, cidr2, gw2, "", "", excl2, nil, nil)
			_ = subnetClient.CreateSync(subE2)

			// Label node1 as GW before creating the VPC.
			ginkgo.By("Labeling node1 as external GW node")
			setNodeGWLabel(cs, node1, true)

			// Create VPC with EnableExternal=true, no ExtraExternalSubnets.
			ginkgo.By("Creating VPC with EnableExternal=true and no ExtraExternalSubnets")
			vpc := framework.MakeVpc(vpcName, "", true, false, nil)
			_ = vpcClient.CreateSync(vpc)

			ginkgo.By("Waiting for default external LRP to be created")
			waitLRPPresent(vpcName, extDefaultSubnet)
		})

		ginkgo.AfterEach(func() {
			if node1 != "" {
				origNode1GW.restore(cs, node1)
			}

			// Delete the VPC before its auto-created LRP EIPs: while the VPC still has
			// enableExternal=true the controller re-allocates the LRP EIP as fast as the test
			// deletes it, so the EIP would never converge (see [group:rlr]).
			vpcClient.DeleteSync(vpcName)
			deleteOvnEipBestEffort(ovnEipClient, vpcName+"-"+extDefaultSubnet)
			// The extra subnets are owned by this suite and deleted below, so their LRP EIPs must
			// be gone first; wait for them instead of leaving them to the best-effort default path.
			ovnEipClient.DeleteSync(vpcName + "-" + extra1SubnetName)
			ovnEipClient.DeleteSync(vpcName + "-" + extra2SubnetName)

			subnetClient.DeleteSync(extra2SubnetName)
			subnetClient.DeleteSync(extra1SubnetName)
			vlanClient.Delete("vlan-sub-extra2-" + suffix)
			vlanClient.Delete("vlan-sub-extra1-" + suffix)
			providerNetworkClient.DeleteSync("pn-sub-ext2")
			providerNetworkClient.DeleteSync("pn-sub-ext1")
			if createdDefaultInfra {
				// The default external subnet is shared with other suites: remove it only after
				// every LRP EIP allocated from it is gone, and never fail the spec on cleanup.
				subnetClient.Delete(extDefaultSubnet)
				if waitSubnetGoneBestEffort(subnetClient, extDefaultSubnet) {
					vlanClient.Delete("vlan-sub-main-" + suffix)
					providerNetworkClient.DeleteSync("pn-sub-main")
				} else {
					framework.Logf("subnet %s was not removed, keeping its provider network, vlan and docker network for the next run", extDefaultSubnet)
					disconnectMain = nil
				}
			}

			if disconnectExtra2 != nil {
				disconnectExtra2()
			}
			if disconnectExtra1 != nil {
				disconnectExtra1()
			}
			if disconnectMain != nil {
				disconnectMain()
			}
		})

		framework.ConformanceIt("should manage LRP connections and chassis through external subnet configuration changes", func() {
			f.SkipVersionPriorTo(1, 17, "VPC external LRP chassis reconciliation was introduced in v1.17")

			// ------------------------------------------------------------------
			// Phase 1: Default subnet active — toggle EnableExternal
			// ------------------------------------------------------------------
			ginkgo.By("Phase 1: Verify initial state — default LRP active with chassis")
			waitLRPPresent(vpcName, extDefaultSubnet)
			waitLRPChassisCount(vpcName, extDefaultSubnet, gwBase+1)
			exists1, err := lrpExists(vpcName, extra1SubnetName)
			framework.ExpectNoError(err)
			framework.ExpectEqual(exists1, false)
			exists2, err := lrpExists(vpcName, extra2SubnetName)
			framework.ExpectNoError(err)
			framework.ExpectEqual(exists2, false)

			ginkgo.By("Phase 1: Disable EnableExternal → default LRP removed")
			patchVPCExternal(vpcClient, vpcName, false, nil)
			waitLRPAbsent(vpcName, extDefaultSubnet)

			ginkgo.By("Phase 1: Re-enable EnableExternal → default LRP re-created with chassis")
			patchVPCExternal(vpcClient, vpcName, true, nil)
			waitLRPPresent(vpcName, extDefaultSubnet)
			waitLRPChassisCount(vpcName, extDefaultSubnet, gwBase+1)

			// ------------------------------------------------------------------
			// Phase 2: Switch to extra1 (default LRP must be removed)
			// ------------------------------------------------------------------
			ginkgo.By("Phase 2: Set ExtraExternalSubnets=[extra1] → extra1 LRP created, default LRP removed")
			patchVPCExternal(vpcClient, vpcName, true, []string{extra1SubnetName})
			waitLRPPresent(vpcName, extra1SubnetName)
			waitLRPAbsent(vpcName, extDefaultSubnet)
			waitLRPChassisCount(vpcName, extra1SubnetName, gwBase+1)

			// ------------------------------------------------------------------
			// Phase 3: Add extra2 (both extra LRPs active)
			// ------------------------------------------------------------------
			ginkgo.By("Phase 3: Add extra2 to ExtraExternalSubnets → both extra LRPs active with chassis")
			patchVPCExternal(vpcClient, vpcName, true, []string{extra1SubnetName, extra2SubnetName})
			waitLRPPresent(vpcName, extra2SubnetName)
			waitLRPChassisCount(vpcName, extra2SubnetName, gwBase+1)
			extra1Exists, err := lrpExists(vpcName, extra1SubnetName)
			framework.ExpectNoError(err)
			framework.ExpectEqual(extra1Exists, true)

			// ------------------------------------------------------------------
			// Phase 4: Toggle EnableExternal with both extra LRPs active
			// ------------------------------------------------------------------
			ginkgo.By("Phase 4: Disable EnableExternal → both extra LRPs removed")
			patchVPCExternal(vpcClient, vpcName, false, nil)
			waitLRPAbsent(vpcName, extra1SubnetName)
			waitLRPAbsent(vpcName, extra2SubnetName)

			ginkgo.By("Phase 4: Re-enable with ExtraExternalSubnets=[extra1,extra2] → both re-created with chassis")
			patchVPCExternal(vpcClient, vpcName, true, []string{extra1SubnetName, extra2SubnetName})
			waitLRPPresent(vpcName, extra1SubnetName)
			waitLRPPresent(vpcName, extra2SubnetName)
			waitLRPChassisCount(vpcName, extra1SubnetName, gwBase+1)
			waitLRPChassisCount(vpcName, extra2SubnetName, gwBase+1)

			// ------------------------------------------------------------------
			// Phase 5: Remove extra1 (only extra2 remains)
			// ------------------------------------------------------------------
			ginkgo.By("Phase 5: Remove extra1 from ExtraExternalSubnets → extra1 LRP removed, extra2 stays")
			patchVPCExternal(vpcClient, vpcName, true, []string{extra2SubnetName})
			waitLRPAbsent(vpcName, extra1SubnetName)
			extra2Exists, err := lrpExists(vpcName, extra2SubnetName)
			framework.ExpectNoError(err)
			framework.ExpectEqual(extra2Exists, true)

			// ------------------------------------------------------------------
			// Phase 6: Clear ExtraExternalSubnets → back to default subnet
			// ------------------------------------------------------------------
			ginkgo.By("Phase 6: Clear ExtraExternalSubnets → extra2 LRP removed, default LRP re-created with chassis")
			patchVPCExternal(vpcClient, vpcName, true, nil)
			waitLRPAbsent(vpcName, extra2SubnetName)
			waitLRPPresent(vpcName, extDefaultSubnet)
			waitLRPChassisCount(vpcName, extDefaultSubnet, gwBase+1)
		})
	})
})
