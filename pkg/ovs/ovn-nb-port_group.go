package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c *ovnClient) CreatePortGroup(pgName string, externalIDs map[string]string) error {
	exist, err := c.PortGroupExists(pgName)
	if err != nil {
		return err
	}

	// ingnore
	if exist {
		return nil
	}

	pg := &ovnnb.PortGroup{
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

// PortGroupAddPorts add ports to port group
func (c *ovnClient) PortGroupAddPorts(pgName string, lspNames ...string) error {
	return c.PortGroupUpdatePorts(pgName, ovsdb.MutateOperationInsert, lspNames...)
}

// PortGroupRemovePorts remove ports from port group
func (c *ovnClient) PortGroupRemovePorts(pgName string, lspNames ...string) error {
	return c.PortGroupUpdatePorts(pgName, ovsdb.MutateOperationDelete, lspNames...)
}

func (c *ovnClient) PortGroupSetPorts(pgName string, ports []string) error {
	pg, err := c.GetPortGroup(pgName, false)
	if err != nil {
		return fmt.Errorf("get port group %s: %v", pgName, err)
	}

	uuids := make(map[string]struct{}, len(ports))
	for _, port := range ports {
		lsp, err := c.GetLogicalSwitchPort(port, true)
		if err != nil {
			return err
		}
		if lsp != nil {
			uuids[lsp.UUID] = struct{}{}
		}
	}

	var toDel []string
	for _, uuid := range pg.Ports {
		if _, ok := uuids[uuid]; ok {
			delete(uuids, uuid)
			continue
		}
		toDel = append(toDel, uuid)
	}

	toAdd := make([]string, 0, len(uuids))
	for uuid := range uuids {
		toAdd = append(toAdd, uuid)
	}

	insertOps, err := c.portGroupUpdatePortOp(pgName, toAdd, ovsdb.MutateOperationInsert)
	if err != nil {
		return fmt.Errorf("failed generate operations for adding ports %v to port group %s: %v", toAdd, pgName, err)
	}
	deleteOps, err := c.portGroupUpdatePortOp(pgName, toDel, ovsdb.MutateOperationDelete)
	if err != nil {
		return fmt.Errorf("failed generate operations for deleting ports %v from port group %s: %v", toDel, pgName, err)
	}

	if err = c.Transact("pg-ports-update", append(insertOps, deleteOps...)); err != nil {
		return fmt.Errorf("port group %s set ports %v: %v", pgName, ports, err)
	}

	return nil
}

// UpdatePortGroup update port group
func (c *ovnClient) UpdatePortGroup(pg *ovnnb.PortGroup, fields ...interface{}) error {
	op, err := c.Where(pg).Update(pg, fields...)
	if err != nil {
		return fmt.Errorf("generate operations for updating port group %s: %v", pg.Name, err)
	}

	if err = c.Transact("pg-update", op); err != nil {
		return fmt.Errorf("update port group %s: %v", pg.Name, err)
	}

	return nil
}

// PortGroupUpdatePorts add several ports to or from port group once
func (c *ovnClient) PortGroupUpdatePorts(pgName string, op ovsdb.Mutator, lspNames ...string) error {
	if len(lspNames) == 0 {
		return nil
	}

	lspUUIDs := make([]string, 0, len(lspNames))

	for _, lspName := range lspNames {
		lsp, err := c.GetLogicalSwitchPort(lspName, true)
		if err != nil {
			return err
		}

		// ignore non-existent object
		if lsp != nil {
			lspUUIDs = append(lspUUIDs, lsp.UUID)
		}
	}

	ops, err := c.portGroupUpdatePortOp(pgName, lspUUIDs, op)
	if err != nil {
		return fmt.Errorf("generate operations for port group %s update ports %v: %v", pgName, lspNames, err)
	}

	if err := c.Transact("pg-ports-update", ops); err != nil {
		return fmt.Errorf("port group %s update ports %v: %v", pgName, lspNames, err)
	}

	return nil
}

func (c *ovnClient) DeletePortGroup(pgName string) error {
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
func (c *ovnClient) GetPortGroup(pgName string, ignoreNotFound bool) (*ovnnb.PortGroup, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	pg := &ovnnb.PortGroup{Name: pgName}
	if err := c.ovnNbClient.Get(ctx, pg); err != nil {
		if ignoreNotFound && err == client.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get port group %s: %v", pgName, err)
	}

	return pg, nil
}

// ListPortGroups list port groups which match the given externalIDs,
// result should include all port groups when externalIDs is empty,
// result should include all port groups which externalIDs[key] is not empty when externalIDs[key] is ""
func (c *ovnClient) ListPortGroups(externalIDs map[string]string) ([]ovnnb.PortGroup, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

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
	}).List(ctx, &pgs); err != nil {
		klog.Errorf("list logical switch ports: %v", err)
		return nil, err
	}

	return pgs, nil
}

func (c *ovnClient) PortGroupExists(pgName string) (bool, error) {
	lsp, err := c.GetPortGroup(pgName, true)
	return lsp != nil, err
}

// portGroupUpdatePortOp create operations add port to or delete port from port group
func (c *ovnClient) portGroupUpdatePortOp(pgName string, lspUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
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

// portGroupUpdateAclOp create operations add acl to or delete acl from port group
func (c *ovnClient) portGroupUpdateAclOp(pgName string, aclUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	if len(aclUUIDs) == 0 {
		return nil, nil
	}

	mutation := func(pg *ovnnb.PortGroup) *model.Mutation {
		mutation := &model.Mutation{
			Field:   &pg.ACLs,
			Value:   aclUUIDs,
			Mutator: op,
		}

		return mutation
	}

	return c.portGroupOp(pgName, mutation)
}

// portGroupOp create operations about port group
func (c *ovnClient) portGroupOp(pgName string, mutationsFunc ...func(pg *ovnnb.PortGroup) *model.Mutation) ([]ovsdb.Operation, error) {
	pg, err := c.GetPortGroup(pgName, false)
	if err != nil {
		return nil, fmt.Errorf("get port group %s: %v", pgName, err)
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
