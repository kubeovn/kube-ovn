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
	ipam    *ipam.IPAM
	allocMu sync.Mutex
	mu      sync.Mutex
	next    atomic.Uint64
	owners  map[subnetIPAMKey]subnetIPAMAllocation
}

type subnetIPAMKey struct {
	subnet string
	pod    string
	nic    string
}

type subnetIPAMAllocation struct {
	key   subnetIPAMKey
	token uint64
	uid   string
	v4    string
	v6    string
	mac   string
}

func newSubnetIPAMAdapter(writer *subnetSideEffectWriter, allocation *ipam.IPAM) (*subnetIPAMAdapter, error) {
	if writer == nil || allocation == nil {
		return nil, errors.New("subnet IPAM adapter requires a writer and IPAM")
	}
	return &subnetIPAMAdapter{writer: writer, ipam: allocation, owners: make(map[subnetIPAMKey]subnetIPAMAllocation)}, nil
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
	plan := SubnetSideEffectPlan{Apply: func(context.Context) (SubnetSideEffectReceipt, error) {
		if allocation.v4, allocation.v6, allocation.mac, noOp = existingIPAMAllocation(a.ipam, snapshot.Name, podName, nicName); noOp {
			return nil, nil
		}
		if err := a.ipam.WithTransaction(func(tx *ipam.Transaction) error {
			var err error
			allocation.v4, allocation.v6, allocation.mac, err = tx.GetRandomAddress(podName, nicName, nil, snapshot.Name, poolName, nil, true)
			return err
		}); err != nil {
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
		return allocation.v4, allocation.v6, allocation.mac, nil, nil
	}
	a.mu.Lock()
	a.owners[key] = allocation
	a.mu.Unlock()
	return allocation.v4, allocation.v6, allocation.mac, receipt, nil
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
	v4, v6, mac, ok := existingIPAMAllocation(r.adapter.ipam, r.allocation.key.subnet, r.allocation.key.pod, r.allocation.key.nic)
	if !ok || v4 != r.allocation.v4 || v6 != r.allocation.v6 || mac != r.allocation.mac {
		return errors.New("IPAM allocation receipt no longer matches the owned address")
	}
	if err := r.adapter.ipam.WithTransaction(func(tx *ipam.Transaction) error {
		return tx.ReleaseAddressByNic(r.allocation.key.pod, r.allocation.key.nic, r.allocation.key.subnet)
	}); err != nil {
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

func existingIPAMAllocation(allocation *ipam.IPAM, subnetName, podName, nicName string) (string, string, string, bool) {
	return allocation.GetPodAddressByNic(podName, nicName, subnetName)
}
