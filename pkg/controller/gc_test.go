package controller

import (
	"errors"
	"fmt"
	"testing"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/scylladb/go-set/strset"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"

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
				ObjectMeta: metav1.ObjectMeta{Name: "vm-primary", Namespace: "ns1"},
				Spec:       kubevirtv1.VirtualMachineSpec{Template: vmTemplate(nil, nil)},
			},
			{
				// Default multus network: primary lsp is skipped, but the attachment
				// network derived from NetworkName must still be kept.
				ObjectMeta: metav1.ObjectMeta{Name: "vm-default-multus", Namespace: "ns2"},
				Spec: kubevirtv1.VirtualMachineSpec{Template: vmTemplate([]kubevirtv1.Network{{
					Name: "secondary",
					NetworkSource: kubevirtv1.NetworkSource{
						Multus: &kubevirtv1.MultusNetwork{Default: true, NetworkName: "ns2/net2"},
					},
				}}, nil)},
			},
			{
				// NAD annotation contributes an attachment lsp on top of the primary one.
				ObjectMeta: metav1.ObjectMeta{Name: "vm-nad", Namespace: "ns3"},
				Spec:       kubevirtv1.VirtualMachineSpec{Template: vmTemplate(nil, map[string]string{nadv1.NetworkAttachmentAnnot: "netx"})},
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

func TestGcNetworkPolicyDeletesLeftoversWhenDisabled(t *testing.T) {
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Nodes: []*corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
	})
	require.NoError(t, err)
	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient
	ctrl.config.EnableNP = false

	npPG := ovnnb.PortGroup{
		Name: "allow.default",
		ExternalIDs: map[string]string{
			networkPolicyKey: "default/allow",
		},
	}
	nodePG := ovnnb.PortGroup{
		Name: "node.node1",
		ExternalIDs: map[string]string{
			networkPolicyKey: "node/node1",
		},
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

func TestGcAdminNetworkPolicySkipsWhenEnabled(t *testing.T) {
	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	ctrl.config.EnableANP = true

	require.NoError(t, ctrl.gcAdminNetworkPolicy())
}

func TestGcAdminNetworkPolicyDeletesLeftoversWhenDisabled(t *testing.T) {
	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient
	ctrl.config.EnableANP = false

	anpPG := ovnnb.PortGroup{
		Name: "anp.foo",
		ExternalIDs: map[string]string{
			adminNetworkPolicyKey: "anp.foo",
		},
	}
	banpPG := ovnnb.PortGroup{
		Name: "banp.bar",
		ExternalIDs: map[string]string{
			baselineAdminNetworkPolicyKey: "banp.bar",
		},
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
