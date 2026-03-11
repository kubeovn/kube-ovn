package ovs

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c LegacyClient) ovnIcNbCommand(cmdArgs ...string) (string, error) {
	start := time.Now()
	cmdArgs = append([]string{fmt.Sprintf("--timeout=%d", c.OvnTimeout), "--db=" + c.OvnICNbAddress}, cmdArgs...)
	raw, err := exec.Command(OVNIcNbCtl, cmdArgs...).CombinedOutput()
	elapsed := float64((time.Since(start)) / time.Millisecond)
	klog.V(4).Infof("command %s %s in %vms", OVNIcNbCtl, strings.Join(cmdArgs, " "), elapsed)
	method := ""
	for _, arg := range cmdArgs {
		if !strings.HasPrefix(arg, "--") {
			method = arg
			break
		}
	}
	code := "0"
	defer func() {
		ovsClientRequestLatency.WithLabelValues("ovn-ic-nb", method, code).Observe(elapsed)
	}()

	if err != nil {
		code = "1"
		klog.Warningf("ovn-ic-nbctl command error: %s %s in %vms", OVNIcNbCtl, strings.Join(cmdArgs, " "), elapsed)
		return "", fmt.Errorf("%s, %w", raw, err)
	} else if elapsed > 500 {
		klog.Warningf("ovn-ic-nbctl command took too long: %s %s in %vms", OVNIcNbCtl, strings.Join(cmdArgs, " "), elapsed)
	}
	return trimCommandOutput(raw), nil
}

func (c LegacyClient) GetTsSubnet(ts string) (string, error) {
	subnet, err := c.ovnIcNbCommand("get", "Transit_Switch", ts, "external_ids:subnet")
	if err != nil {
		klog.Error(err)
		return "", fmt.Errorf("failed to get ts subnet, %w", err)
	}
	return subnet, nil
}

func (c LegacyClient) GetTs() ([]string, error) {
	cmd := []string{
		"--format=csv", "--data=bare", "--no-heading", "--columns=name",
		"find", "Transit_Switch",
		fmt.Sprintf("external-ids:%s=%s", ExternalIDVendor, util.CniTypeName),
	}
	output, err := c.ovnIcNbCommand(cmd...)
	if err != nil {
		klog.Errorf("failed to list transit switch: %v", err)
		return nil, err
	}
	lines := strings.Split(output, "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		if l = strings.TrimSpace(l); len(l) != 0 {
			result = append(result, l)
		}
	}
	return result, nil
}
