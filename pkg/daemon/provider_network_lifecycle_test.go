package daemon

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
)

type lifecycleProviderNetworkLister struct {
	kubeovnlisters.ProviderNetworkLister
	err error
}

func (l lifecycleProviderNetworkLister) Get(string) (*kubeovnv1.ProviderNetwork, error) {
	return nil, l.err
}

func newProviderNetworkLifecycleController(t *testing.T) (*Controller, *kubeovnv1.ProviderNetwork, cache.Indexer) {
	t.Helper()
	pn := &kubeovnv1.ProviderNetwork{
		Name:   "provider-network-1",
		UID:    "original-provider-network",
		Spec:   kubeovnv1.ProviderNetworkSpec{DefaultInterface: "eth1"},
		Status: kubeovnv1.ProviderNetworkStatus{Vlans: []string{"vlan-1"}},
	}
	controller, _ := newProviderNetworkEventController(t, pn, &corev1.Node{Name: "node-1"})
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(pn))
	controller.providerNetworksLister = kubeovnlisters.NewProviderNetworkLister(indexer)
	return controller, pn, indexer
}

func requireProviderNetworkOperationBlocked(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("provider network operation completed before the lock was released: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

func receiveProviderNetworkOperation(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("provider network operation did not finish")
		return nil
	}
}

func TestProviderNetworkUpdateWaitsForMutex(t *testing.T) {
	controller, pn, indexer := newProviderNetworkLifecycleController(t)
	now := metav1.Now()
	pn = pn.DeepCopy()
	pn.DeletionTimestamp = &now
	require.NoError(t, indexer.Update(pn))

	controller.providerNetworkMutex.Lock()
	done := make(chan error, 1)
	go func() { done <- controller.handleAddOrUpdateProviderNetwork(pn.Name) }()
	requireProviderNetworkOperationBlocked(t, done)
	controller.providerNetworkMutex.Unlock()
	require.NoError(t, receiveProviderNetworkOperation(t, done))
}

func TestProviderNetworkDeleteWaitsForMutex(t *testing.T) {
	controller, pn, indexer := newProviderNetworkLifecycleController(t)
	replacement := pn.DeepCopy()
	replacement.UID = "replacement-provider-network"
	require.NoError(t, indexer.Update(replacement))

	controller.providerNetworkMutex.Lock()
	done := make(chan error, 1)
	go func() { done <- controller.handleDeleteProviderNetwork(pn) }()
	requireProviderNetworkOperationBlocked(t, done)
	controller.providerNetworkMutex.Unlock()
	require.NoError(t, receiveProviderNetworkOperation(t, done))
}

func TestProviderNetworkUpdateSkipsDeletingObject(t *testing.T) {
	controller, pn, indexer := newProviderNetworkLifecycleController(t)
	now := metav1.Now()
	pn = pn.DeepCopy()
	pn.DeletionTimestamp = &now
	require.NoError(t, indexer.Update(pn))

	require.NoError(t, controller.handleAddOrUpdateProviderNetwork(pn.Name))
}

func TestProviderNetworkDeleteSkipsRecreatedObject(t *testing.T) {
	controller, pn, indexer := newProviderNetworkLifecycleController(t)
	replacement := pn.DeepCopy()
	replacement.UID = "replacement-provider-network"
	require.NoError(t, indexer.Update(replacement))

	require.NoError(t, controller.handleDeleteProviderNetwork(pn))
}

func TestProviderNetworkDeleteRetriesCacheFailure(t *testing.T) {
	controller, pn, _ := newProviderNetworkLifecycleController(t)
	cacheErr := errors.New("provider network cache unavailable")
	controller.providerNetworksLister = lifecycleProviderNetworkLister{err: cacheErr}

	require.ErrorIs(t, controller.handleDeleteProviderNetwork(pn), cacheErr)
}
