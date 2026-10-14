package controller

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) serviceCIDRsByProtocol(protocol string) []string {
	if protocol == kubeovnv1.ProtocolIPv6 {
		return c.serviceCIDRStore.V6CIDRs()
	}
	return c.serviceCIDRStore.V4CIDRs()
}

// These subnets use node join ports for host Service proxy access. Physical
// underlay and custom VPCs retain their existing routing paths.
func (c *Controller) subnetUsesNodeServiceProxy(subnet *kubeovnv1.Subnet) bool {
	return subnet.Name != c.config.NodeSwitch && subnet.Spec.Vpc == c.config.ClusterRouter &&
		(subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway) && isOvnSubnet(subnet)
}

// LS load balancing precedes the router policy stage. Drop destinations still
// in a ServiceCIDR before the node gateway reroute can send them to kube-proxy.
// The router ingress port scopes protection to this LB-enabled subnet without
// matching Pod source addresses or affecting other subnets sharing the router.
func (c *Controller) reconcileServiceProxyFallbackDropPolicy(subnet *kubeovnv1.Subnet) error {
	externalIDs := buildPolicyRouteExternalIDs(subnet.Name, map[string]string{"isServiceProxyFallbackDropPolicy": "true"})
	policies, err := c.OVNNbClient.ListLogicalRouterPolicies(c.config.ClusterRouter, -1, externalIDs, true)
	if err != nil {
		return fmt.Errorf("list service proxy fallback drop policies for subnet %s: %w", subnet.Name, err)
	}
	desired := make(map[string]struct{})
	if c.subnetUsesNodeServiceProxy(subnet) && c.config.EnableOvnLB && subnetEnablesServiceLB(subnet, c.config.EnableOvnLB) {
		subnetProtocol := util.CheckProtocol(subnet.Spec.CIDRBlock)
		portName := ovs.LogicalRouterPortName(c.config.ClusterRouter, subnet.Name)
		for _, protocol := range []string{kubeovnv1.ProtocolIPv4, kubeovnv1.ProtocolIPv6} {
			if subnetProtocol != kubeovnv1.ProtocolDual && subnetProtocol != protocol {
				continue
			}
			ipSuffix := getIPSuffix(protocol)
			for _, serviceCIDR := range c.serviceCIDRsByProtocol(protocol) {
				matchParts := []string{fmt.Sprintf("inport == %q && %s.dst == %s", portName, ipSuffix, serviceCIDR)}
				for _, dnsIP := range c.config.NodeLocalDNSIPs {
					if util.CheckProtocol(dnsIP) == protocol && util.CIDRContainIP(serviceCIDR, dnsIP) {
						matchParts = append(matchParts, fmt.Sprintf("%s.dst != %s", ipSuffix, dnsIP))
					}
				}
				match := strings.Join(matchParts, " && ")
				desired[match] = struct{}{}
				if err := c.addPolicyRouteToVpc(c.config.ClusterRouter, &kubeovnv1.PolicyRoute{
					Priority: util.ServiceProxyFallbackDropPriority,
					Match:    match,
					Action:   kubeovnv1.PolicyRouteActionDrop,
				}, externalIDs); err != nil {
					return fmt.Errorf("add service proxy fallback drop policy for subnet %s: %w", subnet.Name, err)
				}
			}
		}
	}
	for _, policy := range policies {
		if policy.Priority == util.ServiceProxyFallbackDropPriority {
			if _, ok := desired[policy.Match]; ok {
				continue
			}
		}
		if err := c.OVNNbClient.DeleteLogicalRouterPolicyByUUID(c.config.ClusterRouter, policy.UUID); err != nil {
			return fmt.Errorf("delete stale service proxy fallback drop policy %s: %w", policy.UUID, err)
		}
	}
	return nil
}

// The subnet-node port group identifies the Pod's node, independently of the
// centralized gateway. Scope the policy to the subnet's ingress port and
// addresses so overlapping VLAN subnets cannot share or steal policies.
func (c *Controller) reconcileServiceProxyFallbackReroutePolicy(subnet *kubeovnv1.Subnet) error {
	owned := map[string]string{"isServiceProxyFallbackReroutePolicy": "true"}
	policies, err := c.OVNNbClient.ListLogicalRouterPolicies(c.config.ClusterRouter, -1, buildPolicyRouteExternalIDs(subnet.Name, owned), true)
	if err != nil {
		return fmt.Errorf("list service proxy fallback reroute policies for subnet %s: %w", subnet.Name, err)
	}
	desired := make(map[string]struct{})
	desiredGroups := make(map[string]struct{})
	if c.needsServiceProxyFallbackReroute(subnet) {
		nodes, listErr := c.nodesLister.List(labels.Everything())
		if listErr != nil {
			return fmt.Errorf("list nodes for centralized service proxy fallback on subnet %s: %w", subnet.Name, listErr)
		}
		portName := ovs.LogicalRouterPortName(c.config.ClusterRouter, subnet.Name)
		for _, node := range nodes {
			joinIP := strings.TrimSpace(node.Annotations[util.IPAddressAnnotation])
			if joinIP == "" {
				continue
			}
			nodePortName := serviceProxyFallbackPortGroupName(subnet.Name, node.Name)
			if err := c.syncServiceProxyFallbackPortGroup(subnet, node.Name); err != nil {
				return err
			}
			desiredGroups[nodePortName] = struct{}{}
			for subnetCIDR := range strings.SplitSeq(subnet.Spec.CIDRBlock, ",") {
				if subnetCIDR == "" {
					continue
				}
				protocol := util.CheckProtocol(subnetCIDR)
				ipSuffix := getIPSuffix(protocol)
				for _, serviceCIDR := range c.serviceCIDRsByProtocol(protocol) {
					match := fmt.Sprintf("inport == %q && %s.src == $%s_%s && %s.src == %s && %s.dst == %s", portName, ipSuffix, nodePortName, ipSuffix, ipSuffix, subnetCIDR, ipSuffix, serviceCIDR)
					var nextHop string
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
	return c.deleteStaleServiceProxyFallbackPortGroups(subnet, desiredGroups)
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
	if owner == "isServiceProxyFallbackReroutePolicy" {
		return c.deleteStaleServiceProxyFallbackPortGroups(subnet, nil)
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
		if c.subnetUsesNodeServiceProxy(subnet) && subnet.Spec.GatewayType == kubeovnv1.GWCentralizedType {
			c.addOrUpdateSubnetQueue.Add(subnet.Name)
		}
	}
}
