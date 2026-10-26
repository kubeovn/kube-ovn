package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnclient "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestProviderNetworkIPDeletionRequiresCleanup(t *testing.T) {
	for _, tt := range []struct {
		name     string
		phase    kubeovnv1.ProviderNetworkAddressPhase
		staleUID bool
		ackUID   string
		released bool
	}{
		{name: "allocated", phase: kubeovnv1.ProviderNetworkAddressAllocated},
		{name: "disconnected node", phase: kubeovnv1.ProviderNetworkAddressCleanupPending},
		{name: "phase without acknowledgement", phase: kubeovnv1.ProviderNetworkAddressCleanupSucceeded},
		{name: "acknowledgement before phase promotion", phase: kubeovnv1.ProviderNetworkAddressCleanupPending, ackUID: "ip-uid"},
		{name: "stale IP identity", phase: kubeovnv1.ProviderNetworkAddressCleanupSucceeded, staleUID: true, ackUID: "ip-uid"},
		{name: "stale acknowledgement", phase: kubeovnv1.ProviderNetworkAddressCleanupSucceeded, ackUID: "old-ip-uid"},
		{name: "acknowledged", phase: kubeovnv1.ProviderNetworkAddressCleanupSucceeded, ackUID: "ip-uid", released: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pn, _, ip := providerNetworkAddressTestObjects()
			ip.DeletionTimestamp = &metav1.Time{Time: time.Now()}
			address := providerNetworkNodeAddress(ip, tt.phase)
			address.CleanupAcknowledgedIPUID = tt.ackUID
			if tt.staleUID {
				address.IPUID = "old-ip-uid"
			}
			pn.Status.VlanSubinterfaceAddresses = []kubeovnv1.ProviderNetworkNodeAddress{*address}
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
			err = ctrl.handleUpdateIP(ip.Name)
			if tt.released {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, !tt.released, ctrl.ipam.ContainAddress(ip.Spec.IPAddress))
		})
	}
}

func TestProviderNetworkRejectsMACOnlyAllocation(t *testing.T) {
	pn, node, _ := providerNetworkAddressTestObjects()
	pn.Status.VlanSubinterfaceAddresses = nil
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: pn.Spec.DefaultInterfaceSubnet},
		Spec:       kubeovnv1.SubnetSpec{Provider: "external"},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn}, Subnets: []*kubeovnv1.Subnet{subnet},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, "", "", nil))
	for range 2 {
		_, err := ctrl.reconcileProviderNetworkNodeAddresses(pn, []*corev1.Node{node})
		require.ErrorContains(t, err, "MAC-only subnets are not supported")
		require.Empty(t, pn.Status.VlanSubinterfaceAddresses)
		ips, err := ctrl.config.KubeOvnClient.KubeovnV1().IPs().List(context.Background(), metav1.ListOptions{})
		require.NoError(t, err)
		require.Empty(t, ips.Items, "IP-less allocations must not create IP CRs")
		require.Empty(t, ctrl.ipam.Subnets[subnet.Name].NicToMac, "failed allocation must release its MAC")
		require.Empty(t, ctrl.ipam.Subnets[subnet.Name].PodToNicList)
	}
}

func TestReconcileProviderNetworkNodeAddresses(t *testing.T) {
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "underlay"},
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock: "192.0.2.0/29",
			Gateway:   "192.0.2.1",
		},
	}
	pn := &kubeovnv1.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "provider", UID: "provider-uid"},
		Spec: kubeovnv1.ProviderNetworkSpec{
			DefaultInterface:            "eth0.100",
			AutoCreateVlanSubinterfaces: true,
			DefaultInterfaceSubnet:      subnet.Name,
			CustomInterfaces: []kubeovnv1.CustomInterface{
				{Interface: "eth1.200", Nodes: []string{"custom-node"}},
				{Interface: "eth0.100", Nodes: []string{"same-interface-custom-node"}},
			},
		},
	}
	nodes := []*corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node-2"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "custom-node"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "same-interface-custom-node"}},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets:          []*kubeovnv1.Subnet{subnet},
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn},
		Nodes:            nodes,
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))

	changed, err := ctrl.reconcileProviderNetworkNodeAddresses(pn, nodes)
	require.NoError(t, err)
	require.True(t, changed)
	require.Len(t, pn.Status.VlanSubinterfaceAddresses, 2)
	require.NotEqual(t, pn.Status.VlanSubinterfaceAddresses[0].IPAddress, pn.Status.VlanSubinterfaceAddresses[1].IPAddress)

	for _, address := range pn.Status.VlanSubinterfaceAddresses {
		require.Equal(t, subnet.Name, address.Subnet)
		ipCR, err := ctrl.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), util.ProviderNetworkAddressName(pn.Name, address.Node), metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, address.IPAddress, ipCR.Spec.IPAddress)
		require.Equal(t, util.KindProviderNetwork, ipCR.Spec.PodType)
		require.Equal(t, "false", ipCR.Labels[util.IPReservedLabel])
		require.Len(t, ipCR.OwnerReferences, 1)
		require.Equal(t, util.KindProviderNetwork, ipCR.OwnerReferences[0].Kind)
		require.Equal(t, pn.Name, ipCR.OwnerReferences[0].Name)
		exists, err := ctrl.checkIPOwnerExists(ipCR)
		require.NoError(t, err)
		require.True(t, exists)

		// A custom assignment never inherits defaultInterfaceSubnet, even with the same interface name.
		for _, node := range []string{"custom-node", "same-interface-custom-node"} {
			customIP := ipCR.DeepCopy()
			customIP.Name = util.ProviderNetworkAddressName(pn.Name, node)
			customIP.Spec.NodeName = node
			exists, err := ctrl.checkIPOwnerExists(customIP)
			require.NoError(t, err)
			require.False(t, exists)
		}
	}
	ips, err := ctrl.config.KubeOvnClient.KubeovnV1().IPs().List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, ips.Items, 2)

	changed, err = ctrl.reconcileProviderNetworkNodeAddresses(pn, nodes)
	require.NoError(t, err)
	require.False(t, changed)

	// Disabling allocation requests deletion; the standard IP finalizer releases the address.
	allocations := slices.Clone(pn.Status.VlanSubinterfaceAddresses)
	pn.Spec.AutoCreateVlanSubinterfaces = false
	changed, err = ctrl.reconcileProviderNetworkNodeAddresses(pn, nodes)
	require.NoError(t, err)
	require.True(t, changed)
	for _, allocation := range allocations {
		require.True(t, ctrl.ipam.ContainAddress(allocation.IPAddress))
	}
}

func TestProviderNetworkSyncPublishesPartialAllocations(t *testing.T) {
	pn, _, _ := providerNetworkAddressTestObjects()
	pn.Status.VlanSubinterfaceAddresses = nil
	nodes := []*corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node-2"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node-3"}},
	}
	for _, node := range nodes[:2] {
		node.Labels = map[string]string{fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name): "true"}
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	// Only .2 and .3 are available for three nodes.
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet("underlay", "192.0.2.0/29", "192.0.2.1", []string{"192.0.2.1", "192.0.2.4..192.0.2.6"}))
	ctrl.syncProviderNetworkStatus(pn, nodes, nil)
	stored, err := ctrl.config.KubeOvnClient.KubeovnV1().ProviderNetworks().Get(context.Background(), pn.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Len(t, stored.Status.VlanSubinterfaceAddresses, 2)
	for i, address := range stored.Status.VlanSubinterfaceAddresses {
		require.Equal(t, nodes[i].Name, address.Node)
		ip, err := ctrl.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), util.ProviderNetworkAddressName(pn.Name, address.Node), metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, ip.Spec.IPAddress, address.IPAddress)
	}
	require.ElementsMatch(t, []string{"node-1", "node-2"}, stored.Status.ReadyNodes)
	require.Equal(t, []string{"node-3"}, stored.Status.NotReadyNodes)
	require.False(t, stored.Status.Ready)
	// Readiness remains label-based, even for a node whose allocation failed.
	nodes[2].Labels = map[string]string{fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name): "true"}
	ctrl.syncProviderNetworkStatus(stored, nodes, nil)
	stored, err = ctrl.config.KubeOvnClient.KubeovnV1().ProviderNetworks().Get(context.Background(), pn.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, stored.Status.Ready)
	require.Len(t, stored.Status.VlanSubinterfaceAddresses, 2)
}

func TestProviderNetworkAddressContinuesAfterNodeError(t *testing.T) {
	for _, operation := range []string{"get", "create", "owner"} {
		t.Run(operation, func(t *testing.T) {
			pn, node, ip := providerNetworkAddressTestObjects()
			healthyNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "healthy-node"}}
			healthyIP := ip.DeepCopy()
			healthyIP.Name = util.ProviderNetworkAddressName(pn.Name, healthyNode.Name)
			healthyIP.Spec.NodeName = healthyNode.Name
			healthyIP.Spec.IPAddress = "192.0.2.4"
			ips := []*kubeovnv1.IP{healthyIP}
			if operation != "create" {
				ips = append(ips, ip)
			}
			if operation == "owner" {
				ip.OwnerReferences[0].UID = "other-provider-uid"
			}
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn}, IPs: ips,
			})
			require.NoError(t, err)
			ctrl := fc.fakeController
			require.NoError(t, ctrl.ipam.AddOrUpdateSubnet("underlay", "192.0.2.0/29", "192.0.2.1", nil))
			client := ctrl.config.KubeOvnClient.(*kubeovnfake.Clientset)
			apiErr := errors.New("injected API failure")
			client.PrependReactor(operation, "ips", func(action clienttesting.Action) (bool, runtime.Object, error) {
				if operation == "get" && action.(clienttesting.GetAction).GetName() != ip.Name {
					return false, nil, nil
				}
				return true, nil, apiErr
			})
			changed, err := ctrl.reconcileProviderNetworkNodeAddresses(pn, []*corev1.Node{node, healthyNode})
			require.Error(t, err)
			if operation != "owner" {
				require.ErrorIs(t, err, apiErr)
			}
			require.True(t, changed)
			require.Equal(t, []kubeovnv1.ProviderNetworkNodeAddress{*providerNetworkNodeAddress(healthyIP, kubeovnv1.ProviderNetworkAddressAllocated)}, pn.Status.VlanSubinterfaceAddresses)
			// Failure must not turn a desired node into a deletion target.
			for _, action := range client.Actions() {
				require.NotEqual(t, "delete", action.GetVerb())
			}
		})
	}
}

func TestProviderNetworkAddressAggregatesCleanupErrors(t *testing.T) {
	pn, node, ip := providerNetworkAddressTestObjects()
	ips := []*kubeovnv1.IP{ip}
	for _, name := range []string{"removed-1", "removed-2"} {
		oldIP := ip.DeepCopy()
		oldIP.Name = util.ProviderNetworkAddressName(pn.Name, name)
		oldIP.Spec.NodeName = name
		ips = append(ips, oldIP)
		pn.Status.VlanSubinterfaceAddresses = append(pn.Status.VlanSubinterfaceAddresses,
			kubeovnv1.ProviderNetworkNodeAddress{Node: name, Subnet: oldIP.Spec.Subnet, IPAddress: oldIP.Spec.IPAddress})
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn}, IPs: ips,
	})
	require.NoError(t, err)
	client := fc.fakeController.config.KubeOvnClient.(*kubeovnfake.Clientset)
	apiErr := errors.New("injected cleanup failure")
	deletions := 0
	client.PrependReactor("delete", "ips", func(clienttesting.Action) (bool, runtime.Object, error) {
		deletions++
		return true, nil, apiErr
	})
	changed, err := fc.fakeController.reconcileProviderNetworkNodeAddresses(pn, []*corev1.Node{node})
	require.ErrorIs(t, err, apiErr)
	require.ErrorContains(t, err, "removed-1")
	require.ErrorContains(t, err, "removed-2")
	require.Equal(t, 2, deletions)
	require.True(t, changed)
	require.Len(t, pn.Status.VlanSubinterfaceAddresses, 3)
	for _, address := range pn.Status.VlanSubinterfaceAddresses {
		if address.Node == node.Name {
			require.Equal(t, kubeovnv1.ProviderNetworkAddressAllocated, address.Phase)
		} else {
			require.Equal(t, kubeovnv1.ProviderNetworkAddressCleanupPending, address.Phase)
		}
	}
}

func TestProviderNetworkSyncPublishesDespiteReadTimeout(t *testing.T) {
	pn, node, ip := providerNetworkAddressTestObjects()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn}, IPs: []*kubeovnv1.IP{ip},
	})
	require.NoError(t, err)
	client := fc.fakeController.config.KubeOvnClient.(*kubeovnfake.Clientset)
	client.PrependReactor("get", "ips", func(action clienttesting.Action) (bool, runtime.Object, error) {
		if action.(clienttesting.GetAction).GetName() == ip.Name {
			return false, nil, nil
		}
		return true, nil, context.DeadlineExceeded
	})
	nodes := []*corev1.Node{node, {ObjectMeta: metav1.ObjectMeta{Name: "interrupted-node"}}}
	fc.fakeController.syncProviderNetworkStatus(pn, nodes, nil)
	stored, err := client.KubeovnV1().ProviderNetworks().Get(context.Background(), pn.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, []kubeovnv1.ProviderNetworkNodeAddress{*providerNetworkNodeAddress(ip, kubeovnv1.ProviderNetworkAddressAllocated)}, stored.Status.VlanSubinterfaceAddresses)
}

type providerNetworkContextTransport func(*http.Request) (*http.Response, error)

func (f providerNetworkContextTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestProviderNetworkSyncUsesLocalTimeouts(t *testing.T) {
	for _, operation := range []string{"get", "delete", "status"} {
		t.Run(operation, func(t *testing.T) {
			pn, node, ip := providerNetworkAddressTestObjects()
			pn.Status.VlanSubinterfaceAddresses = nil
			if operation == "status" {
				pn.Spec.AutoCreateVlanSubinterfaces = false
				node.Labels = map[string]string{fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name): "true"}
			}
			ip.TypeMeta = metav1.TypeMeta{APIVersion: kubeovnv1.SchemeGroupVersion.String(), Kind: util.KindIP}
			ip.Spec.Subnet = "old-underlay"
			ipJSON, err := json.Marshal(ip)
			require.NoError(t, err)
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn},
			})
			require.NoError(t, err)
			ctrl := fc.fakeController
			require.NoError(t, ctrl.ipam.AddOrUpdateSubnet("underlay", "192.0.2.0/29", "192.0.2.1", nil))

			requestFailed := false
			apiErr := errors.New("injected API failure")
			client, err := kubeovnclient.NewForConfig(&rest.Config{
				Host: "http://provider-network.test",
				Transport: providerNetworkContextTransport(func(req *http.Request) (*http.Response, error) {
					deadline, ok := req.Context().Deadline()
					require.True(t, ok)
					require.LessOrEqual(t, time.Until(deadline), 30*time.Second)
					if requestFailed {
						return nil, apiErr
					}
					if operation == "get" || req.Method != http.MethodGet {
						expectedMethod := map[string]string{
							"get": http.MethodGet, "delete": http.MethodDelete, "status": http.MethodPut,
						}[operation]
						require.Equal(t, expectedMethod, req.Method)
						requestFailed = true
						return nil, apiErr
					}
					status, body := http.StatusOK, string(ipJSON)
					if strings.HasSuffix(req.URL.Path, "/ips") {
						items := "[]"
						if operation == "delete" {
							items = "[" + string(ipJSON) + "]"
						}
						body = `{"kind":"IPList","apiVersion":"kubeovn.io/v1","items":` + items + `}`
					}
					return &http.Response{
						StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
						Body: io.NopCloser(strings.NewReader(body)),
					}, nil
				}),
			})
			require.NoError(t, err)
			ctrl.config.KubeOvnClient = client
			ctrl.syncProviderNetworkStatus(pn, []*corev1.Node{node}, nil)
			require.True(t, requestFailed, "expected API operation was not reached")
		})
	}
}

func TestCreateOrUpdateProviderNetworkIPCRUsesLocalTimeout(t *testing.T) {
	pn, node, ip := providerNetworkAddressTestObjects()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn},
	})
	require.NoError(t, err)
	apiErr := errors.New("injected create failure")
	var methods []string
	client, err := kubeovnclient.NewForConfig(&rest.Config{
		Host: "http://provider-network.test",
		Transport: providerNetworkContextTransport(func(req *http.Request) (*http.Response, error) {
			methods = append(methods, req.Method)
			deadline, ok := req.Context().Deadline()
			require.True(t, ok)
			require.Positive(t, time.Until(deadline))
			require.LessOrEqual(t, time.Until(deadline), 30*time.Second)
			if req.Method == http.MethodGet {
				return &http.Response{
					StatusCode: http.StatusNotFound, Header: http.Header{"Content-Type": {"application/json"}},
					Body: io.NopCloser(strings.NewReader(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`)),
				}, nil
			}
			return nil, apiErr
		}),
	})
	require.NoError(t, err)
	fc.fakeController.config.KubeOvnClient = client
	err = fc.fakeController.createOrUpdateIPCR("", pn.Name, ip.Spec.IPAddress, ip.Spec.MacAddress, ip.Spec.Subnet, "", node.Name, util.KindProviderNetwork)
	require.ErrorIs(t, err, apiErr)
	require.Equal(t, []string{http.MethodGet, http.MethodPost}, methods)
}

func providerNetworkAddressTestObjects() (*kubeovnv1.ProviderNetwork, *corev1.Node, *kubeovnv1.IP) {
	pn := &kubeovnv1.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "provider", UID: "provider-uid", Finalizers: []string{util.ProviderNetworkCleanupFinalizer}},
		Spec: kubeovnv1.ProviderNetworkSpec{
			DefaultInterface: "eth0.100", AutoCreateVlanSubinterfaces: true, DefaultInterfaceSubnet: "underlay",
		},
		Status: kubeovnv1.ProviderNetworkStatus{
			VlanSubinterfaceAddresses: []kubeovnv1.ProviderNetworkNodeAddress{
				{Node: "node", Subnet: "underlay", IPAddress: "192.0.2.3"},
			},
		},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}}
	ip := &kubeovnv1.IP{
		ObjectMeta: metav1.ObjectMeta{
			Name: util.ProviderNetworkAddressName(pn.Name, node.Name), UID: "ip-uid",
			Finalizers: []string{util.KubeOVNControllerFinalizer},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: kubeovnv1.SchemeGroupVersion.String(), Kind: util.KindProviderNetwork,
				Name: pn.Name, UID: pn.UID,
			}},
		},
		Spec: kubeovnv1.IPSpec{
			PodType: util.KindProviderNetwork, NodeName: node.Name, Subnet: "underlay",
			IPAddress: "192.0.2.2", MacAddress: "02:00:00:00:00:02",
		},
	}
	return pn, node, ip
}

func TestProviderNetworkAddressDiscoveryUsesOwnerIndex(t *testing.T) {
	pn, node, ip := providerNetworkAddressTestObjects()
	pn.Status.VlanSubinterfaceAddresses = nil // Discovery must not depend on persisted status.
	other := ip.DeepCopy()
	other.Name = "pn.other.other-node"
	other.Spec.NodeName = "other-node"
	other.OwnerReferences[0].UID = "previous-provider-uid"
	podIP := &kubeovnv1.IP{
		ObjectMeta: metav1.ObjectMeta{Name: "pod.default"},
		Spec:       kubeovnv1.IPSpec{PodName: "pod", Namespace: "default", IPAddress: "192.0.2.4"},
	}
	spoofed := ip.DeepCopy()
	spoofed.Name, spoofed.Spec.NodeName = "pn.spoofed.node", "spoofed-node"
	spoofed.OwnerReferences[0].Name = "other-provider"
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn}, IPs: []*kubeovnv1.IP{ip, other, podIP, spoofed},
	})
	require.NoError(t, err)
	client := fc.fakeController.config.KubeOvnClient.(*kubeovnfake.Clientset)
	client.ClearActions()
	addresses, err := fc.fakeController.providerNetworkAddresses(pn)
	require.NoError(t, err)
	require.Equal(t, map[string]kubeovnv1.ProviderNetworkNodeAddress{
		node.Name: *providerNetworkNodeAddress(ip, kubeovnv1.ProviderNetworkAddressAllocated),
	}, addresses)
	require.Empty(t, client.Actions(), "discovery must use the owner index without API requests")
}

func TestProviderNetworkAddressUsesIPCR(t *testing.T) {
	for _, withStatus := range []bool{false, true} {
		t.Run(fmt.Sprint("withStatus=", withStatus), func(t *testing.T) {
			pn, node, ip := providerNetworkAddressTestObjects()
			if !withStatus {
				pn.Status.VlanSubinterfaceAddresses = nil
			}
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn}, IPs: []*kubeovnv1.IP{ip},
			})
			require.NoError(t, err)
			// No IPAM subnet is needed: an existing allocation must be read, not allocated again.
			changed, err := fc.fakeController.reconcileProviderNetworkNodeAddresses(pn, []*corev1.Node{node})
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, []kubeovnv1.ProviderNetworkNodeAddress{*providerNetworkNodeAddress(ip, kubeovnv1.ProviderNetworkAddressAllocated)}, pn.Status.VlanSubinterfaceAddresses)
		})
	}
}

func TestProviderNetworkAddressRecreatesMissingIPCR(t *testing.T) {
	pn, node, ip := providerNetworkAddressTestObjects()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn}, IPs: []*kubeovnv1.IP{ip},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet("underlay", "192.0.2.0/29", "192.0.2.1", nil))
	// Delete through the API without updating the informer cache.
	ips := ctrl.config.KubeOvnClient.KubeovnV1().IPs()
	require.NoError(t, ips.Delete(context.Background(), ip.Name, metav1.DeleteOptions{}))
	_, err = ctrl.reconcileProviderNetworkNodeAddresses(pn, []*corev1.Node{node})
	require.NoError(t, err)
	restored, err := ips.Get(context.Background(), ip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, providerNetworkOwnsNodeIP(pn, node.Name, restored))
	require.Equal(t, restored.Spec.IPAddress, pn.Status.VlanSubinterfaceAddresses[0].IPAddress)
	require.True(t, ctrl.ipam.ContainAddress(restored.Spec.IPAddress))
}

func TestProviderNetworkAddressRejectsInvalidIPCR(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*kubeovnv1.IP)
	}{
		{"owner UID", func(ip *kubeovnv1.IP) { ip.OwnerReferences[0].UID = "old-provider-uid" }},
		{"owner name", func(ip *kubeovnv1.IP) { ip.OwnerReferences[0].Name = "other-provider" }},
		{"missing owner", func(ip *kubeovnv1.IP) { ip.OwnerReferences = nil }},
		{"node", func(ip *kubeovnv1.IP) { ip.Spec.NodeName = "other-node" }},
		{"pod type", func(ip *kubeovnv1.IP) { ip.Spec.PodType = util.KindNode }},
		{"empty address", func(ip *kubeovnv1.IP) { ip.Spec.IPAddress = "" }},
		{"invalid address", func(ip *kubeovnv1.IP) { ip.Spec.IPAddress = "not-an-ip" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pn, node, ip := providerNetworkAddressTestObjects()
			tt.mutate(ip)
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn}, IPs: []*kubeovnv1.IP{ip},
			})
			require.NoError(t, err)
			_, err = fc.fakeController.reconcileProviderNetworkNodeAddresses(pn, []*corev1.Node{node})
			require.Error(t, err)
			unchanged, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), ip.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, ip, unchanged)
		})
	}
}

func TestProviderNetworkAddressCreationDoesNotOverwriteExistingIPCR(t *testing.T) {
	pn, node, ip := providerNetworkAddressTestObjects()
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ips := ctrl.config.KubeOvnClient.KubeovnV1().IPs()
	// The API knows about this CR while the informer cache does not.
	_, err = ips.Create(context.Background(), ip, metav1.CreateOptions{})
	require.NoError(t, err)
	err = ctrl.createOrUpdateIPCR("", pn.Name, "192.0.2.4", ip.Spec.MacAddress, ip.Spec.Subnet, "", node.Name, util.KindProviderNetwork)
	require.ErrorContains(t, err, "already exists")
	unchanged, err := ips.Get(context.Background(), ip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, ip, unchanged)
}

func TestProviderNetworkAddressCleanupDoesNotDeleteForeignIPCR(t *testing.T) {
	pn, node, ip := providerNetworkAddressTestObjects()
	pn.Spec.AutoCreateVlanSubinterfaces = false
	ip.OwnerReferences[0].UID = "other-provider-uid"
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn}, IPs: []*kubeovnv1.IP{ip},
	})
	require.NoError(t, err)
	_, err = fc.fakeController.reconcileProviderNetworkNodeAddresses(pn, []*corev1.Node{node})
	require.Error(t, err)
	unchanged, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), ip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, ip, unchanged)
}

func TestProviderNetworkAddressWaitsForDeletion(t *testing.T) {
	for _, terminating := range []bool{false, true} {
		t.Run(fmt.Sprint("terminating=", terminating), func(t *testing.T) {
			pn, node, ip := providerNetworkAddressTestObjects()
			if terminating {
				ip.DeletionTimestamp = &metav1.Time{Time: time.Now()}
			} else {
				ip.Spec.Subnet = "old-underlay"
			}
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn}, IPs: []*kubeovnv1.IP{ip},
			})
			require.NoError(t, err)
			changed, err := fc.fakeController.reconcileProviderNetworkNodeAddresses(pn, []*corev1.Node{node})
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, []kubeovnv1.ProviderNetworkNodeAddress{*providerNetworkNodeAddress(ip, kubeovnv1.ProviderNetworkAddressCleanupPending)}, pn.Status.VlanSubinterfaceAddresses)
			if !terminating {
				_, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), ip.Name, metav1.GetOptions{})
				require.True(t, k8serrors.IsNotFound(err))
			}
		})
	}
}
