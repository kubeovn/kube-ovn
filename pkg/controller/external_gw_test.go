package controller

import (
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// Keep the lister snapshot frozen while API writes advance, as can happen before
// the informer observes a concurrent external gateway or VPC reconciliation.
func freezeVpcLister(t *testing.T, ctrl *Controller, vpc *kubeovnv1.Vpc) {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(vpc.DeepCopy()))
	ctrl.vpcsLister = kubeovnlister.NewVpcLister(indexer)
}

func TestUpdateDefaultVpcExternalPreservesConcurrentStatus(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			t.Parallel()
			cachedVpc := &kubeovnv1.Vpc{
				Name: util.DefaultVpc,
				Spec: kubeovnv1.VpcSpec{EnableExternal: !enabled},
				Status: kubeovnv1.VpcStatus{
					EnableExternal: !enabled,
					Subnets:        []string{"old-subnet"},
				},
			}
			liveVpc := cachedVpc.DeepCopy()
			liveVpc.Spec.Namespaces = []string{"new-namespace"}
			liveVpc.Status.Subnets = []string{"new-subnet"}
			liveVpc.Status.TCPLoadBalancer = "new-load-balancer"
			liveVpc.Status.BFDPort.Name = "new-bfd-port"
			ctrl := &Controller{config: &Configuration{
				ClusterRouter: util.DefaultVpc,
				KubeOvnClient: kubeovnfake.NewSimpleClientset(liveVpc),
			}}
			freezeVpcLister(t, ctrl, cachedVpc)

			require.NoError(t, ctrl.updateDefaultVpcExternal(enabled))

			got, err := ctrl.config.KubeOvnClient.KubeovnV1().Vpcs().Get(t.Context(), util.DefaultVpc, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, enabled, got.Spec.EnableExternal)
			require.Equal(t, enabled, got.Status.EnableExternal)
			liveVpc.Status.EnableExternal = enabled
			require.Equal(t, liveVpc.Status, got.Status)
			liveVpc.Spec.EnableExternal = enabled
			require.Equal(t, liveVpc.Spec, got.Spec)
		})
	}
}

func TestResyncExternalGatewayRepairsVpcStatus(t *testing.T) {
	oldEnabled, oldConfig := exGwEnabled, lastExGwCM
	t.Cleanup(func() { exGwEnabled, lastExGwCM = oldEnabled, oldConfig })

	for _, enabled := range []bool{false, true} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			vpc := &kubeovnv1.Vpc{
				Name:   util.DefaultVpc,
				Spec:   kubeovnv1.VpcSpec{EnableExternal: enabled},
				Status: kubeovnv1.VpcStatus{EnableExternal: !enabled},
			}
			cm := &corev1.ConfigMap{
				Name:      util.ExternalGatewayConfig,
				Namespace: metav1.NamespaceSystem,
				Data:      map[string]string{"enable-external-gw": strconv.FormatBool(enabled)},
			}
			opts := &FakeControllerOptions{Vpcs: []*kubeovnv1.Vpc{vpc}}
			if enabled {
				opts.ConfigMaps = []*corev1.ConfigMap{cm}
			}
			fc, err := newFakeControllerWithOptions(t, opts)
			require.NoError(t, err)
			ctrl := fc.fakeController
			ctrl.config.ExternalGatewayConfigNS = cm.Namespace
			freezeVpcLister(t, ctrl, vpc)
			exGwEnabled, lastExGwCM = strconv.FormatBool(enabled), cm.Data
			attempts := 0
			client := ctrl.config.KubeOvnClient.(*kubeovnfake.Clientset)
			client.PrependReactor("patch", "vpcs", func(action ktesting.Action) (bool, runtime.Object, error) {
				if action.GetSubresource() == "status" {
					attempts++
					if attempts == 1 {
						return true, nil, errors.New("temporary status update failure")
					}
				}
				return false, nil, nil
			})

			// No OVN operations are expected when only the VPC status has drifted.
			ctrl.resyncExternalGateway()
			require.Equal(t, 1, attempts)
			ctrl.resyncExternalGateway()
			require.Equal(t, 2, attempts)

			got, err := ctrl.config.KubeOvnClient.KubeovnV1().Vpcs().Get(t.Context(), vpc.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, enabled, got.Spec.EnableExternal)
			require.Equal(t, enabled, got.Status.EnableExternal)
		})
	}
}
