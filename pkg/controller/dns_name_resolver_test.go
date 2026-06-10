package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovninformerfactory "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
)

func newFakeDNSNameResolverController(t *testing.T, enableANP bool, resolvers ...*kubeovnv1.DNSNameResolver) *Controller {
	informerFactory := kubeovninformerfactory.NewSharedInformerFactory(kubeovnfake.NewSimpleClientset(), 0)
	informer := informerFactory.Kubeovn().V1().DNSNameResolvers()
	for _, resolver := range resolvers {
		require.NoError(t, informer.Informer().GetIndexer().Add(resolver))
	}

	ctrl := &Controller{
		config:                 &Configuration{EnableANP: enableANP, EnableDNSNameResolver: true},
		dnsNameResolversLister: informer.Lister(),
	}
	if enableANP {
		ctrl.updateAnpQueue = newTypedRateLimitingQueue[*AdminNetworkPolicyChangedDelta]("UpdateAdminNetworkPolicy", nil)
		ctrl.updateCnpQueue = newTypedRateLimitingQueue[*ClusterNetworkPolicyChangedDelta]("UpdateClusterNetworkPolicy", nil)
	}
	return ctrl
}

func newTestDNSNameResolver() *kubeovnv1.DNSNameResolver {
	return &kubeovnv1.DNSNameResolver{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "anp-test-12345678",
			Labels: map[string]string{adminNetworkPolicyKey: "test-anp"},
		},
		Spec: kubeovnv1.DNSNameResolverSpec{Name: "example.com"},
	}
}

func Test_handleAddOrUpdateDNSNameResolver(t *testing.T) {
	t.Run("anp disabled", func(t *testing.T) {
		// the ANP/CNP update queues are nil when ANP support is disabled,
		// the handler must not enqueue into them
		resolver := newTestDNSNameResolver()
		ctrl := newFakeDNSNameResolverController(t, false, resolver)
		require.NotPanics(t, func() {
			require.NoError(t, ctrl.handleAddOrUpdateDNSNameResolver(resolver.Name))
		})
	})

	t.Run("anp enabled", func(t *testing.T) {
		resolver := newTestDNSNameResolver()
		ctrl := newFakeDNSNameResolverController(t, true, resolver)
		require.NoError(t, ctrl.handleAddOrUpdateDNSNameResolver(resolver.Name))
		require.Equal(t, 1, ctrl.updateAnpQueue.Len())
		require.Equal(t, 1, ctrl.updateCnpQueue.Len())
	})
}

func Test_handleDeleteDNSNameResolver(t *testing.T) {
	t.Run("anp disabled", func(t *testing.T) {
		// the ANP/CNP update queues are nil when ANP support is disabled,
		// the handler must not enqueue into them
		resolver := newTestDNSNameResolver()
		ctrl := newFakeDNSNameResolverController(t, false)
		require.NotPanics(t, func() {
			require.NoError(t, ctrl.handleDeleteDNSNameResolver(resolver))
		})
	})

	t.Run("anp enabled", func(t *testing.T) {
		resolver := newTestDNSNameResolver()
		ctrl := newFakeDNSNameResolverController(t, true)
		require.NoError(t, ctrl.handleDeleteDNSNameResolver(resolver))
		require.Equal(t, 1, ctrl.updateAnpQueue.Len())
		require.Equal(t, 1, ctrl.updateCnpQueue.Len())
	})
}
