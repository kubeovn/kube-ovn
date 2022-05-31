package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/client"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c OvnClient) GetLogicalRouter(name string, ignoreNotFound bool) (*ovnnb.LogicalRouter, error) {
	predicate := func(model *ovnnb.LogicalRouter) bool {
		return model.Name == name
	}
	// Logical_Router has no indexes defined in the schema
	var result []*ovnnb.LogicalRouter
	if err := c.ovnNbClient.WhereCache(predicate).List(context.TODO(), &result); err != nil || len(result) == 0 {
		if ignoreNotFound && (err == client.ErrNotFound || len(result) == 0) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get logical router %s: %v", name, err)
	}

	return result[0], nil
}

func (c OvnClient) LogicalRouterExists(name string) (bool, error) {
	lsp, err := c.GetLogicalRouter(name, true)
	return lsp != nil, err
}
