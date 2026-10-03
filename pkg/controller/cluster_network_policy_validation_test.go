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
			name:      "named port",
			protocols: []v1alpha2.ClusterNetworkPolicyProtocol{{DestinationNamedPort: "https"}},
			peers:     []v1alpha2.ClusterNetworkPolicyEgressPeer{{Namespaces: &metav1.LabelSelector{}}},
			wantError: "destinationNamedPort is not supported",
		},
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
