package ovs

import (
	"context"
	"fmt"
	"strings"

	"github.com/ovn-org/libovsdb/ovsdb"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type DHCPOptionsUUIDs struct {
	DHCPv4OptionsUUID string
	DHCPv6OptionsUUID string
}

func (c *ovnClient) CreateDHCPOptions(lsName, cidr, options string) error {
	dhcpOpt, err := newDHCPOptions(lsName, cidr, options)
	if err != nil {
		return err
	}

	op, err := c.ovnNbClient.Create(dhcpOpt)
	if err != nil {
		return fmt.Errorf("generate operations for creating dhcp options 'cidr %s options %s': %v", cidr, options, err)
	}

	if err = c.Transact("acl-create", op); err != nil {
		return fmt.Errorf("create dhcp options 'cidr %s options %s': %v", cidr, options, err)
	}

	return nil
}

func (c *ovnClient) UpdateDHCPOptions(subnet *kubeovnv1.Subnet) (*DHCPOptionsUUIDs, error) {
	lsName := subnet.Name
	cidrBlock := subnet.Spec.CIDRBlock
	gateway := subnet.Spec.Gateway
	enableDHCP := subnet.Spec.EnableDHCP

	/* delete dhcp options */
	if !enableDHCP {
		if err := c.DeleteDHCPOptions(lsName, subnet.Spec.Protocol); err != nil {
			return nil, fmt.Errorf("delete dhcp options for logical switch %s: %v", lsName, err)
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

	dhcpV4OptUUID, err := c.updateDHCPv4Options(lsName, v4CIDR, v4Gateway, subnet.Spec.DHCPv4Options)
	if err != nil {
		return nil, fmt.Errorf("update IPv4 dhcp options for logical switch %s: %v", lsName, err)
	}

	dhcpV6OptUUID, err := c.updateDHCPv6Options(lsName, v6CIDR, subnet.Spec.DHCPv6Options)
	if err != nil {
		return nil, fmt.Errorf("update IPv6 dhcp options for logical switch %s: %v", lsName, err)
	}

	return &DHCPOptionsUUIDs{
		dhcpV4OptUUID,
		dhcpV6OptUUID,
	}, nil
}

func (c *ovnClient) updateDHCPv4Options(lsName, cidr, gateway, options string) (uuid string, err error) {
	protocol := util.CheckProtocol(cidr)
	if protocol != kubeovnv1.ProtocolIPv4 {
		return "", fmt.Errorf("cidr %s must be a valid ipv4 address", cidr)
	}

	dhcpOpt, err := c.GetDHCPOptions(lsName, protocol, true)
	if err != nil {
		return
	}

	if len(options) == 0 {
		mac := util.GenerateMac()
		if dhcpOpt != nil && len(dhcpOpt.Options) != 0 {
			mac = dhcpOpt.Options["server_mac"]
		}

		options = fmt.Sprintf("lease_time=%d,router=%s,server_id=%s,server_mac=%s", 3600, gateway, "169.254.0.254", mac)
	}

	/* update */
	if dhcpOpt != nil {
		dhcpOpt.Cidr = cidr
		dhcpOpt.Options = parseDHCPOptions(options)
		return dhcpOpt.UUID, c.updateDHCPOptions(dhcpOpt, &dhcpOpt.Cidr, &dhcpOpt.Options)
	}

	/* create */
	if err := c.CreateDHCPOptions(lsName, cidr, options); err != nil {
		return "", fmt.Errorf("create dhcp options: %v", err)
	}

	dhcpOpt, err = c.GetDHCPOptions(lsName, protocol, false)
	if err != nil {
		return "", err
	}

	return dhcpOpt.UUID, nil
}

func (c *ovnClient) updateDHCPv6Options(lsName, cidr, options string) (uuid string, err error) {
	protocol := util.CheckProtocol(cidr)
	if protocol != kubeovnv1.ProtocolIPv6 {
		return "", fmt.Errorf("cidr %s must be a valid ipv4 address", cidr)
	}

	dhcpOpt, err := c.GetDHCPOptions(lsName, protocol, true)
	if err != nil {
		return
	}

	if len(options) == 0 {
		mac := util.GenerateMac()
		if dhcpOpt != nil && len(dhcpOpt.Options) != 0 {
			mac = dhcpOpt.Options["server_id"]
		}

		options = fmt.Sprintf("server_id=%s", mac)
	}

	/* update */
	if dhcpOpt != nil {
		dhcpOpt.Cidr = cidr
		dhcpOpt.Options = parseDHCPOptions(options)
		return dhcpOpt.UUID, c.updateDHCPOptions(dhcpOpt, &dhcpOpt.Cidr, &dhcpOpt.Options)
	}

	/* create */
	if err := c.CreateDHCPOptions(lsName, cidr, options); err != nil {
		return "", fmt.Errorf("create dhcp options: %v", err)
	}

	dhcpOpt, err = c.GetDHCPOptions(lsName, protocol, false)
	if err != nil {
		return "", err
	}

	return dhcpOpt.UUID, nil
}

// updateDHCPOptions update dhcp options
func (c *ovnClient) updateDHCPOptions(dhcpOpt *ovnnb.DHCPOptions, fields ...interface{}) error {
	if dhcpOpt == nil {
		return fmt.Errorf("dhcp_options is nil")
	}

	op, err := c.ovnNbClient.Where(dhcpOpt).Update(dhcpOpt, fields...)
	if err != nil {
		return fmt.Errorf("generate operations for updating dhcp options %s: %v", dhcpOpt.UUID, err)
	}

	if err = c.Transact("dhcp-options-update", op); err != nil {
		return fmt.Errorf("update dhcp options %s: %v", dhcpOpt.UUID, err)
	}

	return nil
}

// DeleteDHCPOptionsByUUIDs delete dhcp options by uuid
func (c *ovnClient) DeleteDHCPOptionsByUUIDs(uuidList ...string) error {
	ops := make([]ovsdb.Operation, 0, len(uuidList))
	for _, uuid := range uuidList {
		dhcpOptions := &ovnnb.DHCPOptions{
			UUID: uuid,
		}

		op, err := c.Where(dhcpOptions).Delete()
		if err != nil {
			return err
		}
		ops = append(ops, op...)
	}

	if err := c.Transact("dhcp-options-del", ops); err != nil {
		return fmt.Errorf("delete dhcp options %v: %v", uuidList, err)
	}

	return nil
}

// DeleteDHCPOptions delete dhcp options which belongs to logical switch
func (c *ovnClient) DeleteDHCPOptions(lsName string, protocol string) error {
	if protocol == kubeovnv1.ProtocolDual {
		protocol = ""
	}
	externalIDs := map[string]string{
		logicalSwitchKey: lsName,
		"protocol":       protocol, // list all protocol dhcp options when protocol is ""
	}

	op, err := c.WhereCache(dhcpOptionsFilter(true, externalIDs)).Delete()
	if err != nil {
		return fmt.Errorf("generate operation for deleting dhcp options: %v", err)
	}

	if err = c.Transact("dhcp-options-del", op); err != nil {
		return fmt.Errorf("delete logical switch %s dhcp options: %v", lsName, err)
	}

	return nil
}

// GetDHCPOptions get dhcp options,
// a dhcp options is uniquely identified by switch(lsName) and protocol
func (c *ovnClient) GetDHCPOptions(lsName, protocol string, ignoreNotFound bool) (*ovnnb.DHCPOptions, error) {
	if len(lsName) == 0 {
		return nil, fmt.Errorf("the logical router name is required")
	}

	if protocol != kubeovnv1.ProtocolIPv4 && protocol != kubeovnv1.ProtocolIPv6 {
		return nil, fmt.Errorf("protocol must be IPv4 or IPv6")
	}

	dhcpOptList, err := c.ListDHCPOptions(true, map[string]string{
		logicalSwitchKey: lsName,
		"protocol":       protocol,
	})

	if err != nil {
		return nil, fmt.Errorf("get logical switch %s %s dhcp options: %v", lsName, protocol, err)
	}

	// not found
	if len(dhcpOptList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}

		return nil, fmt.Errorf("not found logical switch %s %s dhcp options: %v", lsName, protocol, err)
	}

	if len(dhcpOptList) > 1 {
		return nil, fmt.Errorf("more than one %s dhcp options in logical switch %s", protocol, lsName)
	}

	return &dhcpOptList[0], nil
}

// ListDHCPOptions list dhcp options which match the given externalIDs
func (c *ovnClient) ListDHCPOptions(needVendorFilter bool, externalIDs map[string]string) ([]ovnnb.DHCPOptions, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	dhcpOptList := make([]ovnnb.DHCPOptions, 0)

	if err := c.WhereCache(dhcpOptionsFilter(needVendorFilter, externalIDs)).List(ctx, &dhcpOptList); err != nil {
		return nil, fmt.Errorf("list dhcp options with external IDs %v: %v", externalIDs, err)
	}

	return dhcpOptList, nil
}

func (c *ovnClient) DHCPOptionsExists(lsName, cidr string) (bool, error) {
	dhcpOpt, err := c.GetDHCPOptions(lsName, cidr, true)
	return dhcpOpt != nil, err
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
			logicalSwitchKey: lsName,
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
