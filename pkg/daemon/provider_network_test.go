package daemon

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type errorVlanLister struct {
	err error
}

func (l errorVlanLister) List(labels.Selector) ([]*kubeovnv1.Vlan, error) {
	return nil, l.err
}

func (l errorVlanLister) Get(string) (*kubeovnv1.Vlan, error) {
	return nil, l.err
}

func newProviderNetworkEventController(t *testing.T, pn *kubeovnv1.ProviderNetwork, node *corev1.Node) (*Controller, *record.FakeRecorder) {
	t.Helper()

	providerNetworkIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, providerNetworkIndexer.Add(pn))
	nodeIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, nodeIndexer.Add(node))
	recorder := record.NewFakeRecorder(10)

	return &Controller{
		config:                 &Configuration{NodeName: node.Name},
		providerNetworksLister: kubeovnlisters.NewProviderNetworkLister(providerNetworkIndexer),
		nodesLister:            corelisters.NewNodeLister(nodeIndexer),
		recorder:               recorder,
	}, recorder
}

func requireProviderNetworkEvent(t *testing.T, recorder *record.FakeRecorder, parts ...string) {
	t.Helper()

	select {
	case event := <-recorder.Events:
		for _, part := range parts {
			require.Contains(t, event, part)
		}
	case <-time.After(time.Second):
		t.Fatal("expected provider network event")
	}
}

func requireNoProviderNetworkEvent(t *testing.T, recorder *record.FakeRecorder) {
	t.Helper()

	select {
	case event := <-recorder.Events:
		t.Fatalf("unexpected provider network event: %s", event)
	default:
	}
}

func TestHandleAddOrUpdateProviderNetworkRecordsValidationFailureEvent(t *testing.T) {
	pn := &kubeovnv1.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "provider-network-1"},
		Spec: kubeovnv1.ProviderNetworkSpec{
			DefaultInterface: "eth1",
			NodeSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key:      "network-role",
				Operator: metav1.LabelSelectorOperator("Invalid"),
			}}},
		},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}}
	controller, recorder := newProviderNetworkEventController(t, pn, node)

	require.Error(t, controller.handleAddOrUpdateProviderNetwork(pn.Name))
	requireProviderNetworkEvent(t, recorder,
		"Warning", "InitOVSBridgeFailed", "node=node-1", "interface=eth1", "failed to check nodeSelector")
}

func TestHandleAddOrUpdateProviderNetworkRecordsInitializationFailureEvent(t *testing.T) {
	pn := &kubeovnv1.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "provider-network-1"},
		Spec:       kubeovnv1.ProviderNetworkSpec{DefaultInterface: "eth1"},
		Status:     kubeovnv1.ProviderNetworkStatus{Vlans: []string{"vlan-1"}},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}}
	controller, recorder := newProviderNetworkEventController(t, pn, node)
	controller.vlansLister = errorVlanLister{err: errors.New("cache unavailable")}

	require.EqualError(t, controller.handleAddOrUpdateProviderNetwork(pn.Name), "cache unavailable")
	requireProviderNetworkEvent(t, recorder,
		"Warning", "InitOVSBridgeFailed", "node=node-1", "interface=eth1", "cache unavailable")
}

func TestHandleAddOrUpdateProviderNetworkRecordsInitializationSuccessEvent(t *testing.T) {
	pn := &kubeovnv1.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "provider-network-1"},
		Spec:       kubeovnv1.ProviderNetworkSpec{DefaultInterface: "eth1"},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}}
	controller, recorder := newProviderNetworkEventController(t, pn, node)

	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "kube-ovn-cni-node-1", Namespace: metav1.NamespaceSystem}}
	podIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, podIndexer.Add(pod))
	controller.podsLister = corelisters.NewPodLister(podIndexer)
	controller.config.KubeClient = fake.NewSimpleClientset(node, pod)
	controller.config.PodName = pod.Name
	controller.config.PodNamespace = pod.Namespace

	originalOVSInitProviderNetwork := ovsInitProviderNetworkFn
	ovsInitProviderNetworkFn = func(_ *Controller, provider, nic string, trunks []string, exchangeLinkName, macLearningFallback bool, vlanInterfaceMap map[string]int) (int, error) {
		require.Equal(t, pn.Name, provider)
		require.Equal(t, pn.Spec.DefaultInterface, nic)
		require.ElementsMatch(t, []string{"0"}, trunks)
		require.False(t, exchangeLinkName)
		require.False(t, macLearningFallback)
		require.Empty(t, vlanInterfaceMap)
		return 1500, nil
	}
	t.Cleanup(func() { ovsInitProviderNetworkFn = originalOVSInitProviderNetwork })

	require.NoError(t, controller.handleAddOrUpdateProviderNetwork(pn.Name))
	requireProviderNetworkEvent(t, recorder,
		"Normal", "InitOVSBridgeSucceeded", "node=node-1", "interface=eth1", "mtu=1500")
}

func TestHandleAddOrUpdateProviderNetworkDoesNotRepeatSuccessEvent(t *testing.T) {
	const providerNetworkName = "provider-network-1"
	pn := &kubeovnv1.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: providerNetworkName},
		Spec:       kubeovnv1.ProviderNetworkSpec{DefaultInterface: "eth1"},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "node-1",
		Labels: map[string]string{
			fmt.Sprintf(util.ProviderNetworkReadyTemplate, providerNetworkName):     "true",
			fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, providerNetworkName): "eth1",
			fmt.Sprintf(util.ProviderNetworkMtuTemplate, providerNetworkName):       "1500",
		},
	}}
	controller, recorder := newProviderNetworkEventController(t, pn, node)

	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "kube-ovn-cni-node-1", Namespace: metav1.NamespaceSystem}}
	podIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, podIndexer.Add(pod))
	controller.podsLister = corelisters.NewPodLister(podIndexer)
	controller.config.KubeClient = fake.NewSimpleClientset(node, pod)
	controller.config.PodName = pod.Name
	controller.config.PodNamespace = pod.Namespace

	originalOVSInitProviderNetwork := ovsInitProviderNetworkFn
	ovsInitProviderNetworkFn = func(_ *Controller, _, _ string, _ []string, _, _ bool, _ map[string]int) (int, error) {
		return 1500, nil
	}
	t.Cleanup(func() { ovsInitProviderNetworkFn = originalOVSInitProviderNetwork })

	require.NoError(t, controller.handleAddOrUpdateProviderNetwork(pn.Name))
	requireNoProviderNetworkEvent(t, recorder)
}
