package speaker

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"gopkg.in/yaml.v3"

	"github.com/kubeovn/kube-ovn/pkg/util"
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
				NatGwMode:         new(true),
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
		name        string
		yamlContent string
		want        Configuration
		wantError   bool
	}{
		{
			name:        "IPv4 neighbors",
			yamlContent: "neighbor-address: [192.168.1.1, 192.168.1.2]\ngrpc-host: 10.0.0.1\nrouter-id: 10.0.0.254\n",
			want: Configuration{
				NeighborAddresses: IPList{{IP: net.ParseIP("192.168.1.1")}, {IP: net.ParseIP("192.168.1.2")}},
				GrpcHost:          IP{IP: net.ParseIP("10.0.0.1")}, RouterID: IP{IP: net.ParseIP("10.0.0.254")},
			},
		},
		{
			name:        "IPv6 neighbors",
			yamlContent: "neighbor-ipv6-address: [2001:db8::1, 2001:db8::2]\ngrpc-host: fd00::1\nrouter-id: fd00::254\n",
			want: Configuration{
				NeighborIPv6Addresses: IPList{{IP: net.ParseIP("2001:db8::1")}, {IP: net.ParseIP("2001:db8::2")}},
				GrpcHost:              IP{IP: net.ParseIP("fd00::1")}, RouterID: IP{IP: net.ParseIP("fd00::254")},
			},
		},
		{
			name: "mixed address families",
			yamlContent: `neighbor-address: [192.168.1.1]
neighbor-ipv6-address: [2001:db8::1]
allowed-source-addresses: [10.0.0.1, 10.0.0.2]
allowed-source-ipv6-addresses: [fd00::1]
grpc-host: 192.168.1.100
router-id: 192.168.1.254
`,
			want: Configuration{
				NeighborAddresses:          IPList{{IP: net.ParseIP("192.168.1.1")}},
				NeighborIPv6Addresses:      IPList{{IP: net.ParseIP("2001:db8::1")}},
				AllowedSourceAddresses:     IPList{{IP: net.ParseIP("10.0.0.1")}, {IP: net.ParseIP("10.0.0.2")}},
				AllowedSourceIPv6Addresses: IPList{{IP: net.ParseIP("fd00::1")}},
				GrpcHost:                   IP{IP: net.ParseIP("192.168.1.100")}, RouterID: IP{IP: net.ParseIP("192.168.1.254")},
			},
		},
		{name: "invalid IPv4", yamlContent: "neighbor-address: [192.168.1.256]", wantError: true},
		{name: "invalid format", yamlContent: "neighbor-address: [not-an-ip]", wantError: true},
		{
			name:        "node IPs",
			yamlContent: "node-ips: {IPv4: 10.0.0.1, IPv6: 'fd00::1'}",
			want: Configuration{NodeIPs: map[string]IP{
				"IPv4": {IP: net.ParseIP("10.0.0.1")}, "IPv6": {IP: net.ParseIP("fd00::1")},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Configuration
			err := yaml.Unmarshal([]byte(tt.yamlContent), &cfg)
			if tt.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, cfg)
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
		name          string
		yamlValue     string
		want          time.Duration
		errorContains string
	}{
		{name: "string seconds", yamlValue: "90s", want: 90 * time.Second},
		{name: "string minutes", yamlValue: "6m", want: 6 * time.Minute},
		{name: "string hours", yamlValue: "1h", want: time.Hour},
		{name: "complex string", yamlValue: "1h30m", want: time.Hour + 30*time.Minute},
		{name: "integer seconds", yamlValue: "360", want: 360 * time.Second},
		{name: "integer zero", yamlValue: "0"},
		{name: "largest integer seconds", yamlValue: "9223372036", want: 9223372036 * time.Second},
		{name: "smallest integer seconds", yamlValue: "-9223372036", want: -9223372036 * time.Second},
		{name: "positive overflow", yamlValue: "9223372037", errorContains: "overflows time.Duration"},
		{name: "negative overflow", yamlValue: "-9223372037", errorContains: "overflows time.Duration"},
		{name: "overflow wraps into valid holdtime", yamlValue: "18446744164", errorContains: "overflows time.Duration"},
		{name: "invalid string", yamlValue: "invalid", errorContains: "invalid duration"},
		{name: "string without unit", yamlValue: "\"360\"", errorContains: "missing unit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration
			err := yaml.Unmarshal([]byte(tt.yamlValue), &d)
			if tt.errorContains != "" {
				require.ErrorContains(t, err, tt.errorContains)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, d.Duration)
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
			errorContains: "GracefulRestartTime should be between 1 and 4095 seconds",
		},
		{
			name: "graceful restart time zero",
			config: &Configuration{
				GracefulRestartTime:         Duration{Duration: 0},
				GracefulRestartDeferralTime: Duration{Duration: 360 * time.Second},
			},
			expectError:   true,
			errorContains: "GracefulRestartTime should be between 1 and 4095 seconds",
		},
		{
			name: "graceful restart deferral time too large",
			config: &Configuration{
				GracefulRestartTime:         Duration{Duration: 90 * time.Second},
				GracefulRestartDeferralTime: Duration{Duration: 19 * time.Hour},
			},
			expectError:   true,
			errorContains: "GracefulRestartDeferralTime should be between 1 second and 18 hours",
		},
		{
			name: "graceful restart deferral time zero",
			config: &Configuration{
				GracefulRestartTime:         Duration{Duration: 90 * time.Second},
				GracefulRestartDeferralTime: Duration{Duration: 0},
			},
			expectError:   true,
			errorContains: "GracefulRestartDeferralTime should be between 1 second and 18 hours",
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

func TestParseFlags_RejectInvalidYAMLValues(t *testing.T) {
	tests := []struct {
		name          string
		yamlContent   string
		errorContains string
	}{
		{"overflowing holdtime", "holdtime: 18446744164\n", "overflows time.Duration"},
		{"null IPv6 neighbor", "neighbor-ipv6-address: [null]\n", "invalid IP address list entry"},
		{"empty IPv6 neighbor", "neighbor-ipv6-address: [\"\"]\n", "invalid IP address list entry"},
		{"null IPv6 allowed source", "allowed-source-ipv6-addresses: [null]\n", "invalid IP address list entry"},
		{"empty IPv6 allowed source", "allowed-source-ipv6-addresses: [\"\"]\n", "invalid IP address list entry"},
		{"zero holdtime", "holdtime: 0\n", "holdtime must be in the range"},
		{"null holdtime", "holdtime: null\n", "holdtime must be in the range"},
		{"zero multihop", "ebgp-multihop: 0\n", "MultihopTtl must be in the range"},
		{"zero BFD TX", "enable-bfd: true\nbfd-min-tx: 0\n", "--bfd-min-tx must be > 0"},
		{"zero BFD RX", "enable-bfd: true\nbfd-min-rx: 0\n", "--bfd-min-rx must be > 0"},
		{"zero BFD multiplier", "enable-bfd: true\nbfd-detection-multiplier: 0\n", "--bfd-detection-multiplier must be between"},
		{"zero local AS", "cluster-as: 0\n", "--cluster-as must be specified"},
		{"zero neighbor AS", "neighbor-as: 0\n", "--neighbor-as must be specified"},
		{"empty node name", "node-name: \"\"\n", "--node-name must be specified"},
		{"IPv4 node IP under IPv6", "node-ips: {IPv6: 192.0.2.1}\n", "expected IPv6"},
		{"mapped IPv4 node IP under IPv6", "node-ips: {IPv6: '::ffff:192.0.2.1'}\n", "expected IPv6"},
		{"IPv6 node IP under IPv4", "node-ips: {IPv4: '2001:db8::1'}\n", "expected IPv4"},
		{"unknown node IP key", "node-ips: {ipv4: 192.0.2.1}\n", "invalid node-ips key"},
		{"second YAML document", "cluster-as: 65000\n---\nneighbor-as: 65001\n", "only one YAML document"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldArgs, oldFlags, oldPflags := os.Args, flag.CommandLine, pflag.CommandLine
			t.Cleanup(func() {
				os.Args, flag.CommandLine, pflag.CommandLine = oldArgs, oldFlags, oldPflags
			})
			flag.CommandLine = flag.NewFlagSet("speaker", flag.ContinueOnError)
			pflag.CommandLine = pflag.NewFlagSet("speaker", pflag.ContinueOnError)
			t.Setenv(util.EnvPodIP, "")
			t.Setenv(util.EnvPodIPs, "")
			configFile := filepath.Join(t.TempDir(), "speaker.yaml")
			require.NoError(t, os.WriteFile(configFile, []byte(tt.yamlContent), 0o600))
			os.Args = []string{
				"speaker", "--config", configFile,
				"--neighbor-address", "192.0.2.1", "--cluster-as", "65000",
				"--neighbor-as", "65001", "--node-name", "node1",
			}
			config, err := ParseFlags()
			require.ErrorContains(t, err, tt.errorContains)
			require.Nil(t, config)
		})
	}
}

func TestConfiguration_LoadFileConfig(t *testing.T) {
	tests := []struct {
		name          string
		yamlContent   string
		errorContains string
	}{
		{name: "empty file"},
		{name: "explicit null document", yamlContent: "null"},
		{name: "second valid document", yamlContent: "cluster-as: 65000\n---\nneighbor-as: 65001\n", errorContains: "only one YAML document"},
		{name: "second unknown document", yamlContent: "cluster-as: 65000\n---\nunknown: true\n", errorContains: "only one YAML document"},
		{name: "second malformed document", yamlContent: "cluster-as: 65000\n---\nneighbor-address: [\n", errorContains: "yaml:"},
		{name: "second empty document", yamlContent: "{}\n---\n", errorContains: "only one YAML document"},
		{name: "IPv4 node IP under IPv6", yamlContent: "node-ips: {IPv6: 192.0.2.1}", errorContains: "expected IPv6"},
		{name: "mapped IPv4 node IP under IPv6", yamlContent: "node-ips: {IPv6: '::ffff:192.0.2.1'}", errorContains: "expected IPv6"},
		{name: "IPv6 node IP under IPv4", yamlContent: "node-ips: {IPv4: '2001:db8::1'}", errorContains: "expected IPv4"},
		{name: "unknown node IP key", yamlContent: "node-ips: {ipv4: 192.0.2.1}", errorContains: "invalid node-ips key"},
		{name: "unknown null node IP key", yamlContent: "node-ips: {ipv4: null}", errorContains: "invalid node-ips key"},
		{name: "valid node IPs", yamlContent: "node-ips: {IPv4: 192.0.2.1, IPv6: '2001:db8::1'}"},
		{name: "mapped IPv4 node IP under IPv4", yamlContent: "node-ips: {IPv4: '::ffff:192.0.2.1'}"},
		{name: "comments only", yamlContent: "# Use CLI values\n"},
		{name: "known fields", yamlContent: "cluster-as: 65000\nholdtime: 90s\nneighbor-address: [192.0.2.1]\n"},
		{
			name:          "unknown field",
			yamlContent:   "cluster-as: 65000\nneighbor-address-extra: [192.0.2.1]\n",
			errorContains: "field neighbor-address-extra not found",
		},
		{
			name:          "runtime field is not configurable",
			yamlContent:   "config-file: /tmp/speaker.yaml\n",
			errorContains: "field config-file not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Configuration{ConfigFile: filepath.Join(t.TempDir(), "speaker.yaml")}
			require.NoError(t, os.WriteFile(config.ConfigFile, []byte(tt.yamlContent), 0o600))
			cfg, err := config.loadFileConfig()
			if tt.errorContains != "" {
				require.ErrorContains(t, err, tt.errorContains)
				require.Nil(t, cfg)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, cfg)
			if tt.name == "known fields" {
				require.Equal(t, uint32(65000), cfg.ClusterAs)
				require.Equal(t, 90*time.Second, cfg.HoldTime.Duration)
				require.Equal(t, IPList{{IP: net.ParseIP("192.0.2.1")}}, cfg.NeighborAddresses)
			}
		})
	}
}

func TestConfiguration_MergeFileConfig_AddressPrecedence(t *testing.T) {
	fields := []struct {
		name        string
		cliAddress  string
		yamlAddress string
		addresses   func(*Configuration) *IPList
	}{
		{"neighbor-address", "192.0.2.1", "192.0.2.2", func(c *Configuration) *IPList { return &c.NeighborAddresses }},
		{"neighbor-ipv6-address", "2001:db8::1", "2001:db8::2", func(c *Configuration) *IPList { return &c.NeighborIPv6Addresses }},
		{"allowed-source-addresses", "192.0.2.3", "192.0.2.4", func(c *Configuration) *IPList { return &c.AllowedSourceAddresses }},
		{"allowed-source-ipv6-addresses", "2001:db8::3", "2001:db8::4", func(c *Configuration) *IPList { return &c.AllowedSourceIPv6Addresses }},
	}
	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			tests := []struct {
				name        string
				yamlContent string
				want        []IP
			}{
				{"omitted", "{}", []IP{{IP: net.ParseIP(field.cliAddress)}}},
				{"explicit empty", fmt.Sprintf("%s: []\n", field.name), []IP{}},
				{"explicit null", fmt.Sprintf("%s: null\n", field.name), nil},
				{"replacement", fmt.Sprintf("%s: [%s]\n", field.name, field.yamlAddress), []IP{{IP: net.ParseIP(field.yamlAddress)}}},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					config := &Configuration{ConfigFile: filepath.Join(t.TempDir(), "speaker.yaml")}
					*field.addresses(config) = []IP{{IP: net.ParseIP(field.cliAddress)}}
					require.NoError(t, os.WriteFile(config.ConfigFile, []byte(tt.yamlContent), 0o600))
					cfg, err := config.loadFileConfig()
					require.NoError(t, err)
					config.mergeFileConfig(cfg)
					require.Equal(t, IPList(tt.want), *field.addresses(config))
				})
			}
			for _, entry := range []string{"null", "\"\""} {
				t.Run("invalid entry "+entry, func(t *testing.T) {
					config := &Configuration{ConfigFile: filepath.Join(t.TempDir(), "speaker.yaml")}
					yamlContent := fmt.Sprintf("%s: [%s]\n", field.name, entry)
					require.NoError(t, os.WriteFile(config.ConfigFile, []byte(yamlContent), 0o600))
					cfg, err := config.loadFileConfig()
					require.ErrorContains(t, err, "invalid IP address list entry")
					require.Nil(t, cfg)
				})
			}
		})
	}
}

func TestConfiguration_MergeFileConfig_BooleanPrecedence(t *testing.T) {
	tests := []struct {
		name string
		base Configuration
		file Configuration
		want Configuration
	}{
		{
			name: "omitted booleans preserve CLI",
			base: Configuration{AnnounceClusterIP: new(true), GracefulRestart: new(true), PassiveMode: new(true)},
			file: Configuration{GrpcHost: IP{IP: net.ParseIP("10.0.0.1")}},
			want: Configuration{AnnounceClusterIP: new(true), GracefulRestart: new(true), PassiveMode: new(true), GrpcHost: IP{IP: net.ParseIP("10.0.0.1")}},
		},
		{
			name: "false overrides true",
			base: Configuration{AnnounceClusterIP: new(true), GracefulRestart: new(true)},
			file: Configuration{AnnounceClusterIP: new(false), GracefulRestart: new(false)},
			want: Configuration{AnnounceClusterIP: new(false), GracefulRestart: new(false)},
		},
		{
			name: "true overrides false",
			base: Configuration{PassiveMode: new(false)},
			file: Configuration{PassiveMode: new(true)},
			want: Configuration{PassiveMode: new(true)},
		},
		{
			name: "mixed set and omitted",
			base: Configuration{AnnounceClusterIP: new(true), GracefulRestart: new(false), PassiveMode: new(true)},
			file: Configuration{GracefulRestart: new(true)},
			want: Configuration{AnnounceClusterIP: new(true), GracefulRestart: new(true), PassiveMode: new(true)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.base.mergeFileConfig(&tt.file)
			require.Equal(t, tt.want, tt.base)
		})
	}
}

func TestConfiguration_MergeFileConfig_ScalarPresence(t *testing.T) {
	base := Configuration{
		GrpcHost: IP{IP: net.ParseIP("127.0.0.1")}, GrpcPort: 50051, ClusterAs: 65000,
		RouterID: IP{IP: net.ParseIP("192.0.2.1")}, NeighborAs: 65001, AuthPassword: "cli-secret",
		HoldTime: Duration{Duration: 90 * time.Second}, EbgpMultihopTTL: 1,
		GracefulRestartTime:         Duration{Duration: 90 * time.Second},
		GracefulRestartDeferralTime: Duration{Duration: 360 * time.Second},
		BFDMinTX:                    1000, BFDMinRX: 1000, BFDDetectionMultiplier: 3,
		NodeName: "node1", KubeConfigFile: "cli.conf", PprofPort: 10667, LogPerm: "640",
	}
	fields := []struct {
		name       string
		clearValue string
		get        func(*Configuration) any
		want       any
	}{
		{"grpc-host", `""`, func(c *Configuration) any { return c.GrpcHost.IP }, net.IP(nil)},
		{"grpc-port", "0", func(c *Configuration) any { return c.GrpcPort }, int32(0)},
		{"cluster-as", "0", func(c *Configuration) any { return c.ClusterAs }, uint32(0)},
		{"router-id", `""`, func(c *Configuration) any { return c.RouterID.IP }, net.IP(nil)},
		{"neighbor-as", "0", func(c *Configuration) any { return c.NeighborAs }, uint32(0)},
		{"auth-password", `""`, func(c *Configuration) any { return c.AuthPassword }, ""},
		{"holdtime", "0", func(c *Configuration) any { return c.HoldTime.Duration }, time.Duration(0)},
		{"graceful-restart-time", "0", func(c *Configuration) any { return c.GracefulRestartTime.Duration }, time.Duration(0)},
		{"graceful-restart-deferral-time", "0", func(c *Configuration) any { return c.GracefulRestartDeferralTime.Duration }, time.Duration(0)},
		{"ebgp-multihop", "0", func(c *Configuration) any { return c.EbgpMultihopTTL }, uint8(0)},
		{"bfd-min-tx", "0", func(c *Configuration) any { return c.BFDMinTX }, uint32(0)},
		{"bfd-min-rx", "0", func(c *Configuration) any { return c.BFDMinRX }, uint32(0)},
		{"bfd-detection-multiplier", "0", func(c *Configuration) any { return c.BFDDetectionMultiplier }, uint8(0)},
		{"node-name", `""`, func(c *Configuration) any { return c.NodeName }, ""},
		{"kubeconfig", `""`, func(c *Configuration) any { return c.KubeConfigFile }, ""},
		{"pprof-port", "0", func(c *Configuration) any { return c.PprofPort }, int32(0)},
		{"log-perm", `""`, func(c *Configuration) any { return c.LogPerm }, ""},
	}
	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			for _, value := range []string{"omitted", field.clearValue, "null"} {
				t.Run(value, func(t *testing.T) {
					config := base
					config.ConfigFile = filepath.Join(t.TempDir(), "speaker.yaml")
					content, want := fmt.Sprintf("%s: %s\n", field.name, value), field.want
					if value == "omitted" {
						content, want = "{}", field.get(&base)
					}
					require.NoError(t, os.WriteFile(config.ConfigFile, []byte(content), 0o600))
					cfg, err := config.loadFileConfig()
					require.NoError(t, err)
					config.mergeFileConfig(cfg)
					require.Equal(t, want, field.get(&config))
				})
			}
		})
	}
	// Non-zero values from directly constructed configs retain the existing merge behavior.
	var config Configuration
	config.mergeFileConfig(&base)
	require.Equal(t, base, config)
	config.mergeFileConfig(&Configuration{})
	require.Equal(t, base, config)
}

func TestConfiguration_MergeFileConfig_NodeIPs(t *testing.T) {
	ipv4, ipv6 := IP{IP: net.ParseIP("192.0.2.1")}, IP{IP: net.ParseIP("2001:db8::1")}
	replacement := IP{IP: net.ParseIP("192.0.2.2")}
	tests := []struct {
		name        string
		yamlContent string
		want        map[string]IP
	}{
		{"omitted", "{}", map[string]IP{"IPv4": ipv4, "IPv6": ipv6}},
		{"null map", "node-ips: null", nil},
		{"empty map", "node-ips: {}", nil},
		{"null IPv4", "node-ips: {IPv4: null}", map[string]IP{"IPv6": ipv6}},
		{"empty IPv4", `node-ips: {IPv4: ""}`, map[string]IP{"IPv6": ipv6}},
		{"null IPv6", "node-ips: {IPv6: null}", map[string]IP{"IPv4": ipv4}},
		{"empty IPv6", `node-ips: {IPv6: ""}`, map[string]IP{"IPv4": ipv4}},
		{"replace IPv4", "node-ips: {IPv4: 192.0.2.2}", map[string]IP{"IPv4": replacement, "IPv6": ipv6}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Configuration{
				ConfigFile: filepath.Join(t.TempDir(), "speaker.yaml"),
				NodeIPs:    map[string]IP{"IPv4": ipv4, "IPv6": ipv6},
			}
			require.NoError(t, os.WriteFile(config.ConfigFile, []byte(tt.yamlContent), 0o600))
			cfg, err := config.loadFileConfig()
			require.NoError(t, err)
			config.mergeFileConfig(cfg)
			require.Equal(t, tt.want, config.NodeIPs)
		})
	}
}

func TestConfiguration_MergeFileConfig_YAMLBooleans(t *testing.T) {
	fields := []struct {
		name string
		get  func(*Configuration) **bool
	}{
		{"announce-cluster-ip", func(c *Configuration) **bool { return &c.AnnounceClusterIP }},
		{"graceful-restart", func(c *Configuration) **bool { return &c.GracefulRestart }},
		{"passivemode", func(c *Configuration) **bool { return &c.PassiveMode }},
		{"extended-nexthop", func(c *Configuration) **bool { return &c.ExtendedNexthop }},
		{"nat-gw-mode", func(c *Configuration) **bool { return &c.NatGwMode }},
		{"enable-metrics", func(c *Configuration) **bool { return &c.EnableMetrics }},
		{"enable-bfd", func(c *Configuration) **bool { return &c.EnableBFD }},
	}
	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			for _, value := range []string{"omitted", "false", "true", "null"} {
				t.Run(value, func(t *testing.T) {
					config := Configuration{ConfigFile: filepath.Join(t.TempDir(), "speaker.yaml")}
					*field.get(&config) = new(true)
					content := fmt.Sprintf("%s: %s\n", field.name, value)
					if value == "omitted" {
						content = "{}"
					}
					require.NoError(t, os.WriteFile(config.ConfigFile, []byte(content), 0o600))
					cfg, err := config.loadFileConfig()
					require.NoError(t, err)
					config.mergeFileConfig(cfg)
					if value == "null" {
						require.Nil(t, *field.get(&config))
					} else {
						require.NotNil(t, *field.get(&config))
						require.Equal(t, value != "false", **field.get(&config))
					}
				})
			}
		})
	}
}

func TestConfiguration_MergeFileConfig_YAMLAliases(t *testing.T) {
	config := Configuration{
		ConfigFile:   filepath.Join(t.TempDir(), "speaker.yaml"),
		AuthPassword: "cli-secret", HoldTime: Duration{Duration: 90 * time.Second},
	}
	// Merge keys and aliases must preserve explicit zero/empty values as well.
	content := "<<: &defaults {auth-password: '', holdtime: 0}\nrouter-id: &unset null\ngrpc-host: *unset\n"
	require.NoError(t, os.WriteFile(config.ConfigFile, []byte(content), 0o600))
	cfg, err := config.loadFileConfig()
	require.NoError(t, err)
	config.mergeFileConfig(cfg)
	require.Empty(t, config.AuthPassword)
	require.Zero(t, config.HoldTime.Duration)
}
