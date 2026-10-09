package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	if os.Getenv("KO_IPSEC_STROKE_TEST") == "1" && filepath.Base(os.Args[0]) == "ipsec" {
		os.Exit(runTestStroke())
	}
	os.Exit(m.Run())
}

// The agent-owned test client connects through the inherited root descriptor,
// exercising a real Unix socket in a different simulated Pod filesystem.
func runTestStroke() int {
	if len(os.Args) != 3 || os.Args[1] != "stroke" {
		return 2
	}
	settings := os.Getenv("STRONGSWAN_CONF")
	data, err := os.ReadFile(settings)
	if err != nil || !strings.Contains(string(data), "socket = unix:///proc/self/fd/3/run/charon.ctl") {
		return 3
	}
	settingsLink, err := os.Readlink(settings)
	if err != nil || !strings.HasSuffix(settingsLink, " (deleted)") {
		return 9
	}
	settingsPath := strings.TrimSuffix(settingsLink, " (deleted)")
	if record := os.Getenv("KO_IPSEC_STROKE_SETTINGS_RECORD"); record != "" {
		if err := os.WriteFile(record, []byte(settingsPath), 0o600); err != nil {
			return 10
		}
	}
	if os.Args[2] == "statusall" && os.Getenv("KO_IPSEC_STROKE_FAIL") == "1" {
		return 17
	}
	connection, err := net.Dial("unix", "/proc/self/fd/3/run/charon.ctl")
	if err != nil {
		return 4
	}
	defer connection.Close()
	if os.Getenv("KO_IPSEC_STROKE_REPORT_PID") == "1" {
		if _, err := fmt.Fprintf(connection, "%d\n", os.Getpid()); err != nil {
			return 8
		}
	}
	if _, err := io.WriteString(connection, os.Args[2]+"\n"); err != nil {
		return 5
	}
	if _, err := io.Copy(os.Stdout, connection); err != nil {
		return 6
	}
	if _, err := os.Stdout.WriteString("settings=" + settingsPath + "\n"); err != nil {
		return 7
	}
	return 0
}

func TestIPsecCollectorProcess(_ *testing.T) {
	proc := os.Getenv("KO_IPSEC_COLLECTOR_PROC")
	if proc == "" {
		return
	}
	if err := collectIPsec(context.Background(), proc, "source", os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestIPsecUsesPinnedPodSocketAndAgentTools(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("host proc directory descriptors are Linux-only")
	}
	proc := t.TempDir()
	root := filepath.Join(proc, "42/root")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "etc"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "run"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(proc, "42/comm"), []byte("charon\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(proc, "42/cgroup"), []byte("0::/kubepods/podsource"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "etc/ipsec.conf"), []byte("active Pod configuration\n"), 0o600))
	// Keep the socket's bind pathname short enough for sockaddr_un.
	socketDir, err := os.MkdirTemp("/tmp", "ko-stroke-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(socketDir)) })
	socket := filepath.Join(socketDir, "charon.ctl")
	listener, err := net.Listen("unix", socket)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })
	require.NoError(t, os.Symlink(socket, filepath.Join(root, "run/charon.ctl")))
	served := make(chan error, 1)
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			var request [64]byte
			n, err := connection.Read(request[:])
			if err == nil {
				_, err = io.WriteString(connection, "active Pod socket: "+string(request[:n]))
			}
			err = errors.Join(err, connection.Close())
			if err != nil {
				served <- err
				return
			}
		}
	}()
	tools := t.TempDir()
	executable, err := os.Executable()
	require.NoError(t, err)
	require.NoError(t, os.Symlink(executable, filepath.Join(tools, "ipsec")))
	t.Setenv("PATH", tools)
	t.Setenv("KO_IPSEC_STROKE_TEST", "1")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	require.NoError(t, collectIPsec(ctx, proc, "source", &stdout, &stderr))
	require.Empty(t, stderr.String())
	require.Contains(t, stdout.String(), "active Pod configuration")
	for _, operation := range []string{"listcacerts", "listcerts", "statusall"} {
		require.Contains(t, stdout.String(), "active Pod socket: "+operation)
	}
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		if path, ok := strings.CutPrefix(line, "settings="); ok {
			require.NoFileExists(t, path, "private stroke settings must be removed")
		}
	}
	select {
	case err := <-served:
		require.NoError(t, err)
	default:
	}
	t.Setenv("KO_IPSEC_STROKE_FAIL", "1")
	require.ErrorContains(t, collectIPsec(ctx, proc, "source", io.Discard, io.Discard), "exit status 17")
	require.ErrorContains(t, collectIPsec(ctx, proc, "other", io.Discard, io.Discard), "no live charon")
}
