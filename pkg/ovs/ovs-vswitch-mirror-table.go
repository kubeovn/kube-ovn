package ovs

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// mirrorTableRow deliberately contains only columns needed by the mirror
// reconciler. Keeping it separate from the generated model lets old OVS
// schemas omit newer columns such as Mirror.filter.
type mirrorTableRow struct {
	UUID          string   `ovsdb:"_uuid"`
	Name          string   `ovsdb:"name"`
	SelectAll     bool     `ovsdb:"select_all"`
	OutputPort    *string  `ovsdb:"output_port"`
	SelectDstPort []string `ovsdb:"select_dst_port"`
}

// EnsureVswitchMirror configures the named internal mirror output port and
// replaces the bridge's mirror set atomically.
func EnsureVswitchMirror(ctx context.Context, provider compat.TableProvider, portName string, selectAll bool) error {
	if provider == nil {
		return errors.New("ovsdb table provider is nil")
	}
	if portName == "" {
		return errors.New("mirror port name is empty")
	}
	iface := &vswitch.Interface{Name: portName, Type: "internal"}
	if err := EnsureVswitchPort(ctx, provider, VswitchPortConfig{
		BridgeName: "br-int",
		Port:       &vswitch.Port{Name: portName},
		Interface:  iface,
		InterfaceFields: []any{
			&iface.Type,
		},
	}); err != nil {
		return fmt.Errorf("ensure mirror output port %q: %w", portName, err)
	}

	bridge, err := findVswitchBridgeOptional(ctx, provider, "br-int")
	if err != nil {
		return err
	}
	if bridge == nil {
		return fmt.Errorf("OVS bridge %q not found", "br-int")
	}
	port, err := findVswitchPort(ctx, provider, portName)
	if err != nil {
		return err
	}
	if port == nil {
		return fmt.Errorf("OVS mirror port %q not found", portName)
	}

	optionalProvider, ok := provider.(compat.OptionalTableProvider)
	if !ok {
		return errors.New("OVS provider does not support optional tables")
	}
	mirrorTable := optionalProvider.OptionalTable(vswitch.MirrorTable, &mirrorTableRow{})
	var mirrors []mirrorTableRow
	if err := mirrorTable.List(ctx, &mirrors); err != nil {
		return fmt.Errorf("find OVS mirror %q: %w", util.MirrorDefaultName, err)
	}
	mirrors = slices.DeleteFunc(mirrors, func(row mirrorTableRow) bool {
		return row.Name != util.MirrorDefaultName
	})
	if len(mirrors) > 1 {
		return fmt.Errorf("expected at most one OVS mirror %q, found %d", util.MirrorDefaultName, len(mirrors))
	}

	bridgeTable := provider.Table(&vswitch.Bridge{})
	operations := make([]ovsdb.Operation, 0, 4)

	var mirror *mirrorTableRow
	if len(mirrors) == 0 {
		outputPort := new(string)
		*outputPort = port.UUID
		mirror = &mirrorTableRow{
			UUID:       ovsclient.NamedUUID(),
			Name:       util.MirrorDefaultName,
			SelectAll:  selectAll,
			OutputPort: outputPort,
		}
		createOps, err := mirrorTable.CreateOps(mirror)
		if err != nil {
			return fmt.Errorf("create OVS mirror operation: %w", err)
		}
		operations = append(operations, createOps...)
	} else {
		mirror = &mirrors[0]
		mirror.SelectAll = selectAll
		mirror.OutputPort = new(string)
		*mirror.OutputPort = port.UUID
		updateOps, err := mirrorTable.UpdateOps(mirror, mirror, &mirror.SelectAll, &mirror.OutputPort)
		if err != nil {
			return fmt.Errorf("update OVS mirror operation: %w", err)
		}
		operations = append(operations, updateOps...)
	}
	bridge.Mirrors = []string{mirror.UUID}
	attachOps, err := bridgeTable.UpdateOps(bridge, bridge, &bridge.Mirrors)
	if err != nil {
		return fmt.Errorf("attach OVS mirror to bridge: %w", err)
	}
	operations = append(operations, attachOps...)
	return bridgeTable.Transact(ctx, "vswitch-mirror-ensure", operations...)
}

// ConfigVswitchInterfaceMirror adds or removes an OVS port from the default
// mirror's select_dst_port set. The Mirror table is optional in older OVS
// schemas, so this path deliberately uses OptionalTableProvider instead of
// assuming the table is part of the monitored client model.
func ConfigVswitchInterfaceMirror(ctx context.Context, provider compat.TableProvider, open bool, ifaceID string) error {
	if provider == nil {
		return errors.New("ovsdb table provider is nil")
	}
	if ifaceID == "" {
		return errors.New("OVS interface ID is empty")
	}

	optionalProvider, ok := provider.(compat.OptionalTableProvider)
	if !ok {
		return errors.New("OVS provider does not support optional tables")
	}

	var interfaces []vswitch.Interface
	if err := provider.Table(&vswitch.Interface{}).Filter(ctx, func(row *vswitch.Interface) bool {
		return row.ExternalIDs["iface-id"] == ifaceID
	}, &interfaces); err != nil {
		return fmt.Errorf("find OVS interface for %q: %w", ifaceID, err)
	}

	var mirrors []mirrorTableRow
	mirrorTable := optionalProvider.OptionalTable(vswitch.MirrorTable, &mirrorTableRow{})
	if err := mirrorTable.List(ctx, &mirrors); err != nil {
		return fmt.Errorf("list OVS mirror table: %w", err)
	}
	mirrors = slices.DeleteFunc(mirrors, func(row mirrorTableRow) bool {
		return row.Name != util.MirrorDefaultName
	})
	if len(mirrors) == 0 {
		return fmt.Errorf("find mirror failed, mirror name=%s", util.MirrorDefaultName)
	}
	if len(mirrors) > 1 {
		return fmt.Errorf("repeated mirror data, mirror name=%s", util.MirrorDefaultName)
	}
	mirror := mirrors[0]

	operations := make([]ovsdb.Operation, 0, len(interfaces))
	for _, iface := range interfaces {
		if iface.UUID == "" {
			return fmt.Errorf("OVS interface %q has no UUID", ifaceID)
		}
		var ports []vswitch.Port
		if err := provider.Table(&vswitch.Port{}).Filter(ctx, func(row *vswitch.Port) bool {
			return slices.Contains(row.Interfaces, iface.UUID)
		}, &ports); err != nil {
			return fmt.Errorf("find OVS port for interface %q: %w", ifaceID, err)
		}
		if len(ports) != 1 {
			return fmt.Errorf("find port failed, interface=%s, count=%d", ifaceID, len(ports))
		}

		desired := slices.Clone(mirror.SelectDstPort)
		if open {
			if slices.Contains(desired, ports[0].UUID) {
				continue
			}
			desired = append(desired, ports[0].UUID)
		} else {
			desired = slices.DeleteFunc(desired, func(uuid string) bool {
				return uuid == ports[0].UUID
			})
			if slices.Equal(desired, mirror.SelectDstPort) {
				continue
			}
		}

		selector := &mirrorTableRow{UUID: mirror.UUID}
		update := &mirrorTableRow{UUID: mirror.UUID, SelectDstPort: desired}
		updateOps, err := mirrorTable.UpdateOps(selector, update, &update.SelectDstPort)
		if err != nil {
			return fmt.Errorf("update mirror %q operation: %w", util.MirrorDefaultName, err)
		}
		operations = append(operations, updateOps...)
		mirror.SelectDstPort = desired
	}
	if len(operations) == 0 {
		return nil
	}
	return mirrorTable.Transact(ctx, "vswitch-mirror-interface-config", operations...)
}
