package controllerruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestSubnetReconcilerIgnoresStatusOnlyUpdates(t *testing.T) {
	oldSubnet := testSubnet("")
	newSubnet := oldSubnet.DeepCopy()
	newSubnet.Status.GatewayMAC = "00:11:22:33:44:55"
	require.False(t, subnetSpecOrLifecycleChanged(event.UpdateEvent{ObjectOld: oldSubnet, ObjectNew: newSubnet}))

	newSubnet.Generation++
	require.True(t, subnetSpecOrLifecycleChanged(event.UpdateEvent{ObjectOld: oldSubnet, ObjectNew: newSubnet}))
}

func TestSubnetReconcilerFansOutProviderNetworkEvents(t *testing.T) {
	vlan := testVlan("vlan-a", "provider-a")
	vlan.Status.Subnets = []string{"subnet-a"}
	provider := testProvider("provider-a", false, "vlan-a")
	reconciler := &SubnetReconciler{Reader: newRuntimeClient(t, vlan, provider)}
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[ctrl.Request]())
	t.Cleanup(queue.ShutDown)

	reconciler.enqueueProviderNetworkRequests(t.Context(), queue, provider)
	require.Equal(t, 1, queue.Len())
	request, _ := queue.Get()
	queue.Done(request)
	require.Equal(t, ctrl.Request{Name: "subnet-a"}, request)
}

func TestSubnetReconcilerFansOutProviderNetworkUpdateUnion(t *testing.T) {
	oldProvider := testProvider("provider-a", false, "vlan-old")
	newProvider := testProvider("provider-a", true, "vlan-new")
	oldVlan := testVlan("vlan-old", "provider-a")
	oldVlan.Status.Subnets = []string{"subnet-old"}
	newVlan := testVlan("vlan-new", "provider-a")
	newVlan.Status.Subnets = []string{"subnet-new"}
	reconciler := &SubnetReconciler{Reader: newRuntimeClient(t, oldVlan, newVlan, newProvider)}
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[ctrl.Request]())
	t.Cleanup(queue.ShutDown)

	reconciler.enqueueProviderNetworkRequests(t.Context(), queue, oldProvider, newProvider)
	require.Equal(t, 2, queue.Len())
	requests := make([]ctrl.Request, 0, queue.Len())
	for queue.Len() > 0 {
		request, _ := queue.Get()
		queue.Done(request)
		requests = append(requests, request)
	}
	require.Equal(t, []ctrl.Request{{Name: "subnet-new"}, {Name: "subnet-old"}}, requests)
}

func TestSubnetReconcilerFailsClosedWithoutDependencies(t *testing.T) {
	reconciler := &SubnetReconciler{}
	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: "subnet-a"})
	require.ErrorIs(t, err, ErrSubnetWriterRecoveryMissing)
	reconciler.RecoveryGate = func(context.Context) error { return nil }
	_, err = reconciler.Reconcile(t.Context(), ctrl.Request{Name: "subnet-a"})
	require.ErrorIs(t, err, ErrSubnetWriterAdapterMissing)
	reconciler.Adapter = &recordingSubnetWriterAdapter{}
	_, err = reconciler.Reconcile(t.Context(), ctrl.Request{Name: "subnet-a"})
	require.ErrorIs(t, err, ErrSubnetWriterLeaseMissing)
}

func TestSetupSubnetReconcilerFailsClosedBeforeManagerRegistration(t *testing.T) {
	ownership := &SubnetWriterOwnership{}
	lease, err := ownership.Claim(t.Context(), SubnetWriterOwnerRuntime)
	require.NoError(t, err)
	require.ErrorIs(t, SetupSubnetReconciler(nil, nil, nil, nil, lease), ErrSubnetWriterRecoveryMissing)
	require.ErrorIs(t, SetupSubnetReconciler(nil, nil, func(context.Context) error { return nil }, nil, lease), ErrSubnetWriterAdapterMissing)
	require.ErrorIs(t, SetupSubnetReconciler(nil, nil, func(context.Context) error { return nil }, &recordingSubnetWriterAdapter{}, lease), ErrSubnetWriterManagerMissing)
}

func TestSubnetReconcilerDelegatesImmutableSnapshot(t *testing.T) {
	subnet := testSubnet("")
	reader := &countingSubnetReader{Reader: newRuntimeClient(t, subnet)}
	var gotSnapshot SubnetWriteSnapshot
	var gotObservation SubnetShadowObservation
	called := false
	reconciler := &SubnetReconciler{
		Reader:       reader,
		RecoveryGate: func(context.Context) error { return nil },
		Adapter: subnetWriterAdapterFunc(func(_ context.Context, snapshot SubnetWriteSnapshot, observation SubnetShadowObservation) error {
			called = true
			gotSnapshot = snapshot
			gotObservation = observation
			return nil
		}),
		Lease: testSubnetWriterLease(t),
	}

	result, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: subnet.Name})
	require.NoError(t, err)
	require.Zero(t, result)
	require.True(t, called)
	require.Equal(t, subnet.Name, gotSnapshot.Name)
	require.Equal(t, subnet.UID, gotSnapshot.UID)
	require.Equal(t, subnet.Spec, gotSnapshot.Spec)
	require.Equal(t, subnet.Name, gotObservation.Name)
	require.Equal(t, gotSnapshot.Generation, gotObservation.Generation)
	require.Equal(t, 1, reader.subnetGets, "reconcile must reuse the snapshot Subnet read")
}

func TestSubnetReconcilerRecoversBeforeReconcileAndOnlyOncePerUID(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid-1")
	reader := newRuntimeClient(t, subnet)
	var calls []string
	adapter := &recordingSubnetWriterAdapter{
		recover: func(_ context.Context, got SubnetWriteSnapshot) error {
			calls = append(calls, "recover:"+got.Name)
			return nil
		},
		reconcile: func(_ context.Context, got SubnetWriteSnapshot, _ SubnetShadowObservation) error {
			calls = append(calls, "reconcile:"+got.Name)
			return nil
		},
	}
	reconciler := &SubnetReconciler{
		Reader:       reader,
		RecoveryGate: func(context.Context) error { return nil },
		Adapter:      adapter,
		Lease:        testSubnetWriterLease(t),
	}

	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: subnet.Name})
	require.NoError(t, err)
	_, err = reconciler.Reconcile(t.Context(), ctrl.Request{Name: subnet.Name})
	require.NoError(t, err)
	require.Equal(t, []string{"recover:" + subnet.Name, "reconcile:" + subnet.Name, "reconcile:" + subnet.Name}, calls)
}

func TestSubnetReconcilerPropagatesAdapterRecoveryError(t *testing.T) {
	recoveryErr := errors.New("adapter recovery failed")
	reconciled := false
	reconciler := &SubnetReconciler{
		Reader:       newRuntimeClient(t, testSubnet("")),
		RecoveryGate: func(context.Context) error { return nil },
		Adapter: &recordingSubnetWriterAdapter{
			recover: func(context.Context, SubnetWriteSnapshot) error { return recoveryErr },
			reconcile: func(context.Context, SubnetWriteSnapshot, SubnetShadowObservation) error {
				reconciled = true
				return nil
			},
		},
		Lease: testSubnetWriterLease(t),
	}

	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: "subnet-a"})
	require.ErrorIs(t, err, recoveryErr)
	require.False(t, reconciled)
}

func TestSubnetReconcilerConsumesDeleteSnapshotWhenAPIObjectIsGone(t *testing.T) {
	deleted := testSubnet("")
	deleted.UID = types.UID("deleted-uid")
	deletionTimestamp := metav1.Now()
	deleted.DeletionTimestamp = &deletionTimestamp
	snapshot, err := NewSubnetWriteSnapshot(deleted, false)
	require.NoError(t, err)
	called := false
	adapter := &recordingSubnetWriterAdapter{deleted: func(_ context.Context, got SubnetWriteSnapshot) error {
		called = true
		require.Equal(t, snapshot.UID, got.UID)
		require.True(t, got.Deleting)
		return nil
	}}
	reconciler := &SubnetReconciler{
		Reader:       newRuntimeClient(t),
		RecoveryGate: func(context.Context) error { return nil },
		Adapter:      adapter,
		Lease:        testSubnetWriterLease(t),
		deletes:      map[string][]SubnetWriteSnapshot{deleted.Name: {snapshot}},
	}

	_, err = reconciler.Reconcile(t.Context(), ctrl.Request{Name: deleted.Name})
	require.NoError(t, err)
	require.True(t, called)
	require.Empty(t, reconciler.deletes)
}

func TestSubnetReconcilerRetainsDeleteSnapshotWhenAdapterFails(t *testing.T) {
	deleted := testSubnet("")
	deleted.UID = types.UID("deleted-uid")
	deletionTimestamp := metav1.Now()
	deleted.DeletionTimestamp = &deletionTimestamp
	snapshot, err := NewSubnetWriteSnapshot(deleted, false)
	require.NoError(t, err)
	adapterErr := errors.New("delete cleanup failed")
	adapter := &recordingSubnetWriterAdapter{deleted: func(context.Context, SubnetWriteSnapshot) error {
		return adapterErr
	}}
	reconciler := &SubnetReconciler{
		Reader:       newRuntimeClient(t),
		RecoveryGate: func(context.Context) error { return nil },
		Adapter:      adapter,
		Lease:        testSubnetWriterLease(t),
		deletes:      map[string][]SubnetWriteSnapshot{deleted.Name: {snapshot}},
	}

	_, err = reconciler.Reconcile(t.Context(), ctrl.Request{Name: deleted.Name})
	require.ErrorIs(t, err, adapterErr)
	require.Equal(t, []SubnetWriteSnapshot{snapshot}, reconciler.deletes[deleted.Name])

	called := false
	adapter.deleted = func(_ context.Context, got SubnetWriteSnapshot) error {
		called = true
		require.Equal(t, snapshot.UID, got.UID)
		return nil
	}
	_, err = reconciler.Reconcile(t.Context(), ctrl.Request{Name: deleted.Name})
	require.NoError(t, err)
	require.True(t, called)
	require.Empty(t, reconciler.deletes)
}

func TestSubnetReconcilerRetainsDeleteSnapshotForReplacementUID(t *testing.T) {
	replacement := testSubnet("")
	replacement.UID = types.UID("replacement-uid")
	deleted := replacement.DeepCopy()
	deleted.UID = types.UID("deleted-uid")
	deletionTimestamp := metav1.Now()
	deleted.DeletionTimestamp = &deletionTimestamp
	snapshot, err := NewSubnetWriteSnapshot(deleted, false)
	require.NoError(t, err)
	called := false
	adapter := &recordingSubnetWriterAdapter{deleted: func(context.Context, SubnetWriteSnapshot) error {
		called = true
		return nil
	}}
	reconciler := &SubnetReconciler{
		Reader:       newRuntimeClient(t, replacement),
		RecoveryGate: func(context.Context) error { return nil },
		Adapter:      adapter,
		Lease:        testSubnetWriterLease(t),
		deletes:      map[string][]SubnetWriteSnapshot{deleted.Name: {snapshot}},
	}

	_, err = reconciler.Reconcile(t.Context(), ctrl.Request{Name: replacement.Name})
	require.NoError(t, err)
	require.False(t, called)
	require.Equal(t, []SubnetWriteSnapshot{snapshot}, reconciler.deletes[replacement.Name])
}

func TestSubnetReconcilerConsumesSameNameDeleteSnapshotsInUIDOrder(t *testing.T) {
	deletedA := testSubnet("")
	deletedA.UID = types.UID("deleted-uid-a")
	deletionTimestamp := metav1.Now()
	deletedA.DeletionTimestamp = &deletionTimestamp
	snapshotA, err := NewSubnetWriteSnapshot(deletedA, false)
	require.NoError(t, err)

	deletedB := deletedA.DeepCopy()
	deletedB.UID = types.UID("deleted-uid-b")
	snapshotB, err := NewSubnetWriteSnapshot(deletedB, false)
	require.NoError(t, err)

	var calls []types.UID
	adapter := &recordingSubnetWriterAdapter{deleted: func(_ context.Context, snapshot SubnetWriteSnapshot) error {
		calls = append(calls, snapshot.UID)
		return nil
	}}
	reconciler := &SubnetReconciler{
		Reader:       newRuntimeClient(t),
		RecoveryGate: func(context.Context) error { return nil },
		Adapter:      adapter,
		Lease:        testSubnetWriterLease(t),
	}
	reconciler.addDeleteSnapshot(deletedA.Name, snapshotA)
	reconciler.addDeleteSnapshot(deletedB.Name, snapshotB)

	result, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: deletedA.Name})
	require.NoError(t, err)
	require.True(t, result.Requeue)
	require.Equal(t, []types.UID{snapshotA.UID}, calls)

	result, err = reconciler.Reconcile(t.Context(), ctrl.Request{Name: deletedA.Name})
	require.NoError(t, err)
	require.Zero(t, result)
	require.Equal(t, []types.UID{snapshotA.UID, snapshotB.UID}, calls)
	require.Empty(t, reconciler.deletes)
}

func TestSubnetReconcilerRetainsDeleteSnapshotsForLiveReplacement(t *testing.T) {
	live := testSubnet("")
	live.UID = types.UID("live-uid")
	old := live.DeepCopy()
	old.UID = types.UID("old-uid")
	deletionTimestamp := metav1.Now()
	old.DeletionTimestamp = &deletionTimestamp
	oldSnapshot, err := NewSubnetWriteSnapshot(old, false)
	require.NoError(t, err)

	reconciler := &SubnetReconciler{}
	reconciler.addDeleteSnapshot(live.Name, oldSnapshot)
	reconciler.retainDeleteSnapshotForLive(live.Name, live)
	require.Equal(t, []SubnetWriteSnapshot{oldSnapshot}, reconciler.deletes[live.Name])
}

func TestSubnetReconcilerPropagatesRecoveryError(t *testing.T) {
	recoveryErr := errors.New("recovery pending")
	reconciler := &SubnetReconciler{
		Reader:       newRuntimeClient(t, testSubnet("")),
		RecoveryGate: func(context.Context) error { return recoveryErr },
		Adapter: subnetWriterAdapterFunc(func(context.Context, SubnetWriteSnapshot, SubnetShadowObservation) error {
			t.Fatal("adapter must not run before recovery")
			return nil
		}),
		Lease: testSubnetWriterLease(t),
	}
	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: "subnet-a"})
	require.ErrorIs(t, err, recoveryErr)
}

func testSubnetWriterLease(t *testing.T) SubnetWriterLease {
	t.Helper()
	lease, err := (&SubnetWriterOwnership{}).Claim(t.Context(), SubnetWriterOwnerRuntime)
	require.NoError(t, err)
	return lease
}

type subnetWriterAdapterFunc func(context.Context, SubnetWriteSnapshot, SubnetShadowObservation) error

func (f subnetWriterAdapterFunc) RecoverSubnet(context.Context, SubnetWriteSnapshot) error {
	return nil
}

func (f subnetWriterAdapterFunc) ReconcileSubnet(ctx context.Context, snapshot SubnetWriteSnapshot, observation SubnetShadowObservation) error {
	return f(ctx, snapshot, observation)
}

var _ SubnetWriterAdapter = subnetWriterAdapterFunc(nil)

type recordingSubnetWriterAdapter struct {
	recover   func(context.Context, SubnetWriteSnapshot) error
	reconcile func(context.Context, SubnetWriteSnapshot, SubnetShadowObservation) error
	deleted   func(context.Context, SubnetWriteSnapshot) error
}

func (a *recordingSubnetWriterAdapter) RecoverSubnet(ctx context.Context, snapshot SubnetWriteSnapshot) error {
	if a.recover == nil {
		return nil
	}
	return a.recover(ctx, snapshot)
}

func (a *recordingSubnetWriterAdapter) ReconcileSubnet(ctx context.Context, snapshot SubnetWriteSnapshot, observation SubnetShadowObservation) error {
	if a.reconcile == nil {
		return nil
	}
	return a.reconcile(ctx, snapshot, observation)
}

func (a *recordingSubnetWriterAdapter) ReconcileDeletedSubnet(ctx context.Context, snapshot SubnetWriteSnapshot) error {
	if a.deleted == nil {
		return nil
	}
	return a.deleted(ctx, snapshot)
}

var _ SubnetWriterAdapter = (*recordingSubnetWriterAdapter)(nil)

type countingSubnetReader struct {
	client.Reader
	subnetGets int
}

func (r *countingSubnetReader) Get(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*kubeovnv1.Subnet); ok {
		r.subnetGets++
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}
