package ovs

import (
	"testing"

	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (suite *OvnClientTestSuite) testGetLogicalSwitch() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	name := "test-get-ls"

	err := ovnClient.CreateBareLogicalSwitch(name)
	require.NoError(t, err)

	t.Run("should return no err when found logical switch", func(t *testing.T) {
		lr, err := ovnClient.GetLogicalSwitch(name, false)
		require.NoError(t, err)
		require.Equal(t, name, lr.Name)
		require.NotEmpty(t, lr.UUID)
	})

	t.Run("should return err when not found logical switch", func(t *testing.T) {
		_, err := ovnClient.GetLogicalSwitch("test-get-lr-non-existent", false)
		require.ErrorContains(t, err, "not found logical switch")
	})

	t.Run("no err when not found logical switch and ignoreNotFound is true", func(t *testing.T) {
		_, err := ovnClient.GetLogicalSwitch("test-get-lr-non-existent", true)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testLogicalSwitchOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-port-op-ls"
	lspName := "test-port-op-lsp"

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	lsp := &ovnnb.LogicalSwitchPort{
		UUID: ovsclient.UUID(),
		Name: lspName,
	}

	t.Run("add new port to logical switch", func(t *testing.T) {
		t.Parallel()
		ops, err := ovnClient.LogicalSwitchOp(lsName, lsp, true)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "ports",
				Mutator: ovsdb.MutateOperationInsert,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: lsp.UUID,
						},
					},
				},
			},
		}, ops[0].Mutations)
	})

	t.Run("del port from logical switch", func(t *testing.T) {
		t.Parallel()
		ops, err := ovnClient.LogicalSwitchOp(lsName, lsp, false)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "ports",
				Mutator: ovsdb.MutateOperationDelete,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: lsp.UUID,
						},
					},
				},
			},
		}, ops[0].Mutations)
	})

	t.Run("should return err when logical router does not exist", func(t *testing.T) {
		t.Parallel()
		_, err := ovnClient.LogicalSwitchOp("test-port-op-ls-non-existent", lsp, true)
		require.ErrorContains(t, err, "not found logical switch")
	})
}
