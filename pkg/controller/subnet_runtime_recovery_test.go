package controller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
)

func TestSubnetRuntimeRecoveryWaitsForStartup(t *testing.T) {
	t.Parallel()

	controller := &Controller{}
	controller.beginSubnetRuntimeRecovery()
	result := make(chan error, 1)
	go func() { result <- controller.WaitForSubnetRuntimeRecovery(context.Background()) }()

	select {
	case err := <-result:
		t.Fatalf("recovery gate opened before startup completed: %v", err)
	case <-time.After(10 * time.Millisecond):
	}

	controller.finishSubnetRuntimeRecovery()
	require.NoError(t, <-result)
	require.NoError(t, controller.WaitForSubnetRuntimeRecovery(t.Context()))
}

func TestSubnetRuntimeRecoveryHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	controller := &Controller{}
	controller.beginSubnetRuntimeRecovery()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, controller.WaitForSubnetRuntimeRecovery(ctx), context.Canceled)
}

func TestSubnetRuntimeRecoveryFinishIsIdempotent(t *testing.T) {
	t.Parallel()

	controller := &Controller{}
	controller.beginSubnetRuntimeRecovery()
	controller.finishSubnetRuntimeRecovery()
	controller.finishSubnetRuntimeRecovery()
	require.NoError(t, controller.WaitForSubnetRuntimeRecovery(t.Context()))
}

func TestRecoverSubnetRuntimeRestoresSubnetAndU2OAddress(t *testing.T) {
	t.Parallel()

	subnet := &kubeovnv1.Subnet{
		Name: "subnet",
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock: "10.0.0.0/29",
			Gateway:   "10.0.0.1",
			Vpc:       "ovn-cluster",
		},
		Status: kubeovnv1.SubnetStatus{
			U2OInterconnectionIP:  "10.0.0.2",
			U2OInterconnectionMAC: "00:00:00:00:00:22",
		},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{})
	require.NoError(t, err)
	controller := fc.fakeController
	controller.ipam = ipam.NewIPAM()

	require.NoError(t, controller.RecoverSubnetRuntime(t.Context(), subnet))
	_, _, mac, err := controller.ipam.GetStaticAddress(
		"u2o-interconnection.ovn-cluster.subnet",
		"ovn-cluster-subnet",
		subnet.Status.U2OInterconnectionIP,
		new(subnet.Status.U2OInterconnectionMAC),
		subnet.Name,
		false,
	)
	require.NoError(t, err)
	require.Equal(t, subnet.Status.U2OInterconnectionMAC, mac)

	address, _, _, err := controller.ipam.GetRandomAddress("pod", "pod-nic", nil, subnet.Name, "", nil, true)
	require.NoError(t, err)
	require.NotEqual(t, subnet.Status.U2OInterconnectionIP, address)
}

func TestRecoverSubnetRuntimeStopsBeforeIPAMWhenFenceFails(t *testing.T) {
	t.Parallel()

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{})
	require.NoError(t, err)
	controller := fc.fakeController
	controller.ipam = ipam.NewIPAM()
	fenceErr := context.Canceled
	require.NoError(t, controller.SetSubnetRuntimeFence(func(context.Context) error { return fenceErr }))

	subnet := &kubeovnv1.Subnet{
		Name: "subnet",
		Spec: kubeovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/29", Gateway: "10.0.0.1"},
	}
	require.ErrorIs(t, controller.RecoverSubnetRuntime(t.Context(), subnet), fenceErr)
	_, _, _, err = controller.ipam.GetRandomAddress("pod", "pod-nic", nil, subnet.Name, "", nil, true)
	require.ErrorIs(t, err, ipam.ErrNoSubnet)
}

func TestRecoverSubnetRuntimeStopsBeforeU2OWhenFenceExpires(t *testing.T) {
	t.Parallel()

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{})
	require.NoError(t, err)
	controller := fc.fakeController
	controller.ipam = ipam.NewIPAM()
	fenceErr := context.Canceled
	fenceChecks := 0
	require.NoError(t, controller.SetSubnetRuntimeFence(func(context.Context) error {
		fenceChecks++
		if fenceChecks > 1 {
			return fenceErr
		}
		return nil
	}))

	subnet := &kubeovnv1.Subnet{
		Name: "subnet",
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock: "10.0.0.0/29",
			Gateway:   "10.0.0.1",
			Vpc:       "ovn-cluster",
		},
		Status: kubeovnv1.SubnetStatus{
			U2OInterconnectionIP:  "10.0.0.2",
			U2OInterconnectionMAC: "00:00:00:00:00:22",
		},
	}
	require.ErrorIs(t, controller.RecoverSubnetRuntime(t.Context(), subnet), fenceErr)
	require.NotNil(t, controller.ipam.Subnets[subnet.Name])
	_, _, _, allocated := controller.ipam.GetPodAddressByNic(
		"u2o-interconnection.ovn-cluster.subnet",
		"ovn-cluster-subnet",
		subnet.Name,
	)
	require.False(t, allocated)
}

func TestRecoverSubnetRuntimeFailsClosedWithoutU2OMAC(t *testing.T) {
	t.Parallel()

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{})
	require.NoError(t, err)
	controller := fc.fakeController
	controller.ipam = ipam.NewIPAM()
	fc.mockOvnClient.EXPECT().GetLogicalRouterPort("ovn-cluster-subnet", true).Return(nil, nil)

	subnet := &kubeovnv1.Subnet{
		Name: "subnet",
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock: "10.0.0.0/29",
			Gateway:   "10.0.0.1",
			Vpc:       "ovn-cluster",
		},
		Status: kubeovnv1.SubnetStatus{U2OInterconnectionIP: "10.0.0.2"},
	}
	require.ErrorContains(t, controller.RecoverSubnetRuntime(t.Context(), subnet), "MAC is unavailable")
	_, _, _, allocated := controller.ipam.GetPodAddressByNic(
		"u2o-interconnection.ovn-cluster.subnet",
		"ovn-cluster-subnet",
		subnet.Name,
	)
	require.False(t, allocated)
}
