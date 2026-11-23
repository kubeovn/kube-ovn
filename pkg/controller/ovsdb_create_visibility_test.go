package controller

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
)

type hiddenCreatedRowsBackend struct {
	*table.Client
	hidden reflect.Type
}

type hiddenCreatedRowsConditional struct {
	table.ConditionalAPI
	backend *hiddenCreatedRowsBackend
}

func (b *hiddenCreatedRowsBackend) Transact(ctx context.Context, operations ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	return b.TransactResults(ctx, operations...)
}

func (b *hiddenCreatedRowsBackend) WhereCache(predicate any) table.ConditionalAPI {
	return hiddenCreatedRowsConditional{ConditionalAPI: b.Client.WhereCache(predicate), backend: b}
}

func (c hiddenCreatedRowsConditional) List(ctx context.Context, result any) error {
	if reflect.TypeOf(result) == c.backend.hidden {
		return nil
	}
	return c.ConditionalAPI.List(ctx, result)
}

func TestControllerProviderBFDRequiresVisibleUUID(t *testing.T) {
	client := newInMemoryOVNNbClient(t)
	backend := &hiddenCreatedRowsBackend{Client: client.Client, hidden: reflect.TypeFor[*[]ovnnb.BFD]()}
	controller := &Controller{OVNNbTables: table.NewDatabase(backend, time.Second, table.RetryPolicy{})}
	row, err := controller.createBFD("gateway-port", "192.0.2.10", 100, 200, 3, nil)
	require.ErrorIs(t, err, table.ErrNotFound)
	require.Nil(t, row)
	require.Eventually(t, func() bool {
		rows, err := client.ListBFDs("gateway-port", "192.0.2.10")
		return err == nil && len(rows) == 1
	}, time.Second, time.Millisecond)
	backend.hidden = nil
	row, err = controller.createBFD("gateway-port", "192.0.2.10", 100, 200, 3, nil)
	require.NoError(t, err)
	require.True(t, ovsdb.IsValidUUID(row.UUID))
}

func TestControllerProviderDHCPRequiresVisibleUUID(t *testing.T) {
	client := newInMemoryOVNNbClient(t)
	backend := &hiddenCreatedRowsBackend{Client: client.Client, hidden: reflect.TypeFor[*[]ovnnb.DHCPOptions]()}
	controller := &Controller{OVNNbTables: table.NewDatabase(backend, time.Second, table.RetryPolicy{})}
	uuid, err := controller.updateDHCPOptionTable("subnet", "", "10.0.0.0/24", kubeovnv1.ProtocolIPv4, "10.0.0.1", "", 1500)
	require.ErrorIs(t, err, table.ErrNotFound)
	require.Empty(t, uuid)
	require.Eventually(t, func() bool {
		rows, err := client.ListDHCPOptions(true, nil)
		return err == nil && len(rows) == 1
	}, time.Second, time.Millisecond)
	backend.hidden = nil
	uuid, err = controller.updateDHCPOptionTable("subnet", "", "10.0.0.0/24", kubeovnv1.ProtocolIPv4, "10.0.0.1", "", 1500)
	require.NoError(t, err)
	require.True(t, ovsdb.IsValidUUID(uuid))
}
