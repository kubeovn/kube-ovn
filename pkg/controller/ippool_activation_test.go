package controller

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
)

func TestIPPoolActivationPersistence(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "status failure"}[fail], func(t *testing.T) {
			subnet := &kubeovnv1.Subnet{Name: "subnet", Spec: kubeovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/29"}}
			pool := &kubeovnv1.IPPool{Name: "pool", Spec: kubeovnv1.IPPoolSpec{Subnet: subnet.Name, IPCount: 2}, Status: kubeovnv1.IPPoolStatus{IPs: []string{"10.0.0.1"}}}
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{subnet}, IPPools: []*kubeovnv1.IPPool{pool}})
			require.NoError(t, err)
			c := fc.fakeController
			c.ippoolKeyMutex = keymutex.NewHashed(97)
			require.NoError(t, c.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, "", nil))
			require.NoError(t, c.ipam.AddOrUpdateIPPool(subnet.Name, pool.Name, pool.Status.IPs))
			fc.mockOvnClient.EXPECT().DeleteAddressSet(pool.Name).Return(nil)
			patchErr := errors.New("status write failed")
			patched := false
			c.config.KubeOvnClient.(*kubeovnfake.Clientset).PrependReactor("patch", "ippools", func(action ktesting.Action) (bool, runtime.Object, error) {
				if action.GetSubresource() != "status" {
					return false, nil, nil
				}
				var patch struct {
					Status map[string]json.RawMessage `json:"status"`
				}
				require.NoError(t, json.Unmarshal(action.(ktesting.PatchAction).GetPatch(), &patch))
				if _, ok := patch.Status["ips"]; !ok {
					return false, nil, nil
				}
				patched = true
				// Persistence runs before the new member is visible to allocation.
				require.Equal(t, "10.0.0.1", c.ipam.Subnets[subnet.Name].IPPools[pool.Name].V4IPs.String())
				if fail {
					return true, nil, patchErr
				}
				return false, nil, nil
			})
			err = c.handleAddOrUpdateIPPool(pool.Name)
			require.True(t, patched)
			if fail {
				require.ErrorIs(t, err, patchErr)
			} else {
				require.NoError(t, err)
			}
			got, err := c.config.KubeOvnClient.KubeovnV1().IPPools().Get(context.Background(), pool.Name, metav1.GetOptions{})
			require.NoError(t, err)
			expected := []string{"10.0.0.1", "10.0.0.2"}
			if fail {
				expected = expected[:1]
			}
			require.Equal(t, expected, got.Status.IPs)
			// Reconstruct exactly what InitIPAM restores after a process exit.
			restored := ipam.NewIPAM()
			require.NoError(t, restored.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, "", nil))
			require.NoError(t, restored.AddOrUpdateIPPool(subnet.Name, pool.Name, ippoolIPs(got)))
			require.Equal(t, c.ipam.Subnets[subnet.Name].IPPools[pool.Name].V4IPs.String(), restored.Subnets[subnet.Name].IPPools[pool.Name].V4IPs.String())
		})
	}
}
