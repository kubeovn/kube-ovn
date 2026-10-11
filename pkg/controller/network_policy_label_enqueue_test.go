package controller

import (
	"fmt"
	"testing"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"
	v1alpha2 "sigs.k8s.io/network-policy-api/apis/v1alpha2"
	anplister "sigs.k8s.io/network-policy-api/pkg/client/listers/apis/v1alpha1"
	cnplister "sigs.k8s.io/network-policy-api/pkg/client/listers/apis/v1alpha2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestEnqueueUpdatePodRefreshesAllAdminPolicyKindsForOldAndNewLabels(t *testing.T) {
	for _, oldLabels := range []map[string]string{{"app": "old"}, nil} {
		t.Run(fmt.Sprintf("old-labels-%v", oldLabels), func(t *testing.T) {
			ns := &corev1.Namespace{Name: metav1.NamespaceDefault}
			controller := newPolicyLabelTestController(t, ns)
			addPolicyLabelTestFixtures(t, controller, false)
			oldPod := &corev1.Pod{
				Name: "selected", Namespace: ns.Name, ResourceVersion: "1", Labels: oldLabels,
				Annotations: map[string]string{util.LogicalSwitchAnnotation: util.DefaultSubnet},
			}
			newPod := oldPod.DeepCopy()
			newPod.ResourceVersion = "2"
			newPod.Labels = map[string]string{"app": "new"}
			controller.enqueueUpdatePod(oldPod, newPod)
			want := []string{"old", "new", "both"}
			if oldLabels == nil {
				want = []string{"new", "both"}
			}
			assertPolicyLabelDeltas(t, controller, want, oldLabels != nil)
		})
	}
}

func TestEnqueueUpdateNamespaceRefreshesAllAdminPolicyKindsForOldAndNewLabels(t *testing.T) {
	ns := &corev1.Namespace{
		Name: metav1.NamespaceDefault, ResourceVersion: "1", Labels: map[string]string{"tenant": "old"},
		Annotations: map[string]string{util.LogicalSwitchAnnotation: util.DefaultSubnet},
	}
	controller := newPolicyLabelTestController(t, ns)
	addPolicyLabelTestFixtures(t, controller, true)
	newNs := ns.DeepCopy()
	newNs.ResourceVersion = "2"
	newNs.Labels = map[string]string{"tenant": "new"}
	controller.enqueueUpdateNamespace(ns, newNs)
	assertPolicyLabelDeltas(t, controller, []string{"old", "new", "both"}, true)
}

func TestUnlabeledPodEventsDoNotMatchNonemptyAdminPolicyPodSelectors(t *testing.T) {
	for _, event := range []string{"routed", "delete"} {
		t.Run(event, func(t *testing.T) {
			controller := newPolicyLabelTestController(t, &corev1.Namespace{Name: metav1.NamespaceDefault})
			addPolicyLabelTestFixtures(t, controller, false)
			pod := &corev1.Pod{
				Name: "unlabeled", Namespace: metav1.NamespaceDefault, ResourceVersion: "1",
				Annotations: map[string]string{util.LogicalSwitchAnnotation: util.DefaultSubnet},
			}
			if event == "delete" {
				controller.enqueueDeletePod(pod)
			} else {
				newPod := pod.DeepCopy()
				newPod.ResourceVersion = "2"
				newPod.Annotations[fmt.Sprintf(util.RoutedAnnotationTemplate, util.OvnProvider)] = "true"
				controller.enqueueUpdatePod(pod, newPod)
			}
			assertPolicyLabelDeltas(t, controller, nil, false)
		})
	}
}

func newPolicyLabelTestController(t *testing.T, ns *corev1.Namespace) *Controller {
	t.Helper()
	fake, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Namespaces: []*corev1.Namespace{ns}, Subnets: []*kubeovnv1.Subnet{{Name: util.DefaultSubnet}},
	})
	require.NoError(t, err)
	c := fake.fakeController
	c.config.EnableANP = true
	c.deletingPodObjMap = xsync.NewMap[string, *corev1.Pod]()
	c.addOrUpdatePodQueue = newTypedRateLimitingQueue[string]("AddOrUpdatePod", nil)
	c.deletePodQueue = newTypedRateLimitingQueue[string]("DeletePod", nil)
	c.addNamespaceQueue = newTypedRateLimitingQueue[string]("AddNamespace", nil)
	c.updateAnpQueue = newTypedRateLimitingQueue[*AdminNetworkPolicyChangedDelta]("UpdateAdminNetworkPolicy", nil)
	c.updateBanpQueue = newTypedRateLimitingQueue[*AdminNetworkPolicyChangedDelta]("UpdateBaseAdminNetworkPolicy", nil)
	c.updateCnpQueue = newTypedRateLimitingQueue[*ClusterNetworkPolicyChangedDelta]("UpdateClusterNetworkPolicy", nil)
	t.Cleanup(c.addOrUpdatePodQueue.ShutDown)
	t.Cleanup(c.deletePodQueue.ShutDown)
	t.Cleanup(c.addNamespaceQueue.ShutDown)
	t.Cleanup(c.updateAnpQueue.ShutDown)
	t.Cleanup(c.updateBanpQueue.ShutDown)
	t.Cleanup(c.updateCnpQueue.ShutDown)
	return c
}

func addPolicyLabelTestFixtures(t *testing.T, c *Controller, namespaceEvent bool) {
	t.Helper()
	anpIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	banpIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	cnpIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, name := range []string{"old", "new", "both", "unrelated"} {
		selector := metav1.LabelSelector{MatchLabels: map[string]string{"app": name}}
		if namespaceEvent {
			selector = metav1.LabelSelector{MatchLabels: map[string]string{"tenant": name}}
		}
		if name == "both" {
			key := "app"
			if namespaceEvent {
				key = "tenant"
			}
			selector = metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key: key, Operator: metav1.LabelSelectorOpIn, Values: []string{"old", "new"},
			}}}
		}
		nsSelector, podSelector := metav1.LabelSelector{}, selector
		if namespaceEvent {
			nsSelector, podSelector = selector, metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}
		}
		anp := &v1alpha1.AdminNetworkPolicy{Name: name}
		banp := &v1alpha1.BaselineAdminNetworkPolicy{Name: name}
		cnp := &v1alpha2.ClusterNetworkPolicy{Name: name}
		selectors := []metav1.LabelSelector{selector}
		if name == "both" {
			key := "app"
			if namespaceEvent {
				key = "tenant"
			}
			for _, value := range []string{"old", "new"} {
				selectors = append(selectors, metav1.LabelSelector{MatchLabels: map[string]string{key: value}})
			}
		}
		for index, peerSelector := range selectors {
			if namespaceEvent {
				nsSelector = peerSelector
			} else {
				podSelector = peerSelector
			}
			podsV1 := &v1alpha1.NamespacedPod{NamespaceSelector: nsSelector, PodSelector: podSelector}
			podsV2 := &v1alpha2.NamespacedPod{NamespaceSelector: nsSelector, PodSelector: podSelector}
			if index == 0 {
				anp.Spec.Subject.Pods = podsV1
				banp.Spec.Subject.Pods = podsV1
				cnp.Spec.Subject.Pods = podsV2
			}
			ingressName, egressName := fmt.Sprintf("ingress-%d", index), fmt.Sprintf("egress-%d", index)
			anp.Spec.Ingress = append(anp.Spec.Ingress, v1alpha1.AdminNetworkPolicyIngressRule{Name: ingressName, From: []v1alpha1.AdminNetworkPolicyIngressPeer{{Pods: podsV1}}})
			anp.Spec.Egress = append(anp.Spec.Egress, v1alpha1.AdminNetworkPolicyEgressRule{Name: egressName, To: []v1alpha1.AdminNetworkPolicyEgressPeer{{Pods: podsV1}}})
			banp.Spec.Ingress = append(banp.Spec.Ingress, v1alpha1.BaselineAdminNetworkPolicyIngressRule{Name: ingressName, From: []v1alpha1.AdminNetworkPolicyIngressPeer{{Pods: podsV1}}})
			banp.Spec.Egress = append(banp.Spec.Egress, v1alpha1.BaselineAdminNetworkPolicyEgressRule{Name: egressName, To: []v1alpha1.BaselineAdminNetworkPolicyEgressPeer{{Pods: podsV1}}})
			cnp.Spec.Ingress = append(cnp.Spec.Ingress, v1alpha2.ClusterNetworkPolicyIngressRule{Name: ingressName, From: []v1alpha2.ClusterNetworkPolicyIngressPeer{{Pods: podsV2}}})
			cnp.Spec.Egress = append(cnp.Spec.Egress, v1alpha2.ClusterNetworkPolicyEgressRule{Name: egressName, To: []v1alpha2.ClusterNetworkPolicyEgressPeer{{Pods: podsV2}}})
		}
		require.NoError(t, anpIndexer.Add(anp))
		require.NoError(t, banpIndexer.Add(banp))
		require.NoError(t, cnpIndexer.Add(cnp))
	}
	c.anpsLister = anplister.NewAdminNetworkPolicyLister(anpIndexer)
	c.banpsLister = anplister.NewBaselineAdminNetworkPolicyLister(banpIndexer)
	c.cnpsLister = cnplister.NewClusterNetworkPolicyLister(cnpIndexer)
}

func assertPolicyLabelDeltas(t *testing.T, c *Controller, policyNames []string, oldMatches bool) {
	t.Helper()
	want := make(map[string]map[ChangedField][]string, len(policyNames))
	for _, name := range policyNames {
		want[name] = map[ChangedField][]string{ChangedSubject: nil, ChangedIngressRule: {"ingress-0"}, ChangedEgressRule: {"egress-0"}}
		if name == "both" {
			if oldMatches {
				want[name][ChangedIngressRule] = append(want[name][ChangedIngressRule], "ingress-1")
				want[name][ChangedEgressRule] = append(want[name][ChangedEgressRule], "egress-1")
			}
			want[name][ChangedIngressRule] = append(want[name][ChangedIngressRule], "ingress-2")
			want[name][ChangedEgressRule] = append(want[name][ChangedEgressRule], "egress-2")
		}
	}
	t.Run("ANP", func(t *testing.T) { assertAdminPolicyFields(t, c.updateAnpQueue, want) })
	t.Run("BANP", func(t *testing.T) { assertAdminPolicyFields(t, c.updateBanpQueue, want) })
	t.Run("CNP", func(t *testing.T) { assertCnpFields(t, c.updateCnpQueue, want) })
}

func TestLabelMatchersTreatNamespaceEventsWithoutPodLabelsAsNamespaceMatches(t *testing.T) {
	nsSelector := &metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "team-a"}}
	podSelectorV1 := &v1alpha1.NamespacedPod{
		NamespaceSelector: *nsSelector,
		PodSelector:       metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
	}
	podSelectorV2 := &v1alpha2.NamespacedPod{
		NamespaceSelector: *nsSelector,
		PodSelector:       metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
	}
	nsLabels := map[string]string{"tenant": "team-a"}
	require.True(t, isLabelsMatch(nil, podSelectorV1, nsLabels, nil))
	require.True(t, doCnpLabelsMatch(nil, podSelectorV2, nsLabels, nil))
}

func TestBaselineAdminPolicyLabelMatcherIncludesIngressPeers(t *testing.T) {
	ingress := []v1alpha1.BaselineAdminNetworkPolicyIngressRule{{
		Name: "ingress",
		From: []v1alpha1.AdminNetworkPolicyIngressPeer{{
			Namespaces: &metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "team-a"}},
		}},
	}}
	changedIngress, changedEgress := isLabelsMatchBaselineAnpRulePeers(ingress, nil, map[string]string{"tenant": "team-a"}, nil)
	require.True(t, changedIngress[0].isMatch)
	require.Equal(t, "ingress", changedIngress[0].curRuleName)
	require.True(t, isRulesArrayEmpty(changedEgress))
}

func assertAdminPolicyFields(t *testing.T, queue workqueue.TypedRateLimitingInterface[*AdminNetworkPolicyChangedDelta], want map[string]map[ChangedField][]string) {
	t.Helper()
	require.Equal(t, 3*len(want), queue.Len())
	got := make(map[string]map[ChangedField][]string, len(want))
	for queue.Len() != 0 {
		item, shutdown := queue.Get()
		require.False(t, shutdown)
		queue.Done(item)
		if got[item.key] == nil {
			got[item.key] = make(map[ChangedField][]string)
		}
		got[item.key][item.field] = nil
		for _, rule := range item.ruleNames {
			if rule.curRuleName != "" {
				got[item.key][item.field] = append(got[item.key][item.field], rule.curRuleName)
			}
		}
	}
	require.Equal(t, want, got)
}

func assertCnpFields(t *testing.T, queue workqueue.TypedRateLimitingInterface[*ClusterNetworkPolicyChangedDelta], want map[string]map[ChangedField][]string) {
	t.Helper()
	require.Equal(t, 3*len(want), queue.Len())
	got := make(map[string]map[ChangedField][]string, len(want))
	for queue.Len() != 0 {
		item, shutdown := queue.Get()
		require.False(t, shutdown)
		queue.Done(item)
		if got[item.key] == nil {
			got[item.key] = make(map[ChangedField][]string)
		}
		got[item.key][item.field] = nil
		for _, rule := range item.ruleNames {
			if rule.curRuleName != "" {
				got[item.key][item.field] = append(got[item.key][item.field], rule.curRuleName)
			}
		}
	}
	require.Equal(t, want, got)
}
