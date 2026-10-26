package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func newProviderNetworkCleanupClient(t *testing.T, pn *kubeovnv1.ProviderNetwork) *kubeovnfake.Clientset {
	t.Helper()

	// Seed through the API so the tracker uses the CRD's hyphenated resource name.
	client := kubeovnfake.NewSimpleClientset()
	_, err := client.KubeovnV1().ProviderNetworks().Create(context.Background(), pn, metav1.CreateOptions{})
	require.NoError(t, err)
	return client
}

func providerNetworkCleanupTestObject() *kubeovnv1.ProviderNetwork {
	return &kubeovnv1.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "provider", UID: "provider-uid"},
		Spec: kubeovnv1.ProviderNetworkSpec{
			DefaultInterface: "eth0.100", AutoCreateVlanSubinterfaces: true, DefaultInterfaceSubnet: "underlay",
		},
		Status: kubeovnv1.ProviderNetworkStatus{
			VlanSubinterfaceAddresses: []kubeovnv1.ProviderNetworkNodeAddress{{
				Node: "node", Subnet: "underlay", IPAddress: "192.0.2.2",
				IPName: "pn.provider.node", IPUID: "ip-uid", Phase: kubeovnv1.ProviderNetworkAddressCleanupPending,
			}},
		},
	}
}

func TestAcknowledgeProviderNetworkCleanup(t *testing.T) {
	pn := providerNetworkCleanupTestObject()
	other := pn.Status.VlanSubinterfaceAddresses[0]
	other.Node, other.IPName, other.IPUID = "other-node", "other-ip", "other-uid"
	pn.Status.VlanSubinterfaceAddresses = append(pn.Status.VlanSubinterfaceAddresses, other)
	client := newProviderNetworkCleanupClient(t, pn)
	ctrl := &Controller{config: &Configuration{NodeName: "node", KubeOvnClient: client}}
	ctx := context.Background()
	address := pn.Status.VlanSubinterfaceAddresses[0]

	require.NoError(t, ctrl.acknowledgeProviderNetworkAddressCleanup(pn, address, "netlink cleanup failed"))
	current, err := client.KubeovnV1().ProviderNetworks().Get(ctx, pn.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, kubeovnv1.ProviderNetworkAddressCleanupPending, current.Status.VlanSubinterfaceAddresses[0].Phase)
	require.Equal(t, "netlink cleanup failed", current.Status.VlanSubinterfaceAddresses[0].Message)
	require.Empty(t, current.Status.VlanSubinterfaceAddresses[0].CleanupAcknowledgedIPUID)
	require.Equal(t, other, current.Status.VlanSubinterfaceAddresses[1])

	require.NoError(t, ctrl.acknowledgeProviderNetworkAddressCleanup(pn, address, ""))
	current, err = client.KubeovnV1().ProviderNetworks().Get(ctx, pn.Name, metav1.GetOptions{})
	require.NoError(t, err)
	expected := address
	expected.CleanupAcknowledgedIPUID = address.IPUID
	require.Equal(t, expected, current.Status.VlanSubinterfaceAddresses[0], "daemon must not change controller-owned fields")
	require.Equal(t, other, current.Status.VlanSubinterfaceAddresses[1])

	// A stale failed retry cannot revoke an acknowledgement or replace its message.
	require.NoError(t, ctrl.acknowledgeProviderNetworkAddressCleanup(pn, address, "late failure"))
	stored, err := client.KubeovnV1().ProviderNetworks().Get(ctx, pn.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, current.Status, stored.Status)
	// While the controller has not yet advanced phase, do not repeat local cleanup.
	skipInitialization, err := ctrl.processProviderNetworkAddressCleanup(current, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}})
	require.NoError(t, err)
	require.True(t, skipInitialization)
}

func TestProviderNetworkCleanupRetriesStatusConflict(t *testing.T) {
	pn := providerNetworkCleanupTestObject()
	other := pn.Status.VlanSubinterfaceAddresses[0]
	other.Node, other.IPName, other.IPUID = "other-node", "other-ip", "other-uid"
	pn.Status.VlanSubinterfaceAddresses = append(pn.Status.VlanSubinterfaceAddresses, other)
	client := newProviderNetworkCleanupClient(t, pn)
	updates := 0
	client.PrependReactor("update", "provider-networks", func(action clienttesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "status" {
			return false, nil, nil
		}
		updates++
		if updates != 1 {
			return false, nil, nil
		}
		concurrent := pn.DeepCopy()
		concurrent.Status.VlanSubinterfaceAddresses[1].Message = "other daemon reported an error"
		resource := kubeovnv1.SchemeGroupVersion.WithResource("provider-networks")
		require.NoError(t, client.Tracker().Update(resource, concurrent, ""))
		return true, nil, k8serrors.NewConflict(resource.GroupResource(), pn.Name, errors.New("concurrent status update"))
	})
	ctrl := &Controller{config: &Configuration{NodeName: "node", KubeOvnClient: client}}
	require.NoError(t, ctrl.acknowledgeProviderNetworkAddressCleanup(pn, pn.Status.VlanSubinterfaceAddresses[0], ""))
	require.Equal(t, 2, updates)
	stored, err := client.KubeovnV1().ProviderNetworks().Get(context.Background(), pn.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, kubeovnv1.ProviderNetworkAddressCleanupPending, stored.Status.VlanSubinterfaceAddresses[0].Phase)
	require.Equal(t, "ip-uid", stored.Status.VlanSubinterfaceAddresses[0].CleanupAcknowledgedIPUID)
	require.Equal(t, "other daemon reported an error", stored.Status.VlanSubinterfaceAddresses[1].Message)
}

func TestProviderNetworkCleanupRejectsStaleAcknowledgement(t *testing.T) {
	pn := providerNetworkCleanupTestObject()
	client := newProviderNetworkCleanupClient(t, pn)
	ctrl := &Controller{config: &Configuration{NodeName: "node", KubeOvnClient: client}}
	address := pn.Status.VlanSubinterfaceAddresses[0]
	address.IPUID = "previous-ip-uid"
	require.Error(t, ctrl.acknowledgeProviderNetworkAddressCleanup(pn, address, ""))
	current, err := client.KubeovnV1().ProviderNetworks().Get(context.Background(), pn.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, pn.Status, current.Status)
}

func TestProviderNetworkStaleCleanupDoesNotTouchNewAllocation(t *testing.T) {
	cached := providerNetworkCleanupTestObject()
	current := cached.DeepCopy()
	current.Status.VlanSubinterfaceAddresses[0].IPUID = "new-ip-uid"
	current.Status.VlanSubinterfaceAddresses[0].Phase = kubeovnv1.ProviderNetworkAddressAllocated
	client := newProviderNetworkCleanupClient(t, current)
	ctrl := &Controller{config: &Configuration{NodeName: "node", KubeOvnClient: client}}
	// No netlink or Kubernetes node client is needed: the stale request must be ignored.
	skipInitialization, err := ctrl.processProviderNetworkAddressCleanup(cached, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}})
	require.NoError(t, err)
	require.True(t, skipInitialization)
	stored, err := client.KubeovnV1().ProviderNetworks().Get(context.Background(), current.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, current.Status, stored.Status)
}

func TestProviderNetworkCleanupWithoutStatusFailsClosed(t *testing.T) {
	pn := providerNetworkCleanupTestObject()
	pn.Status.VlanSubinterfaceAddresses = nil
	client := newProviderNetworkCleanupClient(t, pn)
	apiErr := errors.New("IP API unavailable")
	client.PrependReactor("get", "ips", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apiErr
	})
	ctrl := &Controller{config: &Configuration{NodeName: "node", KubeOvnClient: client}}
	err := ctrl.cleanupProviderNetworkVlanSubinterfaceAddress(pn, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}})
	require.ErrorIs(t, err, apiErr)
}

func TestProviderNetworkRejectsEmptyAddressAndInvalidCIDR(t *testing.T) {
	for _, tt := range []struct {
		name      string
		ip        string
		cidr      string
		wantError string
	}{
		{name: "MAC-only allocation", wantError: "empty IP address"},
		{name: "empty address", cidr: "192.0.2.0/24", wantError: "empty IP address"},
		{name: "MAC-only subnet", ip: "192.0.2.2", wantError: `invalid CIDR ""`},
		{name: "malformed CIDR", ip: "192.0.2.2", cidr: "not-a-cidr", wantError: "invalid CIDR"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pn := providerNetworkCleanupTestObject()
			allocation := &pn.Status.VlanSubinterfaceAddresses[0]
			allocation.Phase = kubeovnv1.ProviderNetworkAddressAllocated
			allocation.IPAddress = tt.ip
			client := newProviderNetworkCleanupClient(t, pn)
			_, err := client.KubeovnV1().IPs().Create(context.Background(), &kubeovnv1.IP{
				ObjectMeta: metav1.ObjectMeta{Name: allocation.IPName, UID: "ip-uid"},
				Spec: kubeovnv1.IPSpec{
					NodeName: allocation.Node, Subnet: allocation.Subnet, IPAddress: tt.ip, PodType: util.KindProviderNetwork,
				},
			}, metav1.CreateOptions{})
			require.NoError(t, err)
			subnetIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			require.NoError(t, subnetIndexer.Add(&kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: allocation.Subnet},
				Spec:       kubeovnv1.SubnetSpec{CIDRBlock: tt.cidr},
			}))
			ctrl := &Controller{
				config:        &Configuration{NodeName: allocation.Node, KubeOvnClient: client},
				subnetsLister: kubeovnlisters.NewSubnetLister(subnetIndexer),
			}
			require.NotPanics(t, func() {
				err = ctrl.configureProviderNetworkVlanSubinterfaceAddress(pn, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: allocation.Node}})
			})
			require.ErrorContains(t, err, tt.wantError)
		})
	}
}

func TestProviderNetworkStaleAllocationIsNotReapplied(t *testing.T) {
	current := providerNetworkCleanupTestObject()
	cached := current.DeepCopy()
	cached.Status.VlanSubinterfaceAddresses[0].Phase = kubeovnv1.ProviderNetworkAddressAllocated
	client := newProviderNetworkCleanupClient(t, current)
	ctrl := &Controller{config: &Configuration{NodeName: "node", KubeOvnClient: client}}
	err := ctrl.configureProviderNetworkVlanSubinterfaceAddress(cached, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}})
	require.ErrorContains(t, err, "no longer active")
}
