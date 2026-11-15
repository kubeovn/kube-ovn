package controllerruntime

import (
	"context"
	"errors"

	"k8s.io/apimachinery/pkg/types"
)

var (
	ErrSubnetMutationIdentityMissing = errors.New("subnet mutation identity is missing")
	ErrSubnetMutationIdentityChanged = errors.New("subnet mutation receipt identity changed")
	ErrSubnetWriterEpochUnavailable  = errors.New("subnet writer epoch is unavailable")
)

// SubnetMutationIdentity binds one backend mutation to the API object and
// runtime writer epoch that admitted it. Backend receipts must persist and
// compare this tuple before compensation or recovery can touch state again.
type SubnetMutationIdentity struct {
	Name            string
	UID             types.UID
	Generation      int64
	ResourceVersion string
	Operation       SubnetWriteOperation
	WriterEpoch     string
}

func (i SubnetMutationIdentity) Validate() error {
	if i.Name == "" || i.UID == "" || i.ResourceVersion == "" || i.Operation == "" {
		return ErrSubnetMutationIdentityMissing
	}
	return nil
}

func (i SubnetMutationIdentity) Equal(other SubnetMutationIdentity) bool {
	return i == other
}

type subnetMutationIdentityContextKey struct{}

func withSubnetMutationIdentity(ctx context.Context, identity SubnetMutationIdentity) context.Context {
	return context.WithValue(ctx, subnetMutationIdentityContextKey{}, identity)
}

// SubnetMutationIdentityFromContext returns the identity admitted immediately
// before a side-effect plan was applied. A missing identity must fail closed in
// concrete OVN/IPAM adapters rather than inventing ownership after the write.
func SubnetMutationIdentityFromContext(ctx context.Context) (SubnetMutationIdentity, bool) {
	if ctx == nil {
		return SubnetMutationIdentity{}, false
	}
	identity, ok := ctx.Value(subnetMutationIdentityContextKey{}).(SubnetMutationIdentity)
	return identity, ok
}

func newSubnetMutationIdentity(ctx context.Context, snapshot SubnetWriteSnapshot, operation SubnetWriteOperation, fence SubnetWriterFence) (SubnetMutationIdentity, error) {
	identity := SubnetMutationIdentity{
		Name:            snapshot.Name,
		UID:             snapshot.UID,
		Generation:      snapshot.Generation,
		ResourceVersion: snapshot.ResourceVersion,
		Operation:       operation,
	}
	if err := identity.Validate(); err != nil {
		return SubnetMutationIdentity{}, err
	}
	if fence == nil {
		return identity, nil
	}
	epochProvider, ok := fence.(SubnetWriterFenceEpochProvider)
	if !ok {
		return SubnetMutationIdentity{}, ErrSubnetWriterEpochUnavailable
	}
	epoch, err := epochProvider.Epoch(ctx)
	if err != nil {
		return SubnetMutationIdentity{}, err
	}
	if epoch == "" {
		return SubnetMutationIdentity{}, ErrSubnetWriterEpochUnavailable
	}
	identity.WriterEpoch = epoch
	return identity, nil
}
