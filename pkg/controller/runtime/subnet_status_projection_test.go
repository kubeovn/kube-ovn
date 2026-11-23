package controllerruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/internal"
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

func TestSubnetStatusSnapshotsIsolateIPCounters(t *testing.T) {
	snapshot, err := NewSubnetWriteSnapshot(testSubnet(""), true)
	require.NoError(t, err)
	counterValues := []string{"42", "21", "340282366920938463463374607431768211455", "18446744073709551614"}
	counters := func(status *kubeovnv1.SubnetStatus) []*internal.BigInt {
		return []*internal.BigInt{&status.V4AvailableIPs, &status.V4UsingIPs, &status.V6AvailableIPs, &status.V6UsingIPs}
	}
	for _, path := range []string{"generated deepcopy", "status projection"} {
		t.Run(path, func(t *testing.T) {
			status := &kubeovnv1.SubnetStatus{}
			for i, counter := range counters(status) {
				_, ok := counter.SetString(counterValues[i], 10)
				require.True(t, ok)
			}
			var copied *kubeovnv1.SubnetStatus
			if path == "generated deepcopy" {
				copied = status.DeepCopy()
			} else {
				projection, err := NewSubnetStatusProjection(snapshot, status)
				require.NoError(t, err)
				copied = &projection.Status
			}
			for i, counter := range counters(copied) {
				require.Equal(t, counterValues[i], counter.String())
				counter.SetInt64(999)
				require.Equal(t, counterValues[i], counters(status)[i].String(), "mutating a copied counter must not affect the source")
				copiedValue := counter.String()
				original := counters(status)[i]
				original.SetInt64(777)
				require.Equal(t, copiedValue, counter.String(), "mutating the source must not affect the snapshot")
			}
		})
	}
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
