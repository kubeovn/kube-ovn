package util

import (
	"net"
	"testing"
)

func TestMacEqual(t *testing.T) {
	tests := []struct {
		name     string
		mac1     net.HardwareAddr
		mac2     net.HardwareAddr
		expected bool
	}{
		{
			name:     "equal",
			mac1:     net.HardwareAddr{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
			mac2:     net.HardwareAddr{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
			expected: true,
		},
		{
			name:     "not_equal",
			mac1:     net.HardwareAddr{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
			mac2:     net.HardwareAddr{0x01, 0x02, 0x03, 0x04, 0x05, 0x07},
			expected: false,
		},
		{
			name:     "different_lengths",
			mac1:     net.HardwareAddr{0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
			mac2:     net.HardwareAddr{0x01, 0x02, 0x03, 0x04, 0x05},
			expected: false,
		},
		{
			name:     "empty_macs",
			mac1:     nil,
			mac2:     nil,
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := macEqual(test.mac1, test.mac2)
			if result != test.expected {
				t.Errorf("Expected %v, got %v", test.expected, result)
			}
		})
	}
}
