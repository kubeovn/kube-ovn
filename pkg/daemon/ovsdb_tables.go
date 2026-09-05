package daemon

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type databaseLifecycle interface {
	Close()
}

func (c *Controller) listVswitchBridges(needVendorFilter bool, filter func(*vswitch.Bridge) bool) ([]vswitch.Bridge, error) {
	if c.vswitchTables == nil {
		return c.vswitchClient.ListBridge(needVendorFilter, filter)
	}
	var rows []vswitch.Bridge
	err := c.vswitchTables.Table(&vswitch.Bridge{}).Filter(context.Background(), func(row *vswitch.Bridge) bool {
		if needVendorFilter && row.ExternalIDs[ovs.ExternalIDVendor] != util.CniTypeName {
			return false
		}
		return filter == nil || filter(row)
	}, &rows)
	return rows, err
}

func (c *Controller) listVswitchPorts(filter func(*vswitch.Port) bool) ([]vswitch.Port, error) {
	if c.vswitchTables == nil {
		return c.vswitchClient.ListPort(filter)
	}
	var rows []vswitch.Port
	err := c.vswitchTables.Table(&vswitch.Port{}).Filter(context.Background(), func(row *vswitch.Port) bool {
		return filter == nil || filter(row)
	}, &rows)
	return rows, err
}

func (c *Controller) listVswitchInterfaces(filter func(*vswitch.Interface) bool) ([]vswitch.Interface, error) {
	if c.vswitchTables == nil {
		return c.vswitchClient.ListInterface(filter)
	}
	var rows []vswitch.Interface
	err := c.vswitchTables.Table(&vswitch.Interface{}).Filter(context.Background(), func(row *vswitch.Interface) bool {
		return filter == nil || filter(row)
	}, &rows)
	return rows, err
}

func (c *Controller) getVswitchPortExternalID(name, key string) (string, error) {
	ports, err := c.listVswitchPorts(func(row *vswitch.Port) bool {
		return row.Name == name
	})
	if err != nil {
		return "", err
	}
	if len(ports) == 0 {
		return "", nil
	}
	return ports[0].ExternalIDs[key], nil
}

func cleanVswitchDuplicatePort(provider compat.TableProvider, ifaceID, portName string) error {
	if provider == nil {
		ovs.CleanDuplicatePort(ifaceID, portName)
		return nil
	}
	var interfaces []vswitch.Interface
	if err := provider.Table(&vswitch.Interface{}).Filter(context.Background(), func(row *vswitch.Interface) bool {
		return row.ExternalIDs["iface-id"] == ifaceID && row.Name != portName
	}, &interfaces); err != nil {
		return fmt.Errorf("list duplicate OVS interfaces for %s: %w", ifaceID, err)
	}
	for i := range interfaces {
		iface := &interfaces[i]
		if _, ok := iface.ExternalIDs["iface-id"]; !ok {
			continue
		}
		if err := provider.Table(&vswitch.Interface{}).Mutate(context.Background(), "daemon-interface-duplicate-cleanup", iface,
			model.Mutation{Field: &iface.ExternalIDs, Mutator: ovsdb.MutateOperationDelete, Value: map[string]string{"iface-id": ifaceID}}); err != nil {
			return fmt.Errorf("clear duplicate OVS interface %s: %w", iface.Name, err)
		}
	}
	return nil
}

func getVswitchInterfacePodNs(provider compat.TableProvider, ifaceID string) (string, error) {
	if provider == nil {
		return ovs.GetInterfacePodNs(ifaceID)
	}
	var interfaces []vswitch.Interface
	if err := provider.Table(&vswitch.Interface{}).Filter(context.Background(), func(row *vswitch.Interface) bool {
		return row.ExternalIDs["iface-id"] == ifaceID
	}, &interfaces); err != nil {
		return "", fmt.Errorf("list OVS interfaces for %s: %w", ifaceID, err)
	}
	if len(interfaces) == 0 {
		return "", nil
	}
	return interfaces[0].ExternalIDs["pod_netns"], nil
}

func (c *Controller) vswitchBridgeExists(name string) (bool, error) {
	if c.vswitchTables == nil {
		return ovs.BridgeExists(name)
	}
	bridges, err := c.listVswitchBridges(false, func(bridge *vswitch.Bridge) bool {
		return bridge.Name == name
	})
	if err != nil {
		return false, err
	}
	return len(bridges) != 0, nil
}

func (c *Controller) vswitchPortExists(name string) (bool, error) {
	if c.vswitchTables == nil {
		return ovs.PortExists(name)
	}
	ports, err := c.listVswitchPorts(func(port *vswitch.Port) bool {
		return port.Name == name
	})
	if err != nil {
		return false, err
	}
	return len(ports) != 0, nil
}

func (c *Controller) validateVswitchPortVendor(name string) (bool, error) {
	if c.vswitchTables == nil {
		return ovs.ValidatePortVendor(name)
	}
	ports, err := c.listVswitchPorts(func(port *vswitch.Port) bool {
		return port.Name == name
	})
	if err != nil {
		return false, err
	}
	return len(ports) != 0 && ports[0].ExternalIDs[ovs.ExternalIDVendor] == util.CniTypeName, nil
}

func (c *Controller) listVswitchBridgePorts(bridgeName string) ([]vswitch.Port, error) {
	if c.vswitchTables == nil {
		output, err := ovs.Exec("list-ports", bridgeName)
		if err != nil {
			return nil, err
		}
		ports := make([]vswitch.Port, 0)
		for portName := range strings.SplitSeq(output, "\n") {
			if portName != "" {
				ports = append(ports, vswitch.Port{Name: portName})
			}
		}
		return ports, nil
	}
	bridges, err := c.listVswitchBridges(false, func(bridge *vswitch.Bridge) bool {
		return bridge.Name == bridgeName
	})
	if err != nil {
		return nil, err
	}
	if len(bridges) == 0 {
		return nil, nil
	}
	portIDs := bridges[0].Ports
	return c.listVswitchPorts(func(port *vswitch.Port) bool {
		return slices.Contains(portIDs, port.UUID)
	})
}

func (c *Controller) vswitchPortToBridge(portName string) (string, error) {
	if c.vswitchTables == nil {
		return ovs.Exec("port-to-br", portName)
	}
	ports, err := c.listVswitchPorts(func(port *vswitch.Port) bool {
		return port.Name == portName
	})
	if err != nil {
		return "", err
	}
	if len(ports) == 0 {
		return "", nil
	}
	portID := ports[0].UUID
	bridges, err := c.listVswitchBridges(false, func(bridge *vswitch.Bridge) bool {
		return slices.Contains(bridge.Ports, portID)
	})
	if err != nil {
		return "", err
	}
	if len(bridges) == 0 {
		return "", nil
	}
	return bridges[0].Name, nil
}

func (c *Controller) closeVswitch() {
	if lifecycle, ok := c.vswitchTables.(databaseLifecycle); ok {
		lifecycle.Close()
		return
	}
	if c.vswitchClient != nil {
		c.vswitchClient.Close()
	}
}
