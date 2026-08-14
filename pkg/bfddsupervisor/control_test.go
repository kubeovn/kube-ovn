package bfddsupervisor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestControlClientStatusReturnsTypedSessions(t *testing.T) {
	dir := t.TempDir()
	controlPath := filepath.Join(dir, "bfdd-control")
	script := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' 'There are 2 sessions:'
printf '%s\n' 'Session 1'
printf '%s\n' ' id=1 local=10.16.0.2 (p) remote=10.255.255.255 state=Up'
printf '%s\n' 'Session 2'
printf '%s\n' ' id=2 local=fd00::2 (p) remote=fd00::ffff state=Down'
`
	require.NoError(t, os.WriteFile(controlPath, []byte(script), 0o755))

	client := NewControlClient(controlPath, time.Second)
	sessions, err := client.Status(context.Background())
	require.NoError(t, err)
	require.Equal(t, []Session{
		{ID: 1, Local: "10.16.0.2", Remote: "10.255.255.255", State: SessionUp},
		{ID: 2, Local: "fd00::2", Remote: "fd00::ffff", State: SessionDown},
	}, sessions)
}

func TestControlClientRejectsSemanticFailureWithZeroExitCode(t *testing.T) {
	dir := t.TempDir()
	controlPath := filepath.Join(dir, "bfdd-control")
	script := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' 'Unable to complete status request because the beacon is shutting down.'
`
	require.NoError(t, os.WriteFile(controlPath, []byte(script), 0o755))

	client := NewControlClient(controlPath, time.Second)
	_, err := client.Status(context.Background())
	require.ErrorContains(t, err, "rejected")
}

func TestControlClientRejectsUnknownSessionState(t *testing.T) {
	dir := t.TempDir()
	controlPath := filepath.Join(dir, "bfdd-control")
	script := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' 'There are 1 sessions:'
printf '%s\n' ' id=1 local=10.16.0.2 (p) remote=10.255.255.255 state=Unexpected'
`
	require.NoError(t, os.WriteFile(controlPath, []byte(script), 0o755))

	client := NewControlClient(controlPath, time.Second)
	_, err := client.Status(context.Background())
	require.ErrorContains(t, err, "invalid BFD session state")
}

func TestControlClientConfigureAppliesDefaultsAndPassivePeers(t *testing.T) {
	dir := t.TempDir()
	controlPath := filepath.Join(dir, "bfdd-control")
	logPath := filepath.Join(dir, "commands")
	script := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${COMMAND_LOG}"
case "$*" in
  'session new set mintx 100 ms') printf '%s\n' 'Attempting to set mintx to 100,000 us.' ;;
  'session new set minrx 200 ms') printf '%s\n' 'Attempting to set minrx to 200,000 us.' ;;
  'session new set multi 3') printf '%s\n' 'Attempting to set multi to 3.' ;;
  'allow 10.255.255.255') printf '%s\n' 'Allowing connections from 10.255.255.255' ;;
  'allow fd00::ffff') printf '%s\n' 'Allowing connections from fd00::ffff' ;;
  'log type command no') printf '%s\n' 'Log type command set to no, was yes' ;;
  *) exit 2 ;;
esac
`
	require.NoError(t, os.WriteFile(controlPath, []byte(script), 0o755))
	t.Setenv("COMMAND_LOG", logPath)

	client := NewControlClient(controlPath, time.Second)
	err := client.Configure(context.Background(), DaemonConfig{
		MinTXMilliseconds: 100,
		MinRXMilliseconds: 200,
		Multiplier:        3,
		PeerIPs:           []string{"10.255.255.255", "fd00::ffff"},
	})
	require.NoError(t, err)

	commands, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Equal(t, "session new set mintx 100 ms\nsession new set minrx 200 ms\nsession new set multi 3\nallow 10.255.255.255\nallow fd00::ffff\nlog type command no\n", string(commands))
}

func TestControlClientResetUsesAddressPairAndConfirmsGenerationChanged(t *testing.T) {
	dir := t.TempDir()
	controlPath := filepath.Join(dir, "bfdd-control")
	statusMarker := filepath.Join(dir, "status-marker")
	script := `#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  status)
    id=1
    if [[ -e "${STATUS_MARKER}" ]]; then id=2; else touch "${STATUS_MARKER}"; fi
    printf '%s\n' 'There are 1 sessions:'
    printf ' id=%s local=10.16.0.2 (p) remote=10.255.255.255 state=Down\n' "${id}"
    ;;
  'session local 10.16.0.2 remote 10.255.255.255 reset')
    printf '%s\n' 'Attempting to reset session(s).'
    ;;
  *) exit 2 ;;
esac
`
	require.NoError(t, os.WriteFile(controlPath, []byte(script), 0o755))
	t.Setenv("STATUS_MARKER", statusMarker)

	client := NewControlClient(controlPath, time.Second)
	err := client.Reset(context.Background(), SessionPair{Local: "10.16.0.2", Remote: "10.255.255.255"})
	require.NoError(t, err)
}
