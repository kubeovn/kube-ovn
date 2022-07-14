package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog/v2"

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
	if nil != err {
		return err
	}

	// not found, skip
	if nil == lr {
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
	if 0 == len(lrList) {
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
		klog.Errorf("list logical router: %v", err)
		return nil, err
	}

	return lrList, nil
}

func (c OvnClient) LogicalRouterOp(lrName, lrpName string, opIsAdd bool) ([]ovsdb.Operation, error) {
	lr, err := c.GetLogicalRouter(lrName, false)
	if err != nil {
		return nil, err
	}

	lrp, err := c.GetLogicalRouterPort(lrpName, false)
	if err != nil {
		return nil, err
	}

	portMap := make(map[string]struct{}, len(lr.Ports))

	for _, port := range lr.Ports {
		portMap[port] = struct{}{}
	}

	// do nothing if port exist when operation is add
	if _, ok := portMap[lrp.UUID]; ok && opIsAdd {
		return nil, nil
	}

	if opIsAdd {
		lr.Ports = append(lr.Ports, lrp.UUID)
	} else {
		delete(portMap, lrp.UUID)
		ports := make([]string, 0, len(portMap))

		for port := range portMap {
			ports = append(ports, port)
		}

		lr.Ports = ports
	}

	ops, err := c.ovnNbClient.Where(lr).Update(lr, &lr.Ports)
	if err != nil {
		return nil, fmt.Errorf("generate update operations for logical router %s: %v", lrName, err)
	}

	return ops, nil
}

func (c OvnClient) LogicalRouterAddPort(lrName, portName string) error {
	ops, err := c.LogicalRouterOp(lrName, portName, true)
	if nil != err {
		return err
	}

	if err = c.Transact("lr-add-port", ops); err != nil {
		return fmt.Errorf("update ports of logical router %s: %v", lrName, err)
	}
	return nil
}
