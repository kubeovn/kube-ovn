package ovs

import (
	"context"

	"github.com/ovn-org/libovsdb/client"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c OvnClient) GetLogicalRouterRouteByOpts(key, value string) ([]ovnnb.LogicalRouterStaticRoute, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	api, err := c.ovnNbClient.WherePredict(ctx, func(r *ovnnb.LogicalRouterStaticRoute) bool {
		if len(r.Options) > 0 {
			if v, ok := r.Options[key]; ok {
				return v == value
			}
		}
		return false
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

func (c OvnClient) GetLogicalRouterPoliciesByExtID(key, value string) ([]ovnnb.LogicalRouterPolicy, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	api, err := c.ovnNbClient.WherePredict(ctx, func(p *ovnnb.LogicalRouterPolicy) bool {
		if len(p.ExternalIDs) > 0 {
			if v, ok := p.ExternalIDs[key]; ok {
				return v == value
			}
		}
		return false
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
