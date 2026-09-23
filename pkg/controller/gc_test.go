package controller

import (
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/scylladb/go-set/strset"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	corelisters "k8s.io/client-go/listers/core/v1"
	netlisters "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func newLogicalRouterPort(lrName, lrpName, mac string, networks []string) *ovnnb.LogicalRouterPort {
	return &ovnnb.LogicalRouterPort{
		Name:     lrpName,
		MAC:      mac,
		Networks: networks,
		ExternalIDs: map[string]string{
			"lr":     lrName,
			"vendor": util.CniTypeName,
		},
	}
}

func Test_logicalRouterPortFilter(t *testing.T) {
	t.Parallel()

	exceptPeerPorts := strset.New(
		"except-lrp-0",
		"except-lrp-1",
	)

	lrpNames := []string{"other-0", "other-1", "other-2", "except-lrp-0", "except-lrp-1"}
	lrps := make([]*ovnnb.LogicalRouterPort, 0)
	for _, lrpName := range lrpNames {
		lrp := newLogicalRouterPort("", lrpName, "", nil)
		peer := lrpName + "-peer"
		lrp.Peer = &peer
		lrps = append(lrps, lrp)
	}

	filterFunc := logicalRouterPortFilter(exceptPeerPorts)

	for _, lrp := range lrps {
		if exceptPeerPorts.Has(lrp.Name) {
			require.False(t, filterFunc(lrp))
		} else {
			require.True(t, filterFunc(lrp))
		}
	}
}

func TestGcSecurityGroupSkipsVpcEgressGatewayPortGroup(t *testing.T) {
	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient

	mockOvnClient.EXPECT().ListPortGroups(map[string]string{"vendor": util.CniTypeName}).Return([]ovnnb.PortGroup{{
		Name: "VEG.0b5177562709",
		ExternalIDs: map[string]string{
			"af":                           "4",
			ovs.ExternalIDVendor:           util.CniTypeName,
			ovs.ExternalIDVpcEgressGateway: "default/egress-ha-a",
		},
	}}, nil)
	mockOvnClient.EXPECT().DeletePortGroup(gomock.Any()).Times(0)

	require.NoError(t, ctrl.gcSecurityGroup())
}

func TestGcNetworkPolicyQueuesNormalizedPortGroupDeletion(t *testing.T) {
	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	ctrl.config.EnableNP = true
	ctrl.deleteNpQueue = newTypedRateLimitingQueue[networkPolicyDeleteRequest]("TestDeleteNetworkPolicy", nil)
	ctrl.npsLister = netlisters.NewNetworkPolicyLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))
	ctrl.nodesLister = corelisters.NewNodeLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))
	ctrl.subnetsLister = kubeovnlisters.NewSubnetLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))
	t.Cleanup(ctrl.deleteNpQueue.ShutDown)

	fakeController.mockOvnClient.EXPECT().ListPortGroups(map[string]string{networkPolicyKey: ""}).Return([]ovnnb.PortGroup{{
		Name:        "np1test.default",
		ExternalIDs: map[string]string{networkPolicyKey: "default/np1test"},
	}}, nil)
	// Enabled NetworkPolicy cleanup runs through the serialized delete worker;
	// GC must not delete a port group after a replacement policy appears.
	fakeController.mockOvnClient.EXPECT().DeletePortGroup().Return(nil)

	require.NoError(t, ctrl.gcNetworkPolicy())
	require.Equal(t, 1, ctrl.deleteNpQueue.Len())
	request, shutdown := ctrl.deleteNpQueue.Get()
	require.False(t, shutdown)
	ctrl.deleteNpQueue.Done(request)
	require.Equal(t, "default/np1test", request.key)
	require.Equal(t, "np1test.default", request.portGroupName)
}

func TestMarkAndCleanLSPEnqueuesMissingNodeLSP(t *testing.T) {
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Nodes: []*corev1.Node{{
			Name: "node-1",
			Annotations: map[string]string{
				util.AllocatedAnnotation: "true",
			},
		}},
	})
	require.NoError(t, err)

	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient
	ctrl.config.EnableKeepVMIP = false
	ctrl.addNodeQueue = newTypedRateLimitingQueue[string]("AddNode", nil)
	ctrl.virtualIpsLister = kubeovnlisters.NewVipLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))
	ctrl.ovnEipsLister = kubeovnlisters.NewOvnEipLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))
	mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(false, nil).Return(nil, nil)

	require.NoError(t, ctrl.markAndCleanLSP())
	require.Equal(t, 1, ctrl.addNodeQueue.Len())
}

func TestMarkAndCleanLSPKeepsExistingNodeLSP(t *testing.T) {
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Nodes: []*corev1.Node{{
			Name: "node-1",
			Annotations: map[string]string{
				util.AllocatedAnnotation: "true",
			},
		}},
	})
	require.NoError(t, err)

	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient
	ctrl.config.EnableKeepVMIP = false
	ctrl.addNodeQueue = newTypedRateLimitingQueue[string]("AddNode", nil)
	ctrl.virtualIpsLister = kubeovnlisters.NewVipLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))
	ctrl.ovnEipsLister = kubeovnlisters.NewOvnEipLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))

	previousLastNoPodLSP := lastNoPodLSP
	lastNoPodLSP = strset.New()
	t.Cleanup(func() { lastNoPodLSP = previousLastNoPodLSP })

	mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(false, nil).Return([]ovnnb.LogicalSwitchPort{
		{Name: util.NodeLspName("node-1")},
		{Name: "orphan"},
	}, nil)

	require.NoError(t, ctrl.markAndCleanLSP())
	require.Equal(t, 0, ctrl.addNodeQueue.Len())
	require.True(t, lastNoPodLSP.Has("orphan"))
}

func TestEnqueueMissingNodeLSPsSkipsExistingLSP(t *testing.T) {
	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	ctrl.addNodeQueue = newTypedRateLimitingQueue[string]("AddNode", nil)

	ctrl.enqueueMissingNodeLSPs(
		map[string]string{
			util.NodeLspName("node-1"): "node-1",
			util.NodeLspName("node-2"): "node-2",
		},
		strset.New(util.NodeLspName("node-1")),
	)

	require.Equal(t, 1, ctrl.addNodeQueue.Len())
	item, shutdown := ctrl.addNodeQueue.Get()
	require.False(t, shutdown)
	require.Equal(t, "node-2", item)
	ctrl.addNodeQueue.Done(item)
}

func TestGcNodeKeepsPolicyOfNodeCreatedDuringGc(t *testing.T) {
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Nodes: []*corev1.Node{{Name: "node-live"}},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.nodeKeyMutex = keymutex.NewHashed(0)

	newPolicy := func(node, ip string) *ovnnb.LogicalRouterPolicy {
		return &ovnnb.LogicalRouterPolicy{
			UUID:        "policy-" + node,
			Priority:    util.NodeRouterPolicyPriority,
			Match:       "ip4.dst == " + ip,
			ExternalIDs: map[string]string{"vendor": util.CniTypeName, "node": node},
		}
	}
	livePolicy := newPolicy("node-live", "10.0.0.1")
	latePolicy := newPolicy("node-late", "10.0.0.2")
	orphanPolicy := newPolicy("node-gone", "10.0.0.3")

	// workers run while gc lists: node-late joins after the node snapshot and before the policy list
	fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies(
		ctrl.config.ClusterRouter, util.NodeRouterPolicyPriority, map[string]string{"vendor": util.CniTypeName}, false,
	).DoAndReturn(func(string, int, map[string]string, bool) ([]*ovnnb.LogicalRouterPolicy, error) {
		lateNode := &corev1.Node{Name: "node-late"}
		require.NoError(t, fc.fakeInformers.nodeInformer.Informer().GetIndexer().Add(lateNode))
		return []*ovnnb.LogicalRouterPolicy{livePolicy, latePolicy, orphanPolicy}, nil
	})
	fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies(
		ctrl.config.ClusterRouter, util.GatewayRouterPolicyPriority, map[string]string{"vendor": util.CniTypeName}, false,
	).Return(nil, nil)
	// only the orphan goes, and only under the ownership guard so a takeover after the list survives
	fc.mockOvnClient.EXPECT().DeleteLogicalRouterPolicyIfUnchanged(ctrl.config.ClusterRouter, orphanPolicy).Return(true, nil)
	fc.mockOvnClient.EXPECT().DeleteLogicalRouterPolicy(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	require.NoError(t, ctrl.gcNode())
}

// ipListerAddingNode adds a node to the informer when the IPs are listed, inside the gc window between the
// node reads and the ip list.
type ipListerAddingNode struct {
	kubeovnlisters.IPLister
	addNode func()
}

func (l ipListerAddingNode) List(selector labels.Selector) ([]*kubeovnv1.IP, error) {
	l.addNode()
	return l.IPLister.List(selector)
}

func TestGcNodeKeepsIPOfNodeCreatedDuringGc(t *testing.T) {
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		IPs: []*kubeovnv1.IP{{Name: util.NodeLspName("node-late")}},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.nodeKeyMutex = keymutex.NewHashed(0)

	// workers run while gc lists: node-late joins after any node snapshot and before its ip is listed
	ctrl.ipsLister = ipListerAddingNode{IPLister: ctrl.ipsLister, addNode: func() {
		require.NoError(t, fc.fakeInformers.nodeInformer.Informer().GetIndexer().Add(&corev1.Node{Name: "node-late"}))
	}}
	fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies(ctrl.config.ClusterRouter, gomock.Any(), map[string]string{"vendor": util.CniTypeName}, false).Return(nil, nil).Times(2)
	// no other call is expected: deleting the node would start with its logical switch port
	fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(gomock.Any()).Times(0)

	require.NoError(t, ctrl.gcNode())
}

func TestGcNodeWaitsForNodeHandlerBeforeDeletingNode(t *testing.T) {
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		IPs: []*kubeovnv1.IP{{Name: util.NodeLspName("node-late")}},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.nodeKeyMutex = keymutex.NewHashed(0)

	// the add handler of node-late holds the node lock while gc runs, and the node is in the informer before it
	// releases the lock: gc must read the node after the handler, not between the ip list and the delete
	ctrl.nodeKeyMutex.LockKey("node-late")
	var handler sync.WaitGroup
	ctrl.ipsLister = ipListerAddingNode{IPLister: ctrl.ipsLister, addNode: func() {
		handler.Go(func() {
			time.Sleep(50 * time.Millisecond)
			require.NoError(t, fc.fakeInformers.nodeInformer.Informer().GetIndexer().Add(&corev1.Node{Name: "node-late"}))
			require.NoError(t, ctrl.nodeKeyMutex.UnlockKey("node-late"))
		})
	}}
	fc.mockOvnClient.EXPECT().ListLogicalRouterPolicies(ctrl.config.ClusterRouter, gomock.Any(), map[string]string{"vendor": util.CniTypeName}, false).Return(nil, nil).Times(2)
	fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(gomock.Any()).Times(0)

	require.NoError(t, ctrl.gcNode())
	handler.Wait()
}

// nbClientRelabelingAfterList hands every listed node policy to another node right after listing it,
// inside the gc window between the policy list and the delete.
type nbClientRelabelingAfterList struct {
	ovs.NbClient
	t        *testing.T
	newOwner string
}

func (c nbClientRelabelingAfterList) ListLogicalRouterPolicies(lrName string, priority int, externalIDs map[string]string, ignoreExtIDEmptyValue bool) ([]*ovnnb.LogicalRouterPolicy, error) {
	policies, err := c.NbClient.ListLogicalRouterPolicies(lrName, priority, externalIDs, ignoreExtIDEmptyValue)
	for _, policy := range policies {
		ids := maps.Clone(policy.ExternalIDs)
		ids["node"] = c.newOwner
		require.NoError(c.t, c.AddLogicalRouterPolicy(lrName, policy.Priority, policy.Match, policy.Action, policy.Nexthops, policy.BFDSessions, ids))
	}
	return policies, err
}

func TestGcNodeKeepsPolicyTakenOverAfterList(t *testing.T) {
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Nodes: []*corev1.Node{{Name: "node-b"}}})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.nodeKeyMutex = keymutex.NewHashed(0)

	nbClient := newInMemoryOVNNbClient(t)
	require.NoError(t, nbClient.CreateLogicalRouter(ctrl.config.ClusterRouter))
	match := "ip4.dst == 172.15.2.16"
	orphanIDs := map[string]string{"vendor": util.CniTypeName, "node": "node-gone", "address-family": "4"}
	require.NoError(t, nbClient.AddLogicalRouterPolicy(ctrl.config.ClusterRouter, util.NodeRouterPolicyPriority, match, ovnnb.LogicalRouterPolicyActionReroute, []string{"100.64.0.2"}, nil, orphanIDs))

	// node-b reuses the join address and takes the row over between the gc list and the gc delete
	ctrl.OVNNbClient = nbClientRelabelingAfterList{NbClient: nbClient, t: t, newOwner: "node-b"}
	require.NoError(t, ctrl.gcNode())

	remaining, err := nbClient.GetLogicalRouterPolicy(ctrl.config.ClusterRouter, util.NodeRouterPolicyPriority, match, true)
	require.NoError(t, err)
	require.Len(t, remaining, 1, "a policy taken over by a live node after the gc list must survive")
	require.Equal(t, "node-b", remaining[0].ExternalIDs["node"])
}

// nodeListerRunningReplacement starts a same-name replacement of a node the moment gc reads the node as gone,
// the way its add handler would run: under the node lock, adding the node and adopting its policies.
type nodeListerRunningReplacement struct {
	corelisters.NodeLister
	node        string
	replacement func()
	started     sync.WaitGroup
}

func (l *nodeListerRunningReplacement) Get(name string) (*corev1.Node, error) {
	node, err := l.NodeLister.Get(name)
	if name == l.node && k8serrors.IsNotFound(err) {
		l.started.Go(l.replacement)
		// leave the replacement time to run now, if nothing holds it back
		time.Sleep(50 * time.Millisecond)
	}
	return node, err
}

func TestGcNodeKeepsPolicyAdoptedBySameNameReplacement(t *testing.T) {
	fc := newFakeController(t)
	ctrl := fc.fakeController
	ctrl.nodeKeyMutex = keymutex.NewHashed(0)

	nbClient := newInMemoryOVNNbClient(t)
	ctrl.OVNNbClient = nbClient
	require.NoError(t, nbClient.CreateLogicalRouter(ctrl.config.ClusterRouter))
	match := "ip4.dst == 172.15.2.16"
	ids := map[string]string{"vendor": util.CniTypeName, "node": "node-a", "address-family": "4"}
	addPolicy := func() error {
		return nbClient.AddLogicalRouterPolicy(ctrl.config.ClusterRouter, util.NodeRouterPolicyPriority, match, ovnnb.LogicalRouterPolicyActionReroute, []string{"100.64.0.2"}, nil, ids)
	}
	require.NoError(t, addPolicy())

	// the replacement keeps the name, so it adopts the row unchanged: same UUID, same external IDs
	replacement := &nodeListerRunningReplacement{NodeLister: ctrl.nodesLister, node: "node-a", replacement: func() {
		ctrl.nodeKeyMutex.LockKey("node-a")
		defer func() { _ = ctrl.nodeKeyMutex.UnlockKey("node-a") }()
		require.NoError(t, fc.fakeInformers.nodeInformer.Informer().GetIndexer().Add(&corev1.Node{Name: "node-a", UID: "replacement"}))
		require.NoError(t, addPolicy())
	}}
	ctrl.nodesLister = replacement

	require.NoError(t, ctrl.gcNode())
	replacement.started.Wait()

	remaining, err := nbClient.GetLogicalRouterPolicy(ctrl.config.ClusterRouter, util.NodeRouterPolicyPriority, match, true)
	require.NoError(t, err)
	require.Len(t, remaining, 1, "the policy of a same-name replacement node must survive gc")
}
