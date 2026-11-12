package controllerruntime

import (
	"context"
	"errors"
	"fmt"
	"time"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
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
// a receipt only for a confirmed mutation that it owns. No-op, uncommitted,
// and uncertain outcomes return a nil receipt; uncertain outcomes require
// adapter readback or recovery rather than speculative compensation.
type SubnetSideEffectPlan struct {
	Apply func(context.Context) (SubnetSideEffectReceipt, error)
}

// subnetSideEffectWriter gates OVN and IPAM side effects behind the immutable
// Subnet snapshot and recovery barrier. It does not own or implement either
// backend and remains unwired while the legacy controller is the sole writer.
type subnetSideEffectWriter struct {
	ctx  context.Context
	gate SubnetWriterGate
}

const subnetSideEffectCompensationTimeout = 5 * time.Second

func (w *subnetSideEffectWriter) AllocateOVN(snapshot SubnetWriteSnapshot, current *kubeovnv1.Subnet, action SubnetSideEffectAction) error {
	return w.execute(snapshot, current, SubnetOVNAllocateOperation, action)
}

func (w *subnetSideEffectWriter) CleanupOVN(snapshot SubnetWriteSnapshot, current *kubeovnv1.Subnet, action SubnetSideEffectAction) error {
	return w.execute(snapshot, current, SubnetOVNCleanupOperation, action)
}

func (w *subnetSideEffectWriter) AllocateIPAM(snapshot SubnetWriteSnapshot, current *kubeovnv1.Subnet, action SubnetSideEffectAction) error {
	return w.execute(snapshot, current, SubnetIPAMAllocateOperation, action)
}

func (w *subnetSideEffectWriter) ReleaseIPAM(snapshot SubnetWriteSnapshot, current *kubeovnv1.Subnet, action SubnetSideEffectAction) error {
	return w.execute(snapshot, current, SubnetIPAMReleaseOperation, action)
}

func (w *subnetSideEffectWriter) execute(snapshot SubnetWriteSnapshot, current *kubeovnv1.Subnet, operation SubnetWriteOperation, action SubnetSideEffectAction) error {
	if action == nil {
		return errors.New("subnet side effect action is not configured")
	}
	return w.Execute(snapshot, current, operation, SubnetSideEffectPlan{
		Apply: func(ctx context.Context) (SubnetSideEffectReceipt, error) {
			return nil, action(ctx)
		},
	})
}

// Execute runs one gated side-effect plan. Only a failed apply with a receipt
// and a live writer context can attempt conditional compensation. Cancellation
// never authorizes rollback; the recovered owner must resolve pending work.
// There are no internal retries or deduplication across Execute calls.
func (w *subnetSideEffectWriter) Execute(snapshot SubnetWriteSnapshot, current *kubeovnv1.Subnet, operation SubnetWriteOperation, plan SubnetSideEffectPlan) error {
	switch operation {
	case SubnetOVNAllocateOperation, SubnetOVNCleanupOperation, SubnetIPAMAllocateOperation, SubnetIPAMReleaseOperation:
	default:
		return fmt.Errorf("unsupported Subnet side effect operation %q", operation)
	}
	if plan.Apply == nil {
		return errors.New("subnet side effect action is not configured")
	}
	if err := w.gate.Validate(w.ctx, snapshot, current, operation); err != nil {
		return err
	}
	if err := w.ctx.Err(); err != nil {
		return err
	}
	receipt, applyErr := plan.Apply(w.ctx)
	if err := w.ctx.Err(); err != nil {
		return errors.Join(applyErr, err)
	}
	if applyErr != nil {
		return w.compensate(receipt, applyErr)
	}
	return nil
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
