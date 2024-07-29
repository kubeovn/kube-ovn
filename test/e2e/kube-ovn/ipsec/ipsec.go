package ipsec

import (
	"context"
	"fmt"
	"strings"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

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
		ginkgo.By("Checking ip xfrm state")

		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)

		ginkgo.By("Getting kube-ovn-cni pods")
		daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
		ds := daemonSetClient.Get("kube-ovn-cni")
		pods := make([]corev1.Pod, 0, len(nodeList.Items))
		nodeIPs := make([]string, 0, len(nodeList.Items))
		for _, node := range nodeList.Items {
			pod, err := daemonSetClient.GetPodOnNode(ds, node.Name)
			framework.ExpectNoError(err, "failed to get kube-ovn-cni pod running on node %s", node.Name)
			pods = append(pods, *pod)
			nodeIPs = append(nodeIPs, node.Status.Addresses[0].Address)
		}

		for _, pod := range pods {
			cmd := fmt.Sprintf("ip xfrm state | grep \"src %s dst %s\" | wc -l ", nodeIPs[0], nodeIPs[1])
			output := strings.TrimSpace(e2epodoutput.RunHostCmdOrDie(pod.Namespace, pod.Name, cmd))
			framework.ExpectEqual(output, "2")

			cmd = fmt.Sprintf("ip xfrm state | grep \"src %s dst %s\" | wc -l ", nodeIPs[1], nodeIPs[0])
			output = strings.TrimSpace(e2epodoutput.RunHostCmdOrDie(pod.Namespace, pod.Name, cmd))
			framework.ExpectEqual(output, "2")
		}

		ginkgo.By("Rollout restart ovs-ovn Pod ")

	})
})
