package bgp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPeersForFamilyFiltersFRRAddressFamilies(t *testing.T) {
	var summary frrSummary
	require.NoError(t, json.Unmarshal([]byte(`{
		"peers": {
			"10.0.1.2": {"state": "Established"},
			"10.0.1.3": {"state": "Established"},
			"fd00:10:1::2": {"state": "Established"},
			"fd00:10:1::3": {"state": "Established"}
		}
	}`), &summary))

	require.Len(t, peersForFamily(summary, ipv4Family), 2)
	require.Len(t, peersForFamily(summary, ipv6Family), 2)
}

func TestRoutePathsFromRoutePreservesPeerAndValidity(t *testing.T) {
	var route frrRoute
	require.NoError(t, json.Unmarshal([]byte(`{
		"paths": [
			{
				"peerId": "10.0.1.3",
				"valid": false,
				"nexthops": [{"ip": "10.0.1.3"}]
			},
			{
				"peerId": "10.0.1.2",
				"valid": true,
				"nexthops": [{"ip": "10.0.1.2"}]
			}
		]
	}`), &route))

	require.Equal(t, []frrRoutePath{
		{PeerID: "10.0.1.2", NextHop: "10.0.1.2", Valid: true},
		{PeerID: "10.0.1.3", NextHop: "10.0.1.3", Valid: false},
	}, routePathsFromRoute(&route))
}

func TestRoutePathsFromRouteHandlesAbsentRoute(t *testing.T) {
	require.Empty(t, routePathsFromRoute(&frrRoute{}))
}

func TestReadFRRRouteWithRunnerUsesFullRIBPeerMetadata(t *testing.T) {
	const prefix = "10.16.0.8/32"
	var command string
	route, err := readFRRRouteWithRunner(prefix, func(cmd string) ([]byte, error) {
		command = cmd
		return []byte(`{
			"routes": {
				"10.16.0.8/32": [{
					"peerId": "10.0.1.3",
					"valid": true,
					"nexthops": [{"ip": "10.0.1.3"}]
				}]
			}
		}`), nil
	})
	require.NoError(t, err)
	require.Equal(t, "show bgp ipv4 unicast json", command)
	require.Equal(t, []frrRoutePath{{PeerID: "10.0.1.3", NextHop: "10.0.1.3", Valid: true}}, routePathsFromRoute(route))
}

func TestReadFRRRouteWithRunnerSelectsIPv6RIB(t *testing.T) {
	const prefix = "fd00:10:16::8/128"
	var command string
	route, err := readFRRRouteWithRunner(prefix, func(cmd string) ([]byte, error) {
		command = cmd
		return []byte(`{"routes":{"fd00:10:16::8/128":[{"peerId":"fd00:10:1::3","valid":true,"nexthops":[{"ip":"fd00:10:1::3"}]}]}}`), nil
	})
	require.NoError(t, err)
	require.Equal(t, "show bgp ipv6 unicast json", command)
	require.Equal(t, []frrRoutePath{{PeerID: "fd00:10:1::3", NextHop: "fd00:10:1::3", Valid: true}}, routePathsFromRoute(route))
}
