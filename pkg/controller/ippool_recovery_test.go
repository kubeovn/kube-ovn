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
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/keymutex"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	fake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	listers "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestIPPoolStaleSelectionStillPersists(t *testing.T) {
	subnet := &apiv1.Subnet{Name: "subnet", Spec: apiv1.SubnetSpec{CIDRBlock: "10.0.0.0/29"}}
	pool := &apiv1.IPPool{Name: "pool", Finalizers: []string{util.KubeOVNControllerFinalizer}, Spec: apiv1.IPPoolSpec{Subnet: subnet.Name, IPCount: 2}, Status: apiv1.IPPoolStatus{IPs: []string{"10.0.0.1"}}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*apiv1.Subnet{subnet}, IPPools: []*apiv1.IPPool{pool}})
	require.NoError(t, err)
	c := fc.fakeController
	c.ippoolKeyMutex = keymutex.NewHashed(97)
	index := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, index.Add(pool.DeepCopy()))
	c.ippoolLister = listers.NewIPPoolLister(index)
	require.NoError(t, c.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, "", nil))
	require.NoError(t, c.ipam.AddOrUpdateIPPool(subnet.Name, pool.Name, pool.Status.IPs))
	fc.mockOvnClient.EXPECT().DeleteAddressSet(pool.Name).Return(nil).Times(3)
	client := c.config.KubeOvnClient.(*fake.Clientset)
	interleaved := false
	client.PrependReactor("patch", "ippools", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "status" || interleaved {
			return false, nil, nil
		}
		var patch struct {
			Status map[string]json.RawMessage `json:"status"`
		}
		require.NoError(t, json.Unmarshal(action.(ktesting.PatchAction).GetPatch(), &patch))
		if _, ok := patch.Status["ips"]; !ok {
			return false, nil, nil
		}
		// The user shrinks while the grow is in flight. Its older snapshot arrives first.
		shrink := pool.DeepCopy()
		shrink.Spec.IPCount = 1
		require.NoError(t, client.Tracker().Update(apiv1.SchemeGroupVersion.WithResource("ippools"), shrink, ""))
		require.NoError(t, index.Update(shrink))
		interleaved = true
		return false, nil, nil
	})
	require.NoError(t, c.handleAddOrUpdateIPPool(pool.Name))
	require.True(t, interleaved)
	grown, err := client.KubeovnV1().IPPools().Get(context.Background(), pool.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, int32(1), grown.Spec.IPCount)
	require.Equal(t, []string{"10.0.0.1", "10.0.0.2"}, grown.Status.IPs)
	require.NoError(t, c.handleAddOrUpdateIPPool(pool.Name))
	got, err := client.KubeovnV1().IPPools().Get(context.Background(), pool.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.1"}, got.Status.IPs)
	require.Equal(t, "10.0.0.1", c.ipam.Subnets[subnet.Name].IPPools[pool.Name].V4IPs.String())
	c.ipam = ipam.NewIPAM()
	require.NoError(t, c.InitIPAM())
	require.Equal(t, "10.0.0.1", c.ipam.Subnets[subnet.Name].IPPools[pool.Name].V4IPs.String())
}

func TestIPPoolRetryKeepsDurableMembershipOrder(t *testing.T) {
	subnet := &apiv1.Subnet{Name: "subnet", Spec: apiv1.SubnetSpec{CIDRBlock: "10.0.0.0/27"}}
	pool := &apiv1.IPPool{Name: "pool", Finalizers: []string{util.KubeOVNControllerFinalizer}, Spec: apiv1.IPPoolSpec{Subnet: subnet.Name, IPCount: 2}, Status: apiv1.IPPoolStatus{IPs: []string{"10.0.0.20", "10.0.0.1"}}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*apiv1.Subnet{subnet}, IPPools: []*apiv1.IPPool{pool}})
	require.NoError(t, err)
	c := fc.fakeController
	c.ippoolKeyMutex = keymutex.NewHashed(97)
	stale := pool.DeepCopy()
	stale.Status.IPs = nil
	index := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, index.Add(stale))
	c.ippoolLister = listers.NewIPPoolLister(index)
	require.NoError(t, c.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, "", nil))
	require.NoError(t, c.ipam.AddOrUpdateIPPool(subnet.Name, pool.Name, []string{"10.0.0.20"}))
	failed := errors.New("response lost")
	require.ErrorIs(t, c.ipam.ActivateIPPoolIPs(subnet.Name, pool.Name, pool.Status.IPs, 2, 0, func() error { return failed }), failed)
	fc.mockOvnClient.EXPECT().DeleteAddressSet(pool.Name).Return(nil)
	require.NoError(t, c.handleAddOrUpdateIPPool(pool.Name))
	got, err := c.config.KubeOvnClient.KubeovnV1().IPPools().Get(context.Background(), pool.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, pool.Status.IPs, got.Status.IPs, "range sorting must not replace the durable grow/shrink order")
}

func TestIPPoolCommittedWriteReadbackUnavailable(t *testing.T) {
	subnet := &apiv1.Subnet{Name: "subnet", Spec: apiv1.SubnetSpec{CIDRBlock: "10.0.0.0/29"}}
	pool := &apiv1.IPPool{Name: "pool", Finalizers: []string{util.KubeOVNControllerFinalizer}, Spec: apiv1.IPPoolSpec{Subnet: subnet.Name, IPCount: 2}, Status: apiv1.IPPoolStatus{IPs: []string{"10.0.0.1"}}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*apiv1.Subnet{subnet}, IPPools: []*apiv1.IPPool{pool}})
	require.NoError(t, err)
	c := fc.fakeController
	c.ippoolKeyMutex = keymutex.NewHashed(97)
	require.NoError(t, c.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, "", nil))
	require.NoError(t, c.ipam.AddOrUpdateIPPool(subnet.Name, pool.Name, pool.Status.IPs))
	fc.mockOvnClient.EXPECT().DeleteAddressSet(pool.Name).Return(nil).Times(2)
	client := c.config.KubeOvnClient.(*fake.Clientset)
	unavailable := true
	written := false
	client.PrependReactor("patch", "ippools", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "status" || !unavailable {
			return false, nil, nil
		}
		var patch struct {
			Status map[string]json.RawMessage `json:"status"`
		}
		require.NoError(t, json.Unmarshal(action.(ktesting.PatchAction).GetPatch(), &patch))
		if _, ok := patch.Status["ips"]; !ok {
			return false, nil, nil
		}
		updated := pool.DeepCopy()
		require.NoError(t, json.Unmarshal(patch.Status["ips"], &updated.Status.IPs))
		require.NoError(t, client.Tracker().Update(apiv1.SchemeGroupVersion.WithResource("ippools"), updated, ""))
		written = true
		return true, nil, context.DeadlineExceeded
	})
	client.PrependReactor("get", "ippools", func(ktesting.Action) (bool, runtime.Object, error) {
		if unavailable && written {
			return true, nil, errors.New("API unavailable")
		}
		return false, nil, nil
	})
	require.Error(t, c.handleAddOrUpdateIPPool(pool.Name))
	_, _, _, err = c.ipam.GetRandomAddress("pool-pod", "pool-nic", nil, subnet.Name, pool.Name, nil, true)
	require.ErrorIs(t, err, ipam.ErrNoAvailable)
	address, _, _, err := c.ipam.GetRandomAddress("default-pod", "default-nic", nil, subnet.Name, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.3", address, "uncertain members must stay out of the default pool")
	unavailable = false
	require.NoError(t, c.handleAddOrUpdateIPPool(pool.Name))
	got, err := client.KubeovnV1().IPPools().Get(context.Background(), pool.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.1", "10.0.0.2"}, got.Status.IPs)
	_, _, _, err = c.ipam.GetRandomAddress("pool-pod", "pool-nic", nil, subnet.Name, pool.Name, nil, true)
	require.NoError(t, err)
}

func TestIPPoolFailedShrinkBlocksNewAllocations(t *testing.T) {
	subnet := &apiv1.Subnet{Name: "subnet", Spec: apiv1.SubnetSpec{CIDRBlock: "10.0.0.0/29"}}
	pool := &apiv1.IPPool{Name: "pool", Finalizers: []string{util.KubeOVNControllerFinalizer}, Spec: apiv1.IPPoolSpec{Subnet: subnet.Name, IPCount: 1, EnableAddressSet: true}, Status: apiv1.IPPoolStatus{IPs: []string{"10.0.0.1", "10.0.0.2"}}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*apiv1.Subnet{subnet}, IPPools: []*apiv1.IPPool{pool}})
	require.NoError(t, err)
	c := fc.fakeController
	c.ippoolKeyMutex = keymutex.NewHashed(97)
	require.NoError(t, c.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, "", nil))
	require.NoError(t, c.ipam.AddOrUpdateIPPool(subnet.Name, pool.Name, pool.Status.IPs))
	_, _, _, err = c.ipam.GetStaticAddress("existing", "existing-nic", "10.0.0.1", nil, subnet.Name, false)
	require.NoError(t, err)
	fc.mockOvnClient.EXPECT().CreateAddressSet(pool.Name, map[string]string{ippoolKey: pool.Name}).Return(nil).Times(2)
	fc.mockOvnClient.EXPECT().AddressSetUpdateAddress(pool.Name, "10.0.0.1").Return(nil).Times(2)
	client := c.config.KubeOvnClient.(*fake.Clientset)
	fail := true
	client.PrependReactor("patch", "ippools", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "status" || !fail {
			return false, nil, nil
		}
		var patch struct {
			Status map[string]json.RawMessage `json:"status"`
		}
		require.NoError(t, json.Unmarshal(action.(ktesting.PatchAction).GetPatch(), &patch))
		if _, ok := patch.Status["ips"]; !ok {
			return false, nil, nil
		}
		return true, nil, errors.New("write rejected")
	})
	require.Error(t, c.handleAddOrUpdateIPPool(pool.Name))
	_, _, _, err = c.ipam.GetRandomAddress("new-pool-pod", "new-pool-nic", nil, subnet.Name, pool.Name, nil, true)
	require.ErrorIs(t, err, ipam.ErrNoAvailable)
	_, _, _, err = c.ipam.GetStaticAddress("new-pool-pod", "new-pool-nic", "10.0.0.2", nil, subnet.Name, false)
	require.ErrorIs(t, err, ipam.ErrNoAvailable)
	_, _, _, err = c.ipam.GetStaticAddress("existing", "existing-nic", "10.0.0.1", nil, subnet.Name, false)
	require.NoError(t, err, "already allocated addresses remain recoverable")
	fail = false
	require.NoError(t, c.handleAddOrUpdateIPPool(pool.Name))
	got, err := client.KubeovnV1().IPPools().Get(context.Background(), pool.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.1"}, got.Status.IPs)
	released, _, _, err := c.ipam.GetRandomAddress("default-pod", "default-nic", nil, subnet.Name, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.2", released)
}

func TestInitIPAMRestoresCountPoolAddressSetBeforeActivation(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore", true: "OVN unavailable"}[fail], func(t *testing.T) {
			subnet := &apiv1.Subnet{Name: "subnet", Spec: apiv1.SubnetSpec{CIDRBlock: "10.0.0.0/29"}}
			pool := &apiv1.IPPool{Name: "pool", Spec: apiv1.IPPoolSpec{Subnet: subnet.Name, IPCount: 2, EnableAddressSet: true}, Status: apiv1.IPPoolStatus{IPs: []string{"10.0.0.1", "10.0.0.2"}}}
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*apiv1.Subnet{subnet}, IPPools: []*apiv1.IPPool{pool}})
			require.NoError(t, err)
			c := fc.fakeController
			var prepareErr error
			if fail {
				prepareErr = errors.New("OVN unavailable")
			}
			fc.mockOvnClient.EXPECT().CreateAddressSet(pool.Name, map[string]string{ippoolKey: pool.Name}).DoAndReturn(func(string, map[string]string) error {
				require.Nil(t, c.ipam.Subnets[subnet.Name].IPPools[pool.Name])
				return prepareErr
			})
			if !fail {
				fc.mockOvnClient.EXPECT().AddressSetUpdateAddress(pool.Name, "10.0.0.1", "10.0.0.2").Return(nil)
			}
			err = c.InitIPAM()
			if fail {
				require.ErrorIs(t, err, prepareErr)
				require.Nil(t, c.ipam.Subnets[subnet.Name].IPPools[pool.Name])
			} else {
				require.NoError(t, err)
				address, _, _, err := c.ipam.GetRandomAddress("pod", "nic", nil, subnet.Name, pool.Name, nil, true)
				require.NoError(t, err)
				require.Contains(t, pool.Status.IPs, address)
			}
		})
	}
}

func TestDeleteIPPoolRetainsFailedPreparationUntilAddressSetDeleted(t *testing.T) {
	subnet := &apiv1.Subnet{Name: "subnet", Spec: apiv1.SubnetSpec{CIDRBlock: "10.0.0.0/29"}}
	pool := &apiv1.IPPool{Name: "pool", Spec: apiv1.IPPoolSpec{Subnet: subnet.Name, IPCount: 2}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*apiv1.Subnet{subnet}, IPPools: []*apiv1.IPPool{pool}})
	require.NoError(t, err)
	c := fc.fakeController
	c.ippoolKeyMutex = keymutex.NewHashed(97)
	require.NoError(t, c.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, "", nil))
	failure := errors.New("OVN unavailable")
	fc.mockOvnClient.EXPECT().DeleteAddressSet(pool.Name).Return(failure).Times(2)
	require.Error(t, c.handleAddOrUpdateIPPool(pool.Name))
	pool, err = c.config.KubeOvnClient.KubeovnV1().IPPools().Get(context.Background(), pool.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, pool.Finalizers)
	require.Error(t, c.handleDeleteIPPool(pool))
	defaultIP, _, _, err := c.ipam.GetRandomAddress("default", "default-nic", nil, subnet.Name, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.3", defaultIP)
	fc.mockOvnClient.EXPECT().DeleteAddressSet(pool.Name).Return(nil)
	require.NoError(t, c.handleDeleteIPPool(pool))
	defaultIP, _, _, err = c.ipam.GetRandomAddress("next", "next-nic", nil, subnet.Name, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.1", defaultIP)
	got, err := c.config.KubeOvnClient.KubeovnV1().IPPools().Get(context.Background(), pool.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, got.Finalizers)
}
