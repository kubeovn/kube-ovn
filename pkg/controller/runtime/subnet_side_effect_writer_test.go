package controllerruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func newSubnetSideEffectWriter(t *testing.T) (*subnetSideEffectWriter, *kubeovnv1.Subnet, SubnetWriteSnapshot) {
	t.Helper()
	subnet := testSubnet("")
	subnet.UID = "uid-a"
	subnet.Generation = 3
	subnet.ResourceVersion = "7"
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	w := &subnetSideEffectWriter{
		ctx:  t.Context(),
		gate: SubnetWriterGate{Recovery: func(context.Context) error { return nil }},
	}
	return w, subnet, snapshot
}

func TestSubnetSideEffectWriterRunsEachOperationAfterGate(t *testing.T) {
	w, subnet, snapshot := newSubnetSideEffectWriter(t)
	operations := []struct {
		name string
		run  func(SubnetSideEffectAction) error
	}{
		{name: "OVN allocation", run: func(action SubnetSideEffectAction) error { return w.AllocateOVN(snapshot, subnet, action) }},
		{name: "IPAM allocation", run: func(action SubnetSideEffectAction) error { return w.AllocateIPAM(snapshot, subnet, action) }},
	}
	deletion := metav1.Now()
	terminating := subnet.DeepCopy()
	terminating.DeletionTimestamp = &deletion
	terminatingSnapshot, err := NewSubnetWriteSnapshot(terminating, false)
	require.NoError(t, err)
	operations = append(operations,
		struct {
			name string
			run  func(SubnetSideEffectAction) error
		}{name: "OVN cleanup", run: func(action SubnetSideEffectAction) error {
			return w.CleanupOVN(terminatingSnapshot, terminating, action)
		}},
		struct {
			name string
			run  func(SubnetSideEffectAction) error
		}{name: "IPAM release", run: func(action SubnetSideEffectAction) error {
			return w.ReleaseIPAM(terminatingSnapshot, terminating, action)
		}},
	)
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			calls := 0
			require.NoError(t, operation.run(func(context.Context) error {
				calls++
				return nil
			}))
			require.Equal(t, 1, calls)
		})
	}
}

func TestSubnetSideEffectWriterRejectsInvalidOperationsBeforeCallback(t *testing.T) {
	w, subnet, snapshot := newSubnetSideEffectWriter(t)
	tests := []struct {
		name string
		run  func(SubnetSideEffectAction) error
	}{
		{name: "OVN allocation denied", run: func(action SubnetSideEffectAction) error {
			snapshot.AllocationAllowed = false
			return w.AllocateOVN(snapshot, subnet, action)
		}},
		{name: "IPAM allocation denied", run: func(action SubnetSideEffectAction) error {
			snapshot.AllocationAllowed = false
			return w.AllocateIPAM(snapshot, subnet, action)
		}},
		{name: "cleanup active", run: func(action SubnetSideEffectAction) error { return w.CleanupOVN(snapshot, subnet, action) }},
		{name: "release active", run: func(action SubnetSideEffectAction) error { return w.ReleaseIPAM(snapshot, subnet, action) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			err := test.run(func(context.Context) error {
				calls++
				return nil
			})
			require.Error(t, err)
			require.Zero(t, calls)
		})
	}

	changed := subnet.DeepCopy()
	changed.ResourceVersion = "8"
	calls := 0
	err := w.AllocateOVN(snapshot, changed, func(context.Context) error { calls++; return nil })
	require.True(t, apierrors.IsConflict(err), err)
	require.Zero(t, calls)
}

func TestSubnetSideEffectWriterRejectsRecoveryFailureAndCancellation(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = "uid-a"
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	calls := 0
	w := &subnetSideEffectWriter{ctx: t.Context()}
	require.EqualError(t, w.AllocateIPAM(snapshot, subnet, func(context.Context) error { calls++; return nil }), "subnet writer recovery gate is not configured")
	require.Zero(t, calls)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	w.ctx = ctx
	require.ErrorIs(t, w.AllocateIPAM(snapshot, subnet, func(context.Context) error { calls++; return nil }), context.Canceled)
	require.Zero(t, calls)
}

func TestSubnetSideEffectWriterReturnsActionErrorAndRunsOnce(t *testing.T) {
	w, subnet, snapshot := newSubnetSideEffectWriter(t)
	want := errors.New("OVN transaction failed")
	calls := 0
	err := w.AllocateOVN(snapshot, subnet, func(context.Context) error {
		calls++
		return want
	})
	require.ErrorIs(t, err, want)
	require.Equal(t, 1, calls)

	require.EqualError(t, w.AllocateOVN(snapshot, subnet, nil), "subnet side effect action is not configured")
}

func TestSubnetSideEffectWriterReturnsCancellationAfterAction(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	w, subnet, snapshot := newSubnetSideEffectWriter(t)
	w.ctx = ctx
	calls := 0
	err := w.AllocateIPAM(snapshot, subnet, func(context.Context) error {
		calls++
		cancel()
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
}
