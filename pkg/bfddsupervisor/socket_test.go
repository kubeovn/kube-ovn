package bfddsupervisor

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSupervisorUnixSocketServesLiveReadyAndStatus(t *testing.T) {
	clock := &fakeClock{now: time.Unix(8_000, 0)}
	control := &fakeControl{sessions: []Session{{
		ID: 1, Local: "10.16.0.2", Remote: "10.255.255.255", State: SessionUp,
	}}}
	child := &fakeChild{running: true}
	supervisor, err := NewSupervisor(SupervisorConfig{
		Daemon: DaemonConfig{MinTXMilliseconds: 100, MinRXMilliseconds: 100, Multiplier: 3, PeerIPs: []string{"10.255.255.255"}},
		PodIPs: []string{"10.16.0.2"}, GracePeriod: time.Minute, StablePeriod: time.Minute,
		Backoffs: []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}, CircuitOpen: time.Hour,
	}, control, child, clock)
	require.NoError(t, err)
	require.NoError(t, supervisor.Reconcile(context.Background()))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	socketPath := filepath.Join(t.TempDir(), "supervisor.sock")
	serverErr := make(chan error, 1)
	go func() { serverErr <- supervisor.Serve(ctx, socketPath) }()
	require.Eventually(t, func() bool {
		_, err := Probe(context.Background(), socketPath, "live")
		return err == nil
	}, time.Second, 10*time.Millisecond)

	live, err := Probe(context.Background(), socketPath, "live")
	require.NoError(t, err)
	require.True(t, live.Live)
	ready, err := Probe(context.Background(), socketPath, "ready")
	require.NoError(t, err)
	require.True(t, ready.Ready)
	status, err := Probe(context.Background(), socketPath, "status")
	require.NoError(t, err)
	require.Len(t, status.Sessions, 1)

	cancel()
	require.NoError(t, <-serverErr)
}
