package controllerruntime

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/controller"
)

func TestIPPoolNativeWatchUpdates(t *testing.T) {
	tests := []struct {
		name      string
		change    func(*kubeovnv1.IPPool)
		operation controller.IPPoolOperation
		want      bool
	}{
		{name: "resync"},
		{name: "statistics", change: func(p *kubeovnv1.IPPool) { p.Status.V4UsingIPs.SetInt64(1) }},
		{name: "conditions", change: func(p *kubeovnv1.IPPool) { p.Status.EnsureStandardConditions() }},
		{name: "foreign finalizer", change: func(p *kubeovnv1.IPPool) { p.Finalizers = []string{"example.com/owner"} }},
		{name: "metadata", change: func(p *kubeovnv1.IPPool) { p.Labels = map[string]string{"external": "value"} }},
		{name: "count", change: func(p *kubeovnv1.IPPool) { p.Spec.IPCount++ }, want: true},
		{name: "explicit ranges", change: func(p *kubeovnv1.IPPool) { p.Spec.IPs = []string{"10.0.0.2..10.0.0.3"} }, want: true},
		{name: "namespaces", change: func(p *kubeovnv1.IPPool) { p.Spec.Namespaces = []string{"tenant"} }, want: true},
		{name: "address set", change: func(p *kubeovnv1.IPPool) { p.Spec.EnableAddressSet = true }, want: true},
		{name: "durable membership", change: func(p *kubeovnv1.IPPool) { p.Status.IPs = []string{"10.0.0.2", "10.0.0.4"} }, want: true},
		{name: "membership order", change: func(p *kubeovnv1.IPPool) { p.Status.IPs = []string{"10.0.0.3", "10.0.0.2"} }, want: true},
		{name: "terminating", change: func(p *kubeovnv1.IPPool) { p.DeletionTimestamp = new(metav1.Now()) }, operation: controller.IPPoolDelete, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := &kubeovnv1.IPPool{Name: "pool", UID: "uid", ResourceVersion: "1", Spec: kubeovnv1.IPPoolSpec{Subnet: "subnet", IPCount: 1}, Status: kubeovnv1.IPPoolStatus{IPs: []string{"10.0.0.2", "10.0.0.3"}}}
			updated := old.DeepCopy()
			updated.ResourceVersion = "2"
			if tt.change != nil {
				tt.change(updated)
			}
			q := newIPPoolEventQueue(t)
			ipPoolEventHandler().Update(t.Context(), event.TypedUpdateEvent[*kubeovnv1.IPPool]{ObjectOld: old, ObjectNew: updated}, q)
			if !tt.want {
				require.Zero(t, q.Len())
				return
			}
			require.Equal(t, 1, q.Len())
			request, _ := q.Get()
			defer q.Done(request)
			require.Equal(t, updated.Name, request.Name)
			require.Equal(t, tt.operation, request.Operation)
			if tt.operation == controller.IPPoolDelete {
				require.Equal(t, updated, request.Deleted)
				require.NotSame(t, updated, request.Deleted)
			} else {
				require.Nil(t, request.Deleted)
			}
		})
	}
}

func TestIPPoolNativeWatchRetainsDeletionSnapshotsAndReplacement(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "observed delete", true: "unknown final state"}[unknown], func(t *testing.T) {
			q := newIPPoolEventQueue(t)
			h := ipPoolEventHandler()
			old := &kubeovnv1.IPPool{Name: "pool", UID: "old", Spec: kubeovnv1.IPPoolSpec{Subnet: "old-subnet", IPs: []string{"10.0.0.2"}}}
			expected := old.DeepCopy()
			h.Delete(t.Context(), event.TypedDeleteEvent[*kubeovnv1.IPPool]{Object: old, DeleteStateUnknown: unknown}, q)
			// A new object with the same name must not coalesce with the old cleanup.
			replacement := &kubeovnv1.IPPool{Name: old.Name, UID: "new", Spec: kubeovnv1.IPPoolSpec{Subnet: "new-subnet"}}
			h.Create(t.Context(), event.TypedCreateEvent[*kubeovnv1.IPPool]{Object: replacement}, q)
			old.Spec.IPs[0] = "10.0.0.9"
			old.Spec.Subnet = "changed"
			require.Equal(t, 2, q.Len())
			deleted, _ := q.Get()
			defer q.Done(deleted)
			require.Equal(t, controller.IPPoolDelete, deleted.Operation)
			require.Equal(t, expected, deleted.Deleted)
			created, _ := q.Get()
			defer q.Done(created)
			require.Equal(t, controller.IPPoolRequest{Name: replacement.Name, Operation: controller.IPPoolReconcile}, created)
		})
	}
}

func TestIPPoolNativeWatchInitialTerminatingObject(t *testing.T) {
	q := newIPPoolEventQueue(t)
	pool := &kubeovnv1.IPPool{Name: "pool", UID: "uid", DeletionTimestamp: new(metav1.Now())}
	ipPoolEventHandler().Create(t.Context(), event.TypedCreateEvent[*kubeovnv1.IPPool]{Object: pool, IsInInitialList: true}, q)
	require.Equal(t, 1, q.Len())
	request, _ := q.Get()
	defer q.Done(request)
	require.Equal(t, controller.IPPoolDelete, request.Operation)
	require.Equal(t, pool, request.Deleted)
	require.NotSame(t, pool, request.Deleted)
}

func TestIPPoolNativeWatchIgnoresInvalidAndGenericEvents(t *testing.T) {
	q := newIPPoolEventQueue(t)
	h := ipPoolEventHandler()
	h.Create(t.Context(), event.TypedCreateEvent[*kubeovnv1.IPPool]{}, q)
	h.Delete(t.Context(), event.TypedDeleteEvent[*kubeovnv1.IPPool]{}, q)
	h.Update(t.Context(), event.TypedUpdateEvent[*kubeovnv1.IPPool]{}, q)
	h.Update(t.Context(), event.TypedUpdateEvent[*kubeovnv1.IPPool]{ObjectOld: &kubeovnv1.IPPool{}}, q)
	h.Update(t.Context(), event.TypedUpdateEvent[*kubeovnv1.IPPool]{ObjectNew: &kubeovnv1.IPPool{}}, q)
	h.Generic(t.Context(), event.TypedGenericEvent[*kubeovnv1.IPPool]{Object: &kubeovnv1.IPPool{Name: "pool"}}, q)
	require.Zero(t, q.Len())
}

func newIPPoolEventQueue(t *testing.T) workqueue.TypedRateLimitingInterface[controller.IPPoolRequest] {
	t.Helper()
	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[controller.IPPoolRequest]())
	t.Cleanup(q.ShutDown)
	return q
}

func TestIPPoolNativeWatchRelistReplacement(t *testing.T) {
	q := newIPPoolEventQueue(t)
	old := &kubeovnv1.IPPool{Name: "pool", UID: "old", Spec: kubeovnv1.IPPoolSpec{Subnet: "subnet"}}
	replacement := old.DeepCopy()
	replacement.UID = "new"
	ipPoolEventHandler().Update(t.Context(), event.TypedUpdateEvent[*kubeovnv1.IPPool]{ObjectOld: old, ObjectNew: replacement}, q)
	require.Equal(t, 2, q.Len(), "same-spec replacement must clean old reservations and reconcile the new UID")
	deleted, _ := q.Get()
	defer q.Done(deleted)
	require.Equal(t, controller.IPPoolDelete, deleted.Operation)
	require.Equal(t, old, deleted.Deleted)
	require.NotSame(t, old, deleted.Deleted)
	created, _ := q.Get()
	defer q.Done(created)
	require.Equal(t, controller.IPPoolRequest{Name: replacement.Name, Operation: controller.IPPoolReconcile}, created)
}
