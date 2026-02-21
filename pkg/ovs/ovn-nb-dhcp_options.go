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
	dhcpOpt, err := newDHCPOptions(lsName, cidr, options)
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
		dhcpV4OptUUID, err := c.updateDHCPv4Options(lsName, v4CIDR, v4Gateway, subnet.Spec.DHCPv4Options, mtu)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("update IPv4 dhcp options for logical switch %s: %w", lsName, err)
		}
		dhcpOptionsUUIDs.DHCPv4OptionsUUID = dhcpV4OptUUID
	}

	if len(v6CIDR) != 0 {
		dhcpV6OptUUID, err := c.updateDHCPv6Options(lsName, v6CIDR, subnet.Spec.DHCPv6Options)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("update IPv6 dhcp options for logical switch %s: %w", lsName, err)
		}
		dhcpOptionsUUIDs.DHCPv6OptionsUUID = dhcpV6OptUUID
	}

	return dhcpOptionsUUIDs, nil
}

func (c *OVNNbClient) updateDHCPv4Options(lsName, cidr, gateway, options string, mtu int) (uuid string, err error) {
	necessaryV4DHCPOptions := []string{"lease_time", "router", "server_id", "server_mac", "mtu"}

	protocol := util.CheckProtocol(cidr)
	if protocol != kubeovnv1.ProtocolIPv4 {
		return "", fmt.Errorf("cidr %s must be a valid ipv4 address", cidr)
	}

	dhcpOpt, err := c.GetDHCPOptions(lsName, protocol, true)
	if err != nil {
		klog.Error(err)
		return uuid, err
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
	options = formatDHCPOptions(buildDHCPv4Options(options, gateway, mac, mtu, necessaryV4DHCPOptions))
	if err := c.CreateDHCPOptions(lsName, cidr, options); err != nil {
		klog.Error(err)
		return "", fmt.Errorf("create dhcp options: %w", err)
	}

	dhcpOpt, err = c.GetDHCPOptions(lsName, protocol, false)
	if err != nil {
		klog.Error(err)
		return "", err
	}

	return dhcpOpt.UUID, nil
}

func (c *OVNNbClient) updateDHCPv6Options(lsName, cidr, options string) (uuid string, err error) {
	necessaryV6DHCPOptions := []string{"server_id"}

	protocol := util.CheckProtocol(cidr)
	if protocol != kubeovnv1.ProtocolIPv6 {
		return "", fmt.Errorf("cidr %s must be a valid ipv6 address", cidr)
	}

	dhcpOpt, err := c.GetDHCPOptions(lsName, protocol, true)
	if err != nil {
		klog.Error(err)
		return uuid, err
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
	options = formatDHCPOptions(buildDHCPv6Options(options, mac, necessaryV6DHCPOptions))
	if err := c.CreateDHCPOptions(lsName, cidr, options); err != nil {
		klog.Error(err)
		return "", fmt.Errorf("create dhcp options: %w", err)
	}

	dhcpOpt, err = c.GetDHCPOptions(lsName, protocol, false)
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

// GetDHCPOptions get dhcp options,
// a dhcp options is uniquely identified by switch(lsName) and protocol
func (c *OVNNbClient) GetDHCPOptions(lsName, protocol string, ignoreNotFound bool) (*ovnnb.DHCPOptions, error) {
	if len(lsName) == 0 {
		return nil, errors.New("the logical router name is required")
	}

	if protocol != kubeovnv1.ProtocolIPv4 && protocol != kubeovnv1.ProtocolIPv6 {
		return nil, errors.New("protocol must be IPv4 or IPv6")
	}

	dhcpOptList, err := c.ListDHCPOptions(true, map[string]string{
		LogicalSwitchKey: lsName,
		"protocol":       protocol,
	})
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("get logical switch %s %s dhcp options: %w", lsName, protocol, err)
	}

	// not found
	if len(dhcpOptList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}

		return nil, fmt.Errorf("not found logical switch %s %s dhcp options: %w", lsName, protocol, err)
	}

	if len(dhcpOptList) > 1 {
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

// newDHCPOptions return dhcp options with basic information
func newDHCPOptions(lsName, cidr, options string) (*ovnnb.DHCPOptions, error) {
	if len(cidr) == 0 || len(lsName) == 0 {
		return nil, fmt.Errorf("logical switch name %s and cidr %s is required", lsName, cidr)
	}

	protocol := util.CheckProtocol(cidr)
	if len(protocol) == 0 {
		return nil, fmt.Errorf("cidr %s must be a valid ipv4 or ipv6 address", cidr)
	}

	return &ovnnb.DHCPOptions{
		Cidr: cidr,
		ExternalIDs: map[string]string{
			LogicalSwitchKey: lsName,
			"protocol":       protocol,
			"vendor":         util.CniTypeName,
		},
		Options: parseDHCPOptions(options),
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
