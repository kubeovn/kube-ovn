package controllerruntime

import (
	"context"
	"errors"
	"fmt"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
)

var (
	ErrSubnetOVNResolverMissing = errors.New("subnet OVN mutation resolver is not configured")
	ErrSubnetOVNResolverUnknown = errors.New("subnet OVN mutation state is unknown")
	ErrSubnetOVNResolverPending = errors.New("subnet OVN mutation has not converged")
)

// SubnetOVNMutationState is the result of a backend readback after a process
// restart. Unknown is explicit: callers must retain the receipt and marker.
type SubnetOVNMutationState string

const (
	SubnetOVNMutationStateCommitted SubnetOVNMutationState = "committed"
	SubnetOVNMutationStateAbsent    SubnetOVNMutationState = "absent"
	SubnetOVNMutationStateUnknown   SubnetOVNMutationState = "unknown"
)

// SubnetOVNMutationResolver reads the concrete OVN state for one durable
// receipt. Implementations must not infer commitment from marker presence.
type SubnetOVNMutationResolver interface {
	ResolveSubnetOVNMutation(context.Context, SubnetWriteSnapshot, SubnetOVNMutationKey, SubnetOVNMutationReceipt) (SubnetOVNMutationState, error)
}

// OVSDBSubnetOVNMutationResolver reads the logical switch that represents a
// Subnet's OVN data-plane mutation. It is constructed from the initialized NB
// table provider, so the legacy controller does not need to import runtime
// interfaces or implement a runtime-only provider method.
type OVSDBSubnetOVNMutationResolver struct {
	provider table.Provider
}

func NewOVSDBSubnetOVNMutationResolver(provider table.Provider) (*OVSDBSubnetOVNMutationResolver, error) {
	if provider == nil {
		return nil, errors.New("subnet OVN mutation resolver requires an OVSDB provider")
	}
	return &OVSDBSubnetOVNMutationResolver{provider: provider}, nil
}

func (r *OVSDBSubnetOVNMutationResolver) ResolveSubnetOVNMutation(ctx context.Context, snapshot SubnetWriteSnapshot, key SubnetOVNMutationKey, receipt SubnetOVNMutationReceipt) (SubnetOVNMutationState, error) {
	if r == nil || r.provider == nil {
		return SubnetOVNMutationStateUnknown, ErrSubnetOVNResolverMissing
	}
	if err := contextError(ctx); err != nil {
		return SubnetOVNMutationStateUnknown, err
	}
	if key != receipt.Key || key.Name != snapshot.Name || key.UID != snapshot.UID {
		return SubnetOVNMutationStateUnknown, fmt.Errorf("%w: mutation key does not match Subnet snapshot", ErrSubnetOVNResolverUnknown)
	}
	if err := receipt.valid(); err != nil {
		return SubnetOVNMutationStateUnknown, fmt.Errorf("%w: invalid mutation intent: %w", ErrSubnetOVNResolverUnknown, err)
	}
	if receipt.Intent.LogicalSwitchName != snapshot.Name {
		return SubnetOVNMutationStateUnknown, fmt.Errorf("%w: mutation intent targets logical switch %q", ErrSubnetOVNResolverUnknown, receipt.Intent.LogicalSwitchName)
	}
	if key.Operation != SubnetOVNAllocateOperation && key.Operation != SubnetOVNCleanupOperation {
		return SubnetOVNMutationStateUnknown, fmt.Errorf("%w: unsupported mutation operation %q", ErrSubnetOVNResolverUnknown, key.Operation)
	}
	row := &ovnnb.LogicalSwitch{Name: receipt.Intent.LogicalSwitchName}
	handle := r.provider.Table(&ovnnb.LogicalSwitch{})
	if handle == nil {
		return SubnetOVNMutationStateUnknown, errors.New("subnet OVN mutation resolver table is not configured")
	}
	if err := handle.Get(ctx, row); err != nil {
		if errors.Is(err, table.ErrNotFound) {
			return SubnetOVNMutationStateAbsent, nil
		}
		return SubnetOVNMutationStateUnknown, err
	}
	return SubnetOVNMutationStateCommitted, nil
}

func validateSubnetOVNMutationState(state SubnetOVNMutationState) error {
	switch state {
	case SubnetOVNMutationStateCommitted, SubnetOVNMutationStateAbsent:
		return nil
	case SubnetOVNMutationStateUnknown:
		return ErrSubnetOVNResolverUnknown
	default:
		return ErrSubnetOVNResolverUnknown
	}
}

func subnetOVNResolverAllowsRelease(operation SubnetWriteOperation, state SubnetOVNMutationState) bool {
	switch operation {
	case SubnetOVNAllocateOperation:
		return state == SubnetOVNMutationStateCommitted
	case SubnetOVNCleanupOperation:
		return state == SubnetOVNMutationStateAbsent
	default:
		return false
	}
}

// subnetOVNResolverAllowsTombstoneRelease permits a delete tombstone to
// discard an allocation receipt after the backend confirms that allocation is
// absent. Active allocation recovery for a live Subnet deliberately keeps the
// receipt and reacquires its marker so the allocation can be retried.
func subnetOVNResolverAllowsTombstoneRelease(snapshot SubnetWriteSnapshot, operation SubnetWriteOperation, state SubnetOVNMutationState) bool {
	return snapshot.Deleting && operation == SubnetOVNAllocateOperation && state == SubnetOVNMutationStateAbsent
}
