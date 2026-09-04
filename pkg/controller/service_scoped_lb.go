package controller

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"

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

func serviceUsesScopedLB(svc *v1.Service) bool {
	return svc.Spec.SessionAffinity == v1.ServiceAffinityClientIP || serviceUsesDistributedLB(svc)
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
	identity := fmt.Sprintf("%s/%s/%s", svc.Namespace, svc.Name, svc.UID)
	identityHash := util.Sha256Hash([]byte(identity))[:12]
	return fmt.Sprintf("svc-%s-%s", identityHash, strings.ToLower(string(protocol)))
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
	name := serviceScopedLBName(svc, protocol)
	timeout, err := serviceSessionAffinityTimeout(svc)
	if err != nil {
		return "", err
	}
	if serviceUsesDistributedLB(svc) && !c.config.EnableOVNLBDistributed {
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
	}
	if err := c.OVNNbClient.SetLoadBalancerDistributed(name, serviceUsesDistributedLB(svc)); err != nil {
		return "", fmt.Errorf("set distributed mode on service-scoped load balancer %s: %w", name, err)
	}
	return name, nil
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
	return nil
}

func serviceScopedLBNames(svc *v1.Service) []string {
	names := make([]string, 0, len(svc.Spec.Ports))
	seen := make(map[string]struct{}, len(svc.Spec.Ports))
	for _, port := range svc.Spec.Ports {
		name := serviceScopedLBName(svc, port.Protocol)
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}
