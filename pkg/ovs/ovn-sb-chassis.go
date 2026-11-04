package ovs

import (
	"errors"
	"fmt"
	"maps"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var ErrOneNodeMultiChassis = errors.New("OneNodeMultiChassis")

func (c *OVNSbClient) UpdateChassis(chassis *ovnsb.Chassis, fields ...any) error {
	op, err := c.Database.Table(&ovnsb.Chassis{}).UpdateOps(chassis, chassis, fields...)
	return transactGeneratedOn(c, "chassis-update", op, err,
		wrapErr("failed to generate update operations for chassis: %w"),
		func(err error) error { return fmt.Errorf("failed to update chassis %s: %w", chassis.Name, err) },
	)
}

// DeleteChassis delete one chassis and its chassis_private by name
func (c *OVNSbClient) DeleteChassis(chassisName string) error {
	chassis, err := c.GetChassis(chassisName, true)
	if err != nil {
		klog.Error(err)
		return err
	}
	if chassis == nil {
		return nil
	}
	ops, err := c.Database.Where(chassis).Delete()
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to generate delete chassis operations for node %s: %w", chassis.Hostname, err)
	}
	// chassis_private is not monitored, delete it by the indexed name column
	privateOps, err := c.Database.Where(&ovnsb.ChassisPrivate{Name: chassisName}).Delete()
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to generate delete chassis_private operations for node %s: %w", chassis.Hostname, err)
	}
	ops = append(ops, privateOps...)
	if err = c.Transact("chassis-del", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to delete chassis for node %s: %w", chassis.Hostname, err)
	}
	return nil
}

// GetChassis return south bound db chassis from cache
func (c *OVNSbClient) GetChassis(chassisName string, ignoreNotFound bool) (*ovnsb.Chassis, error) {
	if err := requireName(chassisName, "chassis name is empty"); err != nil {
		return nil, err
	}
	chassis, err := getIndexedWrap(c.Database, &ovnsb.Chassis{Name: chassisName}, ignoreNotFound, func(err error) error {
		return fmt.Errorf("failed to get chassis %s: %w", chassisName, err)
	})
	if err != nil {
		return nil, err
	}
	klog.V(3).Infof("get chassis: %+v", chassis)
	return chassis, nil
}

// ListChassis return south bound db chassis from cache
func (c *OVNSbClient) ListChassis() (*[]ovnsb.Chassis, error) {
	css, err := filterLogged(c.Database, &ovnsb.Chassis{}, func(*ovnsb.Chassis) bool { return true }, func(err error) error {
		return fmt.Errorf("failed to list Chassis: %w", err)
	})
	if err != nil {
		return nil, err
	}
	return &css, nil
}

func (c *OVNSbClient) GetChassisByHost(nodeName string) (*ovnsb.Chassis, error) {
	if err := requireName(nodeName, "failed to get Chassis with empty hostname"); err != nil {
		return nil, err
	}
	chassisList, err := filterLogged(c.Database, &ovnsb.Chassis{}, func(chassis *ovnsb.Chassis) bool {
		return chassis.Hostname == nodeName
	}, func(err error) error {
		return fmt.Errorf("failed to list Chassis with hostname=%s: %w", nodeName, err)
	})
	if err != nil {
		return nil, err
	}
	chassis, err := table.Unique(chassisList, false,
		fmt.Errorf("failed to get Chassis with hostname=%s", nodeName),
		ErrOneNodeMultiChassis,
	)
	if err != nil {
		return nil, logErr(err)
	}
	return chassis, nil
}

// DeleteChassisByHost delete all chassis by node name
func (c *OVNSbClient) DeleteChassisByHost(nodeName string) error {
	chassisList, err := filterLogged(c.Database, &ovnsb.Chassis{}, func(chassis *ovnsb.Chassis) bool {
		return chassis.Hostname == nodeName || (chassis.ExternalIDs != nil && chassis.ExternalIDs["node"] == nodeName)
	}, func(err error) error {
		return fmt.Errorf("failed to list Chassis with hostname=%s: %w", nodeName, err)
	})
	if err != nil {
		return err
	}
	for _, chassis := range chassisList {
		klog.Infof("delete chassis: %+v", chassis)
		if err := c.DeleteChassis(chassis.Name); err != nil {
			err := fmt.Errorf("failed to delete chassis %s, %w", chassis.Name, err)
			return logErr(err)
		}
	}
	return nil
}

func (c *OVNSbClient) UpdateChassisTag(chassisName, nodeName string) error {
	chassis, err := c.GetChassis(chassisName, true)
	if err != nil {
		return logErr(err)
	}
	if chassis == nil {
		err := fmt.Errorf("fail to get chassis by name=%s", chassisName)
		// restart kube-ovn-cni, chassis will be created
		return logErr(err)
	}
	if chassis.ExternalIDs == nil || chassis.ExternalIDs["vendor"] != util.CniTypeName {
		// The only field this function actually writes is "vendor"; gate the
		// update on that, not on "node" (a key this function stopped writing
		// below - "not need to filter chassis by node name if we use
		// libovsdb" - so comparing it here was always comparing against a
		// value nothing sets, making this a no-op whenever a legacy row
		// happened to still carry a stale but matching "node" id).
		externalIDs := make(map[string]string, len(chassis.ExternalIDs)+2)
		maps.Copy(externalIDs, chassis.ExternalIDs)
		externalIDs["vendor"] = util.CniTypeName
		chassis.ExternalIDs = externalIDs
		if err := c.UpdateChassis(chassis, &chassis.ExternalIDs); err != nil {
			return logWrap(err, wrapErr("failed to init chassis node %s: %w", nodeName))
		}
	}
	return nil
}

// GetKubeOvnChassises return all chassis which vendor is kube-ovn
func (c *OVNSbClient) GetKubeOvnChassises() (*[]ovnsb.Chassis, error) {
	chassisList, err := filterWrap(c.Database, &ovnsb.Chassis{}, func(chassis *ovnsb.Chassis) bool {
		return hasVendor(chassis.ExternalIDs)
	}, func(err error) error {
		return fmt.Errorf("failed to list Chassis with vendor=%s: %w", util.CniTypeName, err)
	})
	if err != nil {
		return nil, err
	}
	return &chassisList, nil
}
