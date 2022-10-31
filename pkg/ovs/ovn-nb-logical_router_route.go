package ovs

import (
	"context"

	"github.com/ovn-org/libovsdb/client"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c OvnClient) GetLogicalRouterRouteByOpts(key, value string) ([]ovnnb.LogicalRouterStaticRoute, error) {
	var lrRouteList []ovnnb.LogicalRouterStaticRoute
	err := c.ovnNbClient.WhereCache(
		func(r *ovnnb.LogicalRouterStaticRoute) bool {
			return r.Options[key] == value
		}).List(context.TODO(), &lrRouteList)
	if err != nil && err != client.ErrNotFound {
		return nil, err
	}

	return lrRouteList, nil
}

func (c OvnClient) GetLogicalRouterPoliciesByExtID(key, value string) ([]ovnnb.LogicalRouterPolicy, error) {
	var lrPolicyList []ovnnb.LogicalRouterPolicy
	err := c.ovnNbClient.WhereCache(
		func(p *ovnnb.LogicalRouterPolicy) bool {
			return p.ExternalIDs[key] == value
		}).List(context.TODO(), &lrPolicyList)
	if err != nil && err != client.ErrNotFound {
		return nil, err
	}

	return lrPolicyList, nil
}
