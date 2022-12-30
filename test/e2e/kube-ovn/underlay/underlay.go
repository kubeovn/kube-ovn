package underlay

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iproute"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

const dockerNetworkName = "kube-ovn-vlan"

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

var _ = framework.Describe("[group:underlay]", func() {
	f := framework.NewDefaultFramework("provider-network")

	var skip bool
	var itFn func(bool)
	var cs clientset.Interface
	var nodeNames []string
	var clusterName, providerNetworkName, vlanName, subnetName, namespaceName, u2oPodNameUnderlay, u2oOverlaySubnetName, u2oPodNameOverlay string
	var linkMap map[string]*iproute.Link
	var routeMap map[string][]iproute.Route
	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient
	var vlanClient *framework.VlanClient
	var providerNetworkClient *framework.ProviderNetworkClient
	var dockerNetwork *dockertypes.NetworkResource

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		vlanClient = f.VlanClient()
		providerNetworkClient = f.ProviderNetworkClient()
		namespaceName = f.Namespace.Name
		subnetName = "subnet-" + framework.RandomSuffix()
		vlanName = "vlan-" + framework.RandomSuffix()
		providerNetworkName = "pn-" + framework.RandomSuffix()

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
			ginkgo.By("Creating provider network")
			pn := makeProviderNetwork(providerNetworkName, exchangeLinkName, linkMap)
			pn = providerNetworkClient.CreateSync(pn)

			ginkgo.By("Getting k8s nodes")
			k8sNodes, err := e2enode.GetReadySchedulableNodes(cs)
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
		if u2oPodNameUnderlay != "" {
			ginkgo.By("Deleting underlay pod " + u2oPodNameUnderlay)
			podClient.DeleteSync(u2oPodNameUnderlay)

			ginkgo.By("Deleting overlay pod " + u2oPodNameOverlay)
			podClient.DeleteSync(u2oPodNameOverlay)

			ginkgo.By("Deleting subnet " + u2oOverlaySubnetName)
			subnetClient.DeleteSync(u2oOverlaySubnetName)
		}

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)

		ginkgo.By("Deleting vlan " + vlanName)
		vlanClient.Delete(vlanName, metav1.DeleteOptions{})

		ginkgo.By("Deleting provider network")
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
		itFn(true)
	})

	framework.ConformanceIt("should keep pod mtu the same with node interface", func() {
		ginkgo.By("Creating provider network")
		pn := makeProviderNetwork(providerNetworkName, false, linkMap)
		_ = providerNetworkClient.CreateSync(pn)

		ginkgo.By("Getting docker network " + dockerNetworkName)
		network, err := docker.NetworkGet(dockerNetworkName)
		framework.ExpectNoError(err, "getting docker network "+dockerNetworkName)

		ginkgo.By("Creating vlan " + vlanName)
		vlan := framework.MakeVlan(vlanName, providerNetworkName, 0)
		_ = vlanClient.Create(vlan)

		ginkgo.By("Creating subnet " + subnetName)
		cidr := make([]string, 0, 2)
		gateway := make([]string, 0, 2)
		for _, config := range dockerNetwork.IPAM.Config {
			cidr = append(cidr, config.Subnet)
			gateway = append(gateway, config.Gateway)
		}
		excludeIPs := make([]string, 0, len(network.Containers)*2)
		for _, container := range network.Containers {
			if container.IPv4Address != "" {
				excludeIPs = append(excludeIPs, container.IPv4Address)
			}
			if container.IPv6Address != "" {
				excludeIPs = append(excludeIPs, container.IPv6Address)
			}
		}
		subnet := framework.MakeSubnet(subnetName, vlanName, strings.Join(cidr, ","), strings.Join(gateway, ","), excludeIPs, nil, []string{namespaceName})
		_ = subnetClient.CreateSync(subnet)

		podName := "pod-" + framework.RandomSuffix()
		ginkgo.By("Creating pod " + podName)
		cmd := []string{"sh", "-c", "sleep 600"}
		pod := framework.MakePod(namespaceName, podName, nil, nil, framework.GetKubeOvnImage(cs), cmd, nil)
		_ = podClient.CreateSync(pod)

		ginkgo.By("Validating pod MTU")
		links, err := iproute.AddressShow("eth0", func(cmd ...string) ([]byte, []byte, error) {
			return framework.KubectlExec(namespaceName, podName, cmd...)
		})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(links, 1, "should get eth0 information")
		framework.ExpectEqual(links[0].Mtu, docker.MTU)

		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)
	})

	framework.ConformanceIt("underlay to overlay subnet interconnection ", func() {
		ginkgo.By("Creating provider network")
		pn := makeProviderNetwork(providerNetworkName, false, linkMap)
		_ = providerNetworkClient.CreateSync(pn)

		ginkgo.By("Getting docker network " + dockerNetworkName)
		network, err := docker.NetworkGet(dockerNetworkName)
		framework.ExpectNoError(err, "getting docker network "+dockerNetworkName)

		ginkgo.By("Creating vlan " + vlanName)
		vlan := framework.MakeVlan(vlanName, providerNetworkName, 0)
		_ = vlanClient.Create(vlan)

		ginkgo.By("Creating underlay subnet " + subnetName)
		underlayCidr := make([]string, 0, 2)
		gateway := make([]string, 0, 2)
		for _, config := range dockerNetwork.IPAM.Config {
			underlayCidr = append(underlayCidr, config.Subnet)
			gateway = append(gateway, config.Gateway)
		}

		excludeIPs := make([]string, 0, len(network.Containers)*2)
		for _, container := range network.Containers {
			if container.IPv4Address != "" {
				excludeIPs = append(excludeIPs, container.IPv4Address)
			}
			if container.IPv6Address != "" {
				excludeIPs = append(excludeIPs, container.IPv6Address)
			}
		}
		subnet := framework.MakeSubnet(subnetName, vlanName, strings.Join(underlayCidr, ","), strings.Join(gateway, ","), excludeIPs, nil, []string{namespaceName})
		subnet.Spec.U2OInterconnection = true
		_ = subnetClient.CreateSync(subnet)
		ginkgo.By("Creating underlay subnet pod")
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}

		u2oPodNameUnderlay = "pod-" + framework.RandomSuffix()
		sleepCmd := []string{"sh", "-c", "sleep 600"}
		underlayPod := framework.MakePod(namespaceName, u2oPodNameUnderlay, nil, annotations, framework.GetKubeOvnImage(cs), sleepCmd, nil)
		underlayPod = podClient.CreateSync(underlayPod)

		// get subnet again because ipam change
		subnet = subnetClient.Get(subnetName)

		ginkgo.By("Creating overlay subnet")
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		namespaceName = f.Namespace.Name
		u2oOverlaySubnetName = "subnet-" + framework.RandomSuffix()
		cidr := framework.RandomCIDR(f.ClusterIpFamily)

		overlaySubnet := framework.MakeSubnet(u2oOverlaySubnetName, "", cidr, "", nil, nil, nil)
		overlaySubnet = subnetClient.CreateSync(overlaySubnet)

		ginkgo.By("Creating overlay subnet pod")
		u2oPodNameOverlay = "pod-" + framework.RandomSuffix()
		overlayAnnotations := map[string]string{
			util.LogicalSwitchAnnotation: overlaySubnet.Name,
		}
		overlayPod := framework.MakePod(namespaceName, u2oPodNameOverlay, nil, overlayAnnotations, framework.PauseImage, nil, nil)
		overlayPod = podClient.CreateSync(overlayPod)

		ginkgo.By("step1: Enable u2o check")
		checkU2OItems(true, subnet, underlayPod, overlayPod)

		ginkgo.By("step2: Disable u2o check")
		podClient.DeleteSync(u2oPodNameUnderlay)

		subnet = subnetClient.Get(subnetName)
		modifiedSubnet := subnet.DeepCopy()
		modifiedSubnet.Spec.U2OInterconnection = false
		subnetClient.PatchSync(subnet, modifiedSubnet)
		time.Sleep(5 * time.Second)
		underlayPod = framework.MakePod(namespaceName, u2oPodNameUnderlay, nil, annotations, framework.GetKubeOvnImage(cs), sleepCmd, nil)
		underlayPod = podClient.CreateSync(underlayPod)
		subnet = subnetClient.Get(subnetName)
		checkU2OItems(false, subnet, underlayPod, overlayPod)

		ginkgo.By("step3: recover enable u2o check")
		podClient.DeleteSync(u2oPodNameUnderlay)

		subnet = subnetClient.Get(subnetName)
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.U2OInterconnection = true
		subnetClient.PatchSync(subnet, modifiedSubnet)
		time.Sleep(5 * time.Second)
		underlayPod = framework.MakePod(namespaceName, u2oPodNameUnderlay, nil, annotations, framework.GetKubeOvnImage(cs), sleepCmd, nil)
		underlayPod = podClient.CreateSync(underlayPod)
		subnet = subnetClient.Get(subnetName)
		checkU2OItems(true, subnet, underlayPod, overlayPod)

		ginkgo.By("step4: check if kube-ovn-controller restart")
		restartCmd := "kubectl rollout restart deployment kube-ovn-controller -n kube-system"
		_, err = exec.Command("bash", "-c", restartCmd).CombinedOutput()
		framework.ExpectNoError(err, "restart kube-ovn-controller")
		checkU2OItems(true, subnet, underlayPod, overlayPod)

		ginkgo.By("step5: Disable u2o check after restart kube-controller")
		podClient.DeleteSync(u2oPodNameUnderlay)

		subnet = subnetClient.Get(subnetName)
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.U2OInterconnection = false
		subnetClient.PatchSync(subnet, modifiedSubnet)
		time.Sleep(5 * time.Second)
		underlayPod = framework.MakePod(namespaceName, u2oPodNameUnderlay, nil, annotations, framework.GetKubeOvnImage(cs), sleepCmd, nil)
		underlayPod = podClient.CreateSync(underlayPod)
		subnet = subnetClient.Get(subnetName)
		checkU2OItems(false, subnet, underlayPod, overlayPod)

		ginkgo.By("step6: recover enable u2o check after restart kube-controller")
		podClient.DeleteSync(u2oPodNameUnderlay)

		subnet = subnetClient.Get(subnetName)
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.U2OInterconnection = true
		subnetClient.PatchSync(subnet, modifiedSubnet)
		time.Sleep(5 * time.Second)
		underlayPod = framework.MakePod(namespaceName, u2oPodNameUnderlay, nil, annotations, framework.GetKubeOvnImage(cs), sleepCmd, nil)
		underlayPod = podClient.CreateSync(underlayPod)
		subnet = subnetClient.Get(subnetName)
		checkU2OItems(true, subnet, underlayPod, overlayPod)
	})
})

func checkU2OItems(isEnableU2O bool, subnet *apiv1.Subnet, underlayPod, overlayPod *corev1.Pod) {

	ginkgo.By("checking underlay subnet's u2o interconnect ip")
	if isEnableU2O {
		framework.ExpectEqual(subnet.Spec.U2OInterconnection, true)
		framework.ExpectNotEqual(subnet.Status.U2OInterconnectionIP, "")
	} else {
		framework.ExpectNotEqual(subnet.Spec.U2OInterconnection, true)
		framework.ExpectEqual(subnet.Status.U2OInterconnectionIP, "")
	}

	gws := strings.Split(subnet.Spec.Gateway, ",")
	var v4gw, v6gw string
	for _, gw := range gws {
		if util.CheckProtocol(gw) == "IPv4" {
			v4gw = gw
		} else {
			v6gw = gw
		}
	}

	underlayCidr := strings.Split(subnet.Spec.CIDRBlock, ",")
	for _, cidr := range underlayCidr {
		if util.CheckProtocol(cidr) == "IPv4" {
			ginkgo.By("checking underlay subnet's using ips")
			if isEnableU2O {
				framework.ExpectEqual(int(subnet.Status.V4UsingIPs), 2)
			} else {
				framework.ExpectEqual(int(subnet.Status.V4UsingIPs), 1)
			}

			agStr := strings.Replace(fmt.Sprintf("%s.u2o_exclude_ip.ip4", subnet.Name), "-", ".", -1)
			ginkgo.By("checking underlay subnet's policy1 route")
			hitPolicyStr := fmt.Sprintf("[31000 ip4.dst == $%s && ip4.src == %s allow]", agStr, cidr)
			checkPolicy(hitPolicyStr, isEnableU2O)

			ginkgo.By("checking underlay subnet's policy2 route")
			hitPolicyStr = fmt.Sprintf("[31000 ip4.dst == %s && ip4.dst != $%s allow]", cidr, agStr)
			checkPolicy(hitPolicyStr, isEnableU2O)

			ginkgo.By("checking underlay subnet's policy3 route")
			hitPolicyStr = fmt.Sprintf("[29000 ip4.src == %s reroute %s]", cidr, v4gw)
			checkPolicy(hitPolicyStr, isEnableU2O)
		} else {
			if isEnableU2O {
				framework.ExpectEqual(int(subnet.Status.V6UsingIPs), 2)
			} else {
				framework.ExpectEqual(int(subnet.Status.V6UsingIPs), 1)
			}

			agStr := strings.Replace(fmt.Sprintf("%s.u2o_exclude_ip.ip6", subnet.Name), "-", ".", -1)
			ginkgo.By("checking underlay subnet's policy1 route")
			hitPolicyStr := fmt.Sprintf("[31000 ip6.dst == $%s && ip6.src == %s allow]", agStr, cidr)
			checkPolicy(hitPolicyStr, isEnableU2O)

			ginkgo.By("checking underlay subnet's policy2 route")
			hitPolicyStr = fmt.Sprintf("[31000 ip6.dst == %s && ip6.dst != $%s allow]", cidr, agStr)
			checkPolicy(hitPolicyStr, isEnableU2O)

			ginkgo.By("checking underlay subnet's policy3 route")
			hitPolicyStr = fmt.Sprintf("[29000 ip6.src == %s reroute %s]", cidr, v6gw)
			checkPolicy(hitPolicyStr, isEnableU2O)
		}
	}

	ginkgo.By("checking underlay pod's ip route's nexthop equal the u2o interconnection ip")
	routes, err := getPodDefaultRoute(underlayPod)
	framework.ExpectNoError(err)
	framework.ExpectNotEmpty(routes)

	interconnIps := strings.Split(subnet.Status.U2OInterconnectionIP, ",")
	var v4InterconnIp, v6InterconnIp string
	for _, connIp := range interconnIps {
		if util.CheckProtocol(connIp) == "IPv4" {
			v4InterconnIp = connIp
		} else {
			v6InterconnIp = connIp
		}
	}

	for _, route := range routes {
		if route.Dst == "default" {
			if util.CheckProtocol(route.Gateway) == "IPv4" {
				if isEnableU2O {
					framework.ExpectEqual(route.Gateway, v4InterconnIp)
				} else {
					framework.ExpectEqual(route.Gateway, v4gw)
				}
			} else {
				if isEnableU2O {
					framework.ExpectEqual(route.Gateway, v6InterconnIp)
				} else {
					framework.ExpectEqual(route.Gateway, v6gw)
				}
			}
		}
	}

	ginkgo.By("checking underlay pod access to overlay pod")
	checkPing(underlayPod.Name, underlayPod.Namespace, overlayPod.Status.PodIP, isEnableU2O)
}

func getPodDefaultRoute(pod *corev1.Pod) ([]iproute.Route, error) {
	var routes, routes6 []iproute.Route
	stdout, _ := exec.Command("bash", "-c", fmt.Sprintf("kubectl exec %s -n %s -- ip -d -j route show dev eth0", pod.Name, pod.Namespace)).CombinedOutput()

	if err := json.Unmarshal(stdout, &routes); err != nil {
		return nil, fmt.Errorf("failed to decode json %q: %v", string(stdout), err)
	}

	stdout, _ = exec.Command("bash", "-c", fmt.Sprintf("kubectl exec %s -n %s -- ip -d -j -6 route show dev eth0", pod.Name, pod.Namespace)).CombinedOutput()
	if err := json.Unmarshal(stdout, &routes6); err != nil {
		return nil, fmt.Errorf("failed to decode json %q: %v", string(stdout), err)
	}
	return append(routes, routes6...), nil
}

func checkPing(podName, podNamespace, targetIP string, expectReachable bool) {
	isReachable := false
	pingCmd := fmt.Sprintf("kubectl exec %s -n %s -- ping %s -c 1 -W 1 ", podName, podNamespace, targetIP)
	output, _ := exec.Command("bash", "-c", pingCmd).CombinedOutput()
	outputStr := string(output)
	lines := strings.Split(outputStr, "\n")
	for _, line := range lines {
		if strings.Contains(line, "1 packets received") {
			isReachable = true
		}
	}
	framework.ExpectEqual(isReachable, expectReachable)
}

func checkPolicy(hitPolicyStr string, expectPolicyExist bool) {
	policyExist := false
	output, _ := exec.Command("bash", "-c", "kubectl ko nbctl lr-policy-list ovn-cluster").CombinedOutput()
	outputStr := string(output)
	lines := strings.Split(outputStr, "\n")
	for _, line := range lines {
		formatLine := fmt.Sprintf("%s\n", strings.Fields(line))
		if strings.Contains(formatLine, hitPolicyStr) {
			policyExist = true
		}

	}
	framework.ExpectEqual(policyExist, expectPolicyExist)
}
