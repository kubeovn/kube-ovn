package table

import (
	"context"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"
)

func TestCreateIfAbsentByName(t *testing.T) {
	for _, tc := range []struct {
		name           string
		cached         int
		operationError string
		transportError error
		wantError      bool
	}{
		{name: "insert"},
		{name: "existing", cached: 1},
		{name: "duplicates", cached: 2},
		{name: "concurrent creator", operationError: "timed out"},
		{name: "constraint failure", operationError: "constraint violation", wantError: true},
		{name: "transport timeout", transportError: context.DeadlineExceeded, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fakeBackend{
				conditional: recordingConditional{list: func(_ context.Context, result any) error {
					rows := result.(*[]exampleRow)
					for range tc.cached {
						*rows = append(*rows, exampleRow{Name: "root"})
					}
					return nil
				}},
				create: func(...model.Model) ([]ovsdb.Operation, error) {
					return []ovsdb.Operation{{Op: ovsdb.OperationInsert, Table: "Root", Row: ovsdb.Row{"name": "root"}}}, nil
				},
				transact: func(_ context.Context, ops ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
					require.Len(t, ops, 2)
					require.Equal(t, ovsdb.OperationWait, ops[0].Op)
					require.Equal(t, "Root", ops[0].Table)
					require.Equal(t, new(0), ops[0].Timeout)
					require.Equal(t, []ovsdb.Condition{{Column: "name", Function: ovsdb.ConditionEqual, Value: "root"}}, ops[0].Where)
					require.Equal(t, []string{"name"}, ops[0].Columns)
					require.Equal(t, string(ovsdb.WaitConditionNotEqual), ops[0].Until)
					require.Equal(t, []ovsdb.Row{{"name": "root"}}, ops[0].Rows)
					require.Equal(t, ovsdb.OperationInsert, ops[1].Op)
					return []ovsdb.OperationResult{{Error: tc.operationError}, {}}, tc.transportError
				},
			}
			database := NewDatabase(backend, time.Second, RetryPolicy{})
			err := CreateIfAbsentByName(t.Context(), database, &exampleRow{Name: "root"}, "Root", "root", "root-add", func(row *exampleRow) string { return row.Name })
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.cached > 0 {
				require.Zero(t, backend.transacts)
			} else {
				require.Equal(t, 1, backend.transacts)
			}
		})
	}
}

func TestTransactConditionalWaitFailure(t *testing.T) {
	backend := &fakeBackend{transact: func(context.Context, ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
		return []ovsdb.OperationResult{{Error: "timed out"}}, nil
	}}
	database := NewDatabase(backend, time.Second, RetryPolicy{})
	ops := []ovsdb.Operation{{Op: ovsdb.OperationWait, Table: "Root"}}
	created, err := database.TransactConditional("root-add", ops)
	require.NoError(t, err)
	require.False(t, created)
	require.Error(t, database.Transact("strict-wait", ops))

	backend.transact = func(context.Context, ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
		return nil, context.DeadlineExceeded
	}
	created, err = database.TransactConditional("root-add", ops)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, created)
}
