package controller

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestExpandIPPoolAddresses(t *testing.T) {
	addresses, err := util.ExpandIPPoolAddresses([]string{
		"10.0.0.1",
		"2001:db8::1",
		"192.168.1.0/24",
		"10.0.0.1", // duplicate should be removed
		" 2001:db8::1 ",
	})
	require.NoError(t, err)
	require.Equal(t, []string{
		"10.0.0.1/32",
		"192.168.1.0/24",
		"2001:db8::1/128",
	}, addresses)
}

func TestExpandIPPoolAddressesRange(t *testing.T) {
	addresses, err := util.ExpandIPPoolAddresses([]string{"10.0.0.0..10.0.0.3"})
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.0/30"}, addresses)

	addresses, err = util.ExpandIPPoolAddresses([]string{"10.0.0.1..10.0.0.5"})
	require.NoError(t, err)
	require.Equal(t, []string{
		"10.0.0.1/32",
		"10.0.0.2/31",
		"10.0.0.4/31",
	}, addresses)

	addresses, err = util.ExpandIPPoolAddresses([]string{"2001:db8::1..2001:db8::4"})
	require.NoError(t, err)
	require.Equal(t, []string{
		"2001:db8::1/128",
		"2001:db8::2/127",
		"2001:db8::4/128",
	}, addresses)
}

func TestExpandIPPoolAddressesInvalid(t *testing.T) {
	_, err := util.ExpandIPPoolAddresses([]string{"10.0.0.1..2001:db8::1"})
	require.Error(t, err)

	_, err = util.ExpandIPPoolAddresses([]string{"foo"})
	require.Error(t, err)
}

func TestIPPoolAddressSetName(t *testing.T) {
	require.Equal(t, "foo.bar", util.IPPoolAddressSetName("foo-bar"))
	require.Equal(t, "123pool", util.IPPoolAddressSetName("123pool"))
}

// Additional comprehensive tests for IPPool utilities

func TestExpandIPPoolAddressesEdgeCases(t *testing.T) {
	t.Run("Empty and whitespace entries", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddresses([]string{
			"",
			"   ",
			"\t",
			"10.0.0.1",
		})
		require.NoError(t, err)
		require.Len(t, addresses, 1)
		require.Equal(t, "10.0.0.1/32", addresses[0])
	})

	t.Run("CIDR normalization", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddresses([]string{
			"192.168.1.5/24", // non-canonical, should normalize to 192.168.1.0/24
		})
		require.NoError(t, err)
		require.Equal(t, []string{"192.168.1.0/24"}, addresses)
	})

	t.Run("Large range", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddresses([]string{
			"10.0.0.0..10.0.0.255",
		})
		require.NoError(t, err)
		require.Equal(t, []string{"10.0.0.0/24"}, addresses)
	})

	t.Run("Complex mixed input", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddresses([]string{
			"10.0.0.1",
			"10.0.0.5..10.0.0.10",
			"192.168.1.0/24",
			"2001:db8::1",
			"2001:db8::100..2001:db8::103",
		})
		require.NoError(t, err)
		require.Greater(t, len(addresses), 5) // Should have multiple CIDRs
		require.Contains(t, addresses, "10.0.0.1/32")
		require.Contains(t, addresses, "192.168.1.0/24")
		require.Contains(t, addresses, "2001:db8::1/128")
	})
}

func TestExpandIPPoolAddressesRangeEdgeCases(t *testing.T) {
	t.Run("Unaligned IPv4 range", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddresses([]string{"10.0.0.1..10.0.0.5"})
		require.NoError(t, err)
		require.Equal(t, []string{
			"10.0.0.1/32",
			"10.0.0.2/31",
			"10.0.0.4/31",
		}, addresses)
	})

	t.Run("Power of 2 range", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddresses([]string{"10.0.0.0..10.0.0.7"})
		require.NoError(t, err)
		require.Equal(t, []string{"10.0.0.0/29"}, addresses)
	})

	t.Run("Single IP range", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddresses([]string{"10.0.0.1..10.0.0.1"})
		require.NoError(t, err)
		require.Equal(t, []string{"10.0.0.1/32"}, addresses)
	})

	t.Run("IPv6 unaligned range", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddresses([]string{"2001:db8::1..2001:db8::4"})
		require.NoError(t, err)
		require.Equal(t, []string{
			"2001:db8::1/128",
			"2001:db8::2/127",
			"2001:db8::4/128",
		}, addresses)
	})
}

func TestExpandIPPoolAddressesErrorConditions(t *testing.T) {
	t.Run("Completely invalid input", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddresses([]string{"totally-invalid"})
		require.Error(t, err)
	})

	t.Run("Invalid range - missing end", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddresses([]string{"10.0.0.1.."})
		require.Error(t, err)
	})

	t.Run("Invalid range - missing start", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddresses([]string{"..10.0.0.1"})
		require.Error(t, err)
	})

	t.Run("Invalid CIDR - out of range prefix", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddresses([]string{"10.0.0.0/33"})
		require.Error(t, err)
	})

	t.Run("Invalid CIDR - IPv6 out of range", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddresses([]string{"2001:db8::/129"})
		require.Error(t, err)
	})

	t.Run("Mixed IP families in range", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddresses([]string{"10.0.0.1..2001:db8::1"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "mixes IPv4 and IPv6")
	})

	t.Run("Range with end less than start", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddresses([]string{"10.0.0.100..10.0.0.1"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "start is greater than end")
	})
}

func TestCanonicalizeIPPoolEntries(t *testing.T) {
	t.Run("Basic functionality", func(t *testing.T) {
		result, err := util.CanonicalizeIPPoolEntries([]string{
			"10.0.0.1",
			"192.168.1.0/24",
		})
		require.NoError(t, err)
		require.True(t, result["10.0.0.1/32"])
		require.True(t, result["192.168.1.0/24"])
		require.False(t, result["10.0.0.2/32"])
	})

	t.Run("Empty input", func(t *testing.T) {
		result, err := util.CanonicalizeIPPoolEntries([]string{})
		require.NoError(t, err)
		require.Empty(t, result)
	})

	t.Run("Duplicate detection", func(t *testing.T) {
		result, err := util.CanonicalizeIPPoolEntries([]string{
			"10.0.0.1",
			"10.0.0.1",
		})
		require.NoError(t, err)
		require.Len(t, result, 1)
		require.True(t, result["10.0.0.1/32"])
	})
}

func TestNormalizeAddressSetEntries(t *testing.T) {
	t.Run("Standard OVN format", func(t *testing.T) {
		result := util.NormalizeAddressSetEntries(`"10.0.0.1/32" "10.0.0.2/32"`)
		require.Len(t, result, 2)
		require.True(t, result["10.0.0.1/32"])
		require.True(t, result["10.0.0.2/32"])
	})

	t.Run("Extra whitespace", func(t *testing.T) {
		result := util.NormalizeAddressSetEntries(`  "10.0.0.1/32"   "10.0.0.2/32"  `)
		require.Len(t, result, 2)
		require.True(t, result["10.0.0.1/32"])
	})

	t.Run("Empty input", func(t *testing.T) {
		result := util.NormalizeAddressSetEntries("")
		require.Empty(t, result)
	})

	t.Run("No quotes", func(t *testing.T) {
		result := util.NormalizeAddressSetEntries("10.0.0.1/32 10.0.0.2/32")
		require.Len(t, result, 2)
		require.True(t, result["10.0.0.1/32"])
		require.True(t, result["10.0.0.2/32"])
	})

	t.Run("Mixed formats", func(t *testing.T) {
		result := util.NormalizeAddressSetEntries(`"192.168.1.0/24" "2001:db8::1/128"`)
		require.Len(t, result, 2)
		require.True(t, result["192.168.1.0/24"])
		require.True(t, result["2001:db8::1/128"])
	})
}

func TestNormalizeIP(t *testing.T) {
	t.Run("Standard IPv4", func(t *testing.T) {
		ip, err := util.NormalizeIP("192.168.1.1")
		require.NoError(t, err)
		require.Equal(t, "192.168.1.1", ip.String())
		require.NotNil(t, ip.To4())
	})

	t.Run("Standard IPv6", func(t *testing.T) {
		ip, err := util.NormalizeIP("2001:db8::1")
		require.NoError(t, err)
		require.Equal(t, "2001:db8::1", ip.String())
		require.Nil(t, ip.To4())
	})

	t.Run("IPv6 full form", func(t *testing.T) {
		ip, err := util.NormalizeIP("2001:0db8:0000:0000:0000:0000:0000:0001")
		require.NoError(t, err)
		require.Equal(t, "2001:db8::1", ip.String())
	})

	t.Run("Whitespace handling", func(t *testing.T) {
		ip, err := util.NormalizeIP("  10.0.0.1  ")
		require.NoError(t, err)
		require.Equal(t, "10.0.0.1", ip.String())
	})

	t.Run("Invalid IP", func(t *testing.T) {
		_, err := util.NormalizeIP("999.999.999.999")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid IP address")
	})

	t.Run("Not an IP", func(t *testing.T) {
		_, err := util.NormalizeIP("hostname")
		require.Error(t, err)
	})

	t.Run("IPv4 loopback", func(t *testing.T) {
		ip, err := util.NormalizeIP("127.0.0.1")
		require.NoError(t, err)
		require.Equal(t, "127.0.0.1", ip.String())
	})

	t.Run("IPv6 loopback", func(t *testing.T) {
		ip, err := util.NormalizeIP("::1")
		require.NoError(t, err)
		require.Equal(t, "::1", ip.String())
	})
}

func TestExpandIPPoolAddressesForOVNIntegration(t *testing.T) {
	// These tests use ExpandIPPoolAddressesForOVN which enforces OVN address set limitation
	t.Run("Mixed IPv4 and IPv6 - should fail", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddressesForOVN([]string{
			"10.0.0.1",
			"2001:db8::1",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "mixed IPv4 and IPv6 addresses are not supported")
	})

	t.Run("Pure IPv4 pool", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{
			"192.168.1.0/30",
			"10.0.0.1..10.0.0.5",
		})
		require.NoError(t, err)
		require.NotEmpty(t, addresses)
		// Verify all are IPv4
		for _, addr := range addresses {
			require.NotContains(t, addr, ":")
		}
		// CIDR should be preserved
		require.Contains(t, addresses, "192.168.1.0/30")
	})

	t.Run("Pure IPv6 pool", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{
			"2001:db8::/126",
			"fd00::1..fd00::3",
		})
		require.NoError(t, err)
		require.NotEmpty(t, addresses)
		// Verify all are IPv6
		for _, addr := range addresses {
			require.Contains(t, addr, ":")
		}
		// CIDR should be preserved
		require.Contains(t, addresses, "2001:db8::/126")
	})

	t.Run("Single IPs are simplified", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{
			"10.0.0.1",
			"10.0.0.2",
		})
		require.NoError(t, err)
		require.Len(t, addresses, 2)
		// Should not have /32 suffix
		require.Contains(t, addresses, "10.0.0.1")
		require.Contains(t, addresses, "10.0.0.2")
		require.NotContains(t, addresses, "10.0.0.1/32")
		require.NotContains(t, addresses, "10.0.0.2/32")
	})

	t.Run("Mixed in range notation", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddressesForOVN([]string{
			"10.0.0.1..10.0.0.5",
			"fd00::1",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "mixed IPv4 and IPv6")
	})

	t.Run("Mixed in CIDR notation", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddressesForOVN([]string{
			"192.168.1.0/24",
			"2001:db8::/64",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "mixed IPv4 and IPv6")
	})

	t.Run("Range expansion with simplification", func(t *testing.T) {
		// Range that expands to /32 should be simplified
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{
			"10.0.0.1..10.0.0.1", // Single IP range
		})
		require.NoError(t, err)
		require.Len(t, addresses, 1)
		require.Equal(t, "10.0.0.1", addresses[0])
		require.NotContains(t, addresses[0], "/32")
	})

	t.Run("Range with multiple CIDRs - some simplified", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{
			"10.0.0.1..10.0.0.5",
		})
		require.NoError(t, err)
		require.NotEmpty(t, addresses)
		// Should contain simplified /32 and non-simplified /31
		require.Contains(t, addresses, "10.0.0.1")    // Simplified from /32
		require.Contains(t, addresses, "10.0.0.2/31") // Not /32, keep as-is
		require.Contains(t, addresses, "10.0.0.4/31") // Not /32, keep as-is
	})

	t.Run("Empty input", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{})
		require.NoError(t, err)
		require.Empty(t, addresses)
	})

	t.Run("Only whitespace entries", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{"", "  ", "\t"})
		require.NoError(t, err)
		require.Empty(t, addresses)
	})

	t.Run("IPv6 single IP simplified", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{"2001:db8::1"})
		require.NoError(t, err)
		require.Len(t, addresses, 1)
		require.Equal(t, "2001:db8::1", addresses[0])
		require.NotContains(t, addresses[0], "/128")
	})

	t.Run("IPv6 range with /128 simplified", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{"fd00::1..fd00::1"})
		require.NoError(t, err)
		require.Len(t, addresses, 1)
		require.Equal(t, "fd00::1", addresses[0])
	})

	t.Run("Duplicate IPs deduplicated and simplified", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{
			"10.0.0.1",
			"10.0.0.1",
			" 10.0.0.1 ",
		})
		require.NoError(t, err)
		require.Len(t, addresses, 1)
		require.Equal(t, "10.0.0.1", addresses[0])
	})

	t.Run("CIDR normalization preserved", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{"192.168.1.5/24"})
		require.NoError(t, err)
		require.Len(t, addresses, 1)
		require.Equal(t, "192.168.1.0/24", addresses[0])
	})
}

func TestExpandIPPoolAddressesGeneralUse(t *testing.T) {
	// ExpandIPPoolAddresses (without ForOVN suffix) allows mixed IP families
	t.Run("Mixed IPv4 and IPv6 - allowed for general use", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddresses([]string{
			"10.0.0.1",
			"2001:db8::1",
		})
		require.NoError(t, err)
		require.NotEmpty(t, addresses)
		require.Contains(t, addresses, "10.0.0.1/32")
		require.Contains(t, addresses, "2001:db8::1/128")
	})
}
