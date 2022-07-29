package ovs

import (
	"context"
	"fmt"
	"strings"

	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c OvnClient) CreatePortGroup(pgName string, externalIDs map[string]string) error {
	pg, err := c.GetPortGroup(pgName, true)
	if err != nil {
		return err
	}

	// found, ingore
	if pg != nil {
		return nil
	}

	pg = &ovnnb.PortGroup{
		Name:        pgName,
		ExternalIDs: externalIDs,
	}

	ops, err := c.ovnNbClient.Create(pg)
	if err != nil {
		return fmt.Errorf("generate operations for creating port group %s: %v", pgName, err)
	}

	if err = c.Transact("pg-add", ops); err != nil {
		return fmt.Errorf("create port group %s: %v", pgName, err)
	}

	return nil
}

// PortGroupUpdatePorts add several ports to or from port group once
func (c OvnClient) PortGroupUpdatePorts(pgName string, op ovsdb.Mutator, lspNames ...string) error {
	if len(lspNames) == 0 {
		return nil
	}

	lspUUIDs := make([]string, 0, len(lspNames))

	for _, lspName := range lspNames {
		lsp, err := c.GetLogicalSwitchPort(lspName, true)
		if err != nil {
			return err
		}

		// ingnore non-existent object
		if lsp != nil {
			lspUUIDs = append(lspUUIDs, lsp.UUID)
		}
	}

	ops, err := c.portGroupUpdatePortOp(pgName, lspUUIDs, op)
	if err != nil {
		return fmt.Errorf("generate operations for port group %s update ports %s: %v", pgName, strings.Join(lspNames, " "), err)
	}

	if err := c.Transact("pg-ports-update", ops); err != nil {
		return fmt.Errorf("port group %s update ports %s: %v", pgName, strings.Join(lspNames, " "), err)
	}

	return nil
}

func (c OvnClient) DeletePortGroup(pgName string) error {
	pg, err := c.GetPortGroup(pgName, true)
	if err != nil {
		return fmt.Errorf("get port group %s when delete: %v", pgName, err)
	}

	// not found, skip
	if pg == nil {
		return nil
	}

	op, err := c.Where(pg).Delete()
	if err != nil {
		return err
	}

	if err := c.Transact("pg-del", op); err != nil {
		return fmt.Errorf("delete port group %s: %v", pgName, err)
	}

	return nil
}

// GetPortGroup get port group by name
func (c OvnClient) GetPortGroup(name string, ignoreNotFound bool) (*ovnnb.PortGroup, error) {
	pg := &ovnnb.PortGroup{Name: name}
	if err := c.ovnNbClient.Get(context.TODO(), pg); err != nil {
		if ignoreNotFound && err == client.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get port group %s: %v", name, err)
	}

	return pg, nil
}

// ListPortGroups list port groups which match the given externalIDs,
// result should include all port groups when externalIDs is empty,
// result should include all port groups which externalIDs[key] is not empty when externalIDs[key] is ""
func (c OvnClient) ListPortGroups(externalIDs map[string]string) ([]ovnnb.PortGroup, error) {
	pgs := make([]ovnnb.PortGroup, 0)

	if err := c.WhereCache(func(pg *ovnnb.PortGroup) bool {
		if len(pg.ExternalIDs) < len(externalIDs) {
			return false
		}

		if len(pg.ExternalIDs) != 0 {
			for k, v := range externalIDs {
				// if only key exist but not value in externalIDs, we should include this pg,
				// it's equal to shell command `ovn-nbctl --columns=xx find port_group external_ids:key!=\"\"`
				if len(v) == 0 {
					if len(pg.ExternalIDs[k]) == 0 {
						return false
					}
				} else {
					if pg.ExternalIDs[k] != v {
						return false
					}
				}

			}
		}

		return true
	}).List(context.TODO(), &pgs); err != nil {
		klog.Errorf("list logical switch ports: %v", err)
		return nil, err
	}

	return pgs, nil
}

func (c OvnClient) PortGroupExists(name string) (bool, error) {
	lsp, err := c.GetPortGroup(name, true)
	return lsp != nil, err
}

// PortGroupALLNotExist return true if all port group does not exist
func (c OvnClient) PortGroupALLNotExist(sgs []string) (bool, error) {
	if len(sgs) == 0 {
		return true, nil
	}

	notExistsCount := 0
	// sgs format: sg1/sg2/sg3
	for _, sg := range sgs {
		ok, err := c.PortGroupExists(GetSgPortGroupName(sg))
		if err != nil {
			return true, err
		}

		if !ok {
			notExistsCount++
		}
	}

	return notExistsCount == len(sgs), nil
}

// portGroupUpdatePortOp create operations add port to or delete port from port group
func (c OvnClient) portGroupUpdatePortOp(pgName string, lspUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	if len(lspUUIDs) == 0 {
		return nil, nil
	}

	mutation := func(pg *ovnnb.PortGroup) *model.Mutation {
		mutation := &model.Mutation{
			Field:   &pg.Ports,
			Value:   lspUUIDs,
			Mutator: op,
		}

		return mutation
	}

	return c.portGroupOp(pgName, mutation)
}

// portGroupOp create operations about port group
func (c OvnClient) portGroupOp(pgName string, mutationsFunc ...func(pg *ovnnb.PortGroup) *model.Mutation) ([]ovsdb.Operation, error) {
	pg, err := c.GetPortGroup(pgName, false)
	if err != nil {
		return nil, fmt.Errorf("get port group %s when generate mutate operations: %v", pgName, err)
	}

	if len(mutationsFunc) == 0 {
		return nil, nil
	}

	mutations := make([]model.Mutation, 0, len(mutationsFunc))

	for _, f := range mutationsFunc {
		mutation := f(pg)

		if mutation != nil {
			mutations = append(mutations, *mutation)
		}
	}

	ops, err := c.ovnNbClient.Where(pg).Mutate(pg, mutations...)
	if err != nil {
		return nil, fmt.Errorf("generate operations for mutating port group %s: %v", pgName, err)
	}

	return ops, nil
}

/*
----------------------------------------------------------------------------------------------
TODO: wait to be deleted
*/
func (c OvnClient) portGroupPortOp(pgName, portName string, opIsAdd bool) error {
	pg, err := c.GetPortGroup(pgName, false)
	if err != nil {
		return err
	}

	lsp, err := c.GetLogicalSwitchPort(portName, false)
	if err != nil {
		return err
	}

	portMap := make(map[string]struct{}, len(pg.Ports))
	for _, port := range pg.Ports {
		portMap[port] = struct{}{}
	}
	if _, ok := portMap[lsp.UUID]; ok == opIsAdd {
		return nil
	}

	if opIsAdd {
		pg.Ports = append(pg.Ports, lsp.UUID)
	} else {
		delete(portMap, lsp.UUID)
		pg.Ports = make([]string, 0, len(portMap))
		for port := range portMap {
			pg.Ports = append(pg.Ports, port)
		}
	}

	ops, err := c.ovnNbClient.Where(pg).Update(pg, &pg.Ports)
	if err != nil {
		return fmt.Errorf("failed to generate update operations for port group %s: %v", pgName, err)
	}
	if err = c.Transact("update", ops); err != nil {
		return fmt.Errorf("failed to update ports of port group %s: %v", pgName, err)
	}

	return nil
}

func (c OvnClient) PortGroupAddPort(pgName, portName string) error {
	return c.portGroupPortOp(pgName, portName, true)
}

func (c OvnClient) PortGroupRemovePort(pgName, portName string) error {
	return c.portGroupPortOp(pgName, portName, false)
}
