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
	ingressKPS, err := parseAndScaleBandwidthRate(ingress, 1000)
	if err != nil {
		return fmt.Errorf("invalid ingress bandwidth: %w", err)
	}
	egressBPS, err := parseAndScaleBandwidthRate(egress, 1000*1000)
	if err != nil {
		return fmt.Errorf("invalid egress bandwidth: %w", err)
	}
	ingressBurstKbit := computeIngressPolicingBurstKbit(ingressKPS, ingressBurst)
	egressBurstBytes := computeHtbBurstBytes(egressBPS, egressBurst)

	ctx := context.Background()
	var interfaces []vswitch.Interface
	if err := provider.Table(&vswitch.Interface{}).Filter(ctx, func(row *vswitch.Interface) bool {
		return row.ExternalIDs["iface-id"] == iface
	}, &interfaces); err != nil {
		return fmt.Errorf("list interfaces for %s: %w", iface, err)
	}
	if len(interfaces) == 0 {
		return nil
	}

	var queues []vswitch.Queue
	if err := provider.Table(&vswitch.Queue{}).Filter(ctx, func(row *vswitch.Queue) bool {
		return row.ExternalIDs["iface-id"] == iface
	}, &queues); err != nil {
		return fmt.Errorf("list queues for %s: %w", iface, err)
	}
	var qosRows []vswitch.QoS
	if err := provider.Table(&vswitch.QoS{}).Filter(ctx, func(row *vswitch.QoS) bool {
		return row.ExternalIDs["iface-id"] == iface
	}, &qosRows); err != nil {
		return fmt.Errorf("list QoS rows for %s: %w", iface, err)
	}

	queueByID := make(map[string]*vswitch.Queue, len(queues))
	for i := range queues {
		queueByID[queues[i].UUID] = &queues[i]
	}
	var qos *vswitch.QoS
	if len(qosRows) != 0 {
		qos = &qosRows[0]
	}

	ops := make([]ovsdb.Operation, 0, len(interfaces)*4+3)
	for i := range interfaces {
		ifaceRow := &interfaces[i]
		interfaceUpdate := &vswitch.Interface{
			UUID:                 ifaceRow.UUID,
			IngressPolicingRate:  int(ingressKPS),
			IngressPolicingBurst: int(ingressBurstKbit),
		}
		interfaceOps, err := provider.Table(&vswitch.Interface{}).UpdateOps(ifaceRow, interfaceUpdate,
			&interfaceUpdate.IngressPolicingRate, &interfaceUpdate.IngressPolicingBurst)
		if err != nil {
			return fmt.Errorf("build interface bandwidth update for %s: %w", ifaceRow.Name, err)
		}
		ops = append(ops, interfaceOps...)

		portRows, err := listPortsForInterface(provider, ifaceRow.UUID)
		if err != nil {
			return err
		}
		if egressBPS <= 0 {
			if qos == nil || qos.Type != util.HtbQos {
				continue
			}
			queueID, ok := qos.Queues[0]
			if !ok || queueID == "" {
				continue
			}
			queue, ok := queueByID[queueID]
			if !ok {
				continue
			}
			deleteValues := make(map[string]string, 2)
			for _, key := range []string{"max-rate", "burst"} {
				if value, exists := queue.OtherConfig[key]; exists {
					deleteValues[key] = value
				}
			}
			if len(deleteValues) == 0 {
				continue
			}
			queueOps, err := provider.Table(&vswitch.Queue{}).MutateOps(queue,
				model.Mutation{Field: &queue.OtherConfig, Mutator: ovsdb.MutateOperationDelete, Value: deleteValues})
			if err != nil {
				return fmt.Errorf("build HTB queue limit removal for %s: %w", iface, err)
			}
			ops = append(ops, queueOps...)
			if len(queue.OtherConfig) == len(deleteValues) {
				for _, port := range portRows {
					if port.QOS == nil || *port.QOS != qos.UUID {
						continue
					}
					portUpdate := &vswitch.Port{UUID: port.UUID}
					portOps, err := provider.Table(&vswitch.Port{}).UpdateOps(&port, portUpdate, &portUpdate.QOS)
					if err != nil {
						return fmt.Errorf("build HTB QoS binding removal for port %s: %w", port.Name, err)
					}
					ops = append(ops, portOps...)
				}
				qosOps, err := provider.Table(&vswitch.QoS{}).DeleteOps(qos)
				if err != nil {
					return fmt.Errorf("build HTB QoS delete for %s: %w", iface, err)
				}
				ops = append(ops, qosOps...)
				queueOps, err = provider.Table(&vswitch.Queue{}).DeleteOps(queue)
				if err != nil {
					return fmt.Errorf("build HTB queue delete for %s: %w", iface, err)
				}
				ops = append(ops, queueOps...)
			}
			continue
		}

		queueID := ""
		if len(queues) != 0 {
			queueID = queues[0].UUID
		}
		if queueID == "" {
			queueID = ovsclient.NamedUUID()
			queue := &vswitch.Queue{
				UUID: queueID,
				ExternalIDs: map[string]string{
					"iface-id": iface,
				},
				OtherConfig: htbQueueConfig(egressBPS, egressBurstBytes),
			}
			if podName != "" && podNamespace != "" {
				queue.ExternalIDs["pod"] = podNamespace + "/" + podName
			}
			queueOps, err := provider.Table(&vswitch.Queue{}).CreateOps(queue)
			if err != nil {
				return fmt.Errorf("build HTB queue create for %s: %w", iface, err)
			}
			ops = append(ops, queueOps...)
		} else if queue := queueByID[queueID]; queue != nil {
			config := maps.Clone(queue.OtherConfig)
			if config == nil {
				config = make(map[string]string, 2)
			}
			maps.Copy(config, htbQueueConfig(egressBPS, egressBurstBytes))
			queueUpdate := &vswitch.Queue{UUID: queue.UUID, OtherConfig: config}
			queueOps, err := provider.Table(&vswitch.Queue{}).UpdateOps(queue, queueUpdate, &queueUpdate.OtherConfig)
			if err != nil {
				return fmt.Errorf("build HTB queue update for %s: %w", iface, err)
			}
			ops = append(ops, queueOps...)
		}

		if qos == nil {
			qos = &vswitch.QoS{
				UUID: queueID,
				Type: util.HtbQos,
				ExternalIDs: map[string]string{
					"iface-id": iface,
				},
				Queues: map[int]string{0: queueID},
			}
			qos.UUID = ovsclient.NamedUUID()
			if podName != "" && podNamespace != "" {
				qos.ExternalIDs["pod"] = podNamespace + "/" + podName
			}
			qosOps, err := provider.Table(&vswitch.QoS{}).CreateOps(qos)
			if err != nil {
				return fmt.Errorf("build HTB QoS create for %s: %w", iface, err)
			}
			ops = append(ops, qosOps...)
		} else if qos.Type != util.HtbQos || qos.Queues[0] != queueID {
			queuesMap := maps.Clone(qos.Queues)
			if queuesMap == nil {
				queuesMap = make(map[int]string, 1)
			}
			queuesMap[0] = queueID
			qosUpdate := &vswitch.QoS{UUID: qos.UUID, Type: util.HtbQos, Queues: queuesMap}
			qosOps, err := provider.Table(&vswitch.QoS{}).UpdateOps(qos, qosUpdate, &qosUpdate.Type, &qosUpdate.Queues)
			if err != nil {
				return fmt.Errorf("build HTB QoS update for %s: %w", iface, err)
			}
			ops = append(ops, qosOps...)
		}

		for _, port := range portRows {
			if port.QOS != nil && *port.QOS == qos.UUID {
				continue
			}
			qosID := qos.UUID
			portUpdate := &vswitch.Port{UUID: port.UUID, QOS: &qosID}
			portOps, err := provider.Table(&vswitch.Port{}).UpdateOps(&port, portUpdate, &portUpdate.QOS)
			if err != nil {
				return fmt.Errorf("build QoS binding for port %s: %w", port.Name, err)
			}
			ops = append(ops, portOps...)
		}
	}

	if len(ops) == 0 {
		return nil
	}
	return provider.Table(&vswitch.Interface{}).Transact(ctx, "interface-bandwidth-update", ops...)
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
