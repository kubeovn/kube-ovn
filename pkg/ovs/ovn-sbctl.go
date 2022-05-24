package ovs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"k8s.io/klog/v2"
)

func (c LegacyClient) ovnSbCommand(cmdArgs ...string) (string, error) {
	start := time.Now()
	if os.Getenv("ENABLE_SSL") == "true" {
		cmdArgs = append([]string{
			fmt.Sprintf("--timeout=%d", c.OvnTimeout),
			fmt.Sprintf("--db=%s", c.OvnSbAddress),
			"-p", "/var/run/tls/key",
			"-c", "/var/run/tls/cert",
			"-C", "/var/run/tls/cacert"}, cmdArgs...)
	} else {
		cmdArgs = append([]string{
			fmt.Sprintf("--timeout=%d", c.OvnTimeout),
			fmt.Sprintf("--db=%s", c.OvnSbAddress)}, cmdArgs...)
	}
	raw, err := exec.Command(OvnSbCtl, cmdArgs...).CombinedOutput()
	elapsed := float64((time.Since(start)) / time.Millisecond)
	klog.V(4).Infof("command %s %s in %vms", OvnSbCtl, strings.Join(cmdArgs, " "), elapsed)
	method := ""
	for _, arg := range cmdArgs {
		if !strings.HasPrefix(arg, "--") {
			method = arg
			break
		}
	}
	code := "0"
	defer func() {
		ovsClientRequestLatency.WithLabelValues("ovn-sb", method, code).Observe(elapsed)
	}()

	if err != nil {
		code = "1"
		klog.Warningf("ovn-sbctl command error: %s %s in %vms", OvnSbCtl, strings.Join(cmdArgs, " "), elapsed)
		return "", fmt.Errorf("%s, %q", raw, err)
	} else if elapsed > 500 {
		klog.Warningf("ovn-sbctl command took too long: %s %s in %vms", OvnSbCtl, strings.Join(cmdArgs, " "), elapsed)
	}
	return trimCommandOutput(raw), nil
}

func (c LegacyClient) DeleteChassis(node string) error {
	output, err := c.ovnSbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=name", "find", "chassis", fmt.Sprintf("hostname=%s", node))
	if err != nil {
		return fmt.Errorf("failed to find node chassis %s, %v", node, err)
	}
	for _, chassis := range strings.Split(output, "\n") {
		chassis = strings.TrimSpace(chassis)
		if len(chassis) > 0 {
			if _, err := c.ovnSbCommand("chassis-del", strings.TrimSpace(chassis), "--", "destroy", "chassis_private", strings.TrimSpace(chassis)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c LegacyClient) GetChassis(node string) (string, error) {
	output, err := c.ovnSbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=name", "find", "chassis", fmt.Sprintf("hostname=%s", node))
	if err != nil {
		return "", fmt.Errorf("failed to find node chassis %s, %v", node, err)
	}
	return strings.TrimSpace(output), nil
}

func (c LegacyClient) GetAllChassisHostname() ([]string, error) {
	output, err := c.ovnSbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=hostname", "find", "chassis")
	if err != nil {
		return nil, fmt.Errorf("failed to find node chassis, %v", err)
	}
	lines := strings.Split(output, "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		if len(strings.TrimSpace(l)) == 0 {
			continue
		}
		result = append(result, strings.TrimSpace(l))
	}
	return result, nil
}
