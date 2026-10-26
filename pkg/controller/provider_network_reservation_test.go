package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type pendingProviderNetworkFixture struct {
	ctrl        *Controller
	pn          *kubeovnv1.ProviderNetwork
	node        *corev1.Node
	client      *kubeovnfake.Clientset
	createErr   error
	reservation kubeovnv1.ProviderNetworkNodeAddress
}

func newPendingProviderNetworkFixture(t *testing.T, createErr error) *pendingProviderNetworkFixture {
	t.Helper()
	pn, node, _ := providerNetworkAddressTestObjects()
	pn.Status.VlanSubinterfaceAddresses = nil
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn},
	})
	require.NoError(t, err)
	f := &pendingProviderNetworkFixture{
		ctrl: fc.fakeController, pn: pn, node: node,
		client: fc.fakeController.config.KubeOvnClient.(*kubeovnfake.Clientset), createErr: createErr,
	}
	require.NoError(t, f.ctrl.ipam.AddOrUpdateSubnet(pn.Spec.DefaultInterfaceSubnet, "192.0.2.0/29", "192.0.2.1", []string{"192.0.2.1"}))
	f.client.PrependReactor("create", "ips", func(clienttesting.Action) (bool, runtime.Object, error) {
		if f.createErr != nil {
			return true, nil, f.createErr
		}
		return false, nil, nil
	})
	_, err = f.ctrl.reconcileProviderNetworkNodeAddresses(pn, []*corev1.Node{node})
	require.ErrorIs(t, err, createErr)
	require.Empty(t, pn.Status.VlanSubinterfaceAddresses)
	key := providerNetworkReservationKey{providerNetworkUID: string(pn.UID), node: node.Name, subnet: pn.Spec.DefaultInterfaceSubnet}
	value, ok := f.ctrl.providerNetworkPendingIPs.Load(key)
	require.True(t, ok)
	f.reservation = value.(kubeovnv1.ProviderNetworkNodeAddress)
	require.True(t, f.ctrl.ipam.ContainAddress(f.reservation.IPAddress))
	return f
}

func TestProviderNetworkPendingReservationReleasedWhenNoLongerDesired(t *testing.T) {
	for _, reason := range []string{"excluded node", "deleted node", "disabled allocation", "custom interface"} {
		t.Run(reason, func(t *testing.T) {
			f := newPendingProviderNetworkFixture(t, context.DeadlineExceeded)
			nodes := []*corev1.Node{f.node}
			switch reason {
			case "excluded node":
				f.pn.Spec.ExcludeNodes = []string{f.node.Name}
			case "deleted node":
				nodes = nil
			case "disabled allocation":
				f.pn.Spec.AutoCreateVlanSubinterfaces = false
			case "custom interface":
				f.pn.Spec.CustomInterfaces = []kubeovnv1.CustomInterface{{Interface: "eth1.100", Nodes: []string{f.node.Name}}}
			}
			_, err := f.ctrl.reconcileProviderNetworkNodeAddresses(f.pn, nodes)
			require.NoError(t, err)
			require.False(t, f.ctrl.ipam.ContainAddress(f.reservation.IPAddress))
			require.Empty(t, f.ctrl.ipam.GetPodAddress(f.reservation.IPName))
			require.Empty(t, f.pn.Status.VlanSubinterfaceAddresses)
			_, ok := f.ctrl.providerNetworkPendingIPs.Load(providerNetworkReservationKey{
				providerNetworkUID: string(f.pn.UID), node: f.node.Name, subnet: f.reservation.Subnet,
			})
			require.False(t, ok)
		})
	}
}

func TestProviderNetworkPendingReservationReusedOnRetry(t *testing.T) {
	f := newPendingProviderNetworkFixture(t, errors.New("injected create failure"))
	_, err := f.ctrl.reconcileProviderNetworkNodeAddresses(f.pn, []*corev1.Node{f.node})
	require.ErrorIs(t, err, f.createErr)
	addresses := f.ctrl.ipam.GetPodAddress(f.reservation.IPName)
	require.Len(t, addresses, 1)
	require.Equal(t, f.reservation.IPAddress, addresses[0].IP)

	f.createErr = nil
	_, err = f.ctrl.reconcileProviderNetworkNodeAddresses(f.pn, []*corev1.Node{f.node})
	require.NoError(t, err)
	require.Len(t, f.pn.Status.VlanSubinterfaceAddresses, 1)
	require.Equal(t, f.reservation.IPAddress, f.pn.Status.VlanSubinterfaceAddresses[0].IPAddress)
	_, ok := f.ctrl.providerNetworkPendingIPs.Load(providerNetworkReservationKey{
		providerNetworkUID: string(f.pn.UID), node: f.node.Name, subnet: f.reservation.Subnet,
	})
	require.False(t, ok, "persisted IP CR takes over the reservation lifecycle")
}

func TestProviderNetworkPendingReservationWaitsForSuccessfulAPIRead(t *testing.T) {
	f := newPendingProviderNetworkFixture(t, context.DeadlineExceeded)
	f.pn.Spec.ExcludeNodes = []string{f.node.Name}
	readErr := errors.New("IP API unavailable")
	f.client.PrependReactor("get", "ips", func(clienttesting.Action) (bool, runtime.Object, error) {
		if readErr != nil {
			return true, nil, readErr
		}
		return false, nil, nil
	})
	_, err := f.ctrl.reconcileProviderNetworkNodeAddresses(f.pn, []*corev1.Node{f.node})
	require.ErrorIs(t, err, readErr)
	require.True(t, f.ctrl.ipam.ContainAddress(f.reservation.IPAddress))

	readErr = nil
	_, err = f.ctrl.reconcileProviderNetworkNodeAddresses(f.pn, []*corev1.Node{f.node})
	require.NoError(t, err)
	require.False(t, f.ctrl.ipam.ContainAddress(f.reservation.IPAddress))
}

func TestProviderNetworkPendingReservationReleasedOnSubnetChange(t *testing.T) {
	f := newPendingProviderNetworkFixture(t, errors.New("injected create failure"))
	f.pn.Spec.DefaultInterfaceSubnet = "new-underlay"
	require.NoError(t, f.ctrl.ipam.AddOrUpdateSubnet("new-underlay", "198.51.100.0/29", "198.51.100.1", []string{"198.51.100.1"}))
	f.createErr = nil
	_, err := f.ctrl.reconcileProviderNetworkNodeAddresses(f.pn, []*corev1.Node{f.node})
	require.NoError(t, err)
	require.False(t, f.ctrl.ipam.ContainAddress(f.reservation.IPAddress))
	require.Len(t, f.pn.Status.VlanSubinterfaceAddresses, 1)
	address := f.pn.Status.VlanSubinterfaceAddresses[0]
	require.Equal(t, "new-underlay", address.Subnet)
	require.True(t, f.ctrl.ipam.ContainAddress(address.IPAddress))
}

func TestProviderNetworkPendingReservationDoesNotBypassPersistedIPCleanup(t *testing.T) {
	f := newPendingProviderNetworkFixture(t, context.DeadlineExceeded)
	// Simulate a successful create whose response was lost. Cleanup must use the CR handshake.
	f.createErr = nil
	_, err := f.client.KubeovnV1().IPs().Create(context.Background(), &kubeovnv1.IP{
		ObjectMeta: metav1.ObjectMeta{
			Name: f.reservation.IPName, UID: "persisted-ip-uid", Finalizers: []string{util.KubeOVNControllerFinalizer},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: kubeovnv1.SchemeGroupVersion.String(), Kind: util.KindProviderNetwork, Name: f.pn.Name, UID: f.pn.UID,
			}},
		},
		Spec: kubeovnv1.IPSpec{
			NodeName: f.node.Name, Subnet: f.reservation.Subnet, IPAddress: f.reservation.IPAddress, PodType: util.KindProviderNetwork,
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	f.pn.Spec.ExcludeNodes = []string{f.node.Name}
	_, err = f.ctrl.reconcileProviderNetworkNodeAddresses(f.pn, []*corev1.Node{f.node})
	require.NoError(t, err)
	require.True(t, f.ctrl.ipam.ContainAddress(f.reservation.IPAddress))
	require.Len(t, f.pn.Status.VlanSubinterfaceAddresses, 1)
	address := f.pn.Status.VlanSubinterfaceAddresses[0]
	require.Equal(t, kubeovnv1.ProviderNetworkAddressCleanupPending, address.Phase)
	require.Equal(t, "persisted-ip-uid", address.IPUID)
}

func TestProviderNetworkPendingReservationIsScopedToOwnerUID(t *testing.T) {
	f := newPendingProviderNetworkFixture(t, context.DeadlineExceeded)
	replacement := f.pn.DeepCopy()
	replacement.UID = "replacement-provider-uid"
	_, err := f.ctrl.reconcileProviderNetworkNodeAddresses(replacement, nil)
	require.NoError(t, err)
	require.True(t, f.ctrl.ipam.ContainAddress(f.reservation.IPAddress), "a different owner must not release the reservation")

	f.pn.Spec.ExcludeNodes = []string{f.node.Name}
	_, err = f.ctrl.reconcileProviderNetworkNodeAddresses(f.pn, []*corev1.Node{f.node})
	require.NoError(t, err)
	require.False(t, f.ctrl.ipam.ContainAddress(f.reservation.IPAddress))
}

func TestProviderNetworkDeletionReleasesPendingReservationWithoutIPCR(t *testing.T) {
	f := newPendingProviderNetworkFixture(t, context.DeadlineExceeded)
	f.pn.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	_, err := f.client.KubeovnV1().ProviderNetworks().Update(context.Background(), f.pn, metav1.UpdateOptions{})
	require.NoError(t, err)
	f.ctrl.syncProviderNetworkStatus(f.pn, nil, nil)
	require.False(t, f.ctrl.ipam.ContainAddress(f.reservation.IPAddress))
	pn, err := f.client.KubeovnV1().ProviderNetworks().Get(context.Background(), f.pn.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotContains(t, pn.Finalizers, util.ProviderNetworkCleanupFinalizer)
	_, err = f.client.KubeovnV1().IPs().Get(context.Background(), f.reservation.IPName, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err))
}
