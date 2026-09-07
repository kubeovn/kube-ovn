package compat

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
)

// Table is a reconcile-oriented resource handle. It keeps table access
// independent from a concrete NB, SB, or vswitchd schema while centralizing
// cache queries, indexed queries, operation construction, and transactions.
type Table struct {
	db        *Database
	prototype model.Model
}

// TableHandle is the table-scoped capability exposed to reconcile code.
// Keeping the returned handle behind an interface lets tests and independent
// compatibility implementations replace table behavior without depending on
// the concrete Table implementation.
type TableHandle interface {
	Get(context.Context, model.Model) error
	List(context.Context, any) error
	Query(context.Context, model.Model, any) error
	Filter(context.Context, any, any) error
	FilterByUUIDs(context.Context, any, any, ...string) error
	Where(...model.Model) ConditionalAPI
	WhereCache(any) ConditionalAPI
	CreateOps(...model.Model) ([]ovsdb.Operation, error)
	UpdateOps(model.Model, model.Model, ...any) ([]ovsdb.Operation, error)
	MutateOps(model.Model, ...model.Mutation) ([]ovsdb.Operation, error)
	DeleteOps(...model.Model) ([]ovsdb.Operation, error)
	Create(context.Context, string, ...model.Model) error
	Update(context.Context, string, model.Model, model.Model, ...any) error
	Mutate(context.Context, string, model.Model, ...model.Mutation) error
	Delete(context.Context, string, ...model.Model) error
	DeleteFilter(context.Context, string, any) error
	Transact(context.Context, string, ...ovsdb.Operation) error
}

// TableProvider supplies generic table handles for a database schema.
// Database implements this interface, and database-specific clients can
// expose it through embedding without expanding their legacy client APIs.
type TableProvider interface {
	Table(model.Model) TableHandle
}

var (
	_ TableProvider = (*Database)(nil)
	_ TableHandle   = (*Table)(nil)
)

// Table returns a resource handle for a model table. The prototype is used by
// Query when the caller omits an indexed selector and also documents the row
// type owned by the handle.
// A reconcile loop can keep its database code at this level:
//
//	table := database.Table(&rowModel{})
//	var rows []rowModel
//	err := table.Filter(ctx, predicate, &rows)
//	err = table.Update(ctx, "resource-update", selector, desired, &desired.Field)
func (d *Database) Table(prototype model.Model) TableHandle {
	return &Table{db: d, prototype: prototype}
}

// WhereTable returns an indexed operation builder scoped to the selector's
// model. It is a convenience for callers composing multi-table transactions;
// regular reconcile code should prefer Table(prototype).Update/Mutate/Delete.
func (d *Database) WhereTable(selectors ...model.Model) ConditionalAPI {
	if len(selectors) == 0 {
		return nil
	}
	return d.Table(selectors[0]).Where(selectors...)
}

func (t *Table) ensure() error {
	if t == nil || t.db == nil || t.db.Client == nil {
		return errors.New("ovsdb table is nil")
	}
	if t.prototype == nil {
		return errors.New("ovsdb table prototype is nil")
	}
	value := reflect.ValueOf(t.prototype)
	if value.Kind() == reflect.Pointer && value.IsNil() {
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
	value := reflect.ValueOf(row)
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return errors.New("ovsdb table model is nil")
	}
	if actual, expected := reflect.TypeOf(row), reflect.TypeOf(t.prototype); actual != expected {
		return fmt.Errorf("ovsdb table model type %v does not match prototype %v", actual, expected)
	}
	return nil
}

func (t *Table) ensureResult(result any) error {
	if err := t.ensure(); err != nil {
		return err
	}
	value := reflect.ValueOf(result)
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Slice {
		return errors.New("ovsdb table result must be a non-nil pointer to a slice")
	}
	elementType := value.Elem().Type().Elem()
	prototypeType := reflect.TypeOf(t.prototype)
	if elementType == prototypeType || prototypeType.Kind() == reflect.Pointer && elementType == prototypeType.Elem() {
		return nil
	}
	return fmt.Errorf("ovsdb table result element type %v does not match prototype %v", elementType, prototypeType)
}

func (t *Table) ensurePredicate(predicate any) error {
	if err := t.ensure(); err != nil {
		return err
	}
	value := reflect.ValueOf(predicate)
	if !value.IsValid() || value.Kind() != reflect.Func || value.IsNil() {
		return errors.New("ovsdb table predicate must be a non-nil function")
	}
	typeOfPredicate := value.Type()
	if typeOfPredicate.NumIn() != 1 || typeOfPredicate.In(0) != reflect.TypeOf(t.prototype) {
		return fmt.Errorf("ovsdb table predicate model type does not match prototype %v", reflect.TypeOf(t.prototype))
	}
	if typeOfPredicate.NumOut() != 1 || typeOfPredicate.Out(0).Kind() != reflect.Bool {
		return errors.New("ovsdb table predicate must return bool")
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
	if err := t.ensureResult(result); err != nil {
		return err
	}
	return t.db.List(ctx, result)
}

// Query reads rows selected by an indexed model. If selector is nil, the
// table prototype is used, which is useful for tables with a zero-value index.
func (t *Table) Query(ctx context.Context, selector model.Model, result any) error {
	if err := t.ensureResult(result); err != nil {
		return err
	}
	if selector == nil {
		selector = t.prototype
	} else if err := t.ensureModel(selector); err != nil {
		return err
	}
	return t.db.Where(selector).List(ctx, result)
}

// Filter reads rows selected by a predicate evaluated against the monitored
// cache.
func (t *Table) Filter(ctx context.Context, predicate, result any) error {
	if err := t.ensurePredicate(predicate); err != nil {
		return err
	}
	if err := t.ensureResult(result); err != nil {
		return err
	}
	return t.db.WhereCache(predicate).List(ctx, result)
}

// FilterByUUIDs reads cache rows selected by a predicate and UUID allowlist.
func (t *Table) FilterByUUIDs(ctx context.Context, predicate, result any, uuids ...string) error {
	if err := t.ensurePredicate(predicate); err != nil {
		return err
	}
	if err := t.ensureResult(result); err != nil {
		return err
	}
	return t.db.WhereCacheByUUIDs(predicate, uuids...).List(ctx, result)
}

// Where returns an indexed operation builder scoped to this table.
// Callers can use it to compose a larger transaction while keeping the table
// selection behind the generic facade.
func (t *Table) Where(selectors ...model.Model) ConditionalAPI {
	if len(selectors) == 0 {
		return tableConditional{table: t, err: errors.New("ovsdb table selector is nil")}
	}
	for _, selector := range selectors {
		if err := t.ensureModel(selector); err != nil {
			return tableConditional{table: t, err: err}
		}
	}
	return tableConditional{table: t, backend: t.db.Where(selectors...)}
}

// WhereCache returns a cache-backed operation builder scoped to this table.
func (t *Table) WhereCache(predicate any) ConditionalAPI {
	if err := t.ensurePredicate(predicate); err != nil {
		return tableConditional{table: t, err: err}
	}
	return tableConditional{table: t, backend: t.db.WhereCache(predicate)}
}

// CreateOps builds insert operations without submitting a transaction.
func (t *Table) CreateOps(rows ...model.Model) ([]ovsdb.Operation, error) {
	if err := t.ensure(); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if err := t.ensureModel(row); err != nil {
			return nil, err
		}
	}
	return t.db.Create(rows...)
}

// UpdateOps builds update operations without submitting a transaction.
func (t *Table) UpdateOps(selector, update model.Model, fields ...any) ([]ovsdb.Operation, error) {
	if err := t.ensureModel(selector); err != nil {
		return nil, err
	}
	if err := t.ensureModel(update); err != nil {
		return nil, err
	}
	return t.Where(selector).Update(update, fields...)
}

// MutateOps builds mutation operations without submitting a transaction.
func (t *Table) MutateOps(selector model.Model, mutations ...model.Mutation) ([]ovsdb.Operation, error) {
	if err := t.ensureModel(selector); err != nil {
		return nil, err
	}
	return t.Where(selector).Mutate(selector, mutations...)
}

// DeleteOps builds delete operations without submitting a transaction.
func (t *Table) DeleteOps(selectors ...model.Model) ([]ovsdb.Operation, error) {
	if err := t.ensure(); err != nil {
		return nil, err
	}
	for _, selector := range selectors {
		if err := t.ensureModel(selector); err != nil {
			return nil, err
		}
	}
	return t.Where(selectors...).Delete()
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
	if err := t.ensurePredicate(predicate); err != nil {
		return err
	}
	return t.db.DeleteWhereCacheAndTransact(ctx, method, predicate)
}

type tableConditional struct {
	table   *Table
	backend ConditionalAPI
	err     error
}

func (c tableConditional) List(ctx context.Context, result any) error {
	if c.err != nil {
		return c.err
	}
	if err := c.table.ensureResult(result); err != nil {
		return err
	}
	return c.backend.List(ctx, result)
}

func (c tableConditional) Mutate(row model.Model, mutations ...model.Mutation) ([]ovsdb.Operation, error) {
	if c.err != nil {
		return nil, c.err
	}
	if err := c.table.ensureModel(row); err != nil {
		return nil, err
	}
	return c.backend.Mutate(row, mutations...)
}

func (c tableConditional) Update(row model.Model, fields ...any) ([]ovsdb.Operation, error) {
	if c.err != nil {
		return nil, c.err
	}
	if err := c.table.ensureModel(row); err != nil {
		return nil, err
	}
	return c.backend.Update(row, fields...)
}

func (c tableConditional) Delete() ([]ovsdb.Operation, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.backend.Delete()
}

func (c tableConditional) Wait(condition ovsdb.WaitCondition, timeout *int, row model.Model, fields ...any) ([]ovsdb.Operation, error) {
	if c.err != nil {
		return nil, c.err
	}
	if err := c.table.ensureModel(row); err != nil {
		return nil, err
	}
	return c.backend.Wait(condition, timeout, row, fields...)
}

func (c tableConditional) Select(row model.Model, fields ...any) ([]ovsdb.Operation, error) {
	if c.err != nil {
		return nil, c.err
	}
	if err := c.table.ensureModel(row); err != nil {
		return nil, err
	}
	return c.backend.Select(row, fields...)
}

// Transact submits operations built by one or more table handles as a single
// transaction. It keeps transaction policy behind the same database facade
// while allowing callers to preserve atomic multi-row updates.
func (t *Table) Transact(ctx context.Context, method string, operations ...ovsdb.Operation) error {
	if err := t.ensure(); err != nil {
		return err
	}
	_, err := t.db.transact(ctx, method, operations...)
	return err
}
