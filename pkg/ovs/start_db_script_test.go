package ovs

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStartDBAddressFunctions(t *testing.T) {
	bash := requireBash(t)

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to get current filename")
	}

	for _, script := range []struct {
		name      string
		path      string
		endMarker string
	}{
		{
			name:      "start-db",
			path:      filepath.Join(filepath.Dir(filename), "..", "..", "dist", "images", "start-db.sh"),
			endMarker: "function get_leader_ip",
		},
		{
			name:      "start-ic-db",
			path:      filepath.Join(filepath.Dir(filename), "..", "..", "dist", "images", "start-ic-db.sh"),
			endMarker: "function ovn_db_pre_start",
		},
	} {
		t.Run(script.name, func(t *testing.T) {
			content, err := os.ReadFile(script.path)
			if err != nil {
				t.Fatalf("failed to read %s: %v", script.path, err)
			}

			functions := extractStartDBAddressFunctions(t, string(content), script.endMarker)

			tests := []struct {
				name     string
				command  string
				expected string
			}{
				{
					name:     "hostname connection address",
					command:  "ENABLE_SSL=false; gen_conn_addr ovn-central-0.ovn-central.hcp.svc.cluster.local 6643",
					expected: "tcp:ovn-central-0.ovn-central.hcp.svc.cluster.local:6643",
				},
				{
					name:     "ssl hostname connection address",
					command:  "ENABLE_SSL=true; gen_conn_addr ovn-central-0.ovn-central.hcp.svc.cluster.local 6643",
					expected: "ssl:ovn-central-0.ovn-central.hcp.svc.cluster.local:6643",
				},
				{
					name:     "ipv4 connection address",
					command:  "ENABLE_SSL=false; gen_conn_addr 10.3.0.58 6643",
					expected: "tcp:[10.3.0.58]:6643",
				},
				{
					name:     "ipv6 connection address",
					command:  "ENABLE_SSL=false; gen_conn_addr fd00::58 6643",
					expected: "tcp:[fd00::58]:6643",
				},
				{
					name:     "hostname connection string",
					command:  "ENABLE_SSL=false; NODE_IPS='ovn-central-0.ovn-central.hcp.svc.cluster.local, ovn-central-1.ovn-central.hcp.svc.cluster.local'; gen_conn_str 6641",
					expected: "tcp:ovn-central-0.ovn-central.hcp.svc.cluster.local:6641,tcp:ovn-central-1.ovn-central.hcp.svc.cluster.local:6641",
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					output, err := exec.Command(bash, "-c", functions+"\n"+tt.command).CombinedOutput() // #nosec G204
					if err != nil {
						t.Fatalf("command failed: %v\noutput: %s", err, output)
					}

					got := strings.TrimSpace(string(output))
					if got != tt.expected {
						t.Fatalf("expected %q, got %q", tt.expected, got)
					}
				})
			}
		})
	}
}

func TestStartDBClusterAddressArgsUseFormatter(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to get current filename")
	}

	scriptPath := filepath.Join(filepath.Dir(filename), "..", "..", "dist", "images", "start-db.sh")
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", scriptPath, err)
	}
	script := string(content)

	for _, oldArg := range []string{
		"--db-nb-cluster-local-addr=[$DB_CLUSTER_ADDR]",
		"--db-sb-cluster-local-addr=[$DB_CLUSTER_ADDR]",
		"--db-nb-cluster-remote-addr=[$nb_leader_addr]",
		"--db-sb-cluster-remote-addr=[$sb_leader_addr]",
	} {
		if strings.Contains(script, oldArg) {
			t.Fatalf("cluster address argument %q should use format_ovsdb_addr", oldArg)
		}
	}
}

func TestStartDBExpandsSvcAddressesFromDNSResponse(t *testing.T) {
	bash := requireBash(t)

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to get current filename")
	}

	scriptPath := filepath.Join(filepath.Dir(filename), "..", "..", "dist", "images", "start-db.sh")
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", scriptPath, err)
	}

	functions := extractStartDBAddressFunctions(t, string(content), "function gen_conn_addr")
	command := strings.Join([]string{
		`function getent { case "$2" in ovn-central-0.ovn-central.hcp.svc) echo "10.3.0.67 ovn-central-0.ovn-central.hcp.svc.acme.local" ;; ovn-central-1.ovn-central.hcp.svc) echo "10.3.0.68 ovn-central-1.ovn-central.hcp.svc.acme.local" ;; esac; }`,
		"POD_NAMESPACE=hcp",
		"DB_CLUSTER_ADDR=ovn-central-0.ovn-central.hcp.svc",
		"POD_IP=$DB_CLUSTER_ADDR",
		"NODE_IPS=ovn-central-0.ovn-central.hcp.svc,ovn-central-1.ovn-central.hcp.svc",
		"normalize_raft_addrs",
		"printf '%s\\n%s\\n%s\\n' \"$DB_CLUSTER_ADDR\" \"$POD_IP\" \"$NODE_IPS\"",
	}, "; ")

	output, err := exec.Command(bash, "-c", functions+"\n"+command).CombinedOutput() // #nosec G204
	if err != nil {
		t.Fatalf("command failed: %v\noutput: %s", err, output)
	}

	expected := strings.Join([]string{
		"ovn-central-0.ovn-central.hcp.svc.acme.local",
		"ovn-central-0.ovn-central.hcp.svc.acme.local",
		"ovn-central-0.ovn-central.hcp.svc.acme.local,ovn-central-1.ovn-central.hcp.svc.acme.local",
	}, "\n")
	if got := strings.TrimSpace(string(output)); got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func requireBash(t *testing.T) string {
	t.Helper()

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is required to test start-db.sh snippets")
	}
	return bash
}

func extractStartDBAddressFunctions(t *testing.T, script, endMarker string) string {
	t.Helper()

	start := strings.Index(script, "function format_ovsdb_addr")
	if start == -1 {
		start = firstFunctionIndex(script, "function gen_conn_addr", "function gen_conn_str")
	}
	if start == -1 {
		t.Fatal("failed to find address functions")
	}
	end := strings.Index(script[start:], endMarker)
	if end == -1 {
		t.Fatalf("failed to find %s", endMarker)
	}
	return script[start : start+end]
}

func firstFunctionIndex(script string, markers ...string) int {
	first := -1
	for _, marker := range markers {
		index := strings.Index(script, marker)
		if index != -1 && (first == -1 || index < first) {
			first = index
		}
	}
	return first
}
