package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestHandleAddVirtualIP_SwitchLBRuleUsesOwnMac(t *testing.T) {
	t.Parallel()

	subnet := &kubeovnv1.Subnet{
		Name: "test-subnet",
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock: "10.0.0.0/24",
			Gateway:   "10.0.0.1",
			Protocol:  kubeovnv1.ProtocolIPv4,
			Provider:  util.OvnProvider,
			Vpc:       "test-vpc",
		},
	}
	vip := &kubeovnv1.Vip{
		Name: "test-switch-lb-vip",
		Spec: kubeovnv1.VipSpec{
			Namespace: "default",
			Subnet:    subnet.Name,
			Type:      util.SwitchLBRuleVip,
			V4ip:      "10.0.0.50",
		},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet},
		Vips:    []*kubeovnv1.Vip{vip},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))
	const gatewayMac = "00:00:00:00:00:01"
	require.NoError(t, ctrl.ipam.RecordGatewayMAC(subnet.Name, gatewayMac))

	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)

	// GetLogicalRouterPort must never be called: the VIP's own IPAM-assigned mac is used
	// directly, it must never be replaced with the subnet gateway's mac.
	var lspMac string
	fc.mockOvnClient.EXPECT().
		CreateLogicalSwitchPort(subnet.Name, portName, vip.Spec.V4ip, gomock.Not(gatewayMac), vip.Name, vip.Spec.Namespace, false, "", "", false, nil, subnet.Spec.Vpc).
		DoAndReturn(func(_, _, _, mac, _, _ string, _ bool, _, _ string, _ bool, _ *ovs.DHCPOptionsUUIDs, _ string) error {
			lspMac = mac
			return nil
		})

	require.NoError(t, ctrl.handleAddVirtualIP(vip.Name))

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().Vips().Get(t.Context(), vip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, got.Status.Mac)
	require.NotEqual(t, gatewayMac, got.Status.Mac)
	require.Equal(t, got.Status.Mac, lspMac)
}

// TestHandleAddVirtualIP_SwitchLBRuleRepairsStaleGatewayMac covers vips created before
// the own-mac fix: their Status.Mac and lsp mac were left equal to the subnet gateway
// mac. On the next reconcile they must be repaired with a freshly allocated mac,
// keeping the same IP.
func TestHandleAddVirtualIP_SwitchLBRuleRepairsStaleGatewayMac(t *testing.T) {
	t.Parallel()

	subnet := &kubeovnv1.Subnet{
		Name: "test-subnet-repair",
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock: "10.0.1.0/24",
			Gateway:   "10.0.1.1",
			Protocol:  kubeovnv1.ProtocolIPv4,
			Provider:  util.OvnProvider,
			Vpc:       "test-vpc",
		},
	}
	const gatewayMac = "00:00:00:00:00:02"
	vip := &kubeovnv1.Vip{
		Name: "test-switch-lb-vip-repair",
		Spec: kubeovnv1.VipSpec{
			Namespace: "default",
			Subnet:    subnet.Name,
			Type:      util.SwitchLBRuleVip,
			V4ip:      "10.0.1.50",
		},
		Status: kubeovnv1.VipStatus{
			V4ip: "10.0.1.50",
			Mac:  gatewayMac,
		},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet},
		Vips:    []*kubeovnv1.Vip{vip},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))

	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
	// Simulate the pre-fix state: on a controller restart, init.go re-registers the vip's
	// nic in ipam using its (stale) Status.Mac before the subnet controller has recorded
	// the real gateway mac, so the registration itself does not see a conflict.
	_, _, _, err = ctrl.ipam.GetStaticAddress(vip.Name, portName, vip.Status.V4ip, &vip.Status.Mac, subnet.Name, false)
	require.NoError(t, err)
	require.NoError(t, ctrl.ipam.RecordGatewayMAC(subnet.Name, gatewayMac))

	var lspMac string
	fc.mockOvnClient.EXPECT().
		CreateLogicalSwitchPort(subnet.Name, portName, vip.Status.V4ip, gomock.Not(gatewayMac), vip.Name, vip.Spec.Namespace, false, "", "", false, nil, subnet.Spec.Vpc).
		DoAndReturn(func(_, _, _, mac, _, _ string, _ bool, _, _ string, _ bool, _ *ovs.DHCPOptionsUUIDs, _ string) error {
			lspMac = mac
			return nil
		})
	// Status.V4ip is already populated (unlike a brand-new vip), so handleUpdateVirtualParents
	// also (re)creates the virtual lsp for it.
	fc.mockOvnClient.EXPECT().CreateVirtualLogicalSwitchPort(vip.Name, subnet.Name, vip.Status.V4ip).Return(nil)

	require.NoError(t, ctrl.handleAddVirtualIP(vip.Name))

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().Vips().Get(t.Context(), vip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, vip.Status.V4ip, got.Status.V4ip)
	require.NotEqual(t, gatewayMac, got.Status.Mac)
	require.Equal(t, got.Status.Mac, lspMac)
}
