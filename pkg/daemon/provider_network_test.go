package daemon

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	clienttesting "k8s.io/client-go/testing"
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

func TestFinishProviderNetworkInitReadiness(t *testing.T) {
	for _, tt := range []struct {
		name       string
		needsIP    bool
		patchFails bool
	}{
		{name: "missing allocation clears stale readiness", needsIP: true},
		{name: "patch failure preserves initialization error", needsIP: true, patchFails: true},
		{name: "successful initialization keeps readiness"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pn := &kubeovnv1.ProviderNetwork{
				Name: "provider",
				Spec: kubeovnv1.ProviderNetworkSpec{
					DefaultInterface: "eth0.100", DefaultInterfaceSubnet: "underlay",
					AutoCreateVlanSubinterfaces: tt.needsIP,
				},
			}
			node := providerNetworkReadyNode("node-1", pn.Name, pn.Spec.DefaultInterface, "1500")
			node.Labels["unrelated"] = "keep"
			node.Labels[fmt.Sprintf(util.ProviderNetworkReadyTemplate, "other-provider")] = "true"
			node.Labels[fmt.Sprintf(util.ProviderNetworkVlanIntTemplate, pn.Name)] = "true"
			originalNode := node.DeepCopy()
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "cni", Namespace: "kube-system"}}
			client := fake.NewSimpleClientset(node.DeepCopy(), pod)
			podIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
			require.NoError(t, podIndexer.Add(pod))
			controller := &Controller{
				config:     &Configuration{KubeClient: client, PodName: pod.Name, PodNamespace: pod.Namespace},
				podsLister: corelisters.NewPodLister(podIndexer),
			}
			if tt.patchFails {
				client.PrependReactor("patch", "nodes", func(clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("node patch unavailable")
				})
			}
			patch := util.KVPatch{
				fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name):     nil,
				fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, pn.Name): nil,
				fmt.Sprintf(util.ProviderNetworkMtuTemplate, pn.Name):       nil,
				fmt.Sprintf(util.ProviderNetworkExcludeTemplate, pn.Name):   nil,
				fmt.Sprintf(util.ProviderNetworkVlanIntTemplate, pn.Name):   nil,
			}
			err := controller.finishProviderNetworkInit(pn, node, pn.Spec.DefaultInterface, 1500, false, patch)
			if tt.needsIP {
				require.EqualError(t, err, "VLAN subinterface address for node node-1 has not been allocated yet")
			} else {
				require.NoError(t, err)
			}
			stored, getErr := client.CoreV1().Nodes().Get(context.Background(), node.Name, metav1.GetOptions{})
			require.NoError(t, getErr)
			readyLabel := fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name)
			if tt.needsIP && !tt.patchFails {
				for _, template := range []string{util.ProviderNetworkReadyTemplate, util.ProviderNetworkInterfaceTemplate,
					util.ProviderNetworkMtuTemplate, util.ProviderNetworkVlanIntTemplate} {
					require.NotContains(t, stored.Labels, fmt.Sprintf(template, pn.Name))
				}
			} else {
				require.Equal(t, "true", stored.Labels[readyLabel])
			}
			require.Equal(t, "keep", stored.Labels["unrelated"])
			require.Equal(t, "true", stored.Labels[fmt.Sprintf(util.ProviderNetworkReadyTemplate, "other-provider")])
			require.Equal(t, originalNode, node, "informer-backed node must not be mutated")
			storedPod, getErr := client.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
			require.NoError(t, getErr)
			if tt.needsIP {
				require.Equal(t, err.Error(), storedPod.Annotations[fmt.Sprintf(util.ProviderNetworkErrMessageTemplate, pn.Name)])
			}
			nodePatches := 0
			for _, action := range client.Actions() {
				if action.GetVerb() == "patch" && action.GetResource().Resource == "nodes" {
					nodePatches++
				}
			}
			require.Equal(t, 1, nodePatches, "successful sync must not reset readiness before setting it")
		})
	}
}

func TestDefaultInterfaceSubnetDoesNotApplyToCustomInterfaces(t *testing.T) {
	for _, nic := range []string{"eth1.200", "eth0.100"} {
		t.Run(nic, func(t *testing.T) {
			pn := &kubeovnv1.ProviderNetwork{
				Spec: kubeovnv1.ProviderNetworkSpec{
					DefaultInterface:            "eth0.100",
					AutoCreateVlanSubinterfaces: true,
					DefaultInterfaceSubnet:      "underlay",
					CustomInterfaces:            []kubeovnv1.CustomInterface{{Interface: nic, Nodes: []string{"node-1"}}},
				},
			}
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}}
			// No allocation, API access, or netlink operations are needed for a custom interface.
			require.NoError(t, (&Controller{}).configureProviderNetworkVlanSubinterfaceAddress(pn, node))
		})
	}
}

func TestConfigureProviderNetworkVlanSubinterfaceAddressWaitsForAllocation(t *testing.T) {
	pn := &kubeovnv1.ProviderNetwork{
		Spec: kubeovnv1.ProviderNetworkSpec{
			DefaultInterface:            "eth0.100",
			AutoCreateVlanSubinterfaces: true,
			DefaultInterfaceSubnet:      "underlay",
		},
	}

	err := (&Controller{}).configureProviderNetworkVlanSubinterfaceAddress(pn, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}})
	require.EqualError(t, err, "VLAN subinterface address for node node-1 has not been allocated yet")
}

func TestHandleAddOrUpdateProviderNetworkRecordsValidationFailureEvent(t *testing.T) {
	pn := &kubeovnv1.ProviderNetwork{
		Name: "provider-network-1",
		Spec: kubeovnv1.ProviderNetworkSpec{
			DefaultInterface: "eth1",
			NodeSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key:      "network-role",
				Operator: metav1.LabelSelectorOperator("Invalid"),
			}}},
		},
	}
	node := &corev1.Node{Name: "node-1"}
	controller, recorder := newProviderNetworkEventController(t, pn, node)

	require.Error(t, controller.handleAddOrUpdateProviderNetwork(pn.Name))
	requireProviderNetworkEvent(t, recorder,
		"Warning", "InitOVSBridgeFailed", "node=node-1", "interface=eth1", "failed to check nodeSelector")
}

func TestHandleAddOrUpdateProviderNetworkRecordsInitializationFailureEvent(t *testing.T) {
	pn := &kubeovnv1.ProviderNetwork{
		Name:   "provider-network-1",
		Spec:   kubeovnv1.ProviderNetworkSpec{DefaultInterface: "eth1"},
		Status: kubeovnv1.ProviderNetworkStatus{Vlans: []string{"vlan-1"}},
	}
	node := &corev1.Node{Name: "node-1"}
	controller, recorder := newProviderNetworkEventController(t, pn, node)
	controller.vlansLister = errorVlanLister{err: errors.New("cache unavailable")}

	require.EqualError(t, controller.handleAddOrUpdateProviderNetwork(pn.Name), "cache unavailable")
	requireProviderNetworkEvent(t, recorder,
		"Warning", "InitOVSBridgeFailed", "node=node-1", "interface=eth1", "cache unavailable")
}

func providerNetworkReadyNode(nodeName, providerNetwork, nic, mtu string) *corev1.Node {
	return &corev1.Node{
		Name: nodeName,
		Labels: map[string]string{
			fmt.Sprintf(util.ProviderNetworkReadyTemplate, providerNetwork):     "true",
			fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, providerNetwork): nic,
			fmt.Sprintf(util.ProviderNetworkMtuTemplate, providerNetwork):       mtu,
		},
	}
}

func TestEnqueueUpdateNodeRecordsProviderNetworkInitializationSuccessEvent(t *testing.T) {
	const providerNetworkName = "provider-network-1"
	pn := &kubeovnv1.ProviderNetwork{
		Name: providerNetworkName,
		Spec: kubeovnv1.ProviderNetworkSpec{DefaultInterface: "eth1"},
	}

	tests := []struct {
		name    string
		oldNode *corev1.Node
	}{
		{
			name:    "readiness changed",
			oldNode: &corev1.Node{Name: "node-1"},
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
	pn := &kubeovnv1.ProviderNetwork{Name: providerNetworkName}
	node := providerNetworkReadyNode("node-1", providerNetworkName, "eth1", "1500")
	controller, recorder := newProviderNetworkEventController(t, pn, node)

	controller.enqueueUpdateNode(node, node.DeepCopy())
	requireNoProviderNetworkEvent(t, recorder)
}
