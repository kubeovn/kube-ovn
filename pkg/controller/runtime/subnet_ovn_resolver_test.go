package controllerruntime

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func TestNewOVSDBSubnetOVNMutationResolverRequiresProvider(t *testing.T) {
	resolver, err := NewOVSDBSubnetOVNMutationResolver(nil)
	require.Nil(t, resolver)
	require.EqualError(t, err, "subnet OVN mutation resolver requires an OVSDB provider")
}

func TestOVSDBSubnetOVNMutationResolverReadsLogicalSwitch(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	subnet.Generation = 3
	subnet.ResourceVersion = "7"
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	key := subnetOVNMutationKey(snapshot, SubnetOVNAllocateOperation)
	receipt := SubnetOVNMutationReceipt{Key: key, Intent: subnetOVNMutationIntentForSnapshot(snapshot), Token: 8, Generation: snapshot.Generation, ResourceVersion: snapshot.ResourceVersion, WriterEpoch: "epoch-a"}
	handle := &markerTestHandle{logicalSwitch: &ovnnb.LogicalSwitch{Name: snapshot.Name}}
	resolver, err := NewOVSDBSubnetOVNMutationResolver(&markerTestProvider{handle: handle})
	require.NoError(t, err)

	state, err := resolver.ResolveSubnetOVNMutation(t.Context(), snapshot, key, receipt)
	require.NoError(t, err)
	require.Equal(t, SubnetOVNMutationStateCommitted, state)

	handle.logicalSwitch = nil
	state, err = resolver.ResolveSubnetOVNMutation(t.Context(), snapshot, key, receipt)
	require.NoError(t, err)
	require.Equal(t, SubnetOVNMutationStateAbsent, state)
}

func TestOVSDBSubnetOVNMutationResolverRejectsReceiptWithoutIntent(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	subnet.Generation = 3
	subnet.ResourceVersion = "7"
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	key := subnetOVNMutationKey(snapshot, SubnetOVNAllocateOperation)
	receipt := SubnetOVNMutationReceipt{Key: key, Token: 8, Generation: snapshot.Generation, ResourceVersion: snapshot.ResourceVersion, WriterEpoch: "epoch-a"}
	resolver, err := NewOVSDBSubnetOVNMutationResolver(&markerTestProvider{handle: &markerTestHandle{logicalSwitch: &ovnnb.LogicalSwitch{Name: snapshot.Name}}})
	require.NoError(t, err)

	state, err := resolver.ResolveSubnetOVNMutation(t.Context(), snapshot, key, receipt)
	require.ErrorIs(t, err, ErrSubnetOVNResolverUnknown)
	require.Equal(t, SubnetOVNMutationStateUnknown, state)
}

func TestOVSDBSubnetOVNMutationResolverReadsPatchPortIntent(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = types.UID("subnet-uid")
	subnet.Generation = 3
	subnet.ResourceVersion = "7"
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	intent := SubnetOVNMutationIntent{
		LogicalSwitchName:     snapshot.Name,
		Generation:            snapshot.Generation,
		LogicalRouterName:     "ovn-cluster",
		LogicalRouterPortName: "ovn-cluster-subnet-a",
		LogicalSwitchPortName: "subnet-a-ovn-cluster",
		LogicalRouterNetworks: []string{"10.0.0.1/24", "fd00::1/64"},
		GatewayMAC:            "00:00:00:00:00:01",
		RequirePatchPorts:     true,
	}
	key := subnetOVNMutationKeyForIntent(snapshot, SubnetOVNAllocateOperation, intent)
	receipt := SubnetOVNMutationReceipt{Key: key, Intent: intent, Token: 8, Generation: snapshot.Generation, ResourceVersion: snapshot.ResourceVersion, WriterEpoch: "epoch-a"}
	handle := &markerTestHandle{
		logicalSwitch:     &ovnnb.LogicalSwitch{Name: snapshot.Name, Ports: []string{"lsp-uuid"}},
		logicalRouter:     &ovnnb.LogicalRouter{Name: intent.LogicalRouterName, Ports: []string{"lrp-uuid"}},
		logicalRouterPort: &ovnnb.LogicalRouterPort{UUID: "lrp-uuid", Name: intent.LogicalRouterPortName, Networks: []string{"fd00::1/64", "10.0.0.1/24"}, MAC: intent.GatewayMAC},
		logicalSwitchPort: &ovnnb.LogicalSwitchPort{UUID: "lsp-uuid", Name: intent.LogicalSwitchPortName, Type: "router", Options: map[string]string{"router-port": intent.LogicalRouterPortName}},
	}
	resolver, err := NewOVSDBSubnetOVNMutationResolver(&markerTestProvider{handle: handle})
	require.NoError(t, err)

	state, err := resolver.ResolveSubnetOVNMutation(t.Context(), snapshot, key, receipt)
	require.NoError(t, err)
	require.Equal(t, SubnetOVNMutationStateCommitted, state)
	handle.logicalSwitch.Ports = nil
	state, err = resolver.ResolveSubnetOVNMutation(t.Context(), snapshot, key, receipt)
	require.NoError(t, err)
	require.Equal(t, SubnetOVNMutationStatePending, state)
	handle.logicalSwitch.Ports = []string{"lsp-uuid"}
	handle.logicalRouter.Ports = nil
	state, err = resolver.ResolveSubnetOVNMutation(t.Context(), snapshot, key, receipt)
	require.NoError(t, err)
	require.Equal(t, SubnetOVNMutationStatePending, state)
	handle.logicalRouter.Ports = []string{"lrp-uuid"}

	handle.logicalRouterPort.Networks = []string{"10.0.0.2/24", "fd00::1/64"}
	state, err = resolver.ResolveSubnetOVNMutation(t.Context(), snapshot, key, receipt)
	require.NoError(t, err)
	require.Equal(t, SubnetOVNMutationStatePending, state)

	cleanupKey := subnetOVNMutationKeyForIntent(snapshot, SubnetOVNCleanupOperation, intent)
	cleanupReceipt := receipt
	cleanupReceipt.Key = cleanupKey
	state, err = resolver.ResolveSubnetOVNMutation(t.Context(), snapshot, cleanupKey, cleanupReceipt)
	require.NoError(t, err)
	require.Equal(t, SubnetOVNMutationStatePending, state)

	handle.logicalSwitch = nil
	handle.logicalRouterPort = nil
	handle.logicalSwitchPort = nil
	state, err = resolver.ResolveSubnetOVNMutation(t.Context(), snapshot, cleanupKey, cleanupReceipt)
	require.NoError(t, err)
	require.Equal(t, SubnetOVNMutationStateAbsent, state)
}

func TestValidateSubnetOVNMutationStateClassifiesPending(t *testing.T) {
	require.ErrorIs(t, validateSubnetOVNMutationState(SubnetOVNMutationStatePending), ErrSubnetOVNResolverPending)
	require.ErrorIs(t, validateSubnetOVNMutationState(SubnetOVNMutationStateUnknown), ErrSubnetOVNResolverUnknown)
}
