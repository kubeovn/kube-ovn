package util

import (
	"math/big"
	"net"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
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
			if ans := IP2BigInt(c.ip); !reflect.DeepEqual(ans, c.expect) {
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
			if ans := BigInt2Ip(c.ip); ans != c.expect {
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
			if ans := SubnetNumber(c.subnet); ans != c.expect {
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
			// TODO: this is a bug, the broadcast address should be 192.128.23.1
			name:   "v4/31",
			subnet: "192.128.23.0/31",
			expect: "",
		},
		{
			name:   "v4/32",
			subnet: "192.128.23.0/32",
			expect: "192.128.23.0",
		},
		{
			name:   "v6",
			subnet: "ffff:ffff:ffff:ffff:ffff:0:ffff:ffff/96",
			expect: "ffff:ffff:ffff:ffff:ffff:0:ffff:ffff",
		},
		{
			name:   "v6/127",
			subnet: "ffff:ffff:ffff:ffff:ffff:0:ffff:ffff/127",
			expect: "",
		},
		{
			name:   "v6/128",
			subnet: "ffff:ffff:ffff:ffff:ffff:0:ffff:ffff/128",
			expect: "ffff:ffff:ffff:ffff:ffff:0:ffff:ffff",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			if ans := SubnetBroadcast(c.subnet); ans != c.expect {
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
		},
		{
			name:   "base31netmask",
			subnet: "192.168.0.23/31",
			expect: "192.168.0.22",
			err:    "",
		},
		{
			name:   "base31netmask",
			subnet: "192.168.0.0/31",
			expect: "192.168.0.0",
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
		{
			name:   "v6127netmask",
			subnet: "ffff:ffff:ffff:ffff:ffff:0:ffff:0/127",
			expect: "ffff:ffff:ffff:ffff:ffff:0:ffff:0",
			err:    "",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ans, err := FirstIP(c.subnet)
			if ans != c.expect || !ErrorContains(err, c.err) {
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
			name:   "base31netmask",
			subnet: "192.168.0.2/31",
			expect: "192.168.0.3",
			err:    "",
		},
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
		{
			name:   "v6127netmask",
			subnet: "ffff:ffff:ffff:ffff:ffff:0:ffff:0/127",
			expect: "ffff:ffff:ffff:ffff:ffff:0:ffff:1",
			err:    "",
		},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			ans, err := LastIP(c.subnet)
			if ans != c.expect || !ErrorContains(err, c.err) {
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
			cidrStr: "192.168.0.23/31",
			ipStr:   "192.168.0.23,192.168.0.22",
			want:    true,
		},
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
		{
			name:    "v6",
			cidrStr: "ffff:ffff:ffff:ffff:ffff:0:ffff:4/127",
			ipStr:   "ffff:ffff:ffff:ffff:ffff:0:ffff:4,ffff:ffff:ffff:ffff:ffff:0:ffff:5",
			want:    true,
		},
		{
			name:    "empty cidr",
			cidrStr: "",
			ipStr:   "192.168.0.1",
			want:    false,
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
			want: 2,
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
				ans := GenerateRandomIP(c.cidr)
				if c.want != ans {
					t.Errorf("%v expected %v, but %v got",
						c.cidr, c.want, ans)
				}
			} else {
				ans := GenerateRandomIP(c.cidr)
				if IPNets.Contains(net.ParseIP(GenerateRandomIP(c.cidr))) {
					t.Errorf("%v expected %v, but %v got",
						c.cidr, c.want, ans)
				}
			}
		})
	}
}

func TestGenerateRandomV6IP(t *testing.T) {
	tests := []struct {
		name     string
		cidr     string
		wantErr  bool
		wantIPv6 bool
	}{
		{
			name:     "valid IPv6 CIDR",
			cidr:     "2001:db8::/64",
			wantErr:  false,
			wantIPv6: true,
		},
		{
			name:     "invalid CIDR format",
			cidr:     "2001:db8::1",
			wantErr:  true,
			wantIPv6: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := GenerateRandomIP(tt.cidr)
			if tt.wantErr {
				if ip != "" {
					t.Errorf("GenerateRandomV6IP(%s) = %s; want empty string", tt.cidr, ip)
				}
				return
			}

			parsedIP, _, err := net.ParseCIDR(ip)
			if err != nil {
				t.Errorf("GenerateRandomV6IP(%s) returned invalid IP: %v", tt.cidr, err)
				return
			}

			isIPv6 := parsedIP.To4() == nil
			if isIPv6 != tt.wantIPv6 {
				t.Errorf("GenerateRandomV6IP(%s) returned %v; want IPv6: %v", tt.cidr, parsedIP, tt.wantIPv6)
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

func TestCheckNodeDNSIP(t *testing.T) {
	tests := []struct {
		name    string
		dnsIP   string
		wantErr bool
	}{
		{
			name:    "valid IPv4 address",
			dnsIP:   "192.168.1.1",
			wantErr: false,
		},
		{
			name:    "valid IPv6 address",
			dnsIP:   "2001:db8::1",
			wantErr: false,
		},
		{
			name:    "invalid IP address",
			dnsIP:   "invalid",
			wantErr: true,
		},
		{
			name:    "empty string",
			dnsIP:   "",
			wantErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := CheckNodeDNSIP(test.dnsIP)
			if test.wantErr != (err != nil) {
				t.Errorf("CheckNodeDNSIP(%q) expected error = %v, but got error = %v", test.dnsIP, test.wantErr, err)
			}
		})
	}
}

func TestGetExternalNetwork(t *testing.T) {
	tests := []struct {
		name        string
		externalNet string
		expected    string
	}{
		{
			name:        "External network specified",
			externalNet: "custom-external-network",
			expected:    "custom-external-network",
		},
		{
			name:        "External network not specified",
			externalNet: "",
			expected:    "ovn-vpc-external-network",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetExternalNetwork(tt.externalNet)
			if result != tt.expected {
				t.Errorf("got %v, but want %v", result, tt.expected)
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
			name: "v4",
			cidr: "10.16.0.112/31",
			want: "10.16.0.112",
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
			if slices.Equal(ans4, c.want4) && slices.Equal(ans6, c.want6) {
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

func TestGetIPAddrWithMask(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		cidr string
		want string
	}{
		{
			name: "Single IPv4 address",
			ip:   "192.168.1.1",
			cidr: "192.168.1.0/24",
			want: "192.168.1.1/24",
		},
		{
			name: "Single IPv6 address",
			ip:   "2001:db8::1",
			cidr: "2001:db8::/32",
			want: "2001:db8::1/32",
		},
		{
			name: "Dual stack addresses",
			ip:   "192.168.1.1,2001:db8::1",
			cidr: "192.168.1.0/24,2001:db8::/32",
			want: "192.168.1.1/24,2001:db8::1/32",
		},
		{
			name: "Invalid dual stack ip format",
			ip:   "192.168.1.1",
			cidr: "192.168.1.0/24,2001:db8::/32",
			want: "",
		},
		{
			name: "Invalid dual stack cidr format",
			ip:   "192.168.1.1,2001:db8::1",
			cidr: "192.168.1.0/24",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetIPAddrWithMask(tt.ip, tt.cidr)
			if (err != nil && tt.want != "") || (err == nil && got != tt.want) {
				t.Errorf("got %v, but want %v", got, tt.want)
			}
		})
	}
}

func TestGetIPAddrWithMaskForCNI(t *testing.T) {
	tests := []struct {
		name      string
		ip        string
		cidr      string
		wantAddr  string
		wantNoIP  bool
		wantError bool
	}{
		{
			name:      "Normal IPv4 address",
			ip:        "192.168.1.1",
			cidr:      "192.168.1.0/24",
			wantAddr:  "192.168.1.1/24",
			wantNoIP:  false,
			wantError: false,
		},
		{
			name:      "Normal IPv6 address",
			ip:        "2001:db8::1",
			cidr:      "2001:db8::/32",
			wantAddr:  "2001:db8::1/32",
			wantNoIP:  false,
			wantError: false,
		},
		{
			name:      "Dual stack addresses",
			ip:        "192.168.1.1,2001:db8::1",
			cidr:      "192.168.1.0/24,2001:db8::/32",
			wantAddr:  "192.168.1.1/24,2001:db8::1/32",
			wantNoIP:  false,
			wantError: false,
		},
		{
			name:      "No-IPAM mode (empty IP with CIDR)",
			ip:        "",
			cidr:      "192.168.1.0/24",
			wantAddr:  "",
			wantNoIP:  true,
			wantError: false,
		},
		{
			name:      "No-IPAM mode (empty IP with dual-stack CIDR)",
			ip:        "",
			cidr:      "192.168.1.0/24,2001:db8::/32",
			wantAddr:  "",
			wantNoIP:  true,
			wantError: false,
		},
		{
			name:      "Error: invalid dual stack ip format",
			ip:        "192.168.1.1",
			cidr:      "192.168.1.0/24,2001:db8::/32",
			wantAddr:  "",
			wantNoIP:  false,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAddr, gotNoIP, gotErr := GetIPAddrWithMaskForCNI(tt.ip, tt.cidr)

			if tt.wantError {
				if gotErr == nil {
					t.Errorf("GetIPAddrWithMaskForCNI() expected error but got nil")
				}
			} else {
				if gotErr != nil {
					t.Errorf("GetIPAddrWithMaskForCNI() unexpected error: %v", gotErr)
				}
			}

			if gotAddr != tt.wantAddr {
				t.Errorf("GetIPAddrWithMaskForCNI() gotAddr = %v, want %v", gotAddr, tt.wantAddr)
			}

			if gotNoIP != tt.wantNoIP {
				t.Errorf("GetIPAddrWithMaskForCNI() gotNoIP = %v, want %v", gotNoIP, tt.wantNoIP)
			}
		})
	}
}

func TestGetIPWithoutMask(t *testing.T) {
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
			ans := GetIPWithoutMask(c.cidr)
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
			if !slices.Equal(ans, c.want) {
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
			ans := CountIPNums(c.excl)
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

func TestTCPConnectivityListen(t *testing.T) {
	// Start a ipv4 TCP server
	validEndpoint := "127.0.0.1:65531"
	err := TCPConnectivityListen(validEndpoint)
	require.NoError(t, err)
	invalidEndpoint := "127.0.0.1:65536"
	err = TCPConnectivityListen(invalidEndpoint)
	require.Error(t, err)
	// Start a ipv6 TCP server
	validEndpoint = "[::1]:65531"
	err = TCPConnectivityListen(validEndpoint)
	require.NoError(t, err)
	invalidEndpoint = "[::1]:65536"
	err = TCPConnectivityListen(invalidEndpoint)
	require.Error(t, err)
}

func TestTCPConnectivityCheck(t *testing.T) {
	// Start a ipv4 TCP server
	validEndpoint := "127.0.0.1:65532"
	err := TCPConnectivityListen(validEndpoint)
	require.NoError(t, err)
	err = TCPConnectivityCheck(validEndpoint)
	require.NoError(t, err)
	invalidEndpoint := "127.0.0.1:65536"
	err = TCPConnectivityCheck(invalidEndpoint)
	require.Error(t, err)
	// Start a ipv6 TCP server
	validEndpoint = "[::1]:65532"
	err = TCPConnectivityListen(validEndpoint)
	require.NoError(t, err)
	err = TCPConnectivityCheck(validEndpoint)
	require.NoError(t, err)
	invalidEndpoint = "[::1]:65536"
	err = TCPConnectivityCheck(invalidEndpoint)
	require.Error(t, err)
}

func TestUDPConnectivityListen(t *testing.T) {
	// Start a ipv4 UDP server
	validEndpoint := "127.0.0.1:65533"
	err := UDPConnectivityListen(validEndpoint)
	require.NoError(t, err)
	invalidEndpoint := "127.0.0.1:65536"
	err = UDPConnectivityListen(invalidEndpoint)
	require.Error(t, err)
	invalidEndpoint = "127.0.0.256:65536"
	err = UDPConnectivityListen(invalidEndpoint)
	require.Error(t, err)
	// Start a ipv6 UDP server
	validEndpoint = "[::1]:65533"
	err = UDPConnectivityListen(validEndpoint)
	require.NoError(t, err)
	invalidEndpoint = "[::1]:65536"
	err = UDPConnectivityListen(invalidEndpoint)
	require.Error(t, err)
	invalidEndpoint = "[::g]:65536"
	err = UDPConnectivityListen(invalidEndpoint)
	require.Error(t, err)
}

func TestUDPConnectivityCheck(t *testing.T) {
	// Start a ipv4 UDP server
	validEndpoint := "127.0.0.1:65534"
	err := UDPConnectivityListen(validEndpoint)
	require.NoError(t, err)
	err = UDPConnectivityCheck(validEndpoint)
	require.NoError(t, err)
	invalidEndpoint := "127.0.0.1:65536"
	err = UDPConnectivityCheck(invalidEndpoint)
	require.Error(t, err)
	invalidEndpoint = "127.0.0.256:65536"
	err = UDPConnectivityCheck(invalidEndpoint)
	require.Error(t, err)
	// Start a ipv6 UDP server
	validEndpoint = "[::1]:65534"
	err = UDPConnectivityListen(validEndpoint)
	require.NoError(t, err)
	err = UDPConnectivityCheck(validEndpoint)
	require.NoError(t, err)
	invalidEndpoint = "[::1]:65536"
	err = UDPConnectivityCheck(invalidEndpoint)
	require.Error(t, err)
	invalidEndpoint = "[::g]:65536"
	err = UDPConnectivityCheck(invalidEndpoint)
	require.Error(t, err)
}

func TestGetDefaultListenAddr(t *testing.T) {
	require.Equal(t, GetDefaultListenAddr(), []string{"0.0.0.0"})
	err := os.Setenv("ENABLE_BIND_LOCAL_IP", "true")
	require.NoError(t, err)
	err = os.Setenv("POD_IPS", "10.10.10.10")
	require.NoError(t, err)
	require.Equal(t, GetDefaultListenAddr(), []string{"10.10.10.10"})
	err = os.Setenv("POD_IPS", "fd00::1")
	require.NoError(t, err)
	require.Equal(t, GetDefaultListenAddr(), []string{"fd00::1"})
	err = os.Setenv("POD_IPS", "10.10.10.10,fd00::1")
	require.NoError(t, err)
	require.Equal(t, GetDefaultListenAddr(), []string{"10.10.10.10", "fd00::1"})
	err = os.Setenv("ENABLE_BIND_LOCAL_IP", "false")
	require.NoError(t, err)
	require.Equal(t, GetDefaultListenAddr(), []string{"0.0.0.0"})
}

func TestContainsUppercase(t *testing.T) {
	validIPv6 := "2001:db8::1"
	contained := ContainsUppercase(validIPv6)
	require.False(t, contained)
	invalidIPv6 := "2001:DB8::1"
	contained = ContainsUppercase(invalidIPv6)
	require.True(t, contained)
}

func TestInvalidCIDR(t *testing.T) {
	validCIDR := "10.10.10.0/24"
	err := InvalidSpecialCIDR(validCIDR)
	require.NoError(t, err)
	validCIDR = "2001:db8::1/64"
	err = InvalidSpecialCIDR(validCIDR)
	require.NoError(t, err)
	invalidCIDR := "0.0.0.0/0"
	err = InvalidSpecialCIDR(invalidCIDR)
	require.Error(t, err)
	invalidCIDR = "255.255.255.255"
	err = InvalidSpecialCIDR(invalidCIDR)
	require.Error(t, err)
}

func TestInvalidNetworkMask(t *testing.T) {
	validNet := net.IPNet{
		IP:   net.ParseIP("10.10.10.0"),
		Mask: net.CIDRMask(0, 32),
	}
	err := InvalidNetworkMask(&validNet)
	require.Nil(t, err)

	validNet = net.IPNet{
		IP:   net.ParseIP("10.10.10.0"),
		Mask: net.CIDRMask(0, 33),
	}
	err = InvalidNetworkMask(&validNet)
	require.Error(t, err)

	validNet = net.IPNet{
		IP:   net.ParseIP("0.0.0.0"),
		Mask: net.CIDRMask(0, 0),
	}
	err = InvalidNetworkMask(&validNet)
	require.Error(t, err)

	// Test for IPv6
	validNet = net.IPNet{
		IP:   net.ParseIP("2001:db8::1"),
		Mask: net.CIDRMask(0, 128),
	}
	err = InvalidNetworkMask(&validNet)
	require.Nil(t, err)
	validNet = net.IPNet{
		IP:   net.ParseIP("2001:db8::1"),
		Mask: net.CIDRMask(0, 129),
	}
	err = InvalidNetworkMask(&validNet)
	require.Error(t, err)
	validNet = net.IPNet{
		IP:   net.ParseIP("2001:db8::1"),
		Mask: net.CIDRMask(0, 0),
	}
	err = InvalidNetworkMask(&validNet)
	require.Error(t, err)
	validNet = net.IPNet{
		IP:   net.ParseIP("0:0::0"),
		Mask: net.CIDRMask(0, 0),
	}
	err = InvalidNetworkMask(&validNet)
	require.Error(t, err)
}

func TestCIDRContainsCIDR(t *testing.T) {
	tests := []struct {
		name    string
		cidr1   string
		cidr2   string
		wantErr bool
		wantRet bool
	}{
		{
			name:    "different family",
			cidr1:   "10.0.0.0/16",
			cidr2:   "fd00::/64",
			wantErr: true,
			wantRet: false,
		},
		{
			name:    "invalid cidr1",
			cidr1:   "1.1.1.1/33",
			cidr2:   "10.0.0.0/25",
			wantErr: true,
			wantRet: false,
		},
		{
			name:    "invalid cidr2",
			cidr1:   "10.0.0.0/24",
			cidr2:   "1.1.1.1/33",
			wantErr: true,
			wantRet: false,
		},
		{
			name:    "cidr1 contains cidr2",
			cidr1:   "10.0.0.0/24",
			cidr2:   "10.0.0.0/25",
			wantErr: false,
			wantRet: true,
		},
		{
			name:    "cidr2 contains cidr1",
			cidr1:   "10.0.0.0/24",
			cidr2:   "10.0.0.0/23",
			wantErr: false,
			wantRet: false,
		},
		{
			name:    "cidr1 does not contain cidr2",
			cidr1:   "10.0.0.0/24",
			cidr2:   "10.0.1.0/24",
			wantErr: false,
			wantRet: false,
		},
		{
			name:    "cidr1 equals cidr2",
			cidr1:   "10.0.0.0/24",
			cidr2:   "10.0.0.0/24",
			wantErr: false,
			wantRet: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret, err := CIDRContainsCIDR(tt.cidr1, tt.cidr2)
			if (err != nil) != tt.wantErr {
				t.Errorf("CIDRContainsCIDR() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if ret != tt.wantRet {
				t.Errorf("CIDRContainsCIDR() got = %v, want %v", ret, tt.wantRet)
			}
		})
	}
}
