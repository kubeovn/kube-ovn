package ovs

import (
	"context"
	"fmt"
	"strings"

	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c OvnClient) CreateLogicalSwitchPort(lsName, lspName, ip, mac, podName, namespace string, portSecurity bool, securityGroups string, vips string, liveMigration bool, enableDHCP bool, dhcpOptions *DHCPOptionsUUIDs, vpc string) error {
	lsp, err := c.GetLogicalSwitchPort(lspName, true)
	if err != nil {
		return fmt.Errorf("get logical switch port %s: %v", lspName, err)
	}

	// when kubevirt vm live-migrate, just consider the this case for now(keep-vm-ip=true):
	// lspName is 'vmName.namespace.providerName' when keep-vm-ip=true, there are two pod with the same lspName,
	// so to determine vm is in live migration
	if liveMigration && lsp != nil {
		lsp.Addresses = []string{mac}
		lsp.ExternalIDs["liveMigration"] = "1"
		if err := c.UpdateLogicalSwitchPort(lsp, &lsp.Addresses, &lsp.ExternalIDs); err != nil {
			return fmt.Errorf("set liveMigration=1 to logical switch port %s: %v", lspName, err)
		}
		return nil
	}

	/* normal lsp creation */
	lsp = &ovnnb.LogicalSwitchPort{
		UUID:        ovsclient.UUID(),
		Name:        lspName,
		ExternalIDs: make(map[string]string),
	}

	ipList := strings.Split(ip, ",")
	vipList := strings.Split(vips, ",")
	addresses := make([]string, 0, len(ipList)+len(vipList)+1) // +1 is the mac length
	addresses = append(addresses, mac)
	addresses = append(addresses, ipList...)

	// addresses is the first element of addresses
	lsp.Addresses = []string{strings.Join(addresses, " ")}

	if portSecurity {
		if len(vips) != 0 {
			addresses = append(addresses, vipList...)
		}
		// addresses is the first element of port_security
		lsp.PortSecurity = []string{strings.Join(addresses, " ")}

		// set sercurity groups
		if len(securityGroups) != 0 {
			lsp.ExternalIDs["security_groups"] = strings.ReplaceAll(securityGroups, ",", "/")

			sgList := strings.Split(securityGroups, ",")
			for _, sg := range sgList {
				lsp.ExternalIDs["associated_sg_"+sg] = "true"
			}
		}
	}

	// add lsp which does not belong to defualt vpc to default-securitygroup when default-securitygroup configMap exist
	if vpc != "" && vpc != util.DefaultVpc && !strings.Contains(securityGroups, util.DefaultSecurityGroupName) {
		lsp.ExternalIDs["associated_sg_"+util.DefaultSecurityGroupName] = "false"
	}

	// set vips info to external-ids
	if len(vips) != 0 {
		lsp.ExternalIDs["vips"] = strings.ReplaceAll(vips, ",", "/")
		lsp.ExternalIDs["attach-vips"] = "true"
	}

	// set pod info to external-ids
	if len(podName) != 0 && len(namespace) != 0 {
		lsp.ExternalIDs["pod"] = namespace + "/" + podName
	}

	// set dhcp options
	if enableDHCP && dhcpOptions != nil {
		if len(dhcpOptions.DHCPv4OptionsUUID) != 0 {
			lsp.Dhcpv4Options = &dhcpOptions.DHCPv4OptionsUUID
		}
		if len(dhcpOptions.DHCPv6OptionsUUID) != 0 {
			lsp.Dhcpv6Options = &dhcpOptions.DHCPv6OptionsUUID
		}
	}

	ops, err := c.CreateLogicalSwitchPortOp(lsp, lsName)
	if err != nil {
		return fmt.Errorf("generate operations for creating logical switch port %s: %v", lspName, err)
	}

	if err = c.Transact("lsp-add", ops); err != nil {
		return fmt.Errorf("create logical switch port %s: %v", lspName, err)
	}

	return nil
}

// CreateLocalnetLogicalSwitchPort create localnet type logical switch port
func (c OvnClient) CreateLocalnetLogicalSwitchPort(lsName, lspName, provider string, vlanID int) error {
	exist, err := c.LogicalSwitchPortExists(lspName)
	if err != nil {
		return err
	}

	// ignore
	if exist {
		return nil
	}

	/* create logical switch port */
	lsp := &ovnnb.LogicalSwitchPort{
		UUID:      ovsclient.UUID(),
		Name:      lspName,
		Type:      "localnet",
		Addresses: []string{"unknown"},
		Options: map[string]string{
			"network_name": provider,
		},
	}

	if vlanID > 0 && vlanID < 4096 {
		lsp.Tag = &vlanID
	}

	ops, err := c.CreateLogicalSwitchPortOp(lsp, lsName)
	if err != nil {
		return err
	}

	if err = c.Transact("lsp-add", ops); err != nil {
		return fmt.Errorf("create localnet logical switch port %s: %v", lspName, err)
	}

	return nil
}

// CreateVirtualLogicalSwitchPorts create several virtual type logical switch port once
func (c OvnClient) CreateVirtualLogicalSwitchPorts(lsName string, ips ...string) error {
	ops := make([]ovsdb.Operation, 0, len(ips))

	for _, ip := range ips {
		lspName := fmt.Sprintf("%s-vip-%s", lsName, ip)

		exist, err := c.LogicalSwitchPortExists(lspName)
		if err != nil {
			return err
		}

		// ignore
		if exist {
			continue
		}

		lsp := &ovnnb.LogicalSwitchPort{
			UUID: ovsclient.UUID(),
			Name: lspName,
			Type: "virtual",
			Options: map[string]string{
				"virtual-ip": ip,
			},
		}

		op, err := c.CreateLogicalSwitchPortOp(lsp, lsName)
		if err != nil {
			return err
		}

		ops = append(ops, op...)
	}

	if err := c.Transact("lsp-add", ops); err != nil {
		return fmt.Errorf("create virtual logical switch ports for logical switch %s: %v", lsName, err)
	}

	return nil
}

// CreateBareLogicalSwitchPort create logical switch port with basic configuration
func (c OvnClient) CreateBareLogicalSwitchPort(lsName, lspName string) error {
	/* create logical switch port */
	lsp := &ovnnb.LogicalSwitchPort{
		UUID: ovsclient.UUID(),
		Name: lspName,
	}

	ops, err := c.CreateLogicalSwitchPortOp(lsp, lsName)
	if err != nil {
		return err
	}

	if err = c.Transact("lsp-add", ops); err != nil {
		return fmt.Errorf("create logical switch port %s: %v", lspName, err)
	}

	return nil
}

// CreateVirtualLogicalSwitchPorts update several virtual type logical switch port virtual-parents once
func (c OvnClient) SetLogicalSwitchPortVirtualParents(lsName, parents string, ips ...string) error {
	ops := make([]ovsdb.Operation, 0, len(ips))
	for _, ip := range ips {
		lspName := fmt.Sprintf("%s-vip-%s", lsName, ip)

		lsp, err := c.GetLogicalSwitchPort(lspName, false)
		if err != nil {
			return fmt.Errorf("get logical switch port %s: %v", lspName, err)
		}

		lsp.Options["virtual-parents"] = parents
		if len(parents) == 0 {
			delete(lsp.Options, "virtual-parents")
		}

		op, err := c.UpdateLogicalSwitchPortOp(lsp, &lsp.Options)
		if err != nil {
			return err
		}

		ops = append(ops, op...)
	}

	if err := c.Transact("lsp-update", ops); err != nil {
		return fmt.Errorf("set logical switch port virtual-parents %v", err)
	}
	return nil
}

// SetLogicalSwitchPortSecurity set logical switch port port_security
func (c OvnClient) SetLogicalSwitchPortSecurity(portSecurity bool, lspName, mac, ips, vips string) error {
	lsp, err := c.GetLogicalSwitchPort(lspName, false)
	if err != nil {
		return fmt.Errorf("get logical switch port %s: %v", lspName, err)
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
		lsp.PortSecurity = []string{strings.Join(addresses, " ")}
	}

	if vips != "" {
		// be careful that dont't overwite origin ExternalIDs
		if lsp.ExternalIDs == nil {
			lsp.ExternalIDs = make(map[string]string)
		}
		lsp.ExternalIDs["vips"] = strings.ReplaceAll(vips, ",", "/")
		lsp.ExternalIDs["attach-vips"] = "true"
	} else {
		delete(lsp.ExternalIDs, "vips")
		delete(lsp.ExternalIDs, "attach-vips")
	}

	if err := c.UpdateLogicalSwitchPort(lsp, &lsp.PortSecurity, &lsp.ExternalIDs); err != nil {
		return fmt.Errorf("set logical switch port %s port_security %v: %v", lspName, lsp.PortSecurity, err)
	}

	return nil
}

// EnablePortLayer2forward set logical switch port addresses as 'unknown'
func (c OvnClient) EnablePortLayer2forward(lspName string) error {
	lsp, err := c.GetLogicalSwitchPort(lspName, false)
	if err != nil {
		return fmt.Errorf("get logical switch port %s: %v", lspName, err)
	}

	lsp.Addresses = []string{"unknown"}

	if err := c.UpdateLogicalSwitchPort(lsp, &lsp.Addresses); err != nil {
		return fmt.Errorf("set logical switch port %s addressed=unknown: %v", lspName, err)
	}

	return nil
}

func (c OvnClient) SetLogicalSwitchPortVlanTag(lspName string, vlanID int) error {
	// valid vlan id is 0~4095
	if vlanID < 1 || vlanID > 4095 {
		return fmt.Errorf("invalid vlan id %d", vlanID)
	}

	lsp, err := c.GetLogicalSwitchPort(lspName, false)
	if err != nil {
		return fmt.Errorf("get logical switch port %s: %v", lspName, err)
	}

	// no need update vlan id
	if lsp.Tag != nil && *lsp.Tag == vlanID {
		return nil
	}

	lsp.Tag = &vlanID

	if err := c.UpdateLogicalSwitchPort(lsp, &lsp.Tag); err != nil {
		return fmt.Errorf("set logical switch port %s tag %d: %v", lspName, vlanID, err)
	}

	return nil
}

// UpdateLogicalSwitchPort update logical switch port
func (c OvnClient) UpdateLogicalSwitchPort(lsp *ovnnb.LogicalSwitchPort, fields ...interface{}) error {
	if lsp == nil {
		return fmt.Errorf("logical_switch_port is nil")
	}

	op, err := c.Where(lsp).Update(lsp, fields...)
	if err != nil {
		return fmt.Errorf("generate operations for updating logical switch port %s: %v", lsp.Name, err)
	}

	if err = c.Transact("lsp-update", op); err != nil {
		return fmt.Errorf("update logical switch port %s: %v", lsp.Name, err)
	}

	return nil
}

// DeleteLogicalSwitchPort delete logical switch port in ovn
func (c OvnClient) DeleteLogicalSwitchPort(lspName string) error {
	ops, err := c.DeleteLogicalSwitchPortOp(lspName)
	if err != nil {
		return err
	}

	if err = c.Transact("lsp-del", ops); err != nil {
		return fmt.Errorf("delete logical switch port %s", lspName)
	}

	return nil
}

// GetLogicalSwitchPort get logical switch port by name
func (c OvnClient) GetLogicalSwitchPort(name string, ignoreNotFound bool) (*ovnnb.LogicalSwitchPort, error) {
	lsp := &ovnnb.LogicalSwitchPort{Name: name}
	if err := c.Get(context.TODO(), lsp); err != nil {
		if ignoreNotFound && err == client.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get logical switch port %s: %v", name, err)
	}

	return lsp, nil
}

func (c OvnClient) ListRemoteTypeLogicalSwitchPorts() ([]ovnnb.LogicalSwitchPort, error) {
	lspList := make([]ovnnb.LogicalSwitchPort, 0, 1)
	if err := c.WhereCache(func(lsp *ovnnb.LogicalSwitchPort) bool {
		return lsp.Type == "remote"
	}).List(context.TODO(), &lspList); err != nil {
		return nil, fmt.Errorf("list logical switch port which type is remote: %v", err)
	}

	return lspList, nil
}

// ListLogicalSwitchPorts list logical switch ports which match the given externalIDs,
// result should include all logical switch ports when externalIDs is empty,
// result should include all logical switch ports which externalIDs[key] is not empty when externalIDs[key] is ""
func (c OvnClient) ListLogicalSwitchPorts(needVendorFilter bool, externalIDs map[string]string) ([]ovnnb.LogicalSwitchPort, error) {
	lspList := make([]ovnnb.LogicalSwitchPort, 0)

	if err := c.WhereCache(func(lsp *ovnnb.LogicalSwitchPort) bool {
		if lsp.Type != "" {
			return false
		}

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

		return true
	}).List(context.TODO(), &lspList); err != nil {
		return nil, fmt.Errorf("list logical switch ports: %v", err)
	}

	return lspList, nil
}

func (c OvnClient) LogicalSwitchPortExists(name string) (bool, error) {
	lsp, err := c.GetLogicalSwitchPort(name, true)
	return lsp != nil, err
}

// CreateLogicalSwitchPortOp create operations which create logical switch port
func (c OvnClient) CreateLogicalSwitchPortOp(lsp *ovnnb.LogicalSwitchPort, lsName string) ([]ovsdb.Operation, error) {
	if lsp == nil {
		return nil, fmt.Errorf("logical_switch_port is nil")
	}

	if lsp.ExternalIDs == nil {
		lsp.ExternalIDs = make(map[string]string)
	}

	// attach necessary info
	lsp.ExternalIDs[logicalSwitchKey] = lsName
	lsp.ExternalIDs["vendor"] = util.CniTypeName

	/* create logical switch port */
	lspCreateOp, err := c.Create(lsp)
	if err != nil {
		return nil, fmt.Errorf("generate operations for creating logical switch port %s: %v", lsp.Name, err)
	}

	/* add logical switch port to logical switch*/
	lspAddOp, err := c.LogicalSwitchUpdatePortOp(lsName, lsp.UUID, ovsdb.MutateOperationInsert)
	if err != nil {
		return nil, err
	}

	ops := make([]ovsdb.Operation, 0, len(lspCreateOp)+len(lspAddOp))
	ops = append(ops, lspCreateOp...)
	ops = append(ops, lspAddOp...)

	return ops, nil
}

// DeleteLogicalSwitchPortOp create operations which delete logical switch port
func (c OvnClient) DeleteLogicalSwitchPortOp(lspName string) ([]ovsdb.Operation, error) {
	lsp, err := c.GetLogicalSwitchPort(lspName, true)
	if err != nil {
		return nil, fmt.Errorf("get logical switch port %s when generate delete operations: %v", lspName, err)
	}

	// not found, skip
	if lsp == nil {
		return nil, nil
	}

	lsName, ok := lsp.ExternalIDs[logicalSwitchKey]
	if !ok {
		return nil, fmt.Errorf("no %s exist in lsp's external_ids", logicalSwitchKey)
	}

	// remove logical switch port from logical switch
	lspRemoveOp, err := c.LogicalSwitchUpdatePortOp(lsName, lsp.UUID, ovsdb.MutateOperationDelete)
	if err != nil {
		return nil, fmt.Errorf("generate operations for removing port %s from logical switch %s: %v", lspName, lsName, err)
	}

	// delete logical switch port
	lspDelOp, err := c.Where(lsp).Delete()
	if err != nil {
		return nil, fmt.Errorf("generate operations for deleting logical switch port %s: %v", lspName, err)
	}

	ops := make([]ovsdb.Operation, 0, len(lspRemoveOp)+len(lspDelOp))
	ops = append(ops, lspRemoveOp...)
	ops = append(ops, lspDelOp...)

	return ops, nil
}

// UpdateLogicalSwitchPortOp create operations which update logical switch port
func (c OvnClient) UpdateLogicalSwitchPortOp(lsp *ovnnb.LogicalSwitchPort, fields ...interface{}) ([]ovsdb.Operation, error) {
	// not found, skip
	if lsp == nil {
		return nil, nil
	}

	op, err := c.Where(lsp).Update(lsp, fields...)
	if err != nil {
		return nil, fmt.Errorf("generate operations for updating logical switch port %s: %v", lsp.Name, err)
	}

	return op, nil
}
