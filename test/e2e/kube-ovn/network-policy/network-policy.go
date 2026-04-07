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
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

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

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		defaultServiceClient = f.ServiceClientNS(metav1.NamespaceDefault)
		netpolClient = f.NetworkPolicyClient()
		daemonSetClient = f.DaemonSetClientNS(framework.KubeOvnNamespace)
		namespaceName = f.Namespace.Name
		netpolName = "netpol-" + framework.RandomSuffix()
		podName = "pod-" + framework.RandomSuffix()
		subnetName = "subnet-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIPFamily)
	})
	ginkgo.AfterEach(func() {
		// Level 1: Delete pod and network policy in parallel
		ginkgo.By("Deleting pod " + podName + " and network policy " + netpolName)
		podClient.DeleteGracefully(podName)
		netpolClient.Delete(netpolName)

		podClient.WaitForNotFound(podName)
		framework.ExpectNoError(netpolClient.WaitToDisappear(netpolName, 0, 2*time.Minute))

		// Level 2: Subnet (needs pod deleted first)
		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
	})

	framework.ConformanceIt("should be able to access pods from node after creating a network policy with empty ingress rules", func() {
		ginkgo.By("Creating network policy " + netpolName)
		netpol := &netv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: netpolName,
			},
			Spec: netv1.NetworkPolicySpec{
				Ingress: []netv1.NetworkPolicyIngressRule{},
			},
		}
		_ = netpolClient.Create(netpol)

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating pod " + podName)
		port := strconv.Itoa(8000 + rand.IntN(1000))
		args := []string{"netexec", "--http-port", port}
		annotations := map[string]string{util.LogicalSwitchAnnotation: subnetName}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, framework.AgnhostImage, nil, args)
		pod = podClient.CreateSync(pod)

		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)

		ginkgo.By("Getting daemonset kube-ovn-cni")
		ds := daemonSetClient.Get("kube-ovn-cni")

		ginkgo.By("Getting kube-ovn-cni pods")
		pods := make([]corev1.Pod, 0, len(nodeList.Items))
		for _, node := range nodeList.Items {
			pod, err := daemonSetClient.GetPodOnNode(ds, node.Name)
			framework.ExpectNoError(err, "failed to get kube-ovn-cni pod running on node %s", node.Name)
			pods = append(pods, *pod)
		}

		for _, podIP := range pod.Status.PodIPs {
			ip := podIP.IP
			protocol := strings.ToLower(util.CheckProtocol(ip))
			cmd := "curl -q -s --connect-timeout 2 --max-time 2 " + net.JoinHostPort(ip, port)

			var podSameNode *corev1.Pod
			for _, hostPod := range pods {
				nodeName := hostPod.Spec.NodeName
				if nodeName == pod.Spec.NodeName {
					podSameNode = hostPod.DeepCopy()
					continue
				}

				ginkgo.By("Checking connection from node " + nodeName + " to " + podName + " via " + protocol)
				ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, hostPod.Namespace, hostPod.Name))
				framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
					_, err := e2epodoutput.RunHostCmd(hostPod.Namespace, hostPod.Name, cmd)
					return err != nil, nil
				}, "")
			}

			ginkgo.By("Checking connection from node " + podSameNode.Spec.NodeName + " to " + podName + " via " + protocol)
			ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, podSameNode.Namespace, podSameNode.Name))
			framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
				_, err := e2epodoutput.RunHostCmd(podSameNode.Namespace, podSameNode.Name, cmd)
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
				_, err := e2epodoutput.RunHostCmd(hostPod.Namespace, hostPod.Name, cmd)
				framework.ExpectError(err)
			}
		}
	})

	framework.ConformanceIt("should be able to access svc with backend host network pod after any other ingress network policy rules created", func() {
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
		_ = netpolClient.Create(netpol)

		ginkgo.By("Creating pod " + podName)
		pod := framework.MakePod(namespaceName, podName, nil, nil, framework.AgnhostImage, nil, nil)
		pod = podClient.CreateSync(pod)

		svc := defaultServiceClient.Get("kubernetes")
		clusterIP := svc.Spec.ClusterIP

		ginkgo.By("Checking connection from pod " + podName + " to " + clusterIP + " via TCP")

		cmd := "curl -k -q -s --connect-timeout 2 https://" + net.JoinHostPort(clusterIP, "443")
		ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, pod.Namespace, pod.Name))

		framework.WaitUntil(time.Second, 3*time.Minute, func(_ context.Context) (bool, error) {
			_, err := e2epodoutput.RunHostCmd(pod.Namespace, pod.Name, cmd)
			return err == nil, nil
		}, "")
	})

	framework.ConformanceIt("should correctly handle multiple IPBlocks in a single ingress rule", func() {
		ginkgo.By("Creating server pod " + podName)
		port := strconv.Itoa(8000 + rand.IntN(1000))
		args := []string{"netexec", "--http-port", port}
		pod := framework.MakePod(namespaceName, podName, map[string]string{"app": "server"}, nil, framework.AgnhostImage, nil, args)
		pod = podClient.CreateSync(pod)

		ginkgo.By("Creating client pod 1 " + "client1-" + podName)
		client1PodName := "client1-" + podName
		client1Pod := framework.MakePod(namespaceName, client1PodName, map[string]string{"app": "client"}, nil, framework.AgnhostImage, nil, nil)
		client1Pod = podClient.CreateSync(client1Pod)

		var client1IP string
		for _, podIP := range client1Pod.Status.PodIPs {
			if strings.EqualFold(util.CheckProtocol(podIP.IP), f.ClusterIPFamily) {
				client1IP = podIP.IP
				break
			}
		}
		if client1IP == "" && f.IsDual() {
			client1IP = client1Pod.Status.PodIP
		}
		framework.ExpectNotEmpty(client1IP)

		ginkgo.By("Creating client pod 2 " + "client2-" + podName)
		client2PodName := "client2-" + podName
		client2Pod := framework.MakePod(namespaceName, client2PodName, map[string]string{"app": "client"}, nil, framework.AgnhostImage, nil, nil)
		client2Pod = podClient.CreateSync(client2Pod)

		var client2IP string
		for _, podIP := range client2Pod.Status.PodIPs {
			if strings.EqualFold(util.CheckProtocol(podIP.IP), f.ClusterIPFamily) {
				client2IP = podIP.IP
				break
			}
		}
		if client2IP == "" && f.IsDual() {
			client2IP = client2Pod.Status.PodIP
		}
		framework.ExpectNotEmpty(client2IP)

		ginkgo.By("Creating network policy " + netpolName + " with two IPBlocks")
		// The first IPBlock matches client1 IP.
		// The second IPBlock is some other dummy CIDR.
		// client2 IP is not included initially.
		mask := "/32"
		dummyCIDR := "1.2.3.4/32"
		if f.ClusterIPFamily == "ipv6" {
			mask = "/128"
			dummyCIDR = "fd00::1234/128"
		}
		netpol := &netv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: netpolName,
			},
			Spec: netv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "server"},
				},
				Ingress: []netv1.NetworkPolicyIngressRule{
					{
						From: []netv1.NetworkPolicyPeer{
							{
								IPBlock: &netv1.IPBlock{
									CIDR: client1IP + mask,
								},
							},
							{
								IPBlock: &netv1.IPBlock{
									CIDR: dummyCIDR,
								},
							},
						},
					},
				},
				PolicyTypes: []netv1.PolicyType{netv1.PolicyTypeIngress},
			},
		}
		_ = netpolClient.Create(netpol)

		for _, podIP := range pod.Status.PodIPs {
			ip := podIP.IP
			protocol := strings.ToLower(util.CheckProtocol(ip))
			if protocol != strings.ToLower(f.ClusterIPFamily) {
				continue
			}

			cmd := "curl -q -s --connect-timeout 2 --max-time 2 " + net.JoinHostPort(ip, port)

			ginkgo.By("Checking connection from client1 pod " + client1PodName + " to " + podName + " via " + protocol)
			ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, client1Pod.Namespace, client1Pod.Name))
			framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
				_, err := e2epodoutput.RunHostCmd(client1Pod.Namespace, client1Pod.Name, cmd)
				return err == nil, nil
			}, "Connection from client1 should be allowed by IPBlock")

			ginkgo.By("Checking connection from client2 pod " + client2PodName + " to " + podName + " via " + protocol + " (should be denied)")
			ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, client2Pod.Namespace, client2Pod.Name))
			framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
				_, err := e2epodoutput.RunHostCmd(client2Pod.Namespace, client2Pod.Name, cmd)
				return err != nil, nil
			}, "Connection from client2 should be denied by NetworkPolicy")
		}

		ginkgo.By("Updating network policy " + netpolName + " to include client2 pod CIDR")
		netpol = netpolClient.Get(netpolName)
		// Update the second IPBlock to include client2IP
		netpol.Spec.Ingress[0].From[1].IPBlock.CIDR = client2IP + mask
		_, err := netpolClient.Update(context.TODO(), netpol, metav1.UpdateOptions{})
		framework.ExpectNoError(err)

		for _, podIP := range pod.Status.PodIPs {
			ip := podIP.IP
			protocol := strings.ToLower(util.CheckProtocol(ip))
			if protocol != strings.ToLower(f.ClusterIPFamily) {
				continue
			}

			cmd := "curl -q -s --connect-timeout 2 --max-time 2 " + net.JoinHostPort(ip, port)

			ginkgo.By("Checking connection from client1 pod " + client1PodName + " to " + podName + " via " + protocol)
			ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, client1Pod.Namespace, client1Pod.Name))
			framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
				_, err := e2epodoutput.RunHostCmd(client1Pod.Namespace, client1Pod.Name, cmd)
				return err == nil, nil
			}, "Connection from client1 should still be allowed")

			ginkgo.By("Checking connection from client2 pod " + client2PodName + " to " + podName + " via " + protocol)
			ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, client2Pod.Namespace, client2Pod.Name))
			framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
				_, err := e2epodoutput.RunHostCmd(client2Pod.Namespace, client2Pod.Name, cmd)
				return err == nil, nil
			}, "Connection from client2 should now be allowed by updated IPBlock")
		}

		podClient.DeleteGracefully(client1PodName)
		podClient.DeleteGracefully(client2PodName)
	})
})
