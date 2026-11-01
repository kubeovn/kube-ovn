package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/keymutex"
	"sigs.k8s.io/network-policy-api/apis/v1alpha2"
	cnplister "sigs.k8s.io/network-policy-api/pkg/client/listers/apis/v1alpha2"
)

func TestCnpRejectsUnsupportedRulesBeforeOVNChanges(t *testing.T) {
	for _, tt := range []struct {
		name      string
		protocols []v1alpha2.ClusterNetworkPolicyProtocol
		peers     []v1alpha2.ClusterNetworkPolicyEgressPeer
		wantError string
	}{
		{
			name:      "disabled DNS resolver",
			peers:     []v1alpha2.ClusterNetworkPolicyEgressPeer{{DomainNames: []v1alpha2.DomainName{"example.test."}}},
			wantError: "DNSNameResolver is disabled",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeController(t)
			c := fake.fakeController
			c.config.EnableDNSNameResolver = false
			c.cnpKeyMutex = keymutex.NewHashed(1)
			c.anpPrioNameMap = map[int32]string{55: "existing"}
			c.anpNamePrioMap = map[string]int32{"existing": 55}
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			policy := &v1alpha2.ClusterNetworkPolicy{
				Name: "existing",
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Tier: v1alpha2.AdminTier, Priority: 56,
					Subject: v1alpha2.ClusterNetworkPolicySubject{Namespaces: &metav1.LabelSelector{}},
					Ingress: []v1alpha2.ClusterNetworkPolicyIngressRule{{
						Action: v1alpha2.ClusterNetworkPolicyRuleActionDeny,
						From:   []v1alpha2.ClusterNetworkPolicyIngressPeer{{Namespaces: &metav1.LabelSelector{}}},
					}},
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{{
						Action: v1alpha2.ClusterNetworkPolicyRuleActionAccept, Protocols: tt.protocols, To: tt.peers,
					}},
				},
			}
			require.NoError(t, indexer.Add(policy))
			c.cnpsLister = cnplister.NewClusterNetworkPolicyLister(indexer)
			// No OVN expectations: rejection must precede changes to either direction.
			require.ErrorContains(t, c.handleAddCnp(policy.Name), tt.wantError)
			require.Equal(t, map[int32]string{55: "existing"}, c.anpPrioNameMap)
			require.Equal(t, map[string]int32{"existing": 55}, c.anpNamePrioMap)
		})
	}
}

func TestCnpAllowsSamePriorityCnp(t *testing.T) {
	fake := newFakeController(t)
	c := fake.fakeController
	c.anpPrioNameMap = map[int32]string{10: "existing"}
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	existing := &v1alpha2.ClusterNetworkPolicy{
		Name: "existing",
		Spec: v1alpha2.ClusterNetworkPolicySpec{
			Tier:     v1alpha2.AdminTier,
			Priority: 10,
		},
	}
	require.NoError(t, indexer.Add(existing))
	c.cnpsLister = cnplister.NewClusterNetworkPolicyLister(indexer)

	candidate := &v1alpha2.ClusterNetworkPolicy{
		Name: "candidate",
		Spec: v1alpha2.ClusterNetworkPolicySpec{
			Tier:     v1alpha2.AdminTier,
			Priority: 10,
		},
	}
	require.NoError(t, c.validateCnpConfig(candidate))
}

func TestCnpPriorityMapRestoresSharedPriorityAfterDelete(t *testing.T) {
	fake := newFakeController(t)
	c := fake.fakeController
	c.anpPrioNameMap = map[int32]string{10: "first"}
	c.anpNamePrioMap = map[string]int32{"first": 10, "second": 10}

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	first := &v1alpha2.ClusterNetworkPolicy{
		Name: "first",
		Spec: v1alpha2.ClusterNetworkPolicySpec{Tier: v1alpha2.AdminTier, Priority: 10},
	}
	second := &v1alpha2.ClusterNetworkPolicy{
		Name: "second",
		Spec: v1alpha2.ClusterNetworkPolicySpec{Tier: v1alpha2.AdminTier, Priority: 10},
	}
	require.NoError(t, indexer.Add(first))
	require.NoError(t, indexer.Add(second))
	c.cnpsLister = cnplister.NewClusterNetworkPolicyLister(indexer)

	// Reprocessing the second policy must preserve the shared priority entry.
	require.NoError(t, c.updateCnpPriorityMapEntries(second))
	require.Equal(t, "second", c.anpPrioNameMap[10])

	// The informer removes the object before the delete queue handles it. The
	// remaining CNP must become the representative for the shared priority.
	require.NoError(t, indexer.Delete(second))
	require.NoError(t, c.deleteCnpPriorityMapEntries(second))
	require.Equal(t, "first", c.anpPrioNameMap[10])
	require.Equal(t, int32(10), c.anpNamePrioMap["first"])
	_, exists := c.anpNamePrioMap["second"]
	require.False(t, exists)

	require.NoError(t, indexer.Delete(first))
	require.NoError(t, c.deleteCnpPriorityMapEntries(first))
	_, exists = c.anpPrioNameMap[10]
	require.False(t, exists)
}
