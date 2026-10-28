package controller

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corelister "k8s.io/client-go/listers/core/v1"
	networkinglister "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"
	v1alpha2 "sigs.k8s.io/network-policy-api/apis/v1alpha2"
)

const (
	benchmarkPolicyLabelCount = 16
	benchmarkNamespace        = "benchmark"
)

// BenchmarkNetworkPolicyIndexedFanout compares the production matching paths
// with a ready index and with the full-scan fallback over the same policies.
func BenchmarkNetworkPolicyIndexedFanout(b *testing.B) {
	oldPod := &corev1.Pod{Namespace: benchmarkNamespace, Labels: map[string]string{"app": "app-0"}}
	newPod := oldPod.DeepCopy()
	newPod.Labels = map[string]string{"app": "app-1"}
	oldNamespace := &corev1.Namespace{Name: benchmarkNamespace, Labels: map[string]string{"tenant": "tenant-0"}}
	newNamespace := oldNamespace.DeepCopy()
	newNamespace.Labels = map[string]string{"tenant": "tenant-1"}

	for _, count := range []int{100, 1000} {
		for _, indexed := range []bool{false, true} {
			b.Run(fmt.Sprintf("policies-%d/indexed-%t", count, indexed), func(b *testing.B) {
				policyIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
				namespaceIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
				if err := namespaceIndexer.Add(oldNamespace); err != nil {
					b.Fatal(err)
				}
				policies := benchmarkNetworkPolicies(count)
				for i := range policies {
					policies[i].Name = fmt.Sprintf("policy-%d", i)
					if err := policyIndexer.Add(&policies[i]); err != nil {
						b.Fatal(err)
					}
				}
				c := &Controller{
					config:           &Configuration{EnableNP: true},
					npsLister:        networkinglister.NewNetworkPolicyLister(policyIndexer),
					namespacesLister: corelister.NewNamespaceLister(namespaceIndexer),
				}
				if indexed {
					if err := c.rebuildPolicyEventIndex(); err != nil {
						b.Fatal(err)
					}
				}
				b.Run("Pod", func(b *testing.B) {
					candidateCount := count * 2
					if indexed {
						candidateCount = len(c.policyEventIndexCandidatesForPod(oldPod, oldNamespace)) +
							len(c.policyEventIndexCandidatesForPod(newPod, oldNamespace))
					}
					b.ReportAllocs()
					for b.Loop() {
						c.podMatchNetworkPolicies(oldPod)
						c.podMatchNetworkPolicies(newPod)
					}
					b.ReportMetric(float64(candidateCount), "candidate-policies/op")
				})
				b.Run("Namespace", func(b *testing.B) {
					candidateCount := count * 2
					if indexed {
						candidateCount = len(c.policyEventIndex.candidatesForNamespace(oldNamespace.Labels)) +
							len(c.policyEventIndex.candidatesForNamespace(newNamespace.Labels))
					}
					b.ReportAllocs()
					for b.Loop() {
						c.namespaceMatchNetworkPolicies(oldNamespace)
						c.namespaceMatchNetworkPolicies(newNamespace)
					}
					b.ReportMetric(float64(candidateCount), "candidate-policies/op")
				})
			})
		}
	}
}

func benchmarkSelector(key, value string) metav1.LabelSelector {
	return metav1.LabelSelector{MatchLabels: map[string]string{key: value}}
}

func benchmarkNetworkPolicies(count int) []networkingv1.NetworkPolicy {
	policies := make([]networkingv1.NetworkPolicy, count)
	for i := range policies {
		podSelector := benchmarkSelector("app", fmt.Sprintf("app-%d", i%benchmarkPolicyLabelCount))
		namespaceSelector := benchmarkSelector("tenant", fmt.Sprintf("tenant-%d", i%benchmarkPolicyLabelCount))
		policies[i] = networkingv1.NetworkPolicy{
			Namespace: benchmarkNamespace,
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: podSelector,
				Ingress: []networkingv1.NetworkPolicyIngressRule{{From: []networkingv1.NetworkPolicyPeer{{
					PodSelector:       &podSelector,
					NamespaceSelector: &namespaceSelector,
				}}}},
				Egress: []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{{
					PodSelector:       &podSelector,
					NamespaceSelector: &namespaceSelector,
				}}}},
			},
		}
	}
	return policies
}

func benchmarkANPRules(count int) ([]v1alpha1.AdminNetworkPolicyIngressRule, []v1alpha1.AdminNetworkPolicyEgressRule) {
	ingress := make([]v1alpha1.AdminNetworkPolicyIngressRule, count)
	egress := make([]v1alpha1.AdminNetworkPolicyEgressRule, count)
	for i := range count {
		namespacedPod := v1alpha1.NamespacedPod{
			NamespaceSelector: benchmarkSelector("tenant", fmt.Sprintf("tenant-%d", i%benchmarkPolicyLabelCount)),
			PodSelector:       benchmarkSelector("app", fmt.Sprintf("app-%d", i%benchmarkPolicyLabelCount)),
		}
		ingress[i] = v1alpha1.AdminNetworkPolicyIngressRule{
			Name: fmt.Sprintf("ingress-%d", i),
			From: []v1alpha1.AdminNetworkPolicyIngressPeer{{Pods: &namespacedPod}},
		}
		egress[i] = v1alpha1.AdminNetworkPolicyEgressRule{
			Name: fmt.Sprintf("egress-%d", i),
			To:   []v1alpha1.AdminNetworkPolicyEgressPeer{{Pods: &namespacedPod}},
		}
	}
	return ingress, egress
}

func benchmarkBANPRules(count int) ([]v1alpha1.BaselineAdminNetworkPolicyIngressRule, []v1alpha1.BaselineAdminNetworkPolicyEgressRule) {
	ingress := make([]v1alpha1.BaselineAdminNetworkPolicyIngressRule, count)
	egress := make([]v1alpha1.BaselineAdminNetworkPolicyEgressRule, count)
	for i := range count {
		namespacedPod := v1alpha1.NamespacedPod{
			NamespaceSelector: benchmarkSelector("tenant", fmt.Sprintf("tenant-%d", i%benchmarkPolicyLabelCount)),
			PodSelector:       benchmarkSelector("app", fmt.Sprintf("app-%d", i%benchmarkPolicyLabelCount)),
		}
		ingress[i] = v1alpha1.BaselineAdminNetworkPolicyIngressRule{
			Name: fmt.Sprintf("ingress-%d", i),
			From: []v1alpha1.AdminNetworkPolicyIngressPeer{{Pods: &namespacedPod}},
		}
		egress[i] = v1alpha1.BaselineAdminNetworkPolicyEgressRule{
			Name: fmt.Sprintf("egress-%d", i),
			To:   []v1alpha1.BaselineAdminNetworkPolicyEgressPeer{{Pods: &namespacedPod}},
		}
	}
	return ingress, egress
}

func benchmarkCNPRules(count int) ([]v1alpha2.ClusterNetworkPolicyIngressRule, []v1alpha2.ClusterNetworkPolicyEgressRule) {
	ingress := make([]v1alpha2.ClusterNetworkPolicyIngressRule, count)
	egress := make([]v1alpha2.ClusterNetworkPolicyEgressRule, count)
	for i := range count {
		namespacedPod := v1alpha2.NamespacedPod{
			NamespaceSelector: benchmarkSelector("tenant", fmt.Sprintf("tenant-%d", i%benchmarkPolicyLabelCount)),
			PodSelector:       benchmarkSelector("app", fmt.Sprintf("app-%d", i%benchmarkPolicyLabelCount)),
		}
		ingress[i] = v1alpha2.ClusterNetworkPolicyIngressRule{
			Name: fmt.Sprintf("ingress-%d", i),
			From: []v1alpha2.ClusterNetworkPolicyIngressPeer{{Pods: &namespacedPod}},
		}
		egress[i] = v1alpha2.ClusterNetworkPolicyEgressRule{
			Name: fmt.Sprintf("egress-%d", i),
			To:   []v1alpha2.ClusterNetworkPolicyEgressPeer{{Pods: &namespacedPod}},
		}
	}
	return ingress, egress
}

func benchmarkPodLabelChange() labelChange {
	labels := map[string]string{"tenant": "tenant-0"}
	return labelChange{
		oldNsLabels:  labels,
		newNsLabels:  labels,
		oldPodLabels: map[string]string{"app": "app-0"},
		newPodLabels: map[string]string{"app": "app-1"},
	}
}

func benchmarkNamespaceLabelChange() labelChange {
	return labelChange{
		oldNsLabels:  map[string]string{"tenant": "tenant-0"},
		newNsLabels:  map[string]string{"tenant": "tenant-1"},
		oldPodLabels: nil,
		newPodLabels: nil,
	}
}

func BenchmarkNetworkPolicyPodEventFanout(b *testing.B) {
	oldPod := &corev1.Pod{Namespace: benchmarkNamespace, Labels: map[string]string{"app": "app-0"}}
	newPod := oldPod.DeepCopy()
	newPod.Labels = map[string]string{"app": "app-1"}
	namespace := &corev1.Namespace{Name: benchmarkNamespace, Labels: map[string]string{"tenant": "tenant-0"}}

	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprintf("policies-%d", count), func(b *testing.B) {
			policies := benchmarkNetworkPolicies(count)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				for i := range policies {
					_ = isPodMatchNetworkPolicy(oldPod, namespace, &policies[i], benchmarkNamespace)
					_ = isPodMatchNetworkPolicy(newPod, namespace, &policies[i], benchmarkNamespace)
				}
			}
		})
	}
}

func BenchmarkNetworkPolicyNamespaceEventFanout(b *testing.B) {
	oldNamespace := &corev1.Namespace{Name: benchmarkNamespace, Labels: map[string]string{"tenant": "tenant-0"}}
	newNamespace := oldNamespace.DeepCopy()
	newNamespace.Labels = map[string]string{"tenant": "tenant-1"}
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprintf("policies-%d", count), func(b *testing.B) {
			policies := benchmarkNetworkPolicies(count)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				for i := range policies {
					_ = isNamespaceMatchNetworkPolicy(oldNamespace, &policies[i])
					_ = isNamespaceMatchNetworkPolicy(newNamespace, &policies[i])
				}
			}
		})
	}
}

// BenchmarkAdminNetworkPolicyRuleFanout measures selector change matching for
// one policy containing many rules.
func BenchmarkAdminNetworkPolicyRuleFanout(b *testing.B) {
	changes := [...]struct {
		name   string
		change labelChange
	}{{"Pod", benchmarkPodLabelChange()}, {"Namespace", benchmarkNamespaceLabelChange()}}
	// ANP/BANP/CNP keep changed rule names in fixed-size arrays, so these
	// benchmarks stay within the API's per-object rule limit.
	for _, count := range []int{10, 100} {
		b.Run(fmt.Sprintf("rules-%d", count), func(b *testing.B) {
			ingress, egress := benchmarkANPRules(count)
			for _, scenario := range changes {
				b.Run(scenario.name, func(b *testing.B) {
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						_, _ = isLabelsMatchAnpRulePeersForChange(ingress, egress, scenario.change)
					}
				})
			}
		})
	}
}

// BenchmarkBaselineAdminNetworkPolicyRuleFanout measures selector change
// matching for one baseline policy containing many rules.
func BenchmarkBaselineAdminNetworkPolicyRuleFanout(b *testing.B) {
	changes := [...]struct {
		name   string
		change labelChange
	}{{"Pod", benchmarkPodLabelChange()}, {"Namespace", benchmarkNamespaceLabelChange()}}
	for _, count := range []int{10, 100} {
		b.Run(fmt.Sprintf("rules-%d", count), func(b *testing.B) {
			ingress, egress := benchmarkBANPRules(count)
			for _, scenario := range changes {
				b.Run(scenario.name, func(b *testing.B) {
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						_, _ = isLabelsMatchBanpRulePeersForChange(ingress, egress, scenario.change)
					}
				})
			}
		})
	}
}

// BenchmarkClusterNetworkPolicyRuleFanout measures selector change matching
// for one cluster policy containing many rules.
func BenchmarkClusterNetworkPolicyRuleFanout(b *testing.B) {
	changes := [...]struct {
		name   string
		change labelChange
	}{{"Pod", benchmarkPodLabelChange()}, {"Namespace", benchmarkNamespaceLabelChange()}}
	for _, count := range []int{10, 25} {
		b.Run(fmt.Sprintf("rules-%d", count), func(b *testing.B) {
			ingress, egress := benchmarkCNPRules(count)
			cnp := &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{Ingress: ingress, Egress: egress},
			}
			for _, scenario := range changes {
				b.Run(scenario.name, func(b *testing.B) {
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						_, _ = getAffectedCnpRulesForChange(cnp, scenario.change)
					}
				})
			}
		})
	}
}
