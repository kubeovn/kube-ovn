package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

// CreateGatewayChassises create multiple gateway chassis once
func (c OvnClient) CreateGatewayChassises(namePrefix string, chassises []string) error {
	models := make([]model.Model, 0, len(chassises))

	for i, chassisName := range chassises {
		gwChassisName := namePrefix + "-" + chassisName
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

	err = c.Transact("gateway-chassises-create", op)
	if err != nil {
		return fmt.Errorf("create gateway chassis: %v", err)
	}

	return nil
}

// CreateGatewayChassis create gateway chassis
func (c OvnClient) CreateGatewayChassis(gwChassis *ovnnb.GatewayChassis) error {
	if nil == gwChassis {
		return fmt.Errorf("gateway_chassis is nil")
	}

	op, err := c.Create(gwChassis)
	if err != nil {
		return fmt.Errorf("generate create operations for gateway chassis %s: %v", gwChassis.Name, err)
	}

	err = c.Transact("gateway-chassis-create", op)
	if err != nil {
		return fmt.Errorf("create gateway chassis %s: %v", gwChassis.Name, err)
	}

	return nil
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
