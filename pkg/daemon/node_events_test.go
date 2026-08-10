package daemon

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func requireNodeEvent(t *testing.T, recorder *record.FakeRecorder, parts ...string) {
	t.Helper()

	select {
	case event := <-recorder.Events:
		for _, part := range parts {
			require.Contains(t, event, part)
		}
	case <-time.After(time.Second):
		t.Fatal("expected node event")
	}
}

func requireNoNodeEvent(t *testing.T, recorder *record.FakeRecorder) {
	t.Helper()

	select {
	case event := <-recorder.Events:
		t.Fatalf("unexpected node event: %s", event)
	default:
	}
}

func newNodeEventTestController(t *testing.T, node *corev1.Node) (*Controller, *record.FakeRecorder) {
	t.Helper()

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, indexer.Add(node))
	recorder := record.NewFakeRecorder(10)
	return &Controller{
		config:      &Configuration{NodeName: node.Name},
		nodesLister: listerv1.NewNodeLister(indexer),
		recorder:    recorder,
	}, recorder
}

func TestHandleUpdateNodeRecordsFailureEvent(t *testing.T) {
	t.Parallel()

	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "node-1",
		Annotations: map[string]string{
			util.NodeNetworksAnnotation: "invalid json",
		},
	}}
	controller, recorder := newNodeEventTestController(t, node)

	err := controller.handleUpdateNode(node.Name)

	require.Error(t, err)
	requireNodeEvent(t, recorder, "Warning", "UpdateNodeFailed", "stage=updateNodeNetworks", err.Error())
}

func TestReconcileNodeNetworkStageDeduplicatesFailuresUntilSuccess(t *testing.T) {
	t.Parallel()

	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", UID: "old-uid"}}
	controller, recorder := newNodeEventTestController(t, node)
	failure := errors.New("iptables failed")

	require.ErrorIs(t, controller.reconcileNodeNetworkStage(node, "setIptables", func() error { return failure }), failure)
	requireNodeEvent(t, recorder, "Warning", "UpdateNodeFailed", "stage=setIptables", failure.Error())

	require.ErrorIs(t, controller.reconcileNodeNetworkStage(node, "setIptables", func() error { return failure }), failure)
	requireNoNodeEvent(t, recorder)

	replacementNode := node.DeepCopy()
	replacementNode.UID = "new-uid"
	require.ErrorIs(t, controller.reconcileNodeNetworkStage(replacementNode, "setIptables", func() error { return failure }), failure)
	requireNodeEvent(t, recorder, "Warning", "UpdateNodeFailed", "stage=setIptables", failure.Error())
	require.Len(t, controller.nodeFailures, 1)

	require.NoError(t, controller.reconcileNodeNetworkStage(replacementNode, "setIptables", func() error { return nil }))
	requireNoNodeEvent(t, recorder)
	require.Empty(t, controller.nodeFailures)

	require.ErrorIs(t, controller.reconcileNodeNetworkStage(replacementNode, "setIptables", func() error { return failure }), failure)
	requireNodeEvent(t, recorder, "Warning", "UpdateNodeFailed", "stage=setIptables", failure.Error())
}

func TestInitNodeGatewayRecordsFailureEvent(t *testing.T) {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "node-1",
		Annotations: map[string]string{
			util.IPAddressAnnotation:  "10.0.0.2",
			util.CidrAnnotation:       "10.0.0.0/24",
			util.MacAddressAnnotation: "00:00:00:00:00:01",
			util.PortNameAnnotation:   "node-node-1",
			util.GatewayAnnotation:    "10.0.0.1",
		},
	}}
	failure := errors.New("configure ovn0 failed")
	originalConfigureNodeGateway := configureNodeGateway
	configureNodeGateway = func(kubernetes.Interface, string, string, string, string, string, net.HardwareAddr, int, bool) error {
		return failure
	}
	t.Cleanup(func() { configureNodeGateway = originalConfigureNodeGateway })

	client := fake.NewSimpleClientset(node)
	err := InitNodeGateway(&Configuration{
		KubeClient: client,
		NodeName:   node.Name,
	})

	require.ErrorIs(t, err, failure)
	events, listErr := client.CoreV1().Events(metav1.NamespaceDefault).List(t.Context(), metav1.ListOptions{})
	require.NoError(t, listErr)
	require.Len(t, events.Items, 1)
	require.Equal(t, corev1.EventTypeWarning, events.Items[0].Type)
	require.Equal(t, addNodeFailedReason, events.Items[0].Reason)
	require.Contains(t, events.Items[0].Message, "stage=initializeOvn0")
	require.Contains(t, events.Items[0].Message, failure.Error())
}
