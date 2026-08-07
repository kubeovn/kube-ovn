package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// The nftable LB service feature makes a vpc-nat-gw act like kube-proxy for a
// LoadBalancer type Service: it watches the Service and its EndpointSlices and
// programs one share-type IptablesDnatRule per (servicePort, ready backend).
// The share-DNAT dataplane (nft numgen random map, added by PR #6858) then load
// balances new connections across the backends and pins them by conntrack.
//
// Binding model:
//   - Only LoadBalancer type Services carrying the `ovn.kubernetes.io/eip`
//     annotation (util.EipAnnotation) are handled; the annotation value is the
//     name of an existing IptablesEIP. The gateway is derived from the EIP's
//     spec.natGwDp, so no extra gateway annotation is needed.
//   - The generated IptablesDnatRule share the EIP:externalPort:protocol identity
//     (externalPort = servicePort, internalPort = endpoint target port).
//
// Lifecycle:
//   IptablesDnatRule is cluster-scoped, so it cannot use an OwnerReference to the
//   namespaced Service for garbage collection. Instead each generated rule is
//   labeled with the owning Service (NftableLbSvcNsLabel / NftableLbSvcNameLabel)
//   and the set is reconciled (create missing, delete stale) on every Service or
//   EndpointSlice change; when the Service is deleted or no longer qualifies, all
//   labeled rules are removed.
//
// Traffic policy scope (by design):
//   This feature intentionally aligns with kube-proxy's *Cluster* traffic policy
//   only. It always load balances across all Ready endpoints of the Service and
//   does NOT implement ExternalTrafficPolicy=Local / InternalTrafficPolicy=Local,
//   topology-aware routing, or terminating-endpoint fallback. Those node-local
//   semantics do not map cleanly onto a centralized vpc-nat-gw (which is not
//   per-node), and Cluster policy is the intended, sufficient behavior here.
//   Consequently client source IP is not preserved (traffic is DNAT'd through the
//   gateway), matching what Cluster policy already implies.

// nftableLbSvcQualifies reports whether a Service should be handled by the
// nftable LB service feature: it must be a LoadBalancer and reference an EIP.
func nftableLbSvcQualifies(svc *v1.Service) bool {
	return svc.Spec.Type == v1.ServiceTypeLoadBalancer && svc.Annotations[util.EipAnnotation] != ""
}

// enqueueNftableLbService enqueues a Service key for nftable LB reconciliation.
// It is a no-op unless both the load balancer and nftable LB service features
// are enabled. Qualification and cleanup decisions are made in the handler so a
// Service that stops qualifying still gets its stale rules cleaned up.
func (c *Controller) enqueueNftableLbService(key string) {
	if !c.config.EnableLb || !c.config.EnableNftableLbSvc {
		return
	}
	klog.V(3).Infof("enqueue add/update nftable lb service %s", key)
	c.addOrUpdateNftableLbSvcQueue.Add(key)
}

func (c *Controller) handleAddOrUpdateNftableLbService(key string) error {
	if !c.config.EnableNftableLbSvc {
		return nil
	}

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.nftableLbSvcKeyMutex.LockKey(key)
	defer func() { _ = c.nftableLbSvcKeyMutex.UnlockKey(key) }()
	klog.Infof("handle add/update nftable lb service %s", key)

	cachedSvc, err := c.servicesLister.Services(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// service deleted: remove all rules owned by it
			return c.cleanupNftableLbService(nil, namespace, name)
		}
		klog.Error(err)
		return err
	}

	// service being deleted or no longer qualifying: clean up owned rules (and clear the
	// ingress IP we published, if any)
	if !cachedSvc.DeletionTimestamp.IsZero() || !nftableLbSvcQualifies(cachedSvc) {
		return c.cleanupNftableLbService(cachedSvc, namespace, name)
	}

	eipName := cachedSvc.Annotations[util.EipAnnotation]
	eip, err := c.GetEip(eipName)
	if err != nil {
		// EIP not ready yet: requeue and retry once it has an IPv4 address
		klog.Errorf("nftable lb service %s references eip %s which is not ready: %v", key, eipName, err)
		return err
	}
	// share DNAT is implemented with `ip daddr`/`ip saddr` and only supports IPv4
	if util.CheckProtocol(eip.Status.IP) != kubeovnv1.ProtocolIPv4 {
		klog.Errorf("nftable lb service %s references eip %s without an IPv4 address, skipping (share DNAT is IPv4 only)", key, eipName)
		return c.cleanupNftableLbService(cachedSvc, namespace, name)
	}

	// Publish the EIP's IPv4 address as the Service's external LoadBalancer ingress IP.
	// Unlike the classic lb-svc path (which runs a VIP pod), this mode has no allocated
	// ingress IP of its own; the EIP is the externally reachable address, so surface it in
	// status.loadBalancer.ingress to fill in the otherwise-<pending> EXTERNAL-IP.
	if err = c.ensureNftableLbSvcIngressIP(cachedSvc, eip.Status.IP); err != nil {
		klog.Errorf("failed to set ingress ip for nftable lb service %s: %v", key, err)
		return err
	}

	endpointSlices, err := c.endpointSlicesLister.EndpointSlices(namespace).List(labels.Set{discoveryv1.LabelServiceName: name}.AsSelector())
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return c.cleanupNftableLbService(cachedSvc, namespace, name)
		}
		klog.Error(err)
		return err
	}

	// The gateway lives in its VpcNatGateway's VPC and can only DNAT to backends reachable
	// there, so backend IPs are resolved against that VPC (see nftableLbBackendResolver).
	natGw, err := c.vpcNatGatewayLister.Get(eip.Spec.NatGwDp)
	if err != nil {
		klog.Errorf("nftable lb service %s: failed to get nat gateway %s referenced by eip %s: %v", key, eip.Spec.NatGwDp, eipName, err)
		return err
	}

	desired := buildDesiredNftableLbDnatRules(cachedSvc, eipName, endpointSlices, c.nftableLbBackendResolver(cachedSvc, natGw.Spec.Vpc))

	// A share DNAT identity (eip+externalPort+protocol) aggregates all its backends into
	// a single nft map, so it can be programmed by only one owner. When multiple services
	// (or a manually-created share rule) target the same identity, a deterministic winner
	// keeps it and the others back off; this avoids backend cross-talk and reconcile
	// oscillation between the competing owners.
	if len(desired) > 0 {
		if err = c.resolveNftableLbConflicts(cachedSvc, key, desired); err != nil {
			return err
		}
	}

	existing, err := c.iptablesDnatRulesLister.List(labels.SelectorFromSet(labels.Set{
		util.NftableLbSvcNsLabel:   namespace,
		util.NftableLbSvcNameLabel: name,
	}))
	if err != nil {
		klog.Errorf("failed to list nftable lb dnat rules for service %s: %v", key, err)
		return err
	}
	existingByName := make(map[string]*kubeovnv1.IptablesDnatRule, len(existing))
	for _, rule := range existing {
		existingByName[rule.Name] = rule
	}

	// Reconcile existing rules against the desired set. The rule name encodes only the
	// backend identity (svc, protocol, ports, backend IP), not mutable fields such as the
	// EIP or session-affinity settings, so a same-named rule whose Spec drifted from the
	// desired one must be recreated: share rules are immutable once Ready (webhook), so the
	// controller deletes the drifted rule and recreates it on a follow-up reconcile.
	needRequeue := false
	for _, rule := range existing {
		want, ok := desired[rule.Name]
		if !ok {
			// stale: not desired anymore
			if err = c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Delete(context.Background(), rule.Name, metav1.DeleteOptions{}); err != nil {
				if !k8serrors.IsNotFound(err) {
					klog.Errorf("failed to delete stale nftable lb dnat rule %s for service %s: %v", rule.Name, key, err)
					return err
				}
			}
			klog.Infof("deleted stale nftable lb dnat rule %s for service %s", rule.Name, key)
			continue
		}
		if !nftableLbDnatSpecEqual(&rule.Spec, &want.Spec) {
			// drifted (e.g. session affinity or EIP changed): delete and recreate later
			if err = c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Delete(context.Background(), rule.Name, metav1.DeleteOptions{}); err != nil {
				if !k8serrors.IsNotFound(err) {
					klog.Errorf("failed to delete drifted nftable lb dnat rule %s for service %s: %v", rule.Name, key, err)
					return err
				}
			}
			klog.Infof("deleted drifted nftable lb dnat rule %s for service %s (will recreate)", rule.Name, key)
			// do not recreate in the same pass: the object is still terminating
			delete(desired, rule.Name)
			needRequeue = true
		}
	}

	// create missing rules
	for _, rule := range desired {
		if _, ok := existingByName[rule.Name]; ok {
			continue
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Create(context.Background(), rule, metav1.CreateOptions{}); err != nil {
			if k8serrors.IsAlreadyExists(err) {
				// a previous drifted instance is still terminating; retry later
				needRequeue = true
				continue
			}
			klog.Errorf("failed to create nftable lb dnat rule %s for service %s: %v", rule.Name, key, err)
			return err
		}
		klog.Infof("created nftable lb dnat rule %s for service %s (eip %s, %s:%s -> %s:%s, affinity=%q)",
			rule.Name, key, rule.Spec.EIP, rule.Spec.EIP, rule.Spec.ExternalPort, rule.Spec.InternalIP, rule.Spec.InternalPort, rule.Spec.SessionAffinity)
	}

	if needRequeue {
		c.addOrUpdateNftableLbSvcQueue.AddAfter(key, 2*time.Second)
	}

	return nil
}

// nftableLbDnatSpecEqual compares the controller-managed fields of two share DNAT specs.
// It intentionally ignores server-defaulted or unrelated fields so only meaningful drift
// (identity, backend, or session-affinity changes) triggers a rule recreate.
func nftableLbDnatSpecEqual(a, b *kubeovnv1.IptablesDnatRuleSpec) bool {
	return a.EIP == b.EIP &&
		a.ExternalPort == b.ExternalPort &&
		a.Protocol == b.Protocol &&
		a.InternalIP == b.InternalIP &&
		a.InternalPort == b.InternalPort &&
		a.Type == b.Type &&
		a.SessionAffinity == b.SessionAffinity &&
		a.SessionAffinityTimeoutSeconds == b.SessionAffinityTimeoutSeconds
}

// ensureNftableLbSvcIngressIP sets the Service's status.loadBalancer.ingress to the EIP's
// IPv4 address so the LoadBalancer reports the externally reachable IP instead of staying
// <pending>. It is a no-op when the ingress list already advertises exactly that IP.
func (c *Controller) ensureNftableLbSvcIngressIP(svc *v1.Service, ip string) error {
	ingress := svc.Status.LoadBalancer.Ingress
	if len(ingress) == 1 && ingress[0].IP == ip {
		return nil
	}

	updated := svc.DeepCopy()
	updated.Status.LoadBalancer.Ingress = []v1.LoadBalancerIngress{{IP: ip}}
	if _, err := c.config.KubeClient.CoreV1().Services(svc.Namespace).UpdateStatus(context.Background(), updated, metav1.UpdateOptions{}); err != nil {
		klog.Errorf("failed to update status of nftable lb service %s/%s: %v", svc.Namespace, svc.Name, err)
		return err
	}
	klog.Infof("set nftable lb service %s/%s ingress ip to %s", svc.Namespace, svc.Name, ip)
	return nil
}

// clearNftableLbSvcIngressIP removes a LoadBalancer ingress IP that this mode previously
// published (so EXTERNAL-IP returns to <pending> once the Service leaves nftable-lb-svc
// mode). It is a no-op when there is nothing to clear or the Service is being deleted.
func (c *Controller) clearNftableLbSvcIngressIP(svc *v1.Service) error {
	if svc == nil || !svc.DeletionTimestamp.IsZero() || len(svc.Status.LoadBalancer.Ingress) == 0 {
		return nil
	}
	updated := svc.DeepCopy()
	updated.Status.LoadBalancer.Ingress = nil
	if _, err := c.config.KubeClient.CoreV1().Services(svc.Namespace).UpdateStatus(context.Background(), updated, metav1.UpdateOptions{}); err != nil {
		klog.Errorf("failed to clear ingress ip of nftable lb service %s/%s: %v", svc.Namespace, svc.Name, err)
		return err
	}
	klog.Infof("cleared nftable lb service %s/%s ingress ip", svc.Namespace, svc.Name)
	return nil
}

// cleanupNftableLbService removes all share DNAT rules generated for the given Service and,
// when it was actually managed by this mode, clears the LoadBalancer ingress IP it published.
// Ownership is inferred from the presence of owned DNAT rules, so the status of services this
// mode never managed (e.g. LoadBalancer services handled by another provider) is never touched.
// svc may be nil (the Service was already deleted), in which case only the rules are removed.
func (c *Controller) cleanupNftableLbService(svc *v1.Service, namespace, name string) error {
	rules, err := c.iptablesDnatRulesLister.List(labels.SelectorFromSet(labels.Set{
		util.NftableLbSvcNsLabel:   namespace,
		util.NftableLbSvcNameLabel: name,
	}))
	if err != nil {
		klog.Errorf("failed to list nftable lb dnat rules for service %s/%s: %v", namespace, name, err)
		return err
	}

	// Only clear the ingress IP if we were managing this Service (we own rules for it).
	if svc != nil && len(rules) > 0 {
		if err = c.clearNftableLbSvcIngressIP(svc); err != nil {
			return err
		}
	}

	for _, rule := range rules {
		if err = c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Delete(context.Background(), rule.Name, metav1.DeleteOptions{}); err != nil {
			if k8serrors.IsNotFound(err) {
				continue
			}
			klog.Errorf("failed to delete nftable lb dnat rule %s for service %s/%s: %v", rule.Name, namespace, name, err)
			return err
		}
		klog.Infof("deleted nftable lb dnat rule %s for service %s/%s", rule.Name, namespace, name)
	}
	return nil
}

// resolveNftableLbConflicts removes from desired any rule whose share DNAT identity
// (eip+externalPort+protocol) is owned by another service or a manually-created share rule.
// A contested identity is resolved deterministically (see chooseNftableLbOwner) so exactly
// one owner programs it; losing services emit a warning event and requeue to take over once
// the identity is released. It returns an error only when the underlying list fails.
func (c *Controller) resolveNftableLbConflicts(svc *v1.Service, key string, desired map[string]*kubeovnv1.IptablesDnatRule) error {
	selfKey := svc.Namespace + "/" + svc.Name
	eipName := svc.Annotations[util.EipAnnotation]

	// identities this service wants to program
	wanted := make(map[string]struct{})
	for _, rule := range desired {
		wanted[nftableLbDnatIdentity(rule.Spec.EIP, rule.Spec.ExternalPort, rule.Spec.Protocol)] = struct{}{}
	}

	// Competing owners are derived from the Service objects that reference the same EIP and
	// expose the same port+protocol (stable intent), not from existing rules, so the winner
	// is deterministic regardless of reconcile ordering (a loser must never program a rule
	// even briefly before the winner's rules exist). Self is always a candidate.
	owners := make(map[string]map[string]struct{}, len(wanted))
	for id := range wanted {
		owners[id] = map[string]struct{}{selfKey: {}}
	}

	// Look up only the services referencing this EIP via the informer index (O(matched)),
	// instead of scanning every Service in the cluster.
	svcObjs, err := c.svcIndexer.ByIndex(IndexServiceByNftableLbEip, eipName)
	if err != nil {
		klog.Errorf("failed to query services by eip for nftable lb conflict check %s: %v", key, err)
		return err
	}
	for _, obj := range svcObjs {
		s, ok := obj.(*v1.Service)
		if !ok || (s.Namespace == svc.Namespace && s.Name == svc.Name) {
			continue
		}
		for _, id := range nftableLbSvcIdentities(s, eipName) {
			if _, ok := owners[id]; ok {
				owners[id][s.Namespace+"/"+s.Name] = struct{}{}
			}
		}
	}

	// A manually-created share rule (owner "") of the same identity always wins, so include
	// those too by scanning existing rules that are not managed by this feature.
	allDnats, err := c.iptablesDnatRulesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list iptables dnat rules for nftable lb conflict check %s: %v", key, err)
		return err
	}
	for _, d := range allDnats {
		if d.Spec.Type != kubeovnv1.DnatRuleTypeShare || nftableLbSvcOwnerKey(d) != "" {
			continue
		}
		id := nftableLbDnatIdentity(d.Spec.EIP, d.Spec.ExternalPort, d.Spec.Protocol)
		if _, ok := owners[id]; ok {
			owners[id][""] = struct{}{}
		}
	}

	// drop desired rules for identities this service does not win
	droppedIdentities := make(map[string]string)
	for name, rule := range desired {
		id := nftableLbDnatIdentity(rule.Spec.EIP, rule.Spec.ExternalPort, rule.Spec.Protocol)
		winner := chooseNftableLbOwner(owners[id])
		if winner == selfKey {
			continue
		}
		delete(desired, name)
		if _, done := droppedIdentities[id]; !done {
			droppedIdentities[id] = nftableLbOwnerDesc(winner)
		}
	}

	for id, winnerDesc := range droppedIdentities {
		klog.Warningf("nftable lb service %s yields share DNAT identity %s to %s; a given EIP:port can back only one owner", key, id, winnerDesc)
		c.recorder.Eventf(svc, v1.EventTypeWarning, "NftableLbSvcConflict",
			"share DNAT identity %s is owned by %s; this service will not program it (a given EIP:port can back only one LoadBalancer service)", id, winnerDesc)
	}

	// retry so this service can take over once the current owner releases the identity
	if len(droppedIdentities) > 0 {
		c.addOrUpdateNftableLbSvcQueue.AddAfter(key, 10*time.Second)
	}

	return nil
}

// nftableLbSvcIdentities returns the share DNAT identities (eip/externalPort/protocol) a
// LoadBalancer Service would program for the given EIP, one per tcp/udp servicePort.
func nftableLbSvcIdentities(svc *v1.Service, eipName string) []string {
	ids := make([]string, 0, len(svc.Spec.Ports))
	for _, port := range svc.Spec.Ports {
		protocol := strings.ToLower(string(port.Protocol))
		if protocol != "tcp" && protocol != "udp" {
			continue
		}
		ids = append(ids, nftableLbDnatIdentity(eipName, strconv.Itoa(int(port.Port)), protocol))
	}
	return ids
}

// nftableLbSvcOwnerKey returns the owning Service key (namespace/name) encoded in a share
// DNAT rule's labels, or "" when the rule is not managed by the nftable LB service feature
// (e.g. a manually-created share rule).
func nftableLbSvcOwnerKey(rule *kubeovnv1.IptablesDnatRule) string {
	return util.NftableLbSvcOwnerKey(rule.Labels)
}

// nftableLbDnatIdentity returns the share DNAT identity key (eip/externalPort/protocol)
// that determines which backends are aggregated into a single nft map.
func nftableLbDnatIdentity(eip, externalPort, protocol string) string {
	return eip + "/" + externalPort + "/" + strings.ToLower(protocol)
}

// chooseNftableLbOwner deterministically selects the owner that keeps a contested share
// DNAT identity. A manually-created share rule (owner "") always wins so the feature never
// stomps hand-managed rules; otherwise the lexicographically smallest service key wins.
func chooseNftableLbOwner(owners map[string]struct{}) string {
	if _, ok := owners[""]; ok {
		return ""
	}
	best := ""
	for o := range owners {
		if best == "" || o < best {
			best = o
		}
	}
	return best
}

// nftableLbOwnerDesc renders a human-readable description of a share DNAT identity owner.
func nftableLbOwnerDesc(owner string) string {
	if owner == "" {
		return "a manually-created share DNAT rule"
	}
	return "service " + owner
}

// for a Service: one rule per (servicePort, ready backend). backendIP resolves each ready
// endpoint to the single IPv4 the gateway must DNAT to (the NIC in the gateway's VPC; see
// nftableLbBackendResolver); an endpoint it cannot resolve is skipped. The map is keyed by
// rule name; the name deterministically encodes the full identity so that an unchanged
// backend always maps to the same rule (idempotent reconcile).
func buildDesiredNftableLbDnatRules(svc *v1.Service, eipName string, endpointSlices []*discoveryv1.EndpointSlice, backendIP func(discoveryv1.Endpoint) (string, bool)) map[string]*kubeovnv1.IptablesDnatRule {
	desired := make(map[string]*kubeovnv1.IptablesDnatRule)

	// Translate the Service's client-IP session affinity into the share DNAT fields. All
	// backends of one identity carry the same affinity settings, matching kube-proxy where
	// affinity is a per-ServicePort property.
	sessionAffinity := kubeovnv1.DnatSessionAffinityNone
	var affinityTimeout int32
	if svc.Spec.SessionAffinity == v1.ServiceAffinityClientIP {
		sessionAffinity = kubeovnv1.DnatSessionAffinityClientIP
		if cfg := svc.Spec.SessionAffinityConfig; cfg != nil && cfg.ClientIP != nil && cfg.ClientIP.TimeoutSeconds != nil {
			affinityTimeout = *cfg.ClientIP.TimeoutSeconds
		}
	}

	for _, port := range svc.Spec.Ports {
		protocol := strings.ToLower(string(port.Protocol))
		// share DNAT only supports tcp/udp
		if protocol != "tcp" && protocol != "udp" {
			klog.Warningf("skipping service %s/%s port %d: nftable lb service only supports tcp/udp, got %s",
				svc.Namespace, svc.Name, port.Port, port.Protocol)
			continue
		}
		externalPort := strconv.Itoa(int(port.Port))

		for _, endpointSlice := range endpointSlices {
			var targetPort int32
			for _, p := range endpointSlice.Ports {
				if p.Name != nil && *p.Name == port.Name && p.Port != nil {
					targetPort = *p.Port
					break
				}
			}
			if targetPort == 0 {
				continue
			}

			for _, endpoint := range endpointSlice.Endpoints {
				// Cluster traffic policy only (see file header): every Ready endpoint of the
				// Service is a backend, regardless of node locality. ExternalTrafficPolicy/
				// InternalTrafficPolicy=Local, topology, and terminating-endpoint fallback are
				// intentionally not honored here.
				if !endpointReady(endpoint) {
					continue
				}
				// Resolve to the IPv4 the gateway can reach in its VPC; skip a backend the
				// gateway cannot reach (resolver emits a Warning event once per pod).
				address, ok := backendIP(endpoint)
				if !ok {
					continue
				}
				internalPort := strconv.Itoa(int(targetPort))
				name := nftableLbDnatRuleName(svc.Namespace, svc.Name, protocol, externalPort, address, internalPort)
				desired[name] = &kubeovnv1.IptablesDnatRule{
					ObjectMeta: metav1.ObjectMeta{
						Name: name,
						Labels: map[string]string{
							util.NftableLbSvcNsLabel:   svc.Namespace,
							util.NftableLbSvcNameLabel: svc.Name,
						},
					},
					Spec: kubeovnv1.IptablesDnatRuleSpec{
						EIP:                           eipName,
						ExternalPort:                  externalPort,
						Protocol:                      protocol,
						InternalIP:                    address,
						InternalPort:                  internalPort,
						Type:                          kubeovnv1.DnatRuleTypeShare,
						SessionAffinity:               sessionAffinity,
						SessionAffinityTimeoutSeconds: affinityTimeout,
					},
				}
			}
		}
	}

	return desired
}

// nftableLbNicCandidate is a resolvable kube-ovn NIC of a backend pod: its IPv4 address and
// the VPC its subnet belongs to. Used to pick the backend IP reachable from the gateway.
type nftableLbNicCandidate struct {
	ipv4 string
	vpc  string
}

// selectNftableLbBackendIPv4 returns the IPv4 of the NIC that sits in gwVpc, i.e. the address
// the vpc-nat-gw (which lives in gwVpc) can actually DNAT to. A single-NIC backend in the
// gateway VPC matches on its only NIC. When a backend has several NICs in the gateway VPC the
// lowest IPv4 is chosen deterministically: a dual-NIC backend (one default-VPC NIC for
// kube-proxy + one gateway-VPC NIC for the gateway) covers ~99% of cases and has exactly one
// match, so per-NIC selection is intentionally not exposed.
func selectNftableLbBackendIPv4(candidates []nftableLbNicCandidate, gwVpc string) (string, bool) {
	var matches []string
	for _, candidate := range candidates {
		if candidate.ipv4 != "" && candidate.vpc == gwVpc {
			matches = append(matches, candidate.ipv4)
		}
	}
	if len(matches) == 0 {
		return "", false
	}
	slices.Sort(matches)
	return matches[0], true
}

// nftableLbBackendResolver returns a resolver mapping a ready endpoint to the single IPv4 the
// gateway must DNAT to. For a multi-NIC backend it selects the NIC in the gateway's VPC
// (gwVpc); a backend that has kube-ovn NICs but none in gwVpc is unreachable from the gateway
// and is skipped (with a one-shot Warning event) to avoid a black-hole share DNAT. Endpoints
// without a resolvable kube-ovn pod fall back to their primary IPv4 (best effort).
func (c *Controller) nftableLbBackendResolver(svc *v1.Service, gwVpc string) func(discoveryv1.Endpoint) (string, bool) {
	type resolved struct {
		ip string
		ok bool
	}
	// Cache one result per backend pod: a pod appears once per Service port, so memoizing
	// both avoids repeated lister traversals and emits at most one skip event per pod.
	cache := make(map[string]resolved)
	return func(ep discoveryv1.Endpoint) (string, bool) {
		primary := firstIPv4(ep.Addresses)
		if gwVpc == "" {
			return primary, primary != ""
		}
		pod := c.nftableLbEndpointPod(ep, svc.Namespace)
		if pod == nil {
			return primary, primary != ""
		}
		podKey := pod.Namespace + "/" + pod.Name
		if r, done := cache[podKey]; done {
			return r.ip, r.ok
		}

		// default to the primary endpoint IP (best effort for non-kube-ovn pods)
		ip, ok := primary, primary != ""
		candidates := c.nftableLbPodNicCandidates(pod)
		if selected, matched := selectNftableLbBackendIPv4(candidates, gwVpc); matched {
			ip, ok = selected, true
		} else if len(candidates) > 0 {
			// has kube-ovn NICs but none in the gateway VPC: unreachable from the gateway
			ip, ok = "", false
			c.recorder.Eventf(svc, v1.EventTypeWarning, "NftableLbSvcBackendSkipped",
				"backend pod %s has no NIC in gateway VPC %q; skipping it to avoid an unreachable share DNAT", podKey, gwVpc)
		}
		cache[podKey] = resolved{ip, ok}
		return ip, ok
	}
}

// nftableLbEndpointPod returns the backend Pod referenced by an endpoint, or nil when the
// endpoint has no Pod target or the pod is not in cache.
func (c *Controller) nftableLbEndpointPod(ep discoveryv1.Endpoint, defaultNamespace string) *v1.Pod {
	if ep.TargetRef == nil || ep.TargetRef.Kind != "Pod" || ep.TargetRef.Name == "" {
		return nil
	}
	namespace := ep.TargetRef.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}
	pod, err := c.podsLister.Pods(namespace).Get(ep.TargetRef.Name)
	if err != nil {
		return nil
	}
	return pod
}

// nftableLbPodNicCandidates returns one candidate per resolvable kube-ovn NIC of the pod
// (primary and attached), pairing the NIC's IPv4 with the VPC of its subnet.
func (c *Controller) nftableLbPodNicCandidates(pod *v1.Pod) []nftableLbNicCandidate {
	providers, err := c.getPodProviders(pod)
	if err != nil {
		klog.Warningf("failed to get providers for backend pod %s/%s: %v", pod.Namespace, pod.Name, err)
		return nil
	}
	var candidates []nftableLbNicCandidate
	for _, provider := range providers {
		subnetName, err := getSubnetByProvider(pod, provider)
		if err != nil {
			continue
		}
		subnet, err := c.subnetsLister.Get(subnetName)
		if err != nil {
			continue
		}
		ips := strings.Split(pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)], ",")
		candidates = append(candidates, nftableLbNicCandidate{
			ipv4: firstIPv4(ips),
			vpc:  subnet.Spec.Vpc,
		})
	}
	return candidates
}

// firstIPv4 returns the first IPv4 address in the list, or "" when there is none.
func firstIPv4(addresses []string) string {
	for _, address := range addresses {
		if util.CheckProtocol(address) == kubeovnv1.ProtocolIPv4 {
			return address
		}
	}
	return ""
}

// nftableLbDnatRuleName builds a deterministic, DNS-1123 compliant name that uniquely
// encodes the rule identity. The name is "lb-<sanitized svc name>-<12 hex hash>"; the
// svc name portion is truncated so the total length never exceeds 63 characters.
func nftableLbDnatRuleName(namespace, name, protocol, externalPort, backendIP, internalPort string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{namespace, name, protocol, externalPort, backendIP, internalPort}, "/")))
	hash := hex.EncodeToString(sum[:])[:12]

	const prefix = "lb-"
	// reserve room for prefix, the "-" separator and the 12-char hash
	maxNameLen := 63 - len(prefix) - 1 - len(hash)
	svcPart := name
	if len(svcPart) > maxNameLen {
		svcPart = svcPart[:maxNameLen]
	}
	return fmt.Sprintf("%s%s-%s", prefix, svcPart, hash)
}
