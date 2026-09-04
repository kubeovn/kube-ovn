package ha

import (
	"strings"
	"testing"
)

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
