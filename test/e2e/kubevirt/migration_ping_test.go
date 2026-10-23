package kubevirt

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newLocalMigrationPing(t *testing.T) *migrationPing {
	t.Helper()
	if _, err := exec.LookPath("ping"); err != nil {
		t.Skip("ping is not installed")
	}
	if err := exec.Command("ping", "-c", "1", "-w", "1", "127.0.0.1").Run(); err != nil {
		t.Skipf("loopback ICMP is unavailable: %v", err)
	}
	probe := &migrationPing{
		dir: filepath.Join(t.TempDir(), "probe's output"),
		exec: func(command string) (string, string, error) {
			cmd := exec.Command("sh", "-c", command)
			cmd.Env = append(os.Environ(), "LC_ALL=C")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			return stdout.String(), stderr.String(), err
		},
	}
	t.Cleanup(func() {
		if err := probe.cleanup(); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	return probe
}

func waitForMigrationPing(t *testing.T, condition func() error) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		err := condition()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiting for migration ping: %v", err)
		}
	}
}

func TestMigrationPingStopCollectsStatistics(t *testing.T) {
	probe := newLocalMigrationPing(t)
	if err := probe.start("127.0.0.1", 30); err != nil {
		t.Fatal(err)
	}
	waitForMigrationPing(t, probe.ready)
	// Simulate preparation followed by migration while the probe runs independently.
	time.Sleep(time.Second)
	stdout, err := probe.stop()
	if err != nil {
		t.Fatal(err)
	}
	tx, rx, lost, err := parsePingStats(stdout)
	if err != nil || tx < 2 || rx != tx || lost != 0 {
		t.Fatalf("unexpected statistics: tx=%d rx=%d lost=%d err=%v; output: %s", tx, rx, lost, err, stdout)
	}
}

func TestMigrationPingRejectsEarlyExit(t *testing.T) {
	probe := newLocalMigrationPing(t)
	if err := probe.start("127.0.0.1", 1); err != nil {
		t.Fatal(err)
	}
	waitForMigrationPing(t, func() error {
		_, err := os.Stat(filepath.Join(probe.dir, "done"))
		return err
	})
	if err := probe.ready(); err == nil {
		t.Fatal("an expired probe must not be ready")
	}
	if _, err := probe.stop(); err == nil || !strings.Contains(err.Error(), "ping exited before") {
		t.Fatalf("expected premature exit error, got %v", err)
	}
}

func TestMigrationPingCleanupStopsRunningProbe(t *testing.T) {
	probe := newLocalMigrationPing(t)
	if err := probe.start("127.0.0.1", 30); err != nil {
		t.Fatal(err)
	}
	waitForMigrationPing(t, probe.ready)
	if err := probe.cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(probe.dir); !os.IsNotExist(err) {
		t.Fatalf("probe directory remains after cleanup: %v", err)
	}
}
