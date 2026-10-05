package kubectl_ko

import (
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
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

type inspectedNetwork struct {
	NetNS       string               `json:"netns"`
	HostNetwork bool                 `json:"hostNetwork"`
	Interfaces  []inspectedInterface `json:"interfaces"`
}

type inspectedInterface struct {
	Name       string              `json:"name"`
	Index      int                 `json:"index"`
	Kind       string              `json:"kind"`
	MAC        string              `json:"mac"`
	MTU        int                 `json:"mtu"`
	Addresses  []string            `json:"addresses"`
	HostPeer   *inspectedInterface `json:"hostPeer"`
	Parent     *inspectedInterface `json:"parent"`
	Statistics *struct {
		RX map[string]uint64 `json:"rx"`
		TX map[string]uint64 `json:"tx"`
	} `json:"statistics"`
}

func checkInterfaceStatistics(link inspectedInterface) {
	ginkgo.GinkgoHelper()
	framework.ExpectNotNil(link.Statistics, "statistics must be reported for %s", link.Name)
	for _, counters := range []map[string]uint64{link.Statistics.RX, link.Statistics.TX} {
		for _, name := range []string{"bytes", "packets", "errors", "dropped"} {
			_, found := counters[name]
			framework.ExpectTrue(found, "%s counter must be reported for %s, including zero", name, link.Name)
		}
	}
}

func inspectNetwork(pod *corev1.Pod) inspectedNetwork {
	ginkgo.GinkgoHelper()
	output := e2ekubectl.NewKubectlCommand("", "ko", "network", "inspect", "--pod", pod.Namespace+"/"+pod.Name, "--output", "json").ExecOrDie("")
	var info inspectedNetwork
	framework.ExpectNoError(json.Unmarshal([]byte(output), &info))
	framework.ExpectNotEmpty(info.NetNS)
	framework.ExpectNotEmpty(info.Interfaces)
	return info
}

func execOrDie(cmd string, checks ...func(string)) {
	ginkgo.GinkgoHelper()
	cmd = strings.Join(framework.KubectlKoArgs(strings.Fields(cmd)...), " ")
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

		// All resources are independent (no subnet dependency), delete in parallel
		ginkgo.By("Deleting network policy " + netpolName + ", service " + serviceName + ", pods " + pod2Name + " and " + podName)
		netpolClient.Delete(netpolName)
		serviceClient.Delete(serviceName)
		podClient.DeleteGracefully(pod2Name)
		podClient.DeleteGracefully(podName)

		framework.ExpectNoError(netpolClient.WaitToDisappear(netpolName, 0, 2*time.Minute))
		framework.ExpectNoError(serviceClient.WaitToDisappear(serviceName, 0, 2*time.Minute))
		podClient.WaitForNotFound(pod2Name)
		podClient.WaitForNotFound(podName)
	})

	framework.ConformanceIt(`should support "kubectl ko nbctl show"`, func() {
		execOrDie("ko nbctl show")
	})

	framework.ConformanceIt(`should support "kubectl ko sbctl show"`, func() {
		execOrDie("ko sbctl show")
	})

	framework.ConformanceIt(`should inspect Pod netns, host veth peers and macvlan/ipvlan parents`, func() {
		f.SkipVersionPriorTo(1, 17, "Network inspection and the independent node agent were introduced in v1.17")
		pod := podClient.CreateSync(framework.MakePod(namespaceName, podName, nil, nil, "", nil, nil))
		info := inspectNetwork(pod)
		framework.ExpectFalse(info.HostNetwork)
		foundIP, foundPeer := false, false
		for _, link := range info.Interfaces {
			for _, ip := range pod.Status.PodIPs {
				for _, address := range link.Addresses {
					if strings.HasPrefix(address, ip.IP+"/") {
						foundIP = true
					}
				}
			}
			if link.Kind == "veth" && link.HostPeer != nil {
				framework.ExpectNotEmpty(link.HostPeer.Name)
				framework.ExpectTrue(link.HostPeer.Index > 0)
				checkInterfaceStatistics(link)
				checkInterfaceStatistics(*link.HostPeer)
				foundPeer = true
			}
		}
		framework.ExpectTrue(foundIP, "Pod IP must appear in interface addresses")
		framework.ExpectTrue(foundPeer, "Pod veth must expose its host peer")

		agents, err := cs.CoreV1().Pods("kube-system").List(context.Background(), metav1.ListOptions{LabelSelector: "app=kubectl-ko-node-agent", FieldSelector: "spec.nodeName=" + pod.Spec.NodeName})
		framework.ExpectNoError(err)
		framework.ExpectHaveLen(agents.Items, 1)
		agent := agents.Items[0].Name
		processNetns := strings.TrimSpace(e2ekubectl.NewKubectlCommand("", "exec", "-n", "kube-system", agent, "-c", "agent", "--", "/kube-ovn/kubectl-ko-node-agent", "netns", string(pod.UID)).ExecOrDie(""))
		inodes := strings.Fields(e2ekubectl.NewKubectlCommand("", "exec", "-n", "kube-system", agent, "-c", "agent", "--", "stat", "-Lc", "%i", info.NetNS, processNetns).ExecOrDie(""))
		framework.ExpectHaveLen(inodes, 2)
		framework.ExpectEqual(inodes[0], inodes[1], "OVS and Pod-UID resolvers must identify the same live network namespace")
		run := func(args ...string) {
			ginkgo.GinkgoHelper()
			command := append([]string{"exec", "-n", "kube-system", agent, "-c", "agent", "--", "ip"}, args...)
			e2ekubectl.NewKubectlCommand("", command...).ExecOrDie("")
		}
		for _, kind := range []string{"macvlan", "ipvlan"} {
			suffix := framework.RandomSuffix()
			parent := "ko-" + kind[:2] + "-" + suffix[len(suffix)-8:]
			child := "net-" + kind[:2]
			run("link", "add", "name", parent, "type", "dummy")
			ginkgo.DeferCleanup(func() { run("link", "delete", "dev", parent) })
			run("link", "add", "link", parent, "name", child, "netns", info.NetNS, "type", kind)
			updated := inspectNetwork(pod)
			index := slices.IndexFunc(updated.Interfaces, func(link inspectedInterface) bool { return link.Name == child })
			framework.ExpectTrue(index >= 0, "%s must be included alongside the main Pod interface", kind)
			link := updated.Interfaces[index]
			framework.ExpectEqual(link.Kind, kind)
			framework.ExpectNotNil(link.Parent)
			framework.ExpectEqual(link.Parent.Name, parent)
			framework.ExpectEqual(link.Parent.Kind, "dummy")
			framework.ExpectNotEmpty(link.Parent.MAC)
			framework.ExpectTrue(link.Parent.Index > 0 && link.Parent.MTU > 0)
			checkInterfaceStatistics(link)
			checkInterfaceStatistics(*link.Parent)
		}
		localParent := slices.IndexFunc(info.Interfaces, func(link inspectedInterface) bool { return link.Name == "eth0" })
		framework.ExpectTrue(localParent >= 0)
		runLocal := func(args ...string) {
			ginkgo.GinkgoHelper()
			command := append([]string{"exec", "-n", "kube-system", agent, "-c", "agent", "--", "nsenter", "--net=" + info.NetNS, "--", "ip"}, args...)
			e2ekubectl.NewKubectlCommand("", command...).ExecOrDie("")
		}
		for _, kind := range []string{"macvlan", "ipvlan"} {
			child := "net-local-" + kind[:2]
			runLocal("link", "add", "link", "eth0", "name", child, "type", kind)
			func() {
				// The two kinds cannot share a lower device simultaneously.
				defer runLocal("link", "delete", "dev", child)
				updated := inspectNetwork(pod)
				index := slices.IndexFunc(updated.Interfaces, func(link inspectedInterface) bool { return link.Name == child })
				framework.ExpectTrue(index >= 0)
				link := updated.Interfaces[index]
				framework.ExpectNotNil(link.Parent)
				framework.ExpectEqual(link.Parent.Index, info.Interfaces[localParent].Index)
				framework.ExpectEqual(link.Parent.MAC, info.Interfaces[localParent].MAC, "parent must be Pod eth0, not host eth0")
				checkInterfaceStatistics(*link.Parent)
			}()
		}
	})

	framework.ConformanceIt(`should inspect every host-network Pod interface`, func() {
		f.SkipVersionPriorTo(1, 17, "Network inspection was introduced in v1.17")
		pod := framework.MakePod(namespaceName, podName, nil, nil, "", nil, nil)
		pod.Spec.HostNetwork = true
		pod = podClient.CreateSync(pod)
		info := inspectNetwork(pod)
		framework.ExpectTrue(info.HostNetwork)
		framework.ExpectEqual(info.NetNS, "/proc/1/ns/net")
		framework.ExpectTrue(slices.ContainsFunc(info.Interfaces, func(link inspectedInterface) bool { return link.Name == "lo" }))
		for _, link := range info.Interfaces {
			if link.Name == "lo" {
				checkInterfaceStatistics(link)
			}
		}
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
			}
		}
	})

	framework.ConformanceIt(`should download intact standalone database backups with provenance`, func() {
		f.SkipVersionPriorTo(1, 17, "The Go plugin backup metadata was introduced in v1.17")
		directory, err := os.MkdirTemp("", "kubectl-ko-backup-")
		framework.ExpectNoError(err)
		ginkgo.DeferCleanup(func() { framework.ExpectNoError(os.RemoveAll(directory)) })
		for _, role := range []string{"nb", "sb"} {
			filename := filepath.Join(directory, role+".backup")
			e2ekubectl.NewKubectlCommand("", "ko", "db", role, "backup", "--output", filename).ExecOrDie("")
			data, err := os.ReadFile(filename)
			framework.ExpectNoError(err)
			if !strings.HasPrefix(string(data), "OVSDB JSON ") {
				framework.Failf("backup %s is not a standalone OVSDB log", filename)
			}
			metadata, err := os.ReadFile(filename + ".json")
			framework.ExpectNoError(err)
			var origin struct{ Database, SHA256, Pod, Namespace string }
			framework.ExpectNoError(json.Unmarshal(metadata, &origin))
			if origin.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) || origin.Pod == "" || origin.Namespace == "" {
				framework.Failf("backup %s checksum or provenance is invalid", filename)
			}
			database := "OVN_Northbound"
			if role == "sb" {
				database = "OVN_Southbound"
			}
			if origin.Database != database || !strings.Contains(string(data), database) {
				framework.Failf("backup %s contains the wrong database", filename)
			}
		}
	})

	framework.ConformanceIt(`should check every Linux environment with and without CNI Pods`, func() {
		f.SkipVersionPriorTo(1, 17, "The structured environment command was introduced in v1.17")
		nodes, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{LabelSelector: corev1.LabelOSStable + "=linux"})
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodes.Items)
		output := e2ekubectl.NewKubectlCommand("", "ko", "diagnose", "environment").ExecOrDie("")
		checkEnvironmentOutput(output, nodes.Items)
		checkAgentOnlyEnvironment(f, nodes.Items)
	})

	framework.ConformanceIt(`should resolve NB and SB member removal without executing it`, func() {
		f.SkipVersionPriorTo(1, 17, "The database member-removal dry-run was introduced in v1.17")
		for _, role := range []string{"nb", "sb"} {
			options := metav1.ListOptions{LabelSelector: "ovn-" + role + "-leader=true"}
			leaders, err := cs.CoreV1().Pods(framework.KubeOvnNamespace).List(context.Background(), options)
			framework.ExpectNoError(err)
			framework.ExpectHaveLen(leaders.Items, 1)
			leader := leaders.Items[0]
			database := "OVN_Northbound"
			if role == "sb" {
				database = "OVN_Southbound"
			}
			argv := []string{"ovn-appctl", "-t", "/var/run/ovn/ovn" + role + "_db.ctl", "cluster/kick", database, "ffffffff"}
			output := e2ekubectl.NewKubectlCommand("", "ko", "db", role, "kick", "ffffffff", "--dry-run").ExecOrDie("")
			framework.ExpectEqual(output, fmt.Sprintf("%s/%s: %q\n", leader.Namespace, leader.Name, argv))
			current, err := cs.CoreV1().Pods(leader.Namespace).Get(context.Background(), leader.Name, metav1.GetOptions{})
			framework.ExpectNoError(err)
			framework.ExpectEqual(current.UID, leader.UID)
			framework.ExpectEqual(current.Labels["ovn-"+role+"-leader"], "true")
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
			Name: netpolName,
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
					output := e2ekubectl.NewKubectlCommand("", framework.KubectlKoArgs(strings.Fields(cmd)...)...).ExecOrDie("")
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
		if !f.VersionPriorTo(1, 17) {
			checkIndependentIPsecCollection(f, podClient, namespaceName, podName)
		}
	})

	// Cluster health checks must not overlap other specs' IPAM failure injection
	// or Pod teardown. Keep the diagnostic checks intact and run after cleanup.
	framework.ConformanceIt(`should support "kubectl ko diagnose subnet IPPorts <IPPorts>"`, k8sframework.WithSerial(), func() {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12")
		execOrDie("ko diagnose subnet ovn-default")
		if f.VersionPriorTo(1, 17) {
			execOrDie("ko diagnose IPPorts tcp-1.1.1.1-53,udp-1.1.1.1-53")
			return
		}

		ginkgo.By("Creating a controlled TCP/UDP probe server")
		// Bind both listeners before readiness and reply to every UDP sender.
		command := `import socket
import socketserver
import threading

class TCPHandler(socketserver.BaseRequestHandler):
    def handle(self):
        self.request.sendall(b"health check")

class UDPHandler(socketserver.BaseRequestHandler):
    def handle(self):
        self.request[1].sendto(b"health check", self.client_address)

class TCPServer(socketserver.ThreadingTCPServer):
    address_family = socket.AF_INET6

    def server_bind(self):
        self.socket.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 0)
        super().server_bind()

class UDPServer(socketserver.UDPServer):
    address_family = socket.AF_INET6

    def server_bind(self):
        self.socket.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 0)
        super().server_bind()

with UDPServer(("::", 8101), UDPHandler) as udp:
    with TCPServer(("::", 8100), TCPHandler) as tcp:
        threading.Thread(target=udp.serve_forever, daemon=True).start()
        tcp.serve_forever()
`
		pod := framework.MakePod(namespaceName, podName, nil, nil, f.KubeOVNImage, []string{"python3", "-u", "-c", command}, nil)
		pod.Spec.Containers[0].ReadinessProbe = &corev1.Probe{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(8100)}}
		pod = podClient.CreateSync(pod)
		for _, ip := range pod.Status.PodIPs {
			execOrDie(fmt.Sprintf("ko diagnose IPPorts tcp-%s-8100,udp-%s-8101", ip.IP, ip.IP))
		}

		ginkgo.By("Checking that an unreachable endpoint returns a failure")
		_, err := e2ekubectl.NewKubectlCommand("", framework.KubectlKoArgs("ko", "diagnose", "IPPorts", fmt.Sprintf("tcp-%s-8102", pod.Status.PodIP))...).Exec()
		framework.ExpectError(err)
	})
})
