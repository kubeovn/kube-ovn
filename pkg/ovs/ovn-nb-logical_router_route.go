package ovs

import (
	"errors"
	"fmt"
	"slices"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/scylladb/go-set/strset"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"k8s.io/utils/set"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *OVNNbClient) ListLogicalRouterStaticRoutesByOption(lrName, _, key, value string) ([]*ovnnb.LogicalRouterStaticRoute, error) {
	fnFilter := func(route *ovnnb.LogicalRouterStaticRoute) bool {
		if len(route.Options) != 0 {
			if _, ok := route.Options[key]; ok {
				return route.Options[key] == value
			}
		}
		return false
	}
	return c.listLogicalRouterStaticRoutesByFilter(lrName, fnFilter)
}

// CreateLogicalRouterStaticRoutes create several logical router static route once
func (c *OVNNbClient) CreateLogicalRouterStaticRoutes(lrName string, routes ...*ovnnb.LogicalRouterStaticRoute) error {
	models, uuids := modelsAndUUIDs(routes, func(route *ovnnb.LogicalRouterStaticRoute) string { return route.UUID })
	ops, err := createAndAttachOps(c, &ovnnb.LogicalRouterStaticRoute{}, models, func(ids []string) ([]ovsdb.Operation, error) {
		return c.LogicalRouterUpdateStaticRouteOp(lrName, ids, ovsdb.MutateOperationInsert)
	}, uuids)
	return c.transactGenerated("lr-routes-add", ops, err,
		wrapErr("generate operations for adding static routes to logical router %s: %w", lrName),
		wrapErr("add static routes to %s: %w", lrName),
	)
}

// AddLogicalRouterStaticRoute add a logical router static route
func (c *OVNNbClient) AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix string, bfdID *string, externalIDs map[string]string, nexthops ...string) error {
	if len(policy) == 0 {
		policy = ovnnb.LogicalRouterStaticRoutePolicyDstIP
	}

	routes, err := c.ListLogicalRouterStaticRoutes(lrName, &routeTable, &policy, ipPrefix, nil)
	if err != nil {
		return logErr(err)
	}

	existing := strset.New()
	var toDel []string
	for _, route := range routes {
		if slices.Contains(nexthops, route.Nexthop) {
			existing.Add(route.Nexthop)
		} else {
			if route.BFD != nil && bfdID != nil && *route.BFD != *bfdID {
				continue
			}
			toDel = append(toDel, route.UUID)
		}
	}
	var toAdd []*ovnnb.LogicalRouterStaticRoute
	for _, nexthop := range nexthops {
		if !existing.Has(nexthop) {
			route, err := c.newLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, bfdID, externalIDs)
			if err != nil {
				return logErr(err)
			}
			toAdd = append(toAdd, route)
		}
	}
	if len(toDel) != 0 {
		klog.Infof("logical router %s del static routes: %v", lrName, toDel)
	}
	if err = c.detachRouterUUIDs(lrName, toDel, "lr-route-del", "static routes", c.LogicalRouterUpdateStaticRouteOp); err != nil {
		return err
	}
	if err = c.CreateLogicalRouterStaticRoutes(lrName, toAdd...); err != nil {
		return logWrap(err, wrapErr("failed to add static routes to logical router %s: %w", lrName))
	}
	return nil
}

// UpdateLogicalRouterStaticRoute update logical router static route
func (c *OVNNbClient) UpdateLogicalRouterStaticRoute(route *ovnnb.LogicalRouterStaticRoute, fields ...any) error {
	if route == nil {
		return errors.New("route is nil")
	}

	return c.updateModelLogged("net-update", route, func(err error) error {
		return fmt.Errorf("update logical router static route 'policy %s ip_prefix %s': %w", *route.Policy, route.IPPrefix, err)
	}, fields...)
}

// DeleteLogicalRouterStaticRoute delete a logical router static route
func (c *OVNNbClient) DeleteLogicalRouterStaticRoute(lrName string, routeTable, policy *string, ipPrefix, nexthop string) error {
	if policy == nil || len(*policy) == 0 {
		policy = ptr.To(ovnnb.LogicalRouterStaticRoutePolicyDstIP)
	}
	lr, err := c.GetLogicalRouter(lrName, true)
	if err != nil {
		return logErr(err)
	}
	if lr == nil {
		return nil
	}
	routes, err := c.ListLogicalRouterStaticRoutes(lrName, routeTable, policy, ipPrefix, nil)
	if err != nil {
		return logErr(err)
	}
	uuids := make([]string, 0, len(routes))
	for _, route := range routes {
		if nexthop == "" || route.Nexthop == nexthop {
			uuids = append(uuids, route.UUID)
		}
	}
	return c.detachRouterUUIDs(lrName, uuids, "lr-route-del", fmt.Sprintf("static routes %v", uuids), c.LogicalRouterUpdateStaticRouteOp)
}

func (c *OVNNbClient) DeleteLogicalRouterStaticRouteByUUID(lrName, uuid string) error {
	lr, err := c.GetLogicalRouter(lrName, true)
	if err != nil {
		return err
	}
	if lr == nil {
		return nil
	}
	return c.detachRouterUUIDs(lrName, []string{uuid}, "lr-route-del", fmt.Sprintf("static route %s", uuid), c.LogicalRouterUpdateStaticRouteOp)
}

func (c *OVNNbClient) DeleteLogicalRouterStaticRouteByExternalIDs(lrName string, externalIDs map[string]string) error {
	lr, err := c.GetLogicalRouter(lrName, true)
	if err != nil {
		return err
	}
	if lr == nil {
		return nil
	}
	routes, err := c.ListLogicalRouterStaticRoutes(lrName, nil, nil, "", externalIDs)
	if err != nil {
		return logErr(err)
	}
	uuids := rowUUIDs(routes, func(route *ovnnb.LogicalRouterStaticRoute) string { return route.UUID })
	return c.detachRouterUUIDs(lrName, uuids, "lr-route-del", fmt.Sprintf("static routes %v", uuids), c.LogicalRouterUpdateStaticRouteOp)
}

func (c *OVNNbClient) BatchDeleteLogicalRouterStaticRoute(lrName string, staticRoutes []*ovnnb.LogicalRouterStaticRoute) error {
	lr, err := c.GetLogicalRouter(lrName, true)
	if err != nil {
		return logErr(err)
	}
	if lr == nil {
		return nil
	}

	staticRoutesMap := make(map[string]string, len(staticRoutes))
	for _, route := range staticRoutes {
		if route == nil {
			continue
		}
		if route.Policy == nil {
			route.Policy = ptr.To(ovnnb.LogicalRouterStaticRoutePolicyDstIP)
		}

		staticRoutesMap[createStaticRouteKey(route.RouteTable, *route.Policy, route.IPPrefix)] = route.Nexthop
	}
	routes, err := c.batchListLogicalRouterStaticRoutesForDelete(staticRoutesMap, lr.StaticRoutes)
	if err != nil {
		return logErr(err)
	}

	// not found, skip
	if len(routes) == 0 {
		return nil
	}

	uuids := make([]string, 0, len(routes))
	for _, route := range routes {
		key := createStaticRouteKey(route.RouteTable, *route.Policy, route.IPPrefix)
		nexthop, exists := staticRoutesMap[key]
		if exists && (nexthop == "" || route.Nexthop == nexthop) {
			uuids = append(uuids, route.UUID)
		}
	}

	return c.detachRouterUUIDs(lrName, uuids, "lr-route-del", fmt.Sprintf("static routes %v", uuids), c.LogicalRouterUpdateStaticRouteOp)
}

// ClearLogicalRouterStaticRoute clear static route from logical router once
func (c *OVNNbClient) ClearLogicalRouterStaticRoute(lrName string) error {
	lr, err := c.GetLogicalRouter(lrName, false)
	if err != nil {
		return logWrap(err, wrapErr("get logical router %s: %w", lrName))
	}

	// clear static route
	lr.StaticRoutes = nil
	ops, err := c.UpdateLogicalRouterOp(lr, &lr.StaticRoutes)
	return c.transactGenerated("lr-route-clear", ops, err,
		wrapErr("generate operations for clearing logical router %s static route: %w", lrName),
		wrapErr("clear logical router %s static routes: %w", lrName),
	)
}

// GetLogicalRouterStaticRoute get logical router static route by some attribute,
// a static route is uniquely identified by router(lrName), policy and ipPrefix when route is not ecmp
// a static route is uniquely identified by router(lrName), policy, ipPrefix and nexthop when route is ecmp
func (c *OVNNbClient) GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop string, ignoreNotFound bool) (*ovnnb.LogicalRouterStaticRoute, error) {
	// this is necessary because may exist same static route in different logical router
	if len(lrName) == 0 {
		return nil, errors.New("the logical router name is required")
	}

	fnFilter := func(route *ovnnb.LogicalRouterStaticRoute) bool {
		return route.RouteTable == routeTable && route.Policy != nil && *route.Policy == policy && route.IPPrefix == ipPrefix && route.Nexthop == nexthop
	}
	routeList, err := c.listLogicalRouterStaticRoutesByFilter(lrName, fnFilter)
	if err != nil {
		return nil, logWrap(err, wrapErr("get logical router %s static route 'policy %s ip_prefix %s nexthop %s': %w", lrName, policy, ipPrefix, nexthop))
	}

	return uniquePtrs(routeList, ignoreNotFound,
		fmt.Errorf("not found logical router %s static route 'policy %s ip_prefix %s nexthop %s'", lrName, policy, ipPrefix, nexthop),
		fmt.Errorf("more than one static route 'policy %s ip_prefix %s nexthop %s' in logical router %s", policy, ipPrefix, nexthop, lrName),
	)
}

// ListLogicalRouterStaticRoutes list route which match the given externalIDs
func (c *OVNNbClient) ListLogicalRouterStaticRoutes(lrName string, routeTable, policy *string, ipPrefix string, externalIDs map[string]string) ([]*ovnnb.LogicalRouterStaticRoute, error) {
	fnFilter := func(route *ovnnb.LogicalRouterStaticRoute) bool {
		if !matchExternalIDs(route.ExternalIDs, externalIDs) {
			return false
		}
		if routeTable != nil && route.RouteTable != *routeTable {
			return false
		}
		if policy != nil {
			if route.Policy != nil {
				if *route.Policy != *policy {
					return false
				}
			} else if *policy != ovnnb.LogicalRouterStaticRoutePolicyDstIP {
				return false
			}
		}
		return ipPrefix == "" || route.IPPrefix == ipPrefix
	}
	return c.listLogicalRouterStaticRoutesByFilter(lrName, fnFilter)
}

func (c *OVNNbClient) LogicalRouterStaticRouteExists(lrName, routeTable, policy, ipPrefix, nexthop string) (bool, error) {
	route, err := c.GetLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop, true)
	return route != nil, err
}

// newLogicalRouterStaticRoute return logical router static route with basic information
func (c *OVNNbClient) newLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop string, bfdID *string, externalIDs map[string]string, options ...func(route *ovnnb.LogicalRouterStaticRoute)) (*ovnnb.LogicalRouterStaticRoute, error) {
	if len(lrName) == 0 {
		return nil, errors.New("the logical router name is required")
	}

	if len(policy) == 0 {
		policy = ovnnb.LogicalRouterStaticRoutePolicyDstIP
	}

	exists, err := c.LogicalRouterStaticRouteExists(lrName, routeTable, policy, ipPrefix, nexthop)
	if err != nil {
		return nil, logWrap(err, wrapErr("get logical router %s route: %w", lrName))
	}

	// found, ignore
	if exists {
		return nil, nil
	}

	route := &ovnnb.LogicalRouterStaticRoute{
		UUID:        ovsclient.NamedUUID(),
		Policy:      &policy,
		IPPrefix:    ipPrefix,
		Nexthop:     nexthop,
		RouteTable:  routeTable,
		ExternalIDs: externalIDs,
	}
	for _, option := range options {
		option(route)
	}

	if bfdID != nil {
		route.BFD = bfdID
		if route.Options == nil {
			route.Options = make(map[string]string)
		}
		route.Options[util.StaticRouteBfdEcmp] = "true"
	}
	return route, nil
}

func (c *OVNNbClient) listLogicalRouterStaticRoutesByFilter(lrName string, filter func(route *ovnnb.LogicalRouterStaticRoute) bool) ([]*ovnnb.LogicalRouterStaticRoute, error) {
	return c.listRouterChildren(lrName, func(lr *ovnnb.LogicalRouter) []string { return lr.StaticRoutes }, &ovnnb.LogicalRouterStaticRoute{}, func(route *ovnnb.LogicalRouterStaticRoute) string { return route.UUID }, filter)
}

// batchListLogicalRouterStaticRoutesForDelete batch list route which match the given condition when need delete static route
func (c *OVNNbClient) batchListLogicalRouterStaticRoutesForDelete(staticRoutes map[string]string, lrStaticRoute []string) ([]*ovnnb.LogicalRouterStaticRoute, error) {
	lrStaticRouteSet := set.New(lrStaticRoute...)
	fnFilter := func(route *ovnnb.LogicalRouterStaticRoute) bool {
		if !lrStaticRouteSet.Has(route.UUID) {
			return false
		}

		if route.Policy == nil {
			route.Policy = ptr.To(ovnnb.LogicalRouterStaticRoutePolicyDstIP)
		}

		key := createStaticRouteKey(route.RouteTable, *route.Policy, route.IPPrefix)
		_, exists := staticRoutes[key]
		return exists
	}

	rows, err := filterLogged(c.Database, &ovnnb.LogicalRouterStaticRoute{}, fnFilter, func(err error) error {
		return fmt.Errorf("batch list logical static router %v lr static route %v route: %w", staticRoutes, lrStaticRoute, err)
	})
	if err != nil {
		return nil, err
	}
	return pointersOf(rows), nil
}

func createStaticRouteKey(routeTable, policy, ipPrefix string) string {
	return fmt.Sprintf("%s-%s-%s", routeTable, policy, ipPrefix)
}
