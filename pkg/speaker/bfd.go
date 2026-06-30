package speaker

import (
	"context"

	"github.com/osrg/gobgp/v4/api"
	"k8s.io/klog/v2"
)

type bfdErrorCounters struct {
	receivedDrop  uint64
	receivedError uint64
	invalidPacket uint64
	unknownPeer   uint64
}

func bfdErrorCountersFromStats(stats *api.BfdState) bfdErrorCounters {
	return bfdErrorCounters{
		receivedDrop:  stats.ReceivedDrop,
		receivedError: stats.ReceivedError,
		invalidPacket: stats.InvalidPacket,
		unknownPeer:   stats.UnknownPeer,
	}
}

func (curr bfdErrorCounters) hasIncreaseSince(prev bfdErrorCounters) bool {
	return curr.receivedDrop > prev.receivedDrop ||
		curr.receivedError > prev.receivedError ||
		curr.invalidPacket > prev.invalidPacket ||
		curr.unknownPeer > prev.unknownPeer
}

func (curr bfdErrorCounters) isResetComparedTo(prev bfdErrorCounters) bool {
	return curr.receivedDrop < prev.receivedDrop ||
		curr.receivedError < prev.receivedError ||
		curr.invalidPacket < prev.invalidPacket ||
		curr.unknownPeer < prev.unknownPeer
}

// sub returns the per-counter increase of curr over prev.
// Callers must ensure curr >= prev (no counter reset) to avoid underflow.
func (curr bfdErrorCounters) sub(prev bfdErrorCounters) bfdErrorCounters {
	return bfdErrorCounters{
		receivedDrop:  curr.receivedDrop - prev.receivedDrop,
		receivedError: curr.receivedError - prev.receivedError,
		invalidPacket: curr.invalidPacket - prev.invalidPacket,
		unknownPeer:   curr.unknownPeer - prev.unknownPeer,
	}
}

// bfdSessionStateString converts a BFD session state enum to a human-readable string.
func bfdSessionStateString(state api.BfdSessionState) string {
	switch state {
	case api.BfdSessionState_BFD_SESSION_STATE_UP:
		return "UP"
	case api.BfdSessionState_BFD_SESSION_STATE_DOWN:
		return "DOWN"
	case api.BfdSessionState_BFD_SESSION_STATE_INIT:
		return "INIT"
	case api.BfdSessionState_BFD_SESSION_STATE_ADMIN_DOWN:
		return "ADMIN_DOWN"
	default:
		return "UNKNOWN"
	}
}

// logBFDStatus logs the BFD status for all peers.
// Error/recovery stats are always checked; per-peer state dump is gated by V(3).
func (c *Controller) logBFDStatus() {
	if !c.config.EnableBFD {
		return
	}

	if stats := c.config.BgpServer.GetBfdServerStats(); stats != nil {
		curr := bfdErrorCountersFromStats(stats)

		hasNewErrors := false
		if c.hasLastBFDStatsSample {
			if curr.isResetComparedTo(c.lastBFDErrorCounters) {
				// Counter reset/rollover: re-baseline and avoid false positives.
				hasNewErrors = false
			} else {
				hasNewErrors = curr.hasIncreaseSince(c.lastBFDErrorCounters)
			}
		}

		if hasNewErrors {
			delta := curr.sub(c.lastBFDErrorCounters)
			klog.Warningf("BFD server stats: new errors since last check: dropped=+%d, errors=+%d, invalid=+%d, unknownPeer=+%d (cumulative: received=%d, dropped=%d, errors=%d, invalid=%d, unknownPeer=%d)",
				delta.receivedDrop, delta.receivedError, delta.invalidPacket, delta.unknownPeer,
				stats.ReceivedPacket, stats.ReceivedDrop, stats.ReceivedError, stats.InvalidPacket, stats.UnknownPeer)
		} else if c.lastBFDStatsHasErrors {
			klog.Infof("BFD server stats recovered: received=%d, dropped=%d, errors=%d, invalid=%d, unknownPeer=%d",
				stats.ReceivedPacket, stats.ReceivedDrop, stats.ReceivedError, stats.InvalidPacket, stats.UnknownPeer)
		}

		c.lastBFDErrorCounters = curr
		c.hasLastBFDStatsSample = true
		c.lastBFDStatsHasErrors = hasNewErrors
	}

	// Guard per-peer BFD state dump with V(3) to skip gRPC calls when log level is insufficient.
	if !klog.V(3).Enabled() {
		return
	}

	if c.lastBFDPeerStates == nil {
		c.lastBFDPeerStates = make(map[string]string)
	}

	c.config.BgpServer.ListBfdPeer(context.Background(), func(addr string, state *api.BfdPeerState) {
		if state == nil {
			klog.Warningf("BFD peer %s: no state available", addr)
			return
		}

		current := bfdSessionStateString(state.SessionState)
		if prev, ok := c.lastBFDPeerStates[addr]; ok && prev == current {
			return
		}
		c.lastBFDPeerStates[addr] = current

		if async := state.BfdAsync; async != nil {
			klog.Infof("BFD peer %s: state=%s, tx=%d, rx=%d",
				addr, current, async.TransmittedPackets, async.ReceivedPackets)
		} else {
			klog.Infof("BFD peer %s: state=%s", addr, current)
		}
	})
}

// newBFDPeerConfig creates a BfdPeerConfig from the speaker Configuration.
// Returns nil when BFD is disabled.
// GoBGP expects BFD intervals in microseconds (RFC 5880), while CLI flags
// accept milliseconds for user convenience; this function performs the conversion.
func newBFDPeerConfig(config *Configuration) *api.BfdPeerConfig {
	if !config.EnableBFD {
		return nil
	}
	return &api.BfdPeerConfig{
		Enabled:                  true,
		DesiredMinimumTxInterval: config.BFDMinTX * 1000, // ms → μs
		RequiredMinimumReceive:   config.BFDMinRX * 1000, // ms → μs
		DetectionMultiplier:      uint32(config.BFDDetectionMultiplier),
	}
}
