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

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

const (
	frrRouterContainer  = "clab-bgp-router"
	workerNode          = "kube-ovn-worker"
	controlPlaneAddress = "10.0.1.2"
	workerAddress       = "10.0.1.3"
)

type frrSummary struct {
	Peers map[string]struct {
		State string `json:"state"`
	} `json:"peers"`
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

func bgpPeersEstablished() (bool, error) {
	output, err := runFRR("show bgp ipv4 unicast summary json")
	if err != nil {
		return false, err
	}
	var summary frrSummary
	if err = json.Unmarshal(output, &summary); err != nil {
		return false, fmt.Errorf("failed to parse FRR BGP summary: %w: %s", err, output)
	}
	if len(summary.Peers) != 2 {
		framework.Logf("Expected two FRR BGP peers, got %d; output=%s", len(summary.Peers), output)
		return false, nil
	}
	for address, peer := range summary.Peers {
		if peer.State != "Established" {
			framework.Logf("FRR BGP peer %s is in state %s", address, peer.State)
			return false, nil
		}
	}
	return true, nil
}

func waitForBGPPeers() {
	ginkgo.GinkgoHelper()
	framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		established, err := bgpPeersEstablished()
		if err != nil {
			framework.Logf("Failed to read FRR BGP peers: %v", err)
			return false, nil
		}
		return established, nil
	}, "both BGP speaker peers to become Established")
}

func readFRRRoute(prefix string) (*frrRoute, error) {
	return readFRRRouteWithRunner(prefix, runFRR)
}

func routePaths(prefix string) ([]frrRoutePath, error) {
	route, err := readFRRRoute(prefix)
	if err != nil {
		return nil, err
	}
	return routePathsFromRoute(route), nil
}

func waitForRoutePaths(prefix string, expected ...frrRoutePath) {
	ginkgo.GinkgoHelper()
	sortRoutePaths(expected)
	var lastObserved []frrRoutePath
	haveObservation := false
	framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		paths, err := routePaths(prefix)
		if err != nil {
			framework.Logf("Failed to read FRR route %s: %v", prefix, err)
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

func waitForRouteWithdrawal(prefix string) {
	ginkgo.GinkgoHelper()
	framework.WaitUntil(2*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
		established, err := bgpPeersEstablished()
		if err != nil {
			framework.Logf("Failed to read FRR BGP peers while waiting for route %s withdrawal: %v", prefix, err)
			return false, nil
		}
		if !established {
			return false, nil
		}

		route, err := readFRRRoute(prefix)
		if err != nil {
			framework.Logf("Failed to read FRR route %s while waiting for withdrawal: %v", prefix, err)
			return false, nil
		}
		return len(route.Paths) == 0, nil
	}, fmt.Sprintf("route %s to be absent while both BGP peers remain Established", prefix))
}

func ipv4Prefix(address string) string {
	ginkgo.GinkgoHelper()
	address = strings.TrimSpace(strings.Split(address, ",")[0])
	if strings.Contains(address, "/") {
		prefix, err := netip.ParsePrefix(address)
		framework.ExpectNoError(err)
		return prefix.Masked().String()
	}
	addr, err := netip.ParseAddr(address)
	framework.ExpectNoError(err)
	return netip.PrefixFrom(addr, addr.BitLen()).String()
}

func createPodOnNode(f *framework.Framework, name, nodeName string) (*corev1.Pod, string) {
	ginkgo.GinkgoHelper()
	pod := framework.MakePod(f.Namespace.Name, name, nil, nil, framework.AgnhostImage, nil, nil)
	pod.Spec.NodeName = nodeName
	pod = f.PodClient().CreateSync(pod)
	return pod, ipv4Prefix(pod.Annotations[util.IPAddressAnnotation])
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

func defaultSubnetIPv4CIDR(f *framework.Framework) string {
	ginkgo.GinkgoHelper()
	subnet, err := f.KubeOVNClientSet.KubeovnV1().Subnets().Get(context.TODO(), util.DefaultSubnet, metav1.GetOptions{})
	framework.ExpectNoError(err)
	return ipv4Prefix(subnet.Spec.CIDRBlock)
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
		if !f.IsIPv4() {
			ginkgo.Skip("the current BGP containerlab topology supports IPv4 only")
		}
		requireReadySpeakerPodStates(f)
		waitForBGPPeers()
	})

	ginkgo.It("should advertise and withdraw a local Pod route", func() {
		podName := "local-route-" + framework.RandomSuffix()
		_, prefix := createPodOnNode(f, podName, workerNode)

		ginkgo.By("Waiting for the worker speaker to advertise the Pod route")
		waitForRoutePaths(prefix, validNodeRoutePath(workerAddress))

		ginkgo.By("Deleting the Pod")
		f.PodClient().DeleteSync(podName)

		ginkgo.By("Waiting for the Pod route to be withdrawn")
		waitForRouteWithdrawal(prefix)
		waitForBGPPeers()
	})

	ginkgo.It("should reconcile cluster and local policies without restarting speakers", func() {
		podName := "policy-route-" + framework.RandomSuffix()
		_, podPrefix := createPodOnNode(f, podName, workerNode)
		ginkgo.DeferCleanup(func() { f.PodClient().DeleteSync(podName) })
		subnetPrefix := defaultSubnetIPv4CIDR(f)
		initialSpeakers := requireReadySpeakerPodStates(f)

		ginkgo.By("Switching the default subnet to cluster policy")
		oldPolicy := setDefaultSubnetBGPPolicy(f, "cluster")
		ginkgo.DeferCleanup(func() { setDefaultSubnetBGPPolicy(f, oldPolicy) })
		waitForRoutePaths(podPrefix,
			validNodeRoutePath(controlPlaneAddress),
			validNodeRoutePath(workerAddress),
		)
		waitForRoutePaths(subnetPrefix,
			validNodeRoutePath(controlPlaneAddress),
			validNodeRoutePath(workerAddress),
		)

		ginkgo.By("Switching the default subnet back to local policy")
		setDefaultSubnetBGPPolicy(f, "local")
		waitForRoutePaths(podPrefix, validNodeRoutePath(workerAddress))
		waitForRouteWithdrawal(subnetPrefix)
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
			ObjectMeta: metav1.ObjectMeta{
				Name:        serviceName,
				Namespace:   f.Namespace.Name,
				Annotations: map[string]string{util.BgpAnnotation: "true"},
			},
			Spec: corev1.ServiceSpec{
				Selector: labels,
				Ports: []corev1.ServicePort{{
					Name: "http",
					Port: 80,
				}},
			},
		}
		serviceClient := f.ServiceClient()
		service = serviceClient.Create(service)
		ginkgo.DeferCleanup(func() { serviceClient.DeleteSync(serviceName) })
		servicePrefix := ipv4Prefix(service.Spec.ClusterIP)

		ginkgo.By("Waiting for both speakers to advertise the ClusterIP")
		waitForRoutePaths(servicePrefix,
			validNodeRoutePath(controlPlaneAddress),
			validNodeRoutePath(workerAddress),
		)

		ginkgo.By("Removing the BGP annotation while keeping the Service")
		original := serviceClient.Get(serviceName)
		modified := original.DeepCopy()
		delete(modified.Annotations, util.BgpAnnotation)
		service = serviceClient.Patch(original, modified)
		waitForRouteWithdrawal(servicePrefix)

		current := serviceClient.Get(serviceName)
		gomega.Expect(current.Spec.ClusterIP).To(gomega.Equal(service.Spec.ClusterIP))
	})
})
