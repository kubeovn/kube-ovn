package ovs

import (
	"context"
	"fmt"
	"strings"

	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c Client) GetLRP(name string, ignoreNotFound bool) (*ovnnb.LogicalRouterPort, error) {
	lrpList := make([]ovnnb.LogicalRouterPort, 0, 1)
	if err := c.nbClient.WhereCache(func(lrp *ovnnb.LogicalRouterPort) bool { return lrp.Name == name }).List(context.TODO(), &lrpList); err != nil {
		return nil, fmt.Errorf("failed to list logical router port: %v", err)
	}
	if len(lrpList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("logical router port %s does not exist", name)
	}
	if len(lrpList) != 1 {
		return nil, fmt.Errorf("found multiple logical router ports with the same name %s", name)
	}

	return &lrpList[0], nil
}

func (c Client) createLRP(name, router, mac, ips string, gatewayChassis []string) error {
	lrp, err := c.GetLRP(name, true)
	if err != nil {
		return err
	}

	var needCreate bool
	if lrp == nil {
		needCreate = true
		lrp = &ovnnb.LogicalRouterPort{UUID: ovsclient.NamedUUID(), Name: name}
	}

	lrp.MAC = mac
	lrp.Networks = strings.Split(ips, ",")
	// if len(gatewayChassis) != 0 {
	// 	lrp.GatewayChassis = gatewayChassis
	// }
	if lrp.ExternalIDs == nil {
		lrp.ExternalIDs = make(map[string]string)
	}
	lrp.ExternalIDs["vendor"] = util.CniTypeName

	var ops []ovsdb.Operation
	if needCreate {
		lr, err := c.GetLogicalRouter(router, false)
		if err != nil {
			return err
		}

		if ops, err = c.nbClient.Create(lrp); err != nil {
			return fmt.Errorf("failed to generate create operations for logical router port %s: %v", name, err)
		}

		insertOps, err := c.nbClient.Where(lr).Mutate(lr, model.Mutation{
			Field:   &lr.Ports,
			Mutator: ovsdb.MutateOperationInsert,
			Value:   []string{lrp.UUID},
		})
		if err != nil {
			return fmt.Errorf("failed to generate operations for attaching logical router port %s to logical router %s: %v", name, router, err)
		}
		ops = append(ops, insertOps...)
	} else if ops, err = c.nbClient.Where(lrp).Update(lrp, &lrp.MAC, &lrp.Networks, &lrp.GatewayChassis, &lrp.ExternalIDs); err != nil {
		return fmt.Errorf("failed to generate update operations for logical router port %s: %v", name, err)
	}

	if err = Transact(c.nbClient, "lrp-add", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to create logical router port %s: %v", name, err)
	}

	for index, chassis := range gatewayChassis {
		if _, err := c.ovnNbCommand("lrp-set-gateway-chassis", name, chassis, fmt.Sprintf("%d", 100-index)); err != nil {
			return fmt.Errorf("failed to set gateway chassis: %v", err)
		}
	}

	return nil
}

func (c Client) LrpSetGatewayChassis(port, chassis string, priority int) error {
	lrp, err := c.GetLRP(port, false)
	if err != nil {
		return err
	}

	if util.ContainsString(lrp.GatewayChassis, chassis) {
		return nil
	}
	lrp.GatewayChassis = append(lrp.GatewayChassis, chassis)
	ops, err := c.nbClient.Where(lrp).Update(lrp, &lrp.GatewayChassis)
	if err != nil {
		return fmt.Errorf("failed to generate update operations for logical router port %s: %v", port, err)
	}
	if err = Transact(c.nbClient, "lrp-set-gateway-chassis", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to set gateway chassis of logical router port %s: %v", port, err)
	}

	// TODO: set priority

	return nil
}

// DeleteLogicalRouterPort delete logical switch port in ovn
func (c Client) DeleteLogicalRouterPort(name string) error {
	lrp, err := c.GetLRP(name, true)
	if err != nil {
		return err
	}
	if lrp == nil {
		return nil
	}

	ops, err := c.nbClient.Where(lrp).Delete()
	if err != nil {
		return fmt.Errorf("failed to generate delete operations for logical router port %s: %v", name, err)
	}

	lrList, err := c.ListLogicalRouters(false)
	if err != nil {
		return err
	}

	var lr *ovnnb.LogicalRouter
	for i := range lrList {
		if util.ContainsString(lrList[i].Ports, lrp.UUID) {
			lr = &lrList[i]
			break
		}
	}

	if lr != nil {
		deleteOps, err := c.nbClient.Where(lr).Mutate(lr, model.Mutation{
			Field:   &lr.Ports,
			Mutator: ovsdb.MutateOperationDelete,
			Value:   []string{lrp.UUID},
		})
		if err != nil {
			return fmt.Errorf("failed to generate operations for detaching logical router port %s from logical router %s: %v", name, lr.Name, err)
		}
		ops = append(ops, deleteOps...)
	}

	if err = Transact(c.nbClient, "lrp-del", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to delete logical router port %s: %v", name, err)
	}

	return nil
}

func (c Client) CreateICLogicalRouterPort(az, mac, subnet string, gatewayChassis []string) error {
	lrp := fmt.Sprintf("%s-ts", az)
	if err := c.createLRP(lrp, c.ClusterRouter, mac, subnet, gatewayChassis); err != nil {
		return fmt.Errorf("failed to create ovn-ic LRP %s: %v", lrp, err)
	}

	lsp := fmt.Sprintf("ts-%s", az)
	if err := c.createLSP(lsp, "router", util.InterconnectionSwitch, "router", "", "", nil, map[string]string{"router-port": lrp}, nil, false, false, ""); err != nil {
		return fmt.Errorf("failed to create ovn-ic LSP %s: %v", lsp, err)
	}

	return nil
}

func (c Client) DeleteICLogicalRouterPort(az string) error {
	if err := c.DeleteLogicalRouterPort(fmt.Sprintf("%s-ts", az)); err != nil {
		return fmt.Errorf("failed to delete ovn-ic logical router port: %v", err)
	}
	if err := c.DeleteLogicalSwitchPort(fmt.Sprintf("ts-%s", az)); err != nil {
		return fmt.Errorf("failed to delete ovn-ic logical switch port: %v", err)
	}
	return nil
}
