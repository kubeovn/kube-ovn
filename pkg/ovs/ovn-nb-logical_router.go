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
func (c OvnClient) CreateLogicalRouter(lrName string) error {
	exist, err := c.LogicalRouterExists(lrName)
	if err != nil {
		return err
	}

	// found, ignore
	if exist {
		return nil
	}

	lr := &ovnnb.LogicalRouter{
		Name:        lrName,
		ExternalIDs: map[string]string{"vendor": util.CniTypeName},
	}

	op, err := c.ovnNbClient.Create(lr)
	if err != nil {
		return fmt.Errorf("generate operations for creating logical router %s: %v", lrName, err)
	}

	if err := c.Transact("lr-add", op); err != nil {
		return fmt.Errorf("create logical router %s: %v", lrName, err)
	}

	return nil
}

// DeleteLogicalRouter delete logical router in ovn
func (c OvnClient) DeleteLogicalRouter(lrName string) error {
	lr, err := c.GetLogicalRouter(lrName, true)
	if err != nil {
		return fmt.Errorf("get logical router %s when delete: %v", lrName, err)
	}

	// not found, skip
	if lr == nil {
		return nil
	}

	op, err := c.Where(lr).Delete()
	if err != nil {
		return err
	}

	if err := c.Transact("lr-del", op); err != nil {
		return fmt.Errorf("delete logical router %s: %v", lrName, err)
	}

	return nil
}

func (c OvnClient) GetLogicalRouter(lrName string, ignoreNotFound bool) (*ovnnb.LogicalRouter, error) {
	lrList := make([]ovnnb.LogicalRouter, 0)
	if err := c.ovnNbClient.WhereCache(func(lr *ovnnb.LogicalRouter) bool {
		return lr.Name == lrName
	}).List(context.TODO(), &lrList); err != nil {
		return nil, fmt.Errorf("list logical router %q: %v", lrName, err)
	}

	// not found
	if len(lrList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("not found logical router %q", lrName)
	}

	if len(lrList) > 1 {
		return nil, fmt.Errorf("more than one logical router with same name %q", lrName)
	}

	return &lrList[0], nil
}

func (c OvnClient) LogicalRouterExists(name string) (bool, error) {
	lrp, err := c.GetLogicalRouter(name, true)
	return lrp != nil, err
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

// LogicalRouterUpdatePortOp create operations add to or delete from logical router
func (c OvnClient) LogicalRouterUpdatePortOp(lrName, lrpUUID string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	lr, err := c.GetLogicalRouter(lrName, false)
	if err != nil {
		return nil, fmt.Errorf("get logical router %s when add or del port: %v", lrName, err)
	}

	if len(lrpUUID) == 0 {
		return nil, fmt.Errorf("the uuid of port add or del to logical router %s cannot be empty", lrName)
	}

	mutation := model.Mutation{
		Field:   &lr.Ports,
		Value:   []string{lrpUUID},
		Mutator: op,
	}

	ops, err := c.ovnNbClient.Where(lr).Mutate(lr, mutation)
	if err != nil {
		return nil, fmt.Errorf("generate operations for mutating logical router %s: %v", lrName, err)
	}

	return ops, nil
}
