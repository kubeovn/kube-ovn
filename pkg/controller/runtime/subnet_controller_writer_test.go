package controllerruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

type recordingSubnetDataPlaneBackend struct {
	recover   int
	reconcile int
	deleting  bool
	last      *kubeovnv1.Subnet
}

func (b *recordingSubnetDataPlaneBackend) RecoverSubnetRuntime(context.Context, *kubeovnv1.Subnet) error {
	b.recover++
	return nil
}

func (b *recordingSubnetDataPlaneBackend) ReconcileSubnetRuntime(_ context.Context, subnet *kubeovnv1.Subnet) error {
	b.reconcile++
	b.last = subnet.DeepCopy()
	b.deleting = !subnet.DeletionTimestamp.IsZero()
	return nil
}

func TestSubnetControllerWriterAdapterDelegatesDomainPath(t *testing.T) {
	backend := &recordingSubnetDataPlaneBackend{}
	subnet := testSubnet("")
	adapter := &SubnetControllerWriterAdapter{Backend: backend, Reader: newRuntimeClient(t, subnet), Gate: SubnetWriterGate{Recovery: func(context.Context) error { return nil }}}
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	require.NoError(t, adapter.RecoverSubnet(t.Context(), snapshot))
	require.NoError(t, adapter.ReconcileSubnet(t.Context(), snapshot, SubnetShadowObservation{}))
	require.Equal(t, 1, backend.recover)
	require.Equal(t, 1, backend.reconcile)
	require.False(t, backend.deleting)
}

func TestSubnetControllerWriterAdapterPreservesTermination(t *testing.T) {
	backend := &recordingSubnetDataPlaneBackend{}
	subnet := testSubnet("")
	subnet.Finalizers = []string{"kube-ovn.io/subnet"}
	now := metav1.Now()
	subnet.DeletionTimestamp = &now
	adapter := &SubnetControllerWriterAdapter{Backend: backend, Reader: newRuntimeClient(t, subnet), Gate: SubnetWriterGate{Recovery: func(context.Context) error { return nil }}}
	snapshot, err := NewSubnetWriteSnapshot(subnet, false)
	require.NoError(t, err)
	require.NoError(t, adapter.ReconcileSubnet(t.Context(), snapshot, SubnetShadowObservation{}))
	require.True(t, backend.deleting)
}

func TestSubnetControllerWriterAdapterPreservesStatusAndFinalizers(t *testing.T) {
	backend := &recordingSubnetDataPlaneBackend{}
	subnet := testSubnet("")
	subnet.Finalizers = []string{"kube-ovn.io/subnet"}
	subnet.Status.GatewayMAC = "00:11:22:33:44:55"
	adapter := &SubnetControllerWriterAdapter{Backend: backend, Reader: newRuntimeClient(t, subnet), Gate: SubnetWriterGate{Recovery: func(context.Context) error { return nil }}}
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	require.NoError(t, adapter.ReconcileSubnet(t.Context(), snapshot, SubnetShadowObservation{}))
	require.Equal(t, subnet.Finalizers, backend.last.Finalizers)
	require.Equal(t, subnet.Status.GatewayMAC, backend.last.Status.GatewayMAC)
}

func TestSubnetControllerWriterAdapterRejectsStaleSnapshot(t *testing.T) {
	backend := &recordingSubnetDataPlaneBackend{}
	live := testSubnet("vlan-new")
	live.UID = types.UID("uid-new")
	live.Generation = 2
	live.ResourceVersion = "8"
	adapter := &SubnetControllerWriterAdapter{
		Backend: backend,
		Reader:  newRuntimeClient(t, live),
		Gate:    SubnetWriterGate{Recovery: func(context.Context) error { return nil }},
	}
	stale := live.DeepCopy()
	stale.UID = types.UID("uid-old")
	stale.Generation = 1
	stale.ResourceVersion = "7"
	stale.Spec.Vlan = "vlan-old"
	snapshot, err := NewSubnetWriteSnapshot(stale, true)
	require.NoError(t, err)

	err = adapter.ReconcileSubnet(t.Context(), snapshot, SubnetShadowObservation{})
	require.True(t, apierrors.IsConflict(err), err)
	require.Zero(t, backend.reconcile)
}

func TestSubnetControllerWriterAdapterFailsClosed(t *testing.T) {
	adapter := &SubnetControllerWriterAdapter{}
	snapshot, err := NewSubnetWriteSnapshot(testSubnet(""), true)
	require.NoError(t, err)
	require.EqualError(t, adapter.ReconcileSubnet(t.Context(), snapshot, SubnetShadowObservation{}), "subnet controller writer backend is not configured")
	adapter.Backend = &recordingSubnetDataPlaneBackend{}
	adapter.Reader = newRuntimeClient(t, testSubnet(""))
	adapter.Gate = SubnetWriterGate{Recovery: func(context.Context) error { return errors.New("recovery pending") }}
	require.ErrorContains(t, adapter.ReconcileSubnet(t.Context(), snapshot, SubnetShadowObservation{}), "recovery pending")
}
