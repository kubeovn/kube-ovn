package pinger

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAppctlSocketUsesRecordedPIDWithoutNamespaceLockLookup(t *testing.T) {
	file := filepath.Join(t.TempDir(), "ovs-vswitchd.pid")
	// No local process or lock owns this PID: it belongs to the daemon's namespace.
	require.NoError(t, os.WriteFile(file, []byte("2147483647\n"), 0o600))
	socket, err := appctlSocket(file)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(filepath.Dir(file), "ovs-vswitchd.2147483647.ctl"), socket)
}

func TestAppctlSocketRejectsMissingAndInvalidPID(t *testing.T) {
	file := filepath.Join(t.TempDir(), "ovn-controller.pid")
	_, err := appctlSocket(file)
	require.Error(t, err)
	for _, pid := range []string{"", "0", "-1", "12\n13", "../socket", "2147483648"} {
		require.NoError(t, os.WriteFile(file, []byte(pid), 0o600))
		_, err = appctlSocket(file)
		require.Error(t, err, "PID %q", pid)
	}
}
