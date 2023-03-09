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

	lr, err := ovnClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)

	localnetLsp, err := ovnClient.GetLogicalSwitchPort(localnetLspName, false)
	require.NoError(t, err)
	require.Equal(t, "localnet", localnetLsp.Type)

	lrp, err := ovnClient.GetLogicalRouterPort(lrpName, false)
	require.NoError(t, err)
	require.Contains(t, lr.Ports, lrp.UUID)

	lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
	require.NoError(t, err)
	require.Contains(t, ls.Ports, lsp.UUID)
}

func (suite *OvnClientTestSuite) testCreateLogicalPatchPort() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-create-router-ls"
	lrName := "test-create-router-lr"
	lspName := fmt.Sprintf("%s-%s", lsName, lrName)
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)
	chassises := []string{"5de32fcb-495a-40df-919e-f09812c4dffe", "25310674-65ce-41fd-bcfa-65b25268926b"}

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	t.Run("create router port with chassises", func(t *testing.T) {
		t.Parallel()
		err := ovnClient.CreateLogicalPatchPort(lsName, lrName, lspName, lrpName, "192.168.230.1/24,fc00::0af4:01/112", util.GenerateMac(), chassises...)
		require.NoError(t, err)

		lrp, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"192.168.230.1/24", "fc00::0af4:01/112"}, lrp.Networks)

		for _, chassisName := range chassises {
			gwChassisName := lrpName + "-" + chassisName
			gwChassis, err := ovnClient.GetGatewayChassis(gwChassisName, false)
			require.NoError(t, err)
			require.Contains(t, lrp.GatewayChassis, gwChassis.UUID)
		}
	})

	t.Run("create router port with no chassises", func(t *testing.T) {
		t.Parallel()
		lsName := "test-create-ls-no-chassises"
		lrName := "test-create-lr-no-chassises"
		lspName := fmt.Sprintf("%s-%s", lsName, lrName)
		lrpName := fmt.Sprintf("%s-%s", lrName, lsName)

		err := ovnClient.CreateLogicalRouter(lrName)
		require.NoError(t, err)

		err = ovnClient.CreateBareLogicalSwitch(lsName)
		require.NoError(t, err)

		err = ovnClient.CreateLogicalPatchPort(lsName, lrName, lspName, lrpName, "192.168.230.1/24,fc00::0af4:01/112", util.GenerateMac())
		require.NoError(t, err)

		lrp, err := ovnClient.GetLogicalRouterPort(lrpName, false)
		require.NoError(t, err)
		require.Empty(t, lrp.GatewayChassis)
	})
}

func (suite *OvnClientTestSuite) testRemoveRouterPort() {
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
		err = ovnClient.CreateLogicalPatchPort(lsName, lrName, lspName, lrpName, "192.168.230.1/24,fc00::0af4:01/112", util.GenerateMac())
		require.NoError(t, err)

		err = ovnClient.RemoveLogicalPatchPort(lspName, lrpName)
		require.NoError(t, err)

		/* validate logical switch port*/
		_, err = ovnClient.GetLogicalSwitchPort(lspName, false)
		require.ErrorContains(t, err, "object not found")

		/* validate logical router port*/
		_, err = ovnClient.GetLogicalRouterPort(lrpName, false)
		require.ErrorContains(t, err, "object not found")
	})

	t.Run("should no err normal del router type port repeatedly", func(t *testing.T) {
		err = ovnClient.RemoveLogicalPatchPort(lspName, lrpName)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testDeleteLogicalGatewaySwitch() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lsName := "test-del-gw-ls"
	lrName := "test-del-gw-lr"
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = ovnClient.CreateGatewayLogicalSwitch(lsName, lrName, "test-external", "192.168.230.1/24,fc00::0af4:01/112", util.GenerateMac(), 210)
	require.NoError(t, err)

	// localnet port and lsp will be deleted when delete logical switch in real ovsdb,
	// it's different from the mock memory ovsdb,
	// so no need to check localnet port and lsp existence
	err = ovnClient.DeleteLogicalGatewaySwitch(lsName, lrName)
	require.NoError(t, err)

	_, err = ovnClient.GetLogicalSwitch(lsName, false)
	require.ErrorContains(t, err, "not found logical switch")

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

func (suite *OvnClientTestSuite) testGetEntityInfo() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient

	lsName := "test-get-entity-ls"
	err := ovnClient.CreateBareLogicalSwitch(lsName)
	require.NoError(t, err)

	lspName := "test-get-entity-lsp"
	err = ovnClient.CreateBareLogicalSwitchPort(lsName, lspName, "", "")
	require.NoError(t, err)

	t.Run("get logical switch by uuid", func(t *testing.T) {
		t.Parallel()

		ls, err := ovnClient.GetLogicalSwitch(lsName, false)
		require.NoError(t, err)

		newLs := &ovnnb.LogicalSwitch{UUID: ls.UUID}
		err = ovnClient.GetEntityInfo(newLs)
		require.NoError(t, err)
		require.Equal(t, lsName, newLs.Name)
	})

	t.Run("get logical switch by name which is not index", func(t *testing.T) {
		t.Parallel()

		ls := &ovnnb.LogicalSwitch{Name: lsName}
		err = ovnClient.GetEntityInfo(ls)
		require.ErrorContains(t, err, "object not found")
	})

	t.Run("get logical switch port by uuid", func(t *testing.T) {
		t.Parallel()

		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)

		newLsp := &ovnnb.LogicalSwitchPort{UUID: lsp.UUID}
		err = ovnClient.GetEntityInfo(newLsp)
		require.NoError(t, err)
		require.Equal(t, lspName, newLsp.Name)
	})

	t.Run("get logical switch port by name which is index", func(t *testing.T) {
		t.Parallel()

		lsp, err := ovnClient.GetLogicalSwitchPort(lspName, false)
		require.NoError(t, err)

		newLsp := &ovnnb.LogicalSwitchPort{Name: lspName}
		err = ovnClient.GetEntityInfo(newLsp)
		require.NoError(t, err)
		require.Equal(t, lsp.UUID, newLsp.UUID)
	})

	t.Run("entity is not a pointer", func(t *testing.T) {
		t.Parallel()

		newLsp := ovnnb.LogicalSwitchPort{Name: lspName}
		err = ovnClient.GetEntityInfo(newLsp)
		require.ErrorContains(t, err, "entity must be pointer")
	})
}
