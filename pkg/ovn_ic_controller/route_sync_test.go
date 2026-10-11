package ovn_ic_controller

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/database/inmemory"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb/serverdb"
	"github.com/ovn-kubernetes/libovsdb/server"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// newICRouteTestClient serves an in-memory OVN northbound database so handlers run against real rows.
func newICRouteTestClient(t *testing.T) *ovs.OVNNbClient {
	t.Helper()

	nbModel, err := ovnnb.FullDatabaseModel()
	require.NoError(t, err)
	serverModel, err := serverdb.FullDatabaseModel()
	require.NoError(t, err)
	db := inmemory.NewDatabase(map[string]model.ClientDBModel{ovnnb.DatabaseName: nbModel, serverdb.Schema().Name: serverModel}, nil)
	nbDB, errs := model.NewDatabaseModel(ovnnb.Schema(), nbModel)
	require.Empty(t, errs)
	serverDB, errs := model.NewDatabaseModel(serverdb.Schema(), serverModel)
	require.Empty(t, errs)
	dbServer, err := server.NewOvsdbServer(db, nil, nbDB, serverDB)
	require.NoError(t, err)

	socket := filepath.Join(t.TempDir(), "nb.sock")
	go func() {
		if err := dbServer.Serve("unix", socket); err != nil {
			t.Error(err)
		}
	}()
	require.Eventually(t, dbServer.Ready, time.Second, 10*time.Millisecond)
	t.Cleanup(dbServer.Close)

	nbClient, err := ovs.NewOvnNbClient("unix:"+socket, 3, 3, 0, 1)
	require.NoError(t, err)
	t.Cleanup(nbClient.Close)
	return nbClient
}

func TestICRouteSyncPreservesUnownedPolicies(t *testing.T) {
	for _, origin := range []string{util.OvnICNone, util.OvnICConnected, util.OvnICStatic} {
		t.Run("origin="+origin, func(t *testing.T) {
			client := newICRouteTestClient(t)
			router := util.DefaultVpc
			require.NoError(t, client.CreateLogicalRouter(router))
			c := &Controller{config: &Configuration{ClusterRouter: router}, OVNNbTables: client}
			// Reproduce startup before IC configuration: the ordinary default route
			// and node return policy have no origin key.
			require.NoError(t, client.AddLogicalRouterStaticRoute(router, "", ovnnb.LogicalRouterStaticRoutePolicyDstIP, "0.0.0.0/0", nil, nil, "100.64.0.1"))
			nodeMatch := "ip4.dst == 172.18.0.4"
			require.NoError(t, client.AddLogicalRouterPolicy(router, util.NodeRouterPolicyPriority, nodeMatch, ovnnb.LogicalRouterPolicyActionReroute, []string{"100.64.0.4"}, nil, map[string]string{"node": "worker"}))
			unownedMatch := "ip4.dst == 192.0.2.0/24"
			require.NoError(t, client.AddLogicalRouterPolicy(router, util.OvnICPolicyPriority, unownedMatch, ovnnb.LogicalRouterPolicyActionAllow, nil, nil, nil))

			c.syncOneRouteToPolicy(util.OvnICKey, origin)
			policies, err := client.ListLogicalRouterPolicies(router, -1, nil, false)
			require.NoError(t, err)
			require.Len(t, policies, 2, "IC startup must not create policies from an untagged default route or remove node policies")
			requireICPolicyPresent(t, client, util.NodeRouterPolicyPriority, nodeMatch)
			requireICPolicyPresent(t, client, util.OvnICPolicyPriority, unownedMatch)

			// Explicitly empty origin is a supported legacy IC tag.
			route := &ovnnb.LogicalRouterStaticRoute{UUID: ovsclient.NamedUUID(), IPPrefix: "10.20.0.0/16", Nexthop: "100.64.0.10", Options: map[string]string{util.OvnICKey: origin}}
			require.NoError(t, client.CreateLogicalRouterStaticRoutes(router, route))
			for range 2 {
				c.syncOneRouteToPolicy(util.OvnICKey, origin)
				policies, err = client.ListLogicalRouterPolicies(router, -1, nil, false)
				require.NoError(t, err)
				require.Len(t, policies, 3)
				requireICPolicyPresent(t, client, util.NodeRouterPolicyPriority, nodeMatch)
				requireICPolicyPresent(t, client, util.OvnICPolicyPriority, unownedMatch)
				learned := requireICPolicyPresent(t, client, util.OvnICPolicyPriority, "ip4.dst == 10.20.0.0/16")
				require.Contains(t, learned.ExternalIDs, util.OvnICKey)
				require.Equal(t, origin, learned.ExternalIDs[util.OvnICKey])
			}

			require.NoError(t, client.DeleteLogicalRouterStaticRouteByExternalIDs(router, nil))
			c.syncOneRouteToPolicy(util.OvnICKey, origin)
			policies, err = client.ListLogicalRouterPolicies(router, -1, nil, false)
			require.NoError(t, err)
			require.Len(t, policies, 2, "only the stale tagged IC policy should be removed")
			requireICPolicyPresent(t, client, util.NodeRouterPolicyPriority, nodeMatch)
			requireICPolicyPresent(t, client, util.OvnICPolicyPriority, unownedMatch)
		})
	}
}

func requireICPolicyPresent(t *testing.T, client *ovs.OVNNbClient, priority int, match string) *ovnnb.LogicalRouterPolicy {
	t.Helper()
	policies, err := client.GetLogicalRouterPolicy(util.DefaultVpc, priority, match, false)
	require.NoError(t, err)
	require.Len(t, policies, 1)
	return policies[0]
}
