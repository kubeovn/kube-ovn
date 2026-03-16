package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestVpcNatGwScriptConstants(t *testing.T) {
	// Verify the constants are set correctly for backward compatibility
	assert.Equal(t, "/kube-ovn", vpcNatGwScriptMountPath, "Script mount path should be /kube-ovn")
	assert.Equal(t, "nat-gw-script", vpcNatGwScriptVolumeName, "Volume name should be nat-gw-script")
	assert.Equal(t, "nat-gateway.sh", vpcNatGwScriptName, "Script name should be nat-gateway.sh")
	assert.Equal(t, "/kube-ovn/nat-gateway.sh", vpcNatGwScriptPath, "Script path should be /kube-ovn/nat-gateway.sh")
	assert.Equal(t, "vpc-nat-gw", vpcNatGwContainerName, "Container name should be vpc-nat-gw")
	assert.Equal(t, "vpc-nat-gw", vpcNatGwServiceAccountName, "ServiceAccount name should be vpc-nat-gw")
}

func TestIsVpcNatGwChanged(t *testing.T) {
	tests := []struct {
		name     string
		gw       *kubeovnv1.VpcNatGateway
		expected bool
	}{
		{
			name: "no changes returns false",
			gw: &kubeovnv1.VpcNatGateway{
				Spec: kubeovnv1.VpcNatGatewaySpec{
					ExternalSubnets: []string{"subnet1"},
					Selector:        []string{"node=worker1"},
				},
				Status: kubeovnv1.VpcNatGatewayStatus{
					ExternalSubnets: []string{"subnet1"},
					Selector:        []string{"node=worker1"},
				},
			},
			expected: false,
		},
		{
			name: "ExternalSubnets changed returns true",
			gw: &kubeovnv1.VpcNatGateway{
				Spec: kubeovnv1.VpcNatGatewaySpec{
					ExternalSubnets: []string{"subnet2"},
					Selector:        []string{"node=worker1"},
				},
				Status: kubeovnv1.VpcNatGatewayStatus{
					ExternalSubnets: []string{"subnet1"},
					Selector:        []string{"node=worker1"},
				},
			},
			expected: true,
		},
		{
			name: "Selector changed returns true",
			gw: &kubeovnv1.VpcNatGateway{
				Spec: kubeovnv1.VpcNatGatewaySpec{
					ExternalSubnets: []string{"subnet1"},
					Selector:        []string{"node=worker2"},
				},
				Status: kubeovnv1.VpcNatGatewayStatus{
					ExternalSubnets: []string{"subnet1"},
					Selector:        []string{"node=worker1"},
				},
			},
			expected: true,
		},
		{
			name: "Tolerations changed returns true",
			gw: &kubeovnv1.VpcNatGateway{
				Spec: kubeovnv1.VpcNatGatewaySpec{
					ExternalSubnets: []string{"subnet1"},
					Selector:        []string{"node=worker1"},
					Tolerations:     []corev1.Toleration{{Key: "new-key"}},
				},
				Status: kubeovnv1.VpcNatGatewayStatus{
					ExternalSubnets: []string{"subnet1"},
					Selector:        []string{"node=worker1"},
				},
			},
			expected: true,
		},
		{
			name: "Affinity changed returns true",
			gw: &kubeovnv1.VpcNatGateway{
				Spec: kubeovnv1.VpcNatGatewaySpec{
					ExternalSubnets: []string{"subnet1"},
					Selector:        []string{"node=worker1"},
					Affinity: corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{},
					},
				},
				Status: kubeovnv1.VpcNatGatewayStatus{
					ExternalSubnets: []string{"subnet1"},
					Selector:        []string{"node=worker1"},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isVpcNatGwChanged(tt.gw)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetSubnetProvider(t *testing.T) {
	tests := []struct {
		name             string
		subnetName       string
		subnets          []*kubeovnv1.Subnet
		expectedProvider string
		expectError      bool
		description      string
	}{
		{
			name:       "Valid OVN subnets with different providers",
			subnetName: "ovn-default",
			subnets: []*kubeovnv1.Subnet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ovn-default",
					},
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "10.244.0.0/24",
						Provider:  util.OvnProvider,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "net1-subnet",
					},
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "192.168.1.0/24",
						Provider:  "net1.default.ovn",
					},
				},
			},
			expectedProvider: util.OvnProvider,
			expectError:      false,
			description:      "Should return correct provider for valid OVN subnet among multiple subnets",
		},
		{
			name:       "Non-existent subnet",
			subnetName: "non-existent",
			subnets: []*kubeovnv1.Subnet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ovn-default",
					},
					Spec: kubeovnv1.SubnetSpec{
						CIDRBlock: "10.244.0.0/24",
						Provider:  util.OvnProvider,
					},
				},
			},
			expectedProvider: "",
			expectError:      true,
			description:      "Should return error for non-existent subnet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create controller with subnets
			fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Subnets: tt.subnets,
			})
			require.NoError(t, err, "Failed to create fake controller")
			controller := fakeController.fakeController
			// Call the method under test
			provider, err := controller.GetSubnetProvider(tt.subnetName)

			// Check for errors
			if tt.expectError {
				assert.Error(t, err, "Expected an error but got none: %s", tt.description)
				return
			}
			require.NoError(t, err, "Unexpected error: %s", tt.description)

			// Verify provider
			assert.Equal(t, tt.expectedProvider, provider, "Provider mismatch: %s", tt.description)
		})
	}

	// Test multiple provider scenarios in a single comprehensive test
	t.Run("Multiple provider scenarios", func(t *testing.T) {
		subnets := []*kubeovnv1.Subnet{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "default-subnet"},
				Spec:       kubeovnv1.SubnetSpec{Provider: util.OvnProvider},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-subnet"},
				Spec:       kubeovnv1.SubnetSpec{Provider: "custom.provider.ovn"},
			},
		}

		fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: subnets,
		})
		require.NoError(t, err)
		controller := fakeController.fakeController
		// Test default provider
		provider, err := controller.GetSubnetProvider("default-subnet")
		require.NoError(t, err)
		assert.Equal(t, util.OvnProvider, provider)

		// Test custom provider
		provider, err = controller.GetSubnetProvider("custom-subnet")
		require.NoError(t, err)
		assert.Equal(t, "custom.provider.ovn", provider)

		// Test non-existent subnet
		_, err = controller.GetSubnetProvider("missing-subnet")
		assert.Error(t, err, "Should error for missing subnet")
	})
}

func TestGetExternalSubnetNad(t *testing.T) {
	tests := []struct {
		name              string
		gw                *kubeovnv1.VpcNatGateway
		subnets           []*kubeovnv1.Subnet
		podNamespace      string
		expectedNamespace string
		expectedName      string
		expectError       bool
	}{
		{
			name: "provider with 3 parts (name.namespace.ovn)",
			gw: &kubeovnv1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
				Spec:       kubeovnv1.VpcNatGatewaySpec{ExternalSubnets: []string{"external-subnet"}},
			},
			subnets: []*kubeovnv1.Subnet{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "external-subnet"},
					Spec:       kubeovnv1.SubnetSpec{Provider: "real-eip.kube-system.ovn"},
				},
			},
			podNamespace:      "kube-system",
			expectedNamespace: "kube-system",
			expectedName:      "real-eip",
			expectError:       false,
		},
		{
			name: "provider with 2 parts (name.namespace)",
			gw: &kubeovnv1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
				Spec:       kubeovnv1.VpcNatGatewaySpec{ExternalSubnets: []string{"external-subnet"}},
			},
			subnets: []*kubeovnv1.Subnet{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "external-subnet"},
					Spec:       kubeovnv1.SubnetSpec{Provider: "my-nad.default"},
				},
			},
			podNamespace:      "kube-system",
			expectedNamespace: "default",
			expectedName:      "my-nad",
			expectError:       false,
		},
		{
			name: "provider is ovn (fallback to subnet name)",
			gw: &kubeovnv1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
				Spec:       kubeovnv1.VpcNatGatewaySpec{ExternalSubnets: []string{"ovn-vpc-external-network"}},
			},
			subnets: []*kubeovnv1.Subnet{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "ovn-vpc-external-network"},
					Spec:       kubeovnv1.SubnetSpec{Provider: util.OvnProvider},
				},
			},
			podNamespace:      "kube-system",
			expectedNamespace: "kube-system",
			expectedName:      "ovn-vpc-external-network",
			expectError:       false,
		},
		{
			name: "empty provider (fallback to subnet name)",
			gw: &kubeovnv1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
				Spec:       kubeovnv1.VpcNatGatewaySpec{ExternalSubnets: []string{"my-external-subnet"}},
			},
			subnets: []*kubeovnv1.Subnet{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "my-external-subnet"},
					Spec:       kubeovnv1.SubnetSpec{Provider: ""},
				},
			},
			podNamespace:      "kube-system",
			expectedNamespace: "kube-system",
			expectedName:      "my-external-subnet",
			expectError:       false,
		},
		{
			name: "empty ExternalSubnets (use default)",
			gw: &kubeovnv1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
				Spec:       kubeovnv1.VpcNatGatewaySpec{ExternalSubnets: []string{}},
			},
			subnets: []*kubeovnv1.Subnet{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "ovn-vpc-external-network"},
					Spec:       kubeovnv1.SubnetSpec{Provider: "external.default.ovn"},
				},
			},
			podNamespace:      "kube-system",
			expectedNamespace: "default",
			expectedName:      "external",
			expectError:       false,
		},
		{
			name: "subnet not found",
			gw: &kubeovnv1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
				Spec:       kubeovnv1.VpcNatGatewaySpec{ExternalSubnets: []string{"non-existent-subnet"}},
			},
			subnets:      []*kubeovnv1.Subnet{},
			podNamespace: "kube-system",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Subnets: tt.subnets,
			})
			require.NoError(t, err)
			controller := fakeController.fakeController
			controller.config.PodNamespace = tt.podNamespace

			namespace, name, err := controller.getExternalSubnetNad(tt.gw)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedNamespace, namespace, "namespace mismatch")
			assert.Equal(t, tt.expectedName, name, "name mismatch")
		})
	}
}
