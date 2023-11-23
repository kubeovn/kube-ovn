package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// CreateGatewayChassises create multiple gateway chassis once
func (c *OVNNbClient) CreateGatewayChassises(lrpName string, chassises ...string) error {
	op, err := c.CreateGatewayChassisesOp(lrpName, chassises)
	if err != nil {
		return fmt.Errorf("generate operations for creating gateway chassis %v", err)
	}

	if err = c.Transact("gateway-chassises-add", op); err != nil {
		return fmt.Errorf("create gateway chassis %v for logical router port %s: %v", chassises, lrpName, err)
	}

	return nil
}

// UpdateGatewayChassis update gateway chassis
func (c *OVNNbClient) UpdateGatewayChassis(gwChassis *ovnnb.GatewayChassis, fields ...interface{}) error {
	op, err := c.ovsDbClient.Where(gwChassis).Update(gwChassis, fields...)
	if err != nil {
		err := fmt.Errorf("failed to generate operations for gateway chassis %s with fields %v: %v", gwChassis.ChassisName, fields, err)
		klog.Error(err)
		return err
	}
	if err = c.Transact("gateway-chassis-update", op); err != nil {
		err := fmt.Errorf("failed to update gateway chassis %s: %v", gwChassis.ChassisName, err)
		klog.Error(err)
		return err
	}
	return nil
}

// DeleteGatewayChassises delete multiple gateway chassis once
func (c *OVNNbClient) DeleteGatewayChassises(lrpName string, chassises []string) error {
	if len(chassises) == 0 {
		return nil
	}

	ops := make([]ovsdb.Operation, 0, len(chassises))

	for _, chassisName := range chassises {
		gwChassisName := lrpName + "-" + chassisName
		op, err := c.DeleteGatewayChassisOp(gwChassisName)
		if err != nil {
			klog.Error(err)
			return nil
		}

		// ignore non-existent object
		if len(op) == 0 {
			continue
		}

		ops = append(ops, op...)
	}

	if err := c.Transact("gateway-chassises-delete", ops); err != nil {
		return fmt.Errorf("delete gateway chassises %v from logical router port %s: %v", chassises, lrpName, err)
	}

	return nil
}

// ListGatewayChassisByLogicalRouterPort get gateway chassis by lrp name
func (c *OVNNbClient) ListGatewayChassisByLogicalRouterPort(lrpName string, ignoreNotFound bool) ([]ovnnb.GatewayChassis, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	gwChassisList := make([]ovnnb.GatewayChassis, 0)
	if err := c.ovsDbClient.WhereCache(func(gwChassis *ovnnb.GatewayChassis) bool {
		if gwChassis.ExternalIDs != nil && gwChassis.ExternalIDs["lrp"] == lrpName {
			return true
		}
		return false
	}).List(ctx, &gwChassisList); err != nil {
		if ignoreNotFound && err == client.ErrNotFound {
			return nil, nil
		}
		err = fmt.Errorf("failed to list gw chassis for lrp %s: %v", lrpName, err)
		klog.Error(err)
		return nil, err
	}

	return gwChassisList, nil
}

// GetGatewayChassis get gateway chassis by name
func (c *OVNNbClient) GetGatewayChassis(name string, ignoreNotFound bool) (*ovnnb.GatewayChassis, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	gwChassis := &ovnnb.GatewayChassis{Name: name}
	if err := c.Get(ctx, gwChassis); err != nil {
		if ignoreNotFound && err == client.ErrNotFound {
			return nil, nil
		}

		return nil, fmt.Errorf("get gateway chassis %s: %v", name, err)
	}

	return gwChassis, nil
}

func (c *OVNNbClient) GatewayChassisExist(name string) (bool, error) {
	gwChassis, err := c.GetGatewayChassis(name, true)
	return gwChassis != nil, err
}

// newGatewayChassis return gateway chassis with basic information
func (c *OVNNbClient) newGatewayChassis(lrpName, chassisName string, priority int) (*ovnnb.GatewayChassis, error) {
	gwChassisName := lrpName + "-" + chassisName
	exists, err := c.GatewayChassisExist(gwChassisName)
	if err != nil {
		klog.Error(err)
		return nil, err
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
		gwChassis, err := c.newGatewayChassis(lrpName, chassisName, util.GwChassisMaxPriority-i)
		if err != nil {
			klog.Error(err)
			return nil, err
		}

		// found, skip
		if gwChassis != nil {
			models = append(models, model.Model(gwChassis))
			uuids = append(uuids, gwChassis.UUID)
		}
	}

	gwChassisCreateop, err := c.Create(models...)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("generate operations for creating gateway chassis %v", err)
	}

	/* add gateway chassis to logical router port */
	gwChassisAddOp, err := c.LogicalRouterPortUpdateGatewayChassisOp(lrpName, uuids, ovsdb.MutateOperationInsert)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	ops := make([]ovsdb.Operation, 0, len(gwChassisCreateop)+len(gwChassisAddOp))
	ops = append(ops, gwChassisCreateop...)
	ops = append(ops, gwChassisAddOp...)

	return ops, nil
}

// DeleteGatewayChassisOp create operation which delete gateway chassis
func (c *OVNNbClient) DeleteGatewayChassisOp(chassisName string) ([]ovsdb.Operation, error) {
	gwChassis, err := c.GetGatewayChassis(chassisName, true)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	// not found, skip
	if gwChassis == nil {
		return nil, nil
	}

	op, err := c.Where(gwChassis).Delete()
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	return op, nil
}
