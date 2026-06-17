package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestAggregateVpcBFDStatus(t *testing.T) {
	up := ovnnb.BFDStatusUp
	down := ovnnb.BFDStatusDown

	require.Equal(t, bfdPortBFDStatusUnknown, aggregateVpcBFDStatus(nil, nil))
	require.Equal(t, bfdPortBFDStatusUnknown, aggregateVpcBFDStatus([]ovnnb.BFD{{Status: &up}}, nil))
	require.Equal(t, bfdPortBFDStatusUnknown, aggregateVpcBFDStatus([]ovnnb.BFD{{Status: &down}}, nil))
	require.Equal(t, bfdPortBFDStatusPartial, aggregateVpcBFDStatus(nil, []ovnsb.BFD{
		{Status: ovnsb.BFDStatusUp},
		{Status: ovnsb.BFDStatusDown},
	}))
	require.Equal(t, bfdPortBFDStatusDown, aggregateVpcBFDStatus([]ovnnb.BFD{{Status: &up}}, []ovnsb.BFD{
		{Status: ovnsb.BFDStatusDown},
	}))
}

func TestShouldMarkVpcBFDPortRepair(t *testing.T) {
	boundDownStatus := kubeovnv1.BFDPortStatus{
		BindingStatus: bfdPortBindingStatusBound,
		BFDStatus:     bfdPortBFDStatusDown,
		ActiveChassis: "chassis-a",
	}

	require.True(t, shouldMarkVpcBFDPortRepair(kubeovnv1.BFDPortStatus{
		BFDStatus:     bfdPortBFDStatusUp,
		ActiveChassis: "chassis-a",
	}, boundDownStatus))
	require.True(t, shouldMarkVpcBFDPortRepair(kubeovnv1.BFDPortStatus{
		BFDStatus:     bfdPortBFDStatusPartial,
		ActiveChassis: "chassis-a",
	}, boundDownStatus))
	require.False(t, shouldMarkVpcBFDPortRepair(kubeovnv1.BFDPortStatus{
		BFDStatus:     bfdPortBFDStatusUnknown,
		ActiveChassis: "chassis-a",
	}, boundDownStatus))
	require.False(t, shouldMarkVpcBFDPortRepair(kubeovnv1.BFDPortStatus{
		BFDStatus:     bfdPortBFDStatusDown,
		ActiveChassis: "chassis-a",
	}, boundDownStatus))
	require.False(t, shouldMarkVpcBFDPortRepair(kubeovnv1.BFDPortStatus{
		BFDStatus:     bfdPortBFDStatusUp,
		ActiveChassis: "chassis-a",
	}, kubeovnv1.BFDPortStatus{
		BindingStatus: bfdPortBindingStatusBound,
		BFDStatus:     bfdPortBFDStatusDown,
	}))
}

func TestMoveChassisToEnd(t *testing.T) {
	require.Equal(t, []string{"b", "c", "a"}, moveChassisToEnd([]string{"a", "b", "c"}, "a"))
	require.Equal(t, []string{"a", "b"}, moveChassisToEnd([]string{"a", "b"}, "missing"))
}

func Test_handleAddOrUpdateVpc_staticRoutes(t *testing.T) {
	t.Parallel()

	vpcName := "test-vpc"

	// Policy variables for taking pointers
	srcIPPolicy := ovnnb.LogicalRouterStaticRoutePolicySrcIP
	dstIPPolicy := ovnnb.LogicalRouterStaticRoutePolicyDstIP

	// Internal static route created directly in OVN with kube-ovn vendor
	internalStaticRoute := &ovnnb.LogicalRouterStaticRoute{
		UUID: "internal-static-route-uuid",
		ExternalIDs: map[string]string{
			"vendor": util.CniTypeName,
		},
		IPPrefix:   "10.0.0.0/24",
		Nexthop:    "1.2.3.4",
		Policy:     &srcIPPolicy,
		RouteTable: util.MainRouteTable,
	}

	// Static route that matches VPC spec
	managedStaticRoute := &ovnnb.LogicalRouterStaticRoute{
		UUID: "managed-static-route-uuid",
		ExternalIDs: map[string]string{
			"vendor": util.CniTypeName,
		},
		IPPrefix:   "192.168.0.0/24",
		Nexthop:    "10.0.0.1",
		Policy:     &dstIPPolicy,
		RouteTable: util.MainRouteTable,
	}

	t.Run("only try to manage static routes with vendor kube-ovn", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController
		fakeinformers := fakeController.fakeInformers
		mockOvnClient := fakeController.mockOvnClient

		// Initialize mutexes
		ctrl.vpcKeyMutex = keymutex.NewHashed(500)

		vpc := &kubeovnv1.Vpc{
			ObjectMeta: metav1.ObjectMeta{
				Name: vpcName,
			},
			Spec: kubeovnv1.VpcSpec{
				StaticRoutes: []*kubeovnv1.StaticRoute{
					{
						CIDR:       "192.168.0.0/24",
						NextHopIP:  "10.0.0.1",
						Policy:     kubeovnv1.PolicyDst,
						RouteTable: util.MainRouteTable,
					},
				},
				EnableExternal: false,
				PolicyRoutes:   nil,
			},
			Status: kubeovnv1.VpcStatus{
				Subnets:        []string{},
				EnableExternal: false,
			},
		}

		_, err := ctrl.config.KubeOvnClient.KubeovnV1().Vpcs().Create(context.Background(), vpc, metav1.CreateOptions{})
		require.NoError(t, err)

		err = fakeinformers.vpcInformer.Informer().GetStore().Add(vpc)
		require.NoError(t, err)

		existingKubeOvnRoutes := []*ovnnb.LogicalRouterStaticRoute{
			internalStaticRoute,
		}

		externalIDs := map[string]string{"vendor": util.CniTypeName}

		mockOvnClient.EXPECT().CreateLogicalRouter(vpcName).Return(nil)
		mockOvnClient.EXPECT().UpdateLogicalRouter(gomock.Any(), gomock.Any()).Return(nil)
		mockOvnClient.EXPECT().ListLogicalRouterStaticRoutes(vpcName, nil, nil, "", externalIDs).Return(existingKubeOvnRoutes, nil)
		mockOvnClient.EXPECT().GetLogicalRouter(vpcName, false).Return(&ovnnb.LogicalRouter{
			Name: vpcName,
			Nat:  []string{},
		}, nil)
		mockOvnClient.EXPECT().DeleteLogicalRouterStaticRoute(vpcName, gomock.Any(), gomock.Any(), "10.0.0.0/24", "1.2.3.4").Return(nil)
		mockOvnClient.EXPECT().AddLogicalRouterStaticRoute(
			vpcName,
			util.MainRouteTable,
			"dst-ip",
			"192.168.0.0/24",
			nil,
			externalIDs,
			"10.0.0.1",
		).Return(nil)
		mockOvnClient.EXPECT().ListLogicalRouterPolicies(vpcName, -1, nil, true).Return(nil, nil)
		mockOvnClient.EXPECT().ListLogicalSwitch(gomock.Any(), gomock.Any()).Return([]ovnnb.LogicalSwitch{}, nil).AnyTimes()
		mockOvnClient.EXPECT().ListLogicalRouter(gomock.Any(), gomock.Any()).Return([]ovnnb.LogicalRouter{}, nil).AnyTimes()
		mockOvnClient.EXPECT().DeleteLogicalRouterPort(fmt.Sprintf("bfd@%s", vpcName)).Return(nil)
		mockOvnClient.EXPECT().DeleteHAChassisGroup(fmt.Sprintf("bfd@%s", vpcName)).Return(nil)
		err = ctrl.handleAddOrUpdateVpc(vpcName)
		require.NoError(t, err)
	})

	t.Run("delete orphaned kube-ovn routes", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController
		fakeinformers := fakeController.fakeInformers
		mockOvnClient := fakeController.mockOvnClient

		ctrl.vpcKeyMutex = keymutex.NewHashed(500)

		vpc := &kubeovnv1.Vpc{
			ObjectMeta: metav1.ObjectMeta{
				Name: vpcName,
			},
			Spec: kubeovnv1.VpcSpec{
				StaticRoutes: []*kubeovnv1.StaticRoute{
					{
						CIDR:       "192.168.0.0/24",
						NextHopIP:  "10.0.0.1",
						Policy:     kubeovnv1.PolicyDst,
						RouteTable: util.MainRouteTable,
					},
				},
				EnableExternal: false,
				PolicyRoutes:   nil,
			},
			Status: kubeovnv1.VpcStatus{
				Subnets:        []string{},
				EnableExternal: false,
			},
		}

		_, err := ctrl.config.KubeOvnClient.KubeovnV1().Vpcs().Create(context.Background(), vpc, metav1.CreateOptions{})
		require.NoError(t, err)

		err = fakeinformers.vpcInformer.Informer().GetStore().Add(vpc)
		require.NoError(t, err)

		existingKubeOvnRoutes := []*ovnnb.LogicalRouterStaticRoute{
			internalStaticRoute,
			managedStaticRoute,
		}

		externalIDs := map[string]string{"vendor": util.CniTypeName}

		mockOvnClient.EXPECT().CreateLogicalRouter(vpcName).Return(nil)
		mockOvnClient.EXPECT().UpdateLogicalRouter(gomock.Any(), gomock.Any()).Return(nil)
		mockOvnClient.EXPECT().ListLogicalRouterStaticRoutes(vpcName, nil, nil, "", externalIDs).Return(existingKubeOvnRoutes, nil)
		mockOvnClient.EXPECT().GetLogicalRouter(vpcName, false).Return(&ovnnb.LogicalRouter{
			Name: vpcName,
			Nat:  []string{},
		}, nil)
		mockOvnClient.EXPECT().DeleteLogicalRouterStaticRoute(vpcName, gomock.Any(), gomock.Any(), "10.0.0.0/24", "1.2.3.4").Return(nil)
		mockOvnClient.EXPECT().ListLogicalRouterPolicies(vpcName, -1, nil, true).Return(nil, nil)
		mockOvnClient.EXPECT().ListLogicalSwitch(gomock.Any(), gomock.Any()).Return([]ovnnb.LogicalSwitch{}, nil).AnyTimes()
		mockOvnClient.EXPECT().ListLogicalRouter(gomock.Any(), gomock.Any()).Return([]ovnnb.LogicalRouter{}, nil).AnyTimes()
		mockOvnClient.EXPECT().DeleteLogicalRouterPort(fmt.Sprintf("bfd@%s", vpcName)).Return(nil)
		mockOvnClient.EXPECT().DeleteHAChassisGroup(fmt.Sprintf("bfd@%s", vpcName)).Return(nil)
		err = ctrl.handleAddOrUpdateVpc(vpcName)
		require.NoError(t, err)
	})

	t.Run("handle empty VPC static routes", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController
		fakeinformers := fakeController.fakeInformers
		mockOvnClient := fakeController.mockOvnClient

		ctrl.vpcKeyMutex = keymutex.NewHashed(500)

		vpcEmpty := &kubeovnv1.Vpc{
			ObjectMeta: metav1.ObjectMeta{
				Name: vpcName,
			},
			Spec: kubeovnv1.VpcSpec{
				StaticRoutes:   []*kubeovnv1.StaticRoute{},
				EnableExternal: false,
				PolicyRoutes:   nil,
			},
			Status: kubeovnv1.VpcStatus{
				Subnets:        []string{},
				EnableExternal: false,
			},
		}

		_, err := ctrl.config.KubeOvnClient.KubeovnV1().Vpcs().Create(context.Background(), vpcEmpty, metav1.CreateOptions{})
		require.NoError(t, err)

		err = fakeinformers.vpcInformer.Informer().GetStore().Add(vpcEmpty)
		require.NoError(t, err)

		existingKubeOvnRoutes := []*ovnnb.LogicalRouterStaticRoute{
			internalStaticRoute,
			managedStaticRoute,
		}

		externalIDs := map[string]string{"vendor": util.CniTypeName}

		mockOvnClient.EXPECT().CreateLogicalRouter(vpcName).Return(nil)
		mockOvnClient.EXPECT().UpdateLogicalRouter(gomock.Any(), gomock.Any()).Return(nil)
		mockOvnClient.EXPECT().ListLogicalRouterStaticRoutes(vpcName, nil, nil, "", externalIDs).Return(existingKubeOvnRoutes, nil)
		mockOvnClient.EXPECT().GetLogicalRouter(vpcName, false).Return(&ovnnb.LogicalRouter{
			Name: vpcName,
			Nat:  []string{},
		}, nil)
		mockOvnClient.EXPECT().DeleteLogicalRouterStaticRoute(vpcName, gomock.Any(), gomock.Any(), "10.0.0.0/24", "1.2.3.4").Return(nil)
		mockOvnClient.EXPECT().DeleteLogicalRouterStaticRoute(vpcName, gomock.Any(), gomock.Any(), "192.168.0.0/24", "10.0.0.1").Return(nil)
		mockOvnClient.EXPECT().ListLogicalRouterPolicies(vpcName, -1, nil, true).Return(nil, nil)
		mockOvnClient.EXPECT().ListLogicalSwitch(gomock.Any(), gomock.Any()).Return([]ovnnb.LogicalSwitch{}, nil).AnyTimes()
		mockOvnClient.EXPECT().ListLogicalRouter(gomock.Any(), gomock.Any()).Return([]ovnnb.LogicalRouter{}, nil).AnyTimes()
		mockOvnClient.EXPECT().DeleteLogicalRouterPort(fmt.Sprintf("bfd@%s", vpcName)).Return(nil)
		mockOvnClient.EXPECT().DeleteHAChassisGroup(fmt.Sprintf("bfd@%s", vpcName)).Return(nil)
		err = ctrl.handleAddOrUpdateVpc(vpcName)
		require.NoError(t, err)
	})

	t.Run("add static routes from VPC spec when none exist", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController
		fakeinformers := fakeController.fakeInformers
		mockOvnClient := fakeController.mockOvnClient

		ctrl.vpcKeyMutex = keymutex.NewHashed(500)

		vpc := &kubeovnv1.Vpc{
			ObjectMeta: metav1.ObjectMeta{
				Name: vpcName,
			},
			Spec: kubeovnv1.VpcSpec{
				StaticRoutes: []*kubeovnv1.StaticRoute{
					{
						CIDR:       "192.168.0.0/24",
						NextHopIP:  "10.0.0.1",
						Policy:     kubeovnv1.PolicyDst,
						RouteTable: util.MainRouteTable,
					},
				},
				EnableExternal: false,
				PolicyRoutes:   nil,
			},
			Status: kubeovnv1.VpcStatus{
				Subnets:        []string{},
				EnableExternal: false,
			},
		}

		_, err := ctrl.config.KubeOvnClient.KubeovnV1().Vpcs().Create(context.Background(), vpc, metav1.CreateOptions{})
		require.NoError(t, err)

		err = fakeinformers.vpcInformer.Informer().GetStore().Add(vpc)
		require.NoError(t, err)

		externalIDs := map[string]string{"vendor": util.CniTypeName}

		mockOvnClient.EXPECT().CreateLogicalRouter(vpcName).Return(nil)
		mockOvnClient.EXPECT().UpdateLogicalRouter(gomock.Any(), gomock.Any()).Return(nil)
		mockOvnClient.EXPECT().ListLogicalRouterStaticRoutes(vpcName, nil, nil, "", externalIDs).Return(nil, nil)
		mockOvnClient.EXPECT().GetLogicalRouter(vpcName, false).Return(&ovnnb.LogicalRouter{
			Name: vpcName,
			Nat:  []string{},
		}, nil)
		mockOvnClient.EXPECT().AddLogicalRouterStaticRoute(
			vpcName,
			util.MainRouteTable,
			"dst-ip",
			"192.168.0.0/24",
			nil,
			externalIDs,
			"10.0.0.1",
		).Return(nil)
		mockOvnClient.EXPECT().ListLogicalRouterPolicies(vpcName, -1, nil, true).Return(nil, nil)
		mockOvnClient.EXPECT().ListLogicalSwitch(gomock.Any(), gomock.Any()).Return([]ovnnb.LogicalSwitch{}, nil).AnyTimes()
		mockOvnClient.EXPECT().ListLogicalRouter(gomock.Any(), gomock.Any()).Return([]ovnnb.LogicalRouter{}, nil).AnyTimes()
		mockOvnClient.EXPECT().DeleteLogicalRouterPort(fmt.Sprintf("bfd@%s", vpcName)).Return(nil)
		mockOvnClient.EXPECT().DeleteHAChassisGroup(fmt.Sprintf("bfd@%s", vpcName)).Return(nil)
		err = ctrl.handleAddOrUpdateVpc(vpcName)
		require.NoError(t, err)
	})
}

func Test_handleAddOrUpdateVpc_policyRoutes_ecmpNextHops(t *testing.T) {
	t.Parallel()

	vpcName := "test-vpc-policy"

	t.Run("empty NextHopIP with non-reroute action should pass nil next-hops", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController
		fakeinformers := fakeController.fakeInformers
		mockOvnClient := fakeController.mockOvnClient

		ctrl.vpcKeyMutex = keymutex.NewHashed(500)

		vpc := &kubeovnv1.Vpc{
			ObjectMeta: metav1.ObjectMeta{
				Name: vpcName,
			},
			Spec: kubeovnv1.VpcSpec{
				StaticRoutes:   []*kubeovnv1.StaticRoute{},
				EnableExternal: false,
				PolicyRoutes: []*kubeovnv1.PolicyRoute{
					{
						Priority:  200,
						Match:     "ip4.src == 10.0.0.0/8",
						Action:    kubeovnv1.PolicyRouteActionDrop,
						NextHopIP: "",
					},
				},
			},
			Status: kubeovnv1.VpcStatus{
				Subnets:        []string{},
				EnableExternal: false,
			},
		}

		_, err := ctrl.config.KubeOvnClient.KubeovnV1().Vpcs().Create(context.Background(), vpc, metav1.CreateOptions{})
		require.NoError(t, err)

		err = fakeinformers.vpcInformer.Informer().GetStore().Add(vpc)
		require.NoError(t, err)

		externalIDs := map[string]string{"vendor": util.CniTypeName}

		mockOvnClient.EXPECT().CreateLogicalRouter(vpcName).Return(nil)
		mockOvnClient.EXPECT().UpdateLogicalRouter(gomock.Any(), gomock.Any()).Return(nil)
		mockOvnClient.EXPECT().ListLogicalRouterStaticRoutes(vpcName, nil, nil, "", externalIDs).Return(nil, nil)
		mockOvnClient.EXPECT().GetLogicalRouter(vpcName, false).Return(&ovnnb.LogicalRouter{
			Name: vpcName,
			Nat:  []string{},
		}, nil)
		mockOvnClient.EXPECT().ListLogicalRouterPolicies(vpcName, -1, nil, true).Return(nil, nil)
		// The key assertion: empty NextHopIP must produce nil next-hops, not [""]
		mockOvnClient.EXPECT().AddLogicalRouterPolicy(
			vpcName,
			200,
			"ip4.src == 10.0.0.0/8",
			string(kubeovnv1.PolicyRouteActionDrop),
			([]string)(nil),
			([]string)(nil),
			externalIDs,
		).Return(nil)
		mockOvnClient.EXPECT().ListLogicalSwitch(gomock.Any(), gomock.Any()).Return([]ovnnb.LogicalSwitch{}, nil).AnyTimes()
		mockOvnClient.EXPECT().ListLogicalRouter(gomock.Any(), gomock.Any()).Return([]ovnnb.LogicalRouter{}, nil).AnyTimes()
		mockOvnClient.EXPECT().DeleteLogicalRouterPort(fmt.Sprintf("bfd@%s", vpcName)).Return(nil)
		mockOvnClient.EXPECT().DeleteHAChassisGroup(fmt.Sprintf("bfd@%s", vpcName)).Return(nil)
		err = ctrl.handleAddOrUpdateVpc(vpcName)
		require.NoError(t, err)
	})

	t.Run("ECMP next-hops are split correctly for custom VPC policy routes", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController
		fakeinformers := fakeController.fakeInformers
		mockOvnClient := fakeController.mockOvnClient

		ctrl.vpcKeyMutex = keymutex.NewHashed(500)

		vpc := &kubeovnv1.Vpc{
			ObjectMeta: metav1.ObjectMeta{
				Name: vpcName,
			},
			Spec: kubeovnv1.VpcSpec{
				StaticRoutes:   []*kubeovnv1.StaticRoute{},
				EnableExternal: false,
				PolicyRoutes: []*kubeovnv1.PolicyRoute{
					{
						Priority:  100,
						Match:     "ip4.dst == 10.1.0.0/16",
						Action:    kubeovnv1.PolicyRouteActionReroute,
						NextHopIP: "192.168.1.1,192.168.1.2",
					},
				},
			},
			Status: kubeovnv1.VpcStatus{
				Subnets:        []string{},
				EnableExternal: false,
			},
		}

		_, err := ctrl.config.KubeOvnClient.KubeovnV1().Vpcs().Create(context.Background(), vpc, metav1.CreateOptions{})
		require.NoError(t, err)

		err = fakeinformers.vpcInformer.Informer().GetStore().Add(vpc)
		require.NoError(t, err)

		externalIDs := map[string]string{"vendor": util.CniTypeName}

		mockOvnClient.EXPECT().CreateLogicalRouter(vpcName).Return(nil)
		mockOvnClient.EXPECT().UpdateLogicalRouter(gomock.Any(), gomock.Any()).Return(nil)
		mockOvnClient.EXPECT().ListLogicalRouterStaticRoutes(vpcName, nil, nil, "", externalIDs).Return(nil, nil)
		mockOvnClient.EXPECT().GetLogicalRouter(vpcName, false).Return(&ovnnb.LogicalRouter{
			Name: vpcName,
			Nat:  []string{},
		}, nil)
		// No existing policies in OVN for this custom VPC
		mockOvnClient.EXPECT().ListLogicalRouterPolicies(vpcName, -1, nil, true).Return(nil, nil)
		// The key assertion: next-hops must be split into a slice, not wrapped as one element
		mockOvnClient.EXPECT().AddLogicalRouterPolicy(
			vpcName,
			100,
			"ip4.dst == 10.1.0.0/16",
			string(kubeovnv1.PolicyRouteActionReroute),
			[]string{"192.168.1.1", "192.168.1.2"},
			([]string)(nil),
			externalIDs,
		).Return(nil)
		mockOvnClient.EXPECT().ListLogicalSwitch(gomock.Any(), gomock.Any()).Return([]ovnnb.LogicalSwitch{}, nil).AnyTimes()
		mockOvnClient.EXPECT().ListLogicalRouter(gomock.Any(), gomock.Any()).Return([]ovnnb.LogicalRouter{}, nil).AnyTimes()
		mockOvnClient.EXPECT().DeleteLogicalRouterPort(fmt.Sprintf("bfd@%s", vpcName)).Return(nil)
		mockOvnClient.EXPECT().DeleteHAChassisGroup(fmt.Sprintf("bfd@%s", vpcName)).Return(nil)
		err = ctrl.handleAddOrUpdateVpc(vpcName)
		require.NoError(t, err)
	})
}

func TestDiffPolicyRouteWithLogical_NormalizesNextHopIPs(t *testing.T) {
	t.Parallel()

	target := []*kubeovnv1.PolicyRoute{{
		Priority:  32000,
		Match:     "ip4.src == 172.16.8.149/32",
		Action:    kubeovnv1.PolicyRouteActionReroute,
		NextHopIP: "172.31.255.253,172.31.255.254",
	}}

	existing := []*ovnnb.LogicalRouterPolicy{{
		Priority: 32000,
		Match:    "ip4.src == 172.16.8.149/32",
		Action:   string(kubeovnv1.PolicyRouteActionReroute),
		Nexthops: []string{"172.31.255.254", "172.31.255.253"},
	}}

	dels, adds := diffPolicyRouteWithLogical(existing, target)
	require.Empty(t, dels)
	require.Empty(t, adds)
}

func TestDiffPolicyRouteWithLogical_HandlesLegacyNextHopField(t *testing.T) {
	t.Parallel()

	nextHop := "172.31.255.253"
	target := []*kubeovnv1.PolicyRoute{{
		Priority:  32000,
		Match:     "ip4.src == 172.16.8.149/32",
		Action:    kubeovnv1.PolicyRouteActionReroute,
		NextHopIP: nextHop,
	}}

	existing := []*ovnnb.LogicalRouterPolicy{{
		Priority: 32000,
		Match:    "ip4.src == 172.16.8.149/32",
		Action:   string(kubeovnv1.PolicyRouteActionReroute),
		Nexthop:  &nextHop,
	}}

	dels, adds := diffPolicyRouteWithLogical(existing, target)
	require.Empty(t, dels)
	require.Empty(t, adds)
}

func TestDiffPolicyRouteWithLogical_IgnoresPoliciesOwnedByOtherControllers(t *testing.T) {
	t.Parallel()

	target := []*kubeovnv1.PolicyRoute{{
		Priority:  32000,
		Match:     "ip4.dst == 10.2.0.0/16",
		Action:    kubeovnv1.PolicyRouteActionDrop,
		NextHopIP: "",
	}}
	existing := []*ovnnb.LogicalRouterPolicy{
		{
			Priority: util.NatGatewayPolicyPriority,
			Match:    "ip4.src == 10.0.7.0/24",
			Action:   string(kubeovnv1.PolicyRouteActionReroute),
			Nexthops: []string{"10.0.7.4", "10.0.7.5"},
			ExternalIDs: map[string]string{
				ovs.ExternalIDVendor:        util.CniTypeName,
				ovs.ExternalIDVpcNatGateway: "ha-test-natgw",
			},
		},
		{
			Priority: util.EgressGatewayPolicyPriority,
			Match:    "ip4.src == 10.0.8.0/24",
			Action:   string(kubeovnv1.PolicyRouteActionReroute),
			Nexthops: []string{"10.0.8.4"},
			ExternalIDs: map[string]string{
				ovs.ExternalIDVendor:           util.CniTypeName,
				ovs.ExternalIDVpcEgressGateway: "default/egress-gw",
			},
		},
		{
			Priority: 32000,
			Match:    "ip4.dst == 10.2.0.0/16",
			Action:   string(kubeovnv1.PolicyRouteActionDrop),
			ExternalIDs: map[string]string{
				ovs.ExternalIDVendor: util.CniTypeName,
			},
		},
	}

	dels, adds := diffPolicyRouteWithLogical(existing, target)
	require.Empty(t, dels)
	require.Empty(t, adds)
}

func TestReconcileVpcBfdLRPClearsHAChassisGroupWhenSelectorMatchesNoNodes(t *testing.T) {
	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient

	vpc := &kubeovnv1.Vpc{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vpc-bfd"},
		Spec: kubeovnv1.VpcSpec{
			BFDPort: &kubeovnv1.BFDPort{
				Enabled: true,
				IP:      "169.254.0.1/32",
				NodeSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"egress": "true"},
				},
			},
		},
	}

	portName := "bfd@test-vpc-bfd"
	networks := []string{"169.254.0.1/32"}
	mockOvnClient.EXPECT().CreateLogicalRouterPort(vpc.Name, portName, "", networks).Return(nil)
	mockOvnClient.EXPECT().UpdateLogicalRouterPortNetworks(portName, networks).Return(nil)
	mockOvnClient.EXPECT().UpdateLogicalRouterPortOptions(portName, map[string]string{"bfd-only": "true"}).Return(nil)
	mockOvnClient.EXPECT().CreateHAChassisGroup(portName, []string{}, map[string]string{"lrp": portName}).Return(nil)
	mockOvnClient.EXPECT().SetLogicalRouterPortHAChassisGroup(portName, portName).Return(nil)

	status, err := ctrl.reconcileVpcBfdLRP(vpc)
	require.NoError(t, err)
	require.Equal(t, portName, status.Name)
	require.Empty(t, status.Nodes)
}

func TestReconcileVpcBfdLRPDoesNotDemoteStartupDownStatus(t *testing.T) {
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Nodes: []*corev1.Node{
			{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "node-b"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "node-c"}},
		},
	})
	require.NoError(t, err)
	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient
	mockOvnSbClient := fakeController.mockOvnSbClient

	vpc := &kubeovnv1.Vpc{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vpc-bfd"},
		Spec: kubeovnv1.VpcSpec{
			BFDPort: &kubeovnv1.BFDPort{
				Enabled: true,
				IP:      "169.254.0.1/32",
			},
		},
		Status: kubeovnv1.VpcStatus{
			BFDPort: kubeovnv1.BFDPortStatus{
				BFDStatus:     bfdPortBFDStatusDown,
				ActiveChassis: "chassis-a",
			},
		},
	}

	portName := "bfd@test-vpc-bfd"
	networks := []string{"169.254.0.1/32"}
	chassisNames := []string{"chassis-a", "chassis-b", "chassis-c"}
	activeChassis := "chassis-a"

	mockOvnSbClient.EXPECT().GetChassisByHost("node-a").Return(&ovnsb.Chassis{Name: "chassis-a"}, nil)
	mockOvnSbClient.EXPECT().GetChassisByHost("node-b").Return(&ovnsb.Chassis{Name: "chassis-b"}, nil)
	mockOvnSbClient.EXPECT().GetChassisByHost("node-c").Return(&ovnsb.Chassis{Name: "chassis-c"}, nil)
	mockOvnClient.EXPECT().CreateLogicalRouterPort(vpc.Name, portName, "", networks).Return(nil)
	mockOvnClient.EXPECT().UpdateLogicalRouterPortNetworks(portName, networks).Return(nil)
	mockOvnClient.EXPECT().UpdateLogicalRouterPortOptions(portName, map[string]string{"bfd-only": "true"}).Return(nil)
	mockOvnClient.EXPECT().CreateHAChassisGroup(portName, chassisNames, map[string]string{"lrp": portName}).Return(nil)
	mockOvnClient.EXPECT().SetLogicalRouterPortHAChassisGroup(portName, portName).Return(nil)
	mockOvnClient.EXPECT().ListHAChassis(portName).Return([]ovnnb.HAChassis{
		{ChassisName: "chassis-a", Priority: 32767},
		{ChassisName: "chassis-b", Priority: 32766},
		{ChassisName: "chassis-c", Priority: 32765},
	}, nil)
	mockOvnSbClient.EXPECT().GetPortBinding(portName, true).Return(&ovnsb.PortBinding{Chassis: &activeChassis}, nil)
	mockOvnClient.EXPECT().ListBFDs(portName, "").Return(nil, nil)
	mockOvnSbClient.EXPECT().ListBFDs(portName, "").Return([]ovnsb.BFD{{Status: ovnsb.BFDStatusDown}}, nil)

	status, err := ctrl.reconcileVpcBfdLRP(vpc)
	require.NoError(t, err)
	require.Equal(t, bfdPortBFDStatusDown, status.BFDStatus)
	require.Equal(t, activeChassis, status.ActiveChassis)
}
