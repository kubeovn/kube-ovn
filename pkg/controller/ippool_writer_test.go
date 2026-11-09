package controller

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

type recordingIPPoolWriter struct {
	finalizerErr error
	statistics   int
	status       int
}

func (w *recordingIPPoolWriter) PatchIPs(*kubeovnv1.IPPool, []string) error {
	return errors.New("unexpected membership write")
}

func (w *recordingIPPoolWriter) PatchStatus(*kubeovnv1.IPPool) error {
	w.status++
	return nil
}

func (w *recordingIPPoolWriter) PatchStatistics(*kubeovnv1.IPPool) error {
	w.statistics++
	return nil
}

func (w *recordingIPPoolWriter) AddFinalizer(*kubeovnv1.IPPool) error {
	return w.finalizerErr
}

func (w *recordingIPPoolWriter) RemoveFinalizer(*kubeovnv1.IPPool) error {
	return errors.New("unexpected finalizer removal")
}

func TestIPPoolWriterInstallationRequiresExclusiveRuntimeMode(t *testing.T) {
	c := &Controller{config: &Configuration{ControllerRuntimeMode: ControllerRuntimeModeVlan}}
	w := &recordingIPPoolWriter{}
	require.Error(t, c.SetIPPoolWriter(w))
	c.config.ControllerRuntimeMode = ControllerRuntimeModeIPPool
	require.Error(t, c.SetIPPoolWriter(nil))
	require.NoError(t, c.SetIPPoolWriter(w))
	require.Error(t, c.SetIPPoolWriter(w))
}

func TestIPPoolWriterFinalizerFailureAbortsBeforeSelectingAddresses(t *testing.T) {
	subnet := &kubeovnv1.Subnet{Name: "subnet", Spec: kubeovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/29"}}
	pool := &kubeovnv1.IPPool{Name: "pool", Spec: kubeovnv1.IPPoolSpec{Subnet: subnet.Name, IPCount: 2}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{subnet}, IPPools: []*kubeovnv1.IPPool{pool}})
	require.NoError(t, err)
	c := fc.fakeController
	c.ippoolKeyMutex = keymutex.NewHashed(97)
	c.config.ControllerRuntimeMode = ControllerRuntimeModeIPPool
	require.NoError(t, c.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, "", nil))
	writeErr := errors.New("object disappeared or changed during finalizer write")
	w := &recordingIPPoolWriter{finalizerErr: writeErr}
	require.NoError(t, c.SetIPPoolWriter(w))
	// No OVN expectations: a failed guard cannot prepare the data plane.
	require.ErrorIs(t, c.handleAddOrUpdateIPPool(pool.Name), writeErr)
	require.NotContains(t, c.ipam.Subnets[subnet.Name].IPPools, pool.Name)
	require.Zero(t, w.statistics)
	require.Zero(t, w.status)
}

func TestIPPoolWriterStatisticsDispatchOwnsNoConditions(t *testing.T) {
	pool := &kubeovnv1.IPPool{Name: "pool", Spec: kubeovnv1.IPPoolSpec{Subnet: "subnet", IPs: []string{"10.0.0.2"}}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{IPPools: []*kubeovnv1.IPPool{pool}})
	require.NoError(t, err)
	c := fc.fakeController
	c.config.ControllerRuntimeMode = ControllerRuntimeModeIPPool
	require.NoError(t, c.ipam.AddOrUpdateSubnet("subnet", "10.0.0.0/29", "", nil))
	require.NoError(t, c.ipam.AddOrUpdateIPPool("subnet", pool.Name, pool.Spec.IPs))
	w := &recordingIPPoolWriter{}
	require.NoError(t, c.SetIPPoolWriter(w))
	require.NoError(t, c.handleUpdateIPPoolStatusObject(pool))
	require.Equal(t, 1, w.statistics)
	require.Zero(t, w.status)
}
