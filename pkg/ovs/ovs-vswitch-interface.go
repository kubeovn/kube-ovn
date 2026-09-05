package ovs

import (
	"context"
	"fmt"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

// ListInterface lists ovs interfaces
func (c *VswitchClient) ListInterface(filter func(sw *vswitch.Interface) bool) ([]vswitch.Interface, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var ifaceList []vswitch.Interface
	if err := c.Database.Table(&vswitch.Interface{}).Filter(ctx, func(iface *vswitch.Interface) bool {
		if filter != nil {
			return filter(iface)
		}
		return true
	}, &ifaceList); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to list interface: %w", err)
	}

	return ifaceList, nil
}

// CleanInterface removes an orphaned OVS port and the rows owned by it. The
// operation is assembled from the monitored tables so callers do not need to
// shell out to ovs-vsctl for database state changes.
func (c *VswitchClient) CleanInterface(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	return DeleteVswitchPort(ctx, c, name)
}
