package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/client"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c OvnClient) GetLogicalRouter(name string, ignoreNotFound bool) (*ovnnb.LogicalRouter, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	api, err := c.ovnNbClient.WherePredict(ctx, func(model *ovnnb.LogicalRouter) bool {
		return model.Name == name
	})
	if err != nil {
		return nil, err
	}

	var result []*ovnnb.LogicalRouter
	if err = api.List(context.TODO(), &result); err != nil || len(result) == 0 {
		if ignoreNotFound && (err == client.ErrNotFound || len(result) == 0) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get logical router %s: %v", name, err)
	}

	return result[0], nil
}

func (c OvnClient) LogicalRouterExists(name string) (bool, error) {
	lr, err := c.GetLogicalRouter(name, true)
	return lr != nil, err
}
