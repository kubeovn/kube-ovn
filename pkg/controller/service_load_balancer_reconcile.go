package controller

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type resourceLoadBalancerClient interface {
	ReconcileLoadBalancers(ovs.LoadBalancerReconciliation) error
}

type serviceLoadBalancerReconciliation struct {
	batch   ovs.LoadBalancerReconciliation
	desired map[string]*ovs.LoadBalancerDesiredState
}

func newServiceLoadBalancerReconciliation(svc *v1.Service) *serviceLoadBalancerReconciliation {
	owner := serviceScopedLBOwner(svc)
	return &serviceLoadBalancerReconciliation{
		batch: ovs.LoadBalancerReconciliation{
			OwnerExternalIDs: map[string]string{
				serviceLBOwnerExternalID: owner.uid, serviceLBOwnerKindID: owner.kind,
				serviceLBNamespaceID: owner.namespace, serviceLBNameExternalID: owner.name,
				serviceLBVersionID: serviceLBVersion,
			},
			TemplateVariablePrefixes: []string{serviceTrafficDistributionVariablePrefix(svc), serviceExternalLocalTemplatePrefix(svc)},
		},
		desired: make(map[string]*ovs.LoadBalancerDesiredState),
	}
}

func (c *Controller) serviceScopedLBConfig(svc *v1.Service, protocol v1.Protocol, trafficClass serviceLBTrafficClass, family string) (ovs.LoadBalancerConfig, error) {
	timeout, err := serviceSessionAffinityTimeout(svc)
	if err != nil {
		return ovs.LoadBalancerConfig{}, err
	}
	config := ovs.LoadBalancerConfig{
		Name:            serviceScopedLBNameForTrafficClassAndFamily(svc, protocol, trafficClass, family),
		Protocol:        strings.ToLower(string(protocol)),
		SelectionFields: serviceScopedLBSelectionFields(svc, protocol, timeout, family),
		ExternalIDs:     serviceScopedLBExternalIDs(svc, serviceVPCName(svc, c.config.ClusterRouter), trafficClass),
		Options:         make(map[string]string),
	}
	if svc.Spec.SessionAffinity == v1.ServiceAffinityClientIP {
		config.Options["affinity_timeout"] = strconv.Itoa(timeout)
	} else {
		config.DeleteOptions = append(config.DeleteOptions, "affinity_timeout")
	}
	if trafficClass == serviceLBInternalTraffic && serviceUsesDistributedLB(svc) {
		config.Options["distributed"] = "true"
	} else {
		config.DeleteOptions = append(config.DeleteOptions, "distributed")
	}
	config.Options["template"] = "false"
	if (trafficClass == serviceLBInternalTraffic && serviceUsesTemplateLB(svc)) ||
		(trafficClass == serviceLBExternalTraffic && serviceUsesExternalLocalTemplate(svc)) {
		config.Options["template"] = "true"
		// Template LBs are per-Service and select a backend from chassis
		// variables. Flush conntrack entries when an endpoint is removed so a
		// connection created before the EndpointSlice update cannot continue
		// using the deleted backend after the template variable changes.
		config.Options["ct_flush"] = "true"
		if family != "" {
			config.Options["address-family"] = family
		}
	} else {
		config.DeleteOptions = append(config.DeleteOptions, "address-family")
		config.DeleteOptions = append(config.DeleteOptions, "ct_flush")
	}
	return config, nil
}

func (c *Controller) planServiceScopedLoadBalancers(ctx *endpointSliceReconcileContext) error {
	svc := ctx.service
	classes := []serviceLBTrafficClass{serviceLBInternalTraffic}
	owner := serviceScopedLBOwner(svc)
	if owner.kind == switchLBRuleLBOwnerKind || owner.kind == routerLBRuleLBOwnerKind {
		classes = []serviceLBTrafficClass{serviceLBExternalTraffic}
	} else if slices.ContainsFunc(ctx.profile.lbVips, func(vip string) bool { return ctx.profile.trafficClasses[vip] == serviceLBExternalTraffic }) {
		classes = append(classes, serviceLBExternalTraffic)
	}
	for _, port := range svc.Spec.Ports {
		for _, class := range classes {
			families := serviceScopedInternalLBFamilies(svc)
			if class == serviceLBExternalTraffic {
				families = serviceScopedExternalLBFamilies(svc)
			}
			for _, family := range families {
				config, err := c.serviceScopedLBConfig(svc, port.Protocol, class, family)
				if err != nil {
					return err
				}
				if ctx.lbReconciliation.desired[config.Name] != nil {
					continue
				}
				switches, routers, err := c.serviceLoadBalancerParents(ctx, class)
				if err != nil {
					return err
				}
				ctx.lbReconciliation.desired[config.Name] = &ovs.LoadBalancerDesiredState{
					Config: config, VIPs: make(map[string]string), IPPortMappings: make(map[string]string),
					HealthChecks: make(map[string]map[string]string), LogicalSwitches: switches, LogicalRouters: routers,
				}
				if family == "" && (class == serviceLBInternalTraffic || owner.kind != serviceLBOwnerKind) {
					ctx.loadBalancers.current[port.Protocol] = config.Name
				}
			}
		}
	}
	return nil
}

func (c *Controller) serviceLoadBalancerParents(ctx *endpointSliceReconcileContext, class serviceLBTrafficClass) ([]string, []string, error) {
	svc := ctx.service
	owner := serviceScopedLBOwner(svc)
	if owner.kind == switchLBRuleLBOwnerKind {
		logicalSwitch := svc.Annotations[util.LogicalSwitchAnnotation]
		if logicalSwitch == "" {
			logicalSwitch = ctx.subnetName
		}
		if logicalSwitch == "" {
			return nil, nil, fmt.Errorf("switch load balancer rule service %s/%s has no logical switch", svc.Namespace, svc.Name)
		}
		attached, err := c.serviceScopedVipAttachSubnets(svc, logicalSwitch)
		return append(attached, logicalSwitch), nil, err
	}
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		return nil, nil, err
	}
	var switches, routers []string
	for _, subnet := range subnets {
		if subnet.Name != c.config.NodeSwitch && isOvnSubnet(subnet) && subnet.Spec.Vpc == ctx.vpcName && subnetEnablesServiceLB(subnet, c.config.EnableOvnLB) {
			switches = append(switches, subnet.Name)
		}
	}
	if owner.kind == routerLBRuleLBOwnerKind || svc.Spec.Type == v1.ServiceTypeNodePort || (svc.Spec.Type == v1.ServiceTypeLoadBalancer && class == serviceLBExternalTraffic) {
		routers = append(routers, ctx.vpcName)
	}
	return switches, routers, nil
}

func (c *Controller) planServiceEndpointVIP(ctx *endpointSliceReconcileContext, state *serviceEndpointVIPState) {
	plan := ctx.lbReconciliation
	lb := plan.desired[state.lb]
	if len(state.backends) != 0 {
		backends := slices.Clone(state.backends)
		slices.Sort(backends)
		lb.VIPs[state.vip] = strings.Join(backends, ",")
		maps.Copy(lb.IPPortMappings, state.mapping)
		if !ctx.profile.ignoreHealthCheck {
			lb.HealthChecks[state.vip] = maps.Clone(state.externals)
		}
	}
	for _, name := range c.serviceLBMigrationCandidates(ctx.service, state.port.Protocol, ctx.vpc, state.trafficClass) {
		plan.batch.VIPDeletions = append(plan.batch.VIPDeletions, ovs.TemplateVIPDeletion{LoadBalancer: name, VIP: state.vip})
	}
}

func (plan *serviceLoadBalancerReconciliation) addTemplateState(prefix string, migrations []ovs.TemplateVIPMigration, desiredVIPs, variablesByChassis map[string]map[string]string) error {
	for name, vips := range desiredVIPs {
		lb := plan.desired[name]
		if lb == nil {
			return fmt.Errorf("template load balancer %s is not planned", name)
		}
		maps.Copy(lb.VIPs, vips)
	}
	for _, migration := range migrations {
		for _, name := range migration.OldLoadBalancers {
			plan.batch.VIPDeletions = append(plan.batch.VIPDeletions, ovs.TemplateVIPDeletion{LoadBalancer: name, VIP: migration.OldVIP})
		}
	}
	for _, chassis := range slices.Sorted(maps.Keys(variablesByChassis)) {
		plan.batch.TemplateVariables = append(plan.batch.TemplateVariables, ovs.ChassisTemplateVariableReconciliation{
			Chassis: chassis, Prefix: prefix, Variables: variablesByChassis[chassis],
		})
	}
	return nil
}

func (c *Controller) commitServiceLoadBalancers(ctx *endpointSliceReconcileContext) error {
	plan := ctx.lbReconciliation
	for _, name := range slices.Sorted(maps.Keys(plan.desired)) {
		plan.batch.LoadBalancers = append(plan.batch.LoadBalancers, *plan.desired[name])
	}
	regular, session := c.legacyVpcLoadBalancerNames(ctx.vpc)
	for _, lbs := range []map[v1.Protocol]string{regular, session} {
		plan.batch.DeleteEmptyLoadBalancers = append(plan.batch.DeleteEmptyLoadBalancers, slices.Collect(maps.Values(lbs))...)
	}
	if err := c.OVNNbClient.(resourceLoadBalancerClient).ReconcileLoadBalancers(plan.batch); err != nil {
		return fmt.Errorf("reconcile load balancers for service %s/%s: %w", ctx.service.Namespace, ctx.service.Name, err)
	}
	return nil
}

func (c *Controller) deleteServiceLoadBalancerState(service *vpcService, services []*v1.Service) error {
	plan := newServiceLoadBalancerReconciliation(service.Svc)
	var vpc *kubeovnv1.Vpc
	if service.Vpc != "" {
		var err error
		vpc, err = c.vpcsLister.Get(service.Vpc)
		if err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("get vpc %s for legacy load balancer cleanup: %w", service.Vpc, err)
		}
	}
	legacy := c.GenVpcLoadBalancer(service.Vpc)
	regular := map[v1.Protocol]string{v1.ProtocolTCP: legacy.TCPLoadBalancer, v1.ProtocolUDP: legacy.UDPLoadBalancer, v1.ProtocolSCTP: legacy.SctpLoadBalancer}
	session := map[v1.Protocol]string{v1.ProtocolTCP: legacy.TCPSessLoadBalancer, v1.ProtocolUDP: legacy.UDPSessLoadBalancer, v1.ProtocolSCTP: legacy.SctpSessLoadBalancer}
	if vpc != nil {
		regular, session = c.legacyVpcLoadBalancerNames(vpc)
		for _, names := range []map[v1.Protocol]string{regular, session} {
			plan.batch.DeleteEmptyLoadBalancers = append(plan.batch.DeleteEmptyLoadBalancers, slices.Collect(maps.Values(names))...)
		}
	}
	for _, ip := range getVipIps(service.Svc) {
		if slices.ContainsFunc(services, func(svc *v1.Service) bool { return slices.Contains(util.ServiceClusterIPs(*svc), ip) }) {
			continue
		}
		for _, port := range service.Svc.Spec.Ports {
			for _, name := range []string{regular[port.Protocol], session[port.Protocol]} {
				plan.batch.VIPDeletions = append(plan.batch.VIPDeletions, ovs.TemplateVIPDeletion{LoadBalancer: name, VIP: util.JoinHostPort(ip, port.Port)})
			}
		}
	}
	if err := c.OVNNbClient.(resourceLoadBalancerClient).ReconcileLoadBalancers(plan.batch); err != nil {
		return err
	}
	if vpc != nil {
		return c.cleanupLegacyVpcLoadBalancers(vpc)
	}
	return nil
}

func (c *Controller) deleteServiceScopedLoadBalancersWithLegacyVIPs(svc *v1.Service, vpcName string, vips []string, healthCheckDeletions ...[]string) error {
	client, ok := c.OVNNbClient.(resourceLoadBalancerClient)
	if !ok {
		if err := c.deleteLegacyVpcVIPs(vpcName, vips); err != nil {
			return err
		}
		return c.deleteServiceScopedLoadBalancers(svc)
	}
	plan := newServiceLoadBalancerReconciliation(svc)
	if len(healthCheckDeletions) > 0 {
		plan.batch.HealthCheckDeletions = slices.Clone(healthCheckDeletions[0])
	}
	if vpcName != "" {
		vpc, err := c.vpcsLister.Get(vpcName)
		if err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("get vpc %s for legacy load balancer cleanup: %w", vpcName, err)
		}
		if err == nil {
			regular, session := c.legacyVpcLoadBalancerNames(vpc)
			for _, names := range []map[v1.Protocol]string{regular, session} {
				plan.batch.DeleteEmptyLoadBalancers = append(plan.batch.DeleteEmptyLoadBalancers, slices.Collect(maps.Values(names))...)
				for _, name := range names {
					for _, vip := range vips {
						plan.batch.VIPDeletions = append(plan.batch.VIPDeletions, ovs.TemplateVIPDeletion{LoadBalancer: name, VIP: vip})
					}
				}
			}
		}
	}
	if err := client.ReconcileLoadBalancers(plan.batch); err != nil {
		return err
	}
	if vpcName != "" {
		if vpc, err := c.vpcsLister.Get(vpcName); err == nil {
			return c.cleanupLegacyVpcLoadBalancers(vpc)
		} else if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("get vpc %s for legacy load balancer cleanup: %w", vpcName, err)
		}
	}
	return nil
}
