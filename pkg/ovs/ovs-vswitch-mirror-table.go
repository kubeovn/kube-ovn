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
	UUID       string  `ovsdb:"_uuid"`
	Name       string  `ovsdb:"name"`
	SelectAll  bool    `ovsdb:"select_all"`
	OutputPort *string `ovsdb:"output_port"`
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
