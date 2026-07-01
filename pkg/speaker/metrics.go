package speaker

import (
	"context"
	"strconv"
	"sync"
	"time"

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
			Help: "Local BFD session state of the peer (0=unspecified,1=up,2=down,3=admin_down,4=init).",
		},
		[]string{"node", "peer"})

	metricBFDPeerRemoteState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_speaker_bfd_peer_remote_session_state",
			Help: "Remote BFD session state of the peer (0=unspecified,1=up,2=down,3=admin_down,4=init). Not reported by GoBGP v4.6.0 (always 0); will become meaningful once upstream populates it.",
		},
		[]string{"node", "peer"})

	metricBFDPeerFailureTransitions = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kube_ovn_speaker_bfd_peer_failure_transitions",
			Help: "Cumulative number of BFD session failure transitions for the peer. Not reported by GoBGP v4.6.0 (always 0); will become meaningful once upstream populates it.",
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

// metricsCollectTimeout bounds each GoBGP gRPC query issued during metrics
// collection. The reconcile loop runs serially, so an unbounded gRPC call to a
// hung GoBGP server would block the whole loop (including route sync), not just
// metrics; the timeout isolates that failure to the metrics path.
const metricsCollectTimeout = 3 * time.Second

// bgpMessageTypes enumerates the "type" label values produced by
// bgpMessageCounts. It is used to delete stale per-peer message series.
var bgpMessageTypes = []string{
	"open", "update", "keepalive", "notification", "refresh",
	"withdraw_update", "withdraw_prefix", "discarded", "total",
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

// deleteStaleBGPPeerSeries removes the per-peer BGP series for peers that were
// exported in the previous collection cycle (last) but are no longer reported
// in the current one (current), or whose ASN changed. A changed ASN keeps the
// peer address but yields a new peer_asn label set, so the previous asn-labeled
// series must be dropped to avoid a stale series lingering forever.
//
// With the current speaker the neighbor set is fixed at startup (no runtime
// DeletePeer), so this is normally a no-op; it only deletes when a peer stops
// appearing in ListPeer, e.g. a future runtime neighbor reconfiguration, an ASN
// change, or a cycle where the peer was skipped because Conf/State was nil.
func deleteStaleBGPPeerSeries(node string, last, current map[string]string) {
	for addr, asn := range last {
		// Skip only when the peer is still present with the same ASN.
		if newASN, ok := current[addr]; ok && newASN == asn {
			continue
		}
		metricBGPPeerUp.DeleteLabelValues(node, addr, asn)
		metricBGPPeerState.DeleteLabelValues(node, addr, asn)
		metricBGPPeerFlapCount.DeleteLabelValues(node, addr, asn)
		metricBGPPeerOutQueue.DeleteLabelValues(node, addr, asn)
		for _, typ := range bgpMessageTypes {
			metricBGPPeerReceivedMessages.DeleteLabelValues(node, addr, typ)
			metricBGPPeerSentMessages.DeleteLabelValues(node, addr, typ)
		}
	}
}

// deleteStaleBFDPeerSeries removes the per-peer BFD series for peers that were
// exported in the previous cycle (last) but are no longer reported (current).
// Like the BGP variant this is normally a no-op because BFD peers are created
// once at startup and never removed at runtime; it only deletes when a peer
// stops appearing in ListBfdPeer (future reconfiguration, or a nil-state skip).
func deleteStaleBFDPeerSeries(node string, last, current map[string]struct{}) {
	for addr := range last {
		if _, ok := current[addr]; ok {
			continue
		}
		metricBFDPeerUp.DeleteLabelValues(node, addr)
		metricBFDPeerState.DeleteLabelValues(node, addr)
		metricBFDPeerRemoteState.DeleteLabelValues(node, addr)
		metricBFDPeerFailureTransitions.DeleteLabelValues(node, addr)
		metricBFDPeerTransmittedPackets.DeleteLabelValues(node, addr)
		metricBFDPeerReceivedPackets.DeleteLabelValues(node, addr)
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
// message counters as Prometheus metrics. Series belonging to peers that have
// disappeared since the previous cycle are deleted individually so that a
// concurrent scrape never observes an empty/partial set of series.
func (c *Controller) collectBGPMetrics() {
	node := c.config.NodeName

	currentPeers := make(map[string]string)
	ctx, cancel := context.WithTimeout(context.Background(), metricsCollectTimeout)
	defer cancel()

	if err := c.config.BgpServer.ListPeer(ctx, &api.ListPeerRequest{}, func(peer *api.Peer) {
		if peer == nil || peer.Conf == nil || peer.State == nil {
			return
		}

		addr := peer.Conf.NeighborAddress
		asn := strconv.FormatUint(uint64(peer.Conf.PeerAsn), 10)
		state := peer.State.SessionState
		currentPeers[addr] = asn

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
		// Keep the previous series on error so a transient gRPC failure does not
		// blank out the metrics; stale peers are reconciled on the next success.
		klog.Errorf("failed to list BGP peers for metrics: %v", err)
		return
	}

	deleteStaleBGPPeerSeries(node, c.lastBGPPeers, currentPeers)
	c.lastBGPPeers = currentPeers
}

// collectBFDMetrics exports the BFD server statistics and per-peer session
// state as Prometheus metrics. Disappeared peers are deleted individually to
// avoid metric flapping during scrapes.
func (c *Controller) collectBFDMetrics() {
	node := c.config.NodeName

	if stats := c.config.BgpServer.GetBfdServerStats(); stats != nil {
		metricBFDServerReceivedPackets.WithLabelValues(node).Set(float64(stats.ReceivedPacket))
		metricBFDServerReceivedDrop.WithLabelValues(node).Set(float64(stats.ReceivedDrop))
		metricBFDServerReceivedError.WithLabelValues(node).Set(float64(stats.ReceivedError))
		metricBFDServerInvalidPacket.WithLabelValues(node).Set(float64(stats.InvalidPacket))
		metricBFDServerUnknownPeer.WithLabelValues(node).Set(float64(stats.UnknownPeer))
	}

	currentPeers := make(map[string]struct{})
	ctx, cancel := context.WithTimeout(context.Background(), metricsCollectTimeout)
	defer cancel()

	c.config.BgpServer.ListBfdPeer(ctx, func(addr string, state *api.BfdPeerState) {
		if state == nil {
			return
		}
		currentPeers[addr] = struct{}{}

		metricBFDPeerUp.WithLabelValues(node, addr).Set(bfdPeerUpValue(state.SessionState))
		metricBFDPeerState.WithLabelValues(node, addr).Set(float64(state.SessionState))
		// GoBGP v4.6.0's getPeerStateList only populates SessionState and BfdAsync,
		// so RemoteSessionState and FailureTransitions are always 0 here. The series
		// are still exported (with a note in their Help text) so they start reporting
		// real values automatically once upstream fills these fields.
		metricBFDPeerRemoteState.WithLabelValues(node, addr).Set(float64(state.RemoteSessionState))
		metricBFDPeerFailureTransitions.WithLabelValues(node, addr).Set(float64(state.FailureTransitions))

		if async := state.BfdAsync; async != nil {
			metricBFDPeerTransmittedPackets.WithLabelValues(node, addr).Set(float64(async.TransmittedPackets))
			metricBFDPeerReceivedPackets.WithLabelValues(node, addr).Set(float64(async.ReceivedPackets))
		}
	})

	deleteStaleBFDPeerSeries(node, c.lastBFDPeers, currentPeers)
	c.lastBFDPeers = currentPeers
}
