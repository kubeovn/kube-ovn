package multus

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	nadutils "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	kubeletevents "k8s.io/kubernetes/pkg/kubelet/events"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

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

type multusAttachment struct {
	provider  string
	ifaceName string
	family    string
	ip        string
	mac       string
}

type multusAttachmentExpecter struct {
	ipClient   *framework.IPClient
	subnet     *apiv1.Subnet
	subnetName string
}

func valuesForIPFamily(family, ipv4Value, ipv6Value string) (value, oppositeValue string) {
	ginkgo.GinkgoHelper()

	switch family {
	case apiv1.ProtocolIPv4:
		return ipv4Value, ipv6Value
	case apiv1.ProtocolIPv6:
		return ipv6Value, ipv4Value
	default:
		framework.Failf("unexpected IP family %q", family)
	}
	return "", ""
}

func oppositeIPFamily(family string) string {
	ginkgo.GinkgoHelper()

	switch family {
	case apiv1.ProtocolIPv4:
		return apiv1.ProtocolIPv6
	case apiv1.ProtocolIPv6:
		return apiv1.ProtocolIPv4
	default:
		framework.Failf("unexpected IP family %q", family)
	}
	return ""
}

func (e multusAttachmentExpecter) expectPodAttachment(
	pod *corev1.Pod,
	nad *nadv1.NetworkAttachmentDefinition,
	provider, ifaceName, family string,
) multusAttachment {
	ginkgo.GinkgoHelper()

	attachment := e.expectAttachmentIPFamily(pod, provider, ifaceName, family)
	e.expectAttachmentIPCR(pod, attachment)
	expectNetworkStatus(pod, nad, attachment)
	e.expectAttachmentInterfaceIPFamily(pod, attachment)
	expectAttachmentDHCPOptions(pod, attachment)
	return attachment
}

func (e multusAttachmentExpecter) expectAttachmentIPFamily(pod *corev1.Pod, provider, ifaceName, family string) multusAttachment {
	ginkgo.GinkgoHelper()

	ip := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)]
	cidr := pod.Annotations[fmt.Sprintf(util.CidrAnnotationTemplate, provider)]
	gateway := pod.Annotations[fmt.Sprintf(util.GatewayAnnotationTemplate, provider)]
	mac := pod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, provider)]
	framework.ExpectHaveKeyWithValue(pod.Annotations, fmt.Sprintf(util.AllocatedAnnotationTemplate, provider), "true")
	framework.ExpectHaveKeyWithValue(pod.Annotations, fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider), e.subnet.Name)
	framework.ExpectIPInCIDR(ip, cidr)
	framework.ExpectMAC(mac)

	ipv4, ipv6 := util.SplitStringIP(ip)
	gatewayV4, gatewayV6 := util.SplitStringIP(gateway)
	familyIP, otherFamilyIP := valuesForIPFamily(family, ipv4, ipv6)
	familyGateway, _ := valuesForIPFamily(family, gatewayV4, gatewayV6)
	framework.ExpectNotEmpty(familyIP)
	framework.ExpectEmpty(otherFamilyIP)
	framework.ExpectNotEmpty(familyGateway)

	return multusAttachment{
		provider:  provider,
		ifaceName: ifaceName,
		family:    family,
		ip:        ip,
		mac:       mac,
	}
}

func (e multusAttachmentExpecter) expectAttachmentIPCR(pod *corev1.Pod, attachment multusAttachment) {
	ginkgo.GinkgoHelper()

	ipName := ovs.PodNameToPortName(pod.Name, pod.Namespace, attachment.provider)
	ginkgo.By("Validating IP resource " + ipName)
	ipCR := e.ipClient.Get(ipName)
	framework.ExpectEqual(ipCR.Spec.Subnet, e.subnetName)
	framework.ExpectEqual(ipCR.Spec.PodName, pod.Name)
	framework.ExpectEqual(ipCR.Spec.Namespace, pod.Namespace)
	framework.ExpectEqual(ipCR.Spec.NodeName, pod.Spec.NodeName)
	framework.ExpectEqual(ipCR.Spec.IPAddress, attachment.ip)
	framework.ExpectEqual(ipCR.Spec.MacAddress, attachment.mac)
	ipv4, ipv6 := util.SplitStringIP(attachment.ip)
	framework.ExpectEqual(ipCR.Spec.V4IPAddress, ipv4)
	framework.ExpectEqual(ipCR.Spec.V6IPAddress, ipv6)
}

func expectNetworkStatus(pod *corev1.Pod, nad *nadv1.NetworkAttachmentDefinition, attachment multusAttachment) {
	ginkgo.GinkgoHelper()

	statuses, err := nadutils.GetNetworkStatus(pod)
	framework.ExpectNoError(err)
	nadKey := cache.MetaObjectToName(nad).String()
	found := false
	for _, status := range statuses {
		if status.Name == nadKey && status.Interface == attachment.ifaceName {
			framework.ExpectConsistOf(status.IPs, strings.Split(attachment.ip, ","))
			framework.ExpectEqual(status.Mac, attachment.mac)
			found = true
			break
		}
	}
	framework.ExpectTrue(found, "network status should contain %s interface %s", nadKey, attachment.ifaceName)
}

func expectAttachmentDHCPOptions(pod *corev1.Pod, attachment multusAttachment) {
	ginkgo.GinkgoHelper()

	portName := ovs.PodNameToPortName(pod.Name, pod.Namespace, attachment.provider)
	ginkgo.By("Validating DHCP options for logical switch port " + portName)
	dhcpV4, _, err := framework.NBExec("ovn-nbctl get logical_switch_port " + portName + " dhcpv4_options")
	framework.ExpectNoError(err)
	dhcpV6, _, err := framework.NBExec("ovn-nbctl get logical_switch_port " + portName + " dhcpv6_options")
	framework.ExpectNoError(err)
	dhcpUUID, otherDHCPUUID := valuesForIPFamily(attachment.family, parseDHCPOptionUUID(dhcpV4), parseDHCPOptionUUID(dhcpV6))
	framework.ExpectNotEmpty(dhcpUUID)
	framework.ExpectEmpty(otherDHCPUUID)
	framework.ExpectEqual(util.CheckProtocol(getDHCPOptionCIDR(dhcpUUID)), attachment.family)
}

func parseDHCPOptionUUID(output []byte) string {
	uuid := strings.TrimSpace(string(output))
	if uuid == "[]" {
		return ""
	}
	uuid = strings.TrimPrefix(uuid, "[")
	uuid = strings.TrimSuffix(uuid, "]")
	uuid = strings.TrimSpace(uuid)
	return strings.Trim(uuid, `"`)
}

func getDHCPOptionCIDR(uuid string) string {
	ginkgo.GinkgoHelper()

	output, _, err := framework.NBExec("ovn-nbctl get DHCP_Options " + uuid + " cidr")
	framework.ExpectNoError(err)
	return strings.Trim(strings.TrimSpace(string(output)), `"`)
}

func (e multusAttachmentExpecter) expectAttachmentInterfaceIPFamily(pod *corev1.Pod, attachment multusAttachment) {
	ginkgo.GinkgoHelper()

	ginkgo.By("Validating attachment interface IPs")
	links, err := iproute.AddressShow(attachment.ifaceName, func(cmd ...string) ([]byte, []byte, error) {
		return framework.KubectlExec(pod.Namespace, pod.Name, cmd...)
	})
	framework.ExpectNoError(err)
	framework.ExpectHaveLen(links, 1)
	framework.ExpectEqual(links[0].Address, attachment.mac)
	framework.ExpectConsistOf(links[0].NonLinkLocalIPs(), strings.Split(attachment.ip, ","))

	ginkgo.By("Validating attachment interface routes")
	podRoutes, err := iproute.RouteShow("", attachment.ifaceName, func(cmd ...string) ([]byte, []byte, error) {
		return framework.KubectlExec(pod.Namespace, pod.Name, cmd...)
	})
	framework.ExpectNoError(err)
	actualRoutes := routesWithDestinationOrGateway(podRoutes)
	nadIPv4CIDR, nadIPv6CIDR := util.SplitStringIP(e.subnet.Spec.CIDRBlock)
	familyCIDR, otherFamilyCIDR := valuesForIPFamily(attachment.family, nadIPv4CIDR, nadIPv6CIDR)
	nadIPv4Gateway, nadIPv6Gateway := util.SplitStringIP(pod.Annotations[fmt.Sprintf(util.GatewayAnnotationTemplate, attachment.provider)])
	familyGateway, otherFamilyGateway := valuesForIPFamily(attachment.family, nadIPv4Gateway, nadIPv6Gateway)

	framework.ExpectContainElement(actualRoutes, request.Route{Destination: familyCIDR})
	framework.ExpectNotContainElement(actualRoutes, request.Route{Destination: otherFamilyCIDR})
	framework.ExpectTrue(hasNonLinkLocalConnectedRouteForFamily(actualRoutes, attachment.family))
	framework.ExpectFalse(hasNonLinkLocalConnectedRouteForFamily(actualRoutes, oppositeIPFamily(attachment.family)))
	if pod.Annotations[fmt.Sprintf(util.DefaultRouteAnnotationTemplate, attachment.provider)] == "true" {
		framework.ExpectContainElement(actualRoutes, request.Route{Destination: "default", Gateway: familyGateway})
		framework.ExpectTrue(podHasDefaultRouteForFamily(pod, attachment.ifaceName, attachment.family))
	} else {
		framework.ExpectNotContainElement(actualRoutes, request.Route{Destination: "default", Gateway: familyGateway})
		framework.ExpectFalse(podHasDefaultRouteForFamily(pod, attachment.ifaceName, attachment.family))
	}
	framework.ExpectNotContainElement(actualRoutes, request.Route{Destination: "default", Gateway: otherFamilyGateway})
	framework.ExpectFalse(podHasDefaultRouteForFamily(pod, attachment.ifaceName, oppositeIPFamily(attachment.family)))
}

func routesWithDestinationOrGateway(routes []iproute.Route) []request.Route {
	actualRoutes := make([]request.Route, 0, len(routes))
	for _, r := range routes {
		if r.Gateway != "" || r.Dst != "" {
			actualRoutes = append(actualRoutes, request.Route{Destination: r.Dst, Gateway: r.Gateway})
		}
	}
	return actualRoutes
}

func podHasDefaultRouteForFamily(pod *corev1.Pod, ifaceName, family string) bool {
	ginkgo.GinkgoHelper()

	args := []string{"ip", "-j", "route", "show", "default", "dev", ifaceName}
	if family == apiv1.ProtocolIPv6 {
		args = []string{"ip", "-j", "-6", "route", "show", "default", "dev", ifaceName}
	}
	output, _, err := framework.KubectlExec(pod.Namespace, pod.Name, args...)
	framework.ExpectNoError(err)
	var routes []iproute.Route
	framework.ExpectNoError(json.Unmarshal(output, &routes))
	return len(routes) != 0
}

func hasNonLinkLocalConnectedRouteForFamily(actualRoutes []request.Route, family string) bool {
	ginkgo.GinkgoHelper()

	for _, route := range actualRoutes {
		if route.Gateway != "" || route.Destination == "" || route.Destination == "default" ||
			route.Destination == "0.0.0.0/0" || route.Destination == "::/0" {
			continue
		}
		ip, _, err := net.ParseCIDR(route.Destination)
		framework.ExpectNoError(err)
		if ip.IsLinkLocalUnicast() {
			continue
		}
		if util.CheckProtocol(route.Destination) == family {
			return true
		}
	}
	return false
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

	framework.ConformanceIt("should allocate requested IP family for attachment interface", func() {
		f.SkipVersionPriorTo(1, 17, "Per-pod IP family selection was introduced in v1.17")
		if !f.IsDual() {
			ginkgo.Skip("This test requires a dual-stack cluster")
		}

		provider := fmt.Sprintf("%s.%s.%s", nadName, namespaceName, util.OvnProvider)

		ginkgo.By("Creating network attachment definition " + nadName)
		nad := framework.MakeOVNNetworkAttachmentDefinition(nadName, namespaceName, provider, nil)
		nad = nadClient.Create(nad)
		framework.Logf("created network attachment definition config:\n%s", nad.Spec.Config)

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet.Spec.EnableDHCP = true
		subnet.Spec.Provider = provider
		subnet = subnetClient.CreateSync(subnet)
		attachmentExpecter := multusAttachmentExpecter{ipClient: ipClient, subnet: subnet, subnetName: subnetName}

		for _, family := range []string{apiv1.ProtocolIPv4, apiv1.ProtocolIPv6} {
			ginkgo.By("Creating pod " + podName + " with " + family + " attachment IP family")
			annotations := map[string]string{
				nadv1.NetworkAttachmentAnnot:                               fmt.Sprintf("%s/%s", nad.Namespace, nad.Name),
				fmt.Sprintf(util.IPFamilyAnnotationTemplate, provider):     strings.ToLower(family),
				fmt.Sprintf(util.DefaultRouteAnnotationTemplate, provider): "true",
			}
			cmd := []string{"sleep", "infinity"}
			pod := framework.MakePrivilegedPod(namespaceName, podName, nil, annotations, f.KubeOVNImage, cmd, nil)
			pod = podClient.CreateSync(pod)

			ginkgo.By("Validating pod annotations")
			framework.ExpectHaveKey(pod.Annotations, nadv1.NetworkStatusAnnot)
			framework.Logf("pod network status:\n%s", pod.Annotations[nadv1.NetworkStatusAnnot])
			attachmentExpecter.expectPodAttachment(pod, nad, provider, "net1", family)

			ginkgo.By("Deleting pod " + podName)
			podClient.DeleteSync(podName)
			ipClient.DeleteSync(ovs.PodNameToPortName(podName, namespaceName, provider))
		}
	})

	framework.ConformanceIt("should allocate requested IP families for multiple interfaces from the same NAD", func() {
		f.SkipVersionPriorTo(1, 17, "Per-pod IP family selection was introduced in v1.17")
		if !f.IsDual() {
			ginkgo.Skip("This test requires a dual-stack cluster")
		}

		provider := fmt.Sprintf("%s.%s.%s", nadName, namespaceName, util.OvnProvider)
		providerNet1 := provider + ".net1"
		providerNet2 := provider + ".net2"

		ginkgo.By("Creating network attachment definition " + nadName)
		nad := framework.MakeOVNNetworkAttachmentDefinition(nadName, namespaceName, provider, nil)
		nad = nadClient.Create(nad)
		framework.Logf("created network attachment definition config:\n%s", nad.Spec.Config)

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet.Spec.EnableDHCP = true
		subnet.Spec.Provider = provider
		subnet = subnetClient.CreateSync(subnet)
		attachmentExpecter := multusAttachmentExpecter{ipClient: ipClient, subnet: subnet, subnetName: subnetName}

		ginkgo.By("Generating networks annotation with the same NAD attached as net1 and net2")
		networks := []*nadv1.NetworkSelectionElement{
			{
				Name:             nad.Name,
				Namespace:        nad.Namespace,
				InterfaceRequest: "net1",
			},
			{
				Name:             nad.Name,
				Namespace:        nad.Namespace,
				InterfaceRequest: "net2",
			},
		}
		networksAnnotation, err := json.Marshal(networks)
		framework.ExpectNoError(err)
		framework.Logf("networks annotation: %s", string(networksAnnotation))

		ginkgo.By("Creating pod " + podName + " with different IP families per interface")
		annotations := map[string]string{
			nadv1.NetworkAttachmentAnnot:                               string(networksAnnotation),
			fmt.Sprintf(util.IPFamilyAnnotationTemplate, providerNet1): strings.ToLower(apiv1.ProtocolIPv4),
			fmt.Sprintf(util.IPFamilyAnnotationTemplate, providerNet2): strings.ToLower(apiv1.ProtocolIPv6),
		}
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePrivilegedPod(namespaceName, podName, nil, annotations, f.KubeOVNImage, cmd, nil)
		pod = podClient.CreateSync(pod)

		ginkgo.By("Validating net1 IPv4-only allocation")
		attachmentExpecter.expectPodAttachment(pod, nad, providerNet1, "net1", apiv1.ProtocolIPv4)

		ginkgo.By("Validating net2 IPv6-only allocation")
		attachmentExpecter.expectPodAttachment(pod, nad, providerNet2, "net2", apiv1.ProtocolIPv6)
	})

	framework.ConformanceIt("should reject ovn attachment provider without matching subnet", func() {
		f.SkipVersionPriorTo(1, 15, "multiple network attachment validation requires v1.15+")

		provider := fmt.Sprintf("%s.%s.%s", nadName, namespaceName, util.OvnProvider)
		mismatchProvider := fmt.Sprintf("%s-mismatch.%s.%s", nadName, namespaceName, util.OvnProvider)

		ginkgo.By("Creating subnet " + subnetName + " with a different provider")
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", util.DefaultVpc, mismatchProvider, nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating network attachment definition " + nadName)
		nad := framework.MakeOVNNetworkAttachmentDefinition(nadName, namespaceName, provider, nil)
		nad = nadClient.Create(nad)
		framework.Logf("created network attachment definition config:\n%s", nad.Spec.Config)

		ginkgo.By("Creating pod " + podName)
		annotations := map[string]string{nadv1.NetworkAttachmentAnnot: fmt.Sprintf("%s/%s", nad.Namespace, nad.Name)}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, "", nil, nil)
		_ = podClient.Create(pod)

		ginkgo.By("Waiting for kubelet to report sandbox creation failure")
		wantMessage := fmt.Sprintf("no address allocated to pod %s/%s provider %s", namespaceName, podName, util.OvnProvider)
		gomega.Eventually(func() bool {
			events, err := f.ClientSet.CoreV1().Events(namespaceName).List(context.Background(), metav1.ListOptions{
				FieldSelector: fmt.Sprintf("involvedObject.kind=%s,involvedObject.name=%s,type=%s,reason=%s", util.KindPod, podName, corev1.EventTypeWarning, kubeletevents.FailedCreatePodSandBox),
			})
			if err != nil {
				return false
			}
			for _, event := range events.Items {
				if strings.Contains(event.Message, wantMessage) {
					return true
				}
			}
			return false
		}, 60*time.Second, time.Second).Should(gomega.BeTrue(), "expected pod event to contain %q", wantMessage)

		pod = podClient.GetPod(podName)
		framework.ExpectNotEqual(pod.Status.Phase, corev1.PodRunning, "pod should not run when attachment provider has no subnet")
		framework.ExpectEmpty(pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider)], "attachment should not fall back to the default subnet")
	})

	framework.ConformanceIt("should be able to create attachment interface with custom routes", func() {
		provider := fmt.Sprintf("%s.%s.%s", nadName, namespaceName, util.OvnProvider)

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet.Spec.Provider = provider
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Constructing network attachment definition config")
		var routeDst string
		for range 3 {
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
		if !f.VersionPriorTo(1, 13) {
			framework.ExpectHaveKeyWithValue(pod.Annotations, fmt.Sprintf(util.MacAddressAnnotationTemplate, provider), mac)
		}

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
		for range 3 {
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
		if !f.VersionPriorTo(1, 13) {
			framework.ExpectHaveKeyWithValue(pod.Annotations, fmt.Sprintf(util.MacAddressAnnotationTemplate, provider), mac)
		}

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

		ginkgo.By("Generating k8s.v1.cni.cncf.io/networks annotation")
		mac := util.GenerateMac()
		ips := strings.Split(framework.RandomIPs(subnet.Spec.CIDRBlock, "", 1), ",")
		networks := []*nadv1.NetworkSelectionElement{{
			Name:       nad.Name,
			Namespace:  nad.Namespace,
			MacRequest: mac,
			IPRequest:  ips,
		}}
		networksAnnotation, err := json.Marshal(networks)
		framework.ExpectNoError(err)
		framework.Logf("networks annotation: %s", string(networksAnnotation))

		ginkgo.By("Creating pod " + podName)
		annotations := map[string]string{nadv1.NetworkAttachmentAnnot: string(networksAnnotation)}
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePrivilegedPod(namespaceName, podName, nil, annotations, f.KubeOVNImage, cmd, nil)
		pod = podClient.CreateSync(pod)

		ginkgo.By("Validating pod annotations")
		framework.ExpectHaveKey(pod.Annotations, nadv1.NetworkStatusAnnot)
		framework.Logf("pod network status:\n%s", pod.Annotations[nadv1.NetworkStatusAnnot])
		framework.ExpectHaveKeyWithValue(pod.Annotations, fmt.Sprintf(util.MacAddressAnnotationTemplate, provider), mac)
		framework.ExpectHaveKeyWithValue(pod.Annotations, fmt.Sprintf(util.IPAddressAnnotationTemplate, provider), strings.Join(ips, ","))

		ginkgo.By("Getting attachment interface name")
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

		ginkgo.By("Validating pod ip and mac")
		links, err := iproute.AddressShow(ifaceName, func(cmd ...string) ([]byte, []byte, error) {
			return framework.KubectlExec(pod.Namespace, pod.Name, cmd...)
		})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(links, 1)
		framework.ExpectEqual(links[0].Address, mac)
		framework.ExpectConsistOf(links[0].NonLinkLocalIPs(), ips)
	})

	framework.ConformanceIt("should be able to provide IPAM for macvlan with ip provided by k8s.v1.cni.cncf.io/networks annotation", func() {
		if f.VersionPriorTo(1, 13) {
			ginkgo.Skip("this feature was introduced in v1.13")
		}

		provider := fmt.Sprintf("%s.%s", nadName, namespaceName)

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet.Spec.Provider = provider
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating network attachment definition " + nadName)
		nad := framework.MakeMacvlanNetworkAttachmentDefinition(nadName, namespaceName, "eth0", "bridge", provider, nil)
		nad = nadClient.Create(nad)
		framework.Logf("created network attachment definition config:\n%s", nad.Spec.Config)

		ginkgo.By("Generating networks annotation")
		mac := util.GenerateMac()
		ips := strings.Split(framework.RandomIPs(subnet.Spec.CIDRBlock, "", 1), ",")
		networks := []*nadv1.NetworkSelectionElement{{
			Name:       nad.Name,
			Namespace:  nad.Namespace,
			MacRequest: mac,
			IPRequest:  ips,
		}}
		networksAnnotation, err := json.Marshal(networks)
		framework.ExpectNoError(err)
		framework.Logf("networks annotation: %s", string(networksAnnotation))

		ginkgo.By("Creating pod " + podName)
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
		framework.ExpectHaveKeyWithValue(pod.Annotations, fmt.Sprintf(util.MacAddressAnnotationTemplate, provider), mac)

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

		ginkgo.By("Validating pod ip and mac")
		links, err := iproute.AddressShow(ifaceName, func(cmd ...string) ([]byte, []byte, error) {
			return framework.KubectlExec(namespaceName, podName, cmd...)
		})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(links, 1)
		framework.ExpectEqual(links[0].Address, mac)
		framework.ExpectConsistOf(links[0].NonLinkLocalIPs(), ips)
	})
})
