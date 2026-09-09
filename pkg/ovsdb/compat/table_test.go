package compat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"
)

type exampleRow struct {
	Name string
}

type otherExampleRow struct {
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
	require.NoError(t, table.Transact(context.Background(), "composed", tableOperation()...))
	require.Equal(t, 6, fake.transacts)
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

func TestTableRejectsModelsFromAnotherTable(t *testing.T) {
	database := NewDatabase(&fakeBackend{conditional: crudConditional{}}, time.Second, RetryPolicy{})
	table := database.Table(&exampleRow{})
	other := &otherExampleRow{}
	var rows []exampleRow
	var otherRows []otherExampleRow

	require.Error(t, table.Get(t.Context(), other))
	require.Error(t, table.List(t.Context(), &otherRows))
	require.Error(t, table.Query(t.Context(), other, &rows))
	require.Error(t, table.Filter(t.Context(), func(*otherExampleRow) bool { return true }, &rows))
	require.Error(t, table.FilterByUUIDs(t.Context(), func(*exampleRow) bool { return true }, &otherRows, "uuid"))
	_, err := table.CreateOps(other)
	require.Error(t, err)
	_, err = table.UpdateOps(other, other)
	require.Error(t, err)
	_, err = table.MutateOps(other)
	require.Error(t, err)
	_, err = table.DeleteOps(other)
	require.Error(t, err)
	require.Error(t, table.Create(t.Context(), "create", other))
	require.Error(t, table.Update(t.Context(), "update", other, other))
	require.Error(t, table.Mutate(t.Context(), "mutate", other))
	require.Error(t, table.Delete(t.Context(), "delete", other))
	require.Error(t, table.DeleteFilter(t.Context(), "delete-filter", func(*otherExampleRow) bool { return true }))
	_, err = table.Where(other).Delete()
	require.Error(t, err)
	_, err = table.WhereCache(func(*otherExampleRow) bool { return true }).Delete()
	require.Error(t, err)
	_, err = table.Where(&exampleRow{}).Update(other)
	require.Error(t, err)
	_, err = table.WhereCache(func(*exampleRow) bool { return true }).Update(other)
	require.Error(t, err)
	require.Error(t, table.Where(&exampleRow{}).List(t.Context(), &otherRows))
}

func TestTableAcceptsPointerAndValueSlices(t *testing.T) {
	database := NewDatabase(&fakeBackend{conditional: crudConditional{}}, time.Second, RetryPolicy{})
	table := database.Table(&exampleRow{})

	require.NoError(t, table.List(t.Context(), &[]exampleRow{}))
	require.NoError(t, table.List(t.Context(), &[]*exampleRow{}))
}

func TestGenericTableHelpers(t *testing.T) {
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
	prototype := &exampleRow{}
	ctx := context.Background()

	_, err := List[exampleRow](ctx, database, prototype)
	require.NoError(t, err)
	_, err = Query[exampleRow](ctx, database, prototype, &exampleRow{Name: "key"})
	require.NoError(t, err)
	_, err = Filter[exampleRow](ctx, database, prototype, func(*exampleRow) bool { return true })
	require.NoError(t, err)
	_, err = FilterByUUIDs[exampleRow](ctx, database, prototype, func(*exampleRow) bool { return true }, "uuid")
	require.NoError(t, err)
	require.NoError(t, Get(ctx, database, prototype, &exampleRow{Name: "key"}))
	require.NoError(t, Create(ctx, database, prototype, "create", &exampleRow{Name: "new"}))
	require.NoError(t, Update(ctx, database, prototype, "update", &exampleRow{Name: "key"}, &exampleRow{Name: "updated"}, "Name"))
	require.NoError(t, Mutate(ctx, database, prototype, "mutate", &exampleRow{Name: "key"}))
	require.NoError(t, Delete(ctx, database, prototype, "delete", &exampleRow{Name: "key"}))
	require.NoError(t, DeleteFilter(ctx, database, prototype, "delete-filter", func(*exampleRow) bool { return true }))
	require.NoError(t, Transact(ctx, database, prototype, "transact", tableOperation()...))
	require.Equal(t, 6, fake.transacts)
}

func TestGenericTableHelpersRejectNilProvider(t *testing.T) {
	_, err := List[exampleRow](context.Background(), nil, &exampleRow{})
	require.EqualError(t, err, "ovsdb table provider is nil")
}

func TestWaitForRowsWaitsForCachePropagation(t *testing.T) {
	handle := &waitRowsHandle{}
	provider := waitRowsProvider{handle: handle}
	var rows []exampleRow

	require.NoError(t, WaitForRows(t.Context(), provider, &exampleRow{}, func(*exampleRow) bool {
		return true
	}, &rows))
	require.Len(t, rows, 1)
	require.Equal(t, 3, handle.calls)
}

type waitRowsProvider struct {
	handle TableHandle
}

func (p waitRowsProvider) Table(model.Model) TableHandle {
	return p.handle
}

type waitRowsHandle struct {
	TableHandle
	calls int
}

func (h *waitRowsHandle) Filter(_ context.Context, _, result any) error {
	h.calls++
	rows := result.(*[]exampleRow)
	if h.calls == 3 {
		*rows = []exampleRow{{Name: "ready"}}
	}
	return nil
}

func TestUnique(t *testing.T) {
	t.Parallel()

	notFound := errors.New("missing")
	duplicated := errors.New("dup")
	row, err := Unique([]exampleRow{{Name: "one"}}, false, notFound, duplicated)
	require.NoError(t, err)
	require.Equal(t, "one", row.Name)

	row, err = Unique([]exampleRow{}, true, notFound, duplicated)
	require.NoError(t, err)
	require.Nil(t, row)

	_, err = Unique([]exampleRow{}, false, notFound, duplicated)
	require.ErrorIs(t, err, notFound)

	_, err = Unique([]exampleRow{{Name: "a"}, {Name: "b"}}, false, notFound, duplicated)
	require.ErrorIs(t, err, duplicated)
}

func TestUniqueByName(t *testing.T) {
	t.Parallel()

	row, err := UniqueByName([]exampleRow{{Name: "one"}}, "one", "example", false)
	require.NoError(t, err)
	require.Equal(t, "one", row.Name)

	row, err = UniqueByName([]exampleRow{}, "missing", "example", true)
	require.NoError(t, err)
	require.Nil(t, row)

	_, err = UniqueByName([]exampleRow{}, "missing", "example", false)
	require.EqualError(t, err, `not found example "missing"`)

	_, err = UniqueByName([]exampleRow{{Name: "dup"}, {Name: "dup"}}, "dup", "example", false)
	require.EqualError(t, err, `more than one example with same name "dup"`)
}

type getByNameHandle struct {
	TableHandle
	rows []exampleRow
}

func (h getByNameHandle) Filter(_ context.Context, predicate, result any) error {
	match := predicate.(func(*exampleRow) bool)
	out := result.(*[]exampleRow)
	for i := range h.rows {
		if match(&h.rows[i]) {
			*out = append(*out, h.rows[i])
		}
	}
	return nil
}

type getByNameProvider struct {
	handle TableHandle
}

func (p getByNameProvider) Table(model.Model) TableHandle {
	return p.handle
}

func TestGetByName(t *testing.T) {
	t.Parallel()

	provider := getByNameProvider{handle: getByNameHandle{rows: []exampleRow{{Name: "keep"}, {Name: "other"}}}}
	row, err := GetByName(t.Context(), provider, &exampleRow{}, "keep", "example", false, func(row *exampleRow) string {
		return row.Name
	})
	require.NoError(t, err)
	require.Equal(t, "keep", row.Name)
}
