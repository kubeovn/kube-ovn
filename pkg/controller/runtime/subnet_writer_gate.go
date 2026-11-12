package controllerruntime

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

// SubnetWriteOperation identifies the side effect a future Subnet writer is
// about to perform. Allocation and cleanup have separate gates because a
// terminating Subnet must release existing state without allocating new data.
type SubnetWriteOperation string

const (
	SubnetStatusWriteOperation     SubnetWriteOperation = "status"
	SubnetFinalizerAddOperation    SubnetWriteOperation = "finalizer-add"
	SubnetFinalizerRemoveOperation SubnetWriteOperation = "finalizer-remove"
	SubnetOVNAllocateOperation     SubnetWriteOperation = "ovn-allocate"
	SubnetOVNCleanupOperation      SubnetWriteOperation = "ovn-cleanup"
	SubnetIPAMAllocateOperation    SubnetWriteOperation = "ipam-allocate"
	SubnetIPAMReleaseOperation     SubnetWriteOperation = "ipam-release"
)

// SubnetWriteSnapshot is the immutable API state a writer must carry across
// status, finalizer, OVN, and IPAM work. It prevents a delayed operation from
// applying to a changed or same-name replacement Subnet.
type SubnetWriteSnapshot struct {
	Name              string
	UID               types.UID
	Generation        int64
	ResourceVersion   string
	Spec              kubeovnv1.SubnetSpec
	Deleting          bool
	AllocationAllowed bool
}

// NewSubnetWriteSnapshot captures the API identity and spec together with the
// read-only allocation gate. The snapshot is independent of the API object.
func NewSubnetWriteSnapshot(subnet *kubeovnv1.Subnet, allocationAllowed bool) (SubnetWriteSnapshot, error) {
	if subnet == nil {
		return SubnetWriteSnapshot{}, errors.New("subnet write snapshot requires a Subnet")
	}
	return SubnetWriteSnapshot{
		Name:              subnet.Name,
		UID:               subnet.UID,
		Generation:        subnet.Generation,
		ResourceVersion:   subnet.ResourceVersion,
		Spec:              *subnet.Spec.DeepCopy(),
		Deleting:          !subnet.DeletionTimestamp.IsZero(),
		AllocationAllowed: allocationAllowed,
	}, nil
}

// Validate checks the live object immediately before a side effect. A
// conflict forces reconciliation to restart from a fresh snapshot.
func (s SubnetWriteSnapshot) Validate(current *kubeovnv1.Subnet, operation SubnetWriteOperation) error {
	if current == nil {
		return subnetWriteConflict(s.Name, "live Subnet is missing")
	}
	if current.Name != s.Name || current.UID != s.UID || current.Generation != s.Generation ||
		current.ResourceVersion != s.ResourceVersion || !equality.Semantic.DeepEqual(current.Spec, s.Spec) ||
		(!current.DeletionTimestamp.IsZero()) != s.Deleting {
		return subnetWriteConflict(s.Name, "Subnet changed since the reconciliation snapshot")
	}

	switch operation {
	case SubnetStatusWriteOperation, SubnetFinalizerAddOperation:
		if s.Deleting {
			return fmt.Errorf("subnet %s is terminating; %s is not allowed", s.Name, operation)
		}
	case SubnetFinalizerRemoveOperation:
		if !s.Deleting {
			return fmt.Errorf("subnet %s is not terminating; finalizer removal is not allowed", s.Name)
		}
	case SubnetOVNAllocateOperation, SubnetIPAMAllocateOperation:
		if s.Deleting || !s.AllocationAllowed {
			return fmt.Errorf("subnet %s is not allocation-ready for %s", s.Name, operation)
		}
	case SubnetOVNCleanupOperation, SubnetIPAMReleaseOperation:
		if !s.Deleting {
			return fmt.Errorf("subnet %s is not terminating; cleanup is not allowed", s.Name)
		}
	default:
		return fmt.Errorf("unknown Subnet write operation %q", operation)
	}
	return nil
}

func subnetWriteConflict(name, reason string) error {
	return apierrors.NewConflict(
		schema.GroupResource{Group: kubeovnv1.SchemeGroupVersion.Group, Resource: "subnets"},
		name,
		errors.New(reason),
	)
}

// SubnetWriterGate blocks every writer operation until durable recovery has
// completed. A missing recovery callback fails closed so a restart cannot
// allocate or mutate OVN state from an uninitialized process.
type SubnetWriterGate struct {
	Recovery func(context.Context) error
}

func (g SubnetWriterGate) WaitForRecovery(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if g.Recovery == nil {
		return errors.New("subnet writer recovery gate is not configured")
	}
	return g.Recovery(ctx)
}

func (g SubnetWriterGate) Validate(ctx context.Context, snapshot SubnetWriteSnapshot, current *kubeovnv1.Subnet, operation SubnetWriteOperation) error {
	if err := g.WaitForRecovery(ctx); err != nil {
		return err
	}
	return snapshot.Validate(current, operation)
}
