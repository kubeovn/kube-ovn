package controller

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func Test_handleUpdateIP_deletedSubnet(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	ip := &kubeovnv1.IP{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-ip",
			DeletionTimestamp: &now,
			Finalizers:        []string{util.KubeOVNControllerFinalizer},
		},
		Spec: kubeovnv1.IPSpec{
			Subnet:    "deleted-subnet",
			Namespace: "default",
			PodName:   "test-pod",
		},
	}

	fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		IPs: []*kubeovnv1.IP{ip},
	})
	require.NoError(t, err)

	ctrl := fakeCtrl.fakeController

	// Shut down work queues to avoid goroutine leaks
	t.Cleanup(func() {
		ctrl.updateSubnetStatusQueue.ShutDown()
		ctrl.syncVirtualPortsQueue.ShutDown()
	})

	// The subnet "deleted-subnet" does not exist in the fake client.
	// This must not panic (previously caused NPE in isOvnSubnet).
	err = ctrl.handleUpdateIP("test-ip")
	require.NoError(t, err)

	// Verify the subnet status update was enqueued
	require.Equal(t, 1, ctrl.updateSubnetStatusQueue.Len())
}

func TestHandleAddReservedIPRecordsFailureEvent(t *testing.T) {
	t.Parallel()

	ip := &kubeovnv1.IP{ObjectMeta: metav1.ObjectMeta{Name: "test-ip"}}
	fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		IPs: []*kubeovnv1.IP{ip},
	})
	require.NoError(t, err)

	err = fakeCtrl.fakeController.handleAddReservedIP(ip.Name)

	require.EqualError(t, err, "subnet parameter cannot be empty")
	require.Equal(t, "Warning AddIPFailed subnet parameter cannot be empty", requireRecorderEvent(t, fakeCtrl.fakeController.recorder.(*record.FakeRecorder)))
}

func TestHandleUpdateIPRecordsFailureEvent(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	ip := &kubeovnv1.IP{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-ip",
			DeletionTimestamp: &now,
			Finalizers:        []string{util.KubeOVNControllerFinalizer},
		},
		Spec: kubeovnv1.IPSpec{Subnet: "test-subnet"},
	}
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: ip.Spec.Subnet},
		Spec:       kubeovnv1.SubnetSpec{Provider: util.OvnProvider},
	}
	fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		IPs:     []*kubeovnv1.IP{ip},
		Subnets: []*kubeovnv1.Subnet{subnet},
	})
	require.NoError(t, err)
	injectedErr := errors.New("get logical switch port failed")
	fakeCtrl.mockOvnClient.EXPECT().GetLogicalSwitchPort(ip.Name, true).Return(nil, injectedErr)

	err = fakeCtrl.fakeController.handleUpdateIP(ip.Name)

	require.ErrorIs(t, err, injectedErr)
	require.Equal(t, "Warning UpdateIPFailed "+injectedErr.Error(), requireRecorderEvent(t, fakeCtrl.fakeController.recorder.(*record.FakeRecorder)))
}

func TestEnqueueUpdateIPRecordsImmutableFieldChangeEvent(t *testing.T) {
	original := &kubeovnv1.IP{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ip"},
		Spec: kubeovnv1.IPSpec{
			Subnet:      "subnet-a",
			Namespace:   "default",
			PodName:     "pod-a",
			PodType:     util.KindStatefulSet,
			MacAddress:  "00:00:00:AA:BB:CC",
			V4IPAddress: "10.0.0.2",
			V6IPAddress: "fd00::2",
		},
	}
	tests := []struct {
		name    string
		mutate  func(*kubeovnv1.IP)
		message string
	}{
		{name: "namespace", mutate: func(ip *kubeovnv1.IP) { ip.Spec.Namespace = "other" }, message: "ip test-ip namespace can not change"},
		{name: "pod name", mutate: func(ip *kubeovnv1.IP) { ip.Spec.PodName = "pod-b" }, message: "ip test-ip podName can not change"},
		{name: "pod type", mutate: func(ip *kubeovnv1.IP) { ip.Spec.PodType = util.KindVirtualMachine }, message: "ip test-ip podType can not change"},
		{name: "MAC address", mutate: func(ip *kubeovnv1.IP) { ip.Spec.MacAddress = "00:00:00:DD:EE:FF" }, message: "ip test-ip macAddress can not change"},
		{name: "IPv4 address", mutate: func(ip *kubeovnv1.IP) { ip.Spec.V4IPAddress = "10.0.0.3" }, message: "ip test-ip v4IPAddress can not change"},
		{name: "uppercase IPv6 address", mutate: func(ip *kubeovnv1.IP) { ip.Spec.V6IPAddress = "FD00::2" }, message: "ip test-ip v6 ip address FD00::2 can not contain upper case"},
		{name: "IPv6 address", mutate: func(ip *kubeovnv1.IP) { ip.Spec.V6IPAddress = "fd00::3" }, message: "ip test-ip v6IPAddress can not change"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated := original.DeepCopy()
			tt.mutate(updated)
			recorder := record.NewFakeRecorder(1)
			controller := &Controller{recorder: recorder}

			controller.enqueueUpdateIP(original, updated)

			require.Equal(t, "Warning UpdateIPFailed "+tt.message, requireRecorderEvent(t, recorder))
		})
	}
}
