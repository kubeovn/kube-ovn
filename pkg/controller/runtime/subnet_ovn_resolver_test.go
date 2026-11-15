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
