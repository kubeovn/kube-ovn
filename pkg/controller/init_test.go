package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	ovnipam "github.com/kubeovn/kube-ovn/pkg/ipam"
)

func TestHasAllocatedAnnotation(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    bool
	}{
		{
			name:        "nil annotations",
			annotations: nil,
			expected:    false,
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			expected:    false,
		},
		{
			name: "default provider allocated",
			annotations: map[string]string{
				"ovn.kubernetes.io/allocated": "true",
			},
			expected: true,
		},
		{
			name: "custom provider allocated",
			annotations: map[string]string{
				"my-provider.kubernetes.io/allocated": "true",
			},
			expected: true,
		},
		{
			name: "allocated is false",
			annotations: map[string]string{
				"ovn.kubernetes.io/allocated": "false",
			},
			expected: false,
		},
		{
			name: "unrelated annotations only",
			annotations: map[string]string{
				"app":                          "test",
				"ovn.kubernetes.io/ip_address": "10.0.0.1",
			},
			expected: false,
		},
		{
			name: "multiple providers with one allocated",
			annotations: map[string]string{
				"ovn.kubernetes.io/allocated":         "false",
				"my-provider.kubernetes.io/allocated": "true",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tt.annotations,
				},
			}
			require.Equal(t, tt.expected, hasAllocatedAnnotation(pod))
		})
	}
}

func TestInitSubnetIPAM(t *testing.T) {
	t.Run("succeeds when subnet can be initialized", func(t *testing.T) {
		controller := &Controller{
			config: &Configuration{AllowFirstIPv4Address: true},
			ipam:   ovnipam.NewIPAM(),
		}
		subnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
			Spec:       kubeovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/24"},
		}

		err := controller.initSubnetIPAM(subnet)
		require.NoError(t, err)
	})

	t.Run("fails fast when disabling first IPv4 allocation with allocated first IPv4 address", func(t *testing.T) {
		controller := &Controller{
			config: &Configuration{AllowFirstIPv4Address: false},
			ipam:   ovnipam.NewIPAM(),
		}
		subnet := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-subnet"},
			Spec:       kubeovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/24"},
		}

		require.NoError(t, controller.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, "", nil, true))
		v4, _, _, err := controller.ipam.GetRandomAddress("pod.default", "pod.default", nil, subnet.Name, "", nil, true)
		require.NoError(t, err)
		require.Equal(t, "10.0.0.0", v4)

		err = controller.initSubnetIPAM(subnet)
		require.ErrorContains(t, err, "failed to init subnet test-subnet")
		require.ErrorContains(t, err, "cannot disable allowFirstIPv4Address")
	})
}
