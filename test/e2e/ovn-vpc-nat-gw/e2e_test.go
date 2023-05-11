package ovn_eip

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/onsi/ginkgo/v2"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iproute"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const dockerNetworkName = "kube-ovn-vlan"

func makeProviderNetwork(providerNetworkName string, exchangeLinkName bool, linkMap map[string]*iproute.Link) *apiv1.ProviderNetwork {
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

func makeOvnEip(name, subnet, v4ip, v6ip, mac, usage string) *apiv1.OvnEip {
	return framework.MakeOvnEip(name, subnet, v4ip, v6ip, mac, usage)
}

func makeOvnVip(name, subnet, v4ip, v6ip string) *apiv1.Vip {
	return framework.MakeVip(name, subnet, v4ip, v6ip)
}

func makeOvnFip(name, ovnEip, ipType, ipName string) *apiv1.OvnFip {
	return framework.MakeOvnFip(name, ovnEip, ipType, ipName)
}

func makeOvnSnat(name, ovnEip, vpcSubnet, ipName string) *apiv1.OvnSnatRule {
	return framework.MakeOvnSnatRule(name, ovnEip, vpcSubnet, ipName)
}

func makeOvnDnat(name, ovnEip, ipType, ipName, internalPort, externalPort, protocol string) *apiv1.OvnDnatRule {
	return framework.MakeOvnDnatRule(name, ovnEip, ipType, ipName, internalPort, externalPort, protocol)
}

var _ = framework.Describe("[group:ovn-vpc-nat-gw]", func() {
	f := framework.NewDefaultFramework("ovn-vpc-nat-gw")

	var skip bool
	var itFn func(bool)
	var cs clientset.Interface
	var nodeNames []string
	var clusterName, providerNetworkName, vlanName, subnetName, vpcName, overlaySubnetName string
	var linkMap map[string]*iproute.Link
	var providerNetworkClient *framework.ProviderNetworkClient
	var vlanClient *framework.VlanClient
	var vpcClient *framework.VpcClient
	var subnetClient *framework.SubnetClient
	var ovnEipClient *framework.OvnEipClient
	var fipVipName, eipName, fipName, dnatVipName, dnatEipName, dnatName, snatEipName, snatName, namespaceName string
	var ipClient *framework.IpClient
	var vipClient *framework.VipClient
	var ovnFipClient *framework.OvnFipClient
	var ovnSnatRuleClient *framework.OvnSnatRuleClient
	var ovnDnatRuleClient *framework.OvnDnatRuleClient

	var podClient *framework.PodClient

	var dockerNetwork *dockertypes.NetworkResource
	var containerID string
	var image string

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		subnetClient = f.SubnetClient()
		vlanClient = f.VlanClient()
		vpcClient = f.VpcClient()
		providerNetworkClient = f.ProviderNetworkClient()
		ovnEipClient = f.OvnEipClient()
		ipClient = f.IpClient()
		vipClient = f.VipClient()
		ovnFipClient = f.OvnFipClient()
		ovnSnatRuleClient = f.OvnSnatRuleClient()
		ovnDnatRuleClient = f.OvnDnatRuleClient()

		podClient = f.PodClient()

		namespaceName = f.Namespace.Name
		vpcName = "vpc-" + framework.RandomSuffix()

		fipVipName = "fip-vip-" + framework.RandomSuffix()
		eipName = "fip-eip-" + framework.RandomSuffix()
		fipName = "fip-" + framework.RandomSuffix()

		dnatVipName = "dnat-vip-" + framework.RandomSuffix()
		dnatEipName = "dnat-eip-" + framework.RandomSuffix()
		dnatName = "dnat-" + framework.RandomSuffix()

		snatEipName = "snat-eip-" + framework.RandomSuffix()
		snatName = "snat-" + framework.RandomSuffix()
		overlaySubnetName = "overlay-subnet-" + framework.RandomSuffix()
		providerNetworkName = "external"
		vlanName = "vlan-" + framework.RandomSuffix()
		subnetName = "external"
		containerID = ""
		if image == "" {
			image = framework.GetKubeOvnImage(cs)
		}

		if skip {
			ginkgo.Skip("underlay spec only runs on kind clusters")
		}

		if clusterName == "" {
			ginkgo.By("Getting k8s nodes")
			k8sNodes, err := e2enode.GetReadySchedulableNodes(cs)
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
			ginkgo.By("Creating provider network")
			pn := makeProviderNetwork(providerNetworkName, exchangeLinkName, linkMap)
			pn = providerNetworkClient.CreateSync(pn)

			ginkgo.By("Getting k8s nodes")
			k8sNodes, err := e2enode.GetReadySchedulableNodes(cs)
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

		ginkgo.By("Deleting custom vpc " + vpcName)
		vpcClient.DeleteSync(vpcName)

		ginkgo.By("Deleting subnet " + overlaySubnetName)
		subnetClient.DeleteSync(overlaySubnetName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)

		ginkgo.By("Deleting vlan " + vlanName)
		vlanClient.Delete(vlanName, metav1.DeleteOptions{})

		ginkgo.By("Deleting provider network")
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
		ginkgo.By("create underlay provider network")
		exchangeLinkName := false
		itFn(exchangeLinkName)

		ginkgo.By("Getting docker network " + dockerNetworkName)
		network, err := docker.NetworkInspect(dockerNetworkName)
		framework.ExpectNoError(err, "getting docker network "+dockerNetworkName)

		ginkgo.By("Creating underlay vlan " + vlanName)
		vlan := framework.MakeVlan(vlanName, providerNetworkName, 0)
		_ = vlanClient.Create(vlan)

		ginkgo.By("Creating underlay subnet " + subnetName)
		cidr := make([]string, 0, 2)
		gateway := make([]string, 0, 2)
		for _, config := range dockerNetwork.IPAM.Config {
			switch util.CheckProtocol(config.Subnet) {
			case apiv1.ProtocolIPv4:
				if f.ClusterIpFamily != "ipv6" {
					cidr = append(cidr, config.Subnet)
					gateway = append(gateway, config.Gateway)
				}
			case apiv1.ProtocolIPv6:
				if f.ClusterIpFamily != "ipv4" {
					cidr = append(cidr, config.Subnet)
					gateway = append(gateway, config.Gateway)
				}
			}
		}
		excludeIPs := make([]string, 0, len(network.Containers)*2)
		for _, container := range network.Containers {
			if container.IPv4Address != "" && f.ClusterIpFamily != "ipv6" {
				excludeIPs = append(excludeIPs, strings.Split(container.IPv4Address, "/")[0])
			}
			if container.IPv6Address != "" && f.ClusterIpFamily != "ipv4" {
				excludeIPs = append(excludeIPs, strings.Split(container.IPv6Address, "/")[0])
			}
		}
		subnet := framework.MakeSubnet(subnetName, vlanName, strings.Join(cidr, ","), strings.Join(gateway, ","), "", "", excludeIPs, nil, nil)
		_ = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating config map ovn-external-gw-config for centralized case")
		cmData := map[string]string{
			"enable-external-gw": "true",
			"external-gw-nodes":  "kube-ovn-control-plane,kube-ovn-worker",
			"type":               "centralized",
			"external-gw-nic":    "eth1",
			"external-gw-addr":   strings.Join(cidr, ","),
		}
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ovn-external-gw-config",
				Namespace: "kube-system",
			},
			Data: cmData,
		}
		_, err = cs.CoreV1().ConfigMaps("kube-system").Create(context.Background(), configMap, metav1.CreateOptions{})
		framework.ExpectNoError(err, "failed to create ConfigMap")

		ginkgo.By("Creating custom vpc enable external and bfd")
		overlaySubnetV4Cidr := "192.168.0.0/24"
		overlaySubnetV4Gw := "192.168.0.1"
		enableExternal := true
		enableBfd := true
		vpc := framework.MakeVpc(vpcName, overlaySubnetV4Gw, enableExternal, enableBfd)
		_ = vpcClient.CreateSync(vpc)

		ginkgo.By("Creating overlay subnet enable ecmp")
		overlaySubnet := framework.MakeSubnet(overlaySubnetName, "", overlaySubnetV4Cidr, overlaySubnetV4Gw, vpcName, util.OvnProvider, nil, nil, nil)
		_ = subnetClient.CreateSync(overlaySubnet)

		ginkgo.By("Getting k8s nodes")
		k8sNodes, err := e2enode.GetReadySchedulableNodes(cs)
		framework.ExpectNoError(err)

		for _, node := range k8sNodes.Items {
			ginkgo.By("Creating ovn eip " + node.Name)
			eip := makeOvnEip(node.Name, subnetName, "", "", "", util.NodeExtGwUsingEip)
			_ = ovnEipClient.CreateSync(eip)
		}
		ginkgo.By("Creating crd in centralized case")
		// for now, vip do not have parent ip can be used in centralized external gw case
		ginkgo.By("Creating ovn vip " + fipVipName)
		fipVip := makeOvnVip(fipVipName, overlaySubnetName, "", "")
		_ = vipClient.CreateSync(fipVip)
		ginkgo.By("Creating ovn eip " + eipName)
		eip := makeOvnEip(eipName, subnetName, "", "", "", util.FipUsingEip)
		_ = ovnEipClient.CreateSync(eip)
		ginkgo.By("Creating ovn fip " + fipName)
		fip := makeOvnFip(fipName, eipName, util.NatUsingVip, fipVipName)
		_ = ovnFipClient.CreateSync(fip)

		ginkgo.By("Creating ovn eip " + snatEipName)
		snatEip := makeOvnEip(snatEipName, subnetName, "", "", "", util.SnatUsingEip)
		_ = ovnEipClient.CreateSync(snatEip)
		ginkgo.By("Creating ovn snat" + snatName)
		snat := makeOvnSnat(snatName, snatEipName, overlaySubnetName, "")
		_ = ovnSnatRuleClient.CreateSync(snat)

		ginkgo.By("Creating ovn vip " + dnatVipName)
		dnatVip := makeOvnVip(dnatVipName, overlaySubnetName, "", "")
		_ = vipClient.CreateSync(dnatVip)
		ginkgo.By("Creating ovn eip " + dnatEipName)
		dnatEip := makeOvnEip(dnatEipName, subnetName, "", "", "", util.DnatUsingEip)
		_ = ovnEipClient.CreateSync(dnatEip)
		ginkgo.By("Creating ovn dnat " + dnatName)
		dnat := makeOvnDnat(dnatName, dnatEipName, util.NatUsingVip, dnatVipName, "80", "8080", "tcp")
		_ = ovnDnatRuleClient.CreateSync(dnat)

		k8sNodes, err = e2enode.GetReadySchedulableNodes(cs)
		framework.ExpectNoError(err)
		for _, node := range k8sNodes.Items {
			// label should be true after setup node external gw
			framework.ExpectHaveKeyWithValue(node.Labels, util.NodeExtGwLabel, "true")
		}

		k8sNodes, err = e2enode.GetReadySchedulableNodes(cs)
		framework.ExpectNoError(err)
		for _, node := range k8sNodes.Items {
			ginkgo.By("Deleting ovn eip " + node.Name)
			ovnEipClient.DeleteSync(node.Name)
		}

		ginkgo.By("Deleting crd in centralized case")
		ginkgo.By("Deleting ovn fip " + fipName)
		ovnFipClient.DeleteSync(fipName)
		ginkgo.By("Deleting ovn dnat " + dnatName)
		ovnDnatRuleClient.DeleteSync(dnatName)
		ginkgo.By("Deleting ovn snat " + snatName)
		ovnSnatRuleClient.DeleteSync(snatName)

		ginkgo.By("Deleting ovn eip " + eipName)
		ovnEipClient.DeleteSync(eipName)
		ginkgo.By("Deleting ovn eip " + dnatEipName)
		ovnEipClient.DeleteSync(dnatEipName)
		ginkgo.By("Deleting ovn eip " + snatEipName)
		ovnEipClient.DeleteSync(snatEipName)

		lrpEipName := fmt.Sprintf("%s-%s", vpcName, subnetName)
		ginkgo.By("Deleting ovn eip " + lrpEipName)
		ovnEipClient.DeleteSync(lrpEipName)

		defaultVpcLrpEipName := fmt.Sprintf("%s-%s", util.DefaultVpc, "external")
		ginkgo.By("Deleting ovn eip " + defaultVpcLrpEipName)
		ovnEipClient.DeleteSync(defaultVpcLrpEipName)

		ginkgo.By("Deleting ovn vip " + fipVipName)
		vipClient.DeleteSync(fipVipName)
		ginkgo.By("Deleting ovn vip " + dnatVipName)
		vipClient.DeleteSync(dnatVipName)

		ginkgo.By("Creating config map ovn-external-gw-config for distributed case")
		cmData = map[string]string{
			"enable-external-gw": "true",
			"external-gw-nodes":  "kube-ovn-control-plane,kube-ovn-worker",
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
			podName := fmt.Sprintf("fip-%s", node)
			ginkgo.By("Creating pod " + podName + " with subnet " + overlaySubnetName)
			annotations := map[string]string{util.LogicalSwitchAnnotation: overlaySubnetName}
			cmd := []string{"sh", "-c", "sleep infinity"}
			pod := framework.MakePod(namespaceName, podName, nil, annotations, image, cmd, nil)
			pod.Spec.NodeName = node
			_ = podClient.CreateSync(pod)
			// create fip in distributed case
			// for now, vip has no lsp, so not support in distributed case
			ipName := ovs.PodNameToPortName(podName, namespaceName, overlaySubnet.Spec.Provider)
			ginkgo.By("Get pod ip" + ipName)
			ip := ipClient.Get(ipName)
			eipName = fmt.Sprintf("fip-%s", node)
			ginkgo.By("Creating ovn eip " + eipName)
			eip = makeOvnEip(eipName, subnetName, "", "", "", util.FipUsingEip)
			_ = ovnEipClient.CreateSync(eip)
			fipName = fmt.Sprintf("fip-%s", node)
			ginkgo.By("Creating ovn fip " + fipName)
			fip := makeOvnFip(fipName, eipName, "", ip.Name)
			_ = ovnFipClient.CreateSync(fip)
			// clean fip eip in distributed case
			ginkgo.By("Deleting ovn fip " + fipName)
			ovnFipClient.DeleteSync(fipName)
			ginkgo.By("Deleting ovn eip " + eipName)
			ovnEipClient.DeleteSync(eipName)
		}

		ginkgo.By("Deleting crd in distributed case")
		for _, node := range nodeNames {
			eipName = fmt.Sprintf("fip-%s", node)
			fipName = fmt.Sprintf("fip-%s", node)
			ginkgo.By("Deleting ovn fip " + fipName)
			ovnFipClient.DeleteSync(fipName)
			ginkgo.By("Deleting ovn eip " + eipName)
			ovnEipClient.DeleteSync(eipName)
			podName := fmt.Sprintf("fip-%s", node)
			ipName := ovs.PodNameToPortName(podName, namespaceName, overlaySubnet.Spec.Provider)
			ginkgo.By("Deleting pod ip" + ipName)
			ipClient.DeleteSync(ipName)
		}

		k8sNodes, err = e2enode.GetReadySchedulableNodes(cs)
		framework.ExpectNoError(err)
		for _, node := range k8sNodes.Items {
			// label should be false after remove node external gw
			framework.ExpectHaveKeyWithValue(node.Labels, util.NodeExtGwLabel, "false")
		}

		ginkgo.By("Deleting configmap")
		err = cs.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Delete(context.Background(), "ovn-external-gw-config", metav1.DeleteOptions{})
		framework.ExpectNoError(err, "failed to delete ConfigMap")
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
	if k8sframework.TestContext.KubeConfig == "" {
		k8sframework.TestContext.KubeConfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)

	e2e.RunE2ETests(t)
}
