package compat

import (
	"context"
	"errors"

	"github.com/ovn-kubernetes/libovsdb/model"
)

// Table is a reconcile-oriented resource handle. It keeps table access
// independent from a concrete NB, SB, or vswitchd schema while centralizing
// cache queries, indexed queries, operation construction, and transactions.
type Table struct {
	db        *Database
	prototype model.Model
}

// TableProvider supplies generic table handles for a database schema.
// Database implements this interface, and database-specific clients can
// expose it through embedding without expanding their legacy client APIs.
type TableProvider interface {
	Table(model.Model) *Table
}

var _ TableProvider = (*Database)(nil)

// Table returns a resource handle for a model table. The prototype is used by
// Query when the caller omits an indexed selector and also documents the row
// type owned by the handle.
// A reconcile loop can keep its database code at this level:
//
//	table := database.Table(&rowModel{})
//	var rows []rowModel
//	err := table.Filter(ctx, predicate, &rows)
//	err = table.Update(ctx, "resource-update", selector, desired, &desired.Field)
func (d *Database) Table(prototype model.Model) *Table {
	return &Table{db: d, prototype: prototype}
}

func (t *Table) ensure() error {
	if t == nil || t.db == nil || t.db.Client == nil {
		return errors.New("ovsdb table is nil")
	}
	if t.prototype == nil {
		return errors.New("ovsdb table prototype is nil")
	}
	return nil
}

func (t *Table) ensureModel(row model.Model) error {
	if err := t.ensure(); err != nil {
		return err
	}
	if row == nil {
		return errors.New("ovsdb table model is nil")
	}
	return nil
}

// Get reads one row identified by the model's UUID or configured index.
func (t *Table) Get(ctx context.Context, result model.Model) error {
	if err := t.ensureModel(result); err != nil {
		return err
	}
	return t.db.Get(ctx, result)
}

// List reads all monitored rows for the table model.
func (t *Table) List(ctx context.Context, result any) error {
	if err := t.ensure(); err != nil {
		return err
	}
	return t.db.List(ctx, result)
}

// Query reads rows selected by an indexed model. If selector is nil, the
// table prototype is used, which is useful for tables with a zero-value index.
func (t *Table) Query(ctx context.Context, selector model.Model, result any) error {
	if err := t.ensure(); err != nil {
		return err
	}
	if selector == nil {
		selector = t.prototype
	}
	return t.db.Where(selector).List(ctx, result)
}

// Filter reads rows selected by a predicate evaluated against the monitored
// cache.
func (t *Table) Filter(ctx context.Context, predicate, result any) error {
	if err := t.ensure(); err != nil {
		return err
	}
	return t.db.WhereCache(predicate).List(ctx, result)
}

// FilterByUUIDs reads cache rows selected by a predicate and UUID allowlist.
func (t *Table) FilterByUUIDs(ctx context.Context, predicate, result any, uuids ...string) error {
	if err := t.ensure(); err != nil {
		return err
	}
	return t.db.WhereCacheByUUIDs(predicate, uuids...).List(ctx, result)
}

// Create inserts rows and submits the transaction with the supplied method
// name for metrics and tracing.
func (t *Table) Create(ctx context.Context, method string, rows ...model.Model) error {
	if err := t.ensure(); err != nil {
		return err
	}
	for _, row := range rows {
		if err := t.ensureModel(row); err != nil {
			return err
		}
	}
	return t.db.CreateAndTransact(ctx, method, rows...)
}

// Update updates fields on rows selected by an indexed model.
func (t *Table) Update(ctx context.Context, method string, selector, update model.Model, fields ...any) error {
	if err := t.ensureModel(selector); err != nil {
		return err
	}
	if err := t.ensureModel(update); err != nil {
		return err
	}
	return t.db.UpdateAndTransact(ctx, method, selector, update, fields...)
}

// Mutate applies OVSDB mutations to rows selected by an indexed model.
func (t *Table) Mutate(ctx context.Context, method string, selector model.Model, mutations ...model.Mutation) error {
	if err := t.ensureModel(selector); err != nil {
		return err
	}
	return t.db.MutateAndTransact(ctx, method, selector, mutations...)
}

// Delete deletes rows selected by one or more indexed models.
func (t *Table) Delete(ctx context.Context, method string, selectors ...model.Model) error {
	if err := t.ensure(); err != nil {
		return err
	}
	for _, selector := range selectors {
		if err := t.ensureModel(selector); err != nil {
			return err
		}
	}
	return t.db.DeleteAndTransact(ctx, method, selectors...)
}

// DeleteFilter deletes rows selected by a cache predicate.
func (t *Table) DeleteFilter(ctx context.Context, method string, predicate any) error {
	if err := t.ensure(); err != nil {
		return err
	}
	return t.db.DeleteWhereCacheAndTransact(ctx, method, predicate)
}
