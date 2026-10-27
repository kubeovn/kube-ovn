package ipsec

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGenerationCollectionPreservesRecoveryAndForeignFiles(t *testing.T) {
	s := store{dir: t.TempDir()}
	now := time.Now()
	makeGeneration := func(name string, old, recorded bool) *generation {
		t.Helper()
		g := &generation{ID: digest([]byte(name)), NodeUID: "node-uid", Chassis: "chassis"}
		require.NoError(t, s.write(g, "private-key", []byte("fixture")))
		if recorded {
			require.NoError(t, s.recordGeneration(g))
		}
		if old {
			entries, err := os.ReadDir(filepath.Dir(s.path(g, "private-key")))
			require.NoError(t, err)
			for _, entry := range entries {
				path := filepath.Join(filepath.Dir(s.path(g, "private-key")), entry.Name())
				require.NoError(t, os.Chtimes(path, now.Add(-48*time.Hour), now.Add(-48*time.Hour)))
			}
		}
		return g
	}
	current := makeGeneration("current", true, true)
	previous := makeGeneration("previous", true, true)
	pending := makeGeneration("pending", true, true)
	database := makeGeneration("interrupted-commit", true, true)
	retired := makeGeneration("retired", true, true)
	recent := makeGeneration("recent", false, true)
	unrecorded := makeGeneration("legacy-or-foreign", true, false)
	unknownFile := makeGeneration("foreign-file", true, true)
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(s.path(unknownFile, "private-key")), "foreign.txt"), []byte("retain"), 0o600))
	for name, g := range map[string]*generation{"current": current, "previous": previous, "pending": pending} {
		require.NoError(t, s.save(name, g))
	}
	// A generation symlink must not be traversed, even when its basename
	// resembles a module ID. The target contains a file that must survive.
	external := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(external, "foreign.txt"), []byte("retain"), 0o600))
	require.NoError(t, os.Symlink(external, filepath.Join(s.dir, "generations", digest([]byte("external")))))
	paths := map[string]string{"certificate": s.path(database, "certificate")}
	for range 2 {
		require.NoError(t, s.collectGenerations(now, paths))
	}
	require.NoDirExists(t, filepath.Dir(s.path(retired, "private-key")))
	for _, g := range []*generation{current, previous, pending, database, recent, unrecorded, unknownFile} {
		require.FileExists(t, s.path(g, "private-key"))
	}
	require.FileExists(t, filepath.Join(external, "foreign.txt"))
}

func TestGenerationCollectionRejectsCorruptReferences(t *testing.T) {
	for _, name := range []string{"current", "pending", "previous"} {
		t.Run(name, func(t *testing.T) {
			s := store{dir: t.TempDir()}
			require.NoError(t, os.WriteFile(filepath.Join(s.dir, name+".json"), []byte("broken"), 0o600))
			require.Error(t, s.collectGenerations(time.Now(), nil))
		})
	}
}

func TestRetainPreviousSurvivesInterruptedActivation(t *testing.T) {
	s := store{dir: t.TempDir()}
	old := &generation{ID: digest([]byte("old"))}
	next := &generation{ID: digest([]byte("next"))}
	require.NoError(t, s.save("current", old))
	require.NoError(t, s.retainPrevious(next))
	// Retry without a current commit must still retain the old generation.
	require.NoError(t, s.retainPrevious(next))
	previous, err := s.load("previous")
	require.NoError(t, err)
	require.Equal(t, old, previous)
	require.NoError(t, s.save("current", next))
	require.NoError(t, s.retainPrevious(next))
	previous, err = s.load("previous")
	require.NoError(t, err)
	require.Equal(t, old, previous, "steady reconciliation must not advance previous")
}
