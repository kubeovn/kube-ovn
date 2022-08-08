package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// CreateLogicalRouter delete logical router in ovn
func (c OvnClient) CreateLogicalRouter(name string) error {
	lr := &ovnnb.LogicalRouter{
		Name:        name,
		ExternalIDs: map[string]string{"vendor": util.CniTypeName},
	}

	op, err := c.ovnNbClient.Create(lr)
	if err != nil {
		return fmt.Errorf("generate create operations for logical router %s: %v", name, err)
	}

	return c.Transact("lr-add", op)
}

// DeleteLogicalRouter delete logical router in ovn
func (c OvnClient) DeleteLogicalRouter(name string) error {
	lr, err := c.GetLogicalRouter(name, true)
	if err != nil {
		return err
	}

	// not found, skip
	if lr == nil {
		return nil
	}

	op, err := c.Where(lr).Delete()
	if err != nil {
		return err
	}

	return c.Transact("lr-del", op)
}

func (c OvnClient) GetLogicalRouter(name string, ignoreNotFound bool) (*ovnnb.LogicalRouter, error) {
	lrList := make([]ovnnb.LogicalRouter, 0)
	if err := c.ovnNbClient.WhereCache(func(lr *ovnnb.LogicalRouter) bool {
		return lr.Name == name
	}).List(context.TODO(), &lrList); err != nil {
		return nil, fmt.Errorf("list logical router %q: %v", name, err)
	}

	// not found
	if len(lrList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("not found logical router %q", name)
	}

	if len(lrList) > 1 {
		return nil, fmt.Errorf("more than one logical router with same name %q", name)
	}

	return &lrList[0], nil
}

// ListLogicalRouter list logical router
func (c OvnClient) ListLogicalRouter(needVendorFilter bool) ([]ovnnb.LogicalRouter, error) {
	lrList := make([]ovnnb.LogicalRouter, 0)

	if err := c.ovnNbClient.WhereCache(func(lr *ovnnb.LogicalRouter) bool {
		if needVendorFilter && (len(lr.ExternalIDs) == 0 || lr.ExternalIDs["vendor"] != util.CniTypeName) {
			return false
		}
		return true
	}).List(context.TODO(), &lrList); err != nil {
		return nil, fmt.Errorf("list logical router: %v", err)
	}

	return lrList, nil
}

// LogicalRouterOp create operations add port to logical router
func (c OvnClient) LogicalRouterOp(lrName, lrpUUID string, opIsAdd bool) ([]ovsdb.Operation, error) {
	lr, err := c.GetLogicalRouter(lrName, false)
	if err != nil {
		return nil, err
	}

	if len(lrpUUID) == 0 {
		return nil, fmt.Errorf("the uuid of port added to logical router %s cannot be empty", lrName)
	}

	mutation := model.Mutation{
		Field: &lr.Ports,
		Value: []string{lrpUUID},
	}

	if opIsAdd {
		mutation.Mutator = ovsdb.MutateOperationInsert
	} else {
		mutation.Mutator = ovsdb.MutateOperationDelete
	}

	ops, err := c.ovnNbClient.Where(lr).Mutate(lr, mutation)
	if err != nil {
		return nil, fmt.Errorf("generate mutate operations for logical router %s: %v", lrName, err)
	}

	return ops, nil
}
