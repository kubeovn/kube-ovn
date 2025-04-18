package ipsec

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
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

func checkPodXfrmState(pod corev1.Pod, node1IP, node2IP string) {
	ginkgo.GinkgoHelper()

	ginkgo.By("Checking ip xfrm state for pod " + pod.Name + " on node " + pod.Spec.NodeName + " from " + node1IP + " to " + node2IP)
	output, err := e2epodoutput.RunHostCmd(pod.Namespace, pod.Name, "ip xfrm state")
	framework.ExpectNoError(err)

	var count int
	for line := range strings.SplitSeq(output, "\n") {
		if line == fmt.Sprintf("src %s dst %s", node1IP, node2IP) {
			count++
		}
	}
	framework.ExpectEqual(count, 2)
}

func checkXfrmState(pods []corev1.Pod, node1IP, node2IP string) {
	ginkgo.GinkgoHelper()

	for _, pod := range pods {
		checkPodXfrmState(pod, node1IP, node2IP)
		checkPodXfrmState(pod, node2IP, node1IP)
	}
}

var _ = framework.SerialDescribe("[group:ipsec]", func() {
	f := framework.NewDefaultFramework("ipsec")

	var podClient *framework.PodClient
	var podName string
	var cs clientset.Interface

	ginkgo.BeforeEach(func() {
		podClient = f.PodClient()
		cs = f.ClientSet
		podName = "pod-" + framework.RandomSuffix()
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)
	})

	framework.ConformanceIt("Should support OVN IPSec", func() {
		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)
		framework.ExpectTrue(len(nodeList.Items) >= 2)

		ginkgo.By("Getting kube-ovn-cni pods")
		daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
		ds := daemonSetClient.Get("kube-ovn-cni")
		podList, err := daemonSetClient.GetPods(ds)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(podList.Items, len(nodeList.Items))
		nodeIPs := make([]string, 0, len(nodeList.Items))
		for _, node := range nodeList.Items {
			for _, addr := range node.Status.Addresses {
				if addr.Type == corev1.NodeInternalIP {
					nodeIPs = append(nodeIPs, node.Status.Addresses[0].Address)
					break
				}
			}
		}
		framework.ExpectHaveLen(nodeIPs, len(nodeList.Items))

		ginkgo.By("Checking ip xfrm state")
		checkXfrmState(podList.Items, nodeIPs[0], nodeIPs[1])

		ginkgo.By("Restarting ds kube-ovn-cni")
		daemonSetClient.RestartSync(ds)

		ds = daemonSetClient.Get("kube-ovn-cni")
		podList, err = daemonSetClient.GetPods(ds)
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(podList.Items, len(nodeList.Items))

		ginkgo.By("Checking ip xfrm state")
		checkXfrmState(podList.Items, nodeIPs[0], nodeIPs[1])

		ginkgo.By("Restarting ds ovs-ovn")
		ds = daemonSetClient.Get("ovs-ovn")
		daemonSetClient.RestartSync(ds)

		ginkgo.By("Checking ip xfrm state")
		checkXfrmState(podList.Items, nodeIPs[0], nodeIPs[1])
	})
})
