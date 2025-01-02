package multus

import (
	"context"
	"flag"
	"fmt"
	"math/rand/v2"
	"net"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	dockernetwork "github.com/docker/docker/api/types/network"
	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"
	commontest "k8s.io/kubernetes/test/e2e/common"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iproute"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
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

const (
	kindNetwork = "kind"

	controlPlaneLabel = "node-role.kubernetes.io/control-plane"
)

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

var _ = framework.Describe("[group:veg]", func() {
	f := framework.NewDefaultFramework("veg")

	var vpcClient *framework.VpcClient
	var subnetClient *framework.SubnetClient
	var nadClient *framework.NetworkAttachmentDefinitionClient
	var nadName, externalSubnetName, namespaceName string
	var nodes, schedulableNodes []corev1.Node
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

		nodeList, err = e2enode.GetReadySchedulableNodes(context.Background(), f.ClientSet)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)
		schedulableNodes = nodeList.Items

		replicas = min(int32(len(schedulableNodes)), 3)
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
				if link.Address == node.NetworkSettings.Networks[dockerNetworkName].MacAddress {
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

		vpcName := "ovn-cluster"
		cidr := framework.RandomCIDR(f.ClusterIPFamily)
		bfdIP := framework.RandomIPs(cidr, ";", 1)
		ginkgo.By("Enabling BFD Port with IP " + bfdIP + " for VPC " + vpcName)
		vpc := vpcClient.Get(vpcName)
		patchedVpc := vpc.DeepCopy()
		patchedVpc.Spec.BFDPort = &apiv1.BFDPort{
			Enabled: true,
			IP:      bfdIP,
			NodeSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      controlPlaneLabel,
					Operator: metav1.LabelSelectorOpExists,
				}},
			},
		}
		updatedVpc := vpcClient.PatchSync(vpc, patchedVpc, 10*time.Second)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Disabling BFD Port for VPC " + vpcName)
			patchedVpc := updatedVpc.DeepCopy()
			patchedVpc.Spec.BFDPort = nil
			updatedVpc := vpcClient.PatchSync(updatedVpc, patchedVpc, 10*time.Second)
			framework.ExpectEmpty(updatedVpc.Status.BFDPort.Name)
			framework.ExpectEmpty(updatedVpc.Status.BFDPort.Nodes)
		})

		framework.ExpectNotEmpty(updatedVpc.Status.BFDPort.Name)
		for _, node := range nodes {
			if slices.Contains(updatedVpc.Status.BFDPort.Nodes, node.Name) {
				framework.ExpectHaveKey(node.Labels, controlPlaneLabel)
			} else {
				framework.ExpectNotHaveKey(node.Labels, controlPlaneLabel)
			}
		}

		// TODO: check ovn LRP

		vegTest(f, true, provider, nadName, vpcName, vpc.Status.DefaultLogicalSwitch, externalSubnetName, replicas)
	})

	framework.ConformanceIt("should be able to create vpc-egress-gateway with macvlan", func() {
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
		vpc := &apiv1.Vpc{ObjectMeta: metav1.ObjectMeta{Name: vpcName}}
		vpc = vpcClient.CreateSync(vpc)
		framework.ExpectEmpty(vpc.Status.BFDPort.Name)
		framework.ExpectEmpty(vpc.Status.BFDPort.IP)
		framework.ExpectEmpty(vpc.Status.BFDPort.Nodes)

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

		vegTest(f, false, provider, nadName, vpcName, internalSubnetName, externalSubnetName, replicas)
	})
})

func generateSubnetFromDockerNetwork(subnetName string, network *dockernetwork.Inspect, ipv4, ipv6 bool) *apiv1.Subnet {
	ginkgo.GinkgoHelper()

	ginkgo.By("Generating subnet configuration from docker network " + network.Name)
	var cidrV4, cidrV6, gatewayV4, gatewayV6 string
	for _, config := range network.IPAM.Config {
		switch util.CheckProtocol(config.Subnet) {
		case apiv1.ProtocolIPv4:
			if ipv4 {
				cidrV4 = config.Subnet
				gatewayV4 = config.Gateway
			}
		case apiv1.ProtocolIPv6:
			if ipv6 {
				cidrV6 = config.Subnet
				if gatewayV6 = config.Gateway; gatewayV6 == "" {
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
		if container.IPv4Address != "" && ipv4 {
			excludeIPs = append(excludeIPs, strings.Split(container.IPv4Address, "/")[0])
		}
		if container.IPv6Address != "" && ipv6 {
			excludeIPs = append(excludeIPs, strings.Split(container.IPv6Address, "/")[0])
		}
	}

	return framework.MakeSubnet(subnetName, "", strings.Join(cidr, ","), strings.Join(gateway, ","), "", "", excludeIPs, nil, nil)
}

func checkEgressAccess(f *framework.Framework, namespaceName, svrPodName, image, svrPort string, svrIPs, intIPs, extIPs []string, subnetName string, snat bool) {
	ginkgo.GinkgoHelper()

	podName := "pod-" + framework.RandomSuffix()
	ginkgo.By("Creating client pod " + podName + " within subnet " + subnetName)
	annotations := map[string]string{util.LogicalSwitchAnnotation: subnetName}
	pod := framework.MakePod(namespaceName, podName, nil, annotations, image, []string{"sleep", "infinity"}, nil)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Deleting pod " + podName)
		f.PodClient().DeleteSync(podName)
	})
	pod = f.PodClient().CreateSync(pod)

	framework.CheckPodEgressRoutes(pod.Namespace, pod.Name, f.HasIPv4(), f.HasIPv6(), 2, intIPs)

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
		ginkgo.By(fmt.Sprintf(`Executing %q in pod %s`, cmd, pod.Name))
		output := e2epodoutput.RunHostCmdOrDie(pod.Namespace, pod.Name, cmd)
		clientIP, _, err := net.SplitHostPort(strings.TrimSpace(output))
		framework.ExpectNoError(err)
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
		for _, ip := range nextHops {
			args += fmt.Sprintf(" nexthop via %s dev net1 weight 1", ip)
		}
	}
	for _, dst := range destinations {
		cmd := fmt.Sprintf("ip route add %s%s", dst, args)
		output, err := e2epodoutput.RunHostCmd(namespaceName, podName, cmd)
		framework.ExpectNoError(err, output)
	}
}

func vegTest(f *framework.Framework, bfd bool, provider, nadName, vpcName, internalSubnetName, externalSubnetName string, replicas int32) {
	ginkgo.GinkgoHelper()

	namespaceName := f.Namespace.Name
	snatSubnetName := "snat-" + framework.RandomSuffix()
	forwardSubnetName := "forward-" + framework.RandomSuffix()
	subnetClient := f.SubnetClient()
	vegClient := f.VpcEgressGatewayClient()
	deployClient := f.DeploymentClient()
	podClient := f.PodClient()

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
	ginkgo.By("Creating vpc egress gateway " + vegName)
	veg := framework.MakeVpcEgressGateway(namespaceName, vegName, vpcName, replicas, internalSubnetName, externalSubnetName)
	if rand.Int32N(2) == 0 {
		veg.Spec.Prefix = fmt.Sprintf("e2e-%s-", framework.RandomSuffix())
	}
	veg.Spec.BFD.Enabled = bfd
	veg.Spec.Policies = []apiv1.VpcEgressGatewayPolicy{{
		SNAT:    true,
		Subnets: []string{snatSubnetName},
	}, {
		SNAT:     false,
		IPBlocks: strings.Split(forwardSubnet.Spec.CIDRBlock, ","),
	}}
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Deleting vpc egress gateway " + vegName)
		vegClient.DeleteSync(vegName)
	})
	veg = vegClient.CreateSync(veg)

	ginkgo.By("Validating vpc egress gateway status")
	framework.ExpectTrue(veg.Status.Ready)
	framework.ExpectEqual(veg.Status.Phase, apiv1.PhaseCompleted)
	framework.ExpectHaveLen(veg.Status.InternalIPs, int(replicas))
	framework.ExpectHaveLen(veg.Status.ExternalIPs, int(replicas))

	ginkgo.By("Validating vpc egress gateway workload")
	framework.ExpectEqual(veg.Status.Workload.Name, veg.Spec.Prefix+veg.Name)
	deploy := deployClient.Get(veg.Status.Workload.Name)
	framework.ExpectEqual(deploy.Status.Replicas, replicas)
	framework.ExpectEqual(deploy.Status.ReadyReplicas, replicas)
	gvk := appsv1.SchemeGroupVersion.WithKind(reflect.TypeOf(deploy).Elem().Name())
	framework.ExpectEqual(veg.Status.Workload.APIVersion, gvk.GroupVersion().String())
	framework.ExpectEqual(veg.Status.Workload.Kind, gvk.Kind)
	framework.ExpectHaveLen(veg.Status.Workload.Nodes, int(replicas))
	workloadPods, err := deployClient.GetPods(deploy)
	framework.ExpectNoError(err)
	framework.ExpectHaveLen(workloadPods.Items, int(replicas))
	podNodes := make([]string, 0, len(workloadPods.Items))
	for _, pod := range workloadPods.Items {
		framework.ExpectNotContainElement(podNodes, pod.Spec.NodeName)
		podNodes = append(podNodes, pod.Spec.NodeName)
	}
	framework.ExpectConsistOf(veg.Status.Workload.Nodes, podNodes)

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

	image := workloadPods.Items[0].Spec.Containers[0].Image
	intIPs := make([]string, 0, len(veg.Status.InternalIPs)*2)
	extIPs := make([]string, 0, len(veg.Status.ExternalIPs)*2)
	for _, ip := range veg.Status.InternalIPs {
		intIPs = append(intIPs, strings.Split(ip, ",")...)
	}
	for _, ips := range veg.Status.ExternalIPs {
		extIPs = append(extIPs, strings.Split(ips, ",")...)
	}
	checkEgressAccess(f, namespaceName, svrPodName, image, port, svrIPs, intIPs, extIPs, snatSubnetName, true)
	checkEgressAccess(f, namespaceName, svrPodName, image, port, svrIPs, intIPs, extIPs, forwardSubnetName, false)
}
