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
	ovsRunDir = "/var/run/openvswitch"
	ovnRunDir = "/var/run/ovn"

	cmdOvsAppctl = "ovs-appctl"

	OvsdbServer   = "ovsdb-server"
	OvsVswitchd   = "ovs-vswitchd"
	OvnController = "ovn-controller"
)

func appctlByTarget(target string, args ...string) (string, error) {
	args = slices.Insert(args, 0, "-t", target)
	cmd := exec.Command(cmdOvsAppctl, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		cmd := strings.Join(slices.Insert(args, 0, cmdOvsAppctl), " ")
		return "", fmt.Errorf("failed to run command %q: %w", cmd, err)
	}
	return string(output), nil
}

func Appctl(component string, args ...string) (string, error) {
	var runDir string
	if strings.HasPrefix(component, "ovs") {
		runDir = ovsRunDir
	} else if strings.HasPrefix(component, "ovn") {
		runDir = ovnRunDir
	} else {
		return "", fmt.Errorf("unknown component %q", component)
	}

	pidFile := filepath.Join(runDir, component+".pid")
	pid, err := os.ReadFile(pidFile)
	if err != nil {
		return "", fmt.Errorf("failed to read pid file %q: %w", pidFile, err)
	}

	target := filepath.Join(runDir, fmt.Sprintf("%s.%s.ctl", component, strings.TrimSpace(string(pid))))
	return appctlByTarget(target, args...)
}
