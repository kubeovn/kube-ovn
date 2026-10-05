package ipsec

import (
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/kubeovn/kube-ovn/pkg/fileutil"
)

// recordGeneration marks directories created by this module. Older or foreign
// directories without this record are left intact, even if their names match.
func (s store) recordGeneration(g *generation) error {
	data, err := json.Marshal(g)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(filepath.Join(filepath.Dir(s.path(g, "private-key")), "metadata.json"), data, 0o600)
}

func (s store) retainPrevious(next *generation) error {
	current, err := s.load("current")
	if err != nil || current == nil || current.ID == next.ID {
		return err
	}
	// Persist before changing OVSDB so either side of an interrupted activation
	// retains its files. Repeating the same generation never advances previous.
	return s.save("previous", current)
}

// collectGenerations requires the node owner lock. It keeps every durable or
// database reference and delays deletion for a day after the last file change.
func (s store) collectGenerations(now time.Time, ovsPaths map[string]string) error {
	keep := make(map[string]bool)
	for _, name := range []string{"current", "pending", "previous"} {
		g, err := s.load(name)
		if err != nil {
			return err
		}
		if g != nil {
			keep[g.ID] = true
		}
	}
	dir := filepath.Join(s.dir, "generations")
	for _, path := range ovsPaths {
		// Any path within a generation keeps the whole generation, regardless
		// of which OVSDB key references it.
		parent := filepath.Dir(filepath.Clean(path))
		if filepath.Dir(parent) == dir {
			keep[filepath.Base(parent)] = true
		}
	}
	if err := secureDirectory(dir); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		id := entry.Name()
		if keep[id] || len(id) != 64 || !entry.IsDir() {
			continue
		}
		if _, err := hex.DecodeString(id); err != nil {
			continue
		}
		eligible, err := s.retiredGeneration(id, now)
		if err != nil {
			return err
		}
		if eligible {
			if err := root.RemoveAll(id); err != nil {
				return fmt.Errorf("collect retired IPsec generation: %w", err)
			}
		}
	}
	return nil
}

func (s store) retiredGeneration(id string, now time.Time) (bool, error) {
	dir := filepath.Join(s.dir, "generations", id)
	metadata, err := readRegularFile(filepath.Join(dir, "metadata.json"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var g generation
	if err := json.Unmarshal(metadata, &g); err != nil {
		return false, err
	}
	if g.ID != id || g.NodeUID == "" || g.Chassis == "" {
		return false, errors.New("retired IPsec generation has an invalid owner record")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if !slices.Contains([]string{"metadata.json", "private-key.pem", "certificate.pem", "ca-bundle.pem"}, entry.Name()) {
			return false, nil
		}
		info, err := entry.Info()
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() {
			return false, errors.New("retired IPsec generation contains a non-regular entry")
		}
		if now.Sub(info.ModTime()) < 24*time.Hour {
			return false, nil
		}
	}
	return true, nil
}
