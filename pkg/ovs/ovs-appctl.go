package ovs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const (
	OvsdbServer   = "ovsdb-server"
	OvsVswitchd   = "ovs-vswitchd"
	OvnController = "ovn-controller"

	ovsRunDir = "/var/run/openvswitch"
	ovnRunDir = "/var/run/ovn"

	cmdOvsAppctl = "ovs-appctl"
)

func appctlByTarget(target string, args ...string) (string, error) {
	args = slices.Insert(args, 0, "-t", target)
	cmd := exec.Command(cmdOvsAppctl, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to run command %q: %w", cmd.String(), err)
	}
	return string(output), nil
}

func Appctl(component string, args ...string) (string, error) {
	var runDir string
	switch {
	case strings.HasPrefix(component, "ovs"):
		runDir = ovsRunDir
	case strings.HasPrefix(component, "ovn"):
		runDir = ovnRunDir
	default:
		return "", fmt.Errorf("unknown component %q", component)
	}

	pidFile := filepath.Join(runDir, component+".pid")
	pidBytes, err := os.ReadFile(pidFile)
	if err != nil {
		return "", fmt.Errorf("failed to read pid file %q: %w", pidFile, err)
	}
	pidFields := strings.Fields(string(pidBytes))
	if len(pidFields) == 0 {
		return "", fmt.Errorf("pid file %q is empty or contains only whitespace", pidFile)
	}
	target := filepath.Join(runDir, fmt.Sprintf("%s.%s.ctl", component, pidFields[0]))

	return appctlByTarget(target, args...)
}
