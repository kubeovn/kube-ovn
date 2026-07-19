package daemon

import (
	"strconv"

	"sigs.k8s.io/knftables"
)

const nftGatewayTable = "kube-ovn"

type nftGatewayBackend struct {
	writer  knftables.Interface
	readers map[knftables.Family]knftables.Interface
}

func (b *nftGatewayBackend) renderFull(snapshot gatewayNFTSnapshot) *knftables.Transaction {
	tx := b.writer.NewTransaction()
	for _, family := range snapshot.Families {
		b.renderNFTBaseSchema(tx, family)
		b.renderNFTSets(tx, family)
		b.renderNFTNATRules(tx, family)
	}
	return tx
}

func (*nftGatewayBackend) renderNFTBaseSchema(tx *knftables.Transaction, snapshot nftFamilySnapshot) {
	family, table := snapshot.Family, snapshot.Table
	if table == "" {
		table = nftGatewayTable
	}
	tx.Add(&knftables.Table{Family: family, Name: table})
	tx.Add(&knftables.Chain{Family: family, Table: table, Name: "schema-v1"})
	for _, name := range []string{"nodeport-local", "nodeport-local-action", "nat-policy"} {
		tx.Add(&knftables.Chain{Family: family, Table: table, Name: name})
	}

	baseChains := []knftables.Chain{
		{
			Name:     "tproxy-prerouting",
			Type:     knftables.PtrTo(knftables.FilterType),
			Hook:     knftables.PtrTo(knftables.PreroutingHook),
			Priority: knftables.PtrTo(knftables.ManglePriority),
		},
		{
			Name:     "tproxy-output",
			Type:     knftables.PtrTo(knftables.RouteType),
			Hook:     knftables.PtrTo(knftables.OutputHook),
			Priority: knftables.PtrTo(knftables.ManglePriority),
		},
		{
			Name:     "filter-input",
			Type:     knftables.PtrTo(knftables.FilterType),
			Hook:     knftables.PtrTo(knftables.InputHook),
			Priority: knftables.PtrTo(knftables.FilterPriority + "-10"),
		},
		{
			Name:     "filter-forward",
			Type:     knftables.PtrTo(knftables.FilterType),
			Hook:     knftables.PtrTo(knftables.ForwardHook),
			Priority: knftables.PtrTo(knftables.FilterPriority + "-10"),
		},
		{
			Name:     "filter-output",
			Type:     knftables.PtrTo(knftables.FilterType),
			Hook:     knftables.PtrTo(knftables.OutputHook),
			Priority: knftables.PtrTo(knftables.FilterPriority + "-10"),
		},
		{
			Name:     "mangle-postrouting",
			Type:     knftables.PtrTo(knftables.FilterType),
			Hook:     knftables.PtrTo(knftables.PostroutingHook),
			Priority: knftables.PtrTo(knftables.ManglePriority),
		},
		{
			Name:     "nat-host-service",
			Type:     knftables.PtrTo(knftables.NATType),
			Hook:     knftables.PtrTo(knftables.PostroutingHook),
			Priority: knftables.PtrTo(knftables.SNATPriority + "-10"),
		},
		{
			Name:     "nat-postrouting",
			Type:     knftables.PtrTo(knftables.NATType),
			Hook:     knftables.PtrTo(knftables.PostroutingHook),
			Priority: knftables.PtrTo(knftables.SNATPriority + "+10"),
		},
	}
	for i := range baseChains {
		baseChains[i].Family = family
		baseChains[i].Table = table
		tx.Add(&baseChains[i])
	}
}

func (*nftGatewayBackend) renderNFTSets(tx *knftables.Transaction, snapshot nftFamilySnapshot) {
	family, table := snapshot.Family, nftSnapshotTable(snapshot)
	addressType := "ipv4_addr"
	if family == knftables.IPv6Family {
		addressType = "ipv6_addr"
	}

	interval := []knftables.SetFlag{knftables.IntervalFlag}
	sets := []knftables.Set{
		{Family: family, Table: table, Name: "cluster-ip-ports", Type: addressType + " . inet_proto . inet_service"},
		{Family: family, Table: table, Name: "service-vip-ports", Type: addressType + " . inet_proto . inet_service"},
		{Family: family, Table: table, Name: "subnets", Type: addressType, Flags: interval},
		{Family: family, Table: table, Name: "nat-subnets", Type: addressType, Flags: interval},
		{Family: family, Table: table, Name: "distributed-gw-subnets", Type: addressType, Flags: interval},
		{Family: family, Table: table, Name: "other-node-ips", Type: addressType},
		{Family: family, Table: table, Name: "node-ips", Type: addressType},
		{Family: family, Table: table, Name: "local-nodeports", Type: "inet_proto . inet_service"},
	}
	for i := range sets {
		tx.Add(&sets[i])
	}

	for _, item := range snapshot.ClusterIPPorts {
		addNFTSetElement(tx, family, table, "cluster-ip-ports", item.Address, item.Protocol, strconv.FormatInt(int64(item.Port), 10))
	}
	for _, item := range snapshot.ServiceVIPPorts {
		addNFTSetElement(tx, family, table, "service-vip-ports", item.Address, item.Protocol, strconv.FormatInt(int64(item.Port), 10))
	}
	for _, item := range snapshot.Subnets {
		addNFTSetElement(tx, family, table, "subnets", item)
	}
	for _, item := range snapshot.NATSubnets {
		addNFTSetElement(tx, family, table, "nat-subnets", item)
	}
	for _, item := range snapshot.DistributedGWSubnets {
		addNFTSetElement(tx, family, table, "distributed-gw-subnets", item)
	}
	for _, item := range snapshot.OtherNodeIPs {
		addNFTSetElement(tx, family, table, "other-node-ips", item)
	}
	for _, item := range snapshot.NodeIPs {
		addNFTSetElement(tx, family, table, "node-ips", item)
	}
	for _, item := range snapshot.LocalNodePorts {
		addNFTSetElement(tx, family, table, "local-nodeports", item.Protocol, strconv.FormatInt(int64(item.Port), 10))
	}
}

func addNFTSetElement(tx *knftables.Transaction, family knftables.Family, table, set string, key ...string) {
	tx.Add(&knftables.Element{Family: family, Table: table, Set: set, Key: key})
}

func (*nftGatewayBackend) renderNFTNATRules(tx *knftables.Transaction, snapshot nftFamilySnapshot) {
	family, table := snapshot.Family, nftSnapshotTable(snapshot)
	ipToken := "ip"
	if family == knftables.IPv6Family {
		ipToken = "ip6"
	}
	originalDestination := "ct original " + ipToken + " daddr"
	originalSource := "ct original " + ipToken + " saddr"

	if snapshot.NodeInternalIP != "" {
		addNFTRule(tx, family, table, "nat-host-service", knftables.Concat(
			"ct status dnat",
			"fib saddr type local",
			ipToken+" daddr @subnets",
			originalDestination+" . ct original protocol . ct original proto-dst @cluster-ip-ports",
			"snat to", snapshot.NodeInternalIP, "fully-random",
		), "host-service-snat")
	}

	addNFTRule(tx, family, table, "nodeport-local", knftables.Concat(
		originalDestination+" . ct original protocol . ct original proto-dst @service-vip-ports",
		"return",
	), "nodeport-vip-guard")
	addNFTRule(tx, family, table, "nodeport-local", "ct original protocol . ct original proto-dst @local-nodeports jump nodeport-local-action", "nodeport-local-match")
	addNFTRule(tx, family, table, "nodeport-local", "return", "nodeport-local-return")

	addNFTRule(tx, family, table, "nodeport-local-action", originalSource+" @other-node-ips masquerade fully-random", "nodeport-other-node")
	addNFTRule(tx, family, table, "nodeport-local-action", ipToken+" daddr @distributed-gw-subnets accept", "nodeport-distributed")
	addNFTRule(tx, family, table, "nodeport-local-action", "masquerade fully-random", "nodeport-centralized")

	addNFTRule(tx, family, table, "nat-postrouting", knftables.Concat(
		"ct status dnat",
		`iifname "ovn0"`,
		ipToken+" saddr @subnets",
		ipToken+" daddr @subnets",
		originalDestination+" . ct original protocol . ct original proto-dst @cluster-ip-ports",
		"masquerade fully-random",
	), "nat-service")
	addNFTRule(tx, family, table, "nat-postrouting", knftables.Concat(
		ipToken+" saddr @subnets",
		ipToken+" daddr @subnets",
		"masquerade fully-random",
	), "nat-subnet-between")
	addNFTRule(tx, family, table, "nat-postrouting", knftables.Concat(
		"ct status dnat",
		ipToken+" daddr @subnets",
		originalDestination+" @node-ips",
		"jump nodeport-local",
	), "nat-nodeport")
	addNFTRule(tx, family, table, "nat-postrouting", "tcp flags & syn == 0 ct state new accept", "nat-direct-routing")
	addNFTRule(tx, family, table, "nat-postrouting", knftables.Concat(
		ipToken+" saddr != @subnets",
		ipToken+" saddr != @other-node-ips",
		ipToken+" daddr @nat-subnets",
		"accept",
	), "nat-route-traffic")
	addNFTRule(tx, family, table, "nat-postrouting", "jump nat-policy", "nat-policy")
	for _, item := range snapshot.CentralizedSNATs {
		addNFTRule(tx, family, table, "nat-postrouting", knftables.Concat(
			ipToken+" saddr", item.CIDR,
			ipToken+" daddr != @subnets",
			"snat to", item.IP, "fully-random",
		), "nat-centralized-snat:"+item.CIDR)
	}
	addNFTRule(tx, family, table, "nat-postrouting", knftables.Concat(
		ipToken+" saddr @nat-subnets",
		ipToken+" daddr != @subnets",
		"masquerade fully-random",
	), "nat-default")
}

func addNFTRule(tx *knftables.Transaction, family knftables.Family, table, chain, rule, comment string) {
	tx.Add(&knftables.Rule{
		Family:  family,
		Table:   table,
		Chain:   chain,
		Rule:    rule,
		Comment: new(comment),
	})
}

func nftSnapshotTable(snapshot nftFamilySnapshot) string {
	if snapshot.Table == "" {
		return nftGatewayTable
	}
	return snapshot.Table
}
