package controllerruntime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestNewSubnetWriteSnapshotCopiesIdentityAndSpec(t *testing.T) {
	subnet := testSubnet("vlan-a")
	subnet.UID = types.UID("uid-a")
	subnet.Generation = 4
	subnet.ResourceVersion = "7"
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)

	subnet.Spec.ExcludeIps = []string{"10.0.0.1"}
	require.Equal(t, types.UID("uid-a"), snapshot.UID)
	require.Equal(t, int64(4), snapshot.Generation)
	require.Equal(t, "7", snapshot.ResourceVersion)
	require.Empty(t, snapshot.Spec.ExcludeIps)
	require.True(t, snapshot.AllocationAllowed)
}

func TestSubnetWriteSnapshotRejectsLiveConflicts(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = types.UID("uid-a")
	subnet.Generation = 2
	subnet.ResourceVersion = "3"
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)

	tests := []struct {
		name   string
		change func(*kubeovnv1.Subnet)
	}{
		{name: "replacement UID", change: func(current *kubeovnv1.Subnet) { current.UID = types.UID("uid-b") }},
		{name: "generation", change: func(current *kubeovnv1.Subnet) { current.Generation++ }},
		{name: "resource version", change: func(current *kubeovnv1.Subnet) { current.ResourceVersion = "4" }},
		{name: "spec", change: func(current *kubeovnv1.Subnet) { current.Spec.Vlan = "vlan-a" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current := subnet.DeepCopy()
			test.change(current)
			err := snapshot.Validate(current, SubnetStatusWriteOperation)
			require.True(t, apierrors.IsConflict(err), err)
		})
	}
}

func TestSubnetWriteSnapshotGatesOperations(t *testing.T) {
	active := testSubnet("")
	active.UID = types.UID("uid-a")
	active.ResourceVersion = "1"
	activeSnapshot, err := NewSubnetWriteSnapshot(active, true)
	require.NoError(t, err)

	for _, operation := range []SubnetWriteOperation{
		SubnetStatusWriteOperation,
		SubnetFinalizerAddOperation,
		SubnetOVNAllocateOperation,
		SubnetIPAMAllocateOperation,
	} {
		require.NoError(t, activeSnapshot.Validate(active, operation), operation)
	}

	terminating := active.DeepCopy()
	deletionTimestamp := metav1.Now()
	terminating.DeletionTimestamp = &deletionTimestamp
	terminatingSnapshot, err := NewSubnetWriteSnapshot(terminating, false)
	require.NoError(t, err)
	for _, operation := range []SubnetWriteOperation{
		SubnetFinalizerRemoveOperation,
		SubnetOVNCleanupOperation,
		SubnetIPAMReleaseOperation,
	} {
		require.NoError(t, terminatingSnapshot.Validate(terminating, operation), operation)
	}
	for _, operation := range []SubnetWriteOperation{
		SubnetStatusWriteOperation,
		SubnetFinalizerAddOperation,
		SubnetOVNAllocateOperation,
		SubnetIPAMAllocateOperation,
	} {
		require.Error(t, terminatingSnapshot.Validate(terminating, operation), operation)
	}
}

func TestSubnetWriteSnapshotRequiresAllocationGate(t *testing.T) {
	subnet := testSubnet("")
	snapshot, err := NewSubnetWriteSnapshot(subnet, false)
	require.NoError(t, err)
	require.Error(t, snapshot.Validate(subnet, SubnetOVNAllocateOperation))
	require.Error(t, snapshot.Validate(subnet, SubnetIPAMAllocateOperation))
}

func TestSubnetWriterGateFailsClosedAndWaitsForRecovery(t *testing.T) {
	subnet := testSubnet("")
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)

	gate := SubnetWriterGate{}
	require.EqualError(t, gate.Validate(t.Context(), snapshot, subnet, SubnetStatusWriteOperation), "subnet writer recovery gate is not configured")

	var recovered bool
	gate.Recovery = func(context.Context) error {
		recovered = true
		return nil
	}
	require.NoError(t, gate.Validate(t.Context(), snapshot, subnet, SubnetStatusWriteOperation))
	require.True(t, recovered)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, gate.WaitForRecovery(ctx), context.Canceled)
}
