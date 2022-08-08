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
