package table

import (
	"context"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"
)

func TestTxPlanPreservesOrderAndCopiesOperations(t *testing.T) {
	plan := NewTxPlan("resource-update")
	first := ovsdb.Operation{Op: ovsdb.OperationComment, Comment: new("first")}
	second := ovsdb.Operation{Op: ovsdb.OperationComment, Comment: new("second")}
	plan.Add(first)
	operations := plan.Operations()
	plan.Add(second)

	require.Equal(t, "resource-update", plan.Method())
	require.Equal(t, []ovsdb.Operation{first, second}, plan.Operations())
	operations[0] = second
	require.Equal(t, first, plan.Operations()[0])
}

func TestDatabaseExecuteSkipsEmptyPlan(t *testing.T) {
	fake := &fakeBackend{transact: func(context.Context, ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
		t.Fatal("empty plans must not call the backend")
		return nil, nil
	}}
	database := NewDatabase(fake, 0, RetryPolicy{})
	require.NoError(t, database.Execute(t.Context(), NewTxPlan("empty")))
	require.NoError(t, database.Execute(t.Context(), nil))
}

func TestDatabaseCommitReturnsResultAndObservesTransaction(t *testing.T) {
	operation := ovsdb.Operation{Op: ovsdb.OperationComment, Comment: new("ok")}
	fake := &fakeBackend{transact: func(context.Context, ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
		return []ovsdb.OperationResult{{}}, nil
	}}
	var event TransactionEvent
	database := NewDatabase(fake, time.Second, RetryPolicy{},
		WithDatabaseName("test-db"),
		WithTransactionObserver(TransactionObserverFunc(func(observed TransactionEvent) {
			event = observed
		})),
	)

	result, err := database.Commit(t.Context(), func() *TxPlan {
		plan := NewTxPlan("commit-row")
		plan.Add(operation)
		return plan
	}())
	require.NoError(t, err)
	require.Equal(t, CommitResult{Method: "commit-row", OperationCount: 1, Applied: true}, result)
	require.Equal(t, "commit-row", event.Method)
	require.Equal(t, []ovsdb.Operation{operation}, event.Operations)
}

func TestDatabaseCommitWrapsOperationError(t *testing.T) {
	operation := ovsdb.Operation{Op: ovsdb.OperationWait}
	fake := &fakeBackend{transact: func(context.Context, ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
		return []ovsdb.OperationResult{{Error: "timed out", Details: "wait expired"}}, nil
	}}
	database := NewDatabase(fake, time.Second, RetryPolicy{})
	plan := NewTxPlan("conditional-delete")
	plan.Add(operation)

	result, err := database.Commit(t.Context(), plan)
	require.Error(t, err)
	require.False(t, result.Applied)
	require.ErrorIs(t, err, ErrTransaction)
	var timedOut *ovsdb.TimedOut
	require.ErrorAs(t, err, &timedOut)
	var operationErr *OperationError
	require.ErrorAs(t, err, &operationErr)
	require.Equal(t, "conditional-delete", operationErr.Method)
	require.Equal(t, 0, operationErr.Index)
	require.Equal(t, ovsdb.OperationWait, operationErr.OperationType)
}

type legacyPlanExecutor struct {
	plan *TxPlan
}

func (e *legacyPlanExecutor) Execute(_ context.Context, plan *TxPlan) error {
	e.plan = plan
	return nil
}

type failingLegacyPlanExecutor struct {
	err error
}

func (e failingLegacyPlanExecutor) Execute(context.Context, *TxPlan) error {
	return e.err
}

func TestCommitPlanAdaptsLegacyExecutor(t *testing.T) {
	executor := &legacyPlanExecutor{}
	plan := NewTxPlan("legacy-plan")
	plan.Add(ovsdb.Operation{Op: ovsdb.OperationComment})

	result, err := CommitPlan(t.Context(), executor, plan)
	require.NoError(t, err)
	require.Equal(t, CommitResult{Method: "legacy-plan", OperationCount: 1, Applied: true}, result)
	require.Same(t, plan, executor.plan)
}

func TestCommitPlanWrapsLegacyExecutorError(t *testing.T) {
	plan := NewTxPlan("legacy-failure")
	plan.Add(ovsdb.Operation{Op: ovsdb.OperationComment})
	result, err := CommitPlan(t.Context(), failingLegacyPlanExecutor{err: context.DeadlineExceeded}, plan)
	require.Error(t, err)
	require.False(t, result.Applied)
	require.ErrorIs(t, err, ErrTransaction)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	var transactionErr *TransactionError
	require.ErrorAs(t, err, &transactionErr)
	require.Equal(t, "legacy-failure", transactionErr.Method)
	require.Equal(t, 1, transactionErr.OperationCount)
}

type cacheBarrierFunc func(context.Context, model.Model, any, any) error

func (f cacheBarrierFunc) WaitForRows(ctx context.Context, prototype model.Model, predicate, result any) error {
	return f(ctx, prototype, predicate, result)
}

func TestCommitResultWaitForCacheRequiresBarrier(t *testing.T) {
	result := CommitResult{Method: "cache-row", OperationCount: 1, Applied: true}
	row := &struct{}{}
	var rows []struct{}

	require.ErrorIs(t, result.WaitForCache(t.Context(), nil, row, func(*struct{}) bool { return true }, &rows), ErrCacheSyncUnsupported)
	require.NoError(t, result.WaitForCache(t.Context(), cacheBarrierFunc(func(_ context.Context, _ model.Model, _, _ any) error {
		return nil
	}), row, func(*struct{}) bool { return true }, &rows))
	require.ErrorIs(t, (CommitResult{Method: "empty-row"}).WaitForCache(t.Context(), cacheBarrierFunc(func(_ context.Context, _ model.Model, _, _ any) error {
		return nil
	}), row, func(*struct{}) bool { return true }, &rows), ErrCommitNotApplied)
}

func TestWaitForRowsReportsCacheStaleContext(t *testing.T) {
	fake := &fakeBackend{conditional: recordingConditional{list: func(ctx context.Context, _ any) error {
		<-ctx.Done()
		return ctx.Err()
	}}}
	database := NewDatabase(fake, 0, RetryPolicy{})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()

	var rows []struct{}
	err := database.WaitForRows(ctx, &struct{}{}, func(*struct{}) bool { return true }, &rows)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NotErrorIs(t, err, ErrCacheStale)
}

func TestWaitForRowsReportsCacheStaleCancellation(t *testing.T) {
	fake := &fakeBackend{conditional: recordingConditional{list: func(ctx context.Context, _ any) error {
		<-ctx.Done()
		return ctx.Err()
	}}}
	database := NewDatabase(fake, 0, RetryPolicy{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var rows []struct{}
	err := database.WaitForRows(ctx, &struct{}{}, func(*struct{}) bool { return true }, &rows)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, ErrCacheStale)
}

func TestTableValidationErrorsAreTyped(t *testing.T) {
	fake := &fakeBackend{}
	database := NewDatabase(fake, 0, RetryPolicy{})
	var rows []struct{}

	err := database.Table(&struct{}{}).List(t.Context(), &struct{}{})
	require.ErrorIs(t, err, ErrInvalidResult)

	err = database.Table(&struct{}{}).Filter(t.Context(), nil, &rows)
	require.ErrorIs(t, err, ErrInvalidPredicate)

	err = database.Table(&struct{}{}).Get(t.Context(), nil)
	require.ErrorIs(t, err, ErrInvalidModel)
}
