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

	"github.com/kubeovn/kube-ovn/pkg/ovs"
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
