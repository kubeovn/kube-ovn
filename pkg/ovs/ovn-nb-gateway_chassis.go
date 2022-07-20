package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

// CreateGatewayChassises create multiple gateway chassis once
func (c OvnClient) CreateGatewayChassises(lrpName string, chassises []string) error {
	if len(chassises) == 0 {
		return nil
	}

	models := make([]model.Model, 0, len(chassises))

	for i, chassisName := range chassises {
		gwChassisName := lrpName + "-" + chassisName
		gwChassis := &ovnnb.GatewayChassis{
			Name:        gwChassisName,
			ChassisName: chassisName,
			Priority:    100 - i,
		}
		models = append(models, model.Model(gwChassis))
	}

	op, err := c.Create(models...)
	if err != nil {
		return fmt.Errorf("generate create operations for gateway chassis %v", err)
	}

	if err = c.Transact("gateway-chassises-create", op); err != nil {
		return fmt.Errorf("create gateway chassis for logical router port %s: %v", lrpName, err)
	}

	return nil
}

// CreateGatewayChassis create gateway chassis
func (c OvnClient) CreateGatewayChassis(gwChassis *ovnnb.GatewayChassis) error {
	if gwChassis == nil {
		return fmt.Errorf("gateway_chassis is nil")
	}

	op, err := c.Create(gwChassis)
	if err != nil {
		return fmt.Errorf("generate create operations for gateway chassis %s: %v", gwChassis.Name, err)
	}

	if err = c.Transact("gateway-chassis-create", op); err != nil {
		return fmt.Errorf("create gateway chassis %s: %v", gwChassis.Name, err)
	}

	return nil
}

// DeleteGatewayChassises delete multiple gateway chassis once
func (c OvnClient) DeleteGatewayChassises(lrpName string, chassises []string) error {
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

		ops = append(ops, op...)
	}

	if err := c.Transact("gateway-chassises-delete", ops); err != nil {
		return fmt.Errorf("delete gateway chassis for logical router port %s: %v", lrpName, err)
	}

	return nil
}

// DeleteGatewayChassis delete multiple gateway chassis
func (c OvnClient) DeleteGatewayChassisOp(chassisName string) ([]ovsdb.Operation, error) {
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

func (c OvnClient) GetGatewayChassis(name string, ignoreNotFound bool) (*ovnnb.GatewayChassis, error) {
	gwChassis := &ovnnb.GatewayChassis{Name: name}
	if err := c.Get(context.TODO(), gwChassis); err != nil {
		if ignoreNotFound && err == client.ErrNotFound {
			return nil, nil
		}

		return nil, fmt.Errorf("get gateway chassis %s: %v", name, err)
	}

	return gwChassis, nil
}
