package vpc

import (
	"net/netip"
	"testing"

	"github.com/moby/moby/api/types/network"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestOrderedDockerSubnet(t *testing.T) {
	ipv4 := network.IPAMConfig{
		Subnet:  netip.MustParsePrefix("172.20.0.0/16"),
		Gateway: netip.MustParseAddr("172.20.0.1"),
	}
	ipv6 := network.IPAMConfig{
		Subnet:  netip.MustParsePrefix("fc00::/64"),
		Gateway: netip.MustParseAddr("fc00::1"),
	}

	tests := []struct {
		name    string
		configs []network.IPAMConfig
		hasIPv4 bool
		hasIPv6 bool
		cidr    string
		gw      string
	}{
		{
			name:    "ipv4 before ipv6",
			configs: []network.IPAMConfig{ipv4, ipv6},
			hasIPv4: true,
			hasIPv6: true,
			cidr:    "172.20.0.0/16,fc00::/64",
			gw:      "172.20.0.1,fc00::1",
		},
		{
			// The Docker IPAM config order is not guaranteed, and kube-ovn requires the IPv4 CIDR
			// first, so an IPv6-first network must not produce an IPv6-first subnet.
			name:    "ipv6 before ipv4",
			configs: []network.IPAMConfig{ipv6, ipv4},
			hasIPv4: true,
			hasIPv6: true,
			cidr:    "172.20.0.0/16,fc00::/64",
			gw:      "172.20.0.1,fc00::1",
		},
		{
			name:    "ipv6 disabled",
			configs: []network.IPAMConfig{ipv6, ipv4},
			hasIPv4: true,
			cidr:    "172.20.0.0/16",
			gw:      "172.20.0.1",
		},
		{
			name:    "ipv4 disabled",
			configs: []network.IPAMConfig{ipv6, ipv4},
			hasIPv6: true,
			cidr:    "fc00::/64",
			gw:      "fc00::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cidr, gw := orderedDockerSubnet(tt.configs, tt.hasIPv4, tt.hasIPv6)
			if cidr != tt.cidr || gw != tt.gw {
				t.Fatalf("orderedDockerSubnet() = %q, %q, want %q, %q", cidr, gw, tt.cidr, tt.gw)
			}
		})
	}
}

func TestGWLabelStateRestoreValue(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{
			name:   "nil labels",
			labels: nil,
			want:   "",
		},
		{
			name:   "label absent",
			labels: map[string]string{"role": "worker"},
			want:   "",
		},
		{
			name:   "gateway label enabled",
			labels: map[string]string{util.ExGatewayLabel: "true"},
			want:   "true",
		},
		{
			// kube-ovn sets the label to "false" when a node stops being an external gateway node,
			// and the suite must not turn such a node back into a gateway on cleanup.
			name:   "gateway label disabled",
			labels: map[string]string{util.ExGatewayLabel: "false"},
			want:   "false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gwLabelStateOf(tt.labels).restoreValue(); got != tt.want {
				t.Fatalf("restoreValue() = %q, want %q", got, tt.want)
			}
		})
	}
}
