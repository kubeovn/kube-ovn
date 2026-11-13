package controllerruntime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SubnetSideEffectAction is the operation-specific OVN or IPAM mutation that
// a future Subnet writer wants to perform. The action is deliberately injected
// so this contract can be tested without wiring a second production writer.
type SubnetSideEffectAction func(context.Context) error

// SubnetSideEffectReceipt identifies a confirmed mutation made by one apply.
// The adapter must bind it to the Subnet UID, mutation revision, and writer
// ownership epoch. Compensate must atomically check those backend identities
// before reverting only that mutation. Replays must preserve existing state,
// replacement objects, and changes made by a subsequent writer.
type SubnetSideEffectReceipt interface {
	Compensate(context.Context) error
}

// SubnetSideEffectPlan describes one gated backend mutation. Apply may return
// a receipt only for a confirmed mutation that it owns. No-op and uncommitted
// outcomes return a nil receipt. If cancellation races with a committed
// mutation, an adapter may perform bounded readback and return a receipt with
// the cancellation error so the caller preserves the owner for recovery. When
// readback cannot confirm the mutation, recovery must resolve it; cancellation
// never authorizes speculative compensation.
type SubnetSideEffectPlan struct {
	Apply func(context.Context) (SubnetSideEffectReceipt, error)
}

// subnetSideEffectWriter gates OVN and IPAM side effects behind the immutable
// Subnet snapshot and recovery barrier. It does not own or implement either
// backend and remains unwired while the legacy controller is the sole writer.
type subnetSideEffectWriter struct {
	ctx context.Context
	// reader must be an uncached APIReader, not a manager cache.
	reader client.Reader
	gate   SubnetWriterGate
}

const (
	subnetSideEffectReadTimeout         = 5 * time.Second
	subnetSideEffectCompensationTimeout = 5 * time.Second
)

func (w *subnetSideEffectWriter) AllocateOVN(snapshot SubnetWriteSnapshot, action SubnetSideEffectAction) error {
	return w.execute(snapshot, SubnetOVNAllocateOperation, action)
}

func (w *subnetSideEffectWriter) CleanupOVN(snapshot SubnetWriteSnapshot, action SubnetSideEffectAction) error {
	return w.execute(snapshot, SubnetOVNCleanupOperation, action)
}

func (w *subnetSideEffectWriter) AllocateIPAM(snapshot SubnetWriteSnapshot, action SubnetSideEffectAction) error {
	return w.execute(snapshot, SubnetIPAMAllocateOperation, action)
}

func (w *subnetSideEffectWriter) ReleaseIPAM(snapshot SubnetWriteSnapshot, action SubnetSideEffectAction) error {
	return w.execute(snapshot, SubnetIPAMReleaseOperation, action)
}

func (w *subnetSideEffectWriter) execute(snapshot SubnetWriteSnapshot, operation SubnetWriteOperation, action SubnetSideEffectAction) error {
	if action == nil {
		return errors.New("subnet side effect action is not configured")
	}
	return w.Execute(snapshot, operation, SubnetSideEffectPlan{
		Apply: func(ctx context.Context) (SubnetSideEffectReceipt, error) {
			return nil, action(ctx)
		},
	})
}

// Execute reads live Subnet state after recovery for each side-effect plan.
// Only a failed apply with a receipt and a live writer context can attempt
// conditional compensation. Cancellation never authorizes rollback; the
// recovered owner must resolve pending work.
// There are no internal retries or deduplication across Execute calls.
func (w *subnetSideEffectWriter) Execute(snapshot SubnetWriteSnapshot, operation SubnetWriteOperation, plan SubnetSideEffectPlan) error {
	_, err := w.executePlan(snapshot, operation, plan)
	return err
}

func (w *subnetSideEffectWriter) executePlan(snapshot SubnetWriteSnapshot, operation SubnetWriteOperation, plan SubnetSideEffectPlan) (SubnetSideEffectReceipt, error) {
	switch operation {
	case SubnetOVNAllocateOperation, SubnetOVNCleanupOperation, SubnetIPAMAllocateOperation, SubnetIPAMReleaseOperation:
	default:
		return nil, fmt.Errorf("unsupported Subnet side effect operation %q", operation)
	}
	if plan.Apply == nil {
		return nil, errors.New("subnet side effect action is not configured")
	}
	if err := w.gate.WaitForRecovery(w.ctx); err != nil {
		return nil, err
	}
	if w.reader == nil {
		return nil, errors.New("subnet side effect API reader is not configured")
	}
	current, err := getSubnet(w.ctx, w.reader, snapshot.Name, subnetSideEffectReadTimeout)
	if err != nil {
		return nil, err
	}
	if err := snapshot.Validate(current, operation); err != nil {
		return nil, err
	}
	if err := w.ctx.Err(); err != nil {
		return nil, err
	}
	receipt, applyErr := plan.Apply(w.ctx)
	if err := w.ctx.Err(); err != nil {
		// Preserve a confirmed receipt when cancellation races with a
		// committed backend operation. The caller must retain the owner for
		// recovery; cancellation never authorizes speculative compensation.
		return receipt, errors.Join(applyErr, err)
	}
	if applyErr != nil {
		return nil, w.compensate(receipt, applyErr)
	}
	return receipt, nil
}

func (w *subnetSideEffectWriter) compensate(receipt SubnetSideEffectReceipt, cause error) error {
	if receipt == nil {
		return cause
	}
	ctx, cancel := context.WithTimeout(w.ctx, subnetSideEffectCompensationTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return errors.Join(cause, err)
	}
	err := receipt.Compensate(ctx)
	return errors.Join(cause, err, ctx.Err())
}
