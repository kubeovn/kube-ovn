package kubectl_ko

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"

	clientset "k8s.io/client-go/kubernetes"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	e2ekubectl "k8s.io/kubernetes/test/e2e/framework/kubectl"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

const (
	targetIPv4 = "8.8.8.8"
	targetIPv6 = "2001:4860:4860::8888"
)

func execOrDie(cmd string) {
	ginkgo.By(`Executing "kubectl ` + cmd + `"`)
	e2ekubectl.NewKubectlCommand("", strings.Fields(cmd)...).ExecOrDie("")
}

var _ = framework.Describe("[group:kubectl-ko]", func() {
	f := framework.NewDefaultFramework("kubectl-ko")

	var cs clientset.Interface
	var podClient *framework.PodClient
	var namespaceName, podName, kubectlConfig string
	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		podClient = f.PodClient()
		namespaceName = f.Namespace.Name
		podName = "pod-" + framework.RandomSuffix()
		kubectlConfig = k8sframework.TestContext.KubeConfig
		k8sframework.TestContext.KubeConfig = ""
	})
	ginkgo.AfterEach(func() {
		k8sframework.TestContext.KubeConfig = kubectlConfig

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
		image := framework.GetKubeOvnImage(cs)
		cmd := []string{"sh", "-c", fmt.Sprintf(`while true; do %s -c1 -w1 %s; sleep 1; done`, ping, target)}
		pod := framework.MakePod(namespaceName, podName, nil, nil, image, cmd, nil)
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

	framework.ConformanceIt(`should support "kubectl ko log kube-ovn all"`, func() {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12")
		components := [...]string{"kube-ovn", "ovn", "ovs", "linux", "all"}
		for _, component := range components {
			execOrDie(fmt.Sprintf("ko log %s", component))
		}
	})

	framework.ConformanceIt(`should support "kubectl ko diagnose subnet IPPorts <IPPorts>"`, func() {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12")
		execOrDie("ko diagnose subnet ovn-default")
		execOrDie("ko diagnose IPPorts tcp-1.1.1.1-53,udp-1.1.1.1-53")
	})
})
