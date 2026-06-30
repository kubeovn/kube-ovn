package speaker

import (
	"testing"

	"github.com/osrg/gobgp/v4/api"
	"github.com/stretchr/testify/require"
)

func TestBGPPeerUpValue(t *testing.T) {
	tests := []struct {
		name     string
		state    api.PeerState_SessionState
		expected float64
	}{
		{"established is up", api.PeerState_SESSION_STATE_ESTABLISHED, 1},
		{"idle is down", api.PeerState_SESSION_STATE_IDLE, 0},
		{"active is down", api.PeerState_SESSION_STATE_ACTIVE, 0},
		{"opensent is down", api.PeerState_SESSION_STATE_OPENSENT, 0},
		{"unspecified is down", api.PeerState_SESSION_STATE_UNSPECIFIED, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, bgpPeerUpValue(tt.state))
		})
	}
}

func TestBFDPeerUpValue(t *testing.T) {
	tests := []struct {
		name     string
		state    api.BfdSessionState
		expected float64
	}{
		{"up", api.BfdSessionState_BFD_SESSION_STATE_UP, 1},
		{"down", api.BfdSessionState_BFD_SESSION_STATE_DOWN, 0},
		{"init", api.BfdSessionState_BFD_SESSION_STATE_INIT, 0},
		{"admin_down", api.BfdSessionState_BFD_SESSION_STATE_ADMIN_DOWN, 0},
		{"unspecified", api.BfdSessionState_BFD_SESSION_STATE_UNSPECIFIED, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, bfdPeerUpValue(tt.state))
		})
	}
}

func TestBGPMessageCounts(t *testing.T) {
	t.Run("nil message yields no entries", func(t *testing.T) {
		require.Nil(t, bgpMessageCounts(nil))
	})

	t.Run("flattens all counter fields", func(t *testing.T) {
		msg := &api.Message{
			Open:           1,
			Update:         2,
			Keepalive:      3,
			Notification:   4,
			Refresh:        5,
			WithdrawUpdate: 6,
			WithdrawPrefix: 7,
			Discarded:      8,
			Total:          9,
		}
		got := bgpMessageCounts(msg)
		require.Equal(t, map[string]float64{
			"open":            1,
			"update":          2,
			"keepalive":       3,
			"notification":    4,
			"refresh":         5,
			"withdraw_update": 6,
			"withdraw_prefix": 7,
			"discarded":       8,
			"total":           9,
		}, got)
	})
}

func TestRegisterSpeakerMetricsIdempotent(t *testing.T) {
	// registerSpeakerMetrics must be safe to call multiple times without panicking
	// on duplicate registration.
	require.NotPanics(t, func() {
		registerSpeakerMetrics()
		registerSpeakerMetrics()
	})
}
