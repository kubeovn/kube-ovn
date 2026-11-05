package ovs

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type DHCPOptionsUUIDs struct {
	DHCPv4OptionsUUID string
	DHCPv6OptionsUUID string
}

func (c *OVNNbClient) CreateDHCPOptions(lsName, cidr, options string) error {
	return c.createDHCPEntry(lsName, "", cidr, options)
}

func (c *OVNNbClient) UpdateDHCPOptions(subnet *kubeovnv1.Subnet, mtu int, routerPortNames ...string) (*DHCPOptionsUUIDs, error) {
	lsName := subnet.Name
	cidrBlock := subnet.Spec.CIDRBlock
	gateway := subnet.Spec.Gateway
	if subnet.Status.U2OInterconnectionIP != "" && subnet.Spec.U2OInterconnection {
		gateway = subnet.Status.U2OInterconnectionIP
	}
	enableDHCP := subnet.Spec.EnableDHCP
	var ops []ovsdb.Operation
	for _, portName := range routerPortNames {
		raOps, err := c.logicalRouterPortRAOp(portName, subnet.Spec.IPv6RAConfigs, subnet.Spec.EnableIPv6RA)
		if err != nil {
			return nil, err
		}
		ops = append(ops, raOps...)
	}

	/* delete dhcp options */
	if !enableDHCP {
		deleteOps, err := c.DeleteDHCPOptionsOp(lsName, subnet.Spec.Protocol, true)
		if err != nil {
			return nil, err
		}
		ops = append(ops, deleteOps...)
		if err := c.Transact("subnet-dhcp-options-update", ops); err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("delete dhcp options for logical switch %s: %w", lsName, err)
		}
		return &DHCPOptionsUUIDs{}, nil
	}

	/* update dhcp options*/
	var v4CIDR, v6CIDR string
	var v4Gateway string
	switch util.CheckProtocol(cidrBlock) {
	case kubeovnv1.ProtocolIPv4:
		v4CIDR = cidrBlock
		v4Gateway = gateway
	case kubeovnv1.ProtocolIPv6:
		v6CIDR = cidrBlock
	case kubeovnv1.ProtocolDual:
		cidrBlocks := strings.Split(cidrBlock, ",")
		gateways := strings.Split(gateway, ",")
		v4CIDR, v6CIDR = cidrBlocks[0], cidrBlocks[1]
		v4Gateway = gateways[0]
	}

	dhcpOptionsUUIDs := &DHCPOptionsUUIDs{}
	if len(v4CIDR) != 0 {
		v4Ops, err := c.subnetDHCPOptionsOp(lsName, v4CIDR, v4Gateway, subnet.Spec.DHCPv4Options, mtu)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("update IPv4 dhcp options for logical switch %s: %w", lsName, err)
		}
		ops = append(ops, v4Ops...)
	}

	if len(v6CIDR) != 0 {
		v6Ops, err := c.subnetDHCPOptionsOp(lsName, v6CIDR, "", subnet.Spec.DHCPv6Options, mtu)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("update IPv6 dhcp options for logical switch %s: %w", lsName, err)
		}
		ops = append(ops, v6Ops...)
	}
	if err := c.Transact("subnet-dhcp-options-update", ops); err != nil {
		return nil, fmt.Errorf("update DHCP options for logical switch %s: %w", lsName, err)
	}
	for _, family := range []struct {
		cidr     string
		protocol string
		uuid     *string
	}{
		{v4CIDR, kubeovnv1.ProtocolIPv4, &dhcpOptionsUUIDs.DHCPv4OptionsUUID},
		{v6CIDR, kubeovnv1.ProtocolIPv6, &dhcpOptionsUUIDs.DHCPv6OptionsUUID},
	} {
		if family.cidr != "" {
			entry, err := c.GetDHCPOptions(lsName, family.protocol, false)
			if err != nil {
				return nil, err
			}
			*family.uuid = entry.UUID
		}
	}

	return dhcpOptionsUUIDs, nil
}

func (c *OVNNbClient) subnetDHCPOptionsOp(lsName, cidr, gateway, options string, mtu int) ([]ovsdb.Operation, error) {
	protocol := util.CheckProtocol(cidr)
	if protocol != kubeovnv1.ProtocolIPv4 && protocol != kubeovnv1.ProtocolIPv6 {
		return nil, fmt.Errorf("invalid DHCP cidr %s", cidr)
	}
	entry, err := c.GetDHCPOptions(lsName, protocol, true)
	if err != nil {
		return nil, err
	}
	create := entry == nil
	if create {
		entry, err = newDHCPOptionsEntry(lsName, "", cidr, "")
		if err != nil {
			return nil, err
		}
		entry.UUID = ovsclient.NamedUUID()
	}
	macKey := "server_mac"
	if protocol == kubeovnv1.ProtocolIPv6 {
		macKey = "server_id"
	}
	mac := entry.Options[macKey]
	if create {
		mac = util.GenerateMac()
	}
	entry.Cidr = cidr
	if protocol == kubeovnv1.ProtocolIPv4 {
		entry.Options = buildDHCPv4Options(options, gateway, mac, mtu, []string{"lease_time", "router", "server_id", "server_mac", "mtu"})
	} else {
		entry.Options = buildDHCPv6Options(options, mac, []string{"server_id"})
	}
	if create {
		return c.Create(entry)
	}
	return c.Where(entry).Update(entry, &entry.Cidr, &entry.Options)
}

// UpdateDHCPOptionsForPort creates or updates per-port DHCP_Options for a logical switch port.
// cidrBlock and gateway may be comma-separated dual-stack values.
// v4Options/v6Options are the annotation values that fully override the subnet-level options.
// A family is only processed when its options string is non-empty, which signals that the
// corresponding per-pod annotation was set. At least one of v4Options/v6Options must be
// non-empty. Unset families return an empty UUID so callers can fall back to subnet-level.
func (c *OVNNbClient) UpdateDHCPOptionsForPort(lsName, portName, cidrBlock, gateway, v4Options, v6Options string, mtu int) (*DHCPOptionsUUIDs, error) {
	if lsName == "" {
		return nil, errors.New("the logical switch name is required")
	}
	if portName == "" {
		return nil, errors.New("the port name is required")
	}
	if v4Options == "" && v6Options == "" {
		return nil, errors.New("at least one of v4Options or v6Options must be non-empty")
	}

	var v4CIDR, v6CIDR, v4Gateway string
	switch util.CheckProtocol(cidrBlock) {
	case kubeovnv1.ProtocolIPv4:
		v4CIDR = cidrBlock
		v4Gateway = gateway
	case kubeovnv1.ProtocolIPv6:
		v6CIDR = cidrBlock
	case kubeovnv1.ProtocolDual:
		cidrBlocks := strings.Split(cidrBlock, ",")
		gateways := strings.Split(gateway, ",")
		if len(cidrBlocks) < 2 || len(gateways) < 1 {
			return nil, fmt.Errorf("invalid dual-stack cidrBlock %q or gateway %q", cidrBlock, gateway)
		}
		v4CIDR, v6CIDR = cidrBlocks[0], cidrBlocks[1]
		v4Gateway = gateways[0]
	}

	dhcpOptionsUUIDs := &DHCPOptionsUUIDs{}
	// Only process a family when the corresponding annotation is set (options non-empty).
	if len(v4CIDR) != 0 && v4Options != "" {
		uuid, err := c.updateDHCPv4Options(lsName, portName, v4CIDR, v4Gateway, v4Options, mtu)
		if err != nil {
			return nil, logWrap(err, wrapErr("update per-port IPv4 dhcp options for port %s: %w", portName))
		}
		dhcpOptionsUUIDs.DHCPv4OptionsUUID = uuid
	}

	if len(v6CIDR) != 0 && v6Options != "" {
		uuid, err := c.updateDHCPv6Options(lsName, portName, v6CIDR, v6Options)
		if err != nil {
			return nil, logWrap(err, wrapErr("update per-port IPv6 dhcp options for port %s: %w", portName))
		}
		dhcpOptionsUUIDs.DHCPv6OptionsUUID = uuid
	}

	return dhcpOptionsUUIDs, nil
}

// updateDHCPv4Options creates or updates a DHCPv4 DHCP_Options record.
// When portName is empty the record is subnet-scoped; when non-empty it is per-port.
func (c *OVNNbClient) upsertDHCPOptions(lsName, portName, cidr, protocol string, buildUpdate func(*ovnnb.DHCPOptions) map[string]string, buildCreate func(mac string) string) (string, error) {
	dhcpOpt, err := c.getDHCPOptionsEntry(lsName, portName, protocol, true)
	if err != nil {
		klog.Error(err)
		return "", err
	}
	if dhcpOpt != nil {
		dhcpOpt.Cidr = cidr
		dhcpOpt.Options = buildUpdate(dhcpOpt)
		return dhcpOpt.UUID, c.updateDHCPOptions(dhcpOpt, &dhcpOpt.Cidr, &dhcpOpt.Options)
	}

	if err := c.createDHCPEntry(lsName, portName, cidr, buildCreate(util.GenerateMac())); err != nil {
		klog.Error(err)
		return "", fmt.Errorf("create dhcp options: %w", err)
	}
	dhcpOpt, err = c.getDHCPOptionsEntry(lsName, portName, protocol, false)
	if err != nil {
		klog.Error(err)
		return "", err
	}
	return dhcpOpt.UUID, nil
}

func (c *OVNNbClient) updateDHCPv4Options(lsName, portName, cidr, gateway, options string, mtu int) (string, error) {
	necessaryV4DHCPOptions := []string{"lease_time", "router", "server_id", "server_mac", "mtu"}
	protocol := util.CheckProtocol(cidr)
	if protocol != kubeovnv1.ProtocolIPv4 {
		return "", fmt.Errorf("cidr %s must be a valid ipv4 address", cidr)
	}
	return c.upsertDHCPOptions(lsName, portName, cidr, protocol,
		func(dhcpOpt *ovnnb.DHCPOptions) map[string]string {
			return buildDHCPv4Options(options, gateway, dhcpOpt.Options["server_mac"], mtu, necessaryV4DHCPOptions)
		},
		func(mac string) string {
			return formatDHCPOptions(buildDHCPv4Options(options, gateway, mac, mtu, necessaryV4DHCPOptions))
		},
	)
}

func (c *OVNNbClient) updateDHCPv6Options(lsName, portName, cidr, options string) (string, error) {
	necessaryV6DHCPOptions := []string{"server_id"}
	protocol := util.CheckProtocol(cidr)
	if protocol != kubeovnv1.ProtocolIPv6 {
		return "", fmt.Errorf("cidr %s must be a valid ipv6 address", cidr)
	}
	return c.upsertDHCPOptions(lsName, portName, cidr, protocol,
		func(dhcpOpt *ovnnb.DHCPOptions) map[string]string {
			return buildDHCPv6Options(options, dhcpOpt.Options["server_id"], necessaryV6DHCPOptions)
		},
		func(mac string) string {
			return formatDHCPOptions(buildDHCPv6Options(options, mac, necessaryV6DHCPOptions))
		},
	)
}

func (c *OVNNbClient) updateDHCPOptions(dhcpOpt *ovnnb.DHCPOptions, fields ...any) error {
	if dhcpOpt == nil {
		return errors.New("dhcp_options is nil")
	}

	op, err := c.Database.Table(&ovnnb.DHCPOptions{}).UpdateOps(dhcpOpt, dhcpOpt, fields...)
	return c.transactGenerated("dhcp-options-update", op, err,
		wrapErr("generate operations for updating dhcp options %s: %w", dhcpOpt.UUID),
		wrapErr("update dhcp options %s: %w", dhcpOpt.UUID),
	)
}

// DeleteDHCPOptionsByUUIDs delete dhcp options by uuid
func (c *OVNNbClient) DeleteDHCPOptionsByUUIDs(uuidList ...string) error {
	ops := make([]ovsdb.Operation, 0, len(uuidList))
	for _, uuid := range uuidList {
		dhcpOptions := &ovnnb.DHCPOptions{
			UUID: uuid,
		}

		op, err := c.Database.Table(&ovnnb.DHCPOptions{}).DeleteOps(dhcpOptions)
		if err != nil {
			return logErr(err)
		}
		ops = append(ops, op...)
	}

	return c.transactGenerated("dhcp-options-del", ops, nil, nil, wrapErr("delete dhcp options %v: %w", uuidList))
}

func (c *OVNNbClient) deleteDHCPOptionsFiltered(method string, filter func(*ovnnb.DHCPOptions) bool, wrapGen, wrapTx func(error) error) error {
	return deleteWhereCache(c, &ovnnb.DHCPOptions{}, method, filter, wrapGen, wrapTx)
}

func (c *OVNNbClient) deleteDHCPOptions(lsName, protocol string, keepPerPort bool) error {
	if protocol == kubeovnv1.ProtocolDual {
		protocol = ""
	}
	filter := dhcpOptionsFilter(true, map[string]string{LogicalSwitchKey: lsName, "protocol": protocol})
	return c.deleteDHCPOptionsFiltered(
		"dhcp-options-del",
		func(o *ovnnb.DHCPOptions) bool { return filter(o) && (!keepPerPort || o.ExternalIDs[PortKey] == "") },
		nil,
		wrapErr("delete logical switch %s dhcp options: %w", lsName),
	)
}

// DeleteDHCPOptions delete dhcp options which belongs to logical switch
func (c *OVNNbClient) DeleteDHCPOptions(lsName, protocol string) error {
	return c.deleteDHCPOptions(lsName, protocol, false)
}

// DeleteDHCPOptionsOp returns operations that remove DHCP options owned by a
// logical switch. It is used by callers that need to combine DHCP cleanup
// with other logical switch changes in one transaction.
func (c *OVNNbClient) DeleteDHCPOptionsOp(lsName, protocol string, keepPerPort bool) ([]ovsdb.Operation, error) {
	if protocol == kubeovnv1.ProtocolDual {
		protocol = ""
	}
	externalIDs := map[string]string{
		LogicalSwitchKey: lsName,
		"protocol":       protocol,
	}
	filter := dhcpOptionsFilter(true, externalIDs)
	return c.WhereCache(func(o *ovnnb.DHCPOptions) bool {
		// Disabling subnet DHCP must preserve options owned by individual ports.
		return filter(o) && (!keepPerPort || o.ExternalIDs[PortKey] == "")
	}).Delete()
}

// DeleteDHCPOptionsForPort deletes all per-port DHCP_Options entries for the given port.
func (c *OVNNbClient) DeleteDHCPOptionsForPort(portName string) error {
	if portName == "" {
		return errors.New("the port name is required")
	}
	return c.deleteDHCPOptionsFiltered(
		"dhcp-port-options-del",
		dhcpOptionsFilter(true, map[string]string{PortKey: portName}),
		func(err error) error {
			return fmt.Errorf("generate operation for deleting per-port dhcp options for %s: %w", portName, err)
		},
		func(err error) error {
			return fmt.Errorf("delete per-port dhcp options for port %s: %w", portName, err)
		},
	)
}

// GetDHCPOptions get dhcp options,
// a dhcp options is uniquely identified by switch(lsName) and protocol.
// Per-port DHCP options (those with PortKey in ExternalIDs) are excluded.
func (c *OVNNbClient) GetDHCPOptions(lsName, protocol string, ignoreNotFound bool) (*ovnnb.DHCPOptions, error) {
	if len(lsName) == 0 {
		return nil, errors.New("the logical switch name is required")
	}

	if protocol != kubeovnv1.ProtocolIPv4 && protocol != kubeovnv1.ProtocolIPv6 {
		return nil, errors.New("protocol must be IPv4 or IPv6")
	}

	return c.getDHCPOptionsEntry(lsName, "", protocol, ignoreNotFound)
}

// getDHCPOptionsEntry is the unified internal getter for both subnet-level and per-port DHCP_Options.
// When portName is empty, it returns the subnet-level entry (excludes per-port entries).
// When portName is non-empty, it returns the per-port entry for that specific port.
func (c *OVNNbClient) getDHCPOptionsEntry(lsName, portName, protocol string, ignoreNotFound bool) (*ovnnb.DHCPOptions, error) {
	if portName == "" && len(lsName) == 0 {
		return nil, errors.New("the logical switch name is required")
	}

	var filterIDs map[string]string
	if portName != "" {
		filterIDs = map[string]string{PortKey: portName, "protocol": protocol}
	} else {
		filterIDs = map[string]string{LogicalSwitchKey: lsName, "protocol": protocol}
	}

	dhcpOptList, err := c.ListDHCPOptions(true, filterIDs)
	if err != nil {
		return nil, logWrap(err, wrapErr("get %s dhcp options (ls=%s port=%s): %w", protocol, lsName, portName))
	}

	// For subnet-level queries, exclude per-port entries that share the same ls+protocol.
	if portName == "" {
		n := 0
		for _, opt := range dhcpOptList {
			if opt.ExternalIDs[PortKey] == "" {
				dhcpOptList[n] = opt
				n++
			}
		}
		dhcpOptList = dhcpOptList[:n]
	}

	var notFound, duplicated error
	if portName != "" {
		notFound = fmt.Errorf("not found per-port %s dhcp options for port %s", protocol, portName)
		duplicated = fmt.Errorf("more than one per-port %s dhcp options for port %s", protocol, portName)
	} else {
		notFound = fmt.Errorf("not found logical switch %s %s dhcp options", lsName, protocol)
		duplicated = fmt.Errorf("more than one %s dhcp options in logical switch %s", protocol, lsName)
	}
	return table.Unique(dhcpOptList, ignoreNotFound, notFound, duplicated)
}

// ListDHCPOptions list dhcp options which match the given externalIDs
func (c *OVNNbClient) ListDHCPOptions(needVendorFilter bool, externalIDs map[string]string) ([]ovnnb.DHCPOptions, error) {
	return filterLogged(c.Database, &ovnnb.DHCPOptions{}, dhcpOptionsFilter(needVendorFilter, externalIDs), func(err error) error {
		return fmt.Errorf("list dhcp options with external IDs %v: %w", externalIDs, err)
	})
}

func (c *OVNNbClient) createDHCPEntry(lsName, portName, cidr, options string) error {
	dhcpOpt, err := newDHCPOptionsEntry(lsName, portName, cidr, options)
	if err != nil {
		return logErr(err)
	}

	op, err := c.Database.Table(&ovnnb.DHCPOptions{}).CreateOps(dhcpOpt)
	return c.transactGenerated("dhcp-create", op, err,
		wrapErr("generate operations for creating dhcp options 'cidr %s options %s': %w", cidr, options),
		wrapErr("create dhcp options with cidr %q options %q: %w", cidr, options),
	)
}

// newDHCPOptionsEntry returns a DHCP_Options struct.
// When portName is empty the entry is subnet-scoped; when non-empty it is per-port.
func newDHCPOptionsEntry(lsName, portName, cidr, options string) (*ovnnb.DHCPOptions, error) {
	if len(cidr) == 0 || len(lsName) == 0 {
		return nil, fmt.Errorf("logical switch name %s and cidr %s is required", lsName, cidr)
	}

	protocol := util.CheckProtocol(cidr)
	if len(protocol) == 0 {
		return nil, fmt.Errorf("cidr %s must be a valid ipv4 or ipv6 address", cidr)
	}

	externalIDs := map[string]string{
		LogicalSwitchKey: lsName,
		"protocol":       protocol,
		"vendor":         util.CniTypeName,
	}
	if portName != "" {
		externalIDs[PortKey] = portName
	}

	return &ovnnb.DHCPOptions{
		Cidr:        cidr,
		ExternalIDs: externalIDs,
		Options:     parseDHCPOptions(options),
	}, nil
}

// dhcpOptionsFilter filter dhcp options which match the given externalIDs,
// result should include all dhcp options when externalIDs is empty,
// result should include all dhcp options which externalIDs[key] is not empty when externalIDs[key] is ""
func dhcpOptionsFilter(needVendorFilter bool, externalIDs map[string]string) func(dhcpOpt *ovnnb.DHCPOptions) bool {
	return func(dhcpOpt *ovnnb.DHCPOptions) bool {
		if needVendorFilter && !hasVendor(dhcpOpt.ExternalIDs) {
			return false
		}
		return matchExternalIDs(dhcpOpt.ExternalIDs, externalIDs)
	}
}
