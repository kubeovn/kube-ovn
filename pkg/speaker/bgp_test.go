package speaker

import (
	"context"
	"net"
	"testing"

	"github.com/osrg/gobgp/v4/api"
	"github.com/osrg/gobgp/v4/pkg/apiutil"
	"github.com/osrg/gobgp/v4/pkg/packet/bgp"
	gobgp "github.com/osrg/gobgp/v4/pkg/server"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"k8s.io/utils/set"
)

func newSpeakerTestBgpServer(t *testing.T, routerID string) *gobgp.BgpServer {
	t.Helper()

	server := gobgp.NewBgpServer()
	go server.Serve()
	require.NoError(t, server.StartBgp(context.Background(), &api.StartBgpRequest{
		Global: &api.Global{Asn: 65000, RouterId: routerID, ListenPort: -1},
	}))
	t.Cleanup(func() {
		require.NoError(t, server.StopBgp(context.Background(), &api.StopBgpRequest{}))
		server.Stop()
	})
	return server
}

func speakerTestPrefixNextHops(t *testing.T, server *gobgp.BgpServer, afi api.Family_Afi) map[string][]net.IP {
	t.Helper()

	prefixes := map[string][]net.IP{}
	err := server.ListPath(apiutil.ListPathRequest{
		TableType: api.TableType_TABLE_TYPE_GLOBAL,
		Family:    apiutil.ToFamily(&api.Family{Afi: afi, Safi: api.Family_SAFI_UNICAST}),
	}, func(prefix bgp.NLRI, paths []*apiutil.Path) {
		for _, path := range paths {
			prefixes[prefix.String()] = append(prefixes[prefix.String()], getNextHopFromPathAttributes(path.Attrs))
		}
	})
	require.NoError(t, err)
	return prefixes
}

func TestReconcileRoutesRefreshesIPv4NextHop(t *testing.T) {
	const (
		routerID       = "192.0.2.254"
		neighbor       = "203.0.113.1"
		prefix         = "10.244.0.8/32"
		initialNextHop = "127.0.0.2"
		updatedNextHop = "127.0.0.3"
	)

	nextHop := net.ParseIP(initialNextHop)
	controller := &Controller{config: &Configuration{
		RouterID:          net.ParseIP(routerID),
		NeighborAddresses: []net.IP{net.ParseIP(neighbor)},
		BgpServer:         newSpeakerTestBgpServer(t, routerID),
		routeLookup: func(address net.IP) ([]netlink.Route, error) {
			require.True(t, net.ParseIP(neighbor).Equal(address))
			return []netlink.Route{{Src: nextHop}}, nil
		},
	}}
	expectedPrefixes := prefixMap{api.Family_AFI_IP: set.New(prefix)}

	require.NoError(t, controller.reconcileRoutes(expectedPrefixes))
	routes := speakerTestPrefixNextHops(t, controller.config.BgpServer, api.Family_AFI_IP)
	require.Len(t, routes[prefix], 1)
	require.True(t, net.ParseIP(initialNextHop).Equal(routes[prefix][0]))

	nextHop = net.ParseIP(updatedNextHop)
	require.NoError(t, controller.reconcileRoutes(expectedPrefixes))
	routes = speakerTestPrefixNextHops(t, controller.config.BgpServer, api.Family_AFI_IP)
	require.Len(t, routes[prefix], 1)
	require.True(t, net.ParseIP(updatedNextHop).Equal(routes[prefix][0]))
}
