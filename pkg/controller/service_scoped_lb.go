package controller

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	serviceLBOwnerExternalID = "kube-ovn.io/lb-owner-uid"
	serviceLBOwnerKindID     = "kube-ovn.io/lb-owner-kind"
	serviceLBNamespaceID     = "kube-ovn.io/lb-owner-namespace"
	serviceLBNameExternalID  = "kube-ovn.io/lb-owner-name"
	serviceLBVPCExternalID   = "kube-ovn.io/lb-vpc"
	serviceLBScopeExternalID = "kube-ovn.io/lb-attachment-scope"
	serviceLBVersionID       = "kube-ovn.io/service-lb-version"
	serviceLBVersion         = "v1"
	maxOVNLBSessionTimeout   = 65535
	serviceTemplateVarRoot   = "kube_ovn_svc_"

	serviceLBOwnerKindAnnotation = "kube-ovn.io/lb-owner-kind"
	serviceLBOwnerNameAnnotation = "kube-ovn.io/lb-owner-name"
	serviceLBOwnerUIDAnnotation  = "kube-ovn.io/lb-owner-uid"
	serviceLBOwnerKind           = "service"
	switchLBRuleLBOwnerKind      = "switchlbrule"
	routerLBRuleLBOwnerKind      = "routerlbrule"
)

type serviceLBOwner struct {
	kind      string
	namespace string
	name      string
	uid       string
}

type serviceLBTrafficClass string

const (
	serviceLBInternalTraffic serviceLBTrafficClass = "internal"
	serviceLBExternalTraffic serviceLBTrafficClass = "external"
)

func serviceUsesScopedLB(svc *v1.Service) bool {
	if svc.Annotations[util.SwitchLBRuleVipsAnnotation] != "" || svc.Annotations[util.RouterLBRuleVipsAnnotation] != "" {
		return true
	}
	return svc.Spec.Type != v1.ServiceTypeExternalName && svc.Spec.ClusterIP != v1.ClusterIPNone
}

func serviceScopedLBOwner(svc *v1.Service) serviceLBOwner {
	owner := serviceLBOwner{
		kind:      serviceLBOwnerKind,
		namespace: svc.Namespace,
		name:      svc.Name,
		uid:       string(svc.UID),
	}
	if kind := svc.Annotations[serviceLBOwnerKindAnnotation]; kind != "" {
		owner.kind = kind
	}
	if name := svc.Annotations[serviceLBOwnerNameAnnotation]; name != "" {
		owner.name = name
	}
	if uid := svc.Annotations[serviceLBOwnerUIDAnnotation]; uid != "" {
		owner.uid = uid
	}
	return owner
}

func setServiceScopedLBOwner(svc *v1.Service, kind, name, uid string) {
	if svc.Annotations == nil {
		svc.Annotations = make(map[string]string)
	}
	svc.Annotations[serviceLBOwnerKindAnnotation] = kind
	svc.Annotations[serviceLBOwnerNameAnnotation] = name
	svc.Annotations[serviceLBOwnerUIDAnnotation] = uid
}

func serviceOwnsScopedLB(svc *v1.Service, lb *ovnnb.LoadBalancer) bool {
	owner := serviceScopedLBOwner(svc)
	return owner.uid != "" && lb.ExternalIDs[serviceLBOwnerExternalID] == owner.uid &&
		lb.ExternalIDs[serviceLBOwnerKindID] == owner.kind &&
		lb.ExternalIDs[serviceLBNamespaceID] == owner.namespace &&
		lb.ExternalIDs[serviceLBNameExternalID] == owner.name &&
		lb.ExternalIDs[serviceLBVersionID] == serviceLBVersion
}

func serviceUsesDistributedLB(svc *v1.Service) bool {
	return svc.Spec.InternalTrafficPolicy != nil && *svc.Spec.InternalTrafficPolicy == v1.ServiceInternalTrafficPolicyLocal
}

func serviceUsesTemplateLB(svc *v1.Service) bool {
	return serviceUsesTrafficDistribution(svc) && !serviceUsesDistributedLB(svc)
}

func serviceUsesTrafficDistribution(svc *v1.Service) bool {
	return svc.Spec.Type == v1.ServiceTypeClusterIP && svc.Spec.TrafficDistribution != nil &&
		svc.Annotations[util.SwitchLBRuleVipsAnnotation] == "" &&
		svc.Annotations[util.RouterLBRuleVipsAnnotation] == ""
}

func serviceSessionAffinityTimeout(svc *v1.Service) (int, error) {
	if svc.Spec.SessionAffinity != v1.ServiceAffinityClientIP {
		return util.DefaultServiceSessionStickinessTimeout, nil
	}

	timeout := util.DefaultServiceSessionStickinessTimeout
	if config := svc.Spec.SessionAffinityConfig; config != nil && config.ClientIP != nil && config.ClientIP.TimeoutSeconds != nil {
		timeout = int(*config.ClientIP.TimeoutSeconds)
	}
	if timeout < 1 || timeout > maxOVNLBSessionTimeout {
		return 0, fmt.Errorf("service %s/%s session affinity timeout %d is outside OVN's supported range [1, %d] seconds", svc.Namespace, svc.Name, timeout, maxOVNLBSessionTimeout)
	}
	return timeout, nil
}

func serviceScopedLBName(svc *v1.Service, protocol v1.Protocol) string {
	return serviceScopedLBNameForTrafficClassAndFamily(svc, protocol, serviceLBInternalTraffic, "")
}

func serviceScopedLBNameForTrafficClass(svc *v1.Service, protocol v1.Protocol, trafficClass serviceLBTrafficClass) string {
	owner := serviceScopedLBOwner(svc)
	name := fmt.Sprintf("%s:%s/%s:%s", owner.kind, owner.namespace, owner.name, strings.ToLower(string(protocol)))
	return name + ":" + string(trafficClass)
}

func serviceScopedLBNameForTrafficClassAndFamily(svc *v1.Service, protocol v1.Protocol, trafficClass serviceLBTrafficClass, family string) string {
	name := serviceScopedLBNameForTrafficClass(svc, protocol, trafficClass)
	if family == "" {
		return name
	}
	return name + ":" + strings.ToLower(family)
}

func serviceTemplateLBAddressFamilies(svc *v1.Service) []string {
	families := make([]string, 0, len(svc.Spec.ClusterIPs))
	seen := make(map[string]struct{}, len(svc.Spec.ClusterIPs))
	for _, clusterIP := range util.ServiceClusterIPs(*svc) {
		family := strings.ToLower(util.CheckProtocol(clusterIP))
		if family == "" {
			continue
		}
		if _, ok := seen[family]; ok {
			continue
		}
		seen[family] = struct{}{}
		families = append(families, family)
	}
	return families
}

func serviceScopedLBExternalIDs(svc *v1.Service, vpcName string, trafficClass serviceLBTrafficClass) map[string]string {
	owner := serviceScopedLBOwner(svc)
	return map[string]string{
		"vendor":                 util.CniTypeName,
		serviceLBOwnerExternalID: owner.uid,
		serviceLBOwnerKindID:     owner.kind,
		serviceLBNamespaceID:     owner.namespace,
		serviceLBNameExternalID:  owner.name,
		serviceLBVPCExternalID:   vpcName,
		serviceLBScopeExternalID: serviceScopedLBAttachmentScope(svc, trafficClass),
		serviceLBVersionID:       serviceLBVersion,
	}
}

func serviceScopedLBAttachmentScope(svc *v1.Service, trafficClass serviceLBTrafficClass) string {
	switch serviceScopedLBOwner(svc).kind {
	case switchLBRuleLBOwnerKind:
		return "logical-switch/external"
	case routerLBRuleLBOwnerKind:
		return "logical-router/external"
	default:
		return "logical-switch/" + string(trafficClass)
	}
}

func vpcLoadBalancerNames(vpc *kubeovnv1.Vpc) (map[v1.Protocol]string, map[v1.Protocol]string) {
	regular := map[v1.Protocol]string{
		v1.ProtocolTCP:  vpc.Status.TCPLoadBalancer,
		v1.ProtocolUDP:  vpc.Status.UDPLoadBalancer,
		v1.ProtocolSCTP: vpc.Status.SctpLoadBalancer,
	}
	session := map[v1.Protocol]string{
		v1.ProtocolTCP:  vpc.Status.TCPSessionLoadBalancer,
		v1.ProtocolUDP:  vpc.Status.UDPSessionLoadBalancer,
		v1.ProtocolSCTP: vpc.Status.SctpSessionLoadBalancer,
	}
	return regular, session
}

func (c *Controller) legacyVpcLoadBalancerNames(vpc *kubeovnv1.Vpc) (map[v1.Protocol]string, map[v1.Protocol]string) {
	regular, session := vpcLoadBalancerNames(vpc)
	expected := c.GenVpcLoadBalancer(vpc.Name)
	expectedRegular := map[v1.Protocol]string{
		v1.ProtocolTCP:  expected.TCPLoadBalancer,
		v1.ProtocolUDP:  expected.UDPLoadBalancer,
		v1.ProtocolSCTP: expected.SctpLoadBalancer,
	}
	expectedSession := map[v1.Protocol]string{
		v1.ProtocolTCP:  expected.TCPSessLoadBalancer,
		v1.ProtocolUDP:  expected.UDPSessLoadBalancer,
		v1.ProtocolSCTP: expected.SctpSessLoadBalancer,
	}
	for protocol, name := range regular {
		if name != expectedRegular[protocol] {
			regular[protocol] = ""
		}
	}
	for protocol, name := range session {
		if name != expectedSession[protocol] {
			session[protocol] = ""
		}
	}
	return regular, session
}

func (c *Controller) ensureServiceScopedLB(svc *v1.Service, protocol v1.Protocol, family string) (string, error) {
	return c.ensureServiceScopedLBForTrafficClass(svc, protocol, serviceLBInternalTraffic, family)
}

func (c *Controller) ensureServiceScopedLBForTrafficClass(svc *v1.Service, protocol v1.Protocol, trafficClass serviceLBTrafficClass, family string) (string, error) {
	name := serviceScopedLBNameForTrafficClassAndFamily(svc, protocol, trafficClass, family)
	vpcName := serviceVPCName(svc, c.config.ClusterRouter)
	timeout, err := serviceSessionAffinityTimeout(svc)
	if err != nil {
		return "", err
	}
	distributed := trafficClass == serviceLBInternalTraffic && serviceUsesDistributedLB(svc)
	if distributed && !c.config.EnableOVNLBDistributed {
		return "", fmt.Errorf("service %s/%s uses internalTrafficPolicy=Local but OVN distributed load balancers are disabled; enable --enable-ovn-lb-distributed with OVN 26.03+", svc.Namespace, svc.Name)
	}
	selectFields := []string(nil)
	if svc.Spec.SessionAffinity == v1.ServiceAffinityClientIP {
		selectFields = []string{ovnnb.LoadBalancerSelectionFieldsIPSrc, ovnnb.LoadBalancerSelectionFieldsIpv6Src}
	}
	if err := c.OVNNbClient.CreateLoadBalancer(name, strings.ToLower(string(protocol)), selectFields...); err != nil {
		return "", fmt.Errorf("create service-scoped load balancer %s: %w", name, err)
	}
	if err := c.OVNNbClient.SetLoadBalancerSelectionFields(name, selectFields); err != nil {
		return "", fmt.Errorf("set selection fields on service-scoped load balancer %s: %w", name, err)
	}
	if err := c.OVNNbClient.SetLoadBalancerExternalIDs(name, serviceScopedLBExternalIDs(svc, vpcName, trafficClass)); err != nil {
		return "", fmt.Errorf("set ownership metadata on service-scoped load balancer %s: %w", name, err)
	}
	if svc.Spec.SessionAffinity == v1.ServiceAffinityClientIP {
		if err := c.OVNNbClient.SetLoadBalancerAffinityTimeout(name, timeout); err != nil {
			return "", fmt.Errorf("set affinity timeout on service-scoped load balancer %s: %w", name, err)
		}
	} else if err := c.OVNNbClient.DeleteLoadBalancerAffinityTimeout(name); err != nil {
		return "", fmt.Errorf("delete affinity timeout on service-scoped load balancer %s: %w", name, err)
	}
	if err := c.OVNNbClient.SetLoadBalancerDistributed(name, distributed); err != nil {
		return "", fmt.Errorf("set distributed mode on service-scoped load balancer %s: %w", name, err)
	}
	if serviceUsesTemplateLB(svc) && trafficClass == serviceLBInternalTraffic {
		if err := c.OVNNbClient.SetLoadBalancerTemplate(name, true); err != nil {
			return "", fmt.Errorf("set template mode on service-scoped load balancer %s: %w", name, err)
		}
		if err := c.OVNNbClient.SetLoadBalancerAddressFamily(name, family); err != nil {
			return "", fmt.Errorf("set address family on service-scoped load balancer %s: %w", name, err)
		}
	} else if distributed {
		if err := c.OVNNbClient.SetLoadBalancerTemplate(name, false); err != nil {
			return "", fmt.Errorf("clear template mode on distributed service-scoped load balancer %s: %w", name, err)
		}
	}
	return name, nil
}

func (c *Controller) ensureServiceScopedLBExternalTraffic(svc *v1.Service, protocol v1.Protocol) (string, error) {
	lb, err := c.ensureServiceScopedLBForTrafficClass(svc, protocol, serviceLBExternalTraffic, "")
	if err != nil {
		return "", err
	}
	if serviceUsesTrafficDistribution(svc) {
		if err := c.OVNNbClient.SetLoadBalancerTemplate(lb, false); err != nil {
			return "", fmt.Errorf("disable template mode on external service-scoped load balancer %s: %w", lb, err)
		}
	}
	return lb, nil
}

func serviceVPCName(svc *v1.Service, defaultVPC string) string {
	if name := svc.Annotations[util.VpcAnnotation]; name != "" {
		return name
	}
	if name := svc.Annotations[util.LogicalRouterAnnotation]; name != "" {
		return name
	}
	return defaultVPC
}

func (c *Controller) serviceScopedLoadBalancerNamesForVPC(vpcName string) ([]string, error) {
	services, err := c.servicesLister.Services(v1.NamespaceAll).List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list services for scoped load balancers in vpc %s: %w", vpcName, err)
	}
	names := make(map[string]struct{})
	for _, svc := range services {
		ownerKind := serviceScopedLBOwner(svc).kind
		if !serviceUsesScopedLB(svc) || serviceVPCName(svc, c.config.ClusterRouter) != vpcName {
			continue
		}
		if ownerKind != serviceLBOwnerKind && ownerKind != routerLBRuleLBOwnerKind {
			continue
		}
		for _, name := range serviceScopedLBNames(svc) {
			names[name] = struct{}{}
		}
	}
	return slices.Sorted(maps.Keys(names)), nil
}

func (c *Controller) reconcileServiceScopedLoadBalancerAttachments(vpcName string, lbNames ...string) error {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list subnets for service-scoped load balancers in vpc %s: %w", vpcName, err)
	}
	insertSwitches := make([]string, 0, len(subnets))
	deleteSwitches := make([]string, 0, len(subnets))
	for _, subnet := range subnets {
		if subnet.Name == c.config.NodeSwitch || !isOvnSubnet(subnet) {
			continue
		}
		if subnet.Spec.Vpc == vpcName && subnet.Spec.EnableLb != nil && *subnet.Spec.EnableLb {
			insertSwitches = append(insertSwitches, subnet.Name)
		} else {
			deleteSwitches = append(deleteSwitches, subnet.Name)
		}
	}
	slices.Sort(insertSwitches)
	slices.Sort(deleteSwitches)
	for _, logicalSwitch := range insertSwitches {
		if err := c.OVNNbClient.LogicalSwitchUpdateLoadBalancers(logicalSwitch, ovsdb.MutateOperationInsert, lbNames...); err != nil {
			return fmt.Errorf("attach service-scoped load balancers to subnet %s: %w", logicalSwitch, err)
		}
	}
	for _, logicalSwitch := range deleteSwitches {
		if err := c.OVNNbClient.LogicalSwitchUpdateLoadBalancers(logicalSwitch, ovsdb.MutateOperationDelete, lbNames...); err != nil {
			return fmt.Errorf("detach service-scoped load balancers from subnet %s: %w", logicalSwitch, err)
		}
	}
	return nil
}

func (c *Controller) reconcileResourceScopedLoadBalancerAttachments(svc *v1.Service, vpcName, subnetName string, lbNames ...string) error {
	switch serviceScopedLBOwner(svc).kind {
	case switchLBRuleLBOwnerKind:
		logicalSwitch := svc.Annotations[util.LogicalSwitchAnnotation]
		if logicalSwitch == "" {
			logicalSwitch = subnetName
		}
		if logicalSwitch == "" {
			return fmt.Errorf("switch load balancer rule service %s/%s has no logical switch annotation", svc.Namespace, svc.Name)
		}
		if err := c.OVNNbClient.LogicalSwitchUpdateLoadBalancers(logicalSwitch, ovsdb.MutateOperationInsert, lbNames...); err != nil {
			return fmt.Errorf("attach resource-scoped load balancers to logical switch %s: %w", logicalSwitch, err)
		}
		subnets, err := c.subnetsLister.List(labels.Everything())
		if err != nil {
			return fmt.Errorf("list logical switches for resource-scoped load balancer attachment: %w", err)
		}
		for _, subnet := range subnets {
			if subnet.Name == logicalSwitch || !isOvnSubnet(subnet) {
				continue
			}
			if err := c.OVNNbClient.LogicalSwitchUpdateLoadBalancers(subnet.Name, ovsdb.MutateOperationDelete, lbNames...); err != nil {
				return fmt.Errorf("detach resource-scoped load balancers from logical switch %s: %w", subnet.Name, err)
			}
		}
		return nil
	case routerLBRuleLBOwnerKind:
		if vpcName == "" {
			return fmt.Errorf("router load balancer rule service %s/%s has no logical router annotation", svc.Namespace, svc.Name)
		}
		if err := c.OVNNbClient.LogicalRouterUpdateLoadBalancers(vpcName, ovsdb.MutateOperationInsert, lbNames...); err != nil {
			return fmt.Errorf("attach resource-scoped load balancers to logical router %s: %w", vpcName, err)
		}
		vpcs, err := c.vpcsLister.List(labels.Everything())
		if err != nil {
			return fmt.Errorf("list logical routers for resource-scoped load balancer attachment: %w", err)
		}
		for _, vpc := range vpcs {
			if vpc.Name == vpcName {
				continue
			}
			if err := c.OVNNbClient.LogicalRouterUpdateLoadBalancers(vpc.Name, ovsdb.MutateOperationDelete, lbNames...); err != nil {
				return fmt.Errorf("detach resource-scoped load balancers from logical router %s: %w", vpc.Name, err)
			}
		}
		// OVN health checks originate in the backend logical-switch datapath.
		// Keep the RLR load balancer attached to the router for external traffic,
		// and to the VPC switches so health-check traffic can reach its backends.
		return c.reconcileServiceScopedLoadBalancerAttachments(vpcName, lbNames...)
	default:
		return c.reconcileServiceScopedLoadBalancerAttachments(vpcName, lbNames...)
	}
}

func (c *Controller) deleteServiceScopedLoadBalancers(svc *v1.Service) error {
	owner := serviceScopedLBOwner(svc)
	if owner.uid == "" {
		return nil
	}
	if err := c.OVNNbClient.DeleteLoadBalancers(func(lb *ovnnb.LoadBalancer) bool {
		return lb.ExternalIDs[serviceLBOwnerExternalID] == owner.uid &&
			lb.ExternalIDs[serviceLBOwnerKindID] == owner.kind &&
			lb.ExternalIDs[serviceLBVersionID] == serviceLBVersion
	}); err != nil {
		return fmt.Errorf("delete service-scoped load balancers for %s/%s: %w", svc.Namespace, svc.Name, err)
	}
	if serviceUsesTrafficDistribution(svc) {
		if err := c.cleanupServiceTrafficDistributionState(svc); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) deleteServiceScopedLBTrafficClass(svc *v1.Service, protocol v1.Protocol, trafficClass serviceLBTrafficClass) error {
	owner := serviceScopedLBOwner(svc)
	if owner.uid == "" {
		return nil
	}
	name := serviceScopedLBNameForTrafficClass(svc, protocol, trafficClass)
	if err := c.OVNNbClient.DeleteLoadBalancers(func(lb *ovnnb.LoadBalancer) bool {
		return lb.Name == name && lb.ExternalIDs[serviceLBOwnerExternalID] == owner.uid &&
			lb.ExternalIDs[serviceLBOwnerKindID] == owner.kind &&
			lb.ExternalIDs[serviceLBVersionID] == serviceLBVersion
	}); err != nil {
		return fmt.Errorf("delete %s service-scoped load balancer for %s/%s: %w", trafficClass, svc.Namespace, svc.Name, err)
	}
	return nil
}

func (c *Controller) deleteServiceScopedLBExternalTraffic(svc *v1.Service) error {
	protocols := make(map[v1.Protocol]struct{}, len(svc.Spec.Ports))
	for _, port := range svc.Spec.Ports {
		protocols[port.Protocol] = struct{}{}
	}
	for protocol := range protocols {
		if err := c.deleteServiceScopedLBTrafficClass(svc, protocol, serviceLBExternalTraffic); err != nil {
			return err
		}
	}
	return nil
}

func serviceLBTrafficClassForVIP(classes map[string]serviceLBTrafficClass, vip string) serviceLBTrafficClass {
	if classes[vip] == serviceLBExternalTraffic {
		return serviceLBExternalTraffic
	}
	return serviceLBInternalTraffic
}

func (c *Controller) serviceLBMigrationCandidates(svc *v1.Service, protocol v1.Protocol, vpc *kubeovnv1.Vpc, trafficClass serviceLBTrafficClass) []string {
	candidates := make([]string, 0, 6)
	seen := make(map[string]struct{}, 6)
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		candidates = append(candidates, name)
	}
	regular, session := c.legacyVpcLoadBalancerNames(vpc)
	add(regular[protocol])
	add(session[protocol])
	if serviceUsesScopedLB(svc) {
		add(serviceScopedLBNameForTrafficClass(svc, protocol, trafficClass))
		if trafficClass == serviceLBInternalTraffic && serviceUsesTemplateLB(svc) {
			for _, family := range serviceTemplateLBAddressFamilies(svc) {
				add(serviceScopedLBNameForTrafficClassAndFamily(svc, protocol, trafficClass, family))
			}
		}
	}
	return candidates
}

func (c *Controller) cleanupLegacyVpcLoadBalancers(vpc *kubeovnv1.Vpc) error {
	regular, session := c.legacyVpcLoadBalancerNames(vpc)
	statusFields := map[string]string{
		"tcpLoadBalancer":         regular[v1.ProtocolTCP],
		"udpLoadBalancer":         regular[v1.ProtocolUDP],
		"sctpLoadBalancer":        regular[v1.ProtocolSCTP],
		"tcpSessionLoadBalancer":  session[v1.ProtocolTCP],
		"udpSessionLoadBalancer":  session[v1.ProtocolUDP],
		"sctpSessionLoadBalancer": session[v1.ProtocolSCTP],
	}
	cleared := make(map[string]string)
	for field, name := range statusFields {
		if name == "" {
			continue
		}
		lb, err := c.OVNNbClient.GetLoadBalancer(name, true)
		if err != nil {
			return fmt.Errorf("get legacy vpc load balancer %s: %w", name, err)
		}
		if lb != nil && len(lb.Vips) != 0 {
			continue
		}
		if lb != nil {
			if err := c.OVNNbClient.DeleteLoadBalancers(func(candidate *ovnnb.LoadBalancer) bool {
				return candidate.Name == name
			}); err != nil {
				return fmt.Errorf("delete empty legacy vpc load balancer %s: %w", name, err)
			}
		}
		cleared[field] = ""
	}
	if len(cleared) == 0 {
		return nil
	}
	body, err := jsonv2.Marshal(map[string]any{"status": cleared})
	if err != nil {
		return fmt.Errorf("marshal legacy load balancer status patch for vpc %s: %w", vpc.Name, err)
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().Vpcs().Patch(context.Background(), vpc.Name, types.MergePatchType, body, metav1.PatchOptions{}, "status"); err != nil {
		return fmt.Errorf("clear legacy load balancer status for vpc %s: %w", vpc.Name, err)
	}
	return nil
}

func (c *Controller) deleteLegacyVpcVIPs(vpcName string, vips []string) error {
	if vpcName == "" || len(vips) == 0 {
		return nil
	}
	vpc, err := c.vpcsLister.Get(vpcName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get vpc %s for legacy load balancer cleanup: %w", vpcName, err)
	}
	regular, session := c.legacyVpcLoadBalancerNames(vpc)
	seen := make(map[string]struct{}, len(regular)+len(session))
	for _, loadBalancers := range []map[v1.Protocol]string{regular, session} {
		for _, name := range loadBalancers {
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			for _, vip := range vips {
				if err := c.OVNNbClient.LoadBalancerDeleteVip(name, vip, true); err != nil && !k8serrors.IsNotFound(err) {
					return fmt.Errorf("delete vip %s from legacy vpc load balancer %s: %w", vip, name, err)
				}
			}
		}
	}
	return nil
}

func (c *Controller) cleanupServiceScopedLBVIPs(svc *v1.Service, desired map[string]map[string]struct{}) error {
	owner := serviceScopedLBOwner(svc)
	lbs, err := c.OVNNbClient.ListLoadBalancers(func(lb *ovnnb.LoadBalancer) bool {
		return lb.ExternalIDs[serviceLBOwnerExternalID] == owner.uid &&
			lb.ExternalIDs[serviceLBOwnerKindID] == owner.kind &&
			lb.ExternalIDs[serviceLBVersionID] == serviceLBVersion
	})
	if err != nil {
		return fmt.Errorf("list service-scoped load balancers for %s/%s: %w", svc.Namespace, svc.Name, err)
	}
	for _, lb := range lbs {
		for vip := range lb.Vips {
			if strings.HasPrefix(vip, "^"+serviceTemplateVarRoot) {
				continue
			}
			if _, ok := desired[lb.Name][vip]; ok {
				continue
			}
			if err := c.OVNNbClient.LoadBalancerDeleteVip(lb.Name, vip, true); err != nil {
				return fmt.Errorf("delete stale vip %s from service-scoped load balancer %s: %w", vip, lb.Name, err)
			}
			if err := c.OVNNbClient.LoadBalancerDeleteIPPortMapping(lb.Name, vip); err != nil {
				return fmt.Errorf("delete stale vip %s mapping from service-scoped load balancer %s: %w", vip, lb.Name, err)
			}
		}
	}
	return nil
}

func (c *Controller) gcServiceTrafficDistributionVariables(services []*v1.Service) error {
	activePrefixes := make(map[string]struct{}, len(services))
	for _, svc := range services {
		if serviceUsesTemplateLB(svc) {
			activePrefixes[serviceTrafficDistributionVariablePrefix(svc)] = struct{}{}
		}
	}
	if err := c.OVNNbClient.DeleteChassisTemplateVariables(func(name string) bool {
		if !strings.HasPrefix(name, serviceTemplateVarRoot) {
			return false
		}
		for prefix := range activePrefixes {
			if strings.HasPrefix(name, prefix) {
				return false
			}
		}
		return true
	}); err != nil {
		return fmt.Errorf("garbage collect service traffic distribution variables: %w", err)
	}
	return nil
}

func (c *Controller) deleteServiceLBMigrationVIP(svc *v1.Service, protocol v1.Protocol, currentLB, vip string, vpc *kubeovnv1.Vpc, trafficClass serviceLBTrafficClass) error {
	seen := make(map[string]struct{})
	for _, legacyLB := range c.serviceLBMigrationCandidates(svc, protocol, vpc, trafficClass) {
		if legacyLB == "" || legacyLB == currentLB {
			continue
		}
		if _, ok := seen[legacyLB]; ok {
			continue
		}
		seen[legacyLB] = struct{}{}
		exists, err := c.OVNNbClient.LoadBalancerExists(legacyLB)
		if err != nil {
			return fmt.Errorf("check old load balancer %s for vip %s: %w", legacyLB, vip, err)
		}
		if !exists {
			continue
		}
		if err := c.OVNNbClient.LoadBalancerDeleteVip(legacyLB, vip, true); err != nil {
			return fmt.Errorf("delete vip %s from old load balancer %s: %w", vip, legacyLB, err)
		}
	}
	return nil
}

func serviceScopedLBNames(svc *v1.Service) []string {
	names := make([]string, 0, len(svc.Spec.Ports)*2)
	seen := make(map[string]struct{}, len(svc.Spec.Ports))
	ownerKind := serviceScopedLBOwner(svc).kind
	for _, port := range svc.Spec.Ports {
		trafficClasses := []serviceLBTrafficClass{serviceLBInternalTraffic}
		if ownerKind == switchLBRuleLBOwnerKind || ownerKind == routerLBRuleLBOwnerKind {
			trafficClasses = []serviceLBTrafficClass{serviceLBExternalTraffic}
		} else if svc.Spec.Type == v1.ServiceTypeLoadBalancer || serviceUsesDistributedLB(svc) {
			trafficClasses = append(trafficClasses, serviceLBExternalTraffic)
		}
		for _, trafficClass := range trafficClasses {
			families := []string{""}
			if trafficClass == serviceLBInternalTraffic && serviceUsesTemplateLB(svc) {
				families = serviceTemplateLBAddressFamilies(svc)
			}
			for _, family := range families {
				name := serviceScopedLBNameForTrafficClassAndFamily(svc, port.Protocol, trafficClass, family)
				if _, ok := seen[name]; ok {
					continue
				}
				seen[name] = struct{}{}
				names = append(names, name)
			}
		}
	}
	return names
}
