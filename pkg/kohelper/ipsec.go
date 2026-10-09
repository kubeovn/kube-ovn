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
	var failures []error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			if selected != nil {
				return nil, errors.Join(err, selected.Close())
			}
			return nil, err
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 || !entry.IsDir() {
			continue
		}
		process := filepath.Join(procRoot, entry.Name())
		matches, err := isPodCharon(process, uid)
		if err != nil {
			if !processGone(err) {
				failures = append(failures, err)
			}
			continue
		}
		if !matches {
			continue
		}
		root, err := os.Open(filepath.Join(process, "root"))
		if err != nil {
			if !processGone(err) {
				failures = append(failures, err)
			}
			continue
		}
		matches, err = isPodCharon(process, uid)
		if err != nil || !matches {
			if err != nil && !processGone(err) {
				failures = append(failures, err)
			}
			if err := root.Close(); err != nil {
				failures = append(failures, err)
			}
			continue
		}
		if selected != nil {
			return nil, errors.Join(errors.New("multiple charon processes match the pod UID"), root.Close(), selected.Close())
		}
		selected = root
	}
	if selected == nil {
		if len(failures) != 0 {
			return nil, fmt.Errorf("cannot discover pod IPsec process: %w", errors.Join(failures...))
		}
		return nil, errors.New("no live charon process matches the pod UID (IPsec may be disabled)")
	}
	return selected, nil
}

func isPodCharon(process, uid string) (bool, error) {
	comm, err := os.ReadFile(filepath.Join(process, "comm"))
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(string(comm)) != "charon" {
		return false, nil
	}
	cgroup, err := os.ReadFile(filepath.Join(process, "cgroup"))
	return err == nil && hasPodCgroup(string(cgroup), uid), err
}
