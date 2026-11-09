package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	listers "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestIPPoolRuntimeRecoveryGate(t *testing.T) {
	c := &Controller{}
	c.beginIPPoolRuntimeRecovery()

	blocked, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	require.ErrorIs(t, c.waitForIPPoolRuntimeRecovery(blocked), context.DeadlineExceeded)
	cancel()

	c.finishIPPoolRuntimeRecovery()
	require.NoError(t, c.waitForIPPoolRuntimeRecovery(t.Context()))
	c.finishIPPoolRuntimeRecovery()
}

func TestIPPoolRuntimeRecoveryGateFollowsContext(t *testing.T) {
	c := &Controller{}
	c.beginIPPoolRuntimeRecovery()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, c.waitForIPPoolRuntimeRecovery(ctx), context.Canceled)
}

func TestIPPoolRuntimeRecoveryGateWithoutSetup(t *testing.T) {
	c := &Controller{}
	c.finishIPPoolRuntimeRecovery()
	require.NoError(t, c.WaitForIPPoolRuntimeRecovery(t.Context()))
}

func TestIPPoolRuntimeBridgeRetainsStartupBacklog(t *testing.T) {
	c := &Controller{
		addOrUpdateIPPoolQueue:  workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]()),
		updateIPPoolStatusQueue: workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]()),
		deleteIPPoolQueue:       workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[*kubeovnv1.IPPool]()),
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	const count = 64
	for i := range count {
		name := fmt.Sprintf("pool-%d", i)
		c.addOrUpdateIPPoolQueue.Add(name)
		c.updateIPPoolStatusQueue.Add(name)
		c.deleteIPPoolQueue.Add(&kubeovnv1.IPPool{Name: name, UID: types.UID(name)})
	}
	// No manager consumes events until recovery is complete. The ingress is
	// unbounded and retains Subnet notifications and pre-manager deletions.
	received := make(chan IPPoolRequest, count*3)
	c.StartIPPoolEventBridge(ctx, func(request IPPoolRequest) { received <- request })
	seen := make(map[string]bool)
	for range count * 3 {
		select {
		case request := <-received:
			key := fmt.Sprintf("%s/%d", request.Name, request.Operation)
			require.False(t, seen[key])
			seen[key] = true
			if request.Operation == IPPoolDelete {
				require.Equal(t, types.UID(request.Name), request.Deleted.UID)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("startup events were lost")
		}
	}
	cancel()
	require.Eventually(t, func() bool {
		return c.addOrUpdateIPPoolQueue.ShuttingDown() && c.updateIPPoolStatusQueue.ShuttingDown() && c.deleteIPPoolQueue.ShuttingDown()
	}, time.Second, time.Millisecond)
}

func TestIPPoolRuntimeStatusUsesLiveObjectWithoutMembershipSideEffects(t *testing.T) {
	pool := &kubeovnv1.IPPool{Name: "pool", UID: "current", Spec: kubeovnv1.IPPoolSpec{Subnet: "subnet", IPs: []string{"10.0.0.2"}}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{IPPools: []*kubeovnv1.IPPool{pool}})
	require.NoError(t, err)
	c := fc.fakeController
	c.ippoolKeyMutex = keymutex.NewHashed(97)
	require.NoError(t, c.ipam.AddOrUpdateSubnet("subnet", "10.0.0.0/29", "", nil))
	require.NoError(t, c.ipam.AddOrUpdateIPPool("subnet", "pool", pool.Spec.IPs))
	_, _, _, err = c.ipam.GetRandomAddress("pod", "nic", nil, "subnet", "pool", nil, true)
	require.NoError(t, err)
	stale := pool.DeepCopy()
	stale.Status.IPs = []string{"10.0.0.6"}
	index := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, index.Add(stale))
	c.ippoolLister = listers.NewIPPoolLister(index)
	// No OVN expectations: a statistics notification must never prepare an
	// address set or activate a membership selection.
	require.NoError(t, c.ReconcileIPPool(t.Context(), IPPoolRequest{Name: pool.Name, Operation: IPPoolStatus}))
	got, err := c.config.KubeOvnClient.KubeovnV1().IPPools().Get(t.Context(), pool.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, int64(1), got.Status.V4UsingIPs.Int64())
	require.Empty(t, got.Status.IPs)
	require.Equal(t, "10.0.0.2", c.ipam.Subnets["subnet"].IPPools["pool"].V4IPs.String())
	require.Empty(t, got.Finalizers)
	require.Empty(t, got.Status.Conditions)
}

func TestIPPoolRuntimeFullReconcileUsesLiveSpec(t *testing.T) {
	pool := &kubeovnv1.IPPool{Name: "pool", UID: "current", Spec: kubeovnv1.IPPoolSpec{Subnet: "subnet", IPs: []string{"10.0.0.2"}}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{IPPools: []*kubeovnv1.IPPool{pool}})
	require.NoError(t, err)
	c := fc.fakeController
	c.ippoolKeyMutex = keymutex.NewHashed(97)
	require.NoError(t, c.ipam.AddOrUpdateSubnet("subnet", "10.0.0.0/29", "", nil))
	stale := pool.DeepCopy()
	stale.Spec.IPs = []string{"10.0.0.6"}
	index := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, index.Add(stale))
	c.ippoolLister = listers.NewIPPoolLister(index)
	fc.mockOvnClient.EXPECT().DeleteAddressSet("pool").Return(nil)
	require.NoError(t, c.ReconcileIPPool(t.Context(), IPPoolRequest{Name: pool.Name, Operation: IPPoolReconcile}))
	require.Equal(t, "10.0.0.2", c.ipam.Subnets["subnet"].IPPools["pool"].V4IPs.String())
}

func TestIPPoolRuntimeDeletionGuardsReplacementAndCleansMissingObject(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("replacement=%t", existing), func(t *testing.T) {
			deleted := &kubeovnv1.IPPool{Name: "pool", UID: "old", Finalizers: []string{util.KubeOVNControllerFinalizer}, Spec: kubeovnv1.IPPoolSpec{Subnet: "subnet"}}
			var pools []*kubeovnv1.IPPool
			if existing {
				replacement := deleted.DeepCopy()
				replacement.UID = "new"
				pools = append(pools, replacement)
			}
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{IPPools: pools})
			require.NoError(t, err)
			c := fc.fakeController
			c.ippoolKeyMutex = keymutex.NewHashed(97)
			require.NoError(t, c.ipam.AddOrUpdateSubnet("subnet", "10.0.0.0/29", "", nil))
			require.NoError(t, c.ipam.AddOrUpdateIPPool("subnet", "pool", []string{"10.0.0.2"}))
			if !existing {
				fc.mockOvnClient.EXPECT().DeleteAddressSet("pool").Return(nil)
			}
			require.NoError(t, c.ReconcileIPPool(t.Context(), IPPoolRequest{Name: "pool", Operation: IPPoolDelete, Deleted: deleted}))
			_, present := c.ipam.Subnets["subnet"].IPPools["pool"]
			require.Equal(t, existing, present)
			require.Equal(t, []string{util.KubeOVNControllerFinalizer}, deleted.Finalizers, "the watch snapshot is immutable")
		})
	}
}

func TestIPPoolRuntimeTerminatingAddRunsCleanup(t *testing.T) {
	pool := &kubeovnv1.IPPool{Name: "pool", UID: "current", Finalizers: []string{util.KubeOVNControllerFinalizer}, DeletionTimestamp: new(metav1.Now()), Spec: kubeovnv1.IPPoolSpec{Subnet: "subnet"}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{IPPools: []*kubeovnv1.IPPool{pool}})
	require.NoError(t, err)
	c := fc.fakeController
	c.ippoolKeyMutex = keymutex.NewHashed(97)
	require.NoError(t, c.ipam.AddOrUpdateSubnet("subnet", "10.0.0.0/29", "", nil))
	require.NoError(t, c.ipam.AddOrUpdateIPPool("subnet", "pool", []string{"10.0.0.2"}))
	fc.mockOvnClient.EXPECT().DeleteAddressSet("pool").Return(nil)
	require.NoError(t, c.ReconcileIPPool(t.Context(), IPPoolRequest{Name: pool.Name, Operation: IPPoolReconcile}))
	require.NotContains(t, c.ipam.Subnets["subnet"].IPPools, "pool")
	got, err := c.config.KubeOvnClient.KubeovnV1().IPPools().Get(t.Context(), pool.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, got.Finalizers)
}

func newIPPoolRuntimeReplacement(t *testing.T) (*fakeController, *kubeovnv1.IPPool, *kubeovnv1.IPPool) {
	t.Helper()
	deleted := &kubeovnv1.IPPool{Name: "pool", UID: "old", Finalizers: []string{util.KubeOVNControllerFinalizer}, Spec: kubeovnv1.IPPoolSpec{Subnet: "old-subnet"}}
	subnet := &kubeovnv1.Subnet{Name: "new-subnet", Spec: kubeovnv1.SubnetSpec{CIDRBlock: "10.1.0.0/29"}}
	replacement := &kubeovnv1.IPPool{Name: deleted.Name, UID: "new", Finalizers: []string{util.KubeOVNControllerFinalizer}, Spec: kubeovnv1.IPPoolSpec{Subnet: subnet.Name, IPCount: 1, EnableAddressSet: true}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{subnet}, IPPools: []*kubeovnv1.IPPool{replacement}})
	require.NoError(t, err)
	c := fc.fakeController
	c.ippoolKeyMutex = keymutex.NewHashed(97)
	require.NoError(t, c.ipam.AddOrUpdateSubnet(deleted.Spec.Subnet, "10.0.0.0/29", "", nil))
	require.NoError(t, c.ipam.AddOrUpdateIPPool(deleted.Spec.Subnet, deleted.Name, []string{"10.0.0.1"}))
	require.NoError(t, c.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, "", nil))
	return fc, deleted, replacement
}

func TestIPPoolRuntimeDeletionCleansOldSubnetAfterPreparingReplacement(t *testing.T) {
	fc, deleted, replacement := newIPPoolRuntimeReplacement(t)
	c := fc.fakeController
	fc.mockOvnClient.EXPECT().CreateAddressSet(replacement.Name, map[string]string{ippoolKey: replacement.Name}).Return(nil)
	fc.mockOvnClient.EXPECT().AddressSetUpdateAddress(replacement.Name, "10.1.0.1").DoAndReturn(func(string, ...string) error {
		require.Contains(t, c.ipam.Subnets[deleted.Spec.Subnet].IPPools, deleted.Name, "old addresses must stay reserved until OVN stops referencing them")
		return nil
	})
	require.NoError(t, c.ReconcileIPPool(t.Context(), IPPoolRequest{Name: deleted.Name, Operation: IPPoolDelete, Deleted: deleted}))
	require.NotContains(t, c.ipam.Subnets[deleted.Spec.Subnet].IPPools, deleted.Name)
	require.Equal(t, "10.1.0.1", c.ipam.Subnets[replacement.Spec.Subnet].IPPools[replacement.Name].V4IPs.String())
	got, err := c.config.KubeOvnClient.KubeovnV1().IPPools().Get(t.Context(), replacement.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, replacement.UID, got.UID)
	require.Equal(t, replacement.Finalizers, got.Finalizers)
	require.Equal(t, []string{"10.1.0.1"}, got.Status.IPs)
}

func TestIPPoolRuntimeReplacementPreparationFailureRetainsReservations(t *testing.T) {
	for _, failure := range []string{"OVN", "status API"} {
		t.Run(failure, func(t *testing.T) {
			fc, deleted, replacement := newIPPoolRuntimeReplacement(t)
			c := fc.fakeController
			prepareErr := errors.New("replacement preparation failed")
			failing := true
			fc.mockOvnClient.EXPECT().CreateAddressSet(replacement.Name, map[string]string{ippoolKey: replacement.Name}).DoAndReturn(func(string, map[string]string) error {
				require.Contains(t, c.ipam.Subnets[deleted.Spec.Subnet].IPPools, deleted.Name)
				if failing && failure == "OVN" {
					return prepareErr
				}
				return nil
			}).Times(2)
			updates := 2
			if failure == "OVN" {
				updates = 1
			}
			fc.mockOvnClient.EXPECT().AddressSetUpdateAddress(replacement.Name, "10.1.0.1").Return(nil).Times(updates)
			c.config.KubeOvnClient.(*kubeovnfake.Clientset).PrependReactor("patch", "ippools", func(action ktesting.Action) (bool, runtime.Object, error) {
				if failing && failure == "status API" && action.GetSubresource() == "status" && bytes.Contains(action.(ktesting.PatchAction).GetPatch(), []byte(`"ips":`)) {
					return true, nil, prepareErr
				}
				return false, nil, nil
			})
			request := IPPoolRequest{Name: deleted.Name, Operation: IPPoolDelete, Deleted: deleted}
			require.ErrorIs(t, c.ReconcileIPPool(t.Context(), request), prepareErr)
			require.Contains(t, c.ipam.Subnets[deleted.Spec.Subnet].IPPools, deleted.Name)
			_, _, _, allocErr := c.ipam.GetRandomAddress("pod", "nic", nil, replacement.Spec.Subnet, replacement.Name, nil, true)
			require.ErrorIs(t, allocErr, ipam.ErrNoAvailable, "failed replacement selections must remain reserved")
			failing = false
			require.NoError(t, c.ReconcileIPPool(t.Context(), request))
			require.NotContains(t, c.ipam.Subnets[deleted.Spec.Subnet].IPPools, deleted.Name)
			require.Equal(t, "10.1.0.1", c.ipam.Subnets[replacement.Spec.Subnet].IPPools[replacement.Name].V4IPs.String())
			got, err := c.config.KubeOvnClient.KubeovnV1().IPPools().Get(t.Context(), replacement.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, replacement.Finalizers, got.Finalizers)
		})
	}
}

func TestIPPoolRuntimeDeletionCleansTerminatingReplacementBeforeOldSubnet(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("cleanup failure=%t", fail), func(t *testing.T) {
			fc, deleted, replacement := newIPPoolRuntimeReplacement(t)
			c := fc.fakeController
			replacement.DeletionTimestamp = new(metav1.Now())
			_, err := c.config.KubeOvnClient.KubeovnV1().IPPools().Update(t.Context(), replacement, metav1.UpdateOptions{})
			require.NoError(t, err)
			require.NoError(t, c.ipam.AddOrUpdateIPPool(replacement.Spec.Subnet, replacement.Name, []string{"10.1.0.1"}))
			cleanupErr := errors.New("address set cleanup failed")
			attempts := 1
			if fail {
				attempts++
			}
			fc.mockOvnClient.EXPECT().DeleteAddressSet(replacement.Name).DoAndReturn(func(string) error {
				require.Contains(t, c.ipam.Subnets[deleted.Spec.Subnet].IPPools, deleted.Name)
				require.Contains(t, c.ipam.Subnets[replacement.Spec.Subnet].IPPools, replacement.Name)
				if fail {
					return cleanupErr
				}
				return nil
			}).Times(attempts)
			request := IPPoolRequest{Name: deleted.Name, Operation: IPPoolDelete, Deleted: deleted}
			if fail {
				require.ErrorIs(t, c.ReconcileIPPool(t.Context(), request), cleanupErr)
				require.Contains(t, c.ipam.Subnets[deleted.Spec.Subnet].IPPools, deleted.Name)
				require.Contains(t, c.ipam.Subnets[replacement.Spec.Subnet].IPPools, replacement.Name)
				fail = false
			}
			require.NoError(t, c.ReconcileIPPool(t.Context(), request))
			require.NotContains(t, c.ipam.Subnets[deleted.Spec.Subnet].IPPools, deleted.Name)
			require.NotContains(t, c.ipam.Subnets[replacement.Spec.Subnet].IPPools, replacement.Name)
			got, err := c.config.KubeOvnClient.KubeovnV1().IPPools().Get(t.Context(), replacement.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Empty(t, got.Finalizers)
		})
	}
}

func TestIPPoolRuntimeReplacementDisappearsOrTerminatesDuringCountRead(t *testing.T) {
	for _, disappears := range []bool{false, true} {
		t.Run(fmt.Sprintf("disappears=%t", disappears), func(t *testing.T) {
			fc, deleted, replacement := newIPPoolRuntimeReplacement(t)
			c := fc.fakeController
			client := c.config.KubeOvnClient.(*kubeovnfake.Clientset)
			gets := 0
			client.PrependReactor("get", "ippools", func(ktesting.Action) (bool, runtime.Object, error) {
				gets++
				if gets == 2 {
					resource := kubeovnv1.SchemeGroupVersion.WithResource("ippools")
					if disappears {
						require.NoError(t, client.Tracker().Delete(resource, "", replacement.Name))
					} else {
						terminating := replacement.DeepCopy()
						terminating.DeletionTimestamp = new(metav1.Now())
						require.NoError(t, client.Tracker().Update(resource, terminating, ""))
					}
				}
				return false, nil, nil
			})
			request := IPPoolRequest{Name: deleted.Name, Operation: IPPoolDelete, Deleted: deleted}
			// The business helper returns nil without touching the address set.
			// The old addresses must remain reserved until deletion is retried.
			require.Error(t, c.ReconcileIPPool(t.Context(), request))
			require.Contains(t, c.ipam.Subnets[deleted.Spec.Subnet].IPPools, deleted.Name)
			fc.mockOvnClient.EXPECT().DeleteAddressSet(replacement.Name).Return(nil)
			require.NoError(t, c.ReconcileIPPool(t.Context(), request))
			require.NotContains(t, c.ipam.Subnets[deleted.Spec.Subnet].IPPools, deleted.Name)
			if disappears {
				for _, action := range client.Actions() {
					require.NotEqual(t, "patch", action.GetVerb(), "missing objects must not receive finalizer patches")
				}
			}
		})
	}
}

func TestIPPoolRuntimeReplacementChangesDuringCountRead(t *testing.T) {
	for _, changeUID := range []bool{false, true} {
		t.Run(fmt.Sprintf("UID change=%t", changeUID), func(t *testing.T) {
			fc, deleted, replacement := newIPPoolRuntimeReplacement(t)
			c := fc.fakeController
			client := c.config.KubeOvnClient.(*kubeovnfake.Clientset)
			gets := 0
			client.PrependReactor("get", "ippools", func(ktesting.Action) (bool, runtime.Object, error) {
				gets++
				if gets == 2 {
					changed := replacement.DeepCopy()
					if changeUID {
						changed.UID = "newer"
					} else {
						changed.Generation++
					}
					require.NoError(t, client.Tracker().Update(kubeovnv1.SchemeGroupVersion.WithResource("ippools"), changed, ""))
				}
				return false, nil, nil
			})
			fc.mockOvnClient.EXPECT().CreateAddressSet(replacement.Name, map[string]string{ippoolKey: replacement.Name}).Return(nil).Times(2)
			fc.mockOvnClient.EXPECT().AddressSetUpdateAddress(replacement.Name, "10.1.0.1").Return(nil).Times(2)
			request := IPPoolRequest{Name: deleted.Name, Operation: IPPoolDelete, Deleted: deleted}
			require.ErrorContains(t, c.ReconcileIPPool(t.Context(), request), "changed during replacement reconciliation")
			require.Contains(t, c.ipam.Subnets[deleted.Spec.Subnet].IPPools, deleted.Name)
			require.NoError(t, c.ReconcileIPPool(t.Context(), request))
			require.NotContains(t, c.ipam.Subnets[deleted.Spec.Subnet].IPPools, deleted.Name)
			require.Equal(t, "10.1.0.1", c.ipam.Subnets[replacement.Spec.Subnet].IPPools[replacement.Name].V4IPs.String())
		})
	}
}

func TestStartControllerRuntimeSharesBackendAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	c := &Controller{}
	started := make(chan *Controller, 1)
	done := startControllerRuntime(ctx, c, func(ctx context.Context, backend *Controller) error {
		started <- backend
		<-ctx.Done()
		return ctx.Err()
	})
	select {
	case backend := <-started:
		require.Same(t, c, backend)
	case <-time.After(time.Second):
		t.Fatal("runtime did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runtime did not stop with leader context")
	}
	select {
	case <-startControllerRuntime(t.Context(), c, nil):
	default:
		t.Fatal("disabled mode must not wait for a runtime")
	}
}

func TestIPPoolRuntimeIngressOwnership(t *testing.T) {
	for _, mode := range []string{ControllerRuntimeModeDisabled, ControllerRuntimeModeManager, ControllerRuntimeModeVlan, ControllerRuntimeModeIPPool} {
		t.Run(mode, func(t *testing.T) {
			c := &Controller{
				config:                 &Configuration{ControllerRuntimeMode: mode},
				addOrUpdateIPPoolQueue: workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]()),
				deleteIPPoolQueue:      workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[*kubeovnv1.IPPool]()),
			}
			t.Cleanup(c.addOrUpdateIPPoolQueue.ShutDown)
			t.Cleanup(c.deleteIPPoolQueue.ShutDown)
			h := c.ipPoolEventHandler()
			pool := &kubeovnv1.IPPool{Name: "pool", UID: "uid", Spec: kubeovnv1.IPPoolSpec{IPCount: 1}}
			h.OnAdd(pool, true)
			updated := pool.DeepCopy()
			updated.Name = "updated"
			updated.Spec.IPCount = 2
			h.OnUpdate(pool, updated)
			if mode == ControllerRuntimeModeIPPool {
				require.Zero(t, c.addOrUpdateIPPoolQueue.Len(), "native watch owns create/update events")
			} else {
				require.Equal(t, 2, c.addOrUpdateIPPoolQueue.Len(), "other modes retain legacy ingress")
			}
			h.OnDelete(cache.DeletedFinalStateUnknown{Obj: pool})
			require.Equal(t, 1, c.deleteIPPoolQueue.Len(), "pre-manager deletion must survive in every mode")
			snapshot, _ := c.deleteIPPoolQueue.Get()
			require.Equal(t, pool, snapshot)
			c.deleteIPPoolQueue.Done(snapshot)
			if mode == ControllerRuntimeModeIPPool {
				replacement := pool.DeepCopy()
				replacement.UID = "replacement"
				h.OnUpdate(pool, replacement)
				require.Equal(t, 1, c.deleteIPPoolQueue.Len(), "relist replacement must preserve old UID cleanup")
				replaced, _ := c.deleteIPPoolQueue.Get()
				defer c.deleteIPPoolQueue.Done(replaced)
				require.Equal(t, pool, replaced)
			}
		})
	}
}
