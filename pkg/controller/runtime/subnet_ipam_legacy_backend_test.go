package controllerruntime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubnetRuntimeIPAMBackendRecoveryDropsReleasedReceipt(t *testing.T) {
	_, _, _, snapshot := newSubnetSideEffectWriterAPI(t, false)
	allocation := testDurableSubnetIPAMAllocation()
	allocation.uid = string(snapshot.UID)
	allocation.key.subnet = snapshot.Name
	allocation.identity = SubnetMutationIdentity{
		Name:            snapshot.Name,
		UID:             snapshot.UID,
		Generation:      snapshot.Generation,
		ResourceVersion: snapshot.ResourceVersion,
		Operation:       SubnetIPAMAddressAllocateOperation,
		WriterEpoch:     "old-epoch",
	}
	allocation.phase = subnetIPAMReceiptPhaseReleasing

	store := newMemorySubnetIPAMReceiptStore()
	require.NoError(t, store.Remember(t.Context(), allocation))
	backend, err := newSubnetRuntimeIPAMBackend(&recordingSubnetIPAMAddressBackend{}, store)
	require.NoError(t, err)

	recovered, err := backend.Recover(t.Context(), snapshot)
	require.NoError(t, err)
	require.Empty(t, recovered)
	_, found, err := store.Lookup(t.Context(), allocation.key)
	require.NoError(t, err)
	require.False(t, found)
}
