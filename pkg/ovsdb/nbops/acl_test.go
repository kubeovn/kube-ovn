package nbops

import (
	"testing"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func filterRows[T any](candidates []T) func(any, any) error {
	return func(predicate, result any) error {
		rows := result.(*[]T)
		filter := predicate.(func(*T) bool)
		for i := range candidates {
			if filter(&candidates[i]) {
				*rows = append(*rows, candidates[i])
			}
		}
		return nil
	}
}

func newACLFacade(switchRows []ovnnb.LogicalSwitch, pgRows []ovnnb.PortGroup) (*ACLs, *recordingTable, *recordingTable, *recordingExecutor) {
	aclTable := &recordingTable{get: func(row model.Model) error {
		row.(*ovnnb.ACL).UUID = "acl-uuid"
		return nil
	}}
	switchTable := &recordingTable{filter: filterRows(switchRows)}
	pgTable := &recordingTable{filter: filterRows(pgRows)}
	executor := &recordingExecutor{}
	return &ACLs{acl: aclTable, switches: switchTable, portGroups: pgTable, executor: executor}, switchTable, pgTable, executor
}

func TestEnsureACLParentDetachesStaleSwitchBeforePortGroup(t *testing.T) {
	facade, switchTable, pgTable, executor := newACLFacade(
		[]ovnnb.LogicalSwitch{{Name: "old", ACLs: []string{"acl-uuid"}}},
		[]ovnnb.PortGroup{{Name: "target"}},
	)

	require.NoError(t, facade.EnsureParent(t.Context(), "target", "pg", "acl-uuid"))
	require.Len(t, executor.plans, 1)
	require.Len(t, executor.plans[0].Operations(), 2)
	require.Equal(t, "acl-parent", executor.plans[0].Method())
	require.Len(t, switchTable.mutations, 1)
	require.Equal(t, ovsdb.MutateOperationDelete, switchTable.mutations[0][0].Mutator)
	require.Len(t, pgTable.mutations, 1)
	require.Equal(t, ovsdb.MutateOperationInsert, pgTable.mutations[0][0].Mutator)
}

func TestEnsureACLParentDetachesStalePortGroupBeforeSwitch(t *testing.T) {
	facade, switchTable, pgTable, executor := newACLFacade(
		[]ovnnb.LogicalSwitch{{Name: "target"}},
		[]ovnnb.PortGroup{{Name: "old", ACLs: []string{"acl-uuid"}}},
	)

	require.NoError(t, facade.EnsureParent(t.Context(), "target", "ls", "acl-uuid"))
	require.Len(t, executor.plans, 1)
	require.Len(t, executor.plans[0].Operations(), 2)
	require.Equal(t, "acl-parent", executor.plans[0].Method())
	require.Len(t, pgTable.mutations, 1)
	require.Equal(t, ovsdb.MutateOperationDelete, pgTable.mutations[0][0].Mutator)
	require.Len(t, switchTable.mutations, 1)
	require.Equal(t, ovsdb.MutateOperationInsert, switchTable.mutations[0][0].Mutator)
}

func TestEnsureACLParentAlreadyOnTargetIsNoop(t *testing.T) {
	facade, switchTable, pgTable, executor := newACLFacade(
		nil,
		[]ovnnb.PortGroup{{Name: "target", ACLs: []string{"acl-uuid"}}},
	)

	require.NoError(t, facade.EnsureParent(t.Context(), "target", "pg", "acl-uuid"))
	require.Len(t, executor.plans, 1)
	require.Empty(t, executor.plans[0].Operations())
	require.Equal(t, "acl-parent", executor.plans[0].Method())
	require.Empty(t, switchTable.mutations)
	require.Empty(t, pgTable.mutations)
}
