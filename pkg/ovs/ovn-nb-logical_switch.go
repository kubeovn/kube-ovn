package ovs

import (
	"context"
	"fmt"
	"strings"

	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// CreateLogicalSwitch create logical switch
func (c OvnClient) CreateLogicalSwitch(lsName, lrName, cidrBlock, gateway string, needRouter bool) error {
	lspName := fmt.Sprintf("%s-%s", lsName, lrName)
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)

	networks := util.GetIpAddrWithMask(gateway, cidrBlock)

	exist, err := c.LogicalSwitchExists(lsName)
	if err != nil {
		return err
	}

	// only update logical router port networks when logical switch exist
	if exist {
		lrp := &ovnnb.LogicalRouterPort{
			Name:     lrpName,
			Networks: strings.Split(networks, ","),
		}
		if err := c.UpdateLogicalRouterPort(lrp, &lrp.Networks); err != nil {
			return fmt.Errorf("update logical router port %s", lrpName)
		}
	} else {
		if err := c.CreateBareLogicalSwitch(lsName); err != nil {
			return fmt.Errorf("create logical switch %s", lsName)
		}
	}

	if needRouter {
		if err := c.CreateRouterPort(lsName, lrName, networks); err != nil {
			return fmt.Errorf("create router type port %s and %s: %v", lspName, lrpName, err)
		}
	} else {
		if err := c.RemoveRouterTypePort(lspName, lrpName); err != nil {
			return fmt.Errorf("remove router type port %s and %s: %v", lspName, lrpName, err)
		}
	}

	return nil
}

// CreateBareLogicalSwitch create logical switch with basic configuration
func (c OvnClient) CreateBareLogicalSwitch(lsName string) error {
	ls := &ovnnb.LogicalSwitch{
		Name:        lsName,
		ExternalIDs: map[string]string{"vendor": util.CniTypeName},
	}

	op, err := c.ovnNbClient.Create(ls)
	if err != nil {
		return fmt.Errorf("generate create operations for logical switch %s: %v", lsName, err)
	}

	if err := c.Transact("ls-add", op); err != nil {
		return fmt.Errorf("create logical switch %s: %v", lsName, err)
	}

	return nil
}

func (c OvnClient) LogicalSwitchAddPort(lsName, lspName string) error {
	lsp, err := c.GetLogicalSwitchPort(lspName, false)
	if err != nil {
		return err
	}

	ops, err := c.LogicalSwitchUpdatePortOp(lsName, lsp.UUID, true)
	if err != nil {
		return fmt.Errorf("generate operations for logical switch %s add port %s: %v", lsName, lspName, err)
	}

	if err := c.Transact("lsp-add", ops); err != nil {
		return fmt.Errorf("add port %s to logical switch %s: %v", lspName, lsName, err)
	}

	return nil
}

func (c OvnClient) LogicalSwitchDelPort(lsName, lspName string) error {
	lsp, err := c.GetLogicalSwitchPort(lspName, false)
	if err != nil {
		return err
	}

	ops, err := c.LogicalSwitchUpdatePortOp(lsName, lsp.UUID, false)
	if err != nil {
		return fmt.Errorf("generate operations for logical switch %s del port %s: %v", lsName, lspName, err)
	}

	if err := c.Transact("lsp-del", ops); err != nil {
		return fmt.Errorf("del port %s from logical switch %s: %v", lspName, lsName, err)
	}

	return nil
}

// DeleteLogicalSwitch delete logical switch
func (c OvnClient) DeleteLogicalSwitch(lsName string) error {
	ls, err := c.GetLogicalSwitch(lsName, true)
	if err != nil {
		return err
	}

	// not found, skip
	if ls == nil {
		return nil
	}

	op, err := c.Where(ls).Delete()
	if err != nil {
		return err
	}

	if err := c.Transact("ls-del", op); err != nil {
		return fmt.Errorf("delete logical switch %s: %v", lsName, err)
	}

	return nil
}

func (c OvnClient) GetLogicalSwitch(name string, ignoreNotFound bool) (*ovnnb.LogicalSwitch, error) {
	lsList := make([]ovnnb.LogicalSwitch, 0)
	if err := c.ovnNbClient.WhereCache(func(ls *ovnnb.LogicalSwitch) bool {
		return ls.Name == name
	}).List(context.TODO(), &lsList); err != nil {
		return nil, fmt.Errorf("list switch switch %q: %v", name, err)
	}

	// not found
	if len(lsList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("not found logical switch %q", name)
	}

	if len(lsList) > 1 {
		return nil, fmt.Errorf("more than one logical switch with same name %q", name)
	}

	return &lsList[0], nil
}

func (c OvnClient) LogicalSwitchExists(name string) (bool, error) {
	lrp, err := c.GetLogicalSwitch(name, true)
	return lrp != nil, err
}

// ListLogicalSwitch list logical switch
func (c OvnClient) ListLogicalSwitch(needVendorFilter bool) ([]ovnnb.LogicalSwitch, error) {
	lsList := make([]ovnnb.LogicalSwitch, 0)

	if err := c.ovnNbClient.WhereCache(func(ls *ovnnb.LogicalSwitch) bool {
		if needVendorFilter && (len(ls.ExternalIDs) == 0 || ls.ExternalIDs["vendor"] != util.CniTypeName) {
			return false
		}
		return true
	}).List(context.TODO(), &lsList); err != nil {
		return nil, fmt.Errorf("list logical switch: %v", err)
	}

	return lsList, nil
}

func (c OvnClient) LogicalSwitchUpdatePortOp(lsName string, lspUUID string, opIsAdd bool) ([]ovsdb.Operation, error) {
	if len(lspUUID) == 0 {
		return nil, fmt.Errorf("uuid %s add or del to logical switch %s cannot be empty", lspUUID, lsName)
	}

	mutation := func(ls *ovnnb.LogicalSwitch) model.Mutation {
		mutation := model.Mutation{
			Field: &ls.Ports,
			Value: []string{lspUUID},
		}

		if opIsAdd {
			mutation.Mutator = ovsdb.MutateOperationInsert
		} else {
			mutation.Mutator = ovsdb.MutateOperationDelete
		}

		return mutation
	}

	return c.LogicalSwitchOp(lsName, mutation)
}

func (c OvnClient) LogicalSwitchUpdateLoadBalancerOp(lb, ls string) error {
	return nil
}

func (c OvnClient) LogicalSwitchOp(lsName string, mutationsFunc ...func(ls *ovnnb.LogicalSwitch) model.Mutation) ([]ovsdb.Operation, error) {
	ls, err := c.GetLogicalSwitch(lsName, false)
	if err != nil {
		return nil, err
	}

	if len(mutationsFunc) == 0 {
		return nil, nil
	}

	mutations := make([]model.Mutation, 0, len(mutationsFunc))

	for _, f := range mutationsFunc {
		mutations = append(mutations, f(ls))
	}

	ops, err := c.ovnNbClient.Where(ls).Mutate(ls, mutations...)
	if err != nil {
		return nil, fmt.Errorf("generate mutate operations for logical switch %s: %v", ls.Name, err)
	}

	return ops, nil
}
