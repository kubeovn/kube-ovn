package ovn_eip

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	dockertypes "github.com/docker/docker/api/types"
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

func makeOvnVip(name, subnet, v4ip, v6ip, vipType string) *kubeovnv1.Vip {
	return framework.MakeVip(name, subnet, v4ip, v6ip, vipType)
}

func makeOvnFip(name, ovnEip, ipType, ipName string) *kubeovnv1.OvnFip {
	return framework.MakeOvnFip(name, ovnEip, ipType, ipName)
}

func makeOvnSnat(name, ovnEip, vpcSubnet, ipName string) *kubeovnv1.OvnSnatRule {
	return framework.MakeOvnSnatRule(name, ovnEip, vpcSubnet, ipName)
}

func makeOvnDnat(name, ovnEip, ipType, ipName, internalPort, externalPort, protocol string) *kubeovnv1.OvnDnatRule {
	return framework.MakeOvnDnatRule(name, ovnEip, ipType, ipName, internalPort, externalPort, protocol)
}

var _ = framework.Describe("[group:ovn-vpc-nat-gw]", func() {
	f := framework.NewDefaultFramework("ovn-vpc-nat-gw")

	var skip bool
	var itFn func(bool)
	var cs clientset.Interface
	var nodeNames []string
	var clusterName, providerNetworkName, vlanName, underlaySubnetName, noBfdVpcName, bfdVpcName, noBfdSubnetName, bfdSubnetName string
	var linkMap map[string]*iproute.Link
	var providerNetworkClient *framework.ProviderNetworkClient
	var vlanClient *framework.VlanClient
	var vpcClient *framework.VpcClient
	var subnetClient *framework.SubnetClient
	var ovnEipClient *framework.OvnEipClient
	var fipVipName, fipEipName, fipName, dnatVipName, dnatEipName, dnatName, snatEipName, snatName, namespaceName string
	var ipClient *framework.IPClient
	var vipClient *framework.VipClient
	var ovnFipClient *framework.OvnFipClient
	var ovnSnatRuleClient *framework.OvnSnatRuleClient
	var ovnDnatRuleClient *framework.OvnDnatRuleClient
	var arpProxyVip1Name, arpProxyVip2Name string

	var podClient *framework.PodClient

	var dockerNetwork *dockertypes.NetworkResource
	var containerID string
	var image string

	var sharedVipName, sharedEipName, sharedEipDnatName, sharedEipSnatName, sharedEipFipShoudOkName, sharedEipFipShoudFailName string

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
		noBfdVpcName = "no-bfd-vpc-" + framework.RandomSuffix()
		bfdVpcName = "bfd-vpc-" + framework.RandomSuffix()

		// test arp proxy vip
		// should have the same mac, which is vpc overlay subnet gw mac
		arpProxyVip1Name = "arp-proxy-vip1-" + framework.RandomSuffix()
		arpProxyVip2Name = "arp-proxy-vip2-" + framework.RandomSuffix()

		// test allow address pair vip
		fipVipName = "fip-vip-" + framework.RandomSuffix()
		fipEipName = "fip-eip-" + framework.RandomSuffix()
		fipName = "fip-" + framework.RandomSuffix()

		dnatVipName = "dnat-vip-" + framework.RandomSuffix()
		dnatEipName = "dnat-eip-" + framework.RandomSuffix()
		dnatName = "dnat-" + framework.RandomSuffix()

		snatEipName = "snat-eip-" + framework.RandomSuffix()
		snatName = "snat-" + framework.RandomSuffix()
		noBfdSubnetName = "no-bfd-subnet-" + framework.RandomSuffix()
		bfdSubnetName = "bfd-subnet-" + framework.RandomSuffix()
		providerNetworkName = "external"
		vlanName = "vlan-" + framework.RandomSuffix()
		underlaySubnetName = "external"

		// sharing case
		sharedVipName = "shared-vip-" + framework.RandomSuffix()
		sharedEipName = "shared-eip-" + framework.RandomSuffix()
		sharedEipDnatName = "shared-eip-dnat-" + framework.RandomSuffix()
		sharedEipSnatName = "shared-eip-snat-" + framework.RandomSuffix()
		sharedEipFipShoudOkName = "shared-eip-fip-should-ok-" + framework.RandomSuffix()
		sharedEipFipShoudFailName = "shared-eip-fip-should-fail-" + framework.RandomSuffix()

		containerID = ""
		if image == "" {
			image = framework.GetKubeOvnImage(cs)
		}

		if skip {
			ginkgo.Skip("underlay spec only runs on kind clusters")
		}

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

		ginkgo.By("Getting kind nodes")
		nodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")
		framework.ExpectNotEmpty(nodes)

		ginkgo.By("Connecting nodes to the docker network")
		err = kind.NetworkConnect(dockerNetwork.ID, nodes)
		framework.ExpectNoError(err, "connecting nodes to network "+dockerNetworkName)

		ginkgo.By("Getting node links that belong to the docker network")
		nodes, err = kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")

		linkMap = make(map[string]*iproute.Link, len(nodes))
		nodeNames = make([]string, 0, len(nodes))
		// node ext gw ovn eip name is the same as node name in this scenario

		for _, node := range nodes {
			links, err := node.ListLinks()
			framework.ExpectNoError(err, "failed to list links on node %s: %v", node.Name(), err)

			for _, link := range links {
				if link.Address == node.NetworkSettings.Networks[dockerNetworkName].MacAddress {
					linkMap[node.ID] = &link
					break
				}
			}
			framework.ExpectHaveKey(linkMap, node.ID)
			linkMap[node.Name()] = linkMap[node.ID]
			nodeNames = append(nodeNames, node.Name())
		}

		itFn = func(exchangeLinkName bool) {
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
		if containerID != "" {
			ginkgo.By("Deleting container " + containerID)
			err := docker.ContainerRemove(containerID)
			framework.ExpectNoError(err)
		}

		ginkgo.By("Deleting ovn fip " + fipName)
		ovnFipClient.DeleteSync(fipName)
		ginkgo.By("Deleting ovn dnat " + dnatName)
		ovnDnatRuleClient.DeleteSync(dnatName)
		ginkgo.By("Deleting ovn snat " + snatName)
		ovnSnatRuleClient.DeleteSync(snatName)

		ginkgo.By("Deleting ovn fip " + fipEipName)
		ovnFipClient.DeleteSync(fipEipName)
		ginkgo.By("Deleting ovn eip " + dnatEipName)
		ovnEipClient.DeleteSync(dnatEipName)
		ginkgo.By("Deleting ovn eip " + snatEipName)
		ovnEipClient.DeleteSync(snatEipName)

		ginkgo.By("Deleting ovn arp proxy vip " + arpProxyVip1Name)
		vipClient.DeleteSync(arpProxyVip1Name)
		ginkgo.By("Deleting ovn arp proxy vip " + arpProxyVip2Name)

		// clean up share eip case resource
		ginkgo.By("Deleting share ovn dnat " + sharedEipDnatName)
		ovnDnatRuleClient.DeleteSync(sharedEipDnatName)
		ginkgo.By("Deleting share ovn fip " + sharedEipFipShoudOkName)
		ovnFipClient.DeleteSync(sharedEipFipShoudOkName)
		ginkgo.By("Deleting share ovn fip " + sharedEipFipShoudFailName)
		ovnFipClient.DeleteSync(sharedEipFipShoudFailName)
		ginkgo.By("Deleting share ovn snat " + sharedEipSnatName)
		ovnSnatRuleClient.DeleteSync(sharedEipSnatName)

		vipClient.DeleteSync(arpProxyVip2Name)
		ginkgo.By("Deleting ovn vip " + dnatVipName)
		vipClient.DeleteSync(dnatVipName)
		ginkgo.By("Deleting ovn vip " + fipVipName)
		vipClient.DeleteSync(fipVipName)
		ginkgo.By("Deleting ovn share vip " + sharedVipName)
		vipClient.DeleteSync(sharedVipName)

		ginkgo.By("Deleting subnet " + noBfdSubnetName)
		subnetClient.DeleteSync(noBfdSubnetName)
		ginkgo.By("Deleting subnet " + bfdSubnetName)
		subnetClient.DeleteSync(bfdSubnetName)
		ginkgo.By("Deleting underlay subnet " + underlaySubnetName)
		subnetClient.DeleteSync(underlaySubnetName)

		ginkgo.By("Deleting no bfd custom vpc " + noBfdVpcName)
		vpcClient.DeleteSync(noBfdVpcName)
		ginkgo.By("Deleting bfd custom vpc " + bfdVpcName)
		vpcClient.DeleteSync(bfdVpcName)

		ginkgo.By("Deleting vlan " + vlanName)
		vlanClient.Delete(vlanName, metav1.DeleteOptions{})

		ginkgo.By("Deleting provider network " + providerNetworkName)
		providerNetworkClient.DeleteSync(providerNetworkName)

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
	})

	framework.ConformanceIt("ovn eip fip snat dnat", func() {
		ginkgo.By("Getting docker network " + dockerNetworkName)
		network, err := docker.NetworkInspect(dockerNetworkName)
		framework.ExpectNoError(err, "getting docker network "+dockerNetworkName)

		exchangeLinkName := false
		itFn(exchangeLinkName)

		ginkgo.By("Creating underlay vlan " + vlanName)
		vlan := framework.MakeVlan(vlanName, providerNetworkName, 0)
		_ = vlanClient.Create(vlan)

		ginkgo.By("Creating underlay subnet " + underlaySubnetName)
		cidr := make([]string, 0, 2)
		gateway := make([]string, 0, 2)
		for _, config := range dockerNetwork.IPAM.Config {
			switch util.CheckProtocol(config.Subnet) {
			case kubeovnv1.ProtocolIPv4:
				if f.HasIPv4() {
					cidr = append(cidr, config.Subnet)
					gateway = append(gateway, config.Gateway)
				}
			case kubeovnv1.ProtocolIPv6:
				if f.HasIPv6() {
					cidr = append(cidr, config.Subnet)
					gateway = append(gateway, config.Gateway)
				}
			}
		}
		excludeIPs := make([]string, 0, len(network.Containers)*2)
		for _, container := range network.Containers {
			if container.IPv4Address != "" && f.HasIPv4() {
				excludeIPs = append(excludeIPs, strings.Split(container.IPv4Address, "/")[0])
			}
			if container.IPv6Address != "" && f.HasIPv6() {
				excludeIPs = append(excludeIPs, strings.Split(container.IPv6Address, "/")[0])
			}
		}
		vlanSubnetCidr := strings.Join(cidr, ",")
		vlanSubnetGw := strings.Join(gateway, ",")
		underlaySubnet := framework.MakeSubnet(underlaySubnetName, vlanName, vlanSubnetCidr, vlanSubnetGw, "", "", excludeIPs, nil, nil)
		_ = subnetClient.CreateSync(underlaySubnet)
		vlanSubnet := subnetClient.Get(underlaySubnetName)
		ginkgo.By("Checking underlay vlan " + vlanSubnet.Name)
		framework.ExpectEqual(vlanSubnet.Spec.Vlan, vlanName)
		framework.ExpectNotEqual(vlanSubnet.Spec.CIDRBlock, "")

		externalGwNodes := strings.Join(nodeNames, ",")
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

		ginkgo.By("1. Creating custom vpc enable external no bfd")
		noBfdSubnetV4Cidr := "192.168.0.0/24"
		noBfdSubnetV4Gw := "192.168.0.1"
		enableExternal := true
		disableBfd := false
		noBfdVpc := framework.MakeVpc(noBfdVpcName, "", enableExternal, disableBfd, nil)
		_ = vpcClient.CreateSync(noBfdVpc)
		ginkgo.By("Creating overlay subnet enable ecmp")
		noBfdSubnet := framework.MakeSubnet(noBfdSubnetName, "", noBfdSubnetV4Cidr, noBfdSubnetV4Gw, noBfdVpcName, util.OvnProvider, nil, nil, nil)
		_ = subnetClient.CreateSync(noBfdSubnet)

		// share eip case
		ginkgo.By("Creating share vip")
		shareVip := framework.MakeVip(sharedVipName, noBfdSubnetName, "", "", "")
		_ = vipClient.CreateSync(shareVip)
		ginkgo.By("Creating share ovn eip")
		shareEip := framework.MakeOvnEip(sharedEipName, underlaySubnetName, "", "", "", "")
		_ = ovnEipClient.CreateSync(shareEip)
		ginkgo.By("Creating the first ovn fip with share eip vip should be ok")
		shareFipShouldOk := framework.MakeOvnFip(sharedEipFipShoudOkName, sharedEipName, util.Vip, sharedVipName)
		_ = ovnFipClient.CreateSync(shareFipShouldOk)
		ginkgo.By("Creating the second ovn fip with share eip vip should be failed")
		shareFipShouldFail := framework.MakeOvnFip(sharedEipFipShoudFailName, sharedEipName, util.Vip, sharedVipName)
		_ = ovnFipClient.Create(shareFipShouldFail)
		ginkgo.By("Creating ovn dnat for dnat with share eip vip")
		shareDnat := framework.MakeOvnDnatRule(sharedEipDnatName, sharedEipName, util.Vip, sharedVipName, "80", "8080", "tcp")
		_ = ovnDnatRuleClient.CreateSync(shareDnat)
		ginkgo.By("Creating ovn snat with share eip vip")
		shareSnat := framework.MakeOvnSnatRule(sharedEipSnatName, sharedEipName, noBfdSubnetName, "")
		_ = ovnSnatRuleClient.CreateSync(shareSnat)

		ginkgo.By("Get share eip")
		shareEip = ovnEipClient.Get(sharedEipName)
		ginkgo.By("Get share dnat")
		shareDnat = ovnDnatRuleClient.Get(sharedEipDnatName)
		ginkgo.By("Get share snat")
		shareSnat = ovnSnatRuleClient.Get(sharedEipSnatName)
		ginkgo.By("Get share fip should ok")
		shareFipShouldOk = ovnFipClient.Get(sharedEipFipShoudOkName)
		ginkgo.By("Get share fip should fail")
		shareFipShouldFail = ovnFipClient.Get(sharedEipFipShoudFailName)
		// check
		ginkgo.By("Check share eip should has the external ip label")
		framework.ExpectHaveKeyWithValue(shareEip.Labels, util.EipV4IpLabel, shareEip.Spec.V4Ip)
		ginkgo.By("Check share dnat should has the external ip label")
		framework.ExpectHaveKeyWithValue(shareDnat.Labels, util.EipV4IpLabel, shareEip.Spec.V4Ip)
		ginkgo.By("Check share snat should has the external ip label")
		framework.ExpectHaveKeyWithValue(shareSnat.Labels, util.EipV4IpLabel, shareEip.Spec.V4Ip)
		ginkgo.By("Check share fip should ok should has the external ip label")
		framework.ExpectHaveKeyWithValue(shareFipShouldOk.Labels, util.EipV4IpLabel, shareEip.Spec.V4Ip)
		ginkgo.By("Check share fip should fail should not be ready")
		framework.ExpectEqual(shareFipShouldFail.Status.Ready, false)
		// make sure eip is shared
		nats := []string{util.DnatUsingEip, util.FipUsingEip, util.SnatUsingEip}
		framework.ExpectEqual(shareEip.Status.Nat, strings.Join(nats, ","))
		// make sure vpc has normal external static routes
		noBfdVpc = vpcClient.Get(noBfdVpcName)
		for _, route := range noBfdVpc.Spec.StaticRoutes {
			framework.ExpectEqual(route.RouteTable, util.MainRouteTable)
			framework.ExpectEqual(route.Policy, kubeovnv1.PolicyDst)
			framework.ExpectContainSubstring(vlanSubnetGw, route.NextHopIP)
		}

		ginkgo.By("2. Creating custom vpc enable external and bfd")
		for _, nodeName := range nodeNames {
			ginkgo.By("Creating ovn node-ext-gw type eip on node " + nodeName)
			eip := makeOvnEip(nodeName, underlaySubnetName, "", "", "", util.Lsp)
			_ = ovnEipClient.CreateSync(eip)
		}
		bfdSubnetV4Cidr := "192.168.1.0/24"
		bfdSubnetV4Gw := "192.168.1.1"
		enableBfd := true
		bfdVpc := framework.MakeVpc(bfdVpcName, "", enableExternal, enableBfd, nil)
		_ = vpcClient.CreateSync(bfdVpc)
		ginkgo.By("Creating overlay subnet enable ecmp")
		bfdSubnet := framework.MakeSubnet(bfdSubnetName, "", bfdSubnetV4Cidr, bfdSubnetV4Gw, bfdVpcName, util.OvnProvider, nil, nil, nil)
		bfdSubnet.Spec.EnableEcmp = true
		_ = subnetClient.CreateSync(bfdSubnet)

		// arp proxy vip test case
		ginkgo.By("Creating two arp proxy vips, should have the same mac which is from gw subnet mac")
		ginkgo.By("Creating arp proxy vip " + arpProxyVip1Name)
		arpProxyVip1 := makeOvnVip(arpProxyVip1Name, bfdSubnetName, "", "", util.SwitchLBRuleVip)
		_ = vipClient.CreateSync(arpProxyVip1)
		ginkgo.By("Creating arp proxy vip " + arpProxyVip2Name)
		arpProxyVip2 := makeOvnVip(arpProxyVip2Name, bfdSubnetName, "", "", util.SwitchLBRuleVip)
		_ = vipClient.CreateSync(arpProxyVip2)

		arpProxyVip1 = vipClient.Get(arpProxyVip1Name)
		arpProxyVip2 = vipClient.Get(arpProxyVip2Name)
		framework.ExpectEqual(arpProxyVip1.Status.Mac, arpProxyVip2.Status.Mac)

		// allowed address pair vip test case
		ginkgo.By("Creating crd in centralized case")
		// for now, vip do not have parent ip can be used in centralized external gw case
		ginkgo.By("Creating ovn vip " + fipVipName)
		fipVip := makeOvnVip(fipVipName, bfdSubnetName, "", "", "")
		_ = vipClient.CreateSync(fipVip)
		ginkgo.By("Creating ovn eip " + fipEipName)
		eip := makeOvnEip(fipEipName, underlaySubnetName, "", "", "", "")
		_ = ovnEipClient.CreateSync(eip)
		ginkgo.By("Creating ovn fip " + fipName)
		fip := makeOvnFip(fipName, fipEipName, util.Vip, fipVipName)
		_ = ovnFipClient.CreateSync(fip)

		ginkgo.By("Creating ovn eip " + snatEipName)
		snatEip := makeOvnEip(snatEipName, underlaySubnetName, "", "", "", "")
		_ = ovnEipClient.CreateSync(snatEip)
		ginkgo.By("Creating ovn snat" + snatName)
		snat := makeOvnSnat(snatName, snatEipName, bfdSubnetName, "")
		_ = ovnSnatRuleClient.CreateSync(snat)

		ginkgo.By("Creating ovn vip " + dnatVipName)
		dnatVip := makeOvnVip(dnatVipName, bfdSubnetName, "", "", "")
		_ = vipClient.CreateSync(dnatVip)
		ginkgo.By("Creating ovn eip " + dnatEipName)
		dnatEip := makeOvnEip(dnatEipName, underlaySubnetName, "", "", "", "")
		_ = ovnEipClient.CreateSync(dnatEip)
		ginkgo.By("Creating ovn dnat " + dnatName)
		dnat := makeOvnDnat(dnatName, dnatEipName, util.Vip, dnatVipName, "80", "8080", "tcp")
		_ = ovnDnatRuleClient.CreateSync(dnat)

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
		k8sNodes, err = e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		for _, node := range k8sNodes.Items {
			ginkgo.By("Deleting ovn eip " + node.Name)
			ovnEipClient.DeleteSync(node.Name)
		}

		ginkgo.By("2. Updating config map ovn-external-gw-config for distributed case")
		cmData = map[string]string{
			"enable-external-gw": "true",
			"external-gw-nodes":  externalGwNodes,
			"type":               "distributed",
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
		ginkgo.By("Creating crd in distributed case")
		for _, node := range nodeNames {
			podOnNodeName := fmt.Sprintf("on-node-%s", node)
			eipOnNodeName := fmt.Sprintf("eip-on-node-%s", node)
			fipOnNodeName := fmt.Sprintf("fip-on-node-%s", node)
			ginkgo.By("Creating pod " + podOnNodeName + " with subnet " + bfdSubnetName)
			annotations := map[string]string{util.LogicalSwitchAnnotation: bfdSubnetName}
			cmd := []string{"sh", "-c", "sleep infinity"}
			pod := framework.MakePod(namespaceName, podOnNodeName, nil, annotations, image, cmd, nil)
			pod.Spec.NodeName = node
			_ = podClient.CreateSync(pod)
			// create fip in distributed case
			// for now, vip has no lsp, so not support in distributed case
			ipName := ovs.PodNameToPortName(podOnNodeName, namespaceName, bfdSubnet.Spec.Provider)
			ginkgo.By("Get pod ip" + ipName)
			ip := ipClient.Get(ipName)
			ginkgo.By("Creating ovn eip " + eipOnNodeName)
			eip = makeOvnEip(eipOnNodeName, underlaySubnetName, "", "", "", "")
			_ = ovnEipClient.CreateSync(eip)
			ginkgo.By("Creating ovn fip " + fipOnNodeName)
			fip := makeOvnFip(fipOnNodeName, eipOnNodeName, "", ip.Name)
			_ = ovnFipClient.CreateSync(fip)
		}

		ginkgo.By("Deleting crd in distributed case")
		for _, node := range nodeNames {
			podOnNodeName := fmt.Sprintf("on-node-%s", node)
			eipOnNodeName := fmt.Sprintf("eip-on-node-%s", node)
			fipOnNodeName := fmt.Sprintf("fip-on-node-%s", node)
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
