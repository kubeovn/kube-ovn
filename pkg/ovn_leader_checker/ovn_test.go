package ovn_leader_checker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	validClusterID = "6d240b86-177e-4f17-aded-ed1b7b364d97"
	validServerID  = "8d77699d-8dc6-4f32-b1ba-b66aad05ba46"
	zeroClusterID  = "00000000-0000-0000-0000-000000000000"
)

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
	t.Setenv("FAKE_RAFT_HEADER", raftHeaderJSON("d401ddf6-deac-4e26-aeb5-cc4ce07f6515", validServerID))
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

func TestOvnDBPreStartFallsBackWhenRaftHeaderCannotRejoin(t *testing.T) {
	dbDir := t.TempDir()
	hdrFile := filepath.Join(dbDir, "ovnnb_db.hdr")
	if err := os.WriteFile(hdrFile, []byte(raftHeaderJSON(zeroClusterID, validServerID)), 0o600); err != nil {
		t.Fatal(err)
	}
	functions := loadStartDBFunctions(t, dbDir)

	harness := fmt.Sprintf(`set -eu
DB_CLUSTER_ADDR=10.0.0.1
NODE_IPS=10.0.0.1,10.0.0.2
NB_CLUSTER_PORT=6643
NB_PORT=6641
DB_ADDRESSES=::
ENABLE_SSL=false
gen_conn_addr() { echo "tcp:$1:$2"; }
gen_listen_addr() { echo "ptcp:$2:[$1]"; }
random_str() { echo abc123; }
ovsdb-tool() {
    case "$1" in
        rejoin-cluster) : > "$2"; return 1 ;;
        create) : > "$2" ;;
        transact) return 0 ;;
        *) return 1 ;;
    esac
}
%s
ovn_db_pre_start nb
test ! -e %q
test ! -e %q
test -e %q
compgen -G %q >/dev/null
`, functions, filepath.Join(dbDir, "ovnnb_db.db"), hdrFile, filepath.Join(dbDir, "ovnnb_local_config.db"), filepath.Join(dbDir, "ovnnb_db.db.failed-rejoin-*"))

	cmd := exec.Command("bash", "-c", harness)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ovn_db_pre_start did not recover from an invalid raft header: %v\n%s", err, output)
	}
}

func TestOvnDBPreStartFallsBackWhenCorruptDatabaseCannotRejoin(t *testing.T) {
	dbDir := t.TempDir()
	dbFile := filepath.Join(dbDir, "ovnnb_db.db")
	hdrFile := filepath.Join(dbDir, "ovnnb_db.hdr")
	if err := os.WriteFile(dbFile, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hdrFile, []byte(raftHeaderJSON(zeroClusterID, validServerID)), 0o600); err != nil {
		t.Fatal(err)
	}
	functions := loadStartDBFunctions(t, dbDir)

	harness := fmt.Sprintf(`set -eu
DB_CLUSTER_ADDR=10.0.0.1
NODE_IPS=10.0.0.1,10.0.0.2
NB_CLUSTER_PORT=6643
NB_PORT=6641
DB_ADDRESSES=::
ENABLE_SSL=false
gen_conn_addr() { echo "tcp:$1:$2"; }
gen_listen_addr() { echo "ptcp:$2:[$1]"; }
random_str() { echo abc123; }
ovsdb-tool() {
    case "$1" in
        db-name) echo Wrong_Database ;;
        db-is-clustered) return 1 ;;
        rejoin-cluster) : > "$2"; return 1 ;;
        create) : > "$2" ;;
        transact) return 0 ;;
        *) return 1 ;;
    esac
}
%s
ovn_db_pre_start nb
test ! -e %q
test ! -e %q
test -e %q
compgen -G %q >/dev/null
compgen -G %q >/dev/null
compgen -G %q >/dev/null
`, functions, dbFile, hdrFile, filepath.Join(dbDir, "ovnnb_local_config.db"), dbFile+".backup-*", dbFile+".failed-rejoin-*", hdrFile+".invalid-*")

	cmd := exec.Command("bash", "-c", harness)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ovn_db_pre_start did not recover from a corrupt database and invalid raft header: %v\n%s", err, output)
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

func loadStartDBFunctions(t *testing.T, dbDir string) string {
	t.Helper()
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	startDBPath := filepath.Join(repoRoot, "dist", "images", "start-db.sh")
	content, err := os.ReadFile(startDBPath)
	if err != nil {
		t.Fatal(err)
	}

	rejoinFunction, err := extractShellFunction(string(content), "rejoin_db_from_raft_header")
	if err != nil {
		t.Fatal(err)
	}
	preStartFunction, err := extractShellFunction(string(content), "ovn_db_pre_start")
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(rejoinFunction+"\n"+preStartFunction, "/etc/ovn", dbDir)
}

func extractShellFunction(script, name string) (string, error) {
	startMarker := "function " + name + "() {"
	start := strings.Index(script, startMarker)
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
