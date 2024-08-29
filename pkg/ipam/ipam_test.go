package ipam

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewIPAM(t *testing.T) {
	ipam := NewIPAM()
	require.NotNil(t, ipam)
	require.NotNil(t, ipam.Subnets)
	require.Equal(t, 0, len(ipam.Subnets))
}

func TestGetRandomAddress(t *testing.T) {
	ipam := NewIPAM()
	// test v4 subnet
	v4ExcludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
	}
	v4SubnetName := "v4Subnet"
	v4Subnet, err := NewSubnet(v4SubnetName, "10.0.0.0/24", v4ExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, v4Subnet)
	ipam.Subnets[v4SubnetName] = v4Subnet
	v4PodName := "pod1.default"
	v4NicName := "pod1.default"
	var mac *string
	v4, v6, macStr, err := ipam.GetRandomAddress(v4PodName, v4NicName, mac, v4SubnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.1", v4)
	require.Empty(t, v6)
	require.NotEmpty(t, macStr)
	// test v6 subnet
	v6ExcludeIps := []string{
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	v6SubnetName := "v6Subnet"
	v6Subnet, err := NewSubnet(v6SubnetName, "2001:db8::/64", v6ExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, v6Subnet)
	ipam.Subnets[v6SubnetName] = v6Subnet
	v6PodName := "pod2.default"
	v6NicName := "pod2.default"
	v4, v6, macStr, err = ipam.GetRandomAddress(v6PodName, v6NicName, mac, v6SubnetName, "", nil, true)
	require.NoError(t, err)
	require.Empty(t, v4)
	require.Equal(t, "2001:db8::1", v6)
	require.NotEmpty(t, macStr)
	// test dual stack subnet
	dualSubnetName := "dualSubnet"
	dualExcludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	dualSubnet, err := NewSubnet(dualSubnetName, "10.0.0.0/24,2001:db8::/64", dualExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, dualSubnet)
	ipam.Subnets[dualSubnetName] = dualSubnet
	dualPodName := "pod3.default"
	dualNicName := "pod3.default"
	v4, v6, macStr, err = ipam.GetRandomAddress(dualPodName, dualNicName, mac, dualSubnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.1", v4)
	require.Equal(t, "2001:db8::1", v6)
	require.NotEmpty(t, macStr)
	// test v4 subnet with skipped addresses
	v4ExcludeIps = []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
	}
	v4SubnetName = "v4Subnet"
	v4Subnet, err = NewSubnet(v4SubnetName, "10.0.0.0/24", v4ExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, v4Subnet)
	ipam.Subnets[v4SubnetName] = v4Subnet
	v4PodName = "pod4.default"
	v4NicName = "pod4.default"
	skippedAddrs := []string{"10.0.0.1", "10.0.0.3"}
	v4, v6, macStr, err = ipam.GetRandomAddress(v4PodName, v4NicName, mac, v4SubnetName, "", skippedAddrs, true)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.5", v4)
	require.Empty(t, v6)
	require.NotEmpty(t, macStr)
	// test v6 subnet with skipped addresses
	v6SubnetName = "v6Subnet"
	v6ExcludeIps = []string{
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	v6Subnet, err = NewSubnet(v6SubnetName, "2001:db8::/64", v6ExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, v6Subnet)
	ipam.Subnets[v6SubnetName] = v6Subnet
	v6PodName = "pod5.default"
	v6NicName = "pod5.default"
	skippedAddrs = []string{"2001:db8::1", "2001:db8::3"}
	v4, v6, macStr, err = ipam.GetRandomAddress(v6PodName, v6NicName, mac, v6SubnetName, "", skippedAddrs, true)
	require.NoError(t, err)
	require.Empty(t, v4)
	require.Equal(t, "2001:db8::5", v6)
	require.NotEmpty(t, macStr)
	// test dual stack subnet with skipped addresses
	dualSubnetName = "dualSubnet2"
	dualExcludeIps = []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	dualSubnet, err = NewSubnet(dualSubnetName, "10.0.0.0/24,2001:db8::/64", dualExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, dualSubnet)
	ipam.Subnets[dualSubnetName] = dualSubnet
	dualPodName = "pod6.default"
	dualNicName = "pod6.default"
	skippedAddrs = []string{"10.0.0.1", "10.0.0.3", "2001:db8::1", "2001:db8::3"}
	v4, v6, macStr, err = ipam.GetRandomAddress(dualPodName, dualNicName, mac, dualSubnetName, "", skippedAddrs, true)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.5", v4)
	require.Equal(t, "2001:db8::5", v6)
	require.NotEmpty(t, macStr)
}

func TestGetStaticAddress(t *testing.T) {
	ipam := NewIPAM()
	// test v4 subnet
	v4ExcludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
	}
	v4SubnetName := "v4Subnet"
	v4Subnet, err := NewSubnet(v4SubnetName, "10.0.0.0/24", v4ExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, v4Subnet)
	ipam.Subnets[v4SubnetName] = v4Subnet
	v4PodName := "pod1.default"
	v4NicName := "pod1.default"
	var mac *string
	v4StaticIP := "10.0.0.1"
	v4, v6, macStr, err := ipam.GetStaticAddress(v4PodName, v4NicName, v4StaticIP, mac, v4SubnetName, true)
	require.NoError(t, err)
	require.Equal(t, v4StaticIP, v4)
	require.Empty(t, v6)
	require.NotEmpty(t, macStr)
	// should conflict with v4 static ip
	v4PodName = "pod2.default"
	v4NicName = "pod2.default"
	v4StaticIP = "10.0.0.1"
	v4, v6, macStr, err = ipam.GetStaticAddress(v4PodName, v4NicName, v4StaticIP, mac, v4SubnetName, true)
	require.Error(t, err)
	require.Empty(t, v4)
	require.Empty(t, v6)
	require.Empty(t, macStr)
	// test v6 subnet
	v6SubnetName := "v6Subnet1"
	v6ExcludeIps := []string{
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	v6Subnet, err := NewSubnet(v6SubnetName, "2001:db8::/64", v6ExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, v6Subnet)
	ipam.Subnets[v6SubnetName] = v6Subnet
	v6PodName := "pod3.default"
	v6NicName := "pod3.default"
	v6StaticIP := "2001:db8::1"
	v4, v6, macStr, err = ipam.GetStaticAddress(v6PodName, v6NicName, v6StaticIP, mac, v6SubnetName, true)
	require.NoError(t, err)
	require.Empty(t, v4)
	require.Equal(t, v6StaticIP, v6)
	require.NotEmpty(t, macStr)
	// should conflict with v6 static ip
	v6PodName = "pod4.default"
	v6NicName = "pod4.default"
	v6StaticIP = "2001:db8::1"
	v4, v6, macStr, err = ipam.GetStaticAddress(v6PodName, v6NicName, v6StaticIP, mac, v6SubnetName, true)
	require.Error(t, err)
	require.Empty(t, v4)
	require.Empty(t, v6)
	require.Empty(t, macStr)

	// test dual stack subnet
	dualSubnetName := "dualSubnet1"
	dualExcludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	dualSubnet, err := NewSubnet(dualSubnetName, "10.0.0.0/24,2001:db8::/64", dualExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, dualSubnet)
	ipam.Subnets[dualSubnetName] = dualSubnet
	dualPodName := "pod5.default"
	dualNicName := "pod5.default"
	dualStaticIP := "10.0.0.1,2001:db8::1"
	v4, v6, macStr, err = ipam.GetStaticAddress(dualPodName, dualNicName, dualStaticIP, mac, dualSubnetName, true)
	require.NoError(t, err)
	require.Equal(t, v4StaticIP, v4)
	require.Equal(t, v6StaticIP, v6)
	require.NotEmpty(t, macStr)
	// should conflict with v4 static ip
	dualPodName = "pod6.default"
	dualNicName = "pod6.default"
	dualStaticIP = "10.0.0.1,2001:db8::3"
	v4, v6, macStr, err = ipam.GetStaticAddress(dualPodName, dualNicName, dualStaticIP, mac, dualSubnetName, true)
	require.Error(t, err)
	require.Empty(t, v4)
	require.Empty(t, v6)
	require.Empty(t, macStr)
	// should conflict with v6 static ip
	dualPodName = "pod7.default"
	dualNicName = "pod7.default"
	dualStaticIP = "10.0.0.3,2001:db8::1"
	v4, v6, macStr, err = ipam.GetStaticAddress(dualPodName, dualNicName, dualStaticIP, mac, dualSubnetName, true)
	require.Error(t, err)
	require.Empty(t, v4)
	require.Empty(t, v6)
	require.Empty(t, macStr)
}

func TestCheckAndAppendIpsForDual(t *testing.T) {
	ipam := NewIPAM()
	dualExcludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	dualSubnetName := "dualSubnet1"
	dualSubnet, err := NewSubnet(dualSubnetName, "10.0.0.0/24,2001:db8::/64", dualExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, dualSubnet)
	ipam.Subnets[dualSubnetName] = dualSubnet
	// test only v4 ip
	v4PodName := "pod1.default"
	v4NicName := "pod1.default"
	var onlyV4Ips []IP
	onlyV4Ips = append(onlyV4Ips, IP(net.ParseIP("10.0.0.1")))
	mac := "00:00:00:00:00:01"
	newIps, err := checkAndAppendIpsForDual(onlyV4Ips, mac, v4PodName, v4NicName, dualSubnet, true)
	require.NoError(t, err)
	require.Len(t, newIps, 2)
	require.Contains(t, newIps[0].String(), "10.0.0.1")
	require.Contains(t, newIps[1].String(), "2001:db8::1")
	// test only v6 ip
	v6PodName := "pod2.default"
	v6NicName := "pod2.default"
	var onlyV6Ips []IP
	onlyV6Ips = append(onlyV6Ips, IP(net.ParseIP("2001:db8::1")))
	mac = "00:00:00:00:00:02"
	newIps, err = checkAndAppendIpsForDual(onlyV6Ips, mac, v6PodName, v6NicName, dualSubnet, true)
	require.NoError(t, err)
	require.Len(t, newIps, 2)
	require.Contains(t, newIps[0].String(), "10.0.0.1")
	require.Contains(t, newIps[1].String(), "2001:db8::1")
	// test mac conflict
	v4PodName = "pod3.default"
	v4NicName = "pod3.default"
	var v4Ips []IP
	v4Ips = append(v4Ips, IP(net.ParseIP("10.0.0.3")))
	mac = "00:00:00:00:00:01"
	newIps, err = checkAndAppendIpsForDual(v4Ips, mac, v4PodName, v4NicName, dualSubnet, true)
	require.Error(t, err)
	require.Len(t, newIps, 0)
}

func TestReleaseAddressByPod(t *testing.T) {
	ipam := NewIPAM()
	// test v4 subnet
	v4ExcludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
	}
	v4SubnetName := "v4Subnet"
	v4Subnet, err := NewSubnet(v4SubnetName, "10.0.0.0/24", v4ExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, v4Subnet)
	ipam.Subnets[v4SubnetName] = v4Subnet
	v4PodName := "pod1.default"
	v4NicName := "pod1.default"
	var mac *string
	v4, v6, macStr, err := ipam.GetRandomAddress(v4PodName, v4NicName, mac, v4SubnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.1", v4)
	require.Empty(t, v6)
	require.NotEmpty(t, macStr)
	require.Equal(t, 1, v4Subnet.V4Using.Len())
	ipam.ReleaseAddressByPod(v4PodName, v4SubnetName)
	require.Equal(t, 0, v4Subnet.V4Using.Len())

	// test v6 subnet
	v6ExcludeIps := []string{
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	v6SubnetName := "v6Subnet"
	v6Subnet, err := NewSubnet("v6Subnet", "2001:db8::/64", v6ExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, v6Subnet)
	ipam.Subnets[v6SubnetName] = v6Subnet
	v6PodName := "pod2.default"
	v6NicName := "pod2.default"
	v4, v6, macStr, err = ipam.GetRandomAddress(v6PodName, v6NicName, mac, v6SubnetName, "", nil, true)
	require.NoError(t, err)
	require.Empty(t, v4)
	require.Equal(t, "2001:db8::1", v6)
	require.NotEmpty(t, macStr)
	require.Equal(t, 1, v6Subnet.V6Using.Len())
	ipam.ReleaseAddressByPod(v6PodName, "v6Subnet")
	require.Equal(t, 0, v6Subnet.V6Using.Len())

	// test dual stack subnet
	dualExcludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	dualSubnetName := "dualSubnet"
	dualSubnet, err := NewSubnet(dualSubnetName, "10.0.0.0/24,2001:db8::/64", dualExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, dualSubnet)
	ipam.Subnets[dualSubnetName] = dualSubnet
	dualPodName := "pod3.default"
	dualNicName := "pod3.default"
	v4, v6, macStr, err = ipam.GetRandomAddress(dualPodName, dualNicName, mac, dualSubnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.1", v4)
	require.Equal(t, "2001:db8::1", v6)
	require.NotEmpty(t, macStr)
	require.Equal(t, 1, dualSubnet.V4Using.Len())
	require.Equal(t, 1, dualSubnet.V6Using.Len())
	ipam.ReleaseAddressByPod(dualPodName, dualSubnetName)
	require.Equal(t, 0, dualSubnet.V4Using.Len())
	require.Equal(t, 0, dualSubnet.V6Using.Len())
}

func TestGetSubnetV4Mask(t *testing.T) {
	ipam := NewIPAM()
	// get mask for exist subnet
	v4ExcludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
	}
	v4SubnetName := "v4Subnet"
	ipv4CIDR := "10.0.0.0/24"
	v4Gw := "10.0.0.1"
	v4SubnetMask := "24"
	err := ipam.AddOrUpdateSubnet(v4SubnetName, ipv4CIDR, v4Gw, v4ExcludeIps)
	require.NoError(t, err)
	mask, err := ipam.GetSubnetV4Mask(v4SubnetName)
	require.NoError(t, err)
	require.Equal(t, mask, v4SubnetMask)

	// get mask for non-exist subnet
	nonExistSubnetName := "nonExistSubnet"
	mask, err = ipam.GetSubnetV4Mask(nonExistSubnetName)
	require.Equal(t, err, ErrNoAvailable)
	require.Empty(t, mask)
}

func TestGetSubnetIPRangeString(t *testing.T) {
	// test v4 exist subnet
	ipam := NewIPAM()
	v4ExcludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
	}
	v4SubnetName := "v4Subnet"
	ipv4CIDR := "10.0.0.0/24"
	v4Gw := "10.0.0.1"
	err := ipam.AddOrUpdateSubnet(v4SubnetName, ipv4CIDR, v4Gw, v4ExcludeIps)
	require.NoError(t, err)
	v4UsingIPStr, v6UsingIPStr, v4AvailableIPStr, v6AvailableIPStr := ipam.GetSubnetIPRangeString(v4SubnetName, v4ExcludeIps)
	require.Equal(t, v4UsingIPStr, "")
	require.Equal(t, v6UsingIPStr, "")
	require.Equal(t, v4AvailableIPStr, "10.0.0.1,10.0.0.3,10.0.0.5-10.0.0.99,10.0.0.101-10.0.0.251")
	require.Equal(t, v6AvailableIPStr, "")

	// test not exist subnet
	nonExistSubnetName := "nonExistSubnet"
	v4UsingIPStr, v6UsingIPStr, v4AvailableIPStr, v6AvailableIPStr = ipam.GetSubnetIPRangeString(nonExistSubnetName, v4ExcludeIps)
	require.Equal(t, v4UsingIPStr, "")
	require.Equal(t, v6UsingIPStr, "")
	require.Equal(t, v4AvailableIPStr, "")
	require.Equal(t, v6AvailableIPStr, "")

	// test v6 exist subnet
	v6ExcludeIps := []string{
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	v6SubnetName := "v6Subnet"
	ipv6CIDR := "2001:db8::/64"
	v6Gw := "2001:db8::1"
	err = ipam.AddOrUpdateSubnet(v6SubnetName, ipv6CIDR, v6Gw, v6ExcludeIps)
	require.NoError(t, err)
	v4UsingIPStr, v6UsingIPStr, v4AvailableIPStr, v6AvailableIPStr = ipam.GetSubnetIPRangeString(v6SubnetName, v6ExcludeIps)
	require.Equal(t, v4UsingIPStr, "")
	require.Equal(t, v6UsingIPStr, "")
	require.Equal(t, v4AvailableIPStr, "")
	require.Equal(t, v6AvailableIPStr, "2001:db8::1,2001:db8::3,2001:db8::5-2001:db8::ff,2001:db8::101-2001:db8::251,2001:db8::255-2001:db8::ffff:ffff:ffff:fffe")

	// test dual stack subnet
	dualSubnetName := "dualSubnet"
	dualExcludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	cidr := "10.0.0.0/24,2001:db8::/64"
	dualSubnet, err := NewSubnet(dualSubnetName, cidr, dualExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, dualSubnet)
	ipam.Subnets[dualSubnetName] = dualSubnet
	v4UsingIPStr, v6UsingIPStr, v4AvailableIPStr, v6AvailableIPStr = ipam.GetSubnetIPRangeString(dualSubnetName, dualExcludeIps)
	require.Equal(t, v4UsingIPStr, "")
	require.Equal(t, v6UsingIPStr, "")
	require.Equal(t, v4AvailableIPStr, "10.0.0.1,10.0.0.3,10.0.0.5-10.0.0.99,10.0.0.101-10.0.0.251")
	require.Equal(t, v6AvailableIPStr, "2001:db8::1,2001:db8::3,2001:db8::5-2001:db8::ff,2001:db8::101-2001:db8::251,2001:db8::255-2001:db8::ffff:ffff:ffff:fffe")
}

func TestAddOrUpdateIPPool(t *testing.T) {
	// test v4 subnet
	ipam := NewIPAM()
	v4ExcludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
	}
	v4SubnetName := "v4Subnet"
	ipv4CIDR := "10.0.0.0/24"
	v4Gw := "10.0.0.1"
	err := ipam.AddOrUpdateSubnet(v4SubnetName, ipv4CIDR, v4Gw, v4ExcludeIps)
	require.NoError(t, err)
	// create v4 pool in exist subnet
	v4PoolName := "v4Pool"
	v4PoolIPs := []string{"10.0.0.21", "10.0.0.41", "10.0.0.101"}
	err = ipam.AddOrUpdateIPPool(v4SubnetName, v4PoolName, v4PoolIPs)
	require.NoError(t, err)
	// create v4 pool in non-exist subnet
	nonExistSubnetName := "nonExistSubnet"
	v4PoolName = "v4Pool"
	err = ipam.AddOrUpdateIPPool(nonExistSubnetName, v4PoolName, v4PoolIPs)
	require.Error(t, err)

	// test v6 subnet
	v6ExcludeIps := []string{
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	v6SubnetName := "v6Subnet"
	ipv6CIDR := "2001:db8::/64"
	v6Gw := "2001:db8::1"
	err = ipam.AddOrUpdateSubnet(v6SubnetName, ipv6CIDR, v6Gw, v6ExcludeIps)
	require.NoError(t, err)
	// create v6 pool in exist subnet
	v6PoolName := "v6Pool"
	v6PoolIPs := []string{"2001:db8::21", "2001:db8::41", "2001:db8::101"}
	err = ipam.AddOrUpdateIPPool(v6SubnetName, v6PoolName, v6PoolIPs)
	require.NoError(t, err)
	// create v6 pool in non-exist subnet
	v6PoolName = "v6Pool"
	err = ipam.AddOrUpdateIPPool(nonExistSubnetName, v6PoolName, v6PoolIPs)
	require.Error(t, err)

	// test dual stack subnet
	dualSubnetName := "dualSubnet"
	dualExcludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	cidr := "10.0.0.0/24,2001:db8::/64"
	gw := "10.0.0.1,2001:db8::1"
	err = ipam.AddOrUpdateSubnet(dualSubnetName, cidr, gw, dualExcludeIps)
	require.NoError(t, err)
	dualPoolName := "dualPool"
	dualPoolIPs := []string{"10.0.0.21", "10.0.0.41", "2001:db8::21", "2001:db8::41"}
	err = ipam.AddOrUpdateIPPool(dualSubnetName, dualPoolName, dualPoolIPs)
	require.NoError(t, err)
	// create dual pool in non-exist subnet
	dualPoolName = "dualPool"
	err = ipam.AddOrUpdateIPPool(nonExistSubnetName, dualPoolName, dualPoolIPs)
	require.Error(t, err)
}

func TestRemoveIPPool(t *testing.T) {
	// test dual stack subnet
	ipam := NewIPAM()
	dualSubnetName := "dualSubnet"
	dualExcludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	cidr := "10.0.0.0/24,2001:db8::/64"
	gw := "10.0.0.1,2001:db8::1"
	err := ipam.AddOrUpdateSubnet(dualSubnetName, cidr, gw, dualExcludeIps)
	require.NoError(t, err)
	dualPoolName := "dualPool"
	dualPoolIPs := []string{"10.0.0.21", "10.0.0.41", "2001:db8::21", "2001:db8::41"}
	err = ipam.AddOrUpdateIPPool(dualSubnetName, dualPoolName, dualPoolIPs)
	require.NoError(t, err)
	// remove exist pool
	ipam.RemoveIPPool(dualSubnetName, dualPoolName)
	_, ok := ipam.Subnets[dualSubnetName].IPPools[dualPoolName]
	require.False(t, ok)

	// remove already exist pool
	ipam.RemoveIPPool(dualSubnetName, dualPoolName)
	_, ok = ipam.Subnets[dualSubnetName].IPPools[dualPoolName]
	require.False(t, ok)

	// remove non-exist pool
	nonExistPoolName := "nonExistPool"
	ipam.RemoveIPPool(dualSubnetName, nonExistPoolName)
}

func TestIPPoolStatistics(t *testing.T) {
	// test dual stack subnet
	ipam := NewIPAM()
	dualSubnetName := "dualSubnet"
	dualExcludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	cidr := "10.0.0.0/24,2001:db8::/64"
	gw := "10.0.0.1,2001:db8::1"
	err := ipam.AddOrUpdateSubnet(dualSubnetName, cidr, gw, dualExcludeIps)
	require.NoError(t, err)
	dualPoolName := "dualPool"
	dualPoolIPs := []string{"10.0.0.21", "10.0.0.41", "2001:db8::21", "2001:db8::41"}
	err = ipam.AddOrUpdateIPPool(dualSubnetName, dualPoolName, dualPoolIPs)
	require.NoError(t, err)
	// get exist pool statistics
	v4Available, v4Using, v6Available, v6Using, v4AvailableRange, v4UsingRange, v6AvailableRange, v6UsingRange := ipam.IPPoolStatistics(dualSubnetName, dualPoolName)
	require.Equal(t, "2", v4Available.String())
	require.Empty(t, v4Using)
	require.Equal(t, "2", v6Available.String())
	require.Empty(t, v6Using)
	require.Equal(t, "10.0.0.21,10.0.0.41", v4AvailableRange)
	require.Equal(t, "", v4UsingRange)
	require.Equal(t, "2001:db8::21,2001:db8::41", v6AvailableRange)
	require.Equal(t, "", v6UsingRange)
	// get non-exist pool statistics
	nonExistPoolName := "nonExistPool"
	v4Available, v4Using, v6Available, v6Using, v4AvailableRange, v4UsingRange, v6AvailableRange, v6UsingRange = ipam.IPPoolStatistics(dualSubnetName, nonExistPoolName)
	require.Equal(t, "0", v4Available.String())
	require.Empty(t, v4Using)
	require.Equal(t, "0", v6Available.String())
	require.Empty(t, v6Using)
	require.Empty(t, v4AvailableRange)
	require.Empty(t, v4UsingRange)
	require.Empty(t, v6AvailableRange)
	require.Empty(t, v6UsingRange)

	// get pool statistics from non-exist subnet
	nonExistSubnetName := "nonExistSubnet"
	v4Available, v4Using, v6Available, v6Using, v4AvailableRange, v4UsingRange, v6AvailableRange, v6UsingRange = ipam.IPPoolStatistics(nonExistSubnetName, dualPoolName)
	require.Equal(t, "0", v4Available.String())
	require.Empty(t, v4Using)
	require.Equal(t, "0", v6Available.String())
	require.Empty(t, v6Using)
	require.Empty(t, v4AvailableRange)
	require.Empty(t, v4UsingRange)
	require.Empty(t, v6AvailableRange)
	require.Empty(t, v6UsingRange)
}
