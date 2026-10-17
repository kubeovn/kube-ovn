package ipsec

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLegacyMonitorLockPreventsTakeover(t *testing.T) {
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "ovs-monitor-ipsec.pid")
	socket := filepath.Join(dir, "db.sock")
	require.NoError(t, checkLegacyMonitor(socket))
	// PID text alone, including an existing process's PID, is not a live lock.
	require.NoError(t, os.WriteFile(pidfile, []byte("1\n"), 0o600))
	require.NoError(t, checkLegacyMonitor(socket))
	// The holder releases its lock during cleanup, after t.Context is canceled.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, "python3", "-c", `
import fcntl,sys
with open(sys.argv[1], "r+") as f:
    fcntl.lockf(f, fcntl.LOCK_EX|fcntl.LOCK_NB)
    print("locked", flush=True)
    sys.stdin.read()
`, pidfile)
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		require.NoError(t, stdin.Close())
		require.NoError(t, cmd.Wait())
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "locked\n", line)
	require.ErrorContains(t, checkLegacyMonitor(socket), "still owns its pidfile lock")
}

func TestLegacyMonitorRejectsUnsafePidfile(t *testing.T) {
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "ovs-monitor-ipsec.pid")
	target := filepath.Join(t.TempDir(), "unrelated")
	require.NoError(t, os.WriteFile(target, []byte("unrelated\n"), 0o600))
	require.NoError(t, os.Symlink(target, pidfile))
	require.Error(t, checkLegacyMonitor(filepath.Join(dir, "db.sock")))
	contents, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "unrelated\n", string(contents))
}
