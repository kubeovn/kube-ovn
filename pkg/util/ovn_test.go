package util

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation"
)

func TestProviderNetworkAddressName(t *testing.T) {
	for _, tt := range []struct {
		provider string
		node     string
		want     string
	}{
		{provider: "provider", node: "node", want: "pn.provider.node"},
		{provider: "a-b", node: "c", want: "pn.a-b.c"},
		{provider: "a", node: "b-c", want: "pn.a.b-c"},
		{provider: "underlay", node: "worker.example.com", want: "pn.underlay.worker.example.com"},
	} {
		if name := ProviderNetworkAddressName(tt.provider, tt.node); name != tt.want {
			t.Errorf("unexpected provider network address name %q, want %q", name, tt.want)
		}
	}
}

func TestProviderNetworkAddressNameLongNames(t *testing.T) {
	provider := strings.Repeat("p", 63)
	// Names that fit the API limit remain intact and do not get a hash.
	node := strings.Repeat("n", 186)
	fullName := "pn." + provider + "." + node
	if name := ProviderNetworkAddressName(provider, node); name != fullName || len(name) != 253 {
		t.Fatalf("unexpected name at the length limit: %q", name)
	}

	node = strings.Repeat("n", 253)
	name := ProviderNetworkAddressName(provider, node)
	if len(name) > 253 {
		t.Fatalf("provider network address name is too long: %d", len(name))
	}
	if errs := validation.IsDNS1123Subdomain(name); len(errs) != 0 {
		t.Fatalf("invalid provider network address name %q: %v", name, errs)
	}
	if !strings.HasPrefix(name, "pn."+provider+".") {
		t.Fatalf("name lost its readable prefix: %q", name)
	}
	if name != ProviderNetworkAddressName(provider, node) {
		t.Fatal("name generation is not deterministic")
	}
	if name == ProviderNetworkAddressName(provider, node[:len(node)-1]+"x") {
		t.Fatal("different full names produced the same shortened name")
	}
}

func TestNodeLspName(t *testing.T) {
	tests := []struct {
		name     string
		nodeName string
		expected string
	}{
		{
			name:     "base case",
			nodeName: "node1",
			expected: "node-node1",
		},
		{
			name:     "empty node name",
			nodeName: "",
			expected: "node-",
		},
		{
			name:     "long node name",
			nodeName: "this-is-a-very-long-node-name",
			expected: "node-this-is-a-very-long-node-name",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := NodeLspName(test.nodeName)
			if result != test.expected {
				t.Errorf("got %s, but expected %s", result, test.expected)
			}
		})
	}
}
