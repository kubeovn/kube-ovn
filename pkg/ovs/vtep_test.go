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
	globalUUID := ovsclient.NamedUUID()
	portOps, err := vtepClient.Create(&vtep.PhysicalPort{
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
	// Physical_Switch / Physical_Port are not root tables; they must be
	// referenced from Global.switches or OVSDB garbage-collects them.
	globalOps, err := vtepClient.Create(&vtep.Global{
		UUID:     globalUUID,
		Switches: []string{psUUID},
	})
	require.NoError(t, err)
	ops := append(portOps, psOps...)
	ops = append(ops, globalOps...)
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

func (suite *OvnClientTestSuite) Test_RemoveVtepBindingSkipsForeignMapping() {
	suite.testRemoveVtepBindingSkipsForeignMapping()
}

func (suite *OvnClientTestSuite) testRemoveVtepBindingSkipsForeignMapping() {
	t := suite.T()
	t.Parallel()

	vtepClient := suite.vtepClient
	require.NotNil(t, vtepClient)

	psName := "nexus-ut-safe"
	portName := "Ethernet1/2"
	ownerLS := "owner-ls"
	foreignLS := "foreign-ls"
	vlanID := 200

	portUUID := ovsclient.NamedUUID()
	psUUID := ovsclient.NamedUUID()
	globalUUID := ovsclient.NamedUUID()
	portOps, err := vtepClient.Create(&vtep.PhysicalPort{
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
	globalOps, err := vtepClient.Create(&vtep.Global{
		UUID:     globalUUID,
		Switches: []string{psUUID},
	})
	require.NoError(t, err)
	ops := append(portOps, psOps...)
	ops = append(ops, globalOps...)
	require.NoError(t, vtepClient.Transact("vtep-fixture-safe", ops))

	require.NoError(t, vtepClient.EnsureVtepBinding(psName, portName, ownerLS, "owner-binding", vlanID))
	_, err = vtepClient.GetLogicalSwitch(ownerLS, false)
	require.NoError(t, err)

	// Simulate another CR overwriting vlan_bindings to a different Logical_Switch.
	foreign, err := vtepClient.EnsureLogicalSwitch(foreignLS, "foreign-binding")
	require.NoError(t, err)
	port, err := vtepClient.GetPhysicalPort(psName, portName, false)
	require.NoError(t, err)
	port.VLANBindings[vlanID] = foreign.UUID
	updateOps, err := vtepClient.Where(port).Update(port, &port.VLANBindings)
	require.NoError(t, err)
	require.NoError(t, vtepClient.Transact("vtep-overwrite", updateOps))

	// Owner cleanup must not clear the foreign mapping.
	require.NoError(t, vtepClient.RemoveVtepBinding(psName, portName, ownerLS, "owner-binding", vlanID))
	port, err = vtepClient.GetPhysicalPort(psName, portName, false)
	require.NoError(t, err)
	require.Equal(t, foreign.UUID, port.VLANBindings[vlanID])
}

func (suite *OvnClientTestSuite) Test_SharedLogicalSwitchDeleteAndGC() {
	suite.testSharedLogicalSwitchDeleteAndGC()
}

func (suite *OvnClientTestSuite) testSharedLogicalSwitchDeleteAndGC() {
	t := suite.T()
	t.Parallel()

	vtepClient := suite.vtepClient
	require.NotNil(t, vtepClient)

	psName := "nexus-ut-shared"
	portName := "Ethernet1/3"
	lsName := "shared-ls"
	vlanA, vlanB := 120, 121

	portUUID := ovsclient.NamedUUID()
	psUUID := ovsclient.NamedUUID()
	globalUUID := ovsclient.NamedUUID()
	portOps, err := vtepClient.Create(&vtep.PhysicalPort{
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
	globalOps, err := vtepClient.Create(&vtep.Global{
		UUID:     globalUUID,
		Switches: []string{psUUID},
	})
	require.NoError(t, err)
	ops := append(portOps, psOps...)
	ops = append(ops, globalOps...)
	require.NoError(t, vtepClient.Transact("vtep-fixture-shared", ops))

	require.NoError(t, vtepClient.EnsureVtepBinding(psName, portName, lsName, "binding-a", vlanA))
	require.NoError(t, vtepClient.EnsureVtepBinding(psName, portName, lsName, "binding-b", vlanB))

	require.NoError(t, vtepClient.RemoveVtepBinding(psName, portName, lsName, "binding-a", vlanA))
	ls, err := vtepClient.GetLogicalSwitch(lsName, true)
	require.NoError(t, err)
	require.NotNil(t, ls)

	require.NoError(t, vtepClient.RemoveVtepBinding(psName, portName, lsName, "binding-b", vlanB))
	ls, err = vtepClient.GetLogicalSwitch(lsName, true)
	require.NoError(t, err)
	require.Nil(t, ls)
}

func (suite *OvnClientTestSuite) Test_GCOrphanedVtepState() {
	suite.testGCOrphanedVtepState()
}

func (suite *OvnClientTestSuite) testGCOrphanedVtepState() {
	t := suite.T()
	t.Parallel()

	vtepClient := suite.vtepClient
	require.NotNil(t, vtepClient)

	psName := "nexus-ut-gc"
	portName := "Ethernet1/4"
	lsName := "gc-ls"
	vlanID := 130

	portUUID := ovsclient.NamedUUID()
	psUUID := ovsclient.NamedUUID()
	globalUUID := ovsclient.NamedUUID()
	portOps, err := vtepClient.Create(&vtep.PhysicalPort{
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
	globalOps, err := vtepClient.Create(&vtep.Global{
		UUID:     globalUUID,
		Switches: []string{psUUID},
	})
	require.NoError(t, err)
	ops := append(portOps, psOps...)
	ops = append(ops, globalOps...)
	require.NoError(t, vtepClient.Transact("vtep-fixture-gc", ops))

	require.NoError(t, vtepClient.EnsureVtepBinding(psName, portName, lsName, "gc-binding", vlanID))
	require.NoError(t, vtepClient.GCOrphanedVtepState(nil))

	port, err := vtepClient.GetPhysicalPort(psName, portName, false)
	require.NoError(t, err)
	_, ok := port.VLANBindings[vlanID]
	require.False(t, ok)
	ls, err := vtepClient.GetLogicalSwitch(lsName, true)
	require.NoError(t, err)
	require.Nil(t, ls)
}

func (suite *OvnClientTestSuite) testGetPortBindingByLogicalPort() {
	t := suite.T()
	t.Parallel()

	sbClient := suite.ovnSBClient
	logicalPort := "vtep.test-port-binding"

	encapUUID := ovsclient.NamedUUID()
	chassisUUID := ovsclient.NamedUUID()
	datapathUUID := ovsclient.NamedUUID()
	pbUUID := ovsclient.NamedUUID()
	up := true

	encapOps, err := sbClient.Create(&ovnsb.Encap{
		UUID:        encapUUID,
		Type:        ovnsb.EncapTypeVxlan,
		IP:          "192.0.2.10",
		ChassisName: "vtep-chassis-ut",
	})
	require.NoError(t, err)
	chassisOps, err := sbClient.Create(&ovnsb.Chassis{
		UUID:   chassisUUID,
		Name:   "vtep-chassis-ut",
		Encaps: []string{encapUUID},
	})
	require.NoError(t, err)
	datapathOps, err := sbClient.Create(&ovnsb.DatapathBinding{
		UUID:      datapathUUID,
		TunnelKey: 42,
	})
	require.NoError(t, err)
	pbOps, err := sbClient.Create(&ovnsb.PortBinding{
		UUID:        pbUUID,
		LogicalPort: logicalPort,
		Datapath:    datapathUUID,
		TunnelKey:   1,
		Chassis:     &chassisUUID,
		Up:          &up,
		Type:        "vtep",
	})
	require.NoError(t, err)
	ops := append(encapOps, chassisOps...)
	ops = append(ops, datapathOps...)
	ops = append(ops, pbOps...)
	require.NoError(t, sbClient.Transact("pb-add", ops))

	pb, err := sbClient.GetPortBindingByLogicalPort(logicalPort, false)
	require.NoError(t, err)
	require.Equal(t, logicalPort, pb.LogicalPort)
	require.True(t, IsPortBindingChassisBound(pb))

	pb, err = sbClient.GetPortBindingByLogicalPort("missing-lsp", true)
	require.NoError(t, err)
	require.Nil(t, pb)
}
