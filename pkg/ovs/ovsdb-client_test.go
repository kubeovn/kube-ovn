package ovs

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestOvsdbServerAddress(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     intstr.IntOrString
		envValue string
		expected string
	}{
		{
			name:     "tcp scheme",
			host:     "localhost",
			port:     intstr.FromInt32(6641),
			envValue: "false",
			expected: "tcp:localhost:6641",
		},
		{
			name:     "ssl scheme",
			host:     "127.0.0.1",
			port:     intstr.FromInt(6642),
			envValue: "true",
			expected: "ssl:127.0.0.1:6642",
		},
		{
			name:     "tcp scheme with ipv6 address",
			host:     "::1",
			port:     intstr.FromInt(6643),
			envValue: "false",
			expected: "tcp:[::1]:6643",
		},
		{
			name:     "ssl scheme with ipv6 address",
			host:     "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
			port:     intstr.FromInt(6644),
			envValue: "true",
			expected: "ssl:[2001:0db8:85a3:0000:0000:8a2e:0370:7334]:6644",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ENABLE_SSL", tt.envValue)
			result := OvsdbServerAddress(tt.host, tt.port)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}
