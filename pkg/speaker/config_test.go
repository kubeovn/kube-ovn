package speaker

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"gopkg.in/yaml.v3"
)

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool { p := new(bool); *p = b; return p }

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
				NeighborAddresses: []IP{{IP: net.ParseIP("192.168.1.1")}},
				ClusterAs:         65000,
				NeighborAs:        65001,
				NodeName:          "node1",
			},
			expectError: false,
		},
		{
			name: "all required flags provided with IPv6 neighbor",
			config: &Configuration{
				NeighborIPv6Addresses: []IP{{IP: net.ParseIP("2001:db8::1")}},
				ClusterAs:             65000,
				NeighborAs:            65001,
				NodeName:              "node1",
			},
			expectError: false,
		},
		{
			name: "all required flags provided with both IPv4 and IPv6 neighbors",
			config: &Configuration{
				NeighborAddresses:     []IP{{IP: net.ParseIP("192.168.1.1")}},
				NeighborIPv6Addresses: []IP{{IP: net.ParseIP("2001:db8::1")}},
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
				NeighborAddresses: []IP{{IP: net.ParseIP("192.168.1.1")}},
				NeighborAs:        65001,
				NodeName:          "node1",
			},
			expectError: true,
			errContains: []string{"cluster-as"},
		},
		{
			name: "missing neighbor-as",
			config: &Configuration{
				NeighborAddresses: []IP{{IP: net.ParseIP("192.168.1.1")}},
				ClusterAs:         65000,
				NodeName:          "node1",
			},
			expectError: true,
			errContains: []string{"neighbor-as"},
		},
		{
			name: "missing node-name",
			config: &Configuration{
				NeighborAddresses: []IP{{IP: net.ParseIP("192.168.1.1")}},
				ClusterAs:         65000,
				NeighborAs:        65001,
			},
			expectError: true,
			errContains: []string{"node-name"},
		},
		{
			name: "nat-gw mode does not require node-name",
			config: &Configuration{
				NeighborAddresses: []IP{{IP: net.ParseIP("192.168.1.1")}},
				ClusterAs:         65000,
				NeighborAs:        65001,
				NatGwMode:         boolPtr(true),
			},
			expectError: false,
		},
		{
			name:        "missing all required flags",
			config:      &Configuration{},
			expectError: true,
			errContains: []string{"neighbor-address", "cluster-as", "neighbor-as", "node-name"},
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
		NeighborAddresses:     []IP{{IP: net.ParseIP("10.32.32.1")}},
		NeighborIPv6Addresses: []IP{{IP: net.ParseIP("fd00::254")}},
	}

	require.NoError(t, config.initNeighborLocalAddresses())
	require.Nil(t, config.getNeighborLocalAddress(net.ParseIP("10.32.32.1")))
	require.Nil(t, config.getNeighborLocalAddress(net.ParseIP("fd00::254")))
}

func TestIP_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name        string
		yamlValue   string
		expectError bool
		expectIP    net.IP
		errorMsg    string
	}{
		{
			name:        "valid IPv4 address",
			yamlValue:   "192.168.1.1",
			expectIP:    net.ParseIP("192.168.1.1"),
			expectError: false,
		},
		{
			name:        "valid IPv6 address",
			yamlValue:   "2001:db8::1",
			expectIP:    net.ParseIP("2001:db8::1"),
			expectError: false,
		},
		{
			name:        "IPv6 loopback",
			yamlValue:   "::1",
			expectIP:    net.ParseIP("::1"),
			expectError: false,
		},
		{
			name:        "IPv4 zero",
			yamlValue:   "0.0.0.0",
			expectIP:    net.ParseIP("0.0.0.0"),
			expectError: false,
		},
		{
			name:        "invalid IP string",
			yamlValue:   "invalid-ip",
			expectError: true,
			errorMsg:    "invalid IP address",
		},
		{
			name:        "empty string",
			yamlValue:   "",
			expectError: false,
		},
		{
			name:        "non-string value",
			yamlValue:   "123",
			expectError: true,
			errorMsg:    "invalid IP value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ip IP
			err := yaml.Unmarshal([]byte(tt.yamlValue), &ip)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					require.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
				require.True(t, tt.expectIP.Equal(ip.IP), "expected %v, got %v", tt.expectIP, ip.IP)
			}
		})
	}
}

func TestIP_MarshalYAML(t *testing.T) {
	tests := []struct {
		name     string
		ip       IP
		expected string
	}{
		{
			name:     "IPv4 address",
			ip:       IP{IP: net.ParseIP("192.168.1.1")},
			expected: "192.168.1.1\n",
		},
		{
			name:     "IPv6 address",
			ip:       IP{IP: net.ParseIP("2001:db8::1")},
			expected: "2001:db8::1\n",
		},
		{
			name:     "IPv6 loopback",
			ip:       IP{IP: net.ParseIP("::1")},
			expected: "::1\n",
		},
		{
			name:     "nil IP",
			ip:       IP{IP: nil},
			expected: "null\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := yaml.Marshal(tt.ip)
			require.NoError(t, err)
			require.Equal(t, tt.expected, string(data))
		})
	}
}

func TestIP_String(t *testing.T) {
	tests := []struct {
		name     string
		ip       IP
		expected string
	}{
		{
			name:     "IPv4 address",
			ip:       IP{IP: net.ParseIP("192.168.1.1")},
			expected: "192.168.1.1",
		},
		{
			name:     "IPv6 address",
			ip:       IP{IP: net.ParseIP("2001:db8::1")},
			expected: "2001:db8::1",
		},
		{
			name:     "nil IP",
			ip:       IP{IP: nil},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.ip.String())
		})
	}
}

// Tests for Duration type YAML marshaling/unmarshaling
func TestConfiguration_LoadFileConfigWithIP(t *testing.T) {
	tests := []struct {
		name           string
		yamlContent    string
		expectError    bool
		expectedConfig *Configuration
	}{
		{
			name: "load config with IPv4 neighbor addresses",
			yamlContent: `
neighbor-address:
  - 192.168.1.1
  - 192.168.1.2
grpc-host: 10.0.0.1
router-id: 10.0.0.254
`,
			expectError: false,
			expectedConfig: &Configuration{
				NeighborAddresses: []IP{
					{IP: net.ParseIP("192.168.1.1")},
					{IP: net.ParseIP("192.168.1.2")},
				},
				GrpcHost: IP{IP: net.ParseIP("10.0.0.1")},
				RouterID: IP{IP: net.ParseIP("10.0.0.254")},
			},
		},
		{
			name: "load config with IPv6 neighbor addresses",
			yamlContent: `
neighbor-ipv6-address:
  - 2001:db8::1
  - 2001:db8::2
grpc-host: fd00::1
router-id: fd00::254
`,
			expectError: false,
			expectedConfig: &Configuration{
				NeighborIPv6Addresses: []IP{
					{IP: net.ParseIP("2001:db8::1")},
					{IP: net.ParseIP("2001:db8::2")},
				},
				GrpcHost: IP{IP: net.ParseIP("fd00::1")},
				RouterID: IP{IP: net.ParseIP("fd00::254")},
			},
		},
		{
			name: "load config with mixed IPv4 and IPv6",
			yamlContent: `
neighbor-address:
  - 192.168.1.1
neighbor-ipv6-address:
  - 2001:db8::1
allowed-source-addresses:
  - 10.0.0.1
  - 10.0.0.2
allowed-source-ipv6-addresses:
  - fd00::1
grpc-host: 192.168.1.100
router-id: 192.168.1.254
`,
			expectError: false,
			expectedConfig: &Configuration{
				NeighborAddresses:     []IP{{IP: net.ParseIP("192.168.1.1")}},
				NeighborIPv6Addresses: []IP{{IP: net.ParseIP("2001:db8::1")}},
				AllowedSourceAddresses: []IP{
					{IP: net.ParseIP("10.0.0.1")},
					{IP: net.ParseIP("10.0.0.2")},
				},
				AllowedSourceIPv6Addresses: []IP{{IP: net.ParseIP("fd00::1")}},
				GrpcHost:                   IP{IP: net.ParseIP("192.168.1.100")},
				RouterID:                   IP{IP: net.ParseIP("192.168.1.254")},
			},
		},
		{
			name: "load config with invalid IPv4 address",
			yamlContent: `
neighbor-address:
  - 192.168.1.256
`,
			expectError: true,
		},
		{
			name: "load config with invalid IP format",
			yamlContent: `
neighbor-address:
  - not-an-ip
`,
			expectError: true,
		},
		{
			name: "load config with node-ips",
			yamlContent: `
node-ips:
   IPv4: 10.0.0.1
   IPv6: fd00::1
`,
			expectError: false,
			expectedConfig: &Configuration{
				NodeIPs: map[string]IP{
					"IPv4": {IP: net.ParseIP("10.0.0.1")},
					"IPv6": {IP: net.ParseIP("fd00::1")},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Configuration
			err := yaml.Unmarshal([]byte(tt.yamlContent), &cfg)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.expectedConfig.GrpcHost.IP != nil {
					require.True(t, tt.expectedConfig.GrpcHost.Equal(cfg.GrpcHost.IP), "GrpcHost mismatch")
				}
				if tt.expectedConfig.RouterID.IP != nil {
					require.True(t, tt.expectedConfig.RouterID.Equal(cfg.RouterID.IP), "RouterID mismatch")
				}
				require.Len(t, cfg.NeighborAddresses, len(tt.expectedConfig.NeighborAddresses), "NeighborAddresses length mismatch")
				for i, addr := range cfg.NeighborAddresses {
					require.True(t, addr.Equal(tt.expectedConfig.NeighborAddresses[i].IP), "NeighborAddresses[%d] mismatch", i)
				}
				require.Len(t, cfg.NeighborIPv6Addresses, len(tt.expectedConfig.NeighborIPv6Addresses), "NeighborIPv6Addresses length mismatch")
				for i, addr := range cfg.NeighborIPv6Addresses {
					require.True(t, addr.Equal(tt.expectedConfig.NeighborIPv6Addresses[i].IP), "NeighborIPv6Addresses[%d] mismatch", i)
				}
				require.Len(t, cfg.AllowedSourceAddresses, len(tt.expectedConfig.AllowedSourceAddresses), "AllowedSourceAddresses length mismatch")
				for i, addr := range cfg.AllowedSourceAddresses {
					require.True(t, addr.Equal(tt.expectedConfig.AllowedSourceAddresses[i].IP), "AllowedSourceAddresses[%d] mismatch", i)
				}
				require.Len(t, cfg.AllowedSourceIPv6Addresses, len(tt.expectedConfig.AllowedSourceIPv6Addresses), "AllowedSourceIPv6Addresses length mismatch")
				for i, addr := range cfg.AllowedSourceIPv6Addresses {
					require.True(t, addr.Equal(tt.expectedConfig.AllowedSourceIPv6Addresses[i].IP), "AllowedSourceIPv6Addresses[%d] mismatch", i)
				}
				if len(tt.expectedConfig.NodeIPs) != 0 {
					require.Len(t, cfg.NodeIPs, len(tt.expectedConfig.NodeIPs), "NodeIPs length mismatch")
					for k, v := range tt.expectedConfig.NodeIPs {
						cfgVal, ok := cfg.NodeIPs[k]
						require.True(t, ok, "NodeIPs key %s not found", k)
						require.True(t, v.Equal(cfgVal.IP), "NodeIPs[%s] mismatch", k)
					}
				}
			}
		})
	}
}

func TestConfiguration_LoadFileConfigWithBooleans(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
		expectError bool
		checkFunc   func(*testing.T, *Configuration)
	}{
		{
			name: "YAML with announce-cluster-ip true",
			yamlContent: `
announce-cluster-ip: true
`,
			expectError: false,
			checkFunc: func(t *testing.T, cfg *Configuration) {
				require.NotNil(t, cfg.AnnounceClusterIP)
				require.True(t, *cfg.AnnounceClusterIP)
			},
		},
		{
			name: "YAML with announce-cluster-ip false",
			yamlContent: `
announce-cluster-ip: false
`,
			expectError: false,
			checkFunc: func(t *testing.T, cfg *Configuration) {
				require.NotNil(t, cfg.AnnounceClusterIP)
				require.False(t, *cfg.AnnounceClusterIP)
			},
		},
		{
			name: "YAML omits boolean field",
			yamlContent: `
grpc-host: 10.0.0.1
`,
			expectError: false,
			checkFunc: func(t *testing.T, cfg *Configuration) {
				// Omitted boolean should be nil, not false
				require.Nil(t, cfg.AnnounceClusterIP)
				require.Nil(t, cfg.GracefulRestart)
				require.Nil(t, cfg.PassiveMode)
			},
		},
		{
			name: "YAML with multiple boolean fields",
			yamlContent: `
graceful-restart: true
passivemode: false
enable-metrics: true
enable-bfd: true
`,
			expectError: false,
			checkFunc: func(t *testing.T, cfg *Configuration) {
				require.NotNil(t, cfg.GracefulRestart)
				require.True(t, *cfg.GracefulRestart)
				require.NotNil(t, cfg.PassiveMode)
				require.False(t, *cfg.PassiveMode)
				require.NotNil(t, cfg.EnableMetrics)
				require.True(t, *cfg.EnableMetrics)
				require.NotNil(t, cfg.EnableBFD)
				require.True(t, *cfg.EnableBFD)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Configuration
			err := yaml.Unmarshal([]byte(tt.yamlContent), &cfg)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				tt.checkFunc(t, &cfg)
			}
		})
	}
}

func TestDuration_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name           string
		yamlValue      string
		expectError    bool
		expectDuration time.Duration
		errorContains  string
	}{
		{
			name:           "string format seconds",
			yamlValue:      "90s",
			expectDuration: 90 * time.Second,
			expectError:    false,
		},
		{
			name:           "string format minutes",
			yamlValue:      "6m",
			expectDuration: 6 * time.Minute,
			expectError:    false,
		},
		{
			name:           "string format hours",
			yamlValue:      "1h",
			expectDuration: 1 * time.Hour,
			expectError:    false,
		},
		{
			name:           "string format complex",
			yamlValue:      "1h30m",
			expectDuration: 1*time.Hour + 30*time.Minute,
			expectError:    false,
		},
		{
			name:           "integer seconds",
			yamlValue:      "360",
			expectDuration: 360 * time.Second,
			expectError:    false,
		},
		{
			name:           "integer zero",
			yamlValue:      "0",
			expectDuration: 0,
			expectError:    false,
		},
		{
			name:          "invalid string format",
			yamlValue:     "invalid",
			expectError:   true,
			errorContains: "invalid duration",
		},
		{
			name:          "string without unit",
			yamlValue:     "\"360\"",
			expectError:   true,
			errorContains: "missing unit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration

			// Parse the YAML value
			err := yaml.Unmarshal([]byte(tt.yamlValue), &d)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					require.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectDuration, d.Duration)
			}
		})
	}
}

func TestDuration_MarshalYAML(t *testing.T) {
	tests := []struct {
		name     string
		duration Duration
		expected string
	}{
		{
			name:     "seconds",
			duration: Duration{Duration: 90 * time.Second},
			expected: "1m30s",
		},
		{
			name:     "minutes",
			duration: Duration{Duration: 6 * time.Minute},
			expected: "6m0s",
		},
		{
			name:     "hours",
			duration: Duration{Duration: 1 * time.Hour},
			expected: "1h0m0s",
		},
		{
			name:     "zero duration",
			duration: Duration{Duration: 0},
			expected: "0s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := yaml.Marshal(tt.duration)
			require.NoError(t, err)
			require.Equal(t, tt.expected+"\n", string(data))
		})
	}
}

func TestDuration_Seconds(t *testing.T) {
	tests := []struct {
		name     string
		duration Duration
		expected uint32
	}{
		{
			name:     "90 seconds",
			duration: Duration{Duration: 90 * time.Second},
			expected: 90,
		},
		{
			name:     "6 minutes",
			duration: Duration{Duration: 6 * time.Minute},
			expected: 360,
		},
		{
			name:     "1 hour",
			duration: Duration{Duration: 1 * time.Hour},
			expected: 3600,
		},
		{
			name:     "zero duration",
			duration: Duration{Duration: 0},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.duration.Seconds())
		})
	}
}

func TestDuration_IsZero(t *testing.T) {
	tests := []struct {
		name     string
		duration Duration
		expected bool
	}{
		{
			name:     "zero duration",
			duration: Duration{Duration: 0},
			expected: true,
		},
		{
			name:     "non-zero duration",
			duration: Duration{Duration: 90 * time.Second},
			expected: false,
		},
		{
			name:     "negative duration",
			duration: Duration{Duration: -90 * time.Second},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.duration.IsZero())
		})
	}
}

func TestToNetIPs(t *testing.T) {
	tests := []struct {
		name     string
		input    []IP
		expected []net.IP
	}{
		{
			name:     "empty slice",
			input:    []IP{},
			expected: []net.IP{},
		},
		{
			name:     "single IPv4",
			input:    []IP{{IP: net.ParseIP("192.168.1.1")}},
			expected: []net.IP{net.ParseIP("192.168.1.1")},
		},
		{
			name: "multiple IPv4 and IPv6",
			input: []IP{
				{IP: net.ParseIP("192.168.1.1")},
				{IP: net.ParseIP("2001:db8::1")},
				{IP: net.ParseIP("10.0.0.1")},
			},
			expected: []net.IP{
				net.ParseIP("192.168.1.1"),
				net.ParseIP("2001:db8::1"),
				net.ParseIP("10.0.0.1"),
			},
		},
		{
			name: "skip nil IPs",
			input: []IP{
				{IP: net.ParseIP("192.168.1.1")},
				{IP: nil},
				{IP: net.ParseIP("10.0.0.1")},
			},
			expected: []net.IP{
				net.ParseIP("192.168.1.1"),
				net.ParseIP("10.0.0.1"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toNetIPs(tt.input)
			require.Len(t, result, len(tt.expected))
			for i, ip := range result {
				require.True(t, ip.Equal(tt.expected[i]), "index %d: expected %v, got %v", i, tt.expected[i], ip)
			}
		})
	}
}

func TestConfiguration_LoadFileConfigWithDuration(t *testing.T) {
	tests := []struct {
		name           string
		yamlContent    string
		expectError    bool
		expectedConfig *Configuration
	}{
		{
			name: "load config with string duration format",
			yamlContent: `
holdtime: 90s
graceful-restart-time: 90s
graceful-restart-deferral-time: 360s
`,
			expectError: false,
			expectedConfig: &Configuration{
				HoldTime:                    Duration{Duration: 90 * time.Second},
				GracefulRestartTime:         Duration{Duration: 90 * time.Second},
				GracefulRestartDeferralTime: Duration{Duration: 360 * time.Second},
			},
		},
		{
			name: "load config with integer duration format",
			yamlContent: `
holdtime: 90
graceful-restart-time: 90
graceful-restart-deferral-time: 360
`,
			expectError: false,
			expectedConfig: &Configuration{
				HoldTime:                    Duration{Duration: 90 * time.Second},
				GracefulRestartTime:         Duration{Duration: 90 * time.Second},
				GracefulRestartDeferralTime: Duration{Duration: 360 * time.Second},
			},
		},
		{
			name: "load config with mixed duration format",
			yamlContent: `
holdtime: 90s
graceful-restart-time: 90
graceful-restart-deferral-time: 6m
`,
			expectError: false,
			expectedConfig: &Configuration{
				HoldTime:                    Duration{Duration: 90 * time.Second},
				GracefulRestartTime:         Duration{Duration: 90 * time.Second},
				GracefulRestartDeferralTime: Duration{Duration: 6 * time.Minute},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Configuration
			err := yaml.Unmarshal([]byte(tt.yamlContent), &cfg)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedConfig.HoldTime, cfg.HoldTime)
				require.Equal(t, tt.expectedConfig.GracefulRestartTime, cfg.GracefulRestartTime)
				require.Equal(t, tt.expectedConfig.GracefulRestartDeferralTime, cfg.GracefulRestartDeferralTime)
			}
		})
	}
}

func TestConfiguration_CheckGracefulRestartOptions(t *testing.T) {
	tests := []struct {
		name          string
		config        *Configuration
		expectError   bool
		errorContains string
	}{
		{
			name: "valid graceful restart options",
			config: &Configuration{
				GracefulRestartTime:         Duration{Duration: 90 * time.Second},
				GracefulRestartDeferralTime: Duration{Duration: 360 * time.Second},
			},
			expectError: false,
		},
		{
			name: "graceful restart time too large",
			config: &Configuration{
				GracefulRestartTime:         Duration{Duration: 4096 * time.Second},
				GracefulRestartDeferralTime: Duration{Duration: 360 * time.Second},
			},
			expectError:   true,
			errorContains: "less than 4095 seconds",
		},
		{
			name: "graceful restart time zero",
			config: &Configuration{
				GracefulRestartTime:         Duration{Duration: 0},
				GracefulRestartDeferralTime: Duration{Duration: 360 * time.Second},
			},
			expectError:   true,
			errorContains: "more than 0",
		},
		{
			name: "graceful restart deferral time too large",
			config: &Configuration{
				GracefulRestartTime:         Duration{Duration: 90 * time.Second},
				GracefulRestartDeferralTime: Duration{Duration: 19 * time.Hour},
			},
			expectError:   true,
			errorContains: "less than 18 hours",
		},
		{
			name: "graceful restart deferral time zero",
			config: &Configuration{
				GracefulRestartTime:         Duration{Duration: 90 * time.Second},
				GracefulRestartDeferralTime: Duration{Duration: 0},
			},
			expectError:   true,
			errorContains: "more than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.checkGracefulRestartOptions()
			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					require.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfiguration_MergeFileConfig_BooleanPrecedence(t *testing.T) {
	tests := []struct {
		name           string
		baseConfig     *Configuration
		fileConfig     *Configuration
		expectedResult *Configuration
		description    string
	}{
		{
			name: "YAML omits boolean - CLI value preserved",
			baseConfig: &Configuration{
				AnnounceClusterIP: boolPtr(true),
				GracefulRestart:   boolPtr(true),
				PassiveMode:       boolPtr(true),
			},
			fileConfig: &Configuration{
				// All boolean fields are nil (not set in YAML)
				GrpcHost: IP{IP: net.ParseIP("10.0.0.1")},
			},
			expectedResult: &Configuration{
				AnnounceClusterIP: boolPtr(true),
				GracefulRestart:   boolPtr(true),
				PassiveMode:       boolPtr(true),
				GrpcHost:          IP{IP: net.ParseIP("10.0.0.1")},
			},
			description: "When YAML omits boolean fields, CLI values should be preserved",
		},
		{
			name: "YAML explicitly sets false - overrides CLI true",
			baseConfig: &Configuration{
				AnnounceClusterIP: boolPtr(true),
				GracefulRestart:   boolPtr(true),
			},
			fileConfig: &Configuration{
				AnnounceClusterIP: boolPtr(false),
				GracefulRestart:   boolPtr(false),
			},
			expectedResult: &Configuration{
				AnnounceClusterIP: boolPtr(false),
				GracefulRestart:   boolPtr(false),
			},
			description: "When YAML explicitly sets false, it should override CLI true",
		},
		{
			name: "YAML sets true - overrides CLI false",
			baseConfig: &Configuration{
				PassiveMode: boolPtr(false),
			},
			fileConfig: &Configuration{
				PassiveMode: boolPtr(true),
			},
			expectedResult: &Configuration{
				PassiveMode: boolPtr(true),
			},
			description: "When YAML sets true, it should override CLI false",
		},
		{
			name: "Mixed - some set, some omitted",
			baseConfig: &Configuration{
				AnnounceClusterIP: boolPtr(true),
				GracefulRestart:   boolPtr(false),
				PassiveMode:       boolPtr(true),
			},
			fileConfig: &Configuration{
				GracefulRestart: boolPtr(true),
				// AnnounceClusterIP and PassiveMode omitted
			},
			expectedResult: &Configuration{
				AnnounceClusterIP: boolPtr(true), // preserved from CLI
				GracefulRestart:   boolPtr(true), // overridden by YAML
				PassiveMode:       boolPtr(true), // preserved from CLI
			},
			description: "Mixed scenario - only explicitly set YAML fields override CLI",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.baseConfig.mergeFileConfig(tt.fileConfig)

			if tt.expectedResult.AnnounceClusterIP != nil {
				require.NotNil(t, tt.baseConfig.AnnounceClusterIP, "AnnounceClusterIP")
				require.Equal(t, *tt.expectedResult.AnnounceClusterIP, *tt.baseConfig.AnnounceClusterIP,
					"AnnounceClusterIP: %s", tt.description)
			}
			if tt.expectedResult.GracefulRestart != nil {
				require.NotNil(t, tt.baseConfig.GracefulRestart, "GracefulRestart")
				require.Equal(t, *tt.expectedResult.GracefulRestart, *tt.baseConfig.GracefulRestart,
					"GracefulRestart: %s", tt.description)
			}
			if tt.expectedResult.PassiveMode != nil {
				require.NotNil(t, tt.baseConfig.PassiveMode, "PassiveMode")
				require.Equal(t, *tt.expectedResult.PassiveMode, *tt.baseConfig.PassiveMode,
					"PassiveMode: %s", tt.description)
			}
			if tt.expectedResult.GrpcHost.IP != nil {
				require.True(t, tt.expectedResult.GrpcHost.Equal(tt.baseConfig.GrpcHost.IP), "GrpcHost")
			}
		})
	}
}
