package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/client"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *ovnClient) InitChassisNodeTag(chassisName string, nodeName string) error {
	chassis, err := c.GetChassis(chassisName, true)
	if err != nil {
		klog.Error(err)
		return err
	}
	if chassis == nil {
		err := fmt.Errorf("faile to get chassis by name=%s", chassisName)
		klog.Error(err)
		return err
	}
	if chassis.ExternalIDs == nil || chassis.ExternalIDs["node"] != nodeName {
		chassis.ExternalIDs = map[string]string{
			"vendor": util.CniTypeName,
			"node":   nodeName,
		}
	}
	ops, err := c.Create(chassis)
	if err != nil {
		err := fmt.Errorf("failed to generate operations for Chassis creation with Hostname=%s and Name=%s: %v", nodeName, chassisName, err)
		klog.Error(err)
		return err
	}
	if err = c.Transact("chassis-add", ops); err != nil {
		err := fmt.Errorf("failed to create Chassis with  Hostname=%s and Name=%s: %v", nodeName, chassisName, err)
		klog.Error(err)
		return err
	}
	return nil
}

// GetKubeOvnChassisses return all chassis which vendor is kube-ovn
func (c *ovnClient) GetKubeOvnChassisses() (*[]ovnsb.Chassis, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	chassisList := make([]ovnsb.Chassis, 0)
	if err := c.ovsDbClient.WhereCache(func(chassis *ovnsb.Chassis) bool {
		if chassis.ExternalIDs != nil && chassis.ExternalIDs["vendor"] == util.CniTypeName {
			return true
		}
		return false
	}).List(ctx, &chassisList); err != nil {
		return nil, fmt.Errorf("failed to list Chassis with vendor=%s: %v", util.CniTypeName, err)
	}
	return &chassisList, nil
}

func (c *ovnClient) CreateChassis(chassisName, nodeName string) (*ovnsb.Chassis, error) {
	chassis, err := c.GetChassis(chassisName, true)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	if chassis != nil {
		return chassis, nil
	}

	chassis = &ovnsb.Chassis{
		Name:     chassisName,
		Hostname: nodeName,
		ExternalIDs: map[string]string{
			"vendor": util.CniTypeName,
		},
	}
	ops, err := c.Create(chassis)
	if err != nil {
		return nil, fmt.Errorf("failed to generate operations for Chassis creation with Hostname=%s and Name=%s: %v", nodeName, chassisName, err)
	}
	if err = c.Transact("chassis-add", ops); err != nil {
		return nil, fmt.Errorf("failed to create Chassis with  Hostname=%s and Name=%s: %v", nodeName, chassisName, err)
	}

	chassis, err = c.GetChassis(chassisName, true)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	if chassis == nil {
		err := fmt.Errorf("faile to get chassis by name=%s", chassisName)
		klog.Error(err)
		return nil, err
	}
	return chassis, nil
}

// DeleteChassis delete one chassis by name
func (c *ovnClient) DeleteChassis(chassisName string) error {
	chassis, err := c.GetChassis(chassisName, true)
	if err != nil {
		klog.Error(err)
		return err
	}
	if chassis == nil {
		return nil
	}
	ops, err := c.Where(chassis).Delete()
	if err != nil {
		return fmt.Errorf("failed to generate delete operations for chassis %s: %v", chassis.UUID, err)
	}
	if err = c.Transact("chassis-del", ops); err != nil {
		return fmt.Errorf("failed to delete chassis with with UUID %s: %v", chassis.UUID, err)
	}
	return nil
}

// GetChassis return south bound node chassis from cache
func (c *ovnClient) GetChassis(chassisName string, ignoreNotFound bool) (*ovnsb.Chassis, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	chassis := &ovnsb.Chassis{Name: chassisName}
	if err := c.Get(ctx, chassis); err != nil {
		if ignoreNotFound && err == client.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get south bound node chassis %s: %v", chassisName, err)
	}
	return chassis, nil
}

func (c *ovnClient) GetChassisByHost(nodeName string) (*ovnsb.Chassis, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	chassisList := make([]ovnsb.Chassis, 0)
	if err := c.ovsDbClient.WhereCache(func(chassis *ovnsb.Chassis) bool {
		return chassis.Hostname == nodeName
	}).List(ctx, &chassisList); err != nil {
		return nil, fmt.Errorf("failed to list Chassis with host name=%s: %v", nodeName, err)
	}
	if len(chassisList) == 0 {
		err := fmt.Errorf("failed to get Chassis with with host name=%s", nodeName)
		klog.Error(err)
		return nil, err
	}
	if len(chassisList) != 1 {
		err := fmt.Errorf("found more than one Chassis with with host name=%s", nodeName)
		klog.Error(err)
		return nil, err
	}
	return &chassisList[0], nil
}

func (c *ovnClient) GetChassisByTagNode(nodeName string) (*ovnsb.Chassis, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	chassisList := make([]ovnsb.Chassis, 0)
	if err := c.ovsDbClient.WhereCache(func(chassis *ovnsb.Chassis) bool {
		return chassis.ExternalIDs != nil && chassis.ExternalIDs["node"] == nodeName
	}).List(ctx, &chassisList); err != nil {
		return nil, fmt.Errorf("failed to list Chassis with host name=%s: %v", nodeName, err)
	}
	if len(chassisList) == 0 {
		err := fmt.Errorf("failed to get Chassis with with host name=%s", nodeName)
		klog.Error(err)
		return nil, err
	}
	if len(chassisList) != 1 {
		err := fmt.Errorf("found more than one Chassis with with host name=%s", nodeName)
		klog.Error(err)
		return nil, err
	}
	return &chassisList[0], nil
}

// DeleteChassisByNode delete all chassis by node name
func (c *ovnClient) DeleteChassisByNode(nodeName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	chassisList := make([]ovnsb.Chassis, 0)
	if err := c.ovsDbClient.WhereCache(func(chassis *ovnsb.Chassis) bool {
		return chassis.Hostname == nodeName || (chassis.ExternalIDs != nil && chassis.ExternalIDs["node"] == nodeName)
	}).List(ctx, &chassisList); err != nil {
		return fmt.Errorf("failed to list Chassis with host name=%s: %v", nodeName, err)
	}

	for _, chassis := range chassisList {
		if err := c.DeleteChassis(chassis.Name); err != nil {
			err := fmt.Errorf("failed to delete chassis %s, %v", chassis.Name, err)
			klog.Error(err)
			return err
		}
	}
	return nil
}
