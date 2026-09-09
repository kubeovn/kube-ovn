package ovs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
)

const (
	ovsRunDir = "/var/run/openvswitch"
	ovnRunDir = "/var/run/ovn"

	cmdOvsAppctl = "ovs-appctl"
	cmdOvnAppctl = "ovn-appctl"

	ovnNBCtlSocket = ovnRunDir + "/ovnnb_db.ctl"
	ovnSBCtlSocket = ovnRunDir + "/ovnsb_db.ctl"
)

func appctlByTarget(appctlCmd, target, command string, args ...string) (string, error) {
	args = slices.Insert(args, 0, "-t", target, command)
	cmd := exec.Command(appctlCmd, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to run command %q: %w", cmd.String(), err)
	}
	return string(output), nil
}

// componentRunDir maps a component to the runtime directory holding its pid file and
// control socket. Only daemons whose control socket is suffixed with their pid belong
// here: the ovsdb-server processes serving the OVN databases listen on a fixed socket
// path instead and must be reached through OvnDatabaseControl.
var componentRunDir = map[string]string{
	OvsdbServer:   ovsRunDir,
	OvsVswitchd:   ovsRunDir,
	OvnController: ovnRunDir,
	OvnNorthd:     ovnRunDir,
}

func runDirOfComponent(component string) (string, error) {
	runDir, ok := componentRunDir[component]
	if !ok {
		return "", fmt.Errorf("unknown component %q", component)
	}
	return runDir, nil
}

func ctlSocketPathIn(runDir, component string) (string, error) {
	pidFile := filepath.Join(runDir, component+".pid")
	pidBytes, err := os.ReadFile(pidFile)
	if err != nil {
		return "", fmt.Errorf("failed to read pid file %q: %w", pidFile, err)
	}
	pidFields := strings.Fields(string(pidBytes))
	if len(pidFields) == 0 {
		return "", fmt.Errorf("pid file %q is empty or contains only whitespace", pidFile)
	}

	return filepath.Join(runDir, fmt.Sprintf("%s.%s.ctl", component, pidFields[0])), nil
}

// ctlSocketPath resolves the control socket path of the given component by reading its pid file.
// We must not let ovs-appctl/ovn-appctl resolve a bare component name on their own:
// they open the pid file for writing and take the pid from fcntl(F_GETLK) instead of
// the file content, so the resolution fails for a non-root user as well as for a
// component running in another pid namespace, where the kernel reports the lock owner as pid 0.
func ctlSocketPath(component string) (string, error) {
	runDir, err := runDirOfComponent(component)
	if err != nil {
		return "", err
	}
	return ctlSocketPathIn(runDir, component)
}

// OvnDatabaseControl sends a command to the specified OVN database control socket
// and returns the output or an error if the command fails.
func OvnDatabaseControl(db, command string, args ...string) (string, error) {
	var socket string
	switch db {
	case "nb", ovnnb.DatabaseName:
		socket = ovnNBCtlSocket
	case "sb", ovnsb.DatabaseName:
		socket = ovnSBCtlSocket
	default:
		return "", fmt.Errorf("unknown db %q", db)
	}
	return Appctl(socket, command, args...)
}

// Appctl sends a command to an ovs/ovn daemon. The target is either an absolute
// path of a control socket, or the name of a component whose control socket is
// resolved from its pid file.
func Appctl(target, command string, args ...string) (string, error) {
	if !filepath.IsAbs(target) {
		socket, err := ctlSocketPath(target)
		if err != nil {
			return "", err
		}
		target = socket
	}

	cmd := cmdOvsAppctl
	if strings.HasPrefix(target, ovnRunDir+string(os.PathSeparator)) {
		cmd = cmdOvnAppctl
	}

	return appctlByTarget(cmd, target, command, args...)
}
