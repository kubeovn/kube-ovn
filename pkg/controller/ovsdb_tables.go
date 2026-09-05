package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
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

func (c *Controller) portGroupExists(name string) (bool, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.PortGroupExists(name)
	}
	row, err := c.getPortGroup(name, true)
	return row != nil, err
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

func (c *Controller) logicalSwitchPortExists(name string) (bool, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.LogicalSwitchPortExists(name)
	}
	row, err := c.getLogicalSwitchPort(name, true)
	return row != nil, err
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

func (c *Controller) listLogicalSwitchNames(needVendorFilter bool, filter func(*ovnnb.LogicalSwitch) bool) ([]string, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ListLogicalSwitchNames(needVendorFilter, filter)
	}
	rows, err := c.listLogicalSwitches(needVendorFilter, filter)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return names, nil
}

func (c *Controller) listLogicalRouterNames(needVendorFilter bool, filter func(*ovnnb.LogicalRouter) bool) ([]string, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ListLogicalRouterNames(needVendorFilter, filter)
	}
	rows, err := c.listLogicalRouters(needVendorFilter, filter)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return names, nil
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

func (c *Controller) listLogicalRouterPolicies(lrName string, priority int, externalIDs map[string]string, ignoreExtIDEmptyValue bool) ([]*ovnnb.LogicalRouterPolicy, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ListLogicalRouterPolicies(lrName, priority, externalIDs, ignoreExtIDEmptyValue)
	}
	lr, err := c.getLogicalRouter(lrName, false)
	if err != nil {
		return nil, err
	}
	policyUUIDs := make(map[string]struct{}, len(lr.Policies))
	for _, uuid := range lr.Policies {
		policyUUIDs[uuid] = struct{}{}
	}
	var rows []*ovnnb.LogicalRouterPolicy
	err = c.OVNNbTables.Table(&ovnnb.LogicalRouterPolicy{}).Filter(context.Background(), func(row *ovnnb.LogicalRouterPolicy) bool {
		if _, ok := policyUUIDs[row.UUID]; !ok {
			return false
		}
		if priority >= 0 && row.Priority != priority {
			return false
		}
		return matchesExternalIDsWithEmptyValue(row.ExternalIDs, externalIDs, ignoreExtIDEmptyValue)
	}, &rows)
	return rows, err
}

func (c *Controller) getLogicalRouterPolicy(lrName string, priority int, match string, ignoreNotFound bool) ([]*ovnnb.LogicalRouterPolicy, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.GetLogicalRouterPolicy(lrName, priority, match, ignoreNotFound)
	}
	rows, err := c.listLogicalRouterPolicies(lrName, priority, nil, false)
	if err != nil {
		return nil, fmt.Errorf("get policy priority %d match %s in logical router %s: %w", priority, match, lrName, err)
	}
	filtered := rows[:0]
	for _, row := range rows {
		if row.Match == match {
			filtered = append(filtered, row)
		}
	}
	if len(filtered) == 0 && !ignoreNotFound {
		return nil, fmt.Errorf("not found policy priority %d match %s in logical router %s", priority, match, lrName)
	}
	return filtered, nil
}

func (c *Controller) getLogicalRouterPoliciesByExtID(lrName, key, value string) ([]*ovnnb.LogicalRouterPolicy, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.GetLogicalRouterPoliciesByExtID(lrName, key, value)
	}
	return c.listLogicalRouterPoliciesWithFilter(lrName, func(row *ovnnb.LogicalRouterPolicy) bool {
		actual, ok := row.ExternalIDs[key]
		return ok && actual == value
	})
}

func (c *Controller) listLogicalRouterPoliciesWithFilter(lrName string, filter func(*ovnnb.LogicalRouterPolicy) bool) ([]*ovnnb.LogicalRouterPolicy, error) {
	if c.OVNNbTables == nil {
		rows, err := c.OVNNbClient.ListLogicalRouterPolicies(lrName, -1, nil, false)
		if err != nil {
			return nil, err
		}
		filtered := rows[:0]
		for _, row := range rows {
			if filter == nil || filter(row) {
				filtered = append(filtered, row)
			}
		}
		return filtered, nil
	}
	lr, err := c.getLogicalRouter(lrName, false)
	if err != nil {
		return nil, err
	}
	policyUUIDs := make(map[string]struct{}, len(lr.Policies))
	for _, uuid := range lr.Policies {
		policyUUIDs[uuid] = struct{}{}
	}
	var rows []*ovnnb.LogicalRouterPolicy
	err = c.OVNNbTables.Table(&ovnnb.LogicalRouterPolicy{}).Filter(context.Background(), func(row *ovnnb.LogicalRouterPolicy) bool {
		if _, ok := policyUUIDs[row.UUID]; !ok {
			return false
		}
		return filter == nil || filter(row)
	}, &rows)
	return rows, err
}

func (c *Controller) listLogicalRouterStaticRoutes(lrName string, routeTable, policy *string, ipPrefix string, externalIDs map[string]string) ([]*ovnnb.LogicalRouterStaticRoute, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ListLogicalRouterStaticRoutes(lrName, routeTable, policy, ipPrefix, externalIDs)
	}
	lr, err := c.getLogicalRouter(lrName, false)
	if err != nil {
		return nil, err
	}
	routeUUIDs := make(map[string]struct{}, len(lr.StaticRoutes))
	for _, uuid := range lr.StaticRoutes {
		routeUUIDs[uuid] = struct{}{}
	}
	var rows []*ovnnb.LogicalRouterStaticRoute
	err = c.OVNNbTables.Table(&ovnnb.LogicalRouterStaticRoute{}).Filter(context.Background(), func(row *ovnnb.LogicalRouterStaticRoute) bool {
		if _, ok := routeUUIDs[row.UUID]; !ok {
			return false
		}
		if !matchesExternalIDsWithEmptyValue(row.ExternalIDs, externalIDs, true) {
			return false
		}
		if routeTable != nil && row.RouteTable != *routeTable {
			return false
		}
		if policy != nil {
			if row.Policy != nil {
				if *row.Policy != *policy {
					return false
				}
			} else if *policy != ovnnb.LogicalRouterStaticRoutePolicyDstIP {
				return false
			}
		}
		return ipPrefix == "" || row.IPPrefix == ipPrefix
	}, &rows)
	return rows, err
}

func (c *Controller) getLoadBalancer(name string, ignoreNotFound bool) (*ovnnb.LoadBalancer, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.GetLoadBalancer(name, ignoreNotFound)
	}
	var rows []ovnnb.LoadBalancer
	err := c.OVNNbTables.Table(&ovnnb.LoadBalancer{}).Filter(context.Background(), func(row *ovnnb.LoadBalancer) bool {
		return row.Name == name
	}, &rows)
	if err != nil {
		return nil, fmt.Errorf("failed to list load balancer %q: %w", name, err)
	}
	switch len(rows) {
	case 0:
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("not found load balancer %q", name)
	case 1:
		return &rows[0], nil
	default:
		return nil, fmt.Errorf("more than one load balancer with same name %q", name)
	}
}

func (c *Controller) listLoadBalancers(filter func(*ovnnb.LoadBalancer) bool) ([]ovnnb.LoadBalancer, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ListLoadBalancers(filter)
	}
	var rows []ovnnb.LoadBalancer
	err := c.OVNNbTables.Table(&ovnnb.LoadBalancer{}).Filter(context.Background(), func(row *ovnnb.LoadBalancer) bool {
		return filter == nil || filter(row)
	}, &rows)
	return rows, err
}

func (c *Controller) listLoadBalancerHealthChecks(filter func(*ovnnb.LoadBalancerHealthCheck) bool) ([]ovnnb.LoadBalancerHealthCheck, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ListLoadBalancerHealthChecks(filter)
	}
	var rows []ovnnb.LoadBalancerHealthCheck
	err := c.OVNNbTables.Table(&ovnnb.LoadBalancerHealthCheck{}).Filter(context.Background(), func(row *ovnnb.LoadBalancerHealthCheck) bool {
		return filter == nil || filter(row)
	}, &rows)
	return rows, err
}

func (c *Controller) getNATByUUID(uuid string) (*ovnnb.NAT, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.GetNATByUUID(uuid)
	}
	row := &ovnnb.NAT{UUID: uuid}
	if err := c.OVNNbTables.Table(&ovnnb.NAT{}).Get(context.Background(), row); err != nil {
		return nil, err
	}
	return row, nil
}

func (c *Controller) findBFD(externalIDs map[string]string) ([]ovnnb.BFD, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.FindBFD(externalIDs)
	}
	var rows []ovnnb.BFD
	err := c.OVNNbTables.Table(&ovnnb.BFD{}).Filter(context.Background(), func(row *ovnnb.BFD) bool {
		if len(row.ExternalIDs) == 0 && len(externalIDs) != 0 {
			return false
		}
		return matchesExternalIDsExact(row.ExternalIDs, externalIDs)
	}, &rows)
	return rows, err
}

func (c *Controller) natExists(lrName, natType, externalIP, logicalIP string) (bool, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.NatExists(lrName, natType, externalIP, logicalIP)
	}
	if lrName == "" {
		return false, errors.New("the logical router name is required")
	}
	if natType == ovnnb.NATTypeDNAT {
		return false, errors.New("does not support dnat for now")
	}
	if natType != "" && natType != ovnnb.NATTypeSNAT && natType != ovnnb.NATTypeDNATAndSNAT {
		return false, errors.New("nat type must be one of [ snat, dnat_and_snat ]")
	}
	if natType == ovnnb.NATTypeSNAT && logicalIP == "" {
		return false, fmt.Errorf("logical ip is required when nat type is %s", natType)
	}
	if (natType == ovnnb.NATTypeSNAT || natType == ovnnb.NATTypeDNATAndSNAT) && externalIP == "" {
		return false, fmt.Errorf("external ip is required when nat type is %s", natType)
	}
	lr, err := c.getLogicalRouter(lrName, false)
	if err != nil {
		return false, err
	}
	natUUIDs := make(map[string]struct{}, len(lr.Nat))
	for _, uuid := range lr.Nat {
		natUUIDs[uuid] = struct{}{}
	}
	var rows []ovnnb.NAT
	err = c.OVNNbTables.Table(&ovnnb.NAT{}).Filter(context.Background(), func(row *ovnnb.NAT) bool {
		if _, ok := natUUIDs[row.UUID]; !ok {
			return false
		}
		if natType == "" {
			return row.LogicalIP == logicalIP
		}
		if natType == ovnnb.NATTypeSNAT {
			return row.Type == natType && row.ExternalIP == externalIP && row.LogicalIP == logicalIP
		}
		if natType == ovnnb.NATTypeDNATAndSNAT {
			return row.Type == natType && row.ExternalIP == externalIP && (logicalIP == "" || row.LogicalIP == logicalIP)
		}
		return row.Type == natType && row.ExternalIP == externalIP
	}, &rows)
	if err != nil {
		return false, err
	}
	if len(rows) > 1 {
		return false, fmt.Errorf("more than one nat 'type %s external ip %s logical ip %s' in logical router %s", natType, externalIP, logicalIP, lrName)
	}
	return len(rows) == 1, nil
}

func (c *Controller) getChassis(name string, ignoreNotFound bool) (*ovnsb.Chassis, error) {
	if c.OVNSbTables == nil {
		return c.OVNSbClient.GetChassis(name, ignoreNotFound)
	}
	if name == "" {
		return nil, errors.New("chassis name is empty")
	}
	row := &ovnsb.Chassis{Name: name}
	if err := c.OVNSbTables.Table(&ovnsb.Chassis{}).Get(context.Background(), row); err != nil {
		if ignoreNotFound && errors.Is(err, compat.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get chassis %s: %w", name, err)
	}
	return row, nil
}

func (c *Controller) getChassisByHost(hostname string) (*ovnsb.Chassis, error) {
	if c.OVNSbTables == nil {
		return c.OVNSbClient.GetChassisByHost(hostname)
	}
	if hostname == "" {
		return nil, errors.New("failed to get Chassis with empty hostname")
	}
	var rows []ovnsb.Chassis
	if err := c.OVNSbTables.Table(&ovnsb.Chassis{}).Filter(context.Background(), func(row *ovnsb.Chassis) bool {
		return row.Hostname == hostname
	}, &rows); err != nil {
		return nil, fmt.Errorf("failed to list Chassis with hostname=%s: %w", hostname, err)
	}
	switch len(rows) {
	case 0:
		return nil, fmt.Errorf("failed to get Chassis with hostname=%s", hostname)
	case 1:
		return &rows[0], nil
	default:
		return nil, ovs.ErrOneNodeMultiChassis
	}
}

func (c *Controller) listKubeOvnChassises() ([]ovnsb.Chassis, error) {
	if c.OVNSbTables == nil {
		rows, err := c.OVNSbClient.GetKubeOvnChassises()
		if err != nil || rows == nil {
			return nil, err
		}
		return *rows, nil
	}
	var rows []ovnsb.Chassis
	err := c.OVNSbTables.Table(&ovnsb.Chassis{}).Filter(context.Background(), func(row *ovnsb.Chassis) bool {
		return row.ExternalIDs != nil && row.ExternalIDs["vendor"] == util.CniTypeName
	}, &rows)
	return rows, err
}

func matchesExternalIDsWithEmptyValue(actual, expected map[string]string, ignoreEmpty bool) bool {
	if len(actual) < len(expected) {
		return false
	}
	for key, value := range expected {
		if value == "" && ignoreEmpty {
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

func matchesExternalIDsExact(actual, expected map[string]string) bool {
	if len(actual) < len(expected) {
		return false
	}
	for key, value := range expected {
		actualValue, ok := actual[key]
		if !ok || actualValue != value {
			return false
		}
	}
	return true
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
