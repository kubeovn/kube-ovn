package ovs

import (
	"testing"

	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (suite *OvnClientTestSuite) testCreateLogicalSwitchPortOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lspName := "test-create-op-lsp"
	lsName := "test-create-op-ls"

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	t.Run("merget ExternalIDs when exist ExternalIDs", func(t *testing.T) {
		lsp := &ovnnb.LogicalSwitchPort{
			UUID: ovsclient.UUID(),
			Name: lspName,
			ExternalIDs: map[string]string{
				"pod": lspName,
			},
		}

		ops, err := ovnClient.CreateLogicalSwitchPortOp(lsp, lsName)
		require.NoError(t, err)
		require.Len(t, ops, 2)
		require.Equal(t, ovsdb.OvsMap{
			GoMap: map[interface{}]interface{}{
				logicalSwitchKey: lsName,
				"pod":            lspName,
				"vendor":         "kube-ovn",
			},
		}, ops[0].Row["external_ids"])

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
		}, ops[1].Mutations)
	})

	t.Run("attach ExternalIDs when does't exist ExternalIDs", func(t *testing.T) {
		lspName := "test-create-op-lsp-none-exid"

		lsp := &ovnnb.LogicalSwitchPort{
			UUID: ovsclient.UUID(),
			Name: lspName,
		}

		ops, err := ovnClient.CreateLogicalSwitchPortOp(lsp, lsName)
		require.NoError(t, err)
		require.Len(t, ops, 2)
		require.Equal(t, ovsdb.OvsMap{
			GoMap: map[interface{}]interface{}{
				logicalSwitchKey: lsName,
				"vendor":         "kube-ovn",
			},
		}, ops[0].Row["external_ids"])

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
		}, ops[1].Mutations)
	})
}

func (suite *OvnClientTestSuite) testDeleteLogicalSwitchPortOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lspName := "test-del-op-lsp"
	lsName := "test-del-op-ls"

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	lsp := &ovnnb.LogicalSwitchPort{
		UUID: ovsclient.UUID(),
		Name: lspName,
		ExternalIDs: map[string]string{
			"pod":            lspName,
			logicalSwitchKey: lsName,
		},
	}

	ops, err := ovnClient.DeleteLogicalSwitchPortOp(lsp)
	require.NoError(t, err)
	require.Len(t, ops, 2)

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

	require.Equal(t,
		ovsdb.Operation{
			Op:    "delete",
			Table: "Logical_Switch_Port",
			Where: []ovsdb.Condition{
				{
					Column:   "_uuid",
					Function: "==",
					Value: ovsdb.UUID{
						GoUUID: lsp.UUID,
					},
				},
			},
		}, ops[1])
}
