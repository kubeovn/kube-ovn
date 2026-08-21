package daemon

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/scylladb/go-set/strset"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	listerv1 "k8s.io/client-go/listers/core/v1"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	k8sipset "k8s.io/kubernetes/pkg/proxy/ipvs/ipset"

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

type sequenceIPSet struct {
	k8sipset.Interface
	errors []error
	call   int
}

func (s *sequenceIPSet) ListSets() ([]string, error) {
	err := s.errors[s.call]
	s.call++
	return nil, err
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

func TestGatewayIPSetListFailureIsRecordedUntilSuccess(t *testing.T) {
	t.Parallel()

	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", UID: "node-uid"}}
	controller, recorder := newNodeEventTestController(t, node)
	failure := errors.New("failed to list ipsets")
	controller.k8sipsets = &sequenceIPSet{errors: []error{failure, failure, nil, failure}}
	reconcile := func() error {
		return controller.removeNatOutGoingPolicyRuleIPset(string(corev1.IPv4Protocol), strset.New())
	}

	require.ErrorIs(t, controller.reconcileNodeNetworkStage(node, "setIPSet", reconcile), failure)
	requireNodeEvent(t, recorder, "UpdateNodeFailed", "stage=setIPSet", failure.Error())
	require.ErrorIs(t, controller.reconcileNodeNetworkStage(node, "setIPSet", reconcile), failure)
	requireNoNodeEvent(t, recorder)
	require.NoError(t, controller.reconcileNodeNetworkStage(node, "setIPSet", reconcile))
	requireNoNodeEvent(t, recorder)
	require.ErrorIs(t, controller.reconcileNodeNetworkStage(node, "setIPSet", reconcile), failure)
	requireNodeEvent(t, recorder, "UpdateNodeFailed", "stage=setIPSet", failure.Error())
}

func TestRecordLocalNodeFailureSyncFallsBackToAPI(t *testing.T) {
	t.Parallel()

	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", UID: "node-uid"}}
	client := fake.NewSimpleClientset(node)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	controller := &Controller{
		config:      &Configuration{KubeClient: client, NodeName: node.Name},
		nodesLister: listerv1.NewNodeLister(indexer),
	}

	controller.recordLocalNodeFailureSync("checkOvn0Link", errors.New("ovn0 is down"))

	events, err := client.CoreV1().Events(metav1.NamespaceDefault).List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, events.Items, 1)
	require.Equal(t, node.UID, events.Items[0].InvolvedObject.UID)
	require.Contains(t, events.Items[0].Message, "stage=checkOvn0Link")
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

func TestInitNodeGatewayDeduplicatesInvalidAnnotations(t *testing.T) {
	invalidNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "node-1",
		UID:  "node-uid",
		Annotations: map[string]string{
			util.IPAddressAnnotation:  "invalid",
			util.CidrAnnotation:       "10.0.0.0/24",
			util.MacAddressAnnotation: "00:00:00:00:00:01",
			util.PortNameAnnotation:   "node-node-1",
			util.GatewayAnnotation:    "10.0.0.1",
		},
	}}
	replacementNode := invalidNode.DeepCopy()
	replacementNode.UID = "replacement-uid"
	validNode := replacementNode.DeepCopy()
	validNode.Annotations[util.IPAddressAnnotation] = "10.0.0.2"
	missingAddressNode := replacementNode.DeepCopy()
	delete(missingAddressNode.Annotations, util.IPAddressAnnotation)
	client := fake.NewSimpleClientset(validNode)
	getCalls := 0
	client.PrependReactor("get", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		getCalls++
		switch getCalls {
		case 1, 2:
			return true, invalidNode.DeepCopy(), nil
		case 3, 4:
			return true, replacementNode.DeepCopy(), nil
		case 6, 7:
			return true, missingAddressNode.DeepCopy(), nil
		default:
			return true, validNode.DeepCopy(), nil
		}
	})

	originalConfigureNodeGateway := configureNodeGateway
	originalRetryInterval := nodeGatewayInitRetryInterval
	configureNodeGateway = func(kubernetes.Interface, string, string, string, string, string, net.HardwareAddr, int, bool) error {
		return nil
	}
	nodeGatewayInitRetryInterval = 0
	t.Cleanup(func() {
		configureNodeGateway = originalConfigureNodeGateway
		nodeGatewayInitRetryInterval = originalRetryInterval
	})
	config := &Configuration{KubeClient: client, NodeName: validNode.Name}

	require.NoError(t, InitNodeGateway(config))
	require.NoError(t, InitNodeGateway(config))

	events, err := client.CoreV1().Events(metav1.NamespaceDefault).List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, events.Items, 3)
	for _, event := range events.Items {
		require.Equal(t, addNodeFailedReason, event.Reason)
		require.Contains(t, event.Message, "stage=validateOvn0Annotations")
	}
	require.Contains(t, events.Items[0].Message+events.Items[1].Message+events.Items[2].Message, "no ovn0 address")
}

func TestInitNodeGatewayRetriesFailedAnnotationEventWrite(t *testing.T) {
	invalidNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "node-1",
		UID:  "node-uid",
		Annotations: map[string]string{
			util.IPAddressAnnotation:  "invalid",
			util.CidrAnnotation:       "10.0.0.0/24",
			util.MacAddressAnnotation: "00:00:00:00:00:01",
			util.PortNameAnnotation:   "node-node-1",
			util.GatewayAnnotation:    "10.0.0.1",
		},
	}}
	validNode := invalidNode.DeepCopy()
	validNode.Annotations[util.IPAddressAnnotation] = "10.0.0.2"
	client := fake.NewSimpleClientset(validNode)
	getCalls := 0
	client.PrependReactor("get", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		getCalls++
		if getCalls <= 2 {
			return true, invalidNode.DeepCopy(), nil
		}
		return true, validNode.DeepCopy(), nil
	})
	eventCreateCalls := 0
	eventWriteFailure := errors.New("event API unavailable")
	client.PrependReactor("create", "events", func(k8stesting.Action) (bool, runtime.Object, error) {
		eventCreateCalls++
		if eventCreateCalls == 1 {
			return true, nil, eventWriteFailure
		}
		return false, nil, nil
	})

	originalConfigureNodeGateway := configureNodeGateway
	originalRetryInterval := nodeGatewayInitRetryInterval
	configureNodeGateway = func(kubernetes.Interface, string, string, string, string, string, net.HardwareAddr, int, bool) error {
		return nil
	}
	nodeGatewayInitRetryInterval = 0
	t.Cleanup(func() {
		configureNodeGateway = originalConfigureNodeGateway
		nodeGatewayInitRetryInterval = originalRetryInterval
	})

	require.NoError(t, InitNodeGateway(&Configuration{KubeClient: client, NodeName: validNode.Name}))
	require.Equal(t, 2, eventCreateCalls)
	events, err := client.CoreV1().Events(metav1.NamespaceDefault).List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, events.Items, 1)
	require.Contains(t, events.Items[0].Message, "stage=validateOvn0Annotations")
}

func TestInitNodeGatewayRecordsRouteFailureWhenGatewayIsReady(t *testing.T) {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "node-1",
		UID:  "node-uid",
		Annotations: map[string]string{
			util.IPAddressAnnotation:  "10.0.0.2",
			util.CidrAnnotation:       "10.0.0.0/24",
			util.MacAddressAnnotation: "00:00:00:00:00:01",
			util.PortNameAnnotation:   "node-node-1",
			util.GatewayAnnotation:    "10.0.0.1",
		},
	}}
	client := fake.NewSimpleClientset(node)
	routeFailure := errors.New("route replace failed")
	originalConfigureNodeGateway := configureNodeGateway
	originalReplaceNodeRoute := replaceNodeRoute
	originalCheckNodeGatewayReady := checkNodeGatewayReady
	configureNodeGateway = func(client kubernetes.Interface, nodeName, _, ip, gateway, _ string, _ net.HardwareAddr, _ int, enableNonPrimaryCNI bool) error {
		link := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: util.NodeNic, Index: 1}}
		return finishNodeGatewaySetup(client, nodeName, ip, gateway, link, []netlink.Route{{}}, enableNonPrimaryCNI)
	}
	replaceNodeRoute = func(*netlink.Route) error { return routeFailure }
	checkNodeGatewayReady = func(string, string, string, bool, bool, int, chan struct{}) error { return nil }
	t.Cleanup(func() {
		configureNodeGateway = originalConfigureNodeGateway
		replaceNodeRoute = originalReplaceNodeRoute
		checkNodeGatewayReady = originalCheckNodeGatewayReady
	})

	err := InitNodeGateway(&Configuration{KubeClient: client, NodeName: node.Name})

	require.ErrorIs(t, err, routeFailure)
	events, listErr := client.CoreV1().Events(metav1.NamespaceDefault).List(t.Context(), metav1.ListOptions{})
	require.NoError(t, listErr)
	require.Len(t, events.Items, 1)
	require.Equal(t, addNodeFailedReason, events.Items[0].Reason)
	require.Contains(t, events.Items[0].Message, routeFailure.Error())
	updatedNode, getErr := client.CoreV1().Nodes().Get(t.Context(), node.Name, metav1.GetOptions{})
	require.NoError(t, getErr)
	require.Len(t, updatedNode.Status.Conditions, 1)
	require.Equal(t, corev1.ConditionFalse, updatedNode.Status.Conditions[0].Status)
	require.Equal(t, "JoinSubnetGatewayReachable", updatedNode.Status.Conditions[0].Reason)
}

func TestUpdateNodeNetworkUnavailableConditionReturnsPatchFailure(t *testing.T) {
	t.Parallel()

	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}}
	client := fake.NewSimpleClientset(node)
	failure := errors.New("failed to patch node condition")
	client.PrependReactor("patch", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, failure
	})

	err := updateNodeNetworkUnavailableCondition(client, node.Name, "10.0.0.1", nil)

	require.ErrorIs(t, err, failure)
}
