package ovs

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (suite *OvnClientTestSuite) testCreateGatewayLogicalSwitch() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-create-gw-ls"
	lrName := "test-create-gw-lr"
	lspName := fmt.Sprintf("%s-%s", lsName, lrName)
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)
	localnetLspName := fmt.Sprintf("ln-%s", lsName)
	chassises := []string{"5de32fcb-495a-40df-919e-f09812c4d11e", "25310674-65ce-69fd-bcfa-65b25268926b"}

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = ovnClient.CreateGatewayLogicalSwitch(lsName, lrName, "test-external", "192.168.230.1/24,fc00::0af4:01/112", util.GenerateMac(), 210, chassises...)
	require.NoError(t, err)

	ls, err := ovnClient.GetLogicalSwitch(lsName, false)
	require.NoError(t, err)

	localnetLsp, err := ovnClient.GetLogicalSwitchPort(localnetLspName, false)
	require.NoError(t, err)
	require.Equal(t, "localnet", localnetLsp.Type)

	_, err = ovnClient.GetLogicalRouterPort(lrpName, false)
	require.NoError(t, err)

	lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
	require.NoError(t, err)
	require.Contains(t, ls.Ports, lsp.UUID)
}

func (suite *OvnClientTestSuite) testCreateRouterPort() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-create-router-ls"
	lrName := "test-create-router-lr"
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)
	chassises := []string{"5de32fcb-495a-40df-919e-f09812c4dffe", "25310674-65ce-41fd-bcfa-65b25268926b"}

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	t.Run("create router port with chassises", func(t *testing.T) {
		t.Parallel()
		err := ovnClient.CreateRouterPort(lsName, lrName, "192.168.230.1/24,fc00::0af4:01/112", util.GenerateMac(), chassises...)
		require.NoError(t, err)

		lrp, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.Equal(t, []string{"192.168.230.1/24", "fc00::0af4:01/112"}, lrp.Networks)

		for _, chassisName := range chassises {
			gwChassisName := lrpName + "-" + chassisName
			_, err := ovnClient.GetGatewayChassis(gwChassisName, false)
			require.NoError(t, err)
		}
	})

	t.Run("create router port with no chassises", func(t *testing.T) {
		t.Parallel()
		lsName := "test-create-ls-no-chassises"
		lrName := "test-create-lr-no-chassises"

		err := ovnClient.CreateLogicalRouter(lrName)
		require.NoError(t, err)

		err = ovnClient.CreateBareLogicalSwitch(lsName)
		require.NoError(t, err)

		err = ovnClient.CreateRouterPort(lsName, lrName, "192.168.230.1/24,fc00::0af4:01/112", util.GenerateMac())
		require.NoError(t, err)
	})

	t.Run("create router port with no ip", func(t *testing.T) {
		t.Parallel()
		lsName := "test-create-ls-no-ip"
		lrName := "test-create-lr-no-ip"
		lrpName := fmt.Sprintf("%s-%s", lrName, lsName)

		err := ovnClient.CreateLogicalRouter(lrName)
		require.NoError(t, err)

		err = ovnClient.CreateBareLogicalSwitch(lsName)
		require.NoError(t, err)

		err = ovnClient.CreateRouterPort(lsName, lrName, "", util.GenerateMac())
		require.NoError(t, err)

		lrp, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.Empty(t, lrp.Networks)
	})
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
		err = ovnClient.CreateRouterTypePort(lsName, lrName, util.GenerateMac(), func(lrp *ovnnb.LogicalRouterPort) {
			lrp.Networks = []string{"192.168.230.1/24", "fc00::0af4:01/112"}
			if len(chassises) != 0 {
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
		err = ovnClient.CreateRouterTypePort(lsName, lrName, util.GenerateMac(), func(lrp *ovnnb.LogicalRouterPort) {
			if len(chassises) != 0 {
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
		err = ovnClient.CreateRouterPort(lsName, lrName, "192.168.230.1/24,fc00::0af4:01/112", util.GenerateMac())
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

func (suite *OvnClientTestSuite) testDeleteLogicalGatewaySwitch() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-del-gw-ls"
	lrName := "test-del-gw-lr"
	lspName := fmt.Sprintf("%s-%s", lsName, lrName)
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)
	localnetLspName := fmt.Sprintf("ln-%s", lsName)

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = ovnClient.CreateGatewayLogicalSwitch(lsName, lrName, "test-external", "192.168.230.1/24,fc00::0af4:01/112", util.GenerateMac(), 210)
	require.NoError(t, err)

	err = ovnClient.DeleteLogicalGatewaySwitch(lsName, lrName)
	require.NoError(t, err)

	_, err = ovnClient.GetLogicalSwitch(lsName, false)
	require.ErrorContains(t, err, "not found logical switch")

	_, err = ovnClient.GetLogicalSwitchPort(localnetLspName, false)
	require.ErrorContains(t, err, "object not found")

	_, err = ovnClient.GetLogicalSwitchPort(lspName, false)
	require.ErrorContains(t, err, "object not found")

	_, err = ovnClient.GetLogicalRouterPort(lrpName, false)
	require.ErrorContains(t, err, "object not found")
}

func (suite *OvnClientTestSuite) testDeleteSecurityGroup() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	sgName := "test_del_sg"
	asName := "test_del_sg_as"
	pgName := GetSgPortGroupName(sgName)
	priority := "5111"
	match := "outport == @ovn.sg.test_del_sg && ip"

	/* prepate test */
	err := ovnClient.CreatePortGroup(pgName, map[string]string{
		"type": "security_group",
		sgKey:  sgName,
	})
	require.NoError(t, err)

	acl, err := ovnClient.newAcl(pgName, ovnnb.ACLDirectionToLport, priority, match, ovnnb.ACLActionAllowRelated)
	require.NoError(t, err)

	err = ovnClient.CreateAcls(pgName, portGroupKey, acl)
	require.NoError(t, err)

	err = ovnClient.CreateAddressSet(asName, map[string]string{
		sgKey: sgName,
	})
	require.NoError(t, err)

	/* run test */
	err = ovnClient.DeleteSecurityGroup(sgName)
	require.NoError(t, err)

	_, err = ovnClient.GetAcl(pgName, ovnnb.ACLDirectionToLport, priority, match, false)
	require.ErrorContains(t, err, "not found acl")

	_, err = ovnClient.GetAddressSet(asName, false)
	require.ErrorContains(t, err, "object not found")

	_, err = ovnClient.GetPortGroup(pgName, false)
	require.ErrorContains(t, err, "object not found")
}
