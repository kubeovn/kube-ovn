package controller

import (
	"context"
	"math/big"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/set"

	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/internal"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestExpandIPPoolAddresses(t *testing.T) {
	addresses, err := util.ExpandIPPoolAddresses([]string{
		"10.0.0.1",
		"2001:db8::1",
		"192.168.1.0/24",
		"10.0.0.1", // duplicate should be removed
		" 2001:db8::1 ",
	})
	require.NoError(t, err)
	require.Equal(t, []string{
		"10.0.0.1/32",
		"192.168.1.0/24",
		"2001:db8::1/128",
	}, addresses)
}

func TestExpandIPPoolAddressesRange(t *testing.T) {
	addresses, err := util.ExpandIPPoolAddresses([]string{"10.0.0.0..10.0.0.3"})
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.0/30"}, addresses)

	addresses, err = util.ExpandIPPoolAddresses([]string{"10.0.0.1..10.0.0.5"})
	require.NoError(t, err)
	require.Equal(t, []string{
		"10.0.0.1/32",
		"10.0.0.2/31",
		"10.0.0.4/31",
	}, addresses)

	addresses, err = util.ExpandIPPoolAddresses([]string{"2001:db8::1..2001:db8::4"})
	require.NoError(t, err)
	require.Equal(t, []string{
		"2001:db8::1/128",
		"2001:db8::2/127",
		"2001:db8::4/128",
	}, addresses)
}

func TestExpandIPPoolAddressesInvalid(t *testing.T) {
	_, err := util.ExpandIPPoolAddresses([]string{"10.0.0.1..2001:db8::1"})
	require.Error(t, err)

	_, err = util.ExpandIPPoolAddresses([]string{"foo"})
	require.Error(t, err)
}

func TestIPPoolAddressSetName(t *testing.T) {
	require.Equal(t, "foo.bar", util.IPPoolAddressSetName("foo-bar"))
	require.Equal(t, "123pool", util.IPPoolAddressSetName("123pool"))
}

// Additional comprehensive tests for IPPool utilities

func TestExpandIPPoolAddressesEdgeCases(t *testing.T) {
	t.Run("Empty and whitespace entries", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddresses([]string{
			"",
			"   ",
			"\t",
			"10.0.0.1",
		})
		require.NoError(t, err)
		require.Len(t, addresses, 1)
		require.Equal(t, "10.0.0.1/32", addresses[0])
	})

	t.Run("CIDR normalization", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddresses([]string{
			"192.168.1.5/24", // non-canonical, should normalize to 192.168.1.0/24
		})
		require.NoError(t, err)
		require.Equal(t, []string{"192.168.1.0/24"}, addresses)
	})

	t.Run("Large range", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddresses([]string{
			"10.0.0.0..10.0.0.255",
		})
		require.NoError(t, err)
		require.Equal(t, []string{"10.0.0.0/24"}, addresses)
	})

	t.Run("Complex mixed input", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddresses([]string{
			"10.0.0.1",
			"10.0.0.5..10.0.0.10",
			"192.168.1.0/24",
			"2001:db8::1",
			"2001:db8::100..2001:db8::103",
		})
		require.NoError(t, err)
		require.Greater(t, len(addresses), 5) // Should have multiple CIDRs
		require.Contains(t, addresses, "10.0.0.1/32")
		require.Contains(t, addresses, "192.168.1.0/24")
		require.Contains(t, addresses, "2001:db8::1/128")
	})
}

func TestExpandIPPoolAddressesRangeEdgeCases(t *testing.T) {
	t.Run("Unaligned IPv4 range", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddresses([]string{"10.0.0.1..10.0.0.5"})
		require.NoError(t, err)
		require.Equal(t, []string{
			"10.0.0.1/32",
			"10.0.0.2/31",
			"10.0.0.4/31",
		}, addresses)
	})

	t.Run("Power of 2 range", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddresses([]string{"10.0.0.0..10.0.0.7"})
		require.NoError(t, err)
		require.Equal(t, []string{"10.0.0.0/29"}, addresses)
	})

	t.Run("Single IP range", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddresses([]string{"10.0.0.1..10.0.0.1"})
		require.NoError(t, err)
		require.Equal(t, []string{"10.0.0.1/32"}, addresses)
	})

	t.Run("IPv6 unaligned range", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddresses([]string{"2001:db8::1..2001:db8::4"})
		require.NoError(t, err)
		require.Equal(t, []string{
			"2001:db8::1/128",
			"2001:db8::2/127",
			"2001:db8::4/128",
		}, addresses)
	})
}

func TestExpandIPPoolAddressesErrorConditions(t *testing.T) {
	t.Run("Completely invalid input", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddresses([]string{"totally-invalid"})
		require.Error(t, err)
	})

	t.Run("Invalid range - missing end", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddresses([]string{"10.0.0.1.."})
		require.Error(t, err)
	})

	t.Run("Invalid range - missing start", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddresses([]string{"..10.0.0.1"})
		require.Error(t, err)
	})

	t.Run("Invalid CIDR - out of range prefix", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddresses([]string{"10.0.0.0/33"})
		require.Error(t, err)
	})

	t.Run("Invalid CIDR - IPv6 out of range", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddresses([]string{"2001:db8::/129"})
		require.Error(t, err)
	})

	t.Run("Mixed IP families in range", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddresses([]string{"10.0.0.1..2001:db8::1"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "mixes IPv4 and IPv6")
	})

	t.Run("Range with end less than start", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddresses([]string{"10.0.0.100..10.0.0.1"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "start is greater than end")
	})
}

func TestCanonicalizeIPPoolEntries(t *testing.T) {
	t.Run("Basic functionality", func(t *testing.T) {
		result, err := util.CanonicalizeIPPoolEntries([]string{
			"10.0.0.1",
			"192.168.1.0/24",
		})
		require.NoError(t, err)
		require.True(t, result["10.0.0.1/32"])
		require.True(t, result["192.168.1.0/24"])
		require.False(t, result["10.0.0.2/32"])
	})

	t.Run("Empty input", func(t *testing.T) {
		result, err := util.CanonicalizeIPPoolEntries([]string{})
		require.NoError(t, err)
		require.Empty(t, result)
	})

	t.Run("Duplicate detection", func(t *testing.T) {
		result, err := util.CanonicalizeIPPoolEntries([]string{
			"10.0.0.1",
			"10.0.0.1",
		})
		require.NoError(t, err)
		require.Len(t, result, 1)
		require.True(t, result["10.0.0.1/32"])
	})
}

func TestNormalizeAddressSetEntries(t *testing.T) {
	t.Run("Standard OVN format", func(t *testing.T) {
		result := util.NormalizeAddressSetEntries(`"10.0.0.1/32" "10.0.0.2/32"`)
		require.Equal(t, set.New("10.0.0.1/32", "10.0.0.2/32"), result)
	})

	t.Run("Extra whitespace", func(t *testing.T) {
		result := util.NormalizeAddressSetEntries(`  "10.0.0.1/32"   "10.0.0.2/32"  `)
		require.Equal(t, set.New("10.0.0.1/32", "10.0.0.2/32"), result)
	})

	t.Run("Empty input", func(t *testing.T) {
		result := util.NormalizeAddressSetEntries("")
		require.Empty(t, result)
	})

	t.Run("No quotes", func(t *testing.T) {
		result := util.NormalizeAddressSetEntries("10.0.0.1/32 10.0.0.2/32")
		require.Equal(t, set.New("10.0.0.1/32", "10.0.0.2/32"), result)
	})

	t.Run("Mixed formats", func(t *testing.T) {
		result := util.NormalizeAddressSetEntries(`"192.168.1.0/24" "2001:db8::1/128"`)
		require.Equal(t, set.New("192.168.1.0/24", "2001:db8::1/128"), result)
	})
}

func TestNormalizeIP(t *testing.T) {
	t.Run("Standard IPv4", func(t *testing.T) {
		ip, err := util.NormalizeIP("192.168.1.1")
		require.NoError(t, err)
		require.Equal(t, "192.168.1.1", ip.String())
		require.NotNil(t, ip.To4())
	})

	t.Run("Standard IPv6", func(t *testing.T) {
		ip, err := util.NormalizeIP("2001:db8::1")
		require.NoError(t, err)
		require.Equal(t, "2001:db8::1", ip.String())
		require.Nil(t, ip.To4())
	})

	t.Run("IPv6 full form", func(t *testing.T) {
		ip, err := util.NormalizeIP("2001:0db8:0000:0000:0000:0000:0000:0001")
		require.NoError(t, err)
		require.Equal(t, "2001:db8::1", ip.String())
	})

	t.Run("Whitespace handling", func(t *testing.T) {
		ip, err := util.NormalizeIP("  10.0.0.1  ")
		require.NoError(t, err)
		require.Equal(t, "10.0.0.1", ip.String())
	})

	t.Run("Invalid IP", func(t *testing.T) {
		_, err := util.NormalizeIP("999.999.999.999")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid IP address")
	})

	t.Run("Not an IP", func(t *testing.T) {
		_, err := util.NormalizeIP("hostname")
		require.Error(t, err)
	})

	t.Run("IPv4 loopback", func(t *testing.T) {
		ip, err := util.NormalizeIP("127.0.0.1")
		require.NoError(t, err)
		require.Equal(t, "127.0.0.1", ip.String())
	})

	t.Run("IPv6 loopback", func(t *testing.T) {
		ip, err := util.NormalizeIP("::1")
		require.NoError(t, err)
		require.Equal(t, "::1", ip.String())
	})
}

func TestExpandIPPoolAddressesForOVNIntegration(t *testing.T) {
	// These tests use ExpandIPPoolAddressesForOVN which enforces OVN address set limitation
	t.Run("Mixed IPv4 and IPv6 - should fail", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddressesForOVN([]string{
			"10.0.0.1",
			"2001:db8::1",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "mixed IPv4 and IPv6 addresses are not supported")
	})

	t.Run("Pure IPv4 pool", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{
			"192.168.1.0/30",
			"10.0.0.1..10.0.0.5",
		})
		require.NoError(t, err)
		require.NotEmpty(t, addresses)
		// Verify all are IPv4
		for _, addr := range addresses {
			require.NotContains(t, addr, ":")
		}
		// CIDR should be preserved
		require.Contains(t, addresses, "192.168.1.0/30")
	})

	t.Run("Pure IPv6 pool", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{
			"2001:db8::/126",
			"fd00::1..fd00::3",
		})
		require.NoError(t, err)
		require.NotEmpty(t, addresses)
		// Verify all are IPv6
		for _, addr := range addresses {
			require.Contains(t, addr, ":")
		}
		// CIDR should be preserved
		require.Contains(t, addresses, "2001:db8::/126")
	})

	t.Run("Single IPs are simplified", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{
			"10.0.0.1",
			"10.0.0.2",
		})
		require.NoError(t, err)
		require.Len(t, addresses, 2)
		// Should not have /32 suffix
		require.Contains(t, addresses, "10.0.0.1")
		require.Contains(t, addresses, "10.0.0.2")
		require.NotContains(t, addresses, "10.0.0.1/32")
		require.NotContains(t, addresses, "10.0.0.2/32")
	})

	t.Run("Mixed in range notation", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddressesForOVN([]string{
			"10.0.0.1..10.0.0.5",
			"fd00::1",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "mixed IPv4 and IPv6")
	})

	t.Run("Mixed in CIDR notation", func(t *testing.T) {
		_, err := util.ExpandIPPoolAddressesForOVN([]string{
			"192.168.1.0/24",
			"2001:db8::/64",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "mixed IPv4 and IPv6")
	})

	t.Run("Range expansion with simplification", func(t *testing.T) {
		// Range that expands to /32 should be simplified
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{
			"10.0.0.1..10.0.0.1", // Single IP range
		})
		require.NoError(t, err)
		require.Len(t, addresses, 1)
		require.Equal(t, "10.0.0.1", addresses[0])
		require.NotContains(t, addresses[0], "/32")
	})

	t.Run("Range with multiple CIDRs - some simplified", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{
			"10.0.0.1..10.0.0.5",
		})
		require.NoError(t, err)
		require.NotEmpty(t, addresses)
		// Should contain simplified /32 and non-simplified /31
		require.Contains(t, addresses, "10.0.0.1")    // Simplified from /32
		require.Contains(t, addresses, "10.0.0.2/31") // Not /32, keep as-is
		require.Contains(t, addresses, "10.0.0.4/31") // Not /32, keep as-is
	})

	t.Run("Empty input", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{})
		require.NoError(t, err)
		require.Empty(t, addresses)
	})

	t.Run("Only whitespace entries", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{"", "  ", "\t"})
		require.NoError(t, err)
		require.Empty(t, addresses)
	})

	t.Run("IPv6 single IP simplified", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{"2001:db8::1"})
		require.NoError(t, err)
		require.Len(t, addresses, 1)
		require.Equal(t, "2001:db8::1", addresses[0])
		require.NotContains(t, addresses[0], "/128")
	})

	t.Run("IPv6 range with /128 simplified", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{"fd00::1..fd00::1"})
		require.NoError(t, err)
		require.Len(t, addresses, 1)
		require.Equal(t, "fd00::1", addresses[0])
	})

	t.Run("Duplicate IPs deduplicated and simplified", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{
			"10.0.0.1",
			"10.0.0.1",
			" 10.0.0.1 ",
		})
		require.NoError(t, err)
		require.Len(t, addresses, 1)
		require.Equal(t, "10.0.0.1", addresses[0])
	})

	t.Run("CIDR normalization preserved", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddressesForOVN([]string{"192.168.1.5/24"})
		require.NoError(t, err)
		require.Len(t, addresses, 1)
		require.Equal(t, "192.168.1.0/24", addresses[0])
	})
}

func TestExpandIPPoolAddressesGeneralUse(t *testing.T) {
	// ExpandIPPoolAddresses (without ForOVN suffix) allows mixed IP families
	t.Run("Mixed IPv4 and IPv6 - allowed for general use", func(t *testing.T) {
		addresses, err := util.ExpandIPPoolAddresses([]string{
			"10.0.0.1",
			"2001:db8::1",
		})
		require.NoError(t, err)
		require.NotEmpty(t, addresses)
		require.Contains(t, addresses, "10.0.0.1/32")
		require.Contains(t, addresses, "2001:db8::1/128")
	})
}

func TestEnqueueAddIPPoolRoutesTerminatingToDeleteQueue(t *testing.T) {
	c := &Controller{
		addOrUpdateIPPoolQueue: newTypedRateLimitingQueue[string]("AddIPPool", nil),
		deleteIPPoolQueue:      newTypedRateLimitingQueue[*kubeovnv1.IPPool]("DeleteIPPool", nil),
	}
	t.Cleanup(func() {
		c.addOrUpdateIPPoolQueue.ShutDown()
		c.deleteIPPoolQueue.ShutDown()
	})

	// After a controller restart the informer (re)list surfaces a terminating
	// ippool only via the add handler; it must go to the delete queue or the
	// finalizer is never removed.
	now := metav1.Now()
	terminating := &kubeovnv1.IPPool{
		Name:              "stuck",
		DeletionTimestamp: &now,
		Finalizers:        []string{util.KubeOVNControllerFinalizer},
	}
	c.enqueueAddIPPool(terminating)
	require.Equal(t, 0, c.addOrUpdateIPPoolQueue.Len())
	require.Equal(t, 1, c.deleteIPPoolQueue.Len())
	item, _ := c.deleteIPPoolQueue.Get()
	require.Same(t, terminating, item)
	c.deleteIPPoolQueue.Done(item)

	// A live ippool keeps going to the add/update queue.
	live := &kubeovnv1.IPPool{Name: "live"}
	c.enqueueAddIPPool(live)
	require.Equal(t, 1, c.addOrUpdateIPPoolQueue.Len())
	require.Equal(t, 0, c.deleteIPPoolQueue.Len())
}

func TestResolveIPPoolIPs(t *testing.T) {
	subnet := &kubeovnv1.Subnet{
		Name: "subnet1",
		Spec: kubeovnv1.SubnetSpec{
			Protocol:   kubeovnv1.ProtocolIPv4,
			CIDRBlock:  "10.0.0.0/28",
			Gateway:    "10.0.0.1",
			ExcludeIps: []string{"10.0.0.1"},
		},
	}
	ippool := &kubeovnv1.IPPool{
		Name: "pool1",
		Spec: kubeovnv1.IPPoolSpec{Subnet: subnet.Name, IPCount: 3},
	}
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet},
		IPPools: []*kubeovnv1.IPPool{ippool},
	})
	require.NoError(t, err)
	ctrl := fakeController.fakeController
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, subnet.Spec.ExcludeIps))

	pool := ippool.DeepCopy()
	ips, err := ctrl.resolveIPPoolIPs(pool)
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.2", "10.0.0.3", "10.0.0.4"}, ips)
	// the picked addresses are recorded by the caller only after IPAM accepted them
	require.Empty(t, pool.Status.IPs)
	require.NoError(t, ctrl.ipam.AddOrUpdateIPPool(subnet.Name, pool.Name, ips))
	pool.Status.IPs = ips

	// growing keeps the existing addresses and appends newly picked ones
	pool.Spec.IPCount = 5
	ips, err = ctrl.resolveIPPoolIPs(pool)
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.2", "10.0.0.3", "10.0.0.4", "10.0.0.5", "10.0.0.6"}, ips)
	require.NoError(t, ctrl.ipam.AddOrUpdateIPPool(subnet.Name, pool.Name, ips))
	pool.Status.IPs = ips

	// shrinking releases the most recently added addresses first
	pool.Spec.IPCount = 2
	ips, err = ctrl.resolveIPPoolIPs(pool)
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.2", "10.0.0.3"}, ips)
	require.NoError(t, ctrl.ipam.AddOrUpdateIPPool(subnet.Name, pool.Name, ips))
	pool.Status.IPs = ips

	// not enough free addresses in the subnet, which only holds 13 of them
	pool.Spec.IPCount = 20
	_, err = ctrl.resolveIPPoolIPs(pool)
	require.ErrorContains(t, err, "does not have enough free addresses")

	// a failed selection is not recorded, so the next retry recomputes it
	require.Equal(t, []string{"10.0.0.2", "10.0.0.3"}, pool.Status.IPs)

	// spec.ips is used as is
	pool.Spec.IPCount = 0
	pool.Spec.IPs = []string{"10.0.0.10"}
	ips, err = ctrl.resolveIPPoolIPs(pool)
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.10"}, ips)
}

func TestHandleAddOrUpdateIPPoolWithIPCount(t *testing.T) {
	subnet := &kubeovnv1.Subnet{
		Name: "subnet2",
		Spec: kubeovnv1.SubnetSpec{
			Protocol:   kubeovnv1.ProtocolIPv4,
			CIDRBlock:  "10.1.0.0/24",
			Gateway:    "10.1.0.1",
			ExcludeIps: []string{"10.1.0.1"},
		},
	}
	ippool := &kubeovnv1.IPPool{
		Name: "pool2",
		Spec: kubeovnv1.IPPoolSpec{Subnet: subnet.Name, IPCount: 2},
	}
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet},
		IPPools: []*kubeovnv1.IPPool{ippool},
	})
	require.NoError(t, err)
	ctrl := fakeController.fakeController
	ctrl.ippoolKeyMutex = keymutex.NewHashed(97)
	fakeController.mockOvnClient.EXPECT().DeleteAddressSet(gomock.Any()).Return(nil).AnyTimes()
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, subnet.Spec.ExcludeIps))

	require.NoError(t, ctrl.handleAddOrUpdateIPPool(ippool.Name))

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().IPPools().Get(context.Background(), ippool.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, []string{"10.1.0.2", "10.1.0.3"}, got.Status.IPs)
	require.True(t, got.Status.IsReady())
}

func TestResolveIPPoolIPsDualStack(t *testing.T) {
	subnet := &kubeovnv1.Subnet{
		Name: "subnet3",
		Spec: kubeovnv1.SubnetSpec{
			CIDRBlock:  "10.2.0.0/24,fd00::/120",
			Gateway:    "10.2.0.1,fd00::1",
			ExcludeIps: []string{"10.2.0.1", "fd00::1"},
		},
	}
	ippool := &kubeovnv1.IPPool{
		Name: "pool3",
		Spec: kubeovnv1.IPPoolSpec{Subnet: subnet.Name, IPCount: 2},
	}
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet},
		IPPools: []*kubeovnv1.IPPool{ippool},
	})
	require.NoError(t, err)
	ctrl := fakeController.fakeController
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, subnet.Spec.ExcludeIps))

	// ipCount applies to each IP family
	pool := ippool.DeepCopy()
	ips, err := ctrl.resolveIPPoolIPs(pool)
	require.NoError(t, err)
	require.Equal(t, []string{"10.2.0.2", "10.2.0.3", "fd00::2", "fd00::3"}, ips)
	require.NoError(t, ctrl.ipam.AddOrUpdateIPPool(subnet.Name, pool.Name, ips))
	pool.Status.IPs = ips

	pool.Spec.IPCount = 1
	ips, err = ctrl.resolveIPPoolIPs(pool)
	require.NoError(t, err)
	require.Equal(t, []string{"10.2.0.2", "fd00::2"}, ips)
}

func TestIPPoolIPs(t *testing.T) {
	// a pool using spec.ips keeps them even if a stale status.ips is present
	pool := &kubeovnv1.IPPool{
		Spec:   kubeovnv1.IPPoolSpec{IPs: []string{"10.0.0.1"}},
		Status: kubeovnv1.IPPoolStatus{IPs: []string{"10.0.0.9"}},
	}
	require.Equal(t, []string{"10.0.0.1"}, ippoolIPs(pool))

	// a pool using spec.ipCount is restored from status.ips, e.g. on controller restart
	pool.Spec.IPs, pool.Spec.IPCount = nil, 1
	require.Equal(t, []string{"10.0.0.9"}, ippoolIPs(pool))
}

// TestPatchIPPoolStatusKeepsIPs guards the regression where a statistics-only status
// update, built from an informer cache that has not caught up with the last reconcile
// yet, rolled status.ips back to its previous value.
func TestPatchIPPoolStatusKeepsIPs(t *testing.T) {
	ippool := &kubeovnv1.IPPool{
		Name: "pool4",
		Spec: kubeovnv1.IPPoolSpec{Subnet: "subnet4", IPCount: 2},
	}
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{IPPools: []*kubeovnv1.IPPool{ippool}})
	require.NoError(t, err)
	ctrl := fakeController.fakeController
	ippools := ctrl.config.KubeOvnClient.KubeovnV1().IPPools()

	ips := []string{"10.3.0.2", "10.3.0.3"}
	require.NoError(t, ctrl.patchIPPoolIPs(ippool, ips))
	require.Equal(t, ips, ippool.Status.IPs)
	got, err := ippools.Get(context.Background(), ippool.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, ips, got.Status.IPs)

	// a stale copy, as returned by the lister before the patch above is observed
	stale := ippool.DeepCopy()
	stale.Status.IPs = nil
	stale.Status.V4UsingIPs = internal.BigInt{Int: *big.NewInt(1)}
	require.NoError(t, ctrl.patchIPPoolStatus(stale))

	got, err = ippools.Get(context.Background(), ippool.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, ips, got.Status.IPs)
	require.Equal(t, int64(1), got.Status.V4UsingIPs.Int64())

	// re-recording the same addresses does not issue a patch
	ippool.Status.IPs = ips
	require.NoError(t, ctrl.patchIPPoolIPs(ippool, ips))
}

func TestEnqueueUpdateIPPool(t *testing.T) {
	ctrl := newFakeController(t).fakeController
	ctrl.addOrUpdateIPPoolQueue = newTypedRateLimitingQueue[string]("TestAddIPPool", nil)
	defer ctrl.addOrUpdateIPPoolQueue.ShutDown()

	old := &kubeovnv1.IPPool{
		Name: "pool5",
		Spec: kubeovnv1.IPPoolSpec{Subnet: "subnet5", IPCount: 2},
	}

	// a status-only change must not be enqueued, otherwise recording status.ips would
	// trigger an endless reconcile loop
	unchanged := old.DeepCopy()
	unchanged.Status.IPs = []string{"10.4.0.2", "10.4.0.3"}
	ctrl.enqueueUpdateIPPool(old, unchanged)
	require.Equal(t, 0, ctrl.addOrUpdateIPPoolQueue.Len())

	// changing the requested number of addresses must trigger a reconcile
	resized := old.DeepCopy()
	resized.Spec.IPCount = 3
	ctrl.enqueueUpdateIPPool(old, resized)
	require.Equal(t, 1, ctrl.addOrUpdateIPPoolQueue.Len())
	key, _ := ctrl.addOrUpdateIPPoolQueue.Get()
	require.Equal(t, old.Name, key)
}

func TestHandleAddOrUpdateIPPoolResolveFailure(t *testing.T) {
	subnet := &kubeovnv1.Subnet{
		Name: "subnet6",
		Spec: kubeovnv1.SubnetSpec{Protocol: kubeovnv1.ProtocolIPv4, CIDRBlock: "10.5.0.0/24"},
	}
	ippool := &kubeovnv1.IPPool{
		Name:   "pool6",
		Spec:   kubeovnv1.IPPoolSpec{Subnet: subnet.Name, IPCount: 2},
		Status: kubeovnv1.IPPoolStatus{IPs: []string{"10.5.0.2", "10.5.0.3"}},
	}
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet},
		IPPools: []*kubeovnv1.IPPool{ippool},
	})
	require.NoError(t, err)
	ctrl := fakeController.fakeController
	ctrl.ippoolKeyMutex = keymutex.NewHashed(97)

	// the subnet is not registered in IPAM yet, so no address can be picked
	err = ctrl.handleAddOrUpdateIPPool(ippool.Name)
	require.ErrorContains(t, err, "does not exist in IPAM")

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().IPPools().Get(context.Background(), ippool.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, ippool.Status.IPs, got.Status.IPs, "a failed reconcile must not drop the recorded addresses")
	require.False(t, got.Status.IsReady())
}

func TestHandleAddOrUpdateIPPoolAddressSetWithIPCount(t *testing.T) {
	subnet := &kubeovnv1.Subnet{
		Name: "subnet7",
		Spec: kubeovnv1.SubnetSpec{
			Protocol:   kubeovnv1.ProtocolIPv4,
			CIDRBlock:  "10.6.0.0/24",
			Gateway:    "10.6.0.1",
			ExcludeIps: []string{"10.6.0.1"},
		},
	}
	ippool := &kubeovnv1.IPPool{
		Name: "pool7",
		Spec: kubeovnv1.IPPoolSpec{Subnet: subnet.Name, IPCount: 2, EnableAddressSet: true},
	}
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet},
		IPPools: []*kubeovnv1.IPPool{ippool},
	})
	require.NoError(t, err)
	ctrl := fakeController.fakeController
	ctrl.ippoolKeyMutex = keymutex.NewHashed(97)
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, subnet.Spec.ExcludeIps))

	asName := util.IPPoolAddressSetName(ippool.Name)
	mockOvnClient := fakeController.mockOvnClient
	mockOvnClient.EXPECT().CreateAddressSet(asName, map[string]string{ippoolKey: ippool.Name}).Return(nil)
	// the address set must hold the addresses picked for spec.ipCount, not spec.ips
	mockOvnClient.EXPECT().AddressSetUpdateAddress(asName, "10.6.0.2", "10.6.0.3").Return(nil)

	require.NoError(t, ctrl.handleAddOrUpdateIPPool(ippool.Name))

	got, err := ctrl.config.KubeOvnClient.KubeovnV1().IPPools().Get(context.Background(), ippool.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, []string{"10.6.0.2", "10.6.0.3"}, got.Status.IPs)
}

func TestResolveIPPoolIPsStaleAddresses(t *testing.T) {
	subnet := &kubeovnv1.Subnet{
		Name: "subnet8",
		Spec: kubeovnv1.SubnetSpec{
			Protocol:   kubeovnv1.ProtocolIPv4,
			CIDRBlock:  "10.7.0.0/24",
			Gateway:    "10.7.0.1",
			ExcludeIps: []string{"10.7.0.1"},
		},
	}
	ippool := &kubeovnv1.IPPool{
		Name: "pool8",
		Spec: kubeovnv1.IPPoolSpec{Subnet: subnet.Name, IPCount: 3},
		// 10.8.0.2 is left over from the previous CIDR of the subnet
		Status: kubeovnv1.IPPoolStatus{IPs: []string{"10.7.0.5", "10.8.0.2", "10.7.0.6"}},
	}
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet},
		IPPools: []*kubeovnv1.IPPool{ippool},
	})
	require.NoError(t, err)
	ctrl := fakeController.fakeController
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, subnet.Spec.ExcludeIps))

	// addresses outside the current CIDR are dropped, the missing one is picked again
	pool := ippool.DeepCopy()
	ips, err := ctrl.resolveIPPoolIPs(pool)
	require.NoError(t, err)
	require.Equal(t, []string{"10.7.0.5", "10.7.0.6", "10.7.0.2"}, ips)

	// a pool whose subnet does not exist yet cannot be resolved
	pool.Spec.Subnet = "noSuchSubnet"
	_, err = ctrl.resolveIPPoolIPs(pool)
	require.Error(t, err)
}

// TestResolveIPPoolIPsExceedingMaxCount covers pools created before the CRD gained its
// upper bound: such a pool is rejected instead of carving a large chunk out of the subnet.
func TestResolveIPPoolIPsExceedingMaxCount(t *testing.T) {
	subnet := &kubeovnv1.Subnet{
		Name: "subnet9",
		Spec: kubeovnv1.SubnetSpec{Protocol: kubeovnv1.ProtocolIPv4, CIDRBlock: "10.9.0.0/26", Gateway: "10.9.0.1"},
	}
	ippool := &kubeovnv1.IPPool{
		Name: "pool9",
		Spec: kubeovnv1.IPPoolSpec{Subnet: subnet.Name, IPCount: maxIPPoolIPCount + 1},
	}
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet},
		IPPools: []*kubeovnv1.IPPool{ippool},
	})
	require.NoError(t, err)
	ctrl := fakeController.fakeController
	require.NoError(t, ctrl.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, nil))

	_, err = ctrl.resolveIPPoolIPs(ippool.DeepCopy())
	require.ErrorContains(t, err, "more than the maximum")

	// the limit itself is allowed
	pool := ippool.DeepCopy()
	pool.Spec.IPCount = maxIPPoolIPCount
	ips, err := ctrl.resolveIPPoolIPs(pool)
	require.NoError(t, err)
	require.Len(t, ips, maxIPPoolIPCount)
}
