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

// createNftDnatMapInPod creates or updates an nftables map-based DNAT rule for Share type.
// This is the core function for the nft LB feature: it builds an nft transaction that
// atomically updates the per-identity chain with the full set of backends.
//
// The identity is programmed on every running gateway instance, like the exclusive iptables
// rules already are (see createEipInPod): an HA gateway load balances its VIP across instances,
// so each of them has to be able to DNAT the traffic it receives.
//
// The transaction (submitted via nft -f) contains:
//  1. Ensure table, base chain, and vmap exist
//  2. Flush the per-identity chain (remove old rule)
//  3. Add new rule with numgen random mod N map { all backends }
//  4. Ensure vmap element dispatches to this chain
//
// The backends format passed to the gateway script is "ip1:port1@ip2:port2@...".
// '@' is used as the separator (not ';') because the rule string is passed as a single
// argument through the pod-exec API into a shell context, where ';' would be interpreted
// as a command separator; '@' never appears in an ip:port and is shell-safe.
func (c *Controller) createNftDnatMapInPod(dp, protocol, v4ip, externalPort string, backends []string, sessionAffinity string, affinityTimeoutSeconds int32) error {
	gwPods, err := c.getNatGwPods(dp, c.natGwNamespaceByName(dp), false)
	if err != nil {
		klog.Errorf("failed to get nat gw pods, %v", err)
		return err
	}
	return c.createNftDnatMapInPods(gwPods, protocol, v4ip, externalPort, backends, sessionAffinity, affinityTimeoutSeconds)
}

// createNftDnatMapInPods is createNftDnatMapInPod on an already resolved set of gateway instances,
// so a caller that programs several identities of one gateway lists its Pods once.
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

// getShareBackends queries all Share-type DNAT rules with the same identity (eip, externalPort, protocol)
// and returns their backends. The current DNAT (identified by dnatName) is excluded from the results.
//
// Backends are derived from each sibling's Spec (not Status) on purpose: the nft map for a given
// identity is global and is rebuilt in full by whichever sibling reconciles last. Relying on
// Status.Ready would create a race across the add/update/delete queues, where a sibling that is
// momentarily not-yet-Ready gets excluded and silently dropped from the map by the last writer.
// A sibling's Spec backend is populated as soon as it exists, so using it makes the rebuild
// order-independent. Siblings that are being deleted are skipped so their backend is not re-added.
//
// This relies on the informer cache being in sync: a sibling's Spec must already be visible in
// the lister for its backend to be included. If a sibling was just created and its create event
// has not yet propagated to this lister cache, the last writer will build a map that temporarily
// omits that backend. This is not a bug: the missing sibling's own add/update event triggers a
// later reconcile that rebuilds the full map, so the set self-heals to the complete backend list.
// getShareBackends returns the live backend list for a share DNAT identity (excluding dnatName)
// together with the session-affinity settings carried by those live rules. The affinity is read
// from a remaining rule instead of the rule being deleted: during a takeover or an affinity
// change the old rule may still be terminating while newer live rules already carry the new
// affinity, and the nft map must always be rebuilt with the live rules' settings.
func (c *Controller) getShareBackends(gwName string, rule *kubeovnv1.IptablesDnatRule, externalPort, protocol string) ([]string, string, int32, error) {
	return c.shareBackends(gwName, rule.Name, externalPort, protocol, func(d *kubeovnv1.IptablesDnatRule) bool {
		return sameDnatIdentity(&d.Spec, &rule.Spec) && sameShareOwner(d, rule)
	})
}

// sameShareOwner reports whether two rules belong to the same owner: every rule the nftable LB
// service feature generates for a Service carries that Service's owner key, a hand-managed rule
// carries none. Rules of different owners must never share an nft map, because that map aggregates
// the backends of everything matching it (the webhook rejects such a pair; this is the last line of
// defence for the window where admission ran against a cache that had not seen the other rule yet).
func sameShareOwner(a, b *kubeovnv1.IptablesDnatRule) bool {
	return nftableLbSvcOwnerKey(a) == nftableLbSvcOwnerKey(b)
}

// shareClusterIPBackends returns the backends of the live share rules of the gateway that serve the
// given internal VIP, excluding the rule dnatName. The ClusterIP identity is not the EIP identity,
// so its siblings are not the siblings of the EIP: a hand-managed rule may carry an internal VIP
// that no other rule serves, and the empty result is what tells the caller to drop the address
// instead of rebuilding it with somebody else's backends.
func (c *Controller) shareClusterIPBackends(gwName string, rule *kubeovnv1.IptablesDnatRule, externalPort, protocol, clusterIP string) ([]string, string, int32, error) {
	return c.shareBackends(gwName, rule.Name, externalPort, protocol, func(d *kubeovnv1.IptablesDnatRule) bool {
		return d.Spec.ClusterIP == clusterIP && sameShareOwner(d, rule)
	})
}

func (c *Controller) shareBackends(gwName, dnatName, externalPort, protocol string, sameIdentity func(*kubeovnv1.IptablesDnatRule) bool) ([]string, string, int32, error) {
	// The label selector only coarse-filters by gateway name; the VIP, port and protocol
	// identity is intentionally enforced as a Spec post-filter below (sameDnatIdentity)
	// rather than added to the selector:
	//   - EIP name cannot be a label value: IptablesEIP is a cluster-scoped CR whose name may be
	//     up to 253 chars, exceeding the 63-char Kubernetes label-value limit, which would make
	//     patchDnatLabel fail and stall reconcile. The authoritative identity is therefore matched
	//     by name in the Spec post-filter, which has no length limit. A ClusterIP rule has an
	//     address as its identity instead, which has no such limit either.
	//   - EIP IP (EipV4IpLabel) could technically be added to the selector, but it gives no real
	//     benefit. The informer registers only a namespace indexer (no label index), so
	//     lister.List(selector) always does cache.ListAll: a full O(all-DNATs) scan that applies
	//     selector.Matches per object. Adding EipV4IpLabel does not shrink that scan; it only moves
	//     the EIP comparison from the post-filter loop into the per-object selector match during the
	//     same full scan, so total work is unchanged (arguably a hair more). It would also couple
	//     every call site (including the redo path, which only has cachedDnat.Status.V4ip and no eip
	//     object) to the Spec.V4ip == Status.IP backfill invariant. We keep EIP as a single-source
	//     Spec post-filter. A genuine speedup would require a dedicated label indexer, which is
	//     over-engineering for this small per-(gw,eport) set.
	// gwName is a safe selector dimension: it is always populated, immutable (NatGwDp is
	// webhook-immutable), and short.
	// gwName is explicitly length-validated by the VpcNatGateway webhook via
	// ValidateNatGwStatefulSetNameLength (<=52 chars, derived from the 63-char label-value limit
	// minus the StatefulSet revision-hash suffix), so it always fits in a label value; IptablesEIP
	// has no such name-length webhook, which is the real reason its name cannot be used as a label.
	dnats, err := c.iptablesDnatRulesLister.List(labels.SelectorFromSet(labels.Set{
		util.VpcNatGatewayNameLabel: gwName,
	}))
	if err != nil {
		return nil, "", 0, err
	}

	var backends []string
	var affinity string
	var affinityTimeout int32
	affinitySet := false
	canonicalExternalPort := canonicalDnatPort(externalPort)
	canonicalProtocol := strings.ToLower(protocol)
	for _, d := range dnats {
		if d.Name == dnatName {
			continue
		}
		if !sameIdentity(d) || strings.ToLower(d.Spec.Protocol) != canonicalProtocol || canonicalDnatPort(d.Spec.ExternalPort) != canonicalExternalPort {
			continue
		}
		if d.Spec.Type != kubeovnv1.DnatRuleTypeShare {
			continue
		}
		if !d.DeletionTimestamp.IsZero() {
			klog.V(4).Infof("skipping share dnat %s: being deleted", d.Name)
			continue
		}
		if d.Spec.InternalIP == "" || d.Spec.InternalPort == "" {
			klog.V(4).Infof("skipping share dnat %s: incomplete spec", d.Name)
			continue
		}
		backends = append(backends, fmt.Sprintf("%s:%s", d.Spec.InternalIP, d.Spec.InternalPort))
		if !affinitySet {
			affinity = d.Spec.SessionAffinity
			affinityTimeout = d.Spec.SessionAffinityTimeoutSeconds
			affinitySet = true
		}
	}
	return backends, affinity, affinityTimeout, nil
}

// applyShareDnatIdentity programs the share DNAT identities of one rule on every running gateway
// instance: the address it is programmed with (the EIP's public address), and — for a rule that
// carries an internal VIP — that ClusterIP with the same backends. Each identity gets the hairpin
// SNAT rule it needs.
//
// The ClusterIP shares the other identity's backends because both belong to one Service port: the
// siblings getShareBackends aggregates are exactly that Service's backends. A rule that serves only
// a ClusterIP is programmed with it, so it has a single identity.
//
// The ClusterIP is also held on lo by the gateway; that part of the state is shared by every
// identity of the Service, so it is reconciled as a set (syncNatGwVipAddrs) rather than per
// identity: an address no live rule asks for any more is released by the same call.
func (c *Controller) applyShareDnatIdentity(rule *kubeovnv1.IptablesDnatRule, gwName, protocol, v4ip, externalPort string, backends []string) error {
	gwPods, err := c.getNatGwPods(gwName, c.natGwNamespaceByName(gwName), false)
	if err != nil {
		klog.Errorf("failed to get nat gw pods, %v", err)
		return err
	}
	// One writer per identity (dnatIdentityWinner): a rule that shares an identity with a rule of
	// another owner -- which admission would have refused, so only a race produces it -- leaves it to
	// the winner instead of overwriting its map. The hairpin rule and the address set below are
	// per-address and idempotent, so they are applied either way.
	primaryEip, primaryClusterIP := rule.Spec.EIP, ""
	if primaryEip == "" {
		primaryClusterIP = v4ip
	}
	wonPrimary, err := c.dnatIdentityWinnerIs(gwName, rule, protocol, externalPort, primaryEip, primaryClusterIP)
	if err != nil {
		return err
	}
	if wonPrimary {
		if err = c.createNftDnatMapInPods(gwPods, protocol, v4ip, externalPort, backends, rule.Spec.SessionAffinity, rule.Spec.SessionAffinityTimeoutSeconds); err != nil {
			return err
		}
	} else {
		klog.Warningf("dnat %s: identity %s is programmed by another rule, leaving it alone", rule.Name, dnatIdentityName(&rule.Spec))
	}
	clusterIP := rule.Spec.ClusterIP
	// A rule that serves only a ClusterIP has already programmed it as its own address, so the
	// internal identity is not programmed twice. The hairpin rule and the address set below are
	// needed either way.
	if clusterIP != "" && clusterIP != v4ip {
		wonClusterIP, err := c.dnatIdentityWinnerIs(gwName, rule, protocol, externalPort, "", clusterIP)
		if err != nil {
			return err
		}
		if !wonClusterIP {
			klog.Warningf("dnat %s: clusterIP %s is programmed by another rule, leaving it alone", rule.Name, clusterIP)
			return c.applyShareDnatVipState(gwPods, rule, gwName, protocol, v4ip, externalPort, clusterIP)
		}
		clusterIPBackends, affinity, timeout, err := c.clusterIPIdentityBackends(gwName, rule, protocol, externalPort, clusterIP)
		if err != nil {
			return err
		}
		if err = c.createNftDnatMapInPods(gwPods, protocol, clusterIP, externalPort, clusterIPBackends, affinity, timeout); err != nil {
			return err
		}
	}
	return c.applyShareDnatVipState(gwPods, rule, gwName, protocol, v4ip, externalPort, clusterIP)
}

// clusterIPIdentityBackends returns the backends of the internal VIP of one rule: the live rules
// serving that same address, plus the rule itself. It is computed by address and not through the
// EIP identity's siblings, because those are not the same set: two hand-managed rules can share an
// EIP:port while each serves its own internal VIP, and reusing the EIP aggregation would repoint one
// address at the other's backends (the teardown decides the address the same way, see
// shareClusterIPBackends).
func (c *Controller) clusterIPIdentityBackends(gwName string, rule *kubeovnv1.IptablesDnatRule, protocol, externalPort, clusterIP string) ([]string, string, int32, error) {
	siblings, siblingAffinity, siblingTimeout, err := c.shareClusterIPBackends(gwName, rule, externalPort, protocol, clusterIP)
	if err != nil {
		return nil, "", 0, err
	}
	backends := append(siblings, fmt.Sprintf("%s:%s", rule.Spec.InternalIP, rule.Spec.InternalPort))
	// A surviving sibling is authoritative for the settings of the shared identity, exactly like the
	// teardown treats it; the rule's own settings are the fallback when it is the only one.
	affinity, timeout := rule.Spec.SessionAffinity, rule.Spec.SessionAffinityTimeoutSeconds
	if len(siblings) != 0 {
		affinity, timeout = siblingAffinity, siblingTimeout
	}
	return backends, affinity, timeout, nil
}

// applyShareDnatVipState programs the VIP state of one identity: the hairpin SNAT rule and the
// addresses the gateway holds on lo. It is not the share DNAT data plane itself (the identity is
// programmed by the caller either way, because hand-managed rules use it too), so it is only
// programmed while the feature is enabled: with --enable-gw-nftable-lb-svc off the gateway leaves
// the VIP state alone, and "off" means the implementation is inactive rather than half programmed.
// The delete path is gated the same way, so nothing is ever left behind by the pair.
//
// gwName is the gateway the caller resolved for this rule, not what the rule spells out: an EIP
// rule may leave Spec.VpcNatGwDp empty (its gateway comes from the EIP), and the address set is the
// set of that gateway, so using the spec field here would look up a gateway the rule does not name
// and release the addresses of the real one.
func (c *Controller) applyShareDnatVipState(gwPods []*corev1.Pod, rule *kubeovnv1.IptablesDnatRule, gwName, protocol, v4ip, externalPort, clusterIP string) error {
	if !c.gwNftableLbSvcEnabled() {
		return nil
	}
	// VPC-originated traffic that gets DNAT'd back into the VPC is SNAT'd to the gateway's own VPC
	// address, so the backend's reply returns to the instance holding the conntrack. The rules are
	// per identity, so no two reconciles of one gateway share state here.
	if err := c.execNatGwRulesInPods(gwPods, natGwVipHairpinAdd, shareDnatHairpinRules(protocol, externalPort, v4ip, clusterIP)); err != nil {
		return err
	}
	return c.syncNatGwVipAddrs(gwPods, gwName, rule)
}

// handOverShareIdentity rebuilds an identity with the backends of the owner that holds it now, when
// the rule being torn down was the last one of the previous owner. Nothing is done when the identity
// is this owner's own (the rebuild path handles that) or when no rule is left (the release path
// releases it).
func (c *Controller) handOverShareIdentity(key string, rule *kubeovnv1.IptablesDnatRule, gwName, protocol, v4ip, externalPort string) error {
	primaryEip, primaryClusterIP := rule.Spec.EIP, ""
	if primaryEip == "" {
		primaryClusterIP = v4ip
	}
	if err := c.handOverDnatIdentity(rule, gwName, protocol, v4ip, externalPort, primaryEip, primaryClusterIP, false); err != nil {
		return fmt.Errorf("failed to hand over %s for dnat %s: %w", dnatIdentityName(&rule.Spec), key, err)
	}
	clusterIP := rule.Spec.ClusterIP
	if clusterIP == "" || clusterIP == v4ip {
		return nil
	}
	if err := c.handOverDnatIdentity(rule, gwName, protocol, clusterIP, externalPort, "", clusterIP, true); err != nil {
		return fmt.Errorf("failed to hand over clusterIP %s for dnat %s: %w", clusterIP, key, err)
	}
	return nil
}

func (c *Controller) handOverDnatIdentity(rule *kubeovnv1.IptablesDnatRule, gwName, protocol, v4ip, externalPort, eip, clusterIP string, isClusterIP bool) error {
	rules, err := c.shareIdentityRules(gwName, protocol, externalPort, eip, clusterIP, nil)
	if err != nil {
		return err
	}
	winner := dnatIdentityWinnerRule(rules)
	if winner == nil || sameShareOwner(winner, rule) {
		return nil
	}
	identity := clusterIP
	if !isClusterIP {
		identity = dnatIdentityName(&winner.Spec)
	}
	backends, affinity, timeout, err := c.handOverBackends(gwName, winner, externalPort, protocol, clusterIP)
	if err != nil || len(backends) == 0 {
		return err
	}
	klog.Infof("dnat %s: %s is handed over to %s", rule.Name, identity, winner.Name)
	return c.createNftDnatMapInPod(gwName, protocol, v4ip, externalPort, backends, affinity, timeout)
}

// handOverBackends returns every backend of the owner that takes an identity over: all of its live
// rules on that identity, its own included. getShareBackends describes a rule that is going away and
// leaves that rule out, which would drop a backend from the map being handed over.
func (c *Controller) handOverBackends(gwName string, winner *kubeovnv1.IptablesDnatRule, externalPort, protocol, clusterIP string) ([]string, string, int32, error) {
	if clusterIP != "" {
		return c.shareBackends(gwName, "", externalPort, protocol, func(d *kubeovnv1.IptablesDnatRule) bool {
			return d.Spec.ClusterIP == clusterIP && sameShareOwner(d, winner)
		})
	}
	return c.shareBackends(gwName, "", externalPort, protocol, func(d *kubeovnv1.IptablesDnatRule) bool {
		return sameDnatIdentity(&d.Spec, &winner.Spec) && sameShareOwner(d, winner)
	})
}

// deleteShareDnatIdentity removes the share DNAT identities of one rule from every running gateway
// instance: the address it was programmed with, and its internal VIP with the hairpin rules of both
// when it has one. The ClusterIP on lo is released by the set sync once no live rule records it any
// more.
//
// The address set is released by syncNatGwVipAddrs, which (like the hairpin rules) only touches the
// gateway while the feature is enabled: with the feature off a hand-managed rule programs and
// releases its own nft identity, and the VIP state it may have left behind is not reconciled.
func (c *Controller) deleteShareDnatIdentity(rule *kubeovnv1.IptablesDnatRule, gwName, protocol, v4ip, externalPort string) error {
	deleted, err := c.natGwDeleted(gwName)
	if err != nil || deleted {
		// The gateway (and its Pod) is gone, so its data plane went with it.
		return err
	}
	gwPods, err := c.getNatGwPods(gwName, c.natGwNamespaceByName(gwName), false)
	if err != nil {
		klog.Errorf("failed to get nat gw pods, %v", err)
		return err
	}
	// The primary identity is the EIP one for an EIP rule and the internal VIP one otherwise (see
	// resolveDnatAddress); it is released only while no rule of another owner programs it.
	primaryEip, primaryClusterIP := rule.Spec.EIP, ""
	if primaryEip == "" {
		primaryClusterIP = v4ip
	}
	released := map[string]bool{}
	release, err := c.releaseShareDnatIdentity(gwPods, rule, gwName, protocol, v4ip, externalPort, primaryEip, primaryClusterIP)
	if err != nil {
		return err
	}
	released[v4ip] = release
	clusterIP := rule.Spec.ClusterIP
	// See applyShareDnatIdentity: a rule that serves only a ClusterIP has already removed it as
	// its own address, but its hairpin rule and the address set still have to be reconciled.
	if clusterIP != "" && clusterIP != v4ip {
		release, err = c.releaseShareDnatIdentity(gwPods, rule, gwName, protocol, clusterIP, externalPort, "", clusterIP)
		if err != nil {
			return err
		}
		released[clusterIP] = release
	}
	// Everything below is the VIP state the feature owns, so a disabled feature stops here: the
	// identities above have already been removed.
	if !c.gwNftableLbSvcEnabled() {
		return nil
	}
	if hairpins := releasedHairpinRules(protocol, externalPort, released); len(hairpins) > 0 {
		if err = c.execNatGwRulesInPods(gwPods, natGwVipHairpinDel, hairpins); err != nil {
			return err
		}
	}
	return c.syncNatGwVipAddrs(gwPods, gwName, rule)
}

// syncNatGwVipAddrs makes the given instances hold exactly the ClusterIPs the gateway's live rules
// ask for. The gateway script reconciles the whole set in one call, so an address whose Service is
// gone is released by the next identity change of that gateway, whatever the teardown path did or
// missed. It runs on the paths that already exec into the gateway, so it costs no extra round trip
// beyond the one the old per-identity add/del did.
//
// The gateway lock covers the whole read-modify-write: the DNAT handlers lock by rule name, and
// add and update run in separate workers, so two rules of one gateway reconcile concurrently. Each
// would compute the full set from its own lister snapshot, and the call that lands last wins — a
// teardown overtaking a concurrent apply would drop an address that no later event re-adds, since
// the route sync no longer touches lo. The lock is only held around this list and exec, and the
// order (dnat rule -> gateway VIP -> gateway pod exec) is the one syncNatGwVipState follows too.
func (c *Controller) syncNatGwVipAddrs(gwPods []*corev1.Pod, gwName string, applying *kubeovnv1.IptablesDnatRule) error {
	if !c.gwNftableLbSvcEnabled() {
		return nil
	}
	c.natGwVipKeyMutex.LockKey(gwName)
	defer func() { _ = c.natGwVipKeyMutex.UnlockKey(gwName) }()

	_, clusterIPs, err := c.desiredNatGwVipState(gwName, applying)
	if err != nil {
		return err
	}
	return c.execNatGwRulesInPods(gwPods, natGwVipAddrSync, clusterIPs)
}

// gwNftableLbSvcEnabled reports whether this feature is enabled. It gates everything the feature
// owns beyond the share DNAT identity itself: the per-identity hairpin SNAT rules, the addresses
// held on lo, and the VIP routes.
func (c *Controller) gwNftableLbSvcEnabled() bool {
	return c.config != nil && c.config.EnableGwNftableLbSvc
}

// shareDnatHairpinRules returns the hairpin SNAT rule arguments of the VIPs of one Service port.
// The gateway script SNATs them to its own VPC address; add_eip's VIP-wide hairpin rule SNATs to the
// EIP instead, and the per-identity rules here override it for the identities this feature owns (see
// the TODO in nat-gateway.sh about unifying both on the gateway's address).
// A rule that serves only a ClusterIP is programmed with that same address, so it would otherwise
// contribute its identity twice (v4ip == clusterIP). The guard is the same one the identity
// programming uses, so the two cannot disagree about whether there is a second identity.
func shareDnatHairpinRules(protocol, externalPort, v4ip, clusterIP string) []string {
	rules := make([]string, 0, 2)
	if v4ip != "" {
		rules = append(rules, fmt.Sprintf("%s,%s,%s", v4ip, externalPort, protocol))
	}
	if clusterIP != "" && clusterIP != v4ip {
		rules = append(rules, fmt.Sprintf("%s,%s,%s", clusterIP, externalPort, protocol))
	}
	return rules
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

	vips := make(map[string]string)
	clusterIPs := set.New[string]()
	eipIPs := make(map[string]string)
	for _, rule := range rules {
		// A terminating rule is on its way out: it must not keep a route alive.
		if !rule.DeletionTimestamp.IsZero() || rule.Spec.Type != kubeovnv1.DnatRuleTypeShare {
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
	if !c.gwNftableLbSvcEnabled() {
		// The feature owns the VIP state exclusively, and with it disabled it programs nothing: the
		// rules it generated are not reconciled either.
		return nil
	}
	// Serialize per gateway: a concurrent sync could otherwise delete a route that a rule made
	// visible in the meantime (see natGwVipKeyMutex).
	c.natGwVipKeyMutex.LockKey(gwName)
	defer func() { _ = c.natGwVipKeyMutex.UnlockKey(gwName) }()

	gw, err := c.vpcNatGatewayLister.Get(gwName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// The gateway is gone and its VPC routes went with it.
			return nil
		}
		return err
	}

	desired, _, err := c.desiredNatGwVipState(gwName, applying)
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
		if !slices.Equal(current, nextHops) {
			policy.Nexthops, policy.BFDSessions = nextHops, nil
			if err = c.OVNNbClient.UpdateLogicalRouterPolicy(policy, &policy.Nexthops, &policy.BFDSessions); err != nil {
				return fmt.Errorf("failed to update vip route %s of nat gw %s: %w", policy.Match, gwName, err)
			}
			klog.Infof("nat gw %s: updated vip route %s nexthops %v", gwName, policy.Match, nextHops)
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

// syncNatGwVipStateForDnat reconciles the gateway's shared VIP state after a change to one of its
// share DNAT rules. Every share rule contributes to that state (see desiredNatGwVipState), so every
// one of them has to refresh it.
func (c *Controller) syncNatGwVipStateForDnat(rule *kubeovnv1.IptablesDnatRule, gwName string) error {
	if gwName == "" || rule.Spec.Type != kubeovnv1.DnatRuleTypeShare {
		return nil
	}
	if err := c.syncNatGwVipState(gwName, rule); err != nil {
		klog.Errorf("failed to sync vip state for nat gw %s: %v", gwName, err)
		return err
	}
	return nil
}

// cleanupShareDnatInPod rebuilds the share nft map with the remaining backends for the given
// identity, or deletes the rule entirely when no backend is left after excluding dnatName.
//
// When a single backend is removed while the identity still has other backends, this rebuilds
// the per-identity map in place without deleting the identity or flushing conntrack. Backends are
// balanced with numgen random, so established connections are pinned by conntrack: connections to
// surviving backends are unaffected, and connections to the removed backend are not flushed here
// (they simply break once that backend is gone). New connections are distributed by the rebuilt
// numgen random map. This is the expected behavior when detaching a backend from a load balancer.
// Conntrack is only cleared on full identity deletion (see deleteNftDnatMapInPods /
// del_nft_dnat_map in the gateway script).
func (c *Controller) cleanupShareDnatInPod(key string, rule *kubeovnv1.IptablesDnatRule, gwName, protocol, v4ip, externalPort string) error {
	remainingBackends, affinity, affinityTimeout, err := c.getShareBackends(gwName, rule, externalPort, protocol)
	if err != nil {
		return fmt.Errorf("failed to get share backends for dnat %s: %w", key, err)
	}
	if len(remainingBackends) == 0 {
		// This rule's owner has nothing left for the identity. Another owner may hold it (a race the
		// webhook would have refused), and that owner is not woken up by this teardown: program the
		// map with its backends here, instead of leaving it on the backends that just went away.
		if err := c.handOverShareIdentity(key, rule, gwName, protocol, v4ip, externalPort); err != nil {
			return err
		}
		// No remaining backends, delete the identity (and the internal VIP identity, if any)
		if err := c.deleteShareDnatIdentity(rule, gwName, protocol, v4ip, externalPort); err != nil {
			return fmt.Errorf("failed to delete nft dnat map for %s: %w", key, err)
		}
		return nil
	}
	// Rebuild both identities with the remaining backends. The affinity of the surviving siblings
	// is authoritative: the nft map of one identity is global and is rebuilt in full by whichever
	// sibling reconciles last, so every identity of a Service port has to carry the same settings.
	// A surviving sibling of this owner does not make the identity its own: another owner may share
	// it (a rule the webhook could not see when this one was admitted), and rebuilding it here would
	// overwrite that owner's map. Leave such an identity to its reconcile.
	primaryEip, primaryClusterIP := rule.Spec.EIP, ""
	if primaryEip == "" {
		primaryClusterIP = v4ip
	}
	wonPrimary, err := c.dnatIdentityWinnerIs(gwName, rule, protocol, externalPort, primaryEip, primaryClusterIP)
	if err != nil {
		return fmt.Errorf("failed to check the owner of %s for dnat %s: %w", dnatIdentityName(&rule.Spec), key, err)
	}
	servedByOther, err := c.identityStillServedByAnotherOwner(gwName, rule, protocol, externalPort, primaryEip, primaryClusterIP)
	if err != nil {
		return fmt.Errorf("failed to check the owners of %s for dnat %s: %w", dnatIdentityName(&rule.Spec), key, err)
	}
	if servedByOther || !wonPrimary {
		// Only this identity is left to its owner: the rule's other identity is independent and has
		// to keep converging below, otherwise its map would keep the backend that just went away.
		klog.Warningf("dnat %s: identity %s is still programmed by a rule of another owner, not rebuilding it", key, dnatIdentityName(&rule.Spec))
	} else if err = c.createNftDnatMapInPod(gwName, protocol, v4ip, externalPort, remainingBackends, affinity, affinityTimeout); err != nil {
		return fmt.Errorf("failed to rebuild nft dnat map for %s: %w", key, err)
	}
	clusterIP := rule.Spec.ClusterIP
	if clusterIP == "" || clusterIP == v4ip {
		return nil
	}
	// The internal VIP is decided on its own: the surviving siblings of the EIP identity above are
	// not necessarily serving this address (a hand-managed rule may be the only one that does), so
	// rebuilding it with their backends would silently repoint the address instead of dropping it.
	clusterIPBackends, clusterIPAffinity, clusterIPTimeout, err := c.shareClusterIPBackends(gwName, rule, externalPort, protocol, clusterIP)
	if err != nil {
		return fmt.Errorf("failed to get the backends of clusterIP %s for dnat %s: %w", clusterIP, key, err)
	}
	if len(clusterIPBackends) == 0 {
		// Another owner may still program this identity (a rule the webhook could not see when this
		// one was admitted). Released here it would take that owner's data plane with it, while its
		// rule remains: leave the identity to it, its own reconcile programs the map it wants.
		servedByOther, err := c.identityStillServedByAnotherOwner(gwName, rule, protocol, externalPort, "", clusterIP)
		if err != nil {
			return fmt.Errorf("failed to check the owners of clusterIP %s for dnat %s: %w", clusterIP, key, err)
		}
		if servedByOther {
			klog.Warningf("dnat %s: clusterIP %s is still served by a rule of another owner, leaving its nft identity alone", key, clusterIP)
			return nil
		}
		if err = c.deleteClusterIPDnatIdentity(gwName, protocol, clusterIP, externalPort); err != nil {
			return fmt.Errorf("failed to delete the nft dnat map of clusterIP %s for %s: %w", clusterIP, key, err)
		}
		return nil
	}
	if err = c.createNftDnatMapInPod(gwName, protocol, clusterIP, externalPort, clusterIPBackends, clusterIPAffinity, clusterIPTimeout); err != nil {
		return fmt.Errorf("failed to rebuild nft dnat map for %s: %w", key, err)
	}
	return nil
}

// identityStillServedByAnotherOwner reports whether a live rule of a different owner still programs
// the given identity, which is the EIP one when eip is set and the internal VIP one otherwise. Both
// identities of a rule need this check before they are released: two rules of different owners can
// share either of them (a rule the webhook could not see when the other was admitted), and releasing
// a map that the remaining rule still programs would take its data plane away while it exists.
func (c *Controller) identityStillServedByAnotherOwner(gwName string, rule *kubeovnv1.IptablesDnatRule, protocol, externalPort, eip, clusterIP string) (bool, error) {
	rules, err := c.shareIdentityRules(gwName, protocol, externalPort, eip, clusterIP, nil)
	if err != nil {
		return false, err
	}
	for _, d := range rules {
		if !sameShareOwner(d, rule) {
			return true, nil
		}
	}
	return false, nil
}

// shareIdentityRules lists the live share rules of the gateway that program the given identity: the
// EIP one when eip is set, the internal VIP one otherwise. They may belong to different owners, which
// is what the winner rule and the release guards have to arbitrate.
func (c *Controller) shareIdentityRules(gwName, protocol, externalPort, eip, clusterIP string, applying *kubeovnv1.IptablesDnatRule) ([]*kubeovnv1.IptablesDnatRule, error) {
	dnats, err := c.iptablesDnatRulesLister.List(labels.SelectorFromSet(labels.Set{
		util.VpcNatGatewayNameLabel: gwName,
	}))
	if err != nil {
		return nil, err
	}
	canonicalExternalPort := canonicalDnatPort(externalPort)
	canonicalProtocol := strings.ToLower(protocol)
	sameRule := func(d *kubeovnv1.IptablesDnatRule) bool {
		sameIdentity := (eip != "" && d.Spec.EIP == eip) || (eip == "" && clusterIP != "" && d.Spec.ClusterIP == clusterIP)
		return sameIdentity && canonicalDnatPort(d.Spec.ExternalPort) == canonicalExternalPort &&
			strings.ToLower(d.Spec.Protocol) == canonicalProtocol
	}
	rules := make([]*kubeovnv1.IptablesDnatRule, 0, len(dnats)+1)
	for _, d := range dnats {
		// The rule being reconciled replaces its cached copy: its gateway label is patched just before
		// this runs, so the informer may not have observed it yet (the same cache lag the VIP state
		// handles) and leaving it out would make it look ownerless.
		if d.Name == ruleName(applying) || !d.DeletionTimestamp.IsZero() || d.Spec.Type != kubeovnv1.DnatRuleTypeShare || !sameRule(d) {
			continue
		}
		rules = append(rules, d)
	}
	if applying != nil && applying.Spec.Type == kubeovnv1.DnatRuleTypeShare && sameRule(applying) {
		rules = append(rules, applying)
	}
	return rules, nil
}

func ruleName(rule *kubeovnv1.IptablesDnatRule) string {
	if rule == nil {
		return ""
	}
	return rule.Name
}

// dnatIdentityWinnerRule returns one rule of the owner that owns the identity, chosen the same
// deterministic way as the Service conflict resolver: the smallest owner key wins, and a hand-managed
// rule has no key so it wins over a Service. The smallest name breaks a tie, which makes the choice
// total and stable across reconciles and restarts -- a shared identity must never alternate owners.
//
// The winner is an owner, not one of its rules: every rule of an owner is a backend of the same
// Service port and any of them has to be able to rebuild the identity with all of that owner's
// backends, otherwise a backend added to a Service (a new rule, whose name may be larger) could never
// write the map while the rule that could gets no event. Returns nil when no rule is left.
func dnatIdentityWinnerRule(rules []*kubeovnv1.IptablesDnatRule) *kubeovnv1.IptablesDnatRule {
	var winner *kubeovnv1.IptablesDnatRule
	for _, rule := range rules {
		if winner == nil {
			winner = rule
			continue
		}
		winnerKey, key := nftableLbSvcOwnerKey(winner), nftableLbSvcOwnerKey(rule)
		if key < winnerKey || (key == winnerKey && rule.Name < winner.Name) {
			winner = rule
		}
	}
	return winner
}

// dnatIdentityWinnerIs reports whether the rule's owner is the one that writes the given identity.
// Only the winning owner programs, rebuilds and deletes the nft map and the hairpin rule of an
// identity; the rules of any other owner keep existing and keep reconciling, they just leave the
// identity to the winner. The rule being reconciled is always part of the candidates, so a rule the
// informer has not observed yet is never mistaken for a loser.
func (c *Controller) dnatIdentityWinnerIs(gwName string, rule *kubeovnv1.IptablesDnatRule, protocol, externalPort, eip, clusterIP string) (bool, error) {
	rules, err := c.shareIdentityRules(gwName, protocol, externalPort, eip, clusterIP, rule)
	if err != nil {
		return false, err
	}
	winner := dnatIdentityWinnerRule(rules)
	return winner != nil && nftableLbSvcOwnerKey(winner) == nftableLbSvcOwnerKey(rule), nil
}

// releaseShareDnatIdentity removes one identity from the gateway, unless a rule of another owner
// still programs it.
// releaseShareDnatIdentity removes one identity from the gateway, unless a rule of another owner
// still programs it. It reports whether the identity was actually released, because its hairpin rule
// has to go with it and only then: leaving a kept identity without its hairpin would break the return
// path of the rule that still uses it.
func (c *Controller) releaseShareDnatIdentity(gwPods []*corev1.Pod, rule *kubeovnv1.IptablesDnatRule, gwName, protocol, v4ip, externalPort, eip, clusterIP string) (bool, error) {
	servedByOther, err := c.identityStillServedByAnotherOwner(gwName, rule, protocol, externalPort, eip, clusterIP)
	if err != nil {
		return false, err
	}
	if servedByOther {
		klog.Warningf("dnat %s: identity %s is still programmed by a rule of another owner, leaving it alone",
			rule.Name, dnatIdentityName(&rule.Spec))
		return false, nil
	}
	if err = c.deleteNftDnatMapInPods(gwPods, protocol, v4ip, externalPort); err != nil {
		return false, err
	}
	return true, nil
}

// releasedHairpinRules returns the hairpin SNAT arguments to delete for a torn down rule: one per
// identity that was actually released. The two identities of a rule are independent, so a kept one
// must keep its hairpin (see releaseShareDnatIdentity).
func releasedHairpinRules(protocol, externalPort string, released map[string]bool) []string {
	rules := make([]string, 0, len(released))
	for vip, ok := range released {
		if !ok || vip == "" {
			continue
		}
		rules = append(rules, fmt.Sprintf("%s,%s,%s", vip, externalPort, protocol))
	}
	slices.Sort(rules)
	return rules
}

// deleteClusterIPDnatIdentity removes the nft identity of an internal VIP that no live rule serves
// any more, together with the hairpin SNAT rule of that identity: the address and its VPC route are
// released by the VIP state sync, but both rules would otherwise outlive the VIP they belong to.
func (c *Controller) deleteClusterIPDnatIdentity(gwName, protocol, clusterIP, externalPort string) error {
	deleted, err := c.natGwDeleted(gwName)
	if err != nil || deleted {
		// The gateway (and its Pod) is gone, so its data plane went with it.
		return err
	}
	gwPods, err := c.getNatGwPods(gwName, c.natGwNamespaceByName(gwName), false)
	if err != nil {
		klog.Errorf("failed to get nat gw pods, %v", err)
		return err
	}
	if err = c.deleteNftDnatMapInPods(gwPods, protocol, clusterIP, externalPort); err != nil {
		return err
	}
	// The hairpin rule belongs to the VIP state, which the feature owns: with the feature off a rule
	// programs and releases its identity alone (see deleteShareDnatIdentity).
	if !c.gwNftableLbSvcEnabled() {
		return nil
	}
	return c.execNatGwRulesInPods(gwPods, natGwVipHairpinDel, shareDnatHairpinRules(protocol, externalPort, "", clusterIP))
}

// isDnatDuplicated checks if a DNAT rule with the same identity already exists.
// For Share type rules, multiple rules with the same identity can coexist.
// For Exclusive type, only one rule per identity is allowed.
//
// Consistency note: this check (and the equivalent webhook check in ValidateIptablesDnat) reads
// the informer lister / controller-runtime cache, which is only eventually consistent. If two
// conflicting exclusive rules are created concurrently before either is visible in the cache,
// both can pass this check and be admitted. This is a pre-existing limitation of the cache-based
// duplicate detection, not specific to the share feature (the share feature only widens the set
// of legitimately-coexisting objects under one identity). The reconcile loop is the eventual
// authority: it re-runs this check on every sync, so a conflict that slipped through is detected
// on a subsequent reconcile and the offending rule fails to become Ready (last-writer-wins /
// eventual consistency). The webhook is best-effort admission-time protection, not a hard guarantee.
// dnatsShareIdentity reports whether two rules program the same nft map. A rule carrying both an EIP
// and a ClusterIP contributes two identities (one per address:port), so two rules share an identity
// whenever either of their addresses matches -- the same notion the webhook applies
// (identitiesOverlap) and that decides whether two rules may merge their backends.
func dnatsShareIdentity(a, b *kubeovnv1.IptablesDnatRuleSpec) bool {
	if a.EIP != "" && a.EIP == b.EIP {
		return true
	}
	return a.ClusterIP != "" && a.ClusterIP == b.ClusterIP
}

func (c *Controller) isDnatDuplicated(gwName string, rule *kubeovnv1.IptablesDnatRule) (bool, error) {
	spec := &rule.Spec
	dnatName := rule.Name
	// Check if the tuple "vip:external port:protocol" is already used by another DNAT rule.
	// The identity is enforced via the Spec post-filter (dnatsShareIdentity) below rather
	// than in the selector; see getShareBackends for why it cannot be used as a label.
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
		if d.Name == dnatName || !dnatsShareIdentity(&d.Spec, spec) || strings.ToLower(d.Spec.Protocol) != canonicalProtocol || canonicalDnatPort(d.Spec.ExternalPort) != canonicalExternalPort {
			continue
		}
		// Found a DNAT with same identity
		if spec.Type == kubeovnv1.DnatRuleTypeShare && d.Spec.Type == kubeovnv1.DnatRuleTypeShare {
			// Two share rules may hold one identity. They are the backends of one Service, several
			// hand-managed rules adding backends to one address, or -- when admission could not see
			// the other rule -- two owners that the webhook would have refused. Owners are separated
			// by the winner rule (dnatIdentityWinner) rather than by rejecting the second rule: a
			// rejected rule would never reconcile again, and after a redo neither side would have a
			// data plane. Only the winner writes the identity, the other leaves it alone.
			continue
		}
		// Type conflict: Exclusive vs Share, or Exclusive vs Exclusive
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
