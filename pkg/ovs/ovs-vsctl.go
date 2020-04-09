package ovs

import (
	"fmt"
	"k8s.io/klog"
	"os/exec"
	"strconv"
	"strings"
)

// Glory belongs to openvswitch/ovn-kubernetes
// https://github.com/openvswitch/ovn-kubernetes/blob/master/go-controller/pkg/util/ovs.go

func ovsExec(args ...string) (string, error) {
	args = append([]string{"--timeout=30"}, args...)
	output, err := exec.Command("ovs-vsctl", args...).CombinedOutput()

	if err != nil {
		return "", fmt.Errorf("failed to run 'ovs-vsctl %s': %v\n  %q", strings.Join(args, " "), err, output)
	}

	outStr := string(output)
	trimmed := strings.TrimSpace(outStr)
	// If output is a single line, strip the trailing newline
	if strings.Count(trimmed, "\n") == 0 {
		outStr = trimmed
	}
	return outStr, nil
}

func ovsCreate(table string, values ...string) (string, error) {
	args := append([]string{"create", table}, values...)
	return ovsExec(args...)
}

func ovsDestroy(table, record string) error {
	_, err := ovsExec("--if-exists", "destroy", table, record)
	return err
}

func ovsSet(table, record string, values ...string) error {
	args := append([]string{"set", table, record}, values...)
	_, err := ovsExec(args...)
	return err
}

// Returns the given column of records that match the condition
func ovsFind(table, column, condition string) ([]string, error) {
	output, err := ovsExec("--no-heading", "--columns="+column, "find", table, condition)
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
	_, err := ovsExec(args...)
	return err
}

// ClearPodBandwidth remove qos related to this pod. Only used when remove pod.
func ClearPodBandwidth(podName, podNamespace string) error {
	qosList, err := ovsFind("qos", "_uuid", fmt.Sprintf(`external-ids:iface-id="%s.%s"`, podName, podNamespace))
	if err != nil {
		return err
	}
	for _, qos := range qosList {
		if err := ovsDestroy("qos", qos); err != nil {
			return err
		}
	}
	return nil
}

// SetPodBandwidth set ingress/egress qos for given pod
func SetPodBandwidth(podName, podNamespace, ingress, egress string) error {
	ingressMPS, _ := strconv.Atoi(ingress)
	ingressKPS := ingressMPS * 1000
	interfaceList, err := ovsFind("interface", "name", fmt.Sprintf("external-ids:iface-id=%s.%s", podName, podNamespace))
	if err != nil {
		return err
	}

	for _, ifName := range interfaceList {
		// ingress_policing_rate is in Kbps
		err := ovsSet("interface", ifName, fmt.Sprintf("ingress_policing_rate=%d", ingressKPS), fmt.Sprintf("ingress_policing_burst=%d", ingressKPS*10/8))
		if err != nil {
			return err
		}

		egressMPS, _ := strconv.Atoi(egress)
		egressBPS := egressMPS * 1000 * 1000

		qosList, err := ovsFind("qos", "_uuid", fmt.Sprintf("external-ids:iface-id=%s.%s", podName, podNamespace))
		if err != nil {
			return err
		}
		if egressBPS > 0 {
			if len(qosList) == 0 {
				qos, err := ovsCreate("qos", "type=linux-htb", fmt.Sprintf("other-config:max-rate=%d", egressBPS), fmt.Sprintf("external-ids:iface-id=%s.%s", podName, podNamespace))
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
			output, err := exec.Command("ovs-vsctl", "--if-exists", "--with-iface", "del-port", "br-int", name).CombinedOutput()
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
		if out, err := ovsExec("remove", "Interface", uuid, "external-ids", "iface-id"); err != nil {
			klog.Errorf("failed to clear stale OVS port %q iface-id %q: %v\n  %q", uuid, ifaceID, err, out)
		}
	}
}

func SetPortTag(port, tag string) error {
	return ovsSet("port", port, fmt.Sprintf("tag=%s", tag))
}
