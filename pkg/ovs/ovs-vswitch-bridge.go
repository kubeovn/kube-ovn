package ovs

import (
	"context"
	"fmt"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// ListBridge lists ovs bridges
func (c *VswitchClient) ListBridge(needVendorFilter bool, filter func(bridge *vswitch.Bridge) bool) ([]vswitch.Bridge, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var bridgeList []vswitch.Bridge
	if err := c.ovsDbClient.WhereCache(func(bridge *vswitch.Bridge) bool {
		if needVendorFilter && (len(bridge.ExternalIDs) == 0 || bridge.ExternalIDs[ExternalIDVendor] != util.CniTypeName) {
			return false
		}
		if filter != nil {
			return filter(bridge)
		}
		return true
	}).List(ctx, &bridgeList); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to list bridge: %w", err)
	}

	return bridgeList, nil
}
