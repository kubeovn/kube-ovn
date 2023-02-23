package ovs

import (
	"context"
	"fmt"
	"strings"

	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *ovnClient) GetLogicalRouterRouteByOpts(key, value string) ([]ovnnb.LogicalRouterStaticRoute, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	api, err := c.ovnNbClient.WherePredict(ctx, func(r *ovnnb.LogicalRouterStaticRoute) bool {
		return r.Options[key] == value
	})
	if err != nil {
		return nil, err
	}

	var lrRouteList []ovnnb.LogicalRouterStaticRoute
	if err = api.List(ctx, &lrRouteList); err != nil && err != client.ErrNotFound {
		return nil, err
	}

	return lrRouteList, nil
}

func (c *ovnClient) GetLogicalRouterPoliciesByExtID(key, value string) ([]ovnnb.LogicalRouterPolicy, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	api, err := c.ovnNbClient.WherePredict(ctx, func(p *ovnnb.LogicalRouterPolicy) bool {
		return p.ExternalIDs[key] == value
	})
	if err != nil {
		return nil, err
	}

	var lrPolicyList []ovnnb.LogicalRouterPolicy
	if err = api.List(ctx, &lrPolicyList); err != nil && err != client.ErrNotFound {
		return nil, err
	}

	return lrPolicyList, nil
}

func (c *ovnClient) CreateBareLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, routeType string) error {
	route, err := c.newLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, routeType)
	if err != nil {
		return err
	}

	op, err := c.ovnNbClient.Create(route)
	if err != nil {
		return fmt.Errorf("generate operations for creating logical router static route: %v", err)
	}

	if err = c.Transact("lr-route-add", op); err != nil {
		return fmt.Errorf("create logical router static route: %v", err)
	}

	return nil
}

// CreateLogicalRouterStaticRoutes create several logical router static route once
func (c *ovnClient) CreateLogicalRouterStaticRoutes(lrName string, routes ...*ovnnb.LogicalRouterStaticRoute) error {
	if len(routes) == 0 {
		return nil
	}

	models := make([]model.Model, 0, len(routes))
	routeUUIDs := make([]string, 0, len(routes))
	for _, route := range routes {
		if route != nil {
			models = append(models, model.Model(route))
			routeUUIDs = append(routeUUIDs, route.UUID)
		}
	}

	createRoutesOp, err := c.ovnNbClient.Create(models...)
	if err != nil {
		return fmt.Errorf("generate operations for creating static routes: %v", err)
	}

	routeAddOp, err := c.LogicalRouterUpdateStaticRouteOp(lrName, routeUUIDs, ovsdb.MutateOperationInsert)
	if err != nil {
		return fmt.Errorf("generate operations for adding static routes to logical router %s: %v", lrName, err)
	}

	ops := make([]ovsdb.Operation, 0, len(createRoutesOp)+len(routeAddOp))
	ops = append(ops, createRoutesOp...)
	ops = append(ops, routeAddOp...)

	if err = c.Transact("lr-routes-add", ops); err != nil {
		return fmt.Errorf("add static routes to %s: %v", lrName, err)
	}

	return nil
}

// AddLogicalRouterStaticRoute add a logical router static route
func (c *ovnClient) AddLogicalRouterStaticRoute(lrName, policy, cidrBlock, nextHops, routeType string) error {
	if len(policy) == 0 {
		policy = ovnnb.LogicalRouterStaticRoutePolicyDstIP
	}

	routes := make([]*ovnnb.LogicalRouterStaticRoute, 0, 2)

	for _, prefix := range strings.Split(cidrBlock, ",") {
		for _, nextHop := range strings.Split(nextHops, ",") {
			if util.CheckProtocol(prefix) != util.CheckProtocol(nextHop) {
				continue // ignore different address family
			}

			if strings.Contains(nextHop, "/") {
				nextHop = strings.Split(nextHop, "/")[0]
			}

			route, err := c.GetLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, routeType, true)
			if err != nil {
				return err
			}

			if route != nil {
				if routeType == util.EcmpRouteType {
					continue // ignore existent same nextHop ecmp route
				}

				// update existent normal route nextHop
				route.Nexthop = nextHop
				if err := c.UpdateLogicalRouterStaticRoute(route, &route.Nexthop); err != nil {
					return fmt.Errorf("update logical router static route nextHop %s: %v", nextHop, err)
				}
			} else {
				// new route
				route, err = c.newLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, routeType)
				if err != nil {
					return err
				}
				routes = append(routes, route)
			}
		}
	}

	if err := c.CreateLogicalRouterStaticRoutes(lrName, routes...); err != nil {
		return fmt.Errorf("add static routes to logical router %s: %v", lrName, err)
	}

	return nil
}

// UpdateLogicalRouterStaticRoute update logical router static route
func (c *ovnClient) UpdateLogicalRouterStaticRoute(route *ovnnb.LogicalRouterStaticRoute, fields ...interface{}) error {
	if route == nil {
		return fmt.Errorf("route is nil")
	}

	op, err := c.ovnNbClient.Where(route).Update(route, fields...)
	if err != nil {
		return fmt.Errorf("generate operations for updating logical router static route 'policy %s prefix %s': %v", *route.Policy, route.IPPrefix, err)
	}

	if err = c.Transact("net-update", op); err != nil {
		return fmt.Errorf("update logical router static route 'policy %s prefix %s': %v", *route.Policy, route.IPPrefix, err)
	}

	return nil
}

// DeleteLogicalRouterStaticRoute add a logical router static route
func (c *ovnClient) DeleteLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, routeType string) error {
	if len(policy) == 0 {
		policy = ovnnb.LogicalRouterStaticRoutePolicyDstIP
	}

	route, err := c.GetLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, routeType, true)
	if err != nil {
		return err
	}

	// not found, skip
	if route == nil {
		return nil
	}

	// remove static route from logical router
	routeRemoveOp, err := c.LogicalRouterUpdateStaticRouteOp(lrName, []string{route.UUID}, ovsdb.MutateOperationDelete)
	if err != nil {
		return fmt.Errorf("generate operations for removing static %s from logical router %s: %v", route.UUID, lrName, err)
	}

	// delete static route
	routeDelOp, err := c.Where(route).Delete()
	if err != nil {
		return fmt.Errorf("generate operations for deleting static route %s: %v", route.UUID, err)
	}

	ops := make([]ovsdb.Operation, 0, len(routeRemoveOp)+len(routeDelOp))
	ops = append(ops, routeRemoveOp...)
	ops = append(ops, routeDelOp...)

	if err = c.Transact("lr-route-del", ops); err != nil {
		return fmt.Errorf("delete static route %s", route.UUID)
	}

	return nil
}

// ClearLogicalRouterStaticRoute clear static route from logical router once
func (c *ovnClient) ClearLogicalRouterStaticRoute(lrName string) error {
	lr, err := c.GetLogicalRouter(lrName, false)
	if err != nil {
		return fmt.Errorf("get logical router %s: %v", lrName, err)
	}

	// clear static route
	lr.StaticRoutes = nil
	routeClearOp, err := c.UpdateLogicalRouterOp(lr, &lr.StaticRoutes)
	if err != nil {
		return fmt.Errorf("generate operations for clear logical router %s static route: %v", lrName, err)
	}

	// delete static route
	routeDelOp, err := c.WhereCache(func(route *ovnnb.LogicalRouterStaticRoute) bool {
		return len(route.ExternalIDs) != 0 && route.ExternalIDs[logicalRouterKey] == lrName
	}).Delete()
	if err != nil {
		return fmt.Errorf("generate operations for deleting logical router %s static routes: %v", lrName, err)
	}

	ops := make([]ovsdb.Operation, 0, len(routeClearOp)+len(routeDelOp))
	ops = append(ops, routeClearOp...)
	ops = append(ops, routeDelOp...)

	if err = c.Transact("lr-route-clear", ops); err != nil {
		return fmt.Errorf("clear logical router %s static routes: %v", lrName, err)
	}

	return nil
}

// GetLogicalRouterStaticRoute get logical router static route by some attribute,
// a static route is uniquely identified by router(lrName), policy and prefix when route is not ecmp
// a static route is uniquely identified by router(lrName), policy, prefix and nextHop when route is ecmp
func (c *ovnClient) GetLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, routeType string, ignoreNotFound bool) (*ovnnb.LogicalRouterStaticRoute, error) {
	// this is necessary because may exist same static route in different logical router
	if len(lrName) == 0 {
		return nil, fmt.Errorf("the logical router name is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	routeList := make([]ovnnb.LogicalRouterStaticRoute, 0)
	if err := c.ovnNbClient.WhereCache(func(route *ovnnb.LogicalRouterStaticRoute) bool {
		if len(route.ExternalIDs) == 0 || route.ExternalIDs[logicalRouterKey] != lrName {
			return false
		}

		// ecmp route
		if routeType == util.EcmpRouteType {
			return route.Policy != nil && *route.Policy == policy && route.IPPrefix == prefix && route.Nexthop == nextHop
		}

		// normal route
		return route.Policy != nil && *route.Policy == policy && route.IPPrefix == prefix

	}).List(ctx, &routeList); err != nil {
		return nil, fmt.Errorf("get logical router %s static route 'policy %s prefix %s nextHop %s': %v", lrName, policy, prefix, nextHop, err)
	}

	// not found
	if len(routeList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}

		return nil, fmt.Errorf("not found logical router %s static route 'policy %s prefix %s nextHop %s'", lrName, policy, prefix, nextHop)
	}

	if len(routeList) > 1 {
		return nil, fmt.Errorf("more than one static route 'policy %s prefix %s nextHop %s' in logical router %s", policy, prefix, nextHop, lrName)
	}

	return &routeList[0], nil
}

// ListLogicalRouterStaticRoutes list route which match the given externalIDs
func (c *ovnClient) ListLogicalRouterStaticRoutes(externalIDs map[string]string) ([]ovnnb.LogicalRouterStaticRoute, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	routeList := make([]ovnnb.LogicalRouterStaticRoute, 0)

	if err := c.WhereCache(func(route *ovnnb.LogicalRouterStaticRoute) bool {
		if len(route.ExternalIDs) < len(externalIDs) {
			return false
		}

		if len(route.ExternalIDs) != 0 {
			for k, v := range externalIDs {
				// if only key exist but not value in externalIDs, we should include this lsp,
				// it's equal to shell command `ovn-nbctl --columns=xx find logical_router_static_route external_ids:key!=\"\"`
				if len(v) == 0 {
					if len(route.ExternalIDs[k]) == 0 {
						return false
					}
				} else {
					if route.ExternalIDs[k] != v {
						return false
					}
				}
			}
		}

		return true
	}).List(ctx, &routeList); err != nil {
		return nil, fmt.Errorf("list logical router static routes: %v", err)
	}

	return routeList, nil
}

func (c *ovnClient) LogicalRouterStaticRouteExists(lrName, policy, prefix, nextHop, routeType string) (bool, error) {
	route, err := c.GetLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, routeType, true)
	return route != nil, err
}

// newLogicalRouterStaticRoute return logical router static route with basic information
func (c *ovnClient) newLogicalRouterStaticRoute(lrName, policy, prefix, nextHop, routeType string, options ...func(route *ovnnb.LogicalRouterStaticRoute)) (*ovnnb.LogicalRouterStaticRoute, error) {
	if len(lrName) == 0 {
		return nil, fmt.Errorf("the logical router name is required")
	}

	if len(policy) == 0 {
		policy = ovnnb.LogicalRouterStaticRoutePolicyDstIP
	}

	exists, err := c.LogicalRouterStaticRouteExists(lrName, policy, prefix, nextHop, routeType)
	if err != nil {
		return nil, fmt.Errorf("get logical router %s route: %v", lrName, err)
	}

	// found, ignore
	if exists {
		return nil, nil
	}

	route := &ovnnb.LogicalRouterStaticRoute{
		UUID:     ovsclient.NamedUUID(),
		Policy:   &policy,
		IPPrefix: prefix,
		Nexthop:  nextHop,
		ExternalIDs: map[string]string{
			logicalRouterKey: lrName,
		},
	}

	for _, option := range options {
		option(route)
	}

	return route, nil
}
