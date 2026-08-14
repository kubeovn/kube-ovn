package multus

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"math/rand/v2"
	"net"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	dockernetwork "github.com/moby/moby/api/types/network"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilversion "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/cmd/kubeadm/app/constants"
	commontest "k8s.io/kubernetes/test/e2e/common"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"
	"k8s.io/utils/set"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iproute"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

var (
	uuidRegexp    = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	ipTokenRegexp = regexp.MustCompile(`[0-9A-Fa-f:.]+`)
)

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	// Register flags.
	config.CopyFlags(config.Flags, flag.CommandLine)
	k8sframework.RegisterCommonFlags(flag.CommandLine)
	k8sframework.RegisterClusterFlags(flag.CommandLine)
}

func TestE2E(t *testing.T) {
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)

	logs.InitLogs()
	defer logs.FlushLogs()
	klog.EnableContextualLogging(true)

	gomega.RegisterFailHandler(k8sframework.Fail)

	// Run tests through the Ginkgo runner with output to console + JUnit for Jenkins
	suiteConfig, reporterConfig := k8sframework.CreateGinkgoConfig()
	klog.Infof("Starting e2e run %q on Ginkgo node %d", k8sframework.RunID, suiteConfig.ParallelProcess)
	ginkgo.RunSpecs(t, "Kube-OVN e2e suite", suiteConfig, reporterConfig)
}

const kindNetwork = "kind"

var clusterName string

var _ = ginkgo.SynchronizedBeforeSuite(func() []byte {
	// Reference common test to make the import valid.
	commontest.CurrentSuite = commontest.E2E

	cs, err := k8sframework.LoadClientset()
	framework.ExpectNoError(err)

	ginkgo.By("Getting k8s nodes")
	k8sNodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
	framework.ExpectNoError(err)

	var ok bool
	if clusterName, ok = kind.IsKindProvided(k8sNodes.Items[0].Spec.ProviderID); !ok {
		ginkgo.Fail("vpc-egress-gateway spec only runs on kind clusters")
	}

	return []byte(clusterName)
}, func(data []byte) {
	clusterName = string(data)
})

var _ = framework.SerialDescribe("[group:veg]", func() {
	f := framework.NewDefaultFramework("veg")

	var vpcClient *framework.VpcClient
	var subnetClient *framework.SubnetClient
	var nadClient *framework.NetworkAttachmentDefinitionClient
	var nadName, externalSubnetName, namespaceName string
	var nodes, schedulableNodes []corev1.Node
	var controlPlaneNodeNames []string
	var replicas int32
	ginkgo.BeforeEach(func() {
		namespaceName = f.Namespace.Name
		nadName = "nad-" + framework.RandomSuffix()
		externalSubnetName = "ext-" + framework.RandomSuffix()
		vpcClient = f.VpcClient()
		subnetClient = f.SubnetClient()
		nadClient = f.NetworkAttachmentDefinitionClient()

		nodeList, err := e2enode.GetReadyNodesIncludingTainted(context.Background(), f.ClientSet)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)
		nodes = nodeList.Items

		if len(controlPlaneNodeNames) == 0 {
			for _, node := range nodes {
				if _, ok := node.Labels[constants.LabelNodeRoleControlPlane]; !ok {
					continue
				}
				if len(node.Spec.Taints) != 0 && node.Spec.Taints[0] == constants.ControlPlaneTaint {
					controlPlaneNodeNames = append(controlPlaneNodeNames, node.Name)
				}
			}
			framework.ExpectNotEmpty(controlPlaneNodeNames, "no control plane nodes found")
		}
		framework.Logf("control plane nodes with NoSchedule taint: %v", controlPlaneNodeNames)

		nodeList, err = e2enode.GetReadySchedulableNodes(context.Background(), f.ClientSet)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)
		schedulableNodes = nodeList.Items

		replicas = min(int32(len(schedulableNodes)), 3)
	})

	createMacvlanVpc := func() (string, *apiv1.Vpc, string) {
		ginkgo.GinkgoHelper()

		provider := fmt.Sprintf("%s.%s", nadName, namespaceName)
		ginkgo.By("Creating network attachment definition " + nadName)
		nad := framework.MakeMacvlanNetworkAttachmentDefinition(nadName, namespaceName, "eth0", "bridge", provider, nil)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting network attachment definition " + nadName)
			nadClient.Delete(nadName)
		})
		nad = nadClient.Create(nad)
		framework.Logf("created network attachment definition config:\n%s", nad.Spec.Config)

		vpcName := "vpc-" + framework.RandomSuffix()
		ginkgo.By("Creating vpc " + vpcName)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting vpc " + vpcName)
			vpcClient.DeleteSync(vpcName)
		})
		vpc := vpcClient.CreateSync(&apiv1.Vpc{ObjectMeta: metav1.ObjectMeta{Name: vpcName}})

		internalSubnetName := "int-" + framework.RandomSuffix()
		ginkgo.By("Creating internal subnet " + internalSubnetName)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting internal subnet " + internalSubnetName)
			subnetClient.DeleteSync(internalSubnetName)
		})
		cidr := framework.RandomCIDR(f.ClusterIPFamily)
		internalSubnet := framework.MakeSubnet(internalSubnetName, "", cidr, "", vpcName, "", nil, nil, nil)
		_ = subnetClient.CreateSync(internalSubnet)

		ginkgo.By("Getting docker network " + kindNetwork)
		network, err := docker.NetworkInspect(kindNetwork)
		framework.ExpectNoError(err, "getting docker network "+kindNetwork)
		externalSubnet := generateSubnetFromDockerNetwork(externalSubnetName, network, f.HasIPv4(), f.HasIPv6())
		externalSubnet.Spec.Provider = provider
		ginkgo.By("Creating macvlan subnet " + externalSubnetName)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting external subnet " + externalSubnetName)
			subnetClient.DeleteSync(externalSubnetName)
		})
		_ = subnetClient.CreateSync(externalSubnet)

		return provider, vpc, internalSubnetName
	}

	framework.ConformanceIt("should be able to specify tolerations", func() {
		provider := fmt.Sprintf("%s.%s", nadName, namespaceName)

		ginkgo.By("Creating network attachment definition " + nadName)
		nad := framework.MakeMacvlanNetworkAttachmentDefinition(nadName, namespaceName, "eth0", "bridge", provider, nil)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting network attachment definition " + nadName)
			nadClient.Delete(nadName)
		})
		nad = nadClient.Create(nad)
		framework.Logf("created network attachment definition config:\n%s", nad.Spec.Config)

		internalSubnetName := "int-" + framework.RandomSuffix()
		ginkgo.By("Creating internal subnet " + internalSubnetName)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting internal subnet " + internalSubnetName)
			subnetClient.DeleteSync(internalSubnetName)
		})
		cidr := framework.RandomCIDR(f.ClusterIPFamily)
		internalSubnet := framework.MakeSubnet(internalSubnetName, "", cidr, "", "", "", nil, nil, nil)
		_ = subnetClient.CreateSync(internalSubnet)

		ginkgo.By("Getting docker network " + kindNetwork)
		network, err := docker.NetworkInspect(kindNetwork)
		framework.ExpectNoError(err, "getting docker network "+kindNetwork)

		externalSubnet := generateSubnetFromDockerNetwork(externalSubnetName, network, f.HasIPv4(), f.HasIPv6())
		externalSubnet.Spec.Provider = provider

		ginkgo.By("Creating macvlan subnet " + externalSubnetName)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting external subnet " + externalSubnetName)
			subnetClient.DeleteSync(externalSubnetName)
		})
		_ = subnetClient.CreateSync(externalSubnet)

		vegTest(f, false, provider, nadName, "", internalSubnetName, externalSubnetName, int32(len(controlPlaneNodeNames)), "", controlPlaneNodeNames)
	})

	framework.ConformanceIt("should be able to create vpc-egress-gateway with underlay subnet", func() {
		provider := fmt.Sprintf("%s.%s.%s", nadName, namespaceName, util.OvnProvider)

		ginkgo.By("Creating network attachment definition " + nadName)
		nad := framework.MakeOVNNetworkAttachmentDefinition(nadName, namespaceName, provider, nil)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting network attachment definition " + nadName)
			nadClient.Delete(nadName)
		})
		nad = nadClient.Create(nad)
		framework.Logf("created network attachment definition config:\n%s", nad.Spec.Config)

		dockerNetworkName := "net-" + framework.RandomSuffix()
		ginkgo.By("Creating docker network " + dockerNetworkName)
		dockerNetwork, err := docker.NetworkCreate(dockerNetworkName, true, true)
		framework.ExpectNoError(err, "creating docker network "+dockerNetworkName)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting docker network " + dockerNetworkName)
			err = docker.NetworkRemove(dockerNetworkName)
			framework.ExpectNoError(err, "removing docker network "+dockerNetworkName)
		})

		ginkgo.By("Getting kind nodes")
		kindNodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")
		framework.ExpectNotEmpty(nodes)

		ginkgo.By("Connecting nodes to the docker network")
		err = kind.NetworkConnect(dockerNetwork.ID, kindNodes)
		framework.ExpectNoError(err, "connecting nodes to network "+dockerNetworkName)
		ginkgo.DeferCleanup(func() {
			err = kind.NetworkDisconnect(dockerNetwork.ID, kindNodes)
			framework.ExpectNoError(err, "disconnecting nodes from network "+dockerNetworkName)
		})

		ginkgo.By("Getting node links that belong to the docker network")
		kindNodes, err = kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")
		linkMap := make(map[string]*iproute.Link, len(nodes))
		for _, node := range kindNodes {
			links, err := node.ListLinks()
			framework.ExpectNoError(err, "failed to list links on node %s: %v", node.Name(), err)

			for _, link := range links {
				if link.Address == node.NetworkSettings.Networks[dockerNetworkName].MacAddress.String() {
					linkMap[node.Name()] = &link
					break
				}
			}
			framework.ExpectHaveKey(linkMap, node.Name())
		}

		providerNetworkName := "pn-" + framework.RandomSuffix()
		ginkgo.By("Creating provider network " + providerNetworkName)
		providerNetworkClient := f.ProviderNetworkClient()
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting provider network " + providerNetworkName)
			providerNetworkClient.DeleteSync(providerNetworkName)
		})
		var defaultInterface string
		customInterfaces := make(map[string][]string, 0)
		for node, link := range linkMap {
			if defaultInterface == "" {
				defaultInterface = link.IfName
			} else if link.IfName != defaultInterface {
				customInterfaces[link.IfName] = append(customInterfaces[link.IfName], node)
			}
		}
		pn := framework.MakeProviderNetwork(providerNetworkName, false, defaultInterface, customInterfaces, nil)
		_ = providerNetworkClient.CreateSync(pn)

		vlanName := "vlan-" + framework.RandomSuffix()
		ginkgo.By("Creating vlan " + vlanName)
		vlanClient := f.VlanClient()
		vlan := framework.MakeVlan(vlanName, providerNetworkName, 0)
		_ = vlanClient.Create(vlan)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting vlan " + vlanName)
			vlanClient.Delete(vlanName)
		})

		ginkgo.By("Getting docker network " + dockerNetworkName)
		network, err := docker.NetworkInspect(dockerNetworkName)
		framework.ExpectNoError(err, "getting docker network "+dockerNetworkName)

		externalSubnet := generateSubnetFromDockerNetwork(externalSubnetName, network, f.HasIPv4(), f.HasIPv6())
		externalSubnet.Spec.Provider = provider
		externalSubnet.Spec.Vlan = vlanName

		ginkgo.By("Creating underlay subnet " + externalSubnetName)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting external subnet " + externalSubnetName)
			subnetClient.DeleteSync(externalSubnetName)
		})
		_ = subnetClient.CreateSync(externalSubnet)

		vpcName := util.DefaultVpc
		vpc := vpcClient.Get(vpcName)
		ginkgo.By("Validating local traffic policy without BFD")
		vegTest(f, false, provider, nadName, vpcName, vpc.Status.DefaultLogicalSwitch, externalSubnetName, replicas, "", nil)

		cidr := framework.RandomCIDR(f.ClusterIPFamily)
		bfdIP := framework.RandomIPs(cidr, ";", 1)
		ginkgo.By("Enabling BFD Port with IP " + bfdIP + " for VPC " + vpcName)
		patchedVpc := vpc.DeepCopy()
		patchedVpc.Spec.BFDPort = &apiv1.BFDPort{
			Enabled: true,
			IP:      bfdIP,
			NodeSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      constants.LabelNodeRoleControlPlane,
					Operator: metav1.LabelSelectorOpExists,
				}},
			},
		}
		updatedVpc := vpcClient.PatchSync(vpc, patchedVpc, 10*time.Second)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Disabling BFD Port for VPC " + vpcName)
			patchedVpc := updatedVpc.DeepCopy()
			patchedVpc.Spec.BFDPort = nil
			vpcClient.PatchSync(updatedVpc, patchedVpc, 10*time.Second)
			framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
				vpc := vpcClient.Get(vpcName)
				return vpc.Status.BFDPort.Name == "" && len(vpc.Status.BFDPort.Nodes) == 0, nil
			}, "BFD port status to be cleared for VPC "+vpcName)
		})

		framework.ExpectNotEmpty(updatedVpc.Status.BFDPort.Name)
		for _, node := range nodes {
			if slices.Contains(updatedVpc.Status.BFDPort.Nodes, node.Name) {
				framework.ExpectHaveKey(node.Labels, constants.LabelNodeRoleControlPlane)
			} else {
				framework.ExpectNotHaveKey(node.Labels, constants.LabelNodeRoleControlPlane)
			}
		}

		// TODO: check ovn LRP

		vegTest(f, true, provider, nadName, vpcName, vpc.Status.DefaultLogicalSwitch, externalSubnetName, replicas, "", nil)
	})

	framework.ConformanceIt("should be able to create vpc-egress-gateway with macvlan", func() {
		provider, vpc, internalSubnetName := createMacvlanVpc()
		framework.ExpectEmpty(vpc.Status.BFDPort.Name)
		framework.ExpectEmpty(vpc.Status.BFDPort.IP)
		framework.ExpectEmpty(vpc.Status.BFDPort.Nodes)

		vegTest(f, false, provider, nadName, vpc.Name, internalSubnetName, externalSubnetName, replicas, "", nil)
	})

	registerVpcEgressGatewayObservabilityTest(f, &schedulableNodes, createMacvlanVpc, &nadName, &externalSubnetName)

	framework.ConformanceIt("should allow preferred pod anti-affinity", func() {
		f.SkipVersionPriorTo(1, 17, "VpcEgressGateway preferred pod anti-affinity requires v1.17+")

		provider, vpc, internalSubnetName := createMacvlanVpc()

		vegTest(f, false, provider, nadName, vpc.Name, internalSubnetName, externalSubnetName,
			2, apiv1.PodAntiAffinityPreferred, []string{schedulableNodes[0].Name})
	})

	framework.ConformanceIt("should be ready with default dual-stack internal subnet and IPv4-only external subnet", func() {
		if !f.IsDual() {
			ginkgo.Skip("dual-stack cluster is required")
		}

		provider := fmt.Sprintf("%s.%s", nadName, namespaceName)

		ginkgo.By("Creating network attachment definition " + nadName)
		nad := framework.MakeMacvlanNetworkAttachmentDefinition(nadName, namespaceName, "eth0", "bridge", provider, nil)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting network attachment definition " + nadName)
			nadClient.Delete(nadName)
		})
		nad = nadClient.Create(nad)
		framework.Logf("created network attachment definition config:\n%s", nad.Spec.Config)

		defaultVpc := vpcClient.Get(util.DefaultVpc)
		defaultSubnet := subnetClient.Get(defaultVpc.Status.DefaultLogicalSwitch)
		if util.CheckProtocol(defaultSubnet.Spec.CIDRBlock) != apiv1.ProtocolDual {
			ginkgo.Skip("default subnet is not dual-stack")
		}

		ginkgo.By("Getting docker network " + kindNetwork)
		network, err := docker.NetworkInspect(kindNetwork)
		framework.ExpectNoError(err, "getting docker network "+kindNetwork)

		externalSubnet := generateSubnetFromDockerNetwork(externalSubnetName, network, true, false)
		externalSubnet.Spec.Provider = provider
		framework.ExpectEqual(util.CheckProtocol(externalSubnet.Spec.CIDRBlock), apiv1.ProtocolIPv4)

		ginkgo.By("Creating IPv4-only macvlan subnet " + externalSubnetName)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting external subnet " + externalSubnetName)
			subnetClient.DeleteSync(externalSubnetName)
		})
		_ = subnetClient.CreateSync(externalSubnet)

		vegClient := f.VpcEgressGatewayClient()
		vegName := "veg-" + framework.RandomSuffix()
		veg := framework.MakeVpcEgressGateway(namespaceName, vegName, "", 1, "", externalSubnetName)
		veg.Spec.Policies = []apiv1.VpcEgressGatewayPolicy{{
			Subnets: []string{defaultSubnet.Name},
		}}
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting vpc egress gateway " + vegName)
			vegClient.DeleteSync(vegName)
		})

		ginkgo.By(fmt.Sprintf("Creating vpc egress gateway %s:\n%s", vegName, format.Object(veg, 2)))
		veg = vegClient.CreateSync(veg)
		framework.ExpectTrue(veg.Status.Ready)
		framework.ExpectHaveLen(veg.Status.InternalIPs, 1)
		framework.ExpectHaveLen(veg.Status.ExternalIPs, 1)
		_, externalIPv6 := util.SplitStringIP(veg.Status.ExternalIPs[0])
		framework.ExpectEmpty(externalIPv6)
	})

	framework.ConformanceIt("should update BFD HA chassis group when selected nodes change", func() {
		f.SkipVersionPriorTo(1, 14, "VPC BFDPort for VpcEgressGateway BFD requires v1.14+")
		if len(schedulableNodes) < 2 {
			ginkgo.Skip("at least two schedulable nodes are required")
		}

		provider := fmt.Sprintf("%s.%s", nadName, namespaceName)
		selectedNode1 := schedulableNodes[0].Name
		selectedNode2 := schedulableNodes[1].Name
		labelKey := "veg-bfd-e2e-" + framework.RandomSuffix()
		labelValue := "selected"

		ginkgo.DeferCleanup(func() {
			ginkgo.By("Cleaning up BFD selector labels")
			setNodeLabel(f.ClientSet, selectedNode1, labelKey, "")
			setNodeLabel(f.ClientSet, selectedNode2, labelKey, "")
		})

		ginkgo.By("Selecting the initial BFD node " + selectedNode1)
		setNodeLabel(f.ClientSet, selectedNode1, labelKey, labelValue)

		ginkgo.By("Creating network attachment definition " + nadName)
		nad := framework.MakeMacvlanNetworkAttachmentDefinition(nadName, namespaceName, "eth0", "bridge", provider, nil)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting network attachment definition " + nadName)
			nadClient.Delete(nadName)
		})
		nad = nadClient.Create(nad)
		framework.Logf("created network attachment definition config:\n%s", nad.Spec.Config)

		vpcName := "vpc-" + framework.RandomSuffix()
		internalSubnetName := "int-" + framework.RandomSuffix()
		internalCIDR := framework.RandomCIDR(f.ClusterIPFamily)
		bfdIP := framework.RandomIPs(internalCIDR, ";", 1)

		ginkgo.By("Creating VPC " + vpcName + " with BFDPort nodeSelector")
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting vpc " + vpcName)
			vpcClient.DeleteSync(vpcName)
		})
		vpc := framework.MakeVpc(vpcName, "", false, false, nil)
		vpc.Spec.BFDPort = &apiv1.BFDPort{
			Enabled: true,
			IP:      bfdIP,
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					labelKey: labelValue,
				},
			},
		}
		_ = vpcClient.CreateSync(vpc)

		ginkgo.By("Creating internal subnet " + internalSubnetName)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting internal subnet " + internalSubnetName)
			subnetClient.DeleteSync(internalSubnetName)
		})
		internalSubnet := framework.MakeSubnet(internalSubnetName, "", internalCIDR, "", vpcName, "", nil, nil, nil)
		_ = subnetClient.CreateSync(internalSubnet)

		ginkgo.By("Getting docker network " + kindNetwork)
		network, err := docker.NetworkInspect(kindNetwork)
		framework.ExpectNoError(err, "getting docker network "+kindNetwork)

		externalSubnet := generateSubnetFromDockerNetwork(externalSubnetName, network, f.HasIPv4(), f.HasIPv6())
		externalSubnet.Spec.Provider = provider

		ginkgo.By("Creating macvlan subnet " + externalSubnetName)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting external subnet " + externalSubnetName)
			subnetClient.DeleteSync(externalSubnetName)
		})
		_ = subnetClient.CreateSync(externalSubnet)

		vegClient := f.VpcEgressGatewayClient()
		vegName := "veg-" + framework.RandomSuffix()
		veg := framework.MakeVpcEgressGateway(namespaceName, vegName, vpcName, 2, internalSubnetName, externalSubnetName)
		veg.Spec.BFD.Enabled = true
		veg.Spec.Policies = []apiv1.VpcEgressGatewayPolicy{{
			Subnets: []string{internalSubnetName},
		}}
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting vpc egress gateway " + vegName)
			vegClient.DeleteSync(vegName)
		})

		ginkgo.By(fmt.Sprintf("Creating BFD vpc egress gateway %s:\n%s", vegName, format.Object(veg, 2)))
		veg = vegClient.CreateSync(veg)
		framework.ExpectTrue(veg.Status.Ready)
		framework.ExpectHaveLen(veg.Status.Workload.Nodes, 2)
		framework.ExpectHaveLen(veg.Status.InternalIPs, 2)
		framework.ExpectHaveLen(veg.Status.ExternalIPs, 2)

		groupName := "bfd@" + vpcName
		waitVpcBFDNodes(vpcClient, vpcName, []string{selectedNode1})
		waitHAChassisCount(groupName, 1)
		waitHAChassisGroupCount(groupName, 1)
		waitLRPHAChassisGroup(groupName, groupName)

		ginkgo.By("Adding BFD selector label to " + selectedNode2)
		setNodeLabel(f.ClientSet, selectedNode2, labelKey, labelValue)
		waitVpcBFDNodes(vpcClient, vpcName, []string{selectedNode1, selectedNode2})
		waitHAChassisCount(groupName, 2)
		waitHAChassisGroupCount(groupName, 2)
		waitLRPHAChassisGroup(groupName, groupName)

		ginkgo.By("Removing BFD selector label from " + selectedNode1)
		setNodeLabel(f.ClientSet, selectedNode1, labelKey, "")
		waitVpcBFDNodes(vpcClient, vpcName, []string{selectedNode2})
		waitHAChassisCount(groupName, 1)
		waitHAChassisGroupCount(groupName, 1)
		waitLRPHAChassisGroup(groupName, groupName)

		ginkgo.By("Removing all BFD selector labels")
		setNodeLabel(f.ClientSet, selectedNode2, labelKey, "")
		waitVpcBFDNodes(vpcClient, vpcName, nil)
		waitHAChassisCount(groupName, 0)
		waitHAChassisGroupCount(groupName, 0)
		waitLRPHAChassisGroup(groupName, groupName)
	})

	framework.ConformanceIt("should report not ready when workload pod attachment network status is missing", func() {
		f.SkipVersionPriorTo(1, 17, "VpcEgressGateway workload network status validation was introduced in v1.17")

		provider := fmt.Sprintf("%s.%s", nadName, namespaceName)

		ginkgo.By("Creating network attachment definition " + nadName)
		nad := framework.MakeMacvlanNetworkAttachmentDefinition(nadName, namespaceName, "eth0", "bridge", provider, nil)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting network attachment definition " + nadName)
			nadClient.Delete(nadName)
		})
		nad = nadClient.Create(nad)
		framework.Logf("created network attachment definition config:\n%s", nad.Spec.Config)

		internalSubnetName := "int-" + framework.RandomSuffix()
		ginkgo.By("Creating internal subnet " + internalSubnetName)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting internal subnet " + internalSubnetName)
			subnetClient.DeleteSync(internalSubnetName)
		})
		cidr := framework.RandomCIDR(f.ClusterIPFamily)
		internalSubnet := framework.MakeSubnet(internalSubnetName, "", cidr, "", "", "", nil, nil, nil)
		_ = subnetClient.CreateSync(internalSubnet)

		ginkgo.By("Getting docker network " + kindNetwork)
		network, err := docker.NetworkInspect(kindNetwork)
		framework.ExpectNoError(err, "getting docker network "+kindNetwork)

		externalSubnet := generateSubnetFromDockerNetwork(externalSubnetName, network, f.HasIPv4(), f.HasIPv6())
		externalSubnet.Spec.Provider = provider

		ginkgo.By("Creating macvlan subnet " + externalSubnetName)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting external subnet " + externalSubnetName)
			subnetClient.DeleteSync(externalSubnetName)
		})
		_ = subnetClient.CreateSync(externalSubnet)

		vegClient := f.VpcEgressGatewayClient()
		deployClient := f.DeploymentClient()
		podClient := f.PodClient()
		vegName := "veg-" + framework.RandomSuffix()
		veg := framework.MakeVpcEgressGateway(namespaceName, vegName, "", 1, internalSubnetName, externalSubnetName)
		veg.Spec.Policies = []apiv1.VpcEgressGatewayPolicy{{
			Subnets: []string{internalSubnetName},
		}}
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting vpc egress gateway " + vegName)
			vegClient.DeleteSync(vegName)
		})

		ginkgo.By(fmt.Sprintf("Creating vpc egress gateway %s:\n%s", vegName, format.Object(veg, 2)))
		veg = vegClient.CreateSync(veg)
		framework.ExpectTrue(veg.Status.Ready)
		framework.ExpectHaveLen(veg.Status.ExternalIPs, 1)

		ginkgo.By("Removing the workload pod attachment network status")
		deploy := deployClient.Get(veg.Status.Workload.Name)
		workloadPods, err := deployClient.GetPods(deploy)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(workloadPods.Items, 1)
		pod := podClient.GetPod(workloadPods.Items[0].Name)
		podIPs := util.PodIPs(*pod)
		framework.ExpectNotEmpty(podIPs)
		modifiedPod := pod.DeepCopy()
		if modifiedPod.Annotations == nil {
			modifiedPod.Annotations = map[string]string{}
		}
		modifiedPod.Annotations[nadv1.NetworkStatusAnnot] = fmt.Sprintf(`[{"name":"kube-ovn","ips":[%q]}]`, podIPs[0])
		_ = podClient.Patch(pod, modifiedPod)

		ginkgo.By("Validating vpc egress gateway reports workload network not ready")
		veg = vegClient.WaitUntil(vegName, func(g *apiv1.VpcEgressGateway) (bool, error) {
			condition := g.Status.Conditions.GetCondition(apiv1.Ready)
			return !g.Status.Ready &&
				g.Status.Phase == apiv1.PhaseProcessing &&
				len(g.Status.ExternalIPs) == 0 &&
				condition != nil &&
				condition.Status == corev1.ConditionFalse &&
				condition.Reason == "WorkloadNetworkNotReady", nil
		}, "WorkloadNetworkNotReady", 2*time.Second, 2*time.Minute)
		framework.ExpectFalse(veg.Status.Ready)
	})

	framework.ConformanceIt("should preserve vpc egress gateway port group during controller startup gc", func() {
		f.SkipVersionPriorTo(1, 14, "VpcEgressGateway port groups require v1.14+")

		vegKey := namespaceName + "/veg-" + framework.RandomSuffix()
		pgName := "VEG." + util.Sha256Hash([]byte(vegKey))[:12]

		ginkgo.By("Creating vpc egress gateway port group " + pgName)
		createVpcEgressGatewayPortGroup(pgName, vegKey)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting vpc egress gateway port group " + pgName)
			deletePortGroup(pgName)
		})
		waitPortGroupExists(pgName)

		ginkgo.By("Restarting kube-ovn-controller to trigger startup GC")
		deployClient := f.DeploymentClientNS(framework.KubeOvnNamespace)
		deployClient.RestartSync(deployClient.Get("kube-ovn-controller"))

		ginkgo.By("Validating vpc egress gateway port group still exists")
		waitPortGroupExists(pgName)
	})
})

func registerVpcEgressGatewayObservabilityTest(
	f *framework.Framework,
	schedulableNodes *[]corev1.Node,
	createMacvlanVpc func() (string, *apiv1.Vpc, string),
	nadName, externalSubnetName *string,
) {
	framework.ConformanceIt("should expose native observability metrics and JSON flow logs", func() {
		f.SkipVersionPriorTo(1, 17, "VpcEgressGateway observability requires v1.17+")
		serverVersion, err := f.ClientSet.Discovery().ServerVersion()
		framework.ExpectNoError(err)
		version, err := utilversion.ParseSemantic(serverVersion.GitVersion)
		framework.ExpectNoError(err)
		if version.LessThan(utilversion.MustParseSemantic("1.29.0")) {
			ginkgo.Skip("restartable init containers require Kubernetes 1.29 or later")
		}
		if len(*schedulableNodes) < 2 {
			ginkgo.Skip("at least two schedulable nodes are required")
		}

		provider, vpc, internalSubnetName := createMacvlanVpc()
		veg, _, snatSubnetName, snatLabelValue := createVegTestGateway(
			f, false, provider, vpc.Name, internalSubnetName, *externalSubnetName, 2, "", nil,
			func(veg *apiv1.VpcEgressGateway) {
				veg.Spec.Observability = &apiv1.VpcEgressGatewayObservability{
					InterfaceMetrics: apiv1.VpcEgressGatewayObservabilityFeature{Enabled: true},
					Conntrack: apiv1.VpcEgressGatewayConntrackObservability{
						Metrics: apiv1.VpcEgressGatewayObservabilityFeature{Enabled: true},
						Log:     apiv1.VpcEgressGatewayConntrackLog{Enabled: true},
					},
					ServiceMonitor: apiv1.VpcEgressGatewayServiceMonitor{Labels: map[string]string{"e2e": "vpc-egress-observability"}},
				}
			},
		)
		workloadPods, intIPs := validateVegTestWorkload(f, veg, nil)
		interfaceBytes := waitVpcEgressObserverInterfaceBytes(f, workloadPods)
		validateVegTestSNATAccess(f, veg, *nadName, snatSubnetName, snatLabelValue, workloadPods, intIPs)
		validateVpcEgressObservability(f, veg, workloadPods, interfaceBytes)

		reloadCounts := waitVpcEgressObserverReloadCounts(f, workloadPods)
		original := veg.DeepCopy()
		modified := veg.DeepCopy()
		modified.Spec.Observability.Conntrack.Log.Events = []string{apiv1.ObservabilityEventEnd}
		modified.Spec.Observability.Conntrack.Log.RateLimit.RecordsPerSecond = 50
		veg = f.VpcEgressGatewayClient().PatchSync(original, modified)
		validateVpcEgressObservabilityHotReload(f, veg, workloadPods, reloadCounts)
	})
}

func generateSubnetFromDockerNetwork(subnetName string, network *dockernetwork.Inspect, ipv4, ipv6 bool) *apiv1.Subnet {
	ginkgo.GinkgoHelper()

	ginkgo.By("Generating subnet configuration from docker network " + network.Name)
	var cidrV4, cidrV6, gatewayV4, gatewayV6 string
	for _, config := range network.IPAM.Config {
		switch util.CheckProtocol(config.Subnet.String()) {
		case apiv1.ProtocolIPv4:
			if ipv4 {
				cidrV4 = config.Subnet.String()
				gatewayV4 = config.Gateway.String()
			}
		case apiv1.ProtocolIPv6:
			if ipv6 {
				cidrV6 = config.Subnet.String()
				if config.Gateway.IsValid() {
					gatewayV6 = config.Gateway.String()
				} else {
					var err error
					gatewayV6, err = util.FirstIP(cidrV6)
					framework.ExpectNoError(err)
				}
			}
		}
	}

	cidr := make([]string, 0, 2)
	gateway := make([]string, 0, 2)
	if ipv4 {
		cidr = append(cidr, cidrV4)
		gateway = append(gateway, gatewayV4)
	}
	if ipv6 {
		cidr = append(cidr, cidrV6)
		gateway = append(gateway, gatewayV6)
	}

	excludeIPs := make([]string, 0, len(network.Containers)*2)
	for _, container := range network.Containers {
		if container.IPv4Address.IsValid() && ipv4 {
			excludeIPs = append(excludeIPs, container.IPv4Address.Addr().String())
		}
		if container.IPv6Address.IsValid() && ipv6 {
			excludeIPs = append(excludeIPs, container.IPv6Address.Addr().String())
		}
	}

	return framework.MakeSubnet(subnetName, "", strings.Join(cidr, ","), strings.Join(gateway, ","), "", "", excludeIPs, nil, nil)
}

func setNodeLabel(cs clientset.Interface, nodeName, key, value string) {
	ginkgo.GinkgoHelper()

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		node, err := cs.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if node.Labels == nil {
			node.Labels = map[string]string{}
		}
		if value == "" {
			delete(node.Labels, key)
		} else {
			node.Labels[key] = value
		}
		_, err = cs.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
		return err
	})
	framework.ExpectNoError(err, "updating label %s on node %s", key, nodeName)
}

func waitVpcBFDNodes(vpcClient *framework.VpcClient, vpcName string, expected []string) {
	ginkgo.GinkgoHelper()

	framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		vpc := vpcClient.Get(vpcName)
		nodes := slices.Clone(vpc.Status.BFDPort.Nodes)
		expectedNodes := slices.Clone(expected)
		slices.Sort(nodes)
		slices.Sort(expectedNodes)
		if slices.Equal(nodes, expectedNodes) {
			return true, nil
		}
		framework.Logf("VPC %s BFDPort nodes are %v, expected %v", vpcName, vpc.Status.BFDPort.Nodes, expected)
		return false, nil
	}, "VPC "+vpcName+" BFDPort nodes to match")
}

func waitHAChassisCount(groupName string, expected int) {
	ginkgo.GinkgoHelper()

	framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		cmd := fmt.Sprintf("ovn-nbctl --format=csv --data=bare --no-heading --columns=_uuid find HA_Chassis external_ids:group=%s", groupName)
		stdout, _, err := framework.NBExec(cmd)
		if err != nil {
			framework.Logf("failed to query HA_Chassis for group %s: %v", groupName, err)
			return false, nil
		}
		count := countNonEmptyLines(string(stdout))
		if count == expected {
			return true, nil
		}
		framework.Logf("HA_Chassis count for group %s is %d, expected %d: %s", groupName, count, expected, strings.TrimSpace(string(stdout)))
		return false, nil
	}, fmt.Sprintf("HA_Chassis count for group %s to be %d", groupName, expected))
}

func waitHAChassisGroupCount(groupName string, expected int) {
	ginkgo.GinkgoHelper()

	framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		cmd := fmt.Sprintf("ovn-nbctl --format=csv --data=bare --no-heading --columns=ha_chassis find HA_Chassis_Group name=%s", groupName)
		stdout, _, err := framework.NBExec(cmd)
		if err != nil {
			framework.Logf("failed to query HA_Chassis_Group %s: %v", groupName, err)
			return false, nil
		}
		count := countUUIDs(string(stdout))
		if count == expected {
			return true, nil
		}
		framework.Logf("HA_Chassis_Group %s has %d HA chassis refs, expected %d: %s", groupName, count, expected, strings.TrimSpace(string(stdout)))
		return false, nil
	}, fmt.Sprintf("HA_Chassis_Group %s HA chassis refs to be %d", groupName, expected))
}

func waitLRPHAChassisGroup(lrpName, groupName string) {
	ginkgo.GinkgoHelper()

	framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		groupCmd := fmt.Sprintf("ovn-nbctl --format=csv --data=bare --no-heading --columns=_uuid find HA_Chassis_Group name=%s", groupName)
		groupStdout, _, err := framework.NBExec(groupCmd)
		if err != nil {
			framework.Logf("failed to query HA_Chassis_Group %s uuid: %v", groupName, err)
			return false, nil
		}
		groupUUID := strings.TrimSpace(string(groupStdout))
		if groupUUID == "" {
			framework.Logf("HA_Chassis_Group %s does not exist yet", groupName)
			return false, nil
		}

		lrpCmd := fmt.Sprintf("ovn-nbctl --format=csv --data=bare --no-heading --columns=ha_chassis_group find Logical_Router_Port name=%s", lrpName)
		lrpStdout, _, err := framework.NBExec(lrpCmd)
		if err != nil {
			framework.Logf("failed to query Logical_Router_Port %s HA chassis group: %v", lrpName, err)
			return false, nil
		}
		lrpGroupUUID := strings.TrimSpace(string(lrpStdout))
		if lrpGroupUUID == groupUUID {
			return true, nil
		}
		framework.Logf("Logical_Router_Port %s is bound to HA chassis group %q, expected %q", lrpName, lrpGroupUUID, groupUUID)
		return false, nil
	}, fmt.Sprintf("Logical_Router_Port %s to bind HA_Chassis_Group %s", lrpName, groupName))
}

func createVpcEgressGatewayPortGroup(pgName, vegKey string) {
	ginkgo.GinkgoHelper()

	quotedPgName := shellQuote(pgName)
	cmd := fmt.Sprintf(
		"if [ -z \"$(ovn-nbctl --format=csv --data=bare --no-heading --columns=name find Port_Group name=%[1]s)\" ]; then ovn-nbctl pg-add %[1]s; fi; ovn-nbctl set Port_Group %[1]s external_ids:vendor=%[2]s external_ids:vpc-egress-gateway=%[3]s external_ids:af=4",
		quotedPgName,
		shellQuote(util.CniTypeName),
		shellQuote(vegKey),
	)
	_, _, err := framework.NBExec(cmd)
	framework.ExpectNoError(err)
}

func deletePortGroup(pgName string) {
	ginkgo.GinkgoHelper()

	quotedPgName := shellQuote(pgName)
	cmd := fmt.Sprintf("if [ -n \"$(ovn-nbctl --format=csv --data=bare --no-heading --columns=name find Port_Group name=%[1]s)\" ]; then ovn-nbctl pg-del %[1]s; fi", quotedPgName)
	_, _, err := framework.NBExec(cmd)
	framework.ExpectNoError(err)
}

func waitPortGroupExists(pgName string) {
	ginkgo.GinkgoHelper()

	framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		cmd := fmt.Sprintf("ovn-nbctl --format=csv --data=bare --no-heading --columns=name find Port_Group name=%s", shellQuote(pgName))
		stdout, _, err := framework.NBExec(cmd)
		if err != nil {
			framework.Logf("failed to query Port_Group %s: %v", pgName, err)
			return false, nil
		}
		if strings.TrimSpace(string(stdout)) == pgName {
			return true, nil
		}
		framework.Logf("Port_Group %s does not exist yet: %s", pgName, strings.TrimSpace(string(stdout)))
		return false, nil
	}, "Port_Group "+pgName+" to exist")
}

func waitVpcEgressGatewayPolicyNexthops(vegKey string, priority, af int, expected []string) {
	ginkgo.GinkgoHelper()
	want := set.New(expected...)

	framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		cmd := fmt.Sprintf(
			"ovn-nbctl --format=csv --data=bare --no-heading --columns=nexthops find Logical_Router_Policy priority=%d external_ids:vpc-egress-gateway=%s external_ids:af=%d",
			priority,
			shellQuote(vegKey),
			af,
		)
		stdout, _, err := framework.NBExec(cmd)
		if err != nil {
			framework.Logf("failed to query policies for %s: %v", vegKey, err)
			return false, nil
		}

		lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
		if len(lines) != 2 || lines[0] == "" {
			framework.Logf("gateway %s has %d matching policies, expected 2", vegKey, len(lines))
			return false, nil
		}
		for _, line := range lines {
			got := set.New[string]()
			for _, token := range ipTokenRegexp.FindAllString(line, -1) {
				if net.ParseIP(token) != nil {
					got.Insert(token)
				}
			}
			if !want.Equal(got) {
				framework.Logf("gateway %s policy nexthops are %v, expected %v", vegKey, got.UnsortedList(), expected)
				return false, nil
			}
		}
		return true, nil
	}, fmt.Sprintf("gateway %s priority %d IPv%d policy nexthops", vegKey, priority, af))
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func countNonEmptyLines(output string) int {
	count := 0
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func countUUIDs(output string) int {
	return len(uuidRegexp.FindAllString(output, -1))
}

func checkEgressAccess(f *framework.Framework, namespaceName, svrPodName, image, svrPort string, svrIPs, extIPs []string, intIPs map[string][]string, subnetName, nodeName, snatLabelValue string, snat bool) {
	ginkgo.GinkgoHelper()

	podName := "pod-" + framework.RandomSuffix()
	ginkgo.By("Creating client pod " + podName + " within subnet " + subnetName)
	labels := map[string]string{"snat": strconv.FormatBool(snat)}
	if snat {
		labels["snat"] = snatLabelValue
	}
	annotations := map[string]string{util.LogicalSwitchAnnotation: subnetName}
	pod := framework.MakePrivilegedPod(namespaceName, podName, labels, annotations, image, []string{"sleep", "infinity"}, nil)
	pod.Spec.NodeName = nodeName
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Deleting pod " + podName)
		f.PodClient().DeleteSync(podName)
	})
	pod = f.PodClient().CreateSync(pod)

	if !snat {
		// skip egress route check if SNAT is enabled
		// traceroute does not work for pods selected by the selectors
		var hops []string
		if nodeName == "" {
			for ips := range maps.Values(intIPs) {
				hops = append(hops, ips...)
			}
		} else {
			hops = intIPs[nodeName]
		}
		framework.CheckPodEgressRoutes(pod.Namespace, pod.Name, f.HasIPv4(), f.HasIPv6(), 2, hops)
	}

	if !snat {
		podIPv4, podIPv6 := util.SplitIpsByProtocol(util.PodIPs(*pod))
		hopsIPv4, hopsIPv6 := util.SplitIpsByProtocol(extIPs)
		addEcmpRoutes(namespaceName, svrPodName, podIPv4, hopsIPv4)
		addEcmpRoutes(namespaceName, svrPodName, podIPv6, hopsIPv6)
	}

	expectedClientIPs := extIPs
	if !snat {
		expectedClientIPs = util.PodIPs(*pod)
	}
	for _, svrIP := range svrIPs {
		protocol := strings.ToLower(util.CheckProtocol(svrIP))
		ginkgo.By("Checking connection from " + pod.Name + " to " + svrIP + " via " + protocol)
		cmd := fmt.Sprintf("curl -q -s --connect-timeout 2 --max-time 2 %s/clientip", net.JoinHostPort(svrIP, svrPort))
		ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, pod.Namespace, pod.Name))
		var clientIP string
		framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			output, err := e2epodoutput.RunHostCmd(pod.Namespace, pod.Name, cmd)
			if err != nil {
				return false, nil
			}
			clientIP, _, err = net.SplitHostPort(strings.TrimSpace(output))
			return err == nil, nil
		}, fmt.Sprintf("curl %s from pod %s/%s", net.JoinHostPort(svrIP, svrPort), pod.Namespace, pod.Name))
		framework.ExpectContainElement(expectedClientIPs, clientIP)
	}
}

func addEcmpRoutes(namespaceName, podName string, destinations, nextHops []string) {
	ginkgo.GinkgoHelper()

	if len(destinations) == 0 || len(nextHops) == 0 {
		return
	}

	var args string
	if len(nextHops) == 1 {
		args = " via " + nextHops[0]
	} else {
		nexthops := make([]string, len(nextHops))
		for i, ip := range nextHops {
			nexthops[i] = fmt.Sprintf(" nexthop via %s dev net1 weight 1", ip)
		}
		args = strings.Join(nexthops, "")
	}
	for _, dst := range destinations {
		cmd := fmt.Sprintf("ip route add %s%s", dst, args)
		output, err := e2epodoutput.RunHostCmd(namespaceName, podName, cmd)
		framework.ExpectNoError(err, output)
	}
}

func verifyBFDDZeroSessionsRecovery(f *framework.Framework, namespaceName string, pod corev1.Pod) {
	ginkgo.GinkgoHelper()

	const bfddContainer = "bfdd"
	podClient := f.PodClientNS(namespaceName)
	usesSupervisor := bfddUsesSupervisor(f, namespaceName, pod.Name)
	peerIPs := bfddPeerIPs(f, namespaceName, pod.Name)

	ginkgo.By("Waiting for the BFD session in gateway pod " + pod.Name)
	waitForAllBFDSessionsUp(f, namespaceName, pod.Name, usesSupervisor, peerIPs)

	pod = *podClient.GetPod(pod.Name)
	initialRestartCount := containerRestartCount(pod, bfddContainer)

	ginkgo.By("Blocking peers and removing BFD sessions from gateway pod " + pod.Name)
	injectBFDDZeroSessions(f, namespaceName, pod.Name)
	peersRestored := false
	restorePeers := func() {
		if peersRestored {
			return
		}
		restoreBFDDPeers(f, namespaceName, pod.Name)
		peersRestored = true
	}
	ginkgo.DeferCleanup(restorePeers)

	ginkgo.By("Validating the gateway pod reports zero BFD sessions")
	framework.WaitUntil(200*time.Millisecond, 10*time.Second, func(_ context.Context) (bool, error) {
		stdout, _, err := framework.ExecCommandInContainer(f, namespaceName, pod.Name, bfddContainer, "bfdd-control", "status")
		return err == nil && strings.Contains(stdout, "There are 0 sessions:"), nil
	}, "gateway pod to report zero BFD sessions")

	if usesSupervisor {
		waitForSupervisorBFDDRecovery(f, namespaceName, pod.Name, initialRestartCount)
	} else {
		waitForLegacyBFDDRestart(f, namespaceName, pod.Name, initialRestartCount)
	}

	ginkgo.By("Validating every expected BFD session recovers")
	waitForAllBFDSessionsUp(f, namespaceName, pod.Name, usesSupervisor, peerIPs)
	if usesSupervisor {
		gomega.Expect(containerRestartCount(*podClient.GetPod(pod.Name), bfddContainer)).To(gomega.Equal(initialRestartCount))
	}
	restorePeers()
}

func bfddUsesSupervisor(f *framework.Framework, namespaceName, podName string) bool {
	ginkgo.GinkgoHelper()
	const detectSupervisor = `tr '\0' ' ' </proc/1/cmdline; printf '\n'; if [ -S /var/run/kube-ovn/bfdd-supervisor/control.sock ]; then printf socket; else printf missing; fi`
	stdout, stderr, err := framework.ExecShellInContainer(f, namespaceName, podName, "bfdd", detectSupervisor)
	framework.ExpectNoError(err, "failed to detect BFD implementation in pod %s/%s: %s", namespaceName, podName, stderr)
	return bfddSupervisorRuntimeDetected(stdout)
}

func bfddSupervisorRuntimeDetected(output string) bool {
	parts := strings.SplitN(strings.TrimSpace(output), "\n", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) != "socket" {
		return false
	}
	command := strings.Fields(parts[0])
	return len(command) >= 2 && command[0] == "/kube-ovn/kube-ovn-bfdd-supervisor" && command[1] == "run"
}

func bfddPeerIPs(f *framework.Framework, namespaceName, podName string) []string {
	ginkgo.GinkgoHelper()
	stdout, stderr, err := framework.ExecCommandInContainer(f, namespaceName, podName, "bfdd", "printenv", "BFD_PEER_IPS")
	framework.ExpectNoError(err, "failed to get BFD peers from pod %s/%s: %s", namespaceName, podName, stderr)
	peers := strings.FieldsFunc(strings.TrimSpace(stdout), func(r rune) bool { return r == ',' })
	gomega.Expect(peers).NotTo(gomega.BeEmpty(), "BFD_PEER_IPS must contain at least one peer")
	return peers
}

func injectBFDDZeroSessions(f *framework.Framework, namespaceName, podName string) {
	ginkgo.GinkgoHelper()
	const injectZeroSessions = `bash -c '
set -euo pipefail
IFS=, read -ra peers <<< "${BFD_PEER_IPS:-}"
for peer in "${peers[@]}"; do
  [[ -z "${peer}" ]] || bfdd-control block "${peer}" >/dev/null
done
bfdd-control session all kill >/dev/null
'`
	_, stderr, err := framework.ExecShellInContainer(f, namespaceName, podName, "bfdd", injectZeroSessions)
	framework.ExpectNoError(err, "failed to inject zero BFD sessions in pod %s/%s: %s", namespaceName, podName, stderr)
}

func restoreBFDDPeers(f *framework.Framework, namespaceName, podName string) {
	ginkgo.GinkgoHelper()
	const restorePeers = `bash -c '
set -euo pipefail
IFS=, read -ra peers <<< "${BFD_PEER_IPS:-}"
for peer in "${peers[@]}"; do
  [[ -z "${peer}" ]] || bfdd-control allow "${peer}" >/dev/null
done
'`
	_, stderr, err := framework.ExecShellInContainer(f, namespaceName, podName, "bfdd", restorePeers)
	framework.ExpectNoError(err, "failed to restore BFD peers in pod %s/%s: %s", namespaceName, podName, stderr)
}

type bfddSupervisorE2EStatus struct {
	Live     bool
	Sessions []bfddSupervisorE2ESessionStatus
}

type bfddSupervisorE2ESessionStatus struct {
	Attempts   int
	LastAction string
}

func waitForSupervisorBFDDRecovery(f *framework.Framework, namespaceName, podName string, initialRestartCount int32) {
	ginkgo.GinkgoHelper()
	podClient := f.PodClientNS(namespaceName)
	ginkgo.By("Waiting for the supervisor to replay configuration without restarting the container")
	framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		gomega.Expect(containerRestartCount(*podClient.GetPod(podName), "bfdd")).To(gomega.Equal(initialRestartCount))
		stdout, _, err := framework.ExecCommandInContainer(
			f, namespaceName, podName, "bfdd", "/kube-ovn/kube-ovn-bfdd-supervisor", "status",
		)
		if err != nil {
			return false, nil
		}
		var status bfddSupervisorE2EStatus
		if err := json.Unmarshal([]byte(stdout), &status); err != nil {
			framework.Logf("failed to decode BFD supervisor status from pod %s/%s: %v", namespaceName, podName, err)
			return false, nil
		}
		gomega.Expect(status.Live).To(gomega.BeTrue(), "BFD supervisor must remain live during session recovery")
		return slices.ContainsFunc(status.Sessions, func(session bfddSupervisorE2ESessionStatus) bool {
			return session.Attempts > 0 && session.LastAction == "ReplayConfiguration"
		}), nil
	}, "BFD supervisor to replay configuration")
}

func waitForLegacyBFDDRestart(f *framework.Framework, namespaceName, podName string, initialRestartCount int32) {
	ginkgo.GinkgoHelper()
	podClient := f.PodClientNS(namespaceName)
	ginkgo.By("Waiting for the legacy bfdd liveness probe to restart the container")
	framework.WaitUntil(time.Second, 45*time.Second, func(_ context.Context) (bool, error) {
		return containerRestartCount(*podClient.GetPod(podName), "bfdd") > initialRestartCount, nil
	}, "bfdd container to restart after consecutive zero-session health check failures")
}

func waitForAllBFDSessionsUp(f *framework.Framework, namespaceName, podName string, usesSupervisor bool, peerIPs []string) {
	ginkgo.GinkgoHelper()
	framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		if usesSupervisor {
			_, _, err := framework.ExecCommandInContainer(
				f, namespaceName, podName, "bfdd", "/kube-ovn/kube-ovn-bfdd-supervisor", "ready",
			)
			return err == nil, nil
		}
		stdout, _, err := framework.ExecCommandInContainer(f, namespaceName, podName, "bfdd", "bfdd-control", "status")
		return err == nil && bfddStatusHasAllExpectedPeersUp(stdout, peerIPs), nil
	}, "all expected BFD sessions to be Up")
}

func bfddStatusHasAllExpectedPeersUp(output string, peerIPs []string) bool {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var reported int
	if len(lines) == 0 {
		return false
	}
	if _, err := fmt.Sscanf(lines[0], "There are %d sessions:", &reported); err != nil || reported == 0 {
		return false
	}
	observed := make(map[string]struct{}, reported)
	sessionCount := 0
	for _, line := range lines[1:] {
		var remote, state string
		for field := range strings.FieldsSeq(line) {
			if value, ok := strings.CutPrefix(field, "remote="); ok {
				remote = value
			} else if value, ok := strings.CutPrefix(field, "state="); ok {
				state = value
			}
		}
		if remote == "" && state == "" {
			continue
		}
		address := net.ParseIP(remote)
		if address == nil || state != "Up" {
			return false
		}
		observed[address.String()] = struct{}{}
		sessionCount++
	}
	if sessionCount != reported {
		return false
	}
	for _, peer := range peerIPs {
		address := net.ParseIP(peer)
		if address == nil {
			return false
		}
		if _, exists := observed[address.String()]; !exists {
			return false
		}
	}
	return true
}

func TestBFDDStatusHasAllExpectedPeersUp(t *testing.T) {
	const dualUp = `There are 2 sessions:
Session 1
 id=1 local=10.16.0.2 (p) remote=10.255.255.255 state=Up
Session 2
 id=2 local=fd00::2 (p) remote=fd00::ffff state=Up`
	tests := []struct {
		name   string
		status string
		peers  []string
		want   bool
	}{
		{name: "all expected peers up", status: dualUp, peers: []string{"10.255.255.255", "fd00::ffff"}, want: true},
		{name: "expected peer missing", status: dualUp, peers: []string{"10.255.255.255", "fd00::1"}},
		{name: "session down", status: strings.Replace(dualUp, "state=Up", "state=Down", 1), peers: []string{"10.255.255.255", "fd00::ffff"}},
		{name: "reported count mismatch", status: strings.Replace(dualUp, "There are 2", "There are 3", 1), peers: []string{"10.255.255.255", "fd00::ffff"}},
		{name: "invalid expected peer", status: dualUp, peers: []string{"not-an-ip"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gomega.NewWithT(t).Expect(bfddStatusHasAllExpectedPeersUp(tt.status, tt.peers)).To(gomega.Equal(tt.want))
		})
	}
}

func TestBFDDUsesSupervisorRuntime(t *testing.T) {
	tests := []struct {
		name    string
		runtime string
		want    bool
	}{
		{name: "supervisor PID 1 with control socket", runtime: "/kube-ovn/kube-ovn-bfdd-supervisor run\nsocket", want: true},
		{name: "legacy PID 1 in new image", runtime: "bash /kube-ovn/start-bfdd.sh\nsocket"},
		{name: "supervisor without control socket", runtime: "/kube-ovn/kube-ovn-bfdd-supervisor run\nmissing"},
		{name: "supervisor probe subcommand", runtime: "/kube-ovn/kube-ovn-bfdd-supervisor live\nsocket"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gomega.NewWithT(t).Expect(bfddSupervisorRuntimeDetected(tt.runtime)).To(gomega.Equal(tt.want))
		})
	}
}

func containerRestartCount(pod corev1.Pod, containerName string) int32 {
	ginkgo.GinkgoHelper()

	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == containerName {
			return status.RestartCount
		}
	}
	framework.Failf("container %s not found in pod %s/%s", containerName, pod.Namespace, pod.Name)
	return 0
}

func validateVegTestSNATAccess(f *framework.Framework, veg *apiv1.VpcEgressGateway, nadName, snatSubnetName, snatLabelValue string, workloadPods []corev1.Pod, intIPs map[string][]string) {
	ginkgo.GinkgoHelper()

	namespaceName := f.Namespace.Name
	attachmentNetworkName := fmt.Sprintf("%s/%s", namespaceName, nadName)
	annotations := map[string]string{nadv1.NetworkAttachmentAnnot: attachmentNetworkName}
	port := strconv.Itoa(8000 + rand.IntN(1000))
	svrPodName := "svr-" + framework.RandomSuffix()
	args := []string{"netexec", "--http-port", port}
	podClient := f.PodClient()
	svrPod := framework.MakePrivilegedPod(namespaceName, svrPodName, nil, annotations, framework.AgnhostImage, nil, args)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Deleting pod " + svrPodName)
		podClient.DeleteSync(svrPodName)
	})
	svrPod = podClient.CreateSync(svrPod)
	svrIPs, err := util.PodAttachmentIPs(svrPod, attachmentNetworkName)
	framework.ExpectNoError(err)

	image := workloadPods[0].Spec.Containers[0].Image
	extIPs := make([]string, 0, len(veg.Status.ExternalIPs)*2)
	for _, ips := range veg.Status.ExternalIPs {
		extIPs = append(extIPs, strings.Split(ips, ",")...)
	}
	checkEgressAccess(f, namespaceName, svrPodName, image, port, svrIPs, extIPs, intIPs, snatSubnetName, "", snatLabelValue, true)
}

func validateVpcEgressObservability(f *framework.Framework, veg *apiv1.VpcEgressGateway, workloadPods []corev1.Pod, interfaceBytes float64) {
	ginkgo.GinkgoHelper()
	const observerContainer = "observability"
	resourceName := util.NormalizeLabelValue("vpc-egress-" + strings.ReplaceAll(veg.Spec.Prefix+veg.Name+"-observability", ".", "-"))
	configMap, err := f.ClientSet.CoreV1().ConfigMaps(veg.Namespace).Get(context.Background(), resourceName, metav1.GetOptions{})
	framework.ExpectNoError(err)
	framework.ExpectContainSubstring(configMap.Data["config.json"], `"interfaceMetrics":{"enabled":true}`)
	service, err := f.ClientSet.CoreV1().Services(veg.Namespace).Get(context.Background(), resourceName, metav1.GetOptions{})
	framework.ExpectNoError(err)
	framework.ExpectEqual(service.Spec.ClusterIP, corev1.ClusterIPNone)
	framework.ExpectTrue(service.Spec.PublishNotReadyAddresses)
	framework.ExpectEqual(service.Spec.Ports[0].TargetPort.IntVal, int32(10666))

	for _, pod := range workloadPods {
		found := false
		for _, container := range pod.Spec.InitContainers {
			if container.Name != observerContainer {
				continue
			}
			found = true
			framework.ExpectNotNil(container.RestartPolicy)
			framework.ExpectEqual(*container.RestartPolicy, corev1.ContainerRestartPolicyAlways)
			framework.ExpectNotNil(container.SecurityContext)
			framework.ExpectTrue(*container.SecurityContext.RunAsNonRoot)
			framework.ExpectEqual(*container.SecurityContext.RunAsUser, int64(65534))
			framework.ExpectEqual(*container.SecurityContext.RunAsGroup, int64(65534))
			framework.ExpectTrue(*container.SecurityContext.AllowPrivilegeEscalation)
			framework.ExpectTrue(*container.SecurityContext.ReadOnlyRootFilesystem)
			framework.ExpectConsistOf(container.SecurityContext.Capabilities.Drop, corev1.Capability("ALL"))
			framework.ExpectConsistOf(container.SecurityContext.Capabilities.Add, corev1.Capability("NET_ADMIN"))
			framework.ExpectNil(container.LivenessProbe.HTTPGet)
			framework.ExpectEqual(container.LivenessProbe.Exec.Command, []string{
				"/bin/sh", "-ec",
				"if [ ! -x /kube-ovn/vpc-egress-gateway-observer ]; then exit 0; fi; exec /kube-ovn/vpc-egress-gateway-observer --health-check",
			})
		}
		framework.ExpectTrue(found, "gateway pod %s/%s should have the observability restartable init container", pod.Namespace, pod.Name)
		metrics := waitVpcEgressObserverMetrics(f, pod.Namespace, pod.Name)
		framework.ExpectContainSubstring(metrics, "kube_ovn_vpc_egress_gateway_interface_rx_bytes_total")
		framework.ExpectContainSubstring(metrics, "kube_ovn_vpc_egress_gateway_interface_tx_packets_total")
		framework.ExpectContainSubstring(metrics, `namespace="`+veg.Namespace+`"`)
		framework.ExpectContainSubstring(metrics, `name="`+veg.Name+`"`)
	}
	waitVpcEgressObserverInterfaceCounterGrowth(f, workloadPods, interfaceBytes)
	validateVpcEgressObserverCollectorFailureIsolation(f, workloadPods[0])
	waitVpcEgressObserverConntrackMetrics(f, workloadPods)
	waitVpcEgressObserverFlowLog(f, veg, workloadPods)

	condition := veg.Status.Conditions.GetCondition(apiv1.ObservabilityConfigured)
	framework.ExpectNotNil(condition)
	framework.ExpectEqual(condition.Status, corev1.ConditionTrue)
	serviceMonitorCondition := veg.Status.Conditions.GetCondition(apiv1.ServiceMonitorReady)
	framework.ExpectNotNil(serviceMonitorCondition)
	if serviceMonitorCondition.Status == corev1.ConditionTrue {
		config, err := k8sframework.LoadConfig()
		framework.ExpectNoError(err)
		dynamicClient, err := dynamic.NewForConfig(config)
		framework.ExpectNoError(err)
		serviceMonitor, err := dynamicClient.Resource(schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "servicemonitors"}).Namespace(veg.Namespace).Get(context.Background(), resourceName, metav1.GetOptions{})
		framework.ExpectNoError(err)
		framework.ExpectEqual(serviceMonitor.GetLabels()["e2e"], "vpc-egress-observability")
	} else {
		framework.ExpectEqual(serviceMonitorCondition.Reason, "ServiceMonitorCRDNotInstalled")
	}
}

func validateVpcEgressObservabilityHotReload(f *framework.Framework, veg *apiv1.VpcEgressGateway, originalPods []corev1.Pod, initialReloads map[string]float64) {
	ginkgo.GinkgoHelper()
	resourceName := util.NormalizeLabelValue("vpc-egress-" + strings.ReplaceAll(veg.Spec.Prefix+veg.Name+"-observability", ".", "-"))
	var configResourceVersion string
	err := wait.PollUntilContextTimeout(context.Background(), time.Second, 30*time.Second, false, func(ctx context.Context) (bool, error) {
		configMap, err := f.ClientSet.CoreV1().ConfigMaps(veg.Namespace).Get(ctx, resourceName, metav1.GetOptions{})
		if err != nil || !strings.Contains(configMap.Data["config.json"], `"recordsPerSecond":50`) {
			return false, nil
		}
		configResourceVersion = configMap.ResourceVersion
		return true, nil
	})
	framework.ExpectNoError(err)

	deploy := f.DeploymentClient().Get(veg.Status.Workload.Name)
	currentPods, err := f.DeploymentClient().GetPods(deploy)
	framework.ExpectNoError(err)
	framework.ExpectHaveLen(currentPods.Items, len(originalPods))
	originalUIDs := make([]string, 0, len(originalPods))
	currentUIDs := make([]string, 0, len(currentPods.Items))
	for _, pod := range originalPods {
		originalUIDs = append(originalUIDs, string(pod.UID))
	}
	for _, pod := range currentPods.Items {
		currentUIDs = append(currentUIDs, string(pod.UID))
	}
	framework.ExpectConsistOf(currentUIDs, originalUIDs)
	framework.ExpectNoError(triggerVpcEgressObserverConfigRefresh(f, currentPods.Items, configResourceVersion))
	framework.ExpectNoError(waitVpcEgressObserverConfigProjection(f, currentPods.Items), "updated observer config to be projected into all original pods")
	lastReloads := make(map[string]float64, len(currentPods.Items))
	err = wait.PollUntilContextTimeout(context.Background(), time.Second, time.Minute, false, func(context.Context) (bool, error) {
		for _, currentPod := range currentPods.Items {
			initial, ok := initialReloads[currentPod.Name]
			if !ok {
				return false, fmt.Errorf("missing initial config reload count for pod %s", currentPod.Name)
			}
			metrics, err := scrapeVpcEgressObserverMetrics(f, currentPod.Namespace, currentPod.Name)
			if err != nil {
				return false, nil
			}
			value, found, err := observerMetricValue(metrics, "kube_ovn_vpc_egress_gateway_observability_config_reload_total", `result="success"`)
			if err != nil {
				return false, err
			}
			lastReloads[currentPod.Name] = value
			if !found || value <= initial {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		framework.Logf("observer config reload counts before patch: %v; last observed: %v", initialReloads, lastReloads)
		for _, pod := range currentPods.Items {
			stdout, stderr, execErr := framework.ExecCommandInContainer(f, pod.Namespace, pod.Name, "observability", "cat", "/etc/kube-ovn-observer/config.json")
			framework.Logf("mounted observer config from pod %s (error=%v, stderr=%q):\n%s", pod.Name, execErr, stderr, stdout)
			logVpcEgressObserverDiagnostics(f, pod)
		}
	}
	framework.ExpectNoError(err, "all observability sidecars to apply the hot-reloaded configuration")

	pod := currentPods.Items[0]
	restarts := observerRestartCount(pod)
	_, stderr, err := framework.ExecCommandInContainer(f, pod.Namespace, pod.Name, "observability", "/bin/sh", "-c", "kill 1")
	framework.ExpectNoError(err, "terminating observability sidecar; stderr: "+stderr)
	err = wait.PollUntilContextTimeout(context.Background(), time.Second, time.Minute, false, func(ctx context.Context) (bool, error) {
		updated, err := f.ClientSet.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		return err == nil && observerRestartCount(*updated) > restarts, nil
	})
	framework.ExpectNoError(err, "observability sidecar to restart")
	_ = waitVpcEgressObserverMetrics(f, pod.Namespace, pod.Name)
	updatedVeg := f.VpcEgressGatewayClient().Get(veg.Name)
	framework.ExpectTrue(updatedVeg.Ready())
}

func triggerVpcEgressObserverConfigRefresh(f *framework.Framework, pods []corev1.Pod, resourceVersion string) error {
	const annotation = "e2e.kube-ovn.io/observability-config-version"
	for _, pod := range pods {
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			current, err := f.ClientSet.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if current.Annotations == nil {
				current.Annotations = make(map[string]string, 1)
			}
			// Updating Pod metadata asks kubelet to refresh projected ConfigMaps immediately without replacing the Pod.
			current.Annotations[annotation] = resourceVersion
			_, err = f.ClientSet.CoreV1().Pods(pod.Namespace).Update(context.Background(), current, metav1.UpdateOptions{})
			return err
		}); err != nil {
			return fmt.Errorf("trigger observer config refresh for pod %s/%s: %w", pod.Namespace, pod.Name, err)
		}
	}
	return nil
}

func waitVpcEgressObserverConfigProjection(f *framework.Framework, pods []corev1.Pod) error {
	lastConfigs := make(map[string]string, len(pods))
	err := wait.PollUntilContextTimeout(context.Background(), time.Second, 3*time.Minute, false, func(context.Context) (bool, error) {
		for _, pod := range pods {
			stdout, _, err := framework.ExecCommandInContainer(f, pod.Namespace, pod.Name, "observability", "cat", "/etc/kube-ovn-observer/config.json")
			if err != nil {
				return false, nil
			}
			lastConfigs[pod.Name] = stdout
			if !strings.Contains(stdout, `"recordsPerSecond":50`) {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		framework.Logf("last projected observer configs: %v", lastConfigs)
	}
	return err
}

func waitVpcEgressObserverReloadCounts(f *framework.Framework, pods []corev1.Pod) map[string]float64 {
	ginkgo.GinkgoHelper()
	counts := make(map[string]float64, len(pods))
	err := wait.PollUntilContextTimeout(context.Background(), time.Second, time.Minute, false, func(context.Context) (bool, error) {
		clear(counts)
		for _, pod := range pods {
			metrics, err := scrapeVpcEgressObserverMetrics(f, pod.Namespace, pod.Name)
			if err != nil {
				return false, nil
			}
			value, found, err := observerMetricValue(metrics, "kube_ovn_vpc_egress_gateway_observability_config_reload_total", `result="success"`)
			if err != nil {
				return false, err
			}
			if !found {
				return false, nil
			}
			counts[pod.Name] = value
		}
		return true, nil
	})
	framework.ExpectNoError(err, "config reload metrics to become ready on all observability sidecars")
	return counts
}

func validateVpcEgressObserverCollectorFailureIsolation(f *framework.Framework, pod corev1.Pod) {
	ginkgo.GinkgoHelper()
	const script = `
set -eu
observer_pid=
cleanup() {
  if [ -n "$observer_pid" ]; then
    kill "$observer_pid" 2>/dev/null || true
    wait "$observer_pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT
/kube-ovn/vpc-egress-gateway-observer --config /etc/kube-ovn-observer/config.json --network-status /does-not-exist --listen-address 127.0.0.1:10667 >/dev/null 2>/dev/null &
observer_pid=$!
attempt=0
while [ "$attempt" -lt 30 ]; do
  metrics="$(curl -fsS http://127.0.0.1:10667/metrics 2>/dev/null || true)"
  if printf '%s\n' "$metrics" | grep -q '^kube_ovn_vpc_egress_gateway_observability_collector_up{.*collector="interface".*} 0$' &&
     printf '%s\n' "$metrics" | grep -q '^kube_ovn_vpc_egress_gateway_observability_collector_up{.*collector="conntrack".*} 1$'; then
    printf '%s\n' "$metrics"
    exit 0
  fi
  attempt=$((attempt + 1))
  sleep 1
done
exit 1
`
	stdout, stderr, err := framework.ExecCommandInContainer(f, pod.Namespace, pod.Name, "observability", "/bin/sh", "-ec", script)
	framework.ExpectNoError(err, "validating observer collector failure isolation; stderr: "+stderr)
	interfaceUp, found, err := observerMetricValue(stdout, "kube_ovn_vpc_egress_gateway_observability_collector_up", `collector="interface"`)
	framework.ExpectNoError(err)
	framework.ExpectTrue(found)
	framework.ExpectEqual(interfaceUp, float64(0))
	conntrackUp, found, err := observerMetricValue(stdout, "kube_ovn_vpc_egress_gateway_observability_collector_up", `collector="conntrack"`)
	framework.ExpectNoError(err)
	framework.ExpectTrue(found)
	framework.ExpectEqual(conntrackUp, float64(1))
}

func observerMetricValue(metrics, name string, requiredLabels ...string) (float64, bool, error) {
	values, err := observerMetricValues(metrics, name, requiredLabels...)
	if err != nil {
		return 0, false, err
	}
	if len(values) == 0 {
		return 0, false, nil
	}
	return values[0], true, nil
}

func observerMetricSum(metrics, name string, requiredLabels ...string) (float64, bool, error) {
	values, err := observerMetricValues(metrics, name, requiredLabels...)
	if err != nil {
		return 0, false, err
	}
	var total float64
	for _, value := range values {
		total += value
	}
	return total, len(values) != 0, nil
}

func observerMetricValues(metrics, name string, requiredLabels ...string) ([]float64, error) {
	var values []float64
	for line := range strings.SplitSeq(metrics, "\n") {
		if !strings.HasPrefix(line, name+"{") {
			continue
		}
		matched := true
		for _, label := range requiredLabels {
			if !strings.Contains(line, label) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("parse Prometheus sample %q: expected metric and value", line)
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return nil, fmt.Errorf("parse Prometheus sample %q: %w", line, err)
		}
		values = append(values, value)
	}
	return values, nil
}

func waitVpcEgressObserverInterfaceBytes(f *framework.Framework, pods []corev1.Pod) float64 {
	ginkgo.GinkgoHelper()
	var total float64
	err := wait.PollUntilContextTimeout(context.Background(), time.Second, time.Minute, false, func(context.Context) (bool, error) {
		var ok bool
		var err error
		total, ok, err = vpcEgressObserverInterfaceBytes(f, pods)
		return ok, err
	})
	framework.ExpectNoError(err, "external interface metrics to become ready on all gateway replicas")
	return total
}

func waitVpcEgressObserverInterfaceCounterGrowth(f *framework.Framework, pods []corev1.Pod, baseline float64) {
	ginkgo.GinkgoHelper()
	err := wait.PollUntilContextTimeout(context.Background(), time.Second, time.Minute, false, func(context.Context) (bool, error) {
		current, ok, err := vpcEgressObserverInterfaceBytes(f, pods)
		return ok && current > baseline, err
	})
	framework.ExpectNoError(err, "aggregate external interface byte counters to grow after SNAT traffic")
}

func vpcEgressObserverInterfaceBytes(f *framework.Framework, pods []corev1.Pod) (float64, bool, error) {
	var total float64
	for _, pod := range pods {
		metrics, err := scrapeVpcEgressObserverMetrics(f, pod.Namespace, pod.Name)
		if err != nil {
			return 0, false, nil
		}
		for _, name := range []string{
			"kube_ovn_vpc_egress_gateway_interface_rx_bytes_total",
			"kube_ovn_vpc_egress_gateway_interface_tx_bytes_total",
		} {
			value, found, err := observerMetricSum(metrics, name, `type="external"`)
			if err != nil {
				return 0, false, err
			}
			if !found {
				return 0, false, nil
			}
			total += value
		}
	}
	return total, true, nil
}

func waitVpcEgressObserverMetrics(f *framework.Framework, namespace, pod string) string {
	ginkgo.GinkgoHelper()
	var metrics string
	err := wait.PollUntilContextTimeout(context.Background(), time.Second, time.Minute, false, func(context.Context) (bool, error) {
		stdout, err := scrapeVpcEgressObserverMetrics(f, namespace, pod)
		if err != nil {
			return false, nil
		}
		metrics = stdout
		return strings.Contains(metrics, "kube_ovn_vpc_egress_gateway_observability_collector_up"), nil
	})
	framework.ExpectNoError(err, "observer metrics endpoint to become ready")
	return metrics
}

func waitVpcEgressObserverConntrackMetrics(f *framework.Framework, pods []corev1.Pod) {
	ginkgo.GinkgoHelper()
	lastMetrics := make(map[string]string, len(pods))
	err := wait.PollUntilContextTimeout(context.Background(), time.Second, time.Minute, false, func(context.Context) (bool, error) {
		for _, pod := range pods {
			metrics, err := scrapeVpcEgressObserverMetrics(f, pod.Namespace, pod.Name)
			if err != nil {
				continue
			}
			lastMetrics[pod.Name] = metrics
			if strings.Contains(metrics, "kube_ovn_vpc_egress_gateway_conntrack_nat_flows_active") {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		for _, pod := range pods {
			framework.Logf("last observer metrics from pod %s:\n%s", pod.Name, lastMetrics[pod.Name])
			logVpcEgressObserverDiagnostics(f, pod)
		}
	}
	framework.ExpectNoError(err, "at least one gateway replica to observe the generated SNAT flow")
}

func logVpcEgressObserverDiagnostics(f *framework.Framework, pod corev1.Pod) {
	ginkgo.GinkgoHelper()
	const observerContainer = "observability"
	tailLines := int64(100)
	logs, err := f.ClientSet.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: observerContainer, TailLines: &tailLines}).DoRaw(context.Background())
	if err != nil {
		framework.Logf("failed to read observer logs from pod %s: %v", pod.Name, err)
	} else {
		framework.Logf("observer logs from pod %s:\n%s", pod.Name, logs)
	}
	stdout, stderr, err := framework.ExecCommandInContainer(f, pod.Namespace, pod.Name, observerContainer, "/bin/sh", "-c", "id; grep -E '^(Cap|NoNewPrivs)' /proc/1/status")
	framework.Logf("observer process status from pod %s (error=%v, stderr=%q):\n%s", pod.Name, err, stderr, stdout)
}

func scrapeVpcEgressObserverMetrics(f *framework.Framework, namespace, pod string) (string, error) {
	stdout, _, err := framework.ExecCommandInContainer(f, namespace, pod, "observability", "/bin/sh", "-c", "curl -fsS http://127.0.0.1:10666/metrics")
	return stdout, err
}

type vpcEgressObserverFlowKey struct {
	pod  string
	zone uint16
	id   uint32
}

type vpcEgressObserverFlowTuple struct {
	SourceIP        string  `json:"sourceIP"`
	SourcePort      *uint16 `json:"sourcePort"`
	DestinationIP   string  `json:"destinationIP"`
	DestinationPort *uint16 `json:"destinationPort"`
}

type vpcEgressObserverFlowRecord struct {
	SchemaVersion string                      `json:"schemaVersion"`
	Timestamp     *time.Time                  `json:"timestamp"`
	Event         string                      `json:"event"`
	ConntrackID   *uint32                     `json:"conntrackID"`
	Zone          *uint16                     `json:"zone"`
	Namespace     string                      `json:"namespace"`
	Name          string                      `json:"name"`
	Pod           string                      `json:"pod"`
	Node          string                      `json:"node"`
	AddressFamily string                      `json:"addressFamily"`
	Protocol      string                      `json:"protocol"`
	ProtocolNum   *uint8                      `json:"protocolNumber"`
	NatType       []string                    `json:"natType"`
	Original      *vpcEgressObserverFlowTuple `json:"original"`
	Translated    *vpcEgressObserverFlowTuple `json:"translated"`
}

type vpcEgressObserverFlowObservation struct {
	events set.Set[string]
	start  *vpcEgressObserverFlowRecord
}

func waitVpcEgressObserverFlowLog(f *framework.Framework, veg *apiv1.VpcEgressGateway, pods []corev1.Pod) {
	ginkgo.GinkgoHelper()

	var selectedKey vpcEgressObserverFlowKey
	err := wait.PollUntilContextTimeout(context.Background(), time.Second, time.Minute, false, func(ctx context.Context) (bool, error) {
		observations := vpcEgressObserverFlowObservations(ctx, f, veg, pods)
		for key, observation := range observations {
			if observation.events.HasAll(apiv1.ObservabilityEventStart, apiv1.ObservabilityEventEnd) {
				selectedKey = key
				return true, nil
			}
		}
		for key, observation := range observations {
			if observation.start == nil || observation.events.Has(apiv1.ObservabilityEventEnd) {
				continue
			}
			stdout, stderr, err := deleteVpcEgressObserverConntrackFlow(f, veg.Namespace, key.pod, *observation.start)
			if err != nil {
				framework.Logf("failed to delete conntrack flow %d in zone %d from pod %s: %v, stdout: %s, stderr: %s", key.id, key.zone, key.pod, err, stdout, stderr)
				continue
			}
			selectedKey = key
			return true, nil
		}
		return false, nil
	})
	framework.ExpectNoError(err, "observer to emit a schema-valid start record for an active SNAT flow")

	err = wait.PollUntilContextTimeout(context.Background(), time.Second, time.Minute, false, func(ctx context.Context) (bool, error) {
		observations := vpcEgressObserverFlowObservations(ctx, f, veg, pods)
		observation := observations[selectedKey]
		return observation != nil && observation.events.HasAll(apiv1.ObservabilityEventStart, apiv1.ObservabilityEventEnd), nil
	})
	if err != nil {
		for _, pod := range pods {
			logVpcEgressObserverDiagnostics(f, pod)
		}
	}
	framework.ExpectNoError(err, "observer to emit schema-valid start and end records for the selected SNAT flow")
}

func vpcEgressObserverFlowObservations(ctx context.Context, f *framework.Framework, veg *apiv1.VpcEgressGateway, pods []corev1.Pod) map[vpcEgressObserverFlowKey]*vpcEgressObserverFlowObservation {
	observations := map[vpcEgressObserverFlowKey]*vpcEgressObserverFlowObservation{}
	for _, pod := range pods {
		tailLines := int64(500)
		data, err := f.ClientSet.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: "observability", TailLines: &tailLines}).DoRaw(ctx)
		if err != nil {
			framework.Logf("failed to read observer flow logs from pod %s/%s: %v", pod.Namespace, pod.Name, err)
			continue
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			var record vpcEgressObserverFlowRecord
			if json.Unmarshal([]byte(line), &record) != nil || !validVpcEgressObserverFlowRecord(record, veg, pod.Name) {
				continue
			}
			key := vpcEgressObserverFlowKey{pod: pod.Name, zone: *record.Zone, id: *record.ConntrackID}
			if observations[key] == nil {
				observations[key] = &vpcEgressObserverFlowObservation{events: set.New[string]()}
			}
			observations[key].events.Insert(record.Event)
			if record.Event == apiv1.ObservabilityEventStart {
				observations[key].start = &record
			}
		}
	}
	return observations
}

func validVpcEgressObserverFlowRecord(record vpcEgressObserverFlowRecord, veg *apiv1.VpcEgressGateway, podName string) bool {
	return record.SchemaVersion == "v1" && record.Timestamp != nil && !record.Timestamp.IsZero() && record.ConntrackID != nil && *record.ConntrackID != 0 && record.Zone != nil &&
		record.Namespace == veg.Namespace && record.Name == veg.Name && record.Pod == podName && record.Node != "" && record.AddressFamily != "" && record.Protocol != "" && record.ProtocolNum != nil && *record.ProtocolNum != 0 &&
		record.Original != nil && record.Translated != nil && net.ParseIP(record.Original.SourceIP) != nil && net.ParseIP(record.Original.DestinationIP) != nil &&
		record.Original.SourcePort != nil && *record.Original.SourcePort != 0 && record.Original.DestinationPort != nil && *record.Original.DestinationPort != 0 &&
		net.ParseIP(record.Translated.SourceIP) != nil && net.ParseIP(record.Translated.DestinationIP) != nil && record.Translated.SourcePort != nil && *record.Translated.SourcePort != 0 &&
		record.Translated.DestinationPort != nil && *record.Translated.DestinationPort != 0 && slices.Contains(record.NatType, apiv1.ObservabilityNatTypeSNAT)
}

func deleteVpcEgressObserverConntrackFlow(f *framework.Framework, namespace, pod string, record vpcEgressObserverFlowRecord) (string, string, error) {
	return framework.ExecCommandInContainer(
		f, namespace, pod, "observability", "conntrack", "-D",
		"-w", strconv.FormatUint(uint64(*record.Zone), 10),
		"-p", record.Protocol,
		"-s", record.Original.SourceIP,
		"-d", record.Original.DestinationIP,
		"--sport", strconv.FormatUint(uint64(*record.Original.SourcePort), 10),
		"--dport", strconv.FormatUint(uint64(*record.Original.DestinationPort), 10),
	)
}

func observerRestartCount(pod corev1.Pod) int32 {
	for _, status := range pod.Status.InitContainerStatuses {
		if status.Name == "observability" {
			return status.RestartCount
		}
	}
	return 0
}

func createVegTestGateway(f *framework.Framework, bfd bool, provider, vpcName, internalSubnetName, externalSubnetName string, replicas int32, antiAffinityMode string, expectedNodes []string, mutators ...func(*apiv1.VpcEgressGateway)) (*apiv1.VpcEgressGateway, *apiv1.Subnet, string, string) {
	ginkgo.GinkgoHelper()

	namespaceName := f.Namespace.Name
	snatSubnetName := "snat-" + framework.RandomSuffix()
	forwardSubnetName := "forward-" + framework.RandomSuffix()
	subnetClient := f.SubnetClient()
	vegClient := f.VpcEgressGatewayClient()

	var forwardSubnet *apiv1.Subnet
	for _, subnetName := range []string{snatSubnetName, forwardSubnetName} {
		ginkgo.By("Creating subnet " + subnetName)
		cidr := framework.RandomCIDR(f.ClusterIPFamily)
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", vpcName, "", nil, nil, nil)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting subnet " + subnetName)
			subnetClient.DeleteSync(subnetName)
		})
		_ = subnetClient.CreateSync(subnet)
		if subnetName == forwardSubnetName {
			forwardSubnet = subnet
		}
	}

	vegName := "veg-" + framework.RandomSuffix()
	snatLabelValue := vegName
	veg := framework.MakeVpcEgressGateway(namespaceName, vegName, vpcName, replicas, internalSubnetName, externalSubnetName)
	if rand.Int32N(2) == 0 {
		veg.Spec.Prefix = fmt.Sprintf("e2e-%s-", framework.RandomSuffix())
	}
	veg.Spec.BFD.Enabled = bfd
	veg.Spec.PodAntiAffinity = antiAffinityMode
	veg.Spec.Policies = []apiv1.VpcEgressGatewayPolicy{{
		SNAT:     false,
		IPBlocks: strings.Split(forwardSubnet.Spec.CIDRBlock, ","),
	}}
	if len(expectedNodes) != 0 {
		if antiAffinityMode == apiv1.PodAntiAffinityPreferred {
			veg.Spec.NodeSelector = []apiv1.VpcEgressGatewayNodeSelector{{
				MatchLabels: map[string]string{corev1.LabelHostname: expectedNodes[0]},
			}}
		} else {
			// test vpc egress gateway with node selector and tolerations
			veg.Spec.NodeSelector = []apiv1.VpcEgressGatewayNodeSelector{{
				MatchLabels: map[string]string{
					constants.LabelNodeRoleControlPlane: "",
				},
			}}
			veg.Spec.Tolerations = []corev1.Toleration{{
				Key:    constants.LabelNodeRoleControlPlane,
				Effect: corev1.TaintEffectNoSchedule,
			}}
		}
	}
	if vpcName == util.DefaultVpc {
		veg.Spec.VPC = "" // test whether the veg works without specifying VPC
		veg.Spec.TrafficPolicy = apiv1.TrafficPolicyLocal
	}
	if util.IsOvnProvider(provider) {
		veg.Spec.Selectors = []apiv1.VpcEgressGatewaySelector{{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					corev1.LabelMetadataName: namespaceName,
				},
			},
			PodSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"snat": snatLabelValue,
				},
			},
		}}
	} else {
		veg.Spec.Policies = append(veg.Spec.Policies, apiv1.VpcEgressGatewayPolicy{
			SNAT:    true,
			Subnets: []string{snatSubnetName},
		})
	}
	for _, mutate := range mutators {
		mutate(veg)
	}

	ginkgo.DeferCleanup(func() {
		ginkgo.By("Deleting vpc egress gateway " + vegName)
		vegClient.DeleteSync(vegName)
	})
	ginkgo.By(fmt.Sprintf("Creating vpc egress gateway %s:\n%s", vegName, format.Object(veg, 2)))
	veg = vegClient.CreateSync(veg)

	ginkgo.By("Validating vpc egress gateway status")
	framework.ExpectTrue(veg.Status.Ready)
	framework.ExpectEqual(veg.Status.Phase, apiv1.PhaseCompleted)
	framework.ExpectHaveLen(veg.Status.InternalIPs, int(replicas))
	framework.ExpectHaveLen(veg.Status.ExternalIPs, int(replicas))
	return veg, forwardSubnet, snatSubnetName, snatLabelValue
}

func validateVegTestWorkload(f *framework.Framework, veg *apiv1.VpcEgressGateway, expectedNodes []string) ([]corev1.Pod, map[string][]string) {
	ginkgo.GinkgoHelper()

	ginkgo.By("Validating vpc egress gateway workload")
	framework.ExpectEqual(veg.Status.Workload.Name, veg.Spec.Prefix+veg.Name)
	deployClient := f.DeploymentClient()
	deploy := deployClient.Get(veg.Status.Workload.Name)
	framework.ExpectEqual(deploy.Status.Replicas, veg.Spec.Replicas)
	framework.ExpectEqual(deploy.Status.ReadyReplicas, veg.Spec.Replicas)
	gvk := appsv1.SchemeGroupVersion.WithKind(reflect.TypeFor[appsv1.Deployment]().Name())
	framework.ExpectEqual(veg.Status.Workload.APIVersion, gvk.GroupVersion().String())
	framework.ExpectEqual(veg.Status.Workload.Kind, gvk.Kind)
	expectedNodeCount := int(veg.Spec.Replicas)
	if veg.Spec.PodAntiAffinity == apiv1.PodAntiAffinityPreferred {
		expectedNodeCount = 1
	}
	framework.ExpectHaveLen(veg.Status.Workload.Nodes, expectedNodeCount)
	workloadPods, err := deployClient.GetPods(deploy)
	framework.ExpectNoError(err)
	framework.ExpectHaveLen(workloadPods.Items, int(veg.Spec.Replicas))
	podNodes := make([]string, 0, len(workloadPods.Items))
	intIPs := make(map[string][]string, len(workloadPods.Items))
	requiredPodAntiAffinity := []corev1.PodAffinityTerm{{
		LabelSelector: &metav1.LabelSelector{
			MatchLabels: maps.Clone(deploy.Spec.Selector.MatchLabels),
		},
		TopologyKey: corev1.LabelHostname,
	}}
	for _, pod := range workloadPods.Items {
		framework.ExpectEmpty(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution)
		framework.ExpectNil(pod.Spec.Affinity.PodAffinity)
		framework.ExpectNotNil(pod.Spec.Affinity.PodAntiAffinity)
		if veg.Spec.PodAntiAffinity == apiv1.PodAntiAffinityPreferred {
			framework.ExpectEmpty(pod.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution)
			preferred := pod.Spec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution
			framework.ExpectHaveLen(preferred, 1)
			framework.ExpectEqual(preferred[0].Weight, int32(100))
			framework.ExpectEqual(preferred[0].PodAffinityTerm, requiredPodAntiAffinity[0])
			framework.ExpectEqual(pod.Spec.NodeName, expectedNodes[0])
		} else {
			framework.ExpectNil(pod.Spec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution)
			framework.ExpectEqual(pod.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution, requiredPodAntiAffinity)
			framework.ExpectNotContainElement(podNodes, pod.Spec.NodeName)
		}
		podNodes = append(podNodes, pod.Spec.NodeName)
		intIPs[pod.Spec.NodeName] = append(intIPs[pod.Spec.NodeName], util.PodIPs(pod)...)
	}
	uniquePodNodes := slices.Clone(podNodes)
	slices.Sort(uniquePodNodes)
	uniquePodNodes = slices.Compact(uniquePodNodes)
	framework.ExpectConsistOf(veg.Status.Workload.Nodes, uniquePodNodes)
	if len(expectedNodes) != 0 {
		framework.ExpectConsistOf(uniquePodNodes, expectedNodes)
	}
	expectedNexthops := make([]string, 0, len(veg.Status.InternalIPs)*2)
	for _, ips := range veg.Status.InternalIPs {
		expectedNexthops = append(expectedNexthops, strings.Split(ips, ",")...)
	}
	expectedIPv4, expectedIPv6 := util.SplitIpsByProtocol(expectedNexthops)
	if veg.Spec.PodAntiAffinity == apiv1.PodAntiAffinityPreferred {
		vegKey := veg.Namespace + "/" + veg.Name
		if len(expectedIPv4) != 0 {
			waitVpcEgressGatewayPolicyNexthops(vegKey, util.EgressGatewayPolicyPriority, 4, expectedIPv4)
		}
		if len(expectedIPv6) != 0 {
			waitVpcEgressGatewayPolicyNexthops(vegKey, util.EgressGatewayPolicyPriority, 6, expectedIPv6)
		}
	}
	if veg.Spec.BFD.Enabled && !f.VersionPriorTo(1, 15) {
		verifyBFDDZeroSessionsRecovery(f, veg.Namespace, workloadPods.Items[0])
	}
	return workloadPods.Items, intIPs
}

func validateVegTestAccess(f *framework.Framework, veg *apiv1.VpcEgressGateway, provider, nadName string, forwardSubnet *apiv1.Subnet, snatSubnetName, snatLabelValue string, workloadPods []corev1.Pod, intIPs map[string][]string) {
	ginkgo.GinkgoHelper()

	namespaceName := f.Namespace.Name
	podClient := f.PodClient()
	svrPodName := "svr-" + framework.RandomSuffix()
	ginkgo.By("Creating netexec server pod " + svrPodName)
	routes := util.NewPodRoutes()
	dstV4, dstV6 := util.SplitStringIP(forwardSubnet.Spec.CIDRBlock)
	gwV4, gwV6 := util.SplitStringIP(veg.Status.ExternalIPs[0])
	routes.Add(provider, dstV4, gwV4)
	routes.Add(provider, dstV6, gwV6)
	annotations, err := routes.ToAnnotations()
	framework.ExpectNoError(err)
	attachmentNetworkName := fmt.Sprintf("%s/%s", namespaceName, nadName)
	annotations[nadv1.NetworkAttachmentAnnot] = attachmentNetworkName
	port := strconv.Itoa(8000 + rand.IntN(1000))
	args := []string{"netexec", "--http-port", port}
	svrPod := framework.MakePrivilegedPod(namespaceName, svrPodName, nil, annotations, framework.AgnhostImage, nil, args)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Deleting pod " + svrPodName)
		podClient.DeleteSync(svrPodName)
	})
	svrPod = podClient.CreateSync(svrPod)
	svrIPs, err := util.PodAttachmentIPs(svrPod, attachmentNetworkName)
	framework.ExpectNoError(err)

	image := workloadPods[0].Spec.Containers[0].Image
	extIPs := make([]string, 0, len(veg.Status.ExternalIPs)*2)
	for _, ips := range veg.Status.ExternalIPs {
		extIPs = append(extIPs, strings.Split(ips, ",")...)
	}
	checkAccess := func(nodeName string) {
		checkEgressAccess(f, namespaceName, svrPodName, image, port, svrIPs, extIPs, intIPs, snatSubnetName, nodeName, snatLabelValue, true)
		checkEgressAccess(f, namespaceName, svrPodName, image, port, svrIPs, extIPs, intIPs, forwardSubnet.Name, nodeName, snatLabelValue, false)
	}

	var nodeName string
	if veg.Spec.TrafficPolicy == apiv1.TrafficPolicyLocal {
		nodeName = veg.Status.Workload.Nodes[0]
	}
	checkAccess(nodeName)

	if veg.Spec.PodAntiAffinity == apiv1.PodAntiAffinityPreferred {
		expectedNexthops := make([]string, 0, len(veg.Status.InternalIPs)*2)
		for _, ips := range veg.Status.InternalIPs {
			expectedNexthops = append(expectedNexthops, strings.Split(ips, ",")...)
		}
		expectedIPv4, expectedIPv6 := util.SplitIpsByProtocol(expectedNexthops)
		original := veg.DeepCopy()
		modified := veg.DeepCopy()
		modified.Spec.TrafficPolicy = apiv1.TrafficPolicyLocal
		vegClient := f.VpcEgressGatewayClient()
		veg = vegClient.PatchSync(original, modified)

		vegKey := namespaceName + "/" + veg.Name
		if len(expectedIPv4) != 0 {
			waitVpcEgressGatewayPolicyNexthops(vegKey, util.EgressGatewayLocalPolicyPriority, 4, expectedIPv4)
		}
		if len(expectedIPv6) != 0 {
			waitVpcEgressGatewayPolicyNexthops(vegKey, util.EgressGatewayLocalPolicyPriority, 6, expectedIPv6)
		}
		checkAccess(veg.Status.Workload.Nodes[0])
	}
}

func vegTest(f *framework.Framework, bfd bool, provider, nadName, vpcName, internalSubnetName, externalSubnetName string, replicas int32, antiAffinityMode string, expectedNodes []string) {
	ginkgo.GinkgoHelper()

	veg, forwardSubnet, snatSubnetName, snatLabelValue := createVegTestGateway(
		f, bfd, provider, vpcName, internalSubnetName, externalSubnetName, replicas, antiAffinityMode, expectedNodes,
	)
	workloadPods, intIPs := validateVegTestWorkload(f, veg, expectedNodes)
	validateVegTestAccess(f, veg, provider, nadName, forwardSubnet, snatSubnetName, snatLabelValue, workloadPods, intIPs)
}
