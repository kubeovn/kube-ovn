package pinger

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// appctlSocket resolves a shared daemon PID file without checking its lock owner.
// appctl's named-target lookup compares that owner with the recorded PID, which
// fails across the separate PID namespaces used by pinger and ovs-ovn pods.
// Connecting to the resulting socket still verifies that the daemon is alive.
func appctlSocket(pidFile string) (string, error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return "", fmt.Errorf("read daemon PID: %w", err)
	}
	pid, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 31)
	if err != nil || pid == 0 {
		return "", fmt.Errorf("invalid daemon PID in %s", pidFile)
	}
	return fmt.Sprintf("%s.%d.ctl", strings.TrimSuffix(pidFile, ".pid"), pid), nil
}
