package speaker

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateRequiredFlags(t *testing.T) {
	tests := []struct {
		name        string
		config      *Configuration
		expectError bool
		errContains []string
	}{
		{
			name: "all required flags provided with IPv4 neighbor",
			config: &Configuration{
				NeighborAddresses: []net.IP{net.ParseIP("192.168.1.1")},
				ClusterAs:         65000,
				NeighborAs:        65001,
			},
			expectError: false,
		},
		{
			name: "all required flags provided with IPv6 neighbor",
			config: &Configuration{
				NeighborIPv6Addresses: []net.IP{net.ParseIP("2001:db8::1")},
				ClusterAs:             65000,
				NeighborAs:            65001,
			},
			expectError: false,
		},
		{
			name: "all required flags provided with both IPv4 and IPv6 neighbors",
			config: &Configuration{
				NeighborAddresses:     []net.IP{net.ParseIP("192.168.1.1")},
				NeighborIPv6Addresses: []net.IP{net.ParseIP("2001:db8::1")},
				ClusterAs:             65000,
				NeighborAs:            65001,
			},
			expectError: false,
		},
		{
			name: "missing neighbor addresses",
			config: &Configuration{
				ClusterAs:  65000,
				NeighborAs: 65001,
			},
			expectError: true,
			errContains: []string{"neighbor-address", "neighbor-ipv6-address"},
		},
		{
			name: "missing cluster-as",
			config: &Configuration{
				NeighborAddresses: []net.IP{net.ParseIP("192.168.1.1")},
				NeighborAs:        65001,
			},
			expectError: true,
			errContains: []string{"cluster-as"},
		},
		{
			name: "missing neighbor-as",
			config: &Configuration{
				NeighborAddresses: []net.IP{net.ParseIP("192.168.1.1")},
				ClusterAs:         65000,
			},
			expectError: true,
			errContains: []string{"neighbor-as"},
		},
		{
			name:        "missing all required flags",
			config:      &Configuration{},
			expectError: true,
			errContains: []string{"neighbor-address", "cluster-as", "neighbor-as"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validateRequiredFlags()
			if tt.expectError {
				require.Error(t, err)
				for _, s := range tt.errContains {
					require.Contains(t, err.Error(), s)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
