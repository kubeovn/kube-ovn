package ovs

import (
	"context"
	"errors"
	"time"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
)

func (suite *OvnClientTestSuite) Test_SubnetPortGroupMembership() {
	t := suite.T()
	ctx := t.Context()
	nb := suite.ovnNBClient
	subnet := "test-pg-membership"
	src, dst, unrelated := subnet+"-src", subnet+"-dst", subnet+"-unrelated"
	pod, retained, localnet := subnet+"-pod", subnet+"-retained", subnet+"-localnet"

	require.NoError(t, nb.CreateBareLogicalSwitch(subnet))
	require.NoError(t, nb.CreateBareLogicalSwitchPort(subnet, pod, "unknown", ""))
	require.NoError(t, nb.CreateBareLogicalSwitchPort(subnet, retained, "unknown", ""))
	require.NoError(t, nb.CreateLocalnetLogicalSwitchPort(subnet, localnet, "localnet-provider", "192.0.2.0/24", 0))
	require.NoError(t, nb.CreatePortGroup(src, map[string]string{"subnet": subnet, "node": "source"}))
	require.NoError(t, nb.CreatePortGroup(dst, map[string]string{"subnet": subnet, "node": "destination"}))
	require.NoError(t, nb.CreatePortGroup(unrelated, map[string]string{"subnet": subnet + "-other"}))
	require.NoError(t, nb.PortGroupAddPorts(src, pod, retained))
	require.NoError(t, nb.PortGroupAddPorts(unrelated, pod))

	readGroups := func() (map[string]ovnnb.PortGroup, map[string]ovnnb.LogicalSwitchPort) {
		t.Helper()
		groups, ports, err := nb.GetSubnetPortGroupMembership(ctx, subnet)
		require.NoError(t, err)
		require.Len(t, groups, 2)
		groupsByName := make(map[string]ovnnb.PortGroup, len(groups))
		for _, pg := range groups {
			groupsByName[pg.Name] = pg
		}
		portsByName := make(map[string]ovnnb.LogicalSwitchPort, len(ports))
		for _, lsp := range ports {
			portsByName[lsp.Name] = lsp
		}
		return groupsByName, portsByName
	}

	groups, ports := readGroups()
	require.Len(t, ports, 3)
	require.Equal(t, "localnet", ports[localnet].Type)
	require.Equal(t, subnet, ports[pod].ExternalIDs[LogicalSwitchKey])
	require.Equal(t, "source", groups[src].ExternalIDs["node"])
	podID, retainedID := ports[pod].UUID, ports[retained].UUID
	require.NotEmpty(t, podID)
	require.ElementsMatch(t, []string{podID, retainedID}, groups[src].Ports)

	movePod := []PortGroupMembershipChange{
		{Group: groups[src], Remove: []string{podID}},
		{Group: groups[dst], Add: []string{podID}},
	}

	// A stale ownership snapshot prevents the source mutation as well as the add.
	require.NoError(t, nb.CreatePortGroup(dst, map[string]string{"subnet": subnet, "node": "new-destination"}))
	require.Error(t, nb.ApplyPortGroupMembershipChanges(ctx, movePod, nil))
	groups, _ = readGroups()
	require.ElementsMatch(t, []string{podID, retainedID}, groups[src].Ports)
	require.Empty(t, groups[dst].Ports)

	movePod[1].Group = groups[dst]
	require.NoError(t, nb.ApplyPortGroupMembershipChanges(ctx, movePod, nil))
	require.Error(t, nb.ApplyPortGroupMembershipChanges(ctx, movePod, nil))
	groups, _ = readGroups()
	require.Equal(t, []string{retainedID}, groups[src].Ports)
	require.Equal(t, []string{podID}, groups[dst].Ports)
	other, err := nb.GetPortGroup(unrelated, false)
	require.NoError(t, err)
	require.Equal(t, []string{podID}, other.Ports)

	// A target already containing the port still has to match its snapshot
	// before the duplicate membership in the source is removed.
	require.NoError(t, nb.PortGroupAddPorts(src, pod))
	groups, _ = readGroups()
	require.NoError(t, nb.PortGroupRemovePorts(dst, pod))
	require.Error(t, nb.ApplyPortGroupMembershipChanges(ctx, []PortGroupMembershipChange{
		{Group: groups[src], Remove: []string{podID}},
		{Group: groups[dst]},
	}, nil))
	groups, _ = readGroups()
	require.ElementsMatch(t, []string{podID, retainedID}, groups[src].Ports)
	require.Empty(t, groups[dst].Ports)
}

func (suite *OvnClientTestSuite) Test_SubnetPortGroupMembershipMissingPort() {
	t := suite.T()
	ctx := t.Context()
	nb := suite.ovnNBClient
	subnet := "test-pg-membership-missing-port"
	pgName, lspName := subnet+"-group", subnet+"-port"

	require.NoError(t, nb.CreateBareLogicalSwitch(subnet))
	require.NoError(t, nb.CreateBareLogicalSwitchPort(subnet, lspName, "unknown", ""))
	require.NoError(t, nb.CreatePortGroup(pgName, map[string]string{"subnet": subnet}))
	groups, ports, err := nb.GetSubnetPortGroupMembership(ctx, subnet)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Len(t, ports, 1)
	require.NoError(t, nb.DeleteLogicalSwitchPort(lspName))

	// If the LSP disappears after the snapshot, a weak reference could otherwise
	// be silently dropped by the database while the transaction reports success.
	require.Error(t, nb.ApplyPortGroupMembershipChanges(ctx, []PortGroupMembershipChange{
		{Group: groups[0], Add: []string{ports[0].UUID}},
	}, nil))
	require.NoError(t, suite.failedOvnNBClient.ApplyPortGroupMembershipChanges(ctx, []PortGroupMembershipChange{
		{Group: groups[0]},
	}, nil))

	disconnected, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stop()
	_, _, err = suite.failedOvnNBClient.GetSubnetPortGroupMembership(disconnected, subnet)
	require.Error(t, err)

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = nb.GetSubnetPortGroupMembership(cancelled, subnet)
	require.ErrorIs(t, err, context.Canceled)
}

type portGroupMoveClient struct {
	*table.Client
	mutations      int
	beforeMutation func()
	dropReply      bool
}

func (c *portGroupMoveClient) Transact(ctx context.Context, operations ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	hasMutation := false
	for _, operation := range operations {
		if operation.Op == ovsdb.OperationMutate {
			hasMutation = true
			c.mutations++
		}
	}
	if hasMutation && c.beforeMutation != nil {
		c.beforeMutation()
	}
	result, err := c.TransactResults(ctx, operations...)
	if hasMutation && err == nil && c.dropReply {
		return nil, errors.New("transaction reply lost")
	}
	return result, err
}

func (suite *OvnClientTestSuite) Test_MovePortToPortGroup() {
	t := suite.T()
	ctx := t.Context()
	nb := suite.ovnNBClient
	subnet, port := "test-pg-move", "test-pg-move-port"
	source, target := subnet+"-source", subnet+"-target"
	unrelated, emptyValue, absentKey, idle := subnet+"-unrelated", subnet+"-empty", subnet+"-absent", subnet+"-idle"
	scope := map[string]string{"subnet": subnet, "node": "", "np": ""}

	require.NoError(t, nb.CreateBareLogicalSwitch(subnet))
	require.NoError(t, nb.CreateBareLogicalSwitchPort(subnet, port, "unknown", ""))
	for _, name := range []string{source, target, idle} {
		require.NoError(t, nb.CreatePortGroup(name, map[string]string{"subnet": subnet, "node": name, "np": name}))
	}
	require.NoError(t, nb.CreatePortGroup(unrelated, map[string]string{"subnet": "other", "node": "node", "np": "policy"}))
	require.NoError(t, nb.CreatePortGroup(emptyValue, map[string]string{"subnet": subnet, "node": "", "np": "policy"}))
	require.NoError(t, nb.CreatePortGroup(absentKey, map[string]string{"subnet": subnet, "node": "node"}))
	for _, name := range []string{unrelated, emptyValue, absentKey} {
		require.NoError(t, nb.PortGroupAddPorts(name, port))
	}

	recorder := &portGroupMoveClient{Client: nb.Client}
	mover := &OVNNbClient{Database: table.NewDatabase(recorder, nb.Timeout, table.RetryPolicy{})}
	require.NoError(t, mover.MovePortToPortGroup(ctx, port, target, scope))
	require.Equal(t, 1, recorder.mutations)
	recorder.mutations = 0
	require.NoError(t, mover.MovePortToPortGroup(ctx, port, target, scope))
	require.Zero(t, recorder.mutations, "steady membership must perform no mutation")

	require.NoError(t, nb.PortGroupAddPorts(source, port))
	recorder.dropReply = true
	require.ErrorContains(t, mover.MovePortToPortGroup(ctx, port, target, scope), "reply lost")
	recorder.dropReply = false
	recorder.mutations = 0
	require.NoError(t, mover.MovePortToPortGroup(ctx, port, target, scope))
	require.Zero(t, recorder.mutations, "retry after a committed write must use the fresh snapshot")

	uuid, groups, err := nb.getPortGroupMoveSnapshot(ctx, port, target, scope)
	require.NoError(t, err)
	require.Len(t, groups, 3, "empty source groups and other subnets must not be returned")
	require.NotEmpty(t, uuid)
	for _, name := range []string{target, unrelated, emptyValue, absentKey} {
		group, err := nb.GetPortGroup(name, false)
		require.NoError(t, err)
		require.Equal(t, []string{uuid}, group.Ports)
	}
	group, err := nb.GetPortGroup(source, false)
	require.NoError(t, err)
	require.Empty(t, group.Ports)
}

func (suite *OvnClientTestSuite) Test_MovePortToPortGroupFailures() {
	t := suite.T()
	ctx := t.Context()
	nb := suite.ovnNBClient
	subnet, port, retained := "test-pg-move-failure", "test-pg-move-failure-port", "test-pg-move-failure-retained"
	source, target, unscoped := subnet+"-source", subnet+"-target", subnet+"-unscoped"
	scope := map[string]string{"subnet": subnet, "node": ""}
	require.NoError(t, nb.CreateBareLogicalSwitch(subnet))
	for _, name := range []string{port, retained} {
		require.NoError(t, nb.CreateBareLogicalSwitchPort(subnet, name, "unknown", ""))
	}
	for _, name := range []string{source, target} {
		require.NoError(t, nb.CreatePortGroup(name, map[string]string{"subnet": subnet, "node": name}))
	}
	require.NoError(t, nb.CreatePortGroup(unscoped, map[string]string{"subnet": subnet}))
	require.NoError(t, nb.PortGroupAddPorts(source, port))
	require.NoError(t, nb.PortGroupAddPorts(target, retained))

	require.ErrorContains(t, nb.MovePortToPortGroup(ctx, port+"-missing", target, scope), "does not exist")
	require.ErrorContains(t, nb.MovePortToPortGroup(ctx, port, target+"-missing", scope), "does not exist")
	require.ErrorContains(t, nb.MovePortToPortGroup(ctx, port, unscoped, scope), "requested scope")

	recorder := &portGroupMoveClient{
		Client: nb.Client,
		beforeMutation: func() {
			require.NoError(t, nb.PortGroupRemovePorts(target, retained))
		},
	}
	mover := &OVNNbClient{Database: table.NewDatabase(recorder, nb.Timeout, table.RetryPolicy{})}
	require.Error(t, mover.MovePortToPortGroup(ctx, port, target, scope))
	sourceGroup, err := nb.GetPortGroup(source, false)
	require.NoError(t, err)
	portRow, err := nb.GetLogicalSwitchPort(port, false)
	require.NoError(t, err)
	require.Equal(t, []string{portRow.UUID}, sourceGroup.Ports, "stale target must abort source removal")
	recorder.beforeMutation = nil
	require.NoError(t, mover.MovePortToPortGroup(ctx, port, target, scope))

	outage, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	require.Error(t, suite.failedOvnNBClient.MovePortToPortGroup(outage, port, target, scope))
}

func (suite *OvnClientTestSuite) Test_MovePortToNodePortGroup() {
	t := suite.T()
	ctx := t.Context()
	nb := suite.ovnNBClient
	subnet, port := "test-pg-move-node", "test-pg-move-node-port"
	source, target, subnetGroup := subnet+"-source", subnet+"-target", subnet+"-subnet"
	scope := map[string]string{"node": "", "np": ""}
	require.NoError(t, nb.CreateBareLogicalSwitch(subnet))
	require.NoError(t, nb.CreateBareLogicalSwitchPort(subnet, port, "unknown", ""))
	require.NoError(t, nb.CreatePortGroup(source, map[string]string{"node": "source", "np": "source"}))
	require.NoError(t, nb.CreatePortGroup(target, map[string]string{"node": "target", "np": "target", "subnet": ""}))
	require.NoError(t, nb.CreatePortGroup(subnetGroup, map[string]string{"node": "target", "np": "target", "subnet": subnet}))
	require.NoError(t, nb.PortGroupAddPorts(source, port))
	require.NoError(t, nb.PortGroupAddPorts(subnetGroup, port))

	recorder := &portGroupMoveClient{Client: nb.Client}
	mover := &OVNNbClient{Database: table.NewDatabase(recorder, nb.Timeout, table.RetryPolicy{})}
	require.ErrorContains(t, mover.MovePortToPortGroup(ctx, port, subnetGroup, scope, "subnet"), "requested scope")
	require.Zero(t, recorder.mutations)
	require.NoError(t, mover.MovePortToPortGroup(ctx, port, target, scope, "subnet"))
	require.Equal(t, 2, recorder.mutations)
	recorder.mutations = 0
	require.NoError(t, mover.MovePortToPortGroup(ctx, port, target, scope, "subnet"))
	require.Zero(t, recorder.mutations)

	uuid, groups, err := nb.getPortGroupMoveSnapshot(ctx, port, target, scope, "subnet")
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, target, groups[0].Name)
	sourceGroup, err := nb.GetPortGroup(source, false)
	require.NoError(t, err)
	require.Empty(t, sourceGroup.Ports)
	retainedGroup, err := nb.GetPortGroup(subnetGroup, false)
	require.NoError(t, err)
	require.Equal(t, []string{uuid}, retainedGroup.Ports)
}
