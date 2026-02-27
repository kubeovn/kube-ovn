package underlay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	kubeletevents "k8s.io/kubernetes/pkg/kubelet/events"
	kubeletserver "k8s.io/kubernetes/pkg/kubelet/server"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

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
	curlListenPort    = 8081
)

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

func waitSubnetStatusUpdate(subnetName string, subnetClient *framework.SubnetClient, expectedUsingIPs float64) {
	ginkgo.GinkgoHelper()

	ginkgo.By("Waiting for using ips count of subnet " + subnetName + " to be " + fmt.Sprintf("%.0f", expectedUsingIPs))
	framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
		subnet := subnetClient.Get(subnetName)
		if (subnet.Status.V4AvailableIPs != 0 && subnet.Status.V4UsingIPs != expectedUsingIPs) ||
			(subnet.Status.V6AvailableIPs != 0 && subnet.Status.V6UsingIPs != expectedUsingIPs) {
			framework.Logf("current subnet status: v4AvailableIPs = %.0f, v4UsingIPs = %.0f, v6AvailableIPs = %.0f, v6UsingIPs = %.0f",
				subnet.Status.V4AvailableIPs, subnet.Status.V4UsingIPs, subnet.Status.V6AvailableIPs, subnet.Status.V6UsingIPs)
			return false, nil
		}
		return true, nil
	}, fmt.Sprintf("using IPs count of subnet %s to be %.0f", subnetName, expectedUsingIPs))
}

func waitSubnetU2OStatus(f *framework.Framework, subnetName string, subnetClient *framework.SubnetClient, enableU2O bool) {
	ginkgo.GinkgoHelper()

	framework.WaitUntil(1*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
		subnet := subnetClient.Get(subnetName)
		if enableU2O {
			if !f.VersionPriorTo(1, 11) {
				if subnet.Status.U2OInterconnectionIP != "" && subnet.Status.U2OInterconnectionVPC != "" {
					framework.Logf("current enable U2O subnet status: U2OInterconnectionIP = %s, U2OInterconnectionVPC = %s",
						subnet.Status.U2OInterconnectionIP, subnet.Status.U2OInterconnectionVPC)
					return true, nil
				}
			} else {
				if subnet.Status.U2OInterconnectionIP != "" {
					framework.Logf("current enable U2O subnet status: U2OInterconnectionIP = %s",
						subnet.Status.U2OInterconnectionIP)
					return true, nil
				}
			}
			framework.Logf("keep waiting for U2O to be true: U2OInterconnectionIP = %s, U2OInterconnectionVPC = %s",
				subnet.Status.U2OInterconnectionIP, subnet.Status.U2OInterconnectionVPC)
		} else {
			if subnet.Status.U2OInterconnectionIP == "" && subnet.Status.U2OInterconnectionVPC == "" {
				return true, nil
			}
			framework.Logf("keep waiting for U2O to be false: U2OInterconnectionIP = %s, U2OInterconnectionVPC = %s",
				subnet.Status.U2OInterconnectionIP, subnet.Status.U2OInterconnectionVPC)
		}
		return false, nil
	}, fmt.Sprintf("U2OInterconnection status of subnet %s to be %v", subnetName, enableU2O))
}

var _ = framework.SerialDescribe("[group:underlay]", func() {
	f := framework.NewDefaultFramework("underlay")

	var skip bool
	var itFn func(bool)
	var cs clientset.Interface
	var nodeNames []string
	var clusterName, providerNetworkName, vlanName, subnetName, podName, namespaceName, netpolName string
	var vpcName string
	var u2oPodNameUnderlay, u2oOverlaySubnetName, u2oPodNameOverlay, u2oOverlaySubnetNameCustomVPC, u2oPodOverlayCustomVPC string
	var linkMap map[string]*iproute.Link
	var routeMap map[string][]iproute.Route
	var eventClient *framework.EventClient
	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient
	var vpcClient *framework.VpcClient
	var vlanClient *framework.VlanClient
	var providerNetworkClient *framework.ProviderNetworkClient
	var dockerNetwork *dockernetwork.Inspect
	var netpolClient *framework.NetworkPolicyClient
	var containerID string
	var conflictVlan1Name, conflictVlanSubnet1Name, conflictVlan2Name, conflictVlanSubnet2Name string

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		eventClient = f.EventClient()
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		vpcClient = f.VpcClient()
		vlanClient = f.VlanClient()
		providerNetworkClient = f.ProviderNetworkClient()
		netpolClient = f.NetworkPolicyClient()
		namespaceName = f.Namespace.Name
		podName = "pod-" + framework.RandomSuffix()
		u2oPodNameOverlay = "pod-" + framework.RandomSuffix()
		u2oPodNameUnderlay = "pod-" + framework.RandomSuffix()
		u2oPodOverlayCustomVPC = "pod-" + framework.RandomSuffix()
		subnetName = "subnet-" + framework.RandomSuffix()
		u2oOverlaySubnetName = "subnet-" + framework.RandomSuffix()
		u2oOverlaySubnetNameCustomVPC = "subnet-" + framework.RandomSuffix()
		vlanName = "vlan-" + framework.RandomSuffix()
		providerNetworkName = "pn-" + framework.RandomSuffix()
		vpcName = "vpc-" + framework.RandomSuffix()
		netpolName = "netpol-" + framework.RandomSuffix()
		containerID = ""
		conflictVlan1Name = "conflict-vlan1-" + framework.RandomSuffix()
		conflictVlanSubnet1Name = "conflict-vlan-subnet1-" + framework.RandomSuffix()
		conflictVlan2Name = "conflict-vlan2-" + framework.RandomSuffix()
		conflictVlanSubnet2Name = "conflict-vlan-subnet2-" + framework.RandomSuffix()

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
		routeMap = make(map[string][]iproute.Route, len(nodes))
		nodeNames = make([]string, 0, len(nodes))
		for _, node := range nodes {
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
		}

		itFn = func(exchangeLinkName bool) {
			ginkgo.GinkgoHelper()

			ginkgo.By("Creating provider network " + providerNetworkName)
			pn := makeProviderNetwork(providerNetworkName, exchangeLinkName, linkMap)
			pn = providerNetworkClient.CreateSync(pn)

			ginkgo.By("Getting k8s nodes")
			k8sNodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
			framework.ExpectNoError(err)

			ginkgo.By("Validating node labels")
			for _, node := range k8sNodes.Items {
				link := linkMap[node.Name]
				framework.ExpectHaveKeyWithValue(node.Labels, fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, providerNetworkName), link.IfName)
				framework.ExpectHaveKeyWithValue(node.Labels, fmt.Sprintf(util.ProviderNetworkReadyTemplate, providerNetworkName), "true")
				framework.ExpectHaveKeyWithValue(node.Labels, fmt.Sprintf(util.ProviderNetworkMtuTemplate, providerNetworkName), strconv.Itoa(link.Mtu))
				framework.ExpectNotHaveKey(node.Labels, fmt.Sprintf(util.ProviderNetworkExcludeTemplate, providerNetworkName))
			}

			ginkgo.By("Validating provider network status")
			framework.ExpectEqual(pn.Status.Ready, true, "field .status.ready should be true")
			framework.ExpectConsistOf(pn.Status.ReadyNodes, nodeNames)
			framework.ExpectEmpty(pn.Status.Vlans)

			ginkgo.By("Getting kind nodes")
			kindNodes, err := kind.ListNodes(clusterName, "")
			framework.ExpectNoError(err)

			ginkgo.By("Validating node links")
			linkNameMap := make(map[string]string, len(kindNodes))
			bridgeName := util.ExternalBridgeName(providerNetworkName)
			for _, node := range kindNodes {
				if exchangeLinkName {
					bridgeName = linkMap[node.ID].IfName
				}

				links, err := node.ListLinks()
				framework.ExpectNoError(err, "failed to list links on node %s: %v", node.Name(), err)

				var port, bridge *iproute.Link
				for i, link := range links {
					if link.IfIndex == linkMap[node.ID].IfIndex {
						port = &links[i]
					} else if link.IfName == bridgeName {
						bridge = &links[i]
					}
					if port != nil && bridge != nil {
						break
					}
				}
				framework.ExpectNotNil(port)
				framework.ExpectEqual(port.Address, linkMap[node.ID].Address)
				framework.ExpectEqual(port.Mtu, linkMap[node.ID].Mtu)
				framework.ExpectEqual(port.Master, "ovs-system")
				framework.ExpectEqual(port.OperState, "UP")
				if exchangeLinkName {
					framework.ExpectEqual(port.IfName, util.ExternalBridgeName(providerNetworkName))
				}

				framework.ExpectNotNil(bridge)
				framework.ExpectEqual(bridge.LinkInfo.InfoKind, "openvswitch")
				framework.ExpectEqual(bridge.Address, port.Address)
				framework.ExpectEqual(bridge.Mtu, port.Mtu)
				framework.ExpectEqual(bridge.OperState, "UNKNOWN")
				framework.ExpectContainElement(bridge.Flags, "UP")

				framework.ExpectEmpty(port.NonLinkLocalAddresses())
				framework.ExpectConsistOf(bridge.NonLinkLocalAddresses(), linkMap[node.ID].NonLinkLocalAddresses())

				linkNameMap[node.ID] = port.IfName
			}

			ginkgo.By("Validating node routes")
			for _, node := range kindNodes {
				if exchangeLinkName {
					bridgeName = linkMap[node.ID].IfName
				}

				routes, err := node.ListRoutes(true)
				framework.ExpectNoError(err, "failed to list routes on node %s: %v", node.Name(), err)

				var portRoutes, bridgeRoutes []iproute.Route
				for _, route := range routes {
					r := iproute.Route{
						Dst:     route.Dst,
						Gateway: route.Gateway,
						Dev:     route.Dev,
						Flags:   route.Flags,
					}
					switch route.Dev {
					case linkNameMap[node.ID]:
						portRoutes = append(portRoutes, r)
					case bridgeName:
						r.Dev = linkMap[node.ID].IfName
						bridgeRoutes = append(bridgeRoutes, r)
					}
				}

				framework.ExpectEmpty(portRoutes, "no routes should exists on provider link")
				framework.ExpectConsistOf(bridgeRoutes, routeMap[node.ID])
			}
		}
	})
	ginkgo.AfterEach(func() {
		if containerID != "" {
			ginkgo.By("Deleting container " + containerID)
			err := docker.ContainerRemove(containerID)
			framework.ExpectNoError(err)
		}

		ginkgo.By("Deleting network policy " + netpolName)
		netpolClient.DeleteSync(netpolName)

		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)

		ginkgo.By("Deleting pod " + u2oPodNameUnderlay)
		podClient.DeleteSync(u2oPodNameUnderlay)

		ginkgo.By("Deleting pod " + u2oPodNameOverlay)
		podClient.DeleteSync(u2oPodNameOverlay)

		ginkgo.By("Deleting pod " + u2oPodOverlayCustomVPC)
		podClient.DeleteSync(u2oPodOverlayCustomVPC)

		ginkgo.By("Deleting subnet " + u2oOverlaySubnetNameCustomVPC)
		subnetClient.DeleteSync(u2oOverlaySubnetNameCustomVPC)

		ginkgo.By("Deleting subnet " + u2oOverlaySubnetName)
		subnetClient.DeleteSync(u2oOverlaySubnetName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)

		ginkgo.By("Deleting vpc " + vpcName)
		vpcClient.DeleteSync(vpcName)

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
	})

	framework.ConformanceIt(`should be able to create provider network`, func() {
		itFn(false)
	})

	framework.ConformanceIt(`should exchange link names`, func() {
		f.SkipVersionPriorTo(1, 9, "Support for exchanging link names was introduced in v1.9")

		itFn(true)
	})

	framework.ConformanceIt("should keep pod mtu the same with node interface", func() {
		ginkgo.By("Creating provider network " + providerNetworkName)
		pn := makeProviderNetwork(providerNetworkName, false, linkMap)
		_ = providerNetworkClient.CreateSync(pn)

		ginkgo.By("Getting docker network " + dockerNetworkName)
		network, err := docker.NetworkInspect(dockerNetworkName)
		framework.ExpectNoError(err, "getting docker network "+dockerNetworkName)

		ginkgo.By("Creating vlan " + vlanName)
		vlan := framework.MakeVlan(vlanName, providerNetworkName, 0)
		_ = vlanClient.Create(vlan)

		ginkgo.By("Creating subnet " + subnetName)
		var cidrV4, cidrV6, gatewayV4, gatewayV6 string
		for _, config := range dockerNetwork.IPAM.Config {
			switch util.CheckProtocol(config.Subnet.String()) {
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
		subnet := framework.MakeSubnet(subnetName, vlanName, strings.Join(cidr, ","), strings.Join(gateway, ","), "", "", excludeIPs, nil, []string{namespaceName})
		_ = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating pod " + podName)
		cmd := []string{"sh", "-c", "sleep 600"}
		pod := framework.MakePrivilegedPod(namespaceName, podName, nil, nil, f.KubeOVNImage, cmd, nil)
		_ = podClient.CreateSync(pod)

		ginkgo.By("Validating pod MTU")
		links, err := iproute.AddressShow("eth0", func(cmd ...string) ([]byte, []byte, error) {
			return framework.KubectlExec(namespaceName, podName, cmd...)
		})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(links, 1, "should get eth0 information")
		framework.ExpectEqual(links[0].Mtu, docker.MTU)
	})

	framework.ConformanceIt("should be able to detect duplicate address", func() {
		f.SkipVersionPriorTo(1, 9, "Duplicate address detection was introduced in v1.9")
		if !f.HasIPv4() {
			f.SkipVersionPriorTo(1, 14, "Duplicate address detection for IPv6 was introduced in v1.14")
		}

		ginkgo.By("Creating provider network " + providerNetworkName)
		pn := makeProviderNetwork(providerNetworkName, false, linkMap)
		_ = providerNetworkClient.CreateSync(pn)

		ginkgo.By("Getting docker network " + dockerNetworkName)
		network, err := docker.NetworkInspect(dockerNetworkName)
		framework.ExpectNoError(err, "getting docker network "+dockerNetworkName)

		containerName := "container-" + framework.RandomSuffix()
		ginkgo.By("Creating container " + containerName)
		cmd := []string{"sleep", "infinity"}
		containerInfo, err := docker.ContainerCreate(containerName, f.KubeOVNImage, dockerNetworkName, cmd)
		framework.ExpectNoError(err)
		containerID = containerInfo.ID

		ginkgo.By("Creating vlan " + vlanName)
		vlan := framework.MakeVlan(vlanName, providerNetworkName, 0)
		_ = vlanClient.Create(vlan)

		ginkgo.By("Creating subnet " + subnetName)
		var cidrV4, cidrV6, gatewayV4, gatewayV6 string
		for _, config := range dockerNetwork.IPAM.Config {
			switch util.CheckProtocol(config.Subnet.String()) {
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
			if f.HasIPv4() && container.IPv4Address.IsValid() {
				excludeIPs = append(excludeIPs, container.IPv4Address.Addr().String())
			}
			if f.HasIPv6() && container.IPv6Address.IsValid() {
				excludeIPs = append(excludeIPs, container.IPv6Address.Addr().String())
			}
		}
		subnet := framework.MakeSubnet(subnetName, vlanName, strings.Join(cidr, ","), strings.Join(gateway, ","), "", "", excludeIPs, nil, []string{namespaceName})
		_ = subnetClient.CreateSync(subnet)

		networkInfo := containerInfo.NetworkSettings.Networks[dockerNetworkName]
		ips := make([]string, 0, 2)
		if f.HasIPv4() {
			ips = append(ips, networkInfo.IPAddress.String())
		}
		if f.HasIPv6() {
			ips = append(ips, networkInfo.GlobalIPv6Address.String())
		}
		ip := strings.Join(ips, ",")
		mac := networkInfo.MacAddress
		ginkgo.By("Creating pod " + podName + " with IP address " + ip)
		annotations := map[string]string{util.IPAddressAnnotation: ip}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, "", nil, nil)
		pod.Spec.TerminationGracePeriodSeconds = nil
		_ = podClient.Create(pod)

		ginkgo.By("Waiting for pod events")
		events := eventClient.WaitToHaveEvent(util.KindPod, podName, corev1.EventTypeWarning, kubeletevents.FailedCreatePodSandBox, kubeletserver.ComponentKubelet, "")
		ip = networkInfo.IPAddress.String()
		if f.IsIPv6() {
			ip = networkInfo.GlobalIPv6Address.String()
		}
		message := fmt.Sprintf("IP address %s has already been used by host with MAC %s", ip, mac)
		var found bool
		for _, event := range events {
			if strings.Contains(event.Message, message) {
				found = true
				framework.Logf("Found pod event: %s", event.Message)
				break
			}
		}
		framework.ExpectTrue(found, "Address conflict should be reported in pod events")
	})

	framework.ConformanceIt("should support underlay to overlay subnet interconnection", func() {
		f.SkipVersionPriorTo(1, 9, "This feature was introduced in v1.9")

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
			switch util.CheckProtocol(config.Subnet.String()) {
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
		underlayCidr := make([]string, 0, 2)
		gateway := make([]string, 0, 2)
		if f.HasIPv4() {
			underlayCidr = append(underlayCidr, cidrV4)
			gateway = append(gateway, gatewayV4)
		}
		if f.HasIPv6() {
			underlayCidr = append(underlayCidr, cidrV6)
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

		ginkgo.By("Creating underlay subnet " + subnetName)
		subnet := framework.MakeSubnet(subnetName, vlanName, strings.Join(underlayCidr, ","), strings.Join(gateway, ","), "", "", excludeIPs, nil, []string{namespaceName})
		subnet.Spec.U2OInterconnection = true
		// only ipv4 needs to verify that the gateway address is consistent with U2OInterconnectionIP when enabling DHCP and U2O
		if f.HasIPv4() {
			subnet.Spec.EnableDHCP = true
		}
		_ = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating underlay pod " + u2oPodNameUnderlay)
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		args := []string{"netexec", "--http-port", strconv.Itoa(curlListenPort)}
		originUnderlayPod := framework.MakePod(namespaceName, u2oPodNameUnderlay, nil, annotations, framework.AgnhostImage, nil, args)
		underlayPod := podClient.CreateSync(originUnderlayPod)
		waitSubnetStatusUpdate(subnetName, subnetClient, 2)

		ginkgo.By("Creating overlay subnet " + u2oOverlaySubnetName)
		cidr := framework.RandomCIDR(f.ClusterIPFamily)
		overlaySubnet := framework.MakeSubnet(u2oOverlaySubnetName, "", cidr, "", "", "", nil, nil, nil)
		overlaySubnet = subnetClient.CreateSync(overlaySubnet)

		ginkgo.By("Creating overlay pod " + u2oPodNameOverlay)
		overlayAnnotations := map[string]string{
			util.LogicalSwitchAnnotation: overlaySubnet.Name,
		}
		args = []string{"netexec", "--http-port", strconv.Itoa(curlListenPort)}
		overlayPod := framework.MakePod(namespaceName, u2oPodNameOverlay, nil, overlayAnnotations, framework.AgnhostImage, nil, args)
		overlayPod = podClient.CreateSync(overlayPod)

		ginkgo.By("step1: Enable u2o check")
		subnet = subnetClient.Get(subnetName)
		ginkgo.By("1. waiting for U2OInterconnection status of subnet " + subnetName + " to be true")
		waitSubnetU2OStatus(f, subnetName, subnetClient, true)
		checkU2OItems(f, subnet, underlayPod, overlayPod, false)

		ginkgo.By("step2: Disable u2o check")

		ginkgo.By("Deleting underlay pod " + u2oPodNameUnderlay)
		podClient.DeleteSync(u2oPodNameUnderlay)

		ginkgo.By("Turning off U2OInterconnection of subnet " + subnetName)
		subnet = subnetClient.Get(subnetName)
		modifiedSubnet := subnet.DeepCopy()
		modifiedSubnet.Spec.U2OInterconnection = false
		subnetClient.PatchSync(subnet, modifiedSubnet)

		ginkgo.By("Creating underlay pod " + u2oPodNameUnderlay)
		underlayPod = podClient.CreateSync(originUnderlayPod)
		waitSubnetStatusUpdate(subnetName, subnetClient, 1)

		subnet = subnetClient.Get(subnetName)
		ginkgo.By("2. waiting for U2OInterconnection status of subnet " + subnetName + " to be false")
		waitSubnetU2OStatus(f, subnetName, subnetClient, false)
		checkU2OItems(f, subnet, underlayPod, overlayPod, false)

		ginkgo.By("step3: Recover enable u2o check")

		ginkgo.By("Deleting underlay pod " + u2oPodNameUnderlay)
		podClient.DeleteSync(u2oPodNameUnderlay)

		ginkgo.By("Turning on U2OInterconnection of subnet " + subnetName)
		subnet = subnetClient.Get(subnetName)
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.U2OInterconnection = true
		subnetClient.PatchSync(subnet, modifiedSubnet)

		ginkgo.By("Creating underlay pod " + u2oPodNameUnderlay)
		underlayPod = podClient.CreateSync(originUnderlayPod)
		waitSubnetStatusUpdate(subnetName, subnetClient, 2)

		subnet = subnetClient.Get(subnetName)
		ginkgo.By("3. waiting for U2OInterconnection status of subnet " + subnetName + " to be true")
		waitSubnetU2OStatus(f, subnetName, subnetClient, true)
		checkU2OItems(f, subnet, underlayPod, overlayPod, false)

		ginkgo.By("step4: Check if kube-ovn-controller restart")

		ginkgo.By("Restarting kube-ovn-controller")
		deployClient := f.DeploymentClientNS(framework.KubeOvnNamespace)
		deploy := deployClient.Get("kube-ovn-controller")
		deployClient.RestartSync(deploy)

		subnet = subnetClient.Get(subnetName)
		ginkgo.By("4. waiting for U2OInterconnection status of subnet " + subnetName + " to be true")
		waitSubnetU2OStatus(f, subnetName, subnetClient, true)
		checkU2OItems(f, subnet, underlayPod, overlayPod, false)

		ginkgo.By("step5: Disable u2o check after restart kube-controller")

		ginkgo.By("Deleting underlay pod " + u2oPodNameUnderlay)
		podClient.DeleteSync(u2oPodNameUnderlay)

		ginkgo.By("Turning off U2OInterconnection of subnet " + subnetName)
		subnet = subnetClient.Get(subnetName)
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.U2OInterconnection = false
		subnetClient.PatchSync(subnet, modifiedSubnet)

		ginkgo.By("Creating underlay pod " + u2oPodNameUnderlay)
		underlayPod = podClient.CreateSync(originUnderlayPod)
		waitSubnetStatusUpdate(subnetName, subnetClient, 1)

		subnet = subnetClient.Get(subnetName)
		ginkgo.By("5. waiting for U2OInterconnection status of subnet " + subnetName + " to be false")
		waitSubnetU2OStatus(f, subnetName, subnetClient, false)
		checkU2OItems(f, subnet, underlayPod, overlayPod, false)

		ginkgo.By("step6: Recover enable u2o check after restart kube-ovn-controller")

		ginkgo.By("Deleting underlay pod " + u2oPodNameUnderlay)
		podClient.DeleteSync(u2oPodNameUnderlay)

		ginkgo.By("Turning on U2OInterconnection of subnet " + subnetName)
		subnet = subnetClient.Get(subnetName)
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.U2OInterconnection = true
		subnetClient.PatchSync(subnet, modifiedSubnet)

		ginkgo.By("Creating underlay pod " + u2oPodNameUnderlay)
		underlayPod = podClient.CreateSync(originUnderlayPod)
		waitSubnetStatusUpdate(subnetName, subnetClient, 2)

		subnet = subnetClient.Get(subnetName)
		ginkgo.By("6. waiting for U2OInterconnection status of subnet " + subnetName + " to be true")
		waitSubnetU2OStatus(f, subnetName, subnetClient, true)
		checkU2OItems(f, subnet, underlayPod, overlayPod, false)

		if f.VersionPriorTo(1, 9) {
			return
		}

		ginkgo.By("step7: Specify u2oInterconnectionIP")

		// change u2o interconnection ip twice
		for index := range 2 {
			getAvailableIPs := func(subnet *apiv1.Subnet) string {
				var availIPs []string
				v4Cidr, v6Cidr := util.SplitStringIP(subnet.Spec.CIDRBlock)
				if v4Cidr != "" {
					startIP := strings.Split(v4Cidr, "/")[0]
					ip, _ := ipam.NewIP(startIP)
					availIPs = append(availIPs, ip.Add(100+int64(index)).String())
				}
				if v6Cidr != "" {
					startIP := strings.Split(v6Cidr, "/")[0]
					ip, _ := ipam.NewIP(startIP)
					availIPs = append(availIPs, ip.Add(100+int64(index)).String())
				}
				return strings.Join(availIPs, ",")
			}

			subnet = subnetClient.Get(subnetName)
			u2oIP := getAvailableIPs(subnet)
			ginkgo.By("Setting U2OInterconnectionIP to " + u2oIP + " for subnet " + subnetName)
			modifiedSubnet = subnet.DeepCopy()
			modifiedSubnet.Spec.U2OInterconnectionIP = u2oIP
			modifiedSubnet.Spec.U2OInterconnection = true
			subnetClient.PatchSync(subnet, modifiedSubnet)

			ginkgo.By("Deleting underlay pod " + u2oPodNameUnderlay)
			podClient.DeleteSync(u2oPodNameUnderlay)

			ginkgo.By("Creating underlay pod " + u2oPodNameUnderlay)
			underlayPod = podClient.CreateSync(originUnderlayPod)
			waitSubnetStatusUpdate(subnetName, subnetClient, 2)

			subnet = subnetClient.Get(subnetName)
			ginkgo.By("7. waiting for U2OInterconnection status of subnet " + subnetName + " to be true")
			waitSubnetU2OStatus(f, subnetName, subnetClient, true)
			checkU2OItems(f, subnet, underlayPod, overlayPod, false)
		}

		if f.VersionPriorTo(1, 11) {
			return
		}

		ginkgo.By("step8: Change underlay subnet interconnection to overlay subnet in custom vpc")

		ginkgo.By("Deleting underlay pod " + u2oPodNameUnderlay)
		podClient.DeleteSync(u2oPodNameUnderlay)

		ginkgo.By("Creating VPC " + vpcName)
		customVPC := framework.MakeVpc(vpcName, "", false, false, []string{namespaceName})
		vpcClient.CreateSync(customVPC)

		ginkgo.By("Creating subnet " + u2oOverlaySubnetNameCustomVPC)
		cidr = framework.RandomCIDR(f.ClusterIPFamily)
		overlaySubnetCustomVpc := framework.MakeSubnet(u2oOverlaySubnetNameCustomVPC, "", cidr, "", vpcName, "", nil, nil, []string{namespaceName})
		_ = subnetClient.CreateSync(overlaySubnetCustomVpc)

		ginkgo.By("Creating overlay pod " + u2oPodOverlayCustomVPC)
		args = []string{"netexec", "--http-port", strconv.Itoa(curlListenPort)}
		u2oPodOverlayCustomVPCAnnotations := map[string]string{
			util.LogicalSwitchAnnotation: u2oOverlaySubnetNameCustomVPC,
		}
		podOverlayCustomVPC := framework.MakePod(namespaceName, u2oPodOverlayCustomVPC, nil, u2oPodOverlayCustomVPCAnnotations, framework.AgnhostImage, nil, args)
		podOverlayCustomVPC = podClient.CreateSync(podOverlayCustomVPC)

		ginkgo.By("Turning on U2OInterconnection and set VPC to " + vpcName + " for subnet " + subnetName)
		subnet = subnetClient.Get(subnetName)
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.Vpc = vpcName
		modifiedSubnet.Spec.U2OInterconnection = true
		subnetClient.PatchSync(subnet, modifiedSubnet)

		ginkgo.By("Creating underlay pod " + u2oPodNameUnderlay)
		underlayPod = podClient.CreateSync(originUnderlayPod)
		waitSubnetStatusUpdate(subnetName, subnetClient, 2)

		subnet = subnetClient.Get(subnetName)
		ginkgo.By("8. waiting for U2OInterconnection status of subnet " + subnetName + " to be true")
		waitSubnetU2OStatus(f, subnetName, subnetClient, true)
		checkU2OItems(f, subnet, underlayPod, podOverlayCustomVPC, true)

		ginkgo.By("step9: Change underlay subnet interconnection to overlay subnet in default vpc")

		ginkgo.By("Deleting underlay pod " + u2oPodNameUnderlay)
		podClient.DeleteSync(u2oPodNameUnderlay)

		ginkgo.By("Setting VPC to " + util.DefaultVpc + " for subnet " + subnetName)
		subnet = subnetClient.Get(subnetName)
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.Vpc = util.DefaultVpc
		modifiedSubnet.Spec.Namespaces = nil
		subnetClient.PatchSync(subnet, modifiedSubnet)

		ginkgo.By("Creating underlay pod " + u2oPodNameUnderlay)
		underlayPod = podClient.CreateSync(originUnderlayPod)
		waitSubnetStatusUpdate(subnetName, subnetClient, 2)

		subnet = subnetClient.Get(subnetName)
		ginkgo.By("9. waiting for U2OInterconnection status of subnet " + subnetName + " to be true")
		waitSubnetU2OStatus(f, subnetName, subnetClient, true)
		checkU2OItems(f, subnet, underlayPod, overlayPod, false)

		ginkgo.By("step10: Disable u2o")

		ginkgo.By("Deleting underlay pod " + u2oPodNameUnderlay)
		podClient.DeleteSync(u2oPodNameUnderlay)

		ginkgo.By("Turning off U2OInterconnection of subnet " + subnetName)
		subnet = subnetClient.Get(subnetName)
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.U2OInterconnection = false
		subnetClient.PatchSync(subnet, modifiedSubnet)

		ginkgo.By("Creating underlay pod " + u2oPodNameUnderlay)
		underlayPod = podClient.CreateSync(originUnderlayPod)
		waitSubnetStatusUpdate(subnetName, subnetClient, 1)

		subnet = subnetClient.Get(subnetName)
		ginkgo.By("10. waiting for U2OInterconnection status of subnet " + subnetName + " to be false")
		waitSubnetU2OStatus(f, subnetName, subnetClient, false)
		checkU2OItems(f, subnet, underlayPod, overlayPod, false)
	})

	framework.ConformanceIt(`should drop ARP/ND request from localnet port to LRP`, func() {
		f.SkipVersionPriorTo(1, 9, "This feature was introduced in v1.9")

		ginkgo.By("Creating provider network " + providerNetworkName)
		pn := makeProviderNetwork(providerNetworkName, false, linkMap)
		_ = providerNetworkClient.CreateSync(pn)

		containerName := "container-" + framework.RandomSuffix()
		ginkgo.By("Creating container " + containerName)
		cmd := []string{"sh", "-c", "sleep 600"}
		containerInfo, err := docker.ContainerCreate(containerName, f.KubeOVNImage, dockerNetworkName, cmd)
		framework.ExpectNoError(err)
		containerID = containerInfo.ID

		ginkgo.By("Getting docker network " + dockerNetworkName)
		network, err := docker.NetworkInspect(dockerNetworkName)
		framework.ExpectNoError(err, "getting docker network "+dockerNetworkName)

		ginkgo.By("Creating vlan " + vlanName)
		vlan := framework.MakeVlan(vlanName, providerNetworkName, 0)
		_ = vlanClient.Create(vlan)

		ginkgo.By("Creating underlay subnet " + subnetName)
		var cidrV4, cidrV6, gatewayV4, gatewayV6 string
		for _, config := range dockerNetwork.IPAM.Config {
			switch util.CheckProtocol(config.Subnet.String()) {
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
		underlayCidr := make([]string, 0, 2)
		gateway := make([]string, 0, 2)
		if f.HasIPv4() {
			underlayCidr = append(underlayCidr, cidrV4)
			gateway = append(gateway, gatewayV4)
		}
		if f.HasIPv6() {
			underlayCidr = append(underlayCidr, cidrV6)
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

		ginkgo.By("Creating subnet " + subnetName + " with u2o interconnection enabled")
		subnet := framework.MakeSubnet(subnetName, vlanName, strings.Join(underlayCidr, ","), strings.Join(gateway, ","), "", "", excludeIPs, nil, []string{namespaceName})
		subnet.Spec.U2OInterconnection = true
		subnet = subnetClient.CreateSync(subnet)

		lrpIPv4, lrpIPv6 := util.SplitStringIP(subnet.Status.U2OInterconnectionIP)
		if f.HasIPv4() {
			if f.VersionPriorTo(1, 13) {
				err := checkU2OFilterOpenFlowExist(clusterName, pn, subnet, true)
				framework.ExpectNoError(err)
			} else {
				ginkgo.By("Sending ARP request to " + lrpIPv4 + " from container " + containerName)
				cmd := strings.Fields("arping -c1 -w1 " + lrpIPv4)
				_, _, err = docker.Exec(containerID, nil, cmd...)
				framework.ExpectError(err)
				framework.ExpectEqual(err, docker.ErrNonZeroExitCode{Cmd: cmd, ExitCode: 1})
			}
		} else {
			framework.ExpectEmpty(lrpIPv4)
		}
		if f.HasIPv6() {
			if f.VersionPriorTo(1, 13) {
				err := checkU2OFilterOpenFlowExist(clusterName, pn, subnet, true)
				framework.ExpectNoError(err)
			} else {
				ginkgo.By("Sending ND NS request to " + lrpIPv6 + " from container " + containerName)
				cmd := strings.Fields("ndisc6 -1 -n -r1 -w1000 " + lrpIPv6 + " eth0")
				_, _, err = docker.Exec(containerID, nil, cmd...)
				framework.ExpectError(err)
				framework.ExpectEqual(err, docker.ErrNonZeroExitCode{Cmd: cmd, ExitCode: 2})
			}
		} else {
			framework.ExpectEmpty(lrpIPv6)
		}
	})

	framework.ConformanceIt(`should support IPv6 connectivity to node IPs when pod start immediately`, func() {
		if !f.HasIPv6() {
			ginkgo.Skip("This test requires IPv6 support")
		}

		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12")
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
			switch util.CheckProtocol(config.Subnet.String()) {
			case apiv1.ProtocolIPv4:
				if f.HasIPv4() {
					cidrV4 = config.Subnet.String()
					gatewayV4 = config.Gateway.String()
				}
			case apiv1.ProtocolIPv6:
				cidrV6 = config.Subnet.String()
				gatewayV6 = config.Gateway.String()
			}
		}

		underlayCidr := make([]string, 0, 2)
		gateway := make([]string, 0, 2)
		if f.HasIPv4() {
			underlayCidr = append(underlayCidr, cidrV4)
			gateway = append(gateway, gatewayV4)
		}
		underlayCidr = append(underlayCidr, cidrV6)
		gateway = append(gateway, gatewayV6)

		excludeIPs := make([]string, 0, len(network.Containers)*2)
		for _, container := range network.Containers {
			if container.IPv4Address.IsValid() && f.HasIPv4() {
				excludeIPs = append(excludeIPs, container.IPv4Address.Addr().String())
			}
			if container.IPv6Address.IsValid() {
				excludeIPs = append(excludeIPs, container.IPv6Address.Addr().String())
			}
		}

		ginkgo.By("Creating subnet " + subnetName)
		subnet := framework.MakeSubnet(subnetName, vlanName, strings.Join(underlayCidr, ","), strings.Join(gateway, ","), "", "", excludeIPs, nil, []string{namespaceName})
		_ = subnetClient.CreateSync(subnet)

		ginkgo.By("Getting node IPv6 addresses")
		nodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err, "getting nodes in kind cluster")

		// Find a node with IPv6 address
		var nodeIPv6 string
		var selectedNode kind.Node
		for _, n := range nodes {
			for _, link := range linkMap {
				for _, addr := range link.NonLinkLocalAddresses() {
					if util.CheckProtocol(addr) == apiv1.ProtocolIPv6 {
						nodeIPv6 = addr
						selectedNode = n
						break
					}
				}
				if nodeIPv6 != "" {
					break
				}
			}
			if nodeIPv6 != "" {
				break
			}
		}
		framework.ExpectNotEmpty(nodeIPv6, "No node with IPv6 address found")
		ipv6Addr := strings.Split(nodeIPv6, "/")[0]

		ginkgo.By(fmt.Sprintf("Creating pod %s that pings IPv6 node IP %s on node %s", podName, ipv6Addr, selectedNode.Name()))
		// Use ping6 with one attempt and 1s timeout, checking the return code
		pingCmd := []string{"sh", "-c", fmt.Sprintf("ping6 -c 1 -w 1 %s && sleep 600 || exit $?", ipv6Addr)}
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}

		pod := framework.MakePod(namespaceName, podName, nil, annotations, framework.AgnhostImage, pingCmd, nil)
		pod = podClient.CreateSync(pod)

		ginkgo.By("If pod is running, the ping was successful")
		framework.ExpectEqual(pod.Status.Phase, corev1.PodRunning, "Pod should be in Running state, indicating successful ping with 1s timeout")
	})

	framework.ConformanceIt("should be able to detect conflict vlan subnet", func() {
		f.SkipVersionPriorTo(1, 14, "Conflict vlan detection was introduced in v1.14")

		ginkgo.By("Creating provider network " + providerNetworkName)
		pn := makeProviderNetwork(providerNetworkName, false, linkMap)
		_ = providerNetworkClient.CreateSync(pn)

		ginkgo.By("Creating conflict vlan subnet1 " + conflictVlanSubnet1Name)
		conflictVlan1 := framework.MakeVlan(conflictVlan1Name, providerNetworkName, 100)
		_ = vlanClient.Create(conflictVlan1)

		cidr1 := framework.RandomCIDR(f.ClusterIPFamily)
		conflictVlanSubnet1 := framework.MakeSubnet(conflictVlanSubnet1Name, conflictVlan1Name, cidr1, "", "", "", nil, nil, []string{namespaceName})
		_ = subnetClient.CreateSync(conflictVlanSubnet1)

		// create a second VLAN with the same ID
		ginkgo.By("Creating conflict vlan subnet2 " + conflictVlanSubnet2Name)
		// wait for conflictVlan1 to be processed by the controller before creating the conflicting vlan
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			v := vlanClient.Get(conflictVlan1Name)
			return !v.Status.Conflict, nil
		}, fmt.Sprintf("vlan %s should be processed and non-conflicting", conflictVlan1Name))

		conflictVlan2 := framework.MakeVlan(conflictVlan2Name, providerNetworkName, 100)
		_ = vlanClient.Create(conflictVlan2)

		cidr2 := framework.RandomCIDR(f.ClusterIPFamily)
		conflictVlanSubnet2 := framework.MakeSubnet(conflictVlanSubnet2Name, conflictVlan2Name, cidr2, "", "", "", nil, nil, []string{namespaceName})
		_ = subnetClient.Create(conflictVlanSubnet2)
		// wait for the controller to detect the conflict
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			v := vlanClient.Get(conflictVlan2Name)
			return v.Status.Conflict, nil
		}, fmt.Sprintf("vlan %s should be detected as conflicting", conflictVlan2Name))

		// check
		conflictVlan1 = vlanClient.Get(conflictVlan1Name)
		conflictVlanSubnet1 = subnetClient.Get(conflictVlanSubnet1Name)
		conflictVlan2 = vlanClient.Get(conflictVlan2Name)
		conflictVlanSubnet2 = subnetClient.Get(conflictVlanSubnet2Name)
		framework.ExpectFalse(conflictVlan1.Status.Conflict)
		// new vlan should be conflict
		framework.ExpectTrue(conflictVlan2.Status.Conflict)
		if f.HasIPv4() {
			framework.ExpectNotEmpty(conflictVlanSubnet1.Status.V4AvailableIPRange)
			// new conflict vlan subnet should not have available ip
			framework.ExpectEmpty(conflictVlanSubnet2.Status.V4AvailableIPRange)
		}
		if f.HasIPv6() {
			framework.ExpectNotEmpty(conflictVlanSubnet1.Status.V6AvailableIPRange)
			framework.ExpectEmpty(conflictVlanSubnet2.Status.V6AvailableIPRange)
		}
	})

	framework.ConformanceIt("should support nodeSelector to include only specific nodes", func() {
		f.SkipVersionPriorTo(1, 15, "This feature was introduced in v1.15")

		ginkgo.By("Getting k8s nodes")
		k8sNodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(k8sNodes.Items)

		// Select the first node for inclusion
		selectedNodeName := k8sNodes.Items[0].Name
		testLabelKey := "provider-network-test"
		testLabelValue := "selected"

		ginkgo.By("Adding test label to selected node " + selectedNodeName)
		selectedNode := &k8sNodes.Items[0]
		if selectedNode.Labels == nil {
			selectedNode.Labels = make(map[string]string)
		}
		selectedNode.Labels[testLabelKey] = testLabelValue
		_, err = cs.CoreV1().Nodes().Update(context.Background(), selectedNode, metav1.UpdateOptions{})
		framework.ExpectNoError(err)

		ginkgo.By("Creating provider network with nodeSelector " + providerNetworkName)
		pn := makeProviderNetwork(providerNetworkName, false, linkMap)
		pn.Spec.NodeSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{
				testLabelKey: testLabelValue,
			},
		}
		pn = providerNetworkClient.CreateSync(pn)

		ginkgo.By("Getting updated k8s nodes")
		updatedNodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)

		var selectedUpdatedNode, nonSelectedUpdatedNode *corev1.Node
		for i := range updatedNodes.Items {
			if updatedNodes.Items[i].Name == selectedNodeName {
				selectedUpdatedNode = &updatedNodes.Items[i]
			} else {
				nonSelectedUpdatedNode = &updatedNodes.Items[i]
				break // Take the first non-selected node for verification
			}
		}
		framework.ExpectNotNil(selectedUpdatedNode, "Selected node should be found")
		framework.ExpectNotNil(nonSelectedUpdatedNode, "At least one non-selected node should exist")

		ginkgo.By("Validating that only selected node has ready annotation")
		framework.ExpectHaveKeyWithValue(selectedUpdatedNode.Labels, fmt.Sprintf(util.ProviderNetworkReadyTemplate, providerNetworkName), "true")
		framework.ExpectHaveKeyWithValue(selectedUpdatedNode.Labels, fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, providerNetworkName), linkMap[selectedNodeName].IfName)
		framework.ExpectHaveKeyWithValue(selectedUpdatedNode.Labels, fmt.Sprintf(util.ProviderNetworkMtuTemplate, providerNetworkName), strconv.Itoa(linkMap[selectedNodeName].Mtu))

		ginkgo.By("Validating that non-selected node does not have ready annotation")
		framework.ExpectNotHaveKey(nonSelectedUpdatedNode.Labels, fmt.Sprintf(util.ProviderNetworkReadyTemplate, providerNetworkName))
		framework.ExpectNotHaveKey(nonSelectedUpdatedNode.Labels, fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, providerNetworkName))
		framework.ExpectNotHaveKey(nonSelectedUpdatedNode.Labels, fmt.Sprintf(util.ProviderNetworkMtuTemplate, providerNetworkName))

		ginkgo.By("Validating provider network status")
		framework.ExpectEqual(pn.Status.Ready, true, "field .status.ready should be true")
		framework.ExpectConsistOf(pn.Status.ReadyNodes, []string{selectedNodeName})
		framework.ExpectNotContainElement(pn.Status.ReadyNodes, nonSelectedUpdatedNode.Name)

		ginkgo.By("Cleaning up test label from selected node")
		cleanupNode, err := cs.CoreV1().Nodes().Get(context.Background(), selectedNodeName, metav1.GetOptions{})
		framework.ExpectNoError(err)
		if cleanupNode.Labels != nil {
			delete(cleanupNode.Labels, testLabelKey)
			_, err = cs.CoreV1().Nodes().Update(context.Background(), cleanupNode, metav1.UpdateOptions{})
			framework.ExpectNoError(err)
		}
	})
})

func checkU2OItems(f *framework.Framework, subnet *apiv1.Subnet, underlayPod, overlayPod *corev1.Pod, isU2OCustomVpc bool) {
	ginkgo.GinkgoHelper()

	ginkgo.By("checking subnet's u2o interconnect ip of underlay subnet " + subnet.Name)
	if subnet.Spec.U2OInterconnection {
		framework.ExpectTrue(subnet.Spec.U2OInterconnection)
		framework.ExpectIPInCIDR(subnet.Status.U2OInterconnectionIP, subnet.Spec.CIDRBlock)
		if !f.VersionPriorTo(1, 11) {
			framework.ExpectEqual(subnet.Status.U2OInterconnectionVPC, subnet.Spec.Vpc)
		}
		if !f.VersionPriorTo(1, 9) {
			if subnet.Spec.U2OInterconnectionIP != "" {
				framework.ExpectEqual(subnet.Spec.U2OInterconnectionIP, subnet.Status.U2OInterconnectionIP)
			}
		}
		if f.HasIPv4() && subnet.Spec.EnableDHCP {
			if !f.VersionPriorTo(1, 12) {
				ginkgo.By("checking u2o dhcp gateway ip of underlay subnet " + subnet.Name)
				v4Cidr, _ := util.SplitStringIP(subnet.Spec.CIDRBlock)
				v4Gateway, _ := util.SplitStringIP(subnet.Status.U2OInterconnectionIP)
				nbctlCmd := "ovn-nbctl --bare --columns=options find Dhcp_Options cidr=" + v4Cidr
				output, _, err := framework.NBExec(nbctlCmd)
				framework.ExpectNoError(err)
				framework.ExpectContainElement(strings.Fields(string(output)), "router="+v4Gateway)
			}
		}
	} else {
		framework.ExpectFalse(subnet.Spec.U2OInterconnection)
		framework.ExpectEmpty(subnet.Status.U2OInterconnectionIP)
		if !f.VersionPriorTo(1, 11) {
			framework.ExpectEmpty(subnet.Status.U2OInterconnectionVPC)
		}
		if !f.VersionPriorTo(1, 9) {
			framework.ExpectEmpty(subnet.Spec.U2OInterconnectionIP)
		}
	}

	v4gw, v6gw := util.SplitStringIP(subnet.Spec.Gateway)
	underlayCidr := strings.SplitSeq(subnet.Spec.CIDRBlock, ",")
	for cidr := range underlayCidr {
		var protocolStr, gw string
		if util.CheckProtocol(cidr) == apiv1.ProtocolIPv4 {
			protocolStr = "ip4"
			gw = v4gw
			ginkgo.By("checking subnet's using ips of underlay subnet " + subnet.Name + " " + protocolStr)
			if subnet.Spec.U2OInterconnection {
				framework.ExpectEqual(int(subnet.Status.V4UsingIPs), 2)
			} else {
				framework.ExpectEqual(int(subnet.Status.V4UsingIPs), 1)
			}
		} else {
			protocolStr = "ip6"
			gw = v6gw
			ginkgo.By("checking subnet's using ips of underlay subnet " + subnet.Name + " " + protocolStr)
			if subnet.Spec.U2OInterconnection {
				framework.ExpectEqual(int(subnet.Status.V6UsingIPs), 2)
			} else {
				framework.ExpectEqual(int(subnet.Status.V6UsingIPs), 1)
			}
		}

		asName := strings.ReplaceAll(fmt.Sprintf("%s.u2o_exclude_ip.%s", subnet.Name, protocolStr), "-", ".")
		if !isU2OCustomVpc {
			ginkgo.By("checking underlay subnet's policy1 route " + protocolStr)
			hitPolicyStr := fmt.Sprintf("%d %s.dst == %s allow", util.U2OSubnetPolicyPriority, protocolStr, cidr)
			checkPolicy(hitPolicyStr, subnet.Spec.U2OInterconnection, subnet.Spec.Vpc)

			ginkgo.By("checking underlay subnet's policy2 route " + protocolStr)
			hitPolicyStr = fmt.Sprintf("%d %s.dst == $%s && %s.src == %s reroute %s", util.SubnetRouterPolicyPriority, protocolStr, asName, protocolStr, cidr, gw)
			checkPolicy(hitPolicyStr, subnet.Spec.U2OInterconnection, subnet.Spec.Vpc)
		}

		ginkgo.By("checking underlay subnet's policy3 route " + protocolStr)
		hitPolicyStr := fmt.Sprintf("%d %s.src == %s reroute %s", util.GatewayRouterPolicyPriority, protocolStr, cidr, gw)
		checkPolicy(hitPolicyStr, subnet.Spec.U2OInterconnection, subnet.Spec.Vpc)
	}

	ginkgo.By("checking underlay pod's ip route's nexthop equal the u2o interconnection ip")
	routes, err := iproute.RouteShow("", "eth0", func(cmd ...string) ([]byte, []byte, error) {
		return framework.KubectlExec(underlayPod.Namespace, underlayPod.Name, cmd...)
	})
	framework.ExpectNoError(err)
	framework.ExpectNotEmpty(routes)

	v4InterconnIP, v6InterconnIP := util.SplitStringIP(subnet.Status.U2OInterconnectionIP)

	isV4DefaultRouteExist := false
	isV6DefaultRouteExist := false
	for _, route := range routes {
		if route.Dst == "default" {
			if util.CheckProtocol(route.Gateway) == apiv1.ProtocolIPv4 {
				if subnet.Spec.U2OInterconnection {
					framework.ExpectEqual(route.Gateway, v4InterconnIP)
				} else {
					framework.ExpectEqual(route.Gateway, v4gw)
				}
				isV4DefaultRouteExist = true
			} else {
				if subnet.Spec.U2OInterconnection {
					framework.ExpectEqual(route.Gateway, v6InterconnIP)
				} else {
					framework.ExpectEqual(route.Gateway, v6gw)
				}
				isV6DefaultRouteExist = true
			}
		}
	}

	switch subnet.Spec.Protocol {
	case apiv1.ProtocolIPv4:
		framework.ExpectTrue(isV4DefaultRouteExist)
	case apiv1.ProtocolIPv6:
		framework.ExpectTrue(isV6DefaultRouteExist)
	case apiv1.ProtocolDual:
		framework.ExpectTrue(isV4DefaultRouteExist)
		framework.ExpectTrue(isV6DefaultRouteExist)
	}

	UPodIPs := underlayPod.Status.PodIPs
	OPodIPs := overlayPod.Status.PodIPs
	var v4UPodIP, v4OPodIP, v6UPodIP, v6OPodIP string
	for _, UPodIP := range UPodIPs {
		if util.CheckProtocol(UPodIP.IP) == apiv1.ProtocolIPv4 {
			v4UPodIP = UPodIP.IP
		} else {
			v6UPodIP = UPodIP.IP
		}
	}
	for _, OPodIP := range OPodIPs {
		if util.CheckProtocol(OPodIP.IP) == apiv1.ProtocolIPv4 {
			v4OPodIP = OPodIP.IP
		} else {
			v6OPodIP = OPodIP.IP
		}
	}

	if v4UPodIP != "" && v4OPodIP != "" {
		ginkgo.By("checking underlay pod access to overlay pod v4")
		checkReachable(underlayPod.Name, underlayPod.Namespace, v4UPodIP, v4OPodIP, strconv.Itoa(curlListenPort), subnet.Spec.U2OInterconnection)

		ginkgo.By("checking overlay pod access to underlay pod v4")
		checkReachable(overlayPod.Name, overlayPod.Namespace, v4OPodIP, v4UPodIP, strconv.Itoa(curlListenPort), subnet.Spec.U2OInterconnection)
	}

	if v6UPodIP != "" && v6OPodIP != "" {
		ginkgo.By("checking underlay pod access to overlay pod v6")
		checkReachable(underlayPod.Name, underlayPod.Namespace, v6UPodIP, v6OPodIP, strconv.Itoa(curlListenPort), subnet.Spec.U2OInterconnection)

		ginkgo.By("checking overlay pod access to underlay pod v6")
		checkReachable(overlayPod.Name, overlayPod.Namespace, v6OPodIP, v6UPodIP, strconv.Itoa(curlListenPort), subnet.Spec.U2OInterconnection)
	}
}

func checkReachable(podName, podNamespace, sourceIP, targetIP, targetPort string, expectReachable bool) {
	ginkgo.GinkgoHelper()

	ginkgo.By("checking curl reachable")
	cmd := fmt.Sprintf("curl -q -s --connect-timeout 2 --max-time 2 %s/clientip", net.JoinHostPort(targetIP, targetPort))
	output, err := e2epodoutput.RunHostCmd(podNamespace, podName, cmd)
	if expectReachable {
		framework.ExpectNoError(err)
		client, _, err := net.SplitHostPort(strings.TrimSpace(output))
		framework.ExpectNoError(err)
		// check packet has not SNAT
		framework.ExpectEqual(sourceIP, client)
	} else {
		framework.ExpectError(err)
	}
}

func checkPolicy(hitPolicyStr string, expectPolicyExist bool, vpcName string) {
	ginkgo.GinkgoHelper()

	framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
		cmd := "ovn-nbctl lr-policy-list " + vpcName
		output, _, err := framework.NBExec(cmd)
		if err != nil {
			return false, err
		}
		found := false
		for line := range strings.SplitSeq(string(output), "\n") {
			if strings.Contains(strings.Join(strings.Fields(line), " "), hitPolicyStr) {
				found = true
				break
			}
		}
		return found == expectPolicyExist, nil
	}, fmt.Sprintf("policy %q exist=%v in vpc %s", hitPolicyStr, expectPolicyExist, vpcName))
}

func checkU2OFilterOpenFlowExist(clusterName string, pn *apiv1.ProviderNetwork, subnet *apiv1.Subnet, expectRuleExist bool) error {
	nodes, err := kind.ListNodes(clusterName, "")
	if err != nil {
		return fmt.Errorf("getting nodes in kind cluster: %w", err)
	}
	if len(nodes) == 0 {
		return errors.New("no nodes found in kind cluster")
	}

	for _, node := range nodes {
		gws := strings.Split(subnet.Spec.Gateway, ",")
		u2oIPs := strings.Split(subnet.Status.U2OInterconnectionIP, ",")
		for index, gw := range gws {
			var cmd string
			if util.CheckProtocol(gw) == apiv1.ProtocolIPv4 {
				cmd = fmt.Sprintf("kubectl ko ofctl %s dump-flows br-%s | grep 0x1000", node.Name(), pn.Name)
			} else {
				cmd = fmt.Sprintf("kubectl ko ofctl %s dump-flows br-%s | grep 0x1001", node.Name(), pn.Name)
			}

			var matchStr string
			if util.CheckProtocol(gw) == apiv1.ProtocolIPv4 {
				matchStr = fmt.Sprintf("priority=10000,arp,in_port=1,arp_spa=%s,arp_tpa=%s,arp_op=1", gw, u2oIPs[index])
			} else {
				matchStr = "priority=10000,icmp6,in_port=1,icmp_type=135,nd_target=" + u2oIPs[index]
			}

			var success bool
			for deadline := time.Now().Add(30 * time.Second); ; {
				output, _ := exec.Command("bash", "-c", cmd).CombinedOutput()
				outputStr := string(output)
				framework.Logf("matchStr rule %s", matchStr)
				framework.Logf("outputStr rule %s", outputStr)
				ruleExist := strings.Contains(outputStr, matchStr)
				if (expectRuleExist && ruleExist) || (!expectRuleExist && outputStr == "") {
					success = true
					break
				}
				if time.Now().After(deadline) {
					break
				}
				time.Sleep(2 * time.Second)
			}

			if !success {
				if expectRuleExist {
					return errors.New("expected rule does not exist after 30s")
				}
				return errors.New("unexpected rule exists after 30s")
			}
		}
	}
	framework.Logf("checkU2OFilterOpenFlowExist works successfully")
	return nil
}
