package security

import "testing"

func TestContainerNameForProcess(t *testing.T) {
	tests := map[string]string{
		"kube-ovn-daemon":     "cni-server",
		"ovsdb-server":        "ovn-central",
		"kube-ovn-controller": "kube-ovn-controller",
	}
	for process, want := range tests {
		if got := containerNameForProcess(process); got != want {
			t.Errorf("containerNameForProcess(%q) = %q, want %q", process, got, want)
		}
	}
}
