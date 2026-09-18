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
	"k8s.io/apimachinery/pkg/types"
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
//   - A Service is handled when it names its gateway in the
//     util.VpcNatGatewaySvcAnnotation annotation. A LoadBalancer Service also
//     carries the `ovn.kubernetes.io/eip` annotation (util.EipAnnotation), which
//     is where its ingress IP comes from and which must belong to that gateway;
//     a ClusterIP Service carries no EIP, its internal VIP is all it has.
//   - The generated IptablesDnatRule (externalPort = servicePort, internalPort =
//     endpoint target port) records both addresses of the Service port: the
//     ClusterIP it serves (util.IptablesDnatRuleSpec.ClusterIP) and, for a
//     LoadBalancer Service, the EIP whose address is the ingress IP. They are
//     aligned on one rule because they belong to one Service port.
//   - The gateway holds the ClusterIP on lo and programs the same nft map for it,
//     so the Service is reachable from inside the VPC as well; the ClusterIP on
//     the rule is the durable record of that VIP once the Service is gone.
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
//

// nftableLbSvcQualifies reports whether a Service belongs to the gateway mode selected globally.
// It must name its gateway, and a LoadBalancer Service must also reference the EIP its ingress IP
// comes from.
func nftableLbSvcQualifies(svc *v1.Service) bool {
	if svc.Annotations[util.VpcNatGatewaySvcAnnotation] == "" {
		return false
	}
	switch svc.Spec.Type {
	case v1.ServiceTypeLoadBalancer:
		return svc.Annotations[util.EipAnnotation] != ""
	case v1.ServiceTypeClusterIP:
		return true
	default:
		return false
	}
}

// nftableLbSvcGateway returns the gateway that serves the Service, or "" when none is named.
func nftableLbSvcGateway(svc *v1.Service) string {
	return svc.Annotations[util.VpcNatGatewaySvcAnnotation]
}

// enqueueNftableLbService enqueues a Service only while gateway mode is selected. Qualification
// and cleanup decisions stay in the handler so a Service that stops qualifying releases its rules.
func (c *Controller) enqueueNftableLbService(key string) {
	if c.config == nil || !c.config.EnableGwNftableLbSvc || c.addOrUpdateNftableLbSvcQueue == nil || key == "" {
		return
	}
	klog.V(3).Infof("enqueue add/update nftable lb service %s", key)
	c.addOrUpdateNftableLbSvcQueue.Add(key)
}

// enqueueNftableLbSvcOwnersFromRules queues every Service that still owns generated DNAT
// rules. The rules are cluster-scoped and cannot carry an OwnerReference to a namespaced
// Service, so a Service deleted while the controller is down would otherwise leave its rules
// behind forever (informers do not replay deletion events). Existing Services are already
// enqueued by synthetic Add events on startup, which also recreates rules deleted while the
// controller was down; this scan only adds owners that can be discovered from the rules that
// are still present.
func (c *Controller) enqueueNftableLbSvcOwnersFromRules() error {
	if c.config == nil || !c.config.EnableGwNftableLbSvc || c.addOrUpdateNftableLbSvcQueue == nil {
		return nil
	}
	rules, err := c.iptablesDnatRulesLister.List(labels.Everything())
	if err != nil {
		return err
	}
	owners := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		if owner := nftableLbSvcOwnerKey(rule); owner != "" {
			owners[owner] = struct{}{}
		}
	}
	for owner := range owners {
		c.addOrUpdateNftableLbSvcQueue.Add(owner)
	}
	return nil
}

func (c *Controller) enqueueNftableLbServicesForPod(pod *v1.Pod) {
	if c.config == nil || !c.config.EnableGwNftableLbSvc || pod == nil || c.endpointSlicesLister == nil {
		return
	}
	slices, err := c.endpointSlicesLister.EndpointSlices(pod.Namespace).List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to find endpoint slices for pod %s/%s: %v", pod.Namespace, pod.Name, err)
		return
	}
	seen := make(map[string]struct{})
	for _, endpointSlice := range slices {
		for _, endpoint := range endpointSlice.Endpoints {
			if endpoint.TargetRef == nil || endpoint.TargetRef.Kind != "Pod" || endpoint.TargetRef.Name != pod.Name {
				continue
			}
			key := findServiceKey(endpointSlice)
			if key != "" {
				seen[key] = struct{}{}
			}
		}
	}
	for key := range seen {
		c.enqueueNftableLbService(key)
	}
}

func (c *Controller) enqueueNftableLbServicesForNatGw(natGwName string) {
	if c.config == nil || !c.config.EnableGwNftableLbSvc || natGwName == "" || c.svcIndexer == nil {
		return
	}
	// The Services that name this gateway, which is every Service this feature can own: a Service that
	// reaches this gateway through its EIP names it as well (the EIP's natGwDp must be that gateway),
	// so this index alone covers the gateway's Services, whatever kind they are. A Service whose EIP
	// names another gateway is not served here at all, and handleAddOrUpdateNftableLbService cleans
	// up what that mismatch left behind.
	svcs, err := c.svcIndexer.ByIndex(IndexServiceByNftableLbGw, natGwName)
	if err != nil {
		klog.Errorf("failed to list nftable lb services of nat gw %s: %v", natGwName, err)
		return
	}
	for _, obj := range svcs {
		if svc, ok := obj.(*v1.Service); ok {
			c.enqueueNftableLbService(svc.Namespace + "/" + svc.Name)
		}
	}
}

func (c *Controller) enqueueNftableLbServicesForEIP(eipName string) {
	if c.config == nil || !c.config.EnableGwNftableLbSvc || c.svcIndexer == nil || eipName == "" {
		return
	}
	services, err := c.svcIndexer.ByIndex(IndexServiceByNftableLbEip, eipName)
	if err != nil {
		klog.Errorf("failed to find nftable lb services for eip %s: %v", eipName, err)
		return
	}
	for _, obj := range services {
		svc, ok := obj.(*v1.Service)
		if ok {
			c.enqueueNftableLbService(svc.Namespace + "/" + svc.Name)
		}
	}
}

func (c *Controller) handleAddOrUpdateNftableLbService(key string) error {
	if !c.config.EnableGwNftableLbSvc {
		return nil
	}

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

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

	// The Service names its gateway; a LoadBalancer Service additionally takes its ingress IP
	// from an EIP, which must belong to that gateway so both addresses of the Service port are
	// served by one data plane.
	gwName := nftableLbSvcGateway(cachedSvc)
	var eipName, eipIP string
	if cachedSvc.Spec.Type == v1.ServiceTypeLoadBalancer {
		eipName = cachedSvc.Annotations[util.EipAnnotation]
		eip, err := c.GetEip(eipName)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return c.cleanupNftableLbService(cachedSvc, namespace, name)
			}
			// EIP not ready yet: requeue and retry once it has an IPv4 address.
			klog.Errorf("nftable lb service %s references eip %s which is not ready: %v", key, eipName, err)
			return err
		}
		// share DNAT is implemented with `ip daddr`/`ip saddr` and only supports IPv4
		if util.CheckProtocol(eip.Status.IP) != kubeovnv1.ProtocolIPv4 {
			klog.Errorf("nftable lb service %s references eip %s without an IPv4 address, skipping (share DNAT is IPv4 only)", key, eipName)
			return c.cleanupNftableLbService(cachedSvc, namespace, name)
		}
		if eip.Spec.NatGwDp != "" && eip.Spec.NatGwDp != gwName {
			klog.Errorf("nftable lb service %s: eip %s belongs to nat gw %s, not to the annotated gw %s", key, eipName, eip.Spec.NatGwDp, gwName)
			return c.cleanupNftableLbService(cachedSvc, namespace, name)
		}
		eipIP = eip.Status.IP
	}

	// The gateway lives in its VpcNatGateway's VPC and can only DNAT to backends reachable
	// there, so backend IPs are resolved against that VPC (see nftableLbBackendResolver).
	natGw, err := c.vpcNatGatewayLister.Get(gwName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return c.cleanupNftableLbService(cachedSvc, namespace, name)
		}
		klog.Errorf("nftable lb service %s: failed to get nat gateway %s: %v", key, gwName, err)
		return err
	}
	if !natGw.DeletionTimestamp.IsZero() {
		return c.cleanupNftableLbService(cachedSvc, namespace, name)
	}

	// The internal VIP is what a ClusterIP Service is served through, and what a LoadBalancer
	// Service is served through from inside its VPC. An IPv6-only ClusterIP has no share DNAT
	// data plane (it is IPv4 only), so the Service keeps being load balanced by OVN.
	if nftableLbSvcClusterIP(cachedSvc) == "" && eipIP == "" {
		klog.Errorf("nftable lb service %s has no IPv4 clusterIP and no eip address, skipping (share DNAT is IPv4 only)", key)
		return c.cleanupNftableLbService(cachedSvc, namespace, name)
	}

	endpointSlices, err := c.endpointSlicesLister.EndpointSlices(namespace).List(labels.Set{discoveryv1.LabelServiceName: name}.AsSelector())
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return c.cleanupNftableLbService(cachedSvc, namespace, name)
		}
		klog.Error(err)
		return err
	}

	desired := buildDesiredNftableLbDnatRules(cachedSvc, eipName, gwName, endpointSlices, c.nftableLbBackendResolver(cachedSvc, natGw.Spec.Vpc))

	// A share DNAT identity (eip+externalPort+protocol) aggregates all its backends into
	// a single nft map, so it can be programmed by only one owner. When multiple services
	// (or a manually-created share rule) target the same identity, a deterministic winner
	// keeps it and the others back off; this avoids backend cross-talk and reconcile
	// oscillation between the competing owners.
	conflicted, err := c.resolveNftableLbConflicts(cachedSvc, key, desired)
	if err != nil {
		return err
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
	// desired one must be recreated. Session affinity is identity-level and webhook-immutable
	// after creation (see iptablesDnatUpdateHook), so the controller deletes the drifted rule
	// and recreates it on a follow-up reconcile.
	needRequeue := false
	for _, rule := range existing {
		if !rule.DeletionTimestamp.IsZero() {
			// The object is still terminating (e.g. a same-named rule from an earlier
			// incarnation). Do not treat it as a live rule; recreate it once its name is
			// released on a later pass.
			if _, ok := desired[rule.Name]; ok {
				delete(desired, rule.Name)
				needRequeue = true
			}
			continue
		}
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

	// Publish an external address only after its DNAT rules have reached the gateway. Backend
	// changes do not clear an address that has already been published.
	managed := cachedSvc.Annotations[util.NftableLbSvcManagedAnnotation] == "true"
	if conflicted || cachedSvc.Spec.Type != v1.ServiceTypeLoadBalancer {
		if err = c.clearNftableLbSvcIngressIP(cachedSvc); err != nil {
			return err
		}
	} else if managed || nftableLbDnatRulesReady(desired, existingByName) {
		if err = c.ensureNftableLbSvcIngressIP(cachedSvc, eipIP); err != nil {
			klog.Errorf("failed to set ingress ip for nftable lb service %s: %v", key, err)
			return err
		}
	}

	// The gateway's VIP routes and lo addresses are derived from the rules reconciled above, which
	// are the durable record. Reconciling them here as well closes the loop for a gateway whose
	// rules already exist (controller restart, or a Service that was already programmed) and whose
	// state drifted out of band: without it it would only be repaired by a later rule event.
	if err = c.syncNatGwVipState(natGw.Name, nil); err != nil {
		klog.Errorf("failed to sync vip state for nat gw %s: %v", natGw.Name, err)
		return err
	}

	return nil
}

// nftableLbSvcClusterIP returns the IPv4 ClusterIP that the gateway programs as a second share
// DNAT identity next to the Service's EIP, or "" when the Service has none: a headless Service,
// a ClusterIP that is not allocated yet, or an IPv6-only Service (share DNAT is IPv4 only).
func nftableLbSvcClusterIP(svc *v1.Service) string {
	return firstIPv4(util.ServiceClusterIPs(*svc))
}

// nftableLbDnatSpecEqual compares the controller-managed fields of two share DNAT specs.
// It intentionally ignores server-defaulted or unrelated fields so only meaningful drift
// (identity, backend, or session-affinity changes) triggers a rule recreate.
func nftableLbDnatSpecEqual(a, b *kubeovnv1.IptablesDnatRuleSpec) bool {
	return a.EIP == b.EIP &&
		a.ClusterIP == b.ClusterIP &&
		a.VpcNatGwDp == b.VpcNatGwDp &&
		a.ExternalPort == b.ExternalPort &&
		a.Protocol == b.Protocol &&
		a.InternalIP == b.InternalIP &&
		a.InternalPort == b.InternalPort &&
		a.Type == b.Type &&
		a.SessionAffinity == b.SessionAffinity &&
		a.SessionAffinityTimeoutSeconds == b.SessionAffinityTimeoutSeconds
}

func nftableLbDnatRulesReady(desired, existing map[string]*kubeovnv1.IptablesDnatRule) bool {
	if len(desired) == 0 {
		return false
	}
	for name, want := range desired {
		rule := existing[name]
		if rule == nil || !rule.DeletionTimestamp.IsZero() || !rule.Status.Ready || !nftableLbDnatSpecEqual(&rule.Spec, &want.Spec) {
			return false
		}
	}
	return true
}

func (c *Controller) ensureNftableLbSvcManaged(svc *v1.Service) error {
	if svc.Annotations[util.NftableLbSvcManagedAnnotation] == "true" {
		return nil
	}
	patch := []byte(fmt.Sprintf(`{"metadata":{"annotations":{"%s":"true"}}}`, util.NftableLbSvcManagedAnnotation))
	_, err := c.config.KubeClient.CoreV1().Services(svc.Namespace).Patch(context.Background(), svc.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

// ensureNftableLbSvcIngressIP sets the Service's status.loadBalancer.ingress to the EIP's
// IPv4 address so the LoadBalancer reports the externally reachable IP instead of staying
// <pending>. It is a no-op when the ingress list already advertises exactly that IP.
func (c *Controller) ensureNftableLbSvcIngressIP(svc *v1.Service, ip string) error {
	if len(svc.Status.LoadBalancer.Ingress) != 1 || svc.Status.LoadBalancer.Ingress[0].IP != ip {
		updated := svc.DeepCopy()
		updated.Status.LoadBalancer.Ingress = []v1.LoadBalancerIngress{{IP: ip}}
		var err error
		if svc, err = c.config.KubeClient.CoreV1().Services(svc.Namespace).UpdateStatus(context.Background(), updated, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to update status of nftable lb service %s/%s: %v", svc.Namespace, svc.Name, err)
			return err
		}
		klog.Infof("set nftable lb service %s/%s ingress ip to %s", svc.Namespace, svc.Name, ip)
	}
	return c.ensureNftableLbSvcManaged(svc)
}

// clearNftableLbSvcIngressIP removes a LoadBalancer ingress IP that this mode previously
// published (so EXTERNAL-IP returns to <pending> once the Service leaves nftable-lb-svc
// mode). It is a no-op when there is nothing to clear or the Service is being deleted.
func (c *Controller) clearNftableLbSvcIngressIP(svc *v1.Service) error {
	if svc == nil || !svc.DeletionTimestamp.IsZero() {
		return nil
	}
	if svc.Annotations[util.NftableLbSvcManagedAnnotation] != "true" && len(svc.Status.LoadBalancer.Ingress) == 0 {
		return nil
	}
	updated := svc.DeepCopy()
	updated.Status.LoadBalancer.Ingress = nil
	updated, err := c.config.KubeClient.CoreV1().Services(svc.Namespace).UpdateStatus(context.Background(), updated, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("failed to clear ingress ip of nftable lb service %s/%s: %v", svc.Namespace, svc.Name, err)
		return err
	}
	delete(updated.Annotations, util.NftableLbSvcManagedAnnotation)
	if _, err = c.config.KubeClient.CoreV1().Services(svc.Namespace).Update(context.Background(), updated, metav1.UpdateOptions{}); err != nil {
		return err
	}
	klog.Infof("cleared nftable lb service %s/%s ingress ip", svc.Namespace, svc.Name)
	return nil
}

// cleanupNftableLbService removes all share DNAT rules generated for the given Service and clears
// the LoadBalancer ingress IP this controller marked as managed. Existing rules are also evidence
// of ownership when cleanup resumes after a controller restart.
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
	if svc != nil && (svc.Annotations[util.NftableLbSvcManagedAnnotation] == "true" || len(rules) > 0) {
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
// the identity is released. A Service that is being deleted releases the identity right away,
// so a successor can take it over without waiting for the terminating Service to leave the
// informer cache. It returns an error only when the underlying list fails.
func (c *Controller) resolveNftableLbConflicts(svc *v1.Service, key string, desired map[string]*kubeovnv1.IptablesDnatRule) (bool, error) {
	selfKey := svc.Namespace + "/" + svc.Name
	eipName := svc.Annotations[util.EipAnnotation]
	if eipName == "" {
		// Only an EIP:port identity can be contested: several Services can point at the same EIP,
		// while a ClusterIP is unique to its Service and its nft map is shared by nobody else.
		return false, nil
	}

	// Identities come from Service intent, not desired backends. A Service can have no
	// ready endpoints temporarily, but it must still lose a contested identity and must
	// not publish an EIP that routes to another Service's backends.
	wanted := make(map[string]struct{})
	for _, id := range nftableLbSvcIdentities(svc, eipName) {
		wanted[id] = struct{}{}
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
		return false, err
	}
	for _, obj := range svcObjs {
		s, ok := obj.(*v1.Service)
		// The index only holds qualifying Services, so skip just self and Services that are
		// going away: a terminating owner must not block its successor.
		if !ok || (s.Namespace == svc.Namespace && s.Name == svc.Name) || !s.DeletionTimestamp.IsZero() {
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
		return false, err
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

	// Resolve every Service identity even when there are no desired backend rules.
	droppedIdentities := make(map[string]string)
	for id := range wanted {
		winner := chooseNftableLbOwner(owners[id])
		if winner == selfKey {
			continue
		}
		droppedIdentities[id] = nftableLbOwnerDesc(winner)
	}

	// Drop available backend rules for identities this service does not win.
	for name, rule := range desired {
		id := nftableLbDnatIdentity(rule.Spec.EIP, rule.Spec.ExternalPort, rule.Spec.Protocol)
		if _, conflicted := droppedIdentities[id]; conflicted {
			delete(desired, name)
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

	return len(droppedIdentities) > 0, nil
}

// nftableLbSvcIdentities returns the EIP-anchored share DNAT identities (eip/externalPort/protocol)
// a LoadBalancer Service would program, one per servicePort. Only an EIP identity can be contested
// between Services, which is why the conflict resolver uses it and nothing else.
func nftableLbSvcIdentities(svc *v1.Service, eipName string) []string {
	ids := make([]string, 0, len(svc.Spec.Ports))
	for _, port := range svc.Spec.Ports {
		protocol := strings.ToLower(string(port.Protocol))
		if protocol != "tcp" && protocol != "udp" {
			// Matches the ports the generation programs (see buildDesiredNftableLbDnatRules).
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

func endpointPortMatchesServicePort(port discoveryv1.EndpointPort, servicePort v1.ServicePort) bool {
	if port.Name == nil {
		if servicePort.Name != "" {
			return false
		}
	} else if *port.Name != servicePort.Name {
		return false
	}
	return port.Protocol == nil || *port.Protocol == servicePort.Protocol
}

// buildDesiredNftableLbDnatRules builds one rule per (servicePort, ready backend). backendIP resolves each ready
// endpoint to the single IPv4 the gateway must DNAT to (the NIC in the gateway's VPC; see
// nftableLbBackendResolver); an endpoint it cannot resolve is skipped. The map is keyed by
// rule name; the name deterministically encodes the full identity so that an unchanged
// backend always maps to the same rule (idempotent reconcile).
func buildDesiredNftableLbDnatRules(svc *v1.Service, eipName, gateway string, endpointSlices []*discoveryv1.EndpointSlice, backendIP func(discoveryv1.Endpoint) (string, bool)) map[string]*kubeovnv1.IptablesDnatRule {
	desired := make(map[string]*kubeovnv1.IptablesDnatRule)

	// The internal VIP the gateway programs with the same backends (see file header).
	clusterIP := nftableLbSvcClusterIP(svc)

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
		// share DNAT only supports tcp/udp for now; see the TODO on util.ValidateProtocol for what
		// extending it takes (the nft data plane itself is protocol agnostic).
		if protocol != "tcp" && protocol != "udp" {
			klog.Warningf("skipping service %s/%s port %d: nftable lb service only supports tcp/udp for now, got %s",
				svc.Namespace, svc.Name, port.Port, port.Protocol)
			continue
		}
		externalPort := strconv.Itoa(int(port.Port))

		for _, endpointSlice := range endpointSlices {
			var targetPort int32
			for _, p := range endpointSlice.Ports {
				if endpointPortMatchesServicePort(p, port) && p.Port != nil {
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
				rule := &kubeovnv1.IptablesDnatRule{
					Name: name,
					Labels: map[string]string{
						util.NftableLbSvcNsLabel:   svc.Namespace,
						util.NftableLbSvcNameLabel: svc.Name,
					},
					Spec: kubeovnv1.IptablesDnatRuleSpec{
						// Both addresses of the Service port are recorded on the rule: the
						// ClusterIP is the internal VIP the gateway holds on lo, EIP is the public
						// one a LoadBalancer Service publishes. A ClusterIP Service has no EIP,
						// so its rule carries only the ClusterIP; the gateway is spelled out
						// because a rule without an EIP cannot derive it.
						EIP:                           eipName,
						ClusterIP:                     clusterIP,
						VpcNatGwDp:                    gateway,
						ExternalPort:                  externalPort,
						Protocol:                      protocol,
						InternalIP:                    address,
						InternalPort:                  internalPort,
						Type:                          kubeovnv1.DnatRuleTypeShare,
						SessionAffinity:               sessionAffinity,
						SessionAffinityTimeoutSeconds: affinityTimeout,
					},
				}
				desired[name] = rule
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
// and is skipped (with a one-shot Warning event) to avoid a black-hole share DNAT. An endpoint
// whose Pod target no longer exists is skipped for the same reason; only endpoints without a
// Pod target (e.g. externally maintained EndpointSlices) fall back to their primary IPv4.
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
			// An endpoint that still references a Pod which no longer exists must not fall
			// back to its recorded address: the backend is gone and DNATing to it would black
			// hole traffic (kube-ovn removes automatic EndpointSlices entries, but manually
			// maintained EndpointSlices without a Service selector are not updated for us).
			// Endpoints without a Pod target still fall back to their primary IPv4.
			if ep.TargetRef != nil && ep.TargetRef.Kind == "Pod" && ep.TargetRef.Name != "" {
				return "", false
			}
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
