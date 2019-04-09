package ovs

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"k8s.io/klog"
)

func (c Client) ovnSbCommand(arg ...string) (string, error) {
	cmdArgs := []string{fmt.Sprintf("--db=%s", c.OvnSbAddress)}
	cmdArgs = append(cmdArgs, arg...)
	klog.V(5).Infof("execute %s command %s", OvnSbCtl, strings.Join(cmdArgs, " "))

	start := time.Now()
	raw, err := exec.Command(OvnSbCtl, cmdArgs...).CombinedOutput()
	elapsed := float64((time.Since(start)) / time.Millisecond)
	klog.Infof("%s command %s in %vms", OvnSbCtl, strings.Join(cmdArgs, " "), elapsed)
	if err != nil {
		return "", fmt.Errorf("%s, %v", string(raw), err)
	}
	return trimCommandOutput(raw), nil
}

func (c Client) GetChassisByHostname(hostname string) (string, error) {
	output, err := c.ovnSbCommand("--data=bare", "--no-heading", "--columns=_uuid",
		"find", "Chassis", fmt.Sprintf("hostname=%s", hostname))
	if err != nil {
		return "", err
	}
	if output != "" {
		return output, nil
	}
	return "", ErrNotFound
}

func (c Client) GetLogicalSwitchPortInChassis(chassis string) (string, error) {
	output, err := c.ovnSbCommand("--data=bare", "--no-heading", "--columns=logical_port",
		"find", "Port_Binding", fmt.Sprintf("chassis=%s", chassis))
	if err != nil {
		return "", err
	}
	if output != "" {
		return output, nil
	}
	return "", ErrNotFound
}
