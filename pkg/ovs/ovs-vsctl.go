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

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func vswitchProvider(providers ...compat.TableProvider) (compat.TableProvider, error) {
	if len(providers) == 0 || providers[0] == nil {
		return nil, errors.New("vswitch table provider is nil")
	}
	return providers[0], nil
}

// Bridges returns bridges created by Kube-OVN.
func Bridges(providers ...compat.TableProvider) ([]string, error) {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return nil, err
	}
	var rows []vswitch.Bridge
	if err := provider.Table(&vswitch.Bridge{}).Filter(context.Background(), func(row *vswitch.Bridge) bool {
		return row.ExternalIDs[ExternalIDVendor] == util.CniTypeName
	}, &rows); err != nil {
		return nil, fmt.Errorf("list Kube-OVN OVS bridges: %w", err)
	}
	bridges := make([]string, 0, len(rows))
	for _, row := range rows {
		bridges = append(bridges, row.Name)
	}
	return bridges, nil
}

// BridgeExists checks whether the bridge already exists
func BridgeExists(name string, providers ...compat.TableProvider) (bool, error) {
	bridges, err := Bridges(providers...)
	if err != nil {
		klog.Error(err)
		return false, err
	}
	return slices.Contains(bridges, name), nil
}

// PortExists checks whether the port already exists

func PortExists(name string, providers ...compat.TableProvider) (bool, error) {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return false, err
	}
	var rows []vswitch.Port
	if err := provider.Table(&vswitch.Port{}).Filter(context.Background(), func(row *vswitch.Port) bool {
		return row.Name == name
	}, &rows); err != nil {
		return false, fmt.Errorf("find OVS port %q: %w", name, err)
	}
	return len(rows) != 0, nil
}

func GetQosList(podName, podNamespace, ifaceID string, providers ...compat.TableProvider) ([]string, error) {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return nil, err
	}
	var rows []vswitch.QoS
	if err := provider.Table(&vswitch.QoS{}).Filter(context.Background(), func(row *vswitch.QoS) bool {
		if ifaceID != "" {
			return row.ExternalIDs["iface-id"] == ifaceID
		}
		return row.ExternalIDs["pod"] == podNamespace+"/"+podName
	}, &rows); err != nil {
		return nil, fmt.Errorf("list QoS rows: %w", err)
	}
	qosIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		qosIDs = append(qosIDs, row.UUID)
	}
	return qosIDs, nil
}

// ClearPodBandwidth remove qos related to this pod.
func ClearPodBandwidth(podName, podNamespace, ifaceID string, providers ...compat.TableProvider) error {
	if len(providers) == 0 || providers[0] == nil {
		return errors.New("vswitch table provider is nil")
	}
	return clearPodBandwidthTable(providers[0], podName, podNamespace, ifaceID)
}

var lastInterfacePodMap map[string]string

func ListInterfacePodMap(providers ...compat.TableProvider) (map[string]string, error) {
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

func CleanInterface(name string, providers ...compat.TableProvider) error {
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
func CleanDuplicatePort(ifaceID, portName string, providers ...compat.TableProvider) {
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
func ValidatePortVendor(port string, providers ...compat.TableProvider) (bool, error) {
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

func GetInterfacePodNs(iface string, providers ...compat.TableProvider) (string, error) {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return "", err
	}
	var rows []vswitch.Interface
	if err := provider.Table(&vswitch.Interface{}).Filter(context.Background(), func(row *vswitch.Interface) bool {
		return row.ExternalIDs["iface-id"] == iface
	}, &rows); err != nil {
		return "", fmt.Errorf("find OVS interface %q: %w", iface, err)
	}
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].ExternalIDs["pod_netns"], nil
}

// config mirror for interface by pod annotations and install param
func ConfigInterfaceMirror(globalMirror bool, open, iface string, providers ...compat.TableProvider) error {
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
func ClearPortQosBinding(ifaceID string, providers ...compat.TableProvider) error {
	if len(providers) == 0 || providers[0] == nil {
		return errors.New("vswitch table provider is nil")
	}
	return clearPortQosBindingTable(providers[0], ifaceID)
}

func ListExternalIDs(table string, providers ...compat.TableProvider) (map[string]string, error) {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	ctx := context.Background()
	switch strings.ToLower(table) {
	case "interface":
		var rows []vswitch.Interface
		if err := provider.Table(&vswitch.Interface{}).Filter(ctx, func(row *vswitch.Interface) bool { return row.ExternalIDs["iface-id"] != "" }, &rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			result[row.ExternalIDs["iface-id"]] = row.UUID
		}
	case "port":
		var rows []vswitch.Port
		if err := provider.Table(&vswitch.Port{}).Filter(ctx, func(row *vswitch.Port) bool { return row.ExternalIDs["iface-id"] != "" }, &rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			result[row.ExternalIDs["iface-id"]] = row.UUID
		}
	case "qos":
		var rows []vswitch.QoS
		if err := provider.Table(&vswitch.QoS{}).Filter(ctx, func(row *vswitch.QoS) bool { return row.ExternalIDs["iface-id"] != "" }, &rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			result[row.ExternalIDs["iface-id"]] = row.UUID
		}
	case "queue":
		var rows []vswitch.Queue
		if err := provider.Table(&vswitch.Queue{}).Filter(ctx, func(row *vswitch.Queue) bool { return row.ExternalIDs["iface-id"] != "" }, &rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			result[row.ExternalIDs["iface-id"]] = row.UUID
		}
	default:
		return nil, fmt.Errorf("unsupported OVS table %q", table)
	}
	return result, nil
}

func ListQosQueueIDs(providers ...compat.TableProvider) (map[string]string, error) {
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
