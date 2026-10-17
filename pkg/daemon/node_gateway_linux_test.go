//go:build linux

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

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

func TestCheckNodeGatewayBFDSkipsWithoutPeers(t *testing.T) {
	if err := checkNodeGatewayBFD(NodeGatewayRequest{}); err != nil {
		t.Fatalf("BFD check without peers returned an error: %v", err)
	}
}

func TestReadyNodeGatewayPeersRequiresReadyIPv4LRP(t *testing.T) {
	eips := []*kubeovnv1.OvnEip{
		{Status: kubeovnv1.OvnEipStatus{Ready: false}, Spec: kubeovnv1.OvnEipSpec{V4Ip: "192.0.2.10"}},
		{Status: kubeovnv1.OvnEipStatus{Ready: true}, Spec: kubeovnv1.OvnEipSpec{}},
		{Status: kubeovnv1.OvnEipStatus{Ready: true}, Spec: kubeovnv1.OvnEipSpec{V4Ip: "192.0.2.20"}},
	}
	peers := readyNodeGatewayPeers(eips)
	if len(peers) != 1 || peers[0] != "192.0.2.20" {
		t.Fatalf("ready node gateway peers = %v, want only ready IPv4 peer", peers)
	}
	if got := readyNodeGatewayPeers(eips[:2]); len(got) != 0 {
		t.Fatalf("unready LRP peers unexpectedly enabled node gateway: %v", got)
	}
}

func TestGetOrCreateNodeGatewayNSReusesMountedNamespace(t *testing.T) {
	gwNS, err := getOrCreateNodeGatewayNS("/proc/self/ns/net", func(string) error {
		t.Error("existing network namespace must not be bind-mounted again")
		return errors.New("unexpected create")
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gwNS.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestGetOrCreateNodeGatewayNSRecoversStaleFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ovnext")
	if err := os.WriteFile(path, nil, 0o660); err != nil {
		t.Fatal(err)
	}
	createErr := errors.New("namespace creation failed")
	created := false
	_, err := getOrCreateNodeGatewayNS(path, func(got string) error {
		created = true
		if got != path {
			t.Fatalf("namespace path = %q, want %q", got, path)
		}
		return createErr
	})
	if !created || !errors.Is(err, createErr) {
		t.Fatalf("stale-file recovery: created=%v, error=%v", created, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("failed recovery removed the reusable placeholder: %v", err)
	}
}

func TestGetOrCreateNodeGatewayNSRejectsCreateWithoutMountedNamespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ovnext")
	_, err := getOrCreateNodeGatewayNS(path, func(got string) error {
		return os.WriteFile(got, nil, 0o660)
	})
	if err == nil {
		t.Fatal("ordinary file returned as a network namespace")
	}
}

func TestHostMountNamespaceUnavailableDoesNotRunOperation(t *testing.T) {
	if _, err := os.Stat(nodeGatewayHostMount); err == nil {
		t.Skip("host mount namespace is present in the test environment")
	}
	called := false
	err := withHostMountNamespace(func() error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatalf("missing host mount namespace: error=%v, callback called=%v", err, called)
	}
}
