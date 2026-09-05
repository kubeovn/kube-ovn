package daemon

import (
	"context"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) listVswitchBridges(needVendorFilter bool, filter func(*vswitch.Bridge) bool) ([]vswitch.Bridge, error) {
	if c.vswitchTables == nil {
		return c.vswitchClient.ListBridge(needVendorFilter, filter)
	}
	var rows []vswitch.Bridge
	err := c.vswitchTables.Table(&vswitch.Bridge{}).Filter(context.Background(), func(row *vswitch.Bridge) bool {
		if needVendorFilter && row.ExternalIDs[ovs.ExternalIDVendor] != util.CniTypeName {
			return false
		}
		return filter == nil || filter(row)
	}, &rows)
	return rows, err
}

func (c *Controller) listVswitchPorts(filter func(*vswitch.Port) bool) ([]vswitch.Port, error) {
	if c.vswitchTables == nil {
		return c.vswitchClient.ListPort(filter)
	}
	var rows []vswitch.Port
	err := c.vswitchTables.Table(&vswitch.Port{}).Filter(context.Background(), func(row *vswitch.Port) bool {
		return filter == nil || filter(row)
	}, &rows)
	return rows, err
}

func (c *Controller) listVswitchInterfaces(filter func(*vswitch.Interface) bool) ([]vswitch.Interface, error) {
	if c.vswitchTables == nil {
		return c.vswitchClient.ListInterface(filter)
	}
	var rows []vswitch.Interface
	err := c.vswitchTables.Table(&vswitch.Interface{}).Filter(context.Background(), func(row *vswitch.Interface) bool {
		return filter == nil || filter(row)
	}, &rows)
	return rows, err
}
