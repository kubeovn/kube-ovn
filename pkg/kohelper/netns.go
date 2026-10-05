package kohelper

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ResolvePodNetns finds a live namespace by Pod UID in the host proc mount.
// It also works when a macvlan/ipvlan-only Pod has no OVS interface. The returned
// path uses host PIDs, so the independent agent must have hostPID enabled.
func ResolvePodNetns(ctx context.Context, procRoot, uid string) (string, error) {
	if uid == "" || strings.ContainsAny(uid, "/\n\r") {
		return "", errors.New("pod UID is required")
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return "", fmt.Errorf("read host proc: %w", err)
	}
	var selectedPath, selectedNamespace string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 || !entry.IsDir() {
			continue
		}
		cgroupPath := filepath.Join(procRoot, entry.Name(), "cgroup")
		cgroup, err := os.ReadFile(cgroupPath)
		if err != nil || !hasPodCgroup(string(cgroup), uid) {
			continue // Processes may exit while the proc directory is scanned.
		}
		path := filepath.Join(procRoot, entry.Name(), "ns/net")
		namespace, err := os.Readlink(path)
		if err != nil || !strings.HasPrefix(namespace, "net:[") {
			continue
		}
		// Recheck identity after opening the namespace metadata, avoiding a PID
		// that was reused for another Pod during the scan.
		current, err := os.ReadFile(cgroupPath)
		if err != nil || !hasPodCgroup(string(current), uid) {
			continue
		}
		if selectedNamespace != "" && selectedNamespace != namespace {
			return "", errors.New("pod processes have multiple network namespaces")
		}
		if selectedPath == "" {
			selectedPath = fmt.Sprintf("/proc/%d/ns/net", pid)
			selectedNamespace = namespace
		}
	}
	if selectedPath == "" {
		return "", errors.New("no live host process matches the pod UID")
	}
	return selectedPath, nil
}

func hasPodCgroup(cgroup, uid string) bool {
	// cgroupfs uses the raw UID; systemd slice names replace '-' with '_'.
	for line := range strings.SplitSeq(cgroup, "\n") {
		for component := range strings.SplitSeq(line, "/") {
			if component == "pod"+uid || strings.HasSuffix(component, "-pod"+strings.ReplaceAll(uid, "-", "_")+".slice") {
				return true
			}
		}
	}
	return false
}
