package ovs

import (
	"context"
	"fmt"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"k8s.io/klog/v2"
)

func (c *ovnClient) ListBFD(lrpName, dstIP string) ([]ovnnb.BFD, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	bfdList := make([]ovnnb.BFD, 0)
	if err := c.ovnNbClient.WhereCache(func(bfd *ovnnb.BFD) bool {
		if bfd.LogicalPort != lrpName {
			return false
		}
		return dstIP == "" || bfd.DstIP == dstIP
	}).List(ctx, &bfdList); err != nil {
		return nil, fmt.Errorf("failed to list BFD with logical_port=%s and dst_ip=%s: %v", lrpName, dstIP, err)
	}

	return bfdList, nil
}

func (c *ovnClient) CreateBFD(lrpName, dstIP string, minRx, minTx, detectMult int) (*ovnnb.BFD, error) {
	bfdList, err := c.ListBFD(lrpName, dstIP)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	if len(bfdList) != 0 {
		return &bfdList[0], nil
	}

	bfd := &ovnnb.BFD{
		LogicalPort: lrpName,
		DstIP:       dstIP,
		MinRx:       &minRx,
		MinTx:       &minTx,
		DetectMult:  &detectMult,
	}
	ops, err := c.Create(bfd)
	if err != nil {
		return nil, fmt.Errorf("failed to generate operations for BFD creation with logical_port=%s and dst_ip=%s: %v", lrpName, dstIP, err)
	}
	if err = c.Transact("bfd-add", ops); err != nil {
		return nil, fmt.Errorf("failed to create BFD with logical_port=%s and dst_ip=%s: %v", lrpName, dstIP, err)
	}

	if bfdList, err = c.ListBFD(lrpName, dstIP); err != nil {
		return nil, err
	}
	if len(bfdList) == 0 {
		return nil, fmt.Errorf("BFD with logical_port=%s and dst_ip=%s not found", lrpName, dstIP)
	}
	return &bfdList[0], nil
}

func (c *ovnClient) DeleteBFD(lrpName, dstIP string) error {
	bfdList, err := c.ListBFD(lrpName, dstIP)
	if err != nil {
		klog.Error(err)
		return err
	}
	if len(bfdList) == 0 {
		return nil
	}

	for _, bfd := range bfdList {
		ops, err := c.Where(&bfd).Delete()
		if err != nil {
			return fmt.Errorf("failed to generate operations for BFD deletion with UUID %s: %v", bfd.UUID, err)
		}
		if err = c.Transact("bfd-del", ops); err != nil {
			return fmt.Errorf("failed to delete BFD with with UUID %s: %v", bfd.UUID, err)
		}
	}

	return nil
}
