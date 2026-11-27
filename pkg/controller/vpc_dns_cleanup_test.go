package controller

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	corelisters "k8s.io/client-go/listers/core/v1"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var vpcDNSResource = schema.GroupVersionResource{Group: "kubeovn.io", Version: "v1", Resource: "vpc-dnses"}

// newVpcDNSCleanupController returns a controller without vpc-dns-config whose fake client,
// like the default controller ClusterRole, does not allow deletecollection on vpc-dnses
func newVpcDNSCleanupController(t *testing.T, names ...string) (*Controller, *kubeovnfake.Clientset) {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	client := kubeovnfake.NewSimpleClientset()
	for _, name := range names {
		// created through the client: objects passed to NewSimpleClientset are tracked as "vpcdnses", not "vpc-dnses"
		dns, err := client.KubeovnV1().VpcDnses().Create(t.Context(), &kubeovnv1.VpcDns{Name: name}, metav1.CreateOptions{})
		require.NoError(t, err)
		require.NoError(t, indexer.Add(dns))
	}
	require.ElementsMatch(t, names, remainingVpcDNSNames(t, client))
	client.ClearActions()
	client.PrependReactor("delete-collection", "vpc-dnses", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, k8serrors.NewForbidden(vpcDNSResource.GroupResource(), "", errors.New("deletecollection is not allowed"))
	})
	return &Controller{
		config:           &Configuration{PodNamespace: metav1.NamespaceSystem, KubeOvnClient: client},
		configMapsLister: corelisters.NewConfigMapLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})),
		vpcDNSLister:     kubeovnlister.NewVpcDnsLister(indexer),
	}, client
}

func remainingVpcDNSNames(t *testing.T, client *kubeovnfake.Clientset) []string {
	t.Helper()
	list, err := client.KubeovnV1().VpcDnses().List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		names = append(names, item.Name)
	}
	return names
}

func deletedVpcDNSNames(t *testing.T, client *kubeovnfake.Clientset) []string {
	t.Helper()
	var names []string
	for _, action := range client.Actions() {
		require.NotEqual(t, "delete-collection", action.GetVerb())
		if action.GetVerb() == "delete" && action.GetResource() == vpcDNSResource {
			names = append(names, action.(ktesting.DeleteAction).GetName())
		}
	}
	return names
}

// keepVpcDNSCache restores the cached vpc-dns config changed by a test
func keepVpcDNSCache(t *testing.T) {
	t.Helper()
	enabled, applied, synced, image, vip, version := enableCoreDNS, appliedEnableCoreDNS, vpcDNSConfigSynced, corednsImage, corednsVip, cmVersion
	t.Cleanup(func() {
		enableCoreDNS, appliedEnableCoreDNS, vpcDNSConfigSynced, corednsImage, corednsVip, cmVersion = enabled, applied, synced, image, vip, version
	})
}

// setVpcDNSConfigMap makes the lister return vpc-dns-config with the given version and data
func setVpcDNSConfigMap(t *testing.T, controller *Controller, version string, data map[string]string) {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(&corev1.ConfigMap{Name: util.VpcDNSConfig, Namespace: metav1.NamespaceSystem, ResourceVersion: version, Data: data}))
	controller.configMapsLister = corelisters.NewConfigMapLister(indexer)
}

func setVpcDNSQueue(t *testing.T, controller *Controller) {
	t.Helper()
	controller.addOrUpdateVpcDNSQueue = newTypedRateLimitingQueue[string]("AddOrUpdateVpcDns", nil)
	t.Cleanup(controller.addOrUpdateVpcDNSQueue.ShutDown)
}

func TestCleanVpcDNSDeletesEachVpcDNS(t *testing.T) {
	t.Parallel()
	controller, client := newVpcDNSCleanupController(t, "dns-a", "dns-b", "dns-c")

	require.NoError(t, controller.cleanVpcDNS())

	require.ElementsMatch(t, []string{"dns-a", "dns-b", "dns-c"}, deletedVpcDNSNames(t, client))
	require.Empty(t, remainingVpcDNSNames(t, client))
}

func TestCleanVpcDNSWithoutVpcDNS(t *testing.T) {
	t.Parallel()
	controller, client := newVpcDNSCleanupController(t)

	require.NoError(t, controller.cleanVpcDNS())

	require.Empty(t, deletedVpcDNSNames(t, client))
}

func TestCleanVpcDNSIgnoresAlreadyDeleted(t *testing.T) {
	t.Parallel()
	controller, client := newVpcDNSCleanupController(t, "dns-a", "dns-b")
	// dns-a is deleted by someone else between the list and its delete
	client.PrependReactor("delete", "vpc-dnses", func(action ktesting.Action) (bool, runtime.Object, error) {
		if name := action.(ktesting.DeleteAction).GetName(); name == "dns-a" {
			require.NoError(t, client.Tracker().Delete(vpcDNSResource, "", name))
			return true, nil, k8serrors.NewNotFound(vpcDNSResource.GroupResource(), name)
		}
		return false, nil, nil
	})

	require.NoError(t, controller.cleanVpcDNS())

	require.ElementsMatch(t, []string{"dns-a", "dns-b"}, deletedVpcDNSNames(t, client))
	require.Empty(t, remainingVpcDNSNames(t, client))
}

func TestCleanVpcDNSDeletesVpcDNSMissingFromCache(t *testing.T) {
	t.Parallel()
	controller, client := newVpcDNSCleanupController(t, "dns-a")
	// dns-b exists in the API server but its add event has not reached the informer yet
	_, err := client.KubeovnV1().VpcDnses().Create(t.Context(), &kubeovnv1.VpcDns{Name: "dns-b"}, metav1.CreateOptions{})
	require.NoError(t, err)
	client.ClearActions()

	require.NoError(t, controller.cleanVpcDNS())

	require.ElementsMatch(t, []string{"dns-a", "dns-b"}, deletedVpcDNSNames(t, client))
	require.Empty(t, remainingVpcDNSNames(t, client))
}

func TestCleanVpcDNSDeletesOthersWhenOneFails(t *testing.T) {
	t.Parallel()
	controller, client := newVpcDNSCleanupController(t, "dns-a", "dns-b", "dns-c")
	// two of three fail, so that stopping at the first error is caught in any listing order
	client.PrependReactor("delete", "vpc-dnses", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.(ktesting.DeleteAction).GetName() != "dns-c" {
			return true, nil, k8serrors.NewInternalError(errors.New("boom"))
		}
		return false, nil, nil
	})

	err := controller.cleanVpcDNS()

	require.ErrorContains(t, err, "failed to delete vpc-dns dns-a")
	require.ErrorContains(t, err, "failed to delete vpc-dns dns-b")
	require.ElementsMatch(t, []string{"dns-a", "dns-b", "dns-c"}, deletedVpcDNSNames(t, client))
	require.ElementsMatch(t, []string{"dns-a", "dns-b"}, remainingVpcDNSNames(t, client))
}

func TestResyncVpcDNSConfigConfigMapDeleted(t *testing.T) {
	keepVpcDNSCache(t)
	enableCoreDNS, appliedEnableCoreDNS, vpcDNSConfigSynced, corednsImage, cmVersion = true, true, true, "example.com/coredns:deleted", "1"
	controller, client := newVpcDNSCleanupController(t, "dns-a", "dns-b")

	controller.resyncVpcDNSConfig()

	require.False(t, enableCoreDNS)
	require.Empty(t, corednsImage)
	require.Empty(t, cmVersion)
	require.Empty(t, remainingVpcDNSNames(t, client))
}

func TestResyncVpcDNSConfigConfigMapDeletedCleanupFails(t *testing.T) {
	keepVpcDNSCache(t)
	enableCoreDNS, appliedEnableCoreDNS, vpcDNSConfigSynced, corednsImage, cmVersion = true, true, true, "example.com/coredns:deleted", "1"
	controller, client := newVpcDNSCleanupController(t, "dns-a")
	failDelete := true
	client.PrependReactor("delete", "vpc-dnses", func(ktesting.Action) (bool, runtime.Object, error) {
		if failDelete {
			return true, nil, k8serrors.NewForbidden(vpcDNSResource.GroupResource(), "dns-a", errors.New("delete is not allowed"))
		}
		return false, nil, nil
	})

	controller.resyncVpcDNSConfig()

	// the cache is reset at once, cmVersion is kept so that the next resync retries the cleanup
	require.False(t, enableCoreDNS)
	require.Empty(t, corednsImage)
	require.Equal(t, "1", cmVersion)
	require.Equal(t, []string{"dns-a"}, remainingVpcDNSNames(t, client))

	failDelete = false
	controller.resyncVpcDNSConfig()

	require.Empty(t, cmVersion)
	require.Empty(t, remainingVpcDNSNames(t, client))
}

func TestResyncVpcDNSConfigDisableCleanupFails(t *testing.T) {
	keepVpcDNSCache(t)
	enableCoreDNS, appliedEnableCoreDNS, vpcDNSConfigSynced, corednsImage, cmVersion = true, true, true, "example.com/coredns:old", "1"
	controller, client := newVpcDNSCleanupController(t, "dns-a")
	setVpcDNSConfigMap(t, controller, "2", map[string]string{"enable-vpc-dns": "false"})
	failDelete := true
	client.PrependReactor("delete", "vpc-dnses", func(ktesting.Action) (bool, runtime.Object, error) {
		if failDelete {
			return true, nil, k8serrors.NewForbidden(vpcDNSResource.GroupResource(), "dns-a", errors.New("delete is not allowed"))
		}
		return false, nil, nil
	})

	controller.resyncVpcDNSConfig()

	// the gate is closed at once, cmVersion is kept so that the next resync retries the cleanup
	require.False(t, enableCoreDNS)
	require.Equal(t, "1", cmVersion)
	require.Equal(t, []string{"dns-a"}, remainingVpcDNSNames(t, client))

	failDelete = false
	controller.resyncVpcDNSConfig()

	require.False(t, enableCoreDNS)
	require.False(t, appliedEnableCoreDNS)
	require.Equal(t, "2", cmVersion)
	require.Empty(t, remainingVpcDNSNames(t, client))
}

func TestResyncVpcDNSConfigFirstSyncWithoutConfigMap(t *testing.T) {
	keepVpcDNSCache(t)
	enableCoreDNS, appliedEnableCoreDNS, vpcDNSConfigSynced, corednsImage, cmVersion = false, false, false, "", ""
	controller, client := newVpcDNSCleanupController(t, "dns-a", "dns-b")

	controller.resyncVpcDNSConfig()

	require.True(t, vpcDNSConfigSynced)
	require.Empty(t, remainingVpcDNSNames(t, client))

	// later syncs without the ConfigMap do not clean up again
	_, err := client.KubeovnV1().VpcDnses().Create(t.Context(), &kubeovnv1.VpcDns{Name: "dns-c"}, metav1.CreateOptions{})
	require.NoError(t, err)
	controller.resyncVpcDNSConfig()

	require.Equal(t, []string{"dns-c"}, remainingVpcDNSNames(t, client))
}

func TestResyncVpcDNSConfigFirstSyncDisabled(t *testing.T) {
	keepVpcDNSCache(t)
	enableCoreDNS, appliedEnableCoreDNS, vpcDNSConfigSynced, corednsImage, cmVersion = false, false, false, "", ""
	controller, client := newVpcDNSCleanupController(t, "dns-a")
	setVpcDNSQueue(t, controller)
	setVpcDNSConfigMap(t, controller, "1", map[string]string{"enable-vpc-dns": "false"})

	controller.resyncVpcDNSConfig()

	require.False(t, enableCoreDNS)
	require.Equal(t, "1", cmVersion)
	require.Empty(t, remainingVpcDNSNames(t, client))
	require.Zero(t, controller.addOrUpdateVpcDNSQueue.Len())
}

func TestResyncVpcDNSConfigFirstSyncEnabled(t *testing.T) {
	keepVpcDNSCache(t)
	enableCoreDNS, appliedEnableCoreDNS, vpcDNSConfigSynced, corednsImage, cmVersion = false, false, false, "", ""
	controller, client := newVpcDNSCleanupController(t, "dns-a")
	setVpcDNSQueue(t, controller)
	setVpcDNSConfigMap(t, controller, "1", map[string]string{"coredns-image": "example.com/coredns:new"})

	controller.resyncVpcDNSConfig()

	require.True(t, enableCoreDNS)
	require.Equal(t, "example.com/coredns:new", corednsImage)
	require.Equal(t, "1", cmVersion)
	require.Equal(t, []string{"dns-a"}, remainingVpcDNSNames(t, client))
	require.Equal(t, 1, controller.addOrUpdateVpcDNSQueue.Len())
}

func TestHandleAddOrUpdateVpcDNSAfterConfigMapDeleted(t *testing.T) {
	keepVpcDNSCache(t)
	// the ConfigMap is already gone from the informer, but the periodic resync has not run yet
	enableCoreDNS, appliedEnableCoreDNS, vpcDNSConfigSynced, corednsImage, cmVersion = true, true, true, "example.com/coredns:deleted", "1"
	controller, client := newVpcDNSCleanupController(t, "dns-a")

	err := controller.handleAddOrUpdateVPCDNS("dns-a")

	require.ErrorContains(t, err, "not enabled")
	// dns-a was deleted by the cleanup, so its status is not updated
	require.NotContains(t, err.Error(), "failed to update vpc dns status")
	require.False(t, enableCoreDNS)
	require.Empty(t, corednsImage)
	require.Empty(t, cmVersion)
	require.Empty(t, remainingVpcDNSNames(t, client))
}

func TestHandleAddOrUpdateVpcDNSInvalidConfig(t *testing.T) {
	keepVpcDNSCache(t)
	// an applied, enabled configuration whose enable-vpc-dns is then changed to a value that does not parse
	enableCoreDNS, appliedEnableCoreDNS, vpcDNSConfigSynced, corednsImage, corednsVip, cmVersion = true, true, true, "example.com/coredns:old", "10.96.0.3", "1"
	controller, client := newVpcDNSCleanupController(t, "dns-a")
	// without the error from the config sync, the handler went on with the old configuration up to this lister
	controller.vpcsLister = kubeovnlister.NewVpcLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))
	setVpcDNSConfigMap(t, controller, "2", map[string]string{"enable-vpc-dns": "maybe"})

	err := controller.handleAddOrUpdateVPCDNS("dns-a")

	require.ErrorContains(t, err, "failed to apply "+util.VpcDNSConfig)
	require.ErrorContains(t, err, "invalid syntax")
	// nothing is deleted for an invalid configuration, and the new version is retried on the next sync
	require.True(t, enableCoreDNS)
	require.Equal(t, "1", cmVersion)
	require.Equal(t, []string{"dns-a"}, remainingVpcDNSNames(t, client))
}
