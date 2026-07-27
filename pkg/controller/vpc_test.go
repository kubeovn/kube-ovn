package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

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

	name, nodes, err := ctrl.reconcileVpcBfdLRP(vpc)
	require.NoError(t, err)
	require.Equal(t, portName, name)
	require.Empty(t, nodes)
}

func Test_createVpcRouter_dynamicRouting(t *testing.T) {
	t.Parallel()

	t.Run("dynamic routing enabled sets lr options", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController
		mockOvnClient := fakeController.mockOvnClient

		vpc := &kubeovnv1.Vpc{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vpc-dr"},
			Spec: kubeovnv1.VpcSpec{
				DynamicRouting: &kubeovnv1.VpcDynamicRouting{
					Enabled:      true,
					Redistribute: []kubeovnv1.RedistributeType{kubeovnv1.RedistributeNAT, kubeovnv1.RedistributeLB},
					LocalOnly:    true,
					VrfName:      "vpc-dr",
					VrfID:        1001,
				},
			},
		}

		mockOvnClient.EXPECT().CreateLogicalRouter(vpc.Name).Return(nil)
		mockOvnClient.EXPECT().GetLogicalRouter(vpc.Name, false).Return(&ovnnb.LogicalRouter{Name: vpc.Name}, nil)
		mockOvnClient.EXPECT().UpdateLogicalRouter(gomock.Any(), gomock.Any()).DoAndReturn(
			func(lr *ovnnb.LogicalRouter, _ ...any) error {
				require.Equal(t, map[string]string{
					"mac_binding_age_threshold":               "300",
					"dynamic_neigh_routers":                   "true",
					"dynamic-routing":                         "true",
					"dynamic-routing-redistribute":            "nat,lb",
					"dynamic-routing-redistribute-local-only": "true",
					"dynamic-routing-vrf-name":                "vpc-dr",
					"dynamic-routing-vrf-id":                  "1001",
				}, lr.Options)
				return nil
			})

		require.NoError(t, ctrl.createVpcRouter(vpc, true))
	})

	t.Run("dynamic routing disabled clears lr options", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController
		mockOvnClient := fakeController.mockOvnClient

		vpc := &kubeovnv1.Vpc{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vpc-dr-off"},
			Spec:       kubeovnv1.VpcSpec{},
		}

		mockOvnClient.EXPECT().CreateLogicalRouter(vpc.Name).Return(nil)
		mockOvnClient.EXPECT().GetLogicalRouter(vpc.Name, false).Return(&ovnnb.LogicalRouter{
			Name: vpc.Name,
			Options: map[string]string{
				"mac_binding_age_threshold": "300",
				"dynamic_neigh_routers":     "true",
				"dynamic-routing":           "true",
			},
		}, nil)
		mockOvnClient.EXPECT().UpdateLogicalRouter(gomock.Any(), gomock.Any()).DoAndReturn(
			func(lr *ovnnb.LogicalRouter, _ ...any) error {
				require.Equal(t, map[string]string{
					"mac_binding_age_threshold": "300",
					"dynamic_neigh_routers":     "true",
				}, lr.Options)
				return nil
			})

		require.NoError(t, ctrl.createVpcRouter(vpc, true))
	})
}

func Test_reconcileVpcDynamicRoutingLrpOptions(t *testing.T) {
	t.Parallel()

	t.Run("maintain vrf set on existing external lrp", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController
		mockOvnClient := fakeController.mockOvnClient
		ctrl.config.ExternalGatewaySwitch = "external204"

		vpc := &kubeovnv1.Vpc{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vpc-dr"},
			Spec: kubeovnv1.VpcSpec{
				EnableExternal: true,
				DynamicRouting: &kubeovnv1.VpcDynamicRouting{
					Enabled:     true,
					MaintainVrf: true,
				},
			},
		}

		lrpName := "test-vpc-dr-external204"
		mockOvnClient.EXPECT().GetLogicalRouterPort(lrpName, true).Return(&ovnnb.LogicalRouterPort{Name: lrpName}, nil)
		mockOvnClient.EXPECT().UpdateLogicalRouterPortOptions(lrpName, map[string]string{
			"dynamic-routing-maintain-vrf": "true",
		}).Return(nil)

		require.NoError(t, ctrl.reconcileVpcDynamicRoutingLrpOptions(vpc))
	})

	t.Run("maintain vrf cleared when dynamic routing disabled", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController
		mockOvnClient := fakeController.mockOvnClient
		ctrl.config.ExternalGatewaySwitch = "external204"

		vpc := &kubeovnv1.Vpc{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vpc-dr"},
			Spec:       kubeovnv1.VpcSpec{EnableExternal: true},
		}

		lrpName := "test-vpc-dr-external204"
		mockOvnClient.EXPECT().GetLogicalRouterPort(lrpName, true).Return(&ovnnb.LogicalRouterPort{Name: lrpName}, nil)
		mockOvnClient.EXPECT().UpdateLogicalRouterPortOptions(lrpName, map[string]string{
			"dynamic-routing-maintain-vrf": "",
		}).Return(nil)

		require.NoError(t, ctrl.reconcileVpcDynamicRoutingLrpOptions(vpc))
	})

	t.Run("missing lrp is skipped", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController
		mockOvnClient := fakeController.mockOvnClient
		ctrl.config.ExternalGatewaySwitch = "external204"

		vpc := &kubeovnv1.Vpc{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vpc-dr"},
			Spec:       kubeovnv1.VpcSpec{EnableExternal: true},
		}

		mockOvnClient.EXPECT().GetLogicalRouterPort("test-vpc-dr-external204", true).Return(nil, nil)

		require.NoError(t, ctrl.reconcileVpcDynamicRoutingLrpOptions(vpc))
	})
}

func Test_enqueueUpdateVpc_dynamicRouting(t *testing.T) {
	t.Parallel()

	baseVpc := func() *kubeovnv1.Vpc {
		return &kubeovnv1.Vpc{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vpc-dr", ResourceVersion: "1"},
			Spec: kubeovnv1.VpcSpec{
				EnableExternal: true,
				DynamicRouting: &kubeovnv1.VpcDynamicRouting{
					Enabled:      true,
					Redistribute: []kubeovnv1.RedistributeType{kubeovnv1.RedistributeNAT},
				},
			},
		}
	}

	tests := []struct {
		name     string
		mutate   func(vpc *kubeovnv1.Vpc)
		enqueued bool
	}{
		{
			name:     "no change is skipped",
			mutate:   func(*kubeovnv1.Vpc) {},
			enqueued: false,
		},
		{
			name:     "disabling dynamic routing is enqueued",
			mutate:   func(vpc *kubeovnv1.Vpc) { vpc.Spec.DynamicRouting.Enabled = false },
			enqueued: true,
		},
		{
			name: "redistribute change is enqueued",
			mutate: func(vpc *kubeovnv1.Vpc) {
				vpc.Spec.DynamicRouting.Redistribute = []kubeovnv1.RedistributeType{kubeovnv1.RedistributeLB}
			},
			enqueued: true,
		},
		{
			name:     "removing the whole block is enqueued",
			mutate:   func(vpc *kubeovnv1.Vpc) { vpc.Spec.DynamicRouting = nil },
			enqueued: true,
		},
		{
			name: "adding the block to a vpc without it is enqueued",
			mutate: func(vpc *kubeovnv1.Vpc) {
				vpc.Spec.DynamicRouting.MaintainVrf = true
			},
			enqueued: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := &Controller{
				addOrUpdateVpcQueue: newTypedRateLimitingQueue[string]("AddOrUpdateVpc", nil),
				vpcLastPoliciesMap:  xsync.NewMap[string, string](),
			}
			defer ctrl.addOrUpdateVpcQueue.ShutDown()

			oldVpc, newVpc := baseVpc(), baseVpc()
			newVpc.ResourceVersion = "2"
			tt.mutate(newVpc)

			ctrl.enqueueUpdateVpc(oldVpc, newVpc)
			require.Equal(t, tt.enqueued, ctrl.addOrUpdateVpcQueue.Len() == 1)
		})
	}
}
