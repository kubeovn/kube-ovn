package speaker

import (
	"math"
	"net"
	"testing"

	"github.com/osrg/gobgp/v4/api"
	"github.com/stretchr/testify/require"
)

func TestNewBFDPeerConfig(t *testing.T) {
	tests := []struct {
		name     string
		config   *Configuration
		expected *api.BfdPeerConfig
	}{
		{
			name: "BFD disabled returns nil",
			config: &Configuration{
				EnableBFD: boolPtr(false),
			},
			expected: nil,
		},
		{
			name: "default values converts ms to us",
			config: &Configuration{
				EnableBFD:              boolPtr(true),
				BFDMinTX:               1000,
				BFDMinRX:               1000,
				BFDDetectionMultiplier: 3,
			},
			expected: &api.BfdPeerConfig{
				Enabled:                  true,
				DesiredMinimumTxInterval: 1000000, // 1000ms = 1,000,000μs
				RequiredMinimumReceive:   1000000,
				DetectionMultiplier:      3,
			},
		},
		{
			name: "custom values converts ms to us",
			config: &Configuration{
				EnableBFD:              boolPtr(true),
				BFDMinTX:               300,
				BFDMinRX:               500,
				BFDDetectionMultiplier: 5,
			},
			expected: &api.BfdPeerConfig{
				Enabled:                  true,
				DesiredMinimumTxInterval: 300000, // 300ms = 300,000μs
				RequiredMinimumReceive:   500000,
				DetectionMultiplier:      5,
			},
		},
		{
			name: "aggressive timers",
			config: &Configuration{
				EnableBFD:              boolPtr(true),
				BFDMinTX:               100,
				BFDMinRX:               100,
				BFDDetectionMultiplier: 3,
			},
			expected: &api.BfdPeerConfig{
				Enabled:                  true,
				DesiredMinimumTxInterval: 100000, // 100ms = 100,000μs
				RequiredMinimumReceive:   100000,
				DetectionMultiplier:      3,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newBFDPeerConfig(tt.config)
			if tt.expected == nil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, tt.expected.Enabled, got.Enabled)
			require.Equal(t, tt.expected.DesiredMinimumTxInterval, got.DesiredMinimumTxInterval)
			require.Equal(t, tt.expected.RequiredMinimumReceive, got.RequiredMinimumReceive)
			require.Equal(t, tt.expected.DetectionMultiplier, got.DetectionMultiplier)
		})
	}
}

func TestBfdSessionStateString(t *testing.T) {
	tests := []struct {
		state    api.BfdSessionState
		expected string
	}{
		{api.BfdSessionState_BFD_SESSION_STATE_UP, "UP"},
		{api.BfdSessionState_BFD_SESSION_STATE_DOWN, "DOWN"},
		{api.BfdSessionState_BFD_SESSION_STATE_INIT, "INIT"},
		{api.BfdSessionState_BFD_SESSION_STATE_ADMIN_DOWN, "ADMIN_DOWN"},
		{api.BfdSessionState_BFD_SESSION_STATE_UNSPECIFIED, "UNKNOWN"},
		{api.BfdSessionState(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			require.Equal(t, tt.expected, bfdSessionStateString(tt.state))
		})
	}
}

func TestValidateBFDFlags(t *testing.T) {
	tests := []struct {
		name    string
		config  *Configuration
		wantErr bool
	}{
		{
			name: "valid BFD config",
			config: &Configuration{
				EnableBFD:              boolPtr(true),
				BFDMinTX:               1000,
				BFDMinRX:               1000,
				BFDDetectionMultiplier: 3,
				NeighborAs:             65001,
				ClusterAs:              65000,
				NodeName:               "test-node",
			},
			wantErr: false,
		},
		{
			// Upper bound (255) is enforced by the uint8 type, so no >255 test case is needed.
			name: "detection multiplier zero",
			config: &Configuration{
				EnableBFD:              boolPtr(true),
				BFDMinTX:               1000,
				BFDMinRX:               1000,
				BFDDetectionMultiplier: 0,
				NeighborAs:             65001,
				ClusterAs:              65000,
				NodeName:               "test-node",
			},
			wantErr: true,
		},
		{
			name: "zero min-tx",
			config: &Configuration{
				EnableBFD:              boolPtr(true),
				BFDMinTX:               0,
				BFDMinRX:               1000,
				BFDDetectionMultiplier: 3,
				NeighborAs:             65001,
				ClusterAs:              65000,
				NodeName:               "test-node",
			},
			wantErr: true,
		},
		{
			name: "zero min-rx",
			config: &Configuration{
				EnableBFD:              boolPtr(true),
				BFDMinTX:               1000,
				BFDMinRX:               0,
				BFDDetectionMultiplier: 3,
				NeighborAs:             65001,
				ClusterAs:              65000,
				NodeName:               "test-node",
			},
			wantErr: true,
		},
		{
			name: "BFD disabled skips validation",
			config: &Configuration{
				EnableBFD:              boolPtr(false),
				BFDDetectionMultiplier: 255,
				NeighborAs:             65001,
				ClusterAs:              65000,
				NodeName:               "test-node",
			},
			wantErr: false,
		},
		{
			name: "min-tx at overflow boundary is valid",
			config: &Configuration{
				EnableBFD:              boolPtr(true),
				BFDMinTX:               math.MaxUint32 / 1000,
				BFDMinRX:               1000,
				BFDDetectionMultiplier: 3,
				NeighborAs:             65001,
				ClusterAs:              65000,
				NodeName:               "test-node",
			},
			wantErr: false,
		},
		{
			name: "min-tx exceeds overflow boundary",
			config: &Configuration{
				EnableBFD:              boolPtr(true),
				BFDMinTX:               math.MaxUint32/1000 + 1,
				BFDMinRX:               1000,
				BFDDetectionMultiplier: 3,
				NeighborAs:             65001,
				ClusterAs:              65000,
				NodeName:               "test-node",
			},
			wantErr: true,
		},
		{
			name: "min-rx exceeds overflow boundary",
			config: &Configuration{
				EnableBFD:              boolPtr(true),
				BFDMinTX:               1000,
				BFDMinRX:               math.MaxUint32/1000 + 1,
				BFDDetectionMultiplier: 3,
				NeighborAs:             65001,
				ClusterAs:              65000,
				NodeName:               "test-node",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Need at least one neighbor for validation to pass
			tt.config.NeighborAddresses = []IP{{IP: net.ParseIP("10.0.0.1")}}
			err := tt.config.validateRequiredFlags()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
