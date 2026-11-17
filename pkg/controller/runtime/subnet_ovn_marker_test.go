package controllerruntime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

func TestMemorySubnetOVNMarkerBackendCAS(t *testing.T) {
	backend := newMemorySubnetOVNMarkerBackend()
	marker := testSubnetOVNOwnerMarker()
	require.NoError(t, backend.Acquire(t.Context(), marker))
	require.NoError(t, backend.Acquire(t.Context(), marker))
	current, found, err := backend.Read(t.Context(), marker.Key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, marker, current)

	wrong := marker
	wrong.Owner.Token++
	require.ErrorIs(t, backend.Acquire(t.Context(), wrong), ErrSubnetOVNMarkerChanged)
	require.ErrorIs(t, backend.Release(t.Context(), wrong), ErrSubnetOVNMarkerChanged)

	rebind := marker
	rebind.Owner.Token++
	rebind.Owner.WriterEpoch = "epoch-b"
	require.NoError(t, backend.Rebind(t.Context(), marker, rebind))
	require.ErrorIs(t, backend.Release(t.Context(), marker), ErrSubnetOVNMarkerChanged)
	require.NoError(t, backend.Release(t.Context(), rebind))
	_, found, err = backend.Read(t.Context(), marker.Key)
	require.NoError(t, err)
	require.False(t, found)
}

func TestMemorySubnetOVNMarkerBackendRejectsWrongIdentity(t *testing.T) {
	backend := newMemorySubnetOVNMarkerBackend()
	marker := testSubnetOVNOwnerMarker()
	for name, wrong := range map[string]SubnetOVNOwnerMarker{
		"token": func() SubnetOVNOwnerMarker { m := marker; m.Owner.Token = 0; return m }(),
		"uid":   func() SubnetOVNOwnerMarker { m := marker; m.Owner.SubnetUID = ""; return m }(),
		"owner UID differs from key": func() SubnetOVNOwnerMarker {
			m := marker
			m.Owner.SubnetUID = "other-uid"
			return m
		}(),
		"rv":    func() SubnetOVNOwnerMarker { m := marker; m.Owner.ResourceVersion = ""; return m }(),
		"epoch": func() SubnetOVNOwnerMarker { m := marker; m.Owner.WriterEpoch = ""; return m }(),
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, backend.Acquire(t.Context(), wrong), ErrSubnetOVNMarkerInvalid)
		})
	}
}

func TestMemorySubnetOVNMarkerBackendRebindIsReplaySafe(t *testing.T) {
	backend := newMemorySubnetOVNMarkerBackend()
	previous := testSubnetOVNOwnerMarker()
	replacement := previous
	replacement.Owner.Token++
	replacement.Owner.WriterEpoch = "epoch-b"
	require.NoError(t, backend.Acquire(t.Context(), previous))
	require.NoError(t, backend.Rebind(t.Context(), previous, replacement))
	require.NoError(t, backend.Rebind(t.Context(), previous, replacement))
	require.ErrorIs(t, backend.Rebind(t.Context(), previous, testSubnetOVNOwnerMarker()), ErrSubnetOVNMarkerChanged)
}

func TestMemorySubnetOVNMarkerBackendHonorsCancellation(t *testing.T) {
	backend := newMemorySubnetOVNMarkerBackend()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, backend.Acquire(ctx, testSubnetOVNOwnerMarker()), context.Canceled)
}

func TestSubnetOVNMutationKeyIsStableAcrossResourceVersions(t *testing.T) {
	snapshot := testSubnetWriteSnapshot("7")
	updated := snapshot
	updated.ResourceVersion = "8"

	require.Equal(t, subnetOVNMutationKey(snapshot, SubnetOVNAllocateOperation), subnetOVNMutationKey(updated, SubnetOVNAllocateOperation))
}

func testSubnetWriteSnapshot(resourceVersion string) SubnetWriteSnapshot {
	return SubnetWriteSnapshot{
		Name:            "subnet-a",
		UID:             types.UID("uid-a"),
		Generation:      3,
		ResourceVersion: resourceVersion,
	}
}

func testSubnetOVNOwnerMarker() SubnetOVNOwnerMarker {
	return SubnetOVNOwnerMarker{
		Key: SubnetOVNMutationKey{
			Name: "subnet-a", UID: types.UID("uid-a"), Operation: SubnetOVNAllocateOperation, IntentHash: "intent-a",
		},
		Owner: SubnetOVNOwner{Token: 7, SubnetUID: types.UID("uid-a"), Generation: 3, ResourceVersion: "9", WriterEpoch: "epoch-a"},
	}
}
