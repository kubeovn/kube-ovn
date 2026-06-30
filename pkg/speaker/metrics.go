package speaker

import (
	"context"
	"strconv"
	"sync"

	"github.com/osrg/gobgp/v4/api"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Prometheus metrics for the BGP speaker and its optional BFD sessions.
//
// Counters reported by GoBGP (BGP message counters, BFD packet/error counters)
// are cumulative monotonic values that reset only when the underlying GoBGP
// server restarts. They are exposed as gauges holding the latest cumulative
// snapshot so that PromQL rate()/increase() can derive per-interval deltas,
// which is the standard way to alert on such counters (mirrors Cilium's
// cilium_bgp_control_plane_* gauge design).
var (
	// BGP per-peer metrics.
	metricBGPPeerUp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_speaker_bgp_peer_up",
			Help: "Whether the BGP session with the peer is established (1) or not (0).",
		},
		[]string{"node", "peer", "peer_asn"})

	metricBGPPeerState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_speaker_bgp_peer_session_state",
			Help: "BGP session FSM state of the peer (0=unspecified,1=idle,2=connect,3=active,4=opensent,5=openconfirm,6=established).",
		},
		[]string{"node", "peer", "peer_asn"})

	metricBGPPeerFlapCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_speaker_bgp_peer_flap_count",
			Help: "Number of times the BGP session with the peer has flapped.",
		},
		[]string{"node", "peer", "peer_asn"})

	metricBGPPeerOutQueue = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_speaker_bgp_peer_out_queue",
			Help: "Number of BGP messages queued to be sent to the peer.",
		},
		[]string{"node", "peer", "peer_asn"})

	metricBGPPeerReceivedMessages = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_speaker_bgp_peer_received_messages",
			Help: "Cumulative number of BGP messages received from the peer, by message type.",
		},
		[]string{"node", "peer", "type"})

	metricBGPPeerSentMessages = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_speaker_bgp_peer_sent_messages",
			Help: "Cumulative number of BGP messages sent to the peer, by message type.",
		},
		[]string{"node", "peer", "type"})

	// BFD server-wide metrics.
	metricBFDServerReceivedPackets = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_speaker_bfd_server_received_packets",
			Help: "Cumulative number of BFD packets received by the server.",
		},
		[]string{"node"})

	metricBFDServerReceivedDrop = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_speaker_bfd_server_received_drop",
			Help: "Cumulative number of BFD packets dropped by the server.",
		},
		[]string{"node"})

	metricBFDServerReceivedError = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_speaker_bfd_server_received_error",
			Help: "Cumulative number of BFD packets received with errors by the server.",
		},
		[]string{"node"})

	metricBFDServerInvalidPacket = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_speaker_bfd_server_invalid_packet",
			Help: "Cumulative number of invalid BFD packets received by the server.",
		},
		[]string{"node"})

	metricBFDServerUnknownPeer = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_speaker_bfd_server_unknown_peer",
			Help: "Cumulative number of BFD packets received from unknown peers.",
		},
		[]string{"node"})

	// BFD per-peer metrics.
	metricBFDPeerUp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_speaker_bfd_peer_up",
			Help: "Whether the BFD session with the peer is UP (1) or not (0).",
		},
		[]string{"node", "peer"})

	metricBFDPeerState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_speaker_bfd_peer_session_state",
			Help: "Local BFD session state of the peer (0=unspecified,1=admin_down,2=down,3=init,4=up).",
		},
		[]string{"node", "peer"})

	metricBFDPeerRemoteState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_speaker_bfd_peer_remote_session_state",
			Help: "Remote BFD session state of the peer (0=unspecified,1=admin_down,2=down,3=init,4=up).",
		},
		[]string{"node", "peer"})

	metricBFDPeerFailureTransitions = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_speaker_bfd_peer_failure_transitions",
			Help: "Cumulative number of BFD session failure transitions for the peer.",
		},
		[]string{"node", "peer"})

	metricBFDPeerTransmittedPackets = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_speaker_bfd_peer_transmitted_packets",
			Help: "Cumulative number of BFD control packets transmitted to the peer.",
		},
		[]string{"node", "peer"})

	metricBFDPeerReceivedPackets = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_speaker_bfd_peer_received_packets",
			Help: "Cumulative number of BFD control packets received from the peer.",
		},
		[]string{"node", "peer"})
)

var registerSpeakerMetricsOnce sync.Once

// registerSpeakerMetrics registers all BGP/BFD metrics with the controller-runtime
// registry. It is safe to call multiple times; registration happens only once.
func registerSpeakerMetrics() {
	registerSpeakerMetricsOnce.Do(func() {
		metrics.Registry.MustRegister(
			metricBGPPeerUp,
			metricBGPPeerState,
			metricBGPPeerFlapCount,
			metricBGPPeerOutQueue,
			metricBGPPeerReceivedMessages,
			metricBGPPeerSentMessages,
			metricBFDServerReceivedPackets,
			metricBFDServerReceivedDrop,
			metricBFDServerReceivedError,
			metricBFDServerInvalidPacket,
			metricBFDServerUnknownPeer,
			metricBFDPeerUp,
			metricBFDPeerState,
			metricBFDPeerRemoteState,
			metricBFDPeerFailureTransitions,
			metricBFDPeerTransmittedPackets,
			metricBFDPeerReceivedPackets,
		)
	})
}

// resetBGPPeerMetrics clears all per-peer BGP series so that peers which no
// longer exist do not leave stale time series behind. Called before each
// collection cycle, then the vectors are repopulated with the current peers.
func resetBGPPeerMetrics() {
	metricBGPPeerUp.Reset()
	metricBGPPeerState.Reset()
	metricBGPPeerFlapCount.Reset()
	metricBGPPeerOutQueue.Reset()
	metricBGPPeerReceivedMessages.Reset()
	metricBGPPeerSentMessages.Reset()
}

// resetBFDPeerMetrics clears all per-peer BFD series to avoid stale time series.
func resetBFDPeerMetrics() {
	metricBFDPeerUp.Reset()
	metricBFDPeerState.Reset()
	metricBFDPeerRemoteState.Reset()
	metricBFDPeerFailureTransitions.Reset()
	metricBFDPeerTransmittedPackets.Reset()
	metricBFDPeerReceivedPackets.Reset()
}

// bgpPeerUpValue returns 1 when the BGP session is established, otherwise 0.
func bgpPeerUpValue(state api.PeerState_SessionState) float64 {
	if state == api.PeerState_SESSION_STATE_ESTABLISHED {
		return 1
	}
	return 0
}

// bfdPeerUpValue returns 1 when the BFD session is UP, otherwise 0.
func bfdPeerUpValue(state api.BfdSessionState) float64 {
	if state == api.BfdSessionState_BFD_SESSION_STATE_UP {
		return 1
	}
	return 0
}

// bgpMessageCounts flattens a BGP *Message counter set into (type, value) pairs.
// A nil message yields no entries.
func bgpMessageCounts(m *api.Message) map[string]float64 {
	if m == nil {
		return nil
	}
	return map[string]float64{
		"open":            float64(m.Open),
		"update":          float64(m.Update),
		"keepalive":       float64(m.Keepalive),
		"notification":    float64(m.Notification),
		"refresh":         float64(m.Refresh),
		"withdraw_update": float64(m.WithdrawUpdate),
		"withdraw_prefix": float64(m.WithdrawPrefix),
		"discarded":       float64(m.Discarded),
		"total":           float64(m.Total),
	}
}

// collectMetrics refreshes all BGP/BFD Prometheus metrics from the GoBGP server.
// It is invoked from the reconcile loop and only performs gRPC queries when
// metrics are enabled.
func (c *Controller) collectMetrics() {
	c.collectBGPMetrics()
	if c.config.EnableBFD {
		c.collectBFDMetrics()
	}
}

// collectBGPMetrics lists all BGP peers and exports their session state and
// message counters as Prometheus metrics.
func (c *Controller) collectBGPMetrics() {
	node := c.config.NodeName

	resetBGPPeerMetrics()
	if err := c.config.BgpServer.ListPeer(context.Background(), &api.ListPeerRequest{}, func(peer *api.Peer) {
		if peer == nil || peer.Conf == nil || peer.State == nil {
			return
		}

		addr := peer.Conf.NeighborAddress
		asn := strconv.FormatUint(uint64(peer.Conf.PeerAsn), 10)
		state := peer.State.SessionState

		metricBGPPeerUp.WithLabelValues(node, addr, asn).Set(bgpPeerUpValue(state))
		metricBGPPeerState.WithLabelValues(node, addr, asn).Set(float64(state))
		metricBGPPeerFlapCount.WithLabelValues(node, addr, asn).Set(float64(peer.State.Flops))
		metricBGPPeerOutQueue.WithLabelValues(node, addr, asn).Set(float64(peer.State.OutQ))

		if msgs := peer.State.Messages; msgs != nil {
			for typ, val := range bgpMessageCounts(msgs.Received) {
				metricBGPPeerReceivedMessages.WithLabelValues(node, addr, typ).Set(val)
			}
			for typ, val := range bgpMessageCounts(msgs.Sent) {
				metricBGPPeerSentMessages.WithLabelValues(node, addr, typ).Set(val)
			}
		}
	}); err != nil {
		klog.Errorf("failed to list BGP peers for metrics: %v", err)
	}
}

// collectBFDMetrics exports the BFD server statistics and per-peer session
// state as Prometheus metrics.
func (c *Controller) collectBFDMetrics() {
	node := c.config.NodeName

	if stats := c.config.BgpServer.GetBfdServerStats(); stats != nil {
		metricBFDServerReceivedPackets.WithLabelValues(node).Set(float64(stats.ReceivedPacket))
		metricBFDServerReceivedDrop.WithLabelValues(node).Set(float64(stats.ReceivedDrop))
		metricBFDServerReceivedError.WithLabelValues(node).Set(float64(stats.ReceivedError))
		metricBFDServerInvalidPacket.WithLabelValues(node).Set(float64(stats.InvalidPacket))
		metricBFDServerUnknownPeer.WithLabelValues(node).Set(float64(stats.UnknownPeer))
	}

	resetBFDPeerMetrics()
	c.config.BgpServer.ListBfdPeer(context.Background(), func(addr string, state *api.BfdPeerState) {
		if state == nil {
			return
		}

		metricBFDPeerUp.WithLabelValues(node, addr).Set(bfdPeerUpValue(state.SessionState))
		metricBFDPeerState.WithLabelValues(node, addr).Set(float64(state.SessionState))
		metricBFDPeerRemoteState.WithLabelValues(node, addr).Set(float64(state.RemoteSessionState))
		metricBFDPeerFailureTransitions.WithLabelValues(node, addr).Set(float64(state.FailureTransitions))

		if async := state.BfdAsync; async != nil {
			metricBFDPeerTransmittedPackets.WithLabelValues(node, addr).Set(float64(async.TransmittedPackets))
			metricBFDPeerReceivedPackets.WithLabelValues(node, addr).Set(float64(async.ReceivedPackets))
		}
	})
}
