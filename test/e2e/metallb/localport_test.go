package kubevirt

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

const localportInterface = "mlb0"

type localportTestEnvironment struct {
	f                   *framework.Framework
	nodes               map[string]*kind.Node
	names               []string
	macs                map[string]string
	service             *corev1.Service
	containerID         string
	clientIPv4          string
	clientIPv6          string
	clusterName         string
	providerNetworkName string
}

func (t *localportTestEnvironment) waitInterface(nodeName string) {
	ginkgo.GinkgoHelper()
	var mac string
	framework.WaitUntil(time.Second, time.Minute, func(ctx context.Context) (bool, error) {
		node, err := t.f.ClientSet.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil || node.Labels[util.ExternalLBReadyLabel(localportInterface)] != "true" {
			return false, err
		}
		output, _, err := t.nodes[nodeName].Exec("ip", "-o", "addr", "show", "dev", localportInterface)
		if err != nil || strings.TrimSpace(string(output)) != "" {
			return false, nil
		}
		for name, expected := range map[string]string{
			"ipv4/conf/mlb0/arp_ignore": "8", "ipv4/conf/mlb0/proxy_arp": "0",
			"ipv4/conf/mlb0/proxy_arp_pvlan": "0", "ipv4/conf/mlb0/forwarding": "0",
		} {
			output, _, err = t.nodes[nodeName].Exec("cat", "/proc/sys/net/"+name)
			if err != nil || strings.TrimSpace(string(output)) != expected {
				return false, nil
			}
		}
		output, _, err = t.nodes[nodeName].Exec("ip", "-d", "link", "show", "dev", localportInterface)
		if err != nil || !strings.Contains(string(output), "addrgenmode none") {
			return false, nil
		}
		output, _, err = t.nodes[nodeName].Exec("cat", "/sys/class/net/mlb0/address")
		mac = strings.TrimSpace(string(output))
		return err == nil && mac != "", nil
	}, "addressless protected speaker interface on "+nodeName)
	if previous := t.macs[nodeName]; previous != "" {
		framework.ExpectEqual(mac, previous, "speaker MAC should survive repair on "+nodeName)
	}
	for other, existing := range t.macs {
		if other != nodeName {
			framework.ExpectNotEqual(mac, existing, "speaker MACs must be unique across nodes")
		}
	}
	t.macs[nodeName] = mac
}

func (t *localportTestEnvironment) waitAllInterfaces() {
	ginkgo.GinkgoHelper()
	for name := range t.nodes {
		t.waitInterface(name)
	}
}

func (t *localportTestEnvironment) waitInterfaceRemoved(nodeName string) {
	ginkgo.GinkgoHelper()
	framework.WaitUntil(time.Second, time.Minute, func(ctx context.Context) (bool, error) {
		node, err := t.f.ClientSet.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		_, _, err = t.nodes[nodeName].Exec("ip", "link", "show", "dev", localportInterface)
		return err != nil && node.Labels[util.ExternalLBReadyLabel(localportInterface)] == "", nil
	}, "speaker interface and readiness label removed from "+nodeName)
}

func (t *localportTestEnvironment) checkLogicalPort(subnetName string, present bool) {
	ginkgo.GinkgoHelper()
	name := util.ExternalLBLocalPortName(subnetName)
	framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
		output, stderr, err := framework.NBExec("ovn-nbctl", "--bare", "--columns=name", "find", "Logical_Switch_Port", "name="+name)
		framework.ExpectNoError(err, "%s", stderr)
		return (strings.TrimSpace(string(output)) == name) == present, nil
	}, "shared localport presence should match the subnet mode")
	if present {
		for column, expected := range map[string]string{"type": "localport", "addresses": "[unknown]", "options:localnet-arp-only": "true"} {
			output, stderr, err := framework.NBExec("ovn-nbctl", "get", "Logical_Switch_Port", name, column)
			framework.ExpectNoError(err, "%s", stderr)
			framework.ExpectEqual(strings.Trim(strings.TrimSpace(string(output)), "\""), expected)
		}
	}
}

func (t *localportTestEnvironment) checkTraffic() string {
	ginkgo.GinkgoHelper()
	vip := t.service.Status.LoadBalancer.Ingress[0].IP
	var vipNode string
	// The legacy helper identifies the leader by its physical NIC MAC. This
	// mode uses an independent MAC, so verify the leader and local backend here.
	checkReachable(t.f, t.containerID, t.clientIPv4, t.clientIPv6, vip, "80", t.clusterName, true, false)
	framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
		statuses, err := t.f.MetallbClientSet.ListServiceL2Statuses()
		if err != nil {
			return false, nil
		}
		var announcingNodes []string
		for _, status := range statuses.Items {
			if status.Status.ServiceNamespace == t.f.Namespace.Name && status.Status.ServiceName == t.service.Name {
				announcingNodes = append(announcingNodes, status.Status.Node)
			}
		}
		if len(announcingNodes) != 1 || t.macs[announcingNodes[0]] == "" {
			return false, nil
		}
		vipNode = announcingNodes[0]
		output, _, err := docker.Exec(t.containerID, nil, "ip", "neigh", "show", vip)
		return err == nil && strings.Contains(string(output), "lladdr "+t.macs[vipNode]+" "), nil
	}, "external client should learn the single announcing speaker's MAC")
	framework.WaitUntil(time.Second, time.Minute, func(ctx context.Context) (bool, error) {
		output, _, err := docker.Exec(t.containerID, nil, "curl", "-q", "-sf", "--connect-timeout", "2", "--max-time", "2", fmt.Sprintf("%s:80/hostname", vip))
		if err != nil {
			return false, nil
		}
		backend, err := t.f.ClientSet.CoreV1().Pods(t.f.Namespace.Name).Get(ctx, strings.TrimSpace(string(output)), metav1.GetOptions{})
		return err == nil && (t.service.Spec.ExternalTrafficPolicy == corev1.ServiceExternalTrafficPolicyTypeCluster || backend.Spec.NodeName == vipNode), nil
	}, "backend selection should respect ExternalTrafficPolicy")
	waitUnderlayServiceFlowCleaned(t.names, t.providerNetworkName, vip, 80, 30*time.Second)
	return vipNode
}

func (t *localportTestEnvironment) checkRecovery(nodeName string) {
	ginkgo.GinkgoHelper()
	ginkgo.By("Removing an accidentally assigned speaker IPv4 address")
	_, stderr, err := t.nodes[nodeName].Exec("ip", "addr", "add", "198.51.100.254/32", "dev", localportInterface)
	framework.ExpectNoError(err, "%s", stderr)
	t.waitInterface(nodeName)
	t.checkTraffic()

	ginkgo.By("Recreating a deleted OVS internal port with the same MAC")
	pods, err := t.f.ClientSet.CoreV1().Pods(framework.KubeOvnNamespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: "app=ovs", FieldSelector: "spec.nodeName=" + nodeName,
	})
	framework.ExpectNoError(err)
	framework.ExpectHaveLen(pods.Items, 1)
	_, stderr, err = framework.KubectlExec(framework.KubeOvnNamespace, pods.Items[0].Name, "ovs-vsctl", "--if-exists", "del-port", "br-int", localportInterface)
	framework.ExpectNoError(err, "%s", stderr)
	t.waitInterface(nodeName)
	t.checkTraffic()

	ginkgo.By("Preserving the interfaces and their MACs across a CNI daemon restart")
	client := t.f.DaemonSetClientNS(framework.KubeOvnNamespace)
	client.RestartSync(client.Get("kube-ovn-cni"))
	t.waitAllInterfaces()
	t.checkTraffic()
}

func (t *localportTestEnvironment) checkProviderExclusion(nodeName string) {
	ginkgo.GinkgoHelper()
	ginkgo.By("Withdrawing the speaker interface on an excluded provider node")
	client := t.f.ProviderNetworkClient()
	original := client.Get(t.providerNetworkName)
	modified := original.DeepCopy()
	modified.Spec.ExcludeNodes = append(modified.Spec.ExcludeNodes, nodeName)
	client.Patch(original, modified)
	defer func() {
		ginkgo.By("Restoring the provider node and its speaker interface")
		current := client.Get(t.providerNetworkName)
		restored := current.DeepCopy()
		restored.Spec.ExcludeNodes = original.Spec.ExcludeNodes
		// The restore patch is accepted by the API, and waitInterface below
		// observes the controller's actual recovery result. Waiting for a
		// ResourceVersion change is racy here because restoring the original
		// exclusion can leave the provider network status unchanged.
		client.Patch(current, restored)
		// ProviderNetwork reconciliation can briefly recompute readiness for
		// every node while the exclusion is restored.
		t.waitAllInterfaces()
		t.checkTraffic()
	}()
	t.waitInterfaceRemoved(nodeName)
	t.checkTraffic()
}
