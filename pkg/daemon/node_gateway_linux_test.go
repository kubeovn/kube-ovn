//go:build linux

package daemon

import "testing"

func TestNodeGatewayRequestValidate(t *testing.T) {
	valid := NodeGatewayRequest{
		Operation:  nodeGatewayEnsure,
		NodeName:   "node-a",
		IP:         "192.0.2.10/24,2001:db8::10/64",
		Gateway:    "192.0.2.1,2001:db8::1",
		MAC:        "02:42:ac:11:00:02",
		MTU:        1500,
		BFDLocalIP: "192.0.2.10",
		BFDPeers:   []string{"192.0.2.20"},
	}
	if err := valid.Validate("node-a"); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	tests := []struct {
		name string
		edit func(*NodeGatewayRequest)
	}{
		{"wrong node", func(r *NodeGatewayRequest) { r.NodeName = "node-b" }},
		{"wrong operation", func(r *NodeGatewayRequest) { r.Operation = "run" }},
		{"invalid mac", func(r *NodeGatewayRequest) { r.MAC = "00:11" }},
		{"family mismatch", func(r *NodeGatewayRequest) { r.Gateway = "192.0.2.1,192.0.2.2" }},
		{"invalid bfd peer", func(r *NodeGatewayRequest) { r.BFDPeers = []string{"not-an-ip"} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := valid
			tt.edit(&request)
			if err := request.Validate("node-a"); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
	if err := (NodeGatewayRequest{Operation: nodeGatewayDelete, NodeName: "node-a"}).Validate("node-a"); err != nil {
		t.Fatalf("delete request rejected: %v", err)
	}
}

func TestContainsNoSession(t *testing.T) {
	if !containsNoSession("remote status: No session\n") {
		t.Fatal("embedded no-session status was not detected")
	}
	if containsNoSession("Session Up\n") {
		t.Fatal("active session was incorrectly detected as absent")
	}
}
