package controller

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	"k8s.io/utils/set"
)

// nft share DNAT architecture (follows kube-proxy nftables pattern):
//
//   Table: kube-ovn
//   ├── Base chain: prerouting (type nat hook prerouting priority -150)
//   │   └── Rule: ip daddr . meta l4proto . th dport vmap @service-ips
//   ├── Named vmap: service-ips
//   │   └── Elements: { eip . protocol . port : goto dnat-XXXXX }  (element-level add/delete)
//   └── Per-identity chains: dnat-XXXXX
//       └── Rule: numgen random mod N map { 0 : ip1 . port1, 1 : ip2 . port2, ... }
//
// Operations:
//   - Add/remove backend: flush per-identity chain + re-add rule (atomic batch via nft -f)
//   - Add new identity: create chain + add rule + add vmap element
//   - Remove identity: delete vmap element + flush & delete chain
//
// Coexistence with exclusive (iptables) DNAT:
//   Two DNAT paths run in the same gateway pod. Exclusive-type rules use iptables
//   nat/PREROUTING (priority NF_IP_PRI_NAT_DST, ~ -100), while share-type rules use this nft
//   base chain at priority -150, so the nft hook runs first. This is not a functional conflict
//   because the webhook enforces that a given identity (eip + externalPort + protocol) is
//   mutually exclusive across types, and DNAT/FIP for the same EIP are also mutually exclusive;
//   a packet is therefore only ever matched by one path. The ordering matters mainly for
//   troubleshooting boundary cases, e.g. when a rule's type is switched after conntrack has
//   already pinned a connection (existing flows keep their old destination until they expire).
//   Recommendation: within a single gateway, prefer using one mode consistently (share/nft) so
//   there is a single DNAT path to reason about and debug.
//
// Naming conventions:
//   - Shell variables: UPPER_CASE (NFT_TABLE, NFT_SERVICES_MAP, ...)
//   - nft object names: lower_case with hyphens/underscores (Linux/nftables convention)
//   - Per-identity chain: "dnat-" + md5(eip:port:protocol)[:12]  (generated in shell)
//
// Client-IP session affinity: when a share DNAT's Spec.SessionAffinity is "ClientIP", the
// gateway script programs the kube-proxy nftables affinity pattern instead of the stateless
// numgen-random map: each backend gets a dynamic timeout set ("update @affinity-set { ip saddr }"
// in a per-endpoint chain) and the per-identity chain gains a preceding "ip saddr @affinity-set
// goto <ep>" lookup, layered on top of the same numgen random dispatch (not jhash). Affinity is
// an identity-level property (all siblings share it) and is threaded through createNftDnatMapInPod
// as (sessionAffinity, affinityTimeoutSeconds); "" / none keeps the original stateless behavior.

const (
	// natGwNftDnatMapAdd is the shell command for adding/updating a share DNAT identity.
	natGwNftDnatMapAdd = "nft-dnat-map-add"

	// natGwNftDnatMapDel is the shell command for deleting a share DNAT identity.
	natGwNftDnatMapDel = "nft-dnat-map-del"
)

// createNftDnatMapInPods atomically creates or updates one share DNAT identity on the
// gateway instances already resolved by the Service controller.
func (c *Controller) createNftDnatMapInPods(gwPods []*corev1.Pod, protocol, v4ip, externalPort string, backends []string, sessionAffinity string, affinityTimeoutSeconds int32) error {
	if v4ip == "" {
		// Share DNAT is implemented with `ip daddr`/`ip saddr` nft rules and only supports IPv4.
		return errors.New("cannot create nft dnat map: empty IPv4 EIP (share dnat does not support IPv6)")
	}
	// Normalize the backend set: dedup and sort so that an unchanged set of backends always
	// produces an identical nft map. Without this, the lister's non-deterministic order would
	// rewrite the numgen random map on every rebuild/redo even when nothing changed, causing
	// gratuitous nft rule churn (and reshuffling index->backend, which only affects the random
	// choice for new connections; established connections stay pinned by conntrack).
	backends = dedupSortedBackends(backends)
	if len(backends) == 0 {
		return fmt.Errorf("cannot create nft dnat map for %s:%s (%s): no backends", v4ip, externalPort, protocol)
	}

	// Encode client-IP session affinity for the gateway script. "none" keeps the original
	// stateless numgen-random map; "clientip" enables per-backend affinity sets with the given
	// sticky timeout (kube-proxy nftables pattern). The timeout is only meaningful for clientip.
	affinity := "none"
	timeout := int32(0)
	if sessionAffinity == kubeovnv1.DnatSessionAffinityClientIP {
		affinity = "clientip"
		timeout = affinityTimeoutSeconds
		if timeout <= 0 {
			timeout = kubeovnv1.DefaultDnatSessionAffinityTimeoutSeconds
		}
	}

	backendStr := strings.Join(backends, "@")
	rule := fmt.Sprintf("%s,%s,%s,%s,%d,%s", v4ip, externalPort, protocol, affinity, timeout, backendStr)
	return c.execNatGwRulesInPods(gwPods, natGwNftDnatMapAdd, []string{rule})
}

// deleteNftDnatMapInPods deletes an nftables map-based DNAT rule by identity, on an already resolved
// set of gateway instances: it removes the vmap element and the per-identity chain atomically (see
// createNftDnatMapInPods). The caller resolves the gateway and its Pods once, so removing several
// identities of one gateway costs one live list instead of one per identity (and an empty result
// even sleeps before failing, so the saving is not only round trips).
func (c *Controller) deleteNftDnatMapInPods(gwPods []*corev1.Pod, protocol, v4ip, externalPort string) error {
	rule := fmt.Sprintf("%s,%s,%s", v4ip, externalPort, protocol)
	return c.execNatGwRulesInPods(gwPods, natGwNftDnatMapDel, []string{rule})
}

// dnatUsesEip reports whether a rule addresses a public IP through an EIP: the address is
// allocated by the EIP and bound on the external interface.
func dnatUsesEip(spec *kubeovnv1.IptablesDnatRuleSpec) bool {
	return spec.EIP != ""
}

// dnatServesClusterIP reports whether a rule also serves an internal VIP: the address is held on lo
// in the gateway. A Service handled by the nftable LB service feature carries both addresses on one
// rule (its ingress IP through the EIP and its ClusterIP), a ClusterIP Service only the ClusterIP.
func dnatServesClusterIP(spec *kubeovnv1.IptablesDnatRuleSpec) bool {
	return spec.ClusterIP != ""
}

// sameDnatIdentity reports whether two rules address the same share DNAT identity, i.e. the same
// address, port and protocol.
//
// An EIP is the identity whenever either side has one: the nft map of an EIP:port is shared by
// every rule of that EIP, so a rule that only differs by not carrying the ClusterIP of the Service
// still programs the same map and must be treated as the same identity. Only rules that have no EIP
// at all are identified by their ClusterIP.
func sameDnatIdentity(a, b *kubeovnv1.IptablesDnatRuleSpec) bool {
	if a.EIP != "" || b.EIP != "" {
		return a.EIP == b.EIP
	}
	return a.ClusterIP == b.ClusterIP
}

// dnatIdentityName names the identity for logs and errors.
func dnatIdentityName(spec *kubeovnv1.IptablesDnatRuleSpec) string {
	if dnatUsesEip(spec) {
		return "eip " + spec.EIP
	}
	return "clusterIP " + spec.ClusterIP
}

// dedupSortedBackends returns the unique backends in sorted order, dropping empty entries.
func dedupSortedBackends(backends []string) []string {
	if len(backends) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(backends))
	uniq := make([]string, 0, len(backends))
	for _, b := range backends {
		if b == "" {
			continue
		}
		if _, ok := seen[b]; ok {
			continue
		}
		seen[b] = struct{}{}
		uniq = append(uniq, b)
	}
	if len(uniq) == 0 {
		return nil
	}
	sort.Strings(uniq)
	return uniq
}

// gwNftableLbSvcEnabled reports whether this feature is enabled. It gates everything the feature
// owns beyond the share DNAT identity itself: the per-identity hairpin SNAT rules, the addresses
// held on lo, and the VIP routes.
func (c *Controller) gwNftableLbSvcEnabled() bool {
	return c.config != nil && c.config.EnableGwNftableLbSvc
}

// natGwVipRouteExternalIDs identifies the VPC policy routes this feature owns for one gateway.
// The routes are keyed by gateway so the desired set of one gateway can never be claimed by
// another, and the priority keeps them apart from the NAT gateway's internal CIDR policies.
func natGwVipRouteExternalIDs(gwName string) map[string]string {
	return map[string]string{
		ovs.ExternalIDVendor:        util.CniTypeName,
		ovs.ExternalIDVpcNatGateway: gwName,
		"vipRoute":                  "true",
	}
}

// natGwVipRouteMatch is the policy route match of one VIP: VPC traffic destined for the VIP is
// rerouted to the gateway, which is what makes a Service reachable from inside the VPC through
// its EIP and its ClusterIP alike. The match is on the destination (unlike the NAT gateway's
// outbound policies, which match the client range) because the VIP is a destination.
func natGwVipRouteMatch(vip string) string {
	return "ip4.dst == " + vip
}

// desiredNatGwVipState returns the VIPs the given gateway has to route, keyed by policy route
// match, and the ClusterIPs among them, which it also has to hold on lo.
//
// Every share rule contributes the addresses it serves, whether the feature generated it or an
// operator created it by hand: the rule describes what it forwards, so it is the same rule shape,
// the same identity and the same state in both cases. There is no per-rule bookkeeping and no
// owner label involved -- a rule that is gone simply stops contributing, and the state converges
// to what the live rules ask for.
//
// applying is the rule whose reconcile is running, or nil. It overrides what the informer cache
// says about that one rule, because the caller knows better and the cache can be behind: the DNAT
// handler patches the gateway label (patchDnatLabel) and then programs the rule in the same pass,
// so the first rule of a Service is regularly still invisible to this List, and a terminating rule
// can still look live. Without the override the gateway would be left with the nft identity but no
// route and no lo address, and nothing bounds when a later event would repair it.
func (c *Controller) desiredNatGwVipState(gwName string, applying *kubeovnv1.IptablesDnatRule) (map[string]string, []string, error) {
	return c.desiredNatGwVipStateForService(gwName, applying, "", nil, "")
}

// desiredNatGwVipStateForService computes the complete gateway VIP state while replacing one
// Service's cached accounting records with its current desired records. The Service controller
// uses this immediately after programming the gateway, before the informer can observe record
// creates/deletes; other Services and exclusive DNAT state remain cache-derived.
func (c *Controller) desiredNatGwVipStateForService(gwName string, applying *kubeovnv1.IptablesDnatRule,
	owner string, desired map[string]*kubeovnv1.IptablesDnatRule, eipIP string,
) (map[string]string, []string, error) {
	rules, err := c.iptablesDnatRulesLister.List(labels.SelectorFromSet(labels.Set{util.VpcNatGatewayNameLabel: gwName}))
	if err != nil {
		return nil, nil, err
	}
	if applying != nil {
		// The override carries its own DeletionTimestamp, so one substitution covers both
		// directions: the loop below keeps a live rule and skips a terminating one.
		rules = append(slices.DeleteFunc(rules, func(rule *kubeovnv1.IptablesDnatRule) bool {
			return rule.Name == applying.Name
		}), applying)
	}
	if owner != "" {
		rules = slices.DeleteFunc(rules, func(rule *kubeovnv1.IptablesDnatRule) bool {
			return util.NftableLbSvcOwnerKey(rule.Labels) == owner
		})
	}

	vips := make(map[string]string)
	clusterIPs := set.New[string]()
	eipIPs := make(map[string]string)
	for _, rule := range rules {
		// Only Service accounting records contribute share VIP state. A manually created share CR
		// has no forwarding semantics even if admission is bypassed.
		if !rule.DeletionTimestamp.IsZero() || rule.Spec.Type != kubeovnv1.DnatRuleTypeShare ||
			!util.IsNftableLbSvcRecord(rule.Labels) {
			continue
		}
		// The internal VIP of a rule is held on lo and routed to the gateway. A rule that only
		// serves a ClusterIP is self-describing, so it needs no owner label to contribute it.
		if clusterIP := rule.Spec.ClusterIP; util.CheckProtocol(clusterIP) == kubeovnv1.ProtocolIPv4 {
			vips[natGwVipRouteMatch(clusterIP)] = clusterIP
			clusterIPs.Insert(clusterIP)
		}
		// The public VIP of a rule is the address of the EIP it serves. Resolving it here (instead
		// of at creation) keeps the two addresses of one rule in one place and lets a rule that has
		// no internal VIP, like a hand-managed one, contribute its public address alone.
		if !dnatUsesEip(&rule.Spec) {
			continue
		}
		ip, resolved := eipIPs[rule.Spec.EIP]
		if !resolved {
			// The lister is read directly: an EIP that exists but has no address yet contributes no
			// route (its own update re-triggers this sync), while a lister failure must not silently
			// narrow the desired set and delete a route that is still wanted.
			eip, err := c.iptablesEipsLister.Get(rule.Spec.EIP)
			switch {
			case err == nil:
				ip = eip.Status.IP
			case k8serrors.IsNotFound(err):
				klog.Infof("nat gw %s: eip %s of dnat %s is gone, dropping its vip route", gwName, rule.Spec.EIP, rule.Name)
			default:
				return nil, nil, fmt.Errorf("nat gw %s: failed to resolve eip %s of dnat %s for its vip route: %w", gwName, rule.Spec.EIP, rule.Name, err)
			}
			eipIPs[rule.Spec.EIP] = ip
		}
		if util.CheckProtocol(ip) == kubeovnv1.ProtocolIPv4 {
			vips[natGwVipRouteMatch(ip)] = ip
		}
	}
	for _, rule := range desired {
		if clusterIP := rule.Spec.ClusterIP; util.CheckProtocol(clusterIP) == kubeovnv1.ProtocolIPv4 {
			vips[natGwVipRouteMatch(clusterIP)] = clusterIP
			clusterIPs.Insert(clusterIP)
		}
		if rule.Spec.EIP != "" && util.CheckProtocol(eipIP) == kubeovnv1.ProtocolIPv4 {
			vips[natGwVipRouteMatch(eipIP)] = eipIP
		}
	}
	return vips, clusterIPs.SortedList(), nil
}

// natGwVipRouteNextHops returns the sorted LAN addresses of the gateway instances that can serve
// the VIPs, which are the next hops of its VIP routes.
//
// An instance that still needs initialization is excluded: the vpc-nat-gw container has no
// readiness probe, so the kubelet reports it ready as soon as it runs, well before the controller
// has programmed its chains and share DNAT identities. Routing to it would drop the connections
// ECMP hashes there. This narrows the window rather than closing it: the annotation only proves
// that initialization finished, not that the DNAT redo has re-applied every identity.
func natGwVipRouteNextHops(gw *kubeovnv1.VpcNatGateway, pods []*corev1.Pod) ([]string, error) {
	configured := make([]*corev1.Pod, 0, len(pods))
	for _, pod := range pods {
		if natGwPodPendingInit(pod) {
			klog.V(3).Infof("nat gw %s: instance %s/%s is not initialized yet, not a vip route next hop", gw.Name, pod.Namespace, pod.Name)
			continue
		}
		configured = append(configured, pod)
	}
	nextHopByNode, err := getNatGwNextHops(gw, configured)
	if err != nil {
		return nil, err
	}
	nextHops := make([]string, 0, len(nextHopByNode))
	for _, ip := range nextHopByNode {
		// Share DNAT is IPv4 only: an IPv6 next hop must not be programmed into an ip4.dst route.
		if util.CheckProtocol(ip) != kubeovnv1.ProtocolIPv4 {
			continue
		}
		nextHops = append(nextHops, ip)
	}
	sort.Strings(nextHops)
	return nextHops, nil
}

// syncNatGwVipState reconciles the VPC policy routes that send traffic destined for a share DNAT
// VIP to the gateway, so a Service handled by the nftable LB service feature is reachable from
// inside the VPC through both its EIP and its ClusterIP, not only through the external network.
// The ClusterIPs the instances hold on lo are the in-pod half of the same state; they are synced
// from the paths that already exec into the gateway (see syncNatGwVipAddrs).
//
// The desired set is derived from the gateway's DNAT rules, which are the durable record: a
// deleted rule stops contributing its VIP, and the route is removed once no rule references it.
// Reconciling by set (rather than by reference counting each teardown) is what makes the state
// converge: a route that was missed once is removed by the next sync of its gateway.
func (c *Controller) syncNatGwVipState(gwName string, applying *kubeovnv1.IptablesDnatRule) error {
	return c.syncNatGwVipStateForService(gwName, applying, "", nil, "")
}

func (c *Controller) syncNatGwVipStateForService(gwName string, applying *kubeovnv1.IptablesDnatRule,
	owner string, records map[string]*kubeovnv1.IptablesDnatRule, eipIP string,
) error {
	if !c.gwNftableLbSvcEnabled() {
		// The feature owns the VIP state exclusively, and with it disabled it programs nothing: the
		// rules it generated are not reconciled either.
		return nil
	}
	gw, err := c.vpcNatGatewayLister.Get(gwName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// The gateway is gone and its VPC routes went with it.
			return nil
		}
		return err
	}

	desired, _, err := c.desiredNatGwVipStateForService(gwName, applying, owner, records, eipIP)
	if err != nil {
		return err
	}
	pods, err := c.listNatGwPods(gw)
	if err != nil {
		return err
	}
	nextHops, err := natGwVipRouteNextHops(gw, pods)
	if err != nil {
		return err
	}
	if len(nextHops) == 0 {
		// A reroute policy without next hops is rejected by OVN, so the routes have to go while no
		// instance is reachable. The routes come back with the first ready instance, which the
		// gateway reconcile syncs.
		desired = map[string]string{}
	}

	existing, err := c.OVNNbClient.ListLogicalRouterPolicies(gw.Spec.Vpc, util.NatGatewayVipPolicyPriority, natGwVipRouteExternalIDs(gwName), false)
	if err != nil {
		return fmt.Errorf("failed to list vip routes of nat gw %s: %w", gwName, err)
	}
	for _, policy := range existing {
		if _, wanted := desired[policy.Match]; !wanted {
			if err = c.OVNNbClient.DeleteLogicalRouterPolicyByUUID(gw.Spec.Vpc, policy.UUID); err != nil {
				return fmt.Errorf("failed to delete vip route %s of nat gw %s: %w", policy.Match, gwName, err)
			}
			klog.Infof("nat gw %s: removed vip route %s", gwName, policy.Match)
			continue
		}
		current := append([]string(nil), policy.Nexthops...)
		sort.Strings(current)
		if policy.Action != string(kubeovnv1.PolicyRouteActionReroute) || !slices.Equal(current, nextHops) {
			policy.Action, policy.Nexthops, policy.BFDSessions = string(kubeovnv1.PolicyRouteActionReroute), nextHops, nil
			if err = c.OVNNbClient.UpdateLogicalRouterPolicy(policy, &policy.Action, &policy.Nexthops, &policy.BFDSessions); err != nil {
				return fmt.Errorf("failed to update vip route %s of nat gw %s: %w", policy.Match, gwName, err)
			}
			klog.Infof("nat gw %s: restored vip route %s action reroute and nexthops %v", gwName, policy.Match, nextHops)
		}
		delete(desired, policy.Match)
	}

	for match := range desired {
		if err = c.addPolicyRouteToVpc(gw.Spec.Vpc, &kubeovnv1.PolicyRoute{
			Priority:  util.NatGatewayVipPolicyPriority,
			Match:     match,
			Action:    kubeovnv1.PolicyRouteActionReroute,
			NextHopIP: strings.Join(nextHops, ","),
		}, natGwVipRouteExternalIDs(gwName)); err != nil {
			return fmt.Errorf("failed to add vip route %s to nat gw %s: %w", match, gwName, err)
		}
		klog.Infof("nat gw %s: routed vip %s to %v", gwName, desired[match], nextHops)
	}
	return nil
}

// sameDnatVip reports whether two exclusive rules use the same VIP. Share records are owned and
// programmed by the Service controller and never reach this duplicate check.
func sameDnatVip(a, b *kubeovnv1.IptablesDnatRuleSpec) bool {
	if a.EIP != "" && a.EIP == b.EIP {
		return true
	}
	return a.ClusterIP != "" && a.ClusterIP == b.ClusterIP
}

func (c *Controller) isDnatDuplicated(gwName string, rule *kubeovnv1.IptablesDnatRule) (bool, error) {
	spec := &rule.Spec
	dnatName := rule.Name
	// Check if the tuple "vip:external port:protocol" is already used by another DNAT rule.
	// VIP remains a Spec post-filter because the gateway and external-port labels are the only
	// indexed parts of the exclusive DNAT identity.
	dnats, err := c.iptablesDnatRulesLister.List(labels.SelectorFromSet(labels.Set{
		util.VpcNatGatewayNameLabel: gwName,
		util.VpcDnatEPortLabel:      spec.ExternalPort,
	}))
	if err != nil {
		return false, err
	}
	if len(dnats) == 0 {
		return false, nil
	}

	canonicalExternalPort := canonicalDnatPort(spec.ExternalPort)
	canonicalProtocol := strings.ToLower(spec.Protocol)
	for _, d := range dnats {
		if d.Name == dnatName || !sameDnatVip(&d.Spec, spec) || strings.ToLower(d.Spec.Protocol) != canonicalProtocol || canonicalDnatPort(d.Spec.ExternalPort) != canonicalExternalPort {
			continue
		}
		err = fmt.Errorf("failed to create dnat %s, duplicate, same %s, same external port '%s', same protocol '%s' is used by dnat %s (type=%s)",
			dnatName, dnatIdentityName(spec), spec.ExternalPort, spec.Protocol, d.Name, d.Spec.Type)
		return true, err
	}
	return false, nil
}

func canonicalDnatPort(port string) string {
	value, err := strconv.Atoi(port)
	if err != nil {
		return port
	}
	return strconv.Itoa(value)
}
