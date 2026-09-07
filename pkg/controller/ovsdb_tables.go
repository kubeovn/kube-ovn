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

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// deleteAddressSets removes address sets selected by their names through the
// generic table facade.
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
	rows, err := compat.Filter[ovnnb.AddressSet](
		context.Background(), c.OVNNbTables, &ovnnb.AddressSet{},
		func(row *ovnnb.AddressSet) bool {
			_, ok := wanted[row.Name]
			return ok
		},
	)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	selectors := make([]model.Model, len(rows))
	for i := range rows {
		selectors[i] = &rows[i]
	}
	return compat.Delete(context.Background(), c.OVNNbTables, &ovnnb.AddressSet{}, "as-del", selectors...)
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
	rows, err := compat.Filter[ovnnb.PortGroup](
		context.Background(), c.OVNNbTables, &ovnnb.PortGroup{},
		func(row *ovnnb.PortGroup) bool {
			_, ok := wanted[row.Name]
			return ok
		},
	)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	selectors := make([]model.Model, len(rows))
	for i := range rows {
		selectors[i] = &rows[i]
	}
	return compat.Delete(context.Background(), c.OVNNbTables, &ovnnb.PortGroup{}, "pg-del", selectors...)
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
	return compat.DeleteFilter(
		context.Background(), c.OVNNbTables, &ovnnb.AddressSet{}, "ass-del",
		func(row *ovnnb.AddressSet) bool {
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
	rows, err := compat.Filter[ovnnb.AddressSet](
		context.Background(), c.OVNNbTables, &ovnnb.AddressSet{},
		func(row *ovnnb.AddressSet) bool { return row.Name == name },
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
	return compat.Create(
		context.Background(), c.OVNNbTables, &ovnnb.AddressSet{}, "as-add",
		&ovnnb.AddressSet{Name: name, ExternalIDs: finalExternalIDs},
	)
}

func (c *Controller) getAddressSet(name string, ignoreNotFound bool) (*ovnnb.AddressSet, error) {
	row := &ovnnb.AddressSet{Name: name}
	if err := compat.Get(context.Background(), c.OVNNbTables, &ovnnb.AddressSet{}, row); err != nil {
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
	return compat.Update(
		context.Background(), c.OVNNbTables, &ovnnb.AddressSet{}, "as-update", as, as, &as.Addresses,
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

// createBareLogicalSwitch creates a vendor-owned logical switch when it is
// absent. The helper intentionally only manages the switch row; ports and
// router references are reconciled by their respective helpers.
func (c *Controller) createBareLogicalSwitch(name string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.CreateBareLogicalSwitch(name)
	}
	if name == "" {
		return errors.New("the logical switch name is required")
	}
	if existing, err := c.getLogicalSwitch(name, true); err != nil {
		return err
	} else if existing != nil {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Create(
		context.Background(), "ls-add", &ovnnb.LogicalSwitch{
			Name:        name,
			ExternalIDs: map[string]string{"vendor": util.CniTypeName},
		},
	)
}

// createLogicalSwitch reconciles the logical switch and its optional router
// patch port without depending on the legacy domain client implementation.
func (c *Controller) createLogicalSwitch(lsName, lrName, cidrBlock, gateway, gatewayMAC string, needRouter, randomAllocateGW bool) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.CreateLogicalSwitch(lsName, lrName, cidrBlock, gateway, gatewayMAC, needRouter, randomAllocateGW)
	}
	if lsName == "" {
		return errors.New("the logical switch name is required")
	}

	var switchNetworks string
	if cidrBlock != "" {
		networks, err := util.GetIPAddrWithMask(gateway, cidrBlock)
		if err != nil {
			return fmt.Errorf("get gateway networks for logical switch %s: %w", lsName, err)
		}
		switchNetworks = networks
	}

	existing, err := c.getLogicalSwitch(lsName, true)
	if err != nil {
		return err
	}
	if existing == nil {
		if err := c.createBareLogicalSwitch(lsName); err != nil {
			return fmt.Errorf("create logical switch %s: %w", lsName, err)
		}
	} else if switchNetworks != "" {
		if randomAllocateGW {
			return nil
		}
		lrpName := fmt.Sprintf("%s-%s", lrName, lsName)
		lrp, getErr := c.getLogicalRouterPort(lrpName, true)
		if getErr != nil {
			return getErr
		}
		if lrp != nil {
			if err := c.updateLogicalRouterPortNetworks(lrpName, strings.Split(switchNetworks, ",")); err != nil {
				return fmt.Errorf("update logical router port %s: %w", lrpName, err)
			}
			if gatewayMAC != "" && lrp.MAC != gatewayMAC {
				lrp.MAC = gatewayMAC
				if err := c.OVNNbTables.Table(&ovnnb.LogicalRouterPort{}).Update(
					context.Background(), "lrp-update", lrp, lrp, &lrp.MAC,
				); err != nil {
					return fmt.Errorf("update logical router port %s MAC: %w", lrpName, err)
				}
			}
		}
	}

	lspName := fmt.Sprintf("%s-%s", lsName, lrName)
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)
	if needRouter && switchNetworks != "" {
		if err := c.createLogicalPatchPort(lsName, lrName, lspName, lrpName, switchNetworks, gatewayMAC); err != nil {
			return err
		}
		return nil
	}
	if randomAllocateGW {
		return nil
	}
	if err := c.removeLogicalPatchPort(lspName, lrpName); err != nil {
		return fmt.Errorf("remove router type port %s and %s: %w", lspName, lrpName, err)
	}
	return nil
}

func genericACL(parent, direction string, priority any, match string, action any, tier int, externalIDs map[string]string, configure ...func(*ovnnb.ACL)) *ovnnb.ACL {
	priorityValue := 0
	switch value := priority.(type) {
	case int:
		priorityValue = value
	case string:
		priorityValue = mustParseACLInt(value)
	default:
		panic(fmt.Sprintf("unsupported ACL priority type %T", priority))
	}
	ids := maps.Clone(externalIDs)
	if ids == nil {
		ids = make(map[string]string, 2)
	}
	ids["parent"] = parent
	ids["vendor"] = util.CniTypeName
	acl := &ovnnb.ACL{
		UUID:        ovsclient.NamedUUID(),
		Direction:   direction,
		Priority:    priorityValue,
		Match:       match,
		Action:      fmt.Sprint(action),
		Tier:        tier,
		ExternalIDs: ids,
	}
	for _, option := range configure {
		option(acl)
	}
	return acl
}

func (c *Controller) logicalSwitchACLOps(ls *ovnnb.LogicalSwitch, acls []*ovnnb.ACL, mutator ovsdb.Mutator) ([]ovsdb.Operation, error) {
	uuids := make([]string, 0, len(acls))
	for _, acl := range acls {
		if acl != nil {
			uuids = append(uuids, acl.UUID)
		}
	}
	if len(uuids) == 0 {
		return nil, nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).MutateOps(ls, model.Mutation{
		Field: &ls.ACLs, Value: uuids, Mutator: mutator,
	})
}

func (c *Controller) deleteLogicalSwitchACLOps(ls *ovnnb.LogicalSwitch, direction string, externalIDs map[string]string) ([]ovsdb.Operation, error) {
	if ls == nil {
		return nil, errors.New("logical switch is nil")
	}
	allowed := make(map[string]struct{}, len(ls.ACLs))
	for _, uuid := range ls.ACLs {
		allowed[uuid] = struct{}{}
	}
	var rows []ovnnb.ACL
	if err := c.OVNNbTables.Table(&ovnnb.ACL{}).Filter(context.Background(), func(row *ovnnb.ACL) bool {
		if _, ok := allowed[row.UUID]; !ok {
			return false
		}
		if direction != "" && row.Direction != direction {
			return false
		}
		return matchesExternalIDs(row.ExternalIDs, externalIDs)
	}, &rows); err != nil {
		return nil, err
	}
	selectors := make([]*ovnnb.ACL, len(rows))
	for i := range rows {
		selectors[i] = &rows[i]
	}
	return c.logicalSwitchACLOps(ls, selectors, ovsdb.MutateOperationDelete)
}

func (c *Controller) deleteLogicalSwitchACLs(lsName, direction string, externalIDs map[string]string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteAcls(lsName, ovs.LogicalSwitchKey, direction, externalIDs)
	}
	ls, err := c.getLogicalSwitch(lsName, true)
	if err != nil || ls == nil {
		return err
	}
	ops, err := c.deleteLogicalSwitchACLOps(ls, direction, externalIDs)
	if err != nil {
		return err
	}
	if len(ops) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Transact(context.Background(), "acls-del", ops...)
}

func (c *Controller) deletePortGroupACLOps(pgName, direction string, externalIDs map[string]string) ([]ovsdb.Operation, error) {
	pg, err := c.getPortGroup(pgName, false)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]struct{}, len(pg.ACLs))
	for _, uuid := range pg.ACLs {
		allowed[uuid] = struct{}{}
	}
	var rows []ovnnb.ACL
	if err := c.OVNNbTables.Table(&ovnnb.ACL{}).Filter(context.Background(), func(row *ovnnb.ACL) bool {
		if _, ok := allowed[row.UUID]; !ok {
			return false
		}
		if direction != "" && row.Direction != direction {
			return false
		}
		return matchesExternalIDs(row.ExternalIDs, externalIDs)
	}, &rows); err != nil {
		return nil, err
	}
	uuids := make([]*ovnnb.ACL, len(rows))
	for i := range rows {
		uuids[i] = &rows[i]
	}
	if len(uuids) == 0 {
		return nil, nil
	}
	ids := make([]string, len(uuids))
	for i, acl := range uuids {
		ids[i] = acl.UUID
	}
	return c.OVNNbTables.Table(&ovnnb.PortGroup{}).MutateOps(pg, model.Mutation{
		Field: &pg.ACLs, Value: ids, Mutator: ovsdb.MutateOperationDelete,
	})
}

func (c *Controller) deletePortGroupACLs(pgName, direction string, externalIDs map[string]string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteAcls(pgName, "pg", direction, externalIDs)
	}
	ops, err := c.deletePortGroupACLOps(pgName, direction, externalIDs)
	if err != nil || len(ops) == 0 {
		return err
	}
	return c.OVNNbTables.Table(&ovnnb.PortGroup{}).Transact(context.Background(), "acls-del", ops...)
}

func (c *Controller) transactNB(method string, operations ...ovsdb.Operation) error {
	// NetworkPolicy/ANP/CNP builders remain domain capabilities on the OVN
	// client: they resolve address-set semantics, meters, ACL names, and policy
	// tiers before returning operations. The generic table facade owns the final
	// transaction so those builders do not bypass provider transaction policy.
	if len(operations) == 0 {
		return nil
	}
	if c.OVNNbTables == nil {
		return errors.New("OVN NB table provider is nil")
	}
	return c.OVNNbTables.Table(&ovnnb.ACL{}).Transact(context.Background(), method, operations...)
}

func (c *Controller) createLogicalSwitchACLTable(ls *ovnnb.LogicalSwitch, acls ...*ovnnb.ACL) ([]ovsdb.Operation, error) {
	nonNil := make([]model.Model, 0, len(acls))
	for _, acl := range acls {
		if acl != nil {
			nonNil = append(nonNil, acl)
		}
	}
	if len(nonNil) == 0 {
		return nil, nil
	}
	createOps, err := c.OVNNbTables.Table(&ovnnb.ACL{}).CreateOps(nonNil...)
	if err != nil {
		return nil, err
	}
	insertOps, err := c.logicalSwitchACLOps(ls, acls, ovsdb.MutateOperationInsert)
	if err != nil {
		return nil, err
	}
	return append(createOps, insertOps...), nil
}

// createPortGroupACLTable builds ACL insert and parent-reference operations
// for a port group. Keeping the parent mutation in the same operation list
// avoids leaving an orphan ACL when a reconcile creates both rows together.
func (c *Controller) createPortGroupACLTable(pg *ovnnb.PortGroup, acls ...*ovnnb.ACL) ([]ovsdb.Operation, error) {
	if pg == nil {
		return nil, errors.New("port group is nil")
	}
	nonNil := make([]model.Model, 0, len(acls))
	for _, acl := range acls {
		if acl != nil {
			nonNil = append(nonNil, acl)
		}
	}
	if len(nonNil) == 0 {
		return nil, nil
	}
	createOps, err := c.OVNNbTables.Table(&ovnnb.ACL{}).CreateOps(nonNil...)
	if err != nil {
		return nil, err
	}
	uuidValues := make([]string, 0, len(nonNil))
	for _, row := range nonNil {
		uuidValues = append(uuidValues, row.(*ovnnb.ACL).UUID)
	}
	insertOps, err := c.OVNNbTables.Table(&ovnnb.PortGroup{}).MutateOps(pg, model.Mutation{
		Field: &pg.ACLs, Value: uuidValues, Mutator: ovsdb.MutateOperationInsert,
	})
	if err != nil {
		return nil, err
	}
	return append(createOps, insertOps...), nil
}

func (c *Controller) createPortGroupACLs(pgName string, acls ...*ovnnb.ACL) error {
	pg, err := c.getPortGroup(pgName, false)
	if err != nil {
		return err
	}
	operations, err := c.createPortGroupACLTable(pg, acls...)
	if err != nil {
		return err
	}
	if len(operations) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.PortGroup{}).Transact(context.Background(), "pg-acls-add", operations...)
}

func (c *Controller) createGatewayACL(lsName, pgName string) error {
	if c.OVNNbTables == nil {
		return errors.New("OVN NB table provider is nil")
	}
	parentName, parentType := pgName, "pg"
	if parentName == "" {
		parentName, parentType = lsName, ovs.LogicalSwitchKey
	}
	if parentName == "" {
		return errors.New("one of port group name and logical switch name must be specified")
	}
	egressOptions := func(acl *ovnnb.ACL) {
		if acl.Options == nil {
			acl.Options = make(map[string]string, 1)
		}
		acl.Options["apply-after-lb"] = "true"
	}
	acls := []*ovnnb.ACL{
		genericACL(parentName, ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, "icmp6", ovnnb.ACLActionAllowStateless, util.NetpolACLTier, nil, egressOptions),
		genericACL(parentName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, "icmp6", ovnnb.ACLActionAllowStateless, util.NetpolACLTier, nil),
	}
	if parentType == "pg" {
		return c.createMissingACLsForPortGroup(parentName, acls...)
	}
	ls, err := c.getLogicalSwitch(parentName, false)
	if err != nil {
		return err
	}
	return c.createMissingACLsForLogicalSwitch(ls, acls...)
}

func (c *Controller) createMissingACLsForPortGroup(pgName string, desired ...*ovnnb.ACL) error {
	pg, err := c.getPortGroup(pgName, false)
	if err != nil {
		return err
	}
	return c.createMissingACLs(pgName, pg.ACLs, desired, func(rows []*ovnnb.ACL) ([]ovsdb.Operation, error) {
		return c.createPortGroupACLTable(pg, rows...)
	})
}

func (c *Controller) createMissingACLsForLogicalSwitch(ls *ovnnb.LogicalSwitch, desired ...*ovnnb.ACL) error {
	return c.createMissingACLs(ls.Name, ls.ACLs, desired, func(rows []*ovnnb.ACL) ([]ovsdb.Operation, error) {
		return c.createLogicalSwitchACLTable(ls, rows...)
	})
}

func (c *Controller) createMissingACLs(parent string, attached []string, desired []*ovnnb.ACL, build func([]*ovnnb.ACL) ([]ovsdb.Operation, error)) error {
	allowed := make(map[string]struct{}, len(attached))
	for _, uuid := range attached {
		allowed[uuid] = struct{}{}
	}
	var existing []ovnnb.ACL
	if err := c.OVNNbTables.Table(&ovnnb.ACL{}).Filter(context.Background(), func(row *ovnnb.ACL) bool {
		_, attached := allowed[row.UUID]
		return attached && row.ExternalIDs["parent"] == parent
	}, &existing); err != nil {
		return err
	}
	has := make(map[string]struct{}, len(existing))
	for _, row := range existing {
		has[aclIdentity(&row)] = struct{}{}
	}
	missing := make([]*ovnnb.ACL, 0, len(desired))
	for _, row := range desired {
		if row == nil {
			continue
		}
		key := aclIdentity(row)
		if _, ok := has[key]; ok {
			continue
		}
		has[key] = struct{}{}
		missing = append(missing, row)
	}
	if len(missing) == 0 {
		return nil
	}
	operations, err := build(missing)
	if err != nil {
		return err
	}
	return c.OVNNbTables.Table(&ovnnb.ACL{}).Transact(context.Background(), "acls-add", operations...)
}

// aclIdentity matches the fields used by OVN's ACL lookup semantics. Action is
// intentionally excluded: direction, priority, match, and tier identify one
// ACL under a given parent in the legacy client as well.
func aclIdentity(row *ovnnb.ACL) string {
	return fmt.Sprintf("%s|%d|%s|%d", row.Direction, row.Priority, row.Match, row.Tier)
}

func (c *Controller) createNodeACL(pgName, nodeIPStr, joinIPStr string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.CreateNodeACL(pgName, nodeIPStr, joinIPStr)
	}
	pg, err := c.getPortGroup(pgName, false)
	if err != nil {
		return err
	}
	nodeIPs := strings.Split(nodeIPStr, ",")
	desired := make([]*ovnnb.ACL, 0, len(nodeIPs)*2)
	for _, nodeIP := range nodeIPs {
		if nodeIP == "" {
			continue
		}
		ipSuffix := "ip4"
		if util.CheckProtocol(nodeIP) == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
		}
		pgAs := fmt.Sprintf("%s_%s", pgName, ipSuffix)
		desired = append(desired,
			genericACL(pgName, ovnnb.ACLDirectionToLport, util.NodeAllowPriority, fmt.Sprintf("%s.src == %s && %s.dst == $%s", ipSuffix, nodeIP, ipSuffix, pgAs), ovnnb.ACLActionAllowRelated, util.NetpolACLTier, nil),
			genericACL(pgName, ovnnb.ACLDirectionFromLport, util.NodeAllowPriority, fmt.Sprintf("%s.dst == %s && %s.src == $%s", ipSuffix, nodeIP, ipSuffix, pgAs), ovnnb.ACLActionAllowRelated, util.NetpolACLTier, nil, func(acl *ovnnb.ACL) {
				acl.Options = map[string]string{"apply-after-lb": "true"}
			}),
		)
	}
	allowed := make(map[string]struct{}, len(pg.ACLs))
	for _, uuid := range pg.ACLs {
		allowed[uuid] = struct{}{}
	}
	var existing []ovnnb.ACL
	if err := c.OVNNbTables.Table(&ovnnb.ACL{}).Filter(context.Background(), func(row *ovnnb.ACL) bool {
		_, attached := allowed[row.UUID]
		return attached && row.ExternalIDs["parent"] == pgName
	}, &existing); err != nil {
		return err
	}
	var operations []ovsdb.Operation
	var stale []string
	for joinIP := range strings.SplitSeq(joinIPStr, ",") {
		if joinIP == "" || slices.Contains(nodeIPs, joinIP) {
			continue
		}
		ipSuffix := "ip4"
		if util.CheckProtocol(joinIP) == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
		}
		pgAs := fmt.Sprintf("%s_%s", pgName, ipSuffix)
		matches := []struct {
			direction ovnnb.ACLDirection
			match     string
		}{
			{ovnnb.ACLDirectionToLport, fmt.Sprintf("%s.src == %s && %s.dst == $%s", ipSuffix, joinIP, ipSuffix, pgAs)},
			{ovnnb.ACLDirectionFromLport, fmt.Sprintf("%s.dst == %s && %s.src == $%s", ipSuffix, joinIP, ipSuffix, pgAs)},
		}
		for _, candidate := range matches {
			for _, row := range existing {
				if row.Direction == candidate.direction && row.Priority == mustParseACLInt(util.NodeAllowPriority) && row.Match == candidate.match && row.Tier == util.NetpolACLTier {
					stale = append(stale, row.UUID)
				}
			}
		}
	}
	if len(stale) != 0 {
		operations, err = c.OVNNbTables.Table(&ovnnb.PortGroup{}).MutateOps(pg, model.Mutation{
			Field: &pg.ACLs, Value: stale, Mutator: ovsdb.MutateOperationDelete,
		})
		if err != nil {
			return err
		}
	}
	seen := make(map[string]struct{}, len(existing))
	for _, row := range existing {
		seen[aclIdentity(&row)] = struct{}{}
	}
	missing := make([]*ovnnb.ACL, 0, len(desired))
	for _, row := range desired {
		key := aclIdentity(row)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		missing = append(missing, row)
	}
	if len(missing) != 0 {
		createOps, createErr := c.createPortGroupACLTable(pg, missing...)
		if createErr != nil {
			return createErr
		}
		operations = append(operations, createOps...)
	}
	if len(operations) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.PortGroup{}).Transact(context.Background(), "node-acls-update", operations...)
}

func mustParseACLInt(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		panic(fmt.Sprintf("invalid internal ACL priority %q: %v", value, err))
	}
	return parsed
}

func (c *Controller) setNetPolACLLog(pgName string, logEnable, isIngress bool) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.SetNetPolACLLog(pgName, logEnable, isIngress)
	}
	pg, err := c.getPortGroup(pgName, false)
	if err != nil {
		return err
	}
	direction, portDirection := ovnnb.ACLDirectionToLport, "outport"
	priority := mustParseACLInt(util.IngressDefaultDrop)
	if !isIngress {
		direction, portDirection = ovnnb.ACLDirectionFromLport, "inport"
		priority = mustParseACLInt(util.EgressDefaultDrop)
	}
	match := ovs.NewAndACLMatch(ovs.NewACLMatch(portDirection, "==", "@"+pgName, ""), ovs.NewACLMatch("ip", "", "", "")).String()
	allowed := make(map[string]struct{}, len(pg.ACLs))
	for _, uuid := range pg.ACLs {
		allowed[uuid] = struct{}{}
	}
	var rows []ovnnb.ACL
	if err := c.OVNNbTables.Table(&ovnnb.ACL{}).Filter(context.Background(), func(row *ovnnb.ACL) bool {
		_, attached := allowed[row.UUID]
		return attached && row.ExternalIDs["parent"] == pgName && row.Direction == direction && row.Priority == priority && row.Match == match && row.Tier == util.NetpolACLTier
	}, &rows); err != nil {
		return err
	}
	if len(rows) == 0 || rows[0].Log == logEnable {
		return nil
	}
	rows[0].Log = logEnable
	return c.OVNNbTables.Table(&ovnnb.ACL{}).Update(context.Background(), "acl-log-update", &rows[0], &rows[0], &rows[0].Log)
}

func (c *Controller) createGatewayLogicalSwitch(lsName, lrName, provider, ip, mac string, vlanID int, chassises ...string) error {
	if c.OVNNbTables == nil {
		return errors.New("OVN NB table provider is nil")
	}
	oldLocalnet := "ln-" + lsName
	if err := c.deleteLogicalSwitchPort(oldLocalnet); err != nil {
		return fmt.Errorf("delete old localnet %s: %w", oldLocalnet, err)
	}
	if err := c.createBareLogicalSwitch(lsName); err != nil {
		return err
	}
	if err := c.createLocalnetLogicalSwitchPort(lsName, ovs.GetLocalnetName(lsName), provider, "", vlanID); err != nil {
		return err
	}
	return c.createLogicalPatchPort(
		lsName, lrName, fmt.Sprintf("%s-%s", lsName, lrName), fmt.Sprintf("%s-%s", lrName, lsName), ip, mac, chassises...,
	)
}

func (c *Controller) updateLogicalSwitchACLTable(lsName, cidrBlock string, subnetAcls []kubeovnv1.ACL, allowEWTraffic bool) error {
	ls, err := c.getLogicalSwitch(lsName, false)
	if err != nil {
		return err
	}
	removeOps, err := c.deleteLogicalSwitchACLOps(ls, "", map[string]string{"subnet": lsName})
	if err != nil {
		return err
	}
	if len(subnetAcls) == 0 && !allowEWTraffic {
		if len(removeOps) == 0 {
			return nil
		}
		return c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Transact(context.Background(), "acls-update", removeOps...)
	}
	acls := make([]*ovnnb.ACL, 0, len(subnetAcls)+4)
	ids := map[string]string{"subnet": lsName}
	if allowEWTraffic {
		for cidr := range strings.SplitSeq(cidrBlock, ",") {
			protocol := util.CheckProtocol(cidr)
			ipSuffix := "ip4"
			if protocol == kubeovnv1.ProtocolIPv6 {
				ipSuffix = "ip6"
			}
			match := ovs.NewAndACLMatch(
				ovs.NewACLMatch(ipSuffix+".src", "==", cidr, ""),
				ovs.NewACLMatch(ipSuffix+".dst", "==", cidr, ""),
			).String()
			for _, direction := range []string{ovnnb.ACLDirectionToLport, ovnnb.ACLDirectionFromLport} {
				acls = append(acls, genericACL(lsName, direction, util.AllowEWTrafficPriority, match, ovnnb.ACLActionAllow, util.NetpolACLTier, ids))
			}
		}
	}
	for _, subnetACL := range subnetAcls {
		acls = append(acls, genericACL(lsName, subnetACL.Direction, subnetACL.Priority, subnetACL.Match, subnetACL.Action, util.NetpolACLTier, ids))
	}
	createOps, err := c.createLogicalSwitchACLTable(ls, acls...)
	if err != nil {
		return err
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Transact(context.Background(), "acls-update", append(removeOps, createOps...)...)
}

func (c *Controller) setLogicalSwitchPrivateTable(lsName, cidrBlock, nodeSwitchCIDR string, allowSubnets []string) error {
	ls, err := c.getLogicalSwitch(lsName, false)
	if err != nil {
		return err
	}
	removeOps, err := c.deleteLogicalSwitchACLOps(ls, "", nil)
	if err != nil {
		return err
	}
	acls := []*ovnnb.ACL{genericACL(lsName, ovnnb.ACLDirectionToLport, util.DefaultDropPriority, "ip", ovnnb.ACLActionDrop, util.NetpolACLTier, nil, func(acl *ovnnb.ACL) {
		acl.Log = true
		severity := ovnnb.ACLSeverityWarning
		acl.Severity = &severity
	})}
	for cidr := range strings.SplitSeq(cidrBlock, ",") {
		protocol := util.CheckProtocol(cidr)
		ipSuffix := "ip4"
		if protocol == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
		}
		sameSubnet := ovs.NewAndACLMatch(ovs.NewACLMatch(ipSuffix+".src", "==", cidr, ""), ovs.NewACLMatch(ipSuffix+".dst", "==", cidr, "")).String()
		acls = append(acls, genericACL(lsName, ovnnb.ACLDirectionToLport, util.SubnetAllowPriority, sameSubnet, ovnnb.ACLActionAllowRelated, util.NetpolACLTier, nil))
		for nodeCidr := range strings.SplitSeq(nodeSwitchCIDR, ",") {
			if util.CheckProtocol(nodeCidr) != protocol {
				continue
			}
			match := ovs.NewACLMatch(ipSuffix+".src", "==", nodeCidr, "").String()
			acls = append(acls, genericACL(lsName, ovnnb.ACLDirectionToLport, util.NodeAllowPriority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier, nil))
		}
		for _, allowed := range allowSubnets {
			allowed = strings.TrimSpace(allowed)
			if allowed == "" || util.CheckProtocol(allowed) != protocol {
				continue
			}
			match := ovs.NewOrACLMatch(
				ovs.NewAndACLMatch(ovs.NewACLMatch(ipSuffix+".src", "==", cidr, ""), ovs.NewACLMatch(ipSuffix+".dst", "==", allowed, "")),
				ovs.NewAndACLMatch(ovs.NewACLMatch(ipSuffix+".src", "==", allowed, ""), ovs.NewACLMatch(ipSuffix+".dst", "==", cidr, "")),
			).String()
			acls = append(acls, genericACL(lsName, ovnnb.ACLDirectionToLport, util.SubnetAllowPriority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier, nil))
		}
	}
	createOps, err := c.createLogicalSwitchACLTable(ls, acls...)
	if err != nil {
		return err
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Transact(context.Background(), "acls-private", append(removeOps, createOps...)...)
}

func (c *Controller) setLogicalSwitchRoutedTable(lsName, router, cidrBlock, gateway, gatewayMAC, nodeSwitchCIDR string, allowSubnets []string, private bool) error {
	if lsName == "" || router == "" || gatewayMAC == "" {
		return errors.New("logical switch, router and gateway MAC are required for routed mode")
	}
	ls, err := c.getLogicalSwitch(lsName, false)
	if err != nil {
		return err
	}
	removeOps, err := c.deleteLogicalSwitchACLOps(ls, "", nil)
	if err != nil {
		return err
	}
	acls := make([]*ovnnb.ACL, 0, 16)
	for gw := range strings.SplitSeq(gateway, ",") {
		gw = strings.TrimSpace(gw)
		switch util.CheckProtocol(gw) {
		case kubeovnv1.ProtocolIPv4:
			acls = append(acls,
				genericACL(lsName, ovnnb.ACLDirectionFromLport, util.RoutedAllowPriority, ovs.NewAndACLMatch(ovs.NewACLMatch("arp", "", "", ""), ovs.NewACLMatch("arp.tpa", "==", gw, "")).String(), ovnnb.ACLActionAllow, util.NetpolACLTier, nil),
				genericACL(lsName, ovnnb.ACLDirectionToLport, util.RoutedAllowPriority, ovs.NewAndACLMatch(ovs.NewACLMatch("arp", "", "", ""), ovs.NewACLMatch("arp.spa", "==", gw, "")).String(), ovnnb.ACLActionAllow, util.NetpolACLTier, nil))
		case kubeovnv1.ProtocolIPv6:
			acls = append(acls,
				genericACL(lsName, ovnnb.ACLDirectionFromLport, util.RoutedAllowPriority, ovs.NewAndACLMatch(ovs.NewACLMatch("nd_ns", "", "", ""), ovs.NewACLMatch("nd.target", "==", gw, "")).String(), ovnnb.ACLActionAllow, util.NetpolACLTier, nil),
				genericACL(lsName, ovnnb.ACLDirectionToLport, util.RoutedAllowPriority, ovs.NewAndACLMatch(ovs.NewACLMatch("nd_na", "", "", ""), ovs.NewACLMatch("ip6.src", "==", gw, "")).String(), ovnnb.ACLActionAllow, util.NetpolACLTier, nil))
		}
	}
	routerLSP := ovs.LogicalSwitchPortName(router, lsName)
	toRouter := ovs.NewAndACLMatch(ovs.NewACLMatch("ip", "", "", ""), ovs.NewACLMatch("eth.dst", "==", gatewayMAC, "")).String()
	acls = append(acls,
		genericACL(lsName, ovnnb.ACLDirectionFromLport, util.RoutedAllowPriority, toRouter, ovnnb.ACLActionAllowRelated, util.NetpolACLTier, nil),
		genericACL(lsName, ovnnb.ACLDirectionToLport, util.RoutedAllowPriority, toRouter, ovnnb.ACLActionAllowRelated, util.NetpolACLTier, nil))
	fromRouter := ovs.NewAndACLMatch(ovs.NewACLMatch("ip", "", "", ""), ovs.NewACLMatch("inport", "==", fmt.Sprintf(`"%s"`, routerLSP), ""), ovs.NewACLMatch("eth.src", "==", gatewayMAC, "")).String()
	acls = append(acls, genericACL(lsName, ovnnb.ACLDirectionFromLport, util.RoutedAllowPriority, fromRouter, ovnnb.ACLActionAllowRelated, util.NetpolACLTier, nil))
	if !private {
		fromGateway := ovs.NewAndACLMatch(ovs.NewACLMatch("ip", "", "", ""), ovs.NewACLMatch("eth.src", "==", gatewayMAC, "")).String()
		acls = append(acls, genericACL(lsName, ovnnb.ACLDirectionToLport, util.RoutedAllowPriority, fromGateway, ovnnb.ACLActionAllowRelated, util.NetpolACLTier, nil))
	} else {
		for cidr := range strings.SplitSeq(cidrBlock, ",") {
			if cidr == "" {
				continue
			}
			protocol := util.CheckProtocol(cidr)
			ipSuffix := "ip4"
			if protocol == kubeovnv1.ProtocolIPv6 {
				ipSuffix = "ip6"
			}
			for _, source := range append([]string{cidr}, append(strings.Split(nodeSwitchCIDR, ","), allowSubnets...)...) {
				source = strings.TrimSpace(source)
				if source == "" || util.CheckProtocol(source) != protocol {
					continue
				}
				match := ovs.NewAndACLMatch(ovs.NewACLMatch("ip", "", "", ""), ovs.NewACLMatch("eth.src", "==", gatewayMAC, ""), ovs.NewACLMatch(ipSuffix+".src", "==", source, "")).String()
				acls = append(acls, genericACL(lsName, ovnnb.ACLDirectionToLport, util.RoutedAllowPriority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier, nil))
			}
		}
	}
	for _, direction := range []string{ovnnb.ACLDirectionFromLport, ovnnb.ACLDirectionToLport} {
		for _, match := range []string{"ip", "arp", "nd_ns", "nd_na"} {
			acls = append(acls, genericACL(lsName, direction, util.RoutedDefaultDropPriority, match, ovnnb.ACLActionDrop, util.NetpolACLTier, nil, func(acl *ovnnb.ACL) {
				acl.Log = true
				severity := ovnnb.ACLSeverityWarning
				acl.Severity = &severity
			}))
		}
	}
	createOps, err := c.createLogicalSwitchACLTable(ls, acls...)
	if err != nil {
		return err
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Transact(context.Background(), "acls-routed", append(removeOps, createOps...)...)
}

func (c *Controller) updateLogicalSwitchACL(lsName, cidrBlock string, subnetAcls []kubeovnv1.ACL, allowEWTraffic bool) error {
	if c.OVNNbTables == nil {
		return errors.New("OVN NB table provider is nil")
	}
	return c.updateLogicalSwitchACLTable(lsName, cidrBlock, subnetAcls, allowEWTraffic)
}

func (c *Controller) setLogicalSwitchPrivate(lsName, cidrBlock, nodeSwitchCIDR string, allowSubnets []string) error {
	if c.OVNNbTables == nil {
		return errors.New("OVN NB table provider is nil")
	}
	return c.setLogicalSwitchPrivateTable(lsName, cidrBlock, nodeSwitchCIDR, allowSubnets)
}

func (c *Controller) setLogicalSwitchRouted(lsName, router, cidrBlock, gateway, gatewayMAC, nodeSwitchCIDR string, allowSubnets []string, private bool) error {
	if c.OVNNbTables == nil {
		return errors.New("OVN NB table provider is nil")
	}
	return c.setLogicalSwitchRoutedTable(lsName, router, cidrBlock, gateway, gatewayMAC, nodeSwitchCIDR, allowSubnets, private)
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

func parseIPv6RAConfigs(raw string) map[string]string {
	if raw == "" {
		return map[string]string{
			"address_mode": "dhcpv6_stateful", "max_interval": "30", "min_interval": "5", "send_periodic": "true",
		}
	}
	configs := make(map[string]string)
	for option := range strings.SplitSeq(strings.ReplaceAll(raw, " ", ""), ",") {
		key, value, ok := strings.Cut(option, "=")
		if ok && key != "" && value != "" {
			configs[key] = value
		}
	}
	return configs
}

func ipv6Prefixes(networks []string) []string {
	prefixes := make([]string, 0, len(networks))
	for _, network := range networks {
		address, prefix, ok := strings.Cut(network, "/")
		if ok {
			ip := net.ParseIP(address)
			if ip != nil && ip.To4() == nil {
				prefixes = append(prefixes, prefix)
			}
		}
	}
	return prefixes
}

func (c *Controller) updateLogicalRouterPortRA(name, configs string, enabled bool) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.UpdateLogicalRouterPortRA(name, configs, enabled)
	}
	lrp, err := c.getLogicalRouterPort(name, false)
	if err != nil {
		return err
	}
	if !enabled {
		lrp.Ipv6Prefix = nil
		lrp.Ipv6RaConfigs = nil
	} else {
		lrp.Ipv6Prefix = ipv6Prefixes(lrp.Networks)
		lrp.Ipv6RaConfigs = parseIPv6RAConfigs(configs)
		if len(lrp.Ipv6Prefix) == 0 || len(lrp.Ipv6RaConfigs) == 0 {
			return nil
		}
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouterPort{}).Update(
		context.Background(), "lrp-update", lrp, lrp, &lrp.Ipv6Prefix, &lrp.Ipv6RaConfigs,
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

func (c *Controller) deleteDHCPOptions(lsName, protocol string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteDHCPOptions(lsName, protocol)
	}
	if lsName == "" {
		return errors.New("the logical switch name is required")
	}
	if protocol == kubeovnv1.ProtocolDual {
		protocol = ""
	}
	return c.OVNNbTables.Table(&ovnnb.DHCPOptions{}).DeleteFilter(
		context.Background(), "dhcp-options-del", func(row *ovnnb.DHCPOptions) bool {
			if row.ExternalIDs["vendor"] != util.CniTypeName || row.ExternalIDs[ovs.LogicalSwitchKey] != lsName || row.ExternalIDs[ovs.PortKey] != "" {
				return false
			}
			return protocol == "" || row.ExternalIDs["protocol"] == protocol
		},
	)
}

func (c *Controller) listDHCPOptionsTable(lsName, portName, protocol string) ([]ovnnb.DHCPOptions, error) {
	var rows []ovnnb.DHCPOptions
	err := c.OVNNbTables.Table(&ovnnb.DHCPOptions{}).Filter(
		context.Background(), func(row *ovnnb.DHCPOptions) bool {
			if row.ExternalIDs["vendor"] != util.CniTypeName || row.ExternalIDs["protocol"] != protocol {
				return false
			}
			if portName == "" {
				return row.ExternalIDs[ovs.LogicalSwitchKey] == lsName && row.ExternalIDs[ovs.PortKey] == ""
			}
			return row.ExternalIDs[ovs.PortKey] == portName
		}, &rows,
	)
	return rows, err
}

func (c *Controller) getDHCPOptionsTable(lsName, portName, protocol string, ignoreNotFound bool) (*ovnnb.DHCPOptions, error) {
	rows, err := c.listDHCPOptionsTable(lsName, portName, protocol)
	if err != nil {
		return nil, fmt.Errorf("list %s DHCP options: %w", protocol, err)
	}
	switch len(rows) {
	case 0:
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("DHCP options not found for switch %s port %s", lsName, portName)
	case 1:
		return &rows[0], nil
	default:
		return nil, fmt.Errorf("multiple %s DHCP options for switch %s port %s", protocol, lsName, portName)
	}
}

func (c *Controller) updateDHCPOptionTable(lsName, portName, cidr, protocol, gateway, options string, mtu int) (string, error) {
	if lsName == "" || cidr == "" {
		return "", errors.New("logical switch name and cidr are required")
	}
	current, err := c.getDHCPOptionsTable(lsName, portName, protocol, true)
	if err != nil {
		return "", err
	}
	var optionMap map[string]string
	if protocol == kubeovnv1.ProtocolIPv4 {
		optionMap = ovs.BuildDHCPv4Options(options, gateway, "", mtu, []string{"lease_time", "router", "server_id", "server_mac", "mtu"})
	} else {
		optionMap = ovs.BuildDHCPv6Options(options, "", []string{"server_id"})
	}
	if current != nil {
		if protocol == kubeovnv1.ProtocolIPv4 && optionMap["server_mac"] == "" {
			optionMap["server_mac"] = current.Options["server_mac"]
		}
		if protocol == kubeovnv1.ProtocolIPv6 && optionMap["server_id"] == "" {
			optionMap["server_id"] = current.Options["server_id"]
		}
		if current.Cidr == cidr && maps.Equal(current.Options, optionMap) {
			return current.UUID, nil
		}
		current.Cidr = cidr
		current.Options = optionMap
		if err := c.OVNNbTables.Table(&ovnnb.DHCPOptions{}).Update(
			context.Background(), "dhcp-options-update", current, current, &current.Cidr, &current.Options,
		); err != nil {
			return "", err
		}
		return current.UUID, nil
	}

	if protocol == kubeovnv1.ProtocolIPv4 && optionMap["server_mac"] == "" {
		optionMap["server_mac"] = util.GenerateMac()
	}
	if protocol == kubeovnv1.ProtocolIPv6 && optionMap["server_id"] == "" {
		optionMap["server_id"] = util.GenerateMac()
	}
	externalIDs := map[string]string{
		ovs.LogicalSwitchKey: lsName,
		"protocol":           protocol,
		"vendor":             util.CniTypeName,
	}
	if portName != "" {
		externalIDs[ovs.PortKey] = portName
	}
	created := &ovnnb.DHCPOptions{
		UUID:        ovsclient.NamedUUID(),
		Cidr:        cidr,
		Options:     optionMap,
		ExternalIDs: externalIDs,
	}
	if err := c.OVNNbTables.Table(&ovnnb.DHCPOptions{}).Create(
		context.Background(), "dhcp-options-create", created,
	); err != nil {
		return "", err
	}
	if current, err = c.getDHCPOptionsTable(lsName, portName, protocol, true); err != nil {
		return "", err
	} else if current != nil {
		return current.UUID, nil
	}
	return created.UUID, nil
}

func (c *Controller) updateSubnetDHCPOptionsTable(subnet *kubeovnv1.Subnet, mtu int) (*ovs.DHCPOptionsUUIDs, error) {
	if !subnet.Spec.EnableDHCP {
		protocol := subnet.Spec.Protocol
		if protocol == kubeovnv1.ProtocolDual {
			protocol = ""
		}
		return &ovs.DHCPOptionsUUIDs{}, c.OVNNbTables.Table(&ovnnb.DHCPOptions{}).DeleteFilter(
			context.Background(), "dhcp-options-del", func(row *ovnnb.DHCPOptions) bool {
				if row.ExternalIDs["vendor"] != util.CniTypeName || row.ExternalIDs[ovs.LogicalSwitchKey] != subnet.Name || row.ExternalIDs[ovs.PortKey] != "" {
					return false
				}
				return protocol == "" || row.ExternalIDs["protocol"] == protocol
			},
		)
	}

	cidrBlocks := strings.Split(subnet.Spec.CIDRBlock, ",")
	gateways := strings.Split(subnet.Spec.Gateway, ",")
	if subnet.Status.U2OInterconnectionIP != "" && subnet.Spec.U2OInterconnection {
		gateways = strings.Split(subnet.Status.U2OInterconnectionIP, ",")
	}
	result := &ovs.DHCPOptionsUUIDs{}
	var err error
	switch util.CheckProtocol(subnet.Spec.CIDRBlock) {
	case kubeovnv1.ProtocolIPv4:
		result.DHCPv4OptionsUUID, err = c.updateDHCPOptionTable(subnet.Name, "", cidrBlocks[0], kubeovnv1.ProtocolIPv4, gateways[0], subnet.Spec.DHCPv4Options, mtu)
	case kubeovnv1.ProtocolIPv6:
		result.DHCPv6OptionsUUID, err = c.updateDHCPOptionTable(subnet.Name, "", cidrBlocks[0], kubeovnv1.ProtocolIPv6, "", subnet.Spec.DHCPv6Options, mtu)
	case kubeovnv1.ProtocolDual:
		if len(cidrBlocks) < 2 || len(gateways) < 1 {
			return nil, fmt.Errorf("invalid dual-stack subnet %s", subnet.Name)
		}
		result.DHCPv4OptionsUUID, err = c.updateDHCPOptionTable(subnet.Name, "", cidrBlocks[0], kubeovnv1.ProtocolIPv4, gateways[0], subnet.Spec.DHCPv4Options, mtu)
		if err == nil {
			result.DHCPv6OptionsUUID, err = c.updateDHCPOptionTable(subnet.Name, "", cidrBlocks[1], kubeovnv1.ProtocolIPv6, "", subnet.Spec.DHCPv6Options, mtu)
		}
	default:
		return nil, fmt.Errorf("unsupported subnet protocol %q", subnet.Spec.Protocol)
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Controller) updatePortDHCPOptionsTable(lsName, portName string, subnetDHCP *ovs.DHCPOptionsUUIDs, cidrBlock, gateway, v4Options, v6Options string, mtu int) (*ovs.DHCPOptionsUUIDs, bool, error) {
	if v4Options != "" || v6Options != "" {
		cidrs := strings.Split(cidrBlock, ",")
		gws := strings.Split(gateway, ",")
		result := &ovs.DHCPOptionsUUIDs{}
		protocol := util.CheckProtocol(cidrBlock)
		var err error
		if (protocol == kubeovnv1.ProtocolIPv4 || protocol == kubeovnv1.ProtocolDual) && v4Options != "" {
			gw := ""
			if len(gws) > 0 {
				gw = gws[0]
			}
			result.DHCPv4OptionsUUID, err = c.updateDHCPOptionTable(lsName, portName, cidrs[0], kubeovnv1.ProtocolIPv4, gw, v4Options, mtu)
		}
		if err == nil && (protocol == kubeovnv1.ProtocolIPv6 || protocol == kubeovnv1.ProtocolDual) && v6Options != "" {
			index := 0
			if protocol == kubeovnv1.ProtocolDual {
				index = 1
			}
			result.DHCPv6OptionsUUID, err = c.updateDHCPOptionTable(lsName, portName, cidrs[index], kubeovnv1.ProtocolIPv6, "", v6Options, mtu)
		}
		if err != nil {
			return nil, false, err
		}
		if subnetDHCP != nil {
			if result.DHCPv4OptionsUUID == "" {
				result.DHCPv4OptionsUUID = subnetDHCP.DHCPv4OptionsUUID
			}
			if result.DHCPv6OptionsUUID == "" {
				result.DHCPv6OptionsUUID = subnetDHCP.DHCPv6OptionsUUID
			}
		}
		lsp, err := c.getLogicalSwitchPort(portName, true)
		if err != nil {
			return nil, false, err
		}
		if lsp != nil {
			if err := c.updateLSPDHCPPointersTable(lsp, result); err != nil {
				return nil, false, err
			}
		}
		return result, true, nil
	}

	lsp, err := c.getLogicalSwitchPort(portName, true)
	if err != nil || lsp == nil {
		return subnetDHCP, false, err
	}
	subnetV4, subnetV6 := "", ""
	if subnetDHCP != nil {
		subnetV4, subnetV6 = subnetDHCP.DHCPv4OptionsUUID, subnetDHCP.DHCPv6OptionsUUID
	}
	if ptrString(lsp.Dhcpv4Options) == subnetV4 && ptrString(lsp.Dhcpv6Options) == subnetV6 {
		return subnetDHCP, false, nil
	}
	if err := c.deleteDHCPOptionsForPort(portName); err != nil {
		return nil, false, err
	}
	if err := c.updateLSPDHCPPointersTable(lsp, subnetDHCP); err != nil {
		return nil, false, err
	}
	return subnetDHCP, false, nil
}

func (c *Controller) updateLSPDHCPPointersTable(lsp *ovnnb.LogicalSwitchPort, options *ovs.DHCPOptionsUUIDs) error {
	var v4, v6 *string
	if options != nil {
		if options.DHCPv4OptionsUUID != "" {
			v4 = &options.DHCPv4OptionsUUID
		}
		if options.DHCPv6OptionsUUID != "" {
			v6 = &options.DHCPv6OptionsUUID
		}
	}
	if equalOptionalString(lsp.Dhcpv4Options, v4) && equalOptionalString(lsp.Dhcpv6Options, v6) {
		return nil
	}
	lsp.Dhcpv4Options, lsp.Dhcpv6Options = v4, v6
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).Update(
		context.Background(), "lsp-update", lsp, lsp, &lsp.Dhcpv4Options, &lsp.Dhcpv6Options,
	)
}

func ptrString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

type databaseLifecycle interface {
	Echo(context.Context) error
	Close()
}

type bfdMonitor interface {
	MonitorBFD()
}

func (c *Controller) echoNB(ctx context.Context) error {
	if lifecycle, ok := c.OVNNbTables.(databaseLifecycle); ok {
		return lifecycle.Echo(ctx)
	}
	return c.OVNNbClient.Echo(ctx)
}

func (c *Controller) echoSB(ctx context.Context) error {
	if lifecycle, ok := c.OVNSbTables.(databaseLifecycle); ok {
		return lifecycle.Echo(ctx)
	}
	return c.OVNSbClient.Echo(ctx)
}

func (c *Controller) closeNB() {
	if lifecycle, ok := c.OVNNbTables.(databaseLifecycle); ok {
		lifecycle.Close()
		return
	}
	if c.OVNNbClient != nil {
		c.OVNNbClient.Close()
	}
}

func (c *Controller) closeSB() {
	if lifecycle, ok := c.OVNSbTables.(databaseLifecycle); ok {
		lifecycle.Close()
		return
	}
	if c.OVNSbClient != nil {
		c.OVNSbClient.Close()
	}
}

func (c *Controller) monitorBFD() {
	if monitor, ok := c.OVNNbTables.(bfdMonitor); ok {
		monitor.MonitorBFD()
		return
	}
	if c.OVNNbClient != nil {
		c.OVNNbClient.MonitorBFD()
	}
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

func (c *Controller) createPeerRouterPort(localRouter, remoteRouter, localRouterPortIP string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.CreatePeerRouterPort(localRouter, remoteRouter, localRouterPortIP)
	}
	lrpName := fmt.Sprintf("%s-%s", localRouter, remoteRouter)
	if existing, err := c.getLogicalRouterPort(lrpName, true); err != nil {
		return err
	} else if existing != nil {
		networks := strings.Split(localRouterPortIP, ",")
		if slices.Equal(existing.Networks, networks) {
			return nil
		}
		existing.Networks = networks
		return c.OVNNbTables.Table(&ovnnb.LogicalRouterPort{}).Update(
			context.Background(), "lrp-update", existing, existing, &existing.Networks,
		)
	}
	parent, err := c.getLogicalRouter(localRouter, false)
	if err != nil {
		return err
	}
	peerName := fmt.Sprintf("%s-%s", remoteRouter, localRouter)
	row := &ovnnb.LogicalRouterPort{
		UUID:     ovsclient.NamedUUID(),
		Name:     lrpName,
		MAC:      util.GenerateMac(),
		Networks: strings.Split(localRouterPortIP, ","),
		Peer:     &peerName,
		ExternalIDs: map[string]string{
			"lr": localRouter, "vendor": util.CniTypeName,
		},
	}
	createOps, err := c.OVNNbTables.Table(&ovnnb.LogicalRouterPort{}).CreateOps(row)
	if err != nil {
		return err
	}
	parentOps, err := c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).MutateOps(parent, model.Mutation{
		Field: &parent.Ports, Value: []string{row.UUID}, Mutator: ovsdb.MutateOperationInsert,
	})
	if err != nil {
		return err
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouterPort{}).Transact(
		context.Background(), "lrp-add", append(createOps, parentOps...)...,
	)
}

func (c *Controller) createGatewayChassisesOps(lrp *ovnnb.LogicalRouterPort, chassises []string) ([]ovsdb.Operation, error) {
	if lrp == nil || len(chassises) == 0 {
		return nil, nil
	}
	var operations []ovsdb.Operation
	uuids := make([]string, 0, len(chassises))
	for index, chassisName := range chassises {
		name := lrp.Name + "-" + chassisName
		existing := &ovnnb.GatewayChassis{Name: name}
		if err := c.OVNNbTables.Table(&ovnnb.GatewayChassis{}).Get(context.Background(), existing); err != nil {
			if !errors.Is(err, compat.ErrNotFound) {
				return nil, err
			}
			row := &ovnnb.GatewayChassis{
				UUID:        ovsclient.NamedUUID(),
				Name:        name,
				ChassisName: chassisName,
				Priority:    100 - index,
				ExternalIDs: map[string]string{"lrp": lrp.Name},
			}
			createOps, createErr := c.OVNNbTables.Table(&ovnnb.GatewayChassis{}).CreateOps(row)
			if createErr != nil {
				return nil, createErr
			}
			operations = append(operations, createOps...)
			uuids = append(uuids, row.UUID)
			continue
		}
		uuids = append(uuids, existing.UUID)
	}
	if len(uuids) == 0 {
		return operations, nil
	}
	parentOps, err := c.OVNNbTables.Table(&ovnnb.LogicalRouterPort{}).MutateOps(lrp, model.Mutation{
		Field: &lrp.GatewayChassis, Value: slices.Clip(uuids), Mutator: ovsdb.MutateOperationInsert,
	})
	if err != nil {
		return nil, err
	}
	return append(operations, parentOps...), nil
}

func (c *Controller) reconcileLogicalSwitchPatchPortOps(lsName, lspName, lrpName string) (*ovnnb.LogicalSwitchPort, []ovsdb.Operation, error) {
	ls, err := c.getLogicalSwitch(lsName, false)
	if err != nil {
		return nil, nil, err
	}
	lsp, err := c.getLogicalSwitchPort(lspName, true)
	if err != nil {
		return nil, nil, err
	}
	var operations []ovsdb.Operation
	if lsp == nil {
		lsp = &ovnnb.LogicalSwitchPort{
			UUID:        ovsclient.NamedUUID(),
			Name:        lspName,
			Addresses:   []string{"router"},
			Type:        "router",
			Options:     map[string]string{"router-port": lrpName},
			ExternalIDs: map[string]string{ovs.LogicalSwitchKey: lsName, "vendor": util.CniTypeName},
		}
		createOps, err := c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).CreateOps(lsp)
		if err != nil {
			return nil, nil, err
		}
		operations = append(operations, createOps...)
	}
	parentOps, err := c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).MutateOps(ls, model.Mutation{
		Field: &ls.Ports, Value: []string{lsp.UUID}, Mutator: ovsdb.MutateOperationInsert,
	})
	if err != nil {
		return nil, nil, err
	}
	return lsp, append(operations, parentOps...), nil
}

func (c *Controller) reconcileLogicalRouterPatchPortOps(lrName, lrpName, ip, mac string) (*ovnnb.LogicalRouterPort, []ovsdb.Operation, error) {
	lr, err := c.getLogicalRouter(lrName, false)
	if err != nil {
		return nil, nil, err
	}
	lrp, err := c.getLogicalRouterPort(lrpName, true)
	if err != nil {
		return nil, nil, err
	}
	var operations []ovsdb.Operation
	if lrp == nil {
		lrp = &ovnnb.LogicalRouterPort{
			UUID:        ovsclient.NamedUUID(),
			Name:        lrpName,
			Networks:    strings.Split(ip, ","),
			MAC:         mac,
			ExternalIDs: map[string]string{"lr": lrName, "vendor": util.CniTypeName},
		}
		createOps, err := c.OVNNbTables.Table(&ovnnb.LogicalRouterPort{}).CreateOps(lrp)
		if err != nil {
			return nil, nil, err
		}
		operations = append(operations, createOps...)
	}
	parentOps, err := c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).MutateOps(lr, model.Mutation{
		Field: &lr.Ports, Value: []string{lrp.UUID}, Mutator: ovsdb.MutateOperationInsert,
	})
	if err != nil {
		return nil, nil, err
	}
	return lrp, append(operations, parentOps...), nil
}

func (c *Controller) createLogicalPatchPort(lsName, lrName, lspName, lrpName, ip, mac string, chassises ...string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.CreateLogicalPatchPort(lsName, lrName, lspName, lrpName, ip, mac, chassises...)
	}
	if ip != "" {
		if err := util.CheckCidrs(ip); err != nil {
			return fmt.Errorf("invalid ip %s: %w", ip, err)
		}
	}
	if mac == "" {
		mac = util.GenerateMac()
	}
	_, lspOps, err := c.reconcileLogicalSwitchPatchPortOps(lsName, lspName, lrpName)
	if err != nil {
		return err
	}
	lrp, lrpOps, err := c.reconcileLogicalRouterPatchPortOps(lrName, lrpName, ip, mac)
	if err != nil {
		return err
	}
	operations := append(lspOps, lrpOps...)
	gatewayOps, err := c.createGatewayChassisesOps(lrp, chassises)
	if err != nil {
		return err
	}
	operations = append(operations, gatewayOps...)
	if len(operations) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouterPort{}).Transact(
		context.Background(), "lrp-lsp-add", operations...,
	)
}

func (c *Controller) removeLogicalPatchPort(lspName, lrpName string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.RemoveLogicalPatchPort(lspName, lrpName)
	}
	var operations []ovsdb.Operation
	if lsp, err := c.getLogicalSwitchPort(lspName, true); err != nil {
		return err
	} else if lsp != nil {
		ops, opErr := c.logicalSwitchPortDeleteOps(lsp)
		if opErr != nil {
			return opErr
		}
		operations = append(operations, ops...)
	}
	if lrp, err := c.getLogicalRouterPort(lrpName, true); err != nil {
		return err
	} else if lrp != nil {
		ops, opErr := c.logicalRouterPortDeleteOps(lrp)
		if opErr != nil {
			return opErr
		}
		operations = append(operations, ops...)
	}
	if len(operations) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouterPort{}).Transact(
		context.Background(), "lrp-lsp-del", operations...,
	)
}

// buildLogicalSwitchPortModel constructs the normal LSP row without coupling
// controller reconcile code to the legacy ovs client implementation.
func buildLogicalSwitchPortModel(lsName, lspName, ip, mac, podName, namespace string, portSecurity bool, securityGroups, vips string, enableDHCP bool, dhcpOptions *ovs.DHCPOptionsUUIDs, vpc string) *ovnnb.LogicalSwitchPort {
	row := &ovnnb.LogicalSwitchPort{
		UUID:        ovsclient.NamedUUID(),
		Name:        lspName,
		ExternalIDs: make(map[string]string),
	}
	ipList := strings.Split(ip, ",")
	vipList := strings.Split(vips, ",")
	addresses := make([]string, 0, len(ipList)+len(vipList)+1)
	addresses = append(addresses, mac)
	addresses = append(addresses, ipList...)
	if ip == "" {
		row.Addresses = []string{mac}
	} else {
		row.Addresses = []string{strings.TrimSpace(strings.Join(addresses, " "))}
		if portSecurity {
			if vips != "" {
				addresses = append(addresses, vipList...)
			}
			row.PortSecurity = []string{strings.TrimSpace(strings.Join(addresses, " "))}
		}
	}
	row.ExternalIDs["vendor"] = util.CniTypeName
	if securityGroups != "" {
		row.ExternalIDs[sgsKey] = strings.ReplaceAll(securityGroups, ",", "/")
		for sg := range strings.SplitSeq(securityGroups, ",") {
			row.ExternalIDs["associated_sg_"+sg] = "true"
		}
	}
	if vpc != "" && vpc != util.DefaultVpc && !strings.Contains(securityGroups, util.DefaultSecurityGroupName) {
		row.ExternalIDs["associated_sg_"+util.DefaultSecurityGroupName] = "false"
	}
	if vips != "" {
		row.ExternalIDs["vips"] = vips
		row.ExternalIDs["attach-vips"] = "true"
	}
	if podName != "" && namespace != "" {
		row.ExternalIDs["pod"] = namespace + "/" + podName
	}
	row.ExternalIDs[ovs.LogicalSwitchKey] = lsName
	if enableDHCP && dhcpOptions != nil {
		if dhcpOptions.DHCPv4OptionsUUID != "" {
			row.Dhcpv4Options = &dhcpOptions.DHCPv4OptionsUUID
		}
		if dhcpOptions.DHCPv6OptionsUUID != "" {
			row.Dhcpv6Options = &dhcpOptions.DHCPv6OptionsUUID
		}
	}
	return row
}

func equalOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

// createLogicalSwitchPort reconciles a normal LSP row and its parent switch
// reference. Complex DHCP reconciliation remains in the OVS compatibility
// fallback when no TableProvider is installed.
func (c *Controller) createLogicalSwitchPort(lsName, lspName, ip, mac, podName, namespace string, portSecurity bool, securityGroups, vips string, enableDHCP bool, dhcpOptions *ovs.DHCPOptionsUUIDs, vpc string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.CreateLogicalSwitchPort(lsName, lspName, ip, mac, podName, namespace, portSecurity, securityGroups, vips, enableDHCP, dhcpOptions, vpc)
	}
	existing, err := c.getLogicalSwitchPort(lspName, true)
	if err != nil {
		return err
	}
	desired := buildLogicalSwitchPortModel(lsName, lspName, ip, mac, podName, namespace, portSecurity, securityGroups, vips, enableDHCP, dhcpOptions, vpc)
	if existing != nil && existing.ExternalIDs[ovs.LogicalSwitchKey] == lsName {
		desired.UUID = existing.UUID
		if maps.Equal(existing.ExternalIDs, desired.ExternalIDs) && slices.Equal(existing.Addresses, desired.Addresses) &&
			slices.Equal(existing.PortSecurity, desired.PortSecurity) && equalOptionalString(existing.Dhcpv4Options, desired.Dhcpv4Options) &&
			equalOptionalString(existing.Dhcpv6Options, desired.Dhcpv6Options) {
			return nil
		}
		return c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).Update(
			context.Background(), "lsp-update", existing, desired,
			&desired.Addresses, &desired.Dhcpv4Options, &desired.Dhcpv6Options, &desired.PortSecurity, &desired.ExternalIDs,
		)
	}
	var operations []ovsdb.Operation
	if existing != nil {
		detachOps, err := c.logicalSwitchPortDeleteOps(existing)
		if err != nil {
			return fmt.Errorf("generate operations for moving logical switch port %s from logical switch %s: %w", lspName, existing.ExternalIDs[ovs.LogicalSwitchKey], err)
		}
		operations = append(operations, detachOps...)
	}
	parent, err := c.getLogicalSwitch(lsName, false)
	if err != nil {
		return err
	}
	createOps, err := c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).CreateOps(desired)
	if err != nil {
		return fmt.Errorf("generate operations for creating logical switch port %s: %w", lspName, err)
	}
	parentOps, err := c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).MutateOps(parent, model.Mutation{
		Field: &parent.Ports, Value: []string{desired.UUID}, Mutator: ovsdb.MutateOperationInsert,
	})
	if err != nil {
		return fmt.Errorf("generate operations for adding logical switch port %s to logical switch %s: %w", lspName, lsName, err)
	}
	operations = append(operations, createOps...)
	operations = append(operations, parentOps...)
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).Transact(
		context.Background(), "lsp-add", operations...,
	)
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
		// Keep the legacy call for tests and upgrades that have not installed
		// the table provider yet. Production wiring always sets OVNNbTables.
		if c.OVNNbClient == nil {
			return errors.New("OVN NB table provider is nil")
		}
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
		// A provider may acknowledge the transaction before its monitor cache
		// observes the inserted row. The named UUID remains valid for callers
		// composing the next operation, so return the created model in that case.
		return row, nil
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

func (c *Controller) deleteLogicalGatewaySwitch(lsName, lrName string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteLogicalGatewaySwitch(lsName, lrName)
	}
	var operations []ovsdb.Operation
	if ls, err := c.getLogicalSwitch(lsName, true); err != nil {
		return err
	} else if ls != nil {
		ops, opErr := c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).DeleteOps(ls)
		if opErr != nil {
			return opErr
		}
		operations = append(operations, ops...)
	}
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)
	if lrp, err := c.getLogicalRouterPort(lrpName, true); err != nil {
		return err
	} else if lrp != nil {
		ops, opErr := c.logicalRouterPortDeleteOps(lrp)
		if opErr != nil {
			return opErr
		}
		operations = append(operations, ops...)
	}
	if len(operations) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Transact(
		context.Background(), "gw-ls-del", operations...,
	)
}

func (c *Controller) updateLogicalRouterPolicy(policy *ovnnb.LogicalRouterPolicy, fields ...any) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.UpdateLogicalRouterPolicy(policy, fields...)
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouterPolicy{}).Update(
		context.Background(), "lr-policy-update", policy, policy, fields...,
	)
}

func equalStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]struct{}, len(left))
	for _, value := range left {
		seen[value] = struct{}{}
	}
	for _, value := range right {
		if _, ok := seen[value]; !ok {
			return false
		}
	}
	return true
}

func logicalRouterPolicyMatches(existing, desired *ovnnb.LogicalRouterPolicy) bool {
	if existing.Priority != desired.Priority || existing.Match != desired.Match || existing.Action != desired.Action {
		return false
	}
	return existing.Action != ovnnb.LogicalRouterPolicyActionReroute || equalStringSet(existing.Nexthops, desired.Nexthops)
}

// addLogicalRouterPolicy reconciles one policy and its parent router reference.
func (c *Controller) addLogicalRouterPolicy(lrName string, priority int, match, action string, nextHops, bfdSessions []string, externalIDs map[string]string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.AddLogicalRouterPolicy(lrName, priority, match, action, nextHops, bfdSessions, externalIDs)
	}
	lr, err := c.getLogicalRouter(lrName, false)
	if err != nil {
		return err
	}
	existing, err := c.listLogicalRouterPoliciesWithFilter(lrName, func(row *ovnnb.LogicalRouterPolicy) bool {
		return row.Priority == priority && row.Match == match
	})
	if err != nil {
		return err
	}
	desired := &ovnnb.LogicalRouterPolicy{
		Priority:    priority,
		Match:       match,
		Action:      action,
		Nexthops:    nextHops,
		BFDSessions: bfdSessions,
		ExternalIDs: maps.Clone(externalIDs),
	}
	var found *ovnnb.LogicalRouterPolicy
	deleteUUIDs := make([]string, 0, len(existing))
	for _, row := range existing {
		if found == nil && logicalRouterPolicyMatches(row, desired) {
			found = row
			continue
		}
		deleteUUIDs = append(deleteUUIDs, row.UUID)
	}
	mutations := make([]model.Mutation, 0, 2)
	if len(deleteUUIDs) != 0 {
		mutations = append(mutations, model.Mutation{
			Field: &lr.Policies, Value: slices.Clip(deleteUUIDs), Mutator: ovsdb.MutateOperationDelete,
		})
	}
	var operations []ovsdb.Operation
	if found == nil {
		desired.UUID = ovsclient.NamedUUID()
		createOps, createErr := c.OVNNbTables.Table(&ovnnb.LogicalRouterPolicy{}).CreateOps(desired)
		if createErr != nil {
			return createErr
		}
		operations = append(operations, createOps...)
		mutations = append(mutations, model.Mutation{
			Field: &lr.Policies, Value: []string{desired.UUID}, Mutator: ovsdb.MutateOperationInsert,
		})
	} else if !maps.Equal(found.ExternalIDs, externalIDs) {
		updated := *found
		updated.ExternalIDs = maps.Clone(externalIDs)
		updateOps, updateErr := c.OVNNbTables.Table(&ovnnb.LogicalRouterPolicy{}).UpdateOps(found, &updated, &updated.ExternalIDs)
		if updateErr != nil {
			return updateErr
		}
		operations = append(operations, updateOps...)
	}
	if len(mutations) != 0 {
		parentOps, parentErr := c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).MutateOps(lr, mutations...)
		if parentErr != nil {
			return parentErr
		}
		operations = append(operations, parentOps...)
	}
	if len(operations) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouterPolicy{}).Transact(
		context.Background(), "lr-policy-reconcile", operations...,
	)
}

func (c *Controller) batchAddLogicalRouterPolicies(lrName string, policies []*ovnnb.LogicalRouterPolicy) error {
	if c.OVNNbTables == nil {
		return errors.New("OVN NB table provider is nil")
	}
	if len(policies) == 0 {
		return nil
	}
	for _, policy := range policies {
		if policy == nil {
			continue
		}
		if err := c.addLogicalRouterPolicy(lrName, policy.Priority, policy.Match, policy.Action, policy.Nexthops, policy.BFDSessions, policy.ExternalIDs); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) batchDeleteLogicalRouterPolicies(lrName string, policies []*ovnnb.LogicalRouterPolicy) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.BatchDeleteLogicalRouterPolicy(lrName, policies)
	}
	if len(policies) == 0 {
		return nil
	}
	lr, err := c.getLogicalRouter(lrName, false)
	if err != nil {
		return err
	}
	wanted := make(map[string]struct{}, len(policies))
	for _, policy := range policies {
		if policy != nil {
			wanted[fmt.Sprintf("%d\x00%s", policy.Priority, policy.Match)] = struct{}{}
		}
	}
	rows, err := c.listLogicalRouterPoliciesWithFilter(lrName, func(row *ovnnb.LogicalRouterPolicy) bool {
		_, ok := wanted[fmt.Sprintf("%d\x00%s", row.Priority, row.Match)]
		return ok
	})
	if err != nil {
		return err
	}
	uuids := make([]string, 0, len(rows))
	for _, row := range rows {
		uuids = append(uuids, row.UUID)
	}
	if len(uuids) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Mutate(
		context.Background(), "lr-policies-del", lr,
		model.Mutation{Field: &lr.Policies, Value: slices.Clip(uuids), Mutator: ovsdb.MutateOperationDelete},
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

func (c *Controller) ensureSnat(lrName, externalIP, logicalIP string) error {
	if c.OVNNbTables == nil {
		return errors.New("OVN NB table provider is nil")
	}
	if externalIP == "" {
		return errors.New("snat external ip is required")
	}
	if logicalIP == "" {
		return errors.New("snat logical ip is required")
	}
	return c.addNat(lrName, ovnnb.NATTypeSNAT, externalIP, logicalIP, "", "", nil)
}

func (c *Controller) updateDnatAndSnat(lrName, externalIP, logicalIP, lspName, externalMac, gatewayType string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.UpdateDnatAndSnat(lrName, externalIP, logicalIP, lspName, externalMac, gatewayType)
	}
	if externalIP == "" || logicalIP == "" {
		return errors.New("nat external ip and logical ip are required")
	}
	nats, err := c.listNATs(lrName, ovnnb.NATTypeDNATAndSNAT, "", nil)
	if err != nil {
		return err
	}
	var nat *ovnnb.NAT
	for _, candidate := range nats {
		if candidate.ExternalIP == externalIP {
			nat = candidate
			break
		}
	}
	distributed := gatewayType == "distributed"
	if nat != nil {
		if !distributed {
			return nil
		}
		if equalOptionalString(nat.LogicalPort, &lspName) && equalOptionalString(nat.ExternalMAC, &externalMac) {
			return nil
		}
		nat.LogicalPort = new(lspName)
		nat.ExternalMAC = new(externalMac)
		return c.OVNNbTables.Table(&ovnnb.NAT{}).Update(
			context.Background(), "nat-update", nat, nat, &nat.LogicalPort, &nat.ExternalMAC,
		)
	}
	options := map[string]string(nil)
	if distributed {
		options = map[string]string{"stateless": "true"}
	}
	return c.addNat(lrName, ovnnb.NATTypeDNATAndSNAT, externalIP, logicalIP, externalMac, lspName, options)
}

// setLoadBalancerOption updates one option on a load balancer without
// replacing options managed by another reconcile path.
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
		rows, err := c.OVNNbClient.ListLogicalSwitch(false, func(row *ovnnb.LogicalSwitch) bool { return row.Name == name })
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
		return nil, errors.New("not found NB_Global")
	case 1:
		return &rows[0], nil
	default:
		return nil, errors.New("more than one NB_Global row")
	}
}

func (c *Controller) setNBGlobalOption(key, value string, present bool, legacy ...func() error) error {
	if c.OVNNbTables == nil {
		if len(legacy) != 0 && legacy[0] != nil {
			return legacy[0]()
		}
		return errors.New("OVN NB table provider is nil")
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

func (c *Controller) setNBGlobalIPSec(enabled bool, legacy ...func() error) error {
	if c.OVNNbTables == nil {
		if len(legacy) != 0 && legacy[0] != nil {
			return legacy[0]()
		}
		return errors.New("OVN NB table provider is nil")
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
	parentOps, err := c.OVNNbTables.Table(&ovnnb.LoadBalancer{}).MutateOps(lb, model.Mutation{
		Field: &lb.HealthCheck, Value: []string{uuid}, Mutator: ovsdb.MutateOperationDelete,
	})
	if err != nil {
		return err
	}
	childOps, err := c.OVNNbTables.Table(&ovnnb.LoadBalancerHealthCheck{}).DeleteOps(
		&ovnnb.LoadBalancerHealthCheck{UUID: uuid},
	)
	if err != nil {
		return err
	}
	return c.OVNNbTables.Table(&ovnnb.LoadBalancer{}).Transact(
		context.Background(), "lb-hc-del", append(parentOps, childOps...)...,
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
		filtered := make([]*ovnnb.LogicalRouterPolicy, 0, len(rows))
		for i := range rows {
			if filter == nil || filter(rows[i]) {
				filtered = append(filtered, rows[i])
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
