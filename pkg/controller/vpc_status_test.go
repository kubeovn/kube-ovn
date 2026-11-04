package controller

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestVpcReconciliationPreservesExternalGatewayStatus(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"cached-read", "status-reconcile", "bfd-reconcile"} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enabled=%t", stage, enabled), func(t *testing.T) {
				t.Parallel()
				vpc := &kubeovnv1.Vpc{
					Name:       util.DefaultVpc,
					Finalizers: []string{util.KubeOVNControllerFinalizer},
					Spec:       kubeovnv1.VpcSpec{EnableExternal: !enabled},
					Status:     kubeovnv1.VpcStatus{EnableExternal: !enabled},
				}
				fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Vpcs: []*kubeovnv1.Vpc{vpc}})
				require.NoError(t, err)
				ctrl := fc.fakeController
				ctrl.config.EnableEipSnat = true
				// Keep this test focused on the VPC status writes, without join routes.
				ctrl.addOrUpdateVpcQueue = workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
				t.Cleanup(ctrl.addOrUpdateVpcQueue.ShutDown)
				freezeVpcLister(t, ctrl, vpc)

				// The external gateway writes between the VPC worker's cached read
				// and its status write, reproducing the failed NAT E2E interleaving.
				updateExternal := func() {
					ctrl.config.ClusterRouter = vpc.Name
					require.NoError(t, ctrl.updateDefaultVpcExternal(enabled))
					ctrl.config.ClusterRouter = "other-router"
				}
				if stage != "bfd-reconcile" {
					updateExternal()
				}
				ctrl.config.ClusterRouter = "other-router"

				if stage == "status-reconcile" {
					require.NoError(t, ctrl.handleUpdateVpcStatus(vpc.Name))
				} else {
					mock := fc.mockOvnClient
					mock.EXPECT().CreateLogicalRouter(vpc.Name).Return(nil)
					mock.EXPECT().GetLogicalRouter(vpc.Name, false).Return(&ovnnb.LogicalRouter{Name: vpc.Name}, nil)
					mock.EXPECT().UpdateLogicalRouter(gomock.Any(), gomock.Any()).Return(nil)
					mock.EXPECT().ListLogicalRouterStaticRoutes(vpc.Name, nil, nil, "", gomock.Any()).Return(nil, nil)
					mock.EXPECT().ListLogicalRouterPolicies(vpc.Name, -1, nil, true).Return(nil, nil)
					mock.EXPECT().DeleteLogicalRouterPort("bfd@" + vpc.Name).
						DoAndReturn(func(string) error {
							if stage == "bfd-reconcile" {
								updateExternal()
								// A concurrent reconciliation can update unrelated fields too.
								current, err := ctrl.config.KubeOvnClient.KubeovnV1().Vpcs().Get(t.Context(), vpc.Name, metav1.GetOptions{})
								require.NoError(t, err)
								current.Status.TCPLoadBalancer = "concurrent-load-balancer"
								_, err = ctrl.config.KubeOvnClient.KubeovnV1().Vpcs().UpdateStatus(t.Context(), current, metav1.UpdateOptions{})
								require.NoError(t, err)
							}
							return nil
						})
					mock.EXPECT().DeleteHAChassisGroup("bfd@" + vpc.Name).Return(nil)
					require.NoError(t, ctrl.handleAddOrUpdateVpc(vpc.Name))
				}

				got, err := ctrl.config.KubeOvnClient.KubeovnV1().Vpcs().Get(t.Context(), vpc.Name, metav1.GetOptions{})
				require.NoError(t, err)
				require.Equal(t, enabled, got.Spec.EnableExternal)
				require.Equal(t, enabled, got.Status.EnableExternal)
				require.Empty(t, got.Status.Subnets)
				if stage == "bfd-reconcile" {
					require.Equal(t, "concurrent-load-balancer", got.Status.TCPLoadBalancer)
				}
			})
		}
	}
}

func TestVpcStatusHelpersPreserveConcurrentStatus(t *testing.T) {
	t.Parallel()
	for _, external := range []bool{false, true} {
		t.Run(fmt.Sprintf("external=%t", external), func(t *testing.T) {
			t.Parallel()
			cachedVpc := &kubeovnv1.Vpc{
				Name: "custom-vpc",
				Spec: kubeovnv1.VpcSpec{
					EnableExternal:       true,
					EnableBfd:            true,
					ExtraExternalSubnets: []string{"extra"},
				},
			}
			liveVpc := cachedVpc.DeepCopy()
			liveVpc.Status.Subnets = []string{"new-subnet"}
			liveVpc.Status.TCPLoadBalancer = "new-load-balancer"
			liveVpc.Status.BFDPort.Name = "new-bfd-port"
			ctrl := &Controller{config: &Configuration{KubeOvnClient: kubeovnfake.NewSimpleClientset(liveVpc)}}
			freezeVpcLister(t, ctrl, cachedVpc)

			if external {
				require.NoError(t, ctrl.updateVpcExternalStatus(cachedVpc.Name))
				liveVpc.Status.ExtraExternalSubnets = []string{"extra"}
			} else {
				require.NoError(t, ctrl.patchVpcBfdStatus(cachedVpc.Name))
			}

			got, err := ctrl.config.KubeOvnClient.KubeovnV1().Vpcs().Get(t.Context(), cachedVpc.Name, metav1.GetOptions{})
			require.NoError(t, err)
			liveVpc.Status.EnableExternal = true
			liveVpc.Status.EnableBfd = true
			require.Equal(t, liveVpc.Status, got.Status)
		})
	}
}
