package controller

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestControllerProviderAtomicRootCreate(t *testing.T) {
	nbClient := newInMemoryOVNNbClient(t)
	controller := &Controller{OVNNbTables: nbClient}
	for _, tc := range []struct {
		kind   string
		create func(string) error
		row    func(string) model.Model
		count  func(string) (int, error)
	}{
		{
			kind:   "switch",
			create: controller.createBareLogicalSwitch,
			row:    func(name string) model.Model { return &ovnnb.LogicalSwitch{Name: name} },
			count: func(name string) (int, error) {
				rows, err := table.Filter[ovnnb.LogicalSwitch](t.Context(), nbClient, &ovnnb.LogicalSwitch{}, func(row *ovnnb.LogicalSwitch) bool { return row.Name == name })
				return len(rows), err
			},
		},
		{
			kind:   "router",
			create: controller.createLogicalRouter,
			row:    func(name string) model.Model { return &ovnnb.LogicalRouter{Name: name} },
			count: func(name string) (int, error) {
				rows, err := table.Filter[ovnnb.LogicalRouter](t.Context(), nbClient, &ovnnb.LogicalRouter{}, func(row *ovnnb.LogicalRouter) bool { return row.Name == name })
				return len(rows), err
			},
		},
		{
			kind:   "load balancer",
			create: func(name string) error { return controller.createLoadBalancer(name, "tcp", "ip_src") },
			row:    func(name string) model.Model { return &ovnnb.LoadBalancer{Name: name} },
			count: func(name string) (int, error) {
				rows, err := table.Filter[ovnnb.LoadBalancer](t.Context(), nbClient, &ovnnb.LoadBalancer{}, func(row *ovnnb.LoadBalancer) bool { return row.Name == name })
				return len(rows), err
			},
		},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			require.Error(t, tc.create(""))
			t.Run("existing duplicates", func(t *testing.T) {
				name := fmt.Sprintf("duplicate-%s", tc.kind)
				first, second := tc.row(name), tc.row(name)
				require.NoError(t, nbClient.Table(first).Create(t.Context(), "seed-duplicates", first, second))
				require.NoError(t, tc.create(name))
				count, err := tc.count(name)
				require.NoError(t, err)
				require.Equal(t, 2, count)
			})
			t.Run("concurrent creates", func(t *testing.T) {
				name := fmt.Sprintf("concurrent-%s", tc.kind)
				const creators = 10
				errs := make([]error, creators)
				var wg sync.WaitGroup
				for i := range creators {
					wg.Go(func() { errs[i] = tc.create(name) })
				}
				wg.Wait()
				for _, err := range errs {
					require.NoError(t, err)
				}
				count, err := tc.count(name)
				require.NoError(t, err)
				require.Equal(t, 1, count)
			})
		})
	}
}

func TestControllerProviderLoadBalancerPreservesExisting(t *testing.T) {
	nbClient := newInMemoryOVNNbClient(t)
	c := &Controller{OVNNbTables: nbClient}
	row := &ovnnb.LoadBalancer{Name: "existing", Protocol: new("udp"), SelectionFields: []string{"ip_dst"}, ExternalIDs: map[string]string{"owner": "other"}}
	require.NoError(t, nbClient.Table(row).Create(t.Context(), "seed-load-balancer", row))
	existing, err := c.getLoadBalancer(row.Name, false)
	require.NoError(t, err)
	require.NoError(t, c.createLoadBalancer(row.Name, "tcp", "ip_src"))
	current, err := c.getLoadBalancer(row.Name, false)
	require.NoError(t, err)
	require.Equal(t, existing, current)
	require.NoError(t, nbClient.Table(row).Create(t.Context(), "seed-duplicate", &ovnnb.LoadBalancer{Name: row.Name}))
	require.NoError(t, c.createLoadBalancer(row.Name, "tcp"))
	_, err = c.getLoadBalancer(row.Name, true)
	require.Error(t, err, "ordinary lookups must continue rejecting duplicate names")
}

type loadBalancerCreateBackend struct {
	*tableBackend
	operations     []ovsdb.Operation
	operationError string
	transportError error
}

func (b *loadBalancerCreateBackend) Create(rows ...model.Model) ([]ovsdb.Operation, error) {
	b.created = append(b.created, rows...)
	return []ovsdb.Operation{{Op: ovsdb.OperationInsert, Table: ovnnb.LoadBalancerTable}}, nil
}

func (b *loadBalancerCreateBackend) Transact(_ context.Context, operations ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	b.operations = operations
	results := make([]ovsdb.OperationResult, len(operations))
	results[0].Error = b.operationError
	return results, b.transportError
}

func TestControllerProviderLoadBalancerAtomicTransaction(t *testing.T) {
	for _, tc := range []struct {
		name           string
		operationError string
		transportError error
		wantError      bool
	}{
		{name: "insert"},
		{name: "stale cache after concurrent insert", operationError: "timed out"},
		{name: "constraint failure", operationError: "constraint violation", wantError: true},
		{name: "transport timeout", transportError: context.DeadlineExceeded, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &loadBalancerCreateBackend{tableBackend: newTableBackend(), operationError: tc.operationError, transportError: tc.transportError}
			c := &Controller{OVNNbTables: table.NewDatabase(backend, time.Second, table.RetryPolicy{})}
			err := c.createLoadBalancer("service-lb", "tcp", "ip_src", "tp_src")
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Len(t, backend.operations, 2)
			require.Equal(t, ovsdb.Operation{
				Op: ovsdb.OperationWait, Table: ovnnb.LoadBalancerTable, Timeout: new(0),
				Where:   []ovsdb.Condition{{Column: "name", Function: ovsdb.ConditionEqual, Value: "service-lb"}},
				Columns: []string{"name"}, Until: string(ovsdb.WaitConditionNotEqual), Rows: []ovsdb.Row{{"name": "service-lb"}},
			}, backend.operations[0])
			require.Equal(t, ovsdb.OperationInsert, backend.operations[1].Op)
			require.Equal(t, &ovnnb.LoadBalancer{
				Name: "service-lb", Protocol: new("tcp"), SelectionFields: []string{"ip_src", "tp_src"}, ExternalIDs: map[string]string{"vendor": util.CniTypeName},
			}, backend.created[0])
		})
	}
}
