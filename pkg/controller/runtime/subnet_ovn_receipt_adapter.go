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
func prepareSubnetOVNReceipt(ctx context.Context, store subnetOVNMutationReceiptStore, marker SubnetOVNOwnerMarker, identity SubnetMutationIdentity) (SubnetOVNOwnerMarker, SubnetOVNMutationReceipt, error) {
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
		if receipt.ResourceVersion != identity.ResourceVersion {
			replacement := receipt
			replacement.ResourceVersion = identity.ResourceVersion
			if err := store.Rebind(ctx, receipt, replacement); err != nil {
				return marker, SubnetOVNMutationReceipt{}, err
			}
			receipt = replacement
		}
		return marker, receipt, nil
	}
	receipt = SubnetOVNMutationReceipt{
		Key:             marker.Key,
		Intent:          SubnetOVNMutationIntent{LogicalSwitchName: marker.Key.Name, Generation: identity.Generation},
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
	operation := SubnetOVNAllocateOperation
	if snapshot.Deleting {
		operation = SubnetOVNCleanupOperation
	}
	key := subnetOVNMutationKey(snapshot, operation)
	legacyKey := legacySubnetOVNMutationKey(snapshot, operation)
	for _, receipt := range receipts {
		if receipt.Key.UID != snapshot.UID {
			return ErrSubnetOVNReceiptStale
		}
		if !snapshot.Deleting && receipt.Key != key && receipt.Key != legacyKey {
			return ErrSubnetOVNReceiptStale
		}
		if receipt.Generation == snapshot.Generation {
			receiptIdentity, identityErr := newSubnetMutationIdentity(ctx, snapshot, receipt.Key.Operation, a.Gate.Fence)
			if identityErr != nil {
				return identityErr
			}
			replacementReceipt, rebindErr := rebindSubnetOVNReceiptIdentity(receipt, receiptIdentity.UID, receiptIdentity.Generation, receiptIdentity.ResourceVersion, receiptIdentity.WriterEpoch)
			if rebindErr != nil {
				return rebindErr
			}
			if replacementReceipt != receipt {
				if err := a.ovnReceipts.Rebind(ctx, receipt, replacementReceipt); err != nil {
					return err
				}
				receipt = replacementReceipt
			}
		}
		state, err := a.resolveSubnetOVNReceipt(ctx, snapshot, receipt)
		if err != nil {
			return err
		}
		marker, found, err := a.ovnMarker.Read(ctx, receipt.Key)
		if err != nil {
			return err
		}
		if receipt.Phase == SubnetOVNReceiptPhaseReleasing {
			if !subnetOVNResolverAllowsRelease(receipt.Key.Operation, state) && !subnetOVNResolverAllowsTombstoneRelease(snapshot, receipt.Key.Operation, state) {
				return ErrSubnetOVNResolverPending
			}
			if found {
				if !subnetOVNReceiptOwnerMatchesMarker(receipt, marker) {
					return ErrSubnetOVNReceiptChanged
				}
				if err := a.ovnMarker.Release(ctx, marker); err != nil {
					return err
				}
			}
			if err := a.ovnReceipts.Delete(ctx, receipt); err != nil {
				return err
			}
			continue
		}
		if subnetOVNResolverAllowsRelease(receipt.Key.Operation, state) || subnetOVNResolverAllowsTombstoneRelease(snapshot, receipt.Key.Operation, state) {
			if found {
				if !subnetOVNReceiptOwnerMatchesMarker(receipt, marker) {
					return ErrSubnetOVNReceiptChanged
				}
				if err := a.ovnMarker.Release(ctx, marker); err != nil {
					return err
				}
			}
			if err := a.ovnReceipts.Delete(ctx, receipt); err != nil {
				return err
			}
			continue
		}
		if !found {
			if snapshot.Deleting && receipt.Generation != snapshot.Generation {
				continue
			}
			marker = SubnetOVNOwnerMarker{Key: receipt.Key, Owner: SubnetOVNOwner{
				Token: receipt.Token, SubnetUID: receipt.Key.UID, Generation: receipt.Generation,
				ResourceVersion: receipt.ResourceVersion, WriterEpoch: receipt.WriterEpoch,
			}}
			if err := a.ovnMarker.Acquire(ctx, marker); err != nil {
				return err
			}
			continue
		}
		if !subnetOVNReceiptOwnerMatchesMarker(receipt, marker) {
			return ErrSubnetOVNReceiptChanged
		}
		if snapshot.Deleting || receipt.Key != key {
			continue
		}
	}
	return nil
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
