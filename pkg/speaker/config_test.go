package speaker

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
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
				NodeName:          "node1",
			},
			expectError: false,
		},
		{
			name: "all required flags provided with IPv6 neighbor",
			config: &Configuration{
				NeighborIPv6Addresses: []net.IP{net.ParseIP("2001:db8::1")},
				ClusterAs:             65000,
				NeighborAs:            65001,
				NodeName:              "node1",
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
				NodeName:              "node1",
			},
			expectError: false,
		},
		{
			name: "missing neighbor addresses",
			config: &Configuration{
				ClusterAs:  65000,
				NeighborAs: 65001,
				NodeName:   "node1",
			},
			expectError: true,
			errContains: []string{"neighbor-address", "neighbor-ipv6-address"},
		},
		{
			name: "missing cluster-as",
			config: &Configuration{
				NeighborAddresses: []net.IP{net.ParseIP("192.168.1.1")},
				NeighborAs:        65001,
				NodeName:          "node1",
			},
			expectError: true,
			errContains: []string{"cluster-as"},
		},
		{
			name: "missing neighbor-as",
			config: &Configuration{
				NeighborAddresses: []net.IP{net.ParseIP("192.168.1.1")},
				ClusterAs:         65000,
				NodeName:          "node1",
			},
			expectError: true,
			errContains: []string{"neighbor-as"},
		},
		{
			name: "missing node-name in normal mode is allowed",
			config: &Configuration{
				NeighborAddresses: []net.IP{net.ParseIP("192.168.1.1")},
				ClusterAs:         65000,
				NeighborAs:        65001,
			},
			expectError: false,
		},
		{
			name: "enable lb svc announce requires node-name",
			config: &Configuration{
				NeighborAddresses:   []net.IP{net.ParseIP("192.168.1.1")},
				ClusterAs:           65000,
				NeighborAs:          65001,
				EnableLbSvcAnnounce: true,
			},
			expectError: true,
			errContains: []string{"enable-lb-svc-announce", "node-name"},
		},
		{
			name: "nat-gw mode does not require node-name",
			config: &Configuration{
				NeighborAddresses: []net.IP{net.ParseIP("192.168.1.1")},
				ClusterAs:         65000,
				NeighborAs:        65001,
				NatGwMode:         true,
			},
			expectError: false,
		},
		{
			name: "node-route-eip mode requires node-name",
			config: &Configuration{
				NeighborAddresses: []net.IP{net.ParseIP("192.168.1.1")},
				ClusterAs:         65000,
				NeighborAs:        65001,
				NodeRouteEIPMode:  true,
			},
			expectError: true,
			errContains: []string{"node-name"},
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

func TestValidateLocalAddressFamily(t *testing.T) {
	tests := []struct {
		name                string
		neighborAddress     net.IP
		localAddress        net.IP
		expectedErrContains string
	}{
		{
			name:            "allow ipv4 local address for ipv4 neighbor",
			neighborAddress: net.ParseIP("10.32.32.1"),
			localAddress:    net.ParseIP("10.32.32.2"),
		},
		{
			name:            "allow ipv6 local address for ipv6 neighbor",
			neighborAddress: net.ParseIP("fd00::254"),
			localAddress:    net.ParseIP("fd00::10"),
		},
		{
			name:                "reject ipv6 local address for ipv4 neighbor",
			neighborAddress:     net.ParseIP("10.32.32.1"),
			localAddress:        net.ParseIP("fd00::10"),
			expectedErrContains: "invalid local address",
		},
		{
			name:                "reject ipv4 local address for ipv6 neighbor",
			neighborAddress:     net.ParseIP("fd00::254"),
			localAddress:        net.ParseIP("10.32.32.2"),
			expectedErrContains: "invalid local address",
		},
		{
			name:                "reject nil neighbor address",
			localAddress:        net.ParseIP("10.32.32.2"),
			expectedErrContains: "invalid nil BGP neighbor address",
		},
		{
			name:                "reject nil local address",
			neighborAddress:     net.ParseIP("10.32.32.1"),
			expectedErrContains: "invalid nil local address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLocalAddressFamily(tt.neighborAddress, tt.localAddress)
			if tt.expectedErrContains != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedErrContains)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestValidateAllowedLocalAddress(t *testing.T) {
	tests := []struct {
		name                string
		neighborAddress     net.IP
		localAddress        net.IP
		allowedLocalAddrs   []net.IP
		expectedErrContains string
	}{
		{
			name:              "allow when whitelist is empty",
			neighborAddress:   net.ParseIP("10.32.32.1"),
			localAddress:      net.ParseIP("10.32.32.2"),
			allowedLocalAddrs: nil,
		},
		{
			name:              "allow selected source address inside whitelist",
			neighborAddress:   net.ParseIP("10.32.32.1"),
			localAddress:      net.ParseIP("10.32.32.2"),
			allowedLocalAddrs: []net.IP{net.ParseIP("10.32.32.2"), net.ParseIP("10.32.32.3")},
		},
		{
			name:                "reject selected source address outside whitelist",
			neighborAddress:     net.ParseIP("10.32.32.1"),
			localAddress:        net.ParseIP("10.32.32.2"),
			allowedLocalAddrs:   []net.IP{net.ParseIP("10.32.32.3")},
			expectedErrContains: "not in allowed source address list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAllowedLocalAddress(tt.neighborAddress, tt.localAddress, tt.allowedLocalAddrs)
			if tt.expectedErrContains != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedErrContains)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestSelectNeighborLocalAddressFromRoutes(t *testing.T) {
	tests := []struct {
		name                string
		neighborAddress     net.IP
		routes              []netlink.Route
		allowedLocalAddrs   []net.IP
		expectedLocalAddr   net.IP
		expectedErrContains string
	}{
		{
			name:            "skip non-whitelisted source and use first whitelisted match",
			neighborAddress: net.ParseIP("10.32.32.1"),
			routes: []netlink.Route{
				{Src: net.ParseIP("10.32.32.2")},
				{Src: net.ParseIP("10.32.32.3")},
			},
			allowedLocalAddrs: []net.IP{net.ParseIP("10.32.32.3")},
			expectedLocalAddr: net.ParseIP("10.32.32.3"),
		},
		{
			name:            "reject when no source address matches whitelist",
			neighborAddress: net.ParseIP("10.32.32.1"),
			routes: []netlink.Route{
				{Src: net.ParseIP("10.32.32.2")},
				{Src: net.ParseIP("10.32.32.4")},
			},
			allowedLocalAddrs:   []net.IP{net.ParseIP("10.32.32.3")},
			expectedErrContains: "not in allowed source address list",
		},
		{
			name:            "skip non-whitelisted ipv6 source and use first whitelisted match",
			neighborAddress: net.ParseIP("fd00::1"),
			routes: []netlink.Route{
				{Src: net.ParseIP("fd00::2")},
				{Src: net.ParseIP("fd00::3")},
			},
			allowedLocalAddrs: []net.IP{net.ParseIP("fd00::3")},
			expectedLocalAddr: net.ParseIP("fd00::3"),
		},
		{
			name:            "reject when all source addresses have wrong family",
			neighborAddress: net.ParseIP("10.32.32.1"),
			routes: []netlink.Route{
				{Src: net.ParseIP("fd00::2")},
				{Src: net.ParseIP("fd00::3")},
			},
			allowedLocalAddrs:   []net.IP{net.ParseIP("10.32.32.3")},
			expectedErrContains: "no route source matched the required address family for whitelist evaluation",
		},
		{
			name:            "reject when route lookup returns no source addresses",
			neighborAddress: net.ParseIP("10.32.32.1"),
			routes: []netlink.Route{
				{},
				{},
			},
			allowedLocalAddrs:   []net.IP{net.ParseIP("10.32.32.3")},
			expectedErrContains: "route lookup returned no source address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			localAddr, err := selectNeighborLocalAddressFromRoutes(tt.neighborAddress, tt.routes, tt.allowedLocalAddrs)
			if tt.expectedErrContains != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedErrContains)
				require.Nil(t, localAddr)
				return
			}

			require.NoError(t, err)
			require.True(t, tt.expectedLocalAddr.Equal(localAddr))
		})
	}
}

func TestInitNeighborLocalAddressesPureExtensionMode(t *testing.T) {
	config := &Configuration{
		NeighborAddresses:     []net.IP{net.ParseIP("10.32.32.1")},
		NeighborIPv6Addresses: []net.IP{net.ParseIP("fd00::254")},
	}

	require.NoError(t, config.initNeighborLocalAddresses())
	require.Nil(t, config.getNeighborLocalAddress(net.ParseIP("10.32.32.1")))
	require.Nil(t, config.getNeighborLocalAddress(net.ParseIP("fd00::254")))
}
