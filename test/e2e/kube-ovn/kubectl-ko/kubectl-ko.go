package kubectl_ko

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientset "k8s.io/client-go/kubernetes"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	e2ekubectl "k8s.io/kubernetes/test/e2e/framework/kubectl"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	"k8s.io/utils/ptr"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

const (
	targetIPv4 = "8.8.8.8"
	targetIPv6 = "2001:4860:4860::8888"
)

func execOrDie(cmd string, checks ...func(string)) {
	ginkgo.GinkgoHelper()
	ginkgo.By(`Executing "kubectl ` + cmd + `"`)
	output := e2ekubectl.NewKubectlCommand("", strings.Fields(cmd)...).ExecOrDie("")
	for _, check := range checks {
		check(output)
	}
}

var _ = framework.Describe("[group:kubectl-ko]", func() {
	f := framework.NewDefaultFramework("kubectl-ko")

	var cs clientset.Interface
	var podClient *framework.PodClient
	var serviceClient *framework.ServiceClient
	var netpolClient *framework.NetworkPolicyClient
	var namespaceName, podName, pod2Name, serviceName, netpolName, kubectlConfig string
	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		podClient = f.PodClient()
		serviceClient = f.ServiceClient()
		netpolClient = f.NetworkPolicyClient()
		namespaceName = f.Namespace.Name
		podName = "pod-" + framework.RandomSuffix()
		pod2Name = "pod-" + framework.RandomSuffix()
		serviceName = "svc-" + framework.RandomSuffix()
		netpolName = "netpol-" + framework.RandomSuffix()
		kubectlConfig = k8sframework.TestContext.KubeConfig
		k8sframework.TestContext.KubeConfig = ""
	})
	ginkgo.AfterEach(func() {
		k8sframework.TestContext.KubeConfig = kubectlConfig

		ginkgo.By("Deleting network policy " + netpolName)
		netpolClient.DeleteSync(netpolName)

		ginkgo.By("Deleting service " + serviceName)
		serviceClient.DeleteSync(serviceName)

		ginkgo.By("Deleting pod " + pod2Name)
		podClient.DeleteSync(pod2Name)

		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)
	})

	framework.ConformanceIt(`should support "kubectl ko nbctl show"`, func() {
		execOrDie("ko nbctl show")
	})

	framework.ConformanceIt(`should support "kubectl ko sbctl show"`, func() {
		execOrDie("ko sbctl show")
	})

	framework.ConformanceIt(`should support "kubectl ko vsctl <node> show"`, func() {
		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)

		for _, node := range nodeList.Items {
			execOrDie(fmt.Sprintf("ko vsctl %s show", node.Name))
		}
	})

	framework.ConformanceIt(`should support "kubectl ko ofctl <node> show br-int"`, func() {
		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)

		for _, node := range nodeList.Items {
			execOrDie(fmt.Sprintf("ko ofctl %s show br-int", node.Name))
		}
	})

	framework.ConformanceIt(`should support "kubectl ko dpctl <node> show"`, func() {
		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)

		for _, node := range nodeList.Items {
			execOrDie(fmt.Sprintf("ko dpctl %s show", node.Name))
		}
	})

	framework.ConformanceIt(`should support "kubectl ko appctl <node> list-commands"`, func() {
		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)

		for _, node := range nodeList.Items {
			execOrDie(fmt.Sprintf("ko appctl %s list-commands", node.Name))
		}
	})

	framework.ConformanceIt(`should support "kubectl ko nb/sb status/backup"`, func() {
		databases := [...]string{"nb", "sb"}
		actions := [...]string{"status", "backup"}
		for _, db := range databases {
			for _, action := range actions {
				execOrDie(fmt.Sprintf("ko %s %s", db, action))
				// TODO: verify backup files are present
			}
		}
	})

	framework.ConformanceIt(`should support "kubectl ko tcpdump <pod> -c1"`, func() {
		ping, target := "ping", targetIPv4
		if f.IsIPv6() {
			ping, target = "ping6", targetIPv6
		}

		ginkgo.By("Creating pod " + podName)
		cmd := []string{"sh", "-c", fmt.Sprintf(`while true; do %s -c1 -w1 %s; sleep 1; done`, ping, target)}
		pod := framework.MakePod(namespaceName, podName, nil, nil, f.KubeOVNImage, cmd, nil)
		pod = podClient.CreateSync(pod)

		execOrDie(fmt.Sprintf("ko tcpdump %s/%s -c1", pod.Namespace, pod.Name))
	})

	framework.ConformanceIt(`should support "kubectl ko trace <pod> <args...>"`, func() {
		ginkgo.By("Creating pod " + podName)
		pod := framework.MakePod(namespaceName, podName, nil, nil, "", nil, nil)
		pod = podClient.CreateSync(pod)

		supportARP := !f.VersionPriorTo(1, 11)
		supportDstMAC := !f.VersionPriorTo(1, 10)
		if !supportARP {
			framework.Logf("Support for ARP was introduced in v1.11")
		}
		if !supportDstMAC {
			framework.Logf("Support for destination MAC was introduced in v1.10")
		}

		for _, ip := range pod.Status.PodIPs {
			target, testARP := targetIPv4, supportARP
			if util.CheckProtocol(ip.IP) == apiv1.ProtocolIPv6 {
				target, testARP = targetIPv6, false
			}

			targetMAC := util.GenerateMac()
			prefix := fmt.Sprintf("ko trace %s/%s %s", pod.Namespace, pod.Name, target)
			if testARP {
				execOrDie(fmt.Sprintf("%s %s arp reply", prefix, targetMAC))
			}

			targetMACs := []string{"", targetMAC}
			for _, mac := range targetMACs {
				if mac != "" && !supportDstMAC {
					continue
				}
				if testARP {
					execOrDie(fmt.Sprintf("%s %s arp", prefix, mac))
					execOrDie(fmt.Sprintf("%s %s arp request", prefix, mac))
				}
				execOrDie(fmt.Sprintf("%s %s icmp", prefix, mac))
				execOrDie(fmt.Sprintf("%s %s tcp 80", prefix, mac))
				execOrDie(fmt.Sprintf("%s %s udp 53", prefix, mac))
			}
		}
	})

	framework.ConformanceIt(`should support "kubectl ko trace <pod> <args...>" for pod with host network`, func() {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12")

		ginkgo.By("Creating pod " + podName + " with host network")
		pod := framework.MakePod(namespaceName, podName, nil, nil, "", nil, nil)
		pod.Spec.HostNetwork = true
		pod = podClient.CreateSync(pod)

		for _, ip := range pod.Status.PodIPs {
			target, testARP := targetIPv4, true
			if util.CheckProtocol(ip.IP) == apiv1.ProtocolIPv6 {
				target, testARP = targetIPv6, false
			}

			targetMAC := util.GenerateMac()
			prefix := fmt.Sprintf("ko trace %s/%s %s", pod.Namespace, pod.Name, target)
			if testARP {
				execOrDie(fmt.Sprintf("%s %s arp reply", prefix, targetMAC))
			}

			targetMACs := []string{"", targetMAC}
			for _, mac := range targetMACs {
				if testARP {
					execOrDie(fmt.Sprintf("%s %s arp", prefix, mac))
					execOrDie(fmt.Sprintf("%s %s arp request", prefix, mac))
				}
				execOrDie(fmt.Sprintf("%s %s icmp", prefix, mac))
				execOrDie(fmt.Sprintf("%s %s tcp 80", prefix, mac))
				execOrDie(fmt.Sprintf("%s %s udp 53", prefix, mac))
			}
		}
	})

	framework.ConformanceIt(`should support "kubectl ko trace <node> <args...>"`, func() {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12")

		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		framework.ExpectNotNil(nodeList)
		framework.ExpectNotEmpty(nodeList.Items)
		node := nodeList.Items[rand.IntN(len(nodeList.Items))]

		nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(node)
		for _, ip := range []string{nodeIPv4, nodeIPv6} {
			if ip == "" {
				continue
			}
			target, testARP := targetIPv4, true
			if util.CheckProtocol(ip) == apiv1.ProtocolIPv6 {
				target, testARP = targetIPv6, false
			}

			targetMAC := util.GenerateMac()
			prefix := fmt.Sprintf("ko trace node//%s %s", node.Name, target)
			if testARP {
				execOrDie(fmt.Sprintf("%s %s arp reply", prefix, targetMAC))
			}

			targetMACs := []string{"", targetMAC}
			for _, mac := range targetMACs {
				if testARP {
					execOrDie(fmt.Sprintf("%s %s arp", prefix, mac))
					execOrDie(fmt.Sprintf("%s %s arp request", prefix, mac))
				}
				execOrDie(fmt.Sprintf("%s %s icmp", prefix, mac))
				execOrDie(fmt.Sprintf("%s %s tcp 80", prefix, mac))
				execOrDie(fmt.Sprintf("%s %s udp 53", prefix, mac))
			}
		}
	})

	framework.ConformanceIt(`"kubectl ko trace ..." should work with network policy`, func() {
		ginkgo.By("Creating pod " + pod2Name)
		labels := map[string]string{"foo": "bar"}
		pod2 := framework.MakePod(namespaceName, pod2Name, labels, nil, "", nil, nil)
		pod2 = podClient.CreateSync(pod2)

		ginkgo.By("Creating network policy " + netpolName)
		tcpPort := 8000 + rand.Int32N(1000)
		udpPort := 8000 + rand.Int32N(1000)
		netpol := &netv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: netpolName,
			},
			Spec: netv1.NetworkPolicySpec{
				PolicyTypes: []netv1.PolicyType{netv1.PolicyTypeEgress},
				Egress: []netv1.NetworkPolicyEgressRule{{
					Ports: []netv1.NetworkPolicyPort{{
						Protocol: ptr.To(corev1.ProtocolTCP),
						Port:     new(intstr.FromInt32(tcpPort)),
					}, {
						Protocol: ptr.To(corev1.ProtocolUDP),
						Port:     new(intstr.FromInt32(udpPort)),
					}},
					To: []netv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{},
						PodSelector:       &metav1.LabelSelector{MatchLabels: labels},
					}},
				}},
			},
		}
		_ = netpolClient.Create(netpol)

		ginkgo.By("Creating service " + serviceName)
		ports := []corev1.ServicePort{{
			Name:       "tcp",
			Protocol:   corev1.ProtocolTCP,
			Port:       tcpPort,
			TargetPort: intstr.FromInt32(tcpPort),
		}, {
			Name:       "udp",
			Protocol:   corev1.ProtocolUDP,
			Port:       udpPort,
			TargetPort: intstr.FromInt32(udpPort),
		}}
		service := framework.MakeService(serviceName, corev1.ServiceTypeClusterIP, nil, labels, ports, "")
		service = serviceClient.CreateSync(service, func(s *corev1.Service) (bool, error) {
			return len(s.Spec.ClusterIPs) != 0, nil
		}, "cluster ips are not empty")

		ginkgo.By("Waiting for endpoints " + serviceName + " to be ready")
		framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
			eps, err := cs.CoreV1().Endpoints(namespaceName).Get(context.TODO(), serviceName, metav1.GetOptions{})
			if err == nil {
				for _, subset := range eps.Subsets {
					if len(subset.Addresses) > 0 {
						return true, nil
					}
				}
				return false, nil
			}
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}, fmt.Sprintf("endpoints %s has at least one ready address", serviceName))

		ginkgo.By("Creating pod " + podName)
		pod := framework.MakePod(namespaceName, podName, nil, nil, "", nil, nil)
		pod = podClient.CreateSync(pod)

		ginkgo.By("Checking trace output")
		var traceService bool
		subCmd := "ovn-trace"
		if f.VersionPriorTo(1, 12) {
			subCmd = "trace"
		}
		matchPod := fmt.Sprintf("output to %q", ovs.PodNameToPortName(pod2Name, pod2.Namespace, util.OvnProvider))
		matchLocalnet := fmt.Sprintf("output to %q", "localnet."+util.DefaultSubnet)
		checkOutput := func(output, match string) bool {
			if subCmd == "ovn-trace" {
				lines := strings.Split(strings.TrimSpace(output), "\n")
				return strings.Contains(lines[len(lines)-1], match)
			}
			return strings.Contains(output, match)
		}
		checkFunc := func(output string) {
			ginkgo.GinkgoHelper()
			var match string
			if traceService && f.VersionPriorTo(1, 11) && f.IsUnderlay() {
				match = matchLocalnet
			} else {
				match = matchPod
			}
			if !checkOutput(output, match) {
				framework.Failf("expected trace output to contain %q, but got %q", match, output)
			}
		}
		for protocol, port := range map[corev1.Protocol]int32{corev1.ProtocolTCP: tcpPort, corev1.ProtocolUDP: udpPort} {
			proto := strings.ToLower(string(protocol))
			traceService = false
			for _, ip := range pod2.Status.PodIPs {
				execOrDie(fmt.Sprintf("ko %s %s/%s %s %s %d", subCmd, pod.Namespace, pod.Name, ip.IP, proto, port), checkFunc)
			}
			traceService = true
			for _, ip := range service.Spec.ClusterIPs {
				cmd := fmt.Sprintf("ko %s %s/%s %s %s %d", subCmd, pod.Namespace, pod.Name, ip, proto, port)
				var match string
				if f.VersionPriorTo(1, 11) && f.IsUnderlay() {
					match = matchLocalnet
				} else {
					match = matchPod
				}
				// Retry Service ClusterIP trace to allow OVN LB rules to be synced
				framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
					ginkgo.By(fmt.Sprintf("Executing \"kubectl %s\"", cmd))
					output := e2ekubectl.NewKubectlCommand("", strings.Fields(cmd)...).ExecOrDie("")
					return checkOutput(output, match), nil
				}, fmt.Sprintf("trace to service %s should reach target pod", ip))
			}
		}
	})

	framework.ConformanceIt(`should support "kubectl ko log kube-ovn all"`, func() {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12")
		components := [...]string{"kube-ovn", "ovn", "ovs", "linux", "all"}
		for _, component := range components {
			execOrDie("ko log " + component)
		}
	})

	framework.ConformanceIt(`should support "kubectl ko diagnose subnet IPPorts <IPPorts>"`, func() {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12")
		execOrDie("ko diagnose subnet ovn-default")
		execOrDie("ko diagnose IPPorts tcp-1.1.1.1-53,udp-1.1.1.1-53")
	})
})
