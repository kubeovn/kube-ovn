package ovn_eip

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dockertypes "github.com/docker/docker/api/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

const dockerExtNet1Name = "kube-ovn-ext-net1"
const dockerExtNet2Name = "kube-ovn-ext-net2"
const vpcNatGWConfigMapName = "ovn-vpc-nat-gw-config"
const networkAttachDefName = "ovn-vpc-external-network"
const externalSubnetProvider = "ovn-vpc-external-network.kube-system"

func setupVpcNatGwTestEnvironment(
	f *framework.Framework,
	dockerExtNetNetwork *dockertypes.NetworkResource,
	attachNetClient *framework.NetworkAttachmentDefinitionClient,
	subnetClient *framework.SubnetClient,
	vpcClient *framework.VpcClient,
	vpcNatGwClient *framework.VpcNatGatewayClient,
	vpcName string,
	overlaySubnetName string,
	vpcNatGwName string,
	overlaySubnetV4Cidr string,
	overlaySubnetV4Gw string,
	lanIp string,
	dockerExtNetName string,
	externalNetworkName string,
	nicName string,
	provider string,
) {
	ginkgo.By("Getting docker network " + dockerExtNetName)
	network, err := docker.NetworkInspect(dockerExtNetName)
	framework.ExpectNoError(err, "getting docker network "+dockerExtNetName)

	ginkgo.By("Getting k8s nodes")
	_, err = e2enode.GetReadySchedulableNodes(context.Background(), f.ClientSet)
	framework.ExpectNoError(err)

	ginkgo.By("Getting network attachment definition " + externalNetworkName)
	attachConf := fmt.Sprintf(`{"cniVersion": "0.3.0","type": "macvlan","master": "%s","mode": "bridge"}`, nicName)
	attachNet := framework.MakeNetworkAttachmentDefinition(externalNetworkName, framework.KubeOvnNamespace, attachConf)
	attachNetClient.Create(attachNet)

	nad := attachNetClient.Get(externalNetworkName)
	framework.ExpectNoError(err, "failed to get")
	ginkgo.By("Got network attachment definition " + nad.Name)

	ginkgo.By("Creating underlay macvlan subnet " + externalNetworkName)
	cidr := make([]string, 0, 2)
	gateway := make([]string, 0, 2)
	for _, config := range dockerExtNetNetwork.IPAM.Config {
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
	macvlanSubnet := framework.MakeSubnet(externalNetworkName, "", strings.Join(cidr, ","), strings.Join(gateway, ","), "", provider, excludeIPs, nil, nil)
	_ = subnetClient.CreateSync(macvlanSubnet)

	ginkgo.By("Getting config map " + vpcNatGWConfigMapName)
	_, err = f.ClientSet.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Get(context.Background(), vpcNatGWConfigMapName, metav1.GetOptions{})
	framework.ExpectNoError(err, "failed to get ConfigMap")

	ginkgo.By("Creating custom vpc")
	vpc := framework.MakeVpc(vpcName, lanIp, false, false, nil)
	_ = vpcClient.CreateSync(vpc)

	ginkgo.By("Creating custom overlay subnet")
	overlaySubnet := framework.MakeSubnet(overlaySubnetName, "", overlaySubnetV4Cidr, overlaySubnetV4Gw, vpcName, "", nil, nil, nil)
	_ = subnetClient.CreateSync(overlaySubnet)

	ginkgo.By("Creating custom vpc nat gw")
	vpcNatGw := framework.MakeVpcNatGateway(vpcNatGwName, vpcName, overlaySubnetName, lanIp, externalNetworkName)
	_ = vpcNatGwClient.CreateSync(vpcNatGw)
}

var _ = framework.Describe("[group:iptables-vpc-nat-gw]", func() {
	f := framework.NewDefaultFramework("iptables-vpc-nat-gw")

	var skip bool
	var cs clientset.Interface
	var attachNetClient *framework.NetworkAttachmentDefinitionClient
	var clusterName, vpcName, vpcNatGwName, overlaySubnetName string
	var vpcClient *framework.VpcClient
	var vpcNatGwClient *framework.VpcNatGatewayClient
	var subnetClient *framework.SubnetClient
	var fipVipName, fipEipName, fipName, dnatVipName, dnatEipName, dnatName, snatEipName, snatName string
	// sharing case
	var sharedVipName, sharedEipName, sharedEipDnatName, sharedEipSnatName, sharedEipFipShoudOkName, sharedEipFipShoudFailName string
	var vipClient *framework.VipClient
	var ipClient *framework.IpClient
	var iptablesEIPClient *framework.IptablesEIPClient
	var iptablesFIPClient *framework.IptablesFIPClient
	var iptablesSnatRuleClient *framework.IptablesSnatClient
	var iptablesDnatRuleClient *framework.IptablesDnatClient

	var dockerExtNet1Network *dockertypes.NetworkResource
	var containerID string
	var image string
	var net1NicName string

	// multiple external network case
	var dockerExtNet2Network *dockertypes.NetworkResource
	var net2NicName string
	var net2AttachDefName string
	var net2SubnetProvider string
	var net2OverlaySubnetName string
	var net2VpcNatGwName string
	var net2VpcName string
	var net2EipName string

	vpcName = "vpc-" + framework.RandomSuffix()
	vpcNatGwName = "gw-" + framework.RandomSuffix()

	fipVipName = "fip-vip-" + framework.RandomSuffix()
	fipEipName = "fip-eip-" + framework.RandomSuffix()
	fipName = "fip-" + framework.RandomSuffix()

	dnatVipName = "dnat-vip-" + framework.RandomSuffix()
	dnatEipName = "dnat-eip-" + framework.RandomSuffix()
	dnatName = "dnat-" + framework.RandomSuffix()

	// sharing case
	sharedVipName = "shared-vip-" + framework.RandomSuffix()
	sharedEipName = "shared-eip-" + framework.RandomSuffix()
	sharedEipDnatName = "shared-eip-dnat-" + framework.RandomSuffix()
	sharedEipSnatName = "shared-eip-snat-" + framework.RandomSuffix()
	sharedEipFipShoudOkName = "shared-eip-fip-should-ok-" + framework.RandomSuffix()
	sharedEipFipShoudFailName = "shared-eip-fip-should-fail-" + framework.RandomSuffix()

	snatEipName = "snat-eip-" + framework.RandomSuffix()
	snatName = "snat-" + framework.RandomSuffix()
	overlaySubnetName = "overlay-subnet-" + framework.RandomSuffix()

	net2AttachDefName = "net2-ovn-vpc-external-network-" + framework.RandomSuffix()
	net2SubnetProvider = net2AttachDefName + ".kube-system"
	net2OverlaySubnetName = "net2-overlay-subnet-" + framework.RandomSuffix()
	net2VpcNatGwName = "net2-gw-" + framework.RandomSuffix()
	net2VpcName = "net2-vpc-" + framework.RandomSuffix()
	net2EipName = "net2-eip-" + framework.RandomSuffix()

	ginkgo.BeforeEach(func() {
		containerID = ""
		cs = f.ClientSet
		attachNetClient = f.NetworkAttachmentDefinitionClient(framework.KubeOvnNamespace)
		subnetClient = f.SubnetClient()
		vpcClient = f.VpcClient()
		vpcNatGwClient = f.VpcNatGatewayClient()
		iptablesEIPClient = f.IptablesEIPClient()
		vipClient = f.VipClient()
		ipClient = f.IpClient()
		iptablesFIPClient = f.IptablesFIPClient()
		iptablesSnatRuleClient = f.IptablesSnatClient()
		iptablesDnatRuleClient = f.IptablesDnatClient()

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

		if dockerExtNet1Network == nil {
			ginkgo.By("Ensuring docker network " + dockerExtNet1Name + " exists")
			network1, err := docker.NetworkCreate(dockerExtNet1Name, true, true)
			framework.ExpectNoError(err, "creating docker network "+dockerExtNet1Name)
			dockerExtNet1Network = network1
		}

		if dockerExtNet2Network == nil {
			ginkgo.By("Ensuring docker network " + dockerExtNet2Name + " exists")
			network2, err := docker.NetworkCreate(dockerExtNet2Name, true, true)
			framework.ExpectNoError(err, "creating docker network "+dockerExtNet2Name)
			dockerExtNet2Network = network2
		}

		ginkgo.By("Getting kind nodes")
		nodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")
		framework.ExpectNotEmpty(nodes)

		ginkgo.By("Connecting nodes to the docker network")
		err = kind.NetworkConnect(dockerExtNet1Network.ID, nodes)
		framework.ExpectNoError(err, "connecting nodes to network "+dockerExtNet1Name)

		ginkgo.By("Connecting nodes to the docker network")
		err = kind.NetworkConnect(dockerExtNet2Network.ID, nodes)
		framework.ExpectNoError(err, "connecting nodes to network "+dockerExtNet2Name)

		ginkgo.By("Getting node links that belong to the docker network")
		nodes, err = kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")

		ginkgo.By("Validating node links")
		network1, err := docker.NetworkInspect(dockerExtNet1Name)
		framework.ExpectNoError(err)
		network2, err := docker.NetworkInspect(dockerExtNet2Name)
		framework.ExpectNoError(err)
		var eth0Exist, net1Exist, net2Exist bool
		for _, node := range nodes {
			links, err := node.ListLinks()
			framework.ExpectNoError(err, "failed to list links on node %s: %v", node.Name(), err)
			net1Mac := network1.Containers[node.ID].MacAddress
			net2Mac := network2.Containers[node.ID].MacAddress
			for _, link := range links {
				ginkgo.By("exist node nic " + link.IfName)
				if link.IfName == "eth0" {
					eth0Exist = true
				}
				if link.Address == net1Mac {
					net1NicName = link.IfName
					net1Exist = true
				}
				if link.Address == net2Mac {
					net2NicName = link.IfName
					net2Exist = true
				}
			}
			framework.ExpectTrue(eth0Exist)
			framework.ExpectTrue(net1Exist)
			framework.ExpectTrue(net2Exist)
		}
	})

	ginkgo.AfterEach(func() {
		if containerID != "" {
			ginkgo.By("Deleting container " + containerID)
			err := docker.ContainerRemove(containerID)
			framework.ExpectNoError(err)
		}

		ginkgo.By("Deleting macvlan underlay subnet " + networkAttachDefName)
		subnetClient.DeleteSync(networkAttachDefName)

		// delete net1 attachment definition
		ginkgo.By("Deleting nad " + networkAttachDefName)
		attachNetClient.Delete(networkAttachDefName)
		// delete net2 attachment definition
		ginkgo.By("Deleting nad " + net2AttachDefName)
		attachNetClient.Delete(net2AttachDefName)

		ginkgo.By("Getting nodes")
		nodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in cluster")

		if dockerExtNet1Network != nil {
			ginkgo.By("Disconnecting nodes from the docker network")
			err = kind.NetworkDisconnect(dockerExtNet1Network.ID, nodes)
			framework.ExpectNoError(err, "disconnecting nodes from network "+dockerExtNet1Name)
		}
		if dockerExtNet2Network != nil {
			ginkgo.By("Disconnecting nodes from the docker network")
			err = kind.NetworkDisconnect(dockerExtNet2Network.ID, nodes)
			framework.ExpectNoError(err, "disconnecting nodes from network "+dockerExtNet2Name)
		}
	})

	framework.ConformanceIt("iptables eip fip snat dnat", func() {
		overlaySubnetV4Cidr := "192.168.0.0/24"
		overlaySubnetV4Gw := "192.168.0.1"
		lanIp := "192.168.0.254"
		setupVpcNatGwTestEnvironment(
			f, dockerExtNet1Network, attachNetClient,
			subnetClient, vpcClient, vpcNatGwClient,
			vpcName, overlaySubnetName, vpcNatGwName,
			overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIp,
			dockerExtNet1Name, networkAttachDefName, net1NicName,
			externalSubnetProvider,
		)

		ginkgo.By("Creating iptables vip for fip")
		fipVip := framework.MakeVip(fipVipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(fipVip)
		fipVip = vipClient.Get(fipVipName)
		ginkgo.By("Creating iptables eip for fip")
		fipEip := framework.MakeIptablesEIP(fipEipName, "", "", "", vpcNatGwName, "")
		_ = iptablesEIPClient.CreateSync(fipEip)
		ginkgo.By("Creating iptables fip")
		fip := framework.MakeIptablesFIPRule(fipName, fipEipName, fipVip.Status.V4ip)
		_ = iptablesFIPClient.CreateSync(fip)

		ginkgo.By("Creating iptables eip for snat")
		snatEip := framework.MakeIptablesEIP(snatEipName, "", "", "", vpcNatGwName, "")
		_ = iptablesEIPClient.CreateSync(snatEip)
		ginkgo.By("Creating iptables snat")
		snat := framework.MakeIptablesSnatRule(snatName, snatEipName, overlaySubnetV4Cidr)
		_ = iptablesSnatRuleClient.CreateSync(snat)

		ginkgo.By("Creating iptables vip for dnat")
		dnatVip := framework.MakeVip(dnatVipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(dnatVip)
		dnatVip = vipClient.Get(dnatVipName)
		ginkgo.By("Creating iptables eip for dnat")
		dnatEip := framework.MakeIptablesEIP(dnatEipName, "", "", "", vpcNatGwName, "")
		_ = iptablesEIPClient.CreateSync(dnatEip)
		ginkgo.By("Creating iptables dnat")
		dnat := framework.MakeIptablesDnatRule(dnatName, dnatEipName, "80", "tcp", dnatVip.Status.V4ip, "8080")
		_ = iptablesDnatRuleClient.CreateSync(dnat)

		// share eip case
		ginkgo.By("Creating share vip")
		shareVip := framework.MakeVip(sharedVipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(shareVip)
		fipVip = vipClient.Get(fipVipName)
		ginkgo.By("Creating share iptables eip")
		shareEip := framework.MakeIptablesEIP(sharedEipName, "", "", "", vpcNatGwName, "")
		_ = iptablesEIPClient.CreateSync(shareEip)
		ginkgo.By("Creating the first iptables fip with share eip vip should be ok")
		shareFipShouldOk := framework.MakeIptablesFIPRule(sharedEipFipShoudOkName, sharedEipName, fipVip.Status.V4ip)
		_ = iptablesFIPClient.CreateSync(shareFipShouldOk)
		ginkgo.By("Creating the second iptables fip with share eip vip should be failed")
		shareFipShouldFail := framework.MakeIptablesFIPRule(sharedEipFipShoudFailName, sharedEipName, fipVip.Status.V4ip)
		_ = iptablesFIPClient.Create(shareFipShouldFail)
		ginkgo.By("Creating iptables dnat for dnat with share eip vip")
		shareDnat := framework.MakeIptablesDnatRule(sharedEipDnatName, sharedEipName, "80", "tcp", fipVip.Status.V4ip, "8080")
		_ = iptablesDnatRuleClient.CreateSync(shareDnat)
		ginkgo.By("Creating iptables snat with share eip vip")
		shareSnat := framework.MakeIptablesSnatRule(sharedEipSnatName, sharedEipName, overlaySubnetV4Cidr)
		_ = iptablesSnatRuleClient.CreateSync(shareSnat)

		ginkgo.By("Get share eip")
		shareEip = iptablesEIPClient.Get(sharedEipName)
		ginkgo.By("Get share dnat")
		shareDnat = iptablesDnatRuleClient.Get(sharedEipDnatName)
		ginkgo.By("Get share snat")
		shareSnat = iptablesSnatRuleClient.Get(sharedEipSnatName)
		ginkgo.By("Get share fip should ok")
		shareFipShouldOk = iptablesFIPClient.Get(sharedEipFipShoudOkName)
		ginkgo.By("Get share fip should fail")
		shareFipShouldFail = iptablesFIPClient.Get(sharedEipFipShoudFailName)

		ginkgo.By("Check share eip should has the external ip label")
		framework.ExpectHaveKeyWithValue(shareEip.Labels, util.IptablesEipV4IPLabel, shareEip.Spec.V4ip)
		ginkgo.By("Check share dnat should has the external ip label")
		framework.ExpectHaveKeyWithValue(shareDnat.Labels, util.IptablesEipV4IPLabel, shareEip.Spec.V4ip)
		ginkgo.By("Check share snat should has the external ip label")
		framework.ExpectHaveKeyWithValue(shareSnat.Labels, util.IptablesEipV4IPLabel, shareEip.Spec.V4ip)
		ginkgo.By("Check share fip should ok should has the external ip label")
		framework.ExpectHaveKeyWithValue(shareFipShouldOk.Labels, util.IptablesEipV4IPLabel, shareEip.Spec.V4ip)
		ginkgo.By("Check share fip should fail should not be ready")
		framework.ExpectEqual(shareFipShouldFail.Status.Ready, false)

		// define a list and strings join
		nats := []string{util.DnatUsingEip, util.FipUsingEip, util.SnatUsingEip}
		framework.ExpectEqual(shareEip.Status.Nat, strings.Join(nats, ","))

		ginkgo.By("Deleting share iptables fip " + sharedEipFipShoudOkName)
		iptablesFIPClient.DeleteSync(sharedEipFipShoudOkName)
		ginkgo.By("Deleting share iptables fip " + sharedEipFipShoudFailName)
		iptablesFIPClient.DeleteSync(sharedEipFipShoudFailName)
		ginkgo.By("Deleting share iptables dnat " + dnatName)
		iptablesDnatRuleClient.DeleteSync(dnatName)
		ginkgo.By("Deleting share iptables snat " + snatName)
		iptablesSnatRuleClient.DeleteSync(snatName)

		ginkgo.By("Deleting iptables fip " + fipName)
		iptablesFIPClient.DeleteSync(fipName)
		ginkgo.By("Deleting iptables dnat " + dnatName)
		iptablesDnatRuleClient.DeleteSync(dnatName)
		ginkgo.By("Deleting iptables snat " + snatName)
		iptablesSnatRuleClient.DeleteSync(snatName)

		ginkgo.By("Deleting iptables eip " + fipEipName)
		iptablesEIPClient.DeleteSync(fipEipName)
		ginkgo.By("Deleting iptables eip " + dnatEipName)
		iptablesEIPClient.DeleteSync(dnatEipName)
		ginkgo.By("Deleting iptables eip " + snatEipName)
		iptablesEIPClient.DeleteSync(snatEipName)
		ginkgo.By("Deleting iptables share eip " + sharedEipName)
		iptablesEIPClient.DeleteSync(sharedEipName)

		ginkgo.By("Deleting vip " + fipVipName)
		vipClient.DeleteSync(fipVipName)
		ginkgo.By("Deleting vip " + dnatVipName)
		vipClient.DeleteSync(dnatVipName)
		ginkgo.By("Deleting vip " + sharedVipName)
		vipClient.DeleteSync(sharedVipName)

		ginkgo.By("Deleting custom vpc " + vpcName)
		vpcClient.DeleteSync(vpcName)

		ginkgo.By("Deleting custom vpc nat gw")
		vpcNatGwClient.DeleteSync(vpcNatGwName)

		// the only pod for vpc nat gateway
		vpcNatGwPodName := "vpc-nat-gw-" + vpcNatGwName + "-0"

		// delete vpc nat gw statefulset remaining ip for eth0 and net1
		overlaySubnet := subnetClient.Get(overlaySubnetName)
		macvlanSubnet := subnetClient.Get(networkAttachDefName)
		eth0IpName := ovs.PodNameToPortName(vpcNatGwPodName, framework.KubeOvnNamespace, overlaySubnet.Spec.Provider)
		net1IpName := ovs.PodNameToPortName(vpcNatGwPodName, framework.KubeOvnNamespace, macvlanSubnet.Spec.Provider)
		ginkgo.By("Deleting vpc nat gw eth0 ip " + eth0IpName)
		ipClient.DeleteSync(eth0IpName)
		ginkgo.By("Deleting vpc nat gw net1 ip " + net1IpName)
		ipClient.DeleteSync(net1IpName)

		ginkgo.By("Deleting overlay subnet " + overlaySubnetName)
		subnetClient.DeleteSync(overlaySubnetName)

		// multiple external network case
		net2OverlaySubnetV4Cidr := "192.168.1.0/24"
		net2OoverlaySubnetV4Gw := "192.168.1.1"
		net2LanIp := "192.168.1.254"
		setupVpcNatGwTestEnvironment(
			f, dockerExtNet2Network, attachNetClient,
			subnetClient, vpcClient, vpcNatGwClient,
			net2VpcName, net2OverlaySubnetName, net2VpcNatGwName,
			net2OverlaySubnetV4Cidr, net2OoverlaySubnetV4Gw, net2LanIp,
			dockerExtNet2Name, net2AttachDefName, net2NicName,
			net2SubnetProvider)

		ginkgo.By("Creating iptables eip of net2")
		net2Eip := framework.MakeIptablesEIP(net2EipName, "", "", "", net2VpcNatGwName, net2AttachDefName)
		_ = iptablesEIPClient.CreateSync(net2Eip)

		ginkgo.By("Deleting iptables eip " + net2EipName)
		iptablesEIPClient.DeleteSync(net2EipName)

		ginkgo.By("Deleting custom vpc " + net2VpcName)
		vpcClient.DeleteSync(net2VpcName)

		ginkgo.By("Deleting custom vpc nat gw")
		vpcNatGwClient.DeleteSync(net2VpcNatGwName)

		// the only pod for vpc nat gateway
		vpcNatGwPodName = "vpc-nat-gw-" + net2VpcNatGwName + "-0"

		// delete vpc nat gw statefulset remaining ip for eth0 and net2
		overlaySubnet = subnetClient.Get(net2OverlaySubnetName)
		macvlanSubnet = subnetClient.Get(net2AttachDefName)
		eth0IpName = ovs.PodNameToPortName(vpcNatGwPodName, framework.KubeOvnNamespace, overlaySubnet.Spec.Provider)
		net2IpName := ovs.PodNameToPortName(vpcNatGwPodName, framework.KubeOvnNamespace, macvlanSubnet.Spec.Provider)
		ginkgo.By("Deleting vpc nat gw eth0 ip " + eth0IpName)
		ipClient.DeleteSync(eth0IpName)
		ginkgo.By("Deleting vpc nat gw net2 ip " + net2IpName)
		ipClient.DeleteSync(net2IpName)

		ginkgo.By("Deleting macvlan underlay subnet " + net2AttachDefName)
		subnetClient.DeleteSync(net2AttachDefName)

		ginkgo.By("Deleting overlay subnet " + net2OverlaySubnetName)
		subnetClient.DeleteSync(net2OverlaySubnetName)
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
