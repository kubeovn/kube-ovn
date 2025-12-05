package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

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

func Test_checkAndUpdateExcludeIPs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		subnet         *kubeovnv1.Subnet
		expectedChange bool
		expectedIPs    []string
	}{
		{
			name: "gateway within CIDR should be added to excludeIPs",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-subnet-1",
				},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock:  "10.16.0.0/16",
					Gateway:    "10.16.0.1",
					ExcludeIps: []string{},
				},
			},
			expectedChange: true,
			expectedIPs:    []string{"10.16.0.1"},
		},
		{
			name: "gateway outside CIDR should not be added to excludeIPs",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-subnet-2",
				},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock:  "10.253.251.0/24",
					Gateway:    "10.34.251.254",
					ExcludeIps: []string{},
				},
			},
			expectedChange: true,
			expectedIPs:    []string{},
		},
		{
			name: "multiple gateways with mixed inside and outside CIDR",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-subnet-3",
				},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock:  "10.16.0.0/16,fd00::/64",
					Gateway:    "10.16.0.1,192.168.1.1,fd00::1",
					ExcludeIps: []string{},
				},
			},
			expectedChange: true,
			expectedIPs:    []string{"10.16.0.1", "fd00::1"},
		},
		{
			name: "gateway already in excludeIPs should not change",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-subnet-4",
				},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock:  "10.16.0.0/16",
					Gateway:    "10.16.0.1",
					ExcludeIps: []string{"10.16.0.1"},
				},
			},
			expectedChange: false,
			expectedIPs:    []string{"10.16.0.1"},
		},
		{
			name: "gateway outside CIDR with existing excludeIPs should not add gateway",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-subnet-5",
				},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock:  "10.253.251.0/24",
					Gateway:    "10.34.251.254",
					ExcludeIps: []string{"10.253.251.100"},
				},
			},
			expectedChange: false,
			expectedIPs:    []string{"10.253.251.100"},
		},
		{
			name: "IPv6 gateway outside CIDR should not be added",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-subnet-6",
				},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock:  "2001:db8::/32",
					Gateway:    "2001:db9::1",
					ExcludeIps: []string{},
				},
			},
			expectedChange: true,
			expectedIPs:    []string{},
		},
		{
			name: "cleanup gateway outside CIDR from existing excludeIPs",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-subnet-7",
				},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock:  "10.253.251.0/24",
					Gateway:    "10.34.251.254",
					ExcludeIps: []string{"10.253.251.254", "10.34.251.254"},
				},
			},
			expectedChange: true,
			expectedIPs:    []string{"10.253.251.254"},
		},
		{
			name: "cleanup only gateway IPs outside CIDR, keep other excludeIPs",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-subnet-8",
				},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock:  "10.253.251.0/24",
					Gateway:    "10.34.251.254",
					ExcludeIps: []string{"10.253.251.100", "10.34.251.254", "10.253.251.200"},
				},
			},
			expectedChange: true,
			expectedIPs:    []string{"10.253.251.100", "10.253.251.200"},
		},
		{
			name: "cleanup multiple gateway IPs outside CIDR",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-subnet-9",
				},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock:  "10.16.0.0/16,fd00::/64",
					Gateway:    "10.16.0.1,192.168.1.1,fd00::1,192.168.1.2",
					ExcludeIps: []string{"10.16.0.1", "192.168.1.1", "fd00::1", "192.168.1.2", "10.16.0.100"},
				},
			},
			expectedChange: true,
			expectedIPs:    []string{"10.16.0.1", "fd00::1", "10.16.0.100"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := checkAndUpdateExcludeIPs(tt.subnet)
			require.Equal(t, tt.expectedChange, changed, "expected change status mismatch")
			require.ElementsMatch(t, tt.expectedIPs, tt.subnet.Spec.ExcludeIps, "excludeIPs mismatch")
		})
	}
}
