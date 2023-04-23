package ovn_eip

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dockertypes "github.com/docker/docker/api/types"
	attachnetclientset "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned"
	"github.com/onsi/ginkgo/v2"
	clientset "k8s.io/client-go/kubernetes"

	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const dockerNetworkName = "kube-ovn-vlan"
const vpcNatGWConfigMapName = "ovn-vpc-nat-gw-config"
const networkAttachDefName = "ovn-vpc-external-network"
const externalSubnetProvider = "ovn-vpc-external-network.kube-system"

var _ = framework.Describe("[group:iptables-vpc-nat-gw]", func() {
	f := framework.NewDefaultFramework("iptables-vpc-nat-gw")

	var skip bool
	var cs clientset.Interface
	var attachNetClient attachnetclientset.Interface
	var clusterName, vpcName, vpcNatGwName, overlaySubnetName string
	var vpcClient *framework.VpcClient
	var vpcNatGwClient *framework.VpcNatGatewayClient
	var subnetClient *framework.SubnetClient
	var fipVipName, fipEipName, fipName, dnatVipName, dnatEipName, dnatName, snatEipName, snatName string
	var vipClient *framework.VipClient
	var ipClient *framework.IpClient
	var iptablesEIPClient *framework.IptablesEIPClient
	var iptablesFIPClient *framework.IptablesFIPClient
	var iptablesSnatRuleClient *framework.IptablesSnatClient
	var iptablesDnatRuleClient *framework.IptablesDnatClient

	var dockerNetwork *dockertypes.NetworkResource
	var containerID string
	var image string

	vpcName = "vpc-" + framework.RandomSuffix()
	vpcNatGwName = "gw-" + framework.RandomSuffix()

	fipVipName = "fip-vip-" + framework.RandomSuffix()
	fipEipName = "fip-eip-" + framework.RandomSuffix()
	fipName = "fip-" + framework.RandomSuffix()

	dnatVipName = "dnat-vip-" + framework.RandomSuffix()
	dnatEipName = "dnat-eip-" + framework.RandomSuffix()
	dnatName = "dnat-" + framework.RandomSuffix()

	snatEipName = "snat-eip-" + framework.RandomSuffix()
	snatName = "snat-" + framework.RandomSuffix()
	overlaySubnetName = "overlay-subnet-" + framework.RandomSuffix()

	ginkgo.BeforeEach(func() {
		containerID = ""
		cs = f.ClientSet
		attachNetClient = f.AttachNetClient
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

		ginkgo.By("Validating node links")
		var eth0Exist, eth1Exist bool
		for _, node := range nodes {
			links, err := node.ListLinks()
			framework.ExpectNoError(err, "failed to list links on node %s: %v", node.Name(), err)

			for _, link := range links {
				ginkgo.By("exist node nic " + link.IfName)
				if link.IfName == "eth0" {
					eth0Exist = true
				}
				if link.IfName == "eth1" {
					eth1Exist = true
				}
			}
			framework.ExpectTrue(eth0Exist)
			// nat gw pod use eth1 in this case
			// retest this case should rebuild kind cluster
			framework.ExpectTrue(eth1Exist)
		}
	})

	ginkgo.AfterEach(func() {
		if containerID != "" {
			ginkgo.By("Deleting container " + containerID)
			err := docker.ContainerRemove(containerID)
			framework.ExpectNoError(err)
		}

		ginkgo.By("Deleting overlay subnet " + overlaySubnetName)
		subnetClient.DeleteSync(overlaySubnetName)

		ginkgo.By("Deleting macvlan underlay subnet " + networkAttachDefName)
		subnetClient.DeleteSync(networkAttachDefName)

		ginkgo.By("Getting nodes")
		nodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in cluster")

		if dockerNetwork != nil {
			ginkgo.By("Disconnecting nodes from the docker network")
			err = kind.NetworkDisconnect(dockerNetwork.ID, nodes)
			framework.ExpectNoError(err, "disconnecting nodes from network "+dockerNetworkName)
		}
	})

	framework.ConformanceIt("iptables eip fip snat dnat", func() {
		ginkgo.By("Getting docker network " + dockerNetworkName)
		network, err := docker.NetworkInspect(dockerNetworkName)
		framework.ExpectNoError(err, "getting docker network "+dockerNetworkName)

		ginkgo.By("Getting k8s nodes")
		_, err = e2enode.GetReadySchedulableNodes(cs)
		framework.ExpectNoError(err)

		ginkgo.By("Getting network attachment fefinition " + networkAttachDefName)
		networkClient := attachNetClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(framework.KubeOvnNamespace)
		nad, err := networkClient.Get(context.Background(), networkAttachDefName, metav1.GetOptions{})
		framework.ExpectNoError(err, "failed to get")
		ginkgo.By("Got network attachment definition " + nad.Name)

		ginkgo.By("Creating underlay macvlan subnet " + networkAttachDefName)
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
		macvlanSubnet := framework.MakeSubnet(networkAttachDefName, "", strings.Join(cidr, ","), strings.Join(gateway, ","), "", externalSubnetProvider, excludeIPs, nil, nil)
		_ = subnetClient.CreateSync(macvlanSubnet)

		ginkgo.By("Getting config map " + vpcNatGWConfigMapName)
		_, err = cs.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Get(context.Background(), vpcNatGWConfigMapName, metav1.GetOptions{})
		framework.ExpectNoError(err, "failed to get ConfigMap")

		ginkgo.By("Creating custom vpc")
		overlaySubnetV4Cidr := "192.168.0.0/24"
		overlaySubnetV4Gw := "192.168.0.1"
		lanIp := "192.168.0.254"
		vpc := framework.MakeVpc(vpcName, lanIp, false, false)
		_ = vpcClient.CreateSync(vpc)

		ginkgo.By("Creating custom overlay subnet")
		overlaySubnet := framework.MakeSubnet(overlaySubnetName, "", overlaySubnetV4Cidr, overlaySubnetV4Gw, vpcName, "", nil, nil, nil)
		_ = subnetClient.CreateSync(overlaySubnet)

		ginkgo.By("Creating custom vpc nat gw")
		vpcNatGw := framework.MakeVpcNatGateway(vpcNatGwName, vpcName, overlaySubnetName, lanIp)
		_ = vpcNatGwClient.CreateSync(vpcNatGw)

		ginkgo.By("Creating iptables vip for fip")
		fipVip := framework.MakeVip(fipVipName, overlaySubnetName, "", "")
		_ = vipClient.CreateSync(fipVip)
		fipVip = vipClient.Get(fipVipName)
		ginkgo.By("Creating iptables eip for fip")
		fipEip := framework.MakeIptablesEIP(fipEipName, "", "", "", vpcNatGwName)
		_ = iptablesEIPClient.CreateSync(fipEip)
		ginkgo.By("Creating iptables fip")
		fip := framework.MakeIptablesFIPRule(fipName, fipEipName, fipVip.Status.V4ip)
		_ = iptablesFIPClient.CreateSync(fip)

		ginkgo.By("Creating iptables eip for snat")
		snatEip := framework.MakeIptablesEIP(snatEipName, "", "", "", vpcNatGwName)
		_ = iptablesEIPClient.CreateSync(snatEip)
		ginkgo.By("Creating iptables snat")
		snat := framework.MakeIptablesSnatRule(snatName, snatEipName, overlaySubnetV4Cidr)
		_ = iptablesSnatRuleClient.CreateSync(snat)

		ginkgo.By("Creating iptables vip for dnat")
		dnatVip := framework.MakeVip(dnatVipName, overlaySubnetName, "", "")
		_ = vipClient.CreateSync(dnatVip)
		dnatVip = vipClient.Get(dnatVipName)
		ginkgo.By("Creating iptables eip for dnat")
		dnatEip := framework.MakeIptablesEIP(dnatEipName, "", "", "", vpcNatGwName)
		_ = iptablesEIPClient.CreateSync(dnatEip)
		ginkgo.By("Creating iptables dnat")
		dnat := framework.MakeIptablesDnatRule(dnatName, dnatEipName, "80", "tcp", dnatVip.Status.V4ip, "8080")
		_ = iptablesDnatRuleClient.CreateSync(dnat)

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

		ginkgo.By("Deleting vip " + fipVipName)
		vipClient.DeleteSync(fipVipName)
		ginkgo.By("Deleting vip " + dnatVipName)
		vipClient.DeleteSync(dnatVipName)

		ginkgo.By("Deleting custom vpc " + vpcName)
		vpcClient.DeleteSync(vpcName)

		ginkgo.By("Deleting custom vpc nat gw")
		vpcNatGwClient.DeleteSync(vpcNatGwName)

		ginkgo.By("Deleting configmap " + vpcNatGWConfigMapName)
		err = cs.CoreV1().ConfigMaps(framework.KubeOvnNamespace).Delete(context.Background(), vpcNatGWConfigMapName, metav1.DeleteOptions{})
		framework.ExpectNoError(err, "failed to delete ConfigMap")

		// the only pod for vpc nat gateway
		vpcNatGwPodName := "vpc-nat-gw-" + vpcNatGwName + "-0"

		// delete vpc nat gw statefulset remaining ip for eth0 and net1
		overlaySubnet = subnetClient.Get(overlaySubnetName)
		macvlanSubnet = subnetClient.Get(networkAttachDefName)
		eth0IpName := ovs.PodNameToPortName(vpcNatGwPodName, framework.KubeOvnNamespace, overlaySubnet.Spec.Provider)
		net1IpName := ovs.PodNameToPortName(vpcNatGwPodName, framework.KubeOvnNamespace, macvlanSubnet.Spec.Provider)
		ginkgo.By("Deleting vpc nat gw eth0 ip " + eth0IpName)
		ipClient.DeleteSync(eth0IpName)
		ginkgo.By("Deleting vpc nat gw net1 ip " + net1IpName)
		ipClient.DeleteSync(net1IpName)

		err = networkClient.Delete(context.Background(), networkAttachDefName, metav1.DeleteOptions{})
		framework.ExpectNoError(err, "failed to delete")
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
