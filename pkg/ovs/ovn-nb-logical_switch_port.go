package ovs

import (
	"context"
	"fmt"
	"strings"

	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/ovsdb"

	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	logicalSwitchKey = "logical_switch"
)

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

// SetLogicalSwitchPortSecurity set logical switch port port_security
func (c OvnClient) SetLogicalSwitchPortSecurity(portSecurity bool, lspName, mac, ips, vips string) error {
	lsp, err := c.GetLogicalSwitchPort(lspName, false)
	if err != nil {
		return err
	}

	// note: addresses is the first element of port_security
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
		return fmt.Errorf("update logical switch port %s: %v", lspName, err)
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
		return fmt.Errorf("generate update operations for logical switch port %s: %v", lsp.Name, err)
	}

	if err = c.Transact("lsp-update", op); err != nil {
		return fmt.Errorf("update logical switch port %s: %v", lsp.Name, err)
	}

	return nil
}

// DeleteLogicalSwitchPort delete logical switch port in ovn
func (c OvnClient) DeleteLogicalSwitchPort(name string) error {
	lsp, err := c.GetLogicalSwitchPort(name, true)
	if err != nil {
		return err
	}

	ops, err := c.DeleteLogicalSwitchPortOp(lsp)
	if err != nil {
		return err
	}

	if err = c.Transact("lsp-del", ops); err != nil {
		return fmt.Errorf("delete logical switch port %s", name)
	}

	return nil
}

func (c OvnClient) GetLogicalSwitchPort(name string, ignoreNotFound bool) (*ovnnb.LogicalSwitchPort, error) {
	lsp := &ovnnb.LogicalSwitchPort{Name: name}
	if err := c.Get(context.TODO(), lsp); err != nil {
		if ignoreNotFound && err == client.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get logical switch port %s: %v", name, err)
	}

	return lsp, nil
}

func (c OvnClient) ListRemoteTypeLogicalSwitchPorts() ([]ovnnb.LogicalSwitchPort, error) {
	lspList := make([]ovnnb.LogicalSwitchPort, 0, 1)
	if err := c.WhereCache(func(lsp *ovnnb.LogicalSwitchPort) bool {
		return lsp.Type == "remote"
	}).List(context.TODO(), &lspList); err != nil {
		return nil, fmt.Errorf("failed to list logical switch port which type is remote: %v", err)
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
		klog.Errorf("list logical switch ports: %v", err)
		return nil, err
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
		return nil, fmt.Errorf("generate create operations for logical switch port %s: %v", lsp.Name, err)
	}

	/* add logical switch port to logical switch*/
	lspAddOp, err := c.LogicalSwitchOp(lsName, lsp, true)
	if err != nil {
		return nil, err
	}

	ops := make([]ovsdb.Operation, 0, len(lspCreateOp)+len(lspAddOp))
	ops = append(ops, lspCreateOp...)
	ops = append(ops, lspAddOp...)

	return ops, nil
}

// DeleteLogicalSwitchPortOp create operations which delete logical switch port
func (c OvnClient) DeleteLogicalSwitchPortOp(lsp *ovnnb.LogicalSwitchPort) ([]ovsdb.Operation, error) {
	// not found, skip
	if lsp == nil {
		return nil, nil
	}

	lsName, ok := lsp.ExternalIDs[logicalSwitchKey]
	if !ok {
		return nil, fmt.Errorf("no %s exist in lsp's external_ids", logicalSwitchKey)
	}

	// remove logical switch port from logical switch
	lspRemoveOp, err := c.LogicalSwitchOp(lsName, lsp, false)
	if err != nil {
		return nil, err
	}

	// delete logical switch port
	lspDelOp, err := c.Where(lsp).Delete()
	if err != nil {
		return nil, err
	}

	ops := make([]ovsdb.Operation, 0, len(lspRemoveOp)+len(lspDelOp))
	ops = append(ops, lspRemoveOp...)
	ops = append(ops, lspDelOp...)

	return ops, nil
}
