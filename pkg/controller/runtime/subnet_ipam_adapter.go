package controllerruntime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/kubeovn/kube-ovn/pkg/ipam"
)

// subnetIPAMAdapter is an unwired adapter for one IPAM allocation operation.
// Receipt ownership is delegated to a store so fencing survives adapter
// recreation when a durable store is wired in. The default store is
// process-local and is not durable.
type subnetIPAMAdapter struct {
	writer  *subnetSideEffectWriter
	backend subnetIPAMBackend
	store   subnetIPAMReceiptStore
	allocMu sync.Mutex
	next    atomic.Uint64
}

var (
	ErrSubnetIPAMRecoveryUnavailable = errors.New("subnet IPAM durable recovery is not configured")
	ErrSubnetIPAMRecoveryUnsafe      = errors.New("subnet IPAM durable recovery identity is unsafe")
	ErrSubnetIPAMReceiptStale        = errors.New("IPAM allocation receipt is stale or owned by another writer")
	ErrSubnetIPAMReceiptChanged      = errors.New("IPAM allocation owner changed during compensation")
)

// subnetIPAMReceiptStore persists allocation identity for receipt fencing.
// Production wiring must provide a durable implementation; the in-memory
// implementation below only preserves the existing process-local behavior.
type subnetIPAMReceiptStore interface {
	Remember(ctx context.Context, allocation subnetIPAMAllocation) error
	Lookup(ctx context.Context, key subnetIPAMKey) (subnetIPAMAllocation, bool, error)
	Delete(ctx context.Context, allocation subnetIPAMAllocation) error
}

type memorySubnetIPAMReceiptStore struct {
	mu          sync.Mutex
	allocations map[subnetIPAMKey]subnetIPAMAllocation
}

func newMemorySubnetIPAMReceiptStore() *memorySubnetIPAMReceiptStore {
	return &memorySubnetIPAMReceiptStore{allocations: make(map[subnetIPAMKey]subnetIPAMAllocation)}
}

func (s *memorySubnetIPAMReceiptStore) Remember(ctx context.Context, allocation subnetIPAMAllocation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.allocations[allocation.key]; ok && current != allocation {
		return ErrSubnetIPAMReceiptStale
	}
	s.allocations[allocation.key] = allocation
	return nil
}

func (s *memorySubnetIPAMReceiptStore) Lookup(ctx context.Context, key subnetIPAMKey) (subnetIPAMAllocation, bool, error) {
	if err := ctx.Err(); err != nil {
		return subnetIPAMAllocation{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	allocation, ok := s.allocations[key]
	return allocation, ok, nil
}

func (s *memorySubnetIPAMReceiptStore) Delete(ctx context.Context, allocation subnetIPAMAllocation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.allocations[allocation.key]
	if !ok || current != allocation {
		return ErrSubnetIPAMReceiptChanged
	}
	delete(s.allocations, allocation.key)
	return nil
}

// subnetIPAMBackend is the narrow IPAM seam consumed by the future Subnet
// writer. A production implementation must preserve the same no-op,
// transaction and uncertain-commit semantics before it can be wired in.
type subnetIPAMBackend interface {
	ExistingAllocation(ctx context.Context, subnetName, podName, nicName string) (subnetIPAMAddress, bool, error)
	AllocateRandom(ctx context.Context, podName, nicName, subnetName, poolName string) (subnetIPAMAddress, error)
	// Recover returns durable allocations for the Subnet snapshot. A backend
	// that cannot provide durable recovery must fail closed rather than allow a
	// new adapter instance to mutate IPAM without fencing old receipts.
	Recover(ctx context.Context, snapshot SubnetWriteSnapshot) ([]subnetIPAMAllocation, error)
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

func (b *transactionalSubnetIPAMBackend) Recover(context.Context, SubnetWriteSnapshot) ([]subnetIPAMAllocation, error) {
	return nil, ErrSubnetIPAMRecoveryUnavailable
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
	return newSubnetIPAMAdapterWithBackendAndStore(writer, backend, newMemorySubnetIPAMReceiptStore())
}

func newSubnetIPAMAdapterWithBackendAndStore(writer *subnetSideEffectWriter, backend subnetIPAMBackend, store subnetIPAMReceiptStore) (*subnetIPAMAdapter, error) {
	if writer == nil || backend == nil || store == nil {
		return nil, errors.New("subnet IPAM adapter requires a writer and backend")
	}
	return &subnetIPAMAdapter{writer: writer, backend: backend, store: store}, nil
}

// Recover restores receipt ownership after a process restart. The backend is
// responsible for reading a durable record; this adapter validates every
// identity before publishing any owner so stale or replacement Subnets fail
// closed without partially restoring receipts.
func (a *subnetIPAMAdapter) Recover(ctx context.Context, snapshot SubnetWriteSnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.allocMu.Lock()
	defer a.allocMu.Unlock()
	allocations, err := a.backend.Recover(ctx, snapshot)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, allocation := range allocations {
		if allocation.key.subnet != snapshot.Name || allocation.key.pod == "" || allocation.key.nic == "" ||
			allocation.uid != string(snapshot.UID) || allocation.token == 0 ||
			(allocation.address.v4 == "" && allocation.address.v6 == "" && allocation.address.mac == "") {
			return ErrSubnetIPAMRecoveryUnsafe
		}
	}
	existing := make(map[subnetIPAMKey]bool, len(allocations))
	seen := make(map[subnetIPAMKey]subnetIPAMAllocation, len(allocations))
	for _, allocation := range allocations {
		if previous, ok := seen[allocation.key]; ok && previous != allocation {
			return ErrSubnetIPAMRecoveryUnsafe
		}
		seen[allocation.key] = allocation
		current, ok, err := a.store.Lookup(ctx, allocation.key)
		if err != nil {
			return err
		}
		if ok && current != allocation {
			return ErrSubnetIPAMRecoveryUnsafe
		}
		existing[allocation.key] = ok
	}
	remembered := make([]subnetIPAMAllocation, 0, len(allocations))
	for _, allocation := range allocations {
		if err := a.store.Remember(ctx, allocation); err != nil {
			for _, candidate := range append(remembered, allocation) {
				if !existing[candidate.key] {
					_ = a.store.Delete(context.WithoutCancel(ctx), candidate)
				}
			}
			return err
		}
		if !existing[allocation.key] {
			remembered = append(remembered, allocation)
		}
	}
	return nil
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
		receipt := &subnetIPAMReceipt{adapter: a, allocation: allocation}
		if err := a.store.Remember(ctx, allocation); err != nil {
			releaseErr := a.backend.ReleaseIfMatches(ctx, allocation)
			if releaseErr == nil {
				releaseErr = a.store.Delete(context.WithoutCancel(ctx), allocation)
			}
			return nil, errors.Join(err, releaseErr)
		}
		return receipt, nil
	}}
	receipt, err := a.writer.executePlan(snapshot, SubnetIPAMAllocateOperation, plan)
	if err != nil {
		return "", "", "", nil, err
	}
	if receipt == nil || noOp {
		return allocation.address.v4, allocation.address.v6, allocation.address.mac, nil, nil
	}
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
	r.adapter.allocMu.Lock()
	defer r.adapter.allocMu.Unlock()
	current, ok, err := r.adapter.store.Lookup(ctx, r.allocation.key)
	if err != nil {
		return err
	}
	if !ok || current != r.allocation {
		return ErrSubnetIPAMReceiptStale
	}
	if err := r.adapter.backend.ReleaseIfMatches(ctx, r.allocation); err != nil {
		if !errors.Is(err, ipam.ErrAddressOwnershipChanged) {
			return err
		}
		_, found, lookupErr := r.adapter.backend.ExistingAllocation(ctx, r.allocation.key.subnet, r.allocation.key.pod, r.allocation.key.nic)
		if lookupErr != nil {
			return errors.Join(err, lookupErr)
		}
		if found {
			return err
		}
		return r.adapter.store.Delete(ctx, r.allocation)
	}
	if err := r.adapter.store.Delete(ctx, r.allocation); err != nil {
		return err
	}
	return nil
}
