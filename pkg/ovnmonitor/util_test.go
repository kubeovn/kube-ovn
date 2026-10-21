package ovnmonitor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetDatabaseStatusUsesStorageStatusForStandaloneDatabase(t *testing.T) {
	testCommandPath(t, "ovsdb-tool", `#!/bin/sh
if [ "$1" = "db-is-standalone" ]; then
  exit 0
fi
exit 1
`)
	testCommandPath(t, "ovn-appctl", `#!/bin/sh
	if [ "$3" = "ovsdb-server/get-db-storage-status" ]; then
  echo "status: ok"
  exit 0
fi
exit 1
`)

	require.Equal(t, 1, getDatabaseStatus("OVN_Northbound", "/etc/ovn/ovnnb_db.db"))
}

func TestGetDatabaseStatusUsesClusterRoleForClusteredDatabase(t *testing.T) {
	testCommandPath(t, "ovsdb-tool", `#!/bin/sh
exit 1
`)
	testCommandPath(t, "ovn-appctl", `#!/bin/sh
	if [ "$3" = "cluster/status" ]; then
  echo "Role: leader"
  exit 0
fi
exit 1
`)

	require.Equal(t, 1, getDatabaseStatus("OVN_Northbound", "/etc/ovn/ovnnb_db.db"))
}

func testCommandPath(t *testing.T, name, script string) {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, name)
	require.NoError(t, os.WriteFile(path, []byte(script), 0o700))
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
}
