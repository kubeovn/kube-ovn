package controller

import (
	"fmt"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	v1 "k8s.io/api/core/v1"

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
)

type serviceLBTrafficClass string

const (
	serviceLBInternalTraffic serviceLBTrafficClass = "internal"
	serviceLBExternalTraffic serviceLBTrafficClass = "external"
)

func serviceUsesScopedLB(svc *v1.Service) bool {
	return svc.Spec.SessionAffinity == v1.ServiceAffinityClientIP || serviceUsesDistributedLB(svc) || svc.Spec.TrafficDistribution != nil
}

func serviceUsesDistributedLB(svc *v1.Service) bool {
	return svc.Spec.InternalTrafficPolicy != nil && *svc.Spec.InternalTrafficPolicy == v1.ServiceInternalTrafficPolicyLocal
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

func serviceScopedLBExternalIDs(svc *v1.Service) map[string]string {
	return map[string]string{
		"vendor":                 util.CniTypeName,
		serviceLBOwnerExternalID: string(svc.UID),
		serviceLBNamespaceID:     svc.Namespace,
		serviceLBNameExternalID:  svc.Name,
		serviceLBVersionID:       serviceLBVersion,
	}
}

func (c *Controller) ensureServiceScopedLB(svc *v1.Service, protocol v1.Protocol) (string, error) {
	return c.ensureServiceScopedLBForTrafficClass(svc, protocol, serviceLBInternalTraffic)
}

func (c *Controller) ensureServiceScopedLBForTrafficClass(svc *v1.Service, protocol v1.Protocol, trafficClass serviceLBTrafficClass) (string, error) {
	name := serviceScopedLBNameForTrafficClass(svc, protocol, trafficClass)
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
	if svc.Spec.TrafficDistribution != nil {
		if err := c.OVNNbClient.SetLoadBalancerTemplate(name, true); err != nil {
			return "", fmt.Errorf("set template mode on service-scoped load balancer %s: %w", name, err)
		}
	}
	return name, nil
}

func (c *Controller) ensureServiceScopedLBExternalTraffic(svc *v1.Service, protocol v1.Protocol, subnetName string) (string, error) {
	lb, err := c.ensureServiceScopedLBForTrafficClass(svc, protocol, serviceLBExternalTraffic)
	if err != nil {
		return "", err
	}
	if svc.Spec.TrafficDistribution != nil {
		if err := c.OVNNbClient.SetLoadBalancerTemplate(lb, false); err != nil {
			return "", fmt.Errorf("disable template mode on external service-scoped load balancer %s: %w", lb, err)
		}
	}
	if err := c.OVNNbClient.LogicalSwitchUpdateLoadBalancers(subnetName, ovsdb.MutateOperationInsert, lb); err != nil {
		return "", fmt.Errorf("attach external service-scoped load balancer to subnet %s: %w", subnetName, err)
	}
	return lb, nil
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
	if svc.Spec.TrafficDistribution != nil && c.OVNSbClient != nil {
		chassises, err := c.OVNSbClient.ListChassis()
		if err != nil {
			return fmt.Errorf("list OVN chassis while deleting service %s/%s template variables: %w", svc.Namespace, svc.Name, err)
		}
		prefix := serviceTrafficDistributionVariablePrefix(svc)
		for _, chassis := range *chassises {
			if err := c.OVNNbClient.ReconcileChassisTemplateVariables(chassis.Name, prefix, nil); err != nil {
				return err
			}
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
	switch protocol {
	case v1.ProtocolTCP:
		candidates = append(candidates, vpc.Status.TCPLoadBalancer, vpc.Status.TCPSessionLoadBalancer)
	case v1.ProtocolUDP:
		candidates = append(candidates, vpc.Status.UDPLoadBalancer, vpc.Status.UDPSessionLoadBalancer)
	case v1.ProtocolSCTP:
		candidates = append(candidates, vpc.Status.SctpLoadBalancer, vpc.Status.SctpSessionLoadBalancer)
	}
	if serviceUsesScopedLB(svc) {
		candidates = append(candidates, serviceScopedLBNameForTrafficClass(svc, protocol, trafficClass))
	}
	return candidates
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
			name := serviceScopedLBNameForTrafficClass(svc, port.Protocol, trafficClass)
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	return names
}
