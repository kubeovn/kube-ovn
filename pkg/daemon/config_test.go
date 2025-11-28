package daemon

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
				ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
			},
			expected: map[string]string{},
		},
		{
			name: "node with empty annotation",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-node",
					Annotations: map[string]string{util.NodeNetworksAnnotation: ""},
				},
			},
			expected: map[string]string{},
		},
		{
			name: "node with valid single network",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-node",
					Annotations: map[string]string{util.NodeNetworksAnnotation: `{"storage": "192.168.1.3"}`},
				},
			},
			expected: map[string]string{"storage": "192.168.1.3"},
		},
		{
			name: "node with valid multiple networks",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-node",
					Annotations: map[string]string{util.NodeNetworksAnnotation: `{"storage": "192.168.1.3", "app": "172.10.0.10"}`},
				},
			},
			expected: map[string]string{"storage": "192.168.1.3", "app": "172.10.0.10"},
		},
		{
			name: "node with IPv6 address",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-node",
					Annotations: map[string]string{util.NodeNetworksAnnotation: `{"storage": "fd00::1"}`},
				},
			},
			expected: map[string]string{"storage": "fd00::1"},
		},
		{
			name: "invalid JSON format",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-node",
					Annotations: map[string]string{util.NodeNetworksAnnotation: `invalid json`},
				},
			},
			expectError: true,
		},
		{
			name: "invalid IP address",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-node",
					Annotations: map[string]string{util.NodeNetworksAnnotation: `{"storage": "invalid-ip"}`},
				},
			},
			expectError: true,
		},
		{
			name: "IP with CIDR notation (invalid)",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-node",
					Annotations: map[string]string{util.NodeNetworksAnnotation: `{"storage": "192.168.1.3/24"}`},
				},
			},
			expectError: true,
		},
		{
			name: "empty IP value",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-node",
					Annotations: map[string]string{util.NodeNetworksAnnotation: `{"storage": ""}`},
				},
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
