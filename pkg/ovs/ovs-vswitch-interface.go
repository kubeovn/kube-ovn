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
	if err := c.ovsDbClient.WhereCache(func(iface *vswitch.Interface) bool {
		if filter != nil {
			return filter(iface)
		}
		return true
	}).List(ctx, &ifaceList); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to list interface: %w", err)
	}

	return ifaceList, nil
}
