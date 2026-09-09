package table

import (
	"context"
	"errors"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
)

// TxPlan is an immutable-at-read transaction plan. Callers append operations
// while composing a resource change, then hand the plan to an Executor.
type TxPlan struct {
	method     string
	operations []ovsdb.Operation
}

// NewTxPlan creates an empty transaction plan with a metric/trace label.
func NewTxPlan(method string) *TxPlan {
	return &TxPlan{method: method}
}

// Add appends operations in the order in which they must be applied.
func (p *TxPlan) Add(operations ...ovsdb.Operation) {
	if p == nil || len(operations) == 0 {
		return
	}
	p.operations = append(p.operations, operations...)
}

// Method returns the transaction label.
func (p *TxPlan) Method() string {
	if p == nil {
		return ""
	}
	return p.method
}

// Operations returns a copy so the executor owns submission ordering.
func (p *TxPlan) Operations() []ovsdb.Operation {
	if p == nil {
		return nil
	}
	return append([]ovsdb.Operation(nil), p.operations...)
}

// Empty reports whether the plan has no operations.
func (p *TxPlan) Empty() bool {
	return p == nil || len(p.operations) == 0
}

// Executor submits a plan while preserving database transaction policy.
type Executor interface {
	Execute(context.Context, *TxPlan) error
}

var _ Executor = (*Database)(nil)

// Execute submits a plan through the database timeout, retry, validation, and
// transaction observer path.
func (d *Database) Execute(ctx context.Context, plan *TxPlan) error {
	if plan == nil || plan.Empty() {
		return nil
	}
	if d == nil || d.Client == nil {
		return errors.New("ovsdb database is nil")
	}
	_, err := d.transact(ctx, plan.Method(), plan.operations...)
	return err
}
