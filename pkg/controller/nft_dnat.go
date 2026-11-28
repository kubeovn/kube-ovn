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
//   │   └── Elements: { vip . protocol . port : goto dnat-XXXXX }  (element-level add/delete)
//   └── Per-identity chains: dnat-XXXXX
//       └── Rule: numgen random mod N map { 0 : ip1 . port1, 1 : ip2 . port2, ... }
//
// The vip of an identity is the address the Service is served through: the address of the EIP a
// LoadBalancer Service publishes, or the ClusterIP the gateway holds on lo. Both shapes are the
// same identity (vip, external port, protocol) and are programmed the same way.
//
// Operations:
//   - Add/remove backend: flush per-identity chain + re-add rule (atomic batch via nft -f)
//   - Add new identity: create chain + add rule + add vmap element
//   - Remove identity: delete vmap element + flush & delete chain
//
// Coexistence with exclusive (iptables) DNAT:
//   Two DNAT paths run in the same gateway pod. Exclusive-type rules use iptables
//   nat/PREROUTING (priority NF_IP_PRI_NAT_DST, ~ -100), while share-type identities use this nft
//   base chain at priority -150, so the nft hook runs first. This is not a functional conflict:
//   a share identity is owned by a Service, an exclusive rule is a hand-managed object, and both
//   the webhook and isDnatDuplicated reject an exclusive rule on an EIP:port a Service record
//   already uses (DNAT/FIP for the same EIP are mutually exclusive as well), so a packet is only
//   ever matched by one path. The ordering matters mainly for troubleshooting boundary cases,
//   e.g. when conntrack has already pinned a connection (existing flows keep their old
//   destination until they expire).
//
// Naming conventions:
//   - Shell variables: UPPER_CASE (NFT_TABLE, NFT_SERVICES_MAP, ...)
//   - nft object names: lower_case with hyphens/underscores (Linux/nftables convention)
//   - Per-identity chain: "dnat-" + md5(vip:port:protocol)[:12]  (generated in shell)
//
// Client-IP session affinity: when a share DNAT's Spec.SessionAffinity is "ClientIP", the
// gateway script programs the kube-proxy nftables affinity pattern instead of the stateless
// numgen-random map: each backend gets a dynamic timeout set ("update @affinity-set { ip saddr }"
// in a per-endpoint chain) and the per-identity chain gains a preceding "ip saddr @affinity-set
// goto <ep>" lookup, layered on top of the same numgen random dispatch (not jhash). Affinity is
// an identity-level property (every backend of the identity shares it) and is threaded through
// createNftDnatMapInPods as (sessionAffinity, affinityTimeoutSeconds); "" / none keeps the original
// stateless behavior.

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
	return c.config != nil && (c.config.EnableGwNftableLbSvc || c.config.EnableGwNftableSvcClusterIP)
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

// nftableLbSvcVips is the VIP state one Service programs on one gateway: the EIP it publishes and
// the ClusterIPs it serves. The ClusterIPs are also the addresses the instances hold on lo.
type nftableLbSvcVips struct {
	eipIP      string
	clusterIPs []string
}

// nftableLbSvcVipsOf derives that state from a Service's desired accounting records.
func nftableLbSvcVipsOf(records map[string]*kubeovnv1.IptablesDnatRule, eipIP string) nftableLbSvcVips {
	state := nftableLbSvcVips{}
	clusterIPs := set.New[string]()
	for _, rule := range records {
		if util.CheckProtocol(rule.Spec.ClusterIP) == kubeovnv1.ProtocolIPv4 {
			clusterIPs.Insert(rule.Spec.ClusterIP)
		}
		if rule.Spec.EIP != "" && util.CheckProtocol(eipIP) == kubeovnv1.ProtocolIPv4 {
			state.eipIP = eipIP
		}
	}
	state.clusterIPs = clusterIPs.SortedList()
	return state
}

// nftableLbExistingClusterIPs is the durable ledger of this Service's old loopback addresses.
func nftableLbExistingClusterIPs(records []*kubeovnv1.IptablesDnatRule) []string {
	ips := set.New[string]()
	for _, record := range records {
		if util.CheckProtocol(record.Spec.ClusterIP) == kubeovnv1.ProtocolIPv4 {
			ips.Insert(record.Spec.ClusterIP)
		}
	}
	return ips.SortedList()
}

// syncNftableLbSvcClusterIPs changes only this Service's addresses, never a gateway-wide set.
func (c *Controller) syncNftableLbSvcClusterIPs(pods []*corev1.Pod, desired, existing []string) error {
	if len(desired) != 0 {
		if err := c.execNatGwRulesInPods(pods, natGwVipAddrAdd, desired); err != nil {
			return err
		}
	}
	stale := set.New(existing...).Difference(set.New(desired...)).SortedList()
	if len(stale) != 0 {
		return c.execNatGwRulesInPods(pods, natGwVipAddrDel, stale)
	}
	return nil
}

// natGwVipRoutesForService scopes route reconciliation to this Service's desired and previous
// VIPs. Other Services contribute only retention of a shared EIP, through their stable intent.
func (c *Controller) natGwVipRoutesForService(gwName, owner string, state nftableLbSvcVips,
	clusterIPs []string, eips []nftableLbEipIdentityRef,
) (map[string]string, set.Set[string], error) {
	desired := make(map[string]string)
	managed := set.New[string]()
	for _, ip := range clusterIPs {
		managed.Insert(natGwVipRouteMatch(ip))
	}
	for _, ip := range state.clusterIPs {
		match := natGwVipRouteMatch(ip)
		desired[match] = ip
		managed.Insert(match)
	}
	if state.eipIP != "" {
		match := natGwVipRouteMatch(state.eipIP)
		desired[match] = state.eipIP
		managed.Insert(match)
	}
	eipMatches := set.New[string]()
	for _, ref := range eips {
		if util.CheckProtocol(ref.eipIP) == kubeovnv1.ProtocolIPv4 {
			match := natGwVipRouteMatch(ref.eipIP)
			managed.Insert(match)
			eipMatches.Insert(match)
		}
	}
	if eipMatches.Len() == 0 || c.svcIndexer == nil {
		return desired, managed, nil
	}
	svcs, err := c.svcIndexer.ByIndex(IndexGwNftableLbServiceByGateway, gwName)
	if err != nil {
		return nil, nil, err
	}
	for _, obj := range svcs {
		svc, ok := obj.(*corev1.Service)
		if !ok {
			continue
		}
		serveEIP, _ := c.nftableLbSvcQualifies(svc)
		if !serveEIP || svc.Namespace+"/"+svc.Name == owner || !svc.DeletionTimestamp.IsZero() ||
			len(nftableLbSvcIdentities(svc, svc.Annotations[util.EipAnnotation])) == 0 {
			continue
		}
		eip, err := c.iptablesEipsLister.Get(svc.Annotations[util.EipAnnotation])
		if k8serrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		match := natGwVipRouteMatch(eip.Status.IP)
		if eipMatches.Has(match) && (eip.Spec.NatGwDp == "" || eip.Spec.NatGwDp == gwName) {
			desired[match] = eip.Status.IP
		}
	}
	return desired, managed, nil
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

// syncNatGwVipStateForService reconciles only this Service's VIP routes, including cleanup while
// the feature gates are off. The gateway's existing external IDs remain compatible with upgrades.
func (c *Controller) syncNatGwVipStateForService(gwName, owner string, state nftableLbSvcVips,
	clusterIPs []string, eips []nftableLbEipIdentityRef,
) error {
	gw, err := c.vpcNatGatewayLister.Get(gwName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// The gateway is gone and its VPC routes went with it.
			return nil
		}
		return err
	}

	desired, managed, err := c.natGwVipRoutesForService(gwName, owner, state, clusterIPs, eips)
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
		if !managed.Has(policy.Match) {
			continue
		}
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

func (c *Controller) isDnatDuplicated(gwName string, rule *kubeovnv1.IptablesDnatRule) (bool, error) {
	spec := &rule.Spec
	dnatName := rule.Name
	// Check if the tuple "eip:external port:protocol" is already used by another DNAT rule.
	// The EIP remains a Spec post-filter because the gateway and external-port labels are the only
	// indexed parts of the DNAT identity: an EIP name can exceed the 63-char label-value limit.
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
		if d.Name == dnatName || d.Spec.EIP != spec.EIP || strings.ToLower(d.Spec.Protocol) != canonicalProtocol || canonicalDnatPort(d.Spec.ExternalPort) != canonicalExternalPort {
			continue
		}
		err = fmt.Errorf("failed to create dnat %s, duplicate, same eip %s, same external port '%s', same protocol '%s' is used by dnat %s (type=%s)",
			dnatName, spec.EIP, spec.ExternalPort, spec.Protocol, d.Name, d.Spec.Type)
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
