package ovs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
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

func (c LegacyClient) DeleteChassisByNode(node string) error {
	output, err := c.ovnSbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=name", "find", "chassis", fmt.Sprintf("external_ids:node=%s", node))
	if err != nil {
		return fmt.Errorf("failed to get node chassis %s, %v", node, err)
	}
	for _, chassis := range strings.Split(output, "\n") {
		chassis = strings.TrimSpace(chassis)
		if len(chassis) > 0 {
			if err := c.DeleteChassisByName(chassis); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c LegacyClient) DeleteChassisByName(chassisName string) error {
	ovnVersion, err := c.GetVersion()
	if err != nil {
		return fmt.Errorf("failed to get ovn version, %v", err)
	}

	cmdArg := []string{"chassis-del", strings.TrimSpace(chassisName)}
	if util.CompareVersion("20.09", ovnVersion) >= 0 {
		cmdArg = append(cmdArg, "--", "destroy", "chassis_private", strings.TrimSpace(chassisName))
	}
	if _, err := c.ovnSbCommand(cmdArg...); err != nil {
		return err
	}
	return nil
}

func (c LegacyClient) GetChassis(node string) (string, error) {
	output, err := c.ovnSbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=name", "find", "chassis", fmt.Sprintf("hostname=%s", node))
	if err != nil {
		return "", fmt.Errorf("failed to find node chassis %s, %v", node, err)
	}
	if len(output) == 0 {
		output, err = c.ovnSbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=name", "find", "chassis", fmt.Sprintf("external_ids:node=%s", node))
		if err != nil {
			return "", fmt.Errorf("failed to find node chassis %s, %v", node, err)
		}
	}
	return strings.TrimSpace(output), nil
}

func (c LegacyClient) ChassisExist(chassisName string) (bool, error) {
	output, err := c.ovnSbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=name", "find", "chassis", fmt.Sprintf("name=%s", chassisName))
	if err != nil {
		return false, fmt.Errorf("failed to find node chassis %s, %v", chassisName, err)
	}
	if len(strings.Split(output, "\n")) == 0 {
		return false, nil
	}
	return true, nil
}

func (c LegacyClient) InitChassisNodeTag(chassisName string, nodeName string) error {
	_, err := c.ovnSbCommand("set", "chassis", chassisName, fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName), fmt.Sprintf("external_ids:node=%s", nodeName))
	if err != nil {
		return fmt.Errorf("failed to set chassis external_ids, %v", err)
	}
	return nil
}

// GetAllChassis get all chassis init by kube-ovn
func (c LegacyClient) GetAllChassis() ([]string, error) {
	output, err := c.ovnSbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=name", "find", "chassis", fmt.Sprintf("external_ids:vendor=%s", util.CniTypeName))
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
