package controllerruntime

import (
	"context"
	"errors"

	"k8s.io/apimachinery/pkg/types"

	legacycontroller "github.com/kubeovn/kube-ovn/pkg/controller"
)

type subnetRuntimeIPAMBackend struct {
	backend legacycontroller.SubnetRuntimeIPAMAddressBackend
	owned   legacycontroller.SubnetRuntimeIPAMOwnedAddressBackend
	store   subnetIPAMReceiptStore
}

func newSubnetRuntimeIPAMBackend(backend legacycontroller.SubnetRuntimeIPAMAddressBackend, store subnetIPAMReceiptStore) (*subnetRuntimeIPAMBackend, error) {
	if backend == nil || store == nil {
		return nil, errors.New("subnet runtime IPAM backend requires an address backend and receipt store")
	}
	owned, _ := backend.(legacycontroller.SubnetRuntimeIPAMOwnedAddressBackend)
	return &subnetRuntimeIPAMBackend{backend: backend, owned: owned, store: store}, nil
}

func newStrictSubnetRuntimeIPAMBackend(backend legacycontroller.SubnetRuntimeIPAMAddressBackend, store subnetIPAMReceiptStore) (*subnetRuntimeIPAMBackend, error) {
	runtimeBackend, err := newSubnetRuntimeIPAMBackend(backend, store)
	if err != nil {
		return nil, err
	}
	if runtimeBackend.owned == nil {
		return nil, errors.New("subnet runtime IPAM backend does not provide owner fencing")
	}
	return runtimeBackend, nil
}

func (b *subnetRuntimeIPAMBackend) OwnerFencingEnabled() bool {
	return b.owned != nil
}

func (b *subnetRuntimeIPAMBackend) ExistingAllocation(ctx context.Context, subnetName, podName, nicName string) (subnetIPAMAddress, bool, error) {
	result, found, err := b.backend.ExistingSubnetRuntimeIPAMAddress(ctx, podName, nicName, subnetName)
	return subnetIPAMAddress{v4: result.V4, v6: result.V6, mac: result.MAC}, found, err
}

func (b *subnetRuntimeIPAMBackend) ExistingAllocationOwner(ctx context.Context, subnetName, podName, nicName string) (subnetIPAMAllocation, bool, error) {
	if b.owned == nil {
		return subnetIPAMAllocation{}, false, ErrSubnetIPAMRecoveryUnavailable
	}
	address, found, err := b.ExistingAllocation(ctx, subnetName, podName, nicName)
	if err != nil || !found {
		return subnetIPAMAllocation{}, found, err
	}
	owner, ownerFound, err := b.owned.ExistingSubnetRuntimeIPAMOwner(ctx, podName, nicName, subnetName)
	if err != nil {
		return subnetIPAMAllocation{}, false, err
	}
	if !ownerFound {
		return subnetIPAMAllocation{}, false, ErrSubnetIPAMRecoveryUnsafe
	}
	return subnetIPAMAllocation{
		key:      subnetIPAMKey{subnet: subnetName, pod: podName, nic: nicName},
		token:    owner.Token,
		uid:      owner.SubnetUID,
		address:  address,
		identity: SubnetMutationIdentity{Name: subnetName, UID: types.UID(owner.SubnetUID), Generation: owner.Generation, ResourceVersion: owner.ResourceVersion, Operation: SubnetIPAMAddressAllocateOperation, WriterEpoch: owner.WriterEpoch},
	}, true, nil
}

func (b *subnetRuntimeIPAMBackend) BindIdentity(ctx context.Context, snapshot SubnetWriteSnapshot) error {
	binder, ok := b.backend.(legacycontroller.SubnetRuntimeIPAMIdentityBinder)
	if !ok {
		return ErrSubnetIPAMRecoveryUnavailable
	}
	return binder.BindSubnetRuntimeIPAMIdentity(ctx, snapshot.Name, string(snapshot.UID))
}

func (b *subnetRuntimeIPAMBackend) ListExisting(ctx context.Context, snapshot SubnetWriteSnapshot) ([]subnetIPAMAllocation, error) {
	var allocations []legacycontroller.SubnetRuntimeIPAMAddressAllocation
	var err error
	if b.owned != nil {
		allocations, err = b.owned.ListSubnetRuntimeIPAMOwnedAddresses(ctx, snapshot.Name)
	} else {
		allocations, err = b.backend.ListSubnetRuntimeIPAMAddresses(ctx, snapshot.Name)
	}
	if err != nil {
		return nil, err
	}
	result := make([]subnetIPAMAllocation, 0, len(allocations))
	for _, allocation := range allocations {
		converted := subnetIPAMAllocation{
			key:     subnetIPAMKey{subnet: snapshot.Name, pod: allocation.PodName, nic: allocation.NICName},
			uid:     allocation.SubnetUID,
			address: subnetIPAMAddress{v4: allocation.V4, v6: allocation.V6, mac: allocation.MAC},
		}
		if b.owned != nil {
			converted.token = allocation.Owner.Token
			converted.identity = SubnetMutationIdentity{Name: snapshot.Name, UID: types.UID(allocation.Owner.SubnetUID), Generation: allocation.Owner.Generation, ResourceVersion: allocation.Owner.ResourceVersion, Operation: SubnetIPAMAddressAllocateOperation, WriterEpoch: allocation.Owner.WriterEpoch}
		}
		result = append(result, converted)
	}
	return result, nil
}

func (b *subnetRuntimeIPAMBackend) AllocateRandom(ctx context.Context, podName, nicName, subnetName, poolName string) (subnetIPAMAddress, error) {
	result, err := b.backend.AllocateSubnetRuntimeIPAMRandom(ctx, podName, nicName, subnetName, poolName, nil, true)
	return subnetIPAMAddress{v4: result.V4, v6: result.V6, mac: result.MAC}, err
}

func (b *subnetRuntimeIPAMBackend) AllocateRandomOwned(ctx context.Context, allocation subnetIPAMAllocation, poolName string) (subnetIPAMAddress, error) {
	if b.owned == nil {
		return subnetIPAMAddress{}, ErrSubnetIPAMRecoveryUnavailable
	}
	owner := legacycontroller.SubnetRuntimeIPAMOwner{Token: allocation.token, SubnetUID: allocation.uid, Generation: allocation.identity.Generation, ResourceVersion: allocation.identity.ResourceVersion, WriterEpoch: allocation.identity.WriterEpoch}
	result, err := b.owned.AllocateSubnetRuntimeIPAMRandomOwned(ctx, allocation.key.pod, allocation.key.nic, allocation.key.subnet, poolName, nil, true, owner)
	return subnetIPAMAddress{v4: result.V4, v6: result.V6, mac: result.MAC}, err
}

func (b *subnetRuntimeIPAMBackend) AllocateStatic(ctx context.Context, podName, nicName, requestedIP string, mac *string, subnetName string, checkConflict bool) (subnetIPAMAddress, error) {
	result, err := b.backend.AllocateSubnetRuntimeIPAMStatic(ctx, podName, nicName, requestedIP, mac, subnetName, checkConflict)
	return subnetIPAMAddress{v4: result.V4, v6: result.V6, mac: result.MAC}, err
}

func (b *subnetRuntimeIPAMBackend) AllocateStaticOwned(ctx context.Context, allocation subnetIPAMAllocation, requestedIP string, mac *string, checkConflict bool) (subnetIPAMAddress, error) {
	if b.owned == nil {
		return subnetIPAMAddress{}, ErrSubnetIPAMRecoveryUnavailable
	}
	owner := legacycontroller.SubnetRuntimeIPAMOwner{Token: allocation.token, SubnetUID: allocation.uid, Generation: allocation.identity.Generation, ResourceVersion: allocation.identity.ResourceVersion, WriterEpoch: allocation.identity.WriterEpoch}
	result, err := b.owned.AllocateSubnetRuntimeIPAMStaticOwned(ctx, allocation.key.pod, allocation.key.nic, requestedIP, mac, allocation.key.subnet, checkConflict, owner)
	return subnetIPAMAddress{v4: result.V4, v6: result.V6, mac: result.MAC}, err
}

func (b *subnetRuntimeIPAMBackend) ReleaseIfMatches(ctx context.Context, allocation subnetIPAMAllocation) error {
	if b.owned != nil {
		owner := legacycontroller.SubnetRuntimeIPAMOwner{Token: allocation.token, SubnetUID: allocation.uid, Generation: allocation.identity.Generation, ResourceVersion: allocation.identity.ResourceVersion, WriterEpoch: allocation.identity.WriterEpoch}
		return b.owned.ReleaseSubnetRuntimeIPAMByNicIfMatchesOwned(ctx, allocation.key.pod, allocation.key.nic, allocation.key.subnet, allocation.address.v4, allocation.address.v6, allocation.address.mac, owner)
	}
	return b.backend.ReleaseSubnetRuntimeIPAMByNicIfMatches(ctx, allocation.key.pod, allocation.key.nic, allocation.key.subnet, allocation.address.v4, allocation.address.v6, allocation.address.mac)
}

func (b *subnetRuntimeIPAMBackend) BindOwnerIfUnowned(ctx context.Context, allocation subnetIPAMAllocation) error {
	if b.owned == nil {
		return ErrSubnetIPAMRecoveryUnavailable
	}
	owner := legacycontroller.SubnetRuntimeIPAMOwner{Token: allocation.token, SubnetUID: allocation.uid, Generation: allocation.identity.Generation, ResourceVersion: allocation.identity.ResourceVersion, WriterEpoch: allocation.identity.WriterEpoch}
	return b.owned.BindSubnetRuntimeIPAMOwnerIfUnowned(ctx, allocation.key.pod, allocation.key.nic, allocation.key.subnet, allocation.address.v4, allocation.address.v6, allocation.address.mac, owner)
}

func (b *subnetRuntimeIPAMBackend) RebindOwner(ctx context.Context, previous, replacement subnetIPAMAllocation) error {
	if b.owned == nil {
		return ErrSubnetIPAMRecoveryUnavailable
	}
	oldOwner := legacycontroller.SubnetRuntimeIPAMOwner{Token: previous.token, SubnetUID: previous.uid, Generation: previous.identity.Generation, ResourceVersion: previous.identity.ResourceVersion, WriterEpoch: previous.identity.WriterEpoch}
	newOwner := legacycontroller.SubnetRuntimeIPAMOwner{Token: replacement.token, SubnetUID: replacement.uid, Generation: replacement.identity.Generation, ResourceVersion: replacement.identity.ResourceVersion, WriterEpoch: replacement.identity.WriterEpoch}
	return b.owned.RebindSubnetRuntimeIPAMOwner(ctx, previous.key.pod, previous.key.nic, previous.key.subnet, previous.address.v4, previous.address.v6, previous.address.mac, oldOwner, newOwner)
}

func (b *subnetRuntimeIPAMBackend) ReleaseByPod(ctx context.Context, podName, subnetName string) error {
	return b.backend.ReleaseSubnetRuntimeIPAMByPod(ctx, podName, subnetName)
}

func (b *subnetRuntimeIPAMBackend) Recover(ctx context.Context, snapshot SubnetWriteSnapshot) ([]subnetIPAMAllocation, error) {
	lister, ok := b.store.(subnetIPAMReceiptLister)
	if !ok {
		return nil, ErrSubnetIPAMRecoveryUnavailable
	}
	allocations, err := lister.List(ctx, snapshot.Name)
	if err != nil {
		return nil, err
	}
	recovered := make([]subnetIPAMAllocation, 0, len(allocations))
	for _, allocation := range allocations {
		if allocation.uid != string(snapshot.UID) || allocation.identity.Name != snapshot.Name || allocation.identity.UID != snapshot.UID ||
			allocation.identity.Operation != SubnetIPAMAddressAllocateOperation {
			return nil, ErrSubnetIPAMRecoveryUnsafe
		}
		current, found, err := b.ExistingAllocation(ctx, allocation.key.subnet, allocation.key.pod, allocation.key.nic)
		if err != nil {
			return nil, err
		}
		if !found || current != allocation.address {
			if !found && allocation.phase == subnetIPAMReceiptPhaseReleasing {
				if err := lister.Delete(ctx, allocation); err != nil {
					return nil, err
				}
				continue
			}
			return nil, ErrSubnetIPAMRecoveryUnsafe
		}
		recovered = append(recovered, allocation)
	}
	return recovered, nil
}

var _ subnetIPAMAddressBackend = (*subnetRuntimeIPAMBackend)(nil)
