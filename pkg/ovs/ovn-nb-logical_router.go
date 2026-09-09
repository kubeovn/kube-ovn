package ovs

import (
	"errors"
	"fmt"
	"slices"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/ovs/nbops"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// CreateLogicalRouter create logical router in ovn
func (c *OVNNbClient) CreateLogicalRouter(lrName string) error {
	lr := &ovnnb.LogicalRouter{
		Name:        lrName,
		ExternalIDs: map[string]string{"vendor": util.CniTypeName},
	}
	return logErr(c.logicalRouterTable().createIfAbsent(lrName, "lr-add", lr))
}

// UpdateLogicalRouter update logical router
func (c *OVNNbClient) UpdateLogicalRouter(lr *ovnnb.LogicalRouter, fields ...any) error {
	op, err := c.UpdateLogicalRouterOp(lr, fields...)
	return c.transactGenerated("lr-update", op, err, nil, func(err error) error {
		return fmt.Errorf("update logical router %s: %w", lr.Name, err)
	})
}

// DeleteLogicalRouter delete logical router in ovn
func (c *OVNNbClient) DeleteLogicalRouter(lrName string) error {
	lr, err := c.logicalRouterTable().get(lrName, true)
	if err != nil {
		return logWrap(err, wrapErr("get logical router %s when delete: %w", lrName))
	}
	if lr == nil {
		return nil
	}

	op, err := c.logicalRouterTable().delete(lr)
	return c.transactGenerated("lr-del", op, err, nil, wrapErr("delete logical router %s: %w", lrName))
}

// GetLogicalRouter get logical router by name,
// it is because of lack name index that doesn't use OVNNbClient.Get
func (c *OVNNbClient) GetLogicalRouter(lrName string, ignoreNotFound bool) (*ovnnb.LogicalRouter, error) {
	return logRet(c.logicalRouterTable().get(lrName, ignoreNotFound))
}

func (c *OVNNbClient) listRouterChildren[T any](lrName string, field func(*ovnnb.LogicalRouter) []string, prototype model.Model, uuidOf func(*T) string, filter func(*T) bool) ([]*T, error) {
	lr, err := c.GetLogicalRouter(lrName, false)
	if err != nil {
		return nil, logErr(err)
	}
	return logRet(listByUUIDs(c.Database, prototype, field(lr), uuidOf, filter))
}

func (c *OVNNbClient) LogicalRouterExists(name string) (bool, error) {
	return existsByGet(c.GetLogicalRouter, name)
}

// ListLogicalRouter list logical router
func (c *OVNNbClient) ListLogicalRouter(needVendorFilter bool, filter func(lr *ovnnb.LogicalRouter) bool) ([]ovnnb.LogicalRouter, error) {
	return logRet(c.logicalRouterTable().list(needVendorFilter, filter))
}

// ListLogicalRouterNames list logical router names
func (c *OVNNbClient) ListLogicalRouterNames(needVendorFilter bool, filter func(lr *ovnnb.LogicalRouter) bool) ([]string, error) {
	lrList, err := c.ListLogicalRouter(needVendorFilter, filter)
	return namesFrom(lrList, err, c.logicalRouterTable().names)
}

// LogicalRouterUpdateLoadBalancers add several lb to or from logical router once
func (c *OVNNbClient) LogicalRouterUpdateLoadBalancers(lrName string, op ovsdb.Mutator, lbNames ...string) error {
	if len(lbNames) == 0 {
		return nil
	}

	lbUUIDs, err := c.loadBalancerUUIDs(lbNames...)
	if err != nil {
		return err
	}

	ops, err := c.LogicalRouterOp(lrName, func(lr *ovnnb.LogicalRouter) *model.Mutation {
		return &model.Mutation{
			Field:   &lr.LoadBalancer,
			Value:   lbUUIDs,
			Mutator: op,
		}
	})
	return c.transactGenerated("lr-lb-update", ops, err,
		wrapErr("generate operations for logical router %s update lbs %v: %w", lrName, lbNames),
		wrapErr("logical router %s update lbs %v: %w", lrName, lbNames),
	)
}

// UpdateLogicalRouterOp generate operations which update logical router
func (c *OVNNbClient) UpdateLogicalRouterOp(lr *ovnnb.LogicalRouter, fields ...any) ([]ovsdb.Operation, error) {
	if lr == nil {
		return nil, errors.New("logical_router is nil")
	}

	op, err := c.Database.Table(&ovnnb.LogicalRouter{}).UpdateOps(lr, lr, fields...)
	if err != nil {
		return nil, logWrap(err, wrapErr("generate operations for updating logical router %s: %w", lr.Name))
	}

	return op, nil
}

// EnsureLogicalRouterPortParent reconciles a port's ownership across all
// logical routers. New callers should use this when they need
// detach-before-attach semantics.
func (c *OVNNbClient) EnsureLogicalRouterPortParent(lrpName, lrName string) error {
	ctx, cancel := timeoutCtx(c.Database)
	defer cancel()
	return nbops.NewLogicalRouterPorts(c.Database, c.Database).EnsureParent(ctx, lrpName, lrName)
}

// LogicalRouterUpdatePortOp create operations add to or delete port from logical router
func (c *OVNNbClient) LogicalRouterUpdatePortOp(lrName, lrpUUID string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	return c.logicalRouterTable().mutateOwnedUUID(lrName, lrpUUID, op,
		func(lr *ovnnb.LogicalRouter, uuid string) bool { return slices.Contains(lr.Ports, uuid) },
		func(lr *ovnnb.LogicalRouter) *[]string { return &lr.Ports },
		"LRP", "LR")
}

func (c *OVNNbClient) detachRouterUUIDs(lrName string, uuids []string, method, item string, update func(string, []string, ovsdb.Mutator) ([]ovsdb.Operation, error)) error {
	if len(uuids) == 0 {
		return nil
	}
	ops, err := update(lrName, uuids, ovsdb.MutateOperationDelete)
	if err != nil {
		return logWrap(err, wrapErr("generate operations for removing %s from logical router %s: %w", item, lrName))
	}
	if err := transactOps(c, method, ops); err != nil {
		return logWrap(err, wrapErr("delete %s from logical router %s: %w", item, lrName))
	}
	return nil
}

// LogicalRouterUpdatePolicyOp create operations add to or delete policy from logical router
func (c *OVNNbClient) LogicalRouterUpdatePolicyOp(lrName string, policyUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	return c.logicalRouterTable().mutateUUIDs(lrName, func(lr *ovnnb.LogicalRouter) *[]string { return &lr.Policies }, policyUUIDs, op)
}

// LogicalRouterUpdateNatOp create operations add to or delete nat rule from logical router
func (c *OVNNbClient) LogicalRouterUpdateNatOp(lrName string, natUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	return c.logicalRouterTable().mutateUUIDs(lrName, func(lr *ovnnb.LogicalRouter) *[]string { return &lr.Nat }, natUUIDs, op)
}

// LogicalRouterUpdateStaticRouteOp create operations add to or delete static route from logical router
func (c *OVNNbClient) LogicalRouterUpdateStaticRouteOp(lrName string, routeUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	return c.logicalRouterTable().mutateUUIDs(lrName, func(lr *ovnnb.LogicalRouter) *[]string { return &lr.StaticRoutes }, routeUUIDs, op)
}

// LogicalRouterOp create operations about logical router
func (c *OVNNbClient) LogicalRouterOp(lrName string, mutationsFunc ...func(lr *ovnnb.LogicalRouter) *model.Mutation) ([]ovsdb.Operation, error) {
	return c.logicalRouterTable().mutateNamed(lrName, mutationsFunc...)
}

func (c *OVNNbClient) logicalRouterTable() namedTable[ovnnb.LogicalRouter] {
	return newNamedTable(c.Database, &ovnnb.LogicalRouter{}, "logical router",
		func(lr *ovnnb.LogicalRouter) string { return lr.Name },
		func(lr *ovnnb.LogicalRouter) map[string]string { return lr.ExternalIDs })
}
