package nbops

import (
	"context"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

type recordingTable struct {
	get       func(model.Model) error
	filter    func(any, any) error
	mutations [][]model.Mutation
}

func (t *recordingTable) Get(_ context.Context, row model.Model) error {
	return t.get(row)
}

func (t *recordingTable) Filter(_ context.Context, predicate, result any) error {
	return t.filter(predicate, result)
}

func (t *recordingTable) MutateOps(_ model.Model, mutations ...model.Mutation) ([]ovsdb.Operation, error) {
	t.mutations = append(t.mutations, mutations)
	return []ovsdb.Operation{{Op: ovsdb.OperationMutate}}, nil
}

type recordingExecutor struct {
	plans []*compat.TxPlan
}

func (e *recordingExecutor) Execute(_ context.Context, plan *compat.TxPlan) error {
	e.plans = append(e.plans, plan)
	return nil
}

func TestEnsureParentDetachesStaleParentsBeforeAttach(t *testing.T) {
	portTable := &recordingTable{get: func(row model.Model) error {
		row.(*ovnnb.LogicalSwitchPort).UUID = "lsp-uuid"
		return nil
	}}
	switchTable := &recordingTable{get: func(model.Model) error { return nil }}
	switchTable.filter = func(predicate, result any) error {
		rows := result.(*[]ovnnb.LogicalSwitch)
		candidates := []ovnnb.LogicalSwitch{
			{Name: "old", Ports: []string{"lsp-uuid"}},
			{Name: "target"},
		}
		filter := predicate.(func(*ovnnb.LogicalSwitch) bool)
		for i := range candidates {
			if filter(&candidates[i]) {
				*rows = append(*rows, candidates[i])
			}
		}
		return nil
	}
	executor := &recordingExecutor{}
	facade := &LogicalSwitchPorts{ports: portTable, switches: switchTable, executor: executor}

	require.NoError(t, facade.EnsureParent(t.Context(), "port", "target"))
	require.Len(t, executor.plans, 1)
	require.Len(t, executor.plans[0].Operations(), 2)
	require.Equal(t, "lsp-parent", executor.plans[0].Method())
	require.Len(t, switchTable.mutations, 2)
	require.Equal(t, ovsdb.MutateOperationDelete, switchTable.mutations[0][0].Mutator)
	require.Equal(t, ovsdb.MutateOperationInsert, switchTable.mutations[1][0].Mutator)
}

func TestEnsureRouterParentDetachesStaleParentsBeforeAttach(t *testing.T) {
	portTable := &recordingTable{get: func(row model.Model) error {
		row.(*ovnnb.LogicalRouterPort).UUID = "lrp-uuid"
		return nil
	}}
	routerTable := &recordingTable{get: func(model.Model) error { return nil }}
	routerTable.filter = func(predicate, result any) error {
		rows := result.(*[]ovnnb.LogicalRouter)
		candidates := []ovnnb.LogicalRouter{
			{Name: "old", Ports: []string{"lrp-uuid"}},
			{Name: "target"},
		}
		filter := predicate.(func(*ovnnb.LogicalRouter) bool)
		for i := range candidates {
			if filter(&candidates[i]) {
				*rows = append(*rows, candidates[i])
			}
		}
		return nil
	}
	executor := &recordingExecutor{}
	facade := &LogicalRouterPorts{ports: portTable, routers: routerTable, executor: executor}

	require.NoError(t, facade.EnsureParent(t.Context(), "port", "target"))
	require.Len(t, executor.plans, 1)
	require.Len(t, executor.plans[0].Operations(), 2)
	require.Equal(t, "lrp-parent", executor.plans[0].Method())
	require.Len(t, routerTable.mutations, 2)
	require.Equal(t, ovsdb.MutateOperationDelete, routerTable.mutations[0][0].Mutator)
	require.Equal(t, ovsdb.MutateOperationInsert, routerTable.mutations[1][0].Mutator)
}

func TestEnsureParentKeepsTableSpecificErrors(t *testing.T) {
	require.EqualError(t, (*LogicalSwitchPorts)(nil).EnsureParent(t.Context(), "port", "target"), "logical switch port facade is nil")
	require.EqualError(t, (&LogicalSwitchPorts{}).EnsureParent(t.Context(), "port", "target"), "logical switch port facade is nil")
	require.EqualError(t, (&LogicalSwitchPorts{
		ports:    &recordingTable{},
		switches: &recordingTable{},
		executor: &recordingExecutor{},
	}).EnsureParent(t.Context(), "", "target"), "logical switch port and switch names are required")

	require.EqualError(t, (*LogicalRouterPorts)(nil).EnsureParent(t.Context(), "port", "target"), "logical router port facade is nil")
	require.EqualError(t, (&LogicalRouterPorts{}).EnsureParent(t.Context(), "port", "target"), "logical router port facade is nil")
	require.EqualError(t, (&LogicalRouterPorts{
		ports:    &recordingTable{},
		routers:  &recordingTable{},
		executor: &recordingExecutor{},
	}).EnsureParent(t.Context(), "port", ""), "logical router port and router names are required")
}
