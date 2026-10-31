package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
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
	require.Contains(t, dump, "add chain ip kube-ovn raw-prerouting { type filter hook prerouting priority -300 ;")
	require.Contains(t, dump, "add chain ip kube-ovn raw-output { type filter hook output priority -300 ;")
}

func TestRenderNFTTunnelNoTrack(t *testing.T) {
	t.Run("IPv4 Geneve 6081 notrack", func(t *testing.T) {
		dump := renderTestSnapshot(t, nftFamilySnapshot{
			Family:       knftables.IPv4Family,
			Table:        nftGatewayTable,
			TunnelPort:   6081,
			OtherNodeIPs: []string{"192.168.1.11"},
		})
		require.Contains(t, dump, "add rule ip kube-ovn raw-prerouting udp dport 6081 ip saddr @other-node-ips ip daddr @local-encap-ips notrack")
		require.Contains(t, dump, "add rule ip kube-ovn raw-output udp dport 6081 ip saddr @local-encap-ips ip daddr @other-node-ips notrack")
	})

	t.Run("IPv6 VXLAN 4789 notrack", func(t *testing.T) {
		dump := renderTestSnapshot(t, nftFamilySnapshot{
			Family:       knftables.IPv6Family,
			Table:        nftGatewayTable,
			TunnelPort:   4789,
			OtherNodeIPs: []string{"fd00::11"},
		})
		require.Contains(t, dump, "add rule ip6 kube-ovn raw-prerouting udp dport 4789 ip6 saddr @other-node-ips ip6 daddr @local-encap-ips notrack")
		require.Contains(t, dump, "add rule ip6 kube-ovn raw-output udp dport 4789 ip6 saddr @local-encap-ips ip6 daddr @other-node-ips notrack")
	})

	t.Run("No tunnel 0 port", func(t *testing.T) {
		dump := renderTestSnapshot(t, nftFamilySnapshot{
			Family:       knftables.IPv4Family,
			Table:        nftGatewayTable,
			TunnelPort:   0,
			OtherNodeIPs: []string{"192.168.1.11"},
		})
		require.NotContains(t, dump, "notrack")
	})
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

	require.Contains(t, dump, "tcp dport 8080 meta mark set ((meta mark & 0xfff6fffc) | 0x90003)")
	require.Contains(t, dump, "tcp dport 8080 tproxy ip to 192.168.1.10:8102 meta mark set ((meta mark & 0xfff6fffb) | 0x90004)")
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

func TestNFTAuditRepairsObjectDefinitionDrift(t *testing.T) {
	fake := knftables.NewFake(knftables.IPv4Family, nftGatewayTable)
	drift := false
	desired := gatewayNFTSnapshot{Families: []nftFamilySnapshot{{
		Family:  knftables.IPv4Family,
		Table:   nftGatewayTable,
		Subnets: []string{"10.16.0.0/24"},
	}}}
	backend := &nftGatewayBackend{
		writer:           fake,
		readers:          map[knftables.Family]knftables.Interface{knftables.IPv4Family: fake},
		buildSnapshot:    func() (gatewayNFTSnapshot, error) { return desired, nil },
		checkDefinitions: func(context.Context, nftFamilySnapshot) (bool, error) { return !drift, nil },
		auditInterval:    time.Minute,
	}

	require.NoError(t, backend.Reconcile(context.Background()))
	before := testutil.ToFloat64(metricGatewayNFTRepairs)
	badPriority := knftables.BaseChainPriority("123")
	fake.Lock()
	fake.Table.Chains["filter-input"].Priority = &badPriority
	fake.Table.Sets["subnets"].Flags = nil
	fake.Unlock()
	drift = true
	backend.lastAudit = time.Now().Add(-2 * time.Minute)

	require.NoError(t, backend.Reconcile(context.Background()))
	fake.RLock()
	priority := *fake.Table.Chains["filter-input"].Priority
	flags := slices.Clone(fake.Table.Sets["subnets"].Flags)
	fake.RUnlock()
	require.Equal(t, knftables.FilterPriority+"-10", priority)
	require.Contains(t, flags, knftables.IntervalFlag)
	require.Equal(t, before+1, testutil.ToFloat64(metricGatewayNFTRepairs))
}

func TestNFTAuditFlushesChainReferencesBeforeDeletingChains(t *testing.T) {
	fake := knftables.NewFake("", "")
	reader := &orderedNFTReader{
		Interface: fake,
		objects: map[string][]string{
			"chain": {"nat-policy", "nat-postrouting"},
		},
	}
	backend := &nftGatewayBackend{
		writer:  fake,
		readers: map[knftables.Family]knftables.Interface{knftables.IPv6Family: reader},
	}
	desired := gatewayNFTSnapshot{Families: []nftFamilySnapshot{{
		Family: knftables.IPv6Family,
		Table:  nftGatewayTable,
	}}}

	tx, repaired, err := backend.renderAuditRepair(t.Context(), desired)
	require.NoError(t, err)
	require.True(t, repaired)
	dump := tx.String()
	policyDelete := strings.Index(dump, "delete chain ip6 kube-ovn nat-policy")
	postroutingFlush := strings.Index(dump, "flush chain ip6 kube-ovn nat-postrouting")
	require.NotEqual(t, -1, policyDelete)
	require.NotEqual(t, -1, postroutingFlush)
	require.Less(t, postroutingFlush, policyDelete)
}

func TestParseNFTDefinitions(t *testing.T) {
	data := []byte(`{"nftables":[
		{"set":{"name":"subnets","type":"ipv4_addr","flags":["interval"]}},
		{"set":{"name":"ports","type":["ipv4_addr","inet_proto","inet_service"]}},
		{"chain":{"name":"filter-input","type":"filter","hook":"input","prio":-10}},
		{"chain":{"name":"regular"}}
	]}`)

	actual, err := parseNFTDefinitions(data)
	require.NoError(t, err)
	require.Equal(t, nftTableDefinitions{
		Chains: map[string]nftChainDefinition{
			"filter-input": {Type: "filter", Hook: "input", Priority: new(-10)},
			"regular":      {},
		},
		Sets: map[string]nftSetDefinition{
			"subnets": {Type: "ipv4_addr", Flags: []string{"interval"}},
			"ports":   {Type: "ipv4_addr . inet_proto . inet_service"},
		},
	}, actual)
}

func TestExpectedNFTDefinitionsPreservesMissingFlags(t *testing.T) {
	definitions, err := expectedNFTDefinitions(nftFamilySnapshot{Family: knftables.IPv4Family, Table: nftGatewayTable})
	require.NoError(t, err)
	require.Nil(t, definitions.Sets["cluster-ip-ports"].Flags)
	require.Equal(t, []string{"interval"}, definitions.Sets["subnets"].Flags)
	require.Equal(t, "accept", definitions.Chains["filter-input"].Policy)
}

func TestNFTAuditDoesNotCountHealthyRefreshAsRepair(t *testing.T) {
	fake := knftables.NewFake(knftables.IPv4Family, nftGatewayTable)
	desired := gatewayNFTSnapshot{Families: []nftFamilySnapshot{{
		Family: knftables.IPv4Family,
		Table:  nftGatewayTable,
	}}}
	backend := &nftGatewayBackend{
		writer:           fake,
		readers:          map[knftables.Family]knftables.Interface{knftables.IPv4Family: fake},
		buildSnapshot:    func() (gatewayNFTSnapshot, error) { return desired, nil },
		checkDefinitions: func(context.Context, nftFamilySnapshot) (bool, error) { return true, nil },
		auditInterval:    time.Minute,
	}

	require.NoError(t, backend.Reconcile(context.Background()))
	before := testutil.ToFloat64(metricGatewayNFTRepairs)
	transactions := testutil.ToFloat64(metricGatewayNFTTransactions)
	backend.lastAudit = time.Now().Add(-2 * time.Minute)
	require.NoError(t, backend.Reconcile(context.Background()))
	require.Equal(t, before, testutil.ToFloat64(metricGatewayNFTRepairs))
	require.Equal(t, transactions, testutil.ToFloat64(metricGatewayNFTTransactions))
}

func TestNFTAuditReturnsDefinitionCheckFailure(t *testing.T) {
	fake := knftables.NewFake(knftables.IPv4Family, nftGatewayTable)
	failure := false
	desired := gatewayNFTSnapshot{Families: []nftFamilySnapshot{{
		Family: knftables.IPv4Family,
		Table:  nftGatewayTable,
	}}}
	backend := &nftGatewayBackend{
		writer:        fake,
		readers:       map[knftables.Family]knftables.Interface{knftables.IPv4Family: fake},
		buildSnapshot: func() (gatewayNFTSnapshot, error) { return desired, nil },
		checkDefinitions: func(context.Context, nftFamilySnapshot) (bool, error) {
			if failure {
				return false, errors.New("nft check failed")
			}
			return true, nil
		},
		auditInterval: time.Minute,
	}

	require.NoError(t, backend.Reconcile(context.Background()))
	before := testutil.ToFloat64(metricGatewayNFTRepairs)
	failure = true
	backend.lastAudit = time.Now().Add(-2 * time.Minute)

	err := backend.Reconcile(context.Background())
	require.ErrorContains(t, err, "audit ip nftables definitions")
	require.Equal(t, before, testutil.ToFloat64(metricGatewayNFTRepairs))
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

type orderedNFTReader struct {
	knftables.Interface
	objects map[string][]string
}

func (r *orderedNFTReader) ListAll(context.Context) (map[string][]string, error) {
	return r.objects, nil
}

func (f *failingNFTInterface) Run(ctx context.Context, tx *knftables.Transaction) error {
	if f.fail {
		return errors.New("transaction failed")
	}
	return f.Interface.Run(ctx, tx)
}

func BenchmarkNFTSnapshotDiff(b *testing.B) {
	oldFamily := nftFamilySnapshot{
		Family: knftables.IPv4Family, Table: nftGatewayTable,
		ClusterIPPorts: make([]nftAddressPort, 10_000),
	}
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

func TestNFTTunnelNoTrackReconcile(t *testing.T) {
	for _, family := range []knftables.Family{knftables.IPv4Family, knftables.IPv6Family} {
		t.Run(string(family), func(t *testing.T) {
			fake := knftables.NewFake("", "")
			desired := gatewayNFTSnapshot{Families: []nftFamilySnapshot{{Family: family, Table: nftGatewayTable, TunnelPort: 6081}}}
			backend := &nftGatewayBackend{writer: fake, buildSnapshot: func() (gatewayNFTSnapshot, error) { return desired, nil }}
			require.NoError(t, backend.Reconcile(context.Background()))
			for i := range 3 {
				desired.Families[0].SubnetCounters = []nftSubnetCounter{{Name: "subnet", CIDR: "10.0.0.0/24", UID: strings.Repeat("a", i+1)}}
				require.NoError(t, backend.Reconcile(context.Background()))
				require.Len(t, fake.Tables[family][nftGatewayTable].Chains["raw-prerouting"].Rules, 1)
				require.Len(t, fake.Tables[family][nftGatewayTable].Chains["raw-output"].Rules, 1)
			}
			for _, port := range []int{4789, 0, 6081} {
				desired.Families[0].TunnelPort = port
				require.NoError(t, backend.Reconcile(context.Background()))
				for _, chain := range []string{"raw-prerouting", "raw-output"} {
					rules := fake.Tables[family][nftGatewayTable].Chains[chain].Rules
					if port == 0 {
						require.Empty(t, rules)
					} else {
						require.Len(t, rules, 1)
						require.Contains(t, rules[0].Rule, fmt.Sprintf("udp dport %d", port))
					}
				}
			}
		})
	}
}

func TestNFTTunnelNoTrackNetfilter(t *testing.T) {
	if os.Getenv("KUBE_OVN_TEST_NETFILTER") != "true" {
		t.Skip("requires isolated Linux netfilter environment")
	}
	writer, err := knftables.New("", "", knftables.EmulateDestroy)
	require.NoError(t, err)
	for _, family := range []knftables.Family{knftables.IPv4Family, knftables.IPv6Family} {
		t.Run(string(family), func(t *testing.T) {
			table := "kube-ovn-notrack-test"
			peer, cidr := "172.18.0.2", "10.0.0.0/24"
			if family == knftables.IPv6Family {
				peer, cidr = "fd00::2", "fd01::/64"
			}
			desired := gatewayNFTSnapshot{Families: []nftFamilySnapshot{{Family: family, Table: table, TunnelPort: 6081, OtherNodeIPs: []string{peer}}}}
			backend := &nftGatewayBackend{writer: writer, buildSnapshot: func() (gatewayNFTSnapshot, error) { return desired, nil }}
			t.Cleanup(func() {
				tx := writer.NewTransaction()
				tx.Destroy(&knftables.Table{Family: family, Name: table})
				require.NoError(t, writer.Run(context.Background(), tx))
			})
			require.NoError(t, backend.Reconcile(context.Background()))
			for i, port := range []int{6081, 4789, 0, 6081} {
				desired.Families[0].TunnelPort = port
				desired.Families[0].SubnetCounters = []nftSubnetCounter{{UID: strconv.Itoa(i), Name: "subnet", CIDR: cidr}}
				require.NoError(t, backend.Reconcile(context.Background()))
				reader, err := knftables.New(family, table)
				require.NoError(t, err)
				for _, chain := range []string{"raw-prerouting", "raw-output"} {
					rules, err := reader.ListRules(context.Background(), chain)
					require.NoError(t, err)
					if port == 0 {
						require.Empty(t, rules)
					} else {
						require.Len(t, rules, 1)
					}
				}
			}
		})
	}
}

func TestNFTLocalEncapAddressChange(t *testing.T) {
	fake := knftables.NewFake("", "")
	desired := gatewayNFTSnapshot{Families: []nftFamilySnapshot{{Family: knftables.IPv4Family, Table: nftGatewayTable, TunnelPort: 6081, LocalEncapIPs: []string{"192.0.2.10"}}}}
	backend := &nftGatewayBackend{writer: fake, buildSnapshot: func() (gatewayNFTSnapshot, error) { return desired, nil }}
	require.NoError(t, backend.Reconcile(context.Background()))
	desired.Families[0].LocalEncapIPs[0] = "192.0.2.20"
	require.NoError(t, backend.Reconcile(context.Background()))
	require.Contains(t, fake.LastTransaction.String(), "delete element ip kube-ovn local-encap-ips { 192.0.2.10 }")
	require.Contains(t, fake.LastTransaction.String(), "add element ip kube-ovn local-encap-ips { 192.0.2.20 }")
	require.Len(t, fake.Tables[knftables.IPv4Family][nftGatewayTable].Chains["raw-prerouting"].Rules, 1)
}

func TestNFTTunnelFamilyRemoval(t *testing.T) {
	fake := knftables.NewFake("", "")
	input := nftSnapshotInput{Protocol: "IPv6", TunnelPort: 6081, LocalEncapIPs: []string{"192.0.2.10"}, TunnelPeerIPs: []string{"192.0.2.20"}}
	backend := &nftGatewayBackend{writer: fake, buildSnapshot: func() (gatewayNFTSnapshot, error) { return buildNFTGatewaySnapshot(input) }}
	require.NoError(t, backend.Reconcile(context.Background()))
	require.NotNil(t, fake.Tables[knftables.IPv4Family][nftGatewayTable])
	input.LocalEncapIPs = []string{"2001:db8::10"}
	input.TunnelPeerIPs = []string{"2001:db8::20"}
	require.NoError(t, backend.Reconcile(context.Background()))
	require.Nil(t, fake.Tables[knftables.IPv4Family][nftGatewayTable])
	require.NotNil(t, fake.Tables[knftables.IPv6Family][nftGatewayTable])
}

func TestNFTAuditRemovesInactiveTunnelFamily(t *testing.T) {
	fake := knftables.NewFake("", "")
	old := gatewayNFTSnapshot{Families: []nftFamilySnapshot{{Family: knftables.IPv4Family, Table: nftGatewayTable, TunnelOnly: true, TunnelPort: 6081}}}
	backend := newNFTGatewayBackendForTest(fake)
	require.NoError(t, fake.Run(context.Background(), backend.renderFull(old)))
	reader := knftables.NewFake(knftables.IPv4Family, nftGatewayTable)
	require.NoError(t, reader.Run(context.Background(), backend.renderFull(old)))
	backend.readers = map[knftables.Family]knftables.Interface{knftables.IPv4Family: reader}
	backend.buildSnapshot = func() (gatewayNFTSnapshot, error) {
		return buildNFTGatewaySnapshot(nftSnapshotInput{Protocol: "IPv6", TunnelPort: 6081, LocalEncapIPs: []string{"2001:db8::10"}})
	}
	require.NoError(t, backend.Reconcile(context.Background()))
	require.Nil(t, fake.Tables[knftables.IPv4Family][nftGatewayTable])
	require.NotNil(t, fake.Tables[knftables.IPv6Family][nftGatewayTable])
}

func TestNFTTunnelOnlyAuditIsStable(t *testing.T) {
	fake := knftables.NewFake(knftables.IPv4Family, nftGatewayTable)
	desired := gatewayNFTSnapshot{Families: []nftFamilySnapshot{{Family: knftables.IPv4Family, Table: nftGatewayTable, TunnelOnly: true, TunnelPort: 6081, LocalEncapIPs: []string{"192.0.2.10"}, OtherNodeIPs: []string{"192.0.2.20"}}}}
	backend := &nftGatewayBackend{writer: fake, readers: map[knftables.Family]knftables.Interface{knftables.IPv4Family: fake}, buildSnapshot: func() (gatewayNFTSnapshot, error) { return desired, nil }, checkDefinitions: func(context.Context, nftFamilySnapshot) (bool, error) { return true, nil }, auditInterval: time.Minute}
	require.NoError(t, backend.Reconcile(context.Background()))
	before := testutil.ToFloat64(metricGatewayNFTTransactions)
	backend.lastAudit = time.Now().Add(-2 * time.Minute)
	require.NoError(t, backend.Reconcile(context.Background()))
	require.Equal(t, before, testutil.ToFloat64(metricGatewayNFTTransactions))
}

func TestNFTTunnelOnlyNodeAddressChange(t *testing.T) {
	fake := knftables.NewFake("", "")
	desired := gatewayNFTSnapshot{Families: []nftFamilySnapshot{{Family: knftables.IPv4Family, Table: nftGatewayTable, TunnelOnly: true, TunnelPort: 6081, NodeInternalIP: "192.0.2.10"}}}
	backend := &nftGatewayBackend{writer: fake, buildSnapshot: func() (gatewayNFTSnapshot, error) { return desired, nil }}
	require.NoError(t, backend.Reconcile(context.Background()))
	desired.Families[0].NodeInternalIP = "192.0.2.20"
	require.NoError(t, backend.Reconcile(context.Background()))
	require.NotContains(t, fake.Tables[knftables.IPv4Family][nftGatewayTable].Chains, "nat-postrouting")
}
