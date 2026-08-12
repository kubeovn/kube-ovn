package ovs

import (
	"github.com/stretchr/testify/require"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vtep"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (suite *OvnClientTestSuite) Test_EnsureVtepBinding() {
	suite.testEnsureVtepBinding()
}

func (suite *OvnClientTestSuite) Test_GetPortBindingByLogicalPort() {
	suite.testGetPortBindingByLogicalPort()
}

func (suite *OvnClientTestSuite) Test_IsPortBindingChassisBound() {
	t := suite.T()
	t.Parallel()

	require.False(t, IsPortBindingChassisBound(nil))
	require.False(t, IsPortBindingChassisBound(&ovnsb.PortBinding{}))

	chassis := "chassis-uuid"
	upFalse := false
	upTrue := true
	require.False(t, IsPortBindingChassisBound(&ovnsb.PortBinding{Chassis: &chassis, Up: &upFalse}))
	require.True(t, IsPortBindingChassisBound(&ovnsb.PortBinding{Chassis: &chassis}))
	require.True(t, IsPortBindingChassisBound(&ovnsb.PortBinding{Chassis: &chassis, Up: &upTrue}))
}

func (suite *OvnClientTestSuite) testEnsureVtepBinding() {
	t := suite.T()
	t.Parallel()

	vtepClient := suite.vtepClient
	require.NotNil(t, vtepClient)

	psName := "nexus-ut"
	portName := "Ethernet1/1"
	lsName := "tenant-ut-ls"
	bindingName := "binding-ut"
	vlanID := 120

	portUUID := ovsclient.NamedUUID()
	psUUID := ovsclient.NamedUUID()
	ops, err := vtepClient.Create(&vtep.PhysicalPort{
		UUID: portUUID,
		Name: portName,
	})
	require.NoError(t, err)
	psOps, err := vtepClient.Create(&vtep.PhysicalSwitch{
		UUID:  psUUID,
		Name:  psName,
		Ports: []string{portUUID},
	})
	require.NoError(t, err)
	ops = append(ops, psOps...)
	require.NoError(t, vtepClient.Transact("vtep-fixture", ops))

	err = vtepClient.EnsureVtepBinding(psName, portName, lsName, bindingName, vlanID)
	require.NoError(t, err)

	ls, err := vtepClient.GetLogicalSwitch(lsName, false)
	require.NoError(t, err)
	require.Equal(t, util.CniTypeName, ls.OtherConfig["vendor"])
	require.Equal(t, bindingName, ls.OtherConfig[VtepBindingKey])

	port, err := vtepClient.GetPhysicalPort(psName, portName, false)
	require.NoError(t, err)
	require.Equal(t, ls.UUID, port.VLANBindings[vlanID])

	// idempotent
	require.NoError(t, vtepClient.EnsureVtepBinding(psName, portName, lsName, bindingName, vlanID))

	require.NoError(t, vtepClient.RemoveVtepBinding(psName, portName, lsName, bindingName, vlanID))
	port, err = vtepClient.GetPhysicalPort(psName, portName, false)
	require.NoError(t, err)
	_, ok := port.VLANBindings[vlanID]
	require.False(t, ok)
	ls, err = vtepClient.GetLogicalSwitch(lsName, true)
	require.NoError(t, err)
	require.Nil(t, ls)

	err = vtepClient.EnsureVtepBinding(psName, "missing-port", lsName, bindingName, vlanID)
	require.Error(t, err)
}

func (suite *OvnClientTestSuite) testGetPortBindingByLogicalPort() {
	t := suite.T()
	t.Parallel()

	sbClient := suite.ovnSBClient
	logicalPort := "vtep.test-port-binding"

	chassisUUID := ovsclient.NamedUUID()
	pbUUID := ovsclient.NamedUUID()
	up := true
	ops, err := sbClient.Create(&ovnsb.PortBinding{
		UUID:        pbUUID,
		LogicalPort: logicalPort,
		Datapath:    ovsclient.NamedUUID(),
		TunnelKey:   1,
		Chassis:     &chassisUUID,
		Up:          &up,
		Type:        "vtep",
	})
	require.NoError(t, err)
	require.NoError(t, sbClient.Transact("pb-add", ops))

	pb, err := sbClient.GetPortBindingByLogicalPort(logicalPort, false)
	require.NoError(t, err)
	require.Equal(t, logicalPort, pb.LogicalPort)
	require.True(t, IsPortBindingChassisBound(pb))

	pb, err = sbClient.GetPortBindingByLogicalPort("missing-lsp", true)
	require.NoError(t, err)
	require.Nil(t, pb)
}
