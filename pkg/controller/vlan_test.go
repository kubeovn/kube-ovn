package controller

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
)

type recordedVlanEvent struct {
	object    runtime.Object
	eventType string
	reason    string
	message   string
}

type vlanEventRecorder struct {
	events []recordedVlanEvent
}

type transientErrorSubnetLister struct {
	kubeovnlisters.SubnetLister
	err error
}

func (l *transientErrorSubnetLister) List(selector labels.Selector) ([]*kubeovnv1.Subnet, error) {
	if l.err != nil {
		err := l.err
		l.err = nil
		return nil, err
	}
	return l.SubnetLister.List(selector)
}

func (r *vlanEventRecorder) Event(object runtime.Object, eventType, reason, message string) {
	r.events = append(r.events, recordedVlanEvent{object: object, eventType: eventType, reason: reason, message: message})
}

func (r *vlanEventRecorder) Eventf(object runtime.Object, eventType, reason, messageFmt string, args ...any) {
	r.Event(object, eventType, reason, fmt.Sprintf(messageFmt, args...))
}

func (r *vlanEventRecorder) AnnotatedEventf(object runtime.Object, _ map[string]string, eventType, reason, messageFmt string, args ...any) {
	r.Eventf(object, eventType, reason, messageFmt, args...)
}

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
	vlan := &kubeovnv1.Vlan{ObjectMeta: metav1.ObjectMeta{Name: "vlan-a", UID: "vlan-uid"}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Vlans: []*kubeovnv1.Vlan{vlan}})
	require.NoError(t, err)
	fc.fakeController.vlanKeyMutex = keymutex.NewHashed(0)
	fc.fakeController.config.DefaultProviderName = "provider-a"
	recorder := &vlanEventRecorder{}
	fc.fakeController.recorder = recorder
	sourceErr := errors.New("update failed")
	kubeovnClient := fc.fakeController.config.KubeOvnClient.(*kubeovnfake.Clientset)
	kubeovnClient.PrependReactor("update", "vlans", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, sourceErr
	})

	err = fc.fakeController.handleAddVlan(vlan.Name)

	require.ErrorIs(t, err, sourceErr)
	require.Len(t, recorder.events, 1)
	event := recorder.events[0]
	require.Equal(t, corev1.EventTypeWarning, event.eventType)
	require.Equal(t, "UpdateSpecFailed", event.reason)
	require.Equal(t, sourceErr.Error(), event.message)
	target := event.object.(*kubeovnv1.Vlan)
	require.Equal(t, vlan.Name, target.Name)
	require.Equal(t, vlan.UID, target.UID)
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
	vlan := &kubeovnv1.Vlan{ObjectMeta: metav1.ObjectMeta{Name: vlanName, UID: "vlan-uid"}}
	pn := &kubeovnv1.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: pnName},
		Status:     kubeovnv1.ProviderNetworkStatus{Vlans: []string{vlanName}},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn},
	})
	require.NoError(t, err)
	fc.fakeController.vlanKeyMutex = keymutex.NewHashed(0)
	recorder := &vlanEventRecorder{}
	fc.fakeController.recorder = recorder

	err = fc.fakeController.handleDelVlan(vlan)

	require.NoError(t, err)
	require.Len(t, recorder.events, 1)
	event := recorder.events[0]
	require.Equal(t, corev1.EventTypeNormal, event.eventType)
	require.Equal(t, "DeleteSuccess", event.reason)
	require.Equal(t, "Vlan vlan-a deleted successfully", event.message)
	require.Equal(t, vlan.UID, event.object.(*kubeovnv1.Vlan).UID)
}

type deleteVlanRetryTestCase struct {
	name           string
	tombstone      bool
	failureStage   deleteVlanRetryFailureStage
	expectedReason string
}

type deleteVlanRetryFailureStage int

const (
	deleteVlanProviderNetworkUpdateFailure deleteVlanRetryFailureStage = iota
	deleteVlanSubnetListFailure
)

func runDeleteVlanQueueRetryTest(t *testing.T, tc deleteVlanRetryTestCase) {
	t.Helper()
	const (
		vlanName     = "vlan-a"
		deletedUID   = "deleted-vlan-uid"
		recreatedUID = "recreated-vlan-uid"
		pnName       = "provider-a"
	)
	deletedVlan := &kubeovnv1.Vlan{ObjectMeta: metav1.ObjectMeta{Name: vlanName, UID: deletedUID}}
	recreatedVlan := &kubeovnv1.Vlan{ObjectMeta: metav1.ObjectMeta{Name: vlanName, UID: recreatedUID}}
	options := &FakeControllerOptions{
		Vlans: []*kubeovnv1.Vlan{recreatedVlan},
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{{
			ObjectMeta: metav1.ObjectMeta{Name: pnName},
			Status:     kubeovnv1.ProviderNetworkStatus{Vlans: []string{vlanName}},
		}},
	}
	fc, err := newFakeControllerWithOptions(t, options)
	require.NoError(t, err)
	controller := fc.fakeController
	controller.vlanKeyMutex = keymutex.NewHashed(0)
	controller.delVlanQueue = newTypedRateLimitingQueue(
		"DeleteVlan",
		workqueue.NewTypedItemExponentialFailureRateLimiter[*kubeovnv1.Vlan](0, 0),
	)
	t.Cleanup(controller.delVlanQueue.ShutDown)
	recorder := &vlanEventRecorder{}
	controller.recorder = recorder

	sourceErr := errors.New(tc.expectedReason)
	updateAttempts := 0
	expectedUpdateAttempts := 0
	switch tc.failureStage {
	case deleteVlanProviderNetworkUpdateFailure:
		expectedUpdateAttempts = 2
		kubeovnClient := controller.config.KubeOvnClient.(*kubeovnfake.Clientset)
		kubeovnClient.PrependReactor("update", "provider-networks", func(k8stesting.Action) (bool, runtime.Object, error) {
			updateAttempts++
			if updateAttempts == 1 {
				return true, nil, sourceErr
			}
			return false, nil, nil
		})
	case deleteVlanSubnetListFailure:
		controller.subnetsLister = &transientErrorSubnetLister{
			SubnetLister: controller.subnetsLister,
			err:          sourceErr,
		}
	default:
		t.Fatalf("unknown VLAN deletion failure stage %d", tc.failureStage)
	}

	deleteEvent := any(deletedVlan)
	if tc.tombstone {
		deleteEvent = cache.DeletedFinalStateUnknown{Key: vlanName, Obj: deletedVlan}
	}
	controller.enqueueDelVlan(deleteEvent)
	require.Equal(t, 1, controller.delVlanQueue.Len())

	require.True(t, processNextWorkItem("delete vlan", controller.delVlanQueue, controller.handleDelVlan, getWorkItemKey))
	require.Equal(t, 1, controller.delVlanQueue.NumRequeues(deletedVlan))
	require.Len(t, recorder.events, 1)
	warning := recorder.events[0]
	require.Equal(t, corev1.EventTypeWarning, warning.eventType)
	require.Equal(t, tc.expectedReason, warning.reason)
	require.Equal(t, sourceErr.Error(), warning.message)
	require.Equal(t, deletedVlan.UID, warning.object.(*kubeovnv1.Vlan).UID)

	require.True(t, processNextWorkItem("delete vlan", controller.delVlanQueue, controller.handleDelVlan, getWorkItemKey))
	require.Zero(t, controller.delVlanQueue.NumRequeues(deletedVlan))
	require.Equal(t, expectedUpdateAttempts, updateAttempts)
	require.Len(t, recorder.events, 2)
	success := recorder.events[1]
	require.Equal(t, corev1.EventTypeNormal, success.eventType)
	require.Equal(t, "DeleteSuccess", success.reason)
	require.Equal(t, "Vlan vlan-a deleted successfully", success.message)
	target := success.object.(*kubeovnv1.Vlan)
	require.Equal(t, vlanName, target.Name)
	require.Equal(t, deletedVlan.UID, target.UID)
	require.NotEqual(t, recreatedVlan.UID, target.UID)
}

func TestDeleteVlanQueuePreservesEventIdentityAcrossRetry(t *testing.T) {
	for _, tc := range []deleteVlanRetryTestCase{
		{name: "direct object after provider network update failure", failureStage: deleteVlanProviderNetworkUpdateFailure, expectedReason: "UpdateProviderNetworkStatusFailed"},
		{name: "tombstone after provider network update failure", tombstone: true, failureStage: deleteVlanProviderNetworkUpdateFailure, expectedReason: "UpdateProviderNetworkStatusFailed"},
		{name: "tombstone after subnet list failure", tombstone: true, failureStage: deleteVlanSubnetListFailure, expectedReason: "ListSubnetsFailed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runDeleteVlanQueueRetryTest(t, tc)
		})
	}
}
