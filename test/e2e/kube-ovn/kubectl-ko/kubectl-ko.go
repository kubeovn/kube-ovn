package kubectl_ko

import (
	"fmt"
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
	var namespaceName, kubectlConfig string
	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		podClient = f.PodClient()
		namespaceName = f.Namespace.Name
		kubectlConfig = k8sframework.TestContext.KubeConfig
		k8sframework.TestContext.KubeConfig = ""
	})
	ginkgo.AfterEach(func() {
		k8sframework.TestContext.KubeConfig = kubectlConfig
	})

	framework.ConformanceIt(`should succeed to execute "kubectl ko nbctl show"`, func() {
		execOrDie("ko nbctl show")
	})

	framework.ConformanceIt(`should succeed to execute "kubectl ko sbctl show"`, func() {
		execOrDie("ko sbctl show")
	})

	framework.ConformanceIt(`should succeed to execute "kubectl ko vsctl <node> show"`, func() {
		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(cs)
		framework.ExpectNoError(err)

		for _, node := range nodeList.Items {
			execOrDie(fmt.Sprintf("ko vsctl %s show", node.Name))
		}
	})

	framework.ConformanceIt(`should succeed to execute "kubectl ko ofctl <node> show br-int"`, func() {
		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(cs)
		framework.ExpectNoError(err)

		for _, node := range nodeList.Items {
			execOrDie(fmt.Sprintf("ko ofctl %s show br-int", node.Name))
		}
	})

	framework.ConformanceIt(`should succeed to execute "kubectl ko dpctl <node> show"`, func() {
		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(cs)
		framework.ExpectNoError(err)

		for _, node := range nodeList.Items {
			execOrDie(fmt.Sprintf("ko dpctl %s show", node.Name))
		}
	})

	framework.ConformanceIt(`should succeed to execute "kubectl ko appctl <node> list-commands"`, func() {
		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(cs)
		framework.ExpectNoError(err)

		for _, node := range nodeList.Items {
			execOrDie(fmt.Sprintf("ko appctl %s list-commands", node.Name))
		}
	})

	framework.ConformanceIt(`should succeed to execute "kubectl ko nb/sb status/backup"`, func() {
		databases := [...]string{"nb", "sb"}
		actions := [...]string{"status", "backup"}
		for _, db := range databases {
			for _, action := range actions {
				execOrDie(fmt.Sprintf("ko %s %s", db, action))
				// TODO: verify backup files are present
			}
		}
	})

	framework.ConformanceIt(`should succeed to execute "kubectl ko tcpdump <pod> -c1"`, func() {
		name := "pod-" + framework.RandomSuffix()
		ginkgo.By("Creating pod " + name)
		ping, target := "ping", targetIPv4
		if f.IPv6() {
			ping, target = "ping6", targetIPv6
		}

		cmd := []string{"sh", "-c", fmt.Sprintf(`while true; do %s -c1 -w1 %s; sleep 1; done`, ping, target)}
		pod := framework.MakePod(namespaceName, name, nil, nil, framework.BusyBoxImage, cmd, nil)
		pod.Spec.TerminationGracePeriodSeconds = new(int64)
		pod = podClient.CreateSync(pod)

		execOrDie(fmt.Sprintf("ko tcpdump %s/%s -c1", pod.Namespace, pod.Name))

		ginkgo.By("Deleting pod " + name)
		podClient.DeleteSync(pod.Name)
	})

	framework.ConformanceIt(`should succeed to execute "kubectl ko trace <pod> <args...>"`, func() {
		name := "pod-" + framework.RandomSuffix()
		ginkgo.By("Creating pod " + name)
		pod := framework.MakePod(namespaceName, name, nil, nil, "", nil, nil)
		pod = podClient.CreateSync(pod)

		for _, ip := range pod.Status.PodIPs {
			target := targetIPv4
			if util.CheckProtocol(ip.IP) == apiv1.ProtocolIPv6 {
				target = targetIPv6
			}

			prefix := fmt.Sprintf("ko trace %s/%s %s", pod.Namespace, pod.Name, target)
			execOrDie(fmt.Sprintf("%s icmp", prefix))
			execOrDie(fmt.Sprintf("%s tcp 80", prefix))
			execOrDie(fmt.Sprintf("%s udp 53", prefix))
		}

		ginkgo.By("Deleting pod " + name)
		podClient.DeleteSync(pod.Name)
	})

	framework.ConformanceIt(`should succeed to execute "kubectl ko log kube-ovn all"`, func() {
		components := [...]string{"kube-ovn", "ovn", "ovs", "linux"}
		for _, component := range components {
			execOrDie(fmt.Sprintf("ko log %s all 10", component))
		}

		subComponentMap := make(map[string][]string)
		subComponentMap["kube-ovn"] = []string{"kube-ovn-controller", "kube-ovn-cni", "kube-ovn-pinger", "kube-ovn-monitor"}
		subComponentMap["ovs"] = []string{"ovs-vswitchd", "ovsdb-server"}
		subComponentMap["ovn"] = []string{"ovn-controller", "ovn-northd", "ovsdb-server-nb", "ovsdb-server-sb"}
		subComponentMap["linux"] = []string{"dmesg", "iptables", "route", "link", "neigh", "memory", "top"}

		for component, subComponents := range subComponentMap {
			for _, subComponent := range subComponents {
				execOrDie(fmt.Sprintf("ko log %s %s 10", component, subComponent))
			}
		}
	})
})
