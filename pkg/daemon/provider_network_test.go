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

func providerNetworkReadyNode(nodeName, providerNetwork, nic, mtu string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: nodeName,
		Labels: map[string]string{
			fmt.Sprintf(util.ProviderNetworkReadyTemplate, providerNetwork):     "true",
			fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, providerNetwork): nic,
			fmt.Sprintf(util.ProviderNetworkMtuTemplate, providerNetwork):       mtu,
		},
	}}
}

func TestEnqueueUpdateNodeRecordsProviderNetworkInitializationSuccessEvent(t *testing.T) {
	const providerNetworkName = "provider-network-1"
	pn := &kubeovnv1.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: providerNetworkName},
		Spec:       kubeovnv1.ProviderNetworkSpec{DefaultInterface: "eth1"},
	}

	tests := []struct {
		name    string
		oldNode *corev1.Node
	}{
		{
			name:    "readiness changed",
			oldNode: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
		},
		{
			name:    "interface changed",
			oldNode: providerNetworkReadyNode("node-1", providerNetworkName, "eth0", "1500"),
		},
		{
			name:    "MTU changed",
			oldNode: providerNetworkReadyNode("node-1", providerNetworkName, "eth1", "1400"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newNode := providerNetworkReadyNode("node-1", providerNetworkName, "eth1", "1500")
			controller, recorder := newProviderNetworkEventController(t, pn, tt.oldNode)

			controller.enqueueUpdateNode(tt.oldNode, newNode)
			requireProviderNetworkEvent(t, recorder,
				"Normal", "InitOVSBridgeSucceeded", "node=node-1", "interface=eth1", "mtu=1500")
		})
	}
}

func TestEnqueueUpdateNodeDoesNotRepeatProviderNetworkInitializationSuccessEvent(t *testing.T) {
	const providerNetworkName = "provider-network-1"
	pn := &kubeovnv1.ProviderNetwork{ObjectMeta: metav1.ObjectMeta{Name: providerNetworkName}}
	node := providerNetworkReadyNode("node-1", providerNetworkName, "eth1", "1500")
	controller, recorder := newProviderNetworkEventController(t, pn, node)

	controller.enqueueUpdateNode(node, node.DeepCopy())
	requireNoProviderNetworkEvent(t, recorder)
}
