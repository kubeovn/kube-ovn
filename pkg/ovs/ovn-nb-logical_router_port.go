package ovs

import (
	"context"
	"fmt"
	"strings"

	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"

	"k8s.io/klog/v2"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *ovnClient) CreatePeerRouterPort(localRouter, remoteRouter, localRouterPortIP string) error {
	localRouterPort := fmt.Sprintf("%s-%s", localRouter, remoteRouter)
	remoteRouterPort := fmt.Sprintf("%s-%s", remoteRouter, localRouter)

	exist, err := c.LogicalRouterPortExists(localRouterPort)
	if err != nil {
		return err
	}

	// update networks when logical router port exists
	if exist {
		lrp := &ovnnb.LogicalRouterPort{
			Name:     localRouterPort,
			Networks: strings.Split(localRouterPortIP, ","),
		}
		return c.UpdateLogicalRouterPort(lrp, &lrp.Networks)
	}

	/* create logical router port */
	lrp := &ovnnb.LogicalRouterPort{
		UUID:     ovsclient.NamedUUID(),
		Name:     localRouterPort,
		MAC:      util.GenerateMac(),
		Networks: strings.Split(localRouterPortIP, ","),
		Peer:     &remoteRouterPort,
	}

	ops, err := c.CreateLogicalRouterPortOp(lrp, localRouter)
	if err != nil {
		return err
	}

	if err = c.Transact("lrp-add", ops); err != nil {
		return fmt.Errorf("create vpc external gateway logical router port %s: %v", localRouterPort, err)
	}

	return nil
}

func (c *ovnClient) UpdateLogicalRouterPortRA(lrpName, ipv6RAConfigsStr string, enableIPv6RA bool) error {
	lrp, err := c.GetLogicalRouterPort(lrpName, false)
	if err != nil {
		return err
	}

	if !enableIPv6RA {
		lrp.Ipv6Prefix = nil
		lrp.Ipv6RaConfigs = nil
	} else {
		lrp.Ipv6Prefix = getIpv6Prefix(lrp.Networks)
		lrp.Ipv6RaConfigs = parseIpv6RaConfigs(ipv6RAConfigsStr)

		// dhcpv6 works only with Ipv6Prefix and Ipv6RaConfigs
		if len(lrp.Ipv6Prefix) == 0 || len(lrp.Ipv6RaConfigs) == 0 {
			klog.Warningf("dhcpv6 works only with Ipv6Prefix and Ipv6RaConfigs")
			return nil
		}
	}

	return c.UpdateLogicalRouterPort(lrp, &lrp.Ipv6Prefix, &lrp.Ipv6RaConfigs)
}

// UpdateLogicalRouterPort update logical router port
func (c *ovnClient) UpdateLogicalRouterPort(lrp *ovnnb.LogicalRouterPort, fields ...interface{}) error {
	if lrp == nil {
		return fmt.Errorf("logical_router_port is nil")
	}

	op, err := c.Where(lrp).Update(lrp, fields...)
	if err != nil {
		return fmt.Errorf("generate operations for updating logical router port %s: %v", lrp.Name, err)
	}

	if err = c.Transact("lrp-update", op); err != nil {
		return fmt.Errorf("update logical router port %s: %v", lrp.Name, err)
	}

	return nil
}

// CreateLogicalRouterPort create logical router port with basic configuration
func (c *ovnClient) CreateLogicalRouterPort(lrName string, lrp *ovnnb.LogicalRouterPort) error {
	op, err := c.CreateLogicalRouterPortOp(lrp, lrName)
	if err != nil {
		return fmt.Errorf("generate operations for creating logical router port %s: %v", lrp.Name, err)
	}

	if err = c.Transact("lrp-add", op); err != nil {
		return fmt.Errorf("create logical router port %s: %v", lrp.Name, err)
	}

	return nil
}

// DeleteLogicalRouterPort delete logical router port from logical router
func (c *ovnClient) DeleteLogicalRouterPort(lrpName string) error {
	ops, err := c.DeleteLogicalRouterPortOp(lrpName)
	if err != nil {
		return err
	}

	if err = c.Transact("lrp-del", ops); err != nil {
		return fmt.Errorf("delete logical router port %s", lrpName)
	}

	return nil
}

// GetLogicalRouterPort get logical router port by name,
func (c *ovnClient) GetLogicalRouterPort(lrpName string, ignoreNotFound bool) (*ovnnb.LogicalRouterPort, error) {
	lrp := &ovnnb.LogicalRouterPort{Name: lrpName}
	if err := c.Get(context.TODO(), lrp); err != nil {
		if ignoreNotFound && err == client.ErrNotFound {
			return nil, nil
		}

		return nil, fmt.Errorf("get logical router port %s: %v", lrpName, err)
	}

	return lrp, nil
}

func (c *ovnClient) LogicalRouterPortExists(lrpName string) (bool, error) {
	lrp, err := c.GetLogicalRouterPort(lrpName, true)
	return lrp != nil, err
}

func (c *ovnClient) AddLogicalRouterPort(lr, name, mac, networks string) error {
	router, err := c.GetLogicalRouter(lr, false)
	if err != nil {
		return err
	}

	if mac == "" {
		mac = util.GenerateMac()
	}

	var ops []ovsdb.Operation

	lrp := &ovnnb.LogicalRouterPort{
		UUID:        ovsclient.NamedUUID(),
		Name:        name,
		MAC:         mac,
		Networks:    strings.Split(networks, ","),
		ExternalIDs: map[string]string{"vendor": util.CniTypeName},
	}

	// ensure there is no port in the same name, before we create it in the transaction
	waitOp := ConstructWaitForNameNotExistsOperation(name, "Logical_Router_Port")
	ops = append(ops, waitOp)

	createOps, err := c.ovnNbClient.Create(lrp)
	if err != nil {
		return err
	}
	ops = append(ops, createOps...)

	mutationOps, err := c.ovnNbClient.
		Where(router).
		Mutate(router,
			model.Mutation{
				Field:   &router.Ports,
				Mutator: ovsdb.MutateOperationInsert,
				Value:   []string{lrp.UUID},
			},
		)
	if err != nil {
		return err
	}
	ops = append(ops, mutationOps...)

	if err := Transact(c.ovnNbClient, "lrp-add", ops, c.ovnNbClient.Timeout); err != nil {
		return fmt.Errorf("failed to create logical router port %s: %v", name, err)
	}
	return nil
}

// CreateLogicalRouterPortOp create operation which create logical router port
func (c *ovnClient) CreateLogicalRouterPortOp(lrp *ovnnb.LogicalRouterPort, lrName string) ([]ovsdb.Operation, error) {
	if lrp == nil {
		return nil, fmt.Errorf("logical_router_port is nil")
	}

	if lrp.ExternalIDs == nil {
		lrp.ExternalIDs = make(map[string]string)
	}

	// attach necessary info
	lrp.ExternalIDs[logicalRouterKey] = lrName

	/* create logical router port */
	lrpCreateOp, err := c.Create(lrp)
	if err != nil {
		return nil, fmt.Errorf("generate operations for creating logical router port %s: %v", lrp.Name, err)
	}

	/* add logical router port to logical router*/
	lrpAddOp, err := c.LogicalRouterUpdatePortOp(lrName, lrp.UUID, ovsdb.MutateOperationInsert)
	if err != nil {
		return nil, err
	}

	ops := make([]ovsdb.Operation, 0, len(lrpCreateOp)+len(lrpAddOp))
	ops = append(ops, lrpCreateOp...)
	ops = append(ops, lrpAddOp...)

	return ops, nil
}

// DeleteLogicalRouterPortOp create operation which delete logical router port
func (c *ovnClient) DeleteLogicalRouterPortOp(lrpName string) ([]ovsdb.Operation, error) {
	lrp, err := c.GetLogicalRouterPort(lrpName, true)
	if err != nil {
		return nil, fmt.Errorf("get logical router port %s when generate delete operations: %v", lrpName, err)
	}

	// not found, skip
	if lrp == nil {
		return nil, nil
	}

	lrName, ok := lrp.ExternalIDs[logicalRouterKey]
	if !ok {
		return nil, fmt.Errorf("no %s exist in lsp's external_ids", logicalRouterKey)
	}

	// remove logical router port from logical router
	lrpRemoveOp, err := c.LogicalRouterUpdatePortOp(lrName, lrp.UUID, ovsdb.MutateOperationDelete)
	if err != nil {
		return nil, err
	}

	// delete logical router port
	lrpDelOp, err := c.Where(lrp).Delete()
	if err != nil {
		return nil, err
	}

	ops := make([]ovsdb.Operation, 0, len(lrpRemoveOp)+len(lrpDelOp))
	ops = append(ops, lrpRemoveOp...)
	ops = append(ops, lrpDelOp...)

	return ops, nil
}
