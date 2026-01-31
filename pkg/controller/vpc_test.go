package controller

import (
	"context"
	"fmt"
	"testing"

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
		mockOvnClient.EXPECT().ClearLogicalRouterPolicy(vpcName).Return(nil)
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
		mockOvnClient.EXPECT().ClearLogicalRouterPolicy(vpcName).Return(nil)
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
		mockOvnClient.EXPECT().ClearLogicalRouterPolicy(vpcName).Return(nil)
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
		mockOvnClient.EXPECT().ClearLogicalRouterPolicy(vpcName).Return(nil)
		mockOvnClient.EXPECT().ListLogicalSwitch(gomock.Any(), gomock.Any()).Return([]ovnnb.LogicalSwitch{}, nil).AnyTimes()
		mockOvnClient.EXPECT().ListLogicalRouter(gomock.Any(), gomock.Any()).Return([]ovnnb.LogicalRouter{}, nil).AnyTimes()
		mockOvnClient.EXPECT().DeleteLogicalRouterPort(fmt.Sprintf("bfd@%s", vpcName)).Return(nil)
		mockOvnClient.EXPECT().DeleteHAChassisGroup(fmt.Sprintf("bfd@%s", vpcName)).Return(nil)
		err = ctrl.handleAddOrUpdateVpc(vpcName)
		require.NoError(t, err)
	})
}

func Test_diffPolicyRouteWithLogical(t *testing.T) {
	t.Parallel()

	t.Run("detect externalIDs changes", func(t *testing.T) {
		exists := []*ovnnb.LogicalRouterPolicy{
			{
				Priority: 100,
				Match:    "ip4.src == 10.0.0.0/24",
				Action:   string(kubeovnv1.PolicyRouteActionReroute),
				ExternalIDs: map[string]string{
					"vendor": util.CniTypeName,
					"source": "old-source",
				},
			},
		}
		target := []*kubeovnv1.PolicyRoute{
			{
				Priority: 100,
				Match:    "ip4.src == 10.0.0.0/24",
				Action:   kubeovnv1.PolicyRouteActionReroute,
				ExternalIDs: map[string]string{
					"source": "new-source",
				},
			},
		}

		dels, adds := diffPolicyRouteWithLogical(exists, target)

		require.Empty(t, dels)
		// externalIDs changed, so it should be in adds for upsert
		require.Len(t, adds, 1)
		require.Equal(t, "ip4.src == 10.0.0.0/24", adds[0].Match)
		require.Equal(t, "new-source", adds[0].ExternalIDs["source"])
	})

	t.Run("no change when externalIDs match", func(t *testing.T) {
		exists := []*ovnnb.LogicalRouterPolicy{
			{
				Priority: 100,
				Match:    "ip4.src == 10.0.0.0/24",
				Action:   string(kubeovnv1.PolicyRouteActionReroute),
				ExternalIDs: map[string]string{
					"vendor": util.CniTypeName,
					"source": "my-source",
				},
			},
		}
		target := []*kubeovnv1.PolicyRoute{
			{
				Priority: 100,
				Match:    "ip4.src == 10.0.0.0/24",
				Action:   kubeovnv1.PolicyRouteActionReroute,
				ExternalIDs: map[string]string{
					"source": "my-source",
				},
			},
		}

		dels, adds := diffPolicyRouteWithLogical(exists, target)

		require.Empty(t, dels)
		require.Empty(t, adds)
	})

	t.Run("add new policy route with externalIDs", func(t *testing.T) {
		exists := []*ovnnb.LogicalRouterPolicy{}
		target := []*kubeovnv1.PolicyRoute{
			{
				Priority: 100,
				Match:    "ip4.src == 10.0.0.0/24",
				Action:   kubeovnv1.PolicyRouteActionReroute,
				ExternalIDs: map[string]string{
					"source": "my-source",
				},
			},
		}

		dels, adds := diffPolicyRouteWithLogical(exists, target)

		require.Empty(t, dels)
		require.Len(t, adds, 1)
		require.Equal(t, "my-source", adds[0].ExternalIDs["source"])
	})

	t.Run("delete policy route", func(t *testing.T) {
		exists := []*ovnnb.LogicalRouterPolicy{
			{
				Priority: 100,
				Match:    "ip4.src == 10.0.0.0/24",
				Action:   string(kubeovnv1.PolicyRouteActionReroute),
				ExternalIDs: map[string]string{
					"vendor": util.CniTypeName,
				},
			},
		}
		target := []*kubeovnv1.PolicyRoute{}

		dels, adds := diffPolicyRouteWithLogical(exists, target)

		require.Len(t, dels, 1)
		require.Empty(t, adds)
	})

	t.Run("policy route without externalIDs uses default", func(t *testing.T) {
		exists := []*ovnnb.LogicalRouterPolicy{
			{
				Priority: 100,
				Match:    "ip4.src == 10.0.0.0/24",
				Action:   string(kubeovnv1.PolicyRouteActionReroute),
				ExternalIDs: map[string]string{
					"vendor": util.CniTypeName,
				},
			},
		}
		// Target has no externalIDs, should match existing (vendor: kube-ovn)
		target := []*kubeovnv1.PolicyRoute{
			{
				Priority: 100,
				Match:    "ip4.src == 10.0.0.0/24",
				Action:   kubeovnv1.PolicyRouteActionReroute,
			},
		}

		dels, adds := diffPolicyRouteWithLogical(exists, target)

		require.Empty(t, dels)
		require.Empty(t, adds) // No change because merged externalIDs match
	})

	t.Run("vendor key cannot be overwritten by user", func(t *testing.T) {
		exists := []*ovnnb.LogicalRouterPolicy{
			{
				Priority: 100,
				Match:    "ip4.src == 10.0.0.0/24",
				Action:   string(kubeovnv1.PolicyRouteActionReroute),
				ExternalIDs: map[string]string{
					"vendor": util.CniTypeName,
				},
			},
		}
		// User tries to override vendor key - should be ignored
		target := []*kubeovnv1.PolicyRoute{
			{
				Priority: 100,
				Match:    "ip4.src == 10.0.0.0/24",
				Action:   kubeovnv1.PolicyRouteActionReroute,
				ExternalIDs: map[string]string{
					"vendor": "custom-vendor", // This should be ignored
				},
			},
		}

		dels, adds := diffPolicyRouteWithLogical(exists, target)

		require.Empty(t, dels)
		require.Empty(t, adds) // No change because vendor is always kube-ovn
	})
}

func Test_diffPolicyRouteWithExisted(t *testing.T) {
	t.Parallel()

	t.Run("detect externalIDs changes", func(t *testing.T) {
		exists := []*kubeovnv1.PolicyRoute{
			{
				Priority: 100,
				Match:    "ip4.src == 10.0.0.0/24",
				Action:   kubeovnv1.PolicyRouteActionReroute,
				ExternalIDs: map[string]string{
					"source": "old-source",
				},
			},
		}
		target := []*kubeovnv1.PolicyRoute{
			{
				Priority: 100,
				Match:    "ip4.src == 10.0.0.0/24",
				Action:   kubeovnv1.PolicyRouteActionReroute,
				ExternalIDs: map[string]string{
					"source": "new-source",
				},
			},
		}

		dels, adds := diffPolicyRouteWithExisted(exists, target)

		require.Empty(t, dels)
		// externalIDs changed, so it should be in adds for upsert
		require.Len(t, adds, 1)
		require.Equal(t, "new-source", adds[0].ExternalIDs["source"])
	})

	t.Run("no change when externalIDs match", func(t *testing.T) {
		exists := []*kubeovnv1.PolicyRoute{
			{
				Priority: 100,
				Match:    "ip4.src == 10.0.0.0/24",
				Action:   kubeovnv1.PolicyRouteActionReroute,
				ExternalIDs: map[string]string{
					"source": "my-source",
				},
			},
		}
		target := []*kubeovnv1.PolicyRoute{
			{
				Priority: 100,
				Match:    "ip4.src == 10.0.0.0/24",
				Action:   kubeovnv1.PolicyRouteActionReroute,
				ExternalIDs: map[string]string{
					"source": "my-source",
				},
			},
		}

		dels, adds := diffPolicyRouteWithExisted(exists, target)

		require.Empty(t, dels)
		require.Empty(t, adds)
	})

	t.Run("add new policy route", func(t *testing.T) {
		exists := []*kubeovnv1.PolicyRoute{}
		target := []*kubeovnv1.PolicyRoute{
			{
				Priority: 100,
				Match:    "ip4.src == 10.0.0.0/24",
				Action:   kubeovnv1.PolicyRouteActionReroute,
				ExternalIDs: map[string]string{
					"source": "my-source",
				},
			},
		}

		dels, adds := diffPolicyRouteWithExisted(exists, target)

		require.Empty(t, dels)
		require.Len(t, adds, 1)
	})

	t.Run("delete policy route", func(t *testing.T) {
		exists := []*kubeovnv1.PolicyRoute{
			{
				Priority: 100,
				Match:    "ip4.src == 10.0.0.0/24",
				Action:   kubeovnv1.PolicyRouteActionReroute,
			},
		}
		target := []*kubeovnv1.PolicyRoute{}

		dels, adds := diffPolicyRouteWithExisted(exists, target)

		require.Len(t, dels, 1)
		require.Empty(t, adds)
	})

	t.Run("vendor key cannot be overwritten by user", func(t *testing.T) {
		exists := []*kubeovnv1.PolicyRoute{
			{
				Priority: 100,
				Match:    "ip4.src == 10.0.0.0/24",
				Action:   kubeovnv1.PolicyRouteActionReroute,
				ExternalIDs: map[string]string{
					"source": "my-source",
				},
			},
		}
		// User tries to override vendor key - should be ignored
		target := []*kubeovnv1.PolicyRoute{
			{
				Priority: 100,
				Match:    "ip4.src == 10.0.0.0/24",
				Action:   kubeovnv1.PolicyRouteActionReroute,
				ExternalIDs: map[string]string{
					"vendor": "custom-vendor", // This should be ignored
					"source": "my-source",
				},
			},
		}

		dels, adds := diffPolicyRouteWithExisted(exists, target)

		require.Empty(t, dels)
		require.Empty(t, adds) // No change because vendor is always kube-ovn
	})
}
