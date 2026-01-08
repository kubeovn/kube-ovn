package util

import (
	"math/big"
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExpandIPPoolAddresses(t *testing.T) {
	t.Run("Empty input", func(t *testing.T) {
		result, err := ExpandIPPoolAddresses(nil)
		require.NoError(t, err)
		require.Nil(t, result)

		result, err = ExpandIPPoolAddresses([]string{})
		require.NoError(t, err)
		require.Nil(t, result)
	})

	t.Run("Single IPv4", func(t *testing.T) {
		result, err := ExpandIPPoolAddresses([]string{"10.0.0.1"})
		require.NoError(t, err)
		require.Equal(t, []string{"10.0.0.1/32"}, result)
	})

	t.Run("Single IPv6", func(t *testing.T) {
		result, err := ExpandIPPoolAddresses([]string{"2001:db8::1"})
		require.NoError(t, err)
		require.Equal(t, []string{"2001:db8::1/128"}, result)
	})

	t.Run("IPv4 CIDR", func(t *testing.T) {
		result, err := ExpandIPPoolAddresses([]string{"192.168.1.0/24"})
		require.NoError(t, err)
		require.Equal(t, []string{"192.168.1.0/24"}, result)
	})

	t.Run("IPv6 CIDR", func(t *testing.T) {
		result, err := ExpandIPPoolAddresses([]string{"2001:db8::/64"})
		require.NoError(t, err)
		require.Equal(t, []string{"2001:db8::/64"}, result)
	})

	t.Run("IPv4 Range", func(t *testing.T) {
		result, err := ExpandIPPoolAddresses([]string{"10.0.0.0..10.0.0.3"})
		require.NoError(t, err)
		require.Equal(t, []string{"10.0.0.0/30"}, result)
	})

	t.Run("IPv6 Range", func(t *testing.T) {
		result, err := ExpandIPPoolAddresses([]string{"2001:db8::1..2001:db8::4"})
		require.NoError(t, err)
		require.Equal(t, []string{
			"2001:db8::1/128",
			"2001:db8::2/127",
			"2001:db8::4/128",
		}, result)
	})

	t.Run("Mixed types", func(t *testing.T) {
		result, err := ExpandIPPoolAddresses([]string{
			"10.0.0.1",
			"2001:db8::1",
			"192.168.1.0/24",
		})
		require.NoError(t, err)
		require.Equal(t, []string{
			"10.0.0.1/32",
			"192.168.1.0/24",
			"2001:db8::1/128",
		}, result)
	})

	t.Run("Duplicates removed", func(t *testing.T) {
		result, err := ExpandIPPoolAddresses([]string{
			"10.0.0.1",
			"10.0.0.1",
			"10.0.0.2",
		})
		require.NoError(t, err)
		require.Equal(t, []string{
			"10.0.0.1/32",
			"10.0.0.2/32",
		}, result)
	})

	t.Run("Whitespace trimmed", func(t *testing.T) {
		result, err := ExpandIPPoolAddresses([]string{
			" 10.0.0.1 ",
			"\t2001:db8::1\t",
			"  192.168.1.0/24  ",
		})
		require.NoError(t, err)
		require.Equal(t, []string{
			"10.0.0.1/32",
			"192.168.1.0/24",
			"2001:db8::1/128",
		}, result)
	})

	t.Run("Empty strings skipped", func(t *testing.T) {
		result, err := ExpandIPPoolAddresses([]string{
			"10.0.0.1",
			"",
			"   ",
			"10.0.0.2",
		})
		require.NoError(t, err)
		require.Equal(t, []string{
			"10.0.0.1/32",
			"10.0.0.2/32",
		}, result)
	})

	t.Run("Overlapping ranges NOT merged", func(t *testing.T) {
		result, err := ExpandIPPoolAddresses([]string{
			"10.0.0.1..10.0.0.5",
			"10.0.0.3..10.0.0.10",
		})
		require.NoError(t, err)
		// Should have overlapping CIDRs, not merged
		require.Greater(t, len(result), 2)
		require.Contains(t, result, "10.0.0.1/32")
		require.Contains(t, result, "10.0.0.10/32")
	})
}

func TestExpandIPPoolAddressesErrors(t *testing.T) {
	t.Run("Invalid IP", func(t *testing.T) {
		_, err := ExpandIPPoolAddresses([]string{"not-an-ip"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid IP address")
	})

	t.Run("Invalid CIDR", func(t *testing.T) {
		_, err := ExpandIPPoolAddresses([]string{"10.0.0.0/33"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid CIDR")
	})

	t.Run("Invalid range format", func(t *testing.T) {
		_, err := ExpandIPPoolAddresses([]string{"10.0.0.1..10.0.0.2..10.0.0.3"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid IP range")
	})

	t.Run("Mixed IP families in range", func(t *testing.T) {
		_, err := ExpandIPPoolAddresses([]string{"10.0.0.1..2001:db8::1"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "mixes IPv4 and IPv6")
	})

	t.Run("Reversed range", func(t *testing.T) {
		_, err := ExpandIPPoolAddresses([]string{"10.0.0.10..10.0.0.1"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "start is greater than end")
	})
}

func TestExpandIPPoolAddressesForOVN(t *testing.T) {
	t.Run("Mixed IPv4 and IPv6 addresses - OVN limitation", func(t *testing.T) {
		_, err := ExpandIPPoolAddressesForOVN([]string{
			"10.0.0.1",
			"2001:db8::1",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "mixed IPv4 and IPv6 addresses are not supported")
	})

	t.Run("Mixed IP families in multiple entries", func(t *testing.T) {
		_, err := ExpandIPPoolAddressesForOVN([]string{
			"192.168.1.0/24",
			"10.0.0.1..10.0.0.10",
			"2001:db8::/64",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "mixed IPv4 and IPv6")
	})

	t.Run("Pure IPv4 only - should succeed", func(t *testing.T) {
		result, err := ExpandIPPoolAddressesForOVN([]string{
			"10.0.0.1",
			"192.168.1.0/24",
			"172.16.0.1..172.16.0.10",
		})
		require.NoError(t, err)
		require.NotEmpty(t, result)
		// Single IP should not have /32 suffix
		require.Contains(t, result, "10.0.0.1")
		require.NotContains(t, result, "10.0.0.1/32")
		// CIDR should be preserved
		require.Contains(t, result, "192.168.1.0/24")
	})

	t.Run("Pure IPv6 only - should succeed", func(t *testing.T) {
		result, err := ExpandIPPoolAddressesForOVN([]string{
			"2001:db8::1",
			"2001:db8::/64",
			"fd00::1..fd00::10",
		})
		require.NoError(t, err)
		require.NotEmpty(t, result)
		// Single IP should not have /128 suffix
		require.Contains(t, result, "2001:db8::1")
		require.NotContains(t, result, "2001:db8::1/128")
		// CIDR should be preserved
		require.Contains(t, result, "2001:db8::/64")
	})

	t.Run("Single IPs simplified", func(t *testing.T) {
		result, err := ExpandIPPoolAddressesForOVN([]string{
			"10.0.0.1",
			"10.0.0.2",
			"192.168.1.1",
		})
		require.NoError(t, err)
		require.Len(t, result, 3)
		// All should be simple IPs without /32
		require.Contains(t, result, "10.0.0.1")
		require.Contains(t, result, "10.0.0.2")
		require.Contains(t, result, "192.168.1.1")
		// None should have /32
		for _, addr := range result {
			require.NotContains(t, addr, "/32")
		}
	})

	t.Run("Mixed in single range", func(t *testing.T) {
		// This is caught earlier in expandIPRange
		_, err := ExpandIPPoolAddressesForOVN([]string{"10.0.0.1..2001:db8::1"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "mixes IPv4 and IPv6")
	})
}

func TestExpandIPPoolAddressesMixedAllowed(t *testing.T) {
	// When not using OVN address sets, mixed IP families should be allowed
	t.Run("Mixed IPv4 and IPv6 - allowed without OVN", func(t *testing.T) {
		result, err := ExpandIPPoolAddresses([]string{
			"10.0.0.1",
			"2001:db8::1",
		})
		require.NoError(t, err)
		require.NotEmpty(t, result)
		require.Contains(t, result, "10.0.0.1/32")
		require.Contains(t, result, "2001:db8::1/128")
	})

	t.Run("Mixed families in multiple entries", func(t *testing.T) {
		result, err := ExpandIPPoolAddresses([]string{
			"192.168.1.0/24",
			"10.0.0.1..10.0.0.3",
			"2001:db8::/126",
		})
		require.NoError(t, err)
		require.NotEmpty(t, result)
	})

	t.Run("Complex mixed scenario", func(t *testing.T) {
		result, err := ExpandIPPoolAddresses([]string{
			"10.0.0.1",
			"192.168.0.0/16",
			"2001:db8::1..2001:db8::10",
			"fd00::/64",
		})
		require.NoError(t, err)
		require.NotEmpty(t, result)
		// Should contain both IPv4 and IPv6
		hasIPv4 := false
		hasIPv6 := false
		for _, addr := range result {
			if strings.Contains(addr, ":") {
				hasIPv6 = true
			} else {
				hasIPv4 = true
			}
		}
		require.True(t, hasIPv4, "Should contain IPv4 addresses")
		require.True(t, hasIPv6, "Should contain IPv6 addresses")
	})
}

func TestCanonicalizeIPPoolEntries(t *testing.T) {
	t.Run("Normal case", func(t *testing.T) {
		result, err := CanonicalizeIPPoolEntries([]string{
			"10.0.0.1",
			"10.0.0.2",
			"192.168.1.0/24",
		})
		require.NoError(t, err)
		require.Len(t, result, 3)
		require.True(t, result["10.0.0.1/32"])
		require.True(t, result["10.0.0.2/32"])
		require.True(t, result["192.168.1.0/24"])
	})

	t.Run("Empty input", func(t *testing.T) {
		result, err := CanonicalizeIPPoolEntries([]string{})
		require.NoError(t, err)
		require.Empty(t, result)
	})

	t.Run("Error propagation", func(t *testing.T) {
		_, err := CanonicalizeIPPoolEntries([]string{"invalid"})
		require.Error(t, err)
	})
}

func TestNormalizeAddressSetEntries(t *testing.T) {
	t.Run("Normal case", func(t *testing.T) {
		result := NormalizeAddressSetEntries(`"10.0.0.1/32" "10.0.0.2/32" "192.168.1.0/24"`)
		require.Len(t, result, 3)
		require.True(t, result["10.0.0.1/32"])
		require.True(t, result["10.0.0.2/32"])
		require.True(t, result["192.168.1.0/24"])
	})

	t.Run("Empty string", func(t *testing.T) {
		result := NormalizeAddressSetEntries("")
		require.Empty(t, result)
	})

	t.Run("Whitespace only", func(t *testing.T) {
		result := NormalizeAddressSetEntries("   ")
		require.Empty(t, result)
	})

	t.Run("Mixed whitespace", func(t *testing.T) {
		result := NormalizeAddressSetEntries(`  "10.0.0.1/32"   "10.0.0.2/32"  `)
		require.Len(t, result, 2)
		require.True(t, result["10.0.0.1/32"])
		require.True(t, result["10.0.0.2/32"])
	})
}

func TestIPPoolAddressSetName(t *testing.T) {
	t.Run("With hyphens", func(t *testing.T) {
		require.Equal(t, "foo.bar.baz", IPPoolAddressSetName("foo-bar-baz"))
	})

	t.Run("Without hyphens", func(t *testing.T) {
		require.Equal(t, "foobar", IPPoolAddressSetName("foobar"))
	})

	t.Run("Multiple consecutive hyphens", func(t *testing.T) {
		require.Equal(t, "foo..bar", IPPoolAddressSetName("foo--bar"))
	})

	t.Run("Empty string", func(t *testing.T) {
		require.Equal(t, "", IPPoolAddressSetName(""))
	})
}

func TestNormalizeIP(t *testing.T) {
	t.Run("IPv4", func(t *testing.T) {
		ip, err := NormalizeIP("10.0.0.1")
		require.NoError(t, err)
		require.Equal(t, "10.0.0.1", ip.String())
		require.NotNil(t, ip.To4())
	})

	t.Run("IPv6", func(t *testing.T) {
		ip, err := NormalizeIP("2001:db8::1")
		require.NoError(t, err)
		require.Equal(t, "2001:db8::1", ip.String())
		require.Nil(t, ip.To4())
	})

	t.Run("IPv6 expanded form", func(t *testing.T) {
		ip, err := NormalizeIP("2001:0db8:0000:0000:0000:0000:0000:0001")
		require.NoError(t, err)
		require.Equal(t, "2001:db8::1", ip.String())
	})

	t.Run("Whitespace trimmed", func(t *testing.T) {
		ip, err := NormalizeIP("  10.0.0.1  ")
		require.NoError(t, err)
		require.Equal(t, "10.0.0.1", ip.String())
	})

	t.Run("Invalid IP", func(t *testing.T) {
		_, err := NormalizeIP("not-an-ip")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid IP address")
	})

	t.Run("Empty string", func(t *testing.T) {
		_, err := NormalizeIP("")
		require.Error(t, err)
	})
}

func TestIPRangeToCIDRs(t *testing.T) {
	t.Run("IPv4 aligned range", func(t *testing.T) {
		start := net.ParseIP("10.0.0.0")
		end := net.ParseIP("10.0.0.3")
		result, err := IPRangeToCIDRs(start, end)
		require.NoError(t, err)
		require.Equal(t, []string{"10.0.0.0/30"}, result)
	})

	t.Run("IPv4 unaligned range", func(t *testing.T) {
		start := net.ParseIP("10.0.0.1")
		end := net.ParseIP("10.0.0.5")
		result, err := IPRangeToCIDRs(start, end)
		require.NoError(t, err)
		require.Equal(t, []string{
			"10.0.0.1/32",
			"10.0.0.2/31",
			"10.0.0.4/31",
		}, result)
	})

	t.Run("IPv6 aligned range", func(t *testing.T) {
		start := net.ParseIP("2001:db8::0")
		end := net.ParseIP("2001:db8::3")
		result, err := IPRangeToCIDRs(start, end)
		require.NoError(t, err)
		require.Equal(t, []string{"2001:db8::/126"}, result)
	})

	t.Run("Single IP", func(t *testing.T) {
		start := net.ParseIP("10.0.0.1")
		end := net.ParseIP("10.0.0.1")
		result, err := IPRangeToCIDRs(start, end)
		require.NoError(t, err)
		require.Equal(t, []string{"10.0.0.1/32"}, result)
	})

	t.Run("Large IPv4 range", func(t *testing.T) {
		start := net.ParseIP("10.0.0.0")
		end := net.ParseIP("10.0.0.255")
		result, err := IPRangeToCIDRs(start, end)
		require.NoError(t, err)
		require.Equal(t, []string{"10.0.0.0/24"}, result)
	})

	t.Run("Reversed range error", func(t *testing.T) {
		start := net.ParseIP("10.0.0.10")
		end := net.ParseIP("10.0.0.1")
		_, err := IPRangeToCIDRs(start, end)
		require.Error(t, err)
		require.Contains(t, err.Error(), "start is greater than end")
	})
}

func TestExpandIPRange(t *testing.T) {
	t.Run("Valid IPv4 range", func(t *testing.T) {
		result, err := expandIPRange("10.0.0.1..10.0.0.5")
		require.NoError(t, err)
		require.NotEmpty(t, result)
	})

	t.Run("Invalid format - too many parts", func(t *testing.T) {
		_, err := expandIPRange("10.0.0.1..10.0.0.2..10.0.0.3")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid IP range")
	})

	t.Run("Invalid format - too few parts", func(t *testing.T) {
		_, err := expandIPRange("10.0.0.1")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid IP range")
	})

	t.Run("Invalid start IP", func(t *testing.T) {
		_, err := expandIPRange("invalid..10.0.0.5")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid range start")
	})

	t.Run("Invalid end IP", func(t *testing.T) {
		_, err := expandIPRange("10.0.0.1..invalid")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid range end")
	})

	t.Run("Mixed IP families", func(t *testing.T) {
		_, err := expandIPRange("10.0.0.1..2001:db8::1")
		require.Error(t, err)
		require.Contains(t, err.Error(), "mixes IPv4 and IPv6")
	})
}

func TestNormalizeCIDR(t *testing.T) {
	t.Run("Valid IPv4 CIDR", func(t *testing.T) {
		result, err := normalizeCIDR("192.168.1.0/24")
		require.NoError(t, err)
		require.Equal(t, "192.168.1.0/24", result)
	})

	t.Run("Valid IPv6 CIDR", func(t *testing.T) {
		result, err := normalizeCIDR("2001:db8::/64")
		require.NoError(t, err)
		require.Equal(t, "2001:db8::/64", result)
	})

	t.Run("Non-canonical CIDR normalized", func(t *testing.T) {
		result, err := normalizeCIDR("192.168.1.5/24")
		require.NoError(t, err)
		require.Equal(t, "192.168.1.0/24", result)
	})

	t.Run("Invalid CIDR", func(t *testing.T) {
		_, err := normalizeCIDR("10.0.0.0/33")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid CIDR")
	})

	t.Run("Not a CIDR", func(t *testing.T) {
		_, err := normalizeCIDR("10.0.0.1")
		require.Error(t, err)
	})
}

func TestIpToCIDR(t *testing.T) {
	t.Run("IPv4 to /32", func(t *testing.T) {
		result, err := ipToCIDR("10.0.0.1")
		require.NoError(t, err)
		require.Equal(t, "10.0.0.1/32", result)
	})

	t.Run("IPv6 to /128", func(t *testing.T) {
		result, err := ipToCIDR("2001:db8::1")
		require.NoError(t, err)
		require.Equal(t, "2001:db8::1/128", result)
	})

	t.Run("Invalid IP", func(t *testing.T) {
		_, err := ipToCIDR("not-an-ip")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid IP address")
	})
}

func TestBigIntConversions(t *testing.T) {
	t.Run("IPv4 round trip", func(t *testing.T) {
		original := net.ParseIP("192.168.1.100").To4()
		bigInt := ipToBigInt(original)
		result := bigIntToIP(bigInt, net.IPv4len)
		require.Equal(t, original.String(), result.String())
	})

	t.Run("IPv6 round trip", func(t *testing.T) {
		original := net.ParseIP("2001:db8::1234")
		bigInt := ipToBigInt(original)
		result := bigIntToIP(bigInt, net.IPv6len)
		require.Equal(t, original.String(), result.String())
	})

	t.Run("Zero IP", func(t *testing.T) {
		original := net.ParseIP("0.0.0.0").To4()
		bigInt := ipToBigInt(original)
		result := bigIntToIP(bigInt, net.IPv4len)
		require.Equal(t, "0.0.0.0", result.String())
	})
}

func TestCountTrailingZeros(t *testing.T) {
	t.Run("Zero value", func(t *testing.T) {
		value := big.NewInt(0)
		zeros := countTrailingZeros(value, 32)
		require.Equal(t, 32, zeros)
	})

	t.Run("Odd number", func(t *testing.T) {
		value := big.NewInt(1)
		zeros := countTrailingZeros(value, 32)
		require.Equal(t, 0, zeros)
	})

	t.Run("Even number", func(t *testing.T) {
		value := big.NewInt(8) // binary 1000
		zeros := countTrailingZeros(value, 32)
		require.Equal(t, 3, zeros)
	})

	t.Run("Large power of 2", func(t *testing.T) {
		value := big.NewInt(256) // binary 100000000
		zeros := countTrailingZeros(value, 32)
		require.Equal(t, 8, zeros)
	})
}

func TestSimplifyOVNAddress(t *testing.T) {
	t.Run("IPv4 single IP with /32", func(t *testing.T) {
		result := simplifyOVNAddress("10.0.0.1/32")
		require.Equal(t, "10.0.0.1", result)
	})

	t.Run("IPv6 single IP with /128", func(t *testing.T) {
		result := simplifyOVNAddress("2001:db8::1/128")
		require.Equal(t, "2001:db8::1", result)
	})

	t.Run("IPv4 CIDR not /32", func(t *testing.T) {
		result := simplifyOVNAddress("192.168.1.0/24")
		require.Equal(t, "192.168.1.0/24", result)
	})

	t.Run("IPv6 CIDR not /128", func(t *testing.T) {
		result := simplifyOVNAddress("2001:db8::/64")
		require.Equal(t, "2001:db8::/64", result)
	})

	t.Run("Already simplified IPv4", func(t *testing.T) {
		result := simplifyOVNAddress("10.0.0.1")
		require.Equal(t, "10.0.0.1", result)
	})

	t.Run("Already simplified IPv6", func(t *testing.T) {
		result := simplifyOVNAddress("fd00::1")
		require.Equal(t, "fd00::1", result)
	})

	t.Run("IPv4 /31", func(t *testing.T) {
		result := simplifyOVNAddress("10.0.0.0/31")
		require.Equal(t, "10.0.0.0/31", result)
	})

	t.Run("IPv6 /127", func(t *testing.T) {
		result := simplifyOVNAddress("2001:db8::/127")
		require.Equal(t, "2001:db8::/127", result)
	})

	t.Run("Edge case - /30", func(t *testing.T) {
		result := simplifyOVNAddress("10.0.0.0/30")
		require.Equal(t, "10.0.0.0/30", result)
	})

	t.Run("Edge case - /126", func(t *testing.T) {
		result := simplifyOVNAddress("2001:db8::/126")
		require.Equal(t, "2001:db8::/126", result)
	})

	t.Run("Empty string", func(t *testing.T) {
		result := simplifyOVNAddress("")
		require.Equal(t, "", result)
	})

	t.Run("No slash at all", func(t *testing.T) {
		result := simplifyOVNAddress("10.0.0.1")
		require.Equal(t, "10.0.0.1", result)
	})
}

func TestCompareIP(t *testing.T) {
	t.Run("Equal IPs", func(t *testing.T) {
		a := net.ParseIP("10.0.0.1")
		b := net.ParseIP("10.0.0.1")
		require.Equal(t, 0, compareIP(a, b))
	})

	t.Run("First IP less", func(t *testing.T) {
		a := net.ParseIP("10.0.0.1")
		b := net.ParseIP("10.0.0.2")
		require.Less(t, compareIP(a, b), 0)
	})

	t.Run("First IP greater", func(t *testing.T) {
		a := net.ParseIP("10.0.0.2")
		b := net.ParseIP("10.0.0.1")
		require.Greater(t, compareIP(a, b), 0)
	})

	t.Run("IPv6 comparison", func(t *testing.T) {
		a := net.ParseIP("2001:db8::1")
		b := net.ParseIP("2001:db8::2")
		require.Less(t, compareIP(a, b), 0)
	})
}
