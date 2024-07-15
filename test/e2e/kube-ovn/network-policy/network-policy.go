package network_policy

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.SerialDescribe("[group:network-policy]", func() {
	f := framework.NewDefaultFramework("network-policy")

	var subnet *apiv1.Subnet
	var cs clientset.Interface
	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient
	var defaultServiceClient *framework.ServiceClient
	var netpolClient *framework.NetworkPolicyClient
	var daemonSetClient *framework.DaemonSetClient
	var namespaceName, netpolName, subnetName, podName string
	var cidr string

	ginkgo.BeforeEach(ginkgo.NodeTimeout(time.Second), func(_ ginkgo.SpecContext) {
		cs = f.ClientSet
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		defaultServiceClient = f.ServiceClientNS("default")
		netpolClient = f.NetworkPolicyClient()
		daemonSetClient = f.DaemonSetClientNS(framework.KubeOvnNamespace)
		namespaceName = f.Namespace.Name
		netpolName = "netpol-" + framework.RandomSuffix()
		podName = "pod-" + framework.RandomSuffix()
		subnetName = "subnet-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIPFamily)
	})
	ginkgo.AfterEach(ginkgo.NodeTimeout(20*time.Second), func(ctx ginkgo.SpecContext) {
		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(ctx, podName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(ctx, subnetName)

		ginkgo.By("Deleting network policy " + netpolName)
		netpolClient.DeleteSync(ctx, netpolName)
	})

	framework.ConformanceIt("should be able to access pods from node after creating a network policy with empty ingress rules", ginkgo.SpecTimeout(90*time.Second), func(ctx ginkgo.SpecContext) {
		ginkgo.By("Creating network policy " + netpolName)
		netpol := &netv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: netpolName,
			},
			Spec: netv1.NetworkPolicySpec{
				Ingress: []netv1.NetworkPolicyIngressRule{},
			},
		}
		_ = netpolClient.Create(ctx, netpol)

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet = subnetClient.CreateSync(ctx, subnet)

		ginkgo.By("Creating pod " + podName)
		port := strconv.Itoa(8000 + rand.IntN(1000))
		args := []string{"netexec", "--http-port", port}
		annotations := map[string]string{util.LogicalSwitchAnnotation: subnetName}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, framework.AgnhostImage, nil, args)
		pod = podClient.CreateSync(ctx, pod)

		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(ctx, cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)

		ginkgo.By("Getting daemonset kube-ovn-cni")
		ds := daemonSetClient.Get(ctx, "kube-ovn-cni")

		ginkgo.By("Getting kube-ovn-cni pods")
		pods := make([]corev1.Pod, 0, len(nodeList.Items))
		for _, node := range nodeList.Items {
			pod, err := daemonSetClient.GetPodOnNode(ctx, ds, node.Name)
			framework.ExpectNoError(err, "failed to get kube-ovn-cni pod running on node %s", node.Name)
			pods = append(pods, *pod)
		}

		for _, podIP := range pod.Status.PodIPs {
			ip := podIP.IP
			protocol := strings.ToLower(util.CheckProtocol(ip))
			cmd := fmt.Sprintf("curl -q -s --connect-timeout 2 --max-time 2 %s", net.JoinHostPort(ip, port))

			var podSameNode *corev1.Pod
			for _, hostPod := range pods {
				nodeName := hostPod.Spec.NodeName
				if nodeName == pod.Spec.NodeName {
					podSameNode = hostPod.DeepCopy()
					continue
				}

				ginkgo.By("Checking connection from node " + nodeName + " to " + podName + " via " + protocol)
				ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, hostPod.Namespace, hostPod.Name))
				framework.WaitUntil(ctx, time.Minute, func(ctx context.Context) (bool, error) {
					_, _, err := framework.KubectlExec(ctx, hostPod.Namespace, hostPod.Name, cmd)
					return err != nil, nil
				}, "")
			}

			ginkgo.By("Checking connection from node " + podSameNode.Spec.NodeName + " to " + podName + " via " + protocol)
			ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, podSameNode.Namespace, podSameNode.Name))
			framework.WaitUntil(ctx, time.Minute, func(ctx context.Context) (bool, error) {
				_, _, err := framework.KubectlExec(ctx, podSameNode.Namespace, podSameNode.Name, cmd)
				return err == nil, nil
			}, "")

			// check one more time
			for _, hostPod := range pods {
				nodeName := hostPod.Spec.NodeName
				if nodeName == pod.Spec.NodeName {
					continue
				}

				ginkgo.By("Checking connection from node " + nodeName + " to " + podName + " via " + protocol)
				ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, hostPod.Namespace, hostPod.Name))
				_, _, err := framework.KubectlExec(ctx, hostPod.Namespace, hostPod.Name, cmd)
				framework.ExpectError(err)
			}
		}
	})

	framework.ConformanceIt(
		"should be able to access svc with backend host network pod after any other ingress network policy rules created",
		ginkgo.SpecTimeout(time.Minute),
		func(ctx ginkgo.SpecContext) {
			ginkgo.By("Creating network policy " + netpolName)
			netpol := &netv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: netpolName,
				},
				Spec: netv1.NetworkPolicySpec{
					Ingress: []netv1.NetworkPolicyIngressRule{
						{
							From: []netv1.NetworkPolicyPeer{
								{
									PodSelector:       nil,
									NamespaceSelector: nil,
									IPBlock:           &netv1.IPBlock{CIDR: "0.0.0.0/0", Except: []string{"127.0.0.1/32"}},
								},
							},
						},
					},
					PolicyTypes: []netv1.PolicyType{netv1.PolicyTypeIngress},
				},
			}
			_ = netpolClient.Create(ctx, netpol)

			ginkgo.By("Creating pod " + podName)
			pod := framework.MakePod(namespaceName, podName, nil, nil, framework.AgnhostImage, nil, nil)
			pod = podClient.CreateSync(ctx, pod)

			svc := defaultServiceClient.Get(ctx, "kubernetes")
			clusterIP := svc.Spec.ClusterIP

			ginkgo.By("Checking connection from pod " + podName + " to " + clusterIP + " via TCP")

			cmd := fmt.Sprintf("curl -k -q -s --connect-timeout 2 https://%s", net.JoinHostPort(clusterIP, "443"))
			ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, pod.Namespace, pod.Name))

			framework.WaitUntil(ctx, time.Minute, func(ctx context.Context) (bool, error) {
				_, _, err := framework.KubectlExec(ctx, pod.Namespace, pod.Name, cmd)
				return err == nil, nil
			}, "")
		},
	)
})
