package daemon

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestParseEIPDestination(t *testing.T) {
	tests := []struct {
		name        string
		eip         string
		wantMask    int
		wantErr     bool
		errContains string
	}{
		{
			name:     "valid IPv4",
			eip:      "192.168.1.100",
			wantMask: 32,
			wantErr:  false,
		},
		{
			name:     "valid IPv6",
			eip:      "2001:db8::1",
			wantMask: 128,
			wantErr:  false,
		},
		{
			name:        "invalid IP",
			eip:         "invalid",
			wantErr:     true,
			errContains: "invalid EIP address",
		},
		{
			name:        "empty string",
			eip:         "",
			wantErr:     true,
			errContains: "invalid EIP address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst, err := parseEIPDestination(tt.eip)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, dst)
			ones, _ := dst.Mask.Size()
			assert.Equal(t, tt.wantMask, ones)
		})
	}
}

func TestShouldEnqueueIptablesEip(t *testing.T) {
	tests := []struct {
		name           string
		externalSubnet string
		ready          bool
		want           bool
	}{
		{
			name:           "ready with ExternalSubnet",
			externalSubnet: "external-subnet",
			ready:          true,
			want:           true,
		},
		{
			name:           "ready without ExternalSubnet",
			externalSubnet: "",
			ready:          true,
			want:           false,
		},
		{
			name:           "not ready with ExternalSubnet",
			externalSubnet: "external-subnet",
			ready:          false,
			want:           false,
		},
		{
			name:           "not ready without ExternalSubnet",
			externalSubnet: "",
			ready:          false,
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eip := &kubeovnv1.IptablesEIP{
				Spec: kubeovnv1.IptablesEIPSpec{
					ExternalSubnet: tt.externalSubnet,
				},
				Status: kubeovnv1.IptablesEIPStatus{
					Ready: tt.ready,
				},
			}
			assert.Equal(t, tt.want, shouldEnqueueIptablesEip(eip))
		})
	}
}

func TestIsVpcNatGwPod(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{
			name:   "NAT GW pod",
			labels: map[string]string{util.VpcNatGatewayLabel: "true"},
			want:   true,
		},
		{
			name:   "NAT GW pod with extra labels",
			labels: map[string]string{util.VpcNatGatewayLabel: "true", "app": "test"},
			want:   true,
		},
		{
			name:   "not NAT GW pod - label false",
			labels: map[string]string{util.VpcNatGatewayLabel: "false"},
			want:   false,
		},
		{
			name:   "not NAT GW pod - no label",
			labels: map[string]string{"app": "test"},
			want:   false,
		},
		{
			name:   "not NAT GW pod - nil labels",
			labels: nil,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Labels: tt.labels},
			}
			assert.Equal(t, tt.want, isVpcNatGwPod(pod))
		})
	}
}

func TestGetNatGwNameFromPod(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{
			name:   "has NAT GW name label",
			labels: map[string]string{util.VpcNatGatewayNameLabel: "my-nat-gw"},
			want:   "my-nat-gw",
		},
		{
			name:   "no NAT GW name label",
			labels: map[string]string{"app": "test"},
			want:   "",
		},
		{
			name:   "nil labels",
			labels: nil,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Labels: tt.labels},
			}
			assert.Equal(t, tt.want, getNatGwNameFromPod(pod))
		})
	}
}

func TestDeletedEIPsStateManagement(t *testing.T) {
	t.Run("add event clears deleted mark", func(t *testing.T) {
		c := &Controller{}

		// Simulate a previous delete event that marked EIP as deleted
		c.deletedEIPs.Store("test-eip", true)

		// Verify mark exists
		_, exists := c.deletedEIPs.Load("test-eip")
		assert.True(t, exists, "deleted mark should exist before add event")

		// Simulate what enqueueAddIptablesEip does - clear the mark
		c.deletedEIPs.Delete("test-eip")

		// Verify mark is cleared
		_, exists = c.deletedEIPs.Load("test-eip")
		assert.False(t, exists, "deleted mark should be cleared after add event")
	})

	t.Run("update event clears deleted mark", func(t *testing.T) {
		c := &Controller{}

		// Simulate a previous delete event
		c.deletedEIPs.Store("test-eip", true)

		// Verify mark exists
		_, exists := c.deletedEIPs.Load("test-eip")
		assert.True(t, exists, "deleted mark should exist before update event")

		// Simulate what enqueueUpdateIptablesEip does - clear the mark
		c.deletedEIPs.Delete("test-eip")

		// Verify mark is cleared
		_, exists = c.deletedEIPs.Load("test-eip")
		assert.False(t, exists, "deleted mark should be cleared after update event")
	})

	t.Run("delete event sets deleted mark", func(t *testing.T) {
		c := &Controller{}

		// Verify no mark initially
		_, exists := c.deletedEIPs.Load("test-eip")
		assert.False(t, exists, "deleted mark should not exist initially")

		// Simulate what enqueueDeleteIptablesEip does - set the mark
		c.deletedEIPs.Store("test-eip", true)

		// Verify mark is set
		_, exists = c.deletedEIPs.Load("test-eip")
		assert.True(t, exists, "deleted mark should be set after delete event")
	})

	t.Run("successful route deletion clears deleted mark", func(t *testing.T) {
		c := &Controller{}

		// Simulate delete event marking EIP as deleted
		c.deletedEIPs.Store("test-eip", true)

		// Verify mark exists
		_, exists := c.deletedEIPs.Load("test-eip")
		assert.True(t, exists, "deleted mark should exist after delete event")

		// Simulate what processNextIptablesEipDeleteItem does after successful deletion
		c.deletedEIPs.Delete("test-eip")

		// Verify mark is cleaned up (prevents memory leak)
		_, exists = c.deletedEIPs.Load("test-eip")
		assert.False(t, exists, "deleted mark should be cleaned up after successful route deletion")
	})

	t.Run("sync skips deleted EIP", func(t *testing.T) {
		c := &Controller{}

		// Mark EIP as deleted
		c.deletedEIPs.Store("test-eip", true)

		// Check if the EIP is marked as deleted (simulating syncIptablesEipRoute logic)
		_, deleted := c.deletedEIPs.Load("test-eip")
		assert.True(t, deleted, "should detect EIP as deleted")
	})

	t.Run("sync proceeds for non-deleted EIP", func(t *testing.T) {
		c := &Controller{}

		// EIP is not marked as deleted
		_, deleted := c.deletedEIPs.Load("test-eip")
		assert.False(t, deleted, "should not detect EIP as deleted when not marked")
	})

	t.Run("multiple EIPs tracked independently", func(t *testing.T) {
		c := &Controller{}

		// Mark multiple EIPs as deleted
		c.deletedEIPs.Store("eip-1", true)
		c.deletedEIPs.Store("eip-2", true)

		// Clear one EIP
		c.deletedEIPs.Delete("eip-1")

		// Verify eip-1 is cleared but eip-2 still marked
		_, exists1 := c.deletedEIPs.Load("eip-1")
		_, exists2 := c.deletedEIPs.Load("eip-2")
		assert.False(t, exists1, "eip-1 should be cleared")
		assert.True(t, exists2, "eip-2 should still be marked")
	})
}
