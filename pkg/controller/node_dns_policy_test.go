package controller

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// pgAs mirrors the production logic at node.go:1087
func pgAs(nodePortName string, af int) string {
	return strings.ReplaceAll(fmt.Sprintf("%s_ip%d", nodePortName, af), "-", ".")
}

func TestAddPolicyRouteForLocalDNSCacheOnNode_DualStackCrossDeletion(t *testing.T) {
	t.Parallel()

	// This test verifies that in a dual-stack environment,
	// addPolicyRouteForLocalDNSCacheOnNode(af=4) does NOT delete af=6 policies,
	// and vice versa.
	//
	// BUG: L1096 uses GetLogicalRouterPoliciesByExtID which only filters by "node" key,
	// returning policies from ALL address families. The loop at L1102-1116 then deletes
	// any policy whose Match is not in the current af's match set — which includes
	// all policies from the other address family.

	fc := newFakeController(t)
	ctrl := fc.fakeController
	mockOvnClient := fc.mockOvnClient

	const (
		nodeName     = "test-node"
		nodePortName = "test-node"
		nodeIPv4     = "10.0.0.1"
		nodeIPv6     = "fd00::1"
		dnsIPv4      = "169.254.20.10"
		dnsIPv6      = "fd00::ffff:1"
	)

	matchV4 := fmt.Sprintf("ip4.src == $%s && ip4.dst == %s", pgAs(nodePortName, 4), dnsIPv4)
	matchV6 := fmt.Sprintf("ip6.src == $%s && ip6.dst == %s", pgAs(nodePortName, 6), dnsIPv6)

	// Simulate existing policies for BOTH address families in OVN
	existingV4Policy := &ovnnb.LogicalRouterPolicy{
		UUID:     "uuid-v4-dns",
		Priority: util.NodeRouterPolicyPriority,
		Match:    matchV4,
		Action:   string(kubeovnv1.PolicyRouteActionReroute),
		Nexthops: []string{nodeIPv4},
		ExternalIDs: map[string]string{
			"vendor":          util.CniTypeName,
			"node":            nodeName,
			"address-family":  "4",
			"isLocalDnsCache": "true",
		},
	}

	existingV6Policy := &ovnnb.LogicalRouterPolicy{
		UUID:     "uuid-v6-dns",
		Priority: util.NodeRouterPolicyPriority,
		Match:    matchV6,
		Action:   string(kubeovnv1.PolicyRouteActionReroute),
		Nexthops: []string{nodeIPv6},
		ExternalIDs: map[string]string{
			"vendor":          util.CniTypeName,
			"node":            nodeName,
			"address-family":  "6",
			"isLocalDnsCache": "true",
		},
	}

	t.Run("af4_call_should_not_delete_af6_policy", func(t *testing.T) {
		// After fix: ListLogicalRouterPolicies filters by address-family=4,
		// so only the v4 policy is returned. The v6 policy is never seen.
		mockOvnClient.EXPECT().
			ListLogicalRouterPolicies(ctrl.config.ClusterRouter, -1, map[string]string{
				"vendor":          util.CniTypeName,
				"node":            nodeName,
				"address-family":  "4",
				"isLocalDnsCache": "true",
			}, true).
			Return([]*ovnnb.LogicalRouterPolicy{existingV4Policy}, nil)

		// No delete calls expected — the v4 policy matches and the v6 is not returned.

		err := ctrl.addPolicyRouteForLocalDNSCacheOnNode(
			[]string{dnsIPv4}, nodePortName, nodeIPv4, nodeName, 4,
		)
		require.NoError(t, err)
	})

	t.Run("af6_call_should_not_delete_af4_policy", func(t *testing.T) {
		// After fix: only af=6 policies are returned
		mockOvnClient.EXPECT().
			ListLogicalRouterPolicies(ctrl.config.ClusterRouter, -1, map[string]string{
				"vendor":          util.CniTypeName,
				"node":            nodeName,
				"address-family":  "6",
				"isLocalDnsCache": "true",
			}, true).
			Return([]*ovnnb.LogicalRouterPolicy{existingV6Policy}, nil)

		// No delete calls expected

		err := ctrl.addPolicyRouteForLocalDNSCacheOnNode(
			[]string{dnsIPv6}, nodePortName, nodeIPv6, nodeName, 6,
		)
		require.NoError(t, err)
	})
}

func TestAddPolicyRouteForLocalDNSCacheOnNode_DualStackFullSimulation(t *testing.T) {
	t.Parallel()

	// Simulate the full dual-stack scenario from handleAddNode:
	// 1. First call with af=4 creates v4 policies (no existing policies)
	// 2. Second call with af=6 creates v6 policies WITHOUT touching v4 ones

	fc := newFakeController(t)
	ctrl := fc.fakeController
	mockOvnClient := fc.mockOvnClient

	const (
		nodeName     = "dualnode"
		nodePortName = "dualnode"
		nodeIPv4     = "10.0.0.2"
		nodeIPv6     = "fd00::2"
		dnsIPv4      = "169.254.20.10"
		dnsIPv6      = "fd00::ffff:1"
	)

	matchV4 := fmt.Sprintf("ip4.src == $%s && ip4.dst == %s", pgAs(nodePortName, 4), dnsIPv4)
	matchV6 := fmt.Sprintf("ip6.src == $%s && ip6.dst == %s", pgAs(nodePortName, 6), dnsIPv6)

	externalIDsV4 := map[string]string{
		"vendor":          util.CniTypeName,
		"node":            nodeName,
		"address-family":  "4",
		"isLocalDnsCache": "true",
	}
	externalIDsV6 := map[string]string{
		"vendor":          util.CniTypeName,
		"node":            nodeName,
		"address-family":  "6",
		"isLocalDnsCache": "true",
	}

	t.Run("step1_af4_creates_policy", func(t *testing.T) {
		// No existing af=4 policies
		mockOvnClient.EXPECT().
			ListLogicalRouterPolicies(ctrl.config.ClusterRouter, -1, externalIDsV4, true).
			Return(nil, nil)

		// Should create v4 policy
		mockOvnClient.EXPECT().
			AddLogicalRouterPolicy(
				ctrl.config.ClusterRouter,
				util.NodeRouterPolicyPriority,
				matchV4,
				string(kubeovnv1.PolicyRouteActionReroute),
				[]string{nodeIPv4},
				([]string)(nil),
				externalIDsV4,
			).Return(nil)

		err := ctrl.addPolicyRouteForLocalDNSCacheOnNode(
			[]string{dnsIPv4}, nodePortName, nodeIPv4, nodeName, 4,
		)
		require.NoError(t, err)
	})

	t.Run("step2_af6_creates_without_deleting_af4", func(t *testing.T) {
		// After fix: ListLogicalRouterPolicies filters by af=6, v4 policy is invisible
		mockOvnClient.EXPECT().
			ListLogicalRouterPolicies(ctrl.config.ClusterRouter, -1, externalIDsV6, true).
			Return(nil, nil)

		// No delete expected — v4 policy is safe

		// v6 policy gets created
		mockOvnClient.EXPECT().
			AddLogicalRouterPolicy(
				ctrl.config.ClusterRouter,
				util.NodeRouterPolicyPriority,
				matchV6,
				string(kubeovnv1.PolicyRouteActionReroute),
				[]string{nodeIPv6},
				([]string)(nil),
				externalIDsV6,
			).Return(nil)

		err := ctrl.addPolicyRouteForLocalDNSCacheOnNode(
			[]string{dnsIPv6}, nodePortName, nodeIPv6, nodeName, 6,
		)
		require.NoError(t, err)
	})
}

func TestAddPolicyRouteForLocalDNSCacheOnNode_DeletesStalePolicy(t *testing.T) {
	t.Parallel()

	// Verify that stale policies within the SAME address family are still deleted.
	// For example, if a DNS IP changes, the old policy should be removed.

	fc := newFakeController(t)
	ctrl := fc.fakeController
	mockOvnClient := fc.mockOvnClient

	const (
		nodeName     = "node1"
		nodePortName = "node1"
		nodeIPv4     = "10.0.0.3"
		oldDNSIPv4   = "169.254.20.10"
		newDNSIPv4   = "169.254.20.11"
	)

	oldMatchV4 := fmt.Sprintf("ip4.src == $%s && ip4.dst == %s", pgAs(nodePortName, 4), oldDNSIPv4)
	newMatchV4 := fmt.Sprintf("ip4.src == $%s && ip4.dst == %s", pgAs(nodePortName, 4), newDNSIPv4)

	externalIDsV4 := map[string]string{
		"vendor":          util.CniTypeName,
		"node":            nodeName,
		"address-family":  strconv.Itoa(4),
		"isLocalDnsCache": "true",
	}

	stalePolicy := &ovnnb.LogicalRouterPolicy{
		UUID:        "uuid-stale",
		Priority:    util.NodeRouterPolicyPriority,
		Match:       oldMatchV4,
		Action:      string(kubeovnv1.PolicyRouteActionReroute),
		Nexthops:    []string{nodeIPv4},
		ExternalIDs: externalIDsV4,
	}

	// ListLogicalRouterPolicies returns the stale policy (same af)
	mockOvnClient.EXPECT().
		ListLogicalRouterPolicies(ctrl.config.ClusterRouter, -1, externalIDsV4, true).
		Return([]*ovnnb.LogicalRouterPolicy{stalePolicy}, nil)

	// Stale policy should be deleted (old DNS IP, same af)
	mockOvnClient.EXPECT().
		DeleteLogicalRouterPolicyByUUID(ctrl.config.ClusterRouter, "uuid-stale").
		Return(nil)

	// New policy should be created
	mockOvnClient.EXPECT().
		AddLogicalRouterPolicy(
			ctrl.config.ClusterRouter,
			util.NodeRouterPolicyPriority,
			newMatchV4,
			string(kubeovnv1.PolicyRouteActionReroute),
			[]string{nodeIPv4},
			([]string)(nil),
			externalIDsV4,
		).Return(nil)

	err := ctrl.addPolicyRouteForLocalDNSCacheOnNode(
		[]string{newDNSIPv4}, nodePortName, nodeIPv4, nodeName, 4,
	)
	require.NoError(t, err)
}
