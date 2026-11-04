package controller

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
)

func TestControllerProviderLogicalSwitchLifecycleAtomic(t *testing.T) {
	for _, abort := range []bool{false, true} {
		name := "commit"
		if abort {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			nb := newInMemoryOVNNbClient(t)
			require.NoError(t, nb.CreateLogicalRouter("router"))
			recorder := &serviceLBTransactionClient{Client: nb.Client, fail: abort}
			nb.Database = table.NewDatabase(recorder, nb.Timeout, table.RetryPolicy{})
			c := &Controller{OVNNbTables: nb}
			err := c.createLogicalSwitch("switch", "router", "10.0.0.0/24", "10.0.0.1", "00:00:00:00:00:01", true, false)
			require.Len(t, recorder.transactions, 1, "the switch and both patch ports must commit together")
			if abort {
				require.Error(t, err)
				ls, getErr := nb.GetLogicalSwitch("switch", true)
				require.NoError(t, getErr)
				require.Nil(t, ls)
				lsp, getErr := nb.GetLogicalSwitchPort("switch-router", true)
				require.NoError(t, getErr)
				require.Nil(t, lsp)
				lrp, getErr := nb.GetLogicalRouterPort("router-switch", true)
				require.NoError(t, getErr)
				require.Nil(t, lrp)
				return
			}
			require.NoError(t, err)
			recorder.transactions = nil
			require.NoError(t, c.createLogicalSwitch("switch", "router", "10.0.0.0/24", "10.0.0.2", "00:00:00:00:00:02", true, false))
			require.Len(t, recorder.transactions, 1)
			lrp, getErr := nb.GetLogicalRouterPort("router-switch", false)
			require.NoError(t, getErr)
			require.Equal(t, []string{"10.0.0.2/24"}, lrp.Networks)
			require.Equal(t, "00:00:00:00:00:02", lrp.MAC)
			recorder.transactions = nil
			require.NoError(t, c.createLogicalSwitch("switch", "router", "10.0.0.0/24", "10.0.0.3", "00:00:00:00:00:03", true, true))
			require.Len(t, recorder.transactions, 1)
			lrp, getErr = nb.GetLogicalRouterPort("router-switch", false)
			require.NoError(t, getErr)
			require.Equal(t, []string{"10.0.0.2/24"}, lrp.Networks)
			require.Equal(t, "00:00:00:00:00:02", lrp.MAC)
		})
	}
}
