package kubevirt

import (
	"context"
	"flag"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	dockernetwork "github.com/docker/docker/api/types/network"
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

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iproute"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

const (
	dockerNetworkName = "kube-ovn-vlan"
	curlListenPort    = 80
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

var _ = framework.Describe("[group:metallb]", func() {
	f := framework.NewDefaultFramework("metallb")

	var cs clientset.Interface
	var nodeNames []string
	var clusterName, providerNetworkName, vlanName, subnetName, deployName, containerName, serviceName, containerID, metallbIPPoolName string
	var linkMap map[string]*iproute.Link
	var routeMap map[string][]iproute.Route
	var subnetClient *framework.SubnetClient
	var vlanClient *framework.VlanClient
	var serviceClient *framework.ServiceClient
	var providerNetworkClient *framework.ProviderNetworkClient
	var dockerNetwork *dockernetwork.Inspect
	var deployClient *framework.DeploymentClient
	var clientip string

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
		metallbIPPoolName = "metallb-ip-pool-" + framework.RandomSuffix()
		serviceName = "service-" + framework.RandomSuffix()

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
		for _, node := range nodes {
			links, err := node.ListLinks()
			framework.ExpectNoError(err, "failed to list links on node %s: %v", node.Name(), err)
			routes, err := node.ListRoutes(true)
			framework.ExpectNoError(err, "failed to list routes on node %s: %v", node.Name(), err)
			for _, link := range links {
				if link.Address == node.NetworkSettings.Networks[dockerNetworkName].MacAddress {
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
		}

		ginkgo.By("Creating a new kind node as Client and connecting it to the docker network")
		cmd := []string{"sh", "-c", "sleep 600"}
		containerInfo, err := docker.ContainerCreate(containerName, f.KubeOVNImage, dockerNetworkName, cmd)
		framework.ExpectNoError(err)
		containerID = containerInfo.ID
		ContainerInspect, err := docker.ContainerInspect(containerID)
		framework.ExpectNoError(err)
		clientip = ContainerInspect.NetworkSettings.Networks[dockerNetworkName].IPAddress
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting the IPAddressPool for metallb")
		f.MetallbClientSet.DeleteIPAddressPool(metallbIPPoolName) // nolint:errcheck

		ginkgo.By("Deleting the l2 advertisement for metallb")
		f.MetallbClientSet.DeleteL2Advertisement(metallbIPPoolName) // nolint:errcheck

		ginkgo.By("Deleting the deployment " + deployName)
		deployClient.DeleteSync(deployName)

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
			err = node.WaitLinkToDisappear(util.ExternalBridgeName(providerNetworkName), 2*time.Second, deadline)
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

	framework.ConformanceIt("should support metallb and underlay combine", func() {
		underlayCidr := make([]string, 0, 2)
		gateway := make([]string, 0, 2)
		var metallbVIPv4s, metallbVIPv6s []string
		var metallbVIPv4Str, metallbVIPv6Str string
		var err error

		ginkgo.By("Creating provider network " + providerNetworkName)
		pn := makeProviderNetwork(providerNetworkName, false, linkMap)
		_ = providerNetworkClient.CreateSync(pn)

		ginkgo.By("Getting docker network " + dockerNetworkName)
		network, err := docker.NetworkInspect(dockerNetworkName)
		framework.ExpectNoError(err, "getting docker network "+dockerNetworkName)

		ginkgo.By("Creating vlan " + vlanName)
		vlan := framework.MakeVlan(vlanName, providerNetworkName, 0)
		_ = vlanClient.Create(vlan)

		ginkgo.By("Creating underlay subnet " + subnetName)
		var cidrV4, cidrV6, gatewayV4, gatewayV6 string
		for _, config := range dockerNetwork.IPAM.Config {
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

		if f.HasIPv4() {
			underlayCidr = append(underlayCidr, cidrV4)
			gateway = append(gateway, gatewayV4)
			for index := range 5 {
				startIP := strings.Split(cidrV4, "/")[0]
				ip, _ := ipam.NewIP(startIP)
				metallbVIPv4s = append(metallbVIPv4s, ip.Add(100+int64(index)).String())
			}
			metallbVIPv4Str = fmt.Sprintf("%s-%s", metallbVIPv4s[0], metallbVIPv4s[len(metallbVIPv4s)-1])
		}
		if f.HasIPv6() {
			underlayCidr = append(underlayCidr, cidrV6)
			gateway = append(gateway, gatewayV6)
			for index := range 5 {
				startIP := strings.Split(cidrV6, "/")[0]
				ip, _ := ipam.NewIP(startIP)
				metallbVIPv6s = append(metallbVIPv6s, ip.Add(100+int64(index)).String())
			}
			metallbVIPv6Str = fmt.Sprintf("%s-%s", metallbVIPv6s[0], metallbVIPv6s[len(metallbVIPv6s)-1])
		}

		excludeIPs := make([]string, 0, len(network.Containers)*2)
		for _, container := range network.Containers {
			if container.IPv4Address != "" && f.HasIPv4() {
				excludeIPs = append(excludeIPs, strings.Split(container.IPv4Address, "/")[0])
				if len(metallbVIPv4s) > 0 {
					excludeIPs = append(excludeIPs, metallbVIPv4s...)
				}
			}
			if container.IPv6Address != "" && f.HasIPv6() {
				excludeIPs = append(excludeIPs, strings.Split(container.IPv6Address, "/")[0])
				if len(metallbVIPv6s) > 0 {
					excludeIPs = append(excludeIPs, metallbVIPv6s...)
				}
			}
		}

		ginkgo.By("Creating an IPAddressPool for metallb with address " + metallbVIPv4Str + " and " + metallbVIPv6Str)
		ipAddressPool := &metallbv1beta1.IPAddressPool{
			ObjectMeta: metav1.ObjectMeta{
				Name: metallbIPPoolName,
			},
			Spec: metallbv1beta1.IPAddressPoolSpec{
				Addresses: []string{},
			},
		}
		if metallbVIPv4Str != "" {
			ipAddressPool.Spec.Addresses = append(ipAddressPool.Spec.Addresses, metallbVIPv4Str)
		}
		if metallbVIPv6Str != "" {
			ipAddressPool.Spec.Addresses = append(ipAddressPool.Spec.Addresses, metallbVIPv6Str)
		}
		_, err = f.MetallbClientSet.CreateIPAddressPool(ipAddressPool)
		framework.ExpectNoError(err)

		ginkgo.By("Creating an L2Advertisement for metallb")
		l2Advertisement := &metallbv1beta1.L2Advertisement{
			ObjectMeta: metav1.ObjectMeta{
				Name: metallbIPPoolName,
			},
			Spec: metallbv1beta1.L2AdvertisementSpec{
				IPAddressPools: []string{metallbIPPoolName},
			},
		}
		_, err = f.MetallbClientSet.CreateL2Advertisement(l2Advertisement)
		framework.ExpectNoError(err)

		ginkgo.By("Creating underlay subnet " + subnetName)
		subnet := framework.MakeSubnet(subnetName, vlanName, strings.Join(underlayCidr, ","), strings.Join(gateway, ","), "", "", excludeIPs, nil, []string{})
		subnet.Spec.EnableExternalLBAddress = true
		_ = subnetClient.CreateSync(subnet)

		ginkgo.By("Create deploy in underlay subnet")
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		podLabels := map[string]string{"app": "nginx"}

		args := []string{"netexec", "--http-port", strconv.Itoa(curlListenPort)}
		deploy := framework.MakeDeployment(deployName, 3, podLabels, annotations, "nginx", framework.AgnhostImage, "")
		deploy.Spec.Template.Spec.Containers[0].Args = args
		_ = deployClient.CreateSync(deploy)

		ginkgo.By("Creating a service for the deployment")
		ports := []corev1.ServicePort{
			{
				Name:       "http",
				Port:       80,
				TargetPort: intstr.FromInt(80),
				Protocol:   corev1.ProtocolTCP,
			},
		}
		service := framework.MakeService(serviceName, corev1.ServiceTypeLoadBalancer, nil, podLabels, ports, "")
		service.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
		_ = serviceClient.CreateSync(service, func(s *corev1.Service) (bool, error) {
			return len(s.Status.LoadBalancer.Ingress) != 0, nil
		}, "lb service ip is not empty")

		ginkgo.By("Checking the service is reachable")
		service = f.ServiceClient().Get(serviceName)
		lbsvcIP := service.Status.LoadBalancer.Ingress[0].IP

		checkReachable(f, containerID, clientip, lbsvcIP, "80", clusterName, true)
	})
})

func checkReachable(f *framework.Framework, containerID, sourceIP, targetIP, targetPort, clusterName string, expectReachable bool) {
	ginkgo.GinkgoHelper()
	ginkgo.By("checking curl reachable")
	cmd := strings.Fields(fmt.Sprintf("curl -q -s --connect-timeout 2 --max-time 2 %s/clientip", net.JoinHostPort(targetIP, targetPort)))
	output, _, err := docker.Exec(containerID, nil, cmd...)
	if expectReachable {
		framework.ExpectNoError(err)
		client, _, err := net.SplitHostPort(strings.TrimSpace(string(output)))
		framework.ExpectNoError(err)
		// check packet has not SNAT
		framework.ExpectEqual(sourceIP, client)
	} else {
		framework.ExpectError(err)
	}

	ginkgo.By("checking vip node is same as backend pod's host")
	cmd = strings.Fields(fmt.Sprintf("curl -q -s --connect-timeout 2 --max-time 2 %s/hostname", net.JoinHostPort(targetIP, targetPort)))
	output, _, err = docker.Exec(containerID, nil, cmd...)
	framework.ExpectNoError(err)
	backendPodName := strings.TrimSpace(string(output))
	framework.Logf("Packet reached backend: %s", backendPodName)

	cmd = strings.Fields("ip neigh show " + targetIP)
	output, _, err = docker.Exec(containerID, nil, cmd...)
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
			if networkSettings.MacAddress == vipMac {
				vipNode = node.Name()
				break
			}
		}
	}

	framework.ExpectNotEmpty(vipNode, "Failed to find the node with MAC address: %s", vipMac)
	framework.Logf("Node with MAC address %s is %s", vipMac, vipNode)

	ginkgo.By("Checking the backend pod's host is same as the metallb vip's node")
	backendPod := f.PodClient().GetPod(backendPodName)
	backendPodNode := backendPod.Spec.NodeName
	framework.ExpectEqual(backendPodNode, vipNode)
}
