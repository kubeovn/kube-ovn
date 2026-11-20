package ha

import (
	"errors"
	"strings"
	"testing"
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

func TestCleanupZeroTailFiles(t *testing.T) {
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
			err := cleanupZeroTailFiles(members, "database.db", test.restore)
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
