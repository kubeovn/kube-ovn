package util

import (
	"math/big"
	"net"
	"reflect"
	"testing"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/stretchr/testify/require"
)

func TestCheckSystemCIDR(t *testing.T) {
	cases := []struct {
		name   string
		config []string
		expect string
	}{
		// for all check
		{"1v4", []string{"10.16.0.0/16"}, ""},
		{"v4", []string{"10.16.0.0/16", "10.96.0.0/12", "100.64.0.0/16"}, ""},
		{"dual", []string{"10.16.0.0/16,fd00:10:16::/64", "10.96.0.0/12,fd00:10:96::/112", "100.64.0.0/16,fd00:100:64::/64"}, ""},
		{"v6", []string{"fd00:10:16::/64", "fd00:10:96::/112", "fd00:100:64::/64"}, ""},
		{"169254", []string{"10.16.0.0/16", "10.96.0.0/12", "169.254.0.0/16"}, "169.254.0.0/16 conflict with v4 link local cidr 169.254.0.0/16"},
		{"0000", []string{"10.16.0.0/16", "10.96.0.0/12", "0.0.0.0/16"}, "0.0.0.0/16 conflict with v4 localnet cidr 0.0.0.0/32"},
		{"127", []string{"10.16.0.0/16", "10.96.0.0/12", "127.127.0.0/16"}, "127.127.0.0/16 conflict with v4 loopback cidr 127.0.0.1/8"},
		{"255", []string{"10.16.0.0/16", "10.96.0.0/12", "255.255.0.0/16"}, "255.255.0.0/16 conflict with v4 broadcast cidr 255.255.255.255/32"},
		{"ff80", []string{"10.16.0.0/16,ff80::/64", "10.96.0.0/12,fd00:10:96::/112", "100.64.0.0/16,fd00:100:64::/64"}, "10.16.0.0/16,ff80::/64 conflict with v6 multicast cidr ff00::/8"},
		// overlap only
		{"overlapped", []string{"10.16.0.0/16", "10.96.0.0/12", "10.96.0.2/16"}, "conflict with cidr"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ans := CheckSystemCIDR(c.config)
			if !ErrorContains(ans, c.expect) {
				t.Errorf("%v expected error %v, but %v got",
					c.config, c.expect, ans)
			}
		})
	}
}

func TestCIDROverlap(t *testing.T) {
	cases := []struct {
		name    string
		subnet1 string
		subnet2 string
		expect  bool
	}{
		// for all check
		{"v4", "10.16.0.0/16", "10.96.0.0/12", false},
		{"dual", "10.16.0.0/16,fd00:10:16::/64", "10.96.0.0/12,fd00:10:96::/112", false},
		{"v6", "fd00:10:16::/64", "fd00:10:96::/112", false},
		{"overlap", "10.96.0.0/12", "10.96.0.2/16", true},
		// overlap only, spec ignore
		{"spec", "10.16.0.0/16", "169.254.0.0/12", false},
		{"allerr", "10.16.0/16", "169.254.0/12", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ans := CIDROverlap(c.subnet1, c.subnet2)
			if ans != c.expect {
				t.Errorf("%v and %v expected %v, but %v got",
					c.subnet1, c.subnet2, c.expect, ans)
			}
		})
	}
}

func TestCIDRGlobalUnicast(t *testing.T) {
	cases := []struct {
		name   string
		subnet string
		expect string
	}{
		// for all check
		{"1v4", "10.16.0.0/16", ""},
		{"dual", "10.16.0.0/16,fd00:10:16::/64", ""},
		{"v6", "fd00:10:16::/64", ""},
		{"169254", "169.254.0.0/16", "169.254.0.0/16 conflict with v4 link local cidr 169.254.0.0/16"},
		{"v4mulcast", "224.0.0.0/16", "224.0.0.0/16 conflict with v4 multicast cidr 224.0.0.0/4"},
		{"v6unspeci", "::/128", "::/128 conflict with v6 unspecified cidr ::/128"},
		{"v6loopbak", "::1/128", "::1/128 conflict with v6 loopback cidr ::1/128"},
		{"v6linklocal", "FE80::/10", "FE80::/10 conflict with v6 link local cidr FE80::/10"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ans := CIDRGlobalUnicast(c.subnet)
			if !ErrorContains(ans, c.expect) {
				t.Errorf("%v expected error, but %v got",
					c.subnet, ans)
			}
		})
	}
}

func TestGenerateMac(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{
			name: "correct",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ans := GenerateMac()
			if _, err := net.ParseMAC(ans); err != nil {
				t.Errorf("%v expected %v, but %v got",
					c.name, c.want, ans)
			}
		})
	}
}

func TestIp2BigInt(t *testing.T) {
	tests := []struct {
		expect *big.Int
		ip     string
		name   string
	}{
		{
			name:   "correctv4",
			ip:     "192.168.1.1",
			expect: big.NewInt(0).SetBytes(net.ParseIP("192.168.1.1").To4()),
		},
		{
			name:   "v6",
			ip:     "1050:0:0:0:5:600:300c:326b",
			expect: big.NewInt(0).SetBytes(net.ParseIP("1050:0:0:0:5:600:300c:326b").To16()),
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			if ans := Ip2BigInt(c.ip); !reflect.DeepEqual(ans, c.expect) {
				t.Errorf("%v expected %v, but %v got",
					c.ip, c.expect, ans)
			}
		})
	}
}

func TestBigInt2Ip(t *testing.T) {
	tests := []struct {
		name   string
		ip     *big.Int
		expect string
	}{
		{
			name:   "v4",
			ip:     big.NewInt(0).SetBytes(net.ParseIP("192.168.1.1").To4()),
			expect: "192.168.1.1",
		},
		{
			name:   "v6",
			expect: "1050::5:600:300c:326b",
			ip:     big.NewInt(0).SetBytes(net.ParseIP("1050:0:0:0:5:600:300c:326b").To16()),
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			if ans := BigInt2Ip(c.ip); !reflect.DeepEqual(ans, c.expect) {
				t.Errorf("%v expected %v, but %v got",
					c.ip, c.expect, ans)
			}
		})
	}
}

func TestSubnetNumber(t *testing.T) {
	tests := []struct {
		name   string
		expect string
		subnet string
	}{
		{
			name:   "correct",
			subnet: "192.168.0.1/24",
			expect: "192.168.0.0",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			if ans := SubnetNumber(c.subnet); !reflect.DeepEqual(ans, c.expect) {
				t.Errorf("%v expected %v, but %v got",
					c.subnet, c.expect, ans)
			}
		})
	}
}

func TestSubnetBroadcast(t *testing.T) {
	tests := []struct {
		name   string
		subnet string
		expect string
	}{
		{
			name:   "v4",
			subnet: "192.128.23.1/15",
			expect: "192.129.255.255",
		},
		{
			name:   "v6",
			subnet: "ffff:ffff:ffff:ffff:ffff:0:ffff:ffff/96",
			expect: "ffff:ffff:ffff:ffff:ffff:0:ffff:ffff",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			if ans := SubnetBroadcast(c.subnet); !reflect.DeepEqual(ans, c.expect) {
				t.Errorf("%v expected %v, but %v got",
					c.subnet, c.expect, ans)
			}
		})
	}
}

func TestFirstIP(t *testing.T) {
	tests := []struct {
		name   string
		subnet string
		expect string
		err    string
	}{
		{
			name:   "base",
			subnet: "192.168.0.23/24",
			expect: "192.168.0.1",
			err:    "",
		},
		{
			name:   "controversy",
			subnet: "192.168.0.23/32",
			expect: "192.168.0.24",
			err:    "",
		},
		{
			name:   "subneterr",
			subnet: "192.168.0.0",
			expect: "",
			err:    "192.168.0.0 is not a valid cidr",
		},
		{
			name:   "v6",
			subnet: "ffff:ffff:ffff:ffff:ffff:0:ffff:0/96",
			expect: "ffff:ffff:ffff:ffff:ffff::1",
			err:    "",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ans, err := FirstIP(c.subnet)
			if !reflect.DeepEqual(ans, c.expect) || !ErrorContains(err, c.err) {
				t.Errorf("%v expected %v, %v, but %v, %v got",
					c.subnet, c.expect, c.err, ans, err)
			}
		})
	}
}

func TestLastIP(t *testing.T) {
	tests := []struct {
		name   string
		subnet string
		expect string
		err    string
	}{
		{
			name:   "base",
			subnet: "192.168.0.23/24",
			expect: "192.168.0.254",
			err:    "",
		},
		{
			name:   "subneterr",
			subnet: "192.168.0.0",
			expect: "",
			err:    "192.168.0.0 is not a valid cidr",
		},
		{
			name:   "v6",
			subnet: "ffff:ffff:ffff:ffff:ffff:0:ffff:0/96",
			expect: "ffff:ffff:ffff:ffff:ffff:0:ffff:fffe",
			err:    "",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ans, err := LastIP(c.subnet)
			if !reflect.DeepEqual(ans, c.expect) || !ErrorContains(err, c.err) {
				t.Errorf("%v expected %v, %v, but %v, %v got",
					c.subnet, c.expect, c.err, ans, err)
			}
		})
	}
}

// unstable function
func TestCIDRContainIP(t *testing.T) {
	tests := []struct {
		name    string
		cidrStr string
		ipStr   string
		want    bool
	}{
		{
			name:    "base",
			cidrStr: "192.168.0.23/24",
			ipStr:   "192.168.0.23,192.168.0.254",
			want:    true,
		},
		{
			name:    "baseNoMask",
			cidrStr: "192.168.0.23",
			ipStr:   "192.168.0.23,192.168.0.254",
			want:    false,
		},
		{
			name:    "v6",
			cidrStr: "ffff:ffff:ffff:ffff:ffff:0:ffff:0/96",
			ipStr:   "ffff:ffff:ffff:ffff:ffff:0:ffff:fffe",
			want:    true,
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			if ans := CIDRContainIP(c.cidrStr, c.ipStr); ans != c.want {
				t.Errorf("%v, %v expected %v, but %v got",
					c.cidrStr, c.ipStr, c.want, ans)
			}
		})
	}
}

func TestCheckProtocol(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    string
	}{
		{
			name:    "v4",
			address: "192.168.0.23",
			want:    kubeovnv1.ProtocolIPv4,
		},
		{
			name:    "v6",
			address: "ffff:ffff:ffff:ffff:ffff:0:ffff:fffe",
			want:    kubeovnv1.ProtocolIPv6,
		},
		{
			name:    "dual",
			address: "192.168.0.23,ffff:ffff:ffff:ffff:ffff:0:ffff:fffe",
			want:    kubeovnv1.ProtocolDual,
		},
		{
			name:    "error",
			address: "192.168.0.23,192.168.0.24",
			want:    "",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			if ans := CheckProtocol(c.address); ans != c.want {
				t.Errorf("%v expected %v, but %v got",
					c.address, c.want, ans)
			}
		})
	}
}

func TestAddressCount(t *testing.T) {
	tests := []struct {
		name    string
		network *net.IPNet
		want    float64
	}{
		{
			name: "base",
			network: &net.IPNet{
				IP:   net.ParseIP("192.168.1.0"),
				Mask: net.IPMask{255, 255, 255, 0},
			},
			want: 254,
		},
		{
			name: "baseNoIP",
			network: &net.IPNet{
				IP:   net.ParseIP("192.168.1.0"),
				Mask: net.IPMask{255, 255, 255, 254},
			},
			want: 0,
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			if ans := AddressCount(c.network); ans != c.want {
				t.Errorf("%v expected %v, but %v got",
					c.network, c.want, ans)
			}
		})
	}
}

func TestGenerateRandomV4IP(t *testing.T) {
	tests := []struct {
		name string
		cidr string
		want string
	}{
		{
			name: "base",
			cidr: "10.16.0.0/16",
			want: "10.16.0.0/16",
		},
		{
			name: "wrongcidr",
			cidr: "10.16.0.0",
			want: "",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			_, IPNets, err := net.ParseCIDR(c.cidr)
			if err != nil {
				ans := GenerateRandomV4IP(c.cidr)
				if c.want != ans {
					t.Errorf("%v expected %v, but %v got",
						c.cidr, c.want, ans)
				}

			} else {
				ans := GenerateRandomV4IP(c.cidr)
				if IPNets.Contains(net.ParseIP(GenerateRandomV4IP(c.cidr))) {
					t.Errorf("%v expected %v, but %v got",
						c.cidr, c.want, ans)
				}
			}
		})
	}
}

func TestIPToString(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want string
	}{
		{
			name: "cidr",
			ip:   "10.16.0.1/16",
			want: "10.16.0.1",
		},
		{
			name: "ip",
			ip:   "10.16.0.1",
			want: "10.16.0.1",
		},
		{
			name: "error",
			ip:   "10.16.0",
			want: "",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			if ans := IPToString(c.ip); ans != c.want {
				t.Errorf("%v expected %v, but %v got",
					c.ip, c.want, ans)
			}
		})
	}
}

func TestIsValidIP(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{
			name: "base",
			ip:   "10.16.0.1",
			want: true,
		},
		{
			name: "v6",
			ip:   "::ffff:192.0.2.1",
			want: true,
		},
		{
			name: "err",
			ip:   "10.16.1",
			want: false,
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			if ans := IsValidIP(c.ip); ans != c.want {
				t.Errorf("%v expected %v, but %v got",
					c.ip, c.want, ans)
			}
		})
	}
}

func TestCheckCidrs(t *testing.T) {
	tests := []struct {
		name string
		cidr string
		want string
	}{
		{
			name: "v4",
			cidr: "10.16.0.1/24",
			want: "",
		},
		{
			name: "v6",
			cidr: "::ffff:192.0.2.1/96",
			want: "",
		},
		{
			name: "Morev6",
			cidr: "10.16.0.1/24,::ffff:192.0.2.1/96",
			want: "",
		},
		{
			name: "err",
			cidr: "10.16.1",
			want: "CIDRInvalid",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ans := CheckCidrs(c.cidr)
			if !ErrorContains(ans, c.want) {
				t.Errorf("%v expected %v, but %v got",
					c.cidr, c.want, ans)
			}
		})
	}
}

func TestGetGwByCidr(t *testing.T) {
	tests := []struct {
		name string
		cidr string
		want string
		err  string
	}{
		{
			name: "v4",
			cidr: "10.16.0.112/24",
			want: "10.16.0.1",
			err:  "",
		},
		{
			name: "dual",
			cidr: "10.16.0.112/24,ffff:ffff:ffff:ffff:ffff:0:ffff:0/96",
			want: "10.16.0.1,ffff:ffff:ffff:ffff:ffff::1",
			err:  "",
		},
		{
			name: "err",
			cidr: "10.16.112/24",
			want: "",
			err:  "10.16.112/24 is not a valid cidr",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ans, err := GetGwByCidr(c.cidr)
			if !ErrorContains(err, c.err) || c.want != ans {
				t.Errorf("%v expected %v, %v, but %v, %v got",
					c.cidr, c.want, c.err, ans, err)
			}
		})
	}
}

func TestAppendGwByCidr(t *testing.T) {
	tests := []struct {
		name string
		gw   string
		cidr string
		want string
		err  string
	}{
		{
			name: "correct",
			gw:   "10.16.0.1",
			cidr: "ffff:ffff:ffff:ffff:ffff:0:ffff:0/96",
			want: "10.16.0.1,ffff:ffff:ffff:ffff:ffff::1",
			err:  "",
		},
		{
			name: "versa",
			gw:   "ffff:ffff:ffff:ffff:ffff::1",
			cidr: "10.16.0.112/24",
			want: "10.16.0.1,ffff:ffff:ffff:ffff:ffff::1",
			err:  "",
		},
		{
			name: "dual",
			gw:   "10.16.0.1",
			cidr: "10.16.0.112/24,ffff:ffff:ffff:ffff:ffff:0:ffff:0/96",
			want: "10.16.0.1,ffff:ffff:ffff:ffff:ffff::1",
			err:  "",
		},
		{
			name: "err",
			gw:   "ffff:ffff:ffff:ffff:ffff::1",
			cidr: "10.16.112/24",
			want: "",
			err:  "10.16.112/24 is not a valid cidr",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ans, err := AppendGwByCidr(c.gw, c.cidr)
			if !ErrorContains(err, c.err) || c.want != ans {
				t.Errorf("%v expected %v, %v, but %v, %v got",
					c.cidr, c.want, c.err, ans, err)
			}
		})
	}
}

func TestSplitIpsByProtocol(t *testing.T) {
	tests := []struct {
		name  string
		excl  []string
		want4 []string
		want6 []string
	}{
		{
			name:  "v4",
			excl:  []string{"10.66.0.1..10.66.0.10", "10.66.0.101..10.66.0.151"},
			want4: []string{"10.66.0.1", "10.66.0.10", "10.66.0.101", "10.66.0.151"},
			want6: []string{},
		},
		{
			name:  "v6",
			excl:  []string{"10.66.0.1..10.66.0.10", "10.66.0.101..10.66.0.151", "ffff:ffff:ffff:ffff:ffff::2"},
			want4: []string{"10.66.0.1", "10.66.0.10", "10.66.0.101", "10.66.0.151"},
			want6: []string{"ffff:ffff:ffff:ffff:ffff::2"},
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ans4, ans6 := SplitIpsByProtocol(c.excl)
			if reflect.DeepEqual(ans4, c.want4) && reflect.DeepEqual(ans6, c.want6) {
				t.Errorf("%v expected %v, %v,  but %v, %v got",
					c.excl, c.want4, c.want6, ans4, ans6)
			}
		})
	}
}

func TestGetStringIP(t *testing.T) {
	tests := []struct {
		name string
		v4   string
		v6   string
		want string
	}{
		{
			name: "base",
			v4:   "10.16.0.1",
			v6:   "ffff:ffff:ffff:ffff:ffff::1",
			want: "10.16.0.1,ffff:ffff:ffff:ffff:ffff::1",
		},
		{
			name: "err",
			v4:   "10.16.1",
			v6:   "ffff:ffff:ffff:ffff:ffff::1",
			want: "ffff:ffff:ffff:ffff:ffff::1",
		},
		{
			name: "v4Okv6Er",
			v4:   "10.16.0.1",
			v6:   ":ffff:ffff:ffff:ffff::1",
			want: "10.16.0.1",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ans := GetStringIP(c.v4, c.v6)
			if c.want != ans {
				t.Errorf("%v, %v expected %v, but %v got",
					c.v4, c.v6, c.want, ans)
			}
		})
	}
}

func TestGetIpAddrWithMask(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		cidr string
		want string
	}{
		{
			name: "base",
			ip:   "10.16.0.23",
			cidr: "10.16.0.0/24",
			want: "10.16.0.23/24",
		},
		{
			name: "v6",
			ip:   "ffff:ffff:ffff:ffff:ffff::23",
			cidr: "ffff:ffff:ffff:ffff:ffff:0:ffff:0/96",
			want: "ffff:ffff:ffff:ffff:ffff::23/96",
		},
		{
			name: "dual",
			ip:   "10.16.0.23,ffff:ffff:ffff:ffff:ffff::23",
			cidr: "10.16.0.0/24,ffff:ffff:ffff:ffff:ffff:0:ffff:0/96",
			want: "10.16.0.23/24,ffff:ffff:ffff:ffff:ffff::23/96",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ans := GetIpAddrWithMask(c.ip, c.cidr)
			if c.want != ans {
				t.Errorf("%v, %v expected %v, but %v got",
					c.ip, c.cidr, c.want, ans)
			}
		})
	}
}

func TestGetIpWithoutMask(t *testing.T) {
	tests := []struct {
		name string
		cidr string
		want string
	}{
		{
			name: "v4",
			cidr: "10.16.0.23/24",
			want: "10.16.0.23",
		},
		{
			name: "dual",
			cidr: "10.16.0.23/24,ffff:ffff:ffff:ffff:ffff:0:ffff:23/96",
			want: "10.16.0.23,ffff:ffff:ffff:ffff:ffff:0:ffff:23",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ans := GetIpWithoutMask(c.cidr)
			if c.want != ans {
				t.Errorf("%v expected %v, but %v got",
					c.cidr, c.want, ans)
			}
		})
	}
}

func TestSplitStringIP(t *testing.T) {
	tests := []struct {
		name string
		cidr string
		wv4  string
		wv6  string
	}{
		{
			name: "v4",
			cidr: "10.16.0.23/24",
			wv4:  "10.16.0.23/24",
			wv6:  "",
		},
		{
			name: "dual",
			cidr: "10.16.0.23/24,ffff:ffff:ffff:ffff:ffff:0:ffff:23/96",
			wv4:  "10.16.0.23/24",
			wv6:  "ffff:ffff:ffff:ffff:ffff:0:ffff:23/96",
		},
		{
			name: "V6",
			cidr: "ffff:ffff:ffff:ffff:ffff:0:ffff:23/96",
			wv4:  "",
			wv6:  "ffff:ffff:ffff:ffff:ffff:0:ffff:23/96",
		},
		{
			name: "err",
			cidr: "10.16.23/24,ffff:ffff:ffff:ffff:ffff:0:ffff:23/96",
			wv4:  "",
			wv6:  "",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ansv4, ansv6 := SplitStringIP(c.cidr)
			if c.wv4 != ansv4 || c.wv6 != ansv6 {
				t.Errorf("%v expected %v, %v but %v, %v got",
					c.cidr, c.wv4, c.wv6, ansv4, ansv6)
			}
		})
	}
}

func TestExpandExcludeIPs(t *testing.T) {
	tests := []struct {
		name string
		cidr string
		excl []string
		want []string
	}{
		{
			name: "base",
			cidr: "10.16.0.23/24",
			excl: []string{"10.16.0.0..10.16.0.20", "10.16.0.23", "10.16.0.255"},
			want: []string{"10.16.0.1..10.16.0.20", "10.16.0.23"},
		},
		{
			name: "exIPOutCIDR",
			cidr: "10.16.0.23/24",
			excl: []string{"10.16.0.0..10.16.0.20,10.16.0.23"},
			want: []string{},
		},
		{
			name: "exIPFormatErr",
			cidr: "10.16.0.23/24,ffff:ffff:ffff:ffff:ffff:0:ffff:23/96",
			excl: []string{"10.16.0.20..10.16.0.1", "10.16.0.23", "ffff:ffff:ffff:ffff:ffff:0:ffff:23..ffff:ffff:ffff:ffff:ffff:0:ffff:25"},
			want: []string{"10.16.0.23", "ffff:ffff:ffff:ffff:ffff:0:ffff:23..ffff:ffff:ffff:ffff:ffff:0:ffff:25"},
		},
		{
			name: "CIDRErr",
			cidr: "10.16.0.254/32,10.16.0.1/33,10.16.0.23/24,ffff:ffff:ffff:ffff:ffff:0:ffff:23/96",
			excl: []string{"10.16.0.1..10.16.0.10", "10.16.0.23", "ffff:ffff:ffff:ffff:ffff:0:ffff:23..ffff:ffff:ffff:ffff:ffff:0:ffff:25"},
			want: []string{"10.16.0.1..10.16.0.10", "10.16.0.23", "ffff:ffff:ffff:ffff:ffff:0:ffff:23..ffff:ffff:ffff:ffff:ffff:0:ffff:25"},
		},
		{
			name: "exIPOverflow",
			cidr: "10.16.0.23/24,ffff:ffff:ffff:ffff:ffff:0:ffff:23/96",
			excl: []string{"10.16.0.1..10.16.1.10", "10.16.0.23", "ffff:ffff:ffff:ffff:ffff:0:ffff:23..ffff:ffff:ffff:ffff:ffff:0:ffff:25"},
			want: []string{"10.16.0.1..10.16.0.254", "10.16.0.23", "ffff:ffff:ffff:ffff:ffff:0:ffff:23..ffff:ffff:ffff:ffff:ffff:0:ffff:25"},
		},
		{
			name: "exIPformatErr",
			cidr: "10.16.0.23/24,ffff:ffff:ffff:ffff:ffff:0:ffff:23/96",
			excl: []string{"10.16.0.10..10.16.0.10", "10.16.0.23", "ffff:ffff:ffff:ffff:ffff:0:ffff:23..ffff:ffff:ffff:ffff:ffff:0:ffff:25"},
			want: []string{"10.16.0.10", "10.16.0.23", "ffff:ffff:ffff:ffff:ffff:0:ffff:23..ffff:ffff:ffff:ffff:ffff:0:ffff:25"},
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ans := ExpandExcludeIPs(c.excl, c.cidr)
			if !reflect.DeepEqual(ans, c.want) {
				t.Errorf("%v expected %v but %v got",
					c.cidr, c.want, ans)
			}
		})
	}
}

func TestContainsIPs(t *testing.T) {
	tests := []struct {
		name string
		excl string
		ip   string
		want bool
	}{
		{
			name: "base",
			excl: "10.16.0.0..10.16.0.20",
			ip:   "10.16.0.10",
			want: true,
		},
		{
			name: "diffFormat",
			excl: "10.16.0.10",
			ip:   "10.16.0.10",
			want: true,
		},
		{
			name: "notOverlap",
			excl: "10.16.0.0..10.16.0.20",
			ip:   "10.16.0.30",
			want: false,
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ans := ContainsIPs(c.excl, c.ip)
			if ans != c.want {
				t.Errorf("%v %v expected %v but %v got",
					c.excl, c.ip, c.want, ans)
			}
		})
	}
}

func TestCountIpNums(t *testing.T) {
	tests := []struct {
		name string
		excl []string
		want float64
	}{
		{
			name: "base",
			excl: []string{"10.16.0.0..10.16.0.20", "10.16.0.30"},
			want: 22,
		},
		{
			name: "only1",
			excl: []string{"10.16.0.255"},
			want: 1,
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ans := CountIpNums(c.excl)
			if ans != c.want {
				t.Errorf("%v expected %v but %v got",
					c.excl, c.want, ans)
			}
		})
	}
}

func TestGatewayContains(t *testing.T) {
	tests := []struct {
		name           string
		gatewayNodeStr string
		gateway        string
		want           bool
	}{
		{
			name:           "base",
			gatewayNodeStr: "kube-ovn-worker:172.18.0.2, kube-ovn-control-plane:172.18.0.3",
			gateway:        "kube-ovn-worker",
			want:           true,
		},
		{
			name:           "err",
			gatewayNodeStr: "kube-ovn-worker:172.18.0.2, kube-ovn-control-plane:172.18.0.3",
			gateway:        "kube-ovn-worker1",
			want:           false,
		},
		{
			name:           "formatDiff",
			gatewayNodeStr: "kube-ovn-worker, kube-ovn-control-plane:172.18.0.3",
			gateway:        "kube-ovn-worker",
			want:           true,
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ans := GatewayContains(c.gatewayNodeStr, c.gateway)
			if ans != c.want {
				t.Errorf("%v, %v expected %v but %v got",
					c.gatewayNodeStr, c.gateway, c.want, ans)
			}
		})
	}
}

func TestJoinHostPort(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int32
		want string
	}{
		{
			name: "v4",
			host: "10.16.0.23",
			port: 80,
			want: "10.16.0.23:80",
		},
		{
			name: "v6",
			host: "ffff:ffff:ffff:ffff:ffff:0:ffff:23",
			port: 80,
			want: "[ffff:ffff:ffff:ffff:ffff:0:ffff:23]:80",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ans := JoinHostPort(c.host, c.port)
			if ans != c.want {
				t.Errorf("%v, %v expected %v but %v got",
					c.host, c.port, c.want, ans)
			}
		})
	}
}

func Test_CIDRContainIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc  string
		cidrs string
		ips   string
		want  bool
	}{
		{
			"ipv4 family",
			"192.168.230.0/24",
			"192.168.230.10,192.168.230.11",
			true,
		},
		{
			"ipv4 family which CIDR does't contain ip",
			"192.168.230.0/24",
			"192.168.231.10,192.168.230.11",
			false,
		},
		{
			"ipv6 family",
			"fc00::0af4:00/112",
			"fc00::0af4:10,fc00::0af4:11",
			true,
		},
		{
			"ipv6 family which CIDR does't contain ip",
			"fd00::c0a8:d200/120",
			"fc00::c0a8:d210",
			false,
		},
		{
			"dual",
			"192.168.230.0/24,fc00::0af4:00/112",
			"fc00::0af4:10,fc00::0af4:11,192.168.230.10,192.168.230.11",
			true,
		},
		{
			"dual which CIDR does't contain ip",
			"192.168.230.0/24,fc00::0af4:00/112",
			"fc00::0af4:10,fd00::0af4:11,192.168.230.10,192.168.230.11",
			false,
		},
		{
			"different family",
			"fd00::c0a8:d200/120",
			"10.96.0.1",
			false,
		},
		{
			"different family",
			"10.96.0.0/16",
			"fd00::c0a8:d201",
			false,
		},
		{
			"ipv4 family which CIDR has no mask",
			"192.168.0.23",
			"192.168.0.23,192.168.0.254",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := CIDRContainIP(tt.cidrs, tt.ips)
			require.Equal(t, got, tt.want)
		})
	}
}
