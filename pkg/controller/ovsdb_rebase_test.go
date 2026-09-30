package controller

import (
	"context"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
)

func TestControllerProviderVirtualPortUpgrade(t *testing.T) {
	nbClient := newInMemoryOVNNbClient(t)
	c := &Controller{OVNNbTables: nbClient}
	require.NoError(t, nbClient.CreateBareLogicalSwitch("vip-switch"))
	require.NoError(t, nbClient.CreateVirtualLogicalSwitchPort("old-vip", "vip-switch", "10.0.0.5"))
	legacy, err := nbClient.GetLogicalSwitchPort("old-vip", false)
	require.NoError(t, err)

	name := "vip:old-vip:ipv4"
	require.NoError(t, c.createVirtualLogicalSwitchPort(name, "vip-switch", "10.0.0.5"))
	current, err := nbClient.GetLogicalSwitchPort(name, false)
	require.NoError(t, err)
	require.Equal(t, legacy.UUID, current.UUID, "upgrading a VIP must preserve its port identity")
	old, err := nbClient.GetLogicalSwitchPort("old-vip", true)
	require.NoError(t, err)
	require.Nil(t, old)

	require.NoError(t, c.createVirtualLogicalSwitchPort(name, "vip-switch", "10.0.0.6"))
	require.NoError(t, c.setVirtualLogicalSwitchPortAddresses(name, "10.0.0.6"))
	current, err = nbClient.GetLogicalSwitchPort(name, false)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.6", current.Options["virtual-ip"])
	require.Equal(t, []string{"10.0.0.6"}, current.Addresses)
}

func TestControllerProviderMigrationResetPreservesNewOwner(t *testing.T) {
	nbClient := newInMemoryOVNNbClient(t)
	c := &Controller{OVNNbTables: nbClient}
	require.NoError(t, nbClient.CreateBareLogicalSwitch("vm-switch"))
	require.NoError(t, nbClient.CreateBareLogicalSwitchPort("vm-switch", "vm-port", "10.0.0.5", "00:00:00:00:00:01"))
	require.NoError(t, c.setLogicalSwitchPortMigrateOptions("vm-port", "node-a", "node-c"))
	require.NoError(t, c.resetLogicalSwitchPortMigrateOptions("vm-port", "node-a", "node-b", true))
	port, err := nbClient.GetLogicalSwitchPort("vm-port", false)
	require.NoError(t, err)
	require.Equal(t, "node-a,node-c", port.Options["requested-chassis"])
	require.Equal(t, "rarp", port.Options["activation-strategy"])

	require.NoError(t, c.resetLogicalSwitchPortMigrateOptions("vm-port", "node-a", "node-c", false))
	port, err = nbClient.GetLogicalSwitchPort("vm-port", false)
	require.NoError(t, err)
	require.Equal(t, "node-c", port.Options["requested-chassis"])
	require.NotContains(t, port.Options, "activation-strategy")
}

func TestControllerProviderPolicyCompareAndDelete(t *testing.T) {
	nbClient := newInMemoryOVNNbClient(t)
	c := &Controller{OVNNbTables: nbClient}
	require.NoError(t, nbClient.CreateLogicalRouter("node-router"))
	match := "ip4.dst == 10.0.0.5"
	require.NoError(t, c.addLogicalRouterPolicy("node-router", 30000, match, ovnnb.LogicalRouterPolicyActionReroute, []string{"100.64.0.2"}, nil, map[string]string{"node": "old-node"}))
	policies, err := c.getLogicalRouterPolicy("node-router", 30000, match, false)
	require.NoError(t, err)
	require.Len(t, policies, 1)
	observed := policies[0]
	require.NoError(t, c.addLogicalRouterPolicy("node-router", 30000, match, ovnnb.LogicalRouterPolicyActionReroute, []string{"100.64.0.2"}, nil, map[string]string{"node": "new-node"}))

	deleted, err := c.deleteLogicalRouterPolicyIfUnchanged("node-router", observed)
	require.NoError(t, err)
	require.False(t, deleted, "cleanup of the old node must preserve the new owner's policy")
	policies, err = c.getLogicalRouterPolicy("node-router", 30000, match, false)
	require.NoError(t, err)
	require.Len(t, policies, 1)
	require.Equal(t, "new-node", policies[0].ExternalIDs["node"])
	deleted, err = c.deleteLogicalRouterPolicyIfUnchanged("node-router", policies[0])
	require.NoError(t, err)
	require.True(t, deleted)
}

type beforeProviderPolicyUpdateBackend struct {
	*table.Client
	beforeUpdate func()
}

func (b *beforeProviderPolicyUpdateBackend) Transact(ctx context.Context, operations ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	for _, operation := range operations {
		if operation.Op == ovsdb.OperationUpdate && operation.Table == ovnnb.LogicalRouterPolicyTable && b.beforeUpdate != nil {
			before := b.beforeUpdate
			b.beforeUpdate = nil
			before()
			break
		}
	}
	return b.TransactResults(ctx, operations...)
}

func TestControllerProviderPolicyRecreatesVanishedRow(t *testing.T) {
	nbClient := newInMemoryOVNNbClient(t)
	require.NoError(t, nbClient.CreateLogicalRouter("node-router"))
	match := "ip4.dst == 10.0.0.5"
	require.NoError(t, nbClient.AddLogicalRouterPolicy("node-router", 30000, match, ovnnb.LogicalRouterPolicyActionReroute, []string{"100.64.0.2"}, nil, map[string]string{"node": "old-node"}))
	policies, err := nbClient.GetLogicalRouterPolicy("node-router", 30000, match, false)
	require.NoError(t, err)
	require.Len(t, policies, 1)

	gcRan := false
	backend := &beforeProviderPolicyUpdateBackend{Client: nbClient.Client, beforeUpdate: func() {
		gcRan = true
		deleted, err := nbClient.DeleteLogicalRouterPolicyIfUnchanged("node-router", policies[0])
		require.NoError(t, err)
		require.True(t, deleted)
	}}
	c := &Controller{OVNNbTables: table.NewDatabase(backend, 3*time.Second, table.RetryPolicy{})}
	require.NoError(t, c.addLogicalRouterPolicy("node-router", 30000, match, ovnnb.LogicalRouterPolicyActionReroute, []string{"100.64.0.2"}, nil, map[string]string{"node": "new-node"}))
	require.True(t, gcRan)
	policies, err = nbClient.GetLogicalRouterPolicy("node-router", 30000, match, false)
	require.NoError(t, err)
	require.Len(t, policies, 1)
	require.Equal(t, "new-node", policies[0].ExternalIDs["node"])
}
