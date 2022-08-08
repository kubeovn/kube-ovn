package ovs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

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
		err = ovnClient.CreateLogicalSwitch(lsName, lrName, "192.168.2.0/24,fd00::c0a8:6400/120", "192.168.2.1,fd00::c0a8:6401", true)
		require.NoError(t, err)

		_, err := ovnClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)

		_, err = ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)

		_, err = ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
	})

	t.Run("only update networks when logical switch exist and router type port exist and needRouter is true", func(t *testing.T) {
		err = ovnClient.CreateLogicalSwitch(lsName, lrName, "192.168.2.0/24,fd00::c0a8:9900/120", "192.168.2.1,fd00::c0a8:9901", true)
		require.NoError(t, err)

		lrp, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.Equal(t, []string{"192.168.2.1/24", "fd00::c0a8:9901/120"}, lrp.Networks)
	})

	t.Run("remove router type port when needRouter is false", func(t *testing.T) {
		err = ovnClient.CreateLogicalSwitch(lsName, lrName, "192.168.2.0/24,fd00::c0a8:9900/120", "192.168.2.1,fd00::c0a8:9901", false)
		require.NoError(t, err)

		_, err = ovnClient.GetLogicalSwitchPort(lspName, false)
		require.ErrorContains(t, err, "object not found")

		_, err = ovnClient.GetLogicalRouterPort(lrpName, false)
		require.ErrorContains(t, err, "object not found")
	})

	t.Run("should no err when router type port doest't exist", func(t *testing.T) {
		err = ovnClient.CreateLogicalSwitch(lsName+"-1", lrName+"-1", "192.168.2.0/24,fd00::c0a8:9900/120", "192.168.2.1,fd00::c0a8:9901", false)
		require.NoError(t, err)

		_, err = ovnClient.GetLogicalSwitchPort(lspName, false)
		require.ErrorContains(t, err, "object not found")

		_, err = ovnClient.GetLogicalRouterPort(lrpName, false)
		require.ErrorContains(t, err, "object not found")

		err = ovnClient.CreateLogicalSwitch(lsName+"-1", lrName+"-1", "192.168.2.0/24,fd00::c0a8:9900/120", "192.168.2.1,fd00::c0a8:9901", false)
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

	names := make([]string, 3)
	for i := 0; i < 3; i++ {
		names[i] = fmt.Sprintf("%s-%d", namePrefix, i)
		err := ovnClient.CreateBareLogicalSwitch(names[i])
		require.NoError(t, err)
	}

	t.Run("return all logical switch which match vendor", func(t *testing.T) {
		t.Parallel()
		lss, err := ovnClient.ListLogicalSwitch(true)
		require.NoError(t, err)
		require.NotEmpty(t, lss)

		for _, ls := range lss {
			if !strings.Contains(ls.Name, namePrefix) {
				continue
			}
			require.Contains(t, names, ls.Name)
		}
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
