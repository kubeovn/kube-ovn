package controllerruntime

import (
	"context"
	"errors"

	"k8s.io/apimachinery/pkg/types"
)

// prepareSubnetOVNReceipt persists the operation owner before the OVSDB
// marker is acquired. The two stores are deliberately coordinated as a
// retryable saga: a receipt that survives a process crash is resolved by
// recovery rather than being silently discarded.
func prepareSubnetOVNReceipt(ctx context.Context, store subnetOVNMutationReceiptStore, marker SubnetOVNOwnerMarker, identity SubnetMutationIdentity, intent SubnetOVNMutationIntent) (SubnetOVNOwnerMarker, SubnetOVNMutationReceipt, error) {
	if store == nil {
		return marker, SubnetOVNMutationReceipt{}, errors.New("subnet OVN receipt store is not configured")
	}
	receipt, found, err := store.Lookup(ctx, marker.Key)
	if err != nil {
		return marker, SubnetOVNMutationReceipt{}, err
	}
	if found {
		if err := receipt.valid(); err != nil {
			return marker, SubnetOVNMutationReceipt{}, err
		}
		if receipt.Phase != SubnetOVNReceiptPhaseActive || receipt.Key != marker.Key ||
			receipt.Key.UID != identity.UID || receipt.Generation != identity.Generation || receipt.WriterEpoch != identity.WriterEpoch {
			return marker, SubnetOVNMutationReceipt{}, ErrSubnetOVNReceiptChanged
		}
		marker.Owner.Token = receipt.Token
		replacement := receipt
		replacement.Intent = intent
		if receipt.ResourceVersion != identity.ResourceVersion {
			replacement.ResourceVersion = identity.ResourceVersion
		}
		if !replacement.equal(receipt) {
			if err := store.Rebind(ctx, receipt, replacement); err != nil {
				return marker, SubnetOVNMutationReceipt{}, err
			}
			receipt = replacement
		}
		return marker, receipt, nil
	}
	receipt = SubnetOVNMutationReceipt{
		Key:             marker.Key,
		Intent:          intent,
		Token:           marker.Owner.Token,
		Generation:      identity.Generation,
		ResourceVersion: identity.ResourceVersion,
		WriterEpoch:     identity.WriterEpoch,
		Phase:           SubnetOVNReceiptPhaseActive,
	}
	if err := store.Remember(ctx, receipt); err != nil {
		return marker, SubnetOVNMutationReceipt{}, err
	}
	return marker, receipt, nil
}

func finalizeSubnetOVNReceipt(ctx context.Context, store subnetOVNMutationReceiptStore, marker SubnetOVNOwnerMarker, receipt SubnetOVNMutationReceipt, release func(context.Context, SubnetOVNOwnerMarker) error) error {
	claimed, err := store.Claim(ctx, receipt)
	if err != nil {
		return err
	}
	if err := release(ctx, marker); err != nil {
		return err
	}
	return store.Delete(ctx, claimed)
}

func rebindSubnetOVNReceiptIdentity(receipt SubnetOVNMutationReceipt, uid types.UID, generation int64, resourceVersion, writerEpoch string) (SubnetOVNMutationReceipt, error) {
	if receipt.Key.UID != uid || receipt.Generation != generation {
		return SubnetOVNMutationReceipt{}, ErrSubnetOVNReceiptChanged
	}
	receipt.ResourceVersion = resourceVersion
	receipt.WriterEpoch = writerEpoch
	return receipt, nil
}

func subnetOVNReceiptOwnerMatchesMarker(receipt SubnetOVNMutationReceipt, marker SubnetOVNOwnerMarker) bool {
	return receipt.Key == marker.Key && receipt.Token == marker.Owner.Token && receipt.Key.UID == marker.Owner.SubnetUID &&
		receipt.Generation == marker.Owner.Generation && receipt.ResourceVersion == marker.Owner.ResourceVersion &&
		receipt.WriterEpoch == marker.Owner.WriterEpoch
}

func (a *SubnetControllerWriterAdapter) resolveSubnetOVNReceipt(ctx context.Context, snapshot SubnetWriteSnapshot, receipt SubnetOVNMutationReceipt) (SubnetOVNMutationState, error) {
	if a == nil || a.ovnResolver == nil {
		return SubnetOVNMutationStateUnknown, ErrSubnetOVNResolverMissing
	}
	state, err := a.ovnResolver.ResolveSubnetOVNMutation(ctx, snapshot, receipt.Key, receipt)
	if err != nil {
		return SubnetOVNMutationStateUnknown, err
	}
	if err := validateSubnetOVNMutationState(state); err != nil {
		return SubnetOVNMutationStateUnknown, err
	}
	return state, nil
}

func (a *SubnetControllerWriterAdapter) recoverSubnetOVNReceipts(ctx context.Context, snapshot SubnetWriteSnapshot) error {
	if a == nil || a.ovnReceipts == nil {
		return nil
	}
	if a.ovnMarker == nil {
		return errors.New("subnet OVN receipt recovery requires an OVN marker backend")
	}
	lister, ok := a.ovnReceipts.(subnetOVNMutationReceiptLister)
	if !ok {
		return errors.New("subnet OVN receipt recovery requires a receipt lister")
	}
	receipts, err := lister.List(ctx, snapshot.Name)
	if err != nil {
		return err
	}
	for _, receipt := range receipts {
		if err := validateSubnetOVNReceiptForRecovery(snapshot, receipt); err != nil {
			return err
		}
		receipt, err = a.rebindSubnetOVNReceiptForRecovery(ctx, snapshot, receipt)
		if err != nil {
			return err
		}
		if err := a.recoverSubnetOVNReceipt(ctx, snapshot, receipt); err != nil {
			return err
		}
	}
	return nil
}

func validateSubnetOVNReceiptForRecovery(snapshot SubnetWriteSnapshot, receipt SubnetOVNMutationReceipt) error {
	if receipt.Key.UID != snapshot.UID {
		return ErrSubnetOVNReceiptStale
	}
	if !snapshot.Deleting && receipt.Key.Operation != SubnetOVNAllocateOperation {
		return ErrSubnetOVNReceiptStale
	}
	if !snapshot.Deleting && receipt.Generation != snapshot.Generation {
		return ErrSubnetOVNReceiptStale
	}
	return nil
}

func (a *SubnetControllerWriterAdapter) rebindSubnetOVNReceiptForRecovery(ctx context.Context, snapshot SubnetWriteSnapshot, receipt SubnetOVNMutationReceipt) (SubnetOVNMutationReceipt, error) {
	if receipt.Generation != snapshot.Generation {
		return receipt, nil
	}
	receiptIdentity, err := newSubnetMutationIdentity(ctx, snapshot, receipt.Key.Operation, a.Gate.Fence)
	if err != nil {
		return SubnetOVNMutationReceipt{}, err
	}
	replacement, err := rebindSubnetOVNReceiptIdentity(receipt, receiptIdentity.UID, receiptIdentity.Generation, receiptIdentity.ResourceVersion, receiptIdentity.WriterEpoch)
	if err != nil {
		return SubnetOVNMutationReceipt{}, err
	}
	if replacement.equal(receipt) {
		return receipt, nil
	}
	if err := a.ovnReceipts.Rebind(ctx, receipt, replacement); err != nil {
		return SubnetOVNMutationReceipt{}, err
	}
	return replacement, nil
}

func (a *SubnetControllerWriterAdapter) recoverSubnetOVNReceipt(ctx context.Context, snapshot SubnetWriteSnapshot, receipt SubnetOVNMutationReceipt) error {
	state, err := a.resolveSubnetOVNReceipt(ctx, snapshot, receipt)
	if err != nil {
		return err
	}
	marker, found, err := a.ovnMarker.Read(ctx, receipt.Key)
	if err != nil {
		return err
	}
	canRelease := subnetOVNResolverAllowsRecoveryRelease(snapshot, receipt.Key.Operation, state)
	if !snapshot.Deleting && receipt.Key.Operation == SubnetOVNAllocateOperation && state == SubnetOVNMutationStateAbsent && isCoarseOrLegacySubnetOVNMutationKey(receipt.Key, snapshot.Generation) {
		return a.releaseRecoveredSubnetOVNReceipt(ctx, receipt, marker, found)
	}
	if receipt.Phase == SubnetOVNReceiptPhaseReleasing {
		keepDeletionAllocation := snapshot.Deleting && receipt.Key.Operation == SubnetOVNAllocateOperation
		if canRelease {
			return a.releaseRecoveredSubnetOVNReceipt(ctx, receipt, marker, found)
		}
		if !keepDeletionAllocation {
			return ErrSubnetOVNResolverPending
		}
	}
	if canRelease {
		return a.releaseRecoveredSubnetOVNReceipt(ctx, receipt, marker, found)
	}
	if !found {
		return a.acquireRecoveredSubnetOVNMarker(ctx, snapshot, receipt)
	}
	if !subnetOVNReceiptOwnerMatchesMarker(receipt, marker) {
		return ErrSubnetOVNReceiptChanged
	}
	return nil
}

func (a *SubnetControllerWriterAdapter) releaseRecoveredSubnetOVNReceipt(ctx context.Context, receipt SubnetOVNMutationReceipt, marker SubnetOVNOwnerMarker, found bool) error {
	if found {
		if !subnetOVNReceiptOwnerMatchesMarker(receipt, marker) {
			return ErrSubnetOVNReceiptChanged
		}
		if err := a.ovnMarker.Release(ctx, marker); err != nil {
			return err
		}
	}
	return a.ovnReceipts.Delete(ctx, receipt)
}

func (a *SubnetControllerWriterAdapter) acquireRecoveredSubnetOVNMarker(ctx context.Context, snapshot SubnetWriteSnapshot, receipt SubnetOVNMutationReceipt) error {
	if snapshot.Deleting && receipt.Generation != snapshot.Generation {
		return nil
	}
	marker := SubnetOVNOwnerMarker{Key: receipt.Key, Owner: SubnetOVNOwner{
		Token: receipt.Token, SubnetUID: receipt.Key.UID, Generation: receipt.Generation,
		ResourceVersion: receipt.ResourceVersion, WriterEpoch: receipt.WriterEpoch,
	}}
	return a.ovnMarker.Acquire(ctx, marker)
}

func (a *SubnetControllerWriterAdapter) releaseSubnetOVNReceipts(ctx context.Context, snapshot SubnetWriteSnapshot) error {
	if a == nil || a.ovnReceipts == nil {
		return nil
	}
	if a.ovnMarker == nil {
		return errors.New("subnet OVN receipt release requires an OVN marker backend")
	}
	lister, ok := a.ovnReceipts.(subnetOVNMutationReceiptLister)
	if !ok {
		return errors.New("subnet OVN receipt release requires a receipt lister")
	}
	receipts, err := lister.List(ctx, snapshot.Name)
	if err != nil {
		return err
	}
	for _, receipt := range receipts {
		if receipt.Key.UID != snapshot.UID {
			return ErrSubnetOVNReceiptStale
		}
		state, err := a.resolveSubnetOVNReceipt(ctx, snapshot, receipt)
		if err != nil {
			return err
		}
		if !subnetOVNResolverAllowsRelease(receipt.Key.Operation, state) && !subnetOVNResolverAllowsTombstoneRelease(snapshot, receipt.Key.Operation, state) {
			return ErrSubnetOVNResolverPending
		}
		claimed := receipt
		if receipt.Phase == SubnetOVNReceiptPhaseActive {
			claimed, err = a.ovnReceipts.Claim(ctx, receipt)
			if err != nil {
				return err
			}
		}
		marker, found, err := a.ovnMarker.Read(ctx, receipt.Key)
		if err != nil {
			return err
		}
		if found {
			if !subnetOVNReceiptOwnerMatchesMarker(claimed, marker) {
				return ErrSubnetOVNReceiptChanged
			}
			if err := a.ovnMarker.Release(ctx, marker); err != nil {
				return err
			}
		}
		if err := a.ovnReceipts.Delete(ctx, claimed); err != nil {
			return err
		}
	}
	return nil
}
