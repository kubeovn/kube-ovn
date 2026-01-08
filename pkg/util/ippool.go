package util

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"net"
	"sort"
	"strings"
)

// ExpandIPPoolAddresses expands a list of pool entries (IPs, ranges, CIDRs) into canonical CIDR strings without duplicates.
// This function provides the same parsing logic as ipam.NewIPRangeListFrom but returns CIDR strings suitable for OVN address sets.
//
// IMPORTANT: This function does NOT merge overlapping IP ranges. Each input entry is processed independently.
// For example, ["10.0.0.1..10.0.0.5", "10.0.0.3..10.0.0.10"] will generate CIDRs covering both ranges
// without merging them first, which may result in overlapping CIDRs in the output.
//
// Alternative: ipam.NewIPRangeListFrom(...).ToCIDRs() merges overlapping ranges before converting to CIDRs,
// producing a more compact result. However, it cannot be used here due to circular dependency (ipam -> util).
func ExpandIPPoolAddresses(entries []string) ([]string, error) {
	return expandIPPoolAddressesInternal(entries, false)
}

// ExpandIPPoolAddressesForOVN expands IP pool entries for OVN address sets.
// OVN Limitation: OVN address sets only support either IPv4 or IPv6, not both.
// This function will return an error if the input contains mixed IP families.
// For simplicity, single IP addresses are returned without /32 or /128 suffix.
func ExpandIPPoolAddressesForOVN(entries []string) ([]string, error) {
	addresses, err := expandIPPoolAddressesInternal(entries, true)
	if err != nil {
		return nil, err
	}

	// Simplify single IPs by removing /32 and /128 suffixes
	for i, addr := range addresses {
		addresses[i] = simplifyOVNAddress(addr)
	}
	return addresses, nil
}

func expandIPPoolAddressesInternal(entries []string, checkMixedIPFamily bool) ([]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{})
	hasIPv4 := false
	hasIPv6 := false

	addUnique := func(cidr string) {
		if _, exists := seen[cidr]; !exists {
			seen[cidr] = struct{}{}
			// Detect IP family if check is enabled
			if checkMixedIPFamily {
				if strings.Contains(cidr, ":") {
					hasIPv6 = true
				} else {
					hasIPv4 = true
				}
			}
		}
	}

	for _, raw := range entries {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}

		switch {
		case strings.Contains(value, ".."):
			cidrs, err := expandIPRange(value)
			if err != nil {
				return nil, err
			}
			for _, cidr := range cidrs {
				addUnique(cidr)
			}
		case strings.Contains(value, "/"):
			cidr, err := normalizeCIDR(value)
			if err != nil {
				return nil, err
			}
			addUnique(cidr)
		default:
			cidr, err := ipToCIDR(value)
			if err != nil {
				return nil, err
			}
			addUnique(cidr)
		}
	}

	// Check for mixed IP families if enabled (OVN address set limitation)
	if checkMixedIPFamily && hasIPv4 && hasIPv6 {
		return nil, errors.New("mixed IPv4 and IPv6 addresses are not supported in OVN address set")
	}

	// Convert set to sorted slice
	addresses := make([]string, 0, len(seen))
	for cidr := range seen {
		addresses = append(addresses, cidr)
	}
	sort.Strings(addresses)
	return addresses, nil
}

// expandIPRange expands an IP range (e.g., "10.0.0.1..10.0.0.10") into CIDRs.
func expandIPRange(value string) ([]string, error) {
	parts := strings.Split(value, "..")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid IP range %q", value)
	}

	start, err := NormalizeIP(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid range start %q: %w", parts[0], err)
	}
	end, err := NormalizeIP(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid range end %q: %w", parts[1], err)
	}
	if (start.To4() != nil) != (end.To4() != nil) {
		return nil, fmt.Errorf("range %q mixes IPv4 and IPv6 addresses", value)
	}
	if compareIP(start, end) > 0 {
		return nil, fmt.Errorf("range %q start is greater than end", value)
	}

	cidrs, err := IPRangeToCIDRs(start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to convert IP range %q to CIDRs: %w", value, err)
	}
	return cidrs, nil
}

// normalizeCIDR normalizes a CIDR string to canonical form.
func normalizeCIDR(value string) (string, error) {
	_, network, err := net.ParseCIDR(value)
	if err != nil {
		return "", fmt.Errorf("invalid CIDR %q: %w", value, err)
	}
	return network.String(), nil
}

// ipToCIDR converts a single IP to CIDR notation (/32 for IPv4, /128 for IPv6).
func ipToCIDR(value string) (string, error) {
	ip, err := NormalizeIP(value)
	if err != nil {
		return "", fmt.Errorf("invalid IP address %q: %w", value, err)
	}
	bits := 32
	if ip.To4() == nil {
		bits = 128
	}
	return fmt.Sprintf("%s/%d", ip.String(), bits), nil
}

// simplifyOVNAddress removes /32 and /128 suffixes for single IP addresses in OVN address sets.
// OVN accepts both "10.0.0.1" and "10.0.0.1/32", but the former is simpler.
func simplifyOVNAddress(cidr string) string {
	if strings.HasSuffix(cidr, "/32") {
		return strings.TrimSuffix(cidr, "/32")
	}
	if strings.HasSuffix(cidr, "/128") {
		return strings.TrimSuffix(cidr, "/128")
	}
	return cidr
}

// CanonicalizeIPPoolEntries returns a set of canonical pool entries for comparison purposes.
func CanonicalizeIPPoolEntries(entries []string) (map[string]bool, error) {
	expanded, err := ExpandIPPoolAddresses(entries)
	if err != nil {
		return nil, err
	}

	set := make(map[string]bool, len(expanded))
	for _, token := range expanded {
		set[token] = true
	}
	return set, nil
}

// NormalizeAddressSetEntries normalizes an OVN address set string list into a lookup map.
func NormalizeAddressSetEntries(raw string) map[string]bool {
	clean := strings.ReplaceAll(raw, "\"", "")
	tokens := strings.Fields(strings.TrimSpace(clean))
	set := make(map[string]bool, len(tokens))
	for _, token := range tokens {
		set[token] = true
	}
	return set
}

// IPPoolAddressSetName converts an IPPool name into the OVN address set name.
func IPPoolAddressSetName(name string) string {
	return strings.ReplaceAll(name, "-", ".")
}

// NormalizeIP parses an IP string and returns the canonical IP.
func NormalizeIP(value string) (net.IP, error) {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address %q", value)
	}
	if v4 := ip.To4(); v4 != nil {
		return v4, nil
	}
	return ip.To16(), nil
}

// IPRangeToCIDRs converts an IP range into the minimal set of covering CIDRs.
func IPRangeToCIDRs(start, end net.IP) ([]string, error) {
	length := net.IPv4len
	totalBits := 32
	if start.To4() == nil {
		length = net.IPv6len
		totalBits = 128
	}

	startInt := ipToBigInt(start)
	endInt := ipToBigInt(end)
	if startInt.Cmp(endInt) > 0 {
		return nil, fmt.Errorf("range %s..%s start is greater than end", start, end)
	}

	result := make([]string, 0)
	tmp := new(big.Int)
	for startInt.Cmp(endInt) <= 0 {
		zeros := countTrailingZeros(startInt, totalBits)
		if zeros > totalBits {
			return nil, fmt.Errorf("trailing zero count %d exceeds total bits %d", zeros, totalBits)
		}

		diff := tmp.Sub(endInt, startInt)
		diff.Add(diff, big.NewInt(1))

		var maxDiff int
		if bits := diff.BitLen(); bits > 0 {
			maxDiff = bits - 1
		}

		size := min(zeros, maxDiff)
		size = min(size, totalBits)
		if size < 0 {
			return nil, fmt.Errorf("calculated negative prefix size %d", size)
		}

		prefix := totalBits - size
		networkInt := new(big.Int).Set(startInt)
		networkIP := bigIntToIP(networkInt, length)
		network := &net.IPNet{IP: networkIP, Mask: net.CIDRMask(prefix, totalBits)}
		result = append(result, network.String())

		increment := new(big.Int).Lsh(big.NewInt(1), uint(size))
		startInt.Add(startInt, increment)
	}

	return result, nil
}

func ipToBigInt(ip net.IP) *big.Int {
	return new(big.Int).SetBytes(ip)
}

func bigIntToIP(value *big.Int, length int) net.IP {
	bytes := value.Bytes()
	if len(bytes) < length {
		padded := make([]byte, length)
		copy(padded[length-len(bytes):], bytes)
		bytes = padded
	} else if len(bytes) > length {
		bytes = bytes[len(bytes)-length:]
	}

	ip := make(net.IP, length)
	copy(ip, bytes)
	if length == net.IPv4len {
		return ip.To4()
	}
	return ip
}

func countTrailingZeros(value *big.Int, totalBits int) int {
	if value.Sign() == 0 {
		return totalBits
	}

	zeros := 0
	for zeros < totalBits && value.Bit(zeros) == 0 {
		zeros++
	}
	return zeros
}

func compareIP(a, b net.IP) int {
	return bytes.Compare(a, b)
}
