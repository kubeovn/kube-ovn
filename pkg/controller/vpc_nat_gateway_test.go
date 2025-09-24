package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
