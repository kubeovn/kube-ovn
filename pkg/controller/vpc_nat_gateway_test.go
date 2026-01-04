package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestResolveNatGwRoutes(t *testing.T) {
	tests := []struct {
		name        string
		gw          *kubeovnv1.VpcNatGateway
		subnet      *kubeovnv1.Subnet
		wantRoutes  map[string]string
		wantErr     bool
		description string
	}{
		{
			name: "resolve gateway keyword to v4",
			gw: &kubeovnv1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
				Spec: kubeovnv1.VpcNatGatewaySpec{
					Subnet: "test-subnet",
					Routes: []kubeovnv1.Route{
						{CIDR: "10.0.0.0/8", NextHopIP: "gateway"},
					},
				},
			},
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
				Spec:       kubeovnv1.SubnetSpec{Gateway: "192.168.1.1"},
			},
			wantRoutes:  map[string]string{"10.0.0.0/8": "192.168.1.1"},
			wantErr:     false,
			description: "Should resolve 'gateway' to v4 gateway IP for v4 CIDR",
		},
		{
			name: "resolve gateway keyword to v6",
			gw: &kubeovnv1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
				Spec: kubeovnv1.VpcNatGatewaySpec{
					Subnet: "test-subnet",
					Routes: []kubeovnv1.Route{
						{CIDR: "fd00::/8", NextHopIP: "gateway"},
					},
				},
			},
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
				Spec:       kubeovnv1.SubnetSpec{Gateway: "192.168.1.1,fd00::1"},
			},
			wantRoutes:  map[string]string{"fd00::/8": "fd00::1"},
			wantErr:     false,
			description: "Should resolve 'gateway' to v6 gateway IP for v6 CIDR",
		},
		{
			name: "explicit nexthop IP",
			gw: &kubeovnv1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
				Spec: kubeovnv1.VpcNatGatewaySpec{
					Subnet: "test-subnet",
					Routes: []kubeovnv1.Route{
						{CIDR: "10.0.0.0/8", NextHopIP: "172.16.0.1"},
					},
				},
			},
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
				Spec:       kubeovnv1.SubnetSpec{Gateway: "192.168.1.1"},
			},
			wantRoutes:  map[string]string{"10.0.0.0/8": "172.16.0.1"},
			wantErr:     false,
			description: "Should use explicit nexthop IP as-is",
		},
		{
			name: "skip empty nexthop",
			gw: &kubeovnv1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
				Spec: kubeovnv1.VpcNatGatewaySpec{
					Subnet: "test-subnet",
					Routes: []kubeovnv1.Route{
						{CIDR: "10.0.0.0/8", NextHopIP: ""},
						{CIDR: "172.16.0.0/12", NextHopIP: "192.168.1.1"},
					},
				},
			},
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
				Spec:       kubeovnv1.SubnetSpec{Gateway: "192.168.1.1"},
			},
			wantRoutes:  map[string]string{"172.16.0.0/12": "192.168.1.1"},
			wantErr:     false,
			description: "Should skip routes with empty nextHopIP",
		},
		{
			name: "subnet not found",
			gw: &kubeovnv1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
				Spec: kubeovnv1.VpcNatGatewaySpec{
					Subnet: "non-existent",
					Routes: []kubeovnv1.Route{
						{CIDR: "10.0.0.0/8", NextHopIP: "gateway"},
					},
				},
			},
			subnet:      nil,
			wantRoutes:  nil,
			wantErr:     true,
			description: "Should return error when subnet not found",
		},
		{
			name: "empty routes",
			gw: &kubeovnv1.VpcNatGateway{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gw"},
				Spec: kubeovnv1.VpcNatGatewaySpec{
					Subnet: "test-subnet",
					Routes: []kubeovnv1.Route{},
				},
			},
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
				Spec:       kubeovnv1.SubnetSpec{Gateway: "192.168.1.1"},
			},
			wantRoutes:  map[string]string{},
			wantErr:     false,
			description: "Should return empty map for empty routes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var subnets []*kubeovnv1.Subnet
			if tt.subnet != nil {
				subnets = []*kubeovnv1.Subnet{tt.subnet}
			}

			fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Subnets: subnets,
			})
			require.NoError(t, err)
			controller := fakeController.fakeController

			gotRoutes, err := controller.resolveNatGwRoutes(tt.gw)

			if tt.wantErr {
				assert.Error(t, err, tt.description)
				return
			}
			require.NoError(t, err, tt.description)
			assert.Equal(t, tt.wantRoutes, gotRoutes, tt.description)
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

func TestDiffNatGwRoutes(t *testing.T) {
	tests := []struct {
		name           string
		desiredRoutes  map[string]string
		currentRoutes  map[string]string
		wantDelCount   int
		wantAddCount   int
		wantRouteCount int
	}{
		{
			name:           "empty to empty",
			desiredRoutes:  map[string]string{},
			currentRoutes:  map[string]string{},
			wantDelCount:   0,
			wantAddCount:   0,
			wantRouteCount: 0,
		},
		{
			name: "add new routes",
			desiredRoutes: map[string]string{
				"10.0.0.0/8":    "192.168.1.1",
				"172.16.0.0/12": "192.168.1.1",
			},
			currentRoutes:  map[string]string{},
			wantDelCount:   0,
			wantAddCount:   2,
			wantRouteCount: 2,
		},
		{
			name:          "delete all routes",
			desiredRoutes: map[string]string{},
			currentRoutes: map[string]string{
				"10.0.0.0/8": "192.168.1.1",
			},
			wantDelCount:   1,
			wantAddCount:   0,
			wantRouteCount: 0,
		},
		{
			name: "update route nexthop",
			desiredRoutes: map[string]string{
				"10.0.0.0/8": "192.168.1.2", // changed nexthop
			},
			currentRoutes: map[string]string{
				"10.0.0.0/8": "192.168.1.1",
			},
			wantDelCount:   1, // delete old route with old nexthop
			wantAddCount:   1, // add with new nexthop
			wantRouteCount: 1,
		},
		{
			name: "mixed add and delete",
			desiredRoutes: map[string]string{
				"10.0.0.0/8":     "192.168.1.1", // keep
				"192.168.0.0/16": "192.168.1.1", // add
			},
			currentRoutes: map[string]string{
				"10.0.0.0/8":    "192.168.1.1", // keep
				"172.16.0.0/12": "192.168.1.1", // delete
			},
			wantDelCount:   1,
			wantAddCount:   1,
			wantRouteCount: 2,
		},
		{
			name: "no changes needed",
			desiredRoutes: map[string]string{
				"10.0.0.0/8": "192.168.1.1",
			},
			currentRoutes: map[string]string{
				"10.0.0.0/8": "192.168.1.1",
			},
			wantDelCount:   0,
			wantAddCount:   0,
			wantRouteCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routesToDel, routesToAdd, resolvedRoutes := diffNatGwRoutes(tt.desiredRoutes, tt.currentRoutes)

			assert.Len(t, routesToDel, tt.wantDelCount, "delete count mismatch")
			assert.Len(t, routesToAdd, tt.wantAddCount, "add count mismatch")
			assert.Len(t, resolvedRoutes, tt.wantRouteCount, "resolved routes count mismatch")

			// Verify format of routesToAdd/routesToDel: "cidr,nextHopIP"
			for _, r := range routesToAdd {
				assert.Contains(t, r, ",", "route string should contain comma separator")
			}
			for _, r := range routesToDel {
				assert.Contains(t, r, ",", "route string should contain comma separator")
			}
		})
	}
}

func TestDiffNatGwRoutesContent(t *testing.T) {
	tests := []struct {
		name             string
		desiredRoutes    map[string]string
		currentRoutes    map[string]string
		expectedDel      []string
		expectedAdd      []string
		expectedResolved []kubeovnv1.Route
		description      string
	}{
		{
			name: "add single route",
			desiredRoutes: map[string]string{
				"10.0.0.0/8": "192.168.1.1",
			},
			currentRoutes: map[string]string{},
			expectedDel:   []string{},
			expectedAdd:   []string{"10.0.0.0/8,192.168.1.1"},
			expectedResolved: []kubeovnv1.Route{
				{CIDR: "10.0.0.0/8", NextHopIP: "192.168.1.1"},
			},
			description: "Should correctly format route to add",
		},
		{
			name:          "delete single route",
			desiredRoutes: map[string]string{},
			currentRoutes: map[string]string{
				"10.0.0.0/8": "192.168.1.1",
			},
			expectedDel:      []string{"10.0.0.0/8,192.168.1.1"},
			expectedAdd:      []string{},
			expectedResolved: []kubeovnv1.Route{},
			description:      "Should correctly format route to delete",
		},
		{
			name: "update nexthop - route should be in add list",
			desiredRoutes: map[string]string{
				"10.0.0.0/8": "192.168.1.2",
			},
			currentRoutes: map[string]string{
				"10.0.0.0/8": "192.168.1.1",
			},
			expectedDel: []string{"10.0.0.0/8,192.168.1.1"},
			expectedAdd: []string{"10.0.0.0/8,192.168.1.2"},
			expectedResolved: []kubeovnv1.Route{
				{CIDR: "10.0.0.0/8", NextHopIP: "192.168.1.2"},
			},
			description: "Changed nexthop should trigger delete and add",
		},
		{
			name: "mixed operations with content verification",
			desiredRoutes: map[string]string{
				"10.0.0.0/8":     "192.168.1.1", // keep unchanged
				"192.168.0.0/16": "172.16.0.1",  // add new
			},
			currentRoutes: map[string]string{
				"10.0.0.0/8":    "192.168.1.1", // keep unchanged
				"172.16.0.0/12": "192.168.1.1", // delete this
			},
			expectedDel: []string{"172.16.0.0/12,192.168.1.1"},
			expectedAdd: []string{"192.168.0.0/16,172.16.0.1"},
			expectedResolved: []kubeovnv1.Route{
				{CIDR: "10.0.0.0/8", NextHopIP: "192.168.1.1"},
				{CIDR: "192.168.0.0/16", NextHopIP: "172.16.0.1"},
			},
			description: "Mixed add/delete should have correct content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routesToDel, routesToAdd, resolvedRoutes := diffNatGwRoutes(tt.desiredRoutes, tt.currentRoutes)

			// Verify delete content
			assert.ElementsMatch(t, tt.expectedDel, routesToDel, "delete routes content mismatch: %s", tt.description)

			// Verify add content
			assert.ElementsMatch(t, tt.expectedAdd, routesToAdd, "add routes content mismatch: %s", tt.description)

			// Verify resolved routes content
			assert.ElementsMatch(t, tt.expectedResolved, resolvedRoutes, "resolved routes content mismatch: %s", tt.description)
		})
	}
}
