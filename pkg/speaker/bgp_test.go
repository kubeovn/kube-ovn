package speaker

import (
	"context"
	"net"
	"net/netip"
	"testing"

	"github.com/osrg/gobgp/v4/api"
	"github.com/osrg/gobgp/v4/pkg/apiutil"
	"github.com/osrg/gobgp/v4/pkg/packet/bgp"
	gobgp "github.com/osrg/gobgp/v4/pkg/server"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"k8s.io/utils/set"
)

func newTestBgpServer(t *testing.T, routerID string) *gobgp.BgpServer {
	t.Helper()

	server := gobgp.NewBgpServer()
	go server.Serve()
	require.NoError(t, server.StartBgp(context.Background(), &api.StartBgpRequest{
		Global: &api.Global{
			Asn:        65000,
			RouterId:   routerID,
			ListenPort: -1,
		},
	}))
	t.Cleanup(func() {
		require.NoError(t, server.StopBgp(context.Background(), &api.StopBgpRequest{}))
		server.Stop()
	})
	return server
}

func listTestPrefixNextHops(t *testing.T, server *gobgp.BgpServer, afi api.Family_Afi) map[string][]net.IP {
	t.Helper()

	prefixes := map[string][]net.IP{}
	err := server.ListPath(apiutil.ListPathRequest{
		TableType: api.TableType_TABLE_TYPE_GLOBAL,
		Family: apiutil.ToFamily(&api.Family{
			Afi:  afi,
			Safi: api.Family_SAFI_UNICAST,
		}),
	}, func(prefix bgp.NLRI, paths []*apiutil.Path) {
		for _, path := range paths {
			prefixes[prefix.String()] = append(prefixes[prefix.String()], getNextHopFromPathAttributes(path.Attrs))
		}
	})
	require.NoError(t, err)
	return prefixes
}

func TestReconcileRoutesAddsAndWithdrawsIPv4Routes(t *testing.T) {
	const (
		routerID = "192.0.2.10"
		neighbor = "192.0.2.1"
		prefix   = "10.244.0.8/32"
	)

	controller := &Controller{config: &Configuration{
		RouterID:               net.ParseIP(routerID),
		NeighborAddresses:      []net.IP{net.ParseIP(neighbor)},
		NeighborLocalAddresses: map[string]net.IP{neighbor: net.ParseIP(routerID)},
		BgpServer:              newTestBgpServer(t, routerID),
	}}

	require.NoError(t, controller.reconcileRoutes(prefixMap{
		api.Family_AFI_IP: set.New(prefix),
	}))
	routes := listTestPrefixNextHops(t, controller.config.BgpServer, api.Family_AFI_IP)
	require.Contains(t, routes, prefix)
	require.Len(t, routes[prefix], 1)
	require.True(t, net.ParseIP(routerID).Equal(routes[prefix][0]))

	require.NoError(t, controller.reconcileRoutes(prefixMap{
		api.Family_AFI_IP: set.New[string](),
	}))
	require.NotContains(t, listTestPrefixNextHops(t, controller.config.BgpServer, api.Family_AFI_IP), prefix)
}

func TestGetPathRequest(t *testing.T) {
	const (
		ipv4Neighbor = "192.0.2.1"
		ipv4NextHop  = "192.0.2.10"
		ipv6Neighbor = "2001:db8::1"
		ipv6NextHop  = "2001:db8::10"
	)

	tests := []struct {
		name             string
		route            string
		extendedNexthop  bool
		expectedPrefix   string
		expectedFamily   bgp.Family
		expectedNextHops []string
		expectError      string
	}{
		{
			name:             "IPv4 route uses IPv4 neighbors",
			route:            "10.244.0.8",
			expectedPrefix:   "10.244.0.8/32",
			expectedFamily:   bgp.RF_IPv4_UC,
			expectedNextHops: []string{ipv4NextHop},
		},
		{
			name:             "IPv6 route uses IPv6 neighbors",
			route:            "fd00::8/128",
			expectedPrefix:   "fd00::8/128",
			expectedFamily:   bgp.RF_IPv6_UC,
			expectedNextHops: []string{ipv6NextHop},
		},
		{
			name:             "extended nexthop uses every neighbor",
			route:            "10.244.0.0/24",
			extendedNexthop:  true,
			expectedPrefix:   "10.244.0.0/24",
			expectedFamily:   bgp.RF_IPv4_UC,
			expectedNextHops: []string{ipv4NextHop, ipv6NextHop},
		},
		{
			name:        "invalid route is rejected",
			route:       "not-an-ip",
			expectError: "failed to parse route",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &Controller{config: &Configuration{
				NeighborAddresses:     []net.IP{net.ParseIP(ipv4Neighbor)},
				NeighborIPv6Addresses: []net.IP{net.ParseIP(ipv6Neighbor)},
				NeighborLocalAddresses: map[string]net.IP{
					ipv4Neighbor: net.ParseIP(ipv4NextHop),
					ipv6Neighbor: net.ParseIP(ipv6NextHop),
				},
				ExtendedNexthop: tt.extendedNexthop,
			}}

			paths, err := controller.getPathRequest(tt.route)
			if tt.expectError != "" {
				require.ErrorContains(t, err, tt.expectError)
				return
			}

			require.NoError(t, err)
			require.Len(t, paths, len(tt.expectedNextHops))
			for i, pathGroup := range paths {
				require.Len(t, pathGroup, 1)
				path := pathGroup[0]
				require.Equal(t, tt.expectedFamily, path.Family)
				require.Equal(t, tt.expectedPrefix, path.Nlri.String())
				require.True(t, net.ParseIP(tt.expectedNextHops[i]).Equal(getNextHopFromPathAttributes(path.Attrs)))
			}
		})
	}
}

func TestGetNextHopFromPathAttributes(t *testing.T) {
	ipv4 := net.ParseIP("192.0.2.10")
	ipv6 := net.ParseIP("2001:db8::10")
	nextHopAttr, err := bgp.NewPathAttributeNextHop(netip.MustParseAddr(ipv4.String()))
	require.NoError(t, err)
	ipv6Prefix, err := bgp.NewIPAddrPrefix(netip.MustParsePrefix("fd00::8/128"))
	require.NoError(t, err)
	mpReachAttr, err := bgp.NewPathAttributeMpReachNLRI(
		bgp.RF_IPv6_UC,
		[]bgp.PathNLRI{{NLRI: ipv6Prefix}},
		netip.MustParseAddr(ipv6.String()),
	)
	require.NoError(t, err)

	tests := []struct {
		name     string
		attrs    []bgp.PathAttributeInterface
		expected net.IP
	}{
		{
			name:     "NEXT_HOP",
			attrs:    []bgp.PathAttributeInterface{nextHopAttr},
			expected: ipv4,
		},
		{
			name:     "MP_REACH_NLRI",
			attrs:    []bgp.PathAttributeInterface{mpReachAttr},
			expected: ipv6,
		},
		{
			name:  "missing next hop",
			attrs: []bgp.PathAttributeInterface{bgp.NewPathAttributeOrigin(0)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := getNextHopFromPathAttributes(tt.attrs)
			if tt.expected == nil {
				require.Nil(t, actual)
				return
			}
			require.True(t, tt.expected.Equal(actual))
		})
	}
}

func TestGetNextHopAttributeReusesInitializedNeighborLocalAddresses(t *testing.T) {
	ipv4Neighbor := net.ParseIP("192.0.2.1")
	ipv4StartupAddress := net.ParseIP("192.0.2.10")
	ipv6Neighbor := net.ParseIP("2001:db8::1")
	ipv6StartupAddress := net.ParseIP("2001:db8::10")
	routeSources := map[string]net.IP{
		ipv4Neighbor.String(): ipv4StartupAddress,
		ipv6Neighbor.String(): ipv6StartupAddress,
	}
	config := &Configuration{
		NeighborAddresses:          []net.IP{ipv4Neighbor},
		NeighborIPv6Addresses:      []net.IP{ipv6Neighbor},
		AllowedSourceAddresses:     []net.IP{ipv4StartupAddress},
		AllowedSourceIPv6Addresses: []net.IP{ipv6StartupAddress},
	}
	require.NoError(t, config.initNeighborLocalAddressesWithRouteLookup(func(address net.IP) ([]netlink.Route, error) {
		source, ok := routeSources[address.String()]
		require.True(t, ok)
		return []netlink.Route{{Src: source}}, nil
	}))

	routeSources[ipv4Neighbor.String()] = net.ParseIP("192.0.2.20")
	routeSources[ipv6Neighbor.String()] = net.ParseIP("2001:db8::20")
	controller := &Controller{config: config}
	require.True(t, ipv4StartupAddress.Equal(controller.getNextHopAttribute(ipv4Neighbor)))
	require.True(t, ipv6StartupAddress.Equal(controller.getNextHopAttribute(ipv6Neighbor)))
}
