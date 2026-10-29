package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	networkinglister "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestEnqueueUpdateNamespaceWhenLabelsStillMatchNetworkPolicy(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "client",
			ResourceVersion: "1",
			Labels:          map[string]string{"tenant": "old"},
			Annotations:     map[string]string{util.LogicalSwitchAnnotation: util.DefaultSubnet},
		},
	}
	fake, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Namespaces: []*corev1.Namespace{ns}})
	require.NoError(t, err)
	controller := fake.fakeController
	controller.config.EnableNP = true
	controller.updateNpQueue = newTypedRateLimitingQueue[string]("UpdateNetworkPolicy", nil)
	t.Cleanup(controller.updateNpQueue.ShutDown)
	controller.addNamespaceQueue = newTypedRateLimitingQueue[string]("AddNamespace", nil)
	t.Cleanup(controller.addNamespaceQueue.ShutDown)

	npIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, npIndexer.Add(&networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "allow-client", Namespace: metav1.NamespaceDefault},
		Spec: networkingv1.NetworkPolicySpec{Ingress: []networkingv1.NetworkPolicyIngressRule{
			{From: []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key: "tenant", Operator: metav1.LabelSelectorOpExists,
				}}},
			}}},
			{From: []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "old"}},
			}}},
		}},
	}))
	for _, tenant := range []string{"old", "new"} {
		require.NoError(t, npIndexer.Add(&networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "allow-" + tenant, Namespace: metav1.NamespaceDefault},
			Spec: networkingv1.NetworkPolicySpec{Ingress: []networkingv1.NetworkPolicyIngressRule{{From: []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"tenant": tenant}},
			}}}}},
		}))
	}
	controller.npsLister = networkinglister.NewNetworkPolicyLister(npIndexer)
	require.NoError(t, controller.rebuildPolicyEventIndex())
	require.True(t, controller.policyEventIndex.isReady())

	oldNs := ns.DeepCopy()
	newNs := ns.DeepCopy()
	newNs.ResourceVersion = "2"
	newNs.Labels["tenant"] = "new"

	controller.enqueueUpdateNamespace(oldNs, newNs)

	require.Equal(t, 3, controller.updateNpQueue.Len())
	var policies []string
	for range 3 {
		item, shutdown := controller.updateNpQueue.Get()
		require.False(t, shutdown)
		controller.updateNpQueue.Done(item)
		policies = append(policies, item)
	}
	require.ElementsMatch(t, []string{"default/allow-client", "default/allow-old", "default/allow-new"}, policies)
}
