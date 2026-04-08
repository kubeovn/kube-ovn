package daemon

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWaitNetworkReady_IPGatewayMismatch(t *testing.T) {
	tests := []struct {
		name    string
		ipAddr  string
		gateway string
	}{
		{
			name:    "gateway has more elements than ips",
			ipAddr:  "10.0.0.2/24",
			gateway: "10.0.0.1,fd00::1",
		},
		{
			name:    "ips has more elements than gateway",
			ipAddr:  "10.0.0.2/24,fd00::2/64",
			gateway: "10.0.0.1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := waitNetworkReady("eth0", tt.ipAddr, tt.gateway, false, false, 1, nil)
			require.Error(t, err)
			require.Contains(t, err.Error(), "mismatch")
		})
	}
}
