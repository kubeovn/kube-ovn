package ovs

import (
	"fmt"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *OVNNbClient) listBFDs(predicate func(*ovnnb.BFD) bool, wrap func(error) error) ([]ovnnb.BFD, error) {
	bfdList, err := filterTimeout(c.Database, &ovnnb.BFD{}, predicate)
	if err != nil {
		return nil, logErr(wrap(err))
	}
	return bfdList, nil
}

func (c *OVNNbClient) ListBFDs(lrpName, dstIP string) ([]ovnnb.BFD, error) {
	return c.listBFDs(func(bfd *ovnnb.BFD) bool {
		return bfd.LogicalPort == lrpName && (dstIP == "" || bfd.DstIP == dstIP)
	}, func(err error) error {
		return fmt.Errorf("failed to list BFD with logical_port=%s and dst_ip=%s: %w", lrpName, dstIP, err)
	})
}

func (c *OVNNbClient) ListDownBFDs(dstIP string) ([]ovnnb.BFD, error) {
	return c.listBFDs(func(bfd *ovnnb.BFD) bool {
		return bfd.DstIP == dstIP && (*bfd.Status == ovnnb.BFDStatusDown || *bfd.Status == ovnnb.BFDStatusAdminDown)
	}, func(err error) error {
		return fmt.Errorf("failed to list down BFDs: %w", err)
	})
}

func (c *OVNNbClient) ListUpBFDs(dstIP string) ([]ovnnb.BFD, error) {
	return c.listBFDs(func(bfd *ovnnb.BFD) bool {
		return bfd.DstIP == dstIP && *bfd.Status == ovnnb.BFDStatusUp
	}, func(err error) error {
		return fmt.Errorf("failed to list up BFDs: %w", err)
	})
}

func (c *OVNNbClient) CreateBFD(lrpName, dstIP string, minRx, minTx, detectMult int, externalIDs map[string]string) (*ovnnb.BFD, error) {
	bfdList, err := c.ListBFDs(lrpName, dstIP)
	if err != nil {
		return nil, logErr(err)
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
		ExternalIDs: externalIDs,
	}
	ops, err := c.Database.Table(&ovnnb.BFD{}).CreateOps(bfd)
	if err := c.transactGenerated("bfd-add", ops, err,
		wrapErr("failed to generate operations for BFD creation with logical_port=%s and dst_ip=%s: %w", lrpName, dstIP),
		wrapErr("failed to create BFD with logical_port=%s and dst_ip=%s: %w", lrpName, dstIP),
	); err != nil {
		return nil, err
	}

	if bfdList, err = c.ListBFDs(lrpName, dstIP); err != nil {
		return nil, logFmt("failed to list BFDs: %w", err)
	}
	if len(bfdList) == 0 {
		return nil, fmt.Errorf("BFD with logical_port=%s and dst_ip=%s not found", lrpName, dstIP)
	}
	return &bfdList[0], nil
}

// UpdateBFD update BFD
func (c *OVNNbClient) UpdateBFD(bfd *ovnnb.BFD, fields ...any) error {
	return c.updateModelLogged("bfd-update", bfd, func(err error) error {
		return fmt.Errorf("failed to update bfd %s for lrp %s: %w", bfd.UUID, bfd.LogicalPort, err)
	}, fields...)
}

func (c *OVNNbClient) deleteBFDByUUID(uuid string) error {
	ops, err := c.Database.Table(&ovnnb.BFD{}).DeleteOps(&ovnnb.BFD{UUID: uuid})
	return c.transactGenerated("bfd-del", ops, err,
		wrapErr("failed to generate operations for BFD deletion with UUID %s: %w", uuid),
		wrapErr("failed to delete BFD with UUID %s: %w", uuid),
	)
}

func (c *OVNNbClient) DeleteBFD(uuid string) error {
	return c.deleteBFDByUUID(uuid)
}

func (c *OVNNbClient) DeleteBFDByDstIP(lrpName, dstIP string) error {
	bfdList, err := c.ListBFDs(lrpName, dstIP)
	if err != nil {
		return logErr(err)
	}
	if len(bfdList) == 0 {
		return nil
	}
	for _, bfd := range bfdList {
		klog.Infof("delete lrp %s BFD dst ip %s", lrpName, bfd.DstIP)
		if err := c.deleteBFDByUUID(bfd.UUID); err != nil {
			return err
		}
	}
	return nil
}

// MonitorBFD will add a handler
// to NB libovsdb cache to update the BFD priority.
// This function should only be called once.
func (c *OVNNbClient) MonitorBFD() {
	c.Database.Cache().AddEventHandler(&compat.EventHandlerFuncs{
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
		return false, logFmt("lrp %s bfd status is nil", lrpName)
	} else if *bfd.Status == ovnnb.BFDStatusUp {
		klog.Infof("lrp %s bfd dst ip %s status is up", lrpName, bfd.DstIP)
		return true, nil
	}
	// bfd status is still down
	return false, logFmt("lrp %s bfd dst ip %s status is down", lrpName, bfd.DstIP)
}

func (c *OVNNbClient) bfdAddL3HAHandler(table string, model model.Model) {
	if table != ovnnb.BFDTable {
		return
	}

	bfd := model.(*ovnnb.BFD)
	if bfd.ExternalIDs[ExternalIDVpcEgressGateway] != "" || bfd.ExternalIDs[ExternalIDVpcNatGateway] != "" {
		return
	}

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

	if newBfd.ExternalIDs[ExternalIDVpcEgressGateway] != "" || newBfd.ExternalIDs[ExternalIDVpcNatGateway] != "" {
		return
	}
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
	if bfd.ExternalIDs[ExternalIDVpcEgressGateway] != "" || bfd.ExternalIDs[ExternalIDVpcNatGateway] != "" {
		return
	}
	klog.Infof("lrp %s del BFD to dst ip %s", bfd.LogicalPort, bfd.DstIP)
}

func (c *OVNNbClient) FindBFD(externalIDs map[string]string) ([]ovnnb.BFD, error) {
	return c.listBFDs(func(bfd *ovnnb.BFD) bool {
		return matchExternalIDsMode(bfd.ExternalIDs, externalIDs, false)
	}, func(err error) error {
		return fmt.Errorf("failed to find ovn BFD: %w", err)
	})
}
