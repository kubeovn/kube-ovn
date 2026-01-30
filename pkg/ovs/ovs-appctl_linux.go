package ovs

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
)

const (
	cmdOvsAppctl = "ovs-appctl"
	cmdOvnAppctl = "ovn-appctl"

	ovnNBCtlSocket = "/var/run/ovn/ovnnb_db.ctl"
	ovnSBCtlSocket = "/var/run/ovn/ovnsb_db.ctl"
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

func Appctl(target, command string, args ...string) (string, error) {
	var cmd string
	switch {
	case strings.IndexRune(target, os.PathSeparator) == 0:
		fallthrough
	case strings.HasPrefix(target, "ovn"):
		cmd = cmdOvnAppctl
	case strings.HasPrefix(target, "ovs"):
		cmd = cmdOvsAppctl
	default:
		return "", fmt.Errorf("unknown target %q", target)
	}

	return appctlByTarget(cmd, target, command, args...)
}
