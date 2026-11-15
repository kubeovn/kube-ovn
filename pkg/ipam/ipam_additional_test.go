package ipam

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdditionalIPAM(t *testing.T) {
	subnetName := "test"
	ipv4CIDR, ipv6CIDR := "10.16.0.0/16", "fd00::/112"
	ipv4ExcludeIPs := []string{
		"10.16.0.1",
		"10.16.0.10..10.16.0.20",
		"10.16.0.15..10.16.0.23",
		"10.16.0.4",
		"192.168.0.1..192.168.0.10",
	}
	ipv6ExcludeIPs := []string{
		"fd00::1",
		"fd00::a..fd00::14",
		"fd00::f..fd00::17",
		"fd00::4",
		"2001::1..2001::a",
	}
	v4Gw := "10.16.0.1"
	v6Gw := "fd00::1"
	dualGw := "10.16.0.1,fd00::1"

	dualCIDR := fmt.Sprintf("%s,%s", ipv4CIDR, ipv6CIDR)
	dualExcludeIPs := append(ipv4ExcludeIPs, ipv6ExcludeIPs...)

	t.Run("Subnet", func(t *testing.T) {
		t.Run("IPv4", func(t *testing.T) {
			t.Run("invalid subnet", func(t *testing.T) {
				im := NewIPAM()

				t.Log("invalid mask len > 32")
				maskV4Length := 33
				err := im.AddOrUpdateSubnet(subnetName, fmt.Sprintf("1.1.1.1/%d", maskV4Length), v4Gw, nil)
				require.ErrorIs(t, err, ErrInvalidCIDR)

				t.Log("invalid ip range")
				invalidV4Ip := "1.1.256.1/24"
				err = im.AddOrUpdateSubnet(subnetName, invalidV4Ip, v4Gw, nil)
				require.ErrorIs(t, err, ErrInvalidCIDR)
			})

			t.Run("normal subnet", func(t *testing.T) {
				t.Log("create pod with static ip")
				im := NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, ipv4CIDR, v4Gw, ipv4ExcludeIPs)
				require.NoError(t, err)
				mask, err := im.GetSubnetV4Mask(subnetName)
				require.NoError(t, err)
				require.Equal(t, strings.Split(ipv4CIDR, "/")[1], mask)
				require.Equal(t, v4Gw, im.Subnets[subnetName].V4Gw)

				pod1 := "pod1.ns"
				pod1Nic1 := "pod1nic1.ns"
				freeIP1 := im.Subnets[subnetName].V4Free.At(0).Start().String()
				ip, _, _, err := im.GetStaticAddress(pod1, pod1Nic1, freeIP1, nil, subnetName, true)
				require.NoError(t, err)
				require.Equal(t, freeIP1, ip)

				ip, _, _, err = im.GetRandomAddress(pod1, pod1Nic1, nil, subnetName, "", nil, true)
				require.NoError(t, err)
				require.Equal(t, freeIP1, ip)

				t.Log("create multiple ips on one pod")
				pod2 := "pod2.ns"
				pod2Nic1 := "pod2Nic1.ns"
				pod2Nic2 := "pod2Nic2.ns"

				freeIP2 := im.Subnets[subnetName].V4Free.At(0).Start().String()
				ip, _, _, err = im.GetRandomAddress(pod2, pod2Nic1, nil, subnetName, "", nil, true)
				require.NoError(t, err)
				require.Equal(t, freeIP2, ip)

				freeIP3 := im.Subnets[subnetName].V4Free.At(0).Start().String()
				ip, _, _, err = im.GetRandomAddress(pod2, pod2Nic2, nil, subnetName, "", nil, true)

				require.NoError(t, err)
				require.Equal(t, freeIP3, ip)

				addresses := im.GetPodAddress(pod2)
				require.Len(t, addresses, 2)
				require.Equal(t, []string{freeIP2, freeIP3}, []string{addresses[0].IP, addresses[1].IP})
				require.True(t, im.ContainAddress(freeIP2))
				require.True(t, im.ContainAddress(freeIP3))

				_, isIPAssigned := im.IsIPAssignedToOtherPod(freeIP2, subnetName, pod2)
				require.False(t, isIPAssigned)

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIP3, subnetName, pod2)
				require.False(t, isIPAssigned)

				assignedPod, isIPAssigned := im.IsIPAssignedToOtherPod(freeIP1, subnetName, pod2)
				require.True(t, isIPAssigned)
				require.Equal(t, pod1, assignedPod)

				t.Log("get static ip conflict with ip in use")
				pod3 := "pod3.ns"
				pod3Nic1 := "pod3Nic1.ns"
				_, _, _, err = im.GetStaticAddress(pod3, pod3Nic1, freeIP3, nil, subnetName, true)
				require.ErrorIs(t, err, ErrConflict)

				t.Log("release pod with multiple nics")
				im.ReleaseAddressByPod(pod2, "")
				ip2, err := NewIP(freeIP2)
				require.NoError(t, err)
				ip3, err := NewIP(freeIP3)
				require.NoError(t, err)
				require.True(t, im.Subnets[subnetName].IPPools[""].V4Released.Contains(ip2))
				require.True(t, im.Subnets[subnetName].IPPools[""].V4Released.Contains(ip3))

				t.Log("release pod with single nic")
				im.ReleaseAddressByPod(pod1, "")
				ip1, err := NewIP(freeIP1)
				require.NoError(t, err)
				require.True(t, im.Subnets[subnetName].IPPools[""].V4Released.Contains(ip1))

				t.Log("create new pod with released ips")
				pod4 := "pod4.ns"
				pod4Nic1 := "pod4Nic1.ns"

				_, _, _, err = im.GetStaticAddress(pod4, pod4Nic1, freeIP1, nil, subnetName, true)
				require.NoError(t, err)

				t.Log("create pod with no initialized subnet")
				pod5 := "pod5.ns"
				pod5Nic1 := "pod5Nic1.ns"

				_, _, _, err = im.GetRandomAddress(pod5, pod5Nic1, nil, "invalid_subnet", "", nil, true)
				require.ErrorIs(t, err, ErrNoSubnet)
			})

		})

		t.Run("IPv6", func(t *testing.T) {
			t.Run("invalid subnet", func(t *testing.T) {
				im := NewIPAM()

				maskV6Length := 129
				t.Log("invalid mask len > 128")
				err := im.AddOrUpdateSubnet(subnetName, fmt.Sprintf("fd00::/%d", maskV6Length), v6Gw, nil)
				require.ErrorIs(t, err, ErrInvalidCIDR)

				t.Log("invalid ip range")
				err = im.AddOrUpdateSubnet(subnetName, "fd00::g/120", v6Gw, nil)
				require.ErrorIs(t, err, ErrInvalidCIDR)
			})

			t.Run("normal subnet", func(t *testing.T) {
				t.Log("create pod with static ip")
				im := NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, ipv6CIDR, v6Gw, ipv6ExcludeIPs)
				require.NoError(t, err)
				require.Equal(t, v6Gw, im.Subnets[subnetName].V6Gw)

				pod1 := "pod1.ns"
				pod1Nic1 := "pod1nic1.ns"
				freeIP1 := im.Subnets[subnetName].V6Free.At(0).Start().String()
				_, ip, _, err := im.GetStaticAddress(pod1, pod1Nic1, freeIP1, nil, subnetName, true)

				require.NoError(t, err)
				require.Equal(t, freeIP1, ip)

				_, ip, _, err = im.GetRandomAddress(pod1, pod1Nic1, nil, subnetName, "", nil, true)
				require.NoError(t, err)
				require.Equal(t, freeIP1, ip)

				t.Log("create multiple ips on one pod")
				pod2 := "pod2.ns"
				pod2Nic1 := "pod2Nic1.ns"
				pod2Nic2 := "pod2Nic2.ns"

				freeIP2 := im.Subnets[subnetName].V6Free.At(0).Start().String()
				_, ip, _, err = im.GetRandomAddress(pod2, pod2Nic1, nil, subnetName, "", nil, true)
				require.NoError(t, err)
				require.Equal(t, freeIP2, ip)

				freeIP3 := im.Subnets[subnetName].V6Free.At(0).Start().String()
				_, ip, _, err = im.GetRandomAddress(pod2, pod2Nic2, nil, subnetName, "", nil, true)
				require.NoError(t, err)
				require.Equal(t, freeIP3, ip)

				addresses := im.GetPodAddress(pod2)
				require.Len(t, addresses, 2)
				require.Equal(t, []string{freeIP2, freeIP3}, []string{addresses[0].IP, addresses[1].IP})
				require.True(t, im.ContainAddress(freeIP2))
				require.True(t, im.ContainAddress(freeIP3))

				_, isIPAssigned := im.IsIPAssignedToOtherPod(freeIP2, subnetName, pod2)
				require.False(t, isIPAssigned)

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIP3, subnetName, pod2)
				require.False(t, isIPAssigned)

				assignedPod, isIPAssigned := im.IsIPAssignedToOtherPod(freeIP1, subnetName, pod2)
				require.True(t, isIPAssigned)
				require.Equal(t, pod1, assignedPod)

				t.Log("get static ip conflict with ip in use")
				pod3 := "pod3.ns"
				pod3Nic1 := "pod3Nic1.ns"
				_, _, _, err = im.GetStaticAddress(pod3, pod3Nic1, freeIP3, nil, subnetName, true)
				require.ErrorIs(t, err, ErrConflict)

				t.Log("release pod with multiple nics")
				im.ReleaseAddressByPod(pod2, "")
				ip2, err := NewIP(freeIP2)
				require.NoError(t, err)
				ip3, err := NewIP(freeIP3)
				require.NoError(t, err)
				require.True(t, im.Subnets[subnetName].IPPools[""].V6Released.Contains(ip2))
				require.True(t, im.Subnets[subnetName].IPPools[""].V6Released.Contains(ip3))

				t.Log("release pod with single nic")
				im.ReleaseAddressByPod(pod1, "")
				ip1, err := NewIP(freeIP1)
				require.NoError(t, err)
				require.True(t, im.Subnets[subnetName].IPPools[""].V6Released.Contains(ip1))

				t.Log("create new pod with released ips")
				pod4 := "pod4.ns"
				pod4Nic1 := "pod4Nic1.ns"

				_, _, _, err = im.GetStaticAddress(pod4, pod4Nic1, freeIP1, nil, subnetName, true)
				require.NoError(t, err)

				t.Log("create pod with no initialized subnet")
				pod5 := "pod5.ns"
				pod5Nic1 := "pod5Nic1.ns"

				_, _, _, err = im.GetRandomAddress(pod5, pod5Nic1, nil, "invalid_subnet", "", nil, true)
				require.ErrorIs(t, err, ErrNoSubnet)
			})

		})

		t.Run("DualStack", func(t *testing.T) {
			t.Run("invalid subnet", func(t *testing.T) {
				im := NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, "1.1.1.1/64,"+ipv6CIDR, dualGw, nil)
				require.ErrorIs(t, err, ErrInvalidCIDR)
				err = im.AddOrUpdateSubnet(subnetName, "1.1.256.1/24,"+ipv6CIDR, dualGw, nil)
				require.ErrorIs(t, err, ErrInvalidCIDR)
				err = im.AddOrUpdateSubnet(subnetName, ipv4CIDR+",fd00::/130", dualGw, nil)
				require.ErrorIs(t, err, ErrInvalidCIDR)
				err = im.AddOrUpdateSubnet(subnetName, ipv4CIDR+",fd00::g/120", dualGw, nil)
				require.ErrorIs(t, err, ErrInvalidCIDR)
			})

			t.Run("normal subnet", func(t *testing.T) {
				t.Log("create pod with static ip")
				im := NewIPAM()
				err := im.AddOrUpdateSubnet(subnetName, dualCIDR, dualGw, dualExcludeIPs)
				require.NoError(t, err)
				require.Equal(t, v6Gw, im.Subnets[subnetName].V6Gw)
				require.Equal(t, v4Gw, im.Subnets[subnetName].V4Gw)

				pod1 := "pod1.ns"
				pod1Nic1 := "pod1nic1.ns"
				freeIP41 := im.Subnets[subnetName].V4Free.At(0).Start().String()
				freeIP61 := im.Subnets[subnetName].V6Free.At(0).Start().String()
				dualIP := fmt.Sprintf("%s,%s", freeIP41, freeIP61)
				ip4, ip6, _, err := im.GetStaticAddress(pod1, pod1Nic1, dualIP, nil, subnetName, true)
				require.NoError(t, err)
				require.Equal(t, freeIP41, ip4)
				require.Equal(t, freeIP61, ip6)

				ip4, ip6, _, err = im.GetRandomAddress(pod1, pod1Nic1, nil, subnetName, "", nil, true)
				require.NoError(t, err)
				require.Equal(t, freeIP41, ip4)
				require.Equal(t, freeIP61, ip6)

				t.Log("create multiple ips on one pod")
				pod2 := "pod2.ns"
				pod2Nic1 := "pod2Nic1.ns"
				pod2Nic2 := "pod2Nic2.ns"

				freeIP42 := im.Subnets[subnetName].V4Free.At(0).Start().String()
				freeIP62 := im.Subnets[subnetName].V6Free.At(0).Start().String()
				ip4, ip6, _, err = im.GetRandomAddress(pod2, pod2Nic1, nil, subnetName, "", nil, true)
				require.NoError(t, err)
				require.Equal(t, freeIP42, ip4)
				require.Equal(t, freeIP62, ip6)

				freeIP43 := im.Subnets[subnetName].V4Free.At(0).Start().String()
				freeIP63 := im.Subnets[subnetName].V6Free.At(0).Start().String()
				ip4, ip6, _, err = im.GetRandomAddress(pod2, pod2Nic2, nil, subnetName, "", nil, true)
				require.NoError(t, err)
				require.Equal(t, freeIP43, ip4)
				require.Equal(t, freeIP63, ip6)

				addresses := im.GetPodAddress(pod2)
				require.Len(t, addresses, 4)
				require.Equal(t, []string{freeIP42, freeIP62, freeIP43, freeIP63}, []string{addresses[0].IP, addresses[1].IP, addresses[2].IP, addresses[3].IP})
				require.True(t, im.ContainAddress(freeIP42))
				require.True(t, im.ContainAddress(freeIP43))
				require.True(t, im.ContainAddress(freeIP62))
				require.True(t, im.ContainAddress(freeIP63))

				_, isIPAssigned := im.IsIPAssignedToOtherPod(freeIP42, subnetName, pod2)
				require.False(t, isIPAssigned)

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIP62, subnetName, pod2)
				require.False(t, isIPAssigned)

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIP43, subnetName, pod2)
				require.False(t, isIPAssigned)

				_, isIPAssigned = im.IsIPAssignedToOtherPod(freeIP63, subnetName, pod2)
				require.False(t, isIPAssigned)

				assignedPod, isIPAssigned := im.IsIPAssignedToOtherPod(freeIP41, subnetName, pod2)
				require.True(t, isIPAssigned)
				require.Equal(t, pod1, assignedPod)

				t.Log("get static ip conflict with ip in use")
				pod3 := "pod3.ns"
				pod3Nic1 := "pod3Nic1.ns"
				_, _, _, err = im.GetStaticAddress(pod3, pod3Nic1, freeIP43, nil, subnetName, true)
				require.ErrorIs(t, err, ErrConflict)

				_, _, _, err = im.GetStaticAddress(pod3, pod3Nic1, freeIP63, nil, subnetName, true)
				require.ErrorIs(t, err, ErrConflict)

				t.Log("release pod with multiple nics")
				im.ReleaseAddressByPod(pod2, "")
				ip42, err := NewIP(freeIP42)
				require.NoError(t, err)
				ip43, err := NewIP(freeIP43)
				require.NoError(t, err)
				ip62, err := NewIP(freeIP62)
				require.NoError(t, err)
				ip63, err := NewIP(freeIP63)
				require.NoError(t, err)
				require.True(t, im.Subnets[subnetName].IPPools[""].V4Released.Contains(ip42))
				require.True(t, im.Subnets[subnetName].IPPools[""].V4Released.Contains(ip43))
				require.True(t, im.Subnets[subnetName].IPPools[""].V6Released.Contains(ip62))
				require.True(t, im.Subnets[subnetName].IPPools[""].V6Released.Contains(ip63))

				t.Log("release pod with single nic")
				im.ReleaseAddressByPod(pod1, "")
				ip41, err := NewIP(freeIP41)
				require.NoError(t, err)
				ip61, err := NewIP(freeIP61)
				require.NoError(t, err)
				require.True(t, im.Subnets[subnetName].IPPools[""].V4Released.Contains(ip41))
				require.True(t, im.Subnets[subnetName].IPPools[""].V6Released.Contains(ip61))

				t.Log("create new pod with released ips")
				pod4 := "pod4.ns"
				pod4Nic1 := "pod4Nic1.ns"

				_, _, _, err = im.GetStaticAddress(pod4, pod4Nic1, freeIP41, nil, subnetName, true)
				require.NoError(t, err)

				_, _, _, err = im.GetStaticAddress(pod4, pod4Nic1, freeIP61, nil, subnetName, true)
				require.NoError(t, err)

				t.Log("create pod with no initialized subnet")
				pod5 := "pod5.ns"
				pod5Nic1 := "pod5Nic1.ns"

				_, _, _, err = im.GetRandomAddress(pod5, pod5Nic1, nil, "invalid_subnet", "", nil, true)
				require.ErrorIs(t, err, ErrNoSubnet)
			})

		})
	})
}
