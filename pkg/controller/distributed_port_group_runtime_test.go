package controller

import (
	"testing"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestDistributedSubnetSweepsRespectRuntimeWriter(t *testing.T) {
	subnet := &kubeovnv1.Subnet{
		Name: "ovn-default",
		Spec: kubeovnv1.SubnetSpec{Vpc: "ovn-cluster", GatewayType: kubeovnv1.GWDistributedType},
	}
	fixture, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)
	ctrl := fixture.fakeController
	ctrl.config.ControllerRuntimeMode = ControllerRuntimeModeSubnetWriter
	ctrl.subnetRuntimeEvents = newSubnetRuntimeEventFanout()
	var events []string
	unsubscribe := ctrl.SubscribeSubnetRuntimeEvents(t.Context(), func(name string) { events = append(events, name) })
	t.Cleanup(unsubscribe)
	// No database expectations: both sweeps must enter the runtime's fenced
	// reconcile path, including when no node event has set the dirty bit.
	ctrl.syncDistributedSubnetPortGroups(t.Context())
	ctrl.distributedSubnetNeedSync.Store(true)
	ctrl.syncDistributedSubnetRoutesWithContext(t.Context())
	require.Equal(t, []string{subnet.Name, subnet.Name}, events)
	require.Zero(t, ctrl.addOrUpdateSubnetQueue.Len())
	require.Zero(t, ctrl.addOrUpdateSubnetQueue.NumRequeues(subnet.Name))
}
