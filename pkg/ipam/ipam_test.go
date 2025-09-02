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

func TestDeleteSubnet(t *testing.T) {
	ipam := NewIPAM()
	// new v4 subnet
	v4ExcludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
	}
	v4SubnetName := "v4Subnet"
	v4Subnet, err := NewSubnet(v4SubnetName, "10.0.0.0/24", v4ExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, v4Subnet)
	ipam.Subnets[v4SubnetName] = v4Subnet

	// new v6 subnet
	v6ExcludeIps := []string{
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	v6SubnetName := "v6Subnet"
	v6Subnet, err := NewSubnet("v6Subnet", "2001:db8::/64", v6ExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, v6Subnet)
	ipam.Subnets[v6SubnetName] = v6Subnet

	// new dual stack subnet
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

	// test delete subnet
	require.Len(t, ipam.Subnets, 3)
	ipam.DeleteSubnet(v4SubnetName)
	require.Len(t, ipam.Subnets, 2)
	ipam.DeleteSubnet(v6SubnetName)
	require.Len(t, ipam.Subnets, 1)
	ipam.DeleteSubnet(dualSubnetName)
	require.Len(t, ipam.Subnets, 0)
}

func TestGetPodAddress(t *testing.T) {
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
	addresses := ipam.GetPodAddress(v4PodName)
	require.Len(t, addresses, 1)

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
	v6Addresses := ipam.GetPodAddress(v6PodName)
	require.Len(t, v6Addresses, 1)

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
	dualAddresses := ipam.GetPodAddress(dualPodName)
	require.Len(t, dualAddresses, 2)
}

func TestContainAddress(t *testing.T) {
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
	isContained := ipam.ContainAddress(v4)
	require.True(t, isContained)
	notExistV4 := "10.0.0.10"
	isContained = ipam.ContainAddress(notExistV4)
	require.False(t, isContained)
	invalidV4 := "10.0.0.10.10"
	isContained = ipam.ContainAddress(invalidV4)
	require.False(t, isContained)

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
	isContained = ipam.ContainAddress(v6)
	require.True(t, isContained)
	notExistV6 := "2001:db8::10"
	isContained = ipam.ContainAddress(notExistV6)
	require.False(t, isContained)
	invalidV6 := "2001:db8::10.10"
	isContained = ipam.ContainAddress(invalidV6)
	require.False(t, isContained)

	// test dual stack subnet
	dualExcludeIps := []string{
		"10.10.0.2", "10.10.0.4", "10.10.0.100",
		"10.10.0.252", "10.10.0.253", "10.10.0.254",
		"2001:db88::2", "2001:db88::4", "2001:db88::100",
		"2001:db88::252", "2001:db88::253", "2001:db88::254",
	}
	dualSubnetName := "dualSubnet"
	dualSubnet, err := NewSubnet(dualSubnetName, "10.10.0.0/24,2001:db88::/64", dualExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, dualSubnet)
	ipam.Subnets[dualSubnetName] = dualSubnet
	dualPodName := "pod3.default"
	dualNicName := "pod3.default"
	v4, v6, macStr, err = ipam.GetRandomAddress(dualPodName, dualNicName, mac, dualSubnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, "10.10.0.1", v4)
	require.Equal(t, "2001:db88::1", v6)
	require.NotEmpty(t, macStr)
	isContained = ipam.ContainAddress(v4)
	require.True(t, isContained)
	isContained = ipam.ContainAddress(v6)
	require.True(t, isContained)
	notExistDual := "10.10.0.10"
	isContained = ipam.ContainAddress(notExistDual)
	require.False(t, isContained)
	notExistDual = "2001:db88::10"
	isContained = ipam.ContainAddress(notExistDual)
	require.False(t, isContained)
	invalidDual := "10.10.0.10.10"
	isContained = ipam.ContainAddress(invalidDual)
	require.False(t, isContained)
}

func TestIsIPAssignedToOtherPod(t *testing.T) {
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
	v4PodName := "v4pod.default"
	v4NicName := "v4pod.default"
	var mac *string
	v4, _, _, err := ipam.GetRandomAddress(v4PodName, v4NicName, mac, v4SubnetName, "", nil, true)
	require.NoError(t, err)
	v4Pod2Name := "pod2.default"
	assignedPod, ok := ipam.IsIPAssignedToOtherPod(v4, v4SubnetName, v4Pod2Name)
	require.True(t, ok)
	require.Equal(t, v4PodName, assignedPod)
	notUsingV4 := "10.0.0.10"
	_, ok = ipam.IsIPAssignedToOtherPod(notUsingV4, v4SubnetName, v4Pod2Name)
	require.False(t, ok)

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
	v6PodName := "v6pod.default"
	v6NicName := "v6pod.default"
	_, v6, _, err := ipam.GetRandomAddress(v6PodName, v6NicName, mac, v6SubnetName, "", nil, true)
	require.NoError(t, err)
	v6Pod2Name := "pod2.default"
	assignedPod, ok = ipam.IsIPAssignedToOtherPod(v6, v6SubnetName, v6Pod2Name)
	require.True(t, ok)
	require.Equal(t, v6PodName, assignedPod)
	notUsingV6 := "2001:db8::10"
	_, ok = ipam.IsIPAssignedToOtherPod(notUsingV6, v6SubnetName, v6Pod2Name)
	require.False(t, ok)

	// test dual stack subnet
	dualExcludeIps := []string{
		"10.10.0.2", "10.10.0.4", "10.10.0.100",
		"10.10.0.252", "10.10.0.253", "10.10.0.254",
		"2001:db88::2", "2001:db88::4", "2001:db88::100",
		"2001:db88::252", "2001:db88::253", "2001:db88::254",
	}
	dualSubnetName := "dualSubnet"
	dualSubnet, err := NewSubnet(dualSubnetName, "10.10.0.0/24,2001:db88::/64", dualExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, dualSubnet)
	ipam.Subnets[dualSubnetName] = dualSubnet
	dualPodName := "dualpod.default"
	dualNicName := "dualpod.default"
	v4, v6, _, err = ipam.GetRandomAddress(dualPodName, dualNicName, mac, dualSubnetName, "", nil, true)
	require.NoError(t, err)
	dualPod2Name := "pod2.default"
	assignedPod, ok = ipam.IsIPAssignedToOtherPod(v4, dualSubnetName, dualPod2Name)
	require.True(t, ok)
	require.Equal(t, dualPodName, assignedPod)
	assignedPod, ok = ipam.IsIPAssignedToOtherPod(v6, dualSubnetName, dualPod2Name)
	require.True(t, ok)
	require.Equal(t, dualPodName, assignedPod)
	notUsingDualV4 := "10.10.0.10"
	_, ok = ipam.IsIPAssignedToOtherPod(notUsingDualV4, dualSubnetName, dualPod2Name)
	require.False(t, ok)
	notUsingDualV6 := "2001:db88::10"
	_, ok = ipam.IsIPAssignedToOtherPod(notUsingDualV6, dualSubnetName, dualPod2Name)
	require.False(t, ok)
	// test subnet not exist
	notExistSubnet := "notExistSubnet"
	_, ok = ipam.IsIPAssignedToOtherPod(v4, notExistSubnet, dualPod2Name)
	require.False(t, ok)
}

func TestIPAMAddOrUpdateSubnet(t *testing.T) {
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
	// test valid empty exclude ips
	v4ExcludeIps = []string{}
	err = ipam.AddOrUpdateSubnet(v4SubnetName, ipv4CIDR, v4Gw, v4ExcludeIps)
	require.NoError(t, err)
	// test valid empty gw
	v4Gw = ""
	err = ipam.AddOrUpdateSubnet(v4SubnetName, ipv4CIDR, v4Gw, v4ExcludeIps)
	require.NoError(t, err)
	// test invalid ipv4 cidr
	ipv4CIDR = "10.0.0./24"
	err = ipam.AddOrUpdateSubnet(v4SubnetName, ipv4CIDR, v4Gw, v4ExcludeIps)
	require.Equal(t, err, ErrInvalidCIDR)

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

	// test valid empty exclude ips
	v6ExcludeIps = []string{}
	err = ipam.AddOrUpdateSubnet(v6SubnetName, ipv6CIDR, v6Gw, v6ExcludeIps)
	require.NoError(t, err)

	// test valid empty gw
	v6Gw = ""
	err = ipam.AddOrUpdateSubnet(v6SubnetName, ipv6CIDR, v6Gw, v6ExcludeIps)
	require.NoError(t, err)

	// test invalid ipv6 cidr
	ipv6CIDR = "2001:g6::/64"
	err = ipam.AddOrUpdateSubnet(v6SubnetName, ipv6CIDR, v6Gw, v6ExcludeIps)
	require.Equal(t, err, ErrInvalidCIDR)

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

	// test valid empty exclude ips
	dualExcludeIps = []string{}
	err = ipam.AddOrUpdateSubnet(dualSubnetName, cidr, gw, dualExcludeIps)
	require.NoError(t, err)

	// test invalid empty gw
	gw = ""
	err = ipam.AddOrUpdateSubnet(dualSubnetName, cidr, gw, dualExcludeIps)
	require.Error(t, err)

	// test invalid empty cidr
	cidr = ""
	err = ipam.AddOrUpdateSubnet(dualSubnetName, cidr, gw, dualExcludeIps)
	require.Error(t, err)
	// test invalid v4 cidr
	cidr = "10.0.0./24,2001:db8::/64"
	err = ipam.AddOrUpdateSubnet(dualSubnetName, cidr, gw, dualExcludeIps)
	require.Error(t, err)
	// test invalid v6 cidr
	cidr = "10.0.0./24,2001:db8::/64"
	err = ipam.AddOrUpdateSubnet(dualSubnetName, cidr, gw, dualExcludeIps)
	require.Error(t, err)
}

func TestIPAMAddOrUpdateIPPool(t *testing.T) {
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

func TestIPAMAddOrUpdateSubnetWithIPPools(t *testing.T) {
	podName := "pod1.default"
	nicName := "pod1.default"

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
	// allocate random ip from subnet
	ipv4, _, _, err := ipam.GetRandomAddress(podName, nicName, nil, v4SubnetName, "", nil, true)
	require.NoError(t, err)
	// create v4 pool with the allocated ip
	v4PoolName := "v4Pool"
	v4PoolIPs := []string{ipv4}
	err = ipam.AddOrUpdateIPPool(v4SubnetName, v4PoolName, v4PoolIPs)
	require.NoError(t, err)
	require.Equal(t, ipv4, ipam.Subnets[v4SubnetName].IPPools[v4PoolName].V4Using.String())
	require.Equal(t, 0, ipam.Subnets[v4SubnetName].IPPools[v4PoolName].V4Free.Len())
	// update subnet
	v4ExcludeIps = append(v4ExcludeIps, "10.0.0.250")
	err = ipam.AddOrUpdateSubnet(v4SubnetName, ipv4CIDR, v4Gw, v4ExcludeIps)
	require.NoError(t, err)
	require.Equal(t, ipv4, ipam.Subnets[v4SubnetName].IPPools[v4PoolName].V4Using.String())
	require.Equal(t, 0, ipam.Subnets[v4SubnetName].IPPools[v4PoolName].V4Free.Len())

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
	// allocate random ip from subnet
	_, ipv6, _, err := ipam.GetRandomAddress(podName, nicName, nil, v6SubnetName, "", nil, true)
	require.NoError(t, err)
	// create v6 pool with the allocated ip
	v6PoolName := "v6Pool"
	v6PoolIPs := []string{ipv6}
	err = ipam.AddOrUpdateIPPool(v6SubnetName, v6PoolName, v6PoolIPs)
	require.NoError(t, err)
	require.Equal(t, ipv6, ipam.Subnets[v6SubnetName].IPPools[v6PoolName].V6Using.String())
	require.Equal(t, 0, ipam.Subnets[v6SubnetName].IPPools[v6PoolName].V6Free.Len())
	// update subnet
	v6ExcludeIps = append(v6ExcludeIps, "2001:db8::250")
	err = ipam.AddOrUpdateSubnet(v6SubnetName, ipv6CIDR, v6Gw, v6ExcludeIps)
	require.NoError(t, err)
	require.Equal(t, ipv6, ipam.Subnets[v6SubnetName].IPPools[v6PoolName].V6Using.String())
	require.Equal(t, 0, ipam.Subnets[v6SubnetName].IPPools[v6PoolName].V6Free.Len())

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
	// allocate random ip from subnet
	ipv4, ipv6, _, err = ipam.GetRandomAddress(podName, nicName, nil, dualSubnetName, "", nil, true)
	require.NoError(t, err)
	// create dual-stack pool with the allocated ip
	dualPoolName := "dualPool"
	dualPoolIPs := []string{ipv4, ipv6}
	err = ipam.AddOrUpdateIPPool(dualSubnetName, dualPoolName, dualPoolIPs)
	require.NoError(t, err)
	require.Equal(t, ipv4, ipam.Subnets[v4SubnetName].IPPools[v4PoolName].V4Using.String())
	require.Equal(t, ipv6, ipam.Subnets[v6SubnetName].IPPools[v6PoolName].V6Using.String())
	require.Equal(t, 0, ipam.Subnets[v4SubnetName].IPPools[v4PoolName].V4Free.Len())
	require.Equal(t, 0, ipam.Subnets[v6SubnetName].IPPools[v6PoolName].V6Free.Len())
	// update subnet
	dualExcludeIps = append(dualExcludeIps, "10.0.0.250", "2001:db8::250")
	err = ipam.AddOrUpdateSubnet(dualSubnetName, cidr, gw, dualExcludeIps)
	require.NoError(t, err)
	require.Equal(t, ipv4, ipam.Subnets[v4SubnetName].IPPools[v4PoolName].V4Using.String())
	require.Equal(t, ipv6, ipam.Subnets[v6SubnetName].IPPools[v6PoolName].V6Using.String())
	require.Equal(t, 0, ipam.Subnets[v4SubnetName].IPPools[v4PoolName].V4Free.Len())
	require.Equal(t, 0, ipam.Subnets[v6SubnetName].IPPools[v6PoolName].V6Free.Len())
}

func TestIPAMRemoveIPPool(t *testing.T) {
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

func TestIPAMIPPoolStatistics(t *testing.T) {
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
