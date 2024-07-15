package pod

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iptables"
)

var _ = framework.SerialDescribe("[group:pod]", func() {
	f := framework.NewDefaultFramework("vpc-pod-probe")

	var podClient *framework.PodClient
	var eventClient *framework.EventClient
	var subnetClient *framework.SubnetClient
	var vpcClient *framework.VpcClient
	var namespaceName, subnetName, podName, vpcName string
	var subnet *apiv1.Subnet
	var cidr string
	var extraSubnetNames []string

	ginkgo.BeforeEach(ginkgo.NodeTimeout(10*time.Second), func(ctx ginkgo.SpecContext) {
		podClient = f.PodClient()
		eventClient = f.EventClient()
		subnetClient = f.SubnetClient()
		vpcClient = f.VpcClient()
		namespaceName = f.Namespace.Name
		subnetName = "subnet-" + framework.RandomSuffix()
		podName = "pod-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIPFamily)

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		subnet = subnetClient.CreateSync(ctx, subnet)
	})
	ginkgo.AfterEach(ginkgo.NodeTimeout(20*time.Second), func(ctx ginkgo.SpecContext) {
		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(ctx, podName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(ctx, subnetName)

		ginkgo.By("Deleting custom vpc " + vpcName)
		vpcClient.DeleteSync(ctx, vpcName)

		for _, subnetName := range extraSubnetNames {
			subnetClient.DeleteSync(ctx, subnetName)
		}
	})

	framework.ConformanceIt("should support http and tcp readiness probe in custom vpc pod", ginkgo.SpecTimeout(2*time.Minute), func(ctx ginkgo.SpecContext) {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12")

		daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
		originDs := daemonSetClient.Get(ctx, "kube-ovn-cni")
		modifyDs := originDs.DeepCopy()

		newArgs := originDs.Spec.Template.Spec.Containers[0].Args
		for index, arg := range newArgs {
			if arg == "--enable-tproxy=false" {
				newArgs = append(newArgs[:index], newArgs[index+1:]...)
			}
		}
		newArgs = append(newArgs, "--enable-tproxy=true")
		modifyDs.Spec.Template.Spec.Containers[0].Args = newArgs

		daemonSetClient.PatchSync(ctx, modifyDs)

		custVPCSubnetName := "subnet-" + framework.RandomSuffix()
		extraSubnetNames = append(extraSubnetNames, custVPCSubnetName)

		ginkgo.By("Create Custom Vpc subnet Pod")
		vpcName = "vpc-" + framework.RandomSuffix()
		customVPC := framework.MakeVpc(vpcName, "", false, false, nil)
		vpcClient.CreateSync(ctx, customVPC)

		ginkgo.By("Creating subnet " + custVPCSubnetName)
		cidr = framework.RandomCIDR(f.ClusterIPFamily)
		subnet := framework.MakeSubnet(custVPCSubnetName, "", cidr, "", vpcName, "", nil, nil, nil)
		_ = subnetClient.CreateSync(ctx, subnet)

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
		}
		pod = podClient.CreateSync(ctx, pod)
		checkTProxyRules(ctx, f, pod, port, true)

		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(ctx, podName)

		ginkgo.By("Creating pod with HTTP readiness probe that port is not accessible " + podName)
		pod = framework.MakePod(namespaceName, podName, nil, map[string]string{util.LogicalSwitchAnnotation: custVPCSubnetName}, framework.AgnhostImage, nil, args)
		pod.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Port: intstr.FromInt32(port + 1),
				},
			},
		}
		_ = podClient.Create(ctx, pod)

		ginkgo.By("Waiting for pod readiness probe failure")
		events := eventClient.WaitToHaveEvent(ctx, "Pod", podName, "Warning", "Unhealthy", "kubelet", "")
		var found bool
		for _, event := range events {
			if strings.Contains(event.Message, "Readiness probe failed") {
				found = true
				framework.Logf("Found pod event: %s", event.Message)
				break
			}
		}
		framework.ExpectTrue(found, "Pod readiness probe is expected to fail")

		pod = podClient.Get(ctx, podName)
		checkTProxyRules(ctx, f, pod, port+1, true)

		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(ctx, podName)

		ginkgo.By("Creating pod with TCP readiness probe that port is accessible " + podName)
		pod = framework.MakePod(namespaceName, podName, nil, map[string]string{util.LogicalSwitchAnnotation: custVPCSubnetName}, framework.AgnhostImage, nil, args)
		pod.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt32(port),
				},
			},
		}
		pod = podClient.CreateSync(ctx, pod)
		checkTProxyRules(ctx, f, pod, port, true)

		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(ctx, podName)

		ginkgo.By("Creating pod with TCP readiness probe that port is not accessible " + podName)
		pod = framework.MakePod(namespaceName, podName, nil, map[string]string{util.LogicalSwitchAnnotation: custVPCSubnetName}, framework.AgnhostImage, nil, args)
		pod.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt32(port - 1),
				},
			},
		}
		_ = podClient.Create(ctx, pod)
		podClient.WaitForRunning(ctx, podName)

		ginkgo.By("Waiting for pod readiness probe failure")
		events = eventClient.WaitToHaveEvent(ctx, "Pod", podName, "Warning", "Unhealthy", "kubelet", "")
		found = false
		for _, event := range events {
			if strings.Contains(event.Message, "Readiness probe failed") {
				found = true
				framework.Logf("Found pod event: %s", event.Message)
				break
			}
		}
		framework.ExpectTrue(found, "Pod readiness probe is expected to fail")

		pod = podClient.Get(ctx, podName)
		checkTProxyRules(ctx, f, pod, port-1, false)
	})
})

func checkTProxyRules(ctx context.Context, f *framework.Framework, pod *corev1.Pod, probePort int32, exist bool) {
	ginkgo.GinkgoHelper()

	nodeName := pod.Spec.NodeName
	tProxyOutputMarkMask := fmt.Sprintf("%#x/%#x", util.TProxyOutputMark, util.TProxyOutputMask)
	tProxyPreRoutingMarkMask := fmt.Sprintf("%#x/%#x", util.TProxyPreroutingMark, util.TProxyPreroutingMask)

	isZeroIP := false
	if len(pod.Status.PodIPs) == 2 {
		isZeroIP = true
	}

	for _, podIP := range pod.Status.PodIPs {
		if util.CheckProtocol(podIP.IP) == apiv1.ProtocolIPv4 {
			expectedRules := []string{
				fmt.Sprintf(`-A OVN-OUTPUT -d %s/32 -p tcp -m tcp --dport %d -j MARK --set-xmark %s`, podIP.IP, probePort, tProxyOutputMarkMask),
			}
			iptables.CheckIptablesRulesOnNode(ctx, f, nodeName, util.Mangle, util.OvnOutput, apiv1.ProtocolIPv4, expectedRules, exist)
			hostIP := pod.Status.HostIP
			if isZeroIP {
				hostIP = net.IPv4zero.String()
			}
			expectedRules = []string{
				fmt.Sprintf(`-A OVN-PREROUTING -d %s/32 -p tcp -m tcp --dport %d -j TPROXY --on-port %d --on-ip %s --tproxy-mark %s`, podIP.IP, probePort, util.TProxyListenPort, hostIP, tProxyPreRoutingMarkMask),
			}
			iptables.CheckIptablesRulesOnNode(ctx, f, nodeName, util.Mangle, util.OvnPrerouting, apiv1.ProtocolIPv4, expectedRules, exist)
		} else if util.CheckProtocol(podIP.IP) == apiv1.ProtocolIPv6 {
			expectedRules := []string{
				fmt.Sprintf(`-A OVN-OUTPUT -d %s/128 -p tcp -m tcp --dport %d -j MARK --set-xmark %s`, podIP.IP, probePort, tProxyOutputMarkMask),
			}
			iptables.CheckIptablesRulesOnNode(ctx, f, nodeName, util.Mangle, util.OvnOutput, apiv1.ProtocolIPv6, expectedRules, exist)

			hostIP := pod.Status.HostIP
			if isZeroIP {
				hostIP = "::"
			}
			expectedRules = []string{
				fmt.Sprintf(`-A OVN-PREROUTING -d %s/128 -p tcp -m tcp --dport %d -j TPROXY --on-port %d --on-ip %s --tproxy-mark %s`, podIP.IP, probePort, util.TProxyListenPort, hostIP, tProxyPreRoutingMarkMask),
			}
			iptables.CheckIptablesRulesOnNode(ctx, f, nodeName, util.Mangle, util.OvnPrerouting, apiv1.ProtocolIPv6, expectedRules, exist)
		}
	}
}
