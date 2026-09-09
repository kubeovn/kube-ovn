package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"strconv"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// computeIngressPolicingBurstKbit returns the ingress_policing_burst value (kbit) to write.
// burstMbit is the user-supplied value in Mbit. An empty string preserves the historical
// default of 80% of rate; an explicit "0" is honored as-is (strict policing, no burst).
// When the rate is non-positive the burst is forced to 0 to keep ingress_policing_rate
// and ingress_policing_burst consistent. Unparseable input falls back to the default so
// a typo cannot silently disable the burst budget.
func computeIngressPolicingBurstKbit(rateKbit int64, burstMbit string) int64 {
	if rateKbit <= 0 {
		return 0
	}
	if burstMbit == "" {
		return defaultIngressPolicingBurstKbit(rateKbit)
	}
	v, err := strconv.ParseInt(burstMbit, 10, 64)
	if err != nil || v > math.MaxInt64/1000 || v < math.MinInt64/1000 {
		klog.Warningf("invalid ingress burst value %q, falling back to default", burstMbit)
		return defaultIngressPolicingBurstKbit(rateKbit)
	}
	return v * 1000
}

func defaultIngressPolicingBurstKbit(rateKbit int64) int64 {
	return rateKbit/10*8 + rateKbit%10*8/10
}

// computeHtbBurstBytes returns the linux-htb other_config:burst value (bytes) to write.
// burstMbit is the user-supplied value in Mbit. An empty string defaults to 80% of one
// second worth of rate (rate*0.8/8 bytes); an explicit "0" is honored as-is. A
// non-positive rate forces burst to 0, and unparseable input falls back to the default.
func computeHtbBurstBytes(rateBPS int64, burstMbit string) int64 {
	if rateBPS <= 0 {
		return 0
	}
	if burstMbit == "" {
		return rateBPS / 10
	}
	v, err := strconv.ParseInt(burstMbit, 10, 64)
	if err != nil || v > math.MaxInt64/125000 || v < math.MinInt64/125000 {
		klog.Warningf("invalid egress burst value %q, falling back to default", burstMbit)
		return rateBPS / 10
	}
	return v * 125000
}

func parseAndScaleBandwidthRate(rate string, scale int64) (int64, error) {
	if rate == "" {
		return 0, nil
	}

	value, err := strconv.ParseInt(rate, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid bandwidth rate %q: %w", rate, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("bandwidth rate %q must not be negative", rate)
	}
	if value > kubeovnv1.MaxBandwidthMbps || value > math.MaxInt64/scale {
		return 0, fmt.Errorf("bandwidth rate %q overflows when scaled by %d", rate, scale)
	}
	return value * scale, nil
}

// SetInterfaceBandwidth set ingress/egress qos for given pod, annotation values are for node/pod
// but ingress/egress parameters here are from the point of ovs port/interface view, so reverse input parameters when call func SetInterfaceBandwidth.
//
// ingress and egress are rate values in Mbps; ingressBurst and egressBurst are burst
// values in Mbit. An empty burst falls back to 80% of the corresponding rate; an
// explicit "0" is passed through verbatim.
func SetInterfaceBandwidth(podName, podNamespace, iface, ingress, egress, ingressBurst, egressBurst string, providers ...compat.TableProvider) error {
	if len(providers) == 0 || providers[0] == nil {
		if _, err := parseAndScaleBandwidthRate(ingress, 1000); err != nil {
			return fmt.Errorf("invalid ingress bandwidth: %w", err)
		}
		if _, err := parseAndScaleBandwidthRate(egress, 1000*1000); err != nil {
			return fmt.Errorf("invalid egress bandwidth: %w", err)
		}
		return errors.New("vswitch table provider is nil")
	}
	return setInterfaceBandwidthTable(providers[0], podName, podNamespace, iface, ingress, egress, ingressBurst, egressBurst)
}

func ClearHtbQosQueue(podName, podNamespace, iface string, providers ...compat.TableProvider) error {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return err
	}
	return clearHtbQosQueueTable(provider, podName, podNamespace, iface)
}

func IsHtbQos(iface string, providers ...compat.TableProvider) (bool, error) {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return false, err
	}
	return isHtbQosTable(provider, iface)
}

func SetHtbQosQueueRecord(podName, podNamespace, iface string, maxRateBPS, burstBytes int64, queueIfaceUIDMap map[string]string, providers ...compat.TableProvider) (string, error) {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return "", err
	}
	ctx := context.Background()
	rows, err := vswitchRowsByIfaceID(ctx, provider, &vswitch.Queue{}, iface, func(row *vswitch.Queue) map[string]string { return row.ExternalIDs })
	if err != nil {
		return "", fmt.Errorf("list HTB queues for %s: %w", iface, err)
	}
	existing, err := compat.Unique(rows, true, nil, fmt.Errorf("more than one HTB queue for %s", iface))
	if err != nil {
		return "", err
	}
	config := htbQueueConfig(maxRateBPS, burstBytes)
	var operations []ovsdb.Operation
	queueID := ""
	if existing == nil {
		queueID = ovsclient.NamedUUID()
		externalIDs := map[string]string{"iface-id": iface}
		if podName != "" && podNamespace != "" {
			externalIDs["pod"] = podNamespace + "/" + podName
		}
		row := &vswitch.Queue{UUID: queueID, ExternalIDs: externalIDs, OtherConfig: config}
		ops, err := provider.Table(&vswitch.Queue{}).CreateOps(row)
		if err != nil {
			return "", err
		}
		operations = append(operations, ops...)
	} else {
		queueID = existing.UUID
		update := &vswitch.Queue{UUID: queueID, OtherConfig: mergeVswitchMap(existing.OtherConfig, config)}
		ops, err := provider.Table(&vswitch.Queue{}).UpdateOps(existing, update, &update.OtherConfig)
		if err != nil {
			return "", err
		}
		operations = append(operations, ops...)
	}
	if err := transactTable(ctx, provider.Table(&vswitch.Queue{}), "htb-queue-record", operations); err != nil {
		return "", err
	}
	if queueIfaceUIDMap != nil {
		queueIfaceUIDMap[iface] = queueID
	}
	return queueID, nil
}

// SetQosQueueBinding associates a Queue row with the QoS row for an interface
// and binds that QoS row to the corresponding Port. The optional provider is
// kept variadic for source compatibility with old unit tests; production code
// must provide the vswitch table provider.
func SetQosQueueBinding(podName, podNamespace, ifName, iface, queueUID string, qosIfaceUIDMap map[string]string, providers ...compat.TableProvider) error {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return err
	}
	if queueUID == "" {
		return errors.New("QoS queue UUID is empty")
	}

	ctx := context.Background()
	port, err := findNamedRow(ctx, provider, &vswitch.Port{}, ifName, "port", true, func(row *vswitch.Port) string { return row.Name })
	if err != nil {
		return err
	}

	var qosRows []vswitch.QoS
	if qosID := qosIfaceUIDMap[iface]; qosID != "" {
		qosRows, err = vswitchRowsByUUID(ctx, provider, &vswitch.QoS{}, qosID, func(row *vswitch.QoS) string { return row.UUID })
		if err != nil {
			return fmt.Errorf("find QoS %q: %w", qosID, err)
		}
	}
	if len(qosRows) == 0 {
		qosRows, err = vswitchRowsByIfaceID(ctx, provider, &vswitch.QoS{}, iface, func(row *vswitch.QoS) map[string]string { return row.ExternalIDs })
		if err != nil {
			return fmt.Errorf("find QoS for interface %q: %w", iface, err)
		}
	}
	qos, err := compat.Unique(qosRows, true, nil, fmt.Errorf("more than one QoS row for interface %q", iface))
	if err != nil {
		return err
	}

	qosTable := provider.Table(&vswitch.QoS{})
	portTable := provider.Table(&vswitch.Port{})
	operations := make([]ovsdb.Operation, 0, 3)
	if qos == nil {
		qos = &vswitch.QoS{
			UUID:        ovsclient.NamedUUID(),
			Type:        util.HtbQos,
			ExternalIDs: map[string]string{"iface-id": iface},
			Queues:      map[int]string{0: queueUID},
		}
		if podName != "" && podNamespace != "" {
			qos.ExternalIDs["pod"] = podNamespace + "/" + podName
		}
		createOps, err := qosTable.CreateOps(qos)
		if err != nil {
			return fmt.Errorf("build QoS create for %q: %w", iface, err)
		}
		operations = append(operations, createOps...)
		if qosIfaceUIDMap != nil {
			qosIfaceUIDMap[iface] = qos.UUID
		}
	} else {
		if qos.Type != util.HtbQos {
			klog.Errorf("netem QoS exists for pod %s/%s, changing it to HTB QoS", podNamespace, podName)
		}
		if qos.Type != util.HtbQos || qos.Queues[0] != queueUID {
			queues := maps.Clone(qos.Queues)
			if queues == nil {
				queues = make(map[int]string, 1)
			}
			queues[0] = queueUID
			update := &vswitch.QoS{UUID: qos.UUID, Type: util.HtbQos, Queues: queues}
			updateOps, err := qosTable.UpdateOps(qos, update, &update.Type, &update.Queues)
			if err != nil {
				return fmt.Errorf("build QoS update for %q: %w", iface, err)
			}
			operations = append(operations, updateOps...)
		}
		if qosIfaceUIDMap != nil {
			qosIfaceUIDMap[iface] = qos.UUID
		}
	}

	if port.QOS == nil || *port.QOS != qos.UUID {
		qosID := qos.UUID
		portUpdate := &vswitch.Port{UUID: port.UUID, QOS: &qosID}
		updateOps, err := portTable.UpdateOps(port, portUpdate, &portUpdate.QOS)
		if err != nil {
			return fmt.Errorf("build QoS binding for port %q: %w", ifName, err)
		}
		operations = append(operations, updateOps...)
	}
	return transactTable(ctx, qosTable, "qos-queue-binding", operations)
}

// The latency value expressed in us.
func SetNetemQos(podName, podNamespace, iface, latency, limit, loss, jitter string, providers ...compat.TableProvider) error {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return err
	}
	return setNetemQosTable(provider, podName, podNamespace, iface, latency, limit, loss, jitter)
}

func getNetemQosConfig(qosID string, providers ...compat.TableProvider) (string, string, string, string, error) {
	var latency, loss, limit, jitter string
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return latency, loss, limit, jitter, err
	}

	rows, err := vswitchRowsByUUID(context.Background(), provider, &vswitch.QoS{}, qosID, func(row *vswitch.QoS) string { return row.UUID })
	if err != nil {
		return latency, loss, limit, jitter, fmt.Errorf("find QoS %q: %w", qosID, err)
	}
	qos, err := compat.Unique(rows, false,
		fmt.Errorf("expected one QoS %q, found %d", qosID, len(rows)),
		fmt.Errorf("expected one QoS %q, found %d", qosID, len(rows)),
	)
	if err != nil {
		return latency, loss, limit, jitter, err
	}
	config := qos.OtherConfig
	if len(config) == 0 {
		return latency, loss, limit, jitter, nil
	}
	latency = config["latency"]
	loss = config["loss"]
	limit = config["limit"]
	jitter = config["jitter"]
	return latency, loss, limit, jitter, nil
}

func deleteNetemQosByID(qosID, iface, podName, podNamespace string, providers ...compat.TableProvider) error {
	if len(providers) == 0 || providers[0] == nil {
		return nil
	}
	rows, err := vswitchRowsByUUID(context.Background(), providers[0], &vswitch.QoS{}, qosID, func(row *vswitch.QoS) string { return row.UUID })
	if err != nil {
		return fmt.Errorf("find QoS %q: %w", qosID, err)
	}
	if len(rows) == 0 || rows[0].Type != util.NetemQos {
		return nil
	}

	if err := ClearPortQosBinding(iface, providers[0]); err != nil {
		klog.Errorf("failed to delete qos binding info for interface %s: %v", iface, err)
		return err
	}

	// reuse this function to delete qos record
	if err := ClearPodBandwidth(podName, podNamespace, iface, providers[0]); err != nil {
		klog.Errorf("failed to delete netemqos record for pod %s/%s: %v", podNamespace, podName, err)
		return err
	}
	return nil
}

func IsUserspaceDataPath(providers ...compat.TableProvider) (is bool, err error) {
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return false, err
	}
	return isUserspaceDataPathTable(provider)
}

func CheckAndUpdateHtbQos(podName, podNamespace, ifaceID string, queueIfaceUIDMap map[string]string, providers ...compat.TableProvider) error {
	var queueUID string
	var ok bool
	if queueUID, ok = queueIfaceUIDMap[ifaceID]; !ok {
		return nil
	}
	provider, err := vswitchProvider(providers...)
	if err != nil {
		return err
	}

	queues, err := vswitchRowsByUUID(context.Background(), provider, &vswitch.Queue{}, queueUID, func(row *vswitch.Queue) string { return row.UUID })
	if err != nil {
		return fmt.Errorf("find queue %q: %w", queueUID, err)
	}
	if len(queues) == 0 {
		return fmt.Errorf("queue %q not found", queueUID)
	}
	// bandwidth or priority exists, can not delete qos
	if len(queues[0].OtherConfig) != 0 {
		return nil
	}

	if htbQos, err := IsHtbQos(ifaceID, provider); err != nil {
		return err
	} else if !htbQos {
		return nil
	}

	if err := ClearPortQosBinding(ifaceID, provider); err != nil {
		klog.Errorf("failed to delete qos binding info: %v", err)
		return err
	}

	if err := ClearPodBandwidth(podName, podNamespace, ifaceID, provider); err != nil {
		klog.Errorf("failed to delete htbqos record: %v", err)
		return err
	}

	if err := ClearHtbQosQueue(podName, podNamespace, ifaceID, provider); err != nil {
		klog.Errorf("failed to delete htbqos queue: %v", err)
		return err
	}
	return nil
}
