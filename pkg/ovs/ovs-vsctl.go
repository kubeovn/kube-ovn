package ovs

import (
	"context"
	"fmt"
	"maps"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

var limiter *Limiter

func init() {
	limiter = new(Limiter)
}

func UpdateOVSVsctlLimiter(c int32) {
	if c >= 0 {
		limiter.Update(c)
		klog.V(4).Infof("update ovs-vsctl concurrency limit to %d", limiter.Limit())
	}
}

// Glory belongs to openvswitch/ovn-kubernetes
// https://github.com/openvswitch/ovn-kubernetes/blob/master/go-controller/pkg/util/ovs.go

var podNetNsRegexp = regexp.MustCompile(`pod_netns="([^"]+)"`)

func Exec(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var (
		start        time.Time
		elapsed      float64
		output       []byte
		method, code string
		err          error
	)

	if err = limiter.Wait(ctx); err != nil {
		klog.V(4).Infof("command %s %s waiting for execution timeout by concurrency limit of %d", OvsVsCtl, strings.Join(args, " "), limiter.Limit())
		return "", err
	}
	defer limiter.Done()
	klog.V(4).Infof("command %s %s waiting for execution concurrency %d/%d", OvsVsCtl, strings.Join(args, " "), limiter.Current(), limiter.Limit())

	start = time.Now()
	args = append([]string{"--timeout=30"}, args...)
	output, err = exec.Command(OvsVsCtl, args...).CombinedOutput()
	elapsed = float64((time.Since(start)) / time.Millisecond)
	klog.V(4).Infof("command %s %s in %vms", OvsVsCtl, strings.Join(args, " "), elapsed)

	for _, arg := range args {
		if !strings.HasPrefix(arg, "--") {
			method = arg
			break
		}
	}

	code = "0"
	defer func() {
		ovsClientRequestLatency.WithLabelValues("ovsdb", method, code).Observe(elapsed)
	}()

	if err != nil {
		code = "1"
		klog.Warningf("ovs-vsctl command error: %s %s in %vms", OvsVsCtl, strings.Join(args, " "), elapsed)
		return "", fmt.Errorf("failed to run '%s %s': %w\n  %q", OvsVsCtl, strings.Join(args, " "), err, output)
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

func Set(table, record string, values ...string) error {
	args := append([]string{"set", table, record}, values...)
	_, err := Exec(args...)
	return err
}

func ovsAdd(table, record, column string, values ...string) error {
	args := append([]string{"add", table, record, column}, values...)
	_, err := Exec(args...)
	return err
}

// Returns the given column of records that match the condition
func ovsFind(table, column string, conditions ...string) ([]string, error) {
	args := make([]string, len(conditions)+4)
	args[0], args[1], args[2], args[3] = "--no-heading", "--columns="+column, "find", table
	copy(args[4:], conditions)
	output, err := Exec(args...)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	ret := parseOvsFindOutput(output)
	return ret, nil
}

func parseOvsFindOutput(output string) []string {
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
	return ret
}

func Remove(table, record, column string, keys ...string) error {
	args := append([]string{"remove", table, record, column}, keys...)
	_, err := Exec(args...)
	return err
}

func ovsClear(table, record string, columns ...string) error {
	args := append([]string{"--if-exists", "clear", table, record}, columns...)
	_, err := Exec(args...)
	return err
}

func Get(table, record, column, key string, ifExists bool) (string, error) {
	var columnVal string
	if key == "" {
		columnVal = column
	} else {
		columnVal = column + ":" + key
	}
	args := []string{"get", table, record, columnVal}
	if ifExists {
		args = append([]string{"--if-exists"}, args...)
	}
	return Exec(args...)
}

// Bridges returns bridges created by Kube-OVN
func Bridges() ([]string, error) {
	return ovsFind("bridge", "name", "external-ids:vendor="+util.CniTypeName)
}

// BridgeExists checks whether the bridge already exists
func BridgeExists(name string) (bool, error) {
	bridges, err := Bridges()
	if err != nil {
		klog.Error(err)
		return false, err
	}
	return slices.Contains(bridges, name), nil
}

// PortExists checks whether the port already exists
func PortExists(name string) (bool, error) {
	result, err := ovsFind("port", "_uuid", "name="+name)
	if err != nil {
		klog.Errorf("failed to find port with name %s: %v", name, err)
		return false, err
	}
	return len(result) != 0, nil
}

func GetQosList(podName, podNamespace, ifaceID string) ([]string, error) {
	var qosList []string
	var err error

	if ifaceID != "" {
		qosList, err = ovsFind("qos", "_uuid", fmt.Sprintf(`external-ids:iface-id="%s"`, ifaceID))
		if err != nil {
			klog.Error(err)
			return qosList, err
		}
	} else {
		qosList, err = ovsFind("qos", "_uuid", fmt.Sprintf(`external-ids:pod="%s/%s"`, podNamespace, podName))
		if err != nil {
			klog.Error(err)
			return qosList, err
		}
	}

	return qosList, nil
}

// ClearPodBandwidth remove qos related to this pod.
func ClearPodBandwidth(podName, podNamespace, ifaceID string) error {
	qosList, err := GetQosList(podName, podNamespace, ifaceID)
	if err != nil {
		klog.Error(err)
		return err
	}

	// https://github.com/kubeovn/kube-ovn/issues/1191
	usedQosList, err := ovsFind("port", "qos", "qos!=[]")
	if err != nil {
		klog.Error(err)
		return err
	}

	for _, qosID := range qosList {
		found := slices.Contains(usedQosList, qosID)
		if found {
			continue
		}

		if err := ovsDestroy("qos", qosID); err != nil {
			klog.Error(err)
			return err
		}
	}
	return nil
}

var lastInterfacePodMap map[string]string

func ListInterfacePodMap() (map[string]string, error) {
	output, err := Exec("--data=bare", "--format=csv", "--no-heading", "--columns=name,error,external_ids", "find",
		"interface", "external_ids:pod_name!=[]", "external_ids:pod_namespace!=[]", "link_state!=up")
	if err != nil {
		klog.Errorf("failed to list interface, %v", err)
		return nil, err
	}
	lines := strings.Split(output, "\n")
	result := make(map[string]string, len(lines))
	for _, l := range lines {
		if len(strings.TrimSpace(l)) == 0 {
			continue
		}
		parts := strings.SplitN(strings.TrimSpace(l), ",", 3)
		if len(parts) != 3 {
			continue
		}
		ifaceName := strings.TrimSpace(parts[0])
		errText := strings.TrimSpace(parts[1])
		var podNamespace, podName string
		for externalID := range strings.FieldsSeq(parts[2]) {
			if strings.Contains(externalID, "pod_name=") {
				podName = strings.TrimPrefix(strings.TrimSpace(externalID), "pod_name=")
			}

			if strings.Contains(externalID, "pod_namespace=") {
				podNamespace = strings.TrimPrefix(strings.TrimSpace(externalID), "pod_namespace=")
			}
		}
		result[ifaceName] = fmt.Sprintf("%s/%s/%s", podNamespace, podName, errText)
	}
	if !maps.Equal(result, lastInterfacePodMap) {
		klog.Infof("interface pod map: %v", result)
		lastInterfacePodMap = maps.Clone(result)
	}
	return result, nil
}

func CleanInterface(name string) error {
	qosList, err := ovsFind("port", "qos", "name="+name)
	if err != nil {
		klog.Errorf("failed to find related port %v", err)
		return err
	}
	klog.Infof("delete lost port %s", name)
	output, err := Exec("--if-exists", "--with-iface", "del-port", name)
	if err != nil {
		klog.Errorf("failed to delete ovs port %v, %s", err, output)
		return err
	}
	for _, qos := range qosList {
		qos = strings.TrimSpace(qos)
		if qos != "" && qos != "[]" {
			klog.Infof("delete lost qos %s", qos)
			err = ovsDestroy("qos", qos)
			if err != nil {
				klog.Errorf("failed to delete qos %s, %v", qos, err)
				return err
			}
		}
	}
	return nil
}

// Find and remove any existing OVS port with this iface-id. Pods can
// have multiple sandboxes if some are waiting for garbage collection,
// but only the latest one should have the iface-id set.
// See: https://github.com/ovn-org/ovn-kubernetes/pull/869
func CleanDuplicatePort(ifaceID, portName string) {
	uuids, _ := ovsFind("Interface", "_uuid", "external-ids:iface-id="+ifaceID, "name!="+portName)
	for _, uuid := range uuids {
		if out, err := Exec("remove", "Interface", uuid, "external-ids", "iface-id"); err != nil {
			klog.Errorf("failed to clear stale OVS port %q iface-id %q: %v\n  %q", uuid, ifaceID, err, out)
		}
	}
}

// ValidatePortVendor returns true if the port's external_ids:vendor=kube-ovn
func ValidatePortVendor(port string) (bool, error) {
	output, err := ovsFind("Port", "name", "external_ids:vendor="+util.CniTypeName)
	return slices.Contains(output, port), err
}

func GetInterfacePodNs(iface string) (string, error) {
	ret, err := ovsFind("interface", "external-ids", "external-ids:iface-id="+iface)
	if err != nil {
		klog.Error(err)
		return "", err
	}

	if len(ret) == 0 {
		return "", nil
	}

	podNetNs := ""
	match := podNetNsRegexp.FindStringSubmatch(ret[0])
	if len(match) > 1 {
		podNetNs = match[1]
	}

	return podNetNs, nil
}

// config mirror for interface by pod annotations and install param
func ConfigInterfaceMirror(globalMirror bool, open, iface string) error {
	if globalMirror {
		return nil
	}
	// find interface name for port
	interfaceList, err := ovsFind("interface", "name", "external-ids:iface-id="+iface)
	if err != nil {
		klog.Error(err)
		return err
	}
	for _, ifName := range interfaceList {
		// ifName example: xxx_h
		// find port uuid by interface name
		portUUIDs, err := ovsFind("port", "_uuid", "name="+ifName)
		if err != nil {
			klog.Error(err)
			return err
		}
		if len(portUUIDs) != 1 {
			return fmt.Errorf("find port failed, portName=%s", ifName)
		}
		portID := portUUIDs[0]
		if open == "true" {
			// add port to mirror
			err = ovsAdd("mirror", util.MirrorDefaultName, "select_dst_port", portID)
			if err != nil {
				klog.Error(err)
				return err
			}
		} else {
			mirrorPorts, err := ovsFind("mirror", "select_dst_port", "name="+util.MirrorDefaultName)
			if err != nil {
				klog.Error(err)
				return err
			}
			if len(mirrorPorts) == 0 {
				return fmt.Errorf("find mirror failed, mirror name=%s", util.MirrorDefaultName)
			}
			if len(mirrorPorts) > 1 {
				return fmt.Errorf("repeated mirror data, mirror name=%s", util.MirrorDefaultName)
			}
			for _, mirrorPortIDs := range mirrorPorts {
				if strings.Contains(mirrorPortIDs, portID) {
					// remove port from mirror
					_, err := Exec("remove", "mirror", util.MirrorDefaultName, "select_dst_port", portID)
					if err != nil {
						klog.Error(err)
						return err
					}
				}
			}
		}
	}
	return nil
}

// remove qos related to this port.
func ClearPortQosBinding(ifaceID string) error {
	interfaceList, err := ovsFind("interface", "name", fmt.Sprintf(`external-ids:iface-id="%s"`, ifaceID))
	if err != nil {
		klog.Error(err)
		return err
	}

	for _, ifName := range interfaceList {
		if err = ovsClear("port", ifName, "qos"); err != nil {
			klog.Error(err)
			return err
		}
	}
	return nil
}

func ListExternalIDs(table string) (map[string]string, error) {
	output, err := Exec("--data=bare", "--format=csv", "--no-heading", "--columns=_uuid,external_ids", "find", table, "external_ids:iface-id!=[]")
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
		for externalID := range strings.FieldsSeq(parts[1]) {
			if !strings.Contains(externalID, "iface-id=") {
				continue
			}
			iface := strings.TrimPrefix(strings.TrimSpace(externalID), "iface-id=")
			result[iface] = uuid
			break
		}
	}
	return result, nil
}

func ListQosQueueIDs() (map[string]string, error) {
	output, err := Exec("--data=bare", "--format=csv", "--no-heading", "--columns=_uuid,queues", "find", "qos", "queues:0!=[]")
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
		qosID := strings.TrimSpace(parts[0])
		if !strings.Contains(strings.TrimSpace(parts[1]), "0=") {
			continue
		}
		queueID := strings.TrimPrefix(strings.TrimSpace(parts[1]), "0=")
		result[qosID] = queueID
	}
	return result, nil
}

// SetInterfaceBandwidth set ingress/egress qos for given pod, annotation values are for node/pod
// but ingress/egress parameters here are from the point of ovs port/interface view, so reverse input parameters when call func SetInterfaceBandwidth
func SetInterfaceBandwidth(podName, podNamespace, iface, ingress, egress string) error {
	ingressMPS, _ := strconv.Atoi(ingress)
	ingressKPS := ingressMPS * 1000
	interfaceList, err := ovsFind("interface", "name", "external-ids:iface-id="+iface)
	if err != nil {
		klog.Error(err)
		return err
	}

	qosIfaceUIDMap, err := ListExternalIDs("qos")
	if err != nil {
		klog.Error(err)
		return err
	}

	queueIfaceUIDMap, err := ListExternalIDs("queue")
	if err != nil {
		klog.Error(err)
		return err
	}

	for _, ifName := range interfaceList {
		// ingress_policing_rate is in Kbps
		err := Set("interface", ifName, fmt.Sprintf("ingress_policing_rate=%d", ingressKPS), fmt.Sprintf("ingress_policing_burst=%d", ingressKPS*8/10))
		if err != nil {
			klog.Error(err)
			return err
		}

		egressMPS, _ := strconv.Atoi(egress)
		egressBPS := egressMPS * 1000 * 1000

		if egressBPS > 0 {
			queueUID, err := SetHtbQosQueueRecord(podName, podNamespace, iface, egressBPS, queueIfaceUIDMap)
			if err != nil {
				klog.Error(err)
				return err
			}

			if err = SetQosQueueBinding(podName, podNamespace, ifName, iface, queueUID, qosIfaceUIDMap); err != nil {
				klog.Error(err)
				return err
			}
		} else {
			if qosUID, ok := qosIfaceUIDMap[iface]; ok {
				qosType, err := Get("qos", qosUID, "type", "", false)
				if err != nil {
					klog.Error(err)
					return err
				}
				if qosType != util.HtbQos {
					continue
				}
				queueID, err := Get("qos", qosUID, "queues", "0", false)
				if err != nil {
					klog.Error(err)
					return err
				}

				if _, err := Exec("remove", "queue", queueID, "other_config", "max-rate"); err != nil {
					klog.Error(err)
					return fmt.Errorf("failed to remove rate limit for queue in pod %v/%v, %w", podNamespace, podName, err)
				}
			}
		}

		// Delete Qos and Queue record if both bandwidth and priority do not exist
		if err = CheckAndUpdateHtbQos(podName, podNamespace, iface, queueIfaceUIDMap); err != nil {
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
			klog.Error(err)
			return err
		}
	} else {
		queueList, err = ovsFind("queue", "_uuid", fmt.Sprintf(`external-ids:pod="%s/%s"`, podNamespace, podName))
		if err != nil {
			klog.Error(err)
			return err
		}
	}

	// https://github.com/kubeovn/kube-ovn/issues/1191
	qosQueueMap, err := ListQosQueueIDs()
	if err != nil {
		klog.Error(err)
		return err
	}

	for _, queueID := range queueList {
		found := false
		for _, usedQueueID := range qosQueueMap {
			if queueID == usedQueueID {
				found = true
				break
			}
		}
		if found {
			continue
		}

		if err := ovsDestroy("queue", queueID); err != nil {
			klog.Error(err)
			return err
		}
	}
	return nil
}

func IsHtbQos(iface string) (bool, error) {
	qosType, err := ovsFind("qos", "type", fmt.Sprintf(`external-ids:iface-id="%s"`, iface))
	if err != nil {
		klog.Error(err)
		return false, err
	}

	if len(qosType) != 0 && qosType[0] == util.HtbQos {
		return true, nil
	}
	return false, nil
}

func SetHtbQosQueueRecord(podName, podNamespace, iface string, maxRateBPS int, queueIfaceUIDMap map[string]string) (string, error) {
	var queueCommandValues []string
	var err error
	if maxRateBPS > 0 {
		queueCommandValues = append(queueCommandValues, fmt.Sprintf("other_config:max-rate=%d", maxRateBPS))
	}

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
func SetNetemQos(podName, podNamespace, iface, latency, limit, loss, jitter string) error {
	latencyMs, _ := strconv.Atoi(latency)
	latencyUs := latencyMs * 1000
	jitterMs, _ := strconv.Atoi(jitter)
	jitterUs := jitterMs * 1000
	limitPkts, _ := strconv.Atoi(limit)
	lossPercent, _ := strconv.ParseFloat(loss, 64)

	interfaceList, err := ovsFind("interface", "name", "external-ids:iface-id="+iface)
	if err != nil {
		klog.Error(err)
		return err
	}

	for _, ifName := range interfaceList {
		qosList, err := GetQosList(podName, podNamespace, iface)
		if err != nil {
			klog.Error(err)
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
					klog.Error(err)
					return err
				}

				if err = Set("port", ifName, "qos="+qos); err != nil {
					klog.Error(err)
					return err
				}
			} else {
				for _, qos := range qosList {
					qosType, err := Get("qos", qos, "type", "", false)
					if err != nil {
						klog.Error(err)
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

					if err = deleteNetemQosByID(qos, iface, podName, podNamespace); err != nil {
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

					if err = Set("port", ifName, "qos="+qos); err != nil {
						klog.Errorf("failed to set netem qos to port: %v", err)
						return err
					}
				}
			}
		} else {
			for _, qos := range qosList {
				if err := deleteNetemQosByID(qos, iface, podName, podNamespace); err != nil {
					klog.Errorf("failed to delete netem qos: %v", err)
					return err
				}
			}
		}
	}
	return nil
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
		klog.Error(err)
		return false, err
	}
	return len(dp) > 0 && dp[0] == "netdev", nil
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
