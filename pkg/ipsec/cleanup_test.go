package ipsec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCleanupNeverInfersLostOwnershipIsFresh(t *testing.T) {
	newAgent := func() *Agent {
		return &Agent{config: Configuration{ProtectionDir: t.TempDir()}, store: store{dir: t.TempDir()}}
	}
	a := newAgent()
	require.NoError(t, a.noCleanupEvidence(nil))
	for _, key := range []string{"ovn-ipsec-protection-node-uid", "ovn-ipsec-protection-lease", "ovn-ipsec-protection-mark", "ovn-ipsec-protection-reqid"} {
		t.Run(key, func(t *testing.T) {
			require.ErrorContains(t, a.noCleanupEvidence(map[string]string{key: "foreign"}), "ownership reservation")
		})
	}
	for _, name := range []string{"current.json", "pending.json", "connections"} {
		t.Run(name, func(t *testing.T) {
			a := newAgent()
			require.NoError(t, os.WriteFile(filepath.Join(a.store.dir, name), []byte("invalid"), 0o600))
			require.ErrorContains(t, a.noCleanupEvidence(nil), "ownership reservation")
		})
	}
	t.Run("broken intent symlink", func(t *testing.T) {
		a := newAgent()
		require.NoError(t, os.Symlink("missing", filepath.Join(a.config.ProtectionDir, "required")))
		require.ErrorContains(t, a.noCleanupEvidence(nil), "ownership reservation")
	})
}
