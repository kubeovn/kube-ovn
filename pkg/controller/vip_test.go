package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestHandleAddVirtualIPSwitchLBRuleUsesOwnMac(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                 string
		refreshCache         bool
		wantVirtualPortCalls int
	}{
		{name: "stale cache"},
		{name: "refreshed cache", refreshCache: true, wantVirtualPortCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			subnet := &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock: "10.0.0.0/24",
					Gateway:   "10.0.0.1",
					Protocol:  kubeovnv1.ProtocolIPv4,
					Provider:  util.OvnProvider,
					Vpc:       "test-vpc",
				},
			}
			vip := &kubeovnv1.Vip{
				ObjectMeta: metav1.ObjectMeta{Name: "test-switch-lb-vip"},
				Spec: kubeovnv1.VipSpec{
					Namespace: "default",
					Subnet:    subnet.Name,
					Type:      util.SwitchLBRuleVip,
					V4ip:      "10.0.0.50",
				},
			}

			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Subnets: []*kubeovnv1.Subnet{subnet},
				Vips:    []*kubeovnv1.Vip{vip},
			})
			require.NoError(t, err)
			ctrl := fc.fakeController
			require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))

			// Isolate the VIP lister from the background informer so both cache
			// timings after the API update are deterministic.
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			require.NoError(t, indexer.Add(vip.DeepCopy()))
			ctrl.virtualIpsLister = kubeovnlisters.NewVipLister(indexer)
			if tc.refreshCache {
				ctrl.config.KubeOvnClient.(*kubeovnfake.Clientset).PrependReactor("update", "vips",
					func(action clienttesting.Action) (bool, runtime.Object, error) {
						updated := action.(clienttesting.UpdateAction).GetObject()
						require.NoError(t, indexer.Update(updated.DeepCopyObject()))
						// Let the normal fake tracker persist the API update as well.
						return false, nil, nil
					})
			}

			portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
			fc.mockOvnClient.EXPECT().
				CreateLogicalSwitchPort(subnet.Name, portName, vip.Spec.V4ip, gomock.Not(""), vip.Name, vip.Spec.Namespace, false, "", "", false, nil, subnet.Spec.Vpc).
				Return(nil)
			fc.mockOvnClient.EXPECT().SetLogicalSwitchPortArpProxy(portName, true).Return(nil)
			fc.mockOvnClient.EXPECT().
				CreateVirtualLogicalSwitchPort(vip.Name, subnet.Name, vip.Spec.V4ip).
				Return(nil).
				Times(tc.wantVirtualPortCalls)

			require.NoError(t, ctrl.handleAddVirtualIP(vip.Name))

			got, err := ctrl.config.KubeOvnClient.KubeovnV1().Vips().Get(t.Context(), vip.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.NotEmpty(t, got.Status.Mac)
		})
	}
}
