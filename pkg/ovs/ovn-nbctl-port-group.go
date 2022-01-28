package ovs

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c Client) GetPortGroup(name string, ignoreNotFound bool) (*ovnnb.PortGroup, error) {
	pgList := make([]ovnnb.PortGroup, 0, 1)
	if err := c.nbClient.WhereCache(func(pg *ovnnb.PortGroup) bool { return pg.Name == name }).List(context.TODO(), &pgList); err != nil {
		return nil, fmt.Errorf("failed to list port group with name %s: %v", name, err)
	}
	if len(pgList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("port group %s does not exist", name)
	}
	if len(pgList) != 1 {
		return nil, fmt.Errorf("found multiple port groups with the same name %s", name)
	}

	return &pgList[0], nil
}

func (c Client) CreatePortGroup(name string, externalIDs map[string]string) error {
	pg, err := c.GetPortGroup(name, true)
	if err != nil {
		return err
	}
	if pg != nil {
		return nil
	}

	pg = &ovnnb.PortGroup{
		Name:        name,
		ExternalIDs: externalIDs,
	}
	ops, err := c.nbClient.Create(pg)
	if err != nil {
		return fmt.Errorf("failed to generate create operations for port group %s: %v", name, err)
	}
	if err = Transact(c.nbClient, "pg-add", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to create port group %s: %v", name, err)
	}

	return nil
}

func (c Client) DeletePortGroup(name string) error {
	pg, err := c.GetPortGroup(name, true)
	if err != nil {
		return err
	}
	if pg == nil {
		return nil
	}

	ops, err := c.nbClient.Where(pg).Delete()
	if err != nil {
		return fmt.Errorf("failed to generate delete operations for port group %s: %v", name, err)
	}
	if err = Transact(c.nbClient, "pg-del", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to delete port group %s: %v", name, err)
	}

	return nil
}

func (c Client) GetPortGroupPorts(name string) ([]string, error) {
	pg, err := c.GetPortGroup(name, false)
	if err != nil {
		return nil, err
	}

	result := make([]string, 0, len(pg.Ports))
	for _, port := range pg.Ports {
		lsp := &ovnnb.LogicalSwitchPort{UUID: port}
		if err := c.nbClient.Get(context.TODO(), lsp); err != nil {
			return nil, fmt.Errorf("failed to get logical switch port with UUID %s: %v", port, err)
		}
		result = append(result, lsp.Name)
	}
	return result, nil
}

func (c Client) UpdatePortGroupPorts(name string, ports []string) error {
	pg, err := c.GetPortGroup(name, false)
	if err != nil {
		return err
	}

	portUUIDs := make([]string, 0, len(ports))
	for _, port := range ports {
		lsp := &ovnnb.LogicalSwitchPort{Name: port}
		if err := c.nbClient.Get(context.TODO(), lsp); err != nil {
			return fmt.Errorf("failed to update ports of port group %s: failed to get logical switch port %s: %v", name, port, err)
		}
		portUUIDs = append(portUUIDs, lsp.UUID)
	}

	sort.Strings(portUUIDs)
	sort.Strings(pg.Ports)
	if reflect.DeepEqual(portUUIDs, pg.Ports) {
		return nil
	}

	pg.Ports = portUUIDs
	ops, err := c.nbClient.Where(pg).Update(pg, &pg.Ports)
	if err != nil {
		return fmt.Errorf("failed to generate update operations for port group %s: %v", name, err)
	}
	if err = Transact(c.nbClient, "update", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to update ports of port group %s: %v", name, err)
	}

	return nil
}

func (c Client) CreateNpPortGroup(name, npNs, npName string) error {
	return c.CreatePortGroup(name, map[string]string{"np": fmt.Sprintf("%s/%s", npNs, npName)})
}

type npPortGroup struct {
	Name        string
	NpName      string
	NpNamespace string
}

func (c Client) ListNpPortGroup() ([]npPortGroup, error) {
	pgList := make([]ovnnb.PortGroup, 0, 1)
	if err := c.nbClient.WhereCache(func(pg *ovnnb.PortGroup) bool {
		return len(pg.ExternalIDs) != 0 && len(strings.Split(pg.ExternalIDs["np"], "/")) == 2
	}).List(context.TODO(), &pgList); err != nil {
		return nil, fmt.Errorf("failed to list port group: %v", err)
	}

	result := make([]npPortGroup, 0, len(pgList))
	for _, pg := range pgList {
		np := strings.Split(pg.ExternalIDs["np"], "/")
		result = append(result, npPortGroup{Name: pg.Name, NpNamespace: np[0], NpName: np[1]})
	}
	return result, nil
}

func SgPortGroupName(sgName string) string {
	return strings.ReplaceAll(fmt.Sprintf("ovn.sg.%s", sgName), "-", ".")
}

func SgV4AssociatedName(sgName string) string {
	return strings.ReplaceAll(fmt.Sprintf("ovn.sg.%s.associated.v4", sgName), "-", ".")
}

func SgV6AssociatedName(sgName string) string {
	return strings.ReplaceAll(fmt.Sprintf("ovn.sg.%s.associated.v6", sgName), "-", ".")
}

func (c Client) CreateSgPortGroup(sgName string) error {
	pgName := SgPortGroupName(sgName)
	return c.CreatePortGroup(pgName, map[string]string{"type": "security_group", "sg": sgName, "name": pgName})
}

func (c Client) DeleteSgPortGroup(sgName string) error {
	sgPortGroupName := SgPortGroupName(sgName)
	// delete acl
	if err := c.DeleteACL(sgPortGroupName, ""); err != nil {
		return err
	}

	// delete address_set
	asList, err := c.ListSgRuleAddressSet(sgName, "")
	if err != nil {
		return err
	}
	for _, as := range asList {
		if err = c.DeleteAddressSet(as); err != nil {
			return err
		}
	}

	// delete port_group
	err = c.DeletePortGroup(sgPortGroupName)
	if err != nil {
		return err
	}
	return nil
}
