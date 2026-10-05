package ipsec

import (
	"encoding/json/v2"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCleanupIdentityRequiresCompleteOwnedGeneration(t *testing.T) {
	s := store{dir: t.TempDir()}
	paths, err := s.cleanupIdentity(nil, "node", "ns", "uid", "chassis")
	require.NoError(t, err)
	require.Nil(t, paths, "an interrupted Arm need not have an encryption identity")
	key, err := newPrivateKey()
	require.NoError(t, err)
	source := &generation{ID: digest(key), NodeName: "node", Namespace: "ns", NodeUID: "uid", Chassis: "chassis"}
	require.NoError(t, s.write(source, "private-key", key))
	// Cleanup verifies ownership/content rather than certificate validity. The
	// expired or incomplete certificate that prevented startup must not make a
	// proven owned database reference permanently impossible to remove.
	require.NoError(t, s.write(source, "certificate", []byte("expired certificate")))
	g, err := s.prepareGeneration(source, []byte("committed trust"))
	require.NoError(t, err)
	paths = map[string]string{"certificate": s.path(g, "certificate"), "private_key": s.path(g, "private-key"), "ca_cert": s.path(g, "ca-bundle"), "other-module": "preserve"}
	owned, err := s.cleanupIdentity(paths, "node", "ns", "uid", "chassis")
	require.NoError(t, err, "the OVS commit may have happened before current.json was saved")
	require.Len(t, owned, 3)
	for _, key := range []string{"certificate", "private_key", "ca_cert"} {
		changed := maps.Clone(paths)
		changed[key] = "foreign"
		_, err := s.cleanupIdentity(changed, "node", "ns", "uid", "chassis")
		require.Error(t, err)
		delete(changed, key)
		_, err = s.cleanupIdentity(changed, "node", "ns", "uid", "chassis")
		require.Error(t, err)
	}
	_, err = s.cleanupIdentity(paths, "node", "ns", "replacement", "chassis")
	require.Error(t, err)
	_, err = s.cleanupIdentity(paths, "node", "ns", "uid", "replacement")
	require.Error(t, err)
	_, err = s.cleanupIdentity(paths, "foreign", "ns", "uid", "chassis")
	require.Error(t, err)
	require.NoError(t, os.WriteFile(s.path(g, "certificate"), []byte("changed"), 0o600))
	_, err = s.cleanupIdentity(paths, "node", "ns", "uid", "chassis")
	require.ErrorContains(t, err, "content changed")
	require.NoError(t, os.WriteFile(s.path(g, "certificate"), []byte("expired certificate"), 0o600))
	metadata := filepath.Join(filepath.Dir(s.path(g, "private-key")), "metadata.json")
	require.NoError(t, os.Remove(metadata))
	_, err = s.cleanupIdentity(paths, "node", "ns", "uid", "chassis")
	require.Error(t, err)
	data, err := json.Marshal(g)
	require.NoError(t, err)
	foreign := filepath.Join(t.TempDir(), "metadata.json")
	require.NoError(t, os.WriteFile(foreign, data, 0o600))
	require.NoError(t, os.Symlink(foreign, metadata))
	_, err = s.cleanupIdentity(paths, "node", "ns", "uid", "chassis")
	require.Error(t, err, "ownership evidence must not follow a symlink")
}
