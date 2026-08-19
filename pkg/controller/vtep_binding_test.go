package controller

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/keymutex"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	mockovs "github.com/kubeovn/kube-ovn/mocks/pkg/ovs"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestVtepBindingLogicalSwitchName(t *testing.T) {
	t.Parallel()

	binding := &kubeovnv1.VtepBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "rack1"},
		Spec: kubeovnv1.VtepBindingSpec{
			Subnet: "tenant-a",
		},
	}
	require.Equal(t, "tenant-a", binding.VtepLogicalSwitchName())

	binding.Spec.VtepLogicalSwitch = "custom-ls"
	require.Equal(t, "custom-ls", binding.VtepLogicalSwitchName())
}

func TestGetVtepLogicalSwitchPortName(t *testing.T) {
	t.Parallel()
	require.Equal(t, "vtep.rack1-tenant-a", ovs.GetVtepLogicalSwitchPortName("rack1-tenant-a"))
}

func TestValidateVtepBindingConflict(t *testing.T) {
	t.Parallel()

	existing := &kubeovnv1.VtepBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "existing"},
		Spec: kubeovnv1.VtepBindingSpec{
			Subnet:         "subnet-a",
			PhysicalSwitch: "nexus01",
			PhysicalPort:   "Ethernet1/20",
			VlanID:         120,
		},
	}
	candidate := &kubeovnv1.VtepBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "candidate"},
		Spec: kubeovnv1.VtepBindingSpec{
			Subnet:         "subnet-b",
			PhysicalSwitch: "nexus01",
			PhysicalPort:   "Ethernet1/20",
			VlanID:         120,
		},
	}

	c := &Controller{}
	c.vtepBindingsLister = &fakeVtepBindingLister{items: []*kubeovnv1.VtepBinding{existing}}

	err := c.validateVtepBindingConflict(candidate)
	require.Error(t, err)
	require.Contains(t, err.Error(), "physicalPort")
	require.Contains(t, err.Error(), "vlanID")

	candidate.Spec.VlanID = 121
	err = c.validateVtepBindingConflict(candidate)
	require.NoError(t, err)

	candidate.Spec.Subnet = "subnet-a"
	candidate.Spec.PhysicalPort = "Ethernet1/21"
	err = c.validateVtepBindingConflict(candidate)
	require.Error(t, err)
	require.Contains(t, err.Error(), "vtepLogicalSwitch")
}

func TestHandleAddOrUpdateVtepBindingDisabled(t *testing.T) {
	t.Parallel()

	binding := vtepBindingFixture("disabled")
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets:      []*kubeovnv1.Subnet{vtepSubnetFixture()},
		VtepBindings: []*kubeovnv1.VtepBinding{binding},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.EnableHardwareVtep = false

	require.NoError(t, ctrl.handleAddOrUpdateVtepBinding(binding.Name))
	got, err := ctrl.config.KubeOvnClient.KubeovnV1().VtepBindings().Get(context.Background(), binding.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotContains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
}

func TestHandleAddOrUpdateVtepBindingAddsFinalizer(t *testing.T) {
	t.Parallel()

	binding := vtepBindingFixture("add-finalizer")
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets:      []*kubeovnv1.Subnet{vtepSubnetFixture()},
		VtepBindings: []*kubeovnv1.VtepBinding{binding},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.EnableHardwareVtep = true
	expectVtepPending(fc.mockOvnClient, fc.mockOvnSbClient, binding)

	require.NoError(t, ctrl.handleAddOrUpdateVtepBinding(binding.Name))
	got, err := ctrl.config.KubeOvnClient.KubeovnV1().VtepBindings().Get(context.Background(), binding.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
	require.Equal(t, eventWaitingForChassis, got.Status.GetCondition(kubeovnv1.Ready).Reason)
	requireVtepEventContains(t, drainVtepEvents(ctrl), "Warning", eventWaitingForChassis)
}

func TestHandleAddOrUpdateVtepBindingStopsWhenObjectGone(t *testing.T) {
	t.Parallel()

	binding := vtepBindingFixture("gone")
	fc := newFakeController(t)
	ctrl := fc.fakeController
	ctrl.config.EnableHardwareVtep = true
	ctrl.vtepBindingKeyMutex = keymutex.NewHashed(0)
	ctrl.vtepBindingsLister = &fakeVtepBindingLister{items: []*kubeovnv1.VtepBinding{binding}}

	require.NoError(t, ctrl.handleAddOrUpdateVtepBinding(binding.Name))
}

func TestCleanupVtepBindingRetriesWhenDBDisconnected(t *testing.T) {
	t.Parallel()

	binding := vtepBindingFixture("db-disconnect-del")
	now := metav1.Now()
	binding.DeletionTimestamp = &now
	controllerutil.AddFinalizer(binding, util.KubeOVNControllerFinalizer)

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets:      []*kubeovnv1.Subnet{vtepSubnetFixture()},
		VtepBindings: []*kubeovnv1.VtepBinding{binding},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.EnableHardwareVtep = true
	ctrl.config.VtepDbAddr = "tcp:192.0.2.10:6640"

	err = ctrl.handleAddOrUpdateVtepBinding(binding.Name)
	require.Error(t, err)
	require.ErrorIs(t, err, errVtepDBNotConnected)

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().VtepBindings().Get(context.Background(), binding.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
	requireVtepEventContains(t, drainVtepEvents(ctrl), "Warning", eventVtepBindingCleanupFailed, eventVTEPDBNotConnected)
}

func TestHandleAddOrUpdateVtepBindingReadyDespiteDBOutage(t *testing.T) {
	t.Parallel()

	binding := vtepBindingFixture("ready-db-down")
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets:      []*kubeovnv1.Subnet{vtepSubnetFixture()},
		VtepBindings: []*kubeovnv1.VtepBinding{binding},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.EnableHardwareVtep = true
	ctrl.config.VtepDbAddr = "tcp:192.0.2.10:6640"
	expectVtepReady(fc.mockOvnClient, fc.mockOvnSbClient, binding)

	require.NoError(t, ctrl.handleAddOrUpdateVtepBinding(binding.Name))
	got, err := ctrl.config.KubeOvnClient.KubeovnV1().VtepBindings().Get(context.Background(), binding.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, got.Status.Ready)
	require.Equal(t, corev1.ConditionFalse, got.Status.GetCondition(kubeovnv1.VTEPDBReady).Status)
	require.Equal(t, eventVTEPDBNotConnected, got.Status.GetCondition(kubeovnv1.VTEPDBReady).Reason)
	require.Contains(t, got.Status.GetCondition(kubeovnv1.Ready).Message, "vtep.ready-db-down")
	require.Contains(t, got.Status.GetCondition(kubeovnv1.Ready).Message, "gw1")
	events := drainVtepEvents(ctrl)
	requireVtepEventContains(t, events, "Normal", eventVTEPAttachmentReady, "vtep.ready-db-down", "gw1")
	requireVtepEventContains(t, events, "Warning", eventVTEPDBNotConnected)
}

func TestHandleAddOrUpdateVtepBindingReadyWhenChassisUpFalse(t *testing.T) {
	t.Parallel()

	binding := vtepBindingFixture("ready-up-false")
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets:      []*kubeovnv1.Subnet{vtepSubnetFixture()},
		VtepBindings: []*kubeovnv1.VtepBinding{binding},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.EnableHardwareVtep = true
	expectVtepLSP(fc.mockOvnClient, binding)
	chassis := "chassis-uuid"
	upFalse := false
	fc.mockOvnSbClient.EXPECT().GetPortBindingByLogicalPort(ovs.GetVtepLogicalSwitchPortName(binding.Name), true).Return(&ovnsb.PortBinding{Chassis: &chassis, Up: &upFalse}, nil)
	fc.mockOvnSbClient.EXPECT().GetChassisNameByUUID(chassis).Return("gw1", nil)

	require.NoError(t, ctrl.handleAddOrUpdateVtepBinding(binding.Name))
	got, err := ctrl.config.KubeOvnClient.KubeovnV1().VtepBindings().Get(context.Background(), binding.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, got.Status.Ready)
	require.Equal(t, "gw1", got.Status.Chassis)
}

func TestHandleAddOrUpdateVtepBindingClearsErrorOnRecovery(t *testing.T) {
	t.Parallel()

	binding := vtepBindingFixture("error-recovery")
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets:      []*kubeovnv1.Subnet{vtepSubnetFixture()},
		VtepBindings: []*kubeovnv1.VtepBinding{binding},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.EnableHardwareVtep = true

	fc.mockOvnClient.EXPECT().LogicalSwitchExists("subnet-a").Return(false, nil)
	require.Error(t, ctrl.handleAddOrUpdateVtepBinding(binding.Name))
	failed, err := ctrl.config.KubeOvnClient.KubeovnV1().VtepBindings().Get(context.Background(), binding.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, failed.Status.IsConditionTrue(kubeovnv1.Error))
	requireVtepEventContains(t, drainVtepEvents(ctrl), "Warning", "ReconcileFailed")

	expectVtepReady(fc.mockOvnClient, fc.mockOvnSbClient, binding)
	require.NoError(t, ctrl.handleAddOrUpdateVtepBinding(binding.Name))
	got, err := ctrl.config.KubeOvnClient.KubeovnV1().VtepBindings().Get(context.Background(), binding.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, got.Status.Ready)
	require.Equal(t, corev1.ConditionFalse, got.Status.GetCondition(kubeovnv1.Error).Status)
}

func TestHandleAddOrUpdateVtepBindingRevalidatesReady(t *testing.T) {
	t.Parallel()

	binding := vtepBindingFixture("revalidate")
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets:      []*kubeovnv1.Subnet{vtepSubnetFixture()},
		VtepBindings: []*kubeovnv1.VtepBinding{binding},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.EnableHardwareVtep = true
	expectVtepReady(fc.mockOvnClient, fc.mockOvnSbClient, binding)
	require.NoError(t, ctrl.handleAddOrUpdateVtepBinding(binding.Name))

	require.Eventually(t, func() bool {
		cached, err := ctrl.vtepBindingsLister.Get(binding.Name)
		return err == nil && cached.Status.Ready
	}, 2*time.Second, 20*time.Millisecond)

	_ = drainVtepEvents(ctrl)
	expectVtepPending(fc.mockOvnClient, fc.mockOvnSbClient, binding)
	require.NoError(t, ctrl.handleAddOrUpdateVtepBinding(binding.Name))
	got, err := ctrl.config.KubeOvnClient.KubeovnV1().VtepBindings().Get(context.Background(), binding.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.False(t, got.Status.Ready)
	require.Equal(t, eventChassisLost, got.Status.GetCondition(kubeovnv1.Ready).Reason)
	requireVtepEventContains(t, drainVtepEvents(ctrl), "Warning", eventChassisLost)
}

func TestHandleAddOrUpdateVtepBindingDBReconcileFailed(t *testing.T) {
	t.Parallel()

	binding := vtepBindingFixture("db-reconcile-fail")
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets:      []*kubeovnv1.Subnet{vtepSubnetFixture()},
		VtepBindings: []*kubeovnv1.VtepBinding{binding},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.EnableHardwareVtep = true
	ctrl.config.VtepDbAddr = "tcp:192.0.2.10:6640"

	mockVtep := mockovs.NewMockVtepDBClient(gomock.NewController(t))
	mockVtep.EXPECT().EnsureVtepBinding(
		binding.Spec.PhysicalSwitch,
		binding.Spec.PhysicalPort,
		binding.VtepLogicalSwitchName(),
		binding.Name,
		binding.Spec.VlanID,
	).Return(errors.New("vlan mutate failed"))
	ctrl.setVtepClient(mockVtep)
	expectVtepReady(fc.mockOvnClient, fc.mockOvnSbClient, binding)

	require.NoError(t, ctrl.handleAddOrUpdateVtepBinding(binding.Name))
	got, err := ctrl.config.KubeOvnClient.KubeovnV1().VtepBindings().Get(context.Background(), binding.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, got.Status.Ready)
	require.Equal(t, eventVTEPDBReconcileFailed, got.Status.GetCondition(kubeovnv1.VTEPDBReady).Reason)
	requireVtepEventContains(t, drainVtepEvents(ctrl), "Warning", eventVTEPDBReconcileFailed, "vlan mutate failed")
}

func TestCleanupVtepBindingSkipsUnownedLSP(t *testing.T) {
	t.Parallel()

	binding := vtepBindingFixture("unowned-lsp")
	now := metav1.Now()
	binding.DeletionTimestamp = &now
	controllerutil.AddFinalizer(binding, util.KubeOVNControllerFinalizer)

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets:      []*kubeovnv1.Subnet{vtepSubnetFixture()},
		VtepBindings: []*kubeovnv1.VtepBinding{binding},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.EnableHardwareVtep = true
	lspName := ovs.GetVtepLogicalSwitchPortName(binding.Name)
	fc.mockOvnClient.EXPECT().GetLogicalSwitchPort(lspName, true).Return(&ovnnb.LogicalSwitchPort{
		Name: lspName,
		ExternalIDs: map[string]string{
			ovs.VtepBindingKey:    binding.Name,
			ovs.VtepBindingUIDKey: "other-uid",
		},
	}, nil)
	fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(gomock.Any()).Times(0)

	require.NoError(t, ctrl.handleAddOrUpdateVtepBinding(binding.Name))
	got, err := ctrl.config.KubeOvnClient.KubeovnV1().VtepBindings().Get(context.Background(), binding.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotContains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
	requireVtepEventContains(t, drainVtepEvents(ctrl), "Normal", eventVtepBindingCleanedUp)
}

func TestEnqueueAllVtepBindingsIncludesTerminating(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	terminating := vtepBindingFixture("terminating")
	terminating.DeletionTimestamp = &now
	live := vtepBindingFixture("live")

	c := &Controller{
		config:                      &Configuration{EnableHardwareVtep: true},
		addOrUpdateVtepBindingQueue: newTypedRateLimitingQueue[string]("AddOrUpdateVtepBinding", nil),
		vtepBindingsLister:          &fakeVtepBindingLister{items: []*kubeovnv1.VtepBinding{terminating, live}},
	}
	c.enqueueAllVtepBindings()
	require.Equal(t, 2, c.addOrUpdateVtepBindingQueue.Len())
}

func TestCleanupVtepBindingAfterReconnect(t *testing.T) {
	t.Parallel()

	binding := vtepBindingFixture("reconnect-del")
	now := metav1.Now()
	binding.DeletionTimestamp = &now
	controllerutil.AddFinalizer(binding, util.KubeOVNControllerFinalizer)

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets:      []*kubeovnv1.Subnet{vtepSubnetFixture()},
		VtepBindings: []*kubeovnv1.VtepBinding{binding},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.EnableHardwareVtep = true
	ctrl.config.VtepDbAddr = "tcp:192.0.2.10:6640"

	mockVtep := mockovs.NewMockVtepDBClient(gomock.NewController(t))
	mockVtep.EXPECT().RemoveVtepBinding(
		binding.Spec.PhysicalSwitch,
		binding.Spec.PhysicalPort,
		binding.VtepLogicalSwitchName(),
		binding.Name,
		binding.Spec.VlanID,
	).Return(nil)
	ctrl.setVtepClient(mockVtep)

	lspName := ovs.GetVtepLogicalSwitchPortName(binding.Name)
	fc.mockOvnClient.EXPECT().GetLogicalSwitchPort(lspName, true).Return(&ovnnb.LogicalSwitchPort{
		Name: lspName,
		ExternalIDs: map[string]string{
			ovs.VtepBindingKey:    binding.Name,
			ovs.VtepBindingUIDKey: string(binding.UID),
		},
	}, nil)
	fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(lspName).Return(nil)

	require.NoError(t, ctrl.handleAddOrUpdateVtepBinding(binding.Name))
	got, err := ctrl.config.KubeOvnClient.KubeovnV1().VtepBindings().Get(context.Background(), binding.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotContains(t, got.Finalizers, util.KubeOVNControllerFinalizer)
	requireVtepEventContains(t, drainVtepEvents(ctrl), "Normal", eventVtepBindingCleanedUp)
}

func TestGcVtepBindingDisabled(t *testing.T) {
	t.Parallel()

	fc := newFakeController(t)
	require.NoError(t, fc.fakeController.gcVtepBinding())
}

func TestGcVtepBindingDeletesOrphanLSP(t *testing.T) {
	t.Parallel()

	fc := newFakeController(t)
	ctrl := fc.fakeController
	ctrl.config.EnableHardwareVtep = true
	orphan := &ovnnb.LogicalSwitchPort{
		Name: "vtep.orphan",
		Type: "vtep",
		ExternalIDs: map[string]string{
			"vendor":              util.CniTypeName,
			ovs.VtepBindingKey:    "missing",
			ovs.VtepBindingUIDKey: "missing-uid",
		},
	}
	fc.mockOvnClient.EXPECT().ListLogicalSwitchPorts(true, nil, gomock.Any()).Return([]ovnnb.LogicalSwitchPort{*orphan}, nil)
	fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort("vtep.orphan").Return(nil)

	require.NoError(t, ctrl.gcVtepBinding())
}

func TestTryConnectVtepClientNoopsWhenDisabled(t *testing.T) {
	t.Parallel()
	c := &Controller{config: &Configuration{EnableHardwareVtep: false, VtepDbAddr: "tcp:192.0.2.10:6640"}}
	require.NoError(t, c.tryConnectVtepClient())
	require.Nil(t, c.getVtepClient())
}

type fakeVtepBindingLister struct {
	items []*kubeovnv1.VtepBinding
}

func (f *fakeVtepBindingLister) List(labels.Selector) ([]*kubeovnv1.VtepBinding, error) {
	return f.items, nil
}

func (f *fakeVtepBindingLister) Get(name string) (*kubeovnv1.VtepBinding, error) {
	for _, item := range f.items {
		if item.Name == name {
			return item, nil
		}
	}
	return nil, k8serrors.NewNotFound(kubeovnv1.Resource("vtepbindings"), name)
}

func vtepSubnetFixture() *kubeovnv1.Subnet {
	return &kubeovnv1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "subnet-a"}}
}

func vtepBindingFixture(name string) *kubeovnv1.VtepBinding {
	return &kubeovnv1.VtepBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  types.UID(name + "-uid"),
		},
		Spec: kubeovnv1.VtepBindingSpec{
			Subnet:         "subnet-a",
			PhysicalSwitch: "nexus01",
			PhysicalPort:   "Ethernet1/20",
			VlanID:         120,
		},
	}
}

func expectVtepLSP(mockNb *mockovs.MockNbClient, binding *kubeovnv1.VtepBinding) {
	lspName := ovs.GetVtepLogicalSwitchPortName(binding.Name)
	mockNb.EXPECT().LogicalSwitchExists("subnet-a").Return(true, nil)
	mockNb.EXPECT().CreateVtepLogicalSwitchPort(
		"subnet-a",
		lspName,
		binding.Spec.PhysicalSwitch,
		binding.VtepLogicalSwitchName(),
		gomock.Any(),
	).Return(nil)
}

func expectVtepPending(mockNb *mockovs.MockNbClient, mockSb *mockovs.MockSbClient, binding *kubeovnv1.VtepBinding) {
	expectVtepLSP(mockNb, binding)
	mockSb.EXPECT().GetPortBindingByLogicalPort(ovs.GetVtepLogicalSwitchPortName(binding.Name), true).Return(&ovnsb.PortBinding{}, nil)
}

func expectVtepReady(mockNb *mockovs.MockNbClient, mockSb *mockovs.MockSbClient, binding *kubeovnv1.VtepBinding) {
	expectVtepLSP(mockNb, binding)
	chassis := "chassis-uuid"
	mockSb.EXPECT().GetPortBindingByLogicalPort(ovs.GetVtepLogicalSwitchPortName(binding.Name), true).Return(&ovnsb.PortBinding{Chassis: &chassis}, nil)
	mockSb.EXPECT().GetChassisNameByUUID(chassis).Return("gw1", nil)
}

func drainVtepEvents(ctrl *Controller) []string {
	rec, ok := ctrl.recorder.(*record.FakeRecorder)
	if !ok {
		return nil
	}
	var events []string
	for {
		select {
		case event := <-rec.Events:
			events = append(events, event)
		default:
			return events
		}
	}
}

func requireVtepEventContains(t *testing.T, events []string, parts ...string) {
	t.Helper()
	for _, event := range events {
		matched := true
		for _, part := range parts {
			if !strings.Contains(event, part) {
				matched = false
				break
			}
		}
		if matched {
			return
		}
	}
	require.Failf(t, "missing vtep binding event", "want parts %v in events %v", parts, events)
}
