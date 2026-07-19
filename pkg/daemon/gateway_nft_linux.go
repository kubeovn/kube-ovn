package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/knftables"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const nftGatewayTable = "kube-ovn"

type nftGatewayBackend struct {
	mutex         sync.Mutex
	controller    *Controller
	writer        knftables.Interface
	readers       map[knftables.Family]knftables.Interface
	buildSnapshot func() (gatewayNFTSnapshot, error)
	applied       *gatewayNFTSnapshot
	lastAudit     time.Time
	auditInterval time.Duration
	counterValues map[string]nftCounterValue
}

type nftCounterValue struct {
	packets uint64
	bytes   uint64
}

type nftCounterMetadata struct {
	subnetName string
	cidr       string
	direction  string
	protocol   string
}

func newNFTGatewayBackend(controller *Controller) (*nftGatewayBackend, error) {
	writer, err := knftables.New("", "", knftables.EmulateDestroy)
	if err != nil {
		return nil, fmt.Errorf("初始化 nft writer: %w", err)
	}
	readers := make(map[knftables.Family]knftables.Interface, 2)
	for _, family := range []knftables.Family{knftables.IPv4Family, knftables.IPv6Family} {
		reader, err := knftables.New(family, nftGatewayTable)
		if err != nil {
			return nil, fmt.Errorf("初始化 %s nft reader: %w", family, err)
		}
		readers[family] = reader
	}
	backend := &nftGatewayBackend{
		controller:    controller,
		writer:        writer,
		readers:       readers,
		auditInterval: 5 * time.Minute,
		counterValues: make(map[string]nftCounterValue),
	}
	backend.buildSnapshot = controller.buildNFTSnapshot
	return backend, nil
}

func (*nftGatewayBackend) Name() gatewayNetfilterMode {
	return gatewayNetfilterModeNFTables
}

func (b *nftGatewayBackend) Cleanup(ctx context.Context) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	tx := b.writer.NewTransaction()
	for _, family := range []knftables.Family{knftables.IPv4Family, knftables.IPv6Family} {
		tx.Destroy(&knftables.Table{Family: family, Name: nftGatewayTable})
	}
	if err := b.writer.Run(ctx, tx); err != nil {
		return fmt.Errorf("清理 Kube-OVN nft table: %w", err)
	}
	b.applied = nil
	b.lastAudit = time.Time{}
	clear(b.counterValues)
	return nil
}

func (b *nftGatewayBackend) Reconcile(ctx context.Context) error {
	desired, err := b.buildSnapshot()
	if err != nil {
		return err
	}
	b.mutex.Lock()
	defer b.mutex.Unlock()
	initial := b.applied == nil

	var (
		tx      *knftables.Transaction
		audited bool
	)
	switch {
	case len(b.readers) != 0 && (b.applied == nil || b.auditDue()):
		tx, err = b.renderAuditRepair(ctx, desired)
		if err != nil {
			return err
		}
		audited = true
	case b.applied == nil:
		tx = b.renderFull(desired)
	default:
		tx = b.renderDiff(*b.applied, desired)
	}
	if tx.NumOperations() != 0 {
		if initial {
			if err := b.writer.Check(ctx, tx); err != nil {
				return fmt.Errorf("校验 nft transaction: %w", err)
			}
		}
		if err := b.writer.Run(ctx, tx); err != nil {
			if knftables.IsNotFound(err) {
				b.applied = nil
			}
			return fmt.Errorf("执行 nft transaction: %w", err)
		}
	}

	applied := cloneGatewayNFTSnapshot(desired)
	b.applied = &applied
	if audited {
		b.lastAudit = time.Now()
	}
	return nil
}

func (b *nftGatewayBackend) ReadSubnetCounters(ctx context.Context) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if b.applied == nil || b.controller == nil {
		return nil
	}

	metadata := nftCounterMetadataByName(*b.applied)
	for family, reader := range b.readers {
		counters, err := reader.ListCounters(ctx)
		if err != nil {
			if knftables.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("读取 %s nft counter: %w", family, err)
		}
		for _, counter := range counters {
			key := string(family) + "/" + counter.Name
			meta, ok := metadata[key]
			if !ok {
				continue
			}
			current := nftCounterValue{packets: nftUint64(counter.Packets), bytes: nftUint64(counter.Bytes)}
			previous, exists := b.counterValues[key]
			b.counterValues[key] = current
			if !exists || current.packets < previous.packets || current.bytes < previous.bytes {
				continue
			}
			metricName := strings.Join([]string{meta.subnetName, meta.direction, meta.protocol}, "/")
			metricOvnSubnetGatewayPackets.WithLabelValues(b.controller.config.NodeName, metricName, meta.cidr, meta.direction, meta.protocol).Add(float64(current.packets - previous.packets))
			metricOvnSubnetGatewayPacketBytes.WithLabelValues(b.controller.config.NodeName, metricName, meta.cidr, meta.direction, meta.protocol).Add(float64(current.bytes - previous.bytes))
		}
	}
	return nil
}

func (c *Controller) buildNFTSnapshot() (gatewayNFTSnapshot, error) {
	services, err := c.servicesLister.List(labels.Everything())
	if err != nil {
		return gatewayNFTSnapshot{}, fmt.Errorf("列出 Service: %w", err)
	}
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		return gatewayNFTSnapshot{}, fmt.Errorf("列出 Subnet: %w", err)
	}
	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		return gatewayNFTSnapshot{}, fmt.Errorf("列出 Node: %w", err)
	}

	var tproxyPods []*corev1.Pod
	if c.config.EnableTProxy {
		pods, err := c.podsLister.List(labels.Everything())
		if err != nil {
			return gatewayNFTSnapshot{}, fmt.Errorf("列出 Pod: %w", err)
		}
		tproxyPods, err = c.getTProxyConditionPod(pods, true)
		if err != nil {
			return gatewayNFTSnapshot{}, err
		}
	}
	localAddresses, err := localNFTAddresses()
	if err != nil {
		return gatewayNFTSnapshot{}, err
	}
	return buildNFTGatewaySnapshot(nftSnapshotInput{
		Protocol:       c.protocol,
		ClusterRouter:  c.config.ClusterRouter,
		NodeName:       c.config.NodeName,
		Services:       services,
		Subnets:        subnets,
		Nodes:          nodes,
		TProxyPods:     tproxyPods,
		LocalAddresses: localAddresses,
	})
}

func localNFTAddresses() ([]string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("列出本机网络接口: %w", err)
	}
	var result []string
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			return nil, fmt.Errorf("列出接口 %s 地址: %w", iface.Name, err)
		}
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err == nil {
				result = append(result, ip.String())
			}
		}
	}
	return sortedUniqueStrings(result), nil
}

func nftCounterMetadataByName(snapshot gatewayNFTSnapshot) map[string]nftCounterMetadata {
	result := make(map[string]nftCounterMetadata)
	for _, family := range snapshot.Families {
		protocol := kubeovnv1.ProtocolIPv4
		if family.Family == knftables.IPv6Family {
			protocol = kubeovnv1.ProtocolIPv6
		}
		for _, counter := range family.SubnetCounters {
			egress, ingress := nftSubnetCounterNames(counter)
			result[string(family.Family)+"/"+egress] = nftCounterMetadata{subnetName: counter.Name, cidr: counter.CIDR, direction: "egress", protocol: protocol}
			result[string(family.Family)+"/"+ingress] = nftCounterMetadata{subnetName: counter.Name, cidr: counter.CIDR, direction: "ingress", protocol: protocol}
		}
	}
	return result
}

func nftUint64(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

func (b *nftGatewayBackend) auditDue() bool {
	return b.auditInterval > 0 && time.Since(b.lastAudit) >= b.auditInterval
}

func (b *nftGatewayBackend) renderFull(snapshot gatewayNFTSnapshot) *knftables.Transaction {
	tx := b.writer.NewTransaction()
	for _, family := range snapshot.Families {
		b.renderFullFamily(tx, family)
	}
	return tx
}

func (b *nftGatewayBackend) renderFullFamily(tx *knftables.Transaction, family nftFamilySnapshot) {
	b.renderNFTBaseSchema(tx, family)
	b.renderNFTSets(tx, family)
	b.renderNFTNATRules(tx, family)
	b.renderNFTPolicyRules(tx, family)
	b.renderNFTTProxyRules(tx, family)
	b.renderNFTFilterAndMangleRules(tx, family)
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
	renderNFTSetDefinitions(tx, snapshot)
	renderNFTSetElements(tx, snapshot)
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

func (*nftGatewayBackend) renderNFTPolicyRules(tx *knftables.Transaction, snapshot nftFamilySnapshot) {
	family, table := snapshot.Family, nftSnapshotTable(snapshot)
	ipToken := nftIPToken(family)
	for _, policy := range snapshot.NATPolicies {
		id := nftNATPolicyID(policy)
		parts := []any{ipToken + " saddr", policy.SubnetCIDR}
		if len(policy.SrcIPs) != 0 {
			parts = append(parts, ipToken+" saddr @nat-policy-"+id+"-src")
		}
		if len(policy.DstIPs) != 0 {
			parts = append(parts, ipToken+" daddr @nat-policy-"+id+"-dst")
		}
		switch strings.ToLower(policy.Action) {
		case "nat":
			parts = append(parts, "masquerade fully-random")
		case "forward":
			parts = append(parts, "accept")
		default:
			continue
		}
		addNFTRule(tx, family, table, "nat-policy", knftables.Concat(parts...), "nat-policy:"+id)
	}
	addNFTRule(tx, family, table, "nat-policy", "return", "nat-policy:return")
}

func (*nftGatewayBackend) renderNFTTProxyRules(tx *knftables.Transaction, snapshot nftFamilySnapshot) {
	if snapshot.NodeInternalIP == "" {
		return
	}
	family, table := snapshot.Family, nftSnapshotTable(snapshot)
	ipToken := nftIPToken(family)
	tproxyFamily := ipToken
	tproxyAddress := snapshot.NodeInternalIP + ":" + strconv.Itoa(util.TProxyListenPort)
	if family == knftables.IPv6Family {
		tproxyAddress = "[" + snapshot.NodeInternalIP + "]:" + strconv.Itoa(util.TProxyListenPort)
	}
	for _, target := range snapshot.TProxyTargets {
		match := knftables.Concat(ipToken+" daddr", target.Address, "tcp dport", target.Port)
		addNFTRule(tx, family, table, "tproxy-output", knftables.Concat(
			match,
			"meta mark set", fmt.Sprintf("%#x", TProxyOutputMark),
		), "tproxy-output:"+target.Address+":"+strconv.FormatInt(int64(target.Port), 10))
		addNFTRule(tx, family, table, "tproxy-prerouting", knftables.Concat(
			match,
			"tproxy", tproxyFamily, "to", tproxyAddress,
			"meta mark set", fmt.Sprintf("%#x", TProxyPreroutingMark),
		), "tproxy-prerouting:"+target.Address+":"+strconv.FormatInt(int64(target.Port), 10))
	}
}

func (*nftGatewayBackend) renderNFTFilterAndMangleRules(tx *knftables.Transaction, snapshot nftFamilySnapshot) {
	family, table := snapshot.Family, nftSnapshotTable(snapshot)
	ipToken := nftIPToken(family)
	for _, counter := range snapshot.SubnetCounters {
		egress, ingress := nftSubnetCounterNames(counter)
		tx.Add(&knftables.Counter{Family: family, Table: table, Name: egress, Comment: new(counter.Name + " egress")})
		tx.Add(&knftables.Counter{Family: family, Table: table, Name: ingress, Comment: new(counter.Name + " ingress")})
		addNFTRule(tx, family, table, "filter-forward", knftables.Concat(
			ipToken+" saddr", counter.CIDR, "counter name", egress,
		), "counter:"+egress)
		addNFTRule(tx, family, table, "filter-forward", knftables.Concat(
			ipToken+" daddr", counter.CIDR, "counter name", ingress,
		), "counter:"+ingress)
	}

	addNFTRule(tx, family, table, "filter-input", ipToken+" saddr @subnets accept", "filter-input-source")
	addNFTRule(tx, family, table, "filter-input", ipToken+" daddr @subnets accept", "filter-input-destination")
	addNFTRule(tx, family, table, "filter-forward", ipToken+" saddr @subnets accept", "filter-forward-source")
	addNFTRule(tx, family, table, "filter-forward", ipToken+" daddr @subnets accept", "filter-forward-destination")
	addNFTRule(tx, family, table, "filter-output", "udp dport { 6081, 4789 } meta mark set 0", "filter-output-tunnel-unmark")
	addNFTRule(tx, family, table, "mangle-postrouting", knftables.Concat(
		ipToken+" saddr @subnets",
		"tcp flags & rst == rst",
		"ct state invalid drop",
	), "mangle-invalid-rst")
	for _, item := range snapshot.CentralizedSNATs {
		addNFTRule(tx, family, table, "mangle-postrouting", knftables.Concat(
			ipToken+" saddr", item.CIDR,
			"tcp flags & syn == 0",
			"ct state new",
			ipToken+" daddr != @subnets",
			"drop",
		), "mangle-centralized-orphan:"+item.CIDR)
	}
}

func nftIPToken(family knftables.Family) string {
	if family == knftables.IPv6Family {
		return "ip6"
	}
	return "ip"
}

func nftNATPolicyID(policy nftNATPolicy) string {
	return nftStableID(policy.SubnetCIDR + "|" + policy.RuleID)
}

func nftStableID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:6])
}

func nftSubnetCounterNames(counter nftSubnetCounter) (string, string) {
	id := nftStableID(counter.UID + "|" + counter.CIDR)
	return "subnet-" + id + "-egress", "subnet-" + id + "-ingress"
}

func (b *nftGatewayBackend) renderDiff(oldSnapshot, newSnapshot gatewayNFTSnapshot) *knftables.Transaction {
	tx := b.writer.NewTransaction()
	oldFamilies := make(map[knftables.Family]nftFamilySnapshot, len(oldSnapshot.Families))
	for _, family := range oldSnapshot.Families {
		oldFamilies[family.Family] = family
	}

	for _, newFamily := range newSnapshot.Families {
		oldFamily, ok := oldFamilies[newFamily.Family]
		if !ok {
			b.renderNFTBaseSchema(tx, newFamily)
			b.renderNFTSets(tx, newFamily)
			b.renderNFTNATRules(tx, newFamily)
			b.renderNFTPolicyRules(tx, newFamily)
			b.renderNFTTProxyRules(tx, newFamily)
			b.renderNFTFilterAndMangleRules(tx, newFamily)
			continue
		}

		oldDefinitions := nftSetDefinitions(oldFamily)
		newDefinitions := nftSetDefinitions(newFamily)
		for _, name := range sortedMapKeys(newDefinitions) {
			if _, exists := oldDefinitions[name]; !exists {
				definition := newDefinitions[name]
				tx.Add(&definition)
			}
		}
		renderNFTElementDiff(tx, oldFamily, newFamily)

		natChanged := oldFamily.NodeInternalIP != newFamily.NodeInternalIP ||
			!reflect.DeepEqual(oldFamily.CentralizedSNATs, newFamily.CentralizedSNATs)
		policyChanged := !reflect.DeepEqual(nftNATPolicyShapes(oldFamily.NATPolicies), nftNATPolicyShapes(newFamily.NATPolicies))
		tproxyChanged := oldFamily.NodeInternalIP != newFamily.NodeInternalIP ||
			!reflect.DeepEqual(oldFamily.TProxyTargets, newFamily.TProxyTargets)
		filterChanged := !reflect.DeepEqual(oldFamily.SubnetCounters, newFamily.SubnetCounters) ||
			!reflect.DeepEqual(oldFamily.CentralizedSNATs, newFamily.CentralizedSNATs)

		if natChanged {
			flushNFTChains(tx, newFamily, "nat-host-service", "nodeport-local", "nodeport-local-action", "nat-postrouting")
			b.renderNFTNATRules(tx, newFamily)
		}
		if policyChanged {
			flushNFTChains(tx, newFamily, "nat-policy")
			b.renderNFTPolicyRules(tx, newFamily)
		}
		if tproxyChanged {
			flushNFTChains(tx, newFamily, "tproxy-output", "tproxy-prerouting")
			b.renderNFTTProxyRules(tx, newFamily)
		}
		if filterChanged {
			flushNFTChains(tx, newFamily, "filter-input", "filter-forward", "filter-output", "mangle-postrouting")
			b.renderNFTFilterAndMangleRules(tx, newFamily)
		}

		for _, name := range sortedMapKeys(oldDefinitions) {
			if _, exists := newDefinitions[name]; exists {
				continue
			}
			definition := oldDefinitions[name]
			tx.Delete(&definition)
		}
	}
	return tx
}

func renderNFTSetDefinitions(tx *knftables.Transaction, snapshot nftFamilySnapshot) {
	definitions := nftSetDefinitions(snapshot)
	for _, name := range sortedMapKeys(definitions) {
		definition := definitions[name]
		tx.Add(&definition)
	}
}

func nftSetDefinitions(snapshot nftFamilySnapshot) map[string]knftables.Set {
	family, table := snapshot.Family, nftSnapshotTable(snapshot)
	addressType := "ipv4_addr"
	if family == knftables.IPv6Family {
		addressType = "ipv6_addr"
	}
	interval := []knftables.SetFlag{knftables.IntervalFlag}
	definitions := map[string]knftables.Set{
		"cluster-ip-ports":       {Family: family, Table: table, Name: "cluster-ip-ports", Type: addressType + " . inet_proto . inet_service"},
		"service-vip-ports":      {Family: family, Table: table, Name: "service-vip-ports", Type: addressType + " . inet_proto . inet_service"},
		"subnets":                {Family: family, Table: table, Name: "subnets", Type: addressType, Flags: interval},
		"nat-subnets":            {Family: family, Table: table, Name: "nat-subnets", Type: addressType, Flags: interval},
		"distributed-gw-subnets": {Family: family, Table: table, Name: "distributed-gw-subnets", Type: addressType, Flags: interval},
		"other-node-ips":         {Family: family, Table: table, Name: "other-node-ips", Type: addressType},
		"node-ips":               {Family: family, Table: table, Name: "node-ips", Type: addressType},
		"local-nodeports":        {Family: family, Table: table, Name: "local-nodeports", Type: "inet_proto . inet_service"},
	}
	for _, policy := range snapshot.NATPolicies {
		id := nftNATPolicyID(policy)
		if len(policy.SrcIPs) != 0 {
			name := "nat-policy-" + id + "-src"
			definitions[name] = knftables.Set{Family: family, Table: table, Name: name, Type: addressType, Flags: interval}
		}
		if len(policy.DstIPs) != 0 {
			name := "nat-policy-" + id + "-dst"
			definitions[name] = knftables.Set{Family: family, Table: table, Name: name, Type: addressType, Flags: interval}
		}
	}
	return definitions
}

func renderNFTSetElements(tx *knftables.Transaction, snapshot nftFamilySnapshot) {
	family, table := snapshot.Family, nftSnapshotTable(snapshot)
	elementsBySet := nftSetElements(snapshot)
	for _, name := range sortedMapKeys(elementsBySet) {
		elements := elementsBySet[name]
		for _, key := range elements {
			addNFTSetElement(tx, family, table, name, key...)
		}
	}
}

func nftSetElements(snapshot nftFamilySnapshot) map[string][][]string {
	elements := map[string][][]string{
		"cluster-ip-ports":       {},
		"service-vip-ports":      {},
		"subnets":                {},
		"nat-subnets":            {},
		"distributed-gw-subnets": {},
		"other-node-ips":         {},
		"node-ips":               {},
		"local-nodeports":        {},
	}
	for _, item := range snapshot.ClusterIPPorts {
		elements["cluster-ip-ports"] = append(elements["cluster-ip-ports"], []string{item.Address, item.Protocol, strconv.FormatInt(int64(item.Port), 10)})
	}
	for _, item := range snapshot.ServiceVIPPorts {
		elements["service-vip-ports"] = append(elements["service-vip-ports"], []string{item.Address, item.Protocol, strconv.FormatInt(int64(item.Port), 10)})
	}
	for _, item := range snapshot.Subnets {
		elements["subnets"] = append(elements["subnets"], []string{item})
	}
	for _, item := range snapshot.NATSubnets {
		elements["nat-subnets"] = append(elements["nat-subnets"], []string{item})
	}
	for _, item := range snapshot.DistributedGWSubnets {
		elements["distributed-gw-subnets"] = append(elements["distributed-gw-subnets"], []string{item})
	}
	for _, item := range snapshot.OtherNodeIPs {
		elements["other-node-ips"] = append(elements["other-node-ips"], []string{item})
	}
	for _, item := range snapshot.NodeIPs {
		elements["node-ips"] = append(elements["node-ips"], []string{item})
	}
	for _, item := range snapshot.LocalNodePorts {
		elements["local-nodeports"] = append(elements["local-nodeports"], []string{item.Protocol, strconv.FormatInt(int64(item.Port), 10)})
	}
	for _, policy := range snapshot.NATPolicies {
		id := nftNATPolicyID(policy)
		for _, item := range policy.SrcIPs {
			name := "nat-policy-" + id + "-src"
			elements[name] = append(elements[name], []string{item})
		}
		for _, item := range policy.DstIPs {
			name := "nat-policy-" + id + "-dst"
			elements[name] = append(elements[name], []string{item})
		}
	}
	return elements
}

func renderNFTElementDiff(tx *knftables.Transaction, oldSnapshot, newSnapshot nftFamilySnapshot) {
	family, table := newSnapshot.Family, nftSnapshotTable(newSnapshot)
	oldElements := nftSetElements(oldSnapshot)
	newElements := nftSetElements(newSnapshot)
	for _, setName := range sortedMapKeys(newElements) {
		oldSet := nftElementMap(oldElements[setName])
		newSet := nftElementMap(newElements[setName])
		for _, key := range sortedMapKeys(oldSet) {
			if _, exists := newSet[key]; !exists {
				tx.Delete(&knftables.Element{Family: family, Table: table, Set: setName, Key: oldSet[key]})
			}
		}
		for _, key := range sortedMapKeys(newSet) {
			if _, exists := oldSet[key]; !exists {
				addNFTSetElement(tx, family, table, setName, newSet[key]...)
			}
		}
	}
}

func nftElementMap(elements [][]string) map[string][]string {
	result := make(map[string][]string, len(elements))
	for _, key := range elements {
		result[strings.Join(key, "\x00")] = key
	}
	return result
}

type nftNATPolicyShape struct {
	SubnetCIDR string
	RuleID     string
	Action     string
	HasSrcSet  bool
	HasDstSet  bool
}

func nftNATPolicyShapes(policies []nftNATPolicy) []nftNATPolicyShape {
	result := make([]nftNATPolicyShape, 0, len(policies))
	for _, policy := range policies {
		result = append(result, nftNATPolicyShape{
			SubnetCIDR: policy.SubnetCIDR,
			RuleID:     policy.RuleID,
			Action:     policy.Action,
			HasSrcSet:  len(policy.SrcIPs) != 0,
			HasDstSet:  len(policy.DstIPs) != 0,
		})
	}
	return result
}

func flushNFTChains(tx *knftables.Transaction, snapshot nftFamilySnapshot, chains ...string) {
	for _, chain := range chains {
		tx.Flush(&knftables.Chain{Family: snapshot.Family, Table: nftSnapshotTable(snapshot), Name: chain})
	}
}

func sortedMapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func cloneGatewayNFTSnapshot(snapshot gatewayNFTSnapshot) gatewayNFTSnapshot {
	clone := gatewayNFTSnapshot{Families: make([]nftFamilySnapshot, len(snapshot.Families))}
	for i, family := range snapshot.Families {
		clone.Families[i] = family
		clone.Families[i].ClusterIPPorts = slices.Clone(family.ClusterIPPorts)
		clone.Families[i].ServiceVIPPorts = slices.Clone(family.ServiceVIPPorts)
		clone.Families[i].Subnets = slices.Clone(family.Subnets)
		clone.Families[i].NATSubnets = slices.Clone(family.NATSubnets)
		clone.Families[i].DistributedGWSubnets = slices.Clone(family.DistributedGWSubnets)
		clone.Families[i].OtherNodeIPs = slices.Clone(family.OtherNodeIPs)
		clone.Families[i].NodeIPs = slices.Clone(family.NodeIPs)
		clone.Families[i].LocalNodePorts = slices.Clone(family.LocalNodePorts)
		clone.Families[i].NATPolicies = make([]nftNATPolicy, len(family.NATPolicies))
		for j, policy := range family.NATPolicies {
			clone.Families[i].NATPolicies[j] = policy
			clone.Families[i].NATPolicies[j].SrcIPs = slices.Clone(policy.SrcIPs)
			clone.Families[i].NATPolicies[j].DstIPs = slices.Clone(policy.DstIPs)
		}
		clone.Families[i].CentralizedSNATs = slices.Clone(family.CentralizedSNATs)
		clone.Families[i].TProxyTargets = slices.Clone(family.TProxyTargets)
		clone.Families[i].SubnetCounters = slices.Clone(family.SubnetCounters)
	}
	return clone
}

func (b *nftGatewayBackend) renderAuditRepair(ctx context.Context, desired gatewayNFTSnapshot) (*knftables.Transaction, error) {
	tx := b.writer.NewTransaction()
	for _, family := range desired.Families {
		reader := b.readers[family.Family]
		if reader == nil {
			b.renderFullFamily(tx, family)
			continue
		}

		objects, err := reader.ListAll(ctx)
		if err != nil {
			if knftables.IsNotFound(err) {
				b.renderFullFamily(tx, family)
				continue
			}
			return nil, fmt.Errorf("审计 %s nft table: %w", family.Family, err)
		}
		if !slices.Contains(objects["chain"], "schema-v1") {
			tx.Delete(&knftables.Table{Family: family.Family, Name: nftSnapshotTable(family)})
			b.renderFullFamily(tx, family)
			continue
		}

		definitions := nftSetDefinitions(family)
		for _, name := range sortedMapKeys(definitions) {
			if !slices.Contains(objects["set"], name) {
				definition := definitions[name]
				tx.Add(&definition)
				for _, key := range nftSetElements(family)[name] {
					addNFTSetElement(tx, family.Family, nftSnapshotTable(family), name, key...)
				}
				continue
			}
			actual, err := reader.ListElements(ctx, "set", name)
			if err != nil {
				return nil, fmt.Errorf("审计 %s nft set %s: %w", family.Family, name, err)
			}
			renderNFTElementMapDiff(tx, family, name, nftElementsToKeys(actual), nftSetElements(family)[name])
		}

		ruleDrift, err := nftRulesDrifted(ctx, reader, family, objects["chain"])
		if err != nil {
			return nil, err
		}
		if ruleDrift {
			b.renderNFTBaseSchema(tx, family)
			renderNFTSetDefinitions(tx, family)
			flushNFTChains(tx, family, nftRuleChainNames()...)
			b.renderNFTNATRules(tx, family)
			b.renderNFTPolicyRules(tx, family)
			b.renderNFTTProxyRules(tx, family)
			b.renderNFTFilterAndMangleRules(tx, family)
		}

		for _, name := range objects["set"] {
			if strings.HasPrefix(name, "nat-policy-") {
				if _, expected := definitions[name]; !expected {
					tx.Delete(&knftables.Set{Family: family.Family, Table: nftSnapshotTable(family), Name: name})
				}
			}
		}
		if !ruleDrift {
			for _, counter := range family.SubnetCounters {
				egress, ingress := nftSubnetCounterNames(counter)
				if !slices.Contains(objects["counter"], egress) {
					tx.Add(&knftables.Counter{Family: family.Family, Table: nftSnapshotTable(family), Name: egress, Comment: new(counter.Name + " egress")})
				}
				if !slices.Contains(objects["counter"], ingress) {
					tx.Add(&knftables.Counter{Family: family.Family, Table: nftSnapshotTable(family), Name: ingress, Comment: new(counter.Name + " ingress")})
				}
			}
		}
		expectedCounters := nftExpectedCounterNames(family)
		for _, name := range objects["counter"] {
			if strings.HasPrefix(name, "subnet-") && !slices.Contains(expectedCounters, name) {
				tx.Delete(&knftables.Counter{Family: family.Family, Table: nftSnapshotTable(family), Name: name})
			}
		}
	}
	return tx, nil
}

func nftRulesDrifted(ctx context.Context, reader knftables.Interface, snapshot nftFamilySnapshot, actualChains []string) (bool, error) {
	expected := nftExpectedRuleComments(snapshot)
	for _, chain := range nftRuleChainNames() {
		if !slices.Contains(actualChains, chain) {
			return true, nil
		}
		rules, err := reader.ListRules(ctx, chain)
		if err != nil {
			return false, fmt.Errorf("审计 %s nft chain %s: %w", snapshot.Family, chain, err)
		}
		comments := make([]string, 0, len(rules))
		for _, rule := range rules {
			if rule.Comment == nil {
				comments = append(comments, "")
			} else {
				comments = append(comments, *rule.Comment)
			}
		}
		if !reflect.DeepEqual(comments, expected[chain]) {
			return true, nil
		}
	}
	return false, nil
}

func nftRuleChainNames() []string {
	return []string{
		"nodeport-local",
		"nodeport-local-action",
		"nat-policy",
		"tproxy-prerouting",
		"tproxy-output",
		"filter-input",
		"filter-forward",
		"filter-output",
		"mangle-postrouting",
		"nat-host-service",
		"nat-postrouting",
	}
}

func nftExpectedRuleComments(snapshot nftFamilySnapshot) map[string][]string {
	comments := map[string][]string{
		"nodeport-local":        {"nodeport-vip-guard", "nodeport-local-match", "nodeport-local-return"},
		"nodeport-local-action": {"nodeport-other-node", "nodeport-distributed", "nodeport-centralized"},
		"nat-policy":            {},
		"tproxy-prerouting":     {},
		"tproxy-output":         {},
		"filter-input":          {"filter-input-source", "filter-input-destination"},
		"filter-forward":        {},
		"filter-output":         {"filter-output-tunnel-unmark"},
		"mangle-postrouting":    {"mangle-invalid-rst"},
		"nat-host-service":      {},
		"nat-postrouting": {
			"nat-service",
			"nat-subnet-between",
			"nat-nodeport",
			"nat-direct-routing",
			"nat-route-traffic",
			"nat-policy",
		},
	}
	if snapshot.NodeInternalIP != "" {
		comments["nat-host-service"] = append(comments["nat-host-service"], "host-service-snat")
	}
	for _, item := range snapshot.CentralizedSNATs {
		comments["nat-postrouting"] = append(comments["nat-postrouting"], "nat-centralized-snat:"+item.CIDR)
	}
	comments["nat-postrouting"] = append(comments["nat-postrouting"], "nat-default")

	for _, policy := range snapshot.NATPolicies {
		switch strings.ToLower(policy.Action) {
		case "nat", "forward":
			comments["nat-policy"] = append(comments["nat-policy"], "nat-policy:"+nftNATPolicyID(policy))
		}
	}
	comments["nat-policy"] = append(comments["nat-policy"], "nat-policy:return")

	if snapshot.NodeInternalIP != "" {
		for _, target := range snapshot.TProxyTargets {
			suffix := target.Address + ":" + strconv.FormatInt(int64(target.Port), 10)
			comments["tproxy-output"] = append(comments["tproxy-output"], "tproxy-output:"+suffix)
			comments["tproxy-prerouting"] = append(comments["tproxy-prerouting"], "tproxy-prerouting:"+suffix)
		}
	}

	for _, counter := range snapshot.SubnetCounters {
		egress, ingress := nftSubnetCounterNames(counter)
		comments["filter-forward"] = append(comments["filter-forward"], "counter:"+egress)
		comments["filter-forward"] = append(comments["filter-forward"], "counter:"+ingress)
	}
	comments["filter-forward"] = append(comments["filter-forward"], "filter-forward-source", "filter-forward-destination")
	for _, item := range snapshot.CentralizedSNATs {
		comments["mangle-postrouting"] = append(comments["mangle-postrouting"], "mangle-centralized-orphan:"+item.CIDR)
	}
	return comments
}

func nftExpectedCounterNames(snapshot nftFamilySnapshot) []string {
	names := make([]string, 0, len(snapshot.SubnetCounters)*2)
	for _, counter := range snapshot.SubnetCounters {
		egress, ingress := nftSubnetCounterNames(counter)
		names = append(names, egress, ingress)
	}
	slices.Sort(names)
	return names
}

func nftElementsToKeys(elements []*knftables.Element) [][]string {
	result := make([][]string, 0, len(elements))
	for _, element := range elements {
		result = append(result, slices.Clone(element.Key))
	}
	return result
}

func renderNFTElementMapDiff(tx *knftables.Transaction, family nftFamilySnapshot, setName string, oldElements, newElements [][]string) {
	oldSet := nftElementMap(oldElements)
	newSet := nftElementMap(newElements)
	table := nftSnapshotTable(family)
	for _, key := range sortedMapKeys(oldSet) {
		if _, exists := newSet[key]; !exists {
			tx.Delete(&knftables.Element{Family: family.Family, Table: table, Set: setName, Key: oldSet[key]})
		}
	}
	for _, key := range sortedMapKeys(newSet) {
		if _, exists := oldSet[key]; !exists {
			addNFTSetElement(tx, family.Family, table, setName, newSet[key]...)
		}
	}
}

func nftSnapshotTable(snapshot nftFamilySnapshot) string {
	if snapshot.Table == "" {
		return nftGatewayTable
	}
	return snapshot.Table
}
