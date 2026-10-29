package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"
	v1alpha2 "sigs.k8s.io/network-policy-api/apis/v1alpha2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestCoalescingAdminNetworkPolicyDeltaQueue(t *testing.T) {
	queue := newCoalescingDeltaQueue("test-admin-policy-delta", func(delta *AdminNetworkPolicyChangedDelta) string {
		return delta.key
	}, mergeAdminNetworkPolicyDelta)
	t.Cleanup(queue.ShutDown)

	queue.Add(&AdminNetworkPolicyChangedDelta{key: "policy", field: ChangedSubject})
	queue.Add(&AdminNetworkPolicyChangedDelta{
		key:       "policy",
		field:     ChangedIngressRule,
		ruleNames: [util.AnpMaxRules]ChangedName{{curRuleName: "ingress-1"}},
	})
	queue.Add(&AdminNetworkPolicyChangedDelta{
		key:       "policy",
		field:     ChangedEgressRule,
		ruleNames: [util.AnpMaxRules]ChangedName{{curRuleName: "egress-1"}},
	})
	queue.Add(&AdminNetworkPolicyChangedDelta{
		key:       "policy",
		field:     ChangedIngressRule,
		ruleNames: [util.AnpMaxRules]ChangedName{{isMatch: true}},
	})

	require.Equal(t, 1, queue.Len())
	item, shutdown := queue.Get()
	require.False(t, shutdown)
	require.Equal(t, changedFieldSubject|changedFieldIngressRule|changedFieldEgressRule, item.fieldsMask())
	require.Equal(t, "ingress-1", item.ruleNamesFor(ChangedIngressRule)[0].curRuleName)
	require.True(t, item.ruleNamesFor(ChangedIngressRule)[0].isMatch)
	require.Equal(t, "egress-1", item.ruleNamesFor(ChangedEgressRule)[0].curRuleName)
	queue.Forget(item)
	queue.Done(item)
}

func TestEnqueueUpdateAnpCoalescesFieldsByPolicy(t *testing.T) {
	controller := prepareAnpQueueTestController(t)
	controller.updateAnpQueue = newCoalescingDeltaQueue("test-enqueue-admin-policy-delta", func(delta *AdminNetworkPolicyChangedDelta) string {
		return delta.key
	}, mergeAdminNetworkPolicyDelta)
	t.Cleanup(controller.updateAnpQueue.ShutDown)

	oldAnp := newTestAnp("ingress-rule", "egress-rule", map[string]string{"app": "old"})
	newAnp := oldAnp.DeepCopy()
	newAnp.Spec.Subject.Namespaces.MatchLabels = map[string]string{"tenant": "new"}
	newAnp.Spec.Ingress[0].From[0].Namespaces.MatchLabels = map[string]string{"app": "new"}
	newAnp.Spec.Egress[0].To[0].Namespaces.MatchLabels = map[string]string{"app": "new"}

	controller.enqueueUpdateAnp(oldAnp, newAnp)
	require.Equal(t, 1, controller.updateAnpQueue.Len())
	item, shutdown := controller.updateAnpQueue.Get()
	require.False(t, shutdown)
	require.Equal(t, changedFieldSubject|changedFieldIngressRule|changedFieldEgressRule, item.fieldsMask())
	controller.updateAnpQueue.Forget(item)
	controller.updateAnpQueue.Done(item)
}

func TestCoalescingAdminNetworkPolicyDeltaQueuePreservesDNSRetrySemantics(t *testing.T) {
	for _, dnsFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "dns-first", false: "policy-first"}[dnsFirst], func(t *testing.T) {
			queue := newCoalescingDeltaQueue("test-admin-policy-dns-delta", func(delta *AdminNetworkPolicyChangedDelta) string {
				return delta.key
			}, mergeAdminNetworkPolicyDelta)
			t.Cleanup(queue.ShutDown)

			dns := &AdminNetworkPolicyChangedDelta{key: "policy", field: ChangedEgressRule, DNSReconcileDone: true}
			policy := &AdminNetworkPolicyChangedDelta{
				key: "policy", field: ChangedEgressRule,
				ruleNames: [util.AnpMaxRules]ChangedName{{curRuleName: "egress-1"}},
			}
			if dnsFirst {
				queue.Add(dns)
				queue.Add(policy)
			} else {
				queue.Add(policy)
				queue.Add(dns)
			}

			item, shutdown := queue.Get()
			require.False(t, shutdown)
			require.True(t, item.DNSReconcileDone)
			require.True(t, item.DNSReconcileNeeded)
			require.Equal(t, "egress-1", item.ruleNamesFor(ChangedEgressRule)[0].curRuleName)
			queue.Add(&AdminNetworkPolicyChangedDelta{key: "policy", field: ChangedSubject})
			queue.Forget(item)
			queue.Done(item)

			require.Eventually(t, func() bool { return queue.Len() == 1 }, time.Second, time.Millisecond)
			next, shutdown := queue.Get()
			require.False(t, shutdown)
			require.Equal(t, changedFieldSubject, next.fieldsMask())
			queue.Forget(next)
			queue.Done(next)
		})
	}
}

func TestCoalescingClusterNetworkPolicyDeltaPreservesDNSAndPolicyUpdates(t *testing.T) {
	queue := newCoalescingDeltaQueue("test-cluster-policy-dns-delta", func(delta *ClusterNetworkPolicyChangedDelta) string {
		return delta.key
	}, mergeClusterNetworkPolicyDelta)
	t.Cleanup(queue.ShutDown)

	queue.Add(&ClusterNetworkPolicyChangedDelta{key: "policy", field: ChangedEgressRule})
	queue.Add(&ClusterNetworkPolicyChangedDelta{key: "policy", field: ChangedEgressRule, DNSReconcileDone: true})
	item, shutdown := queue.Get()
	require.False(t, shutdown)
	require.True(t, item.DNSReconcileDone)
	require.True(t, item.DNSReconcileNeeded)
	queue.Forget(item)
	queue.Done(item)
}

func TestCoalescingDeltaQueueSkipsExpiredRetryAfterNewEvent(t *testing.T) {
	queue := newCoalescingDeltaQueue("test-policy-delta-expired-retry", func(delta *AdminNetworkPolicyChangedDelta) string {
		return delta.key
	}, mergeAdminNetworkPolicyDelta)
	t.Cleanup(queue.ShutDown)

	queue.Add(&AdminNetworkPolicyChangedDelta{key: "policy", field: ChangedIngressRule})
	first, shutdown := queue.Get()
	require.False(t, shutdown)
	queue.Add(&AdminNetworkPolicyChangedDelta{key: "policy", field: ChangedSubject})
	queue.AddRateLimited(first)
	queue.Done(first)

	next, shutdown := queue.Get()
	require.False(t, shutdown)
	require.Equal(t, changedFieldIngressRule|changedFieldSubject, next.fieldsMask())
	queue.Forget(next)
	queue.Done(next)

	require.Eventually(t, func() bool { return queue.Len() == 1 }, time.Second, time.Millisecond)
	queue.Add(&AdminNetworkPolicyChangedDelta{key: "fresh", field: ChangedEgressRule})
	fresh, shutdown := queue.Get()
	require.False(t, shutdown)
	require.Equal(t, "fresh", fresh.key)
	require.Equal(t, changedFieldEgressRule, fresh.fieldsMask())
	queue.Forget(fresh)
	queue.Done(fresh)
}

func TestUnnamedPolicyRuleChangesKeepTheirIndex(t *testing.T) {
	change := ChangedName{isMatch: true}
	require.True(t, changedNameMatches(change, ""))
	require.False(t, changedNameMatches(ChangedName{}, ""))
	require.True(t, changedNameMatches(ChangedName{curRuleName: "named"}, "named"))

	controller := prepareAnpQueueTestController(t)
	controller.updateAnpQueue = newCoalescingDeltaQueue("test-unnamed-anp", func(delta *AdminNetworkPolicyChangedDelta) string {
		return delta.key
	}, mergeAdminNetworkPolicyDelta)
	t.Cleanup(controller.updateAnpQueue.ShutDown)
	oldAnp := newTestAnp("", "", map[string]string{"app": "old"})
	oldAnp.Spec.Ingress = append(oldAnp.Spec.Ingress, oldAnp.Spec.Ingress[0])
	oldAnp.Spec.Egress = append(oldAnp.Spec.Egress, oldAnp.Spec.Egress[0])
	newAnp := oldAnp.DeepCopy()
	newAnp.Spec.Ingress[0].From[0].Namespaces.MatchLabels = map[string]string{"app": "new"}
	newAnp.Spec.Egress[0].To[0].Namespaces.MatchLabels = map[string]string{"app": "new"}
	controller.enqueueUpdateAnp(oldAnp, newAnp)
	require.Equal(t, 1, controller.updateAnpQueue.Len())
	item, shutdown := controller.updateAnpQueue.Get()
	require.False(t, shutdown)
	require.Equal(t, changedFieldIngressRule|changedFieldEgressRule, item.fieldsMask())
	for _, field := range []ChangedField{ChangedIngressRule, ChangedEgressRule} {
		names := item.ruleNamesFor(field)
		require.True(t, names[0].isMatch)
		require.False(t, changedNameMatches(names[1], ""))
	}
	controller.updateAnpQueue.Done(item)

	oldCnp := &v1alpha2.ClusterNetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "cnp"}, Spec: v1alpha2.ClusterNetworkPolicySpec{
		Ingress: []v1alpha2.ClusterNetworkPolicyIngressRule{{From: []v1alpha2.ClusterNetworkPolicyIngressPeer{{Namespaces: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "old"}}}}}},
	}}
	newCnp := oldCnp.DeepCopy()
	newCnp.Spec.Ingress[0].From[0].Namespaces.MatchLabels = map[string]string{"app": "new"}
	controller.addCnpQueue = newTypedRateLimitingQueue[string]("test-unnamed-cnp-add", nil)
	controller.updateCnpQueue = newCoalescingDeltaQueue("test-unnamed-cnp", func(delta *ClusterNetworkPolicyChangedDelta) string {
		return delta.key
	}, mergeClusterNetworkPolicyDelta)
	t.Cleanup(controller.addCnpQueue.ShutDown)
	t.Cleanup(controller.updateCnpQueue.ShutDown)
	controller.enqueueUpdateCnp(oldCnp, newCnp)
	require.Zero(t, controller.addCnpQueue.Len())
	require.Equal(t, 1, controller.updateCnpQueue.Len())
	cnpItem, shutdown := controller.updateCnpQueue.Get()
	require.False(t, shutdown)
	require.True(t, cnpItem.ruleNamesFor(ChangedIngressRule)[0].isMatch)
	controller.updateCnpQueue.Done(cnpItem)

	oldBanp := &v1alpha1.BaselineAdminNetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "default"}, Spec: v1alpha1.BaselineAdminNetworkPolicySpec{
		Ingress: []v1alpha1.BaselineAdminNetworkPolicyIngressRule{{From: []v1alpha1.AdminNetworkPolicyIngressPeer{{Namespaces: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "old"}}}}}},
	}}
	newBanp := oldBanp.DeepCopy()
	newBanp.Spec.Ingress[0].From[0].Namespaces.MatchLabels = map[string]string{"app": "new"}
	controller.updateBanpQueue = newTypedRateLimitingQueue[*AdminNetworkPolicyChangedDelta]("test-unnamed-banp", nil)
	controller.enqueueUpdateBanp(oldBanp, newBanp)
	require.Equal(t, 1, controller.updateBanpQueue.Len())
	banpItem, shutdown := controller.updateBanpQueue.Get()
	require.False(t, shutdown)
	require.True(t, banpItem.ruleNamesFor(ChangedIngressRule)[0].isMatch)
	controller.updateBanpQueue.Done(banpItem)
}

func TestCoalescingBaselineAdminNetworkPolicyDeltaQueue(t *testing.T) {
	queue := newCoalescingDeltaQueue("test-baseline-admin-policy-delta", func(delta *AdminNetworkPolicyChangedDelta) string {
		return delta.key
	}, mergeAdminNetworkPolicyDelta)
	t.Cleanup(queue.ShutDown)

	queue.Add(&AdminNetworkPolicyChangedDelta{key: "baseline", field: ChangedSubject})
	queue.Add(&AdminNetworkPolicyChangedDelta{
		key:       "baseline",
		field:     ChangedIngressRule,
		ruleNames: [util.AnpMaxRules]ChangedName{{curRuleName: "ingress-1"}},
	})
	queue.Add(&AdminNetworkPolicyChangedDelta{
		key:       "baseline",
		field:     ChangedEgressRule,
		ruleNames: [util.AnpMaxRules]ChangedName{{isMatch: true}},
	})

	item, shutdown := queue.Get()
	require.False(t, shutdown)
	require.Equal(t, changedFieldSubject|changedFieldIngressRule|changedFieldEgressRule, item.fieldsMask())
	require.Equal(t, "ingress-1", item.ruleNamesFor(ChangedIngressRule)[0].curRuleName)
	require.True(t, item.ruleNamesFor(ChangedEgressRule)[0].isMatch)
	queue.Forget(item)
	queue.Done(item)
}

func TestCoalescingClusterNetworkPolicyDeltaQueue(t *testing.T) {
	queue := newCoalescingDeltaQueue("test-cluster-network-policy-delta", func(delta *ClusterNetworkPolicyChangedDelta) string {
		return delta.key
	}, mergeClusterNetworkPolicyDelta)
	t.Cleanup(queue.ShutDown)

	queue.Add(&ClusterNetworkPolicyChangedDelta{key: "cluster", field: ChangedSubject})
	queue.Add(&ClusterNetworkPolicyChangedDelta{
		key:       "cluster",
		field:     ChangedIngressRule,
		ruleNames: [util.CnpMaxRules]ChangedName{{curRuleName: "ingress-1"}},
	})
	queue.Add(&ClusterNetworkPolicyChangedDelta{
		key:       "cluster",
		field:     ChangedEgressRule,
		ruleNames: [util.CnpMaxRules]ChangedName{{curRuleName: "egress-1"}},
	})

	item, shutdown := queue.Get()
	require.False(t, shutdown)
	require.Equal(t, changedFieldSubject|changedFieldIngressRule|changedFieldEgressRule, item.fieldsMask())
	require.Equal(t, "ingress-1", item.ruleNamesFor(ChangedIngressRule)[0].curRuleName)
	require.Equal(t, "egress-1", item.ruleNamesFor(ChangedEgressRule)[0].curRuleName)
	queue.Forget(item)
	queue.Done(item)
}

func TestCoalescingDeltaQueuePreservesEventsDuringRetry(t *testing.T) {
	queue := newCoalescingDeltaQueue("test-policy-delta-retry", func(delta *AdminNetworkPolicyChangedDelta) string {
		return delta.key
	}, mergeAdminNetworkPolicyDelta)
	t.Cleanup(queue.ShutDown)

	queue.Add(&AdminNetworkPolicyChangedDelta{
		key: "policy", field: ChangedEgressRule,
		ruleNames: [util.AnpMaxRules]ChangedName{{curRuleName: "egress-1"}},
	})
	item, shutdown := queue.Get()
	require.False(t, shutdown)

	// A new event arrives while the previous snapshot is being retried.
	queue.Add(&AdminNetworkPolicyChangedDelta{key: "policy", field: ChangedSubject})
	queue.AddRateLimited(item)
	queue.Done(item)

	require.Eventually(t, func() bool { return queue.Len() == 1 }, time.Second, time.Millisecond)
	next, shutdown := queue.Get()
	require.False(t, shutdown)
	require.Equal(t, changedFieldSubject|changedFieldEgressRule, next.fieldsMask())
	require.Equal(t, "egress-1", next.ruleNamesFor(ChangedEgressRule)[0].curRuleName)
	queue.Forget(next)
	queue.Done(next)
}

func TestPolicyLabelEventsCoalesceThroughAllPolicyHandlers(t *testing.T) {
	controller := newPolicyLabelTestController(t, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: metav1.NamespaceDefault}})
	controller.updateAnpQueue = newCoalescingDeltaQueue("UpdateAdminNetworkPolicy", func(delta *AdminNetworkPolicyChangedDelta) string {
		return delta.key
	}, mergeAdminNetworkPolicyDelta)
	controller.updateBanpQueue = newCoalescingDeltaQueue("UpdateBaseAdminNetworkPolicy", func(delta *AdminNetworkPolicyChangedDelta) string {
		return delta.key
	}, mergeAdminNetworkPolicyDelta)
	controller.updateCnpQueue = newCoalescingDeltaQueue("UpdateClusterNetworkPolicy", func(delta *ClusterNetworkPolicyChangedDelta) string {
		return delta.key
	}, mergeClusterNetworkPolicyDelta)
	t.Cleanup(controller.updateAnpQueue.ShutDown)
	t.Cleanup(controller.updateBanpQueue.ShutDown)
	t.Cleanup(controller.updateCnpQueue.ShutDown)
	addPolicyLabelTestFixtures(t, controller, false)

	oldPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "selected", Namespace: metav1.NamespaceDefault, ResourceVersion: "1",
			Labels:      map[string]string{"app": "old"},
			Annotations: map[string]string{util.LogicalSwitchAnnotation: util.DefaultSubnet},
		},
	}
	newPod := oldPod.DeepCopy()
	newPod.ResourceVersion = "2"
	newPod.Labels = map[string]string{"app": "new"}
	controller.enqueueUpdatePod(oldPod, newPod)

	assertCoalescedPolicyFields(t, controller.updateAnpQueue, []string{"old", "new", "both"})
	assertCoalescedPolicyFields(t, controller.updateBanpQueue, []string{"old", "new", "both"})
	assertCoalescedCnpFields(t, controller.updateCnpQueue, []string{"old", "new", "both"})
}

func assertCoalescedPolicyFields(t *testing.T, queue workqueue.TypedRateLimitingInterface[*AdminNetworkPolicyChangedDelta], keys []string) {
	t.Helper()
	require.Equal(t, len(keys), queue.Len())
	for queue.Len() > 0 {
		item, shutdown := queue.Get()
		require.False(t, shutdown)
		require.Contains(t, keys, item.key)
		require.Equal(t, changedFieldSubject|changedFieldIngressRule|changedFieldEgressRule, item.fieldsMask())
		queue.Done(item)
	}
}

func assertCoalescedCnpFields(t *testing.T, queue workqueue.TypedRateLimitingInterface[*ClusterNetworkPolicyChangedDelta], keys []string) {
	t.Helper()
	require.Equal(t, len(keys), queue.Len())
	for queue.Len() > 0 {
		item, shutdown := queue.Get()
		require.False(t, shutdown)
		require.Contains(t, keys, item.key)
		require.Equal(t, changedFieldSubject|changedFieldIngressRule|changedFieldEgressRule, item.fieldsMask())
		queue.Done(item)
	}
}
