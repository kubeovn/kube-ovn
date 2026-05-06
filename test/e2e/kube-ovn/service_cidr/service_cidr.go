package service_cidr

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

// ServiceCIDR (KEP-1880) became GA in K8s 1.33. The kube-ovn integration ships
// in v1.17 — older releases either lack the controller wiring or the daemon
// store, so the test is gated on both fronts.
const skipVersionMajor, skipVersionMinor uint = 1, 17

// extraCIDR sits well outside the kind default (10.96.0.0/12) and the
// 10.0.0.0/16 pool used by RandomCIDR for subnet tests, so it never collides
// with parallel cases.
const extraCIDR = "10.250.0.0/24"

var _ = framework.Describe("[group:service-cidr]", func() {
	f := framework.NewDefaultFramework("service-cidr")

	var cs clientset.Interface
	var name string

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		name = "extra-" + framework.RandomSuffix()
	})

	ginkgo.AfterEach(func() {
		err := cs.NetworkingV1().ServiceCIDRs().Delete(context.Background(), name, metav1.DeleteOptions{})
		if err != nil && !k8serrors.IsNotFound(err) {
			framework.Failf("failed to delete ServiceCIDR %s: %v", name, err)
		}
	})

	framework.ConformanceIt("should propagate a runtime ServiceCIDR object into node ipsets and remove it on deletion", func() {
		f.SkipVersionPriorTo(skipVersionMajor, skipVersionMinor, "ServiceCIDR support landed in v1.17")
		skipIfNoServiceCIDRAPI(cs)

		ginkgo.By("Creating ServiceCIDR " + name + " (" + extraCIDR + ")")
		sc := &networkingv1.ServiceCIDR{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       networkingv1.ServiceCIDRSpec{CIDRs: []string{extraCIDR}},
		}
		_, err := cs.NetworkingV1().ServiceCIDRs().Create(context.Background(), sc, metav1.CreateOptions{})
		framework.ExpectNoError(err)

		ginkgo.By("Waiting for ServiceCIDR " + name + " to become Ready")
		framework.WaitUntil(2*time.Second, time.Minute, func(ctx context.Context) (bool, error) {
			got, err := cs.NetworkingV1().ServiceCIDRs().Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			for _, cond := range got.Status.Conditions {
				if cond.Type == networkingv1.ServiceCIDRConditionReady && cond.Status == metav1.ConditionTrue {
					return true, nil
				}
			}
			return false, nil
		}, "ServiceCIDR Ready=True")

		ginkgo.By("Listing ready schedulable nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)

		ginkgo.By("Asserting ipset ovn40services contains " + extraCIDR + " on every node")
		for _, n := range nodeList.Items {
			expectIPSetContains(f, n, extraCIDR, true)
		}

		ginkgo.By("Deleting ServiceCIDR " + name)
		framework.ExpectNoError(cs.NetworkingV1().ServiceCIDRs().Delete(context.Background(), name, metav1.DeleteOptions{}))

		ginkgo.By("Asserting ipset ovn40services no longer contains " + extraCIDR + " on every node")
		for _, n := range nodeList.Items {
			expectIPSetContains(f, n, extraCIDR, false)
		}
	})
})

// skipIfNoServiceCIDRAPI marks the spec as skipped on clusters where the
// networking.k8s.io/v1 ServiceCIDR API is unavailable (K8s <1.31 or 1.31/1.32
// with the MultiCIDRServiceAllocator feature gate disabled). The kube-ovn
// fallback path is still exercised by every other test on those clusters.
func skipIfNoServiceCIDRAPI(cs clientset.Interface) {
	ginkgo.GinkgoHelper()

	list, err := cs.Discovery().ServerResourcesForGroupVersion(networkingv1.SchemeGroupVersion.String())
	if err != nil {
		if k8serrors.IsNotFound(err) {
			ginkgo.Skip("networking.k8s.io/v1 ServiceCIDR API is not present in this cluster")
		}
		framework.ExpectNoError(err)
	}
	for _, r := range list.APIResources {
		if r.Kind == "ServiceCIDR" {
			return
		}
	}
	ginkgo.Skip("networking.k8s.io/v1 ServiceCIDR API is not present in this cluster")
}

// expectIPSetContains polls the ovs-ovn pod on the given node and verifies
// the membership of cidr in ovn40services. The 3-second daemon reconcile loop
// means the assertion needs a generous window; 30s comfortably covers it.
func expectIPSetContains(f *framework.Framework, node corev1.Node, cidr string, want bool) {
	ginkgo.GinkgoHelper()

	dsClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
	ds := dsClient.Get(framework.DaemonSetOvsOvn)
	pod, err := dsClient.GetPodOnNode(ds, node.Name)
	framework.ExpectNoError(err)

	cmd := "ipset list ovn40services | sed -n '/^Members:/,$p' | tail -n +2"
	framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
		out, err := e2epodoutput.RunHostCmd(pod.Namespace, pod.Name, cmd)
		if err != nil {
			return false, nil
		}
		members := strings.Fields(out)
		has := slices.Contains(members, cidr)
		return has == want, nil
	}, fmt.Sprintf("ipset on node %s contains %s == %v", node.Name, cidr, want))
}
