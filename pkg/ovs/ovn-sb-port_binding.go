package ovs

import (
	"context"
	"errors"
	"fmt"

	"github.com/ovn-kubernetes/libovsdb/client"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
)

func (c *OVNSbClient) GetPortBinding(logicalPort string, ignoreNotFound bool) (*ovnsb.PortBinding, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	portBinding := &ovnsb.PortBinding{LogicalPort: logicalPort}
	if err := c.Get(ctx, portBinding); err != nil {
		if ignoreNotFound && errors.Is(err, client.ErrNotFound) {
			return nil, nil
		}
		err := fmt.Errorf("failed to get port binding %s: %w", logicalPort, err)
		klog.Error(err)
		return nil, err
	}

	return portBinding, nil
}
