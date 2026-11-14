package controllerruntime

import (
	"context"
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

var ErrSubnetRuntimeReaderMissing = errors.New("subnet runtime reader is not configured")

// SubnetDataPlaneBackend is the explicit seam for the initialized legacy
// Controller. In subnet-writer mode it is the only implementation that may
// touch IPAM and OVN; the legacy Subnet workers are disabled by the mode gate.
type SubnetDataPlaneBackend interface {
	RecoverSubnetRuntime(context.Context, *kubeovnv1.Subnet) error
	ReconcileSubnetRuntime(context.Context, *kubeovnv1.Subnet) error
}

// SubnetControllerWriterAdapter wires the real Controller domain path into
// the runtime reconciler. The facade carries the immutable API snapshot into
// the existing status, finalizer, IPAM, and OVN sequence.
type SubnetControllerWriterAdapter struct {
	Backend SubnetDataPlaneBackend
	Reader  client.Reader
	Gate    SubnetWriterGate
}

func (a *SubnetControllerWriterAdapter) RecoverSubnet(ctx context.Context, snapshot SubnetWriteSnapshot) error {
	if a == nil || a.Backend == nil {
		return errors.New("subnet controller writer backend is not configured")
	}
	return a.Backend.RecoverSubnetRuntime(ctx, subnetFromSnapshot(snapshot))
}

func (a *SubnetControllerWriterAdapter) ReconcileSubnet(ctx context.Context, snapshot SubnetWriteSnapshot, _ SubnetShadowObservation) error {
	if a == nil || a.Backend == nil {
		return errors.New("subnet controller writer backend is not configured")
	}
	if a.Reader == nil {
		return ErrSubnetRuntimeReaderMissing
	}
	if err := a.Gate.WaitForRecovery(ctx); err != nil {
		return err
	}
	live := &kubeovnv1.Subnet{}
	if err := a.Reader.Get(ctx, client.ObjectKey{Name: snapshot.Name}, live); err != nil {
		return err
	}
	operation := SubnetStatusWriteOperation
	if snapshot.Deleting {
		operation = SubnetFinalizerRemoveOperation
	}
	if err := snapshot.Validate(live, operation); err != nil {
		return err
	}
	return a.Backend.ReconcileSubnetRuntime(ctx, subnetFromSnapshot(snapshot))
}

func subnetFromSnapshot(snapshot SubnetWriteSnapshot) *kubeovnv1.Subnet {
	subnet := &kubeovnv1.Subnet{
		Name:            snapshot.Name,
		UID:             snapshot.UID,
		Generation:      snapshot.Generation,
		ResourceVersion: snapshot.ResourceVersion,
		Finalizers:      append([]string(nil), snapshot.Finalizers...),
		Spec:            *snapshot.Spec.DeepCopy(),
		Status:          *snapshot.Status.DeepCopy(),
	}
	if snapshot.Deleting {
		subnet.DeletionTimestamp = new(metav1.Time)
		subnet.DeletionTimestamp.Time = metav1.Now().Time
	}
	return subnet
}

var _ SubnetWriterAdapter = (*SubnetControllerWriterAdapter)(nil)
