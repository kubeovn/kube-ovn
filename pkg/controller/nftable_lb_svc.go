package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"reflect"
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

// The nftable LB service feature makes a vpc-nat-gw act like kube-proxy. Service and
// EndpointSlice are the source of truth and the only trigger: this controller derives each
// VIP:port:protocol identity and its complete backend set, then writes the gateway nft map,
// hairpin rule, loopback VIP and VPC route directly.
//
// Binding model:
//   - A Service is handled when it names its gateway in util.VpcNatGatewayAnnotation.
//     A LoadBalancer Service also names the EIP that supplies its ingress address; a
//     ClusterIP Service uses its ClusterIP as the only VIP.
//   - One IptablesDnatRule record is kept per Service port/backend so operators can query the
//     programmed forwarding in one layer and EIP accounting continues to work. A type=share
//     object is only this ledger: its add/update/delete events never write or remove NAT state.
//   - The Kube-OVN controller finalizer claims the whole data plane. Cleanup removes nft identities, hairpin,
//     loopback VIPs and routes, then deletes the records and finally releases the Service.
//   - EIP, Pod and ordinary gateway events do not trigger this controller. Their state is read
//     when a Service or EndpointSlice event reconciles the Service. Gateway instance replacement
//     is the recovery exception: it only re-enqueues bound Services, and records never redo.
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
	if svc.Annotations[util.VpcNatGatewayAnnotation] == "" {
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
	return svc.Annotations[util.VpcNatGatewayAnnotation]
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

func nftableLbSvcChanged(oldSvc, newSvc *v1.Service) bool {
	if !oldSvc.DeletionTimestamp.Equal(newSvc.DeletionTimestamp) ||
		oldSvc.Annotations[util.VpcNatGatewayAnnotation] != newSvc.Annotations[util.VpcNatGatewayAnnotation] ||
		oldSvc.Annotations[util.EipAnnotation] != newSvc.Annotations[util.EipAnnotation] ||
		oldSvc.Spec.Type != newSvc.Spec.Type ||
		!slices.Equal(util.ServiceClusterIPs(*oldSvc), util.ServiceClusterIPs(*newSvc)) ||
		!reflect.DeepEqual(oldSvc.Spec.Ports, newSvc.Spec.Ports) ||
		oldSvc.Spec.SessionAffinity != newSvc.Spec.SessionAffinity ||
		!reflect.DeepEqual(oldSvc.Spec.SessionAffinityConfig, newSvc.Spec.SessionAffinityConfig) {
		return true
	}
	return false
}

func (c *Controller) enqueueNftableLbServicesForNatGw(natGwName string) {
	if c.config == nil || !c.config.EnableGwNftableLbSvc || natGwName == "" || c.svcIndexer == nil {
		return
	}
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
	var eip *kubeovnv1.IptablesEIP
	var eipName, eipIP string
	if cachedSvc.Spec.Type == v1.ServiceTypeLoadBalancer {
		eipName = cachedSvc.Annotations[util.EipAnnotation]
		eip, err = c.GetEip(eipName)
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

	// Claim the Service before touching the gateway. Stop this pass after the write so subsequent
	// status and record updates use the fresh ResourceVersion delivered by the informer.
	added, err := c.ensureServiceControllerFinalizer(cachedSvc)
	if err != nil {
		return err
	}
	if added {
		if c.addOrUpdateNftableLbSvcQueue != nil {
			c.addOrUpdateNftableLbSvcQueue.AddAfter(key, time.Second)
		}
		return nil
	}

	// Do not publish accounting records or ingress status before a gateway instance exists. A
	// replacement instance starts empty; the gateway redo path enqueues this Service again, and
	// this delayed retry also covers a temporarily unavailable instance.
	gone, err := c.natGwDataPlaneGone(gwName)
	if err != nil {
		return err
	}
	if gone {
		if c.addOrUpdateNftableLbSvcQueue != nil {
			c.addOrUpdateNftableLbSvcQueue.AddAfter(key, 2*time.Second)
		}
		return nil
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
	for _, record := range desired {
		record.Labels[util.VpcNatGatewayNameLabel] = gwName
		record.Labels[util.VpcDnatEPortLabel] = record.Spec.ExternalPort
		if eip != nil {
			record.Labels[util.EipV4IpLabel] = eipIP
			record.Labels[util.EipUIDLabel] = string(eip.UID)
		}
	}

	// A share DNAT identity (EIP, external port and protocol) has one Service writer. When
	// several Services declare it, the deterministic Service winner keeps it.
	conflicted, err := c.resolveNftableLbConflicts(cachedSvc, key, desired)
	if err != nil {
		return err
	}
	// Service is the authoritative writer. Accounting records are synchronized only after the
	// gateway data plane succeeds; record events never trigger NAT changes.
	existing, err := c.existingNftableLbSvcRules(namespace, name)
	if err != nil {
		return err
	}
	if err = c.programNftableLbServiceDirect(cachedSvc, natGw.Name, eipIP, desired, existing); err != nil {
		return err
	}
	if err = c.syncNftableLbRecords(cachedSvc, eipIP, gwName, desired, existing); err != nil {
		return err
	}

	// The Service already programmed the gateway, so record status is not a handoff condition.
	if conflicted || cachedSvc.Spec.Type != v1.ServiceTypeLoadBalancer {
		return c.clearNftableLbSvcIngressIP(cachedSvc)
	}
	if err = c.ensureNftableLbSvcIngressIP(cachedSvc, eipIP); err != nil {
		klog.Errorf("failed to set ingress ip for nftable lb service %s: %v", key, err)
		return err
	}
	if eipName != "" {
		c.resetIptablesEipQueue.AddAfter(eipName, 3*time.Second)
	}
	return nil
}

// nftableLbIdentity is one share DNAT map: one VIP, external port and protocol, with all
// backends that the Service wants behind it. It is an internal desired-state value, not a CRD.
type nftableLbIdentity struct {
	vip             string
	externalPort    string
	protocol        string
	backends        []string
	affinity        string
	affinityTimeout int32
}

// nftableLbProgram is the executable desired state of one identity. Keeping construction pure
// makes the Service writer testable without a Kubernetes pod-exec server.
type nftableLbProgram struct {
	identity    *nftableLbIdentity
	hairpinRule string
}

// buildNftableLbIdentities groups Service accounting records into the identities the Service
// controller will eventually program. A LoadBalancer record with both EIP and ClusterIP produces
// two identities with the same backend set; a ClusterIP-only record produces one.
func buildNftableLbIdentities(records map[string]*kubeovnv1.IptablesDnatRule, eipIP string) map[string]*nftableLbIdentity {
	identities := make(map[string]*nftableLbIdentity)
	add := func(vip string, rule *kubeovnv1.IptablesDnatRule) {
		if vip == "" {
			return
		}
		key := vip + "/" + rule.Spec.ExternalPort + "/" + strings.ToLower(rule.Spec.Protocol)
		id := identities[key]
		if id == nil {
			id = &nftableLbIdentity{
				vip:             vip,
				externalPort:    rule.Spec.ExternalPort,
				protocol:        strings.ToLower(rule.Spec.Protocol),
				affinity:        rule.Spec.SessionAffinity,
				affinityTimeout: rule.Spec.SessionAffinityTimeoutSeconds,
			}
			identities[key] = id
		}
		id.backends = append(id.backends, fmt.Sprintf("%s:%s", rule.Spec.InternalIP, rule.Spec.InternalPort))
	}
	for _, rule := range records {
		if rule.Spec.EIP != "" {
			add(eipIP, rule)
		}
		add(rule.Spec.ClusterIP, rule)
	}
	for _, id := range identities {
		id.backends = dedupSortedBackends(id.backends)
	}
	return identities
}

// programNftableLbServiceDirect writes the complete desired share-DNAT identities for one
// Service. The Service and EndpointSlices are the source of truth; DNAT records are written
// afterwards only for inspection and accounting.
func (c *Controller) programNftableLbServiceDirect(svc *v1.Service, gateway, eipIP string,
	desired map[string]*kubeovnv1.IptablesDnatRule, existing []*kubeovnv1.IptablesDnatRule,
) error {
	pods, err := c.getNatGwPods(gateway, c.natGwNamespaceByName(gateway), false)
	if err != nil {
		return err
	}

	programs := buildNftableLbPrograms(desired, eipIP)
	wanted := make(map[string]struct{}, len(programs))
	for _, program := range programs {
		wanted[program.identity.vip+"/"+program.identity.externalPort+"/"+program.identity.protocol] = struct{}{}
	}
	for _, program := range programs {
		identity := program.identity
		if err = c.createNftDnatMapInPods(pods, identity.protocol, identity.vip, identity.externalPort,
			identity.backends, identity.affinity, identity.affinityTimeout); err != nil {
			return fmt.Errorf("failed to program share dnat identity %s:%s/%s for service %s/%s: %w",
				identity.vip, identity.externalPort, identity.protocol, svc.Namespace, svc.Name, err)
		}
		if err = c.execNatGwRulesInPods(pods, natGwVipHairpinAdd, []string{program.hairpinRule}); err != nil {
			return fmt.Errorf("failed to program hairpin for service %s/%s identity %s:%s/%s: %w",
				svc.Namespace, svc.Name, identity.vip, identity.externalPort, identity.protocol, err)
		}
	}

	for key, identity := range nftableLbExistingIdentities(existing) {
		if _, ok := wanted[key]; ok {
			continue
		}
		if err = c.deleteNftDnatMapInPods(pods, identity.protocol, identity.vip, identity.externalPort); err != nil {
			return fmt.Errorf("failed to remove stale identity %s of service %s/%s: %w", key, svc.Namespace, svc.Name, err)
		}
		if err = c.execNatGwRulesInPods(pods, natGwVipHairpinDel,
			[]string{fmt.Sprintf("%s,%s,%s", identity.vip, identity.externalPort, identity.protocol)}); err != nil {
			return fmt.Errorf("failed to remove stale hairpin %s of service %s/%s: %w", key, svc.Namespace, svc.Name, err)
		}
	}

	owner := svc.Namespace + "/" + svc.Name
	_, clusterIPs, err := c.desiredNatGwVipStateForService(gateway, nil, owner, desired, eipIP)
	if err != nil {
		return err
	}
	if err = c.execNatGwRulesInPods(pods, natGwVipAddrSync, clusterIPs); err != nil {
		return fmt.Errorf("failed to sync cluster VIPs of service %s/%s: %w", svc.Namespace, svc.Name, err)
	}
	if err = c.syncNatGwVipStateForService(gateway, nil, owner, desired, eipIP); err != nil {
		return fmt.Errorf("failed to sync VIP routes of service %s/%s: %w", svc.Namespace, svc.Name, err)
	}
	return nil
}

func (c *Controller) existingNftableLbSvcRules(namespace, name string) ([]*kubeovnv1.IptablesDnatRule, error) {
	return c.iptablesDnatRulesLister.List(labels.SelectorFromSet(labels.Set{
		util.NftableLbSvcNsLabel: namespace, util.NftableLbSvcNameLabel: name,
	}))
}

func nftableLbExistingIdentities(records []*kubeovnv1.IptablesDnatRule) map[string]*nftableLbIdentity {
	identities := make(map[string]*nftableLbIdentity)
	for _, record := range records {
		protocol := strings.ToLower(record.Spec.Protocol)
		add := func(vip string) {
			if vip == "" {
				return
			}
			key := vip + "/" + record.Spec.ExternalPort + "/" + protocol
			identities[key] = &nftableLbIdentity{vip: vip, externalPort: record.Spec.ExternalPort, protocol: protocol}
		}
		vip := record.Status.V4ip
		if vip == "" {
			vip = record.Labels[util.EipV4IpLabel]
		}
		add(vip)
		if record.Spec.ClusterIP != vip {
			add(record.Spec.ClusterIP)
		}
	}
	return identities
}

func buildNftableLbPrograms(records map[string]*kubeovnv1.IptablesDnatRule, eipIP string) []nftableLbProgram {
	identities := buildNftableLbIdentities(records, eipIP)
	keys := make([]string, 0, len(identities))
	for key := range identities {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	programs := make([]nftableLbProgram, 0, len(keys))
	for _, key := range keys {
		id := identities[key]
		programs = append(programs, nftableLbProgram{
			identity:    id,
			hairpinRule: fmt.Sprintf("%s,%s,%s", id.vip, id.externalPort, id.protocol),
		})
	}
	return programs
}

// syncNftableLbRecords mirrors the Service desired state for inspection and EIP accounting.
// Records are plain data: update/create desired records and delete stale ones synchronously.
func (c *Controller) syncNftableLbRecords(svc *v1.Service, eipIP, gateway string,
	desired map[string]*kubeovnv1.IptablesDnatRule, existing []*kubeovnv1.IptablesDnatRule,
) error {
	client := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules()
	for _, current := range existing {
		want, ok := desired[current.Name]
		if !ok {
			if err := client.Delete(context.Background(), current.Name, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
				return fmt.Errorf("failed to delete stale nftable LB record %s: %w", current.Name, err)
			}
			continue
		}
		if !current.DeletionTimestamp.IsZero() {
			// The user is removing an accounting object. It has no data-plane semantics; do not
			// block Service reconciliation or attempt to recreate it in this pass.
			delete(desired, current.Name)
			continue
		}
		record := current
		if !nftableLbDnatSpecEqual(&current.Spec, &want.Spec) || !maps.Equal(current.Labels, want.Labels) || len(current.Finalizers) != 0 {
			updated := current.DeepCopy()
			updated.Spec = want.Spec
			updated.Labels = maps.Clone(want.Labels)
			updated.Finalizers = nil
			var err error
			record, err = client.Update(context.Background(), updated, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update nftable LB record %s: %w", current.Name, err)
			}
		}
		if err := c.setNftableLbRecordStatus(record, eipIP, gateway); err != nil {
			return err
		}
		delete(desired, current.Name)
	}
	for _, record := range desired {
		created, err := client.Create(context.Background(), record, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create nftable LB record %s for Service %s/%s: %w", record.Name, svc.Namespace, svc.Name, err)
		}
		if err = c.setNftableLbRecordStatus(created, eipIP, gateway); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) setNftableLbRecordStatus(record *kubeovnv1.IptablesDnatRule, eipIP, gateway string) error {
	vip := eipIP
	if vip == "" {
		vip = record.Spec.ClusterIP
	}
	updated := record.DeepCopy()
	updated.Status.Ready = true
	updated.Status.V4ip = vip
	updated.Status.NatGwDp = gateway
	updated.Status.Protocol = record.Spec.Protocol
	updated.Status.ExternalPort = record.Spec.ExternalPort
	updated.Status.InternalIP = record.Spec.InternalIP
	updated.Status.InternalPort = record.Spec.InternalPort
	_, err := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().UpdateStatus(context.Background(), updated, metav1.UpdateOptions{})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (c *Controller) ensureServiceControllerFinalizer(svc *v1.Service) (bool, error) {
	if slices.Contains(svc.Finalizers, util.KubeOVNControllerFinalizer) {
		return false, nil
	}
	updated := svc.DeepCopy()
	updated.Finalizers = append(updated.Finalizers, util.KubeOVNControllerFinalizer)
	_, err := c.config.KubeClient.CoreV1().Services(svc.Namespace).Update(context.Background(), updated, metav1.UpdateOptions{})
	return err == nil, err
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
	return nil
}

// clearNftableLbSvcIngressIP removes a LoadBalancer ingress IP that this mode previously
// published (so EXTERNAL-IP returns to <pending> once the Service leaves nftable-lb-svc
// mode). It is a no-op when there is nothing to clear or the Service is being deleted.
func (c *Controller) clearNftableLbSvcIngressIP(svc *v1.Service) error {
	if svc == nil || !svc.DeletionTimestamp.IsZero() {
		return nil
	}
	if !slices.Contains(svc.Finalizers, util.KubeOVNControllerFinalizer) || len(svc.Status.LoadBalancer.Ingress) == 0 {
		return nil
	}
	updated := svc.DeepCopy()
	updated.Status.LoadBalancer.Ingress = nil
	_, err := c.config.KubeClient.CoreV1().Services(svc.Namespace).UpdateStatus(context.Background(), updated, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("failed to clear ingress ip of nftable lb service %s/%s: %v", svc.Namespace, svc.Name, err)
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
		klog.Errorf("failed to list nftable lb dnat records for service %s/%s: %v", namespace, name, err)
		return err
	}

	// Records are the ledger of the identities this Service programmed. Remove those identities
	// directly; deleting a record never drives the gateway.
	byGateway := make(map[string][]*kubeovnv1.IptablesDnatRule)
	for _, rule := range rules {
		gw := rule.Labels[util.VpcNatGatewayNameLabel]
		if gw == "" {
			gw = rule.Spec.VpcNatGwDp
		}
		if gw == "" {
			gw = rule.Status.NatGwDp
		}
		if gw != "" {
			byGateway[gw] = append(byGateway[gw], rule)
		}
	}
	for gw, ledger := range byGateway {
		gone, err := c.natGwDataPlaneGone(gw)
		if err != nil {
			return err
		}
		if gone {
			continue
		}
		pods, err := c.getNatGwPods(gw, c.natGwNamespaceByName(gw), false)
		if err != nil {
			return err
		}
		for key, identity := range nftableLbExistingIdentities(ledger) {
			if err = c.deleteNftDnatMapInPods(pods, identity.protocol, identity.vip, identity.externalPort); err != nil {
				return fmt.Errorf("failed to remove identity %s of service %s/%s: %w", key, namespace, name, err)
			}
			if err = c.execNatGwRulesInPods(pods, natGwVipHairpinDel,
				[]string{fmt.Sprintf("%s,%s,%s", identity.vip, identity.externalPort, identity.protocol)}); err != nil {
				return fmt.Errorf("failed to remove hairpin %s of service %s/%s: %w", key, namespace, name, err)
			}
		}
		owner := namespace + "/" + name
		_, clusterIPs, stateErr := c.desiredNatGwVipStateForService(gw, nil, owner, nil, "")
		if stateErr != nil {
			return stateErr
		}
		if err = c.execNatGwRulesInPods(pods, natGwVipAddrSync, clusterIPs); err != nil {
			return fmt.Errorf("failed to release cluster VIPs of service %s/%s: %w", namespace, name, err)
		}
		if err = c.syncNatGwVipStateForService(gw, nil, owner, nil, ""); err != nil {
			return fmt.Errorf("failed to release VIP routes of service %s/%s: %w", namespace, name, err)
		}
	}

	if svc != nil && slices.Contains(svc.Finalizers, util.KubeOVNControllerFinalizer) {
		if err = c.clearNftableLbSvcIngressIP(svc); err != nil {
			return err
		}
	}

	for _, rule := range rules {
		if err = c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Delete(context.Background(), rule.Name, metav1.DeleteOptions{}); err != nil {
			if k8serrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to delete nftable lb dnat record %s for service %s/%s: %w", rule.Name, namespace, name, err)
		}
	}
	if svc == nil || !slices.Contains(svc.Finalizers, util.KubeOVNControllerFinalizer) {
		return nil
	}
	// clearNftableLbSvcIngressIP may have updated status and metadata. Read the latest object before
	// removing the finalizer so the cleanup does not fail on a stale ResourceVersion.
	latest, err := c.config.KubeClient.CoreV1().Services(svc.Namespace).Get(context.Background(), svc.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	latest.Finalizers = slices.DeleteFunc(slices.Clone(latest.Finalizers), func(finalizer string) bool {
		return finalizer == util.KubeOVNControllerFinalizer
	})
	_, err = c.config.KubeClient.CoreV1().Services(svc.Namespace).Update(context.Background(), latest, metav1.UpdateOptions{})
	return err
}

// resolveNftableLbConflicts resolves conflicts between Services that declare the same
// EIP:externalPort:protocol identity. Manual share DNAT is unsupported and is not considered:
// share CRs are accounting records of Services, not independent forwarding intent.
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

// nftableLbDnatIdentity returns the share DNAT identity key (eip/externalPort/protocol)
// that determines which backends are aggregated into a single nft map.
func nftableLbDnatIdentity(eip, externalPort, protocol string) string {
	return eip + "/" + externalPort + "/" + strings.ToLower(protocol)
}

// chooseNftableLbOwner deterministically selects the lexicographically smallest Service key.
func chooseNftableLbOwner(owners map[string]struct{}) string {
	best := ""
	for owner := range owners {
		if best == "" || owner < best {
			best = owner
		}
	}
	return best
}

func nftableLbOwnerDesc(owner string) string {
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
						util.NftableLbSvcNsLabel:     svc.Namespace,
						util.NftableLbSvcNameLabel:   svc.Name,
						util.NftableLbSvcUIDLabel:    string(svc.UID),
						util.NftableLbSvcRecordLabel: "true",
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
