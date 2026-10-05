package controller

import (
	"fmt"
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

func TestEnqueueUpdatePodWhenRoutedChanges(t *testing.T) {
	pod := &corev1.Pod{
		Name:            "selected",
		Namespace:       metav1.NamespaceDefault,
		ResourceVersion: "1",
		Labels:          map[string]string{"app": "selected"},
		Annotations: map[string]string{
			util.LogicalSwitchAnnotation:                                    util.DefaultSubnet,
			fmt.Sprintf(util.AllocatedAnnotationTemplate, util.OvnProvider): "true",
		},
	}
	fake, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Pods: []*corev1.Pod{pod},
		Subnets: []*kubeovnv1.Subnet{{
			Name: util.DefaultSubnet,
		}},
	})
	require.NoError(t, err)
	controller := fake.fakeController
	controller.config.EnableNP = true
	controller.namedPort = NewNamedPort()
	controller.addOrUpdatePodQueue = newTypedRateLimitingQueue[string]("AddOrUpdatePod", nil)
	controller.updateNpQueue = newTypedRateLimitingQueue[string]("UpdateNetworkPolicy", nil)

	npIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, npIndexer.Add(&networkingv1.NetworkPolicy{
		Name:      "default-deny",
		Namespace: metav1.NamespaceDefault,
		Spec:      networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "selected"}}},
	}))
	controller.npsLister = networkinglister.NewNetworkPolicyLister(npIndexer)

	oldPod := pod.DeepCopy()
	newPod := pod.DeepCopy()
	newPod.ResourceVersion = "2"
	newPod.Annotations[fmt.Sprintf(util.RoutedAnnotationTemplate, util.OvnProvider)] = "true"

	controller.enqueueUpdatePod(oldPod, newPod)

	require.Equal(t, 1, controller.updateNpQueue.Len())
	item, shutdown := controller.updateNpQueue.Get()
	require.False(t, shutdown)
	controller.updateNpQueue.Done(item)
	require.Equal(t, "default/default-deny", item)
}
