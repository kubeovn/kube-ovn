package controller

import (
	"context"
	"fmt"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func matchesExternalIDs(actual, expected map[string]string) bool {
	if len(actual) < len(expected) {
		return false
	}
	for key, value := range expected {
		if value == "" {
			if actual[key] == "" {
				return false
			}
			continue
		}
		if actual[key] != value {
			return false
		}
	}
	return true
}

func (c *Controller) listAddressSets(externalIDs map[string]string) ([]ovnnb.AddressSet, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ListAddressSets(externalIDs)
	}
	var rows []ovnnb.AddressSet
	err := c.OVNNbTables.Table(&ovnnb.AddressSet{}).Filter(context.Background(), func(row *ovnnb.AddressSet) bool {
		return matchesExternalIDs(row.ExternalIDs, externalIDs)
	}, &rows)
	return rows, err
}

func (c *Controller) listPortGroups(externalIDs map[string]string) ([]ovnnb.PortGroup, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ListPortGroups(externalIDs)
	}
	var rows []ovnnb.PortGroup
	err := c.OVNNbTables.Table(&ovnnb.PortGroup{}).Filter(context.Background(), func(row *ovnnb.PortGroup) bool {
		return matchesExternalIDs(row.ExternalIDs, externalIDs)
	}, &rows)
	return rows, err
}

func (c *Controller) getPortGroup(name string, ignoreNotFound bool) (*ovnnb.PortGroup, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.GetPortGroup(name, ignoreNotFound)
	}
	var rows []ovnnb.PortGroup
	err := c.OVNNbTables.Table(&ovnnb.PortGroup{}).Filter(context.Background(), func(row *ovnnb.PortGroup) bool {
		return row.Name == name
	}, &rows)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("not found port group %q", name)
	}
	if len(rows) > 1 {
		return nil, fmt.Errorf("more than one port group with same name %q", name)
	}
	return &rows[0], nil
}

func (c *Controller) listChassis() ([]ovnsb.Chassis, error) {
	if c.OVNSbTables == nil {
		rows, err := c.OVNSbClient.ListChassis()
		if err != nil || rows == nil {
			return nil, err
		}
		return *rows, nil
	}
	var rows []ovnsb.Chassis
	err := c.OVNSbTables.Table(&ovnsb.Chassis{}).List(context.Background(), &rows)
	return rows, err
}

func (c *Controller) listLogicalSwitchPorts(needVendorFilter bool, externalIDs map[string]string, filter func(*ovnnb.LogicalSwitchPort) bool) ([]ovnnb.LogicalSwitchPort, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ListLogicalSwitchPorts(needVendorFilter, externalIDs, filter)
	}
	var rows []ovnnb.LogicalSwitchPort
	err := c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).Filter(context.Background(), func(row *ovnnb.LogicalSwitchPort) bool {
		if needVendorFilter && row.ExternalIDs["vendor"] != util.CniTypeName {
			return false
		}
		return matchesExternalIDs(row.ExternalIDs, externalIDs) && (filter == nil || filter(row))
	}, &rows)
	return rows, err
}

func (c *Controller) getLogicalSwitchPort(name string, ignoreNotFound bool) (*ovnnb.LogicalSwitchPort, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.GetLogicalSwitchPort(name, ignoreNotFound)
	}
	var rows []ovnnb.LogicalSwitchPort
	err := c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).Filter(context.Background(), func(row *ovnnb.LogicalSwitchPort) bool {
		return row.Name == name
	}, &rows)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("not found logical switch port %q", name)
	}
	if len(rows) > 1 {
		return nil, fmt.Errorf("more than one logical switch port with same name %q", name)
	}
	return &rows[0], nil
}

func (c *Controller) listNormalLogicalSwitchPorts(needVendorFilter bool, externalIDs map[string]string) ([]ovnnb.LogicalSwitchPort, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ListNormalLogicalSwitchPorts(needVendorFilter, externalIDs)
	}
	return c.listLogicalSwitchPorts(needVendorFilter, externalIDs, func(row *ovnnb.LogicalSwitchPort) bool {
		return row.Type == ""
	})
}

func (c *Controller) listLogicalRouters(needVendorFilter bool, filter func(*ovnnb.LogicalRouter) bool) ([]ovnnb.LogicalRouter, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ListLogicalRouter(needVendorFilter, filter)
	}
	var rows []ovnnb.LogicalRouter
	err := c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Filter(context.Background(), func(row *ovnnb.LogicalRouter) bool {
		if needVendorFilter && row.ExternalIDs["vendor"] != util.CniTypeName {
			return false
		}
		return filter == nil || filter(row)
	}, &rows)
	return rows, err
}

func (c *Controller) listLogicalSwitches(needVendorFilter bool, filter func(*ovnnb.LogicalSwitch) bool) ([]ovnnb.LogicalSwitch, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ListLogicalSwitch(needVendorFilter, filter)
	}
	var rows []ovnnb.LogicalSwitch
	err := c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Filter(context.Background(), func(row *ovnnb.LogicalSwitch) bool {
		if needVendorFilter && row.ExternalIDs["vendor"] != util.CniTypeName {
			return false
		}
		return filter == nil || filter(row)
	}, &rows)
	return rows, err
}

func (c *Controller) getLogicalRouter(name string, ignoreNotFound bool) (*ovnnb.LogicalRouter, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.GetLogicalRouter(name, ignoreNotFound)
	}
	var rows []ovnnb.LogicalRouter
	err := c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Filter(context.Background(), func(row *ovnnb.LogicalRouter) bool {
		return row.Name == name
	}, &rows)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("not found logical router %q", name)
	}
	if len(rows) > 1 {
		return nil, fmt.Errorf("more than one logical router with same name %q", name)
	}
	return &rows[0], nil
}

func (c *Controller) getLogicalRouterPort(name string, ignoreNotFound bool) (*ovnnb.LogicalRouterPort, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.GetLogicalRouterPort(name, ignoreNotFound)
	}
	var rows []ovnnb.LogicalRouterPort
	err := c.OVNNbTables.Table(&ovnnb.LogicalRouterPort{}).Filter(context.Background(), func(row *ovnnb.LogicalRouterPort) bool {
		return row.Name == name
	}, &rows)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("not found logical router port %q", name)
	}
	if len(rows) > 1 {
		return nil, fmt.Errorf("more than one logical router port with same name %q", name)
	}
	return &rows[0], nil
}

func (c *Controller) getLogicalRouterPortByUUID(uuid string) (*ovnnb.LogicalRouterPort, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.GetLogicalRouterPortByUUID(uuid)
	}
	row := &ovnnb.LogicalRouterPort{UUID: uuid}
	if err := c.OVNNbTables.Table(&ovnnb.LogicalRouterPort{}).Get(context.Background(), row); err != nil {
		return nil, err
	}
	return row, nil
}

// logicalSwitchExists uses the generic table seam when controller wiring has
// supplied it. The legacy client fallback keeps unit fixtures and incremental
// migrations compatible while callers move away from domain-specific helpers.
func (c *Controller) logicalSwitchExists(name string) (bool, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.LogicalSwitchExists(name)
	}

	var rows []ovnnb.LogicalSwitch
	err := c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Filter(
		context.Background(),
		func(row *ovnnb.LogicalSwitch) bool { return row.Name == name },
		&rows,
	)
	if err != nil {
		return false, fmt.Errorf("list logical switch %q: %w", name, err)
	}
	return len(rows) > 0, nil
}
