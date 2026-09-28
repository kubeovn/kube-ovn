package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestVlanConflictRecovery(t *testing.T) {
	for _, change := range []string{"delete", "id", "provider", "untagged"} {
		t.Run(change, func(t *testing.T) {
			remaining := &kubeovnv1.Vlan{
				Name:   "existing",
				Spec:   kubeovnv1.VlanSpec{ID: 341, Provider: "provider"},
				Status: kubeovnv1.VlanStatus{Conflict: true},
			}
			duplicate := remaining.DeepCopy()
			duplicate.Name = "duplicate"
			subnet := &kubeovnv1.Subnet{Name: "underlay", Spec: kubeovnv1.SubnetSpec{Vlan: remaining.Name}}
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Vlans:            []*kubeovnv1.Vlan{remaining, duplicate},
				Subnets:          []*kubeovnv1.Subnet{subnet},
				ProviderNetworks: []*kubeovnv1.ProviderNetwork{{Name: "provider"}, {Name: "other"}},
			})
			require.NoError(t, err)
			c := fc.fakeController
			c.vlanKeyMutex = keymutex.NewHashed(0)
			c.addVlanQueue = newTypedRateLimitingQueue[string]("test-add-vlan", nil)
			c.updateVlanQueue = newTypedRateLimitingQueue[string]("test-update-vlan", nil)
			t.Cleanup(c.addVlanQueue.ShutDown)
			t.Cleanup(c.updateVlanQueue.ShutDown)
			t.Cleanup(c.addOrUpdateSubnetQueue.ShutDown)
			vlans := c.config.KubeOvnClient.KubeovnV1().Vlans()
			if change == "delete" {
				require.NoError(t, vlans.Delete(t.Context(), duplicate.Name, metav1.DeleteOptions{}))
				require.Eventually(t, func() bool {
					_, err := c.vlansLister.Get(duplicate.Name)
					return err != nil
				}, time.Second, time.Millisecond)
				require.NoError(t, c.handleDelVlan(duplicate))
			} else {
				updated := duplicate.DeepCopy()
				switch change {
				case "id":
					updated.Spec.ID++
				case "provider":
					updated.Spec.Provider = "other"
				case "untagged":
					updated.Spec.ID = 0
				}
				_, err := vlans.Update(t.Context(), updated, metav1.UpdateOptions{})
				require.NoError(t, err)
				require.Eventually(t, func() bool {
					vlan, err := c.vlansLister.Get(duplicate.Name)
					return err == nil && vlan.Spec == updated.Spec
				}, time.Second, time.Millisecond)
				c.enqueueUpdateVlan(duplicate, updated)
				require.NoError(t, c.handleUpdateVlan(duplicate.Name))
				updated, err = vlans.Get(t.Context(), duplicate.Name, metav1.GetOptions{})
				require.NoError(t, err)
				require.False(t, updated.Status.Conflict)
			}

			// The survivor must be runnable immediately, without waiting for its
			// previous conflict retry's exponential backoff.
			require.Equal(t, 1, c.addVlanQueue.Len())
			key, shutdown := c.addVlanQueue.Get()
			require.False(t, shutdown)
			require.Equal(t, remaining.Name, key)
			require.NoError(t, c.handleAddVlan(key))
			c.addVlanQueue.Done(key)
			updated, err := vlans.Get(t.Context(), remaining.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.False(t, updated.Status.Conflict)
			require.Equal(t, 1, c.addOrUpdateSubnetQueue.Len())
		})
	}
}

func TestVlanUpdateRequeuesSubnet(t *testing.T) {
	vlan := &kubeovnv1.Vlan{Name: "vlan", Spec: kubeovnv1.VlanSpec{ID: 341, Provider: "provider"}, Status: kubeovnv1.VlanStatus{Conflict: true}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vlans:            []*kubeovnv1.Vlan{vlan},
		Subnets:          []*kubeovnv1.Subnet{{Name: "underlay", Spec: kubeovnv1.SubnetSpec{Vlan: vlan.Name}}},
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{{Name: "provider"}},
	})
	require.NoError(t, err)
	c := fc.fakeController
	c.vlanKeyMutex = keymutex.NewHashed(0)
	t.Cleanup(c.addOrUpdateSubnetQueue.ShutDown)
	fc.mockOvnClient.EXPECT().SetLogicalSwitchPortVlanTag("localnet.underlay", 341).Return(nil)
	require.NoError(t, c.handleUpdateVlan(vlan.Name))
	require.Equal(t, 1, c.addOrUpdateSubnetQueue.Len())
}

func TestVlanSpecChangeRequeuesConflictGroups(t *testing.T) {
	old := &kubeovnv1.Vlan{Name: "changed", Spec: kubeovnv1.VlanSpec{ID: 341, Provider: "old-provider"}}
	updated := old.DeepCopy()
	updated.Spec = kubeovnv1.VlanSpec{ID: 342, Provider: "new-provider"}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vlans: []*kubeovnv1.Vlan{
			updated,
			{Name: "old-peer", Spec: old.Spec},
			{Name: "new-peer", Spec: updated.Spec},
			{Name: "different-id", Spec: kubeovnv1.VlanSpec{ID: 343, Provider: old.Spec.Provider}},
			{Name: "different-provider", Spec: kubeovnv1.VlanSpec{ID: old.Spec.ID, Provider: "unrelated"}},
		},
	})
	require.NoError(t, err)
	c := fc.fakeController
	c.addVlanQueue = newTypedRateLimitingQueue[string]("test-add-vlan", nil)
	c.updateVlanQueue = newTypedRateLimitingQueue[string]("test-update-vlan", nil)
	t.Cleanup(c.addVlanQueue.ShutDown)
	t.Cleanup(c.updateVlanQueue.ShutDown)
	c.enqueueUpdateVlan(old, updated)
	require.Equal(t, 2, c.addVlanQueue.Len())
	var queued []string
	for c.addVlanQueue.Len() > 0 {
		key, shutdown := c.addVlanQueue.Get()
		require.False(t, shutdown)
		queued = append(queued, key)
		c.addVlanQueue.Done(key)
	}
	require.ElementsMatch(t, []string{"old-peer", "new-peer"}, queued)

	statusUpdate := updated.DeepCopy()
	statusUpdate.Status.Conflict = true
	c.enqueueUpdateVlan(updated, statusUpdate)
	require.Zero(t, c.addVlanQueue.Len(), "status changes must not make conflicting peers enqueue each other")
}
