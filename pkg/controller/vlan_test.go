package controller

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
)

func requireVlanEvent(t *testing.T, recorder *record.FakeRecorder, parts ...string) string {
	t.Helper()
	select {
	case event := <-recorder.Events:
		for _, part := range parts {
			require.Contains(t, event, part)
		}
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for VLAN event")
		return ""
	}
}

func TestRecordVlanError(t *testing.T) {
	recorder := record.NewFakeRecorder(1)
	controller := &Controller{recorder: recorder}
	vlan := &kubeovnv1.Vlan{ObjectMeta: metav1.ObjectMeta{Name: "vlan-a"}}
	sourceErr := errors.New("reconcile failed")

	err := controller.recordVlanError(vlan, "ReconcileFailed", sourceErr)

	require.ErrorIs(t, err, sourceErr)
	require.Equal(t, "Warning ReconcileFailed reconcile failed", requireVlanEvent(t, recorder))
}

func TestHandleAddVlanRecordsSuccess(t *testing.T) {
	const (
		vlanName = "vlan-a"
		pnName   = "provider-a"
	)
	vlan := &kubeovnv1.Vlan{
		ObjectMeta: metav1.ObjectMeta{Name: vlanName},
		Spec:       kubeovnv1.VlanSpec{Provider: pnName},
	}
	pn := &kubeovnv1.ProviderNetwork{ObjectMeta: metav1.ObjectMeta{Name: pnName}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vlans:            []*kubeovnv1.Vlan{vlan},
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn},
	})
	require.NoError(t, err)
	fc.fakeController.vlanKeyMutex = keymutex.NewHashed(0)

	err = fc.fakeController.handleAddVlan(vlanName)

	require.NoError(t, err)
	require.Equal(t, "Normal ReconcileSuccess Vlan vlan-a reconciled successfully", requireVlanEvent(t, fc.fakeController.recorder.(*record.FakeRecorder)))
}

func TestHandleAddVlanRecordsMissingProviderNetwork(t *testing.T) {
	vlan := &kubeovnv1.Vlan{
		ObjectMeta: metav1.ObjectMeta{Name: "vlan-a"},
		Spec:       kubeovnv1.VlanSpec{Provider: "missing-provider"},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Vlans: []*kubeovnv1.Vlan{vlan}})
	require.NoError(t, err)
	fc.fakeController.vlanKeyMutex = keymutex.NewHashed(0)

	err = fc.fakeController.handleAddVlan(vlan.Name)

	require.Error(t, err)
	requireVlanEvent(t, fc.fakeController.recorder.(*record.FakeRecorder),
		"Warning GetProviderNetworkFailed", `providernetwork.kubeovn.io "missing-provider" not found`)
}

func TestHandleAddVlanRecordsUpdateFailure(t *testing.T) {
	vlan := &kubeovnv1.Vlan{ObjectMeta: metav1.ObjectMeta{Name: "vlan-a"}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Vlans: []*kubeovnv1.Vlan{vlan}})
	require.NoError(t, err)
	fc.fakeController.vlanKeyMutex = keymutex.NewHashed(0)
	fc.fakeController.config.DefaultProviderName = "provider-a"
	sourceErr := errors.New("update failed")
	kubeovnClient := fc.fakeController.config.KubeOvnClient.(*kubeovnfake.Clientset)
	kubeovnClient.PrependReactor("update", "vlans", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, sourceErr
	})

	err = fc.fakeController.handleAddVlan(vlan.Name)

	require.ErrorIs(t, err, sourceErr)
	require.Equal(t, "Warning UpdateSpecFailed update failed", requireVlanEvent(t, fc.fakeController.recorder.(*record.FakeRecorder)))
}

func TestHandleAddVlanRecordsConflict(t *testing.T) {
	const pnName = "provider-a"
	vlans := []*kubeovnv1.Vlan{
		{ObjectMeta: metav1.ObjectMeta{Name: "vlan-a"}, Spec: kubeovnv1.VlanSpec{ID: 100, Provider: pnName}},
		{ObjectMeta: metav1.ObjectMeta{Name: "vlan-b"}, Spec: kubeovnv1.VlanSpec{ID: 100, Provider: pnName}},
	}
	pn := &kubeovnv1.ProviderNetwork{ObjectMeta: metav1.ObjectMeta{Name: pnName}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vlans:            vlans,
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn},
	})
	require.NoError(t, err)
	fc.fakeController.vlanKeyMutex = keymutex.NewHashed(0)

	err = fc.fakeController.handleAddVlan("vlan-b")

	require.Error(t, err)
	requireVlanEvent(t, fc.fakeController.recorder.(*record.FakeRecorder),
		"Warning VlanConflict", "provider provider-a new vlan vlan-b conflict with old vlan vlan-a")
}

func TestHandleUpdateVlanRecordsLocalnetFailure(t *testing.T) {
	const (
		vlanName   = "vlan-a"
		pnName     = "provider-a"
		subnetName = "subnet-a"
	)
	vlan := &kubeovnv1.Vlan{
		ObjectMeta: metav1.ObjectMeta{Name: vlanName},
		Spec:       kubeovnv1.VlanSpec{ID: 100, Provider: pnName},
	}
	pn := &kubeovnv1.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: pnName},
		Status:     kubeovnv1.ProviderNetworkStatus{Vlans: []string{vlanName}},
	}
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: subnetName},
		Spec:       kubeovnv1.SubnetSpec{Vlan: vlanName},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vlans:            []*kubeovnv1.Vlan{vlan},
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn},
		Subnets:          []*kubeovnv1.Subnet{subnet},
	})
	require.NoError(t, err)
	fc.fakeController.vlanKeyMutex = keymutex.NewHashed(0)
	fc.mockOvnClient.EXPECT().
		SetLogicalSwitchPortVlanTag(ovs.GetLocalnetName(subnetName), 100).
		Return(errors.New("set tag failed"))

	err = fc.fakeController.handleUpdateVlan(vlanName)

	require.Error(t, err)
	requireVlanEvent(t, fc.fakeController.recorder.(*record.FakeRecorder),
		corev1.EventTypeWarning+" SetLocalnetTagFailed", "set tag failed")
}

func TestHandleDelVlanRecordsSuccess(t *testing.T) {
	const (
		vlanName = "vlan-a"
		pnName   = "provider-a"
	)
	pn := &kubeovnv1.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: pnName},
		Status:     kubeovnv1.ProviderNetworkStatus{Vlans: []string{vlanName}},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn},
	})
	require.NoError(t, err)
	fc.fakeController.vlanKeyMutex = keymutex.NewHashed(0)

	err = fc.fakeController.handleDelVlan(vlanName)

	require.NoError(t, err)
	require.Equal(t, "Normal DeleteSuccess Vlan vlan-a deleted successfully", requireVlanEvent(t, fc.fakeController.recorder.(*record.FakeRecorder)))
}
