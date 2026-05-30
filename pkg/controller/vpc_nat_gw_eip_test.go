package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

// fakeGw returns a minimal VpcNatGateway CRD object for use in tests.
func fakeGw(name string) *kubeovnv1.VpcNatGateway {
	return &kubeovnv1.VpcNatGateway{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       kubeovnv1.VpcNatGatewaySpec{},
	}
}

func TestNatGwDeleted(t *testing.T) {
	t.Parallel()

	t.Run("gateway CRD exists returns false", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			VpcNatGateways: []*kubeovnv1.VpcNatGateway{fakeGw("test-gw")},
		})
		require.NoError(t, err)
		deleted, err := fc.fakeController.natGwDeleted("test-gw")
		require.NoError(t, err)
		require.False(t, deleted)
	})

	t.Run("gateway CRD missing returns true", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, nil)
		require.NoError(t, err)
		deleted, err := fc.fakeController.natGwDeleted("missing-gw")
		require.NoError(t, err)
		require.True(t, deleted)
	})
}

// TestDeleteEipInPod_NatGwGone verifies that deleteEipInPod returns nil (skips
// cleanup) when the VpcNatGateway CRD no longer exists, allowing the EIP to
// be finalized without an infinite retry loop.
func TestDeleteEipInPod_NatGwGone(t *testing.T) {
	t.Parallel()
	fc, err := newFakeControllerWithOptions(t, nil) // no VpcNatGateway
	require.NoError(t, err)
	err = fc.fakeController.deleteEipInPod("missing-gw", "10.0.0.1/24", "kube-system")
	require.NoError(t, err, "should skip cleanup when gateway CRD is gone")
}

// TestDeleteEipInPod_NatGwExistsPodMissing verifies that deleteEipInPod returns
// an error (triggering a reconcile retry) when the VpcNatGateway CRD exists but
// its pod is not yet available (e.g., being recreated).
func TestDeleteEipInPod_NatGwExistsPodMissing(t *testing.T) {
	t.Parallel()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{fakeGw("test-gw")},
	})
	require.NoError(t, err)
	err = fc.fakeController.deleteEipInPod("test-gw", "10.0.0.1/24", "kube-system")
	require.Error(t, err, "should return error to retry when pod is temporarily absent")
}

// TestDelEipQoSInPod_NatGwGone verifies cleanup is skipped when gateway is gone.
func TestDelEipQoSInPod_NatGwGone(t *testing.T) {
	t.Parallel()
	fc, err := newFakeControllerWithOptions(t, nil)
	require.NoError(t, err)
	err = fc.fakeController.delEipQoSInPod("missing-gw", "10.0.0.1", "kube-system", kubeovnv1.QoSDirectionIngress)
	require.NoError(t, err, "should skip cleanup when gateway CRD is gone")
}

// TestDelEipQoSInPod_NatGwExistsPodMissing verifies that an error is returned
// when the gateway CRD exists but the pod is not ready.
func TestDelEipQoSInPod_NatGwExistsPodMissing(t *testing.T) {
	t.Parallel()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{fakeGw("test-gw")},
	})
	require.NoError(t, err)
	err = fc.fakeController.delEipQoSInPod("test-gw", "10.0.0.1", "kube-system", kubeovnv1.QoSDirectionEgress)
	require.Error(t, err, "should return error to retry when pod is temporarily absent")
}
