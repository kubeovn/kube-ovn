package underlay

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	"github.com/onsi/ginkgo/v2"

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
	ginkgo.By("Waiting for status of subnet " + subnetName + " to be updated")
	framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
		subnet := subnetClient.Get(subnetName)
		if (subnet.Status.V4AvailableIPs != 0 && subnet.Status.V4UsingIPs != expectedUsingIPs) ||
			(subnet.Status.V6AvailableIPs != 0 && subnet.Status.V6UsingIPs != expectedUsingIPs) {
			return false, nil
		}
		return true, nil
	}, "")
}

var _ = framework.SerialDescribe("[group:underlay]", func() {
	f := framework.NewDefaultFramework("underlay")

	var skip bool
	var itFn func(bool)
	var cs clientset.Interface
	var nodeNames []string
	var clusterName, providerNetworkName, vlanName, subnetName, podName, namespaceName string
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
	var dockerNetwork *dockertypes.NetworkResource
	var containerID string
	var image string

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		eventClient = f.EventClient()
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		vpcClient = f.VpcClient()
		vlanClient = f.VlanClient()
		providerNetworkClient = f.ProviderNetworkClient()
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
		containerID = ""
		if image == "" {
			image = framework.GetKubeOvnImage(cs)
		}

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
			framework.ExpectHaveKey(linkMap, node.ID)

			linkMap[node.Name()] = linkMap[node.ID]
			routeMap[node.Name()] = routeMap[node.ID]
			nodeNames = append(nodeNames, node.Name())
		}

		itFn = func(exchangeLinkName bool) {
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
					if route.Dev == linkNameMap[node.ID] {
						portRoutes = append(portRoutes, r)
					} else if route.Dev == bridgeName {
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

		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)

		if u2oPodNameUnderlay != "" {
			ginkgo.By("Deleting underlay pod " + u2oPodNameUnderlay)
			podClient.DeleteSync(u2oPodNameUnderlay)
		}

		if u2oPodNameOverlay != "" {
			ginkgo.By("Deleting overlay pod default subnet " + u2oPodNameOverlay)
			podClient.DeleteSync(u2oPodNameOverlay)
		}

		if u2oPodOverlayCustomVPC != "" {
			ginkgo.By("Deleting overlay pod custom vpc " + u2oPodOverlayCustomVPC)
			podClient.DeleteSync(u2oPodOverlayCustomVPC)
		}

		if u2oOverlaySubnetNameCustomVPC != "" {
			ginkgo.By("Deleting subnet in custom vpc" + u2oOverlaySubnetNameCustomVPC)
			subnetClient.DeleteSync(u2oOverlaySubnetNameCustomVPC)
		}

		if u2oOverlaySubnetName != "" {
			ginkgo.By("Deleting subnet " + u2oOverlaySubnetName)
			subnetClient.DeleteSync(u2oOverlaySubnetName)
		}

		if vpcName != "" {
			ginkgo.By("Deleting custom vpc " + vpcName)
			vpcClient.DeleteSync(vpcName)
		}

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)

		ginkgo.By("Deleting vlan " + vlanName)
		vlanClient.Delete(vlanName, metav1.DeleteOptions{})

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
		cidr := make([]string, 0, 2)
		gateway := make([]string, 0, 2)
		for _, config := range dockerNetwork.IPAM.Config {
			switch util.CheckProtocol(config.Subnet) {
			case apiv1.ProtocolIPv4:
				if f.HasIPv4() {
					cidr = append(cidr, config.Subnet)
					gateway = append(gateway, config.Gateway)
				}
			case apiv1.ProtocolIPv6:
				if f.HasIPv6() {
					cidr = append(cidr, config.Subnet)
					gateway = append(gateway, config.Gateway)
				}
			}
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
		subnet := framework.MakeSubnet(subnetName, vlanName, strings.Join(cidr, ","), strings.Join(gateway, ","), "", "", excludeIPs, nil, []string{namespaceName})
		_ = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating pod " + podName)
		cmd := []string{"sh", "-c", "sleep 600"}
		pod := framework.MakePod(namespaceName, podName, nil, nil, image, cmd, nil)
		_ = podClient.CreateSync(pod)

		ginkgo.By("Validating pod MTU")
		links, err := iproute.AddressShow("eth0", func(cmd ...string) ([]byte, []byte, error) {
			return framework.KubectlExec(namespaceName, podName, cmd...)
		})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(links, 1, "should get eth0 information")
		framework.ExpectEqual(links[0].Mtu, docker.MTU)
	})

	framework.ConformanceIt("should be able to detect IPv4 address conflict", func() {
		if !f.IsIPv4() {
			ginkgo.Skip("Address conflict detection only supports IPv4")
		}
		f.SkipVersionPriorTo(1, 9, "Address conflict detection was introduced in v1.9")

		ginkgo.By("Creating provider network " + providerNetworkName)
		pn := makeProviderNetwork(providerNetworkName, false, linkMap)
		_ = providerNetworkClient.CreateSync(pn)

		ginkgo.By("Getting docker network " + dockerNetworkName)
		network, err := docker.NetworkInspect(dockerNetworkName)
		framework.ExpectNoError(err, "getting docker network "+dockerNetworkName)

		containerName := "container-" + framework.RandomSuffix()
		ginkgo.By("Creating container " + containerName)
		cmd := []string{"sh", "-c", "sleep 600"}
		containerInfo, err := docker.ContainerCreate(containerName, image, dockerNetworkName, cmd)
		framework.ExpectNoError(err)
		containerID = containerInfo.ID

		ginkgo.By("Creating vlan " + vlanName)
		vlan := framework.MakeVlan(vlanName, providerNetworkName, 0)
		_ = vlanClient.Create(vlan)

		ginkgo.By("Creating subnet " + subnetName)
		cidr := make([]string, 0, 2)
		gateway := make([]string, 0, 2)
		for _, config := range dockerNetwork.IPAM.Config {
			if util.CheckProtocol(config.Subnet) == apiv1.ProtocolIPv4 {
				cidr = append(cidr, config.Subnet)
				gateway = append(gateway, config.Gateway)
				break
			}
		}
		excludeIPs := make([]string, 0, len(network.Containers)*2)
		for _, container := range network.Containers {
			if container.IPv4Address != "" {
				excludeIPs = append(excludeIPs, strings.Split(container.IPv4Address, "/")[0])
			}
		}
		subnet := framework.MakeSubnet(subnetName, vlanName, strings.Join(cidr, ","), strings.Join(gateway, ","), "", "", excludeIPs, nil, []string{namespaceName})
		_ = subnetClient.CreateSync(subnet)

		ip := containerInfo.NetworkSettings.Networks[dockerNetworkName].IPAddress
		mac := containerInfo.NetworkSettings.Networks[dockerNetworkName].MacAddress
		ginkgo.By("Creating pod " + podName + " with IP address " + ip)
		annotations := map[string]string{util.IPAddressAnnotation: ip}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, image, cmd, nil)
		pod.Spec.TerminationGracePeriodSeconds = nil
		_ = podClient.Create(pod)

		ginkgo.By("Waiting for pod events")
		events := eventClient.WaitToHaveEvent("Pod", podName, "Warning", "FailedCreatePodSandBox", "kubelet", "")
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
		underlayCidr := make([]string, 0, 2)
		gateway := make([]string, 0, 2)
		for _, config := range dockerNetwork.IPAM.Config {
			switch util.CheckProtocol(config.Subnet) {
			case apiv1.ProtocolIPv4:
				if f.HasIPv4() {
					underlayCidr = append(underlayCidr, config.Subnet)
					gateway = append(gateway, config.Gateway)
				}
			case apiv1.ProtocolIPv6:
				if f.HasIPv6() {
					underlayCidr = append(underlayCidr, config.Subnet)
					gateway = append(gateway, config.Gateway)
				}
			}
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

		ginkgo.By("Creating underlay subnet " + subnetName)
		subnet := framework.MakeSubnet(subnetName, vlanName, strings.Join(underlayCidr, ","), strings.Join(gateway, ","), "", "", excludeIPs, nil, []string{namespaceName})
		subnet.Spec.U2OInterconnection = true
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
		checkU2OItems(f, subnet, underlayPod, overlayPod, false)

		ginkgo.By("step4: Check if kube-ovn-controller restart")

		ginkgo.By("Restarting kube-ovn-controller")
		deployClient := f.DeploymentClientNS(framework.KubeOvnNamespace)
		deploy := deployClient.Get("kube-ovn-controller")
		deployClient.RestartSync(deploy)

		subnet = subnetClient.Get(subnetName)
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
		checkU2OItems(f, subnet, underlayPod, overlayPod, false)

		if f.VersionPriorTo(1, 9) {
			return
		}

		ginkgo.By("step7: Specify u2oInterconnectionIP")

		// change u2o interconnection ip twice
		for index := 0; index < 2; index++ {
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
		checkU2OItems(f, subnet, underlayPod, overlayPod, false)
	})
})

func checkU2OItems(f *framework.Framework, subnet *apiv1.Subnet, underlayPod, overlayPod *corev1.Pod, isU2OCustomVpc bool) {
	ginkgo.By("checking underlay subnet's u2o interconnect ip")
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
	underlayCidr := strings.Split(subnet.Spec.CIDRBlock, ",")
	for _, cidr := range underlayCidr {
		var protocolStr, gw string
		if util.CheckProtocol(cidr) == apiv1.ProtocolIPv4 {
			protocolStr = "ip4"
			gw = v4gw
			ginkgo.By("checking underlay subnet's using ips")
			if subnet.Spec.U2OInterconnection {
				framework.ExpectEqual(int(subnet.Status.V4UsingIPs), 2)
			} else {
				framework.ExpectEqual(int(subnet.Status.V4UsingIPs), 1)
			}
		} else {
			protocolStr = "ip6"
			gw = v6gw
			if subnet.Spec.U2OInterconnection {
				framework.ExpectEqual(int(subnet.Status.V6UsingIPs), 2)
			} else {
				framework.ExpectEqual(int(subnet.Status.V6UsingIPs), 1)
			}
		}

		asName := strings.ReplaceAll(fmt.Sprintf("%s.u2o_exclude_ip.%s", subnet.Name, protocolStr), "-", ".")
		if !isU2OCustomVpc {
			ginkgo.By(fmt.Sprintf("checking underlay subnet's policy1 route %s", protocolStr))
			hitPolicyStr := fmt.Sprintf("%d %s.dst == %s allow", util.U2OSubnetPolicyPriority, protocolStr, cidr)
			checkPolicy(hitPolicyStr, subnet.Spec.U2OInterconnection, subnet.Spec.Vpc)

			ginkgo.By(fmt.Sprintf("checking underlay subnet's policy2 route %s", protocolStr))
			hitPolicyStr = fmt.Sprintf("%d %s.dst == $%s && %s.src == %s reroute %s", util.SubnetRouterPolicyPriority, protocolStr, asName, protocolStr, cidr, gw)
			checkPolicy(hitPolicyStr, subnet.Spec.U2OInterconnection, subnet.Spec.Vpc)
		}

		ginkgo.By(fmt.Sprintf("checking underlay subnet's policy3 route %s", protocolStr))
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

	switch {
	case subnet.Spec.Protocol == apiv1.ProtocolIPv4:
		framework.ExpectTrue(isV4DefaultRouteExist)
	case subnet.Spec.Protocol == apiv1.ProtocolIPv6:
		framework.ExpectTrue(isV6DefaultRouteExist)
	case subnet.Spec.Protocol == apiv1.ProtocolDual:
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
	ginkgo.By("checking curl reachable")
	cmd := fmt.Sprintf("kubectl exec %s -n %s -- curl -q -s --connect-timeout 5 %s/clientip", podName, podNamespace, net.JoinHostPort(targetIP, targetPort))
	output, _ := exec.Command("bash", "-c", cmd).CombinedOutput()
	outputStr := string(output)
	if expectReachable {
		client, _, err := net.SplitHostPort(strings.TrimSpace(outputStr))
		framework.ExpectNoError(err)
		// check packet has not SNAT
		framework.ExpectEqual(sourceIP, client)
	} else {
		isReachable := !strings.Contains(outputStr, "terminated with exit code")
		framework.ExpectEqual(isReachable, expectReachable)
	}
}

func checkPolicy(hitPolicyStr string, expectPolicyExist bool, vpcName string) {
	framework.WaitUntil(time.Second, 10*time.Second, func(_ context.Context) (bool, error) {
		output, err := exec.Command("bash", "-c", fmt.Sprintf("kubectl ko nbctl lr-policy-list %s", vpcName)).CombinedOutput()
		if err != nil {
			return false, err
		}
		outputStr := string(output)
		for _, line := range strings.Split(outputStr, "\n") {
			if strings.Contains(strings.Join(strings.Fields(line), " "), hitPolicyStr) == expectPolicyExist {
				return true, nil
			}
		}
		return false, nil
	}, "")
}
