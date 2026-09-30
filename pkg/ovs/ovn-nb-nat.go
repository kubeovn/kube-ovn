package ovs

import (
	"errors"
	"fmt"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c *OVNNbClient) AddNat(lrName, natType, externalIP, logicalIP, logicalMac, port string, options map[string]string) error {
	// The logical_port and external_mac are only accepted
	// when router is a distributed router (rather than a gateway router)
	// and type is dnat_and_snat. The logical_port is the name
	// of an existing logical switch port where the logical_ip resides.
	// The external_mac is an Ethernet address.

	// When the logical_port and external_mac are specified,
	// the NAT rule will be programmed on the chassis where the logical_port resides.
	// This includes ARP replies for the external_ip, which return the value of external_mac.
	// All packets transmitted with source IP address equal to external_ip will be sent using the external_mac.

	if natType == ovnnb.NATTypeDNATAndSNAT {
		return c.addOrUpdateDnatAndSnat(lrName, externalIP, logicalIP, logicalMac, port, options)
	}

	nat, err := c.newNat(lrName, natType, externalIP, logicalIP, logicalMac, port, func(nat *ovnnb.NAT) {
		copyStringMapInto(&nat.Options, options)
	})
	if err != nil {
		klog.Errorf("failed to new nat: %v", err)
		return err
	}

	return c.CreateNats(lrName, nat)
}

// addOrUpdateDnatAndSnat is the robust path for dnat_and_snat.
// On EIP primary-pod swap we do a broad Delete (by externalIP) + fresh
// Create. This avoids libovsdb cache staleness. We bypass newNat to skip
// the stale "found, ignore".
func (c *OVNNbClient) addOrUpdateDnatAndSnat(lrName, externalIP, logicalIP, logicalMac, port string, options map[string]string) error {
	if err := requireValue(externalIP, fmt.Errorf("external ip is required when nat type is %s", ovnnb.NATTypeDNATAndSNAT)); err != nil {
		return err
	}

	// Broad delete clears any previous row for this EIP (old logicalIP may differ).
	if err := c.DeleteNat(lrName, ovnnb.NATTypeDNATAndSNAT, externalIP, ""); err != nil {
		klog.Errorf("failed to clear prior dnat_and_snat external_ip=%s: %v", externalIP, err)
		return err
	}

	// Fresh create, direct construction + CreateNats to bypass newNat's cache check.
	nat := &ovnnb.NAT{
		UUID:       ovsclient.NamedUUID(),
		Type:       ovnnb.NATTypeDNATAndSNAT,
		ExternalIP: externalIP,
		LogicalIP:  logicalIP,
	}
	if logicalMac != "" {
		nat.ExternalMAC = &logicalMac
	}
	if port != "" {
		nat.LogicalPort = &port
	}
	copyStringMapInto(&nat.Options, options)

	klog.V(2).Infof("installing dnat_and_snat external_ip=%s logical_ip=%s logical_port=%s",
		externalIP, logicalIP, port)
	return c.CreateNats(lrName, nat)
}

// CreateNats create several logical router nat rule once
func (c *OVNNbClient) CreateNats(lrName string, nats ...*ovnnb.NAT) error {
	if len(nats) == 0 {
		err := errors.New("nats is empty")
		return logErr(err)
	}
	models, uuids := modelsAndUUIDs(nats, func(nat *ovnnb.NAT) string { return nat.UUID })
	ops, err := createAndAttachOps(c, &ovnnb.NAT{}, models, func(ids []string) ([]ovsdb.Operation, error) {
		return c.LogicalRouterUpdateNatOp(lrName, ids, ovsdb.MutateOperationInsert)
	}, uuids)
	return c.transactGenerated("lr-nats-add", ops, err,
		wrapErr("generate operations for adding nats to logical router %s: %w", lrName),
		wrapErr("add nats to %s: %w", lrName),
	)
}

// EnsureSnat ensures a SNAT rule exists for the given (externalIP, logicalIP) pair.
// If the rule already exists, it is a no-op; otherwise a new rule is created.
func (c *OVNNbClient) EnsureSnat(lrName, externalIP, logicalIP string) error {
	if err := requireName(externalIP, "snat external ip is required"); err != nil {
		return err
	}
	if err := requireName(logicalIP, "snat logical ip is required"); err != nil {
		return err
	}

	natType := ovnnb.NATTypeSNAT
	nat, err := c.GetNat(lrName, natType, externalIP, logicalIP, true)
	if err != nil {
		return logErr(err)
	}

	// nat already exists with the correct external_ip, nothing to update
	if nat != nil {
		return nil
	}

	/* create nat */
	if nat, err = c.newNat(lrName, natType, externalIP, logicalIP, "", ""); err != nil {
		return logWrap(err, wrapErr("new logical router %s nat 'type %s external ip %s logical ip %s': %w", lrName, natType, externalIP, logicalIP))
	}

	if err := c.CreateNats(lrName, nat); err != nil {
		return logWrap(err, wrapErr("add nat 'type %s external ip %s logical ip %s' to logical router %s: %w", natType, externalIP, logicalIP, lrName))
	}

	return nil
}

// UpdateDnatAndSnat update dnat_and_snat rule
func (c *OVNNbClient) UpdateDnatAndSnat(lrName, externalIP, logicalIP, lspName, externalMac, gatewayType string) error {
	if err := requireName(externalIP, "nat external ip is required"); err != nil {
		return err
	}
	if err := requireName(logicalIP, "nat logical ip is required"); err != nil {
		return err
	}
	natType := ovnnb.NATTypeDNATAndSNAT

	nat, err := c.GetNat(lrName, natType, externalIP, "", true)
	if err != nil {
		return logErr(err)
	}

	// update logical port and external mac when nat exists
	if nat != nil {
		if gatewayType == kubeovnv1.GWDistributedType {
			// clear lspName and externalMac when they are empty
			nat.LogicalPort = &lspName
			nat.ExternalMAC = &externalMac
			return c.UpdateNat(nat, &nat.LogicalPort, &nat.ExternalMAC)
		}
		return nil // do nothing when gw is centralized
	}

	options := func(nat *ovnnb.NAT) {
		if gatewayType == kubeovnv1.GWDistributedType {
			nat.LogicalPort = &lspName
			nat.ExternalMAC = &externalMac

			if nat.Options == nil {
				nat.Options = make(map[string]string, 1)
			}
			nat.Options["stateless"] = "true"
		}
	}

	/* create nat */
	if nat, err = c.newNat(lrName, natType, externalIP, logicalIP, "", "", options); err != nil {
		return logWrap(err, wrapErr("new logical router %s nat 'type %s external ip %s logical ip %s logical port %s external mac %s': %w", lrName, natType, externalIP, logicalIP, lspName, externalMac))
	}

	if err := c.CreateNats(lrName, nat); err != nil {
		return logWrap(err, wrapErr("add nat 'type %s external ip %s logical ip %s logical port %s external mac %s' to logical router %s: %w", natType, externalIP, logicalIP, lspName, externalMac, lrName))
	}

	return nil
}

// UpdateNat update nat
func (c *OVNNbClient) UpdateNat(nat *ovnnb.NAT, fields ...any) error {
	if nat == nil {
		return errors.New("nat is nil")
	}

	return c.updateModelLogged("net-update", nat, func(err error) error {
		return fmt.Errorf("update nat 'type %s external ip %s logical ip %s': %w", nat.Type, nat.ExternalIP, nat.LogicalIP, err)
	}, fields...)
}

// DeleteNat delete several nat rule once
func (c *OVNNbClient) DeleteNats(lrName, natType, logicalIP string) error {
	nats, err := c.ListNats(lrName, natType, logicalIP, nil)
	if err != nil {
		return logWrap(err, wrapErr("list logical router %s nats 'type %s logical ip %s': %w", lrName, natType, logicalIP))
	}
	uuids := rowUUIDs(nats, func(nat *ovnnb.NAT) string { return nat.UUID })
	return c.detachRouterUUIDs(lrName, uuids, "nats-del", "nats", c.LogicalRouterUpdateNatOp)
}

func (c *OVNNbClient) DeleteNat(lrName, natType, externalIP, logicalIP string) error {
	nat, err := c.GetNat(lrName, natType, externalIP, logicalIP, true)
	if err != nil {
		return logErr(err)
	}
	if nat == nil {
		// The NAT row may have already been removed (by another reconcile
		// or direct lr-nat-del). Return success so callers (e.g. OvnFip)
		// can clear their finalizer without requeuing forever.
		return nil
	}
	return c.detachRouterUUIDs(lrName, []string{nat.UUID}, "lr-nat-del", "nat", c.LogicalRouterUpdateNatOp)
}

func (c *OVNNbClient) GetNATByUUID(uuid string) (*ovnnb.NAT, error) {
	return getIndexedLogged(c.Database, &ovnnb.NAT{UUID: uuid})
}

func requireNatSpec(lrName, natType, externalIP, logicalIP string) error {
	if err := requireName(lrName, "the logical router name is required"); err != nil {
		return err
	}
	switch natType {
	case ovnnb.NATTypeDNAT:
		return logErr(errors.New("does not support dnat for now"))
	case ovnnb.NATTypeSNAT:
		if err := requireValue(logicalIP, fmt.Errorf("logical ip is required when nat type is %s", natType)); err != nil {
			return err
		}
		return requireValue(externalIP, fmt.Errorf("external ip is required when nat type is %s", natType))
	case ovnnb.NATTypeDNATAndSNAT:
		return requireValue(externalIP, fmt.Errorf("external ip is required when nat type is %s", natType))
	default:
		return logErr(errors.New("nat type must be one of [ snat, dnat_and_snat ]"))
	}
}

// GetNat retrieves a NAT rule by its identifying attributes.
// SNAT rules are uniquely identified by (lrName, natType, external_ip, logical_ip);
// external_ip is required and must not be empty for SNAT lookups.
// DNATAndSNAT rules are uniquely identified by (lrName, natType, external_ip).
func (c *OVNNbClient) GetNat(lrName, natType, externalIP, logicalIP string, ignoreNotFound bool) (*ovnnb.NAT, error) {
	// this is necessary because may exist same nat rule in different logical router
	if err := requireNatSpec(lrName, natType, externalIP, logicalIP); err != nil {
		return nil, err
	}

	fnFilter := func(nat *ovnnb.NAT) bool {
		if natType == "" {
			return nat.LogicalIP == logicalIP
		}
		if natType == ovnnb.NATTypeSNAT {
			return nat.Type == natType && nat.ExternalIP == externalIP && nat.LogicalIP == logicalIP
		}
		// For DNATAndSNAT: if logicalIP given, require externalIP+logicalIP.
		// Prevents stale Delete (old logicalIP) from clobbering new row.
		// Empty logicalIP keeps compat for Update/NatExists.
		if natType == ovnnb.NATTypeDNATAndSNAT {
			if nat.Type != natType || nat.ExternalIP != externalIP {
				return false
			}
			if logicalIP != "" && nat.LogicalIP != logicalIP {
				return false
			}
			return true
		}
		return nat.Type == natType && nat.ExternalIP == externalIP
	}
	natList, err := c.listLogicalRouterNatByFilter(lrName, fnFilter)
	if err != nil {
		return nil, logWrap(err, wrapErr("get logical router %s nat 'type %s external ip %s logical ip %s': %w", lrName, natType, externalIP, logicalIP))
	}

	return uniquePtrs(natList, ignoreNotFound,
		fmt.Errorf("not found logical router %s nat 'type %s external ip %s logical ip %s'", lrName, natType, externalIP, logicalIP),
		fmt.Errorf("more than one nat 'type %s external ip %s logical ip %s' in logical router %s", natType, externalIP, logicalIP, lrName),
	)
}

// ListNats list acls which match the given externalIDs
func (c *OVNNbClient) ListNats(lrName, natType, logicalIP string, externalIDs map[string]string) ([]*ovnnb.NAT, error) {
	return c.listLogicalRouterNatByFilter(lrName, natFilter(natType, logicalIP, externalIDs))
}

func (c *OVNNbClient) NatExists(lrName, natType, externalIP, logicalIP string) (bool, error) {
	nat, err := c.GetNat(lrName, natType, externalIP, logicalIP, true)
	return nat != nil, err
}

// newNat returns a NAT object with basic information.
// SNAT rules are uniquely identified by (lrName, natType, external_ip, logical_ip).
// DNATAndSNAT rules are uniquely identified by (lrName, natType, external_ip).
func (c *OVNNbClient) newNat(lrName, natType, externalIP, logicalIP, logicalMac, port string, options ...func(nat *ovnnb.NAT)) (*ovnnb.NAT, error) {
	if err := requireNatSpec(lrName, natType, externalIP, logicalIP); err != nil {
		return nil, err
	}

	exists, err := c.NatExists(lrName, natType, externalIP, logicalIP)
	if err != nil {
		return nil, logWrap(err, wrapErr("get logical router %s nat: %w", lrName))
	}

	// found, ignore
	if exists {
		return nil, nil
	}

	nat := &ovnnb.NAT{
		UUID:       ovsclient.NamedUUID(),
		Type:       natType,
		ExternalIP: externalIP,
		LogicalIP:  logicalIP,
	}
	if logicalMac != "" {
		nat.ExternalMAC = &logicalMac
	}
	if port != "" {
		nat.LogicalPort = &port
	}

	for _, option := range options {
		option(nat)
	}

	return nat, nil
}

// natFilter filter nat which match the given externalIDs,
// result should include all logicalIP nats when natType is empty,
// result should include all nats when externalIDs is empty,
// result should include all nats which externalIDs[key] is not empty when externalIDs[key] is ""
func natFilter(natType, logicalIP string, externalIDs map[string]string) func(nat *ovnnb.NAT) bool {
	return func(nat *ovnnb.NAT) bool {
		if !matchExternalIDs(nat.ExternalIDs, externalIDs) {
			return false
		}
		if len(natType) != 0 && nat.Type != natType {
			return false
		}
		if len(logicalIP) != 0 && nat.LogicalIP != logicalIP {
			return false
		}
		return true
	}
}

func (c *OVNNbClient) listLogicalRouterNatByFilter(lrName string, filter func(route *ovnnb.NAT) bool) ([]*ovnnb.NAT, error) {
	return c.listRouterChildren(lrName, func(lr *ovnnb.LogicalRouter) []string { return lr.Nat }, &ovnnb.NAT{}, func(nat *ovnnb.NAT) string { return nat.UUID }, filter)
}
