package ovs

import (
	"context"
	"fmt"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

// ListPort lists ovs ports
func (c *VswitchClient) ListPort(filter func(sw *vswitch.Port) bool) ([]vswitch.Port, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var portList []vswitch.Port
	if err := c.ovsDbClient.WhereCache(func(port *vswitch.Port) bool {
		if filter != nil {
			return filter(port)
		}
		return true
	}).List(ctx, &portList); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to list port: %w", err)
	}

	return portList, nil
}
