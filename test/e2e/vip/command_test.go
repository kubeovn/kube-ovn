package vip

import (
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestVirtualVipFindConditions(t *testing.T) {
	for _, tt := range []struct {
		name string
		ip   string
	}{
		{name: "vip:vip1-123456789:ipv4", ip: "10.246.52.3"},
		{name: "vip:vip1-123456789:ipv6", ip: "fd00::3"},
		{name: "vip1-123456789", ip: "10.246.52.3"},
		{name: "vip1-123456789", ip: "10.246.52.3,fd00::3"},
	} {
		t.Run(tt.name+"/"+tt.ip, func(t *testing.T) {
			// NBExec uses /bin/sh -c. Capture the arguments after shell parsing,
			// where OVN must still receive quoted string values.
			cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", `printf '%s\n' `+virtualVipFindConditions(tt.name, tt.ip))
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("parse conditions through shell: %v: %s", err, output)
			}
			got := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
			want := []string{"type=virtual", "name=" + strconv.Quote(tt.name), "options:virtual-ip=" + strconv.Quote(tt.ip)}
			if !slices.Equal(got, want) {
				t.Fatalf("arguments after shell parsing = %q, want %q", got, want)
			}
		})
	}
}
