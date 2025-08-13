package ovs

import (
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
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

func (suite *OvnClientTestSuite) testNewOvnNbClient() {
	t := suite.T()

	ovnNbTimeout := 5
	ovsDbConTimeout := 10
	ovsDbInactivityTimeout := 20

	clientSchema := ovnnb.Schema()
	clientDBModel, err := ovnnb.FullDatabaseModel()
	require.NoError(suite.T(), err)

	_, sock := newOVSDBServer(suite.T(), "test-nb-client", clientDBModel, clientSchema)
	endpoint := "unix:" + sock
	require.FileExists(suite.T(), sock)

	t.Run("successful client creation", func(t *testing.T) {
		client, err := NewOvnNbClient(endpoint, ovnNbTimeout, ovsDbConTimeout, ovsDbInactivityTimeout, 1)
		require.NoError(t, err)
		require.NotNil(t, client)
		require.Equal(t, time.Duration(ovnNbTimeout)*time.Second, client.Timeout)
	})

	t.Run("ovsdb client error with max retry", func(t *testing.T) {
		client, err := NewOvnNbClient("invalid addr", 5, 10, 20, 1)
		require.Error(t, err)
		require.Nil(t, client)
	})
}

func (suite *OvnClientTestSuite) testNewOvnSbClient() {
	t := suite.T()

	ovnSbTimeout := 5
	ovsDbConTimeout := 10
	ovsDbInactivityTimeout := 20

	clientSchema := ovnsb.Schema()
	clientDBModel, err := ovnsb.FullDatabaseModel()
	require.NoError(suite.T(), err)

	_, sock := newOVSDBServer(suite.T(), "test-sb-client", clientDBModel, clientSchema)
	endpoint := "unix:" + sock
	require.FileExists(suite.T(), sock)

	t.Run("successful client creation", func(t *testing.T) {
		client, err := NewOvnSbClient(endpoint, ovnSbTimeout, ovsDbConTimeout, ovsDbInactivityTimeout, 1)
		require.NoError(t, err)
		require.NotNil(t, client)
		require.Equal(t, time.Duration(ovnSbTimeout)*time.Second, client.Timeout)
	})

	t.Run("ovsdb client error with max retry", func(t *testing.T) {
		client, err := NewOvnSbClient("invalid addr", 5, 10, 20, 1)
		require.Error(t, err)
		require.Nil(t, client)
	})
}
