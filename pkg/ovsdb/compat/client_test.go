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

type fakeBackend struct {
	transact    func(context.Context, ...ovsdb.Operation) ([]ovsdb.OperationResult, error)
	conditional ConditionalAPI
	cache       Cache
	transacts   int
}

func (f *fakeBackend) Get(context.Context, model.Model) error { return nil }

func (f *fakeBackend) List(context.Context, any) error { return nil }

func (f *fakeBackend) WhereCache(any) ConditionalAPI { return f.conditional }

func (f *fakeBackend) WhereCacheByUUIDs(any, ...string) ConditionalAPI { return f.conditional }

func (f *fakeBackend) Where(...model.Model) ConditionalAPI { return f.conditional }

func (f *fakeBackend) WhereAny(model.Model, ...model.Condition) ConditionalAPI { return f.conditional }

func (f *fakeBackend) WhereAll(model.Model, ...model.Condition) ConditionalAPI { return f.conditional }

func (f *fakeBackend) Select(model.Model, ...any) ([]ovsdb.Operation, error) { return nil, nil }

func (f *fakeBackend) Create(...model.Model) ([]ovsdb.Operation, error) { return nil, nil }

func (f *fakeBackend) Cache() Cache { return f.cache }

func (f *fakeBackend) Schema() ovsdb.DatabaseSchema { return ovsdb.DatabaseSchema{} }

func (f *fakeBackend) Connected() bool { return true }

func (f *fakeBackend) NewMonitor(...MonitorOption) *Monitor { return nil }

func (f *fakeBackend) Monitor(context.Context, *Monitor) (MonitorCookie, error) {
	return MonitorCookie{}, nil
}

func (f *fakeBackend) Echo(context.Context) error { return nil }

func (f *fakeBackend) Close() {}

func (f *fakeBackend) Transact(ctx context.Context, operations ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	f.transacts++
	return f.transact(ctx, operations...)
}

type recordingConditional struct {
	list func(context.Context, any) error
}

func (r recordingConditional) List(ctx context.Context, result any) error {
	return r.list(ctx, result)
}

func (recordingConditional) Mutate(model.Model, ...model.Mutation) ([]ovsdb.Operation, error) {
	return nil, nil
}

func (recordingConditional) Update(model.Model, ...any) ([]ovsdb.Operation, error) {
	return nil, nil
}

func (recordingConditional) Delete() ([]ovsdb.Operation, error) { return nil, nil }

func (recordingConditional) Wait(ovsdb.WaitCondition, *int, model.Model, ...any) ([]ovsdb.Operation, error) {
	return nil, nil
}

func (recordingConditional) Select(model.Model, ...any) ([]ovsdb.Operation, error) {
	return nil, nil
}

func TestSelectorListUsesCallTimeout(t *testing.T) {
	called := make(chan struct{})
	fake := &fakeBackend{
		conditional: recordingConditional{list: func(ctx context.Context, _ any) error {
			defer close(called)
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			require.LessOrEqual(t, time.Until(deadline), 50*time.Millisecond)
			<-ctx.Done()
			return ctx.Err()
		}},
	}
	callLayer := New(fake, 5*time.Millisecond, RetryPolicy{})
	err := callLayer.WhereCache(func(*struct{}) bool { return true }).List(context.Background(), &[]struct{}{})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	<-called
}

func TestEventHandlerFuncsAdaptCallbacks(t *testing.T) {
	var events []string
	handler := &EventHandlerFuncs{
		AddFunc:    func(string, model.Model) { events = append(events, "add") },
		UpdateFunc: func(string, model.Model, model.Model) { events = append(events, "update") },
		DeleteFunc: func(string, model.Model) { events = append(events, "delete") },
	}
	row := &struct{}{}
	handler.OnAdd("table", row)
	handler.OnUpdate("table", row, row)
	handler.OnDelete("table", row)
	require.Equal(t, []string{"add", "update", "delete"}, events)
}

var (
	_ Backend        = (*rawBackend)(nil)
	_ EventHandler   = (*EventHandlerFuncs)(nil)
	_ ConditionalAPI = recordingConditional{}
)

func TestTransactRetriesDisconnectedBackend(t *testing.T) {
	disconnected := ErrNotConnected
	fake := &fakeBackend{}
	fake.transact = func(context.Context, ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
		if fake.transacts < 3 {
			return nil, disconnected
		}
		return []ovsdb.OperationResult{{}}, nil
	}

	callLayer := New(fake, time.Second, RetryPolicy{Attempts: 2, Delay: time.Millisecond})
	err := callLayer.Transact(context.Background(), "test", []ovsdb.Operation{{Op: ovsdb.OperationComment, Comment: new("ok")}})
	require.NoError(t, err)
	require.Equal(t, 3, fake.transacts)
}

func TestTransactDoesNotRetryByDefault(t *testing.T) {
	fake := &fakeBackend{transact: func(context.Context, ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
		return nil, ErrNotConnected
	}}
	callLayer := New(fake, time.Second, RetryPolicy{})
	err := callLayer.Transact(context.Background(), "test", []ovsdb.Operation{{Op: ovsdb.OperationComment, Comment: new("ok")}})
	require.ErrorIs(t, err, ErrNotConnected)
	require.Equal(t, 1, fake.transacts)
}

func TestTransactStopsWhenContextExpires(t *testing.T) {
	fake := &fakeBackend{transact: func(context.Context, ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
		return nil, ErrNotConnected
	}}
	callLayer := New(fake, time.Second, RetryPolicy{Attempts: 3, Delay: time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	err := callLayer.Transact(ctx, "test", []ovsdb.Operation{{Op: ovsdb.OperationComment, Comment: new("ok")}})
	require.Error(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestTransactSkipsEmptyOperations(t *testing.T) {
	fake := &fakeBackend{transact: func(context.Context, ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
		t.Fatal("backend should not be called")
		return nil, nil
	}}
	callLayer := New(fake, time.Second, RetryPolicy{})
	require.NoError(t, callLayer.Transact(context.Background(), "test", nil))
}

func TestTransactChecksOperationResults(t *testing.T) {
	fake := &fakeBackend{transact: func(context.Context, ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
		return []ovsdb.OperationResult{{Error: "constraint violation", Details: "duplicate"}}, nil
	}}
	callLayer := New(fake, time.Second, RetryPolicy{})

	err := callLayer.Transact(context.Background(), "test", []ovsdb.Operation{{Op: ovsdb.OperationInsert}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "operations failed")
}

func TestDatabaseObservesTransactions(t *testing.T) {
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

	require.NoError(t, database.Transact("insert-row", []ovsdb.Operation{operation}))
	require.Equal(t, "test-db", event.Database)
	require.Equal(t, "insert-row", event.Method)
	require.Equal(t, []ovsdb.Operation{operation}, event.Operations)
	require.NoError(t, event.Err)
	require.GreaterOrEqual(t, event.Duration, time.Duration(0))
}

func TestDatabaseGetEntityInfoRequiresPointer(t *testing.T) {
	fake := &fakeBackend{}
	database := NewDatabase(fake, time.Second, RetryPolicy{})

	require.Error(t, database.GetEntityInfo(struct{}{}))
	require.NoError(t, database.GetEntityInfo(&struct{}{}))
}

var _ model.Model = (*struct{})(nil)
