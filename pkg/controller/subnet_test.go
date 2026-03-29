package controller

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/internal"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func Test_readyToRemoveFinalizer(t *testing.T) {
	t.Parallel()

	now := metav1.NewTime(time.Now())

	tests := []struct {
		name   string
		subnet *kubeovnv1.Subnet
		want   bool
	}{
		{
			name:   "not deleted",
			subnet: &kubeovnv1.Subnet{},
			want:   false,
		},
		{
			name: "deleted with no IPs in use",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now},
				Status:     kubeovnv1.SubnetStatus{},
			},
			want: true,
		},
		{
			name: "deleted with V4 IPs in use",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now},
				Status:     kubeovnv1.SubnetStatus{V4UsingIPs: internal.NewBigInt(2), V6UsingIPs: internal.BigInt{}},
			},
			want: false,
		},
		{
			name: "deleted dual-stack with only V6 IPs in use",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now},
				Status:     kubeovnv1.SubnetStatus{V4UsingIPs: internal.BigInt{}, V6UsingIPs: internal.NewBigInt(3)},
			},
			want: false,
		},
		{
			name: "deleted dual-stack with both V4 and V6 IPs in use",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now},
				Status:     kubeovnv1.SubnetStatus{V4UsingIPs: internal.NewBigInt(1), V6UsingIPs: internal.NewBigInt(1)},
			},
			want: false,
		},
		{
			name: "deleted with only U2O interconnection IPv4 IP remaining",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now},
				Status: kubeovnv1.SubnetStatus{
					V4UsingIPs: internal.NewBigInt(1), V6UsingIPs: internal.BigInt{},
					U2OInterconnectionIP: "10.0.0.1",
				},
			},
			want: true,
		},
		{
			name: "deleted dual-stack with only U2O interconnection IPs remaining",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now},
				Status: kubeovnv1.SubnetStatus{
					V4UsingIPs: internal.NewBigInt(1), V6UsingIPs: internal.NewBigInt(1),
					U2OInterconnectionIP: "10.0.0.1,fd00::1",
				},
			},
			want: true,
		},
		{
			name: "deleted with U2O IP but extra IPs still in use",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now},
				Status: kubeovnv1.SubnetStatus{
					V4UsingIPs: internal.NewBigInt(2), V6UsingIPs: internal.NewBigInt(1),
					U2OInterconnectionIP: "10.0.0.1,fd00::1",
				},
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, readyToRemoveFinalizer(tc.subnet))
		})
	}
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

func Test_syncVirtualPort_noSubstringMatch(t *testing.T) {
	t.Parallel()

	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	fakeinformers := fakeController.fakeInformers
	mockOvnClient := fakeController.mockOvnClient

	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ovn-test",
		},
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock: "10.0.0.0/24",
			Vips:      []string{"10.0.0.1"},
		},
	}

	err := fakeinformers.subnetInformer.Informer().GetStore().Add(subnet)
	require.NoError(t, err)

	// LSP has vip "10.0.0.10" which contains "10.0.0.1" as a substring
	// but should NOT match the vip "10.0.0.1"
	lsps := []ovnnb.LogicalSwitchPort{
		{
			Name: "lsp-no-match",
			ExternalIDs: map[string]string{
				"ls":   "",
				"vips": "10.0.0.10,10.0.0.2",
			},
		},
		{
			Name: "lsp-match",
			ExternalIDs: map[string]string{
				"ls":   "",
				"vips": "10.0.0.1,10.0.0.3",
			},
		},
	}

	mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, gomock.Any()).Return(lsps, nil)
	// Only "lsp-match" should be a virtual parent, not "lsp-no-match"
	mockOvnClient.EXPECT().SetLogicalSwitchPortVirtualParents(subnet.Name, "lsp-match", "10.0.0.1").Return(nil)

	err = ctrl.syncVirtualPort(subnet.Name)
	require.NoError(t, err)
}

func Test_formatSubnet(t *testing.T) {
	t.Parallel()

	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController

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
					Provider:    util.OvnProvider,
					GatewayType: kubeovnv1.GWDistributedType,
					EnableLb:    new(ctrl.config.EnableLb),
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
					EnableLb:    new(false),
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
					EnableLb:    new(false),
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
					EnableLb:   new(false),
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
					EnableLb:   new(false),
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
					EnableLb:   new(false),
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
					EnableLb:   new(false),
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
			formattedSubnet.SetManagedFields(nil)
			require.Equal(t, tc.output, formattedSubnet)
			err = ctrl.config.KubeOvnClient.KubeovnV1().Subnets().Delete(context.Background(), tc.input.Name, metav1.DeleteOptions{})
			require.NoError(t, err)
		})
	}
}

func Test_handleAddOrUpdateSubnet_vlanValidationError(t *testing.T) {
	t.Parallel()

	// Create a subnet that references a non-existent vlan
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-underlay",
		},
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock: "10.0.0.0/24",
			Gateway:   "10.0.0.1",
			Vlan:      "non-existent-vlan",
		},
	}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet},
	})
	require.NoError(t, err)
	ctrl := fakeController.fakeController

	// handleAddOrUpdateSubnet should return an error when the vlan does not exist,
	// so that the work queue retries the item instead of forgetting it
	err = ctrl.handleAddOrUpdateSubnet("test-underlay")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to validate vlan")
}

func Test_isOvnSubnet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		subnet *kubeovnv1.Subnet
		want   bool
	}{
		{
			name:   "nil subnet returns false",
			subnet: nil,
			want:   false,
		},
		{
			name: "empty provider defaults to OVN",
			subnet: &kubeovnv1.Subnet{
				Spec: kubeovnv1.SubnetSpec{Provider: ""},
			},
			want: true,
		},
		{
			name: "explicit OVN provider",
			subnet: &kubeovnv1.Subnet{
				Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider},
			},
			want: true,
		},
		{
			name: "non-OVN provider",
			subnet: &kubeovnv1.Subnet{
				Spec: kubeovnv1.SubnetSpec{Provider: "external.provider"},
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isOvnSubnet(tc.subnet))
		})
	}
}

func Test_checkSubnetConflict(t *testing.T) {
	t.Parallel()

	existingSubnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "existing-subnet",
		},
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock: "10.0.0.0/24",
			Vpc:       util.DefaultVpc,
		},
	}

	t.Run("CIDR overlap should return error", func(t *testing.T) {
		newSubnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "new-subnet",
			},
			Spec: kubeovnv1.SubnetSpec{
				CIDRBlock: "10.0.0.0/16",
				Vpc:       util.DefaultVpc,
			},
		}

		fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{existingSubnet, newSubnet},
		})
		require.NoError(t, err)

		err = fakeCtrl.fakeController.checkSubnetConflict(newSubnet)
		require.Error(t, err, "checkSubnetConflict should return error for overlapping CIDRs")
		require.Contains(t, err.Error(), "conflict")
	})

	t.Run("PolicyRoutingTableID conflict should return error", func(t *testing.T) {
		existingWithEgress := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "existing-egress",
			},
			Spec: kubeovnv1.SubnetSpec{
				CIDRBlock:             "10.1.0.0/24",
				Vpc:                   util.DefaultVpc,
				ExternalEgressGateway: "1.2.3.4",
				PolicyRoutingTableID:  100,
			},
		}
		newWithEgress := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "new-egress",
			},
			Spec: kubeovnv1.SubnetSpec{
				CIDRBlock:             "10.2.0.0/24",
				Vpc:                   util.DefaultVpc,
				ExternalEgressGateway: "5.6.7.8",
				PolicyRoutingTableID:  100,
			},
		}

		fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{existingWithEgress, newWithEgress},
		})
		require.NoError(t, err)

		err = fakeCtrl.fakeController.checkSubnetConflict(newWithEgress)
		require.Error(t, err, "checkSubnetConflict should return error for conflicting PolicyRoutingTableID")
		require.Contains(t, err.Error(), "conflict")
	})

	t.Run("node address conflict should return error", func(t *testing.T) {
		nodeSubnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-conflict-subnet",
			},
			Spec: kubeovnv1.SubnetSpec{
				CIDRBlock: "192.168.1.0/24",
				Vpc:       util.DefaultVpc,
			},
		}

		fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{nodeSubnet},
			Nodes: []*corev1.Node{{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{{
						Type:    corev1.NodeInternalIP,
						Address: "192.168.1.10",
					}},
				},
			}},
		})
		require.NoError(t, err)

		err = fakeCtrl.fakeController.checkSubnetConflict(nodeSubnet)
		require.Error(t, err, "checkSubnetConflict should return error for node address conflict")
		require.Contains(t, err.Error(), "conflict")
	})

	t.Run("no conflict should return nil", func(t *testing.T) {
		noConflictSubnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "no-conflict",
			},
			Spec: kubeovnv1.SubnetSpec{
				CIDRBlock: "172.16.0.0/24",
				Vpc:       util.DefaultVpc,
			},
		}

		fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{existingSubnet, noConflictSubnet},
		})
		require.NoError(t, err)

		err = fakeCtrl.fakeController.checkSubnetConflict(noConflictSubnet)
		require.NoError(t, err)
	})
}

func Test_validateSubnetVlan(t *testing.T) {
	t.Parallel()

	pn := &kubeovnv1.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pn"},
		Status: kubeovnv1.ProviderNetworkStatus{
			Vlans: []string{"ready-vlan"},
		},
	}
	conflictVlan := &kubeovnv1.Vlan{
		ObjectMeta: metav1.ObjectMeta{Name: "conflict-vlan"},
		Spec:       kubeovnv1.VlanSpec{ID: 100, Provider: "test-pn"},
		Status:     kubeovnv1.VlanStatus{Conflict: true},
	}
	readyVlan := &kubeovnv1.Vlan{
		ObjectMeta: metav1.ObjectMeta{Name: "ready-vlan"},
		Spec:       kubeovnv1.VlanSpec{ID: 200, Provider: "test-pn"},
		Status:     kubeovnv1.VlanStatus{Conflict: false},
	}
	unprocessedVlan := &kubeovnv1.Vlan{
		ObjectMeta: metav1.ObjectMeta{Name: "unprocessed-vlan"},
		Spec:       kubeovnv1.VlanSpec{ID: 300, Provider: "test-pn"},
		Status:     kubeovnv1.VlanStatus{Conflict: false}, // same as ready, but NOT in pn.Status.Vlans
	}
	emptyProviderVlan := &kubeovnv1.Vlan{
		ObjectMeta: metav1.ObjectMeta{Name: "empty-provider-vlan"},
		Spec:       kubeovnv1.VlanSpec{ID: 400, Provider: ""}, // not yet defaulted by vlan handler
	}

	fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vlans:            []*kubeovnv1.Vlan{conflictVlan, readyVlan, unprocessedVlan, emptyProviderVlan},
		ProviderNetworks: []*kubeovnv1.ProviderNetwork{pn},
	})
	require.NoError(t, err)
	ctrl := fakeCtrl.fakeController

	t.Run("conflict vlan is rejected", func(t *testing.T) {
		subnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
			Spec:       kubeovnv1.SubnetSpec{Vlan: "conflict-vlan"},
		}
		err := ctrl.validateSubnetVlan(subnet)
		require.Error(t, err)
		require.Contains(t, err.Error(), "conflict")
	})

	t.Run("ready vlan passes validation", func(t *testing.T) {
		subnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
			Spec:       kubeovnv1.SubnetSpec{Vlan: "ready-vlan"},
		}
		err := ctrl.validateSubnetVlan(subnet)
		require.NoError(t, err)
	})

	t.Run("unprocessed vlan defers subnet processing", func(t *testing.T) {
		subnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
			Spec:       kubeovnv1.SubnetSpec{Vlan: "unprocessed-vlan"},
		}
		err := ctrl.validateSubnetVlan(subnet)
		require.Error(t, err)
		require.ErrorIs(t, err, errVlanNotReady)
	})

	t.Run("empty provider vlan is not ready", func(t *testing.T) {
		subnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
			Spec:       kubeovnv1.SubnetSpec{Vlan: "empty-provider-vlan"},
		}
		err := ctrl.validateSubnetVlan(subnet)
		require.Error(t, err)
		require.ErrorIs(t, err, errVlanNotReady)
	})

	t.Run("missing vlan returns error", func(t *testing.T) {
		subnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
			Spec:       kubeovnv1.SubnetSpec{Vlan: "nonexistent-vlan"},
		}
		err := ctrl.validateSubnetVlan(subnet)
		require.Error(t, err)
	})

	t.Run("empty vlan passes validation", func(t *testing.T) {
		subnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
			Spec:       kubeovnv1.SubnetSpec{Vlan: ""},
		}
		err := ctrl.validateSubnetVlan(subnet)
		require.NoError(t, err)
	})
}

func Test_handleMcastQuerierChange(t *testing.T) {
	t.Parallel()

	subnetName := "test-mcast-subnet"
	lspName := fmt.Sprintf(util.McastQuerierName, subnetName)
	querierIP := "10.16.0.100"
	querierMAC := "00:00:00:ab:cd:ef"

	t.Run("enable multicast snoop successfully", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController
		mockOvnClient := fakeController.mockOvnClient

		subnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: subnetName},
			Spec:       kubeovnv1.SubnetSpec{EnableMulticastSnoop: true},
			Status:     kubeovnv1.SubnetStatus{McastQuerierIP: querierIP, McastQuerierMAC: querierMAC},
		}

		mockOvnClient.EXPECT().CreateLogicalSwitchPort(subnetName, lspName, querierIP, querierMAC, lspName, metav1.NamespaceDefault, false, "", "", false, nil, "").Return(nil)
		mockOvnClient.EXPECT().LogicalSwitchUpdateOtherConfig(subnetName, ovsdb.MutateOperationInsert, gomock.Any()).Return(nil)

		err := ctrl.handleMcastQuerierChange(subnet)
		require.NoError(t, err)
	})

	t.Run("enable multicast snoop create lsp fails", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController
		mockOvnClient := fakeController.mockOvnClient

		subnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: subnetName},
			Spec:       kubeovnv1.SubnetSpec{EnableMulticastSnoop: true},
			Status:     kubeovnv1.SubnetStatus{McastQuerierIP: querierIP, McastQuerierMAC: querierMAC},
		}

		mockOvnClient.EXPECT().CreateLogicalSwitchPort(subnetName, lspName, querierIP, querierMAC, lspName, metav1.NamespaceDefault, false, "", "", false, nil, "").Return(errors.New("create lsp failed"))

		err := ctrl.handleMcastQuerierChange(subnet)
		require.Error(t, err)
		require.Contains(t, err.Error(), "create lsp failed")
	})

	t.Run("enable multicast snoop update other config fails", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController
		mockOvnClient := fakeController.mockOvnClient

		subnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: subnetName},
			Spec:       kubeovnv1.SubnetSpec{EnableMulticastSnoop: true},
			Status:     kubeovnv1.SubnetStatus{McastQuerierIP: querierIP, McastQuerierMAC: querierMAC},
		}

		mockOvnClient.EXPECT().CreateLogicalSwitchPort(subnetName, lspName, querierIP, querierMAC, lspName, metav1.NamespaceDefault, false, "", "", false, nil, "").Return(nil)
		mockOvnClient.EXPECT().LogicalSwitchUpdateOtherConfig(subnetName, ovsdb.MutateOperationInsert, gomock.Any()).Return(errors.New("update config failed"))

		err := ctrl.handleMcastQuerierChange(subnet)
		require.Error(t, err)
		require.Contains(t, err.Error(), "update config failed")
	})

	t.Run("disable multicast snoop successfully", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController
		mockOvnClient := fakeController.mockOvnClient

		subnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: subnetName},
			Spec:       kubeovnv1.SubnetSpec{EnableMulticastSnoop: false},
		}

		lss := []ovnnb.LogicalSwitch{{
			Name: subnetName,
			OtherConfig: map[string]string{
				"mcast_snoop":   "true",
				"mcast_querier": "true",
				"mcast_ip4_src": querierIP,
				"mcast_eth_src": querierMAC,
			},
		}}

		mockOvnClient.EXPECT().ListLogicalSwitch(false, gomock.Any()).Return(lss, nil)
		mockOvnClient.EXPECT().LogicalSwitchUpdateOtherConfig(subnetName, ovsdb.MutateOperationDelete, gomock.Any()).Return(nil)
		mockOvnClient.EXPECT().DeleteLogicalSwitchPort(lspName).Return(nil)

		err := ctrl.handleMcastQuerierChange(subnet)
		require.NoError(t, err)
	})

	t.Run("disable multicast snoop list logical switch fails", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController
		mockOvnClient := fakeController.mockOvnClient

		subnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: subnetName},
			Spec:       kubeovnv1.SubnetSpec{EnableMulticastSnoop: false},
		}

		mockOvnClient.EXPECT().ListLogicalSwitch(false, gomock.Any()).Return(nil, errors.New("list failed"))

		err := ctrl.handleMcastQuerierChange(subnet)
		require.Error(t, err)
		require.Contains(t, err.Error(), "list failed")
	})

	// KEY TEST: This is the bug scenario - logical switch not found should return error, not nil
	t.Run("disable multicast snoop logical switch not found returns error", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController
		mockOvnClient := fakeController.mockOvnClient

		subnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: subnetName},
			Spec:       kubeovnv1.SubnetSpec{EnableMulticastSnoop: false},
		}

		mockOvnClient.EXPECT().ListLogicalSwitch(false, gomock.Any()).Return([]ovnnb.LogicalSwitch{}, nil)

		err := ctrl.handleMcastQuerierChange(subnet)
		require.Error(t, err)
		require.Contains(t, err.Error(), subnetName)
	})

	t.Run("disable multicast snoop delete other config fails", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController
		mockOvnClient := fakeController.mockOvnClient

		subnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: subnetName},
			Spec:       kubeovnv1.SubnetSpec{EnableMulticastSnoop: false},
		}

		lss := []ovnnb.LogicalSwitch{{
			Name:        subnetName,
			OtherConfig: map[string]string{},
		}}

		mockOvnClient.EXPECT().ListLogicalSwitch(false, gomock.Any()).Return(lss, nil)
		mockOvnClient.EXPECT().LogicalSwitchUpdateOtherConfig(subnetName, ovsdb.MutateOperationDelete, gomock.Any()).Return(errors.New("delete config failed"))

		err := ctrl.handleMcastQuerierChange(subnet)
		require.Error(t, err)
		require.Contains(t, err.Error(), "delete config failed")
	})

	t.Run("disable multicast snoop delete lsp fails", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController
		mockOvnClient := fakeController.mockOvnClient

		subnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: subnetName},
			Spec:       kubeovnv1.SubnetSpec{EnableMulticastSnoop: false},
		}

		lss := []ovnnb.LogicalSwitch{{
			Name:        subnetName,
			OtherConfig: map[string]string{},
		}}

		mockOvnClient.EXPECT().ListLogicalSwitch(false, gomock.Any()).Return(lss, nil)
		mockOvnClient.EXPECT().LogicalSwitchUpdateOtherConfig(subnetName, ovsdb.MutateOperationDelete, gomock.Any()).Return(nil)
		mockOvnClient.EXPECT().DeleteLogicalSwitchPort(lspName).Return(errors.New("delete lsp failed"))

		err := ctrl.handleMcastQuerierChange(subnet)
		require.Error(t, err)
		require.Contains(t, err.Error(), "delete lsp failed")
	})
}
