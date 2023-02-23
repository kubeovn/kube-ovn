package ovs

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/ovn-org/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
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
		return fmt.Errorf("create logical switch %s: %v", lsName, err)
	}

	if err := c.CreateLocalnetLogicalSwitchPort(lsName, localnetLspName, provider, vlanID); err != nil {
		return fmt.Errorf("create localnet logical switch port %s: %v", localnetLspName, err)
	}

	if err := c.CreateLogicalPatchPort(lsName, lrName, lspName, lrpName, ip, mac, chassises...); err != nil {
		return err
	}

	return nil
}

// CreateLogicalPatchPort create logical router port and associated logical switch port which type is router
func (c *ovnClient) CreateLogicalPatchPort(lsName, lrName, lspName, lrpName, ip, mac string, chassises ...string) error {
	if len(ip) != 0 {
		// check ip format: 192.168.231.1/24,fc00::0af4:01/112
		if err := util.CheckCidrs(ip); err != nil {
			return err
		}
	}

	/* create router port */
	ops, err := c.CreateRouterPortOp(lsName, lrName, lspName, lrpName, ip, mac)
	if err != nil {
		return fmt.Errorf("generate operations for creating patch port: %v", err)
	}

	if err = c.Transact("lrp-lsp-add", ops); err != nil {
		return fmt.Errorf("create logical patch port %s and %s: %v", lspName, lrpName, err)
	}

	/* create gateway chassises for logical router port */
	if err = c.CreateGatewayChassises(lrpName, chassises...); err != nil {
		return err
	}

	return nil
}

// DeleteLogicalGatewaySwitch delete gateway switch and corresponding port
func (c *ovnClient) DeleteLogicalGatewaySwitch(lsName, lrName string) error {
	lrpName := fmt.Sprintf("%s-%s", lrName, lsName)

	// all corresponding logical switch port(e.g. localnet port and normal port) will be deleted when delete logical switch
	lsDelOp, err := c.DeleteLogicalSwitchOp(lsName)
	if err != nil {
		return fmt.Errorf("generate operations for deleting gateway switch %s: %v", lsName, err)
	}

	lrpDelOp, err := c.DeleteLogicalRouterPortOp(lrpName)
	if err != nil {
		return fmt.Errorf("generate operations for deleting gateway router port %s: %v", lrpName, err)
	}

	ops := make([]ovsdb.Operation, 0, len(lsDelOp)+len(lrpDelOp))
	ops = append(ops, lsDelOp...)
	ops = append(ops, lrpDelOp...)

	if err = c.Transact("gw-ls-del", ops); err != nil {
		return fmt.Errorf("delete gateway switch %s: %v", lsName, err)
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

func (c *ovnClient) CreateRouterPortOp(lsName, lrName, lspName, lrpName, ip, mac string) ([]ovsdb.Operation, error) {
	/* do nothing if logical switch port exist */
	lspExist, err := c.LogicalSwitchPortExists(lspName)
	if err != nil {
		return nil, err
	}

	// lsp or lrp must all exist or not because of ovsdb ACID transcation
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
		return nil, err
	}

	/* create logical router port */
	lrp := &ovnnb.LogicalRouterPort{
		UUID:     ovsclient.NamedUUID(),
		Name:     lrpName,
		Networks: strings.Split(ip, ","),
		MAC:      mac,
	}

	lrpCreateOp, err := c.CreateLogicalRouterPortOp(lrp, lrName)
	if err != nil {
		return nil, err
	}

	ops := make([]ovsdb.Operation, 0, len(lspCreateOp)+len(lrpCreateOp))
	ops = append(ops, lspCreateOp...)
	ops = append(ops, lrpCreateOp...)

	return ops, nil
}

// RemoveLogicalPatchPort delete logical router port and associated logical switch port which type is router
func (c *ovnClient) RemoveLogicalPatchPort(lspName, lrpName string) error {
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

// GetEntityInfo get entity info by column which is the index,
// reference to ovn-nb.ovsschema(ovsdb-client get-schema unix:/var/run/ovn/ovnnb_db.sock OVN_Northbound) for more information,
// UUID is index
func (c *ovnClient) GetEntityInfo(entity interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	entityPtr := reflect.ValueOf(entity)
	if entityPtr.Kind() != reflect.Pointer {
		return fmt.Errorf("entity must be pointer")
	}

	err := c.Get(ctx, entity)
	if err != nil {
		return err
	}

	return nil
}
