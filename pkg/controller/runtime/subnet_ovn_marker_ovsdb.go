package controllerruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	subnetOVNMarkerNamePrefix = "kube_ovn_subnet_ovn_marker_"
	subnetOVNMarkerVendor     = util.CniTypeName
	subnetOVNMarkerKey        = "subnet-ovn-owner-marker"

	subnetOVNMarkerNameID      = "subnet-name"
	subnetOVNMarkerUID         = "subnet-uid"
	subnetOVNMarkerOwnerUID    = "owner-subnet-uid"
	subnetOVNMarkerKeyGen      = "subnet-generation"
	subnetOVNMarkerOperation   = "operation"
	subnetOVNMarkerIntentHash  = "intent-hash"
	subnetOVNMarkerToken       = "owner-token"
	subnetOVNMarkerGeneration  = "owner-generation"
	subnetOVNMarkerResourceVer = "owner-resource-version"
	subnetOVNMarkerWriterEpoch = "owner-writer-epoch"
)

// OVSDBSubnetOVNMarkerBackend stores owner markers in dedicated Address_Set
// rows. The rows have no addresses and are identified by a deterministic name;
// their external_ids carry the complete owner tuple. This keeps marker state
// separate from user data and lets each mutation use a conditional transaction.
type OVSDBSubnetOVNMarkerBackend struct {
	provider table.Provider
}

func NewOVSDBSubnetOVNMarkerBackend(provider table.Provider) (*OVSDBSubnetOVNMarkerBackend, error) {
	if provider == nil {
		return nil, errors.New("subnet OVN marker backend requires an OVSDB provider")
	}
	return &OVSDBSubnetOVNMarkerBackend{provider: provider}, nil
}

func (b *OVSDBSubnetOVNMarkerBackend) markerRow(marker SubnetOVNOwnerMarker) *ovnnb.AddressSet {
	return &ovnnb.AddressSet{
		Name: markerName(marker.Key),
		ExternalIDs: map[string]string{
			"vendor":              subnetOVNMarkerVendor,
			subnetOVNMarkerKey:    "true",
			subnetOVNMarkerNameID: marker.Key.Name,

			subnetOVNMarkerUID:         string(marker.Key.UID),
			subnetOVNMarkerOwnerUID:    string(marker.Owner.SubnetUID),
			subnetOVNMarkerKeyGen:      strconv.FormatInt(marker.Owner.Generation, 10),
			subnetOVNMarkerOperation:   string(marker.Key.Operation),
			subnetOVNMarkerIntentHash:  marker.Key.IntentHash,
			subnetOVNMarkerToken:       strconv.FormatUint(marker.Owner.Token, 10),
			subnetOVNMarkerGeneration:  strconv.FormatInt(marker.Owner.Generation, 10),
			subnetOVNMarkerResourceVer: marker.Owner.ResourceVersion,
			subnetOVNMarkerWriterEpoch: marker.Owner.WriterEpoch,
		},
	}
}

func (b *OVSDBSubnetOVNMarkerBackend) row(ctx context.Context, key SubnetOVNMutationKey) (*ovnnb.AddressSet, bool, error) {
	if b == nil || b.provider == nil {
		return nil, false, errors.New("subnet OVN marker backend is not configured")
	}
	if err := contextError(ctx); err != nil {
		return nil, false, err
	}
	for _, name := range []string{markerName(key), legacyMarkerName(key)} {
		row := &ovnnb.AddressSet{Name: name}
		if err := b.provider.Table(&ovnnb.AddressSet{}).Get(ctx, row); err != nil {
			if errors.Is(err, table.ErrNotFound) {
				continue
			}
			return nil, false, err
		}
		return row, true, nil
	}
	return nil, false, nil
}

func (b *OVSDBSubnetOVNMarkerBackend) Acquire(ctx context.Context, marker SubnetOVNOwnerMarker) error {
	if err := marker.valid(); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	row, found, err := b.row(ctx, marker.Key)
	if err != nil {
		return err
	}
	if found {
		current, err := markerFromRow(row)
		if err != nil {
			return err
		}
		if !current.equal(marker) {
			return ErrSubnetOVNMarkerChanged
		}
		return nil
	}
	if err := table.CreateIfAbsentByName(ctx, b.provider, b.markerRow(marker), ovnnb.AddressSetTable, markerName(marker.Key), "subnet-ovn-marker-acquire", func(row *ovnnb.AddressSet) string { return row.Name }); err != nil {
		return err
	}
	if barrier, ok := b.provider.(table.CacheBarrier); ok {
		rows := make([]ovnnb.AddressSet, 0, 1)
		if err := barrier.WaitForRows(ctx, &ovnnb.AddressSet{}, func(row *ovnnb.AddressSet) bool {
			return row.Name == markerName(marker.Key)
		}, &rows); err != nil {
			return err
		}
	}
	row, found, err = b.row(ctx, marker.Key)
	if err != nil {
		return err
	}
	if !found {
		return ErrSubnetOVNMarkerChanged
	}
	current, err := markerFromRow(row)
	if err != nil {
		return err
	}
	if !current.equal(marker) {
		return ErrSubnetOVNMarkerChanged
	}
	return nil
}

func (b *OVSDBSubnetOVNMarkerBackend) Read(ctx context.Context, key SubnetOVNMutationKey) (SubnetOVNOwnerMarker, bool, error) {
	if err := validateSubnetOVNMutationKey(key); err != nil {
		return SubnetOVNOwnerMarker{}, false, err
	}
	row, found, err := b.row(ctx, key)
	if err != nil || !found {
		return SubnetOVNOwnerMarker{}, found, err
	}
	marker, err := markerFromRow(row)
	if err != nil {
		return SubnetOVNOwnerMarker{}, false, err
	}
	if marker.Key != key {
		return SubnetOVNOwnerMarker{}, false, ErrSubnetOVNMarkerChanged
	}
	return marker, true, nil
}

func (b *OVSDBSubnetOVNMarkerBackend) List(ctx context.Context, name string, uid types.UID) ([]SubnetOVNOwnerMarker, error) {
	if b == nil || b.provider == nil {
		return nil, errors.New("subnet OVN marker backend is not configured")
	}
	if name == "" || uid == "" {
		return nil, ErrSubnetOVNMarkerInvalid
	}
	var rows []ovnnb.AddressSet
	if err := b.provider.Table(&ovnnb.AddressSet{}).Filter(ctx, func(row *ovnnb.AddressSet) bool {
		return row.ExternalIDs[subnetOVNMarkerKey] == "true" &&
			row.ExternalIDs[subnetOVNMarkerNameID] == name && row.ExternalIDs[subnetOVNMarkerUID] == string(uid)
	}, &rows); err != nil {
		return nil, err
	}
	markers := make([]SubnetOVNOwnerMarker, 0, len(rows))
	for i := range rows {
		marker, err := markerFromRow(&rows[i])
		if err != nil {
			return nil, err
		}
		markers = append(markers, marker)
	}
	return markers, nil
}

func (b *OVSDBSubnetOVNMarkerBackend) Rebind(ctx context.Context, previous, replacement SubnetOVNOwnerMarker) error {
	if err := previous.valid(); err != nil {
		return err
	}
	if err := replacement.valid(); err != nil {
		return err
	}
	if previous.Key != replacement.Key {
		return ErrSubnetOVNMarkerChanged
	}
	row, found, err := b.row(ctx, previous.Key)
	if err != nil {
		return err
	}
	if !found {
		return ErrSubnetOVNMarkerChanged
	}
	current, err := markerFromRow(row)
	if err != nil {
		return err
	}
	if current.equal(replacement) {
		return nil
	}
	if !current.equal(previous) {
		return ErrSubnetOVNMarkerChanged
	}
	return b.conditionalUpdate(ctx, row.Name, previous, replacement)
}

func (b *OVSDBSubnetOVNMarkerBackend) Release(ctx context.Context, marker SubnetOVNOwnerMarker) error {
	if err := marker.valid(); err != nil {
		return err
	}
	row, found, err := b.row(ctx, marker.Key)
	if err != nil {
		return err
	}
	if !found {
		return ErrSubnetOVNMarkerChanged
	}
	current, err := markerFromRow(row)
	if err != nil {
		return err
	}
	if !current.equal(marker) {
		return ErrSubnetOVNMarkerChanged
	}
	selector := &ovnnb.AddressSet{Name: row.Name}
	expected := b.markerRow(marker)
	expected.Name = row.Name
	wait, err := b.provider.Table(&ovnnb.AddressSet{}).Where(selector).Wait(ovsdb.WaitConditionEqual, new(0), expected, &expected.Name, &expected.ExternalIDs)
	if err != nil {
		return err
	}
	deletes, err := b.provider.Table(&ovnnb.AddressSet{}).Where(selector).Delete()
	if err != nil {
		return err
	}
	ops := append(wait, deletes...)
	if err := b.provider.Table(&ovnnb.AddressSet{}).Transact(ctx, "subnet-ovn-marker-release", ops...); err != nil {
		if _, timedOut := errors.AsType[*ovsdb.TimedOut](err); timedOut {
			return ErrSubnetOVNMarkerChanged
		}
		return err
	}
	return nil
}

func (b *OVSDBSubnetOVNMarkerBackend) conditionalUpdate(ctx context.Context, name string, previous, replacement SubnetOVNOwnerMarker) error {
	tableHandle := b.provider.Table(&ovnnb.AddressSet{})
	selector := &ovnnb.AddressSet{Name: name}
	update := b.markerRow(replacement)
	update.Name = name
	expected := b.markerRow(previous)
	expected.Name = name
	wait, err := tableHandle.Where(selector).Wait(ovsdb.WaitConditionEqual, new(0), expected, &expected.Name, &expected.ExternalIDs)
	if err != nil {
		return err
	}
	updates, err := tableHandle.Where(selector).Update(update, &update.ExternalIDs)
	if err != nil {
		return err
	}
	if err := tableHandle.Transact(ctx, "subnet-ovn-marker-rebind", append(wait, updates...)...); err != nil {
		if _, timedOut := errors.AsType[*ovsdb.TimedOut](err); timedOut {
			return ErrSubnetOVNMarkerChanged
		}
		return err
	}
	return nil
}

func markerFromRow(row *ovnnb.AddressSet) (SubnetOVNOwnerMarker, error) {
	if row == nil || row.ExternalIDs[subnetOVNMarkerKey] != "true" || row.ExternalIDs["vendor"] != subnetOVNMarkerVendor {
		return SubnetOVNOwnerMarker{}, ErrSubnetOVNMarkerInvalid
	}
	ids := row.ExternalIDs
	token, err := strconv.ParseUint(ids[subnetOVNMarkerToken], 10, 64)
	if err != nil {
		return SubnetOVNOwnerMarker{}, fmt.Errorf("decode subnet OVN marker token: %w", err)
	}
	generation, err := strconv.ParseInt(ids[subnetOVNMarkerGeneration], 10, 64)
	if err != nil {
		return SubnetOVNOwnerMarker{}, fmt.Errorf("decode subnet OVN marker generation: %w", err)
	}
	keyGeneration, err := strconv.ParseInt(ids[subnetOVNMarkerKeyGen], 10, 64)
	if err != nil || keyGeneration != generation {
		return SubnetOVNOwnerMarker{}, ErrSubnetOVNMarkerInvalid
	}
	marker := SubnetOVNOwnerMarker{
		Key:   SubnetOVNMutationKey{Name: ids[subnetOVNMarkerNameID], UID: types.UID(ids[subnetOVNMarkerUID]), Operation: SubnetWriteOperation(ids[subnetOVNMarkerOperation]), IntentHash: ids[subnetOVNMarkerIntentHash]},
		Owner: SubnetOVNOwner{Token: token, SubnetUID: types.UID(ids[subnetOVNMarkerOwnerUID]), Generation: generation, ResourceVersion: ids[subnetOVNMarkerResourceVer], WriterEpoch: ids[subnetOVNMarkerWriterEpoch]},
	}
	if err := marker.valid(); err != nil {
		return SubnetOVNOwnerMarker{}, err
	}
	if row.Name != markerName(marker.Key) && row.Name != legacyMarkerName(marker.Key) {
		return SubnetOVNOwnerMarker{}, ErrSubnetOVNMarkerInvalid
	}
	return marker, nil
}

func markerName(key SubnetOVNMutationKey) string {
	identity := string(key.UID) + "\x00" + key.Name + "\x00" + string(key.Operation) + "\x00" + key.IntentHash
	hash := sha256.Sum256([]byte(identity))
	return subnetOVNMarkerNamePrefix + hex.EncodeToString(hash[:16])
}

func legacyMarkerName(key SubnetOVNMutationKey) string {
	return subnetOVNMarkerNamePrefix + key.IntentHash
}

func validateSubnetOVNMutationKey(key SubnetOVNMutationKey) error {
	if key.Name == "" || key.UID == "" || key.IntentHash == "" || (key.Operation != SubnetOVNAllocateOperation && key.Operation != SubnetOVNCleanupOperation) {
		return ErrSubnetOVNMarkerInvalid
	}
	return nil
}

var (
	_ SubnetOVNMarkerBackend = (*OVSDBSubnetOVNMarkerBackend)(nil)
	_ SubnetOVNMarkerLister  = (*OVSDBSubnetOVNMarkerBackend)(nil)
)
