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
// Naming conventions:
//   - Shell variables: UPPER_CASE (NFT_TABLE, NFT_SERVICES_MAP, ...)
//   - nft object names: lower_case with hyphens/underscores (Linux/nftables convention)
//   - Per-identity chain: "dnat-" + md5(eip:port:protocol)[:8]  (generated in shell)

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
// The backends format passed to the gateway script is "ip1:port1;ip2:port2;...".
func (c *Controller) createNftDnatMapInPod(dp, protocol, v4ip, externalPort string, backends []string) error {
	if v4ip == "" {
		// Share DNAT is implemented with `ip daddr`/`ip saddr` nft rules and only supports IPv4.
		return errors.New("cannot create nft dnat map: empty IPv4 EIP (share dnat does not support IPv6)")
	}
	// Normalize the backend set: dedup and sort so that an unchanged set of backends always
	// produces an identical nft map. Without this, the lister's non-deterministic order would
	// reshuffle the map keys on every rebuild/redo and gratuitously rehash live connections.
	backends = dedupSortedBackends(backends)
	if len(backends) == 0 {
		return fmt.Errorf("cannot create nft dnat map for %s:%s (%s): no backends", v4ip, externalPort, protocol)
	}

	gwPod, err := c.getNatGwPod(dp, c.natGwNamespaceByName(dp))
	if err != nil {
		klog.Errorf("failed to get nat gw pod, %v", err)
		return err
	}

	backendStr := strings.Join(backends, ";")
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
func (c *Controller) getShareBackends(gwName, eipName, externalPort, protocol, dnatName string) ([]string, error) {
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
func (c *Controller) isDnatDuplicated(gwName, eipName, dnatName, externalPort, protocol, dnatType string) (bool, error) {
	// Check if the tuple "eip:external port:protocol" is already used by another DNAT rule
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
