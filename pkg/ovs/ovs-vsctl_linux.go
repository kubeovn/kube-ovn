package ovs

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

// SetInterfaceBandwidth set ingress/egress qos for given pod, annotation values are for node/pod
// but ingress/egress parameters here are from the point of ovs port/interface view, so reverse input parameters when call func SetInterfaceBandwidth
func SetInterfaceBandwidth(podName, podNamespace, iface, ingress, egress string) error {
	ingressMPS, _ := strconv.Atoi(ingress)
	ingressKPS := ingressMPS * 1000
	interfaceList, err := ovsFind("interface", "name", fmt.Sprintf("external-ids:iface-id=%s", iface))
	if err != nil {
		return err
	}

	qosIfaceUidMap, err := ListExternalIds("qos")
	if err != nil {
		return err
	}

	queueIfaceUidMap, err := ListExternalIds("queue")
	if err != nil {
		return err
	}

	for _, ifName := range interfaceList {
		// ingress_policing_rate is in Kbps
		err := ovsSet("interface", ifName, fmt.Sprintf("ingress_policing_rate=%d", ingressKPS), fmt.Sprintf("ingress_policing_burst=%d", ingressKPS*8/10))
		if err != nil {
			return err
		}

		egressMPS, _ := strconv.Atoi(egress)
		egressBPS := egressMPS * 1000 * 1000

		if egressBPS > 0 {
			queueUid, err := SetHtbQosQueueRecord(podName, podNamespace, iface, egressBPS, queueIfaceUidMap)
			if err != nil {
				return err
			}

			if err = SetQosQueueBinding(podName, podNamespace, ifName, iface, queueUid, qosIfaceUidMap); err != nil {
				return err
			}
		} else {
			if qosUid, ok := qosIfaceUidMap[iface]; ok {
				qosType, err := ovsGet("qos", qosUid, "type", "")
				if err != nil {
					return err
				}
				if qosType != util.HtbQos {
					continue
				}
				queueId, err := ovsGet("qos", qosUid, "queues", "0")
				if err != nil {
					return err
				}

				if _, err := Exec("remove", "queue", queueId, "other_config", "max-rate"); err != nil {
					return fmt.Errorf("failed to remove rate limit for queue in pod %v/%v, %v", podNamespace, podName, err)
				}
			}
		}

		// Delete Qos and Queue record if both bandwidth and priority do not exist
		if err = CheckAndUpdateHtbQos(podName, podNamespace, iface, queueIfaceUidMap); err != nil {
			klog.Errorf("failed to check htb qos: %v", err)
			return err
		}
	}
	return nil
}

func ClearHtbQosQueue(podName, podNamespace, iface string) error {
	var queueList []string
	var err error
	if iface != "" {
		queueList, err = ovsFind("queue", "_uuid", fmt.Sprintf(`external-ids:iface-id="%s"`, iface))
		if err != nil {
			return err
		}
	} else {
		queueList, err = ovsFind("queue", "_uuid", fmt.Sprintf(`external-ids:pod="%s/%s"`, podNamespace, podName))
		if err != nil {
			return err
		}
	}

	// https://github.com/kubeovn/kube-ovn/issues/1191
	qosQueueMap, err := ListQosQueueIds()
	if err != nil {
		return err
	}

	for _, queueId := range queueList {
		found := false
		for _, usedQueueId := range qosQueueMap {
			if queueId == usedQueueId {
				found = true
				break
			}
		}
		if found {
			continue
		}

		if err := ovsDestroy("queue", queueId); err != nil {
			return err
		}
	}
	return nil
}

func IsHtbQos(iface string) (bool, error) {
	qosType, err := ovsFind("qos", "type", fmt.Sprintf(`external-ids:iface-id="%s"`, iface))
	if err != nil {
		return false, err
	}

	if len(qosType) != 0 && qosType[0] == util.HtbQos {
		return true, nil
	}
	return false, nil
}

func SetHtbQosQueueRecord(podName, podNamespace, iface string, maxRateBPS int, queueIfaceUidMap map[string]string) (string, error) {
	var queueCommandValues []string
	var err error
	if maxRateBPS > 0 {
		queueCommandValues = append(queueCommandValues, fmt.Sprintf("other_config:max-rate=%d", maxRateBPS))
	}

	if queueUid, ok := queueIfaceUidMap[iface]; ok {
		if err := ovsSet("queue", queueUid, queueCommandValues...); err != nil {
			return queueUid, err
		}
	} else {
		queueCommandValues = append(queueCommandValues, fmt.Sprintf("external-ids:iface-id=%s", iface))
		if podNamespace != "" && podName != "" {
			queueCommandValues = append(queueCommandValues, fmt.Sprintf("external-ids:pod=%s/%s", podNamespace, podName))
		}

		var queueId string
		if queueId, err = ovsCreate("queue", queueCommandValues...); err != nil {
			return queueUid, err
		}
		queueIfaceUidMap[iface] = queueId
	}

	return queueIfaceUidMap[iface], nil
}

// SetQosQueueBinding set qos related to queue record.
func SetQosQueueBinding(podName, podNamespace, ifName, iface, queueUid string, qosIfaceUidMap map[string]string) error {
	var qosCommandValues []string
	qosCommandValues = append(qosCommandValues, fmt.Sprintf("queues:0=%s", queueUid))

	if qosUid, ok := qosIfaceUidMap[iface]; !ok {
		qosCommandValues = append(qosCommandValues, "type=linux-htb", fmt.Sprintf(`external-ids:iface-id="%s"`, iface))
		if podNamespace != "" && podName != "" {
			qosCommandValues = append(qosCommandValues, fmt.Sprintf("external-ids:pod=%s/%s", podNamespace, podName))
		}
		qos, err := ovsCreate("qos", qosCommandValues...)
		if err != nil {
			return err
		}
		err = ovsSet("port", ifName, fmt.Sprintf("qos=%s", qos))
		if err != nil {
			return err
		}
		qosIfaceUidMap[iface] = qos
	} else {
		qosType, err := ovsGet("qos", qosUid, "type", "")
		if err != nil {
			return err
		}
		if qosType != util.HtbQos {
			klog.Errorf("netem qos exists for pod %s/%s, conflict with current qos, will be changed to htb qos", podNamespace, podName)
			qosCommandValues = append(qosCommandValues, "type=linux-htb")
		}

		if qosType == util.HtbQos {
			queueId, err := ovsGet("qos", qosUid, "queues", "0")
			if err != nil {
				return err
			}
			if queueId == queueUid {
				return nil
			}
		}

		if err := ovsSet("qos", qosUid, qosCommandValues...); err != nil {
			return err
		}
	}
	return nil
}

// The latency value expressed in us.
func SetNetemQos(podName, podNamespace, iface, latency, limit, loss, jitter string) error {
	latencyMs, _ := strconv.Atoi(latency)
	latencyUs := latencyMs * 1000
	jitterMs, _ := strconv.Atoi(jitter)
	jitterUs := jitterMs * 1000
	limitPkts, _ := strconv.Atoi(limit)
	lossPercent, _ := strconv.ParseFloat(loss, 64)

	interfaceList, err := ovsFind("interface", "name", fmt.Sprintf("external-ids:iface-id=%s", iface))
	if err != nil {
		return err
	}

	for _, ifName := range interfaceList {
		qosList, err := GetQosList(podName, podNamespace, iface)
		if err != nil {
			return err
		}

		var qosCommandValues []string
		if latencyMs > 0 {
			qosCommandValues = append(qosCommandValues, fmt.Sprintf("other_config:latency=%d", latencyUs))
		}
		if jitterMs > 0 {
			qosCommandValues = append(qosCommandValues, fmt.Sprintf("other_config:jitter=%d", jitterUs))
		}
		if limitPkts > 0 {
			qosCommandValues = append(qosCommandValues, fmt.Sprintf("other_config:limit=%d", limitPkts))
		}
		if lossPercent > 0 {
			qosCommandValues = append(qosCommandValues, fmt.Sprintf("other_config:loss=%v", lossPercent))
		}
		if latencyMs > 0 || limitPkts > 0 || lossPercent > 0 || jitterMs > 0 {
			if len(qosList) == 0 {
				qosCommandValues = append(qosCommandValues, "type=linux-netem", fmt.Sprintf(`external-ids:iface-id="%s"`, iface))
				if podNamespace != "" && podName != "" {
					qosCommandValues = append(qosCommandValues, fmt.Sprintf("external-ids:pod=%s/%s", podNamespace, podName))
				}

				qos, err := ovsCreate("qos", qosCommandValues...)
				if err != nil {
					return err
				}

				if err = ovsSet("port", ifName, fmt.Sprintf("qos=%s", qos)); err != nil {
					return err
				}
			} else {
				for _, qos := range qosList {
					qosType, err := ovsGet("qos", qos, "type", "")
					if err != nil {
						return err
					}
					if qosType != util.NetemQos {
						klog.Errorf("htb qos with higher priority exists for pod %v/%v, conflict with netem qos config, please delete htb qos first", podNamespace, podName)
						return nil
					}

					latencyVal, lossVal, limitVal, jitterVal, err := getNetemQosConfig(qos)
					if err != nil {
						klog.Errorf("failed to get other_config for qos %s: %v", qos, err)
						return err
					}

					if latencyVal == strconv.Itoa(latencyUs) && limitVal == limit && lossVal == loss && jitterVal == strconv.Itoa(jitterUs) {
						klog.Infof("no value changed for netem qos, ignore")
						continue
					}

					if err = deleteNetemQosById(qos, iface, podName, podNamespace); err != nil {
						klog.Errorf("failed to delete netem qos: %v", err)
						return err
					}

					qosCommandValues = append(qosCommandValues, "type=linux-netem", fmt.Sprintf(`external-ids:iface-id="%s"`, iface))
					if podNamespace != "" && podName != "" {
						qosCommandValues = append(qosCommandValues, fmt.Sprintf("external-ids:pod=%s/%s", podNamespace, podName))
					}

					qos, err := ovsCreate("qos", qosCommandValues...)
					if err != nil {
						klog.Errorf("failed to create netem qos: %v", err)
						return err
					}

					if err = ovsSet("port", ifName, fmt.Sprintf("qos=%s", qos)); err != nil {
						klog.Errorf("failed to set netem qos to port: %v", err)
						return err
					}
				}
			}
		} else {
			for _, qos := range qosList {
				if err := deleteNetemQosById(qos, iface, podName, podNamespace); err != nil {
					klog.Errorf("failed to delete netem qos: %v", err)
					return err
				}
			}
		}
	}
	return nil
}

func getNetemQosConfig(qosId string) (string, string, string, string, error) {
	var latency, loss, limit, jitter string

	config, err := ovsGet("qos", qosId, "other_config", "")
	if err != nil {
		klog.Errorf("failed to get other_config for qos %s: %v", qosId, err)
		return latency, loss, limit, jitter, err
	}
	if len(config) == 0 {
		return latency, loss, limit, jitter, nil
	}

	values := strings.Split(strings.Trim(config, "{}"), ",")
	for _, value := range values {
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

func deleteNetemQosById(qosId, iface, podName, podNamespace string) error {
	qosType, _ := ovsGet("qos", qosId, "type", "")
	if qosType != util.NetemQos {
		return nil
	}

	if err := ClearPortQosBinding(iface); err != nil {
		klog.Errorf("failed to delete qos bingding info for interface %s: %v", iface, err)
		return err
	}

	// reuse this function to delete qos record
	if err := ClearPodBandwidth(podName, podNamespace, iface); err != nil {
		klog.Errorf("failed to delete netemqos record for pod %s/%s: %v", podNamespace, podName, err)
		return err
	}
	return nil
}

func IsUserspaceDataPath() (is bool, err error) {
	dp, err := ovsFind("bridge", "datapath_type", "name=br-int")
	if err != nil {
		return false, err
	}
	return len(dp) > 0 && dp[0] == "netdev", nil
}

func CheckAndUpdateHtbQos(podName, podNamespace, ifaceID string, queueIfaceUidMap map[string]string) error {
	var queueUid string
	var ok bool
	if queueUid, ok = queueIfaceUidMap[ifaceID]; !ok {
		return nil
	}

	config, err := ovsGet("queue", queueUid, "other_config", "")
	if err != nil {
		klog.Errorf("failed to get other_config for queueId %s: %v", queueUid, err)
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
