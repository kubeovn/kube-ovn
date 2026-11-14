package controllerruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestNewSubnetStatusProjectionCopiesStatusAndSnapshot(t *testing.T) {
	subnet := testSubnet("")
	subnet.UID = "uid-a"
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	status := &kubeovnv1.SubnetStatus{GatewayMAC: "00:00:5e:00:53:01"}

	projection, err := NewSubnetStatusProjection(snapshot, status)
	require.NoError(t, err)
	require.Equal(t, snapshot, projection.Snapshot)
	require.Equal(t, status.GatewayMAC, projection.Status.GatewayMAC)
	status.GatewayMAC = "changed"
	require.Equal(t, "00:00:5e:00:53:01", projection.Status.GatewayMAC)
}

func TestNewSubnetStatusProjectionRejectsMissingInputs(t *testing.T) {
	status := &kubeovnv1.SubnetStatus{}
	_, err := NewSubnetStatusProjection(SubnetWriteSnapshot{}, status)
	require.EqualError(t, err, "subnet status projection requires a Subnet snapshot")

	subnet := testSubnet("")
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	_, err = NewSubnetStatusProjection(snapshot, nil)
	require.EqualError(t, err, "subnet status projection requires status")
}

func TestSubnetStatusProjectorFunc(t *testing.T) {
	subnet := testSubnet("")
	snapshot, err := NewSubnetWriteSnapshot(subnet, true)
	require.NoError(t, err)
	want := errors.New("projection failed")
	projector := SubnetStatusProjectorFunc(func(context.Context, SubnetWriteSnapshot, SubnetShadowObservation) (SubnetStatusProjection, error) {
		return SubnetStatusProjection{}, want
	})
	_, err = projector.ProjectSubnetStatus(t.Context(), snapshot, SubnetShadowObservation{})
	require.ErrorIs(t, err, want)

	var nilProjector SubnetStatusProjectorFunc
	_, err = nilProjector.ProjectSubnetStatus(t.Context(), snapshot, SubnetShadowObservation{})
	require.EqualError(t, err, "subnet status projector is not configured")
}
