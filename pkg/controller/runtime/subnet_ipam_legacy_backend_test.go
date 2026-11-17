package controllerruntime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	legacycontroller "github.com/kubeovn/kube-ovn/pkg/controller"
)

type ownedSubnetIPAMBackendStub struct {
	recordingSubnetIPAMAddressBackend
	owner        legacycontroller.SubnetRuntimeIPAMOwner
	ownerCalls   int
	releaseOwner legacycontroller.SubnetRuntimeIPAMOwner
}

func (b *ownedSubnetIPAMBackendStub) OwnerFencingEnabled() bool { return true }

func (b *ownedSubnetIPAMBackendStub) ExistingSubnetRuntimeIPAMOwner(context.Context, string, string, string) (legacycontroller.SubnetRuntimeIPAMOwner, bool, error) {
	return b.owner, b.owner.Token != 0, nil
}

func (b *ownedSubnetIPAMBackendStub) ListSubnetRuntimeIPAMOwnedAddresses(context.Context, string) ([]legacycontroller.SubnetRuntimeIPAMAddressAllocation, error) {
	return nil, nil
}

func (b *ownedSubnetIPAMBackendStub) AllocateSubnetRuntimeIPAMRandomOwned(context.Context, string, string, string, string, []string, bool, legacycontroller.SubnetRuntimeIPAMOwner) (legacycontroller.SubnetRuntimeIPAMAddressResult, error) {
	return b.allocateResult, nil
}

func (b *ownedSubnetIPAMBackendStub) AllocateSubnetRuntimeIPAMStaticOwned(context.Context, string, string, string, *string, string, bool, legacycontroller.SubnetRuntimeIPAMOwner) (legacycontroller.SubnetRuntimeIPAMAddressResult, error) {
	return b.allocateResult, nil
}

func (b *ownedSubnetIPAMBackendStub) ReleaseSubnetRuntimeIPAMByNicIfMatchesOwned(_ context.Context, _, _, _, _, _, _ string, owner legacycontroller.SubnetRuntimeIPAMOwner) error {
	b.releaseOwner = owner
	return nil
}

func (b *ownedSubnetIPAMBackendStub) BindSubnetRuntimeIPAMOwnerIfUnowned(context.Context, string, string, string, string, string, string, legacycontroller.SubnetRuntimeIPAMOwner) error {
	b.ownerCalls++
	return nil
}

func (b *ownedSubnetIPAMBackendStub) RebindSubnetRuntimeIPAMOwner(context.Context, string, string, string, string, string, string, legacycontroller.SubnetRuntimeIPAMOwner, legacycontroller.SubnetRuntimeIPAMOwner) error {
	b.ownerCalls++
	return nil
}

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

func TestStrictSubnetRuntimeIPAMBackendRequiresOwnerFencing(t *testing.T) {
	store := newMemorySubnetIPAMReceiptStore()
	_, err := newStrictSubnetRuntimeIPAMBackend(&recordingSubnetIPAMAddressBackend{}, store)
	require.EqualError(t, err, "subnet runtime IPAM backend does not provide owner fencing")
	owned := &ownedSubnetIPAMBackendStub{}
	backend, err := newStrictSubnetRuntimeIPAMBackend(owned, store)
	require.NoError(t, err)
	require.NotNil(t, backend)
}

func TestSubnetRuntimeIPAMBackendPassesOwnerToRelease(t *testing.T) {
	owned := &ownedSubnetIPAMBackendStub{}
	backend, err := newSubnetRuntimeIPAMBackend(owned, newMemorySubnetIPAMReceiptStore())
	require.NoError(t, err)
	allocation := testDurableSubnetIPAMAllocation()
	require.NoError(t, backend.ReleaseIfMatches(t.Context(), allocation))
	require.Equal(t, legacycontroller.SubnetRuntimeIPAMOwner{
		Token: allocation.token, SubnetUID: allocation.uid,
		Generation: allocation.identity.Generation, ResourceVersion: allocation.identity.ResourceVersion,
		WriterEpoch: allocation.identity.WriterEpoch,
	}, owned.releaseOwner)
}
