package daemon

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestParseNodeNetworks(t *testing.T) {
	tests := []struct {
		name        string
		node        *corev1.Node
		expected    map[string]string
		expectError bool
	}{
		{
			name:     "nil node",
			node:     nil,
			expected: map[string]string{},
		},
		{
			name: "node without annotations",
			node: &corev1.Node{
				Name: "test-node",
			},
			expected: map[string]string{},
		},
		{
			name: "node with empty annotation",
			node: &corev1.Node{
				Name:        "test-node",
				Annotations: map[string]string{util.NodeNetworksAnnotation: ""},
			},
			expected: map[string]string{},
		},
		{
			name: "node with valid single network",
			node: &corev1.Node{
				Name:        "test-node",
				Annotations: map[string]string{util.NodeNetworksAnnotation: `{"storage": "192.168.1.3"}`},
			},
			expected: map[string]string{"storage": "192.168.1.3"},
		},
		{
			name: "node with valid multiple networks",
			node: &corev1.Node{
				Name:        "test-node",
				Annotations: map[string]string{util.NodeNetworksAnnotation: `{"storage": "192.168.1.3", "app": "172.10.0.10"}`},
			},
			expected: map[string]string{"storage": "192.168.1.3", "app": "172.10.0.10"},
		},
		{
			name: "node with IPv6 address",
			node: &corev1.Node{
				Name:        "test-node",
				Annotations: map[string]string{util.NodeNetworksAnnotation: `{"storage": "fd00::1"}`},
			},
			expected: map[string]string{"storage": "fd00::1"},
		},
		{
			name: "invalid JSON format",
			node: &corev1.Node{
				Name:        "test-node",
				Annotations: map[string]string{util.NodeNetworksAnnotation: `invalid json`},
			},
			expectError: true,
		},
		{
			name: "invalid IP address",
			node: &corev1.Node{
				Name:        "test-node",
				Annotations: map[string]string{util.NodeNetworksAnnotation: `{"storage": "invalid-ip"}`},
			},
			expectError: true,
		},
		{
			name: "IP with CIDR notation (invalid)",
			node: &corev1.Node{
				Name:        "test-node",
				Annotations: map[string]string{util.NodeNetworksAnnotation: `{"storage": "192.168.1.3/24"}`},
			},
			expectError: true,
		},
		{
			name: "empty IP value",
			node: &corev1.Node{
				Name:        "test-node",
				Annotations: map[string]string{util.NodeNetworksAnnotation: `{"storage": ""}`},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseNodeNetworks(tt.node)
			if tt.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestGetEncapIPByNetwork(t *testing.T) {
	config := &Configuration{
		DefaultEncapIP: "10.0.0.1",
		NodeNetworks: map[string]string{
			"storage": "192.168.1.3",
			"app":     "172.10.0.10",
		},
	}

	tests := []struct {
		name        string
		networkName string
		expected    string
		expectError bool
	}{
		{
			name:        "empty network name returns default",
			networkName: "",
			expected:    "10.0.0.1",
		},
		{
			name:        "existing network storage",
			networkName: "storage",
			expected:    "192.168.1.3",
		},
		{
			name:        "existing network app",
			networkName: "app",
			expected:    "172.10.0.10",
		},
		{
			name:        "non-existent network",
			networkName: "unknown",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := config.GetEncapIPByNetwork(tt.networkName)
			if tt.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestGetEncapIPByNetworkEmptyNodeNetworks(t *testing.T) {
	config := &Configuration{
		DefaultEncapIP: "10.0.0.1",
		NodeNetworks:   nil,
	}

	ip, err := config.GetEncapIPByNetwork("")
	require.NoError(t, err)
	require.Equal(t, "10.0.0.1", ip)

	_, err = config.GetEncapIPByNetwork("storage")
	require.Error(t, err)
}

func TestSelectEncapIP(t *testing.T) {
	addrs := func(cidrs ...string) []net.Addr {
		ret := make([]net.Addr, 0, len(cidrs))
		for _, cidr := range cidrs {
			ip, ipNet, err := net.ParseCIDR(cidr)
			require.NoError(t, err)
			ret = append(ret, &net.IPNet{IP: ip, Mask: ipNet.Mask})
		}
		return ret
	}

	tests := []struct {
		name          string
		addrs         []net.Addr
		srcIPs        []string
		hostTunnelSrc bool
		nodeIPs       []string
		expected      string
	}{{
		name:     "no address",
		addrs:    addrs(),
		expected: "",
	}, {
		name:     "single /32 assigned by cloud dhcp",
		addrs:    addrs("10.198.0.140/32", "fe80::2c28:3aff:fe25:bf57/64"),
		expected: "10.198.0.140",
	}, {
		name:     "single /128",
		addrs:    addrs("fd00::1/128", "fe80::1/64"),
		expected: "fd00::1",
	}, {
		name:     "dual stack full mask addresses",
		addrs:    addrs("10.198.0.140/32", "fd00::1/128"),
		expected: "10.198.0.140",
	}, {
		name:     "vip is skipped in favor of the node address",
		addrs:    addrs("192.168.0.11/32", "192.168.0.10/24"),
		expected: "192.168.0.10",
	}, {
		name:          "vip is used when host tunnel src is enabled",
		addrs:         addrs("192.168.0.11/32", "192.168.0.10/24"),
		hostTunnelSrc: true,
		expected:      "192.168.0.11",
	}, {
		name:     "all full mask addresses of the family are skipped but one remains per family",
		addrs:    addrs("192.168.0.11/32", "192.168.0.12/32", "fd00::1/128"),
		expected: "fd00::1",
	}, {
		name:     "node ip is used even when another full mask address exists",
		addrs:    addrs("192.168.0.11/32", "192.168.0.10/32"),
		nodeIPs:  []string{"192.168.0.10"},
		expected: "192.168.0.10",
	}, {
		name:     "no candidate is left when every address of the single stack nic is a vip",
		addrs:    addrs("192.168.0.11/32", "192.168.0.12/32"),
		expected: "",
	}, {
		name:     "address must be a route source when any exists",
		addrs:    addrs("192.168.0.10/24", "192.168.1.10/24"),
		srcIPs:   []string{"192.168.1.10"},
		expected: "192.168.1.10",
	}, {
		name:     "no address matches the route sources",
		addrs:    addrs("192.168.0.10/24"),
		srcIPs:   []string{"192.168.1.10"},
		expected: "",
	}, {
		name:     "full mask address is not a vip when the other address is not a route source",
		addrs:    addrs("10.0.0.10/32", "10.0.0.11/24"),
		srcIPs:   []string{"10.0.0.10"},
		expected: "10.0.0.10",
	}, {
		name:     "loopback and link local addresses are ignored",
		addrs:    addrs("127.0.0.1/8", "169.254.1.1/16", "10.198.0.140/32"),
		expected: "10.198.0.140",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, selectEncapIP(tt.addrs, tt.srcIPs, tt.hostTunnelSrc, tt.nodeIPs...))
		})
	}
}
