package ovs

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (suite *OvnClientTestSuite) testCreateICLogicalRouterPort() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	azName := "az"
	chassises := []string{"5de32fcb-495a-40df-919e-f09812c4dffe", "25310674-65ce-41fd-bcfa-65b25268926b"}

	err := ovnClient.CreateLogicalRouter(ovnClient.ClusterRouter)
	require.NoError(t, err)

	err = ovnClient.CreateBareLogicalSwitch(util.InterconnectionSwitch)
	require.NoError(t, err)

	err = ovnClient.CreateICLogicalRouterPort(azName, "192.168.230.1/24", chassises)
	require.NoError(t, err)
}

func (suite *OvnClientTestSuite) testCreateRouterTypePort() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-create-router-type-ls"
	lrName := "test-create-router-type-lr"
	lspName := fmt.Sprintf("%s-%s", lsName, lrName)
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)
	chassises := []string{"5de32fcb-495a-40df-919e-f09812c4dffe", "25310674-65ce-41fd-bcfa-65b25268926b"}

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	t.Run("normal add router type port", func(t *testing.T) {
		err = ovnClient.CreateRouterTypePort(lsName, lspName, lrName, lrpName, "192.168.230.1/24,fc00::0af4:01/112", func(lrp *ovnnb.LogicalRouterPort) {
			if 0 != len(chassises) {
				lrp.GatewayChassis = chassises
			}
		})
		require.NoError(t, err)

		/* validate logical switch port*/
		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)
		require.Equal(t, []string{"router"}, lsp.Addresses)
		require.Equal(t, "router", lsp.Type)
		require.Equal(t, map[string]string{
			"router-port": lrpName,
		}, lsp.Options)

		ls, err := ovnClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)
		require.Contains(t, ls.Ports, lsp.UUID)

		/* validate logical router port*/
		lrp, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.Equal(t, []string{"192.168.230.1/24", "fc00::0af4:01/112"}, lrp.Networks)
		require.Equal(t, chassises, lrp.GatewayChassis)

		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Contains(t, lr.Ports, lrp.UUID)
	})

	t.Run("should no err when add router type port repeatedly", func(t *testing.T) {
		err = ovnClient.CreateRouterTypePort(lsName, lspName, lrName, lrpName, "192.168.230.1/24,fc00::0af4:01/112", func(lrp *ovnnb.LogicalRouterPort) {
			if 0 != len(chassises) {
				lrp.GatewayChassis = chassises
			}
		})
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testRemoveRouterTypePort() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-remove-router-type-ls"
	lrName := "test-remove-router-type-lr"
	lspName := fmt.Sprintf("%s-%s", lsName, lrName)
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	t.Run("normal del router type port", func(t *testing.T) {
		err = ovnClient.CreateRouterPort(lsName, lrName, "192.168.230.1/24,fc00::0af4:01/112")
		require.NoError(t, err)

		err = ovnClient.RemoveRouterTypePort(lspName, lrpName)
		require.NoError(t, err)

		/* validate logical switch port*/
		_, err = ovnClient.GetLogicalSwitchPort(lspName, false)
		require.ErrorContains(t, err, "object not found")

		/* validate logical router port*/
		_, err = ovnClient.GetLogicalRouterPort(lrpName, false)
		require.ErrorContains(t, err, "object not found")
	})

	t.Run("should no err normal del router type port repeatedly", func(t *testing.T) {
		err = ovnClient.RemoveRouterTypePort(lspName, lrpName)
		require.NoError(t, err)
	})
}
