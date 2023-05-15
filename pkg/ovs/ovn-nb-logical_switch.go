package ovs

import (
	"context"
	"fmt"
	"strings"

	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// CreateLogicalSwitch create logical switch
func (c *ovnClient) CreateLogicalSwitch(lsName, lrName, cidrBlock, gateway string, needRouter, randomAllocateGW bool) error {
	lspName := fmt.Sprintf("%s-%s", lsName, lrName)
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)

	networks := util.GetIpAddrWithMask(gateway, cidrBlock)

	exist, err := c.LogicalSwitchExists(lsName)
	if err != nil {
		return err
	}

	// only update logical router port networks when logical switch exist
	if exist {
		if randomAllocateGW {
			return nil
		}

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
		if err := c.CreateLogicalPatchPort(lsName, lrName, lspName, lrpName, networks, util.GenerateMac()); err != nil {
			return err
		}
	} else {
		if randomAllocateGW {
			return nil
		}

		if err := c.RemoveLogicalPatchPort(lspName, lrpName); err != nil {
			return fmt.Errorf("remove router type port %s and %s: %v", lspName, lrpName, err)
		}
	}

	return nil
}

// CreateBareLogicalSwitch create logical switch with basic configuration
func (c *ovnClient) CreateBareLogicalSwitch(lsName string) error {
	exist, err := c.LogicalSwitchExists(lsName)
	if err != nil {
		return err
	}

	// ingnore
	if exist {
		return nil
	}

	ls := &ovnnb.LogicalSwitch{
		Name:        lsName,
		ExternalIDs: map[string]string{"vendor": util.CniTypeName},
	}

	op, err := c.ovnNbClient.Create(ls)
	if err != nil {
		return fmt.Errorf("generate operations for creating logical switch %s: %v", lsName, err)
	}

	if err := c.Transact("ls-add", op); err != nil {
		return fmt.Errorf("create logical switch %s: %v", lsName, err)
	}

	return nil
}

// LogicalSwitchAddPort add port to logical switch
func (c *ovnClient) LogicalSwitchAddPort(lsName, lspName string) error {
	lsp, err := c.GetLogicalSwitchPort(lspName, false)
	if err != nil {
		return fmt.Errorf("get logical switch port %s when logical switch add port: %v", lspName, err)
	}

	ops, err := c.LogicalSwitchUpdatePortOp(lsName, lsp.UUID, ovsdb.MutateOperationInsert)
	if err != nil {
		return fmt.Errorf("generate operations for logical switch %s add port %s: %v", lsName, lspName, err)
	}

	if err := c.Transact("lsp-add", ops); err != nil {
		return fmt.Errorf("add port %s to logical switch %s: %v", lspName, lsName, err)
	}

	return nil
}

// LogicalSwitchDelPort del port from logical switch
func (c *ovnClient) LogicalSwitchDelPort(lsName, lspName string) error {
	lsp, err := c.GetLogicalSwitchPort(lspName, false)
	if err != nil {
		return fmt.Errorf("get logical switch port %s when logical switch del port: %v", lspName, err)
	}

	ops, err := c.LogicalSwitchUpdatePortOp(lsName, lsp.UUID, ovsdb.MutateOperationDelete)
	if err != nil {
		return fmt.Errorf("generate operations for logical switch %s del port %s: %v", lsName, lspName, err)
	}

	if err := c.Transact("lsp-del", ops); err != nil {
		return fmt.Errorf("del port %s from logical switch %s: %v", lspName, lsName, err)
	}

	return nil
}

// LogicalSwitchUpdateLoadBalancers add several lb to or from logical switch once
func (c *ovnClient) LogicalSwitchUpdateLoadBalancers(lsName string, op ovsdb.Mutator, lbNames ...string) error {
	if len(lbNames) == 0 {
		return nil
	}

	lbUUIDs := make([]string, 0, len(lbNames))

	for _, lbName := range lbNames {
		lb, err := c.GetLoadBalancer(lbName, true)
		if err != nil {
			return err
		}

		// ignore non-existent object
		if lb != nil {
			lbUUIDs = append(lbUUIDs, lb.UUID)
		}
	}

	ops, err := c.LogicalSwitchUpdateLoadBalancerOp(lsName, lbUUIDs, op)
	if err != nil {
		return fmt.Errorf("generate operations for logical switch %s update lbs %v: %v", lsName, lbNames, err)
	}

	if err := c.Transact("ls-lb-update", ops); err != nil {
		return fmt.Errorf("logical switch %s update lbs %v: %v", lsName, lbNames, err)

	}

	return nil
}

// DeleteLogicalSwitch delete logical switch
func (c *ovnClient) DeleteLogicalSwitch(lsName string) error {
	op, err := c.DeleteLogicalSwitchOp(lsName)
	if err != nil {
		return err
	}

	if err := c.Transact("ls-del", op); err != nil {
		return fmt.Errorf("delete logical switch %s: %v", lsName, err)
	}

	return nil
}

// GetLogicalSwitch get logical switch by name,
// it is because of lack name index that does't use ovnNbClient.Get
func (c *ovnClient) GetLogicalSwitch(lsName string, ignoreNotFound bool) (*ovnnb.LogicalSwitch, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	lsList := make([]ovnnb.LogicalSwitch, 0)
	if err := c.ovnNbClient.WhereCache(func(ls *ovnnb.LogicalSwitch) bool {
		return ls.Name == lsName
	}).List(ctx, &lsList); err != nil {
		return nil, fmt.Errorf("list switch switch %q: %v", lsName, err)
	}

	// not found
	if len(lsList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("not found logical switch %q", lsName)
	}

	if len(lsList) > 1 {
		return nil, fmt.Errorf("more than one logical switch with same name %q", lsName)
	}

	return &lsList[0], nil
}

func (c *ovnClient) LogicalSwitchExists(lsName string) (bool, error) {
	ls, err := c.GetLogicalSwitch(lsName, true)
	return ls != nil, err
}

// ListLogicalSwitch list logical switch
func (c *ovnClient) ListLogicalSwitch(needVendorFilter bool, filter func(ls *ovnnb.LogicalSwitch) bool) ([]ovnnb.LogicalSwitch, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	lsList := make([]ovnnb.LogicalSwitch, 0)

	if err := c.ovnNbClient.WhereCache(func(ls *ovnnb.LogicalSwitch) bool {
		if needVendorFilter && (len(ls.ExternalIDs) == 0 || ls.ExternalIDs["vendor"] != util.CniTypeName) {
			return false
		}

		if filter != nil {
			return filter(ls)
		}

		return true
	}).List(ctx, &lsList); err != nil {
		return nil, fmt.Errorf("list logical switch: %v", err)
	}

	return lsList, nil
}

// LogicalSwitchUpdatePortOp create operations add port to or delete port from logical switch
func (c *ovnClient) LogicalSwitchUpdatePortOp(lsName string, lspUUID string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	if len(lspUUID) == 0 {
		return nil, nil
	}

	if lsName == "" && op == ovsdb.MutateOperationDelete {
		lsList, err := c.ListLogicalSwitch(false, func(ls *ovnnb.LogicalSwitch) bool {
			return util.ContainsString(ls.Ports, lspUUID)
		})
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("failed to list LS by LSP UUID %s: %v", lspUUID, err)
		}
		if len(lsList) == 0 {
			err = fmt.Errorf("no LS found for LSP %s", lspUUID)
			klog.Error(err)
			return nil, err
		}
		if len(lsList) != 1 {
			lsNames := make([]string, len(lsList))
			for i := range lsList {
				lsNames[i] = lsList[i].Name
			}
			err = fmt.Errorf("multiple LS found for LSP %s: %s", lspUUID, strings.Join(lsNames, ", "))
			klog.Error(err)
			return nil, err
		}
		lsName = lsList[0].Name
	}

	mutation := func(ls *ovnnb.LogicalSwitch) *model.Mutation {
		mutation := &model.Mutation{
			Field:   &ls.Ports,
			Value:   []string{lspUUID},
			Mutator: op,
		}

		return mutation
	}

	return c.LogicalSwitchOp(lsName, mutation)
}

// LogicalSwitchUpdateLoadBalancerOp create operations add lb to or delete lb from logical switch
func (c *ovnClient) LogicalSwitchUpdateLoadBalancerOp(lsName string, lbUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	if len(lbUUIDs) == 0 {
		return nil, nil
	}

	mutation := func(ls *ovnnb.LogicalSwitch) *model.Mutation {
		mutation := &model.Mutation{
			Field:   &ls.LoadBalancer,
			Value:   lbUUIDs,
			Mutator: op,
		}

		return mutation
	}

	return c.LogicalSwitchOp(lsName, mutation)
}

// logicalSwitchUpdateAclOp create operations add acl to or delete acl from logical switch
func (c *ovnClient) logicalSwitchUpdateAclOp(lsName string, aclUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	if len(aclUUIDs) == 0 {
		return nil, nil
	}

	mutation := func(ls *ovnnb.LogicalSwitch) *model.Mutation {
		mutation := &model.Mutation{
			Field:   &ls.ACLs,
			Value:   aclUUIDs,
			Mutator: op,
		}

		return mutation
	}

	return c.LogicalSwitchOp(lsName, mutation)
}

// LogicalSwitchOp create operations about logical switch
func (c *ovnClient) LogicalSwitchOp(lsName string, mutationsFunc ...func(ls *ovnnb.LogicalSwitch) *model.Mutation) ([]ovsdb.Operation, error) {
	ls, err := c.GetLogicalSwitch(lsName, false)
	if err != nil {
		return nil, fmt.Errorf("get logical switch %s when generate mutate operations: %v", lsName, err)
	}

	if len(mutationsFunc) == 0 {
		return nil, nil
	}

	mutations := make([]model.Mutation, 0, len(mutationsFunc))

	for _, f := range mutationsFunc {
		mutation := f(ls)

		if mutation != nil {
			mutations = append(mutations, *mutation)
		}
	}

	ops, err := c.ovnNbClient.Where(ls).Mutate(ls, mutations...)
	if err != nil {
		return nil, fmt.Errorf("generate operations for mutating logical switch %s: %v", lsName, err)
	}

	return ops, nil
}

// DeleteLogicalSwitchOp create operations that delete logical switch
func (c *ovnClient) DeleteLogicalSwitchOp(lsName string) ([]ovsdb.Operation, error) {
	ls, err := c.GetLogicalSwitch(lsName, true)
	if err != nil {
		return nil, fmt.Errorf("get logical switch %s: %v", lsName, err)
	}

	// not found, skip
	if ls == nil {
		return nil, nil
	}

	op, err := c.Where(ls).Delete()
	if err != nil {
		return nil, fmt.Errorf("generate operations for deleting logical switch %s: %v", lsName, err)
	}

	return op, nil
}
