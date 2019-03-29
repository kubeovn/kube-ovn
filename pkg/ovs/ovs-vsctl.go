package ovs

import (
	"fmt"
	"k8s.io/klog"
	"os/exec"
	"strconv"
	"strings"
)

func ovsExec(args ...string) (string, error) {
	args = append([]string{"--timeout=30"}, args...)
	output, err := exec.Command("ovs-vsctl", args...).CombinedOutput()

	if err != nil {
		return "", fmt.Errorf("failed to run 'ovs-vsctl %s': %v\n  %q", strings.Join(args, " "), err, string(output))
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
			ret = append(ret, strings.TrimSpace(val))
		}
	}
	return ret, nil
}

func ovsClear(table, record string, columns ...string) error {
	args := append([]string{"--if-exists", "clear", table, record}, columns...)
	_, err := ovsExec(args...)
	return err
}

func ClearPodBandwidth(podName, podNamespace string) error {
	// interfaces will have the same name as ports
	interfaceList, err := ovsFind("interface", "name", fmt.Sprintf("external-ids:iface-id=%s.%s", podName, podNamespace))
	if err != nil {
		return err
	}

	// Clear the QoS for any ports of this sandbox
	for _, ifName := range interfaceList {
		if err = ovsClear("port", ifName, "qos"); err != nil {
			return err
		}
	}

	// Now that the QoS is unused remove it
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

func SetPodBandwidth(podName, podNamespace, ingress, egress string) error {
	ingressMPS, _ := strconv.Atoi(ingress)
	ingressKPS := ingressMPS * 1000
	interfaceList, err := ovsFind("interface", "name", fmt.Sprintf("external-ids:iface-id=%s.%s", podName, podNamespace))
	if err != nil {
		return err
	}

	for _, ifName := range interfaceList {
		// ingress_policing_rate is in Kbps
		err := ovsSet("interface", ifName, fmt.Sprintf("ingress_policing_rate=%d", ingressKPS))
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
