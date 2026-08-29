package bgp

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/netip"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	"k8s.io/utils/ptr"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
)

const (
	frrRouterContainer = "clab-bgp-router"
	workerNode         = "kube-ovn-worker"
)

type bgpFamily struct {
	name, controlPlaneAddress, workerAddress, updatedWorkerAddress string
}

var (
	ipv4Family = bgpFamily{"ipv4", "10.0.1.2", "10.0.1.3", "10.0.1.4"}
	ipv6Family = bgpFamily{"ipv6", "fd00:10:1::2", "fd00:10:1::3", "fd00:10:1::4"}
)

func bgpFamilies(f *framework.Framework) []bgpFamily {
	families := make([]bgpFamily, 0, 2)
	if f.HasIPv4() {
		families = append(families, ipv4Family)
	}
	if f.HasIPv6() {
		families = append(families, ipv6Family)
	}
	return families
}

type frrPeer struct {
	State string `json:"state"`
}

type frrSummary struct {
	Peers map[string]frrPeer `json:"peers"`
}

func peersForFamily(summary frrSummary, family bgpFamily) map[string]frrPeer {
	peers := make(map[string]frrPeer)
	wantIPv6 := family.name == "ipv6"
	for address, peer := range summary.Peers {
		parsed, err := netip.ParseAddr(address)
		if err != nil || parsed.Is6() != wantIPv6 {
			continue
		}
		peers[address] = peer
	}
	return peers
}

type speakerPodState struct {
	UID          string
	RestartCount int32
}

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)
	config.CopyFlags(config.Flags, flag.CommandLine)
	k8sframework.RegisterCommonFlags(flag.CommandLine)
	k8sframework.RegisterClusterFlags(flag.CommandLine)
}

func TestE2E(t *testing.T) {
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)
	e2e.RunE2ETests(t)
}

func runFRR(command string) ([]byte, error) {
	cmd := exec.Command("docker", "exec", frrRouterContainer, "vtysh", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to run FRR command %q: %w: %s", command, err, output)
	}
	return output, nil
}

func runNodeCommand(node string, args ...string) error {
	output, stderr, err := docker.Exec(node, nil, args...)
	if err != nil {
		return fmt.Errorf("failed to execute command in %s: %w: stdout=%s stderr=%s", node, err, output, stderr)
	}
	return nil
}

func bgpPeersEstablished(family bgpFamily) (bool, error) {
	output, err := runFRR("show bgp " + family.name + " unicast summary json")
	if err != nil {
		return false, err
	}
	var summary frrSummary
	if err = json.Unmarshal(output, &summary); err != nil {
		return false, fmt.Errorf("failed to parse FRR BGP summary: %w: %s", err, output)
	}
	peers := peersForFamily(summary, family)
	if len(peers) != 2 {
		framework.Logf("Expected two %s FRR BGP peers, got %d; output=%s", family.name, len(peers), output)
		return false, nil
	}
	for address, peer := range peers {
		if peer.State != "Established" {
			framework.Logf("FRR BGP peer %s is in state %s", address, peer.State)
			return false, nil
		}
	}
	return true, nil
}

func waitForBGPPeers(f *framework.Framework) {
	ginkgo.GinkgoHelper()
	for _, family := range bgpFamilies(f) {
		framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			established, err := bgpPeersEstablished(family)
			if err != nil {
				framework.Logf("Failed to read %s FRR BGP peers: %v", family.name, err)
				return false, nil
			}
			return established, nil
		}, "both "+family.name+" BGP speaker peers to become Established")
	}
}

func readFRRRoute(prefix string, family bgpFamily) (*frrRoute, error) {
	return readFRRRouteWithFamilyWithRunner(prefix, family.name, runFRR)
}

func routePaths(prefix string, family bgpFamily) ([]frrRoutePath, error) {
	route, err := readFRRRoute(prefix, family)
	if err != nil {
		return nil, err
	}
	return routePathsFromRoute(route), nil
}

func waitForRoutePaths(family bgpFamily, prefix string, expected ...frrRoutePath) {
	ginkgo.GinkgoHelper()
	sortRoutePaths(expected)
	var lastObserved []frrRoutePath
	haveObservation := false
	framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		paths, err := routePaths(prefix, family)
		if err != nil {
			framework.Logf("Failed to read %s FRR route %s: %v", family.name, prefix, err)
			return false, nil
		}
		if slices.Equal(paths, expected) {
			return true, nil
		}
		if !haveObservation || !slices.Equal(paths, lastObserved) {
			framework.Logf("FRR route %s has paths %v, waiting for %v", prefix, paths, expected)
			lastObserved = slices.Clone(paths)
			haveObservation = true
		}
		return false, nil
	}, fmt.Sprintf("route %s to have paths %v", prefix, expected))
}

func waitForRouteWithdrawal(family bgpFamily, prefix string) {
	ginkgo.GinkgoHelper()
	framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		established, err := bgpPeersEstablished(family)
		if err != nil {
			framework.Logf("Failed to read %s FRR BGP peers while waiting for route %s withdrawal: %v", family.name, prefix, err)
			return false, nil
		}
		if !established {
			return false, nil
		}

		route, err := readFRRRoute(prefix, family)
		if err != nil {
			framework.Logf("Failed to read %s FRR route %s while waiting for withdrawal: %v", family.name, prefix, err)
			return false, nil
		}
		return len(route.Paths) == 0, nil
	}, fmt.Sprintf("route %s to be absent while both BGP peers remain Established", prefix))
}

func prefixForFamily(addresses string, family bgpFamily) string {
	ginkgo.GinkgoHelper()
	wantIPv6 := family.name == "ipv6"
	for address := range strings.SplitSeq(addresses, ",") {
		address = strings.TrimSpace(address)
		if address == "" {
			continue
		}
		if strings.Contains(address, "/") {
			prefix, err := netip.ParsePrefix(address)
			framework.ExpectNoError(err)
			if prefix.Addr().Is6() == wantIPv6 {
				return prefix.Masked().String()
			}
			continue
		}
		addr, err := netip.ParseAddr(address)
		framework.ExpectNoError(err)
		if addr.Is6() == wantIPv6 {
			return netip.PrefixFrom(addr, addr.BitLen()).String()
		}
	}
	framework.ExpectNoError(fmt.Errorf("no %s address found in %q", family.name, addresses))
	return ""
}

func createPodOnNode(f *framework.Framework, name, nodeName string) (*corev1.Pod, map[string]string) {
	ginkgo.GinkgoHelper()
	pod := framework.MakePod(f.Namespace.Name, name, nil, nil, framework.AgnhostImage, nil, nil)
	pod.Spec.NodeName = nodeName
	pod = f.PodClient().CreateSync(pod)
	prefixes := make(map[string]string, 2)
	for _, family := range bgpFamilies(f) {
		prefixes[family.name] = prefixForFamily(pod.Annotations[util.IPAddressAnnotation], family)
	}
	return pod, prefixes
}

func updateWorkerRoute(family bgpFamily) error {
	_, update, _, _ := workerSourceAddressCommands(family)
	return runNodeCommand(workerNode, update...)
}

func addWorkerSourceAddress(family bgpFamily) error {
	add, _, _, _ := workerSourceAddressCommands(family)
	return runNodeCommand(workerNode, add...)
}

func removeWorkerSourceAddress(family bgpFamily) error {
	_, _, restore, remove := workerSourceAddressCommands(family)
	// Remove the temporary source before restoring the host route; Linux may
	// delete a route that still references an address when that address is removed.
	removeErr := runNodeCommand(workerNode, remove...)
	restoreErr := runNodeCommand(workerNode, restore...)
	if restoreErr != nil && removeErr != nil {
		return fmt.Errorf("failed to restore worker route: %w; failed to remove temporary address: %w", restoreErr, removeErr)
	}
	if restoreErr != nil {
		return restoreErr
	}
	return removeErr
}

func workerSourceAddressCommands(family bgpFamily) (add, update, restore, remove []string) {
	if family.name == "ipv6" {
		return []string{"ip", "-6", "addr", "add", family.updatedWorkerAddress + "/64", "dev", "net1", "nodad"},
			[]string{"ip", "-6", "route", "replace", "fd00:10:1::1/128", "dev", "net1", "src", family.updatedWorkerAddress},
			[]string{"ip", "-6", "route", "replace", "fd00:10:1::1/128", "dev", "net1", "src", family.workerAddress},
			[]string{"ip", "-6", "addr", "del", family.updatedWorkerAddress + "/64", "dev", "net1"}
	}
	return []string{"ip", "addr", "add", family.updatedWorkerAddress + "/24", "dev", "net1"},
		[]string{"ip", "route", "replace", "10.0.1.1/32", "dev", "net1", "src", family.updatedWorkerAddress},
		[]string{"ip", "route", "replace", "10.0.1.1/32", "dev", "net1", "src", family.workerAddress},
		[]string{"ip", "addr", "del", family.updatedWorkerAddress + "/24", "dev", "net1"}
}

func setDefaultSubnetBGPPolicy(f *framework.Framework, policy string) string {
	ginkgo.GinkgoHelper()
	client := f.SubnetClient()
	original := client.Get(util.DefaultSubnet)
	oldPolicy := original.Annotations[util.BgpAnnotation]
	modified := original.DeepCopy()
	if modified.Annotations == nil {
		modified.Annotations = map[string]string{}
	}
	modified.Annotations[util.BgpAnnotation] = policy
	client.Patch(original, modified, time.Minute)
	return oldPolicy
}

func defaultSubnetCIDRs(f *framework.Framework) map[string]string {
	ginkgo.GinkgoHelper()
	subnet, err := f.KubeOVNClientSet.KubeovnV1().Subnets().Get(context.TODO(), util.DefaultSubnet, metav1.GetOptions{})
	framework.ExpectNoError(err)
	cidrs := make(map[string]string, 2)
	for _, family := range bgpFamilies(f) {
		cidrs[family.name] = prefixForFamily(subnet.Spec.CIDRBlock, family)
	}
	return cidrs
}

func requireReadySpeakerPodStates(f *framework.Framework) map[string]speakerPodState {
	ginkgo.GinkgoHelper()
	pods, err := f.ClientSet.CoreV1().Pods(framework.KubeOvnNamespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: "app=kube-ovn-speaker",
	})
	framework.ExpectNoError(err)
	gomega.Expect(pods.Items).To(gomega.HaveLen(2))

	states := make(map[string]speakerPodState, len(pods.Items))
	for _, pod := range pods.Items {
		gomega.Expect(pod.Status.ContainerStatuses).To(gomega.HaveLen(1))
		gomega.Expect(pod.Status.ContainerStatuses[0].Ready).To(gomega.BeTrue())
		states[pod.Spec.NodeName] = speakerPodState{
			UID:          string(pod.UID),
			RestartCount: pod.Status.ContainerStatuses[0].RestartCount,
		}
	}
	return states
}

func expectSpeakerPodsUnchanged(f *framework.Framework, expected map[string]speakerPodState) {
	ginkgo.GinkgoHelper()
	gomega.Expect(requireReadySpeakerPodStates(f)).To(gomega.Equal(expected))
}

var _ = framework.SerialDescribe("[group:bgp-speaker] BGP speaker", func() {
	f := framework.NewDefaultFramework("bgp-speaker")

	ginkgo.BeforeEach(func() {
		f.SkipVersionPriorTo(1, 13, "BGP local and cluster announce policies require v1.13+")
		requireReadySpeakerPodStates(f)
		waitForBGPPeers(f)
	})

	ginkgo.It("should advertise and withdraw a local Pod route", func() {
		podName := "local-route-" + framework.RandomSuffix()
		_, prefixes := createPodOnNode(f, podName, workerNode)

		ginkgo.By("Waiting for the worker speaker to advertise the Pod route")
		for _, family := range bgpFamilies(f) {
			waitForRoutePaths(family, prefixes[family.name], validNodeRoutePath(family.workerAddress))
		}

		ginkgo.By("Deleting the Pod")
		f.PodClient().DeleteSync(podName)

		ginkgo.By("Waiting for the Pod route to be withdrawn")
		for _, family := range bgpFamilies(f) {
			waitForRouteWithdrawal(family, prefixes[family.name])
		}
		waitForBGPPeers(f)
	})

	ginkgo.It("should refresh a Pod route when the node source address changes", func() {
		podName := "source-refresh-" + framework.RandomSuffix()
		_, prefixes := createPodOnNode(f, podName, workerNode)
		ginkgo.DeferCleanup(func() { f.PodClient().DeleteSync(podName) })

		ginkgo.By("Waiting for the worker speaker to advertise the Pod route with its original source address")
		for _, family := range bgpFamilies(f) {
			waitForRoutePaths(family, prefixes[family.name], validNodeRoutePath(family.workerAddress))
		}

		ginkgo.By("Changing the worker route source address without restarting the speaker")
		sourceAddressAdded := make(map[string]bool, len(bgpFamilies(f)))
		for _, family := range bgpFamilies(f) {
			ginkgo.DeferCleanup(func() {
				if !sourceAddressAdded[family.name] {
					return
				}
				if err := removeWorkerSourceAddress(family); err != nil {
					framework.Logf("failed to remove temporary %s worker source address: %v", family.name, err)
				}
			})
			framework.ExpectNoError(addWorkerSourceAddress(family))
			sourceAddressAdded[family.name] = true
			framework.ExpectNoError(updateWorkerRoute(family))
		}

		ginkgo.By("Triggering a speaker reconcile after the route source change")
		triggerName := "source-refresh-trigger-" + framework.RandomSuffix()
		createPodOnNode(f, triggerName, workerNode)
		ginkgo.DeferCleanup(func() { f.PodClient().DeleteSync(triggerName) })

		ginkgo.By("Waiting for the original Pod route to use the refreshed source address")
		for _, family := range bgpFamilies(f) {
			waitForRoutePaths(family, prefixes[family.name], validNodeRoutePathWithNextHop(family.workerAddress, family.updatedWorkerAddress))
		}

		ginkgo.By("Restoring the worker route source address and reconciling the original route")
		for _, family := range bgpFamilies(f) {
			framework.ExpectNoError(removeWorkerSourceAddress(family))
			sourceAddressAdded[family.name] = false
		}
		restoreTriggerName := "source-refresh-restore-trigger-" + framework.RandomSuffix()
		createPodOnNode(f, restoreTriggerName, workerNode)
		ginkgo.DeferCleanup(func() { f.PodClient().DeleteSync(restoreTriggerName) })
		for _, family := range bgpFamilies(f) {
			waitForRoutePaths(family, prefixes[family.name], validNodeRoutePath(family.workerAddress))
		}
	})

	ginkgo.It("should reconcile cluster and local policies without restarting speakers", func() {
		podName := "policy-route-" + framework.RandomSuffix()
		_, podPrefixes := createPodOnNode(f, podName, workerNode)
		ginkgo.DeferCleanup(func() { f.PodClient().DeleteSync(podName) })
		subnetPrefixes := defaultSubnetCIDRs(f)
		initialSpeakers := requireReadySpeakerPodStates(f)

		ginkgo.By("Switching the default subnet to cluster policy")
		oldPolicy := setDefaultSubnetBGPPolicy(f, "cluster")
		ginkgo.DeferCleanup(func() { setDefaultSubnetBGPPolicy(f, oldPolicy) })
		for _, family := range bgpFamilies(f) {
			waitForRoutePaths(family, podPrefixes[family.name],
				validNodeRoutePath(family.controlPlaneAddress),
				validNodeRoutePath(family.workerAddress),
			)
			waitForRoutePaths(family, subnetPrefixes[family.name],
				validNodeRoutePath(family.controlPlaneAddress),
				validNodeRoutePath(family.workerAddress),
			)
		}

		ginkgo.By("Switching the default subnet back to local policy")
		setDefaultSubnetBGPPolicy(f, "local")
		for _, family := range bgpFamilies(f) {
			waitForRoutePaths(family, podPrefixes[family.name], validNodeRoutePath(family.workerAddress))
			waitForRouteWithdrawal(family, subnetPrefixes[family.name])
		}
		expectSpeakerPodsUnchanged(f, initialSpeakers)
	})

	ginkgo.It("should advertise and withdraw an annotated ClusterIP Service", func() {
		podName := "service-backend-" + framework.RandomSuffix()
		serviceName := "bgp-service-" + framework.RandomSuffix()
		labels := map[string]string{"app": serviceName}
		pod := framework.MakePod(f.Namespace.Name, podName, labels, nil, framework.AgnhostImage, nil, nil)
		pod.Spec.NodeName = workerNode
		f.PodClient().CreateSync(pod)
		ginkgo.DeferCleanup(func() { f.PodClient().DeleteSync(podName) })

		service := &corev1.Service{
			Name:        serviceName,
			Namespace:   f.Namespace.Name,
			Annotations: map[string]string{util.BgpAnnotation: "true"},
			Spec: corev1.ServiceSpec{
				Selector: labels,
				Ports: []corev1.ServicePort{{
					Name: "http",
					Port: 80,
				}},
			},
		}
		serviceClient := f.ServiceClient()
		if f.IsDual() {
			service.Spec.IPFamilyPolicy = ptr.To(corev1.IPFamilyPolicyRequireDualStack)
		}
		service = serviceClient.CreateSync(service, func(s *corev1.Service) (bool, error) {
			return len(util.ServiceClusterIPs(*s)) == len(bgpFamilies(f)), nil
		}, "all BGP address family cluster IPs are allocated")
		ginkgo.DeferCleanup(func() { serviceClient.DeleteSync(serviceName) })
		servicePrefixes := make(map[string]string, 2)
		for _, family := range bgpFamilies(f) {
			servicePrefixes[family.name] = prefixForFamily(strings.Join(util.ServiceClusterIPs(*service), ","), family)
		}

		ginkgo.By("Waiting for both speakers to advertise the ClusterIP")
		for _, family := range bgpFamilies(f) {
			waitForRoutePaths(family, servicePrefixes[family.name],
				validNodeRoutePath(family.controlPlaneAddress),
				validNodeRoutePath(family.workerAddress),
			)
		}

		ginkgo.By("Removing the BGP annotation while keeping the Service")
		original := serviceClient.Get(serviceName)
		modified := original.DeepCopy()
		delete(modified.Annotations, util.BgpAnnotation)
		service = serviceClient.Patch(original, modified)
		for _, family := range bgpFamilies(f) {
			waitForRouteWithdrawal(family, servicePrefixes[family.name])
		}

		current := serviceClient.Get(serviceName)
		gomega.Expect(current.Spec.ClusterIP).To(gomega.Equal(service.Spec.ClusterIP))
	})
})
