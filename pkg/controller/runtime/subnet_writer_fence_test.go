package controllerruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	coordv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestKubernetesSubnetWriterFenceAcquiresAndChecksLease(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	fence, err := NewKubernetesSubnetWriterFence(client.CoordinationV1().Leases("kube-system"), "subnet-writer", "pod-a", time.Minute)
	require.NoError(t, err)
	require.NoError(t, fence.Acquire(t.Context()))
	require.NoError(t, fence.Check(t.Context()))

	lease, err := client.CoordinationV1().Leases("kube-system").Get(t.Context(), "subnet-writer", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "pod-a", *lease.Spec.HolderIdentity)

	other, err := NewKubernetesSubnetWriterFence(client.CoordinationV1().Leases("kube-system"), "subnet-writer", "pod-b", time.Minute)
	require.NoError(t, err)
	require.ErrorIs(t, other.Acquire(t.Context()), ErrSubnetWriterFenceBusy)
}

func TestKubernetesSubnetWriterFenceTakesOverExpiredLease(t *testing.T) {
	old := metav1.NewMicroTime(time.Now().Add(-2 * time.Minute))
	duration := int32(30)
	holder := "pod-a"
	lease := &coordv1.Lease{Name: "subnet-writer", Spec: coordv1.LeaseSpec{
		HolderIdentity: &holder, LeaseDurationSeconds: &duration, RenewTime: &old,
	}}
	client := k8sfake.NewSimpleClientset(lease)
	fence, err := NewKubernetesSubnetWriterFence(client.CoordinationV1().Leases("kube-system"), "subnet-writer", "pod-b", time.Minute)
	require.NoError(t, err)
	require.NoError(t, fence.Acquire(t.Context()))
	require.NoError(t, fence.Check(t.Context()))
}

func TestKubernetesSubnetWriterFenceStopsOnContextCancel(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	fence, err := NewKubernetesSubnetWriterFence(client.CoordinationV1().Leases("kube-system"), "subnet-writer", "pod-a", time.Minute)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, fence.Start(ctx))
	require.NoError(t, fence.Check(t.Context()))
	cancel()
	require.Eventually(t, func() bool {
		return errors.Is(fence.Check(t.Context()), ErrSubnetWriterFenceInactive)
	}, time.Second, time.Millisecond)
}

func TestSubnetWriterGateFailsClosedWhenDurableFenceIsLost(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	fence, err := NewKubernetesSubnetWriterFence(client.CoordinationV1().Leases("kube-system"), "subnet-writer", "pod-a", time.Minute)
	require.NoError(t, err)
	require.NoError(t, fence.Acquire(t.Context()))
	gate := SubnetWriterGate{Recovery: func(context.Context) error { return nil }, Fence: fence}
	require.NoError(t, gate.WaitForRecovery(t.Context()))
	fence.Stop()
	require.ErrorIs(t, gate.WaitForRecovery(t.Context()), ErrSubnetWriterFenceInactive)
}

func TestKubernetesSubnetWriterFenceSameIdentityRestartUsesNewEpoch(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	leases := client.CoordinationV1().Leases("kube-system")
	first, err := NewKubernetesSubnetWriterFence(leases, "subnet-writer", "pod-a", time.Minute)
	require.NoError(t, err)
	require.NoError(t, first.Acquire(t.Context()))
	stored, err := leases.Get(t.Context(), "subnet-writer", metav1.GetOptions{})
	require.NoError(t, err)
	firstEpoch := stored.Annotations[subnetWriterFenceEpochAnnotation]
	require.NotEmpty(t, firstEpoch)

	restarted, err := NewKubernetesSubnetWriterFence(leases, "subnet-writer", "pod-a", time.Minute)
	require.NoError(t, err)
	require.ErrorIs(t, restarted.Acquire(t.Context()), ErrSubnetWriterFenceBusy)

	first.Stop()
	require.ErrorIs(t, restarted.Acquire(t.Context()), ErrSubnetWriterFenceBusy)
	restarted.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	require.NoError(t, restarted.Acquire(t.Context()))
	stored, err = leases.Get(t.Context(), "subnet-writer", metav1.GetOptions{})
	require.NoError(t, err)
	require.NotEqual(t, firstEpoch, stored.Annotations[subnetWriterFenceEpochAnnotation])
	require.NoError(t, restarted.Check(t.Context()))
}

func TestKubernetesSubnetWriterFenceCheckRechecksActiveAfterLeaseRead(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	fence, err := NewKubernetesSubnetWriterFence(client.CoordinationV1().Leases("kube-system"), "subnet-writer", "pod-a", time.Minute)
	require.NoError(t, err)
	require.NoError(t, fence.Acquire(t.Context()))

	getStarted := make(chan struct{})
	releaseGet := make(chan struct{})
	client.PrependReactor("get", "leases", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		close(getStarted)
		<-releaseGet
		return false, nil, nil
	})
	result := make(chan error, 1)
	go func() { result <- fence.Check(t.Context()) }()
	<-getStarted
	fence.Stop()
	close(releaseGet)
	require.ErrorIs(t, <-result, ErrSubnetWriterFenceInactive)
}
