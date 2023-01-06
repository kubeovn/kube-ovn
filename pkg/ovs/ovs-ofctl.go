package ovs

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"k8s.io/klog/v2"
)

// Glory belongs to openvswitch/ovn-kubernetes
// https://github.com/openvswitch/ovn-kubernetes/blob/master/go-controller/pkg/util/ovs.go

func OvsExec(args ...string) (string, error) {
	start := time.Now()
	args = append([]string{"--timeout=30"}, args...)
	output, err := exec.Command(OvsOfCtl, args...).CombinedOutput()
	elapsed := float64((time.Since(start)) / time.Millisecond)
	klog.V(4).Infof("command %s %s in %vms", OvsOfCtl, strings.Join(args, " "), elapsed)
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
		klog.Warningf("ovs-ofctl command error: %s %s in %vms", OvsOfCtl, strings.Join(args, " "), elapsed)
		return "", fmt.Errorf("failed to run '%s %s': %v\n  %q", OvsOfCtl, strings.Join(args, " "), err, output)
	} else if elapsed > 500 {
		klog.Warningf("ovs-ofctl command took too long: %s %s in %vms", OvsOfCtl, strings.Join(args, " "), elapsed)
	}
	return trimCommandOutput(output), nil
}

func ParseDumpFlowsOutput(output string) []string {
	if output == "" {
		return []string{}
	}

	lines := strings.Split(output, "\n")
	marks := make([]string, 0, len(lines))
	for _, l := range lines {
		if len(strings.TrimSpace(l)) == 0 {
			continue
		}

		if !strings.Contains(l, "pkt_mark") && !strings.Contains(l, "mod_vlan_pcp") {
			continue
		}

		fields := strings.Fields(l)
		for _, field := range fields {
			values := strings.Split(field, "=")
			if len(values) != 2 {
				continue
			}

			if values[0] == "pkt_mark" {
				marks = append(marks, strings.TrimSpace(values[1]))
			}
		}
	}
	return marks
}
