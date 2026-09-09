package ovs

import (
	"fmt"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	logicalRouterKey      = "lr"
	LogicalSwitchKey      = "ls"
	portGroupKey          = "pg"
	aclParentKey          = "parent"
	associatedSgKeyPrefix = "associated_sg_"
	sgsKey                = "security_groups"
	sgKey                 = "sg"
	PortKey               = "port"
)

// CreateGatewayLogicalSwitch create gateway switch connect external networks
func (c *OVNNbClient) CreateGatewayLogicalSwitch(lsName, lrName, provider, ip, mac string, vlanID int, chassises ...string) error {
	lspName := fmt.Sprintf("%s-%s", lsName, lrName)
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)

	// delete old localnet lsp when upgrade before v1.12
	oldLocalnetLspName := "ln-" + lsName
	if err := c.DeleteLogicalSwitchPort(oldLocalnetLspName); err != nil {
		return logWrap(err, wrapErr("failed to delete old localnet %s: %w", oldLocalnetLspName))
	}

	localnetLspName := GetLocalnetName(lsName)
	if err := c.CreateBareLogicalSwitch(lsName); err != nil {
		return logWrap(err, wrapErr("create logical switch %s: %w", lsName))
	}

	if err := c.CreateLocalnetLogicalSwitchPort(lsName, localnetLspName, provider, "", vlanID); err != nil {
		return logWrap(err, wrapErr("create localnet logical switch port %s: %w", localnetLspName))
	}

	return c.CreateLogicalPatchPort(lsName, lrName, lspName, lrpName, ip, mac, chassises...)
}

// CreateLogicalPatchPort create logical router port and associated logical switch port which type is router
func (c *OVNNbClient) CreateLogicalPatchPort(lsName, lrName, lspName, lrpName, ip, mac string, chassises ...string) error {
	if len(ip) != 0 {
		// check ip format: 192.168.231.1/24,fc00::0af4:01/112
		if err := util.CheckCidrs(ip); err != nil {
			err := fmt.Errorf("invalid ip %s: %w", ip, err)
			return logErr(err)
		}
	}
	if mac == "" {
		mac = util.GenerateMac()
	}

	/* create router port */
	ops, err := c.CreateRouterPortOp(lsName, lrName, lspName, lrpName, ip, mac)
	if err := c.transactGenerated("lrp-lsp-add", ops, err,
		wrapErr("generate operations for creating patch port: %w"),
		wrapErr("create logical patch port %s and %s: %w", lspName, lrpName),
	); err != nil {
		return err
	}

	/* create gateway chassises for logical router port */
	if err := c.CreateGatewayChassises(lrpName, chassises...); err != nil {
		return logFmt("create gateway chassises for logical router port %s: %w", lrpName, err)
	}
	return nil
}

// DeleteLogicalGatewaySwitch delete gateway switch and corresponding port
func (c *OVNNbClient) DeleteLogicalGatewaySwitch(lsName, lrName string) error {
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)

	// all corresponding logical switch port(e.g. localnet port and normal port) will be deleted when delete logical switch
	lsDelOp, err := c.DeleteLogicalSwitchOp(lsName)
	if err != nil {
		return logWrap(err, wrapErr("generate operations for deleting gateway switch %s: %w", lsName))
	}

	lrpDelOp, err := c.DeleteLogicalRouterPortOp(lrpName)
	if err != nil {
		return logWrap(err, wrapErr("generate operations for deleting gateway router port %s: %w", lrpName))
	}

	return c.transactGenerated("gw-ls-del", appendOps(lsDelOp, lrpDelOp), nil, nil, wrapErr("delete gateway switch %s: %w", lsName))
}

func (c *OVNNbClient) DeleteSecurityGroup(sgName string) error {
	pgName := GetSgPortGroupName(sgName)

	// clear acl
	if err := c.DeleteAcls(pgName, portGroupKey, "", nil); err != nil {
		return logWrap(err, wrapErr("delete acls from port group %s: %w", pgName))
	}

	// clear address_set
	if err := c.DeleteAddressSets(map[string]string{sgKey: sgName}); err != nil {
		return logErr(err)
	}

	if sgName == util.DefaultSecurityGroupName {
		if err := c.SetLogicalSwitchPortsSecurityGroup(sgName, "remove"); err != nil {
			return logWrap(err, wrapErr("clear default security group %s from logical switch ports: %w", sgName))
		}
	}

	// delete pg
	return c.DeletePortGroup(pgName)
}

func (c *OVNNbClient) CreateRouterPortOp(lsName, lrName, lspName, lrpName, ip, mac string) ([]ovsdb.Operation, error) {
	/* do nothing if logical switch port exist */
	lspExist, err := c.LogicalSwitchPortExists(lspName)
	if err != nil {
		return nil, logErr(err)
	}

	// lsp or lrp must all exist or not because of ovsdb ACID transaction
	if lspExist {
		return nil, nil
	}

	/* create logical switch port */
	lsp := &ovnnb.LogicalSwitchPort{
		UUID:      ovsclient.NamedUUID(),
		Name:      lspName,
		Addresses: []string{"router"},
		Type:      "router",
		Options: map[string]string{
			"router-port": lrpName,
		},
	}

	lspCreateOp, err := c.CreateLogicalSwitchPortOp(lsp, lsName)
	if err != nil {
		return nil, logErr(err)
	}

	/* create logical router port */
	lrp := &ovnnb.LogicalRouterPort{
		UUID:     ovsclient.NamedUUID(),
		Name:     lrpName,
		Networks: strings.Split(ip, ","),
		MAC:      mac,
		ExternalIDs: map[string]string{
			"vendor": util.CniTypeName,
		},
	}

	lrpCreateOp, err := c.CreateLogicalRouterPortOp(lrp, lrName)
	if err != nil {
		return nil, logErr(err)
	}

	return appendOps(lspCreateOp, lrpCreateOp), nil
}

// RemoveLogicalPatchPort delete logical router port and associated logical switch port which type is router
func (c *OVNNbClient) RemoveLogicalPatchPort(lspName, lrpName string) error {
	/* delete logical switch port*/
	lsp, err := c.GetLogicalSwitchPort(lspName, true)
	if err != nil {
		return logWrap(err, wrapErr("failed to get logical switch port %s: %w", lspName))
	}
	var lspDelOp []ovsdb.Operation
	if lsp != nil {
		if lspDelOp, err = c.DeleteLogicalSwitchPortOp(lsp.ExternalIDs[LogicalSwitchKey], lsp.UUID); err != nil {
			return logErr(err)
		}
	}

	/* delete logical router port*/
	lrpDelOp, err := c.DeleteLogicalRouterPortOp(lrpName)
	if err != nil {
		return logErr(err)
	}

	return c.transactGenerated("lrp-lsp-del", appendOps(lspDelOp, lrpDelOp), nil, nil,
		wrapErr("delete logical switch port %s and delete logical router port %s: %w", lspName, lrpName),
	)
}
