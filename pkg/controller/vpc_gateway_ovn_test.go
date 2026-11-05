package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/set"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestGatewayReconciliationLegacyAndProvider(t *testing.T) {
	for _, provider := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "provider"}[provider], func(t *testing.T) {
			client := newInMemoryOVNNbClient(t)
			controller := &Controller{OVNNbClient: client}
			if provider {
				controller.OVNNbTables = client
			}
			const router, port, destination = "gateway-router", "gateway-port", "192.0.2.10"
			externalIDs := map[string]string{"gateway": "test"}
			require.NoError(t, client.CreateLogicalRouter(router))
			require.NoError(t, client.Table(&ovnnb.BFD{}).Create(t.Context(), "seed-gateway-bfd",
				&ovnnb.BFD{LogicalPort: port, DstIP: destination, ExternalIDs: externalIDs},
				&ovnnb.BFD{LogicalPort: port, DstIP: "192.0.2.20", ExternalIDs: externalIDs}))
			require.Eventually(t, func() bool {
				rows, err := client.FindBFD(externalIDs)
				return err == nil && len(rows) == 2
			}, time.Second, time.Millisecond)
			nextHops := set.New(destination)
			ids, mapping, stale, err := controller.reconcileGatewayBFDBackend("192.0.2.1", port, nextHops, 100, 200, 3, externalIDs)
			require.NoError(t, err)
			require.Len(t, ids, 1)
			require.Len(t, stale, 1)
			require.True(t, ids.Has(mapping[destination]))
			require.NoError(t, controller.cleanupStaleBFDBackend(stale))

			reconcilePolicies := func(enabled bool, bfdIDs set.Set[string], cidrs []string) error {
				if provider {
					return controller.reconcileNatGatewayPolicies("test", router, 4, enabled, bfdIDs, cidrs, map[string]string{"node": destination}, externalIDs)
				}
				return reconcileNatGatewayPolicies(client, "test", router, 4, enabled, bfdIDs, cidrs, map[string]string{"node": destination}, externalIDs)
			}
			require.NoError(t, reconcilePolicies(true, ids, []string{"10.0.0.0/24"}))
			require.Eventually(t, func() bool {
				policies, err := client.ListLogicalRouterPolicies(router, util.NatGatewayPolicyPriority, externalIDs, false)
				drops, dropErr := client.ListLogicalRouterPolicies(router, util.NatGatewayDropPolicyPriority, externalIDs, false)
				return err == nil && dropErr == nil && len(policies) == 1 && len(drops) == 1 &&
					policies[0].Match == "ip4.src == 10.0.0.0/24" && ids.Equal(set.New(policies[0].BFDSessions...))
			}, time.Second, time.Millisecond)

			require.NoError(t, reconcilePolicies(false, set.New[string](), []string{"10.1.0.0/24"}))
			require.Eventually(t, func() bool {
				policies, err := client.ListLogicalRouterPolicies(router, util.NatGatewayPolicyPriority, externalIDs, false)
				drops, dropErr := client.ListLogicalRouterPolicies(router, util.NatGatewayDropPolicyPriority, externalIDs, false)
				return err == nil && dropErr == nil && len(policies) == 1 && len(drops) == 0 &&
					policies[0].Match == "ip4.src == 10.1.0.0/24" && len(policies[0].BFDSessions) == 0
			}, time.Second, time.Millisecond)
			ids, mapping, stale, err = controller.reconcileGatewayBFDBackend("", port, nextHops, 100, 200, 3, externalIDs)
			require.NoError(t, err)
			require.Empty(t, ids)
			require.Empty(t, mapping)
			require.NoError(t, controller.cleanupStaleBFDBackend(stale))
			require.NoError(t, reconcilePolicies(false, ids, nil))
			require.Eventually(t, func() bool {
				policies, err := client.ListLogicalRouterPolicies(router, -1, externalIDs, false)
				bfds, bfdErr := client.FindBFD(externalIDs)
				return err == nil && bfdErr == nil && len(policies) == 0 && len(bfds) == 0
			}, time.Second, time.Millisecond)
		})
	}
}
