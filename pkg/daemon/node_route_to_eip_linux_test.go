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
