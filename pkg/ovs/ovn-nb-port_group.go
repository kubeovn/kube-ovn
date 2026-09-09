package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/scylladb/go-set/strset"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c *OVNNbClient) CreatePortGroup(pgName string, externalIDs map[string]string) error {
	pg, err := c.GetPortGroup(pgName, true)
	if err != nil {
		return logErr(err)
	}

	finalExternalIDs := clonedVendorIDs(externalIDs)

	if pg != nil {
		if !maps.Equal(pg.ExternalIDs, finalExternalIDs) {
			pg.ExternalIDs = maps.Clone(finalExternalIDs)
			if err = c.UpdatePortGroup(pg, &pg.ExternalIDs); err != nil {
				err = fmt.Errorf("failed to update port group %s external IDs: %w", pgName, err)
				return logErr(err)
			}
		}
		return nil
	}

	pg = &ovnnb.PortGroup{
		Name:        pgName,
		ExternalIDs: finalExternalIDs,
	}

	if err = c.Database.Table(&ovnnb.PortGroup{}).Create(context.Background(), "pg-add", pg); err != nil {
		return logWrap(err, wrapErr("create port group %s: %w", pgName))
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
		return logWrap(err, wrapErr("get port group %s: %w", pgName))
	}

	expected := strset.NewWithSize(len(ports))
	for _, port := range ports {
		lsp, err := c.GetLogicalSwitchPort(port, true)
		if err != nil {
			return logErr(err)
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
		return logWrap(err, wrapErr("failed generate operations for adding ports %v to port group %s: %w", toAdd, pgName))
	}
	deleteOps, err := c.portGroupUpdatePortOp(pgName, toDel, ovsdb.MutateOperationDelete)
	if err != nil {
		return logWrap(err, wrapErr("failed generate operations for deleting ports %v from port group %s: %w", toDel, pgName))
	}

	return c.transactGenerated("pg-ports-update", append(insertOps, deleteOps...), nil, nil,
		wrapErr("port group %s set ports %v: %w", pgName, ports),
	)
}

// UpdatePortGroup update port group
func (c *OVNNbClient) UpdatePortGroup(pg *ovnnb.PortGroup, fields ...any) error {
	return c.updateModelLogged("pg-update", pg, func(err error) error {
		return fmt.Errorf("update port group %s: %w", pg.Name, err)
	}, fields...)
}

// PortGroupUpdatePorts add several ports to or from port group once
func (c *OVNNbClient) PortGroupUpdatePorts(pgName string, op ovsdb.Mutator, lspNames ...string) error {
	if len(lspNames) == 0 {
		return nil
	}

	lspUUIDs, err := c.logicalSwitchPortUUIDs(lspNames...)
	if err != nil {
		return err
	}

	ops, err := c.portGroupUpdatePortOp(pgName, lspUUIDs, op)
	return c.transactGenerated("pg-ports-update", ops, err,
		wrapErr("generate operations for port group %s update ports %v: %w", pgName, lspNames),
		wrapErr("port group %s update ports %v: %w", pgName, lspNames),
	)
}

func (c *OVNNbClient) DeletePortGroup(pgName ...string) error {
	return deleteNamedRows(c, pgName, c.GetPortGroup, &ovnnb.PortGroup{}, "pg-del",
		"get port group %s when delete: %w", "delete port group %s: %w")
}

// GetPortGroup get port group by name
func (c *OVNNbClient) GetPortGroup(pgName string, ignoreNotFound bool) (*ovnnb.PortGroup, error) {
	if pgName == "" {
		return nil, errors.New("port group name is empty")
	}
	return getIndexedWrap(c.Database, &ovnnb.PortGroup{Name: pgName}, ignoreNotFound, func(err error) error {
		return fmt.Errorf("get port group %s: %w", pgName, err)
	})
}

// ListPortGroups list port groups which match the given externalIDs,
// result should include all port groups when externalIDs is empty,
// result should include all port groups which externalIDs[key] is not empty when externalIDs[key] is ""
func (c *OVNNbClient) ListPortGroups(externalIDs map[string]string) ([]ovnnb.PortGroup, error) {
	pgs, err := filterTimeout(c.Database, &ovnnb.PortGroup{}, func(pg *ovnnb.PortGroup) bool {
		return matchExternalIDs(pg.ExternalIDs, externalIDs)
	})
	if err != nil {
		klog.Errorf("list logical switch ports: %v", err)
		return nil, err
	}
	return pgs, nil
}

func (c *OVNNbClient) PortGroupExists(pgName string) (bool, error) {
	return existsByGet(c.GetPortGroup, pgName)
}

// portGroupUpdatePortOp create operations add port to or delete port from port group
func (c *OVNNbClient) portGroupUpdatePortOp(pgName string, lspUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	return c.portGroupTable().mutateUUIDs(pgName, func(pg *ovnnb.PortGroup) *[]string { return &pg.Ports }, lspUUIDs, op)
}

// portGroupUpdateACLOp create operations add acl to or delete acl from port group
func (c *OVNNbClient) portGroupUpdateACLOp(pgName string, aclUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	return c.portGroupTable().mutateUUIDs(pgName, func(pg *ovnnb.PortGroup) *[]string { return &pg.ACLs }, aclUUIDs, op)
}

// portGroupOp create operations about port group
func (c *OVNNbClient) portGroupOp(pgName string, mutationsFunc ...func(pg *ovnnb.PortGroup) *model.Mutation) ([]ovsdb.Operation, error) {
	return c.portGroupTable().mutateNamed(pgName, mutationsFunc...)
}

func (c *OVNNbClient) portGroupTable() namedTable[ovnnb.PortGroup] {
	table := newNamedTable(c.Database, &ovnnb.PortGroup{}, "port group",
		func(pg *ovnnb.PortGroup) string { return pg.Name },
		func(pg *ovnnb.PortGroup) map[string]string { return pg.ExternalIDs })
	table.newByName = func(name string) *ovnnb.PortGroup { return &ovnnb.PortGroup{Name: name} }
	return table
}

func (c *OVNNbClient) RemovePortFromPortGroups(portName string, portGroupNames ...string) error {
	lsp, err := c.GetLogicalSwitchPort(portName, true)
	if err != nil {
		return logWrap(err, wrapErr("failed to get logical switch port %s: %w", portName))
	}
	if lsp == nil {
		return nil
	}

	portGroups := make([]ovnnb.PortGroup, 0, len(portGroupNames))
	if len(portGroupNames) != 0 {
		for _, pgName := range portGroupNames {
			pg, err := c.GetPortGroup(pgName, true)
			if err != nil {
				return logWrap(err, wrapErr("failed to get port group %s: %w", pgName))
			}
			if pg != nil {
				portGroups = append(portGroups, *pg)
			}
		}
	} else if portGroups, err = c.ListPortGroups(nil); err != nil {
		return logWrap(err, wrapErr("failed to list port groups: %w"))
	}

	var ops []ovsdb.Operation
	for _, pg := range portGroups {
		if !slices.Contains(pg.Ports, lsp.UUID) {
			continue
		}

		op, err := c.portGroupUpdatePortOp(pg.Name, []string{lsp.UUID}, ovsdb.MutateOperationDelete)
		if err != nil {
			return logWrap(err, wrapErr("failed to generate operations for removing port %s from port group %s: %w", portName, pg.Name))
		}
		ops = append(ops, op...)
	}

	return c.transactGenerated("pg-update", ops, nil, nil,
		wrapErr("failed to remove port %s from all port groups: %w", portName),
	)
}
