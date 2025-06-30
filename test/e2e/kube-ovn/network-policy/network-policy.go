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
		defaultServiceClient = f.ServiceClientNS("default")
		netpolClient = f.NetworkPolicyClient()
		daemonSetClient = f.DaemonSetClientNS(framework.KubeOvnNamespace)
		namespaceName = f.Namespace.Name
		netpolName = "netpol-" + framework.RandomSuffix()
		podName = "pod-" + framework.RandomSuffix()
		subnetName = "subnet-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIPFamily)
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)

		ginkgo.By("Deleting network policy " + netpolName)
		netpolClient.DeleteSync(netpolName)
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
				framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
					_, err := e2epodoutput.RunHostCmd(hostPod.Namespace, hostPod.Name, cmd)
					return err != nil, nil
				}, "")
			}

			ginkgo.By("Checking connection from node " + podSameNode.Spec.NodeName + " to " + podName + " via " + protocol)
			ginkgo.By(fmt.Sprintf(`Executing %q in pod %s/%s`, cmd, podSameNode.Namespace, podSameNode.Name))
			framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
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

		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			_, err := e2epodoutput.RunHostCmd(pod.Namespace, pod.Name, cmd)
			return err == nil, nil
		}, "")
	})

	framework.ConformanceIt("should be able to ping node-local-dns-ip after configuring NetworkPolicies", func() {
		if f.IsUnderlay() {
			ginkgo.Skip("underlay mode is not supported node-local-dns-ip")
		}

		var nodeLocalDNSIP string
		if f.IsIPv6() {
			nodeLocalDNSIP = "fd00::10"
		} else {
			nodeLocalDNSIP = "169.254.20.10"
		}

		// Helper function to get protocol pointer
		tcpProtocol := corev1.ProtocolTCP

		ginkgo.By("Getting kube-ovn-controller deployment")
		kubeOvnDeployClient := f.DeploymentClientNS(framework.KubeOvnNamespace)
		originalDeploy := kubeOvnDeployClient.Get("kube-ovn-controller")

		ginkgo.By("Modifying kube-ovn-controller args to enable node-local-dns-ip")
		modifiedDeploy := originalDeploy.DeepCopy()
		container := &modifiedDeploy.Spec.Template.Spec.Containers[0]

		// Add node-local-dns-ip argument if not present
		nodeLocalDNSArg := "--node-local-dns-ip=" + nodeLocalDNSIP
		found := false
		for i, arg := range container.Args {
			if strings.HasPrefix(arg, "--node-local-dns-ip=") {
				container.Args[i] = nodeLocalDNSArg
				found = true
				break
			}
		}
		if !found {
			container.Args = append(container.Args, nodeLocalDNSArg)
		}

		ginkgo.By("Updating kube-ovn-controller deployment")
		_ = kubeOvnDeployClient.PatchSync(originalDeploy, modifiedDeploy)

		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)

		ginkgo.By("Getting daemonset kube-ovn-cni")
		ds := daemonSetClient.Get("kube-ovn-cni")

		ginkgo.By("Adding IP address " + nodeLocalDNSIP + "/32 to management interface on all nodes")
		cniPods := make([]corev1.Pod, 0, len(nodeList.Items))
		for _, node := range nodeList.Items {
			pod, err := daemonSetClient.GetPodOnNode(ds, node.Name)
			framework.ExpectNoError(err, "failed to get kube-ovn-cni pod running on node %s", node.Name)
			cniPods = append(cniPods, *pod)

			// Add IP to management interface (assuming eth0 is the management interface)
			addIPCmd := fmt.Sprintf("ip addr add %s/32 dev eth0", nodeLocalDNSIP)
			ginkgo.By(fmt.Sprintf("Adding IP %s/32 to eth0 on node %s", nodeLocalDNSIP, node.Name))
			_, _ = e2epodoutput.RunHostCmd(pod.Namespace, pod.Name, addIPCmd)
			// Ignore error if IP already exists
		}

		// Cleanup function to remove IP addresses
		defer func() {
			ginkgo.By("Cleaning up: removing IP address " + nodeLocalDNSIP + "/32 from management interface on all nodes")
			for _, pod := range cniPods {
				removeIPCmd := fmt.Sprintf("ip addr del %s/32 dev eth0", nodeLocalDNSIP)
				_, _ = e2epodoutput.RunHostCmd(pod.Namespace, pod.Name, removeIPCmd)
			}

			ginkgo.By("Restoring original kube-ovn-controller deployment")
			_ = kubeOvnDeployClient.PatchSync(modifiedDeploy, originalDeploy)
		}()

		ginkgo.By("Creating ingress NetworkPolicy")
		netpolClient := f.NetworkPolicyClientNS(namespaceName)
		ingressNetpol := &netv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ingress-rules",
				Namespace: namespaceName,
			},
			Spec: netv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{}, // Empty selector matches all pods
				Ingress: []netv1.NetworkPolicyIngressRule{
					{
						From: []netv1.NetworkPolicyPeer{
							{
								IPBlock: &netv1.IPBlock{CIDR: "0.0.0.0/0"},
							},
						},
						Ports: []netv1.NetworkPolicyPort{
							{
								Protocol: &tcpProtocol,
							},
						},
					},
				},
				PolicyTypes: []netv1.PolicyType{netv1.PolicyTypeIngress},
			},
		}
		_ = netpolClient.Create(ingressNetpol)

		defer func() {
			ginkgo.By("Cleaning up ingress NetworkPolicy")
			netpolClient.DeleteSync("ingress-rules")
		}()

		ginkgo.By("Creating egress NetworkPolicy")
		egressNetpol := &netv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "egress-rules",
				Namespace: namespaceName,
			},
			Spec: netv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{}, // Empty selector matches all pods
				Egress: []netv1.NetworkPolicyEgressRule{
					{
						To: []netv1.NetworkPolicyPeer{
							{
								IPBlock: &netv1.IPBlock{CIDR: "0.0.0.0/0"},
							},
						},
						Ports: []netv1.NetworkPolicyPort{
							{
								Protocol: &tcpProtocol,
							},
						},
					},
				},
				PolicyTypes: []netv1.PolicyType{netv1.PolicyTypeEgress},
			},
		}
		_ = netpolClient.Create(egressNetpol)

		defer func() {
			ginkgo.By("Cleaning up egress NetworkPolicy")
			netpolClient.DeleteSync("egress-rules")
		}()

		ginkgo.By("Creating test pod")
		pod := framework.MakePod(namespaceName, podName, nil, nil, framework.AgnhostImage, nil, nil)
		pod = podClient.CreateSync(pod)

		ginkgo.By("Testing ping connectivity to node-local-dns-ip from test pod")
		pingCmd := fmt.Sprintf("ping -W 1 -c 1 %s", nodeLocalDNSIP)
		ginkgo.By(fmt.Sprintf("Executing %q in pod %s/%s", pingCmd, pod.Namespace, pod.Name))

		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			_, err := e2epodoutput.RunHostCmd(pod.Namespace, pod.Name, pingCmd)
			return err == nil, nil
		}, "ping to node-local-dns-ip should succeed")

		ginkgo.By("Verifying ping connectivity from test pod to node-local-dns-ip")
		_, err = e2epodoutput.RunHostCmd(pod.Namespace, pod.Name, pingCmd)
		framework.ExpectNoError(err, "ping to node-local-dns-ip %s should succeed", nodeLocalDNSIP)
	})
})
