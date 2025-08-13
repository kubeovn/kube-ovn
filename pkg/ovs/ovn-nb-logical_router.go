package ovs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// CreateLogicalRouter create logical router in ovn
func (c *OVNNbClient) CreateLogicalRouter(lrName string) error {
	exist, err := c.LogicalRouterExists(lrName)
	if err != nil {
		klog.Error(err)
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

	op, err := c.Create(lr)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for creating logical router %s: %w", lrName, err)
	}

	if err := c.Transact("lr-add", op); err != nil {
		klog.Error(err)
		return fmt.Errorf("create logical router %s: %w", lrName, err)
	}

	return nil
}

// UpdateLogicalRouter update logical router
func (c *OVNNbClient) UpdateLogicalRouter(lr *ovnnb.LogicalRouter, fields ...any) error {
	op, err := c.UpdateLogicalRouterOp(lr, fields...)
	if err != nil {
		klog.Error(err)
		return err
	}

	if err = c.Transact("lr-update", op); err != nil {
		klog.Error(err)
		return fmt.Errorf("update logical router %s: %w", lr.Name, err)
	}

	return nil
}

// DeleteLogicalRouter delete logical router in ovn
func (c *OVNNbClient) DeleteLogicalRouter(lrName string) error {
	lr, err := c.GetLogicalRouter(lrName, true)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("get logical router %s when delete: %w", lrName, err)
	}

	// not found, skip
	if lr == nil {
		return nil
	}

	op, err := c.Where(lr).Delete()
	if err != nil {
		klog.Error(err)
		return err
	}

	if err := c.Transact("lr-del", op); err != nil {
		klog.Error(err)
		return fmt.Errorf("delete logical router %s: %w", lrName, err)
	}

	return nil
}

// GetLogicalRouter get logical router by name,
// it is because of lack name index that does't use OVNNbClient.Get
func (c *OVNNbClient) GetLogicalRouter(lrName string, ignoreNotFound bool) (*ovnnb.LogicalRouter, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	lrList := make([]ovnnb.LogicalRouter, 0)
	if err := c.ovsDbClient.WhereCache(func(lr *ovnnb.LogicalRouter) bool {
		return lr.Name == lrName
	}).List(ctx, &lrList); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("list logical router %q: %w", lrName, err)
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

	// #nosec G602
	return &lrList[0], nil
}

func (c *OVNNbClient) LogicalRouterExists(name string) (bool, error) {
	lrp, err := c.GetLogicalRouter(name, true)
	return lrp != nil, err
}

// ListLogicalRouter list logical router
func (c *OVNNbClient) ListLogicalRouter(needVendorFilter bool, filter func(lr *ovnnb.LogicalRouter) bool) ([]ovnnb.LogicalRouter, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var lrList []ovnnb.LogicalRouter
	if err := c.ovsDbClient.WhereCache(func(lr *ovnnb.LogicalRouter) bool {
		if needVendorFilter && (len(lr.ExternalIDs) == 0 || lr.ExternalIDs["vendor"] != util.CniTypeName) {
			return false
		}

		if filter != nil {
			return filter(lr)
		}

		return true
	}).List(ctx, &lrList); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("list logical router: %w", err)
	}

	return lrList, nil
}

// ListLogicalRouterNames list logical router names
func (c *OVNNbClient) ListLogicalRouterNames(needVendorFilter bool, filter func(lr *ovnnb.LogicalRouter) bool) ([]string, error) {
	lrList, err := c.ListLogicalRouter(needVendorFilter, filter)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	names := make([]string, 0, len(lrList))
	for _, lr := range lrList {
		names = append(names, lr.Name)
	}
	return names, nil
}

// LogicalRouterUpdateLoadBalancers add several lb to or from logical router once
func (c *OVNNbClient) LogicalRouterUpdateLoadBalancers(lrName string, op ovsdb.Mutator, lbNames ...string) error {
	if len(lbNames) == 0 {
		return nil
	}

	lbUUIDs := make([]string, 0, len(lbNames))

	for _, lbName := range lbNames {
		lb, err := c.GetLoadBalancer(lbName, true)
		if err != nil {
			klog.Error(err)
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
		klog.Error(err)
		return fmt.Errorf("generate operations for logical router %s update lbs %v: %w", lrName, lbNames, err)
	}

	if err := c.Transact("lr-lb-update", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("logical router %s update lbs %v: %w", lrName, lbNames, err)
	}

	return nil
}

// UpdateLogicalRouterOp generate operations which update logical router
func (c *OVNNbClient) UpdateLogicalRouterOp(lr *ovnnb.LogicalRouter, fields ...any) ([]ovsdb.Operation, error) {
	if lr == nil {
		return nil, errors.New("logical_router is nil")
	}

	op, err := c.ovsDbClient.Where(lr).Update(lr, fields...)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("generate operations for updating logical router %s: %w", lr.Name, err)
	}

	return op, nil
}

// LogicalRouterUpdatePortOp create operations add to or delete port from logical router
func (c *OVNNbClient) LogicalRouterUpdatePortOp(lrName, lrpUUID string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	if len(lrpUUID) == 0 {
		return nil, nil
	}

	if lrName == "" && op == ovsdb.MutateOperationDelete {
		lrList, err := c.ListLogicalRouter(false, func(lr *ovnnb.LogicalRouter) bool {
			return slices.Contains(lr.Ports, lrpUUID)
		})
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("failed to list LR by LRP UUID %s: %w", lrpUUID, err)
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
func (c *OVNNbClient) LogicalRouterUpdatePolicyOp(lrName string, policyUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
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
func (c *OVNNbClient) LogicalRouterUpdateNatOp(lrName string, natUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
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
func (c *OVNNbClient) LogicalRouterUpdateStaticRouteOp(lrName string, routeUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
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
func (c *OVNNbClient) LogicalRouterOp(lrName string, mutationsFunc ...func(lr *ovnnb.LogicalRouter) *model.Mutation) ([]ovsdb.Operation, error) {
	lr, err := c.GetLogicalRouter(lrName, false)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("get logical router %s: %w", lrName, err)
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

	ops, err := c.ovsDbClient.Where(lr).Mutate(lr, mutations...)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("generate operations for mutating logical router %s: %w", lrName, err)
	}

	return ops, nil
}
