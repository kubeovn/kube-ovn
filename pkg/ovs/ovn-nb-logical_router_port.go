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

const (
	logicalRouterKey = "logical_router"
)

// CreateVpcExGwLogicalRouterPort create logical router port for vpc external gateway
func (c OvnClient) CreateVpcExGwLogicalRouterPort(lrName, mac, ip, lrpName string, chassises []string) error {
	/* create gateway chassises */
	err := c.CreateGatewayChassises(lrpName, chassises)
	if nil != err {
		return err
	}

	/* create logical router port */
	lrp := &ovnnb.LogicalRouterPort{
		UUID:           ovsclient.UUID(),
		Name:           lrpName,
		MAC:            mac,
		Networks:       []string{fmt.Sprintf("%s/24", ip)},
		GatewayChassis: chassises,
	}

	ops, err := c.CreateLogicalRouterPortOp(lrp, lrName)
	if nil != err {
		return err
	}

	if err = c.Transact("lrp-add", ops); err != nil {
		return fmt.Errorf("create vpc external gateway logical router port %s: %v", lrpName, err)
	}

	return nil
}

func (c OvnClient) CreatePeerRouterPort(localRouter, remoteRouter, localRouterPortIP string) error {
	localRouterPort := fmt.Sprintf("%s-%s", localRouter, remoteRouter)
	remoteRouterPort := fmt.Sprintf("%s-%s", remoteRouter, localRouter)

	exist, err := c.LogicalRouterPortExists(localRouterPort)
	if nil != err {
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
		UUID:     ovsclient.UUID(),
		Name:     localRouterPort,
		MAC:      util.GenerateMac(),
		Networks: strings.Split(localRouterPortIP, ","),
		Peer:     &remoteRouterPort,
	}

	ops, err := c.CreateLogicalRouterPortOp(lrp, localRouter)
	if nil != err {
		return err
	}

	if err = c.Transact("lrp-add", ops); err != nil {
		return fmt.Errorf("create vpc external gateway logical router port %s: %v", localRouterPort, err)
	}

	return nil
}

func (c *OvnClient) UpdateRouterPortIPv6RA(lrpName, ipv6RAConfigsStr string, enableIPv6RA bool) error {
	lrp, err := c.GetLogicalRouterPort(lrpName, false)
	if nil != err {
		return err
	}

	if !enableIPv6RA {
		lrp.Ipv6Prefix = nil
		lrp.Ipv6RaConfigs = nil
	} else {
		lrp.Ipv6Prefix = getIpv6Prefix(lrp.Networks)
		lrp.Ipv6RaConfigs = parseIpv6RaConfigs(ipv6RAConfigsStr)

		// dhcpv6 works only with Ipv6Prefix and Ipv6RaConfigs
		if 0 == len(lrp.Ipv6Prefix) || 0 == len(lrp.Ipv6RaConfigs) {
			klog.Warningf("dhcpv6 works only with Ipv6Prefix and Ipv6RaConfigs")
			return nil
		}
	}

	return c.UpdateLogicalRouterPort(lrp, &lrp.Ipv6Prefix, &lrp.Ipv6RaConfigs)
}

// CreateVpcExGwLogicalRouterPort create logical router port for vpc external gateway
func (c OvnClient) UpdateLogicalRouterPort(lrp *ovnnb.LogicalRouterPort, fields ...interface{}) error {
	if nil == lrp {
		return fmt.Errorf("logical_router_port is nil")
	}

	op, err := c.Where(lrp).Update(lrp, fields...)
	if err != nil {
		return fmt.Errorf("generate update operations for logical router port %s: %v", lrp.Name, err)
	}

	err = c.Transact("lrp-set", op)
	if err != nil {
		return fmt.Errorf("update logical router port %s: %v", lrp.Name, err)
	}

	return nil
}

// CreateLogicalRouterPort create logical router port
func (c OvnClient) CreateLogicalRouterPort(lrp *ovnnb.LogicalRouterPort, lrName string) error {
	op, err := c.CreateLogicalRouterPortOp(lrp, lrName)
	if err != nil {
		return fmt.Errorf("generate create operations for logical router port %s: %v", lrp.Name, err)
	}

	err = c.Transact("lrp-add", op)
	if err != nil {
		return fmt.Errorf("create logical router port %s: %v", lrp.Name, err)
	}

	return nil
}

// DeleteLogicalRouterPort delete logical router port from logical router
func (c OvnClient) DeleteLogicalRouterPort(name string) error {
	lrp, err := c.GetLogicalRouterPort(name, true)
	if nil != err {
		return err
	}

	ops, err := c.DeleteLogicalRouterPortOp(lrp)
	if nil != err {
		return err
	}

	err = c.Transact("lrp-del", ops)
	if nil != err {
		return fmt.Errorf("delete logical router port %s", name)
	}

	return nil
}

func (c OvnClient) GetLogicalRouterPort(name string, ignoreNotFound bool) (*ovnnb.LogicalRouterPort, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	lrp := &ovnnb.LogicalRouterPort{Name: name}
	if err := c.ovnNbClient.Get(ctx, lrp); err != nil {
		if ignoreNotFound && err == client.ErrNotFound {
			return nil, nil
		}

		return nil, fmt.Errorf("get logical router port %s: %v", name, err)
	}

	return lrp, nil
}

func (c OvnClient) LogicalRouterPortExists(name string) (bool, error) {
	lrp, err := c.GetLogicalRouterPort(name, true)
	return lrp != nil, err
}

func (c OvnClient) AddLogicalRouterPort(lr, name, mac, networks string) error {
	router, err := c.GetLogicalRouter(lr, false)
	if err != nil {
		return err
	}

	if mac == "" {
		mac = util.GenerateMac()
	}

	lrp := &ovnnb.LogicalRouterPort{
		UUID:        ovsclient.NamedUUID(),
		Name:        name,
		MAC:         mac,
		Networks:    strings.Split(networks, ","),
		ExternalIDs: map[string]string{"vendor": util.CniTypeName},
	}

	// ensure there is no port in the same name, before we create it in the transaction
	waitOp := ConstructWaitForNameNotExistsOperation(name, "Logical_Router_Port")
	ops := []ovsdb.Operation{waitOp}

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
	klog.Infof("add vpc lrp %s, networks %s", name, networks)
	if err := Transact(c.ovnNbClient, "lrp-add", ops, c.ovnNbClient.Timeout); err != nil {
		return fmt.Errorf("failed to create logical router port %s: %v", name, err)
	}
	return nil
}

// CreateLogicalRouterPortOp create operation which create logical router port
func (c OvnClient) CreateLogicalRouterPortOp(lrp *ovnnb.LogicalRouterPort, lrName string) ([]ovsdb.Operation, error) {
	if nil == lrp {
		return nil, fmt.Errorf("logical_router_port is nil")
	}

	if nil == lrp.ExternalIDs {
		lrp.ExternalIDs = make(map[string]string)
	}

	// attach necessary info
	lrp.ExternalIDs[logicalRouterKey] = lrName

	/* create logical router port */
	lrpCreateOp, err := c.Create(lrp)
	if err != nil {
		return nil, fmt.Errorf("generate create operations for logical router port %s: %v", lrp.Name, err)
	}

	/* add logical router port to logical router*/
	lrpAddOp, err := c.LogicalRouterOp(lrName, lrp.UUID, true)
	if nil != err {
		return nil, err
	}

	ops := make([]ovsdb.Operation, 0, len(lrpCreateOp)+len(lrpAddOp))
	ops = append(ops, lrpCreateOp...)
	ops = append(ops, lrpAddOp...)

	return ops, nil
}

// DeleteLogicalRouterPortOp create operation which delete logical router port
func (c OvnClient) DeleteLogicalRouterPortOp(lrp *ovnnb.LogicalRouterPort) ([]ovsdb.Operation, error) {
	// not found, skip
	if nil == lrp {
		return nil, nil
	}

	lrName, ok := lrp.ExternalIDs[logicalRouterKey]
	if !ok {
		return nil, fmt.Errorf("no %s exist in lsp's external_ids", logicalRouterKey)
	}

	// delete logical router port from logical router
	lrpRemoveOp, err := c.LogicalRouterOp(lrName, lrp.UUID, false)
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
