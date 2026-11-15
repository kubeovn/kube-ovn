package controllerruntime

import (
	"context"
	"errors"

	legacycontroller "github.com/kubeovn/kube-ovn/pkg/controller"
)

type subnetRuntimeIPAMBackend struct {
	backend legacycontroller.SubnetRuntimeIPAMAddressBackend
	store   subnetIPAMReceiptStore
}

func newSubnetRuntimeIPAMBackend(backend legacycontroller.SubnetRuntimeIPAMAddressBackend, store subnetIPAMReceiptStore) (*subnetRuntimeIPAMBackend, error) {
	if backend == nil || store == nil {
		return nil, errors.New("subnet runtime IPAM backend requires an address backend and receipt store")
	}
	return &subnetRuntimeIPAMBackend{backend: backend, store: store}, nil
}

func (b *subnetRuntimeIPAMBackend) ExistingAllocation(ctx context.Context, subnetName, podName, nicName string) (subnetIPAMAddress, bool, error) {
	result, found, err := b.backend.ExistingSubnetRuntimeIPAMAddress(ctx, podName, nicName, subnetName)
	return subnetIPAMAddress{v4: result.V4, v6: result.V6, mac: result.MAC}, found, err
}

func (b *subnetRuntimeIPAMBackend) BindIdentity(ctx context.Context, snapshot SubnetWriteSnapshot) error {
	binder, ok := b.backend.(legacycontroller.SubnetRuntimeIPAMIdentityBinder)
	if !ok {
		return ErrSubnetIPAMRecoveryUnavailable
	}
	return binder.BindSubnetRuntimeIPAMIdentity(ctx, snapshot.Name, string(snapshot.UID))
}

func (b *subnetRuntimeIPAMBackend) ListExisting(ctx context.Context, snapshot SubnetWriteSnapshot) ([]subnetIPAMAllocation, error) {
	allocations, err := b.backend.ListSubnetRuntimeIPAMAddresses(ctx, snapshot.Name)
	if err != nil {
		return nil, err
	}
	result := make([]subnetIPAMAllocation, 0, len(allocations))
	for _, allocation := range allocations {
		result = append(result, subnetIPAMAllocation{
			key:     subnetIPAMKey{subnet: snapshot.Name, pod: allocation.PodName, nic: allocation.NICName},
			uid:     allocation.SubnetUID,
			address: subnetIPAMAddress{v4: allocation.V4, v6: allocation.V6, mac: allocation.MAC},
		})
	}
	return result, nil
}

func (b *subnetRuntimeIPAMBackend) AllocateRandom(ctx context.Context, podName, nicName, subnetName, poolName string) (subnetIPAMAddress, error) {
	result, err := b.backend.AllocateSubnetRuntimeIPAMRandom(ctx, podName, nicName, subnetName, poolName, nil, true)
	return subnetIPAMAddress{v4: result.V4, v6: result.V6, mac: result.MAC}, err
}

func (b *subnetRuntimeIPAMBackend) AllocateStatic(ctx context.Context, podName, nicName, requestedIP string, mac *string, subnetName string, checkConflict bool) (subnetIPAMAddress, error) {
	result, err := b.backend.AllocateSubnetRuntimeIPAMStatic(ctx, podName, nicName, requestedIP, mac, subnetName, checkConflict)
	return subnetIPAMAddress{v4: result.V4, v6: result.V6, mac: result.MAC}, err
}

func (b *subnetRuntimeIPAMBackend) ReleaseIfMatches(ctx context.Context, allocation subnetIPAMAllocation) error {
	return b.backend.ReleaseSubnetRuntimeIPAMByNicIfMatches(ctx, allocation.key.pod, allocation.key.nic, allocation.key.subnet, allocation.address.v4, allocation.address.v6, allocation.address.mac)
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
			return nil, ErrSubnetIPAMRecoveryUnsafe
		}
	}
	return allocations, nil
}

var _ subnetIPAMAddressBackend = (*subnetRuntimeIPAMBackend)(nil)
