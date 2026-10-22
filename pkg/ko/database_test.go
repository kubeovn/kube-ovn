package ko

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDatabaseStatusUsesStorageStatusForStandaloneDatabase(t *testing.T) {
	app, executor, _, _ := testApplication(t, readyPod("nb-leader", "worker", "ovn-central", map[string]string{"ovn-nb-leader": "true"}))
	executor.run = func(_ context.Context, _ Target, argv []string, _ Streams) error {
		if slices.Equal(argv, []string{"ovsdb-tool", "db-is-standalone", "/etc/ovn/ovnnb_db.db"}) {
			return nil
		}
		return nil
	}

	require.NoError(t, app.Execute(t.Context(), []string{"db", "nb", "status"}))
	var commands [][]string
	for _, call := range executor.calls {
		commands = append(commands, call.argv)
	}
	require.Equal(t, [][]string{
		{"ovsdb-tool", "db-is-standalone", "/etc/ovn/ovnnb_db.db"},
		{"ovn-appctl", "-t", "/var/run/ovn/ovnnb_db.ctl", "ovsdb-server/get-db-storage-status", "OVN_Northbound"},
	}, commands)
}

func TestDatabaseBackupCopiesStandaloneDatabase(t *testing.T) {
	data := []byte("OVSDB JSON 1\n{\"name\":\"OVN_Northbound\"}\n")
	digest := sha256.Sum256(data)
	app, executor, _, _ := testApplication(t, readyPod("nb-leader", "worker", "ovn-central", map[string]string{"ovn-nb-leader": "true"}))
	executor.run = func(_ context.Context, _ Target, argv []string, streams Streams) error {
		switch argv[0] {
		case "ovsdb-tool":
			if slices.Equal(argv, []string{"ovsdb-tool", "db-is-standalone", "/etc/ovn/ovnnb_db.db"}) {
				return nil
			}
			if slices.Contains(argv, "db-name") {
				_, err := io.WriteString(streams.Out, "OVN_Northbound\n")
				return err
			}
		case "cp", "rm":
			return nil
		case "cat":
			_, err := streams.Out.Write(data)
			return err
		case "sha256sum":
			_, err := fmt.Fprintf(streams.Out, "%s  %s\n", hex.EncodeToString(digest[:]), argv[1])
			return err
		}
		return nil
	}

	destination := filepath.Join(t.TempDir(), "northbound.backup")
	require.NoError(t, app.Execute(t.Context(), []string{"db", "nb", "backup", "--output", destination}))
	actual, err := os.ReadFile(destination)
	require.NoError(t, err)
	require.Equal(t, data, actual)
	assertDatabaseBackupCommands(t, executor.calls)
}

func assertDatabaseBackupCommands(t *testing.T, calls []execCall) {
	t.Helper()
	for _, call := range calls {
		if call.argv[0] == "ovsdb-tool" && slices.Contains(call.argv, "cluster-to-standalone") {
			t.Fatalf("standalone backup unexpectedly converted with cluster-to-standalone: %v", call.argv)
		}
	}
}
