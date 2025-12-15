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
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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

// Docker network configurations will be initialized in init() with random names and subnets
var (
	dockerNetworkName        string
	dockerExtraNetworkName   string
	dockerNetworkSubnet      string
	dockerExtraNetworkSubnet string
)

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

func makeExternalGatewayConfigMap(gwNodes, switchName string, cidr []string, gwType string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.ExternalGatewayConfig,
			Namespace: framework.KubeOvnNamespace,
		},
		Data: map[string]string{
			"enable-external-gw": "true",
			"external-gw-nodes":  gwNodes,
			"external-gw-switch": switchName,
			"type":               gwType,
			"external-gw-nic":    "eth1",
			"external-gw-addr":   strings.Join(cidr, ","),
		},
	}
}

var _ = framework.SerialDescribe("[group:ovn-vpc-nat-gw]", func() {
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

		randomSuffix := framework.RandomSuffix()
		// Use first 6 digits of random suffix for provider network names (12 byte limit)
		shortSuffix := randomSuffix[:6]
		noBfdVpcName = "no-bfd-vpc-" + randomSuffix
		bfdVpcName = "bfd-vpc-" + randomSuffix

		// nats use ip crd name or vip crd
		fipName = "fip-" + randomSuffix

		countingEipName = "counting-eip-" + randomSuffix
		noBfdSubnetName = "no-bfd-subnet-" + randomSuffix
		noBfdExtraSubnetName = "no-bfd-extra-subnet-" + randomSuffix
		lrpEipSnatName = "lrp-eip-snat-" + randomSuffix
		lrpExtraEipSnatName = "lrp-extra-eip-snat-" + randomSuffix
		bfdSubnetName = "bfd-subnet-" + randomSuffix
		// Default external network must use fixed "external" name for global reuse
		// kube-ovn-cni creates default external subnet with name "external" when enable-eip-snat
		providerNetworkName = "external"
		underlaySubnetName = "external"
		vlanName = "vlan"
		// Extra network uses random suffix to test multi-external-subnet scenarios
		// provider network name has 12 bytes limit, use short suffix
		providerExtraNetworkName = "extra-" + shortSuffix
		underlayExtraSubnetName = "extra-" + shortSuffix
		vlanExtraName = "vlan-extra-" + randomSuffix

		// sharing case
		sharedVipName = "shared-vip-" + randomSuffix
		sharedEipDnatName = "shared-eip-dnat-" + randomSuffix
		sharedEipFipShoudOkName = "shared-eip-fip-should-ok-" + randomSuffix
		sharedEipFipShoudFailName = "shared-eip-fip-should-fail-" + randomSuffix

		// pod with fip
		fipPodName = "fip-pod-" + randomSuffix
		podEipName = fipPodName
		podFipName = fipPodName

		// pod with fip for extra external subnet
		fipExtraPodName = "fip-extra-pod-" + randomSuffix
		podExtraEipName = fipExtraPodName
		podExtraFipName = fipExtraPodName

		// fip use ip addr
		ipFipVipName = "ip-fip-vip-" + randomSuffix
		ipFipEipName = "ip-fip-eip-" + randomSuffix
		ipFipName = "ip-fip-" + randomSuffix

		// dnat use ip addr
		ipDnatVipName = "ip-dnat-vip-" + randomSuffix
		ipDnatEipName = "ip-dnat-eip-" + randomSuffix
		ipDnatName = "ip-dnat-" + randomSuffix

		// snat use ip cidr
		cidrSnatEipName = "cidr-snat-eip-" + randomSuffix
		cidrSnatName = "cidr-snat-" + randomSuffix
		ipSnatVipName = "ip-snat-vip-" + randomSuffix
		ipSnatEipName = "ip-snat-eip-" + randomSuffix
		ipSnatName = "ip-snat-" + randomSuffix

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
			ginkgo.By("Ensuring docker network " + dockerNetworkName + " with subnet " + dockerNetworkSubnet + " exists")
			network, err := docker.NetworkCreateWithSubnet(dockerNetworkName, dockerNetworkSubnet, true, true)
			framework.ExpectNoError(err, "ensuring docker network "+dockerNetworkName+" exists")
			dockerNetwork = network
		}

		if dockerExtraNetwork == nil {
			ginkgo.By("Ensuring extra docker network " + dockerExtraNetworkName + " with subnet " + dockerExtraNetworkSubnet + " exists")
			network, err := docker.NetworkCreateWithSubnet(dockerExtraNetworkName, dockerExtraNetworkSubnet, true, true)
			framework.ExpectNoError(err, "ensuring extra docker network "+dockerExtraNetworkName+" exists")
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

		ginkgo.By("Confirming provider networks are fully deleted")
		startTime := time.Now()
		checkCount := 0
		framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			checkCount++
			elapsed := time.Since(startTime)

			pn, err := providerNetworkClient.ProviderNetworkInterface.Get(context.Background(), providerNetworkName, metav1.GetOptions{})
			if err != nil && !k8serrors.IsNotFound(err) {
				return false, err
			}
			if err == nil {
				// Provider network still exists
				framework.Logf("Warning: provider network %s still exists after %v (check #%d), DeletionTimestamp: %v",
					providerNetworkName, elapsed, checkCount, pn.DeletionTimestamp)
				return false, nil
			}

			extraPn, err := providerNetworkClient.ProviderNetworkInterface.Get(context.Background(), providerExtraNetworkName, metav1.GetOptions{})
			if err != nil && !k8serrors.IsNotFound(err) {
				return false, err
			}
			if err == nil {
				// Extra provider network still exists
				framework.Logf("Warning: provider extra network %s still exists after %v (check #%d), DeletionTimestamp: %v",
					providerExtraNetworkName, elapsed, checkCount, extraPn.DeletionTimestamp)
				return false, nil
			}

			// Both provider networks are deleted
			framework.Logf("Provider networks fully deleted after %v (%d checks)", elapsed, checkCount)
			return true, nil
		}, "waiting for provider networks to be fully deleted")

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

		ginkgo.By("Verifying vlan " + vlanName + " does not exist from previous test")
		_, err = vlanClient.VlanInterface.Get(context.Background(), vlanName, metav1.GetOptions{})
		framework.ExpectTrue(k8serrors.IsNotFound(err), "vlan %s should not exist, AfterEach cleanup may have failed", vlanName)

		ginkgo.By("Creating underlay vlan " + vlanName)
		vlan := framework.MakeVlan(vlanName, providerNetworkName, 0)
		_ = vlanClient.Create(vlan)

		ginkgo.By("Preparing subnet configuration for " + underlaySubnetName)
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

		ginkgo.By("Verifying subnet " + underlaySubnetName + " does not exist from previous test")
		_, err = subnetClient.SubnetInterface.Get(context.Background(), underlaySubnetName, metav1.GetOptions{})
		framework.ExpectTrue(k8serrors.IsNotFound(err), "subnet %s should not exist, AfterEach cleanup may have failed", underlaySubnetName)

		ginkgo.By("Creating underlay subnet " + underlaySubnetName)
		var oldUnderlayExternalSubnet *kubeovnv1.Subnet
		underlaySubnet := framework.MakeSubnet(underlaySubnetName, vlanName, vlanSubnetCidr, vlanSubnetGw, "", "", excludeIPs, nil, nil)
		oldUnderlayExternalSubnet = subnetClient.CreateSync(underlaySubnet)
		countingEip := makeOvnEip(countingEipName, underlaySubnetName, "", "", "", "")
		_ = ovnEipClient.CreateSync(countingEip)
		ginkgo.By("Checking underlay vlan " + oldUnderlayExternalSubnet.Name)
		framework.ExpectEqual(oldUnderlayExternalSubnet.Spec.Vlan, vlanName)
		framework.ExpectNotEqual(oldUnderlayExternalSubnet.Spec.CIDRBlock, "")
		ginkgo.By("Wait for ovn eip finalizer to be added")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			eipCR := ovnEipClient.Get(countingEipName)
			return eipCR != nil && len(eipCR.Finalizers) > 0, nil
		}, "OvnEip should have finalizer added")
		ginkgo.By("Wait for subnet status to be updated after ovn eip creation")
		time.Sleep(5 * time.Second)
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
		ginkgo.By("Wait for subnet status to be updated after ovn eip deletion")
		time.Sleep(5 * time.Second)
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
		configMap := makeExternalGatewayConfigMap(externalGwNodes, underlaySubnetName, cidr, kubeovnv1.GWCentralizedType)
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

		ginkgo.By("Verifying extra vlan " + vlanExtraName + " does not exist from previous test")
		_, err = vlanClient.VlanInterface.Get(context.Background(), vlanExtraName, metav1.GetOptions{})
		framework.ExpectTrue(k8serrors.IsNotFound(err), "vlan %s should not exist, AfterEach cleanup may have failed", vlanExtraName)

		ginkgo.By("Creating underlay extra vlan " + vlanExtraName)
		vlan = framework.MakeVlan(vlanExtraName, providerExtraNetworkName, 0)
		_ = vlanClient.Create(vlan)

		ginkgo.By("Preparing extra subnet configuration for " + underlayExtraSubnetName)
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

		ginkgo.By("Verifying extra subnet " + underlayExtraSubnetName + " does not exist from previous test")
		_, err = subnetClient.SubnetInterface.Get(context.Background(), underlayExtraSubnetName, metav1.GetOptions{})
		framework.ExpectTrue(k8serrors.IsNotFound(err), "subnet %s should not exist, AfterEach cleanup may have failed", underlayExtraSubnetName)

		ginkgo.By("Creating extra underlay subnet " + underlayExtraSubnetName)
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
		// TODO:// external-gw-nodes could be auto managed by recognizing gw chassis node which has the external-gw-nic
		configMap = makeExternalGatewayConfigMap(externalGwNodes, underlaySubnetName, cidr, kubeovnv1.GWDistributedType)
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

	// Helper function to wait for OvnEip to be ready
	waitForOvnEipReady := func(eipClient *framework.OvnEipClient, eipName string, timeout time.Duration) *kubeovnv1.OvnEip {
		ginkgo.GinkgoHelper()
		var eip *kubeovnv1.OvnEip
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			eip = eipClient.Get(eipName)
			if eip != nil && eip.Status.V4Ip != "" && eip.Status.Ready {
				framework.Logf("OvnEip %s is ready with V4IP: %s", eipName, eip.Status.V4Ip)
				return eip
			}
			time.Sleep(2 * time.Second)
		}
		framework.Failf("Timeout waiting for OvnEip %s to be ready", eipName)
		return nil
	}

	framework.ConformanceIt("should properly manage OvnEip lifecycle with finalizer and update subnet status", func() {
		f.SkipVersionPriorTo(1, 13, "This feature was introduced in v1.13")

		ginkgo.By("Setting up provider network and underlay subnet")
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
		_ = subnetClient.CreateSync(underlaySubnet)

		ginkgo.By("Step 1: Recording initial underlay subnet status")
		initialSubnet := subnetClient.Get(underlaySubnetName)
		framework.Logf("Initial subnet status - V4: Available=%.0f Using=%.0f, V6: Available=%.0f Using=%.0f",
			initialSubnet.Status.V4AvailableIPs, initialSubnet.Status.V4UsingIPs,
			initialSubnet.Status.V6AvailableIPs, initialSubnet.Status.V6UsingIPs)

		// Store initial status
		initialV4Available := initialSubnet.Status.V4AvailableIPs
		initialV4Using := initialSubnet.Status.V4UsingIPs
		initialV6Available := initialSubnet.Status.V6AvailableIPs
		initialV6Using := initialSubnet.Status.V6UsingIPs

		ginkgo.By("Step 2: Creating OvnEip and waiting for it to be ready")
		eipName := "test-ovn-eip-lifecycle-" + framework.RandomSuffix()
		eip := makeOvnEip(eipName, underlaySubnetName, "", "", "", util.OvnEipTypeNAT)
		_ = ovnEipClient.CreateSync(eip)

		eipCR := waitForOvnEipReady(ovnEipClient, eipName, 2*time.Minute)
		framework.ExpectNotEmpty(eipCR.Status.V4Ip, "OvnEip should have V4 IP assigned")

		ginkgo.By("Step 3: Verifying finalizer is added to OvnEip")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			eipCR = ovnEipClient.Get(eipName)
			return eipCR != nil && len(eipCR.Finalizers) > 0, nil
		}, "OvnEip should have finalizer added")
		framework.ExpectContainElement(eipCR.Finalizers, util.KubeOVNControllerFinalizer,
			"OvnEip must have controller finalizer")

		ginkgo.By("Step 4: Verifying subnet status updated after OvnEip creation")
		time.Sleep(5 * time.Second)
		afterCreateSubnet := subnetClient.Get(underlaySubnetName)

		// Verify based on protocol
		protocol := afterCreateSubnet.Spec.Protocol
		framework.Logf("Verifying subnet status for protocol: %s", protocol)

		if protocol == kubeovnv1.ProtocolIPv4 || protocol == kubeovnv1.ProtocolDual {
			framework.ExpectEqual(initialV4Available-1, afterCreateSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should decrease by 1")
			framework.ExpectEqual(initialV4Using+1, afterCreateSubnet.Status.V4UsingIPs,
				"V4UsingIPs should increase by 1")
			framework.ExpectTrue(strings.Contains(afterCreateSubnet.Status.V4UsingIPRange, eipCR.Status.V4Ip),
				"EIP V4 IP should be in using range")
		}
		if protocol == kubeovnv1.ProtocolIPv6 || protocol == kubeovnv1.ProtocolDual {
			framework.ExpectEqual(initialV6Available-1, afterCreateSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should decrease by 1")
			framework.ExpectEqual(initialV6Using+1, afterCreateSubnet.Status.V6UsingIPs,
				"V6UsingIPs should increase by 1")
		}

		// Store after-create status
		afterCreateV4Available := afterCreateSubnet.Status.V4AvailableIPs
		afterCreateV4Using := afterCreateSubnet.Status.V4UsingIPs
		afterCreateV6Available := afterCreateSubnet.Status.V6AvailableIPs
		afterCreateV6Using := afterCreateSubnet.Status.V6UsingIPs

		ginkgo.By("Step 5: Deleting OvnEip and verifying cleanup")
		ovnEipClient.DeleteSync(eipName)

		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			_, err := f.KubeOVNClientSet.KubeovnV1().OvnEips().Get(context.Background(), eipName, metav1.GetOptions{})
			return k8serrors.IsNotFound(err), nil
		}, "OvnEip should be deleted")

		ginkgo.By("Step 6: Verifying subnet status restored after OvnEip deletion")
		time.Sleep(5 * time.Second)
		afterDeleteSubnet := subnetClient.Get(underlaySubnetName)

		if protocol == kubeovnv1.ProtocolIPv4 || protocol == kubeovnv1.ProtocolDual {
			framework.ExpectEqual(afterCreateV4Available+1, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should increase by 1")
			framework.ExpectEqual(afterCreateV4Using-1, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should decrease by 1")
			framework.ExpectEqual(initialV4Available, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should return to initial value")
			framework.ExpectEqual(initialV4Using, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should return to initial value")
		}
		if protocol == kubeovnv1.ProtocolIPv6 || protocol == kubeovnv1.ProtocolDual {
			framework.ExpectEqual(afterCreateV6Available+1, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should increase by 1")
			framework.ExpectEqual(afterCreateV6Using-1, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should decrease by 1")
			framework.ExpectEqual(initialV6Available, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should return to initial value")
			framework.ExpectEqual(initialV6Using, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should return to initial value")
		}

		framework.Logf("OvnEip lifecycle test completed successfully")
	})

	framework.ConformanceIt("should block OvnEip deletion when used by NAT rules", func() {
		f.SkipVersionPriorTo(1, 13, "This feature was introduced in v1.13")

		ginkgo.By("Setting up test environment")
		network, err := docker.NetworkInspect(dockerNetworkName)
		framework.ExpectNoError(err)

		exchangeLinkName := false
		itFn(exchangeLinkName, providerNetworkName, linkMap, &providerBridgeIps)

		ginkgo.By("Creating underlay vlan and subnet")
		vlan := framework.MakeVlan(vlanName, providerNetworkName, 0)
		_ = vlanClient.Create(vlan)

		var cidrV4, gatewayV4 string
		for _, config := range dockerNetwork.IPAM.Config {
			if util.CheckProtocol(config.Subnet.String()) == kubeovnv1.ProtocolIPv4 && f.HasIPv4() {
				cidrV4 = config.Subnet.String()
				gatewayV4 = config.Gateway.String()
				break
			}
		}
		excludeIPs := make([]string, 0, len(network.Containers))
		for _, container := range network.Containers {
			if container.IPv4Address.IsValid() && f.HasIPv4() {
				excludeIPs = append(excludeIPs, container.IPv4Address.Addr().String())
			}
		}
		underlaySubnet := framework.MakeSubnet(underlaySubnetName, vlanName, cidrV4, gatewayV4, "", "", excludeIPs, nil, nil)
		_ = subnetClient.CreateSync(underlaySubnet)

		ginkgo.By("Step 1: Creating custom VPC and subnet for testing")
		testVpcName := "test-vpc-dep-" + framework.RandomSuffix()
		testSubnetName := "test-subnet-dep-" + framework.RandomSuffix()
		testVpc := framework.MakeVpc(testVpcName, "", false, false, nil)
		_ = vpcClient.CreateSync(testVpc)

		testSubnet := framework.MakeSubnet(testSubnetName, "", "192.168.100.0/24", "192.168.100.1", testVpcName, util.OvnProvider, nil, nil, nil)
		_ = subnetClient.CreateSync(testSubnet)

		ginkgo.By("Step 2: Creating VIP for FIP")
		vipName := "test-vip-dep-" + framework.RandomSuffix()
		vip := makeOvnVip(namespaceName, vipName, testSubnetName, "", "", "")
		vip = vipClient.CreateSync(vip)
		framework.ExpectNotEmpty(vip.Status.V4ip)

		ginkgo.By("Step 3: Creating OvnEip")
		eipName := "test-eip-with-dep-" + framework.RandomSuffix()
		eip := makeOvnEip(eipName, underlaySubnetName, "", "", "", util.OvnEipTypeNAT)
		_ = ovnEipClient.CreateSync(eip)

		eipCR := waitForOvnEipReady(ovnEipClient, eipName, 2*time.Minute)

		ginkgo.By("Step 4: Creating OvnFip using the EIP")
		fipName := "test-fip-dep-" + framework.RandomSuffix()
		fip := makeOvnFip(fipName, eipName, "", "", testVpcName, vip.Status.V4ip)
		_ = ovnFipClient.CreateSync(fip)

		ginkgo.By("Step 5: Verifying EIP Status.Nat shows FIP usage")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			eipCR = ovnEipClient.Get(eipName)
			return eipCR != nil && strings.Contains(eipCR.Status.Nat, util.FipUsingEip), nil
		}, "EIP Status.Nat should contain 'fip'")

		ginkgo.By("Step 6: Attempting to delete EIP (should be blocked by FIP)")
		err = f.KubeOVNClientSet.KubeovnV1().OvnEips().Delete(context.Background(), eipName, metav1.DeleteOptions{})
		framework.ExpectNoError(err, "Delete operation should succeed")

		ginkgo.By("Step 7: Verifying EIP still exists with finalizer (blocked)")
		time.Sleep(5 * time.Second)
		eipCR = ovnEipClient.Get(eipName)
		framework.ExpectNotNil(eipCR, "EIP should still exist")
		framework.ExpectNotNil(eipCR.DeletionTimestamp, "EIP should have DeletionTimestamp")
		framework.ExpectContainElement(eipCR.Finalizers, util.KubeOVNControllerFinalizer,
			"EIP should still have finalizer because FIP is using it")

		ginkgo.By("Step 8: Deleting FIP to unblock EIP deletion")
		ovnFipClient.DeleteSync(fipName)

		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			_, err := f.KubeOVNClientSet.KubeovnV1().OvnFips().Get(context.Background(), fipName, metav1.GetOptions{})
			return k8serrors.IsNotFound(err), nil
		}, "FIP should be deleted")

		ginkgo.By("Step 9: Verifying EIP is now deleted after FIP removal")
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			_, err := f.KubeOVNClientSet.KubeovnV1().OvnEips().Get(context.Background(), eipName, metav1.GetOptions{})
			return k8serrors.IsNotFound(err), nil
		}, "EIP should be deleted after FIP is removed")

		ginkgo.By("Step 10: Cleaning up test resources")
		vipClient.DeleteSync(vipName)
		subnetClient.DeleteSync(testSubnetName)
		vpcClient.DeleteSync(testVpcName)

		framework.Logf("OvnEip dependency blocking test completed successfully")
	})
})

func init() {
	// Generate unique network names and subnets for this test run
	// This avoids conflicts with other parallel test runs on the same host
	suffix := framework.RandomSuffix()
	dockerNetworkName = "kube-ovn-vlan0-" + suffix
	dockerExtraNetworkName = "kube-ovn-extra-vlan0-" + suffix

	// Generate random /24 subnets within 172.28.0.0/16
	// This allows up to 256 parallel test runs on the same host
	subnets := docker.GenerateRandomSubnets(2)
	dockerNetworkSubnet = subnets[0]
	dockerExtraNetworkSubnet = subnets[1]

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
