package speaker

import (
	"testing"

	"github.com/osrg/gobgp/v4/api"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// countSeries returns the number of series currently exported by a collector.
// It mirrors what a Prometheus scrape would observe without pulling in the
// testutil package, which would add a new module dependency.
func countSeries(c prometheus.Collector) int {
	ch := make(chan prometheus.Metric)
	go func() {
		c.Collect(ch)
		close(ch)
	}()
	n := 0
	for range ch {
		n++
	}
	return n
}

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

// resetBGPPeerMetricVecs clears every per-peer BGP metric vector so a test starts
// from a clean slate regardless of other tests mutating the package globals.
func resetBGPPeerMetricVecs() {
	metricBGPPeerUp.Reset()
	metricBGPPeerState.Reset()
	metricBGPPeerFlapCount.Reset()
	metricBGPPeerOutQueue.Reset()
	metricBGPPeerReceivedMessages.Reset()
	metricBGPPeerSentMessages.Reset()
}

// setBGPPeerSeries populates a full per-peer BGP series set for (node, addr, asn),
// mirroring what collectBGPMetrics writes for a single peer.
func setBGPPeerSeries(node, addr, asn string) {
	metricBGPPeerUp.WithLabelValues(node, addr, asn).Set(1)
	metricBGPPeerState.WithLabelValues(node, addr, asn).Set(6)
	metricBGPPeerFlapCount.WithLabelValues(node, addr, asn).Set(0)
	metricBGPPeerOutQueue.WithLabelValues(node, addr, asn).Set(0)
	for _, typ := range bgpMessageTypes {
		metricBGPPeerReceivedMessages.WithLabelValues(node, addr, typ).Set(1)
		metricBGPPeerSentMessages.WithLabelValues(node, addr, typ).Set(1)
	}
}

func TestDeleteStaleBGPPeerSeries(t *testing.T) {
	const node = "node1"

	t.Run("present peer with same ASN is retained", func(t *testing.T) {
		resetBGPPeerMetricVecs()
		setBGPPeerSeries(node, "10.0.0.1", "65001")

		deleteStaleBGPPeerSeries(node,
			map[string]string{"10.0.0.1": "65001"},
			map[string]string{"10.0.0.1": "65001"})

		require.Equal(t, 1, countSeries(metricBGPPeerUp))
		require.Equal(t, len(bgpMessageTypes), countSeries(metricBGPPeerReceivedMessages))
		require.Equal(t, len(bgpMessageTypes), countSeries(metricBGPPeerSentMessages))
	})

	t.Run("disappeared peer has all series deleted", func(t *testing.T) {
		resetBGPPeerMetricVecs()
		setBGPPeerSeries(node, "10.0.0.1", "65001")

		deleteStaleBGPPeerSeries(node,
			map[string]string{"10.0.0.1": "65001"},
			map[string]string{})

		require.Zero(t, countSeries(metricBGPPeerUp))
		require.Zero(t, countSeries(metricBGPPeerState))
		require.Zero(t, countSeries(metricBGPPeerFlapCount))
		require.Zero(t, countSeries(metricBGPPeerOutQueue))
		require.Zero(t, countSeries(metricBGPPeerReceivedMessages))
		require.Zero(t, countSeries(metricBGPPeerSentMessages))
	})

	t.Run("changed ASN drops the old series and keeps the new one", func(t *testing.T) {
		resetBGPPeerMetricVecs()
		// Old asn series remain from the previous cycle; the new asn series was
		// already written by the collection callback for the current cycle.
		setBGPPeerSeries(node, "10.0.0.1", "65001")
		setBGPPeerSeries(node, "10.0.0.1", "65002")
		require.Equal(t, 2, countSeries(metricBGPPeerUp))

		deleteStaleBGPPeerSeries(node,
			map[string]string{"10.0.0.1": "65001"},
			map[string]string{"10.0.0.1": "65002"})

		// Exactly one series must remain, and it must be the new-ASN one. Using
		// DeleteLabelValues as an assertion: it returns true only if the series
		// existed, so the old-ASN lookup must be false and the new-ASN true.
		require.Equal(t, 1, countSeries(metricBGPPeerUp))
		require.False(t, metricBGPPeerUp.DeleteLabelValues(node, "10.0.0.1", "65001"))
		require.True(t, metricBGPPeerUp.DeleteLabelValues(node, "10.0.0.1", "65002"))
	})

	resetBGPPeerMetricVecs()
}

func TestDeleteStaleBFDPeerSeries(t *testing.T) {
	const node = "node1"

	resetBFD := func() {
		metricBFDPeerUp.Reset()
		metricBFDPeerState.Reset()
		metricBFDPeerRemoteState.Reset()
		metricBFDPeerFailureTransitions.Reset()
		metricBFDPeerTransmittedPackets.Reset()
		metricBFDPeerReceivedPackets.Reset()
	}
	setBFD := func(addr string) {
		metricBFDPeerUp.WithLabelValues(node, addr).Set(1)
		metricBFDPeerState.WithLabelValues(node, addr).Set(1)
		metricBFDPeerRemoteState.WithLabelValues(node, addr).Set(1)
		metricBFDPeerFailureTransitions.WithLabelValues(node, addr).Set(0)
		metricBFDPeerTransmittedPackets.WithLabelValues(node, addr).Set(1)
		metricBFDPeerReceivedPackets.WithLabelValues(node, addr).Set(1)
	}

	t.Run("present peer is retained", func(t *testing.T) {
		resetBFD()
		setBFD("10.0.0.1")

		deleteStaleBFDPeerSeries(node,
			map[string]struct{}{"10.0.0.1": {}},
			map[string]struct{}{"10.0.0.1": {}})

		require.Equal(t, 1, countSeries(metricBFDPeerUp))
	})

	t.Run("disappeared peer has all series deleted", func(t *testing.T) {
		resetBFD()
		setBFD("10.0.0.1")

		deleteStaleBFDPeerSeries(node,
			map[string]struct{}{"10.0.0.1": {}},
			map[string]struct{}{})

		require.Zero(t, countSeries(metricBFDPeerUp))
		require.Zero(t, countSeries(metricBFDPeerState))
		require.Zero(t, countSeries(metricBFDPeerRemoteState))
		require.Zero(t, countSeries(metricBFDPeerFailureTransitions))
		require.Zero(t, countSeries(metricBFDPeerTransmittedPackets))
		require.Zero(t, countSeries(metricBFDPeerReceivedPackets))
	})

	resetBFD()
}
