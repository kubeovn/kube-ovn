package ovn_eip

import (
	"context"
	"errors"
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

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

const (
	dockerExtNet1Name      = "kube-ovn-ext-net1"
	dockerExtNet2Name      = "kube-ovn-ext-net2"
	vpcNatGWConfigMapName  = "ovn-vpc-nat-gw-config"
	networkAttachDefName   = "ovn-vpc-external-network"
	externalSubnetProvider = "ovn-vpc-external-network.kube-system"
)

const (
	iperf2Port = "20288"
	skipIperf  = false
)

const (
	eipLimit = iota*5 + 10
	updatedEIPLimit
	newEIPLimit
	specificIPLimit
	defaultNicLimit
)

type qosParams struct {
	vpc1Name       string
	vpc2Name       string
	vpc1SubnetName string
	vpc2SubnetName string
	vpcNat1GwName  string
	vpcNat2GwName  string
	vpc1EIPName    string
	vpc2EIPName    string
	vpc1FIPName    string
	vpc2FIPName    string
	vpc1PodName    string
	vpc2PodName    string
	attachDefName  string
	subnetProvider string
}

func setupNetworkAttachmentDefinition(
	f *framework.Framework,
	dockerExtNetNetwork *dockertypes.NetworkResource,
	attachNetClient *framework.NetworkAttachmentDefinitionClient,
	subnetClient *framework.SubnetClient,
	externalNetworkName string,
	nicName string,
	provider string,
	dockerExtNetName string,
) {
	ginkgo.By("Getting docker network " + dockerExtNetName)
	network, err := docker.NetworkInspect(dockerExtNetName)
	framework.ExpectNoError(err, "getting docker network "+dockerExtNetName)
	ginkgo.By("Getting network attachment definition " + externalNetworkName)
	attachConf := fmt.Sprintf(`{
		"cniVersion": "0.3.0",
		"type": "macvlan",
		"master": "%s",
		"mode": "bridge",
		"ipam": {
		  "type": "kube-ovn",
		  "server_socket": "/run/openvswitch/kube-ovn-daemon.sock",
		  "provider": "%s"
		}
	  }`, nicName, provider)
	attachNet := framework.MakeNetworkAttachmentDefinition(externalNetworkName, framework.KubeOvnNamespace, attachConf)
	attachNetClient.Create(attachNet)
	nad := attachNetClient.Get(externalNetworkName)

	ginkgo.By("Got network attachment definition " + nad.Name)

	ginkgo.By("Creating underlay macvlan subnet " + externalNetworkName)
	cidr := make([]string, 0, 2)
	gateway := make([]string, 0, 2)
	for _, config := range dockerExtNetNetwork.IPAM.Config {
		switch util.CheckProtocol(config.Subnet) {
		case apiv1.ProtocolIPv4:
			if f.HasIPv4() {
				cidr = append(cidr, config.Subnet)
				gateway = append(gateway, config.Gateway)
			}
		case apiv1.ProtocolIPv6:
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
	macvlanSubnet := framework.MakeSubnet(externalNetworkName, "", strings.Join(cidr, ","), strings.Join(gateway, ","), "", provider, excludeIPs, nil, nil)
	_ = subnetClient.CreateSync(macvlanSubnet)
}

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
	natGwQosPolicy string,
	overlaySubnetV4Cidr string,
	overlaySubnetV4Gw string,
	lanIP string,
	dockerExtNetName string,
	externalNetworkName string,
	nicName string,
	provider string,
	skipNADSetup bool,
) {
	if !skipNADSetup {
		setupNetworkAttachmentDefinition(
			f, dockerExtNetNetwork, attachNetClient,
			subnetClient, externalNetworkName, nicName, provider, dockerExtNetName)
	}

	ginkgo.By("Getting config map " + vpcNatGWConfigMapName)
	_, err := f.ClientSet.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Get(context.Background(), vpcNatGWConfigMapName, metav1.GetOptions{})
	framework.ExpectNoError(err, "failed to get ConfigMap")

	ginkgo.By("Creating custom vpc " + vpcName)
	vpc := framework.MakeVpc(vpcName, lanIP, false, false, nil)
	_ = vpcClient.CreateSync(vpc)

	ginkgo.By("Creating custom overlay subnet " + overlaySubnetName)
	overlaySubnet := framework.MakeSubnet(overlaySubnetName, "", overlaySubnetV4Cidr, overlaySubnetV4Gw, vpcName, "", nil, nil, nil)
	_ = subnetClient.CreateSync(overlaySubnet)

	ginkgo.By("Creating custom vpc nat gw " + vpcNatGwName)
	vpcNatGw := framework.MakeVpcNatGateway(vpcNatGwName, vpcName, overlaySubnetName, lanIP, externalNetworkName, natGwQosPolicy)
	_ = vpcNatGwClient.CreateSync(vpcNatGw, f.ClientSet)
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
	var ipClient *framework.IPClient
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
	net2SubnetProvider = fmt.Sprintf("%s.%s", net2AttachDefName, framework.KubeOvnNamespace)
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
		ipClient = f.IPClient()
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
		overlaySubnetV4Cidr := "10.0.0.0/24"
		overlaySubnetV4Gw := "10.0.0.1"
		lanIP := "10.0.0.254"
		natgwQoS := ""
		setupVpcNatGwTestEnvironment(
			f, dockerExtNet1Network, attachNetClient,
			subnetClient, vpcClient, vpcNatGwClient,
			vpcName, overlaySubnetName, vpcNatGwName, natgwQoS,
			overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIP,
			dockerExtNet1Name, networkAttachDefName, net1NicName,
			externalSubnetProvider,
			false,
		)

		ginkgo.By("Creating iptables vip for fip")
		fipVip := framework.MakeVip(f.Namespace.Name, fipVipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(fipVip)
		fipVip = vipClient.Get(fipVipName)
		ginkgo.By("Creating iptables eip for fip")
		fipEip := framework.MakeIptablesEIP(fipEipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(fipEip)
		ginkgo.By("Creating iptables fip")
		fip := framework.MakeIptablesFIPRule(fipName, fipEipName, fipVip.Status.V4ip)
		_ = iptablesFIPClient.CreateSync(fip)

		ginkgo.By("Creating iptables eip for snat")
		snatEip := framework.MakeIptablesEIP(snatEipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(snatEip)
		ginkgo.By("Creating iptables snat")
		snat := framework.MakeIptablesSnatRule(snatName, snatEipName, overlaySubnetV4Cidr)
		_ = iptablesSnatRuleClient.CreateSync(snat)

		ginkgo.By("Creating iptables vip for dnat")
		dnatVip := framework.MakeVip(f.Namespace.Name, dnatVipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(dnatVip)
		dnatVip = vipClient.Get(dnatVipName)
		ginkgo.By("Creating iptables eip for dnat")
		dnatEip := framework.MakeIptablesEIP(dnatEipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(dnatEip)
		ginkgo.By("Creating iptables dnat")
		dnat := framework.MakeIptablesDnatRule(dnatName, dnatEipName, "80", "tcp", dnatVip.Status.V4ip, "8080")
		_ = iptablesDnatRuleClient.CreateSync(dnat)

		// share eip case
		ginkgo.By("Creating share vip")
		shareVip := framework.MakeVip(f.Namespace.Name, sharedVipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(shareVip)
		fipVip = vipClient.Get(fipVipName)
		ginkgo.By("Creating share iptables eip")
		shareEip := framework.MakeIptablesEIP(sharedEipName, "", "", "", vpcNatGwName, "", "")
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
		framework.ExpectHaveKeyWithValue(shareEip.Labels, util.EipV4IpLabel, shareEip.Spec.V4ip)
		ginkgo.By("Check share dnat should has the external ip label")
		framework.ExpectHaveKeyWithValue(shareDnat.Labels, util.EipV4IpLabel, shareEip.Spec.V4ip)
		ginkgo.By("Check share snat should has the external ip label")
		framework.ExpectHaveKeyWithValue(shareSnat.Labels, util.EipV4IpLabel, shareEip.Spec.V4ip)
		ginkgo.By("Check share fip should ok should has the external ip label")
		framework.ExpectHaveKeyWithValue(shareFipShouldOk.Labels, util.EipV4IpLabel, shareEip.Spec.V4ip)
		ginkgo.By("Check share fip should fail should not be ready")
		framework.ExpectEqual(shareFipShouldFail.Status.Ready, false)

		// make sure eip is shared
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
		vpcNatGwPodName := util.GenNatGwPodName(vpcNatGwName)

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
		net2OverlaySubnetV4Cidr := "10.0.1.0/24"
		net2OoverlaySubnetV4Gw := "10.0.1.1"
		net2LanIP := "10.0.1.254"
		natgwQoS = ""
		setupVpcNatGwTestEnvironment(
			f, dockerExtNet2Network, attachNetClient,
			subnetClient, vpcClient, vpcNatGwClient,
			net2VpcName, net2OverlaySubnetName, net2VpcNatGwName, natgwQoS,
			net2OverlaySubnetV4Cidr, net2OoverlaySubnetV4Gw, net2LanIP,
			dockerExtNet2Name, net2AttachDefName, net2NicName,
			net2SubnetProvider,
			false,
		)

		ginkgo.By("Creating iptables eip of net2")
		net2Eip := framework.MakeIptablesEIP(net2EipName, "", "", "", net2VpcNatGwName, net2AttachDefName, "")
		_ = iptablesEIPClient.CreateSync(net2Eip)

		ginkgo.By("Deleting iptables eip " + net2EipName)
		iptablesEIPClient.DeleteSync(net2EipName)

		ginkgo.By("Deleting custom vpc " + net2VpcName)
		vpcClient.DeleteSync(net2VpcName)

		ginkgo.By("Deleting custom vpc nat gw")
		vpcNatGwClient.DeleteSync(net2VpcNatGwName)

		// the only pod for vpc nat gateway
		vpcNatGwPodName = util.GenNatGwPodName(net2VpcNatGwName)

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

func iperf(f *framework.Framework, iperfClientPod *corev1.Pod, iperfServerEIP *apiv1.IptablesEIP) string {
	for i := 0; i < 20; i++ {
		command := fmt.Sprintf("iperf -e -p %s --reportstyle C -i 1 -c %s -t 10", iperf2Port, iperfServerEIP.Status.IP)
		stdOutput, errOutput, err := framework.ExecShellInPod(context.Background(), f, iperfClientPod.Namespace, iperfClientPod.Name, command)
		framework.Logf("output from exec on client pod %s (eip %s)\n", iperfClientPod.Name, iperfServerEIP.Name)
		if stdOutput != "" && err == nil {
			framework.Logf("output:\n%s", stdOutput)
			return stdOutput
		}
		framework.Logf("exec %s failed err: %v, errOutput: %s, stdOutput: %s, retried %d times.", command, err, errOutput, stdOutput, i)
		time.Sleep(6 * time.Second)
	}
	framework.ExpectNoError(errors.New("iperf failed"))
	return ""
}

func checkQos(f *framework.Framework,
	vpc1Pod, vpc2Pod *corev1.Pod, vpc1EIP, vpc2EIP *apiv1.IptablesEIP,
	limit int, expect bool,
) {
	if !skipIperf {
		if expect {
			output := iperf(f, vpc1Pod, vpc2EIP)
			framework.ExpectTrue(vaildRateLimit(output, limit))
			output = iperf(f, vpc2Pod, vpc1EIP)
			framework.ExpectTrue(vaildRateLimit(output, limit))
		} else {
			output := iperf(f, vpc1Pod, vpc2EIP)
			framework.ExpectFalse(vaildRateLimit(output, limit))
			output = iperf(f, vpc2Pod, vpc1EIP)
			framework.ExpectFalse(vaildRateLimit(output, limit))
		}
	}
}

func newVPCQoSParamsInit() *qosParams {
	qosParames := &qosParams{
		vpc1Name:       "qos-vpc1-" + framework.RandomSuffix(),
		vpc2Name:       "qos-vpc2-" + framework.RandomSuffix(),
		vpc1SubnetName: "qos-vpc1-subnet-" + framework.RandomSuffix(),
		vpc2SubnetName: "qos-vpc2-subnet-" + framework.RandomSuffix(),
		vpcNat1GwName:  "qos-vpc1-gw-" + framework.RandomSuffix(),
		vpcNat2GwName:  "qos-vpc2-gw-" + framework.RandomSuffix(),
		vpc1EIPName:    "qos-vpc1-eip-" + framework.RandomSuffix(),
		vpc2EIPName:    "qos-vpc2-eip-" + framework.RandomSuffix(),
		vpc1FIPName:    "qos-vpc1-fip-" + framework.RandomSuffix(),
		vpc2FIPName:    "qos-vpc2-fip-" + framework.RandomSuffix(),
		vpc1PodName:    "qos-vpc1-pod-" + framework.RandomSuffix(),
		vpc2PodName:    "qos-vpc2-pod-" + framework.RandomSuffix(),
		attachDefName:  "qos-ovn-vpc-external-network-" + framework.RandomSuffix(),
	}
	qosParames.subnetProvider = fmt.Sprintf("%s.%s", qosParames.attachDefName, framework.KubeOvnNamespace)
	return qosParames
}

func getNicDefaultQoSPolicy(limit int) apiv1.QoSPolicyBandwidthLimitRules {
	return apiv1.QoSPolicyBandwidthLimitRules{
		&apiv1.QoSPolicyBandwidthLimitRule{
			Name:      "net1-ingress",
			Interface: "net1",
			RateMax:   fmt.Sprint(limit),
			BurstMax:  fmt.Sprint(limit),
			Priority:  3,
			Direction: apiv1.DirectionIngress,
		},
		&apiv1.QoSPolicyBandwidthLimitRule{
			Name:      "net1-egress",
			Interface: "net1",
			RateMax:   fmt.Sprint(limit),
			BurstMax:  fmt.Sprint(limit),
			Priority:  3,
			Direction: apiv1.DirectionEgress,
		},
	}
}

func getEIPQoSRule(limit int) apiv1.QoSPolicyBandwidthLimitRules {
	return apiv1.QoSPolicyBandwidthLimitRules{
		&apiv1.QoSPolicyBandwidthLimitRule{
			Name:      "eip-ingress",
			RateMax:   fmt.Sprint(limit),
			BurstMax:  fmt.Sprint(limit),
			Priority:  1,
			Direction: apiv1.DirectionIngress,
		},
		&apiv1.QoSPolicyBandwidthLimitRule{
			Name:      "eip-egress",
			RateMax:   fmt.Sprint(limit),
			BurstMax:  fmt.Sprint(limit),
			Priority:  1,
			Direction: apiv1.DirectionEgress,
		},
	}
}

func getSpecialQoSRule(limit int, ip string) apiv1.QoSPolicyBandwidthLimitRules {
	return apiv1.QoSPolicyBandwidthLimitRules{
		&apiv1.QoSPolicyBandwidthLimitRule{
			Name:       "net1-extip-ingress",
			Interface:  "net1",
			RateMax:    fmt.Sprint(limit),
			BurstMax:   fmt.Sprint(limit),
			Priority:   2,
			Direction:  apiv1.DirectionIngress,
			MatchType:  apiv1.MatchTypeIP,
			MatchValue: "src " + ip + "/32",
		},
		&apiv1.QoSPolicyBandwidthLimitRule{
			Name:       "net1-extip-egress",
			Interface:  "net1",
			RateMax:    fmt.Sprint(limit),
			BurstMax:   fmt.Sprint(limit),
			Priority:   2,
			Direction:  apiv1.DirectionEgress,
			MatchType:  apiv1.MatchTypeIP,
			MatchValue: "dst " + ip + "/32",
		},
	}
}

// defaultQoSCases test default qos policy=
func defaultQoSCases(f *framework.Framework,
	vpcNatGwClient *framework.VpcNatGatewayClient,
	podClient *framework.PodClient,
	qosPolicyClient *framework.QoSPolicyClient,
	vpc1Pod *corev1.Pod,
	vpc2Pod *corev1.Pod,
	vpc1EIP *apiv1.IptablesEIP,
	vpc2EIP *apiv1.IptablesEIP,
	natgwName string,
) {
	// create nic qos policy
	qosPolicyName := "default-nic-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + qosPolicyName)
	rules := getNicDefaultQoSPolicy(defaultNicLimit)

	qosPolicy := framework.MakeQoSPolicy(qosPolicyName, true, apiv1.QoSBindingTypeNatGw, rules)
	_ = qosPolicyClient.CreateSync(qosPolicy)

	ginkgo.By("Patch natgw " + natgwName + " with qos policy " + qosPolicyName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, qosPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + fmt.Sprint(defaultNicLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, true)

	ginkgo.By("Delete natgw pod " + natgwName + "-0")
	natGwPodName := util.GenNatGwPodName(natgwName)
	podClient.DeleteSync(natGwPodName)

	ginkgo.By("Wait for natgw " + natgwName + "qos rebuid")
	time.Sleep(5 * time.Second)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + fmt.Sprint(defaultNicLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, true)

	ginkgo.By("Remove qos policy " + qosPolicyName + " from natgw " + natgwName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, "")

	ginkgo.By("Deleting qos policy " + qosPolicyName)
	qosPolicyClient.DeleteSync(qosPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is not limited to " + fmt.Sprint(defaultNicLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, false)
}

// eipQoSCases test default qos policy
func eipQoSCases(f *framework.Framework,
	eipClient *framework.IptablesEIPClient,
	podClient *framework.PodClient,
	qosPolicyClient *framework.QoSPolicyClient,
	vpc1Pod *corev1.Pod,
	vpc2Pod *corev1.Pod,
	vpc1EIP *apiv1.IptablesEIP,
	vpc2EIP *apiv1.IptablesEIP,
	eipName string,
	natgwName string,
) {
	// create eip qos policy
	qosPolicyName := "eip-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + qosPolicyName)
	rules := getEIPQoSRule(eipLimit)

	qosPolicy := framework.MakeQoSPolicy(qosPolicyName, false, apiv1.QoSBindingTypeEIP, rules)
	qosPolicy = qosPolicyClient.CreateSync(qosPolicy)

	ginkgo.By("Patch eip " + eipName + " with qos policy " + qosPolicyName)
	_ = eipClient.PatchQoSPolicySync(eipName, qosPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + fmt.Sprint(eipLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, eipLimit, true)

	ginkgo.By("Update qos policy " + qosPolicyName + " with new rate limit")

	rules = getEIPQoSRule(updatedEIPLimit)
	modifiedqosPolicy := qosPolicy.DeepCopy()
	modifiedqosPolicy.Spec.BandwidthLimitRules = rules
	qosPolicyClient.Patch(qosPolicy, modifiedqosPolicy)
	qosPolicyClient.WaitToQoSReady(qosPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is changed to " + fmt.Sprint(updatedEIPLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, updatedEIPLimit, true)

	ginkgo.By("Delete natgw pod " + natgwName + "-0")
	natGwPodName := util.GenNatGwPodName(natgwName)
	podClient.DeleteSync(natGwPodName)

	ginkgo.By("Wait for natgw " + natgwName + "qos rebuid")
	time.Sleep(5 * time.Second)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + fmt.Sprint(updatedEIPLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, updatedEIPLimit, true)

	newQoSPolicyName := "new-eip-qos-policy-" + framework.RandomSuffix()
	newRules := getEIPQoSRule(newEIPLimit)
	newQoSPolicy := framework.MakeQoSPolicy(newQoSPolicyName, false, apiv1.QoSBindingTypeEIP, newRules)
	_ = qosPolicyClient.CreateSync(newQoSPolicy)

	ginkgo.By("Change qos policy of eip " + eipName + " to " + newQoSPolicyName)
	_ = eipClient.PatchQoSPolicySync(eipName, newQoSPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + fmt.Sprint(newEIPLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, newEIPLimit, true)

	ginkgo.By("Remove qos policy " + qosPolicyName + " from natgw " + eipName)
	_ = eipClient.PatchQoSPolicySync(eipName, "")

	ginkgo.By("Deleting qos policy " + qosPolicyName)
	qosPolicyClient.DeleteSync(qosPolicyName)

	ginkgo.By("Deleting qos policy " + newQoSPolicyName)
	qosPolicyClient.DeleteSync(newQoSPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is not limited to " + fmt.Sprint(newEIPLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, newEIPLimit, false)
}

// specifyingIPQoSCases test default qos policy
func specifyingIPQoSCases(f *framework.Framework,
	vpcNatGwClient *framework.VpcNatGatewayClient,
	qosPolicyClient *framework.QoSPolicyClient,
	vpc1Pod *corev1.Pod,
	vpc2Pod *corev1.Pod,
	vpc1EIP *apiv1.IptablesEIP,
	vpc2EIP *apiv1.IptablesEIP,
	natgwName string,
) {
	// create nic qos policy
	qosPolicyName := "specifying-ip-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + qosPolicyName)

	rules := getSpecialQoSRule(specificIPLimit, vpc2EIP.Status.IP)

	qosPolicy := framework.MakeQoSPolicy(qosPolicyName, true, apiv1.QoSBindingTypeNatGw, rules)
	_ = qosPolicyClient.CreateSync(qosPolicy)

	ginkgo.By("Patch natgw " + natgwName + " with qos policy " + qosPolicyName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, qosPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + fmt.Sprint(specificIPLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, specificIPLimit, true)

	ginkgo.By("Remove qos policy " + qosPolicyName + " from natgw " + natgwName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, "")

	ginkgo.By("Deleting qos policy " + qosPolicyName)
	qosPolicyClient.DeleteSync(qosPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is not limited to " + fmt.Sprint(specificIPLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, specificIPLimit, false)
}

// priorityQoSCases test qos match priority
func priorityQoSCases(f *framework.Framework,
	vpcNatGwClient *framework.VpcNatGatewayClient,
	eipClient *framework.IptablesEIPClient,
	qosPolicyClient *framework.QoSPolicyClient,
	vpc1Pod *corev1.Pod,
	vpc2Pod *corev1.Pod,
	vpc1EIP *apiv1.IptablesEIP,
	vpc2EIP *apiv1.IptablesEIP,
	natgwName string,
	eipName string,
) {
	// create nic qos policy
	natGwQoSPolicyName := "priority-nic-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + natGwQoSPolicyName)
	// default qos policy + special qos policy
	natgwRules := getNicDefaultQoSPolicy(defaultNicLimit)
	natgwRules = append(natgwRules, getSpecialQoSRule(specificIPLimit, vpc2EIP.Status.IP)...)

	natgwQoSPolicy := framework.MakeQoSPolicy(natGwQoSPolicyName, true, apiv1.QoSBindingTypeNatGw, natgwRules)
	_ = qosPolicyClient.CreateSync(natgwQoSPolicy)

	ginkgo.By("Patch natgw " + natgwName + " with qos policy " + natGwQoSPolicyName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, natGwQoSPolicyName)

	eipQoSPolicyName := "eip-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + eipQoSPolicyName)
	eipRules := getEIPQoSRule(eipLimit)

	eipQoSPolicy := framework.MakeQoSPolicy(eipQoSPolicyName, false, apiv1.QoSBindingTypeEIP, eipRules)
	_ = qosPolicyClient.CreateSync(eipQoSPolicy)

	ginkgo.By("Patch eip " + eipName + " with qos policy " + eipQoSPolicyName)
	_ = eipClient.PatchQoSPolicySync(eipName, eipQoSPolicyName)

	// match qos of priority 1
	ginkgo.By("Check qos to match priority 1 is limited to " + fmt.Sprint(eipLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, eipLimit, true)

	ginkgo.By("Remove qos policy " + eipQoSPolicyName + " from natgw " + eipName)
	_ = eipClient.PatchQoSPolicySync(eipName, "")

	ginkgo.By("Deleting qos policy " + eipQoSPolicyName)
	qosPolicyClient.DeleteSync(eipQoSPolicyName)

	// match qos of priority 2
	ginkgo.By("Check qos to match priority 2 is limited to " + fmt.Sprint(specificIPLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, specificIPLimit, true)

	// change qos policy of natgw
	newNatGwQoSPolicyName := "new-priority-nic-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + newNatGwQoSPolicyName)
	newNatgwRules := getNicDefaultQoSPolicy(defaultNicLimit)

	newNatgwQoSPolicy := framework.MakeQoSPolicy(newNatGwQoSPolicyName, true, apiv1.QoSBindingTypeNatGw, newNatgwRules)
	_ = qosPolicyClient.CreateSync(newNatgwQoSPolicy)

	ginkgo.By("Change qos policy of natgw " + natgwName + " to " + newNatGwQoSPolicyName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, newNatGwQoSPolicyName)

	// match qos of priority 3
	ginkgo.By("Check qos to match priority 3 is limited to " + fmt.Sprint(specificIPLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, true)

	ginkgo.By("Remove qos policy " + natGwQoSPolicyName + " from natgw " + natgwName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, "")

	ginkgo.By("Deleting qos policy " + natGwQoSPolicyName)
	qosPolicyClient.DeleteSync(natGwQoSPolicyName)

	ginkgo.By("Deleting qos policy " + newNatGwQoSPolicyName)
	qosPolicyClient.DeleteSync(newNatGwQoSPolicyName)

	ginkgo.By("Check qos " + natGwQoSPolicyName + " is not limited to " + fmt.Sprint(defaultNicLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, false)
}

func createNatGwAndSetQosCases(f *framework.Framework,
	vpcNatGwClient *framework.VpcNatGatewayClient,
	ipClient *framework.IPClient,
	eipClient *framework.IptablesEIPClient,
	fipClient *framework.IptablesFIPClient,
	subnetClient *framework.SubnetClient,
	qosPolicyClient *framework.QoSPolicyClient,
	vpc1Pod *corev1.Pod,
	vpc2Pod *corev1.Pod,
	vpc2EIP *apiv1.IptablesEIP,
	natgwName string,
	eipName string,
	fipName string,
	vpcName string,
	overlaySubnetName string,
	lanIP string,
	attachDefName string,
) {
	// delete fip
	ginkgo.By("Deleting fip " + fipName)
	fipClient.DeleteSync(fipName)

	ginkgo.By("Deleting eip " + eipName)
	eipClient.DeleteSync(eipName)

	// the only pod for vpc nat gateway
	vpcNatGw1PodName := util.GenNatGwPodName(natgwName)

	// delete vpc nat gw statefulset remaining ip for eth0 and net2
	ginkgo.By("Deleting custom vpc nat gw " + natgwName)
	vpcNatGwClient.DeleteSync(natgwName)

	overlaySubnet1 := subnetClient.Get(overlaySubnetName)
	macvlanSubnet := subnetClient.Get(attachDefName)
	eth0IpName := ovs.PodNameToPortName(vpcNatGw1PodName, framework.KubeOvnNamespace, overlaySubnet1.Spec.Provider)
	net1IpName := ovs.PodNameToPortName(vpcNatGw1PodName, framework.KubeOvnNamespace, macvlanSubnet.Spec.Provider)
	ginkgo.By("Deleting vpc nat gw eth0 ip " + eth0IpName)
	ipClient.DeleteSync(eth0IpName)
	ginkgo.By("Deleting vpc nat gw net1 ip " + net1IpName)
	ipClient.DeleteSync(net1IpName)

	natgwQoSPolicyName := "default-nic-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + natgwQoSPolicyName)
	rules := getNicDefaultQoSPolicy(defaultNicLimit)

	qosPolicy := framework.MakeQoSPolicy(natgwQoSPolicyName, true, apiv1.QoSBindingTypeNatGw, rules)
	_ = qosPolicyClient.CreateSync(qosPolicy)

	ginkgo.By("Creating custom vpc nat gw")
	vpcNatGw := framework.MakeVpcNatGateway(natgwName, vpcName, overlaySubnetName, lanIP, attachDefName, natgwQoSPolicyName)
	_ = vpcNatGwClient.CreateSync(vpcNatGw, f.ClientSet)

	eipQoSPolicyName := "eip-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + eipQoSPolicyName)
	rules = getEIPQoSRule(eipLimit)

	eipQoSPolicy := framework.MakeQoSPolicy(eipQoSPolicyName, false, apiv1.QoSBindingTypeEIP, rules)
	_ = qosPolicyClient.CreateSync(eipQoSPolicy)

	ginkgo.By("Creating eip " + eipName)
	vpc1EIP := framework.MakeIptablesEIP(eipName, "", "", "", natgwName, attachDefName, eipQoSPolicyName)
	vpc1EIP = eipClient.CreateSync(vpc1EIP)

	ginkgo.By("Creating fip " + fipName)
	fip := framework.MakeIptablesFIPRule(fipName, eipName, vpc1Pod.Status.PodIP)
	_ = fipClient.CreateSync(fip)

	ginkgo.By("Check qos " + eipQoSPolicyName + " is limited to " + fmt.Sprint(eipLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, eipLimit, true)

	ginkgo.By("Remove qos policy " + eipQoSPolicyName + " from natgw " + natgwName)
	_ = eipClient.PatchQoSPolicySync(eipName, "")

	ginkgo.By("Check qos " + natgwQoSPolicyName + " is limited to " + fmt.Sprint(defaultNicLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, true)

	ginkgo.By("Remove qos policy " + natgwQoSPolicyName + " from natgw " + natgwName)
	_ = vpcNatGwClient.PatchQoSPolicySync(natgwName, "")

	ginkgo.By("Check qos " + natgwQoSPolicyName + " is not limited to " + fmt.Sprint(defaultNicLimit) + "Mbps")
	checkQos(f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, false)

	ginkgo.By("Deleting qos policy " + natgwQoSPolicyName)
	qosPolicyClient.DeleteSync(natgwQoSPolicyName)

	ginkgo.By("Deleting qos policy " + eipQoSPolicyName)
	qosPolicyClient.DeleteSync(eipQoSPolicyName)
}

func vaildRateLimit(text string, limit int) bool {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		lastField := fields[len(fields)-1]
		number, err := strconv.Atoi(lastField)
		if err != nil {
			continue
		}
		max := float64(limit) * 1024 * 1024 * 1.2
		min := float64(limit) * 1024 * 1024 * 0.8
		if min <= float64(number) && float64(number) <= max {
			return true
		}
	}
	return false
}

var _ = framework.Describe("[group:qos-policy]", func() {
	f := framework.NewDefaultFramework("qos-policy")

	var skip bool
	var cs clientset.Interface
	var attachNetClient *framework.NetworkAttachmentDefinitionClient
	var clusterName string
	var vpcClient *framework.VpcClient
	var vpcNatGwClient *framework.VpcNatGatewayClient
	var subnetClient *framework.SubnetClient
	var podClient *framework.PodClient
	var ipClient *framework.IPClient
	var iptablesEIPClient *framework.IptablesEIPClient
	var iptablesFIPClient *framework.IptablesFIPClient
	var qosPolicyClient *framework.QoSPolicyClient

	var containerID string
	var image string
	var net1NicName string
	var dockerExtNetName string

	// docker network
	var dockerExtNetNetwork *dockertypes.NetworkResource

	var vpcQosParams *qosParams
	var vpc1Pod *corev1.Pod
	var vpc2Pod *corev1.Pod
	var vpc1EIP *apiv1.IptablesEIP
	var vpc2EIP *apiv1.IptablesEIP
	var vpc1FIP *apiv1.IptablesFIPRule
	var vpc2FIP *apiv1.IptablesFIPRule

	var lanIP string
	var overlaySubnetV4Cidr string
	var overlaySubnetV4Gw string
	var eth0Exist, net1Exist bool
	var annotations1 map[string]string
	var annotations2 map[string]string
	var iperfServerCmd []string

	ginkgo.BeforeEach(func() {
		vpcQosParams = newVPCQoSParamsInit()

		dockerExtNetName = "kube-ovn-qos-" + framework.RandomSuffix()

		vpcQosParams.vpc1SubnetName = "qos-vpc1-subnet-" + framework.RandomSuffix()
		vpcQosParams.vpc2SubnetName = "qos-vpc2-subnet-" + framework.RandomSuffix()

		vpcQosParams.vpcNat1GwName = "qos-gw1-" + framework.RandomSuffix()
		vpcQosParams.vpcNat2GwName = "qos-gw2-" + framework.RandomSuffix()

		vpcQosParams.vpc1EIPName = "qos-vpc1-eip-" + framework.RandomSuffix()
		vpcQosParams.vpc2EIPName = "qos-vpc2-eip-" + framework.RandomSuffix()

		vpcQosParams.vpc1FIPName = "qos-vpc1-fip-" + framework.RandomSuffix()
		vpcQosParams.vpc2FIPName = "qos-vpc2-fip-" + framework.RandomSuffix()

		vpcQosParams.vpc1PodName = "qos-vpc1-pod-" + framework.RandomSuffix()
		vpcQosParams.vpc2PodName = "qos-vpc2-pod-" + framework.RandomSuffix()

		vpcQosParams.attachDefName = "qos-ovn-vpc-external-network-" + framework.RandomSuffix()
		vpcQosParams.subnetProvider = fmt.Sprintf("%s.%s", vpcQosParams.attachDefName, framework.KubeOvnNamespace)

		containerID = ""
		cs = f.ClientSet
		podClient = f.PodClient()
		attachNetClient = f.NetworkAttachmentDefinitionClient(framework.KubeOvnNamespace)
		subnetClient = f.SubnetClient()
		vpcClient = f.VpcClient()
		vpcNatGwClient = f.VpcNatGatewayClient()
		iptablesEIPClient = f.IptablesEIPClient()
		ipClient = f.IPClient()
		iptablesFIPClient = f.IptablesFIPClient()
		qosPolicyClient = f.QoSPolicyClient()
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

		ginkgo.By("Ensuring docker network " + dockerExtNetName + " exists")
		network, err := docker.NetworkCreate(dockerExtNetName, true, true)
		framework.ExpectNoError(err, "creating docker network "+dockerExtNetName)
		dockerExtNetNetwork = network

		ginkgo.By("Getting kind nodes")
		nodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")
		framework.ExpectNotEmpty(nodes)

		ginkgo.By("Connecting nodes to the docker network")
		err = kind.NetworkConnect(dockerExtNetNetwork.ID, nodes)
		framework.ExpectNoError(err, "connecting nodes to network "+dockerExtNetName)

		ginkgo.By("Getting node links that belong to the docker network")
		nodes, err = kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")

		ginkgo.By("Validating node links")
		network1, err := docker.NetworkInspect(dockerExtNetName)
		framework.ExpectNoError(err)
		for _, node := range nodes {
			links, err := node.ListLinks()
			framework.ExpectNoError(err, "failed to list links on node %s: %v", node.Name(), err)
			net1Mac := network1.Containers[node.ID].MacAddress
			for _, link := range links {
				ginkgo.By("exist node nic " + link.IfName)
				if link.IfName == "eth0" {
					eth0Exist = true
				}
				if link.Address == net1Mac {
					net1NicName = link.IfName
					net1Exist = true
				}
			}
			framework.ExpectTrue(eth0Exist)
			framework.ExpectTrue(net1Exist)
		}
		setupNetworkAttachmentDefinition(
			f, dockerExtNetNetwork, attachNetClient,
			subnetClient, vpcQosParams.attachDefName, net1NicName, vpcQosParams.subnetProvider, dockerExtNetName)
	})

	ginkgo.AfterEach(func() {
		if containerID != "" {
			ginkgo.By("Deleting container " + containerID)
			err := docker.ContainerRemove(containerID)
			framework.ExpectNoError(err)
		}

		ginkgo.By("Deleting macvlan underlay subnet " + vpcQosParams.attachDefName)
		subnetClient.DeleteSync(vpcQosParams.attachDefName)

		// delete net1 attachment definition
		ginkgo.By("Deleting nad " + vpcQosParams.attachDefName)
		attachNetClient.Delete(vpcQosParams.attachDefName)

		ginkgo.By("Getting nodes")
		nodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in cluster")

		if dockerExtNetNetwork != nil {
			ginkgo.By("Disconnecting nodes from the docker network")
			err = kind.NetworkDisconnect(dockerExtNetNetwork.ID, nodes)
			framework.ExpectNoError(err, "disconnecting nodes from network "+dockerExtNetName)
			ginkgo.By("Deleting docker network " + dockerExtNetName + " exists")
			err := docker.NetworkRemove(dockerExtNetNetwork.ID)
			framework.ExpectNoError(err, "deleting docker network "+dockerExtNetName)
		}
	})

	_ = framework.Describe("vpc qos", func() {
		ginkgo.BeforeEach(func() {
			iperfServerCmd = []string{"iperf", "-s", "-i", "1", "-p", iperf2Port}
			overlaySubnetV4Cidr = "10.0.0.0/24"
			overlaySubnetV4Gw = "10.0.0.1"
			lanIP = "10.0.0.254"
			natgwQoS := ""
			setupVpcNatGwTestEnvironment(
				f, dockerExtNetNetwork, attachNetClient,
				subnetClient, vpcClient, vpcNatGwClient,
				vpcQosParams.vpc1Name, vpcQosParams.vpc1SubnetName, vpcQosParams.vpcNat1GwName,
				natgwQoS, overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIP,
				dockerExtNetName, vpcQosParams.attachDefName, net1NicName,
				vpcQosParams.subnetProvider,
				true,
			)
			annotations1 = map[string]string{
				util.LogicalSwitchAnnotation: vpcQosParams.vpc1SubnetName,
			}
			ginkgo.By("Creating pod " + vpcQosParams.vpc1PodName)
			vpc1Pod = framework.MakePod(f.Namespace.Name, vpcQosParams.vpc1PodName, nil, annotations1, framework.AgnhostImage, iperfServerCmd, nil)
			vpc1Pod = podClient.CreateSync(vpc1Pod)

			ginkgo.By("Creating eip " + vpcQosParams.vpc1EIPName)
			vpc1EIP = framework.MakeIptablesEIP(vpcQosParams.vpc1EIPName, "", "", "", vpcQosParams.vpcNat1GwName, vpcQosParams.attachDefName, "")
			vpc1EIP = iptablesEIPClient.CreateSync(vpc1EIP)

			ginkgo.By("Creating fip " + vpcQosParams.vpc1FIPName)
			vpc1FIP = framework.MakeIptablesFIPRule(vpcQosParams.vpc1FIPName, vpcQosParams.vpc1EIPName, vpc1Pod.Status.PodIP)
			_ = iptablesFIPClient.CreateSync(vpc1FIP)

			setupVpcNatGwTestEnvironment(
				f, dockerExtNetNetwork, attachNetClient,
				subnetClient, vpcClient, vpcNatGwClient,
				vpcQosParams.vpc2Name, vpcQosParams.vpc2SubnetName, vpcQosParams.vpcNat2GwName,
				natgwQoS, overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIP,
				dockerExtNetName, vpcQosParams.attachDefName, net1NicName,
				vpcQosParams.subnetProvider,
				true,
			)

			annotations2 = map[string]string{
				util.LogicalSwitchAnnotation: vpcQosParams.vpc2SubnetName,
			}

			ginkgo.By("Creating pod " + vpcQosParams.vpc2PodName)
			vpc2Pod = framework.MakePod(f.Namespace.Name, vpcQosParams.vpc2PodName, nil, annotations2, framework.AgnhostImage, iperfServerCmd, nil)
			vpc2Pod = podClient.CreateSync(vpc2Pod)

			ginkgo.By("Creating eip " + vpcQosParams.vpc2EIPName)
			vpc2EIP = framework.MakeIptablesEIP(vpcQosParams.vpc2EIPName, "", "", "", vpcQosParams.vpcNat2GwName, vpcQosParams.attachDefName, "")
			vpc2EIP = iptablesEIPClient.CreateSync(vpc2EIP)

			ginkgo.By("Creating fip " + vpcQosParams.vpc2FIPName)
			vpc2FIP = framework.MakeIptablesFIPRule(vpcQosParams.vpc2FIPName, vpcQosParams.vpc2EIPName, vpc2Pod.Status.PodIP)
			_ = iptablesFIPClient.CreateSync(vpc2FIP)
		})
		ginkgo.AfterEach(func() {
			ginkgo.By("Deleting fip " + vpcQosParams.vpc1FIPName)
			iptablesFIPClient.DeleteSync(vpcQosParams.vpc1FIPName)

			ginkgo.By("Deleting fip " + vpcQosParams.vpc2FIPName)
			iptablesFIPClient.DeleteSync(vpcQosParams.vpc2FIPName)

			ginkgo.By("Deleting eip " + vpcQosParams.vpc1EIPName)
			iptablesEIPClient.DeleteSync(vpcQosParams.vpc1EIPName)

			ginkgo.By("Deleting eip " + vpcQosParams.vpc2EIPName)
			iptablesEIPClient.DeleteSync(vpcQosParams.vpc2EIPName)

			ginkgo.By("Deleting pod " + vpcQosParams.vpc1PodName)
			podClient.DeleteSync(vpcQosParams.vpc1PodName)

			ginkgo.By("Deleting pod " + vpcQosParams.vpc2PodName)
			podClient.DeleteSync(vpcQosParams.vpc2PodName)

			ginkgo.By("Deleting custom vpc " + vpcQosParams.vpc1Name)
			vpcClient.DeleteSync(vpcQosParams.vpc1Name)

			ginkgo.By("Deleting custom vpc " + vpcQosParams.vpc2Name)
			vpcClient.DeleteSync(vpcQosParams.vpc2Name)

			ginkgo.By("Deleting custom vpc nat gw " + vpcQosParams.vpcNat1GwName)
			vpcNatGwClient.DeleteSync(vpcQosParams.vpcNat1GwName)

			ginkgo.By("Deleting custom vpc nat gw " + vpcQosParams.vpcNat2GwName)
			vpcNatGwClient.DeleteSync(vpcQosParams.vpcNat2GwName)

			// the only pod for vpc nat gateway
			vpcNatGw1PodName := util.GenNatGwPodName(vpcQosParams.vpcNat1GwName)

			// delete vpc nat gw statefulset remaining ip for eth0 and net2
			overlaySubnet1 := subnetClient.Get(vpcQosParams.vpc1SubnetName)
			macvlanSubnet := subnetClient.Get(vpcQosParams.attachDefName)
			eth0IpName := ovs.PodNameToPortName(vpcNatGw1PodName, framework.KubeOvnNamespace, overlaySubnet1.Spec.Provider)
			net1IpName := ovs.PodNameToPortName(vpcNatGw1PodName, framework.KubeOvnNamespace, macvlanSubnet.Spec.Provider)
			ginkgo.By("Deleting vpc nat gw eth0 ip " + eth0IpName)
			ipClient.DeleteSync(eth0IpName)
			ginkgo.By("Deleting vpc nat gw net1 ip " + net1IpName)
			ipClient.DeleteSync(net1IpName)
			ginkgo.By("Deleting overlay subnet " + vpcQosParams.vpc1SubnetName)
			subnetClient.DeleteSync(vpcQosParams.vpc1SubnetName)

			ginkgo.By("Getting overlay subnet " + vpcQosParams.vpc2SubnetName)
			overlaySubnet2 := subnetClient.Get(vpcQosParams.vpc2SubnetName)

			vpcNatGw2PodName := util.GenNatGwPodName(vpcQosParams.vpcNat2GwName)
			eth0IpName = ovs.PodNameToPortName(vpcNatGw2PodName, framework.KubeOvnNamespace, overlaySubnet2.Spec.Provider)
			net1IpName = ovs.PodNameToPortName(vpcNatGw2PodName, framework.KubeOvnNamespace, macvlanSubnet.Spec.Provider)
			ginkgo.By("Deleting vpc nat gw eth0 ip " + eth0IpName)
			ipClient.DeleteSync(eth0IpName)
			ginkgo.By("Deleting vpc nat gw net1 ip " + net1IpName)
			ipClient.DeleteSync(net1IpName)
			ginkgo.By("Deleting overlay subnet " + vpcQosParams.vpc2SubnetName)
			subnetClient.DeleteSync(vpcQosParams.vpc2SubnetName)
		})
		framework.ConformanceIt("default nic qos", func() {
			// case 1: set qos policy for natgw
			// case 2: rebuild qos when natgw pod restart
			defaultQoSCases(f, vpcNatGwClient, podClient, qosPolicyClient, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, vpcQosParams.vpcNat1GwName)
		})
		framework.ConformanceIt("eip qos", func() {
			// case 1: set qos policy for eip
			// case 2: update qos policy for eip
			// case 3: change qos policy of eip
			// case 4: rebuild qos when natgw pod restart
			eipQoSCases(f, iptablesEIPClient, podClient, qosPolicyClient, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, vpcQosParams.vpc1EIPName, vpcQosParams.vpcNat1GwName)
		})
		framework.ConformanceIt("specofying ip qos", func() {
			// case 1: set specific ip qos policy for natgw
			specifyingIPQoSCases(f, vpcNatGwClient, qosPolicyClient, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, vpcQosParams.vpcNat1GwName)
		})
		framework.ConformanceIt("qos priority matching", func() {
			// case 1: test qos match priority
			// case 2: change qos policy of natgw
			priorityQoSCases(f, vpcNatGwClient, iptablesEIPClient, qosPolicyClient, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, vpcQosParams.vpcNat1GwName, vpcQosParams.vpc1EIPName)
		})
		framework.ConformanceIt("create resource with qos policy", func() {
			// case 1: test qos when create natgw with qos policy
			// case 2: test qos when create eip with qos policy
			createNatGwAndSetQosCases(f,
				vpcNatGwClient, ipClient, iptablesEIPClient, iptablesFIPClient,
				subnetClient, qosPolicyClient, vpc1Pod, vpc2Pod, vpc2EIP, vpcQosParams.vpcNat1GwName,
				vpcQosParams.vpc1EIPName, vpcQosParams.vpc1FIPName, vpcQosParams.vpc1Name,
				vpcQosParams.vpc1SubnetName, lanIP, vpcQosParams.attachDefName)
		})
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
