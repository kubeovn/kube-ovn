package controllerruntime

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	legacycontroller "github.com/kubeovn/kube-ovn/pkg/controller"
)

var (
	ErrSubnetRuntimeReaderMissing        = errors.New("subnet runtime reader is not configured")
	ErrSubnetRuntimeAllocationNotAllowed = legacycontroller.ErrSubnetRuntimeAllocationNotAllowed
)

// SubnetDataPlaneBackend is the explicit seam for the initialized legacy
// Controller. In subnet-writer mode it is the only implementation that may
// touch IPAM and OVN; the legacy Subnet workers are disabled by the mode gate.
type SubnetDataPlaneBackend interface {
	RecoverSubnetRuntime(context.Context, *kubeovnv1.Subnet) error
	ReconcileSubnetRuntime(context.Context, *kubeovnv1.Subnet) error
}

// SubnetGatedDataPlaneBackend is implemented by the real runtime backend. The
// optional seam keeps the pre-existing backend interface source-compatible
// while allowing the runtime writer to pass its dependency readiness grant.
type SubnetGatedDataPlaneBackend interface {
	ReconcileSubnetRuntimeWithGate(context.Context, *kubeovnv1.Subnet, bool) error
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
	allocationAllowed := snapshot.AllocationAllowed
	if !snapshot.Deleting {
		// ProviderNetwork readiness can change between the shadow observation
		// and this writer admission, so re-read dependencies after checking
		// the Subnet identity.
		freshObservation, err := observeSubnet(ctx, a.Reader, live)
		if err != nil {
			return err
		}
		allocationAllowed = allocationAllowed && freshObservation.AllocationAllowed
		// The legacy runtime backend performs status, finalizer, IPAM, and OVN
		// work as one operation. Until those phases have separate writer seams,
		// fail closed before entering it when dependency readiness denies new
		// allocation.
		if !allocationAllowed {
			return fmt.Errorf("%w: subnet %s", ErrSubnetRuntimeAllocationNotAllowed, snapshot.Name)
		}
	}
	return a.reconcileRuntime(ctx, snapshot, allocationAllowed)
}

func (a *SubnetControllerWriterAdapter) ReconcileDeletedSubnet(ctx context.Context, snapshot SubnetWriteSnapshot) error {
	if a == nil || a.Backend == nil {
		return errors.New("subnet controller writer backend is not configured")
	}
	if !snapshot.Deleting {
		return errors.New("subnet delete snapshot is not terminating")
	}
	if a.Reader != nil {
		live := &kubeovnv1.Subnet{}
		if err := a.Reader.Get(ctx, client.ObjectKey{Name: snapshot.Name}, live); err == nil {
			if live.UID != snapshot.UID {
				return apierrors.NewConflict(schema.GroupResource{Group: kubeovnv1.SchemeGroupVersion.Group, Resource: "subnets"}, snapshot.Name, errors.New("replacement subnet has a different UID"))
			}
			return errors.New("deleting subnet snapshot still has a live API object")
		} else if !apierrors.IsNotFound(err) {
			return err
		}
	}
	if err := a.Gate.WaitForRecovery(ctx); err != nil {
		return err
	}
	return a.reconcileRuntime(ctx, snapshot, snapshot.AllocationAllowed)
}

func (a *SubnetControllerWriterAdapter) reconcileRuntime(ctx context.Context, snapshot SubnetWriteSnapshot, allocationAllowed bool) error {
	subnet := subnetFromSnapshot(snapshot)
	if gated, ok := a.Backend.(SubnetGatedDataPlaneBackend); ok {
		return gated.ReconcileSubnetRuntimeWithGate(ctx, subnet, allocationAllowed)
	}
	// The adapter has already performed active dependency admission. Keep the
	// old backend interface as a compatibility fallback for external
	// implementations that predate the gated seam.
	return a.Backend.ReconcileSubnetRuntime(ctx, subnet)
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
