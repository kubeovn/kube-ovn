package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
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
