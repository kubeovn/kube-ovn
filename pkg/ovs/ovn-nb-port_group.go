package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/scylladb/go-set/strset"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *OVNNbClient) CreatePortGroup(pgName string, externalIDs map[string]string) error {
	pg, err := c.GetPortGroup(pgName, true)
	if err != nil {
		klog.Error(err)
		return err
	}

	// Create new map with vendor tag to avoid modifying caller's map
	finalExternalIDs := make(map[string]string, len(externalIDs)+1)
	maps.Copy(finalExternalIDs, externalIDs)
	finalExternalIDs["vendor"] = util.CniTypeName

	if pg != nil {
		if !maps.Equal(pg.ExternalIDs, finalExternalIDs) {
			pg.ExternalIDs = maps.Clone(finalExternalIDs)
			if err = c.UpdatePortGroup(pg, &pg.ExternalIDs); err != nil {
				err = fmt.Errorf("failed to update port group %s external IDs: %w", pgName, err)
				klog.Error(err)
				return err
			}
		}
		return nil
	}

	pg = &ovnnb.PortGroup{
		Name:        pgName,
		ExternalIDs: finalExternalIDs,
	}

	ops, err := c.Create(pg)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for creating port group %s: %w", pgName, err)
	}

	if err = c.Transact("pg-add", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("create port group %s: %w", pgName, err)
	}

	return nil
}

// PortGroupAddPorts add ports to port group
func (c *OVNNbClient) PortGroupAddPorts(pgName string, lspNames ...string) error {
	return c.PortGroupUpdatePorts(pgName, ovsdb.MutateOperationInsert, lspNames...)
}

// PortGroupRemovePorts remove ports from port group
func (c *OVNNbClient) PortGroupRemovePorts(pgName string, lspNames ...string) error {
	return c.PortGroupUpdatePorts(pgName, ovsdb.MutateOperationDelete, lspNames...)
}

func (c *OVNNbClient) PortGroupSetPorts(pgName string, ports []string) error {
	if pgName == "" {
		return errors.New("port group name is empty")
	}

	pg, err := c.GetPortGroup(pgName, false)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("get port group %s: %w", pgName, err)
	}

	expected := strset.NewWithSize(len(ports))
	for _, port := range ports {
		lsp, err := c.GetLogicalSwitchPort(port, true)
		if err != nil {
			klog.Error(err)
			return err
		}
		if lsp != nil {
			expected.Add(lsp.UUID)
		}
	}

	existing := strset.New(pg.Ports...)
	toAdd := strset.Difference(expected, existing).List()
	toDel := strset.Difference(existing, expected).List()

	insertOps, err := c.portGroupUpdatePortOp(pgName, toAdd, ovsdb.MutateOperationInsert)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed generate operations for adding ports %v to port group %s: %w", toAdd, pgName, err)
	}
	deleteOps, err := c.portGroupUpdatePortOp(pgName, toDel, ovsdb.MutateOperationDelete)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed generate operations for deleting ports %v from port group %s: %w", toDel, pgName, err)
	}

	if err = c.Transact("pg-ports-update", append(insertOps, deleteOps...)); err != nil {
		klog.Error(err)
		return fmt.Errorf("port group %s set ports %v: %w", pgName, ports, err)
	}

	return nil
}

// UpdatePortGroup update port group
func (c *OVNNbClient) UpdatePortGroup(pg *ovnnb.PortGroup, fields ...any) error {
	op, err := c.Where(pg).Update(pg, fields...)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for updating port group %s: %w", pg.Name, err)
	}

	if err = c.Transact("pg-update", op); err != nil {
		klog.Error(err)
		return fmt.Errorf("update port group %s: %w", pg.Name, err)
	}

	return nil
}

// PortGroupUpdatePorts add several ports to or from port group once
func (c *OVNNbClient) PortGroupUpdatePorts(pgName string, op ovsdb.Mutator, lspNames ...string) error {
	if len(lspNames) == 0 {
		return nil
	}

	lspUUIDs := make([]string, 0, len(lspNames))

	for _, lspName := range lspNames {
		lsp, err := c.GetLogicalSwitchPort(lspName, true)
		if err != nil {
			klog.Error(err)
			return err
		}

		// ignore non-existent object
		if lsp != nil {
			lspUUIDs = append(lspUUIDs, lsp.UUID)
		}
	}

	ops, err := c.portGroupUpdatePortOp(pgName, lspUUIDs, op)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for port group %s update ports %v: %w", pgName, lspNames, err)
	}

	if err := c.Transact("pg-ports-update", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("port group %s update ports %v: %w", pgName, lspNames, err)
	}

	return nil
}

func (c *OVNNbClient) DeletePortGroup(pgName ...string) error {
	delList := make([]*ovnnb.PortGroup, 0, len(pgName))
	for _, name := range pgName {
		// get port group
		pg, err := c.GetPortGroup(name, true)
		if err != nil {
			return fmt.Errorf("get port group %s when delete: %w", name, err)
		}
		// not found, skip
		if pg == nil {
			continue
		}
		delList = append(delList, pg)
	}
	if len(delList) == 0 {
		return nil
	}

	modelList := make([]model.Model, len(delList))
	for i, pg := range delList {
		modelList[i] = pg
	}
	op, err := c.Where(modelList...).Delete()
	if err != nil {
		return err
	}
	if err := c.Transact("pg-del", op); err != nil {
		klog.Error(err)
		return fmt.Errorf("delete port group %s: %w", pgName, err)
	}

	return nil
}

// GetPortGroup get port group by name
func (c *OVNNbClient) GetPortGroup(pgName string, ignoreNotFound bool) (*ovnnb.PortGroup, error) {
	if pgName == "" {
		return nil, errors.New("port group name is empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	pg := &ovnnb.PortGroup{Name: pgName}
	if err := c.Get(ctx, pg); err != nil {
		if ignoreNotFound && errors.Is(err, client.ErrNotFound) {
			return nil, nil
		}
		klog.Error(err)
		return nil, fmt.Errorf("get port group %s: %w", pgName, err)
	}

	return pg, nil
}

// ListPortGroups list port groups which match the given externalIDs,
// result should include all port groups when externalIDs is empty,
// result should include all port groups which externalIDs[key] is not empty when externalIDs[key] is ""
func (c *OVNNbClient) ListPortGroups(externalIDs map[string]string) ([]ovnnb.PortGroup, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var pgs []ovnnb.PortGroup
	if err := c.WhereCache(func(pg *ovnnb.PortGroup) bool {
		if len(externalIDs) != 0 && len(pg.ExternalIDs) < len(externalIDs) {
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

func (c *OVNNbClient) PortGroupExists(pgName string) (bool, error) {
	lsp, err := c.GetPortGroup(pgName, true)
	return lsp != nil, err
}

// portGroupUpdatePortOp create operations add port to or delete port from port group
func (c *OVNNbClient) portGroupUpdatePortOp(pgName string, lspUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
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

// portGroupUpdateACLOp create operations add acl to or delete acl from port group
func (c *OVNNbClient) portGroupUpdateACLOp(pgName string, aclUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
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
func (c *OVNNbClient) portGroupOp(pgName string, mutationsFunc ...func(pg *ovnnb.PortGroup) *model.Mutation) ([]ovsdb.Operation, error) {
	pg, err := c.GetPortGroup(pgName, false)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("get port group %s: %w", pgName, err)
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

	ops, err := c.ovsDbClient.Where(pg).Mutate(pg, mutations...)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("generate operations for mutating port group %s: %w", pgName, err)
	}

	return ops, nil
}

func (c *OVNNbClient) RemovePortFromPortGroups(portName string, portGroupNames ...string) error {
	lsp, err := c.GetLogicalSwitchPort(portName, true)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to get logical switch port %s: %w", portName, err)
	}
	if lsp == nil {
		return nil
	}

	portGroups := make([]ovnnb.PortGroup, 0, len(portGroupNames))
	if len(portGroupNames) != 0 {
		for _, pgName := range portGroupNames {
			pg, err := c.GetPortGroup(pgName, true)
			if err != nil {
				klog.Error(err)
				return fmt.Errorf("failed to get port group %s: %w", pgName, err)
			}
			if pg != nil {
				portGroups = append(portGroups, *pg)
			}
		}
	} else if portGroups, err = c.ListPortGroups(nil); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to list port groups: %w", err)
	}

	var ops []ovsdb.Operation
	for _, pg := range portGroups {
		if !slices.Contains(pg.Ports, lsp.UUID) {
			continue
		}

		op, err := c.portGroupUpdatePortOp(pg.Name, []string{lsp.UUID}, ovsdb.MutateOperationDelete)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("failed to generate operations for removing port %s from port group %s: %w", portName, pg.Name, err)
		}
		ops = append(ops, op...)
	}

	if err = c.Transact("pg-update", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to remove port %s from all port groups: %w", portName, err)
	}

	return nil
}
