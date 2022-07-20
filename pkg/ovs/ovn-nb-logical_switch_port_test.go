package ovs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func createLogicalSwitchPort(c *OvnClient, lsp *ovnnb.LogicalSwitchPort) error {
	if nil == lsp {
		return fmt.Errorf("logical_switch_port is nil")
	}

	op, err := c.Create(lsp)
	if err != nil {
		return fmt.Errorf("generate create operations for logical switch port %s: %v", lsp.Name, err)
	}

	return c.Transact("lrp-create", op)
}

func (suite *OvnClientTestSuite) testListLogicalSwitchPorts() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient

	t.Run("result should exclude lsp when vendor is not kube-ovn", func(t *testing.T) {
		lspName := "test-list-lsp-other-vendor"

		lsp := &ovnnb.LogicalSwitchPort{
			UUID:        ovsclient.UUID(),
			Name:        lspName,
			ExternalIDs: map[string]string{"vendor": util.CniTypeName},
		}

		err := createLogicalSwitchPort(ovnClient, lsp)
		require.NoError(t, err)

		out, err := ovnClient.ListLogicalSwitchPorts(true, nil)
		require.NoError(t, err)
		require.NotEmpty(t, out)
	})

	t.Run("result should exclude lsp when externalIDs's length is not equal", func(t *testing.T) {
		lspName := "test-list-lsp-mismatch-length"

		lsp := &ovnnb.LogicalSwitchPort{
			UUID: ovsclient.UUID(),
			Name: lspName,
			ExternalIDs: map[string]string{
				"vendor":          util.CniTypeName,
				"security_groups": "sg",
			},
		}

		err := createLogicalSwitchPort(ovnClient, lsp)
		require.NoError(t, err)

		out, err := ovnClient.ListLogicalSwitchPorts(true, map[string]string{"security_groups": "sg", "key": "value", "key1": "value"})
		require.NoError(t, err)
		require.Empty(t, out)
	})

	t.Run("result should include lsp when key exists in lsp column: external_ids", func(t *testing.T) {
		lspName := "test-list-lsp-exist"

		lsp := &ovnnb.LogicalSwitchPort{
			UUID: ovsclient.UUID(),
			Name: lspName,
			ExternalIDs: map[string]string{
				"vendor":          util.CniTypeName,
				"security_groups": "sg",
			},
		}

		err := createLogicalSwitchPort(ovnClient, lsp)
		require.NoError(t, err)

		out, err := ovnClient.ListLogicalSwitchPorts(true, map[string]string{"security_groups": "sg"})
		require.NoError(t, err)
		require.NotEmpty(t, out)
	})

	t.Run("result should include all lsp when externalIDs is empty", func(t *testing.T) {
		prefix := "test-list-lsp-all"

		for i := 0; i < 4; i++ {
			lspName := fmt.Sprintf("%s-%d", prefix, i)
			lsp := &ovnnb.LogicalSwitchPort{
				UUID: ovsclient.UUID(),
				Name: lspName,
				ExternalIDs: map[string]string{
					"vendor":          util.CniTypeName,
					"security_groups": "sg",
				},
			}

			err := createLogicalSwitchPort(ovnClient, lsp)
			require.NoError(t, err)
		}

		out, err := ovnClient.ListLogicalSwitchPorts(true, nil)
		require.NoError(t, err)
		count := 0
		for _, v := range out {
			if strings.Contains(v.Name, prefix) {
				count++
			}
		}
		require.Equal(t, count, 4)

		out, err = ovnClient.ListLogicalSwitchPorts(true, map[string]string{})
		require.NoError(t, err)
		count = 0
		for _, v := range out {
			if strings.Contains(v.Name, prefix) {
				count++
			}
		}
		require.Equal(t, count, 4)
	})

	t.Run("result should include lsp which externalIDs[key] is ''", func(t *testing.T) {
		lspName := "test-list-lsp-no-val"

		lsp := &ovnnb.LogicalSwitchPort{
			UUID: ovsclient.UUID(),
			Name: lspName,
			ExternalIDs: map[string]string{
				"vendor":               util.CniTypeName,
				"security_groups_test": "sg",
				"key":                  "val",
			},
		}

		err := createLogicalSwitchPort(ovnClient, lsp)
		require.NoError(t, err)

		out, err := ovnClient.ListLogicalSwitchPorts(true, map[string]string{"security_groups_test": "", "key": ""})
		require.NoError(t, err)
		require.Len(t, out, 1)
		require.Equal(t, lspName, out[0].Name)

		out, err = ovnClient.ListLogicalSwitchPorts(true, map[string]string{"security_groups_test": ""})
		require.NoError(t, err)
		require.Len(t, out, 1)
		require.Equal(t, lspName, out[0].Name)

		out, err = ovnClient.ListLogicalSwitchPorts(true, map[string]string{"security_groups_test": "", "key": "", "key1": ""})
		require.NoError(t, err)
		require.Empty(t, out)
	})
}

func (suite *OvnClientTestSuite) testListRemoteTypeLogicalSwitchPorts() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient

	t.Run("should include lsp which type is remote", func(t *testing.T) {
		lspName := "test-list-lsp-remote"

		lsp := &ovnnb.LogicalSwitchPort{
			UUID: ovsclient.UUID(),
			Name: lspName,
			ExternalIDs: map[string]string{
				"vendor":               util.CniTypeName,
				"security_groups_test": "sg",
				"key":                  "val",
			},
			Type: "remote",
		}

		err := createLogicalSwitchPort(ovnClient, lsp)
		require.NoError(t, err)

		out, err := ovnClient.ListRemoteTypeLogicalSwitchPorts()
		require.NoError(t, err)
		require.Len(t, out, 1)
		require.Equal(t, lspName, out[0].Name)
	})
}

func (suite *OvnClientTestSuite) testDeleteLogicalSwitchPort() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lspName := "test-delete-port-lsp"
	lsName := "test-delete-port-ls"

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	err = ovnClient.CreateBareLogicalSwitchPort(lsName, lspName)
	require.NoError(t, err)

	t.Run("no err when delete existent logical switch port", func(t *testing.T) {
		ls, err := ovnClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)

		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.Contains(t, ls.Ports, lsp.UUID)

		err = ovnClient.DeleteLogicalSwitchPort(lspName)
		require.NoError(t, err)

		_, err = ovnClient.GetLogicalSwitchPort(lspName, false)
		require.ErrorContains(t, err, "object not found")

		ls, err = ovnClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.NotContains(t, ls.Ports, lsp.UUID)
	})

	t.Run("no err when delete non-existent logical switch port", func(t *testing.T) {
		err := ovnClient.DeleteLogicalSwitchPort("test-delete-lrp-non-existent")
		require.NoError(t, err)
	})
}

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
