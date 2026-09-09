package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func vswitchProvider(providers ...table.TableProvider) (table.TableProvider, error) {
	if len(providers) == 0 || providers[0] == nil {
		return nil, errors.New("vswitch table provider is nil")
	}
	return providers[0], nil
}

// Bridges returns bridges created by Kube-OVN.
func kubeOvnVswitchBridges(ctx context.Context, provider table.TableProvider) ([]vswitch.Bridge, error) {
	return table.Filter[vswitch.Bridge](ctx, provider, &vswitch.Bridge{}, func(row *vswitch.Bridge) bool {
		return row.ExternalIDs[ExternalIDVendor] == util.CniTypeName
	})
}

func Bridges(providers ...table.TableProvider) ([]string, error) {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return nil, err
	}
	rows, err := kubeOvnVswitchBridges(context.Background(), provider)
	if err != nil {
		return nil, fmt.Errorf("list Kube-OVN OVS bridges: %w", err)
	}
	return fieldSlice(rows, func(row *vswitch.Bridge) string { return row.Name }), nil
}

// BridgeExists checks whether the bridge already exists
func BridgeExists(name string, providers ...table.TableProvider) (bool, error) {
	bridges, err := Bridges(providers...)
	if err != nil {
		klog.Error(err)
		return false, err
	}
	return slices.Contains(bridges, name), nil
}

// PortExists checks whether the port already exists

func PortExists(name string, providers ...table.TableProvider) (bool, error) {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return false, err
	}
	port, err := findVswitchPort(context.Background(), provider, name)
	if err != nil {
		return false, err
	}
	return port != nil, nil
}

func GetQosList(podName, podNamespace, ifaceID string, providers ...table.TableProvider) ([]string, error) {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return nil, err
	}
	rows, err := vswitchRowsByOwner(context.Background(), provider, &vswitch.QoS{}, podName, podNamespace, ifaceID, func(row *vswitch.QoS) map[string]string { return row.ExternalIDs })
	if err != nil {
		return nil, fmt.Errorf("list QoS rows: %w", err)
	}
	return fieldSlice(rows, func(row *vswitch.QoS) string { return row.UUID }), nil
}

// ClearPodBandwidth remove qos related to this pod.
func ClearPodBandwidth(podName, podNamespace, ifaceID string, providers ...table.TableProvider) error {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return err
	}
	return clearPodBandwidthTable(provider, podName, podNamespace, ifaceID)
}

var lastInterfacePodMap map[string]string

func ListInterfacePodMap(providers ...table.TableProvider) (map[string]string, error) {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return nil, err
	}
	var rows []vswitch.Interface
	if err := provider.Table(&vswitch.Interface{}).Filter(context.Background(), func(row *vswitch.Interface) bool {
		return row.ExternalIDs["pod_name"] != "" && row.ExternalIDs["pod_namespace"] != "" &&
			(row.LinkState == nil || *row.LinkState != vswitch.InterfaceLinkStateUp)
	}, &rows); err != nil {
		return nil, fmt.Errorf("list OVS interfaces: %w", err)
	}
	result := make(map[string]string, len(rows))
	for _, row := range rows {
		errText := ""
		if row.Error != nil {
			errText = *row.Error
		}
		result[row.Name] = fmt.Sprintf("%s/%s/%s", row.ExternalIDs["pod_namespace"], row.ExternalIDs["pod_name"], errText)
	}
	if !maps.Equal(result, lastInterfacePodMap) {
		klog.Infof("interface pod map: %v", result)
		lastInterfacePodMap = maps.Clone(result)
	}
	return result, nil
}

func CleanInterface(name string, providers ...table.TableProvider) error {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return err
	}
	return DeleteVswitchPort(context.Background(), provider, name)
}

// Find and remove any existing OVS port with this iface-id. Pods can
// have multiple sandboxes if some are waiting for garbage collection,
// but only the latest one should have the iface-id set.
// See: https://github.com/ovn-org/ovn-kubernetes/pull/869
func CleanDuplicatePort(ifaceID, portName string, providers ...table.TableProvider) {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		klog.Error(err)
		return
	}
	var interfaces []vswitch.Interface
	if err := provider.Table(&vswitch.Interface{}).Filter(context.Background(), func(row *vswitch.Interface) bool {
		return row.ExternalIDs["iface-id"] == ifaceID && row.Name != portName
	}, &interfaces); err != nil {
		klog.Errorf("failed to list duplicate OVS interfaces for %s: %v", ifaceID, err)
		return
	}
	for i := range interfaces {
		iface := &interfaces[i]
		if err := provider.Table(&vswitch.Interface{}).Mutate(context.Background(), "interface-duplicate-cleanup", iface,
			model.Mutation{Field: &iface.ExternalIDs, Mutator: ovsdb.MutateOperationDelete, Value: map[string]string{"iface-id": ifaceID}}); err != nil {
			klog.Errorf("failed to clear stale OVS port %q iface-id %q: %v", iface.UUID, ifaceID, err)
		}
	}
}

// ValidatePortVendor returns true if the port's external_ids:vendor=kube-ovn
func ValidatePortVendor(port string, providers ...table.TableProvider) (bool, error) {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return false, err
	}
	var rows []vswitch.Port
	if err := provider.Table(&vswitch.Port{}).Filter(context.Background(), func(row *vswitch.Port) bool {
		return row.Name == port && row.ExternalIDs[ExternalIDVendor] == util.CniTypeName
	}, &rows); err != nil {
		return false, fmt.Errorf("validate OVS port %q: %w", port, err)
	}
	return len(rows) != 0, nil
}

func GetInterfacePodNs(iface string, providers ...table.TableProvider) (string, error) {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return "", err
	}
	rows, err := vswitchRowsByIfaceID(context.Background(), provider, &vswitch.Interface{}, iface, func(row *vswitch.Interface) map[string]string { return row.ExternalIDs })
	if err != nil {
		return "", fmt.Errorf("find OVS interface %q: %w", iface, err)
	}
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].ExternalIDs["pod_netns"], nil
}

// config mirror for interface by pod annotations and install param
func ConfigInterfaceMirror(globalMirror bool, open, iface string, providers ...table.TableProvider) error {
	if globalMirror {
		return nil
	}
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return err
	}
	return ConfigVswitchInterfaceMirror(context.Background(), provider, open == "true", iface)
}

// remove qos related to this port.
func ClearPortQosBinding(ifaceID string, providers ...table.TableProvider) error {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return err
	}
	return clearPortQosBindingTable(provider, ifaceID)
}

func ListExternalIDs(table string, providers ...table.TableProvider) (map[string]string, error) {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	switch strings.ToLower(table) {
	case "interface":
		return vswitchIfaceIDMap(ctx, provider, &vswitch.Interface{}, func(row *vswitch.Interface) map[string]string { return row.ExternalIDs }, func(row *vswitch.Interface) string { return row.UUID })
	case "port":
		return vswitchIfaceIDMap(ctx, provider, &vswitch.Port{}, func(row *vswitch.Port) map[string]string { return row.ExternalIDs }, func(row *vswitch.Port) string { return row.UUID })
	case "qos":
		return vswitchIfaceIDMap(ctx, provider, &vswitch.QoS{}, func(row *vswitch.QoS) map[string]string { return row.ExternalIDs }, func(row *vswitch.QoS) string { return row.UUID })
	case "queue":
		return vswitchIfaceIDMap(ctx, provider, &vswitch.Queue{}, func(row *vswitch.Queue) map[string]string { return row.ExternalIDs }, func(row *vswitch.Queue) string { return row.UUID })
	default:
		return nil, fmt.Errorf("unsupported OVS table %q", table)
	}
}

func ListQosQueueIDs(providers ...table.TableProvider) (map[string]string, error) {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return nil, err
	}
	var rows []vswitch.QoS
	if err := provider.Table(&vswitch.QoS{}).Filter(context.Background(), func(row *vswitch.QoS) bool {
		return len(row.Queues) > 0
	}, &rows); err != nil {
		return nil, fmt.Errorf("list QoS queue IDs: %w", err)
	}
	result := make(map[string]string, len(rows))
	for _, row := range rows {
		result[row.UUID] = row.Queues[0]
	}
	return result, nil
}
