package ovs

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
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
	if len(providers) == 0 || providers[0] == nil {
		return errors.New("vswitch table provider is nil")
	}
	return clearHtbQosQueueTable(providers[0], podName, podNamespace, iface)
}

func IsHtbQos(iface string, providers ...compat.TableProvider) (bool, error) {
	if len(providers) == 0 || providers[0] == nil {
		return false, errors.New("vswitch table provider is nil")
	}
	return isHtbQosTable(providers[0], iface)
}

func SetHtbQosQueueRecord(podName, podNamespace, iface string, maxRateBPS, burstBytes int64, queueIfaceUIDMap map[string]string) (string, error) {
	var queueCommandValues []string
	var err error
	if maxRateBPS > 0 {
		queueCommandValues = append(queueCommandValues, fmt.Sprintf("other_config:max-rate=%d", maxRateBPS))
	}
	// Always write burst so an explicit "0" from the user is honored (strict
	// shaping with no burst tolerance) and old values on existing queues are
	// overwritten rather than silently retained.
	queueCommandValues = append(queueCommandValues, fmt.Sprintf("other_config:burst=%d", burstBytes))

	if queueUID, ok := queueIfaceUIDMap[iface]; ok {
		if err := Set("queue", queueUID, queueCommandValues...); err != nil {
			klog.Error(err)
			return "", err
		}
	} else {
		queueCommandValues = append(queueCommandValues, "external-ids:iface-id="+iface)
		if podNamespace != "" && podName != "" {
			queueCommandValues = append(queueCommandValues, fmt.Sprintf("external-ids:pod=%s/%s", podNamespace, podName))
		}

		var queueID string
		if queueID, err = ovsCreate("queue", queueCommandValues...); err != nil {
			klog.Error(err)
			return "", err
		}
		queueIfaceUIDMap[iface] = queueID
	}

	return queueIfaceUIDMap[iface], nil
}

// SetQosQueueBinding set qos related to queue record.
func SetQosQueueBinding(podName, podNamespace, ifName, iface, queueUID string, qosIfaceUIDMap map[string]string) error {
	var qosCommandValues []string
	qosCommandValues = append(qosCommandValues, "queues:0="+queueUID)

	if qosUID, ok := qosIfaceUIDMap[iface]; !ok {
		qosCommandValues = append(qosCommandValues, "type=linux-htb", fmt.Sprintf(`external-ids:iface-id="%s"`, iface))
		if podNamespace != "" && podName != "" {
			qosCommandValues = append(qosCommandValues, fmt.Sprintf("external-ids:pod=%s/%s", podNamespace, podName))
		}
		qos, err := ovsCreate("qos", qosCommandValues...)
		if err != nil {
			klog.Error(err)
			return err
		}
		err = Set("port", ifName, "qos="+qos)
		if err != nil {
			klog.Error(err)
			return err
		}
		qosIfaceUIDMap[iface] = qos
	} else {
		qosType, err := Get("qos", qosUID, "type", "", false)
		if err != nil {
			klog.Error(err)
			return err
		}
		if qosType != util.HtbQos {
			klog.Errorf("netem qos exists for pod %s/%s, conflict with current qos, will be changed to htb qos", podNamespace, podName)
			qosCommandValues = append(qosCommandValues, "type=linux-htb")
		}

		if qosType == util.HtbQos {
			queueID, err := Get("qos", qosUID, "queues", "0", false)
			if err != nil {
				klog.Error(err)
				return err
			}
			if queueID == queueUID {
				return nil
			}
		}

		if err := Set("qos", qosUID, qosCommandValues...); err != nil {
			klog.Error(err)
			return err
		}
	}
	return nil
}

// The latency value expressed in us.
func SetNetemQos(podName, podNamespace, iface, latency, limit, loss, jitter string, providers ...compat.TableProvider) error {
	if len(providers) == 0 || providers[0] == nil {
		return errors.New("vswitch table provider is nil")
	}
	return setNetemQosTable(providers[0], podName, podNamespace, iface, latency, limit, loss, jitter)
}

func getNetemQosConfig(qosID string) (string, string, string, string, error) {
	var latency, loss, limit, jitter string

	config, err := Get("qos", qosID, "other_config", "", false)
	if err != nil {
		klog.Errorf("failed to get other_config for qos %s: %v", qosID, err)
		return latency, loss, limit, jitter, err
	}
	if len(config) == 0 {
		return latency, loss, limit, jitter, nil
	}

	values := strings.SplitSeq(strings.Trim(config, "{}"), ",")
	for value := range values {
		records := strings.Split(value, "=")
		switch strings.TrimSpace(records[0]) {
		case "latency":
			latency = strings.TrimSpace(records[1])
		case "loss":
			loss = strings.TrimSpace(records[1])
		case "limit":
			limit = strings.TrimSpace(records[1])
		case "jitter":
			jitter = strings.TrimSpace(records[1])
		}
	}
	return latency, loss, limit, jitter, nil
}

func deleteNetemQosByID(qosID, iface, podName, podNamespace string) error {
	qosType, _ := Get("qos", qosID, "type", "", false)
	if qosType != util.NetemQos {
		return nil
	}

	if err := ClearPortQosBinding(iface); err != nil {
		klog.Errorf("failed to delete qos binding info for interface %s: %v", iface, err)
		return err
	}

	// reuse this function to delete qos record
	if err := ClearPodBandwidth(podName, podNamespace, iface); err != nil {
		klog.Errorf("failed to delete netemqos record for pod %s/%s: %v", podNamespace, podName, err)
		return err
	}
	return nil
}

func IsUserspaceDataPath(providers ...compat.TableProvider) (is bool, err error) {
	if len(providers) == 0 || providers[0] == nil {
		return false, errors.New("vswitch table provider is nil")
	}
	return isUserspaceDataPathTable(providers[0])
}

func CheckAndUpdateHtbQos(podName, podNamespace, ifaceID string, queueIfaceUIDMap map[string]string) error {
	var queueUID string
	var ok bool
	if queueUID, ok = queueIfaceUIDMap[ifaceID]; !ok {
		return nil
	}

	config, err := Get("queue", queueUID, "other_config", "", false)
	if err != nil {
		klog.Errorf("failed to get other_config for queueID %s: %v", queueUID, err)
		return err
	}
	// bandwidth or priority exists, can not delete qos
	if config != "{}" {
		return nil
	}

	if htbQos, _ := IsHtbQos(ifaceID); !htbQos {
		return nil
	}

	if err := ClearPortQosBinding(ifaceID); err != nil {
		klog.Errorf("failed to delete qos binding info: %v", err)
		return err
	}

	if err := ClearPodBandwidth(podName, podNamespace, ifaceID); err != nil {
		klog.Errorf("failed to delete htbqos record: %v", err)
		return err
	}

	if err := ClearHtbQosQueue(podName, podNamespace, ifaceID); err != nil {
		klog.Errorf("failed to delete htbqos queue: %v", err)
		return err
	}
	return nil
}
