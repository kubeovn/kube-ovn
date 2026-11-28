package connectivity

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNodePortDiagnosticsFromSuiteDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "diagnostics")
	t.Setenv("E2E_NODEPORT_DIAGNOSTICS_DIR", directory)

	// Go tests and Ginkgo both run from the suite directory. Stop needs no cluster.
	runNodePortDiagnostics("stop")
	if info, err := os.Stat(directory); err != nil || !info.IsDir() {
		t.Fatalf("diagnostic helper did not run from the suite directory: %v", err)
	}
}

func TestNodePortDiagnosticsCollectsOVSPods(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("E2E_NODEPORT_DIAGNOSTICS_DIR", directory)
	t.Setenv("NODEPORT_NAMESPACE", "fixture")
	t.Setenv("NODEPORT_PORT", "30080")
	t.Setenv("NODEPORT_BACKEND_PORT", "8080")
	if err := os.WriteFile(filepath.Join(directory, "nodes.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	// install.sh and both charts label the ovs-ovn DaemonSet's Pods app=ovs.
	kubectl := `#!/usr/bin/env bash
set -euo pipefail
case " $* " in
  *" get pods -l app=ovs "*) printf 'ovs-fixture\n' ;;
  *" exec ovs-fixture -c openvswitch "*) printf 'OVS snapshot fixture\n' ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(kubectl), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd := exec.CommandContext(t.Context(), "bash", "diagnostics.sh", "snapshot")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("snapshot failed: %v\n%s", err, output)
	}
	output, err := os.ReadFile(filepath.Join(directory, "first-failure", "ovs-fixture-ovs.txt"))
	if err != nil || !strings.Contains(string(output), "OVS snapshot fixture") {
		t.Fatalf("OVS Pod diagnostics missing: %v\n%s", err, output)
	}
}
