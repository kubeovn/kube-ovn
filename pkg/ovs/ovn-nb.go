package ovs

import (
	"fmt"
	"strings"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/ovn-org/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	logicalRouterKey      = "lr"
	logicalSwitchKey      = "ls"
	portGroupKey          = "pg"
	aclParentKey          = "parent"
	associatedSgKeyPrefix = "associated_sg_"
	sgsKey                = "security_groups"
	sgKey                 = "sg"
)

// CreateGatewayLogicalSwitch create gateway switch connect external networks
func (c *ovnClient) CreateGatewayLogicalSwitch(lsName, lrName, provider, ip, mac string, vlanID int, chassises ...string) error {
	lspName := fmt.Sprintf("%s-%s", lsName, lrName)
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)
	localnetLspName := fmt.Sprintf("ln-%s", lsName)

	if err := c.CreateBareLogicalSwitch(lsName); err != nil {
		return fmt.Errorf("create logical switch %s failed: %v", lsName, err)
	}

	if err := c.CreateLocalnetLogicalSwitchPort(lsName, localnetLspName, provider, vlanID); err != nil {
		return fmt.Errorf("create localnet logical switch port %s failed: %v", localnetLspName, err)
	}

	if err := c.CreateRouterPort(lsName, lrName, ip, mac, chassises...); err != nil {
		return fmt.Errorf("create router port %s and %s failed: %v", lspName, lrpName, err)
	}

	return nil
}

// createRouterPort create logical router port and associated logical switch port which type is router
func (c *ovnClient) CreateRouterPort(lsName, lrName, ip, mac string, chassises ...string) error {
	if len(ip) != 0 {
		// check ip format: 192.168.231.1/24,fc00::0af4:01/112
		if err := util.CheckCidrs(ip); err != nil {
			return err
		}
	}

	// create gateway chassis
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)
	if err := c.CreateGatewayChassises(lrpName, chassises); err != nil {
		return err
	}

	// create router type port
	return c.CreateRouterTypePort(lsName, lrName, mac, func(lrp *ovnnb.LogicalRouterPort) {
		if len(ip) != 0 {
			lrp.Networks = strings.Split(ip, ",")
		}

		if len(chassises) != 0 {
			lrp.GatewayChassis = chassises
		}
	})
}

// DeleteRouterPort delete logical router port and associated logical switch port which type is router
func (c *ovnClient) DeleteRouterPort(lspName, lrpName string, chassises ...string) error {
	// delete gateway chassises
	err := c.DeleteGatewayChassises(lrpName, chassises)
	if err != nil {
		return err
	}

	// remove router type port
	return c.RemoveRouterTypePort(lspName, lrpName)
}

// DeleteLogicalGatewaySwitch delete gateway switch and corresponding port
func (c *ovnClient) DeleteLogicalGatewaySwitch(lsName, lrName string) error {
	lspName := fmt.Sprintf("%s-%s", lsName, lrName)
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)
	localnetLspName := fmt.Sprintf("ln-%s", lsName)

	lsDelOp, err := c.DeleteLogicalSwitchOp(lsName)
	if err != nil {
		return fmt.Errorf("generate operations for deleting gateway switch %s: %v", lsName, err)
	}

	localnetLspDelOp, err := c.DeleteLogicalSwitchPortOp(localnetLspName)
	if err != nil {
		return fmt.Errorf("generate operations for deleting gateway switch localnet port %s: %v", localnetLspName, err)
	}

	lspDelOp, err := c.DeleteLogicalSwitchPortOp(lspName)
	if err != nil {
		return fmt.Errorf("generate operations for deleting gateway switch port %s: %v", lspName, err)
	}

	lrpDelOp, err := c.DeleteLogicalRouterPortOp(lrpName)
	if err != nil {
		return fmt.Errorf("generate operations for deleting gateway router port %s: %v", lrpName, err)
	}

	ops := make([]ovsdb.Operation, 0, len(lsDelOp)+len(localnetLspDelOp)+len(lspDelOp)+len(lrpDelOp))
	ops = append(ops, lsDelOp...)
	ops = append(ops, localnetLspDelOp...)
	ops = append(ops, lspDelOp...)
	ops = append(ops, lrpDelOp...)

	if err = c.Transact("gw-ls-del", ops); err != nil {
		return fmt.Errorf("create router type port %s and %s: %v", lspName, lrpName, err)
	}

	return nil
}

func (c *ovnClient) DeleteSecurityGroup(sgName string) error {
	pgName := GetSgPortGroupName(sgName)

	// clear acl
	if err := c.DeleteAcls(pgName, portGroupKey, ""); err != nil {
		return fmt.Errorf("delete acls from port group %s: %v", pgName, err)
	}

	// clear address_set
	if err := c.DeleteAddressSets(map[string]string{sgKey: sgName}); err != nil {
		return err
	}

	if sgName == util.DefaultSecurityGroupName {
		if err := c.SetLogicalSwitchPortsSecurityGroup(sgName, "remove"); err != nil {
			return fmt.Errorf("clear default security group %s from logical switch ports: %v", sgName, err)
		}
	}

	// delete pg
	if err := c.DeletePortGroup(pgName); err != nil {
		return err
	}

	return nil
}

func (c *ovnClient) CreateRouterTypePort(lsName, lrName, mac string, LrpOptions ...func(lrp *ovnnb.LogicalRouterPort)) error {
	lspName := fmt.Sprintf("%s-%s", lsName, lrName)
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)

	/* do nothing if logical switch port or logical router port exist */
	lspExist, err := c.LogicalSwitchPortExists(lspName)
	if err != nil {
		return err
	}

	lrpExist, err := c.LogicalRouterPortExists(lrpName)
	if err != nil {
		return err
	}

	// this condition is "||" because of ovsdb ACID transcation
	if lspExist || lrpExist {
		return nil
	}

	/* create logical switch port */
	lsp := &ovnnb.LogicalSwitchPort{
		UUID:      ovsclient.UUID(),
		Name:      lspName,
		Addresses: []string{"router"},
		Type:      "router",
		Options: map[string]string{
			"router-port": lrpName,
		},
	}

	lspCreateOp, err := c.CreateLogicalSwitchPortOp(lsp, lsName)
	if err != nil {
		return err
	}

	/* create logical router port */
	lrp := &ovnnb.LogicalRouterPort{
		UUID: ovsclient.UUID(),
		Name: lrpName,
		MAC:  mac,
	}

	for _, option := range LrpOptions {
		option(lrp)
	}

	lrpCreateOp, err := c.CreateLogicalRouterPortOp(lrp, lrName)
	if err != nil {
		return err
	}

	ops := make([]ovsdb.Operation, 0, len(lspCreateOp)+len(lrpCreateOp))
	ops = append(ops, lspCreateOp...)
	ops = append(ops, lrpCreateOp...)

	if err = c.Transact("lrp-lsp-add", ops); err != nil {
		return fmt.Errorf("create router type port %s and %s: %v", lspName, lrpName, err)
	}

	return nil
}

// RemoveRouterPort delete logical router port from logical router and delete logical switch port from logical switch
func (c *ovnClient) RemoveRouterTypePort(lspName, lrpName string) error {
	/* delete logical switch port*/
	lspDelOp, err := c.DeleteLogicalSwitchPortOp(lspName)
	if err != nil {
		return err
	}

	/* delete logical router port*/
	lrpDelOp, err := c.DeleteLogicalRouterPortOp(lrpName)
	if err != nil {
		return err
	}

	ops := make([]ovsdb.Operation, 0, len(lspDelOp)+len(lrpDelOp))
	ops = append(ops, lspDelOp...)
	ops = append(ops, lrpDelOp...)

	if err = c.Transact("lrp-lsp-del", ops); err != nil {
		return fmt.Errorf("delete logical switch port %s and delete logical router port %s: %v", lspName, lrpName, err)
	}

	return nil
}
