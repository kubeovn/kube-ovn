package speaker

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCheckGracefulRestartOptions(t *testing.T) {
	tests := []struct {
		name         string
		restartTime  time.Duration
		deferralTime time.Duration
		expectError  string
	}{
		{
			name:         "valid lower boundary values",
			restartTime:  time.Second,
			deferralTime: time.Second,
		},
		{
			name:         "valid upper boundary values",
			restartTime:  4095 * time.Second,
			deferralTime: 18 * time.Hour,
		},
		{
			name:         "restart time below one second",
			restartTime:  time.Second - time.Nanosecond,
			deferralTime: time.Minute,
			expectError:  "GracefulRestartTime",
		},
		{
			name:         "zero restart time",
			restartTime:  0,
			deferralTime: time.Minute,
			expectError:  "GracefulRestartTime",
		},
		{
			name:         "restart time above maximum",
			restartTime:  4095*time.Second + time.Nanosecond,
			deferralTime: time.Minute,
			expectError:  "GracefulRestartTime",
		},
		{
			name:         "deferral time below one second",
			restartTime:  time.Minute,
			deferralTime: time.Second - time.Nanosecond,
			expectError:  "GracefulRestartDeferralTime",
		},
		{
			name:         "zero deferral time",
			restartTime:  time.Minute,
			deferralTime: 0,
			expectError:  "GracefulRestartDeferralTime",
		},
		{
			name:         "deferral time above maximum",
			restartTime:  time.Minute,
			deferralTime: 18*time.Hour + time.Nanosecond,
			expectError:  "GracefulRestartDeferralTime",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Configuration{
				GracefulRestartTime:         tt.restartTime,
				GracefulRestartDeferralTime: tt.deferralTime,
			}
			err := config.checkGracefulRestartOptions()
			if tt.expectError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.expectError)
		})
	}
}

func TestGetNeighborLocalAddress(t *testing.T) {
	const neighbor = "192.0.2.1"
	localAddress := net.ParseIP("192.0.2.10")

	config := &Configuration{
		NeighborLocalAddresses: map[string]net.IP{neighbor: localAddress},
	}
	require.True(t, localAddress.Equal(config.getNeighborLocalAddress(net.ParseIP(neighbor))))

	config = &Configuration{
		AllowedSourceAddresses: []net.IP{localAddress},
		NeighborLocalAddresses: map[string]net.IP{},
	}
	require.PanicsWithValue(t,
		"invariant violated: failed to determine local address for BGP neighbor 192.0.2.1: no allowed source address matched the whitelist",
		func() { config.getNeighborLocalAddress(net.ParseIP(neighbor)) },
	)
}
