package controllerruntime

import (
	"context"
	"errors"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

// SubnetSideEffectAction is the operation-specific OVN or IPAM mutation that
// a future Subnet writer wants to perform. The action is deliberately injected
// so this contract can be tested without wiring a second production writer.
type SubnetSideEffectAction func(context.Context) error

// subnetSideEffectWriter gates OVN and IPAM side effects behind the immutable
// Subnet snapshot and recovery barrier. It does not own or implement either
// backend and remains unwired while the legacy controller is the sole writer.
type subnetSideEffectWriter struct {
	ctx  context.Context
	gate SubnetWriterGate
}

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
	if err := w.gate.Validate(w.ctx, snapshot, current, operation); err != nil {
		return err
	}
	if err := w.ctx.Err(); err != nil {
		return err
	}
	if err := action(w.ctx); err != nil {
		return err
	}
	return w.ctx.Err()
}
