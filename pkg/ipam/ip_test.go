package ipam

import (
	"fmt"
	"math/rand/v2"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewIP(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    IP
		wantErr bool
	}{
		{
			name:    "valid IPv4 address",
			input:   "192.168.1.1",
			want:    IP(net.ParseIP("192.168.1.1").To4()),
			wantErr: false,
		},
		{
			name:    "valid IPv6 address",
			input:   "2001:db8::1",
			want:    IP(net.ParseIP("2001:db8::1")),
			wantErr: false,
		},
		{
			name:    "invalid IP address",
			input:   "invalid",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewIP(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewIP(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !got.Equal(tt.want) {
				t.Errorf("NewIP(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIPClone(t *testing.T) {
	ip := IP(net.ParseIP("192.168.1.1"))
	clone := ip.Clone()

	if !clone.Equal(ip) {
		t.Errorf("Clone() = %v, want %v", clone, ip)
	}

	clone[0] = 10
	if clone.Equal(ip) {
		t.Errorf("Clone() should create a new copy, but it modified the original IP")
	}
}

func TestIPLessThan(t *testing.T) {
	tests := []struct {
		name string
		a    IP
		b    IP
		want bool
	}{
		{
			name: "IPv4 less than",
			a:    IP(net.ParseIP("192.168.1.1")),
			b:    IP(net.ParseIP("192.168.1.2")),
			want: true,
		},
		{
			name: "IPv6 less than",
			a:    IP(net.ParseIP("2001:db8::1")),
			b:    IP(net.ParseIP("2001:db8::2")),
			want: true,
		},
		{
			name: "equal IPs",
			a:    IP(net.ParseIP("192.168.1.1")),
			b:    IP(net.ParseIP("192.168.1.1")),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.LessThan(tt.b); got != tt.want {
				t.Errorf("%v.LessThan(%v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestIPGreaterThan(t *testing.T) {
	tests := []struct {
		name string
		a    IP
		b    IP
		want bool
	}{
		{
			name: "IPv4 greater than",
			a:    IP(net.ParseIP("192.168.1.2")),
			b:    IP(net.ParseIP("192.168.1.1")),
			want: true,
		},
		{
			name: "IPv6 greater than",
			a:    IP(net.ParseIP("2001:db8::2")),
			b:    IP(net.ParseIP("2001:db8::1")),
			want: true,
		},
		{
			name: "equal IPs",
			a:    IP(net.ParseIP("192.168.1.1")),
			b:    IP(net.ParseIP("192.168.1.1")),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.GreaterThan(tt.b); got != tt.want {
				t.Errorf("%v.GreaterThan(%v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestIPAdd(t *testing.T) {
	tests := []struct {
		name string
		a    IP
		n    int64
		want IP
	}{
		{
			name: "IPv4 add",
			a:    IP(net.ParseIP("192.168.1.1")),
			n:    1,
			want: IP(net.ParseIP("192.168.1.2")),
		},
		{
			name: "IPv4 add 2",
			a:    IP(net.ParseIP("192.168.1.1")),
			n:    10,
			want: IP(net.ParseIP("192.168.1.11")),
		},
		{
			name: "IPv6 add",
			a:    IP(net.ParseIP("2001:db8::1")),
			n:    1,
			want: IP(net.ParseIP("2001:db8::2")),
		},
		{
			name: "IPv6 add 2",
			a:    IP(net.ParseIP("1:db8::1")),
			n:    1,
			want: IP(net.ParseIP("1:db8::2")),
		},
		{
			name: "IPv4 add overflow",
			a:    IP(net.ParseIP("255.255.255.255")),
			n:    1,
			want: IP(net.ParseIP("0.0.0.0")),
		},
		{
			name: "IPv4 add overflow 2",
			a:    IP(net.ParseIP("255.255.255.254")),
			n:    2,
			want: IP(net.ParseIP("0.0.0.0")),
		},
		{
			name: "IPv4 add overflow 3",
			a:    IP(net.ParseIP("255.255.255.254")),
			n:    3,
			want: IP(net.ParseIP("0.0.0.1")),
		},
		{
			name: "IPv6 add overflow",
			a:    IP(net.ParseIP("ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff")),
			n:    1,
			want: IP(net.ParseIP("::")),
		},
		{
			name: "IPv6 add overflow 2",
			a:    IP(net.ParseIP("ffff:ffff:ffff:ffff:ffff:ffff:ffff:fffe")),
			n:    2,
			want: IP(net.ParseIP("::")),
		},
		{
			name: "IPv6 add overflow 3",
			a:    IP(net.ParseIP("ffff:ffff:ffff:ffff:ffff:ffff:ffff:fffe")),
			n:    3,
			want: IP(net.ParseIP("::1")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Add(tt.n); !got.Equal(tt.want) {
				t.Errorf("%v.Add(%d) = %v, want %v", tt.a, tt.n, got, tt.want)
			}
		})
	}
}

func TestIPSub(t *testing.T) {
	tests := []struct {
		name string
		a    IP
		n    int64
		want IP
	}{
		{
			name: "IPv4 sub",
			a:    IP(net.ParseIP("192.168.1.2")),
			n:    1,
			want: IP(net.ParseIP("192.168.1.1")),
		},
		{
			name: "IPv6 sub",
			a:    IP(net.ParseIP("2001:db8::2")),
			n:    1,
			want: IP(net.ParseIP("2001:db8::1")),
		},
		{
			name: "IPv4 sub underflow",
			a:    IP(net.ParseIP("0.0.0.0")),
			n:    1,
			want: IP(net.ParseIP("255.255.255.255")),
		},
		{
			name: "IPv4 sub underflow 2",
			a:    IP(net.ParseIP("0.0.0.1")),
			n:    2,
			want: IP(net.ParseIP("255.255.255.255")),
		},
		{
			name: "IPv4 sub underflow 3",
			a:    IP(net.ParseIP("0.0.0.1")),
			n:    3,
			want: IP(net.ParseIP("255.255.255.254")),
		},
		{
			name: "IPv6 sub underflow",
			a:    IP(net.ParseIP("::")),
			n:    1,
			want: IP(net.ParseIP("ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff")),
		},
		{
			name: "IPv6 sub underflow 2",
			a:    IP(net.ParseIP("::1")),
			n:    2,
			want: IP(net.ParseIP("ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff")),
		},
		{
			name: "IPv6 sub underflow 3",
			a:    IP(net.ParseIP("::1")),
			n:    3,
			want: IP(net.ParseIP("ffff:ffff:ffff:ffff:ffff:ffff:ffff:fffe")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Sub(tt.n); !got.Equal(tt.want) {
				t.Errorf("%v.Sub(%d) = %v, want %v", tt.a, tt.n, got, tt.want)
			}
		})
	}
}

func TestBytes2IP(t *testing.T) {
	tests := []struct {
		name   string
		buff   []byte
		length int
		want   IP
	}{
		{
			name:   "valid IPv4 address",
			buff:   []byte{192, 168, 1, 1},
			length: 4,
			want:   IP(net.ParseIP("192.168.1.1").To4()),
		},
		{
			name:   "valid IPv6 address",
			buff:   []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
			length: 16,
			want:   IP(net.ParseIP("2001:db8::1")),
		},
		{
			name:   "buffer shorter than length",
			buff:   []byte{192, 168, 1},
			length: 4,
			want:   IP(net.ParseIP("0.192.168.1").To4()),
		},
		{
			name:   "buffer longer than length",
			buff:   []byte{192, 168, 1, 1, 2, 3, 4},
			length: 4,
			want:   IP(net.ParseIP("1.2.3.4").To4()),
		},
		{
			name:   "empty buffer",
			buff:   []byte{},
			length: 4,
			want:   IP(net.ParseIP("0.0.0.0").To4()),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bytes2IP(tt.buff, tt.length)
			if !got.Equal(tt.want) {
				t.Errorf("bytes2IP(%v, %d) = %v, want %v", tt.buff, tt.length, got, tt.want)
			}
		})
	}
}

func TestIPTo4(t *testing.T) {
	tests := []struct {
		name string
		ip   IP
		want net.IP
	}{
		{
			name: "IPv4 address",
			ip:   IP(net.ParseIP("192.168.1.1")),
			want: net.ParseIP("192.168.1.1").To4(),
		},
		{
			name: "IPv6 address",
			ip:   IP(net.ParseIP("2001:db8::1")),
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ip.To4()
			if !got.Equal(tt.want) {
				t.Errorf("%v.To4() = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestIPTo16(t *testing.T) {
	tests := []struct {
		name string
		ip   IP
		want net.IP
	}{
		{
			name: "IPv4 address",
			ip:   IP(net.ParseIP("192.168.1.1").To4()),
			want: net.ParseIP("192.168.1.1"),
		},
		{
			name: "IPv6 address",
			ip:   IP(net.ParseIP("2001:db8::1")),
			want: net.ParseIP("2001:db8::1"),
		},
		{
			name: "nil IP",
			ip:   nil,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ip.To16()
			if !got.Equal(tt.want) {
				t.Errorf("IP.To16() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPString(t *testing.T) {
	tests := []struct {
		name string
		ip   IP
		want string
	}{
		{
			name: "IPv4 address",
			ip:   IP(net.ParseIP("192.168.1.1")),
			want: "192.168.1.1",
		},
		{
			name: "IPv6 address",
			ip:   IP(net.ParseIP("2001:db8::1")),
			want: "2001:db8::1",
		},
		{
			name: "nil IP",
			ip:   nil,
			want: "<nil>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ip.String(); got != tt.want {
				t.Errorf("IP.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAddOrUpdateSubnet(t *testing.T) {
	// ipv4
	// invalid v4 cidr mask len > 32
	ipam := NewIPAM()
	subnetName := "v4InvalidMaskSubnet"
	v4Gw := "1.1.1.1"
	maskV4Length := rand.Int() + 32
	err := ipam.AddOrUpdateSubnet(subnetName, fmt.Sprintf("1.1.1.0/%d", maskV4Length), v4Gw, nil)
	require.EqualError(t, err, ErrInvalidCIDR.Error())

	// invalid v4 ip range
	subnetName = "v4InvalidRangeSubnet1"
	invalidV4Ip := fmt.Sprintf("%d.1.1.0/24", rand.Int()+256)
	err = ipam.AddOrUpdateSubnet(subnetName, invalidV4Ip, v4Gw, nil)
	require.EqualError(t, err, ErrInvalidCIDR.Error())

	subnetName = "v4InvalidRangeSubnet2"
	invalidV4Ip = fmt.Sprintf("1.%d.1.0/24", rand.Int()+256)
	err = ipam.AddOrUpdateSubnet(subnetName, invalidV4Ip, v4Gw, nil)
	require.EqualError(t, err, ErrInvalidCIDR.Error())

	subnetName = "v4InvalidRangeSubnet3"
	invalidV4Ip = fmt.Sprintf("1.1.%d.0/24", rand.Int()+256)
	err = ipam.AddOrUpdateSubnet(subnetName, invalidV4Ip, v4Gw, nil)
	require.EqualError(t, err, ErrInvalidCIDR.Error())

	// normal subnet
	// create pod with static ip
	ipv4CIDR := "1.1.1.0/24"
	v4Gw = "1.1.1.1"
	ipv4ExcludeIPs := []string{"1.1.1.10", "1.1.1.100", "1.1.1.200"}
	subnetName = "v4NormalSubnet"
	err = ipam.AddOrUpdateSubnet(subnetName, ipv4CIDR, v4Gw, ipv4ExcludeIPs)
	require.NoError(t, err)
	require.Equal(t, v4Gw, ipam.Subnets[subnetName].V4Gw)
	require.Equal(t, ipv4CIDR, ipam.Subnets[subnetName].V4CIDR.String())
	require.Equal(t, len(ipv4ExcludeIPs), len(ipam.Subnets[subnetName].V4Reserved.ranges))

	pod1 := "pod1.ns"
	pod1Nic1 := "pod1nic1.ns"
	freeIP1 := ipam.Subnets[subnetName].V4Free.At(0).Start().String()
	ip, _, _, err := ipam.GetStaticAddress(pod1, pod1Nic1, freeIP1, nil, subnetName, true)
	require.NoError(t, err)
	require.Equal(t, freeIP1, ip)

	ip, _, _, err = ipam.GetRandomAddress(pod1, pod1Nic1, nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, freeIP1, ip)

	// create multiple ips on one pod
	pod2 := "pod2.ns"
	pod2Nic1 := "pod2Nic1.ns"
	pod2Nic2 := "pod2Nic2.ns"

	freeIP2 := ipam.Subnets[subnetName].V4Free.At(0).Start().String()
	ip, _, _, err = ipam.GetRandomAddress(pod2, pod2Nic1, nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, freeIP2, ip)

	freeIP3 := ipam.Subnets[subnetName].V4Free.At(0).Start().String()
	ip, _, _, err = ipam.GetRandomAddress(pod2, pod2Nic2, nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, freeIP3, ip)

	addresses := ipam.GetPodAddress(pod2)
	require.Len(t, addresses, 2)
	require.ElementsMatch(t, []string{addresses[0].IP, addresses[1].IP}, []string{freeIP2, freeIP3})
	require.True(t, ipam.ContainAddress(freeIP2))
	require.True(t, ipam.ContainAddress(freeIP3))

	_, isIPAssigned := ipam.IsIPAssignedToOtherPod(freeIP2, subnetName, pod2)
	require.False(t, isIPAssigned)

	_, isIPAssigned = ipam.IsIPAssignedToOtherPod(freeIP3, subnetName, pod2)
	require.False(t, isIPAssigned)

	assignedPod, isIPAssigned := ipam.IsIPAssignedToOtherPod(freeIP1, subnetName, pod2)
	require.True(t, isIPAssigned)
	require.Equal(t, pod1, assignedPod)

	// get static ip conflict with ip in use
	pod3 := "pod3.ns"
	pod3Nic1 := "pod3Nic1.ns"
	_, _, _, err = ipam.GetStaticAddress(pod3, pod3Nic1, freeIP3, nil, subnetName, true)
	require.EqualError(t, err, ErrConflict.Error())

	// release pod with multiple nics
	ipam.ReleaseAddressByPod(pod2, "")
	ip2, err := NewIP(freeIP2)
	require.NoError(t, err)
	ip3, err := NewIP(freeIP3)
	require.NoError(t, err)
	require.True(t, ipam.Subnets[subnetName].IPPools[""].V4Released.Contains(ip2))
	require.True(t, ipam.Subnets[subnetName].IPPools[""].V4Released.Contains(ip3))

	// release pod with single nic
	ipam.ReleaseAddressByPod(pod1, "")
	ip1, err := NewIP(freeIP1)
	require.NoError(t, err)
	require.True(t, ipam.Subnets[subnetName].IPPools[""].V4Released.Contains(ip1))

	// create new pod with released ips
	pod4 := "pod4.ns"
	pod4Nic1 := "pod4Nic1.ns"
	_, _, _, err = ipam.GetStaticAddress(pod4, pod4Nic1, freeIP1, nil, subnetName, true)
	require.NoError(t, err)

	// create pod with no initialized subnet
	pod5 := "pod5.ns"
	pod5Nic1 := "pod5Nic1.ns"

	_, _, _, err = ipam.GetRandomAddress(pod5, pod5Nic1, nil, "invalid_subnet", "", nil, true)
	require.EqualError(t, err, ErrNoSubnet.Error())

	// change cidr
	ipv4CIDR = "10.16.0.0/16"
	v4Gw = "10.16.0.1"
	subnetName = "v4ChangeCIDRSubnet"
	err = ipam.AddOrUpdateSubnet(subnetName, ipv4CIDR, v4Gw, ipv4ExcludeIPs)
	require.NoError(t, err)

	ipv4CIDR = "10.17.0.0/16"
	v4Gw = "10.17.0.1"
	err = ipam.AddOrUpdateSubnet(subnetName, ipv4CIDR, v4Gw, []string{"10.17.0.1"})
	require.NoError(t, err)
	ip, _, _, err = ipam.GetRandomAddress("pod5.ns", "pod5.ns", nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, ip, "10.17.0.2")

	// update to be invalid cidr, subnet should not change
	err = ipam.AddOrUpdateSubnet(subnetName, "1.1.256.1", v4Gw, nil)
	require.EqualError(t, err, ErrInvalidCIDR.Error())
	require.Equal(t, ipam.Subnets[subnetName].V4CIDR.IP.String(), "10.17.0.0")

	// reuse released address when no unused address
	subnetName = "v4ReuseReleasedAddressSubnet"
	ipv4CIDR = "10.16.0.0/30"
	v4Gw = "10.16.0.1"
	err = ipam.AddOrUpdateSubnet(subnetName, ipv4CIDR, v4Gw, nil)
	require.NoError(t, err)

	ip, _, _, err = ipam.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, ip, "10.16.0.1")

	ipam.ReleaseAddressByPod("pod1.ns", "")
	ip, _, _, err = ipam.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, ip, "10.16.0.2")

	ipam.ReleaseAddressByPod("pod1.ns", "")
	ip, _, _, err = ipam.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, ip, "10.16.0.1")

	// do not reuse released address after update subnet's excludedIps
	subnetName = "v4NotReuseReleasedAddressSubnet"
	ipv4CIDR = "10.16.0.0/30"
	v4Gw = "10.16.0.1"
	err = ipam.AddOrUpdateSubnet(subnetName, ipv4CIDR, v4Gw, nil)
	require.NoError(t, err)

	ip, _, _, err = ipam.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, ip, "10.16.0.1")

	ipam.ReleaseAddressByPod("pod1.ns", "")
	err = ipam.AddOrUpdateSubnet(subnetName, ipv4CIDR, v4Gw, []string{"10.16.0.1..10.16.0.2"})
	require.NoError(t, err)

	_, _, _, err = ipam.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
	require.EqualError(t, err, ErrNoAvailable.Error())

	//  do not count excludedIps as subnet's v4availableIPs and v4usingIPs
	subnetName = "v4ExcludeIPsSubnet"
	ipv4CIDR = "10.16.10.0/28"
	v4Gw = "10.16.10.1"
	err = ipam.AddOrUpdateSubnet(subnetName, ipv4CIDR, v4Gw, []string{"10.16.10.1", "10.16.10.10"})
	require.NoError(t, err)

	ip, _, _, err = ipam.GetStaticAddress("pod1.ns", "pod1.ns", "10.16.10.10", nil, subnetName, true)
	require.NoError(t, err)
	require.Equal(t, ip, "10.16.10.10")

	v4UsingIPStr, _, v4AvailableIPStr, _ := ipam.GetSubnetIPRangeString(subnetName, []string{"10.16.10.10"})
	require.Equal(t, v4UsingIPStr, "")
	require.Equal(t, v4AvailableIPStr, "10.16.10.2-10.16.10.9,10.16.10.11-10.16.10.14")

	err = ipam.AddOrUpdateSubnet(subnetName, "10.16.10.0/28", "10.16.10.1", []string{"10.16.10.1"})
	require.NoError(t, err)
	v4UsingIPStr, _, v4AvailableIPStr, _ = ipam.GetSubnetIPRangeString(subnetName, nil)
	require.Equal(t, v4UsingIPStr, "10.16.10.10")
	require.Equal(t, v4AvailableIPStr, "10.16.10.2-10.16.10.9,10.16.10.11-10.16.10.14")

	// IPv6

	// invalid v6 cidr mask len > 128
	subnetName = "v6InvalidMaskSubnet"
	v6Gw := "fd00::1"
	maskV6Length := rand.Int() + 128
	err = ipam.AddOrUpdateSubnet(subnetName, fmt.Sprintf("fd00::/%d", maskV6Length), v6Gw, nil)
	require.EqualError(t, err, ErrInvalidCIDR.Error())

	// invalid v6 ip range
	subnetName = "v6InvalidRangeSubnet1"
	invalidV6Ip := fmt.Sprintf("fd00::%d::/120", rand.Int()+1)
	err = ipam.AddOrUpdateSubnet(subnetName, invalidV6Ip, v6Gw, nil)
	require.EqualError(t, err, ErrInvalidCIDR.Error())

	// normal subnet
	// create pod with static ip
	ipv6CIDR := "fd00::/120"
	v6Gw = "fd00::1"
	ipv6ExcludeIPs := []string{"fd00::10", "fd00::20", "fd00::30"}
	subnetName = "v6NormalSubnet"
	err = ipam.AddOrUpdateSubnet(subnetName, ipv6CIDR, v6Gw, ipv6ExcludeIPs)
	require.NoError(t, err)
	require.Equal(t, v6Gw, ipam.Subnets[subnetName].V6Gw)
	require.Equal(t, ipv6CIDR, ipam.Subnets[subnetName].V6CIDR.String())
	require.Equal(t, 3, len(ipv6ExcludeIPs))
	require.Equal(t, ipam.Subnets[subnetName].V6Reserved.ranges[0].String(), "fd00::10")
	require.Equal(t, ipam.Subnets[subnetName].V6Reserved.ranges[1].String(), "fd00::20")
	require.Equal(t, ipam.Subnets[subnetName].V6Reserved.ranges[2].String(), "fd00::30")

	// require.Equal(t, nil, ipam.Subnets[subnetName].V6Reserved.ranges)

	pod1 = "pod1.ns"
	pod1Nic1 = "pod1nic1.ns"
	freeIP1 = ipam.Subnets[subnetName].V6Free.At(0).Start().String()
	_, ip, _, err = ipam.GetStaticAddress(pod1, pod1Nic1, freeIP1, nil, subnetName, true)
	require.NoError(t, err)
	require.Equal(t, freeIP1, ip)

	_, ip, _, err = ipam.GetRandomAddress(pod1, pod1Nic1, nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, freeIP1, ip)

	// create multiple ips on one pod
	pod2 = "pod2.ns"
	pod2Nic1 = "pod2Nic1.ns"
	pod2Nic2 = "pod2Nic2.ns"

	freeIP2 = ipam.Subnets[subnetName].V6Free.At(0).Start().String()
	_, ip, _, err = ipam.GetRandomAddress(pod2, pod2Nic1, nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, freeIP2, ip)

	freeIP3 = ipam.Subnets[subnetName].V6Free.At(0).Start().String()
	_, ip, _, err = ipam.GetRandomAddress(pod2, pod2Nic2, nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, freeIP3, ip)

	addresses = ipam.GetPodAddress(pod2)
	require.Len(t, addresses, 2)
	require.ElementsMatch(t, []string{addresses[0].IP, addresses[1].IP}, []string{freeIP2, freeIP3})
	require.True(t, ipam.ContainAddress(freeIP2))
	require.True(t, ipam.ContainAddress(freeIP3))

	_, isIPAssigned = ipam.IsIPAssignedToOtherPod(freeIP2, subnetName, pod2)
	require.False(t, isIPAssigned)

	_, isIPAssigned = ipam.IsIPAssignedToOtherPod(freeIP3, subnetName, pod2)
	require.False(t, isIPAssigned)

	assignedPod, isIPAssigned = ipam.IsIPAssignedToOtherPod(freeIP1, subnetName, pod2)
	require.True(t, isIPAssigned)
	require.Equal(t, pod1, assignedPod)

	// get static ip conflict with ip in use
	pod3 = "pod3.ns"
	pod3Nic1 = "pod3Nic1.ns"
	_, _, _, err = ipam.GetStaticAddress(pod3, pod3Nic1, freeIP3, nil, subnetName, true)
	require.EqualError(t, err, ErrConflict.Error())

	// release pod with multiple nics
	ipam.ReleaseAddressByPod(pod2, "")
	ip2, err = NewIP(freeIP2)
	require.NoError(t, err)
	ip3, err = NewIP(freeIP3)
	require.NoError(t, err)
	require.True(t, ipam.Subnets[subnetName].IPPools[""].V6Released.Contains(ip2))
	require.True(t, ipam.Subnets[subnetName].IPPools[""].V6Released.Contains(ip3))

	// release pod with single nic
	ipam.ReleaseAddressByPod(pod1, "")
	ip1, err = NewIP(freeIP1)
	require.NoError(t, err)
	require.True(t, ipam.Subnets[subnetName].IPPools[""].V6Released.Contains(ip1))

	// create new pod with released ips
	pod4 = "pod4.ns"
	pod4Nic1 = "pod4Nic1.ns"
	_, _, _, err = ipam.GetStaticAddress(pod4, pod4Nic1, freeIP1, nil, subnetName, true)
	require.NoError(t, err)

	// create pod with no initialized subnet
	pod5 = "pod5.ns"
	pod5Nic1 = "pod5Nic1.ns"

	_, _, _, err = ipam.GetRandomAddress(pod5, pod5Nic1, nil, "invalid_subnet", "", nil, true)
	require.EqualError(t, err, ErrNoSubnet.Error())

	// change cidr
	ipv6CIDR = "fe00::/112"
	v6Gw = "fd00::1"
	err = ipam.AddOrUpdateSubnet(subnetName, ipv6CIDR, v6Gw, ipv6ExcludeIPs)
	require.NoError(t, err)

	err = ipam.AddOrUpdateSubnet(subnetName, ipv6CIDR, v6Gw, []string{"fe00::1"})
	require.NoError(t, err)
	_, ip, _, err = ipam.GetRandomAddress("pod5.ns", "pod5.ns", nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, ip, "fe00::2")

	// update to be invalid cidr, subnet should not change
	err = ipam.AddOrUpdateSubnet(subnetName, "fd00::g/120", v6Gw, nil)
	require.EqualError(t, err, ErrInvalidCIDR.Error())
	require.Equal(t, ipam.Subnets[subnetName].V6CIDR.IP.String(), "fe00::")

	// reuse released address when no unused address
	subnetName = "v6ReuseReleasedAddressSubnet"
	ipv6CIDR = "fd00::/126"
	v6Gw = "fd00::1"
	err = ipam.AddOrUpdateSubnet(subnetName, ipv6CIDR, v6Gw, nil)
	require.NoError(t, err)

	_, ip, _, err = ipam.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, ip, "fd00::1")

	ipam.ReleaseAddressByPod("pod1.ns", "")
	_, ip, _, err = ipam.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, ip, "fd00::2")

	ipam.ReleaseAddressByPod("pod1.ns", "")
	_, ip, _, err = ipam.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, ip, "fd00::1")

	// do not reuse released address after update subnet's excludedIps
	subnetName = "v6NotReuseReleasedAddressSubnet"
	ipv6CIDR = "fd00::/126"
	v6Gw = "fd00::1"
	err = ipam.AddOrUpdateSubnet(subnetName, ipv6CIDR, v6Gw, nil)
	require.NoError(t, err)

	_, ip, _, err = ipam.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, ip, "fd00::1")

	ipam.ReleaseAddressByPod("pod1.ns", "")
	err = ipam.AddOrUpdateSubnet(subnetName, ipv6CIDR, v6Gw, []string{"fd00::1..fd00::2"})
	require.NoError(t, err)

	_, _, _, err = ipam.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
	require.EqualError(t, err, ErrNoAvailable.Error())

	// DualStack
	// invalid subnet
	subnetName = "dualInvalidSubnet"
	ipv6CIDR = "fd00::/120"
	dualGw := "fd00::1"
	err = ipam.AddOrUpdateSubnet(subnetName, "1.1.1.1/64,"+ipv6CIDR, dualGw, nil)
	require.EqualError(t, err, ErrInvalidCIDR.Error())
	err = ipam.AddOrUpdateSubnet(subnetName, "1.1.256.1/24,"+ipv6CIDR, dualGw, nil)
	require.EqualError(t, err, ErrInvalidCIDR.Error())
	err = ipam.AddOrUpdateSubnet(subnetName, ipv4CIDR+",fd00::/130", dualGw, nil)
	require.EqualError(t, err, ErrInvalidCIDR.Error())
	err = ipam.AddOrUpdateSubnet(subnetName, ipv4CIDR+",fd00::g/120", dualGw, nil)
	require.EqualError(t, err, ErrInvalidCIDR.Error())

	// normal dual subnet
	// create pod with static ip
	dualCIDR := "10.0.0.0/24,fd00::/120"
	dualGw = "10.0.0.1,fd00::1"
	dualExcludeIPs := []string{"10.0.0.10", "10.0.0.100", "10.0.0.200", "fd00::10", "fd00::20", "fd00::30"}
	err = ipam.AddOrUpdateSubnet(subnetName, dualCIDR, dualGw, dualExcludeIPs)
	require.NoError(t, err)
	require.Contains(t, dualGw, ipam.Subnets[subnetName].V4Gw)
	require.Contains(t, dualGw, ipam.Subnets[subnetName].V6Gw)
	require.Contains(t, dualCIDR, ipam.Subnets[subnetName].V4CIDR.String())
	require.Contains(t, dualCIDR, ipam.Subnets[subnetName].V6CIDR.String())
	require.Equal(t, len(dualExcludeIPs), len(ipam.Subnets[subnetName].V4Reserved.ranges)+len(ipam.Subnets[subnetName].V6Reserved.ranges))

	pod1 = "pod1.ns"
	pod1Nic1 = "pod1nic1.ns"
	freeIP41 := ipam.Subnets[subnetName].V4Free.At(0).Start().String()
	freeIP61 := ipam.Subnets[subnetName].V6Free.At(0).Start().String()
	dualIP := fmt.Sprintf("%s,%s", freeIP41, freeIP61)
	ip4, ip6, _, err := ipam.GetStaticAddress(pod1, pod1Nic1, dualIP, nil, subnetName, true)
	require.NoError(t, err)
	require.Equal(t, freeIP41, ip4)
	require.Equal(t, freeIP61, ip6)

	ip4, ip6, _, err = ipam.GetRandomAddress(pod1, pod1Nic1, nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, freeIP41, ip4)
	require.Equal(t, freeIP61, ip6)

	// create multiple ips on one pod
	pod2 = "pod2.ns"
	pod2Nic1 = "pod2Nic1.ns"
	pod2Nic2 = "pod2Nic2.ns"

	freeIP42 := ipam.Subnets[subnetName].V4Free.At(0).Start().String()
	freeIP62 := ipam.Subnets[subnetName].V6Free.At(0).Start().String()
	ip4, ip6, _, err = ipam.GetRandomAddress(pod2, pod2Nic1, nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, freeIP42, ip4)
	require.Equal(t, freeIP62, ip6)

	freeIP43 := ipam.Subnets[subnetName].V4Free.At(0).Start().String()
	freeIP63 := ipam.Subnets[subnetName].V6Free.At(0).Start().String()
	ip4, ip6, _, err = ipam.GetRandomAddress(pod2, pod2Nic2, nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, freeIP43, ip4)
	require.Equal(t, freeIP63, ip6)

	addresses = ipam.GetPodAddress(pod2)
	require.Len(t, addresses, 4)
	require.ElementsMatch(t, []string{addresses[0].IP, addresses[1].IP, addresses[2].IP, addresses[3].IP}, []string{freeIP42, freeIP62, freeIP43, freeIP63})
	require.True(t, ipam.ContainAddress(freeIP42))
	require.True(t, ipam.ContainAddress(freeIP43))
	require.True(t, ipam.ContainAddress(freeIP62))
	require.True(t, ipam.ContainAddress(freeIP63))

	_, isIPAssigned = ipam.IsIPAssignedToOtherPod(freeIP42, subnetName, pod2)
	require.False(t, isIPAssigned)

	_, isIPAssigned = ipam.IsIPAssignedToOtherPod(freeIP43, subnetName, pod2)
	require.False(t, isIPAssigned)

	_, isIPAssigned = ipam.IsIPAssignedToOtherPod(freeIP62, subnetName, pod2)
	require.False(t, isIPAssigned)

	_, isIPAssigned = ipam.IsIPAssignedToOtherPod(freeIP63, subnetName, pod2)
	require.False(t, isIPAssigned)

	assignedPod, isIPAssigned = ipam.IsIPAssignedToOtherPod(freeIP41, subnetName, pod2)
	require.True(t, isIPAssigned)
	require.Equal(t, pod1, assignedPod)

	// get static ip conflict with ip in use
	pod3 = "pod3.ns"
	pod3Nic1 = "pod3Nic1.ns"
	_, _, _, err = ipam.GetStaticAddress(pod3, pod3Nic1, freeIP43, nil, subnetName, true)
	require.EqualError(t, err, ErrConflict.Error())

	_, _, _, err = ipam.GetStaticAddress(pod3, pod3Nic1, freeIP63, nil, subnetName, true)
	require.EqualError(t, err, ErrConflict.Error())

	// release pod with multiple nics
	ipam.ReleaseAddressByPod(pod2, "")
	ip42, err := NewIP(freeIP42)
	require.NoError(t, err)
	ip43, err := NewIP(freeIP43)
	require.NoError(t, err)
	ip62, err := NewIP(freeIP62)
	require.NoError(t, err)
	ip63, err := NewIP(freeIP63)
	require.NoError(t, err)
	require.True(t, ipam.Subnets[subnetName].IPPools[""].V4Released.Contains(ip42))
	require.True(t, ipam.Subnets[subnetName].IPPools[""].V4Released.Contains(ip43))
	require.True(t, ipam.Subnets[subnetName].IPPools[""].V6Released.Contains(ip62))
	require.True(t, ipam.Subnets[subnetName].IPPools[""].V6Released.Contains(ip63))

	// release pod with single nic
	ipam.ReleaseAddressByPod(pod1, "")
	ip41, err := NewIP(freeIP41)
	require.NoError(t, err)
	require.True(t, ipam.Subnets[subnetName].IPPools[""].V4Released.Contains(ip41))
	ip61, err := NewIP(freeIP61)
	require.NoError(t, err)
	require.True(t, ipam.Subnets[subnetName].IPPools[""].V6Released.Contains(ip61))

	// create new pod with released ips
	pod4 = "pod4.ns"
	pod4Nic1 = "pod4Nic1.ns"

	_, _, _, err = ipam.GetStaticAddress(pod4, pod4Nic1, freeIP41, nil, subnetName, true)
	require.NoError(t, err)

	_, _, _, err = ipam.GetStaticAddress(pod4, pod4Nic1, freeIP61, nil, subnetName, true)
	require.NoError(t, err)

	// create pod with no initialized subnet
	pod5 = "pod5.ns"
	pod5Nic1 = "pod5Nic1.ns"

	_, _, _, err = ipam.GetRandomAddress(pod5, pod5Nic1, nil, "invalid_subnet", "", nil, true)
	require.EqualError(t, err, ErrNoSubnet.Error())

	// dual stack subnet change cidr
	subnetName = "dualChangeCIDRSubnet"
	dualCIDR = "10.1.0.2/16,fe01::/112"
	dualGw = "10.1.0.1,fe01::1"
	dualExcludeIPs = nil
	err = ipam.AddOrUpdateSubnet(subnetName, dualCIDR, dualGw, dualExcludeIPs)
	require.NoError(t, err)

	err = ipam.AddOrUpdateSubnet(subnetName, "10.17.0.2/16,fe00::/112", dualGw, []string{"10.17.0.1", "fe00::1"})
	require.NoError(t, err)

	ipv4, ipv6, _, err := ipam.GetRandomAddress("pod5.ns", "pod5.ns", nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, ipv4, "10.17.0.2")
	require.Equal(t, ipv6, "fe00::2")

	// reuse released address when no unused address
	err = ipam.AddOrUpdateSubnet(subnetName, "10.16.0.2/30,fd00::/126", dualGw, nil)
	require.NoError(t, err)

	ipv4, ipv6, _, err = ipam.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, ipv4, "10.16.0.1")
	require.Equal(t, ipv6, "fd00::1")

	ipam.ReleaseAddressByPod("pod1.ns", "")
	ipv4, ipv6, _, err = ipam.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, ipv4, "10.16.0.2")
	require.Equal(t, ipv6, "fd00::2")

	ipam.ReleaseAddressByPod("pod1.ns", "")
	ipv4, ipv6, _, err = ipam.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, ipv4, "10.16.0.1")
	require.Equal(t, ipv6, "fd00::1")

	// do not reuse released address after update subnet's excludedIps
	err = ipam.AddOrUpdateSubnet(subnetName, "10.16.0.2/30,fd00::/126", dualGw, nil)
	require.NoError(t, err)

	ipv4, ipv6, _, err = ipam.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
	require.NoError(t, err)
	require.Equal(t, ipv4, "10.16.0.1")
	require.Equal(t, ipv6, "fd00::1")

	ipam.ReleaseAddressByPod("pod1.ns", "")
	err = ipam.AddOrUpdateSubnet(subnetName, "10.16.0.2/30,fd00::/126", dualGw, []string{"10.16.0.1..10.16.0.2", "fd00::1..fd00::2"})
	require.NoError(t, err)

	_, _, _, err = ipam.GetRandomAddress("pod1.ns", "pod1.ns", nil, subnetName, "", nil, true)
	require.EqualError(t, err, ErrNoAvailable.Error())
}
