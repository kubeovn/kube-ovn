package ovs

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// setInterfaceBandwidthTable applies interface policing and HTB queue state
// through the monitored Open_vSwitch tables. All rows for one reconcile are
// submitted as a single transaction so queue creation and port binding cannot
// be observed independently.
func setInterfaceBandwidthTable(provider compat.TableProvider, podName, podNamespace, iface, ingress, egress, ingressBurst, egressBurst string) error {
	config, err := newInterfaceBandwidthConfig(podName, podNamespace, iface, ingress, egress, ingressBurst, egressBurst)
	if err != nil {
		return err
	}
	ctx := context.Background()
	interfaces, state, err := loadInterfaceBandwidthState(ctx, provider, iface)
	if err != nil {
		return err
	}
	if len(interfaces) == 0 {
		return nil
	}

	ops := make([]ovsdb.Operation, 0, len(interfaces)*4+3)
	for i := range interfaces {
		ifaceRow := &interfaces[i]
		interfaceOps, err := buildInterfacePolicingOps(provider, ifaceRow, config)
		if err != nil {
			return err
		}
		ops = append(ops, interfaceOps...)

		portRows, err := listPortsForInterface(provider, ifaceRow.UUID)
		if err != nil {
			return err
		}
		egressOps, err := buildHtbBandwidthOps(provider, portRows, state, config)
		if err != nil {
			return err
		}
		ops = append(ops, egressOps...)
	}

	if len(ops) == 0 {
		return nil
	}
	return provider.Table(&vswitch.Interface{}).Transact(ctx, "interface-bandwidth-update", ops...)
}

type interfaceBandwidthConfig struct {
	podName          string
	podNamespace     string
	iface            string
	ingressKPS       int64
	ingressBurstKbit int64
	egressBPS        int64
	egressBurstBytes int64
}

type interfaceBandwidthState struct {
	queues    []vswitch.Queue
	queueByID map[string]*vswitch.Queue
	qos       *vswitch.QoS
}

func newInterfaceBandwidthConfig(podName, podNamespace, iface, ingress, egress, ingressBurst, egressBurst string) (interfaceBandwidthConfig, error) {
	ingressKPS, err := parseAndScaleBandwidthRate(ingress, 1000)
	if err != nil {
		return interfaceBandwidthConfig{}, fmt.Errorf("invalid ingress bandwidth: %w", err)
	}
	egressBPS, err := parseAndScaleBandwidthRate(egress, 1000*1000)
	if err != nil {
		return interfaceBandwidthConfig{}, fmt.Errorf("invalid egress bandwidth: %w", err)
	}
	return interfaceBandwidthConfig{
		podName: podName, podNamespace: podNamespace, iface: iface,
		ingressKPS: ingressKPS, ingressBurstKbit: computeIngressPolicingBurstKbit(ingressKPS, ingressBurst),
		egressBPS: egressBPS, egressBurstBytes: computeHtbBurstBytes(egressBPS, egressBurst),
	}, nil
}

func loadInterfaceBandwidthState(ctx context.Context, provider compat.TableProvider, iface string) ([]vswitch.Interface, *interfaceBandwidthState, error) {
	var interfaces []vswitch.Interface
	if err := provider.Table(&vswitch.Interface{}).Filter(ctx, func(row *vswitch.Interface) bool {
		return row.ExternalIDs["iface-id"] == iface
	}, &interfaces); err != nil {
		return nil, nil, fmt.Errorf("list interfaces for %s: %w", iface, err)
	}
	state := &interfaceBandwidthState{}
	if len(interfaces) == 0 {
		return interfaces, state, nil
	}
	if err := provider.Table(&vswitch.Queue{}).Filter(ctx, func(row *vswitch.Queue) bool {
		return row.ExternalIDs["iface-id"] == iface
	}, &state.queues); err != nil {
		return nil, nil, fmt.Errorf("list queues for %s: %w", iface, err)
	}
	var qosRows []vswitch.QoS
	if err := provider.Table(&vswitch.QoS{}).Filter(ctx, func(row *vswitch.QoS) bool {
		return row.ExternalIDs["iface-id"] == iface
	}, &qosRows); err != nil {
		return nil, nil, fmt.Errorf("list QoS rows for %s: %w", iface, err)
	}
	state.queueByID = make(map[string]*vswitch.Queue, len(state.queues))
	for i := range state.queues {
		state.queueByID[state.queues[i].UUID] = &state.queues[i]
	}
	if len(qosRows) != 0 {
		state.qos = &qosRows[0]
	}
	return interfaces, state, nil
}

func buildInterfacePolicingOps(provider compat.TableProvider, iface *vswitch.Interface, config interfaceBandwidthConfig) ([]ovsdb.Operation, error) {
	update := &vswitch.Interface{
		UUID:                 iface.UUID,
		IngressPolicingRate:  int(config.ingressKPS),
		IngressPolicingBurst: int(config.ingressBurstKbit),
	}
	ops, err := provider.Table(&vswitch.Interface{}).UpdateOps(iface, update,
		&update.IngressPolicingRate, &update.IngressPolicingBurst)
	if err != nil {
		return nil, fmt.Errorf("build interface bandwidth update for %s: %w", iface.Name, err)
	}
	return ops, nil
}

func buildHtbBandwidthOps(provider compat.TableProvider, ports []vswitch.Port, state *interfaceBandwidthState, config interfaceBandwidthConfig) ([]ovsdb.Operation, error) {
	if config.egressBPS <= 0 {
		return buildHtbBandwidthRemovalOps(provider, ports, state, config.iface)
	}
	return buildHtbBandwidthEnsureOps(provider, ports, state, config)
}

func buildHtbBandwidthRemovalOps(provider compat.TableProvider, ports []vswitch.Port, state *interfaceBandwidthState, iface string) ([]ovsdb.Operation, error) {
	qos := state.qos
	if qos == nil || qos.Type != util.HtbQos {
		return nil, nil
	}
	queueID, ok := qos.Queues[0]
	if !ok || queueID == "" {
		return nil, nil
	}
	queue, ok := state.queueByID[queueID]
	if !ok {
		return nil, nil
	}
	deleteValues := make(map[string]string, 2)
	for _, key := range []string{"max-rate", "burst"} {
		if value, exists := queue.OtherConfig[key]; exists {
			deleteValues[key] = value
		}
	}
	if len(deleteValues) == 0 {
		return nil, nil
	}
	ops, err := provider.Table(&vswitch.Queue{}).MutateOps(queue,
		model.Mutation{Field: &queue.OtherConfig, Mutator: ovsdb.MutateOperationDelete, Value: deleteValues})
	if err != nil {
		return nil, fmt.Errorf("build HTB queue limit removal for %s: %w", iface, err)
	}
	if len(queue.OtherConfig) != len(deleteValues) {
		return ops, nil
	}
	for _, port := range ports {
		if port.QOS == nil || *port.QOS != qos.UUID {
			continue
		}
		update := &vswitch.Port{UUID: port.UUID}
		portOps, err := provider.Table(&vswitch.Port{}).UpdateOps(&port, update, &update.QOS)
		if err != nil {
			return nil, fmt.Errorf("build HTB QoS binding removal for port %s: %w", port.Name, err)
		}
		ops = append(ops, portOps...)
	}
	qosOps, err := provider.Table(&vswitch.QoS{}).DeleteOps(qos)
	if err != nil {
		return nil, fmt.Errorf("build HTB QoS delete for %s: %w", iface, err)
	}
	ops = append(ops, qosOps...)
	queueOps, err := provider.Table(&vswitch.Queue{}).DeleteOps(queue)
	if err != nil {
		return nil, fmt.Errorf("build HTB queue delete for %s: %w", iface, err)
	}
	return append(ops, queueOps...), nil
}

func buildHtbBandwidthEnsureOps(provider compat.TableProvider, ports []vswitch.Port, state *interfaceBandwidthState, config interfaceBandwidthConfig) ([]ovsdb.Operation, error) {
	var ops []ovsdb.Operation
	queueID := ""
	if len(state.queues) != 0 {
		queueID = state.queues[0].UUID
	}
	if queueID == "" {
		queueID = ovsclient.NamedUUID()
		queue := &vswitch.Queue{
			UUID: queueID, ExternalIDs: map[string]string{"iface-id": config.iface},
			OtherConfig: htbQueueConfig(config.egressBPS, config.egressBurstBytes),
		}
		if config.podName != "" && config.podNamespace != "" {
			queue.ExternalIDs["pod"] = config.podNamespace + "/" + config.podName
		}
		queueOps, err := provider.Table(&vswitch.Queue{}).CreateOps(queue)
		if err != nil {
			return nil, fmt.Errorf("build HTB queue create for %s: %w", config.iface, err)
		}
		ops = append(ops, queueOps...)
	} else if queue := state.queueByID[queueID]; queue != nil {
		otherConfig := maps.Clone(queue.OtherConfig)
		if otherConfig == nil {
			otherConfig = make(map[string]string, 2)
		}
		maps.Copy(otherConfig, htbQueueConfig(config.egressBPS, config.egressBurstBytes))
		update := &vswitch.Queue{UUID: queue.UUID, OtherConfig: otherConfig}
		queueOps, err := provider.Table(&vswitch.Queue{}).UpdateOps(queue, update, &update.OtherConfig)
		if err != nil {
			return nil, fmt.Errorf("build HTB queue update for %s: %w", config.iface, err)
		}
		ops = append(ops, queueOps...)
	}

	qos := state.qos
	if qos == nil {
		qos = &vswitch.QoS{
			UUID: ovsclient.NamedUUID(), Type: util.HtbQos,
			ExternalIDs: map[string]string{"iface-id": config.iface}, Queues: map[int]string{0: queueID},
		}
		if config.podName != "" && config.podNamespace != "" {
			qos.ExternalIDs["pod"] = config.podNamespace + "/" + config.podName
		}
		qosOps, err := provider.Table(&vswitch.QoS{}).CreateOps(qos)
		if err != nil {
			return nil, fmt.Errorf("build HTB QoS create for %s: %w", config.iface, err)
		}
		ops = append(ops, qosOps...)
		state.qos = qos
	} else if qos.Type != util.HtbQos || qos.Queues[0] != queueID {
		queues := maps.Clone(qos.Queues)
		if queues == nil {
			queues = make(map[int]string, 1)
		}
		queues[0] = queueID
		update := &vswitch.QoS{UUID: qos.UUID, Type: util.HtbQos, Queues: queues}
		qosOps, err := provider.Table(&vswitch.QoS{}).UpdateOps(qos, update, &update.Type, &update.Queues)
		if err != nil {
			return nil, fmt.Errorf("build HTB QoS update for %s: %w", config.iface, err)
		}
		ops = append(ops, qosOps...)
	}

	for _, port := range ports {
		if port.QOS != nil && *port.QOS == qos.UUID {
			continue
		}
		qosID := qos.UUID
		update := &vswitch.Port{UUID: port.UUID, QOS: &qosID}
		portOps, err := provider.Table(&vswitch.Port{}).UpdateOps(&port, update, &update.QOS)
		if err != nil {
			return nil, fmt.Errorf("build QoS binding for port %s: %w", port.Name, err)
		}
		ops = append(ops, portOps...)
	}
	return ops, nil
}

func htbQueueConfig(maxRateBPS, burstBytes int64) map[string]string {
	config := map[string]string{"burst": strconv.FormatInt(burstBytes, 10)}
	if maxRateBPS > 0 {
		config["max-rate"] = strconv.FormatInt(maxRateBPS, 10)
	}
	return config
}

func listPortsForInterface(provider compat.TableProvider, interfaceID string) ([]vswitch.Port, error) {
	var ports []vswitch.Port
	if err := provider.Table(&vswitch.Port{}).Filter(context.Background(), func(row *vswitch.Port) bool {
		return slices.Contains(row.Interfaces, interfaceID)
	}, &ports); err != nil {
		return nil, fmt.Errorf("list ports for interface %s: %w", interfaceID, err)
	}
	return ports, nil
}

func clearPortQosBindingTable(provider compat.TableProvider, ifaceID string) error {
	if ifaceID == "" {
		return nil
	}
	ctx := context.Background()
	var interfaces []vswitch.Interface
	if err := provider.Table(&vswitch.Interface{}).Filter(ctx, func(row *vswitch.Interface) bool {
		return row.ExternalIDs["iface-id"] == ifaceID
	}, &interfaces); err != nil {
		return fmt.Errorf("list interfaces for QoS cleanup %s: %w", ifaceID, err)
	}
	interfaceIDs := make(map[string]struct{}, len(interfaces))
	for _, row := range interfaces {
		interfaceIDs[row.UUID] = struct{}{}
	}
	var ports []vswitch.Port
	if err := provider.Table(&vswitch.Port{}).Filter(ctx, func(row *vswitch.Port) bool {
		for _, interfaceID := range row.Interfaces {
			if _, ok := interfaceIDs[interfaceID]; ok {
				return row.QOS != nil
			}
		}
		return false
	}, &ports); err != nil {
		return fmt.Errorf("list ports for QoS cleanup %s: %w", ifaceID, err)
	}
	if len(ports) == 0 {
		return nil
	}
	operations := make([]ovsdb.Operation, 0, len(ports))
	for i := range ports {
		update := &vswitch.Port{UUID: ports[i].UUID}
		ops, err := provider.Table(&vswitch.Port{}).UpdateOps(&ports[i], update, &update.QOS)
		if err != nil {
			return fmt.Errorf("build QoS binding cleanup for %s: %w", ports[i].Name, err)
		}
		operations = append(operations, ops...)
	}
	return provider.Table(&vswitch.Port{}).Transact(ctx, "qos-port-unbind", operations...)
}

func clearPodBandwidthTable(provider compat.TableProvider, podName, podNamespace, ifaceID string) error {
	ctx := context.Background()
	var qosRows []vswitch.QoS
	if err := provider.Table(&vswitch.QoS{}).Filter(ctx, func(row *vswitch.QoS) bool {
		if ifaceID != "" {
			return row.ExternalIDs["iface-id"] == ifaceID
		}
		return row.ExternalIDs["pod"] == podNamespace+"/"+podName
	}, &qosRows); err != nil {
		return fmt.Errorf("list QoS rows for cleanup: %w", err)
	}
	if len(qosRows) == 0 {
		return nil
	}

	var ports []vswitch.Port
	if err := provider.Table(&vswitch.Port{}).Filter(ctx, func(row *vswitch.Port) bool {
		return row.QOS != nil && *row.QOS != ""
	}, &ports); err != nil {
		return fmt.Errorf("list ports with QoS bindings: %w", err)
	}
	used := make(map[string]struct{}, len(ports))
	for _, port := range ports {
		used[*port.QOS] = struct{}{}
	}

	var ops []ovsdb.Operation
	for i := range qosRows {
		qos := &qosRows[i]
		if _, ok := used[qos.UUID]; ok {
			continue
		}
		deleteOps, err := provider.Table(&vswitch.QoS{}).DeleteOps(qos)
		if err != nil {
			return fmt.Errorf("build QoS cleanup for %s: %w", qos.UUID, err)
		}
		ops = append(ops, deleteOps...)
	}
	return provider.Table(&vswitch.QoS{}).Transact(ctx, "qos-bandwidth-cleanup", ops...)
}

func clearHtbQosQueueTable(provider compat.TableProvider, podName, podNamespace, ifaceID string) error {
	ctx := context.Background()
	var queueRows []vswitch.Queue
	if err := provider.Table(&vswitch.Queue{}).Filter(ctx, func(row *vswitch.Queue) bool {
		if ifaceID != "" {
			return row.ExternalIDs["iface-id"] == ifaceID
		}
		return row.ExternalIDs["pod"] == podNamespace+"/"+podName
	}, &queueRows); err != nil {
		return fmt.Errorf("list queues for cleanup: %w", err)
	}
	if len(queueRows) == 0 {
		return nil
	}

	var qosRows []vswitch.QoS
	if err := provider.Table(&vswitch.QoS{}).List(ctx, &qosRows); err != nil {
		return fmt.Errorf("list QoS queue references: %w", err)
	}
	used := make(map[string]struct{}, len(qosRows))
	for _, qos := range qosRows {
		for _, queueID := range qos.Queues {
			used[queueID] = struct{}{}
		}
	}

	var ops []ovsdb.Operation
	for i := range queueRows {
		queue := &queueRows[i]
		if _, ok := used[queue.UUID]; ok {
			continue
		}
		deleteOps, err := provider.Table(&vswitch.Queue{}).DeleteOps(queue)
		if err != nil {
			return fmt.Errorf("build queue cleanup for %s: %w", queue.UUID, err)
		}
		ops = append(ops, deleteOps...)
	}
	return provider.Table(&vswitch.Queue{}).Transact(ctx, "qos-queue-cleanup", ops...)
}
