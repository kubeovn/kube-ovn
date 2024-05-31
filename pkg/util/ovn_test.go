package util

import (
	"testing"
)

func TestNodeLspName(t *testing.T) {
	tests := []struct {
		name     string
		nodeName string
		expected string
	}{
		{
			name:     "base case",
			nodeName: "node1",
			expected: "node-node1",
		},
		{
			name:     "empty node name",
			nodeName: "",
			expected: "node-",
		},
		{
			name:     "long node name",
			nodeName: "this-is-a-very-long-node-name",
			expected: "node-this-is-a-very-long-node-name",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := NodeLspName(test.nodeName)
			if result != test.expected {
				t.Errorf("got %s, but expected %s", result, test.expected)
			}
		})
	}
}
