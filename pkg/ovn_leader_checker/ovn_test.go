package ovn_leader_checker

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
)

const (
	validClusterID = "6d240b86-177e-4f17-aded-ed1b7b364d97"
	validServerID  = "8d77699d-8dc6-4f32-b1ba-b66aad05ba46"
	zeroClusterID  = "00000000-0000-0000-0000-000000000000"
	otherClusterID = "d401ddf6-deac-4e26-aeb5-cc4ce07f6515"
)

func TestObserveDuplicateLeaderRequiresConsecutiveObservations(t *testing.T) {
	cfg := &Configuration{}

	for i := 1; i < maxDuplicateLeaderObservations; i++ {
		count, confirmed := cfg.observeDuplicateLeader(ovnnb.DatabaseName, true)
		if confirmed {
			t.Fatalf("duplicate leader confirmed after only %d observations", i)
		}
		if count != i {
			t.Fatalf("unexpected duplicate leader observation count: got %d, want %d", count, i)
		}
	}

	count, confirmed := cfg.observeDuplicateLeader(ovnnb.DatabaseName, false)
	if confirmed || count != 0 {
		t.Fatalf("normal leader observation did not reset state: count = %d, confirmed = %t", count, confirmed)
	}

	for i := 1; i <= maxDuplicateLeaderObservations; i++ {
		count, confirmed = cfg.observeDuplicateLeader(ovnnb.DatabaseName, true)
		if count != i {
			t.Fatalf("unexpected duplicate leader observation count after reset: got %d, want %d", count, i)
		}
		if confirmed != (i == maxDuplicateLeaderObservations) {
			t.Fatalf("confirmation after observation %d: got %t, want %t", i, confirmed, i == maxDuplicateLeaderObservations)
		}
	}
}

func TestCheckDuplicateDBLeaderPreservesObservationsOnUnknownResult(t *testing.T) {
	queryErr := errors.New("leader query failed")
	tests := []struct {
		name            string
		localLeader     bool
		localQueryErr   error
		remoteAddresses []string
		queryLeader     dbLeaderQueryFunc
	}{
		{
			name:          "local query failure",
			localQueryErr: queryErr,
		},
		{
			name:            "remote query failure",
			localLeader:     true,
			remoteAddresses: []string{"10.0.0.2"},
			queryLeader: func(_, _ string) (bool, error) {
				return false, queryErr
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Configuration{
				remoteAddresses: tt.remoteAddresses,
				duplicateLeaderObservations: map[string]int{
					ovnnb.DatabaseName: 2,
				},
			}

			checkDuplicateDBLeader(cfg, tt.localLeader, tt.localQueryErr, "ovn-nb", ovnnb.DatabaseName, tt.queryLeader)

			if got := cfg.duplicateLeaderObservations[ovnnb.DatabaseName]; got != 2 {
				t.Fatalf("unknown leader result changed observation count: got %d, want 2", got)
			}
			if count, confirmed := cfg.observeDuplicateLeader(ovnnb.DatabaseName, true); count != 3 || !confirmed {
				t.Fatalf("duplicate observation after unknown result was not confirmed: count = %d, confirmed = %t", count, confirmed)
			}
		})
	}
}

func TestCheckDuplicateDBLeaderResetsOnlyConfirmedNormalObservation(t *testing.T) {
	tests := []struct {
		name            string
		localLeader     bool
		remoteAddresses []string
		queryLeader     dbLeaderQueryFunc
	}{
		{
			name: "local server is not leader",
		},
		{
			name:            "all remote servers are not leaders",
			localLeader:     true,
			remoteAddresses: []string{"10.0.0.2", "10.0.0.3"},
			queryLeader: func(_, _ string) (bool, error) {
				return false, nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Configuration{
				remoteAddresses: tt.remoteAddresses,
				duplicateLeaderObservations: map[string]int{
					ovnnb.DatabaseName: 2,
					ovnsb.DatabaseName: 1,
				},
			}

			checkDuplicateDBLeader(cfg, tt.localLeader, nil, "ovn-nb", ovnnb.DatabaseName, tt.queryLeader)

			if _, ok := cfg.duplicateLeaderObservations[ovnnb.DatabaseName]; ok {
				t.Fatal("confirmed normal observation did not reset the database count")
			}
			if got := cfg.duplicateLeaderObservations[ovnsb.DatabaseName]; got != 1 {
				t.Fatalf("reset changed another database count: got %d, want 1", got)
			}
		})
	}
}

func TestBackupRaftHeaderDoesNotReplaceValidHeaderWithZeroClusterID(t *testing.T) {
	dbDir := t.TempDir()
	hdrFile := filepath.Join(dbDir, "ovnnb_db.hdr")
	validHeader := raftHeaderJSON(validClusterID, validServerID)
	if err := os.WriteFile(hdrFile, []byte(validHeader), 0o600); err != nil {
		t.Fatal(err)
	}

	installFakeOvsdbTool(t)
	t.Setenv("FAKE_RAFT_HEADER", raftHeaderJSON(zeroClusterID, validServerID))
	t.Setenv("FAKE_DB_CID", validClusterID)

	backupRaftHeaderAt("nb", dbDir)

	got, err := os.ReadFile(hdrFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != validHeader {
		t.Fatalf("valid raft header was replaced by an invalid header:\n%s", got)
	}
}

func TestBackupRaftHeaderDoesNotReplaceHeaderWhenClusterIDDoesNotMatchDatabase(t *testing.T) {
	dbDir := t.TempDir()
	hdrFile := filepath.Join(dbDir, "ovnnb_db.hdr")
	validHeader := raftHeaderJSON(validClusterID, validServerID)
	if err := os.WriteFile(hdrFile, []byte(validHeader), 0o600); err != nil {
		t.Fatal(err)
	}

	installFakeOvsdbTool(t)
	t.Setenv("FAKE_RAFT_HEADER", raftHeaderJSON(otherClusterID, validServerID))
	t.Setenv("FAKE_DB_CID", validClusterID)

	backupRaftHeaderAt("nb", dbDir)

	got, err := os.ReadFile(hdrFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != validHeader {
		t.Fatalf("valid raft header was replaced by a header from another cluster:\n%s", got)
	}
}

func TestBackupRaftHeaderWritesHeaderMatchingDatabaseClusterID(t *testing.T) {
	dbDir := t.TempDir()
	expected := raftHeaderJSON(validClusterID, validServerID)

	installFakeOvsdbTool(t)
	t.Setenv("FAKE_RAFT_HEADER", expected)
	t.Setenv("FAKE_DB_CID", validClusterID)

	backupRaftHeaderAt("nb", dbDir)

	got, err := os.ReadFile(filepath.Join(dbDir, "ovnnb_db.hdr"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != expected {
		t.Fatalf("unexpected raft header content:\n%s", got)
	}
}

func TestOvnDBPreStartRejoinsLiveClusterWithHeaderServerID(t *testing.T) {
	for name, headerCID := range map[string]string{
		"zero cluster ID":       zeroClusterID,
		"mismatched cluster ID": otherClusterID,
	} {
		t.Run(name, func(t *testing.T) {
			dbDir := t.TempDir()
			hdrFile := filepath.Join(dbDir, "ovnnb_db.hdr")
			if err := os.WriteFile(hdrFile, []byte(raftHeaderJSON(headerCID, validServerID)), 0o600); err != nil {
				t.Fatal(err)
			}

			runOvnDBPreStartHarness(t, dbDir, validClusterID, `
        rejoin-cluster) return 99 ;;
        --cid)
            test "$2" = "$TEST_LIVE_CID"
            test "$3" = --sid
            test "$4" = "`+validServerID+`"
            test "$5" = join-cluster
            test "$7" = OVN_Northbound
            test "$8" = tcp:10.0.0.1:6643
            test "$9" = tcp:10.0.0.2:6643
            printf 'joined-with-live-cluster-id' > "$6"
            ;;
`, `
grep -q 'joined-with-live-cluster-id' "$TEST_DB_FILE"
test ! -e "$TEST_HDR_FILE"
test ! -e "$TEST_CONFIG_FILE"
compgen -G "$TEST_HDR_FILE.invalid-*" >/dev/null
! compgen -G "$TEST_DB_FILE.failed-rejoin-*" >/dev/null
`)
		})
	}
}

func TestOvnDBPreStartRetriesTransientLiveClusterIDLookup(t *testing.T) {
	for name, headerCID := range map[string]string{
		"zero cluster ID":  zeroClusterID,
		"stale cluster ID": otherClusterID,
	} {
		t.Run(name, func(t *testing.T) {
			dbDir := t.TempDir()
			hdrFile := filepath.Join(dbDir, "ovnnb_db.hdr")
			if err := os.WriteFile(hdrFile, []byte(raftHeaderJSON(headerCID, validServerID)), 0o600); err != nil {
				t.Fatal(err)
			}
			enableLookupAttemptCounter(t, dbDir)
			t.Setenv("TEST_LOOKUP_FAILURES", "6")

			runOvnDBPreStartHarness(t, dbDir, validClusterID, `
        --cid)
            test "$2" = "$TEST_LIVE_CID"
            test "$3" = --sid
            test "$4" = "`+validServerID+`"
            printf 'joined-after-transient-lookup-failures' > "$6"
            ;;
`, `
grep -q 'joined-after-transient-lookup-failures' "$TEST_DB_FILE"
test "$(cat "$TEST_LOOKUP_COUNTER")" -eq 7
test ! -e "$TEST_HDR_FILE"
test ! -e "$TEST_CONFIG_FILE"
`)
		})
	}
}

func TestOvnDBPreStartUsesMatchingRaftHeader(t *testing.T) {
	dbDir := t.TempDir()
	hdrFile := filepath.Join(dbDir, "ovnnb_db.hdr")
	if err := os.WriteFile(hdrFile, []byte(raftHeaderJSON(validClusterID, validServerID)), 0o600); err != nil {
		t.Fatal(err)
	}

	runOvnDBPreStartHarness(t, dbDir, validClusterID, `
        rejoin-cluster) printf 'rejoined-from-header' > "$2" ;;
        --cid) return 99 ;;
`, `
grep -q 'rejoined-from-header' "$TEST_DB_FILE"
test -e "$TEST_HDR_FILE"
test ! -e "$TEST_CONFIG_FILE"
	`)
}

func TestOvnDBPreStartUsesValidRaftHeaderWithoutReachablePeer(t *testing.T) {
	dbDir := t.TempDir()
	hdrFile := filepath.Join(dbDir, "ovnnb_db.hdr")
	if err := os.WriteFile(hdrFile, []byte(raftHeaderJSON(validClusterID, validServerID)), 0o600); err != nil {
		t.Fatal(err)
	}

	runOvnDBPreStartHarness(t, dbDir, "", `
        rejoin-cluster) printf 'rejoined-without-reachable-peer' > "$2" ;;
        --cid) return 99 ;;
`, `
grep -q 'rejoined-without-reachable-peer' "$TEST_DB_FILE"
test -e "$TEST_HDR_FILE"
test ! -e "$TEST_CONFIG_FILE"
`)
}

func TestOvnDBPreStartFallsBackWhenMatchingRaftHeaderCannotRejoin(t *testing.T) {
	dbDir := t.TempDir()
	hdrFile := filepath.Join(dbDir, "ovnnb_db.hdr")
	if err := os.WriteFile(hdrFile, []byte(raftHeaderJSON(validClusterID, validServerID)), 0o600); err != nil {
		t.Fatal(err)
	}

	runOvnDBPreStartHarness(t, dbDir, validClusterID, `
        rejoin-cluster) printf 'partial-rejoin' > "$2"; return 1 ;;
        --cid)
            test "$2" = "$TEST_LIVE_CID"
            test "$3" = --sid
            test "$4" = "`+validServerID+`"
            printf 'joined-after-rejoin-failure' > "$6"
            ;;
`, `
grep -q 'joined-after-rejoin-failure' "$TEST_DB_FILE"
test ! -e "$TEST_HDR_FILE"
test ! -e "$TEST_CONFIG_FILE"
compgen -G "$TEST_DB_FILE.failed-rejoin-*" >/dev/null
compgen -G "$TEST_HDR_FILE.invalid-*" >/dev/null
`)
}

func TestOvnDBPreStartFallsBackToCleanBootstrapWithoutLiveCluster(t *testing.T) {
	dbDir := t.TempDir()
	hdrFile := filepath.Join(dbDir, "ovnnb_db.hdr")
	if err := os.WriteFile(hdrFile, []byte(raftHeaderJSON(zeroClusterID, validServerID)), 0o600); err != nil {
		t.Fatal(err)
	}
	enableLookupAttemptCounter(t, dbDir)

	runOvnDBPreStartHarness(t, dbDir, "", `
        rejoin-cluster|--cid) return 99 ;;
`, `
test ! -e "$TEST_DB_FILE"
test ! -e "$TEST_HDR_FILE"
test -e "$TEST_CONFIG_FILE"
test "$(cat "$TEST_LOOKUP_COUNTER")" -eq 7
compgen -G "$TEST_HDR_FILE.invalid-*" >/dev/null
`)
}

func TestOvnDBPreStartDoesNotRetryConflictingLiveClusterIDs(t *testing.T) {
	dbDir := t.TempDir()
	hdrFile := filepath.Join(dbDir, "ovnnb_db.hdr")
	if err := os.WriteFile(hdrFile, []byte(raftHeaderJSON(zeroClusterID, validServerID)), 0o600); err != nil {
		t.Fatal(err)
	}
	enableLookupAttemptCounter(t, dbDir)
	t.Setenv("TEST_LOOKUP_STATUS", "2")
	t.Setenv("TEST_EXPECTED_PRE_START_STATUS", "1")

	runOvnDBPreStartHarness(t, dbDir, "", `
        --cid) return 99 ;;
`, `
test "$(cat "$TEST_LOOKUP_COUNTER")" -eq 1
test -e "$TEST_HDR_FILE"
test ! -e "$TEST_CONFIG_FILE"
`)
}

func TestOvnDBPreStartRecoversWrongDatabaseWithLiveCluster(t *testing.T) {
	dbDir := t.TempDir()
	dbFile := filepath.Join(dbDir, "ovnnb_db.db")
	hdrFile := filepath.Join(dbDir, "ovnnb_db.hdr")
	if err := os.WriteFile(dbFile, []byte("wrong-database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hdrFile, []byte(raftHeaderJSON(zeroClusterID, validServerID)), 0o600); err != nil {
		t.Fatal(err)
	}

	runOvnDBPreStartHarness(t, dbDir, validClusterID, `
        db-name) echo Wrong_Database ;;
        --cid) printf 'recovered-wrong-database' > "$6" ;;
`, `
grep -q 'recovered-wrong-database' "$TEST_DB_FILE"
test ! -e "$TEST_HDR_FILE"
test ! -e "$TEST_CONFIG_FILE"
compgen -G "$TEST_DB_FILE.backup-*" >/dev/null
compgen -G "$TEST_HDR_FILE.invalid-*" >/dev/null
`)
}

func TestOvnDBPreStartRebuildsCorruptClusterWithLiveIDs(t *testing.T) {
	dbDir := t.TempDir()
	dbFile := filepath.Join(dbDir, "ovnnb_db.db")
	hdrFile := filepath.Join(dbDir, "ovnnb_db.hdr")
	if err := os.WriteFile(dbFile, []byte("corrupt-cluster"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hdrFile, []byte(raftHeaderJSON(otherClusterID, validServerID)), 0o600); err != nil {
		t.Fatal(err)
	}

	runOvnDBPreStartHarness(t, dbDir, validClusterID, `
        db-name) echo OVN_Northbound ;;
        db-is-clustered) return 0 ;;
        check-cluster) return 1 ;;
        fix-cluster) return 1 ;;
        db-sid) echo "`+validServerID+`" ;;
        --cid)
            test "$2" = "$TEST_LIVE_CID"
            test "$4" = "`+validServerID+`"
            printf 'rebuilt-corrupt-cluster' > "$6"
            ;;
`, `
grep -q 'rebuilt-corrupt-cluster' "$TEST_DB_FILE"
test ! -e "$TEST_HDR_FILE"
test -e "$TEST_CONFIG_FILE"
compgen -G "$TEST_DB_FILE.backup-*" >/dev/null
compgen -G "$TEST_HDR_FILE.invalid-*" >/dev/null
	`)
}

func TestGetLiveClusterIDReadsPeerCIDWithoutLeader(t *testing.T) {
	binDir := t.TempDir()
	client := filepath.Join(binDir, "ovsdb-client")
	script := `#!/usr/bin/env bash
test "$1" = query
! grep -q '"leader","==",true' <<< "$3"
grep -q '"columns":\["cid"\]' <<< "$3"
case "$2" in
    *10.0.0.2*) printf '%s' '[{"rows":[{"cid":["set",[["uuid","` + validClusterID + `"]]]}]}]' ;;
    *10.0.0.3*) printf '%s' '[{"rows":[{"cid":["set",[["uuid","` + otherClusterID + `"]]]}]}]' ;;
    *) printf '%s' '[{"rows":[]}]' ;;
esac
`
	if err := os.WriteFile(client, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	functions := loadStartDBShell(t, "", "ovndb_query_database", "ovndb_query_cluster_id", "is_valid_uuid", "get_live_cluster_id")

	harness := fmt.Sprintf(`set -eu
PATH=%q:"$PATH"
DB_CLUSTER_ADDR=10.0.0.1
NODE_IPS=10.0.0.1,10.0.0.2
NB_PORT=6641
ENABLE_SSL=false
gen_conn_addr() { echo "tcp:$1:$2"; }
jq() { return 99; }
%s
test "$(get_live_cluster_id nb)" = %q
NODE_IPS=10.0.0.1,10.0.0.2,10.0.0.3
status=0
get_live_cluster_id nb >/dev/null || status=$?
test "$status" -eq 2
`, binDir, functions, validClusterID)
	cmd := exec.Command("bash", "-c", harness)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("get_live_cluster_id did not read the leader CID: %v\n%s", err, output)
	}
}

func installFakeOvsdbTool(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	tool := filepath.Join(binDir, "ovsdb-tool")
	script := `#!/usr/bin/env bash
case "$1" in
    db-raft-header) printf '%s' "$FAKE_RAFT_HEADER" ;;
    db-cid) printf '%s\n' "$FAKE_DB_CID" ;;
    *) exit 1 ;;
esac
`
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func raftHeaderJSON(clusterID, serverID string) string {
	return fmt.Sprintf(`{
  "cluster_id": %q,
  "local_address": "tcp:[10.0.0.1]:6643",
  "name": "OVN_Northbound",
  "server_id": %q
}`, clusterID, serverID)
}

func enableLookupAttemptCounter(t *testing.T, dbDir string) {
	t.Helper()
	counterFile := filepath.Join(dbDir, "lookup-attempts")
	if err := os.WriteFile(counterFile, []byte("0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_LOOKUP_COUNTER", counterFile)
}

func runOvnDBPreStartHarness(t *testing.T, dbDir, liveClusterID, ovsdbToolCases, assertions string) {
	t.Helper()
	functions := loadStartDBFunctions(t, dbDir)
	dbFile := filepath.Join(dbDir, "ovnnb_db.db")
	hdrFile := filepath.Join(dbDir, "ovnnb_db.hdr")
	configFile := filepath.Join(dbDir, "ovnnb_local_config.db")

	harness := fmt.Sprintf(`set -eu
DB_CLUSTER_ADDR=10.0.0.1
NODE_IPS=10.0.0.1,10.0.0.2
NB_CLUSTER_PORT=6643
NB_PORT=6641
DB_ADDRESSES=::
ENABLE_SSL=false
TEST_LIVE_CID=%q
TEST_DB_FILE=%q
TEST_HDR_FILE=%q
TEST_CONFIG_FILE=%q
gen_conn_addr() { echo "tcp:$1:$2"; }
gen_listen_addr() { echo "ptcp:$2:[$1]"; }
random_str() { echo abc123; }
jq() { return 99; }
sleep() { :; }
get_live_cluster_id() {
    local count=1
    if [ -n "${TEST_LOOKUP_COUNTER:-}" ]; then
        count=$(cat "$TEST_LOOKUP_COUNTER")
        count=$((count + 1))
        echo "$count" > "$TEST_LOOKUP_COUNTER"
    fi
    local failures="${TEST_LOOKUP_FAILURES:-0}"
    if ((count <= failures)); then
        return 1
    fi
    if [ -n "${TEST_LOOKUP_STATUS:-}" ]; then
        return "$TEST_LOOKUP_STATUS"
    fi
    if [ -z "$TEST_LIVE_CID" ]; then
        return 1
    fi
    echo "$TEST_LIVE_CID"
}
ovsdb-tool() {
    case "$1" in
%s
        create) : > "$2" ;;
        transact) return 0 ;;
        db-is-clustered) return 1 ;;
        *) return 1 ;;
    esac
}
%s
status=0
ovn_db_pre_start nb || status=$?
test "$status" -eq "${TEST_EXPECTED_PRE_START_STATUS:-0}"
%s
`, liveClusterID, dbFile, hdrFile, configFile, ovsdbToolCases, functions, assertions)

	cmd := exec.Command("bash", "-c", harness)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ovn_db_pre_start harness failed: %v\n%s", err, output)
	}
}

func loadStartDBFunctions(t *testing.T, dbDir string) string {
	t.Helper()
	return loadStartDBShell(t, dbDir,
		"is_valid_uuid",
		"archive_recovery_file",
		"rejoin_db_from_raft_header",
		"recover_clustered_database",
		"create_local_config",
		"ovn_db_pre_start",
	)
}

func loadStartDBShell(t *testing.T, dbDir string, functionNames ...string) string {
	t.Helper()
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	startDBPath := filepath.Join(repoRoot, "dist", "images", "start-db.sh")
	content, err := os.ReadFile(startDBPath)
	if err != nil {
		t.Fatal(err)
	}

	var functions strings.Builder
	readonly, err := extractShellReadonly(string(content), "UUID_RE")
	if err != nil {
		t.Fatal(err)
	}
	functions.WriteString(readonly)
	functions.WriteByte('\n')
	for _, name := range functionNames {
		function, err := extractShellFunction(string(content), name)
		if err != nil {
			t.Fatal(err)
		}
		functions.WriteString(function)
		functions.WriteByte('\n')
	}
	if dbDir == "" {
		return functions.String()
	}
	return strings.ReplaceAll(functions.String(), "/etc/ovn", dbDir)
}

func extractShellReadonly(script, name string) (string, error) {
	prefix := "readonly " + name + "="
	for line := range strings.SplitSeq(script, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line, nil
		}
	}
	return "", fmt.Errorf("readonly variable %s not found", name)
}

func extractShellFunction(script, name string) (string, error) {
	start := -1
	for _, marker := range []string{"function " + name + "() {", "function " + name + " {"} {
		start = strings.Index(script, marker)
		if start != -1 {
			break
		}
	}
	if start == -1 {
		return "", fmt.Errorf("function %s not found", name)
	}

	end := strings.Index(script[start:], "\n}\n")
	if end == -1 {
		return "", fmt.Errorf("end of function %s not found", name)
	}
	end += start + len("\n}")
	return script[start:end], nil
}
