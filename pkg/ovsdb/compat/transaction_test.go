package compat

import (
	"context"
	"testing"

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
