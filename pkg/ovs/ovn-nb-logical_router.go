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

// CreateLogicalRouter create logical router in ovn
func (c *ovnClient) CreateLogicalRouter(lrName string) error {
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

// UpdateLogicalRouter update logical router
func (c *ovnClient) UpdateLogicalRouter(lr *ovnnb.LogicalRouter, fields ...interface{}) error {
	op, err := c.UpdateLogicalRouterOp(lr, fields...)
	if err != nil {
		return err
	}

	if err = c.Transact("lr-update", op); err != nil {
		return fmt.Errorf("update logical router %s: %v", lr.Name, err)
	}

	return nil
}

// DeleteLogicalRouter delete logical router in ovn
func (c *ovnClient) DeleteLogicalRouter(lrName string) error {
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

// GetLogicalRouter get logical router by name,
// it is because of lack name index that does't use ovnNbClient.Get
func (c *ovnClient) GetLogicalRouter(lrName string, ignoreNotFound bool) (*ovnnb.LogicalRouter, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	lrList := make([]ovnnb.LogicalRouter, 0)
	if err := c.ovnNbClient.WhereCache(func(lr *ovnnb.LogicalRouter) bool {
		return lr.Name == lrName
	}).List(ctx, &lrList); err != nil {
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

func (c *ovnClient) LogicalRouterExists(name string) (bool, error) {
	lrp, err := c.GetLogicalRouter(name, true)
	return lrp != nil, err
}

// ListLogicalRouter list logical router
func (c *ovnClient) ListLogicalRouter(needVendorFilter bool, filter func(lr *ovnnb.LogicalRouter) bool) ([]ovnnb.LogicalRouter, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	lrList := make([]ovnnb.LogicalRouter, 0)

	if err := c.ovnNbClient.WhereCache(func(lr *ovnnb.LogicalRouter) bool {
		if needVendorFilter && (len(lr.ExternalIDs) == 0 || lr.ExternalIDs["vendor"] != util.CniTypeName) {
			return false
		}

		if filter != nil {
			return filter(lr)
		}

		return true
	}).List(ctx, &lrList); err != nil {
		return nil, fmt.Errorf("list logical router: %v", err)
	}

	return lrList, nil
}

// LogicalRouterUpdateLoadBalancers add several lb to or from logical router once
func (c *ovnClient) LogicalRouterUpdateLoadBalancers(lrName string, op ovsdb.Mutator, lbNames ...string) error {
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

	mutation := func(lr *ovnnb.LogicalRouter) *model.Mutation {
		mutation := &model.Mutation{
			Field:   &lr.LoadBalancer,
			Value:   lbUUIDs,
			Mutator: op,
		}

		return mutation
	}

	ops, err := c.LogicalRouterOp(lrName, mutation)
	if err != nil {
		return fmt.Errorf("generate operations for logical router %s update lbs %v: %v", lrName, lbNames, err)
	}

	if err := c.Transact("lr-lb-update", ops); err != nil {
		return fmt.Errorf("logical router %s update lbs %v: %v", lrName, lbNames, err)

	}

	return nil
}

// UpdateLogicalRouterOp generate operations which update logical router
func (c *ovnClient) UpdateLogicalRouterOp(lr *ovnnb.LogicalRouter, fields ...interface{}) ([]ovsdb.Operation, error) {
	if lr == nil {
		return nil, fmt.Errorf("logical_router is nil")
	}

	op, err := c.ovnNbClient.Where(lr).Update(lr, fields...)
	if err != nil {
		return nil, fmt.Errorf("generate operations for updating logical router %s: %v", lr.Name, err)
	}

	return op, nil
}

// LogicalRouterUpdatePortOp create operations add to or delete port from logical router
func (c *ovnClient) LogicalRouterUpdatePortOp(lrName, lrpUUID string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	if len(lrpUUID) == 0 {
		return nil, nil
	}

	if lrName == "" && op == ovsdb.MutateOperationDelete {
		lrList, err := c.ListLogicalRouter(false, func(lr *ovnnb.LogicalRouter) bool {
			return util.ContainsString(lr.Ports, lrpUUID)
		})
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("failed to list LR by LRP UUID %s: %v", lrpUUID, err)
		}
		if len(lrList) == 0 {
			err = fmt.Errorf("no LR found for LRP %s", lrpUUID)
			klog.Error(err)
			return nil, err
		}
		if len(lrList) != 1 {
			lrNames := make([]string, len(lrList))
			for i := range lrList {
				lrNames[i] = lrList[i].Name
			}
			err = fmt.Errorf("multiple LR found for LRP %s: %s", lrpUUID, strings.Join(lrNames, ", "))
			klog.Error(err)
			return nil, err
		}
		lrName = lrList[0].Name
	}

	mutation := func(lr *ovnnb.LogicalRouter) *model.Mutation {
		mutation := &model.Mutation{
			Field:   &lr.Ports,
			Value:   []string{lrpUUID},
			Mutator: op,
		}

		return mutation
	}

	return c.LogicalRouterOp(lrName, mutation)
}

// LogicalRouterUpdatePolicyOp create operations add to or delete policy from logical router
func (c *ovnClient) LogicalRouterUpdatePolicyOp(lrName string, policyUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	if len(policyUUIDs) == 0 {
		return nil, nil
	}

	mutation := func(lr *ovnnb.LogicalRouter) *model.Mutation {
		mutation := &model.Mutation{
			Field:   &lr.Policies,
			Value:   policyUUIDs,
			Mutator: op,
		}

		return mutation
	}

	return c.LogicalRouterOp(lrName, mutation)
}

// LogicalRouterUpdateNatOp create operations add to or delete nat rule from logical router
func (c *ovnClient) LogicalRouterUpdateNatOp(lrName string, natUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	if len(natUUIDs) == 0 {
		return nil, nil
	}

	mutation := func(lr *ovnnb.LogicalRouter) *model.Mutation {
		mutation := &model.Mutation{
			Field:   &lr.Nat,
			Value:   natUUIDs,
			Mutator: op,
		}

		return mutation
	}

	return c.LogicalRouterOp(lrName, mutation)
}

// LogicalRouterUpdateStaticRouteOp create operations add to or delete static route from logical router
func (c *ovnClient) LogicalRouterUpdateStaticRouteOp(lrName string, routeUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	if len(routeUUIDs) == 0 {
		return nil, nil
	}

	mutation := func(lr *ovnnb.LogicalRouter) *model.Mutation {
		mutation := &model.Mutation{
			Field:   &lr.StaticRoutes,
			Value:   routeUUIDs,
			Mutator: op,
		}

		return mutation
	}

	return c.LogicalRouterOp(lrName, mutation)
}

// LogicalRouterOp create operations about logical router
func (c *ovnClient) LogicalRouterOp(lrName string, mutationsFunc ...func(lr *ovnnb.LogicalRouter) *model.Mutation) ([]ovsdb.Operation, error) {
	lr, err := c.GetLogicalRouter(lrName, false)
	if err != nil {
		return nil, fmt.Errorf("get logical router %s: %v", lrName, err)
	}

	if len(mutationsFunc) == 0 {
		return nil, nil
	}

	mutations := make([]model.Mutation, 0, len(mutationsFunc))

	for _, f := range mutationsFunc {
		mutation := f(lr)

		if mutation != nil {
			mutations = append(mutations, *mutation)
		}
	}

	ops, err := c.ovnNbClient.Where(lr).Mutate(lr, mutations...)
	if err != nil {
		return nil, fmt.Errorf("generate operations for mutating logical router %s: %v", lrName, err)
	}

	return ops, nil
}
