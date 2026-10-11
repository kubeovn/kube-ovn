package controller

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	networkinglister "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestEnqueueUpdateNamespaceWhenLabelsStillMatchNetworkPolicy(t *testing.T) {
	ns := &corev1.Namespace{
		Name:            "client",
		ResourceVersion: "1",
		Labels:          map[string]string{"tenant": "old"},
		Annotations:     map[string]string{util.LogicalSwitchAnnotation: util.DefaultSubnet},
	}
	fake, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Namespaces: []*corev1.Namespace{ns}})
	require.NoError(t, err)
	controller := fake.fakeController
	controller.config.EnableNP = true
	controller.updateNpQueue = newTypedRateLimitingQueue[string]("UpdateNetworkPolicy", nil)
	controller.addNamespaceQueue = newTypedRateLimitingQueue[string]("AddNamespace", nil)

	npIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, npIndexer.Add(&networkingv1.NetworkPolicy{
		Name:      "allow-client",
		Namespace: metav1.NamespaceDefault,
		Spec: networkingv1.NetworkPolicySpec{Ingress: []networkingv1.NetworkPolicyIngressRule{{From: []networkingv1.NetworkPolicyPeer{{
			NamespaceSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key: "tenant", Operator: metav1.LabelSelectorOpExists,
			}}},
		}}}}},
	}))
	controller.npsLister = networkinglister.NewNetworkPolicyLister(npIndexer)

	oldNs := ns.DeepCopy()
	newNs := ns.DeepCopy()
	newNs.ResourceVersion = "2"
	newNs.Labels["tenant"] = "new"

	controller.enqueueUpdateNamespace(oldNs, newNs)

	require.Equal(t, 1, controller.updateNpQueue.Len())
	item, shutdown := controller.updateNpQueue.Get()
	require.False(t, shutdown)
	controller.updateNpQueue.Done(item)
	require.Equal(t, "default/allow-client", item)
}

// Test_handleAddNamespace_orphanedSubnet is a regression guard for the bug where
// a subnet referencing a non-existent VPC aborted evaluation of the whole subnet
// list. The orphan handling used to `break` the `for _, s := range subnets` loop,
// so any valid subnet returned by the lister after the orphaned one was never
// bound to the namespace. The fix replaces `break` with `continue`, so the valid
// subnet must always be bound regardless of the (random) lister iteration order.
func Test_handleAddNamespace_orphanedSubnet(t *testing.T) {
	const nsName = "test-ns"
	ns := &corev1.Namespace{
		Name: nsName,
	}
	// A broken subnet whose referenced VPC does not exist - it must not stop the
	// loop from reaching the valid subnet below.
	orphanSubnet := &kubeovnv1.Subnet{
		Name: "orphan-subnet",
		Spec: kubeovnv1.SubnetSpec{
			Vpc:       "ghost-vpc",
			CIDRBlock: "10.16.0.0/16",
		},
	}
	// A valid subnet that binds the namespace via Spec.Namespaces.
	validSubnet := &kubeovnv1.Subnet{
		Name: "valid-subnet",
		Spec: kubeovnv1.SubnetSpec{
			Namespaces: []string{nsName},
			CIDRBlock:  "10.17.0.0/16",
		},
	}

	fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Namespaces: []*corev1.Namespace{ns},
		Subnets:    []*kubeovnv1.Subnet{orphanSubnet, validSubnet},
	})
	require.NoError(t, err)
	ctrl := fakeCtrl.fakeController

	require.NoError(t, ctrl.handleAddNamespace(nsName))

	got, err := ctrl.config.KubeClient.CoreV1().Namespaces().Get(context.Background(), nsName, metav1.GetOptions{})
	require.NoError(t, err)

	lss := strings.Split(got.Annotations[util.LogicalSwitchAnnotation], ",")
	require.Contains(t, lss, validSubnet.Name, "valid subnet must be bound even when an orphaned subnet is present")
	require.NotContains(t, lss, orphanSubnet.Name, "broken subnet must not be bound to the namespace")
}
