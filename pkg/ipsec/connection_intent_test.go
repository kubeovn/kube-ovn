package ipsec

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func connectionRuntimeFixture(t *testing.T) *runtimeManager {
	t.Helper()
	r := &runtimeManager{store: store{dir: t.TempDir()}, expected: runtimeConfiguration{NodeUID: "node-uid"}}
	p := &protectionOwner{store: r.store, reservation: protectionReservation{Version: 1, NodeUID: "node-uid", Lease: "node-lease", Mark: 759811, Reqid: 759811, Required: true, Indexes: [2]int{759801, 759809}}}
	require.NoError(t, p.save())
	return r
}

func TestConnectionSessionsDoNotReuseNamesAcrossRuntimeRestarts(t *testing.T) {
	r := connectionRuntimeFixture(t)
	first, err := r.prepareConnectionSession()
	require.NoError(t, err)
	second, err := r.prepareConnectionSession()
	require.NoError(t, err)
	require.NotEqual(t, first.Prefix, second.Prefix)
	require.NotEqual(t, first.intentPath(r.store), second.intentPath(r.store), "a restart cannot overwrite older ownership evidence")
	for _, session := range []*connectionSession{first, second} {
		path := filepath.Join(filepath.Dir(session.intentPath(r.store)), "session.json")
		data, err := readRegularFile(path)
		require.NoError(t, err)
		var saved connectionSession
		require.NoError(t, json.Unmarshal(data, &saved))
		require.Equal(t, *session, saved)
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		require.Equal(t, "node-uid", saved.NodeUID)
		require.Equal(t, "node-lease", saved.Lease)
	}
}

func TestConnectionSessionRejectsUnboundLeaseAndSymlinkDirectory(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*testing.T, *runtimeManager)
	}{
		{"changed-node", func(_ *testing.T, r *runtimeManager) { r.expected.NodeUID = "replacement-uid" }},
		{"missing-identity", func(_ *testing.T, r *runtimeManager) { r.expected.NodeUID = "" }},
		{"missing-lease", func(t *testing.T, r *runtimeManager) {
			require.NoError(t, os.Remove(filepath.Join(r.store.dir, "protection.json")))
		}},
		{"symlink-directory", func(t *testing.T, r *runtimeManager) {
			require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(r.store.dir, "connections")))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := connectionRuntimeFixture(t)
			test.change(t, r)
			session, err := r.prepareConnectionSession()
			require.Error(t, err)
			require.Nil(t, session)
		})
	}
}
