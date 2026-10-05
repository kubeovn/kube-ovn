package ipsec

import (
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
)

// cleanupIdentity accepts only the complete, content-addressed generation
// recorded by this node. It also covers a crash after the OVSDB commit but
// before current.json advanced. Expiration does not obstruct removing an owned
// reference; legacy/foreign paths and incomplete evidence remain untouched.
func (s store) cleanupIdentity(paths map[string]string, nodeName, namespace, nodeUID, chassis string) (map[string]string, error) {
	keys := []string{"certificate", "private_key", "ca_cert"}
	empty := true
	for _, key := range keys {
		empty = empty && paths[key] == ""
	}
	if empty {
		return nil, nil // Arm may have stopped before acquiring an identity.
	}
	parent := filepath.Dir(paths["private_key"])
	id := filepath.Base(parent)
	if len(id) != 64 || filepath.Dir(parent) != filepath.Join(s.dir, "generations") {
		return nil, errors.New("IPsec cleanup cannot prove the identity generation's owner")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return nil, err
	}
	for _, dir := range []string{s.dir, filepath.Dir(parent), parent} {
		info, err := os.Lstat(dir)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("IPsec cleanup generation directory is not owned storage")
		}
	}
	data, err := readRegularFile(filepath.Join(parent, "metadata.json"))
	if err != nil {
		return nil, err
	}
	var g generation
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, err
	}
	if g.ID != id || g.NodeName != nodeName || g.Namespace != namespace || g.NodeUID != nodeUID || g.Chassis != chassis {
		return nil, errors.New("IPsec cleanup identity belongs to another node or chassis")
	}
	expected := map[string]string{"certificate": s.path(&g, "certificate"), "private_key": s.path(&g, "private-key"), "ca_cert": s.path(&g, "ca-bundle")}
	for _, key := range keys {
		if paths[key] != expected[key] {
			return nil, errors.New("IPsec cleanup identity is a foreign or incomplete generation")
		}
	}
	var content []byte
	for _, name := range []string{"private-key", "certificate", "ca-bundle"} {
		data, err := readRegularFile(s.path(&g, name))
		if err != nil {
			return nil, err
		}
		content = append(content, data...)
	}
	if digest(content) != id {
		return nil, errors.New("IPsec cleanup identity generation content changed")
	}
	return expected, nil
}
