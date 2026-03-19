package ovs

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type DHCPOptionsUUIDs struct {
	DHCPv4OptionsUUID string
	DHCPv6OptionsUUID string
}

func (c *OVNNbClient) CreateDHCPOptions(lsName, cidr, options string) error {
	return c.createDHCPEntry(lsName, "", cidr, options)
}

func (c *OVNNbClient) UpdateDHCPOptions(subnet *kubeovnv1.Subnet, mtu int) (*DHCPOptionsUUIDs, error) {
	lsName := subnet.Name
	cidrBlock := subnet.Spec.CIDRBlock
	gateway := subnet.Spec.Gateway
	if subnet.Status.U2OInterconnectionIP != "" && subnet.Spec.U2OInterconnection {
		gateway = subnet.Status.U2OInterconnectionIP
	}
	enableDHCP := subnet.Spec.EnableDHCP

	/* delete dhcp options */
	if !enableDHCP {
		if err := c.DeleteDHCPOptions(lsName, subnet.Spec.Protocol); err != nil {
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
		dhcpV4OptUUID, err := c.updateDHCPv4Options(lsName, "", v4CIDR, v4Gateway, subnet.Spec.DHCPv4Options, mtu)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("update IPv4 dhcp options for logical switch %s: %w", lsName, err)
		}
		dhcpOptionsUUIDs.DHCPv4OptionsUUID = dhcpV4OptUUID
	}

	if len(v6CIDR) != 0 {
		dhcpV6OptUUID, err := c.updateDHCPv6Options(lsName, "", v6CIDR, subnet.Spec.DHCPv6Options)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("update IPv6 dhcp options for logical switch %s: %w", lsName, err)
		}
		dhcpOptionsUUIDs.DHCPv6OptionsUUID = dhcpV6OptUUID
	}

	return dhcpOptionsUUIDs, nil
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
			klog.Error(err)
			return nil, fmt.Errorf("update per-port IPv4 dhcp options for port %s: %w", portName, err)
		}
		dhcpOptionsUUIDs.DHCPv4OptionsUUID = uuid
	}

	if len(v6CIDR) != 0 && v6Options != "" {
		uuid, err := c.updateDHCPv6Options(lsName, portName, v6CIDR, v6Options)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("update per-port IPv6 dhcp options for port %s: %w", portName, err)
		}
		dhcpOptionsUUIDs.DHCPv6OptionsUUID = uuid
	}

	return dhcpOptionsUUIDs, nil
}

// updateDHCPv4Options creates or updates a DHCPv4 DHCP_Options record.
// When portName is empty the record is subnet-scoped; when non-empty it is per-port.
func (c *OVNNbClient) updateDHCPv4Options(lsName, portName, cidr, gateway, options string, mtu int) (string, error) {
	necessaryV4DHCPOptions := []string{"lease_time", "router", "server_id", "server_mac", "mtu"}

	protocol := util.CheckProtocol(cidr)
	if protocol != kubeovnv1.ProtocolIPv4 {
		return "", fmt.Errorf("cidr %s must be a valid ipv4 address", cidr)
	}

	dhcpOpt, err := c.getDHCPOptionsEntry(lsName, portName, protocol, true)
	if err != nil {
		klog.Error(err)
		return "", err
	}

	/* update */
	if dhcpOpt != nil {
		mac := dhcpOpt.Options["server_mac"]
		dhcpOpt.Cidr = cidr
		dhcpOpt.Options = buildDHCPv4Options(options, gateway, mac, mtu, necessaryV4DHCPOptions)
		return dhcpOpt.UUID, c.updateDHCPOptions(dhcpOpt, &dhcpOpt.Cidr, &dhcpOpt.Options)
	}

	/* create */
	mac := util.GenerateMac()
	optStr := formatDHCPOptions(buildDHCPv4Options(options, gateway, mac, mtu, necessaryV4DHCPOptions))
	if err := c.createDHCPEntry(lsName, portName, cidr, optStr); err != nil {
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

// updateDHCPv6Options creates or updates a DHCPv6 DHCP_Options record.
// When portName is empty the record is subnet-scoped; when non-empty it is per-port.
func (c *OVNNbClient) updateDHCPv6Options(lsName, portName, cidr, options string) (string, error) {
	necessaryV6DHCPOptions := []string{"server_id"}

	protocol := util.CheckProtocol(cidr)
	if protocol != kubeovnv1.ProtocolIPv6 {
		return "", fmt.Errorf("cidr %s must be a valid ipv6 address", cidr)
	}

	dhcpOpt, err := c.getDHCPOptionsEntry(lsName, portName, protocol, true)
	if err != nil {
		klog.Error(err)
		return "", err
	}

	/* update */
	if dhcpOpt != nil {
		mac := dhcpOpt.Options["server_id"]
		dhcpOpt.Cidr = cidr
		dhcpOpt.Options = buildDHCPv6Options(options, mac, necessaryV6DHCPOptions)
		return dhcpOpt.UUID, c.updateDHCPOptions(dhcpOpt, &dhcpOpt.Cidr, &dhcpOpt.Options)
	}

	/* create */
	mac := util.GenerateMac()
	optStr := formatDHCPOptions(buildDHCPv6Options(options, mac, necessaryV6DHCPOptions))
	if err := c.createDHCPEntry(lsName, portName, cidr, optStr); err != nil {
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

// updateDHCPOptions update dhcp options
func (c *OVNNbClient) updateDHCPOptions(dhcpOpt *ovnnb.DHCPOptions, fields ...any) error {
	if dhcpOpt == nil {
		return errors.New("dhcp_options is nil")
	}

	op, err := c.ovsDbClient.Where(dhcpOpt).Update(dhcpOpt, fields...)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for updating dhcp options %s: %w", dhcpOpt.UUID, err)
	}

	if err = c.Transact("dhcp-options-update", op); err != nil {
		klog.Error(err)
		return fmt.Errorf("update dhcp options %s: %w", dhcpOpt.UUID, err)
	}

	return nil
}

// DeleteDHCPOptionsByUUIDs delete dhcp options by uuid
func (c *OVNNbClient) DeleteDHCPOptionsByUUIDs(uuidList ...string) error {
	ops := make([]ovsdb.Operation, 0, len(uuidList))
	for _, uuid := range uuidList {
		dhcpOptions := &ovnnb.DHCPOptions{
			UUID: uuid,
		}

		op, err := c.Where(dhcpOptions).Delete()
		if err != nil {
			klog.Error(err)
			return err
		}
		ops = append(ops, op...)
	}

	if err := c.Transact("dhcp-options-del", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("delete dhcp options %v: %w", uuidList, err)
	}

	return nil
}

// DeleteDHCPOptions delete dhcp options which belongs to logical switch
func (c *OVNNbClient) DeleteDHCPOptions(lsName, protocol string) error {
	if protocol == kubeovnv1.ProtocolDual {
		protocol = ""
	}
	externalIDs := map[string]string{
		LogicalSwitchKey: lsName,
		"protocol":       protocol, // list all protocol dhcp options when protocol is ""
	}

	op, err := c.WhereCache(dhcpOptionsFilter(true, externalIDs)).Delete()
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operation for deleting dhcp options: %w", err)
	}

	if err = c.Transact("dhcp-options-del", op); err != nil {
		klog.Error(err)
		return fmt.Errorf("delete logical switch %s dhcp options: %w", lsName, err)
	}

	return nil
}

// DeleteDHCPOptionsForPort deletes all per-port DHCP_Options entries for the given port.
func (c *OVNNbClient) DeleteDHCPOptionsForPort(portName string) error {
	if portName == "" {
		return errors.New("the port name is required")
	}

	op, err := c.WhereCache(dhcpOptionsFilter(true, map[string]string{
		PortKey: portName,
	})).Delete()
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operation for deleting per-port dhcp options for %s: %w", portName, err)
	}

	if err = c.Transact("dhcp-port-options-del", op); err != nil {
		klog.Error(err)
		return fmt.Errorf("delete per-port dhcp options for port %s: %w", portName, err)
	}

	return nil
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
		klog.Error(err)
		return nil, fmt.Errorf("get %s dhcp options (ls=%s port=%s): %w", protocol, lsName, portName, err)
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

	if len(dhcpOptList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		if portName != "" {
			return nil, fmt.Errorf("not found per-port %s dhcp options for port %s", protocol, portName)
		}
		return nil, fmt.Errorf("not found logical switch %s %s dhcp options", lsName, protocol)
	}

	if len(dhcpOptList) > 1 {
		if portName != "" {
			return nil, fmt.Errorf("more than one per-port %s dhcp options for port %s", protocol, portName)
		}
		return nil, fmt.Errorf("more than one %s dhcp options in logical switch %s", protocol, lsName)
	}

	return &dhcpOptList[0], nil
}

// ListDHCPOptions list dhcp options which match the given externalIDs
func (c *OVNNbClient) ListDHCPOptions(needVendorFilter bool, externalIDs map[string]string) ([]ovnnb.DHCPOptions, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	dhcpOptList := make([]ovnnb.DHCPOptions, 0)

	if err := c.WhereCache(dhcpOptionsFilter(needVendorFilter, externalIDs)).List(ctx, &dhcpOptList); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("list dhcp options with external IDs %v: %w", externalIDs, err)
	}

	return dhcpOptList, nil
}

// createDHCPEntry creates a DHCP_Options row.
// When portName is empty the entry is subnet-scoped; when non-empty it is per-port.
func (c *OVNNbClient) createDHCPEntry(lsName, portName, cidr, options string) error {
	dhcpOpt, err := newDHCPOptionsEntry(lsName, portName, cidr, options)
	if err != nil {
		klog.Error(err)
		return err
	}

	op, err := c.Create(dhcpOpt)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for creating dhcp options 'cidr %s options %s': %w", cidr, options, err)
	}

	if err = c.Transact("dhcp-create", op); err != nil {
		klog.Error(err)
		return fmt.Errorf("create dhcp options with cidr %q options %q: %w", cidr, options, err)
	}

	return nil
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
		if needVendorFilter && (len(dhcpOpt.ExternalIDs) == 0 || dhcpOpt.ExternalIDs["vendor"] != util.CniTypeName) {
			return false
		}

		if len(dhcpOpt.ExternalIDs) < len(externalIDs) {
			return false
		}

		if len(dhcpOpt.ExternalIDs) != 0 {
			for k, v := range externalIDs {
				// if only key exist but not value in externalIDs, we should include this dhcp options,
				// it's equal to shell command `ovn-nbctl --columns=xx find dhcp_options external_ids:key!=\"\"`
				if len(v) == 0 {
					if len(dhcpOpt.ExternalIDs[k]) == 0 {
						return false
					}
				} else {
					if dhcpOpt.ExternalIDs[k] != v {
						return false
					}
				}
			}
		}

		return true
	}
}
