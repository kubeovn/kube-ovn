package controller

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	extTestVpcName       = "vpc-ext"
	extTestStatusVpcName = "vpc-ext-status"
	extTestSwitch        = "external"
	extTestExtraSubnet   = "extra1"
	extTestNodeName      = "node-a"
	extTestNodeName2     = "node-b"
	extTestChassisName   = "chassis-a"
	extTestChassisName2  = "chassis-b"
)

func extTestVpc(extras []string, enableExternal bool) *kubeovnv1.Vpc {
	return &kubeovnv1.Vpc{
		Name: extTestVpcName,
		Spec: kubeovnv1.VpcSpec{
			EnableExternal:       enableExternal,
			ExtraExternalSubnets: extras,
		},
		Status: kubeovnv1.VpcStatus{
			EnableExternal:       enableExternal,
			ExtraExternalSubnets: extras,
		},
	}
}

func extTestGatewayNode(name, chassis string) *corev1.Node {
	node := &corev1.Node{
		Name:   name,
		Labels: map[string]string{util.ExGatewayLabel: "true"},
	}
	if chassis != "" {
		node.Annotations = map[string]string{util.ChassisAnnotation: chassis}
	}
	return node
}

// orderedNodeIndexer returns nodes in a fixed order so that tests can assert on the ordering
// applied by the controller instead of relying on the (unordered) informer store.
type orderedNodeIndexer struct {
	cache.Indexer
	items []any
}

func (i *orderedNodeIndexer) List() []any {
	return i.items
}

func prepareExternalTestController(t *testing.T, opts *FakeControllerOptions) *fakeController {
	t.Helper()

	fake, err := newFakeControllerWithOptions(t, opts)
	require.NoError(t, err)
	ctrl := fake.fakeController
	ctrl.nodeKeyMutex = keymutex.NewHashed(0)
	ctrl.addOrUpdateVpcQueue = newTypedRateLimitingQueue[string]("AddOrUpdateVpc", nil)
	ctrl.config.ExternalGatewaySwitch = extTestSwitch
	return fake
}

func TestNodeIsExternalGateway(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{name: "true", labels: map[string]string{util.ExGatewayLabel: "true"}, want: true},
		{name: "false", labels: map[string]string{util.ExGatewayLabel: "false"}},
		{name: "absent"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, nodeIsExternalGateway(&corev1.Node{Labels: tt.labels}))
		})
	}
}

func TestEnqueueExternalVpcsForReconcileFilter(t *testing.T) {
	t.Parallel()

	statusOnlyVpc := extTestVpc(nil, false)
	statusOnlyVpc.Name = extTestStatusVpcName
	statusOnlyVpc.Status.EnableExternal = true

	ctrl := prepareNodeQueueTestController(t,
		extTestVpc(nil, true),
		statusOnlyVpc,
		&kubeovnv1.Vpc{
			Name: "vpc-plain",
		},
		&kubeovnv1.Vpc{
			Name:   "vpc-external-label",
			Labels: map[string]string{util.VpcExternalLabel: "true"},
			Spec:   kubeovnv1.VpcSpec{EnableExternal: true},
		},
	)

	ctrl.enqueueExternalVpcsForReconcile()

	// One VPC is enqueued because of its spec, one because of its status; the plain VPC and the
	// external VPC object are skipped.
	require.Equal(t, []string{extTestVpcName, extTestStatusVpcName}, drainVpcQueue(t, ctrl))
}

func TestEnqueueUpdateNodeEnqueuesExternalVpcsOnGatewayLabelChange(t *testing.T) {
	t.Parallel()

	t.Run("gateway label added", func(t *testing.T) {
		ctrl := prepareNodeQueueTestController(t, extTestVpc(nil, true))
		oldNode := &corev1.Node{Name: extTestNodeName}
		newNode := oldNode.DeepCopy()
		newNode.Labels = map[string]string{util.ExGatewayLabel: "true"}

		ctrl.enqueueUpdateNode(oldNode, newNode)

		require.Equal(t, []string{extTestVpcName}, drainVpcQueue(t, ctrl))
	})

	t.Run("gateway label removed", func(t *testing.T) {
		ctrl := prepareNodeQueueTestController(t, extTestVpc(nil, true))
		oldNode := &corev1.Node{
			Name:   extTestNodeName,
			Labels: map[string]string{util.ExGatewayLabel: "true"},
		}
		newNode := oldNode.DeepCopy()
		newNode.Labels = map[string]string{}

		ctrl.enqueueUpdateNode(oldNode, newNode)

		require.Equal(t, []string{extTestVpcName}, drainVpcQueue(t, ctrl))
	})

	t.Run("unrelated label change on a non gateway node", func(t *testing.T) {
		ctrl := prepareNodeQueueTestController(t, extTestVpc(nil, true))
		oldNode := &corev1.Node{
			Name:   extTestNodeName,
			Labels: map[string]string{util.ExGatewayLabel: "false", "role": "worker"},
		}
		newNode := oldNode.DeepCopy()
		newNode.Labels = map[string]string{util.ExGatewayLabel: "false", "role": "gateway"}

		ctrl.enqueueUpdateNode(oldNode, newNode)

		require.Empty(t, drainVpcQueue(t, ctrl))
	})

	for _, tt := range []struct {
		name      string
		oldLabels map[string]string
		newLabels map[string]string
	}{
		{name: "false added", newLabels: map[string]string{util.ExGatewayLabel: "false"}},
		{name: "false removed", oldLabels: map[string]string{util.ExGatewayLabel: "false"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := prepareNodeQueueTestController(t, extTestVpc(nil, true))
			ctrl.enqueueUpdateNode(
				&corev1.Node{Name: extTestNodeName, Labels: tt.oldLabels},
				&corev1.Node{Name: extTestNodeName, Labels: tt.newLabels},
			)
			require.Empty(t, drainVpcQueue(t, ctrl))
		})
	}
}

func TestHandleUpdateNodeEnqueuesExternalVpcsWhenChassisIsRegistered(t *testing.T) {
	t.Parallel()

	t.Run("gateway node with a registered chassis", func(t *testing.T) {
		node := extTestGatewayNode(extTestNodeName, extTestChassisName)
		fake := prepareExternalTestController(t, &FakeControllerOptions{
			Vpcs:  []*kubeovnv1.Vpc{extTestVpc(nil, true)},
			Nodes: []*corev1.Node{node},
		})
		ctrl := fake.fakeController

		fake.mockOvnSbClient.EXPECT().GetChassis(extTestChassisName, true).Return(&ovnsb.Chassis{
			Name:        extTestChassisName,
			ExternalIDs: map[string]string{"vendor": util.CniTypeName},
		}, nil)
		fake.mockOvnSbClient.EXPECT().GetChassisByHost(extTestNodeName).Return(nil, nil)

		require.NoError(t, ctrl.handleUpdateNode(extTestNodeName))
		require.Equal(t, []string{extTestVpcName}, drainVpcQueue(t, ctrl))
	})

	t.Run("node without the gateway label", func(t *testing.T) {
		node := &corev1.Node{
			Name:        extTestNodeName,
			Annotations: map[string]string{util.ChassisAnnotation: extTestChassisName},
		}
		fake := prepareExternalTestController(t, &FakeControllerOptions{
			Vpcs:  []*kubeovnv1.Vpc{extTestVpc(nil, true)},
			Nodes: []*corev1.Node{node},
		})
		ctrl := fake.fakeController

		fake.mockOvnSbClient.EXPECT().GetChassis(extTestChassisName, true).Return(&ovnsb.Chassis{
			Name:        extTestChassisName,
			ExternalIDs: map[string]string{"vendor": util.CniTypeName},
		}, nil)
		fake.mockOvnSbClient.EXPECT().GetChassisByHost(extTestNodeName).Return(nil, nil)

		require.NoError(t, ctrl.handleUpdateNode(extTestNodeName))
		require.Empty(t, drainVpcQueue(t, ctrl))
	})

	t.Run("gateway node without a registered chassis", func(t *testing.T) {
		fake := prepareExternalTestController(t, &FakeControllerOptions{
			Vpcs:  []*kubeovnv1.Vpc{extTestVpc(nil, true)},
			Nodes: []*corev1.Node{extTestGatewayNode(extTestNodeName, "")},
		})
		ctrl := fake.fakeController

		// UpdateChassisTag returns early without a chassis annotation, so only the duplicated
		// chassis check runs and the external VPCs must not be enqueued yet.
		fake.mockOvnSbClient.EXPECT().GetChassisByHost(extTestNodeName).Return(nil, nil)

		require.NoError(t, ctrl.handleUpdateNode(extTestNodeName))
		require.Empty(t, drainVpcQueue(t, ctrl))
	})
}

func TestHandleUpdateVpcExternalReconcilesExternalSubnets(t *testing.T) {
	t.Parallel()

	t.Run("extras replace the connected default external subnet", func(t *testing.T) {
		fake := prepareExternalTestController(t, &FakeControllerOptions{
			Vpcs:  []*kubeovnv1.Vpc{extTestVpc([]string{extTestExtraSubnet}, true)},
			Nodes: []*corev1.Node{extTestGatewayNode(extTestNodeName, extTestChassisName)},
		})
		ctrl := fake.fakeController
		defaultLrp := extTestVpcName + "-" + extTestSwitch
		extraLrp := extTestVpcName + "-" + extTestExtraSubnet

		fake.mockOvnClient.EXPECT().GetLogicalRouterPort(defaultLrp, true).
			Return(&ovnnb.LogicalRouterPort{Name: defaultLrp}, nil)
		fake.mockOvnClient.EXPECT().GetLogicalRouterPort(extraLrp, true).
			Return(&ovnnb.LogicalRouterPort{Name: extraLrp}, nil)
		fake.mockOvnClient.EXPECT().RemoveLogicalPatchPort(extTestSwitch+"-"+extTestVpcName, defaultLrp).Return(nil)
		fake.mockOvnClient.EXPECT().DeleteBFDByDstIP(defaultLrp, "").Return(nil)
		fake.mockOvnSbClient.EXPECT().GetChassis(extTestChassisName, true).
			Return(&ovnsb.Chassis{Name: extTestChassisName}, nil)
		fake.mockOvnClient.EXPECT().ReconcileGatewayChassises(extraLrp, []string{extTestChassisName}).Return(nil)

		cachedVpc, err := ctrl.vpcsLister.Get(extTestVpcName)
		require.NoError(t, err)
		require.NoError(t, ctrl.handleUpdateVpcExternal(cachedVpc, false, true, ""))

		vpc, err := ctrl.config.KubeOvnClient.KubeovnV1().Vpcs().
			Get(context.Background(), extTestVpcName, metav1.GetOptions{})
		require.NoError(t, err)
		require.True(t, vpc.Status.EnableExternal)
		require.Equal(t, []string{extTestExtraSubnet}, vpc.Status.ExtraExternalSubnets)
	})

	t.Run("disabling external disconnects the default external subnet", func(t *testing.T) {
		vpc := extTestVpc(nil, true)
		vpc.Spec.EnableExternal = false
		vpc.Status.ExtraExternalSubnets = nil

		fake := prepareExternalTestController(t, &FakeControllerOptions{
			Vpcs: []*kubeovnv1.Vpc{vpc},
		})
		ctrl := fake.fakeController
		defaultLrp := extTestVpcName + "-" + extTestSwitch

		fake.mockOvnClient.EXPECT().GetLogicalRouterPort(defaultLrp, true).
			Return(&ovnnb.LogicalRouterPort{Name: defaultLrp}, nil)
		fake.mockOvnClient.EXPECT().RemoveLogicalPatchPort(extTestSwitch+"-"+extTestVpcName, defaultLrp).Return(nil)
		fake.mockOvnClient.EXPECT().DeleteBFDByDstIP(defaultLrp, "").Return(nil)

		cachedVpc, err := ctrl.vpcsLister.Get(extTestVpcName)
		require.NoError(t, err)
		require.NoError(t, ctrl.handleUpdateVpcExternal(cachedVpc, false, true, ""))

		vpc, err = ctrl.config.KubeOvnClient.KubeovnV1().Vpcs().
			Get(context.Background(), extTestVpcName, metav1.GetOptions{})
		require.NoError(t, err)
		require.False(t, vpc.Status.EnableExternal)
		require.Empty(t, vpc.Status.ExtraExternalSubnets)
	})
}

func TestReconcileVpcExternalSubnetChassis(t *testing.T) {
	t.Parallel()

	lrp := extTestVpcName + "-" + extTestExtraSubnet

	t.Run("node without a chassis annotation is skipped", func(t *testing.T) {
		fake := prepareExternalTestController(t, &FakeControllerOptions{
			Nodes: []*corev1.Node{extTestGatewayNode(extTestNodeName, "")},
		})

		// No OVN NB/SB expectations: the only gateway node is skipped, so no chassis is added and
		// stale entries must not be removed.
		require.NoError(t, fake.fakeController.reconcileVpcExternalSubnetChassis(extTestVpcName, extTestExtraSubnet))
	})

	t.Run("ready chassis are added without removing unknown chassis", func(t *testing.T) {
		fake := prepareExternalTestController(t, &FakeControllerOptions{
			Nodes: []*corev1.Node{
				extTestGatewayNode(extTestNodeName, extTestChassisName),
				extTestGatewayNode(extTestNodeName2, ""),
			},
		})

		fake.mockOvnSbClient.EXPECT().GetChassis(extTestChassisName, true).
			Return(&ovnsb.Chassis{Name: extTestChassisName}, nil)
		fake.mockOvnClient.EXPECT().CreateGatewayChassises(lrp, extTestChassisName).Return(nil)

		require.NoError(t, fake.fakeController.reconcileVpcExternalSubnetChassis(extTestVpcName, extTestExtraSubnet))
	})

	t.Run("chassis are reconciled in a deterministic order", func(t *testing.T) {
		nodeA := extTestGatewayNode(extTestNodeName, extTestChassisName)
		nodeB := extTestGatewayNode(extTestNodeName2, extTestChassisName2)

		fake := prepareExternalTestController(t, &FakeControllerOptions{
			Nodes: []*corev1.Node{nodeA, nodeB},
		})
		ctrl := fake.fakeController

		// Feed the nodes in reverse hash order so that a missing sort would be observable.
		expected := []string{extTestChassisName, extTestChassisName2}
		sort.Slice(expected, func(i, j int) bool {
			return util.Sha256Hash([]byte(extTestVpcName+expected[i])) < util.Sha256Hash([]byte(extTestVpcName+expected[j]))
		})
		reversed := []any{extTestGatewayNode(extTestNodeName, expected[1]), extTestGatewayNode(extTestNodeName2, expected[0])}
		ctrl.nodesLister = corelisters.NewNodeLister(&orderedNodeIndexer{
			Indexer: cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}),
			items:   reversed,
		})

		fake.mockOvnSbClient.EXPECT().GetChassis(expected[1], true).
			Return(&ovnsb.Chassis{Name: expected[1]}, nil)
		fake.mockOvnSbClient.EXPECT().GetChassis(expected[0], true).
			Return(&ovnsb.Chassis{Name: expected[0]}, nil)
		fake.mockOvnClient.EXPECT().ReconcileGatewayChassises(lrp, expected).Return(nil)

		require.NoError(t, ctrl.reconcileVpcExternalSubnetChassis(extTestVpcName, extTestExtraSubnet))
	})

	t.Run("all chassis are reconciled when no gateway nodes remain", func(t *testing.T) {
		fake := prepareExternalTestController(t, &FakeControllerOptions{})
		fake.mockOvnClient.EXPECT().ReconcileGatewayChassises(lrp, []string{}).Return(nil)

		require.NoError(t, fake.fakeController.reconcileVpcExternalSubnetChassis(extTestVpcName, extTestExtraSubnet))
	})
}

func TestHandleDeleteNodeNotifiesExternalVpcsForGatewayOnly(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		value  string
		queued bool
	}{
		{name: "gateway node", value: "true", queued: true},
		{name: "released node", value: "false"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			node := &corev1.Node{
				Name:   extTestNodeName,
				Labels: map[string]string{util.ExGatewayLabel: tt.value},
			}
			fake := prepareExternalTestController(t, &FakeControllerOptions{
				Vpcs: []*kubeovnv1.Vpc{extTestVpc(nil, true)},
			})
			ctrl := fake.fakeController
			ctrl.deletingNodeObjMap = xsync.NewMap[string, *corev1.Node]()
			ctrl.deletingNodeObjMap.Store(node.Name, node)

			fake.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(util.NodeLspName(node.Name)).Return(nil)
			fake.mockOvnSbClient.EXPECT().DeleteChassisByHost(node.Name).Return(nil)
			fake.mockOvnClient.EXPECT().ListLogicalRouterPolicies(ctrl.config.ClusterRouter, -1, map[string]string{
				"vendor":          util.CniTypeName,
				"node":            node.Name,
				"address-family":  "4",
				"isLocalDnsCache": "true",
			}, true).Return(nil, nil)
			fake.mockOvnClient.EXPECT().ListLogicalRouterPolicies(ctrl.config.ClusterRouter, -1, map[string]string{
				"vendor":          util.CniTypeName,
				"node":            node.Name,
				"address-family":  "6",
				"isLocalDnsCache": "true",
			}, true).Return(nil, nil)
			fake.mockOvnClient.EXPECT().DeletePortGroup(strings.ReplaceAll(util.NodeLspName(node.Name), "-", ".")).Return(nil)
			fake.mockOvnClient.EXPECT().DeleteAddressSet(nodeUnderlayAddressSetName(node.Name, 4)).Return(nil)
			fake.mockOvnClient.EXPECT().DeleteAddressSet(nodeUnderlayAddressSetName(node.Name, 6)).Return(nil)

			require.NoError(t, ctrl.handleDeleteNode(node.Name))
			if tt.queued {
				require.Equal(t, []string{extTestVpcName}, drainVpcQueue(t, ctrl))
			} else {
				require.Empty(t, drainVpcQueue(t, ctrl))
			}
		})
	}
}

func TestHandleAddNodeNotifiesExternalVpcsBeforeJoinSetup(t *testing.T) {
	t.Parallel()

	node := extTestGatewayNode(extTestNodeName, "")
	joinSubnet := &kubeovnv1.Subnet{
		Name: "join",
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock: "10.0.0.1/32",
			Gateway:   "10.0.0.1",
		},
	}
	fake := prepareExternalTestController(t, &FakeControllerOptions{
		Vpcs:    []*kubeovnv1.Vpc{extTestVpc(nil, true)},
		Nodes:   []*corev1.Node{node},
		Subnets: []*kubeovnv1.Subnet{joinSubnet},
	})
	ctrl := fake.fakeController
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(
		joinSubnet.Name,
		joinSubnet.Spec.CIDRBlock,
		joinSubnet.Spec.Gateway,
		[]string{joinSubnet.Spec.Gateway},
	))

	// The join network setup fails because no address can be allocated, but the external VPCs have
	// already been notified so that they converge on their own once the node becomes usable.
	err := ctrl.handleAddNode(extTestNodeName)
	require.ErrorContains(t, err, "NoAvailableAddress")
	require.Equal(t, []string{extTestVpcName}, drainVpcQueue(t, ctrl))
}
