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
	v6Subnet, err := NewSubnet("v6Subnet", "2001:db8::/64", v6ExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, v6Subnet)
	ipam.Subnets["v6Subnet"] = v6Subnet
	v6PodName := "pod2.default"
	v6NicName := "pod2.default"
	v4, v6, macStr, err = ipam.GetRandomAddress(v6PodName, v6NicName, mac, "v6Subnet", "", nil, true)
	require.NoError(t, err)
	require.Empty(t, v4)
	require.Equal(t, "2001:db8::1", v6)
	require.NotEmpty(t, macStr)
	// test dual stack subnet
	dualExcludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	dualSubnet, err := NewSubnet("dualSubnet1", "10.0.0.0/24,2001:db8::/64", dualExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, dualSubnet)
	ipam.Subnets["dualSubnet1"] = dualSubnet
	dualPodName := "pod3.default"
	dualNicName := "pod3.default"
	v4, v6, macStr, err = ipam.GetRandomAddress(dualPodName, dualNicName, mac, "dualSubnet1", "", nil, true)
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
	v6ExcludeIps = []string{
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	v6Subnet, err = NewSubnet("v6Subnet", "2001:db8::/64", v6ExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, v6Subnet)
	ipam.Subnets["v6Subnet"] = v6Subnet
	v6PodName = "pod5.default"
	v6NicName = "pod5.default"
	skippedAddrs = []string{"2001:db8::1", "2001:db8::3"}
	v4, v6, macStr, err = ipam.GetRandomAddress(v6PodName, v6NicName, mac, "v6Subnet", "", skippedAddrs, true)
	require.NoError(t, err)
	require.Empty(t, v4)
	require.Equal(t, "2001:db8::5", v6)
	require.NotEmpty(t, macStr)
	// test dual stack subnet with skipped addresses
	dualExcludeIps = []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	dualSubnet, err = NewSubnet("dualSubnet2", "10.0.0.0/24,2001:db8::/64", dualExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, dualSubnet)
	ipam.Subnets["dualSubnet2"] = dualSubnet
	dualPodName = "pod6.default"
	dualNicName = "pod6.default"
	skippedAddrs = []string{"10.0.0.1", "10.0.0.3", "2001:db8::1", "2001:db8::3"}
	v4, v6, macStr, err = ipam.GetRandomAddress(dualPodName, dualNicName, mac, "dualSubnet2", "", skippedAddrs, true)
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
	v6ExcludeIps := []string{
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	v6Subnet, err := NewSubnet("v6Subnet", "2001:db8::/64", v6ExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, v6Subnet)
	ipam.Subnets["v6Subnet"] = v6Subnet
	v6PodName := "pod3.default"
	v6NicName := "pod3.default"
	v6StaticIP := "2001:db8::1"
	v4, v6, macStr, err = ipam.GetStaticAddress(v6PodName, v6NicName, v6StaticIP, mac, "v6Subnet", true)
	require.NoError(t, err)
	require.Empty(t, v4)
	require.Equal(t, v6StaticIP, v6)
	require.NotEmpty(t, macStr)
	// should conflict with v6 static ip
	v6PodName = "pod4.default"
	v6NicName = "pod4.default"
	v6StaticIP = "2001:db8::1"
	v4, v6, macStr, err = ipam.GetStaticAddress(v6PodName, v6NicName, v6StaticIP, mac, "v6Subnet", true)
	require.Error(t, err)
	require.Empty(t, v4)
	require.Empty(t, v6)
	require.Empty(t, macStr)

	// test dual stack subnet
	dualExcludeIps := []string{
		"10.0.0.2", "10.0.0.4", "10.0.0.100",
		"10.0.0.252", "10.0.0.253", "10.0.0.254",
		"2001:db8::2", "2001:db8::4", "2001:db8::100",
		"2001:db8::252", "2001:db8::253", "2001:db8::254",
	}
	dualSubnet, err := NewSubnet("dualSubnet1", "10.0.0.0/24,2001:db8::/64", dualExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, dualSubnet)
	ipam.Subnets["dualSubnet1"] = dualSubnet
	dualPodName := "pod5.default"
	dualNicName := "pod5.default"
	dualStaticIP := "10.0.0.1,2001:db8::1"
	v4, v6, macStr, err = ipam.GetStaticAddress(dualPodName, dualNicName, dualStaticIP, mac, "dualSubnet1", true)
	require.NoError(t, err)
	require.Equal(t, v4StaticIP, v4)
	require.Equal(t, v6StaticIP, v6)
	require.NotEmpty(t, macStr)
	// should conflict with v4 static ip
	dualPodName = "pod6.default"
	dualNicName = "pod6.default"
	dualStaticIP = "10.0.0.1,2001:db8::3"
	v4, v6, macStr, err = ipam.GetStaticAddress(dualPodName, dualNicName, dualStaticIP, mac, "dualSubnet1", true)
	require.Error(t, err)
	require.Empty(t, v4)
	require.Empty(t, v6)
	require.Empty(t, macStr)
	// should conflict with v6 static ip
	dualPodName = "pod7.default"
	dualNicName = "pod7.default"
	dualStaticIP = "10.0.0.3,2001:db8::1"
	v4, v6, macStr, err = ipam.GetStaticAddress(dualPodName, dualNicName, dualStaticIP, mac, "dualSubnet1", true)
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
	dualSubnet, err := NewSubnet("dualSubnet1", "10.0.0.0/24,2001:db8::/64", dualExcludeIps)
	require.NoError(t, err)
	require.NotNil(t, dualSubnet)
	ipam.Subnets["dualSubnet1"] = dualSubnet
	// test only v4 ip given
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
	// test only v6 ip given
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
