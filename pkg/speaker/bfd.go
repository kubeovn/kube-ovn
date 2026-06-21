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

	// Log BFD server statistics
	if klog.V(4).Enabled() {
		if stats := c.config.BgpServer.GetBfdServerStats(); stats != nil {
			klog.V(4).Infof("BFD server stats: received=%d, dropped=%d, errors=%d, invalid=%d, unknownPeer=%d",
				stats.ReceivedPacket, stats.ReceivedDrop, stats.ReceivedError, stats.InvalidPacket, stats.UnknownPeer)
		}
	}

	if !klog.V(5).Enabled() {
		return
	}

	c.config.BgpServer.ListBfdPeer(context.Background(), func(addr string, state *api.BfdPeerState) {
		if state == nil {
			klog.V(5).Infof("BFD peer %s: no state available", addr)
			return
		}

		if async := state.BfdAsync; async != nil {
			klog.V(5).Infof("BFD peer %s: state=%s, tx=%d, rx=%d",
				addr, bfdSessionStateString(state.SessionState),
				async.TransmittedPackets, async.ReceivedPackets)
		} else {
			klog.V(5).Infof("BFD peer %s: state=%s",
				addr, bfdSessionStateString(state.SessionState))
		}
	})
}
