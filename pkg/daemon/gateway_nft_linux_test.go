package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/knftables"
)

func TestRenderNFTBaseSchema(t *testing.T) {
	fake := knftables.NewFake("", "")
	backend := newNFTGatewayBackendForTest(fake)
	tx := backend.renderFull(gatewayNFTSnapshot{Families: []nftFamilySnapshot{{
		Family: knftables.IPv4Family,
		Table:  "kube-ovn",
	}}})

	require.NoError(t, fake.Check(context.Background(), tx))
	dump := tx.String()
	require.Contains(t, dump, "add table ip kube-ovn")
	require.Contains(t, dump, "add chain ip kube-ovn schema-v1")
	require.Contains(t, dump, "add chain ip kube-ovn nat-host-service { type nat hook postrouting priority 90 ;")
	require.Contains(t, dump, "add chain ip kube-ovn nat-postrouting { type nat hook postrouting priority 110 ;")
	require.Contains(t, dump, "add chain ip kube-ovn tproxy-output { type route hook output priority -150 ;")
}

func TestRenderNFTDualStackSingleTransaction(t *testing.T) {
	fake := knftables.NewFake("", "")
	backend := newNFTGatewayBackendForTest(fake)
	tx := backend.renderFull(gatewayNFTSnapshot{Families: []nftFamilySnapshot{
		{Family: knftables.IPv4Family, Table: "kube-ovn"},
		{Family: knftables.IPv6Family, Table: "kube-ovn"},
	}})

	require.NoError(t, fake.Check(context.Background(), tx))
	dump := tx.String()
	require.Contains(t, dump, "add table ip kube-ovn")
	require.Contains(t, dump, "add table ip6 kube-ovn")
}

func newNFTGatewayBackendForTest(writer knftables.Interface) *nftGatewayBackend {
	return &nftGatewayBackend{writer: writer, readers: map[knftables.Family]knftables.Interface{}}
}

func TestRenderNFTServiceNAT(t *testing.T) {
	dump := renderTestSnapshot(t, nftFamilySnapshot{
		Family:         knftables.IPv4Family,
		Table:          nftGatewayTable,
		NodeInternalIP: "192.168.1.10",
		ClusterIPPorts: []nftAddressPort{{Address: "10.96.0.10", Protocol: "tcp", Port: 80}},
		Subnets:        []string{"10.16.0.0/16"},
	})

	require.Contains(t, dump, "ct status dnat fib saddr type local")
	require.Contains(t, dump, "ct original ip daddr . ct original protocol . ct original proto-dst @cluster-ip-ports")
	require.Contains(t, dump, "snat to 192.168.1.10 fully-random")
	require.Contains(t, dump, `iifname "ovn0"`)
	require.Contains(t, dump, "masquerade fully-random")
	require.NotContains(t, dump, "0x4000")
}

func TestRenderNFTNodePort(t *testing.T) {
	dump := renderTestSnapshot(t, nftFamilySnapshot{
		Family:               knftables.IPv4Family,
		Table:                nftGatewayTable,
		Subnets:              []string{"10.16.0.0/16"},
		DistributedGWSubnets: []string{"10.16.0.0/24"},
		OtherNodeIPs:         []string{"192.168.1.11"},
		NodeIPs:              []string{"192.168.1.10"},
		ServiceVIPPorts:      []nftAddressPort{{Address: "192.168.1.10", Protocol: "sctp", Port: 90}},
		LocalNodePorts:       []nftProtocolPort{{Protocol: "sctp", Port: 30090}},
	})

	guard := strings.Index(dump, "@service-vip-ports return")
	local := strings.Index(dump, "@local-nodeports jump nodeport-local-action")
	require.GreaterOrEqual(t, guard, 0)
	require.Greater(t, local, guard)
	require.Contains(t, dump, "sctp . 30090")
	require.Contains(t, dump, "ct original ip saddr @other-node-ips masquerade fully-random")
	require.Contains(t, dump, "ip daddr @distributed-gw-subnets accept")
}

func TestRenderNFTNATOutgoingOrder(t *testing.T) {
	dump := renderTestSnapshot(t, nftFamilySnapshot{
		Family:               knftables.IPv4Family,
		Table:                nftGatewayTable,
		NodeInternalIP:       "192.168.1.10",
		ClusterIPPorts:       []nftAddressPort{{Address: "10.96.0.10", Protocol: "tcp", Port: 80}},
		Subnets:              []string{"10.16.0.0/16"},
		NATSubnets:           []string{"10.16.0.0/16"},
		DistributedGWSubnets: []string{"10.16.0.0/24"},
		OtherNodeIPs:         []string{"192.168.1.11"},
		NodeIPs:              []string{"192.168.1.10"},
		LocalNodePorts:       []nftProtocolPort{{Protocol: "tcp", Port: 30080}},
		CentralizedSNATs:     []nftCentralizedSNAT{{CIDR: "10.17.0.0/24", IP: "192.168.1.20"}},
	})

	comments := []string{
		`comment "nat-service"`,
		`comment "nat-subnet-between"`,
		`comment "nat-nodeport"`,
		`comment "nat-direct-routing"`,
		`comment "nat-route-traffic"`,
		`comment "nat-policy"`,
		`comment "nat-centralized-snat:`,
		`comment "nat-default"`,
	}
	previous := -1
	for _, comment := range comments {
		position := strings.Index(dump, comment)
		require.Greater(t, position, previous, comment)
		previous = position
	}
}

func renderTestSnapshot(t *testing.T, family nftFamilySnapshot) string {
	t.Helper()
	fake := knftables.NewFake("", "")
	backend := newNFTGatewayBackendForTest(fake)
	tx := backend.renderFull(gatewayNFTSnapshot{Families: []nftFamilySnapshot{family}})
	require.NoError(t, fake.Check(context.Background(), tx))
	return tx.String()
}

func TestRenderNFTNATPolicy(t *testing.T) {
	dump := renderTestSnapshot(t, nftFamilySnapshot{
		Family: knftables.IPv4Family,
		Table:  nftGatewayTable,
		NATPolicies: []nftNATPolicy{
			{SubnetCIDR: "10.16.0.0/24", RuleID: "nat-rule", SrcIPs: []string{"10.16.0.10"}, DstIPs: []string{"0.0.0.0/0"}, Action: "nat"},
			{SubnetCIDR: "10.16.0.0/24", RuleID: "forward-rule", DstIPs: []string{"192.0.2.0/24"}, Action: "forward"},
		},
	})

	require.Contains(t, dump, "masquerade fully-random")
	require.Contains(t, dump, "accept")
	require.Contains(t, dump, "add rule ip kube-ovn nat-policy return")
	require.NotContains(t, dump, "0x90001")
	require.NotContains(t, dump, "0x90002")
}

func TestRenderNFTTProxyAndMangle(t *testing.T) {
	dump := renderTestSnapshot(t, nftFamilySnapshot{
		Family:         knftables.IPv4Family,
		Table:          nftGatewayTable,
		NodeInternalIP: "192.168.1.10",
		Subnets:        []string{"10.16.0.0/16"},
		CentralizedSNATs: []nftCentralizedSNAT{{
			CIDR: "10.16.1.0/24",
			IP:   "192.168.1.20",
		}},
		TProxyTargets: []nftTProxyTarget{{Address: "10.30.0.2", Port: 8080}},
	})

	require.Contains(t, dump, "tcp dport 8080 meta mark set 0x90003")
	require.Contains(t, dump, "tcp dport 8080 tproxy ip to 192.168.1.10:8102 meta mark set 0x90004")
	require.Contains(t, dump, "udp dport { 6081, 4789 } meta mark set 0")
	require.Contains(t, dump, "tcp flags & rst == rst ct state invalid drop")
	require.Contains(t, dump, "ip saddr 10.16.1.0/24 tcp flags & syn == 0 ct state new ip daddr != @subnets drop")
}

func TestRenderNFTCounters(t *testing.T) {
	family := nftFamilySnapshot{
		Family: knftables.IPv4Family,
		Table:  nftGatewayTable,
		SubnetCounters: []nftSubnetCounter{{
			UID:  "uid-subnet-a",
			Name: "subnet-a",
			CIDR: "10.16.0.0/24",
		}},
	}
	dump := renderTestSnapshot(t, family)

	require.Contains(t, dump, "add counter ip kube-ovn subnet-")
	require.Contains(t, dump, "counter name subnet-")
	require.Contains(t, dump, `comment "subnet-a egress"`)
	require.Contains(t, dump, `comment "subnet-a ingress"`)
	require.NotContains(t, dump, "delete counter")

	second := renderTestSnapshot(t, family)
	require.NotContains(t, second, "delete counter")
}
