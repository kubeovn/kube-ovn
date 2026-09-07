package ovs

import (
	"context"
	"fmt"
	"maps"
	"math"
	"strconv"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func setNetemQosTable(provider compat.TableProvider, podName, podNamespace, iface, latency, limit, loss, jitter string) error {
	desiredConfig, err := parseNetemQosConfig(latency, limit, loss, jitter)
	if err != nil {
		return err
	}

	ctx := context.Background()
	var interfaces []vswitch.Interface
	if err := provider.Table(&vswitch.Interface{}).Filter(ctx, func(row *vswitch.Interface) bool {
		return row.ExternalIDs["iface-id"] == iface
	}, &interfaces); err != nil {
		return fmt.Errorf("list interfaces for netem qos %s: %w", iface, err)
	}
	if len(interfaces) == 0 {
		return nil
	}

	var qosRows []vswitch.QoS
	if err := provider.Table(&vswitch.QoS{}).Filter(ctx, func(row *vswitch.QoS) bool {
		if iface != "" {
			return row.ExternalIDs["iface-id"] == iface
		}
		return row.ExternalIDs["pod"] == podNamespace+"/"+podName
	}, &qosRows); err != nil {
		return fmt.Errorf("list QoS rows for netem %s: %w", iface, err)
	}

	interfaceIDs := make(map[string]struct{}, len(interfaces))
	for i := range interfaces {
		interfaceIDs[interfaces[i].UUID] = struct{}{}
	}
	var ports []vswitch.Port
	if err := provider.Table(&vswitch.Port{}).Filter(ctx, func(row *vswitch.Port) bool {
		for _, interfaceID := range row.Interfaces {
			if _, ok := interfaceIDs[interfaceID]; ok {
				return true
			}
		}
		return false
	}, &ports); err != nil {
		return fmt.Errorf("list ports for netem qos %s: %w", iface, err)
	}
	var allPorts []vswitch.Port
	if err := provider.Table(&vswitch.Port{}).List(ctx, &allPorts); err != nil {
		return fmt.Errorf("list QoS bindings for netem %s: %w", iface, err)
	}

	for i := range qosRows {
		if qosRows[i].Type != util.NetemQos && len(desiredConfig) != 0 {
			// HTB is intentionally higher priority than netem. Keep the
			// existing configuration and let the caller reconcile it later.
			return nil
		}
	}

	targetPortIDs := make(map[string]struct{}, len(ports))
	for _, port := range ports {
		targetPortIDs[port.UUID] = struct{}{}
	}
	usedByOtherPort := make(map[string]bool, len(qosRows))
	for _, port := range allPorts {
		if port.QOS == nil {
			continue
		}
		if _, target := targetPortIDs[port.UUID]; target {
			continue
		}
		usedByOtherPort[*port.QOS] = true
	}

	ops := make([]ovsdb.Operation, 0, len(qosRows)*2+len(ports)*2+2)
	removeQoS := make(map[string]struct{}, len(qosRows))
	var matchingQoS *vswitch.QoS
	if len(desiredConfig) != 0 {
		for i := range qosRows {
			if qosRows[i].Type == util.NetemQos && maps.Equal(qosRows[i].OtherConfig, desiredConfig) {
				matchingQoS = &qosRows[i]
				break
			}
		}
	}

	for i := range qosRows {
		qos := &qosRows[i]
		if matchingQoS != nil && qos.UUID == matchingQoS.UUID {
			continue
		}
		if qos.Type != util.NetemQos {
			continue
		}
		removeQoS[qos.UUID] = struct{}{}
	}

	for i := range ports {
		port := &ports[i]
		if port.QOS == nil {
			continue
		}
		if _, remove := removeQoS[*port.QOS]; !remove && (matchingQoS == nil || *port.QOS != matchingQoS.UUID) {
			continue
		}
		if matchingQoS != nil && *port.QOS == matchingQoS.UUID && len(desiredConfig) != 0 {
			continue
		}
		portUpdate := &vswitch.Port{UUID: port.UUID}
		portOps, err := provider.Table(&vswitch.Port{}).UpdateOps(port, portUpdate, &portUpdate.QOS)
		if err != nil {
			return fmt.Errorf("build netem QoS unbind for port %s: %w", port.Name, err)
		}
		ops = append(ops, portOps...)
	}

	for qosID := range removeQoS {
		if usedByOtherPort[qosID] {
			continue
		}
		for i := range qosRows {
			if qosRows[i].UUID != qosID {
				continue
			}
			deleteOps, err := provider.Table(&vswitch.QoS{}).DeleteOps(&qosRows[i])
			if err != nil {
				return fmt.Errorf("build netem QoS delete for %s: %w", qosID, err)
			}
			ops = append(ops, deleteOps...)
			break
		}
	}

	if len(desiredConfig) != 0 && matchingQoS == nil {
		qos := &vswitch.QoS{
			UUID:        client.NamedUUID(),
			Type:        util.NetemQos,
			ExternalIDs: netemQosExternalIDs(podName, podNamespace, iface),
			OtherConfig: desiredConfig,
		}
		createOps, err := provider.Table(&vswitch.QoS{}).CreateOps(qos)
		if err != nil {
			return fmt.Errorf("build netem QoS create for %s: %w", iface, err)
		}
		ops = append(ops, createOps...)
		for i := range ports {
			port := &ports[i]
			portUpdate := &vswitch.Port{UUID: port.UUID, QOS: new(qos.UUID)}
			portOps, err := provider.Table(&vswitch.Port{}).UpdateOps(port, portUpdate, &portUpdate.QOS)
			if err != nil {
				return fmt.Errorf("build netem QoS binding for port %s: %w", port.Name, err)
			}
			ops = append(ops, portOps...)
		}
	}

	if len(ops) == 0 {
		return nil
	}
	return provider.Table(&vswitch.QoS{}).Transact(ctx, "netem-qos-reconcile", ops...)
}

func parseNetemQosConfig(latency, limit, loss, jitter string) (map[string]string, error) {
	parseInt := func(name, value string) (int, error) {
		if value == "" {
			return 0, nil
		}
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return 0, fmt.Errorf("invalid netem %s %q: %w", name, value, err)
		}
		if parsed < 0 {
			return 0, fmt.Errorf("netem %s %q must not be negative", name, value)
		}
		return parsed, nil
	}

	latencyMs, err := parseInt("latency", latency)
	if err != nil {
		return nil, err
	}
	limitPkts, err := parseInt("limit", limit)
	if err != nil {
		return nil, err
	}
	jitterMs, err := parseInt("jitter", jitter)
	if err != nil {
		return nil, err
	}
	lossPercent := 0.0
	if loss != "" {
		lossPercent, err = strconv.ParseFloat(loss, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid netem loss %q: %w", loss, err)
		}
		if math.IsNaN(lossPercent) || math.IsInf(lossPercent, 0) || lossPercent < 0 || lossPercent > 100 {
			return nil, fmt.Errorf("netem loss %q must be between 0 and 100", loss)
		}
	}
	return netemQosConfig(latencyMs, limitPkts, lossPercent, jitterMs), nil
}

func netemQosConfig(latencyMs, limitPkts int, lossPercent float64, jitterMs int) map[string]string {
	config := make(map[string]string, 4)
	if latencyMs > 0 {
		config["latency"] = strconv.Itoa(latencyMs * 1000)
	}
	if jitterMs > 0 {
		config["jitter"] = strconv.Itoa(jitterMs * 1000)
	}
	if limitPkts > 0 {
		config["limit"] = strconv.Itoa(limitPkts)
	}
	if lossPercent > 0 {
		config["loss"] = strconv.FormatFloat(lossPercent, 'f', -1, 64)
	}
	return config
}

func netemQosExternalIDs(podName, podNamespace, iface string) map[string]string {
	externalIDs := map[string]string{"iface-id": iface}
	if podName != "" && podNamespace != "" {
		externalIDs["pod"] = podNamespace + "/" + podName
	}
	return externalIDs
}

func isHtbQosTable(provider compat.TableProvider, iface string) (bool, error) {
	var rows []vswitch.QoS
	if err := provider.Table(&vswitch.QoS{}).Filter(context.Background(), func(row *vswitch.QoS) bool {
		return row.ExternalIDs["iface-id"] == iface
	}, &rows); err != nil {
		return false, fmt.Errorf("list QoS rows for %s: %w", iface, err)
	}
	for _, row := range rows {
		if row.Type == util.HtbQos {
			return true, nil
		}
	}
	return false, nil
}

func isUserspaceDataPathTable(provider compat.TableProvider) (bool, error) {
	var bridges []vswitch.Bridge
	if err := provider.Table(&vswitch.Bridge{}).Filter(context.Background(), func(row *vswitch.Bridge) bool {
		return row.Name == "br-int"
	}, &bridges); err != nil {
		return false, fmt.Errorf("list integration bridge: %w", err)
	}
	return len(bridges) > 0 && bridges[0].DatapathType == "netdev", nil
}
