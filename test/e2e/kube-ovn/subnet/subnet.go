package subnet

import (
	"context"
	"fmt"
	"math/big"
	"math/rand"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/test/e2e/framework/deployment"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iproute"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

var _ = framework.Describe("[group:subnet]", func() {
	f := framework.NewDefaultFramework("subnet")

	var subnet *apiv1.Subnet
	var cs clientset.Interface
	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient
	var namespaceName, subnetName string
	var cidr, cidrV4, cidrV6, firstIPv4, lastIPv4, firstIPv6, lastIPv6 string
	var gateways []string
	var podCount int
	var podNamePre, deployName string

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		namespaceName = f.Namespace.Name
		subnetName = "subnet-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIpFamily)
		cidrV4, cidrV6 = util.SplitStringIP(cidr)
		gateways = nil
		podCount = 0
		podNamePre = ""
		deployName = ""
		if cidrV4 == "" {
			firstIPv4 = ""
			lastIPv4 = ""
		} else {
			firstIPv4, _ = util.FirstIP(cidrV4)
			lastIPv4, _ = util.LastIP(cidrV4)
			gateways = append(gateways, firstIPv4)
		}
		if cidrV6 == "" {
			firstIPv6 = ""
			lastIPv6 = ""
		} else {
			firstIPv6, _ = util.FirstIP(cidrV6)
			lastIPv6, _ = util.LastIP(cidrV6)
			gateways = append(gateways, firstIPv6)
		}
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting deployment ")
		if deployName != "" {
			err := cs.AppsV1().Deployments(namespaceName).Delete(context.TODO(), deployName, metav1.DeleteOptions{})
			fmt.Printf("delete deployment %s failed with err %v", deployName, err)
		}

		ginkgo.By("Deleting pods ")
		if podCount != 0 {
			for i := 1; i <= podCount; i++ {
				podClient.DeleteSync(fmt.Sprintf("%s%d", podNamePre, i))
			}
		}

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
	})

	framework.ConformanceIt("should create subnet with only cidr provided", func() {
		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Validating subnet finalizers")
		framework.ExpectContainElement(subnet.Finalizers, util.ControllerName)

		ginkgo.By("Validating subnet spec fields")
		framework.ExpectFalse(subnet.Spec.Default)
		framework.ExpectEqual(subnet.Spec.Protocol, util.CheckProtocol(cidr))
		framework.ExpectEmpty(subnet.Spec.Namespaces)
		framework.ExpectConsistOf(subnet.Spec.ExcludeIps, gateways)
		framework.ExpectEqual(subnet.Spec.Gateway, strings.Join(gateways, ","))
		framework.ExpectEqual(subnet.Spec.GatewayType, apiv1.GWDistributedType)
		framework.ExpectEmpty(subnet.Spec.GatewayNode)
		framework.ExpectFalse(subnet.Spec.NatOutgoing)
		framework.ExpectFalse(subnet.Spec.Private)
		framework.ExpectEmpty(subnet.Spec.AllowSubnets)

		ginkgo.By("Validating subnet status fields")
		framework.ExpectEmpty(subnet.Status.ActivateGateway)
		framework.ExpectZero(subnet.Status.V4UsingIPs)
		framework.ExpectZero(subnet.Status.V6UsingIPs)

		if cidrV4 == "" {
			framework.ExpectZero(subnet.Status.V4AvailableIPs)
		} else {
			_, ipnet, _ := net.ParseCIDR(cidrV4)
			framework.ExpectEqual(subnet.Status.V4AvailableIPs, util.AddressCount(ipnet)-1)
		}
		if cidrV6 == "" {
			framework.ExpectZero(subnet.Status.V6AvailableIPs)
		} else {
			_, ipnet, _ := net.ParseCIDR(cidrV6)
			framework.ExpectEqual(subnet.Status.V6AvailableIPs, util.AddressCount(ipnet)-1)
		}

		// TODO: check routes on ovn0
	})

	framework.ConformanceIt("should format subnet cidr", func() {
		fn := func(cidr string) string {
			if cidr == "" {
				return ""
			}
			_, ipnet, _ := net.ParseCIDR(cidr)
			ipnet.IP = net.ParseIP(framework.RandomIPPool(cidr, ";", 1))
			return ipnet.String()
		}

		s := make([]string, 0, 2)
		if c := fn(cidrV4); c != "" {
			s = append(s, c)
		}
		if c := fn(cidrV6); c != "" {
			s = append(s, c)
		}

		subnet = framework.MakeSubnet(subnetName, "", strings.Join(s, ","), "", nil, nil, nil)
		ginkgo.By("Creating subnet " + subnetName + " with cidr " + subnet.Spec.CIDRBlock)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Validating subnet finalizers")
		framework.ExpectContainElement(subnet.ObjectMeta.Finalizers, util.ControllerName)

		ginkgo.By("Validating subnet spec fields")
		framework.ExpectFalse(subnet.Spec.Default)
		framework.ExpectEqual(subnet.Spec.Protocol, util.CheckProtocol(cidr))
		framework.ExpectEmpty(subnet.Spec.Namespaces)
		framework.ExpectConsistOf(subnet.Spec.ExcludeIps, gateways)
		framework.ExpectEqual(subnet.Spec.Gateway, strings.Join(gateways, ","))
		framework.ExpectEqual(subnet.Spec.GatewayType, apiv1.GWDistributedType)
		framework.ExpectEmpty(subnet.Spec.GatewayNode)
		framework.ExpectFalse(subnet.Spec.NatOutgoing)
		framework.ExpectFalse(subnet.Spec.Private)
		framework.ExpectEmpty(subnet.Spec.AllowSubnets)

		ginkgo.By("Validating subnet status fields")
		framework.ExpectEmpty(subnet.Status.ActivateGateway)
		framework.ExpectZero(subnet.Status.V4UsingIPs)
		framework.ExpectZero(subnet.Status.V6UsingIPs)

		if cidrV4 == "" {
			framework.ExpectZero(subnet.Status.V4AvailableIPs)
		} else {
			_, ipnet, _ := net.ParseCIDR(cidrV4)
			framework.ExpectEqual(subnet.Status.V4AvailableIPs, util.AddressCount(ipnet)-1)
		}
		if cidrV6 == "" {
			framework.ExpectZero(subnet.Status.V6AvailableIPs)
		} else {
			_, ipnet, _ := net.ParseCIDR(cidrV6)
			framework.ExpectEqual(subnet.Status.V6AvailableIPs, util.AddressCount(ipnet)-1)
		}

		// TODO: check routes on ovn0
	})

	framework.ConformanceIt("should create subnet with exclude ips", func() {
		excludeIPv4 := framework.RandomExcludeIPs(cidrV4, rand.Intn(10)+1)
		excludeIPv6 := framework.RandomExcludeIPs(cidrV6, rand.Intn(10)+1)
		excludeIPs := append(excludeIPv4, excludeIPv6...)

		ginkgo.By(fmt.Sprintf("Creating subnet %s with exclude ips %v", subnetName, excludeIPs))
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", excludeIPs, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Validating subnet finalizers")
		framework.ExpectContainElement(subnet.ObjectMeta.Finalizers, util.ControllerName)

		ginkgo.By("Validating subnet spec fields")
		framework.ExpectFalse(subnet.Spec.Default)
		framework.ExpectEqual(subnet.Spec.Protocol, util.CheckProtocol(cidr))
		framework.ExpectEmpty(subnet.Spec.Namespaces)
		framework.ExpectConsistOf(subnet.Spec.ExcludeIps, append(excludeIPs, gateways...))
		framework.ExpectEqual(subnet.Spec.Gateway, strings.Join(gateways, ","))
		framework.ExpectEqual(subnet.Spec.GatewayType, apiv1.GWDistributedType)
		framework.ExpectEmpty(subnet.Spec.GatewayNode)
		framework.ExpectFalse(subnet.Spec.NatOutgoing)
		framework.ExpectFalse(subnet.Spec.Private)
		framework.ExpectEmpty(subnet.Spec.AllowSubnets)

		ginkgo.By("Validating subnet status fields")
		framework.ExpectEmpty(subnet.Status.ActivateGateway)
		framework.ExpectZero(subnet.Status.V4UsingIPs)
		framework.ExpectZero(subnet.Status.V6UsingIPs)

		if cidrV4 == "" {
			framework.ExpectZero(subnet.Status.V4AvailableIPs)
		} else {
			_, ipnet, _ := net.ParseCIDR(cidrV4)
			expected := util.AddressCount(ipnet) - util.CountIpNums(excludeIPv4) - 1
			framework.ExpectEqual(subnet.Status.V4AvailableIPs, expected)
		}
		if cidrV6 == "" {
			framework.ExpectZero(subnet.Status.V6AvailableIPs)
		} else {
			_, ipnet, _ := net.ParseCIDR(cidrV6)
			expected := util.AddressCount(ipnet) - util.CountIpNums(excludeIPv6) - 1
			framework.ExpectEqual(subnet.Status.V6AvailableIPs, expected)
		}
	})

	framework.ConformanceIt("should create subnet with centralized gateway", func() {
		ginkgo.By("Getting nodes")
		nodes, err := e2enode.GetReadySchedulableNodes(cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodes.Items)

		ginkgo.By("Creating subnet " + subnetName)
		gatewayNodes := make([]string, 0, len(nodes.Items))
		for i := 0; i < 3 && i < len(nodes.Items); i++ {
			gatewayNodes = append(gatewayNodes, nodes.Items[i].Name)
		}
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", nil, gatewayNodes, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Validating subnet finalizers")
		framework.ExpectContainElement(subnet.Finalizers, util.ControllerName)

		ginkgo.By("Validating subnet spec fields")
		framework.ExpectFalse(subnet.Spec.Default)
		framework.ExpectEqual(subnet.Spec.Protocol, util.CheckProtocol(cidr))
		framework.ExpectEmpty(subnet.Spec.Namespaces)
		framework.ExpectConsistOf(subnet.Spec.ExcludeIps, gateways)
		framework.ExpectEqual(subnet.Spec.Gateway, strings.Join(gateways, ","))
		framework.ExpectEqual(subnet.Spec.GatewayType, apiv1.GWCentralizedType)
		framework.ExpectConsistOf(strings.Split(subnet.Spec.GatewayNode, ","), gatewayNodes)
		framework.ExpectFalse(subnet.Spec.NatOutgoing)
		framework.ExpectFalse(subnet.Spec.Private)
		framework.ExpectEmpty(subnet.Spec.AllowSubnets)

		ginkgo.By("Validating subnet status fields")
		framework.ExpectContainElement(gatewayNodes, subnet.Status.ActivateGateway)
		framework.ExpectZero(subnet.Status.V4UsingIPs)
		framework.ExpectZero(subnet.Status.V6UsingIPs)

		if cidrV4 == "" {
			framework.ExpectZero(subnet.Status.V4AvailableIPs)
		} else {
			_, ipnet, _ := net.ParseCIDR(cidrV4)
			framework.ExpectEqual(subnet.Status.V4AvailableIPs, util.AddressCount(ipnet)-1)
		}
		if cidrV6 == "" {
			framework.ExpectZero(subnet.Status.V6AvailableIPs)
		} else {
			_, ipnet, _ := net.ParseCIDR(cidrV6)
			framework.ExpectEqual(subnet.Status.V6AvailableIPs, util.AddressCount(ipnet)-1)
		}
	})

	framework.ConformanceIt("should be able to switch gateway mode to centralized", func() {
		ginkgo.By("Getting nodes")
		nodes, err := e2enode.GetReadySchedulableNodes(cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodes.Items)

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Validating subnet finalizers")
		framework.ExpectContainElement(subnet.Finalizers, util.ControllerName)

		ginkgo.By("Validating subnet spec fields")
		framework.ExpectFalse(subnet.Spec.Default)
		framework.ExpectEqual(subnet.Spec.Protocol, util.CheckProtocol(cidr))
		framework.ExpectEmpty(subnet.Spec.Namespaces)
		framework.ExpectConsistOf(subnet.Spec.ExcludeIps, gateways)
		framework.ExpectEqual(subnet.Spec.Gateway, strings.Join(gateways, ","))
		framework.ExpectEqual(subnet.Spec.GatewayType, apiv1.GWDistributedType)
		framework.ExpectEmpty(subnet.Spec.GatewayNode)
		framework.ExpectFalse(subnet.Spec.NatOutgoing)
		framework.ExpectFalse(subnet.Spec.Private)
		framework.ExpectEmpty(subnet.Spec.AllowSubnets)

		ginkgo.By("Validating subnet status fields")
		framework.ExpectEmpty(subnet.Status.ActivateGateway)
		framework.ExpectZero(subnet.Status.V4UsingIPs)
		framework.ExpectZero(subnet.Status.V6UsingIPs)

		if cidrV4 == "" {
			framework.ExpectZero(subnet.Status.V4AvailableIPs)
		} else {
			_, ipnet, _ := net.ParseCIDR(cidrV4)
			framework.ExpectEqual(subnet.Status.V4AvailableIPs, util.AddressCount(ipnet)-1)
		}
		if cidrV6 == "" {
			framework.ExpectZero(subnet.Status.V6AvailableIPs)
		} else {
			_, ipnet, _ := net.ParseCIDR(cidrV6)
			framework.ExpectEqual(subnet.Status.V6AvailableIPs, util.AddressCount(ipnet)-1)
		}

		ginkgo.By("Converting gateway mode to centralized")
		gatewayNodes := make([]string, 0, len(nodes.Items))
		for i := 0; i < 3 && i < len(nodes.Items); i++ {
			gatewayNodes = append(gatewayNodes, nodes.Items[i].Name)
		}
		modifiedSubnet := subnet.DeepCopy()
		modifiedSubnet.Spec.GatewayNode = strings.Join(gatewayNodes, ",")
		modifiedSubnet.Spec.GatewayType = apiv1.GWCentralizedType
		subnet = subnetClient.PatchSync(subnet, modifiedSubnet)

		ginkgo.By("Validating subnet finalizers")
		framework.ExpectContainElement(subnet.ObjectMeta.Finalizers, util.ControllerName)

		ginkgo.By("Validating subnet spec fields")
		framework.ExpectFalse(subnet.Spec.Default)
		framework.ExpectEqual(subnet.Spec.Protocol, util.CheckProtocol(cidr))
		framework.ExpectEmpty(subnet.Spec.Namespaces)
		framework.ExpectConsistOf(subnet.Spec.ExcludeIps, gateways)
		framework.ExpectEqual(subnet.Spec.Gateway, strings.Join(gateways, ","))
		framework.ExpectEqual(subnet.Spec.GatewayType, apiv1.GWCentralizedType)
		framework.ExpectConsistOf(strings.Split(subnet.Spec.GatewayNode, ","), gatewayNodes)
		framework.ExpectFalse(subnet.Spec.NatOutgoing)
		framework.ExpectFalse(subnet.Spec.Private)
		framework.ExpectEmpty(subnet.Spec.AllowSubnets)

		ginkgo.By("Validating subnet status fields")
		framework.ExpectContainElement(gatewayNodes, subnet.Status.ActivateGateway)
		framework.ExpectZero(subnet.Status.V4UsingIPs)
		framework.ExpectZero(subnet.Status.V6UsingIPs)

		if cidrV4 == "" {
			framework.ExpectZero(subnet.Status.V4AvailableIPs)
		} else {
			_, ipnet, _ := net.ParseCIDR(cidrV4)
			framework.ExpectEqual(subnet.Status.V4AvailableIPs, util.AddressCount(ipnet)-1)
		}
		if cidrV6 == "" {
			framework.ExpectZero(subnet.Status.V6AvailableIPs)
		} else {
			_, ipnet, _ := net.ParseCIDR(cidrV6)
			framework.ExpectEqual(subnet.Status.V6AvailableIPs, util.AddressCount(ipnet)-1)
		}
	})

	framework.ConformanceIt("create centralized subnet without enableEcmp", func() {
		f.SkipVersionPriorTo(1, 12, "Support for enableEcmp in subnet is introduced in v1.12")

		ginkgo.By("Getting nodes")
		nodes, err := e2enode.GetReadySchedulableNodes(cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodes.Items)

		ginkgo.By("Creating subnet " + subnetName)
		gatewayNodes := make([]string, 0, len(nodes.Items))
		nodeIPs := make([]string, 0, len(nodes.Items))
		for i := 0; i < 3 && i < len(nodes.Items); i++ {
			gatewayNodes = append(gatewayNodes, nodes.Items[i].Name)
			nodeIPs = append(nodeIPs, nodes.Items[i].Annotations[util.IpAddressAnnotation])
		}
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", nil, gatewayNodes, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Validating subnet finalizers")
		framework.ExpectContainElement(subnet.Finalizers, util.ControllerName)

		ginkgo.By("Validating centralized subnet with active-standby mode")
		framework.ExpectFalse(subnet.Spec.EnableEcmp)
		framework.ExpectEqual(subnet.Status.ActivateGateway, gatewayNodes[0])
		framework.ExpectConsistOf(strings.Split(subnet.Spec.GatewayNode, ","), gatewayNodes)

		ginkgo.By("change subnet spec field enableEcmp to true")
		modifiedSubnet := subnet.DeepCopy()
		modifiedSubnet.Spec.EnableEcmp = true
		subnet = subnetClient.PatchSync(subnet, modifiedSubnet)
		framework.ExpectEmpty(subnet.Status.ActivateGateway)
		time.Sleep(2 * time.Second)

		execCmd := "kubectl ko nbctl --format=csv --data=bare --no-heading --columns=nexthops find logical-router-policy " + fmt.Sprintf("external_ids:subnet=%s", subnetName)
		output, err := exec.Command("bash", "-c", execCmd).CombinedOutput()
		framework.ExpectNoError(err)

		lines := strings.Split(string(output), "\n")
		nextHops := make([]string, 0, len(lines))
		for _, l := range lines {
			if len(strings.TrimSpace(l)) == 0 {
				continue
			}
			nextHops = strings.Fields(l)
		}
		framework.Logf("subnet policy route nextHops %v, gatewayNode IPs %v", nextHops, nodeIPs)

		check := true
		if len(nextHops) < len(nodeIPs) {
			framework.Logf("some gateway nodes maybe not ready for subnet %s", subnetName)
			check = false
		}

		if check {
			for _, nodeIP := range nodeIPs {
				for _, strIP := range strings.Split(nodeIP, ",") {
					if util.CheckProtocol(strIP) != util.CheckProtocol(nextHops[0]) {
						continue
					}
					framework.ExpectContainElement(nextHops, strIP)
				}
			}
		}
	})

	framework.ConformanceIt("should support distributed external egress gateway", func() {
		ginkgo.By("Getting nodes")
		nodes, err := e2enode.GetReadySchedulableNodes(cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodes.Items)

		clusterName, ok := kind.IsKindProvided(nodes.Items[0].Spec.ProviderID)
		if !ok {
			ginkgo.Skip("external egress gateway spec only runs in clusters created by kind")
		}

		ginkgo.By("Getting docker network used by kind")
		network, err := docker.NetworkInspect(kind.NetworkName)
		framework.ExpectNoError(err)

		ginkgo.By("Determine external egress gateway addresses")
		gateways := make([]string, 0, 2)
		for _, config := range network.IPAM.Config {
			if config.Subnet != "" {
				switch util.CheckProtocol(config.Subnet) {
				case apiv1.ProtocolIPv4:
					if cidrV4 != "" {
						gateway, err := util.LastIP(config.Subnet)
						framework.ExpectNoError(err)
						gateways = append(gateways, gateway)
					}
				case apiv1.ProtocolIPv6:
					if cidrV6 != "" {
						gateway, err := util.LastIP(config.Subnet)
						framework.ExpectNoError(err)
						gateways = append(gateways, gateway)
					}
				}
			}
		}

		ginkgo.By("Creating subnet " + subnetName)
		prPriority := 1000 + rand.Intn(1000)
		prTable := 1000 + rand.Intn(1000)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", nil, nil, nil)
		subnet.Spec.ExternalEgressGateway = strings.Join(gateways, ",")
		subnet.Spec.PolicyRoutingPriority = uint32(prPriority)
		subnet.Spec.PolicyRoutingTableID = uint32(prTable)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating pod")
		podName := "pod-" + framework.RandomSuffix()
		pod := framework.MakePod(namespaceName, podName, nil, nil, "", nil, nil)
		pod = podClient.CreateSync(pod)

		ginkgo.By("Getting kind nodes")
		kindNodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(kindNodes)

		for _, node := range kindNodes {
			ginkgo.By("Getting ip rules in node " + node.Name())
			rules, err := iproute.RuleShow("", node.Exec)
			framework.ExpectNoError(err)

			ginkgo.By("Checking ip rules in node " + node.Name())
			podIPs := make([]string, 0, len(pod.Status.PodIPs))
			for _, podIP := range pod.Status.PodIPs {
				podIPs = append(podIPs, podIP.IP)
			}
			for _, rule := range rules {
				if rule.Priority == prPriority &&
					rule.Table == strconv.Itoa(prTable) {
					framework.ExpectEqual(pod.Spec.NodeName, node.Name())
					framework.ExpectContainElement(podIPs, rule.Src)
					framework.ExpectEqual(rule.SrcLen, 0)
				}
			}

			if pod.Spec.NodeName != node.Name() {
				continue
			}

			ginkgo.By("Getting ip routes in node " + node.Name())
			routes, err := iproute.RouteShow(strconv.Itoa(prTable), "", node.Exec)
			framework.ExpectNoError(err)

			ginkgo.By("Checking ip routes in node " + node.Name())
			framework.ExpectHaveLen(routes, len(gateways))
			nexthops := make([]string, 0, 2)
			for _, route := range routes {
				framework.ExpectEqual(route.Dst, "default")
				nexthops = append(nexthops, route.Gateway)
			}
			framework.ExpectConsistOf(nexthops, gateways)
		}
	})

	framework.ConformanceIt("should support centralized external egress gateway", func() {
		ginkgo.By("Getting nodes")
		nodes, err := e2enode.GetReadySchedulableNodes(cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodes.Items)

		clusterName, ok := kind.IsKindProvided(nodes.Items[0].Spec.ProviderID)
		if !ok {
			ginkgo.Skip("external egress gateway spec only runs in clusters created by kind")
		}

		ginkgo.By("Getting docker network used by kind")
		network, err := docker.NetworkInspect(kind.NetworkName)
		framework.ExpectNoError(err)

		ginkgo.By("Determine external egress gateway addresses")
		cidrs := make([]string, 0, 2)
		gateways := make([]string, 0, 2)
		for _, config := range network.IPAM.Config {
			if config.Subnet != "" {
				switch util.CheckProtocol(config.Subnet) {
				case apiv1.ProtocolIPv4:
					if cidrV4 != "" {
						gateway, err := util.LastIP(config.Subnet)
						framework.ExpectNoError(err)
						cidrs = append(cidrs, cidrV4)
						gateways = append(gateways, gateway)
					}
				case apiv1.ProtocolIPv6:
					if cidrV6 != "" {
						gateway, err := util.LastIP(config.Subnet)
						framework.ExpectNoError(err)
						cidrs = append(cidrs, cidrV6)
						gateways = append(gateways, gateway)
					}
				}
			}
		}

		ginkgo.By("Creating subnet " + subnetName)
		gatewayNodes := make([]string, 0, len(nodes.Items))
		for i := 0; i < 3 && i < len(nodes.Items); i++ {
			gatewayNodes = append(gatewayNodes, nodes.Items[i].Name)
		}
		prPriority := 1000 + rand.Intn(1000)
		prTable := 1000 + rand.Intn(1000)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", nil, gatewayNodes, nil)
		subnet.Spec.ExternalEgressGateway = strings.Join(gateways, ",")
		subnet.Spec.PolicyRoutingPriority = uint32(prPriority)
		subnet.Spec.PolicyRoutingTableID = uint32(prTable)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Getting kind nodes")
		kindNodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(kindNodes)

		for _, node := range kindNodes {
			shouldHavePolicyRoute := util.ContainsString(gatewayNodes, node.Name())
			ginkgo.By("Getting ip rules in node " + node.Name())
			rules, err := iproute.RuleShow("", node.Exec)
			framework.ExpectNoError(err)

			ginkgo.By("Checking ip rules in node " + node.Name())
			var found int
			for _, rule := range rules {
				if rule.Priority == prPriority &&
					rule.Table == strconv.Itoa(prTable) {
					framework.ExpectContainElement(cidrs, fmt.Sprintf("%s/%d", rule.Src, rule.SrcLen))
					found++
				}
			}
			if !shouldHavePolicyRoute {
				framework.ExpectZero(found)
				continue
			}
			framework.ExpectEqual(found, len(gateways))

			ginkgo.By("Getting ip routes in node " + node.Name())
			routes, err := iproute.RouteShow(strconv.Itoa(prTable), "", node.Exec)
			framework.ExpectNoError(err)

			ginkgo.By("Checking ip routes in node " + node.Name())
			framework.ExpectHaveLen(routes, len(gateways))
			nexthops := make([]string, 0, 2)
			for _, route := range routes {
				framework.ExpectEqual(route.Dst, "default")
				nexthops = append(nexthops, route.Gateway)
			}
			framework.ExpectConsistOf(nexthops, gateways)
		}
	})

	framework.ConformanceIt("should support subnet AvailableIPRange and UsingIPRange creating pod no specify ip", func() {
		f.SkipVersionPriorTo(1, 12, "Support for display AvailableIPRange and UsingIPRange in v1.12")
		podCount = 5
		podNamePre = "sample"
		var startIPv4, startIPv6 string
		if firstIPv4 != "" {
			startIPv4 = util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(firstIPv4), big.NewInt(1)))
		}
		if firstIPv6 != "" {
			startIPv6 = util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(firstIPv6), big.NewInt(1)))
		}

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating pod with no specify pod ip ")
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		for i := 1; i <= podCount; i++ {
			pod := framework.MakePod("", fmt.Sprintf("%s%d", podNamePre, i), nil, annotations, "", nil, nil)
			podClient.CreateSync(pod)
		}

		subnet = subnetClient.Get(subnetName)
		if cidrV4 != "" {
			v4UsingIPEnd := util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(startIPv4), big.NewInt(int64(podCount-1))))
			v4AvailableIPStart := util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(v4UsingIPEnd), big.NewInt(1)))
			framework.ExpectEqual(subnet.Status.V4UsingIPRange, fmt.Sprintf("%s-%s", startIPv4, v4UsingIPEnd))
			framework.ExpectEqual(subnet.Status.V4AvailableIPRange, fmt.Sprintf("%s-%s", v4AvailableIPStart, lastIPv4))
		}

		if cidrV6 != "" {
			v6UsingIPEnd := util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(startIPv6), big.NewInt(int64(podCount-1))))
			v6AvailableIPStart := util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(v6UsingIPEnd), big.NewInt(1)))
			framework.ExpectEqual(subnet.Status.V6UsingIPRange, fmt.Sprintf("%s-%s", startIPv6, v6UsingIPEnd))
			framework.ExpectEqual(subnet.Status.V6AvailableIPRange, fmt.Sprintf("%s-%s", v6AvailableIPStart, lastIPv6))
		}

		ginkgo.By("delete all pods")
		for i := 1; i <= podCount; i++ {
			podClient.DeleteSync(fmt.Sprintf("%s%d", podNamePre, i))
		}

		subnet = subnetClient.Get(subnetName)
		if cidrV4 != "" {
			framework.ExpectEqual(subnet.Status.V4UsingIPRange, "")
			framework.ExpectEqual(subnet.Status.V4AvailableIPRange, fmt.Sprintf("%s-%s", startIPv4, lastIPv4))
		}

		if cidrV6 != "" {
			framework.ExpectEqual(subnet.Status.V6UsingIPRange, "")
			framework.ExpectEqual(subnet.Status.V6AvailableIPRange, fmt.Sprintf("%s-%s", startIPv6, lastIPv6))
		}
	})

	framework.ConformanceIt("should support subnet AvailableIPRange and UsingIPRange creating pod specify ip", func() {
		f.SkipVersionPriorTo(1, 12, "Support for display AvailableIPRange and UsingIPRange in v1.12")
		podCount = 5
		podNamePre = "sample"
		var startIPv4, startIPv6, usingIPv4Str, availableIPv4Str, usingIPv6Str, availableIPv6Str string

		if firstIPv4 != "" {
			startIPv4 = util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(firstIPv4), big.NewInt(1)))
		}
		if firstIPv6 != "" {
			startIPv6 = util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(firstIPv6), big.NewInt(1)))
		}

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating pod with specify pod ip ")
		podIPv4s, podIPv6s := createPodsByRandomIPs(podClient, subnetClient, subnetName, podNamePre, podCount, startIPv4, startIPv6)
		subnet = subnetClient.Get(subnetName)

		if podIPv4s != nil {
			usingIPv4Str, availableIPv4Str = calcuIPRangeListStr(podIPv4s, startIPv4, lastIPv4)
			framework.ExpectEqual(subnet.Status.V4UsingIPRange, usingIPv4Str)
			framework.ExpectEqual(subnet.Status.V4AvailableIPRange, availableIPv4Str)
		}

		if podIPv6s != nil {
			usingIPv6Str, availableIPv6Str = calcuIPRangeListStr(podIPv6s, startIPv6, lastIPv6)
			framework.ExpectEqual(subnet.Status.V6UsingIPRange, usingIPv6Str)
			framework.ExpectEqual(subnet.Status.V6AvailableIPRange, availableIPv6Str)
		}

		ginkgo.By("delete all pods")
		for i := 1; i <= podCount; i++ {
			podClient.DeleteSync(fmt.Sprintf("%s%d", podNamePre, i))
		}

		subnet = subnetClient.Get(subnetName)
		if cidrV4 != "" {
			framework.ExpectEqual(subnet.Status.V4UsingIPRange, "")
			framework.ExpectEqual(subnet.Status.V4AvailableIPRange, fmt.Sprintf("%s-%s", startIPv4, lastIPv4))
		}

		if cidrV6 != "" {
			framework.ExpectEqual(subnet.Status.V6UsingIPRange, "")
			framework.ExpectEqual(subnet.Status.V6AvailableIPRange, fmt.Sprintf("%s-%s", startIPv6, lastIPv6))
		}
	})

	framework.ConformanceIt("should support subnet AvailableIPRange and UsingIPRange is correct when restart deployment", func() {
		f.SkipVersionPriorTo(1, 12, "Support for display AvailableIPRange and UsingIPRange in v1.12")

		var startIPv4, startIPv6 string
		if firstIPv4 != "" {
			startIPv4 = util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(firstIPv4), big.NewInt(1)))
		}
		if firstIPv6 != "" {
			startIPv6 = util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(firstIPv6), big.NewInt(1)))
		}

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		replicas := int64(5)
		deployName = "deployment-" + framework.RandomSuffix()
		labels := map[string]string{"app": deployName}
		deploy := deployment.NewDeployment(deployName, int32(replicas), labels, "pause", framework.PauseImage, "")
		deploy.Spec.Template.Annotations = map[string]string{util.LogicalSwitchAnnotation: subnetName}
		deploy, err := cs.AppsV1().Deployments(namespaceName).Create(context.TODO(), deploy, metav1.CreateOptions{})
		framework.ExpectNoError(err, "failed to to create deployment")
		err = deployment.WaitForDeploymentComplete(cs, deploy)
		framework.ExpectNoError(err, "deployment failed to complete")

		checkFunc := func(usingIPRange, availableIPRange, startIP, lastIP string, count int64) {
			usingIPEnd := util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(startIP), big.NewInt(count-1)))
			availableIPStart := util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(usingIPEnd), big.NewInt(1)))
			framework.ExpectEqual(usingIPRange, fmt.Sprintf("%s-%s", startIP, usingIPEnd))
			framework.ExpectEqual(availableIPRange, fmt.Sprintf("%s-%s", availableIPStart, lastIP))
		}

		subnet = subnetClient.Get(subnetName)
		if cidrV4 != "" {
			checkFunc(subnet.Status.V4UsingIPRange, subnet.Status.V4AvailableIPRange, startIPv4, lastIPv4, replicas)
		}

		if cidrV6 != "" {
			checkFunc(subnet.Status.V6UsingIPRange, subnet.Status.V6AvailableIPRange, startIPv6, lastIPv6, replicas)
		}

		ginkgo.By("restart deployment ")
		restartCmd := fmt.Sprintf("kubectl rollout restart deployment %s -n %s", deployName, namespaceName)
		_, err = exec.Command("bash", "-c", restartCmd).CombinedOutput()
		framework.ExpectNoError(err, "restart deployment failed")
		err = deployment.WaitForDeploymentComplete(cs, deploy)
		framework.ExpectNoError(err, "deployment failed to complete")

		checkFunc2 := func(usingIPRange, availableIPRange, startIP, lastIP string, count int64, isFrameworkCheck bool) bool {
			expectAvailIPRangeStr := fmt.Sprintf("%s-%s,%s-%s",
				startIP,
				util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(startIP), big.NewInt(count-1))),
				util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(startIP), big.NewInt(2*count))),
				lastIP,
			)

			expectUsingIPRangeStr := fmt.Sprintf("%s-%s",
				util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(startIP), big.NewInt(count))),
				util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(startIP), big.NewInt(2*count-1))),
			)

			fmt.Printf("subnet status usingIPRange %s expect %s \n", usingIPRange, expectUsingIPRangeStr)
			fmt.Printf("subnet status availableIPRange %s expect %s \n", availableIPRange, expectAvailIPRangeStr)

			if isFrameworkCheck {
				framework.ExpectEqual(usingIPRange, expectUsingIPRangeStr)
				framework.ExpectEqual(availableIPRange, expectAvailIPRangeStr)
				return false
			} else {
				return usingIPRange == expectUsingIPRangeStr && availableIPRange == expectAvailIPRangeStr
			}
		}

		checkTimes := 0
		isSuccess := false
		maxRetryTimes := 30
		for {
			time.Sleep(1 * time.Second)
			if checkTimes > maxRetryTimes || isSuccess {
				break
			}
			subnet = subnetClient.Get(subnetName)
			if cidrV4 != "" {
				isSuccess = checkFunc2(subnet.Status.V4UsingIPRange, subnet.Status.V4AvailableIPRange, startIPv4, lastIPv4, replicas, false)
			}

			if cidrV6 != "" {
				isSuccess = checkFunc2(subnet.Status.V6UsingIPRange, subnet.Status.V6AvailableIPRange, startIPv6, lastIPv6, replicas, false)
			}
			checkTimes++
		}

		if cidrV4 != "" {
			checkFunc2(subnet.Status.V4UsingIPRange, subnet.Status.V4AvailableIPRange, startIPv4, lastIPv4, replicas, true)
		}

		if cidrV6 != "" {
			checkFunc2(subnet.Status.V6UsingIPRange, subnet.Status.V6AvailableIPRange, startIPv6, lastIPv6, replicas, true)
		}
	})

	framework.ConformanceIt("create subnet with enableLb option", func() {
		f.SkipVersionPriorTo(1, 12, "Support for enableLb in subnet is introduced in v1.12")

		enbleLb := true
		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", nil, nil, nil)
		subnet.Spec.EnableLb = &enbleLb
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Validating subnet load-balancer records exist")
		framework.ExpectContainElement(subnet.Finalizers, util.ControllerName)
		execCmd := "kubectl ko nbctl --format=csv --data=bare --no-heading --columns=load_balancer find logical-switch " + fmt.Sprintf("name=%s", subnetName)
		output, err := exec.Command("bash", "-c", execCmd).CombinedOutput()
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(strings.TrimSpace(string(output)))

		ginkgo.By("Validating change subnet spec enableLb to false")
		enbleLb = false
		modifiedSubnet := subnet.DeepCopy()
		modifiedSubnet.Spec.EnableLb = &enbleLb
		subnet = subnetClient.PatchSync(subnet, modifiedSubnet)
		execCmd = "kubectl ko nbctl --format=csv --data=bare --no-heading --columns=load_balancer find logical-switch " + fmt.Sprintf("name=%s", subnetName)
		output, err = exec.Command("bash", "-c", execCmd).CombinedOutput()
		framework.ExpectNoError(err)
		framework.ExpectEmpty(strings.TrimSpace(string(output)))

		ginkgo.By("Validating empty subnet spec enableLb field, should keep same value as args enableLb")
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.EnableLb = nil
		subnet = subnetClient.PatchSync(subnet, modifiedSubnet)
		execCmd = "kubectl ko nbctl --format=csv --data=bare --no-heading --columns=load_balancer find logical-switch " + fmt.Sprintf("name=%s", subnetName)
		output, err = exec.Command("bash", "-c", execCmd).CombinedOutput()
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(strings.TrimSpace(string(output)))
	})
})

func createPodsByRandomIPs(podClient *framework.PodClient, subnetClient *framework.SubnetClient, subnetName, podNamePre string, podCount int, startIPv4, startIPv6 string) ([]string, []string) {
	var allocIP string
	var podIPv4s, podIPv6s []string
	podv4IP := startIPv4
	podv6IP := startIPv6

	subnet := subnetClient.Get(subnetName)
	for i := 1; i <= podCount; i++ {
		step := rand.Int63()%10 + 2
		if subnet.Spec.Protocol == apiv1.ProtocolIPv4 {
			podv4IP = util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(podv4IP), big.NewInt(step)))
			allocIP = podv4IP
		} else if subnet.Spec.Protocol == apiv1.ProtocolIPv6 {
			podv6IP = util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(podv6IP), big.NewInt(step)))
			allocIP = podv6IP
		} else {
			podv4IP = util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(podv4IP), big.NewInt(step)))
			podv6IP = util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(podv6IP), big.NewInt(step)))
			allocIP = fmt.Sprintf("%s,%s", podv4IP, podv6IP)
		}

		annotations := map[string]string{
			util.LogicalSwitchAnnotation:                                        subnetName,
			fmt.Sprintf(util.IpAddressAnnotationTemplate, subnet.Spec.Provider): allocIP,
		}

		podName := fmt.Sprintf("%s%d", podNamePre, i)
		pod := framework.MakePod("", podName, nil, annotations, "", nil, nil)
		podClient.CreateSync(pod)

		if podv4IP != "" {
			podIPv4s = append(podIPv4s, podv4IP)
		}
		if podv6IP != "" {
			podIPv6s = append(podIPv6s, podv6IP)
		}
	}

	return podIPv4s, podIPv6s
}

func calcuIPRangeListStr(podIPs []string, startIP, lastIP string) (string, string) {
	var usingIPs, availableIPs []string
	var usingIPStr, availableIPStr, prePodIP string

	for index, podIP := range podIPs {
		usingIPs = append(usingIPs, podIP)
		if index == 0 {
			availableIPs = append(availableIPs, fmt.Sprintf("%s-%s", startIP, util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(podIP), big.NewInt(-1)))))
		} else {
			preIP := prePodIP
			start := util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(preIP), big.NewInt(1)))
			end := util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(podIP), big.NewInt(-1)))

			if start == end {
				availableIPs = append(availableIPs, start)
			} else {
				availableIPs = append(availableIPs, fmt.Sprintf("%s-%s", start, end))
			}
		}
		prePodIP = podIP
	}

	if prePodIP != "" {
		availableIPs = append(availableIPs, fmt.Sprintf("%s-%s", util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(prePodIP), big.NewInt(1))), lastIP))
	}

	usingIPStr = strings.Join(usingIPs, ",")
	availableIPStr = strings.Join(availableIPs, ",")
	fmt.Printf("usingIPStr is %s ", usingIPStr)
	fmt.Printf("availableIPStr is %s ", availableIPStr)
	return usingIPStr, availableIPStr
}
