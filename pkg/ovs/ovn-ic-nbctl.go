package ovs

import (
	"fmt"
	"k8s.io/klog"
	"os/exec"
	"strings"
	"time"
)

func (c Client) ovnIcNbCommand(cmdArgs ...string) (string, error) {
	start := time.Now()
	cmdArgs = append([]string{fmt.Sprintf("--timeout=%d", c.OvnTimeout), fmt.Sprintf("--db=tcp:%s", c.OVNIcNBAddress)}, cmdArgs...)
	raw, err := exec.Command(OVNIcNbCtl, cmdArgs...).CombinedOutput()
	elapsed := float64((time.Since(start)) / time.Millisecond)
	klog.V(4).Infof("%s command %s in %vms", OVNIcNbCtl, strings.Join(cmdArgs, " "), elapsed)
	if err != nil || elapsed > 500 {
		klog.Warning("ovn-ic-nbctl command error or took too long")
		klog.Warningf("%s %s in %vms", OVNIcNbCtl, strings.Join(cmdArgs, " "), elapsed)
	}
	if err != nil {
		return "", fmt.Errorf("%s, %q", raw, err)
	}
	return trimCommandOutput(raw), nil
}

func (c Client) GetTsSubnet(ts string) (string, error) {
	subnet, err := c.ovnIcNbCommand("get", "Transit_Switch", ts, "external_ids:subnet")
	if err != nil {
		return "", fmt.Errorf("failed to get ts subnet, %v", err)
	}
	return subnet, nil
}
