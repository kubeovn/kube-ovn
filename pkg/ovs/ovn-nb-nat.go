package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c *ovnClient) CreateBareNat(lrName, natType, externalIP, logicalIP string) error {
	nat, err := c.newNat(lrName, natType, externalIP, logicalIP)
	if err != nil {
		return err
	}

	op, err := c.ovnNbClient.Create(nat)
	if err != nil {
		return fmt.Errorf("generate operations for creating nat: %v", err)
	}

	if err = c.Transact("lr-nat-add", op); err != nil {
		return fmt.Errorf("create nat: %v", err)
	}

	return nil
}

// CreateNats create several logical router nat rule once
func (c *ovnClient) CreateNats(lrName string, nats ...*ovnnb.NAT) error {
	if len(nats) == 0 {
		return nil
	}

	models := make([]model.Model, 0, len(nats))
	natUUIDs := make([]string, 0, len(nats))
	for _, nat := range nats {
		if nat != nil {
			models = append(models, model.Model(nat))
			natUUIDs = append(natUUIDs, nat.UUID)
		}
	}

	createNatsOp, err := c.ovnNbClient.Create(models...)
	if err != nil {
		return fmt.Errorf("generate operations for creating nats: %v", err)
	}

	natAddOp, err := c.LogicalRouterUpdateNatOp(lrName, natUUIDs, ovsdb.MutateOperationInsert)
	if err != nil {
		return fmt.Errorf("generate operations for adding nats to logical router %s: %v", lrName, err)
	}

	ops := make([]ovsdb.Operation, 0, len(createNatsOp)+len(natAddOp))
	ops = append(ops, createNatsOp...)
	ops = append(ops, natAddOp...)

	if err = c.Transact("lr-nats-add", ops); err != nil {
		return fmt.Errorf("add nats to %s: %v", lrName, err)
	}

	return nil
}

// UpdateSnat update snat rule
func (c *ovnClient) UpdateSnat(lrName, externalIP, logicalIP string) error {
	natType := ovnnb.NATTypeSNAT

	nat, err := c.GetNat(lrName, natType, "", logicalIP, true)
	if err != nil {
		return err
	}

	// update external ip when nat exists
	if nat != nil {
		nat.ExternalIP = externalIP
		return c.UpdateNat(nat, &nat.ExternalIP)
	}

	/* create nat */
	if nat, err = c.newNat(lrName, natType, externalIP, logicalIP); err != nil {
		return fmt.Errorf("new logical router %s nat 'type %s external ip %s logical ip %s': %v", lrName, natType, externalIP, logicalIP, err)
	}

	if err := c.CreateNats(lrName, nat); err != nil {
		return fmt.Errorf("add nat 'type %s external ip %s logical ip %s' to logical router %s: %v", natType, externalIP, logicalIP, lrName, err)
	}

	return nil
}

// UpdateDnatAndSnat update dnat_and_snat rule
func (c *ovnClient) UpdateDnatAndSnat(lrName, externalIP, logicalIP, lspName, externalMac, gatewayType string) error {
	natType := ovnnb.NATTypeDNATAndSNAT

	nat, err := c.GetNat(lrName, natType, externalIP, "", true)
	if err != nil {
		return err
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

			if nil == nat.Options {
				nat.Options = make(map[string]string)
			}

			nat.Options["stateless"] = "true"
		}
	}

	/* create nat */
	if nat, err = c.newNat(lrName, natType, externalIP, logicalIP, options); err != nil {
		return fmt.Errorf("new logical router %s nat 'type %s external ip %s logical ip %s logical port %s external mac %s': %v", lrName, natType, externalIP, logicalIP, lspName, externalMac, err)
	}

	if err := c.CreateNats(lrName, nat); err != nil {
		return fmt.Errorf("add nat 'type %s external ip %s logical ip %s logical port %s external mac %s' to logical router %s: %v", natType, externalIP, logicalIP, lspName, externalMac, lrName, err)
	}

	return nil
}

// UpdateNat update nat
func (c *ovnClient) UpdateNat(nat *ovnnb.NAT, fields ...interface{}) error {
	if nat == nil {
		return fmt.Errorf("nat is nil")
	}

	op, err := c.ovnNbClient.Where(nat).Update(nat, fields...)
	if err != nil {
		return fmt.Errorf("generate operations for updating nat 'type %s external ip %s logical ip %s': %v", nat.Type, nat.ExternalIP, nat.LogicalIP, err)
	}

	if err = c.Transact("net-update", op); err != nil {
		return fmt.Errorf("update nat 'type %s external ip %s logical ip %s': %v", nat.Type, nat.ExternalIP, nat.LogicalIP, err)
	}

	return nil
}

// DeleteNat delete several nat rule once
func (c *ovnClient) DeleteNats(lrName, natType, logicalIP string) error {
	externalIDs := map[string]string{logicalRouterKey: lrName}

	/* delete nats from logical router */
	nats, err := c.ListNats(natType, logicalIP, externalIDs)
	if err != nil {
		return fmt.Errorf("list logical router %s nats 'type %s logical ip %s': %v", lrName, natType, logicalIP, err)
	}

	natsUUIDs := make([]string, 0, len(nats))
	for _, nat := range nats {
		natsUUIDs = append(natsUUIDs, nat.UUID)
	}

	removeNatOp, err := c.LogicalRouterUpdateNatOp(lrName, natsUUIDs, ovsdb.MutateOperationDelete)
	if err != nil {
		return fmt.Errorf("generate operations for deleting nats from logical router %s: %v", lrName, err)
	}

	// delete nats
	delNatsOp, err := c.WhereCache(natFilter(natType, logicalIP, externalIDs)).Delete()
	if err != nil {
		return fmt.Errorf("generate operation for deleting nats: %v", err)
	}

	ops := make([]ovsdb.Operation, 0, len(removeNatOp)+len(delNatsOp))
	ops = append(ops, removeNatOp...)
	ops = append(ops, delNatsOp...)

	if err = c.Transact("nats-del", ops); err != nil {
		return fmt.Errorf("del nats from logical router %s: %v", lrName, err)
	}

	return nil
}

// DeleteNat delete nat rule
func (c *ovnClient) DeleteNat(lrName, natType, externalIP, logicalIP string) error {
	nat, err := c.GetNat(lrName, natType, externalIP, logicalIP, false)
	if err != nil {
		return err
	}

	// remove nat from logical router
	removeNatOp, err := c.LogicalRouterUpdateNatOp(lrName, []string{nat.UUID}, ovsdb.MutateOperationDelete)
	if err != nil {
		return fmt.Errorf("generate operations for deleting nat from logical router %s: %v", lrName, err)
	}

	// delete nat
	delNatsOp, err := c.Where(nat).Delete()
	if err != nil {
		return fmt.Errorf("generate operation for deleting nat: %v", err)
	}

	ops := make([]ovsdb.Operation, 0, len(removeNatOp)+len(delNatsOp))
	ops = append(ops, removeNatOp...)
	ops = append(ops, delNatsOp...)

	if err = c.Transact("lr-nat-del", ops); err != nil {
		return fmt.Errorf("del nat from logical router %s: %v", lrName, err)
	}

	return nil
}

// GetNat get nat by some attribute,
// a nat rule is uniquely identified by router(lrName), type(natType) and logical_ip when snat
// a nat rule is uniquely identified by router(lrName), type(natType) and external_ip when dnat_and_snat
func (c *ovnClient) GetNat(lrName, natType, externalIP, logicalIP string, ignoreNotFound bool) (*ovnnb.NAT, error) {
	// this is necessary because may exist same nat rule in different logical router
	if len(lrName) == 0 {
		return nil, fmt.Errorf("the logical router name is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	if natType == ovnnb.NATTypeDNAT {
		return nil, fmt.Errorf("does not support dnat for now")
	}

	natList := make([]ovnnb.NAT, 0)
	if err := c.ovnNbClient.WhereCache(func(nat *ovnnb.NAT) bool {
		if len(nat.ExternalIDs) == 0 || nat.ExternalIDs[logicalRouterKey] != lrName {
			return false
		}

		if natType == ovnnb.NATTypeSNAT {
			return nat.Type == natType && nat.LogicalIP == logicalIP
		}

		return nat.Type == natType && nat.ExternalIP == externalIP
	}).List(ctx, &natList); err != nil {
		return nil, fmt.Errorf("get logical router %s nat 'type %s external ip %s logical ip %s': %v", lrName, natType, externalIP, logicalIP, err)
	}

	// not found
	if len(natList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}

		return nil, fmt.Errorf("not found logical router %s nat 'type %s external ip %s logical ip %s'", lrName, natType, externalIP, logicalIP)
	}

	if len(natList) > 1 {
		return nil, fmt.Errorf("more than one nat 'type %s external ip %s logical ip %s' in logical router %s", natType, externalIP, logicalIP, lrName)
	}

	return &natList[0], nil
}

// ListNats list acls which match the given externalIDs
func (c *ovnClient) ListNats(natType, logicalIP string, externalIDs map[string]string) ([]ovnnb.NAT, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	natList := make([]ovnnb.NAT, 0)

	if err := c.WhereCache(natFilter(natType, logicalIP, externalIDs)).List(ctx, &natList); err != nil {
		return nil, fmt.Errorf("list acls: %v", err)
	}

	return natList, nil
}

func (c *ovnClient) NatExists(lrName, natType, externalIP, logicalIP string) (bool, error) {
	nat, err := c.GetNat(lrName, natType, externalIP, logicalIP, true)
	return nat != nil, err
}

// newNat return net with basic information
func (c *ovnClient) newNat(lrName, natType, externalIP, logicalIP string, options ...func(nat *ovnnb.NAT)) (*ovnnb.NAT, error) {
	if len(lrName) == 0 {
		return nil, fmt.Errorf("the logical router name is required")
	}

	if natType != ovnnb.NATTypeSNAT && natType != ovnnb.NATTypeDNATAndSNAT {
		return nil, fmt.Errorf("nat type must one of [ snat, dnat_and_snat ]")
	}

	if len(externalIP) == 0 || len(logicalIP) == 0 {
		return nil, fmt.Errorf("nat 'externalIP %s' and 'logicalIP %s' is required", externalIP, logicalIP)
	}

	exists, err := c.NatExists(lrName, natType, externalIP, logicalIP)
	if err != nil {
		return nil, fmt.Errorf("get logical router %s nat: %v", lrName, err)
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
		ExternalIDs: map[string]string{
			logicalRouterKey: lrName,
		},
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
		if len(nat.ExternalIDs) < len(externalIDs) {
			return false
		}

		if len(nat.ExternalIDs) != 0 {
			for k, v := range externalIDs {
				// if only key exist but not value in externalIDs, we should include this lsp,
				// it's equal to shell command `ovn-nbctl --columns=xx find nat external_ids:key!=\"\"`
				if len(v) == 0 {
					if len(nat.ExternalIDs[k]) == 0 {
						return false
					}
				} else {
					if nat.ExternalIDs[k] != v {
						return false
					}
				}
			}
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
