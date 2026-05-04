package vpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
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

func setNodeGWLabel(cs clientset.Interface, nodeName string, add bool) {
	ginkgo.GinkgoHelper()
	node, err := cs.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	framework.ExpectNoError(err)
	updated := node.DeepCopy()
	if updated.Labels == nil {
		updated.Labels = make(map[string]string)
	}
	if add {
		updated.Labels[util.ExGatewayLabel] = "true"
	} else {
		delete(updated.Labels, util.ExGatewayLabel)
	}
	_, err = cs.CoreV1().Nodes().Update(context.Background(), updated, metav1.UpdateOptions{})
	framework.ExpectNoError(err)
}

// lrpChassisList returns the chassis names registered on the given LRP's gateway chassis.
// It queries Gateway_Chassis by ExternalID rather than lrp-gateway-chassis-list because
// some OVN versions return rc=1 for an LRP that has no chassis entries yet.
func lrpChassisList(vpcName, subnetName string) []string {
	lrp := vpcName + "-" + subnetName
	cmd := "ovn-nbctl --bare --columns=name find Gateway_Chassis external_ids:lrp=" + lrp
	out, _, err := framework.NBExec(cmd)
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return nil
	}
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}
	return result
}

func lrpExists(vpcName, subnetName string) bool {
	lrp := vpcName + "-" + subnetName
	cmd := "ovn-nbctl --bare --columns=name find Logical_Router_Port name=" + lrp
	out, _, err := framework.NBExec(cmd)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

func waitLRPChassisCount(vpcName, subnetName string, count int) {
	ginkgo.GinkgoHelper()
	framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		return len(lrpChassisList(vpcName, subnetName)) == count, nil
	}, fmt.Sprintf("LRP %s-%s has %d chassis", vpcName, subnetName, count))
}

func waitLRPPresent(vpcName, subnetName string) {
	ginkgo.GinkgoHelper()
	framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		return lrpExists(vpcName, subnetName), nil
	}, fmt.Sprintf("LRP %s-%s exists in OVN NB", vpcName, subnetName))
}

func waitLRPAbsent(vpcName, subnetName string) {
	ginkgo.GinkgoHelper()
	framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		return !lrpExists(vpcName, subnetName), nil
	}, fmt.Sprintf("LRP %s-%s is absent from OVN NB", vpcName, subnetName))
}

// connectDockerNetwork creates the docker network (if absent), connects all Kind nodes, and
// returns the per-node link map. The returned function disconnects nodes on cleanup.
func connectDockerNetwork(f *framework.Framework, clusterName, netName string) (map[string]*iproute.Link, func()) {
	ginkgo.GinkgoHelper()

	net, err := docker.NetworkCreate(netName, f.HasIPv6(), false)
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
	}
	return linkMap, cleanup
}

// dockerSubnetCIDR extracts IPv4/IPv6 CIDR, gateway, and container exclude-IPs from a docker network.
func dockerSubnetCIDR(f *framework.Framework, netName string) (cidr, gw string, excludeIPs []string) {
	ginkgo.GinkgoHelper()
	net, err := docker.NetworkInspect(netName)
	framework.ExpectNoError(err)

	var cidrParts, gwParts []string
	for _, cfg := range net.IPAM.Config {
		switch util.CheckProtocol(cfg.Subnet.String()) {
		case kubeovnv1.ProtocolIPv4:
			if f.HasIPv4() {
				cidrParts = append(cidrParts, cfg.Subnet.String())
				gwParts = append(gwParts, cfg.Gateway.String())
			}
		case kubeovnv1.ProtocolIPv6:
			if f.HasIPv6() {
				cidrParts = append(cidrParts, cfg.Subnet.String())
				gwParts = append(gwParts, cfg.Gateway.String())
			}
		}
	}
	for _, c := range net.Containers {
		if c.IPv4Address.IsValid() && f.HasIPv4() {
			excludeIPs = append(excludeIPs, c.IPv4Address.Addr().String())
		}
		if c.IPv6Address.IsValid() && f.HasIPv6() {
			excludeIPs = append(excludeIPs, c.IPv6Address.Addr().String())
		}
	}
	return strings.Join(cidrParts, ","), strings.Join(gwParts, ","), excludeIPs
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

var _ = framework.Describe("[group:vpc-external]", func() {
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

			disconnectMain  func()
			disconnectExtra func()

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

			k8sNodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
			framework.ExpectNoError(err)
			if len(k8sNodes.Items) < 2 {
				ginkgo.Skip("GW node lifecycle test requires at least 2 schedulable nodes")
			}
			node1 = k8sNodes.Items[0].Name
			node2 = k8sNodes.Items[1].Name

			// Setup main docker network → provider + VLAN + "external" subnet.
			ginkgo.By("Setting up main docker network for default external subnet")
			mainLinkMap, disconn := connectDockerNetwork(f, clusterName, dockerNetMain)
			disconnectMain = disconn
			pn := makeProviderNetwork("pn-gw-main", mainLinkMap)
			_ = providerNetworkClient.CreateSync(pn)
			vlan := framework.MakeVlan("vlan-gw-main-"+suffix, "pn-gw-main", 0)
			_ = vlanClient.Create(vlan)
			cidr, gw, excl := dockerSubnetCIDR(f, dockerNetMain)
			sub := framework.MakeSubnet(extDefaultSubnet, vlan.Name, cidr, gw, "", "", excl, nil, nil)
			_ = subnetClient.CreateSync(sub)

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
			// Always remove GW labels to avoid contaminating other tests.
			if node1 != "" {
				setNodeGWLabel(cs, node1, false)
			}
			if node2 != "" {
				setNodeGWLabel(cs, node2, false)
			}

			ovnEipClient.DeleteSync(vpc1Name + "-" + extDefaultSubnet)
			ovnEipClient.DeleteSync(vpc2Name + "-" + extraSubnetName)
			vpcClient.DeleteSync(vpc1Name)
			vpcClient.DeleteSync(vpc2Name)

			time.Sleep(time.Second)
			subnetClient.DeleteSync(extraSubnetName)
			subnetClient.DeleteSync(extDefaultSubnet)
			vlanClient.Delete("vlan-gw-extra-" + suffix)
			vlanClient.Delete("vlan-gw-main-" + suffix)
			providerNetworkClient.DeleteSync("pn-gw-extra")
			providerNetworkClient.DeleteSync("pn-gw-main")

			if disconnectExtra != nil {
				disconnectExtra()
			}
			if disconnectMain != nil {
				disconnectMain()
			}
		})

		framework.ConformanceIt("should sync gateway chassis to all VPC LRPs when GW label is added or removed", func() {
			f.SkipVersionPriorTo(1, 16, "VPC external subnet chassis reconciliation was enhanced in v1.16")

			ginkgo.By("Step 1: Verify initial state — node1 chassis on both LRPs")
			waitLRPChassisCount(vpc1Name, extDefaultSubnet, 1)
			waitLRPChassisCount(vpc2Name, extraSubnetName, 1)

			ginkgo.By("Step 2: Label node2 as GW — both chassis appear on both LRPs")
			setNodeGWLabel(cs, node2, true)
			waitLRPChassisCount(vpc1Name, extDefaultSubnet, 2)
			waitLRPChassisCount(vpc2Name, extraSubnetName, 2)

			ginkgo.By("Step 3: Remove GW label from node1 — only node2 chassis remains")
			setNodeGWLabel(cs, node1, false)
			waitLRPChassisCount(vpc1Name, extDefaultSubnet, 1)
			waitLRPChassisCount(vpc2Name, extraSubnetName, 1)

			ginkgo.By("Step 4: Remove GW label from node2 — chassis list becomes empty")
			setNodeGWLabel(cs, node2, false)
			waitLRPChassisCount(vpc1Name, extDefaultSubnet, 0)
			waitLRPChassisCount(vpc2Name, extraSubnetName, 0)

			ginkgo.By("Step 5: Re-label node1 — chassis restored on both LRPs")
			setNodeGWLabel(cs, node1, true)
			waitLRPChassisCount(vpc1Name, extDefaultSubnet, 1)
			waitLRPChassisCount(vpc2Name, extraSubnetName, 1)
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
			suffix                    string
			node1                     string
			vpcName                   string
			extra1SubnetName          string
			extra2SubnetName          string

			disconnectMain   func()
			disconnectExtra1 func()
			disconnectExtra2 func()

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

			k8sNodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
			framework.ExpectNoError(err)
			framework.ExpectNotEmpty(k8sNodes.Items)
			node1 = k8sNodes.Items[0].Name

			// Setup main docker network → "external" subnet.
			ginkgo.By("Setting up main docker network for default external subnet")
			mainLinkMap, disconn := connectDockerNetwork(f, clusterName, dockerNetMain)
			disconnectMain = disconn
			pn := makeProviderNetwork("pn-sub-main", mainLinkMap)
			_ = providerNetworkClient.CreateSync(pn)
			vlan := framework.MakeVlan("vlan-sub-main-"+suffix, "pn-sub-main", 0)
			_ = vlanClient.Create(vlan)
			cidr, gw, excl := dockerSubnetCIDR(f, dockerNetMain)
			sub := framework.MakeSubnet(extDefaultSubnet, vlan.Name, cidr, gw, "", "", excl, nil, nil)
			_ = subnetClient.CreateSync(sub)

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
				setNodeGWLabel(cs, node1, false)
			}

			// Delete any auto-created LRP EIPs for all subnets that may have been connected.
			ovnEipClient.DeleteSync(vpcName + "-" + extDefaultSubnet)
			ovnEipClient.DeleteSync(vpcName + "-" + extra1SubnetName)
			ovnEipClient.DeleteSync(vpcName + "-" + extra2SubnetName)
			vpcClient.DeleteSync(vpcName)

			time.Sleep(time.Second)
			subnetClient.DeleteSync(extra2SubnetName)
			subnetClient.DeleteSync(extra1SubnetName)
			subnetClient.DeleteSync(extDefaultSubnet)
			vlanClient.Delete("vlan-sub-extra2-" + suffix)
			vlanClient.Delete("vlan-sub-extra1-" + suffix)
			vlanClient.Delete("vlan-sub-main-" + suffix)
			providerNetworkClient.DeleteSync("pn-sub-ext2")
			providerNetworkClient.DeleteSync("pn-sub-ext1")
			providerNetworkClient.DeleteSync("pn-sub-main")

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
			f.SkipVersionPriorTo(1, 16, "VPC external subnet chassis reconciliation was enhanced in v1.16")

			// ------------------------------------------------------------------
			// Phase 1: Default subnet active — toggle EnableExternal
			// ------------------------------------------------------------------
			ginkgo.By("Phase 1: Verify initial state — default LRP active with chassis")
			waitLRPPresent(vpcName, extDefaultSubnet)
			waitLRPChassisCount(vpcName, extDefaultSubnet, 1)
			framework.ExpectEqual(lrpExists(vpcName, extra1SubnetName), false)
			framework.ExpectEqual(lrpExists(vpcName, extra2SubnetName), false)

			ginkgo.By("Phase 1: Disable EnableExternal → default LRP removed")
			patchVPCExternal(vpcClient, vpcName, false, nil)
			waitLRPAbsent(vpcName, extDefaultSubnet)

			ginkgo.By("Phase 1: Re-enable EnableExternal → default LRP re-created with chassis")
			patchVPCExternal(vpcClient, vpcName, true, nil)
			waitLRPPresent(vpcName, extDefaultSubnet)
			waitLRPChassisCount(vpcName, extDefaultSubnet, 1)

			// ------------------------------------------------------------------
			// Phase 2: Switch to extra1 (default LRP must be removed)
			// ------------------------------------------------------------------
			ginkgo.By("Phase 2: Set ExtraExternalSubnets=[extra1] → extra1 LRP created, default LRP removed")
			patchVPCExternal(vpcClient, vpcName, true, []string{extra1SubnetName})
			waitLRPPresent(vpcName, extra1SubnetName)
			waitLRPAbsent(vpcName, extDefaultSubnet)
			waitLRPChassisCount(vpcName, extra1SubnetName, 1)

			// ------------------------------------------------------------------
			// Phase 3: Add extra2 (both extra LRPs active)
			// ------------------------------------------------------------------
			ginkgo.By("Phase 3: Add extra2 to ExtraExternalSubnets → both extra LRPs active with chassis")
			patchVPCExternal(vpcClient, vpcName, true, []string{extra1SubnetName, extra2SubnetName})
			waitLRPPresent(vpcName, extra2SubnetName)
			waitLRPChassisCount(vpcName, extra2SubnetName, 1)
			framework.ExpectEqual(lrpExists(vpcName, extra1SubnetName), true)

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
			waitLRPChassisCount(vpcName, extra1SubnetName, 1)
			waitLRPChassisCount(vpcName, extra2SubnetName, 1)

			// ------------------------------------------------------------------
			// Phase 5: Remove extra1 (only extra2 remains)
			// ------------------------------------------------------------------
			ginkgo.By("Phase 5: Remove extra1 from ExtraExternalSubnets → extra1 LRP removed, extra2 stays")
			patchVPCExternal(vpcClient, vpcName, true, []string{extra2SubnetName})
			waitLRPAbsent(vpcName, extra1SubnetName)
			framework.ExpectEqual(lrpExists(vpcName, extra2SubnetName), true)

			// ------------------------------------------------------------------
			// Phase 6: Clear ExtraExternalSubnets → back to default subnet
			// ------------------------------------------------------------------
			ginkgo.By("Phase 6: Clear ExtraExternalSubnets → extra2 LRP removed, default LRP re-created with chassis")
			patchVPCExternal(vpcClient, vpcName, true, nil)
			waitLRPAbsent(vpcName, extra2SubnetName)
			waitLRPPresent(vpcName, extDefaultSubnet)
			waitLRPChassisCount(vpcName, extDefaultSubnet, 1)
		})
	})
})
