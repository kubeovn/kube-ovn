package daemon

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/table"
)

type lifecycleVlanLister struct {
	kubeovnlisters.VlanLister
	get func() error
}

type lifecycleProviderNetworkLister struct {
	kubeovnlisters.ProviderNetworkLister
	err error
}

func (l lifecycleProviderNetworkLister) Get(string) (*kubeovnv1.ProviderNetwork, error) {
	return nil, l.err
}

func (l lifecycleVlanLister) Get(string) (*kubeovnv1.Vlan, error) {
	return nil, l.get()
}

type lifecycleTableProvider struct {
	list func() error
}

func (p lifecycleTableProvider) Table(model.Model) table.Handle {
	return lifecycleTableHandle{list: p.list}
}

type lifecycleTableHandle struct {
	table.Handle
	list func() error
}

func (h lifecycleTableHandle) List(context.Context, any) error {
	return h.list()
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

func requireProviderNetworkOperationBlocked[T any](t *testing.T, ch <-chan T) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal("provider network operation ran before the preceding operation finished")
	case <-time.After(100 * time.Millisecond):
	}
}

func receiveProviderNetworkOperation[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("provider network operation did not finish")
		var zero T
		return zero
	}
}

func TestProviderNetworkDeleteWaitsForInFlightUpdate(t *testing.T) {
	controller, pn, indexer := newProviderNetworkLifecycleController(t)
	updating := make(chan struct{})
	releaseUpdate := make(chan struct{})
	release := sync.OnceFunc(func() { close(releaseUpdate) })
	t.Cleanup(release)
	updateErr := errors.New("stop initialization after reading the provider network")
	controller.vlansLister = lifecycleVlanLister{get: func() error {
		close(updating)
		<-releaseUpdate
		return updateErr
	}}
	cleaning := make(chan struct{})
	cleanupErr := errors.New("stop cleanup before changing OVS")
	provider := lifecycleTableProvider{list: func() error {
		close(cleaning)
		return cleanupErr
	}}
	controller.config.VswitchTables = provider
	controller.vswitchTables = provider
	updateDone := make(chan error, 1)
	go func() { updateDone <- controller.handleAddOrUpdateProviderNetwork(pn.Name) }()
	receiveProviderNetworkOperation(t, updating)
	// The informer removes the object while initialization still holds its snapshot.
	require.NoError(t, indexer.Delete(pn))
	deleteDone := make(chan error, 1)
	go func() { deleteDone <- controller.handleDeleteProviderNetwork(pn) }()
	requireProviderNetworkOperationBlocked(t, cleaning)
	release()
	require.ErrorIs(t, receiveProviderNetworkOperation(t, updateDone), updateErr)
	require.ErrorIs(t, receiveProviderNetworkOperation(t, deleteDone), cleanupErr)
}

func TestProviderNetworkUpdateRechecksCacheAfterDelete(t *testing.T) {
	controller, pn, indexer := newProviderNetworkLifecycleController(t)
	cleaning := make(chan struct{})
	releaseDelete := make(chan struct{})
	release := sync.OnceFunc(func() { close(releaseDelete) })
	t.Cleanup(release)
	cleanupErr := errors.New("stop cleanup before changing OVS")
	provider := lifecycleTableProvider{list: func() error {
		close(cleaning)
		<-releaseDelete
		return cleanupErr
	}}
	controller.config.VswitchTables = provider
	controller.vswitchTables = provider
	controller.vlansLister = errorVlanLister{err: errors.New("deleted provider network was initialized")}
	deleteDone := make(chan error, 1)
	go func() { deleteDone <- controller.handleDeleteProviderNetwork(pn) }()
	receiveProviderNetworkOperation(t, cleaning)
	updateDone := make(chan error, 1)
	go func() { updateDone <- controller.handleAddOrUpdateProviderNetwork(pn.Name) }()
	requireProviderNetworkOperationBlocked(t, updateDone)
	require.NoError(t, indexer.Delete(pn))
	release()
	require.ErrorIs(t, receiveProviderNetworkOperation(t, deleteDone), cleanupErr)
	require.NoError(t, receiveProviderNetworkOperation(t, updateDone))
}

func TestProviderNetworkDeleteSkipsRecreatedObject(t *testing.T) {
	controller, pn, indexer := newProviderNetworkLifecycleController(t)
	replacement := pn.DeepCopy()
	replacement.UID = "replacement-provider-network"
	require.NoError(t, indexer.Update(replacement))
	provider := lifecycleTableProvider{list: func() error {
		t.Error("stale delete reached OVS cleanup for the replacement provider network")
		return errors.New("unexpected OVS cleanup")
	}}
	controller.config.VswitchTables = provider
	controller.vswitchTables = provider
	require.NoError(t, controller.handleDeleteProviderNetwork(pn))
}

func TestProviderNetworkUpdateSkipsDeletingObject(t *testing.T) {
	controller, pn, indexer := newProviderNetworkLifecycleController(t)
	pn = pn.DeepCopy()
	pn.DeletionTimestamp = new(metav1.Now())
	require.NoError(t, indexer.Update(pn))
	controller.vlansLister = errorVlanLister{err: errors.New("deleting provider network was initialized")}
	require.NoError(t, controller.handleAddOrUpdateProviderNetwork(pn.Name))
}

func TestProviderNetworkDeleteRetriesCacheFailure(t *testing.T) {
	controller, pn, _ := newProviderNetworkLifecycleController(t)
	cacheErr := errors.New("provider network cache unavailable")
	controller.providerNetworksLister = lifecycleProviderNetworkLister{err: cacheErr}
	provider := lifecycleTableProvider{list: func() error {
		t.Error("delete reached OVS cleanup without checking the current provider network")
		return errors.New("unexpected OVS cleanup")
	}}
	controller.config.VswitchTables = provider
	controller.vswitchTables = provider
	require.ErrorIs(t, controller.handleDeleteProviderNetwork(pn), cacheErr)
}
