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
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func createLogicalSwitch(c *ovnClient, ls *ovnnb.LogicalSwitch) error {
	op, err := c.Create(ls)
	if err != nil {
		return err
	}

	if err := c.Transact("ls-add", op); err != nil {
		return err
	}

	return nil
}

func (suite *OvnClientTestSuite) testCreateLogicalSwitch() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-create-ls-ls"
	lrName := "test-create-ls-lr"
	lspName := fmt.Sprintf("%s-%s", lsName, lrName)
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("create logical switch and router type port when logical switch does't exist and needRouter is true", func(t *testing.T) {
		err = ovnClient.CreateLogicalSwitch(lsName, lrName, "192.168.2.0/24,fd00::c0a8:6400/120", "192.168.2.1,fd00::c0a8:6401", true, false)
		require.NoError(t, err)

		_, err := ovnClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)

		_, err = ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)

		_, err = ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
	})

	t.Run("only update networks when logical switch exist and router type port exist and needRouter is true", func(t *testing.T) {
		err = ovnClient.CreateLogicalSwitch(lsName, lrName, "192.168.2.0/24,fd00::c0a8:9900/120", "192.168.2.1,fd00::c0a8:9901", true, false)
		require.NoError(t, err)

		lrp, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"192.168.2.1/24", "fd00::c0a8:9901/120"}, lrp.Networks)
	})

	t.Run("remove router type port when needRouter is false", func(t *testing.T) {
		err = ovnClient.CreateLogicalSwitch(lsName, lrName, "192.168.2.0/24,fd00::c0a8:9900/120", "192.168.2.1,fd00::c0a8:9901", false, false)
		require.NoError(t, err)

		_, err = ovnClient.GetLogicalSwitchPort(lspName, false)
		require.ErrorContains(t, err, "object not found")

		_, err = ovnClient.GetLogicalRouterPort(lrpName, false)
		require.ErrorContains(t, err, "object not found")
	})

	t.Run("should no err when router type port doest't exist", func(t *testing.T) {
		err = ovnClient.CreateLogicalSwitch(lsName+"-1", lrName+"-1", "192.168.2.0/24,fd00::c0a8:9900/120", "192.168.2.1,fd00::c0a8:9901", false, false)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testLogicalSwitchAddPort() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-add-port-ls"
	lspName := "test-add-port-lsp"

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	err = ovnClient.CreateBareLogicalSwitchPort(lsName, lspName, "", "")
	require.NoError(t, err)

	lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
	require.NoError(t, err)

	t.Run("add port to logical switch", func(t *testing.T) {
		err = ovnClient.LogicalSwitchAddPort(lsName, lspName)
		require.NoError(t, err)

		ls, err := ovnClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Contains(t, ls.Ports, lsp.UUID)
	})

	t.Run("add port to logical switch repeatedly", func(t *testing.T) {
		err = ovnClient.LogicalSwitchAddPort(lsName, lspName)
		require.NoError(t, err)

		ls, err := ovnClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Contains(t, ls.Ports, lsp.UUID)
	})
}

func (suite *OvnClientTestSuite) testLogicalSwitchDelPort() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-del-port-ls"
	lspName := "test-del-port-lsp"

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	err = ovnClient.CreateBareLogicalSwitchPort(lsName, lspName, "", "")
	require.NoError(t, err)

	lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
	require.NoError(t, err)

	err = ovnClient.LogicalSwitchAddPort(lsName, lspName)
	require.NoError(t, err)

	t.Run("del port from logical switch", func(t *testing.T) {
		ls, err := ovnClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Contains(t, ls.Ports, lsp.UUID)

		err = ovnClient.LogicalSwitchDelPort(lsName, lspName)
		require.NoError(t, err)

		ls, err = ovnClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.NotContains(t, ls.Ports, lsp.UUID)
	})

	t.Run("del port from logical switch repeatedly", func(t *testing.T) {
		err = ovnClient.LogicalSwitchDelPort(lsName, lspName)
		require.NoError(t, err)

		ls, err := ovnClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.NotContains(t, ls.Ports, lsp.UUID)
	})
}

func (suite *OvnClientTestSuite) testLogicalSwitchUpdateLoadBalancers() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-add-lb-to-ls"
	prefix := "test-add-lb"
	lbNames := make([]string, 0, 3)

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	for i := 1; i <= 3; i++ {
		lbName := fmt.Sprintf("%s-%d", prefix, i)
		lbNames = append(lbNames, lbName)
		err := ovnClient.CreateLoadBalancer(lbName, "tcp", "")
		require.NoError(t, err)
	}

	t.Run("add lbs to logical switch", func(t *testing.T) {
		err = ovnClient.LogicalSwitchUpdateLoadBalancers(lsName, ovsdb.MutateOperationInsert, lbNames...)
		require.NoError(t, err)

		ls, err := ovnClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)

		for _, lbName := range lbNames {
			lb, err := ovnClient.GetLoadBalancer(lbName, false)
			require.NoError(t, err)
			require.Contains(t, ls.LoadBalancer, lb.UUID)
		}
	})

	t.Run("should no err when add non-existent lbs to logical switch", func(t *testing.T) {
		// add a non-existent lb
		err = ovnClient.LogicalSwitchUpdateLoadBalancers(lsName, ovsdb.MutateOperationInsert, "test-add-lb-non-existent")
		require.NoError(t, err)
	})

	t.Run("del lbs from logical switch", func(t *testing.T) {
		// delete the first two lbs from logical switch
		err = ovnClient.LogicalSwitchUpdateLoadBalancers(lsName, ovsdb.MutateOperationDelete, lbNames[0:2]...)
		require.NoError(t, err)

		ls, err := ovnClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)

		for i, lbName := range lbNames {
			lb, err := ovnClient.GetLoadBalancer(lbName, false)
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
		err = ovnClient.LogicalSwitchUpdateLoadBalancers(lsName, ovsdb.MutateOperationDelete, []string{"test-del-lb-non-existent", "test-del-lb-non-existent-1"}...)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testDeleteLogicalSwitch() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	name := "test-delete-ls"

	t.Run("no err when delete existent logical switch", func(t *testing.T) {
		t.Parallel()
		err := ovnClient.CreateBareLogicalSwitch(name)
		require.NoError(t, err)

		err = ovnClient.DeleteLogicalSwitch(name)
		require.NoError(t, err)

		_, err = ovnClient.GetLogicalSwitch(name, false)
		require.ErrorContains(t, err, "not found logical switch")
	})

	t.Run("no err when delete non-existent logical switch", func(t *testing.T) {
		t.Parallel()
		err := ovnClient.DeleteLogicalSwitch("test-delete-ls-non-existent")
		require.NoError(t, err)
	})
}

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

func (suite *OvnClientTestSuite) testListLogicalSwitch() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	namePrefix := "test-list-ls"

	i := 0
	// create three logical switch
	for ; i < 3; i++ {
		name := fmt.Sprintf("%s-%d", namePrefix, i)
		err := ovnClient.CreateBareLogicalSwitch(name)
		require.NoError(t, err)
	}

	// create two logical switch which vendor is others
	for ; i < 5; i++ {
		name := fmt.Sprintf("%s-%d", namePrefix, i)
		ls := &ovnnb.LogicalSwitch{
			Name:        name,
			ExternalIDs: map[string]string{"vendor": "test-vendor"},
		}

		err := createLogicalSwitch(ovnClient, ls)
		require.NoError(t, err)
	}

	// create two logical switch without vendor
	for ; i < 7; i++ {
		name := fmt.Sprintf("%s-%d", namePrefix, i)
		ls := &ovnnb.LogicalSwitch{
			Name: name,
		}

		err := createLogicalSwitch(ovnClient, ls)
		require.NoError(t, err)
	}

	t.Run("return all logical switch which match vendor", func(t *testing.T) {
		t.Parallel()
		lss, err := ovnClient.ListLogicalSwitch(true, nil)
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
		lss, err := ovnClient.ListLogicalSwitch(false, func(ls *ovnnb.LogicalSwitch) bool {
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

	ovnClient := suite.ovnClient
	lsName := "test-update-port-op-ls"
	lspUUID := ovsclient.NamedUUID()

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	t.Run("add new port to logical switch", func(t *testing.T) {
		t.Parallel()
		ops, err := ovnClient.LogicalSwitchUpdatePortOp(lsName, lspUUID, ovsdb.MutateOperationInsert)
		require.NoError(t, err)
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
		}, ops[0].Mutations)
	})

	t.Run("del port from logical switch", func(t *testing.T) {
		t.Parallel()
		ops, err := ovnClient.LogicalSwitchUpdatePortOp(lsName, lspUUID, ovsdb.MutateOperationDelete)
		require.NoError(t, err)
		require.Equal(t, []ovsdb.Mutation{
			{
				Column:  "ports",
				Mutator: ovsdb.MutateOperationDelete,
				Value: ovsdb.OvsSet{
					GoSet: []interface{}{
						ovsdb.UUID{
							GoUUID: lspUUID,
						},
					},
				},
			},
		}, ops[0].Mutations)
	})

	t.Run("should return err when logical switch does not exist", func(t *testing.T) {
		t.Parallel()
		_, err := ovnClient.LogicalSwitchUpdatePortOp("test-update-port-op-ls-non-existent", lspUUID, ovsdb.MutateOperationInsert)
		require.ErrorContains(t, err, "not found logical switch")
	})
}

func (suite *OvnClientTestSuite) testLogicalSwitchUpdateLoadBalancerOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-update-lb-ls"
	lbUUIDs := []string{ovsclient.NamedUUID(), ovsclient.NamedUUID(), ovsclient.NamedUUID()}

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	t.Run("add new lb to logical switch", func(t *testing.T) {
		t.Parallel()
		ops, err := ovnClient.LogicalSwitchUpdateLoadBalancerOp(lsName, lbUUIDs, ovsdb.MutateOperationInsert)
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
		ops, err := ovnClient.LogicalSwitchUpdateLoadBalancerOp(lsName, lbUUIDs, ovsdb.MutateOperationDelete)
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
		_, err := ovnClient.LogicalSwitchUpdateLoadBalancerOp("test-port-op-ls-non-existent", nil, ovsdb.MutateOperationInsert)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) test_logicalSwitchUpdateAclOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-update-acl-op-ls"
	aclUUIDs := []string{ovsclient.NamedUUID(), ovsclient.NamedUUID()}

	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	t.Run("add new acl to logical switch ", func(t *testing.T) {
		t.Parallel()

		ops, err := ovnClient.logicalSwitchUpdateAclOp(lsName, aclUUIDs, ovsdb.MutateOperationInsert)
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

		ops, err := ovnClient.logicalSwitchUpdateAclOp(lsName, aclUUIDs, ovsdb.MutateOperationDelete)
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

		_, err := ovnClient.logicalSwitchUpdateAclOp("test-acl-op-ls-non-existent", aclUUIDs, ovsdb.MutateOperationInsert)
		require.ErrorContains(t, err, "not found logical switch")
	})
}

func (suite *OvnClientTestSuite) testLogicalSwitchOp() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-op-ls"

	err := ovnClient.CreateBareLogicalSwitch(lsName)
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

	ops, err := ovnClient.LogicalSwitchOp(lsName, lspMutation, lbMutation)
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
