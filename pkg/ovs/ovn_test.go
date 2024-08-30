package ovs

import (
	"testing"

	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"
)

func TestNewLegacyClient(t *testing.T) {
	timeout := 30
	client := NewLegacyClient(timeout)
	require.NotNil(t, client)
	require.Equal(t, timeout, client.OvnTimeout)
}

func TestConstructWaitForNameNotExistsOperation(t *testing.T) {
	name := "test-name"
	table := "test-table"
	op := ConstructWaitForNameNotExistsOperation(name, table)

	require.Equal(t, ovsdb.OperationWait, op.Op)
	require.Equal(t, table, op.Table)
	require.Equal(t, OVSDBWaitTimeout, *op.Timeout)
	require.Equal(t, []ovsdb.Condition{{Column: "name", Function: ovsdb.ConditionEqual, Value: name}}, op.Where)
	require.Equal(t, []string{"name"}, op.Columns)
	require.Equal(t, string(ovsdb.WaitConditionNotEqual), op.Until)
	require.Equal(t, []ovsdb.Row{{"name": name}}, op.Rows)
}

func TestConstructWaitForUniqueOperation(t *testing.T) {
	table := "test-table"
	column := "test-column"
	value := "test-value"
	op := ConstructWaitForUniqueOperation(table, column, value)

	require.Equal(t, ovsdb.OperationWait, op.Op)
	require.Equal(t, table, op.Table)
	require.Equal(t, OVSDBWaitTimeout, *op.Timeout)
	require.Equal(t, []ovsdb.Condition{{Column: column, Function: ovsdb.ConditionEqual, Value: value}}, op.Where)
	require.Equal(t, []string{column}, op.Columns)
	require.Equal(t, string(ovsdb.WaitConditionNotEqual), op.Until)
	require.Equal(t, []ovsdb.Row{{column: value}}, op.Rows)
}
