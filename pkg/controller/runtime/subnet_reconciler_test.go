package controllerruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
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
	require.ErrorIs(t, SetupSubnetReconciler(nil, nil, func(context.Context) error { return nil }, &recordingSubnetWriterAdapter{}), ErrSubnetWriterManagerMissing)
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
	}

	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: "subnet-a"})
	require.ErrorIs(t, err, recoveryErr)
	require.False(t, reconciled)
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
