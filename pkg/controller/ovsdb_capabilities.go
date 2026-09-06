package controller

import (
	"errors"

	netv1 "k8s.io/api/networking/v1"
	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"
	v1alpha2 "sigs.k8s.io/network-policy-api/apis/v1alpha2"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	"k8s.io/utils/set"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// networkPolicyACLBuilder owns the policy-specific ACL operation builders.
// They resolve address-set semantics, meters, ACL names, logging, and policy
// tiers, so they are intentionally kept as an optional domain capability
// rather than being added to the generic table CRUD interface.
type networkPolicyACLBuilder interface {
	UpdateDefaultBlockACLOps(npName, pgName, direction string, loggingEnabled, lax bool, logRate int) ([]ovsdb.Operation, error)
	UpdateDefaultBlockExceptionsACLOps(npName, pgName, npNamespace, direction string) ([]ovsdb.Operation, error)
	UpdateIngressACLOps(pgName, asIngressName, asExceptName, protocol, aclName string, ports []netv1.NetworkPolicyPort, logEnable bool, logActions []ovnnb.ACLAction, logRate int, namedPortMap map[string]*util.NamedPortInfo) ([]ovsdb.Operation, error)
	UpdateEgressACLOps(pgName, asEgressName, asExceptName, protocol, aclName string, ports []netv1.NetworkPolicyPort, logEnable bool, logActions []ovnnb.ACLAction, logRate int, namedPortMap map[string]*util.NamedPortInfo) ([]ovsdb.Operation, error)
	UpdateIngressIPBlockACLOps(pgName, protocol, aclName string, ipBlocks []netv1.IPBlock, ports []netv1.NetworkPolicyPort, logEnable bool, logActions []ovnnb.ACLAction, logRate int, namedPortMap map[string]*util.NamedPortInfo) ([]ovsdb.Operation, error)
	UpdateEgressIPBlockACLOps(pgName, protocol, aclName string, ipBlocks []netv1.IPBlock, ports []netv1.NetworkPolicyPort, logEnable bool, logActions []ovnnb.ACLAction, logRate int, namedPortMap map[string]*util.NamedPortInfo) ([]ovsdb.Operation, error)
}

// adminNetworkPolicyACLBuilder builds ACL operations for both ANP and BANP.
type adminNetworkPolicyACLBuilder interface {
	UpdateAnpRuleACLOps(pgName, asName, protocol, aclName string, priority int, action ovnnb.ACLAction, logActions []ovnnb.ACLAction, ports []v1alpha1.AdminNetworkPolicyPort, isIngress, isBanp bool) ([]ovsdb.Operation, error)
}

// clusterNetworkPolicyACLBuilder builds ACL operations for CNP.
type clusterNetworkPolicyACLBuilder interface {
	UpdateCnpRuleACLOps(pgName, asName, protocol, aclName string, priority int, action ovnnb.ACLAction, logActions []ovnnb.ACLAction, ports []v1alpha2.ClusterNetworkPolicyPort, isIngress bool, tier int) ([]ovsdb.Operation, error)
}

func (c *Controller) networkPolicyACLBuilder() (networkPolicyACLBuilder, error) {
	if provider, ok := c.OVNNbTables.(networkPolicyACLBuilder); ok {
		return provider, nil
	}
	// Network-policy ACL construction is a domain capability rather than table
	// CRUD. Keep the legacy client as an explicit adapter for old fixtures and
	// callers that inject only NbClient; production wiring uses OVNNbTables.
	if c.OVNNbTables == nil {
		if provider, ok := c.OVNNbClient.(networkPolicyACLBuilder); ok {
			return provider, nil
		}
	}
	return nil, errors.New("OVN NB table provider does not support network policy ACL operations")
}

func (c *Controller) adminNetworkPolicyACLBuilder() (adminNetworkPolicyACLBuilder, error) {
	if provider, ok := c.OVNNbTables.(adminNetworkPolicyACLBuilder); ok {
		return provider, nil
	}
	if c.OVNNbTables == nil {
		if provider, ok := c.OVNNbClient.(adminNetworkPolicyACLBuilder); ok {
			return provider, nil
		}
	}
	return nil, errors.New("OVN NB table provider does not support admin network policy ACL operations")
}

func (c *Controller) clusterNetworkPolicyACLBuilder() (clusterNetworkPolicyACLBuilder, error) {
	if provider, ok := c.OVNNbTables.(clusterNetworkPolicyACLBuilder); ok {
		return provider, nil
	}
	if c.OVNNbTables == nil {
		if provider, ok := c.OVNNbClient.(clusterNetworkPolicyACLBuilder); ok {
			return provider, nil
		}
	}
	return nil, errors.New("OVN NB table provider does not support cluster network policy ACL operations")
}

// reconcilePortDHCPOptionsBackend keeps the compatibility path in one place.
// The provider branch uses the controller's table implementation, while the
// legacy branch is retained for fixtures that only inject NbClient.
func (c *Controller) reconcilePortDHCPOptionsBackend(
	lsName, portName string, subnetDHCP *ovs.DHCPOptionsUUIDs, cidrBlock, gateway, v4Options, v6Options string, mtu int,
) (*ovs.DHCPOptionsUUIDs, bool, error) {
	if c.OVNNbTables == nil {
		if c.OVNNbClient == nil {
			return nil, false, errors.New("OVN NB table provider is nil")
		}
		return c.OVNNbClient.ReconcilePortDHCPOptions(lsName, portName, subnetDHCP, cidrBlock, gateway, v4Options, v6Options, mtu)
	}
	return c.updatePortDHCPOptionsTable(lsName, portName, subnetDHCP, cidrBlock, gateway, v4Options, v6Options, mtu)
}

func (c *Controller) updateSubnetDHCPOptionsBackend(subnet *kubeovnv1.Subnet, mtu int) (*ovs.DHCPOptionsUUIDs, error) {
	if c.OVNNbTables == nil {
		if c.OVNNbClient == nil {
			return nil, errors.New("OVN NB table provider is nil")
		}
		return c.OVNNbClient.UpdateDHCPOptions(subnet, mtu)
	}
	return c.updateSubnetDHCPOptionsTable(subnet, mtu)
}

func (c *Controller) reconcileGatewayBFDWithCleanupBackend(
	bfdIP, lrpName string, nextHops set.Set[string], minTX, minRX, multiplier int32, externalIDs map[string]string,
) (set.Set[string], error) {
	if c.OVNNbTables == nil {
		if c.OVNNbClient == nil {
			return nil, errors.New("OVN NB table provider is nil")
		}
		return reconcileGatewayBFDWithCleanup(c.OVNNbClient, bfdIP, lrpName, nextHops, minTX, minRX, multiplier, externalIDs)
	}
	return c.reconcileGatewayBFDWithCleanupTable(bfdIP, lrpName, nextHops, minTX, minRX, multiplier, externalIDs)
}

func (c *Controller) reconcileGatewayBFDBackend(
	bfdIP, lrpName string, nextHops set.Set[string], minTX, minRX, multiplier int32, externalIDs map[string]string,
) (set.Set[string], map[string]string, set.Set[string], error) {
	if c.OVNNbTables == nil {
		if c.OVNNbClient == nil {
			return nil, nil, nil, errors.New("OVN NB table provider is nil")
		}
		return reconcileGatewayBFD(c.OVNNbClient, bfdIP, lrpName, nextHops, minTX, minRX, multiplier, externalIDs)
	}
	return c.reconcileGatewayBFDTable(bfdIP, lrpName, nextHops, minTX, minRX, multiplier, externalIDs)
}

func (c *Controller) cleanupStaleBFDBackend(staleBFDIDs set.Set[string]) error {
	if c.OVNNbTables == nil {
		if c.OVNNbClient == nil {
			return errors.New("OVN NB table provider is nil")
		}
		return cleanupStaleBFD(c.OVNNbClient, staleBFDIDs)
	}
	return c.cleanupStaleBFDTable(staleBFDIDs)
}
