package ovn_eip

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"testing"
	"time"

	dockernetwork "github.com/moby/moby/api/types/network"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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

func setupNetworkAttachmentDefinition(
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
	network, err := docker.NetworkInspect(dockerExtNetName)
	framework.ExpectNoError(err, "getting docker network "+dockerExtNetName)
	ginkgo.By("Getting or creating network attachment definition " + externalNetworkName)
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

	// Try to get existing NAD first
	nad, err := attachNetClient.NetworkAttachmentDefinitionInterface.Get(context.TODO(), externalNetworkName, metav1.GetOptions{})
	if err != nil && k8serrors.IsNotFound(err) {
		// NAD doesn't exist, create it
		attachNet := framework.MakeNetworkAttachmentDefinition(externalNetworkName, framework.KubeOvnNamespace, attachConf)
		nad = attachNetClient.Create(attachNet)
	} else {
		framework.ExpectNoError(err, "getting network attachment definition "+externalNetworkName)
	}

	ginkgo.By("Got network attachment definition " + nad.Name)

	ginkgo.By("Creating underlay macvlan subnet " + externalNetworkName)
	var cidrV4, cidrV6, gatewayV4, gatewayV6 string
	for _, config := range dockerExtNetNetwork.IPAM.Config {
		switch util.CheckProtocol(config.Subnet.Addr().String()) {
		case apiv1.ProtocolIPv4:
			if f.HasIPv4() {
				cidrV4 = config.Subnet.String()
				gatewayV4 = config.Gateway.String()
			}
		case apiv1.ProtocolIPv6:
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

	// Check if subnet already exists
	_, err = subnetClient.SubnetInterface.Get(context.TODO(), externalNetworkName, metav1.GetOptions{})
	if err != nil && k8serrors.IsNotFound(err) {
		// Subnet doesn't exist, create it
		macvlanSubnet := framework.MakeSubnet(externalNetworkName, "", strings.Join(cidr, ","), strings.Join(gateway, ","), "", provider, excludeIPs, nil, nil)
		_ = subnetClient.CreateSync(macvlanSubnet)
	} else {
		framework.ExpectNoError(err, "getting subnet "+externalNetworkName)
	}
}

func setupVpcNatGwTestEnvironment(
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

func cleanVpcNatGwTestEnvironment(
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
	vpcNatGwClient.DeleteSync(vpcNatGwName)

	ginkgo.By("clean custom overlay subnet " + overlaySubnetName)
	subnetClient.DeleteSync(overlaySubnetName)

	ginkgo.By("clean custom vpc " + vpcName)
	vpcClient.DeleteSync(vpcName)
}

// waitForIptablesEIPReady waits for an IptablesEIP to be ready
func waitForIptablesEIPReady(eipClient *framework.IptablesEIPClient, eipName string, timeout time.Duration) *apiv1.IptablesEIP {
	ginkgo.GinkgoHelper()
	var eip *apiv1.IptablesEIP
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		eip = eipClient.Get(eipName)
		if eip != nil && eip.Status.IP != "" && eip.Status.Ready {
			framework.Logf("IptablesEIP %s is ready with IP: %s", eipName, eip.Status.IP)
			return eip
		}
		time.Sleep(2 * time.Second)
	}
	framework.Failf("Timeout waiting for IptablesEIP %s to be ready", eipName)
	return nil
}

// verifySubnetStatusAfterEIPOperation verifies subnet status after EIP operation
func verifySubnetStatusAfterEIPOperation(subnetClient *framework.SubnetClient, subnetName string,
	protocol, operation, shouldContainIP string,
) {
	ginkgo.GinkgoHelper()

	subnet := subnetClient.Get(subnetName)
	framework.Logf("Verifying subnet %s status after %s: Protocol=%s", subnetName, operation, protocol)

	switch protocol {
	case apiv1.ProtocolIPv4:
		framework.Logf("V4 Status: Available=%.0f, Using=%.0f",
			subnet.Status.V4AvailableIPs, subnet.Status.V4UsingIPs)
		if shouldContainIP != "" {
			framework.ExpectTrue(strings.Contains(subnet.Status.V4UsingIPRange, shouldContainIP),
				"IP %s should be in V4UsingIPRange after %s", shouldContainIP, operation)
		}
	case apiv1.ProtocolIPv6:
		framework.Logf("V6 Status: Available=%.0f, Using=%.0f",
			subnet.Status.V6AvailableIPs, subnet.Status.V6UsingIPs)
		if shouldContainIP != "" {
			framework.ExpectTrue(strings.Contains(subnet.Status.V6UsingIPRange, shouldContainIP),
				"IP %s should be in V6UsingIPRange after %s", shouldContainIP, operation)
		}
	case apiv1.ProtocolDual:
		framework.Logf("Dual Stack Status: V4Available=%.0f, V4Using=%.0f, V6Available=%.0f, V6Using=%.0f",
			subnet.Status.V4AvailableIPs, subnet.Status.V4UsingIPs,
			subnet.Status.V6AvailableIPs, subnet.Status.V6UsingIPs)
	}
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

	ginkgo.BeforeEach(func() {
		randomSuffix := framework.RandomSuffix()
		vpcName = "vpc-" + randomSuffix
		vpcNatGwName = "gw-" + randomSuffix

		fipVipName = "fip-vip-" + randomSuffix
		fipEipName = "fip-eip-" + randomSuffix
		fipName = "fip-" + randomSuffix

		dnatVipName = "dnat-vip-" + randomSuffix
		dnatEipName = "dnat-eip-" + randomSuffix
		dnatName = "dnat-" + randomSuffix

		// sharing case
		sharedVipName = "shared-vip-" + randomSuffix
		sharedEipName = "shared-eip-" + randomSuffix
		sharedEipDnatName = "shared-eip-dnat-" + randomSuffix
		sharedEipSnatName = "shared-eip-snat-" + randomSuffix
		sharedEipFipShouldOkName = "shared-eip-fip-should-ok-" + randomSuffix
		sharedEipFipShouldFailName = "shared-eip-fip-should-fail-" + randomSuffix

		snatEipName = "snat-eip-" + randomSuffix
		snatName = "snat-" + randomSuffix
		overlaySubnetName = "overlay-subnet-" + randomSuffix

		net2AttachDefName = "net2-ovn-vpc-external-network-" + randomSuffix
		net2SubnetProvider = fmt.Sprintf("%s.%s", net2AttachDefName, framework.KubeOvnNamespace)
		net2OverlaySubnetName = "net2-overlay-subnet-" + randomSuffix
		net2VpcNatGwName = "net2-gw-" + randomSuffix
		net2VpcName = "net2-vpc-" + randomSuffix
		net2EipName = "net2-eip-" + randomSuffix

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
				if link.Address == net1Mac.String() {
					net1NicName = link.IfName
					net1Exist = true
				}
				if link.Address == net2Mac.String() {
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
		cleanVpcNatGwTestEnvironment(subnetClient, vpcClient, vpcNatGwClient, vpcName, overlaySubnetName, vpcNatGwName)
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

	framework.ConformanceIt("change gateway image", func() {
		overlaySubnetV4Cidr := "10.0.2.0/24"
		overlaySubnetV4Gw := "10.0.2.1"
		lanIP := "10.0.2.254"
		natgwQoS := ""
		cm, err := f.ClientSet.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Get(context.Background(), vpcNatConfigName, metav1.GetOptions{})
		framework.ExpectNoError(err)
		oldImage := cm.Data["image"]
		cm.Data["image"] = "docker.io/kubeovn/vpc-nat-gateway:v1.14.19"
		cm, err = f.ClientSet.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Update(context.Background(), cm, metav1.UpdateOptions{})
		framework.ExpectNoError(err)
		time.Sleep(3 * time.Second)
		setupVpcNatGwTestEnvironment(
			f, dockerExtNet1Network, attachNetClient,
			subnetClient, vpcClient, vpcNatGwClient,
			vpcName, overlaySubnetName+"image", vpcNatGwName, natgwQoS,
			overlaySubnetV4Cidr, overlaySubnetV4Gw, lanIP,
			dockerExtNet1Name, networkAttachDefName, net1NicName,
			externalSubnetProvider,
			false,
		)
		vpcNatGwPodName := util.GenNatGwPodName(vpcNatGwName)
		pod := f.PodClientNS("kube-system").GetPod(vpcNatGwPodName)
		framework.ExpectNotNil(pod)
		framework.ExpectEqual(pod.Spec.Containers[0].Image, cm.Data["image"])

		// recover the image
		cm.Data["image"] = oldImage
		_, err = f.ClientSet.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Update(context.Background(), cm, metav1.UpdateOptions{})
		framework.ExpectNoError(err)
		vpcNatGwClient.DeleteSync(vpcNatGwName)
		subnetClient.DeleteSync(overlaySubnetName + "image")
	})

	framework.ConformanceIt("iptables eip fip snat dnat", func() {
		overlaySubnetV4Cidr := "10.0.1.0/24"
		overlaySubnetV4Gw := "10.0.1.1"
		lanIP := "10.0.1.254"
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
		shareFipShouldOk := framework.MakeIptablesFIPRule(sharedEipFipShouldOkName, sharedEipName, fipVip.Status.V4ip)
		_ = iptablesFIPClient.CreateSync(shareFipShouldOk)
		ginkgo.By("Creating the second iptables fip with share eip vip should be failed")
		shareFipShouldFail := framework.MakeIptablesFIPRule(sharedEipFipShouldFailName, sharedEipName, fipVip.Status.V4ip)
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
		shareFipShouldOk = iptablesFIPClient.Get(sharedEipFipShouldOkName)
		ginkgo.By("Get share fip should fail")
		shareFipShouldFail = iptablesFIPClient.Get(sharedEipFipShouldFailName)

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
		iptablesFIPClient.DeleteSync(sharedEipFipShouldOkName)
		ginkgo.By("Deleting share iptables fip " + sharedEipFipShouldFailName)
		iptablesFIPClient.DeleteSync(sharedEipFipShouldFailName)
		ginkgo.By("Deleting share iptables dnat " + sharedEipDnatName)
		iptablesDnatRuleClient.DeleteSync(sharedEipDnatName)
		ginkgo.By("Deleting share iptables snat " + sharedEipSnatName)
		iptablesSnatRuleClient.DeleteSync(sharedEipSnatName)

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
		// the only pod for vpc nat gateway
		vpcNatGwPodName := util.GenNatGwPodName(vpcNatGwName)
		// delete vpc nat gw statefulset remaining ip for eth0 and net1
		overlaySubnet := subnetClient.Get(overlaySubnetName)
		macvlanSubnet := subnetClient.Get(networkAttachDefName)
		eth0IpName := ovs.PodNameToPortName(vpcNatGwPodName, framework.KubeOvnNamespace, overlaySubnet.Spec.Provider)
		net1IpName := ovs.PodNameToPortName(vpcNatGwPodName, framework.KubeOvnNamespace, macvlanSubnet.Spec.Provider)

		ginkgo.By("Deleting custom vpc nat gw")
		vpcNatGwClient.DeleteSync(vpcNatGwName)

		ginkgo.By("Deleting vpc nat gw eth0 ip " + eth0IpName)
		ipClient.DeleteSync(eth0IpName)
		ginkgo.By("Deleting vpc nat gw net1 ip " + net1IpName)
		ipClient.DeleteSync(net1IpName)

		ginkgo.By("Deleting overlay subnet " + overlaySubnetName)
		subnetClient.DeleteSync(overlaySubnetName)

		ginkgo.By("Deleting custom vpc " + vpcName)
		vpcClient.DeleteSync(vpcName)

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

		ginkgo.By("Deleting custom vpc " + net2VpcName)
		vpcClient.DeleteSync(net2VpcName)
	})

	framework.ConformanceIt("should properly manage IptablesEIP lifecycle with finalizer and update subnet status", func() {
		f.SkipVersionPriorTo(1, 13, "This feature was introduced in v1.13")

		overlaySubnetV4Cidr := "10.0.3.0/24"
		overlaySubnetV4Gw := "10.0.3.1"
		lanIP := "10.0.3.254"
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

		ginkgo.By("1. Get initial external subnet status")
		externalSubnetName := util.GetExternalNetwork(networkAttachDefName)
		initialSubnet := subnetClient.Get(externalSubnetName)
		initialV4AvailableIPs := initialSubnet.Status.V4AvailableIPs
		initialV4UsingIPs := initialSubnet.Status.V4UsingIPs
		initialV6AvailableIPs := initialSubnet.Status.V6AvailableIPs
		initialV6UsingIPs := initialSubnet.Status.V6UsingIPs
		initialV4AvailableIPRange := initialSubnet.Status.V4AvailableIPRange
		initialV4UsingIPRange := initialSubnet.Status.V4UsingIPRange
		initialV6AvailableIPRange := initialSubnet.Status.V6AvailableIPRange
		initialV6UsingIPRange := initialSubnet.Status.V6UsingIPRange

		ginkgo.By("2. Create IptablesEIP to trigger IP allocation")
		eipName := "test-eip-finalizer-" + framework.RandomSuffix()
		eip := framework.MakeIptablesEIP(eipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(eip)

		ginkgo.By("3. Wait for IptablesEIP CR to be ready")
		eipCR := waitForIptablesEIPReady(iptablesEIPClient, eipName, 60*time.Second)
		framework.ExpectNotNil(eipCR, "IptablesEIP CR should be created and ready")
		framework.ExpectNotEmpty(eipCR.Status.IP, "IptablesEIP should have IP assigned")

		ginkgo.By("4. Wait for IptablesEIP CR finalizer to be added")
		for range 60 {
			eipCR = iptablesEIPClient.Get(eipName)
			if eipCR != nil && len(eipCR.Finalizers) > 0 {
				break
			}
			time.Sleep(1 * time.Second)
		}
		framework.ExpectNotNil(eipCR, "IptablesEIP CR should exist")
		framework.ExpectContainElement(eipCR.Finalizers, util.KubeOVNControllerFinalizer,
			"IptablesEIP CR should have finalizer after creation")

		ginkgo.By("5. Wait for external subnet status to be updated after IptablesEIP creation")
		time.Sleep(5 * time.Second)

		ginkgo.By("6. Verify external subnet status after IptablesEIP CR creation")
		afterCreateSubnet := subnetClient.Get(externalSubnetName)
		verifySubnetStatusAfterEIPOperation(subnetClient, externalSubnetName,
			afterCreateSubnet.Spec.Protocol, "IptablesEIP creation", eipCR.Status.IP)

		// Verify IP count and range changes
		switch afterCreateSubnet.Spec.Protocol {
		case apiv1.ProtocolIPv4:
			framework.ExpectEqual(initialV4AvailableIPs-1, afterCreateSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should decrease by 1 after IptablesEIP creation")
			framework.ExpectEqual(initialV4UsingIPs+1, afterCreateSubnet.Status.V4UsingIPs,
				"V4UsingIPs should increase by 1 after IptablesEIP creation")
			framework.ExpectNotEqual(initialV4AvailableIPRange, afterCreateSubnet.Status.V4AvailableIPRange,
				"V4AvailableIPRange should change after IptablesEIP creation")
			framework.ExpectNotEqual(initialV4UsingIPRange, afterCreateSubnet.Status.V4UsingIPRange,
				"V4UsingIPRange should change after IptablesEIP creation")
		case apiv1.ProtocolIPv6:
			framework.ExpectEqual(initialV6AvailableIPs-1, afterCreateSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should decrease by 1 after IptablesEIP creation")
			framework.ExpectEqual(initialV6UsingIPs+1, afterCreateSubnet.Status.V6UsingIPs,
				"V6UsingIPs should increase by 1 after IptablesEIP creation")
			framework.ExpectNotEqual(initialV6AvailableIPRange, afterCreateSubnet.Status.V6AvailableIPRange,
				"V6AvailableIPRange should change after IptablesEIP creation")
			framework.ExpectNotEqual(initialV6UsingIPRange, afterCreateSubnet.Status.V6UsingIPRange,
				"V6UsingIPRange should change after IptablesEIP creation")
		default:
			// Dual stack
			framework.ExpectEqual(initialV4AvailableIPs-1, afterCreateSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should decrease by 1 after IptablesEIP creation")
			framework.ExpectEqual(initialV4UsingIPs+1, afterCreateSubnet.Status.V4UsingIPs,
				"V4UsingIPs should increase by 1 after IptablesEIP creation")
			framework.ExpectEqual(initialV6AvailableIPs-1, afterCreateSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should decrease by 1 after IptablesEIP creation")
			framework.ExpectEqual(initialV6UsingIPs+1, afterCreateSubnet.Status.V6UsingIPs,
				"V6UsingIPs should increase by 1 after IptablesEIP creation")
			framework.ExpectNotEqual(initialV4AvailableIPRange, afterCreateSubnet.Status.V4AvailableIPRange,
				"V4AvailableIPRange should change after IptablesEIP creation")
			framework.ExpectNotEqual(initialV4UsingIPRange, afterCreateSubnet.Status.V4UsingIPRange,
				"V4UsingIPRange should change after IptablesEIP creation")
			framework.ExpectNotEqual(initialV6AvailableIPRange, afterCreateSubnet.Status.V6AvailableIPRange,
				"V6AvailableIPRange should change after IptablesEIP creation")
			framework.ExpectNotEqual(initialV6UsingIPRange, afterCreateSubnet.Status.V6UsingIPRange,
				"V6UsingIPRange should change after IptablesEIP creation")
		}

		// Store the status after creation for later comparison
		afterCreateV4AvailableIPs := afterCreateSubnet.Status.V4AvailableIPs
		afterCreateV4UsingIPs := afterCreateSubnet.Status.V4UsingIPs
		afterCreateV6AvailableIPs := afterCreateSubnet.Status.V6AvailableIPs
		afterCreateV6UsingIPs := afterCreateSubnet.Status.V6UsingIPs
		afterCreateV4AvailableIPRange := afterCreateSubnet.Status.V4AvailableIPRange
		afterCreateV4UsingIPRange := afterCreateSubnet.Status.V4UsingIPRange
		afterCreateV6AvailableIPRange := afterCreateSubnet.Status.V6AvailableIPRange
		afterCreateV6UsingIPRange := afterCreateSubnet.Status.V6UsingIPRange

		ginkgo.By("7. Delete the IptablesEIP to trigger IP release")
		iptablesEIPClient.DeleteSync(eipName)

		ginkgo.By("8. Wait for IptablesEIP CR to be deleted")
		deleted := false
		for range 30 {
			_, err := f.KubeOVNClientSet.KubeovnV1().IptablesEIPs().Get(context.Background(), eipName, metav1.GetOptions{})
			if err != nil && k8serrors.IsNotFound(err) {
				deleted = true
				break
			}
			time.Sleep(1 * time.Second)
		}
		framework.ExpectTrue(deleted, "IptablesEIP CR should be deleted")

		ginkgo.By("9. Wait for external subnet status to be updated after IptablesEIP deletion")
		time.Sleep(5 * time.Second)

		ginkgo.By("10. Verify external subnet status after IptablesEIP CR deletion")
		afterDeleteSubnet := subnetClient.Get(externalSubnetName)
		verifySubnetStatusAfterEIPOperation(subnetClient, externalSubnetName,
			afterDeleteSubnet.Spec.Protocol, "IptablesEIP deletion", "")

		// Verify IP count and range restoration
		switch afterDeleteSubnet.Spec.Protocol {
		case apiv1.ProtocolIPv4:
			// Verify IP count is restored
			framework.ExpectEqual(afterCreateV4AvailableIPs+1, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should increase by 1 after IptablesEIP deletion")
			framework.ExpectEqual(afterCreateV4UsingIPs-1, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should decrease by 1 after IptablesEIP deletion")

			// Verify IP range changed
			framework.ExpectNotEqual(afterCreateV4AvailableIPRange, afterDeleteSubnet.Status.V4AvailableIPRange,
				"V4AvailableIPRange should change after IptablesEIP deletion")
			framework.ExpectNotEqual(afterCreateV4UsingIPRange, afterDeleteSubnet.Status.V4UsingIPRange,
				"V4UsingIPRange should change after IptablesEIP deletion")

			// Verify counts match initial state
			framework.ExpectEqual(initialV4AvailableIPs, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should return to initial value after IptablesEIP deletion")
			framework.ExpectEqual(initialV4UsingIPs, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should return to initial value after IptablesEIP deletion")
		case apiv1.ProtocolIPv6:
			// Verify IP count is restored
			framework.ExpectEqual(afterCreateV6AvailableIPs+1, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should increase by 1 after IptablesEIP deletion")
			framework.ExpectEqual(afterCreateV6UsingIPs-1, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should decrease by 1 after IptablesEIP deletion")

			// Verify IP range changed
			framework.ExpectNotEqual(afterCreateV6AvailableIPRange, afterDeleteSubnet.Status.V6AvailableIPRange,
				"V6AvailableIPRange should change after IptablesEIP deletion")
			framework.ExpectNotEqual(afterCreateV6UsingIPRange, afterDeleteSubnet.Status.V6UsingIPRange,
				"V6UsingIPRange should change after IptablesEIP deletion")

			// Verify counts match initial state
			framework.ExpectEqual(initialV6AvailableIPs, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should return to initial value after IptablesEIP deletion")
			framework.ExpectEqual(initialV6UsingIPs, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should return to initial value after IptablesEIP deletion")
		default:
			// Dual stack
			framework.ExpectEqual(afterCreateV4AvailableIPs+1, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should increase by 1 after IptablesEIP deletion")
			framework.ExpectEqual(afterCreateV4UsingIPs-1, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should decrease by 1 after IptablesEIP deletion")
			framework.ExpectEqual(afterCreateV6AvailableIPs+1, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should increase by 1 after IptablesEIP deletion")
			framework.ExpectEqual(afterCreateV6UsingIPs-1, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should decrease by 1 after IptablesEIP deletion")

			framework.ExpectNotEqual(afterCreateV4AvailableIPRange, afterDeleteSubnet.Status.V4AvailableIPRange,
				"V4AvailableIPRange should change after IptablesEIP deletion")
			framework.ExpectNotEqual(afterCreateV4UsingIPRange, afterDeleteSubnet.Status.V4UsingIPRange,
				"V4UsingIPRange should change after IptablesEIP deletion")
			framework.ExpectNotEqual(afterCreateV6AvailableIPRange, afterDeleteSubnet.Status.V6AvailableIPRange,
				"V6AvailableIPRange should change after IptablesEIP deletion")
			framework.ExpectNotEqual(afterCreateV6UsingIPRange, afterDeleteSubnet.Status.V6UsingIPRange,
				"V6UsingIPRange should change after IptablesEIP deletion")

			framework.ExpectEqual(initialV4AvailableIPs, afterDeleteSubnet.Status.V4AvailableIPs,
				"V4AvailableIPs should return to initial value after IptablesEIP deletion")
			framework.ExpectEqual(initialV4UsingIPs, afterDeleteSubnet.Status.V4UsingIPs,
				"V4UsingIPs should return to initial value after IptablesEIP deletion")
			framework.ExpectEqual(initialV6AvailableIPs, afterDeleteSubnet.Status.V6AvailableIPs,
				"V6AvailableIPs should return to initial value after IptablesEIP deletion")
			framework.ExpectEqual(initialV6UsingIPs, afterDeleteSubnet.Status.V6UsingIPs,
				"V6UsingIPs should return to initial value after IptablesEIP deletion")
		}

		ginkgo.By("11. Test completed: IptablesEIP CR creation and deletion properly updates external subnet status via finalizer handlers")
	})

	framework.ConformanceIt("Test IptablesEIP finalizer cannot be removed when used by NAT rules", func() {
		f.SkipVersionPriorTo(1, 13, "This feature was introduced in v1.13")

		overlaySubnetV4Cidr := "10.0.4.0/24"
		overlaySubnetV4Gw := "10.0.4.1"
		lanIP := "10.0.4.254"
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

		ginkgo.By("1. Create a VIP for FIP")
		vipName := "test-vip-" + framework.RandomSuffix()
		vip := framework.MakeVip(f.Namespace.Name, vipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(vip)
		vip = vipClient.Get(vipName)

		ginkgo.By("2. Create IptablesEIP")
		eipName := "test-eip-with-fip-" + framework.RandomSuffix()
		eip := framework.MakeIptablesEIP(eipName, "", "", "", vpcNatGwName, "", "")
		_ = iptablesEIPClient.CreateSync(eip)

		ginkgo.By("3. Wait for IptablesEIP to be ready")
		eipCR := waitForIptablesEIPReady(iptablesEIPClient, eipName, 60*time.Second)
		framework.ExpectNotNil(eipCR, "IptablesEIP CR should be created and ready")

		ginkgo.By("4. Create IptablesFIP using the EIP")
		fipName := "test-fip-" + framework.RandomSuffix()
		fip := framework.MakeIptablesFIPRule(fipName, eipName, vip.Status.V4ip)
		_ = iptablesFIPClient.CreateSync(fip)

		ginkgo.By("5. Wait for EIP status to show it's being used by FIP")
		for range 60 {
			eipCR = iptablesEIPClient.Get(eipName)
			if eipCR != nil && strings.Contains(eipCR.Status.Nat, util.FipUsingEip) {
				break
			}
			time.Sleep(1 * time.Second)
		}
		framework.ExpectTrue(strings.Contains(eipCR.Status.Nat, util.FipUsingEip),
			"EIP status.Nat should contain 'fip' when used by FIP rule")

		ginkgo.By("6. Delete the IptablesEIP (should not remove finalizer while FIP exists)")
		err := f.KubeOVNClientSet.KubeovnV1().IptablesEIPs().Delete(context.Background(), eipName, metav1.DeleteOptions{})
		framework.ExpectNoError(err, "Deleting IptablesEIP should succeed")

		ginkgo.By("7. Wait and verify EIP still exists with finalizer (blocked by FIP)")
		time.Sleep(5 * time.Second)
		eipCR = iptablesEIPClient.Get(eipName)
		framework.ExpectNotNil(eipCR, "IptablesEIP should still exist")
		framework.ExpectNotNil(eipCR.DeletionTimestamp, "IptablesEIP should have DeletionTimestamp")
		framework.ExpectContainElement(eipCR.Finalizers, util.KubeOVNControllerFinalizer,
			"IptablesEIP should still have finalizer because it's being used by FIP")

		ginkgo.By("8. Delete the FIP to unblock EIP deletion")
		iptablesFIPClient.DeleteSync(fipName)

		ginkgo.By("9. Wait for FIP to be deleted")
		fipDeleted := false
		for range 30 {
			_, err := f.KubeOVNClientSet.KubeovnV1().IptablesFIPRules().Get(context.Background(), fipName, metav1.GetOptions{})
			if err != nil && k8serrors.IsNotFound(err) {
				fipDeleted = true
				break
			}
			time.Sleep(1 * time.Second)
		}
		framework.ExpectTrue(fipDeleted, "FIP should be deleted")

		ginkgo.By("10. Wait for EIP status.Nat to be cleared or EIP to be deleted")
		for range 30 {
			eipCR, err := f.KubeOVNClientSet.KubeovnV1().IptablesEIPs().Get(context.Background(), eipName, metav1.GetOptions{})
			if err != nil && k8serrors.IsNotFound(err) {
				// EIP already deleted, which is expected
				break
			}
			framework.ExpectNoError(err, "Failed to get IptablesEIP")
			if eipCR.Status.Nat == "" {
				break
			}
			time.Sleep(1 * time.Second)
		}

		ginkgo.By("11. Verify EIP is now deleted after FIP is removed")
		eipDeleted := false
		for range 30 {
			_, err := f.KubeOVNClientSet.KubeovnV1().IptablesEIPs().Get(context.Background(), eipName, metav1.GetOptions{})
			if err != nil && k8serrors.IsNotFound(err) {
				eipDeleted = true
				break
			}
			time.Sleep(1 * time.Second)
		}
		framework.ExpectTrue(eipDeleted, "IptablesEIP should be deleted after FIP is removed")

		ginkgo.By("12. Clean up VIP")
		vipClient.DeleteSync(vipName)

		ginkgo.By("13. Test completed: IptablesEIP finalizer correctly blocks deletion when used by NAT rules")
	})

	framework.ConformanceIt("Test VPC NAT Gateway with no IPAM NAD and noDefaultEIP", func() {
		f.SkipVersionPriorTo(1, 13, "This feature was introduced in v1.13")

		overlaySubnetV4Cidr := "10.0.5.0/24"
		overlaySubnetV4Gw := "10.0.5.1"
		lanIP := "10.0.5.254"
		natgwQoS := ""
		noIPAMNadName := "no-ipam-nad-" + framework.RandomSuffix()
		noIPAMProvider := fmt.Sprintf("%s.%s", noIPAMNadName, framework.KubeOvnNamespace)

		ginkgo.By("1. Setting up NAD without IPAM and creating subnet using standard flow")
		// Create NAD without IPAM section
		ginkgo.By("Getting docker network " + dockerExtNet1Name)
		network, err := docker.NetworkInspect(dockerExtNet1Name)
		framework.ExpectNoError(err, "getting docker network "+dockerExtNet1Name)

		ginkgo.By("Creating network attachment definition without IPAM " + noIPAMNadName)
		// NAD config without ipam - this is the key difference
		attachConf := fmt.Sprintf(`{
			"cniVersion": "0.3.0",
			"type": "macvlan",
			"master": "%s",
			"mode": "bridge"
		}`, net1NicName)

		attachNet := framework.MakeNetworkAttachmentDefinition(noIPAMNadName, framework.KubeOvnNamespace, attachConf)
		nad := attachNetClient.Create(attachNet)
		ginkgo.By("Got network attachment definition " + nad.Name)

		ginkgo.By("Creating underlay macvlan subnet " + noIPAMNadName)
		var cidrV4, cidrV6, gatewayV4, gatewayV6 string
		for _, config := range dockerExtNet1Network.IPAM.Config {
			switch util.CheckProtocol(config.Subnet.Addr().String()) {
			case apiv1.ProtocolIPv4:
				if f.HasIPv4() {
					cidrV4 = config.Subnet.String()
					gatewayV4 = config.Gateway.String()
				}
			case apiv1.ProtocolIPv6:
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
		macvlanSubnet := framework.MakeSubnet(noIPAMNadName, "", strings.Join(cidr, ","), strings.Join(gateway, ","), "", noIPAMProvider, excludeIPs, nil, nil)
		_ = subnetClient.CreateSync(macvlanSubnet)

		ginkgo.By("2. Creating custom vpc " + vpcName)
		vpc := framework.MakeVpc(vpcName, lanIP, false, false, nil)
		_ = vpcClient.CreateSync(vpc)

		ginkgo.By("3. Creating custom overlay subnet " + overlaySubnetName)
		overlaySubnet := framework.MakeSubnet(overlaySubnetName, "", overlaySubnetV4Cidr, overlaySubnetV4Gw, vpcName, "", nil, nil, nil)
		_ = subnetClient.CreateSync(overlaySubnet)

		ginkgo.By("4. Creating custom vpc nat gw with noDefaultEIP=true " + vpcNatGwName)
		vpcNatGw := framework.MakeVpcNatGatewayWithNoDefaultEIP(vpcNatGwName, vpcName, overlaySubnetName, lanIP, noIPAMNadName, natgwQoS, true)
		_ = vpcNatGwClient.CreateSync(vpcNatGw, f.ClientSet)

		ginkgo.By("5. Verifying VPC NAT Gateway is created")
		createdGw := vpcNatGwClient.Get(vpcNatGwName)
		framework.ExpectNotNil(createdGw, "VPC NAT Gateway should be created")
		framework.ExpectTrue(createdGw.Spec.NoDefaultEIP, "noDefaultEIP should be true")

		ginkgo.By("6. Verifying no default EIP is created")
		time.Sleep(10 * time.Second)
		eips, err := f.KubeOVNClientSet.KubeovnV1().IptablesEIPs().List(context.Background(), metav1.ListOptions{})
		framework.ExpectNoError(err, "Failed to list IptablesEIPs")
		hasDefaultEIP := false
		for _, eip := range eips.Items {
			if eip.Spec.NatGwDp == vpcNatGwName {
				hasDefaultEIP = true
				break
			}
		}
		framework.ExpectFalse(hasDefaultEIP, "No default EIP should be created when noDefaultEIP is true")

		ginkgo.By("7. Testing manual EIP creation")
		eipName := "manual-eip-" + framework.RandomSuffix()
		eip := framework.MakeIptablesEIP(eipName, "", "", "", vpcNatGwName, noIPAMNadName, "")
		_ = iptablesEIPClient.CreateSync(eip)

		ginkgo.By("8. Verifying manually created EIP")
		eipCR := waitForIptablesEIPReady(iptablesEIPClient, eipName, 60*time.Second)
		framework.ExpectNotNil(eipCR, "Manual EIP should be created successfully")
		framework.ExpectNotEmpty(eipCR.Status.IP, "Manual EIP should have IP assigned")

		ginkgo.By("9. Testing VIP and FIP with manual EIP")
		vipName := "test-vip-no-ipam-" + framework.RandomSuffix()
		vip := framework.MakeVip(f.Namespace.Name, vipName, overlaySubnetName, "", "", "")
		_ = vipClient.CreateSync(vip)
		vip = vipClient.Get(vipName)

		fipName := "test-fip-no-ipam-" + framework.RandomSuffix()
		fip := framework.MakeIptablesFIPRule(fipName, eipName, vip.Status.V4ip)
		_ = iptablesFIPClient.CreateSync(fip)

		ginkgo.By("10. Verifying FIP is created successfully")
		createdFip := iptablesFIPClient.Get(fipName)
		framework.ExpectNotNil(createdFip, "FIP should be created successfully")
		framework.ExpectTrue(createdFip.Status.Ready, "FIP should be ready")

		ginkgo.By("11. Cleaning up resources")
		iptablesFIPClient.DeleteSync(fipName)
		vipClient.DeleteSync(vipName)
		iptablesEIPClient.DeleteSync(eipName)

		vpcNatGwClient.DeleteSync(vpcNatGwName)
		subnetClient.DeleteSync(overlaySubnetName)
		subnetClient.DeleteSync(noIPAMNadName)
		vpcClient.DeleteSync(vpcName)
		attachNetClient.Delete(noIPAMNadName)

		ginkgo.By("12. Test completed: VPC NAT Gateway with no IPAM NAD and noDefaultEIP works correctly")
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
