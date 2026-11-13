package controllerruntime

import (
	"testing"

	"github.com/stretchr/testify/require"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/internal"
)

func TestNewSubnetStatusCalculatorProjectorReconstructsObservedStatus(t *testing.T) {
	snapshot, err := NewSubnetWriteSnapshot(&kubeovnv1.Subnet{
		Name: "subnet-a", ResourceVersion: "7",
		Spec:       kubeovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/29", Protocol: kubeovnv1.ProtocolIPv4},
	}, true)
	require.NoError(t, err)
	observation := SubnetShadowObservation{Status: SubnetStatusObservation{
		Conditions:         []kubeovnv1.Condition{{Type: kubeovnv1.ConditionType("Foreign")}},
		V4AvailableIPs:     "4",
		V4AvailableIPRange: "10.0.0.4-10.0.0.7",
		V4UsingIPs:         "2",
		V4UsingIPRange:     "10.0.0.2-10.0.0.3",
	}}
	projector := NewSubnetStatusCalculatorProjector(func(subnet *kubeovnv1.Subnet) (*kubeovnv1.Subnet, error) {
		require.Equal(t, snapshot.Name, subnet.Name)
		require.Equal(t, snapshot.Spec, subnet.Spec)
		require.True(t, subnet.Status.V4AvailableIPs.EqualInt64(4))
		require.True(t, subnet.Status.V4UsingIPs.EqualInt64(2))
		subnet.Status.V4AvailableIPs = internal.NewBigInt(3)
		return subnet, nil
	})

	projection, err := projector.ProjectSubnetStatus(t.Context(), snapshot, observation)
	require.NoError(t, err)
	require.True(t, projection.Status.V4AvailableIPs.EqualInt64(3))
	require.Len(t, projection.Status.Conditions, 1)
}

func TestNewSubnetStatusCalculatorProjectorRejectsInvalidObservedCounter(t *testing.T) {
	projector := NewSubnetStatusCalculatorProjector(func(*kubeovnv1.Subnet) (*kubeovnv1.Subnet, error) {
		t.Fatal("calculator must not run")
		return nil, nil
	})
	_, err := projector.ProjectSubnetStatus(t.Context(), SubnetWriteSnapshot{Name: "subnet-a"}, SubnetShadowObservation{
		Status: SubnetStatusObservation{V4AvailableIPs: "invalid"},
	})
	require.EqualError(t, err, `parse subnet subnet-a v4 available IPs: invalid integer "invalid"`)
}
