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
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

// VswitchBridgeConfig describes the schema fields managed by an OVS bridge
// reconciler. Map fields are merged with existing values so unrelated keys are
// preserved.
type VswitchBridgeConfig struct {
	Name        string
	ExternalIDs map[string]string
	OtherConfig map[string]string
}

// EnsureVswitchBridge creates or updates an OVS bridge, its local Port and
// internal Interface, and the root Open_vSwitch.bridges reference in one
// transaction.
func EnsureVswitchBridge(ctx context.Context, provider table.TableProvider, config VswitchBridgeConfig) error {
	if err := requireVswitchTable(provider, config.Name, "OVS bridge name is empty"); err != nil {
		return err
	}

	bridge, err := findVswitchBridgeOptional(ctx, provider, config.Name)
	if err != nil {
		return err
	}
	root, err := uniqueOpenVSwitch(ctx, provider)
	if err != nil {
		return err
	}
	port, operations, err := planVswitchBridgeLocalPort(ctx, provider, config.Name)
	if err != nil {
		return err
	}

	bridgeTable := provider.Table(&vswitch.Bridge{})
	rootTable := provider.Table(&vswitch.OpenvSwitch{})
	bridgeExists := bridge != nil
	if bridge == nil {
		bridge = &vswitch.Bridge{
			UUID:        ovsclient.NamedUUID(),
			Name:        config.Name,
			ExternalIDs: maps.Clone(config.ExternalIDs),
			OtherConfig: maps.Clone(config.OtherConfig),
			Ports:       []string{port.UUID},
		}
		createOps, err := bridgeTable.CreateOps(bridge)
		if err != nil {
			return fmt.Errorf("create OVS bridge %q operation: %w", config.Name, err)
		}
		operations = append(operations, createOps...)
	} else {
		bridge.ExternalIDs = mergeVswitchMap(bridge.ExternalIDs, config.ExternalIDs)
		bridge.OtherConfig = mergeVswitchMap(bridge.OtherConfig, config.OtherConfig)
		updateOps, err := bridgeTable.UpdateOps(bridge, bridge, &bridge.ExternalIDs, &bridge.OtherConfig)
		if err != nil {
			return fmt.Errorf("update OVS bridge %q operation: %w", config.Name, err)
		}
		operations = append(operations, updateOps...)
	}
	attached, err := validateVswitchPortBridge(ctx, provider, port, bridge)
	if err != nil {
		return err
	}
	if bridgeExists && !attached {
		mutateOps, err := bridgeTable.MutateOps(bridge, model.Mutation{
			Field: &bridge.Ports, Value: []string{port.UUID}, Mutator: ovsdb.MutateOperationInsert,
		})
		if err != nil {
			return fmt.Errorf("attach local OVS port %q to bridge: %w", config.Name, err)
		}
		operations = append(operations, mutateOps...)
	}

	if !slices.Contains(root.Bridges, bridge.UUID) {
		mutateOps, err := rootTable.MutateOps(root, model.Mutation{
			Field: &root.Bridges, Value: []string{bridge.UUID}, Mutator: ovsdb.MutateOperationInsert,
		})
		if err != nil {
			return fmt.Errorf("attach OVS bridge %q to Open_vSwitch: %w", config.Name, err)
		}
		operations = append(operations, mutateOps...)
	}

	if err := transactTable(ctx, bridgeTable, "vswitch-bridge-ensure", operations); err != nil {
		return err
	}
	return waitForVswitchBridge(ctx, provider, config.Name, root.UUID)
}

func planVswitchBridgeLocalPort(ctx context.Context, provider table.TableProvider, name string) (*vswitch.Port, []ovsdb.Operation, error) {
	port, err := findVswitchPort(ctx, provider, name)
	if err != nil {
		return nil, nil, err
	}
	iface, err := findVswitchInterface(ctx, provider, name)
	if err != nil {
		return nil, nil, err
	}

	interfaceTable := provider.Table(&vswitch.Interface{})
	portTable := provider.Table(&vswitch.Port{})
	operations := make([]ovsdb.Operation, 0, 3)
	if iface == nil {
		iface = &vswitch.Interface{UUID: ovsclient.NamedUUID(), Name: name, Type: "internal"}
		createOps, err := interfaceTable.CreateOps(iface)
		if err != nil {
			return nil, nil, fmt.Errorf("create local OVS interface %q operation: %w", name, err)
		}
		operations = append(operations, createOps...)
	} else if iface.Type != "internal" {
		iface.Type = "internal"
		updateOps, err := interfaceTable.UpdateOps(iface, iface, &iface.Type)
		if err != nil {
			return nil, nil, fmt.Errorf("update local OVS interface %q operation: %w", name, err)
		}
		operations = append(operations, updateOps...)
	}

	if port == nil {
		port = &vswitch.Port{UUID: ovsclient.NamedUUID(), Name: name, Interfaces: []string{iface.UUID}}
		createOps, err := portTable.CreateOps(port)
		if err != nil {
			return nil, nil, fmt.Errorf("create local OVS port %q operation: %w", name, err)
		}
		operations = append(operations, createOps...)
	} else if !slices.Contains(port.Interfaces, iface.UUID) {
		if len(port.Interfaces) != 0 {
			return nil, nil, fmt.Errorf("local OVS port %q does not reference its interface", name)
		}
		mutateOps, err := portTable.MutateOps(port, model.Mutation{
			Field: &port.Interfaces, Value: []string{iface.UUID}, Mutator: ovsdb.MutateOperationInsert,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("attach local OVS interface %q to port: %w", name, err)
		}
		operations = append(operations, mutateOps...)
	}
	return port, operations, nil
}

// DeleteVswitchBridge removes an OVS bridge and the Port, Interface, and QoS
// rows owned by it. The caller should first perform any domain-specific state
// restoration required for the attached kernel links.
func DeleteVswitchBridge(ctx context.Context, provider table.TableProvider, name string) error {
	if err := requireVswitchTable(provider, name, "OVS bridge name is empty"); err != nil {
		return err
	}

	bridge, err := findVswitchBridgeOptional(ctx, provider, name)
	if err != nil {
		return err
	}
	if bridge == nil {
		return nil
	}
	root, err := uniqueOpenVSwitch(ctx, provider)
	if err != nil {
		return err
	}

	var ports []vswitch.Port
	if err := provider.Table(&vswitch.Port{}).Filter(ctx, func(port *vswitch.Port) bool {
		return slices.Contains(bridge.Ports, port.UUID)
	}, &ports); err != nil {
		return fmt.Errorf("list ports for OVS bridge %q: %w", name, err)
	}
	interfaceIDs, qosIDs := collectVswitchPortRefs(ports)

	bridgeTable := provider.Table(&vswitch.Bridge{})
	rootTable := provider.Table(&vswitch.OpenvSwitch{})
	portTable := provider.Table(&vswitch.Port{})
	interfaceTable := provider.Table(&vswitch.Interface{})
	qosTable := provider.Table(&vswitch.QoS{})
	operations := make([]ovsdb.Operation, 0, 4+len(ports)+len(interfaceIDs)+len(qosIDs))
	if len(bridge.Ports) != 0 {
		mutateOps, err := bridgeTable.MutateOps(bridge, model.Mutation{
			Field: &bridge.Ports, Value: bridge.Ports, Mutator: ovsdb.MutateOperationDelete,
		})
		if err != nil {
			return fmt.Errorf("detach ports from OVS bridge %q: %w", name, err)
		}
		operations = append(operations, mutateOps...)
	}
	if slices.Contains(root.Bridges, bridge.UUID) {
		mutateOps, err := rootTable.MutateOps(root, model.Mutation{
			Field: &root.Bridges, Value: []string{bridge.UUID}, Mutator: ovsdb.MutateOperationDelete,
		})
		if err != nil {
			return fmt.Errorf("detach OVS bridge %q from Open_vSwitch: %w", name, err)
		}
		operations = append(operations, mutateOps...)
	}
	bridgeOps, err := bridgeTable.DeleteOps(&vswitch.Bridge{UUID: bridge.UUID})
	if err != nil {
		return fmt.Errorf("delete OVS bridge %q operation: %w", name, err)
	}
	operations = append(operations, bridgeOps...)
	operations, err = appendVswitchPortDeletes(portTable, operations, ports, "delete OVS port %q operation: %w")
	if err != nil {
		return err
	}
	operations, err = appendVswitchUUIDDeletes(interfaceTable, operations, interfaceIDs, func(uuid string) *vswitch.Interface {
		return &vswitch.Interface{UUID: uuid}
	}, "delete OVS interface %q operation: %w")
	if err != nil {
		return err
	}
	operations, err = appendVswitchUUIDDeletes(qosTable, operations, qosIDs, func(uuid string) *vswitch.QoS {
		return &vswitch.QoS{UUID: uuid}
	}, "delete OVS QoS %q operation: %w")
	if err != nil {
		return err
	}

	return transactTable(ctx, bridgeTable, "vswitch-bridge-delete", operations)
}

func listVswitchOpenVSwitch(ctx context.Context, provider table.TableProvider) ([]vswitch.OpenvSwitch, error) {
	var roots []vswitch.OpenvSwitch
	if err := provider.Table(&vswitch.OpenvSwitch{}).List(ctx, &roots); err != nil {
		return nil, fmt.Errorf("list Open_vSwitch rows: %w", err)
	}
	return roots, nil
}

func findVswitchBridgeOptional(ctx context.Context, provider table.TableProvider, name string) (*vswitch.Bridge, error) {
	return findNamedRow(ctx, provider, &vswitch.Bridge{}, name, "bridge", false, func(row *vswitch.Bridge) string { return row.Name })
}

func requireVswitchTable(provider table.TableProvider, name, emptyErr string) error {
	if provider == nil {
		return errors.New("ovsdb table provider is nil")
	}
	if name == "" {
		return errors.New(emptyErr)
	}
	return nil
}

func uniqueOpenVSwitch(ctx context.Context, provider table.TableProvider) (*vswitch.OpenvSwitch, error) {
	roots, err := listVswitchOpenVSwitch(ctx, provider)
	if err != nil {
		return nil, err
	}
	return table.Unique(roots, false,
		fmt.Errorf("expected one Open_vSwitch row, found %d", len(roots)),
		fmt.Errorf("expected one Open_vSwitch row, found %d", len(roots)),
	)
}

func waitForVswitchRows[T any](ctx context.Context, provider table.TableProvider, prototype model.Model, pred func(*T) bool, wrap, name string) ([]T, error) {
	var rows []T
	if err := table.WaitForRows(ctx, provider, prototype, pred, &rows); err != nil {
		return nil, fmt.Errorf(wrap, name, err)
	}
	return rows, nil
}

func waitForVswitchBridge(ctx context.Context, provider table.TableProvider, name, rootUUID string) error {
	interfaces, err := waitForVswitchRows(ctx, provider, &vswitch.Interface{}, func(row *vswitch.Interface) bool {
		return row.Name == name && row.Type == "internal"
	}, "wait for local OVS interface %q cache update: %w", name)
	if err != nil {
		return err
	}
	interfaceUUID := interfaces[0].UUID

	ports, err := waitForVswitchRows(ctx, provider, &vswitch.Port{}, func(row *vswitch.Port) bool {
		return row.Name == name && slices.Contains(row.Interfaces, interfaceUUID)
	}, "wait for local OVS port %q cache update: %w", name)
	if err != nil {
		return err
	}
	portUUID := ports[0].UUID

	bridges, err := waitForVswitchRows(ctx, provider, &vswitch.Bridge{}, func(row *vswitch.Bridge) bool {
		return row.Name == name && slices.Contains(row.Ports, portUUID)
	}, "wait for OVS bridge %q cache update: %w", name)
	if err != nil {
		return err
	}
	bridgeUUID := bridges[0].UUID

	_, err = waitForVswitchRows(ctx, provider, &vswitch.OpenvSwitch{}, func(row *vswitch.OpenvSwitch) bool {
		return row.UUID == rootUUID && slices.Contains(row.Bridges, bridgeUUID)
	}, "wait for OVS bridge %q root attachment cache update: %w", name)
	return err
}

func mergeVswitchMap(current, desired map[string]string) map[string]string {
	if desired == nil {
		return current
	}
	merged := maps.Clone(current)
	if merged == nil {
		merged = make(map[string]string, len(desired))
	}
	maps.Copy(merged, desired)
	return merged
}

func collectVswitchPortRefs(ports []vswitch.Port) (map[string]struct{}, map[string]struct{}) {
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
	}
	return interfaceIDs, qosIDs
}

func appendVswitchUUIDDeletes[T any](table table.TableHandle, ops []ovsdb.Operation, uuids map[string]struct{}, makeRow func(string) *T, errFmt string) ([]ovsdb.Operation, error) {
	for uuid := range uuids {
		deleteOps, err := table.DeleteOps(makeRow(uuid))
		if err != nil {
			return nil, fmt.Errorf(errFmt, uuid, err)
		}
		ops = append(ops, deleteOps...)
	}
	return ops, nil
}

func appendVswitchPortDeletes(table table.TableHandle, ops []ovsdb.Operation, ports []vswitch.Port, errFmt string) ([]ovsdb.Operation, error) {
	for i := range ports {
		deleteOps, err := table.DeleteOps(&vswitch.Port{UUID: ports[i].UUID})
		if err != nil {
			return nil, fmt.Errorf(errFmt, ports[i].Name, err)
		}
		ops = append(ops, deleteOps...)
	}
	return ops, nil
}

func vswitchOwnerMatch(externalIDs map[string]string, podName, podNamespace, ifaceID string) bool {
	if ifaceID != "" {
		return externalIDs["iface-id"] == ifaceID
	}
	return externalIDs["pod"] == podNamespace+"/"+podName
}

func vswitchRowsByIfaceID[T any](ctx context.Context, provider table.TableProvider, prototype model.Model, iface string, idsOf func(*T) map[string]string) ([]T, error) {
	return table.Filter[T](ctx, provider, prototype, func(row *T) bool {
		return idsOf(row)["iface-id"] == iface
	})
}

func vswitchRowsByUUID[T any](ctx context.Context, provider table.TableProvider, prototype model.Model, uuid string, uuidOf func(*T) string) ([]T, error) {
	return table.Filter[T](ctx, provider, prototype, func(row *T) bool {
		return uuidOf(row) == uuid
	})
}

func vswitchRowsByOwner[T any](ctx context.Context, provider table.TableProvider, prototype model.Model, podName, podNamespace, ifaceID string, idsOf func(*T) map[string]string) ([]T, error) {
	return table.Filter[T](ctx, provider, prototype, func(row *T) bool {
		return vswitchOwnerMatch(idsOf(row), podName, podNamespace, ifaceID)
	})
}

func vswitchIfaceIDMap[T any](ctx context.Context, provider table.TableProvider, prototype model.Model, idsOf func(*T) map[string]string, uuidOf func(*T) string) (map[string]string, error) {
	rows, err := table.Filter[T](ctx, provider, prototype, func(row *T) bool {
		return idsOf(row)["iface-id"] != ""
	})
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(rows))
	for i := range rows {
		result[idsOf(&rows[i])["iface-id"]] = uuidOf(&rows[i])
	}
	return result, nil
}

func deleteUnusedVswitchRows[T any](ctx context.Context, table table.TableHandle, method string, rows []T, used map[string]struct{}, uuidOf func(*T) string, errFmt string) error {
	ops := make([]ovsdb.Operation, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		uuid := uuidOf(row)
		if _, ok := used[uuid]; ok {
			continue
		}
		deleteOps, err := table.DeleteOps(row)
		if err != nil {
			return fmt.Errorf(errFmt, uuid, err)
		}
		ops = append(ops, deleteOps...)
	}
	return transactTable(ctx, table, method, ops)
}
