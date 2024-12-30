package multus

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"testing"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	nadutils "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/utils"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iproute"
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

var _ = framework.SerialDescribe("[group:multus]", func() {
	f := framework.NewDefaultFramework("multus")

	var ipClient *framework.IPClient
	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient
	var nadClient *framework.NetworkAttachmentDefinitionClient
	var nadName, podName, subnetName, namespaceName, cidr string
	var subnet *apiv1.Subnet
	ginkgo.BeforeEach(func() {
		namespaceName = f.Namespace.Name
		nadName = "nad-" + framework.RandomSuffix()
		podName = "pod-" + framework.RandomSuffix()
		subnetName = "subnet-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIPFamily)
		ipClient = f.IPClient()
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		nadClient = f.NetworkAttachmentDefinitionClient()
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)

		ginkgo.By("Deleting network attachment definition " + nadName)
		nadClient.Delete(nadName)
	})

	framework.ConformanceIt("should be able to create attachment interface", func() {
		provider := fmt.Sprintf("%s.%s.%s", nadName, namespaceName, util.OvnProvider)

		ginkgo.By("Creating network attachment definition " + nadName)
		nad := framework.MakeOVNNetworkAttachmentDefinition(nadName, namespaceName, provider, nil)
		nad = nadClient.Create(nad)
		framework.Logf("created network attachment definition config:\n%s", nad.Spec.Config)

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet.Spec.Provider = provider
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating pod " + podName)
		annotations := map[string]string{nadv1.NetworkAttachmentAnnot: fmt.Sprintf("%s/%s", nad.Namespace, nad.Name)}
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePrivilegedPod(namespaceName, podName, nil, annotations, f.KubeOVNImage, cmd, nil)
		pod = podClient.CreateSync(pod)

		ginkgo.By("Validating pod annotations")
		framework.ExpectHaveKey(pod.Annotations, nadv1.NetworkStatusAnnot)
		framework.Logf("pod network status:\n%s", pod.Annotations[nadv1.NetworkStatusAnnot])
		cidr := pod.Annotations[fmt.Sprintf(util.CidrAnnotationTemplate, provider)]
		ip := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)]
		gateway := pod.Annotations[fmt.Sprintf(util.GatewayAnnotationTemplate, provider)]
		mac := pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, provider)]
		framework.ExpectIPInCIDR(ip, cidr)
		framework.ExpectIPInCIDR(gateway, cidr)
		framework.ExpectMAC(mac)

		ipName := ovs.PodNameToPortName(podName, namespaceName, provider)
		ginkgo.By("Validating IP resource " + ipName)
		ipCR := ipClient.Get(ipName)
		framework.ExpectEqual(ipCR.Spec.Subnet, subnetName)
		framework.ExpectEqual(ipCR.Spec.PodName, podName)
		framework.ExpectEqual(ipCR.Spec.Namespace, namespaceName)
		framework.ExpectEqual(ipCR.Spec.NodeName, pod.Spec.NodeName)
		framework.ExpectEqual(ipCR.Spec.IPAddress, ip)
		framework.ExpectEqual(ipCR.Spec.MacAddress, mac)
		ipv4, ipv6 := util.SplitStringIP(ip)
		framework.ExpectEqual(ipCR.Spec.V4IPAddress, ipv4)
		framework.ExpectEqual(ipCR.Spec.V6IPAddress, ipv6)
		framework.ExpectHaveKeyWithValue(ipCR.Labels, subnetName, "")
		framework.ExpectHaveKeyWithValue(ipCR.Labels, util.SubnetNameLabel, subnetName)
		framework.ExpectHaveKeyWithValue(ipCR.Labels, util.NodeNameLabel, pod.Spec.NodeName)
		if !f.VersionPriorTo(1, 13) {
			framework.ExpectHaveKeyWithValue(ipCR.Labels, util.IPReservedLabel, "false")
		}

		ginkgo.By("Retrieving pod routes")
		podRoutes, err := iproute.RouteShow("", "", func(cmd ...string) ([]byte, []byte, error) {
			return framework.KubectlExec(namespaceName, podName, cmd...)
		})
		framework.ExpectNoError(err)

		ginkgo.By("Validating pod routes")
		actualRoutes := make([]request.Route, 0, len(podRoutes))
		for _, r := range podRoutes {
			if r.Gateway != "" || r.Dst != "" {
				actualRoutes = append(actualRoutes, request.Route{Destination: r.Dst, Gateway: r.Gateway})
			}
		}
		ipv4CIDR, ipv6CIDR := util.SplitStringIP(pod.Annotations[util.CidrAnnotation])
		ipv4Gateway, ipv6Gateway := util.SplitStringIP(pod.Annotations[util.GatewayAnnotation])
		nadIPv4CIDR, nadIPv6CIDR := util.SplitStringIP(subnet.Spec.CIDRBlock)
		nadIPv4Gateway, nadIPv6Gateway := util.SplitStringIP(subnet.Spec.Gateway)
		if f.HasIPv4() {
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: ipv4CIDR})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: "default", Gateway: ipv4Gateway})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: nadIPv4CIDR})
			framework.ExpectNotContainElement(actualRoutes, request.Route{Destination: "default", Gateway: nadIPv4Gateway})
		}
		if f.HasIPv6() {
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: ipv6CIDR})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: "default", Gateway: ipv6Gateway})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: nadIPv6CIDR})
			framework.ExpectNotContainElement(actualRoutes, request.Route{Destination: "default", Gateway: nadIPv6Gateway})
		}
	})

	framework.ConformanceIt("should be able to create attachment interface with custom routes", func() {
		provider := fmt.Sprintf("%s.%s.%s", nadName, namespaceName, util.OvnProvider)

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet.Spec.Provider = provider
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Constructing network attachment definition config")
		var routeDst string
		for i := 0; i < 3; i++ {
			routeDst = framework.RandomCIDR(f.ClusterIPFamily)
			if routeDst != subnet.Spec.CIDRBlock {
				break
			}
		}
		framework.ExpectNotEqual(routeDst, subnet.Spec.CIDRBlock)
		routeGw := framework.RandomIPs(subnet.Spec.CIDRBlock, "", 1)
		nadIPv4Gateway, nadIPv6Gateway := util.SplitStringIP(subnet.Spec.Gateway)
		ipv4RouteDst, ipv6RouteDst := util.SplitStringIP(routeDst)
		ipv4RouteGw, ipv6RouteGw := util.SplitStringIP(routeGw)
		routes := make([]request.Route, 0, 4)
		if f.HasIPv4() {
			routes = append(routes, request.Route{Gateway: nadIPv4Gateway})
			routes = append(routes, request.Route{Destination: ipv4RouteDst, Gateway: ipv4RouteGw})
		}
		if f.HasIPv6() {
			routes = append(routes, request.Route{Gateway: nadIPv6Gateway})
			routes = append(routes, request.Route{Destination: ipv6RouteDst, Gateway: ipv6RouteGw})
		}

		ginkgo.By("Creating network attachment definition " + nadName)
		nad := framework.MakeOVNNetworkAttachmentDefinition(nadName, namespaceName, provider, routes)
		nad = nadClient.Create(nad)
		framework.Logf("created network attachment definition config:\n%s", nad.Spec.Config)

		ginkgo.By("Creating pod " + podName)
		annotations := map[string]string{nadv1.NetworkAttachmentAnnot: fmt.Sprintf("%s/%s", nad.Namespace, nad.Name)}
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePrivilegedPod(namespaceName, podName, nil, annotations, f.KubeOVNImage, cmd, nil)
		pod = podClient.CreateSync(pod)

		ginkgo.By("Validating pod annotations")
		framework.ExpectHaveKey(pod.Annotations, nadv1.NetworkStatusAnnot)
		framework.Logf("pod network status:\n%s", pod.Annotations[nadv1.NetworkStatusAnnot])
		cidr := pod.Annotations[fmt.Sprintf(util.CidrAnnotationTemplate, provider)]
		ip := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)]
		gateway := pod.Annotations[fmt.Sprintf(util.GatewayAnnotationTemplate, provider)]
		mac := pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, provider)]
		framework.ExpectIPInCIDR(ip, cidr)
		framework.ExpectIPInCIDR(gateway, cidr)
		framework.ExpectMAC(mac)

		ipName := ovs.PodNameToPortName(podName, namespaceName, provider)
		ginkgo.By("Validating IP resource " + ipName)
		ipCR := ipClient.Get(ipName)
		framework.ExpectEqual(ipCR.Spec.Subnet, subnetName)
		framework.ExpectEqual(ipCR.Spec.PodName, podName)
		framework.ExpectEqual(ipCR.Spec.Namespace, namespaceName)
		framework.ExpectEqual(ipCR.Spec.NodeName, pod.Spec.NodeName)
		framework.ExpectEqual(ipCR.Spec.IPAddress, ip)
		framework.ExpectEqual(ipCR.Spec.MacAddress, mac)
		ipv4, ipv6 := util.SplitStringIP(ip)
		framework.ExpectEqual(ipCR.Spec.V4IPAddress, ipv4)
		framework.ExpectEqual(ipCR.Spec.V6IPAddress, ipv6)
		framework.ExpectHaveKeyWithValue(ipCR.Labels, subnetName, "")
		framework.ExpectHaveKeyWithValue(ipCR.Labels, util.SubnetNameLabel, subnetName)
		framework.ExpectHaveKeyWithValue(ipCR.Labels, util.NodeNameLabel, pod.Spec.NodeName)
		if !f.VersionPriorTo(1, 13) {
			framework.ExpectHaveKeyWithValue(ipCR.Labels, util.IPReservedLabel, "false")
		}

		ginkgo.By("Retrieving pod routes")
		podRoutes, err := iproute.RouteShow("", "", func(cmd ...string) ([]byte, []byte, error) {
			return framework.KubectlExec(namespaceName, podName, cmd...)
		})
		framework.ExpectNoError(err)

		ginkgo.By("Validating pod routes")
		actualRoutes := make([]request.Route, 0, len(podRoutes))
		for _, r := range podRoutes {
			if r.Gateway != "" || r.Dst != "" {
				actualRoutes = append(actualRoutes, request.Route{Destination: r.Dst, Gateway: r.Gateway})
			}
		}
		ipv4CIDR, ipv6CIDR := util.SplitStringIP(pod.Annotations[util.CidrAnnotation])
		ipv4Gateway, ipv6Gateway := util.SplitStringIP(pod.Annotations[util.GatewayAnnotation])
		nadIPv4CIDR, nadIPv6CIDR := util.SplitStringIP(subnet.Spec.CIDRBlock)
		if f.HasIPv4() {
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: ipv4CIDR})
			framework.ExpectNotContainElement(actualRoutes, request.Route{Destination: "default", Gateway: ipv4Gateway})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: nadIPv4CIDR})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: "default", Gateway: nadIPv4Gateway})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: ipv4RouteDst, Gateway: ipv4RouteGw})
		}
		if f.HasIPv6() {
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: ipv6CIDR})
			framework.ExpectNotContainElement(actualRoutes, request.Route{Destination: "default", Gateway: ipv6Gateway})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: nadIPv6CIDR})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: "default", Gateway: nadIPv6Gateway})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: ipv6RouteDst, Gateway: ipv6RouteGw})
		}
	})

	framework.ConformanceIt("should be able to provide IPAM for macvlan", func() {
		provider := fmt.Sprintf("%s.%s", nadName, namespaceName)

		ginkgo.By("Creating network attachment definition " + nadName)
		nad := framework.MakeMacvlanNetworkAttachmentDefinition(nadName, namespaceName, "eth0", "bridge", provider, nil)
		nad = nadClient.Create(nad)
		framework.Logf("created network attachment definition config:\n%s", nad.Spec.Config)

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet.Spec.Provider = provider
		subnet = subnetClient.CreateSync(subnet)

		mac := util.GenerateMac()
		ginkgo.By("Generating networks annotation with MAC " + mac)
		networks := []*nadv1.NetworkSelectionElement{{
			Name:       nad.Name,
			Namespace:  nad.Namespace,
			MacRequest: mac,
		}}
		networksAnnotation, err := json.Marshal(networks)
		framework.ExpectNoError(err)
		framework.Logf("networks annotation: %s", string(networksAnnotation))

		ginkgo.By("Creating pod " + podName + " with MAC address " + mac)
		annotations := map[string]string{nadv1.NetworkAttachmentAnnot: string(networksAnnotation)}
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePrivilegedPod(namespaceName, podName, nil, annotations, f.KubeOVNImage, cmd, nil)
		pod = podClient.CreateSync(pod)

		ginkgo.By("Validating pod annotations")
		framework.ExpectHaveKey(pod.Annotations, nadv1.NetworkStatusAnnot)
		framework.Logf("pod network status:\n%s", pod.Annotations[nadv1.NetworkStatusAnnot])
		statuses, err := nadutils.GetNetworkStatus(pod)
		framework.ExpectNoError(err)
		var ifaceName string
		nadKey := cache.MetaObjectToName(nad).String()
		for _, status := range statuses {
			if status.Name == nadKey {
				framework.ExpectEqual(status.Mac, mac)
				ifaceName = status.Interface
				break
			}
		}
		framework.ExpectNotEmpty(ifaceName)
		cidr := pod.Annotations[fmt.Sprintf(util.CidrAnnotationTemplate, provider)]
		ip := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)]
		gateway := pod.Annotations[fmt.Sprintf(util.GatewayAnnotationTemplate, provider)]
		framework.ExpectIPInCIDR(ip, cidr)
		framework.ExpectIPInCIDR(gateway, cidr)
		framework.ExpectNotHaveKey(pod.Annotations, fmt.Sprintf(util.MacAddressAnnotationTemplate, provider))
		// framework.ExpectHaveKeyWithValue(pod.Annotations, fmt.Sprintf(util.MacAddressAnnotationTemplate, provider), mac)

		ipName := ovs.PodNameToPortName(podName, namespaceName, provider)
		ginkgo.By("Validating IP resource " + ipName)
		ipCR := ipClient.Get(ipName)
		framework.ExpectEqual(ipCR.Spec.Subnet, subnetName)
		framework.ExpectEqual(ipCR.Spec.PodName, podName)
		framework.ExpectEqual(ipCR.Spec.Namespace, namespaceName)
		framework.ExpectEqual(ipCR.Spec.NodeName, pod.Spec.NodeName)
		framework.ExpectEqual(ipCR.Spec.IPAddress, ip)
		framework.ExpectEmpty(ipCR.Spec.MacAddress)
		ipv4, ipv6 := util.SplitStringIP(ip)
		framework.ExpectEqual(ipCR.Spec.V4IPAddress, ipv4)
		framework.ExpectEqual(ipCR.Spec.V6IPAddress, ipv6)
		framework.ExpectHaveKeyWithValue(ipCR.Labels, subnetName, "")
		framework.ExpectHaveKeyWithValue(ipCR.Labels, util.SubnetNameLabel, subnetName)
		framework.ExpectHaveKeyWithValue(ipCR.Labels, util.NodeNameLabel, pod.Spec.NodeName)
		if !f.VersionPriorTo(1, 13) {
			framework.ExpectHaveKeyWithValue(ipCR.Labels, util.IPReservedLabel, "false")
		}

		ginkgo.By("Retrieving MAC address of interface " + ifaceName)
		links, err := iproute.AddressShow(ifaceName, func(cmd ...string) ([]byte, []byte, error) {
			return framework.KubectlExec(namespaceName, podName, cmd...)
		})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(links, 1)
		framework.ExpectEqual(links[0].Address, mac)

		ginkgo.By("Retrieving pod routes")
		podRoutes, err := iproute.RouteShow("", "", func(cmd ...string) ([]byte, []byte, error) {
			return framework.KubectlExec(namespaceName, podName, cmd...)
		})
		framework.ExpectNoError(err)

		ginkgo.By("Validating pod routes")
		actualRoutes := make([]request.Route, 0, len(podRoutes))
		for _, r := range podRoutes {
			if r.Gateway != "" || r.Dst != "" {
				actualRoutes = append(actualRoutes, request.Route{Destination: r.Dst, Gateway: r.Gateway})
			}
		}
		ipv4CIDR, ipv6CIDR := util.SplitStringIP(pod.Annotations[util.CidrAnnotation])
		ipv4Gateway, ipv6Gateway := util.SplitStringIP(pod.Annotations[util.GatewayAnnotation])
		nadIPv4CIDR, nadIPv6CIDR := util.SplitStringIP(subnet.Spec.CIDRBlock)
		nadIPv4Gateway, nadIPv6Gateway := util.SplitStringIP(subnet.Spec.Gateway)
		if f.HasIPv4() {
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: ipv4CIDR})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: "default", Gateway: ipv4Gateway})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: nadIPv4CIDR})
			framework.ExpectNotContainElement(actualRoutes, request.Route{Destination: "default", Gateway: nadIPv4Gateway})
		}
		if f.HasIPv6() {
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: ipv6CIDR})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: "default", Gateway: ipv6Gateway})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: nadIPv6CIDR})
			framework.ExpectNotContainElement(actualRoutes, request.Route{Destination: "default", Gateway: nadIPv6Gateway})
		}
	})

	framework.ConformanceIt("should be able to provide IPAM with custom routes for macvlan", func() {
		provider := fmt.Sprintf("%s.%s", nadName, namespaceName)

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet.Spec.Provider = provider
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Constructing network attachment definition config")
		var routeDst string
		for i := 0; i < 3; i++ {
			routeDst = framework.RandomCIDR(f.ClusterIPFamily)
			if routeDst != subnet.Spec.CIDRBlock {
				break
			}
		}
		framework.ExpectNotEqual(routeDst, subnet.Spec.CIDRBlock)
		routeGw := framework.RandomIPs(subnet.Spec.CIDRBlock, "", 1)
		nadIPv4Gateway, nadIPv6Gateway := util.SplitStringIP(subnet.Spec.Gateway)
		ipv4RouteDst, ipv6RouteDst := util.SplitStringIP(routeDst)
		ipv4RouteGw, ipv6RouteGw := util.SplitStringIP(routeGw)
		routes := make([]request.Route, 0, 4)
		if f.HasIPv4() {
			routes = append(routes, request.Route{Gateway: nadIPv4Gateway})
			routes = append(routes, request.Route{Destination: ipv4RouteDst, Gateway: ipv4RouteGw})
		}
		if f.HasIPv6() {
			routes = append(routes, request.Route{Gateway: nadIPv6Gateway})
			routes = append(routes, request.Route{Destination: ipv6RouteDst, Gateway: ipv6RouteGw})
		}

		ginkgo.By("Creating network attachment definition " + nadName)
		nad := framework.MakeMacvlanNetworkAttachmentDefinition(nadName, namespaceName, "eth0", "bridge", provider, routes)
		nad = nadClient.Create(nad)
		framework.Logf("created network attachment definition config:\n%s", nad.Spec.Config)

		mac := util.GenerateMac()
		ginkgo.By("Generating networks annotation with MAC " + mac)
		networks := []*nadv1.NetworkSelectionElement{{
			Name:       nad.Name,
			Namespace:  nad.Namespace,
			MacRequest: mac,
		}}
		networksAnnotation, err := json.Marshal(networks)
		framework.ExpectNoError(err)
		framework.Logf("networks annotation: %s", string(networksAnnotation))

		ginkgo.By("Creating pod " + podName + " with MAC address " + mac)
		annotations := map[string]string{nadv1.NetworkAttachmentAnnot: string(networksAnnotation)}
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePrivilegedPod(namespaceName, podName, nil, annotations, f.KubeOVNImage, cmd, nil)
		pod = podClient.CreateSync(pod)

		ginkgo.By("Validating pod annotations")
		framework.ExpectHaveKey(pod.Annotations, nadv1.NetworkStatusAnnot)
		framework.Logf("pod network status:\n%s", pod.Annotations[nadv1.NetworkStatusAnnot])
		statuses, err := nadutils.GetNetworkStatus(pod)
		framework.ExpectNoError(err)
		var ifaceName string
		nadKey := cache.MetaObjectToName(nad).String()
		for _, status := range statuses {
			if status.Name == nadKey {
				framework.ExpectEqual(status.Mac, mac)
				ifaceName = status.Interface
				break
			}
		}
		framework.ExpectNotEmpty(ifaceName)
		cidr := pod.Annotations[fmt.Sprintf(util.CidrAnnotationTemplate, provider)]
		ip := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)]
		gateway := pod.Annotations[fmt.Sprintf(util.GatewayAnnotationTemplate, provider)]
		framework.ExpectIPInCIDR(ip, cidr)
		framework.ExpectIPInCIDR(gateway, cidr)
		framework.ExpectNotHaveKey(pod.Annotations, fmt.Sprintf(util.MacAddressAnnotationTemplate, provider))
		// framework.ExpectHaveKeyWithValue(pod.Annotations, fmt.Sprintf(util.MacAddressAnnotationTemplate, provider), mac)

		ipName := ovs.PodNameToPortName(podName, namespaceName, provider)
		ginkgo.By("Validating IP resource " + ipName)
		ipCR := ipClient.Get(ipName)
		framework.ExpectEqual(ipCR.Spec.Subnet, subnetName)
		framework.ExpectEqual(ipCR.Spec.PodName, podName)
		framework.ExpectEqual(ipCR.Spec.Namespace, namespaceName)
		framework.ExpectEqual(ipCR.Spec.NodeName, pod.Spec.NodeName)
		framework.ExpectEqual(ipCR.Spec.IPAddress, ip)
		framework.ExpectEmpty(ipCR.Spec.MacAddress)
		ipv4, ipv6 := util.SplitStringIP(ip)
		framework.ExpectEqual(ipCR.Spec.V4IPAddress, ipv4)
		framework.ExpectEqual(ipCR.Spec.V6IPAddress, ipv6)
		framework.ExpectHaveKeyWithValue(ipCR.Labels, subnetName, "")
		framework.ExpectHaveKeyWithValue(ipCR.Labels, util.SubnetNameLabel, subnetName)
		framework.ExpectHaveKeyWithValue(ipCR.Labels, util.NodeNameLabel, pod.Spec.NodeName)
		if !f.VersionPriorTo(1, 13) {
			framework.ExpectHaveKeyWithValue(ipCR.Labels, util.IPReservedLabel, "false")
		}

		ginkgo.By("Retrieving MAC address of interface " + ifaceName)
		links, err := iproute.AddressShow(ifaceName, func(cmd ...string) ([]byte, []byte, error) {
			return framework.KubectlExec(namespaceName, podName, cmd...)
		})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(links, 1)
		framework.ExpectEqual(links[0].Address, mac)

		ginkgo.By("Retrieving pod routes")
		podRoutes, err := iproute.RouteShow("", "", func(cmd ...string) ([]byte, []byte, error) {
			return framework.KubectlExec(namespaceName, podName, cmd...)
		})
		framework.ExpectNoError(err)

		ginkgo.By("Validating pod routes")
		actualRoutes := make([]request.Route, 0, len(podRoutes))
		for _, r := range podRoutes {
			if r.Gateway != "" || r.Dst != "" {
				actualRoutes = append(actualRoutes, request.Route{Destination: r.Dst, Gateway: r.Gateway})
			}
		}
		ipv4CIDR, ipv6CIDR := util.SplitStringIP(pod.Annotations[util.CidrAnnotation])
		ipv4Gateway, ipv6Gateway := util.SplitStringIP(pod.Annotations[util.GatewayAnnotation])
		nadIPv4CIDR, nadIPv6CIDR := util.SplitStringIP(subnet.Spec.CIDRBlock)
		if f.HasIPv4() {
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: ipv4CIDR})
			framework.ExpectNotContainElement(actualRoutes, request.Route{Destination: "default", Gateway: ipv4Gateway})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: nadIPv4CIDR})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: "default", Gateway: nadIPv4Gateway})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: ipv4RouteDst, Gateway: ipv4RouteGw})
		}
		if f.HasIPv6() {
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: ipv6CIDR})
			framework.ExpectNotContainElement(actualRoutes, request.Route{Destination: "default", Gateway: ipv6Gateway})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: nadIPv6CIDR})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: "default", Gateway: nadIPv6Gateway})
			framework.ExpectContainElement(actualRoutes, request.Route{Destination: ipv6RouteDst, Gateway: ipv6RouteGw})
		}
	})

	framework.ConformanceIt("should be able to use mac and ip provided by k8s.v1.cni.cncf.io/networks annotation", func() {
		if f.VersionPriorTo(1, 13) {
			ginkgo.Skip("this feature is supported from version 1.13")
		}

		provider := fmt.Sprintf("%s.%s.%s", nadName, namespaceName, util.OvnProvider)
		ginkgo.By("Creating network attachment definition " + nadName)
		nad := framework.MakeOVNNetworkAttachmentDefinition(nadName, namespaceName, provider, nil)
		nad = nadClient.Create(nad)
		framework.Logf("created network attachment definition config:\n%s", nad.Spec.Config)

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", provider, nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating pod " + podName)
		mac := "00:00:00:11:22:33"
		randomIP := framework.RandomIPs(subnet.Spec.CIDRBlock, "", 1)

		randomIPArray := strings.Split(randomIP, ",")
		var requestIPString string
		for i, ip := range randomIPArray {
			if i == len(randomIPArray)-1 {
				requestIPString += fmt.Sprintf(`"%s"`, ip)
			} else {
				requestIPString += fmt.Sprintf(`"%s",`, ip)
			}
		}

		framework.Logf("requestIPString: %s", requestIPString)
		annotations := map[string]string{nadv1.NetworkAttachmentAnnot: fmt.Sprintf(`[{"name": "%s", "namespace": "%s", "mac": "%s", "ips": [%s]}]`, nad.Name, nad.Namespace, mac, requestIPString)}
		annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider)] = subnetName

		cmd := []string{"sh", "-c", "sleep infinity"}
		pod := framework.MakePrivilegedPod(namespaceName, podName, nil, annotations, f.KubeOVNImage, cmd, nil)
		pod = podClient.CreateSync(pod)

		ginkgo.By("Validating pod annotations")
		framework.ExpectHaveKey(pod.Annotations, nadv1.NetworkStatusAnnot)
		framework.Logf("pod network status:\n%s", pod.Annotations[nadv1.NetworkStatusAnnot])
		retMac := pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, provider)]
		retIP := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)]

		framework.ExpectEqual(mac, retMac)
		framework.ExpectEqual(strings.Join(randomIPArray, ","), retIP)
	})
})
