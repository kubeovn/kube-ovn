package ovs

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/go-logr/stdr"
	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/database"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/ovn-org/libovsdb/ovsdb/serverdb"
	"github.com/ovn-org/libovsdb/server"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

type OvnClientTestSuite struct {
	suite.Suite
	ovnClient *ovnClient
}

func (suite *OvnClientTestSuite) SetupSuite() {
	fmt.Println("set up OvnClient test suite")
	clientSchema := ovnnb.Schema()
	clientDBModel, err := ovnnb.FullDatabaseModel()
	require.NoError(suite.T(), err)

	_, sock := newOVSDBServer(suite.T(), clientDBModel, clientSchema)
	endpoint := fmt.Sprintf("unix:%s", sock)
	require.FileExists(suite.T(), sock)

	ovnClient, err := newOvnClient(suite.T(), endpoint, 10, "100.64.0.0/16,fd00:100:64::/64")
	require.NoError(suite.T(), err)

	suite.ovnClient = ovnClient
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

func (suite *OvnClientTestSuite) Test_logicalSwitchUpdateAclOp() {
	suite.test_logicalSwitchUpdateAclOp()
}

func (suite *OvnClientTestSuite) Test_LogicalSwitchOp() {
	suite.testLogicalSwitchOp()
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

func (suite *OvnClientTestSuite) Test_CreateBareLogicalSwitchPort() {
	suite.testCreateBareLogicalSwitchPort()
}

func (suite *OvnClientTestSuite) Test_SetLogicalSwitchPortVirtualParents() {
	suite.testSetLogicalSwitchPortVirtualParents()
}

func (suite *OvnClientTestSuite) Test_SetLogicalSwitchPortSecurity() {
	suite.testSetLogicalSwitchPortSecurity()
}

func (suite *OvnClientTestSuite) Test_SetSetLogicalSwitchPortExternalIds() {
	suite.testSetSetLogicalSwitchPortExternalIds()
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

func (suite *OvnClientTestSuite) Test_getLogicalSwitchPortSgs() {
	suite.testgetLogicalSwitchPortSgs()
}

func (suite *OvnClientTestSuite) Test_DeleteLogicalSwitchPort() {
	suite.testDeleteLogicalSwitchPort()
}

func (suite *OvnClientTestSuite) Test_ListLogicalSwitchPorts() {
	suite.testListLogicalSwitchPorts()
}

func (suite *OvnClientTestSuite) Test_CreateLogicalSwitchPortOp() {
	suite.testCreateLogicalSwitchPortOp()
}

func (suite *OvnClientTestSuite) Test_DeleteLogicalSwitchPortOp() {
	suite.testDeleteLogicalSwitchPortOp()
}

func (suite *OvnClientTestSuite) Test_logicalSwitchPortFilter() {
	suite.testlogicalSwitchPortFilter()
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

func (suite *OvnClientTestSuite) Test_testLogicalRouterUpdateLoadBalancers() {
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

func (suite *OvnClientTestSuite) Test_CreateLogicalRouterPort() {
	suite.testCreateLogicalRouterPort()
}

func (suite *OvnClientTestSuite) Test_UpdateLogicalRouterPort() {
	suite.testUpdateLogicalRouterPort()
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

func (suite *OvnClientTestSuite) Test_logicalRouterPortFilter() {
	suite.testlogicalRouterPortFilter()
}

/* gateway_chassis unit test */
func (suite *OvnClientTestSuite) Test_CreateGatewayChassises() {
	suite.testCreateGatewayChassises()
}

func (suite *OvnClientTestSuite) Test_DeleteGatewayChassises() {
	suite.testDeleteGatewayChassises()
}

func (suite *OvnClientTestSuite) Test_DeleteGatewayChassisOp() {
	suite.testDeleteGatewayChassisOp()
}

/* load_balancer unit test */
func (suite *OvnClientTestSuite) Test_CreateLoadBalancer() {
	suite.testCreateLoadBalancer()
}

func (suite *OvnClientTestSuite) Test_UpdateLoadBalancer() {
	suite.testUpdateLoadBalancer()
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
	suite.test_portGroupUpdatePortOp()
}

func (suite *OvnClientTestSuite) Test_portGroupUpdateAclOp() {
	suite.test_portGroupUpdateAclOp()
}

func (suite *OvnClientTestSuite) Test_portGroupOp() {
	suite.test_portGroupOp()
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
	suite.test_addressSetFilter()
}

/* acl unit test */
func (suite *OvnClientTestSuite) Test_CreateIngressAcl() {
	suite.testCreateIngressAcl()
}

func (suite *OvnClientTestSuite) Test_CreateEgressAcl() {
	suite.testCreateEgressAcl()
}

func (suite *OvnClientTestSuite) Test_CreateGatewayAcl() {
	suite.testCreateGatewayAcl()
}

func (suite *OvnClientTestSuite) Test_CreateNodeAcl() {
	suite.testCreateNodeAcl()
}

func (suite *OvnClientTestSuite) Test_CreateSgDenyAllAcl() {
	suite.testCreateSgDenyAllAcl()
}

func (suite *OvnClientTestSuite) Test_UpdateSgAcl() {
	suite.testUpdateSgAcl()
}

func (suite *OvnClientTestSuite) Test_UpdateLogicalSwitchAcl() {
	suite.testUpdateLogicalSwitchAcl()
}

func (suite *OvnClientTestSuite) Test_SetAclLog() {
	suite.testSetAclLog()
}

func (suite *OvnClientTestSuite) Test_SetLogicalSwitchPrivate() {
	suite.testSetLogicalSwitchPrivate()
}

func (suite *OvnClientTestSuite) Test_newSgRuleACL() {
	suite.test_newSgRuleACL()
}

func (suite *OvnClientTestSuite) Test_CreateAcls() {
	suite.testCreateAcls()
}

func (suite *OvnClientTestSuite) Test_DeleteAcls() {
	suite.testDeleteAcls()
}

func (suite *OvnClientTestSuite) Test_GetAcl() {
	suite.testGetAcl()
}

func (suite *OvnClientTestSuite) Test_ListAcls() {
	suite.testListAcls()
}

func (suite *OvnClientTestSuite) Test_newAcl() {
	suite.test_newAcl()
}

func (suite *OvnClientTestSuite) Test_newNetworkPolicyAclMatch() {
	suite.testnewNetworkPolicyAclMatch()
}

func (suite *OvnClientTestSuite) Test_aclFilter() {
	suite.test_aclFilter()
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

func (suite *OvnClientTestSuite) Test_ClearLogicalRouterPolicy() {
	suite.testClearLogicalRouterPolicy()
}

func (suite *OvnClientTestSuite) Test_GetLogicalRouterPolicy() {
	suite.testGetLogicalRouterPolicy()
}

func (suite *OvnClientTestSuite) Test_newLogicalRouterPolicy() {
	suite.test_newLogicalRouterPolicy()
}

func (suite *OvnClientTestSuite) Test_policyFilter() {
	suite.test_policyFilter()
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

func (suite *OvnClientTestSuite) Test_DeleteNats() {
	suite.testDeleteNats()
}

func (suite *OvnClientTestSuite) Test_DeleteNat() {
	suite.testDeleteNat()
}

func (suite *OvnClientTestSuite) Test_GetNat() {
	suite.testGetNat()
}

func (suite *OvnClientTestSuite) Test_newNat() {
	suite.test_newNat()
}

func (suite *OvnClientTestSuite) Test_natFilter() {
	suite.test_natFilter()
}

/* logical_router_static_route unit test */
func (suite *OvnClientTestSuite) Test_CreateLogicalRouterStaticRoutes() {
	suite.testCreateLogicalRouterStaticRoutes()
}

func (suite *OvnClientTestSuite) Test_AddLogicalRouterStaticRoute() {
	suite.testAddLogicalRouterStaticRoute()
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
	suite.test_newLogicalRouterStaticRoute()
}

/* dhcp options unit test */
func (suite *OvnClientTestSuite) Test_UpdateDHCPOptions() {
	suite.testUpdateDHCPOptions()
}

func (suite *OvnClientTestSuite) Test_updateDHCPv4Options() {
	suite.test_updateDHCPv4Options()
}

func (suite *OvnClientTestSuite) Test_updateDHCPv6Options() {
	suite.test_updateDHCPv6Options()
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
	suite.test_dhcpOptionsFilter()
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

func Test_scratch(t *testing.T) {
	t.SkipNow()
	endpoint := "tcp:[172.20.149.35]:6641"
	ovnClient, err := newOvnClient(t, endpoint, 10, "")
	require.NoError(t, err)

	lbName := "test-lb"
	err = ovnClient.CreateLoadBalancer(lbName, "tcp", "ip_src")
	require.NoError(t, err)

	vips := map[string]string{
		"10.96.0.1:443":           "192.168.20.11:6443",
		"10.107.43.237:8080":      "10.244.0.100:8080,10.244.0.16:8080,10.244.0.17:8080",
		"[fd00:10:96::e82f]:8080": "[fc00::af4:f]:8080,[fc00::af4:10]:8080,[fc00::af4:11]:8080",
	}

	for vip, backends := range vips {
		err = ovnClient.LoadBalancerAddVip(lbName, vip, strings.Split(backends, ",")...)
		require.NoError(t, err)
	}

	err = ovnClient.LoadBalancerDeleteVip(lbName, "10.96.0.1:443")
	require.NoError(t, err)
}

func newOVSDBServer(t *testing.T, dbModel model.ClientDBModel, schema ovsdb.DatabaseSchema) (*server.OvsdbServer, string) {
	serverDBModel, err := serverdb.FullDatabaseModel()
	require.NoError(t, err)
	serverSchema := serverdb.Schema()

	db := database.NewInMemoryDatabase(map[string]model.ClientDBModel{
		schema.Name:       dbModel,
		serverSchema.Name: serverDBModel,
	})

	dbMod, errs := model.NewDatabaseModel(schema, dbModel)
	require.Empty(t, errs)

	svrMod, errs := model.NewDatabaseModel(serverSchema, serverDBModel)
	require.Empty(t, errs)

	server, err := server.NewOvsdbServer(db, dbMod, svrMod)
	require.NoError(t, err)

	tmpfile := fmt.Sprintf("/tmp/ovsdb-%d.sock", rand.Intn(10000))
	t.Cleanup(func() {
		os.Remove(tmpfile)
	})
	go func() {
		if err := server.Serve("unix", tmpfile); err != nil {
			t.Error(err)
		}
	}()
	t.Cleanup(server.Close)
	require.Eventually(t, func() bool {
		return server.Ready()
	}, 1*time.Second, 10*time.Millisecond)

	return server, tmpfile
}

func newOvnClient(t *testing.T, ovnNbAddr string, ovnNbTimeout int, nodeSwitchCIDR string) (*ovnClient, error) {
	nbClient, err := newNbClient(ovnNbAddr, ovnNbTimeout)
	require.NoError(t, err)

	return &ovnClient{
		ovnNbClient: ovnNbClient{
			Client:  nbClient,
			Timeout: time.Duration(ovnNbTimeout) * time.Second,
		},
		NodeSwitchCIDR: nodeSwitchCIDR,
	}, nil
}

func newNbClient(addr string, timeout int) (client.Client, error) {
	dbModel, err := ovnnb.FullDatabaseModel()
	if err != nil {
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

	for _, ep := range strings.Split(addr, ",") {
		options = append(options, client.WithEndpoint(ep))
	}

	c, err := client.NewOVSDBClient(dbModel, options...)
	if err != nil {
		return nil, err
	}

	if err = c.Connect(context.TODO()); err != nil {
		return nil, err
	}

	monitorOpts := []client.MonitorOption{
		client.WithTable(&ovnnb.ACL{}),
		client.WithTable(&ovnnb.AddressSet{}),
		client.WithTable(&ovnnb.DHCPOptions{}),
		client.WithTable(&ovnnb.GatewayChassis{}),
		client.WithTable(&ovnnb.LoadBalancer{}),
		client.WithTable(&ovnnb.LogicalRouterPolicy{}),
		client.WithTable(&ovnnb.LogicalRouterPort{}),
		client.WithTable(&ovnnb.LogicalRouterStaticRoute{}),
		client.WithTable(&ovnnb.LogicalRouter{}),
		client.WithTable(&ovnnb.LogicalSwitchPort{}),
		client.WithTable(&ovnnb.LogicalSwitch{}),
		client.WithTable(&ovnnb.NAT{}),
		client.WithTable(&ovnnb.NBGlobal{}),
		client.WithTable(&ovnnb.PortGroup{}),
	}
	if _, err = c.Monitor(context.TODO(), c.NewMonitor(monitorOpts...)); err != nil {
		return nil, err
	}

	return c, nil
}
