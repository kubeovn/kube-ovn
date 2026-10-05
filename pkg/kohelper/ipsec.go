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

// OpenPodIPsecRoot pins the mount namespace of the Pod's live charon process.
// Tools remain in the independent agent; only files and its control socket
// are accessed through this directory descriptor.
func OpenPodIPsecRoot(ctx context.Context, procRoot, uid string) (*os.File, error) {
	if uid == "" || strings.ContainsAny(uid, "/\n\r") {
		return nil, errors.New("pod UID is required")
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, fmt.Errorf("read host proc: %w", err)
	}
	var selected *os.File
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			if selected != nil {
				_ = selected.Close()
			}
			return nil, err
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 || !entry.IsDir() {
			continue
		}
		process := filepath.Join(procRoot, entry.Name())
		if !isPodCharon(process, uid) {
			continue
		}
		root, err := os.Open(filepath.Join(process, "root"))
		if err != nil {
			continue // A process may exit while scanning proc.
		}
		if !isPodCharon(process, uid) {
			_ = root.Close()
			continue
		}
		if selected != nil {
			_ = root.Close()
			_ = selected.Close()
			return nil, errors.New("multiple charon processes match the pod UID")
		}
		selected = root
	}
	if selected == nil {
		return nil, errors.New("no live charon process matches the pod UID (IPsec may be disabled)")
	}
	return selected, nil
}

func isPodCharon(process, uid string) bool {
	comm, err := os.ReadFile(filepath.Join(process, "comm"))
	if err != nil || strings.TrimSpace(string(comm)) != "charon" {
		return false
	}
	cgroup, err := os.ReadFile(filepath.Join(process, "cgroup"))
	return err == nil && hasPodCgroup(string(cgroup), uid)
}
