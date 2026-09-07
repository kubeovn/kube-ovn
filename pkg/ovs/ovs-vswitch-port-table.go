package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

// VswitchPortConfig describes one Port and Interface pair attached to a
// bridge. Fields lists control partial updates when the rows already exist;
// newly created rows use the complete models.
type VswitchPortConfig struct {
	BridgeName      string
	Port            *vswitch.Port
	Interface       *vswitch.Interface
	PortFields      []any
	InterfaceFields []any
}

// EnsureVswitchPort creates or updates an OVS Port/Interface pair and attaches
// it to a bridge in one transaction.
func EnsureVswitchPort(ctx context.Context, provider compat.TableProvider, config VswitchPortConfig) error {
	if err := validateVswitchPortConfig(provider, config); err != nil {
		return err
	}

	bridge, err := findVswitchBridge(ctx, provider, config.BridgeName)
	if err != nil {
		return err
	}
	port, err := findVswitchPort(ctx, provider, config.Port.Name)
	if err != nil {
		return err
	}
	iface, err := findVswitchInterface(ctx, provider, config.Interface.Name)
	if err != nil {
		return err
	}

	bridgeTable := provider.Table(&vswitch.Bridge{})
	portTable := provider.Table(&vswitch.Port{})
	interfaceTable := provider.Table(&vswitch.Interface{})
	operations := make([]ovsdb.Operation, 0, 5)

	if iface == nil {
		config.Interface.UUID = ovsclient.NamedUUID()
		createOps, err := interfaceTable.CreateOps(config.Interface)
		if err != nil {
			return fmt.Errorf("create OVS interface %q operation: %w", config.Interface.Name, err)
		}
		operations = append(operations, createOps...)
		iface = config.Interface
	} else if len(config.InterfaceFields) != 0 {
		mergeVswitchInterfaceMaps(iface, config.Interface)
		config.Interface.UUID = iface.UUID
		updateOps, err := interfaceTable.UpdateOps(iface, config.Interface, config.InterfaceFields...)
		if err != nil {
			return fmt.Errorf("update OVS interface %q operation: %w", config.Interface.Name, err)
		}
		operations = append(operations, updateOps...)
	}

	if port == nil {
		config.Port.UUID = ovsclient.NamedUUID()
		config.Port.Interfaces = []string{iface.UUID}
		createOps, err := portTable.CreateOps(config.Port)
		if err != nil {
			return fmt.Errorf("create OVS port %q operation: %w", config.Port.Name, err)
		}
		operations = append(operations, createOps...)
		port = config.Port
	} else {
		if len(port.Interfaces) != 0 && !slices.Contains(port.Interfaces, iface.UUID) {
			return fmt.Errorf("OVS port %q does not reference interface %q", port.Name, iface.Name)
		}
		config.Port.UUID = port.UUID
		if len(config.PortFields) != 0 {
			mergeVswitchPortMaps(port, config.Port)
			updateOps, err := portTable.UpdateOps(port, config.Port, config.PortFields...)
			if err != nil {
				return fmt.Errorf("update OVS port %q operation: %w", config.Port.Name, err)
			}
			operations = append(operations, updateOps...)
		}
		if len(port.Interfaces) == 0 {
			mutateOps, err := portTable.MutateOps(port, model.Mutation{
				Field: &port.Interfaces, Value: []string{iface.UUID}, Mutator: ovsdb.MutateOperationInsert,
			})
			if err != nil {
				return fmt.Errorf("attach OVS interface %q to port %q: %w", iface.Name, port.Name, err)
			}
			operations = append(operations, mutateOps...)
		}
	}

	attached, err := validateVswitchPortBridge(ctx, provider, port, bridge)
	if err != nil {
		return err
	}
	if !attached {
		mutateOps, err := bridgeTable.MutateOps(bridge, model.Mutation{
			Field: &bridge.Ports, Value: []string{port.UUID}, Mutator: ovsdb.MutateOperationInsert,
		})
		if err != nil {
			return fmt.Errorf("attach OVS port %q to bridge %q: %w", port.Name, bridge.Name, err)
		}
		operations = append(operations, mutateOps...)
	}

	if err := portTable.Transact(ctx, "vswitch-port-ensure", operations...); err != nil {
		return err
	}
	return waitForVswitchPort(ctx, provider, config)
}

func validateVswitchPortConfig(provider compat.TableProvider, config VswitchPortConfig) error {
	if provider == nil {
		return errors.New("ovsdb table provider is nil")
	}
	if config.BridgeName == "" {
		return errors.New("OVS bridge name is empty")
	}
	if config.Port == nil || config.Port.Name == "" {
		return errors.New("OVS port name is empty")
	}
	if config.Interface == nil || config.Interface.Name == "" {
		return errors.New("OVS interface name is empty")
	}
	return nil
}

func waitForVswitchPort(ctx context.Context, provider compat.TableProvider, config VswitchPortConfig) error {
	var interfaces []vswitch.Interface
	if err := compat.WaitForRows(ctx, provider, &vswitch.Interface{}, func(row *vswitch.Interface) bool {
		return row.Name == config.Interface.Name
	}, &interfaces); err != nil {
		return fmt.Errorf("wait for OVS interface %q cache update: %w", config.Interface.Name, err)
	}
	var ports []vswitch.Port
	if err := compat.WaitForRows(ctx, provider, &vswitch.Port{}, func(row *vswitch.Port) bool {
		return row.Name == config.Port.Name
	}, &ports); err != nil {
		return fmt.Errorf("wait for OVS port %q cache update: %w", config.Port.Name, err)
	}
	if len(ports) != 1 {
		return fmt.Errorf("wait for OVS port %q cache update returned %d rows", config.Port.Name, len(ports))
	}
	portUUID := ports[0].UUID
	var bridges []vswitch.Bridge
	if err := compat.WaitForRows(ctx, provider, &vswitch.Bridge{}, func(row *vswitch.Bridge) bool {
		return row.Name == config.BridgeName && slices.Contains(row.Ports, portUUID)
	}, &bridges); err != nil {
		return fmt.Errorf("wait for OVS bridge %q cache update: %w", config.BridgeName, err)
	}
	return nil
}

func mergeVswitchInterfaceMaps(current, desired *vswitch.Interface) {
	if desired.ExternalIDs != nil {
		merged := maps.Clone(current.ExternalIDs)
		if merged == nil {
			merged = make(map[string]string, len(desired.ExternalIDs))
		}
		maps.Copy(merged, desired.ExternalIDs)
		desired.ExternalIDs = merged
	}
	if desired.Options != nil {
		merged := maps.Clone(current.Options)
		if merged == nil {
			merged = make(map[string]string, len(desired.Options))
		}
		maps.Copy(merged, desired.Options)
		desired.Options = merged
	}
	if desired.OtherConfig != nil {
		merged := maps.Clone(current.OtherConfig)
		if merged == nil {
			merged = make(map[string]string, len(desired.OtherConfig))
		}
		maps.Copy(merged, desired.OtherConfig)
		desired.OtherConfig = merged
	}
}

func mergeVswitchPortMaps(current, desired *vswitch.Port) {
	if desired.ExternalIDs != nil {
		merged := maps.Clone(current.ExternalIDs)
		if merged == nil {
			merged = make(map[string]string, len(desired.ExternalIDs))
		}
		maps.Copy(merged, desired.ExternalIDs)
		desired.ExternalIDs = merged
	}
	if desired.OtherConfig != nil {
		merged := maps.Clone(current.OtherConfig)
		if merged == nil {
			merged = make(map[string]string, len(desired.OtherConfig))
		}
		maps.Copy(merged, desired.OtherConfig)
		desired.OtherConfig = merged
	}
}

func findVswitchBridge(ctx context.Context, provider compat.TableProvider, name string) (*vswitch.Bridge, error) {
	var rows []vswitch.Bridge
	if err := provider.Table(&vswitch.Bridge{}).Filter(ctx, func(row *vswitch.Bridge) bool {
		return row.Name == name
	}, &rows); err != nil {
		return nil, fmt.Errorf("find OVS bridge %q: %w", name, err)
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("expected one OVS bridge %q, found %d", name, len(rows))
	}
	return &rows[0], nil
}

func findVswitchPort(ctx context.Context, provider compat.TableProvider, name string) (*vswitch.Port, error) {
	var rows []vswitch.Port
	if err := provider.Table(&vswitch.Port{}).Filter(ctx, func(row *vswitch.Port) bool {
		return row.Name == name
	}, &rows); err != nil {
		return nil, fmt.Errorf("find OVS port %q: %w", name, err)
	}
	if len(rows) > 1 {
		return nil, fmt.Errorf("expected at most one OVS port %q, found %d", name, len(rows))
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func findVswitchInterface(ctx context.Context, provider compat.TableProvider, name string) (*vswitch.Interface, error) {
	var rows []vswitch.Interface
	if err := provider.Table(&vswitch.Interface{}).Filter(ctx, func(row *vswitch.Interface) bool {
		return row.Name == name
	}, &rows); err != nil {
		return nil, fmt.Errorf("find OVS interface %q: %w", name, err)
	}
	if len(rows) > 1 {
		return nil, fmt.Errorf("expected at most one OVS interface %q, found %d", name, len(rows))
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func validateVswitchPortBridge(ctx context.Context, provider compat.TableProvider, port *vswitch.Port, target *vswitch.Bridge) (bool, error) {
	var bridges []vswitch.Bridge
	if err := provider.Table(&vswitch.Bridge{}).List(ctx, &bridges); err != nil {
		return false, fmt.Errorf("list OVS bridges for port %q: %w", port.Name, err)
	}
	for i := range bridges {
		if !slices.Contains(bridges[i].Ports, port.UUID) {
			continue
		}
		if bridges[i].UUID != target.UUID {
			return false, fmt.Errorf("OVS port %q is already attached to bridge %q", port.Name, bridges[i].Name)
		}
		return true, nil
	}
	return false, nil
}

// DeleteVswitchPort detaches matching ports and removes their Interface and
// QoS rows in one transaction. Missing ports are treated as already deleted.
func DeleteVswitchPort(ctx context.Context, provider compat.TableProvider, name string) error {
	if provider == nil {
		return errors.New("ovsdb table provider is nil")
	}
	if name == "" {
		return errors.New("OVS port name is empty")
	}

	var ports []vswitch.Port
	if err := provider.Table(&vswitch.Port{}).Filter(ctx, func(port *vswitch.Port) bool {
		return port.Name == name
	}, &ports); err != nil {
		return fmt.Errorf("find OVS port %q: %w", name, err)
	}
	if len(ports) == 0 {
		return nil
	}

	var bridges []vswitch.Bridge
	if err := provider.Table(&vswitch.Bridge{}).List(ctx, &bridges); err != nil {
		return fmt.Errorf("list OVS bridges while deleting port %q: %w", name, err)
	}

	bridgeTable := provider.Table(&vswitch.Bridge{})
	portTable := provider.Table(&vswitch.Port{})
	interfaceTable := provider.Table(&vswitch.Interface{})
	qosTable := provider.Table(&vswitch.QoS{})
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
		for j := range bridges {
			bridge := &bridges[j]
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
	for uuid := range interfaceIDs {
		ops, err := interfaceTable.DeleteOps(&vswitch.Interface{UUID: uuid})
		if err != nil {
			return fmt.Errorf("delete OVS interface %q: %w", uuid, err)
		}
		operations = append(operations, ops...)
	}
	for uuid := range qosIDs {
		ops, err := qosTable.DeleteOps(&vswitch.QoS{UUID: uuid})
		if err != nil {
			return fmt.Errorf("delete OVS QoS %q: %w", uuid, err)
		}
		operations = append(operations, ops...)
	}
	return portTable.Transact(ctx, "vswitch-port-delete", operations...)
}
