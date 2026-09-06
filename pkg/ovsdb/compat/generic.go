package compat

import (
	"context"
	"errors"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
)

// tableFor resolves a provider once for package-level helpers. Keeping this
// validation in one place gives callers the same error behavior as Table.
func tableFor(provider TableProvider, prototype model.Model) (TableHandle, error) {
	if provider == nil {
		return nil, errors.New("ovsdb table provider is nil")
	}
	if prototype == nil {
		return nil, errors.New("ovsdb table prototype is nil")
	}
	table := provider.Table(prototype)
	if table == nil {
		return nil, errors.New("ovsdb table handle is nil")
	}
	return table, nil
}

// List returns all monitored rows for prototype. T is the non-pointer row
// type, matching the []Row convention used by generated OVN models.
func List[T any](ctx context.Context, provider TableProvider, prototype model.Model) ([]T, error) {
	table, err := tableFor(provider, prototype)
	if err != nil {
		return nil, err
	}
	rows := []T{}
	if err := table.List(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// Query returns rows selected by an indexed model.
func Query[T any](ctx context.Context, provider TableProvider, prototype, selector model.Model) ([]T, error) {
	table, err := tableFor(provider, prototype)
	if err != nil {
		return nil, err
	}
	rows := []T{}
	if err := table.Query(ctx, selector, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// Filter returns monitored rows matching predicate.
func Filter[T any](ctx context.Context, provider TableProvider, prototype model.Model, predicate any) ([]T, error) {
	table, err := tableFor(provider, prototype)
	if err != nil {
		return nil, err
	}
	rows := []T{}
	if err := table.Filter(ctx, predicate, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// FilterByUUIDs returns monitored rows matching predicate and UUIDs.
func FilterByUUIDs[T any](ctx context.Context, provider TableProvider, prototype model.Model, predicate any, uuids ...string) ([]T, error) {
	table, err := tableFor(provider, prototype)
	if err != nil {
		return nil, err
	}
	rows := []T{}
	if err := table.FilterByUUIDs(ctx, predicate, &rows, uuids...); err != nil {
		return nil, err
	}
	return rows, nil
}

// Get reads the row identified by result. The result must be a pointer to the
// generated row type; the prototype selects the table and indexed schema.
func Get(ctx context.Context, provider TableProvider, prototype, result model.Model) error {
	table, err := tableFor(provider, prototype)
	if err != nil {
		return err
	}
	return table.Get(ctx, result)
}

// Create inserts rows and submits one transaction.
func Create(ctx context.Context, provider TableProvider, prototype model.Model, method string, rows ...model.Model) error {
	table, err := tableFor(provider, prototype)
	if err != nil {
		return err
	}
	return table.Create(ctx, method, rows...)
}

// Update changes selected fields on one row and submits one transaction.
func Update(ctx context.Context, provider TableProvider, prototype model.Model, method string, selector, update model.Model, fields ...any) error {
	table, err := tableFor(provider, prototype)
	if err != nil {
		return err
	}
	return table.Update(ctx, method, selector, update, fields...)
}

// Mutate applies mutations to one row and submits one transaction.
func Mutate(ctx context.Context, provider TableProvider, prototype model.Model, method string, selector model.Model, mutations ...model.Mutation) error {
	table, err := tableFor(provider, prototype)
	if err != nil {
		return err
	}
	return table.Mutate(ctx, method, selector, mutations...)
}

// Delete deletes rows selected by their indexed models.
func Delete(ctx context.Context, provider TableProvider, prototype model.Model, method string, selectors ...model.Model) error {
	table, err := tableFor(provider, prototype)
	if err != nil {
		return err
	}
	return table.Delete(ctx, method, selectors...)
}

// DeleteFilter deletes all monitored rows matching predicate.
func DeleteFilter(ctx context.Context, provider TableProvider, prototype model.Model, method string, predicate any) error {
	table, err := tableFor(provider, prototype)
	if err != nil {
		return err
	}
	return table.DeleteFilter(ctx, method, predicate)
}

// Transact submits pre-built operations through the selected table policy.
func Transact(ctx context.Context, provider TableProvider, prototype model.Model, method string, operations ...ovsdb.Operation) error {
	table, err := tableFor(provider, prototype)
	if err != nil {
		return err
	}
	return table.Transact(ctx, method, operations...)
}
