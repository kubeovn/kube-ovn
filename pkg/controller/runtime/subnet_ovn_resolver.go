package controllerruntime

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/ovn-kubernetes/libovsdb/model"

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
	SubnetOVNMutationStatePending   SubnetOVNMutationState = "pending"
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
	intent := receipt.Intent
	lsHandle := r.provider.Table(&ovnnb.LogicalSwitch{})
	if lsHandle == nil {
		return SubnetOVNMutationStateUnknown, errors.New("subnet OVN mutation resolver table is not configured")
	}
	ls := &ovnnb.LogicalSwitch{Name: intent.LogicalSwitchName}
	lsExists, err := readSubnetOVNRow(ctx, lsHandle, ls)
	if err != nil {
		return SubnetOVNMutationStateUnknown, err
	}
	if !intent.RequirePatchPorts {
		if key.Operation == SubnetOVNAllocateOperation {
			if lsExists {
				return SubnetOVNMutationStateCommitted, nil
			}
			return SubnetOVNMutationStateAbsent, nil
		}
		if lsExists {
			return SubnetOVNMutationStatePending, nil
		}
		return SubnetOVNMutationStateAbsent, nil
	}

	lrpHandle := r.provider.Table(&ovnnb.LogicalRouterPort{})
	lspHandle := r.provider.Table(&ovnnb.LogicalSwitchPort{})
	routerHandle := r.provider.Table(&ovnnb.LogicalRouter{})
	if lrpHandle == nil || lspHandle == nil || routerHandle == nil {
		return SubnetOVNMutationStateUnknown, errors.New("subnet OVN patch-port resolver tables are not configured")
	}
	router := &ovnnb.LogicalRouter{Name: intent.LogicalRouterName}
	routerExists, err := readSubnetOVNRow(ctx, routerHandle, router)
	if err != nil {
		return SubnetOVNMutationStateUnknown, err
	}
	lrp := &ovnnb.LogicalRouterPort{Name: intent.LogicalRouterPortName}
	lsp := &ovnnb.LogicalSwitchPort{Name: intent.LogicalSwitchPortName}
	lrpExists, err := readSubnetOVNRow(ctx, lrpHandle, lrp)
	if err != nil {
		return SubnetOVNMutationStateUnknown, err
	}
	lspExists, err := readSubnetOVNRow(ctx, lspHandle, lsp)
	if err != nil {
		return SubnetOVNMutationStateUnknown, err
	}
	if key.Operation == SubnetOVNCleanupOperation {
		if !lsExists && !lrpExists && !lspExists {
			return SubnetOVNMutationStateAbsent, nil
		}
		return SubnetOVNMutationStatePending, nil
	}
	if !lsExists && !lrpExists && !lspExists {
		return SubnetOVNMutationStateAbsent, nil
	}
	if !lsExists || !routerExists || !lrpExists || !lspExists || !subnetOVNLogicalRouterPortMatches(intent, router, lrp) || !subnetOVNLogicalSwitchPortMatches(intent, ls, lsp) {
		return SubnetOVNMutationStatePending, nil
	}
	return SubnetOVNMutationStateCommitted, nil
}

func readSubnetOVNRow(ctx context.Context, handle table.Handle, row model.Model) (bool, error) {
	if err := handle.Get(ctx, row); err != nil {
		if errors.Is(err, table.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func subnetOVNLogicalRouterPortMatches(intent SubnetOVNMutationIntent, router *ovnnb.LogicalRouter, row *ovnnb.LogicalRouterPort) bool {
	if router == nil || row == nil || !slices.Contains(router.Ports, row.UUID) {
		return false
	}
	if intent.GatewayMAC != "" && row.MAC != intent.GatewayMAC {
		return false
	}
	actualNetworks := slices.Clone(row.Networks)
	expectedNetworks := slices.Clone(intent.LogicalRouterNetworks)
	slices.Sort(actualNetworks)
	slices.Sort(expectedNetworks)
	if len(expectedNetworks) > 0 && !slices.Equal(actualNetworks, expectedNetworks) {
		return false
	}
	return true
}

func subnetOVNLogicalSwitchPortMatches(intent SubnetOVNMutationIntent, ls *ovnnb.LogicalSwitch, row *ovnnb.LogicalSwitchPort) bool {
	return ls != nil && row != nil && slices.Contains(ls.Ports, row.UUID) &&
		row.Type == "router" && row.Options["router-port"] == intent.LogicalRouterPortName
}

func validateSubnetOVNMutationState(state SubnetOVNMutationState) error {
	switch state {
	case SubnetOVNMutationStateCommitted, SubnetOVNMutationStateAbsent:
		return nil
	case SubnetOVNMutationStatePending:
		return ErrSubnetOVNResolverPending
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

// subnetOVNResolverAllowsRecoveryRelease keeps an allocation marker fenced
// during deletion until the cleanup readback proves that its OVN rows are
// absent. A committed allocation still owns live rows and must be retained
// while the delete reconciler removes them.
func subnetOVNResolverAllowsRecoveryRelease(snapshot SubnetWriteSnapshot, operation SubnetWriteOperation, state SubnetOVNMutationState) bool {
	if snapshot.Deleting && operation == SubnetOVNAllocateOperation {
		return state == SubnetOVNMutationStateAbsent
	}
	return subnetOVNResolverAllowsRelease(operation, state) || subnetOVNResolverAllowsTombstoneRelease(snapshot, operation, state)
}
