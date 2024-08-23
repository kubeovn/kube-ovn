package ovs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (suite *OvnClientTestSuite) testCreatePortGroup() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient

	t.Run("create new port group", func(t *testing.T) {
		pgName := "test-create-new-pg"
		externalIDs := map[string]string{
			"type": "test",
			"key":  "value",
		}

		err := ovnClient.CreatePortGroup(pgName, externalIDs)
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Equal(t, pgName, pg.Name)
		require.Equal(t, externalIDs, pg.ExternalIDs)
	})

	t.Run("create existing port group", func(t *testing.T) {
		pgName := "test-create-existing-pg"
		externalIDs := map[string]string{
			"type": "test",
			"key":  "value",
		}

		err := ovnClient.CreatePortGroup(pgName, externalIDs)
		require.NoError(t, err)

		err = ovnClient.CreatePortGroup(pgName, map[string]string{"new": "data"})
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Equal(t, pgName, pg.Name)
		require.Equal(t, externalIDs, pg.ExternalIDs)
	})

	t.Run("create port group with nil externalIDs", func(t *testing.T) {
		pgName := "test-create-pg-nil-externalids"
		err := ovnClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Equal(t, pgName, pg.Name)
		require.Empty(t, pg.ExternalIDs)
	})

	t.Run("create port group with empty externalIDs", func(t *testing.T) {
		pgName := "test-create-pg-empty-externalids"
		err := ovnClient.CreatePortGroup(pgName, map[string]string{})
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Equal(t, pgName, pg.Name)
		require.Empty(t, pg.ExternalIDs)
	})
}

func (suite *OvnClientTestSuite) testPortGroupResetPorts() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-reset-pg-ports-ls"
	pgName := "test-reset-pg-ports-pg"
	prefix := "test-reset-pg-ports-lsp"
	lspNames := make([]string, 0, 3)

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	for i := 1; i <= 3; i++ {
		lspName := fmt.Sprintf("%s-%d", prefix, i)
		lspNames = append(lspNames, lspName)

		err := ovnClient.CreateBareLogicalSwitchPort(lsName, lspName, "unknown", "")
		require.NoError(t, err)
	}

	err = ovnClient.CreatePortGroup(pgName, map[string]string{
		"type": "security_group",
		sgKey:  "test-sg",
	})
	require.NoError(t, err)

	err = ovnClient.PortGroupAddPorts(pgName, lspNames...)
	require.NoError(t, err)

	pg, err := ovnClient.GetPortGroup(pgName, false)
	require.NoError(t, err)
	require.NotEmpty(t, pg.Ports)

	err = ovnClient.PortGroupSetPorts(pgName, nil)
	require.NoError(t, err)

	pg, err = ovnClient.GetPortGroup(pgName, false)
	require.NoError(t, err)

	require.Empty(t, pg.Ports)
}

func (suite *OvnClientTestSuite) testPortGroupUpdatePorts() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	pgName := "test-add-ports-to-pg"
	lsName := "test-add-ports-to-ls"
	prefix := "test-add-lsp"
	lspNames := make([]string, 0, 3)

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	err = ovnClient.CreatePortGroup(pgName, map[string]string{
		"type": "security_group",
		sgKey:  "test-sg",
	})
	require.NoError(t, err)

	for i := 1; i <= 3; i++ {
		lspName := fmt.Sprintf("%s-%d", prefix, i)
		lspNames = append(lspNames, lspName)
		err := ovnClient.CreateBareLogicalSwitchPort(lsName, lspName, "", "")
		require.NoError(t, err)
	}

	t.Run("add ports to port group", func(t *testing.T) {
		err = ovnClient.PortGroupUpdatePorts(pgName, ovsdb.MutateOperationInsert, lspNames...)
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		for _, lspName := range lspNames {
			lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			require.Contains(t, pg.Ports, lsp.UUID)
		}
	})

	t.Run("should no err when add non-existent ports to port group", func(t *testing.T) {
		// add a non-existent ports
		err = ovnClient.PortGroupUpdatePorts(pgName, ovsdb.MutateOperationInsert, "test-add-lsp-non-existent")
		require.NoError(t, err)
	})

	t.Run("del ports from port group", func(t *testing.T) {
		err = ovnClient.PortGroupUpdatePorts(pgName, ovsdb.MutateOperationDelete, lspNames[0:2]...)
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		for i, lspName := range lspNames {
			lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
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
		err = ovnClient.PortGroupUpdatePorts(pgName, ovsdb.MutateOperationDelete, "test-del-lsp-non-existent")
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testDeletePortGroup() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	pgName := "test-delete-pg"

	t.Run("no err when delete existent port group", func(t *testing.T) {
		t.Parallel()
		err := ovnClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		err = ovnClient.DeletePortGroup(pgName)
		require.NoError(t, err)

		_, err = ovnClient.GetPortGroup(pgName, false)
		require.ErrorContains(t, err, "object not found")
	})

	t.Run("no err when delete non-existent logical router", func(t *testing.T) {
		t.Parallel()
		err := ovnClient.DeletePortGroup("test-delete-pg-non-existent")
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testGetGetPortGroup() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	pgName := "test-get-pg"

	err := ovnClient.CreatePortGroup(pgName, map[string]string{
		"type": "security_group",
		sgKey:  "test-sg",
	})
	require.NoError(t, err)

	t.Run("should return no err when found port group", func(t *testing.T) {
		t.Parallel()
		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Equal(t, pgName, pg.Name)
		require.NotEmpty(t, pg.UUID)
	})

	t.Run("should return err when not found port group", func(t *testing.T) {
		t.Parallel()
		_, err := ovnClient.GetPortGroup("test-get-pg-non-existent", false)
		require.ErrorContains(t, err, "object not found")
	})

	t.Run("no err when not found port group and ignoreNotFound is true", func(t *testing.T) {
		t.Parallel()
		_, err := ovnClient.GetPortGroup("test-get-pg-non-existent", true)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testListPortGroups() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient

	t.Run("result should exclude pg when externalIDs's length is not equal", func(t *testing.T) {
		pgName := "test-list-pg-mismatch-length"

		err := ovnClient.CreatePortGroup(pgName, map[string]string{"key": "value"})
		require.NoError(t, err)

		pgs, err := ovnClient.ListPortGroups(map[string]string{sgKey: "sg", "type": "security_group", "key": "value"})
		require.NoError(t, err)
		require.Empty(t, pgs)
	})

	t.Run("result should include lsp when key exists in pg column: external_ids", func(t *testing.T) {
		pgName := "test-list-pg-exist-key"

		err := ovnClient.CreatePortGroup(pgName, map[string]string{sgKey: "sg", "type": "security_group", "key": "value"})
		require.NoError(t, err)

		pgs, err := ovnClient.ListPortGroups(map[string]string{"type": "security_group", "key": "value"})
		require.NoError(t, err)
		require.Len(t, pgs, 1)
		require.Equal(t, pgName, pgs[0].Name)
	})

	t.Run("result should include all pg when externalIDs is empty", func(t *testing.T) {
		prefix := "test-list-pg-all"

		for i := 0; i < 4; i++ {
			pgName := fmt.Sprintf("%s-%d", prefix, i)

			err := ovnClient.CreatePortGroup(pgName, map[string]string{sgKey: "sg", "type": "security_group", "key": "value"})
			require.NoError(t, err)
		}

		out, err := ovnClient.ListPortGroups(nil)
		require.NoError(t, err)
		count := 0
		for _, v := range out {
			if strings.Contains(v.Name, prefix) {
				count++
			}
		}
		require.Equal(t, count, 4)

		out, err = ovnClient.ListPortGroups(map[string]string{})
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

		err := ovnClient.CreatePortGroup(pgName, map[string]string{"sg_test": "sg", "type": "security_group", "key": "value"})
		require.NoError(t, err)

		pgs, err := ovnClient.ListPortGroups(map[string]string{"sg_test": "", "key": ""})
		require.NoError(t, err)
		require.Len(t, pgs, 1)
		require.Equal(t, pgName, pgs[0].Name)

		pgs, err = ovnClient.ListPortGroups(map[string]string{"sg_test": ""})
		require.NoError(t, err)
		require.Len(t, pgs, 1)
		require.Equal(t, pgName, pgs[0].Name)

		pgs, err = ovnClient.ListPortGroups(map[string]string{"sg_test": "", "key": "", "key1": ""})
		require.NoError(t, err)
		require.Empty(t, pgs)
	})
}

func (suite *OvnClientTestSuite) testPortGroupUpdatePortOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	pgName := "test-update-port-op-pg"
	lspUUIDs := []string{ovsclient.NamedUUID(), ovsclient.NamedUUID()}

	err := ovnClient.CreatePortGroup(pgName, map[string]string{
		"type": "security_group",
		sgKey:  "test-sg",
	})
	require.NoError(t, err)

	t.Run("add new port to port group", func(t *testing.T) {
		t.Parallel()

		ops, err := ovnClient.portGroupUpdatePortOp(pgName, lspUUIDs, ovsdb.MutateOperationInsert)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "ports",
				Mutator: ovsdb.MutateOperationInsert,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
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

		ops, err := ovnClient.portGroupUpdatePortOp(pgName, lspUUIDs, ovsdb.MutateOperationDelete)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "ports",
				Mutator: ovsdb.MutateOperationDelete,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
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

		_, err := ovnClient.portGroupUpdatePortOp("test-port-op-pg-non-existent", lspUUIDs, ovsdb.MutateOperationInsert)
		require.ErrorContains(t, err, "object not found")
	})
}

func (suite *OvnClientTestSuite) testPortGroupUpdateACLOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	pgName := "test-update-acl-op-pg"
	aclUUIDs := []string{ovsclient.NamedUUID(), ovsclient.NamedUUID()}

	err := ovnClient.CreatePortGroup(pgName, map[string]string{
		"type": "security_group",
		sgKey:  "test-sg",
	})
	require.NoError(t, err)

	t.Run("add new acl to port group", func(t *testing.T) {
		t.Parallel()

		ops, err := ovnClient.portGroupUpdateACLOp(pgName, aclUUIDs, ovsdb.MutateOperationInsert)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "acls",
				Mutator: ovsdb.MutateOperationInsert,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
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

		ops, err := ovnClient.portGroupUpdateACLOp(pgName, aclUUIDs, ovsdb.MutateOperationDelete)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "acls",
				Mutator: ovsdb.MutateOperationDelete,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
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

		_, err := ovnClient.portGroupUpdateACLOp("test-acl-op-pg-non-existent", aclUUIDs, ovsdb.MutateOperationInsert)
		require.ErrorContains(t, err, "object not found")
	})
}

func (suite *OvnClientTestSuite) testPortGroupOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	pgName := "test-port-op-pg"

	err := ovnClient.CreatePortGroup(pgName, map[string]string{
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

	ops, err := ovnClient.portGroupOp(pgName, lspMutation, aclMutation)
	require.NoError(t, err)

	require.Len(t, ops[0].Mutations, 2)
	require.Equal(t, []ovsdb.Mutation{
		{
			Column:  "ports",
			Mutator: ovsdb.MutateOperationInsert,
			Value: ovsdb.OvsSet{
				GoSet: []interface{}{
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
				GoSet: []interface{}{
					ovsdb.UUID{
						GoUUID: aclUUID,
					},
				},
			},
		},
	}, ops[0].Mutations)
}

func (suite *OvnClientTestSuite) testPortGroupRemovePorts() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	pgName := "test-remove-ports-pg"
	lsName := "test-remove-ports-ls"
	prefix := "test-remove-lsp"
	lspNames := make([]string, 0, 5)

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	err = ovnClient.CreatePortGroup(pgName, map[string]string{
		"type": "security_group",
		sgKey:  "test-sg",
	})
	require.NoError(t, err)

	for i := 1; i <= 5; i++ {
		lspName := fmt.Sprintf("%s-%d", prefix, i)
		lspNames = append(lspNames, lspName)
		err := ovnClient.CreateBareLogicalSwitchPort(lsName, lspName, "", "")
		require.NoError(t, err)
	}

	err = ovnClient.PortGroupAddPorts(pgName, lspNames...)
	require.NoError(t, err)

	t.Run("remove some ports from port group", func(t *testing.T) {
		portsToRemove := lspNames[0:3]
		err = ovnClient.PortGroupRemovePorts(pgName, portsToRemove...)
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		for _, lspName := range portsToRemove {
			lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			require.NotContains(t, pg.Ports, lsp.UUID)
		}

		for _, lspName := range lspNames[3:] {
			lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			require.Contains(t, pg.Ports, lsp.UUID)
		}
	})

	t.Run("remove all remaining ports from port group", func(t *testing.T) {
		err = ovnClient.PortGroupRemovePorts(pgName, lspNames[3:]...)
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Empty(t, pg.Ports)
	})

	t.Run("remove non-existent ports from port group", func(t *testing.T) {
		err = ovnClient.PortGroupRemovePorts(pgName, "non-existent-lsp-1", "non-existent-lsp-2")
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Empty(t, pg.Ports)
	})

	t.Run("remove ports from non-existent port group", func(t *testing.T) {
		err = ovnClient.PortGroupRemovePorts("non-existent-pg", lspNames...)
		require.ErrorContains(t, err, "object not found")
	})

	t.Run("remove no ports from port group", func(t *testing.T) {
		err = ovnClient.PortGroupRemovePorts(pgName)
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Empty(t, pg.Ports)
	})
}

func (suite *OvnClientTestSuite) testUpdatePortGroup() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient

	t.Run("update external_ids", func(t *testing.T) {
		pgName := "test-update-external"

		err := ovnClient.CreatePortGroup(pgName, map[string]string{
			"type": "security_group",
			sgKey:  "test-sg",
		})
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		newExternalIDs := map[string]string{
			"type": "security_group",
			sgKey:  "updated-sg",
			"new":  "value",
		}
		pg.ExternalIDs = newExternalIDs

		err = ovnClient.UpdatePortGroup(pg, &pg.ExternalIDs)
		require.NoError(t, err)

		updatedPg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Equal(t, newExternalIDs, updatedPg.ExternalIDs)
	})

	t.Run("update name", func(t *testing.T) {
		pgName := "test-update-name"

		err := ovnClient.CreatePortGroup(pgName, map[string]string{
			"type": "security_group",
			sgKey:  "test-sg",
		})
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		newName := "updated-" + pgName
		pg.Name = newName

		err = ovnClient.UpdatePortGroup(pg, &pg.Name)
		require.NoError(t, err)

		_, err = ovnClient.GetPortGroup(pgName, false)
		require.Error(t, err)

		updatedPg, err := ovnClient.GetPortGroup(newName, false)
		require.NoError(t, err)
		require.Equal(t, newName, updatedPg.Name)
	})

	t.Run("update multiple fields", func(t *testing.T) {
		pgName := "test-update-multiple"

		err := ovnClient.CreatePortGroup(pgName, map[string]string{
			"type": "security_group",
			sgKey:  "test-sg",
		})
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		pg.Name = pgName
		pg.ExternalIDs = map[string]string{"key": "value"}

		err = ovnClient.UpdatePortGroup(pg, &pg.Name, &pg.ExternalIDs)
		require.NoError(t, err)

		updatedPg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Equal(t, pgName, updatedPg.Name)
		require.Equal(t, map[string]string{"key": "value"}, updatedPg.ExternalIDs)
	})

	t.Run("update port group with no changes", func(t *testing.T) {
		pgName := "test-update-no-changes"

		err := ovnClient.CreatePortGroup(pgName, map[string]string{
			"type": "security_group",
			sgKey:  "test-sg",
		})
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		err = ovnClient.UpdatePortGroup(pg)
		require.NoError(t, err)

		updatedPg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Equal(t, pg, updatedPg)
	})

	t.Run("update port group with invalid field", func(t *testing.T) {
		pgName := "test-update-invalid-field"

		err := ovnClient.CreatePortGroup(pgName, nil)
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		err = ovnClient.UpdatePortGroup(pg, "InvalidField")
		require.Error(t, err)
	})

	t.Run("update port group with nil value", func(t *testing.T) {
		pgName := "test-update-nil-value"

		err := ovnClient.CreatePortGroup(pgName, map[string]string{"key": "value"})
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)

		pg.ExternalIDs = nil
		err = ovnClient.UpdatePortGroup(pg, &pg.ExternalIDs)
		require.NoError(t, err)

		updatedPg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Empty(t, updatedPg.ExternalIDs)
	})
}

func (suite *OvnClientTestSuite) testPortGroupSetPorts() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	pgName := "test-set-ports-pg"
	lsName := "test-set-ports-ls"
	prefix := "test-set-lsp"
	lspNames := make([]string, 0, 5)

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	err = ovnClient.CreatePortGroup(pgName, map[string]string{
		"type": "security_group",
		sgKey:  "test-sg",
	})
	require.NoError(t, err)

	for i := 1; i <= 5; i++ {
		lspName := fmt.Sprintf("%s-%d", prefix, i)
		lspNames = append(lspNames, lspName)
		err := ovnClient.CreateBareLogicalSwitchPort(lsName, lspName, "", "")
		require.NoError(t, err)
	}

	t.Run("set ports to empty port group", func(t *testing.T) {
		portsToSet := lspNames[0:3]
		err = ovnClient.PortGroupSetPorts(pgName, portsToSet)
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Len(t, pg.Ports, 3)

		for _, lspName := range portsToSet {
			lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			require.Contains(t, pg.Ports, lsp.UUID)
		}
	})

	t.Run("set ports to non-empty port group", func(t *testing.T) {
		portsToSet := lspNames[2:5]
		err = ovnClient.PortGroupSetPorts(pgName, portsToSet)
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Len(t, pg.Ports, 3)

		for _, lspName := range portsToSet {
			lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			require.Contains(t, pg.Ports, lsp.UUID)
		}

		for _, lspName := range lspNames[0:2] {
			lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			require.NotContains(t, pg.Ports, lsp.UUID)
		}
	})

	t.Run("set ports to empty list", func(t *testing.T) {
		err = ovnClient.PortGroupSetPorts(pgName, []string{})
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Empty(t, pg.Ports)
	})

	t.Run("set ports with non-existent logical switch ports", func(t *testing.T) {
		portsToSet := append(lspNames[0:2], "non-existent-lsp")
		err = ovnClient.PortGroupSetPorts(pgName, portsToSet)
		require.NoError(t, err)

		pg, err := ovnClient.GetPortGroup(pgName, false)
		require.NoError(t, err)
		require.Len(t, pg.Ports, 2)

		for _, lspName := range lspNames[0:2] {
			lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
			require.NoError(t, err)
			require.Contains(t, pg.Ports, lsp.UUID)
		}
	})

	t.Run("set ports to non-existent port group", func(t *testing.T) {
		err = ovnClient.PortGroupSetPorts("non-existent-pg", lspNames)
		require.Error(t, err)
	})

	t.Run("set ports with empty port group name", func(t *testing.T) {
		err = ovnClient.PortGroupSetPorts("", lspNames)
		require.Error(t, err)
	})
}
