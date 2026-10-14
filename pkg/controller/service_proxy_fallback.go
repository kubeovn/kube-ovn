package controller

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) serviceCIDRsByProtocol(protocol string) []string {
	if protocol == kubeovnv1.ProtocolIPv6 {
		return c.serviceCIDRStore.V6CIDRs()
	}
	return c.serviceCIDRStore.V4CIDRs()
}

// Apply the drop after LS load balancing so DNATed Service traffic reaches its
// backend, while unresolved Service destinations cannot escape to kube-proxy.
// Binding to the LS covers every gateway mode, including physical underlay.
func (c *Controller) reconcileServiceProxyFallbackACL(subnet *kubeovnv1.Subnet) error {
	var matches []string
	if c.config.EnableOvnLB && subnetEnablesServiceLB(subnet, c.config.EnableOvnLB) &&
		subnet.Name != c.config.NodeSwitch && isOvnSubnet(subnet) {
		for _, protocol := range []string{kubeovnv1.ProtocolIPv4, kubeovnv1.ProtocolIPv6} {
			ipSuffix := getIPSuffix(protocol)
			for _, serviceCIDR := range c.serviceCIDRsByProtocol(protocol) {
				match := fmt.Sprintf("%s.dst == %s", ipSuffix, serviceCIDR)
				for _, dnsIP := range c.config.NodeLocalDNSIPs {
					if util.CheckProtocol(dnsIP) == protocol && util.CIDRContainIP(serviceCIDR, dnsIP) {
						match += fmt.Sprintf(" && %s.dst != %s", ipSuffix, dnsIP)
					}
				}
				matches = append(matches, match)
			}
		}
	}
	return c.OVNNbClient.ReconcileServiceProxyFallbackACL(subnet.Name, matches)
}

// The node port group identifies the Pod's current node, independently of the
// centralized gateway. Limit its scope to this subnet's addresses because the
// same node can host Pods from subnets with different LB settings.
func (c *Controller) reconcileServiceProxyFallbackReroutePolicy(subnet *kubeovnv1.Subnet) error {
	owned := map[string]string{"isServiceProxyFallbackReroutePolicy": "true"}
	policies, err := c.OVNNbClient.ListLogicalRouterPolicies(c.config.ClusterRouter, -1, buildPolicyRouteExternalIDs(subnet.Name, owned), true)
	if err != nil {
		return fmt.Errorf("list service proxy fallback reroute policies for subnet %s: %w", subnet.Name, err)
	}
	desired := make(map[string]struct{})
	serviceLBEnabled := c.config.EnableOvnLB && subnetEnablesServiceLB(subnet, c.config.EnableOvnLB)
	eligible := subnet.Name != c.config.NodeSwitch && subnet.Spec.Vpc == c.config.ClusterRouter &&
		subnet.Spec.GatewayType == kubeovnv1.GWCentralizedType &&
		(subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway) && isOvnSubnet(subnet) && !serviceLBEnabled
	if eligible {
		nodes, listErr := c.nodesLister.List(labels.Everything())
		if listErr != nil {
			return fmt.Errorf("list nodes for centralized service proxy fallback on subnet %s: %w", subnet.Name, listErr)
		}
		for _, node := range nodes {
			joinIP := strings.TrimSpace(node.Annotations[util.IPAddressAnnotation])
			if joinIP == "" {
				continue
			}
			nodePortName := strings.ReplaceAll(util.NodeLspName(node.Name), "-", ".")
			for subnetCIDR := range strings.SplitSeq(subnet.Spec.CIDRBlock, ",") {
				if subnetCIDR == "" {
					continue
				}
				protocol := util.CheckProtocol(subnetCIDR)
				ipSuffix := getIPSuffix(protocol)
				for _, serviceCIDR := range c.serviceCIDRsByProtocol(protocol) {
					match := fmt.Sprintf("%s.src == $%s_%s && %s.src == %s && %s.dst == %s", ipSuffix, nodePortName, ipSuffix, ipSuffix, subnetCIDR, ipSuffix, serviceCIDR)
					nextHop := joinIP
					v4, v6 := util.SplitStringIP(joinIP)
					if protocol == kubeovnv1.ProtocolIPv4 {
						nextHop = v4
					} else {
						nextHop = v6
					}
					if nextHop == "" {
						continue
					}
					desired[match] = struct{}{}
					if err := c.addPolicyRouteToVpc(c.config.ClusterRouter, &kubeovnv1.PolicyRoute{
						Priority:  util.ServiceProxyFallbackReroutePriority,
						Match:     match,
						Action:    kubeovnv1.PolicyRouteActionReroute,
						NextHopIP: nextHop,
					}, buildPolicyRouteExternalIDs(subnet.Name, map[string]string{
						"isServiceProxyFallbackReroutePolicy": "true",
						"node":                                node.Name,
						"serviceCIDR":                         serviceCIDR,
					})); err != nil {
						return fmt.Errorf("add centralized service proxy fallback policy for subnet %s node %s: %w", subnet.Name, node.Name, err)
					}
				}
			}
		}
	}
	for _, policy := range policies {
		if policy.Priority == util.ServiceProxyFallbackReroutePriority {
			if _, ok := desired[policy.Match]; ok {
				continue
			}
		}
		if err := c.OVNNbClient.DeleteLogicalRouterPolicyByUUID(c.config.ClusterRouter, policy.UUID); err != nil {
			return fmt.Errorf("delete stale centralized service proxy fallback policy %s: %w", policy.UUID, err)
		}
	}
	return nil
}

func (c *Controller) deleteServiceProxyFallbackPolicies(subnet *kubeovnv1.Subnet, owner string) error {
	policies, err := c.OVNNbClient.ListLogicalRouterPolicies(c.config.ClusterRouter, -1,
		buildPolicyRouteExternalIDs(subnet.Name, map[string]string{owner: "true"}), true)
	if err != nil {
		return fmt.Errorf("list %s policies for subnet %s: %w", owner, subnet.Name, err)
	}
	for _, policy := range policies {
		if err := c.OVNNbClient.DeleteLogicalRouterPolicyByUUID(c.config.ClusterRouter, policy.UUID); err != nil {
			return fmt.Errorf("delete %s policy %s: %w", owner, policy.UUID, err)
		}
	}
	return nil
}

func (c *Controller) enqueueCentralizedServiceProxySubnets() {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("list subnets for node-local service proxy routing: %v", err)
		return
	}
	for _, subnet := range subnets {
		if subnet.Spec.Vpc == c.config.ClusterRouter && subnet.Spec.GatewayType == kubeovnv1.GWCentralizedType &&
			(subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway) && isOvnSubnet(subnet) && subnet.Name != c.config.NodeSwitch {
			c.addOrUpdateSubnetQueue.Add(subnet.Name)
		}
	}
}
