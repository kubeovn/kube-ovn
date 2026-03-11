package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/scylladb/go-set/strset"
	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func buildLogicalSwitchPort(lspName, lsName, ip, mac, podName, namespace string, portSecurity bool, securityGroups, vips string, enableDHCP bool, dhcpOptions *DHCPOptionsUUIDs, vpc string) *ovnnb.LogicalSwitchPort {
	/* normal lsp creation */
	lsp := &ovnnb.LogicalSwitchPort{
		UUID:        ovsclient.NamedUUID(),
		Name:        lspName,
		ExternalIDs: make(map[string]string),
	}

	ipList := strings.Split(ip, ",")
	vipList := strings.Split(vips, ",")
	addresses := make([]string, 0, len(ipList)+len(vipList)+1) // +1 is the mac length
	addresses = append(addresses, mac)
	addresses = append(addresses, ipList...)

	// addresses is the first element of addresses
	lsp.Addresses = []string{strings.TrimSpace(strings.Join(addresses, " "))}
	lsp.ExternalIDs["vendor"] = util.CniTypeName

	lsp.PortSecurity = nil
	if portSecurity {
		if len(vips) != 0 {
			addresses = append(addresses, vipList...)
		}
		// addresses is the first element of port_security
		lsp.PortSecurity = []string{strings.TrimSpace(strings.Join(addresses, " "))}
	}

	// set security groups
	if len(securityGroups) != 0 {
		lsp.ExternalIDs[sgsKey] = strings.ReplaceAll(securityGroups, ",", "/")

		sgList := strings.SplitSeq(securityGroups, ",")
		for sg := range sgList {
			lsp.ExternalIDs[associatedSgKeyPrefix+sg] = "true"
		}
	}

	// add lsp which does not belong to default vpc to default-securitygroup when default-securitygroup configMap exist
	if vpc != "" && vpc != util.DefaultVpc && !strings.Contains(securityGroups, util.DefaultSecurityGroupName) {
		lsp.ExternalIDs[associatedSgKeyPrefix+util.DefaultSecurityGroupName] = "false"
	}

	// set vips info to external-ids
	if len(vips) != 0 {
		lsp.ExternalIDs["vips"] = vips
		lsp.ExternalIDs["attach-vips"] = "true"
	}

	// set pod info to external-ids
	if len(podName) != 0 && len(namespace) != 0 {
		lsp.ExternalIDs["pod"] = namespace + "/" + podName
	}

	// attach necessary info
	lsp.ExternalIDs[LogicalSwitchKey] = lsName
	lsp.ExternalIDs["vendor"] = util.CniTypeName

	// set dhcp options
	if enableDHCP && dhcpOptions != nil {
		if len(dhcpOptions.DHCPv4OptionsUUID) != 0 {
			lsp.Dhcpv4Options = &dhcpOptions.DHCPv4OptionsUUID
		}
		if len(dhcpOptions.DHCPv6OptionsUUID) != 0 {
			lsp.Dhcpv6Options = &dhcpOptions.DHCPv6OptionsUUID
		}
	}

	return lsp
}

func (c *OVNNbClient) CreateLogicalSwitchPort(lsName, lspName, ip, mac, podName, namespace string, portSecurity bool, securityGroups, vips string, enableDHCP bool, dhcpOptions *DHCPOptionsUUIDs, vpc string) error {
	existingLsp, err := c.GetLogicalSwitchPort(lspName, true)
	if err != nil {
		klog.Error(err)
		return err
	}

	var ops []ovsdb.Operation
	lsp := buildLogicalSwitchPort(lspName, lsName, ip, mac, podName, namespace, portSecurity, securityGroups, vips, enableDHCP, dhcpOptions, vpc)
	if existingLsp != nil {
		if existingLsp.ExternalIDs[LogicalSwitchKey] == lsName {
			if err := c.UpdateLogicalSwitchPort(lsp, &lsp.Addresses, &lsp.Dhcpv4Options, &lsp.Dhcpv6Options, &lsp.PortSecurity, &lsp.ExternalIDs); err != nil {
				klog.Error(err)
				return fmt.Errorf("failed to update logical switch port %s: %w", lspName, err)
			}
			return nil
		}
		if ops, err = c.LogicalSwitchUpdatePortOp(existingLsp.ExternalIDs[LogicalSwitchKey], existingLsp.UUID, ovsdb.MutateOperationDelete); err != nil {
			klog.Error(err)
			return err
		}
	}

	createLspOps, err := c.CreateLogicalSwitchPortOp(lsp, lsName)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for creating logical switch port %s: %w", lspName, err)
	}

	if err = c.Transact("lsp-add", append(ops, createLspOps...)); err != nil {
		klog.Error(err)
		return fmt.Errorf("create logical switch port %s: %w", lspName, err)
	}

	return nil
}

// CreateLocalnetLogicalSwitchPort create localnet type logical switch port
func (c *OVNNbClient) CreateLocalnetLogicalSwitchPort(lsName, lspName, provider, cidrBlock string, vlanID int) error {
	lsp, err := c.GetLogicalSwitchPort(lspName, true)
	if err != nil {
		klog.Error(err)
		return err
	}

	ipv4CIDR, ipv6CIDR := util.SplitStringIP(cidrBlock)
	externalIDs := make(map[string]string)
	if ipv4CIDR != "" {
		externalIDs["ipv4_network"] = ipv4CIDR
	}
	if ipv6CIDR != "" {
		externalIDs["ipv6_network"] = ipv6CIDR
	}

	if lsp != nil {
		externalIDs[LogicalSwitchKey] = lsName
		externalIDs["vendor"] = util.CniTypeName
		if !maps.Equal(lsp.ExternalIDs, externalIDs) {
			lsp.ExternalIDs = externalIDs
			if err = c.UpdateLogicalSwitchPort(lsp, &lsp.ExternalIDs); err != nil {
				klog.Error(err)
				return fmt.Errorf("failed to update external-ids of logical switch port %s: %w", lspName, err)
			}
		}

		return nil
	}

	/* create logical switch port */
	lsp = &ovnnb.LogicalSwitchPort{
		UUID:      ovsclient.NamedUUID(),
		Name:      lspName,
		Type:      "localnet",
		Addresses: []string{"unknown"},
		Options: map[string]string{
			"network_name": provider,
		},
		ExternalIDs: externalIDs,
	}

	if vlanID > 0 && vlanID < 4096 {
		lsp.Tag = &vlanID
	}

	ops, err := c.CreateLogicalSwitchPortOp(lsp, lsName)
	if err != nil {
		klog.Error(err)
		return err
	}

	if err = c.Transact("lsp-add", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("create localnet logical switch port %s: %w", lspName, err)
	}

	return nil
}

// CreateVirtualLogicalSwitchPorts create several virtual type logical switch port once
func (c *OVNNbClient) CreateVirtualLogicalSwitchPorts(lsName string, ips ...string) error {
	ops := make([]ovsdb.Operation, 0, len(ips))

	for _, ip := range ips {
		lspName := fmt.Sprintf("%s-vip-%s", lsName, ip)

		exist, err := c.LogicalSwitchPortExists(lspName)
		if err != nil {
			klog.Error(err)
			return err
		}

		// ignore
		if exist {
			continue
		}

		lsp := &ovnnb.LogicalSwitchPort{
			UUID: ovsclient.NamedUUID(),
			Name: lspName,
			Type: "virtual",
			Options: map[string]string{
				"virtual-ip": ip,
			},
		}

		op, err := c.CreateLogicalSwitchPortOp(lsp, lsName)
		if err != nil {
			klog.Error(err)
			return err
		}

		ops = append(ops, op...)
	}

	if err := c.Transact("lsp-add", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("create virtual logical switch ports for logical switch %s: %w", lsName, err)
	}

	return nil
}

// CreateVirtualLogicalSwitchPort create one virtual type logical switch port for allowed-address-pair
func (c *OVNNbClient) CreateVirtualLogicalSwitchPort(lspName, lsName, ip string) error {
	exist, err := c.LogicalSwitchPortExists(lspName)
	if err != nil {
		klog.Error(err)
		return err
	}

	// ignore
	if exist {
		return nil
	}

	lsp := &ovnnb.LogicalSwitchPort{
		UUID: ovsclient.NamedUUID(),
		Name: lspName,
		Type: "virtual",
		Options: map[string]string{
			"virtual-ip": ip,
		},
	}

	op, err := c.CreateLogicalSwitchPortOp(lsp, lsName)
	if err != nil {
		klog.Error(err)
		return err
	}

	if err := c.Transact("lsp-add", op); err != nil {
		klog.Error(err)
		return fmt.Errorf("create virtual logical switch port %s for logical switch %s: %w", lspName, lsName, err)
	}

	return nil
}

// CreateBareLogicalSwitchPort create logical switch port with basic configuration
func (c *OVNNbClient) CreateBareLogicalSwitchPort(lsName, lspName, ip, mac string) error {
	exist, err := c.LogicalSwitchPortExists(lspName)
	if err != nil {
		klog.Error(err)
		return err
	}

	// ignore
	if exist {
		return nil
	}

	ipList := strings.Split(ip, ",")
	addresses := make([]string, 0, len(ipList)+1) // +1 is the mac length
	addresses = append(addresses, mac)
	addresses = append(addresses, ipList...)

	/* create logical switch port */
	lsp := &ovnnb.LogicalSwitchPort{
		UUID:      ovsclient.NamedUUID(),
		Name:      lspName,
		Addresses: []string{strings.TrimSpace(strings.Join(addresses, " "))}, // addresses is the first element of addresses
	}

	ops, err := c.CreateLogicalSwitchPortOp(lsp, lsName)
	if err != nil {
		klog.Error(err)
		return err
	}

	if err = c.Transact("lsp-add", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("create logical switch port %s: %w", lspName, err)
	}

	return nil
}

// SetLogicalSwitchPortVirtualParents updates several virtual type logical switch port virtual-parents at once
func (c *OVNNbClient) SetLogicalSwitchPortVirtualParents(lsName, parents string, ips ...string) error {
	ops := make([]ovsdb.Operation, 0, len(ips))
	for _, ip := range ips {
		lspName := fmt.Sprintf("%s-vip-%s", lsName, ip)

		lsp, err := c.GetLogicalSwitchPort(lspName, true)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("get logical switch port %s: %w", lspName, err)
		}

		if lsp == nil {
			err = fmt.Errorf("logical switch port %s not found", lspName)
			klog.Error(err)
			return err
		}

		lsp.Options["virtual-parents"] = parents
		if len(parents) == 0 {
			delete(lsp.Options, "virtual-parents")
		}

		op, err := c.UpdateLogicalSwitchPortOp(lsp, &lsp.Options)
		if err != nil {
			klog.Error(err)
			return err
		}

		ops = append(ops, op...)
	}

	if err := c.Transact("lsp-update", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("set logical switch port virtual-parents %w", err)
	}
	return nil
}

// SetVirtualLogicalSwitchPortVirtualParents updates one virtual type logical switch port virtual-parents for allowed-address-pair
func (c *OVNNbClient) SetVirtualLogicalSwitchPortVirtualParents(lspName, parents string) error {
	lsp, err := c.GetLogicalSwitchPort(lspName, true)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("get logical switch port %s: %w", lspName, err)
	}

	if lsp == nil {
		err := fmt.Errorf("logical switch port %s not found", lspName)
		klog.Error(err)
		return err
	}

	lsp.Options["virtual-parents"] = parents
	if len(parents) == 0 {
		delete(lsp.Options, "virtual-parents")
	}

	op, err := c.UpdateLogicalSwitchPortOp(lsp, &lsp.Options)
	if err != nil {
		klog.Error(err)
		return err
	}

	if err := c.Transact("lsp-update", op); err != nil {
		klog.Error(err)
		return fmt.Errorf("set logical switch port virtual-parents %w", err)
	}
	return nil
}

func (c *OVNNbClient) SetLogicalSwitchPortArpProxy(lspName string, enableArpProxy bool) error {
	lsp, err := c.GetLogicalSwitchPort(lspName, false)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("get logical switch port %s: %w", lspName, err)
	}
	if lsp == nil {
		err = fmt.Errorf("logical switch port %s not found", lspName)
		klog.Error(err)
		return err
	}
	if lsp.Options == nil {
		lsp.Options = make(map[string]string)
	}
	lsp.Options["arp_proxy"] = strconv.FormatBool(enableArpProxy)
	if !enableArpProxy {
		delete(lsp.Options, "arp_proxy")
	}

	op, err := c.UpdateLogicalSwitchPortOp(lsp, &lsp.Options)
	if err != nil {
		klog.Error(err)
		return err
	}
	if err := c.Transact("lsp-update", op); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to set logical switch port option arp_proxy %w", err)
	}
	return nil
}

// SetLogicalSwitchPortSecurity set logical switch port port_security
func (c *OVNNbClient) SetLogicalSwitchPortSecurity(portSecurity bool, lspName, mac, ips, vips string) error {
	lsp, err := c.GetLogicalSwitchPort(lspName, false)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("get logical switch port %s: %w", lspName, err)
	}
	if lsp == nil {
		err = fmt.Errorf("logical switch port %s not found", lspName)
		klog.Error(err)
		return err
	}

	lsp.PortSecurity = nil
	if portSecurity {
		ipList := strings.Split(ips, ",")
		vipList := strings.Split(vips, ",")
		addresses := make([]string, 0, len(ipList)+len(vipList)+1) // +1 is the mac length

		addresses = append(addresses, mac)
		addresses = append(addresses, ipList...)

		// it's necessary to add vip to port_security
		if vips != "" {
			addresses = append(addresses, vipList...)
		}

		// addresses is the first element of port_security
		lsp.PortSecurity = []string{strings.TrimSpace(strings.Join(addresses, " "))}
	}

	if vips != "" {
		// be careful that don't overwrite origin ExternalIDs
		if lsp.ExternalIDs == nil {
			lsp.ExternalIDs = make(map[string]string)
		}
		lsp.ExternalIDs["vips"] = vips
		lsp.ExternalIDs["attach-vips"] = "true"
	} else {
		delete(lsp.ExternalIDs, "vips")
		delete(lsp.ExternalIDs, "attach-vips")
	}

	if err := c.UpdateLogicalSwitchPort(lsp, &lsp.PortSecurity, &lsp.ExternalIDs); err != nil {
		klog.Error(err)
		return fmt.Errorf("set logical switch port %s port_security %v: %w", lspName, lsp.PortSecurity, err)
	}

	return nil
}

// SetLogicalSwitchPortExternalIDs sets logical switch port external ids
func (c *OVNNbClient) SetLogicalSwitchPortExternalIDs(lspName string, externalIDs map[string]string) error {
	lsp, err := c.GetLogicalSwitchPort(lspName, false)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("get logical switch port %s: %w", lspName, err)
	}
	if lsp == nil {
		err = fmt.Errorf("logical switch port %s not found", lspName)
		klog.Error(err)
		return err
	}
	if lsp.ExternalIDs == nil {
		lsp.ExternalIDs = make(map[string]string)
	}

	maps.Copy(lsp.ExternalIDs, externalIDs)

	if err := c.UpdateLogicalSwitchPort(lsp, &lsp.ExternalIDs); err != nil {
		klog.Error(err)
		return fmt.Errorf("set logical switch port %s external ids %v: %w", lspName, externalIDs, err)
	}

	return nil
}

// SetLogicalSwitchPortSecurityGroup set logical switch port security group,
// op is 'add' or 'remove'
func (c *OVNNbClient) SetLogicalSwitchPortSecurityGroup(lsp *ovnnb.LogicalSwitchPort, op string, sgs ...string) ([]string, error) {
	if len(sgs) == 0 {
		return nil, nil
	}

	if op != "add" && op != "remove" {
		return nil, errors.New("op must be 'add' or 'remove'")
	}

	diffSgs := make([]string, 0, len(sgs))
	oldSgs := getLogicalSwitchPortSgs(lsp)
	for _, sgName := range sgs {
		associatedSgKey := associatedSgKeyPrefix + sgName
		if op == "add" {
			if oldSgs.Has(sgName) {
				continue // ignore existent
			}

			lsp.ExternalIDs[associatedSgKey] = "true"
			oldSgs.Add(sgName)
			diffSgs = append(diffSgs, sgName)
		} else {
			if !oldSgs.Has(sgName) {
				continue // ignore non-existent
			}

			lsp.ExternalIDs[associatedSgKey] = "false"
			oldSgs.Remove(sgName)
			diffSgs = append(diffSgs, sgName)
		}
	}

	newSgs := strings.Join(oldSgs.List(), "/")
	lsp.ExternalIDs[sgsKey] = newSgs
	if len(newSgs) == 0 { // when all sgs had been removed, delete sgsKey
		delete(lsp.ExternalIDs, sgsKey)
	}

	if err := c.UpdateLogicalSwitchPort(lsp, &lsp.ExternalIDs); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("set logical switch port %s security group %v: %w", lsp.Name, newSgs, err)
	}
	return diffSgs, nil
}

// SetLogicalSwitchPortsSecurityGroup set logical switch port security group,
// op is 'add' or 'remove'
func (c *OVNNbClient) SetLogicalSwitchPortsSecurityGroup(sgName, op string) error {
	if op != "add" && op != "remove" {
		return errors.New("op must be 'add' or 'remove'")
	}

	/* list sg port */
	associatedSgKey := associatedSgKeyPrefix + sgName
	associated := "false" // list false associated sg port when add sg to port external_ids
	if op == "remove" {   // list true associated sg port when remove sg from port external_ids
		associated = "true"
	}

	externalIDs := map[string]string{associatedSgKey: associated}
	lsps, err := c.ListNormalLogicalSwitchPorts(true, externalIDs)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("list logical switch ports with external_ids %v: %w", externalIDs, err)
	}

	/* add to or remove from sgs form port external_ids */
	for _, lsp := range lsps {
		if _, err := c.SetLogicalSwitchPortSecurityGroup(&lsp, op, sgName); err != nil {
			klog.Error(err)
			return fmt.Errorf("set logical switch port %s security group %s: %w", lsp.Name, sgName, err)
		}
	}

	return nil
}

// EnablePortLayer2forward adds "unknown" to logical switch port's addresses
func (c *OVNNbClient) EnablePortLayer2forward(lspName string) error {
	lsp, err := c.GetLogicalSwitchPort(lspName, false)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("get logical switch port %s: %w", lspName, err)
	}
	if lsp == nil {
		err = fmt.Errorf("logical switch port %s not found", lspName)
		klog.Error(err)
		return err
	}
	if slices.Contains(lsp.Addresses, "unknown") {
		return nil
	}

	lsp.Addresses = append(lsp.Addresses, "unknown")
	if err := c.UpdateLogicalSwitchPort(lsp, &lsp.Addresses); err != nil {
		klog.Error(err)
		return fmt.Errorf("set logical switch port %s addressed=unknown: %w", lspName, err)
	}

	return nil
}

func (c *OVNNbClient) SetLogicalSwitchPortVlanTag(lspName string, vlanID int) error {
	// valid vlan id is 0~4095
	if vlanID < 0 || vlanID > 4095 {
		return fmt.Errorf("invalid vlan id %d", vlanID)
	}

	lsp, err := c.GetLogicalSwitchPort(lspName, false)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("get logical switch port %s: %w", lspName, err)
	}

	if lsp == nil {
		err = fmt.Errorf("logical switch port %s not found", lspName)
		klog.Error(err)
		return err
	}

	// no need update vlan id when vlan id is the same
	if lsp.Tag != nil && *lsp.Tag == vlanID {
		return nil
	}

	lsp.Tag = &vlanID
	if vlanID == 0 {
		lsp.Tag = nil
	}

	if err := c.UpdateLogicalSwitchPort(lsp, &lsp.Tag); err != nil {
		klog.Error(err)
		return fmt.Errorf("set logical switch port %s tag %d: %w", lspName, vlanID, err)
	}

	return nil
}

// UpdateLogicalSwitchPort update logical switch port
func (c *OVNNbClient) UpdateLogicalSwitchPort(lsp *ovnnb.LogicalSwitchPort, fields ...any) error {
	if lsp == nil {
		err := errors.New("logical switch port is nil")
		klog.Error(err)
		return err
	}

	op, err := c.Where(lsp).Update(lsp, fields...)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for updating logical switch port %s: %w", lsp.Name, err)
	}

	if err = c.Transact("lsp-update", op); err != nil {
		klog.Error(err)
		return fmt.Errorf("update logical switch port %s: %w", lsp.Name, err)
	}

	return nil
}

// DeleteLogicalSwitchPort delete logical switch port in ovn
func (c *OVNNbClient) DeleteLogicalSwitchPort(lspName string) error {
	lsp, err := c.GetLogicalSwitchPort(lspName, true)
	if err != nil {
		klog.Error(err)
		return err
	}
	if lsp == nil {
		return nil
	}

	return c.DeleteLogicalSwitchPortByUUID(lsp.ExternalIDs[LogicalSwitchKey], lsp.UUID)
}

func (c *OVNNbClient) DeleteLogicalSwitchPortByUUID(lsName, lspUUID string) error {
	ops, err := c.DeleteLogicalSwitchPortOp(lsName, lspUUID)
	if err != nil {
		klog.Error(err)
		return err
	}

	if err = c.Transact("lsp-del", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to delete logical switch port with UUID %s", lspUUID)
	}

	return nil
}

// DeleteLogicalSwitchPorts delete logical switch port from logical switch
func (c *OVNNbClient) DeleteLogicalSwitchPorts(externalIDs map[string]string, filter func(lrp *ovnnb.LogicalSwitchPort) bool) error {
	lspList, err := c.ListLogicalSwitchPorts(false, externalIDs, filter)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("list switch ports: %w", err)
	}

	ops := make([]ovsdb.Operation, 0, len(lspList))
	for _, lsp := range lspList {
		op, err := c.DeleteLogicalSwitchPortOp(lsp.ExternalIDs[LogicalSwitchKey], lsp.UUID)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("generate operations for deleting logical switch port %s: %w", lsp.Name, err)
		}
		ops = append(ops, op...)
	}

	if err := c.Transact("lsps-del", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("del logical switch ports: %w", err)
	}

	return nil
}

// GetLogicalSwitchPort get logical switch port by name
func (c *OVNNbClient) GetLogicalSwitchPort(lspName string, ignoreNotFound bool) (*ovnnb.LogicalSwitchPort, error) {
	if lspName == "" {
		err := errors.New("logical switch port name is empty")
		klog.Error(err)
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	lsp := &ovnnb.LogicalSwitchPort{Name: lspName}
	if err := c.Get(ctx, lsp); err != nil {
		if ignoreNotFound && errors.Is(err, client.ErrNotFound) {
			return nil, nil
		}
		klog.Error(err)
		return nil, fmt.Errorf("get logical switch port %s: %w", lspName, err)
	}

	return lsp, nil
}

// ListNormalLogicalSwitchPorts list logical switch ports which type is ""
func (c *OVNNbClient) ListNormalLogicalSwitchPorts(needVendorFilter bool, externalIDs map[string]string) ([]ovnnb.LogicalSwitchPort, error) {
	lsps, err := c.ListLogicalSwitchPorts(needVendorFilter, externalIDs, func(lsp *ovnnb.LogicalSwitchPort) bool {
		return lsp.Type == ""
	})
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("list logical switch ports: %w", err)
	}

	return lsps, nil
}

// ListLogicalSwitchPortsWithLegacyExternalIDs list logical switch ports with legacy external-ids
func (c *OVNNbClient) ListLogicalSwitchPortsWithLegacyExternalIDs() ([]ovnnb.LogicalSwitchPort, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	lspList := make([]ovnnb.LogicalSwitchPort, 0)
	if err := c.WhereCache(func(lsp *ovnnb.LogicalSwitchPort) bool {
		return len(lsp.ExternalIDs) == 0 || lsp.ExternalIDs[LogicalSwitchKey] == "" || lsp.ExternalIDs["vendor"] == ""
	}).List(ctx, &lspList); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to list logical switch ports with legacy external-ids: %w", err)
	}

	return lspList, nil
}

// ListLogicalSwitchPorts list logical switch ports
func (c *OVNNbClient) ListLogicalSwitchPorts(needVendorFilter bool, externalIDs map[string]string, filter func(lsp *ovnnb.LogicalSwitchPort) bool) ([]ovnnb.LogicalSwitchPort, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	lspList := make([]ovnnb.LogicalSwitchPort, 0)

	if err := c.WhereCache(logicalSwitchPortFilter(needVendorFilter, externalIDs, filter)).List(ctx, &lspList); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("list logical switch ports: %w", err)
	}

	return lspList, nil
}

func (c *OVNNbClient) LogicalSwitchPortExists(name string) (bool, error) {
	lsp, err := c.GetLogicalSwitchPort(name, true)
	return lsp != nil, err
}

// CreateLogicalSwitchPortOp create operations which create logical switch port
func (c *OVNNbClient) CreateLogicalSwitchPortOp(lsp *ovnnb.LogicalSwitchPort, lsName string) ([]ovsdb.Operation, error) {
	if lsp == nil {
		return nil, errors.New("logical_switch_port is nil")
	}

	if lsp.ExternalIDs == nil {
		lsp.ExternalIDs = make(map[string]string)
	}

	// attach necessary info
	lsp.ExternalIDs[LogicalSwitchKey] = lsName
	lsp.ExternalIDs["vendor"] = util.CniTypeName

	/* create logical switch port */
	klog.V(3).Infof("create logical switch port %s in logical switch %s", lsp.Name, lsName)
	lspCreateOp, err := c.Create(lsp)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("generate operations for creating logical switch port %s: %w", lsp.Name, err)
	}

	/* add logical switch port to logical switch*/
	lspAddOp, err := c.LogicalSwitchUpdatePortOp(lsName, lsp.UUID, ovsdb.MutateOperationInsert)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	ops := make([]ovsdb.Operation, 0, len(lspCreateOp)+len(lspAddOp))
	ops = append(ops, lspCreateOp...)
	ops = append(ops, lspAddOp...)

	return ops, nil
}

// DeleteLogicalSwitchPortOp create operations which delete logical switch port
func (c *OVNNbClient) DeleteLogicalSwitchPortOp(lsName, lspUUID string) ([]ovsdb.Operation, error) {
	if lsName != "" {
		exist, err := c.LogicalSwitchExists(lsName)
		if err != nil {
			klog.Error(err)
			return nil, err
		}
		if !exist {
			lsName = ""
		}
	}
	klog.Infof("delete logical switch port with UUID %s from logical switch %q", lspUUID, lsName)
	ops, err := c.LogicalSwitchUpdatePortOp(lsName, lspUUID, ovsdb.MutateOperationDelete)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("generate operations for deleting logical switch port with UUID %s from logical switch %q: %w", lspUUID, lsName, err)
	}
	return ops, nil
}

// UpdateLogicalSwitchPortOp create operations which update logical switch port
func (c *OVNNbClient) UpdateLogicalSwitchPortOp(lsp *ovnnb.LogicalSwitchPort, fields ...any) ([]ovsdb.Operation, error) {
	// not found, skip
	if lsp == nil {
		return nil, nil
	}

	op, err := c.Where(lsp).Update(lsp, fields...)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("generate operations for updating logical switch port %s: %w", lsp.Name, err)
	}

	return op, nil
}

// logicalSwitchPortFilter filter logical_switch_port which match the given externalIDs and the custom filter
func logicalSwitchPortFilter(needVendorFilter bool, externalIDs map[string]string, filter func(lsp *ovnnb.LogicalSwitchPort) bool) func(lsp *ovnnb.LogicalSwitchPort) bool {
	return func(lsp *ovnnb.LogicalSwitchPort) bool {
		if needVendorFilter && (len(lsp.ExternalIDs) == 0 || lsp.ExternalIDs["vendor"] != util.CniTypeName) {
			return false
		}
		if len(lsp.ExternalIDs) < len(externalIDs) {
			return false
		}

		if len(lsp.ExternalIDs) != 0 {
			for k, v := range externalIDs {
				// if only key exist but not value in externalIDs, we should include this lsp,
				// it's equal to shell command `ovn-nbctl --columns=xx find logical_switch_port external_ids:key!=\"\"`
				if len(v) == 0 {
					if len(lsp.ExternalIDs[k]) == 0 {
						return false
					}
				} else {
					if lsp.ExternalIDs[k] != v {
						return false
					}
				}
			}
		}

		if filter != nil {
			return filter(lsp)
		}

		return true
	}
}

// getLogicalSwitchPortSgs get logical switch port security group
func getLogicalSwitchPortSgs(lsp *ovnnb.LogicalSwitchPort) *strset.Set {
	sgs := strset.New()
	if lsp != nil {
		for key, value := range lsp.ExternalIDs {
			if strings.HasPrefix(key, associatedSgKeyPrefix) && value == "true" {
				sgs.Add(strings.ReplaceAll(key, associatedSgKeyPrefix, ""))
			}
		}
	}

	return sgs
}

// SetLogicalSwitchPortActivationStrategy sets activation-strategy to rarp for the logical switch port
func (c *OVNNbClient) SetLogicalSwitchPortActivationStrategy(lspName, chassis string) error {
	lsp, err := c.GetLogicalSwitchPort(lspName, false)
	if err != nil {
		klog.Errorf("failed to get logical switch port %s: %v", lspName, err)
		return err
	}

	if lsp == nil {
		err = fmt.Errorf("logical switch port %s not found", lspName)
		klog.Error(err)
		return err
	}

	if lsp.Options != nil && lsp.Options["requested-chassis"] != "" {
		delete(lsp.Options, "requested-chassis")
		delete(lsp.Options, "activation-strategy")
		if err = c.UpdateLogicalSwitchPort(lsp, &lsp.Options); err != nil {
			klog.Errorf("failed to clear activation strategy for the logical switch port %s: %v", lspName, err)
			return err
		}
	}

	requestedChassis := fmt.Sprintf("%s,%s", chassis, chassis)
	if lsp.Options == nil {
		lsp.Options = make(map[string]string, 2)
	}
	lsp.Options["requested-chassis"] = requestedChassis
	lsp.Options["activation-strategy"] = "rarp"
	if err = c.UpdateLogicalSwitchPort(lsp, &lsp.Options); err != nil {
		klog.Errorf("failed to set activation strategy to rarp for the logical switch port %s: %v", lspName, err)
		return err
	}

	return nil
}

// SetLogicalSwitchPortMigrateOptions set logical switch port options of migrate
func (c *OVNNbClient) SetLogicalSwitchPortMigrateOptions(lspName, srcNodeName, targetNodeName string) error {
	// to facilitate the migration of the VM: ovn-nbctl lsp-set-options migrator requested-chassis=src,target activation-strategy=rarp
	// the options will be removed after the migration is completed
	if srcNodeName == "" || targetNodeName == "" {
		err := fmt.Errorf("src and target node can not be empty on migrator port %s", lspName)
		klog.Error(err)
		return err
	}
	if srcNodeName == targetNodeName {
		err := fmt.Errorf("src and target node can not be the same on migrator port %s", lspName)
		klog.Error(err)
		return err
	}

	lsp, src, target, err := c.GetLogicalSwitchPortMigrateOptions(lspName)
	if err != nil {
		klog.Error(err)
		return err
	}
	if lsp == nil {
		err := fmt.Errorf("no migrator logical switch port %s", lspName)
		klog.Error(err)
		return err
	}

	if src == srcNodeName && target == targetNodeName {
		// already set
		return nil
	}

	requestedChassis := fmt.Sprintf("%s,%s", srcNodeName, targetNodeName)
	if lsp.Options == nil {
		lsp.Options = make(map[string]string, 2)
	}
	lsp.Options["requested-chassis"] = requestedChassis
	lsp.Options["activation-strategy"] = "rarp"
	klog.Infof("set migrator logical switch port %s options: %v", lspName, lsp.Options)
	if err := c.UpdateLogicalSwitchPort(lsp, &lsp.Options); err != nil {
		err = fmt.Errorf("failed to set migrator logical switch port %s options requested chassis %s: %w", lspName, requestedChassis, err)
		klog.Error(err)
		return err
	}
	return nil
}

// GetLogicalSwitchPortMigrateOptions get logical switch port src and target node name options of migrate
func (c *OVNNbClient) GetLogicalSwitchPortMigrateOptions(lspName string) (*ovnnb.LogicalSwitchPort, string, string, error) {
	lsp, err := c.GetLogicalSwitchPort(lspName, false)
	if err != nil {
		err = fmt.Errorf("failed to get migrator logical switch port %s: %w", lspName, err)
		klog.Error(err)
		return nil, "", "", err
	}
	if lsp == nil {
		err = fmt.Errorf("logical switch port %s not found", lspName)
		klog.Error(err)
		return nil, "", "", err
	}
	if lsp.Options == nil {
		return lsp, "", "", nil
	}

	requestedChassis, ok := lsp.Options["requested-chassis"]
	if ok {
		splits := strings.Split(requestedChassis, ",")
		if len(splits) == 2 {
			return lsp, splits[0], splits[1], nil
		}
	}
	return lsp, "", "", nil
}

func (c *OVNNbClient) ResetLogicalSwitchPortMigrateOptions(lspName, srcNodeName, targetNodeName string, migratedFail bool) error {
	lsp, err := c.GetLogicalSwitchPort(lspName, false)
	if err != nil {
		err = fmt.Errorf("failed to get migrator logical switch port %s: %w", lspName, err)
		klog.Error(err)
		return err
	}
	if lsp == nil {
		err = fmt.Errorf("logical switch port %s not found", lspName)
		klog.Error(err)
		return err
	}

	if lsp.Options == nil {
		klog.Infof("logical switch port %s has no options", lspName)
		return nil
	}
	if _, ok := lsp.Options["requested-chassis"]; !ok {
		klog.Infof("logical switch port %s has no migrator options", lspName)
		return nil
	}
	if migratedFail {
		// rollback
		klog.Infof("reset migrator port %s to source node %s", lspName, srcNodeName)
		lsp.Options["requested-chassis"] = srcNodeName
	} else {
		klog.Infof("reset migrator port %s to target node %s", lspName, targetNodeName)
		lsp.Options["requested-chassis"] = targetNodeName
	}
	delete(lsp.Options, "activation-strategy")
	if err := c.UpdateLogicalSwitchPort(lsp, &lsp.Options); err != nil {
		err = fmt.Errorf("failed to reset options for migrator logical switch port %s: %w", lspName, err)
		klog.Error(err)
		return err
	}
	return nil
}

// CleanLogicalSwitchPortMigrateOptions clean logical switch port options of migration
func (c *OVNNbClient) CleanLogicalSwitchPortMigrateOptions(lspName string) error {
	lsp, err := c.GetLogicalSwitchPort(lspName, true)
	if err != nil {
		err = fmt.Errorf("failed to get migrator logical switch port %s: %w", lspName, err)
		klog.Error(err)
		return err
	}
	if lsp == nil {
		return nil
	}
	if lsp.Options == nil {
		return nil
	}
	if _, ok := lsp.Options["requested-chassis"]; !ok {
		return nil
	}
	// migrated pod port has requested-chassis, if pod force deleted, the vm pod may schedule to another node
	// if not clean the requested-chassis, the pod has no network connectivity
	delete(lsp.Options, "requested-chassis")
	delete(lsp.Options, "activation-strategy")
	klog.Infof("cleaned migrator logical switch port %s options: %v", lspName, lsp.Options)
	if err := c.UpdateLogicalSwitchPort(lsp, &lsp.Options); err != nil {
		err = fmt.Errorf("failed to clean options for migrator logical switch port %s: %w", lspName, err)
		klog.Error(err)
		return err
	}
	return nil
}
