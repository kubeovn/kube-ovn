package controllerruntime

import (
	"context"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
)

func TestOVSDBSubnetOVNMarkerRowRoundTrip(t *testing.T) {
	marker := testOVSDBSubnetOVNMarker()
	backend := &OVSDBSubnetOVNMarkerBackend{}
	row := backend.markerRow(marker)
	decoded, err := markerFromRow(row)
	require.NoError(t, err)
	require.Equal(t, marker, decoded)
	require.Equal(t, markerName(marker.Key), row.Name)
	require.Empty(t, row.Addresses)
	equivalent := marker.Key
	require.Equal(t, markerName(marker.Key), markerName(equivalent))
}

func TestOVSDBSubnetOVNMarkerAcquireRejectsWrongOwner(t *testing.T) {
	marker := testOVSDBSubnetOVNMarker()
	other := marker
	other.Owner.Token++
	row := (&OVSDBSubnetOVNMarkerBackend{}).markerRow(other)
	backend, err := NewOVSDBSubnetOVNMarkerBackend(&markerTestProvider{handle: &markerTestHandle{row: row}})
	require.NoError(t, err)
	require.ErrorIs(t, backend.Acquire(context.Background(), marker), ErrSubnetOVNMarkerChanged)
}

func TestOVSDBSubnetOVNMarkerRejectsCorruptIdentityFields(t *testing.T) {
	marker := testOVSDBSubnetOVNMarker()
	for name, mutate := range map[string]func(map[string]string){
		"owner UID":  func(ids map[string]string) { ids[subnetOVNMarkerOwnerUID] = "other-uid" },
		"generation": func(ids map[string]string) { ids[subnetOVNMarkerKeyGen] = "4" },
		"operation":  func(ids map[string]string) { ids[subnetOVNMarkerOperation] = "unknown" },
	} {
		t.Run(name, func(t *testing.T) {
			row := (&OVSDBSubnetOVNMarkerBackend{}).markerRow(marker)
			mutate(row.ExternalIDs)
			_, err := markerFromRow(row)
			require.ErrorIs(t, err, ErrSubnetOVNMarkerInvalid)
		})
	}
}

func TestOVSDBSubnetOVNMarkerCASOperations(t *testing.T) {
	previous := testOVSDBSubnetOVNMarker()
	replacement := previous
	replacement.Owner.Token++
	replacement.Owner.WriterEpoch = "epoch-b"
	handle := &markerTestHandle{row: (&OVSDBSubnetOVNMarkerBackend{}).markerRow(previous)}
	backend, err := NewOVSDBSubnetOVNMarkerBackend(&markerTestProvider{handle: handle})
	require.NoError(t, err)

	require.NoError(t, backend.Rebind(t.Context(), previous, replacement))
	require.Len(t, handle.transactions, 1)
	require.Len(t, handle.transactions[0], 2)
	require.Equal(t, ovsdb.OperationWait, handle.transactions[0][0].Op)
	require.Equal(t, ovsdb.OperationUpdate, handle.transactions[0][1].Op)

	handle.row = (&OVSDBSubnetOVNMarkerBackend{}).markerRow(replacement)
	require.NoError(t, backend.Release(t.Context(), replacement))
	require.Len(t, handle.transactions, 2)
	require.Len(t, handle.transactions[1], 2)
	require.Equal(t, ovsdb.OperationWait, handle.transactions[1][0].Op)
	require.Equal(t, ovsdb.OperationDelete, handle.transactions[1][1].Op)
}

func TestOVSDBSubnetOVNMarkerAcquireUsesCreateIfAbsentTransaction(t *testing.T) {
	marker := testOVSDBSubnetOVNMarker()
	handle := &markerTestHandle{}
	backend, err := NewOVSDBSubnetOVNMarkerBackend(&markerTestProvider{handle: handle})
	require.NoError(t, err)
	require.NoError(t, backend.Acquire(t.Context(), marker))
	require.Len(t, handle.transactions, 1)
	require.Len(t, handle.transactions[0], 2)
	require.Equal(t, ovsdb.OperationWait, handle.transactions[0][0].Op)
	require.Equal(t, ovsdb.OperationInsert, handle.transactions[0][1].Op)
}

type markerTestProvider struct {
	handle table.Handle
}

func (p *markerTestProvider) Table(model.Model) table.Handle { return p.handle }

type markerTestHandle struct {
	row          *ovnnb.AddressSet
	pending      *ovnnb.AddressSet
	transactions [][]ovsdb.Operation
}

func (h *markerTestHandle) Get(_ context.Context, result model.Model) error {
	if h.row == nil {
		return table.ErrNotFound
	}
	*result.(*ovnnb.AddressSet) = *h.row
	return nil
}

func (*markerTestHandle) List(context.Context, any) error               { return nil }
func (*markerTestHandle) Query(context.Context, model.Model, any) error { return nil }

func (h *markerTestHandle) Filter(_ context.Context, _, result any) error {
	rows := result.(*[]ovnnb.AddressSet)
	if h.row == nil {
		*rows = nil
		return nil
	}
	*rows = []ovnnb.AddressSet{*h.row}
	return nil
}

func (*markerTestHandle) FilterByUUIDs(context.Context, any, any, ...string) error { return nil }

func (h *markerTestHandle) Where(...model.Model) table.ConditionalAPI {
	return markerTestConditional{handle: h}
}

func (h *markerTestHandle) WhereCache(any) table.ConditionalAPI {
	return markerTestConditional{handle: h}
}

func (h *markerTestHandle) CreateOps(rows ...model.Model) ([]ovsdb.Operation, error) {
	h.pending = rows[0].(*ovnnb.AddressSet)
	return []ovsdb.Operation{{Op: ovsdb.OperationInsert, Table: ovnnb.AddressSetTable}}, nil
}

func (*markerTestHandle) UpdateOps(model.Model, model.Model, ...any) ([]ovsdb.Operation, error) {
	return nil, nil
}

func (*markerTestHandle) MutateOps(model.Model, ...model.Mutation) ([]ovsdb.Operation, error) {
	return nil, nil
}

func (*markerTestHandle) DeleteOps(...model.Model) ([]ovsdb.Operation, error)  { return nil, nil }
func (*markerTestHandle) Create(context.Context, string, ...model.Model) error { return nil }

func (*markerTestHandle) Update(context.Context, string, model.Model, model.Model, ...any) error {
	return nil
}

func (*markerTestHandle) Mutate(context.Context, string, model.Model, ...model.Mutation) error {
	return nil
}

func (*markerTestHandle) Delete(context.Context, string, ...model.Model) error { return nil }
func (*markerTestHandle) DeleteFilter(context.Context, string, any) error      { return nil }

func (h *markerTestHandle) Transact(_ context.Context, _ string, operations ...ovsdb.Operation) error {
	h.transactions = append(h.transactions, append([]ovsdb.Operation(nil), operations...))
	if h.row == nil && h.pending != nil {
		h.row = h.pending
		h.pending = nil
	}
	return nil
}

type markerTestConditional struct{ handle *markerTestHandle }

func (markerTestConditional) List(context.Context, any) error { return nil }

func (markerTestConditional) Mutate(model.Model, ...model.Mutation) ([]ovsdb.Operation, error) {
	return nil, nil
}

func (markerTestConditional) Update(model.Model, ...any) ([]ovsdb.Operation, error) {
	return []ovsdb.Operation{{Op: ovsdb.OperationUpdate, Table: ovnnb.AddressSetTable}}, nil
}

func (markerTestConditional) Delete() ([]ovsdb.Operation, error) {
	return []ovsdb.Operation{{Op: ovsdb.OperationDelete, Table: ovnnb.AddressSetTable}}, nil
}

func (markerTestConditional) Wait(ovsdb.WaitCondition, *int, model.Model, ...any) ([]ovsdb.Operation, error) {
	return []ovsdb.Operation{{Op: ovsdb.OperationWait, Table: ovnnb.AddressSetTable}}, nil
}

func (markerTestConditional) Select(model.Model, ...any) ([]ovsdb.Operation, error) { return nil, nil }

func testOVSDBSubnetOVNMarker() SubnetOVNOwnerMarker {
	key := subnetOVNMutationKey(SubnetWriteSnapshot{Name: "subnet-a", UID: types.UID("uid-a"), Generation: 3}, SubnetOVNAllocateOperation)
	return SubnetOVNOwnerMarker{
		Key: SubnetOVNMutationKey{
			Name:       key.Name,
			UID:        key.UID,
			Operation:  key.Operation,
			IntentHash: key.IntentHash,
		},
		Owner: SubnetOVNOwner{
			Token:           7,
			SubnetUID:       types.UID("uid-a"),
			Generation:      3,
			ResourceVersion: "9",
			WriterEpoch:     "epoch-a",
		},
	}
}
