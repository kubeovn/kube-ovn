package controller

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	networkinglister "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"
	v1alpha2 "sigs.k8s.io/network-policy-api/apis/v1alpha2"
	anplister "sigs.k8s.io/network-policy-api/pkg/client/listers/apis/v1alpha1"
	cnplister "sigs.k8s.io/network-policy-api/pkg/client/listers/apis/v1alpha2"

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
	require.NoError(t, controller.rebuildPolicyEventIndex())
	require.True(t, controller.policyEventIndex.isReady())

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

func TestEnqueueUpdatePodWhenLabelsStillMatchNetworkPolicy(t *testing.T) {
	pod := &corev1.Pod{
		Name:            "selected",
		Namespace:       metav1.NamespaceDefault,
		ResourceVersion: "1",
		Labels:          map[string]string{"app": "old"},
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
	t.Cleanup(controller.addOrUpdatePodQueue.ShutDown)
	controller.updateNpQueue = newTypedRateLimitingQueue[string]("UpdateNetworkPolicy", nil)
	t.Cleanup(controller.updateNpQueue.ShutDown)

	npIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, npIndexer.Add(&networkingv1.NetworkPolicy{
		Name:      "allow-selected",
		Namespace: metav1.NamespaceDefault,
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key: "app", Operator: metav1.LabelSelectorOpIn, Values: []string{"old", "new"},
			}}},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{From: []networkingv1.NetworkPolicyPeer{{
				PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "old"}},
			}}}},
		},
	}))
	for _, app := range []string{"old", "new"} {
		require.NoError(t, npIndexer.Add(&networkingv1.NetworkPolicy{
			Name:      "allow-" + app,
			Namespace: metav1.NamespaceDefault,
			Spec:      networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": app}}},
		}))
	}
	controller.npsLister = networkinglister.NewNetworkPolicyLister(npIndexer)
	require.NoError(t, controller.rebuildPolicyEventIndex())
	require.True(t, controller.policyEventIndex.isReady())

	oldPod := pod.DeepCopy()
	newPod := pod.DeepCopy()
	newPod.ResourceVersion = "2"
	newPod.Labels["app"] = "new"

	controller.enqueueUpdatePod(oldPod, newPod)

	require.Equal(t, 3, controller.updateNpQueue.Len())
	var policies []string
	for range 3 {
		item, shutdown := controller.updateNpQueue.Get()
		require.False(t, shutdown)
		controller.updateNpQueue.Done(item)
		policies = append(policies, item)
	}
	require.ElementsMatch(t, []string{"default/allow-selected", "default/allow-old", "default/allow-new"}, policies)
}

func TestEnqueueUpdatePodWhenNamedPortChanges(t *testing.T) {
	pod := &corev1.Pod{
		Name:            "selected",
		Namespace:       metav1.NamespaceDefault,
		ResourceVersion: "1",
		Labels:          map[string]string{"app": "selected"},
		Annotations: map[string]string{
			util.LogicalSwitchAnnotation:                                    util.DefaultSubnet,
			fmt.Sprintf(util.AllocatedAnnotationTemplate, util.OvnProvider): "true",
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 80}}}}},
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
	controller.namedPort.AddNamedPortByPod(pod)
	controller.addOrUpdatePodQueue = newTypedRateLimitingQueue[string]("AddOrUpdatePod", nil)
	controller.updateNpQueue = newTypedRateLimitingQueue[string]("UpdateNetworkPolicy", nil)
	t.Cleanup(controller.addOrUpdatePodQueue.ShutDown)
	t.Cleanup(controller.updateNpQueue.ShutDown)

	npIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, npIndexer.Add(&networkingv1.NetworkPolicy{
		Name:      "allow-selected",
		Namespace: metav1.NamespaceDefault,
		Spec:      networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "selected"}}},
	}))
	controller.npsLister = networkinglister.NewNetworkPolicyLister(npIndexer)
	require.NoError(t, controller.rebuildPolicyEventIndex())

	newPod := pod.DeepCopy()
	newPod.ResourceVersion = "2"
	newPod.Spec.Containers[0].Ports[0].ContainerPort = 8080
	controller.enqueueUpdatePod(pod, newPod)

	require.Equal(t, 1, controller.updateNpQueue.Len())
	item, shutdown := controller.updateNpQueue.Get()
	require.False(t, shutdown)
	controller.updateNpQueue.Done(item)
	require.Equal(t, "default/allow-selected", item)
	result := controller.namedPort.GetNamedPortByNs(metav1.NamespaceDefault)
	require.Equal(t, int32(8080), result["http"].PortID)
}

func TestEnqueueUpdatePodWhenNamedPortAddressChanges(t *testing.T) {
	pod := &corev1.Pod{
		Name:            "selected",
		Namespace:       "remote",
		ResourceVersion: "1",
		Labels:          map[string]string{"app": "selected"},
		Annotations: map[string]string{
			util.LogicalSwitchAnnotation:                                      util.DefaultSubnet,
			fmt.Sprintf(util.AllocatedAnnotationTemplate, util.OvnProvider):   "true",
			fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider):   "10.16.0.2",
			fmt.Sprintf(util.IPAddressAnnotationTemplate, "net1.default.ovn"): "10.17.0.2",
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 80}}}}},
	}
	fake, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Pods:       []*corev1.Pod{pod},
		Namespaces: []*corev1.Namespace{{Name: metav1.NamespaceDefault}, {Name: "remote"}},
		Subnets:    []*kubeovnv1.Subnet{{Name: util.DefaultSubnet}},
	})
	require.NoError(t, err)
	controller := fake.fakeController
	controller.config.EnableNP = true
	controller.namedPort = NewNamedPort()
	controller.namedPort.AddNamedPortByPod(pod)
	controller.addOrUpdatePodQueue = newTypedRateLimitingQueue[string]("AddOrUpdatePod", nil)
	controller.updateNpQueue = newTypedRateLimitingQueue[string]("UpdateNetworkPolicy", nil)
	t.Cleanup(controller.addOrUpdatePodQueue.ShutDown)
	t.Cleanup(controller.updateNpQueue.ShutDown)

	policyIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, policyIndexer.Add(&networkingv1.NetworkPolicy{
		Name:      "allow-http",
		Namespace: metav1.NamespaceDefault,
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				Ports: []networkingv1.NetworkPolicyPort{{Port: new(intstr.FromString("http"))}},
				To:    []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": "remote"}}}},
			}},
		},
	}))
	require.NoError(t, policyIndexer.Add(&networkingv1.NetworkPolicy{
		Name:      "remote-named",
		Namespace: "remote",
		Spec:      networkingv1.NetworkPolicySpec{Egress: []networkingv1.NetworkPolicyEgressRule{{Ports: []networkingv1.NetworkPolicyPort{{Port: new(intstr.FromString("http"))}}}}},
	}))
	controller.npsLister = networkinglister.NewNetworkPolicyLister(policyIndexer)

	newPod := pod.DeepCopy()
	newPod.ResourceVersion = "2"
	delete(newPod.Annotations, fmt.Sprintf(util.IPAddressAnnotationTemplate, "net1.default.ovn"))
	controller.enqueueUpdatePod(pod, newPod)

	require.Equal(t, 2, controller.updateNpQueue.Len())
	items := make([]string, 0, 2)
	for range 2 {
		item, shutdown := controller.updateNpQueue.Get()
		require.False(t, shutdown)
		controller.updateNpQueue.Done(item)
		items = append(items, item)
	}
	require.ElementsMatch(t, []string{"default/allow-http", "remote/remote-named"}, items)

	newerPod := newPod.DeepCopy()
	newerPod.ResourceVersion = "3"
	newerPod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, util.OvnProvider)] = "false"
	controller.enqueueUpdatePod(newPod, newerPod)
	require.Equal(t, 1, controller.updateNpQueue.Len())
}

func TestEnqueueUpdatePodWhenNamedPortAddressChangesRefreshesAdminPolicies(t *testing.T) {
	pod := &corev1.Pod{
		Name:            "selected",
		Namespace:       metav1.NamespaceDefault,
		ResourceVersion: "1",
		Annotations: map[string]string{
			util.LogicalSwitchAnnotation:                                    util.DefaultSubnet,
			fmt.Sprintf(util.AllocatedAnnotationTemplate, util.OvnProvider): "true",
			fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider): "10.16.0.2",
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 80}}}}},
	}
	fake, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{{Name: util.DefaultSubnet}}})
	require.NoError(t, err)
	controller := fake.fakeController
	controller.config.EnableANP = true
	controller.namedPort = NewNamedPort()
	controller.namedPort.AddNamedPortByPod(pod)
	controller.addOrUpdatePodQueue = newTypedRateLimitingQueue[string]("AddOrUpdatePod", nil)
	controller.addAnpQueue = newTypedRateLimitingQueue[string]("AddAdminNetworkPolicy", nil)
	controller.addBanpQueue = newTypedRateLimitingQueue[string]("AddBaselineAdminNetworkPolicy", nil)
	controller.addCnpQueue = newTypedRateLimitingQueue[string]("AddClusterNetworkPolicy", nil)
	for _, queue := range []interface{ ShutDown() }{controller.addOrUpdatePodQueue, controller.addAnpQueue, controller.addBanpQueue, controller.addCnpQueue} {
		t.Cleanup(queue.ShutDown)
	}

	portName := "http"
	anpIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, anpIndexer.Add(&v1alpha1.AdminNetworkPolicy{Name: "anp", Spec: v1alpha1.AdminNetworkPolicySpec{
		Subject: v1alpha1.AdminNetworkPolicySubject{Namespaces: &metav1.LabelSelector{}},
		Ingress: []v1alpha1.AdminNetworkPolicyIngressRule{{Ports: &[]v1alpha1.AdminNetworkPolicyPort{{NamedPort: &portName}}}},
	}}))
	banpIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, banpIndexer.Add(&v1alpha1.BaselineAdminNetworkPolicy{Name: "banp", Spec: v1alpha1.BaselineAdminNetworkPolicySpec{
		Subject: v1alpha1.AdminNetworkPolicySubject{Namespaces: &metav1.LabelSelector{}},
		Ingress: []v1alpha1.BaselineAdminNetworkPolicyIngressRule{{Ports: &[]v1alpha1.AdminNetworkPolicyPort{{NamedPort: &portName}}}},
	}}))
	cnpIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, cnpIndexer.Add(&v1alpha2.ClusterNetworkPolicy{Name: "cnp", Spec: v1alpha2.ClusterNetworkPolicySpec{
		Subject: v1alpha2.ClusterNetworkPolicySubject{Namespaces: &metav1.LabelSelector{}},
		Ingress: []v1alpha2.ClusterNetworkPolicyIngressRule{{Protocols: []v1alpha2.ClusterNetworkPolicyProtocol{{DestinationNamedPort: portName}}}},
	}}))
	controller.anpsLister = anplister.NewAdminNetworkPolicyLister(anpIndexer)
	controller.banpsLister = anplister.NewBaselineAdminNetworkPolicyLister(banpIndexer)
	controller.cnpsLister = cnplister.NewClusterNetworkPolicyLister(cnpIndexer)

	newPod := pod.DeepCopy()
	newPod.ResourceVersion = "2"
	newPod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, util.OvnProvider)] = "10.16.0.3"
	controller.enqueueUpdatePod(pod, newPod)

	require.Equal(t, 1, controller.addAnpQueue.Len())
	require.Equal(t, 1, controller.addBanpQueue.Len())
	require.Equal(t, 1, controller.addCnpQueue.Len())
}

func TestEnqueueNamespaceNamedPortPoliciesForUnselectedPod(t *testing.T) {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, indexer.Add(&networkingv1.NetworkPolicy{
		Name: "named", Namespace: "default",
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "selected"}},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{Ports: []networkingv1.NetworkPolicyPort{{
				Port: new(intstr.FromString("http")),
			}}}},
		},
	}))
	require.NoError(t, indexer.Add(&networkingv1.NetworkPolicy{
		Name: "numeric", Namespace: "default",
		Spec: networkingv1.NetworkPolicySpec{Egress: []networkingv1.NetworkPolicyEgressRule{{Ports: []networkingv1.NetworkPolicyPort{{
			Port: new(intstr.FromInt32(80)),
		}}}}},
	}))
	controller := &Controller{
		npsLister:     networkinglister.NewNetworkPolicyLister(indexer),
		updateNpQueue: newTypedRateLimitingQueue[string]("UpdateNetworkPolicy", nil),
	}
	t.Cleanup(controller.updateNpQueue.ShutDown)

	controller.enqueueNamespaceNamedPortNetworkPolicies("default")
	require.Equal(t, 1, controller.updateNpQueue.Len())
	item, shutdown := controller.updateNpQueue.Get()
	require.False(t, shutdown)
	controller.updateNpQueue.Done(item)
	require.Equal(t, "default/named", item)
}

func TestPodMatchesGlobalNamedEgressPolicy(t *testing.T) {
	remote := &corev1.Namespace{Name: "remote"}
	pod := &corev1.Pod{
		Name: "backend", Namespace: remote.Name,
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}}}}},
	}
	policy := &networkingv1.NetworkPolicy{
		Name: "allow-http", Namespace: metav1.NamespaceDefault,
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			Egress:      []networkingv1.NetworkPolicyEgressRule{{Ports: []networkingv1.NetworkPolicyPort{{Port: new(intstr.FromString("http"))}}}},
		},
	}
	fake, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Namespaces: []*corev1.Namespace{{Name: metav1.NamespaceDefault}, remote}})
	require.NoError(t, err)
	controller := fake.fakeController
	controller.config.EnableNP = true
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, indexer.Add(policy))
	controller.npsLister = networkinglister.NewNetworkPolicyLister(indexer)
	require.NoError(t, controller.rebuildPolicyEventIndex())
	require.Equal(t, []string{"default/allow-http"}, controller.podMatchNetworkPolicies(pod))
	pod.Spec.Containers[0].Ports[0].Protocol = corev1.ProtocolUDP
	require.Empty(t, controller.podMatchNetworkPolicies(pod))
}
