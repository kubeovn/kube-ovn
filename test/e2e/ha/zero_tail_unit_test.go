package ha

import (
	"errors"
	"strings"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega/gbytes"
)

type zeroTailTestNode struct {
	name   string
	fail   string
	called []string
}

func (node *zeroTailTestNode) Name() string { return node.name }

func (node *zeroTailTestNode) Exec(command ...string) ([]byte, []byte, error) {
	node.called = append(node.called, strings.Join(command, " "))
	if command[0] == node.fail {
		return nil, []byte("injected failure"), errors.New("injected failure")
	}
	return nil, nil, nil
}

func TestCleanupZeroTailRecovery(t *testing.T) {
	for _, test := range []struct {
		name    string
		fail    string
		restore bool
	}{
		{name: "successful restore", restore: true},
		{name: "restore failure retains all backups", fail: "cp", restore: true},
		{name: "delete failure continues after restore", fail: "rm", restore: true},
		{name: "successful recovery only removes backups"},
	} {
		t.Run(test.name, func(t *testing.T) {
			first := &zeroTailTestNode{name: "first", fail: test.fail}
			second := &zeroTailTestNode{name: "second"}
			members := map[string]*zeroTailMember{
				"first":  {node: first, backup: "first.backup", backedUp: true},
				"second": {node: second, backup: "second.backup", backedUp: true},
			}
			restarts := 0
			err := cleanupZeroTailRecovery(members, "database.db", test.restore, func() error {
				restarts++
				for _, node := range []*zeroTailTestNode{first, second} {
					calls := strings.Join(node.called, "\n")
					if strings.Contains(calls, "cp ") != test.restore || strings.Contains(calls, "rm ") {
						t.Errorf("files must be restored and backups retained before restart: %s", calls)
					}
				}
				return nil
			})
			if restarts != 1 {
				t.Errorf("restart attempts = %d, want 1 even after file recovery errors", restarts)
			}
			if (err != nil) != (test.fail != "") {
				t.Fatalf("cleanup error = %v, failure = %q", err, test.fail)
			}
			for _, node := range []*zeroTailTestNode{first, second} {
				calls := strings.Join(node.called, "\n")
				if strings.Contains(calls, "cp ") != test.restore {
					t.Errorf("restore attempted on %s: %s", node.name, calls)
				}
				wantRemove := test.fail != "cp"
				if strings.Contains(calls, "rm ") != wantRemove {
					t.Errorf("backup removal on %s: got %s, want removal=%t", node.name, calls, wantRemove)
				}
			}
		})
	}
}

func TestZeroTailBackupsSurviveFailedRestart(t *testing.T) {
	for _, failWithPanic := range []bool{false, true} {
		name := "restart returns an error"
		if failWithPanic {
			name = "rollout assertion panics"
		}
		t.Run(name, func(t *testing.T) {
			node := &zeroTailTestNode{name: "member"}
			members := map[string]*zeroTailMember{
				"member": {node: node, backup: "member.backup", backedUp: true},
			}
			restarted, panicked := false, false
			var cleanupErr error
			func() {
				defer func() { panicked = recover() != nil }()
				cleanupErr = cleanupZeroTailRecovery(members, "database.db", true, func() error {
					restarted = true
					if failWithPanic {
						panic("injected rollout assertion")
					}
					return errors.New("injected restart failure")
				})
			}()
			if !restarted || panicked != failWithPanic || (!failWithPanic && cleanupErr == nil) {
				t.Fatalf("restart attempted=%t, panic=%t, cleanup error=%v", restarted, panicked, cleanupErr)
			}
			if calls := strings.Join(node.called, "\n"); !strings.Contains(calls, "cp ") || strings.Contains(calls, "rm ") {
				t.Fatalf("backup must be restored and retained after failed ensemble restart: %s", calls)
			}
		})
	}
}

func TestZeroTailRestoreErrorLoggedBeforeRestartPanic(t *testing.T) {
	output := gbytes.NewBuffer()
	ginkgo.GinkgoWriter.TeeTo(output)
	t.Cleanup(ginkgo.GinkgoWriter.ClearTeeWriters)
	node := &zeroTailTestNode{name: "failed-member", fail: "cp"}
	members := map[string]*zeroTailMember{
		"failed-member": {node: node, backup: "member.backup", backedUp: true},
	}
	panicked := false
	var cleanupErr error
	func() {
		defer func() { panicked = recover() != nil }()
		cleanupErr = cleanupZeroTailRecovery(members, "database.db", true, func() error {
			panic("injected rollout assertion")
		})
	}()
	if !panicked {
		t.Fatalf("restart assertion must propagate; cleanup returned %v", cleanupErr)
	}
	for _, want := range []string{"restore failed-member", "stderr=injected failure"} {
		if logged := string(output.Contents()); !strings.Contains(logged, want) {
			t.Errorf("restore diagnostic %q lost when restart panicked: %s", want, logged)
		}
	}
	if calls := strings.Join(node.called, "\n"); strings.Contains(calls, "rm ") {
		t.Fatalf("failed recovery must retain its backup: %s", calls)
	}
}
