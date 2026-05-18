package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestAddPolicyRouteForU2OInterconn_OverlayOnlyRouting(t *testing.T) {
	t.Parallel()

	const (
		subnetName  = "subnet-a"
		overlayName = "overlay-a"
		vlanName    = "vlan1"
		cidr        = "10.16.1.0/24"
		overlayCIDR = "10.244.0.0/16"
		gateway     = "10.16.1.1"
	)

	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: subnetName},
		Spec: kubeovnv1.SubnetSpec{
			Vpc:                util.DefaultVpc,
			Vlan:               vlanName,
			CIDRBlock:          cidr,
			Gateway:            gateway,
			U2OInterconnection: true,
			U2OFeatures: kubeovnv1.U2OFeatures{
				OverlayOnlyRouting: true,
			},
		},
		Status: kubeovnv1.SubnetStatus{
			U2OInterconnectionVPC: util.DefaultVpc,
		},
	}
	overlaySubnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: overlayName},
		Spec: kubeovnv1.SubnetSpec{
			Vpc:       util.DefaultVpc,
			CIDRBlock: overlayCIDR,
		},
	}

	fc := newFakeController(t)
	ctrl := fc.fakeController
	mockOvnClient := fc.mockOvnClient
	require.NoError(t, fc.fakeInformers.subnetInformer.Informer().GetStore().Add(subnet))
	require.NoError(t, fc.fakeInformers.subnetInformer.Informer().GetStore().Add(overlaySubnet))

	u2oExcludeIP4Ag := strings.ReplaceAll(fmt.Sprintf(util.U2OExcludeIPAg, subnet.Name, "ip4"), "-", ".")
	u2oExcludeIP6Ag := strings.ReplaceAll(fmt.Sprintf(util.U2OExcludeIPAg, subnet.Name, "ip6"), "-", ".")
	overlayCIDRs4Ag, overlayCIDRs6Ag := u2oOverlayCIDRsAddressSetNames(subnet.Spec.Vpc)
	overlayExternalIDs := u2oOverlayCIDRsAddressSetExternalIDs(subnet.Spec.Vpc)
	overlayPolicyExternalIDs := map[string]string{
		"vendor":                      util.CniTypeName,
		"subnet":                      subnet.Name,
		"isU2ORoutePolicy":            "true",
		"isU2OOverlayOnlyRoutePolicy": "true",
	}
	routeExternalIDs := map[string]string{
		"vendor":           util.CniTypeName,
		"subnet":           subnet.Name,
		"isU2ORoutePolicy": "true",
	}

	mockOvnClient.EXPECT().CreateAddressSet(u2oExcludeIP4Ag, routeExternalIDs).Return(nil)
	mockOvnClient.EXPECT().CreateAddressSet(u2oExcludeIP6Ag, routeExternalIDs).Return(nil)
	mockOvnClient.EXPECT().CreateAddressSet(overlayCIDRs4Ag, overlayExternalIDs).Return(nil)
	mockOvnClient.EXPECT().CreateAddressSet(overlayCIDRs6Ag, overlayExternalIDs).Return(nil)
	mockOvnClient.EXPECT().AddressSetUpdateAddress(overlayCIDRs4Ag, overlayCIDR).Return(nil)
	mockOvnClient.EXPECT().AddressSetUpdateAddress(overlayCIDRs6Ag).Return(nil)
	mockOvnClient.EXPECT().AddLogicalRouterPolicy(util.DefaultVpc, util.U2OSubnetPolicyPriority, fmt.Sprintf("ip4.src == $%s && ip4.dst == %s", overlayCIDRs4Ag, cidr), string(kubeovnv1.PolicyRouteActionAllow), ([]string)(nil), ([]string)(nil), overlayPolicyExternalIDs).Return(nil)
	mockOvnClient.EXPECT().AddLogicalRouterPolicy(util.DefaultVpc, util.SubnetRouterPolicyPriority, fmt.Sprintf("ip4.dst == $%s && ip4.src == %s", u2oExcludeIP4Ag, cidr), string(kubeovnv1.PolicyRouteActionReroute), []string{gateway}, ([]string)(nil), overlayPolicyExternalIDs).Return(nil)
	mockOvnClient.EXPECT().AddLogicalRouterPolicy(util.DefaultVpc, util.U2OSameSubnetPolicyPriority, fmt.Sprintf("ip4.src == %s && ip4.dst == %s", cidr, cidr), string(kubeovnv1.PolicyRouteActionAllow), ([]string)(nil), ([]string)(nil), overlayPolicyExternalIDs).Return(nil)
	mockOvnClient.EXPECT().AddLogicalRouterPolicy(util.DefaultVpc, util.U2OPhysicalGatewayPolicyPriority, fmt.Sprintf("ip4.src == %s", cidr), string(kubeovnv1.PolicyRouteActionReroute), []string{gateway}, ([]string)(nil), routeExternalIDs).Return(nil)
	mockOvnClient.EXPECT().GetLogicalRouter(util.DefaultVpc, true).Return(&ovnnb.LogicalRouter{Name: util.DefaultVpc}, nil)
	mockOvnClient.EXPECT().ListLogicalRouterPolicies(util.DefaultVpc, -1, map[string]string{
		"isU2ORoutePolicy": "true",
		"vendor":           util.CniTypeName,
		"subnet":           subnet.Name,
	}, true).Return(nil, nil)

	err := ctrl.addPolicyRouteForU2OInterconn(subnet)
	require.NoError(t, err)
}

func TestSyncU2OOverlayCIDRsAddressSet_UpdatesAfterOverlaySubnetDeletion(t *testing.T) {
	t.Parallel()

	const (
		vpcName      = util.DefaultVpc
		overlayAName = "overlay-a"
		overlayBName = "overlay-b"
		underlayName = "underlay-a"
		overlayAV4   = "10.244.0.0/16"
		overlayAV6   = "fd00:244::/64"
		overlayBV4   = "10.245.0.0/16"
		overlayBV6   = "fd00:245::/64"
		underlayCIDR = "10.16.1.0/24"
		underlayVlan = "vlan1"
	)

	overlayA := &kubeovnv1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: overlayAName}, Spec: kubeovnv1.SubnetSpec{Vpc: vpcName, CIDRBlock: overlayAV4 + "," + overlayAV6}}
	overlayB := &kubeovnv1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: overlayBName}, Spec: kubeovnv1.SubnetSpec{Vpc: vpcName, CIDRBlock: overlayBV4 + "," + overlayBV6}}
	underlay := &kubeovnv1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: underlayName}, Spec: kubeovnv1.SubnetSpec{Vpc: vpcName, Vlan: underlayVlan, CIDRBlock: underlayCIDR}}

	fc := newFakeController(t)
	ctrl := fc.fakeController
	mockOvnClient := fc.mockOvnClient
	require.NoError(t, fc.fakeInformers.subnetInformer.Informer().GetStore().Add(overlayA))
	require.NoError(t, fc.fakeInformers.subnetInformer.Informer().GetStore().Add(overlayB))
	require.NoError(t, fc.fakeInformers.subnetInformer.Informer().GetStore().Add(underlay))

	overlayCIDRs4Ag, overlayCIDRs6Ag := u2oOverlayCIDRsAddressSetNames(vpcName)
	overlayExternalIDs := u2oOverlayCIDRsAddressSetExternalIDs(vpcName)

	mockOvnClient.EXPECT().CreateAddressSet(overlayCIDRs4Ag, overlayExternalIDs).Return(nil).Times(2)
	mockOvnClient.EXPECT().CreateAddressSet(overlayCIDRs6Ag, overlayExternalIDs).Return(nil).Times(2)
	mockOvnClient.EXPECT().AddressSetUpdateAddress(overlayCIDRs4Ag, gomock.Any(), gomock.Any()).DoAndReturn(func(_ string, addresses ...string) error {
		require.ElementsMatch(t, []string{overlayAV4, overlayBV4}, addresses)
		return nil
	})
	mockOvnClient.EXPECT().AddressSetUpdateAddress(overlayCIDRs6Ag, gomock.Any(), gomock.Any()).DoAndReturn(func(_ string, addresses ...string) error {
		require.ElementsMatch(t, []string{overlayAV6, overlayBV6}, addresses)
		return nil
	})
	mockOvnClient.EXPECT().AddressSetUpdateAddress(overlayCIDRs4Ag, overlayAV4).Return(nil)
	mockOvnClient.EXPECT().AddressSetUpdateAddress(overlayCIDRs6Ag, overlayAV6).Return(nil)

	v4CIDRs, v6CIDRs, err := ctrl.syncU2OOverlayCIDRsAddressSet(vpcName, "")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{overlayAV4, overlayBV4}, v4CIDRs)
	require.ElementsMatch(t, []string{overlayAV6, overlayBV6}, v6CIDRs)

	require.NoError(t, fc.fakeInformers.subnetInformer.Informer().GetStore().Delete(overlayB))
	v4CIDRs, v6CIDRs, err = ctrl.syncU2OOverlayCIDRsAddressSet(vpcName, "")
	require.NoError(t, err)
	require.Equal(t, []string{overlayAV4}, v4CIDRs)
	require.Equal(t, []string{overlayAV6}, v6CIDRs)
}

func Test_reconcileVips(t *testing.T) {
	t.Parallel()

	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient

	lspNamePrefix := "reconcile-vip-lsp"

	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ovn-test",
		},
		Spec: kubeovnv1.SubnetSpec{
			Vips: []string{"192.168.123.10", "192.168.123.11", "192.168.123.12", "192.168.123.13"},
		},
	}

	mockLsp := func(lsName, lspName, vip string) ovnnb.LogicalSwitchPort {
		return ovnnb.LogicalSwitchPort{
			Name: lspName,
			ExternalIDs: map[string]string{
				"ls": lsName,
			},
			Options: map[string]string{
				"virtual-ip": vip,
			},
		}
	}

	lsps := []ovnnb.LogicalSwitchPort{
		mockLsp("", lspNamePrefix+"-0", "192.168.123.8"),
		mockLsp("", lspNamePrefix+"-1", "192.168.123.9"),
		mockLsp("", lspNamePrefix+"-2", "192.168.123.10"),
	}

	t.Run("existent vips and new vips has intersection", func(t *testing.T) {
		mockOvnClient.EXPECT().ListLogicalSwitchPorts(true, gomock.Any(), gomock.Any()).Return(lsps, nil)
		mockOvnClient.EXPECT().DeleteLogicalSwitchPort(lspNamePrefix + "-0").Return(nil)
		mockOvnClient.EXPECT().DeleteLogicalSwitchPort(lspNamePrefix + "-1").Return(nil)
		mockOvnClient.EXPECT().CreateVirtualLogicalSwitchPorts(subnet.Name, "192.168.123.11", "192.168.123.12", "192.168.123.13").Return(nil)

		err := ctrl.reconcileVips(subnet)
		require.NoError(t, err)
	})

	t.Run("existent vips is empty", func(t *testing.T) {
		mockOvnClient.EXPECT().ListLogicalSwitchPorts(true, gomock.Any(), gomock.Any()).Return(nil, nil)
		mockOvnClient.EXPECT().CreateVirtualLogicalSwitchPorts(subnet.Name, "192.168.123.10", "192.168.123.11", "192.168.123.12", "192.168.123.13").Return(nil)

		err := ctrl.reconcileVips(subnet)
		require.NoError(t, err)
	})

	t.Run("new vips is empty", func(t *testing.T) {
		subnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ovn-test",
			},
		}

		mockOvnClient.EXPECT().ListLogicalSwitchPorts(true, gomock.Any(), gomock.Any()).Return(lsps, nil)
		mockOvnClient.EXPECT().DeleteLogicalSwitchPort(lspNamePrefix + "-0").Return(nil)
		mockOvnClient.EXPECT().DeleteLogicalSwitchPort(lspNamePrefix + "-1").Return(nil)
		mockOvnClient.EXPECT().DeleteLogicalSwitchPort(lspNamePrefix + "-2").Return(nil)
		mockOvnClient.EXPECT().CreateVirtualLogicalSwitchPorts(subnet.Name).Return(nil)

		err := ctrl.reconcileVips(subnet)
		require.NoError(t, err)
	})
}

func Test_syncVirtualPort(t *testing.T) {
	t.Parallel()

	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	fakeinformers := fakeController.fakeInformers
	mockOvnClient := fakeController.mockOvnClient

	lspNamePrefix := "sync-virt-lsp"

	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ovn-test",
		},
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock: "192.168.123.0/24",
			Vips:      []string{"192.168.123.10", "192.168.123.11", "192.168.123.12", "192.168.123.13"},
		},
	}

	err := fakeinformers.subnetInformer.Informer().GetStore().Add(subnet)
	require.NoError(t, err)

	mockLsp := func(lsName, lspName, vips string) ovnnb.LogicalSwitchPort {
		return ovnnb.LogicalSwitchPort{
			Name: lspName,
			ExternalIDs: map[string]string{
				"ls":   lsName,
				"vips": vips,
			},
		}
	}

	lsps := []ovnnb.LogicalSwitchPort{
		mockLsp("", lspNamePrefix+"-0", "192.168.123.10,192.168.123.11"),
		mockLsp("", lspNamePrefix+"-1", "192.168.123.10,192.168.123.11"),
	}

	mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return(lsps, nil)
	virtualParents := fmt.Sprintf("%s,%s", lspNamePrefix+"-0", lspNamePrefix+"-1")
	mockOvnClient.EXPECT().SetLogicalSwitchPortVirtualParents(subnet.Name, virtualParents, "192.168.123.10").Return(nil)
	mockOvnClient.EXPECT().SetLogicalSwitchPortVirtualParents(subnet.Name, virtualParents, "192.168.123.11").Return(nil)

	err = ctrl.syncVirtualPort(subnet.Name)
	require.NoError(t, err)
}

func Test_formatSubnet(t *testing.T) {
	t.Parallel()

	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	// enable := true
	disable := false

	tests := map[string]struct {
		input  *kubeovnv1.Subnet
		output *kubeovnv1.Subnet
	}{
		"simple subnet with cidr block only": {
			input: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "simple",
				},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock: "192.168.0.1/24",
				},
			},
			output: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "simple",
				},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock:   "192.168.0.0/24",
					Protocol:    kubeovnv1.ProtocolIPv4,
					Gateway:     "192.168.0.1",
					Vpc:         ctrl.config.ClusterRouter,
					ExcludeIps:  []string{"192.168.0.1"},
					Provider:    "ovn",
					GatewayType: kubeovnv1.GWDistributedType,
					EnableLb:    &ctrl.config.EnableLb,
				},
			},
		},
		"complete subnet that do not need to be formatted": {
			input: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "complete",
				},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock:   "192.168.0.0/24",
					Protocol:    kubeovnv1.ProtocolIPv4,
					Gateway:     "192.168.0.255",
					Vpc:         "test-vpc",
					ExcludeIps:  []string{"192.168.0.1", "192.168.0.255"},
					Provider:    "ovn.test-provider",
					GatewayType: kubeovnv1.GWCentralizedType,
					EnableLb:    &disable,
				},
			},
			output: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "complete",
				},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock:   "192.168.0.0/24",
					Protocol:    kubeovnv1.ProtocolIPv4,
					Gateway:     "192.168.0.255",
					Vpc:         "test-vpc",
					ExcludeIps:  []string{"192.168.0.1", "192.168.0.255"},
					Provider:    "ovn.test-provider",
					GatewayType: kubeovnv1.GWCentralizedType,
					EnableLb:    &disable,
				},
			},
		},
		"do not format gatewayType for custom VPC subnet": {
			input: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "custom-vpc",
				},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock:  "192.168.0.0/24",
					Protocol:   kubeovnv1.ProtocolIPv4,
					Gateway:    "192.168.0.255",
					Vpc:        "test-vpc",
					ExcludeIps: []string{"192.168.0.1", "192.168.0.255"},
					Provider:   "ovn.test-provider",
					EnableLb:   &disable,
				},
			},
			output: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "custom-vpc",
				},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock:  "192.168.0.0/24",
					Protocol:   kubeovnv1.ProtocolIPv4,
					Gateway:    "192.168.0.255",
					Vpc:        "test-vpc",
					ExcludeIps: []string{"192.168.0.1", "192.168.0.255"},
					Provider:   "ovn.test-provider",
					EnableLb:   &disable,
				},
			},
		},
		"do not format gatewayType for non ovn subnet": {
			input: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "external",
				},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock:  "192.168.0.0/24",
					Protocol:   kubeovnv1.ProtocolIPv4,
					Gateway:    "192.168.0.255",
					ExcludeIps: []string{"192.168.0.1", "192.168.0.255"},
					Provider:   "test-provider",
					EnableLb:   &disable,
				},
			},
			output: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "external",
				},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock:  "192.168.0.0/24",
					Protocol:   kubeovnv1.ProtocolIPv4,
					Gateway:    "192.168.0.255",
					ExcludeIps: []string{"192.168.0.1", "192.168.0.255"},
					Provider:   "test-provider",
					EnableLb:   &disable,
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ctrl.config.KubeOvnClient.KubeovnV1().Subnets().Create(context.Background(), tc.input, metav1.CreateOptions{})
			require.NoError(t, err)
			formattedSubnet, err := ctrl.formatSubnet(tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.output, formattedSubnet)
			err = ctrl.config.KubeOvnClient.KubeovnV1().Subnets().Delete(context.Background(), tc.input.Name, metav1.DeleteOptions{})
			require.NoError(t, err)
		})
	}
}
