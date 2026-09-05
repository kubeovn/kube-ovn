package ovs

import (
	"context"
	"fmt"
	"slices"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

// ListInterface lists ovs interfaces
func (c *VswitchClient) ListInterface(filter func(sw *vswitch.Interface) bool) ([]vswitch.Interface, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var ifaceList []vswitch.Interface
	if err := c.Database.Table(&vswitch.Interface{}).Filter(ctx, func(iface *vswitch.Interface) bool {
		if filter != nil {
			return filter(iface)
		}
		return true
	}, &ifaceList); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to list interface: %w", err)
	}

	return ifaceList, nil
}

// CleanInterface removes an orphaned OVS port and the rows owned by it. The
// operation is assembled from the monitored tables so callers do not need to
// shell out to ovs-vsctl for database state changes.
func (c *VswitchClient) CleanInterface(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var ports []vswitch.Port
	if err := c.Database.Table(&vswitch.Port{}).Filter(ctx, func(port *vswitch.Port) bool {
		return port.Name == name
	}, &ports); err != nil {
		return fmt.Errorf("find OVS port %q: %w", name, err)
	}
	if len(ports) == 0 {
		return nil
	}

	var bridges []vswitch.Bridge
	if err := c.Database.Table(&vswitch.Bridge{}).List(ctx, &bridges); err != nil {
		return fmt.Errorf("list OVS bridges while cleaning port %q: %w", name, err)
	}

	bridgeTable := c.Table(&vswitch.Bridge{})
	portTable := c.Table(&vswitch.Port{})
	interfaceTable := c.Table(&vswitch.Interface{})
	qosTable := c.Table(&vswitch.QoS{})
	operations := make([]ovsdb.Operation, 0, len(ports)*4)
	interfaceIDs := make(map[string]struct{})
	qosIDs := make(map[string]struct{})

	for i := range ports {
		port := &ports[i]
		for _, interfaceID := range port.Interfaces {
			interfaceIDs[interfaceID] = struct{}{}
		}
		if port.QOS != nil && *port.QOS != "" {
			qosIDs[*port.QOS] = struct{}{}
		}
		for i := range bridges {
			bridge := &bridges[i]
			if !slices.Contains(bridge.Ports, port.UUID) {
				continue
			}
			ops, err := bridgeTable.MutateOps(bridge, model.Mutation{
				Field: &bridge.Ports, Value: []string{port.UUID}, Mutator: ovsdb.MutateOperationDelete,
			})
			if err != nil {
				return fmt.Errorf("detach OVS port %q from bridge %q: %w", name, bridge.Name, err)
			}
			operations = append(operations, ops...)
		}
		ops, err := portTable.DeleteOps(port)
		if err != nil {
			return fmt.Errorf("delete OVS port %q: %w", name, err)
		}
		operations = append(operations, ops...)
	}

	for interfaceID := range interfaceIDs {
		ops, err := interfaceTable.DeleteOps(&vswitch.Interface{UUID: interfaceID})
		if err != nil {
			return fmt.Errorf("delete OVS interface %q: %w", interfaceID, err)
		}
		operations = append(operations, ops...)
	}
	for qosID := range qosIDs {
		ops, err := qosTable.DeleteOps(&vswitch.QoS{UUID: qosID})
		if err != nil {
			return fmt.Errorf("delete OVS QoS %q: %w", qosID, err)
		}
		operations = append(operations, ops...)
	}

	if len(operations) == 0 {
		return nil
	}
	return portTable.Transact(ctx, "vswitch-interface-cleanup", operations...)
}
