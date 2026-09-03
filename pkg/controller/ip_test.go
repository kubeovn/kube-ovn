package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestNotifyDeletedIPParents(t *testing.T) {
	now := metav1.Now()
	primary := &kubeovnv1.Subnet{Name: "primary", DeletionTimestamp: &now}
	attach := &kubeovnv1.Subnet{Name: "attach", DeletionTimestamp: &now}
	fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{primary, attach},
	})
	require.NoError(t, err)
	ctrl := fakeCtrl.fakeController
	ctrl.deleteSubnetQueue = newTypedRateLimitingQueue[*kubeovnv1.Subnet]("DeleteSubnet", nil)
	t.Cleanup(ctrl.deleteSubnetQueue.ShutDown)

	ctrl.notifyDeletedIPParents(&kubeovnv1.IP{Spec: kubeovnv1.IPSpec{
		Subnet:        primary.Name,
		AttachSubnets: []string{attach.Name},
	}})
	require.Equal(t, 2, ctrl.deleteSubnetQueue.Len())
}

func Test_handleUpdateIP_deletedSubnet(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	ip := &kubeovnv1.IP{
		Name:              "test-ip",
		DeletionTimestamp: &now,
		Finalizers:        []string{util.KubeOVNControllerFinalizer},
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
