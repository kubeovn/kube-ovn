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
	testCases := []struct {
		name          string
		timeoutTarget string
		file          string
		want          []string
	}{
		{"available", "", "ovs-fixture-ovs.txt", []string{"OVS snapshot fixture"}},
		{"discovery-timeout", "discovery", "ovs-discovery.log", []string{"command exit=124"}},
		{"ovs-command-timeout", "ovs", "ovs-fixture-ovs.txt", []string{
			"command: timeout 3 ovs-vsctl show", "command exit=124", "flows fixture", "conntrack fixture",
		}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			directory := prepareNodePortDiagnosticsSnapshot(t, tc.timeoutTarget)
			cmd := exec.CommandContext(t.Context(), "bash", "diagnostics.sh", "snapshot")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("snapshot failed: %v\n%s", err, output)
			}
			output, err := os.ReadFile(filepath.Join(directory, "first-failure", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !strings.Contains(string(output), want) {
					t.Errorf("snapshot missing %q:\n%s", want, output)
				}
			}
		})
	}
}

func prepareNodePortDiagnosticsSnapshot(t *testing.T, timeoutTarget string) string {
	t.Helper()
	directory := t.TempDir()
	t.Setenv("E2E_NODEPORT_DIAGNOSTICS_DIR", directory)
	t.Setenv("NODEPORT_NAMESPACE", "fixture")
	t.Setenv("NODEPORT_PORT", "30080")
	t.Setenv("NODEPORT_BACKEND_PORT", "8080")
	t.Setenv("NODEPORT_TEST_TIMEOUT_TARGET", timeoutTarget)
	realTimeout, err := exec.LookPath("timeout")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("NODEPORT_TEST_REAL_TIMEOUT", realTimeout)
	if err := os.WriteFile(filepath.Join(directory, "nodes.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	// install.sh and both charts label the ovs-ovn DaemonSet's Pods app=ovs.
	// Execute the real remote command group against local tool stubs.
	tools := map[string]string{
		"kubectl": `#!/usr/bin/env bash
set -euo pipefail
case " $* " in
  *" get pods -l app=ovs "*) printf 'ovs-fixture\n' ;;
  *" exec ovs-fixture -c openvswitch "*)
    while [[ $# -gt 0 && $1 != -- ]]; do shift; done
    shift
    exec "$@"
    ;;
esac
`,
		"timeout": `#!/usr/bin/env bash
set -euo pipefail
if [[ $NODEPORT_TEST_TIMEOUT_TARGET == discovery && $1 == 5 ]]; then
  exit 124
fi
if [[ $NODEPORT_TEST_TIMEOUT_TARGET == ovs && $1 == 3 && $2 == ovs-vsctl && $3 == show ]]; then
  exit 124
fi
exec "$NODEPORT_TEST_REAL_TIMEOUT" "$@"
`,
		"ovs-vsctl":  "#!/usr/bin/env bash\nprintf 'OVS snapshot fixture\\n'\n",
		"ovs-ofctl":  "#!/usr/bin/env bash\nprintf 'flows fixture\\n'\n",
		"ovs-appctl": "#!/usr/bin/env bash\nprintf 'conntrack fixture\\n'\n",
	}
	for name, script := range tools {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return directory
}

func TestNodePortDiagnosticsRetainsRuntimeLogs(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("E2E_NODEPORT_DIAGNOSTICS_DIR", directory)
	if err := os.WriteFile(filepath.Join(directory, "nodes.txt"), []byte("kind-fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	docker := "#!/usr/bin/env bash\nprintf 'OVS runtime log fixture\\n'\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(docker), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	runNodePortDiagnostics("stop")
	output, err := os.ReadFile(filepath.Join(directory, "kind-fixture-runtime-logs.txt"))
	if err != nil || !strings.Contains(string(output), "OVS runtime log fixture") {
		t.Fatalf("post-test OVS/OVN logs missing: %v\n%s", err, output)
	}
}
