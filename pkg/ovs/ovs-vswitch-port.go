package ovs

import (
	"fmt"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

// ListPort lists ovs ports
func (c *VswitchClient) ListPort(filter func(sw *vswitch.Port) bool) ([]vswitch.Port, error) {
	return filterLogged(c.Database, &vswitch.Port{}, func(port *vswitch.Port) bool {
		return filter == nil || filter(port)
	}, func(err error) error {
		return fmt.Errorf("failed to list port: %w", err)
	})
}
