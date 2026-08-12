package main

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/bfddsupervisor"
)

type commandTestControl struct{}

func (commandTestControl) Status(context.Context) ([]bfddsupervisor.Session, error) {
	return []bfddsupervisor.Session{{ID: 1, Local: "10.16.0.2", Remote: "10.255.255.255", State: bfddsupervisor.SessionUp}}, nil
}

func (commandTestControl) Configure(context.Context, bfddsupervisor.DaemonConfig) error { return nil }
func (commandTestControl) Reset(context.Context, bfddsupervisor.SessionPair) error      { return nil }

type commandTestChild struct{}

func (commandTestChild) Running() bool                 { return true }
func (commandTestChild) Restart(context.Context) error { return nil }

type commandTestClock struct{ now time.Time }

func (c commandTestClock) Now() time.Time { return c.now }

func TestEnvironmentConfigBuildsSameFamilySessionsAndBoundedGrace(t *testing.T) {
	t.Setenv("POD_IPS", "10.16.0.2,fd00::2")
	t.Setenv("BFD_PEER_IPS", "10.255.255.255,fd00::ffff")
	t.Setenv("BFD_MIN_TX", "2000")
	t.Setenv("BFD_MIN_RX", "1000")
	t.Setenv("BFD_MULTI", "5")

	config, process, err := configFromEnvironment()
	require.NoError(t, err)
	require.Equal(t, 100*time.Second, config.GracePeriod)
	require.Equal(t, 60*time.Second, config.StablePeriod)
	require.Equal(t, []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second}, config.ChildBackoffs)
	require.Equal(t, []string{"--nofork", "--tee", "--listen=10.16.0.2", "--listen=fd00::2"}, process.Args)
}

func TestStatusCommandUsesSupervisorSocketInterface(t *testing.T) {
	supervisor, err := bfddsupervisor.NewSupervisor(bfddsupervisor.SupervisorConfig{
		Daemon: bfddsupervisor.DaemonConfig{MinTXMilliseconds: 100, MinRXMilliseconds: 100, Multiplier: 3, PeerIPs: []string{"10.255.255.255"}},
		PodIPs: []string{"10.16.0.2"}, GracePeriod: time.Minute, StablePeriod: time.Minute,
		Backoffs: []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}, CircuitOpen: time.Hour,
	}, commandTestControl{}, commandTestChild{}, commandTestClock{now: time.Unix(9_000, 0)})
	require.NoError(t, err)
	require.NoError(t, supervisor.Reconcile(context.Background()))

	ctx, cancel := context.WithCancel(context.Background())
	socketPath := filepath.Join(t.TempDir(), "supervisor.sock")
	serveResult := make(chan error, 1)
	go func() { serveResult <- supervisor.Serve(ctx, socketPath) }()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-serveResult)
	})
	t.Setenv("BFDD_SUPERVISOR_SOCKET", socketPath)
	require.Eventually(t, func() bool {
		_, err := bfddsupervisor.Probe(context.Background(), socketPath, "live")
		return err == nil
	}, time.Second, 10*time.Millisecond)

	var output bytes.Buffer
	require.NoError(t, runCommand(context.Background(), []string{"status"}, &output))
	require.True(t, strings.Contains(output.String(), `"Ready":true`))
}

func TestRunCommandStartsChildAndServesRuntimeHealth(t *testing.T) {
	socketPath := configureRunCommandTest(t)

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runCommand(ctx, []string{"run"}, &bytes.Buffer{}) }()
	require.Eventually(t, func() bool {
		status, err := bfddsupervisor.Probe(context.Background(), socketPath, "live")
		return err == nil && status.Live
	}, 2*time.Second, 10*time.Millisecond)

	status, err := bfddsupervisor.Probe(context.Background(), socketPath, "status")
	require.NoError(t, err)
	require.False(t, status.Ready, "zero sessions must not make the runtime unhealthy")
	cancel()
	require.NoError(t, <-result)
}

func TestRunCommandFailsWhenMetricsServerCannotStart(t *testing.T) {
	configureRunCommandTest(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })
	t.Setenv("BFDD_METRICS_ADDRESS", listener.Addr().String())

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = runCommand(ctx, []string{"run"}, &bytes.Buffer{})
	require.ErrorContains(t, err, "BFD supervisor metrics server")
}

func configureRunCommandTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	beaconPath := filepath.Join(dir, "bfdd-beacon")
	beaconScript := `#!/usr/bin/env bash
set -euo pipefail
trap 'exit 0' TERM
while true; do read -r -t 1 _ || true; done
`
	require.NoError(t, os.WriteFile(beaconPath, []byte(beaconScript), 0o755))
	controlPath := filepath.Join(dir, "bfdd-control")
	controlScript := `#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  status) printf '%s\n' 'There are 0 sessions:' ;;
  'session new set mintx 100 ms') printf '%s\n' 'Attempting to set mintx to 100,000 us.' ;;
  'session new set minrx 100 ms') printf '%s\n' 'Attempting to set minrx to 100,000 us.' ;;
  'session new set multi 3') printf '%s\n' 'Attempting to set multi to 3.' ;;
  'allow 10.255.255.255') printf '%s\n' 'Allowing connections from 10.255.255.255' ;;
  'log type command no') printf '%s\n' 'Log type command set to no, was yes' ;;
  *) exit 2 ;;
esac
`
	require.NoError(t, os.WriteFile(controlPath, []byte(controlScript), 0o755))
	socketPath := filepath.Join(dir, "supervisor.sock")
	t.Setenv("POD_IPS", "10.16.0.2")
	t.Setenv("BFD_PEER_IPS", "10.255.255.255")
	t.Setenv("BFD_MIN_TX", "100")
	t.Setenv("BFD_MIN_RX", "100")
	t.Setenv("BFD_MULTI", "3")
	t.Setenv("BFDD_BEACON_PATH", beaconPath)
	t.Setenv("BFDD_CONTROL_PATH", controlPath)
	t.Setenv("BFDD_SUPERVISOR_SOCKET", socketPath)
	t.Setenv("BFDD_SUPERVISOR_STATE", filepath.Join(dir, "state.json"))
	return socketPath
}
