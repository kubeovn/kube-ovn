package bgp_lb_eip

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
)

var bgpPeerContainers = []string{"clab-bgp-router", "clab-bgp-router-1", "clab-bgp-router-2"}

const (
	// This subnet is only an IPAM pool for bgp_lb_vip allocation.
	// It is created as a non-OVN subnet by assigning a Multus NAD-backed provider.
	// The advertised VIP still uses node addresses as BGP next hops and is serviced
	// by kube-proxy/IPVS on nodes; this test is not exercising custom VPC semantics.
	bgpLbVipSubnetCIDR = "198.51.100.0/24"
	bgpLbVipSubnetGW   = "198.51.100.1"
)

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	config.CopyFlags(config.Flags, flag.CommandLine)
	k8sframework.RegisterCommonFlags(flag.CommandLine)
	k8sframework.RegisterClusterFlags(flag.CommandLine)
}

func TestE2E(t *testing.T) {
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)
	e2e.RunE2ETests(t)
}

var _ = framework.SerialDescribe("[group:bgp-lb-eip]", func() {
	f := framework.NewDefaultFramework("bgp-lb-eip")

	var cs clientset.Interface
	var nadClient *framework.NetworkAttachmentDefinitionClient
	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient
	var serviceClient *framework.ServiceClient
	var vipClient *framework.VipClient
	var namespaceName, serviceName, vipName, serverPodName, subnetName, nadName, provider string

	ginkgo.BeforeEach(func() {
		// BGP LB VIP feature introduced in v1.17.
		f.SkipVersionPriorTo(1, 17, "BGP LB VIP (enable-bgp-lb-vip) was introduced in v1.17")

		cs = f.ClientSet
		nadClient = f.NetworkAttachmentDefinitionClient()
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		serviceClient = f.ServiceClient()
		vipClient = f.VipClient()
		namespaceName = f.Namespace.Name
		nadName = "nad-" + framework.RandomSuffix()
		provider = fmt.Sprintf("%s.%s", nadName, namespaceName)
		subnetName = "subnet-" + framework.RandomSuffix()
		vipName = "vip-" + framework.RandomSuffix()
		serviceName = "svc-" + framework.RandomSuffix()
		serverPodName = "server-" + framework.RandomSuffix()
	})

	ginkgo.AfterEach(func() {
		// Service is created by every test case.
		ginkgo.By("Deleting service " + serviceName)
		serviceClient.DeleteSync(serviceName)
		// serverPod, VIP, Subnet, and NAD are only created by specific test cases.
		// Those tests register their own cleanup via ginkgo.DeferCleanup so that
		// AfterEach does not attempt to delete resources that were never created
		// (e.g. the "noop" test case).
	})

	framework.ConformanceIt("should bind bgp_lb_vip to LoadBalancer Service and set externalIPs", func() {
		ginkgo.By("Step 0: Creating backend pod for the LoadBalancer Service")
		labels := map[string]string{"app": serviceName}
		serverArgs := []string{"netexec", "--http-port", "8080"}
		serverPod := framework.MakePod(namespaceName, serverPodName, labels, nil, framework.AgnhostImage, nil, serverArgs)
		serverPod = podClient.CreateSync(serverPod)
		framework.ExpectNotNil(serverPod)

		ginkgo.By("Step 1: Creating a Multus NAD for a non-OVN BGP LB VIP subnet")
		// This NAD is a provider-registration placeholder: no Pod will ever attach to it and
		// the CNI config (including any ipam block) is never executed at runtime.
		// Its only purpose is to supply a non-OVN provider string (<nadName>.<namespace>)
		// so that kube-ovn controller treats the Subnet as IPAM-only and skips OVN LSP creation.
		// kube-ovn controller IPAM (VIP allocation) operates on subnet.spec.provider directly
		// and is entirely independent of the NAD's CNI-level ipam field.
		nad := framework.MakeMacvlanNetworkAttachmentDefinition(nadName, namespaceName, "eth0", "bridge", provider, nil)
		nad = nadClient.Create(nad)
		framework.ExpectNotEmpty(nad.Spec.Config)

		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting server pod " + serverPodName)
			podClient.DeleteSync(serverPodName)
			ginkgo.By("Deleting VIP " + vipName)
			vipClient.DeleteSync(vipName)
			ginkgo.By("Deleting subnet " + subnetName)
			subnetClient.DeleteSync(subnetName)
			ginkgo.By("Deleting network attachment definition " + nadName)
			nadClient.Delete(nadName)
		})

		ginkgo.By("Step 1.1: Creating a dedicated non-OVN subnet for BGP LB VIP allocation")
		subnet := framework.MakeSubnet(subnetName, "", bgpLbVipSubnetCIDR, bgpLbVipSubnetGW, "", provider, nil, nil, []string{namespaceName})
		subnet = subnetClient.CreateSync(subnet)
		framework.ExpectEqual(subnet.Spec.CIDRBlock, bgpLbVipSubnetCIDR)
		framework.ExpectEqual(subnet.Spec.Provider, provider)
		framework.ExpectEqual(subnet.Spec.Vpc, "")

		ginkgo.By("Step 1.2: Creating a BGP LB VIP from the dedicated non-OVN subnet")
		vip := framework.MakeVip(namespaceName, vipName, subnetName, "", "", util.BgpLbVip)
		vip = vipClient.CreateSync(vip)
		framework.ExpectNotEmpty(vip.Status.V4ip, "VIP status.v4ip should be set after creation")
		vipIP := vip.Status.V4ip
		framework.Logf("bgp_lb_vip %s allocated IP: %s", vipName, vipIP)

		ginkgo.By("Step 2: Creating LoadBalancer Service with ovn.kubernetes.io/bgp-vip annotation")
		annotations := map[string]string{
			util.BgpVipAnnotation: vipName,
		}
		ports := []corev1.ServicePort{{
			Name:       "tcp",
			Protocol:   corev1.ProtocolTCP,
			Port:       8080,
			TargetPort: intstr.FromInt32(8080),
		}}
		svc := framework.MakeService(serviceName, corev1.ServiceTypeLoadBalancer, annotations, labels, ports, corev1.ServiceAffinityNone)
		svc.Spec.AllocateLoadBalancerNodePorts = new(false)
		_ = serviceClient.Create(svc)

		ginkgo.By("Step 3: Waiting for status.loadBalancer.ingress to be set")
		framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			svc = serviceClient.Get(serviceName)
			return len(svc.Status.LoadBalancer.Ingress) > 0, nil
		}, "status.loadBalancer.ingress is not empty")

		framework.ExpectHaveLen(svc.Status.LoadBalancer.Ingress, 1)
		framework.ExpectEqual(svc.Status.LoadBalancer.Ingress[0].IP, vipIP,
			"status.loadBalancer.ingress[0].IP should equal the VIP IP")

		ginkgo.By("Step 4: Verifying spec.externalIPs is exactly [vipIP]")
		framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			svc = serviceClient.Get(serviceName)
			return len(svc.Spec.ExternalIPs) == 1 && svc.Spec.ExternalIPs[0] == vipIP, nil
		}, fmt.Sprintf("spec.externalIPs should be exactly [%s]", vipIP))
		framework.ExpectEqual(svc.Spec.ExternalIPs, []string{vipIP},
			"spec.externalIPs must contain exactly the VIP IP and nothing else")

		ginkgo.By("Step 5: Verifying ovn.kubernetes.io/bgp annotation is set to 'true'")
		svc = serviceClient.Get(serviceName)
		framework.ExpectEqual(svc.Annotations[util.BgpAnnotation], "true",
			"controller must set the BGP speaker annotation so the IP is announced")

		ginkgo.By("Step 6: Verifying VIP is still intact after binding")
		vip = vipClient.Get(vipName)
		framework.ExpectEqual(vip.Status.V4ip, vipIP, "VIP IP should not change")

		nodeIPs := getReadyNodeInternalIPv4s(cs)
		reachablePeers := requireReachableBgpPeers()

		ginkgo.By("Step 6.1: Verifying external BGP peer learns the VIP route with a node IP as next hop")
		ensureBgpPeerRoute(reachablePeers, vipIP, nodeIPs)

		ginkgo.By("Step 6.1.1: Verifying the external BGP peer itself can access LB EIP")
		ensureBgpPeerServiceConnectivity(reachablePeers, vipIP, "8080", serverPodName)

		ginkgo.By("Step 7: Updating Service annotation to a second VIP — expecting reconcile")
		vip2Name := "vip-" + framework.RandomSuffix()
		vip2 := framework.MakeVip(namespaceName, vip2Name, subnetName, "", "", util.BgpLbVip)
		vip2 = vipClient.CreateSync(vip2)
		framework.ExpectNotEmpty(vip2.Status.V4ip)
		ginkgo.DeferCleanup(func() {
			cleanupBgpVipBinding(serviceClient, vipClient, serviceName, vip2Name)
		})
		vip2IP := vip2.Status.V4ip
		framework.Logf("second bgp_lb_vip %s allocated IP: %s", vip2Name, vip2IP)

		originalSvc := serviceClient.Get(serviceName)
		modifiedSvc := originalSvc.DeepCopy()
		modifiedSvc.Annotations[util.BgpVipAnnotation] = vip2Name
		serviceClient.Patch(originalSvc, modifiedSvc)

		framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			svc = serviceClient.Get(serviceName)
			for _, ingress := range svc.Status.LoadBalancer.Ingress {
				if ingress.IP == vip2IP {
					return true, nil
				}
			}
			return false, nil
		}, fmt.Sprintf("status.loadBalancer.ingress should be updated to %s", vip2IP))

		// After VIP switch, externalIPs must be replaced (not accumulated).
		svc = serviceClient.Get(serviceName)
		framework.ExpectEqual(svc.Spec.ExternalIPs, []string{vip2IP},
			"spec.externalIPs must be replaced with the new VIP IP, old IP must be gone")

		ginkgo.By("Step 7.1: Verifying external BGP peer learns the replacement VIP route with a node IP as next hop")
		ensureBgpPeerRoute(reachablePeers, vip2IP, nodeIPs)

		ginkgo.By("Step 7.1.1: Verifying the original VIP route is withdrawn from the external BGP peer")
		ensureBgpPeerRouteWithdrawn(reachablePeers, vipIP)

		ginkgo.By("Step 7.1.2: Verifying the external BGP peer itself can access the replacement LB EIP")
		ensureBgpPeerServiceConnectivity(reachablePeers, vip2IP, "8080", serverPodName)

		ginkgo.By("Cleaning up second VIP")
		cleanupBgpVipBinding(serviceClient, vipClient, serviceName, vip2Name)

		ginkgo.By("Verifying cs is reachable for test teardown")
		_, err := cs.CoreV1().Services(namespaceName).Get(context.Background(), serviceName, metav1.GetOptions{})
		framework.ExpectNoError(err)
	})

	framework.ConformanceIt("should be a noop for Service without bgp-vip annotation", func() {
		ginkgo.By("Creating LoadBalancer Service without ovn.kubernetes.io/bgp-vip annotation")
		labels := map[string]string{"app": serviceName}
		ports := []corev1.ServicePort{{
			Name:       "tcp",
			Protocol:   corev1.ProtocolTCP,
			Port:       80,
			TargetPort: intstr.FromInt32(80),
		}}
		svc := framework.MakeService(serviceName, corev1.ServiceTypeLoadBalancer, nil, labels, ports, corev1.ServiceAffinityNone)
		svc.Spec.AllocateLoadBalancerNodePorts = new(false)
		_ = serviceClient.Create(svc)

		ginkgo.By("Confirming bgp-lb-vip controller does not set spec.externalIPs")
		framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			svc = serviceClient.Get(serviceName)
			_, hasBgp := svc.Annotations[util.BgpAnnotation]
			return len(svc.Spec.ExternalIPs) == 0 && len(svc.Status.LoadBalancer.Ingress) == 0 && !hasBgp, nil
		}, "service without bgp-vip annotation should not be reconciled by bgp-lb-vip controller")
		framework.ExpectEmpty(svc.Spec.ExternalIPs,
			"bgp-lb-vip controller must not set spec.externalIPs on a service without the bgp-vip annotation")
		framework.ExpectEmpty(svc.Status.LoadBalancer.Ingress,
			"bgp-lb-vip controller must not set status.loadBalancer.ingress on a service without the bgp-vip annotation")
	})

	framework.ConformanceIt("should unbind bgp_lb_vip when bgp-vip annotation is removed", func() {
		ginkgo.By("Step 0: Creating backend pod for the LoadBalancer Service")
		labels := map[string]string{"app": serviceName}
		serverArgs := []string{"netexec", "--http-port", "8080"}
		serverPod := framework.MakePod(namespaceName, serverPodName, labels, nil, framework.AgnhostImage, nil, serverArgs)
		serverPod = podClient.CreateSync(serverPod)
		framework.ExpectNotNil(serverPod)

		ginkgo.By("Step 1: Creating a Multus NAD for a non-OVN BGP LB VIP subnet")
		// This NAD is a provider-registration placeholder: no Pod will ever attach to it and
		// the CNI config (including any ipam block) is never executed at runtime.
		// Its only purpose is to supply a non-OVN provider string (<nadName>.<namespace>)
		// so that kube-ovn controller treats the Subnet as IPAM-only and skips OVN LSP creation.
		// kube-ovn controller IPAM (VIP allocation) operates on subnet.spec.provider directly
		// and is entirely independent of the NAD's CNI-level ipam field.
		nad := framework.MakeMacvlanNetworkAttachmentDefinition(nadName, namespaceName, "eth0", "bridge", provider, nil)
		nad = nadClient.Create(nad)
		framework.ExpectNotEmpty(nad.Spec.Config)

		ginkgo.DeferCleanup(func() {
			ginkgo.By("Deleting server pod " + serverPodName)
			podClient.DeleteSync(serverPodName)
			ginkgo.By("Deleting VIP " + vipName)
			vipClient.DeleteSync(vipName)
			ginkgo.By("Deleting subnet " + subnetName)
			subnetClient.DeleteSync(subnetName)
			ginkgo.By("Deleting network attachment definition " + nadName)
			nadClient.Delete(nadName)
		})

		ginkgo.By("Step 1.1: Creating a dedicated non-OVN subnet for BGP LB VIP allocation")
		subnet := framework.MakeSubnet(subnetName, "", bgpLbVipSubnetCIDR, bgpLbVipSubnetGW, "", provider, nil, nil, []string{namespaceName})
		subnet = subnetClient.CreateSync(subnet)
		framework.ExpectEqual(subnet.Spec.Provider, provider)
		framework.ExpectEqual(subnet.Spec.Vpc, "")

		ginkgo.By("Step 1.2: Creating a BGP LB VIP from the dedicated non-OVN subnet")
		vip := framework.MakeVip(namespaceName, vipName, subnetName, "", "", util.BgpLbVip)
		vip = vipClient.CreateSync(vip)
		framework.ExpectNotEmpty(vip.Status.V4ip)
		vipIP := vip.Status.V4ip

		ginkgo.By("Step 2: Creating a LoadBalancer Service bound to the BGP LB VIP")
		annotations := map[string]string{util.BgpVipAnnotation: vipName}
		ports := []corev1.ServicePort{{
			Name:       "tcp",
			Protocol:   corev1.ProtocolTCP,
			Port:       8080,
			TargetPort: intstr.FromInt32(8080),
		}}
		svc := framework.MakeService(serviceName, corev1.ServiceTypeLoadBalancer, annotations, labels, ports, corev1.ServiceAffinityNone)
		svc.Spec.AllocateLoadBalancerNodePorts = new(false)
		_ = serviceClient.Create(svc)

		ginkgo.By("Step 3: Waiting for Service binding to converge")
		framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			svc = serviceClient.Get(serviceName)
			return len(svc.Status.LoadBalancer.Ingress) == 1 &&
				len(svc.Spec.ExternalIPs) == 1 &&
				svc.Status.LoadBalancer.Ingress[0].IP == vipIP &&
				svc.Spec.ExternalIPs[0] == vipIP &&
				svc.Annotations[util.BgpAnnotation] == "true", nil
		}, "service is bound to the bgp-lb-vip")

		nodeIPs := getReadyNodeInternalIPv4s(cs)
		reachablePeers := requireReachableBgpPeers()

		ginkgo.By("Step 4: Verifying the external BGP peer learns the VIP route before unbind")
		ensureBgpPeerRoute(reachablePeers, vipIP, nodeIPs)

		ginkgo.By("Step 5: Removing the ovn.kubernetes.io/bgp-vip annotation from the Service")
		originalSvc := serviceClient.Get(serviceName)
		modifiedSvc := originalSvc.DeepCopy()
		delete(modifiedSvc.Annotations, util.BgpVipAnnotation)
		serviceClient.Patch(originalSvc, modifiedSvc)

		ginkgo.By("Step 6: Verifying Service binding state is cleaned up")
		framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			svc = serviceClient.Get(serviceName)
			_, hasBgp := svc.Annotations[util.BgpAnnotation]
			_, hasBgpVip := svc.Annotations[util.BgpVipAnnotation]
			return len(svc.Spec.ExternalIPs) == 0 &&
				len(svc.Status.LoadBalancer.Ingress) == 0 &&
				!hasBgp && !hasBgpVip, nil
		}, "service binding state is cleaned after bgp-vip annotation removal")

		ginkgo.By("Step 7: Verifying the VIP route is withdrawn from the external BGP peer")
		ensureBgpPeerRouteWithdrawn(reachablePeers, vipIP)

		ginkgo.By("Step 8: Verifying the VIP itself still exists for reuse")
		vip = vipClient.Get(vipName)
		framework.ExpectEqual(vip.Status.V4ip, vipIP, "VIP IP should remain allocated after Service unbind")
	})
})

// Ensure vipClient deletion happens even when vip2 was created mid-test.
// vip2 is cleaned up inline in the test itself; AfterEach only deletes the primary vipName.
// Using apiv1 import to suppress "imported and not used" if only used in this file via MakeVip.
var _ = apiv1.SchemeGroupVersion

func ensureBgpPeerRoute(reachablePeers []string, vipIP string, nodeIPs []string) {
	ginkgo.GinkgoHelper()

	prefix := vipIP + "/32"
	framework.WaitUntil(3*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		for _, container := range reachablePeers {
			stdout, _, err := docker.Exec(container, nil, "vtysh", "-c", fmt.Sprintf("show ip bgp %s", prefix))
			if err != nil {
				continue
			}
			output := string(stdout)
			if routeUsesNodeNextHop(output, prefix, nodeIPs) {
				framework.Logf("BGP peer %s learned route %s with a node IP next hop", container, prefix)
				return true, nil
			}
		}
		return false, nil
	}, "external BGP peer learns route "+prefix+" with a node IP next hop")
}

func ensureBgpPeerRouteWithdrawn(reachablePeers []string, vipIP string) {
	ginkgo.GinkgoHelper()

	prefix := vipIP + "/32"
	framework.WaitUntil(3*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		for _, container := range reachablePeers {
			stdout, _, err := docker.Exec(container, nil, "vtysh", "-c", fmt.Sprintf("show ip bgp %s", prefix))
			if err != nil {
				continue
			}
			if strings.Contains(string(stdout), prefix) {
				return false, nil
			}
		}
		return true, nil
	}, "original BGP route "+prefix+" is withdrawn from external peers")
}

func routeUsesNodeNextHop(routeOutput, prefix string, nodeIPs []string) bool {
	if !strings.Contains(routeOutput, prefix) {
		return false
	}

	for _, nodeIP := range nodeIPs {
		if strings.Contains(routeOutput, nodeIP) {
			return true
		}
	}

	return false
}

func ensureBgpPeerServiceConnectivity(reachablePeers []string, vipIP, port, expectedBackend string) {
	ginkgo.GinkgoHelper()

	url := fmt.Sprintf("http://%s:%s/hostname", vipIP, port)
	cmd := buildBgpPeerHTTPCmd(url)
	framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
		for _, container := range reachablePeers {
			stdout, _, err := docker.Exec(container, nil, "sh", "-c", cmd)
			if err != nil {
				continue
			}
			if strings.TrimSpace(string(stdout)) == expectedBackend {
				framework.Logf("BGP peer %s reached service %s and hit backend %s", container, url, expectedBackend)
				return true, nil
			}
		}
		return false, nil
	}, "external BGP peer reaches "+url)
}

func requireReachableBgpPeers() []string {
	ginkgo.GinkgoHelper()

	reachablePeers := make([]string, 0, len(bgpPeerContainers))
	for _, container := range bgpPeerContainers {
		if _, _, err := docker.Exec(container, nil, "sh", "-c", "echo ready"); err == nil {
			reachablePeers = append(reachablePeers, container)
		}
	}

	if len(reachablePeers) == 0 {
		framework.Failf("bgp-lb-eip verification requires an external FRR peer container, but none is reachable")
	}

	return reachablePeers
}

func getReadyNodeInternalIPv4s(cs clientset.Interface) []string {
	ginkgo.GinkgoHelper()

	nodes, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
	framework.ExpectNoError(err)

	nodeIPs := make([]string, 0, len(nodes.Items))
	for _, node := range nodes.Items {
		ipv4, _ := util.GetNodeInternalIP(node)
		if ipv4 != "" {
			nodeIPs = append(nodeIPs, ipv4)
		}
	}

	framework.ExpectNotEmpty(nodeIPs, "at least one ready node internal IPv4 is required for BGP next-hop verification")
	return nodeIPs
}

func buildBgpPeerHTTPCmd(url string) string {
	return fmt.Sprintf("if command -v curl >/dev/null 2>&1; then curl -q -s --connect-timeout 2 --max-time 2 %s; elif command -v wget >/dev/null 2>&1; then wget -qO- --timeout=2 %s; else exit 127; fi", url, url)
}

func cleanupBgpVipBinding(serviceClient *framework.ServiceClient, vipClient *framework.VipClient, serviceName, vipName string) {
	ginkgo.GinkgoHelper()

	service, err := serviceClient.ServiceInterface.Get(context.Background(), serviceName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			framework.Failf("failed to get service %s during bgp vip cleanup: %v", serviceName, err)
		}
		vipClient.DeleteSync(vipName)
		return
	}

	annotationVip := service.Annotations[util.BgpVipAnnotation]
	if annotationVip == vipName {
		modifiedSvc := service.DeepCopy()
		delete(modifiedSvc.Annotations, util.BgpVipAnnotation)
		serviceClient.Patch(service, modifiedSvc)
	}

	vipClient.DeleteSync(vipName)
}
