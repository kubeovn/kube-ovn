package daemon

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectEncapIP(t *testing.T) {
	addrs := func(cidrs ...string) []net.Addr {
		ret := make([]net.Addr, 0, len(cidrs))
		for _, cidr := range cidrs {
			ip, ipNet, err := net.ParseCIDR(cidr)
			require.NoError(t, err)
			ret = append(ret, &net.IPNet{IP: ip, Mask: ipNet.Mask})
		}
		return ret
	}

	tests := []struct {
		name          string
		addrs         []net.Addr
		srcIPs        []string
		hostTunnelSrc bool
		nodeIPs       []string
		expected      string
	}{{
		name:     "no address",
		addrs:    addrs(),
		expected: "",
	}, {
		name:     "single /32 assigned by cloud dhcp",
		addrs:    addrs("10.198.0.140/32", "fe80::2c28:3aff:fe25:bf57/64"),
		expected: "10.198.0.140",
	}, {
		name:     "single /128",
		addrs:    addrs("fd00::1/128", "fe80::1/64"),
		expected: "fd00::1",
	}, {
		name:     "dual stack full mask addresses",
		addrs:    addrs("10.198.0.140/32", "fd00::1/128"),
		expected: "10.198.0.140",
	}, {
		name:     "vip is skipped in favor of the node address",
		addrs:    addrs("192.168.0.11/32", "192.168.0.10/24"),
		expected: "192.168.0.10",
	}, {
		name:          "vip is used when host tunnel src is enabled",
		addrs:         addrs("192.168.0.11/32", "192.168.0.10/24"),
		hostTunnelSrc: true,
		expected:      "192.168.0.11",
	}, {
		name:     "all full mask addresses of the family are skipped but one remains per family",
		addrs:    addrs("192.168.0.11/32", "192.168.0.12/32", "fd00::1/128"),
		expected: "fd00::1",
	}, {
		name:     "node ip is used even when another full mask address exists",
		addrs:    addrs("192.168.0.11/32", "192.168.0.10/32"),
		nodeIPs:  []string{"192.168.0.10"},
		expected: "192.168.0.10",
	}, {
		name:     "no candidate is left when every address of the single stack nic is a vip",
		addrs:    addrs("192.168.0.11/32", "192.168.0.12/32"),
		expected: "",
	}, {
		name:     "address must be a route source when any exists",
		addrs:    addrs("192.168.0.10/24", "192.168.1.10/24"),
		srcIPs:   []string{"192.168.1.10"},
		expected: "192.168.1.10",
	}, {
		name:     "no address matches the route sources",
		addrs:    addrs("192.168.0.10/24"),
		srcIPs:   []string{"192.168.1.10"},
		expected: "",
	}, {
		name:     "full mask address is not a vip when the other address is not a route source",
		addrs:    addrs("10.0.0.10/32", "10.0.0.11/24"),
		srcIPs:   []string{"10.0.0.10"},
		expected: "10.0.0.10",
	}, {
		name:     "ipv6 node address follows an old full mask address",
		addrs:    addrs("2001:db8::84/128", "2001:db8::3d3/128", "fe80::1/64"),
		nodeIPs:  []string{"", "2001:db8::3d3"},
		expected: "2001:db8::3d3",
	}, {
		name:     "ipv6 vip is skipped in favor of a subnet address",
		addrs:    addrs("2001:db8::84/128", "2001:db8::3d3/64"),
		expected: "2001:db8::3d3",
	}, {
		name:     "ambiguous ipv6 full mask addresses are rejected",
		addrs:    addrs("2001:db8::84/128", "2001:db8::3d3/128"),
		expected: "",
	}, {
		name:     "ipv6 node address still requires a matching route source",
		addrs:    addrs("2001:db8::84/128", "2001:db8::3d3/128"),
		srcIPs:   []string{"2001:db8::84"},
		nodeIPs:  []string{"2001:db8::3d3"},
		expected: "2001:db8::84",
	}, {
		name:     "loopback and link local addresses are ignored",
		addrs:    addrs("127.0.0.1/8", "169.254.1.1/16", "10.198.0.140/32"),
		expected: "10.198.0.140",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, selectEncapIP(tt.addrs, tt.srcIPs, tt.hostTunnelSrc, tt.nodeIPs...))
		})
	}
}
