package ovs

import (
	"context"
	"fmt"

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

// DeleteLogicalSwitchPort delete logical switch port in ovn
func (c OvnClient) DeleteLogicalSwitchPort(name string) error {
	lsp, err := c.GetLogicalSwitchPort(name, true)
	if nil != err {
		return err
	}

	ops, err := c.DeleteLogicalSwitchPortOp(lsp)
	if nil != err {
		return err
	}

	if err = c.Transact("lsp-del", ops); nil != err {
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
				if 0 == len(v) {
					if 0 == len(lsp.ExternalIDs[k]) {
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
	if nil == lsp {
		return nil, fmt.Errorf("logical_switch_port is nil")
	}

	if nil == lsp.ExternalIDs {
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
	if nil != err {
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
	if nil == lsp {
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
