package ha

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	corev1 "k8s.io/api/core/v1"

	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

func TestCopyDbFiles(t *testing.T) {
	for _, tt := range []struct {
		name       string
		missingHdr bool
	}{
		{name: "all files copied"},
		{name: "missing header reported", missingHdr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			binDir := t.TempDir()
			script := "#!/bin/sh\nprintf '%s' \"$2\" > \"$3\"\n"
			if tt.missingHdr {
				script = "#!/bin/sh\ncase \"$2\" in *.hdr) echo missing >&2; exit 1;; esac\nprintf '%s' \"$2\" > \"$3\"\n"
			}
			if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", binDir)
			dir := t.TempDir()
			node := &kind.Node{Summary: container.Summary{Names: []string{"/kind-control-plane"}}}
			err := copyDbFiles(node, dir)
			if tt.missingHdr != (err != nil) {
				t.Fatalf("copyDbFiles() error = %v, want missing header = %v", err, tt.missingHdr)
			}
			if tt.missingHdr && !strings.Contains(err.Error(), "ovnnb_db.hdr") {
				t.Fatalf("copyDbFiles() error does not identify the missing file: %v", err)
			}
			for _, db := range [...]string{"nb", "sb"} {
				if _, err := os.Stat(filepath.Join(dir, filepath.Base(dbFileHostPath(db)))); err != nil {
					t.Fatalf("database %s was not copied: %v", db, err)
				}
				_, err := os.Stat(filepath.Join(dir, filepath.Base(raftHeaderFileHostPath(db))))
				if (tt.missingHdr && !os.IsNotExist(err)) || (!tt.missingHdr && err != nil) {
					t.Fatalf("header %s stat error = %v, want missing = %v", db, err, tt.missingHdr)
				}
			}
		})
	}
}

func TestSaveDbFailureDiagnostics(t *testing.T) {
	binDir := t.TempDir()
	docker := "#!/bin/sh\ncase \"$2\" in *ovnsb_db.hdr) echo missing >&2; exit 1;; esac\nprintf '%s' \"$2\" > \"$3\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(docker), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("HA_E2E_DIAGNOSTICS_DIR", t.TempDir())
	node := &kind.Node{Summary: container.Summary{Names: []string{"/kind-control-plane"}}}
	pods := []corev1.Pod{{}, {}}
	pods[0].Name, pods[1].Name = "ovn-central-1", "ovn-central-2"
	for i := range pods {
		pods[i].Spec.NodeName = node.Name()
	}
	lastCheck := &failedCheckCluster{podName: pods[0].Name, command: "ovsdb-tool check-cluster /etc/ovn/ovnnb_db.db", err: errors.New("unhealthy"), stdout: "last stdout", stderr: "last stderr"}
	dir, err := saveDbFailureDiagnostics(pods, map[string]*kind.Node{node.Name(): node}, "check failed", lastCheck, func(pod corev1.Pod, command string) (string, string, error) {
		if strings.Contains(command, "cluster/status") {
			return pod.Name + " status", "", errors.New("status unavailable")
		}
		return pod.Name + " check", "", nil
	})
	if err == nil || !strings.Contains(err.Error(), "ovnsb_db.hdr") || !strings.Contains(err.Error(), "status unavailable") {
		t.Fatalf("snapshot error = %v, want copy and command failures", err)
	}
	for path, want := range map[string]string{
		"failure.txt":                             "check failed",
		"failed-check-cluster.txt":                "last stdout",
		"snapshot-errors.txt":                     "status unavailable",
		filepath.Join(node.Name(), "ovnnb_db.db"): "/etc/origin/ovn/ovnnb_db.db",
		filepath.Join(node.Name(), "ovn-central-1", "nb-check-cluster.txt"):  "ovn-central-1 check",
		filepath.Join(node.Name(), "ovn-central-2", "sb-cluster-status.txt"): "ovn-central-2 status",
	} {
		content, readErr := os.ReadFile(filepath.Join(dir, path))
		if readErr != nil || !strings.Contains(string(content), want) {
			t.Fatalf("file %s: content = %q, error = %v, want %q", path, content, readErr, want)
		}
	}
	failedCheckContent, err := os.ReadFile(filepath.Join(dir, "failed-check-cluster.txt"))
	if err != nil || !strings.Contains(string(failedCheckContent), "error: unhealthy") || !strings.Contains(string(failedCheckContent), "last stderr") {
		t.Fatalf("failed check content = %q, error = %v", failedCheckContent, err)
	}
	if _, err := os.Stat(filepath.Join(dir, node.Name(), "ovnsb_db.hdr")); !os.IsNotExist(err) {
		t.Fatalf("missing header stat error = %v, want not exist", err)
	}
}

func TestSaveDbFailureDiagnosticsWithoutFailedCheck(t *testing.T) {
	t.Setenv("HA_E2E_DIAGNOSTICS_DIR", t.TempDir())
	dir, err := saveDbFailureDiagnostics(nil, nil, "recovery failed", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "failed-check-cluster.txt")); !os.IsNotExist(err) {
		t.Fatalf("failed check file stat error = %v, want not exist", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "failure.txt"))
	if err != nil || !strings.Contains(string(content), "recovery failed") {
		t.Fatalf("failure content = %q, error = %v", content, err)
	}
}

func TestClusterStatusHasAllServers(t *testing.T) {
	const status = `
Status: cluster member
Servers:
    3e81 (3e81 at tcp:[172.18.0.2]:6643) (self)
    68d9 (68d9 at tcp:[172.18.0.4]:6643) last msg 10 ms ago
    007f (007f at tcp:[172.18.0.3]:6643) last msg 10 ms ago
`

	tests := []struct {
		name     string
		output   string
		expected int
		want     bool
	}{
		{name: "all members and healthy status", output: status, expected: 3, want: true},
		{name: "missing member", output: status, expected: 4, want: false},
		{name: "not a cluster member", output: strings.ReplaceAll(status, "Status: cluster member", "Status: joining"), expected: 3, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clusterStatusHasAllServers(tt.output, tt.expected); got != tt.want {
				t.Fatalf("clusterStatusHasAllServers() = %v, want %v", got, tt.want)
			}
		})
	}
}
