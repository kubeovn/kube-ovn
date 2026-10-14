package controller

import (
	"fmt"
	"slices"
	"strings"

	"github.com/scylladb/go-set/strset"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const serviceProxyFallbackPortGroupKey = "serviceProxyFallback"

func (c *Controller) needsServiceProxyFallbackReroute(subnet *kubeovnv1.Subnet) bool {
	return c.subnetUsesNodeServiceProxy(subnet) && subnet.Spec.GatewayType == kubeovnv1.GWCentralizedType &&
		(!c.config.EnableOvnLB || !subnetEnablesServiceLB(subnet, c.config.EnableOvnLB))
}

func serviceProxyFallbackPortGroupName(subnetName, nodeName string) string {
	return "service.proxy." + util.Sha256Hash([]byte(subnetName+"/"+nodeName))
}

// Intersect port UUIDs, not IPs: isolated VLANs can reuse an overlay Pod's IP
// on another node. A node-wide address set cannot identify that Pod's node.
func (c *Controller) syncServiceProxyFallbackPortGroup(subnet *kubeovnv1.Subnet, nodeName string) error {
	pgName := serviceProxyFallbackPortGroupName(subnet.Name, nodeName)
	if err := c.OVNNbClient.CreatePortGroup(pgName, map[string]string{
		serviceProxyFallbackPortGroupKey: "true", "subnet": subnet.Name, "node": nodeName,
	}); err != nil {
		return fmt.Errorf("create service proxy fallback port group %s: %w", pgName, err)
	}
	pg, err := c.OVNNbClient.GetPortGroup(pgName, false)
	if err != nil {
		return err
	}
	nodePG, err := c.OVNNbClient.GetPortGroup(strings.ReplaceAll(util.NodeLspName(nodeName), "-", "."), true)
	if err != nil {
		return err
	}
	portsOnNode := strset.New()
	if nodePG != nil {
		portsOnNode.Add(nodePG.Ports...)
	}
	ports, err := c.OVNNbClient.ListLogicalSwitchPorts(true, map[string]string{logicalSwitchKey: subnet.Name},
		func(port *ovnnb.LogicalSwitchPort) bool { return portsOnNode.Has(port.UUID) })
	if err != nil {
		return err
	}
	portIDs := strset.New()
	portNames := make([]string, 0, len(ports))
	for _, port := range ports {
		portIDs.Add(port.UUID)
		portNames = append(portNames, port.Name)
	}
	if portIDs.IsEqual(strset.New(pg.Ports...)) {
		return nil
	}
	if err := c.OVNNbClient.PortGroupSetPorts(pgName, portNames); err != nil {
		return fmt.Errorf("update service proxy fallback port group %s: %w", pgName, err)
	}
	return nil
}

// Refresh both the new node and any previous node before finishing Pod routing.
func (c *Controller) syncServiceProxyFallbackPodPort(subnet *kubeovnv1.Subnet, nodeName, portName string) error {
	if !c.needsServiceProxyFallbackReroute(subnet) {
		return nil
	}
	port, err := c.OVNNbClient.GetLogicalSwitchPort(portName, false)
	if err != nil {
		return err
	}
	groups, err := c.OVNNbClient.ListPortGroups(buildPolicyRouteExternalIDs(subnet.Name,
		map[string]string{serviceProxyFallbackPortGroupKey: "true"}))
	if err != nil {
		return err
	}
	for _, pg := range groups {
		if pg.ExternalIDs["node"] != nodeName && slices.Contains(pg.Ports, port.UUID) {
			if err := c.syncServiceProxyFallbackPortGroup(subnet, pg.ExternalIDs["node"]); err != nil {
				return err
			}
		}
	}
	return c.syncServiceProxyFallbackPortGroup(subnet, nodeName)
}

func (c *Controller) deleteStaleServiceProxyFallbackPortGroups(subnet *kubeovnv1.Subnet, desired map[string]struct{}) error {
	groups, err := c.OVNNbClient.ListPortGroups(buildPolicyRouteExternalIDs(subnet.Name,
		map[string]string{serviceProxyFallbackPortGroupKey: "true"}))
	if err != nil {
		return err
	}
	for _, pg := range groups {
		if _, ok := desired[pg.Name]; !ok {
			if err := c.OVNNbClient.DeletePortGroup(pg.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// Run after refreshing node groups, including after a controller restart. The
// owner tag keeps these groups separate from network-policy and security groups.
func (c *Controller) syncServiceProxyFallbackPortGroups() error {
	groups, err := c.OVNNbClient.ListPortGroups(map[string]string{
		"vendor": util.CniTypeName, serviceProxyFallbackPortGroupKey: "true",
	})
	if err != nil {
		return err
	}
	for _, pg := range groups {
		subnet, err := c.subnetsLister.Get(pg.ExternalIDs["subnet"])
		if err != nil && !k8serrors.IsNotFound(err) {
			return err
		}
		node, err := c.nodesLister.Get(pg.ExternalIDs["node"])
		if err != nil && !k8serrors.IsNotFound(err) {
			return err
		}
		if subnet != nil && node != nil && c.needsServiceProxyFallbackReroute(subnet) && node.Annotations[util.IPAddressAnnotation] != "" {
			if err := c.syncServiceProxyFallbackPortGroup(subnet, node.Name); err != nil {
				return err
			}
			continue
		}
		if err := c.deleteServiceProxyFallbackPortGroup(&pg); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) deleteServiceProxyFallbackPortGroup(pg *ovnnb.PortGroup) error {
	externalIDs := buildPolicyRouteExternalIDs(pg.ExternalIDs["subnet"], map[string]string{
		"isServiceProxyFallbackReroutePolicy": "true", "node": pg.ExternalIDs["node"],
	})
	policies, err := c.OVNNbClient.ListLogicalRouterPolicies(c.config.ClusterRouter, -1, externalIDs, true)
	if err != nil {
		return err
	}
	for _, policy := range policies {
		if err := c.OVNNbClient.DeleteLogicalRouterPolicyByUUID(c.config.ClusterRouter, policy.UUID); err != nil {
			return err
		}
	}
	return c.OVNNbClient.DeletePortGroup(pg.Name)
}
