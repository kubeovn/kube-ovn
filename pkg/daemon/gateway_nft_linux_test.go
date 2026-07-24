package daemon

import (
	"context"
	"errors"
	"net"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

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

func TestNFTReadSubnetCountersPrunesRemovedBaselines(t *testing.T) {
	applied := gatewayNFTSnapshot{}
	backend := &nftGatewayBackend{
		controller:    &Controller{},
		readers:       map[knftables.Family]knftables.Interface{},
		applied:       &applied,
		counterValues: map[string]nftCounterValue{"ip/subnet-removed-egress": {}},
	}

	require.NoError(t, backend.ReadSubnetCounters(context.Background()))
	require.Empty(t, backend.counterValues)
}

func TestNFTReconcileSetElementDiff(t *testing.T) {
	fake := knftables.NewFake("", "")
	desired := gatewayNFTSnapshot{Families: []nftFamilySnapshot{{
		Family:  knftables.IPv4Family,
		Table:   nftGatewayTable,
		Subnets: []string{"10.16.0.0/24", "10.17.0.0/24"},
	}}}
	backend := &nftGatewayBackend{
		writer:        fake,
		buildSnapshot: func() (gatewayNFTSnapshot, error) { return desired, nil },
	}

	require.NoError(t, backend.Reconcile(context.Background()))
	desired.Families[0].Subnets = []string{"10.17.0.0/24", "10.18.0.0/24"}
	require.NoError(t, backend.Reconcile(context.Background()))

	fake.RLock()
	dump := fake.LastTransaction.String()
	fake.RUnlock()
	require.Contains(t, dump, "delete element ip kube-ovn subnets { 10.16.0.0/24 }")
	require.Contains(t, dump, "add element ip kube-ovn subnets { 10.18.0.0/24 }")
	require.NotContains(t, dump, "add table")
	require.Equal(t, 2, fake.LastTransaction.NumOperations())
}

func TestNFTReconcilePolicyChainDiff(t *testing.T) {
	fake := knftables.NewFake("", "")
	desired := gatewayNFTSnapshot{Families: []nftFamilySnapshot{{
		Family: knftables.IPv4Family,
		Table:  nftGatewayTable,
		NATPolicies: []nftNATPolicy{{
			SubnetCIDR: "10.16.0.0/24",
			RuleID:     "rule-1",
			Action:     "nat",
		}},
	}}}
	backend := &nftGatewayBackend{
		writer:        fake,
		buildSnapshot: func() (gatewayNFTSnapshot, error) { return desired, nil },
	}

	require.NoError(t, backend.Reconcile(context.Background()))
	desired.Families[0].NATPolicies[0].Action = "forward"
	require.NoError(t, backend.Reconcile(context.Background()))

	fake.RLock()
	dump := fake.LastTransaction.String()
	fake.RUnlock()
	require.Contains(t, dump, "flush chain ip kube-ovn nat-policy")
	require.NotContains(t, dump, "flush chain ip kube-ovn tproxy-output")
	require.NotContains(t, dump, "flush chain ip kube-ovn nat-postrouting")
}

func TestNFTReconcileFailureKeepsAppliedSnapshot(t *testing.T) {
	fake := knftables.NewFake("", "")
	writer := &failingNFTInterface{Interface: fake}
	desired := gatewayNFTSnapshot{Families: []nftFamilySnapshot{{
		Family:  knftables.IPv4Family,
		Table:   nftGatewayTable,
		Subnets: []string{"10.16.0.0/24"},
	}}}
	backend := &nftGatewayBackend{
		writer:        writer,
		buildSnapshot: func() (gatewayNFTSnapshot, error) { return desired, nil },
	}

	require.NoError(t, backend.Reconcile(context.Background()))
	applied := *backend.applied
	desired.Families[0].Subnets = []string{"10.17.0.0/24"}
	writer.fail = true
	require.Error(t, backend.Reconcile(context.Background()))
	require.True(t, reflect.DeepEqual(applied, *backend.applied))
}

func TestNFTAuditRepairsElementDrift(t *testing.T) {
	fake := knftables.NewFake(knftables.IPv4Family, nftGatewayTable)
	desired := gatewayNFTSnapshot{Families: []nftFamilySnapshot{{
		Family:  knftables.IPv4Family,
		Table:   nftGatewayTable,
		Subnets: []string{"10.16.0.0/24"},
	}}}
	backend := &nftGatewayBackend{
		writer:        fake,
		readers:       map[knftables.Family]knftables.Interface{knftables.IPv4Family: fake},
		buildSnapshot: func() (gatewayNFTSnapshot, error) { return desired, nil },
		auditInterval: time.Minute,
	}

	require.NoError(t, backend.Reconcile(context.Background()))
	external := fake.NewTransaction()
	external.Delete(&knftables.Element{Set: "subnets", Key: []string{"10.16.0.0/24"}})
	require.NoError(t, fake.Run(context.Background(), external))
	backend.lastAudit = time.Now().Add(-2 * time.Minute)

	require.NoError(t, backend.Reconcile(context.Background()))
	fake.RLock()
	dump := fake.LastTransaction.String()
	fake.RUnlock()
	require.Contains(t, dump, "add element ip kube-ovn subnets { 10.16.0.0/24 }")
}

func TestNFTAuditRepairsRuleDrift(t *testing.T) {
	fake := knftables.NewFake(knftables.IPv4Family, nftGatewayTable)
	desired := gatewayNFTSnapshot{Families: []nftFamilySnapshot{{
		Family:  knftables.IPv4Family,
		Table:   nftGatewayTable,
		Subnets: []string{"10.16.0.0/24"},
	}}}
	backend := &nftGatewayBackend{
		writer:        fake,
		readers:       map[knftables.Family]knftables.Interface{knftables.IPv4Family: fake},
		buildSnapshot: func() (gatewayNFTSnapshot, error) { return desired, nil },
		auditInterval: time.Minute,
	}

	require.NoError(t, backend.Reconcile(context.Background()))
	fake.RLock()
	handle := *fake.Table.Chains["nat-postrouting"].Rules[0].Handle
	fake.RUnlock()
	external := fake.NewTransaction()
	external.Delete(&knftables.Rule{Chain: "nat-postrouting", Handle: &handle})
	require.NoError(t, fake.Run(context.Background(), external))
	backend.lastAudit = time.Now().Add(-2 * time.Minute)

	require.NoError(t, backend.Reconcile(context.Background()))
	fake.RLock()
	dump := fake.LastTransaction.String()
	fake.RUnlock()
	require.Contains(t, dump, "flush chain ip kube-ovn nat-postrouting")
	require.Contains(t, dump, `comment "nat-service"`)
}

func TestNFTAuditRepairsRuleContentDrift(t *testing.T) {
	fake := knftables.NewFake(knftables.IPv4Family, nftGatewayTable)
	desired := gatewayNFTSnapshot{Families: []nftFamilySnapshot{{
		Family:  knftables.IPv4Family,
		Table:   nftGatewayTable,
		Subnets: []string{"10.16.0.0/24"},
	}}}
	backend := &nftGatewayBackend{
		writer:        fake,
		readers:       map[knftables.Family]knftables.Interface{knftables.IPv4Family: fake},
		buildSnapshot: func() (gatewayNFTSnapshot, error) { return desired, nil },
		auditInterval: time.Minute,
	}

	require.NoError(t, backend.Reconcile(context.Background()))
	fake.RLock()
	rule := fake.Table.Chains["nat-postrouting"].Rules[0]
	handle, comment := *rule.Handle, *rule.Comment
	fake.RUnlock()
	external := fake.NewTransaction()
	external.Replace(&knftables.Rule{Chain: "nat-postrouting", Handle: &handle, Rule: "accept", Comment: &comment})
	require.NoError(t, fake.Run(context.Background(), external))
	backend.lastAudit = time.Now().Add(-2 * time.Minute)

	require.NoError(t, backend.Reconcile(context.Background()))
	fake.RLock()
	dump := fake.LastTransaction.String()
	fake.RUnlock()
	require.Contains(t, dump, "flush chain ip kube-ovn nat-postrouting")
	require.Contains(t, dump, `comment "nat-service"`)
}

func TestNFTAuditRestoresCountersBeforeRules(t *testing.T) {
	fake := knftables.NewFake(knftables.IPv4Family, nftGatewayTable)
	counter := nftSubnetCounter{UID: "subnet-uid", Name: "subnet-a", CIDR: "10.16.0.0/24"}
	desired := gatewayNFTSnapshot{Families: []nftFamilySnapshot{{
		Family:         knftables.IPv4Family,
		Table:          nftGatewayTable,
		SubnetCounters: []nftSubnetCounter{counter},
	}}}
	backend := &nftGatewayBackend{
		writer:        fake,
		readers:       map[knftables.Family]knftables.Interface{knftables.IPv4Family: fake},
		buildSnapshot: func() (gatewayNFTSnapshot, error) { return desired, nil },
		auditInterval: time.Minute,
	}

	require.NoError(t, backend.Reconcile(context.Background()))
	egress, _ := nftSubnetCounterNames(counter)
	fake.Lock()
	delete(fake.Table.Counters, egress)
	fake.Unlock()
	backend.lastAudit = time.Now().Add(-2 * time.Minute)

	require.NoError(t, backend.Reconcile(context.Background()))
	fake.RLock()
	dump := fake.LastTransaction.String()
	fake.RUnlock()
	counterPosition := strings.Index(dump, "add counter ip kube-ovn "+egress)
	rulePosition := strings.Index(dump, "counter name "+egress)
	require.NotEqual(t, -1, counterPosition)
	require.NotEqual(t, -1, rulePosition)
	require.Less(t, counterPosition, rulePosition)
}

func TestNFTCleanupBothFamilies(t *testing.T) {
	fake := knftables.NewFake("", "")
	backend := &nftGatewayBackend{writer: fake}

	require.NoError(t, backend.Cleanup(context.Background()))
	fake.RLock()
	dump := fake.LastTransaction.String()
	fake.RUnlock()
	require.Contains(t, dump, "destroy table ip kube-ovn")
	require.Contains(t, dump, "destroy table ip6 kube-ovn")
}

type failingNFTInterface struct {
	knftables.Interface
	fail bool
}

func (f *failingNFTInterface) Run(ctx context.Context, tx *knftables.Transaction) error {
	if f.fail {
		return errors.New("transaction failed")
	}
	return f.Interface.Run(ctx, tx)
}

func BenchmarkNFTSnapshotDiff(b *testing.B) {
	oldFamily := nftFamilySnapshot{Family: knftables.IPv4Family, Table: nftGatewayTable}
	oldFamily.ClusterIPPorts = make([]nftAddressPort, 10_000)
	for i := range oldFamily.ClusterIPPorts {
		oldFamily.ClusterIPPorts[i] = nftAddressPort{
			Address:  net.IPv4(10, byte(i>>16), byte(i>>8), byte(i)).String(),
			Protocol: "tcp",
			Port:     int32(1 + i%65535),
		}
	}
	newFamily := oldFamily
	newFamily.ClusterIPPorts = slices.Clone(oldFamily.ClusterIPPorts)
	newFamily.ClusterIPPorts[len(newFamily.ClusterIPPorts)-1].Port = 443
	backend := &nftGatewayBackend{writer: knftables.NewFake("", "")}
	oldSnapshot := gatewayNFTSnapshot{Families: []nftFamilySnapshot{oldFamily}}
	newSnapshot := gatewayNFTSnapshot{Families: []nftFamilySnapshot{newFamily}}

	b.ResetTimer()
	for range b.N {
		_ = backend.renderDiff(oldSnapshot, newSnapshot)
	}
}
