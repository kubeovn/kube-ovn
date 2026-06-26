package speaker

import (
	"context"

	"github.com/osrg/gobgp/v4/api"
	"k8s.io/klog/v2"
)

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
// Guarded by klog verbosity to avoid unnecessary gRPC calls when logging is off.
func (c *Controller) logBFDStatus() {
	if !c.config.EnableBFD {
		return
	}

	// Guard with Enabled() to skip gRPC calls when log level is insufficient.
	if !klog.V(3).Enabled() {
		return
	}

	if stats := c.config.BgpServer.GetBfdServerStats(); stats != nil {
		hasErrors := stats.ReceivedDrop > 0 || stats.ReceivedError > 0 || stats.InvalidPacket > 0 || stats.UnknownPeer > 0
		if hasErrors {
			klog.Warningf("BFD server stats: received=%d, dropped=%d, errors=%d, invalid=%d, unknownPeer=%d",
				stats.ReceivedPacket, stats.ReceivedDrop, stats.ReceivedError, stats.InvalidPacket, stats.UnknownPeer)
		} else if c.lastBFDStatsHasErrors {
			klog.Infof("BFD server stats recovered: received=%d, dropped=%d, errors=%d, invalid=%d, unknownPeer=%d",
				stats.ReceivedPacket, stats.ReceivedDrop, stats.ReceivedError, stats.InvalidPacket, stats.UnknownPeer)
		}
		c.lastBFDStatsHasErrors = hasErrors
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
