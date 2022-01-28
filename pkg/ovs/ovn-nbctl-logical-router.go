package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c Client) ListLogicalRouters(needVendorFilter bool) ([]ovnnb.LogicalRouter, error) {
	var err error
	lrList := make([]ovnnb.LogicalRouter, 0)
	if needVendorFilter {
		err = c.nbClient.WhereCache(func(lr *ovnnb.LogicalRouter) bool {
			return len(lr.ExternalIDs) != 0 && lr.ExternalIDs["vendor"] == util.CniTypeName
		}).List(context.TODO(), &lrList)
	} else {
		err = c.nbClient.List(context.TODO(), &lrList)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list logical router: %v", err)
	}

	return lrList, nil
}

func (c Client) GetLogicalRouter(name string, ignoreNotFound bool) (*ovnnb.LogicalRouter, error) {
	lrList := make([]ovnnb.LogicalRouter, 0, 1)
	if err := c.nbClient.WhereCache(func(lr *ovnnb.LogicalRouter) bool { return lr.Name == name }).List(context.TODO(), &lrList); err != nil {
		return nil, fmt.Errorf("failed to get logical router %s: %v", name, err)
	}
	if len(lrList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("logical router %s does not exist", name)
	}
	if len(lrList) != 1 {
		return nil, fmt.Errorf("found multiple logical routers with the same name %s", name)
	}

	return &lrList[0], nil
}

func (c Client) CreateLogicalRouter(name string) error {
	lr, err := c.GetLogicalRouter(name, true)
	if err != nil {
		return err
	}

	var ops []ovsdb.Operation
	if lr != nil {
		if len(lr.ExternalIDs) != 0 && lr.ExternalIDs["vendor"] == util.CniTypeName {
			return nil
		}
		if lr.ExternalIDs == nil {
			lr.ExternalIDs = make(map[string]string)
		}
		lr.ExternalIDs["vendor"] = util.CniTypeName
		if ops, err = c.nbClient.Where(lr).Update(lr, &lr.ExternalIDs); err != nil {
			return fmt.Errorf("failed to generate update operations for logical router %s: %v", name, err)
		}
	} else {
		lr = &ovnnb.LogicalRouter{
			Name:        name,
			ExternalIDs: map[string]string{"vendor": util.CniTypeName},
		}
		if ops, err = c.nbClient.Create(lr); err != nil {
			return fmt.Errorf("failed to generate create operations for logical router %s: %v", name, err)
		}
	}

	if err = Transact(c.nbClient, "lr-add", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to create logical router %s: %v", name, err)
	}

	return nil
}

func (c Client) DeleteLogicalRouter(name string) error {
	lr, err := c.GetLogicalRouter(name, true)
	if err != nil {
		return err
	}
	if lr == nil {
		return nil
	}

	ops, err := c.nbClient.Where(lr).Delete()
	if err != nil {
		return fmt.Errorf("failed to generate delete operations for logical router %s: %v", name, err)
	}
	if err = Transact(c.nbClient, "lr-del", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to delete logical router %s: %v", name, err)
	}

	return nil
}

func (c Client) createRouterPort(ls, lr, mac, ip string) error {
	lrp := fmt.Sprintf("%s-%s", lr, ls)
	if err := c.createLRP(lrp, lr, mac, ip, nil); err != nil {
		return err
	}

	lsp := fmt.Sprintf("%s-%s", ls, lr)
	return c.createLSP(lsp, "router", ls, "router", "", "", nil, map[string]string{"router-port": lrp}, nil, false, false, "")
}

func (c Client) RemoveRouterPort(ls, lr string) error {
	if err := c.DeleteLogicalSwitchPort(fmt.Sprintf("%s-%s", ls, lr)); err != nil {
		return err
	}
	return c.DeleteLogicalRouterPort(fmt.Sprintf("%s-%s", lr, ls))
}
