package ovs

import (
	"testing"

	"github.com/stretchr/testify/require"
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
		require.Equal(t, map[string]string{}, config)
		require.Equal(t, 0, len(config))
	})
}

func Test_getIpv6Prefix(t *testing.T) {
	t.Parallel()

	t.Run("return prefix when exists one ipv6 networks", func(t *testing.T) {
		t.Parallel()
		config := getIpv6Prefix([]string{"192.168.100.1/24", "fd00::c0a8:6401/120"})
		require.Equal(t, []string{"120"}, config)
	})

	t.Run("return multiple prefix when exists more than one ipv6 networks", func(t *testing.T) {
		t.Parallel()
		config := getIpv6Prefix([]string{"192.168.100.1/24", "fd00::c0a8:6401/120", "fd00::c0a8:6501/60"})
		require.Equal(t, []string{"120", "60"}, config)
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

func Test_Rule(t *testing.T) {
	t.Parallel()

	t.Run("generate acl match rule", func(t *testing.T) {
		t.Parallel()

		/* match all ip traffic */
		AllIpMatch := NewAndAclMatchRule(
			NewAclRuleKv("inport", "==", "@ovn.sg.test_sg", ""),
			NewAclRuleKv("ip", "", "", ""),
		)

		rule, err := AllIpMatch.Rule()
		require.NoError(t, err)
		require.Equal(t, "inport == @ovn.sg.test_sg && ip", rule)

		/* match allowed ip traffic */
		partialIpMatch := NewAndAclMatchRule(
			AllIpMatch,
			NewAclRuleKv("ip4.dst", "==", "$test.allow.as", ""),
			NewAclRuleKv("ip4.dst", "!=", "$test.except.as", ""),
		)

		rule, err = partialIpMatch.Rule()
		require.NoError(t, err)
		require.Equal(t, "inport == @ovn.sg.test_sg && ip && ip4.dst == $test.allow.as && ip4.dst != $test.except.as", rule)

		/* match all tcp traffic */
		allTcpMatch := NewAndAclMatchRule(
			partialIpMatch,
			NewAclRuleKv("tcp", "", "", ""),
		)

		rule, err = allTcpMatch.Rule()
		require.NoError(t, err)
		require.Equal(t, "inport == @ovn.sg.test_sg && ip && ip4.dst == $test.allow.as && ip4.dst != $test.except.as && tcp", rule)

		/* match one tcp port traffic */
		oneTcpMatch := NewAndAclMatchRule(
			partialIpMatch,
			NewAclRuleKv("tcp.dst", "==", "12345", ""),
		)

		rule, err = oneTcpMatch.Rule()
		require.NoError(t, err)
		require.Equal(t, "inport == @ovn.sg.test_sg && ip && ip4.dst == $test.allow.as && ip4.dst != $test.except.as && tcp.dst == 12345", rule)

		/* match several tcp port traffic */
		rangeTcpMatch := NewAndAclMatchRule(
			partialIpMatch,
			NewAclRuleKv("tcp.dst", "<=", "12345", "12500"),
		)

		rule, err = rangeTcpMatch.Rule()
		require.NoError(t, err)
		require.Equal(t, "inport == @ovn.sg.test_sg && ip && ip4.dst == $test.allow.as && ip4.dst != $test.except.as && 12345 <= tcp.dst <= 12500", rule)
	})

	t.Run("err occurred when key is empty", func(t *testing.T) {
		t.Parallel()

		spec := NewAndAclMatchRule(
			NewAclRuleKv("", "", "", ""),
		)

		_, err := spec.Rule()
		require.ErrorContains(t, err, "acl rule key is required")
	})

	t.Run("generate acl match rule like 'ip'", func(t *testing.T) {
		t.Parallel()

		spec := NewAndAclMatchRule(
			NewAclRuleKv("ip", "==", "", ""),
		)

		rule, err := spec.Rule()
		require.NoError(t, err)
		require.Equal(t, "ip", rule)
	})
}

func Test_String(t *testing.T) {
	t.Parallel()
	t.Run("generate acl match rule", func(t *testing.T) {
		spec := NewAndAclMatchRule(
			NewAclRuleKv("ip.dst", "==", "$test.allow.as", ""),
		)

		require.Equal(t, "ip.dst == $test.allow.as", spec.String())
	})

	t.Run("key is empty", func(t *testing.T) {
		t.Parallel()

		spec := NewAndAclMatchRule(
			NewAclRuleKv("ip.dst", "==", "$test.allow.as", ""),
			NewAclRuleKv("", "", "", ""),
		)

		require.Empty(t, spec.String())
	})

}
