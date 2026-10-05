package ipsec

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRuntimeStopsChildrenAfterLeaderCrash(t *testing.T) {
	r := &runtimeManager{priority: 0}
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	c, err := r.startChild("/bin/sh", "-c", `sleep 60 & echo $! > "$1"; wait`, "runtime-test", pidFile)
	require.NoError(t, err)
	t.Cleanup(c.stop)
	var pid int
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
		return err == nil
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, syscall.Kill(c.cmd.Process.Pid, syscall.SIGKILL))
	select {
	case <-c.done:
	case <-time.After(time.Second):
		t.Fatal("runtime leader did not exit")
	}
	c.stop()
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
		// A zombie has terminated and cannot retain the IKE ports; the host PID
		// namespace's init is responsible for reaping an orphaned descendant.
		return os.IsNotExist(err) || (err == nil && strings.Contains(string(data), ") Z "))
	}, time.Second, 10*time.Millisecond)
}

func TestProbesUseThePrivateSocket(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	a := &Agent{config: Configuration{RuntimeDir: t.TempDir()}, runtime: &runtimeManager{}}
	a.beat.Store(time.Now().UnixNano())
	require.NoError(t, a.serveStatus(ctx))
	require.NoError(t, Check(t.Context(), a.config.RuntimeDir, "livez"))
	require.Error(t, Check(t.Context(), a.config.RuntimeDir, "readyz"))
	status := Status{
		Phase: "Configured", NodeUID: "synthetic-node", Chassis: "synthetic-chassis", Generation: "synthetic",
		CertificateHash: digest([]byte("certificate")), TrustHash: digest([]byte("trust")), Expires: time.Now().Add(time.Hour),
	}
	a.runtime.expectConfiguration(status)
	a.setStatus(status)
	a.runtime.healthy.Store(true)
	require.Error(t, Check(t.Context(), a.config.RuntimeDir, "readyz"), "process health is insufficient before the monitor acknowledges this identity")
	a.runtime.applied.Store(true)
	require.Error(t, Check(t.Context(), a.config.RuntimeDir, "readyz"), "an acknowledged identity without live protection must not be ready")
	require.Equal(t, "Configured", a.Status().Phase)
	require.True(t, a.Status().ConfigurationApplied)
	next := status
	next.Generation = "replacement"
	next.TrustHash = digest([]byte("replacement trust"))
	a.runtime.expectConfiguration(next)
	require.Error(t, Check(t.Context(), a.config.RuntimeDir, "readyz"), "new trust must invalidate the previous acknowledgement")
	a.runtime.applied.Store(true)
	require.Error(t, Check(t.Context(), a.config.RuntimeDir, "readyz"), "even a new monitor acknowledgement must not authorize the old status during activation")
	a.setStatus(next)
	a.runtime.expectConfiguration(next)
	require.True(t, a.Status().ConfigurationApplied, "unchanged public content must preserve the acknowledgement")
	require.Error(t, Check(t.Context(), a.config.RuntimeDir, "readyz"), "public content confirmation cannot substitute for kernel protection")
	for _, field := range []string{"nodeUID", "chassis", "generation", "certificate", "trust"} {
		t.Run(field, func(t *testing.T) {
			stale := next
			switch field {
			case "nodeUID":
				stale.NodeUID = "replacement-node"
			case "chassis":
				stale.Chassis = "replacement-chassis"
			case "generation":
				stale.Generation = status.Generation
			case "certificate":
				stale.CertificateHash = status.TrustHash
			case "trust":
				stale.TrustHash = status.TrustHash
			}
			a.setStatus(stale)
			require.False(t, a.Status().ConfigurationApplied)
			require.Error(t, Check(t.Context(), a.config.RuntimeDir, "readyz"), "confirmation must match the currently reported configuration")
		})
	}
	a.setStatus(next)
	a.runtime.healthy.Store(false)
	require.False(t, a.Status().ConfigurationApplied, "a stopped runtime cannot retain configuration confirmation")
	require.Error(t, Check(t.Context(), a.config.RuntimeDir, "readyz"))
	a.runtime.healthy.Store(true)
	next.Expires = time.Now().Add(-time.Second)
	a.setStatus(next)
	require.Error(t, Check(t.Context(), a.config.RuntimeDir, "readyz"))
	next.Phase, next.Expires = "Degraded", time.Now().Add(time.Hour)
	a.setStatus(next)
	require.Error(t, Check(t.Context(), a.config.RuntimeDir, "readyz"))
	a.beat.Store(time.Now().Add(-10 * time.Minute).UnixNano())
	require.Error(t, Check(t.Context(), a.config.RuntimeDir, "livez"))
}
