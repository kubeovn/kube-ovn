package util

import (
	"testing"
)

func TestGenNatGwStsName(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Test case 1",
			input:    "test-nat-gw",
			expected: "vpc-nat-gw-test-nat-gw",
		},
		{
			name:     "Test case 2",
			input:    "my-nat-gateway",
			expected: "vpc-nat-gw-my-nat-gateway",
		},
		{
			name:     "Test case 3",
			input:    "",
			expected: "vpc-nat-gw-",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := GenNatGwStsName(tc.input)
			if result != tc.expected {
				t.Errorf("Expected %s, but got %s", tc.expected, result)
			}
		})
	}
}

func TestGenNatGwPodName(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Test case 1",
			input:    "test-nat-gw",
			expected: "vpc-nat-gw-test-nat-gw-0",
		},
		{
			name:     "Test case 2",
			input:    "my-nat-gateway",
			expected: "vpc-nat-gw-my-nat-gateway-0",
		},
		{
			name:     "Test case 3",
			input:    "",
			expected: "vpc-nat-gw--0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := GenNatGwPodName(tc.input)
			if result != tc.expected {
				t.Errorf("Expected %s, but got %s", tc.expected, result)
			}
		})
	}
}
