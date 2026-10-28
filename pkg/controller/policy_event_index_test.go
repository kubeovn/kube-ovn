package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"

	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"
)

func TestNetworkPolicyEventIndexTracksPolicyUpdatesAndDeletion(t *testing.T) {
	c := &Controller{
		updateNpQueue: newTypedRateLimitingQueue[string]("UpdateNetworkPolicy", nil),
		deleteNpQueue: newTypedRateLimitingQueue[networkPolicyDeleteRequest]("DeleteNetworkPolicy", nil),
	}
	t.Cleanup(c.updateNpQueue.ShutDown)
	t.Cleanup(c.deleteNpQueue.ShutDown)
	np := &netv1.NetworkPolicy{
		Name: "selected", Namespace: "policy",
		Spec: netv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "old"}},
			Ingress: []netv1.NetworkPolicyIngressRule{{From: []netv1.NetworkPolicyPeer{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "old"}},
			}}}},
		},
	}
	c.enqueueAddNp(np)
	require.Equal(t, []string{"policy/selected"}, c.policyEventIndex.candidatesForPod(nil, map[string]string{"app": "old"}))
	require.Equal(t, []string{"policy/selected"}, c.policyEventIndex.candidatesForNamespace(map[string]string{"tenant": "old"}))

	updated := np.DeepCopy()
	updated.Spec.PodSelector.MatchLabels["app"] = "new"
	updated.Spec.Ingress[0].From[0].NamespaceSelector.MatchLabels["tenant"] = "new"
	c.enqueueUpdateNp(np, updated)
	require.Empty(t, c.policyEventIndex.candidatesForPod(nil, map[string]string{"app": "old"}))
	require.Empty(t, c.policyEventIndex.candidatesForNamespace(map[string]string{"tenant": "old"}))
	require.Equal(t, []string{"policy/selected"}, c.policyEventIndex.candidatesForPod(nil, map[string]string{"app": "new"}))
	require.Equal(t, []string{"policy/selected"}, c.policyEventIndex.candidatesForNamespace(map[string]string{"tenant": "new"}))

	c.enqueueDeleteNp(cache.DeletedFinalStateUnknown{Key: "policy/selected", Obj: updated})
	require.Empty(t, c.policyEventIndex.candidatesForPod(nil, map[string]string{"app": "new"}))
	require.Empty(t, c.policyEventIndex.candidatesForNamespace(map[string]string{"tenant": "new"}))
}

func TestPolicyEventIndexSelectorCandidates(t *testing.T) {
	index := policyEventIndex{}
	matchLabels := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}
	exists := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
		Key: "tier", Operator: metav1.LabelSelectorOpExists,
	}}}
	complexSelector := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
		Key: "track", Operator: metav1.LabelSelectorOpIn, Values: []string{"stable"},
	}}}

	index.upsert(policyEventRef{kind: policyEventNetworkPolicy, key: "ns/web"}, []policyEventSelector{
		{namespace: matchLabels, pod: matchLabels},
	})
	index.upsert(policyEventRef{kind: policyEventNetworkPolicy, key: "ns/exists"}, []policyEventSelector{{pod: exists}})
	index.upsert(policyEventRef{kind: policyEventNetworkPolicy, key: "ns/complex"}, []policyEventSelector{{pod: complexSelector}})

	got := index.candidatesForPod(
		map[string]string{"app": "web"},
		map[string]string{"app": "web", "tier": "frontend"},
	)
	want := []string{"ns/complex", "ns/exists", "ns/web"}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidates[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	got = index.candidatesForPod(map[string]string{"app": "api"}, map[string]string{"app": "api"})
	if len(got) != 1 || got[0] != "ns/complex" {
		t.Fatalf("nonmatching candidates = %v, want only complex fallback", got)
	}

	index.remove(policyEventRef{kind: policyEventNetworkPolicy, key: "ns/complex"})
	got = index.candidatesForPod(nil, map[string]string{"app": "api"})
	if len(got) != 0 {
		t.Fatalf("candidates after removal = %v, want none", got)
	}
}

func TestPolicyEventIndexLabelChangeUsesOldAndNewValues(t *testing.T) {
	index := policyEventIndex{}
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}}
	index.upsert(policyEventRef{kind: policyEventAdminNetworkPolicy, key: "anp"}, []policyEventSelector{{pod: selector}})

	change := labelChange{
		oldPodLabels: map[string]string{"app": "web"},
		newPodLabels: map[string]string{"app": "api"},
	}
	got := index.candidatesForChange(policyEventAdminNetworkPolicy, change)
	if len(got) != 1 || got[0] != "anp" {
		t.Fatalf("candidates = %v, want [anp]", got)
	}

	change.newPodLabels = labels.Set{"other": "value"}
	got = index.candidatesForChange(policyEventAdminNetworkPolicy, change)
	if len(got) != 0 {
		t.Fatalf("candidates after removing old/new selector values = %v, want none", got)
	}
}

func TestPolicyEventIndexNamespaceScopedPodCandidates(t *testing.T) {
	index := policyEventIndex{}
	index.upsert(policyEventRef{kind: policyEventNetworkPolicy, key: "policy/allow-all"}, []policyEventSelector{{podNamespaceAll: "policy"}})

	got := index.candidatesForPodInNamespace("policy", nil, nil)
	if len(got) != 1 || got[0] != "policy/allow-all" {
		t.Fatalf("candidates in policy namespace = %v, want [policy/allow-all]", got)
	}
	got = index.candidatesForPodInNamespace("other", nil, nil)
	if len(got) != 0 {
		t.Fatalf("candidates in other namespace = %v, want none", got)
	}
}

func TestIndexNetworkPolicyEmptyPeerUsesPolicyNamespace(t *testing.T) {
	controller := &Controller{}
	controller.indexNetworkPolicy(&netv1.NetworkPolicy{
		Name: "allow-all", Namespace: "policy",
		Spec: netv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "server"}},
			Ingress:     []netv1.NetworkPolicyIngressRule{{From: []netv1.NetworkPolicyPeer{{}}}},
		},
	})

	got := controller.policyEventIndex.candidatesForPodInNamespace("policy", nil, map[string]string{"app": "client"})
	if len(got) != 1 || got[0] != "policy/allow-all" {
		t.Fatalf("empty peer candidates in policy namespace = %v, want [policy/allow-all]", got)
	}
	got = controller.policyEventIndex.candidatesForPodInNamespace("other", nil, map[string]string{"app": "client"})
	if len(got) != 0 {
		t.Fatalf("empty peer candidates in other namespace = %v, want none", got)
	}
}

func TestPolicyEventIndexBuildsSelectorsForNetworkPolicyKinds(t *testing.T) {
	controller := &Controller{}
	npSelector := metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}
	nsSelector := metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "blue"}}
	controller.indexNetworkPolicy(&netv1.NetworkPolicy{
		Name: "web", Namespace: "policy",
		Spec: netv1.NetworkPolicySpec{
			PodSelector: npSelector,
			Ingress: []netv1.NetworkPolicyIngressRule{{From: []netv1.NetworkPolicyPeer{
				{PodSelector: &npSelector, NamespaceSelector: &nsSelector},
			}}},
		},
	})
	controller.indexAdminNetworkPolicy(&v1alpha1.AdminNetworkPolicy{
		Name: "admin",
		Spec: v1alpha1.AdminNetworkPolicySpec{
			Subject: v1alpha1.AdminNetworkPolicySubject{Namespaces: &nsSelector},
		},
	})

	got := controller.policyEventIndex.candidatesForPod(
		map[string]string{"tenant": "blue"},
		map[string]string{"app": "web"},
	)
	if len(got) != 1 || got[0] != "policy/web" {
		t.Fatalf("network policy candidates = %v, want [policy/web]", got)
	}
	got = controller.policyEventIndex.candidatesForChange(policyEventAdminNetworkPolicy, labelChange{
		oldNsLabels: map[string]string{"tenant": "red"},
		newNsLabels: map[string]string{"tenant": "blue"},
	})
	if len(got) != 1 || got[0] != "admin" {
		t.Fatalf("admin policy candidates = %v, want [admin]", got)
	}
}
