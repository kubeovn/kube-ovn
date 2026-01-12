package ovs

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/go-logr/stdr"
	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/database/inmemory"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/ovn-kubernetes/libovsdb/ovsdb/serverdb"
	"github.com/ovn-kubernetes/libovsdb/server"
	"k8s.io/klog/v2"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
)

type OvnClientTestSuite struct {
	suite.Suite
	ovnNBClient *OVNNbClient
	ovnSBClient *OVNSbClient

	failedOvnNBClient *OVNNbClient
	ovnLegacyClient   *LegacyClient

	ovsSocket string
}

func emptyNbDatabaseModel() (model.ClientDBModel, error) {
	return model.NewClientDBModel(ovnnb.DatabaseName, nil)
}

func (suite *OvnClientTestSuite) SetupSuite() {
	fmt.Println("set up ovn client test suite")
	// setup ovn nb client schema
	nbClientSchema := ovnnb.Schema()

	// setup failed case ovn nb client
	emptyNbDBModel, err := emptyNbDatabaseModel()
	require.NoError(suite.T(), err)

	fakeNBServer, nbSock1 := newOVSDBServer(suite.T(), "fake-nb", emptyNbDBModel, nbClientSchema)
	nbEndpoint1 := "unix:" + nbSock1
	require.FileExists(suite.T(), nbSock1)
	failedOvnNBClient, err := newOvnNbClient(suite.T(), nbEndpoint1, 10)
	require.NoError(suite.T(), err)
	suite.failedOvnNBClient = failedOvnNBClient
	// close the server to simulate the failed case
	fakeNBServer.Close()
	require.NoFileExists(suite.T(), nbSock1)

	// setup ovn nb client
	nbClientDBModel, err := ovnnb.FullDatabaseModel()
	require.NoError(suite.T(), err)

	_, nbSock := newOVSDBServer(suite.T(), "nb", nbClientDBModel, nbClientSchema)
	nbEndpoint := "unix:" + nbSock
	require.FileExists(suite.T(), nbSock)

	ovnNBClient, err := newOvnNbClient(suite.T(), nbEndpoint, 10)
	require.NoError(suite.T(), err)
	suite.ovnNBClient = ovnNBClient

	// setup ovn sb client
	sbClientSchema := ovnsb.Schema()
	sbClientDBModel, err := ovnsb.FullDatabaseModel()
	require.NoError(suite.T(), err)

	_, sbSock := newOVSDBServer(suite.T(), "sb", sbClientDBModel, sbClientSchema)
	sbEndpoint := "unix:" + sbSock
	require.FileExists(suite.T(), sbSock)

	ovnSBClient, err := newOvnSbClient(suite.T(), sbEndpoint, 10)
	require.NoError(suite.T(), err)
	suite.ovnSBClient = ovnSBClient

	// setup ovn legacy client
	suite.ovnLegacyClient = newLegacyClient(10)

	// ovs-ctl ut use ovs-sandbox
	suite.ovsSocket = "--db=unix:/tmp/sandbox/db.sock"
}

// In order for 'go test' to run this suite, we need to create
// a normal test function and pass our suite to suite.Run
func TestOvnClientTestSuite(t *testing.T) {
	suite.Run(t, new(OvnClientTestSuite))
}

/* nb_global unit test */
func (suite *OvnClientTestSuite) Test_GetNbGlobal() {
	suite.testGetNbGlobal()
}

func (suite *OvnClientTestSuite) Test_UpdateNbGlobal() {
	suite.testUpdateNbGlobal()
}

func (suite *OvnClientTestSuite) Test_SetAzName() {
	suite.testSetAzName()
}

func (suite *OvnClientTestSuite) Test_SetICAutoRoute() {
	suite.testSetICAutoRoute()
}

func (suite *OvnClientTestSuite) Test_SetUseCtInvMatch() {
	suite.testSetUseCtInvMatch()
}

func (suite *OvnClientTestSuite) Test_SetLBCIDR() {
	suite.testSetLBCIDR()
}

func (suite *OvnClientTestSuite) Test_SetOVNIPSec() {
	suite.testSetOVNIPSec()
}

func (suite *OvnClientTestSuite) Test_SetNbGlobalOptions() {
	suite.testSetNbGlobalOptions()
}

func (suite *OvnClientTestSuite) Test_SetLsDnatModDlDst() {
	suite.testSetLsDnatModDlDst()
}

func (suite *OvnClientTestSuite) Test_SetLsCtSkipDstLportIPs() {
	suite.testSetLsCtSkipDstLportIPs()
}

func (suite *OvnClientTestSuite) Test_SetNodeLocalDNSIP() {
	suite.testSetNodeLocalDNSIP()
}

/* logical_switch unit test */
func (suite *OvnClientTestSuite) Test_CreateLogicalSwitch() {
	suite.testCreateLogicalSwitch()
}

func (suite *OvnClientTestSuite) Test_LogicalSwitchAddPort() {
	suite.testLogicalSwitchAddPort()
}

func (suite *OvnClientTestSuite) Test_LogicalSwitchDelPort() {
	suite.testLogicalSwitchDelPort()
}

func (suite *OvnClientTestSuite) Test_LogicalSwitchUpdateLoadBalancers() {
	suite.testLogicalSwitchUpdateLoadBalancers()
}

func (suite *OvnClientTestSuite) Test_DeleteLogicalSwitch() {
	suite.testDeleteLogicalSwitch()
}

func (suite *OvnClientTestSuite) Test_GetLogicalSwitch() {
	suite.testGetLogicalSwitch()
}

func (suite *OvnClientTestSuite) Test_ListLogicalSwitch() {
	suite.testListLogicalSwitch()
}

func (suite *OvnClientTestSuite) Test_LogicalSwitchUpdatePortOp() {
	suite.testLogicalSwitchUpdatePortOp()
}

func (suite *OvnClientTestSuite) Test_LogicalSwitchUpdateLoadBalancerOp() {
	suite.testLogicalSwitchUpdateLoadBalancerOp()
}

func (suite *OvnClientTestSuite) Test_LogicalSwitchUpdateAclOp() {
	suite.testLogicalSwitchUpdateACLOp()
}

func (suite *OvnClientTestSuite) Test_LogicalSwitchOp() {
	suite.testLogicalSwitchOp()
}

func (suite *OvnClientTestSuite) Test_CreateBareLogicalSwitch() {
	suite.testCreateBareLogicalSwitch()
}

func (suite *OvnClientTestSuite) Test_LogicalSwitchUpdateOtherConfig() {
	suite.testLogicalSwitchUpdateOtherConfig()
}

func (suite *OvnClientTestSuite) Test_LogicalSwitchUpdateOtherConfigOp() {
	suite.testLogicalSwitchUpdateOtherConfigOp()
}

/* logical_switch_port unit test */
func (suite *OvnClientTestSuite) Test_CreateLogicalSwitchPort() {
	suite.testCreateLogicalSwitchPort()
}

func (suite *OvnClientTestSuite) Test_CreateLocalnetLogicalSwitchPort() {
	suite.testCreateLocalnetLogicalSwitchPort()
}

func (suite *OvnClientTestSuite) Test_CreateVirtualLogicalSwitchPorts() {
	suite.testCreateVirtualLogicalSwitchPorts()
}

func (suite *OvnClientTestSuite) Test_CreateVirtualLogicalSwitchPort() {
	suite.testCreateVirtualLogicalSwitchPort()
}

func (suite *OvnClientTestSuite) Test_CreateBareLogicalSwitchPort() {
	suite.testCreateBareLogicalSwitchPort()
}

func (suite *OvnClientTestSuite) Test_SetLogicalSwitchPortVirtualParents() {
	suite.testSetLogicalSwitchPortVirtualParents()
}

func (suite *OvnClientTestSuite) Test_SetVirtualLogicalSwitchPortVirtualParents() {
	suite.testSetVirtualLogicalSwitchPortVirtualParents()
}

func (suite *OvnClientTestSuite) Test_SetLogicalSwitchPortArpProxy() {
	suite.testSetLogicalSwitchPortArpProxy()
}

func (suite *OvnClientTestSuite) Test_SetLogicalSwitchPortSecurity() {
	suite.testSetLogicalSwitchPortSecurity()
}

func (suite *OvnClientTestSuite) Test_SetSetLogicalSwitchPortExternalIds() {
	suite.testSetSetLogicalSwitchPortExternalIDs()
}

func (suite *OvnClientTestSuite) Test_SetLogicalSwitchPortSecurityGroup() {
	suite.testSetLogicalSwitchPortSecurityGroup()
}

func (suite *OvnClientTestSuite) Test_SetLogicalSwitchPortsSecurityGroup() {
	suite.testSetLogicalSwitchPortsSecurityGroup()
}

func (suite *OvnClientTestSuite) Test_EnablePortLayer2forward() {
	suite.testEnablePortLayer2forward()
}

func (suite *OvnClientTestSuite) Test_SetLogicalSwitchPortVlanTag() {
	suite.testSetLogicalSwitchPortVlanTag()
}

func (suite *OvnClientTestSuite) Test_UpdateLogicalSwitchPort() {
	suite.testUpdateLogicalSwitchPort()
}

func (suite *OvnClientTestSuite) Test_GetLogicalSwitchPortSgs() {
	suite.testGetLogicalSwitchPortSgs()
}

func (suite *OvnClientTestSuite) Test_GetLogicalSwitchPort() {
	suite.testGetLogicalSwitchPort()
}

func (suite *OvnClientTestSuite) Test_DeleteLogicalSwitchPort() {
	suite.testDeleteLogicalSwitchPort()
}

func (suite *OvnClientTestSuite) Test_DeleteLogicalSwitchPorts() {
	suite.testDeleteLogicalSwitchPorts()
}

func (suite *OvnClientTestSuite) Test_ListNormalLogicalSwitchPorts() {
	suite.testListNormalLogicalSwitchPorts()
}

func (suite *OvnClientTestSuite) Test_ListLogicalSwitchPorts() {
	suite.testListLogicalSwitchPorts()
}

func (suite *OvnClientTestSuite) Test_ListLogicalSwitchPortsWithLegacyExternalIDs() {
	suite.testListLogicalSwitchPortsWithLegacyExternalIDs()
}

func (suite *OvnClientTestSuite) Test_CreateLogicalSwitchPortOp() {
	suite.testCreateLogicalSwitchPortOp()
}

func (suite *OvnClientTestSuite) Test_DeleteLogicalSwitchPortOp() {
	suite.testDeleteLogicalSwitchPortOp()
}

func (suite *OvnClientTestSuite) Test_UpdateLogicalSwitchPortOp() {
	suite.testUpdateLogicalSwitchPortOp()
}

func (suite *OvnClientTestSuite) Test_LogicalSwitchPortFilter() {
	suite.testLogicalSwitchPortFilter()
}

func (suite *OvnClientTestSuite) Test_SetLogicalSwitchPortActivationStrategy() {
	suite.testSetLogicalSwitchPortActivationStrategy()
}

func (suite *OvnClientTestSuite) Test_SetLogicalSwitchPortMigrateOptions() {
	suite.testSetLogicalSwitchPortMigrateOptions()
}

func (suite *OvnClientTestSuite) Test_GetLogicalSwitchPortMigrateOptions() {
	suite.testGetLogicalSwitchPortMigrateOptions()
}

func (suite *OvnClientTestSuite) Test_ResetLogicalSwitchPortMigrateOptions() {
	suite.testResetLogicalSwitchPortMigrateOptions()
}

func (suite *OvnClientTestSuite) Test_testCleanLogicalSwitchPortMigrateOptions() {
	suite.testCleanLogicalSwitchPortMigrateOptions()
}

/* logical_router unit test */
func (suite *OvnClientTestSuite) Test_CreateLogicalRouter() {
	suite.testCreateLogicalRouter()
}

func (suite *OvnClientTestSuite) Test_UpdateLogicalRouter() {
	suite.testUpdateLogicalRouter()
}

func (suite *OvnClientTestSuite) Test_DeleteLogicalRouter() {
	suite.testDeleteLogicalRouter()
}

func (suite *OvnClientTestSuite) Test_GetLogicalRouter() {
	suite.testGetLogicalRouter()
}

func (suite *OvnClientTestSuite) Test_ListLogicalRouter() {
	suite.testListLogicalRouter()
}

func (suite *OvnClientTestSuite) Test_LogicalRouterUpdateLoadBalancers() {
	suite.testLogicalRouterUpdateLoadBalancers()
}

func (suite *OvnClientTestSuite) Test_LogicalRouterUpdatePortOp() {
	suite.testLogicalRouterUpdatePortOp()
}

func (suite *OvnClientTestSuite) Test_LogicalRouterUpdatePolicyOp() {
	suite.testLogicalRouterUpdatePolicyOp()
}

func (suite *OvnClientTestSuite) Test_LogicalRouterUpdateNatOp() {
	suite.testLogicalRouterUpdateNatOp()
}

func (suite *OvnClientTestSuite) Test_LogicalRouterUpdateStaticRouteOp() {
	suite.testLogicalRouterUpdateStaticRouteOp()
}

func (suite *OvnClientTestSuite) Test_LogicalRouterOp() {
	suite.testLogicalRouterOp()
}

/* logical_router_port unit test */
func (suite *OvnClientTestSuite) Test_CreatePeerRouterPort() {
	suite.testCreatePeerRouterPort()
}

func (suite *OvnClientTestSuite) Test_UpdateLogicalRouterPortRA() {
	suite.testUpdateLogicalRouterPortRA()
}

func (suite *OvnClientTestSuite) Test_UpdateLogicalRouterPortOptions() {
	suite.testUpdateLogicalRouterPortOptions()
}

func (suite *OvnClientTestSuite) Test_CreateLogicalRouterPort() {
	suite.testCreateLogicalRouterPort()
}

func (suite *OvnClientTestSuite) Test_UpdateLogicalRouterPort() {
	suite.testUpdateLogicalRouterPort()
}

func (suite *OvnClientTestSuite) Test_getLogicalRouterPort() {
	suite.testGetLogicalRouterPort()
}

func (suite *OvnClientTestSuite) Test_getLogicalRouterPortByUUID() {
	suite.testGetLogicalRouterPortByUUID()
}

func (suite *OvnClientTestSuite) Test_DeleteLogicalRouterPorts() {
	suite.testDeleteLogicalRouterPorts()
}

func (suite *OvnClientTestSuite) Test_DeleteLogicalRouterPort() {
	suite.testDeleteLogicalRouterPort()
}

func (suite *OvnClientTestSuite) Test_CreateLogicalRouterPortOp() {
	suite.testCreateLogicalRouterPortOp()
}

func (suite *OvnClientTestSuite) Test_DeleteLogicalRouterPortOp() {
	suite.testDeleteLogicalRouterPortOp()
}

func (suite *OvnClientTestSuite) Test_LogicalRouterPortOp() {
	suite.testLogicalRouterPortOp()
}

func (suite *OvnClientTestSuite) Test_LogicalRouterPortFilter() {
	suite.testLogicalRouterPortFilter()
}

func (suite *OvnClientTestSuite) Test_ListLogicalRouterPorts() {
	suite.testListLogicalRouterPorts()
}

func (suite *OvnClientTestSuite) Test_LogicalRouterPortUpdateGatewayChassisOp() {
	suite.testLogicalRouterPortUpdateGatewayChassisOp()
}

/* bfd unit test */
func (suite *OvnClientTestSuite) Test_CreateBFD() {
	suite.testCreateBFD()
}

func (suite *OvnClientTestSuite) Test_ListBFD() {
	suite.testListBFD()
}

func (suite *OvnClientTestSuite) Test_FindBFD() {
	suite.testFindBFD()
}

func (suite *OvnClientTestSuite) Test_DeleteBFD() {
	suite.testDeleteBFD()
}

func (suite *OvnClientTestSuite) Test_DeleteBFDByDstIP() {
	suite.testDeleteBFDByDstIP()
}

func (suite *OvnClientTestSuite) Test_ListDownBFDs() {
	suite.testListDownBFDs()
}

func (suite *OvnClientTestSuite) Test_ListUpBFDs() {
	suite.testListUpBFDs()
}

func (suite *OvnClientTestSuite) Test_isLrpBfdUp() {
	suite.testIsLrpBfdUp()
}

func (suite *OvnClientTestSuite) Test_BfdAddL3HAHandler() {
	suite.testBfdAddL3HAHandler()
}

func (suite *OvnClientTestSuite) Test_BfdUpdateL3HAHandler() {
	suite.testBfdUpdateL3HAHandler()
}

func (suite *OvnClientTestSuite) Test_BfdDelL3HAHandler() {
	suite.testBfdDelL3HAHandler()
}

func (suite *OvnClientTestSuite) Test_MonitorBFDs() {
	suite.testMonitorBFDs()
}

/* gateway_chassis unit test */
func (suite *OvnClientTestSuite) Test_CreateGatewayChassises() {
	suite.testCreateGatewayChassises()
}

func (suite *OvnClientTestSuite) Test_UpdateGatewayChassis() {
	suite.testUpdateGatewayChassis()
}

func (suite *OvnClientTestSuite) Test_DeleteGatewayChassises() {
	suite.testDeleteGatewayChassises()
}

func (suite *OvnClientTestSuite) Test_DeleteGatewayChassisOp() {
	suite.testDeleteGatewayChassisOp()
}

func (suite *OvnClientTestSuite) Test_NewGatewayChassis() {
	suite.testNewGatewayChassis()
}

/* ha_chassis_group unit test */
func (suite *OvnClientTestSuite) Test_CreateHAChassisGroup() {
	suite.testCreateHAChassisGroup()
}

func (suite *OvnClientTestSuite) Test_GetHAChassisGroup() {
	suite.testGetHAChassisGroup()
}

func (suite *OvnClientTestSuite) Test_DeleteHAChassisGroup() {
	suite.testDeleteHAChassisGroup()
}

/* load_balancer unit test */
func (suite *OvnClientTestSuite) Test_CreateLoadBalancer() {
	suite.testCreateLoadBalancer()
}

func (suite *OvnClientTestSuite) Test_UpdateLoadBalancer() {
	suite.testUpdateLoadBalancer()
}

func (suite *OvnClientTestSuite) Test_LoadBalancerAddHealthCheck() {
	suite.testLoadBalancerAddHealthCheck()
}

func (suite *OvnClientTestSuite) Test_DeleteLoadBalancers() {
	suite.testDeleteLoadBalancers()
}

func (suite *OvnClientTestSuite) Test_DeleteLoadBalancer() {
	suite.testDeleteLoadBalancer()
}

func (suite *OvnClientTestSuite) Test_LoadBalancerDeleteVip() {
	suite.testLoadBalancerDeleteVip()
}

func (suite *OvnClientTestSuite) Test_GetLoadBalancer() {
	suite.testGetLoadBalancer()
}

func (suite *OvnClientTestSuite) Test_ListLoadBalancers() {
	suite.testListLoadBalancers()
}

func (suite *OvnClientTestSuite) Test_LoadBalancerAddVip() {
	suite.testLoadBalancerAddVip()
}

func (suite *OvnClientTestSuite) Test_DeleteLoadBalancerOp() {
	suite.testDeleteLoadBalancerOp()
}

func (suite *OvnClientTestSuite) Test_SetLoadBalancerAffinityTimeout() {
	suite.testSetLoadBalancerAffinityTimeout()
}

func (suite *OvnClientTestSuite) Test_LoadBalancerAddIPPortMapping() {
	suite.testLoadBalancerAddIPPortMapping()
}

func (suite *OvnClientTestSuite) Test_LoadBalancerDeleteIPPortMapping() {
	suite.testLoadBalancerDeleteIPPortMapping()
}

func (suite *OvnClientTestSuite) Test_LoadBalancerWithHealthCheck() {
	suite.testLoadBalancerWithHealthCheck()
}

func (suite *OvnClientTestSuite) Test_LoadBalancerOp() {
	suite.testLoadBalancerOp()
}

func (suite *OvnClientTestSuite) Test_LoadBalancerUpdateHealthCheckOp() {
	suite.testLoadBalancerUpdateHealthCheckOp()
}

/* load_balancer health check unit test */
func (suite *OvnClientTestSuite) Test_CreateLoadBalancerHealthCheck() {
	suite.testAddLoadBalancerHealthCheck()
}

func (suite *OvnClientTestSuite) Test_NewLoadBalancerHealthCheck() {
	suite.testNewLoadBalancerHealthCheck()
}

func (suite *OvnClientTestSuite) Test_UpdateLoadBalancerHealthCheck() {
	suite.testUpdateLoadBalancerHealthCheck()
}

func (suite *OvnClientTestSuite) Test_DeleteLoadBalancerHealthCheck() {
	suite.testDeleteLoadBalancerHealthCheck()
}

func (suite *OvnClientTestSuite) Test_DeleteLoadBalancerHealthChecks() {
	suite.testDeleteLoadBalancerHealthChecks()
}

func (suite *OvnClientTestSuite) Test_GetLoadBalancerHealthCheck() {
	suite.testGetLoadBalancerHealthCheck()
}

func (suite *OvnClientTestSuite) Test_ListLoadBalancerHealthChecks() {
	suite.testListLoadBalancerHealthChecks()
}

func (suite *OvnClientTestSuite) Test_DeleteLoadBalancerHealthCheckOp() {
	suite.testDeleteLoadBalancerHealthCheckOp()
}

/* port_group unit test */
func (suite *OvnClientTestSuite) Test_CreatePortGroup() {
	suite.testCreatePortGroup()
}

func (suite *OvnClientTestSuite) Test_PortGroupResetPorts() {
	suite.testPortGroupResetPorts()
}

func (suite *OvnClientTestSuite) Test_PortGroupUpdatePorts() {
	suite.testPortGroupUpdatePorts()
}

func (suite *OvnClientTestSuite) Test_DeletePortGroup() {
	suite.testDeletePortGroup()
}

func (suite *OvnClientTestSuite) Test_GetGetPortGroup() {
	suite.testGetGetPortGroup()
}

func (suite *OvnClientTestSuite) Test_ListPortGroups() {
	suite.testListPortGroups()
}

func (suite *OvnClientTestSuite) Test_portGroupUpdatePortOp() {
	suite.testPortGroupUpdatePortOp()
}

func (suite *OvnClientTestSuite) Test_portGroupUpdateAclOp() {
	suite.testPortGroupUpdateACLOp()
}

func (suite *OvnClientTestSuite) Test_portGroupOp() {
	suite.testPortGroupOp()
}

func (suite *OvnClientTestSuite) Test_portGroupRemovePorts() {
	suite.testPortGroupRemovePorts()
}

func (suite *OvnClientTestSuite) Test_updatePortGroup() {
	suite.testUpdatePortGroup()
}

func (suite *OvnClientTestSuite) Test_portGroupSetPorts() {
	suite.testPortGroupSetPorts()
}

func (suite *OvnClientTestSuite) Test_removePortFromPortGroups() {
	suite.testRemovePortFromPortGroups()
}

/* address_set unit test */
func (suite *OvnClientTestSuite) Test_CreateAddressSet() {
	suite.testCreateAddressSet()
}

func (suite *OvnClientTestSuite) Test_AddressSetUpdateAddress() {
	suite.testAddressSetUpdateAddress()
}

func (suite *OvnClientTestSuite) Test_DeleteAddressSet() {
	suite.testDeleteAddressSet()
}

func (suite *OvnClientTestSuite) Test_DeleteAddressSets() {
	suite.testDeleteAddressSets()
}

func (suite *OvnClientTestSuite) Test_ListAddressSets() {
	suite.testListAddressSets()
}

func (suite *OvnClientTestSuite) Test_addressSetFilter() {
	suite.testAddressSetFilter()
}

func (suite *OvnClientTestSuite) Test_UpdateAddressSet() {
	suite.testUpdateAddressSet()
}

func (suite *OvnClientTestSuite) Test_BatchDeleteAddressSetByNames() {
	suite.testBatchDeleteAddressSetByNames()
}

/* acl unit test */
func (suite *OvnClientTestSuite) Test_testUpdateDefaultBlockAclOps() {
	suite.testUpdateDefaultBlockACLOps()
}

func (suite *OvnClientTestSuite) Test_testUpdateDefaultBlockExceptionsACLOps() {
	suite.testUpdateDefaultBlockExceptionsACLOps()
}

func (suite *OvnClientTestSuite) Test_testUpdateIngressAclOps() {
	suite.testUpdateIngressACLOps()
}

func (suite *OvnClientTestSuite) Test_UpdateEgressAclOps() {
	suite.testUpdateEgressACLOps()
}

func (suite *OvnClientTestSuite) Test_CreateGatewayAcl() {
	suite.testCreateGatewayACL()
}

func (suite *OvnClientTestSuite) Test_CreateNodeAcl() {
	suite.testCreateNodeACL()
}

func (suite *OvnClientTestSuite) Test_CreateSgDenyAllAcl() {
	suite.testCreateSgDenyAllACL()
}

func (suite *OvnClientTestSuite) Test_CreateSgBaseACL() {
	suite.testCreateSgBaseACL()
}

func (suite *OvnClientTestSuite) Test_UpdateSgAcl() {
	suite.testUpdateSgACL()
}

func (suite *OvnClientTestSuite) Test_UpdateLogicalSwitchAcl() {
	suite.testUpdateLogicalSwitchACL()
}

func (suite *OvnClientTestSuite) Test_SetAclLog() {
	suite.testSetACLLog()
}

func (suite *OvnClientTestSuite) Test_SetLogicalSwitchPrivate() {
	suite.testSetLogicalSwitchPrivate()
}

func (suite *OvnClientTestSuite) Test_newSgRuleACL() {
	suite.testNewSgRuleACL()
}

func (suite *OvnClientTestSuite) Test_CreateAcls() {
	suite.testCreateAcls()
}

func (suite *OvnClientTestSuite) Test_DeleteAcls() {
	suite.testDeleteAcls()
}

func (suite *OvnClientTestSuite) Test_DeleteAcl() {
	suite.testDeleteACL()
}

func (suite *OvnClientTestSuite) Test_GetAcl() {
	suite.testGetACL()
}

func (suite *OvnClientTestSuite) Test_ListAcls() {
	suite.testListAcls()
}

func (suite *OvnClientTestSuite) Test_newAcl() {
	suite.testNewACL()
}

func (suite *OvnClientTestSuite) Test_newACLWithoutCheck() {
	suite.testNewACLWithoutCheck()
}

func (suite *OvnClientTestSuite) Test_newNetworkPolicyAclMatch() {
	suite.testnewNetworkPolicyACLMatch()
}

func (suite *OvnClientTestSuite) Test_aclFilter() {
	suite.testACLFilter()
}

func (suite *OvnClientTestSuite) Test_createAclsOps() {
	suite.testCreateAclsOps()
}

func (suite *OvnClientTestSuite) Test_sgRuleNoACL() {
	suite.testSgRuleNoACL()
}

func (suite *OvnClientTestSuite) Test_SGLostACL() {
	suite.testSGLostACL()
}

func (suite *OvnClientTestSuite) Test_newAnpACLMatch() {
	suite.testNewAnpACLMatch()
}

func (suite *OvnClientTestSuite) Test_CreateBareACL() {
	suite.testCreateBareACL()
}

func (suite *OvnClientTestSuite) Test_UpdateAnpRuleACLOps() {
	suite.testUpdateAnpRuleACLOps()
}

func (suite *OvnClientTestSuite) Test_UpdateCnpRuleACLOps() {
	suite.testUpdateCnpRuleACLOps()
}

func (suite *OvnClientTestSuite) Test_UpdateACL() {
	suite.testUpdateACL()
}

/* logical_router_policy unit test */
func (suite *OvnClientTestSuite) Test_AddLogicalRouterPolicy() {
	suite.testAddLogicalRouterPolicy()
}

func (suite *OvnClientTestSuite) Test_CreateLogicalRouterPolicies() {
	suite.testCreateLogicalRouterPolicies()
}

func (suite *OvnClientTestSuite) Test_DeleteLogicalRouterPolicy() {
	suite.testDeleteLogicalRouterPolicy()
}

func (suite *OvnClientTestSuite) Test_DeleteLogicalRouterPolicies() {
	suite.testDeleteLogicalRouterPolicies()
}

func (suite *OvnClientTestSuite) Test_DeleteLogicalRouterPoliciesByNexthop() {
	suite.testDeleteLogicalRouterPolicyByNexthop()
}

func (suite *OvnClientTestSuite) Test_DeleteRouterPolicy() {
	suite.testDeleteRouterPolicy()
}

func (suite *OvnClientTestSuite) Test_ClearLogicalRouterPolicy() {
	suite.testClearLogicalRouterPolicy()
}

func (suite *OvnClientTestSuite) Test_GetLogicalRouterPolicy() {
	suite.testGetLogicalRouterPolicy()
}

func (suite *OvnClientTestSuite) Test_GetLogicalRouterPolicyByUUID() {
	suite.testGetLogicalRouterPolicyByUUID()
}

func (suite *OvnClientTestSuite) Test_GetLogicalRouterPolicyByExtID() {
	suite.testGetLogicalRouterPolicyByExtID()
}

func (suite *OvnClientTestSuite) Test_NewLogicalRouterPolicy() {
	suite.testNewLogicalRouterPolicy()
}

func (suite *OvnClientTestSuite) Test_UpdateLogicalRouterPolicy() {
	suite.testUpdateLogicalRouterPolicy()
}

func (suite *OvnClientTestSuite) Test_PolicyFilter() {
	suite.testPolicyFilter()
}

func (suite *OvnClientTestSuite) Test_BatchAddLogicalRouterPolicy() {
	suite.testBatchAddLogicalRouterPolicy()
}

func (suite *OvnClientTestSuite) Test_BatchDeleteLogicalRouterPolicyByUUID() {
	suite.testBatchDeleteLogicalRouterPolicyByUUID()
}

func (suite *OvnClientTestSuite) Test_BatchDeleteLogicalRouterPolicy() {
	suite.testBatchDeleteLogicalRouterPolicy()
}

/* nat unit test */
func (suite *OvnClientTestSuite) Test_CreateNats() {
	suite.testCreateNats()
}

func (suite *OvnClientTestSuite) Test_UpdateSnat() {
	suite.testUpdateSnat()
}

func (suite *OvnClientTestSuite) Test_UpdateDnatAndSnat() {
	suite.testUpdateDnatAndSnat()
}

func (suite *OvnClientTestSuite) Test_UpdateNat() {
	suite.testUpdateNat()
}

func (suite *OvnClientTestSuite) Test_DeleteNats() {
	suite.testDeleteNats()
}

func (suite *OvnClientTestSuite) Test_DeleteNat() {
	suite.testDeleteNat()
}

func (suite *OvnClientTestSuite) Test_GetNat() {
	suite.testGetNat()
}

func (suite *OvnClientTestSuite) Test_NewNat() {
	suite.testNewNat()
}

func (suite *OvnClientTestSuite) Test_NatFilter() {
	suite.testNatFilter()
}

func (suite *OvnClientTestSuite) Test_AddNat() {
	suite.testAddNat()
}

func (suite *OvnClientTestSuite) Test_GetNATByUUID() {
	suite.testGetNATByUUID()
}

func (suite *OvnClientTestSuite) Test_GetNatValidations() {
	suite.testGetNatValidations()
}

/* logical_router_static_route unit test */
func (suite *OvnClientTestSuite) Test_CreateLogicalRouterStaticRoutes() {
	suite.testCreateLogicalRouterStaticRoutes()
}

func (suite *OvnClientTestSuite) Test_AddLogicalRouterStaticRoute() {
	suite.testAddLogicalRouterStaticRoute()
}

func (suite *OvnClientTestSuite) TestDeleteLogicalRouterStaticRouteByUUID() {
	suite.testDeleteLogicalRouterStaticRouteByUUID()
}

func (suite *OvnClientTestSuite) TestDeleteLogicalRouterStaticRouteByExternalIDs() {
	suite.testDeleteLogicalRouterStaticRouteByExternalIDs()
}

func (suite *OvnClientTestSuite) Test_DeleteLogicalRouterStaticRoute() {
	suite.testDeleteLogicalRouterStaticRoute()
}

func (suite *OvnClientTestSuite) Test_ClearLogicalRouterStaticRoute() {
	suite.testClearLogicalRouterStaticRoute()
}

func (suite *OvnClientTestSuite) Test_GetLogicalRouterStaticRoute() {
	suite.testGetLogicalRouterStaticRoute()
}

func (suite *OvnClientTestSuite) Test_ListLogicalRouterStaticRoutes() {
	suite.testListLogicalRouterStaticRoutes()
}

func (suite *OvnClientTestSuite) Test_newLogicalRouterStaticRoute() {
	suite.testNewLogicalRouterStaticRoute()
}

func (suite *OvnClientTestSuite) Test_ListLogicalRouterStaticRoutesByOption() {
	suite.testListLogicalRouterStaticRoutesByOption()
}

func (suite *OvnClientTestSuite) Test_UpdateLogicalRouterStaticRoute() {
	suite.testUpdateLogicalRouterStaticRoute()
}

func (suite *OvnClientTestSuite) Test_GetLogicalRouterStaticRouteEdgeCases() {
	suite.testGetLogicalRouterStaticRouteEdgeCases()
}

func (suite *OvnClientTestSuite) Test_tBatchDeleteLogicalRouterStaticRoute() {
	suite.testBatchDeleteLogicalRouterStaticRoute()
}

/* dhcp options unit test */
func (suite *OvnClientTestSuite) Test_UpdateDHCPOptions() {
	suite.testUpdateDHCPOptions()
}

func (suite *OvnClientTestSuite) Test_updateDHCPv4Options() {
	suite.testUpdateDHCPv4Options()
}

func (suite *OvnClientTestSuite) Test_updateDHCPv6Options() {
	suite.testUpdateDHCPv6Options()
}

func (suite *OvnClientTestSuite) Test_DeleteDHCPOptionsByUUIDs() {
	suite.testDeleteDHCPOptionsByUUIDs()
}

func (suite *OvnClientTestSuite) Test_DeleteDHCPOptions() {
	suite.testDeleteDHCPOptions()
}

func (suite *OvnClientTestSuite) Test_GetDHCPOptions() {
	suite.testGetDHCPOptions()
}

func (suite *OvnClientTestSuite) Test_ListDHCPOptions() {
	suite.testListDHCPOptions()
}

func (suite *OvnClientTestSuite) Test_dhcpOptionsFilter() {
	suite.testDhcpOptionsFilter()
}

func (suite *OvnClientTestSuite) Test_CreateDHCPOptions() {
	suite.testCreateDHCPOptions()
}

/* mixed operations unit test */
func (suite *OvnClientTestSuite) Test_CreateGatewayLogicalSwitch() {
	suite.testCreateGatewayLogicalSwitch()
}

func (suite *OvnClientTestSuite) Test_CreateLogicalPatchPort() {
	suite.testCreateLogicalPatchPort()
}

func (suite *OvnClientTestSuite) Test_RemoveRouterPort() {
	suite.testRemoveRouterPort()
}

func (suite *OvnClientTestSuite) Test_DeleteLogicalGatewaySwitch() {
	suite.testDeleteLogicalGatewaySwitch()
}

func (suite *OvnClientTestSuite) Test_DeleteSecurityGroup() {
	suite.testDeleteSecurityGroup()
}

func (suite *OvnClientTestSuite) Test_GetEntityInfo() {
	suite.testGetEntityInfo()
}

func (suite *OvnClientTestSuite) Test_NewOvnNbClient() {
	suite.testNewOvnNbClient()
}

func (suite *OvnClientTestSuite) Test_NewOvnSbClient() {
	suite.testNewOvnSbClient()
}

/* migration unit test */
func (suite *OvnClientTestSuite) Test_MigrateVendorExternalIDs() {
	suite.testMigrateVendorExternalIDs()
}

func (suite *OvnClientTestSuite) Test_MigrateVendorExternalIDsIdempotent() {
	suite.testMigrateVendorExternalIDsIdempotent()
}

func (suite *OvnClientTestSuite) Test_MigrateSkipsWhenVersionSet() {
	suite.testMigrateSkipsWhenVersionSet()
}

func (suite *OvnClientTestSuite) Test_MigrateRunsWhenOldVersion() {
	suite.testMigrateRunsWhenOldVersion()
}

func (suite *OvnClientTestSuite) Test_MigrateVendorExternalIDsSkipsNonKubeOvn() {
	suite.testMigrateVendorExternalIDsSkipsNonKubeOvn()
}

/* sb chassis unit test */
func (suite *OvnClientTestSuite) Test_GetChassis() {
	suite.testGetChassis()
}

func (suite *OvnClientTestSuite) Test_DeleteChassis() {
	suite.testDeleteChassis()
}

func (suite *OvnClientTestSuite) Test_UpdateChassis() {
	suite.testUpdateChassis()
}

func (suite *OvnClientTestSuite) Test_ListChassis() {
	suite.testListChassis()
}

func (suite *OvnClientTestSuite) Test_GetChassisByHost() {
	suite.testGetChassisByHost()
}

func (suite *OvnClientTestSuite) Test_DeleteChassisByHost() {
	suite.testDeleteChassisByHost()
}

func (suite *OvnClientTestSuite) Test_UpdateChassisTag() {
	suite.testUpdateChassisTag()
}

func (suite *OvnClientTestSuite) Test_GetKubeOvnChassisses() {
	suite.testGetKubeOvnChassisses()
}

/* datapath_binding unit test */
func (suite *OvnClientTestSuite) Test_GetLogicalSwitchTunnelKey() {
	suite.testGetLogicalSwitchTunnelKey()
}

// ovn ic
func (suite *OvnClientTestSuite) Test_OvnIcNbCommand() {
	suite.testOvnIcNbCommand()
}

func (suite *OvnClientTestSuite) Test_OvnIcSbCommand() {
	suite.testOvnIcSbCommand()
}

func (suite *OvnClientTestSuite) Test_GetTsSubnet() {
	suite.testGetTsSubnet()
}

func (suite *OvnClientTestSuite) Test_GetTs() {
	suite.testGetTs()
}

func (suite *OvnClientTestSuite) Test_FindUUIDWithAttrInTable() {
	suite.testFindUUIDWithAttrInTable()
}

func (suite *OvnClientTestSuite) Test_DestroyTableWithUUID() {
	suite.testDestroyTableWithUUID()
}

func (suite *OvnClientTestSuite) Test_GetAzUUID() {
	suite.testGetAzUUID()
}

func (suite *OvnClientTestSuite) Test_GetGatewayUUIDsInOneAZ() {
	suite.testGetGatewayUUIDsInOneAZ()
}

func (suite *OvnClientTestSuite) Test_GetRouteUUIDsInOneAZ() {
	suite.testGetRouteUUIDsInOneAZ()
}

func (suite *OvnClientTestSuite) Test_GetPortBindingUUIDsInOneAZ() {
	suite.testGetPortBindingUUIDsInOneAZ()
}

func (suite *OvnClientTestSuite) Test_DestroyGateways() {
	suite.testDestroyGateways()
}

func (suite *OvnClientTestSuite) Test_DestroyRoutes() {
	suite.testDestroyRoutes()
}

func (suite *OvnClientTestSuite) Test_DestroyPortBindings() {
	suite.testDestroyPortBindings()
}

func (suite *OvnClientTestSuite) Test_DestroyChassis() {
	suite.testDestroyChassis()
}

// ovs
func (suite *OvnClientTestSuite) Test_SetInterfaceBandwidth() {
	suite.testSetInterfaceBandwidth()
}

func (suite *OvnClientTestSuite) Test_ClearHtbQosQueue() {
	suite.testClearHtbQosQueue()
}

func (suite *OvnClientTestSuite) Test_IsHtbQos() {
	suite.testIsHtbQos()
}

func (suite *OvnClientTestSuite) Test_SetHtbQosQueueRecord() {
	suite.testSetHtbQosQueueRecord()
}

func (suite *OvnClientTestSuite) Test_SetQosQueueBinding() {
	suite.testSetQosQueueBinding()
}

func (suite *OvnClientTestSuite) Test_SetNetemQos() {
	suite.testSetNetemQos()
}

func (suite *OvnClientTestSuite) Test_GetNetemQosConfig() {
	suite.testGetNetemQosConfig()
}

func (suite *OvnClientTestSuite) Test_DeleteNetemQosByID() {
	suite.testDeleteNetemQosByID()
}

func (suite *OvnClientTestSuite) Test_IsUserspaceDataPath() {
	suite.testIsUserspaceDataPath()
}

func (suite *OvnClientTestSuite) Test_CheckAndUpdateHtbQos() {
	suite.testCheckAndUpdateHtbQos()
}

func (suite *OvnClientTestSuite) Test_UpdateOVSVsctlLimiter() {
	suite.testUpdateOVSVsctlLimiter()
}

func (suite *OvnClientTestSuite) Test_OvsExec() {
	suite.testOvsExec()
}

func (suite *OvnClientTestSuite) Test_OvsCreate() {
	suite.testOvsCreate()
}

func (suite *OvnClientTestSuite) Test_OvsDestroy() {
	suite.testOvsDestroy()
}

func (suite *OvnClientTestSuite) Test_OvsSet() {
	suite.testOvsSet()
}

func (suite *OvnClientTestSuite) Test_OvsAdd() {
	suite.testOvsAdd()
}

func (suite *OvnClientTestSuite) Test_OvsFind() {
	suite.testOvsFind()
}

func (suite *OvnClientTestSuite) Test_ParseOvsFindOutput() {
	suite.testParseOvsFindOutput()
}

func (suite *OvnClientTestSuite) Test_OvsRemove() {
	suite.testOvsRemove()
}

func (suite *OvnClientTestSuite) Test_OvsClear() {
	suite.testOvsClear()
}

func (suite *OvnClientTestSuite) Test_OvsGet() {
	suite.testOvsGet()
}

func (suite *OvnClientTestSuite) Test_OvsFindBridges() {
	suite.testOvsFindBridges()
}

func (suite *OvnClientTestSuite) Test_OvsBridgeExists() {
	suite.testOvsBridgeExists()
}

func (suite *OvnClientTestSuite) Test_OvsPortExists() {
	suite.testOvsPortExists()
}

func (suite *OvnClientTestSuite) Test_GetOvsQosList() {
	suite.testGetOvsQosList()
}

func (suite *OvnClientTestSuite) Test_OvsClearPodBandwidth() {
	suite.testOvsClearPodBandwidth()
}

func (suite *OvnClientTestSuite) Test_OvsCleanDuplicatePort() {
	suite.testOvsCleanDuplicatePort()
}

func (suite *OvnClientTestSuite) Test_ValidatePortVendor() {
	suite.testValidatePortVendor()
}

func (suite *OvnClientTestSuite) Test_GetInterfacePodNs() {
	suite.testGetInterfacePodNs()
}

func (suite *OvnClientTestSuite) Test_ConfigInterfaceMirror() {
	suite.testConfigInterfaceMirror()
}

func (suite *OvnClientTestSuite) Test_ClearPortQosBinding() {
	suite.testClearPortQosBinding()
}

func (suite *OvnClientTestSuite) Test_OvsListExternalIDs() {
	suite.testOvsListExternalIDs()
}

func (suite *OvnClientTestSuite) Test_ListQosQueueIDs() {
	suite.testListQosQueueIDs()
}

func Test_scratch(t *testing.T) {
	t.SkipNow()
	endpoint := "tcp:[172.20.149.35]:6641"
	ovnClient, err := newOvnNbClient(t, endpoint, 10)
	require.NoError(t, err)

	err = ovnClient.DeleteAcls("test_pg", portGroupKey, ovnnb.ACLDirectionToLport, nil)
	require.NoError(t, err)
}

func newOVSDBServer(t *testing.T, name string, dbModel model.ClientDBModel, schema ovsdb.DatabaseSchema) (*server.OvsdbServer, string) {
	serverDBModel, err := serverdb.FullDatabaseModel()
	require.NoError(t, err)
	serverSchema := serverdb.Schema()

	db := inmemory.NewDatabase(map[string]model.ClientDBModel{
		schema.Name:       dbModel,
		serverSchema.Name: serverDBModel,
	}, nil)

	dbMod, errs := model.NewDatabaseModel(schema, dbModel)
	require.Empty(t, errs)

	svrMod, errs := model.NewDatabaseModel(serverSchema, serverDBModel)
	require.Empty(t, errs)

	server, err := server.NewOvsdbServer(db, nil, dbMod, svrMod)
	require.NoError(t, err)

	tmpfile := fmt.Sprintf("/tmp/ovsdb-%s.sock", name)
	t.Cleanup(func() {
		os.Remove(tmpfile)
	})
	go func() {
		if err := server.Serve("unix", tmpfile); err != nil {
			t.Error(err)
		}
	}()
	t.Cleanup(func() {
		if server.Ready() {
			server.Close()
		}
	})
	require.Eventually(t, func() bool {
		return server.Ready()
	}, 1*time.Second, 10*time.Millisecond)

	return server, tmpfile
}

func newOvnNbClient(t *testing.T, ovnNbAddr string, ovnNbTimeout int) (*OVNNbClient, error) {
	nbClient, err := newNbClient(ovnNbAddr, ovnNbTimeout)
	require.NoError(t, err)

	return &OVNNbClient{
		ovsDbClient: ovsDbClient{
			Client:  nbClient,
			Timeout: time.Duration(ovnNbTimeout) * time.Second,
		},
	}, nil
}

// newLegacyClient init a legacy ovn client
func newLegacyClient(timeout int) *LegacyClient {
	return &LegacyClient{
		OvnTimeout: timeout,
	}
}

func newNbClient(addr string, timeout int) (client.Client, error) {
	dbModel, err := ovnnb.FullDatabaseModel()
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	dbModel.SetIndexes(map[string][]model.ClientIndex{
		ovnnb.LogicalRouterPolicyTable: {{Columns: []model.ColumnKey{
			{Column: "match"},
			{Column: "priority"},
		}}, {Columns: []model.ColumnKey{{Column: "priority"}}}, {Columns: []model.ColumnKey{{Column: "match"}}}},
	})

	logger := stdr.New(log.New(os.Stderr, "", log.LstdFlags)).
		WithName("libovsdb").
		WithValues("database", dbModel.Name())
	stdr.SetVerbosity(1)

	options := []client.Option{
		client.WithReconnect(time.Duration(timeout)*time.Second, &backoff.ZeroBackOff{}),
		client.WithLeaderOnly(false),
		client.WithLogger(&logger),
	}

	for ep := range strings.SplitSeq(addr, ",") {
		options = append(options, client.WithEndpoint(ep))
	}

	c, err := client.NewOVSDBClient(dbModel, options...)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	if err = c.Connect(context.TODO()); err != nil {
		klog.Error(err)
		return nil, err
	}

	monitorOpts := []client.MonitorOption{
		client.WithTable(&ovnnb.ACL{}),
		client.WithTable(&ovnnb.AddressSet{}),
		client.WithTable(&ovnnb.BFD{}),
		client.WithTable(&ovnnb.DHCPOptions{}),
		client.WithTable(&ovnnb.GatewayChassis{}),
		client.WithTable(&ovnnb.HAChassis{}),
		client.WithTable(&ovnnb.HAChassisGroup{}),
		client.WithTable(&ovnnb.LoadBalancer{}),
		client.WithTable(&ovnnb.LoadBalancerHealthCheck{}),
		client.WithTable(&ovnnb.LogicalRouterPolicy{}),
		client.WithTable(&ovnnb.LogicalRouterPort{}),
		client.WithTable(&ovnnb.LogicalRouterStaticRoute{}),
		client.WithTable(&ovnnb.LogicalRouter{}),
		client.WithTable(&ovnnb.LogicalSwitchPort{}),
		client.WithTable(&ovnnb.LogicalSwitch{}),
		client.WithTable(&ovnnb.NAT{}),
		client.WithTable(&ovnnb.NBGlobal{}),
		client.WithTable(&ovnnb.PortGroup{}),
		client.WithTable(&ovnnb.Meter{}),
		client.WithTable(&ovnnb.MeterBand{}),
	}
	if _, err = c.Monitor(context.TODO(), c.NewMonitor(monitorOpts...)); err != nil {
		klog.Error(err)
		return nil, err
	}

	return c, nil
}

func newOvnSbClient(t *testing.T, ovnSbAddr string, ovnSbTimeout int) (*OVNSbClient, error) {
	nbClient, err := newSbClient(ovnSbAddr, ovnSbTimeout)
	require.NoError(t, err)

	return &OVNSbClient{
		ovsDbClient: ovsDbClient{
			Client:  nbClient,
			Timeout: time.Duration(ovnSbTimeout) * time.Second,
		},
	}, nil
}

func newSbClient(addr string, timeout int) (client.Client, error) {
	dbModel, err := ovnsb.FullDatabaseModel()
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	logger := stdr.New(log.New(os.Stderr, "", log.LstdFlags)).
		WithName("libovsdb").
		WithValues("database", dbModel.Name())
	stdr.SetVerbosity(1)

	options := []client.Option{
		client.WithReconnect(time.Duration(timeout)*time.Second, &backoff.ZeroBackOff{}),
		client.WithLeaderOnly(false),
		client.WithLogger(&logger),
	}

	for ep := range strings.SplitSeq(addr, ",") {
		options = append(options, client.WithEndpoint(ep))
	}

	c, err := client.NewOVSDBClient(dbModel, options...)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	if err = c.Connect(context.TODO()); err != nil {
		klog.Error(err)
		return nil, err
	}

	monitorOpts := []client.MonitorOption{
		client.WithTable(&ovnsb.Chassis{}),
		client.WithTable(&ovnsb.DatapathBinding{}),
	}
	if _, err = c.Monitor(context.TODO(), c.NewMonitor(monitorOpts...)); err != nil {
		klog.Error(err)
		return nil, err
	}

	return c, nil
}
