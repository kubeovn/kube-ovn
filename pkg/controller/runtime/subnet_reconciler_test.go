package controllerruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestSubnetReconcilerFailsClosedWithoutDependencies(t *testing.T) {
	reconciler := &SubnetReconciler{}
	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: "subnet-a"})
	require.ErrorIs(t, err, ErrSubnetWriterRecoveryMissing)
	reconciler.RecoveryGate = func(context.Context) error { return nil }
	_, err = reconciler.Reconcile(t.Context(), ctrl.Request{Name: "subnet-a"})
	require.ErrorIs(t, err, ErrSubnetWriterAdapterMissing)
}

func TestSetupSubnetReconcilerFailsClosedBeforeManagerRegistration(t *testing.T) {
	require.ErrorIs(t, SetupSubnetReconciler(nil, nil, nil, nil), ErrSubnetWriterRecoveryMissing)
	require.ErrorIs(t, SetupSubnetReconciler(nil, nil, func(context.Context) error { return nil }, nil), ErrSubnetWriterAdapterMissing)
}

func TestSubnetReconcilerDelegatesImmutableSnapshot(t *testing.T) {
	subnet := testSubnet("")
	reader := newRuntimeClient(t, subnet)
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
	}

	result, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: subnet.Name})
	require.NoError(t, err)
	require.Zero(t, result)
	require.True(t, called)
	require.Equal(t, subnet.Name, gotSnapshot.Name)
	require.Equal(t, subnet.UID, gotSnapshot.UID)
	require.Equal(t, subnet.Spec, gotSnapshot.Spec)
	require.Equal(t, subnet.Name, gotObservation.Name)
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
	}
	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: "subnet-a"})
	require.ErrorIs(t, err, recoveryErr)
}

type subnetWriterAdapterFunc func(context.Context, SubnetWriteSnapshot, SubnetShadowObservation) error

func (f subnetWriterAdapterFunc) ReconcileSubnet(ctx context.Context, snapshot SubnetWriteSnapshot, observation SubnetShadowObservation) error {
	return f(ctx, snapshot, observation)
}

var _ SubnetWriterAdapter = subnetWriterAdapterFunc(nil)
