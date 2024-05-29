package util

import (
	"testing"
)

func TestIsOvnProvider(t *testing.T) {
	testCases := []struct {
		name     string
		provider string
		expected bool
	}{
		{
			name:     "empty provider",
			provider: "",
			expected: true,
		},
		{
			name:     "ovn provider",
			provider: OvnProvider,
			expected: true,
		},
		{
			name:     "ovn provider with namespace",
			provider: "namespace.cluster.ovn",
			expected: true,
		},
		{
			name:     "non ovn provider",
			provider: "other-provider",
			expected: false,
		},
		{
			name:     "invalid provider format",
			provider: "invalid.format",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := IsOvnProvider(tc.provider)
			if result != tc.expected {
				t.Errorf("Expected %v, but got %v", tc.expected, result)
			}
		})
	}
}
