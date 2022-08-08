package ovs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (suite *OvnClientTestSuite) testGetLogicalRouter() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	name := "test-get-lr"

	err := ovnClient.CreateLogicalRouter(name)
	require.NoError(t, err)

	t.Run("should return no err when found logical router", func(t *testing.T) {
		t.Parallel()
		lr, err := ovnClient.GetLogicalRouter(name, false)
		require.NoError(t, err)
		require.Equal(t, name, lr.Name)
		require.NotEmpty(t, lr.UUID)
	})

	t.Run("should return err when not found logical router", func(t *testing.T) {
		t.Parallel()
		_, err := ovnClient.GetLogicalRouter("test-get-lr-non-existent", false)
		require.ErrorContains(t, err, "not found logical router")
	})

	t.Run("no err when not found logical router and ignoreNotFound is true", func(t *testing.T) {
		t.Parallel()
		_, err := ovnClient.GetLogicalRouter("test-get-lr-non-existent", true)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testCreateLogicalRouter() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	name := "test-create-lr"

	err := ovnClient.CreateLogicalRouter(name)
	require.NoError(t, err)

	lr, err := ovnClient.GetLogicalRouter(name, false)
	require.NoError(t, err)
	require.Equal(t, name, lr.Name)
	require.NotEmpty(t, lr.UUID)
	require.Equal(t, util.CniTypeName, lr.ExternalIDs["vendor"])
}

func (suite *OvnClientTestSuite) testDeleteLogicalRouter() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	name := "test-delete-lr"

	t.Run("no err when delete existent logical router", func(t *testing.T) {
		t.Parallel()
		err := ovnClient.CreateLogicalRouter(name)
		require.NoError(t, err)

		err = ovnClient.DeleteLogicalRouter(name)
		require.NoError(t, err)

		_, err = ovnClient.GetLogicalRouter(name, false)
		require.ErrorContains(t, err, "not found logical router")
	})

	t.Run("no err when delete non-existent logical router", func(t *testing.T) {
		t.Parallel()
		err := ovnClient.DeleteLogicalRouter("test-delete-lr-non-existent")
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testListLogicalRouter() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	namePrefix := "test-list-lr"

	names := make([]string, 3)
	for i := 0; i < 3; i++ {
		names[i] = fmt.Sprintf("%s-%d", namePrefix, i)
		err := ovnClient.CreateLogicalRouter(names[i])
		require.NoError(t, err)
	}

	t.Run("return all logical router which match vendor", func(t *testing.T) {
		t.Parallel()
		lrs, err := ovnClient.ListLogicalRouter(true)
		require.NoError(t, err)
		require.NotEmpty(t, lrs)

		for _, lr := range lrs {
			if !strings.Contains(lr.Name, namePrefix) {
				continue
			}
			require.Contains(t, names, lr.Name)
		}
	})
}

func (suite *OvnClientTestSuite) testLogicalRouterUpdatePortOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-port-op-lr"
	uuid := ovsclient.UUID()

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("add new port to logical router", func(t *testing.T) {
		t.Parallel()
		ops, err := ovnClient.LogicalRouterUpdatePortOp(lrName, uuid, ovsdb.MutateOperationInsert)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "ports",
				Mutator: ovsdb.MutateOperationInsert,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: uuid,
						},
					},
				},
			},
		}, ops[0].Mutations)
	})

	t.Run("del port from logical router", func(t *testing.T) {
		t.Parallel()
		ops, err := ovnClient.LogicalRouterUpdatePortOp(lrName, uuid, ovsdb.MutateOperationDelete)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "ports",
				Mutator: ovsdb.MutateOperationDelete,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: uuid,
						},
					},
				},
			},
		}, ops[0].Mutations)
	})

	t.Run("should return err when uuid is empty", func(t *testing.T) {
		t.Parallel()
		_, err := ovnClient.LogicalRouterUpdatePortOp(lrName, "", ovsdb.MutateOperationInsert)
		require.ErrorContains(t, err, fmt.Sprintf("the uuid of port add or del to logical router %s cannot be empty", lrName))
	})

	t.Run("should return err when logical router does not exist", func(t *testing.T) {
		t.Parallel()
		_, err := ovnClient.LogicalRouterUpdatePortOp("test-port-op-lr-non-existent", uuid, ovsdb.MutateOperationInsert)
		require.ErrorContains(t, err, "not found logical router")
	})

}
