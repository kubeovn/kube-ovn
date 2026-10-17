package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
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
				Annotations: tt.annotations,
			}
			require.Equal(t, tt.expected, hasAllocatedAnnotation(pod))
		})
	}
}

func TestRestoreVipsToIPAMPreservesDualStackAndV6OnlyAddresses(t *testing.T) {
	// a vip's full v4/v6 address set must be passed to ipam together: a dual-stack vip
	// restored from only its v4ip would get a freshly randomized v6ip (disagreeing with
	// vip.Status and able to hand the old v6ip out again), and a v6-only vip restored from
	// an empty v4ip would fail outright.
	c := newFakeController(t).fakeController
	require.NoError(t, c.ipam.AddOrUpdateSubnet("dual-subnet", "10.0.0.0/24,fd00::/64", "10.0.0.1,fd00::1", nil))
	require.NoError(t, c.ipam.AddOrUpdateSubnet("v6-subnet", "fd01::/64", "fd01::1", nil))

	dualVip := &kubeovnv1.Vip{
		Name:   "dual-vip",
		Spec:   kubeovnv1.VipSpec{Subnet: "dual-subnet"},
		Status: kubeovnv1.VipStatus{V4ip: "10.0.0.5", V6ip: "fd00::5", Mac: "02:ac:10:ff:02:05"},
	}
	v6OnlyVip := &kubeovnv1.Vip{
		Name:   "v6-vip",
		Spec:   kubeovnv1.VipSpec{Subnet: "v6-subnet"},
		Status: kubeovnv1.VipStatus{V6ip: "fd01::5", Mac: "02:ac:10:ff:02:06"},
	}
	subnetProviderMaps := map[string]string{"dual-subnet": "dual-subnet.ovn", "v6-subnet": "v6-subnet.ovn"}

	c.restoreVipsToIPAM([]*kubeovnv1.Vip{dualVip, v6OnlyVip}, subnetProviderMaps)

	dualAddrs := c.ipam.GetPodAddress(dualVip.Name)
	require.Len(t, dualAddrs, 2, "dual-stack vip must keep both its v4 and v6 address")
	var gotV4, gotV6 string
	for _, a := range dualAddrs {
		switch a.IP {
		case dualVip.Status.V4ip:
			gotV4 = a.IP
		case dualVip.Status.V6ip:
			gotV6 = a.IP
		}
	}
	require.Equal(t, dualVip.Status.V4ip, gotV4)
	require.Equal(t, dualVip.Status.V6ip, gotV6, "dual-stack restore must keep the original v6 address, not randomize a new one")

	v6Addrs := c.ipam.GetPodAddress(v6OnlyVip.Name)
	require.Len(t, v6Addrs, 1)
	require.Equal(t, v6OnlyVip.Status.V6ip, v6Addrs[0].IP, "an ipv6-only vip must restore its v6 address instead of failing on an empty v4ip")
}
