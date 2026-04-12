package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
