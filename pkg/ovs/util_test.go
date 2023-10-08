package ovs

import (
	"context"
	"testing"
	"time"

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
}
