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

	ovnClient := suite.ovnClient
	lrName := "test-create-routes-lr"
	routeTable := util.MainRouteTable
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
	ipPrefixes := [...]string{"192.168.30.0/24", "192.168.40.0/24"}
	nexthops := [...]string{"192.168.30.1", "192.168.40.1"}
	routes := make([]*ovnnb.LogicalRouterStaticRoute, 0, 5)

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	for i, ipPrefix := range ipPrefixes {
		route, err := ovnClient.newLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthops[i], nil)
		require.NoError(t, err)

		routes = append(routes, route)
	}

	err = ovnClient.CreateLogicalRouterStaticRoutes(lrName, append(routes, nil)...)
	require.NoError(t, err)

	lr, err := ovnClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)

	for i, ipPrefix := range ipPrefixes {
		route, err := ovnClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthops[i], false)
		require.NoError(t, err)

		require.Contains(t, lr.StaticRoutes, route.UUID)
	}
}

func (suite *OvnClientTestSuite) testAddLogicalRouterStaticRoute() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-add-route-lr"
	routeTable := util.MainRouteTable
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP

	err := ovnClient.CreateLogicalRouter(lrName)
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
				err = ovnClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefixes[i], nil, nexthops[i])
				require.NoError(t, err)
			}

			lr, err := ovnClient.GetLogicalRouter(lrName, false)
			require.NoError(t, err)

			for i := range ipPrefixes {
				route, err := ovnClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefixes[i], nexthops[i], false)
				require.NoError(t, err)
				require.Equal(t, route.Nexthop, strings.Split(nexthops[i], "/")[0])
				require.Contains(t, lr.StaticRoutes, route.UUID)
			}
		})

		t.Run("update route", func(t *testing.T) {
			updatedNexthops := [...]string{"192.168.30.254", "fd00:100:64::fe"}
			for i := range ipPrefixes {
				err = ovnClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefixes[i], nil, updatedNexthops[i])
				require.NoError(t, err)
			}

			lr, err := ovnClient.GetLogicalRouter(lrName, false)
			require.NoError(t, err)

			for i := range ipPrefixes {
				routes, err := ovnClient.ListLogicalRouterStaticRoutes(lrName, &routeTable, &policy, ipPrefixes[i], nil)
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
			err = ovnClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nil, nexthops...)
			require.NoError(t, err)

			lr, err := ovnClient.GetLogicalRouter(lrName, false)
			require.NoError(t, err)

			for _, nexthop := range nexthops {
				route, err := ovnClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, false)
				require.NoError(t, err)
				require.Contains(t, lr.StaticRoutes, route.UUID)
			}
		})

		t.Run("update route", func(t *testing.T) {
			err = ovnClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nil, nexthops...)
			require.NoError(t, err)
		})
	})
}

func (suite *OvnClientTestSuite) testDeleteLogicalRouterStaticRoute() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-del-route-lr"
	routeTable := util.MainRouteTable
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("normal route", func(t *testing.T) {
		ipPrefix := "192.168.30.0/24"
		nexthop := "192.168.30.1"

		err = ovnClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nil, nexthop)
		require.NoError(t, err)

		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		route, err := ovnClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, false)
		require.NoError(t, err)
		require.Contains(t, lr.StaticRoutes, route.UUID)

		err = ovnClient.DeleteLogicalRouterStaticRoute(lrName, &routeTable, &policy, ipPrefix, nexthop)
		require.NoError(t, err)

		lr, err = ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.StaticRoutes)

		_, err = ovnClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, false)
		require.ErrorContains(t, err, "not found")
	})

	t.Run("ecmp policy route", func(t *testing.T) {
		ipPrefix := "192.168.40.0/24"
		nexthops := []string{"192.168.50.1", "192.168.60.1"}

		err = ovnClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nil, nexthops...)
		require.NoError(t, err)

		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		for _, nexthop := range nexthops {
			route, err := ovnClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, false)
			require.NoError(t, err)
			require.Contains(t, lr.StaticRoutes, route.UUID)
		}

		/* delete first route */
		err = ovnClient.DeleteLogicalRouterStaticRoute(lrName, &routeTable, &policy, ipPrefix, nexthops[0])
		require.NoError(t, err)

		lr, err = ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		_, err = ovnClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthops[0], false)
		require.ErrorContains(t, err, `not found logical router test-del-route-lr static route 'policy dst-ip ip_prefix 192.168.40.0/24 nexthop 192.168.50.1'`)

		route, err := ovnClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthops[1], false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{route.UUID}, lr.StaticRoutes)

		/* delete second route */
		err = ovnClient.DeleteLogicalRouterStaticRoute(lrName, &routeTable, &policy, ipPrefix, nexthops[1])
		require.NoError(t, err)

		lr, err = ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.StaticRoutes)

		_, err = ovnClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthops[1], false)
		require.ErrorContains(t, err, `not found logical router test-del-route-lr static route 'policy dst-ip ip_prefix 192.168.40.0/24 nexthop 192.168.60.1'`)
	})
}

func (suite *OvnClientTestSuite) testClearLogicalRouterStaticRoute() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-clear-route-lr"
	routeTable := util.MainRouteTable
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
	ipPrefixes := []string{"192.168.30.0/24", "192.168.40.0/24"}
	nexthops := []string{"192.168.30.1", "192.168.40.1"}
	routes := make([]*ovnnb.LogicalRouterStaticRoute, 0, 5)

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	for i, ipPrefix := range ipPrefixes {
		route, err := ovnClient.newLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthops[i], nil)
		require.NoError(t, err)

		routes = append(routes, route)
	}

	err = ovnClient.CreateLogicalRouterStaticRoutes(lrName, append(routes, nil)...)
	require.NoError(t, err)

	lr, err := ovnClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)
	require.Len(t, lr.StaticRoutes, 2)

	err = ovnClient.ClearLogicalRouterStaticRoute(lrName)
	require.NoError(t, err)

	for _, ipPrefix := range ipPrefixes {
		_, err = ovnClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, "", false)
		require.ErrorContains(t, err, "not found")
	}

	lr, err = ovnClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)
	require.Empty(t, lr.StaticRoutes)
}

func (suite *OvnClientTestSuite) testGetLogicalRouterStaticRoute() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test_get_route_lr"
	routeTable := util.MainRouteTable

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("normal route", func(t *testing.T) {
		t.Parallel()
		policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
		ipPrefix := "192.168.30.0/24"
		nexthop := "192.168.30.1"

		err := ovnClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nil, nexthop)
		require.NoError(t, err)

		t.Run("found route", func(t *testing.T) {
			_, err := ovnClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, false)
			require.NoError(t, err)
		})

		t.Run("policy is different", func(t *testing.T) {
			_, err := ovnClient.GetLogicalRouterStaticRoute(lrName, routeTable, ovnnb.LogicalRouterStaticRoutePolicySrcIP, ipPrefix, nexthop, false)
			require.ErrorContains(t, err, "not found")
		})

		t.Run("ip_prefix is different", func(t *testing.T) {
			_, err := ovnClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, "192.168.30.10", nexthop, false)
			require.ErrorContains(t, err, "not found")
		})

		t.Run("logical router name is different", func(t *testing.T) {
			_, err := ovnClient.GetLogicalRouterStaticRoute(lrName+"x", routeTable, policy, ipPrefix, nexthop, false)
			require.ErrorContains(t, err, "not found")
		})
	})

	t.Run("ecmp route", func(t *testing.T) {
		t.Parallel()
		policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
		ipPrefix := "192.168.40.0/24"
		nexthop := "192.168.40.1"

		err := ovnClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nil, nexthop)
		require.NoError(t, err)

		t.Run("found route", func(t *testing.T) {
			_, err := ovnClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, false)
			require.NoError(t, err)
		})

		t.Run("nexthop is different", func(t *testing.T) {
			_, err := ovnClient.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop+"1", false)
			require.ErrorContains(t, err, "not found")
		})
	})
}

func (suite *OvnClientTestSuite) testListLogicalRouterStaticRoutes() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-list-routes-lr"
	routeTable := util.MainRouteTable
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
	ipPrefixes := []string{"192.168.30.0/24", "192.168.40.0/24", "192.168.50.0/24"}
	nexthops := []string{"192.168.30.1", "192.168.40.1", "192.168.50.1"}
	routes := make([]*ovnnb.LogicalRouterStaticRoute, 0, 5)

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	for i, ipPrefix := range ipPrefixes {
		route, err := ovnClient.newLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthops[i], nil)
		require.NoError(t, err)

		routes = append(routes, route)
	}

	err = ovnClient.CreateLogicalRouterStaticRoutes(lrName, append(routes, nil)...)
	require.NoError(t, err)

	t.Run("include same router routes", func(t *testing.T) {
		out, err := ovnClient.ListLogicalRouterStaticRoutes(lrName, nil, nil, "", nil)
		require.NoError(t, err)
		require.Len(t, out, 3)
	})
}

func (suite *OvnClientTestSuite) testNewLogicalRouterStaticRoute() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-new-route-lr"
	routeTable := util.MainRouteTable
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
	ipPrefix := "192.168.30.0/24"
	nexthop := "192.168.30.1"

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	expect := &ovnnb.LogicalRouterStaticRoute{
		Policy:   &policy,
		IPPrefix: ipPrefix,
		Nexthop:  nexthop,
		BFD:      nil,
	}

	route, err := ovnClient.newLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, nil)
	require.NoError(t, err)
	expect.UUID = route.UUID
	require.Equal(t, expect, route)
}
