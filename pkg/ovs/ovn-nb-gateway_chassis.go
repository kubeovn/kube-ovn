package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

// CreateGatewayChassises create multiple gateway chassis once
func (c *ovnClient) CreateGatewayChassises(lrpName string, chassises ...string) error {
	op, err := c.CreateGatewayChassisesOp(lrpName, chassises)
	if err != nil {
		return fmt.Errorf("generate operations for creating gateway chassis %v", err)
	}

	if err = c.Transact("gateway-chassises-add", op); err != nil {
		return fmt.Errorf("create gateway chassis %v for logical router port %s: %v", chassises, lrpName, err)
	}

	return nil
}

// DeleteGatewayChassises delete multiple gateway chassis once
func (c *ovnClient) DeleteGatewayChassises(lrpName string, chassises []string) error {
	if len(chassises) == 0 {
		return nil
	}

	ops := make([]ovsdb.Operation, 0, len(chassises))

	for _, chassisName := range chassises {
		gwChassisName := lrpName + "-" + chassisName
		op, err := c.DeleteGatewayChassisOp(gwChassisName)
		if err != nil {
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

// GetGatewayChassis get gateway chassis by name
func (c *ovnClient) GetGatewayChassis(name string, ignoreNotFound bool) (*ovnnb.GatewayChassis, error) {
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

func (c *ovnClient) GatewayChassisExist(name string) (bool, error) {
	gwChassis, err := c.GetGatewayChassis(name, true)
	return gwChassis != nil, err
}

// newGatewayChassis return gateway chassis with basic information
func (c *ovnClient) newGatewayChassis(gwChassisName, chassisName string, priority int) (*ovnnb.GatewayChassis, error) {
	exists, err := c.GatewayChassisExist(gwChassisName)
	if err != nil {
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
	}

	return gwChassis, nil
}

// DeleteGatewayChassisOp create operation which create gateway chassis
func (c *ovnClient) CreateGatewayChassisesOp(lrpName string, chassises []string) ([]ovsdb.Operation, error) {
	if len(chassises) == 0 {
		return nil, nil
	}

	models := make([]model.Model, 0, len(chassises))
	uuids := make([]string, 0, len(chassises))

	for i, chassisName := range chassises {
		gwChassisName := lrpName + "-" + chassisName
		gwChassis, err := c.newGatewayChassis(gwChassisName, chassisName, 100-i)
		if err != nil {
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
		return nil, fmt.Errorf("generate operations for creating gateway chassis %v", err)
	}

	/* add gateway chassis to logical router port */
	gwChassisAddOp, err := c.LogicalRouterPortUpdateGatewayChassisOp(lrpName, uuids, ovsdb.MutateOperationInsert)
	if err != nil {
		return nil, err
	}

	ops := make([]ovsdb.Operation, 0, len(gwChassisCreateop)+len(gwChassisAddOp))
	ops = append(ops, gwChassisCreateop...)
	ops = append(ops, gwChassisAddOp...)

	return ops, nil
}

// DeleteGatewayChassisOp create operation which delete gateway chassis
func (c *ovnClient) DeleteGatewayChassisOp(chassisName string) ([]ovsdb.Operation, error) {
	gwChassis, err := c.GetGatewayChassis(chassisName, true)

	if err != nil {
		return nil, err
	}

	// not found, skip
	if gwChassis == nil {
		return nil, nil
	}

	op, err := c.Where(gwChassis).Delete()
	if err != nil {
		return nil, err
	}

	return op, nil
}
