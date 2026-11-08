package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	listers "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

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
	// unbounded and must retain every operation, including deletion snapshots.
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
