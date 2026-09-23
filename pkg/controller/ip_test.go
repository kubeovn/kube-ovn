package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

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

func TestCreateOrUpdateIPCRPersistsReplacementNodeOwner(t *testing.T) {
	t.Parallel()

	// a replacement node with the same name reuses the node ip CR: only the owner uid differs
	node := &corev1.Node{Name: "node-a", UID: "new-uid"}
	fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Nodes: []*corev1.Node{node}})
	require.NoError(t, err)
	ctrl := fakeCtrl.fakeController

	subnet := ctrl.config.NodeSwitch
	joinIP, mac := "100.64.0.5", "00:00:00:00:00:01"
	ip := &kubeovnv1.IP{
		Name:       util.NodeLspName(node.Name),
		Labels:     map[string]string{util.SubnetNameLabel: subnet, util.NodeNameLabel: node.Name, subnet: ""},
		Finalizers: []string{util.KubeOVNControllerFinalizer},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: corev1.SchemeGroupVersion.String(), Kind: util.KindNode, Name: node.Name, UID: "old-uid",
		}},
		Spec: kubeovnv1.IPSpec{
			PodName: node.Name, NodeName: node.Name, Subnet: subnet, IPAddress: joinIP, V4IPAddress: joinIP, MacAddress: mac,
			AttachIPs: []string{}, AttachMacs: []string{}, AttachSubnets: []string{},
		},
	}
	_, err = ctrl.config.KubeOvnClient.KubeovnV1().IPs().Create(context.Background(), ip, metav1.CreateOptions{})
	require.NoError(t, err)
	require.NoError(t, fakeCtrl.fakeInformers.ipInformer.Informer().GetIndexer().Add(ip))

	require.NoError(t, ctrl.createOrUpdateIPCR("", "", joinIP, mac, subnet, "", node.Name, ""))

	updated, err := ctrl.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), ip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Len(t, updated.OwnerReferences, 1)
	require.Equal(t, node.UID, updated.OwnerReferences[0].UID, "the node ip CR must be owned by the replacement node")
}
