package ovn_eip

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	dockernetwork "github.com/moby/moby/api/types/network"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	"github.com/onsi/ginkgo/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iproute"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

const dockerNetworkName = "kube-ovn-vlan"

const dockerExtraNetworkName = "kube-ovn-extra-vlan"

func makeProviderNetwork(providerNetworkName string, exchangeLinkName bool, linkMap map[string]*iproute.Link) *kubeovnv1.ProviderNetwork {
	var defaultInterface string
	customInterfaces := make(map[string][]string, 0)
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

	return framework.MakeProviderNetwork(providerNetworkName, exchangeLinkName, defaultInterface, customInterfaces, nil)
}

func makeOvnEip(name, subnet, v4ip, v6ip, mac, usage string) *kubeovnv1.OvnEip {
	return framework.MakeOvnEip(name, subnet, v4ip, v6ip, mac, usage)
}

func makeOvnVip(namespaceName, name, subnet, v4ip, v6ip, vipType string) *kubeovnv1.Vip {
	return framework.MakeVip(namespaceName, name, subnet, v4ip, v6ip, vipType)
}

func makeOvnFip(name, ovnEip, ipType, ipName, vpc, v4Ip string) *kubeovnv1.OvnFip {
	return framework.MakeOvnFip(name, ovnEip, ipType, ipName, vpc, v4Ip)
}

func makeOvnSnat(name, ovnEip, vpcSubnet, ipName, vpc, v4IpCidr string) *kubeovnv1.OvnSnatRule {
	return framework.MakeOvnSnatRule(name, ovnEip, vpcSubnet, ipName, vpc, v4IpCidr)
}

func makeOvnDnat(name, ovnEip, ipType, ipName, vpc, v4Ip, internalPort, externalPort, protocol string) *kubeovnv1.OvnDnatRule {
	return framework.MakeOvnDnatRule(name, ovnEip, ipType, ipName, vpc, v4Ip, internalPort, externalPort, protocol)
}

var _ = framework.Describe("[group:ovn-vpc-nat-gw]", func() {
	f := framework.NewDefaultFramework("ovn-vpc-nat-gw")

	var skip bool
	var itFn func(bool, string, map[string]*iproute.Link, *[]string)
	var cs clientset.Interface
	var dockerNetwork, dockerExtraNetwork *dockernetwork.Inspect
	var nodeNames, gwNodeNames, providerBridgeIps, extraProviderBridgeIps []string
	var clusterName, providerNetworkName, vlanName, underlaySubnetName, noBfdVpcName, bfdVpcName, noBfdSubnetName, bfdSubnetName string
	var providerExtraNetworkName, vlanExtraName, underlayExtraSubnetName, noBfdExtraSubnetName string
	var linkMap, extraLinkMap map[string]*iproute.Link
	var providerNetworkClient *framework.ProviderNetworkClient
	var vlanClient *framework.VlanClient
	var vpcClient *framework.VpcClient
	var subnetClient *framework.SubnetClient
	var ovnEipClient *framework.OvnEipClient
	var ipClient *framework.IPClient
	var vipClient *framework.VipClient
	var ovnFipClient *framework.OvnFipClient
	var ovnSnatRuleClient *framework.OvnSnatRuleClient
	var ovnDnatRuleClient *framework.OvnDnatRuleClient
	var podClient *framework.PodClient
	var countingEipName, lrpEipSnatName, lrpExtraEipSnatName string
	var fipName string
	var ipDnatVipName, ipDnatEipName, ipDnatName string
	var ipFipVipName, ipFipEipName, ipFipName string
	var cidrSnatEipName, cidrSnatName, ipSnatVipName, ipSnatEipName, ipSnatName string

	var namespaceName string

	var sharedVipName, sharedEipDnatName, sharedEipFipShoudOkName, sharedEipFipShoudFailName string
	var fipPodName, podEipName, podFipName string
	var fipExtraPodName, podExtraEipName, podExtraFipName string

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		subnetClient = f.SubnetClient()
		vlanClient = f.VlanClient()
		vpcClient = f.VpcClient()
		providerNetworkClient = f.ProviderNetworkClient()
		ovnEipClient = f.OvnEipClient()
		ipClient = f.IPClient()
		vipClient = f.VipClient()
		ovnFipClient = f.OvnFipClient()
		ovnSnatRuleClient = f.OvnSnatRuleClient()
		ovnDnatRuleClient = f.OvnDnatRuleClient()

		podClient = f.PodClient()

		namespaceName = f.Namespace.Name

		gwNodeNum := 2
		// gw node is 2 means e2e HA cluster will have 2 gw nodes and a worker node
		// in this env, tcpdump gw nat flows will be more clear

		noBfdVpcName = "no-bfd-vpc-" + framework.RandomSuffix()
		bfdVpcName = "bfd-vpc-" + framework.RandomSuffix()

		// nats use ip crd name or vip crd
		fipName = "fip-" + framework.RandomSuffix()

		countingEipName = "counting-eip-" + framework.RandomSuffix()
		noBfdSubnetName = "no-bfd-subnet-" + framework.RandomSuffix()
		noBfdExtraSubnetName = "no-bfd-extra-subnet-" + framework.RandomSuffix()
		lrpEipSnatName = "lrp-eip-snat-" + framework.RandomSuffix()
		lrpExtraEipSnatName = "lrp-extra-eip-snat-" + framework.RandomSuffix()
		bfdSubnetName = "bfd-subnet-" + framework.RandomSuffix()
		providerNetworkName = "external"
		providerExtraNetworkName = "extra"
		vlanName = "vlan-" + framework.RandomSuffix()
		vlanExtraName = "vlan-extra-" + framework.RandomSuffix()
		underlaySubnetName = "external"
		underlayExtraSubnetName = "extra"

		// sharing case
		sharedVipName = "shared-vip-" + framework.RandomSuffix()
		sharedEipDnatName = "shared-eip-dnat-" + framework.RandomSuffix()
		sharedEipFipShoudOkName = "shared-eip-fip-should-ok-" + framework.RandomSuffix()
		sharedEipFipShoudFailName = "shared-eip-fip-should-fail-" + framework.RandomSuffix()

		// pod with fip
		fipPodName = "fip-pod-" + framework.RandomSuffix()
		podEipName = fipPodName
		podFipName = fipPodName

		// pod with fip for extra external subnet
		fipExtraPodName = "fip-extra-pod-" + framework.RandomSuffix()
		podExtraEipName = fipExtraPodName
		podExtraFipName = fipExtraPodName

		// fip use ip addr
		ipFipVipName = "ip-fip-vip-" + framework.RandomSuffix()
		ipFipEipName = "ip-fip-eip-" + framework.RandomSuffix()
		ipFipName = "ip-fip-" + framework.RandomSuffix()

		// dnat use ip addr
		ipDnatVipName = "ip-dnat-vip-" + framework.RandomSuffix()
		ipDnatEipName = "ip-dnat-eip-" + framework.RandomSuffix()
		ipDnatName = "ip-dnat-" + framework.RandomSuffix()

		// snat use ip cidr
		cidrSnatEipName = "cidr-snat-eip-" + framework.RandomSuffix()
		cidrSnatName = "cidr-snat-" + framework.RandomSuffix()
		ipSnatVipName = "ip-snat-vip-" + framework.RandomSuffix()
		ipSnatEipName = "ip-snat-eip-" + framework.RandomSuffix()
		ipSnatName = "ip-snat-" + framework.RandomSuffix()

		if skip {
			ginkgo.Skip("underlay spec only runs on kind clusters")
		}
		f.SkipVersionPriorTo(1, 15, "Skip e2e tests for Kube-OVN versions prior to 1.15 temporarily")

		if clusterName == "" {
			ginkgo.By("Getting k8s nodes")
			k8sNodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
			framework.ExpectNoError(err)

			cluster, ok := kind.IsKindProvided(k8sNodes.Items[0].Spec.ProviderID)
			if !ok {
				skip = true
				ginkgo.Skip("underlay spec only runs on kind clusters")
			}
			clusterName = cluster
		}

		if dockerNetwork == nil {
			ginkgo.By("Ensuring docker network " + dockerNetworkName + " exists")
			network, err := docker.NetworkCreate(dockerNetworkName, true, true)
			framework.ExpectNoError(err, "creating docker network "+dockerNetworkName)
			dockerNetwork = network
		}

		if dockerExtraNetwork == nil {
			ginkgo.By("Ensuring extra docker network " + dockerExtraNetworkName + " exists")
			network, err := docker.NetworkCreate(dockerExtraNetworkName, true, true)
			framework.ExpectNoError(err, "creating extra docker network "+dockerExtraNetworkName)
			dockerExtraNetwork = network
		}

		ginkgo.By("Getting kind nodes")
		nodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")
		framework.ExpectNotEmpty(nodes)

		ginkgo.By("Connecting nodes to the docker network")
		err = kind.NetworkConnect(dockerNetwork.ID, nodes)
		framework.ExpectNoError(err, "connecting nodes to network "+dockerNetworkName)

		ginkgo.By("Connecting nodes to the extra docker network")
		err = kind.NetworkConnect(dockerExtraNetwork.ID, nodes)
		framework.ExpectNoError(err, "connecting nodes to extra network "+dockerExtraNetworkName)

		ginkgo.By("Getting node links that belong to the docker network")
		nodes, err = kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")

		linkMap = make(map[string]*iproute.Link, len(nodes))
		extraLinkMap = make(map[string]*iproute.Link, len(nodes))
		nodeNames = make([]string, 0, len(nodes))
		gwNodeNames = make([]string, 0, gwNodeNum)
		providerBridgeIps = make([]string, 0, len(nodes))
		extraProviderBridgeIps = make([]string, 0, len(nodes))

		// node ext gw ovn eip name is the same as node name in this scenario
		for index, node := range nodes {
			links, err := node.ListLinks()
			framework.ExpectNoError(err, "failed to list links on node %s: %v", node.Name(), err)
			for _, link := range links {
				if link.Address == node.NetworkSettings.Networks[dockerNetworkName].MacAddress.String() {
					linkMap[node.ID] = &link
					break
				}
			}
			for _, link := range links {
				if link.Address == node.NetworkSettings.Networks[dockerExtraNetworkName].MacAddress.String() {
					extraLinkMap[node.ID] = &link
					break
				}
			}
			framework.ExpectHaveKey(linkMap, node.ID)
			framework.ExpectHaveKey(extraLinkMap, node.ID)
			linkMap[node.Name()] = linkMap[node.ID]
			extraLinkMap[node.Name()] = extraLinkMap[node.ID]
			nodeNames = append(nodeNames, node.Name())
			if index < gwNodeNum {
				gwNodeNames = append(gwNodeNames, node.Name())
			}
		}

		itFn = func(exchangeLinkName bool, providerNetworkName string, linkMap map[string]*iproute.Link, bridgeIps *[]string) {
			ginkgo.GinkgoHelper()

			ginkgo.By("Creating provider network " + providerNetworkName)
			pn := makeProviderNetwork(providerNetworkName, exchangeLinkName, linkMap)
			pn = providerNetworkClient.CreateSync(pn)

			ginkgo.By("Getting k8s nodes")
			k8sNodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
			framework.ExpectNoError(err)

			ginkgo.By("Validating node labels")
			for _, node := range k8sNodes.Items {
				link := linkMap[node.Name]
				framework.ExpectHaveKeyWithValue(node.Labels, fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, providerNetworkName), link.IfName)
				framework.ExpectHaveKeyWithValue(node.Labels, fmt.Sprintf(util.ProviderNetworkReadyTemplate, providerNetworkName), "true")
				framework.ExpectHaveKeyWithValue(node.Labels, fmt.Sprintf(util.ProviderNetworkMtuTemplate, providerNetworkName), strconv.Itoa(link.Mtu))
				framework.ExpectNotHaveKey(node.Labels, fmt.Sprintf(util.ProviderNetworkExcludeTemplate, providerNetworkName))
			}

			ginkgo.By("Validating provider network spec")
			framework.ExpectEqual(pn.Spec.ExchangeLinkName, false, "field .spec.exchangeLinkName should be false")

			ginkgo.By("Validating provider network status")
			framework.ExpectEqual(pn.Status.Ready, true, "field .status.ready should be true")
			framework.ExpectConsistOf(pn.Status.ReadyNodes, nodeNames)
			framework.ExpectEmpty(pn.Status.Vlans)

			ginkgo.By("Getting kind nodes")
			kindNodes, err := kind.ListNodes(clusterName, "")
			framework.ExpectNoError(err)

			ginkgo.By("Validating node links")
			linkNameMap := make(map[string]string, len(kindNodes))
			bridgeName := util.ExternalBridgeName(providerNetworkName)
			for _, node := range kindNodes {
				if exchangeLinkName {
					bridgeName = linkMap[node.ID].IfName
				}

				links, err := node.ListLinks()
				framework.ExpectNoError(err, "failed to list links on node %s: %v", node.Name(), err)

				var port, bridge *iproute.Link
				for i, link := range links {
					if link.IfIndex == linkMap[node.ID].IfIndex {
						port = &links[i]
					} else if link.IfName == bridgeName {
						bridge = &links[i]
						for _, addr := range bridge.NonLinkLocalAddresses() {
							if util.CheckProtocol(addr) == kubeovnv1.ProtocolIPv4 {
								ginkgo.By("get provider bridge v4 ip " + addr)
								*bridgeIps = append(*bridgeIps, addr)
							}
						}
					}
					if port != nil && bridge != nil {
						break
					}
				}
				framework.ExpectNotNil(port)
				framework.ExpectEqual(port.Address, linkMap[node.ID].Address)
				framework.ExpectEqual(port.Mtu, linkMap[node.ID].Mtu)
				framework.ExpectEqual(port.Master, "ovs-system")
				framework.ExpectEqual(port.OperState, "UP")
				if exchangeLinkName {
					framework.ExpectEqual(port.IfName, util.ExternalBridgeName(providerNetworkName))
				}

				framework.ExpectNotNil(bridge)
				framework.ExpectEqual(bridge.LinkInfo.InfoKind, "openvswitch")
				framework.ExpectEqual(bridge.Address, port.Address)
				framework.ExpectEqual(bridge.Mtu, port.Mtu)
				framework.ExpectEqual(bridge.OperState, "UNKNOWN")
				framework.ExpectContainElement(bridge.Flags, "UP")

				framework.ExpectEmpty(port.NonLinkLocalAddresses())
				framework.ExpectConsistOf(bridge.NonLinkLocalAddresses(), linkMap[node.ID].NonLinkLocalAddresses())

				linkNameMap[node.ID] = port.IfName
			}
		}
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting ovn fip " + fipName)
		ovnFipClient.DeleteSync(fipName)
		// clean up share eip case resource
		ginkgo.By("Deleting share ovn dnat " + sharedEipDnatName)
		ovnDnatRuleClient.DeleteSync(sharedEipDnatName)
		ginkgo.By("Deleting share ovn fip " + sharedEipFipShoudOkName)
		ovnFipClient.DeleteSync(sharedEipFipShoudOkName)
		ginkgo.By("Deleting share ovn fip " + sharedEipFipShoudFailName)
		ovnFipClient.DeleteSync(sharedEipFipShoudFailName)
		ginkgo.By("Deleting share ovn snat " + lrpEipSnatName)
		ovnSnatRuleClient.DeleteSync(lrpEipSnatName)
		ginkgo.By("Deleting share ovn snat " + lrpExtraEipSnatName)
		ovnSnatRuleClient.DeleteSync(lrpExtraEipSnatName)

		// clean up nats with ip or ip cidr
		ginkgo.By("Deleting ovn dnat " + ipDnatName)
		ovnDnatRuleClient.DeleteSync(ipDnatName)
		ginkgo.By("Deleting ovn snat " + ipSnatName)
		ovnSnatRuleClient.DeleteSync(ipSnatName)
		ginkgo.By("Deleting ovn fip " + ipFipName)
		ovnFipClient.DeleteSync(ipFipName)
		ginkgo.By("Deleting ovn snat " + cidrSnatName)
		ovnSnatRuleClient.DeleteSync(cidrSnatName)

		ginkgo.By("Deleting ovn eip " + ipFipEipName)
		ovnFipClient.DeleteSync(ipFipEipName)
		ginkgo.By("Deleting ovn eip " + ipDnatEipName)
		ovnEipClient.DeleteSync(ipDnatEipName)
		ginkgo.By("Deleting ovn eip " + ipSnatEipName)
		ovnEipClient.DeleteSync(ipSnatEipName)
		ginkgo.By("Deleting ovn eip " + cidrSnatEipName)
		ovnEipClient.DeleteSync(cidrSnatEipName)
		ginkgo.By("Deleting ovn eip " + ipFipEipName)
		ovnEipClient.DeleteSync(ipFipEipName)

		ginkgo.By("Deleting ovn vip " + ipFipVipName)
		vipClient.DeleteSync(ipFipVipName)
		ginkgo.By("Deleting ovn vip " + ipDnatVipName)
		vipClient.DeleteSync(ipDnatVipName)
		ginkgo.By("Deleting ovn vip " + ipSnatVipName)
		vipClient.DeleteSync(ipSnatVipName)

		ginkgo.By("Deleting ovn share vip " + sharedVipName)
		vipClient.DeleteSync(sharedVipName)

		// clean fip pod
		ginkgo.By("Deleting pod fip " + podFipName)
		ovnFipClient.DeleteSync(podFipName)
		ginkgo.By("Deleting pod with fip " + fipPodName)
		podClient.DeleteSync(fipPodName)
		ginkgo.By("Deleting pod eip " + podEipName)
		ovnEipClient.DeleteSync(podEipName)

		// clean fip extra pod
		ginkgo.By("Deleting pod fip " + podExtraFipName)
		ovnFipClient.DeleteSync(podExtraFipName)
		ginkgo.By("Deleting pod with fip " + fipExtraPodName)
		podClient.DeleteSync(fipExtraPodName)
		ginkgo.By("Deleting pod eip " + podExtraEipName)
		ovnEipClient.DeleteSync(podExtraEipName)

		ginkgo.By("Deleting subnet " + noBfdSubnetName)
		subnetClient.DeleteSync(noBfdSubnetName)
		ginkgo.By("Deleting subnet " + noBfdExtraSubnetName)
		subnetClient.DeleteSync(noBfdExtraSubnetName)
		ginkgo.By("Deleting subnet " + bfdSubnetName)
		subnetClient.DeleteSync(bfdSubnetName)

		ginkgo.By("Deleting no bfd custom vpc " + noBfdVpcName)
		vpcClient.DeleteSync(noBfdVpcName)
		ginkgo.By("Deleting bfd custom vpc " + bfdVpcName)
		vpcClient.DeleteSync(bfdVpcName)

		ginkgo.By("Deleting underlay vlan subnet")
		time.Sleep(1 * time.Second)
		// wait 1s to make sure webhook allow delete subnet
		ginkgo.By("Deleting underlay subnet " + underlaySubnetName)
		subnetClient.DeleteSync(underlaySubnetName)
		ginkgo.By("Deleting extra underlay subnet " + underlayExtraSubnetName)
		subnetClient.DeleteSync(underlayExtraSubnetName)

		ginkgo.By("Deleting vlan " + vlanName)
		vlanClient.Delete(vlanName)
		ginkgo.By("Deleting extra vlan " + vlanExtraName)
		vlanClient.Delete(vlanExtraName)

		ginkgo.By("Deleting provider network " + providerNetworkName)
		providerNetworkClient.DeleteSync(providerNetworkName)

		ginkgo.By("Deleting provider extra network " + providerExtraNetworkName)
		providerNetworkClient.DeleteSync(providerExtraNetworkName)

		ginkgo.By("Getting nodes")
		nodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in cluster")

		ginkgo.By("Waiting for ovs bridge to disappear")
		deadline := time.Now().Add(time.Minute)
		for _, node := range nodes {
			err = node.WaitLinkToDisappear(util.ExternalBridgeName(providerNetworkName), 2*time.Second, deadline)
			framework.ExpectNoError(err, "timed out waiting for ovs bridge to disappear in node %s", node.Name())
		}

		if dockerNetwork != nil {
			ginkgo.By("Disconnecting nodes from the docker network")
			err = kind.NetworkDisconnect(dockerNetwork.ID, nodes)
			framework.ExpectNoError(err, "disconnecting nodes from network "+dockerNetworkName)
		}

		if dockerExtraNetwork != nil {
			ginkgo.By("Disconnecting nodes from the docker extra network")
			err = kind.NetworkDisconnect(dockerExtraNetwork.ID, nodes)
			framework.ExpectNoError(err, "disconnecting nodes from extra network "+dockerExtraNetworkName)
		}
	})

	framework.ConformanceIt("Test ovn eip fip snat dnat", func() {
		f.SkipVersionPriorTo(1, 13, "This feature was introduced in v1.13")
		ginkgo.By("Getting docker network " + dockerNetworkName)
		network, err := docker.NetworkInspect(dockerNetworkName)
		framework.ExpectNoError(err, "getting docker network "+dockerNetworkName)

		exchangeLinkName := false
		itFn(exchangeLinkName, providerNetworkName, linkMap, &providerBridgeIps)

		ginkgo.By("Creating underlay vlan " + vlanName)
		vlan := framework.MakeVlan(vlanName, providerNetworkName, 0)
		_ = vlanClient.Create(vlan)

		ginkgo.By("Creating underlay subnet " + underlaySubnetName)
		var cidrV4, cidrV6, gatewayV4, gatewayV6 string
		for _, config := range dockerNetwork.IPAM.Config {
			switch util.CheckProtocol(config.Subnet.String()) {
			case kubeovnv1.ProtocolIPv4:
				if f.HasIPv4() {
					cidrV4 = config.Subnet.String()
					gatewayV4 = config.Gateway.String()
				}
			case kubeovnv1.ProtocolIPv6:
				if f.HasIPv6() {
					cidrV6 = config.Subnet.String()
					gatewayV6 = config.Gateway.String()
				}
			}
		}
		cidr := make([]string, 0, 2)
		gateway := make([]string, 0, 2)
		if f.HasIPv4() {
			cidr = append(cidr, cidrV4)
			gateway = append(gateway, gatewayV4)
		}
		if f.HasIPv6() {
			cidr = append(cidr, cidrV6)
			gateway = append(gateway, gatewayV6)
		}
		excludeIPs := make([]string, 0, len(network.Containers)*2)
		for _, container := range network.Containers {
			if container.IPv4Address.IsValid() && f.HasIPv4() {
				excludeIPs = append(excludeIPs, container.IPv4Address.Addr().String())
			}
			if container.IPv6Address.IsValid() && f.HasIPv6() {
				excludeIPs = append(excludeIPs, container.IPv6Address.Addr().String())
			}
		}
		vlanSubnetCidr := strings.Join(cidr, ",")
		vlanSubnetGw := strings.Join(gateway, ",")
		underlaySubnet := framework.MakeSubnet(underlaySubnetName, vlanName, vlanSubnetCidr, vlanSubnetGw, "", "", excludeIPs, nil, nil)
		oldUnderlayExternalSubnet := subnetClient.CreateSync(underlaySubnet)
		countingEip := makeOvnEip(countingEipName, underlaySubnetName, "", "", "", "")
		_ = ovnEipClient.CreateSync(countingEip)
		ginkgo.By("Checking underlay vlan " + oldUnderlayExternalSubnet.Name)
		framework.ExpectEqual(oldUnderlayExternalSubnet.Spec.Vlan, vlanName)
		framework.ExpectNotEqual(oldUnderlayExternalSubnet.Spec.CIDRBlock, "")
		time.Sleep(3 * time.Second)
		newUnerlayExternalSubnet := subnetClient.Get(underlaySubnetName)
		ginkgo.By("Check status using ovn eip for subnet " + underlaySubnetName)
		if newUnerlayExternalSubnet.Spec.Protocol == kubeovnv1.ProtocolIPv4 {
			framework.ExpectEqual(oldUnderlayExternalSubnet.Status.V4AvailableIPs-1, newUnerlayExternalSubnet.Status.V4AvailableIPs)
			framework.ExpectEqual(oldUnderlayExternalSubnet.Status.V4UsingIPs+1, newUnerlayExternalSubnet.Status.V4UsingIPs)
			framework.ExpectNotEqual(oldUnderlayExternalSubnet.Status.V4AvailableIPRange, newUnerlayExternalSubnet.Status.V4AvailableIPRange)
			framework.ExpectNotEqual(oldUnderlayExternalSubnet.Status.V4UsingIPRange, newUnerlayExternalSubnet.Status.V4UsingIPRange)
		} else {
			framework.ExpectEqual(oldUnderlayExternalSubnet.Status.V6AvailableIPs-1, newUnerlayExternalSubnet.Status.V6AvailableIPs)
			framework.ExpectEqual(oldUnderlayExternalSubnet.Status.V6UsingIPs+1, newUnerlayExternalSubnet.Status.V6UsingIPs)
			framework.ExpectNotEqual(oldUnderlayExternalSubnet.Status.V6AvailableIPRange, newUnerlayExternalSubnet.Status.V6AvailableIPRange)
			framework.ExpectNotEqual(oldUnderlayExternalSubnet.Status.V6UsingIPRange, newUnerlayExternalSubnet.Status.V6UsingIPRange)
		}
		// delete counting eip
		oldUnderlayExternalSubnet = newUnerlayExternalSubnet
		ovnEipClient.DeleteSync(countingEipName)
		time.Sleep(3 * time.Second)
		newUnerlayExternalSubnet = subnetClient.Get(underlaySubnetName)
		if newUnerlayExternalSubnet.Spec.Protocol == kubeovnv1.ProtocolIPv4 {
			framework.ExpectEqual(oldUnderlayExternalSubnet.Status.V4AvailableIPs+1, newUnerlayExternalSubnet.Status.V4AvailableIPs)
			framework.ExpectEqual(oldUnderlayExternalSubnet.Status.V4UsingIPs-1, newUnerlayExternalSubnet.Status.V4UsingIPs)
			framework.ExpectNotEqual(oldUnderlayExternalSubnet.Status.V4AvailableIPRange, newUnerlayExternalSubnet.Status.V4AvailableIPRange)
			framework.ExpectNotEqual(oldUnderlayExternalSubnet.Status.V4UsingIPRange, newUnerlayExternalSubnet.Status.V4UsingIPRange)
		} else {
			framework.ExpectEqual(oldUnderlayExternalSubnet.Status.V6AvailableIPs+1, newUnerlayExternalSubnet.Status.V6AvailableIPs)
			framework.ExpectEqual(oldUnderlayExternalSubnet.Status.V6UsingIPs-1, newUnerlayExternalSubnet.Status.V6UsingIPs)
			framework.ExpectNotEqual(oldUnderlayExternalSubnet.Status.V6AvailableIPRange, newUnerlayExternalSubnet.Status.V6AvailableIPRange)
			framework.ExpectNotEqual(oldUnderlayExternalSubnet.Status.V6UsingIPRange, newUnerlayExternalSubnet.Status.V6UsingIPRange)
		}

		externalGwNodes := strings.Join(gwNodeNames, ",")
		ginkgo.By("Creating config map ovn-external-gw-config for centralized case")
		cmData := map[string]string{
			"enable-external-gw": "true",
			"external-gw-nodes":  externalGwNodes,
			"type":               kubeovnv1.GWCentralizedType,
			"external-gw-nic":    "eth1",
			"external-gw-addr":   strings.Join(cidr, ","),
		}
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      util.ExternalGatewayConfig,
				Namespace: framework.KubeOvnNamespace,
			},
			Data: cmData,
		}
		_, err = cs.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Create(context.Background(), configMap, metav1.CreateOptions{})
		framework.ExpectNoError(err, "failed to create")

		ginkgo.By("1. Test custom vpc nats using centralized external gw")
		noBfdSubnetV4Cidr := "192.168.0.0/24"
		noBfdSubnetV4Gw := "192.168.0.1"
		enableExternal := true
		disableBfd := false
		noBfdVpc := framework.MakeVpc(noBfdVpcName, "", enableExternal, disableBfd, nil)
		_ = vpcClient.CreateSync(noBfdVpc)
		ginkgo.By("Creating overlay subnet " + noBfdSubnetName)
		noBfdSubnet := framework.MakeSubnet(noBfdSubnetName, "", noBfdSubnetV4Cidr, noBfdSubnetV4Gw, noBfdVpcName, util.OvnProvider, nil, nil, nil)
		_ = subnetClient.CreateSync(noBfdSubnet)
		ginkgo.By("Creating pod on nodes")
		for _, node := range nodeNames {
			// create pod on gw node and worker node
			podOnNodeName := "no-bfd-" + node
			ginkgo.By("Creating no bfd pod " + podOnNodeName + " with subnet " + noBfdSubnetName)
			annotations := map[string]string{util.LogicalSwitchAnnotation: noBfdSubnetName}
			cmd := []string{"sleep", "infinity"}
			pod := framework.MakePod(namespaceName, podOnNodeName, nil, annotations, f.KubeOVNImage, cmd, nil)
			pod.Spec.NodeName = node
			_ = podClient.CreateSync(pod)
		}

		ginkgo.By("Creating pod with fip")
		annotations := map[string]string{util.LogicalSwitchAnnotation: noBfdSubnetName}
		cmd := []string{"sleep", "infinity"}
		fipPod := framework.MakePod(namespaceName, fipPodName, nil, annotations, f.KubeOVNImage, cmd, nil)
		fipPod = podClient.CreateSync(fipPod)
		podEip := framework.MakeOvnEip(podEipName, underlaySubnetName, "", "", "", "")
		_ = ovnEipClient.CreateSync(podEip)
		fipPodIP := ovs.PodNameToPortName(fipPod.Name, fipPod.Namespace, noBfdSubnet.Spec.Provider)
		podFip := framework.MakeOvnFip(podFipName, podEipName, "", fipPodIP, "", "")
		podFip = ovnFipClient.CreateSync(podFip)

		ginkgo.By("1.1 Test fip dnat snat share eip by setting eip name and ip name")
		ginkgo.By("Create snat, dnat, fip with the same vpc lrp eip")
		noBfdlrpEipName := fmt.Sprintf("%s-%s", noBfdVpcName, underlaySubnetName)
		noBfdLrpEip := ovnEipClient.Get(noBfdlrpEipName)
		lrpEipSnat := framework.MakeOvnSnatRule(lrpEipSnatName, noBfdlrpEipName, noBfdSubnetName, "", "", "")
		_ = ovnSnatRuleClient.CreateSync(lrpEipSnat)
		ginkgo.By("Get lrp eip snat")
		lrpEipSnat = ovnSnatRuleClient.Get(lrpEipSnatName)
		ginkgo.By("Check share snat should has the external ip label")
		framework.ExpectHaveKeyWithValue(lrpEipSnat.Labels, util.EipV4IpLabel, noBfdLrpEip.Spec.V4Ip)

		ginkgo.By("Creating share vip")
		shareVip := framework.MakeVip(namespaceName, sharedVipName, noBfdSubnetName, "", "", "")
		_ = vipClient.CreateSync(shareVip)
		ginkgo.By("Creating the first ovn fip with share eip vip should be ok")
		shareFipShouldOk := framework.MakeOvnFip(sharedEipFipShoudOkName, noBfdlrpEipName, util.Vip, sharedVipName, "", "")
		_ = ovnFipClient.CreateSync(shareFipShouldOk)
		ginkgo.By("Creating the second ovn fip with share eip vip should be failed")
		shareFipShouldFail := framework.MakeOvnFip(sharedEipFipShoudFailName, noBfdlrpEipName, util.Vip, sharedVipName, "", "")
		_ = ovnFipClient.Create(shareFipShouldFail)
		ginkgo.By("Creating ovn dnat for dnat with share eip vip")
		shareDnat := framework.MakeOvnDnatRule(sharedEipDnatName, noBfdlrpEipName, util.Vip, sharedVipName, "", "", "80", "8080", "tcp")
		_ = ovnDnatRuleClient.CreateSync(shareDnat)

		ginkgo.By("Get shared lrp eip")
		noBfdLrpEip = ovnEipClient.Get(noBfdlrpEipName)
		ginkgo.By("Get share dnat")
		shareDnat = ovnDnatRuleClient.Get(sharedEipDnatName)

		ginkgo.By("Get share fip should ok")
		shareFipShouldOk = ovnFipClient.Get(sharedEipFipShoudOkName)
		ginkgo.By("Get share fip should fail")
		shareFipShouldFail = ovnFipClient.Get(sharedEipFipShoudFailName)
		// check
		ginkgo.By("Check share eip should has the external ip label")
		framework.ExpectHaveKeyWithValue(noBfdLrpEip.Labels, util.EipV4IpLabel, noBfdLrpEip.Spec.V4Ip)
		ginkgo.By("Check share dnat should has the external ip label")
		framework.ExpectHaveKeyWithValue(shareDnat.Labels, util.EipV4IpLabel, noBfdLrpEip.Spec.V4Ip)
		ginkgo.By("Check share fip should ok should has the external ip label")
		framework.ExpectHaveKeyWithValue(shareFipShouldOk.Labels, util.EipV4IpLabel, noBfdLrpEip.Spec.V4Ip)
		ginkgo.By("Check share fip should fail should not be ready")
		framework.ExpectEqual(shareFipShouldFail.Status.Ready, false)
		// make sure eip is shared
		nats := []string{util.DnatUsingEip, util.FipUsingEip, util.SnatUsingEip}
		framework.ExpectEqual(noBfdLrpEip.Status.Nat, strings.Join(nats, ","))
		// make sure vpc has normal external static routes
		noBfdVpc = vpcClient.Get(noBfdVpcName)
		for _, route := range noBfdVpc.Spec.StaticRoutes {
			framework.ExpectEqual(route.RouteTable, util.MainRouteTable)
			framework.ExpectEqual(route.Policy, kubeovnv1.PolicyDst)
			framework.ExpectContainSubstring(vlanSubnetGw, route.NextHopIP)
		}

		ginkgo.By("1.2 Test snat, fip external connectivity")
		for _, node := range nodeNames {
			// all the pods should ping lrp, node br-external ip successfully
			podOnNodeName := "no-bfd-" + node
			pod := podClient.GetPod(podOnNodeName)
			ginkgo.By("Test pod ping lrp eip " + noBfdlrpEipName)
			command := "ping -W 1 -c 1 " + noBfdLrpEip.Status.V4Ip
			stdOutput, errOutput, err := framework.ExecShellInPod(context.Background(), f, pod.Namespace, pod.Name, command)
			framework.Logf("output from exec on client pod %s dest lrp ip %s\n", pod.Name, noBfdLrpEip.Name)
			if stdOutput != "" && err == nil {
				framework.Logf("output:\n%s", stdOutput)
			}
			framework.Logf("exec %s failed err: %v, errOutput: %s, stdOutput: %s", command, err, errOutput, stdOutput)

			ginkgo.By("Test pod ping pod fip " + podFip.Status.V4Eip)
			command = "ping -W 1 -c 1 " + podFip.Status.V4Eip
			stdOutput, errOutput, err = framework.ExecShellInPod(context.Background(), f, pod.Namespace, pod.Name, command)
			framework.Logf("output from exec on client pod %s dst fip %s\n", pod.Name, noBfdLrpEip.Name)
			if stdOutput != "" && err == nil {
				framework.Logf("output:\n%s", stdOutput)
			}
			framework.Logf("exec %s failed err: %v, errOutput: %s, stdOutput: %s", command, err, errOutput, stdOutput)

			ginkgo.By("Test pod ping node provider bridge ip " + strings.Join(providerBridgeIps, ","))
			for _, ip := range providerBridgeIps {
				command := "ping -W 1 -c 1 " + ip
				stdOutput, errOutput, err = framework.ExecShellInPod(context.Background(), f, pod.Namespace, pod.Name, command)
				framework.Logf("output from exec on client pod %s dest node ip %s\n", pod.Name, ip)
				if stdOutput != "" && err == nil {
					framework.Logf("output:\n%s", stdOutput)
				}
			}
			framework.Logf("exec %s failed err: %v, errOutput: %s, stdOutput: %s", command, err, errOutput, stdOutput)
		}

		ginkgo.By("Getting docker extra network " + dockerExtraNetworkName)
		extraNetwork, err := docker.NetworkInspect(dockerExtraNetworkName)
		framework.ExpectNoError(err, "getting extra docker network "+dockerExtraNetworkName)
		itFn(exchangeLinkName, providerExtraNetworkName, extraLinkMap, &extraProviderBridgeIps)

		ginkgo.By("Creating underlay extra vlan " + vlanExtraName)
		vlan = framework.MakeVlan(vlanExtraName, providerExtraNetworkName, 0)
		_ = vlanClient.Create(vlan)

		ginkgo.By("Creating extra underlay subnet " + underlayExtraSubnetName)
		cidrV4, cidrV6, gatewayV4, gatewayV6 = "", "", "", ""
		for _, config := range dockerExtraNetwork.IPAM.Config {
			switch util.CheckProtocol(config.Subnet.String()) {
			case kubeovnv1.ProtocolIPv4:
				if f.HasIPv4() {
					cidrV4 = config.Subnet.String()
					gatewayV4 = config.Gateway.String()
				}
			case kubeovnv1.ProtocolIPv6:
				if f.HasIPv6() {
					cidrV6 = config.Subnet.String()
					gatewayV6 = config.Gateway.String()
				}
			}
		}
		cidr = make([]string, 0, 2)
		gateway = make([]string, 0, 2)
		if f.HasIPv4() {
			cidr = append(cidr, cidrV4)
			gateway = append(gateway, gatewayV4)
		}
		if f.HasIPv6() {
			cidr = append(cidr, cidrV6)
			gateway = append(gateway, gatewayV6)
		}

		extraExcludeIPs := make([]string, 0, len(extraNetwork.Containers)*2)
		for _, container := range extraNetwork.Containers {
			if container.IPv4Address.IsValid() && f.HasIPv4() {
				extraExcludeIPs = append(extraExcludeIPs, container.IPv4Address.Addr().String())
			}
			if container.IPv6Address.IsValid() && f.HasIPv6() {
				extraExcludeIPs = append(extraExcludeIPs, container.IPv6Address.Addr().String())
			}
		}
		extraVlanSubnetCidr := strings.Join(cidr, ",")
		extraVlanSubnetGw := strings.Join(gateway, ",")
		underlayExtraSubnet := framework.MakeSubnet(underlayExtraSubnetName, vlanExtraName, extraVlanSubnetCidr, extraVlanSubnetGw, "", "", extraExcludeIPs, nil, nil)
		_ = subnetClient.CreateSync(underlayExtraSubnet)
		vlanExtraSubnet := subnetClient.Get(underlayExtraSubnetName)
		ginkgo.By("Checking extra underlay vlan " + vlanExtraSubnet.Name)
		framework.ExpectEqual(vlanExtraSubnet.Spec.Vlan, vlanExtraName)
		framework.ExpectNotEqual(vlanExtraSubnet.Spec.CIDRBlock, "")

		ginkgo.By("1.3 Test custom vpc nats using extra centralized external gw")
		noBfdExtraSubnetV4Cidr := "192.168.3.0/24"
		noBfdExtraSubnetV4Gw := "192.168.3.1"

		cachedVpc := vpcClient.Get(noBfdVpcName)
		noBfdVpc = cachedVpc.DeepCopy()
		noBfdVpc.Spec.ExtraExternalSubnets = append(noBfdVpc.Spec.ExtraExternalSubnets, underlayExtraSubnetName)
		noBfdVpc.Spec.StaticRoutes = append(noBfdVpc.Spec.StaticRoutes, &kubeovnv1.StaticRoute{
			Policy:    kubeovnv1.PolicySrc,
			CIDR:      noBfdExtraSubnetV4Cidr,
			NextHopIP: gatewayV4,
		})
		_, err = vpcClient.Update(context.Background(), noBfdVpc, metav1.UpdateOptions{})
		framework.ExpectNoError(err)

		ginkgo.By("Creating overlay subnet " + noBfdExtraSubnetName)
		noBfdExtraSubnet := framework.MakeSubnet(noBfdExtraSubnetName, "", noBfdExtraSubnetV4Cidr, noBfdExtraSubnetV4Gw, noBfdVpcName, util.OvnProvider, nil, nil, nil)
		_ = subnetClient.CreateSync(noBfdExtraSubnet)

		ginkgo.By("Creating pod on nodes")
		for _, node := range nodeNames {
			// create pod on gw node and worker node
			podOnNodeName := "no-bfd-extra-" + node
			ginkgo.By("Creating no bfd extra pod " + podOnNodeName + " with subnet " + noBfdExtraSubnetName)
			annotations := map[string]string{util.LogicalSwitchAnnotation: noBfdExtraSubnetName}
			cmd := []string{"sleep", "infinity"}
			pod := framework.MakePod(namespaceName, podOnNodeName, nil, annotations, f.KubeOVNImage, cmd, nil)
			pod.Spec.NodeName = node
			_ = podClient.CreateSync(pod)
		}

		ginkgo.By("Creating pod with fip")
		annotations = map[string]string{util.LogicalSwitchAnnotation: noBfdExtraSubnetName}
		fipPod = framework.MakePod(namespaceName, fipExtraPodName, nil, annotations, f.KubeOVNImage, cmd, nil)
		fipPod = podClient.CreateSync(fipPod)
		podEip = framework.MakeOvnEip(podExtraEipName, underlayExtraSubnetName, "", "", "", "")
		_ = ovnEipClient.CreateSync(podEip)
		fipPodIP = ovs.PodNameToPortName(fipPod.Name, fipPod.Namespace, noBfdExtraSubnet.Spec.Provider)
		podFip = framework.MakeOvnFip(podExtraFipName, podExtraEipName, "", fipPodIP, "", "")
		podFip = ovnFipClient.CreateSync(podFip)

		ginkgo.By("Create snat, dnat, fip for extra centralized external gw")
		noBfdlrpEipName = fmt.Sprintf("%s-%s", noBfdVpcName, underlayExtraSubnetName)
		noBfdLrpEip = ovnEipClient.Get(noBfdlrpEipName)
		lrpEipSnat = framework.MakeOvnSnatRule(lrpExtraEipSnatName, noBfdlrpEipName, noBfdExtraSubnetName, "", "", "")
		_ = ovnSnatRuleClient.CreateSync(lrpEipSnat)
		ginkgo.By("Get lrp eip snat")
		lrpEipSnat = ovnSnatRuleClient.Get(lrpExtraEipSnatName)
		ginkgo.By("Check share snat should has the external ip label")
		framework.ExpectHaveKeyWithValue(lrpEipSnat.Labels, util.EipV4IpLabel, noBfdLrpEip.Spec.V4Ip)

		ginkgo.By("1.4 Test snat, fip extra external connectivity")
		for _, node := range nodeNames {
			// all the pods should ping lrp, node br-external ip successfully
			podOnNodeName := "no-bfd-extra-" + node
			pod := podClient.GetPod(podOnNodeName)
			ginkgo.By("Test pod ping lrp eip " + noBfdlrpEipName)
			command := "ping -W 1 -c 1 " + noBfdLrpEip.Status.V4Ip
			stdOutput, errOutput, err := framework.ExecShellInPod(context.Background(), f, pod.Namespace, pod.Name, command)
			framework.Logf("output from exec on client pod %s dest lrp ip %s\n", pod.Name, noBfdLrpEip.Name)
			if stdOutput != "" && err == nil {
				framework.Logf("output:\n%s", stdOutput)
			}
			framework.Logf("exec %s failed err: %v, errOutput: %s, stdOutput: %s", command, err, errOutput, stdOutput)

			ginkgo.By("Test pod ping pod fip " + podFip.Status.V4Eip)
			command = "ping -W 1 -c 1 " + podFip.Status.V4Eip
			stdOutput, errOutput, err = framework.ExecShellInPod(context.Background(), f, pod.Namespace, pod.Name, command)
			framework.Logf("output from exec on client pod %s dst fip %s\n", pod.Name, noBfdLrpEip.Name)
			if stdOutput != "" && err == nil {
				framework.Logf("output:\n%s", stdOutput)
			}
			framework.Logf("exec %s failed err: %v, errOutput: %s, stdOutput: %s", command, err, errOutput, stdOutput)

			ginkgo.By("Test pod ping node provider bridge ip " + strings.Join(extraProviderBridgeIps, ","))
			framework.Logf("Pod can not ping bridge ip through extra external subnet in Kind test")
			for _, ip := range extraProviderBridgeIps {
				command := "ping -W 1 -c 1 " + ip
				stdOutput, errOutput, err = framework.ExecShellInPod(context.Background(), f, pod.Namespace, pod.Name, command)
				framework.Logf("output from exec on client pod %s dest node ip %s\n", pod.Name, ip)
				if stdOutput != "" && err == nil {
					framework.Logf("output:\n%s", stdOutput)
				}
			}
			framework.Logf("exec %s failed err: %v, errOutput: %s, stdOutput: %s", command, err, errOutput, stdOutput)
		}

		// nat with ip crd name and share the same external eip tests all passed
		ginkgo.By("2. Test custom vpc with bfd route")
		ginkgo.By("2.1 Test custom vpc dnat, fip, snat in traditonal way")
		ginkgo.By("Create dnat, fip, snat with eip name and ip or ip cidr")

		for _, nodeName := range nodeNames {
			// in this case, each node has one ecmp bfd ovs lsp nic
			eipName := nodeName
			ginkgo.By("Creating ovn node-ext-gw type eip " + nodeName)
			eip := makeOvnEip(eipName, underlaySubnetName, "", "", "", util.OvnEipTypeLSP)
			_ = ovnEipClient.CreateSync(eip)
		}

		// TODO:// ipv6, dual stack support
		bfdSubnetV4Cidr := "192.168.1.0/24"
		bfdSubnetV4Gw := "192.168.1.1"
		enableBfd := true
		bfdVpc := framework.MakeVpc(bfdVpcName, "", enableExternal, enableBfd, nil)
		_ = vpcClient.CreateSync(bfdVpc)
		ginkgo.By("Creating overlay subnet enable ecmp")
		bfdSubnet := framework.MakeSubnet(bfdSubnetName, "", bfdSubnetV4Cidr, bfdSubnetV4Gw, bfdVpcName, util.OvnProvider, nil, nil, nil)
		bfdSubnet.Spec.EnableEcmp = true
		_ = subnetClient.CreateSync(bfdSubnet)

		// TODO:// support vip type allowed address pair while use security group

		ginkgo.By("Test ovn fip with eip name and ip")
		ginkgo.By("Creating ovn vip " + ipFipVipName)
		ipFipVip := makeOvnVip(namespaceName, ipFipVipName, bfdSubnetName, "", "", "")
		ipFipVip = vipClient.CreateSync(ipFipVip)
		framework.ExpectNotEmpty(ipFipVip.Status.V4ip)
		ginkgo.By("Creating ovn eip " + ipFipEipName)
		ipFipEip := makeOvnEip(ipFipEipName, underlaySubnetName, "", "", "", util.OvnEipTypeNAT)
		ipFipEip = ovnEipClient.CreateSync(ipFipEip)
		framework.ExpectNotEmpty(ipFipEip.Status.V4Ip)
		ginkgo.By("Creating ovn fip " + ipFipName)
		ipFip := makeOvnFip(fipName, ipFipEipName, "", "", bfdVpcName, ipFipVip.Status.V4ip)
		ipFip = ovnFipClient.CreateSync(ipFip)
		framework.ExpectEqual(ipFip.Status.V4Eip, ipFipEip.Status.V4Ip)
		framework.ExpectNotEmpty(ipFip.Status.V4Ip)

		ginkgo.By("Test ovn dnat with eip name and ip")
		ginkgo.By("Creating ovn vip " + ipDnatVipName)
		ipDnatVip := makeOvnVip(namespaceName, ipDnatVipName, bfdSubnetName, "", "", "")
		ipDnatVip = vipClient.CreateSync(ipDnatVip)
		framework.ExpectNotEmpty(ipDnatVip.Status.V4ip)
		ginkgo.By("Creating ovn eip " + ipDnatEipName)
		ipDnatEip := makeOvnEip(ipDnatEipName, underlaySubnetName, "", "", "", util.OvnEipTypeNAT)
		ipDnatEip = ovnEipClient.CreateSync(ipDnatEip)
		framework.ExpectNotEmpty(ipDnatEip.Status.V4Ip)
		ginkgo.By("Creating ovn dnat " + ipDnatName)
		ipDnat := makeOvnDnat(ipDnatName, ipDnatEipName, "", "", bfdVpcName, ipDnatVip.Status.V4ip, "80", "8080", "tcp")
		ipDnat = ovnDnatRuleClient.CreateSync(ipDnat)
		framework.ExpectEqual(ipDnat.Status.Vpc, bfdVpcName)
		framework.ExpectEqual(ipDnat.Status.V4Eip, ipDnatEip.Status.V4Ip)
		framework.ExpectEqual(ipDnat.Status.V4Ip, ipDnatVip.Status.V4ip)

		ginkgo.By("Test ovn snat with eip name and ip cidr")
		ginkgo.By("Creating ovn eip " + cidrSnatEipName)
		cidrSnatEip := makeOvnEip(cidrSnatEipName, underlaySubnetName, "", "", "", "")
		cidrSnatEip = ovnEipClient.CreateSync(cidrSnatEip)
		framework.ExpectNotEmpty(cidrSnatEip.Status.V4Ip)
		ginkgo.By("Creating ovn snat mapping with subnet cidr" + bfdSubnetV4Cidr)
		cidrSnat := makeOvnSnat(cidrSnatName, cidrSnatEipName, "", "", bfdVpcName, bfdSubnetV4Cidr)
		cidrSnat = ovnSnatRuleClient.CreateSync(cidrSnat)
		framework.ExpectEqual(cidrSnat.Status.Vpc, bfdVpcName)
		framework.ExpectEqual(cidrSnat.Status.V4Eip, cidrSnatEip.Status.V4Ip)
		framework.ExpectEqual(cidrSnat.Status.V4IpCidr, bfdSubnetV4Cidr)

		ginkgo.By("Test ovn snat with eip name and ip")
		ginkgo.By("Creating ovn vip " + ipSnatVipName)
		ipSnatVip := makeOvnVip(namespaceName, ipSnatVipName, bfdSubnetName, "", "", "")
		ipSnatVip = vipClient.CreateSync(ipSnatVip)
		framework.ExpectNotEmpty(ipSnatVip.Status.V4ip)
		ginkgo.By("Creating ovn eip " + ipSnatEipName)
		ipSnatEip := makeOvnEip(ipSnatEipName, underlaySubnetName, "", "", "", "")
		ipSnatEip = ovnEipClient.CreateSync(ipSnatEip)
		framework.ExpectNotEmpty(ipSnatEip.Status.V4Ip)
		ginkgo.By("Creating ovn snat " + ipSnatName)
		ipSnat := makeOvnSnat(ipSnatName, ipSnatEipName, "", "", bfdVpcName, ipSnatVip.Status.V4ip)
		ipSnat = ovnSnatRuleClient.CreateSync(ipSnat)
		framework.ExpectEqual(ipSnat.Status.Vpc, bfdVpcName)
		framework.ExpectEqual(ipSnat.Status.V4IpCidr, ipSnatVip.Status.V4ip)

		k8sNodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		for _, node := range k8sNodes.Items {
			// label should be true after setup node external gw
			framework.ExpectHaveKeyWithValue(node.Labels, util.NodeExtGwLabel, "true")
		}
		// make sure vpc has bfd external static routes
		bfdVpc = vpcClient.Get(bfdVpcName)
		for _, route := range bfdVpc.Spec.StaticRoutes {
			framework.ExpectEqual(route.RouteTable, util.MainRouteTable)
			framework.ExpectEqual(route.ECMPMode, util.StaticRouteBfdEcmp)
			framework.ExpectEqual(route.Policy, kubeovnv1.PolicySrc)
			framework.ExpectNotEmpty(route.CIDR)
		}

		for _, node := range nodeNames {
			podOnNodeName := "bfd-" + node
			ginkgo.By("Creating bfd pod " + podOnNodeName + " with subnet " + bfdSubnetName)
			annotations := map[string]string{util.LogicalSwitchAnnotation: bfdSubnetName}
			cmd := []string{"sleep", "infinity"}
			pod := framework.MakePod(namespaceName, podOnNodeName, nil, annotations, f.KubeOVNImage, cmd, nil)
			pod.Spec.NodeName = node
			_ = podClient.CreateSync(pod)
		}
		ginkgo.By("3. Updating config map ovn-external-gw-config for distributed case")
		cmData = map[string]string{
			"enable-external-gw": "true",
			"external-gw-nodes":  externalGwNodes,
			"type":               kubeovnv1.GWDistributedType,
			"external-gw-nic":    "eth1",
			"external-gw-addr":   strings.Join(cidr, ","),
		}
		// TODO:// external-gw-nodes could be auto managed by recognizing gw chassis node which has the external-gw-nic
		configMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      util.ExternalGatewayConfig,
				Namespace: framework.KubeOvnNamespace,
			},
			Data: cmData,
		}
		_, err = cs.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Update(context.Background(), configMap, metav1.UpdateOptions{})
		framework.ExpectNoError(err, "failed to update ConfigMap")

		ginkgo.By("Getting kind nodes")
		nodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")
		framework.ExpectNotEmpty(nodes)
		ginkgo.By("4. Creating crd in distributed case")
		for _, node := range nodeNames {
			podOnNodeName := "bfd-" + node
			eipOnNodeName := "eip-on-node-" + node
			fipOnNodeName := "fip-on-node-" + node
			ipName := ovs.PodNameToPortName(podOnNodeName, namespaceName, bfdSubnet.Spec.Provider)
			ginkgo.By("Get pod ip" + ipName)
			ip := ipClient.Get(ipName)
			ginkgo.By("Creating ovn eip " + eipOnNodeName)
			eipOnNode := makeOvnEip(eipOnNodeName, underlaySubnetName, "", "", "", "")
			_ = ovnEipClient.CreateSync(eipOnNode)
			ginkgo.By("Creating ovn fip " + fipOnNodeName)
			fip := makeOvnFip(fipOnNodeName, eipOnNodeName, "", ip.Name, "", "")
			_ = ovnFipClient.CreateSync(fip)
		}
		// wait here to have an insight into all the ovn nat resources
		ginkgo.By("5. Deleting pod")
		for _, node := range nodeNames {
			podOnNodeName := "bfd-" + node
			ginkgo.By("Deleting pod " + podOnNodeName)
			podClient.DeleteSync(podOnNodeName)
			podOnNodeName = "no-bfd-" + node
			ginkgo.By("Deleting pod " + podOnNodeName)
			podClient.DeleteSync(podOnNodeName)
			podOnNodeName = "no-bfd-extra-" + node
			ginkgo.By("Deleting pod " + podOnNodeName)
			podClient.DeleteSync(podOnNodeName)
		}

		ginkgo.By("6. Deleting crd in distributed case")
		for _, node := range nodeNames {
			ginkgo.By("Deleting node external gw ovn eip " + node)
			ovnEipClient.DeleteSync(node)
			podOnNodeName := "on-node-" + node
			eipOnNodeName := "eip-on-node-" + node
			fipOnNodeName := "fip-on-node-" + node
			ginkgo.By("Deleting node ovn fip " + fipOnNodeName)
			ovnFipClient.DeleteSync(fipOnNodeName)
			ginkgo.By("Deleting node ovn eip " + eipOnNodeName)
			ovnEipClient.DeleteSync(eipOnNodeName)
			ipName := ovs.PodNameToPortName(podOnNodeName, namespaceName, bfdSubnet.Spec.Provider)
			ginkgo.By("Deleting pod ip" + ipName)
			ipClient.DeleteSync(ipName)
		}

		ginkgo.By("Disable ovn eip snat external gateway")
		ginkgo.By("Deleting configmap")
		err = cs.CoreV1().ConfigMaps(configMap.Namespace).Delete(context.Background(), configMap.Name, metav1.DeleteOptions{})
		framework.ExpectNoError(err, "failed to delete ConfigMap")

		lrpEipName := fmt.Sprintf("%s-%s", bfdVpcName, underlaySubnetName)
		ginkgo.By("Deleting ovn eip " + lrpEipName)
		ovnEipClient.DeleteSync(lrpEipName)

		defaultVpcLrpEipName := fmt.Sprintf("%s-%s", util.DefaultVpc, underlaySubnetName)
		ginkgo.By("Deleting ovn eip " + defaultVpcLrpEipName)
		ovnEipClient.DeleteSync(defaultVpcLrpEipName)

		k8sNodes, err = e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		time.Sleep(5 * time.Second)
		for _, node := range k8sNodes.Items {
			// label should be false after remove node external gw
			framework.ExpectHaveKeyWithValue(node.Labels, util.NodeExtGwLabel, "false")
		}
	})
})

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
