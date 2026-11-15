package controllerruntime

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SubnetSideEffectAction is the operation-specific OVN or IPAM mutation passed
// through the runtime writer's phase gate. The action is injected so the gate
// can be tested without duplicating the legacy domain implementation.
type SubnetSideEffectAction func(context.Context) error

// SubnetSideEffectReceipt identifies a confirmed mutation made by one apply.
// The adapter must bind it to the Subnet UID, mutation revision, and writer
// ownership epoch. Compensate must atomically check those backend identities
// before reverting only that mutation. Replays must preserve existing state,
// replacement objects, and changes made by a subsequent writer.
type SubnetSideEffectReceipt interface {
	Compensate(context.Context) error
}

// SubnetSideEffectReceiptIdentity is implemented by receipts that can prove
// which immutable mutation identity they own. Concrete adapters must expose it
// when a plan requires receipt identity validation.
type SubnetSideEffectReceiptIdentity interface {
	SubnetMutationIdentity() SubnetMutationIdentity
}

// SubnetSideEffectPlan describes one gated backend mutation. Apply may return
// a receipt only for a confirmed mutation that it owns. No-op and uncommitted
// outcomes return a nil receipt. If cancellation races with a committed
// mutation, an adapter may perform bounded readback and return a receipt with
// the cancellation error so the caller preserves the owner for recovery. When
// readback cannot confirm the mutation, recovery must resolve it; cancellation
// never authorizes speculative compensation.
type SubnetSideEffectPlan struct {
	Apply                  func(context.Context) (SubnetSideEffectReceipt, error)
	RequireReceiptIdentity bool
	// RetainReceiptOnError keeps a receipt-owned fence when Apply returns an
	// error. A backend may have committed before reporting the error, so
	// speculative compensation could release ownership before recovery has
	// read back the actual data-plane state.
	RetainReceiptOnError bool
}

// subnetSideEffectWriter gates an OVN or IPAM domain phase behind the immutable
// Subnet snapshot and recovery barrier. Runtime mode installs it around the
// existing domain callback; concrete per-mutation receipts remain a follow-up
// seam for the data-plane adapters.
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
	case SubnetOVNAllocateOperation, SubnetOVNCleanupOperation, SubnetIPAMAllocateOperation, SubnetIPAMReleaseOperation, SubnetIPAMRegisterOperation, SubnetIPAMDeleteOperation, SubnetIPAMAddressAllocateOperation, SubnetIPAMAddressReleaseOperation:
	default:
		return nil, fmt.Errorf("unsupported Subnet side effect operation %q", operation)
	}
	if plan.Apply == nil {
		return nil, errors.New("subnet side effect action is not configured")
	}
	if err := w.gate.WaitForRecovery(w.ctx); err != nil {
		return nil, err
	}
	identity, err := newSubnetMutationIdentity(w.ctx, snapshot, operation, w.gate.Fence)
	if err != nil {
		return nil, err
	}
	if plan.RequireReceiptIdentity && identity.WriterEpoch == "" {
		return nil, ErrSubnetWriterEpochUnavailable
	}
	if w.reader == nil {
		return nil, errors.New("subnet side effect API reader is not configured")
	}
	current, err := getSubnet(w.ctx, w.reader, snapshot.Name, subnetSideEffectReadTimeout)
	if err != nil {
		// A delete tombstone is an authoritative snapshot even after the API
		// object has disappeared. The reconciler has already checked that no
		// same-name replacement exists, so cleanup must still reach the backend.
		if snapshot.Deleting && apierrors.IsNotFound(err) {
			current = subnetFromSnapshot(snapshot)
		} else {
			return nil, err
		}
	}
	if err := snapshot.Validate(current, operation); err != nil {
		return nil, err
	}
	if err := w.ctx.Err(); err != nil {
		return nil, err
	}
	receipt, applyErr := plan.Apply(withSubnetMutationIdentity(w.ctx, identity))
	if receipt != nil && plan.RequireReceiptIdentity {
		identityReceipt, ok := receipt.(SubnetSideEffectReceiptIdentity)
		if !ok || !identity.Equal(identityReceipt.SubnetMutationIdentity()) {
			return receipt, errors.Join(applyErr, ErrSubnetMutationIdentityChanged)
		}
	}
	if err := w.ctx.Err(); err != nil {
		// Preserve a confirmed receipt when cancellation races with a
		// committed backend operation. The caller must retain the owner for
		// recovery; cancellation never authorizes speculative compensation.
		return receipt, errors.Join(applyErr, err)
	}
	if applyErr != nil {
		if plan.RetainReceiptOnError {
			return receipt, applyErr
		}
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
