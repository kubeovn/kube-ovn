package ovs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func createLogicalSwitch(c *OVNNbClient, ls *ovnnb.LogicalSwitch) error {
	op, err := c.Create(ls)
	if err != nil {
		return err
	}

	return c.Transact("ls-add", op)
}

func (suite *OvnClientTestSuite) testCreateLogicalSwitch() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lsName := "test-create-ls-ls"
	lrName := "test-create-ls-lr"
	mac := util.GenerateMac()
	lspName := fmt.Sprintf("%s-%s", lsName, lrName)
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("create logical switch and router type port when logical switch does't exist and needRouter is true", func(t *testing.T) {
		err = nbClient.CreateLogicalSwitch(lsName, lrName, "192.168.2.0/24,fd00::c0a8:6400/120", "192.168.2.1,fd00::c0a8:6401", mac, true, false)
		require.NoError(t, err)

		_, err := nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)

		_, err = nbClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)

		lrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.NotNil(t, lrp)
		require.Equal(t, mac, lrp.MAC)
	})

	t.Run("only update networks when logical switch exist and router type port exist and needRouter is true", func(t *testing.T) {
		err = nbClient.CreateLogicalSwitch(lsName, lrName, "192.168.2.0/24,fd00::c0a8:9900/120", "192.168.2.1,fd00::c0a8:9901", mac, true, false)
		require.NoError(t, err)

		lrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.NotNil(t, lrp)
		require.ElementsMatch(t, []string{"192.168.2.1/24", "fd00::c0a8:9901/120"}, lrp.Networks)
		require.Equal(t, mac, lrp.MAC)
	})

	t.Run("don't update networks when logical switch exist and randomAllocateGW is true", func(t *testing.T) {
		err = nbClient.CreateLogicalSwitch(lsName, lrName, "192.168.2.0/24,fd00::c0a8:6400/120", "192.168.2.1,fd00::c0a8:6401", mac, true, true)
		require.NoError(t, err)

		lrp, err := nbClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.NotNil(t, lrp)
		require.ElementsMatch(t, []string{"192.168.2.1/24", "fd00::c0a8:9901/120"}, lrp.Networks)
		require.Equal(t, mac, lrp.MAC)
	})

	t.Run("remove router type port when needRouter is false", func(t *testing.T) {
		err = nbClient.CreateLogicalSwitch(lsName, lrName, "192.168.2.0/24,fd00::c0a8:9900/120", "192.168.2.1,fd00::c0a8:9901", "", false, false)
		require.NoError(t, err)
	})

	t.Run("should no err when router type port doest't exist", func(t *testing.T) {
		err = nbClient.CreateLogicalSwitch(lsName+"-1", lrName+"-1", "192.168.2.0/24,fd00::c0a8:9900/120", "192.168.2.1,fd00::c0a8:9901", "", false, false)
		require.NoError(t, err)
	})

	t.Run("create logical switch when logical switch does't exist and needRouter is false and randomAllocateGW is false", func(t *testing.T) {
		err = nbClient.CreateLogicalSwitch(lsName+"-2", lrName+"-2", "192.168.2.0/24,fd00::c0a8:9900/120", "192.168.2.1,fd00::c0a8:9901", "", false, true)
		require.NoError(t, err)
	})

	t.Run("create logical switch using invalid gateway and cidrBlock", func(t *testing.T) {
		err = nbClient.CreateLogicalSwitch(lsName, lrName, "192.168.2.0/24,fd00::c0a8:6400/120", "192.168.2.1", mac, true, false)
		require.ErrorContains(t, err, "ip 192.168.2.1 should be dualstack")
	})
}

func (suite *OvnClientTestSuite) testLogicalSwitchAddPort() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lsName := "test-add-port-ls"
	lspName := "test-add-port-lsp"

	err := nbClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	err = nbClient.CreateBareLogicalSwitchPort(lsName, lspName, "", "")
	require.NoError(t, err)

	lsp, err := nbClient.GetLogicalSwitchPort(lspName, false)
	require.NoError(t, err)

	t.Run("add port to logical switch", func(t *testing.T) {
		err = nbClient.LogicalSwitchAddPort(lsName, lspName)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Contains(t, ls.Ports, lsp.UUID)
	})

	t.Run("add port to logical switch repeatedly", func(t *testing.T) {
		err = nbClient.LogicalSwitchAddPort(lsName, lspName)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Contains(t, ls.Ports, lsp.UUID)
	})
}

func (suite *OvnClientTestSuite) testLogicalSwitchDelPort() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lsName := "test-del-port-ls"
	lspName := "test-del-port-lsp"

	err := nbClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	err = nbClient.CreateBareLogicalSwitchPort(lsName, lspName, "unknown", "")
	require.NoError(t, err)

	lsp, err := nbClient.GetLogicalSwitchPort(lspName, false)
	require.NoError(t, err)

	err = nbClient.LogicalSwitchAddPort(lsName, lspName)
	require.NoError(t, err)

	t.Run("del port from logical switch", func(t *testing.T) {
		ls, err := nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Contains(t, ls.Ports, lsp.UUID)

		err = nbClient.LogicalSwitchDelPort(lsName, lspName)
		require.NoError(t, err)

		ls, err = nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.NotContains(t, ls.Ports, lsp.UUID)
	})

	t.Run("del port from logical switch repeatedly", func(t *testing.T) {
		err := nbClient.LogicalSwitchDelPort(lsName, lspName)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.NotContains(t, ls.Ports, lsp.UUID)
	})
}

func (suite *OvnClientTestSuite) testLogicalSwitchUpdateLoadBalancers() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lsName := "test-add-lb-to-ls"
	prefix := "test-add-lb"
	lbNames := make([]string, 0, 3)

	err := nbClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	for i := 1; i <= 3; i++ {
		lbName := fmt.Sprintf("%s-%d", prefix, i)
		lbNames = append(lbNames, lbName)
		err := nbClient.CreateLoadBalancer(lbName, "tcp", "")
		require.NoError(t, err)
	}

	t.Run("add lbs to logical switch", func(t *testing.T) {
		err = nbClient.LogicalSwitchUpdateLoadBalancers(lsName, ovsdb.MutateOperationInsert, lbNames...)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)

		for _, lbName := range lbNames {
			lb, err := nbClient.GetLoadBalancer(lbName, false)
			require.NoError(t, err)
			require.Contains(t, ls.LoadBalancer, lb.UUID)
		}
	})

	t.Run("should no err when add non-existent lbs to logical switch", func(t *testing.T) {
		// add a non-existent lb
		err = nbClient.LogicalSwitchUpdateLoadBalancers(lsName, ovsdb.MutateOperationInsert, "test-add-lb-non-existent")
		require.NoError(t, err)
	})

	t.Run("del lbs from logical switch", func(t *testing.T) {
		// delete the first two lbs from logical switch
		err = nbClient.LogicalSwitchUpdateLoadBalancers(lsName, ovsdb.MutateOperationDelete, lbNames[0:2]...)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)

		for i, lbName := range lbNames {
			lb, err := nbClient.GetLoadBalancer(lbName, false)
			require.NoError(t, err)

			// logical switch contains the last lb
			if i == 2 {
				require.Contains(t, ls.LoadBalancer, lb.UUID)
				continue
			}
			require.NotContains(t, ls.LoadBalancer, lb.UUID)
		}
	})

	t.Run("del non-existent lbs from logical switch", func(t *testing.T) {
		err = nbClient.LogicalSwitchUpdateLoadBalancers(lsName, ovsdb.MutateOperationDelete, []string{"test-del-lb-non-existent", "test-del-lb-non-existent-1"}...)
		require.NoError(t, err)
	})

	t.Run("update with empty load balancer list", func(t *testing.T) {
		err := nbClient.LogicalSwitchUpdateLoadBalancers(lsName, ovsdb.MutateOperationInsert)
		require.NoError(t, err)
	})

	t.Run("update load balancers for non-existent logical switch", func(t *testing.T) {
		err := nbClient.LogicalSwitchUpdateLoadBalancers("non-existent-ls", ovsdb.MutateOperationInsert, lbNames...)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found logical switch")
	})
}

func (suite *OvnClientTestSuite) testDeleteLogicalSwitch() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	name := "test-delete-ls"

	t.Run("no err when delete existent logical switch", func(t *testing.T) {
		t.Parallel()
		err := nbClient.CreateBareLogicalSwitch(name)
		require.NoError(t, err)

		err = nbClient.DeleteLogicalSwitch(name)
		require.NoError(t, err)

		_, err = nbClient.GetLogicalSwitch(name, false)
		require.ErrorContains(t, err, "not found logical switch")
	})

	t.Run("no err when delete non-existent logical switch", func(t *testing.T) {
		t.Parallel()
		err := nbClient.DeleteLogicalSwitch("test-delete-ls-non-existent")
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testGetLogicalSwitch() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	name := "test-get-ls"

	err := nbClient.CreateBareLogicalSwitch(name)
	require.NoError(t, err)

	t.Run("should return no err when found logical switch", func(t *testing.T) {
		lr, err := nbClient.GetLogicalSwitch(name, false)
		require.NoError(t, err)
		require.Equal(t, name, lr.Name)
		require.NotEmpty(t, lr.UUID)
	})

	t.Run("should return err when not found logical switch", func(t *testing.T) {
		_, err := nbClient.GetLogicalSwitch("test-get-lr-non-existent", false)
		require.ErrorContains(t, err, "not found logical switch")
	})

	t.Run("no err when not found logical switch and ignoreNotFound is true", func(t *testing.T) {
		_, err := nbClient.GetLogicalSwitch("test-get-lr-non-existent", true)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testListLogicalSwitch() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	namePrefix := "test-list-ls-"

	i := 0
	// create three logical switch
	for ; i < 3; i++ {
		name := fmt.Sprintf("%s%d", namePrefix, i)
		err := nbClient.CreateBareLogicalSwitch(name)
		require.NoError(t, err)
	}

	// create two logical switch which vendor is others
	for ; i < 5; i++ {
		name := fmt.Sprintf("%s%d", namePrefix, i)
		ls := &ovnnb.LogicalSwitch{
			Name:        name,
			ExternalIDs: map[string]string{"vendor": "test-vendor"},
		}

		err := createLogicalSwitch(nbClient, ls)
		require.NoError(t, err)
	}

	// create two logical switch without vendor
	for ; i < 7; i++ {
		name := fmt.Sprintf("%s%d", namePrefix, i)
		ls := &ovnnb.LogicalSwitch{
			Name: name,
		}

		err := createLogicalSwitch(nbClient, ls)
		require.NoError(t, err)
	}

	t.Run("return all logical switch which match vendor", func(t *testing.T) {
		t.Parallel()
		lss, err := nbClient.ListLogicalSwitch(true, nil)
		require.NoError(t, err)
		require.NotEmpty(t, lss)

		count := 0
		for _, ls := range lss {
			if strings.Contains(ls.Name, namePrefix) {
				count++
			}
		}
		require.Equal(t, count, 3)
	})

	t.Run("has custom filter", func(t *testing.T) {
		t.Parallel()
		lss, err := nbClient.ListLogicalSwitch(false, func(ls *ovnnb.LogicalSwitch) bool {
			return len(ls.ExternalIDs) == 0 || ls.ExternalIDs["vendor"] != util.CniTypeName
		})

		require.NoError(t, err)

		count := 0
		for _, ls := range lss {
			if strings.Contains(ls.Name, namePrefix) {
				count++
			}
		}
		require.Equal(t, count, 4)
	})
}

func (suite *OvnClientTestSuite) testLogicalSwitchUpdatePortOp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lsName := "test-update-port-op-ls"
	lspName := "test-update-port-op-lsp"
	lspUUID := ovsclient.NamedUUID()

	err := nbClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	err = nbClient.CreateBareLogicalSwitchPort(lsName, lspName, "unknown", "")
	require.NoError(t, err)

	lsp, err := nbClient.GetLogicalSwitchPort(lspName, false)
	require.NoError(t, err)
	require.NotNil(t, lsp)

	t.Run("del port from logical switch", func(t *testing.T) {
		t.Parallel()
		ops, err := nbClient.LogicalSwitchUpdatePortOp(lsName, lsp.UUID, ovsdb.MutateOperationDelete)
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

	t.Run("should return err when logical switch does not exist", func(t *testing.T) {
		t.Parallel()
		_, err := nbClient.LogicalSwitchUpdatePortOp("test-update-port-op-ls-non-existent", uuid.NewString(), ovsdb.MutateOperationInsert)
		require.ErrorContains(t, err, "not found logical switch")
	})

	t.Run("update port with empty lspUUID", func(t *testing.T) {
		ops, err := nbClient.LogicalSwitchUpdatePortOp(lsName, "", ovsdb.MutateOperationInsert)
		require.NoError(t, err)
		require.Nil(t, ops)
	})

	t.Run("delete port from non-existent logical switch", func(t *testing.T) {
		_, err := nbClient.LogicalSwitchUpdatePortOp("", ovsclient.NamedUUID(), ovsdb.MutateOperationDelete)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no LS found for LSP")
	})

	t.Run("delete port with multiple logical switches", func(t *testing.T) {
		lsName2 := "test-update-port-op-ls2"
		err := nbClient.CreateBareLogicalSwitch(lsName2)
		require.NoError(t, err)

		lspName := "test-lsp-multiple"
		err = nbClient.CreateBareLogicalSwitchPort(lsName, lspName, "unknown", "")
		require.NoError(t, err)

		lsp, err := nbClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)

		err = nbClient.LogicalSwitchAddPort(lsName2, lspName)
		require.NoError(t, err)

		_, err = nbClient.LogicalSwitchUpdatePortOp("", lsp.UUID, ovsdb.MutateOperationDelete)
		require.Error(t, err)
		require.Contains(t, err.Error(), "multiple LS found for LSP")
	})

	t.Run("insert port operation", func(t *testing.T) {
		ops, err := nbClient.LogicalSwitchUpdatePortOp(lsName, lspUUID, ovsdb.MutateOperationInsert)
		require.NoError(t, err)
		require.NotNil(t, ops)
		require.Len(t, ops, 1)
		require.Equal(t, ovsdb.MutateOperationInsert, ops[0].Mutations[0].Mutator)
	})
}

func (suite *OvnClientTestSuite) testLogicalSwitchUpdateLoadBalancerOp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lsName := "test-update-lb-ls"
	lbUUIDs := []string{ovsclient.NamedUUID(), ovsclient.NamedUUID(), ovsclient.NamedUUID()}

	err := nbClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	t.Run("add new lb to logical switch", func(t *testing.T) {
		t.Parallel()
		ops, err := nbClient.LogicalSwitchUpdateLoadBalancerOp(lsName, lbUUIDs, ovsdb.MutateOperationInsert)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "load_balancer",
				Mutator: ovsdb.MutateOperationInsert,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: lbUUIDs[0],
						},
						ovsdb.UUID{
							GoUUID: lbUUIDs[1],
						},
						ovsdb.UUID{
							GoUUID: lbUUIDs[2],
						},
					},
				},
			},
		}, ops[0].Mutations)
	})

	t.Run("del port from logical switch", func(t *testing.T) {
		t.Parallel()
		ops, err := nbClient.LogicalSwitchUpdateLoadBalancerOp(lsName, lbUUIDs, ovsdb.MutateOperationDelete)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "load_balancer",
				Mutator: ovsdb.MutateOperationDelete,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: lbUUIDs[0],
						},
						ovsdb.UUID{
							GoUUID: lbUUIDs[1],
						},
						ovsdb.UUID{
							GoUUID: lbUUIDs[2],
						},
					},
				},
			},
		}, ops[0].Mutations)
	})

	t.Run("should no err when lbUUIDs is empty", func(t *testing.T) {
		t.Parallel()
		_, err := nbClient.LogicalSwitchUpdateLoadBalancerOp("test-port-op-ls-non-existent", nil, ovsdb.MutateOperationInsert)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testLogicalSwitchUpdateACLOp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lsName := "test-update-acl-op-ls"
	aclUUIDs := []string{ovsclient.NamedUUID(), ovsclient.NamedUUID()}

	err := nbClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	t.Run("add new acl to logical switch", func(t *testing.T) {
		t.Parallel()

		ops, err := nbClient.logicalSwitchUpdateACLOp(lsName, aclUUIDs, ovsdb.MutateOperationInsert)
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

	t.Run("del acl from logical switch", func(t *testing.T) {
		t.Parallel()

		ops, err := nbClient.logicalSwitchUpdateACLOp(lsName, aclUUIDs, ovsdb.MutateOperationDelete)
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

	t.Run("should return err when logical switch does not exist", func(t *testing.T) {
		t.Parallel()

		_, err := nbClient.logicalSwitchUpdateACLOp("test-acl-op-ls-non-existent", aclUUIDs, ovsdb.MutateOperationInsert)
		require.ErrorContains(t, err, "not found logical switch")
	})
}

func (suite *OvnClientTestSuite) testLogicalSwitchOp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lsName := "test-op-ls"

	err := nbClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	lspUUID := ovsclient.NamedUUID()
	lspMutation := func(ls *ovnnb.LogicalSwitch) *model.Mutation {
		mutation := &model.Mutation{
			Field:   &ls.Ports,
			Value:   []string{lspUUID},
			Mutator: ovsdb.MutateOperationInsert,
		}

		return mutation
	}

	lbUUID := ovsclient.NamedUUID()
	lbMutation := func(ls *ovnnb.LogicalSwitch) *model.Mutation {
		mutation := &model.Mutation{
			Field:   &ls.LoadBalancer,
			Value:   []string{lbUUID},
			Mutator: ovsdb.MutateOperationInsert,
		}

		return mutation
	}

	ops, err := nbClient.LogicalSwitchOp(lsName, lspMutation, lbMutation)
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
			Column:  "load_balancer",
			Mutator: ovsdb.MutateOperationInsert,
			Value: ovsdb.OvsSet{
				GoSet: []interface{}{
					ovsdb.UUID{
						GoUUID: lbUUID,
					},
				},
			},
		},
	}, ops[0].Mutations)
}

func (suite *OvnClientTestSuite) testCreateBareLogicalSwitch() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lsName := "test-create-bare-ls"

	t.Run("create new logical switch", func(t *testing.T) {
		err := nbClient.CreateBareLogicalSwitch(lsName)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Equal(t, lsName, ls.Name)
		require.Equal(t, util.CniTypeName, ls.ExternalIDs["vendor"])
	})

	t.Run("create existing logical switch", func(t *testing.T) {
		err := nbClient.CreateBareLogicalSwitch(lsName)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Equal(t, lsName, ls.Name)
	})
}

func (suite *OvnClientTestSuite) testLogicalSwitchUpdateOtherConfig() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lsName := "test-update-other-config-ls"

	err := nbClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	t.Run("update other config with insert operation", func(t *testing.T) {
		otherConfig := map[string]string{"key1": "value1", "key2": "value2"}
		err := nbClient.LogicalSwitchUpdateOtherConfig(lsName, ovsdb.MutateOperationInsert, otherConfig)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Equal(t, otherConfig["key1"], ls.OtherConfig["key1"])
		require.Equal(t, otherConfig["key2"], ls.OtherConfig["key2"])
	})

	t.Run("update other config with delete operation", func(t *testing.T) {
		otherConfig := map[string]string{"key1": "value1"}
		err := nbClient.LogicalSwitchUpdateOtherConfig(lsName, ovsdb.MutateOperationDelete, otherConfig)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		_, exists := ls.OtherConfig["key1"]
		require.False(t, exists)
		require.Equal(t, "value2", ls.OtherConfig["key2"])
	})

	t.Run("update other config with empty map", func(t *testing.T) {
		err := nbClient.LogicalSwitchUpdateOtherConfig(lsName, ovsdb.MutateOperationInsert, map[string]string{})
		require.NoError(t, err)
	})

	t.Run("update other config for non-existent logical switch", func(t *testing.T) {
		otherConfig := map[string]string{"key3": "value3"}
		err := nbClient.LogicalSwitchUpdateOtherConfig("non-existent-ls", ovsdb.MutateOperationInsert, otherConfig)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found logical switch")
	})
}

func (suite *OvnClientTestSuite) testLogicalSwitchUpdateOtherConfigOp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lsName := "test-update-other-config-op-ls"

	err := nbClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	t.Run("empty other_config map", func(t *testing.T) {
		ops, err := nbClient.LogicalSwitchUpdateOtherConfigOp(lsName, map[string]string{}, ovsdb.MutateOperationInsert)
		require.NoError(t, err)
		require.Nil(t, ops)
	})
}
