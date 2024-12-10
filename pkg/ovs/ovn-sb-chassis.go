package ovs

import (
	"context"
	"errors"
	"fmt"

	"github.com/ovn-org/libovsdb/client"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	ErrOneNodeMultiChassis = errors.New("OneNodeMultiChassis")
)

func (c *OVNSbClient) UpdateChassis(chassis *ovnsb.Chassis, fields ...interface{}) error {
	op, err := c.ovsDbClient.Where(chassis).Update(chassis, fields...)
	if err != nil {
		err := fmt.Errorf("failed to generate update operations for chassis: %w", err)
		klog.Error(err)
		return err
	}
	if err = c.Transact("chassis-update", op); err != nil {
		err := fmt.Errorf("failed to update chassis %s: %w", chassis.Name, err)
		klog.Error(err)
		return err
	}
	return nil
}

// DeleteChassis delete one chassis by name
func (c *OVNSbClient) DeleteChassis(chassisName string) error {
	chassis, err := c.GetChassis(chassisName, true)
	if err != nil {
		klog.Error(err)
		return err
	}
	if chassis == nil {
		return nil
	}
	ops, err := c.ovsDbClient.Where(chassis).Delete()
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to generate delete chassis operations for node %s: %w", chassis.Hostname, err)
	}
	if err = c.Transact("chassis-del", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to delete chassis for node %s: %w", chassis.Hostname, err)
	}
	return nil
}

// GetChassis return south bound db chassis from cache
func (c *OVNSbClient) GetChassis(chassisName string, ignoreNotFound bool) (*ovnsb.Chassis, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	if chassisName == "" {
		err := errors.New("chassis name is empty")
		klog.Error(err)
		return nil, err
	}
	chassis := &ovnsb.Chassis{Name: chassisName}
	if err := c.ovsDbClient.Get(ctx, chassis); err != nil {
		if ignoreNotFound && errors.Is(err, client.ErrNotFound) {
			return nil, nil
		}
		klog.Error(err)
		return nil, fmt.Errorf("failed to get chassis %s: %w", chassisName, err)
	}
	klog.V(3).Infof("get chassis: %+v", chassis)
	return chassis, nil
}

// ListChassis return south bound db chassis from cache
func (c *OVNSbClient) ListChassis() (*[]ovnsb.Chassis, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	css := []ovnsb.Chassis{}
	if err := c.ovsDbClient.List(ctx, &css); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to list Chassis: %w", err)
	}
	return &css, nil
}

func (c *OVNSbClient) GetChassisByHost(nodeName string) (*ovnsb.Chassis, error) {
	if nodeName == "" {
		err := errors.New("failed to get Chassis with empty hostname")
		klog.Error(err)
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	chassisList := make([]ovnsb.Chassis, 0)
	if err := c.ovsDbClient.WhereCache(func(chassis *ovnsb.Chassis) bool {
		return chassis.Hostname == nodeName
	}).List(ctx, &chassisList); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to list Chassis with host name=%s: %w", nodeName, err)
	}
	if len(chassisList) == 0 {
		err := fmt.Errorf("failed to get Chassis with host name=%s", nodeName)
		klog.Error(err)
		return nil, err
	}
	if len(chassisList) != 1 {
		err := fmt.Errorf("found more than one Chassis with host name=%s", nodeName)
		klog.Error(err)
		return nil, ErrOneNodeMultiChassis
	}

	// #nosec G602
	return &chassisList[0], nil
}

// DeleteChassisByHost delete all chassis by node name
func (c *OVNSbClient) DeleteChassisByHost(nodeName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	chassisList := make([]ovnsb.Chassis, 0)
	if err := c.ovsDbClient.WhereCache(func(chassis *ovnsb.Chassis) bool {
		return chassis.Hostname == nodeName || (chassis.ExternalIDs != nil && chassis.ExternalIDs["node"] == nodeName)
	}).List(ctx, &chassisList); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to list Chassis with host name=%s: %w", nodeName, err)
	}

	for _, chassis := range chassisList {
		klog.Infof("delete chassis: %+v", chassis)
		if err := c.DeleteChassis(chassis.Name); err != nil {
			err := fmt.Errorf("failed to delete chassis %s, %w", chassis.Name, err)
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *OVNSbClient) UpdateChassisTag(chassisName, nodeName string) error {
	chassis, err := c.GetChassis(chassisName, true)
	if err != nil {
		klog.Error(err)
		return err
	}
	if chassis == nil {
		err := fmt.Errorf("fail to get chassis by name=%s", chassisName)
		// restart kube-ovn-cni, chassis will be created
		klog.Error(err)
		return err
	}
	if chassis.ExternalIDs == nil || chassis.ExternalIDs["node"] != nodeName {
		externalIDs := make(map[string]string, len(chassis.ExternalIDs)+2)
		for k, v := range chassis.ExternalIDs {
			externalIDs[k] = v
		}
		externalIDs["vendor"] = util.CniTypeName
		// externalIDs["node"] = nodeName
		// not need filter chassis by node name if we use libovsdb
		chassis.ExternalIDs = externalIDs
		if err := c.UpdateChassis(chassis, &chassis.ExternalIDs); err != nil {
			klog.Error(err)
			return fmt.Errorf("failed to init chassis node %s: %w", nodeName, err)
		}
	}
	return nil
}

// GetKubeOvnChassisses return all chassis which vendor is kube-ovn
func (c *OVNSbClient) GetKubeOvnChassisses() (*[]ovnsb.Chassis, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	chassisList := make([]ovnsb.Chassis, 0)
	if err := c.ovsDbClient.WhereCache(func(chassis *ovnsb.Chassis) bool {
		if chassis.ExternalIDs != nil && chassis.ExternalIDs["vendor"] == util.CniTypeName {
			return true
		}
		return false
	}).List(ctx, &chassisList); err != nil {
		return nil, fmt.Errorf("failed to list Chassis with vendor=%s: %w", util.CniTypeName, err)
	}
	return &chassisList, nil
}
