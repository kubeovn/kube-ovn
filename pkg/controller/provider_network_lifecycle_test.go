package controller

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestProviderNetworkAddressesIgnoreOrder(t *testing.T) {
	for _, phase := range []kubeovnv1.ProviderNetworkAddressPhase{
		kubeovnv1.ProviderNetworkAddressAllocated, kubeovnv1.ProviderNetworkAddressCleanupPending,
	} {
		t.Run(string(phase), func(t *testing.T) {
			pn, _, baseIP := providerNetworkAddressTestObjects()
			pn.Status.VlanSubinterfaceAddresses = nil
			var nodes []*corev1.Node
			var ips []*kubeovnv1.IP
			for _, nodeName := range []string{"node-z", "node-a"} {
				ip := baseIP.DeepCopy()
				ip.Name = util.ProviderNetworkAddressName(pn.Name, nodeName)
				ip.Spec.NodeName = nodeName
				if nodeName == "node-a" {
					ip.Spec.IPAddress = "192.0.2.3"
				}
				if phase == kubeovnv1.ProviderNetworkAddressCleanupPending {
					pn.Spec.AutoCreateVlanSubinterfaces = false
					ip.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				}
				ips = append(ips, ip)
				nodes = append(nodes, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})
				pn.Status.VlanSubinterfaceAddresses = append(pn.Status.VlanSubinterfaceAddresses, *providerNetworkNodeAddress(ip, phase))
			}
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn}, IPs: ips,
			})
			require.NoError(t, err)
			originalSlice := pn.Status.VlanSubinterfaceAddresses
			originalValues := slices.Clone(originalSlice)
			changed, err := fc.fakeController.reconcileProviderNetworkNodeAddresses(pn, nodes)
			require.NoError(t, err)
			require.False(t, changed, "order alone must not trigger an update")
			require.Equal(t, originalValues, originalSlice, "comparison must not mutate the original slice")
			require.Equal(t, "node-a", pn.Status.VlanSubinterfaceAddresses[0].Node)
			require.Equal(t, "node-z", pn.Status.VlanSubinterfaceAddresses[1].Node)

			slices.Reverse(nodes)
			changed, err = fc.fakeController.reconcileProviderNetworkNodeAddresses(pn, nodes)
			require.NoError(t, err)
			require.False(t, changed)
			// Sorting must not hide real allocation changes.
			client := fc.fakeController.config.KubeOvnClient.KubeovnV1().IPs()
			ip, err := client.Get(context.Background(), ips[1].Name, metav1.GetOptions{})
			require.NoError(t, err)
			ip.Spec.IPAddress = "192.0.2.4"
			_, err = client.Update(context.Background(), ip, metav1.UpdateOptions{})
			require.NoError(t, err)
			changed, err = fc.fakeController.reconcileProviderNetworkNodeAddresses(pn, nodes)
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, "192.0.2.4", pn.Status.VlanSubinterfaceAddresses[0].IPAddress)
		})
	}
}

func TestDeletingProviderNetworkWaitsForAddressCleanup(t *testing.T) {
	ctx := context.Background()
	pn, _, ip := providerNetworkAddressTestObjects()
	pn.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	ip.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	// Discover the allocation from the IP CR even if status was never persisted.
	pn.Status.VlanSubinterfaceAddresses = nil
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: ip.Spec.Subnet},
		Spec:       kubeovnv1.SubnetSpec{CIDRBlock: "192.0.2.0/29", Gateway: "192.0.2.1", Provider: "external"},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn}, IPs: []*kubeovnv1.IP{ip}, Subnets: []*kubeovnv1.Subnet{subnet},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
	_, _, _, err = ctrl.ipam.GetStaticAddress(ip.Name, ip.Name, ip.Spec.IPAddress, &ip.Spec.MacAddress, ip.Spec.Subnet, true)
	require.NoError(t, err)
	client := ctrl.config.KubeOvnClient.KubeovnV1()

	ctrl.syncProviderNetworkStatus(pn, nil, nil)
	pn, err = client.ProviderNetworks().Get(ctx, pn.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, pn.Finalizers, util.ProviderNetworkCleanupFinalizer)
	require.False(t, pn.Status.Ready)
	require.Len(t, pn.Status.VlanSubinterfaceAddresses, 1)
	address := pn.Status.VlanSubinterfaceAddresses[0]
	require.Equal(t, *providerNetworkNodeAddress(ip, kubeovnv1.ProviderNetworkAddressCleanupPending), address)
	require.Error(t, ctrl.handleUpdateIP(ip.Name))
	require.True(t, ctrl.ipam.ContainAddress(ip.Spec.IPAddress))

	// The daemon only records its observation; the controller owns phase changes.
	pn.Status.VlanSubinterfaceAddresses[0].CleanupAcknowledgedIPUID = string(ip.UID)
	pn, err = client.ProviderNetworks().UpdateStatus(ctx, pn, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.Equal(t, kubeovnv1.ProviderNetworkAddressCleanupPending, pn.Status.VlanSubinterfaceAddresses[0].Phase)
	require.Error(t, ctrl.handleUpdateIP(ip.Name), "acknowledgement alone must not bypass phase promotion")
	ctrl.syncProviderNetworkStatus(pn, nil, nil)
	pn, err = client.ProviderNetworks().Get(ctx, pn.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, kubeovnv1.ProviderNetworkAddressCleanupSucceeded, pn.Status.VlanSubinterfaceAddresses[0].Phase)
	require.Equal(t, string(ip.UID), pn.Status.VlanSubinterfaceAddresses[0].CleanupAcknowledgedIPUID)
	require.Contains(t, pn.Finalizers, util.ProviderNetworkCleanupFinalizer)
	require.NoError(t, ctrl.handleUpdateIP(ip.Name))
	require.False(t, ctrl.ipam.ContainAddress(ip.Spec.IPAddress))

	// Fake clients do not automatically complete deletion when the finalizer is removed.
	require.NoError(t, client.IPs().Delete(ctx, ip.Name, metav1.DeleteOptions{}))
	ctrl.syncProviderNetworkStatus(pn, nil, nil)
	pn, err = client.ProviderNetworks().Get(ctx, pn.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, pn.Status.VlanSubinterfaceAddresses)
	require.NotContains(t, pn.Finalizers, util.ProviderNetworkCleanupFinalizer)
}

func TestProviderNetworkFinalizerWaitsForUncachedIP(t *testing.T) {
	pn, _, ip := providerNetworkAddressTestObjects()
	pn.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	pn.Status.VlanSubinterfaceAddresses = nil
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn}, IPs: []*kubeovnv1.IP{ip},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	// Simulate an allocation that the API has persisted but the informer has not observed.
	ctrl.ipIndexer = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{IndexIPByProviderNetworkUID: indexIPByProviderNetworkUID})
	ctrl.ipsLister = kubeovnlisters.NewIPLister(ctrl.ipIndexer)
	ctrl.syncProviderNetworkStatus(pn, nil, nil)
	stored, err := ctrl.config.KubeOvnClient.KubeovnV1().ProviderNetworks().Get(context.Background(), pn.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, stored.Finalizers, util.ProviderNetworkCleanupFinalizer)
	require.Empty(t, stored.Status.VlanSubinterfaceAddresses)
}

func TestProviderNetworkCleanupPhaseUsesDaemonAcknowledgement(t *testing.T) {
	for _, tt := range []struct {
		name      string
		phase     kubeovnv1.ProviderNetworkAddressPhase
		ackUID    string
		replaced  bool
		wantPhase kubeovnv1.ProviderNetworkAddressPhase
	}{
		{name: "pending", phase: kubeovnv1.ProviderNetworkAddressCleanupPending, wantPhase: kubeovnv1.ProviderNetworkAddressCleanupPending},
		{name: "confirmed", phase: kubeovnv1.ProviderNetworkAddressCleanupPending, ackUID: "ip-uid", wantPhase: kubeovnv1.ProviderNetworkAddressCleanupSucceeded},
		{name: "phase is not proof", phase: kubeovnv1.ProviderNetworkAddressCleanupSucceeded, wantPhase: kubeovnv1.ProviderNetworkAddressCleanupPending},
		{name: "stale acknowledgement", phase: kubeovnv1.ProviderNetworkAddressCleanupPending, ackUID: "previous-ip-uid", wantPhase: kubeovnv1.ProviderNetworkAddressCleanupPending},
		{name: "replacement allocation", phase: kubeovnv1.ProviderNetworkAddressCleanupSucceeded, ackUID: "ip-uid", replaced: true, wantPhase: kubeovnv1.ProviderNetworkAddressCleanupPending},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pn, _, ip := providerNetworkAddressTestObjects()
			previous := providerNetworkNodeAddress(ip, tt.phase)
			previous.CleanupAcknowledgedIPUID = tt.ackUID
			previous.Message = "daemon observation"
			pn.Status.VlanSubinterfaceAddresses = []kubeovnv1.ProviderNetworkNodeAddress{*previous}
			if tt.replaced {
				ip.UID = "replacement-ip-uid"
			}
			address := providerNetworkCleanupAddress(pn, ip)
			require.Equal(t, tt.wantPhase, address.Phase)
			if tt.replaced {
				require.Empty(t, address.CleanupAcknowledgedIPUID)
				require.Empty(t, address.Message)
			} else {
				require.Equal(t, tt.ackUID, address.CleanupAcknowledgedIPUID)
				require.Equal(t, previous.Message, address.Message)
			}
		})
	}
}

func TestProviderNetworkAddressAllocatesWithLongName(t *testing.T) {
	pn, node, _ := providerNetworkAddressTestObjects()
	pn.Name = strings.Repeat("p", 253)
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet("underlay", "192.0.2.0/29", "192.0.2.1", nil))
	address, err := ctrl.reconcileProviderNetworkNodeAddress(pn, node.Name)
	require.NoError(t, err)
	require.Equal(t, kubeovnv1.ProviderNetworkAddressAllocated, address.Phase)
	require.LessOrEqual(t, len(address.IPName), 253)
	ip, err := ctrl.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), address.IPName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, pn.Name, ip.OwnerReferences[0].Name)
	require.Equal(t, address.IPAddress, ip.Spec.IPAddress)
}

func TestProviderNetworkAddressRejectsInvalidNameBeforeAllocation(t *testing.T) {
	pn, _, _ := providerNetworkAddressTestObjects()
	_, err := (&Controller{}).reconcileProviderNetworkNodeAddress(pn, "invalid_node")
	require.ErrorContains(t, err, "invalid provider network IP name")
}

func TestProviderNetworkCleanupCannotBeCancelledBySpecChange(t *testing.T) {
	pn, node, ip := providerNetworkAddressTestObjects()
	pn.Status.VlanSubinterfaceAddresses = []kubeovnv1.ProviderNetworkNodeAddress{
		*providerNetworkNodeAddress(ip, kubeovnv1.ProviderNetworkAddressCleanupPending),
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn}, IPs: []*kubeovnv1.IP{ip},
	})
	require.NoError(t, err)
	// The node and subnet are desired again, but cleanup must finish before reuse.
	address, err := fc.fakeController.reconcileProviderNetworkNodeAddress(pn, node.Name)
	require.NoError(t, err)
	require.Equal(t, kubeovnv1.ProviderNetworkAddressCleanupPending, address.Phase)
	require.Equal(t, string(ip.UID), address.IPUID)
}
