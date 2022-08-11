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
	ovnClient *OvnClient
}

func (suite *OvnClientTestSuite) SetupSuite() {
	fmt.Println("set up OvnClient test suite")
	clientSchema := ovnnb.Schema()
	clientDBModel, err := ovnnb.FullDatabaseModel()
	require.NoError(suite.T(), err)

	_, sock := newOVSDBServer(suite.T(), clientDBModel, clientSchema)
	endpoint := fmt.Sprintf("unix:%s", sock)
	require.FileExists(suite.T(), sock)

	ovnClient, err := newOvnClient(suite.T(), endpoint, 10, "test-cluster-router")
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

func (suite *OvnClientTestSuite) Test_SetICAutoRoute() {
	suite.testSetICAutoRoute()
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
	suite.testlogicalSwitchUpdateAclOp()
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

func (suite *OvnClientTestSuite) Test_EnablePortLayer2forward() {
	suite.testEnablePortLayer2forward()
}

func (suite *OvnClientTestSuite) Test_SetLogicalSwitchPortVlanTag() {
	suite.testSetLogicalSwitchPortVlanTag()
}

func (suite *OvnClientTestSuite) Test_UpdateLogicalSwitchPort() {
	suite.testUpdateLogicalSwitchPort()
}

func (suite *OvnClientTestSuite) Test_DeleteLogicalSwitchPort() {
	suite.testDeleteLogicalSwitchPort()
}

func (suite *OvnClientTestSuite) Test_ListLogicalSwitchPorts() {
	suite.testListLogicalSwitchPorts()
}

func (suite *OvnClientTestSuite) Test_ListRemoteTypeLogicalSwitchPorts() {
	suite.testListRemoteTypeLogicalSwitchPorts()
}

func (suite *OvnClientTestSuite) Test_CreateLogicalSwitchPortOp() {
	suite.testCreateLogicalSwitchPortOp()
}

func (suite *OvnClientTestSuite) Test_DeleteLogicalSwitchPortOp() {
	suite.testDeleteLogicalSwitchPortOp()
}

/* logical_router unit test */
func (suite *OvnClientTestSuite) Test_CreateLogicalRouter() {
	suite.testCreateLogicalRouter()
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

func (suite *OvnClientTestSuite) Test_LogicalRouterUpdatePortOp() {
	suite.testLogicalRouterUpdatePortOp()
}

/* logical_router_port unit test */
func (suite *OvnClientTestSuite) Test_CreatePeerRouterPort() {
	suite.testCreatePeerRouterPort()
}

func (suite *OvnClientTestSuite) Test_UpdateRouterPortIPv6RA() {
	suite.testUpdateRouterPortIPv6RA()
}

func (suite *OvnClientTestSuite) Test_CreateLogicalRouterPort() {
	suite.testCreateLogicalRouterPort()
}

func (suite *OvnClientTestSuite) Test_UpdateLogicalRouterPort() {
	suite.testUpdateLogicalRouterPort()
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

func (suite *OvnClientTestSuite) Test_DeleteLoadBalancers() {
	suite.testDeleteLoadBalancers()
}

func (suite *OvnClientTestSuite) Test_GetLoadBalancer() {
	suite.testGetLoadBalancer()
}

func (suite *OvnClientTestSuite) Test_ListLoadBalancers() {
	suite.testListLoadBalancers()
}

func (suite *OvnClientTestSuite) Test_LoadBalancerUpdateVips() {
	suite.testLoadBalancerUpdateVips()
}

func (suite *OvnClientTestSuite) Test_DeleteLoadBalancerOp() {
	suite.testDeleteLoadBalancerOp()
}

/* port_group unit test */
func (suite *OvnClientTestSuite) Test_CreatePortGroup() {
	suite.testCreatePortGroup()
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

func (suite *OvnClientTestSuite) Test_PortGroupALLNotExist() {
	suite.testPortGroupALLNotExist()
}

func (suite *OvnClientTestSuite) Test_portGroupUpdatePortOp() {
	suite.testportGroupUpdatePortOp()
}

func (suite *OvnClientTestSuite) Test_portGroupUpdateAclOp() {
	suite.testportGroupUpdateAclOp()
}

func (suite *OvnClientTestSuite) Test_portGroupOp() {
	suite.testportGroupOp()
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
	suite.testaddressSetFilter()
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

func (suite *OvnClientTestSuite) Test_newSgRuleACL() {
	suite.testnewSgRuleACL()
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
	suite.testnewAcl()
}

func (suite *OvnClientTestSuite) Test_newAllowAclMatch() {
	suite.testnewAllowAclMatch()
}

func (suite *OvnClientTestSuite) Test_aclFilter() {
	suite.testaclFilter()
}

/* mixed operations unit test */
func (suite *OvnClientTestSuite) Test_CreateGatewayLogicalSwitch() {
	suite.testCreateGatewayLogicalSwitch()
}

func (suite *OvnClientTestSuite) Test_CreateRouterPort() {
	suite.testCreateRouterPort()
}

func (suite *OvnClientTestSuite) Test_CreateRouterTypePort() {
	suite.testCreateRouterTypePort()
}

func (suite *OvnClientTestSuite) Test_RemoveRouterTypePort() {
	suite.testRemoveRouterTypePort()
}

func (suite *OvnClientTestSuite) Test_DeleteLogicalGatewaySwitch() {
	suite.testDeleteLogicalGatewaySwitch()
}

func newOVSDBServer(t *testing.T, dbModel model.ClientDBModel, schema ovsdb.DatabaseSchema) (*server.OvsdbServer, string) {
	serverDBModel, err := serverdb.FullDatabaseModel()
	require.NoError(t, err)
	serverSchema := serverdb.Schema()

	db := server.NewInMemoryDatabase(map[string]model.ClientDBModel{
		schema.Name:       dbModel,
		serverSchema.Name: serverDBModel,
	})

	dbMod, errs := model.NewDatabaseModel(schema, dbModel)
	require.Empty(t, errs)

	servMod, errs := model.NewDatabaseModel(serverSchema, serverDBModel)
	require.Empty(t, errs)

	server, err := server.NewOvsdbServer(db, dbMod, servMod)
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

func newOvnClient(t *testing.T, ovnNbAddr string, ovnNbTimeout int, clusterRouter string) (*OvnClient, error) {
	nbClient, err := newNbClient(ovnNbAddr, ovnNbTimeout)
	require.NoError(t, err)

	return &OvnClient{
		ovnNbClient: ovnNbClient{
			Client:  nbClient,
			Timeout: ovnNbTimeout,
		},
		ClusterRouter: clusterRouter,
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
		client.WithTable(&ovnnb.LogicalRouter{}),
		client.WithTable(&ovnnb.LogicalRouterPort{}),
		client.WithTable(&ovnnb.LogicalRouterPolicy{}),
		client.WithTable(&ovnnb.LogicalRouterStaticRoute{}),
		client.WithTable(&ovnnb.LogicalSwitch{}),
		client.WithTable(&ovnnb.LogicalSwitchPort{}),
		client.WithTable(&ovnnb.PortGroup{}),
		client.WithTable(&ovnnb.NBGlobal{}),
		client.WithTable(&ovnnb.GatewayChassis{}),
		client.WithTable(&ovnnb.LoadBalancer{}),
		client.WithTable(&ovnnb.AddressSet{}),
		client.WithTable(&ovnnb.ACL{}),
	}
	if _, err = c.Monitor(context.TODO(), c.NewMonitor(monitorOpts...)); err != nil {
		return nil, err
	}

	return c, nil
}
