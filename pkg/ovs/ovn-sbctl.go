package ovs

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"k8s.io/klog"
)

func (c Client) ovnSbCommand(cmdArgs ...string) (string, error) {
	start := time.Now()
	cmdArgs = append([]string{fmt.Sprintf("--timeout=%d", c.OvnTimeout), fmt.Sprintf("--db=%s", c.OvnSbAddress)}, cmdArgs...)
	raw, err := exec.Command(OvnSbCtl, cmdArgs...).CombinedOutput()
	elapsed := float64((time.Since(start)) / time.Millisecond)
	klog.Infof("%s command %s in %vms", OvnSbCtl, strings.Join(cmdArgs, " "), elapsed)
	if err != nil {
		return "", fmt.Errorf("%s, %q", raw, err)
	}
	return trimCommandOutput(raw), nil
}

func (c Client) DeleteChassis(node string) error {
	output, err := c.ovnSbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=name", "find", "chassis", fmt.Sprintf("hostname=%s", node))
	if err != nil {
		return fmt.Errorf("failed to find node chassis %s, %v", node, err)
	}
	for _, chassis := range strings.Split(output, "\n") {
		chassis = strings.TrimSpace(chassis)
		if len(chassis) > 0 {
			if _, err := c.ovnSbCommand("chassis-del", strings.TrimSpace(chassis)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c Client) GetChassis(node string) (string, error) {
	output, err := c.ovnSbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=name", "find", "chassis", fmt.Sprintf("hostname=%s", node))
	if err != nil {
		return "", fmt.Errorf("failed to find node chassis %s, %v", node, err)
	}
	return strings.TrimSpace(output), nil
}
