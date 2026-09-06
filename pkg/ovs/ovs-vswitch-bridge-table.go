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

// VswitchBridgeConfig describes the schema fields managed by an OVS bridge
// reconciler. Map fields are merged with existing values so unrelated keys are
// preserved.
type VswitchBridgeConfig struct {
	Name        string
	ExternalIDs map[string]string
	OtherConfig map[string]string
}

// EnsureVswitchBridge creates or updates an OVS bridge and keeps the root
// Open_vSwitch.bridges reference consistent in the same transaction.
func EnsureVswitchBridge(ctx context.Context, provider compat.TableProvider, config VswitchBridgeConfig) error {
	if provider == nil {
		return errors.New("ovsdb table provider is nil")
	}
	if config.Name == "" {
		return errors.New("OVS bridge name is empty")
	}

	bridge, err := findVswitchBridgeOptional(ctx, provider, config.Name)
	if err != nil {
		return err
	}
	roots, err := listVswitchOpenVSwitch(ctx, provider)
	if err != nil {
		return err
	}
	if len(roots) != 1 {
		return fmt.Errorf("expected one Open_vSwitch row, found %d", len(roots))
	}
	root := &roots[0]

	bridgeTable := provider.Table(&vswitch.Bridge{})
	rootTable := provider.Table(&vswitch.OpenvSwitch{})
	operations := make([]ovsdb.Operation, 0, 4)
	if bridge == nil {
		bridge = &vswitch.Bridge{
			UUID:        ovsclient.NamedUUID(),
			Name:        config.Name,
			ExternalIDs: maps.Clone(config.ExternalIDs),
			OtherConfig: maps.Clone(config.OtherConfig),
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

	if !slices.Contains(root.Bridges, bridge.UUID) {
		mutateOps, err := rootTable.MutateOps(root, model.Mutation{
			Field: &root.Bridges, Value: []string{bridge.UUID}, Mutator: ovsdb.MutateOperationInsert,
		})
		if err != nil {
			return fmt.Errorf("attach OVS bridge %q to Open_vSwitch: %w", config.Name, err)
		}
		operations = append(operations, mutateOps...)
	}

	return bridgeTable.Transact(ctx, "vswitch-bridge-ensure", operations...)
}

// DeleteVswitchBridge removes an OVS bridge and the Port, Interface, and QoS
// rows owned by it. The caller should first perform any domain-specific state
// restoration required for the attached kernel links.
func DeleteVswitchBridge(ctx context.Context, provider compat.TableProvider, name string) error {
	if provider == nil {
		return errors.New("ovsdb table provider is nil")
	}
	if name == "" {
		return errors.New("OVS bridge name is empty")
	}

	bridge, err := findVswitchBridgeOptional(ctx, provider, name)
	if err != nil {
		return err
	}
	if bridge == nil {
		return nil
	}
	roots, err := listVswitchOpenVSwitch(ctx, provider)
	if err != nil {
		return err
	}
	if len(roots) != 1 {
		return fmt.Errorf("expected one Open_vSwitch row, found %d", len(roots))
	}
	root := &roots[0]

	var ports []vswitch.Port
	if err := provider.Table(&vswitch.Port{}).Filter(ctx, func(port *vswitch.Port) bool {
		return slices.Contains(bridge.Ports, port.UUID)
	}, &ports); err != nil {
		return fmt.Errorf("list ports for OVS bridge %q: %w", name, err)
	}
	var interfaces []vswitch.Interface
	if err := provider.Table(&vswitch.Interface{}).List(ctx, &interfaces); err != nil {
		return fmt.Errorf("list interfaces for OVS bridge %q: %w", name, err)
	}
	interfaceIDs := make(map[string]struct{})
	qosIDs := make(map[string]struct{})
	for _, port := range ports {
		for _, interfaceID := range port.Interfaces {
			interfaceIDs[interfaceID] = struct{}{}
		}
		if port.QOS != nil {
			qosIDs[*port.QOS] = struct{}{}
		}
	}

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
	for _, port := range ports {
		portOps, err := portTable.DeleteOps(&vswitch.Port{UUID: port.UUID})
		if err != nil {
			return fmt.Errorf("delete OVS port %q operation: %w", port.Name, err)
		}
		operations = append(operations, portOps...)
	}
	for interfaceID := range interfaceIDs {
		interfaceOps, err := interfaceTable.DeleteOps(&vswitch.Interface{UUID: interfaceID})
		if err != nil {
			return fmt.Errorf("delete OVS interface %q operation: %w", interfaceID, err)
		}
		operations = append(operations, interfaceOps...)
	}
	for qosID := range qosIDs {
		qosOps, err := qosTable.DeleteOps(&vswitch.QoS{UUID: qosID})
		if err != nil {
			return fmt.Errorf("delete OVS QoS %q operation: %w", qosID, err)
		}
		operations = append(operations, qosOps...)
	}

	return bridgeTable.Transact(ctx, "vswitch-bridge-delete", operations...)
}

func listVswitchOpenVSwitch(ctx context.Context, provider compat.TableProvider) ([]vswitch.OpenvSwitch, error) {
	var roots []vswitch.OpenvSwitch
	if err := provider.Table(&vswitch.OpenvSwitch{}).List(ctx, &roots); err != nil {
		return nil, fmt.Errorf("list Open_vSwitch rows: %w", err)
	}
	return roots, nil
}

func findVswitchBridgeOptional(ctx context.Context, provider compat.TableProvider, name string) (*vswitch.Bridge, error) {
	var rows []vswitch.Bridge
	if err := provider.Table(&vswitch.Bridge{}).Filter(ctx, func(row *vswitch.Bridge) bool {
		return row.Name == name
	}, &rows); err != nil {
		return nil, fmt.Errorf("find OVS bridge %q: %w", name, err)
	}
	if len(rows) > 1 {
		return nil, fmt.Errorf("expected at most one OVS bridge %q, found %d", name, len(rows))
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
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
