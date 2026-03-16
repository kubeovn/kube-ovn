package ovs

import (
	"context"
	"errors"
	"fmt"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
)

// GetLogicalSwitchTunnelKey retrieves the tunnel_key for a logical switch from OVN SB Datapath_Binding.
func (c *OVNSbClient) GetLogicalSwitchTunnelKey(lsName string) (int, error) {
	if lsName == "" {
		return 0, errors.New("logical switch name is empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	dpList := make([]ovnsb.DatapathBinding, 0)
	if err := c.ovsDbClient.WhereCache(func(dp *ovnsb.DatapathBinding) bool {
		if dp.ExternalIDs == nil {
			return false
		}
		return dp.ExternalIDs["name"] == lsName
	}).List(ctx, &dpList); err != nil {
		return 0, fmt.Errorf("failed to list datapath binding for logical switch %s: %w", lsName, err)
	}

	if len(dpList) == 0 {
		return 0, fmt.Errorf("datapath binding not found for logical switch %s", lsName)
	}

	if len(dpList) > 1 {
		// This indicates a critical OVN SB database inconsistency.
		// Each logical switch should have exactly one datapath binding.
		// Multiple bindings suggest NB/SB sync issues or database corruption.
		return 0, fmt.Errorf("found %d datapath bindings for logical switch %s, expected exactly one: OVN SB database may be inconsistent", len(dpList), lsName)
	}

	return dpList[0].TunnelKey, nil
}
