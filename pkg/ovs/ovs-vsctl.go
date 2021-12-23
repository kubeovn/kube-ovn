package ovs

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

// Glory belongs to openvswitch/ovn-kubernetes
// https://github.com/openvswitch/ovn-kubernetes/blob/master/go-controller/pkg/util/ovs.go

func Exec(args ...string) (string, error) {
	start := time.Now()
	args = append([]string{"--timeout=30"}, args...)
	output, err := exec.Command(OvsVsCtl, args...).CombinedOutput()
	elapsed := float64((time.Since(start)) / time.Millisecond)
	klog.V(4).Infof("command %s %s in %vms", OvsVsCtl, strings.Join(args, " "), elapsed)
	method := ""
	for _, arg := range args {
		if !strings.HasPrefix(arg, "--") {
			method = arg
			break
		}
	}
	code := "0"
	defer func() {
		ovsClientRequestLatency.WithLabelValues("ovsdb", method, code).Observe(elapsed)
	}()

	if err != nil {
		code = "1"
		klog.Warningf("ovs-vsctl command error: %s %s in %vms", OvsVsCtl, strings.Join(args, " "), elapsed)
		return "", fmt.Errorf("failed to run '%s %s': %v\n  %q", OvsVsCtl, strings.Join(args, " "), err, output)
	} else if elapsed > 500 {
		klog.Warningf("ovs-vsctl command took too long: %s %s in %vms", OvsVsCtl, strings.Join(args, " "), elapsed)
	}
	return trimCommandOutput(output), nil
}

func ovsCreate(table string, values ...string) (string, error) {
	args := append([]string{"create", table}, values...)
	return Exec(args...)
}

func ovsDestroy(table, record string) error {
	_, err := Exec("--if-exists", "destroy", table, record)
	return err
}

func ovsSet(table, record string, values ...string) error {
	args := append([]string{"set", table, record}, values...)
	_, err := Exec(args...)
	return err
}

func ovsAdd(table, record string, column string, values ...string) error {
	args := append([]string{"add", table, record, column}, values...)
	_, err := Exec(args...)
	return err
}

// Returns the given column of records that match the condition
func ovsFind(table, column, condition string) ([]string, error) {
	output, err := Exec("--no-heading", "--columns="+column, "find", table, condition)
	if err != nil {
		return nil, err
	}
	values := strings.Split(output, "\n\n")
	// We want "bare" values for strings, but we can't pass --bare to ovs-vsctl because
	// it breaks more complicated types. So try passing each value through Unquote();
	// if it fails, that means the value wasn't a quoted string, so use it as-is.
	for i, val := range values {
		if unquoted, err := strconv.Unquote(val); err == nil {
			values[i] = unquoted
		}
	}
	ret := make([]string, 0, len(values))
	for _, val := range values {
		if strings.TrimSpace(val) != "" {
			ret = append(ret, strings.Trim(strings.TrimSpace(val), "\""))
		}
	}
	return ret, nil
}

func ovsClear(table, record string, columns ...string) error {
	args := append([]string{"--if-exists", "clear", table, record}, columns...)
	_, err := Exec(args...)
	return err
}

func ovsGet(table, record, column, key string) (string, error) {
	var columnVal string
	if key == "" {
		columnVal = column
	} else {
		columnVal = column + ":" + key
	}
	args := []string{"get", table, record, columnVal}
	return Exec(args...)
}

func ovsRemove(table, record, column, key string) error {
	args := []string{"remove"}
	if key == "" {
		args = append(args, table, record, column)
	} else {
		args = append(args, table, record, column, key)
	}
	_, err := Exec(args...)
	return err
}

// Bridges returns bridges created by Kube-OVN
func Bridges() ([]string, error) {
	return ovsFind("bridge", "name", fmt.Sprintf("external-ids:vendor=%s", util.CniTypeName))
}

func GetQosList(podName, podNamespace, ifaceID string) ([]string, error) {
	var qosList []string
	var err error

	if ifaceID != "" {
		qosList, err = ovsFind("qos", "_uuid", fmt.Sprintf(`external-ids:iface-id="%s"`, ifaceID))
		if err != nil {
			return qosList, err
		}
	} else {
		qosList, err = ovsFind("qos", "_uuid", fmt.Sprintf(`external-ids:pod="%s/%s"`, podNamespace, podName))
		if err != nil {
			return qosList, err
		}
	}

	return qosList, nil
}

// ClearPodBandwidth remove qos related to this pod.
func ClearPodBandwidth(podName, podNamespace, ifaceID string) error {
	qosList, err := GetQosList(podName, podNamespace, ifaceID)
	if err != nil {
		return err
	}

	// https://github.com/kubeovn/kube-ovn/issues/1191
	usedQosList, err := ovsFind("port", "qos", "qos!=[]")
	if err != nil {
		return err
	}

	for _, qosId := range qosList {
		found := false
		for _, usedQosId := range usedQosList {
			if qosId == usedQosId {
				found = true
				break
			}
		}
		if found {
			continue
		}

		if err := ovsDestroy("qos", qosId); err != nil {
			return err
		}
	}
	return nil
}

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

// CleanLostInterface will clean up related ovs port, interface and qos
// When reboot node, the ovs internal interface will be deleted.
func CleanLostInterface() {
	// when interface error ofport will be -1
	interfaceList, err := ovsFind("interface", "name,error", "ofport=-1")
	if err != nil {
		klog.Errorf("failed to list failed interface %v", err)
		return
	}
	if len(interfaceList) > 0 {
		klog.Infof("error interfaces:\n %v", interfaceList)
	}

	for _, intf := range interfaceList {
		name, errText := strings.Trim(strings.Split(intf, "\n")[0], "\""), strings.Split(intf, "\n")[1]
		if strings.Contains(errText, "No such device") {
			qosList, err := ovsFind("port", "qos", fmt.Sprintf("name=%s", name))
			if err != nil {
				klog.Errorf("failed to find related port %v", err)
				return
			}
			klog.Infof("delete lost port %s", name)
			output, err := Exec("--if-exists", "--with-iface", "del-port", "br-int", name)
			if err != nil {
				klog.Errorf("failed to delete ovs port %v, %s", err, output)
				return
			}
			for _, qos := range qosList {
				qos = strings.TrimSpace(qos)
				if qos != "" && qos != "[]" {
					klog.Infof("delete lost qos %s", qos)
					err = ovsDestroy("qos", qos)
					if err != nil {
						klog.Errorf("failed to delete qos %s, %v", qos, err)
						return
					}
				}
			}
		}
	}
}

// Find and remove any existing OVS port with this iface-id. Pods can
// have multiple sandboxes if some are waiting for garbage collection,
// but only the latest one should have the iface-id set.
// See: https://github.com/ovn-org/ovn-kubernetes/pull/869
func CleanDuplicatePort(ifaceID string) {
	uuids, _ := ovsFind("Interface", "_uuid", "external-ids:iface-id="+ifaceID)
	for _, uuid := range uuids {
		if out, err := Exec("remove", "Interface", uuid, "external-ids", "iface-id"); err != nil {
			klog.Errorf("failed to clear stale OVS port %q iface-id %q: %v\n  %q", uuid, ifaceID, err, out)
		}
	}
}

func SetPortTag(port, tag string) error {
	return ovsSet("port", port, fmt.Sprintf("tag=%s", tag))
}

// ValidatePortVendor returns true if the port's external_ids:vendor=kube-ovn
func ValidatePortVendor(port string) (bool, error) {
	output, err := ovsFind("Port", "name", "external_ids:vendor="+util.CniTypeName)
	return util.ContainsString(output, port), err
}

//config mirror for interface by pod annotations and install param
func ConfigInterfaceMirror(globalMirror bool, open string, iface string) error {
	if !globalMirror {
		//find interface name for port
		interfaceList, err := ovsFind("interface", "name", fmt.Sprintf("external-ids:iface-id=%s", iface))
		if err != nil {
			return err
		}
		for _, ifName := range interfaceList {
			//ifName example: xxx_h
			//find port uuid by interface name
			portUUIDs, err := ovsFind("port", "_uuid", fmt.Sprintf("name=%s", ifName))
			if err != nil {
				return err
			}
			if len(portUUIDs) != 1 {
				return fmt.Errorf(fmt.Sprintf("find port failed, portName=%s", ifName))
			}
			portId := portUUIDs[0]
			if open == "true" {
				//add port to mirror
				err = ovsAdd("mirror", util.MirrorDefaultName, "select_dst_port", portId)
				if err != nil {
					return err
				}
			} else {
				mirrorPorts, err := ovsFind("mirror", "select_dst_port", fmt.Sprintf("name=%s", util.MirrorDefaultName))
				if err != nil {
					return err
				}
				if len(mirrorPorts) == 0 {
					return fmt.Errorf("find mirror failed, mirror name=" + util.MirrorDefaultName)
				}
				if len(mirrorPorts) > 1 {
					return fmt.Errorf("repeated mirror data, mirror name=" + util.MirrorDefaultName)
				}
				for _, mirrorPortIds := range mirrorPorts {
					if strings.Contains(mirrorPortIds, portId) {
						//remove port from mirror
						_, err := Exec("remove", "mirror", util.MirrorDefaultName, "select_dst_port", portId)
						if err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

func GetResidualInternalPorts() []string {
	residualPorts := make([]string, 0)
	interfaceList, err := ovsFind("interface", "name,external_ids", "type=internal")
	if err != nil {
		klog.Errorf("failed to list ovs internal interface %v", err)
		return residualPorts
	}

	for _, intf := range interfaceList {
		name := strings.Trim(strings.Split(intf, "\n")[0], "\"")
		if !strings.Contains(name, "_c") {
			continue
		}

		// iface-id field does not exist in external_ids for residual internal port
		externaIds := strings.Split(intf, "\n")[1]
		if !strings.Contains(externaIds, "iface-id") {
			residualPorts = append(residualPorts, name)
		}
	}
	return residualPorts
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

// remove qos related to this port.
func ClearPortQosBinding(ifaceID string) error {
	interfaceList, err := ovsFind("interface", "name", fmt.Sprintf(`external-ids:iface-id="%s"`, ifaceID))
	if err != nil {
		return err
	}

	for _, ifName := range interfaceList {
		if err = ovsClear("port", ifName, "qos"); err != nil {
			return err
		}
	}
	return nil
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

func ListExternalIds(table string) (map[string]string, error) {
	args := []string{"--data=bare", "--format=csv", "--no-heading", "--columns=_uuid,external_ids", "find", table, "external_ids:iface-id!=[]"}
	output, err := Exec(args...)
	if err != nil {
		klog.Errorf("failed to list %s, %v", table, err)
		return nil, err
	}
	lines := strings.Split(output, "\n")
	result := make(map[string]string, len(lines))
	for _, l := range lines {
		if len(strings.TrimSpace(l)) == 0 {
			continue
		}
		parts := strings.Split(strings.TrimSpace(l), ",")
		if len(parts) != 2 {
			continue
		}
		uuid := strings.TrimSpace(parts[0])
		externalIds := strings.Fields(parts[1])
		for _, externalId := range externalIds {
			if !strings.Contains(externalId, "iface-id=") {
				continue
			}
			iface := strings.TrimPrefix(strings.TrimSpace(externalId), "iface-id=")
			result[iface] = uuid
			break
		}
	}
	return result, nil
}

func ListQosQueueIds() (map[string]string, error) {
	args := []string{"--data=bare", "--format=csv", "--no-heading", "--columns=_uuid,queues", "find", "qos", "queues:0!=[]"}
	output, err := Exec(args...)
	if err != nil {
		klog.Errorf("failed to list qos, %v", err)
		return nil, err
	}
	lines := strings.Split(output, "\n")
	result := make(map[string]string, len(lines))
	for _, l := range lines {
		if len(strings.TrimSpace(l)) == 0 {
			continue
		}
		parts := strings.Split(strings.TrimSpace(l), ",")
		if len(parts) != 2 {
			continue
		}
		qosId := strings.TrimSpace(parts[0])
		if !strings.Contains(strings.TrimSpace(parts[1]), "0=") {
			continue
		}
		queueId := strings.TrimPrefix(strings.TrimSpace(parts[1]), "0=")
		result[qosId] = queueId
	}
	return result, nil
}
