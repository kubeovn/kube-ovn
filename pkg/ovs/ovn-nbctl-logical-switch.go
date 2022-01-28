package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c Client) GetLogicalSwitch(name string, needVendorFilter, ignoreNotFound bool) (*ovnnb.LogicalSwitch, error) {
	lsList := make([]ovnnb.LogicalSwitch, 0, 1)
	if err := c.nbClient.WhereCache(func(ls *ovnnb.LogicalSwitch) bool {
		if ls.Name != name {
			return false
		}
		if !needVendorFilter {
			return true
		}
		return len(ls.ExternalIDs) != 0 && ls.ExternalIDs["vendor"] == util.CniTypeName
	}).List(context.TODO(), &lsList); err != nil {
		return nil, fmt.Errorf("failed to list logical switch with name %s: %v", name, err)
	}

	if len(lsList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("logical switch %s does not exist", name)
	}
	if len(lsList) != 1 {
		return nil, fmt.Errorf("found multiple logical switches with the same name %s", name)
	}

	return &lsList[0], nil
}

func (c Client) createLS(name string) error {
	ls, err := c.GetLogicalSwitch(name, false, true)
	if err != nil {
		return err
	}

	var ops []ovsdb.Operation
	if ls != nil {
		if len(ls.ExternalIDs) != 0 && ls.ExternalIDs["vendor"] == util.CniTypeName {
			return nil
		}
		if ls.ExternalIDs == nil {
			ls.ExternalIDs = make(map[string]string)
		}
		ls.ExternalIDs["vendor"] = util.CniTypeName
		if ops, err = c.nbClient.Where(ls).Update(ls, &ls.ExternalIDs); err != nil {
			return fmt.Errorf("failed to generate update operations for logical switch %s: %v", name, err)
		}
	} else {
		ls = &ovnnb.LogicalSwitch{
			Name:        name,
			ExternalIDs: map[string]string{"vendor": util.CniTypeName},
		}
		if ops, err = c.nbClient.Create(ls); err != nil {
			return fmt.Errorf("failed to generate create operations for logical switch %s: %v", name, err)
		}
	}

	if err = Transact(c.nbClient, "ls-add", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to create logical switch %s: %v", name, err)
	}

	return nil
}

// CreateLogicalSwitch creates a logical switch in ovn and connect it to router if necessary
func (c Client) CreateLogicalSwitch(name, router, subnet, gateway string, needRouter bool) error {
	if err := c.createLS(name); err != nil {
		return err
	}

	if needRouter {
		if err := c.createRouterPort(name, router, util.GenerateMac(), util.GetIpAddrWithMask(gateway, subnet)); err != nil {
			klog.Errorf("failed to connect logical switch %s to logical router %s: %v", name, router, err)
			return err
		}
	}

	return nil
}

func (c Client) CreateGatewaySwitch(name, network, mac, ip string, vlan *int, chassis []string) error {
	if err := c.createLS(name); err != nil {
		return err
	}

	lrp := fmt.Sprintf("%s-%s", c.ClusterRouter, name)
	// if err := c.createLRP(lrp, c.ClusterRouter, mac, ip, chassis); err != nil {
	if err := c.createLRP(lrp, c.ClusterRouter, mac, ip, nil); err != nil {
		return err
	}

	lsp := fmt.Sprintf("%s-%s", name, c.ClusterRouter)
	if err := c.createLSP(lsp, "router", name, "router", "", "", nil, map[string]string{"router-port": lrp}, nil, false, false, ""); err != nil {
		return err
	}

	localnetPort := fmt.Sprintf("ln-%s", name)
	if err := c.createLSP(localnetPort, "localnet", name, "unknown", "", "", vlan, map[string]string{"network_name": network}, nil, false, false, ""); err != nil {
		return err
	}

	for index, chassis := range chassis {
		if _, err := c.ovnNbCommand("lrp-set-gateway-chassis", lrp, chassis, fmt.Sprintf("%d", 100-index)); err != nil {
			return fmt.Errorf("failed to set gateway chassis, %v", err)
		}
	}

	return nil
}

func (c Client) DeleteGatewaySwitch(name string) error {
	if err := c.DeleteLogicalRouterPort(fmt.Sprintf("%s-%s", c.ClusterRouter, name)); err != nil {
		return err
	}
	return c.DeleteLogicalSwitch(name)
}

// ListLogicalSwitch list logical switch names
func (c Client) ListLogicalSwitch(needVendorFilter bool) ([]string, error) {
	lsList := make([]ovnnb.LogicalSwitch, 0)
	if err := c.nbClient.WhereCache(func(ls *ovnnb.LogicalSwitch) bool {
		if !needVendorFilter {
			return true
		}
		return len(ls.ExternalIDs) != 0 && ls.ExternalIDs["vendor"] == util.CniTypeName
	}).List(context.TODO(), &lsList); err != nil {
		return nil, fmt.Errorf("failed to list logical switch: %v", err)
	}

	result := make([]string, 0, len(lsList))
	for _, ls := range lsList {
		result = append(result, ls.Name)
	}
	return result, nil
}

func (c Client) LogicalSwitchExists(name string, needVendorFilter bool) (bool, error) {
	lsList := make([]ovnnb.LogicalSwitch, 0)
	if err := c.nbClient.WhereCache(func(ls *ovnnb.LogicalSwitch) bool {
		if ls.Name != name {
			return false
		}
		if !needVendorFilter {
			return true
		}
		return len(ls.ExternalIDs) != 0 && ls.ExternalIDs["vendor"] == util.CniTypeName
	}).List(context.TODO(), &lsList); err != nil {
		return false, fmt.Errorf("failed to list logical switch with name %s: %v", name, err)
	}

	return len(lsList) != 0, nil
}

// DeleteLogicalSwitch delete logical switch
func (c Client) DeleteLogicalSwitch(name string) error {
	ls, err := c.GetLogicalSwitch(name, false, true)
	if err != nil {
		return err
	}
	if ls == nil {
		return nil
	}

	ops, err := c.nbClient.Where(ls).Delete()
	if err != nil {
		return fmt.Errorf("failed to generate delete operations for logical switch %s: %v", name, err)
	}
	if err = Transact(c.nbClient, "ls-del", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to delete logical switch %s: %v", name, err)
	}

	return nil
}
