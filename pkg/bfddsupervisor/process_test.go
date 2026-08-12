package bfddsupervisor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProcessControllerRestartsOnlyAfterPreviousGenerationStops(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "child.log")
	scriptPath := filepath.Join(dir, "child")
	script := `#!/usr/bin/env bash
set -euo pipefail
printf 'start %s\n' "$$" >> "${CHILD_LOG}"
trap 'printf "stop %s\n" "$$" >> "${CHILD_LOG}"; exit 0' TERM
while true; do read -r -t 1 _ || true; done
`
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))
	t.Setenv("CHILD_LOG", logPath)

	controller := NewProcessController(ProcessConfig{Path: scriptPath, StopTimeout: time.Second})
	require.NoError(t, controller.Start())
	t.Cleanup(func() { require.NoError(t, controller.Stop(context.Background())) })
	require.Eventually(t, func() bool { return controller.Running() }, time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(logPath)
		return err == nil && strings.HasPrefix(string(data), "start ")
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, controller.Restart(context.Background()))
	require.True(t, controller.Running())
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(logPath)
		if err != nil {
			return false
		}
		lines := strings.Fields(string(data))
		return len(lines) == 6 && lines[0] == "start" && lines[2] == "stop" && lines[4] == "start" && lines[1] == lines[3] && lines[1] != lines[5]
	}, time.Second, 10*time.Millisecond)
}

func TestProcessControllerReturnsUnexpectedChildExitCause(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "child")
	readyPath := filepath.Join(dir, "ready")
	script := `#!/usr/bin/env bash
set -euo pipefail
trap 'exit 42' TERM
: > "${READY_PATH}"
while true; do read -r -t 1 _ || true; done
`
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))
	t.Setenv("READY_PATH", readyPath)

	controller := NewProcessController(ProcessConfig{Path: scriptPath, StopTimeout: time.Second})
	require.NoError(t, controller.Start())
	require.Eventually(t, func() bool {
		_, err := os.Stat(readyPath)
		return err == nil
	}, time.Second, 10*time.Millisecond)

	err := controller.Stop(context.Background())
	require.ErrorContains(t, err, "exit status 42")
}

func TestProcessControllerTreatsForcedKillAsExpectedStop(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "child")
	readyPath := filepath.Join(dir, "ready")
	script := `#!/usr/bin/env bash
set -euo pipefail
trap '' TERM
: > "${READY_PATH}"
while true; do read -r -t 1 _ || true; done
`
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))
	t.Setenv("READY_PATH", readyPath)

	controller := NewProcessController(ProcessConfig{Path: scriptPath, StopTimeout: 10 * time.Millisecond})
	require.NoError(t, controller.Start())
	require.Eventually(t, func() bool {
		_, err := os.Stat(readyPath)
		return err == nil
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, controller.Stop(context.Background()))
}
