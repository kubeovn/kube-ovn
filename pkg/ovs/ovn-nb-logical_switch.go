package ovs

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/nbops"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// CreateLogicalSwitch create logical switch
func (c *OVNNbClient) CreateLogicalSwitch(lsName, lrName, cidrBlock, gateway, gatewayMAC string, needRouter, randomAllocateGW bool) error {
	lspName := fmt.Sprintf("%s-%s", lsName, lrName)
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)

	// underlay subnets without a CIDR (e.g. BYO-DHCP / external DHCP) have no
	// router-side networks to configure, so skip address/mask computation
	var switchNetworks string
	if cidrBlock != "" {
		networks, err := util.GetIPAddrWithMask(gateway, cidrBlock)
		if err != nil {
			klog.Errorf("failed to get ip %s with mask %s, %v", gateway, cidrBlock, err)
			return err
		}
		switchNetworks = networks
	}

	exist, err := c.LogicalSwitchExists(lsName)
	if err != nil {
		return logErr(err)
	}

	// only update logical router port networks when logical switch exist
	if exist && switchNetworks != "" {
		if randomAllocateGW {
			return nil
		}

		lrp := &ovnnb.LogicalRouterPort{
			Name:     lrpName,
			Networks: strings.Split(switchNetworks, ","),
		}
		fields := []any{&lrp.Networks}
		if gatewayMAC != "" {
			lrp.MAC = gatewayMAC
			fields = append(fields, &lrp.MAC)
		}
		if err := c.UpdateLogicalRouterPort(lrp, fields...); err != nil {
			klog.Error(err)
			return fmt.Errorf("update logical router port %s", lrpName)
		}
	} else {
		if err := c.CreateBareLogicalSwitch(lsName); err != nil {
			klog.Error(err)
			return fmt.Errorf("create logical switch %s", lsName)
		}
	}

	if needRouter && switchNetworks != "" {
		if err := c.CreateLogicalPatchPort(lsName, lrName, lspName, lrpName, switchNetworks, gatewayMAC); err != nil {
			return logErr(err)
		}
	} else {
		if randomAllocateGW {
			return nil
		}

		if err := c.RemoveLogicalPatchPort(lspName, lrpName); err != nil {
			return logWrap(err, wrapErr("remove router type port %s and %s: %w", lspName, lrpName))
		}
	}

	return nil
}

// CreateBareLogicalSwitch create logical switch with basic configuration
func (c *OVNNbClient) CreateBareLogicalSwitch(lsName string) error {
	if err := requireName(lsName, "empty logical switch name"); err != nil {
		return err
	}

	ls := &ovnnb.LogicalSwitch{
		Name:        lsName,
		ExternalIDs: map[string]string{"vendor": util.CniTypeName},
	}
	return logErr(c.logicalSwitchTable().createIfAbsent(lsName, "ls-add", ls))
}

// LogicalSwitchAddPort add port to logical switch
func (c *OVNNbClient) LogicalSwitchAddPort(lsName, lspName string) error {
	if _, err := c.GetLogicalSwitchPort(lspName, false); err != nil {
		return logWrap(err, wrapErr("get logical switch port %s when logical switch add port: %w", lspName))
	}
	return c.EnsureLogicalSwitchPortParent(lspName, lsName)
}

// EnsureLogicalSwitchPortParent reconciles a port's ownership across all
// logical switches. LogicalSwitchAddPort uses this intent-level operation so
// stale parents are detached before the desired parent is attached.
func (c *OVNNbClient) EnsureLogicalSwitchPortParent(lspName, lsName string) error {
	ctx, cancel := timeoutCtx(c.Database)
	defer cancel()
	return nbops.NewLogicalSwitchPorts(c.Database, c.Database).EnsureParent(ctx, lspName, lsName)
}

// LogicalSwitchDelPort del port from logical switch
func (c *OVNNbClient) LogicalSwitchDelPort(lsName, lspName string) error {
	lsp, err := c.GetLogicalSwitchPort(lspName, true)
	if err != nil {
		return logWrap(err, wrapErr("get logical switch port %s when logical switch del port: %w", lspName))
	}

	if lsp == nil {
		return nil
	}

	ops, err := c.LogicalSwitchUpdatePortOp(lsName, lsp.UUID, ovsdb.MutateOperationDelete)
	return c.transactGenerated("lsp-del", ops, err,
		wrapErr("generate operations for logical switch %s del port %s: %w", lsName, lspName),
		wrapErr("del port %s from logical switch %s: %w", lspName, lsName),
	)
}

// LogicalSwitchUpdateLoadBalancers add several lb to or from logical switch once
func (c *OVNNbClient) LogicalSwitchUpdateLoadBalancers(lsName string, op ovsdb.Mutator, lbNames ...string) error {
	if len(lbNames) == 0 {
		return nil
	}

	lbUUIDs, err := c.loadBalancerUUIDs(lbNames...)
	if err != nil {
		return err
	}

	ops, err := c.LogicalSwitchUpdateLoadBalancerOp(lsName, lbUUIDs, op)
	return c.transactGenerated("ls-lb-update", ops, err,
		wrapErr("generate operations for logical switch %s update lbs %v: %w", lsName, lbNames),
		wrapErr("logical switch %s update lbs %v: %w", lsName, lbNames),
	)
}

// LogicalSwitchUpdateOtherConfig add other config to or from logical switch once
func (c *OVNNbClient) LogicalSwitchUpdateOtherConfig(lsName string, op ovsdb.Mutator, otherConfig map[string]string) error {
	if len(otherConfig) == 0 {
		return nil
	}

	ops, err := c.LogicalSwitchUpdateOtherConfigOp(lsName, otherConfig, op)
	return c.transactGenerated("ls-other-config-update", ops, err,
		wrapErr("generate operations for logical switch %s update other config %v: %w", lsName, otherConfig),
		wrapErr("logical switch %s update other config %v: %w", lsName, otherConfig),
	)
}

// DeleteLogicalSwitch delete logical switch
func (c *OVNNbClient) DeleteLogicalSwitch(lsName string) error {
	op, err := c.DeleteLogicalSwitchOp(lsName)
	return c.transactGenerated("ls-del", op, err, nil, wrapErr("delete logical switch %s: %w", lsName))
}

// GetLogicalSwitch get logical switch by name,
// it is because of lack of name index that doesn't use OVNNbClient.Get
func (c *OVNNbClient) GetLogicalSwitch(lsName string, ignoreNotFound bool) (*ovnnb.LogicalSwitch, error) {
	if err := requireName(lsName, "empty logical switch name"); err != nil {
		return nil, err
	}
	return logRet(c.logicalSwitchTable().get(lsName, ignoreNotFound))
}

func (c *OVNNbClient) LogicalSwitchExists(lsName string) (bool, error) {
	return existsByGet(c.GetLogicalSwitch, lsName)
}

// ListLogicalSwitch list logical switch
func (c *OVNNbClient) ListLogicalSwitch(needVendorFilter bool, filter func(ls *ovnnb.LogicalSwitch) bool) ([]ovnnb.LogicalSwitch, error) {
	return logRet(c.logicalSwitchTable().list(needVendorFilter, filter))
}

// ListLogicalSwitchNames list logical switch names
func (c *OVNNbClient) ListLogicalSwitchNames(needVendorFilter bool, filter func(ls *ovnnb.LogicalSwitch) bool) ([]string, error) {
	lsList, err := c.ListLogicalSwitch(needVendorFilter, filter)
	return namesFrom(lsList, err, c.logicalSwitchTable().names)
}

// LogicalSwitchUpdatePortOp create operations add port to or delete port from logical switch
func (c *OVNNbClient) LogicalSwitchUpdatePortOp(lsName, lspUUID string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	return c.logicalSwitchTable().mutateOwnedUUID(lsName, lspUUID, op,
		func(ls *ovnnb.LogicalSwitch, uuid string) bool { return slices.Contains(ls.Ports, uuid) },
		func(ls *ovnnb.LogicalSwitch) *[]string { return &ls.Ports },
		"LSP", "LS")
}

// LogicalSwitchUpdateOtherConfigOp create operations add otherConfig to or delete otherConfig from logical switch
func (c *OVNNbClient) LogicalSwitchUpdateOtherConfigOp(lsName string, otherConfig map[string]string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	return c.logicalSwitchTable().mutateMap(lsName, func(ls *ovnnb.LogicalSwitch) *map[string]string { return &ls.OtherConfig }, otherConfig, op)
}

// LogicalSwitchUpdateLoadBalancerOp create operations add lb to or delete lb from logical switch
func (c *OVNNbClient) LogicalSwitchUpdateLoadBalancerOp(lsName string, lbUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	return c.logicalSwitchTable().mutateUUIDs(lsName, func(ls *ovnnb.LogicalSwitch) *[]string { return &ls.LoadBalancer }, lbUUIDs, op)
}

// logicalSwitchUpdateACLOp create operations add acl to or delete acl from logical switch
func (c *OVNNbClient) logicalSwitchUpdateACLOp(lsName string, aclUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	return c.logicalSwitchTable().mutateUUIDs(lsName, func(ls *ovnnb.LogicalSwitch) *[]string { return &ls.ACLs }, aclUUIDs, op)
}

// LogicalSwitchOp create operations about logical switch
func (c *OVNNbClient) LogicalSwitchOp(lsName string, mutationsFunc ...func(ls *ovnnb.LogicalSwitch) *model.Mutation) ([]ovsdb.Operation, error) {
	return c.logicalSwitchTable().mutateNamed(lsName, mutationsFunc...)
}

// DeleteLogicalSwitchOp create operations that delete logical switch
func (c *OVNNbClient) DeleteLogicalSwitchOp(lsName string) ([]ovsdb.Operation, error) {
	table := c.logicalSwitchTable()
	ls, err := c.GetLogicalSwitch(lsName, true)
	if err != nil {
		return nil, logWrap(err, wrapErr("get logical switch %s: %w", lsName))
	}
	if ls == nil {
		return nil, nil
	}

	op, err := table.delete(ls)
	if err != nil {
		return nil, logWrap(err, wrapErr("generate operations for deleting logical switch %s: %w", lsName))
	}

	return op, nil
}

func (c *OVNNbClient) logicalSwitchTable() namedTable[ovnnb.LogicalSwitch] {
	return newNamedTable(c.Database, &ovnnb.LogicalSwitch{}, "logical switch",
		func(ls *ovnnb.LogicalSwitch) string { return ls.Name },
		func(ls *ovnnb.LogicalSwitch) map[string]string { return ls.ExternalIDs })
}
