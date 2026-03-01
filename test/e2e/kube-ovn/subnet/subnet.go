package subnet

import (
	"context"
	"fmt"
	"math/big"
	"math/rand/v2"
	"net"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iproute"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iptables"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

func getOvsPodOnNode(f *framework.Framework, node string) *corev1.Pod {
	ginkgo.GinkgoHelper()

	daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
	ds := daemonSetClient.Get("ovs-ovn")
	pod, err := daemonSetClient.GetPodOnNode(ds, node)
	framework.ExpectNoError(err)
	return pod
}

func checkSubnetNatOutgoingPolicyRuleStatus(subnetClient *framework.SubnetClient, subnetName string, rules []apiv1.NatOutgoingPolicyRule) *apiv1.Subnet {
	ginkgo.GinkgoHelper()

	ginkgo.By("Waiting for status of subnet " + subnetName + " to be updated")
	var subnet *apiv1.Subnet
	framework.WaitUntil(time.Second, 10*time.Second, func(_ context.Context) (bool, error) {
		s := subnetClient.Get(subnetName)
		if len(s.Status.NatOutgoingPolicyRules) != len(rules) {
			return false, nil
		}
		for i, r := range s.Status.NatOutgoingPolicyRules {
			if r.RuleID == "" || r.NatOutgoingPolicyRule != rules[i] {
				return false, nil
			}
		}
		subnet = s
		return true, nil
	}, "")
	return subnet
}

func checkIPSetOnNode(f *framework.Framework, node string, expectedIPsets []string, shouldExist bool) {
	ginkgo.GinkgoHelper()

	ovsPod := getOvsPodOnNode(f, node)
	cmd := `ipset list | grep '^Name:' | awk '{print $2}'`
	framework.WaitUntil(3*time.Second, 10*time.Second, func(_ context.Context) (bool, error) {
		output := e2epodoutput.RunHostCmdOrDie(ovsPod.Namespace, ovsPod.Name, cmd)
		exitIPsets := strings.Split(output, "\n")
		for _, r := range expectedIPsets {
			framework.Logf("checking ipset %s: %v", r, shouldExist)
			ok, err := gomega.ContainElement(r).Match(exitIPsets)
			if err != nil || ok != shouldExist {
				return false, err
			}
		}
		return true, nil
	}, "")
}

var _ = framework.Describe("[group:subnet]", func() {
	f := framework.NewDefaultFramework("subnet")

	var subnet *apiv1.Subnet
	var cs clientset.Interface
	var podClient *framework.PodClient
	var deployClient *framework.DeploymentClient
	var subnetClient *framework.SubnetClient
	var eventClient *framework.EventClient
	var namespaceName, subnetName, fakeSubnetName, podNamePrefix, deployName, podName string
	var cidr, cidrV4, cidrV6, firstIPv4, lastIPv4, firstIPv6, lastIPv6 string
	var gateways []string
	var podCount int

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		podClient = f.PodClient()
		deployClient = f.DeploymentClient()
		subnetClient = f.SubnetClient()
		namespaceName = f.Namespace.Name
		subnetName = "subnet-" + framework.RandomSuffix()
		fakeSubnetName = "subnet-" + framework.RandomSuffix()
		deployName = "deploy-" + framework.RandomSuffix()
		podNamePrefix = "pod-" + framework.RandomSuffix()
		podName = "pod-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIPFamily)
		cidrV4, cidrV6 = util.SplitStringIP(cidr)
		gateways = nil
		podCount = 0
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
		ginkgo.By("Deleting deployment " + deployName)
		deployClient.DeleteSync(deployName)

		for i := 1; i <= podCount; i++ {
			podName := fmt.Sprintf("%s-%d", podNamePrefix, i)
			ginkgo.By("Deleting pod " + podName)
			podClient.DeleteSync(podName)
		}

		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)

		ginkgo.By("Deleting subnet " + fakeSubnetName)
		subnetClient.DeleteSync(fakeSubnetName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
	})

	framework.ConformanceIt("should create subnet with only cidr provided", func() {
		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Validating subnet finalizers")
		f.ValidateFinalizers(subnet)

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
			ipnet.IP = net.ParseIP(framework.RandomIPs(cidr, ";", 1))
			return ipnet.String()
		}

		s := make([]string, 0, 2)
		if c := fn(cidrV4); c != "" {
			s = append(s, c)
		}
		if c := fn(cidrV6); c != "" {
			s = append(s, c)
		}

		subnet = framework.MakeSubnet(subnetName, "", strings.Join(s, ","), "", "", "", nil, nil, nil)
		ginkgo.By("Creating subnet " + subnetName + " with cidr " + subnet.Spec.CIDRBlock)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Validating subnet finalizers")
		f.ValidateFinalizers(subnet)

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
		excludeIPv4 := framework.RandomExcludeIPs(cidrV4, rand.IntN(10)+1)
		excludeIPv6 := framework.RandomExcludeIPs(cidrV6, rand.IntN(10)+1)
		excludeIPs := append(excludeIPv4, excludeIPv6...)

		ginkgo.By(fmt.Sprintf("Creating subnet %s with exclude ips %v", subnetName, excludeIPs))
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", excludeIPs, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Validating subnet finalizers")
		f.ValidateFinalizers(subnet)

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
			expected := util.AddressCount(ipnet) - util.CountIPNums(excludeIPv4) - 1
			framework.ExpectEqual(subnet.Status.V4AvailableIPs, expected)
		}
		if cidrV6 == "" {
			framework.ExpectZero(subnet.Status.V6AvailableIPs)
		} else {
			_, ipnet, _ := net.ParseCIDR(cidrV6)
			expected := util.AddressCount(ipnet) - util.CountIPNums(excludeIPv6) - 1
			framework.ExpectEqual(subnet.Status.V6AvailableIPs, expected)
		}
	})

	framework.ConformanceIt("should allow pod with fixed IP or IP pool in excludeIPs when available IPs is 0", func() {
		ginkgo.By("Creating a small subnet with very limited IP range")
		var smallCIDR string
		var excludeIPs []string
		var usableIPs []string

		switch f.ClusterIPFamily {
		case "ipv4":
			smallCIDR = "192.168.200.0/30"
			excludeIPs = []string{"192.168.200.1", "192.168.200.2"}
			usableIPs = []string{"192.168.200.2"}
		case "ipv6":
			smallCIDR = "fd00:192:168:200::/126"
			excludeIPs = []string{"fd00:192:168:200::1", "fd00:192:168:200::2"}
			usableIPs = []string{"fd00:192:168:200::2"}
		case "dual":
			smallCIDR = "192.168.200.0/30,fd00:192:168:200::/126"
			excludeIPs = []string{"192.168.200.1", "192.168.200.2", "fd00:192:168:200::1", "fd00:192:168:200::2"}
			usableIPs = []string{"192.168.200.2", "fd00:192:168:200::2"}
		}

		subnetName = "small-subnet-" + framework.RandomSuffix()
		ginkgo.By(fmt.Sprintf("Creating small subnet %s with exclude IPs %v", subnetName, excludeIPs))
		smallSubnet := framework.MakeSubnet(subnetName, "", smallCIDR, "", "", "", excludeIPs, nil, []string{namespaceName})
		smallSubnet = subnetClient.CreateSync(smallSubnet)

		ginkgo.By("Verifying available IPs is 0 after excluding the only usable IPs")
		framework.ExpectZero(smallSubnet.Status.V4AvailableIPs + smallSubnet.Status.V6AvailableIPs)

		// Test cases: both fixed IP and IP pool annotations
		testCases := []struct {
			name            string
			annotationKey   string
			annotationValue string
		}{
			{
				name:            "fix ip",
				annotationKey:   util.IPAddressAnnotation,
				annotationValue: strings.Join(usableIPs, ","),
			},
			{
				name:            "fix ip pool",
				annotationKey:   util.IPPoolAnnotation,
				annotationValue: strings.Join(usableIPs, ","),
			},
		}

		for _, tc := range testCases {
			ginkgo.By(fmt.Sprintf("Creating pod with %s annotation that matches excludeIPs", tc.name))
			podName = fmt.Sprintf("pod-%s-%s", strings.ReplaceAll(tc.name, " ", "-"), framework.RandomSuffix())
			annotations := map[string]string{
				tc.annotationKey: tc.annotationValue,
			}
			cmd := []string{"sleep", "infinity"}
			pod := framework.MakePrivilegedPod(namespaceName, podName, nil, annotations, f.KubeOVNImage, cmd, nil)
			pod = podClient.CreateSync(pod)

			ginkgo.By(fmt.Sprintf("Verifying pod gets the %s IPs despite availableIPs being 0", tc.name))
			framework.ExpectHaveKeyWithValue(pod.Annotations, tc.annotationKey, tc.annotationValue)

			ginkgo.By(fmt.Sprintf("Cleaning up test pod for %s", tc.name))
			podClient.DeleteSync(podName)
		}
	})

	framework.ConformanceIt("should create subnet with centralized gateway", func() {
		ginkgo.By("Getting nodes")
		nodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodes.Items)

		ginkgo.By("Creating subnet " + subnetName)
		n := min(3, max(1, len(nodes.Items)-1))
		gatewayNodes := make([]string, 0, n)
		nodeIPs := make([]string, 0, n*2)
		var ipv4, ipv6 string
		for i := range n {
			gatewayNodes = append(gatewayNodes, nodes.Items[i].Name)
			if f.VersionPriorTo(1, 12) {
				ipv4, ipv6 = util.SplitStringIP(nodes.Items[i].Annotations[util.IPAddressAnnotation])
			} else {
				ipv4, ipv6 = util.GetNodeInternalIP(nodes.Items[i])
			}
			nodeIPs = append(nodeIPs, strings.Split(strings.Trim(ipv4+","+ipv6, ","), ",")...)
		}
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, gatewayNodes, []string{namespaceName})
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Validating subnet finalizers")
		f.ValidateFinalizers(subnet)

		ginkgo.By("Validating subnet spec fields")
		framework.ExpectFalse(subnet.Spec.Default)
		framework.ExpectEqual(subnet.Spec.Protocol, util.CheckProtocol(cidr))
		framework.ExpectConsistOf(subnet.Spec.Namespaces, []string{namespaceName})
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

		ginkgo.By("Creating pod " + podName)
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePrivilegedPod(namespaceName, podName, nil, nil, f.KubeOVNImage, cmd, nil)
		_ = podClient.CreateSync(pod)

		framework.CheckPodEgressRoutes(namespaceName, podName, f.HasIPv4(), f.HasIPv6(), 2, nodeIPs)
	})

	framework.ConformanceIt("should be able to switch gateway mode to centralized", func() {
		ginkgo.By("Getting nodes")
		nodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodes.Items)

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Validating subnet finalizers")
		f.ValidateFinalizers(subnet)

		ginkgo.By("Validating subnet spec fields")
		framework.ExpectFalse(subnet.Spec.Default)
		framework.ExpectEqual(subnet.Spec.Protocol, util.CheckProtocol(cidr))
		framework.ExpectConsistOf(subnet.Spec.Namespaces, []string{namespaceName})
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
		n := min(3, max(1, len(nodes.Items)-1))
		gatewayNodes := make([]string, 0, n)
		nodeIPs := make([]string, 0, n*2)
		var ipv4, ipv6 string
		for i := range n {
			gatewayNodes = append(gatewayNodes, nodes.Items[i].Name)
			if f.VersionPriorTo(1, 12) {
				ipv4, ipv6 = util.SplitStringIP(nodes.Items[i].Annotations[util.IPAddressAnnotation])
			} else {
				ipv4, ipv6 = util.GetNodeInternalIP(nodes.Items[i])
			}
			nodeIPs = append(nodeIPs, strings.Split(strings.Trim(ipv4+","+ipv6, ","), ",")...)
		}
		modifiedSubnet := subnet.DeepCopy()
		modifiedSubnet.Spec.GatewayNode = strings.Join(gatewayNodes, ",")
		modifiedSubnet.Spec.GatewayType = apiv1.GWCentralizedType
		subnet = subnetClient.PatchSync(subnet, modifiedSubnet)

		ginkgo.By("Validating subnet finalizers")
		f.ValidateFinalizers(subnet)

		ginkgo.By("Validating subnet spec fields")
		framework.ExpectFalse(subnet.Spec.Default)
		framework.ExpectEqual(subnet.Spec.Protocol, util.CheckProtocol(cidr))
		framework.ExpectConsistOf(subnet.Spec.Namespaces, []string{namespaceName})
		framework.ExpectConsistOf(subnet.Spec.ExcludeIps, gateways)
		framework.ExpectEqual(subnet.Spec.Gateway, strings.Join(gateways, ","))
		framework.ExpectEqual(subnet.Spec.GatewayType, apiv1.GWCentralizedType)
		framework.ExpectConsistOf(strings.Split(subnet.Spec.GatewayNode, ","), gatewayNodes)
		framework.ExpectFalse(subnet.Spec.NatOutgoing)
		framework.ExpectFalse(subnet.Spec.Private)
		framework.ExpectEmpty(subnet.Spec.AllowSubnets)

		ginkgo.By("Validating subnet status fields")
		subnet = subnetClient.WaitUntil(subnetName, func(s *apiv1.Subnet) (bool, error) {
			return gomega.ContainElement(s.Status.ActivateGateway).Match(gatewayNodes)
		}, fmt.Sprintf("field .status.activateGateway is within %v", gatewayNodes),
			time.Second, time.Minute,
		)
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

		ginkgo.By("Creating pod " + podName)
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePrivilegedPod(namespaceName, podName, nil, nil, f.KubeOVNImage, cmd, nil)
		_ = podClient.CreateSync(pod)

		framework.CheckPodEgressRoutes(namespaceName, podName, f.HasIPv4(), f.HasIPv6(), 2, nodeIPs)
	})

	framework.ConformanceIt("create centralized subnet without enableEcmp", func() {
		f.SkipVersionPriorTo(1, 12, "Support for enableEcmp in subnet is introduced in v1.12")

		ginkgo.By("Getting nodes")
		nodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodes.Items)

		ginkgo.By("Creating subnet " + subnetName)
		n := min(3, max(1, len(nodes.Items)-1))
		if len(nodes.Items) == 2 {
			n = 2
		}
		gatewayNodes := make([]string, 0, n)
		nodeIPs := make([]string, 0, n)
		for i := range n {
			gatewayNodes = append(gatewayNodes, nodes.Items[i].Name)
			nodeIPs = append(nodeIPs, nodes.Items[i].Annotations[util.IPAddressAnnotation])
		}
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, gatewayNodes, []string{namespaceName})
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Validating subnet finalizers")
		f.ValidateFinalizers(subnet)

		ginkgo.By("Validating centralized subnet with active-standby mode")
		framework.ExpectFalse(subnet.Spec.EnableEcmp)
		framework.ExpectEqual(subnet.Status.ActivateGateway, gatewayNodes[0])
		framework.ExpectConsistOf(strings.Split(subnet.Spec.GatewayNode, ","), gatewayNodes)

		ginkgo.By("Creating pod " + podName)
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePrivilegedPod(namespaceName, podName, nil, nil, f.KubeOVNImage, cmd, nil)
		_ = podClient.CreateSync(pod)

		var gwIPv4, gwIPv6 string
		if f.VersionPriorTo(1, 12) {
			gwIPv4, gwIPv6 = util.SplitStringIP(nodes.Items[0].Annotations[util.IPAddressAnnotation])
		} else {
			gwIPv4, gwIPv6 = util.GetNodeInternalIP(nodes.Items[0])
		}
		hops := strings.Split(strings.Trim(gwIPv4+","+gwIPv6, ","), ",")
		framework.CheckPodEgressRoutes(namespaceName, podName, f.HasIPv4(), f.HasIPv6(), 2, hops)

		ginkgo.By("Change subnet spec field enableEcmp to true")
		modifiedSubnet := subnet.DeepCopy()
		modifiedSubnet.Spec.EnableEcmp = true
		subnet = subnetClient.PatchSync(subnet, modifiedSubnet)

		ginkgo.By("Validating active gateway")
		nbctlCmd := "ovn-nbctl --format=csv --data=bare --no-heading --columns=nexthops find logical-router-policy external_ids:subnet=" + subnetName
		output, _, err := framework.NBExec(nbctlCmd)
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
				for strIP := range strings.SplitSeq(nodeIP, ",") {
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
		nodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
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
		var gatewayV4, gatewayV6 string
		for _, config := range network.IPAM.Config {
			if config.Subnet.IsValid() {
				switch util.CheckProtocol(config.Subnet.String()) {
				case apiv1.ProtocolIPv4:
					if cidrV4 != "" {
						gatewayV4, err = util.LastIP(config.Subnet.String())
						framework.ExpectNoError(err)
					}
				case apiv1.ProtocolIPv6:
					if cidrV6 != "" {
						gatewayV6, err = util.LastIP(config.Subnet.String())
						framework.ExpectNoError(err)
					}
				}
			}
		}
		gateways := make([]string, 0, 2)
		if gatewayV4 != "" {
			gateways = append(gateways, gatewayV4)
		}
		if gatewayV6 != "" {
			gateways = append(gateways, gatewayV6)
		}

		ginkgo.By("Creating subnet " + subnetName)
		prPriority := 1000 + rand.IntN(1000)
		prTable := 1000 + rand.IntN(1000)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		subnet.Spec.ExternalEgressGateway = strings.Join(gateways, ",")
		subnet.Spec.PolicyRoutingPriority = uint32(prPriority)
		subnet.Spec.PolicyRoutingTableID = uint32(prTable)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating pod " + podName)
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePod(namespaceName, podName, nil, nil, f.KubeOVNImage, cmd, nil)
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
			podIPs := util.PodIPs(*pod)
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
		nodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
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
		var gatewayV4, gatewayV6 string
		for _, config := range network.IPAM.Config {
			if config.Subnet.IsValid() {
				switch util.CheckProtocol(config.Subnet.String()) {
				case apiv1.ProtocolIPv4:
					if cidrV4 != "" {
						gatewayV4, err = util.LastIP(config.Subnet.String())
						framework.ExpectNoError(err)
					}
				case apiv1.ProtocolIPv6:
					if cidrV6 != "" {
						gatewayV6, err = util.LastIP(config.Subnet.String())
						framework.ExpectNoError(err)
					}
				}
			}
		}
		cidrs := make([]string, 0, 2)
		gateways := make([]string, 0, 2)
		if gatewayV4 != "" {
			cidrs = append(cidrs, cidrV4)
			gateways = append(gateways, gatewayV4)
		}
		if gatewayV6 != "" {
			cidrs = append(cidrs, cidrV6)
			gateways = append(gateways, gatewayV6)
		}

		ginkgo.By("Creating subnet " + subnetName)
		n := min(3, max(1, len(nodes.Items)-1))
		gatewayNodes := make([]string, 0, n)
		for i := range n {
			gatewayNodes = append(gatewayNodes, nodes.Items[i].Name)
		}
		prPriority := 1000 + rand.IntN(1000)
		prTable := 1000 + rand.IntN(1000)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, gatewayNodes, []string{namespaceName})
		subnet.Spec.ExternalEgressGateway = strings.Join(gateways, ",")
		subnet.Spec.PolicyRoutingPriority = uint32(prPriority)
		subnet.Spec.PolicyRoutingTableID = uint32(prTable)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Getting kind nodes")
		kindNodes, err := kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(kindNodes)

		for _, node := range kindNodes {
			shouldHavePolicyRoute := slices.Contains(gatewayNodes, node.Name())
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
		var startIPv4, startIPv6 string
		if firstIPv4 != "" {
			startIPv4 = util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(firstIPv4), big.NewInt(1)))
		}
		if firstIPv6 != "" {
			startIPv6 = util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(firstIPv6), big.NewInt(1)))
		}

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating pod with no specify pod ip")
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		for i := 1; i <= podCount; i++ {
			podName := fmt.Sprintf("%s-%d", podNamePrefix, i)
			ginkgo.By("Creating pod " + podName)
			pod := framework.MakePod("", podName, nil, annotations, "", nil, nil)
			podClient.Create(pod)
		}
		for i := 1; i <= podCount; i++ {
			podName := fmt.Sprintf("%s-%d", podNamePrefix, i)
			ginkgo.By("Waiting pod " + podName + " to be running")
			podClient.WaitForRunning(podName)
		}

		framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			subnet = subnetClient.Get(subnetName)
			if cidrV4 != "" {
				v4UsingIPEnd := util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(startIPv4), big.NewInt(int64(podCount-1))))
				v4AvailableIPStart := util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(v4UsingIPEnd), big.NewInt(1)))
				framework.Logf("V4UsingIPRange: expected %q, current %q",
					fmt.Sprintf("%s-%s", startIPv4, v4UsingIPEnd),
					subnet.Status.V4UsingIPRange,
				)
				framework.Logf("V4AvailableIPRange: expected %q, current %q",
					fmt.Sprintf("%s-%s", v4AvailableIPStart, lastIPv4),
					subnet.Status.V4AvailableIPRange,
				)
				if subnet.Status.V4UsingIPRange != fmt.Sprintf("%s-%s", startIPv4, v4UsingIPEnd) ||
					subnet.Status.V4AvailableIPRange != fmt.Sprintf("%s-%s", v4AvailableIPStart, lastIPv4) {
					return false, nil
				}
			}
			if cidrV6 != "" {
				v6UsingIPEnd := util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(startIPv6), big.NewInt(int64(podCount-1))))
				v6AvailableIPStart := util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(v6UsingIPEnd), big.NewInt(1)))
				framework.Logf("V6UsingIPRange: expected %q, current %q",
					fmt.Sprintf("%s-%s", startIPv6, v6UsingIPEnd),
					subnet.Status.V6UsingIPRange,
				)
				framework.Logf("V6AvailableIPRange: expected %q, current %q",
					fmt.Sprintf("%s-%s", v6AvailableIPStart, lastIPv6),
					subnet.Status.V6AvailableIPRange,
				)
				if subnet.Status.V6UsingIPRange != fmt.Sprintf("%s-%s", startIPv6, v6UsingIPEnd) ||
					subnet.Status.V6AvailableIPRange != fmt.Sprintf("%s-%s", v6AvailableIPStart, lastIPv6) {
					return false, nil
				}
			}
			return true, nil
		}, "")

		for i := 1; i <= podCount; i++ {
			podName := fmt.Sprintf("%s-%d", podNamePrefix, i)
			ginkgo.By("Deleting pod " + podName)
			err := podClient.Delete(podName)
			framework.ExpectNoError(err)
		}
		for i := 1; i <= podCount; i++ {
			podName := fmt.Sprintf("%s-%d", podNamePrefix, i)
			ginkgo.By("Waiting pod " + podName + " to be deleted")
			podClient.WaitForNotFound(podName)
		}

		framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			subnet = subnetClient.Get(subnetName)
			if cidrV4 != "" {
				if subnet.Status.V4UsingIPRange != "" || subnet.Status.V4AvailableIPRange != fmt.Sprintf("%s-%s", startIPv4, lastIPv4) {
					return false, nil
				}
			}
			if cidrV6 != "" {
				if subnet.Status.V6UsingIPRange != "" || subnet.Status.V6AvailableIPRange != fmt.Sprintf("%s-%s", startIPv6, lastIPv6) {
					return false, nil
				}
			}
			return true, nil
		}, "")

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
		var startIPv4, startIPv6, usingIPv4Str, availableIPv4Str, usingIPv6Str, availableIPv6Str string

		if firstIPv4 != "" {
			startIPv4 = util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(firstIPv4), big.NewInt(1)))
		}
		if firstIPv6 != "" {
			startIPv6 = util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(firstIPv6), big.NewInt(1)))
		}

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)
		ginkgo.By("Creating pod with specify pod ip")
		podIPv4s, podIPv6s := createPodsByRandomIPs(podClient, subnetClient, subnetName, podNamePrefix, podCount, startIPv4, startIPv6)
		subnet = subnetClient.Get(subnetName)

		if podIPv4s != nil {
			usingIPv4Str, availableIPv4Str = calcIPRangeListStr(podIPv4s, startIPv4, lastIPv4)
			framework.ExpectEqual(subnet.Status.V4UsingIPRange, usingIPv4Str)
			framework.ExpectEqual(subnet.Status.V4AvailableIPRange, availableIPv4Str)
		}

		if podIPv6s != nil {
			usingIPv6Str, availableIPv6Str = calcIPRangeListStr(podIPv6s, startIPv6, lastIPv6)
			framework.ExpectEqual(subnet.Status.V6UsingIPRange, usingIPv6Str)
			framework.ExpectEqual(subnet.Status.V6AvailableIPRange, availableIPv6Str)
		}

		for i := 1; i <= podCount; i++ {
			podName := fmt.Sprintf("%s-%d", podNamePrefix, i)
			ginkgo.By("Deleting pod " + podName)
			err := podClient.Delete(podName)
			framework.ExpectNoError(err)
		}
		for i := 1; i <= podCount; i++ {
			podName := fmt.Sprintf("%s-%d", podNamePrefix, i)
			ginkgo.By("Waiting pod " + podName + " to be deleted")
			podClient.WaitForNotFound(podName)
		}

		framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			subnet = subnetClient.Get(subnetName)
			if cidrV4 != "" {
				if subnet.Status.V4UsingIPRange != "" || subnet.Status.V4AvailableIPRange != fmt.Sprintf("%s-%s", startIPv4, lastIPv4) {
					return false, nil
				}
			}
			if cidrV6 != "" {
				if subnet.Status.V6UsingIPRange != "" || subnet.Status.V6AvailableIPRange != fmt.Sprintf("%s-%s", startIPv6, lastIPv6) {
					return false, nil
				}
			}
			return true, nil
		}, "")

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
			startIPv4 = util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(firstIPv4), big.NewInt(1)))
		}
		if firstIPv6 != "" {
			startIPv6 = util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(firstIPv6), big.NewInt(1)))
		}

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		deployName = "deployment-" + framework.RandomSuffix()
		ginkgo.By("Creating deployment " + deployName)
		replicas := int64(5)
		labels := map[string]string{"app": deployName}
		annotations := map[string]string{util.LogicalSwitchAnnotation: subnetName}
		deploy := framework.MakeDeployment(deployName, int32(replicas), labels, annotations, "pause", framework.PauseImage, "")
		deploy = deployClient.CreateSync(deploy)

		checkFunc := func(usingIPRange, availableIPRange, startIP, lastIP string, count int64) bool {
			ginkgo.GinkgoHelper()

			if startIP == "" {
				return true
			}

			usingIPEnd := util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(startIP), big.NewInt(count-1)))
			availableIPStart := util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(usingIPEnd), big.NewInt(1)))

			framework.Logf(`subnet status usingIPRange %q expect "%s-%s"`, usingIPRange, startIP, usingIPEnd)
			if usingIPRange != fmt.Sprintf("%s-%s", startIP, usingIPEnd) {
				return false
			}
			framework.Logf(`subnet status availableIPRange %q expect "%s-%s"`, availableIPRange, availableIPStart, lastIP)
			return availableIPRange == fmt.Sprintf("%s-%s", availableIPStart, lastIP)
		}

		ginkgo.By("Checking subnet status")
		framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			subnet = subnetClient.Get(subnetName)
			if !checkFunc(subnet.Status.V4UsingIPRange, subnet.Status.V4AvailableIPRange, startIPv4, lastIPv4, replicas) {
				return false, nil
			}
			return checkFunc(subnet.Status.V6UsingIPRange, subnet.Status.V6AvailableIPRange, startIPv6, lastIPv6, replicas), nil
		}, "")

		ginkgo.By("Restarting deployment " + deployName)
		_ = deployClient.RestartSync(deploy)

		checkFunc2 := func(usingIPRange, availableIPRange, startIP, lastIP string, count int64) bool {
			ginkgo.GinkgoHelper()

			if startIP == "" {
				return true
			}

			expectAvailIPRangeStr := fmt.Sprintf("%s-%s,%s-%s",
				startIP,
				util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(startIP), big.NewInt(count-1))),
				util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(startIP), big.NewInt(2*count))),
				lastIP,
			)
			expectUsingIPRangeStr := fmt.Sprintf("%s-%s",
				util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(startIP), big.NewInt(count))),
				util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(startIP), big.NewInt(2*count-1))),
			)

			framework.Logf("subnet status usingIPRange %q expect %q", usingIPRange, expectUsingIPRangeStr)
			if usingIPRange != expectUsingIPRangeStr {
				return false
			}
			framework.Logf("subnet status availableIPRange %q expect %q", availableIPRange, expectAvailIPRangeStr)
			return availableIPRange == expectAvailIPRangeStr
		}

		ginkgo.By("Checking subnet status")
		subnet = subnetClient.Get(subnetName)
		framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			subnet = subnetClient.Get(subnetName)
			if !checkFunc2(subnet.Status.V4UsingIPRange, subnet.Status.V4AvailableIPRange, startIPv4, lastIPv4, replicas) {
				return false, nil
			}
			return checkFunc2(subnet.Status.V6UsingIPRange, subnet.Status.V6AvailableIPRange, startIPv6, lastIPv6, replicas), nil
		}, "")
	})

	framework.ConformanceIt("create subnet with enableLb option", func() {
		f.SkipVersionPriorTo(1, 12, "Support for enableLb in subnet is introduced in v1.12")

		enableLb := true
		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet.Spec.EnableLb = &enableLb
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Validating subnet finalizers")
		f.ValidateFinalizers(subnet)

		ginkgo.By("Validating subnet load-balancer records exist")
		cmd := "ovn-nbctl --format=csv --data=bare --no-heading --columns=load_balancer list Logical_Switch " + subnetName
		output, _, err := framework.NBExec(cmd)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(strings.TrimSpace(string(output)))

		ginkgo.By("Validating change subnet spec enableLb to false")
		enableLb = false
		modifiedSubnet := subnet.DeepCopy()
		modifiedSubnet.Spec.EnableLb = &enableLb
		subnet = subnetClient.PatchSync(subnet, modifiedSubnet)
		framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
			if output, _, err = framework.NBExec(cmd); err != nil {
				return false, err
			}
			if strings.TrimSpace(string(output)) == "" {
				return true, nil
			}
			return false, nil
		}, fmt.Sprintf("OVN LB record for subnet %s to be empty", subnet.Name))

		ginkgo.By("Validating empty subnet spec enableLb field, should keep same value as args enableLb")
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.EnableLb = nil
		subnet = subnetClient.PatchSync(subnet, modifiedSubnet)
		framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
			if output, _, err = framework.NBExec(cmd); err != nil {
				return false, err
			}
			if strings.TrimSpace(string(output)) != "" {
				return true, nil
			}
			return false, nil
		}, fmt.Sprintf("OVN LB record for subnet %s to sync", subnet.Name))
	})

	framework.ConformanceIt("should support subnet add gateway event and metrics", func() {
		f.SkipVersionPriorTo(1, 12, "Support for subnet add gateway event and metrics is introduced in v1.12")

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Getting nodes")
		nodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodes.Items)

		for _, node := range nodes.Items {
			ginkgo.By("Checking iptables rules on node " + node.Name + " for subnet " + subnetName)

			if cidrV4 != "" {
				expectedRules := []string{
					fmt.Sprintf(`-A %s -d %s -m comment --comment "%s,%s"`, "FORWARD", cidrV4, util.OvnSubnetGatewayIptables, subnetName),
					fmt.Sprintf(`-A %s -s %s -m comment --comment "%s,%s"`, "FORWARD", cidrV4, util.OvnSubnetGatewayIptables, subnetName),
				}

				iptables.CheckIptablesRulesOnNode(f, node.Name, "filter", "FORWARD", apiv1.ProtocolIPv4, expectedRules, true)
			}
			if cidrV6 != "" {
				expectedRules := []string{
					fmt.Sprintf(`-A %s -d %s -m comment --comment "%s,%s"`, "FORWARD", cidrV6, util.OvnSubnetGatewayIptables, subnetName),
					fmt.Sprintf(`-A %s -s %s -m comment --comment "%s,%s"`, "FORWARD", cidrV6, util.OvnSubnetGatewayIptables, subnetName),
				}
				iptables.CheckIptablesRulesOnNode(f, node.Name, "filter", "FORWARD", apiv1.ProtocolIPv6, expectedRules, true)
			}
		}

		ginkgo.By("Checking subnet gateway type/node change " + subnetName)

		gatewayNodes := make([]string, 0, len(nodes.Items))
		for i := 0; i < 3 && i < max(1, len(nodes.Items)-1); i++ {
			gatewayNodes = append(gatewayNodes, nodes.Items[i].Name)
		}
		modifiedSubnet := subnet.DeepCopy()
		modifiedSubnet.Spec.GatewayType = apiv1.GWCentralizedType
		modifiedSubnet.Spec.GatewayNode = strings.Join(gatewayNodes, ",")

		subnet = subnetClient.PatchSync(subnet, modifiedSubnet)
		eventClient = f.EventClientNS(metav1.NamespaceDefault)
		events := eventClient.WaitToHaveEvent(util.KindSubnet, subnetName, corev1.EventTypeNormal, "SubnetGatewayTypeChanged", "kube-ovn-controller", "")

		message := fmt.Sprintf("subnet gateway type changes from %q to %q", apiv1.GWDistributedType, apiv1.GWCentralizedType)
		found := false
		for _, event := range events {
			if event.Message == message {
				found = true
				break
			}
		}
		framework.ExpectTrue(found, "no SubnetGatewayTypeChanged event")
		found = false
		events = eventClient.WaitToHaveEvent(util.KindSubnet, subnetName, corev1.EventTypeNormal, "SubnetGatewayNodeChanged", "kube-ovn-controller", "")
		message = fmt.Sprintf("gateway node changes from %q to %q", "", modifiedSubnet.Spec.GatewayNode)
		for _, event := range events {
			if event.Message == message {
				found = true
				break
			}
		}
		framework.ExpectTrue(found, "no SubnetGatewayNodeChanged event")
		ginkgo.By("when remove subnet the iptables rules will remove")
		subnetClient.DeleteSync(subnetName)

		for _, node := range nodes.Items {
			ginkgo.By("Checking iptables rules on node " + node.Name + " for subnet " + subnetName)
			if cidrV4 != "" {
				expectedRules := []string{
					fmt.Sprintf(`-A %s -d %s -m comment --comment "%s,%s"`, "FORWARD", cidrV4, util.OvnSubnetGatewayIptables, subnetName),
					fmt.Sprintf(`-A %s -s %s -m comment --comment "%s,%s"`, "FORWARD", cidrV4, util.OvnSubnetGatewayIptables, subnetName),
				}

				iptables.CheckIptablesRulesOnNode(f, node.Name, "filter", "FORWARD", apiv1.ProtocolIPv4, expectedRules, false)
			}
			if cidrV6 != "" {
				expectedRules := []string{
					fmt.Sprintf(`-A %s -d %s -m comment --comment "%s,%s"`, "FORWARD", cidrV6, util.OvnSubnetGatewayIptables, subnetName),
					fmt.Sprintf(`-A %s -s %s -m comment --comment "%s,%s"`, "FORWARD", cidrV6, util.OvnSubnetGatewayIptables, subnetName),
				}
				iptables.CheckIptablesRulesOnNode(f, node.Name, "filter", "FORWARD", apiv1.ProtocolIPv6, expectedRules, false)
			}
		}
	})

	framework.ConformanceIt("Should support subnet multicast snoop", func() {
		f.SkipVersionPriorTo(1, 13, "subnet multicast snoop is introduced in v1.13")
		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Checking subnet multicast snoop enable " + subnetName)
		subnet = subnetClient.Get(subnetName)
		modifiedSubnet := subnet.DeepCopy()
		modifiedSubnet.Spec.EnableMulticastSnoop = true
		subnetClient.PatchSync(subnet, modifiedSubnet)

		framework.WaitUntil(time.Second, 5*time.Second, func(_ context.Context) (bool, error) {
			subnet = subnetClient.Get(subnetName)

			if subnet.Status.McastQuerierIP != "" && subnet.Status.McastQuerierMAC != "" {
				return true, nil
			}
			return false, nil
		}, fmt.Sprintf("subnet %s failed to acquire mcast ip and mac", subnet.Name))

		ginkgo.By("Checking subnet multicast snoop disable " + subnetName)
		subnet = subnetClient.Get(subnetName)
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.EnableMulticastSnoop = false
		subnetClient.PatchSync(subnet, modifiedSubnet)

		framework.WaitUntil(time.Second, 5*time.Second, func(_ context.Context) (bool, error) {
			subnet = subnetClient.Get(subnetName)
			if subnet.Status.McastQuerierIP == "" && subnet.Status.McastQuerierMAC == "" {
				return true, nil
			}
			return false, nil
		}, fmt.Sprintf("subnet %s failed to release mcast ip and mac", subnet.Name))
	})

	framework.ConformanceIt("should support subnet add nat outgoing policy rules", func() {
		f.SkipVersionPriorTo(1, 12, "Support for subnet add nat outgoing policy rules in v1.12")

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating pod " + podName)
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}

		pod := framework.MakePod(namespaceName, podName, nil, annotations, framework.AgnhostImage, nil, nil)
		_ = podClient.CreateSync(pod)

		fakeV4Rules := []apiv1.NatOutgoingPolicyRule{
			{
				Match: apiv1.NatOutGoingPolicyMatch{
					SrcIPs: "1.1.1.1",
				},
				Action: util.NatPolicyRuleActionForward,
			},
			{
				Match: apiv1.NatOutGoingPolicyMatch{
					SrcIPs: "1.1.1.1",
					DstIPs: "199.255.0.0/16",
				},
				Action: util.NatPolicyRuleActionNat,
			},
		}

		fakeV6Rules := []apiv1.NatOutgoingPolicyRule{
			{
				Match: apiv1.NatOutGoingPolicyMatch{
					SrcIPs: "ff0e::1",
				},
				Action: util.NatPolicyRuleActionForward,
			},
			{
				Match: apiv1.NatOutGoingPolicyMatch{
					SrcIPs: "ff0e::1",
					DstIPs: "fd12:3456:789a:bcde::/64",
				},
				Action: util.NatPolicyRuleActionNat,
			},
		}

		subnet = subnetClient.Get(subnetName)
		modifiedSubnet := subnet.DeepCopy()

		rules := make([]apiv1.NatOutgoingPolicyRule, 0, 6)
		if cidrV4 != "" {
			rule := apiv1.NatOutgoingPolicyRule{
				Match: apiv1.NatOutGoingPolicyMatch{
					SrcIPs: cidrV4,
				},
				Action: util.NatPolicyRuleActionForward,
			}
			rules = append(rules, rule)
			rules = append(rules, fakeV4Rules...)
		}

		if cidrV6 != "" {
			rule := apiv1.NatOutgoingPolicyRule{
				Match: apiv1.NatOutGoingPolicyMatch{
					SrcIPs: cidrV6,
				},
				Action: util.NatPolicyRuleActionForward,
			}
			rules = append(rules, rule)
			rules = append(rules, fakeV6Rules...)
		}

		ginkgo.By("Step1: Creating nat outgoing policy rules for subnet " + subnetName)
		modifiedSubnet.Spec.NatOutgoing = true
		modifiedSubnet.Spec.NatOutgoingPolicyRules = rules
		_ = subnetClient.PatchSync(subnet, modifiedSubnet)

		ginkgo.By("Creating another subnet with the same rules: " + fakeSubnetName)
		fakeCidr := framework.RandomCIDR(f.ClusterIPFamily)
		fakeCidrV4, fakeCidrV6 := util.SplitStringIP(fakeCidr)
		fakeSubnet := framework.MakeSubnet(fakeSubnetName, "", fakeCidr, "", "", "", nil, nil, nil)
		fakeSubnet.Spec.NatOutgoingPolicyRules = rules
		fakeSubnet.Spec.NatOutgoing = true
		_ = subnetClient.CreateSync(fakeSubnet)

		subnet = checkSubnetNatOutgoingPolicyRuleStatus(subnetClient, subnetName, rules)
		fakeSubnet = checkSubnetNatOutgoingPolicyRuleStatus(subnetClient, fakeSubnetName, rules)
		checkNatPolicyIPsets(f, cs, subnet, cidrV4, cidrV6, true)
		checkNatPolicyRules(f, cs, subnet, cidrV4, cidrV6, true)
		checkNatPolicyIPsets(f, cs, fakeSubnet, fakeCidrV4, fakeCidrV6, true)
		checkNatPolicyRules(f, cs, fakeSubnet, fakeCidrV4, fakeCidrV6, true)

		ginkgo.By("Checking accessible to external")
		checkAccessExternal(podName, namespaceName, subnet.Spec.Protocol, false)

		ginkgo.By("Step2: Change nat policy rules action to nat")
		rules = make([]apiv1.NatOutgoingPolicyRule, 0, 6)
		if cidrV4 != "" {
			rule := apiv1.NatOutgoingPolicyRule{
				Match: apiv1.NatOutGoingPolicyMatch{
					SrcIPs: cidrV4,
				},
				Action: util.NatPolicyRuleActionNat,
			}
			rules = append(rules, rule)
			rules = append(rules, fakeV4Rules...)
		}

		if cidrV6 != "" {
			rule := apiv1.NatOutgoingPolicyRule{
				Match: apiv1.NatOutGoingPolicyMatch{
					SrcIPs: cidrV6,
				},
				Action: util.NatPolicyRuleActionNat,
			}
			rules = append(rules, rule)
			rules = append(rules, fakeV6Rules...)
		}

		subnet = subnetClient.Get(subnetName)
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.NatOutgoing = true
		modifiedSubnet.Spec.NatOutgoingPolicyRules = rules
		_ = subnetClient.PatchSync(subnet, modifiedSubnet)

		cachedSubnet := checkSubnetNatOutgoingPolicyRuleStatus(subnetClient, subnetName, rules)
		checkNatPolicyIPsets(f, cs, cachedSubnet, cidrV4, cidrV6, true)
		checkNatPolicyRules(f, cs, cachedSubnet, cidrV4, cidrV6, true)
		checkNatPolicyIPsets(f, cs, fakeSubnet, fakeCidrV4, fakeCidrV6, true)
		checkNatPolicyRules(f, cs, fakeSubnet, fakeCidrV4, fakeCidrV6, true)

		ginkgo.By("Checking accessible to external")
		checkAccessExternal(podName, namespaceName, subnet.Spec.Protocol, true)

		ginkgo.By("Step3: When natoutgoing disable, natoutgoing policy rule not work")
		subnet = subnetClient.Get(subnetName)
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.NatOutgoing = false
		_ = subnetClient.PatchSync(subnet, modifiedSubnet)

		_ = checkSubnetNatOutgoingPolicyRuleStatus(subnetClient, subnetName, nil)
		checkNatPolicyRules(f, cs, cachedSubnet, cidrV4, cidrV6, false)
		checkNatPolicyIPsets(f, cs, cachedSubnet, cidrV4, cidrV6, false)
		checkNatPolicyIPsets(f, cs, fakeSubnet, fakeCidrV4, fakeCidrV6, true)
		checkNatPolicyRules(f, cs, fakeSubnet, fakeCidrV4, fakeCidrV6, true)

		ginkgo.By("Checking accessible to external")
		checkAccessExternal(podName, namespaceName, subnet.Spec.Protocol, false)

		ginkgo.By("Step4: Remove network policy rules")
		subnet = subnetClient.Get(subnetName)
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.NatOutgoing = true
		modifiedSubnet.Spec.NatOutgoingPolicyRules = nil
		_ = subnetClient.PatchSync(subnet, modifiedSubnet)

		_ = checkSubnetNatOutgoingPolicyRuleStatus(subnetClient, subnetName, nil)
		checkNatPolicyRules(f, cs, cachedSubnet, cidrV4, cidrV6, false)
		checkNatPolicyIPsets(f, cs, cachedSubnet, cidrV4, cidrV6, false)
		checkNatPolicyIPsets(f, cs, fakeSubnet, fakeCidrV4, fakeCidrV6, true)
		checkNatPolicyRules(f, cs, fakeSubnet, fakeCidrV4, fakeCidrV6, true)

		ginkgo.By("Checking accessible to external")
		checkAccessExternal(podName, namespaceName, subnet.Spec.Protocol, true)

		ginkgo.By("Deleting subnet " + fakeSubnetName)
		subnetClient.DeleteSync(fakeSubnetName)
		checkNatPolicyRules(f, cs, fakeSubnet, fakeCidrV4, fakeCidrV6, false)
		checkNatPolicyIPsets(f, cs, fakeSubnet, fakeCidrV4, fakeCidrV6, false)
	})

	framework.ConformanceIt("should support customize mtu of all pods in subnet", func() {
		f.SkipVersionPriorTo(1, 9, "Support for subnet mtu in v1.9")
		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet.Spec.Mtu = 1600
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating pod " + podName)
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}

		pod := framework.MakePod(namespaceName, podName, nil, annotations, framework.AgnhostImage, nil, nil)
		_ = podClient.CreateSync(pod)

		ginkgo.By("Validating pod MTU")
		links, err := iproute.AddressShow("eth0", func(cmd ...string) ([]byte, []byte, error) {
			return framework.KubectlExec(namespaceName, podName, cmd...)
		})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(links, 1, "should get eth0 information")
		framework.ExpectEqual(links[0].Mtu, int(subnet.Spec.Mtu))
	})

	framework.ConformanceIt("should fail to create pod with static MAC that conflicts with gateway MAC", func() {
		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Getting subnet to find gateway MAC")
		subnet = subnetClient.Get(subnetName)
		framework.ExpectNotNil(subnet)

		// Get the router port name to find gateway MAC
		routerPortName := fmt.Sprintf("%s-%s", subnet.Spec.Vpc, subnetName)
		if subnet.Spec.Vpc == "" {
			routerPortName = fmt.Sprintf("ovn-cluster-%s", subnetName)
		}

		ginkgo.By("Getting gateway MAC from router port " + routerPortName)

		output, err := exec.Command("kubectl", "ko", "nbctl", "--data=bare", "--no-heading", "--columns=mac", "find", "logical_router_port", fmt.Sprintf("name=%s", routerPortName)).CombinedOutput()
		framework.Logf("Command output: %s", string(output))

		if err != nil {
			ginkgo.Skip("Could not get gateway MAC, skipping test")
		}

		// Extract MAC address from the output string
		macRegex := regexp.MustCompile(`([0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}`)
		gatewayMAC := macRegex.FindString(string(output))

		if gatewayMAC == "" {
			ginkgo.Skip("Could not find valid gateway MAC in output, skipping test")
		}

		framework.Logf("Gateway MAC: %s", gatewayMAC)

		ginkgo.By("Creating pod with static MAC that conflicts with gateway MAC")
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
			util.MacAddressAnnotation:    gatewayMAC, // Use the same MAC as gateway
		}

		pod := framework.MakePod(namespaceName, podName, nil, annotations, "", nil, nil)

		ginkgo.By("Expecting pod creation to fail due to MAC conflict")
		_ = podClient.Create(pod)

		// poll for the AddressConflict warning event instead of using a fixed sleep
		framework.WaitUntil(500*time.Millisecond, 15*time.Second, func(_ context.Context) (bool, error) {
			events, err := f.EventClient().List(context.Background(), metav1.ListOptions{
				FieldSelector: fmt.Sprintf("involvedObject.name=%s,involvedObject.namespace=%s", podName, namespaceName),
			})
			if err != nil {
				return false, err
			}
			for _, event := range events.Items {
				if event.Type == corev1.EventTypeWarning && strings.Contains(event.Message, "AddressConflict") {
					framework.Logf("Found conflict event: %s", event.Message)
					return true, nil
				}
			}
			return false, nil
		}, fmt.Sprintf("pod %s should have AddressConflict warning event", podName))
	})
})

func checkNatPolicyIPsets(f *framework.Framework, cs clientset.Interface, subnet *apiv1.Subnet, cidrV4, cidrV6 string, shouldExist bool) {
	ginkgo.GinkgoHelper()

	ginkgo.By(fmt.Sprintf("Checking nat policy rule ipset existed: %v", shouldExist))
	nodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
	framework.ExpectNoError(err)
	framework.ExpectNotEmpty(nodes.Items)

	// Build expected ipsets once (same for all nodes)
	var expectedIPsets []string
	if cidrV4 != "" && shouldExist {
		expectedIPsets = append(expectedIPsets, "ovn40subnets-nat-policy")
	}
	if cidrV6 != "" && shouldExist {
		expectedIPsets = append(expectedIPsets, "ovn60subnets-nat-policy")
	}
	for _, natPolicyRule := range subnet.Status.NatOutgoingPolicyRules {
		protocol := ""
		if natPolicyRule.Match.SrcIPs != "" {
			protocol = util.CheckProtocol(strings.Split(natPolicyRule.Match.SrcIPs, ",")[0])
		} else if natPolicyRule.Match.DstIPs != "" {
			protocol = util.CheckProtocol(strings.Split(natPolicyRule.Match.DstIPs, ",")[0])
		}

		if protocol == apiv1.ProtocolIPv4 {
			if natPolicyRule.Match.SrcIPs != "" {
				expectedIPsets = append(expectedIPsets, fmt.Sprintf("ovn40natpr-%s-src", natPolicyRule.RuleID))
			}
			if natPolicyRule.Match.DstIPs != "" {
				expectedIPsets = append(expectedIPsets, fmt.Sprintf("ovn40natpr-%s-dst", natPolicyRule.RuleID))
			}
		}
		if protocol == apiv1.ProtocolIPv6 {
			if natPolicyRule.Match.SrcIPs != "" {
				expectedIPsets = append(expectedIPsets, fmt.Sprintf("ovn60natpr-%s-src", natPolicyRule.RuleID))
			}
			if natPolicyRule.Match.DstIPs != "" {
				expectedIPsets = append(expectedIPsets, fmt.Sprintf("ovn60natpr-%s-dst", natPolicyRule.RuleID))
			}
		}
	}

	// Check all nodes in parallel
	var wg sync.WaitGroup
	for _, node := range nodes.Items {
		wg.Add(1)
		go func(nodeName string) {
			defer ginkgo.GinkgoRecover()
			defer wg.Done()
			checkIPSetOnNode(f, nodeName, expectedIPsets, shouldExist)
		}(node.Name)
	}
	wg.Wait()
}

func checkNatPolicyRules(f *framework.Framework, cs clientset.Interface, subnet *apiv1.Subnet, cidrV4, cidrV6 string, shouldExist bool) {
	ginkgo.GinkgoHelper()

	ginkgo.By(fmt.Sprintf("Checking nat policy rule existed: %v", shouldExist))
	nodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
	framework.ExpectNoError(err)
	framework.ExpectNotEmpty(nodes.Items)

	// Build expected rules once (same for all nodes)
	var expectV4Rules, expectV6Rules, staticV4Rules, staticV6Rules []string
	if cidrV4 != "" {
		staticV4Rules = append(staticV4Rules, "-A OVN-POSTROUTING -m set --match-set ovn40subnets-nat-policy src -m set ! --match-set ovn40subnets dst -j OVN-NAT-POLICY")
		expectV4Rules = append(expectV4Rules, fmt.Sprintf("-A OVN-NAT-POLICY -s %s -m comment --comment natPolicySubnet-%s -j OVN-NAT-PSUBNET-%s", cidrV4, subnet.Name, subnet.UID[len(subnet.UID)-12:]))
	}

	if cidrV6 != "" {
		staticV6Rules = append(staticV6Rules, "-A OVN-POSTROUTING -m set --match-set ovn60subnets-nat-policy src -m set ! --match-set ovn60subnets dst -j OVN-NAT-POLICY")
		expectV6Rules = append(expectV6Rules, fmt.Sprintf("-A OVN-NAT-POLICY -s %s -m comment --comment natPolicySubnet-%s -j OVN-NAT-PSUBNET-%s", cidrV6, subnet.Name, subnet.UID[len(subnet.UID)-12:]))
	}

	for _, natPolicyRule := range subnet.Status.NatOutgoingPolicyRules {
		markCode := ""
		switch natPolicyRule.Action {
		case util.NatPolicyRuleActionNat:
			markCode = "0x90001/0x90001"
		case util.NatPolicyRuleActionForward:
			markCode = "0x90002/0x90002"
		}

		protocol := ""
		if natPolicyRule.Match.SrcIPs != "" {
			protocol = util.CheckProtocol(strings.Split(natPolicyRule.Match.SrcIPs, ",")[0])
		} else if natPolicyRule.Match.DstIPs != "" {
			protocol = util.CheckProtocol(strings.Split(natPolicyRule.Match.DstIPs, ",")[0])
		}

		var rule string
		if protocol == apiv1.ProtocolIPv4 {
			rule = "-A OVN-NAT-PSUBNET-" + util.GetTruncatedUID(string(subnet.UID))
			if natPolicyRule.Match.SrcIPs != "" {
				rule += fmt.Sprintf(" -m set --match-set ovn40natpr-%s-src src", natPolicyRule.RuleID)
			}
			if natPolicyRule.Match.DstIPs != "" {
				rule += fmt.Sprintf(" -m set --match-set ovn40natpr-%s-dst dst", natPolicyRule.RuleID)
			}
			rule += " -j MARK --set-xmark " + markCode
			expectV4Rules = append(expectV4Rules, rule)
		}
		if protocol == apiv1.ProtocolIPv6 {
			rule = "-A OVN-NAT-PSUBNET-" + util.GetTruncatedUID(string(subnet.UID))
			if natPolicyRule.Match.SrcIPs != "" {
				rule += fmt.Sprintf(" -m set --match-set ovn60natpr-%s-src src", natPolicyRule.RuleID)
			}
			if natPolicyRule.Match.DstIPs != "" {
				rule += fmt.Sprintf(" -m set --match-set ovn60natpr-%s-dst dst", natPolicyRule.RuleID)
			}
			rule += " -j MARK --set-xmark " + markCode
			expectV6Rules = append(expectV6Rules, rule)
		}
	}

	// Check all nodes in parallel
	var wg sync.WaitGroup
	for _, node := range nodes.Items {
		wg.Add(1)
		go func(nodeName string) {
			defer ginkgo.GinkgoRecover()
			defer wg.Done()
			if cidrV4 != "" {
				iptables.CheckIptablesRulesOnNode(f, nodeName, "nat", "", apiv1.ProtocolIPv4, staticV4Rules, true)
				iptables.CheckIptablesRulesOnNode(f, nodeName, "nat", "", apiv1.ProtocolIPv4, expectV4Rules, shouldExist)
			}
			if cidrV6 != "" {
				iptables.CheckIptablesRulesOnNode(f, nodeName, "nat", "", apiv1.ProtocolIPv6, staticV6Rules, true)
				iptables.CheckIptablesRulesOnNode(f, nodeName, "nat", "", apiv1.ProtocolIPv6, expectV6Rules, shouldExist)
			}
		}(node.Name)
	}
	wg.Wait()
}

func checkAccessExternal(podName, podNamespace, protocol string, expectReachable bool) {
	ginkgo.GinkgoHelper()

	ginkgo.By("checking external ip reachable")
	if protocol == apiv1.ProtocolIPv4 || protocol == apiv1.ProtocolDual {
		externalIP := "1.1.1.1"
		isv4ExternalIPReachable := func() bool {
			cmd := fmt.Sprintf("ping %s -w 1", externalIP)
			output, _ := exec.Command("bash", "-c", cmd).CombinedOutput()
			outputStr := string(output)
			return strings.Contains(outputStr, "1 received")
		}
		if isv4ExternalIPReachable() {
			cmd := fmt.Sprintf("kubectl exec %s -n %s -- nc -vz -w 2 %s 53", podName, podNamespace, externalIP)
			output, _ := exec.Command("bash", "-c", cmd).CombinedOutput()
			outputStr := string(output)
			framework.ExpectEqual(strings.Contains(outputStr, "succeeded"), expectReachable)
		}
	}

	if protocol == apiv1.ProtocolIPv6 || protocol == apiv1.ProtocolDual {
		externalIP := "2606:4700:4700::1111"
		isv6ExternalIPReachable := func() bool {
			cmd := fmt.Sprintf("ping6 %s -w 1", externalIP)
			output, _ := exec.Command("bash", "-c", cmd).CombinedOutput()
			outputStr := string(output)
			return strings.Contains(outputStr, "1 received")
		}

		if isv6ExternalIPReachable() {
			cmd := fmt.Sprintf("kubectl exec %s -n %s -- nc -6 -vz -w 2 %s 53", podName, podNamespace, externalIP)
			output, _ := exec.Command("bash", "-c", cmd).CombinedOutput()
			outputStr := string(output)
			framework.ExpectEqual(strings.Contains(outputStr, "succeeded"), expectReachable)
		}
	}
}

func createPodsByRandomIPs(podClient *framework.PodClient, subnetClient *framework.SubnetClient, subnetName, podNamePrefix string, podCount int, startIPv4, startIPv6 string) ([]string, []string) {
	var allocIP string
	var podIPv4s, podIPv6s []string
	podv4IP := startIPv4
	podv6IP := startIPv6

	subnet := subnetClient.Get(subnetName)
	for i := 1; i <= podCount; i++ {
		step := rand.Int64()%10 + 2
		switch subnet.Spec.Protocol {
		case apiv1.ProtocolIPv4:
			podv4IP = util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(podv4IP), big.NewInt(step)))
			allocIP = podv4IP
		case apiv1.ProtocolIPv6:
			podv6IP = util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(podv6IP), big.NewInt(step)))
			allocIP = podv6IP
		default:
			podv4IP = util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(podv4IP), big.NewInt(step)))
			podv6IP = util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(podv6IP), big.NewInt(step)))
			allocIP = fmt.Sprintf("%s,%s", podv4IP, podv6IP)
		}

		annotations := map[string]string{
			util.LogicalSwitchAnnotation:                                        subnetName,
			fmt.Sprintf(util.IPAddressAnnotationTemplate, subnet.Spec.Provider): allocIP,
		}

		podName := fmt.Sprintf("%s-%d", podNamePrefix, i)
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

func calcIPRangeListStr(podIPs []string, startIP, lastIP string) (string, string) {
	var usingIPs, availableIPs []string
	var usingIPStr, availableIPStr, prePodIP string

	for index, podIP := range podIPs {
		usingIPs = append(usingIPs, podIP)
		if index == 0 {
			availableIPs = append(availableIPs, fmt.Sprintf("%s-%s", startIP, util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(podIP), big.NewInt(-1)))))
		} else {
			preIP := prePodIP
			start := util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(preIP), big.NewInt(1)))
			end := util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(podIP), big.NewInt(-1)))

			if start == end {
				availableIPs = append(availableIPs, start)
			} else {
				availableIPs = append(availableIPs, fmt.Sprintf("%s-%s", start, end))
			}
		}
		prePodIP = podIP
	}

	if prePodIP != "" {
		availableIPs = append(availableIPs, fmt.Sprintf("%s-%s", util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(prePodIP), big.NewInt(1))), lastIP))
	}

	usingIPStr = strings.Join(usingIPs, ",")
	availableIPStr = strings.Join(availableIPs, ",")
	framework.Logf("usingIPs is %q", usingIPStr)
	framework.Logf("availableIPs is %q", availableIPStr)
	return usingIPStr, availableIPStr
}
