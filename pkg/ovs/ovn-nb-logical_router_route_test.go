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
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
	prefixes := []string{"192.168.30.0/24", "192.168.40.0/24"}
	nextHops := []string{"192.168.30.1", "192.168.40.1"}
	routeType := util.NormalRouteType
	routes := make([]*ovnnb.LogicalRouterStaticRoute, 0, 5)

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	for i, prefix := range prefixes {
		route, err := ovnClient.newLogicalRouterStaticRoute(lrName, policy, prefix, nextHops[i], "")
		require.NoError(t, err)

		routes = append(routes, route)
	}

	err = ovnClient.CreateLogicalRouterStaticRoutes(lrName, append(routes, nil)...)
	require.NoError(t, err)

	lr, err := ovnClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)

	for i, prefix := range prefixes {
		route, err := ovnClient.GetLogicalRouterStaticRoute(lrName, policy, prefix, nextHops[i], routeType, false)
		require.NoError(t, err)

		require.Contains(t, lr.StaticRoutes, route.UUID)
	}
}

func (suite *OvnClientTestSuite) testAddLogicalRouterStaticRoute() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-add-route-lr"
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("normal route", func(t *testing.T) {
		t.Parallel()

		prefixs := "192.168.30.0/24,fd00:100:64::4/64"
		nextHops := "192.168.30.1/24,fd00:100:64::1"
		prefixList := strings.Split(prefixs, ",")
		nextHopList := strings.Split(nextHops, ",")
		routeType := util.NormalRouteType

		t.Run("create route", func(t *testing.T) {
			err = ovnClient.AddLogicalRouterStaticRoute(lrName, policy, prefixs, nextHops, routeType)
			require.NoError(t, err)

			lr, err := ovnClient.GetLogicalRouter(lrName, false)
			require.NoError(t, err)

			for i, prefix := range prefixList {
				route, err := ovnClient.GetLogicalRouterStaticRoute(lrName, policy, prefix, nextHopList[i], "", false)
				require.NoError(t, err)
				require.Equal(t, route.Nexthop, strings.Split(nextHopList[i], "/")[0])

				require.Contains(t, lr.StaticRoutes, route.UUID)
			}
		})

		t.Run("update route", func(t *testing.T) {
			nextHops := "192.168.30.254,fd00:100:64::fe"
			nextHopList := strings.Split(nextHops, ",")

			err = ovnClient.AddLogicalRouterStaticRoute(lrName, policy, prefixs, nextHops, routeType)
			require.NoError(t, err)

			lr, err := ovnClient.GetLogicalRouter(lrName, false)
			require.NoError(t, err)

			for i, prefix := range prefixList {
				route, err := ovnClient.GetLogicalRouterStaticRoute(lrName, policy, prefix, nextHopList[i], "", false)
				require.NoError(t, err)
				require.Equal(t, nextHopList[i], route.Nexthop)

				require.Contains(t, lr.StaticRoutes, route.UUID)
			}
		})
	})

	t.Run("ecmp route", func(t *testing.T) {
		t.Parallel()

		prefix := "192.168.40.0/24"
		nextHops := "192.168.50.1,192.168.60.1"
		nextHopList := strings.Split(nextHops, ",")
		routeType := util.EcmpRouteType

		t.Run("create route", func(t *testing.T) {
			err = ovnClient.AddLogicalRouterStaticRoute(lrName, policy, prefix, nextHops, routeType)
			require.NoError(t, err)

			lr, err := ovnClient.GetLogicalRouter(lrName, false)
			require.NoError(t, err)

			for _, nextHop := range nextHopList {
				route, err := ovnClient.GetLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, routeType, false)
				require.NoError(t, err)

				require.Contains(t, lr.StaticRoutes, route.UUID)
			}
		})

		t.Run("update route", func(t *testing.T) {
			err = ovnClient.AddLogicalRouterStaticRoute(lrName, policy, prefix, nextHops, routeType)
			require.NoError(t, err)
		})
	})
}

func (suite *OvnClientTestSuite) testDeleteLogicalRouterStaticRoute() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-del-route-lr"
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("normal route", func(t *testing.T) {
		prefix := "192.168.30.0/24"
		nextHop := "192.168.30.1"
		routeType := util.NormalRouteType

		err = ovnClient.AddLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, routeType)
		require.NoError(t, err)

		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		route, err := ovnClient.GetLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, "", false)
		require.NoError(t, err)
		require.Contains(t, lr.StaticRoutes, route.UUID)

		err = ovnClient.DeleteLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, "")
		require.NoError(t, err)

		lr, err = ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.StaticRoutes)

		_, err = ovnClient.GetLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, "", false)
		require.ErrorContains(t, err, "not found")
	})

	t.Run("ecmp route", func(t *testing.T) {
		prefix := "192.168.40.0/24"
		nextHops := "192.168.50.1,192.168.60.1"
		nextHopList := strings.Split(nextHops, ",")
		routeType := util.EcmpRouteType

		err = ovnClient.AddLogicalRouterStaticRoute(lrName, policy, prefix, nextHops, routeType)
		require.NoError(t, err)

		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		for _, nextHop := range nextHopList {
			route, err := ovnClient.GetLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, routeType, false)
			require.NoError(t, err)
			require.Contains(t, lr.StaticRoutes, route.UUID)
		}

		/* delete first route */
		err = ovnClient.DeleteLogicalRouterStaticRoute(lrName, policy, prefix, nextHopList[0], routeType)
		require.NoError(t, err)

		lr, err = ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		_, err = ovnClient.GetLogicalRouterStaticRoute(lrName, policy, prefix, nextHopList[0], routeType, false)
		require.ErrorContains(t, err, `not found logical router test-del-route-lr static route 'policy dst-ip prefix 192.168.40.0/24 nextHop 192.168.50.1'`)

		route, err := ovnClient.GetLogicalRouterStaticRoute(lrName, policy, prefix, nextHopList[1], routeType, false)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{route.UUID}, lr.StaticRoutes)

		/* delete second route */
		err = ovnClient.DeleteLogicalRouterStaticRoute(lrName, policy, prefix, nextHopList[1], routeType)
		require.NoError(t, err)

		lr, err = ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.StaticRoutes)

		_, err = ovnClient.GetLogicalRouterStaticRoute(lrName, policy, prefix, nextHopList[1], routeType, false)
		require.ErrorContains(t, err, `not found logical router test-del-route-lr static route 'policy dst-ip prefix 192.168.40.0/24 nextHop 192.168.60.1'`)
	})
}

func (suite *OvnClientTestSuite) testClearLogicalRouterStaticRoute() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-clear-route-lr"
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
	prefixes := []string{"192.168.30.0/24", "192.168.40.0/24"}
	nextHops := []string{"192.168.30.1", "192.168.40.1"}
	routes := make([]*ovnnb.LogicalRouterStaticRoute, 0, 5)

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	for i, prefix := range prefixes {
		route, err := ovnClient.newLogicalRouterStaticRoute(lrName, policy, prefix, nextHops[i], "")
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

	for _, prefix := range prefixes {
		_, err = ovnClient.GetLogicalRouterStaticRoute(lrName, policy, prefix, "", "", false)
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

	t.Run("normal route", func(t *testing.T) {
		t.Parallel()
		policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
		prefix := "192.168.30.0/24"
		nextHop := "192.168.30.1"
		routeType := util.NormalRouteType

		err := ovnClient.CreateBareLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, routeType)
		require.NoError(t, err)

		t.Run("found route", func(t *testing.T) {
			_, err := ovnClient.GetLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, routeType, false)
			require.NoError(t, err)
		})

		t.Run("policy is different", func(t *testing.T) {
			_, err := ovnClient.GetLogicalRouterStaticRoute(lrName, "src-ip", prefix, nextHop, routeType, false)
			require.ErrorContains(t, err, "not found")
		})

		t.Run("prefix is different", func(t *testing.T) {
			_, err := ovnClient.GetLogicalRouterStaticRoute(lrName, policy, "192.168.30.10", nextHop, routeType, false)
			require.ErrorContains(t, err, "not found")
		})

		t.Run("logical router name is different", func(t *testing.T) {
			_, err := ovnClient.GetLogicalRouterStaticRoute(lrName+"x", policy, prefix, nextHop, routeType, false)
			require.ErrorContains(t, err, "not found")
		})
	})

	t.Run("ecmp route", func(t *testing.T) {
		t.Parallel()
		policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
		prefix := "192.168.40.0/24"
		nextHop := "192.168.40.1"
		routeType := util.EcmpRouteType

		err := ovnClient.CreateBareLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, routeType)
		require.NoError(t, err)

		t.Run("found route", func(t *testing.T) {
			_, err := ovnClient.GetLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, routeType, false)
			require.NoError(t, err)
		})

		t.Run("nextHop is different", func(t *testing.T) {
			_, err := ovnClient.GetLogicalRouterStaticRoute(lrName, policy, prefix, nextHop+"1", routeType, false)
			require.ErrorContains(t, err, "not found")
		})
	})
}

func (suite *OvnClientTestSuite) testListLogicalRouterStaticRoutes() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-list-routes-lr"
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
	prefixes := []string{"192.168.30.0/24", "192.168.40.0/24", "192.168.50.0/24"}
	nextHops := []string{"192.168.30.1", "192.168.40.1", "192.168.50.1"}
	routes := make([]*ovnnb.LogicalRouterStaticRoute, 0, 5)

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	for i, prefix := range prefixes {
		route, err := ovnClient.newLogicalRouterStaticRoute(lrName, policy, prefix, nextHops[i], "")
		require.NoError(t, err)

		routes = append(routes, route)
	}

	err = ovnClient.CreateLogicalRouterStaticRoutes(lrName, append(routes, nil)...)
	require.NoError(t, err)

	t.Run("include same router routes", func(t *testing.T) {
		out, err := ovnClient.ListLogicalRouterStaticRoutes(map[string]string{logicalRouterKey: lrName})
		require.NoError(t, err)
		require.Len(t, out, 3)
	})
}

func (suite *OvnClientTestSuite) test_newLogicalRouterStaticRoute() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-new-route-lr"
	policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
	prefix := "192.168.30.0/24"
	nextHop := "192.168.30.1"
	routeType := util.NormalRouteType

	expect := &ovnnb.LogicalRouterStaticRoute{
		Policy:   &policy,
		IPPrefix: prefix,
		Nexthop:  nextHop,
		ExternalIDs: map[string]string{
			logicalRouterKey: lrName,
		},
	}

	route, err := ovnClient.newLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, routeType)
	require.NoError(t, err)
	expect.UUID = route.UUID
	require.Equal(t, expect, route)
}
