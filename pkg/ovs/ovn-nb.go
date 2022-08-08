package ovs

import (
	"fmt"
	"strings"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/ovn-org/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// createRouterPort create logical router port and associated logical switch port which type is router
func (c OvnClient) CreateRouterPort(lsName, lrName, ip string, chassises ...string) error {
	// check ip format: 192.168.231.1/24,fc00::0af4:01/112
	if err := util.CheckCidrs(ip); err != nil {
		return err
	}

	// create gateway chassis
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)
	if err := c.CreateGatewayChassises(lrpName, chassises); err != nil {
		return err
	}

	// create router type port
	return c.CreateRouterTypePort(lsName, lrName, ip, func(lrp *ovnnb.LogicalRouterPort) {
		if len(chassises) != 0 {
			lrp.GatewayChassis = chassises
		}
	})
}

// DeleteRouterPort delete logical router port and associated logical switch port which type is router
func (c OvnClient) DeleteRouterPort(lspName, lrpName string, chassises ...string) error {
	// delete gateway chassises
	err := c.DeleteGatewayChassises(lrpName, chassises)
	if err != nil {
		return err
	}

	// remove router type port
	return c.RemoveRouterTypePort(lspName, lrpName)
}

func (c OvnClient) CreateRouterTypePort(lsName, lrName, ip string, LrpOptions ...func(lrp *ovnnb.LogicalRouterPort)) error {
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

	// this condition is "||" because of the ovsdb transcation
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
		UUID:     ovsclient.UUID(),
		Name:     lrpName,
		MAC:      util.GenerateMac(),
		Networks: strings.Split(ip, ","),
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
func (c OvnClient) RemoveRouterTypePort(lspName, lrpName string) error {
	/* delete logical switch port*/
	lsp, err := c.GetLogicalSwitchPort(lspName, true)
	if err != nil {
		return err
	}

	lspDelOp, err := c.DeleteLogicalSwitchPortOp(lsp)
	if err != nil {
		return err
	}

	/* delete logical router port*/
	lrp, err := c.GetLogicalRouterPort(lrpName, true)
	if err != nil {
		return err
	}

	lrpDelOp, err := c.DeleteLogicalRouterPortOp(lrp)
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
