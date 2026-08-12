package ovs

import (
	"context"
	"errors"
	"fmt"

	"github.com/ovn-kubernetes/libovsdb/client"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
)

// GetPortBindingByLogicalPort returns the SB Port_Binding for a logical port name.
func (c *OVNSbClient) GetPortBindingByLogicalPort(logicalPort string, ignoreNotFound bool) (*ovnsb.PortBinding, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	if logicalPort == "" {
		return nil, errors.New("logical port name is empty")
	}

	var list []ovnsb.PortBinding
	if err := c.WhereCache(func(pb *ovnsb.PortBinding) bool {
		return pb.LogicalPort == logicalPort
	}).List(ctx, &list); err != nil {
		return nil, fmt.Errorf("list port binding for logical port %s: %w", logicalPort, err)
	}
	if len(list) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("port binding for logical port %s not found: %w", logicalPort, client.ErrNotFound)
	}
	if len(list) > 1 {
		return nil, fmt.Errorf("more than one port binding for logical port %s", logicalPort)
	}
	return &list[0], nil
}

// IsPortBindingChassisBound reports whether the Port_Binding is chassis-bound
// (and not explicitly down).
func IsPortBindingChassisBound(pb *ovnsb.PortBinding) bool {
	if pb == nil || pb.Chassis == nil || *pb.Chassis == "" {
		return false
	}
	if pb.Up != nil && !*pb.Up {
		return false
	}
	return true
}

// GetChassisNameByUUID returns Chassis.Name for a Chassis UUID.
func (c *OVNSbClient) GetChassisNameByUUID(uuid string) (string, error) {
	if uuid == "" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var list []ovnsb.Chassis
	if err := c.WhereCache(func(ch *ovnsb.Chassis) bool {
		return ch.UUID == uuid
	}).List(ctx, &list); err != nil {
		return "", fmt.Errorf("list chassis %s: %w", uuid, err)
	}
	if len(list) == 0 {
		klog.V(3).Infof("chassis uuid %s not found in cache", uuid)
		return uuid, nil
	}
	return list[0].Name, nil
}
