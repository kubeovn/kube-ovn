package compat

import (
	"context"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"
)

type exampleRow struct {
	Name string
}

func ExampleTable_reconcile() {
	var database *Database // supplied by controller wiring
	table := database.Table(&exampleRow{})
	ctx := context.Background()

	var rows []exampleRow
	_ = table.List(ctx, &rows)
	_ = table.Query(ctx, &exampleRow{Name: "key"}, &rows)
	_ = table.Filter(ctx, func(row *exampleRow) bool { return row.Name != "" }, &rows)
	_ = table.Create(ctx, "resource-create", &exampleRow{Name: "desired"})
	_ = table.Update(ctx, "resource-update", &exampleRow{Name: "key"}, &exampleRow{Name: "desired"}, "Name")
	_ = table.Delete(ctx, "resource-delete", &exampleRow{Name: "key"})
	// Output:
}

func TestTableQueryAndFilterUseDatabasePolicies(t *testing.T) {
	called := make(chan context.Context, 2)
	conditional := recordingConditional{list: func(ctx context.Context, _ any) error {
		called <- ctx
		return nil
	}}
	database := NewDatabase(&fakeBackend{conditional: conditional}, 50*time.Millisecond, RetryPolicy{})
	table := database.Table(&struct{}{})

	rows := []struct{}{}
	require.NoError(t, table.Query(context.Background(), nil, &rows))
	require.NoError(t, table.Filter(context.Background(), func(*struct{}) bool { return true }, &rows))

	for range 2 {
		select {
		case ctx := <-called:
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			require.LessOrEqual(t, time.Until(deadline), 50*time.Millisecond)
		case <-time.After(time.Second):
			t.Fatal("table query did not reach the conditional backend")
		}
	}
}

func TestTableCRUDDelegatesToDatabase(t *testing.T) {
	fake := &fakeBackend{
		create: func(...model.Model) ([]ovsdb.Operation, error) {
			return tableOperation(), nil
		},
		conditional: crudConditional{},
		transact: func(context.Context, ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
			return []ovsdb.OperationResult{{}}, nil
		},
	}
	database := NewDatabase(fake, time.Second, RetryPolicy{})
	table := database.Table(&struct{}{})
	row := &struct{}{}

	require.NoError(t, table.Create(context.Background(), "create-row", row))
	require.NoError(t, table.Update(context.Background(), "update-row", row, row))
	require.NoError(t, table.Mutate(context.Background(), "mutate-row", row))
	require.NoError(t, table.Delete(context.Background(), "delete-row", row))
	require.NoError(t, table.DeleteFilter(context.Background(), "delete-filtered", func(*struct{}) bool { return true }))
	require.Equal(t, 5, fake.transacts)
}

func TestTableOperationBuilders(t *testing.T) {
	fake := &fakeBackend{
		create:      func(...model.Model) ([]ovsdb.Operation, error) { return tableOperation(), nil },
		conditional: crudConditional{},
	}
	table := NewDatabase(fake, time.Second, RetryPolicy{}).Table(&struct{}{})
	row := &struct{}{}

	for _, build := range []func() ([]ovsdb.Operation, error){
		func() ([]ovsdb.Operation, error) { return table.CreateOps(row) },
		func() ([]ovsdb.Operation, error) { return table.UpdateOps(row, row) },
		func() ([]ovsdb.Operation, error) { return table.MutateOps(row) },
		func() ([]ovsdb.Operation, error) { return table.DeleteOps(row) },
	} {
		operations, err := build()
		require.NoError(t, err)
		require.NotEmpty(t, operations)
	}
}

func tableOperation() []ovsdb.Operation {
	return []ovsdb.Operation{{Op: ovsdb.OperationComment, Comment: new("table")}}
}

type crudConditional struct{}

func (crudConditional) List(context.Context, any) error { return nil }

func (crudConditional) Mutate(model.Model, ...model.Mutation) ([]ovsdb.Operation, error) {
	return tableOperation(), nil
}

func (crudConditional) Update(model.Model, ...any) ([]ovsdb.Operation, error) {
	return tableOperation(), nil
}

func (crudConditional) Delete() ([]ovsdb.Operation, error) {
	return tableOperation(), nil
}

func (crudConditional) Wait(ovsdb.WaitCondition, *int, model.Model, ...any) ([]ovsdb.Operation, error) {
	return tableOperation(), nil
}

func (crudConditional) Select(model.Model, ...any) ([]ovsdb.Operation, error) {
	return tableOperation(), nil
}

func TestTableRejectsMissingPrototype(t *testing.T) {
	database := NewDatabase(&fakeBackend{}, time.Second, RetryPolicy{})
	table := database.Table(nil)

	require.Error(t, table.List(context.Background(), &[]struct{}{}))
	require.Error(t, table.Get(context.Background(), &struct{}{}))
}
