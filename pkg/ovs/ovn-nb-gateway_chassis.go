package ovs

import (
	"errors"
	"fmt"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

// CreateGatewayChassises create multiple gateway chassis once
func (c *OVNNbClient) CreateGatewayChassises(lrpName string, chassises ...string) error {
	op, err := c.CreateGatewayChassisesOp(lrpName, chassises)
	return c.transactGenerated("gateway-chassises-add", op, err,
		wrapErr("generate operations for creating gateway chassis %w"),
		wrapErr("create gateway chassis %v for logical router port %s: %w", chassises, lrpName),
	)
}

// UpdateGatewayChassis update gateway chassis
func (c *OVNNbClient) UpdateGatewayChassis(gwChassis *ovnnb.GatewayChassis, fields ...any) error {
	op, err := c.Database.Table(&ovnnb.GatewayChassis{}).UpdateOps(gwChassis, gwChassis, fields...)
	return c.transactGenerated("gateway-chassis-update", op, err,
		wrapErr("failed to generate operations for gateway chassis %s with fields %v: %w", gwChassis.ChassisName, fields),
		wrapErr("failed to update gateway chassis %s: %w", gwChassis.ChassisName),
	)
}

// ListGatewayChassisByLogicalRouterPort get gateway chassis by lrp name
func (c *OVNNbClient) ListGatewayChassisByLogicalRouterPort(lrpName string, ignoreNotFound bool) ([]ovnnb.GatewayChassis, error) {
	gwChassisList, err := filterTimeout(c.Database, &ovnnb.GatewayChassis{}, func(gwChassis *ovnnb.GatewayChassis) bool {
		return gwChassis.ExternalIDs != nil && gwChassis.ExternalIDs["lrp"] == lrpName
	})
	if err != nil {
		if ignoreNotFound && errors.Is(err, compat.ErrNotFound) {
			return nil, nil
		}
		return nil, logFmt("failed to list gw chassis for lrp %s: %w", lrpName, err)
	}
	return gwChassisList, nil
}

func (c *OVNNbClient) GetGatewayChassis(name string, ignoreNotFound bool) (*ovnnb.GatewayChassis, error) {
	return getIndexedFmt(c.Database, &ovnnb.GatewayChassis{Name: name}, ignoreNotFound, func(err error) error {
		return fmt.Errorf("get gateway chassis %s: %w", name, err)
	})
}

func (c *OVNNbClient) GatewayChassisExist(name string) (bool, error) {
	return existsByGet(c.GetGatewayChassis, name)
}

// newGatewayChassis return gateway chassis with basic information
func (c *OVNNbClient) newGatewayChassis(lrpName, chassisName string, priority int) (*ovnnb.GatewayChassis, error) {
	gwChassisName := lrpName + "-" + chassisName
	exists, err := c.GatewayChassisExist(gwChassisName)
	if err != nil {
		return nil, logErr(err)
	}

	// found, skip
	if exists {
		return nil, nil
	}

	gwChassis := &ovnnb.GatewayChassis{
		UUID:        ovsclient.NamedUUID(),
		Name:        gwChassisName,
		ChassisName: chassisName,
		Priority:    priority,
		ExternalIDs: map[string]string{
			"lrp": lrpName,
		},
	}

	return gwChassis, nil
}

// CreateGatewayChassisesOp create operation which create gateway chassises
func (c *OVNNbClient) CreateGatewayChassisesOp(lrpName string, chassises []string) ([]ovsdb.Operation, error) {
	if len(chassises) == 0 {
		return nil, nil
	}

	models := make([]model.Model, 0, len(chassises))
	uuids := make([]string, 0, len(chassises))

	for i, chassisName := range chassises {
		gwChassisName := lrpName + "-" + chassisName
		gwChassis, err := c.GetGatewayChassis(gwChassisName, true)
		if err != nil {
			return nil, logErr(err)
		}
		if gwChassis != nil {
			continue
		}
		gwChassis, err = c.newGatewayChassis(lrpName, chassisName, 100-i)
		if err != nil {
			return nil, logErr(err)
		}

		// found, skip
		if gwChassis != nil {
			models = append(models, model.Model(gwChassis))
			uuids = append(uuids, gwChassis.UUID)
		}
	}

	gwChassisCreateop, err := c.Database.Table(&ovnnb.GatewayChassis{}).CreateOps(models...)
	if err != nil {
		return nil, logWrap(err, wrapErr("generate operations for creating gateway chassis %w"))
	}

	/* add gateway chassis to logical router port */
	gwChassisAddOp, err := c.LogicalRouterPortUpdateGatewayChassisOp(lrpName, uuids, ovsdb.MutateOperationInsert)
	if err != nil {
		return nil, logErr(err)
	}

	return appendOps(gwChassisCreateop, gwChassisAddOp), nil
}

// DeleteGatewayChassises delete multiple gateway chassis once
func (c *OVNNbClient) DeleteGatewayChassises(lrpName string, chassises []string) error {
	if len(chassises) == 0 {
		return nil
	}

	lrp, err := c.GetLogicalRouterPort(lrpName, false)
	if err != nil {
		return logErr(err)
	}

	ops := make([]ovsdb.Operation, 0, len(chassises)*2)
	for _, chassisName := range chassises {
		gwChassisName := lrpName + "-" + chassisName
		uuid, delOps, err := c.DeleteGatewayChassisOp(gwChassisName)
		if err != nil {
			return logErr(err)
		}
		if uuid == "" {
			continue
		}

		mutateOps, err := c.Database.Table(&ovnnb.LogicalRouterPort{}).MutateOps(lrp, model.Mutation{
			Field:   &lrp.GatewayChassis,
			Value:   []string{uuid},
			Mutator: ovsdb.MutateOperationDelete,
		})
		if err != nil {
			return logErr(err)
		}

		ops = append(ops, mutateOps...)
		ops = append(ops, delOps...)
	}
	if len(ops) == 0 {
		return nil
	}

	return c.transactGenerated("gateway-chassises-delete", ops, nil, nil,
		wrapErr("delete gateway chassises %v from logical router port %s: %w", chassises, lrpName),
	)
}

// DeleteGatewayChassisOp create operation which delete gateway chassis
func (c *OVNNbClient) DeleteGatewayChassisOp(chassisName string) (uuid string, ops []ovsdb.Operation, err error) {
	gwChassis, err := c.GetGatewayChassis(chassisName, true)
	if err != nil {
		klog.Error(err)
		return "", nil, err
	}

	// not found, skip
	if gwChassis == nil {
		return "", nil, nil
	}

	if ops, err = c.Database.Table(&ovnnb.GatewayChassis{}).DeleteOps(gwChassis); err != nil {
		klog.Error(err)
		return "", nil, err
	}

	return gwChassis.UUID, ops, nil
}

// ReconcileGatewayChassises make gateway chassis of a logical router port match the desired list and priority.
func (c *OVNNbClient) ReconcileGatewayChassises(lrpName string, chassises []string) error {
	existing, err := c.ListGatewayChassisByLogicalRouterPort(lrpName, true)
	if err != nil {
		return logErr(err)
	}

	desired := make(map[string]int, len(chassises))
	for i, chassisName := range chassises {
		if chassisName != "" {
			desired[chassisName] = 100 - i
		}
	}

	var stale []string
	for _, gw := range existing {
		if _, ok := desired[gw.ChassisName]; !ok {
			stale = append(stale, gw.ChassisName)
		}
	}
	if err := c.DeleteGatewayChassises(lrpName, stale); err != nil {
		return logErr(err)
	}
	if err := c.CreateGatewayChassises(lrpName, chassises...); err != nil {
		return logErr(err)
	}

	for chassisName, priority := range desired {
		gwChassis, err := c.GetGatewayChassis(lrpName+"-"+chassisName, false)
		if err != nil {
			return logErr(err)
		}
		if gwChassis.Priority == priority {
			continue
		}
		gwChassis.Priority = priority
		if err := c.UpdateGatewayChassis(gwChassis, &gwChassis.Priority); err != nil {
			return logErr(err)
		}
	}
	return nil
}
