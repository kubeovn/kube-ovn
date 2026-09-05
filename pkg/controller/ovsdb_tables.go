package controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// deleteAddressSets removes address sets selected by their names through the
// generic table facade. The legacy client remains the compatibility path for
// tests and callers that have not wired a TableProvider yet.
func (c *Controller) deleteAddressSets(names ...string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteAddressSet(names...)
	}
	if len(names) == 0 {
		return nil
	}

	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		wanted[name] = struct{}{}
	}
	var rows []ovnnb.AddressSet
	if err := c.OVNNbTables.Table(&ovnnb.AddressSet{}).Filter(
		context.Background(),
		func(row *ovnnb.AddressSet) bool {
			_, ok := wanted[row.Name]
			return ok
		},
		&rows,
	); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	selectors := make([]model.Model, len(rows))
	for i := range rows {
		selectors[i] = &rows[i]
	}
	return c.OVNNbTables.Table(&ovnnb.AddressSet{}).Delete(context.Background(), "as-del", selectors...)
}

// deletePortGroups removes port groups selected by their names through the
// generic table facade. Port membership is already represented by the row;
// no separate parent cleanup is needed for this operation.
func (c *Controller) deletePortGroups(names ...string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeletePortGroup(names...)
	}
	if len(names) == 0 {
		return nil
	}

	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		wanted[name] = struct{}{}
	}
	var rows []ovnnb.PortGroup
	if err := c.OVNNbTables.Table(&ovnnb.PortGroup{}).Filter(
		context.Background(),
		func(row *ovnnb.PortGroup) bool {
			_, ok := wanted[row.Name]
			return ok
		},
		&rows,
	); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	selectors := make([]model.Model, len(rows))
	for i := range rows {
		selectors[i] = &rows[i]
	}
	return c.OVNNbTables.Table(&ovnnb.PortGroup{}).Delete(context.Background(), "pg-del", selectors...)
}

func (c *Controller) deleteAddressSetsByExternalIDs(externalIDs map[string]string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteAddressSets(externalIDs)
	}
	// An empty selector is intentionally a no-op. Deleting every address set
	// from a reconcile path would be unsafe.
	if len(externalIDs) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.AddressSet{}).DeleteFilter(
		context.Background(), "ass-del", func(row *ovnnb.AddressSet) bool {
			return matchesExternalIDs(row.ExternalIDs, externalIDs)
		},
	)
}

// createAddressSet creates an address set through the generic table facade.
// Address-set creation is idempotent, matching the legacy client contract.
func (c *Controller) createAddressSet(name string, externalIDs map[string]string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.CreateAddressSet(name, externalIDs)
	}
	var rows []ovnnb.AddressSet
	err := c.OVNNbTables.Table(&ovnnb.AddressSet{}).Filter(
		context.Background(),
		func(row *ovnnb.AddressSet) bool { return row.Name == name },
		&rows,
	)
	if err != nil {
		return err
	}
	if len(rows) != 0 {
		return nil
	}
	finalExternalIDs := maps.Clone(externalIDs)
	if finalExternalIDs == nil {
		finalExternalIDs = make(map[string]string, 1)
	}
	finalExternalIDs["vendor"] = util.CniTypeName
	return c.OVNNbTables.Table(&ovnnb.AddressSet{}).Create(
		context.Background(), "as-add", &ovnnb.AddressSet{Name: name, ExternalIDs: finalExternalIDs},
	)
}

func (c *Controller) getAddressSet(name string, ignoreNotFound bool) (*ovnnb.AddressSet, error) {
	row := &ovnnb.AddressSet{Name: name}
	if err := c.OVNNbTables.Table(&ovnnb.AddressSet{}).Get(context.Background(), row); err != nil {
		if ignoreNotFound && errors.Is(err, compat.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return row, nil
}

func normalizeAddressSetAddresses(addresses []string) []string {
	unique := make(map[string]struct{}, len(addresses))
	for _, address := range addresses {
		if strings.ContainsRune(address, '/') {
			if _, network, err := net.ParseCIDR(address); err == nil {
				address = network.String()
			}
		}
		unique[address] = struct{}{}
	}
	result := slices.Collect(maps.Keys(unique))
	slices.Sort(result)
	return result
}

func equalAddressSetAddresses(actual, expected []string) bool {
	return slices.Equal(normalizeAddressSetAddresses(actual), normalizeAddressSetAddresses(expected))
}

// updateAddressSetAddresses reconciles the address set's complete address
// collection while preserving the legacy normalization and no-op behavior.
func (c *Controller) updateAddressSetAddresses(name string, addresses ...string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.AddressSetUpdateAddress(name, addresses...)
	}
	as, err := c.getAddressSet(name, false)
	if err != nil {
		return err
	}
	if equalAddressSetAddresses(as.Addresses, addresses) {
		return nil
	}
	as.Addresses = normalizeAddressSetAddresses(addresses)
	return c.OVNNbTables.Table(&ovnnb.AddressSet{}).Update(
		context.Background(), "as-update", as, as, &as.Addresses,
	)
}

// createPortGroup creates or updates a port group through the generic table
// facade, preserving the legacy external-ID reconciliation behavior.
func (c *Controller) createPortGroup(name string, externalIDs map[string]string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.CreatePortGroup(name, externalIDs)
	}
	pg, err := c.getPortGroup(name, true)
	if err != nil {
		return err
	}
	finalExternalIDs := maps.Clone(externalIDs)
	if finalExternalIDs == nil {
		finalExternalIDs = make(map[string]string, 1)
	}
	finalExternalIDs["vendor"] = util.CniTypeName
	table := c.OVNNbTables.Table(&ovnnb.PortGroup{})
	if pg != nil {
		if maps.Equal(pg.ExternalIDs, finalExternalIDs) {
			return nil
		}
		pg.ExternalIDs = finalExternalIDs
		return table.Update(context.Background(), "pg-update", pg, pg, &pg.ExternalIDs)
	}
	return table.Create(context.Background(), "pg-add", &ovnnb.PortGroup{Name: name, ExternalIDs: finalExternalIDs})
}

// createLogicalRouter creates a router if it is absent from the monitored
// cache. The operation is intentionally limited to the Logical_Router row;
// callers remain responsible for ports, policies, and other references.
func (c *Controller) createLogicalRouter(name string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.CreateLogicalRouter(name)
	}
	lr, err := c.getLogicalRouter(name, true)
	if err != nil {
		return err
	}
	if lr != nil {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Create(
		context.Background(), "lr-add", &ovnnb.LogicalRouter{
			Name:        name,
			ExternalIDs: map[string]string{"vendor": util.CniTypeName},
		},
	)
}

func (c *Controller) updateLogicalRouter(lr *ovnnb.LogicalRouter, fields ...any) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.UpdateLogicalRouter(lr, fields...)
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Update(context.Background(), "lr-update", lr, lr, fields...)
}

func (c *Controller) deleteLogicalRouter(name string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteLogicalRouter(name)
	}
	lr, err := c.getLogicalRouter(name, true)
	if err != nil {
		return err
	}
	if lr == nil {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Delete(context.Background(), "lr-del", lr)
}

func (c *Controller) updateLogicalRouterPortNetworks(name string, networks []string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.UpdateLogicalRouterPortNetworks(name, networks)
	}
	lrp, err := c.getLogicalRouterPort(name, false)
	if err != nil {
		return err
	}
	if slices.Equal(lrp.Networks, networks) {
		return nil
	}
	lrp.Networks = networks
	return c.OVNNbTables.Table(&ovnnb.LogicalRouterPort{}).Update(
		context.Background(), "lrp-update", lrp, lrp, &lrp.Networks,
	)
}

func (c *Controller) updateLogicalRouterPortOptions(name string, options map[string]string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.UpdateLogicalRouterPortOptions(name, options)
	}
	if len(options) == 0 {
		return nil
	}
	lrp, err := c.getLogicalRouterPort(name, false)
	if err != nil {
		return err
	}
	newOptions := maps.Clone(lrp.Options)
	for key, value := range options {
		if value == "" {
			delete(newOptions, key)
			continue
		}
		if newOptions == nil {
			newOptions = make(map[string]string)
		}
		newOptions[key] = value
	}
	if maps.Equal(newOptions, lrp.Options) {
		return nil
	}
	lrp.Options = newOptions
	return c.OVNNbTables.Table(&ovnnb.LogicalRouterPort{}).Update(
		context.Background(), "lrp-update", lrp, lrp, &lrp.Options,
	)
}

// deleteDHCPOptionsForPort removes per-port DHCP rows before the port is
// detached from its logical switch. The cleanup is best-effort for the same
// reason as the legacy client path: an orphaned DHCP row must not prevent the
// LSP reference from being removed.
func (c *Controller) deleteDHCPOptionsForPort(portName string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteDHCPOptionsForPort(portName)
	}
	if portName == "" {
		return errors.New("the port name is required")
	}
	return c.OVNNbTables.Table(&ovnnb.DHCPOptions{}).DeleteFilter(
		context.Background(), "dhcp-port-options-del", func(row *ovnnb.DHCPOptions) bool {
			return row.ExternalIDs[ovs.PortKey] == portName
		},
	)
}

// logicalSwitchPortParent returns the unique logical switch that owns an LSP
// reference. The external ID is preferred; the UUID scan handles legacy rows
// without a parent external ID and preserves the old client's validation.
func (c *Controller) logicalSwitchPortParent(lsp *ovnnb.LogicalSwitchPort) (*ovnnb.LogicalSwitch, error) {
	if lsp == nil {
		return nil, errors.New("logical switch port is nil")
	}
	if lsName := lsp.ExternalIDs[ovs.LogicalSwitchKey]; lsName != "" {
		return c.getLogicalSwitch(lsName, false)
	}
	rows, err := c.listLogicalSwitches(false, func(row *ovnnb.LogicalSwitch) bool {
		return slices.Contains(row.Ports, lsp.UUID)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list logical switches by LSP UUID %s: %w", lsp.UUID, err)
	}
	switch len(rows) {
	case 0:
		return nil, fmt.Errorf("no logical switch found for LSP %s", lsp.UUID)
	case 1:
		return &rows[0], nil
	default:
		names := make([]string, len(rows))
		for i := range rows {
			names[i] = rows[i].Name
		}
		return nil, fmt.Errorf("multiple logical switches found for LSP %s: %s", lsp.UUID, strings.Join(names, ", "))
	}
}

func (c *Controller) logicalRouterPortParent(lrp *ovnnb.LogicalRouterPort) (*ovnnb.LogicalRouter, error) {
	if lrp == nil {
		return nil, errors.New("logical router port is nil")
	}
	if lrName := lrp.ExternalIDs["lr"]; lrName != "" {
		return c.getLogicalRouter(lrName, false)
	}
	rows, err := c.listLogicalRouters(false, func(row *ovnnb.LogicalRouter) bool {
		return slices.Contains(row.Ports, lrp.UUID)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list logical routers by LRP UUID %s: %w", lrp.UUID, err)
	}
	switch len(rows) {
	case 0:
		return nil, fmt.Errorf("no logical router found for LRP %s", lrp.UUID)
	case 1:
		return &rows[0], nil
	default:
		names := make([]string, len(rows))
		for i := range rows {
			names[i] = rows[i].Name
		}
		return nil, fmt.Errorf("multiple logical routers found for LRP %s: %s", lrp.UUID, strings.Join(names, ", "))
	}
}

func (c *Controller) logicalSwitchPortDeleteOps(lsp *ovnnb.LogicalSwitchPort) ([]ovsdb.Operation, error) {
	parent, err := c.logicalSwitchPortParent(lsp)
	if err != nil {
		return nil, err
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).MutateOps(parent, model.Mutation{
		Field: &parent.Ports, Value: []string{lsp.UUID}, Mutator: ovsdb.MutateOperationDelete,
	})
}

func (c *Controller) logicalRouterPortDeleteOps(lrp *ovnnb.LogicalRouterPort) ([]ovsdb.Operation, error) {
	parent, err := c.logicalRouterPortParent(lrp)
	if err != nil {
		return nil, err
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).MutateOps(parent, model.Mutation{
		Field: &parent.Ports, Value: []string{lrp.UUID}, Mutator: ovsdb.MutateOperationDelete,
	})
}

func (c *Controller) deleteLogicalSwitchPort(name string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteLogicalSwitchPort(name)
	}
	lsp, err := c.getLogicalSwitchPort(name, true)
	if err != nil || lsp == nil {
		return err
	}
	if err = c.deleteDHCPOptionsForPort(name); err != nil {
		klog.Warningf("failed to delete per-port dhcp options for %s during LSP deletion: %v", name, err)
	}
	ops, err := c.logicalSwitchPortDeleteOps(lsp)
	if err != nil {
		return fmt.Errorf("generate operations for deleting logical switch port %s: %w", name, err)
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Transact(context.Background(), "lsp-del", ops...)
}

func (c *Controller) deleteLogicalSwitchPorts(externalIDs map[string]string, filter func(*ovnnb.LogicalSwitchPort) bool) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteLogicalSwitchPorts(externalIDs, filter)
	}
	rows, err := c.listLogicalSwitchPorts(false, externalIDs, filter)
	if err != nil {
		return fmt.Errorf("list switch ports: %w", err)
	}
	var operations []ovsdb.Operation
	for i := range rows {
		ops, opErr := c.logicalSwitchPortDeleteOps(&rows[i])
		if opErr != nil {
			return fmt.Errorf("generate operations for deleting logical switch port %s: %w", rows[i].Name, opErr)
		}
		operations = append(operations, ops...)
	}
	if len(operations) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Transact(context.Background(), "lsps-del", operations...)
}

func (c *Controller) deleteLogicalRouterPort(name string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteLogicalRouterPort(name)
	}
	lrp, err := c.getLogicalRouterPort(name, true)
	if err != nil || lrp == nil {
		return err
	}
	ops, err := c.logicalRouterPortDeleteOps(lrp)
	if err != nil {
		return fmt.Errorf("generate operations for deleting logical router port %s: %w", name, err)
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Transact(context.Background(), "lrp-del", ops...)
}

func (c *Controller) deleteLogicalRouterPorts(externalIDs map[string]string, filter func(*ovnnb.LogicalRouterPort) bool) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteLogicalRouterPorts(externalIDs, filter)
	}
	rows, err := c.listLogicalRouterPorts(externalIDs, filter)
	if err != nil {
		return fmt.Errorf("list logical router ports: %w", err)
	}
	var operations []ovsdb.Operation
	for i := range rows {
		ops, opErr := c.logicalRouterPortDeleteOps(&rows[i])
		if opErr != nil {
			return fmt.Errorf("generate operations for deleting logical router port %s: %w", rows[i].Name, opErr)
		}
		operations = append(operations, ops...)
	}
	if len(operations) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Transact(context.Background(), "lrps-del", operations...)
}

func (c *Controller) getHAChassisGroup(name string, ignoreNotFound bool) (*ovnnb.HAChassisGroup, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.GetHAChassisGroup(name, ignoreNotFound)
	}
	group := &ovnnb.HAChassisGroup{Name: name}
	if err := c.OVNNbTables.Table(&ovnnb.HAChassisGroup{}).Get(context.Background(), group); err != nil {
		if ignoreNotFound && errors.Is(err, compat.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get HA chassis group %q: %w", name, err)
	}
	return group, nil
}

// createHAChassisGroup reconciles both the group row and its HA_Chassis child
// rows in one transaction. The operation order intentionally mirrors the
// legacy helper so a controller retry remains idempotent.
func (c *Controller) createHAChassisGroup(name string, chassises []string, externalIDs map[string]string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.CreateHAChassisGroup(name, chassises, externalIDs)
	}
	group, err := c.getHAChassisGroup(name, true)
	if err != nil {
		return err
	}
	var operations []ovsdb.Operation
	if group == nil {
		group = &ovnnb.HAChassisGroup{
			UUID:        ovsclient.NamedUUID(),
			Name:        name,
			ExternalIDs: map[string]string{"vendor": util.CniTypeName},
		}
		maps.Insert(group.ExternalIDs, maps.All(externalIDs))
		createOps, createErr := c.OVNNbTables.Table(&ovnnb.HAChassisGroup{}).CreateOps(group)
		if createErr != nil {
			return createErr
		}
		operations = append(operations, createOps...)
	} else {
		group.ExternalIDs = map[string]string{"vendor": util.CniTypeName}
		maps.Insert(group.ExternalIDs, maps.All(externalIDs))
		updateOps, updateErr := c.OVNNbTables.Table(&ovnnb.HAChassisGroup{}).UpdateOps(group, group, &group.ExternalIDs)
		if updateErr != nil {
			return updateErr
		}
		operations = append(operations, updateOps...)
	}

	var existing []*ovnnb.HAChassis
	if len(group.HaChassis) != 0 {
		if err = c.OVNNbTables.Table(&ovnnb.HAChassis{}).Filter(context.Background(), func(row *ovnnb.HAChassis) bool {
			return slices.Contains(group.HaChassis, row.UUID)
		}, &existing); err != nil {
			return err
		}
	}
	priorityMap := make(map[string]int, len(chassises))
	for i, chassis := range chassises {
		priorityMap[chassis] = 100 - i
	}
	var removed []string
	for _, chassis := range existing {
		if priority, ok := priorityMap[chassis.ChassisName]; ok {
			delete(priorityMap, chassis.ChassisName)
			if chassis.Priority != priority {
				chassis.Priority = priority
				updateOps, updateErr := c.OVNNbTables.Table(&ovnnb.HAChassis{}).UpdateOps(chassis, chassis, &chassis.Priority)
				if updateErr != nil {
					return updateErr
				}
				operations = append(operations, updateOps...)
			}
			continue
		}
		removed = append(removed, chassis.UUID)
	}
	if len(removed) != 0 {
		deleteOps, deleteErr := c.OVNNbTables.Table(&ovnnb.HAChassisGroup{}).MutateOps(group, model.Mutation{
			Field: &group.HaChassis, Value: removed, Mutator: ovsdb.MutateOperationDelete,
		})
		if deleteErr != nil {
			return deleteErr
		}
		operations = append(operations, deleteOps...)
	}
	for chassis, priority := range priorityMap {
		row := &ovnnb.HAChassis{
			UUID:        ovsclient.NamedUUID(),
			ChassisName: chassis,
			Priority:    priority,
			ExternalIDs: map[string]string{"group": name, "vendor": util.CniTypeName},
		}
		createOps, createErr := c.OVNNbTables.Table(&ovnnb.HAChassis{}).CreateOps(row)
		if createErr != nil {
			return createErr
		}
		insertOps, insertErr := c.OVNNbTables.Table(&ovnnb.HAChassisGroup{}).MutateOps(group, model.Mutation{
			Field: &group.HaChassis, Value: []string{row.UUID}, Mutator: ovsdb.MutateOperationInsert,
		})
		if insertErr != nil {
			return insertErr
		}
		operations = append(operations, createOps...)
		operations = append(operations, insertOps...)
	}
	return c.OVNNbTables.Table(&ovnnb.HAChassisGroup{}).Transact(context.Background(), "ha-chassis-group-add", operations...)
}

func (c *Controller) deleteHAChassisGroup(name string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteHAChassisGroup(name)
	}
	group, err := c.getHAChassisGroup(name, true)
	if err != nil || group == nil {
		return err
	}
	ops, err := c.OVNNbTables.Table(&ovnnb.HAChassisGroup{}).DeleteOps(group)
	if err != nil {
		return err
	}
	return c.OVNNbTables.Table(&ovnnb.HAChassisGroup{}).Transact(context.Background(), "ha-chassis-group-del", ops...)
}

func (c *Controller) setLogicalRouterPortHAChassisGroup(lrpName, groupName string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.SetLogicalRouterPortHAChassisGroup(lrpName, groupName)
	}
	lrp, err := c.getLogicalRouterPort(lrpName, false)
	if err != nil {
		return err
	}
	group, err := c.getHAChassisGroup(groupName, false)
	if err != nil {
		return err
	}
	if lrp.HaChassisGroup != nil && *lrp.HaChassisGroup == group.UUID {
		return nil
	}
	lrp.HaChassisGroup = &group.UUID
	return c.OVNNbTables.Table(&ovnnb.LogicalRouterPort{}).Update(
		context.Background(), "lrp-update", lrp, lrp, &lrp.HaChassisGroup,
	)
}

func (c *Controller) deleteMeter(name string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteMeter(name)
	}
	meter := &ovnnb.Meter{Name: name}
	if err := c.OVNNbTables.Table(&ovnnb.Meter{}).Get(context.Background(), meter); err != nil {
		if errors.Is(err, compat.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("failed to get meter %s: %w", name, err)
	}
	operations, err := c.OVNNbTables.Table(&ovnnb.Meter{}).DeleteOps(meter)
	if err != nil {
		return fmt.Errorf("failed to build delete operations for meter %s: %w", name, err)
	}
	for _, bandUUID := range meter.Bands {
		bandOps, bandErr := c.OVNNbTables.Table(&ovnnb.MeterBand{}).DeleteOps(&ovnnb.MeterBand{UUID: bandUUID})
		if bandErr != nil {
			return fmt.Errorf("failed to remove meter band %s for %s: %w", bandUUID, name, bandErr)
		}
		operations = append(operations, bandOps...)
	}
	return c.OVNNbTables.Table(&ovnnb.Meter{}).Transact(context.Background(), "meter-del", operations...)
}

func (c *Controller) logicalRouterStaticRouteMutationOps(lr *ovnnb.LogicalRouter, uuids []string, mutator ovsdb.Mutator) ([]ovsdb.Operation, error) {
	if len(uuids) == 0 {
		return nil, nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).MutateOps(lr, model.Mutation{
		Field: &lr.StaticRoutes, Value: uuids, Mutator: mutator,
	})
}

func (c *Controller) addLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix string, bfdID *string, externalIDs map[string]string, nexthops ...string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.AddLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, bfdID, externalIDs, nexthops...)
	}
	if policy == "" {
		policy = ovnnb.LogicalRouterStaticRoutePolicyDstIP
	}
	lr, err := c.getLogicalRouter(lrName, false)
	if err != nil {
		return err
	}
	routes, err := c.listLogicalRouterStaticRoutes(lrName, &routeTable, &policy, ipPrefix, nil)
	if err != nil {
		return err
	}
	existing := make(map[string]struct{}, len(routes))
	var toDelete []string
	for _, route := range routes {
		if slices.Contains(nexthops, route.Nexthop) {
			existing[route.Nexthop] = struct{}{}
			continue
		}
		if route.BFD != nil && bfdID != nil && *route.BFD != *bfdID {
			continue
		}
		toDelete = append(toDelete, route.UUID)
	}
	slices.Sort(toDelete)
	if len(toDelete) != 0 {
		ops, opErr := c.logicalRouterStaticRouteMutationOps(lr, toDelete, ovsdb.MutateOperationDelete)
		if opErr != nil {
			return opErr
		}
		if err = c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Transact(context.Background(), "lr-route-del", ops...); err != nil {
			return fmt.Errorf("failed to delete static routes from logical router %s: %w", lrName, err)
		}
	}

	models := make([]model.Model, 0, len(nexthops))
	uuids := make([]string, 0, len(nexthops))
	for _, nexthop := range nexthops {
		if _, ok := existing[nexthop]; ok {
			continue
		}
		routePolicy := policy
		route := &ovnnb.LogicalRouterStaticRoute{
			UUID:        ovsclient.NamedUUID(),
			Policy:      &routePolicy,
			IPPrefix:    ipPrefix,
			Nexthop:     nexthop,
			RouteTable:  routeTable,
			ExternalIDs: externalIDs,
		}
		if bfdID != nil {
			route.BFD = bfdID
			route.Options = map[string]string{util.StaticRouteBfdEcmp: "true"}
		}
		models = append(models, route)
		uuids = append(uuids, route.UUID)
	}
	if len(models) == 0 {
		return nil
	}
	createOps, err := c.OVNNbTables.Table(&ovnnb.LogicalRouterStaticRoute{}).CreateOps(models...)
	if err != nil {
		return fmt.Errorf("generate operations for creating static routes: %w", err)
	}
	insertOps, err := c.logicalRouterStaticRouteMutationOps(lr, uuids, ovsdb.MutateOperationInsert)
	if err != nil {
		return fmt.Errorf("generate operations for adding static routes to logical router %s: %w", lrName, err)
	}
	operations := append(createOps, insertOps...)
	return c.OVNNbTables.Table(&ovnnb.LogicalRouterStaticRoute{}).Transact(context.Background(), "lr-routes-add", operations...)
}

func (c *Controller) deleteLogicalRouterStaticRoute(lrName string, routeTable, policy *string, ipPrefix, nexthop string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteLogicalRouterStaticRoute(lrName, routeTable, policy, ipPrefix, nexthop)
	}
	lr, err := c.getLogicalRouter(lrName, true)
	if err != nil || lr == nil {
		return err
	}
	if policy == nil || *policy == "" {
		policy = new(ovnnb.LogicalRouterStaticRoutePolicyDstIP)
	}
	routes, err := c.listLogicalRouterStaticRoutes(lrName, routeTable, policy, ipPrefix, nil)
	if err != nil {
		return err
	}
	uuids := make([]string, 0, len(routes))
	for _, route := range routes {
		if nexthop == "" || route.Nexthop == nexthop {
			uuids = append(uuids, route.UUID)
		}
	}
	slices.Sort(uuids)
	ops, err := c.logicalRouterStaticRouteMutationOps(lr, uuids, ovsdb.MutateOperationDelete)
	if err != nil {
		return fmt.Errorf("generate operations for removing static routes from logical router %s: %w", lrName, err)
	}
	if len(ops) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Transact(context.Background(), "lr-route-del", ops...)
}

func (c *Controller) batchDeleteLogicalRouterStaticRoutes(lrName string, requested []*ovnnb.LogicalRouterStaticRoute) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.BatchDeleteLogicalRouterStaticRoute(lrName, requested)
	}
	lr, err := c.getLogicalRouter(lrName, true)
	if err != nil || lr == nil {
		return err
	}
	targets := make(map[string]string, len(requested))
	for _, route := range requested {
		if route == nil {
			continue
		}
		policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
		if route.Policy != nil && *route.Policy != "" {
			policy = *route.Policy
		}
		targets[route.RouteTable+"\x00"+policy+"\x00"+route.IPPrefix] = route.Nexthop
	}
	routes, err := c.listLogicalRouterStaticRoutes(lrName, nil, nil, "", nil)
	if err != nil {
		return err
	}
	uuids := make([]string, 0, len(routes))
	for _, route := range routes {
		policy := ovnnb.LogicalRouterStaticRoutePolicyDstIP
		if route.Policy != nil && *route.Policy != "" {
			policy = *route.Policy
		}
		nexthop, ok := targets[route.RouteTable+"\x00"+policy+"\x00"+route.IPPrefix]
		if ok && (nexthop == "" || nexthop == route.Nexthop) {
			uuids = append(uuids, route.UUID)
		}
	}
	slices.Sort(uuids)
	ops, err := c.logicalRouterStaticRouteMutationOps(lr, uuids, ovsdb.MutateOperationDelete)
	if err != nil {
		return err
	}
	if len(ops) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Transact(context.Background(), "lr-route-del", ops...)
}

func (c *Controller) createLogicalRouterPort(lrName, lrpName, mac string, networks []string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.CreateLogicalRouterPort(lrName, lrpName, mac, networks)
	}
	if existing, err := c.getLogicalRouterPort(lrpName, true); err != nil {
		return err
	} else if existing != nil {
		return nil
	}
	if mac == "" {
		mac = util.GenerateMac()
	}
	row := &ovnnb.LogicalRouterPort{
		UUID:        ovsclient.NamedUUID(),
		Name:        lrpName,
		MAC:         mac,
		Networks:    networks,
		ExternalIDs: map[string]string{"lr": lrName, "vendor": util.CniTypeName},
	}
	parent, err := c.getLogicalRouter(lrName, false)
	if err != nil {
		return err
	}
	createOps, err := c.OVNNbTables.Table(&ovnnb.LogicalRouterPort{}).CreateOps(row)
	if err != nil {
		return fmt.Errorf("generate operations for creating logical router port %s: %w", lrpName, err)
	}
	insertOps, err := c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).MutateOps(parent, model.Mutation{
		Field: &parent.Ports, Value: []string{row.UUID}, Mutator: ovsdb.MutateOperationInsert,
	})
	if err != nil {
		return fmt.Errorf("generate operations for adding logical router port %s to logical router %s: %w", lrpName, lrName, err)
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouterPort{}).Transact(context.Background(), "lrp-add", append(createOps, insertOps...)...)
}

func (c *Controller) createBareLogicalSwitchPort(lsName, lspName, ip, mac string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.CreateBareLogicalSwitchPort(lsName, lspName, ip, mac)
	}
	if existing, err := c.getLogicalSwitchPort(lspName, true); err != nil {
		return err
	} else if existing != nil {
		return nil
	}
	ipList := strings.Split(ip, ",")
	addresses := make([]string, 0, len(ipList)+1)
	addresses = append(addresses, mac)
	addresses = append(addresses, ipList...)
	row := &ovnnb.LogicalSwitchPort{
		UUID:        ovsclient.NamedUUID(),
		Name:        lspName,
		Addresses:   []string{strings.TrimSpace(strings.Join(addresses, " "))},
		ExternalIDs: map[string]string{ovs.LogicalSwitchKey: lsName, "vendor": util.CniTypeName},
	}
	parent, err := c.getLogicalSwitch(lsName, false)
	if err != nil {
		return err
	}
	createOps, err := c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).CreateOps(row)
	if err != nil {
		return fmt.Errorf("generate operations for creating logical switch port %s: %w", lspName, err)
	}
	insertOps, err := c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).MutateOps(parent, model.Mutation{
		Field: &parent.Ports, Value: []string{row.UUID}, Mutator: ovsdb.MutateOperationInsert,
	})
	if err != nil {
		return fmt.Errorf("generate operations for adding logical switch port %s to logical switch %s: %w", lspName, lsName, err)
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).Transact(context.Background(), "lsp-add", append(createOps, insertOps...)...)
}

func (c *Controller) createVirtualLogicalSwitchPort(lspName, lsName, ip string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.CreateVirtualLogicalSwitchPort(lspName, lsName, ip)
	}
	if existing, err := c.getLogicalSwitchPort(lspName, true); err != nil {
		return err
	} else if existing != nil {
		return nil
	}
	parent, err := c.getLogicalSwitch(lsName, false)
	if err != nil {
		return err
	}
	row := &ovnnb.LogicalSwitchPort{
		UUID:        ovsclient.NamedUUID(),
		Name:        lspName,
		Type:        "virtual",
		Options:     map[string]string{"virtual-ip": ip},
		ExternalIDs: map[string]string{ovs.LogicalSwitchKey: lsName, "vendor": util.CniTypeName},
	}
	createOps, err := c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).CreateOps(row)
	if err != nil {
		return err
	}
	insertOps, err := c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).MutateOps(parent, model.Mutation{
		Field: &parent.Ports, Value: []string{row.UUID}, Mutator: ovsdb.MutateOperationInsert,
	})
	if err != nil {
		return err
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).Transact(context.Background(), "lsp-add", append(createOps, insertOps...)...)
}

func (c *Controller) createVirtualLogicalSwitchPorts(lsName string, ips ...string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.CreateVirtualLogicalSwitchPorts(lsName, ips...)
	}
	parent, err := c.getLogicalSwitch(lsName, false)
	if err != nil {
		return err
	}
	var createOps, insertOps []ovsdb.Operation
	for _, ip := range ips {
		lspName := fmt.Sprintf("%s-vip-%s", lsName, ip)
		if existing, getErr := c.getLogicalSwitchPort(lspName, true); getErr != nil {
			return getErr
		} else if existing != nil {
			continue
		}
		row := &ovnnb.LogicalSwitchPort{
			UUID:        ovsclient.NamedUUID(),
			Name:        lspName,
			Type:        "virtual",
			Options:     map[string]string{"virtual-ip": ip},
			ExternalIDs: map[string]string{ovs.LogicalSwitchKey: lsName, "vendor": util.CniTypeName},
		}
		ops, createErr := c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).CreateOps(row)
		if createErr != nil {
			return createErr
		}
		refOps, refErr := c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).MutateOps(parent, model.Mutation{
			Field: &parent.Ports, Value: []string{row.UUID}, Mutator: ovsdb.MutateOperationInsert,
		})
		if refErr != nil {
			return refErr
		}
		createOps = append(createOps, ops...)
		insertOps = append(insertOps, refOps...)
	}
	if len(createOps) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).Transact(context.Background(), "lsp-add", append(createOps, insertOps...)...)
}

func (c *Controller) createLocalnetLogicalSwitchPort(lsName, lspName, provider, cidrBlock string, vlanID int) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.CreateLocalnetLogicalSwitchPort(lsName, lspName, provider, cidrBlock, vlanID)
	}
	externalIDs := make(map[string]string)
	if cidrBlock != "" {
		ipv4CIDR, ipv6CIDR := util.SplitStringIP(cidrBlock)
		if ipv4CIDR != "" {
			externalIDs["ipv4_network"] = ipv4CIDR
		}
		if ipv6CIDR != "" {
			externalIDs["ipv6_network"] = ipv6CIDR
		}
	}
	externalIDs[ovs.LogicalSwitchKey] = lsName
	externalIDs["vendor"] = util.CniTypeName
	options := map[string]string{"network_name": provider}
	if existing, err := c.getLogicalSwitchPort(lspName, true); err != nil {
		return err
	} else if existing != nil {
		if maps.Equal(existing.ExternalIDs, externalIDs) && maps.Equal(existing.Options, options) {
			return nil
		}
		existing.ExternalIDs = externalIDs
		existing.Options = options
		return c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).Update(
			context.Background(), "lsp-update", existing, existing, &existing.ExternalIDs, &existing.Options,
		)
	}
	parent, err := c.getLogicalSwitch(lsName, false)
	if err != nil {
		return err
	}
	row := &ovnnb.LogicalSwitchPort{
		UUID:        ovsclient.NamedUUID(),
		Name:        lspName,
		Type:        "localnet",
		Addresses:   []string{"unknown"},
		Options:     options,
		ExternalIDs: externalIDs,
	}
	if vlanID > 0 && vlanID < 4096 {
		row.Tag = new(vlanID)
	}
	createOps, err := c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).CreateOps(row)
	if err != nil {
		return err
	}
	insertOps, err := c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).MutateOps(parent, model.Mutation{
		Field: &parent.Ports, Value: []string{row.UUID}, Mutator: ovsdb.MutateOperationInsert,
	})
	if err != nil {
		return err
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).Transact(context.Background(), "lsp-add", append(createOps, insertOps...)...)
}

func (c *Controller) updatePortGroupPorts(name string, operation ovsdb.Mutator, portNames ...string) error {
	if c.OVNNbTables == nil {
		switch operation {
		case ovsdb.MutateOperationInsert:
			return c.OVNNbClient.PortGroupAddPorts(name, portNames...)
		case ovsdb.MutateOperationDelete:
			return c.OVNNbClient.PortGroupRemovePorts(name, portNames...)
		default:
			return fmt.Errorf("unsupported port group mutation %q", operation)
		}
	}
	if len(portNames) == 0 {
		return nil
	}
	pg, err := c.getPortGroup(name, false)
	if err != nil {
		return err
	}
	uuidSet := make(map[string]struct{}, len(portNames))
	for _, portName := range portNames {
		lsp, err := c.getLogicalSwitchPort(portName, true)
		if err != nil {
			return err
		}
		if lsp != nil {
			uuidSet[lsp.UUID] = struct{}{}
		}
	}
	uuids := slices.Collect(maps.Keys(uuidSet))
	slices.Sort(uuids)
	if len(uuids) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.PortGroup{}).Mutate(
		context.Background(), "pg-ports-update", pg,
		model.Mutation{Field: &pg.Ports, Value: uuids, Mutator: operation},
	)
}

// setPortGroupPorts reconciles the complete PortGroup membership set. LSP
// names are resolved to UUIDs because the OVSDB column stores references.
func (c *Controller) setPortGroupPorts(name string, portNames []string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.PortGroupSetPorts(name, portNames)
	}
	pg, err := c.getPortGroup(name, false)
	if err != nil {
		return err
	}
	expected := make(map[string]struct{}, len(portNames))
	for _, portName := range portNames {
		lsp, err := c.getLogicalSwitchPort(portName, true)
		if err != nil {
			return err
		}
		if lsp != nil {
			expected[lsp.UUID] = struct{}{}
		}
	}
	actual := make(map[string]struct{}, len(pg.Ports))
	for _, uuid := range pg.Ports {
		actual[uuid] = struct{}{}
	}
	toAdd := make([]string, 0, len(expected))
	for uuid := range expected {
		if _, ok := actual[uuid]; !ok {
			toAdd = append(toAdd, uuid)
		}
	}
	toDelete := make([]string, 0, len(actual))
	for uuid := range actual {
		if _, ok := expected[uuid]; !ok {
			toDelete = append(toDelete, uuid)
		}
	}
	if len(toAdd) == 0 && len(toDelete) == 0 {
		return nil
	}
	slices.Sort(toAdd)
	slices.Sort(toDelete)
	mutations := make([]model.Mutation, 0, 2)
	if len(toAdd) != 0 {
		mutations = append(mutations, model.Mutation{
			Field: &pg.Ports, Value: toAdd, Mutator: ovsdb.MutateOperationInsert,
		})
	}
	if len(toDelete) != 0 {
		mutations = append(mutations, model.Mutation{
			Field: &pg.Ports, Value: toDelete, Mutator: ovsdb.MutateOperationDelete,
		})
	}
	return c.OVNNbTables.Table(&ovnnb.PortGroup{}).Mutate(
		context.Background(), "pg-ports-update", pg, mutations...,
	)
}

func (c *Controller) updateLogicalSwitchPortWith(name string, legacy func() error, update func(*ovnnb.LogicalSwitchPort) ([]any, error)) error {
	if c.OVNNbTables == nil {
		return legacy()
	}
	lsp, err := c.getLogicalSwitchPort(name, false)
	if err != nil {
		return err
	}
	fields, err := update(lsp)
	if err != nil {
		return err
	}
	if len(fields) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).Update(
		context.Background(), "lsp-update", lsp, lsp, fields...,
	)
}

func (c *Controller) updateLogicalSwitchPortOptionsWith(name string, legacy func() error, update func(map[string]string) map[string]string) error {
	return c.updateLogicalSwitchPortWith(name, legacy, func(lsp *ovnnb.LogicalSwitchPort) ([]any, error) {
		options := update(maps.Clone(lsp.Options))
		if maps.Equal(options, lsp.Options) {
			return nil, nil
		}
		lsp.Options = options
		return []any{&lsp.Options}, nil
	})
}

func (c *Controller) setLogicalSwitchPortVirtualParents(lsName, parents string, ips ...string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.SetLogicalSwitchPortVirtualParents(lsName, parents, ips...)
	}
	for _, ip := range ips {
		lspName := fmt.Sprintf("%s-vip-%s", lsName, ip)
		if err := c.updateLogicalSwitchPortOptionsWith(lspName, nil, func(options map[string]string) map[string]string {
			if parents == "" {
				delete(options, "virtual-parents")
				return options
			}
			if options == nil {
				options = make(map[string]string, 1)
			}
			options["virtual-parents"] = parents
			return options
		}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) setVirtualLogicalSwitchPortVirtualParents(name, parents string) error {
	return c.updateLogicalSwitchPortOptionsWith(name, func() error {
		return c.OVNNbClient.SetVirtualLogicalSwitchPortVirtualParents(name, parents)
	}, func(options map[string]string) map[string]string {
		if parents == "" {
			delete(options, "virtual-parents")
			return options
		}
		if options == nil {
			options = make(map[string]string, 1)
		}
		options["virtual-parents"] = parents
		return options
	})
}

func (c *Controller) setLogicalSwitchPortArpProxy(name string, enabled bool) error {
	return c.updateLogicalSwitchPortOptionsWith(name, func() error {
		return c.OVNNbClient.SetLogicalSwitchPortArpProxy(name, enabled)
	}, func(options map[string]string) map[string]string {
		if !enabled {
			delete(options, "arp_proxy")
			return options
		}
		if options == nil {
			options = make(map[string]string, 1)
		}
		options["arp_proxy"] = strconv.FormatBool(enabled)
		return options
	})
}

func (c *Controller) enableLogicalSwitchPortLayer2Forward(name string) error {
	return c.updateLogicalSwitchPortWith(name, func() error {
		return c.OVNNbClient.EnablePortLayer2forward(name)
	}, func(lsp *ovnnb.LogicalSwitchPort) ([]any, error) {
		if slices.Contains(lsp.Addresses, "unknown") {
			return nil, nil
		}
		lsp.Addresses = append(lsp.Addresses, "unknown")
		return []any{&lsp.Addresses}, nil
	})
}

func (c *Controller) setLogicalSwitchPortActivationStrategy(name, chassis string) error {
	requestedChassis := fmt.Sprintf("%s,%s", chassis, chassis)
	return c.updateLogicalSwitchPortOptionsWith(name, func() error {
		return c.OVNNbClient.SetLogicalSwitchPortActivationStrategy(name, chassis)
	}, func(options map[string]string) map[string]string {
		if options == nil {
			options = make(map[string]string, 2)
		}
		delete(options, "requested-chassis")
		delete(options, "activation-strategy")
		options["requested-chassis"] = requestedChassis
		options["activation-strategy"] = "rarp"
		return options
	})
}

func (c *Controller) setLogicalSwitchPortMigrateOptions(name, source, target string) error {
	if source == "" || target == "" {
		return fmt.Errorf("src and target node can not be empty on migrator port %s", name)
	}
	if source == target {
		return fmt.Errorf("src and target node can not be the same on migrator port %s", name)
	}
	if c.OVNNbTables == nil {
		return c.OVNNbClient.SetLogicalSwitchPortMigrateOptions(name, source, target)
	}
	lsp, err := c.getLogicalSwitchPort(name, false)
	if err != nil {
		return err
	}
	requestedChassis := fmt.Sprintf("%s,%s", source, target)
	if lsp.Options != nil && lsp.Options["requested-chassis"] == requestedChassis {
		return nil
	}
	options := maps.Clone(lsp.Options)
	if options == nil {
		options = make(map[string]string, 2)
	}
	options["requested-chassis"] = requestedChassis
	options["activation-strategy"] = "rarp"
	lsp.Options = options
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).Update(
		context.Background(), "lsp-update", lsp, lsp, &lsp.Options,
	)
}

func (c *Controller) resetLogicalSwitchPortMigrateOptions(name, source, target string, migratedFail bool) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ResetLogicalSwitchPortMigrateOptions(name, source, target, migratedFail)
	}
	lsp, err := c.getLogicalSwitchPort(name, false)
	if err != nil {
		return err
	}
	if lsp.Options == nil {
		return nil
	}
	if _, ok := lsp.Options["requested-chassis"]; !ok {
		return nil
	}
	options := maps.Clone(lsp.Options)
	if migratedFail {
		options["requested-chassis"] = source
	} else {
		options["requested-chassis"] = target
	}
	delete(options, "activation-strategy")
	lsp.Options = options
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).Update(
		context.Background(), "lsp-update", lsp, lsp, &lsp.Options,
	)
}

func (c *Controller) cleanLogicalSwitchPortMigrateOptions(name string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.CleanLogicalSwitchPortMigrateOptions(name)
	}
	lsp, err := c.getLogicalSwitchPort(name, true)
	if err != nil || lsp == nil || lsp.Options == nil {
		return err
	}
	if _, ok := lsp.Options["requested-chassis"]; !ok {
		return nil
	}
	options := maps.Clone(lsp.Options)
	delete(options, "requested-chassis")
	delete(options, "activation-strategy")
	lsp.Options = options
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).Update(
		context.Background(), "lsp-update", lsp, lsp, &lsp.Options,
	)
}

func (c *Controller) setLogicalSwitchPortSecurity(enabled bool, name, mac, ips, vips string) error {
	return c.updateLogicalSwitchPortWith(name, func() error {
		return c.OVNNbClient.SetLogicalSwitchPortSecurity(enabled, name, mac, ips, vips)
	}, func(lsp *ovnnb.LogicalSwitchPort) ([]any, error) {
		lsp.PortSecurity = nil
		if enabled {
			ipList := strings.Split(ips, ",")
			vipList := strings.Split(vips, ",")
			addresses := make([]string, 0, len(ipList)+len(vipList)+1)
			addresses = append(addresses, mac)
			addresses = append(addresses, ipList...)
			if vips != "" {
				addresses = append(addresses, vipList...)
			}
			lsp.PortSecurity = []string{strings.TrimSpace(strings.Join(addresses, " "))}
		}

		externalIDs := maps.Clone(lsp.ExternalIDs)
		if externalIDs == nil {
			externalIDs = make(map[string]string)
		}
		if vips != "" {
			externalIDs["vips"] = vips
			externalIDs["attach-vips"] = "true"
		} else {
			delete(externalIDs, "vips")
			delete(externalIDs, "attach-vips")
		}
		lsp.ExternalIDs = externalIDs
		return []any{&lsp.PortSecurity, &lsp.ExternalIDs}, nil
	})
}

func (c *Controller) deleteBFD(uuid string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteBFD(uuid)
	}
	return c.OVNNbTables.Table(&ovnnb.BFD{}).Delete(
		context.Background(), "bfd-del", &ovnnb.BFD{UUID: uuid},
	)
}

func (c *Controller) deleteBFDByDestination(logicalPort, destination string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteBFDByDstIP(logicalPort, destination)
	}
	var rows []ovnnb.BFD
	if err := c.OVNNbTables.Table(&ovnnb.BFD{}).Filter(
		context.Background(),
		func(row *ovnnb.BFD) bool {
			return row.LogicalPort == logicalPort && (destination == "" || row.DstIP == destination)
		},
		&rows,
	); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	selectors := make([]model.Model, len(rows))
	for i := range rows {
		selectors[i] = &rows[i]
	}
	return c.OVNNbTables.Table(&ovnnb.BFD{}).Delete(context.Background(), "bfd-del", selectors...)
}

func (c *Controller) createBFD(logicalPort, destination string, minRx, minTx, detectMult int, externalIDs map[string]string) (*ovnnb.BFD, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.CreateBFD(logicalPort, destination, minRx, minTx, detectMult, externalIDs)
	}
	var rows []ovnnb.BFD
	table := c.OVNNbTables.Table(&ovnnb.BFD{})
	if err := table.Filter(context.Background(), func(row *ovnnb.BFD) bool {
		return row.LogicalPort == logicalPort && row.DstIP == destination
	}, &rows); err != nil {
		return nil, fmt.Errorf("failed to list BFD with logical_port=%s and dst_ip=%s: %w", logicalPort, destination, err)
	}
	if len(rows) != 0 {
		return &rows[0], nil
	}
	row := &ovnnb.BFD{
		LogicalPort: logicalPort,
		DstIP:       destination,
		MinRx:       new(minRx),
		MinTx:       new(minTx),
		DetectMult:  new(detectMult),
		ExternalIDs: externalIDs,
	}
	if err := table.Create(context.Background(), "bfd-add", row); err != nil {
		return nil, fmt.Errorf("failed to create BFD with logical_port=%s and dst_ip=%s: %w", logicalPort, destination, err)
	}
	rows = rows[:0]
	if err := table.Filter(context.Background(), func(candidate *ovnnb.BFD) bool {
		return candidate.LogicalPort == logicalPort && candidate.DstIP == destination
	}, &rows); err != nil {
		return nil, fmt.Errorf("failed to list BFD after creation: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("BFD with logical_port=%s and dst_ip=%s not found after creation", logicalPort, destination)
	}
	return &rows[0], nil
}

func (c *Controller) deleteLogicalSwitch(name string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteLogicalSwitch(name)
	}
	var rows []ovnnb.LogicalSwitch
	if err := c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Filter(
		context.Background(),
		func(row *ovnnb.LogicalSwitch) bool { return row.Name == name },
		&rows,
	); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	if len(rows) > 1 {
		return fmt.Errorf("more than one logical switch with same name %q", name)
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Delete(context.Background(), "ls-del", &rows[0])
}

func (c *Controller) updateLogicalRouterPolicy(policy *ovnnb.LogicalRouterPolicy, fields ...any) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.UpdateLogicalRouterPolicy(policy, fields...)
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouterPolicy{}).Update(
		context.Background(), "lr-policy-update", policy, policy, fields...,
	)
}

func (c *Controller) deleteLogicalRouterPolicy(lrName string, priority int, match string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteLogicalRouterPolicy(lrName, priority, match)
	}
	policies, err := c.listLogicalRouterPolicies(lrName, priority, nil, false)
	if err != nil {
		return err
	}
	uuids := make([]string, 0, len(policies))
	for _, policy := range policies {
		if policy.Match == match {
			uuids = append(uuids, policy.UUID)
		}
	}
	if len(uuids) == 0 {
		return nil
	}
	lr, err := c.getLogicalRouter(lrName, false)
	if err != nil {
		return err
	}
	slices.Sort(uuids)
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Mutate(
		context.Background(), "lr-policy-del", lr,
		model.Mutation{Field: &lr.Policies, Value: uuids, Mutator: ovsdb.MutateOperationDelete},
	)
}

func (c *Controller) deleteLogicalRouterPolicyByUUID(lrName, uuid string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteLogicalRouterPolicyByUUID(lrName, uuid)
	}
	if uuid == "" {
		return nil
	}
	lr, err := c.getLogicalRouter(lrName, false)
	if err != nil {
		return err
	}
	if !slices.Contains(lr.Policies, uuid) {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Mutate(
		context.Background(), "lr-policy-del", lr,
		model.Mutation{Field: &lr.Policies, Value: []string{uuid}, Mutator: ovsdb.MutateOperationDelete},
	)
}

func (c *Controller) deleteLogicalRouterPolicies(lrName string, priority int, externalIDs map[string]string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteLogicalRouterPolicies(lrName, priority, externalIDs)
	}
	policies, err := c.listLogicalRouterPolicies(lrName, priority, externalIDs, false)
	if err != nil {
		return err
	}
	if len(policies) == 0 {
		return nil
	}
	lr, err := c.getLogicalRouter(lrName, false)
	if err != nil {
		return err
	}
	uuids := make([]string, 0, len(policies))
	for _, policy := range policies {
		uuids = append(uuids, policy.UUID)
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Mutate(
		context.Background(), "lr-policies-del", lr,
		model.Mutation{Field: &lr.Policies, Value: uuids, Mutator: ovsdb.MutateOperationDelete},
	)
}

func (c *Controller) deleteLogicalRouterPolicyByNexthop(lrName string, priority int, nexthop string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteLogicalRouterPolicyByNexthop(lrName, priority, nexthop)
	}
	policies, err := c.listLogicalRouterPoliciesWithFilter(lrName, func(policy *ovnnb.LogicalRouterPolicy) bool {
		if policy.Priority != priority {
			return false
		}
		return (policy.Nexthop != nil && *policy.Nexthop == nexthop) || slices.Contains(policy.Nexthops, nexthop)
	})
	if err != nil {
		return err
	}
	if len(policies) == 0 {
		return nil
	}
	lr, err := c.getLogicalRouter(lrName, false)
	if err != nil {
		return err
	}
	uuids := make([]string, 0, len(policies))
	for _, policy := range policies {
		uuids = append(uuids, policy.UUID)
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Mutate(
		context.Background(), "lr-policy-del", lr,
		model.Mutation{Field: &lr.Policies, Value: uuids, Mutator: ovsdb.MutateOperationDelete},
	)
}

func (c *Controller) deleteNats(lrName, natType, logicalIP string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteNats(lrName, natType, logicalIP)
	}
	nats, err := c.listNATs(lrName, natType, logicalIP, nil)
	if err != nil {
		return err
	}
	if len(nats) == 0 {
		return nil
	}
	lr, err := c.getLogicalRouter(lrName, false)
	if err != nil {
		return err
	}
	uuids := make([]string, 0, len(nats))
	for _, nat := range nats {
		uuids = append(uuids, nat.UUID)
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Mutate(
		context.Background(), "nats-del", lr,
		model.Mutation{Field: &lr.Nat, Value: uuids, Mutator: ovsdb.MutateOperationDelete},
	)
}

func (c *Controller) deleteNat(lrName, natType, externalIP, logicalIP string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteNat(lrName, natType, externalIP, logicalIP)
	}
	nats, err := c.listNATs(lrName, natType, logicalIP, nil)
	if err != nil {
		return err
	}
	matched := make([]*ovnnb.NAT, 0, len(nats))
	for _, nat := range nats {
		if nat.ExternalIP == externalIP {
			matched = append(matched, nat)
		}
	}
	if len(matched) == 0 {
		return nil
	}
	if len(matched) > 1 {
		return fmt.Errorf("more than one nat type %s external ip %s logical ip %s in logical router %s", natType, externalIP, logicalIP, lrName)
	}
	lr, err := c.getLogicalRouter(lrName, false)
	if err != nil {
		return err
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Mutate(
		context.Background(), "lr-nat-del", lr,
		model.Mutation{Field: &lr.Nat, Value: []string{matched[0].UUID}, Mutator: ovsdb.MutateOperationDelete},
	)
}

// addNat creates a NAT row and adds it to the owning logical router. The
// dnat_and_snat path deliberately replaces an existing EIP rule to preserve
// the swap behavior of the legacy client.
func (c *Controller) addNat(lrName, natType, externalIP, logicalIP, logicalMac, port string, options map[string]string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.AddNat(lrName, natType, externalIP, logicalIP, logicalMac, port, options)
	}
	if lrName == "" {
		return errors.New("the logical router name is required")
	}
	if natType != ovnnb.NATTypeSNAT && natType != ovnnb.NATTypeDNATAndSNAT {
		return errors.New("nat type must be one of [ snat, dnat_and_snat ]")
	}
	if natType == ovnnb.NATTypeSNAT && (externalIP == "" || logicalIP == "") {
		return fmt.Errorf("external ip and logical ip are required when nat type is %s", natType)
	}
	if natType == ovnnb.NATTypeDNATAndSNAT && externalIP == "" {
		return fmt.Errorf("external ip is required when nat type is %s", natType)
	}
	if natType == ovnnb.NATTypeDNATAndSNAT {
		if err := c.deleteNat(lrName, natType, externalIP, ""); err != nil {
			return err
		}
	} else {
		exists, err := c.natExists(lrName, natType, externalIP, logicalIP)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}
	}
	lr, err := c.getLogicalRouter(lrName, false)
	if err != nil {
		return err
	}
	row := &ovnnb.NAT{
		UUID:       ovsclient.NamedUUID(),
		Type:       natType,
		ExternalIP: externalIP,
		LogicalIP:  logicalIP,
		Options:    maps.Clone(options),
	}
	if logicalMac != "" {
		row.ExternalMAC = new(logicalMac)
	}
	if port != "" {
		row.LogicalPort = new(port)
	}
	createOps, err := c.OVNNbTables.Table(&ovnnb.NAT{}).CreateOps(row)
	if err != nil {
		return fmt.Errorf("generate operations for creating nat: %w", err)
	}
	parentOps, err := c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).MutateOps(lr, model.Mutation{
		Field: &lr.Nat, Value: []string{row.UUID}, Mutator: ovsdb.MutateOperationInsert,
	})
	if err != nil {
		return fmt.Errorf("generate operations for adding nat to logical router %s: %w", lrName, err)
	}
	return c.OVNNbTables.Table(&ovnnb.NAT{}).Transact(
		context.Background(), "lr-nats-add", append(createOps, parentOps...)...,
	)
}

// setLoadBalancerOption updates one option on a load balancer without
// replacing options managed by another reconcile path. The legacy callback is
// retained for callers that have not wired a TableProvider yet.
func (c *Controller) setLoadBalancerOption(name, key, value string, legacy func() error) error {
	if c.OVNNbTables == nil {
		return legacy()
	}
	lb, err := c.getLoadBalancer(name, false)
	if err != nil {
		return err
	}
	if lb.Options != nil && lb.Options[key] == value {
		return nil
	}
	options := maps.Clone(lb.Options)
	if options == nil {
		options = make(map[string]string, 1)
	}
	options[key] = value
	lb.Options = options
	return c.OVNNbTables.Table(&ovnnb.LoadBalancer{}).Update(
		context.Background(), "lb-update", lb, lb, &lb.Options,
	)
}

func (c *Controller) setLoadBalancerAffinityTimeout(name string, timeout int) error {
	value := strconv.Itoa(timeout)
	return c.setLoadBalancerOption(name, "affinity_timeout", value, func() error {
		return c.OVNNbClient.SetLoadBalancerAffinityTimeout(name, timeout)
	})
}

func (c *Controller) setLoadBalancerPreferLocalBackend(name string, enabled bool) error {
	value := strconv.FormatBool(enabled)
	return c.setLoadBalancerOption(name, "prefer_local_backend", value, func() error {
		return c.OVNNbClient.SetLoadBalancerPreferLocalBackend(name, enabled)
	})
}

func (c *Controller) setLoadBalancerCtFlush(name string, enabled bool) error {
	value := strconv.FormatBool(enabled)
	return c.setLoadBalancerOption(name, "ct_flush", value, func() error {
		return c.OVNNbClient.SetLoadBalancerCtFlush(name, enabled)
	})
}

func (c *Controller) getLogicalSwitch(name string, ignoreNotFound bool) (*ovnnb.LogicalSwitch, error) {
	if c.OVNNbTables == nil {
		rows, err := c.OVNNbClient.ListLogicalSwitch(false, func(row *ovnnb.LogicalSwitch) bool {
			return row.Name == name
		})
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			if ignoreNotFound {
				return nil, nil
			}
			return nil, fmt.Errorf("not found logical switch %q", name)
		}
		if len(rows) > 1 {
			return nil, fmt.Errorf("more than one logical switch with same name %q", name)
		}
		return &rows[0], nil
	}
	var rows []ovnnb.LogicalSwitch
	if err := c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Filter(
		context.Background(),
		func(row *ovnnb.LogicalSwitch) bool { return row.Name == name },
		&rows,
	); err != nil {
		return nil, err
	}
	switch len(rows) {
	case 0:
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("not found logical switch %q", name)
	case 1:
		return &rows[0], nil
	default:
		return nil, fmt.Errorf("more than one logical switch with same name %q", name)
	}
}

func (c *Controller) getNBGlobal() (*ovnnb.NBGlobal, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.GetNbGlobal()
	}
	var rows []ovnnb.NBGlobal
	if err := c.OVNNbTables.Table(&ovnnb.NBGlobal{}).Filter(
		context.Background(), func(*ovnnb.NBGlobal) bool { return true }, &rows,
	); err != nil {
		return nil, err
	}
	switch len(rows) {
	case 0:
		return nil, fmt.Errorf("not found NB_Global")
	case 1:
		return &rows[0], nil
	default:
		return nil, fmt.Errorf("more than one NB_Global row")
	}
}

func (c *Controller) setNBGlobalOption(key, value string, present bool, legacy func() error) error {
	if c.OVNNbTables == nil {
		return legacy()
	}
	nbGlobal, err := c.getNBGlobal()
	if err != nil {
		return err
	}
	options := maps.Clone(nbGlobal.Options)
	if present {
		if options == nil {
			options = make(map[string]string, 1)
		}
		if options[key] == value {
			return nil
		}
		options[key] = value
	} else {
		if _, ok := options[key]; !ok {
			return nil
		}
		delete(options, key)
	}
	nbGlobal.Options = options
	return c.OVNNbTables.Table(&ovnnb.NBGlobal{}).Update(
		context.Background(), "nb-global-update", nbGlobal, nbGlobal, &nbGlobal.Options,
	)
}

func (c *Controller) setNBGlobalIPSec(enabled bool, legacy func() error) error {
	if c.OVNNbTables == nil {
		return legacy()
	}
	nbGlobal, err := c.getNBGlobal()
	if err != nil {
		return err
	}
	if nbGlobal.Ipsec == enabled {
		return nil
	}
	nbGlobal.Ipsec = enabled
	return c.OVNNbTables.Table(&ovnnb.NBGlobal{}).Update(
		context.Background(), "nb-global-update", nbGlobal, nbGlobal, &nbGlobal.Ipsec,
	)
}

func (c *Controller) loadBalancerUUIDs(names ...string) ([]string, error) {
	uuids := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		lb, err := c.getLoadBalancer(name, true)
		if err != nil {
			return nil, err
		}
		if lb == nil {
			continue
		}
		if _, ok := seen[lb.UUID]; ok {
			continue
		}
		seen[lb.UUID] = struct{}{}
		uuids = append(uuids, lb.UUID)
	}
	return uuids, nil
}

func (c *Controller) updateLogicalSwitchLoadBalancers(name string, operation ovsdb.Mutator, loadBalancerNames ...string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.LogicalSwitchUpdateLoadBalancers(name, operation, loadBalancerNames...)
	}
	if len(loadBalancerNames) == 0 {
		return nil
	}
	ls, err := c.getLogicalSwitch(name, false)
	if err != nil {
		return err
	}
	lbUUIDs, err := c.loadBalancerUUIDs(loadBalancerNames...)
	if err != nil {
		return err
	}
	if len(lbUUIDs) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Mutate(
		context.Background(), "ls-lb-update", ls,
		model.Mutation{Field: &ls.LoadBalancer, Value: lbUUIDs, Mutator: operation},
	)
}

func (c *Controller) updateLogicalRouterLoadBalancers(name string, operation ovsdb.Mutator, loadBalancerNames ...string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.LogicalRouterUpdateLoadBalancers(name, operation, loadBalancerNames...)
	}
	if len(loadBalancerNames) == 0 {
		return nil
	}
	lr, err := c.getLogicalRouter(name, false)
	if err != nil {
		return err
	}
	lbUUIDs, err := c.loadBalancerUUIDs(loadBalancerNames...)
	if err != nil {
		return err
	}
	if len(lbUUIDs) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Mutate(
		context.Background(), "lr-lb-update", lr,
		model.Mutation{Field: &lr.LoadBalancer, Value: lbUUIDs, Mutator: operation},
	)
}

func (c *Controller) updateLogicalSwitchOtherConfig(name string, operation ovsdb.Mutator, otherConfig map[string]string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.LogicalSwitchUpdateOtherConfig(name, operation, otherConfig)
	}
	if len(otherConfig) == 0 {
		return nil
	}
	ls, err := c.getLogicalSwitch(name, false)
	if err != nil {
		return err
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Mutate(
		context.Background(), "ls-other-config-update", ls,
		model.Mutation{Field: &ls.OtherConfig, Value: otherConfig, Mutator: operation},
	)
}

const localExternalVIPKeyPrefix = "kube-ovn.io/local-external-vip/"

func (c *Controller) setLoadBalancerExternalTrafficLocal(name, vip, vipNodeLSP string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.SetLoadBalancerVIPExternalTrafficLocal(name, vip, vipNodeLSP)
	}
	lb, err := c.getLoadBalancer(name, false)
	if err != nil {
		return err
	}
	key := localExternalVIPKeyPrefix + vip
	mutations := make([]model.Mutation, 0, 2)
	if vipNodeLSP != "" {
		if lb.ExternalIDs[key] == vipNodeLSP {
			return nil
		}
		if oldValue, ok := lb.ExternalIDs[key]; ok {
			mutations = append(mutations, model.Mutation{
				Field: &lb.ExternalIDs, Value: map[string]string{key: oldValue}, Mutator: ovsdb.MutateOperationDelete,
			})
		}
		mutations = append(mutations, model.Mutation{
			Field: &lb.ExternalIDs, Value: map[string]string{key: vipNodeLSP}, Mutator: ovsdb.MutateOperationInsert,
		})
	} else {
		oldValue, ok := lb.ExternalIDs[key]
		if !ok {
			return nil
		}
		mutations = append(mutations, model.Mutation{
			Field: &lb.ExternalIDs, Value: map[string]string{key: oldValue}, Mutator: ovsdb.MutateOperationDelete,
		})
	}
	return c.OVNNbTables.Table(&ovnnb.LoadBalancer{}).Mutate(
		context.Background(), "lb-update-external-vip", lb, mutations...,
	)
}

func (c *Controller) removePortFromPortGroups(name string, portGroupNames ...string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.RemovePortFromPortGroups(name, portGroupNames...)
	}
	lsp, err := c.getLogicalSwitchPort(name, true)
	if err != nil {
		return err
	}
	if lsp == nil {
		return nil
	}
	portGroups := make([]ovnnb.PortGroup, 0, len(portGroupNames))
	if len(portGroupNames) == 0 {
		portGroups, err = c.listPortGroups(nil)
	} else {
		for _, portGroupName := range portGroupNames {
			pg, getErr := c.getPortGroup(portGroupName, true)
			if getErr != nil {
				return getErr
			}
			if pg != nil {
				portGroups = append(portGroups, *pg)
			}
		}
	}
	if err != nil {
		return err
	}
	var operations []ovsdb.Operation
	table := c.OVNNbTables.Table(&ovnnb.PortGroup{})
	for i := range portGroups {
		pg := &portGroups[i]
		if !slices.Contains(pg.Ports, lsp.UUID) {
			continue
		}
		ops, mutateErr := table.MutateOps(pg, model.Mutation{
			Field: &pg.Ports, Value: []string{lsp.UUID}, Mutator: ovsdb.MutateOperationDelete,
		})
		if mutateErr != nil {
			return mutateErr
		}
		operations = append(operations, ops...)
	}
	if len(operations) == 0 {
		return nil
	}
	return table.Transact(context.Background(), "pg-update", operations...)
}

// setLogicalSwitchPortExternalIDs merges external IDs into the current row so
// independent controller workers do not erase keys owned by each other.
func (c *Controller) setLogicalSwitchPortExternalIDs(name string, externalIDs map[string]string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.SetLogicalSwitchPortExternalIDs(name, externalIDs)
	}
	lsp, err := c.getLogicalSwitchPort(name, false)
	if err != nil {
		return err
	}
	merged := maps.Clone(lsp.ExternalIDs)
	if merged == nil {
		merged = make(map[string]string, len(externalIDs))
	}
	maps.Copy(merged, externalIDs)
	if maps.Equal(merged, lsp.ExternalIDs) {
		return nil
	}
	lsp.ExternalIDs = merged
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).Update(
		context.Background(), "lsp-update", lsp, lsp, &lsp.ExternalIDs,
	)
}

func (c *Controller) setLogicalSwitchPortVlanTag(name string, vlanID int) error {
	if vlanID < 0 || vlanID > 4095 {
		return fmt.Errorf("invalid vlan id %d", vlanID)
	}
	if c.OVNNbTables == nil {
		return c.OVNNbClient.SetLogicalSwitchPortVlanTag(name, vlanID)
	}
	lsp, err := c.getLogicalSwitchPort(name, false)
	if err != nil {
		return err
	}
	if lsp.Tag != nil && *lsp.Tag == vlanID {
		return nil
	}
	var tag *int
	if vlanID != 0 {
		tag = new(vlanID)
	}
	lsp.Tag = tag
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).Update(
		context.Background(), "lsp-update", lsp, lsp, &lsp.Tag,
	)
}

func (c *Controller) deleteChassis(name string) error {
	if c.OVNSbTables == nil {
		return c.OVNSbClient.DeleteChassis(name)
	}
	chassis, err := c.getChassis(name, true)
	if err != nil {
		return err
	}
	if chassis == nil {
		return nil
	}
	return c.OVNSbTables.Table(&ovnsb.Chassis{}).Delete(context.Background(), "chassis-del", chassis)
}

func (c *Controller) deleteChassisByHost(hostname string) error {
	if c.OVNSbTables == nil {
		return c.OVNSbClient.DeleteChassisByHost(hostname)
	}
	var rows []ovnsb.Chassis
	if err := c.OVNSbTables.Table(&ovnsb.Chassis{}).Filter(context.Background(), func(row *ovnsb.Chassis) bool {
		return row.Hostname == hostname || row.ExternalIDs["node"] == hostname
	}, &rows); err != nil {
		return fmt.Errorf("failed to list Chassis with hostname=%s: %w", hostname, err)
	}
	if len(rows) == 0 {
		return nil
	}
	selectors := make([]model.Model, len(rows))
	for i := range rows {
		selectors[i] = &rows[i]
	}
	return c.OVNSbTables.Table(&ovnsb.Chassis{}).Delete(context.Background(), "chassis-del", selectors...)
}

func (c *Controller) updateChassisTag(name, nodeName string) error {
	if c.OVNSbTables == nil {
		return c.OVNSbClient.UpdateChassisTag(name, nodeName)
	}
	chassis, err := c.getChassis(name, true)
	if err != nil {
		return err
	}
	if chassis == nil {
		return fmt.Errorf("fail to get chassis by name=%s", name)
	}
	if chassis.ExternalIDs != nil && chassis.ExternalIDs["node"] == nodeName {
		return nil
	}
	externalIDs := maps.Clone(chassis.ExternalIDs)
	if externalIDs == nil {
		externalIDs = make(map[string]string, 1)
	}
	externalIDs["vendor"] = util.CniTypeName
	chassis.ExternalIDs = externalIDs
	return c.OVNSbTables.Table(&ovnsb.Chassis{}).Update(
		context.Background(), "chassis-update", chassis, chassis, &chassis.ExternalIDs,
	)
}

// createLoadBalancer creates a load balancer when absent. Selection fields
// and protocol are part of the row insert, so later service reconciliation
// can continue using the existing mutation helpers.
func (c *Controller) createLoadBalancer(name, protocol string, selectFields ...string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.CreateLoadBalancer(name, protocol, selectFields...)
	}
	lb, err := c.getLoadBalancer(name, true)
	if err != nil {
		return err
	}
	if lb != nil {
		return nil
	}
	row := &ovnnb.LoadBalancer{
		Name:        name,
		ExternalIDs: map[string]string{"vendor": util.CniTypeName},
		Protocol:    &protocol,
	}
	if len(selectFields) != 0 {
		row.SelectionFields = selectFields
	}
	return c.OVNNbTables.Table(&ovnnb.LoadBalancer{}).Create(context.Background(), "lb-add", row)
}

func (c *Controller) deleteLoadBalancers(filter func(*ovnnb.LoadBalancer) bool) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteLoadBalancers(filter)
	}
	return c.OVNNbTables.Table(&ovnnb.LoadBalancer{}).DeleteFilter(
		context.Background(), "lb-del", func(row *ovnnb.LoadBalancer) bool {
			return filter == nil || filter(row)
		},
	)
}

func (c *Controller) deleteLoadBalancerHealthChecks(filter func(*ovnnb.LoadBalancerHealthCheck) bool) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteLoadBalancerHealthChecks(filter)
	}
	return c.OVNNbTables.Table(&ovnnb.LoadBalancerHealthCheck{}).DeleteFilter(
		context.Background(), "lbhc-del", func(row *ovnnb.LoadBalancerHealthCheck) bool {
			return filter == nil || filter(row)
		},
	)
}

// addLoadBalancerVIP adds or replaces one VIP entry on a load balancer.
func (c *Controller) addLoadBalancerVIP(lbName, vip string, backends ...string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.LoadBalancerAddVip(lbName, vip, backends...)
	}
	lb, err := c.getLoadBalancer(lbName, false)
	if err != nil {
		return err
	}
	slices.Sort(backends)
	value := strings.Join(backends, ",")
	if lb.Vips[vip] == value {
		return nil
	}
	mutations := make([]model.Mutation, 0, 2)
	if oldValue, ok := lb.Vips[vip]; ok {
		mutations = append(mutations, model.Mutation{
			Field: &lb.Vips, Value: map[string]string{vip: oldValue}, Mutator: ovsdb.MutateOperationDelete,
		})
	}
	mutations = append(mutations, model.Mutation{
		Field: &lb.Vips, Value: map[string]string{vip: value}, Mutator: ovsdb.MutateOperationInsert,
	})
	return c.OVNNbTables.Table(&ovnnb.LoadBalancer{}).Mutate(
		context.Background(), "lb-add", lb, mutations...,
	)
}

// updateLoadBalancerIPPortMapping reconciles the backend-to-LSP map for one VIP.
// Entries still referenced by another VIP are retained.
func (c *Controller) updateLoadBalancerIPPortMapping(lbName, vip string, mappings map[string]string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.LoadBalancerUpdateIPPortMapping(lbName, vip, mappings)
	}
	lb, err := c.getLoadBalancer(lbName, false)
	if err != nil {
		return err
	}
	newBackendIPs := make(map[string]bool, len(mappings))
	for ip := range mappings {
		newBackendIPs[strings.Trim(ip, "[]")] = true
	}
	toDelete := make(map[string]string)
	toInsert := make(map[string]string)
	for mappingKey, mappingValue := range lb.IPPortMappings {
		cleanKey := strings.Trim(mappingKey, "[]")
		if newBackendIPs[cleanKey] {
			continue
		}
		stillUsed := false
		for otherVIP, backends := range lb.Vips {
			if otherVIP == vip {
				continue
			}
			for backend := range strings.SplitSeq(backends, ",") {
				backendIP, _, splitErr := net.SplitHostPort(backend)
				if splitErr == nil && backendIP == cleanKey {
					stillUsed = true
					break
				}
			}
			if stillUsed {
				break
			}
		}
		if !stillUsed {
			toDelete[mappingKey] = mappingValue
		}
	}
	for ip, lsp := range mappings {
		cleanIP := strings.Trim(ip, "[]")
		existingKey, existingLSP, found := "", "", false
		if value, ok := lb.IPPortMappings[ip]; ok {
			existingKey, existingLSP, found = ip, value, true
		} else {
			for key, value := range lb.IPPortMappings {
				if strings.Trim(key, "[]") == cleanIP {
					existingKey, existingLSP, found = key, value, true
					break
				}
			}
		}
		if found {
			if existingLSP == lsp {
				continue
			}
			toDelete[existingKey] = existingLSP
		}
		toInsert[ip] = lsp
	}
	mutations := make([]model.Mutation, 0, 2)
	if len(toDelete) != 0 {
		mutations = append(mutations, model.Mutation{
			Field: &lb.IPPortMappings, Value: toDelete, Mutator: ovsdb.MutateOperationDelete,
		})
	}
	if len(toInsert) != 0 {
		mutations = append(mutations, model.Mutation{
			Field: &lb.IPPortMappings, Value: toInsert, Mutator: ovsdb.MutateOperationInsert,
		})
	}
	if len(mutations) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LoadBalancer{}).Mutate(
		context.Background(), "lb-update", lb, mutations...,
	)
}

// deleteLoadBalancerIPPortMapping removes mappings whose backend IP is no
// longer referenced by another VIP on the same load balancer.
func (c *Controller) deleteLoadBalancerIPPortMapping(lbName, vip string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.LoadBalancerDeleteIPPortMapping(lbName, vip)
	}
	lb, err := c.getLoadBalancer(lbName, true)
	if err != nil || lb == nil || len(lb.IPPortMappings) == 0 {
		return err
	}
	backends, ok := lb.Vips[vip]
	if !ok {
		return nil
	}
	targets := make(map[string]struct{})
	for backend := range strings.SplitSeq(backends, ",") {
		ip, _, splitErr := net.SplitHostPort(backend)
		if splitErr == nil {
			targets[ip] = struct{}{}
		}
	}
	toDelete := make(map[string]string)
	for target := range targets {
		used := false
		for otherVIP, otherBackends := range lb.Vips {
			if otherVIP == vip {
				continue
			}
			for backend := range strings.SplitSeq(otherBackends, ",") {
				ip, _, splitErr := net.SplitHostPort(backend)
				if splitErr == nil && ip == target {
					used = true
					break
				}
			}
			if used {
				break
			}
		}
		if !used {
			for key, value := range lb.IPPortMappings {
				if strings.Trim(key, "[]") == target {
					toDelete[key] = value
				}
			}
		}
	}
	if len(toDelete) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LoadBalancer{}).Mutate(
		context.Background(), "lb-del", lb,
		model.Mutation{Field: &lb.IPPortMappings, Value: toDelete, Mutator: ovsdb.MutateOperationDelete},
	)
}

// addLoadBalancerHealthCheck creates a health-check row and atomically adds its
// UUID to the parent load balancer. The mapping update retains legacy behavior.
func (c *Controller) addLoadBalancerHealthCheck(lbName, vip string, ignoreHealthCheck bool, ipPortMapping, externalIDs map[string]string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.LoadBalancerAddHealthCheck(lbName, vip, ignoreHealthCheck, ipPortMapping, externalIDs)
	}
	if err := c.updateLoadBalancerIPPortMapping(lbName, vip, ipPortMapping); err != nil {
		return err
	}
	if ignoreHealthCheck {
		return nil
	}
	lb, err := c.getLoadBalancer(lbName, false)
	if err != nil {
		return err
	}
	var checks []ovnnb.LoadBalancerHealthCheck
	if err := c.OVNNbTables.Table(&ovnnb.LoadBalancerHealthCheck{}).Filter(
		context.Background(), func(row *ovnnb.LoadBalancerHealthCheck) bool {
			return slices.Contains(lb.HealthCheck, row.UUID) && row.Vip == vip
		}, &checks,
	); err != nil {
		return err
	}
	if len(checks) != 0 {
		return nil
	}
	row := &ovnnb.LoadBalancerHealthCheck{
		UUID:        ovsclient.NamedUUID(),
		ExternalIDs: maps.Clone(externalIDs),
		Options: map[string]string{
			"timeout": "20", "interval": "5", "success_count": "3", "failure_count": "3",
		},
		Vip: vip,
	}
	childOps, err := c.OVNNbTables.Table(&ovnnb.LoadBalancerHealthCheck{}).CreateOps(row)
	if err != nil {
		return err
	}
	parentOps, err := c.OVNNbTables.Table(&ovnnb.LoadBalancer{}).MutateOps(lb, model.Mutation{
		Field: &lb.HealthCheck, Value: []string{row.UUID}, Mutator: ovsdb.MutateOperationInsert,
	})
	if err != nil {
		return err
	}
	return c.OVNNbTables.Table(&ovnnb.LoadBalancer{}).Transact(
		context.Background(), "lbhc-add", append(childOps, parentOps...)...,
	)
}

// deleteLoadBalancerHealthCheck removes a health-check UUID from its parent.
func (c *Controller) deleteLoadBalancerHealthCheck(lbName, uuid string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.LoadBalancerDeleteHealthCheck(lbName, uuid)
	}
	lb, err := c.getLoadBalancer(lbName, false)
	if err != nil || !slices.Contains(lb.HealthCheck, uuid) {
		return err
	}
	return c.OVNNbTables.Table(&ovnnb.LoadBalancer{}).Mutate(
		context.Background(), "lb-hc-del", lb,
		model.Mutation{Field: &lb.HealthCheck, Value: []string{uuid}, Mutator: ovsdb.MutateOperationDelete},
	)
}

// deleteLoadBalancerVIP removes a VIP and, when requested, its health-check
// and now-unused IP-port mappings.
func (c *Controller) deleteLoadBalancerVIP(lbName, vip string, ignoreHealthCheck bool) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.LoadBalancerDeleteVip(lbName, vip, ignoreHealthCheck)
	}
	lb, err := c.getLoadBalancer(lbName, true)
	if err != nil || lb == nil {
		return err
	}
	var checkUUID string
	var checks []ovnnb.LoadBalancerHealthCheck
	if err := c.OVNNbTables.Table(&ovnnb.LoadBalancerHealthCheck{}).Filter(
		context.Background(), func(row *ovnnb.LoadBalancerHealthCheck) bool {
			return slices.Contains(lb.HealthCheck, row.UUID) && row.Vip == vip
		}, &checks,
	); err != nil {
		return err
	}
	if len(checks) > 1 {
		return fmt.Errorf("load balancer %s has more than one health check with vip %s", lbName, vip)
	}
	if len(checks) == 1 {
		checkUUID = checks[0].UUID
	}
	if len(lb.IPPortMappings) != 0 {
		ignoreHealthCheck = false
	}
	if !ignoreHealthCheck && checkUUID != "" {
		if err := c.deleteLoadBalancerIPPortMapping(lbName, vip); err != nil {
			return err
		}
		if err := c.deleteLoadBalancerHealthCheck(lbName, checkUUID); err != nil {
			return err
		}
	}
	if _, ok := lb.Vips[vip]; !ok {
		return nil
	}
	mutations := []model.Mutation{{
		Field: &lb.Vips, Value: map[string]string{vip: lb.Vips[vip]}, Mutator: ovsdb.MutateOperationDelete,
	}}
	key := localExternalVIPKeyPrefix + vip
	if value, ok := lb.ExternalIDs[key]; ok {
		mutations = append(mutations, model.Mutation{
			Field: &lb.ExternalIDs, Value: map[string]string{key: value}, Mutator: ovsdb.MutateOperationDelete,
		})
	}
	return c.OVNNbTables.Table(&ovnnb.LoadBalancer{}).Mutate(
		context.Background(), "lb-del", lb, mutations...,
	)
}

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

func (c *Controller) listLogicalRouterPorts(externalIDs map[string]string, filter func(*ovnnb.LogicalRouterPort) bool) ([]ovnnb.LogicalRouterPort, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ListLogicalRouterPorts(externalIDs, filter)
	}
	var rows []ovnnb.LogicalRouterPort
	err := c.OVNNbTables.Table(&ovnnb.LogicalRouterPort{}).Filter(context.Background(), func(row *ovnnb.LogicalRouterPort) bool {
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

func (c *Controller) listNATs(lrName, natType, logicalIP string, externalIDs map[string]string) ([]*ovnnb.NAT, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ListNats(lrName, natType, logicalIP, externalIDs)
	}
	lr, err := c.getLogicalRouter(lrName, false)
	if err != nil {
		return nil, err
	}
	natUUIDs := make(map[string]struct{}, len(lr.Nat))
	for _, uuid := range lr.Nat {
		natUUIDs[uuid] = struct{}{}
	}
	var rows []*ovnnb.NAT
	err = c.OVNNbTables.Table(&ovnnb.NAT{}).Filter(context.Background(), func(row *ovnnb.NAT) bool {
		if _, ok := natUUIDs[row.UUID]; !ok {
			return false
		}
		if !matchesExternalIDsWithEmptyValue(row.ExternalIDs, externalIDs, true) {
			return false
		}
		if natType != "" && row.Type != natType {
			return false
		}
		return logicalIP == "" || row.LogicalIP == logicalIP
	}, &rows)
	return rows, err
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
