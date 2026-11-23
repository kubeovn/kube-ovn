package table

import (
	"context"
	"errors"
	"fmt"

	"github.com/ovn-kubernetes/libovsdb/model"
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

// Committer submits a plan and reports whether the server applied it. The
// result gives resource facades a small, typed seam without exposing the
// database client or transaction response details.
type Committer interface {
	Commit(context.Context, *TxPlan) (CommitResult, error)
}

// CommitResult describes one submitted transaction. Applied is false for an
// empty plan and for a transaction that returned an error.
type CommitResult struct {
	Method         string
	OperationCount int
	Applied        bool
}

// CommitPlan adapts the result-bearing committer to the legacy Executor
// capability. New facades can use the result without forcing existing test or
// integration adapters to implement Commit.
func CommitPlan(ctx context.Context, executor Executor, plan *TxPlan) (CommitResult, error) {
	result := CommitResult{}
	if plan != nil {
		result.Method = plan.Method()
		result.OperationCount = len(plan.operations)
	}
	if committer, ok := executor.(Committer); ok {
		return committer.Commit(ctx, plan)
	}
	if executor == nil {
		return result, fmt.Errorf("%w: executor is nil", ErrTransaction)
	}
	if err := executor.Execute(ctx, plan); err != nil {
		return result, wrapTransactionError(result.Method, planOperations(plan), err)
	}
	result.Applied = plan != nil && !plan.Empty()
	return result, nil
}

// WaitForCache waits for a committed resource state to become observable in
// the monitored cache. This is an observation barrier, not a transaction
// revision guarantee.
func (r CommitResult) WaitForCache(ctx context.Context, barrier CacheBarrier, prototype model.Model, predicate, result any) error {
	if barrier == nil {
		return ErrCacheSyncUnsupported
	}
	if !r.Applied {
		return fmt.Errorf("%w: transaction %q did not apply", ErrCommitNotApplied, r.Method)
	}
	if err := barrier.WaitForRows(ctx, prototype, predicate, result); err != nil {
		if errors.Is(err, ErrCacheStale) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return cacheStaleError(err)
	}
	return nil
}

func planOperations(plan *TxPlan) []ovsdb.Operation {
	if plan == nil {
		return nil
	}
	return plan.operations
}

var (
	_ Executor  = (*Database)(nil)
	_ Committer = (*Database)(nil)
)

// Commit submits a plan through the database timeout, retry, validation, and
// transaction observer path.
func (d *Database) Commit(ctx context.Context, plan *TxPlan) (CommitResult, error) {
	result := CommitResult{}
	if plan != nil {
		result.Method = plan.Method()
		result.OperationCount = len(plan.operations)
	}
	if plan == nil || plan.Empty() {
		return result, nil
	}
	if d == nil || d.Client == nil {
		return result, &TransactionError{
			Method:         result.Method,
			OperationCount: result.OperationCount,
			Err:            errors.New("ovsdb database is nil"),
		}
	}
	_, err := d.transact(ctx, plan.Method(), plan.operations...)
	if err != nil {
		return result, err
	}
	result.Applied = true
	return result, nil
}

// Execute submits a plan through the database timeout, retry, validation, and
// transaction observer path.
func (d *Database) Execute(ctx context.Context, plan *TxPlan) error {
	_, err := d.Commit(ctx, plan)
	return err
}
