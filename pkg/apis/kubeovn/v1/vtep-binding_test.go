package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestVtepBindingVtepLogicalSwitchName(t *testing.T) {
	t.Parallel()
	b := &VtepBinding{Spec: VtepBindingSpec{Subnet: "subnet-a"}}
	require.Equal(t, "subnet-a", b.VtepLogicalSwitchName())
	b.Spec.VtepLogicalSwitch = "custom"
	require.Equal(t, "custom", b.VtepLogicalSwitchName())
}

func TestVtepBindingStatusReady(t *testing.T) {
	t.Parallel()
	status := &VtepBindingStatus{}
	status.EnsureStandardConditions()
	require.False(t, status.IsReady())

	status.NotReady("ReconcileFailed", "subnet missing")
	require.False(t, status.Ready)
	require.Equal(t, corev1.ConditionFalse, status.GetCondition(Ready).Status)

	status.ReadyCondition("VTEPAttachmentReady", "")
	require.True(t, status.Ready)
	require.True(t, status.IsReady())

	bytes, err := status.Bytes()
	require.NoError(t, err)
	require.Contains(t, string(bytes), `"ready":true`)
}
