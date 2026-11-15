package controllerruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/types"
)

var (
	ErrSubnetOVNMarkerInvalid = errors.New("invalid Subnet OVN owner marker")
	ErrSubnetOVNMarkerChanged = errors.New("subnet OVN owner marker changed")
)

func subnetOVNMutationKey(snapshot SubnetWriteSnapshot, operation SubnetWriteOperation) SubnetOVNMutationKey {
	// ResourceVersion belongs to the owner tuple, not the intent identity. API
	// status/finalizer updates can advance it while the same Subnet generation
	// and OVN operation are still in flight; keeping it out of the key lets
	// recovery find and rebind a marker left by an earlier snapshot.
	intent := fmt.Sprintf("%s\x00%s\x00%d\x00%s", snapshot.Name, snapshot.UID, snapshot.Generation, operation)
	hash := sha256.Sum256([]byte(intent))
	return SubnetOVNMutationKey{Name: snapshot.Name, UID: snapshot.UID, Operation: operation, IntentHash: hex.EncodeToString(hash[:16])}
}

// SubnetOVNOwner identifies the runtime writer that may mutate one OVN
// operation. The marker is deliberately separate from the durable receipt;
// the OVSDB adapter can persist these fields in Address_Set external_ids.
type SubnetOVNOwner struct {
	Token           uint64
	SubnetUID       types.UID
	Generation      int64
	ResourceVersion string
	WriterEpoch     string
}

func (o SubnetOVNOwner) valid() error {
	if o.Token == 0 || o.SubnetUID == "" || o.ResourceVersion == "" || o.WriterEpoch == "" {
		return ErrSubnetOVNMarkerInvalid
	}
	return nil
}

// SubnetOVNOwnerMarker is the owner tuple stored on a deterministic OVN
// marker object. IntentHash prevents unrelated operations on the same Subnet
// from sharing a marker.
type SubnetOVNOwnerMarker struct {
	Key   SubnetOVNMutationKey
	Owner SubnetOVNOwner
}

func (m SubnetOVNOwnerMarker) valid() error {
	if err := m.Owner.valid(); err != nil {
		return err
	}
	if m.Key.Name == "" || m.Key.UID == "" || m.Key.IntentHash == "" ||
		m.Owner.SubnetUID != m.Key.UID ||
		(m.Key.Operation != SubnetOVNAllocateOperation && m.Key.Operation != SubnetOVNCleanupOperation) {
		return ErrSubnetOVNMarkerInvalid
	}
	return nil
}

func (m SubnetOVNOwnerMarker) equal(other SubnetOVNOwnerMarker) bool {
	return m == other
}

// SubnetOVNMarkerBackend is the narrow CAS seam for an OVSDB owner marker.
// Implementations must compare the complete marker before every mutation.
type SubnetOVNMarkerBackend interface {
	Acquire(context.Context, SubnetOVNOwnerMarker) error
	Read(context.Context, SubnetOVNMutationKey) (SubnetOVNOwnerMarker, bool, error)
	Rebind(context.Context, SubnetOVNOwnerMarker, SubnetOVNOwnerMarker) error
	Release(context.Context, SubnetOVNOwnerMarker) error
}

// SubnetOVNMarkerLister is an optional recovery capability. Implementations
// must return every marker for a Subnet identity so active recovery can fail
// closed when an older generation or intent still owns a durable fence. Delete
// recovery uses the same complete view to resolve all markers after cleanup.
type SubnetOVNMarkerLister interface {
	List(context.Context, string, types.UID) ([]SubnetOVNOwnerMarker, error)
}

type subnetOVNMarkerReceipt struct {
	backend  SubnetOVNMarkerBackend
	marker   SubnetOVNOwnerMarker
	identity SubnetMutationIdentity
}

func (r *subnetOVNMarkerReceipt) SubnetMutationIdentity() SubnetMutationIdentity {
	if r == nil {
		return SubnetMutationIdentity{}
	}
	return r.identity
}

func (r *subnetOVNMarkerReceipt) Compensate(ctx context.Context) error {
	if r == nil || r.backend == nil {
		return ErrSubnetOVNMarkerInvalid
	}
	return r.backend.Release(ctx, r.marker)
}

type memorySubnetOVNMarkerBackend struct {
	mu      sync.Mutex
	markers map[SubnetOVNMutationKey]SubnetOVNOwnerMarker
}

func newMemorySubnetOVNMarkerBackend() *memorySubnetOVNMarkerBackend {
	return &memorySubnetOVNMarkerBackend{markers: make(map[SubnetOVNMutationKey]SubnetOVNOwnerMarker)}
}

func (b *memorySubnetOVNMarkerBackend) Acquire(ctx context.Context, marker SubnetOVNOwnerMarker) error {
	if err := marker.valid(); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if current, ok := b.markers[marker.Key]; ok && !current.equal(marker) {
		return ErrSubnetOVNMarkerChanged
	}
	b.markers[marker.Key] = marker
	return nil
}

func (b *memorySubnetOVNMarkerBackend) Read(ctx context.Context, key SubnetOVNMutationKey) (SubnetOVNOwnerMarker, bool, error) {
	if err := contextError(ctx); err != nil {
		return SubnetOVNOwnerMarker{}, false, err
	}
	if key.Name == "" || key.UID == "" || key.IntentHash == "" ||
		(key.Operation != SubnetOVNAllocateOperation && key.Operation != SubnetOVNCleanupOperation) {
		return SubnetOVNOwnerMarker{}, false, ErrSubnetOVNMarkerInvalid
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	marker, ok := b.markers[key]
	return marker, ok, nil
}

func (b *memorySubnetOVNMarkerBackend) List(ctx context.Context, name string, uid types.UID) ([]SubnetOVNOwnerMarker, error) {
	if name == "" || uid == "" {
		return nil, ErrSubnetOVNMarkerInvalid
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	markers := make([]SubnetOVNOwnerMarker, 0)
	for _, marker := range b.markers {
		if marker.Key.Name == name && marker.Key.UID == uid {
			markers = append(markers, marker)
		}
	}
	return markers, nil
}

func (b *memorySubnetOVNMarkerBackend) Rebind(ctx context.Context, previous, replacement SubnetOVNOwnerMarker) error {
	if err := previous.valid(); err != nil {
		return err
	}
	if err := replacement.valid(); err != nil {
		return err
	}
	if previous.Key != replacement.Key {
		return ErrSubnetOVNMarkerChanged
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	current, ok := b.markers[previous.Key]
	if !ok || (!current.equal(previous) && !current.equal(replacement)) {
		return ErrSubnetOVNMarkerChanged
	}
	b.markers[previous.Key] = replacement
	return nil
}

func (b *memorySubnetOVNMarkerBackend) Release(ctx context.Context, marker SubnetOVNOwnerMarker) error {
	if err := marker.valid(); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	current, ok := b.markers[marker.Key]
	if !ok || !current.equal(marker) {
		return ErrSubnetOVNMarkerChanged
	}
	delete(b.markers, marker.Key)
	return nil
}

var (
	_ SubnetOVNMarkerBackend = (*memorySubnetOVNMarkerBackend)(nil)
	_ SubnetOVNMarkerLister  = (*memorySubnetOVNMarkerBackend)(nil)
)
