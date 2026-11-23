package ovs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestICDBHealthcheckValidatesPersistentFiles(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "dist", "images", "ovn-ic-healthcheck.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		nb   string
		sb   string
	}{
		{"healthy", "valid", "valid"},
		{"missing-nb", "", "valid"},
		{"missing-sb", "valid", ""},
		{"invalid-nb", "invalid", "valid"},
		{"invalid-sb", "valid", "invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, db := range []struct{ file, content string }{
				{"ovn_ic_nb_db.db", test.nb},
				{"ovn_ic_sb_db.db", test.sb},
			} {
				if db.content != "" {
					writeRecoveryTestFile(t, filepath.Join(dir, db.file), db.content, 0o600)
				}
			}
			trace := filepath.Join(dir, "commands")
			writeRecoveryTestFile(t, trace, "", 0o600)
			writeRecoveryTestFile(t, filepath.Join(dir, "ovsdb-tool"), `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$TRACE_FILE"
[[ "$1" == check-cluster && -f "$2" && "$(cat "$2")" == valid ]]
`, 0o700)
			writeRecoveryTestFile(t, filepath.Join(dir, "ovn-appctl"), `#!/usr/bin/env bash
set -euo pipefail
printf 'ovn-appctl %s\n' "$*" >> "$TRACE_FILE"
echo 'Status: cluster member'
echo 'Role: leader'
`, 0o700)
			script := strings.ReplaceAll(string(content), "/etc/ovn", "${TEST_DB_DIR}")
			cmd := exec.CommandContext(t.Context(), requireBash(t), "-c", script) // #nosec G204
			cmd.Env = startDBRecoveryEnv(dir, trace, "", "valid")
			output, err := cmd.CombinedOutput()
			wantSuccess := test.name == "healthy"
			if (err == nil) != wantSuccess {
				t.Fatalf("healthcheck success = %v, want %v; output:\n%s", err == nil, wantSuccess, output)
			}
			calls, err := os.ReadFile(trace)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(string(calls), "ovn-appctl"); got != wantSuccess {
				t.Fatalf("runtime queries reached = %v, want %v; calls:\n%s", got, wantSuccess, calls)
			}
			if wantSuccess && strings.Count(string(calls), "check-cluster ") != 2 {
				t.Fatalf("both persistent databases must be checked; calls:\n%s", calls)
			}
		})
	}
}
