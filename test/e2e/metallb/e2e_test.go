package kubevirt

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/onsi/ginkgo/v2"
	metallbv1beta1 "go.universe.tf/metallb/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	"k8s.io/utils/ptr"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iproute"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

const (
	dockerNetworkName         = "kube-ovn-vlan"
	localExternalVIPKeyPrefix = "kube-ovn.io/local-external-vip/"
	curlListenPort            = 80
)

type underlayTestEnvironment struct {
	cidrV4          string
	annotations     map[string]string
	subnet          *apiv1.Subnet
	l2Advertisement *metallbv1beta1.L2Advertisement
}

type internalVIPTestEnvironment struct {
	*underlayTestEnvironment
	service           *corev1.Service
	vip               string
	vipNode           string
	nonVIPBackendNode string
	nonVIPNode        string
	backendPods       []corev1.Pod
	vipNodeBackend    *corev1.Pod
	nonVIPNodeBackend *corev1.Pod
}

type internalVIPClientTopology string

const (
	internalVIPClientOnVIPNode           internalVIPClientTopology = "VIP node"
	internalVIPClientOnNonVIPBackendNode internalVIPClientTopology = "non-VIP node with a local backend"
	internalVIPClientOnNonBackendNode    internalVIPClientTopology = "node without a local backend"
)

type logicalFlow struct {
	pipeline string
	tableID  int
	priority int
	match    string
	actions  string
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
	e2e.RunE2ETests(t)
}

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

var _ = framework.SerialDescribe("[group:metallb]", func() {
	f := framework.NewDefaultFramework("metallb")

	var cs clientset.Interface
	var nodeNames []string
	var nodeExecMap map[string]*kind.Node
	var clusterName, providerNetworkName, vlanName, subnetName, deployName, containerName, serviceName, serviceName2, containerID, metallbIPPoolName string
	var linkMap map[string]*iproute.Link
	var routeMap map[string][]iproute.Route
	var subnetClient *framework.SubnetClient
	var vlanClient *framework.VlanClient
	var serviceClient *framework.ServiceClient
	var providerNetworkClient *framework.ProviderNetworkClient
	var dockerNetwork *dockernetwork.Inspect
	var deployClient *framework.DeploymentClient
	var clientIPv4, clientIPv6 string
	var internalDeployName, externalServiceName string

	ginkgo.BeforeEach(func() {
		f.SkipVersionPriorTo(1, 14, "This feature was introduced in v1.14.")
		cs = f.ClientSet
		deployClient = f.DeploymentClient()
		serviceClient = f.ServiceClient()
		subnetName = "subnet-" + framework.RandomSuffix()
		vlanName = "vlan-" + framework.RandomSuffix()
		providerNetworkName = "pn-" + framework.RandomSuffix()
		subnetClient = f.SubnetClient()
		vlanClient = f.VlanClient()
		providerNetworkClient = f.ProviderNetworkClient()
		containerName = "client-" + framework.RandomSuffix()
		deployName = "deploy-" + framework.RandomSuffix()
		internalDeployName = "internal-deploy-" + framework.RandomSuffix()
		metallbIPPoolName = "metallb-ip-pool-" + framework.RandomSuffix()
		serviceName = "service-" + framework.RandomSuffix()
		serviceName2 = "service2-" + framework.RandomSuffix()
		externalServiceName = "external-service-" + framework.RandomSuffix()

		if clusterName == "" {
			ginkgo.By("Getting k8s nodes")
			k8sNodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
			framework.ExpectNoError(err)

			cluster, ok := kind.IsKindProvided(k8sNodes.Items[0].Spec.ProviderID)
			if !ok {
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

		ginkgo.By("Getting nodes")
		nodes, err = kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in cluster")

		ginkgo.By("Getting node links that belong to the docker network")
		linkMap = make(map[string]*iproute.Link, len(nodes))
		routeMap = make(map[string][]iproute.Route, len(nodes))
		nodeNames = make([]string, 0, len(nodes))
		nodeExecMap = make(map[string]*kind.Node, len(nodes))
		for i := range nodes {
			node := &nodes[i]
			links, err := node.ListLinks()
			framework.ExpectNoError(err, "failed to list links on node %s: %v", node.Name(), err)
			routes, err := node.ListRoutes(true)
			framework.ExpectNoError(err, "failed to list routes on node %s: %v", node.Name(), err)
			for _, link := range links {
				if link.Address == node.NetworkSettings.Networks[dockerNetworkName].MacAddress.String() {
					linkMap[node.ID] = &link
					break
				}
			}
			framework.ExpectHaveKey(linkMap, node.ID)
			link := linkMap[node.ID]
			for _, route := range routes {
				if route.Dev == link.IfName {
					r := iproute.Route{
						Dst:     route.Dst,
						Gateway: route.Gateway,
						Dev:     route.Dev,
						Flags:   route.Flags,
					}
					routeMap[node.ID] = append(routeMap[node.ID], r)
				}
			}
			framework.ExpectHaveKey(routeMap, node.ID)
			linkMap[node.Name()] = linkMap[node.ID]
			routeMap[node.Name()] = routeMap[node.ID]
			nodeNames = append(nodeNames, node.Name())
			nodeExecMap[node.Name()] = node
		}

		ginkgo.By("Creating a new kind node as Client and connecting it to the docker network")
		cmd := []string{"sh", "-c", "sleep 600"}
		containerInfo, err := docker.ContainerCreate(containerName, f.KubeOVNImage, dockerNetworkName, cmd)
		framework.ExpectNoError(err)
		containerID = containerInfo.ID
		ContainerInspect, err := docker.ContainerInspect(containerID)
		framework.ExpectNoError(err)
		// Save both IPv4 and IPv6 addresses for dual stack testing
		if ContainerInspect.NetworkSettings.Networks[dockerNetworkName].IPAddress.IsValid() {
			clientIPv4 = ContainerInspect.NetworkSettings.Networks[dockerNetworkName].IPAddress.String()
		}
		if ContainerInspect.NetworkSettings.Networks[dockerNetworkName].GlobalIPv6Address.IsValid() {
			clientIPv6 = ContainerInspect.NetworkSettings.Networks[dockerNetworkName].GlobalIPv6Address.String()
		}
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Delete service")
		if serviceName != "" {
			f.ServiceClient().DeleteSync(serviceName)
		}

		if serviceName2 != "" {
			f.ServiceClient().DeleteSync(serviceName2)
		}
		if externalServiceName != "" {
			f.ServiceClient().DeleteSync(externalServiceName)
		}

		ginkgo.By("Deleting the IPAddressPool for metallb")
		f.MetallbClientSet.DeleteIPAddressPool(metallbIPPoolName) // nolint:errcheck

		ginkgo.By("Deleting the l2 advertisement for metallb")
		f.MetallbClientSet.DeleteL2Advertisement(metallbIPPoolName) // nolint:errcheck

		ginkgo.By("Deleting the deployment " + deployName)
		deployClient.DeleteSync(deployName)
		deployClient.DeleteSync(internalDeployName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)

		ginkgo.By("Deleting vlan " + vlanName)
		vlanClient.Delete(vlanName)

		ginkgo.By("Deleting provider network " + providerNetworkName)
		providerNetworkClient.DeleteSync(providerNetworkName)

		ginkgo.By("Getting nodes")
		nodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in cluster")

		ginkgo.By("Waiting for ovs bridge to disappear")
		deadline := time.Now().Add(time.Minute)
		for _, node := range nodes {
			err = node.WaitLinkToDisappear(util.ExternalBridgeName(providerNetworkName), time.Second, deadline)
			framework.ExpectNoError(err, "timed out waiting for ovs bridge to disappear in node %s", node.Name())
		}

		if dockerNetwork != nil {
			ginkgo.By("Disconnecting nodes from the docker network")
			err = kind.NetworkDisconnect(dockerNetwork.ID, nodes)
			framework.ExpectNoError(err, "disconnecting nodes from network "+dockerNetworkName)
		}

		if containerID != "" {
			ginkgo.By("Deleting the client container")
			err = docker.ContainerRemove(containerID)
			framework.ExpectNoError(err, "removing container "+containerID)
		}
	})

	setupUnderlayEnvironment := func() *underlayTestEnvironment {
		underlayCIDRs := make([]string, 0, 2)
		gateways := make([]string, 0, 2)
		var metallbVIPv4s, metallbVIPv6s []string
		var metallbVIPv4Range, metallbVIPv6Range string

		ginkgo.By("Creating provider network " + providerNetworkName)
		pn := makeProviderNetwork(providerNetworkName, false, linkMap)
		_ = providerNetworkClient.CreateSync(pn)

		ginkgo.By("Getting docker network " + dockerNetworkName)
		network, err := docker.NetworkInspect(dockerNetworkName)
		framework.ExpectNoError(err, "getting docker network "+dockerNetworkName)

		ginkgo.By("Creating vlan " + vlanName)
		vlan := framework.MakeVlan(vlanName, providerNetworkName, 0)
		_ = vlanClient.Create(vlan)

		var cidrV4, cidrV6, gatewayV4, gatewayV6 string
		for _, config := range dockerNetwork.IPAM.Config {
			switch util.CheckProtocol(config.Subnet.String()) {
			case apiv1.ProtocolIPv4:
				if f.HasIPv4() {
					cidrV4 = config.Subnet.String()
					gatewayV4 = config.Gateway.String()
					framework.Logf("IPv4 config: cidr=%s, gateway=%s", cidrV4, gatewayV4)
				}
			case apiv1.ProtocolIPv6:
				if f.HasIPv6() {
					cidrV6 = config.Subnet.String()
					gatewayV6 = config.Gateway.String()
					framework.Logf("IPv6 config: cidr=%s, gateway=%s", cidrV6, gatewayV6)
				}
			}
		}

		if f.HasIPv4() {
			underlayCIDRs = append(underlayCIDRs, cidrV4)
			if gatewayV4 != "" {
				gateways = append(gateways, gatewayV4)
			}
			for index := range 10 {
				startIP, _, _ := strings.Cut(cidrV4, "/")
				ip, _ := ipam.NewIP(startIP)
				metallbVIPv4s = append(metallbVIPv4s, ip.Add(100+int64(index)).String())
			}
			metallbVIPv4Range = fmt.Sprintf("%s-%s", metallbVIPv4s[0], metallbVIPv4s[len(metallbVIPv4s)-1])
			framework.Logf("metallbVIPv4s: %v", metallbVIPv4s)
		}
		if f.HasIPv6() {
			underlayCIDRs = append(underlayCIDRs, cidrV6)
			if gatewayV6 != "" {
				gateways = append(gateways, gatewayV6)
			}
			for index := range 10 {
				startIP, _, _ := strings.Cut(cidrV6, "/")
				ip, _ := ipam.NewIP(startIP)
				metallbVIPv6s = append(metallbVIPv6s, ip.Add(100+int64(index)).String())
			}
			metallbVIPv6Range = fmt.Sprintf("%s-%s", metallbVIPv6s[0], metallbVIPv6s[len(metallbVIPv6s)-1])
			framework.Logf("metallbVIPv6s: %v", metallbVIPv6s)
		}

		excludeIPs := make([]string, 0, len(network.Containers)*2+len(metallbVIPv4s)+len(metallbVIPv6s))
		for _, container := range network.Containers {
			if container.IPv4Address.IsValid() && f.HasIPv4() {
				excludeIPs = append(excludeIPs, container.IPv4Address.Addr().String())
			}
			if container.IPv6Address.IsValid() && f.HasIPv6() {
				excludeIPs = append(excludeIPs, container.IPv6Address.Addr().String())
			}
		}
		excludeIPs = append(excludeIPs, metallbVIPv4s...)
		excludeIPs = append(excludeIPs, metallbVIPv6s...)

		ginkgo.By("Creating an IPAddressPool for metallb")
		ipAddressPool := &metallbv1beta1.IPAddressPool{
			ObjectMeta: metav1.ObjectMeta{Name: metallbIPPoolName},
		}
		if metallbVIPv4Range != "" {
			ipAddressPool.Spec.Addresses = append(ipAddressPool.Spec.Addresses, metallbVIPv4Range)
		}
		if metallbVIPv6Range != "" {
			ipAddressPool.Spec.Addresses = append(ipAddressPool.Spec.Addresses, metallbVIPv6Range)
		}
		_, err = f.MetallbClientSet.CreateIPAddressPool(ipAddressPool)
		framework.ExpectNoError(err)

		ginkgo.By("Creating an L2Advertisement for metallb")
		l2Advertisement := f.MetallbClientSet.MakeL2Advertisement(metallbIPPoolName, []string{metallbIPPoolName})
		l2Advertisement, err = f.MetallbClientSet.CreateL2Advertisement(l2Advertisement)
		framework.ExpectNoError(err)

		ginkgo.By("Creating underlay subnet " + subnetName)
		subnet := framework.MakeSubnet(
			subnetName,
			vlanName,
			strings.Join(underlayCIDRs, ","),
			strings.Join(gateways, ","),
			"",
			"",
			excludeIPs,
			nil,
			[]string{},
		)
		subnet.Spec.EnableExternalLBAddress = true
		subnet = subnetClient.CreateSync(subnet)

		return &underlayTestEnvironment{
			cidrV4: cidrV4,
			annotations: map[string]string{
				util.LogicalSwitchAnnotation: subnetName,
			},
			subnet:          subnet,
			l2Advertisement: l2Advertisement,
		}
	}

	setupInternalVIPEnvironment := func() *internalVIPTestEnvironment {
		f.SkipVersionPriorTo(1, 17, "Internal underlay MetalLB VIP access was introduced in v1.17.")
		if !f.HasIPv4() {
			ginkgo.Skip("internal MetalLB VIP topology cases require IPv4")
		}
		framework.ExpectTrue(len(nodeNames) >= 3, "the internal VIP topology test requires three nodes")

		underlayEnv := setupUnderlayEnvironment()
		internalPodLabels := map[string]string{"app": "nginx-internal"}
		args := []string{"netexec", "--http-port", strconv.Itoa(curlListenPort)}

		ginkgo.By("Creating two underlay backends for the internal VIP topology test")
		internalDeploy := framework.MakeDeployment(internalDeployName, 2, internalPodLabels, underlayEnv.annotations, "nginx", framework.AgnhostImage, "")
		internalDeploy.Spec.Template.Spec.Containers[0].Args = args
		internalDeploy.Spec.Template.Spec.Affinity = &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{
					LabelSelector: &metav1.LabelSelector{MatchLabels: internalPodLabels},
					TopologyKey:   "kubernetes.io/hostname",
				}},
			},
		}
		_ = deployClient.CreateSync(internalDeploy)

		ginkgo.By("Creating a LoadBalancer service for the internal VIP topology test")
		ports := []corev1.ServicePort{{
			Name:       "http",
			Port:       80,
			TargetPort: intstr.FromInt(80),
			Protocol:   corev1.ProtocolTCP,
		}}
		service := framework.MakeService(externalServiceName, corev1.ServiceTypeLoadBalancer, nil, internalPodLabels, ports, "")
		service.Spec.IPFamilyPolicy = ptr.To(corev1.IPFamilyPolicySingleStack)
		service.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
		service = serviceClient.CreateSync(service, func(s *corev1.Service) (bool, error) {
			return len(s.Status.LoadBalancer.Ingress) != 0, nil
		}, "LoadBalancer service for internal topology has an ingress IP")

		var vip string
		for _, ingress := range service.Status.LoadBalancer.Ingress {
			if util.CheckProtocol(ingress.IP) == apiv1.ProtocolIPv4 {
				vip = ingress.IP
				break
			}
		}
		framework.ExpectNotEmpty(vip, "no IPv4 LoadBalancer ingress IP was assigned")
		waitUnderlayServiceFlowOnAnyNode(nodeNames, providerNetworkName, vip, curlListenPort, 30*time.Second)
		for _, clusterIP := range service.Spec.ClusterIPs {
			if util.CheckProtocol(clusterIP) == apiv1.ProtocolIPv4 {
				expectNoUnderlayVIPBypassLFlow(clusterIP, curlListenPort)
			}
		}

		vipNode := getVIPNodeFromService(f, service.Name)
		waitUnderlayVIPBypassLFlow(vip, curlListenPort, 30*time.Second)
		waitUnderlayVIPNodeLFlow(vip, util.NodeLspName(vipNode), curlListenPort, 30*time.Second)

		backendPodList, err := cs.CoreV1().Pods(f.Namespace.Name).List(context.Background(), metav1.ListOptions{
			LabelSelector: "app=nginx-internal",
		})
		framework.ExpectNoError(err, "listing MetalLB backends")
		framework.ExpectHaveLen(backendPodList.Items, 2, "internal VIP topology requires exactly two backends")

		env := &internalVIPTestEnvironment{
			underlayTestEnvironment: underlayEnv,
			service:                 service,
			vip:                     vip,
			vipNode:                 vipNode,
			backendPods:             backendPodList.Items,
		}
		backendNodes := make(map[string]*corev1.Pod, len(env.backendPods))
		for i := range env.backendPods {
			pod := &env.backendPods[i]
			backendNodes[pod.Spec.NodeName] = pod
			if pod.Spec.NodeName == vipNode {
				env.vipNodeBackend = pod
			}
		}
		framework.ExpectNotNil(env.vipNodeBackend, "no backend is available on VIP node %s", vipNode)

		for _, nodeName := range nodeNames {
			if nodeName == vipNode {
				continue
			}
			if backend := backendNodes[nodeName]; backend != nil {
				env.nonVIPBackendNode = nodeName
				env.nonVIPNodeBackend = backend
			} else {
				env.nonVIPNode = nodeName
			}
		}
		framework.ExpectNotEmpty(env.nonVIPBackendNode, "no non-VIP node with a local backend is available")
		framework.ExpectNotEmpty(env.nonVIPNode, "no node without a local backend is available")
		waitUnderlayVIPBackendLFlowAbsent(env.backendPods, vipNode, 30*time.Second)
		return env
	}

	runInternalVIPTopologyCase := func(env *internalVIPTestEnvironment, topology internalVIPClientTopology) {
		var nodeName string
		switch topology {
		case internalVIPClientOnVIPNode:
			nodeName = env.vipNode
		case internalVIPClientOnNonVIPBackendNode:
			nodeName = env.nonVIPBackendNode
		case internalVIPClientOnNonBackendNode:
			nodeName = env.nonVIPNode
		default:
			framework.Failf("unknown internal VIP client topology %q", topology)
		}

		ginkgo.By("Creating an underlay client on the " + string(topology))
		client := framework.MakePod(
			f.Namespace.Name,
			"internal-client-"+framework.RandomSuffix(),
			map[string]string{"app": "metallb-internal-client"},
			env.annotations,
			framework.AgnhostImage,
			[]string{"sleep", "infinity"},
			nil,
		)
		client.Spec.NodeName = nodeName
		client = f.PodClient().CreateSync(client)
		defer f.PodClient().DeleteSync(client.Name)
		checkInternalPodVIPBackend(f, client, env.vip, env.vipNode)
	}

	enableU2O := func(env *internalVIPTestEnvironment) {
		ginkgo.By("Enabling u2oInterconnection on subnet")
		subnet := subnetClient.Get(subnetName)
		subnet.Spec.U2OInterconnection = true
		env.subnet = subnetClient.UpdateSync(subnet, metav1.UpdateOptions{}, 30*time.Second)

		ginkgo.By("Waiting for u2oInterconnection MAC to be set")
		framework.WaitUntil(5*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			env.subnet = subnetClient.Get(subnetName)
			return env.subnet.Status.U2OInterconnectionMAC != "", nil
		}, "u2oInterconnection MAC not set")
		waitUnderlayVIPBackendLFlows(env.backendPods, env.vipNode, 30*time.Second)
	}

	disableU2O := func(env *internalVIPTestEnvironment) {
		ginkgo.By("Disabling u2oInterconnection on subnet")
		subnet := subnetClient.Get(subnetName)
		subnet.Spec.U2OInterconnection = false
		env.subnet = subnetClient.UpdateSync(subnet, metav1.UpdateOptions{}, 30*time.Second)

		ginkgo.By("Waiting for u2oInterconnection MAC to be cleared")
		framework.WaitUntil(5*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			env.subnet = subnetClient.Get(subnetName)
			return env.subnet.Status.U2OInterconnectionMAC == "", nil
		}, "u2oInterconnection MAC not cleared")
		waitUnderlayVIPBackendLFlowAbsent(env.backendPods, env.vipNode, 30*time.Second)
	}

	addUnderlayPolicyRoutes := func(cidrV4 string) {
		ginkgo.By("Adding policy routes to the underlay CIDR through ovn0 on all nodes")
		const policyRouteTable = "10001"
		ginkgo.DeferCleanup(func() {
			for _, nodeName := range nodeNames {
				node := nodeExecMap[nodeName]
				if node == nil {
					continue
				}
				_, _, _ = node.Exec("ip", "rule", "del", "priority", "10001", "to", cidrV4, "table", policyRouteTable)
				_, _, _ = node.Exec("ip", "route", "del", cidrV4, "table", policyRouteTable)
			}
		})

		for _, nodeName := range nodeNames {
			node := nodeExecMap[nodeName]
			framework.ExpectNotNil(node, "kind node %s not found", nodeName)
			output, stderr, err := node.Exec("sh", "-c", "ip -4 route show dev ovn0 | awk '/ via / {print $3; exit}'")
			framework.ExpectNoError(err, "getting ovn0 gateway on node %s: %s", nodeName, stderr)
			ovn0Gateway := strings.TrimSpace(string(output))
			framework.ExpectNotEmpty(ovn0Gateway, "ovn0 gateway not found on node %s", nodeName)
			_, _, _ = node.Exec("ip", "rule", "del", "priority", "10001", "to", cidrV4, "table", policyRouteTable)
			_, _, _ = node.Exec("ip", "route", "del", cidrV4, "table", policyRouteTable)
			_, stderr, err = node.Exec("ip", "route", "add", cidrV4, "via", ovn0Gateway, "dev", "ovn0", "table", policyRouteTable)
			framework.ExpectNoError(err, "adding policy route for underlay CIDR on node %s: %s", nodeName, stderr)
			_, stderr, err = node.Exec("ip", "rule", "add", "priority", "10001", "to", cidrV4, "table", policyRouteTable)
			framework.ExpectNoError(err, "adding policy rule for underlay CIDR on node %s: %s", nodeName, stderr)

			route, stderr, err := node.Exec("ip", "-4", "route", "show", "table", policyRouteTable, "exact", cidrV4)
			framework.ExpectNoError(err, "getting policy route for underlay CIDR on node %s: %s", nodeName, stderr)
			framework.ExpectContainSubstring(string(route), "dev ovn0", "policy route for underlay CIDR should use ovn0 on node %s, output: %s", nodeName, route)
			rule, stderr, err := node.Exec("ip", "-4", "rule", "show", "priority", "10001")
			framework.ExpectNoError(err, "getting policy rule for underlay CIDR on node %s: %s", nodeName, stderr)
			framework.ExpectContainSubstring(string(rule), "to "+cidrV4, "policy rule for underlay CIDR not found on node %s, output: %s", nodeName, rule)
		}
	}

	framework.ConformanceIt("should support MetalLB underlay service lifecycle", func() {
		env := setupUnderlayEnvironment()

		ginkgo.By("Create deploy in underlay subnet")
		podLabels := map[string]string{"app": "nginx"}

		args := []string{"netexec", "--http-port", strconv.Itoa(curlListenPort)}
		deploy := framework.MakeDeployment(deployName, 3, podLabels, env.annotations, "nginx", framework.AgnhostImage, "")
		deploy.Spec.Template.Spec.Containers[0].Args = args
		_ = deployClient.CreateSync(deploy)

		ginkgo.By("Creating the first service for the deployment")
		ports := []corev1.ServicePort{
			{
				Name:       "http",
				Port:       80,
				TargetPort: intstr.FromInt(80),
				Protocol:   corev1.ProtocolTCP,
			},
		}
		service := framework.MakeService(serviceName, corev1.ServiceTypeLoadBalancer, nil, podLabels, ports, "")
		if f.IsDual() {
			service.Spec.IPFamilyPolicy = ptr.To(corev1.IPFamilyPolicyPreferDualStack)
		} else {
			service.Spec.IPFamilyPolicy = ptr.To(corev1.IPFamilyPolicySingleStack)
		}
		service.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
		_ = serviceClient.CreateSync(service, func(s *corev1.Service) (bool, error) {
			return len(s.Status.LoadBalancer.Ingress) != 0, nil
		}, "first lb service ip is not empty")

		ginkgo.By("Creating the second service for the same deployment")
		service2 := framework.MakeService(serviceName2, corev1.ServiceTypeLoadBalancer, nil, podLabels, ports, "")
		if f.IsDual() {
			service2.Spec.IPFamilyPolicy = ptr.To(corev1.IPFamilyPolicyPreferDualStack)
		} else {
			service2.Spec.IPFamilyPolicy = ptr.To(corev1.IPFamilyPolicySingleStack)
		}
		service2.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
		_ = serviceClient.CreateSync(service2, func(s *corev1.Service) (bool, error) {
			return len(s.Status.LoadBalancer.Ingress) != 0, nil
		}, "second lb service ip is not empty")

		service = f.ServiceClient().Get(serviceName)
		service2 = f.ServiceClient().Get(serviceName2)
		tcpLoadBalancer := f.VpcClient().Get(util.DefaultVpc).Status.TCPLoadBalancer
		framework.ExpectNotEmpty(tcpLoadBalancer, "default VPC TCP load balancer should be set")
		vipNodes := make(map[string]string, 2)
		for _, svc := range []*corev1.Service{service, service2} {
			for _, clusterIP := range svc.Spec.ClusterIPs {
				if util.CheckProtocol(clusterIP) == apiv1.ProtocolIPv4 {
					expectNoUnderlayVIPBypassLFlow(clusterIP, curlListenPort)
				}
			}
			for _, ingress := range svc.Status.LoadBalancer.Ingress {
				if util.CheckProtocol(ingress.IP) != apiv1.ProtocolIPv4 {
					continue
				}
				vipNode := getVIPNodeFromService(f, svc.Name)
				vipNodes[ingress.IP] = vipNode
				waitLoadBalancerVIPNodeMarker(tcpLoadBalancer, ingress.IP, util.NodeLspName(vipNode), 30*time.Second)
				waitUnderlayVIPBypassLFlow(ingress.IP, curlListenPort, 30*time.Second)
				waitUnderlayVIPNodeLFlow(ingress.IP, util.NodeLspName(vipNode), curlListenPort, 30*time.Second)
			}
		}

		ginkgo.By("Waiting for underlay OpenFlow rules to be installed for both services")
		for _, svc := range []*corev1.Service{service, service2} {
			for _, ingress := range svc.Status.LoadBalancer.Ingress {
				waitUnderlayServiceFlowOnAnyNode(nodeNames, providerNetworkName, ingress.IP, curlListenPort, 30*time.Second)
			}
		}

		ginkgo.By("Checking both services are reachable")
		for _, svc := range []*corev1.Service{service, service2} {
			for i, ingress := range svc.Status.LoadBalancer.Ingress {
				lbsvcIP := ingress.IP
				ginkgo.By(fmt.Sprintf("Checking service %s[%d] with IP %s", svc.Name, i, lbsvcIP))
				checkReachable(f, containerID, clientIPv4, clientIPv6, lbsvcIP, "80", clusterName, true)
			}
		}

		ginkgo.By("Switching the first service to externalTrafficPolicy=Cluster")
		modifiedService := service.DeepCopy()
		modifiedService.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeCluster
		service = serviceClient.PatchSync(service, modifiedService, func(s *corev1.Service) (bool, error) {
			return s.Spec.ExternalTrafficPolicy == corev1.ServiceExternalTrafficPolicyTypeCluster, nil
		}, "externalTrafficPolicy is Cluster")
		for _, ingress := range service.Status.LoadBalancer.Ingress {
			if util.CheckProtocol(ingress.IP) != apiv1.ProtocolIPv4 {
				continue
			}
			waitLoadBalancerVIPNodeMarker(tcpLoadBalancer, ingress.IP, "", 30*time.Second)
			waitUnderlayVIPBypassLFlowCleaned(ingress.IP, curlListenPort, 30*time.Second)
			waitUnderlayVIPNodeLFlowCleaned(ingress.IP, util.NodeLspName(vipNodes[ingress.IP]), curlListenPort, 30*time.Second)
		}
		waitServiceVIPNodeMarkers(tcpLoadBalancer, service2, vipNodes, 30*time.Second)

		ginkgo.By("Checking the first service remains reachable with externalTrafficPolicy=Cluster")
		for _, ingress := range service.Status.LoadBalancer.Ingress {
			if util.CheckProtocol(ingress.IP) == apiv1.ProtocolIPv4 {
				checkReachable(f, containerID, clientIPv4, clientIPv6, ingress.IP, "80", clusterName, true)
			}
		}

		ginkgo.By("Switching the first service back to externalTrafficPolicy=Local")
		modifiedService = service.DeepCopy()
		modifiedService.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
		service = serviceClient.PatchSync(service, modifiedService, func(s *corev1.Service) (bool, error) {
			return s.Spec.ExternalTrafficPolicy == corev1.ServiceExternalTrafficPolicyTypeLocal, nil
		}, "externalTrafficPolicy is Local")
		for _, ingress := range service.Status.LoadBalancer.Ingress {
			if util.CheckProtocol(ingress.IP) != apiv1.ProtocolIPv4 {
				continue
			}
			waitLoadBalancerVIPNodeMarker(tcpLoadBalancer, ingress.IP, util.NodeLspName(vipNodes[ingress.IP]), 30*time.Second)
			waitUnderlayVIPBypassLFlow(ingress.IP, curlListenPort, 30*time.Second)
			waitUnderlayVIPNodeLFlow(ingress.IP, util.NodeLspName(vipNodes[ingress.IP]), curlListenPort, 30*time.Second)
		}
		waitServiceVIPNodeMarkers(tcpLoadBalancer, service2, vipNodes, 30*time.Second)

		ginkgo.By("Restarting ds kube-ovn-cni")
		daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
		ds := daemonSetClient.Get("kube-ovn-cni")
		daemonSetClient.RestartSync(ds)

		ginkgo.By("Waiting for underlay OpenFlow rules to be restored within 30s")
		for i, ingress := range service.Status.LoadBalancer.Ingress {
			lbsvcIP := ingress.IP
			ginkgo.By(fmt.Sprintf("Checking flow restoration for service %s[%d] with IP %s", service.Name, i, lbsvcIP))
			vipNode := getVIPNode(containerID, lbsvcIP, clusterName)
			flowRestored := waitUnderlayServiceFlow(vipNode, providerNetworkName, lbsvcIP, curlListenPort, 30*time.Second)
			framework.ExpectEqual(flowRestored, true, "underlay service OpenFlow should be restored within 30s")
		}

		ginkgo.By("Deleting the first service")
		serviceClient.DeleteSync(serviceName)

		ginkgo.By("Waiting for first service's underlay OpenFlow rules to be cleaned up")
		for _, ingress := range service.Status.LoadBalancer.Ingress {
			waitUnderlayServiceFlowCleaned(nodeNames, providerNetworkName, ingress.IP, curlListenPort, 30*time.Second)
			if util.CheckProtocol(ingress.IP) == apiv1.ProtocolIPv4 {
				waitLoadBalancerVIPNodeMarker(tcpLoadBalancer, ingress.IP, "", 30*time.Second)
				waitUnderlayVIPBypassLFlowCleaned(ingress.IP, curlListenPort, 30*time.Second)
				waitUnderlayVIPNodeLFlowCleaned(ingress.IP, util.NodeLspName(vipNodes[ingress.IP]), curlListenPort, 30*time.Second)
			}
		}
		waitServiceVIPNodeMarkers(tcpLoadBalancer, service2, vipNodes, 30*time.Second)

		ginkgo.By("Checking the second service is still reachable after first service deletion")
		for i, ingress := range service2.Status.LoadBalancer.Ingress {
			lbsvcIP2 := ingress.IP
			ginkgo.By(fmt.Sprintf("Checking service %s[%d] with IP %s after first service deletion", service2.Name, i, lbsvcIP2))
			checkReachable(f, containerID, clientIPv4, clientIPv6, lbsvcIP2, "80", clusterName, true)
		}

		ginkgo.By("Enabling u2oInterconnection on subnet")
		subnet := subnetClient.Get(subnetName)
		subnet.Spec.U2OInterconnection = true
		subnet = subnetClient.UpdateSync(subnet, metav1.UpdateOptions{}, 30*time.Second)
		framework.WaitUntil(5*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			subnet = subnetClient.Get(subnetName)
			return subnet.Status.U2OInterconnectionMAC != "", nil
		}, "u2oInterconnection MAC not set")

		ginkgo.By("Checking service is still reachable with u2oInterconnection enabled")
		service2 = f.ServiceClient().Get(serviceName2)
		for _, ingress := range service2.Status.LoadBalancer.Ingress {
			lbsvcIP2 := ingress.IP
			ginkgo.By(fmt.Sprintf("Checking service %s with IP %s (with u2oInterconnection)", service2.Name, lbsvcIP2))
			checkReachable(f, containerID, clientIPv4, clientIPv6, lbsvcIP2, "80", clusterName, true)
		}

		ginkgo.By("Verifying OpenFlow rules use u2oInterconnection MAC")
		u2oMAC := subnet.Status.U2OInterconnectionMAC
		framework.ExpectNotEmpty(u2oMAC, "u2oInterconnection MAC should be set")

		for _, ingress := range service2.Status.LoadBalancer.Ingress {
			if util.CheckProtocol(ingress.IP) == apiv1.ProtocolIPv4 {
				lbsvcIP := ingress.IP
				bridgeName := util.ExternalBridgeName(providerNetworkName)
				cmd := fmt.Sprintf("kubectl ko ofctl %s dump-flows %s | grep 'mod_dl_dst:%s' | grep %s",
					nodeNames[0], bridgeName, u2oMAC, lbsvcIP)
				output, err := exec.Command("bash", "-c", cmd).CombinedOutput()
				framework.ExpectNoError(err, "OpenFlow rule with u2oInterconnection MAC not found, output: %s", string(output))
				framework.Logf("Found OpenFlow rule with u2oInterconnection MAC: %s", strings.TrimSpace(string(output)))
			}
		}

		ginkgo.By("Disabling u2oInterconnection on subnet")
		subnet = subnetClient.Get(subnetName)
		subnet.Spec.U2OInterconnection = false
		subnet = subnetClient.UpdateSync(subnet, metav1.UpdateOptions{}, 30*time.Second)

		ginkgo.By("Waiting for u2oInterconnection MAC to be cleared")
		framework.WaitUntil(5*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			subnet = subnetClient.Get(subnetName)
			return subnet.Status.U2OInterconnectionMAC == "", nil
		}, "u2oInterconnection MAC not cleared")

		ginkgo.By("Checking service is still reachable after disabling u2oInterconnection")
		for _, ingress := range service2.Status.LoadBalancer.Ingress {
			lbsvcIP2 := ingress.IP
			ginkgo.By(fmt.Sprintf("Checking service %s with IP %s (after disabling u2oInterconnection)", service2.Name, lbsvcIP2))
			checkReachable(f, containerID, clientIPv4, clientIPv6, lbsvcIP2, "80", clusterName, true)
		}
	})

	framework.ConformanceIt("should allow an internal client on the VIP node to access an underlay VIP", func() {
		env := setupInternalVIPEnvironment()
		runInternalVIPTopologyCase(env, internalVIPClientOnVIPNode)
	})

	framework.ConformanceIt("should allow an internal client on a non-VIP backend node to access an underlay VIP", func() {
		env := setupInternalVIPEnvironment()
		runInternalVIPTopologyCase(env, internalVIPClientOnNonVIPBackendNode)
	})

	framework.ConformanceIt("should allow an internal client on a node without backends to access an underlay VIP", func() {
		env := setupInternalVIPEnvironment()
		runInternalVIPTopologyCase(env, internalVIPClientOnNonBackendNode)
	})

	framework.ConformanceIt("should allow a U2O client on the VIP node to access an underlay VIP with policy routes", func() {
		env := setupInternalVIPEnvironment()
		enableU2O(env)
		addUnderlayPolicyRoutes(env.cidrV4)
		runInternalVIPTopologyCase(env, internalVIPClientOnVIPNode)
		disableU2O(env)
	})

	framework.ConformanceIt("should allow a U2O client on a non-VIP backend node to access an underlay VIP with policy routes", func() {
		env := setupInternalVIPEnvironment()
		enableU2O(env)
		addUnderlayPolicyRoutes(env.cidrV4)
		runInternalVIPTopologyCase(env, internalVIPClientOnNonVIPBackendNode)
		disableU2O(env)
	})

	framework.ConformanceIt("should allow a U2O client on a node without backends to access an underlay VIP with policy routes", func() {
		env := setupInternalVIPEnvironment()
		enableU2O(env)
		addUnderlayPolicyRoutes(env.cidrV4)
		runInternalVIPTopologyCase(env, internalVIPClientOnNonBackendNode)
		disableU2O(env)
	})

	framework.ConformanceIt("should move internal underlay VIP traffic when the announcing node changes", func() {
		env := setupInternalVIPEnvironment()
		enableU2O(env)
		addUnderlayPolicyRoutes(env.cidrV4)

		firstVIPNode := env.vipNode
		secondVIPNode := env.nonVIPBackendNode

		tcpLoadBalancer := f.VpcClient().Get(util.DefaultVpc).Status.TCPLoadBalancer
		framework.ExpectNotEmpty(tcpLoadBalancer, "default VPC TCP load balancer should be set")
		waitLoadBalancerVIPNodeMarker(tcpLoadBalancer, env.vip, util.NodeLspName(firstVIPNode), 30*time.Second)

		moveVIP := func(oldVIPNode, newVIPNode string) {
			ginkgo.By(fmt.Sprintf("Moving the L2Advertisement from %s to %s", oldVIPNode, newVIPNode))
			advertisement := env.l2Advertisement.DeepCopy()
			advertisement.Spec.NodeSelectors = []metav1.LabelSelector{{
				MatchLabels: map[string]string{
					"kubernetes.io/hostname": newVIPNode,
				},
			}}
			updatedAdvertisement, err := f.MetallbClientSet.UpdateL2Advertisement(advertisement)
			framework.ExpectNoError(err)
			env.l2Advertisement = updatedAdvertisement
			waitVIPNodeFromService(f, env.service.Name, newVIPNode, 30*time.Second)

			waitLoadBalancerVIPNodeMarker(tcpLoadBalancer, env.vip, util.NodeLspName(newVIPNode), 30*time.Second)
			waitUnderlayVIPBypassLFlow(env.vip, curlListenPort, 30*time.Second)
			waitUnderlayVIPNodeLFlowCleaned(env.vip, util.NodeLspName(oldVIPNode), curlListenPort, 30*time.Second)
			waitUnderlayVIPNodeLFlow(env.vip, util.NodeLspName(newVIPNode), curlListenPort, 30*time.Second)
			waitUnderlayVIPBackendLFlowAbsent(env.backendPods, oldVIPNode, 30*time.Second)
			waitUnderlayVIPBackendLFlows(env.backendPods, newVIPNode, 30*time.Second)
		}

		moveVIP(firstVIPNode, secondVIPNode)
		moveVIP(secondVIPNode, firstVIPNode)
		moveVIP(firstVIPNode, secondVIPNode)

		env.vipNode = secondVIPNode
		env.nonVIPBackendNode = firstVIPNode
		runInternalVIPTopologyCase(env, internalVIPClientOnVIPNode)
		runInternalVIPTopologyCase(env, internalVIPClientOnNonVIPBackendNode)
		runInternalVIPTopologyCase(env, internalVIPClientOnNonBackendNode)
		disableU2O(env)
	})
})

func checkInternalPodVIPBackend(f *framework.Framework, client *corev1.Pod, vip, vipNode string) {
	ginkgo.GinkgoHelper()

	ginkgo.By("Checking the backend observes the original client Pod IP")
	clientIPCommand := fmt.Sprintf("curl -q -s --connect-timeout 2 --max-time 2 %s/clientip", net.JoinHostPort(vip, "80"))
	framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
		output, _, err := framework.ExecShellInPod(context.Background(), f, client.Namespace, client.Name, clientIPCommand)
		if err != nil {
			return false, nil
		}
		observedClientIP, _, err := net.SplitHostPort(strings.TrimSpace(output))
		if err != nil {
			return false, nil
		}
		return observedClientIP == client.Status.PodIP, nil
	}, "underlay VIP backend should observe the original client Pod IP")

	ginkgo.By("Checking the selected backend is on the VIP announcing node")
	hostnameCommand := fmt.Sprintf("curl -q -s --connect-timeout 2 --max-time 2 %s/hostname", net.JoinHostPort(vip, "80"))
	framework.WaitUntil(2*time.Second, 30*time.Second, func(ctx context.Context) (bool, error) {
		output, _, err := framework.ExecShellInPod(context.Background(), f, client.Namespace, client.Name, hostnameCommand)
		if err != nil {
			return false, nil
		}

		backend, err := f.PodClient().Get(ctx, strings.TrimSpace(output), metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		return backend.Spec.NodeName == vipNode, nil
	}, "underlay pod traffic to VIP should be DNATed on the VIP node")
}

func waitServiceVIPNodeMarkers(lbName string, service *corev1.Service, vipNodes map[string]string, timeout time.Duration) {
	ginkgo.GinkgoHelper()

	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if util.CheckProtocol(ingress.IP) == apiv1.ProtocolIPv4 {
			framework.ExpectHaveKey(vipNodes, ingress.IP)
			waitLoadBalancerVIPNodeMarker(lbName, ingress.IP, util.NodeLspName(vipNodes[ingress.IP]), timeout)
		}
	}
}

func waitLoadBalancerVIPNodeMarker(lbName, vip, expectedNodeLSP string, timeout time.Duration) {
	ginkgo.GinkgoHelper()

	key := localExternalVIPKeyPrefix + util.JoinHostPort(vip, curlListenPort)
	description := fmt.Sprintf("load balancer %s external ID %s should be absent", lbName, key)
	if expectedNodeLSP != "" {
		description = fmt.Sprintf("load balancer %s external ID %s should equal %q", lbName, key, expectedNodeLSP)
	}
	framework.WaitUntil(time.Second, timeout, func(_ context.Context) (bool, error) {
		exists, err := hasLoadBalancerExternalID(lbName, key, expectedNodeLSP)
		if err != nil {
			return false, nil
		}
		if expectedNodeLSP == "" {
			return !exists, nil
		}
		return exists, nil
	}, description)
}

func hasLoadBalancerExternalID(lbName, key, value string) (bool, error) {
	condition := fmt.Sprintf(`'external_ids:"%s"!=[]'`, key)
	if value != "" {
		condition = fmt.Sprintf(`'external_ids:"%s"="%s"'`, key, value)
	}
	stdout, stderr, err := framework.NBExec(
		"ovn-nbctl",
		"--data=bare",
		"--format=csv",
		"--no-heading",
		"--columns=_uuid",
		"find",
		"Load_Balancer",
		"name="+lbName,
		condition,
	)
	if err != nil {
		return false, fmt.Errorf("finding load balancer %s external ID %s: %w, stderr: %s", lbName, key, err, stderr)
	}
	return strings.TrimSpace(string(stdout)) != "", nil
}

func waitUnderlayVIPBypassLFlow(vip string, port int32, timeout time.Duration) {
	ginkgo.GinkgoHelper()

	match := fmt.Sprintf("ct.new && ip4.dst == %s && tcp.dst == %d", vip, port)
	waitLogicalFlowCount(1, timeout, "exactly one VIP bypass lflow should exist", func(flow logicalFlow) bool {
		return flow.pipeline == "ingress" && flow.tableID == 13 && flow.priority == 139 &&
			flow.match == match && flow.actions == "next;"
	})
}

func expectNoUnderlayVIPBypassLFlow(vip string, port int32) {
	ginkgo.GinkgoHelper()

	waitUnderlayVIPBypassLFlowCleaned(vip, port, 5*time.Second)
}

func waitUnderlayVIPBypassLFlowCleaned(vip string, port int32, timeout time.Duration) {
	ginkgo.GinkgoHelper()

	match := fmt.Sprintf("ct.new && ip4.dst == %s && tcp.dst == %d", vip, port)
	waitLogicalFlowCount(0, timeout, "VIP bypass lflow should be absent", func(flow logicalFlow) bool {
		return flow.pipeline == "ingress" && flow.tableID == 13 && flow.priority == 139 &&
			flow.match == match && flow.actions == "next;"
	})
}

func waitUnderlayVIPNodeLFlow(vip, vipNodeLSP string, port int32, timeout time.Duration) {
	ginkgo.GinkgoHelper()

	match := fmt.Sprintf(
		"ct.new && ip4.dst == %s && tcp.dst == %d && is_chassis_resident(\"%s\")",
		vip,
		port,
		vipNodeLSP,
	)
	waitLogicalFlowCount(1, timeout, "exactly one VIP DNAT lflow should be resident on the announcing node", func(flow logicalFlow) bool {
		return flow.pipeline == "ingress" && flow.tableID == 13 && flow.priority == 140 &&
			flow.match == match && strings.Contains(flow.actions, "ct_lb_mark")
	})
}

func waitUnderlayVIPNodeLFlowCleaned(vip, vipNodeLSP string, port int32, timeout time.Duration) {
	ginkgo.GinkgoHelper()

	match := fmt.Sprintf(
		"ct.new && ip4.dst == %s && tcp.dst == %d && is_chassis_resident(\"%s\")",
		vip,
		port,
		vipNodeLSP,
	)
	waitLogicalFlowCount(0, timeout, "VIP DNAT lflow should be absent from the old announcing node", func(flow logicalFlow) bool {
		return flow.pipeline == "ingress" && flow.tableID == 13 && flow.priority == 140 && flow.match == match
	})
}

func waitUnderlayVIPBackendLFlows(backendPods []corev1.Pod, vipNode string, timeout time.Duration) {
	ginkgo.GinkgoHelper()

	vipNodeBackendFound := false
	for i := range backendPods {
		pod := &backendPods[i]
		expectedCount := 0
		if pod.Spec.NodeName == vipNode {
			expectedCount = 1
			vipNodeBackendFound = true
		}
		waitUnderlayVIPBackendLFlow(*pod, vipNode, expectedCount, timeout)
	}
	framework.ExpectTrue(vipNodeBackendFound, "no backend is available on VIP node %s", vipNode)
}

func waitUnderlayVIPBackendLFlowAbsent(backendPods []corev1.Pod, vipNode string, timeout time.Duration) {
	ginkgo.GinkgoHelper()

	for i := range backendPods {
		waitUnderlayVIPBackendLFlow(backendPods[i], vipNode, 0, timeout)
	}
}

func waitUnderlayVIPBackendLFlow(backendPod corev1.Pod, vipNode string, expectedCount int, timeout time.Duration) {
	ginkgo.GinkgoHelper()

	backendIP := podIPv4(backendPod)
	backendMAC := backendPod.Annotations[util.MacAddressAnnotation]
	framework.ExpectNotEmpty(backendMAC, "backend Pod %s has no MAC annotation", backendPod.Name)
	backendLSP := ovs.PodNameToPortName(backendPod.Name, backendPod.Namespace, util.OvnProvider)
	vipNodeLSP := util.NodeLspName(vipNode)
	match := fmt.Sprintf(
		"reg0[2] != 0 && ip4.dst == %s && is_chassis_resident(\"%s\")",
		backendIP,
		vipNodeLSP,
	)
	actions := fmt.Sprintf("eth.dst = %s; outport = \"%s\"; output;", backendMAC, backendLSP)
	description := fmt.Sprintf("targeted backend lflow count for Pod %s on VIP node %s should be %d", backendPod.Name, vipNode, expectedCount)
	waitLogicalFlowCount(expectedCount, timeout, description, func(flow logicalFlow) bool {
		return flow.pipeline == "ingress" && flow.tableID == 28 && flow.priority == 55 &&
			flow.match == match && flow.actions == actions
	})
}

func podIPv4(pod corev1.Pod) string {
	ginkgo.GinkgoHelper()

	for _, podIP := range pod.Status.PodIPs {
		if util.CheckProtocol(podIP.IP) == apiv1.ProtocolIPv4 {
			return podIP.IP
		}
	}
	framework.Failf("Pod %s/%s has no IPv4 address", pod.Namespace, pod.Name)
	return ""
}

func waitLogicalFlowCount(expectedCount int, timeout time.Duration, description string, match func(logicalFlow) bool) {
	ginkgo.GinkgoHelper()

	framework.WaitUntil(time.Second, timeout, func(_ context.Context) (bool, error) {
		flows, err := listLogicalFlows()
		if err != nil {
			return false, nil
		}
		count := 0
		for _, flow := range flows {
			if match(flow) {
				count++
			}
		}
		return count == expectedCount, nil
	}, description)
}

func listLogicalFlows() ([]logicalFlow, error) {
	output, err := exec.Command(
		"kubectl", "ko", "sbctl",
		"--format=csv",
		"--data=bare",
		"--no-heading",
		"--columns=pipeline,table_id,priority,match,actions",
		"find", "Logical_Flow",
	).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("listing OVN logical flows: %w, output: %s", err, output)
	}

	records, err := csv.NewReader(strings.NewReader(string(output))).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parsing OVN logical flows: %w", err)
	}
	flows := make([]logicalFlow, 0, len(records))
	for _, record := range records {
		if len(record) != 5 {
			return nil, fmt.Errorf("unexpected logical flow record with %d columns: %q", len(record), record)
		}
		tableID, err := strconv.Atoi(record[1])
		if err != nil {
			return nil, fmt.Errorf("parsing logical flow table ID %q: %w", record[1], err)
		}
		priority, err := strconv.Atoi(record[2])
		if err != nil {
			return nil, fmt.Errorf("parsing logical flow priority %q: %w", record[2], err)
		}
		flows = append(flows, logicalFlow{
			pipeline: record[0],
			tableID:  tableID,
			priority: priority,
			match:    record[3],
			actions:  record[4],
		})
	}
	return flows, nil
}

func checkReachable(f *framework.Framework, containerID, sourceIPv4, sourceIPv6, targetIP, targetPort, clusterName string, expectReachable bool) {
	ginkgo.GinkgoHelper()
	ginkgo.By("checking curl reachable")
	isIPv6 := util.CheckProtocol(targetIP) == apiv1.ProtocolIPv6

	// Select the appropriate source IP based on target IP protocol
	var sourceIP string
	if isIPv6 {
		sourceIP = sourceIPv6
	} else {
		sourceIP = sourceIPv4
	}

	var cmd []string
	if isIPv6 {
		cmd = []string{
			"curl", "-q", "-s", "-g", "--connect-timeout", "2", "--max-time", "2",
			fmt.Sprintf("[%s]:%s/clientip", targetIP, targetPort),
		}
	} else {
		cmd = []string{
			"curl", "-q", "-s", "--connect-timeout", "2", "--max-time", "2",
			fmt.Sprintf("%s:%s/clientip", targetIP, targetPort),
		}
	}
	if expectReachable {
		var output []byte
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			var err error
			output, _, err = docker.Exec(containerID, nil, cmd...)
			return err == nil, nil
		}, fmt.Sprintf("service %s:%s should be reachable", targetIP, targetPort))
		client, _, err := net.SplitHostPort(strings.TrimSpace(string(output)))
		framework.ExpectNoError(err)
		// check packet has not SNAT
		framework.ExpectEqual(sourceIP, client)
	} else {
		_, _, err := docker.Exec(containerID, nil, cmd...)
		framework.ExpectError(err)
	}

	ginkgo.By("checking vip node is same as backend pod's host")
	if !isIPv6 {
		cmd = strings.Fields(fmt.Sprintf("arping -c 5 -W 2 %s", targetIP))
		output, _, err := docker.Exec(containerID, nil, cmd...)
		if err != nil {
			framework.Failf("arping failed: %v, output: %s", err, output)
		}
		framework.Logf("arping result is %s ", output)
	}

	if isIPv6 {
		cmd = []string{
			"curl", "-q", "-s", "-g", "--connect-timeout", "2", "--max-time", "2",
			fmt.Sprintf("[%s]:%s/hostname", targetIP, targetPort),
		}
	} else {
		cmd = []string{
			"curl", "-q", "-s", "--connect-timeout", "2", "--max-time", "2",
			fmt.Sprintf("%s:%s/hostname", targetIP, targetPort),
		}
	}
	output, _, err := docker.Exec(containerID, nil, cmd...)
	framework.ExpectNoError(err)
	backendPodName := strings.TrimSpace(string(output))
	framework.Logf("Packet reached backend: %s", backendPodName)

	vipNode := getVIPNode(containerID, targetIP, clusterName)

	ginkgo.By("Checking the backend pod's host is same as the metallb vip's node")
	backendPod := f.PodClient().GetPod(backendPodName)
	backendPodNode := backendPod.Spec.NodeName
	framework.ExpectEqual(backendPodNode, vipNode)
}

func getVIPNodeFromService(f *framework.Framework, serviceName string) string {
	ginkgo.GinkgoHelper()

	var vipNode string
	framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
		statuses, err := f.MetallbClientSet.ListServiceL2Statuses()
		if err != nil {
			return false, err
		}
		for _, status := range statuses.Items {
			if status.Status.ServiceNamespace == f.Namespace.Name && status.Status.ServiceName == serviceName {
				vipNode = status.Status.Node
			}
			if vipNode != "" {
				return true, nil
			}
		}
		return false, nil
	}, "MetalLB did not assign a VIP node for service "+serviceName)
	framework.Logf("VIP node for service %s is %s", serviceName, vipNode)
	return vipNode
}

func waitVIPNodeFromService(f *framework.Framework, serviceName, expectedNode string, timeout time.Duration) {
	ginkgo.GinkgoHelper()

	framework.WaitUntil(time.Second, timeout, func(_ context.Context) (bool, error) {
		statuses, err := f.MetallbClientSet.ListServiceL2Statuses()
		if err != nil {
			return false, nil
		}
		for _, status := range statuses.Items {
			if status.Status.ServiceNamespace == f.Namespace.Name &&
				status.Status.ServiceName == serviceName &&
				status.Status.Node == expectedNode {
				return true, nil
			}
		}
		return false, nil
	}, fmt.Sprintf("MetalLB service %s should be announced from node %s", serviceName, expectedNode))
}

func getVIPNode(containerID, targetIP, clusterName string) string {
	ginkgo.GinkgoHelper()

	cmd := strings.Fields("ip neigh show " + targetIP)
	output, _, err := docker.Exec(containerID, nil, cmd...)
	framework.ExpectNoError(err)
	framework.Logf("ip neigh: %s", string(output))
	lines := strings.Split(string(output), "\n")
	var vipMac string
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[0] == targetIP {
			vipMac = fields[4]
			framework.Logf("VIP MAC address: %s", vipMac)
			break
		}
	}

	var vipNode string
	nodes, err := kind.ListNodes(clusterName, "")
	framework.ExpectNoError(err, "getting nodes in kind cluster")
	for _, node := range nodes {
		for _, networkSettings := range node.NetworkSettings.Networks {
			if networkSettings.MacAddress.String() == vipMac {
				vipNode = node.Name()
				break
			}
		}
	}

	framework.ExpectNotEmpty(vipNode, "Failed to find the node with MAC address: %s", vipMac)
	framework.Logf("Node with MAC address %s is %s", vipMac, vipNode)

	return vipNode
}

func waitUnderlayServiceFlow(nodeName, providerNetworkName, serviceIP string, servicePort int32, timeout time.Duration) bool {
	ginkgo.GinkgoHelper()

	bridgeName := util.ExternalBridgeName(providerNetworkName)
	matchPort := fmt.Sprintf("tp_dst=%d", servicePort)
	cmd := fmt.Sprintf("kubectl ko ofctl %s dump-flows %s | grep -w %s | grep -w %s",
		nodeName, bridgeName, serviceIP, matchPort)

	var flowFound bool
	framework.WaitUntil(1*time.Second, timeout, func(_ context.Context) (bool, error) {
		_, err := exec.Command("bash", "-c", cmd).CombinedOutput()
		flowFound = err == nil
		return flowFound, nil
	}, "")

	return flowFound
}

func waitUnderlayServiceFlowOnAnyNode(nodeNames []string, providerNetworkName, serviceIP string, servicePort int32, timeout time.Duration) {
	ginkgo.GinkgoHelper()

	bridgeName := util.ExternalBridgeName(providerNetworkName)
	matchPort := fmt.Sprintf("tp_dst=%d", servicePort)

	framework.WaitUntil(1*time.Second, timeout, func(_ context.Context) (bool, error) {
		for _, nodeName := range nodeNames {
			cmd := fmt.Sprintf("kubectl ko ofctl %s dump-flows %s | grep -w %s | grep -w %s",
				nodeName, bridgeName, serviceIP, matchPort)
			if _, err := exec.Command("bash", "-c", cmd).CombinedOutput(); err == nil {
				return true, nil
			}
		}
		return false, nil
	}, fmt.Sprintf("underlay service flow for %s should be installed on at least one node", serviceIP))
}

func waitUnderlayServiceFlowCleaned(nodeNames []string, providerNetworkName, serviceIP string, servicePort int32, timeout time.Duration) {
	ginkgo.GinkgoHelper()

	bridgeName := util.ExternalBridgeName(providerNetworkName)
	matchPort := fmt.Sprintf("tp_dst=%d", servicePort)

	framework.WaitUntil(1*time.Second, timeout, func(_ context.Context) (bool, error) {
		for _, nodeName := range nodeNames {
			cmd := fmt.Sprintf("kubectl ko ofctl %s dump-flows %s | grep -w %s | grep -w %s",
				nodeName, bridgeName, serviceIP, matchPort)
			if _, err := exec.Command("bash", "-c", cmd).CombinedOutput(); err == nil {
				return false, nil // flow still exists on this node
			}
		}
		return true, nil // flow cleaned from all nodes
	}, fmt.Sprintf("underlay service flow for %s should be cleaned up", serviceIP))
}
