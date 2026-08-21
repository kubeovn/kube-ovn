package ovs

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/ovn-kubernetes/libovsdb/client"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vtep"
)

// GetPhysicalSwitch returns a Physical_Switch by name.
func (c *VtepClient) GetPhysicalSwitch(name string, ignoreNotFound bool) (*vtep.PhysicalSwitch, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	if name == "" {
		return nil, errors.New("physical switch name is empty")
	}

	var list []vtep.PhysicalSwitch
	if err := c.WhereCache(func(ps *vtep.PhysicalSwitch) bool {
		return ps.Name == name
	}).List(ctx, &list); err != nil {
		return nil, fmt.Errorf("list physical switch %s: %w", name, err)
	}
	if len(list) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("physical switch %s not found: %w", name, client.ErrNotFound)
	}
	if len(list) > 1 {
		return nil, fmt.Errorf("more than one physical switch with name %s", name)
	}
	return &list[0], nil
}

// GetPhysicalPort returns a Physical_Port by name that belongs to the given Physical_Switch.
func (c *VtepClient) GetPhysicalPort(physicalSwitch, portName string, ignoreNotFound bool) (*vtep.PhysicalPort, error) {
	ps, err := c.GetPhysicalSwitch(physicalSwitch, ignoreNotFound)
	if err != nil {
		return nil, err
	}
	if ps == nil {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var ports []vtep.PhysicalPort
	if err = c.WhereCache(func(port *vtep.PhysicalPort) bool {
		return port.Name == portName && slices.Contains(ps.Ports, port.UUID)
	}).List(ctx, &ports); err != nil {
		return nil, fmt.Errorf("list physical port %s on switch %s: %w", portName, physicalSwitch, err)
	}
	if len(ports) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("physical port %s not found on physical switch %s: %w", portName, physicalSwitch, client.ErrNotFound)
	}
	if len(ports) > 1 {
		return nil, fmt.Errorf("more than one physical port %s on switch %s", portName, physicalSwitch)
	}
	return &ports[0], nil
}
