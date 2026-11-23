package ovs

import (
	"context"
	"fmt"
	"slices"

	"github.com/ovn-kubernetes/libovsdb/mapper"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
)

// PortGroupMembershipChange describes a delta against an authoritative port group snapshot.
// An empty delta can be included to guard the membership of an otherwise unchanged group.
type PortGroupMembershipChange struct {
	Group  ovnnb.PortGroup
	Add    []string
	Remove []string
}

// GetSubnetPortGroupMembership reads port groups and ports in one transaction on the
// NB leader so a stale monitor cache cannot turn an incomplete reconcile into a no-op.
func (c *OVNNbClient) GetSubnetPortGroupMembership(ctx context.Context, subnet string) ([]ovnnb.PortGroup, []ovnnb.LogicalSwitchPort, error) {
	ops := []ovsdb.Operation{
		selectSubnetMembership(ovnnb.PortGroupTable, "subnet", subnet, "_uuid", "name", "external_ids", "ports"),
		selectSubnetMembership(ovnnb.LogicalSwitchPortTable, LogicalSwitchKey, subnet, "_uuid", "name", "external_ids", "type"),
	}
	res, err := c.TransactResults(ctx, ops...)
	if err != nil {
		return nil, nil, fmt.Errorf("read port group membership for subnet %q: %w", subnet, err)
	}

	groups, err := decodeMembershipRows[ovnnb.PortGroup](c.Schema(), ovnnb.PortGroupTable, res[0].Rows)
	if err != nil {
		return nil, nil, fmt.Errorf("decode port groups for subnet %q: %w", subnet, err)
	}
	ports, err := decodeMembershipRows[ovnnb.LogicalSwitchPort](c.Schema(), ovnnb.LogicalSwitchPortTable, res[1].Rows)
	if err != nil {
		return nil, nil, fmt.Errorf("decode logical switch ports for subnet %q: %w", subnet, err)
	}
	return groups, ports, nil
}

func decodeMembershipRows[T any](schema ovsdb.DatabaseSchema, tableName string, rows []ovsdb.Row) ([]T, error) {
	tableSchema := schema.Table(tableName)
	if tableSchema == nil {
		return nil, fmt.Errorf("OVSDB table %s is missing", tableName)
	}
	ovsMapper := mapper.NewMapper(schema)
	result := make([]T, len(rows))
	for i := range rows {
		info, err := mapper.NewInfo(tableName, tableSchema, &result[i])
		if err != nil {
			return nil, err
		}
		if err := ovsMapper.GetRowDataWithUUID(&rows[i], info); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func selectSubnetMembership(table, key, subnet string, columns ...string) ovsdb.Operation {
	op := ovsdb.Operation{
		Op:      ovsdb.OperationSelect,
		Table:   table,
		Columns: columns,
		Where: []ovsdb.Condition{{
			Column:   "external_ids",
			Function: ovsdb.ConditionIncludes,
			Value:    ovsdb.OvsMap{GoMap: map[any]any{key: subnet}},
		}},
	}
	return op
}

// ApplyPortGroupMembershipChanges checks the snapshots and applies all mutations
// in one transaction. A stale snapshot or a missing LSP aborts the whole batch.
// Additional LSPs can be supplied when a mutation relies on an existing membership.
func (c *OVNNbClient) ApplyPortGroupMembershipChanges(ctx context.Context, changes []PortGroupMembershipChange, lspUUIDs []string) error {
	var guards, mutations []ovsdb.Operation
	lspRefs := make(map[string]struct{}, len(lspUUIDs))
	for _, id := range lspUUIDs {
		lspRefs[id] = struct{}{}
	}
	for _, change := range changes {
		pg := change.Group
		if pg.UUID == "" {
			return fmt.Errorf("port group %q is missing its UUID", pg.Name)
		}
		externalIDs := make(map[any]any, len(pg.ExternalIDs))
		for key, value := range pg.ExternalIDs {
			externalIDs[key] = value
		}
		where := rowUUIDCondition(pg.UUID)
		timeout := OVSDBWaitTimeout
		guards = append(guards, ovsdb.Operation{
			Op:      ovsdb.OperationWait,
			Table:   ovnnb.PortGroupTable,
			Timeout: &timeout,
			Where:   where,
			Columns: []string{"external_ids", "ports"},
			Until:   string(ovsdb.WaitConditionEqual),
			Rows:    []ovsdb.Row{{"external_ids": ovsdb.OvsMap{GoMap: externalIDs}, "ports": uuidSet(pg.Ports)}},
		})

		var portMutations []ovsdb.Mutation
		if len(change.Remove) != 0 {
			portMutations = append(portMutations, ovsdb.Mutation{Column: "ports", Mutator: ovsdb.MutateOperationDelete, Value: uuidSet(change.Remove)})
		}
		if len(change.Add) != 0 {
			for _, id := range change.Add {
				lspRefs[id] = struct{}{}
			}
			portMutations = append(portMutations, ovsdb.Mutation{Column: "ports", Mutator: ovsdb.MutateOperationInsert, Value: uuidSet(change.Add)})
		}
		if len(portMutations) != 0 {
			mutations = append(mutations, ovsdb.Operation{
				Op:        ovsdb.OperationMutate,
				Table:     ovnnb.PortGroupTable,
				Where:     where,
				Mutations: portMutations,
			})
		}
	}
	if len(mutations) == 0 {
		return nil
	}

	for id := range lspRefs {
		timeout := OVSDBWaitTimeout
		guards = append(guards, ovsdb.Operation{
			Op:      ovsdb.OperationWait,
			Table:   ovnnb.LogicalSwitchPortTable,
			Timeout: &timeout,
			Where:   rowUUIDCondition(id),
			Columns: []string{"_uuid"},
			Until:   string(ovsdb.WaitConditionEqual),
			Rows:    []ovsdb.Row{{"_uuid": ovsdb.UUID{GoUUID: id}}},
		})
	}
	plan := table.NewTxPlan("pg-ports-update")
	plan.Add(append(guards, mutations...)...)
	if err := c.Execute(ctx, plan); err != nil {
		return fmt.Errorf("update port group membership: %w", err)
	}
	return nil
}

func rowUUIDCondition(id string) []ovsdb.Condition {
	return []ovsdb.Condition{{Column: "_uuid", Function: ovsdb.ConditionEqual, Value: ovsdb.UUID{GoUUID: id}}}
}

func uuidSet(ids []string) ovsdb.OvsSet {
	set := make([]any, 0, len(ids))
	for _, id := range ids {
		set = append(set, ovsdb.UUID{GoUUID: id})
	}
	return ovsdb.OvsSet{GoSet: set}
}

// MovePortToPortGroup moves an existing LSP between groups in the supplied scope.
// Reads bypass the monitor cache, and removal and insertion share a guarded
// transaction. absentExternalIDs requires the specified keys to be missing or
// empty. Callers serialize competing moves of the same port.
func (c *OVNNbClient) MovePortToPortGroup(ctx context.Context, portName, targetGroup string, externalIDs map[string]string, absentExternalIDs ...string) error {
	portUUID, groups, err := c.getPortGroupMoveSnapshot(ctx, portName, targetGroup, externalIDs, absentExternalIDs...)
	if err != nil {
		return err
	}
	changes := make([]PortGroupMembershipChange, 0, len(groups))
	foundTarget := false
	for _, group := range groups {
		matchesScope := true
		for key, value := range externalIDs {
			if group.ExternalIDs[key] == "" || (value != "" && group.ExternalIDs[key] != value) {
				matchesScope = false
				break
			}
		}
		if !matchesScope {
			continue
		}
		change := PortGroupMembershipChange{Group: group}
		if group.Name == targetGroup {
			foundTarget = true
			if !slices.Contains(group.Ports, portUUID) {
				change.Add = []string{portUUID}
			}
		} else {
			change.Remove = []string{portUUID}
		}
		changes = append(changes, change)
	}
	if !foundTarget {
		return fmt.Errorf("target port group %q does not exist in the requested scope", targetGroup)
	}
	return c.ApplyPortGroupMembershipChanges(ctx, changes, []string{portUUID})
}

func (c *OVNNbClient) getPortGroupMoveSnapshot(ctx context.Context, portName, targetGroup string, externalIDs map[string]string, absentExternalIDs ...string) (string, []ovnnb.PortGroup, error) {
	ops := []ovsdb.Operation{{
		Op:      ovsdb.OperationSelect,
		Table:   ovnnb.LogicalSwitchPortTable,
		Columns: []string{"_uuid"},
		Where:   []ovsdb.Condition{{Column: "name", Function: ovsdb.ConditionEqual, Value: portName}},
	}}
	result, err := c.TransactResults(ctx, ops...)
	if err != nil {
		return "", nil, err
	}
	ports, err := decodeMembershipRows[ovnnb.LogicalSwitchPort](c.Schema(), ovnnb.LogicalSwitchPortTable, result[0].Rows)
	if err != nil {
		return "", nil, err
	}
	if len(ports) != 1 {
		return "", nil, fmt.Errorf("logical switch port %q does not exist", portName)
	}
	portUUID := ports[0].UUID
	scope := make(map[any]any, len(externalIDs))
	for key, value := range externalIDs {
		if value != "" {
			scope[key] = value
		}
	}
	ops = []ovsdb.Operation{
		{
			Op:      ovsdb.OperationSelect,
			Table:   ovnnb.PortGroupTable,
			Columns: []string{"_uuid", "name", "external_ids", "ports"},
			Where: []ovsdb.Condition{
				{Column: "ports", Function: ovsdb.ConditionIncludes, Value: uuidSet([]string{portUUID})},
				{Column: "external_ids", Function: ovsdb.ConditionIncludes, Value: ovsdb.OvsMap{GoMap: scope}},
			},
		},
		{
			Op:      ovsdb.OperationSelect,
			Table:   ovnnb.PortGroupTable,
			Columns: []string{"_uuid", "name", "external_ids", "ports"},
			Where:   []ovsdb.Condition{{Column: "name", Function: ovsdb.ConditionEqual, Value: targetGroup}},
		},
	}
	result, err = c.TransactResults(ctx, ops...)
	if err != nil {
		return "", nil, err
	}
	rows, err := decodeMembershipRows[ovnnb.PortGroup](c.Schema(), ovnnb.PortGroupTable, append(result[0].Rows, result[1].Rows...))
	if err != nil {
		return "", nil, err
	}
	groups := make([]ovnnb.PortGroup, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, group := range rows {
		if _, exists := seen[group.UUID]; exists {
			continue
		}
		seen[group.UUID] = struct{}{}
		if slices.ContainsFunc(absentExternalIDs, func(key string) bool {
			return group.ExternalIDs[key] != ""
		}) {
			continue
		}
		groups = append(groups, group)
	}
	return portUUID, groups, nil
}
