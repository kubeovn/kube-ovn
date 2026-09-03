package controller

import (
	"testing"

	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/scylladb/go-set/strset"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	netlisters "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
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
