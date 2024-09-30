package util

import (
	"net"
	"testing"
)

func TestUint32ToIPv4(t *testing.T) {
	tests := []struct {
		input    uint32
		expected string
	}{
		{0x7F000001, "127.0.0.1"},
		{0xC0A80001, "192.168.0.1"},
		{0x00000000, "0.0.0.0"},
		{0xFFFFFFFF, "255.255.255.255"},
	}

	for _, tt := range tests {
		result := Uint32ToIPv4(tt.input)
		if result != tt.expected {
			t.Errorf("Uint32ToIPv4(%d) = %s; want %s", tt.input, result, tt.expected)
		}
	}
}

func TestIPv4ToUint32(t *testing.T) {
	tests := []struct {
		input    string
		expected uint32
	}{
		{"127.0.0.1", 0x7F000001},
		{"192.168.0.1", 0xC0A80001},
		{"0.0.0.0", 0x00000000},
		{"255.255.255.255", 0xFFFFFFFF},
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.input).To4()
		if ip == nil {
			t.Errorf("Invalid IP address: %s", tt.input)
			continue
		}
		result := IPv4ToUint32(ip)
		if result != tt.expected {
			t.Errorf("IPv4ToUint32(%s) = %d; want %d", tt.input, result, tt.expected)
		}
	}
}

func TestUint32ToIPv6(t *testing.T) {
	tests := []struct {
		input    [4]uint32
		expected string
	}{
		{[4]uint32{0x20010DB8, 0x00000000, 0x00000000, 0x00000001}, "2001:0db8:0000:0000:0000:0000:0000:0001"},
		{[4]uint32{0xFE800000, 0x00000000, 0x00000000, 0x00000001}, "fe80:0000:0000:0000:0000:0000:0000:0001"},
	}

	for _, tt := range tests {
		result := Uint32ToIPv6(tt.input)
		if result != tt.expected {
			t.Errorf("Uint32ToIPv6(%v) = %s; want %s", tt.input, result, tt.expected)
		}
	}
}
