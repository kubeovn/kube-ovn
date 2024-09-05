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
	// TODO: check pool
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
	v6 = "2001:db8::4"
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
	v6 = "2001:db8::5"
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

func TestGetGetV4RandomAddress(t *testing.T) {
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

func TestGetGetV4RandomAddressPTP(t *testing.T) {
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

func TestGetGetV6RandomAddress(t *testing.T) {
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
	subnet, err := NewSubnet("testV4RleasedSubnet", "10.0.0.0/24", excludeIps)
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
	subnet, err := NewSubnet("testV6RleasedSubnet", "2001:db8::/64", excludeIps)
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
	subnet, err := NewSubnet("testDualRleasedSubnet", "10.0.0.0/24,2001:db8::/64", excludeIps)
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

// TODO: ippool
