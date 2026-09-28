package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestDeletingSubnetWithInvalidVlan(t *testing.T) {
	for _, state := range []string{"missing", "conflict", "not ready"} {
		for _, occupied := range []bool{false, true} {
			name := state + "/empty"
			if occupied {
				name = state + "/occupied"
			}
			t.Run(name, func(t *testing.T) {
				subnet := &kubeovnv1.Subnet{
					Name: "underlay", DeletionTimestamp: new(metav1.Now()),
					Finalizers: []string{util.KubeOVNControllerFinalizer},
					Spec:       kubeovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/24", Gateway: "10.0.0.1", Vlan: "vlan"},
				}
				options := &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{subnet}}
				if state != "missing" {
					options.Vlans = []*kubeovnv1.Vlan{{Name: "vlan", Spec: kubeovnv1.VlanSpec{ID: 341, Provider: "provider"}, Status: kubeovnv1.VlanStatus{Conflict: state == "conflict"}}}
					options.ProviderNetworks = []*kubeovnv1.ProviderNetwork{{Name: "provider"}}
				}
				if occupied {
					// Status starts at zero, so deletion must refresh usage from IP CRs.
					options.IPs = []*kubeovnv1.IP{{Name: "pod.ns", Spec: kubeovnv1.IPSpec{Subnet: subnet.Name, V4IPAddress: "10.0.0.2"}}}
				}
				fc, err := newFakeControllerWithOptions(t, options)
				require.NoError(t, err)
				c := fc.fakeController
				c.virtualIpsLister = kubeovnlisters.NewVipLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))
				require.NoError(t, c.handleAddOrUpdateSubnet(subnet.Name))
				updated, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Get(t.Context(), subnet.Name, metav1.GetOptions{})
				require.NoError(t, err)
				if occupied {
					require.Contains(t, updated.Finalizers, util.KubeOVNControllerFinalizer)
					require.True(t, updated.Status.V4UsingIPs.EqualInt64(1))
					// Releasing the last IP must allow deletion even if the VLAN
					// never recovers.
					require.NoError(t, c.config.KubeOvnClient.KubeovnV1().IPs().Delete(t.Context(), "pod.ns", metav1.DeleteOptions{}))
					require.Eventually(t, func() bool {
						ips, err := c.ipIndexer.ByIndex(IndexIPBySubnet, subnet.Name)
						return err == nil && len(ips) == 0
					}, time.Second, time.Millisecond)
					require.NoError(t, c.handleAddOrUpdateSubnet(subnet.Name))
					updated, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Get(t.Context(), subnet.Name, metav1.GetOptions{})
					require.NoError(t, err)
				}
				require.NotContains(t, updated.Finalizers, util.KubeOVNControllerFinalizer)
			})
		}
	}
}
