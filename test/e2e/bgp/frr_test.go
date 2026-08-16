package bgp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

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
