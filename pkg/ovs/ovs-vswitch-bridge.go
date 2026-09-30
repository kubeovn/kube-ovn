package ovs

import (
	"fmt"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

// ListBridge lists ovs bridges
func (c *VswitchClient) ListBridge(needVendorFilter bool, filter func(bridge *vswitch.Bridge) bool) ([]vswitch.Bridge, error) {
	return filterLogged(c.Database, &vswitch.Bridge{}, func(bridge *vswitch.Bridge) bool {
		if needVendorFilter && !hasVendor(bridge.ExternalIDs) {
			return false
		}
		return filter == nil || filter(bridge)
	}, func(err error) error {
		return fmt.Errorf("failed to list bridge: %w", err)
	})
}
