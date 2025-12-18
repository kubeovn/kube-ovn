package ovs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (suite *OvnClientTestSuite) testCreatePortGroup() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	t.Run("create new port group", func(t *testing.T) {
		pgName := "test-create-new-pg"
		externalIDs := map[string]string{
			"type": "test",
			"key":  "value",
		}

		err := nbClient.CreatePortGroup(pgName, externalIDs)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Equal(t, pgName, pg.Name)
		// vendor is automatically added by CreatePortGroup
		expectedExternalIDs := map[string]string{
			"type":   "test",
			"key":    "value",
			"vendor": util.CniTypeName,
		}
		require.Equal(t, expectedExternalIDs, pg.ExternalIDs)
	})

	t.Run("create existing port group", func(t *testing.T) {
		pgName := "test-create-existing-pg"
		externalIDs := map[string]string{
			"type": "test",
			"key":  "value",
		}
		updatedExternalIDs := map[string]string{"new": "data"}

		err := nbClient.CreatePortGroup(pgName, externalIDs)
		require.NoError(t, err)

		err = nbClient.CreatePortGroup(pgName, updatedExternalIDs)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Equal(t, pgName, pg.Name)
		// vendor is automatically added by CreatePortGroup
		expectedExternalIDs := map[string]string{
			"new":    "data",
			"vendor": util.CniTypeName,
		}
		require.Equal(t, expectedExternalIDs, pg.ExternalIDs)
	})

	t.Run("create port group with nil externalIDs", func(t *testing.T) {
		pgName := "test-create-pg-nil-externalids"
		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Equal(t, pgName, pg.Name)
		// vendor is automatically added by CreatePortGroup even with nil input
		require.Equal(t, map[string]string{"vendor": util.CniTypeName}, pg.ExternalIDs)
	})

	t.Run("create port group with empty externalIDs", func(t *testing.T) {
		pgName := "test-create-pg-empty-externalids"
		err := nbClient.CreatePortGroup(pgName, map[string]string{})
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Equal(t, pgName, pg.Name)
		// vendor is automatically added by CreatePortGroup even with empty input
		require.Equal(t, map[string]string{"vendor": util.CniTypeName}, pg.ExternalIDs)
	})
}

func (suite *OvnClientTestSuite) testPortGroupResetPorts() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lsName := "test-reset-pg-ports-ls"
	pgName := "test-reset-pg-ports-pg"
	prefix := "test-reset-pg-ports-lsp"
	lspNames := make([]string, 0, 3)

	err := nbClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	for i := 1; i <= 3; i++ {
		lspName := fmt.Sprintf("%s-%d", prefix, i)
		lspNames = append(lspNames, lspName)

		err := nbClient.CreateBareLogicalSwitchPort(lsName, lspName, "unknown", "")
		require.NoError(t, err)
	}

	err = nbClient.CreatePortGroup(pgName, map[string]string{
		"type": "security_group",
		sgKey:  "test-sg",
	})
	require.NoError(t, err)

	err = nbClient.PortGroupAddPorts(pgName, lspNames...)
	require.NoError(t, err)

	pg, err := nbClient.GetPortGroup(pgName, false)
	require.NoError(t, err)
	require.NotEmpty(t, pg.Ports)

	err = nbClient.PortGroupSetPorts(pgName, nil)
	require.NoError(t, err)

	pg, err = nbClient.GetPortGroup(pgName, false)
	require.NoError(t, err)

	require.Empty(t, pg.Ports)
}

func (suite *OvnClientTestSuite) testPortGroupUpdatePorts() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	pgName := "test-add-ports-to-pg"
	lsName := "test-add-ports-to-ls"
	prefix := "test-add-lsp"
	lspNames := make([]string, 0, 3)

	err := nbClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	err = nbClient.CreatePortGroup(pgName, map[string]string{
		"type": "security_group",
		sgKey:  "test-sg",
	})
	require.NoError(t, err)

	for i := 1; i <= 3; i++ {
		lspName := fmt.Sprintf("%s-%d", prefix, i)
		lspNames = append(lspNames, lspName)
		err := nbClient.CreateBareLogicalSwitchPort(lsName, lspName, "", "")
		require.NoError(t, err)
	}

	t.Run("add ports to port group", func(t *testing.T) {
		err = nbClient.PortGroupUpdatePorts(pgName, ovsdb.MutateOperationInsert, lspNames...)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		for _, lspName := range lspNames {
			lsp, err := nbClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			require.Contains(t, pg.Ports, lsp.UUID)
		}
	})

	t.Run("should no err when add non-existent ports to port group", func(t *testing.T) {
		// add a non-existent ports
		err = nbClient.PortGroupUpdatePorts(pgName, ovsdb.MutateOperationInsert, "test-add-lsp-non-existent")
		require.NoError(t, err)
	})

	t.Run("del ports from port group", func(t *testing.T) {
		err = nbClient.PortGroupUpdatePorts(pgName, ovsdb.MutateOperationDelete, lspNames[0:2]...)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		for i, lspName := range lspNames {
			lsp, err := nbClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)

			// port group contains the last ports
			if i == 2 {
				require.Contains(t, pg.Ports, lsp.UUID)
				continue
			}
			require.NotContains(t, pg.Ports, lsp.UUID)
		}
	})

	t.Run("del non-existent ports from port group", func(t *testing.T) {
		// del a non-existent ports
		err = nbClient.PortGroupUpdatePorts(pgName, ovsdb.MutateOperationDelete, "test-del-lsp-non-existent")
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testDeletePortGroup() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	pgName := "test-delete-pg"

	t.Run("no err when delete existent port group", func(t *testing.T) {
		t.Parallel()
		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		err = nbClient.DeletePortGroup(pgName)
		require.NoError(t, err)

		_, err = nbClient.GetPortGroup(pgName, false)
		require.ErrorContains(t, err, "object not found")
	})

	t.Run("no err when delete non-existent logical router", func(t *testing.T) {
		t.Parallel()
		err := nbClient.DeletePortGroup("test-delete-pg-non-existent")
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testGetGetPortGroup() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	pgName := "test-get-pg"

	err := nbClient.CreatePortGroup(pgName, map[string]string{
		"type": "security_group",
		sgKey:  "test-sg",
	})
	require.NoError(t, err)

	t.Run("should return no err when found port group", func(t *testing.T) {
		t.Parallel()
		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Equal(t, pgName, pg.Name)
		require.NotEmpty(t, pg.UUID)
	})

	t.Run("should return err when not found port group", func(t *testing.T) {
		t.Parallel()
		_, err := nbClient.GetPortGroup("test-get-pg-non-existent", false)
		require.ErrorContains(t, err, "object not found")
	})

	t.Run("no err when not found port group and ignoreNotFound is true", func(t *testing.T) {
		t.Parallel()
		_, err := nbClient.GetPortGroup("test-get-pg-non-existent", true)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testListPortGroups() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	t.Run("result should exclude pg when externalIDs's length is not equal", func(t *testing.T) {
		pgName := "test-list-pg-mismatch-length"

		err := nbClient.CreatePortGroup(pgName, map[string]string{"key": "value"})
		require.NoError(t, err)

		pgs, err := nbClient.ListPortGroups(map[string]string{sgKey: "sg", "type": "security_group", "key": "value"})
		require.NoError(t, err)
		require.Empty(t, pgs)
	})

	t.Run("result should include lsp when key exists in pg column: external_ids", func(t *testing.T) {
		pgName := "test-list-pg-exist-key"

		err := nbClient.CreatePortGroup(pgName, map[string]string{sgKey: "sg", "type": "security_group", "key": "value"})
		require.NoError(t, err)

		pgs, err := nbClient.ListPortGroups(map[string]string{"type": "security_group", "key": "value"})
		require.NoError(t, err)
		require.Len(t, pgs, 1)
		require.Equal(t, pgName, pgs[0].Name)
	})

	t.Run("result should include all pg when externalIDs is empty", func(t *testing.T) {
		prefix := "test-list-pg-all"

		for i := range 4 {
			pgName := fmt.Sprintf("%s-%d", prefix, i)

			err := nbClient.CreatePortGroup(pgName, map[string]string{sgKey: "sg", "type": "security_group", "key": "value"})
			require.NoError(t, err)
		}

		out, err := nbClient.ListPortGroups(nil)
		require.NoError(t, err)
		count := 0
		for _, v := range out {
			if strings.Contains(v.Name, prefix) {
				count++
			}
		}
		require.Equal(t, count, 4)

		out, err = nbClient.ListPortGroups(map[string]string{})
		require.NoError(t, err)
		count = 0
		for _, v := range out {
			if strings.Contains(v.Name, prefix) {
				count++
			}
		}
		require.Equal(t, count, 4)
	})

	t.Run("result should include pg which externalIDs[key] is ''", func(t *testing.T) {
		pgName := "test-list-pg-no-val"

		err := nbClient.CreatePortGroup(pgName, map[string]string{"sg_test": "sg", "type": "security_group", "key": "value"})
		require.NoError(t, err)

		pgs, err := nbClient.ListPortGroups(map[string]string{"sg_test": "", "key": ""})
		require.NoError(t, err)
		require.Len(t, pgs, 1)
		require.Equal(t, pgName, pgs[0].Name)

		pgs, err = nbClient.ListPortGroups(map[string]string{"sg_test": ""})
		require.NoError(t, err)
		require.Len(t, pgs, 1)
		require.Equal(t, pgName, pgs[0].Name)

		pgs, err = nbClient.ListPortGroups(map[string]string{"sg_test": "", "key": "", "key1": ""})
		require.NoError(t, err)
		require.Empty(t, pgs)
	})
}

func (suite *OvnClientTestSuite) testPortGroupUpdatePortOp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	pgName := "test-update-port-op-pg"
	lspUUIDs := []string{ovsclient.NamedUUID(), ovsclient.NamedUUID()}

	err := nbClient.CreatePortGroup(pgName, map[string]string{
		"type": "security_group",
		sgKey:  "test-sg",
	})
	require.NoError(t, err)

	t.Run("add new port to port group", func(t *testing.T) {
		t.Parallel()

		ops, err := nbClient.portGroupUpdatePortOp(pgName, lspUUIDs, ovsdb.MutateOperationInsert)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "ports",
				Mutator: ovsdb.MutateOperationInsert,
				Value: ovsdb.OvsSet{
					GoSet: []any{
						ovsdb.UUID{
							GoUUID: lspUUIDs[0],
						},
						ovsdb.UUID{
							GoUUID: lspUUIDs[1],
						},
					},
				},
			},
		}, ops[0].Mutations)
	})

	t.Run("del port from port group", func(t *testing.T) {
		t.Parallel()

		ops, err := nbClient.portGroupUpdatePortOp(pgName, lspUUIDs, ovsdb.MutateOperationDelete)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "ports",
				Mutator: ovsdb.MutateOperationDelete,
				Value: ovsdb.OvsSet{
					GoSet: []any{
						ovsdb.UUID{
							GoUUID: lspUUIDs[0],
						},
						ovsdb.UUID{
							GoUUID: lspUUIDs[1],
						},
					},
				},
			},
		}, ops[0].Mutations)
	})

	t.Run("should return err when port group does not exist", func(t *testing.T) {
		t.Parallel()

		_, err := nbClient.portGroupUpdatePortOp("test-port-op-pg-non-existent", lspUUIDs, ovsdb.MutateOperationInsert)
		require.ErrorContains(t, err, "object not found")
	})
}

func (suite *OvnClientTestSuite) testPortGroupUpdateACLOp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	pgName := "test-update-acl-op-pg"
	aclUUIDs := []string{ovsclient.NamedUUID(), ovsclient.NamedUUID()}

	err := nbClient.CreatePortGroup(pgName, map[string]string{
		"type": "security_group",
		sgKey:  "test-sg",
	})
	require.NoError(t, err)

	t.Run("add new acl to port group", func(t *testing.T) {
		t.Parallel()

		ops, err := nbClient.portGroupUpdateACLOp(pgName, aclUUIDs, ovsdb.MutateOperationInsert)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "acls",
				Mutator: ovsdb.MutateOperationInsert,
				Value: ovsdb.OvsSet{
					GoSet: []any{
						ovsdb.UUID{
							GoUUID: aclUUIDs[0],
						},
						ovsdb.UUID{
							GoUUID: aclUUIDs[1],
						},
					},
				},
			},
		}, ops[0].Mutations)
	})

	t.Run("del acl from port group", func(t *testing.T) {
		t.Parallel()

		ops, err := nbClient.portGroupUpdateACLOp(pgName, aclUUIDs, ovsdb.MutateOperationDelete)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "acls",
				Mutator: ovsdb.MutateOperationDelete,
				Value: ovsdb.OvsSet{
					GoSet: []any{
						ovsdb.UUID{
							GoUUID: aclUUIDs[0],
						},
						ovsdb.UUID{
							GoUUID: aclUUIDs[1],
						},
					},
				},
			},
		}, ops[0].Mutations)
	})

	t.Run("should return err when port group does not exist", func(t *testing.T) {
		t.Parallel()

		_, err := nbClient.portGroupUpdateACLOp("test-acl-op-pg-non-existent", aclUUIDs, ovsdb.MutateOperationInsert)
		require.ErrorContains(t, err, "object not found")
	})
}

func (suite *OvnClientTestSuite) testPortGroupOp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	pgName := "test-port-op-pg"

	err := nbClient.CreatePortGroup(pgName, map[string]string{
		"type": "security_group",
		sgKey:  "test-sg",
	})
	require.NoError(t, err)

	lspUUID := ovsclient.NamedUUID()
	lspMutation := func(pg *ovnnb.PortGroup) *model.Mutation {
		mutation := &model.Mutation{
			Field:   &pg.Ports,
			Value:   []string{lspUUID},
			Mutator: ovsdb.MutateOperationInsert,
		}

		return mutation
	}

	aclUUID := ovsclient.NamedUUID()
	aclMutation := func(pg *ovnnb.PortGroup) *model.Mutation {
		mutation := &model.Mutation{
			Field:   &pg.ACLs,
			Value:   []string{aclUUID},
			Mutator: ovsdb.MutateOperationInsert,
		}

		return mutation
	}

	ops, err := nbClient.portGroupOp(pgName, lspMutation, aclMutation)
	require.NoError(t, err)

	require.Len(t, ops[0].Mutations, 2)
	require.Equal(t, []ovsdb.Mutation{
		{
			Column:  "ports",
			Mutator: ovsdb.MutateOperationInsert,
			Value: ovsdb.OvsSet{
				GoSet: []any{
					ovsdb.UUID{
						GoUUID: lspUUID,
					},
				},
			},
		},
		{
			Column:  "acls",
			Mutator: ovsdb.MutateOperationInsert,
			Value: ovsdb.OvsSet{
				GoSet: []any{
					ovsdb.UUID{
						GoUUID: aclUUID,
					},
				},
			},
		},
	}, ops[0].Mutations)

	ops, err = nbClient.portGroupOp(pgName)
	require.NoError(t, err)
	require.Nil(t, ops)
}

func (suite *OvnClientTestSuite) testPortGroupRemovePorts() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	pgName := "test-remove-ports-pg"
	lsName := "test-remove-ports-ls"
	prefix := "test-remove-lsp"
	lspNames := make([]string, 0, 5)

	err := nbClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	err = nbClient.CreatePortGroup(pgName, map[string]string{
		"type": "security_group",
		sgKey:  "test-sg",
	})
	require.NoError(t, err)

	for i := 1; i <= 5; i++ {
		lspName := fmt.Sprintf("%s-%d", prefix, i)
		lspNames = append(lspNames, lspName)
		err := nbClient.CreateBareLogicalSwitchPort(lsName, lspName, "", "")
		require.NoError(t, err)
	}

	err = nbClient.PortGroupAddPorts(pgName, lspNames...)
	require.NoError(t, err)

	t.Run("remove some ports from port group", func(t *testing.T) {
		portsToRemove := lspNames[0:3]
		err = nbClient.PortGroupRemovePorts(pgName, portsToRemove...)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		for _, lspName := range portsToRemove {
			lsp, err := nbClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			require.NotContains(t, pg.Ports, lsp.UUID)
		}

		for _, lspName := range lspNames[3:] {
			lsp, err := nbClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			require.Contains(t, pg.Ports, lsp.UUID)
		}
	})

	t.Run("remove all remaining ports from port group", func(t *testing.T) {
		err = nbClient.PortGroupRemovePorts(pgName, lspNames[3:]...)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Empty(t, pg.Ports)
	})

	t.Run("remove non-existent ports from port group", func(t *testing.T) {
		err = nbClient.PortGroupRemovePorts(pgName, "non-existent-lsp-1", "non-existent-lsp-2")
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Empty(t, pg.Ports)
	})

	t.Run("remove ports from non-existent port group", func(t *testing.T) {
		err = nbClient.PortGroupRemovePorts("non-existent-pg", lspNames...)
		require.ErrorContains(t, err, "object not found")
	})

	t.Run("remove no ports from port group", func(t *testing.T) {
		err = nbClient.PortGroupRemovePorts(pgName)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Empty(t, pg.Ports)
	})
}

func (suite *OvnClientTestSuite) testUpdatePortGroup() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	t.Run("update external_ids", func(t *testing.T) {
		pgName := "test-update-external"

		err := nbClient.CreatePortGroup(pgName, map[string]string{
			"type": "security_group",
			sgKey:  "test-sg",
		})
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		newExternalIDs := map[string]string{
			"type": "security_group",
			sgKey:  "updated-sg",
			"new":  "value",
		}
		pg.ExternalIDs = newExternalIDs

		err = nbClient.UpdatePortGroup(pg, &pg.ExternalIDs)
		require.NoError(t, err)

		updatedPg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Equal(t, newExternalIDs, updatedPg.ExternalIDs)
	})

	t.Run("update name", func(t *testing.T) {
		pgName := "test-update-name"

		err := nbClient.CreatePortGroup(pgName, map[string]string{
			"type": "security_group",
			sgKey:  "test-sg",
		})
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		newName := "updated-" + pgName
		pg.Name = newName

		err = nbClient.UpdatePortGroup(pg, &pg.Name)
		require.NoError(t, err)

		_, err = nbClient.GetPortGroup(pgName, false)
		require.Error(t, err)

		updatedPg, err := nbClient.GetPortGroup(newName, false)
		require.NoError(t, err)
		require.Equal(t, newName, updatedPg.Name)
	})

	t.Run("update multiple fields", func(t *testing.T) {
		pgName := "test-update-multiple"

		err := nbClient.CreatePortGroup(pgName, map[string]string{
			"type": "security_group",
			sgKey:  "test-sg",
		})
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		pg.Name = pgName
		pg.ExternalIDs = map[string]string{"key": "value"}

		err = nbClient.UpdatePortGroup(pg, &pg.Name, &pg.ExternalIDs)
		require.NoError(t, err)

		updatedPg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Equal(t, pgName, updatedPg.Name)
		require.Equal(t, map[string]string{"key": "value"}, updatedPg.ExternalIDs)
	})

	t.Run("update port group with no changes", func(t *testing.T) {
		pgName := "test-update-no-changes"

		err := nbClient.CreatePortGroup(pgName, map[string]string{
			"type": "security_group",
			sgKey:  "test-sg",
		})
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		err = nbClient.UpdatePortGroup(pg)
		require.NoError(t, err)

		updatedPg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Equal(t, pg, updatedPg)
	})

	t.Run("update port group with invalid field", func(t *testing.T) {
		pgName := "test-update-invalid-field"

		err := nbClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		err = nbClient.UpdatePortGroup(pg, "InvalidField")
		require.Error(t, err)
	})

	t.Run("update port group with nil value", func(t *testing.T) {
		pgName := "test-update-nil-value"

		err := nbClient.CreatePortGroup(pgName, map[string]string{"key": "value"})
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		pg.ExternalIDs = nil
		err = nbClient.UpdatePortGroup(pg, &pg.ExternalIDs)
		require.NoError(t, err)

		updatedPg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Empty(t, updatedPg.ExternalIDs)
	})
}

func (suite *OvnClientTestSuite) testPortGroupSetPorts() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	pgName := "test-set-ports-pg"
	lsName := "test-set-ports-ls"
	prefix := "test-set-lsp"
	lspNames := make([]string, 0, 5)

	err := nbClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	err = nbClient.CreatePortGroup(pgName, map[string]string{
		"type": "security_group",
		sgKey:  "test-sg",
	})
	require.NoError(t, err)

	for i := 1; i <= 5; i++ {
		lspName := fmt.Sprintf("%s-%d", prefix, i)
		lspNames = append(lspNames, lspName)
		err := nbClient.CreateBareLogicalSwitchPort(lsName, lspName, "", "")
		require.NoError(t, err)
	}

	t.Run("set ports to empty port group", func(t *testing.T) {
		portsToSet := lspNames[0:3]
		err = nbClient.PortGroupSetPorts(pgName, portsToSet)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Len(t, pg.Ports, 3)

		for _, lspName := range portsToSet {
			lsp, err := nbClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			require.Contains(t, pg.Ports, lsp.UUID)
		}
	})

	t.Run("set ports to non-empty port group", func(t *testing.T) {
		portsToSet := lspNames[2:5]
		err = nbClient.PortGroupSetPorts(pgName, portsToSet)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Len(t, pg.Ports, 3)

		for _, lspName := range portsToSet {
			lsp, err := nbClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			require.Contains(t, pg.Ports, lsp.UUID)
		}

		for _, lspName := range lspNames[0:2] {
			lsp, err := nbClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			require.NotContains(t, pg.Ports, lsp.UUID)
		}
	})

	t.Run("set ports to empty list", func(t *testing.T) {
		err = nbClient.PortGroupSetPorts(pgName, []string{})
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Empty(t, pg.Ports)
	})

	t.Run("set ports with non-existent logical switch ports", func(t *testing.T) {
		portsToSet := append(lspNames[0:2], "non-existent-lsp")
		err = nbClient.PortGroupSetPorts(pgName, portsToSet)
		require.NoError(t, err)

		pg, err := nbClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Len(t, pg.Ports, 2)

		for _, lspName := range lspNames[0:2] {
			lsp, err := nbClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			require.Contains(t, pg.Ports, lsp.UUID)
		}
	})

	t.Run("set ports to non-existent port group", func(t *testing.T) {
		err = nbClient.PortGroupSetPorts("non-existent-pg", lspNames)
		require.Error(t, err)
	})

	t.Run("set ports with empty port group name", func(t *testing.T) {
		err = nbClient.PortGroupSetPorts("", lspNames)
		require.Error(t, err)
	})
}

func (suite *OvnClientTestSuite) testRemovePortFromPortGroups() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lsName := "test-rm-port-from-pgs-ls"
	lspName := "test-rm-port-from-pgs-lsp"
	pg1Name := "test-rm-port-from-pgs-pg1"
	pg2Name := "test-rm-port-from-pgs-pg2"

	err := nbClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	err = nbClient.CreateBareLogicalSwitchPort(lsName, lspName, "", "")
	require.NoError(t, err)

	err = nbClient.CreatePortGroup(pg1Name, nil)
	require.NoError(t, err)

	err = nbClient.CreatePortGroup(pg2Name, nil)
	require.NoError(t, err)

	t.Run("remove port from all port groups", func(t *testing.T) {
		err = nbClient.PortGroupAddPorts(pg1Name, lspName)
		require.NoError(t, err)
		pg1, err := nbClient.GetPortGroup(pg1Name, false)
		require.NoError(t, err)
		require.Len(t, pg1.Ports, 1)

		err = nbClient.PortGroupAddPorts(pg2Name, lspName)
		require.NoError(t, err)
		pg2, err := nbClient.GetPortGroup(pg2Name, false)
		require.NoError(t, err)
		require.Len(t, pg2.Ports, 1)

		err = nbClient.RemovePortFromPortGroups(lspName)
		require.NoError(t, err)

		pg1, err = nbClient.GetPortGroup(pg1Name, false)
		require.NoError(t, err)
		require.Empty(t, pg1.Ports)
		pg2, err = nbClient.GetPortGroup(pg2Name, false)
		require.NoError(t, err)
		require.Empty(t, pg2.Ports)
	})

	t.Run("remove port from specific port group", func(t *testing.T) {
		err = nbClient.PortGroupAddPorts(pg1Name, lspName)
		require.NoError(t, err)
		pg1, err := nbClient.GetPortGroup(pg1Name, false)
		require.NoError(t, err)
		require.Len(t, pg1.Ports, 1)

		err = nbClient.PortGroupAddPorts(pg2Name, lspName)
		require.NoError(t, err)
		pg2, err := nbClient.GetPortGroup(pg2Name, false)
		require.NoError(t, err)
		require.Len(t, pg2.Ports, 1)

		err = nbClient.RemovePortFromPortGroups(lspName, pg1Name)
		require.NoError(t, err)

		pg1, err = nbClient.GetPortGroup(pg1Name, false)
		require.NoError(t, err)
		require.Empty(t, pg1.Ports)

		pg2, err = nbClient.GetPortGroup(pg2Name, false)
		require.NoError(t, err)
		require.Len(t, pg2.Ports, 1)
	})

	t.Run("remove port from specific port groups", func(t *testing.T) {
		err = nbClient.PortGroupAddPorts(pg1Name, lspName)
		require.NoError(t, err)
		pg1, err := nbClient.GetPortGroup(pg1Name, false)
		require.NoError(t, err)
		require.Len(t, pg1.Ports, 1)

		err = nbClient.PortGroupAddPorts(pg2Name, lspName)
		require.NoError(t, err)
		pg2, err := nbClient.GetPortGroup(pg2Name, false)
		require.NoError(t, err)
		require.Len(t, pg2.Ports, 1)

		err = nbClient.RemovePortFromPortGroups(lspName, pg1Name, pg2Name)
		require.NoError(t, err)

		pg1, err = nbClient.GetPortGroup(pg1Name, false)
		require.NoError(t, err)
		require.Empty(t, pg1.Ports)
		pg2, err = nbClient.GetPortGroup(pg2Name, false)
		require.NoError(t, err)
		require.Empty(t, pg2.Ports)
	})
}
