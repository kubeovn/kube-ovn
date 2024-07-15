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

	dockernetwork "github.com/docker/docker/api/types/network"
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
	vpcNatConfigName       = "ovn-vpc-nat-config"
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
	ctx context.Context,
	f *framework.Framework,
	dockerExtNetNetwork *dockernetwork.Inspect,
	attachNetClient *framework.NetworkAttachmentDefinitionClient,
	subnetClient *framework.SubnetClient,
	externalNetworkName string,
	nicName string,
	provider string,
	dockerExtNetName string,
) {
	ginkgo.GinkgoHelper()

	ginkgo.By("Getting docker network " + dockerExtNetName)
	network, err := docker.NetworkInspect(ctx, dockerExtNetName)
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
	attachNetClient.Create(ctx, attachNet)
	nad := attachNetClient.Get(ctx, externalNetworkName)

	ginkgo.By("Got network attachment definition " + nad.Name)

	ginkgo.By("Creating underlay macvlan subnet " + externalNetworkName)
	var cidrV4, cidrV6, gatewayV4, gatewayV6 string
	for _, config := range dockerExtNetNetwork.IPAM.Config {
		switch util.CheckProtocol(config.Subnet) {
		case apiv1.ProtocolIPv4:
			if f.HasIPv4() {
				cidrV4 = config.Subnet
				gatewayV4 = config.Gateway
			}
		case apiv1.ProtocolIPv6:
			if f.HasIPv6() {
				cidrV6 = config.Subnet
				gatewayV6 = config.Gateway
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
		if container.IPv4Address != "" && f.HasIPv4() {
			excludeIPs = append(excludeIPs, strings.Split(container.IPv4Address, "/")[0])
		}
		if container.IPv6Address != "" && f.HasIPv6() {
			excludeIPs = append(excludeIPs, strings.Split(container.IPv6Address, "/")[0])
		}
	}
	macvlanSubnet := framework.MakeSubnet(externalNetworkName, "", strings.Join(cidr, ","), strings.Join(gateway, ","), "", provider, excludeIPs, nil, nil)
	_ = subnetClient.CreateSync(ctx, macvlanSubnet)
}

func setupVpcNatGwTestEnvironment(
	ctx context.Context,
	f *framework.Framework,
	dockerExtNetNetwork *dockernetwork.Inspect,
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
	ginkgo.GinkgoHelper()

	if !skipNADSetup {
		setupNetworkAttachmentDefinition(
			ctx, f, dockerExtNetNetwork, attachNetClient,
			subnetClient, externalNetworkName, nicName, provider, dockerExtNetName)
	}

	ginkgo.By("Getting config map " + vpcNatGWConfigMapName)
	_, err := f.ClientSet.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Get(ctx, vpcNatGWConfigMapName, metav1.GetOptions{})
	framework.ExpectNoError(err, "failed to get ConfigMap")

	ginkgo.By("Creating custom vpc " + vpcName)
	vpc := framework.MakeVpc(vpcName, lanIP, false, false, nil)
	_ = vpcClient.CreateSync(ctx, vpc)

	ginkgo.By("Creating custom overlay subnet " + overlaySubnetName)
	overlaySubnet := framework.MakeSubnet(overlaySubnetName, "", overlaySubnetV4Cidr, overlaySubnetV4Gw, vpcName, "", nil, nil, nil)
	_ = subnetClient.CreateSync(ctx, overlaySubnet)

	ginkgo.By("Creating custom vpc nat gw " + vpcNatGwName)
	vpcNatGw := framework.MakeVpcNatGateway(vpcNatGwName, vpcName, overlaySubnetName, lanIP, externalNetworkName, natGwQosPolicy)
	_ = vpcNatGwClient.CreateSync(ctx, vpcNatGw, f.ClientSet)
}

func cleanVpcNatGwTestEnvironment(
	ctx context.Context,
	subnetClient *framework.SubnetClient,
	vpcClient *framework.VpcClient,
	vpcNatGwClient *framework.VpcNatGatewayClient,
	vpcName string,
	overlaySubnetName string,
	vpcNatGwName string,
) {
	ginkgo.GinkgoHelper()

	ginkgo.By("start to clean custom vpc nat gw      " + vpcNatGwName)
	ginkgo.By("clean custom vpc nat gw " + vpcNatGwName)
	vpcNatGwClient.DeleteSync(ctx, vpcNatGwName)

	ginkgo.By("clean custom overlay subnet " + overlaySubnetName)
	subnetClient.DeleteSync(ctx, overlaySubnetName)

	ginkgo.By("clean custom vpc " + vpcName)
	vpcClient.DeleteSync(ctx, vpcName)
}

var _ = framework.SerialDescribe("[group:iptables-vpc-nat-gw]", func() {
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
	var sharedVipName, sharedEipName, sharedEipDnatName, sharedEipSnatName, sharedEipFipShouldOkName, sharedEipFipShouldFailName string
	var vipClient *framework.VipClient
	var ipClient *framework.IPClient
	var iptablesEIPClient *framework.IptablesEIPClient
	var iptablesFIPClient *framework.IptablesFIPClient
	var iptablesSnatRuleClient *framework.IptablesSnatClient
	var iptablesDnatRuleClient *framework.IptablesDnatClient

	var dockerExtNet1Network *dockernetwork.Inspect
	var net1NicName string

	// multiple external network case
	var dockerExtNet2Network *dockernetwork.Inspect
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
	sharedEipFipShouldOkName = "shared-eip-fip-should-ok-" + framework.RandomSuffix()
	sharedEipFipShouldFailName = "shared-eip-fip-should-fail-" + framework.RandomSuffix()

	snatEipName = "snat-eip-" + framework.RandomSuffix()
	snatName = "snat-" + framework.RandomSuffix()
	overlaySubnetName = "overlay-subnet-" + framework.RandomSuffix()

	net2AttachDefName = "net2-ovn-vpc-external-network-" + framework.RandomSuffix()
	net2SubnetProvider = fmt.Sprintf("%s.%s", net2AttachDefName, framework.KubeOvnNamespace)
	net2OverlaySubnetName = "net2-overlay-subnet-" + framework.RandomSuffix()
	net2VpcNatGwName = "net2-gw-" + framework.RandomSuffix()
	net2VpcName = "net2-vpc-" + framework.RandomSuffix()
	net2EipName = "net2-eip-" + framework.RandomSuffix()

	ginkgo.BeforeEach(ginkgo.NodeTimeout(5*time.Second), func(ctx ginkgo.SpecContext) {
		cs = f.ClientSet
		attachNetClient = f.NetworkAttachmentDefinitionClientNS(framework.KubeOvnNamespace)
		subnetClient = f.SubnetClient()
		vpcClient = f.VpcClient()
		vpcNatGwClient = f.VpcNatGatewayClient()
		iptablesEIPClient = f.IptablesEIPClient()
		vipClient = f.VipClient()
		ipClient = f.IPClient()
		iptablesFIPClient = f.IptablesFIPClient()
		iptablesSnatRuleClient = f.IptablesSnatClient()
		iptablesDnatRuleClient = f.IptablesDnatClient()

		if skip {
			ginkgo.Skip("underlay spec only runs on kind clusters")
		}

		if clusterName == "" {
			ginkgo.By("Getting k8s nodes")
			k8sNodes, err := e2enode.GetReadySchedulableNodes(ctx, cs)
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
			network1, err := docker.NetworkCreate(ctx, dockerExtNet1Name, true, true)
			framework.ExpectNoError(err, "creating docker network "+dockerExtNet1Name)

			dockerExtNet1Network = network1
		}

		if dockerExtNet2Network == nil {
			ginkgo.By("Ensuring docker network " + dockerExtNet2Name + " exists")
			network2, err := docker.NetworkCreate(ctx, dockerExtNet2Name, true, true)
			framework.ExpectNoError(err, "creating docker network "+dockerExtNet2Name)
			dockerExtNet2Network = network2
		}

		ginkgo.By("Getting kind nodes")
		nodes, err := kind.ListNodes(ctx, clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")
		framework.ExpectNotEmpty(nodes)

		ginkgo.By("Connecting nodes to the docker network")
		err = kind.NetworkConnect(ctx, dockerExtNet1Network.ID, nodes)
		framework.ExpectNoError(err, "connecting nodes to network "+dockerExtNet1Name)

		ginkgo.By("Connecting nodes to the docker network")
		err = kind.NetworkConnect(ctx, dockerExtNet2Network.ID, nodes)
		framework.ExpectNoError(err, "connecting nodes to network "+dockerExtNet2Name)

		ginkgo.By("Getting node links that belong to the docker network")
		nodes, err = kind.ListNodes(ctx, clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")

		ginkgo.By("Validating node links")
		network1, err := docker.NetworkInspect(ctx, dockerExtNet1Name)
		framework.ExpectNoError(err)
		network2, err := docker.NetworkInspect(ctx, dockerExtNet2Name)
		framework.ExpectNoError(err)
		var eth0Exist, net1Exist, net2Exist bool
		for _, node := range nodes {
			links, err := node.ListLinks(ctx)
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

	ginkgo.AfterEach(ginkgo.NodeTimeout(15*time.Second), func(ctx ginkgo.SpecContext) {
		cleanVpcNatGwTestEnvironment(ctx, subnetClient, vpcClient, vpcNatGwClient, vpcName, overlaySubnetName, vpcNatGwName)
		ginkgo.By("Deleting macvlan underlay subnet " + networkAttachDefName)
		subnetClient.DeleteSync(ctx, networkAttachDefName)

		// delete net1 attachment definition
		ginkgo.By("Deleting nad " + networkAttachDefName)
		attachNetClient.Delete(ctx, networkAttachDefName)
		// delete net2 attachment definition
		ginkgo.By("Deleting nad " + net2AttachDefName)
		attachNetClient.Delete(ctx, net2AttachDefName)

		ginkgo.By("Getting nodes")
		nodes, err := kind.ListNodes(ctx, clusterName, "")
		framework.ExpectNoError(err, "getting nodes in cluster")

		if dockerExtNet1Network != nil {
			ginkgo.By("Disconnecting nodes from the docker network")
			err = kind.NetworkDisconnect(ctx, dockerExtNet1Network.ID, nodes)
			framework.ExpectNoError(err, "disconnecting nodes from network "+dockerExtNet1Name)
		}
		if dockerExtNet2Network != nil {
			ginkgo.By("Disconnecting nodes from the docker network")
			err = kind.NetworkDisconnect(ctx, dockerExtNet2Network.ID, nodes)
			framework.ExpectNoError(err, "disconnecting nodes from network "+dockerExtNet2Name)
		}
	})

	framework.ConformanceIt("change gateway image", ginkgo.SpecTimeout(time.Minute), func(ctx ginkgo.SpecContext) {
		overlaySubnetV4Cidr := "10.0.0.0/24"
		overlaySubnetV4Gw := "10.0.0.1"
		lanIP := "10.0.0.254"
		natgwQoS := ""
		cm, err := f.ClientSet.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Get(ctx, vpcNatConfigName, metav1.GetOptions{})
		framework.ExpectNoError(err)
		oldImage := cm.Data["image"]
		cm.Data["image"] = "docker.io/kubeovn/vpc-nat-gateway:v1.12.18"
		cm, err = f.ClientSet.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Update(ctx, cm, metav1.UpdateOptions{})
		framework.ExpectNoError(err)
		time.Sleep(3 * time.Second)
		setupVpcNatGwTestEnvironment(
			ctx, f, dockerExtNet1Network, attachNetClient,
			subnetClient, vpcClient, vpcNatGwClient,
			vpcName, overlaySubnetName, vpcNatGwName, natgwQoS,
			overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIP,
			dockerExtNet1Name, networkAttachDefName, net1NicName,
			externalSubnetProvider,
			false,
		)
		vpcNatGwPodName := util.GenNatGwPodName(vpcNatGwName)
		pod := f.PodClientNS("kube-system").Get(ctx, vpcNatGwPodName)
		framework.ExpectNotNil(pod)
		framework.ExpectEqual(pod.Spec.Containers[0].Image, cm.Data["image"])

		// recover the image
		cm.Data["image"] = oldImage
		_, err = f.ClientSet.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Update(ctx, cm, metav1.UpdateOptions{})
		framework.ExpectNoError(err)
	})

	framework.ConformanceIt("iptables eip fip snat dnat", ginkgo.SpecTimeout(2*time.Minute), func(ctx ginkgo.SpecContext) {
		overlaySubnetV4Cidr := "10.0.1.0/24"
		overlaySubnetV4Gw := "10.0.1.1"
		lanIP := "10.0.1.254"
		natgwQoS := ""
		setupVpcNatGwTestEnvironment(
			ctx, f, dockerExtNet1Network, attachNetClient,
			subnetClient, vpcClient, vpcNatGwClient,
			vpcName, overlaySubnetName, vpcNatGwName, natgwQoS,
			overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIP,
			dockerExtNet1Name, networkAttachDefName, net1NicName,
			externalSubnetProvider,
			false,
		)

		ginkgo.By("Creating iptables vip for fip")
		fipVip := framework.MakeVip(f.Namespace.Name, fipVipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(ctx, fipVip)
		fipVip = vipClient.Get(ctx, fipVipName)
		ginkgo.By("Creating iptables eip for fip")
		fipEip := framework.MakeIptablesEIP(fipEipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(ctx, fipEip)
		ginkgo.By("Creating iptables fip")
		fip := framework.MakeIptablesFIPRule(fipName, fipEipName, fipVip.Status.V4ip)
		_ = iptablesFIPClient.CreateSync(ctx, fip)

		ginkgo.By("Creating iptables eip for snat")
		snatEip := framework.MakeIptablesEIP(snatEipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(ctx, snatEip)
		ginkgo.By("Creating iptables snat")
		snat := framework.MakeIptablesSnatRule(snatName, snatEipName, overlaySubnetV4Cidr)
		_ = iptablesSnatRuleClient.CreateSync(ctx, snat)

		ginkgo.By("Creating iptables vip for dnat")
		dnatVip := framework.MakeVip(f.Namespace.Name, dnatVipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(ctx, dnatVip)
		dnatVip = vipClient.Get(ctx, dnatVipName)
		ginkgo.By("Creating iptables eip for dnat")
		dnatEip := framework.MakeIptablesEIP(dnatEipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(ctx, dnatEip)
		ginkgo.By("Creating iptables dnat")
		dnat := framework.MakeIptablesDnatRule(dnatName, dnatEipName, "80", "tcp", dnatVip.Status.V4ip, "8080")
		_ = iptablesDnatRuleClient.CreateSync(ctx, dnat)

		// share eip case
		ginkgo.By("Creating share vip")
		shareVip := framework.MakeVip(f.Namespace.Name, sharedVipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(ctx, shareVip)
		fipVip = vipClient.Get(ctx, fipVipName)
		ginkgo.By("Creating share iptables eip")
		shareEip := framework.MakeIptablesEIP(sharedEipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(ctx, shareEip)
		ginkgo.By("Creating the first iptables fip with share eip vip should be ok")
		shareFipShouldOk := framework.MakeIptablesFIPRule(sharedEipFipShouldOkName, sharedEipName, fipVip.Status.V4ip)
		_ = iptablesFIPClient.CreateSync(ctx, shareFipShouldOk)
		ginkgo.By("Creating the second iptables fip with share eip vip should be failed")
		shareFipShouldFail := framework.MakeIptablesFIPRule(sharedEipFipShouldFailName, sharedEipName, fipVip.Status.V4ip)
		_ = iptablesFIPClient.Create(ctx, shareFipShouldFail)
		ginkgo.By("Creating iptables dnat for dnat with share eip vip")
		shareDnat := framework.MakeIptablesDnatRule(sharedEipDnatName, sharedEipName, "80", "tcp", fipVip.Status.V4ip, "8080")
		_ = iptablesDnatRuleClient.CreateSync(ctx, shareDnat)
		ginkgo.By("Creating iptables snat with share eip vip")
		shareSnat := framework.MakeIptablesSnatRule(sharedEipSnatName, sharedEipName, overlaySubnetV4Cidr)
		_ = iptablesSnatRuleClient.CreateSync(ctx, shareSnat)

		ginkgo.By("Get share eip")
		shareEip = iptablesEIPClient.Get(ctx, sharedEipName)
		ginkgo.By("Get share dnat")
		shareDnat = iptablesDnatRuleClient.Get(ctx, sharedEipDnatName)
		ginkgo.By("Get share snat")
		shareSnat = iptablesSnatRuleClient.Get(ctx, sharedEipSnatName)
		ginkgo.By("Get share fip should ok")
		shareFipShouldOk = iptablesFIPClient.Get(ctx, sharedEipFipShouldOkName)
		ginkgo.By("Get share fip should fail")
		shareFipShouldFail = iptablesFIPClient.Get(ctx, sharedEipFipShouldFailName)

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

		ginkgo.By("Deleting share iptables fip " + sharedEipFipShouldOkName)
		iptablesFIPClient.DeleteSync(ctx, sharedEipFipShouldOkName)
		ginkgo.By("Deleting share iptables fip " + sharedEipFipShouldFailName)
		iptablesFIPClient.DeleteSync(ctx, sharedEipFipShouldFailName)
		ginkgo.By("Deleting share iptables dnat " + dnatName)
		iptablesDnatRuleClient.DeleteSync(ctx, dnatName)
		ginkgo.By("Deleting share iptables snat " + snatName)
		iptablesSnatRuleClient.DeleteSync(ctx, snatName)

		ginkgo.By("Deleting iptables fip " + fipName)
		iptablesFIPClient.DeleteSync(ctx, fipName)
		ginkgo.By("Deleting iptables dnat " + dnatName)
		iptablesDnatRuleClient.DeleteSync(ctx, dnatName)
		ginkgo.By("Deleting iptables snat " + snatName)
		iptablesSnatRuleClient.DeleteSync(ctx, snatName)

		ginkgo.By("Deleting iptables eip " + fipEipName)
		iptablesEIPClient.DeleteSync(ctx, fipEipName)
		ginkgo.By("Deleting iptables eip " + dnatEipName)
		iptablesEIPClient.DeleteSync(ctx, dnatEipName)
		ginkgo.By("Deleting iptables eip " + snatEipName)
		iptablesEIPClient.DeleteSync(ctx, snatEipName)
		ginkgo.By("Deleting iptables share eip " + sharedEipName)
		iptablesEIPClient.DeleteSync(ctx, sharedEipName)

		ginkgo.By("Deleting vip " + fipVipName)
		vipClient.DeleteSync(ctx, fipVipName)
		ginkgo.By("Deleting vip " + dnatVipName)
		vipClient.DeleteSync(ctx, dnatVipName)
		ginkgo.By("Deleting vip " + sharedVipName)
		vipClient.DeleteSync(ctx, sharedVipName)

		ginkgo.By("Deleting custom vpc " + vpcName)
		vpcClient.DeleteSync(ctx, vpcName)

		ginkgo.By("Deleting custom vpc nat gw")
		vpcNatGwClient.DeleteSync(ctx, vpcNatGwName)

		// the only pod for vpc nat gateway
		vpcNatGwPodName := util.GenNatGwPodName(vpcNatGwName)

		// delete vpc nat gw statefulset remaining ip for eth0 and net1
		overlaySubnet := subnetClient.Get(ctx, overlaySubnetName)
		macvlanSubnet := subnetClient.Get(ctx, networkAttachDefName)
		eth0IpName := ovs.PodNameToPortName(vpcNatGwPodName, framework.KubeOvnNamespace, overlaySubnet.Spec.Provider)
		net1IpName := ovs.PodNameToPortName(vpcNatGwPodName, framework.KubeOvnNamespace, macvlanSubnet.Spec.Provider)
		ginkgo.By("Deleting vpc nat gw eth0 ip " + eth0IpName)
		ipClient.DeleteSync(ctx, eth0IpName)
		ginkgo.By("Deleting vpc nat gw net1 ip " + net1IpName)
		ipClient.DeleteSync(ctx, net1IpName)

		ginkgo.By("Deleting overlay subnet " + overlaySubnetName)
		subnetClient.DeleteSync(ctx, overlaySubnetName)

		// multiple external network case
		net2OverlaySubnetV4Cidr := "10.0.1.0/24"
		net2OoverlaySubnetV4Gw := "10.0.1.1"
		net2LanIP := "10.0.1.254"
		natgwQoS = ""
		setupVpcNatGwTestEnvironment(
			ctx, f, dockerExtNet2Network, attachNetClient,
			subnetClient, vpcClient, vpcNatGwClient,
			net2VpcName, net2OverlaySubnetName, net2VpcNatGwName, natgwQoS,
			net2OverlaySubnetV4Cidr, net2OoverlaySubnetV4Gw, net2LanIP,
			dockerExtNet2Name, net2AttachDefName, net2NicName,
			net2SubnetProvider,
			false,
		)

		ginkgo.By("Creating iptables eip of net2")
		net2Eip := framework.MakeIptablesEIP(net2EipName, "", "", "", net2VpcNatGwName, net2AttachDefName, "")
		_ = iptablesEIPClient.CreateSync(ctx, net2Eip)

		ginkgo.By("Deleting iptables eip " + net2EipName)
		iptablesEIPClient.DeleteSync(ctx, net2EipName)

		ginkgo.By("Deleting custom vpc " + net2VpcName)
		vpcClient.DeleteSync(ctx, net2VpcName)

		ginkgo.By("Deleting custom vpc nat gw")
		vpcNatGwClient.DeleteSync(ctx, net2VpcNatGwName)

		// the only pod for vpc nat gateway
		vpcNatGwPodName = util.GenNatGwPodName(net2VpcNatGwName)

		// delete vpc nat gw statefulset remaining ip for eth0 and net2
		overlaySubnet = subnetClient.Get(ctx, net2OverlaySubnetName)
		macvlanSubnet = subnetClient.Get(ctx, net2AttachDefName)
		eth0IpName = ovs.PodNameToPortName(vpcNatGwPodName, framework.KubeOvnNamespace, overlaySubnet.Spec.Provider)
		net2IpName := ovs.PodNameToPortName(vpcNatGwPodName, framework.KubeOvnNamespace, macvlanSubnet.Spec.Provider)
		ginkgo.By("Deleting vpc nat gw eth0 ip " + eth0IpName)
		ipClient.DeleteSync(ctx, eth0IpName)
		ginkgo.By("Deleting vpc nat gw net2 ip " + net2IpName)
		ipClient.DeleteSync(ctx, net2IpName)

		ginkgo.By("Deleting macvlan underlay subnet " + net2AttachDefName)
		subnetClient.DeleteSync(ctx, net2AttachDefName)

		ginkgo.By("Deleting overlay subnet " + net2OverlaySubnetName)
		subnetClient.DeleteSync(ctx, net2OverlaySubnetName)
	})
})

func iperf(ctx context.Context, f *framework.Framework, iperfClientPod *corev1.Pod, iperfServerEIP *apiv1.IptablesEIP) string {
	ginkgo.GinkgoHelper()

	for i := 0; i < 20; i++ {
		command := fmt.Sprintf("iperf -e -p %s --reportstyle C -i 1 -c %s -t 10", iperf2Port, iperfServerEIP.Status.IP)
		stdOutput, errOutput, err := framework.ExecShellInPod(ctx, f, iperfClientPod.Namespace, iperfClientPod.Name, command)
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

func checkQos(ctx context.Context, f *framework.Framework,
	vpc1Pod, vpc2Pod *corev1.Pod, vpc1EIP, vpc2EIP *apiv1.IptablesEIP,
	limit int, expect bool,
) {
	ginkgo.GinkgoHelper()

	if !skipIperf {
		if expect {
			output := iperf(ctx, f, vpc1Pod, vpc2EIP)
			framework.ExpectTrue(validRateLimit(output, limit))
			output = iperf(ctx, f, vpc2Pod, vpc1EIP)
			framework.ExpectTrue(validRateLimit(output, limit))
		} else {
			output := iperf(ctx, f, vpc1Pod, vpc2EIP)
			framework.ExpectFalse(validRateLimit(output, limit))
			output = iperf(ctx, f, vpc2Pod, vpc1EIP)
			framework.ExpectFalse(validRateLimit(output, limit))
		}
	}
}

func newVPCQoSParamsInit() *qosParams {
	qosParams := &qosParams{
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
	qosParams.subnetProvider = fmt.Sprintf("%s.%s", qosParams.attachDefName, framework.KubeOvnNamespace)
	return qosParams
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
func defaultQoSCases(
	ctx context.Context,
	f *framework.Framework,
	vpcNatGwClient *framework.VpcNatGatewayClient,
	podClient *framework.PodClient,
	qosPolicyClient *framework.QoSPolicyClient,
	vpc1Pod *corev1.Pod,
	vpc2Pod *corev1.Pod,
	vpc1EIP *apiv1.IptablesEIP,
	vpc2EIP *apiv1.IptablesEIP,
	natgwName string,
) {
	ginkgo.GinkgoHelper()

	// create nic qos policy
	qosPolicyName := "default-nic-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + qosPolicyName)
	rules := getNicDefaultQoSPolicy(defaultNicLimit)

	qosPolicy := framework.MakeQoSPolicy(qosPolicyName, true, apiv1.QoSBindingTypeNatGw, rules)
	_ = qosPolicyClient.CreateSync(ctx, qosPolicy)

	ginkgo.By("Patch natgw " + natgwName + " with qos policy " + qosPolicyName)
	_ = vpcNatGwClient.PatchQoSPolicySync(ctx, natgwName, qosPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + fmt.Sprint(defaultNicLimit) + "Mbps")
	checkQos(ctx, f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, true)

	ginkgo.By("Delete natgw pod " + natgwName + "-0")
	natGwPodName := util.GenNatGwPodName(natgwName)
	podClient.DeleteSync(ctx, natGwPodName)

	ginkgo.By("Wait for natgw " + natgwName + "qos rebuild")
	time.Sleep(5 * time.Second)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + fmt.Sprint(defaultNicLimit) + "Mbps")
	checkQos(ctx, f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, true)

	ginkgo.By("Remove qos policy " + qosPolicyName + " from natgw " + natgwName)
	_ = vpcNatGwClient.PatchQoSPolicySync(ctx, natgwName, "")

	ginkgo.By("Deleting qos policy " + qosPolicyName)
	qosPolicyClient.DeleteSync(ctx, qosPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is not limited to " + fmt.Sprint(defaultNicLimit) + "Mbps")
	checkQos(ctx, f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, false)
}

// eipQoSCases test default qos policy
func eipQoSCases(
	ctx context.Context,
	f *framework.Framework,
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
	ginkgo.GinkgoHelper()

	// create eip qos policy
	qosPolicyName := "eip-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + qosPolicyName)
	rules := getEIPQoSRule(eipLimit)

	qosPolicy := framework.MakeQoSPolicy(qosPolicyName, false, apiv1.QoSBindingTypeEIP, rules)
	qosPolicy = qosPolicyClient.CreateSync(ctx, qosPolicy)

	ginkgo.By("Patch eip " + eipName + " with qos policy " + qosPolicyName)
	_ = eipClient.PatchQoSPolicySync(ctx, eipName, qosPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + fmt.Sprint(eipLimit) + "Mbps")
	checkQos(ctx, f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, eipLimit, true)

	ginkgo.By("Update qos policy " + qosPolicyName + " with new rate limit")

	rules = getEIPQoSRule(updatedEIPLimit)
	modifiedqosPolicy := qosPolicy.DeepCopy()
	modifiedqosPolicy.Spec.BandwidthLimitRules = rules
	qosPolicyClient.Patch(ctx, qosPolicy, modifiedqosPolicy)
	qosPolicyClient.WaitToQoSReady(ctx, qosPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is changed to " + fmt.Sprint(updatedEIPLimit) + "Mbps")
	checkQos(ctx, f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, updatedEIPLimit, true)

	ginkgo.By("Delete natgw pod " + natgwName + "-0")
	natGwPodName := util.GenNatGwPodName(natgwName)
	podClient.DeleteSync(ctx, natGwPodName)

	ginkgo.By("Wait for natgw " + natgwName + "qos rebuild")
	time.Sleep(5 * time.Second)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + fmt.Sprint(updatedEIPLimit) + "Mbps")
	checkQos(ctx, f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, updatedEIPLimit, true)

	newQoSPolicyName := "new-eip-qos-policy-" + framework.RandomSuffix()
	newRules := getEIPQoSRule(newEIPLimit)
	newQoSPolicy := framework.MakeQoSPolicy(newQoSPolicyName, false, apiv1.QoSBindingTypeEIP, newRules)
	_ = qosPolicyClient.CreateSync(ctx, newQoSPolicy)

	ginkgo.By("Change qos policy of eip " + eipName + " to " + newQoSPolicyName)
	_ = eipClient.PatchQoSPolicySync(ctx, eipName, newQoSPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + fmt.Sprint(newEIPLimit) + "Mbps")
	checkQos(ctx, f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, newEIPLimit, true)

	ginkgo.By("Remove qos policy " + qosPolicyName + " from natgw " + eipName)
	_ = eipClient.PatchQoSPolicySync(ctx, eipName, "")

	ginkgo.By("Deleting qos policy " + qosPolicyName)
	qosPolicyClient.DeleteSync(ctx, qosPolicyName)

	ginkgo.By("Deleting qos policy " + newQoSPolicyName)
	qosPolicyClient.DeleteSync(ctx, newQoSPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is not limited to " + fmt.Sprint(newEIPLimit) + "Mbps")
	checkQos(ctx, f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, newEIPLimit, false)
}

// specifyingIPQoSCases test default qos policy
func specifyingIPQoSCases(
	ctx context.Context,
	f *framework.Framework,
	vpcNatGwClient *framework.VpcNatGatewayClient,
	qosPolicyClient *framework.QoSPolicyClient,
	vpc1Pod *corev1.Pod,
	vpc2Pod *corev1.Pod,
	vpc1EIP *apiv1.IptablesEIP,
	vpc2EIP *apiv1.IptablesEIP,
	natgwName string,
) {
	ginkgo.GinkgoHelper()

	// create nic qos policy
	qosPolicyName := "specifying-ip-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + qosPolicyName)

	rules := getSpecialQoSRule(specificIPLimit, vpc2EIP.Status.IP)

	qosPolicy := framework.MakeQoSPolicy(qosPolicyName, true, apiv1.QoSBindingTypeNatGw, rules)
	_ = qosPolicyClient.CreateSync(ctx, qosPolicy)

	ginkgo.By("Patch natgw " + natgwName + " with qos policy " + qosPolicyName)
	_ = vpcNatGwClient.PatchQoSPolicySync(ctx, natgwName, qosPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is limited to " + fmt.Sprint(specificIPLimit) + "Mbps")
	checkQos(ctx, f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, specificIPLimit, true)

	ginkgo.By("Remove qos policy " + qosPolicyName + " from natgw " + natgwName)
	_ = vpcNatGwClient.PatchQoSPolicySync(ctx, natgwName, "")

	ginkgo.By("Deleting qos policy " + qosPolicyName)
	qosPolicyClient.DeleteSync(ctx, qosPolicyName)

	ginkgo.By("Check qos " + qosPolicyName + " is not limited to " + fmt.Sprint(specificIPLimit) + "Mbps")
	checkQos(ctx, f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, specificIPLimit, false)
}

// priorityQoSCases test qos match priority
func priorityQoSCases(
	ctx context.Context,
	f *framework.Framework,
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
	ginkgo.GinkgoHelper()

	// create nic qos policy
	natGwQoSPolicyName := "priority-nic-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + natGwQoSPolicyName)
	// default qos policy + special qos policy
	natgwRules := getNicDefaultQoSPolicy(defaultNicLimit)
	natgwRules = append(natgwRules, getSpecialQoSRule(specificIPLimit, vpc2EIP.Status.IP)...)

	natgwQoSPolicy := framework.MakeQoSPolicy(natGwQoSPolicyName, true, apiv1.QoSBindingTypeNatGw, natgwRules)
	_ = qosPolicyClient.CreateSync(ctx, natgwQoSPolicy)

	ginkgo.By("Patch natgw " + natgwName + " with qos policy " + natGwQoSPolicyName)
	_ = vpcNatGwClient.PatchQoSPolicySync(ctx, natgwName, natGwQoSPolicyName)

	eipQoSPolicyName := "eip-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + eipQoSPolicyName)
	eipRules := getEIPQoSRule(eipLimit)

	eipQoSPolicy := framework.MakeQoSPolicy(eipQoSPolicyName, false, apiv1.QoSBindingTypeEIP, eipRules)
	_ = qosPolicyClient.CreateSync(ctx, eipQoSPolicy)

	ginkgo.By("Patch eip " + eipName + " with qos policy " + eipQoSPolicyName)
	_ = eipClient.PatchQoSPolicySync(ctx, eipName, eipQoSPolicyName)

	// match qos of priority 1
	ginkgo.By("Check qos to match priority 1 is limited to " + fmt.Sprint(eipLimit) + "Mbps")
	checkQos(ctx, f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, eipLimit, true)

	ginkgo.By("Remove qos policy " + eipQoSPolicyName + " from natgw " + eipName)
	_ = eipClient.PatchQoSPolicySync(ctx, eipName, "")

	ginkgo.By("Deleting qos policy " + eipQoSPolicyName)
	qosPolicyClient.DeleteSync(ctx, eipQoSPolicyName)

	// match qos of priority 2
	ginkgo.By("Check qos to match priority 2 is limited to " + fmt.Sprint(specificIPLimit) + "Mbps")
	checkQos(ctx, f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, specificIPLimit, true)

	// change qos policy of natgw
	newNatGwQoSPolicyName := "new-priority-nic-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + newNatGwQoSPolicyName)
	newNatgwRules := getNicDefaultQoSPolicy(defaultNicLimit)

	newNatgwQoSPolicy := framework.MakeQoSPolicy(newNatGwQoSPolicyName, true, apiv1.QoSBindingTypeNatGw, newNatgwRules)
	_ = qosPolicyClient.CreateSync(ctx, newNatgwQoSPolicy)

	ginkgo.By("Change qos policy of natgw " + natgwName + " to " + newNatGwQoSPolicyName)
	_ = vpcNatGwClient.PatchQoSPolicySync(ctx, natgwName, newNatGwQoSPolicyName)

	// match qos of priority 3
	ginkgo.By("Check qos to match priority 3 is limited to " + fmt.Sprint(specificIPLimit) + "Mbps")
	checkQos(ctx, f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, true)

	ginkgo.By("Remove qos policy " + natGwQoSPolicyName + " from natgw " + natgwName)
	_ = vpcNatGwClient.PatchQoSPolicySync(ctx, natgwName, "")

	ginkgo.By("Deleting qos policy " + natGwQoSPolicyName)
	qosPolicyClient.DeleteSync(ctx, natGwQoSPolicyName)

	ginkgo.By("Deleting qos policy " + newNatGwQoSPolicyName)
	qosPolicyClient.DeleteSync(ctx, newNatGwQoSPolicyName)

	ginkgo.By("Check qos " + natGwQoSPolicyName + " is not limited to " + fmt.Sprint(defaultNicLimit) + "Mbps")
	checkQos(ctx, f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, false)
}

func createNatGwAndSetQosCases(
	ctx context.Context,
	f *framework.Framework,
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
	ginkgo.GinkgoHelper()

	// delete fip
	ginkgo.By("Deleting fip " + fipName)
	fipClient.DeleteSync(ctx, fipName)

	ginkgo.By("Deleting eip " + eipName)
	eipClient.DeleteSync(ctx, eipName)

	// the only pod for vpc nat gateway
	vpcNatGw1PodName := util.GenNatGwPodName(natgwName)

	// delete vpc nat gw statefulset remaining ip for eth0 and net2
	ginkgo.By("Deleting custom vpc nat gw " + natgwName)
	vpcNatGwClient.DeleteSync(ctx, natgwName)

	overlaySubnet1 := subnetClient.Get(ctx, overlaySubnetName)
	macvlanSubnet := subnetClient.Get(ctx, attachDefName)
	eth0IpName := ovs.PodNameToPortName(vpcNatGw1PodName, framework.KubeOvnNamespace, overlaySubnet1.Spec.Provider)
	net1IpName := ovs.PodNameToPortName(vpcNatGw1PodName, framework.KubeOvnNamespace, macvlanSubnet.Spec.Provider)
	ginkgo.By("Deleting vpc nat gw eth0 ip " + eth0IpName)
	ipClient.DeleteSync(ctx, eth0IpName)
	ginkgo.By("Deleting vpc nat gw net1 ip " + net1IpName)
	ipClient.DeleteSync(ctx, net1IpName)

	natgwQoSPolicyName := "default-nic-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + natgwQoSPolicyName)
	rules := getNicDefaultQoSPolicy(defaultNicLimit)

	qosPolicy := framework.MakeQoSPolicy(natgwQoSPolicyName, true, apiv1.QoSBindingTypeNatGw, rules)
	_ = qosPolicyClient.CreateSync(ctx, qosPolicy)

	ginkgo.By("Creating custom vpc nat gw")
	vpcNatGw := framework.MakeVpcNatGateway(natgwName, vpcName, overlaySubnetName, lanIP, attachDefName, natgwQoSPolicyName)
	_ = vpcNatGwClient.CreateSync(ctx, vpcNatGw, f.ClientSet)

	eipQoSPolicyName := "eip-qos-policy-" + framework.RandomSuffix()
	ginkgo.By("Creating qos policy " + eipQoSPolicyName)
	rules = getEIPQoSRule(eipLimit)

	eipQoSPolicy := framework.MakeQoSPolicy(eipQoSPolicyName, false, apiv1.QoSBindingTypeEIP, rules)
	_ = qosPolicyClient.CreateSync(ctx, eipQoSPolicy)

	ginkgo.By("Creating eip " + eipName)
	vpc1EIP := framework.MakeIptablesEIP(eipName, "", "", "", natgwName, attachDefName, eipQoSPolicyName)
	vpc1EIP = eipClient.CreateSync(ctx, vpc1EIP)

	ginkgo.By("Creating fip " + fipName)
	fip := framework.MakeIptablesFIPRule(fipName, eipName, vpc1Pod.Status.PodIP)
	_ = fipClient.CreateSync(ctx, fip)

	ginkgo.By("Check qos " + eipQoSPolicyName + " is limited to " + fmt.Sprint(eipLimit) + "Mbps")
	checkQos(ctx, f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, eipLimit, true)

	ginkgo.By("Remove qos policy " + eipQoSPolicyName + " from natgw " + natgwName)
	_ = eipClient.PatchQoSPolicySync(ctx, eipName, "")

	ginkgo.By("Check qos " + natgwQoSPolicyName + " is limited to " + fmt.Sprint(defaultNicLimit) + "Mbps")
	checkQos(ctx, f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, true)

	ginkgo.By("Remove qos policy " + natgwQoSPolicyName + " from natgw " + natgwName)
	_ = vpcNatGwClient.PatchQoSPolicySync(ctx, natgwName, "")

	ginkgo.By("Check qos " + natgwQoSPolicyName + " is not limited to " + fmt.Sprint(defaultNicLimit) + "Mbps")
	checkQos(ctx, f, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, defaultNicLimit, false)

	ginkgo.By("Deleting qos policy " + natgwQoSPolicyName)
	qosPolicyClient.DeleteSync(ctx, natgwQoSPolicyName)

	ginkgo.By("Deleting qos policy " + eipQoSPolicyName)
	qosPolicyClient.DeleteSync(ctx, eipQoSPolicyName)
}

func validRateLimit(text string, limit int) bool {
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

	var net1NicName string
	var dockerExtNetName string

	// docker network
	var dockerExtNetNetwork *dockernetwork.Inspect

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

	ginkgo.BeforeEach(ginkgo.NodeTimeout(10*time.Second), func(ctx ginkgo.SpecContext) {
		if skip {
			ginkgo.Skip("underlay spec only runs on kind clusters")
		}

		cs = f.ClientSet
		podClient = f.PodClient()
		attachNetClient = f.NetworkAttachmentDefinitionClientNS(framework.KubeOvnNamespace)
		subnetClient = f.SubnetClient()
		vpcClient = f.VpcClient()
		vpcNatGwClient = f.VpcNatGatewayClient()
		iptablesEIPClient = f.IptablesEIPClient()
		ipClient = f.IPClient()
		iptablesFIPClient = f.IptablesFIPClient()
		qosPolicyClient = f.QoSPolicyClient()

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

		if clusterName == "" {
			ginkgo.By("Getting k8s nodes")
			k8sNodes, err := e2enode.GetReadySchedulableNodes(ctx, cs)
			framework.ExpectNoError(err)

			cluster, ok := kind.IsKindProvided(k8sNodes.Items[0].Spec.ProviderID)
			if !ok {
				skip = true
				ginkgo.Skip("underlay spec only runs on kind clusters")
			}
			clusterName = cluster
		}

		ginkgo.By("Ensuring docker network " + dockerExtNetName + " exists")
		network, err := docker.NetworkCreate(ctx, dockerExtNetName, true, true)
		framework.ExpectNoError(err, "creating docker network "+dockerExtNetName)
		dockerExtNetNetwork = network

		ginkgo.By("Getting kind nodes")
		nodes, err := kind.ListNodes(ctx, clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")
		framework.ExpectNotEmpty(nodes)

		ginkgo.By("Connecting nodes to the docker network")
		err = kind.NetworkConnect(ctx, dockerExtNetNetwork.ID, nodes)
		framework.ExpectNoError(err, "connecting nodes to network "+dockerExtNetName)

		ginkgo.By("Getting node links that belong to the docker network")
		nodes, err = kind.ListNodes(ctx, clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")

		ginkgo.By("Validating node links")
		network1, err := docker.NetworkInspect(ctx, dockerExtNetName)
		framework.ExpectNoError(err)
		for _, node := range nodes {
			links, err := node.ListLinks(ctx)
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
			ctx, f, dockerExtNetNetwork, attachNetClient,
			subnetClient, vpcQosParams.attachDefName, net1NicName, vpcQosParams.subnetProvider, dockerExtNetName)
	})

	ginkgo.AfterEach(ginkgo.NodeTimeout(15*time.Second), func(ctx ginkgo.SpecContext) {
		ginkgo.By("Deleting macvlan underlay subnet " + vpcQosParams.attachDefName)
		subnetClient.DeleteSync(ctx, vpcQosParams.attachDefName)

		// delete net1 attachment definition
		ginkgo.By("Deleting nad " + vpcQosParams.attachDefName)
		attachNetClient.Delete(ctx, vpcQosParams.attachDefName)

		ginkgo.By("Getting nodes")
		nodes, err := kind.ListNodes(ctx, clusterName, "")
		framework.ExpectNoError(err, "getting nodes in cluster")

		if dockerExtNetNetwork != nil {
			ginkgo.By("Disconnecting nodes from the docker network")
			err = kind.NetworkDisconnect(ctx, dockerExtNetNetwork.ID, nodes)
			framework.ExpectNoError(err, "disconnecting nodes from network "+dockerExtNetName)
			ginkgo.By("Deleting docker network " + dockerExtNetName + " exists")
			err := docker.NetworkRemove(ctx, dockerExtNetNetwork.ID)
			framework.ExpectNoError(err, "deleting docker network "+dockerExtNetName)
		}
	})

	_ = framework.Describe("vpc qos", func() {
		ginkgo.BeforeEach(ginkgo.NodeTimeout(2*time.Minute), func(ctx ginkgo.SpecContext) {
			iperfServerCmd = []string{"iperf", "-s", "-i", "1", "-p", iperf2Port}
			overlaySubnetV4Cidr = "10.0.0.0/24"
			overlaySubnetV4Gw = "10.0.0.1"
			lanIP = "10.0.0.254"
			natgwQoS := ""
			setupVpcNatGwTestEnvironment(
				ctx, f, dockerExtNetNetwork, attachNetClient,
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
			vpc1Pod = podClient.CreateSync(ctx, vpc1Pod)

			ginkgo.By("Creating eip " + vpcQosParams.vpc1EIPName)
			vpc1EIP = framework.MakeIptablesEIP(vpcQosParams.vpc1EIPName, "", "", "", vpcQosParams.vpcNat1GwName, vpcQosParams.attachDefName, "")
			vpc1EIP = iptablesEIPClient.CreateSync(ctx, vpc1EIP)

			ginkgo.By("Creating fip " + vpcQosParams.vpc1FIPName)
			vpc1FIP = framework.MakeIptablesFIPRule(vpcQosParams.vpc1FIPName, vpcQosParams.vpc1EIPName, vpc1Pod.Status.PodIP)
			_ = iptablesFIPClient.CreateSync(ctx, vpc1FIP)

			setupVpcNatGwTestEnvironment(
				ctx, f, dockerExtNetNetwork, attachNetClient,
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
			vpc2Pod = podClient.CreateSync(ctx, vpc2Pod)

			ginkgo.By("Creating eip " + vpcQosParams.vpc2EIPName)
			vpc2EIP = framework.MakeIptablesEIP(vpcQosParams.vpc2EIPName, "", "", "", vpcQosParams.vpcNat2GwName, vpcQosParams.attachDefName, "")
			vpc2EIP = iptablesEIPClient.CreateSync(ctx, vpc2EIP)

			ginkgo.By("Creating fip " + vpcQosParams.vpc2FIPName)
			vpc2FIP = framework.MakeIptablesFIPRule(vpcQosParams.vpc2FIPName, vpcQosParams.vpc2EIPName, vpc2Pod.Status.PodIP)
			_ = iptablesFIPClient.CreateSync(ctx, vpc2FIP)
		})
		ginkgo.AfterEach(ginkgo.NodeTimeout(2*time.Minute), func(ctx ginkgo.SpecContext) {
			ginkgo.By("Deleting fip " + vpcQosParams.vpc1FIPName)
			iptablesFIPClient.DeleteSync(ctx, vpcQosParams.vpc1FIPName)

			ginkgo.By("Deleting fip " + vpcQosParams.vpc2FIPName)
			iptablesFIPClient.DeleteSync(ctx, vpcQosParams.vpc2FIPName)

			ginkgo.By("Deleting eip " + vpcQosParams.vpc1EIPName)
			iptablesEIPClient.DeleteSync(ctx, vpcQosParams.vpc1EIPName)

			ginkgo.By("Deleting eip " + vpcQosParams.vpc2EIPName)
			iptablesEIPClient.DeleteSync(ctx, vpcQosParams.vpc2EIPName)

			ginkgo.By("Deleting pod " + vpcQosParams.vpc1PodName)
			podClient.DeleteSync(ctx, vpcQosParams.vpc1PodName)

			ginkgo.By("Deleting pod " + vpcQosParams.vpc2PodName)
			podClient.DeleteSync(ctx, vpcQosParams.vpc2PodName)

			ginkgo.By("Deleting custom vpc " + vpcQosParams.vpc1Name)
			vpcClient.DeleteSync(ctx, vpcQosParams.vpc1Name)

			ginkgo.By("Deleting custom vpc " + vpcQosParams.vpc2Name)
			vpcClient.DeleteSync(ctx, vpcQosParams.vpc2Name)

			ginkgo.By("Deleting custom vpc nat gw " + vpcQosParams.vpcNat1GwName)
			vpcNatGwClient.DeleteSync(ctx, vpcQosParams.vpcNat1GwName)

			ginkgo.By("Deleting custom vpc nat gw " + vpcQosParams.vpcNat2GwName)
			vpcNatGwClient.DeleteSync(ctx, vpcQosParams.vpcNat2GwName)

			// the only pod for vpc nat gateway
			vpcNatGw1PodName := util.GenNatGwPodName(vpcQosParams.vpcNat1GwName)

			// delete vpc nat gw statefulset remaining ip for eth0 and net2
			overlaySubnet1 := subnetClient.Get(ctx, vpcQosParams.vpc1SubnetName)
			macvlanSubnet := subnetClient.Get(ctx, vpcQosParams.attachDefName)
			eth0IpName := ovs.PodNameToPortName(vpcNatGw1PodName, framework.KubeOvnNamespace, overlaySubnet1.Spec.Provider)
			net1IpName := ovs.PodNameToPortName(vpcNatGw1PodName, framework.KubeOvnNamespace, macvlanSubnet.Spec.Provider)
			ginkgo.By("Deleting vpc nat gw eth0 ip " + eth0IpName)
			ipClient.DeleteSync(ctx, eth0IpName)
			ginkgo.By("Deleting vpc nat gw net1 ip " + net1IpName)
			ipClient.DeleteSync(ctx, net1IpName)
			ginkgo.By("Deleting overlay subnet " + vpcQosParams.vpc1SubnetName)
			subnetClient.DeleteSync(ctx, vpcQosParams.vpc1SubnetName)

			ginkgo.By("Getting overlay subnet " + vpcQosParams.vpc2SubnetName)
			overlaySubnet2 := subnetClient.Get(ctx, vpcQosParams.vpc2SubnetName)

			vpcNatGw2PodName := util.GenNatGwPodName(vpcQosParams.vpcNat2GwName)
			eth0IpName = ovs.PodNameToPortName(vpcNatGw2PodName, framework.KubeOvnNamespace, overlaySubnet2.Spec.Provider)
			net1IpName = ovs.PodNameToPortName(vpcNatGw2PodName, framework.KubeOvnNamespace, macvlanSubnet.Spec.Provider)
			ginkgo.By("Deleting vpc nat gw eth0 ip " + eth0IpName)
			ipClient.DeleteSync(ctx, eth0IpName)
			ginkgo.By("Deleting vpc nat gw net1 ip " + net1IpName)
			ipClient.DeleteSync(ctx, net1IpName)
			ginkgo.By("Deleting overlay subnet " + vpcQosParams.vpc2SubnetName)
			subnetClient.DeleteSync(ctx, vpcQosParams.vpc2SubnetName)
		})
		framework.ConformanceIt("default nic qos", ginkgo.SpecTimeout(4*time.Minute), func(ctx ginkgo.SpecContext) {
			// case 1: set qos policy for natgw
			// case 2: rebuild qos when natgw pod restart
			defaultQoSCases(ctx, f, vpcNatGwClient, podClient, qosPolicyClient, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, vpcQosParams.vpcNat1GwName)
		})
		framework.ConformanceIt("eip qos", ginkgo.SpecTimeout(4*time.Minute), func(ctx ginkgo.SpecContext) {
			// case 1: set qos policy for eip
			// case 2: update qos policy for eip
			// case 3: change qos policy of eip
			// case 4: rebuild qos when natgw pod restart
			eipQoSCases(ctx, f, iptablesEIPClient, podClient, qosPolicyClient, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, vpcQosParams.vpc1EIPName, vpcQosParams.vpcNat1GwName)
		})
		framework.ConformanceIt("specifying ip qos", ginkgo.SpecTimeout(3*time.Minute), func(ctx ginkgo.SpecContext) {
			// case 1: set specific ip qos policy for natgw
			specifyingIPQoSCases(ctx, f, vpcNatGwClient, qosPolicyClient, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, vpcQosParams.vpcNat1GwName)
		})
		framework.ConformanceIt("qos priority matching", ginkgo.SpecTimeout(4*time.Minute), func(ctx ginkgo.SpecContext) {
			// case 1: test qos match priority
			// case 2: change qos policy of natgw
			priorityQoSCases(ctx, f, vpcNatGwClient, iptablesEIPClient, qosPolicyClient, vpc1Pod, vpc2Pod, vpc1EIP, vpc2EIP, vpcQosParams.vpcNat1GwName, vpcQosParams.vpc1EIPName)
		})
		framework.ConformanceIt("create resource with qos policy", ginkgo.SpecTimeout(4*time.Minute), func(ctx ginkgo.SpecContext) {
			// case 1: test qos when create natgw with qos policy
			// case 2: test qos when create eip with qos policy
			createNatGwAndSetQosCases(ctx, f,
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
