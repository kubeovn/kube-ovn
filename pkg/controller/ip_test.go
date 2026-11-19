package controller

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ktesting "k8s.io/client-go/testing"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
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

func TestCreateOrUpdateIPCREnqueuesSubnetStatus(t *testing.T) {
	t.Parallel()

	const subnet, podIP, oldMac, newMac = "subnet-a", "10.16.0.3", "00:00:00:00:00:01", "00:00:00:00:00:02"
	for _, tt := range []struct {
		name        string
		recordedMac string
		wantUpdate  bool
	}{
		// a pod re-allocated after a controller restart gets a freshly generated MAC, and the
		// update handler rejects the resulting ip CR change as an immutable field change
		{name: "immutable field change rejected by the update handler", recordedMac: oldMac, wantUpdate: true},
		// nothing changed, so no update and no watch event follow
		{name: "ip CR already up to date", recordedMac: newMac},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ip := &kubeovnv1.IP{
				Name:       "pod-a.default",
				Labels:     map[string]string{util.SubnetNameLabel: subnet, util.NodeNameLabel: "node-a", subnet: "", util.IPReservedLabel: "false"},
				Finalizers: []string{util.KubeOVNControllerFinalizer},
				Spec: kubeovnv1.IPSpec{
					PodName: "pod-a", Namespace: "default", NodeName: "node-a", Subnet: subnet,
					IPAddress: podIP, V4IPAddress: podIP, MacAddress: tt.recordedMac,
					AttachIPs: []string{}, AttachMacs: []string{}, AttachSubnets: []string{},
				},
			}
			fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{IPs: []*kubeovnv1.IP{ip}})
			require.NoError(t, err)
			ctrl := fakeCtrl.fakeController
			t.Cleanup(ctrl.updateSubnetStatusQueue.ShutDown)
			client := ctrl.config.KubeOvnClient.(*kubeovnfake.Clientset)
			client.ClearActions()

			require.NoError(t, ctrl.createOrUpdateIPCR(ip.Name, "pod-a", podIP, newMac, subnet, "default", "node-a", ""))

			updated := slices.ContainsFunc(client.Actions(), func(action ktesting.Action) bool {
				return action.Matches("update", "ips")
			})
			require.Equal(t, tt.wantUpdate, updated, "the test must exercise the intended branch")
			require.Equal(t, 1, ctrl.updateSubnetStatusQueue.Len(), "the subnet status must be refreshed")
			key, shutdown := ctrl.updateSubnetStatusQueue.Get()
			require.False(t, shutdown)
			require.Equal(t, subnet, key)
			ctrl.updateSubnetStatusQueue.Done(key)
		})
	}
}
