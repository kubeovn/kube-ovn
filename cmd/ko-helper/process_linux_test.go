package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/kohelper"
)

type childPIDWriter struct{ pid chan string }

func (w childPIDWriter) Write(p []byte) (int, error) {
	w.pid <- string(p)
	return len(p), nil
}

func TestRunnerCancellationStopsIPsecQuery(t *testing.T) {
	proc := t.TempDir()
	root := filepath.Join(proc, "42/root")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "etc"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "run"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(proc, "42/comm"), []byte("charon\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(proc, "42/cgroup"), []byte("0::/kubepods/podsource"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "etc/ipsec.conf"), []byte("fixture\n"), 0o600))
	socketDir, err := os.MkdirTemp("/tmp", "ko-cancel-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(socketDir)) })
	socket := filepath.Join(socketDir, "charon.ctl")
	listener, err := net.Listen("unix", socket)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })
	require.NoError(t, os.Symlink(socket, filepath.Join(root, "run/charon.ctl")))
	require.NoError(t, listener.(*net.UnixListener).SetDeadline(time.Now().Add(5*time.Second)))
	tools := t.TempDir()
	executable, err := os.Executable()
	require.NoError(t, err)
	require.NoError(t, os.Symlink(executable, filepath.Join(tools, "ipsec")))
	t.Setenv("PATH", tools)
	t.Setenv("KO_IPSEC_STROKE_TEST", "1")
	t.Setenv("KO_IPSEC_STROKE_REPORT_PID", "1")
	t.Setenv("KO_IPSEC_COLLECTOR_PROC", proc)
	settingsRecord := filepath.Join(t.TempDir(), "settings-path")
	t.Setenv("KO_IPSEC_STROKE_SETTINGS_RECORD", settingsRecord)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan kohelper.Result, 1)
	go func() {
		done <- (runner{}).Run(ctx, kohelper.Request{Argv: []string{executable, "-test.run=^TestIPsecCollectorProcess$"}}, io.Discard, io.Discard)
	}()
	connection, err := listener.Accept()
	require.NoError(t, err)
	defer connection.Close()
	require.NoError(t, connection.SetReadDeadline(time.Now().Add(5*time.Second)))
	line, err := bufio.NewReader(connection).ReadString('\n')
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	require.NoError(t, err)
	require.Positive(t, pid)
	defer func() { _ = syscall.Kill(pid, syscall.SIGKILL) }()
	settingsPath, err := os.ReadFile(settingsRecord)
	require.NoError(t, err)
	require.NoFileExists(t, string(settingsPath), "settings must already be unlinked while the query is running")
	// The stroke client is blocked waiting for the daemon's reply.
	cancel()
	select {
	case result := <-done:
		require.Equal(t, 130, result.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("IPsec collector did not exit after runner cancellation")
	}
	require.Eventually(t, func() bool {
		data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if os.IsNotExist(err) || errors.Is(err, syscall.ESRCH) {
			return true
		}
		_, state, ok := strings.Cut(string(data), ") ")
		return err == nil && ok && strings.HasPrefix(state, "Z ")
	}, 2*time.Second, 10*time.Millisecond, "nested IPsec query survived runner cancellation")
}

func TestRunnerCancellationStopsDescendants(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	pidOutput := make(chan string, 1)
	done := make(chan kohelper.Result, 1)
	go func() {
		done <- (runner{}).Run(ctx, kohelper.Request{Argv: []string{"sh", "-c", "sleep 60 & echo $!; wait"}}, childPIDWriter{pidOutput}, io.Discard)
	}()
	var pid int
	select {
	case output := <-pidOutput:
		var err error
		pid, err = strconv.Atoi(strings.TrimSpace(output))
		if err != nil || pid <= 0 {
			t.Fatalf("invalid child PID: %q", output)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child process did not start")
	}
	defer func() { _ = syscall.Kill(pid, syscall.SIGKILL) }()
	cancel()
	select {
	case result := <-done:
		if result.Code != 130 {
			t.Errorf("cancelled runner status = %d, want 130", result.Code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled runner is blocked by a surviving child")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		// procfs may return ESRCH when the process is reaped during a read.
		if os.IsNotExist(err) || errors.Is(err, syscall.ESRCH) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		_, state, ok := strings.Cut(string(data), ") ")
		if ok && strings.HasPrefix(state, "Z ") {
			return // A terminated child may await reaping by the container's PID 1.
		}
		if time.Now().After(deadline) {
			t.Fatal("descendant is still running after cancellation")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
