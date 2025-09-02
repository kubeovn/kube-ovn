package multus

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"math/rand/v2"
	"net"
	"reflect"

	// "slices"
	"strconv"
	"strings"
	"testing"

	// "time"

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

	// "github.com/kubeovn/kube-ovn/test/e2e/framework/iproute"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

type GlobalConfig struct {
	ASN             uint32   `json:"asn"`
	RouterID        string   `json:"router_id"`
	ListenPort      int      `json:"listen_port"`
	ListenAddresses []string `json:"listen_addresses"`
}

type NeighborMessages struct {
	Received struct {
		Notification uint64 `json:"notification,omitempty"`
		Open         uint64 `json:"open,omitempty"`
		Update       uint64 `json:"update,omitempty"`
		Keepalive    uint64 `json:"keepalive,omitempty"`
		Total        uint64 `json:"total,omitempty"`
	} `json:"received"`
	Sent struct {
		Open      uint64 `json:"open,omitempty"`
		Update    uint64 `json:"update,omitempty"`
		Keepalive uint64 `json:"keepalive,omitempty"`
		Total     uint64 `json:"total,omitempty"`
	} `json:"sent"`
}

type NeighborConfig struct {
	LocalASN        uint32 `json:"local_asn"`
	NeighborAddress string `json:"neighbor_address"`
	PeerASN         uint32 `json:"peer_asn"`
	Type            int    `json:"type"`
}

type NeighborState struct {
	LocalASN        uint32           `json:"local_asn"`
	Messages        NeighborMessages `json:"messages"`
	NeighborAddress string           `json:"neighbor_address"`
	PeerASN         uint32           `json:"peer_asn"`
	Type            int              `json:"type"`
	SessionState    int              `json:"session_state"`
	RouterID        string           `json:"router_id,omitempty"`
}

type NeighborEntry struct {
	Conf  NeighborConfig `json:"conf"`
	State NeighborState  `json:"state"`
}

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

// controlPlaneLabel = "node-role.kubernetes.io/control-plane"
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

var _ = framework.Describe("[group:ber]", func() {
	f := framework.NewDefaultFramework("ber")

	var vpcClient *framework.VpcClient
	var subnetClient *framework.SubnetClient
	var nadClient *framework.NetworkAttachmentDefinitionClient
	var nadName, externalSubnetName, namespaceName string
	var schedulableNodes []corev1.Node

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

		nodeList, err = e2enode.GetReadySchedulableNodes(context.Background(), f.ClientSet)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)
		schedulableNodes = nodeList.Items

		replicas = min(int32(len(schedulableNodes)), 3)
	})

	framework.ConformanceIt("should be able to create edge-router with macvlan", func() {
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
		vpcCidr := framework.RandomCIDR(f.ClusterIPFamily)
		bfdIP := framework.RandomIPs(vpcCidr, ";", 1)
		ginkgo.By("Creating vpc " + vpcName + ", enabling BFD Port with IP " + bfdIP + " for VPC " + vpcName)
		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting vpc " + vpcName)
			vpcClient.DeleteSync(vpcName)
		})
		vpc := &apiv1.Vpc{
			ObjectMeta: metav1.ObjectMeta{
				Name: vpcName,
			},
			Spec: apiv1.VpcSpec{
				BFDPort: &apiv1.BFDPort{
					Enabled: true,
					IP:      bfdIP,
				},
			},
		}
		vpc = vpcClient.CreateSync(vpc)
		framework.ExpectNotEmpty(vpc.Status.BFDPort.Name)

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

		berTest(f, true, provider, nadName, vpcName, internalSubnetName, externalSubnetName, replicas)
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

func checkEgressAccess(f *framework.Framework, namespaceName, svrPodName, image, svrPort string, svrIPs, extIPs []string, intIPs map[string][]string, subnetName, nodeName string, snat bool) {
	ginkgo.GinkgoHelper()

	podName := "pod-" + framework.RandomSuffix()
	ginkgo.By("Creating client pod " + podName + " within subnet " + subnetName)
	labels := map[string]string{"snat": strconv.FormatBool(snat)}
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

func parseGobgpOutput(output string) (*GlobalConfig, []NeighborEntry, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")

	var globalJSON, neighborJSON string
	var foundGlobal bool

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// global config starts with {
		if strings.HasPrefix(line, "{") && !foundGlobal {
			globalJSON = line
			foundGlobal = true
		} else if strings.HasPrefix(line, "[") {
			// neighbor config starts with [
			neighborJSON = line
		}
	}

	if globalJSON == "" || neighborJSON == "" {
		return nil, nil, errors.New("failed to extract JSON parts from output")
	}

	// GlobalConfig
	var global GlobalConfig
	if err := json.Unmarshal([]byte(globalJSON), &global); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal global config: %w", err)
	}

	// NeighborEntry
	var neighbors []NeighborEntry
	if err := json.Unmarshal([]byte(neighborJSON), &neighbors); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal neighbor config: %w", err)
	}

	return &global, neighbors, nil
}

func checkBgpInitSetting(ber *apiv1.BgpEdgeRouter, output string) {
	ginkgo.GinkgoHelper()

	global, neighbors, err := parseGobgpOutput(output)
	framework.ExpectNoError(err, "parsing gobgp output")

	remoteAsn := ber.Spec.BGP.RemoteASN

	framework.ExpectEqual(global.ASN, ber.Spec.BGP.ASN, "global ASN mismatch")
	if ber.Spec.BGP.RouterID != "" {
		framework.ExpectEqual(global.RouterID, ber.Spec.BGP.RouterID, "global Router ID mismatch")
	}
	// TODO check when ber.Spec.BGP.RouterID is empty

	for _, berNeighborAddr := range ber.Spec.BGP.Neighbors {
		matchFound := false
		for _, neighbor := range neighbors {
			if neighbor.Conf.NeighborAddress == berNeighborAddr {
				matchFound = true
				framework.ExpectEqual(neighbor.Conf.PeerASN, remoteAsn, "neighbor %s peer ASN %d mismatch", berNeighborAddr, remoteAsn)
				break
			}
		}
		if !matchFound {
			framework.Failf("neighbor address %s not found", berNeighborAddr)
		}
	}
}

func berTest(f *framework.Framework, bfd bool, provider, nadName, vpcName, internalSubnetName, externalSubnetName string, replicas int32) {
	ginkgo.GinkgoHelper()

	namespaceName := f.Namespace.Name
	forwardSubnetName := "forward-" + framework.RandomSuffix()
	subnetClient := f.SubnetClient()
	berClient := f.BgpEdgeRouterClient()
	deployClient := f.DeploymentClient()
	podClient := f.PodClient()

	var forwardSubnet *apiv1.Subnet
	ginkgo.By("Creating subnet " + forwardSubnetName)
	cidr := framework.RandomCIDR(f.ClusterIPFamily)
	subnet := framework.MakeSubnet(forwardSubnetName, "", cidr, "", vpcName, "", nil, nil, nil)
	ginkgo.DeferCleanup(func() {
		ginkgo.By("Deleting subnet " + forwardSubnetName)
		subnetClient.DeleteSync(forwardSubnetName)
	})
	_ = subnetClient.CreateSync(subnet)
	forwardSubnet = subnet

	berName := "ber-" + framework.RandomSuffix()
	ginkgo.By("Creating bgp edge router " + berName)
	ber := framework.MakeBgpEdgeRouter(namespaceName, berName, vpcName, replicas, internalSubnetName, externalSubnetName, forwardSubnetName)
	if rand.Int32N(2) == 0 {
		ber.Spec.Prefix = fmt.Sprintf("e2e-%s-", framework.RandomSuffix())
	}
	ber.Spec.BFD.Enabled = bfd
	if vpcName == util.DefaultVpc {
		ber.Spec.VPC = "" // test whether the ber works without specifying VPC
		ber.Spec.TrafficPolicy = apiv1.TrafficPolicyLocal
	}

	ginkgo.DeferCleanup(func() {
		ginkgo.By("Deleting bgp edge router " + berName)
		berClient.DeleteSync(berName)
	})
	ber = berClient.CreateSync(ber)

	ginkgo.By("Validating bgp edge router status")
	framework.ExpectTrue(ber.Status.Ready)
	framework.ExpectEqual(ber.Status.Phase, apiv1.PhaseCompleted)
	framework.ExpectHaveLen(ber.Status.InternalIPs, int(replicas))
	framework.ExpectHaveLen(ber.Status.ExternalIPs, int(replicas))

	ginkgo.By("Validating bgp edge router workload")
	framework.ExpectEqual(ber.Status.Workload.Name, ber.Spec.Prefix+ber.Name)
	deploy := deployClient.Get(ber.Status.Workload.Name)
	framework.ExpectEqual(deploy.Status.Replicas, replicas)
	framework.ExpectEqual(deploy.Status.ReadyReplicas, replicas)
	gvk := appsv1.SchemeGroupVersion.WithKind(reflect.TypeOf(deploy).Elem().Name())
	framework.ExpectEqual(ber.Status.Workload.APIVersion, gvk.GroupVersion().String())
	framework.ExpectEqual(ber.Status.Workload.Kind, gvk.Kind)
	framework.ExpectHaveLen(ber.Status.Workload.Nodes, int(replicas))
	workloadPods, err := deployClient.GetPods(deploy)
	framework.ExpectNoError(err)
	framework.ExpectHaveLen(workloadPods.Items, int(replicas))
	podNodes := make([]string, 0, len(workloadPods.Items))
	intIPs := make(map[string][]string, len(workloadPods.Items))
	for _, pod := range workloadPods.Items {
		framework.ExpectNotContainElement(podNodes, pod.Spec.NodeName)
		podNodes = append(podNodes, pod.Spec.NodeName)
		intIPs[pod.Spec.NodeName] = util.PodIPs(pod)
		// exec and list
		ginkgo.By("Checking bgp setting " + pod.Name)
		cmd := "gobgp global -j && gobgp neighbor -j"
		ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, pod.Namespace, pod.Name))
		output := e2epodoutput.RunHostCmdOrDie(pod.Namespace, pod.Name, cmd)
		checkBgpInitSetting(ber, output)
	}
	framework.ExpectConsistOf(ber.Status.Workload.Nodes, podNodes)

	// TODO
	// Add route advertisement

	// Add bgp policy

	svrPodName := "svr-" + framework.RandomSuffix()
	ginkgo.By("Creating netexec server pod " + svrPodName)
	routes := util.NewPodRoutes()
	dstV4, dstV6 := util.SplitStringIP(forwardSubnet.Spec.CIDRBlock)
	gwV4, gwV6 := util.SplitStringIP(ber.Status.ExternalIPs[0])
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
	extIPs := make([]string, 0, len(ber.Status.ExternalIPs)*2)
	for _, ips := range ber.Status.ExternalIPs {
		extIPs = append(extIPs, strings.Split(ips, ",")...)
	}

	var nodeName string
	if ber.Spec.TrafficPolicy == apiv1.TrafficPolicyLocal {
		nodeName = ber.Status.Workload.Nodes[0]
	}
	checkEgressAccess(f, namespaceName, svrPodName, image, port, svrIPs, extIPs, intIPs, forwardSubnetName, nodeName, false)
}
