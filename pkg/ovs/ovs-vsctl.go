package ovs

import (
	"context"
	"fmt"
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

// Bridges returns bridges created by Kube-OVN
func Bridges() ([]string, error) {
	return ovsFind("bridge", "name", fmt.Sprintf("external-ids:vendor=%s", util.CniTypeName))
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
		found := false
		for _, usedQosID := range usedQosList {
			if qosID == usedQosID {
				found = true
				break
			}
		}
		if found {
			continue
		}

		if err := ovsDestroy("qos", qosID); err != nil {
			return err
		}
	}
	return nil
}

// CleanLostInterface will clean up related ovs port, interface and qos
// When reboot node, the ovs internal interface will be deleted.
func CleanLostInterface() {
	// when interface error ofport will be -1
	interfaceList, err := ovsFind("interface", "name,error", "ofport=-1", "external_ids:pod_netns!=[]")
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
			output, err := Exec("--if-exists", "--with-iface", "del-port", name)
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
func CleanDuplicatePort(ifaceID, portName string) {
	uuids, _ := ovsFind("Interface", "_uuid", "external-ids:iface-id="+ifaceID, "name!="+portName)
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
	return slices.Contains(output, port), err
}

func GetInterfacePodNs(iface string) (string, error) {
	ret, err := ovsFind("interface", "external-ids", fmt.Sprintf("external-ids:iface-id=%s", iface))
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
	interfaceList, err := ovsFind("interface", "name", fmt.Sprintf("external-ids:iface-id=%s", iface))
	if err != nil {
		klog.Error(err)
		return err
	}
	for _, ifName := range interfaceList {
		// ifName example: xxx_h
		// find port uuid by interface name
		portUUIDs, err := ovsFind("port", "_uuid", fmt.Sprintf("name=%s", ifName))
		if err != nil {
			klog.Error(err)
			return err
		}
		if len(portUUIDs) != 1 {
			return fmt.Errorf(fmt.Sprintf("find port failed, portName=%s", ifName))
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
			mirrorPorts, err := ovsFind("mirror", "select_dst_port", fmt.Sprintf("name=%s", util.MirrorDefaultName))
			if err != nil {
				klog.Error(err)
				return err
			}
			if len(mirrorPorts) == 0 {
				return fmt.Errorf("find mirror failed, mirror name=" + util.MirrorDefaultName)
			}
			if len(mirrorPorts) > 1 {
				return fmt.Errorf("repeated mirror data, mirror name=" + util.MirrorDefaultName)
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
		externalIDs := strings.Split(intf, "\n")[1]
		if !strings.Contains(externalIDs, "iface-id") {
			residualPorts = append(residualPorts, name)
		}
	}
	return residualPorts
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
			return err
		}
	}
	return nil
}

func ListExternalIDs(table string) (map[string]string, error) {
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
		externalIDs := strings.Fields(parts[1])
		for _, externalID := range externalIDs {
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
		qosID := strings.TrimSpace(parts[0])
		if !strings.Contains(strings.TrimSpace(parts[1]), "0=") {
			continue
		}
		queueID := strings.TrimPrefix(strings.TrimSpace(parts[1]), "0=")
		result[qosID] = queueID
	}
	return result, nil
}
