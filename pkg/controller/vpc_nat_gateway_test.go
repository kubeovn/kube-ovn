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

func TestIsVpcNatGwChanged_Annotations(t *testing.T) {
	tests := []struct {
		name              string
		specAnnotations   map[string]string
		statusAnnotations map[string]string
		wantChanged       bool
	}{
		{
			name:              "no change - both empty",
			specAnnotations:   map[string]string{},
			statusAnnotations: map[string]string{},
			wantChanged:       false,
		},
		{
			name:              "no change - both nil",
			specAnnotations:   nil,
			statusAnnotations: nil,
			wantChanged:       false,
		},
		{
			name: "no change - same annotations",
			specAnnotations: map[string]string{
				"foo": "bar",
				"key": "value",
			},
			statusAnnotations: map[string]string{
				"foo": "bar",
				"key": "value",
			},
			wantChanged: false,
		},
		{
			name: "changed - annotation added",
			specAnnotations: map[string]string{
				"foo": "bar",
				"new": "annotation",
			},
			statusAnnotations: map[string]string{
				"foo": "bar",
			},
			wantChanged: true,
		},
		{
			name: "changed - annotation removed",
			specAnnotations: map[string]string{
				"foo": "bar",
			},
			statusAnnotations: map[string]string{
				"foo": "bar",
				"old": "annotation",
			},
			wantChanged: true,
		},
		{
			name: "changed - annotation modified",
			specAnnotations: map[string]string{
				"foo": "bar",
				"key": "newvalue",
			},
			statusAnnotations: map[string]string{
				"foo": "bar",
				"key": "oldvalue",
			},
			wantChanged: true,
		},
		{
			name:              "no change - nil vs empty map",
			specAnnotations:   map[string]string{},
			statusAnnotations: nil,
			wantChanged:       false,
		},
		{
			name: "changed - spec has annotations, status is nil",
			specAnnotations: map[string]string{
				"foo": "bar",
			},
			statusAnnotations: nil,
			wantChanged:       true,
		},
		{
			name:            "changed - spec is nil, status has annotations",
			specAnnotations: nil,
			statusAnnotations: map[string]string{
				"foo": "bar",
			},
			wantChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := &kubeovnv1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gw",
				},
				Spec: kubeovnv1.VpcNatGatewaySpec{
					Vpc:         "test-vpc",
					Subnet:      "test-subnet",
					Annotations: tt.specAnnotations,
				},
				Status: kubeovnv1.VpcNatGatewayStatus{
					Annotations: tt.statusAnnotations,
				},
			}

			got := isVpcNatGwChanged(gw)
			assert.Equal(t, tt.wantChanged, got, "isVpcNatGwChanged() annotations check failed")
		})
	}
}

// TestGenNatGwStatefulSet_UserAnnotations tests that user-defined annotations
// from VpcNatGateway CRD are correctly injected into the StatefulSet pod template
func TestGenNatGwStatefulSet_UserAnnotations(t *testing.T) {
	tests := []struct {
		name                   string
		gwAnnotations          map[string]string
		expectedPodAnnotations map[string]string
	}{
		{
			name: "basic user annotations",
			gwAnnotations: map[string]string{
				"foo.bar/annotation1":     "value1",
				"example.com/annotation2": "value2",
			},
			expectedPodAnnotations: map[string]string{
				"foo.bar/annotation1":     "value1",
				"example.com/annotation2": "value2",
			},
		},
		{
			name: "annotations with colons in values",
			gwAnnotations: map[string]string{
				"monitoring.example.com/url": "https://monitor.example.com:8080",
			},
			expectedPodAnnotations: map[string]string{
				"monitoring.example.com/url": "https://monitor.example.com:8080",
			},
		},
		{
			name:                   "empty annotations",
			gwAnnotations:          map[string]string{},
			expectedPodAnnotations: map[string]string{},
		},
		{
			name: "conflict with system annotation",
			gwAnnotations: map[string]string{
				"ovn.kubernetes.io/vpc_nat_gw": "fake-gw-name", // System annotation
				util.LogicalSwitchAnnotation:   "fake-subnet",  // Another System annotation
				"custom/annotation":            "value",
			},
			expectedPodAnnotations: map[string]string{
				"ovn.kubernetes.io/vpc_nat_gw": "test-gw",     // Should keep system value
				util.LogicalSwitchAnnotation:   "test-subnet", // Should keep system value
				"custom/annotation":            "value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create VPC NAT Gateway with annotations
			gw := &kubeovnv1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gw",
				},
				Spec: kubeovnv1.VpcNatGatewaySpec{
					Vpc:             "test-vpc",
					Subnet:          "test-subnet",
					LanIP:           "10.0.1.254",
					ExternalSubnets: []string{"external-subnet"},
					Annotations:     tt.gwAnnotations,
				},
			}

			// Create fake controller
			fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Subnets: []*kubeovnv1.Subnet{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
						Spec:       kubeovnv1.SubnetSpec{CIDRBlock: "10.0.1.0/24", Provider: util.OvnProvider},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "external-subnet"},
						Spec:       kubeovnv1.SubnetSpec{CIDRBlock: "192.168.1.0/24", Provider: "external.default.ovn"},
					},
				},
			})
			require.NoError(t, err)

			// Generate StatefulSet
			sts, err := fakeController.fakeController.genNatGwStatefulSet(gw, nil, 0)
			require.NoError(t, err)
			require.NotNil(t, sts)

			// Verify our injected annotations are appended to pod template
			podAnnotations := sts.Spec.Template.Annotations
			for key, expectedValue := range tt.expectedPodAnnotations {
				actualValue, exists := podAnnotations[key]
				assert.True(t, exists, "User annotation %s should exist", key)
				assert.Equal(t, expectedValue, actualValue, "User annotation %s value mismatch", key)
			}
		})
	}
}

func TestGenNatGwStatefulSet_AnnotationCleanup(t *testing.T) {
	gw := &kubeovnv1.VpcNatGateway{
		ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
		Spec: kubeovnv1.VpcNatGatewaySpec{
			Vpc:             "test-vpc",
			Subnet:          "test-subnet",
			LanIP:           "10.0.1.254",
			ExternalSubnets: []string{"external-subnet"},
			// No user annotations
		},
	}

	oldSts := &v1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-gw",
		},
		Spec: v1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"deleted-annotation":                         "should-be-gone",
						util.VpcNatGatewayContainerRestartAnnotation: "{\"container\": 1}",
					},
				},
			},
		},
	}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
				Spec:       kubeovnv1.SubnetSpec{CIDRBlock: "10.0.1.0/24", Provider: util.OvnProvider},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "external-subnet"},
				Spec:       kubeovnv1.SubnetSpec{CIDRBlock: "192.168.1.0/24", Provider: "external.default.ovn"},
			},
		},
	})
	require.NoError(t, err)

	// Generate StatefulSet with oldSts
	sts, err := fakeController.fakeController.genNatGwStatefulSet(gw, oldSts, 0)
	require.NoError(t, err)

	// Verify annotations
	// 1. "deleted-annotation" from oldSts.Spec.Template.Annotations should be REMOVED
	_, exists := sts.Spec.Template.Annotations["deleted-annotation"]
	assert.False(t, exists, "Deleted user annotation persisted in Pod Template")

	// 2. Restart annotation should be PRESERVED
	val, exists := sts.Spec.Template.Annotations[util.VpcNatGatewayContainerRestartAnnotation]
	assert.True(t, exists, "Restart annotation was lost")
	assert.Equal(t, "{\"container\": 1}", val)
}
