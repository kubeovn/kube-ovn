package ipsec

import (
	"crypto/rand"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/kubeovn/kube-ovn/pkg/fileutil"
)

// Each supervised IKE/monitor pair gets a distinct connection namespace.
// Intent is recorded before loading configuration into IKE. Neither a name
// prefix nor this intent alone proves ownership of an orphaned kernel SA.
type connectionSession struct {
	Version int    `json:"version"`
	NodeUID string `json:"nodeUID"`
	Lease   string `json:"lease"`
	Prefix  string `json:"prefix"`
	Mark    uint32 `json:"mark"`
	Reqid   uint32 `json:"reqid"`
	BootID  string `json:"bootID"`
}

func (s connectionSession) intentPath(owner store) string {
	return filepath.Join(owner.dir, "connections", s.Prefix, "intent.json")
}

func (r *runtimeManager) prepareConnectionSession() (*connectionSession, error) {
	r.mu.Lock()
	nodeUID := r.expected.NodeUID
	r.mu.Unlock()
	if r.store.dir == "" || nodeUID == "" {
		return nil, errors.New("IPsec connection intent requires a persistent node owner")
	}
	lease, err := r.store.loadProtection(nodeUID)
	if err != nil {
		return nil, err
	}
	if lease == nil || !lease.Required || lease.Indexes[0] == 0 || lease.Indexes[1] == 0 {
		return nil, errors.New("IPsec connection intent requires the armed protection lease")
	}
	bootID, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return nil, err
	}
	session := &connectionSession{Version: 1, NodeUID: nodeUID, Lease: lease.Lease, Prefix: "ko" + rand.Text() + "-", Mark: lease.Mark, Reqid: lease.Reqid, BootID: strings.TrimSpace(string(bootID))}
	path := session.intentPath(r.store)
	if err := secureDirectory(filepath.Join(r.store.dir, "connections")); err != nil {
		return nil, err
	}
	// Refuse an existing session even in the unlikely event of a name collision.
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	data, err := json.Marshal(session)
	if err != nil {
		return nil, err
	}
	if err := fileutil.AtomicWriteFile(filepath.Join(filepath.Dir(path), "session.json"), data, 0o600); err != nil {
		return nil, err
	}
	// AtomicWriteFile syncs the session directory. Sync its ancestors too:
	// a crash must not lose a newly created session after IKE has loaded it.
	for _, parent := range []string{filepath.Dir(filepath.Dir(path)), r.store.dir} {
		directory, err := os.Open(parent) // #nosec G304 -- private node-owned store directories.
		if err != nil {
			return nil, err
		}
		err = errors.Join(directory.Sync(), directory.Close())
		if err != nil {
			return nil, err
		}
	}
	return session, nil
}
