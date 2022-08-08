package ovs

import (
	"fmt"
	"strings"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/ovn-org/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// createRouterPort create logical router port and associated logical switch port type is router
func (c OvnClient) CreateRouterPort(lsName, lrName, ip string) error {
	lspName := fmt.Sprintf("%s-%s", lsName, lrName)
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)

	return c.CreateRouterTypePort(lsName, lspName, lrName, lrpName, ip)
}

func (c OvnClient) CreateICLogicalRouterPort(az, subnet string, chassises []string) error {
	lspName := fmt.Sprintf("ts-%s", az)
	lrpName := fmt.Sprintf("%s-ts", az)

	err := c.CreateGatewayChassises(lrpName, chassises)
	if nil != err {
		return err
	}

	if err := c.CreateRouterTypePort(util.InterconnectionSwitch, lspName, c.ClusterRouter, lrpName, subnet,
		func(lrp *ovnnb.LogicalRouterPort) {
			if 0 != len(chassises) {
				lrp.GatewayChassis = chassises
			}
		}); nil != err {
		return err
	}

	return nil
}

func (c OvnClient) CreateRouterTypePort(lsName, lspName, lrName, lrpName, ip string, LrpOptions ...func(lrp *ovnnb.LogicalRouterPort)) error {
	/* do nothing if logical switch port or logical router port exist */
	lspExist, err := c.LogicalSwitchPortExists(lspName)
	if nil != err {
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
	if nil != err {
		return err
	}

	lspDelOp, err := c.DeleteLogicalSwitchPortOp(lsp)
	if err != nil {
		return err
	}

	/* delete logical router port*/
	lrp, err := c.GetLogicalRouterPort(lrpName, true)
	if nil != err {
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
