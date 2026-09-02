package compat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"
)

type fakeBackend struct {
	transact  func(context.Context, ...ovsdb.Operation) ([]ovsdb.OperationResult, error)
	transacts int
}

func (f *fakeBackend) Get(context.Context, model.Model) error { return nil }

func (f *fakeBackend) List(context.Context, any) error { return nil }

func (f *fakeBackend) WhereCache(any) client.ConditionalAPI { return nil }

func (f *fakeBackend) Where(...model.Model) client.ConditionalAPI { return nil }

func (f *fakeBackend) Create(...model.Model) ([]ovsdb.Operation, error) { return nil, nil }

func (f *fakeBackend) Transact(ctx context.Context, operations ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	f.transacts++
	return f.transact(ctx, operations...)
}

func TestTransactRetriesDisconnectedBackend(t *testing.T) {
	disconnected := client.ErrNotConnected
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

func TestTransactStopsWhenContextExpires(t *testing.T) {
	fake := &fakeBackend{transact: func(context.Context, ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
		return nil, client.ErrNotConnected
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

var _ model.Model = (*struct{})(nil)
