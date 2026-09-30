package ovs

import (
	"fmt"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

// ListInterface lists ovs interfaces
func (c *VswitchClient) ListInterface(filter func(sw *vswitch.Interface) bool) ([]vswitch.Interface, error) {
	return filterLogged(c.Database, &vswitch.Interface{}, func(iface *vswitch.Interface) bool {
		return filter == nil || filter(iface)
	}, func(err error) error {
		return fmt.Errorf("failed to list interface: %w", err)
	})
}

func (c *VswitchClient) CleanInterface(name string) error {
	ctx, cancel := timeoutCtx(c.Database)
	defer cancel()
	return DeleteVswitchPort(ctx, c, name)
}
