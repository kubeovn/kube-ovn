package controllerruntime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/kubeovn/kube-ovn/pkg/ipam"
)

// subnetIPAMAdapter is an unwired adapter for one IPAM allocation operation.
// It uses IPAM transactions for atomic staging and keeps a process-local owner
// token so a receipt cannot release an allocation claimed by a later owner.
// Durable fencing still belongs to the future production adapter.
type subnetIPAMAdapter struct {
	writer  *subnetSideEffectWriter
	backend subnetIPAMBackend
	allocMu sync.Mutex
	mu      sync.Mutex
	next    atomic.Uint64
	owners  map[subnetIPAMKey]subnetIPAMAllocation
}

// subnetIPAMBackend is the narrow IPAM seam consumed by the future Subnet
// writer. A production implementation must preserve the same no-op,
// transaction and uncertain-commit semantics before it can be wired in.
type subnetIPAMBackend interface {
	ExistingAllocation(ctx context.Context, subnetName, podName, nicName string) (subnetIPAMAddress, bool, error)
	AllocateRandom(ctx context.Context, podName, nicName, subnetName, poolName string) (subnetIPAMAddress, error)
	// ReleaseIfMatches receives the complete allocation identity so a durable
	// backend can fence by key, Subnet UID, and ownership token in addition to
	// the address tuple.
	ReleaseIfMatches(ctx context.Context, allocation subnetIPAMAllocation) error
}

type subnetIPAMAddress struct {
	v4  string
	v6  string
	mac string
}

type transactionalSubnetIPAMBackend struct {
	ipam *ipam.IPAM
}

func (b *transactionalSubnetIPAMBackend) ExistingAllocation(ctx context.Context, subnetName, podName, nicName string) (subnetIPAMAddress, bool, error) {
	if err := ctx.Err(); err != nil {
		return subnetIPAMAddress{}, false, err
	}
	v4, v6, mac, found := b.ipam.GetPodAddressByNic(podName, nicName, subnetName)
	return subnetIPAMAddress{v4: v4, v6: v6, mac: mac}, found, nil
}

func (b *transactionalSubnetIPAMBackend) AllocateRandom(ctx context.Context, podName, nicName, subnetName, poolName string) (subnetIPAMAddress, error) {
	var address subnetIPAMAddress
	err := b.ipam.WithTransactionContext(ctx, func(tx *ipam.Transaction) error {
		var err error
		address.v4, address.v6, address.mac, err = tx.GetRandomAddress(podName, nicName, nil, subnetName, poolName, nil, true)
		return err
	})
	return address, err
}

func (b *transactionalSubnetIPAMBackend) ReleaseIfMatches(ctx context.Context, allocation subnetIPAMAllocation) error {
	return b.ipam.WithTransactionContext(ctx, func(tx *ipam.Transaction) error {
		return tx.ReleaseAddressByNicIfMatches(allocation.key.pod, allocation.key.nic, allocation.key.subnet,
			allocation.address.v4, allocation.address.v6, allocation.address.mac)
	})
}

type subnetIPAMKey struct {
	subnet string
	pod    string
	nic    string
}

type subnetIPAMAllocation struct {
	key     subnetIPAMKey
	token   uint64
	uid     string
	address subnetIPAMAddress
}

func newSubnetIPAMAdapter(writer *subnetSideEffectWriter, allocation *ipam.IPAM) (*subnetIPAMAdapter, error) {
	if allocation == nil {
		return nil, errors.New("subnet IPAM adapter requires an IPAM")
	}
	return newSubnetIPAMAdapterWithBackend(writer, &transactionalSubnetIPAMBackend{ipam: allocation})
}

func newSubnetIPAMAdapterWithBackend(writer *subnetSideEffectWriter, backend subnetIPAMBackend) (*subnetIPAMAdapter, error) {
	if writer == nil || backend == nil {
		return nil, errors.New("subnet IPAM adapter requires a writer and backend")
	}
	return &subnetIPAMAdapter{writer: writer, backend: backend, owners: make(map[subnetIPAMKey]subnetIPAMAllocation)}, nil
}

// AllocateRandom allocates one IPAM address through an atomic IPAM
// transaction. Existing allocations are returned as a no-op without a
// compensation receipt.
func (a *subnetIPAMAdapter) AllocateRandom(snapshot SubnetWriteSnapshot, podName, nicName, poolName string) (string, string, string, SubnetSideEffectReceipt, error) {
	// Serialize the precheck, backend transaction, and owner publication. This
	// closes the gap where concurrent callers could both observe no allocation
	// and race before one of them publishes its receipt owner.
	a.allocMu.Lock()
	defer a.allocMu.Unlock()

	key := subnetIPAMKey{subnet: snapshot.Name, pod: podName, nic: nicName}
	allocation := subnetIPAMAllocation{key: key, uid: string(snapshot.UID)}
	var noOp bool
	plan := SubnetSideEffectPlan{Apply: func(ctx context.Context) (SubnetSideEffectReceipt, error) {
		var err error
		allocation.address, noOp, err = a.backend.ExistingAllocation(ctx, snapshot.Name, podName, nicName)
		if err != nil {
			return nil, err
		}
		if noOp {
			return nil, nil
		}
		allocation.address, err = a.backend.AllocateRandom(ctx, podName, nicName, snapshot.Name, poolName)
		if err != nil {
			return nil, err
		}
		allocation.token = a.next.Add(1)
		return &subnetIPAMReceipt{adapter: a, allocation: allocation}, nil
	}}
	receipt, err := a.writer.executePlan(snapshot, SubnetIPAMAllocateOperation, plan)
	if err != nil {
		return "", "", "", nil, err
	}
	if receipt == nil || noOp {
		return allocation.address.v4, allocation.address.v6, allocation.address.mac, nil, nil
	}
	a.mu.Lock()
	a.owners[key] = allocation
	a.mu.Unlock()
	return allocation.address.v4, allocation.address.v6, allocation.address.mac, receipt, nil
}

type subnetIPAMReceipt struct {
	adapter    *subnetIPAMAdapter
	allocation subnetIPAMAllocation
}

func (r *subnetIPAMReceipt) Compensate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.adapter.mu.Lock()
	current, ok := r.adapter.owners[r.allocation.key]
	if !ok || current != r.allocation {
		r.adapter.mu.Unlock()
		return errors.New("IPAM allocation receipt is stale or owned by another writer")
	}
	r.adapter.mu.Unlock()
	if err := r.adapter.backend.ReleaseIfMatches(ctx, r.allocation); err != nil {
		return err
	}
	r.adapter.mu.Lock()
	defer r.adapter.mu.Unlock()
	if current, ok := r.adapter.owners[r.allocation.key]; !ok || current != r.allocation {
		return errors.New("IPAM allocation owner changed during compensation")
	}
	delete(r.adapter.owners, r.allocation.key)
	return nil
}
