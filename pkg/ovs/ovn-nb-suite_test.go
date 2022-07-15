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

	ovnClient, err := newOvnClient(suite.T(), endpoint, 10)
	require.NoError(suite.T(), err)

	// create logical switch

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

/* logical_router unit test */
func (suite *OvnClientTestSuite) Test_CreateLogicalRouter() {
	suite.testCreateLogicalRouter()
}

func (suite *OvnClientTestSuite) Test_DeleteLogicalRouterr() {
	suite.testDeleteLogicalRouter()
}

func (suite *OvnClientTestSuite) Test_GetLogicalRouter() {
	suite.testGetLogicalRouter()
}

func (suite *OvnClientTestSuite) Test_ListLogicalRouter() {
	suite.testListLogicalRouter()
}

func (suite *OvnClientTestSuite) Test_LogicalRouterOp() {
	suite.testLogicalRouterOp()
}

/* logical_router_port unit test */
func (suite *OvnClientTestSuite) Test_CreateVpcExGwLogicalRouterPort() {
	suite.testCreateVpcExGwLogicalRouterPort()
}

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

/* gateway chassis unit test */
func (suite *OvnClientTestSuite) Test_CreateGatewayChassis() {
	suite.testCreateGatewayChassis()
}

func (suite *OvnClientTestSuite) Test_CreateGatewayChassises() {
	suite.testCreateGatewayChassises()
}

func (suite *OvnClientTestSuite) Test_scratch() {
	t := suite.T()
	t.Parallel()
	t.SkipNow()

	ovnClient := suite.ovnClient
	name := "test-create-lsp"

	err := ovnClient.CreateLogicalSwitchPort("02:42:83:d3:87:43", "", name)
	require.NoError(t, err)

	out, err := ovnClient.GetLogicalSwitchPort(name, false)
	require.NoError(t, err)
	require.Equal(t, out.Name, name)
	require.NotEmpty(t, out.UUID)
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

func newOvnClient(t *testing.T, ovnNbAddr string, ovnNbTimeout int) (*OvnClient, error) {
	nbClient, err := newNbClient(ovnNbAddr, ovnNbTimeout)
	require.NoError(t, err)

	return &OvnClient{ovnNbClient: ovnNbClient{Client: nbClient, Timeout: ovnNbTimeout}}, nil
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
	}
	if _, err = c.Monitor(context.TODO(), c.NewMonitor(monitorOpts...)); err != nil {
		return nil, err
	}

	return c, nil
}
