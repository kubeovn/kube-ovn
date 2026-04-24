package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

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

func TestSanitizeNatGwScriptHostPath(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    string
		expectError bool
	}{
		{
			name:        "empty path",
			input:       "",
			expected:    "",
			expectError: false,
		},
		{
			name:        "space only path",
			input:       "   ",
			expected:    "",
			expectError: false,
		},
		{
			name:        "valid absolute path",
			input:       "/var/lib/kube-ovn/scripts",
			expected:    "/var/lib/kube-ovn/scripts",
			expectError: false,
		},
		{
			name:        "absolute path with trailing slash",
			input:       "/var/lib/kube-ovn/scripts/",
			expected:    "/var/lib/kube-ovn/scripts",
			expectError: false,
		},
		{
			name:        "relative path",
			input:       "var/lib/kube-ovn/scripts",
			expected:    "",
			expectError: true,
		},
		{
			name:        "path with whitespace",
			input:       "/var/lib/kube ovn/scripts",
			expected:    "",
			expectError: true,
		},
		{
			name:     "path traversal is cleaned by filepath.Clean",
			input:    "/var/../etc/shadow",
			expected: "/etc/shadow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := sanitizeNatGwScriptHostPath(tt.input)
			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestMountNatGwScriptHostPath(t *testing.T) {
	newStatefulSet := func(containerName string) *v1.StatefulSet {
		return &v1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-sts"},
			Spec: v1.StatefulSetSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: containerName}},
					},
				},
			},
		}
	}

	t.Run("skip when host path empty", func(t *testing.T) {
		sts := newStatefulSet(util.VpcNatGwContainerName)

		err := mountNatGwScriptHostPath(sts, "gw-test", "")
		require.NoError(t, err)
		assert.Empty(t, sts.Spec.Template.Spec.Volumes)
		assert.Empty(t, sts.Spec.Template.Spec.Containers[0].VolumeMounts)
	})

	t.Run("mount host path to nat gateway container", func(t *testing.T) {
		scriptHostPath := "/var/lib/kube-ovn/scripts"
		sts := newStatefulSet(util.VpcNatGwContainerName)

		err := mountNatGwScriptHostPath(sts, "gw-test", scriptHostPath)
		require.NoError(t, err)
		require.Len(t, sts.Spec.Template.Spec.Volumes, 1)
		assert.Equal(t, util.VpcNatGwScriptVolumeName, sts.Spec.Template.Spec.Volumes[0].Name)
		require.NotNil(t, sts.Spec.Template.Spec.Volumes[0].HostPath)
		assert.Equal(t, scriptHostPath, sts.Spec.Template.Spec.Volumes[0].HostPath.Path)
		require.NotNil(t, sts.Spec.Template.Spec.Volumes[0].HostPath.Type)
		assert.Equal(t, corev1.HostPathDirectory, *sts.Spec.Template.Spec.Volumes[0].HostPath.Type)

		require.Len(t, sts.Spec.Template.Spec.Containers[0].VolumeMounts, 1)
		assert.Equal(t, util.VpcNatGwScriptVolumeName, sts.Spec.Template.Spec.Containers[0].VolumeMounts[0].Name)
		assert.Equal(t, util.VpcNatGwScriptMountPath, sts.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath)
		assert.True(t, sts.Spec.Template.Spec.Containers[0].VolumeMounts[0].ReadOnly)
	})

	t.Run("return error when nat gateway container missing", func(t *testing.T) {
		sts := newStatefulSet("other-container")

		err := mountNatGwScriptHostPath(sts, "gw-test", "/var/lib/kube-ovn/scripts")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "container vpc-nat-gw not found")
	})
}

func TestShouldSyncVpcNatGwScriptHostPath(t *testing.T) {
	tests := []struct {
		name           string
		currentConfig  vpcNatRuntimeConfig
		scriptHostPath string
		expected       bool
	}{
		{
			name:           "startup with empty path does not sync",
			currentConfig:  vpcNatRuntimeConfig{},
			scriptHostPath: "",
			expected:       false,
		},
		{
			name:           "startup with non-empty path syncs",
			currentConfig:  vpcNatRuntimeConfig{},
			scriptHostPath: "/var/lib/kube-ovn/scripts",
			expected:       true,
		},
		{
			name: "same non-empty path already synced does not sync",
			currentConfig: vpcNatRuntimeConfig{
				scriptHostPath:   "/var/lib/kube-ovn/scripts",
				scriptPathSynced: true,
			},
			scriptHostPath: "/var/lib/kube-ovn/scripts",
			expected:       false,
		},
		{
			name: "same non-empty path not yet synced retries",
			currentConfig: vpcNatRuntimeConfig{
				scriptHostPath:   "/var/lib/kube-ovn/scripts",
				scriptPathSynced: false,
			},
			scriptHostPath: "/var/lib/kube-ovn/scripts",
			expected:       true,
		},
		{
			name: "path change to empty syncs to remove mount",
			currentConfig: vpcNatRuntimeConfig{
				scriptHostPath:   "/var/lib/kube-ovn/scripts",
				scriptPathSynced: true,
			},
			scriptHostPath: "",
			expected:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, shouldSyncVpcNatGwScriptHostPath(tt.currentConfig, tt.scriptHostPath))
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
