package ovs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStartDBRebuildsInvalidDatabasesFromRaftHeader(t *testing.T) {
	for _, db := range []struct {
		script string
		kind   string
		name   string
		file   string
	}{
		{"start-db.sh", "nb", "OVN_Northbound", "ovnnb_db.db"},
		{"start-db.sh", "sb", "OVN_Southbound", "ovnsb_db.db"},
		{"start-ic-db.sh", "ic_nb", "OVN_IC_Northbound", "ovn_ic_nb_db.db"},
		{"start-ic-db.sh", "ic_sb", "OVN_IC_Southbound", "ovn_ic_sb_db.db"},
	} {
		for _, mode := range []string{"valid", "standalone", "missing", "entry-gap", "committed-truncation", "invalid-header", "header-mismatch", "no-quorum", "bad-name", "wrong-name", "unknown-format"} {
			t.Run(db.kind+"/"+mode, func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, db.file)
				original := "original committed history\n"
				header := strings.TrimSuffix(path, ".db") + ".hdr"
				if mode != "missing" {
					writeRecoveryTestFile(t, path, original, 0o600)
					headerContent := fmt.Sprintf(`{"server_id":"60217a20-b040-482e-b898-5f030c4b9f68","name":"%s","cluster_id":"59a70e9f-0492-4f1f-a5f0-8e5d7c5a6b38"}`, db.name)
					switch mode {
					case "invalid-header":
						headerContent = "invalid raft header"
					case "header-mismatch":
						headerContent = strings.ReplaceAll(headerContent, "59a70e9f-0492-4f1f-a5f0-8e5d7c5a6b38", "3e2a6f11-0b6f-4bb0-9b39-6f6ea4ce6e85")
					}
					writeRecoveryTestFile(t, header, headerContent, 0o600)
				}
				trace := filepath.Join(dir, "commands")
				writeRecoveryTestFile(t, trace, "", 0o600)
				writeRecoveryTestFile(t, filepath.Join(dir, "ovsdb-tool"), recoveryToolStub, 0o700)
				command := startDBRecoveryFunctions(t, db.script) + recoveryShellStubs +
					"\ntrap quit EXIT\novn_db_pre_start " + db.kind + "\nprintf 'started\\n' >> \"$TRACE_FILE\"\n"
				for range 2 {
					cmd := exec.CommandContext(t.Context(), requireBash(t), "-c", "set -eo pipefail\n"+command) // #nosec G204
					cmd.Env = startDBRecoveryEnv(dir, trace, db.name, mode)
					output, err := cmd.CombinedOutput()
					wantSuccess := mode == "valid" || mode == "standalone" || mode == "missing" || mode == "entry-gap" || mode == "committed-truncation"
					if (err == nil) != wantSuccess {
						t.Errorf("startup success = %v, want %v; output:\n%s", err == nil, wantSuccess, output)
					}
					got, err := os.ReadFile(path)
					switch mode {
					case "missing":
						if !os.IsNotExist(err) {
							t.Fatalf("missing database was unexpectedly replaced: %v", err)
						}
					case "entry-gap", "committed-truncation":
						if err != nil || string(got) != "rebuilt\n" {
							t.Fatalf("rebuilt database was not installed: %v; content: %q", err, got)
						}
						matches, globErr := filepath.Glob(path + ".backup-*")
						if globErr != nil || len(matches) != 1 {
							t.Fatalf("original database was not preserved: %v, %v", matches, globErr)
						}
						backup, readErr := os.ReadFile(matches[0])
						if readErr != nil || string(backup) != original {
							t.Fatalf("original database backup is incorrect: %v; content: %q", readErr, backup)
						}
					default:
						if err != nil || string(got) != original {
							t.Fatalf("original database was changed or moved: %v; content: %q", err, got)
						}
					}
					if mode != "missing" {
						got, err := os.ReadFile(header)
						expectedHeader := fmt.Sprintf(`{"server_id":"60217a20-b040-482e-b898-5f030c4b9f68","name":"%s","cluster_id":"59a70e9f-0492-4f1f-a5f0-8e5d7c5a6b38"}`, db.name)
						switch mode {
						case "invalid-header":
							expectedHeader = "invalid raft header"
						case "header-mismatch":
							expectedHeader = strings.ReplaceAll(expectedHeader, "59a70e9f-0492-4f1f-a5f0-8e5d7c5a6b38", "3e2a6f11-0b6f-4bb0-9b39-6f6ea4ce6e85")
						}
						if err != nil || string(got) != expectedHeader {
							t.Fatalf("original header was changed or moved: %v; content: %q", err, got)
						}
					}
				}
				calls, err := os.ReadFile(trace)
				if err != nil {
					t.Fatal(err)
				}
				checkRecoveryValidationTrace(t, mode, string(calls))
				if mode == "entry-gap" || mode == "committed-truncation" {
					for _, required := range []string{"rejoin-cluster"} {
						if !strings.Contains(string(calls), required) {
							t.Errorf("expected recovery command %q was not called:\n%s", required, calls)
						}
					}
				}
				if mode != "valid" && mode != "standalone" && mode != "missing" && mode != "entry-gap" && mode != "committed-truncation" && strings.Contains(string(calls), "started") {
					t.Errorf("startup continued after failed validation:\n%s", calls)
				}
				for _, pattern := range []string{path + ".repaired-*", path + ".rejoin-*", filepath.Join(dir, "*.hdr.*")} {
					matches, err := filepath.Glob(pattern)
					if err != nil || len(matches) != 0 {
						t.Errorf("unexpected recovery artifacts: %v, %v", matches, err)
					}
				}
			})
		}
	}
}

func TestStartDBResumesPendingJoins(t *testing.T) {
	for _, db := range []struct {
		script string
		kind   string
		name   string
		file   string
	}{
		{"start-db.sh", "nb", "OVN_Northbound", "ovnnb_db.db"},
		{"start-db.sh", "sb", "OVN_Southbound", "ovnsb_db.db"},
		{"start-ic-db.sh", "ic_nb", "OVN_IC_Northbound", "ovn_ic_nb_db.db"},
		{"start-ic-db.sh", "ic_sb", "OVN_IC_Southbound", "ovn_ic_sb_db.db"},
	} {
		for _, age := range []int{30, 180} {
			t.Run(fmt.Sprintf("%s/age-%d", db.kind, age), func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, db.file)
				header := strings.TrimSuffix(path, ".db") + ".hdr"
				writeRecoveryTestFile(t, path, "pending join", 0o600)
				writeRecoveryTestFile(t, header, "pending header", 0o600)
				trace := filepath.Join(dir, "commands")
				writeRecoveryTestFile(t, trace, "", 0o600)
				writeRecoveryTestFile(t, filepath.Join(dir, "ovsdb-tool"), recoveryToolStub, 0o700)
				command := startDBRecoveryFunctions(t, db.script) + recoveryShellStubs + fmt.Sprintf(`
function stat { echo 100; }
function date { echo %d; }
trap quit EXIT
ovn_db_pre_start %s
`, 100+age, db.kind)
				cmd := exec.CommandContext(t.Context(), requireBash(t), "-c", "set -eo pipefail\n"+command) // #nosec G204
				cmd.Env = startDBRecoveryEnv(dir, trace, db.name, "pending-join")
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("failed to resume pending join: %v\n%s", err, output)
				}
				for _, file := range []struct{ path, content, archive string }{
					{path, "pending join", ".failed-join-"},
					{header, "pending header", ".invalid-"},
				} {
					readPath := file.path
					if db.script == "start-db.sh" && age >= 120 {
						if _, err := os.Stat(readPath); !os.IsNotExist(err) {
							t.Fatalf("stale pending file was not archived: %s, %v", readPath, err)
						}
						readPath += fmt.Sprintf("%s%d-test", file.archive, 100+age)
					}
					content, err := os.ReadFile(readPath)
					if err != nil || string(content) != file.content {
						t.Fatalf("pending file was not preserved: %s, %v, %q", readPath, err, content)
					}
				}
			})
		}
	}
}

func TestStartDBQuitPreservesExitStatus(t *testing.T) {
	for _, script := range []string{"start-db.sh", "start-ic-db.sh"} {
		for _, status := range []int{0, 7} {
			for _, cleanupStatus := range []int{0, 3} {
				t.Run(fmt.Sprintf("%s/exit-%d/cleanup-%d", script, status, cleanupStatus), func(t *testing.T) {
					command := startDBRecoveryFunctions(t, script) + fmt.Sprintf(`
function /usr/share/ovn/scripts/ovn-ctl { return %d; }
trap quit EXIT
exit %d
`, cleanupStatus, status)
					cmd := exec.CommandContext(t.Context(), requireBash(t), "-c", "set -eo pipefail\n"+command) // #nosec G204
					cmd.Env = startDBRecoveryEnv(t.TempDir(), "", "", "valid")
					output, err := cmd.CombinedOutput()
					if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != status {
						t.Fatalf("exit status should be %d: %v; output:\n%s", status, err, output)
					}
				})
			}
		}
	}
}

func startDBRecoveryFunctions(t *testing.T, name string) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate start-db scripts")
	}
	content, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "..", "dist", "images", name))
	if err != nil {
		t.Fatal(err)
	}
	script := string(content)
	start := strings.Index(script, "function ")
	end := strings.Index(script, "\ntrap quit EXIT")
	if start == -1 || end < start {
		t.Fatal("failed to find startup functions")
	}
	functions := strings.ReplaceAll(script[start:end], "/etc/ovn", "${TEST_DB_DIR}")
	modeScript := filepath.Join(filepath.Dir(filename), "..", "..", "dist", "images", "ovn-db-mode.sh")
	return strings.ReplaceAll(functions, "/kube-ovn/ovn-db-mode.sh", "'"+strings.ReplaceAll(modeScript, "'", "'\\''")+"'")
}

func startDBRecoveryEnv(dir, trace, name, mode string) []string {
	return append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "TEST_DB_DIR="+dir,
		"TRACE_FILE="+trace, "TEST_DB_NAME="+name, "TEST_MODE="+mode,
		"NODE_IPS=10.0.0.1,10.0.0.2,10.0.0.3", "POD_IP=10.0.0.1", "DB_CLUSTER_ADDR=10.0.0.1", "ENABLE_SSL=false")
}

func checkRecoveryValidationTrace(t *testing.T, mode, calls string) {
	t.Helper()
	var expected string
	switch mode {
	case "valid", "entry-gap", "committed-truncation", "invalid-header", "header-mismatch", "no-quorum":
		expected = "check-cluster"
	case "bad-name", "wrong-name":
		expected = "db-name"
	case "standalone", "unknown-format":
		expected = "db-is-standalone"
	default:
		return
	}
	if !strings.Contains(calls, expected) {
		t.Fatalf("expected validation command %q was not reached:\n%s", expected, calls)
	}
}

func writeRecoveryTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

const recoveryShellStubs = `
POD_IP=10.0.0.1
DB_CLUSTER_ADDR=$POD_IP
NODE_IPS=10.0.0.1,10.0.0.2,10.0.0.3
ENABLE_SSL=false
NB_CLUSTER_PORT=6643
SB_CLUSTER_PORT=6644
UUID_RE='[0-9a-fA-F-]{36}'
function random_str { echo test; }
function get_live_cluster_id { [[ "$TEST_MODE" != no-quorum ]] || return 1; echo 59a70e9f-0492-4f1f-a5f0-8e5d7c5a6b38; }
function create_local_config { echo local-config >> "$TRACE_FILE"; }
function /usr/share/ovn/scripts/ovn-ctl { echo "ovn-ctl $*" >> "$TRACE_FILE"; }
`

// The stub models a damaged local log rebuilt from a validated Raft header.
const recoveryToolStub = `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$TRACE_FILE"
case "$1" in
    db-name)
        [[ "$TEST_MODE" != bad-name ]] || exit 1
        if [[ "$TEST_MODE" == wrong-name ]]; then echo unexpected_database; else echo "$TEST_DB_NAME"; fi
        ;;
    db-is-clustered)
        [[ "$TEST_MODE" != unknown-format ]] || exit 1
        [[ "$TEST_MODE" != standalone ]] || exit 2
        ;;
    db-is-standalone)
        [[ "$TEST_MODE" == standalone ]]
        ;;
    check-cluster)
        [[ "$TEST_MODE" != valid ]] || exit 0
        if [[ "$TEST_MODE" == pending-join ]]; then
            echo "$2 has not joined the cluster, omitting"
            echo 'all logs are empty' >&2
            exit 1
        fi
        [[ "$(cat "$2")" != 'repaired short history' && "$(cat "$2")" != rebuilt ]] || exit 0
        if [[ "$TEST_MODE" == committed-truncation ]]; then
            echo 'attempts to truncate log from 13241 to 7015 entries, but commit index is already 13240' >&2
        else
            echo 'advances commit index to 7015 but last log index is 7014' >&2
        fi
        exit 1
        ;;
    rejoin-cluster)
        [[ "$TEST_MODE" != invalid-header && "$TEST_MODE" != header-mismatch && "$TEST_MODE" != no-quorum ]] || exit 1
        printf 'rebuilt\n' > "$2"
        ;;
    *) echo "unexpected ovsdb-tool command: $*" >&2; exit 1 ;;
esac
`
