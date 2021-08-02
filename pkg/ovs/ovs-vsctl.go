package ovs

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"k8s.io/klog"
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

// ClearPodBandwidth remove qos related to this pod. Only used when remove pod.
func ClearPodBandwidth(podName, podNamespace string) error {
	qosList, err := ovsFind("qos", "_uuid", fmt.Sprintf(`external-ids:iface-id="%s.%s"`, podName, podNamespace))
	if err != nil {
		return err
	}
	qosListByPod, err := ovsFind("qos", "_uuid", fmt.Sprintf(`external-ids:pod="%s/%s"`, podNamespace, podName))
	if err != nil {
		return err
	}
	qosList = append(qosList, qosListByPod...)
	qosList = util.UniqString(qosList)
	for _, qos := range qosList {
		if err := ovsDestroy("qos", qos); err != nil {
			return err
		}
	}
	return nil
}

// SetInterfaceBandwidth set ingress/egress qos for given pod
func SetInterfaceBandwidth(podName, podNamespace, iface, ingress, egress string) error {
	ingressMPS, _ := strconv.Atoi(ingress)
	ingressKPS := ingressMPS * 1000
	interfaceList, err := ovsFind("interface", "name", fmt.Sprintf("external-ids:iface-id=%s", iface))
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

		qosList, err := ovsFind("qos", "_uuid", fmt.Sprintf("external-ids:iface-id=%s", iface))
		if err != nil {
			return err
		}
		if egressBPS > 0 {
			if len(qosList) == 0 {
				qosCommandValues := []string{"type=linux-htb", fmt.Sprintf("other-config:max-rate=%d", egressBPS), fmt.Sprintf("external-ids:iface-id=%s", iface)}
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
					klog.Infof("qos %s already exists", qos)
					if err := ovsSet("qos", qos, fmt.Sprintf("other-config:max-rate=%d", egressBPS)); err != nil {
						return err
					}
				}
			}
		} else {
			if err = ovsClear("port", ifName, "qos"); err != nil {
				return err
			}
			for _, qos := range qosList {
				if err := ovsDestroy("qos", qos); err != nil {
					return err
				}
			}
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
