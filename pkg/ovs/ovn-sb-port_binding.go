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
		err := errors.New("logical port name is empty")
		klog.Error(err)
		return nil, err
	}

	var list []ovnsb.PortBinding
	if err := c.WhereCache(func(pb *ovnsb.PortBinding) bool {
		return pb.LogicalPort == logicalPort
	}).List(ctx, &list); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("list port binding for logical port %s: %w", logicalPort, err)
	}
	if len(list) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		err := fmt.Errorf("port binding for logical port %s not found: %w", logicalPort, client.ErrNotFound)
		klog.Error(err)
		return nil, err
	}
	if len(list) > 1 {
		err := fmt.Errorf("more than one port binding for logical port %s", logicalPort)
		klog.Error(err)
		return nil, err
	}
	return &list[0], nil
}

// IsPortBindingChassisBound reports whether the Port_Binding has a non-empty
// chassis. Ready does not require up=true: ovn-controller-vtep can leave
// Port_Binding.up=false after chassis is already assigned.
func IsPortBindingChassisBound(pb *ovnsb.PortBinding) bool {
	return pb != nil && pb.Chassis != nil && *pb.Chassis != ""
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
		klog.Error(err)
		return "", fmt.Errorf("list chassis %s: %w", uuid, err)
	}
	if len(list) == 0 {
		klog.V(3).Infof("chassis uuid %s not found in cache", uuid)
		return uuid, nil
	}
	return list[0].Name, nil
}
