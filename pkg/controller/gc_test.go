package controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/scylladb/go-set/strset"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	corelisters "k8s.io/client-go/listers/core/v1"
	netlisters "k8s.io/client-go/listers/networking/v1"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/keymutex"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"
	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"
	v1alpha2 "sigs.k8s.io/network-policy-api/apis/v1alpha2"
	anplisters "sigs.k8s.io/network-policy-api/pkg/client/listers/apis/v1alpha1"
	cnplisters "sigs.k8s.io/network-policy-api/pkg/client/listers/apis/v1alpha2"

	mockovs "github.com/kubeovn/kube-ovn/mocks/pkg/ovs"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
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

func TestGcVPCDNSWhenOvnLBDisabledDeletesGeneratedResources(t *testing.T) {
	const name = "dns1"
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcDNSs: []*kubeovnv1.VpcDns{{Name: name}},
		SwitchLBRules: []*kubeovnv1.SwitchLBRule{{
			Name:   genVpcDNSDpName(name),
			Labels: map[string]string{util.VpcDNSNameLabel: "true"},
		}},
		Deployments: []*appsv1.Deployment{{
			Name:      genVpcDNSDpName(name),
			Namespace: metav1.NamespaceSystem,
			Labels:    map[string]string{util.VpcDNSNameLabel: "true"},
		}},
	})
	require.NoError(t, err)
	fc.fakeController.config.EnableOvnLB = false

	require.NoError(t, fc.fakeController.gcVPCDNS())
	_, err = fc.kubeClient.AppsV1().Deployments(metav1.NamespaceSystem).Get(
		context.Background(), genVpcDNSDpName(name), metav1.GetOptions{},
	)
	require.True(t, k8serrors.IsNotFound(err))
	_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().SwitchLBRules().Get(
		context.Background(), genVpcDNSDpName(name), metav1.GetOptions{},
	)
	require.True(t, k8serrors.IsNotFound(err))
	_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().VpcDnses().Get(
		context.Background(), name, metav1.GetOptions{},
	)
	require.NoError(t, err)
}

func TestGcRouterLBRulesWhenOvnLBDisabledDeletesGeneratedServices(t *testing.T) {
	const name = "rlr1"
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		RouterLBRules: []*kubeovnv1.RouterLBRule{{Name: name}},
		Services: []*corev1.Service{{
			Name:      generateRlrSvcName(name),
			Namespace: metav1.NamespaceDefault,
			Annotations: map[string]string{
				util.RouterLBRuleVipsAnnotation: "192.0.2.1",
			},
		}},
	})
	require.NoError(t, err)
	fc.fakeController.config.EnableOvnLB = false

	require.NoError(t, fc.fakeController.gcRouterLBRules())
	_, err = fc.kubeClient.CoreV1().Services(metav1.NamespaceDefault).Get(
		context.Background(), generateRlrSvcName(name), metav1.GetOptions{},
	)
	require.True(t, k8serrors.IsNotFound(err))
	_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().RouterLBRules().Get(
		context.Background(), name, metav1.GetOptions{},
	)
	require.NoError(t, err)
}

func TestGcLoadBalancerWhenOvnLBDisabledDeletesOwnedOrphans(t *testing.T) {
	const (
		vpcName    = "vpc1"
		subnetName = "subnet1"
		orphanLB   = "orphan-lb"
		keptLB     = "keep-dnat"
	)
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{{
			Name: subnetName,
			Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider},
		}},
		Vpcs: []*kubeovnv1.Vpc{{
			Name: vpcName,
			Status: kubeovnv1.VpcStatus{
				Subnets:                 []string{subnetName},
				TCPLoadBalancer:         "tcp-lb",
				TCPSessionLoadBalancer:  "tcp-session-lb",
				UDPLoadBalancer:         "udp-lb",
				UDPSessionLoadBalancer:  "udp-session-lb",
				SctpLoadBalancer:        "sctp-lb",
				SctpSessionLoadBalancer: "sctp-session-lb",
			},
		}},
		OvnDnatRules: []*kubeovnv1.OvnDnatRule{{Name: keptLB}},
	})
	require.NoError(t, err)
	fc.fakeController.config.EnableOvnLB = false

	fc.mockOvnClient.EXPECT().LogicalSwitchUpdateLoadBalancers(
		subnetName, ovsdb.MutateOperationDelete,
		"tcp-lb", "tcp-session-lb", "udp-lb", "udp-session-lb", "sctp-lb", "sctp-session-lb",
	).Return(nil)
	fc.mockOvnClient.EXPECT().DeleteLoadBalancers(gomock.Any()).DoAndReturn(func(filter func(*ovnnb.LoadBalancer) bool) error {
		require.True(t, filter(&ovnnb.LoadBalancer{Name: orphanLB, ExternalIDs: map[string]string{"vendor": util.CniTypeName}}))
		require.False(t, filter(&ovnnb.LoadBalancer{Name: keptLB, ExternalIDs: map[string]string{"vendor": util.CniTypeName}}))
		require.False(t, filter(&ovnnb.LoadBalancer{Name: "external-lb", ExternalIDs: map[string]string{"vendor": "external"}}))
		return nil
	})
	fc.mockOvnClient.EXPECT().DeleteChassisTemplateVariables(gomock.Any()).Return(nil)

	require.NoError(t, fc.fakeController.gcLoadBalancer())
	vpc, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().Vpcs().Get(
		context.Background(), vpcName, metav1.GetOptions{},
	)
	require.NoError(t, err)
	require.Empty(t, vpc.Status.TCPLoadBalancer)
	require.Empty(t, vpc.Status.TCPSessionLoadBalancer)
	require.Empty(t, vpc.Status.UDPLoadBalancer)
	require.Empty(t, vpc.Status.UDPSessionLoadBalancer)
	require.Empty(t, vpc.Status.SctpLoadBalancer)
	require.Empty(t, vpc.Status.SctpSessionLoadBalancer)
}

func vmTemplate(networks []kubevirtv1.Network, annotations map[string]string) *kubevirtv1.VirtualMachineInstanceTemplateSpec {
	return &kubevirtv1.VirtualMachineInstanceTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Annotations: annotations},
		Spec:       kubevirtv1.VirtualMachineInstanceSpec{Networks: networks},
	}
}

// mockKubevirtList wires a MockKubevirtClient so that a cluster-wide
// VirtualMachine(NamespaceAll).List returns the given list/error exactly once.
func mockKubevirtList(t *testing.T, list *kubevirtv1.VirtualMachineList, listErr error) kubecli.KubevirtClient {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockVMI := kubecli.NewMockVirtualMachineInterface(ctrl)
	mockVMI.EXPECT().List(gomock.Any(), gomock.Any()).Return(list, listErr)
	mockClient := kubecli.NewMockKubevirtClient(ctrl)
	mockClient.EXPECT().VirtualMachine(metav1.NamespaceAll).Return(mockVMI)
	return mockClient
}

func TestGetVMLsps(t *testing.T) {
	t.Parallel()

	t.Run("disabled keeps no vm lsp and never lists", func(t *testing.T) {
		// KubevirtClient is intentionally a mock with no expectations: if getVMLsps
		// touched the apiserver while disabled, gomock would fail the test.
		ctrl := gomock.NewController(t)
		c := &Controller{config: &Configuration{
			EnableKeepVMIP: false,
			KubevirtClient: kubecli.NewMockKubevirtClient(ctrl),
		}}
		lsps, err := c.getVMLsps()
		require.NoError(t, err)
		require.Empty(t, lsps)
	})

	t.Run("missing kubevirt crd resolves to empty set", func(t *testing.T) {
		notFound := k8serrors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachines"}, "")
		c := &Controller{config: &Configuration{
			EnableKeepVMIP: true,
			KubevirtClient: mockKubevirtList(t, nil, notFound),
		}}
		lsps, err := c.getVMLsps()
		require.NoError(t, err)
		require.Empty(t, lsps)
	})

	t.Run("transient list failure is propagated", func(t *testing.T) {
		c := &Controller{config: &Configuration{
			EnableKeepVMIP: true,
			KubevirtClient: mockKubevirtList(t, nil, errors.New("boom")),
		}}
		lsps, err := c.getVMLsps()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to list vms")
		require.Nil(t, lsps)
	})

	t.Run("lists vms cluster-wide using their own namespace", func(t *testing.T) {
		vms := &kubevirtv1.VirtualMachineList{Items: []kubevirtv1.VirtualMachine{
			{
				Name: "vm-primary", Namespace: "ns1",
				Spec: kubevirtv1.VirtualMachineSpec{Template: vmTemplate(nil, nil)},
			},
			{
				// Default multus network: primary lsp is skipped, but the attachment
				// network derived from NetworkName must still be kept.
				Name: "vm-default-multus", Namespace: "ns2",
				Spec: kubevirtv1.VirtualMachineSpec{Template: vmTemplate([]kubevirtv1.Network{{
					Name:   "secondary",
					Multus: &kubevirtv1.MultusNetwork{Default: true, NetworkName: "ns2/net2"},
				}}, nil)},
			},
			{
				// NAD annotation contributes an attachment lsp on top of the primary one.
				Name: "vm-nad", Namespace: "ns3",
				Spec: kubevirtv1.VirtualMachineSpec{Template: vmTemplate(nil, map[string]string{nadv1.NetworkAttachmentAnnot: "netx"})},
			},
		}}
		c := &Controller{config: &Configuration{
			EnableKeepVMIP: true,
			KubevirtClient: mockKubevirtList(t, vms, nil),
		}}

		lsps, err := c.getVMLsps()
		require.NoError(t, err)

		net2Provider := fmt.Sprintf("%s.%s.%s", "net2", "ns2", util.OvnProvider)
		nadProvider := fmt.Sprintf("%s.%s.%s", "netx", "ns3", util.OvnProvider)
		require.ElementsMatch(t, []string{
			ovs.PodNameToPortName("vm-primary", "ns1", util.OvnProvider),
			ovs.PodNameToPortName("vm-default-multus", "ns2", net2Provider),
			ovs.PodNameToPortName("vm-nad", "ns3", util.OvnProvider),
			ovs.PodNameToPortName("vm-nad", "ns3", nadProvider),
		}, lsps)
	})
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
	mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(false, nil).Return(nil, nil)

	require.NoError(t, ctrl.markAndCleanLSP())
	require.Equal(t, 1, ctrl.addNodeQueue.Len())

	item, shutdown := ctrl.addNodeQueue.Get()
	require.False(t, shutdown)
	require.Equal(t, "node-1", item)
	ctrl.addNodeQueue.Done(item)
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

func TestGcNetworkPolicyDeletesLeftoversWhenDisabled(t *testing.T) {
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Nodes: []*corev1.Node{{Name: "node1"}},
	})
	require.NoError(t, err)
	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient
	ctrl.config.EnableNP = false

	npPG := ovnnb.PortGroup{
		Name:        "allow.default",
		ExternalIDs: map[string]string{networkPolicyKey: "default/allow"},
	}
	nodePG := ovnnb.PortGroup{
		Name:        "node.node1",
		ExternalIDs: map[string]string{networkPolicyKey: "node/node1"},
	}

	mockOvnClient.EXPECT().ListPortGroups(map[string]string{networkPolicyKey: ""}).Return([]ovnnb.PortGroup{npPG, nodePG}, nil)
	mockOvnClient.EXPECT().DeletePortGroup(npPG.Name).Return(nil)
	mockOvnClient.EXPECT().ListAddressSets(map[string]string{networkPolicyKey: ""}).Return([]ovnnb.AddressSet{{
		Name: "default.allow.ipv4.ingress",
		ExternalIDs: map[string]string{
			networkPolicyKey: "default/allow/ingress",
		},
	}}, nil)
	mockOvnClient.EXPECT().DeleteMeter("allow.default_to-lport_meter").Return(nil)
	mockOvnClient.EXPECT().DeleteMeter("allow.default_from-lport_meter").Return(nil)
	mockOvnClient.EXPECT().DeleteAddressSets(map[string]string{networkPolicyKey: ""}).Return(nil)

	require.NoError(t, ctrl.gcNetworkPolicy())
}

func TestGcAddressSetDeletesOrphanedU2OOverlayCIDRs(t *testing.T) {
	t.Parallel()

	const liveVpc = "live-vpc"
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vpcs: []*kubeovnv1.Vpc{{Name: liveVpc}},
	})
	require.NoError(t, err)

	orphanV4, orphanV6 := u2oOverlayCIDRsAddressSetNames("orphan-vpc")
	liveV4, liveV6 := u2oOverlayCIDRsAddressSetNames(liveVpc)
	addressSets := []ovnnb.AddressSet{
		{Name: orphanV4, ExternalIDs: u2oOverlayCIDRsAddressSetExternalIDs("orphan-vpc")},
		{Name: orphanV6, ExternalIDs: u2oOverlayCIDRsAddressSetExternalIDs("orphan-vpc")},
		{Name: liveV4, ExternalIDs: u2oOverlayCIDRsAddressSetExternalIDs(liveVpc)},
		{Name: liveV6, ExternalIDs: u2oOverlayCIDRsAddressSetExternalIDs(liveVpc)},
	}

	fc.mockOvnClient.EXPECT().ListAddressSets(map[string]string{"vendor": util.CniTypeName}).Return(addressSets, nil)
	fc.mockOvnClient.EXPECT().DeleteAddressSet(orphanV4, orphanV6).Return(nil)

	require.NoError(t, fc.fakeController.gcAddressSet())
}

func TestGcAddressSetSkipsVpcRecreatedWhileWaitingForLock(t *testing.T) {
	t.Parallel()

	const vpcName = "recreated-vpc"
	fc, err := newFakeControllerWithOptions(t, nil)
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.vpcKeyMutex = keymutex.NewHashed(1)
	orphanV4, orphanV6 := u2oOverlayCIDRsAddressSetNames(vpcName)
	listed := make(chan struct{})
	fc.mockOvnClient.EXPECT().ListAddressSets(map[string]string{"vendor": util.CniTypeName}).DoAndReturn(func(map[string]string) ([]ovnnb.AddressSet, error) {
		close(listed)
		return []ovnnb.AddressSet{
			{Name: orphanV4, ExternalIDs: u2oOverlayCIDRsAddressSetExternalIDs(vpcName)},
			{Name: orphanV6, ExternalIDs: u2oOverlayCIDRsAddressSetExternalIDs(vpcName)},
		}, nil
	})

	ctrl.vpcKeyMutex.LockKey(vpcName)
	errCh := make(chan error, 1)
	go func() { errCh <- ctrl.gcAddressSet() }()
	<-listed
	require.NoError(t, fc.fakeInformers.vpcInformer.Informer().GetStore().Add(&kubeovnv1.Vpc{Name: vpcName}))
	_ = ctrl.vpcKeyMutex.UnlockKey(vpcName)

	require.NoError(t, <-errCh)
}

func TestGcAdminNetworkPolicySkipsWhenEnabled(t *testing.T) {
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{EnableANP: true})
	require.NoError(t, err)
	require.NoError(t, fakeController.fakeController.gcAdminNetworkPolicy())
}

func TestGcAdminNetworkPolicyDeletesLeftoversWhenDisabled(t *testing.T) {
	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient

	anpPG := ovnnb.PortGroup{
		Name:        "anp.foo",
		ExternalIDs: map[string]string{adminNetworkPolicyKey: "anp.foo"},
	}
	banpPG := ovnnb.PortGroup{
		Name:        "banp.bar",
		ExternalIDs: map[string]string{baselineAdminNetworkPolicyKey: "banp.bar"},
	}

	mockOvnClient.EXPECT().ListPortGroups(map[string]string{adminNetworkPolicyKey: ""}).Return([]ovnnb.PortGroup{anpPG}, nil)
	mockOvnClient.EXPECT().DeletePortGroup(anpPG.Name).Return(nil)
	mockOvnClient.EXPECT().DeleteAddressSets(map[string]string{adminNetworkPolicyKey: ""}).Return(nil)

	mockOvnClient.EXPECT().ListPortGroups(map[string]string{baselineAdminNetworkPolicyKey: ""}).Return([]ovnnb.PortGroup{banpPG}, nil)
	mockOvnClient.EXPECT().DeletePortGroup(banpPG.Name).Return(nil)
	mockOvnClient.EXPECT().DeleteAddressSets(map[string]string{baselineAdminNetworkPolicyKey: ""}).Return(nil)

	mockOvnClient.EXPECT().ListPortGroups(map[string]string{clusterNetworkPolicyKey: ""}).Return(nil, nil)
	mockOvnClient.EXPECT().DeletePortGroup().Return(nil)
	mockOvnClient.EXPECT().DeleteAddressSets(map[string]string{clusterNetworkPolicyKey: ""}).Return(nil)

	require.NoError(t, ctrl.gcAdminNetworkPolicy())
}

func TestGcDisabledDNSNameResolvers(t *testing.T) {
	tests := []struct {
		name                  string
		enableANP             bool
		enableDNSNameResolver bool
		shouldDelete          bool
	}{
		{name: "all features enabled", enableANP: true, enableDNSNameResolver: true},
		{name: "anp disabled", enableDNSNameResolver: true, shouldDelete: true},
		{name: "dns resolver disabled", enableANP: true, shouldDelete: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				EnableANP:             tt.enableANP,
				EnableDNSNameResolver: tt.enableDNSNameResolver,
			})
			require.NoError(t, err)
			ctrl := fakeController.fakeController
			ctrl.anpsLister = anplisters.NewAdminNetworkPolicyLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))
			ctrl.cnpsLister = cnplisters.NewClusterNetworkPolicyLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))

			resolver := &kubeovnv1.DNSNameResolver{
				Name:   "anp-example-resolver",
				Labels: map[string]string{adminNetworkPolicyKey: "example"},
			}
			unmanaged := &kubeovnv1.DNSNameResolver{Name: "unmanaged-resolver"}
			client := ctrl.config.KubeOvnClient.KubeovnV1().DNSNameResolvers()
			_, err = client.Create(context.Background(), resolver, metav1.CreateOptions{})
			require.NoError(t, err)
			_, err = client.Create(context.Background(), unmanaged, metav1.CreateOptions{})
			require.NoError(t, err)

			if tt.enableANP && !tt.enableDNSNameResolver {
				fakeController.mockOvnClient.EXPECT().ListPortGroups(map[string]string{adminNetworkPolicyKey: ""}).Return(nil, nil)
				fakeController.mockOvnClient.EXPECT().ListAddressSets(map[string]string{adminNetworkPolicyKey: ""}).Return(nil, nil)
				fakeController.mockOvnClient.EXPECT().ListPortGroups(map[string]string{clusterNetworkPolicyKey: ""}).Return(nil, nil)
				fakeController.mockOvnClient.EXPECT().ListAddressSets(map[string]string{clusterNetworkPolicyKey: ""}).Return(nil, nil)
			}
			require.NoError(t, ctrl.gcDisabledDNSNameResolvers())
			_, err = client.Get(context.Background(), resolver.Name, metav1.GetOptions{})
			if tt.shouldDelete {
				require.True(t, k8serrors.IsNotFound(err))
			} else {
				require.NoError(t, err)
			}
			_, err = client.Get(context.Background(), unmanaged.Name, metav1.GetOptions{})
			require.NoError(t, err)
		})
	}
}

func TestGcDisabledDNSNameResolversDeletesPoliciesWithDomains(t *testing.T) {
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{EnableANP: true})
	require.NoError(t, err)
	ctrl := fakeController.fakeController

	anp := &v1alpha1.AdminNetworkPolicy{
		Name: "uses-dns",
		Spec: v1alpha1.AdminNetworkPolicySpec{Egress: []v1alpha1.AdminNetworkPolicyEgressRule{{
			To: []v1alpha1.AdminNetworkPolicyEgressPeer{{DomainNames: []v1alpha1.DomainName{"example.test."}}},
		}}},
	}
	cnp := &v1alpha2.ClusterNetworkPolicy{
		Name: "uses-dns",
		Spec: v1alpha2.ClusterNetworkPolicySpec{Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{{
			To: []v1alpha2.ClusterNetworkPolicyEgressPeer{{DomainNames: []v1alpha2.DomainName{"example.test."}}},
		}}},
	}
	anpIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	cnpIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, anpIndexer.Add(anp))
	require.NoError(t, cnpIndexer.Add(cnp))
	require.NoError(t, anpIndexer.Add(&v1alpha1.AdminNetworkPolicy{Name: "without-dns"}))
	require.NoError(t, cnpIndexer.Add(&v1alpha2.ClusterNetworkPolicy{Name: "without-dns"}))
	ctrl.anpsLister = anplisters.NewAdminNetworkPolicyLister(anpIndexer)
	ctrl.cnpsLister = cnplisters.NewClusterNetworkPolicyLister(cnpIndexer)

	for _, policy := range []struct {
		key         string
		name        string
		portGroupID string
	}{
		{adminNetworkPolicyKey, getAnpName(anp.Name), getAnpName(anp.Name)},
		{clusterNetworkPolicyKey, getCnpName(cnp.Name), getCnpPortGroupName(cnp)},
	} {
		externalIDs := map[string]string{policy.key: policy.portGroupID}
		pgName := policy.name + ".pg"
		fakeController.mockOvnClient.EXPECT().ListPortGroups(externalIDs).Return([]ovnnb.PortGroup{{Name: pgName}}, nil)
		fakeController.mockOvnClient.EXPECT().DeletePortGroup(pgName).Return(nil)
		fakeController.mockOvnClient.EXPECT().DeleteAddressSets(map[string]string{policy.key: policy.name + "/ingress"}).Return(nil)
		fakeController.mockOvnClient.EXPECT().DeleteAddressSets(map[string]string{policy.key: policy.name + "/egress"}).Return(nil)
	}
	fakeController.mockOvnClient.EXPECT().ListPortGroups(map[string]string{adminNetworkPolicyKey: ""}).Return(nil, nil)
	fakeController.mockOvnClient.EXPECT().ListAddressSets(map[string]string{adminNetworkPolicyKey: ""}).Return(nil, nil)
	fakeController.mockOvnClient.EXPECT().ListPortGroups(map[string]string{clusterNetworkPolicyKey: ""}).Return(nil, nil)
	fakeController.mockOvnClient.EXPECT().ListAddressSets(map[string]string{clusterNetworkPolicyKey: ""}).Return(nil, nil)

	require.NoError(t, ctrl.gcDisabledDNSNameResolvers())
}

func TestGcDisabledDNSNameResolversDeletesOrphanedPolicyResources(t *testing.T) {
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{EnableANP: true})
	require.NoError(t, err)
	ctrl := fakeController.fakeController
	ctrl.anpsLister = anplisters.NewAdminNetworkPolicyLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))
	ctrl.cnpsLister = cnplisters.NewClusterNetworkPolicyLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))

	anpPG := ovnnb.PortGroup{
		Name:        "anp-orphan.pg",
		ExternalIDs: map[string]string{adminNetworkPolicyKey: "anp-orphan"},
	}
	cnpPG := ovnnb.PortGroup{
		Name:        "cnp-orphan.pg",
		ExternalIDs: map[string]string{clusterNetworkPolicyKey: "cnp-orphan"},
	}
	fakeController.mockOvnClient.EXPECT().ListPortGroups(map[string]string{adminNetworkPolicyKey: ""}).Return([]ovnnb.PortGroup{anpPG}, nil)
	fakeController.mockOvnClient.EXPECT().DeletePortGroup(anpPG.Name).Return(nil)
	fakeController.mockOvnClient.EXPECT().ListAddressSets(map[string]string{adminNetworkPolicyKey: ""}).Return([]ovnnb.AddressSet{{
		Name:        "anp-orphan.egress.0.rule.ipv4",
		ExternalIDs: map[string]string{adminNetworkPolicyKey: "anp-orphan/egress"},
	}}, nil)
	fakeController.mockOvnClient.EXPECT().DeleteAddressSets(map[string]string{adminNetworkPolicyKey: "anp-orphan/egress"}).Return(nil)

	fakeController.mockOvnClient.EXPECT().ListPortGroups(map[string]string{clusterNetworkPolicyKey: ""}).Return([]ovnnb.PortGroup{cnpPG}, nil)
	fakeController.mockOvnClient.EXPECT().DeletePortGroup(cnpPG.Name).Return(nil)
	fakeController.mockOvnClient.EXPECT().ListAddressSets(map[string]string{clusterNetworkPolicyKey: ""}).Return([]ovnnb.AddressSet{{
		Name:        "cnp-orphan.egress.0.rule.ipv4",
		ExternalIDs: map[string]string{clusterNetworkPolicyKey: "cnp-orphan/egress"},
	}}, nil)
	fakeController.mockOvnClient.EXPECT().DeleteAddressSets(map[string]string{clusterNetworkPolicyKey: "cnp-orphan/egress"}).Return(nil)

	require.NoError(t, ctrl.gcDisabledDNSNameResolvers())
}

func TestGcDisabledDNSNameResolversPreservesPoliciesWithoutDomains(t *testing.T) {
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{EnableANP: true})
	require.NoError(t, err)
	ctrl := fakeController.fakeController

	anp := &v1alpha1.AdminNetworkPolicy{Name: "live-anp"}
	cnp := &v1alpha2.ClusterNetworkPolicy{Name: "live-cnp"}
	anpIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	cnpIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, anpIndexer.Add(anp))
	require.NoError(t, cnpIndexer.Add(cnp))
	ctrl.anpsLister = anplisters.NewAdminNetworkPolicyLister(anpIndexer)
	ctrl.cnpsLister = cnplisters.NewClusterNetworkPolicyLister(cnpIndexer)

	anpID := getAnpName(anp.Name)
	cnpID := getCnpName(cnp.Name)
	cnpPGID := getCnpPortGroupName(cnp)
	fakeController.mockOvnClient.EXPECT().ListPortGroups(map[string]string{adminNetworkPolicyKey: ""}).Return([]ovnnb.PortGroup{{
		Name:        "live-anp.pg",
		ExternalIDs: map[string]string{adminNetworkPolicyKey: anpID},
	}}, nil)
	fakeController.mockOvnClient.EXPECT().ListAddressSets(map[string]string{adminNetworkPolicyKey: ""}).Return([]ovnnb.AddressSet{{
		Name:        "live-anp.egress.0.rule.ipv4",
		ExternalIDs: map[string]string{adminNetworkPolicyKey: anpID + "/egress"},
	}}, nil)
	fakeController.mockOvnClient.EXPECT().ListPortGroups(map[string]string{clusterNetworkPolicyKey: ""}).Return([]ovnnb.PortGroup{{
		Name:        "live-cnp.pg",
		ExternalIDs: map[string]string{clusterNetworkPolicyKey: cnpPGID},
	}}, nil)
	fakeController.mockOvnClient.EXPECT().ListAddressSets(map[string]string{clusterNetworkPolicyKey: ""}).Return([]ovnnb.AddressSet{{
		Name:        "live-cnp.egress.0.rule.ipv4",
		ExternalIDs: map[string]string{clusterNetworkPolicyKey: cnpID + "/egress"},
	}}, nil)

	require.NoError(t, ctrl.gcDisabledDNSNameResolvers())
}

func TestHandleDeleteNpReturnsMeterError(t *testing.T) {
	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient
	ctrl.npKeyMutex = keymutex.NewHashed(1)
	ctrl.npsLister = netlisters.NewNetworkPolicyLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))
	meterErr := errors.New("meter delete failed")

	mockOvnClient.EXPECT().DeleteMeter("allow.default_to-lport_meter").Return(meterErr)
	mockOvnClient.EXPECT().DeleteMeter("allow.default_from-lport_meter").Return(nil)
	mockOvnClient.EXPECT().DeletePortGroup("allow.default").Return(nil)
	mockOvnClient.EXPECT().DeleteAddressSets(map[string]string{
		networkPolicyKey: "default/allow/ingress",
	}).Return(nil)
	mockOvnClient.EXPECT().DeleteAddressSets(map[string]string{
		networkPolicyKey: "default/allow/egress",
	}).Return(nil)

	err := ctrl.handleDeleteNp(networkPolicyDeleteRequest{
		key:           "default/allow",
		portGroupName: "allow.default",
	})
	require.ErrorIs(t, err, meterErr)
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

func newMarkAndCleanLSPTestController(t *testing.T, opts *FakeControllerOptions) (*Controller, *mockovs.MockNbClient) {
	t.Helper()
	fakeController, err := newFakeControllerWithOptions(t, opts)
	require.NoError(t, err)

	ctrl := fakeController.fakeController
	ctrl.config.EnableKeepVMIP = false
	ctrl.addNodeQueue = newTypedRateLimitingQueue[string]("AddNode", nil)
	ctrl.virtualIpsLister = kubeovnlisters.NewVipLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))

	previousLastNoPodLSP := lastNoPodLSP
	lastNoPodLSP = strset.New()
	t.Cleanup(func() { lastNoPodLSP = previousLastNoPodLSP })
	return ctrl, fakeController.mockOvnClient
}

func TestMarkAndCleanLSPSweepsOrphanOnSecondCycle(t *testing.T) {
	tests := []struct {
		name          string
		ip            *kubeovnv1.IP
		wantIPRemains bool
	}{
		{name: "orphan without ip cr"},
		{
			name: "non-reserved ip cr is deleted with the lsp",
			ip:   &kubeovnv1.IP{Name: "orphan", Spec: kubeovnv1.IPSpec{Subnet: util.DefaultSubnet}},
		},
		{
			name:          "reserved ip cr is kept",
			ip:            &kubeovnv1.IP{Name: "orphan", Labels: map[string]string{util.IPReservedLabel: "true"}, Spec: kubeovnv1.IPSpec{Subnet: util.DefaultSubnet}},
			wantIPRemains: true,
		},
		{
			name: "ip cr without subnet is deleted with the lsp",
			ip:   &kubeovnv1.IP{Name: "orphan"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &FakeControllerOptions{}
			if tt.ip != nil {
				opts.IPs = []*kubeovnv1.IP{tt.ip}
			}
			ctrl, mockOvnClient := newMarkAndCleanLSPTestController(t, opts)

			mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(false, nil).Return([]ovnnb.LogicalSwitchPort{
				{Name: "orphan", ExternalIDs: map[string]string{"pod": "default/orphan"}},
			}, nil).Times(2)

			// First cycle only marks the orphan; no delete is expected yet.
			require.NoError(t, ctrl.markAndCleanLSP())
			require.True(t, lastNoPodLSP.Has("orphan"))

			mockOvnClient.EXPECT().DeleteLogicalSwitchPort("orphan").Return(nil)
			require.NoError(t, ctrl.markAndCleanLSP())

			_, err := ctrl.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), "orphan", metav1.GetOptions{})
			if tt.wantIPRemains {
				require.NoError(t, err)
			} else {
				require.True(t, k8serrors.IsNotFound(err), "expected ip cr to be gone, got %v", err)
			}
		})
	}
}

func TestMarkAndCleanLSPSweepErrors(t *testing.T) {
	errBoom := errors.New("boom")
	ip := &kubeovnv1.IP{Name: "orphan", Spec: kubeovnv1.IPSpec{Subnet: util.DefaultSubnet}}

	tests := []struct {
		name    string
		ips     []*kubeovnv1.IP
		setup   func(*kubeovnfake.Clientset, *mockovs.MockNbClient)
		wantErr bool
	}{
		{
			name: "list lsp error aborts the cycle",
			setup: func(_ *kubeovnfake.Clientset, m *mockovs.MockNbClient) {
				m.EXPECT().ListNormalLogicalSwitchPorts(false, nil).Return(nil, errBoom)
			},
			wantErr: true,
		},
		{
			name: "delete lsp error aborts the cycle",
			setup: func(_ *kubeovnfake.Clientset, m *mockovs.MockNbClient) {
				m.EXPECT().ListNormalLogicalSwitchPorts(false, nil).Return([]ovnnb.LogicalSwitchPort{{Name: "orphan"}}, nil)
				m.EXPECT().DeleteLogicalSwitchPort("orphan").Return(errBoom)
			},
			wantErr: true,
		},
		{
			name: "get ip error aborts the cycle",
			setup: func(c *kubeovnfake.Clientset, m *mockovs.MockNbClient) {
				m.EXPECT().ListNormalLogicalSwitchPorts(false, nil).Return([]ovnnb.LogicalSwitchPort{{Name: "orphan"}}, nil)
				m.EXPECT().DeleteLogicalSwitchPort("orphan").Return(nil)
				c.PrependReactor("get", "ips", func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errBoom
				})
			},
			wantErr: true,
		},
		{
			name: "ip already deleted concurrently is skipped",
			ips:  []*kubeovnv1.IP{ip},
			setup: func(c *kubeovnfake.Clientset, m *mockovs.MockNbClient) {
				m.EXPECT().ListNormalLogicalSwitchPorts(false, nil).Return([]ovnnb.LogicalSwitchPort{{Name: "orphan"}}, nil)
				m.EXPECT().DeleteLogicalSwitchPort("orphan").Return(nil)
				c.PrependReactor("delete", "ips", func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, k8serrors.NewNotFound(schema.GroupResource{Resource: "ips"}, "orphan")
				})
			},
		},
		{
			name: "delete ip error aborts the cycle",
			ips:  []*kubeovnv1.IP{ip},
			setup: func(c *kubeovnfake.Clientset, m *mockovs.MockNbClient) {
				m.EXPECT().ListNormalLogicalSwitchPorts(false, nil).Return([]ovnnb.LogicalSwitchPort{{Name: "orphan"}}, nil)
				m.EXPECT().DeleteLogicalSwitchPort("orphan").Return(nil)
				c.PrependReactor("delete", "ips", func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errBoom
				})
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl, mockOvnClient := newMarkAndCleanLSPTestController(t, &FakeControllerOptions{IPs: tt.ips})
			kubeOvnClient, ok := ctrl.config.KubeOvnClient.(*kubeovnfake.Clientset)
			require.True(t, ok)
			// Pre-mark the orphan so the sweep runs on this single cycle.
			lastNoPodLSP = strset.New("orphan")
			tt.setup(kubeOvnClient, mockOvnClient)

			err := ctrl.markAndCleanLSP()
			if tt.wantErr {
				require.ErrorIs(t, err, errBoom)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGcChassisWithExternalVpcDisabled(t *testing.T) {
	errBoom := errors.New("boom")
	liveNode := &corev1.Node{Name: "node-1", Annotations: map[string]string{util.ChassisAnnotation: "chassis-live"}}
	renamedNode := &corev1.Node{Name: "node-2", Annotations: map[string]string{util.ChassisAnnotation: "chassis-renamed"}}
	notReadyNode := &corev1.Node{Name: "node-3"}

	tests := []struct {
		name    string
		nodes   []*corev1.Node
		setup   func(*mockovs.MockSbClient)
		wantErr bool
	}{
		{
			name:  "chassis of a live node is kept and nodes without chassis annotation are skipped",
			nodes: []*corev1.Node{liveNode, notReadyNode},
			setup: func(m *mockovs.MockSbClient) {
				m.EXPECT().ListChassis().Return(&[]ovnsb.Chassis{{Name: "chassis-live", Hostname: "node-1"}}, nil)
			},
		},
		{
			name:  "chassis whose hostname no longer matches its node is deleted",
			nodes: []*corev1.Node{renamedNode},
			setup: func(m *mockovs.MockSbClient) {
				m.EXPECT().ListChassis().Return(&[]ovnsb.Chassis{{Name: "chassis-renamed", Hostname: "old-name"}}, nil)
				m.EXPECT().DeleteChassis("chassis-renamed").Return(nil).MinTimes(1)
			},
		},
		{
			name:  "untagged chassis is still gc'd when external vpc is disabled",
			nodes: nil,
			setup: func(m *mockovs.MockSbClient) {
				m.EXPECT().ListChassis().Return(&[]ovnsb.Chassis{{Name: "chassis-orphan", Hostname: "gone"}}, nil)
				m.EXPECT().DeleteChassis("chassis-orphan").Return(nil)
			},
		},
		{
			name: "list chassis error aborts gc",
			setup: func(m *mockovs.MockSbClient) {
				m.EXPECT().ListChassis().Return(nil, errBoom)
			},
			wantErr: true,
		},
		{
			name:  "delete error on hostname mismatch aborts gc",
			nodes: []*corev1.Node{renamedNode},
			setup: func(m *mockovs.MockSbClient) {
				m.EXPECT().ListChassis().Return(&[]ovnsb.Chassis{{Name: "chassis-renamed", Hostname: "old-name"}}, nil)
				m.EXPECT().DeleteChassis("chassis-renamed").Return(errBoom)
			},
			wantErr: true,
		},
		{
			name: "delete error on unclaimed chassis aborts gc",
			setup: func(m *mockovs.MockSbClient) {
				m.EXPECT().ListChassis().Return(&[]ovnsb.Chassis{{Name: "chassis-orphan", Hostname: "gone"}}, nil)
				m.EXPECT().DeleteChassis("chassis-orphan").Return(errBoom)
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Nodes: tt.nodes})
			require.NoError(t, err)
			tt.setup(fakeController.mockOvnSbClient)

			err = fakeController.fakeController.gcChassis()
			if tt.wantErr {
				require.ErrorIs(t, err, errBoom)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestGcChassisUnderExternalVpcSkipsUntaggedChassis exercises the
// --enable-external-vpc gating added to gcChassis: an untagged chassis is an
// externally-managed gateway (e.g. a non-k8s KVM hypervisor bridged into the
// same OVN deployment) and must never be deleted under this flag, whether
// it's unclaimed by any node or merely has a hostname that doesn't match a
// node's chassis annotation.
func TestGcChassisUnderExternalVpcSkipsUntaggedChassis(t *testing.T) {
	t.Run("unclaimed untagged chassis survives", func(t *testing.T) {
		fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{})
		require.NoError(t, err)
		ctrl := fakeController.fakeController
		ctrl.config.EnableExternalVpc = true

		fakeController.mockOvnSbClient.EXPECT().ListChassis().Return(&[]ovnsb.Chassis{
			{Name: "external-hv-1", Hostname: "external-hv-1"},
		}, nil)
		// No DeleteChassis expectation: gomock fails if the untagged,
		// unclaimed chassis is deleted.
		require.NoError(t, ctrl.gcChassis())
	})

	t.Run("untagged chassis with a hostname mismatch survives", func(t *testing.T) {
		fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Nodes: []*corev1.Node{{
				Name:        "node-1",
				Annotations: map[string]string{util.ChassisAnnotation: "external-hv-1"},
			}},
		})
		require.NoError(t, err)
		ctrl := fakeController.fakeController
		ctrl.config.EnableExternalVpc = true

		fakeController.mockOvnSbClient.EXPECT().ListChassis().Return(&[]ovnsb.Chassis{
			{Name: "external-hv-1", Hostname: "some-other-host"},
		}, nil)
		// No DeleteChassis expectation: gomock fails if the untagged chassis
		// is deleted despite the hostname mismatch against node-1's
		// annotation (a coincidental name collision, not actually node-1's
		// chassis).
		require.NoError(t, ctrl.gcChassis())
	})

	t.Run("untagged chassis with ExternalIDs set but no vendor key survives", func(t *testing.T) {
		fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{})
		require.NoError(t, err)
		ctrl := fakeController.fakeController
		ctrl.config.EnableExternalVpc = true

		fakeController.mockOvnSbClient.EXPECT().ListChassis().Return(&[]ovnsb.Chassis{
			{Name: "external-hv-1", Hostname: "external-hv-1", ExternalIDs: map[string]string{"other": "tag"}},
		}, nil)
		require.NoError(t, ctrl.gcChassis())
	})
}

// TestGcChassisUnderExternalVpcSelfHealsUntaggedLiveNodeChassis covers the
// upgrade/migration gap a reviewer flagged: a chassis that genuinely belongs
// to a still-live k8s node but is missing the vendor tag (an upgrade from
// before vendor tagging existed, or a race with initNodeChassis/
// UpdateChassisTag) must not be permanently excluded from GC like a real
// external chassis - gcChassis must recognize it via its hostname matching a
// current Node, tag it, and subject it to the normal match/orphan logic.
func TestGcChassisUnderExternalVpcSelfHealsUntaggedLiveNodeChassis(t *testing.T) {
	errBoom := errors.New("boom")

	t.Run("untagged chassis matching a live, correctly-annotated node is tagged and kept", func(t *testing.T) {
		fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Nodes: []*corev1.Node{{
				Name:        "node-1",
				Annotations: map[string]string{util.ChassisAnnotation: "node-1-chassis"},
			}},
		})
		require.NoError(t, err)
		ctrl := fakeController.fakeController
		ctrl.config.EnableExternalVpc = true

		fakeController.mockOvnSbClient.EXPECT().ListChassis().Return(&[]ovnsb.Chassis{
			{Name: "node-1-chassis", Hostname: "node-1"},
		}, nil)
		fakeController.mockOvnSbClient.EXPECT().UpdateChassisTag("node-1-chassis", "node-1").Return(nil)
		// No DeleteChassis expectation: the node is live and correctly
		// annotated, so the now-tagged chassis must be kept, not swept as an
		// orphan in the same pass.
		require.NoError(t, ctrl.gcChassis())
	})

	t.Run("untagged chassis matching a live but not-yet-annotated node is tagged and deferred, not gc'd", func(t *testing.T) {
		// The node hasn't gotten its ChassisAnnotation patched yet (same
		// race as the readiness gate, just past it). Self-healing the vendor
		// tag alone doesn't give this chassis a matching annotation this
		// tick, so it must be deferred rather than swept as an orphan in the
		// same pass - a genuinely external chassis can never share a
		// hostname with a live k8s Node, so treating the match as pending
		// convergence (not absence) is safe. Confirms the tag is still
		// applied, but DeleteChassis is never called.
		fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Nodes: []*corev1.Node{{Name: "node-1"}},
		})
		require.NoError(t, err)
		ctrl := fakeController.fakeController
		ctrl.config.EnableExternalVpc = true

		fakeController.mockOvnSbClient.EXPECT().ListChassis().Return(&[]ovnsb.Chassis{
			{Name: "node-1-chassis", Hostname: "node-1"},
		}, nil)
		fakeController.mockOvnSbClient.EXPECT().UpdateChassisTag("node-1-chassis", "node-1").Return(nil)
		// No DeleteChassis expectation: gomock fails if the chassis is
		// deleted before the node's annotation converges to it.
		require.NoError(t, ctrl.gcChassis())
	})

	t.Run("tag failure aborts gc", func(t *testing.T) {
		fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Nodes: []*corev1.Node{{
				Name:        "node-1",
				Annotations: map[string]string{util.ChassisAnnotation: "node-1-chassis"},
			}},
		})
		require.NoError(t, err)
		ctrl := fakeController.fakeController
		ctrl.config.EnableExternalVpc = true

		fakeController.mockOvnSbClient.EXPECT().ListChassis().Return(&[]ovnsb.Chassis{
			{Name: "node-1-chassis", Hostname: "node-1"},
		}, nil)
		fakeController.mockOvnSbClient.EXPECT().UpdateChassisTag("node-1-chassis", "node-1").Return(errBoom)
		require.ErrorIs(t, ctrl.gcChassis(), errBoom)
	})
}

// TestGcChassisSkipsUntilNodeChassisInitialized covers the startup race a
// reviewer flagged: the periodic GC ticker (controller.go) starts and fires
// its first tick before initResourceOnce's initNodeChassis pass has tagged
// every live node's chassis, and possibly before kube-ovn-cni has even
// patched a just-allocated node's ChassisAnnotation. gcChassis must no-op
// entirely (not even list chassis) until that pass has completed once.
func TestGcChassisSkipsUntilNodeChassisInitialized(t *testing.T) {
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{})
	require.NoError(t, err)
	ctrl := fakeController.fakeController
	ctrl.nodeChassisInitialized.Store(false)

	// No ListChassis/DeleteChassis expectations at all: gomock fails this
	// test if gcChassis does anything besides returning nil early.
	require.NoError(t, ctrl.gcChassis())
}

// TestGcChassisUnderExternalVpcStillGCsTaggedChassis confirms the vendor
// filter does not regress cleanup of kube-ovn's own node chassis: a
// decommissioned/renamed k8s node's chassis is still tagged vendor=kube-ovn
// by UpdateChassisTag, so it must still be gc'd like today even when
// --enable-external-vpc is set.
func TestGcChassisUnderExternalVpcStillGCsTaggedChassis(t *testing.T) {
	kubeOvnTag := map[string]string{"vendor": util.CniTypeName}

	t.Run("unclaimed tagged chassis is deleted", func(t *testing.T) {
		fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{})
		require.NoError(t, err)
		ctrl := fakeController.fakeController
		ctrl.config.EnableExternalVpc = true

		fakeController.mockOvnSbClient.EXPECT().ListChassis().Return(&[]ovnsb.Chassis{
			{Name: "stale-node-chassis", Hostname: "gone", ExternalIDs: kubeOvnTag},
		}, nil)
		fakeController.mockOvnSbClient.EXPECT().DeleteChassis("stale-node-chassis").Return(nil)
		require.NoError(t, ctrl.gcChassis())
	})

	t.Run("tagged chassis of a live node is kept", func(t *testing.T) {
		fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Nodes: []*corev1.Node{{
				Name:        "node-1",
				Annotations: map[string]string{util.ChassisAnnotation: "chassis-live"},
			}},
		})
		require.NoError(t, err)
		ctrl := fakeController.fakeController
		ctrl.config.EnableExternalVpc = true

		fakeController.mockOvnSbClient.EXPECT().ListChassis().Return(&[]ovnsb.Chassis{
			{Name: "chassis-live", Hostname: "node-1", ExternalIDs: kubeOvnTag},
		}, nil)
		require.NoError(t, ctrl.gcChassis())
	})

	t.Run("tagged chassis with a hostname mismatch is deleted", func(t *testing.T) {
		fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Nodes: []*corev1.Node{{
				Name:        "node-2",
				Annotations: map[string]string{util.ChassisAnnotation: "chassis-renamed"},
			}},
		})
		require.NoError(t, err)
		ctrl := fakeController.fakeController
		ctrl.config.EnableExternalVpc = true

		fakeController.mockOvnSbClient.EXPECT().ListChassis().Return(&[]ovnsb.Chassis{
			{Name: "chassis-renamed", Hostname: "old-name", ExternalIDs: kubeOvnTag},
		}, nil)
		fakeController.mockOvnSbClient.EXPECT().DeleteChassis("chassis-renamed").Return(nil).MinTimes(1)
		require.NoError(t, ctrl.gcChassis())
	})
}

// TestGcChassisConcurrentReactiveAndPeriodicRunsSerialize exercises the
// concurrency risk introduced by adding gcChassis to the periodic GC ticker
// (pkg/controller/controller.go) alongside its pre-existing reactive call
// from UpdateChassisTag: both can now call gcChassis at the same time.
// gcChassisMu must serialize them so ListChassis/DeleteChassis sequences
// never interleave.
func TestGcChassisConcurrentReactiveAndPeriodicRunsSerialize(t *testing.T) {
	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController

	var concurrent, maxConcurrent int32
	fakeController.mockOvnSbClient.EXPECT().ListChassis().DoAndReturn(func() (*[]ovnsb.Chassis, error) {
		n := atomic.AddInt32(&concurrent, 1)
		for {
			old := atomic.LoadInt32(&maxConcurrent)
			if n <= old || atomic.CompareAndSwapInt32(&maxConcurrent, old, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&concurrent, -1)
		return &[]ovnsb.Chassis{}, nil
	}).Times(2)

	// require.NoError inside a goroutine only unwinds that goroutine (via
	// t.FailNow -> runtime.Goexit); the parent keeps running past wg.Wait
	// unaware, and the assertion below can mask the real failure. Collect
	// each call's error and assert on it from the parent instead.
	errs := make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); errs[0] = ctrl.gcChassis() }()
	go func() { defer wg.Done(); errs[1] = ctrl.gcChassis() }()
	wg.Wait()

	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, int32(1), atomic.LoadInt32(&maxConcurrent), "gcChassisMu must serialize concurrent gcChassis calls")
}
