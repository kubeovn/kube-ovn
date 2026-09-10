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

	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)

	// GetLogicalRouterPort must never be called: the VIP's own IPAM-assigned mac is used
	// directly, it is not replaced with the subnet gateway's mac.
	fc.mockOvnClient.EXPECT().
		CreateLogicalSwitchPort(subnet.Name, portName, vip.Spec.V4ip, gomock.Not(""), vip.Name, vip.Spec.Namespace, false, "", "", false, nil, subnet.Spec.Vpc).
		Return(nil)
	fc.mockOvnClient.EXPECT().SetLogicalSwitchPortArpProxy(portName, true).Return(nil)

	require.NoError(t, ctrl.handleAddVirtualIP(vip.Name))

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().Vips().Get(t.Context(), vip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, got.Status.Mac)
}
