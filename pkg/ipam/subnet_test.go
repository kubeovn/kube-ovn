package ipam

import (
	"testing"

	"github.com/stretchr/testify/require"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestNewSubnetIPv4(t *testing.T) {
	excludeIps := []string{"10.0.0.2", "10.0.0.4", "10.0.0.100", "10.0.0.252", "10.0.0.253", "10.0.0.254"}
	subnet, err := NewSubnet("v4Subnet", "10.0.0.0/24", excludeIps)
	// check V4
	require.NoError(t, err)
	require.NotNil(t, subnet)
	require.Equal(t, "v4Subnet", subnet.Name)
	require.Equal(t, apiv1.ProtocolIPv4, subnet.Protocol)
	require.NotNil(t, subnet.V4CIDR)
	require.Equal(t, "10.0.0.0/24", subnet.V4CIDR.String())
	require.Empty(t, subnet.V4Gw)
	require.Empty(t, subnet.V6Gw)
	require.NotNil(t, subnet.V4Free)
	require.Equal(t, "10.0.0.1,10.0.0.3,10.0.0.5-10.0.0.99,10.0.0.101-10.0.0.251", subnet.V4Free.String())
	require.NotNil(t, subnet.V4Reserved)
	require.Equal(t, "10.0.0.2,10.0.0.4,10.0.0.100,10.0.0.252-10.0.0.254", subnet.V4Reserved.String())
	require.NotNil(t, subnet.V4Available)
	require.Equal(t, "10.0.0.1,10.0.0.3,10.0.0.5-10.0.0.99,10.0.0.101-10.0.0.251", subnet.V4Available.String())
	require.True(t, subnet.V4Available.Equal(subnet.V4Free))
	require.Equal(t, subnet.V4Using.Len(), 0)
	require.Len(t, subnet.V4NicToIP, 0)
	require.Len(t, subnet.V4IPToPod, 0)
	require.NotNil(t, subnet.V4Available)
	require.True(t, subnet.V4Available.Equal(subnet.V4Free))
	// check V6
	require.Nil(t, subnet.V6CIDR)
	// make sure subnet v6 fields length is 0
	// TODO:// v6 fields should be nil is better than empty list
	require.Equal(t, subnet.V6Free.Len(), 0)
	require.Equal(t, subnet.V6Reserved.Len(), 0)
	require.Equal(t, subnet.V6Available.Len(), 0)
	require.Equal(t, subnet.V6Using.Len(), 0)
	require.Len(t, subnet.V6NicToIP, 0)
	require.Len(t, subnet.V6IPToPod, 0)
	require.Len(t, subnet.PodToNicList, 0)
	// TODO: check pool
}

func TestNewSubnetIPv6(t *testing.T) {
	excludeIps := []string{"2001:db8::2", "2001:db8::4", "2001:db8::100", "2001:db8::252", "2001:db8::253", "2001:db8::254"}
	subnet, err := NewSubnet("v6Subnet", "2001:db8::/64", excludeIps)
	require.NoError(t, err)
	require.NotNil(t, subnet)
	// check V6
	require.NoError(t, err)
	require.NotNil(t, subnet)
	require.Equal(t, "v6Subnet", subnet.Name)
	require.Equal(t, apiv1.ProtocolIPv6, subnet.Protocol)
	require.NotNil(t, subnet.V6CIDR)
	require.Equal(t, "2001:db8::/64", subnet.V6CIDR.String())
	require.Empty(t, subnet.V4Gw)
	require.Empty(t, subnet.V6Gw)
	require.NotNil(t, subnet.V6Free)
	require.Equal(t, "2001:db8::1,2001:db8::3,2001:db8::5-2001:db8::ff,2001:db8::101-2001:db8::251,2001:db8::255-2001:db8::ffff:ffff:ffff:fffe", subnet.V6Free.String())
	require.NotNil(t, subnet.V6Reserved)
	require.Equal(t, "2001:db8::2,2001:db8::4,2001:db8::100,2001:db8::252-2001:db8::254", subnet.V6Reserved.String())
	require.NotNil(t, subnet.V6Available)
	require.Equal(t, "2001:db8::1,2001:db8::3,2001:db8::5-2001:db8::ff,2001:db8::101-2001:db8::251,2001:db8::255-2001:db8::ffff:ffff:ffff:fffe", subnet.V6Available.String())
	require.True(t, subnet.V6Available.Equal(subnet.V6Free))
	require.Equal(t, subnet.V6Using.Len(), 0)
	require.Len(t, subnet.V6NicToIP, 0)
	require.Len(t, subnet.V6IPToPod, 0)
	require.NotNil(t, subnet.V6Available)
	require.True(t, subnet.V6Available.Equal(subnet.V6Free))
	// check V4
	require.Nil(t, subnet.V4CIDR)
	// make sure subnet v4 fields length is 0
	// TODO:// v4 fields should be nil is better than empty list
	require.Equal(t, subnet.V4Free.Len(), 0)
	require.Equal(t, subnet.V4Reserved.Len(), 0)
	require.Equal(t, subnet.V4Available.Len(), 0)
	require.Equal(t, subnet.V4Using.Len(), 0)
	require.Len(t, subnet.V4NicToIP, 0)
	require.Len(t, subnet.V4IPToPod, 0)
	require.Len(t, subnet.PodToNicList, 0)
	// TODO: check pool
}

func TestNewSubnetDualStack(t *testing.T) {
	excludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	subnet, err := NewSubnet("dualSubnet", "10.0.0.0/24,2001:db8::/64", excludeIps)
	require.NoError(t, err)
	require.NotNil(t, subnet)
	require.Equal(t, "dualSubnet", subnet.Name)
	require.Equal(t, apiv1.ProtocolDual, subnet.Protocol)
	// check V4
	require.NotNil(t, subnet.V4CIDR)
	require.Equal(t, "10.0.0.0/24", subnet.V4CIDR.String())
	require.Empty(t, subnet.V4Gw)
	require.Empty(t, subnet.V6Gw)
	require.NotNil(t, subnet.V4Free)
	require.Equal(t, "10.0.0.1,10.0.0.3,10.0.0.5-10.0.0.99,10.0.0.101-10.0.0.251", subnet.V4Free.String())
	require.NotNil(t, subnet.V4Reserved)
	require.Equal(t, "10.0.0.2,10.0.0.4,10.0.0.100,10.0.0.252-10.0.0.254", subnet.V4Reserved.String())
	require.NotNil(t, subnet.V4Available)
	require.Equal(t, "10.0.0.1,10.0.0.3,10.0.0.5-10.0.0.99,10.0.0.101-10.0.0.251", subnet.V4Available.String())
	require.True(t, subnet.V4Available.Equal(subnet.V4Free))
	require.Equal(t, subnet.V4Using.Len(), 0)
	require.Len(t, subnet.V4NicToIP, 0)
	require.Len(t, subnet.V4IPToPod, 0)
	require.NotNil(t, subnet.V4Available)
	require.True(t, subnet.V4Available.Equal(subnet.V4Free))
	// check V6
	require.NotNil(t, subnet.V6CIDR)
	require.Equal(t, "2001:db8::/64", subnet.V6CIDR.String())
	require.NotNil(t, subnet.V6Free)
	require.Equal(t, "2001:db8::1,2001:db8::3,2001:db8::5-2001:db8::ff,2001:db8::101-2001:db8::251,2001:db8::255-2001:db8::ffff:ffff:ffff:fffe", subnet.V6Free.String())
	require.NotNil(t, subnet.V6Reserved)
	require.Equal(t, "2001:db8::2,2001:db8::4,2001:db8::100,2001:db8::252-2001:db8::254", subnet.V6Reserved.String())
	require.NotNil(t, subnet.V6Available)
	require.Equal(t, "2001:db8::1,2001:db8::3,2001:db8::5-2001:db8::ff,2001:db8::101-2001:db8::251,2001:db8::255-2001:db8::ffff:ffff:ffff:fffe", subnet.V6Available.String())
	require.True(t, subnet.V6Available.Equal(subnet.V6Free))
	require.Equal(t, subnet.V6Using.Len(), 0)
	require.Len(t, subnet.V6NicToIP, 0)
	require.Len(t, subnet.V6IPToPod, 0)
	require.NotNil(t, subnet.V6Available)
	require.True(t, subnet.V6Available.Equal(subnet.V6Free))
}

func TestGetV4StaticAddress(t *testing.T) {
	excludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
	}
	subnet, err := NewSubnet("v4Subnet", "10.0.0.0/24", excludeIps)
	require.NoError(t, err)
	require.NotNil(t, subnet)
	// 1. pod1 has v4 ip but no mac, should get specified ip and mac
	podName := "pod1.default"
	nicName := "pod1.default"
	var mac *string
	v4 := "10.0.0.3"
	v4IP, err := NewIP(v4)
	require.NoError(t, err)
	ip1, macStr1, err := subnet.GetStaticAddress(podName, nicName, v4IP, mac, false, true)
	require.NoError(t, err)
	require.Equal(t, v4, ip1.String())
	require.NotEmpty(t, macStr1)
	v4IP, v6IP, m, protocol := subnet.GetPodAddress(nicName)
	require.Equal(t, v4, v4IP.String())
	require.Nil(t, v6IP)
	require.Equal(t, macStr1, m)
	require.Equal(t, apiv1.ProtocolIPv4, protocol)

	// 3. pod2 has v4 ip and mac, should get specified ip and mac
	podName = "pod2.default"
	nicName = "pod2.default"
	v4 = "10.0.0.22"
	macIn := "00:11:22:33:44:55"
	v4IP, err = NewIP(v4)
	require.NoError(t, err)
	ip2, macOut2, err := subnet.GetStaticAddress(podName, nicName, v4IP, &macIn, false, true)
	require.NoError(t, err)
	require.Equal(t, v4, ip2.String())
	require.Equal(t, macIn, macOut2)
	v4IP, v6IP, m, protocol = subnet.GetPodAddress(nicName)
	require.Equal(t, v4, v4IP.String())
	require.Nil(t, v6IP)
	require.Equal(t, macOut2, m)
	require.NotEmpty(t, macStr1)
	require.Equal(t, apiv1.ProtocolIPv4, protocol)

	// compare mac
	require.NotEqual(t, macStr1, macOut2)

	// 3. pod3 only has mac, should get no ip and no mac
	podName = "pod3.default"
	nicName = "pod3.default"
	macIn = "00:11:22:33:44:57"
	ip, macOut, err := subnet.GetStaticAddress(podName, nicName, nil, &macIn, false, true)
	require.NotNil(nil, err)
	require.Nil(t, ip)
	require.Empty(t, macOut)

	// 4. pod4 has the same mac with pod2 should get error
	podName = "pod4.default"
	nicName = "pod4.default"
	v4 = "10.0.0.23"
	macIn = "00:11:22:33:44:55"
	v4IP, err = NewIP(v4)
	require.NoError(t, err)
	ip, macOut, err = subnet.GetStaticAddress(podName, nicName, v4IP, &macIn, false, true)
	require.NotNil(nil, err)
	require.Empty(t, macOut)
	require.Nil(nil, ip)

	// 5. ip is assigned to pod1, should get error
	podName = "pod5.default"
	v4 = "10.0.0.3"
	usingPod, using := subnet.isIPAssignedToOtherPod(v4, podName)
	require.True(t, using)
	require.Equal(t, "pod1.default", usingPod)
}

func TestGetStaticAddressRejectsGatewayIP(t *testing.T) {
	subnet, err := NewSubnet("v4Subnet", "10.0.0.0/24", nil)
	require.NoError(t, err)
	subnet.V4Gw = "10.0.0.1"

	ip, err := NewIP("10.0.0.1")
	require.NoError(t, err)

	allocatedIP, mac, err := subnet.GetStaticAddress("pod.default", "pod.default", ip, nil, false, true)
	require.ErrorIs(t, err, ErrConflict)
	require.Nil(t, allocatedIP)
	require.Empty(t, mac)
}

func TestGetV4StaticAddressPTP(t *testing.T) {
	excludeIps := []string{
		"10.0.0.0",
	}
	subnet, err := NewSubnet("v4Subnet", "10.0.0.0/31", excludeIps)
	require.NoError(t, err)
	require.NotNil(t, subnet)
	// 1. pod1 has v4 ip but no mac, should get specified ip and mac
	podName := "pod1.default"
	nicName := "pod1.default"
	var mac *string
	v4 := "10.0.0.1"
	v4IP, err := NewIP(v4)
	require.NoError(t, err)
	ip1, macStr1, err := subnet.GetStaticAddress(podName, nicName, v4IP, mac, false, true)
	require.NoError(t, err)
	require.Equal(t, v4, ip1.String())
	require.NotEmpty(t, macStr1)
	v4IP, v6IP, m, protocol := subnet.GetPodAddress(nicName)
	require.Equal(t, v4, v4IP.String())
	require.Nil(t, v6IP)
	require.Equal(t, macStr1, m)
	require.Equal(t, apiv1.ProtocolIPv4, protocol)

	// 2. ip is assigned to pod1, should get error
	podName = "pod5.default"
	v4 = "10.0.0.1"
	usingPod, using := subnet.isIPAssignedToOtherPod(v4, podName)
	require.True(t, using)
	require.Equal(t, "pod1.default", usingPod)
}

func TestGetV6StaticAddress(t *testing.T) {
	excludeIps := []string{
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	subnet, err := NewSubnet("v6Subnet", "2001:db8::/64", excludeIps)
	require.NoError(t, err)
	require.NotNil(t, subnet)
	// 1. pod1 has v6 ip but no mac, should get specified ip and mac
	podName := "pod1.default"
	nicName := "pod1.default"
	v6 := "2001:db8::3"
	v6IP, err := NewIP(v6)
	require.NoError(t, err)
	ip1, macStr1, err := subnet.GetStaticAddress(podName, nicName, v6IP, nil, false, true)
	require.NoError(t, err)
	require.Equal(t, v6, ip1.String())
	require.NotEmpty(t, macStr1)
	v4IP, v6IP, m, protocol := subnet.GetPodAddress(nicName)
	require.Nil(t, v4IP)
	require.Equal(t, v6, v6IP.String())
	require.Equal(t, macStr1, m)
	require.Equal(t, apiv1.ProtocolIPv6, protocol)

	// 2. pod2 has v6 ip and mac should get specified ip and mac
	podName = "pod2.default"
	nicName = "pod2.default"
	v6 = "2001:db8::5"
	macIn := "00:11:22:33:44:56"
	v6IP, err = NewIP(v6)
	require.NoError(t, err)
	ip2, macOut2, err := subnet.GetStaticAddress(podName, nicName, v6IP, &macIn, false, true)
	require.NoError(t, err)
	require.Equal(t, v6, ip2.String())
	require.Equal(t, macIn, macOut2)
	v4IP, v6IP, m, protocol = subnet.GetPodAddress(nicName)
	require.Nil(t, v4IP)
	require.Equal(t, v6, v6IP.String())
	require.Equal(t, macOut2, m)
	require.Equal(t, apiv1.ProtocolIPv6, protocol)

	// compare mac
	require.NotEqual(t, macStr1, macOut2)

	// 3. pod3 only has mac should get no ip and no mac
	podName = "pod3.default"
	nicName = "pod3.default"
	macIn = "00:11:22:33:44:57"
	ip, macOut, err := subnet.GetStaticAddress(podName, nicName, nil, &macIn, false, true)
	require.NotNil(nil, err)
	require.Nil(t, ip)
	require.Empty(t, macOut)

	// 4. pod4 has the same mac with pod2 should get error
	podName = "pod4.default"
	nicName = "pod4.default"
	v6 = "2001:db8::6"
	macIn = "00:11:22:33:44:56"
	v6IP, err = NewIP(v6)
	require.NoError(t, err)
	ip, macOut, err = subnet.GetStaticAddress(podName, nicName, v6IP, &macIn, false, true)
	require.NotNil(nil, err)
	require.Empty(t, macOut)
	require.Nil(nil, ip)

	// 5. ip is assigned to pod1, should get error
	podName = "pod5.default"
	v6 = "2001:db8::3"
	usingPod, using := subnet.isIPAssignedToOtherPod(v6, podName)
	require.True(t, using)
	require.Equal(t, "pod1.default", usingPod)
}

func TestGetDualStaticAddress(t *testing.T) {
	excludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	subnet, err := NewSubnet("dualSubnet1", "10.0.0.0/24,2001:db8::/64", excludeIps)
	require.NoError(t, err)
	require.NotNil(t, subnet)
	// 1. pod1 has v4 and v6 ip but no mac, should get specified ip and mac
	podName := "pod1.default"
	nicName := "pod1.default"
	var mac *string
	v4 := "10.0.0.3"
	v6 := "2001:db8::3"
	v4IP, err := NewIP(v4)
	require.NoError(t, err)
	v4Ip1, macStr1, err := subnet.GetStaticAddress(podName, nicName, v4IP, mac, false, true)
	require.NoError(t, err)
	require.Equal(t, v4, v4Ip1.String())
	require.NotEmpty(t, macStr1)
	v6IP, err := NewIP(v6)
	require.NoError(t, err)
	v6Ip1, macStr1, err := subnet.GetStaticAddress(podName, nicName, v6IP, mac, false, true)
	require.NoError(t, err)
	require.Equal(t, v6, v6Ip1.String())
	require.NotEmpty(t, macStr1)
	v4Ip, v6Ip, m, protocol := subnet.GetPodAddress(nicName)
	require.Equal(t, v4, v4Ip.String())
	require.Equal(t, v6, v6Ip.String())
	require.Equal(t, macStr1, m)
	require.Equal(t, apiv1.ProtocolDual, protocol)

	// 2. pod2 has v4 and v6 ip and mac should get specified mac
	podName = "pod2.default"
	nicName = "pod2.default"
	v4 = "10.0.0.22"
	v6 = "2001:db8::22"
	macIn := "00:11:22:33:44:55"
	v4IP, err = NewIP(v4)
	require.NoError(t, err)
	v4Ip2, macOut, err := subnet.GetStaticAddress(podName, nicName, v4IP, &macIn, false, true)
	require.NoError(t, err)
	require.Equal(t, v4, v4Ip2.String())
	require.Equal(t, macIn, macOut)
	v6IP, err = NewIP(v6)
	require.NoError(t, err)
	v6Ip2, macOut, err := subnet.GetStaticAddress(podName, nicName, v6IP, &macIn, false, true)
	require.NoError(t, err)
	require.Equal(t, v6, v6Ip2.String())
	require.Equal(t, macIn, macOut)
	v4Ip, v6Ip, m, protocol = subnet.GetPodAddress(nicName)
	require.Equal(t, v4, v4Ip.String())
	require.Equal(t, v6, v6Ip.String())
	require.Equal(t, macIn, m)
	require.Equal(t, apiv1.ProtocolDual, protocol)

	// compare mac
	require.NotEqual(t, macStr1, macOut)

	// 3. pod3 only has mac should get no ip and no mac
	podName = "pod3.default"
	nicName = "pod3.default"
	macIn = "00:11:22:33:44:57"
	ip, macOut, err := subnet.GetStaticAddress(podName, nicName, nil, &macIn, false, true)
	require.NotNil(nil, err)
	require.Nil(t, ip)
	require.Empty(t, macOut)

	// 4. pod4 has the same mac with pod3 should get error
	podName = "pod4.default"
	nicName = "pod4.default"
	v6 = "2001:db8::66"
	macIn = "00:11:22:33:44:55"
	v6IP, err = NewIP(v6)
	require.NoError(t, err)
	ip, macOut, err = subnet.GetStaticAddress(podName, nicName, v6IP, &macIn, false, true)
	require.NotNil(nil, err)
	require.Empty(t, macOut)
	require.Nil(nil, ip)

	// 5. ip is assigned to pod1, should get error
	podName = "pod5.default"
	v4 = "10.0.0.3"
	usingPod, using := subnet.isIPAssignedToOtherPod(v4, podName)
	require.True(t, using)
	require.Equal(t, "pod1.default", usingPod)
}

func TestGetV4RandomAddress(t *testing.T) {
	excludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
	}
	subnet, err := NewSubnet("randomAddressV4Subnet1", "10.0.0.0/24", excludeIps)
	require.NoError(t, err)
	require.NotNil(t, subnet)
	// 1. no mac, get v4 address for pod1
	podName := "pod1.default"
	nicName := "pod1.default"
	v4IP1, v6IP1, mac1, err := subnet.GetRandomAddress("", podName, nicName, nil, nil, false)
	require.NoError(t, err)
	require.NotEmpty(t, v4IP1.String())
	require.Nil(t, v6IP1)
	require.NotEmpty(t, mac1)
	// 2. has mac, get v4 address for pod2
	podName = "pod2.default"
	nicName = "pod2.default"
	staticMac2 := "00:11:22:33:44:55"
	v4IP2, v6IP2, mac2, err := subnet.GetRandomAddress("", podName, nicName, &staticMac2, nil, false)
	require.NoError(t, err)
	require.NotEmpty(t, v4IP2.String())
	require.Nil(t, v6IP2)
	require.Equal(t, staticMac2, mac2)

	// compare
	require.NotEqual(t, v4IP1.String(), v4IP2.String())
	require.NotEqual(t, mac1, mac2)
}

func TestGetV4RandomAddressPTP(t *testing.T) {
	excludeIps := []string{
		"10.0.0.0",
	}
	subnet, err := NewSubnet("randomAddressV4Subnet1", "10.0.0.0/31", excludeIps)
	require.NoError(t, err)
	require.NotNil(t, subnet)
	// 1. no mac, get v4 address for pod1
	podName := "pod1.default"
	nicName := "pod1.default"
	v4IP1, v6IP1, mac1, err := subnet.GetRandomAddress("", podName, nicName, nil, nil, false)
	require.NoError(t, err)
	require.NotEmpty(t, v4IP1.String())
	require.Nil(t, v6IP1)
	require.NotEmpty(t, mac1)

	// 2. ip is assigned to pod1, should get error
	podName = "pod5.default"
	v4 := "10.0.0.1"
	usingPod, using := subnet.isIPAssignedToOtherPod(v4, podName)
	require.True(t, using)
	require.Equal(t, "pod1.default", usingPod)
}

func TestGetV6RandomAddress(t *testing.T) {
	excludeIps := []string{
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	subnet, err := NewSubnet("v6Subnet", "2001:db8::/64", excludeIps)
	require.NoError(t, err)
	require.NotNil(t, subnet)
	// 1. no mac, get v6 address for pod1
	podName := "pod1.default"
	nicName := "pod1.default"
	v4IP1, v6IP1, mac1, err := subnet.GetRandomAddress("", podName, nicName, nil, nil, false)
	require.NoError(t, err)
	require.Nil(t, v4IP1)
	require.NotEmpty(t, v6IP1.String())
	require.NotEmpty(t, mac1)
	// 2. has mac, get v6 address for pod2
	podName = "pod2.default"
	nicName = "pod2.default"
	staticMac2 := "00:11:22:33:44:55"
	v4IP2, v6IP2, mac2, err := subnet.GetRandomAddress("", podName, nicName, &staticMac2, nil, false)
	require.NoError(t, err)
	require.Nil(t, v4IP2)
	require.NotEmpty(t, v6IP2.String())
	require.Equal(t, staticMac2, mac2)
	// compare
	require.NotEqual(t, v6IP1.String(), v6IP2.String())
	require.NotEqual(t, mac1, mac2)
}

func TestGetRandomDualStackAddress(t *testing.T) {
	excludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	subnet, err := NewSubnet("dualSubnet", "10.0.0.0/24,2001:db8::/64", excludeIps)
	require.NoError(t, err)
	require.NotNil(t, subnet)
	// 1. no mac, get v4, v6 address for pod1
	podName := "pod1.default"
	nicName := "pod1.default"
	poolName := ""
	skippedAddrs := []string{"10.0.0.1", "10.0.0.5", "2001:db8::1", "2001:db8::5"}
	v4IP1, v6IP1, mac1, err := subnet.GetRandomAddress(poolName, podName, nicName, nil, skippedAddrs, true)
	require.NoError(t, err)
	require.NotEmpty(t, v4IP1.String())
	require.NotEmpty(t, v6IP1.String())
	require.NotEmpty(t, mac1)
	// 2. has mac, get v4, v6 address for pod1
	podName = "pod2.default"
	nicName = "pod2.default"
	staticMac2 := "00:11:22:33:44:55"
	v4IP2, v6IP2, mac2, err := subnet.GetRandomAddress(poolName, podName, nicName, &staticMac2, skippedAddrs, true)
	require.NoError(t, err)
	require.NotEmpty(t, v4IP2.String())
	require.NotEmpty(t, v6IP2.String())
	require.Equal(t, staticMac2, mac2)
	// compare
	require.NotEqual(t, v4IP1.String(), v4IP2.String())
	require.NotEqual(t, v6IP1.String(), v6IP2.String())
	require.NotEqual(t, mac1, mac2)
}

func TestReleaseAddrForV4Subnet(t *testing.T) {
	excludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
	}
	subnet, err := NewSubnet("testV4ReleasedSubnet", "10.0.0.0/24", excludeIps)
	require.NoError(t, err)
	require.NotNil(t, subnet)
	// 1. pod1 get random v4 address
	podName := "pod1.default"
	nicName := "pod1.default"
	poolName := ""
	v4, _, _, err := subnet.GetRandomAddress(poolName, podName, nicName, nil, nil, false)
	require.NoError(t, err)
	require.Equal(t, subnet.V4Using.String(), v4.String())
	require.True(t, subnet.ContainAddress(v4))
	// pod1 release random v4 address
	subnet.ReleaseAddress(podName)
	require.Empty(t, subnet.V4Using.String())
	require.False(t, subnet.ContainAddress(v4))
	// 2. pod2 get random v4 address
	podName = "pod2.default"
	nicName = "pod2.default"
	v4, _, _, err = subnet.GetRandomAddress(poolName, podName, nicName, nil, nil, false)
	require.NoError(t, err)
	require.Equal(t, subnet.V4Using.String(), v4.String())
	require.True(t, subnet.ContainAddress(v4))
	// pod2 release random v4 address
	subnet.ReleaseAddressWithNicName(podName, nicName)
	require.Empty(t, subnet.V4Using.String())
	require.False(t, subnet.ContainAddress(v4))
}

func TestReleaseV6SubnetAddrForV6Subnet(t *testing.T) {
	excludeIps := []string{"2001:db8::2", "2001:db8::4", "2001:db8::100", "2001:db8::252", "2001:db8::253", "2001:db8::254"}
	subnet, err := NewSubnet("testV6ReleasedSubnet", "2001:db8::/64", excludeIps)
	require.NoError(t, err)
	require.NotNil(t, subnet)
	// 1. pod1 get random v6 address
	podName := "pod1.default"
	nicName := "pod1.default"
	poolName := ""
	_, v6, _, err := subnet.GetRandomAddress(poolName, podName, nicName, nil, nil, false)
	require.NoError(t, err)
	require.Equal(t, subnet.V6Using.String(), v6.String())
	require.True(t, subnet.ContainAddress(v6))
	// pod1 release random v6 address
	subnet.ReleaseAddress(podName)
	require.Empty(t, subnet.V6Using.String())
	require.False(t, subnet.ContainAddress(v6))
	// 2. pod2 get random v6 address
	podName = "pod2.default"
	nicName = "pod2.default"
	_, v6, _, err = subnet.GetRandomAddress("", podName, nicName, nil, nil, false)
	require.NoError(t, err)
	require.Equal(t, subnet.V6Using.String(), v6.String())
	require.True(t, subnet.ContainAddress(v6))
	// pod2 release random v6 address
	subnet.ReleaseAddressWithNicName(podName, nicName)
	require.Empty(t, subnet.V6Using.String())
	require.False(t, subnet.ContainAddress(v6))
}

func TestReleaseAddrForDualSubnet(t *testing.T) {
	excludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	subnet, err := NewSubnet("testDualReleasedSubnet", "10.0.0.0/24,2001:db8::/64", excludeIps)
	require.NoError(t, err)
	require.NotNil(t, subnet)
	// 1. pod1 get random v4, v6 address
	podName := "pod1.default"
	nicName := "pod1.default"
	poolName := ""
	v4, v6, _, err := subnet.GetRandomAddress(poolName, podName, nicName, nil, nil, true)
	require.NoError(t, err)
	require.Equal(t, subnet.V4Using.String(), v4.String())
	require.True(t, subnet.ContainAddress(v4))
	require.Equal(t, subnet.V6Using.String(), v6.String())
	require.True(t, subnet.ContainAddress(v6))
	// pod1 release random v4, v6 address
	subnet.ReleaseAddress(podName)
	require.Empty(t, subnet.V4Using.String())
	require.Empty(t, subnet.V6Using.String())
	require.False(t, subnet.ContainAddress(v4))
	require.False(t, subnet.ContainAddress(v6))
	// 2. pod1 get random v4, v6 address
	podName = "pod2.default"
	nicName = "pod2.default"
	v4, v6, _, err = subnet.GetRandomAddress("", podName, nicName, nil, nil, true)
	require.NoError(t, err)
	require.Equal(t, subnet.V4Using.String(), v4.String())
	require.True(t, subnet.ContainAddress(v4))
	require.Equal(t, subnet.V6Using.String(), v6.String())
	require.True(t, subnet.ContainAddress(v6))
	// pod1 release random v4, v6 address
	subnet.ReleaseAddressWithNicName(podName, nicName)
	require.Empty(t, subnet.V4Using.String())
	require.Empty(t, subnet.V6Using.String())
	require.False(t, subnet.ContainAddress(v4))
	require.False(t, subnet.ContainAddress(v6))
}

func TestSubnetAddOrUpdateIPPool(t *testing.T) {
	excludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	subnetName := "dualSubnet"
	subnet, err := NewSubnet(subnetName, "10.0.0.0/16,2001:db8::/64", excludeIps)
	require.NoError(t, err)
	// check default pool
	defaultPool := subnet.IPPools[""]
	require.NotNil(t, defaultPool)
	require.NotNil(t, defaultPool.V4IPs)
	require.NotNil(t, defaultPool.V6IPs)
	require.NotNil(t, defaultPool.V4Free)
	require.NotNil(t, defaultPool.V6Free)
	require.NotNil(t, defaultPool.V4Available)
	require.NotNil(t, defaultPool.V6Available)
	require.NotNil(t, defaultPool.V4Reserved)
	require.NotNil(t, defaultPool.V6Reserved)
	require.NotNil(t, defaultPool.V4IPs)
	require.NotNil(t, defaultPool.V4IPs)
	require.NotNil(t, defaultPool.V4Released)
	require.NotNil(t, defaultPool.V6Released)
	require.NotNil(t, defaultPool.V4Using)
	require.NotNil(t, defaultPool.V6Using)
	require.Equal(t, defaultPool.V4IPs.String(), "10.0.0.1-10.0.255.254")
	require.Equal(t, defaultPool.V6IPs.String(), "2001:db8::1-2001:db8::ffff:ffff:ffff:fffe")
	require.Equal(t, defaultPool.V4Free.String(), "10.0.0.1,10.0.0.3,10.0.0.5-10.0.0.99,10.0.0.101-10.0.0.251,10.0.0.255-10.0.255.254")
	require.Equal(t, defaultPool.V6Free.String(), "2001:db8::1,2001:db8::3,2001:db8::5-2001:db8::ff,2001:db8::101-2001:db8::251,2001:db8::255-2001:db8::ffff:ffff:ffff:fffe")
	require.Equal(t, defaultPool.V4Available.String(), "10.0.0.1,10.0.0.3,10.0.0.5-10.0.0.99,10.0.0.101-10.0.0.251,10.0.0.255-10.0.255.254")
	require.Equal(t, defaultPool.V6Available.String(), "2001:db8::1,2001:db8::3,2001:db8::5-2001:db8::ff,2001:db8::101-2001:db8::251,2001:db8::255-2001:db8::ffff:ffff:ffff:fffe")
	require.Equal(t, defaultPool.V4Reserved.String(), "10.0.0.2,10.0.0.4,10.0.0.100,10.0.0.252-10.0.0.254")
	require.Equal(t, defaultPool.V6Reserved.String(), "2001:db8::2,2001:db8::4,2001:db8::100,2001:db8::252-2001:db8::254")
	require.Equal(t, defaultPool.V4Released.String(), "")
	require.Equal(t, defaultPool.V6Released.String(), "")
	require.Equal(t, defaultPool.V4Using.String(), "")
	require.Equal(t, defaultPool.V6Using.String(), "")

	// check V4 valid pool
	v4ValidPoolName := "v4ValidPool"
	validV4IPs := []string{"10.0.0.20", "10.0.0.90", "10.0.0.170", "10.0.0.240", "10.0.0.250"}
	err = subnet.AddOrUpdateIPPool(v4ValidPoolName, validV4IPs)
	require.NoError(t, err)
	require.NotNil(t, subnet.IPPools[v4ValidPoolName])
	v4ValidPool, ok := subnet.IPPools[v4ValidPoolName]
	require.True(t, ok)
	require.NotNil(t, v4ValidPool)
	require.NotNil(t, v4ValidPool.V4IPs)
	require.NotNil(t, v4ValidPool.V6IPs)
	require.NotNil(t, v4ValidPool.V4Free)
	require.NotNil(t, v4ValidPool.V6Free)
	require.NotNil(t, v4ValidPool.V4Available)
	require.NotNil(t, v4ValidPool.V6Available)
	require.NotNil(t, v4ValidPool.V4Reserved)
	require.NotNil(t, v4ValidPool.V6Reserved)
	require.NotNil(t, v4ValidPool.V4IPs)
	require.NotNil(t, v4ValidPool.V6IPs)
	require.NotNil(t, v4ValidPool.V4Released)
	require.NotNil(t, v4ValidPool.V6Released)
	require.NotNil(t, v4ValidPool.V4Using)
	require.NotNil(t, v4ValidPool.V6Using)

	require.Equal(t, v4ValidPool.V4IPs.String(), "10.0.0.20,10.0.0.90,10.0.0.170,10.0.0.240,10.0.0.250")
	require.Equal(t, v4ValidPool.V6IPs.String(), "")
	require.Equal(t, v4ValidPool.V4Free.String(), "10.0.0.20,10.0.0.90,10.0.0.170,10.0.0.240,10.0.0.250")
	require.Equal(t, v4ValidPool.V6Free.String(), "")
	require.Equal(t, v4ValidPool.V4Available.String(), "10.0.0.20,10.0.0.90,10.0.0.170,10.0.0.240,10.0.0.250")
	require.Equal(t, v4ValidPool.V6Available.String(), "")
	require.Equal(t, v4ValidPool.V4Reserved.String(), "")
	require.Equal(t, v4ValidPool.V6Reserved.String(), "")
	require.Equal(t, v4ValidPool.V4Released.String(), "")
	require.Equal(t, v4ValidPool.V6Released.String(), "")
	require.Equal(t, v4ValidPool.V4Using.String(), "")
	require.Equal(t, v4ValidPool.V6Using.String(), "")

	// check V4 invalid pool
	v4InvalidPoolName := "v4InvalidPool"
	invalidV4IPs := []string{"10.0.0.21", "10.0.0.9", "10.0.0.17", "10.0.0.241", "10.0.0.261"}
	err = subnet.AddOrUpdateIPPool(v4InvalidPoolName, invalidV4IPs)
	require.Error(t, err)
	require.Nil(t, subnet.IPPools[v4InvalidPoolName])

	// check V4 different pool has the same ip
	v4ConflictPoolName := "v4ConflictPool"
	conflictV4IPs := []string{"10.0.0.20", "10.0.0.92", "10.0.0.172", "10.0.0.242"}
	err = subnet.AddOrUpdateIPPool(v4ConflictPoolName, conflictV4IPs)
	require.Error(t, err)
	require.Nil(t, subnet.IPPools[v4ConflictPoolName])

	// check V6 valid pool
	v6ValidPoolName := "v6ValidPool"
	validV6IPs := []string{"2001:db8::20", "2001:db8::90", "2001:db8::170", "2001:db8::240", "2001:db8::250"}
	err = subnet.AddOrUpdateIPPool(v6ValidPoolName, validV6IPs)
	require.NoError(t, err)
	require.NotNil(t, subnet.IPPools[v6ValidPoolName])
	v6ValidPool, ok := subnet.IPPools[v6ValidPoolName]
	require.True(t, ok)
	require.NotNil(t, v6ValidPool)
	require.NotNil(t, v6ValidPool.V4IPs)
	require.NotNil(t, v6ValidPool.V6IPs)
	require.NotNil(t, v6ValidPool.V4Free)
	require.NotNil(t, v6ValidPool.V6Free)
	require.NotNil(t, v6ValidPool.V4Available)
	require.NotNil(t, v6ValidPool.V6Available)
	require.NotNil(t, v6ValidPool.V4Reserved)
	require.NotNil(t, v6ValidPool.V4Reserved)
	require.NotNil(t, v6ValidPool.V6Reserved)
	require.NotNil(t, v6ValidPool.V4IPs)
	require.NotNil(t, v6ValidPool.V4IPs)
	require.NotNil(t, v6ValidPool.V4Released)
	require.NotNil(t, v6ValidPool.V6Released)
	require.NotNil(t, v6ValidPool.V4Using)
	require.NotNil(t, v6ValidPool.V6Using)

	require.Equal(t, v6ValidPool.V4IPs.String(), "")
	require.Equal(t, v6ValidPool.V6IPs.String(), "2001:db8::20,2001:db8::90,2001:db8::170,2001:db8::240,2001:db8::250")
	require.Equal(t, v6ValidPool.V4Free.String(), "")
	require.Equal(t, v6ValidPool.V6Free.String(), "2001:db8::20,2001:db8::90,2001:db8::170,2001:db8::240,2001:db8::250")
	require.Equal(t, v6ValidPool.V4Available.String(), "")
	require.Equal(t, v6ValidPool.V6Available.String(), "2001:db8::20,2001:db8::90,2001:db8::170,2001:db8::240,2001:db8::250")
	require.Equal(t, v6ValidPool.V4Reserved.String(), "")
	require.Equal(t, v6ValidPool.V6Reserved.String(), "")
	require.Equal(t, v6ValidPool.V4Released.String(), "")
	require.Equal(t, v6ValidPool.V6Released.String(), "")
	require.Equal(t, v6ValidPool.V4Using.String(), "")
	require.Equal(t, v6ValidPool.V6Using.String(), "")

	// check V6 invalid pool
	v6InvalidPoolName := "v6InvalidPool"
	invalidV6IPs := []string{"2001:db8::21", "2001:db8::9", "2001:db8::17", "2001:db8::241", "2001:db8::g61"}
	err = subnet.AddOrUpdateIPPool(v6InvalidPoolName, invalidV6IPs)
	require.Error(t, err)
	require.Nil(t, subnet.IPPools[v6InvalidPoolName])

	// check V6 different pool has the same ip
	v6ConflictPoolName := "v6ConflictPool"
	conflictV6IPs := []string{"2001:db8::20", "2001:db8::92", "2001:db8::172", "2001:db8::242"}
	err = subnet.AddOrUpdateIPPool(v6ConflictPoolName, conflictV6IPs)
	require.Error(t, err)
	require.Nil(t, subnet.IPPools[v6ConflictPoolName])

	// check dualstack valid pool
	dualValidPoolName := "dualValidPool"
	validDualIPs := []string{"10.0.0.30", "10.0.0.80", "2001:db8::30", "2001:db8::80"}
	err = subnet.AddOrUpdateIPPool(dualValidPoolName, validDualIPs)
	require.NoError(t, err)
	require.NotNil(t, subnet.IPPools[dualValidPoolName])
	dualValidPool, ok := subnet.IPPools[dualValidPoolName]
	require.True(t, ok)
	require.NotNil(t, dualValidPool)
	require.NotNil(t, dualValidPool.V4IPs)
	require.NotNil(t, dualValidPool.V6IPs)
	require.NotNil(t, dualValidPool.V4Free)
	require.NotNil(t, dualValidPool.V6Free)
	require.NotNil(t, dualValidPool.V4Available)
	require.NotNil(t, dualValidPool.V6Available)
	require.NotNil(t, dualValidPool.V4Reserved)
	require.NotNil(t, dualValidPool.V6Reserved)
	require.NotNil(t, dualValidPool.V4IPs)
	require.NotNil(t, dualValidPool.V4IPs)
	require.NotNil(t, dualValidPool.V4Released)
	require.NotNil(t, dualValidPool.V6Released)
	require.NotNil(t, dualValidPool.V4Using)
	require.NotNil(t, dualValidPool.V6Using)

	require.Equal(t, dualValidPool.V4IPs.String(), "10.0.0.30,10.0.0.80")
	require.Equal(t, dualValidPool.V6IPs.String(), "2001:db8::30,2001:db8::80")
	require.Equal(t, dualValidPool.V4Free.String(), "10.0.0.30,10.0.0.80")
	require.Equal(t, dualValidPool.V6Free.String(), "2001:db8::30,2001:db8::80")
	require.Equal(t, dualValidPool.V4Available.String(), "10.0.0.30,10.0.0.80")
	require.Equal(t, dualValidPool.V6Available.String(), "2001:db8::30,2001:db8::80")
	require.Equal(t, dualValidPool.V4Reserved.String(), "")
	require.Equal(t, dualValidPool.V6Reserved.String(), "")
	require.Equal(t, dualValidPool.V4Released.String(), "")
	require.Equal(t, dualValidPool.V6Released.String(), "")
	require.Equal(t, dualValidPool.V4Using.String(), "")
	require.Equal(t, dualValidPool.V6Using.String(), "")

	// check dualstack invalid pool
	dualInvalidPoolName := "dualInvalidPool"
	invalidDualIPs := []string{"10.0.0.31", "10.0.0.256", "2001:db8::31", "2001:db8::79"}
	err = subnet.AddOrUpdateIPPool(dualInvalidPoolName, invalidDualIPs)
	require.Error(t, err)
	require.Nil(t, subnet.IPPools[dualInvalidPoolName])
	invalidDualIPs = []string{"10.0.0.31", "10.0.0.25", "2001:db8::31", "2001:db8::g9"}
	err = subnet.AddOrUpdateIPPool(dualInvalidPoolName, invalidDualIPs)
	require.Error(t, err)
	require.Nil(t, subnet.IPPools[dualInvalidPoolName])

	// check dualstack different pool has the same ip
	dualConflictPoolName := "dualConflictPool"
	conflictDualIPs := []string{"10.0.0.30", "10.0.0.92", "2001:db8::35", "2001:db8::92"}
	err = subnet.AddOrUpdateIPPool(dualConflictPoolName, conflictDualIPs)
	require.Error(t, err)
	require.Nil(t, subnet.IPPools[dualConflictPoolName])
	conflictDualIPs = []string{"10.0.0.30", "10.0.0.93", "2001:db8::30", "2001:db8::92"}
	err = subnet.AddOrUpdateIPPool(dualConflictPoolName, conflictDualIPs)
	require.Error(t, err)
	require.Nil(t, subnet.IPPools[dualConflictPoolName])

	// re check default pool
	defaultPool = subnet.IPPools[""]
	require.NotNil(t, defaultPool)
	require.NotNil(t, defaultPool.V4IPs)
	require.NotNil(t, defaultPool.V6IPs)
	require.NotNil(t, defaultPool.V4Free)
	require.NotNil(t, defaultPool.V6Free)
	require.NotNil(t, defaultPool.V4Available)
	require.NotNil(t, defaultPool.V6Available)
	require.NotNil(t, defaultPool.V4Reserved)
	require.NotNil(t, defaultPool.V6Reserved)
	require.NotNil(t, defaultPool.V4IPs)
	require.NotNil(t, defaultPool.V4IPs)
	require.NotNil(t, defaultPool.V4Released)
	require.NotNil(t, defaultPool.V6Released)
	require.NotNil(t, defaultPool.V4Using)
	require.NotNil(t, defaultPool.V6Using)

	require.Equal(t, defaultPool.V4IPs.String(), "10.0.0.1-10.0.0.19,10.0.0.21-10.0.0.29,10.0.0.31-10.0.0.79,10.0.0.81-10.0.0.89,10.0.0.91-10.0.0.169,10.0.0.171-10.0.0.239,10.0.0.241-10.0.0.249,10.0.0.251-10.0.255.254")
	require.Equal(t, defaultPool.V6IPs.String(), "2001:db8::1-2001:db8::1f,2001:db8::21-2001:db8::2f,2001:db8::31-2001:db8::7f,2001:db8::81-2001:db8::8f,2001:db8::91-2001:db8::16f,2001:db8::171-2001:db8::23f,2001:db8::241-2001:db8::24f,2001:db8::251-2001:db8::ffff:ffff:ffff:fffe")
	require.Equal(t, defaultPool.V4Free.String(), "10.0.0.1,10.0.0.3,10.0.0.5-10.0.0.19,10.0.0.21-10.0.0.29,10.0.0.31-10.0.0.79,10.0.0.81-10.0.0.89,10.0.0.91-10.0.0.99,10.0.0.101-10.0.0.169,10.0.0.171-10.0.0.239,10.0.0.241-10.0.0.249,10.0.0.251,10.0.0.255-10.0.255.254")
	require.Equal(t, defaultPool.V6Free.String(), "2001:db8::1,2001:db8::3,2001:db8::5-2001:db8::1f,2001:db8::21-2001:db8::2f,2001:db8::31-2001:db8::7f,2001:db8::81-2001:db8::8f,2001:db8::91-2001:db8::ff,2001:db8::101-2001:db8::16f,2001:db8::171-2001:db8::23f,2001:db8::241-2001:db8::24f,2001:db8::251,2001:db8::255-2001:db8::ffff:ffff:ffff:fffe")
	require.Equal(t, defaultPool.V4Available.String(), "10.0.0.1,10.0.0.3,10.0.0.5-10.0.0.19,10.0.0.21-10.0.0.29,10.0.0.31-10.0.0.79,10.0.0.81-10.0.0.89,10.0.0.91-10.0.0.99,10.0.0.101-10.0.0.169,10.0.0.171-10.0.0.239,10.0.0.241-10.0.0.249,10.0.0.251,10.0.0.255-10.0.255.254")
	require.Equal(t, defaultPool.V6Available.String(), "2001:db8::1,2001:db8::3,2001:db8::5-2001:db8::1f,2001:db8::21-2001:db8::2f,2001:db8::31-2001:db8::7f,2001:db8::81-2001:db8::8f,2001:db8::91-2001:db8::ff,2001:db8::101-2001:db8::16f,2001:db8::171-2001:db8::23f,2001:db8::241-2001:db8::24f,2001:db8::251,2001:db8::255-2001:db8::ffff:ffff:ffff:fffe")
	require.Equal(t, defaultPool.V4Reserved.String(), "10.0.0.2,10.0.0.4,10.0.0.100,10.0.0.252-10.0.0.254")
	require.Equal(t, defaultPool.V6Reserved.String(), "2001:db8::2,2001:db8::4,2001:db8::100,2001:db8::252-2001:db8::254")
	require.Equal(t, defaultPool.V4Released.String(), "")
	require.Equal(t, defaultPool.V6Released.String(), "")
	require.Equal(t, defaultPool.V4Using.String(), "")
	require.Equal(t, defaultPool.V6Using.String(), "")
}

func TestSubnetRemoveIPPool(t *testing.T) {
	excludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	subnetName := "dualSubnet"
	subnet, err := NewSubnet(subnetName, "10.0.0.0/16,2001:db8::/64", excludeIps)
	require.NoError(t, err)
	// check default pool
	defaultPool := subnet.IPPools[""]
	require.NotNil(t, defaultPool)
	require.NotNil(t, defaultPool.V4IPs)
	require.NotNil(t, defaultPool.V6IPs)
	require.NotNil(t, defaultPool.V4Free)
	require.NotNil(t, defaultPool.V6Free)
	require.NotNil(t, defaultPool.V4Available)
	require.NotNil(t, defaultPool.V6Available)
	require.NotNil(t, defaultPool.V4Reserved)
	require.NotNil(t, defaultPool.V6Reserved)
	require.NotNil(t, defaultPool.V4IPs)
	require.NotNil(t, defaultPool.V4IPs)
	require.NotNil(t, defaultPool.V4Released)
	require.NotNil(t, defaultPool.V6Released)
	require.NotNil(t, defaultPool.V4Using)
	require.NotNil(t, defaultPool.V6Using)
	require.Equal(t, defaultPool.V4IPs.String(), "10.0.0.1-10.0.255.254")
	require.Equal(t, defaultPool.V6IPs.String(), "2001:db8::1-2001:db8::ffff:ffff:ffff:fffe")
	require.Equal(t, defaultPool.V4Free.String(), "10.0.0.1,10.0.0.3,10.0.0.5-10.0.0.99,10.0.0.101-10.0.0.251,10.0.0.255-10.0.255.254")
	require.Equal(t, defaultPool.V6Free.String(), "2001:db8::1,2001:db8::3,2001:db8::5-2001:db8::ff,2001:db8::101-2001:db8::251,2001:db8::255-2001:db8::ffff:ffff:ffff:fffe")
	require.Equal(t, defaultPool.V4Available.String(), "10.0.0.1,10.0.0.3,10.0.0.5-10.0.0.99,10.0.0.101-10.0.0.251,10.0.0.255-10.0.255.254")
	require.Equal(t, defaultPool.V6Available.String(), "2001:db8::1,2001:db8::3,2001:db8::5-2001:db8::ff,2001:db8::101-2001:db8::251,2001:db8::255-2001:db8::ffff:ffff:ffff:fffe")
	require.Equal(t, defaultPool.V4Reserved.String(), "10.0.0.2,10.0.0.4,10.0.0.100,10.0.0.252-10.0.0.254")
	require.Equal(t, defaultPool.V6Reserved.String(), "2001:db8::2,2001:db8::4,2001:db8::100,2001:db8::252-2001:db8::254")
	require.Equal(t, defaultPool.V4Released.String(), "")
	require.Equal(t, defaultPool.V6Released.String(), "")
	require.Equal(t, defaultPool.V4Using.String(), "")
	require.Equal(t, defaultPool.V6Using.String(), "")
	// check dualstack valid pool
	dualValidPoolName := "dualValidPool"
	validDualIPs := []string{"10.0.0.30", "10.0.0.80", "2001:db8::30", "2001:db8::80"}
	err = subnet.AddOrUpdateIPPool(dualValidPoolName, validDualIPs)
	require.NoError(t, err)
	require.NotNil(t, subnet.IPPools[dualValidPoolName])
	_, ok := subnet.IPPools[dualValidPoolName]
	require.True(t, ok)
	require.Equal(t, 2, len(subnet.IPPools))
	// remove dualValidPool
	subnet.RemoveIPPool(dualValidPoolName)
	require.Nil(t, subnet.IPPools[dualValidPoolName])
	require.Equal(t, 1, len(subnet.IPPools))
	// recheck default pool
	defaultPool = subnet.IPPools[""]
	require.NotNil(t, defaultPool)
	require.NotNil(t, defaultPool.V4IPs)
	require.NotNil(t, defaultPool.V6IPs)
	require.NotNil(t, defaultPool.V4Free)
	require.NotNil(t, defaultPool.V6Free)
	require.NotNil(t, defaultPool.V4Available)
	require.NotNil(t, defaultPool.V6Available)
	require.NotNil(t, defaultPool.V4Reserved)
	require.NotNil(t, defaultPool.V6Reserved)
	require.NotNil(t, defaultPool.V4IPs)
	require.NotNil(t, defaultPool.V4IPs)
	require.NotNil(t, defaultPool.V4Released)
	require.NotNil(t, defaultPool.V6Released)
	require.NotNil(t, defaultPool.V4Using)
	require.NotNil(t, defaultPool.V6Using)
	require.Equal(t, defaultPool.V4IPs.String(), "10.0.0.1-10.0.0.29,10.0.0.31-10.0.0.79,10.0.0.81-10.0.255.254")
	require.Equal(t, defaultPool.V6IPs.String(), "2001:db8::1-2001:db8::2f,2001:db8::31-2001:db8::7f,2001:db8::81-2001:db8::ffff:ffff:ffff:fffe")
	require.Equal(t, defaultPool.V4Free.String(), "10.0.0.1,10.0.0.3,10.0.0.5-10.0.0.99,10.0.0.101-10.0.0.251,10.0.0.255-10.0.255.254")
	require.Equal(t, defaultPool.V6Free.String(), "2001:db8::1,2001:db8::3,2001:db8::5-2001:db8::ff,2001:db8::101-2001:db8::251,2001:db8::255-2001:db8::ffff:ffff:ffff:fffe")
	require.Equal(t, defaultPool.V4Available.String(), "10.0.0.1,10.0.0.3,10.0.0.5-10.0.0.99,10.0.0.101-10.0.0.251,10.0.0.255-10.0.255.254")
	require.Equal(t, defaultPool.V6Available.String(), "2001:db8::1,2001:db8::3,2001:db8::5-2001:db8::ff,2001:db8::101-2001:db8::251,2001:db8::255-2001:db8::ffff:ffff:ffff:fffe")
	require.Equal(t, defaultPool.V4Reserved.String(), "10.0.0.2,10.0.0.4,10.0.0.100,10.0.0.252-10.0.0.254")
	require.Equal(t, defaultPool.V6Reserved.String(), "2001:db8::2,2001:db8::4,2001:db8::100,2001:db8::252-2001:db8::254")
	require.Equal(t, defaultPool.V4Released.String(), "")
	require.Equal(t, defaultPool.V6Released.String(), "")
	require.Equal(t, defaultPool.V4Using.String(), "")
	require.Equal(t, defaultPool.V6Using.String(), "")

	// remove dualValidPool
	subnet.RemoveIPPool(dualValidPoolName)
	require.Nil(t, subnet.IPPools[dualValidPoolName])
	require.Equal(t, 1, len(subnet.IPPools))
}

func TestSubnetIPPoolStatistics(t *testing.T) {
	excludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	subnetName := "dualSubnet"
	subnet, err := NewSubnet(subnetName, "10.0.0.0/16,2001:db8::/64", excludeIps)
	require.NoError(t, err)

	// check V4 valid pool
	v4ValidPoolName := "v4ValidPool"
	validV4IPs := []string{"10.0.0.20", "10.0.0.90", "10.0.0.170", "10.0.0.240", "10.0.0.250"}
	err = subnet.AddOrUpdateIPPool(v4ValidPoolName, validV4IPs)
	require.NoError(t, err)
	v4a, v4u, v6a, v6u, v4as, v4us, v6as, v6us := subnet.IPPoolStatistics(v4ValidPoolName)
	require.Equal(t, v4a.String(), "5")
	require.Empty(t, v4u)
	require.Empty(t, v6a)
	require.Empty(t, v6u)
	require.Equal(t, v4as, "10.0.0.20,10.0.0.90,10.0.0.170,10.0.0.240,10.0.0.250")
	require.Empty(t, v4us)
	require.Empty(t, v6as)
	require.Empty(t, v6us)

	// check V6 valid pool
	v6ValidPoolName := "v6ValidPool"
	validV6IPs := []string{"2001:db8::20", "2001:db8::90", "2001:db8::170", "2001:db8::240", "2001:db8::250"}
	err = subnet.AddOrUpdateIPPool(v6ValidPoolName, validV6IPs)
	require.NoError(t, err)
	v4a, v4u, v6a, v6u, v4as, v4us, v6as, v6us = subnet.IPPoolStatistics(v6ValidPoolName)
	require.Empty(t, v4a)
	require.Empty(t, v4u)
	require.Equal(t, v6a.String(), "5")
	require.Empty(t, v6u)
	require.Empty(t, v4as)
	require.Empty(t, v4us)
	require.Equal(t, v6as, "2001:db8::20,2001:db8::90,2001:db8::170,2001:db8::240,2001:db8::250")
	require.Empty(t, v6us)

	// check dualstack valid pool
	dualValidPoolName := "dualValidPool"
	validDualIPs := []string{"10.0.0.30", "10.0.0.80", "2001:db8::30", "2001:db8::80"}
	err = subnet.AddOrUpdateIPPool(dualValidPoolName, validDualIPs)
	require.NoError(t, err)
	v4a, v4u, v6a, v6u, v4as, v4us, v6as, v6us = subnet.IPPoolStatistics(dualValidPoolName)
	require.Equal(t, v4a.String(), "2")
	require.Empty(t, v4u)
	require.Equal(t, v6a.String(), "2")
	require.Empty(t, v6u)
	require.Equal(t, v4as, "10.0.0.30,10.0.0.80")
	require.Empty(t, v4us)
	require.Equal(t, v6as, "2001:db8::30,2001:db8::80")
	require.Empty(t, v6us)

	// check not exist pool
	notExistPoolName := "notExistPool"
	v4a, v4u, v6a, v6u, v4as, v4us, v6as, v6us = subnet.IPPoolStatistics(notExistPoolName)
	require.Empty(t, v4a)
	require.Empty(t, v4u)
	require.Empty(t, v6a)
	require.Empty(t, v6u)
	require.Empty(t, v4as)
	require.Empty(t, v4us)
	require.Empty(t, v6as)
	require.Empty(t, v6us)
}

func TestSubnetReleaseAddr(t *testing.T) {
	v4ExcludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
	}
	subnet, err := NewSubnet("v4Subnet", "10.0.0.0/24", v4ExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, subnet)
	// 1.1 two different pod get the same v4 ip
	pod41Name := "pod41.default"
	nic41Name := "pod41.default"
	// release not exist ip
	subnet.releaseAddr(pod41Name, nic41Name)
	var mac *string
	v4 := "10.0.0.3"
	v4IP, err := NewIP(v4)
	require.NoError(t, err)
	ip1, macStr1, err := subnet.GetStaticAddress(pod41Name, nic41Name, v4IP, mac, false, true)
	require.NoError(t, err)
	require.Equal(t, v4, ip1.String())
	require.NotEmpty(t, macStr1)
	pod42Name := "pod42.default"
	nic42Name := "pod42.default"
	ip2, macStr2, err := subnet.GetStaticAddress(pod42Name, nic42Name, v4IP, mac, false, false)
	require.NoError(t, err)
	require.Equal(t, v4, ip2.String())
	require.NotEmpty(t, macStr2)
	subnet.releaseAddr(pod41Name, nic41Name)
	subnet.releaseAddr(pod42Name, nic42Name)
	pod43Name := "pod43.default"
	nic43Name := "pod43.default"
	// 1.2 release from exclude ip
	v43 := "10.0.0.100"
	v43IP, err := NewIP(v43)
	require.NoError(t, err)
	ip3, macStr3, err := subnet.GetStaticAddress(pod43Name, nic43Name, v43IP, mac, false, false)
	require.NoError(t, err)
	require.Equal(t, v43, ip3.String())
	require.NotEmpty(t, macStr3)
	subnet.releaseAddr(pod43Name, nic43Name)

	// 2. two different pod get the same v6 ip
	v6ExcludeIps := []string{
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	subnet, err = NewSubnet("v6Subnet", "2001:db8::/64", v6ExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, subnet)
	pod61Name := "pod61.default"
	nic61Name := "pod61.default"
	// release not exist ip
	subnet.releaseAddr(pod61Name, nic61Name)
	v6 := "2001:db8::3"
	v6IP, err := NewIP(v6)
	require.NoError(t, err)
	ip1, macStr1, err = subnet.GetStaticAddress(pod61Name, nic61Name, v6IP, nil, false, true)
	require.NoError(t, err)
	require.Equal(t, v6, ip1.String())
	require.NotEmpty(t, macStr1)
	pod62Name := "pod2.default"
	nic62Name := "pod2.default"
	ip2, macStr2, err = subnet.GetStaticAddress(pod62Name, nic62Name, v6IP, mac, false, false)
	require.NoError(t, err)
	require.Equal(t, v6, ip2.String())
	require.NotEmpty(t, macStr2)
	subnet.releaseAddr(pod61Name, nic61Name)
	subnet.releaseAddr(pod62Name, nic62Name)
	// 2.2 release from exclude ip
	pod63Name := "pod63.default"
	nic63Name := "pod63.default"
	v63 := "2001:db8::100"
	v63IP, err := NewIP(v63)
	require.NoError(t, err)
	ip3, macStr3, err = subnet.GetStaticAddress(pod63Name, nic63Name, v63IP, nil, false, false)
	require.NoError(t, err)
	require.Equal(t, v63, ip3.String())
	require.NotEmpty(t, macStr3)
	subnet.releaseAddr(pod63Name, nic63Name)
}

func TestPopPodNic(t *testing.T) {
	subnet, err := NewSubnet("v4Subnet", "10.0.0.0/24", nil)
	require.NoError(t, err)
	require.NotNil(t, subnet)

	// 1. Existing pod and nic
	podName := "pod1.default"
	nicName := "nic1"
	subnet.PodToNicList[podName] = []string{nicName}
	subnet.popPodNic(podName, nicName)
	require.Equal(t, 0, len(subnet.PodToNicList[podName]))

	// 2. Non-existent nic
	subnet.PodToNicList[podName] = []string{nicName}
	subnet.popPodNic(podName, "nonexistentNic")
	require.Equal(t, []string{nicName}, subnet.PodToNicList[podName])

	// 3. List empty after removal
	subnet.popPodNic(podName, nicName)
	require.Equal(t, 0, len(subnet.PodToNicList[podName]))

	// 4. Non-existent pod
	subnet.popPodNic("nonexistentPod", nicName)
	// Ensure no panic occurs and no changes in the map
	require.Equal(t, 0, len(subnet.PodToNicList[podName]))

	// 5. Multiple nics in the list
	subnet.PodToNicList[podName] = []string{"nic1", "nic2", "nic3"}
	subnet.popPodNic(podName, "nic2")
	require.Equal(t, []string{"nic1", "nic3"}, subnet.PodToNicList[podName])

	// 6. Existing pod and nil nic
	subnet.PodToNicList[podName] = nil
	subnet.popPodNic(podName, nicName)
	require.Equal(t, 0, len(subnet.PodToNicList[podName]))
}

func TestGetStaticAddressReleaseExisting(t *testing.T) {
	// Test IPv4 scenario
	t.Run("IPv4_ReleaseExistingAddress", func(t *testing.T) {
		subnet, err := NewSubnet("v4Subnet", "10.0.0.0/24", nil)
		require.NoError(t, err)
		require.NotNil(t, subnet)

		podName := "pod1.default"
		nicName := "nic1"

		// First allocation
		firstIP, err := NewIP("10.0.0.5")
		require.NoError(t, err)
		ip1, mac1, err := subnet.GetStaticAddress(podName, nicName, firstIP, nil, false, true)
		require.NoError(t, err)
		require.Equal(t, "10.0.0.5", ip1.String())
		require.NotEmpty(t, mac1)

		// Verify first allocation
		require.Equal(t, firstIP, subnet.V4NicToIP[nicName])
		require.Equal(t, podName, subnet.V4IPToPod["10.0.0.5"])
		require.True(t, subnet.V4Using.Contains(firstIP))
		require.False(t, subnet.V4Available.Contains(firstIP))

		// Second allocation with different IP for same nicName
		secondIP, err := NewIP("10.0.0.10")
		require.NoError(t, err)
		ip2, mac2, err := subnet.GetStaticAddress(podName, nicName, secondIP, nil, false, true)
		require.NoError(t, err)
		require.Equal(t, "10.0.0.10", ip2.String())
		require.NotEmpty(t, mac2)

		// Verify second allocation and first address is released
		require.Equal(t, secondIP, subnet.V4NicToIP[nicName])
		require.Equal(t, podName, subnet.V4IPToPod["10.0.0.10"])
		require.True(t, subnet.V4Using.Contains(secondIP))
		require.False(t, subnet.V4Available.Contains(secondIP))

		// Verify first address is released
		_, exists := subnet.V4IPToPod["10.0.0.5"]
		require.False(t, exists)
		require.False(t, subnet.V4Using.Contains(firstIP))
	})

	t.Run("IPv4_KeepExistingAddressOnGatewayIPConflict", func(t *testing.T) {
		subnet, err := NewSubnet("v4Subnet", "10.0.0.0/24", nil)
		require.NoError(t, err)
		subnet.V4Gw = "10.0.0.2"

		podName := "pod1.default"
		nicName := "nic1"
		firstIP, err := NewIP("10.0.0.5")
		require.NoError(t, err)
		gatewayIP, err := NewIP("10.0.0.2")
		require.NoError(t, err)

		_, mac, err := subnet.GetStaticAddress(podName, nicName, firstIP, nil, false, true)
		require.NoError(t, err)

		_, _, err = subnet.GetStaticAddress(podName, nicName, gatewayIP, nil, false, true)
		require.ErrorIs(t, err, ErrConflict)
		require.Equal(t, firstIP, subnet.V4NicToIP[nicName])
		require.Equal(t, podName, subnet.V4IPToPod[firstIP.String()])
		require.True(t, subnet.V4Using.Contains(firstIP))
		require.False(t, subnet.V4Available.Contains(firstIP))
		require.Empty(t, subnet.V4IPToPod[gatewayIP.String()])
		require.False(t, subnet.V4Using.Contains(gatewayIP))
		require.Equal(t, mac, subnet.NicToMac[nicName])
	})

	t.Run("IPv6_KeepExistingAddressOnGatewayIPConflict", func(t *testing.T) {
		subnet, err := NewSubnet("v6Subnet", "2001:db8::/64", nil)
		require.NoError(t, err)
		subnet.V6Gw = "2001:db8::2"

		podName := "pod1.default"
		nicName := "nic1"
		firstIP, err := NewIP("2001:db8::5")
		require.NoError(t, err)
		gatewayIP, err := NewIP("2001:db8::2")
		require.NoError(t, err)

		_, mac, err := subnet.GetStaticAddress(podName, nicName, firstIP, nil, false, true)
		require.NoError(t, err)

		_, _, err = subnet.GetStaticAddress(podName, nicName, gatewayIP, nil, false, true)
		require.ErrorIs(t, err, ErrConflict)
		require.Equal(t, firstIP, subnet.V6NicToIP[nicName])
		require.Equal(t, podName, subnet.V6IPToPod[firstIP.String()])
		require.True(t, subnet.V6Using.Contains(firstIP))
		require.False(t, subnet.V6Available.Contains(firstIP))
		require.Empty(t, subnet.V6IPToPod[gatewayIP.String()])
		require.False(t, subnet.V6Using.Contains(gatewayIP))
		require.Equal(t, mac, subnet.NicToMac[nicName])
	})

	// Test IPv6 scenario
	t.Run("IPv6_ReleaseExistingAddress", func(t *testing.T) {
		subnet, err := NewSubnet("v6Subnet", "2001:db8::/64", nil)
		require.NoError(t, err)
		require.NotNil(t, subnet)

		podName := "pod1.default"
		nicName := "nic1"

		// First allocation
		firstIP, err := NewIP("2001:db8::5")
		require.NoError(t, err)
		ip1, mac1, err := subnet.GetStaticAddress(podName, nicName, firstIP, nil, false, true)
		require.NoError(t, err)
		require.Equal(t, "2001:db8::5", ip1.String())
		require.NotEmpty(t, mac1)

		// Verify first allocation
		require.Equal(t, firstIP, subnet.V6NicToIP[nicName])
		require.Equal(t, podName, subnet.V6IPToPod["2001:db8::5"])
		require.True(t, subnet.V6Using.Contains(firstIP))
		require.False(t, subnet.V6Available.Contains(firstIP))

		// Second allocation with different IP for same nicName
		secondIP, err := NewIP("2001:db8::10")
		require.NoError(t, err)
		ip2, mac2, err := subnet.GetStaticAddress(podName, nicName, secondIP, nil, false, true)
		require.NoError(t, err)
		require.Equal(t, "2001:db8::10", ip2.String())
		require.NotEmpty(t, mac2)

		// Verify second allocation and first address is released
		require.Equal(t, secondIP, subnet.V6NicToIP[nicName])
		require.Equal(t, podName, subnet.V6IPToPod["2001:db8::10"])
		require.True(t, subnet.V6Using.Contains(secondIP))
		require.False(t, subnet.V6Available.Contains(secondIP))

		// Verify first address is released
		_, exists := subnet.V6IPToPod["2001:db8::5"]
		require.False(t, exists)
		require.False(t, subnet.V6Using.Contains(firstIP))
	})

	// Test same IP allocation should not release
	t.Run("SameIP_NoRelease", func(t *testing.T) {
		subnet, err := NewSubnet("v4Subnet", "10.0.0.0/24", nil)
		require.NoError(t, err)
		require.NotNil(t, subnet)

		podName := "pod1.default"
		nicName := "nic1"

		// First allocation
		targetIP, err := NewIP("10.0.0.5")
		require.NoError(t, err)
		ip1, mac1, err := subnet.GetStaticAddress(podName, nicName, targetIP, nil, false, true)
		require.NoError(t, err)
		require.Equal(t, "10.0.0.5", ip1.String())
		require.NotEmpty(t, mac1)

		// Second allocation with same IP for same nicName
		ip2, mac2, err := subnet.GetStaticAddress(podName, nicName, targetIP, nil, false, true)
		require.NoError(t, err)
		require.Equal(t, "10.0.0.5", ip2.String())
		require.Equal(t, mac1, mac2) // MAC should remain same

		// Verify address is still allocated
		require.Equal(t, targetIP, subnet.V4NicToIP[nicName])
		require.Equal(t, podName, subnet.V4IPToPod["10.0.0.5"])
		require.True(t, subnet.V4Using.Contains(targetIP))
	})

	// Test dual stack scenario - same protocol replacement
	t.Run("DualStack_SameProtocolReplacement", func(t *testing.T) {
		subnet, err := NewSubnet("dualSubnet", "10.0.0.0/24,2001:db8::/64", nil)
		require.NoError(t, err)
		require.NotNil(t, subnet)

		podName := "pod1.default"
		nicName := "nic1"

		// First allocation - IPv4
		firstV4IP, err := NewIP("10.0.0.5")
		require.NoError(t, err)
		ip1, _, err := subnet.GetStaticAddress(podName, nicName, firstV4IP, nil, false, true)
		require.NoError(t, err)
		require.Equal(t, "10.0.0.5", ip1.String())

		// Second allocation - IPv6 for same nicName (should coexist in dual stack)
		firstV6IP, err := NewIP("2001:db8::5")
		require.NoError(t, err)
		ip2, _, err := subnet.GetStaticAddress(podName, nicName, firstV6IP, nil, false, true)
		require.NoError(t, err)
		require.Equal(t, "2001:db8::5", ip2.String())

		// Verify both IPv4 and IPv6 coexist in dual stack
		require.Equal(t, firstV4IP, subnet.V4NicToIP[nicName], "IPv4 should coexist with IPv6")
		require.Equal(t, firstV6IP, subnet.V6NicToIP[nicName])
		require.Equal(t, podName, subnet.V4IPToPod["10.0.0.5"])
		require.Equal(t, podName, subnet.V6IPToPod["2001:db8::5"])

		// Third allocation - Different IPv4 for same nicName (should replace IPv4 only)
		secondV4IP, err := NewIP("10.0.0.10")
		require.NoError(t, err)
		ip3, _, err := subnet.GetStaticAddress(podName, nicName, secondV4IP, nil, false, true)
		require.NoError(t, err)
		require.Equal(t, "10.0.0.10", ip3.String())

		// Verify IPv4 is replaced but IPv6 remains
		require.Equal(t, secondV4IP, subnet.V4NicToIP[nicName], "New IPv4 should replace old one")
		require.Equal(t, firstV6IP, subnet.V6NicToIP[nicName], "IPv6 should remain unchanged")
		_, v4exists := subnet.V4IPToPod["10.0.0.5"]
		require.False(t, v4exists, "Original IPv4 should be released")
		require.Equal(t, podName, subnet.V4IPToPod["10.0.0.10"])
		require.Equal(t, podName, subnet.V6IPToPod["2001:db8::5"])
	})
}

func TestGetStaticMac(t *testing.T) {
	subnet, err := NewSubnet("v4Subnet", "10.0.0.0/24", nil)
	require.NoError(t, err)
	require.NotNil(t, subnet)
	podName := "pod1.default"
	nicName := "nic1"
	err = subnet.GetStaticMac(podName, nicName, "", false)
	require.NoError(t, err)
}

// TestGetV4RandomAddressMacConflictRollback verifies that when a random IPv4
// allocation fails at the GetStaticMac step (mac conflict), every internal
// counter the allocator already touched is rolled back, so the IP is not
// leaked and the next request can succeed.
func TestGetV4RandomAddressMacConflictRollback(t *testing.T) {
	subnet, err := NewSubnet("rollbackV4Subnet", "10.0.0.0/24", []string{"10.0.0.1"})
	require.NoError(t, err)
	require.NotNil(t, subnet)
	pool := subnet.IPPools[""]
	require.NotNil(t, pool)

	staticMac := "00:11:22:33:44:55"

	// 1. allocate the mac to pod1 first so the next request will conflict
	_, _, mac1, err := subnet.GetRandomAddress("", "pod1.default", "pod1.default", &staticMac, nil, true)
	require.NoError(t, err)
	require.Equal(t, staticMac, mac1)

	availAfterPod1 := subnet.V4Available.Len()
	usingAfterPod1 := subnet.V4Using.Len()
	poolAvailAfterPod1 := pool.V4Available.Len()
	poolUsingAfterPod1 := pool.V4Using.Len()

	// 2. pod2 reuses the same mac with checkConflict=true → GetStaticMac fails
	v4IP2, _, _, err := subnet.GetRandomAddress("", "pod2.default", "pod2.default", &staticMac, nil, true)
	require.ErrorIs(t, err, ErrConflict)
	require.Nil(t, v4IP2)

	// 3. pod2's state must be fully rolled back
	_, hasNic := subnet.V4NicToIP["pod2.default"]
	require.False(t, hasNic, "V4NicToIP should not contain the failed nic")
	require.NotContains(t, subnet.PodToNicList, "pod2.default", "PodToNicList should not contain the failed pod")
	require.NotContains(t, subnet.NicToMac, "pod2.default", "NicToMac should not contain the failed nic")
	for ip, owner := range subnet.V4IPToPod {
		require.NotEqual(t, "pod2.default", owner, "V4IPToPod must not retain a stale entry for pod2, leaked ip=%s", ip)
	}

	// the available/using counters must match the pod1-only baseline (the
	// rolled-back IP goes back to V4Available and pool.V4Released)
	require.Equal(t, availAfterPod1, subnet.V4Available.Len(), "subnet V4Available leaked")
	require.Equal(t, usingAfterPod1, subnet.V4Using.Len(), "subnet V4Using leaked")
	require.Equal(t, poolAvailAfterPod1, pool.V4Available.Len(), "pool V4Available leaked")
	require.Equal(t, poolUsingAfterPod1, pool.V4Using.Len(), "pool V4Using leaked")

	// 4. retry with a different mac must succeed — the previously leaked IP
	// must be reachable through the released pool
	differentMac := "00:11:22:33:44:66"
	v4IP3, _, mac3, err := subnet.GetRandomAddress("", "pod2.default", "pod2.default", &differentMac, nil, true)
	require.NoError(t, err)
	require.NotNil(t, v4IP3)
	require.Equal(t, differentMac, mac3)
}

// TestGetV6RandomAddressMacConflictRollback mirrors the IPv4 test for the v6
// path.
func TestGetV6RandomAddressMacConflictRollback(t *testing.T) {
	subnet, err := NewSubnet("rollbackV6Subnet", "2001:db8::/120", []string{"2001:db8::1"})
	require.NoError(t, err)
	require.NotNil(t, subnet)
	pool := subnet.IPPools[""]
	require.NotNil(t, pool)

	staticMac := "00:11:22:33:44:77"

	_, _, _, err = subnet.GetRandomAddress("", "pod1.default", "pod1.default", &staticMac, nil, true)
	require.NoError(t, err)

	availAfterPod1 := subnet.V6Available.Len()
	usingAfterPod1 := subnet.V6Using.Len()
	poolUsingAfterPod1 := pool.V6Using.Len()

	_, v6IP2, _, err := subnet.GetRandomAddress("", "pod2.default", "pod2.default", &staticMac, nil, true)
	require.ErrorIs(t, err, ErrConflict)
	require.Nil(t, v6IP2)

	_, hasNic := subnet.V6NicToIP["pod2.default"]
	require.False(t, hasNic, "V6NicToIP should not retain the failed nic")
	require.NotContains(t, subnet.PodToNicList, "pod2.default")
	for ip, owner := range subnet.V6IPToPod {
		require.NotEqual(t, "pod2.default", owner, "V6IPToPod must not retain a stale entry, leaked ip=%s", ip)
	}

	require.Equal(t, availAfterPod1, subnet.V6Available.Len(), "subnet V6Available leaked")
	require.Equal(t, usingAfterPod1, subnet.V6Using.Len(), "subnet V6Using leaked")
	require.Equal(t, poolUsingAfterPod1, pool.V6Using.Len(), "pool V6Using leaked")
}

// TestGetDualRandomAddressPreservesExistingV4WhenV6Fails covers the
// dual-stack retry case where the IPv4 half is already cached on the nic.
// When the v6 step then fails (e.g. pool exhausted), rollback must NOT
// tear down the pre-existing IPv4 lease — only counters this call
// actually mutated may be undone.
func TestGetDualRandomAddressPreservesExistingV4WhenV6Fails(t *testing.T) {
	// /127 has 2 v6 addresses; exclude both so the v6 pool is empty and
	// any v6 allocation will fail with ErrNoAvailable.
	subnet, err := NewSubnet(
		"preserveExistingV4Subnet",
		"10.0.0.0/24,2001:db8::/127",
		[]string{"10.0.0.1", "2001:db8::", "2001:db8::1"},
	)
	require.NoError(t, err)
	require.NotNil(t, subnet)

	podName := "pod1.default"
	nicName := "pod1.default"
	staticV4, err := NewIP("10.0.0.5")
	require.NoError(t, err)
	staticMac := "00:11:22:33:44:99"

	// seed pod1 with a v4-only static lease — this mimics a prior
	// allocation that succeeded for the v4 half only.
	gotV4, gotMac, err := subnet.GetStaticAddress(podName, nicName, staticV4, &staticMac, false, true)
	require.NoError(t, err)
	require.Equal(t, staticV4.String(), gotV4.String())
	require.Equal(t, staticMac, gotMac)
	require.Equal(t, staticV4.String(), subnet.V4NicToIP[nicName].String())
	require.Nil(t, subnet.V6NicToIP[nicName])

	availBefore := subnet.V4Available.Len()
	usingBefore := subnet.V4Using.Len()

	// dual-stack request on the same nic: v4 fast-path returns the
	// existing lease, v6 has nothing to allocate and must fail.
	v4IP, v6IP, _, err := subnet.GetRandomAddress("", podName, nicName, &staticMac, nil, true)
	require.ErrorIs(t, err, ErrNoAvailable)
	require.Nil(t, v4IP)
	require.Nil(t, v6IP)

	// pod1's pre-existing v4 lease and MAC mapping must be intact
	require.Equal(t, staticV4.String(), subnet.V4NicToIP[nicName].String(), "v4 lease was wrongly revoked")
	require.Equal(t, staticMac, subnet.NicToMac[nicName], "MAC mapping was wrongly revoked")
	require.Equal(t, podName, subnet.V4IPToPod[staticV4.String()])
	require.Equal(t, podName, subnet.MacToPod[staticMac])
	require.Contains(t, subnet.PodToNicList, podName)
	// counters must not have moved since the v4 fast-path never mutated them
	require.Equal(t, availBefore, subnet.V4Available.Len(), "V4Available was disturbed by a no-op fast-path")
	require.Equal(t, usingBefore, subnet.V4Using.Len(), "V4Using was disturbed by a no-op fast-path")
}

// TestGetV4RandomAddressMacConflictPreservesV6 covers the dual-stack case
// where the nic already has a v6 lease and a fresh v4 allocation fails on
// the static-MAC conflict check. The newly-allocated v4 state must be
// torn down but the unrelated v6 lease must stay put.
func TestGetV4RandomAddressMacConflictPreservesV6(t *testing.T) {
	subnet, err := NewSubnet(
		"preserveV6OnV4FailureSubnet",
		"10.0.0.0/24,2001:db8::/120",
		[]string{"10.0.0.1", "2001:db8::1"},
	)
	require.NoError(t, err)
	require.NotNil(t, subnet)

	// pod0 owns the mac that pod1 will collide on
	conflictMac := "00:11:22:33:44:aa"
	_, _, _, err = subnet.GetRandomAddress("", "pod0.default", "pod0.default", &conflictMac, nil, true)
	require.NoError(t, err)

	// pod1 already has a v6-only lease and a different MAC on the nic
	podName := "pod1.default"
	nicName := "pod1.default"
	staticV6, err := NewIP("2001:db8::5")
	require.NoError(t, err)
	pod1Mac := "00:11:22:33:44:bb"
	gotV6, _, err := subnet.GetStaticAddress(podName, nicName, staticV6, &pod1Mac, false, true)
	require.NoError(t, err)
	require.Equal(t, staticV6.String(), gotV6.String())
	require.Equal(t, staticV6.String(), subnet.V6NicToIP[nicName].String())
	require.Nil(t, subnet.V4NicToIP[nicName])

	// dual-stack request on the same nic, but request pod0's mac so the
	// v4 step's GetStaticMac fails with ErrConflict.
	v4IP, v6IP, _, err := subnet.GetRandomAddress("", podName, nicName, &conflictMac, nil, true)
	require.ErrorIs(t, err, ErrConflict)
	require.Nil(t, v4IP)
	require.Nil(t, v6IP)

	// pod1's pre-existing v6 lease and MAC must survive
	require.Equal(t, staticV6.String(), subnet.V6NicToIP[nicName].String(), "v6 lease was wrongly revoked")
	require.Equal(t, pod1Mac, subnet.NicToMac[nicName], "pod1's MAC was wrongly revoked")
	require.Equal(t, podName, subnet.V6IPToPod[staticV6.String()])
	require.Equal(t, podName, subnet.MacToPod[pod1Mac])
	// no leaked v4 state for pod1
	_, hasV4 := subnet.V4NicToIP[nicName]
	require.False(t, hasV4, "V4NicToIP must not retain a stale entry")
	for ip, owner := range subnet.V4IPToPod {
		require.NotEqual(t, podName, owner, "V4IPToPod leaked ip=%s", ip)
	}
}

// TestGetV6RandomAddressMacConflictPreservesV4 is the IPv6 mirror of the
// previous test: the nic already has a v4 lease and the v6 allocation
// fails on MAC conflict. The v4 lease must be left untouched.
func TestGetV6RandomAddressMacConflictPreservesV4(t *testing.T) {
	subnet, err := NewSubnet(
		"preserveV4OnV6FailureSubnet",
		"10.0.0.0/24,2001:db8::/120",
		[]string{"10.0.0.1", "2001:db8::1"},
	)
	require.NoError(t, err)
	require.NotNil(t, subnet)

	conflictMac := "00:11:22:33:44:cc"
	_, _, _, err = subnet.GetRandomAddress("", "pod0.default", "pod0.default", &conflictMac, nil, true)
	require.NoError(t, err)

	podName := "pod1.default"
	nicName := "pod1.default"
	staticV4, err := NewIP("10.0.0.5")
	require.NoError(t, err)
	pod1Mac := "00:11:22:33:44:dd"
	gotV4, _, err := subnet.GetStaticAddress(podName, nicName, staticV4, &pod1Mac, false, true)
	require.NoError(t, err)
	require.Equal(t, staticV4.String(), gotV4.String())
	require.Equal(t, staticV4.String(), subnet.V4NicToIP[nicName].String())
	require.Nil(t, subnet.V6NicToIP[nicName])

	// dual-stack request: v4 fast-path returns the existing lease, then
	// v6 allocation fails the static MAC check on the conflicting mac.
	v4IP, v6IP, _, err := subnet.GetRandomAddress("", podName, nicName, &conflictMac, nil, true)
	require.ErrorIs(t, err, ErrConflict)
	require.Nil(t, v4IP)
	require.Nil(t, v6IP)

	// pod1's v4 lease and MAC must survive
	require.Equal(t, staticV4.String(), subnet.V4NicToIP[nicName].String(), "v4 lease was wrongly revoked")
	require.Equal(t, pod1Mac, subnet.NicToMac[nicName], "pod1's MAC was wrongly revoked")
	require.Equal(t, podName, subnet.V4IPToPod[staticV4.String()])
	require.Equal(t, podName, subnet.MacToPod[pod1Mac])
	_, hasV6 := subnet.V6NicToIP[nicName]
	require.False(t, hasV6, "V6NicToIP must not retain a stale entry")
	for ip, owner := range subnet.V6IPToPod {
		require.NotEqual(t, podName, owner, "V6IPToPod leaked ip=%s", ip)
	}
}

// TestGetDualRandomAddressV6FailureRollsBackV4 verifies that when the IPv6
// half of a dual-stack random allocation fails after the IPv4 half already
// succeeded, the IPv4 allocation is rolled back too — otherwise the v4
// pool would slowly leak every time the v6 step errors out.
//
// We trigger this by sizing the v6 range so it is empty by the time the
// second dual-stack request runs; the v6 path returns ErrNoAvailable while
// the v4 path has already mutated its counters.
func TestGetDualRandomAddressV6FailureRollsBackV4(t *testing.T) {
	// 2001:db8::/126 has 4 addresses; we exclude all but one so that
	// pod1's allocation drains the v6 pool and pod2's v6 step must fail.
	subnet, err := NewSubnet(
		"rollbackDualSubnet",
		"10.0.0.0/24,2001:db8::/126",
		[]string{"10.0.0.1", "2001:db8::", "2001:db8::1", "2001:db8::3"},
	)
	require.NoError(t, err)
	require.NotNil(t, subnet)
	pool := subnet.IPPools[""]
	require.NotNil(t, pool)

	// pod1 takes the only remaining v6 IP — no mac to keep this isolated
	// from the mac-conflict logic.
	v4Pod1, v6Pod1, _, err := subnet.GetRandomAddress("", "pod1.default", "pod1.default", nil, nil, true)
	require.NoError(t, err)
	require.NotNil(t, v4Pod1)
	require.NotNil(t, v6Pod1)

	v4AvailAfterPod1 := subnet.V4Available.Len()
	v4UsingAfterPod1 := subnet.V4Using.Len()
	poolV4UsingAfterPod1 := pool.V4Using.Len()
	poolV4AvailAfterPod1 := pool.V4Available.Len()

	// pod2 dual-stack request: v4 still has space, v6 pool is empty →
	// getV6RandomAddress returns ErrNoAvailable after getV4RandomAddress
	// already mutated the v4 counters.
	v4IP, v6IP, _, err := subnet.GetRandomAddress("", "pod2.default", "pod2.default", nil, nil, true)
	require.ErrorIs(t, err, ErrNoAvailable)
	require.Nil(t, v4IP)
	require.Nil(t, v6IP)

	// pod2's v4 half must have been rolled back too
	_, hasV4 := subnet.V4NicToIP["pod2.default"]
	require.False(t, hasV4, "V4NicToIP must be cleared when the v6 half fails")
	_, hasV6 := subnet.V6NicToIP["pod2.default"]
	require.False(t, hasV6, "V6NicToIP must be cleared when the v6 half fails")
	require.NotContains(t, subnet.PodToNicList, "pod2.default")
	for ip, owner := range subnet.V4IPToPod {
		require.NotEqual(t, "pod2.default", owner, "V4IPToPod leaked, ip=%s", ip)
	}

	// v4 counters should only reflect pod1, not pod2
	require.Equal(t, v4AvailAfterPod1, subnet.V4Available.Len(), "subnet V4Available leaked across dual-stack failure")
	require.Equal(t, v4UsingAfterPod1, subnet.V4Using.Len(), "subnet V4Using leaked across dual-stack failure")
	require.Equal(t, poolV4UsingAfterPod1, pool.V4Using.Len(), "pool V4Using leaked across dual-stack failure")
	require.Equal(t, poolV4AvailAfterPod1, pool.V4Available.Len(), "pool V4Available leaked across dual-stack failure")
}
