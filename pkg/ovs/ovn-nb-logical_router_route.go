package ovs

import (
	"context"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/ovn-org/libovsdb/client"
)

func (c OvnClient) GetLogicalRouterRouteByOpts(key, value string) ([]ovnnb.LogicalRouterStaticRoute, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	api, err := c.ovnNbClient.WherePredict(ctx, func(r *ovnnb.LogicalRouterStaticRoute) bool {
		return r.Options[key] == value
	})
	if err != nil {
		return nil, err
	}

	var lrRouteList []ovnnb.LogicalRouterStaticRoute
	if err = api.List(context.TODO(), &lrRouteList); err != nil && err != client.ErrNotFound {
		return nil, err
	}

	return lrRouteList, nil
}

func (c OvnClient) GetLogicalRouterRouteByExtIds(key string) ([]ovnnb.LogicalRouterStaticRoute, error) {
	var lrRouteList []ovnnb.LogicalRouterStaticRoute
	err := c.ovnNbClient.WhereCache(
		func(lrroute *ovnnb.LogicalRouterStaticRoute) bool {
			_, ok := lrroute.ExternalIDs[key]
			return ok
		}).List(context.TODO(), &lrRouteList)
	if err != nil && err != client.ErrNotFound {
		return nil, err
	}

	return lrRouteList, nil
}

func (c OvnClient) GetLogicalRouterPoliciesByExtID(key, value string) ([]ovnnb.LogicalRouterPolicy, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	api, err := c.ovnNbClient.WherePredict(ctx, func(p *ovnnb.LogicalRouterPolicy) bool {
		return p.ExternalIDs[key] == value
	})
	if err != nil {
		return nil, err
	}

	var lrPolicyList []ovnnb.LogicalRouterPolicy
	if err = api.List(context.TODO(), &lrPolicyList); err != nil && err != client.ErrNotFound {
		return nil, err
	}

	return lrPolicyList, nil
}
