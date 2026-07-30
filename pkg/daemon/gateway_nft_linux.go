package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
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
	mutex            sync.Mutex
	controller       *Controller
	writer           knftables.Interface
	readers          map[knftables.Family]knftables.Interface
	buildSnapshot    func() (gatewayNFTSnapshot, error)
	checkDefinitions func(context.Context, nftFamilySnapshot) (bool, error)
	applied          *gatewayNFTSnapshot
	lastAudit        time.Time
	auditInterval    time.Duration
	counterValues    map[string]nftCounterValue
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

type nftChainDefinition struct {
	Type     string
	Hook     string
	Priority *int
	Policy   string
	Device   string
}

type nftSetDefinition struct {
	Type  string
	Flags []string
}

type nftTableDefinitions struct {
	Chains map[string]nftChainDefinition
	Sets   map[string]nftSetDefinition
}

func newNFTGatewayBackend(controller *Controller) (*nftGatewayBackend, error) {
	writer, err := knftables.New("", "", knftables.EmulateDestroy)
	if err != nil {
		return nil, fmt.Errorf("initialize nftables writer: %w", err)
	}
	readers := make(map[knftables.Family]knftables.Interface, 2)
	for _, family := range []knftables.Family{knftables.IPv4Family, knftables.IPv6Family} {
		reader, err := knftables.New(family, nftGatewayTable)
		if err != nil {
			return nil, fmt.Errorf("initialize %s nftables reader: %w", family, err)
		}
		readers[family] = reader
	}
	backend := &nftGatewayBackend{
		controller:       controller,
		writer:           writer,
		readers:          readers,
		checkDefinitions: checkNFTDefinitions,
		auditInterval:    5 * time.Minute,
		counterValues:    make(map[string]nftCounterValue),
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
	if err := b.runTransaction(ctx, tx, false); err != nil {
		return fmt.Errorf("clean up Kube-OVN nftables table: %w", err)
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
		tx            *knftables.Transaction
		audited       bool
		detectedDrift bool
	)
	switch {
	case len(b.readers) != 0 && (b.applied == nil || b.auditDue()):
		tx, detectedDrift, err = b.renderAuditRepair(ctx, desired)
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
		if err := b.runTransaction(ctx, tx, initial); err != nil {
			if knftables.IsNotFound(err) {
				b.applied = nil
			}
			return fmt.Errorf("execute nftables transaction: %w", err)
		}
		if detectedDrift && !initial {
			metricGatewayNFTRepairs.Inc()
		}
	}

	applied := cloneGatewayNFTSnapshot(desired)
	b.applied = &applied
	if audited {
		b.lastAudit = time.Now()
	}
	return nil
}

func (b *nftGatewayBackend) runTransaction(ctx context.Context, tx *knftables.Transaction, check bool) error {
	if check {
		if err := b.writer.Check(ctx, tx); err != nil {
			metricGatewayNFTTransactionFailures.Inc()
			return fmt.Errorf("validate nftables transaction: %w", err)
		}
	}
	metricGatewayNFTTransactions.Inc()
	start := time.Now()
	err := b.writer.Run(ctx, tx)
	metricGatewayNFTTransactionDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		metricGatewayNFTTransactionFailures.Inc()
	}
	return err
}

func (b *nftGatewayBackend) ReadSubnetCounters(ctx context.Context) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if b.applied == nil || b.controller == nil {
		return nil
	}

	metadata := nftCounterMetadataByName(*b.applied)
	for key := range b.counterValues {
		if _, ok := metadata[key]; !ok {
			delete(b.counterValues, key)
		}
	}
	for family, reader := range b.readers {
		counters, err := reader.ListCounters(ctx)
		if err != nil {
			if knftables.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("read %s nftables counter: %w", family, err)
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
		return gatewayNFTSnapshot{}, fmt.Errorf("list Services: %w", err)
	}
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		return gatewayNFTSnapshot{}, fmt.Errorf("list Subnets: %w", err)
	}
	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		return gatewayNFTSnapshot{}, fmt.Errorf("list Nodes: %w", err)
	}

	var tproxyPods []*corev1.Pod
	if c.config.EnableTProxy {
		pods, err := c.podsLister.List(labels.Everything())
		if err != nil {
			return gatewayNFTSnapshot{}, fmt.Errorf("list Pods: %w", err)
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
		return nil, fmt.Errorf("list local network interfaces: %w", err)
	}
	var result []string
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			return nil, fmt.Errorf("list addresses on interface %s: %w", iface.Name, err)
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
	b.renderNFTFilterAndMangleRules(tx, family, true)
}

func (*nftGatewayBackend) renderNFTBaseSchema(tx *knftables.Transaction, snapshot nftFamilySnapshot) {
	family, table := snapshot.Family, snapshot.Table
	if table == "" {
		table = nftGatewayTable
	}
	tx.Add(&knftables.Table{Family: family, Name: table})
	renderNFTChains(tx, family, table)
}

func renderNFTChains(tx *knftables.Transaction, family knftables.Family, table string) {
	definitions := nftChainDefinitions(family, table)
	for _, name := range sortedMapKeys(definitions) {
		chain := definitions[name]
		tx.Add(&chain)
	}
}

func nftChainDefinitions(family knftables.Family, table string) map[string]knftables.Chain {
	definitions := map[string]knftables.Chain{}
	for _, name := range []string{"schema-v1", "nodeport-local", "nodeport-local-action", "nat-policy"} {
		definitions[name] = knftables.Chain{Family: family, Table: table, Name: name}
	}
	for _, chain := range []knftables.Chain{
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
	} {
		chain.Family = family
		chain.Table = table
		definitions[chain.Name] = chain
	}
	return definitions
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

func (*nftGatewayBackend) renderNFTFilterAndMangleRules(tx *knftables.Transaction, snapshot nftFamilySnapshot, includeCounterObjects bool) {
	family, table := snapshot.Family, nftSnapshotTable(snapshot)
	ipToken := nftIPToken(family)
	for _, counter := range snapshot.SubnetCounters {
		egress, ingress := nftSubnetCounterNames(counter)
		if includeCounterObjects {
			tx.Add(&knftables.Counter{Family: family, Table: table, Name: egress, Comment: new(counter.Name + " egress")})
			tx.Add(&knftables.Counter{Family: family, Table: table, Name: ingress, Comment: new(counter.Name + " ingress")})
		}
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
			b.renderFullFamily(tx, newFamily)
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
			b.renderNFTFilterAndMangleRules(tx, newFamily, true)
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
	oldElements := nftSetElements(oldSnapshot)
	newElements := nftSetElements(newSnapshot)
	for _, setName := range sortedMapKeys(newElements) {
		renderNFTElementMapDiff(tx, newSnapshot, setName, oldElements[setName], newElements[setName])
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

func checkNFTDefinitions(ctx context.Context, snapshot nftFamilySnapshot) (bool, error) {
	actual, err := readNFTDefinitions(ctx, snapshot.Family, nftSnapshotTable(snapshot))
	if err != nil {
		return false, err
	}
	expected, err := expectedNFTDefinitions(snapshot)
	if err != nil {
		return false, err
	}
	return reflect.DeepEqual(actual, expected), nil
}

func readNFTDefinitions(ctx context.Context, family knftables.Family, table string) (nftTableDefinitions, error) {
	// #nosec G204 -- family and table are generated by Kube-OVN.
	output, err := exec.CommandContext(ctx, "nft", "--json", "list", "table", string(family), table).CombinedOutput()
	if err != nil {
		return nftTableDefinitions{}, fmt.Errorf("list table definitions: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return parseNFTDefinitions(output)
}

func parseNFTDefinitions(data []byte) (nftTableDefinitions, error) {
	var result struct {
		NFTables []map[string]json.RawMessage `json:"nftables"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nftTableDefinitions{}, fmt.Errorf("decode nftables definitions: %w", err)
	}
	definitions := nftTableDefinitions{
		Chains: make(map[string]nftChainDefinition),
		Sets:   make(map[string]nftSetDefinition),
	}
	for _, object := range result.NFTables {
		if raw := object["chain"]; raw != nil {
			var chain struct {
				Name     string `json:"name"`
				Type     string `json:"type"`
				Hook     string `json:"hook"`
				Priority *int   `json:"prio"`
				Policy   string `json:"policy"`
				Device   string `json:"dev"`
			}
			if err := json.Unmarshal(raw, &chain); err != nil {
				return nftTableDefinitions{}, fmt.Errorf("decode nftables chain: %w", err)
			}
			definitions.Chains[chain.Name] = nftChainDefinition{
				Type: chain.Type, Hook: chain.Hook, Priority: chain.Priority, Policy: chain.Policy, Device: chain.Device,
			}
		}
		if raw := object["set"]; raw != nil {
			var set struct {
				Name  string          `json:"name"`
				Type  json.RawMessage `json:"type"`
				Flags []string        `json:"flags"`
			}
			if err := json.Unmarshal(raw, &set); err != nil {
				return nftTableDefinitions{}, fmt.Errorf("decode nftables set: %w", err)
			}
			setType, err := parseNFTSetType(set.Type)
			if err != nil {
				return nftTableDefinitions{}, fmt.Errorf("decode nftables set %s type: %w", set.Name, err)
			}
			slices.Sort(set.Flags)
			definitions.Sets[set.Name] = nftSetDefinition{Type: setType, Flags: set.Flags}
		}
	}
	return definitions, nil
}

func parseNFTSetType(data []byte) (string, error) {
	var scalar string
	if err := json.Unmarshal(data, &scalar); err == nil {
		return scalar, nil
	}
	var concatenated []string
	if err := json.Unmarshal(data, &concatenated); err != nil {
		return "", err
	}
	return strings.Join(concatenated, " . "), nil
}

func expectedNFTDefinitions(snapshot nftFamilySnapshot) (nftTableDefinitions, error) {
	definitions := nftTableDefinitions{
		Chains: make(map[string]nftChainDefinition),
		Sets:   make(map[string]nftSetDefinition),
	}
	for name, chain := range nftChainDefinitions(snapshot.Family, nftSnapshotTable(snapshot)) {
		definition := nftChainDefinition{}
		if chain.Type != nil {
			definition.Type = string(*chain.Type)
		}
		if chain.Hook != nil {
			definition.Hook = string(*chain.Hook)
		}
		if chain.Priority != nil {
			priority, err := knftables.ParsePriority(snapshot.Family, string(*chain.Priority))
			if err != nil {
				return nftTableDefinitions{}, fmt.Errorf("parse nftables chain %s priority: %w", name, err)
			}
			definition.Priority = &priority
		}
		if chain.Policy != nil {
			definition.Policy = string(*chain.Policy)
		} else if chain.Type != nil {
			definition.Policy = string(knftables.AcceptPolicy)
		}
		if chain.Device != nil {
			definition.Device = *chain.Device
		}
		definitions.Chains[name] = definition
	}
	for name, set := range nftSetDefinitions(snapshot) {
		var flags []string
		if len(set.Flags) != 0 {
			flags = make([]string, len(set.Flags))
			for i := range set.Flags {
				flags[i] = string(set.Flags[i])
			}
			slices.Sort(flags)
		}
		definitions.Sets[name] = nftSetDefinition{Type: set.Type, Flags: flags}
	}
	return definitions, nil
}

func (b *nftGatewayBackend) renderAuditRepair(ctx context.Context, desired gatewayNFTSnapshot) (*knftables.Transaction, bool, error) {
	tx := b.writer.NewTransaction()
	detectedDrift := false
	for _, family := range desired.Families {
		reader := b.readers[family.Family]
		if reader == nil {
			b.renderFullFamily(tx, family)
			detectedDrift = true
			continue
		}

		objects, err := reader.ListAll(ctx)
		if err != nil {
			if knftables.IsNotFound(err) {
				b.renderFullFamily(tx, family)
				detectedDrift = true
				continue
			}
			return nil, false, fmt.Errorf("audit %s nftables table: %w", family.Family, err)
		}

		definitions := nftSetDefinitions(family)
		expectedChains := append([]string{"schema-v1"}, nftRuleChainNames()...)
		expectedSets := sortedMapKeys(definitions)
		expectedCounters := nftExpectedCounterNames(family)
		table := nftSnapshotTable(family)
		if b.checkDefinitions != nil {
			match, err := b.checkDefinitions(ctx, family)
			if err != nil {
				return nil, false, fmt.Errorf("audit %s nftables definitions: %w", family.Family, err)
			}
			if !match {
				detectedDrift = true
			}
		}
		if !sameStringSet(objects["chain"], expectedChains) ||
			!sameStringSet(objects["set"], expectedSets) ||
			len(objects["map"]) != 0 ||
			len(objects["flowtable"]) != 0 ||
			!sameStringSet(objects["counter"], expectedCounters) {
			detectedDrift = true
		}
		expectedElements := nftSetElements(family)
		for _, name := range expectedSets {
			if !slices.Contains(objects["set"], name) {
				continue
			}
			actual, err := reader.ListElements(ctx, "set", name)
			if err != nil {
				return nil, false, fmt.Errorf("audit %s nftables set %s: %w", family.Family, name, err)
			}
			if !reflect.DeepEqual(nftElementMap(nftElementsToKeys(actual)), nftElementMap(expectedElements[name])) {
				detectedDrift = true
			}
		}

		for _, name := range objects["chain"] {
			tx.Flush(&knftables.Chain{Family: family.Family, Table: table, Name: name})
			tx.Delete(&knftables.Chain{Family: family.Family, Table: table, Name: name})
		}
		for _, name := range objects["set"] {
			tx.Delete(&knftables.Set{Family: family.Family, Table: table, Name: name})
		}
		for _, name := range objects["map"] {
			tx.Delete(&knftables.Map{Family: family.Family, Table: table, Name: name})
		}
		for _, name := range objects["flowtable"] {
			tx.Delete(&knftables.Flowtable{Family: family.Family, Table: table, Name: name})
		}

		renderNFTChains(tx, family.Family, table)
		b.renderNFTSets(tx, family)
		for _, counter := range family.SubnetCounters {
			egress, ingress := nftSubnetCounterNames(counter)
			if !slices.Contains(objects["counter"], egress) {
				tx.Add(&knftables.Counter{Family: family.Family, Table: table, Name: egress, Comment: new(counter.Name + " egress")})
			}
			if !slices.Contains(objects["counter"], ingress) {
				tx.Add(&knftables.Counter{Family: family.Family, Table: table, Name: ingress, Comment: new(counter.Name + " ingress")})
			}
		}
		b.renderNFTNATRules(tx, family)
		b.renderNFTPolicyRules(tx, family)
		b.renderNFTTProxyRules(tx, family)
		b.renderNFTFilterAndMangleRules(tx, family, false)
		for _, name := range objects["counter"] {
			if !slices.Contains(expectedCounters, name) {
				tx.Delete(&knftables.Counter{Family: family.Family, Table: table, Name: name})
			}
		}
	}
	return tx, detectedDrift, nil
}

func sameStringSet(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for _, item := range expected {
		if !slices.Contains(actual, item) {
			return false
		}
	}
	return true
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
