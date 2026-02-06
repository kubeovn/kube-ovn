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
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func Test_handleAddOrUpdateVpc_staticRoutes(t *testing.T) {
	t.Parallel()

	vpcName := "test-vpc"

	// Policy variables for taking pointers
	srcIPPolicy := ovnnb.LogicalRouterStaticRoutePolicySrcIP
	dstIPPolicy := ovnnb.LogicalRouterStaticRoutePolicyDstIP

	// Internal static route created directly in OVN with fine-grained labels
	internalStaticRoute := &ovnnb.LogicalRouterStaticRoute{
		UUID: "internal-static-route-uuid",
		ExternalIDs: map[string]string{
			"vendor":        util.CniTypeName,
			"controller":    "vpc",
			"resource-name": "test-vpc",
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
			"vendor":        util.CniTypeName,
			"controller":    "vpc",
			"resource-name": "test-vpc",
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

		staticRouteExternalIDs := map[string]string{
			ovs.ExternalIDVendor:       util.CniTypeName,
			ovs.ExternalIDController:   "vpc",
			ovs.ExternalIDResourceName: vpcName,
		}

		mockOvnClient.EXPECT().CreateLogicalRouter(vpcName).Return(nil)
		mockOvnClient.EXPECT().UpdateLogicalRouter(gomock.Any(), gomock.Any()).Return(nil)
		mockOvnClient.EXPECT().ListLogicalRouterStaticRoutes(vpcName, nil, nil, "", staticRouteExternalIDs).Return(existingKubeOvnRoutes, nil)
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
			staticRouteExternalIDs,
			"10.0.0.1",
		).Return(nil)
		policyExternalIDs := map[string]string{
			ovs.ExternalIDVendor:       util.CniTypeName,
			ovs.ExternalIDController:   "vpc",
			ovs.ExternalIDResourceName: vpcName,
		}
		mockOvnClient.EXPECT().DeleteLogicalRouterPolicies(vpcName, -1, policyExternalIDs).Return(nil)
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

		staticRouteExternalIDs := map[string]string{
			ovs.ExternalIDVendor:       util.CniTypeName,
			ovs.ExternalIDController:   "vpc",
			ovs.ExternalIDResourceName: vpcName,
		}

		mockOvnClient.EXPECT().CreateLogicalRouter(vpcName).Return(nil)
		mockOvnClient.EXPECT().UpdateLogicalRouter(gomock.Any(), gomock.Any()).Return(nil)
		mockOvnClient.EXPECT().ListLogicalRouterStaticRoutes(vpcName, nil, nil, "", staticRouteExternalIDs).Return(existingKubeOvnRoutes, nil)
		mockOvnClient.EXPECT().GetLogicalRouter(vpcName, false).Return(&ovnnb.LogicalRouter{
			Name: vpcName,
			Nat:  []string{},
		}, nil)
		mockOvnClient.EXPECT().DeleteLogicalRouterStaticRoute(vpcName, gomock.Any(), gomock.Any(), "10.0.0.0/24", "1.2.3.4").Return(nil)
		policyExternalIDs := map[string]string{
			ovs.ExternalIDVendor:       util.CniTypeName,
			ovs.ExternalIDController:   "vpc",
			ovs.ExternalIDResourceName: vpcName,
		}
		mockOvnClient.EXPECT().DeleteLogicalRouterPolicies(vpcName, -1, policyExternalIDs).Return(nil)
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

		staticRouteExternalIDs := map[string]string{
			ovs.ExternalIDVendor:       util.CniTypeName,
			ovs.ExternalIDController:   "vpc",
			ovs.ExternalIDResourceName: vpcName,
		}

		mockOvnClient.EXPECT().CreateLogicalRouter(vpcName).Return(nil)
		mockOvnClient.EXPECT().UpdateLogicalRouter(gomock.Any(), gomock.Any()).Return(nil)
		mockOvnClient.EXPECT().ListLogicalRouterStaticRoutes(vpcName, nil, nil, "", staticRouteExternalIDs).Return(existingKubeOvnRoutes, nil)
		mockOvnClient.EXPECT().GetLogicalRouter(vpcName, false).Return(&ovnnb.LogicalRouter{
			Name: vpcName,
			Nat:  []string{},
		}, nil)
		mockOvnClient.EXPECT().DeleteLogicalRouterStaticRoute(vpcName, gomock.Any(), gomock.Any(), "10.0.0.0/24", "1.2.3.4").Return(nil)
		mockOvnClient.EXPECT().DeleteLogicalRouterStaticRoute(vpcName, gomock.Any(), gomock.Any(), "192.168.0.0/24", "10.0.0.1").Return(nil)
		policyExternalIDs := map[string]string{
			ovs.ExternalIDVendor:       util.CniTypeName,
			ovs.ExternalIDController:   "vpc",
			ovs.ExternalIDResourceName: vpcName,
		}
		mockOvnClient.EXPECT().DeleteLogicalRouterPolicies(vpcName, -1, policyExternalIDs).Return(nil)
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

		staticRouteExternalIDs := map[string]string{
			ovs.ExternalIDVendor:       util.CniTypeName,
			ovs.ExternalIDController:   "vpc",
			ovs.ExternalIDResourceName: vpcName,
		}

		mockOvnClient.EXPECT().CreateLogicalRouter(vpcName).Return(nil)
		mockOvnClient.EXPECT().UpdateLogicalRouter(gomock.Any(), gomock.Any()).Return(nil)
		mockOvnClient.EXPECT().ListLogicalRouterStaticRoutes(vpcName, nil, nil, "", staticRouteExternalIDs).Return(nil, nil)
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
			staticRouteExternalIDs,
			"10.0.0.1",
		).Return(nil)
		policyExternalIDs := map[string]string{
			ovs.ExternalIDVendor:       util.CniTypeName,
			ovs.ExternalIDController:   "vpc",
			ovs.ExternalIDResourceName: vpcName,
		}
		mockOvnClient.EXPECT().DeleteLogicalRouterPolicies(vpcName, -1, policyExternalIDs).Return(nil)
		mockOvnClient.EXPECT().ListLogicalSwitch(gomock.Any(), gomock.Any()).Return([]ovnnb.LogicalSwitch{}, nil).AnyTimes()
		mockOvnClient.EXPECT().ListLogicalRouter(gomock.Any(), gomock.Any()).Return([]ovnnb.LogicalRouter{}, nil).AnyTimes()
		mockOvnClient.EXPECT().DeleteLogicalRouterPort(fmt.Sprintf("bfd@%s", vpcName)).Return(nil)
		mockOvnClient.EXPECT().DeleteHAChassisGroup(fmt.Sprintf("bfd@%s", vpcName)).Return(nil)
		err = ctrl.handleAddOrUpdateVpc(vpcName)
		require.NoError(t, err)
	})
}
