package controller

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	serviceLBOwnerExternalID = "kube-ovn.io/service-uid"
	serviceLBNamespaceID     = "kube-ovn.io/service-namespace"
	serviceLBNameExternalID  = "kube-ovn.io/service-name"
	serviceLBVersionID       = "kube-ovn.io/service-lb-version"
	serviceLBVersion         = "v1"
	maxOVNLBSessionTimeout   = 65535
	serviceTemplateVarRoot   = "kube_ovn_svc_"
)

type serviceLBTrafficClass string

const (
	serviceLBInternalTraffic serviceLBTrafficClass = "internal"
	serviceLBExternalTraffic serviceLBTrafficClass = "external"
)

func serviceUsesScopedLB(svc *v1.Service) bool {
	return svc.Spec.SessionAffinity == v1.ServiceAffinityClientIP || serviceUsesDistributedLB(svc) || serviceUsesTrafficDistribution(svc)
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
	return serviceScopedLBNameForTrafficClass(svc, protocol, serviceLBInternalTraffic)
}

func serviceScopedLBNameForTrafficClass(svc *v1.Service, protocol v1.Protocol, trafficClass serviceLBTrafficClass) string {
	identity := fmt.Sprintf("%s/%s/%s", svc.Namespace, svc.Name, svc.UID)
	identityHash := util.Sha256Hash([]byte(identity))[:12]
	name := fmt.Sprintf("svc-%s-%s", identityHash, strings.ToLower(string(protocol)))
	if trafficClass == serviceLBExternalTraffic {
		return name + "-external"
	}
	return name
}

func serviceScopedLBNameForTrafficClassAndFamily(svc *v1.Service, protocol v1.Protocol, trafficClass serviceLBTrafficClass, family string) string {
	name := serviceScopedLBNameForTrafficClass(svc, protocol, trafficClass)
	if family == "" {
		return name
	}
	return name + "-" + strings.ToLower(family)
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

func serviceScopedLBExternalIDs(svc *v1.Service) map[string]string {
	return map[string]string{
		"vendor":                 util.CniTypeName,
		serviceLBOwnerExternalID: string(svc.UID),
		serviceLBNamespaceID:     svc.Namespace,
		serviceLBNameExternalID:  svc.Name,
		serviceLBVersionID:       serviceLBVersion,
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

func (c *Controller) ensureServiceScopedLB(svc *v1.Service, protocol v1.Protocol, family string) (string, error) {
	return c.ensureServiceScopedLBForTrafficClass(svc, protocol, serviceLBInternalTraffic, family)
}

func (c *Controller) ensureServiceScopedLBForTrafficClass(svc *v1.Service, protocol v1.Protocol, trafficClass serviceLBTrafficClass, family string) (string, error) {
	name := serviceScopedLBNameForTrafficClassAndFamily(svc, protocol, trafficClass, family)
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
	if err := c.OVNNbClient.SetLoadBalancerExternalIDs(name, serviceScopedLBExternalIDs(svc)); err != nil {
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

func (c *Controller) ensureServiceScopedLBExternalTraffic(svc *v1.Service, protocol v1.Protocol, vpcName string) (string, error) {
	lb, err := c.ensureServiceScopedLBForTrafficClass(svc, protocol, serviceLBExternalTraffic, "")
	if err != nil {
		return "", err
	}
	if serviceUsesTrafficDistribution(svc) {
		if err := c.OVNNbClient.SetLoadBalancerTemplate(lb, false); err != nil {
			return "", fmt.Errorf("disable template mode on external service-scoped load balancer %s: %w", lb, err)
		}
	}
	if err := c.attachServiceScopedLoadBalancers(vpcName, lb); err != nil {
		return "", err
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
		if serviceUsesScopedLB(svc) && serviceVPCName(svc, c.config.ClusterRouter) == vpcName {
			for _, name := range serviceScopedLBNames(svc) {
				names[name] = struct{}{}
			}
		}
	}
	return slices.Sorted(maps.Keys(names)), nil
}

func (c *Controller) serviceScopedLoadBalancerSwitches(vpcName string) ([]string, error) {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list subnets for service-scoped load balancers in vpc %s: %w", vpcName, err)
	}
	switches := make([]string, 0, len(subnets))
	for _, subnet := range subnets {
		if subnet.Name == c.config.NodeSwitch || subnet.Spec.Vpc != vpcName || !isOvnSubnet(subnet) || subnet.Spec.EnableLb == nil || !*subnet.Spec.EnableLb {
			continue
		}
		switches = append(switches, subnet.Name)
	}
	slices.Sort(switches)
	return switches, nil
}

func (c *Controller) attachServiceScopedLoadBalancers(vpcName string, lbNames ...string) error {
	switches, err := c.serviceScopedLoadBalancerSwitches(vpcName)
	if err != nil {
		return err
	}
	for _, logicalSwitch := range switches {
		if err := c.OVNNbClient.LogicalSwitchUpdateLoadBalancers(logicalSwitch, ovsdb.MutateOperationInsert, lbNames...); err != nil {
			return fmt.Errorf("attach service-scoped load balancers to subnet %s: %w", logicalSwitch, err)
		}
	}
	return nil
}

func (c *Controller) deleteServiceScopedLoadBalancers(svc *v1.Service) error {
	owner := string(svc.UID)
	if owner == "" {
		return nil
	}
	if err := c.OVNNbClient.DeleteLoadBalancers(func(lb *ovnnb.LoadBalancer) bool {
		return lb.ExternalIDs[serviceLBOwnerExternalID] == owner && lb.ExternalIDs[serviceLBVersionID] == serviceLBVersion
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
	owner := string(svc.UID)
	if owner == "" {
		return nil
	}
	name := serviceScopedLBNameForTrafficClass(svc, protocol, trafficClass)
	if err := c.OVNNbClient.DeleteLoadBalancers(func(lb *ovnnb.LoadBalancer) bool {
		return lb.Name == name && lb.ExternalIDs[serviceLBOwnerExternalID] == owner && lb.ExternalIDs[serviceLBVersionID] == serviceLBVersion
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

func serviceLBMigrationCandidates(svc *v1.Service, protocol v1.Protocol, oldLB string, vpc *kubeovnv1.Vpc, trafficClass serviceLBTrafficClass) []string {
	candidates := []string{oldLB}
	regular, session := vpcLoadBalancerNames(vpc)
	candidates = append(candidates, regular[protocol], session[protocol])
	if serviceUsesScopedLB(svc) {
		candidates = append(candidates, serviceScopedLBNameForTrafficClass(svc, protocol, trafficClass))
		if trafficClass == serviceLBInternalTraffic && serviceUsesTemplateLB(svc) {
			for _, family := range serviceTemplateLBAddressFamilies(svc) {
				candidates = append(candidates, serviceScopedLBNameForTrafficClassAndFamily(svc, protocol, trafficClass, family))
			}
		}
	}
	return candidates
}

func (c *Controller) cleanupServiceScopedLBVIPs(svc *v1.Service, desired map[string]map[string]struct{}) error {
	lbs, err := c.OVNNbClient.ListLoadBalancers(func(lb *ovnnb.LoadBalancer) bool {
		return lb.ExternalIDs[serviceLBOwnerExternalID] == string(svc.UID) &&
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

func (c *Controller) deleteServiceLBMigrationVIP(svc *v1.Service, protocol v1.Protocol, oldLB, currentLB, vip string, vpc *kubeovnv1.Vpc, trafficClass serviceLBTrafficClass) error {
	seen := make(map[string]struct{})
	for _, legacyLB := range serviceLBMigrationCandidates(svc, protocol, oldLB, vpc, trafficClass) {
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
	for _, port := range svc.Spec.Ports {
		trafficClasses := []serviceLBTrafficClass{serviceLBInternalTraffic}
		if serviceUsesDistributedLB(svc) {
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
