package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clienttesting "k8s.io/client-go/testing"
	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"

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

func TestReconcileAnpDNSNameResolversUsesAllEgressRules(t *testing.T) {
	const policyName = "test-anp"
	resolver := &kubeovnv1.DNSNameResolver{
		ObjectMeta: metav1.ObjectMeta{
			Name:   generateDNSNameResolverName(policyName, "a.example"),
			Labels: map[string]string{adminNetworkPolicyKey: policyName},
		},
		Spec: kubeovnv1.DNSNameResolverSpec{Name: "a.example"},
	}
	ctrl := newFakeDNSNameResolverController(t, true, resolver)
	client := kubeovnfake.NewSimpleClientset(resolver)
	ctrl.config.KubeOvnClient = client
	anp := &v1alpha1.AdminNetworkPolicy{Spec: v1alpha1.AdminNetworkPolicySpec{
		Egress: []v1alpha1.AdminNetworkPolicyEgressRule{
			{To: []v1alpha1.AdminNetworkPolicyEgressPeer{{DomainNames: []v1alpha1.DomainName{"a.example"}}}},
			{To: []v1alpha1.AdminNetworkPolicyEgressPeer{{DomainNames: []v1alpha1.DomainName{"b.example"}}}},
		},
	}}

	require.ElementsMatch(t, []string{"a.example", "b.example"}, getAnpDomainNames(anp))
	require.NoError(t, ctrl.reconcileAnpDNSNameResolvers(policyName, anp))
	actions := client.Actions()
	require.Len(t, actions, 1)
	require.Equal(t, "create", actions[0].GetVerb())
	created := actions[0].(clienttesting.CreateAction).GetObject().(*kubeovnv1.DNSNameResolver)
	require.Equal(t, kubeovnv1.DNSName("b.example"), created.Spec.Name)
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
