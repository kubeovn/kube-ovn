package ovs

import (
	"context"
	"fmt"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
)

func (c *OVNSbClient) ListBFDs(lrpName, dstIP string) ([]ovnsb.BFD, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	bfdList := make([]ovnsb.BFD, 0)
	if err := c.ovsDbClient.WhereCache(func(bfd *ovnsb.BFD) bool {
		if bfd.LogicalPort != lrpName {
			return false
		}
		return dstIP == "" || bfd.DstIP == dstIP
	}).List(ctx, &bfdList); err != nil {
		err := fmt.Errorf("failed to list SB BFD with logical_port=%s and dst_ip=%s: %w", lrpName, dstIP, err)
		klog.Error(err)
		return nil, err
	}

	return bfdList, nil
}
