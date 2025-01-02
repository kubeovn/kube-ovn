package ovs

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (suite *OvnClientTestSuite) testCreateLogicalRouterStaticRoutes() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-create-routes-lr"
	routeTable := util.MainRouteTable
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
	ipPrefixes := [...]string{"192.168.30.0/24", "192.168.40.0/24"}
	nexthops := [...]string{"192.168.30.1", "192.168.40.1"}
	routes := make([]*ovnnb.LogicalRouterStaticRoute, 0, 5)

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	for i, ipPrefix := range ipPrefixes {
		route, err := nbClient.newLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthops[i], nil, nil)
		require.NoError(t, err)

		routes = append(routes, route)
	}

	err = nbClient.CreateLogicalRouterStaticRoutes(lrName, append(routes, nil)...)
	require.NoError(t, err)

	lr, err := nbClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)

	for i, ipPrefix := range ipPrefixes {
		route, err := nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthops[i], false)
		require.NoError(t, err)

		require.Contains(t, lr.StaticRoutes, route.UUID)
	}

	// create logical router static routes for non-exist logical router
	err = nbClient.CreateLogicalRouterStaticRoutes("non-exist-lrName", append(routes, nil)...)
	require.ErrorContains(t, err, "generate operations for adding static routes to logical router")
}

func (suite *OvnClientTestSuite) testAddLogicalRouterStaticRoute() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient
	lrName := "test-add-route-lr"
	routeTable := util.MainRouteTable
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("normal route", func(t *testing.T) {
		t.Parallel()

		ipPrefixes := [...]string{"192.168.30.0/24", "fd00:100:64::4/64"}
		nexthops := [...]string{"192.168.30.1", "fd00:100:64::1"}
		require.Len(t, nexthops, len(ipPrefixes))
		for i := range ipPrefixes {
			require.Equal(t, util.CheckProtocol(nexthops[i]), util.CheckProtocol(ipPrefixes[i]))
		}

		t.Run("create route", func(t *testing.T) {
			for i := range ipPrefixes {
				err = nbClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefixes[i], nil, nil, nexthops[i])
				require.NoError(t, err)
			}

			lr, err := nbClient.GetLogicalRouter(lrName, false)
			require.NoError(t, err)

			for i := range ipPrefixes {
				route, err := nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefixes[i], nexthops[i], false)
				require.NoError(t, err)
				require.Equal(t, route.Nexthop, strings.Split(nexthops[i], "/")[0])
				require.Contains(t, lr.StaticRoutes, route.UUID)
			}
		})

		t.Run("create route for non-exist logical router", func(t *testing.T) {
			for i := range ipPrefixes {
				err = nbClient.AddLogicalRouterStaticRoute("non-exist-lrName", routeTable, "", ipPrefixes[i], nil, nil, nexthops[i])
				require.ErrorContains(t, err, "not found logical router")
			}
		})

		t.Run("update route", func(t *testing.T) {
			updatedNexthops := [...]string{"192.168.30.254", "fd00:100:64::fe"}
			for i := range ipPrefixes {
				err = nbClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefixes[i], nil, nil, updatedNexthops[i])
				require.NoError(t, err)
			}

			lr, err := nbClient.GetLogicalRouter(lrName, false)
			require.NoError(t, err)

			for i := range ipPrefixes {
				routes, err := nbClient.ListLogicalRouterStaticRoutes(lrName, &routeTable, &policy, ipPrefixes[i], nil)
				require.NoError(t, err)
				require.Len(t, routes, 1)
				require.Equal(t, routes[0].Nexthop, updatedNexthops[i])
				require.Contains(t, lr.StaticRoutes, routes[0].UUID)
			}
		})
	})

	t.Run("ecmp route", func(t *testing.T) {
		t.Parallel()

		ipPrefix := "192.168.40.0/24"
		nexthops := []string{"192.168.50.1", "192.168.60.1"}

		t.Run("create route", func(t *testing.T) {
			err = nbClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nil, nil, nexthops...)
			require.NoError(t, err)

			lr, err := nbClient.GetLogicalRouter(lrName, false)
			require.NoError(t, err)

			for _, nexthop := range nexthops {
				route, err := nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, false)
				require.NoError(t, err)
				require.Contains(t, lr.StaticRoutes, route.UUID)
			}
		})

		t.Run("update route", func(t *testing.T) {
			err = nbClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nil, nil, nexthops...)
			require.NoError(t, err)
		})
	})

	t.Run("fail nb client should log err", func(t *testing.T) {
		t.Parallel()

		ipPrefix := "192.168.40.0/24"
		nexthops := []string{"192.168.50.1", "192.168.60.1"}
		err = failedNbClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nil, nil, nexthops...)
		require.Error(t, err)
	})

	t.Run("bfd id mismatch", func(t *testing.T) {
		t.Parallel()

		ipPrefix := "192.168.70.0/24"
		nexthops := []string{"192.168.80.1"}
		existingBFDID := "test-existing-bfd-id"
		newBFDID := "test-new-bfd-id"

		err = nbClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, &existingBFDID, nil, nexthops...)
		require.NoError(t, err)

		initialRoutes, err := nbClient.ListLogicalRouterStaticRoutes(lrName, &routeTable, &policy, ipPrefix, nil)
		require.NoError(t, err)
		require.Len(t, initialRoutes, 1)

		err = nbClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, &newBFDID, nil, nexthops...)
		require.NoError(t, err)

		finalRoutes, err := nbClient.ListLogicalRouterStaticRoutes(lrName, &routeTable, &policy, ipPrefix, nil)
		require.NoError(t, err)
		require.Len(t, finalRoutes, 1)
	})
}

func (suite *OvnClientTestSuite) testDeleteLogicalRouterStaticRouteByUUID() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-del-lr-route-by-uuid"
	routeTable := util.MainRouteTable
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
	ipPrefix := "1.1.1.0/24"
	nexthop := "192.168.0.1"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = nbClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nil, nil, nexthop)
	require.NoError(t, err)

	route, err := nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, true)
	require.NoError(t, err)
	require.NotNil(t, route)

	err = nbClient.DeleteLogicalRouterStaticRouteByUUID(lrName, route.UUID)
	require.NoError(t, err)

	route, err = nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, true)
	require.NoError(t, err)
	require.Nil(t, route)
}

func (suite *OvnClientTestSuite) testDeleteLogicalRouterStaticRouteByExternalIDs() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-del-lr-route-by-ext-ids"
	routeTable := util.MainRouteTable
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
	nexthop := "192.168.0.1"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = nbClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, "1.1.0.0/24", nil, map[string]string{"k1": "v1"}, nexthop)
	require.NoError(t, err)

	err = nbClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, "1.1.1.0/24", nil, map[string]string{"k1": "v1", "k2": "v2"}, nexthop)
	require.NoError(t, err)

	err = nbClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, "1.1.2.0/24", nil, map[string]string{"k1": "v1", "k3": "v3"}, nexthop)
	require.NoError(t, err)

	routes, err := nbClient.ListLogicalRouterStaticRoutes(lrName, nil, nil, "", map[string]string{"k1": "v1"})
	require.NoError(t, err)
	require.Len(t, routes, 3)

	routes, err = nbClient.ListLogicalRouterStaticRoutes(lrName, nil, nil, "", map[string]string{"k2": "v2"})
	require.NoError(t, err)
	require.Len(t, routes, 1)

	routes, err = nbClient.ListLogicalRouterStaticRoutes(lrName, nil, nil, "", map[string]string{"k3": "v3"})
	require.NoError(t, err)
	require.Len(t, routes, 1)

	err = nbClient.DeleteLogicalRouterStaticRouteByExternalIDs(lrName, map[string]string{"foo": "bar"})
	require.NoError(t, err)

	routes, err = nbClient.ListLogicalRouterStaticRoutes(lrName, nil, nil, "", map[string]string{"k1": "v1"})
	require.NoError(t, err)
	require.Len(t, routes, 3)

	routes, err = nbClient.ListLogicalRouterStaticRoutes(lrName, nil, nil, "", map[string]string{"k2": "v2"})
	require.NoError(t, err)
	require.Len(t, routes, 1)

	routes, err = nbClient.ListLogicalRouterStaticRoutes(lrName, nil, nil, "", map[string]string{"k3": "v3"})
	require.NoError(t, err)
	require.Len(t, routes, 1)

	err = nbClient.DeleteLogicalRouterStaticRouteByExternalIDs(lrName, map[string]string{"k2": "v2"})
	require.NoError(t, err)

	routes, err = nbClient.ListLogicalRouterStaticRoutes(lrName, nil, nil, "", map[string]string{"k1": "v1"})
	require.NoError(t, err)
	require.Len(t, routes, 2)

	routes, err = nbClient.ListLogicalRouterStaticRoutes(lrName, nil, nil, "", map[string]string{"k2": "v2"})
	require.NoError(t, err)
	require.Len(t, routes, 0)

	routes, err = nbClient.ListLogicalRouterStaticRoutes(lrName, nil, nil, "", map[string]string{"k3": "v3"})
	require.NoError(t, err)
	require.Len(t, routes, 1)

	err = nbClient.DeleteLogicalRouterStaticRouteByExternalIDs(lrName, map[string]string{"k1": "v1"})
	require.NoError(t, err)

	routes, err = nbClient.ListLogicalRouterStaticRoutes(lrName, nil, nil, "", map[string]string{"k1": "v1"})
	require.NoError(t, err)
	require.Len(t, routes, 0)

	routes, err = nbClient.ListLogicalRouterStaticRoutes(lrName, nil, nil, "", map[string]string{"k2": "v2"})
	require.NoError(t, err)
	require.Len(t, routes, 0)

	routes, err = nbClient.ListLogicalRouterStaticRoutes(lrName, nil, nil, "", map[string]string{"k3": "v3"})
	require.NoError(t, err)
	require.Len(t, routes, 0)
}

func (suite *OvnClientTestSuite) testDeleteLogicalRouterStaticRoute() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-del-route-lr"
	routeTable := util.MainRouteTable
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("normal route", func(t *testing.T) {
		ipPrefix := "192.168.30.0/24"
		nexthop := "192.168.30.1"

		err = nbClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nil, nil, nexthop)
		require.NoError(t, err)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		route, err := nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, false)
		require.NoError(t, err)
		require.Contains(t, lr.StaticRoutes, route.UUID)

		err = nbClient.DeleteLogicalRouterStaticRoute(lrName, &routeTable, &policy, ipPrefix, nexthop)
		require.NoError(t, err)

		lr, err = nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.StaticRoutes)

		_, err = nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, false)
		require.ErrorContains(t, err, "not found")

		// delete non-exist route
		ipPrefix = "192.168.40.0/24"
		nexthop = "192.168.40.1"
		err = nbClient.DeleteLogicalRouterStaticRoute(lrName, &routeTable, &policy, ipPrefix, nexthop)
		require.NoError(t, err)
	})

	t.Run("delete static route for non-exist logical router", func(t *testing.T) {
		ipPrefix := "192.168.30.0/24"
		nexthop := "192.168.30.1"

		err := nbClient.DeleteLogicalRouterStaticRoute("non-exist-lrName", &routeTable, nil, ipPrefix, nexthop)
		require.NoError(t, err)
	})

	t.Run("ecmp policy route", func(t *testing.T) {
		ipPrefix := "192.168.40.0/24"
		nexthops := []string{"192.168.50.1", "192.168.60.1"}

		err = nbClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nil, nil, nexthops...)
		require.NoError(t, err)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		for _, nexthop := range nexthops {
			route, err := nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, false)
			require.NoError(t, err)
			require.Contains(t, lr.StaticRoutes, route.UUID)
		}

		/* delete first route */
		err = nbClient.DeleteLogicalRouterStaticRoute(lrName, &routeTable, &policy, ipPrefix, nexthops[0])
		require.NoError(t, err)

		lr, err = nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		_, err = nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthops[0], false)
		require.ErrorContains(t, err, `not found logical router test-del-route-lr static route 'policy dst-ip ip_prefix 192.168.40.0/24 nexthop 192.168.50.1'`)

		route, err := nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthops[1], false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{route.UUID}, lr.StaticRoutes)

		/* delete second route */
		err = nbClient.DeleteLogicalRouterStaticRoute(lrName, &routeTable, &policy, ipPrefix, nexthops[1])
		require.NoError(t, err)

		lr, err = nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.StaticRoutes)

		_, err = nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthops[1], false)
		require.ErrorContains(t, err, `not found logical router test-del-route-lr static route 'policy dst-ip ip_prefix 192.168.40.0/24 nexthop 192.168.60.1'`)
	})
}

func (suite *OvnClientTestSuite) testClearLogicalRouterStaticRoute() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-clear-route-lr"
	routeTable := util.MainRouteTable
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
	ipPrefixes := []string{"192.168.30.0/24", "192.168.40.0/24"}
	nexthops := []string{"192.168.30.1", "192.168.40.1"}
	routes := make([]*ovnnb.LogicalRouterStaticRoute, 0, 5)

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	for i, ipPrefix := range ipPrefixes {
		route, err := nbClient.newLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthops[i], nil, nil)
		require.NoError(t, err)

		routes = append(routes, route)
	}

	err = nbClient.CreateLogicalRouterStaticRoutes(lrName, append(routes, nil)...)
	require.NoError(t, err)

	lr, err := nbClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)
	require.Len(t, lr.StaticRoutes, 2)

	err = nbClient.ClearLogicalRouterStaticRoute(lrName)
	require.NoError(t, err)

	for _, ipPrefix := range ipPrefixes {
		_, err = nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, "", false)
		require.ErrorContains(t, err, "not found")
	}

	lr, err = nbClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)
	require.Empty(t, lr.StaticRoutes)

	// clear logical router static route for non-exist logical router
	err = nbClient.ClearLogicalRouterStaticRoute("non-exist-lrName")
	require.ErrorContains(t, err, "not found logical router")
}

func (suite *OvnClientTestSuite) testGetLogicalRouterStaticRoute() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test_get_route_lr"
	routeTable := util.MainRouteTable

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("normal route", func(t *testing.T) {
		t.Parallel()
		policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
		ipPrefix := "192.168.30.0/24"
		nexthop := "192.168.30.1"

		err := nbClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nil, nil, nexthop)
		require.NoError(t, err)

		t.Run("found route", func(t *testing.T) {
			_, err := nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, false)
			require.NoError(t, err)
		})

		t.Run("policy is different", func(t *testing.T) {
			_, err := nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, ovnnb.LogicalRouterStaticRoutePolicySrcIP, ipPrefix, nexthop, false)
			require.ErrorContains(t, err, "not found")
		})

		t.Run("ip_prefix is different", func(t *testing.T) {
			_, err := nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, "192.168.30.10", nexthop, false)
			require.ErrorContains(t, err, "not found")
		})

		t.Run("logical router name is different", func(t *testing.T) {
			_, err := nbClient.GetLogicalRouterStaticRoute(lrName+"x", routeTable, policy, ipPrefix, nexthop, false)
			require.ErrorContains(t, err, "not found")
		})
	})

	t.Run("ecmp route", func(t *testing.T) {
		t.Parallel()
		policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
		ipPrefix := "192.168.40.0/24"
		nexthop := "192.168.40.1"

		err := nbClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nil, nil, nexthop)
		require.NoError(t, err)

		t.Run("found route", func(t *testing.T) {
			_, err := nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, false)
			require.NoError(t, err)
		})

		t.Run("nexthop is different", func(t *testing.T) {
			_, err := nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop+"1", false)
			require.ErrorContains(t, err, "not found")
		})
	})
}

func (suite *OvnClientTestSuite) testListLogicalRouterStaticRoutes() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-list-routes-filters-lr"
	routeTable := util.MainRouteTable
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
	routes := make([]*ovnnb.LogicalRouterStaticRoute, 0)

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	// Create test routes with different external IDs
	testData := []struct {
		ipPrefix    string
		nexthop     string
		externalIDs map[string]string
	}{
		{"192.168.60.0/24", "192.168.60.1", map[string]string{"key1": "value1", "key2": "value2"}},
		{"192.168.70.0/24", "192.168.70.1", map[string]string{"key1": "value1"}},
		{"192.168.80.0/24", "192.168.80.1", map[string]string{"key2": "value2"}},
		{"192.168.90.0/24", "192.168.90.1", map[string]string{}},
	}

	for _, data := range testData {
		route, err := nbClient.newLogicalRouterStaticRoute(lrName, routeTable, policy, data.ipPrefix, data.nexthop, nil, nil)
		require.NoError(t, err)
		route.ExternalIDs = data.externalIDs
		routes = append(routes, route)
	}

	err = nbClient.CreateLogicalRouterStaticRoutes(lrName, routes...)
	require.NoError(t, err)

	t.Run("filter by external IDs with multiple keys", func(t *testing.T) {
		result, err := nbClient.ListLogicalRouterStaticRoutes(lrName, nil, nil, "", map[string]string{"key1": "value1", "key2": "value2"})
		require.NoError(t, err)
		require.Len(t, result, 1)
		require.Equal(t, "192.168.60.0/24", result[0].IPPrefix)
	})

	t.Run("filter by external IDs key existence only", func(t *testing.T) {
		result, err := nbClient.ListLogicalRouterStaticRoutes(lrName, nil, nil, "", map[string]string{"key1": ""})
		require.NoError(t, err)
		require.Len(t, result, 2)
	})

	t.Run("filter by route table and external IDs", func(t *testing.T) {
		customTable := "custom"
		result, err := nbClient.ListLogicalRouterStaticRoutes(lrName, &customTable, nil, "", map[string]string{"key1": "value1"})
		require.NoError(t, err)
		require.Empty(t, result)
	})

	t.Run("filter with non-matching external IDs", func(t *testing.T) {
		result, err := nbClient.ListLogicalRouterStaticRoutes(lrName, nil, nil, "", map[string]string{"nonexistent": "value"})
		require.NoError(t, err)
		require.Empty(t, result)
	})

	t.Run("filter with policy and external IDs", func(t *testing.T) {
		srcPolicy := ovnnb.LogicalRouterStaticRoutePolicySrcIP
		result, err := nbClient.ListLogicalRouterStaticRoutes(lrName, nil, &srcPolicy, "", map[string]string{"key1": "value1"})
		require.NoError(t, err)
		require.Empty(t, result)
	})

	t.Run("filter with IP prefix and external IDs", func(t *testing.T) {
		result, err := nbClient.ListLogicalRouterStaticRoutes(lrName, nil, nil, "192.168.70.0/24", map[string]string{"key1": "value1"})
		require.NoError(t, err)
		require.Len(t, result, 1)
		require.Equal(t, "192.168.70.0/24", result[0].IPPrefix)
	})
}

func (suite *OvnClientTestSuite) testNewLogicalRouterStaticRoute() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-new-route-lr"
	routeTable := util.MainRouteTable
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
	ipPrefix := "192.168.30.0/24"
	nexthop := "192.168.30.1"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	expect := &ovnnb.LogicalRouterStaticRoute{
		Policy:   &policy,
		IPPrefix: ipPrefix,
		Nexthop:  nexthop,
		BFD:      nil,
	}

	route, err := nbClient.newLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, nil, nil)
	require.NoError(t, err)
	expect.UUID = route.UUID
	require.Equal(t, expect, route)

	t.Run("with custom options", func(t *testing.T) {
		policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
		ipPrefix := "192.168.100.0/24"
		nexthop := "192.168.100.1"

		customOption := func(route *ovnnb.LogicalRouterStaticRoute) {
			route.Options = map[string]string{
				"ecmp_symmetric_reply": "true",
				"method":               "redirect",
			}
		}

		route, err := nbClient.newLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, nil, nil, customOption)
		require.NoError(t, err)
		require.Equal(t, "true", route.Options["ecmp_symmetric_reply"])
		require.Equal(t, "redirect", route.Options["method"])
	})

	t.Run("with multiple options and bfd", func(t *testing.T) {
		policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
		ipPrefix := "192.168.110.0/24"
		nexthop := "192.168.110.1"
		bfdID := "test-bfd"

		option1 := func(route *ovnnb.LogicalRouterStaticRoute) {
			route.ExternalIDs = map[string]string{"key1": "value1"}
		}
		option2 := func(route *ovnnb.LogicalRouterStaticRoute) {
			if route.Options == nil {
				route.Options = make(map[string]string)
			}
			route.Options["test"] = "value"
		}

		route, err := nbClient.newLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, &bfdID, nil, option1, option2)
		require.NoError(t, err)
		require.Equal(t, "value1", route.ExternalIDs["key1"])
		require.Equal(t, "value", route.Options["test"])
		require.Equal(t, "true", route.Options[util.StaticRouteBfdEcmp])
		require.Equal(t, &bfdID, route.BFD)
	})

	t.Run("with empty policy and existing route", func(t *testing.T) {
		ipPrefix := "192.168.120.0/24"
		nexthop := "192.168.120.1"

		route1, err := nbClient.newLogicalRouterStaticRoute(lrName, routeTable, "", ipPrefix, nexthop, nil, nil)
		require.NoError(t, err)
		err = nbClient.CreateLogicalRouterStaticRoutes(lrName, route1)
		require.NoError(t, err)

		route2, err := nbClient.newLogicalRouterStaticRoute(lrName, routeTable, "", ipPrefix, nexthop, nil, nil)
		require.Nil(t, route2)
		require.NoError(t, err)
	})

	t.Run("with bfd but no options", func(t *testing.T) {
		policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
		ipPrefix := "192.168.130.0/24"
		nexthop := "192.168.130.1"
		bfdID := "test-bfd-2"

		route, err := nbClient.newLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, &bfdID, nil)
		require.NoError(t, err)
		require.Equal(t, "true", route.Options[util.StaticRouteBfdEcmp])
		require.Equal(t, &bfdID, route.BFD)
	})

	t.Run("with empty logical switch name", func(t *testing.T) {
		policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
		ipPrefix := "192.168.130.0/24"
		nexthop := "192.168.130.1"
		bfdID := "test-bfd-2"

		route, err := nbClient.newLogicalRouterStaticRoute("", routeTable, policy, ipPrefix, nexthop, &bfdID, nil)
		require.ErrorContains(t, err, "the logical router name is required")
		require.Nil(t, route)
	})
}

func (suite *OvnClientTestSuite) testListLogicalRouterStaticRoutesByOption() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-list-routes-by-option-lr"
	routeTable := util.MainRouteTable
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
	ipPrefixes := []string{"192.168.30.0/24", "192.168.40.0/24", "192.168.50.0/24"}
	nexthops := []string{"192.168.30.1", "192.168.40.1", "192.168.50.1"}
	routes := make([]*ovnnb.LogicalRouterStaticRoute, 0, 5)

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	for i, ipPrefix := range ipPrefixes {
		var bfdID *string
		if ipPrefixes[i] == "192.168.30.0/24" {
			bfdIDValue := "bfd-id"
			bfdID = &bfdIDValue
		}
		route, err := nbClient.newLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthops[i], bfdID, nil)
		require.NoError(t, err)

		routes = append(routes, route)
	}

	err = nbClient.CreateLogicalRouterStaticRoutes(lrName, append(routes, nil)...)
	require.NoError(t, err)

	t.Run("match single route with option", func(t *testing.T) {
		result, err := nbClient.ListLogicalRouterStaticRoutesByOption(lrName, "", util.StaticRouteBfdEcmp, "true")
		require.NoError(t, err)
		require.Len(t, result, 1)
		require.Equal(t, "192.168.30.0/24", result[0].IPPrefix)
	})
}

func (suite *OvnClientTestSuite) testUpdateLogicalRouterStaticRoute() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-update-route-lr"
	routeTable := util.MainRouteTable
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
	ipPrefix := "192.168.30.0/24"
	nexthop := "192.168.30.1"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("update nil route", func(t *testing.T) {
		err := nbClient.UpdateLogicalRouterStaticRoute(nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "route is nil")
	})

	t.Run("update route fields", func(t *testing.T) {
		err := nbClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nil, nil, nexthop)
		require.NoError(t, err)

		route, err := nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, false)
		require.NoError(t, err)

		newPolicy := ovnnb.LogicalRouterStaticRoutePolicySrcIP
		route.Policy = &newPolicy
		newNexthop := "192.168.30.254"
		route.Nexthop = newNexthop

		err = nbClient.UpdateLogicalRouterStaticRoute(route, &route.Policy, &route.Nexthop)
		require.NoError(t, err)

		updatedRoute, err := nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, newPolicy, ipPrefix, newNexthop, false)
		require.NoError(t, err)
		require.Equal(t, newPolicy, *updatedRoute.Policy)
		require.Equal(t, newNexthop, updatedRoute.Nexthop)
	})
}

func (suite *OvnClientTestSuite) testGetLogicalRouterStaticRouteEdgeCases() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-get-route-edge-lr"
	routeTable := util.MainRouteTable
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("empty logical router name", func(t *testing.T) {
		_, err := nbClient.GetLogicalRouterStaticRoute("", routeTable, policy, "192.168.1.0/24", "192.168.1.1", false)
		require.Error(t, err)
		require.Contains(t, err.Error(), "the logical router name is required")
	})

	t.Run("duplicate routes", func(t *testing.T) {
		ipPrefix := "192.168.2.0/24"
		nexthop := "192.168.2.1"

		route1, err := nbClient.newLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, nil, nil)
		require.NoError(t, err)
		route2, err := nbClient.newLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, nil, nil)
		require.NoError(t, err)

		err = nbClient.CreateLogicalRouterStaticRoutes(lrName, route1, route2)
		require.NoError(t, err)

		_, err = nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, false)
		require.Error(t, err)
		require.Contains(t, err.Error(), "more than one static route")
	})

	t.Run("ignore not found true", func(t *testing.T) {
		route, err := nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, "192.168.3.0/24", "192.168.3.1", true)
		require.NoError(t, err)
		require.Nil(t, route)
	})

	t.Run("empty route table", func(t *testing.T) {
		ipPrefix := "192.168.4.0/24"
		nexthop := "192.168.4.1"

		err := nbClient.AddLogicalRouterStaticRoute(lrName, "", policy, ipPrefix, nil, nil, nexthop)
		require.NoError(t, err)

		route, err := nbClient.GetLogicalRouterStaticRoute(lrName, "", policy, ipPrefix, nexthop, false)
		require.NoError(t, err)
		require.Equal(t, "", route.RouteTable)
	})
}

func (suite *OvnClientTestSuite) testBatchDeleteLogicalRouterStaticRoute() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-batch-del-route-lr"
	routeTable := util.MainRouteTable
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
	ipPrefix := "192.168.30.0/24"
	nexthop := "192.168.30.1"
	staticRouter := &ovnnb.LogicalRouterStaticRoute{
		Policy:     &policy,
		IPPrefix:   ipPrefix,
		Nexthop:    nexthop,
		RouteTable: routeTable,
	}

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("normal static route", func(t *testing.T) {
		err = nbClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nil, nil, nexthop)
		require.NoError(t, err)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		route, err := nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, false)
		require.NoError(t, err)
		require.Contains(t, lr.StaticRoutes, route.UUID)

		err = nbClient.BatchDeleteLogicalRouterStaticRoute(lrName, []*ovnnb.LogicalRouterStaticRoute{staticRouter})
		require.NoError(t, err)

		lr, err = nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.StaticRoutes)

		_, err = nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, false)
		require.ErrorContains(t, err, "not found")
	})

	t.Run("delete non-exist static route", func(t *testing.T) {
		staticRouter.IPPrefix = "192.168.40.0/24"
		staticRouter.Nexthop = "192.168.40.1"
		err = nbClient.BatchDeleteLogicalRouterStaticRoute(lrName, []*ovnnb.LogicalRouterStaticRoute{staticRouter})
		require.NoError(t, err)
	})

	t.Run("delete ecmp policy route", func(t *testing.T) {
		ipPrefix := "192.168.40.0/24"
		nexthops := []string{"192.168.50.1", "192.168.60.1"}
		staticRouter.IPPrefix = ipPrefix

		err = nbClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nil, nil, nexthops...)
		require.NoError(t, err)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		for _, nexthop := range nexthops {
			route, err := nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, false)
			require.NoError(t, err)
			require.Contains(t, lr.StaticRoutes, route.UUID)
		}

		/* delete first route */
		staticRouter.Nexthop = nexthops[0]
		err = nbClient.BatchDeleteLogicalRouterStaticRoute(lrName, []*ovnnb.LogicalRouterStaticRoute{staticRouter})
		require.NoError(t, err)

		lr, err = nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		_, err = nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthops[0], false)
		require.ErrorContains(t, err, `not found logical router test-batch-del-route-lr static route 'policy dst-ip ip_prefix 192.168.40.0/24 nexthop 192.168.50.1'`)

		route, err := nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthops[1], false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{route.UUID}, lr.StaticRoutes)

		/* delete second route */
		staticRouter.Nexthop = nexthops[1]
		err = nbClient.DeleteLogicalRouterStaticRoute(lrName, &routeTable, &policy, ipPrefix, nexthops[1])
		require.NoError(t, err)

		lr, err = nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.StaticRoutes)

		_, err = nbClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthops[1], false)
		require.ErrorContains(t, err, `not found logical router test-batch-del-route-lr static route 'policy dst-ip ip_prefix 192.168.40.0/24 nexthop 192.168.60.1'`)
	})

	t.Run("delete static route for non-exist logical router", func(t *testing.T) {
		err := nbClient.BatchDeleteLogicalRouterStaticRoute("non-exist-lrName", []*ovnnb.LogicalRouterStaticRoute{staticRouter})
		require.NoError(t, err)
	})
}
