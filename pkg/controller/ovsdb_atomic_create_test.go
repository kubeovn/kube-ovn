package controller

import (
	"fmt"
	"sync"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
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
