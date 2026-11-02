package ha

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"

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
			err := copyDbFiles(node, dir, dbFileHostPath("nb"))
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
