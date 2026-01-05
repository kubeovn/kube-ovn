package ovs

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func Test_parseIpv6RaConfigs(t *testing.T) {
	t.Parallel()

	t.Run("return default ipv6 ra config", func(t *testing.T) {
		t.Parallel()
		config := parseIpv6RaConfigs("")
		require.Equal(t, map[string]string{
			"address_mode":  "dhcpv6_stateful",
			"max_interval":  "30",
			"min_interval":  "5",
			"send_periodic": "true",
		}, config)
	})

	t.Run("return custom ipv6 ra config", func(t *testing.T) {
		t.Parallel()
		config := parseIpv6RaConfigs("address_mode=dhcpv6_stateful,max_interval =30,min_interval=5,send_periodic=,test")
		require.Equal(t, map[string]string{
			"address_mode": "dhcpv6_stateful",
			"max_interval": "30",
			"min_interval": "5",
		}, config)
	})

	t.Run("no validation in ipv6 ra config", func(t *testing.T) {
		t.Parallel()
		config := parseIpv6RaConfigs("send_periodic=,test")
		require.Empty(t, config)
	})
}

func Test_parseDHCPOptions(t *testing.T) {
	t.Parallel()

	t.Run("return dhcp options", func(t *testing.T) {
		t.Parallel()
		dhcpOpt := parseDHCPOptions("server_id= 192.168.123.50,server_mac =00:00:00:08:0a:11,router=,test")
		require.Equal(t, map[string]string{
			"server_id":  "192.168.123.50",
			"server_mac": "00:00:00:08:0a:11",
		}, dhcpOpt)
	})

	t.Run("no validation dhcp options", func(t *testing.T) {
		t.Parallel()
		dhcpOpt := parseDHCPOptions("router=,test")
		require.Empty(t, dhcpOpt)
	})

	t.Run("dns_server option with semicolons", func(t *testing.T) {
		t.Parallel()
		result := parseDHCPOptions("dns_server=8.8.8.8;8.8.4.4,server_id=192.168.1.1")
		expected := map[string]string{
			"dns_server": "8.8.8.8,8.8.4.4",
			"server_id":  "192.168.1.1",
		}
		require.Equal(t, expected, result)
	})
}

func Test_getIpv6Prefix(t *testing.T) {
	t.Parallel()

	t.Run("return prefix when exists one ipv6 networks", func(t *testing.T) {
		t.Parallel()
		config := getIpv6Prefix([]string{"192.168.100.1/24", "fd00::c0a8:6401/120"})
		require.ElementsMatch(t, []string{"120"}, config)
	})

	t.Run("return multiple prefix when exists more than one ipv6 networks", func(t *testing.T) {
		t.Parallel()
		config := getIpv6Prefix([]string{"192.168.100.1/24", "fd00::c0a8:6401/120", "fd00::c0a8:6501/60"})
		require.ElementsMatch(t, []string{"120", "60"}, config)
	})
}

func Test_matchAddressSetName(t *testing.T) {
	t.Parallel()

	asName := "ovn.sg.sg.associated.v4"
	matched := matchAddressSetName(asName)
	require.True(t, matched)

	asName = "ovn.sg.sg.associated.v4.123"
	matched = matchAddressSetName(asName)
	require.True(t, matched)

	asName = "ovn-sg.sg.associated.v4"
	matched = matchAddressSetName(asName)
	require.False(t, matched)

	asName = "123ovn.sg.sg.associated.v4"
	matched = matchAddressSetName(asName)
	require.False(t, matched)

	asName = "123.ovn.sg.sg.associated.v4"
	matched = matchAddressSetName(asName)
	require.False(t, matched)
}

func Test_aclMatch_Match(t *testing.T) {
	t.Parallel()

	t.Run("generate rule like 'ip4.src == $test.allow.as'", func(t *testing.T) {
		t.Parallel()

		match := NewACLMatch("ip4.dst", "==", "$test.allow.as", "")
		rule, err := match.Match()
		require.NoError(t, err)
		require.Equal(t, "ip4.dst == $test.allow.as", rule)

		match = NewACLMatch("ip4.dst", "!=", "$test.allow.as", "")
		rule, err = match.Match()
		require.NoError(t, err)
		require.Equal(t, "ip4.dst != $test.allow.as", rule)
	})

	t.Run("generate acl match rule like 'ip'", func(t *testing.T) {
		t.Parallel()

		match := NewACLMatch("ip", "==", "", "")

		rule, err := match.Match()
		require.NoError(t, err)
		require.Equal(t, "ip", rule)
	})

	t.Run("generate rule like '12345 <= tcp.dst <= 12500'", func(t *testing.T) {
		t.Parallel()

		match := NewACLMatch("tcp.dst", "<=", "12345", "12500")
		rule, err := match.Match()
		require.NoError(t, err)
		require.Equal(t, "12345 <= tcp.dst <= 12500", rule)
	})

	t.Run("err occurred when key is empty", func(t *testing.T) {
		t.Parallel()

		match := NewAndACLMatch(
			NewACLMatch("", "", "", ""),
		)

		_, err := match.Match()
		require.ErrorContains(t, err, "acl rule key is required")
	})
}

func Test_AndAclMatch_Match(t *testing.T) {
	t.Parallel()

	t.Run("generate acl match rule", func(t *testing.T) {
		t.Parallel()

		/* match several tcp port traffic */
		match := NewAndACLMatch(
			NewACLMatch("inport", "==", "@ovn.sg.test_sg", ""),
			NewACLMatch("ip", "", "", ""),
			NewACLMatch("ip4.dst", "==", "$test.allow.as", ""),
			NewACLMatch("ip4.dst", "!=", "$test.except.as", ""),
			NewACLMatch("tcp.dst", "<=", "12345", "12500"),
		)

		rule, err := match.Match()
		require.NoError(t, err)
		require.Equal(t, "inport == @ovn.sg.test_sg && ip && ip4.dst == $test.allow.as && ip4.dst != $test.except.as && 12345 <= tcp.dst <= 12500", rule)
	})

	t.Run("err occurred when key is empty", func(t *testing.T) {
		t.Parallel()

		match := NewAndACLMatch(
			NewACLMatch("", "", "", ""),
		)

		_, err := match.Match()
		require.ErrorContains(t, err, "acl rule key is required")
	})
}

func Test_OrAclMatch_Match(t *testing.T) {
	t.Parallel()

	t.Run("has one rule", func(t *testing.T) {
		t.Parallel()

		/* match several tcp port traffic */
		match := NewOrACLMatch(
			NewAndACLMatch(
				NewACLMatch("ip4.src", "==", "10.250.0.0/16", ""),
			),
			NewAndACLMatch(
				NewACLMatch("ip4.src", "==", "10.244.0.0/16", ""),
			),
			NewACLMatch("ip4.src", "==", "10.260.0.0/16", ""),
		)

		rule, err := match.Match()
		require.NoError(t, err)
		require.Equal(t, "ip4.src == 10.250.0.0/16 || ip4.src == 10.244.0.0/16 || ip4.src == 10.260.0.0/16", rule)
	})

	t.Run("has several rules", func(t *testing.T) {
		t.Parallel()

		/* match several tcp port traffic */
		match := NewOrACLMatch(
			NewAndACLMatch(
				NewACLMatch("ip4.src", "==", "10.250.0.0/16", ""),
				NewACLMatch("ip4.dst", "==", "10.244.0.0/16", ""),
			),
			NewAndACLMatch(
				NewACLMatch("ip4.src", "==", "10.244.0.0/16", ""),
				NewACLMatch("ip4.dst", "==", "10.250.0.0/16", ""),
			),
		)

		rule, err := match.Match()
		require.NoError(t, err)
		require.Equal(t, "(ip4.src == 10.250.0.0/16 && ip4.dst == 10.244.0.0/16) || (ip4.src == 10.244.0.0/16 && ip4.dst == 10.250.0.0/16)", rule)
	})

	t.Run("err occurred when key is empty", func(t *testing.T) {
		t.Parallel()

		match := NewAndACLMatch(
			NewACLMatch("", "", "", ""),
		)

		_, err := match.Match()
		require.ErrorContains(t, err, "acl rule key is required")
	})

	t.Run("error propagation", func(t *testing.T) {
		t.Parallel()
		match := NewOrACLMatch(
			NewACLMatch("", "", "", ""),
		)
		_, err := match.Match()
		require.Error(t, err)
		require.Contains(t, err.Error(), "acl rule key is required")
	})
}

func Test_Limiter(t *testing.T) {
	t.Parallel()

	t.Run("without limit", func(t *testing.T) {
		t.Parallel()

		var (
			limiter *Limiter
			err     error
		)

		limiter = new(Limiter)

		err = limiter.Wait(context.Background())
		require.NoError(t, err)
		require.Equal(t, int32(1), limiter.Current())

		err = limiter.Wait(context.Background())
		require.NoError(t, err)
		require.Equal(t, int32(2), limiter.Current())

		limiter.Done()
		require.Equal(t, int32(1), limiter.Current())

		limiter.Done()
		require.Equal(t, int32(0), limiter.Current())
	})

	t.Run("with limit", func(t *testing.T) {
		t.Parallel()

		var (
			limiter *Limiter
			err     error
		)

		limiter = new(Limiter)
		limiter.Update(2)

		err = limiter.Wait(context.Background())
		require.NoError(t, err)
		require.Equal(t, int32(1), limiter.Current())

		err = limiter.Wait(context.Background())
		require.NoError(t, err)
		require.Equal(t, int32(2), limiter.Current())

		time.AfterFunc(10*time.Second, func() {
			limiter.Done()
			require.Equal(t, int32(1), limiter.Current())
		})

		err = limiter.Wait(context.Background())
		require.NoError(t, err)
		require.Equal(t, int32(2), limiter.Current())
	})

	t.Run("with timeout", func(t *testing.T) {
		t.Parallel()

		var (
			limiter *Limiter
			err     error
		)

		limiter = new(Limiter)
		limiter.Update(2)

		err = limiter.Wait(context.Background())
		require.NoError(t, err)
		require.Equal(t, int32(1), limiter.Current())

		err = limiter.Wait(context.Background())
		require.NoError(t, err)
		require.Equal(t, int32(2), limiter.Current())

		time.AfterFunc(10*time.Second, func() {
			limiter.Done()
		})

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		err = limiter.Wait(ctx)
		require.ErrorContains(t, err, "context canceled by timeout")
		require.Equal(t, int32(2), limiter.Current())
	})

	t.Run("default limit", func(t *testing.T) {
		t.Parallel()

		limiter := new(Limiter)
		require.Equal(t, int32(0), limiter.Limit())
	})
}

func TestPodNameToPortName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		pod       string
		namespace string
		provider  string
		expected  string
	}{
		{
			name:      "OvnProvider",
			pod:       "test-pod",
			namespace: "default",
			provider:  util.OvnProvider,
			expected:  "test-pod.default",
		},
		{
			name:      "NonOvnProvider",
			pod:       "test-pod",
			namespace: metav1.NamespaceSystem,
			provider:  "custom-provider",
			expected:  "test-pod.kube-system.custom-provider",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := PodNameToPortName(tc.pod, tc.namespace, tc.provider)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestTrimCommandOutput(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "Whitespace only",
			input:    []byte("   \t\n"),
			expected: "",
		},
		{
			name:     "Quoted string",
			input:    []byte(`"Hello, World!"`),
			expected: "Hello, World!",
		},
		{
			name:     "Unquoted string with spaces",
			input:    []byte("  Hello, World!\t\n"),
			expected: "Hello, World!",
		},
		{
			name:     "Single quotes",
			input:    []byte(`'Hello, World!'`),
			expected: "'Hello, World!'",
		},
		{
			name:     "Newlines and tabs",
			input:    []byte("\t\"Hello,\nWorld!\"\n"),
			expected: "Hello,\nWorld!",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := trimCommandOutput(tc.input)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestLogicalRouterPortName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		lr       string
		ls       string
		expected string
	}{
		{
			name:     "Standard case",
			lr:       "router1",
			ls:       "switch1",
			expected: "router1-switch1",
		},
		{
			name:     "Names with special characters",
			lr:       "router-1",
			ls:       "switch_1",
			expected: "router-1-switch_1",
		},
		{
			name:     "Long names",
			lr:       "very_long_router_name_123456789",
			ls:       "extremely_long_switch_name_987654321",
			expected: "very_long_router_name_123456789-extremely_long_switch_name_987654321",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := LogicalRouterPortName(tc.lr, tc.ls)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestLogicalSwitchPortName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		lr       string
		ls       string
		expected string
	}{
		{
			name:     "Standard case",
			lr:       "router1",
			ls:       "switch1",
			expected: "switch1-router1",
		},
		{
			name:     "Names with special characters",
			lr:       "router-1",
			ls:       "switch_1",
			expected: "switch_1-router-1",
		},
		{
			name:     "Long names",
			lr:       "very_long_router_name_123456789",
			ls:       "extremely_long_switch_name_987654321",
			expected: "extremely_long_switch_name_987654321-very_long_router_name_123456789",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := LogicalSwitchPortName(tc.lr, tc.ls)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestFormatDHCPOptions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		options  map[string]string
		expected string
	}{
		{
			name: "DNS server with commas",
			options: map[string]string{
				"dns_server": "{8.8.8.8,1.1.1.1}",
			},
			expected: "dns_server={8.8.8.8;1.1.1.1}",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := formatDHCPOptions(tc.options)
			require.Equal(t, tc.expected, result)
		})
	}
}
