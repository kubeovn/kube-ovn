package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
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

func TestRestoreVipsToIPAMSkipsUnallocatedVip(t *testing.T) {
	// a vip that has never been allocated (e.g. still pending its first reconcile, or an
	// earlier allocation attempt failed) has an empty v4ip/v6ip/mac status. GetStaticAddress
	// rejects an empty address set outright, so restoring it must be skipped rather than
	// attempted - otherwise it's pure log noise on every restart for any pending vip.
	c := newFakeController(t).fakeController
	require.NoError(t, c.ipam.AddOrUpdateSubnet("subnet-a", "10.0.0.0/24", "10.0.0.1", nil))

	pendingVip := &kubeovnv1.Vip{
		Name: "pending-vip",
		Spec: kubeovnv1.VipSpec{Subnet: "subnet-a"},
	}
	// no subnetProviderMaps entry for subnet-a either: if the pending vip were not skipped
	// first, it would also fail on the missing-provider lookup before ever reaching
	// GetStaticAddress, masking which check actually matters here.
	c.restoreVipsToIPAM([]*kubeovnv1.Vip{pendingVip}, map[string]string{})

	require.Empty(t, c.ipam.GetPodAddress(pendingVip.Name), "nothing should be registered in ipam for an unallocated vip")
}

func TestRestoreVipsToIPAMRestoresMacOnlySubnetVipEvenWithoutIP(t *testing.T) {
	// a vip allocated on a real mac-only-protocol subnet (BYO-DHCP underlay) permanently has
	// an empty v4ip/v6ip by design - only its mac is ever set. Skipping restoration based on
	// an empty v4ip/v6ip (instead of the allocated/unallocated signal, status.Mac) would
	// silently drop its mac registration on every controller restart, letting another nic
	// reuse the same mac undetected.
	c := newFakeController(t).fakeController
	require.NoError(t, c.ipam.AddOrUpdateSubnet("mac-only-subnet", "", "", nil))

	macOnlyVip := &kubeovnv1.Vip{
		Name:   "mac-only-vip",
		Spec:   kubeovnv1.VipSpec{Subnet: "mac-only-subnet"},
		Status: kubeovnv1.VipStatus{Mac: "02:ac:10:ff:02:07"},
	}
	subnetProviderMaps := map[string]string{"mac-only-subnet": "mac-only-subnet.ovn"}

	c.restoreVipsToIPAM([]*kubeovnv1.Vip{macOnlyVip}, subnetProviderMaps)

	mac := macOnlyVip.Status.Mac
	_, _, _, err := c.ipam.GetStaticAddress("someone-else", "someone-else-port", "", &mac, "mac-only-subnet", true)
	require.ErrorIs(t, err, ipam.ErrConflict, "the mac-only vip's mac must still be reserved in ipam after restore")
}

func TestRestoreVipsToIPAMRestoresVipWithIPButNoMac(t *testing.T) {
	// the "unallocated" skip must not key off status.Mac alone. A vip that already has a
	// v4ip/v6ip reserved has to be restored even if, for whatever reason (e.g. a status
	// written before a mac was ever recorded), its mac ended up empty - otherwise that
	// address is left unreserved in ipam and a pod could be handed the same address while
	// the vip is still using it.
	c := newFakeController(t).fakeController
	require.NoError(t, c.ipam.AddOrUpdateSubnet("subnet-a", "10.0.0.0/24", "10.0.0.1", nil))

	noMacVip := &kubeovnv1.Vip{
		Name:   "no-mac-vip",
		Spec:   kubeovnv1.VipSpec{Subnet: "subnet-a"},
		Status: kubeovnv1.VipStatus{V4ip: "10.0.0.9"},
	}
	subnetProviderMaps := map[string]string{"subnet-a": "subnet-a.ovn"}

	c.restoreVipsToIPAM([]*kubeovnv1.Vip{noMacVip}, subnetProviderMaps)

	addrs := c.ipam.GetPodAddress(noMacVip.Name)
	require.Len(t, addrs, 1, "a vip with an allocated ip but no mac must still be restored")
	require.Equal(t, noMacVip.Status.V4ip, addrs[0].IP)
}
