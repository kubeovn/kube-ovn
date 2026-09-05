package controller

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type IPPortMapping map[string]string

// getServiceForEndpointSlice returns the service linked to an EndpointSlice
func getServiceForEndpointSlice(endpointSlice *discoveryv1.EndpointSlice) string {
	if endpointSlice != nil && endpointSlice.Labels != nil {
		return endpointSlice.Labels[discoveryv1.LabelServiceName]
	}

	return ""
}

func findServiceKey(endpointSlice *discoveryv1.EndpointSlice) string {
	service := getServiceForEndpointSlice(endpointSlice)
	if service == "" {
		return ""
	}

	return endpointSlice.Namespace + "/" + service
}

func (c *Controller) enqueueAddEndpointSlice(obj any) {
	if !c.config.EnableLb {
		return
	}

	key := findServiceKey(obj.(*discoveryv1.EndpointSlice))
	if key != "" {
		klog.V(3).Infof("enqueue add endpointSlice %s", key)
		c.addOrUpdateEndpointSliceQueue.Add(key)
	}
}

func (c *Controller) enqueueUpdateEndpointSlice(oldObj, newObj any) {
	if !c.config.EnableLb {
		return
	}

	oldEndpointSlice := oldObj.(*discoveryv1.EndpointSlice)
	newEndpointSlice := newObj.(*discoveryv1.EndpointSlice)
	if oldEndpointSlice.ResourceVersion == newEndpointSlice.ResourceVersion {
		return
	}

	if len(oldEndpointSlice.Endpoints) == 0 && len(newEndpointSlice.Endpoints) == 0 {
		return
	}

	// skip metadata-only churn, e.g. the endpoints.kubernetes.io/last-change-trigger-time
	// annotation refresh: the LB backends are computed solely from endpoints, ports and
	// the owning service.
	if getServiceForEndpointSlice(oldEndpointSlice) == getServiceForEndpointSlice(newEndpointSlice) &&
		reflect.DeepEqual(oldEndpointSlice.Endpoints, newEndpointSlice.Endpoints) &&
		reflect.DeepEqual(oldEndpointSlice.Ports, newEndpointSlice.Ports) {
		return
	}

	key := findServiceKey(newEndpointSlice)
	if key != "" {
		klog.V(3).Infof("enqueue update endpointSlice for service %s", key)
		c.addOrUpdateEndpointSliceQueue.Add(key)
	}
}

func (c *Controller) enqueueDeleteEndpointSlice(obj any) {
	if !c.config.EnableLb {
		return
	}

	var endpointSlice *discoveryv1.EndpointSlice
	switch t := obj.(type) {
	case *discoveryv1.EndpointSlice:
		endpointSlice = t
	case cache.DeletedFinalStateUnknown:
		s, ok := t.Obj.(*discoveryv1.EndpointSlice)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		endpointSlice = s
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	key := findServiceKey(endpointSlice)
	if key != "" {
		klog.V(3).Infof("enqueue delete endpointSlice for service %s", key)
		c.addOrUpdateEndpointSliceQueue.Add(key)
	}
}

type endpointSliceServiceProfile struct {
	lbVips               []string
	trafficClasses       map[string]serviceLBTrafficClass
	externalVIPNode      string
	serviceL2StatusReady bool
	ignoreHealthCheck    bool
	preferLocalBackend   bool
	distributedLocal     bool
}

type serviceLoadBalancerSet struct {
	current  map[v1.Protocol]string
	previous map[v1.Protocol]string
}

type endpointSliceReconcileContext struct {
	service           *v1.Service
	endpointSlices    []*discoveryv1.EndpointSlice
	vpc               *kubeovnv1.Vpc
	vpcName           string
	subnetName        string
	profile           endpointSliceServiceProfile
	loadBalancers     serviceLoadBalancerSet
	desiredScopedVIPs map[string]map[string]struct{}
}

type serviceEndpointVIPState struct {
	port         v1.ServicePort
	lbVip        string
	lb           string
	oldLB        string
	vip          string
	checkIP      string
	backends     []string
	mapping      IPPortMapping
	externals    map[string]string
	trafficClass serviceLBTrafficClass
	distributed  bool
	template     bool
}

func (c *Controller) handleUpdateEndpointSlice(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.epKeyMutex.LockKey(key)
	defer func() { _ = c.epKeyMutex.UnlockKey(key) }()
	klog.Infof("handle update endpointSlice for service %s", key)

	endpointSlices, err := c.endpointSlicesLister.EndpointSlices(namespace).List(labels.Set{discoveryv1.LabelServiceName: name}.AsSelector())
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	cachedService, err := c.servicesLister.Services(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return c.reconcileServiceEndpointSlices(cachedService.DeepCopy(), endpointSlices)
}

func (c *Controller) reconcileServiceEndpointSlices(svc *v1.Service, endpointSlices []*discoveryv1.EndpointSlice) error {
	profile, err := c.endpointSliceServiceProfile(svc)
	if err != nil || len(profile.lbVips) == 0 {
		return err
	}
	if err := c.replaceEndpointSliceSecondaryIPs(svc, endpointSlices); err != nil {
		return err
	}
	vpcName, subnetName, err := c.getVpcAndSubnetForEndpoints(endpointSlices, svc)
	if err != nil {
		return err
	}
	vpc, err := c.vpcsLister.Get(vpcName)
	if err != nil {
		return fmt.Errorf("get vpc %s: %w", vpcName, err)
	}
	reconcileCtx := &endpointSliceReconcileContext{
		service:        svc,
		endpointSlices: endpointSlices,
		vpc:            vpc,
		vpcName:        vpcName,
		subnetName:     subnetName,
		profile:        profile,
		loadBalancers: serviceLoadBalancerSet{
			current:  make(map[v1.Protocol]string),
			previous: make(map[v1.Protocol]string),
		},
		desiredScopedVIPs: make(map[string]map[string]struct{}),
	}
	if err := c.prepareServiceScopedLoadBalancers(reconcileCtx); err != nil {
		return err
	}
	if serviceUsesTrafficDistribution(svc) {
		if err := c.reconcileServiceTrafficDistribution(svc, endpointSlices, vpc, profile.lbVips, profile.trafficClasses); err != nil {
			return err
		}
	}
	if err := c.clearServiceExternalTrafficLocalMarkers(reconcileCtx); err != nil {
		return err
	}
	if err := c.reconcileServiceEndpointVIPs(reconcileCtx); err != nil {
		return err
	}
	if err := c.finishServiceEndpointSliceReconcile(reconcileCtx); err != nil {
		return err
	}
	return nil
}

func (c *Controller) endpointSliceServiceProfile(svc *v1.Service) (endpointSliceServiceProfile, error) {
	profile := endpointSliceServiceProfile{
		trafficClasses:       make(map[string]serviceLBTrafficClass),
		serviceL2StatusReady: true,
		ignoreHealthCheck:    true,
		distributedLocal:     serviceUsesDistributedLB(svc),
	}
	if profile.distributedLocal && !c.config.EnableOVNLBDistributed {
		return profile, fmt.Errorf("service %s/%s uses internalTrafficPolicy=Local but OVN distributed load balancers are disabled; enable --enable-ovn-lb-distributed with OVN 26.03+", svc.Namespace, svc.Name)
	}

	if vip := svc.Annotations[util.SwitchLBRuleVipsAnnotation]; vip != "" {
		profile.addVIP(vip, serviceLBExternalTraffic, svc)
	} else if vips := svc.Annotations[util.RouterLBRuleVipsAnnotation]; vips != "" {
		for vip := range strings.SplitSeq(vips, ",") {
			if vip = strings.TrimSpace(vip); vip != "" {
				profile.addVIP(vip, serviceLBExternalTraffic, svc)
			}
		}
	} else {
		for _, vip := range util.ServiceClusterIPs(*svc) {
			profile.addVIP(vip, serviceLBInternalTraffic, svc)
		}
	}
	if !c.config.EnableOVNLBPreferLocal {
		return profile, nil
	}
	if svc.Spec.Type == v1.ServiceTypeLoadBalancer {
		for _, ingress := range svc.Status.LoadBalancer.Ingress {
			if ingress.IP != "" {
				profile.addVIP(ingress.IP, serviceLBExternalTraffic, svc)
			}
		}
		if svc.Spec.ExternalTrafficPolicy == v1.ServiceExternalTrafficPolicyTypeLocal {
			profile.preferLocalBackend = true
			var err error
			profile.externalVIPNode, profile.serviceL2StatusReady, err = c.getServiceL2StatusNode(svc.Namespace, svc.Name)
			if err != nil {
				return profile, err
			}
		}
	} else if svc.Spec.Type == v1.ServiceTypeClusterIP && profile.distributedLocal {
		profile.preferLocalBackend = true
	}
	return profile, nil
}

func (p *endpointSliceServiceProfile) addVIP(vip string, trafficClass serviceLBTrafficClass, svc *v1.Service) {
	p.lbVips = append(p.lbVips, vip)
	p.trafficClasses[vip] = trafficClass
	if util.CheckProtocol(vip) == kubeovnv1.ProtocolIPv4 && !serviceHealthChecksDisabled(svc) {
		p.ignoreHealthCheck = false
	}
}

func (c *Controller) replaceEndpointSliceSecondaryIPs(svc *v1.Service, endpointSlices []*discoveryv1.EndpointSlice) error {
	if !c.config.EnableNonPrimaryCNI || !serviceHasSelector(svc) {
		return nil
	}
	pods, err := c.podsLister.Pods(svc.Namespace).List(labels.Set(svc.Spec.Selector).AsSelector())
	if err != nil {
		return fmt.Errorf("get pods for service %s/%s: %w", svc.Namespace, svc.Name, err)
	}
	if err := c.replaceEndpointAddressesWithSecondaryIPs(endpointSlices, pods); err != nil {
		return fmt.Errorf("replace endpoint addresses for service %s/%s: %w", svc.Namespace, svc.Name, err)
	}
	return nil
}

func serviceLoadBalancers(vpc *kubeovnv1.Vpc, affinity v1.ServiceAffinity) serviceLoadBalancerSet {
	regular, session := vpcLoadBalancerNames(vpc)
	if affinity == v1.ServiceAffinityClientIP {
		return serviceLoadBalancerSet{current: session, previous: regular}
	}
	return serviceLoadBalancerSet{current: regular, previous: session}
}

func (c *Controller) prepareServiceScopedLoadBalancers(reconcileCtx *endpointSliceReconcileContext) error {
	svc := reconcileCtx.service
	if !serviceUsesScopedLB(svc) {
		return nil
	}
	protocols := make(map[v1.Protocol]struct{}, len(svc.Spec.Ports))
	for _, port := range svc.Spec.Ports {
		protocols[port.Protocol] = struct{}{}
	}
	families := []string{""}
	if serviceUsesTemplateLB(svc) {
		families = serviceTemplateLBAddressFamilies(svc)
	}
	lbNames := make([]string, 0, len(protocols)*len(families))
	for protocol := range protocols {
		for _, family := range families {
			lbName, err := c.ensureServiceScopedLB(svc, protocol, family)
			if err != nil {
				return err
			}
			lbNames = append(lbNames, lbName)
			if family == "" {
				reconcileCtx.loadBalancers.current[protocol] = lbName
			}
		}
	}
	externalProtocols := make(map[v1.Protocol]struct{})
	for _, trafficClass := range reconcileCtx.profile.trafficClasses {
		if trafficClass != serviceLBExternalTraffic || !serviceUsesScopedLB(svc) {
			continue
		}
		for _, port := range svc.Spec.Ports {
			if _, ok := externalProtocols[port.Protocol]; ok {
				continue
			}
			externalProtocols[port.Protocol] = struct{}{}
			if _, err := c.ensureServiceScopedLBExternalTraffic(svc, port.Protocol, reconcileCtx.vpcName); err != nil {
				return err
			}
		}
	}
	if err := c.reconcileServiceScopedLoadBalancerAttachments(reconcileCtx.vpcName, lbNames...); err != nil {
		return err
	}
	return nil
}

func (c *Controller) clearServiceExternalTrafficLocalMarkers(reconcileCtx *endpointSliceReconcileContext) error {
	if !c.config.EnableOVNLBPreferLocal {
		return nil
	}
	svc := reconcileCtx.service
	for _, lbs := range []map[v1.Protocol]string{reconcileCtx.loadBalancers.current, reconcileCtx.loadBalancers.previous} {
		if err := c.clearLoadBalancerVIPExternalTrafficLocal(svc, lbs[v1.ProtocolTCP], lbs[v1.ProtocolUDP], lbs[v1.ProtocolSCTP]); err != nil {
			return err
		}
	}
	if svc.Spec.Type == v1.ServiceTypeLoadBalancer && serviceUsesScopedLB(svc) {
		var external [3]string
		for _, port := range svc.Spec.Ports {
			name := serviceScopedLBNameForTrafficClass(svc, port.Protocol, serviceLBExternalTraffic)
			switch port.Protocol {
			case v1.ProtocolTCP:
				external[0] = name
			case v1.ProtocolUDP:
				external[1] = name
			case v1.ProtocolSCTP:
				external[2] = name
			}
		}
		if err := c.clearLoadBalancerVIPExternalTrafficLocal(svc, external[0], external[1], external[2]); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) reconcileServiceEndpointVIPs(reconcileCtx *endpointSliceReconcileContext) error {
	for _, lbVip := range reconcileCtx.profile.lbVips {
		for _, port := range reconcileCtx.service.Spec.Ports {
			if err := c.reconcileServiceEndpointVIP(reconcileCtx, lbVip, port); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Controller) reconcileServiceEndpointVIP(reconcileCtx *endpointSliceReconcileContext, lbVip string, port v1.ServicePort) error {
	svc, profile := reconcileCtx.service, reconcileCtx.profile
	state := &serviceEndpointVIPState{
		port:         port,
		lbVip:        lbVip,
		lb:           reconcileCtx.loadBalancers.current[port.Protocol],
		oldLB:        reconcileCtx.loadBalancers.previous[port.Protocol],
		vip:          util.JoinHostPort(lbVip, port.Port),
		trafficClass: serviceLBTrafficClassForVIP(profile.trafficClasses, lbVip),
	}
	isExternalVIP := state.trafficClass == serviceLBExternalTraffic
	state.distributed = profile.distributedLocal && !isExternalVIP
	state.template = serviceUsesTemplateLB(svc) && !isExternalVIP
	if isExternalVIP && serviceUsesScopedLB(svc) {
		state.lb = serviceScopedLBNameForTrafficClass(svc, port.Protocol, serviceLBExternalTraffic)
	} else if isExternalVIP {
		shared := serviceLoadBalancers(reconcileCtx.vpc, svc.Spec.SessionAffinity)
		state.lb = shared.current[port.Protocol]
		state.oldLB = shared.previous[port.Protocol]
	}

	if state.template {
		state.lb = serviceScopedLBNameForTrafficClassAndFamily(svc, port.Protocol, serviceLBInternalTraffic, strings.ToLower(util.CheckProtocol(lbVip)))
		return nil
	}

	var err error
	state.checkIP, state.externals, err = c.serviceVIPHealthCheck(reconcileCtx, lbVip)
	if err != nil {
		return err
	}
	state.mapping, err = c.serviceVIPIPPortMapping(reconcileCtx, port, lbVip, state.distributed, state.checkIP)
	if err != nil {
		return err
	}
	state.backends = c.getEndpointBackend(reconcileCtx.endpointSlices, port, lbVip, state.distributed)
	if len(state.backends) == 0 {
		return c.deleteServiceEndpointVIP(reconcileCtx, state)
	}
	return c.addServiceEndpointVIP(reconcileCtx, state)
}

func (c *Controller) serviceVIPHealthCheck(reconcileCtx *endpointSliceReconcileContext, lbVip string) (string, map[string]string, error) {
	var checkIP string
	var externals map[string]string
	if !reconcileCtx.profile.ignoreHealthCheck {
		var err error
		checkIP, err = c.getHealthCheckVip(reconcileCtx.subnetName, lbVip)
		if err != nil {
			return "", nil, err
		}
		externals = map[string]string{util.SwitchLBRuleSubnet: reconcileCtx.subnetName}
	}
	if reconcileCtx.profile.preferLocalBackend {
		checkIP = util.MasqueradeCheckIP
	}
	return checkIP, externals, nil
}

func (c *Controller) serviceVIPIPPortMapping(reconcileCtx *endpointSliceReconcileContext, servicePort v1.ServicePort, serviceIP string, distributed bool, checkIP string) (IPPortMapping, error) {
	if distributed {
		mapping, err := c.getDistributedIPPortMapping(reconcileCtx.endpointSlices, reconcileCtx.service, servicePort, serviceIP)
		if err != nil {
			return nil, fmt.Errorf("get distributed ip port mapping for service %s/%s: %w", reconcileCtx.service.Namespace, reconcileCtx.service.Name, err)
		}
		if !reconcileCtx.profile.ignoreHealthCheck {
			decorateDistributedIPPortMapping(mapping, checkIP)
		}
		return mapping, nil
	}
	if reconcileCtx.profile.ignoreHealthCheck && !reconcileCtx.profile.preferLocalBackend {
		return nil, nil
	}
	mapping, err := c.getIPPortMapping(reconcileCtx.endpointSlices, reconcileCtx.service, checkIP)
	if err != nil {
		return nil, fmt.Errorf("get ip port mapping for service %s/%s: %w", reconcileCtx.service.Namespace, reconcileCtx.service.Name, err)
	}
	return mapping, nil
}

func (c *Controller) addServiceEndpointVIP(reconcileCtx *endpointSliceReconcileContext, state *serviceEndpointVIPState) error {
	svc, profile := reconcileCtx.service, reconcileCtx.profile
	klog.Infof("add vip endpoint %s, backends %v to LB %s", state.vip, state.backends, state.lb)
	candidates := serviceLBMigrationCandidates(svc, state.port.Protocol, state.oldLB, reconcileCtx.vpc, state.trafficClass)
	if err := c.OVNNbClient.LoadBalancerMigrateVIP(state.lb, state.vip, state.backends, state.vip, candidates...); err != nil {
		return fmt.Errorf("migrate vip %s: %w", state.vip, err)
	}
	if profile.preferLocalBackend && svc.Spec.Type == v1.ServiceTypeLoadBalancer &&
		svc.Spec.ExternalTrafficPolicy == v1.ServiceExternalTrafficPolicyTypeLocal && profile.serviceL2StatusReady &&
		slices.ContainsFunc(svc.Status.LoadBalancer.Ingress, func(ingress v1.LoadBalancerIngress) bool { return ingress.IP == state.lbVip }) {
		vipNodeLSP := ""
		if profile.externalVIPNode != "" {
			vipNodeLSP = util.NodeLspName(profile.externalVIPNode)
		}
		if err := c.OVNNbClient.SetLoadBalancerVIPExternalTrafficLocal(state.lb, state.vip, vipNodeLSP); err != nil {
			return fmt.Errorf("mark external local vip %s on load balancer %s: %w", state.vip, state.lb, err)
		}
	}
	if (profile.preferLocalBackend || state.distributed) && len(state.mapping) != 0 {
		if err := c.OVNNbClient.LoadBalancerUpdateIPPortMapping(state.lb, state.vip, state.mapping); err != nil {
			return fmt.Errorf("update ip port mapping for vip %s on load balancer %s: %w", state.vip, state.lb, err)
		}
	}
	if !profile.ignoreHealthCheck {
		if err := c.OVNNbClient.LoadBalancerAddHealthCheck(state.lb, state.vip, profile.ignoreHealthCheck, state.mapping, state.externals); err != nil {
			return fmt.Errorf("add health check for vip %s on load balancer %s: %w", state.vip, state.lb, err)
		}
	}
	if serviceUsesScopedLB(svc) {
		if reconcileCtx.desiredScopedVIPs[state.lb] == nil {
			reconcileCtx.desiredScopedVIPs[state.lb] = make(map[string]struct{})
		}
		reconcileCtx.desiredScopedVIPs[state.lb][state.vip] = struct{}{}
	}
	return nil
}

func (c *Controller) deleteServiceEndpointVIP(reconcileCtx *endpointSliceReconcileContext, state *serviceEndpointVIPState) error {
	if err := c.OVNNbClient.LoadBalancerDeleteVip(state.lb, state.vip, true); err != nil {
		return fmt.Errorf("delete vip %s from load balancer %s: %w", state.vip, state.lb, err)
	}
	if err := c.deleteServiceLBMigrationVIP(reconcileCtx.service, state.port.Protocol, state.oldLB, state.lb, state.vip, reconcileCtx.vpc, state.trafficClass); err != nil {
		return fmt.Errorf("delete migrated vip %s: %w", state.vip, err)
	}
	if c.config.EnableOVNLBPreferLocal || state.distributed {
		if err := c.OVNNbClient.LoadBalancerDeleteIPPortMapping(state.lb, state.vip); err != nil {
			return fmt.Errorf("delete ip port mapping for vip %s from load balancer %s: %w", state.vip, state.lb, err)
		}
		if state.oldLB != "" {
			if err := c.OVNNbClient.LoadBalancerDeleteIPPortMapping(state.oldLB, state.vip); err != nil {
				return fmt.Errorf("delete ip port mapping for vip %s from old load balancer %s: %w", state.vip, state.oldLB, err)
			}
		}
	}
	return nil
}

func (c *Controller) finishServiceEndpointSliceReconcile(reconcileCtx *endpointSliceReconcileContext) error {
	svc := reconcileCtx.service
	if svc.Annotations[util.VpcAnnotation] != reconcileCtx.vpcName {
		patch := util.KVPatch{util.VpcAnnotation: reconcileCtx.vpcName}
		if err := util.PatchAnnotations(c.config.KubeClient.CoreV1().Services(svc.Namespace), svc.Name, patch); err != nil {
			return fmt.Errorf("patch service %s/%s vpc annotation: %w", svc.Namespace, svc.Name, err)
		}
	}
	if !serviceUsesScopedLB(svc) {
		return c.deleteServiceScopedLoadBalancers(svc)
	}
	if err := c.cleanupServiceScopedLBVIPs(svc, reconcileCtx.desiredScopedVIPs); err != nil {
		return err
	}
	if !reconcileCtx.profile.distributedLocal || !slices.ContainsFunc(reconcileCtx.profile.lbVips, func(vip string) bool {
		return reconcileCtx.profile.trafficClasses[vip] == serviceLBExternalTraffic
	}) {
		return c.deleteServiceScopedLBExternalTraffic(svc)
	}
	return nil
}

// Update the endpoint IP address with the secondary IP address of the pod using the network attachment definition annotation
// This is a temporary fix to allow consumers to use the secondary IP address of the pod
// TODO: Remove this function and update the endpoint construction to use the secondary IP address of the pod
func (c *Controller) replaceEndpointAddressesWithSecondaryIPs(endpointSlices []*discoveryv1.EndpointSlice, pods []*v1.Pod) error {
	// Track which pods have been processed
	processedPods := make(map[string]bool)
	// Store pod information in a map
	podMap := make(map[string]*v1.Pod, len(pods))
	for i := range pods {
		podMap[pods[i].Name] = pods[i]
	}
	// Pre-compute secondary IPs for all pods to avoid repeated annotation lookups
	secondaryIPs := make(map[string]string, len(pods))
	for _, pod := range pods {
		providers, err := c.getPodProviders(pod)
		if err != nil {
			return err
		}
		if len(providers) > 0 {
			ipAddress := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, providers[0])]
			if ipAddress != "" {
				secondaryIPs[pod.Name] = ipAddress
			}
		}
	}
	// Process each endpoint slice
	for i, endpoint := range endpointSlices {
		var copiedSlice *discoveryv1.EndpointSlice
		needsUpdate := false
		// Check if any endpoints need updating first
		for j, ep := range endpoint.Endpoints {
			if ep.TargetRef != nil && ep.TargetRef.Kind == util.KindPod {
				podName := ep.TargetRef.Name
				// Skip if already processed this pod
				// Include slice index to handle pod in multiple slices
				podKey := fmt.Sprintf("%s/%d", podName, i)
				if processedPods[podKey] {
					continue
				}
				if secondaryIP, hasSecondaryIP := secondaryIPs[podName]; hasSecondaryIP {
					if pod, ok := podMap[podName]; ok {
						// Check if any address needs replacement
						for k, address := range ep.Addresses {
							// Only replace if it's the primary IP
							if address == pod.Status.PodIP {
								// Lazy deep copy
								if !needsUpdate {
									copiedSlice = endpoint.DeepCopy()
									needsUpdate = true
								}
								klog.Infof("updating pod %s/%s ip address %s to %s",
									pod.Namespace, pod.Name, pod.Status.PodIP, secondaryIP)
								copiedSlice.Endpoints[j].Addresses[k] = secondaryIP
								processedPods[podKey] = true
								// Only one primary IP per endpoint
								break
							} else if address == secondaryIP {
								// Already has secondary IP, mark as processed
								processedPods[podKey] = true
								break
							}
						}
					}
				}
			}
		}
		// Replace the slice if we made changes
		if needsUpdate {
			endpointSlices[i] = copiedSlice
		}
	}

	return nil
}

func (c *Controller) clearLoadBalancerVIPExternalTrafficLocal(svc *v1.Service, tcpLb, udpLb, sctpLb string) error {
	if svc.Spec.Type != v1.ServiceTypeLoadBalancer ||
		svc.Spec.ExternalTrafficPolicy == v1.ServiceExternalTrafficPolicyTypeLocal {
		return nil
	}

	for _, ingress := range svc.Status.LoadBalancer.Ingress {
		if ingress.IP == "" {
			continue
		}
		for _, port := range svc.Spec.Ports {
			var lb string
			switch port.Protocol {
			case v1.ProtocolTCP:
				lb = tcpLb
			case v1.ProtocolUDP:
				lb = udpLb
			case v1.ProtocolSCTP:
				lb = sctpLb
			}
			if lb == "" {
				continue
			}
			vip := util.JoinHostPort(ingress.IP, port.Port)
			if err := c.OVNNbClient.SetLoadBalancerVIPExternalTrafficLocal(lb, vip, ""); err != nil {
				return fmt.Errorf("couldn't clear external local vip marker %s on LB %s: %w", vip, lb, err)
			}
			if err := c.OVNNbClient.LoadBalancerDeleteIPPortMapping(lb, vip); err != nil {
				return fmt.Errorf("couldn't clear external local vip ip port mapping %s on LB %s: %w", vip, lb, err)
			}
		}
	}
	return nil
}

// enqueueStaticEndpointUpdateInNamespace enqueues updates for every statically generated EndpointSlice in a namespace.
// Statically generated EndpointSlices are not generated by the selectors of their parent service.
func (c *Controller) enqueueStaticEndpointUpdateInNamespace(namespace string) {
	// Find all the statically generated EndpointSlices in the namespace
	endpointSlices, err := c.findStaticEndpointSlicesInNamespace(namespace)
	if err != nil {
		err := fmt.Errorf("couldn't find static endpointslices in namespace %s: %w", namespace, err)
		klog.Error(err)
	}

	// Enqueue updates for all the EndpointSlices
	for _, slice := range endpointSlices {
		c.enqueueAddEndpointSlice(slice)
	}
}

// serviceHealthChecksDisabled returns whether health checks must be omitted for a particular service
func serviceHealthChecksDisabled(service *v1.Service) bool {
	// Service must not have disabled health checks
	if service.Annotations != nil && service.Annotations[util.ServiceHealthCheck] == "false" {
		return true
	}

	// If nothing is specified, checks are enabled by default
	return false
}

// findStaticEndpointSlicesInNamespace finds all the EndpointSlices in a namespace that are statically generated.
// Statically generated EndpointSlices are not generated by the selectors of their parent service.
func (c *Controller) findStaticEndpointSlicesInNamespace(namespace string) ([]*discoveryv1.EndpointSlice, error) {
	// Retrieve all the services in the namespace
	services, err := c.servicesLister.Services(namespace).List(labels.Everything())
	if err != nil {
		err := fmt.Errorf("couldn't list services in namespace %s: %w", namespace, err)
		klog.Error(err)
		return nil, err
	}

	// Only handle services that have static endpoints provided, and not selectors
	var filteredServices []*v1.Service
	for _, service := range services {
		if serviceHasSelector(service) {
			continue
		}

		filteredServices = append(filteredServices, service)
	}

	// Find the EndpointSlices linked to those services
	endpointSlices, err := c.findEndpointSlicesForServices(namespace, filteredServices)
	if err != nil {
		return nil, err
	}

	return endpointSlices, nil
}

// findEndpointSlicesForServices returns all the EndpointSlices that are linked to services in the same namespace.
// Parameter "namespace" is the namespace in which all the services are located.
// Parameter "services" is a list of all the services for which we want to find the EndpointSlices.
func (c *Controller) findEndpointSlicesForServices(namespace string, services []*v1.Service) ([]*discoveryv1.EndpointSlice, error) {
	var endpointSlices []*discoveryv1.EndpointSlice

	// Look up EndpointSlices for each service via the byServiceName indexer.
	for _, service := range services {
		objs, err := c.epsIndexer.ByIndex(IndexEPSByService, namespace+"/"+service.Name)
		if err != nil {
			err := fmt.Errorf("couldn't query endpointslices for service %s/%s: %w", namespace, service.Name, err)
			klog.Error(err)
			return nil, err
		}
		for _, obj := range objs {
			endpointSlices = append(endpointSlices, obj.(*discoveryv1.EndpointSlice))
		}
	}

	return endpointSlices, nil
}

// serviceHasSelector returns if a service has selectors
func serviceHasSelector(service *v1.Service) bool {
	return len(service.Spec.Selector) > 0
}

// getCustomServiceVpcAndSubnet returns the custom VPC/Subnet defined on a service
func getCustomServiceVpcAndSubnet(service *v1.Service) (vpcName, subnetName string) {
	if service.Annotations != nil {
		vpcName = service.Annotations[util.LogicalRouterAnnotation]
		subnetName = service.Annotations[util.LogicalSwitchAnnotation]
	}

	return vpcName, subnetName
}

// getDefaultVpcAndSubnet returns the default VPC/Subnet to apply to a LoadBalancer if nothing was found
// during automatic discovery. If both parameters are non-empty, they are returned as is.
func (c *Controller) getDefaultVpcAndSubnet(service *v1.Service, vpcName, subnetName string) (string, string) {
	// Default to what's on the service or to the default VPC
	if vpcName == "" {
		if vpcName = service.Annotations[util.VpcAnnotation]; vpcName == "" {
			vpcName = c.config.ClusterRouter
		}
	}

	// Use the default subnet if it wasn't found
	if subnetName == "" {
		subnetName = util.DefaultSubnet
	}

	return vpcName, subnetName
}

// getVpcAndSubnetForEndpoints returns the name of the VPC/Subnet for EndpointSlices
func (c *Controller) getVpcAndSubnetForEndpoints(endpointSlices []*discoveryv1.EndpointSlice, service *v1.Service) (vpcName, subnetName string, err error) {
	// Let the user self-determine what VPC and subnet to use if they provided annotations on the service
	// Both the VPC and Subnet must be provided
	vpcName, subnetName = getCustomServiceVpcAndSubnet(service)
	if vpcName != "" && subnetName != "" {
		return vpcName, subnetName, nil
	}

	// Choose the most optimized and straightforward way to retrieve the name of the VPC and subnet
	if serviceHasSelector(service) {
		// The service has a selector, which means that the EndpointSlices should have targets.
		// We can use those targets instead of looking at every pod in the namespace.
		vpcName, subnetName = c.findVpcAndSubnetWithTargets(endpointSlices)
	} else {
		// The service has no selectors, we must find which pods in the namespace of the service
		// are targeted by the endpoint by only looking at the IPs.
		pods, err := c.podsLister.Pods(service.Namespace).List(labels.Everything())
		if err != nil {
			err := fmt.Errorf("failed to get pods for service %s in namespace %s: %w", service.Name, service.Namespace, err)
			klog.Error(err)
			return "", "", err
		}

		vpcName, subnetName = c.findVpcAndSubnetWithNoTargets(endpointSlices, pods)
	}

	vpcName, subnetName = c.getDefaultVpcAndSubnet(service, vpcName, subnetName)
	return vpcName, subnetName, nil
}

// findVpcAndSubnetWithTargets returns the name of the VPC and Subnet for endpoints with targets
func (c *Controller) findVpcAndSubnetWithTargets(endpointSlices []*discoveryv1.EndpointSlice) (vpcName, subnetName string) {
	for _, slice := range endpointSlices {
		for _, endpoint := range slice.Endpoints {
			if endpoint.TargetRef == nil {
				continue
			}

			namespace, name := endpoint.TargetRef.Namespace, endpoint.TargetRef.Name
			if name == "" || namespace == "" {
				continue
			}

			pod, err := c.podsLister.Pods(namespace).Get(name)
			if err != nil {
				err := fmt.Errorf("couldn't retrieve pod %s/%s: %w", namespace, name, err)
				klog.Error(err)
				continue
			}

			vpc, subnet, err := c.getEndpointVpcAndSubnet(pod, endpoint.Addresses)
			if err != nil {
				err := fmt.Errorf("couldn't retrieve subnet/vpc for pod %s/%s: %w", namespace, name, err)
				klog.Error(err)
				continue
			}

			if vpcName == "" {
				vpcName = vpc
			}

			if subnetName == "" {
				subnetName = subnet
			}

			if vpcName != "" && subnetName != "" {
				return vpcName, subnetName
			}
		}
	}

	return vpcName, subnetName
}

// findVpcAndSubnetWithNoTargets returns the name of the VPC and Subnet for endpoints with no targets
func (c *Controller) findVpcAndSubnetWithNoTargets(endpointSlices []*discoveryv1.EndpointSlice, pods []*v1.Pod) (vpcName, subnetName string) {
	for _, slice := range endpointSlices {
		for _, endpoint := range slice.Endpoints {
			for _, pod := range pods {
				vpc, subnet, err := c.getEndpointVpcAndSubnet(pod, endpoint.Addresses)
				if err != nil {
					err := fmt.Errorf("couldn't retrieve subnet/vpc for pod %s/%s: %w", pod.Namespace, pod.Name, err)
					klog.Error(err)
					continue
				}

				if vpcName == "" {
					vpcName = vpc
				}

				if subnetName == "" {
					subnetName = subnet
				}

				if vpcName != "" && subnetName != "" {
					return vpcName, subnetName
				}
			}
		}
	}

	return vpcName, subnetName
}

// getHealthCheckVip get health check vip for load balancer, the vip name is the subnet name
// the vip is used to check the health of the backend pod
func (c *Controller) getHealthCheckVip(subnetName, lbVip string) (string, error) {
	var (
		needCreateHealthCheckVip bool
		checkVip                 *kubeovnv1.Vip
		checkIP                  string
		err                      error
	)
	vipName := subnetName
	checkVip, err = c.virtualIpsLister.Get(vipName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			needCreateHealthCheckVip = true
		} else {
			klog.Errorf("failed to get health check vip %s, %v", vipName, err)
			return "", err
		}
	}
	if needCreateHealthCheckVip {
		vip := &kubeovnv1.Vip{
			Name: vipName,
			Spec: kubeovnv1.VipSpec{
				Subnet: subnetName,
			},
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().Vips().Create(context.Background(), vip, metav1.CreateOptions{}); err != nil {
			if !k8serrors.IsAlreadyExists(err) {
				klog.Errorf("failed to create health check vip %s, %v", vipName, err)
				return "", err
			}

			// Another worker created the shared VIP after the lister lookup. Read
			// it from the API so this reconciliation remains idempotent.
			checkVip, err = c.config.KubeOvnClient.KubeovnV1().Vips().Get(context.Background(), vipName, metav1.GetOptions{})
			if err != nil {
				klog.Errorf("failed to get existing health check vip %s, %v", vipName, err)
				return "", err
			}
		} else {
			// wait for vip created
			// TODO: WATCH VIP
			time.Sleep(1 * time.Second)
			checkVip, err = c.virtualIpsLister.Get(vipName)
			if err != nil {
				klog.Errorf("failed to get health check vip %s, %v", vipName, err)
				return "", err
			}
		}
	}

	if checkVip.Status.V4ip == "" && checkVip.Status.V6ip == "" {
		err = fmt.Errorf("vip %s is not ready", vipName)
		klog.Error(err)
		return "", err
	}

	switch util.CheckProtocol(lbVip) {
	case kubeovnv1.ProtocolIPv4:
		checkIP = checkVip.Status.V4ip
	case kubeovnv1.ProtocolIPv6:
		checkIP = checkVip.Status.V6ip
	}
	if checkIP == "" {
		err = fmt.Errorf("failed to get health check vip subnet %s", vipName)
		klog.Error(err)
		return "", err
	}

	return checkIP, nil
}

// getEndpointBackend returns the LB backend for a service
func (c *Controller) getEndpointBackend(endpointSlices []*discoveryv1.EndpointSlice, servicePort v1.ServicePort, serviceIP string, allowTerminatingFallback bool) (backends []string) {
	for _, candidate := range serviceEndpointCandidates(endpointSlices, servicePort, serviceIP, allowTerminatingFallback) {
		for _, address := range candidate.addresses {
			backends = append(backends, util.JoinHostPort(address, candidate.targetPort))
		}
	}
	return backends
}

type serviceEndpointCandidate struct {
	endpoint   discoveryv1.Endpoint
	addresses  []string
	targetPort int32
}

type topologyBackend struct {
	backend string
	hints   *discoveryv1.EndpointHints
}

func endpointSlicePort(endpointSlice *discoveryv1.EndpointSlice, servicePort v1.ServicePort) int32 {
	for _, port := range endpointSlice.Ports {
		if port.Port == nil {
			continue
		}
		if port.Name != nil && *port.Name == servicePort.Name {
			return *port.Port
		}
		if servicePort.Name == "" && port.Name == nil {
			return *port.Port
		}
	}
	return 0
}

func topologyBackends(endpointSlices []*discoveryv1.EndpointSlice, servicePort v1.ServicePort, serviceIP string) []topologyBackend {
	var backends []topologyBackend
	for _, candidate := range serviceEndpointCandidates(endpointSlices, servicePort, serviceIP, false) {
		for _, address := range candidate.addresses {
			backends = append(backends, topologyBackend{
				backend: util.JoinHostPort(address, candidate.targetPort),
				hints:   candidate.endpoint.Hints,
			})
		}
	}
	return backends
}

func serviceEndpointCandidates(endpointSlices []*discoveryv1.EndpointSlice, servicePort v1.ServicePort, serviceIP string, allowTerminatingFallback bool) []serviceEndpointCandidate {
	protocol := util.CheckProtocol(serviceIP)
	var ready, terminating []serviceEndpointCandidate
	for _, endpointSlice := range endpointSlices {
		targetPort := endpointSlicePort(endpointSlice, servicePort)
		if targetPort == 0 {
			continue
		}
		for _, endpoint := range endpointSlice.Endpoints {
			addresses := make([]string, 0, len(endpoint.Addresses))
			for _, address := range endpoint.Addresses {
				if util.CheckProtocol(address) == protocol {
					addresses = append(addresses, address)
				}
			}
			if len(addresses) == 0 {
				continue
			}
			candidate := serviceEndpointCandidate{endpoint: endpoint, addresses: addresses, targetPort: targetPort}
			if endpointReady(endpoint) {
				ready = append(ready, candidate)
			} else if endpointServingAndTerminating(endpoint) {
				terminating = append(terminating, candidate)
			}
		}
	}
	if len(ready) != 0 || !allowTerminatingFallback {
		return ready
	}
	return terminating
}

func topologyBackendSubset(backends []topologyBackend, nodeName, zoneName, trafficDistribution string) []string {
	all := make([]string, 0, len(backends))
	allForNodes, allForZones := true, true
	for _, backend := range backends {
		all = append(all, backend.backend)
		if backend.hints == nil || len(backend.hints.ForNodes) == 0 {
			allForNodes = false
		}
		if backend.hints == nil || len(backend.hints.ForZones) == 0 {
			allForZones = false
		}
	}
	preferNode := trafficDistribution == v1.ServiceTrafficDistributionPreferSameNode
	preferZone := preferNode || trafficDistribution == v1.ServiceTrafficDistributionPreferSameZone || trafficDistribution == v1.ServiceTrafficDistributionPreferClose
	if preferNode && allForNodes && nodeName != "" {
		matched := make([]string, 0, len(backends))
		for _, backend := range backends {
			for _, hint := range backend.hints.ForNodes {
				if hint.Name == nodeName {
					matched = append(matched, backend.backend)
					break
				}
			}
		}
		if len(matched) != 0 {
			return matched
		}
	}
	if preferZone && allForZones && zoneName != "" {
		matched := make([]string, 0, len(backends))
		for _, backend := range backends {
			for _, hint := range backend.hints.ForZones {
				if hint.Name == zoneName {
					matched = append(matched, backend.backend)
					break
				}
			}
		}
		if len(matched) != 0 {
			return matched
		}
	}
	return all
}

func serviceTrafficDistributionVariablePrefix(svc *v1.Service) string {
	return serviceTemplateVarRoot + util.Sha256Hash([]byte(string(svc.UID)))[:12] + "_"
}

func (c *Controller) cleanupServiceTrafficDistributionState(svc *v1.Service) error {
	prefix := serviceTrafficDistributionVariablePrefix(svc)
	lbs, err := c.OVNNbClient.ListLoadBalancers(func(lb *ovnnb.LoadBalancer) bool {
		return lb.ExternalIDs[serviceLBOwnerExternalID] == string(svc.UID)
	})
	if err != nil {
		return fmt.Errorf("list traffic distribution load balancers for service %s/%s cleanup: %w", svc.Namespace, svc.Name, err)
	}
	for _, lb := range lbs {
		for templateVIP := range lb.Vips {
			if !strings.HasPrefix(templateVIP, "^"+prefix) {
				continue
			}
			if err := c.OVNNbClient.LoadBalancerDeleteVip(lb.Name, templateVIP, true); err != nil {
				return fmt.Errorf("delete traffic distribution VIP %s from load balancer %s: %w", templateVIP, lb.Name, err)
			}
		}
	}
	if c.OVNSbClient == nil {
		return nil
	}
	chassises, err := c.OVNSbClient.ListChassis()
	if err != nil {
		return fmt.Errorf("list OVN chassis while cleaning service %s/%s template variables: %w", svc.Namespace, svc.Name, err)
	}
	for _, chassis := range *chassises {
		if err := c.OVNNbClient.ReconcileChassisTemplateVariables(chassis.Name, prefix, nil); err != nil {
			return fmt.Errorf("clean template variables for chassis %s: %w", chassis.Name, err)
		}
	}
	return nil
}

func (c *Controller) reconcileServiceTrafficDistribution(svc *v1.Service, endpointSlices []*discoveryv1.EndpointSlice, vpc *kubeovnv1.Vpc, lbVips []string, classes map[string]serviceLBTrafficClass) error {
	chassises, err := c.OVNSbClient.ListChassis()
	if err != nil {
		return fmt.Errorf("list OVN chassis for service %s/%s traffic distribution: %w", svc.Namespace, svc.Name, err)
	}
	prefix := serviceTrafficDistributionVariablePrefix(svc)
	variablesByChassis := make(map[string]map[string]string, len(*chassises))
	desiredTemplateVIPs := make(map[string]map[string]string)
	for _, chassis := range *chassises {
		variablesByChassis[chassis.Name] = make(map[string]string)
	}
	if !serviceUsesDistributedLB(svc) {
		for _, port := range svc.Spec.Ports {
			for _, lbVip := range lbVips {
				if classes[lbVip] == serviceLBExternalTraffic {
					continue
				}
				family := strings.ToLower(util.CheckProtocol(lbVip))
				lbName := serviceScopedLBNameForTrafficClassAndFamily(svc, port.Protocol, serviceLBInternalTraffic, family)
				vip := util.JoinHostPort(lbVip, port.Port)
				base := fmt.Sprintf("%s%s_%s", prefix, strings.ToLower(string(port.Protocol)), util.Sha256Hash([]byte(vip))[:8])
				vipVariable, backendVariable := base+"_vip", base+"_backends"
				templateVIP := "^" + vipVariable + ":" + strconv.Itoa(int(port.Port))
				shared := serviceLoadBalancers(vpc, svc.Spec.SessionAffinity)
				candidates := serviceLBMigrationCandidates(svc, port.Protocol, shared.previous[port.Protocol], vpc, serviceLBInternalTraffic)
				if err := c.OVNNbClient.LoadBalancerMigrateVIP(lbName, templateVIP, []string{"^" + backendVariable}, vip, candidates...); err != nil {
					return fmt.Errorf("set traffic distribution template for service %s/%s: %w", svc.Namespace, svc.Name, err)
				}
				if desiredTemplateVIPs[lbName] == nil {
					desiredTemplateVIPs[lbName] = make(map[string]string)
				}
				desiredTemplateVIPs[lbName][templateVIP] = "^" + backendVariable
				backends := topologyBackends(endpointSlices, port, lbVip)
				for _, chassis := range *chassises {
					nodeName, zoneName := chassis.Hostname, ""
					if node, getErr := c.nodesLister.Get(chassis.Hostname); getErr == nil {
						zoneName = node.Labels[v1.LabelTopologyZone]
					} else if !k8serrors.IsNotFound(getErr) {
						return fmt.Errorf("get node %s for service %s/%s traffic distribution: %w", chassis.Hostname, svc.Namespace, svc.Name, getErr)
					}
					selected := topologyBackendSubset(backends, nodeName, zoneName, *svc.Spec.TrafficDistribution)
					variablesByChassis[chassis.Name][vipVariable] = lbVip
					variablesByChassis[chassis.Name][backendVariable] = strings.Join(selected, ",")
				}
			}
		}
	}
	lbs, err := c.OVNNbClient.ListLoadBalancers(func(lb *ovnnb.LoadBalancer) bool {
		return lb.ExternalIDs[serviceLBOwnerExternalID] == string(svc.UID)
	})
	if err != nil {
		return fmt.Errorf("list traffic distribution load balancers for service %s/%s: %w", svc.Namespace, svc.Name, err)
	}
	for _, lb := range lbs {
		desired := desiredTemplateVIPs[lb.Name]
		for templateVIP := range lb.Vips {
			if !strings.HasPrefix(templateVIP, "^"+prefix) {
				continue
			}
			if _, ok := desired[templateVIP]; ok {
				continue
			}
			if err := c.OVNNbClient.LoadBalancerDeleteVip(lb.Name, templateVIP, true); err != nil {
				return fmt.Errorf("delete stale traffic distribution VIP %s from load balancer %s: %w", templateVIP, lb.Name, err)
			}
		}
	}
	for chassis, variables := range variablesByChassis {
		if err := c.OVNNbClient.ReconcileChassisTemplateVariables(chassis, prefix, variables); err != nil {
			return err
		}
	}
	return nil
}

// endpointReady returns whether an endpoint can receive traffic
func endpointReady(endpoint discoveryv1.Endpoint) bool {
	return endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready
}

func endpointServingAndTerminating(endpoint discoveryv1.Endpoint) bool {
	return (endpoint.Conditions.Serving == nil || *endpoint.Conditions.Serving) &&
		endpoint.Conditions.Terminating != nil && *endpoint.Conditions.Terminating
}

// addIPPortMappingEntry adds a new entry to an IPPortMapping for a given target, the addresses on that target and the
// VIP used to run the health checks
func (c *Controller) addIPPortMappingEntry(pod *v1.Pod, addresses []string, checkVip string, mapping IPPortMapping) error {
	// Abort if the pod is getting deleted
	if !pod.DeletionTimestamp.IsZero() {
		return nil
	}

	// Compute the name of the LSP for that endpoint target
	lspName, err := c.getEndpointTargetLSPName(pod, addresses)
	if err != nil {
		return fmt.Errorf("couldn't get LSP for the endpoint's target: %w", err)
	}

	for _, address := range addresses {
		key := address
		if util.CheckProtocol(address) == kubeovnv1.ProtocolIPv6 {
			key = fmt.Sprintf("[%s]", address)
		}
		mapping[key] = fmt.Sprintf(util.HealthCheckNamedVipTemplate, lspName, checkVip)
	}

	return nil
}

// getIPPortMapping returns the mapping between each endpoint, LSP and health check VIP
func (c *Controller) getIPPortMapping(endpointSlices []*discoveryv1.EndpointSlice, service *v1.Service, checkVip string) (IPPortMapping, error) {
	// Choose the most optimized and straightforward way to compute the IPPortMapping
	if serviceHasSelector(service) {
		// The service has a selector, which means that the EndpointSlices should have targets.
		// We can use those targets instead of looking at every pod in the namespace.
		return c.getIPPortMappingWithTargets(endpointSlices, checkVip), nil
	}

	// The service has no selectors, we must find which pods in the namespace of the service
	// are targeted by the endpoint by only looking at the IPs.
	pods, err := c.podsLister.Pods(service.Namespace).List(labels.Everything())
	if err != nil {
		err := fmt.Errorf("failed to get pods for service %s in namespace %s: %w", service.Name, service.Namespace, err)
		klog.Error(err)
		return nil, err
	}

	return c.getIPPortMappingWithNoTargets(endpointSlices, pods, checkVip), nil
}

// getDistributedIPPortMapping returns backend IP to logical-port mappings
// required by OVN's distributed load balancer. Strict Local must not infer a
// logical port for selectorless or manually supplied endpoints, because an
// incomplete mapping would make traffic unavailable on every chassis.
func (c *Controller) getDistributedIPPortMapping(endpointSlices []*discoveryv1.EndpointSlice, service *v1.Service, servicePort v1.ServicePort, serviceIP string) (IPPortMapping, error) {
	if !serviceHasSelector(service) {
		return nil, errors.New("service has no selector; EndpointSlice targets are required for distributed load balancing")
	}

	mapping := make(IPPortMapping)
	for _, candidate := range serviceEndpointCandidates(endpointSlices, servicePort, serviceIP, true) {
		endpoint := candidate.endpoint
		if endpoint.TargetRef == nil {
			return nil, fmt.Errorf("eligible endpoint %v has no Pod target", candidate.addresses)
		}
		if endpoint.TargetRef.Kind != "" && endpoint.TargetRef.Kind != util.KindPod {
			return nil, fmt.Errorf("endpoint target %s/%s is not a Pod", endpoint.TargetRef.Namespace, endpoint.TargetRef.Name)
		}
		targetNamespace := endpoint.TargetRef.Namespace
		if targetNamespace == "" {
			targetNamespace = service.Namespace
		}
		pod, err := c.podsLister.Pods(targetNamespace).Get(endpoint.TargetRef.Name)
		if err != nil {
			return nil, fmt.Errorf("get endpoint pod %s/%s: %w", targetNamespace, endpoint.TargetRef.Name, err)
		}
		lspName, err := c.getEndpointTargetLSPName(pod, candidate.addresses)
		if err != nil {
			return nil, fmt.Errorf("get logical port for endpoint pod %s/%s: %w", pod.Namespace, pod.Name, err)
		}
		lsp, err := c.OVNNbClient.GetLogicalSwitchPort(lspName, true)
		if err != nil {
			return nil, fmt.Errorf("get logical port %s for endpoint pod %s/%s: %w", lspName, pod.Namespace, pod.Name, err)
		}
		if lsp == nil {
			return nil, fmt.Errorf("logical port %s for endpoint pod %s/%s does not exist", lspName, pod.Namespace, pod.Name)
		}
		for _, address := range candidate.addresses {
			key := address
			if util.CheckProtocol(address) == kubeovnv1.ProtocolIPv6 {
				key = fmt.Sprintf("[%s]", address)
			}
			mapping[key] = lspName
		}
	}
	return mapping, nil
}

// decorateDistributedIPPortMapping preserves the health-check source VIP while
// keeping the logical port prefix required by OVN distributed load balancers.
func decorateDistributedIPPortMapping(mapping IPPortMapping, sourceIP string) {
	if sourceIP == "" {
		return
	}
	for backendIP, logicalPort := range mapping {
		mapping[backendIP] = fmt.Sprintf("%s:%s", logicalPort, sourceIP)
	}
}

// getIPPortMappingWithTargets returns the IPPortMapping for endpoints with targets
func (c *Controller) getIPPortMappingWithTargets(endpointSlices []*discoveryv1.EndpointSlice, checkVip string) IPPortMapping {
	mapping := make(IPPortMapping)

	for _, slice := range endpointSlices {
		for _, endpoint := range slice.Endpoints {
			if endpoint.TargetRef == nil {
				continue
			}

			namespace, name := endpoint.TargetRef.Namespace, endpoint.TargetRef.Name
			if name == "" || namespace == "" {
				continue
			}

			// Retrieve the pod for that endpoint target
			pod, err := c.podsLister.Pods(namespace).Get(name)
			if err != nil {
				err := fmt.Errorf("couldn't retrieve pod %s/%s: %w", namespace, name, err)
				klog.Error(err)
				continue
			}

			// Compute the IPPortMapping for that endpoint target
			if err := c.addIPPortMappingEntry(pod, endpoint.Addresses, checkVip, mapping); err != nil {
				err := fmt.Errorf("couldn't compute ip port mapping for pod %s/%s: %w", namespace, name, err)
				klog.Error(err)
				continue
			}
		}
	}

	return mapping
}

// getIPPortMappingWithNoTargets returns the IPPortMapping for endpoints with no targets
func (c *Controller) getIPPortMappingWithNoTargets(endpointSlices []*discoveryv1.EndpointSlice, pods []*v1.Pod, checkVip string) IPPortMapping {
	mapping := make(IPPortMapping)

	for _, slice := range endpointSlices {
		for _, endpoint := range slice.Endpoints {
			for _, pod := range pods {
				// Try to find a matching provider for the addresses
				provider, err := c.getEndpointProvider(pod, endpoint.Addresses)
				if err != nil {
					err := fmt.Errorf("couldn't get provider for pod %s/%s: %w", pod.Namespace, pod.Name, err)
					klog.Error(err)
					continue
				}

				// If the pod has a provider that matches that set of addresses, it is an endpoint target.
				// Otherwise, it isn't targeted by the EndpointSlice and can be dismissed.
				if provider == "" {
					continue
				}

				// Compute the IPPortMapping for that endpoint target
				if err := c.addIPPortMappingEntry(pod, endpoint.Addresses, checkVip, mapping); err != nil {
					err := fmt.Errorf("couldn't compute ip port mapping for pod %s/%s: %w", pod.Namespace, pod.Name, err)
					klog.Error(err)
					continue
				}
			}
		}
	}

	return mapping
}

// getPodProviders returns all the providers available on a pod
func (c *Controller) getPodProviders(pod *v1.Pod) ([]string, error) {
	// Get all the networks to which the pod is attached
	podNetworks, err := c.getPodKubeovnNets(pod)
	if err != nil {
		return nil, fmt.Errorf("failed to get pod networks: %w", err)
	}

	// Retrieve all the providers
	var providers []string
	for _, podNetwork := range podNetworks {
		providers = append(providers, podNetwork.ProviderName)
	}

	return providers, nil
}

// getMatchingProviderForAddress returns the provider linked to a subnet in which a particular address is present
func getMatchingProviderForAddress(pod *v1.Pod, providers []string, address string) string {
	if pod.Annotations == nil {
		return ""
	}

	// Find which provider is linked to this address
	for _, provider := range providers {
		ipsForProvider, exists := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)]
		if !exists {
			continue
		}

		ips := strings.Split(ipsForProvider, ",")
		if slices.Contains(ips, address) {
			return provider
		}
	}

	return ""
}

// getEndpointProvider returns the provider linked to the addresses of an endpoint
func (c *Controller) getEndpointProvider(pod *v1.Pod, addresses []string) (string, error) {
	// Retrieve all the providers of the pod
	providers, err := c.getPodProviders(pod)
	if err != nil {
		return "", err
	}

	// Get the first matching provider for any of the address in the endpoint
	var provider string
	for _, address := range addresses {
		if provider = getMatchingProviderForAddress(pod, providers, address); provider != "" {
			return provider, nil
		}
	}

	return "", nil
}

// getEndpointTargetLSPNameFromProvider returns the name of the LSP for a pod targeted by an endpoint.
// A custom provider can be specified if the LSP is within a subnet that doesn't use
// the default "ovn" provider.
func getEndpointTargetLSPNameFromProvider(pod *v1.Pod, provider string) string {
	// If no provider is specified, use the default one
	if provider == "" {
		provider = util.OvnProvider
	}

	target := pod.Name

	// If this pod is a VM launcher pod, we need to retrieve the name of the VM. This is necessary
	// because we do not use the same syntax for the LSP of normal pods and for VM pods
	if vmName, exists := pod.Annotations[fmt.Sprintf(util.VMAnnotationTemplate, provider)]; exists {
		target = vmName
	}

	return ovs.PodNameToPortName(target, pod.Namespace, provider)
}

// getEndpointTargetLSP returns the name of the LSP on which addresses are attached for a specific pod
func (c *Controller) getEndpointTargetLSPName(pod *v1.Pod, addresses []string) (string, error) {
	// Retrieve the provider for those addresses
	provider, err := c.getEndpointProvider(pod, addresses)
	if err != nil {
		return "", err
	}

	return getEndpointTargetLSPNameFromProvider(pod, provider), nil
}

// getSubnetByProvider returns the subnet linked to a provider on a pod
func getSubnetByProvider(pod *v1.Pod, provider string) (string, error) {
	subnetName, exists := pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider)]
	if !exists {
		return "", fmt.Errorf("couldn't find subnet linked to provider %s", provider)
	}

	return subnetName, nil
}

// getVpcByProvider returns the VPC linked to a provider on a pod.
// For underlay subnets without LogicalGateway or U2OInterconnection,
// the logical_router annotation is not set, so an empty string is returned.
func getVpcByProvider(pod *v1.Pod, provider string) string {
	return pod.Annotations[fmt.Sprintf(util.LogicalRouterAnnotationTemplate, provider)]
}

// getEndpointVpcAndSubnet returns the VPC/subnet for a pod and a set of addresses attached to it
func (c *Controller) getEndpointVpcAndSubnet(pod *v1.Pod, addresses []string) (string, string, error) {
	// Retrieve the provider for those addresses
	provider, err := c.getEndpointProvider(pod, addresses)
	if err != nil {
		return "", "", err
	}

	if provider == "" {
		return "", "", nil
	}

	// Retrieve the subnet
	subnet, err := getSubnetByProvider(pod, provider)
	if err != nil {
		return "", "", err
	}

	// Retrieve the VPC
	vpc := getVpcByProvider(pod, provider)

	return vpc, subnet, nil
}
