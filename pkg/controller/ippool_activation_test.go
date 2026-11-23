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
	for _, tt := range []struct {
		name    string
		fail    bool
		applied bool
	}{
		{name: "success"},
		{name: "status failure", fail: true},
		{name: "response lost after write", fail: true, applied: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
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
			writeFails := tt.fail
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
				// Reservations remain unavailable without holding allocation locks during IO.
				available, _, _, _, _, _, _, _ := c.ipam.IPPoolStatistics(subnet.Name, pool.Name)
				require.Zero(t, available.Int64())
				if tt.applied {
					updated := pool.DeepCopy()
					require.NoError(t, json.Unmarshal(patch.Status["ips"], &updated.Status.IPs))
					err := c.config.KubeOvnClient.(*kubeovnfake.Clientset).Tracker().Update(kubeovnv1.SchemeGroupVersion.WithResource("ippools"), updated, "")
					require.NoError(t, err)
				}
				if writeFails {
					if tt.applied {
						return true, nil, context.DeadlineExceeded
					}
					return true, nil, patchErr
				}
				return false, nil, nil
			})
			err = c.handleAddOrUpdateIPPool(pool.Name)
			require.True(t, patched)
			if tt.fail && !tt.applied {
				require.ErrorIs(t, err, patchErr)
			} else {
				require.NoError(t, err)
			}
			got, err := c.config.KubeOvnClient.KubeovnV1().IPPools().Get(context.Background(), pool.Name, metav1.GetOptions{})
			require.NoError(t, err)
			expected := []string{"10.0.0.1", "10.0.0.2"}
			if tt.fail && !tt.applied {
				expected = expected[:1]
			}
			require.Equal(t, expected, got.Status.IPs)
			// Reconstruct exactly what InitIPAM restores after a process exit.
			restored := ipam.NewIPAM()
			require.NoError(t, restored.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, "", nil))
			require.NoError(t, restored.AddOrUpdateIPPool(subnet.Name, pool.Name, ippoolIPs(got)))
			if tt.fail && !tt.applied {
				_, _, _, allocErr := c.ipam.GetRandomAddress("pod", "nic", nil, subnet.Name, pool.Name, nil, true)
				require.ErrorIs(t, allocErr, ipam.ErrNoAvailable)
			} else {
				require.Equal(t, c.ipam.Subnets[subnet.Name].IPPools[pool.Name].V4IPs.String(), restored.Subnets[subnet.Name].IPPools[pool.Name].V4IPs.String())
			}
			if tt.fail && !tt.applied {
				writeFails = false
				fc.mockOvnClient.EXPECT().DeleteAddressSet(pool.Name).Return(nil)
				require.NoError(t, c.handleAddOrUpdateIPPool(pool.Name))
				got, err = c.config.KubeOvnClient.KubeovnV1().IPPools().Get(context.Background(), pool.Name, metav1.GetOptions{})
				require.NoError(t, err)
				require.Equal(t, []string{"10.0.0.1", "10.0.0.2"}, got.Status.IPs)
			}
		})
	}
}

func TestPatchIPPoolIPsReadbackFailure(t *testing.T) {
	pool := &kubeovnv1.IPPool{Name: "pool", Status: kubeovnv1.IPPoolStatus{IPs: []string{"10.0.0.1"}}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{IPPools: []*kubeovnv1.IPPool{pool}})
	require.NoError(t, err)
	client := fc.fakeController.config.KubeOvnClient.(*kubeovnfake.Clientset)
	patchErr, readErr := errors.New("patch failed"), errors.New("read failed")
	client.PrependReactor("patch", "ippools", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, patchErr
	})
	client.PrependReactor("get", "ippools", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, readErr
	})
	err = fc.fakeController.patchIPPoolIPs(pool, []string{"10.0.0.1", "10.0.0.2"})
	require.ErrorIs(t, err, patchErr)
	require.ErrorIs(t, err, readErr)
	require.Equal(t, []string{"10.0.0.1"}, pool.Status.IPs)
}
