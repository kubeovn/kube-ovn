package pod

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	kubeletevents "k8s.io/kubernetes/pkg/kubelet/events"
	kubeletserver "k8s.io/kubernetes/pkg/kubelet/server"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iptables"
)

var (
	tProxyOutputMarkMask     = fmt.Sprintf("%#x/%#x", util.TProxyOutputMark, util.TProxyOutputMask)
	tProxyPreRoutingMarkMask = fmt.Sprintf("%#x/%#x", util.TProxyPreroutingMark, util.TProxyPreroutingMask)
)

var _ = framework.SerialDescribe("[group:pod]", func() {
	f := framework.NewDefaultFramework("vpc-pod-probe")

	var podClient *framework.PodClient
	var eventClient *framework.EventClient
	var subnetClient *framework.SubnetClient
	var vpcClient *framework.VpcClient
	var namespaceName, subnetName, podName, vpcName, custVPCSubnetName string
	var subnet *apiv1.Subnet
	var cidr string

	ginkgo.BeforeEach(func() {
		podClient = f.PodClient()
		eventClient = f.EventClient()
		subnetClient = f.SubnetClient()
		vpcClient = f.VpcClient()
		namespaceName = f.Namespace.Name
		subnetName = "subnet-" + framework.RandomSuffix()
		podName = "pod-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIPFamily)
		custVPCSubnetName = "subnet-" + framework.RandomSuffix()

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		subnet = subnetClient.CreateSync(subnet)
	})
	ginkgo.AfterEach(func() {
		// Level 1: Delete pod
		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)

		// Level 2: Delete subnets in parallel
		ginkgo.By("Deleting subnet " + subnetName + " and " + custVPCSubnetName)
		subnetClient.Delete(subnetName)
		subnetClient.Delete(custVPCSubnetName)
		framework.ExpectNoError(subnetClient.WaitToDisappear(subnetName, 0, 2*time.Minute))
		framework.ExpectNoError(subnetClient.WaitToDisappear(custVPCSubnetName, 0, 2*time.Minute))

		// Level 3: VPC (needs subnets gone)
		ginkgo.By("Deleting VPC " + vpcName)
		vpcClient.DeleteSync(vpcName)
	})

	framework.ConformanceIt("should support http and tcp readiness probe in custom vpc pod", func() {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12")

		ginkgo.By("Getting kube-ovn-cni daemonset")
		daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
		originDs := daemonSetClient.Get("kube-ovn-cni")

		ginkgo.By("Enabling tproxy in kube-ovn-cni daemonset")
		modifyDs := originDs.DeepCopy()
		newArgs := modifyDs.Spec.Template.Spec.Containers[0].Args
		for index, arg := range newArgs {
			if arg == "--enable-tproxy=false" {
				newArgs = slices.Delete(newArgs, index, index+1)
			}
		}
		newArgs = append(newArgs, "--enable-tproxy=true")
		modifyDs.Spec.Template.Spec.Containers[0].Args = newArgs
		daemonSetClient.PatchSync(modifyDs)

		ginkgo.By("Creating VPC " + vpcName)
		vpcName = "vpc-" + framework.RandomSuffix()
		customVPC := framework.MakeVpc(vpcName, "", false, false, nil)
		vpcClient.CreateSync(customVPC)

		ginkgo.By("Creating subnet " + custVPCSubnetName)
		cidr = framework.RandomCIDR(f.ClusterIPFamily)
		subnet := framework.MakeSubnet(custVPCSubnetName, "", cidr, "", vpcName, "", nil, nil, nil)
		_ = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating pod with HTTP readiness probe that port is accessible " + podName)
		port := 8000 + rand.Int32N(1000)
		portStr := strconv.Itoa(int(port))
		args := []string{"netexec", "--http-port", portStr}
		pod := framework.MakePod(namespaceName, podName, nil, map[string]string{util.LogicalSwitchAnnotation: custVPCSubnetName}, framework.AgnhostImage, nil, args)
		pod.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Port: intstr.FromInt32(port),
				},
			},
			PeriodSeconds:    1,
			FailureThreshold: 1,
		}
		pod = podClient.CreateSync(pod)
		checkTProxyRules(f, pod, port, true)

		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)

		ginkgo.By("Creating pod with HTTP readiness probe that port is not accessible " + podName)
		pod = framework.MakePod(namespaceName, podName, nil, map[string]string{util.LogicalSwitchAnnotation: custVPCSubnetName}, framework.AgnhostImage, nil, args)
		pod.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Port: intstr.FromInt32(port + 1),
				},
			},
			PeriodSeconds:    1,
			FailureThreshold: 1,
		}
		_ = podClient.Create(pod)

		ginkgo.By("Waiting for pod readiness probe failure")
		events := eventClient.WaitToHaveEvent(util.KindPod, podName, corev1.EventTypeWarning, kubeletevents.ContainerUnhealthy, kubeletserver.ComponentKubelet, "")
		var found bool
		for _, event := range events {
			if strings.Contains(event.Message, "Readiness probe failed") {
				found = true
				framework.Logf("Found pod event: %s", event.Message)
				break
			}
		}
		framework.ExpectTrue(found, "Pod readiness probe is expected to fail")

		pod = podClient.GetPod(podName)
		checkTProxyRules(f, pod, port+1, true)

		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)

		ginkgo.By("Creating pod with TCP readiness probe that port is accessible " + podName)
		pod = framework.MakePod(namespaceName, podName, nil, map[string]string{util.LogicalSwitchAnnotation: custVPCSubnetName}, framework.AgnhostImage, nil, args)
		pod.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt32(port),
				},
			},
			PeriodSeconds:    1,
			FailureThreshold: 1,
		}
		pod = podClient.CreateSync(pod)
		checkTProxyRules(f, pod, port, true)

		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)

		ginkgo.By("Creating pod with TCP readiness probe that port is not accessible " + podName)
		pod = framework.MakePod(namespaceName, podName, nil, map[string]string{util.LogicalSwitchAnnotation: custVPCSubnetName}, framework.AgnhostImage, nil, args)
		pod.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt32(port - 1),
				},
			},
			PeriodSeconds:    1,
			FailureThreshold: 1,
		}
		_ = podClient.Create(pod)
		podClient.WaitForRunning(podName)

		ginkgo.By("Waiting for pod readiness probe failure")
		events = eventClient.WaitToHaveEvent(util.KindPod, podName, corev1.EventTypeWarning, kubeletevents.ContainerUnhealthy, kubeletserver.ComponentKubelet, "")
		found = false
		for _, event := range events {
			if strings.Contains(event.Message, "Readiness probe failed") {
				found = true
				framework.Logf("Found pod event: %s", event.Message)
				break
			}
		}
		framework.ExpectTrue(found, "Pod readiness probe is expected to fail")

		pod = podClient.GetPod(podName)
		checkTProxyRules(f, pod, port-1, true)
	})
})

func checkTProxyRules(f *framework.Framework, pod *corev1.Pod, probePort int32, exist bool) {
	ginkgo.GinkgoHelper()

	nodeName := pod.Spec.NodeName
	node, err := f.ClientSet.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	framework.ExpectNoError(err)

	nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(*node)
	if len(pod.Status.PodIPs) == 2 && f.VersionPriorTo(1, 13) {
		nodeIPv4, nodeIPv6 = net.IPv4zero.String(), net.IPv6zero.String()
	}

	for _, podIP := range pod.Status.PodIPs {
		if util.CheckProtocol(podIP.IP) == apiv1.ProtocolIPv4 {
			expectedRules := []string{
				fmt.Sprintf(`-A OVN-OUTPUT -d %s/32 -p tcp -m tcp --dport %d -j MARK --set-xmark %s`, podIP.IP, probePort, tProxyOutputMarkMask),
			}
			iptables.CheckIptablesRulesOnNode(f, nodeName, util.Mangle, util.OvnOutput, apiv1.ProtocolIPv4, expectedRules, exist)
			expectedRules = []string{
				fmt.Sprintf(`-A OVN-PREROUTING -d %s/32 -p tcp -m tcp --dport %d -j TPROXY --on-port %d --on-ip %s --tproxy-mark %s`, podIP.IP, probePort, util.TProxyListenPort, nodeIPv4, tProxyPreRoutingMarkMask),
			}
			iptables.CheckIptablesRulesOnNode(f, nodeName, util.Mangle, util.OvnPrerouting, apiv1.ProtocolIPv4, expectedRules, exist)
		} else if util.CheckProtocol(podIP.IP) == apiv1.ProtocolIPv6 {
			expectedRules := []string{
				fmt.Sprintf(`-A OVN-OUTPUT -d %s/128 -p tcp -m tcp --dport %d -j MARK --set-xmark %s`, podIP.IP, probePort, tProxyOutputMarkMask),
			}
			iptables.CheckIptablesRulesOnNode(f, nodeName, util.Mangle, util.OvnOutput, apiv1.ProtocolIPv6, expectedRules, exist)

			expectedRules = []string{
				fmt.Sprintf(`-A OVN-PREROUTING -d %s/128 -p tcp -m tcp --dport %d -j TPROXY --on-port %d --on-ip %s --tproxy-mark %s`, podIP.IP, probePort, util.TProxyListenPort, nodeIPv6, tProxyPreRoutingMarkMask),
			}
			iptables.CheckIptablesRulesOnNode(f, nodeName, util.Mangle, util.OvnPrerouting, apiv1.ProtocolIPv6, expectedRules, exist)
		}
	}
}
