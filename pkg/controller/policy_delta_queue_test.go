package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
	queue := newCoalescingDeltaQueue("test-admin-policy-dns-delta", func(delta *AdminNetworkPolicyChangedDelta) string {
		return delta.key
	}, mergeAdminNetworkPolicyDelta)
	t.Cleanup(queue.ShutDown)

	queue.Add(&AdminNetworkPolicyChangedDelta{key: "policy", field: ChangedEgressRule, DNSReconcileDone: true})
	queue.Add(&AdminNetworkPolicyChangedDelta{key: "policy", field: ChangedEgressRule})

	item, shutdown := queue.Get()
	require.False(t, shutdown)
	require.False(t, item.DNSReconcileDone)
	queue.Add(&AdminNetworkPolicyChangedDelta{key: "policy", field: ChangedSubject})
	queue.Forget(item)
	queue.Done(item)

	require.Eventually(t, func() bool { return queue.Len() == 1 }, time.Second, time.Millisecond)
	next, shutdown := queue.Get()
	require.False(t, shutdown)
	require.Equal(t, changedFieldSubject, next.fieldsMask())
	queue.Forget(next)
	queue.Done(next)
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

	queue.Add(&AdminNetworkPolicyChangedDelta{key: "policy", field: ChangedEgressRule,
		ruleNames: [util.AnpMaxRules]ChangedName{{curRuleName: "egress-1"}}})
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
