package ovs

import (
	"fmt"
	"strconv"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

// SetInterfaceBandwidth set ingress/egress qos for given pod, annotation values are for node/pod
// but ingress/egress parameters here are from the point of ovs port/interface view, so reverse input parameters when call func SetInterfaceBandwidth
func SetInterfaceBandwidth(podName, podNamespace, iface, ingress, egress, podPriority string) error {
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
			queueUid, err := SetHtbQosQueueRecord(podName, podNamespace, iface, podPriority, egressBPS, queueIfaceUidMap)
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

				// It's difficult to check if qos and queue should be destroyed here since can not get subnet info here. So leave destroy operation in loop check
				if _, err := Exec("remove", "queue", queueId, "other_config", "max-rate"); err != nil {
					return fmt.Errorf("failed to remove rate limit for queue in pod %v/%v, %v", podNamespace, podName, err)
				}
			}
		}

		if err = SetHtbQosPriority(podName, podNamespace, iface, ifName, podPriority, qosIfaceUidMap, queueIfaceUidMap); err != nil {
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
	qosList, err := ovsFind("qos", "_uuid", fmt.Sprintf(`external-ids:iface-id="%s"`, iface))
	if err != nil {
		return false, err
	}

	for _, qos := range qosList {
		qosType, err := ovsGet("qos", qos, "type", "")
		if err != nil {
			return false, err
		}
		if qosType == util.HtbQos {
			return true, nil
		}
	}
	return false, nil
}

func SetHtbQosQueueRecord(podName, podNamespace, iface, priority string, maxRateBPS int, queueIfaceUidMap map[string]string) (string, error) {
	var queueCommandValues []string
	var err error
	if maxRateBPS > 0 {
		queueCommandValues = append(queueCommandValues, fmt.Sprintf("other_config:max-rate=%d", maxRateBPS))
	}
	if priority != "" {
		queueCommandValues = append(queueCommandValues, fmt.Sprintf("other_config:priority=%s", priority))
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

// SetPodQosPriority set qos to this pod port.
func SetPodQosPriority(podName, podNamespace, ifaceID, priority string, qosIfaceUidMap, queueIfaceUidMap map[string]string) error {
	interfaceList, err := ovsFind("interface", "name", fmt.Sprintf("external-ids:iface-id=%s", ifaceID))
	if err != nil {
		return err
	}

	for _, ifName := range interfaceList {
		if err = SetHtbQosPriority(podName, podNamespace, ifaceID, ifName, priority, qosIfaceUidMap, queueIfaceUidMap); err != nil {
			return err
		}
	}
	return nil
}

func SetHtbQosPriority(podName, podNamespace, iface, ifName, priority string, qosIfaceUidMap, queueIfaceUidMap map[string]string) error {
	if priority != "" {
		queueUid, err := SetHtbQosQueueRecord(podName, podNamespace, iface, priority, 0, queueIfaceUidMap)
		if err != nil {
			return err
		}

		if err = SetQosQueueBinding(podName, podNamespace, ifName, iface, queueUid, qosIfaceUidMap); err != nil {
			return err
		}
	} else {
		var qosUid string
		var ok bool
		if qosUid, ok = qosIfaceUidMap[iface]; !ok {
			return nil
		}

		qosType, err := ovsGet("qos", qosUid, "type", "")
		if err != nil {
			return err
		}
		if qosType != util.HtbQos {
			return nil
		}
		queueId, err := ovsGet("qos", qosUid, "queues", "0")
		if err != nil {
			return err
		}

		// It's difficult to check if qos and queue should be destroyed here since can not get subnet info here. So leave destroy operation in subnet loop check
		if _, err := Exec("remove", "queue", queueId, "other_config", "priority"); err != nil {
			return fmt.Errorf("failed to remove priority for queue in pod %v/%v, %v", podNamespace, podName, err)
		}
	}

	return nil
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
func SetNetemQos(podName, podNamespace, iface, latency, limit, loss string) error {
	latencyMs, _ := strconv.Atoi(latency)
	latencyUs := latencyMs * 1000
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
		if limitPkts > 0 {
			qosCommandValues = append(qosCommandValues, fmt.Sprintf("other_config:limit=%d", limitPkts))
		}
		if lossPercent > 0 {
			qosCommandValues = append(qosCommandValues, fmt.Sprintf("other_config:loss=%v", lossPercent))
		}
		if latencyMs > 0 || limitPkts > 0 || lossPercent > 0 {
			if len(qosList) == 0 {
				qosCommandValues = append(qosCommandValues, "type=linux-netem", fmt.Sprintf(`external-ids:iface-id="%s"`, iface))
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

					if err := ovsSet("qos", qos, qosCommandValues...); err != nil {
						return err
					}

					if latencyMs == 0 {
						if err := ovsRemove("qos", qos, "other_config", "latency"); err != nil {
							return err
						}
					}
					if limitPkts == 0 {
						if err := ovsRemove("qos", qos, "other_config", "limit"); err != nil {
							return err
						}
					}
					if lossPercent == 0 {
						if err := ovsRemove("qos", qos, "other_config", "loss"); err != nil {
							return err
						}
					}
				}
			}
		} else {
			for _, qos := range qosList {
				qosType, _ := ovsGet("qos", qos, "type", "")
				if qosType != util.NetemQos {
					continue
				}

				if err = ClearPortQosBinding(iface); err != nil {
					klog.Errorf("failed to delete qos bingding info for interface %s: %v", iface, err)
					return err
				}

				// reuse this function to delete qos record
				if err = ClearPodBandwidth(podName, podNamespace, iface); err != nil {
					klog.Errorf("failed to delete netemqos record for pod %s/%s: %v", podNamespace, podName, err)
					return err
				}
			}
		}
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
