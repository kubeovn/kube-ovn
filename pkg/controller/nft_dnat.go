package controller

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
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
// TODO(share-dnat): source-IP session affinity is not supported yet. Backends are selected
// purely at random (numgen random) and then pinned per-connection by conntrack; there is no
// client-IP affinity. kube-proxy's nftables backend implements ClientIP affinity by recording
// the source IP in a dynamic nftables set ("update @affinity-set { ip saddr }" in the per-endpoint
// chain, plus a preceding "ip saddr @affinity-set goto <ep>" lookup), layered on top of the same
// numgen random dispatch (not jhash). The same approach could be adopted here if per-client
// affinity is needed in the future.

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
func (c *Controller) createNftDnatMapInPod(dp, protocol, v4ip, externalPort string, backends []string) error {
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

	gwPod, err := c.getNatGwPod(dp, c.natGwNamespaceByName(dp))
	if err != nil {
		klog.Errorf("failed to get nat gw pod, %v", err)
		return err
	}

	backendStr := strings.Join(backends, "@")
	rule := fmt.Sprintf("%s,%s,%s,%s", v4ip, externalPort, protocol, backendStr)
	if err = c.execNatGwRules(gwPod, natGwNftDnatMapAdd, []string{rule}); err != nil {
		klog.Errorf("failed to create nft dnat map, err: %v", err)
		return err
	}
	return nil
}

// deleteNftDnatMapInPod deletes an nftables map-based DNAT rule by identity.
// It removes the vmap element and the per-identity chain atomically.
func (c *Controller) deleteNftDnatMapInPod(dp, protocol, v4ip, externalPort string) error {
	deleted, err := c.natGwDeleted(dp)
	if err != nil {
		klog.Error(err)
		return err
	}
	if deleted {
		return nil
	}
	gwPod, err := c.getNatGwPod(dp, c.natGwNamespaceByName(dp))
	if err != nil {
		klog.Errorf("failed to get nat gw pod, %v", err)
		return err
	}

	rule := fmt.Sprintf("%s,%s,%s", v4ip, externalPort, protocol)
	if err = c.execNatGwRules(gwPod, natGwNftDnatMapDel, []string{rule}); err != nil {
		klog.Errorf("failed to delete nft dnat map, err: %v", err)
		return err
	}
	return nil
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
func (c *Controller) getShareBackends(gwName, eipName, externalPort, protocol, dnatName string) ([]string, error) {
	// The label selector only coarse-filters by gateway name + external port; the EIP
	// identity is intentionally enforced as a Spec post-filter below (d.Spec.EIP != eipName)
	// rather than added to the selector:
	//   - EIP name cannot be a label value: IptablesEIP is a cluster-scoped CR whose name may be
	//     up to 253 chars, exceeding the 63-char Kubernetes label-value limit, which would make
	//     patchDnatLabel fail and stall reconcile. The authoritative identity is therefore matched
	//     by name in the Spec post-filter, which has no length limit.
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
	// gwName + externalPort are safe selector dimensions: both are always populated, immutable
	// (NatGwDp is webhook-immutable, externalPort comes straight from the DNAT Spec), and short.
	// gwName is explicitly length-validated by the VpcNatGateway webhook via
	// ValidateNatGwStatefulSetNameLength (<=52 chars, derived from the 63-char label-value limit
	// minus the StatefulSet revision-hash suffix), so it always fits in a label value; IptablesEIP
	// has no such name-length webhook, which is the real reason its name cannot be used as a label.
	dnats, err := c.iptablesDnatRulesLister.List(labels.SelectorFromSet(labels.Set{
		util.VpcNatGatewayNameLabel: gwName,
		util.VpcDnatEPortLabel:      externalPort,
	}))
	if err != nil {
		return nil, err
	}

	var backends []string
	for _, d := range dnats {
		if d.Name == dnatName {
			continue
		}
		if d.Spec.EIP != eipName || d.Spec.Protocol != protocol || d.Spec.ExternalPort != externalPort {
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
	}
	return backends, nil
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
// Conntrack is only cleared on full identity deletion (see deleteNftDnatMapInPod /
// del_nft_dnat_map in the gateway script).
func (c *Controller) cleanupShareDnatInPod(key, gwName, eipName, protocol, v4ip, externalPort, dnatName string) error {
	remainingBackends, err := c.getShareBackends(gwName, eipName, externalPort, protocol, dnatName)
	if err != nil {
		return fmt.Errorf("failed to get share backends for dnat %s: %w", key, err)
	}
	if len(remainingBackends) == 0 {
		// No remaining backends, delete the nft rule
		if err := c.deleteNftDnatMapInPod(gwName, protocol, v4ip, externalPort); err != nil {
			return fmt.Errorf("failed to delete nft dnat map for %s: %w", key, err)
		}
		return nil
	}
	// Rebuild nft rule with remaining backends
	if err := c.createNftDnatMapInPod(gwName, protocol, v4ip, externalPort, remainingBackends); err != nil {
		return fmt.Errorf("failed to rebuild nft dnat map for %s: %w", key, err)
	}
	return nil
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
func (c *Controller) isDnatDuplicated(gwName, eipName, dnatName, externalPort, protocol, dnatType string) (bool, error) {
	// Check if the tuple "eip:external port:protocol" is already used by another DNAT rule.
	// EIP identity is enforced via the Spec post-filter (d.Spec.EIP != eipName) below rather
	// than in the selector; see getShareBackends for why EIP name/IP cannot be used as labels.
	dnats, err := c.iptablesDnatRulesLister.List(labels.SelectorFromSet(labels.Set{
		util.VpcNatGatewayNameLabel: gwName,
		util.VpcDnatEPortLabel:      externalPort,
	}))
	if err != nil {
		return false, err
	}
	if len(dnats) == 0 {
		return false, nil
	}

	for _, d := range dnats {
		if d.Name == dnatName || d.Spec.EIP != eipName || d.Spec.Protocol != protocol {
			continue
		}
		// Found a DNAT with same identity
		if dnatType == kubeovnv1.DnatRuleTypeShare && d.Spec.Type == kubeovnv1.DnatRuleTypeShare {
			// Both are Share type, allow coexistence
			continue
		}
		// Type conflict: Exclusive vs Share, or Exclusive vs Exclusive
		err = fmt.Errorf("failed to create dnat %s, duplicate, same eip %s, same external port '%s', same protocol '%s' is used by dnat %s (type=%s)",
			dnatName, eipName, externalPort, protocol, d.Name, d.Spec.Type)
		return true, err
	}
	return false, nil
}
