package controller

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/set"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestVpcEgressGatewayPolicyMatches(t *testing.T) {
	tests := []struct {
		name             string
		includePortGroup bool
		want             []string
	}{
		{
			name:             "subnet-only gateway omits port-group address set",
			includePortGroup: false,
			want:             []string{"ip4.src == $VEG.example.ipv4"},
		},
		{
			name:             "selector gateway includes both address sets",
			includePortGroup: true,
			want: []string{
				"ip4.src == $VEG.example.ipv4",
				"ip4.src == $VEG.example_ip4",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vpcEgressGatewayPolicyMatches(4, "VEG.example", "VEG.example.ipv4", tt.includePortGroup)
			require.Equal(t, tt.want, got.SortedList())
		})
	}
}

func TestVpcEgressGatewayLocalPolicyMatches(t *testing.T) {
	got := vpcEgressGatewayLocalPolicyMatches(6, "node.example", "VEG.example", "VEG.example.ipv6", false)
	require.Equal(t, []string{
		"ip6.src == $node.example_ip6 && ip6.src == $VEG.example.ipv6",
	}, got.SortedList())

	got = vpcEgressGatewayLocalPolicyMatches(6, "node.example", "VEG.example", "VEG.example.ipv6", true)
	require.Equal(t, []string{
		"ip6.src == $node.example_ip6 && ip6.src == $VEG.example.ipv6",
		"ip6.src == $node.example_ip6 && ip6.src == $VEG.example_ip6",
	}, got.SortedList())
}

func TestReconcileVpcEgressGatewayOVNRoutesKeepsCompatibilityMatchWithoutSelectors(t *testing.T) {
	fakeController := newFakeController(t)
	controller := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient
	gw := &kubeovnv1.VpcEgressGateway{
		Name: "veg", Namespace: "default",
		Spec: kubeovnv1.VpcEgressGatewaySpec{
			BFD: kubeovnv1.VpcEgressGatewayBFDConfig{Enabled: true},
		},
	}
	externalIDs := map[string]string{
		ovs.ExternalIDVendor:           util.CniTypeName,
		ovs.ExternalIDVpcEgressGateway: "default/veg",
		"af":                           "4",
	}
	pgName := vegPortGroupName("default/veg")
	asName := vegAddressSetName("default/veg", 4)
	compatAsName := vegPortGroupAddressSetName("default/veg", 4)
	asMatch := "ip4.src == $" + asName
	compatMatch := "ip4.src == $" + compatAsName

	mockOvnClient.EXPECT().DeletePortGroup(pgName).Return(nil)
	mockOvnClient.EXPECT().CreateAddressSet(asName, externalIDs).Return(nil)
	mockOvnClient.EXPECT().AddressSetUpdateAddress(asName, "10.0.0.0/24").Return(nil)
	mockOvnClient.EXPECT().CreateAddressSet(compatAsName, externalIDs).Return(nil)
	mockOvnClient.EXPECT().AddressSetUpdateAddress(compatAsName).Return(nil)
	mockOvnClient.EXPECT().FindBFD(externalIDs).Return(nil, nil)
	mockOvnClient.EXPECT().DeleteLogicalRouterPolicies("tenant", util.EgressGatewayLocalPolicyPriority, externalIDs).Return(nil)
	mockOvnClient.EXPECT().ListLogicalRouterPolicies("tenant", util.EgressGatewayPolicyPriority, externalIDs, false).Return(nil, nil)
	mockOvnClient.EXPECT().AddLogicalRouterPolicy(
		"tenant", util.EgressGatewayPolicyPriority, asMatch, ovnnb.LogicalRouterPolicyActionReroute,
		[]string{"192.0.2.2"}, gomock.Any(), externalIDs,
	).Return(nil)
	mockOvnClient.EXPECT().AddLogicalRouterPolicy(
		"tenant", util.EgressGatewayPolicyPriority, compatMatch, ovnnb.LogicalRouterPolicyActionReroute,
		[]string{"192.0.2.2"}, gomock.Any(), externalIDs,
	).Return(nil)
	mockOvnClient.EXPECT().ListLogicalRouterPolicies("tenant", util.EgressGatewayDropPolicyPriority, externalIDs, false).Return(nil, nil)
	mockOvnClient.EXPECT().AddLogicalRouterPolicy(
		"tenant", util.EgressGatewayDropPolicyPriority, asMatch, ovnnb.LogicalRouterPolicyActionDrop,
		nil, nil, externalIDs,
	).Return(nil)
	mockOvnClient.EXPECT().AddLogicalRouterPolicy(
		"tenant", util.EgressGatewayDropPolicyPriority, compatMatch, ovnnb.LogicalRouterPolicyActionDrop,
		nil, nil, externalIDs,
	).Return(nil)

	err := controller.reconcileVpcEgressGatewayOVNRoutes(
		gw, 4, "tenant", "tenant-public", "",
		map[string]set.Set[string]{"node-a": set.New("192.0.2.2")}, set.New("10.0.0.0/24"),
	)
	require.NoError(t, err)
}

func TestReconcileVpcEgressGatewayOVNRoutesCleansStalePolicyWithoutNextHops(t *testing.T) {
	fakeController := newFakeController(t)
	controller := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient
	gw := &kubeovnv1.VpcEgressGateway{
		Name: "veg", Namespace: "default",
	}
	externalIDs := map[string]string{
		ovs.ExternalIDVendor:           util.CniTypeName,
		ovs.ExternalIDVpcEgressGateway: "default/veg",
		"af":                           "4",
	}
	pgName := vegPortGroupName("default/veg")
	asName := vegAddressSetName("default/veg", 4)
	compatAsName := vegPortGroupAddressSetName("default/veg", 4)
	stalePolicy := &ovnnb.LogicalRouterPolicy{
		UUID:  "stale-policy",
		Match: "ip4.src == $" + pgName + "_ip4",
	}

	mockOvnClient.EXPECT().DeletePortGroup(pgName).Return(nil)
	mockOvnClient.EXPECT().CreateAddressSet(asName, externalIDs).Return(nil)
	mockOvnClient.EXPECT().AddressSetUpdateAddress(asName, "10.0.0.0/24").Return(nil)
	mockOvnClient.EXPECT().CreateAddressSet(compatAsName, externalIDs).Return(nil)
	mockOvnClient.EXPECT().AddressSetUpdateAddress(compatAsName).Return(nil)
	mockOvnClient.EXPECT().FindBFD(externalIDs).Return(nil, nil)
	mockOvnClient.EXPECT().DeleteLogicalRouterPolicies("tenant", util.EgressGatewayLocalPolicyPriority, externalIDs).Return(nil)
	mockOvnClient.EXPECT().ListLogicalRouterPolicies("tenant", util.EgressGatewayPolicyPriority, externalIDs, false).
		Return([]*ovnnb.LogicalRouterPolicy{stalePolicy}, nil)
	mockOvnClient.EXPECT().DeleteLogicalRouterPolicyByUUID("tenant", stalePolicy.UUID).Return(nil)
	mockOvnClient.EXPECT().DeleteLogicalRouterPolicies("tenant", util.EgressGatewayDropPolicyPriority, externalIDs).Return(nil)

	err := controller.reconcileVpcEgressGatewayOVNRoutes(
		gw, 4, "tenant", "tenant-public", "", nil, set.New("10.0.0.0/24"),
	)
	require.NoError(t, err)
}

func TestReconcileVpcEgressGatewayOVNRoutesNonPrimaryCNIRejectsSelectors(t *testing.T) {
	// Regression coverage for: a non-primary-CNI gateway that previously
	// forwarded a subnet's traffic via spec.policies (i.e. `sources`
	// contained the subnet CIDR in the OVN address set, with an active
	// reroute LR policy) has its spec switched to (unsupported, in this
	// mode) selectors, with the subnet removed from policies at the same
	// time. The reconcile must still converge the address set / port group
	// / LR policy / BFD state to the new (selector-less) reality -- clearing
	// the stale subnet entry and its reroute policy -- and only then return
	// the "selectors not supported" error, instead of bailing out up front
	// and leaving the old forwarding state silently active.
	fakeController := newFakeController(t)
	controller := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient
	controller.config.EnableNonPrimaryCNI = true

	gw := &kubeovnv1.VpcEgressGateway{
		Name: "veg", Namespace: "default",
		Spec: kubeovnv1.VpcEgressGatewaySpec{
			BFD: kubeovnv1.VpcEgressGatewayBFDConfig{Enabled: true},
			Selectors: []kubeovnv1.VpcEgressGatewaySelector{{
				PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			}},
		},
	}
	externalIDs := map[string]string{
		ovs.ExternalIDVendor:           util.CniTypeName,
		ovs.ExternalIDVpcEgressGateway: "default/veg",
		"af":                           "4",
	}
	pgName := vegPortGroupName("default/veg")
	asName := vegAddressSetName("default/veg", 4)
	compatAsName := vegPortGroupAddressSetName("default/veg", 4)
	asMatch := "ip4.src == $" + asName
	compatMatch := "ip4.src == $" + compatAsName

	// Selectors are unsupported in this mode, so they must be treated as
	// absent: no port group is (re)created for them (any stale one from a
	// prior reconcile is deleted instead), while the subnet/ipBlock-derived
	// `sources` (already correctly recomputed by the caller from the
	// *current* spec.policies, which no longer include the removed subnet)
	// must still be pushed into the address set.
	mockOvnClient.EXPECT().DeletePortGroup(pgName).Return(nil)
	mockOvnClient.EXPECT().CreateAddressSet(asName, externalIDs).Return(nil)
	mockOvnClient.EXPECT().AddressSetUpdateAddress(asName).Return(nil) // no sources left: stale subnet entry is cleared
	mockOvnClient.EXPECT().CreateAddressSet(compatAsName, externalIDs).Return(nil)
	mockOvnClient.EXPECT().AddressSetUpdateAddress(compatAsName).Return(nil)
	mockOvnClient.EXPECT().FindBFD(externalIDs).Return(nil, nil)
	mockOvnClient.EXPECT().DeleteLogicalRouterPolicies("tenant", util.EgressGatewayLocalPolicyPriority, externalIDs).Return(nil)
	mockOvnClient.EXPECT().ListLogicalRouterPolicies("tenant", util.EgressGatewayPolicyPriority, externalIDs, false).Return(nil, nil)
	mockOvnClient.EXPECT().ListLogicalRouterPolicies("tenant", util.EgressGatewayDropPolicyPriority, externalIDs, false).Return(nil, nil)
	mockOvnClient.EXPECT().AddLogicalRouterPolicy(
		"tenant", util.EgressGatewayDropPolicyPriority, asMatch, ovnnb.LogicalRouterPolicyActionDrop,
		nil, nil, externalIDs,
	).Return(nil)
	mockOvnClient.EXPECT().AddLogicalRouterPolicy(
		"tenant", util.EgressGatewayDropPolicyPriority, compatMatch, ovnnb.LogicalRouterPolicyActionDrop,
		nil, nil, externalIDs,
	).Return(nil)

	// No active nodes/nexthops and an empty `sources` set models the
	// gateway's state right after the subnet was dropped from
	// spec.policies in the same update that introduced the selectors.
	err := controller.reconcileVpcEgressGatewayOVNRoutes(
		gw, 4, "tenant", "tenant-public", "", nil, set.New[string](),
	)

	// The unsupported-selectors error must still surface...
	require.Error(t, err)
	require.ErrorContains(t, err, "not supported")

	// ...but only after the above convergence calls were made: the mock
	// client's expectations assert that no stale port group or address set
	// entry for the previously-forwarded subnet survives the reconcile.
	fakeRecorder, ok := controller.recorder.(*record.FakeRecorder)
	require.True(t, ok, "expected fake recorder")

	event := requireRecorderEvent(t, fakeRecorder)
	require.Contains(t, event, "Warning")
	require.Contains(t, event, "SelectorsNotSupported")
}

// TestReconcileVpcEgressGatewayRoutesConvergesBothAFsOnUnsupportedSelectors is a
// regression test for a non-primary-CNI gateway that transitions from a
// subnet-based policy to (unsupported) selectors. Before the fix,
// reconcileVpcEgressGatewayRoutes returned immediately once the IPv4 AF
// reconcile reported the "selectors not supported" error, so the IPv6
// address set / LR policy / BFD state was never reconciled and could retain
// stale forwarding entries for a dual-stack gateway. This test asserts that
// OVN state for *both* AF=4 and AF=6 is converged (every expected OVN call
// below must be observed for the test to pass) and that the aggregated
// error is still surfaced to the caller.
func TestReconcileVpcEgressGatewayRoutesConvergesBothAFsOnUnsupportedSelectors(t *testing.T) {
	fakeController := newFakeController(t)
	controller := fakeController.fakeController
	controller.config.EnableNonPrimaryCNI = true
	mockOvnClient := fakeController.mockOvnClient

	gw := &kubeovnv1.VpcEgressGateway{
		Name: "veg", Namespace: "default",
		Spec: kubeovnv1.VpcEgressGatewaySpec{
			// Previously used subnet-based policies forwarded subnet A's
			// traffic; now switched to (unsupported, in non-primary CNI
			// mode) selectors. sources below represents what the current
			// spec.policies compute to (empty, since the subnet policy was
			// removed as part of the transition).
			Selectors: []kubeovnv1.VpcEgressGatewaySelector{
				{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}}},
			},
		},
	}
	key := "default/veg"
	pgName := vegPortGroupName(key)

	for _, af := range [...]int{4, 6} {
		externalIDs := map[string]string{
			ovs.ExternalIDVendor:           util.CniTypeName,
			ovs.ExternalIDVpcEgressGateway: key,
			"af":                           strconv.Itoa(af),
		}
		asName := vegAddressSetName(key, af)
		compatAsName := vegPortGroupAddressSetName(key, af)

		mockOvnClient.EXPECT().DeletePortGroup(pgName).Return(nil)
		mockOvnClient.EXPECT().CreateAddressSet(asName, externalIDs).Return(nil)
		mockOvnClient.EXPECT().AddressSetUpdateAddress(asName).Return(nil)
		mockOvnClient.EXPECT().CreateAddressSet(compatAsName, externalIDs).Return(nil)
		mockOvnClient.EXPECT().AddressSetUpdateAddress(compatAsName).Return(nil)
		mockOvnClient.EXPECT().FindBFD(externalIDs).Return(nil, nil)
		mockOvnClient.EXPECT().DeleteLogicalRouterPolicies("tenant", util.EgressGatewayLocalPolicyPriority, externalIDs).Return(nil)
		mockOvnClient.EXPECT().ListLogicalRouterPolicies("tenant", util.EgressGatewayPolicyPriority, externalIDs, false).Return(nil, nil)
		mockOvnClient.EXPECT().DeleteLogicalRouterPolicies("tenant", util.EgressGatewayDropPolicyPriority, externalIDs).Return(nil)
	}

	ctx := &vpcEgressGatewayReconcileContext{
		gateway: gw,
		vpc: &kubeovnv1.Vpc{
			Status: kubeovnv1.VpcStatus{Router: "tenant", BFDPort: kubeovnv1.BFDPortStatus{Name: "tenant-public"}},
		},
	}
	state := &vpcEgressGatewayWorkloadState{
		ipv4Src: set.New[string](),
		ipv6Src: set.New[string](),
	}

	err := controller.reconcileVpcEgressGatewayRoutes(ctx, state)
	require.Error(t, err)
	require.Contains(t, err.Error(), "spec.selectors is not supported when kube-ovn runs as a non-primary CNI")
	// Both AF=4 and AF=6 errors should be present in the aggregated error.
	require.Contains(t, err.Error(), "IPv4")
	require.Contains(t, err.Error(), "IPv6")
}
