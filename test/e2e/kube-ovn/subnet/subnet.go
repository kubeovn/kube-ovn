package subnet

import (
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"

	clientset "k8s.io/client-go/kubernetes"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	"github.com/onsi/ginkgo/v2"

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
	var cidr, cidrV4, cidrV6, firstIPv4, firstIPv6 string
	var gateways []string

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		namespaceName = f.Namespace.Name
		subnetName = "subnet-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIpFamily)
		cidrV4, cidrV6 = util.SplitStringIP(cidr)
		gateways = nil
		if cidrV4 == "" {
			firstIPv4 = ""
		} else {
			firstIPv4, _ = util.FirstIP(cidrV4)
			gateways = append(gateways, firstIPv4)
		}
		if cidrV6 == "" {
			firstIPv6 = ""
		} else {
			firstIPv6, _ = util.FirstIP(cidrV6)
			gateways = append(gateways, firstIPv6)
		}
	})
	ginkgo.AfterEach(func() {
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
})
