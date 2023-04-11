package ovn_eip

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dockertypes "github.com/docker/docker/api/types"
	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	attachnetclientset "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned"
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
const vpcNatGWConfigMapName = "ovn-vpc-nat-gw-config"
const networkAttachDefName = "ovn-vpc-external-network"
const externalSubnetProvider = "ovn-vpc-external-network.kube-system"

func makeNetworkAttachmentDefinition(name, config string) *nadv1.NetworkAttachmentDefinition {
	netAttachDef := nadv1.NetworkAttachmentDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: nadv1.NetworkAttachmentDefinitionSpec{Config: config},
	}
	return &netAttachDef
}

var _ = framework.Describe("[group:iptables-vpc-nat-gw]", func() {
	f := framework.NewDefaultFramework("iptables-vpc-nat-gw")

	var skip bool
	var cs clientset.Interface
	var attachNetClient attachnetclientset.Interface
	var nodeNames []string
	var clusterName, vpcName, vpcNatGwName, overlaySubnetName string
	var linkMap map[string]*iproute.Link
	var vpcClient *framework.VpcClient
	var vpcNatGwClient *framework.VpcNatGatewayClient
	var subnetClient *framework.SubnetClient
	var IptablesEIPClient *framework.IptablesEIPClient
	var fipVipName, fipEipName, fipName, dnatVipName, dnatEipName, dnatName, snatEipName, snatName string
	var vipClient *framework.VipClient
	var IptablesFIPClient *framework.IptablesFIPClient
	var IptablesSnatRuleClient *framework.IptablesSnatClient
	var IptablesDnatRuleClient *framework.IptablesDnatClient

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
		IptablesEIPClient = f.IptablesEIPClient()
		vipClient = f.VipClient()
		IptablesFIPClient = f.IptablesFIPClient()
		IptablesSnatRuleClient = f.IptablesSnatClient()
		IptablesDnatRuleClient = f.IptablesDnatClient()

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

	})

	ginkgo.AfterEach(func() {
		if containerID != "" {
			ginkgo.By("Deleting container " + containerID)
			err := docker.ContainerRemove(containerID)
			framework.ExpectNoError(err)
		}

		// todo:// statefulset nat gw pod ip not release automatically
		// these two subnet will not be deleted successfully

		// ginkgo.By("Deleting subnet " + networkAttachDefName)
		// subnetClient.DeleteSync(networkAttachDefName)

		// ginkgo.By("Deleting overlay subnet " + overlaySubnetName)
		// subnetClient.DeleteSync(overlaySubnetName)

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

		ginkgo.By("Creating network attachment fefinition " + networkAttachDefName)
		nadConfig := `{
			"cniVersion": "0.3.0",
			"type": "macvlan",
			"master": "eth1",
			"mode": "bridge",
			"ipam": {
			  "type": "kube-ovn",
			  "server_socket": "/run/openvswitch/kube-ovn-daemon.sock",
			  "provider": "ovn-vpc-external-network.kube-system"
			}
		  }`

		networkClient := attachNetClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions("kube-system")
		nad := makeNetworkAttachmentDefinition(networkAttachDefName, nadConfig)
		_, err = networkClient.Create(context.Background(), nad, metav1.CreateOptions{})
		framework.ExpectNoError(err, "failed to create")

		nad, err = networkClient.Get(context.Background(), networkAttachDefName, metav1.GetOptions{})
		ginkgo.By("Got network attachment fefinition " + nad.Name)
		framework.ExpectNoError(err, "failed to get")

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

		ginkgo.By("Creating config map " + vpcNatGWConfigMapName)
		cmData := map[string]string{
			"enable-vpc-nat-gw": "true",
		}
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vpcNatGWConfigMapName,
				Namespace: "kube-system",
			},
			Data: cmData,
		}
		_, err = cs.CoreV1().ConfigMaps("kube-system").Create(context.Background(), configMap, metav1.CreateOptions{})
		framework.ExpectNoError(err, "failed to create ConfigMap")

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
		_ = IptablesEIPClient.CreateSync(fipEip)
		ginkgo.By("Creating iptables fip")
		fip := framework.MakeIptablesFIPRule(fipName, fipEipName, fipVip.Status.V4ip)
		_ = IptablesFIPClient.CreateSync(fip)

		ginkgo.By("Creating iptables eip for snat")
		snatEip := framework.MakeIptablesEIP(snatEipName, "", "", "", vpcNatGwName)
		_ = IptablesEIPClient.CreateSync(snatEip)
		ginkgo.By("Creating iptables snat")
		snat := framework.MakeIptablesSnatRule(snatName, snatEipName, overlaySubnetV4Cidr)
		_ = IptablesSnatRuleClient.CreateSync(snat)

		ginkgo.By("Creating iptables vip for dnat")
		dnatVip := framework.MakeVip(dnatVipName, overlaySubnetName, "", "")
		_ = vipClient.CreateSync(dnatVip)
		dnatVip = vipClient.Get(dnatVipName)
		ginkgo.By("Creating iptables eip for dnat")
		dnatEip := framework.MakeIptablesEIP(dnatEipName, "", "", "", vpcNatGwName)
		_ = IptablesEIPClient.CreateSync(dnatEip)
		ginkgo.By("Creating iptables dnat")
		dnat := framework.MakeIptablesDnatRule(dnatName, dnatEipName, "80", "tcp", dnatVip.Status.V4ip, "8080")
		_ = IptablesDnatRuleClient.CreateSync(dnat)

		ginkgo.By("Deleting iptables fip " + fipName)
		IptablesFIPClient.DeleteSync(fipName)
		ginkgo.By("Deleting iptables dnat " + dnatName)
		IptablesDnatRuleClient.DeleteSync(dnatName)
		ginkgo.By("Deleting iptables snat " + snatName)
		IptablesSnatRuleClient.DeleteSync(snatName)

		ginkgo.By("Deleting iptables eip " + fipEipName)
		IptablesEIPClient.DeleteSync(fipEipName)
		ginkgo.By("Deleting iptables eip " + dnatEipName)
		IptablesEIPClient.DeleteSync(dnatEipName)
		ginkgo.By("Deleting iptables eip " + snatEipName)
		IptablesEIPClient.DeleteSync(snatEipName)

		ginkgo.By("Deleting vip " + fipVipName)
		vipClient.DeleteSync(fipVipName)
		ginkgo.By("Deleting vip " + dnatVipName)
		vipClient.DeleteSync(dnatVipName)

		ginkgo.By("Deleting custom vpc " + vpcName)
		vpcClient.DeleteSync(vpcName)

		ginkgo.By("Deleting custom vpc nat gw")
		vpcNatGwClient.DeleteSync(vpcNatGwName)

		ginkgo.By("Deleting configmap " + vpcNatGWConfigMapName)
		err = cs.CoreV1().ConfigMaps("kube-system").Delete(context.Background(), vpcNatGWConfigMapName, metav1.DeleteOptions{})
		framework.ExpectNoError(err, "failed to delete ConfigMap")

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
