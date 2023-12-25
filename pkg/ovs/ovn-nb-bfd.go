package ovs

import (
	"context"
	"fmt"
	"time"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"

	"github.com/ovn-org/libovsdb/cache"
	"github.com/ovn-org/libovsdb/model"
)

func (c *OVNNbClient) ListBFDs(lrpName, dstIP string) ([]ovnnb.BFD, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	bfdList := make([]ovnnb.BFD, 0)
	if err := c.ovsDbClient.WhereCache(func(bfd *ovnnb.BFD) bool {
		if bfd.LogicalPort != lrpName {
			return false
		}
		return dstIP == "" || bfd.DstIP == dstIP
	}).List(ctx, &bfdList); err != nil {
		err := fmt.Errorf("failed to list BFD with logical_port=%s and dst_ip=%s: %v", lrpName, dstIP, err)
		klog.Error(err)
		return nil, err
	}

	return bfdList, nil
}

func (c *OVNNbClient) ListDownBFDs(dstIP string) ([]ovnnb.BFD, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	bfdList := make([]ovnnb.BFD, 0)
	if err := c.ovsDbClient.WhereCache(func(bfd *ovnnb.BFD) bool {
		if bfd.DstIP == dstIP && (*bfd.Status == ovnnb.BFDStatusDown || *bfd.Status == ovnnb.BFDStatusAdminDown) {
			return true
		}
		return false
	}).List(ctx, &bfdList); err != nil {
		err := fmt.Errorf("failed to list down BFDs: %v", err)
		klog.Error(err)
		return nil, err
	}

	return bfdList, nil
}

func (c *OVNNbClient) ListUpBFDs(dstIP string) ([]ovnnb.BFD, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	bfdList := make([]ovnnb.BFD, 0)
	if err := c.ovsDbClient.WhereCache(func(bfd *ovnnb.BFD) bool {
		return bfd.DstIP == dstIP && *bfd.Status == ovnnb.BFDStatusUp
	}).List(ctx, &bfdList); err != nil {
		err := fmt.Errorf("failed to list up BFDs: %v", err)
		klog.Error(err)
		return nil, err
	}

	return bfdList, nil
}

func (c *OVNNbClient) CreateBFD(lrpName, dstIP string, minRx, minTx, detectMult int) (*ovnnb.BFD, error) {
	bfdList, err := c.ListBFDs(lrpName, dstIP)
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
		err := fmt.Errorf("failed to generate operations for BFD creation with logical_port=%s and dst_ip=%s: %v", lrpName, dstIP, err)
		klog.Error(err)
		return nil, err
	}
	if err = c.Transact("bfd-add", ops); err != nil {
		err := fmt.Errorf("failed to create BFD with logical_port=%s and dst_ip=%s: %v", lrpName, dstIP, err)
		klog.Error(err)
		return nil, err
	}

	if bfdList, err = c.ListBFDs(lrpName, dstIP); err != nil {
		err := fmt.Errorf("failed to list BFDs: %v", err)
		klog.Error(err)
		return nil, err
	}
	if len(bfdList) == 0 {
		return nil, fmt.Errorf("BFD with logical_port=%s and dst_ip=%s not found", lrpName, dstIP)
	}
	return &bfdList[0], nil
}

// UpdateBFD update BFD
func (c *OVNNbClient) UpdateBFD(bfd *ovnnb.BFD, fields ...interface{}) error {
	op, err := c.ovsDbClient.Where(bfd).Update(bfd, fields...)
	if err != nil {
		err := fmt.Errorf("failed to generate bfd update operations for lrp %s with fields %v: %v", bfd.LogicalPort, fields, err)
		klog.Error(err)
		return err
	}
	if err = c.Transact("bfd-update", op); err != nil {
		err := fmt.Errorf("failed to update bfd %s for lrp %s: %v", bfd.UUID, bfd.LogicalPort, err)
		klog.Error(err)
		return err
	}
	return nil
}

func (c *OVNNbClient) DeleteBFD(lrpName, dstIP string) error {
	bfdList, err := c.ListBFDs(lrpName, dstIP)
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
			err := fmt.Errorf("failed to generate operations for BFD deletion with UUID %s: %v", bfd.UUID, err)
			klog.Error(err)
			return err
		}
		if err = c.Transact("bfd-del", ops); err != nil {
			err := fmt.Errorf("failed to delete BFD with with UUID %s: %v", bfd.UUID, err)
			klog.Error(err)
			return err
		}
	}

	return nil
}

// MonitorBFD will add a handler
// to NB libovsdb cache to update the BFD priority.
// This function should only be called once.
func (c *OVNNbClient) MonitorBFD() {
	c.ovsDbClient.Cache().AddEventHandler(&cache.EventHandlerFuncs{
		AddFunc: func(table string, model model.Model) {
			c.bfdAddL3HAHandler(table, model)
		},
		UpdateFunc: func(table string, oldModel, newModel model.Model) {
			c.bfdUpdateL3HAHandler(table, oldModel, newModel)
		},
		DeleteFunc: func(table string, model model.Model) {
			c.bfdDelL3HAHandler(table, model)
		},
	})
}

func (c *OVNNbClient) isLrpBfdUp(lrpName, dstIP string) (bool, error) {
	bfdList, err := c.ListBFDs(lrpName, dstIP)
	if err != nil {
		klog.Errorf("failed to list bfd for lrp %s, %v", lrpName, err)
		return false, err
	}
	if len(bfdList) == 0 {
		klog.Errorf("no bfd for lrp %s", lrpName)
		// no bfd, means no need to handle
		return true, nil
	}
	bfd := bfdList[0]
	if bfd.Status == nil {
		err := fmt.Errorf("lrp %s bfd status is nil", lrpName)
		klog.Error(err)
		return false, err
	} else if *bfd.Status == ovnnb.BFDStatusUp {
		klog.Infof("lrp %s bfd dst ip %s status is up", lrpName, bfd.DstIP)
		return true, nil
	}
	// bfd status is still down
	err = fmt.Errorf("lrp %s bfd dst ip %s status is down", lrpName, bfd.DstIP)
	klog.Error(err)
	return false, err
}

func (c *OVNNbClient) bfdAddL3HAHandler(table string, model model.Model) {
	if table != ovnnb.BFDTable {
		return
	}

	bfd := model.(*ovnnb.BFD)
	klog.Infof("lrp %s add BFD to dst ip %s", bfd.LogicalPort, bfd.DstIP)
	needRecheck := false
	if bfd.Status == nil {
		needRecheck = true
	} else if *bfd.Status != ovnnb.BFDStatusUp {
		needRecheck = true
	}
	if !needRecheck {
		return
	}
	// bfd status should be up in 15 seconds
	for try := 1; try < 4; try++ {
		time.Sleep(5 * time.Second)
		klog.Warningf("the %d time check bfd status for lrp %s dst ip %s", try, bfd.LogicalPort, bfd.DstIP)
		if ok, err := c.isLrpBfdUp(bfd.LogicalPort, bfd.DstIP); err != nil {
			klog.Errorf("failed to check bfd status for lrp %s dst ip %s, %v", bfd.LogicalPort, bfd.DstIP, err)
			continue
		} else if ok {
			break
		}
	}
}

func (c *OVNNbClient) bfdUpdateL3HAHandler(table string, oldModel, newModel model.Model) {
	if table != ovnnb.BFDTable {
		return
	}

	oldBfd := oldModel.(*ovnnb.BFD)
	newBfd := newModel.(*ovnnb.BFD)

	if oldBfd.Status == nil || newBfd.Status == nil {
		return
	}
	klog.Infof("lrp %s BFD to dst ip %s status changed from %s to %s", newBfd.LogicalPort, newBfd.DstIP, *oldBfd.Status, *newBfd.Status)

	if *oldBfd.Status == *newBfd.Status {
		return
	}
	lrpName := newBfd.LogicalPort
	dstIP := newBfd.DstIP
	if *oldBfd.Status == ovnnb.BFDStatusAdminDown && *newBfd.Status == ovnnb.BFDStatusDown {
		// bfd status should be up in 15 seconds
		for try := 1; try <= 3; try++ {
			time.Sleep(5 * time.Second)
			klog.Warningf("the %d time check bfd status for lrp %s dst ip %s", try, lrpName, dstIP)
			if ok, err := c.isLrpBfdUp(lrpName, dstIP); err != nil {
				klog.Errorf("failed to check bfd status for lrp %s dst ip %s, %v", lrpName, dstIP, err)
				continue
			} else if ok {
				break
			}
		}
	}

	if *oldBfd.Status == ovnnb.BFDStatusDown && *newBfd.Status == ovnnb.BFDStatusUp {
		// up
		gwChassisList, err := c.ListGatewayChassisByLogicalRouterPort(lrpName, false)
		if err != nil {
			klog.Errorf("failed to list gateway chassis for lrp %s, %v", lrpName, err)
			return
		}
		if len(gwChassisList) == 0 {
			klog.Errorf("no gateway chassis for lrp %s", lrpName)
			return
		}
		goodChassis := gwChassisList[0]
		goodChassis.Priority = util.GwChassisMaxPriority + 1
		klog.Infof("raise good chassis %s priority to %d", goodChassis.Name, goodChassis.Priority)
		if err := c.UpdateGatewayChassis(&goodChassis, &goodChassis.Priority); err != nil {
			klog.Errorf("failed to update good chassis %s, %v", goodChassis.Name, err)
			return
		}
	}

	// LRP may still locate on a bad chassis node
	// update recheck the bfd status later
	if *oldBfd.Status == ovnnb.BFDStatusUp && *newBfd.Status == ovnnb.BFDStatusDown {
		// down
		lrpName := newBfd.LogicalPort
		gwChassisList, err := c.ListGatewayChassisByLogicalRouterPort(lrpName, false)
		if err != nil {
			klog.Errorf("failed to list gateway chassis for lrp %s, %v", lrpName, err)
			return
		}
		if len(gwChassisList) == 0 {
			klog.Errorf("no gateway chassis for lrp %s", lrpName)
			return
		}
		badChassis := gwChassisList[0]
		// centralized gw chassis node number probably less than 5
		badChassis.Priority = util.GwChassisMaxPriority - 5
		klog.Infof("lower bad chassis %s priority to %d", badChassis.Name, badChassis.Priority)
		if err := c.UpdateGatewayChassis(&badChassis, &badChassis.Priority); err != nil {
			klog.Errorf("failed to update bad chassis %s, %v", badChassis.Name, err)
			return
		}
		// lower bad chassis priority will not trigger bfd update
		// recheck until bfd status is up
		try := 1
		for {
			time.Sleep(5 * time.Second)
			klog.Warningf("the %d time check bfd status for lrp %s dst ip %s", try, lrpName, dstIP)
			if ok, err := c.isLrpBfdUp(lrpName, dstIP); err != nil {
				// bfd status is still down
				// update bfd external_ids to trigger bfd update
				klog.Errorf("failed to check bfd status for lrp %s dst ip %s, %v", lrpName, dstIP, err)
				gwChassisList, err = c.ListGatewayChassisByLogicalRouterPort(lrpName, false)
				if err != nil {
					klog.Errorf("failed to list gateway chassis for lrp %s, %v", lrpName, err)
					return
				}
				if len(gwChassisList) == 0 {
					klog.Errorf("no gateway chassis for lrp %s", lrpName)
					return
				}
				badChassis = gwChassisList[0]
				if newBfd.ExternalIDs == nil {
					newBfd.ExternalIDs = make(map[string]string)
				}
				if newBfd.ExternalIDs["gateway_chassis"] == badChassis.Name {
					klog.Errorf("lrp stuck on bad chassis %s", badChassis.Name)
					return
				}
				newBfd.ExternalIDs["gateway_chassis"] = badChassis.Name
				klog.Infof("update bfd for lrp %s dst ip %s external_ids gateway_chassis to %s", newBfd.LogicalPort, dstIP, newBfd.ExternalIDs["gateway_chassis"])
				if err := c.UpdateBFD(newBfd, &newBfd.ExternalIDs); err != nil {
					klog.Errorf("failed to update bfd for lrp %s, %v", lrpName, err)
					return
				}
				continue
			} else if ok {
				break
			}
			try++
		}
	}
}

func (c *OVNNbClient) bfdDelL3HAHandler(table string, model model.Model) {
	if table != ovnnb.BFDTable {
		return
	}
	bfd := model.(*ovnnb.BFD)
	klog.Infof("lrp %s del BFD to dst ip %s", bfd.LogicalPort, bfd.DstIP)
}
