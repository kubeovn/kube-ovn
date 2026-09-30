package ovn_ic_controller

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/database/inmemory"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/ovn-kubernetes/libovsdb/ovsdb/serverdb"
	"github.com/ovn-kubernetes/libovsdb/server"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnicnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnicsb"
)

type cleanupNodeLister struct {
	called bool
	err    error
}

func (l *cleanupNodeLister) List(labels.Selector) ([]*corev1.Node, error) {
	l.called = true
	return nil, l.err
}

func (l *cleanupNodeLister) Get(string) (*corev1.Node, error) { return nil, l.err }

func TestDisableOVNICConnectsSBBeforeCleanup(t *testing.T) {
	stopCleanup := errors.New("stop before changing local state")
	nodes := &cleanupNodeLister{err: stopCleanup}
	c := &Controller{
		config:      &Configuration{OvnTimeout: 1, OvsDbConnectTimeout: 1, OvsDbConnectMaxRetry: 1},
		nodesLister: nodes,
	}
	t.Cleanup(c.closeICClients)

	// An unavailable old endpoint must leave the running deployment untouched.
	err := c.disableOVNIC(map[string]string{"az-name": "old-az", "ic-db-host": "127.0.0.1"})
	require.ErrorContains(t, err, "IC SB endpoint is incomplete")
	require.False(t, nodes.called)
	require.Nil(t, c.ICSbTables)

	// Initial establishment only connects NB. Reconfiguration must initialize
	// the old deployment's SB connection itself before entering local cleanup.
	sbModel, err := ovnicsb.FullDatabaseModel()
	require.NoError(t, err)
	host, port := newICTestEndpoint(t, sbModel, "ovn-ic-sb.ovsschema")
	oldConfig := map[string]string{"az-name": "old-az", "ic-db-host": host, "ic-sb-port": port}
	err = c.disableOVNIC(oldConfig)
	require.ErrorIs(t, err, stopCleanup)
	require.True(t, nodes.called)
	require.NotNil(t, c.ICSbTables)
	require.Equal(t, genHostAddress(host, port), c.icSbAddress)
	client := c.icSbClient
	require.ErrorIs(t, c.disableOVNIC(oldConfig), stopCleanup)
	require.Same(t, client, c.icSbClient, "cleanup retries should reuse the old SB connection")
}

func TestEnsureICNbClientWithUpstreamSchema(t *testing.T) {
	nbModel, err := ovnicnb.FullDatabaseModel()
	require.NoError(t, err)
	host, port := newICTestEndpoint(t, nbModel, "ovn-ic-nb.ovsschema")
	c := &Controller{config: &Configuration{OvnTimeout: 1, OvsDbConnectTimeout: 1, OvsDbConnectMaxRetry: 1}}
	t.Cleanup(c.closeICClients)
	require.NoError(t, c.ensureICNbClient(host, port))
	row := &ovnicnb.TransitSwitch{Name: "ts-old", ExternalIDs: map[string]string{"vendor": "kube-ovn", "subnet": "10.0.0.0/24"}}
	require.NoError(t, c.ICNbTables.Table(row).Create(t.Context(), "seed-transit-switch", row))
	names, err := c.listICTransitSwitches()
	require.NoError(t, err)
	require.Equal(t, []string{"ts-old"}, names)
	subnet, err := c.getICTransitSwitchSubnet("ts-old")
	require.NoError(t, err)
	require.Equal(t, "10.0.0.0/24", subnet)
}

func newICTestEndpoint(t *testing.T, dbModel model.ClientDBModel, schemaFile string) (string, string) {
	t.Helper()
	serverModel, err := serverdb.FullDatabaseModel()
	require.NoError(t, err)
	// Schemas copied from ovn-org/ovn v26.03.0 without modifications.
	data, err := os.ReadFile("testdata/" + schemaFile)
	require.NoError(t, err)
	var schema ovsdb.DatabaseSchema
	require.NoError(t, json.Unmarshal(data, &schema))
	db := inmemory.NewDatabase(map[string]model.ClientDBModel{schema.Name: dbModel, serverdb.Schema().Name: serverModel}, nil)
	icDB, errs := model.NewDatabaseModel(schema, dbModel)
	require.Empty(t, errs)
	serverDB, errs := model.NewDatabaseModel(serverdb.Schema(), serverModel)
	require.Empty(t, errs)
	dbServer, err := server.NewOvsdbServer(db, nil, icDB, serverDB)
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	go func() {
		if err := dbServer.Serve("tcp", address); err != nil {
			t.Error(err)
		}
	}()
	require.Eventually(t, dbServer.Ready, time.Second, 10*time.Millisecond)
	t.Cleanup(dbServer.Close)
	host, port, err := net.SplitHostPort(address)
	require.NoError(t, err)
	return host, port
}
