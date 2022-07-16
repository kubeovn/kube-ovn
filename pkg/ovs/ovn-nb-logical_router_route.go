package ovs

import (
	"context"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/ovn-org/libovsdb/client"
)

func (c OvnClient) GetLogicalRouterRouteByOpts(key, value string) ([]ovnnb.LogicalRouterStaticRoute, error) {

	var lrRouteList []ovnnb.LogicalRouterStaticRoute
	err := c.ovnNbClient.WhereCache(
		func(lrroute *ovnnb.LogicalRouterStaticRoute) bool {
			return lrroute.Options[key] == value
		}).List(context.TODO(), &lrRouteList)
	if err != nil && err != client.ErrNotFound {
		return nil, err
	}

	return lrRouteList, nil
}

func (c OvnClient) GetLogicalRouterPoliciesByExtID(key, value string) ([]ovnnb.LogicalRouterPolicy, error) {

	var lrPolicyList []ovnnb.LogicalRouterPolicy
	err := c.ovnNbClient.WhereCache(
		func(lrPoliy *ovnnb.LogicalRouterPolicy) bool {
			return lrPoliy.ExternalIDs[key] == value
		}).List(context.TODO(), &lrPolicyList)
	if err != nil && err != client.ErrNotFound {
		return nil, err
	}

	return lrPolicyList, nil
}
