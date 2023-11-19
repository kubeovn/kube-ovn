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

func (c *OVNNbClient) CreatePeerRouterPort(localRouter, remoteRouter, localRouterPortIP string) error {
	localRouterPort := fmt.Sprintf("%s-%s", localRouter, remoteRouter)
	remoteRouterPort := fmt.Sprintf("%s-%s", remoteRouter, localRouter)

	exist, err := c.LogicalRouterPortExists(localRouterPort)
	if err != nil {
		klog.Error(err)
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
		klog.Error(err)
		return err
	}

	if err = c.Transact("lrp-add", ops); err != nil {
		err := fmt.Errorf("create peer router port %s for logical router%s: %v", localRouterPort, localRouter, err)
		klog.Error(err)
		return err
	}

	return nil
}

func (c *OVNNbClient) UpdateLogicalRouterPortRA(lrpName, ipv6RAConfigsStr string, enableIPv6RA bool) error {
	lrp, err := c.GetLogicalRouterPort(lrpName, false)
	if err != nil {
		klog.Error(err)
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

func (c *OVNNbClient) UpdateLogicalRouterPortOptions(lrpName string, options map[string]string) error {
	if len(options) == 0 {
		return nil
	}

	lrp, err := c.GetLogicalRouterPort(lrpName, false)
	if err != nil {
		klog.Error(err)
		return err
	}

	for k, v := range options {
		if len(v) == 0 {
			delete(lrp.Options, k)
		} else {
			if len(lrp.Options) == 0 {
				lrp.Options = make(map[string]string)
			}
			lrp.Options[k] = v
		}
	}

	return c.UpdateLogicalRouterPort(lrp, &lrp.Options)
}

// UpdateLogicalRouterPort update logical router port
func (c *OVNNbClient) UpdateLogicalRouterPort(lrp *ovnnb.LogicalRouterPort, fields ...interface{}) error {
	if lrp == nil {
		return fmt.Errorf("logical_router_port is nil")
	}

	op, err := c.Where(lrp).Update(lrp, fields...)
	if err != nil {
		err := fmt.Errorf("generate operations for updating logical router port %s: %v", lrp.Name, err)
		klog.Error(err)
		return err
	}

	if err = c.Transact("lrp-update", op); err != nil {
		err := fmt.Errorf("update logical router port %s: %v", lrp.Name, err)
		klog.Error(err)
		return err
	}

	return nil
}

// CreateLogicalRouterPort create logical router port with basic configuration
func (c *OVNNbClient) CreateLogicalRouterPort(lrName, lrpName, mac string, networks []string) error {
	exists, err := c.LogicalRouterPortExists(lrpName)
	if err != nil {
		klog.Error(err)
		return err
	}

	// ignore
	if exists {
		return nil
	}

	if mac == "" {
		mac = util.GenerateMac()
	}

	lrp := &ovnnb.LogicalRouterPort{
		UUID:     ovsclient.NamedUUID(),
		Name:     lrpName,
		MAC:      mac,
		Networks: networks,
	}

	op, err := c.CreateLogicalRouterPortOp(lrp, lrName)
	if err != nil {
		err := fmt.Errorf("generate operations for creating logical router port %s: %v", lrp.Name, err)
		klog.Error(err)
		return err
	}

	if err = c.Transact("lrp-add", op); err != nil {
		err := fmt.Errorf("create logical router port %s: %v", lrp.Name, err)
		klog.Error(err)
		return err
	}

	return nil
}

// DeleteLogicalRouterPort delete logical router port from logical router
func (c *OVNNbClient) DeleteLogicalRouterPorts(externalIDs map[string]string, filter func(lrp *ovnnb.LogicalRouterPort) bool) error {
	lrpList, err := c.ListLogicalRouterPorts(externalIDs, filter)
	if err != nil {
		err := fmt.Errorf("list logical router ports: %v", err)
		klog.Error(err)
		return err
	}

	ops := make([]ovsdb.Operation, 0, len(lrpList))
	for _, lrp := range lrpList {
		op, err := c.DeleteLogicalRouterPortOp(lrp.Name)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("generate operations for deleting logical router port %s: %v", lrp.Name, err)
		}
		ops = append(ops, op...)
	}

	if err := c.Transact("lrps-del", ops); err != nil {
		err := fmt.Errorf("del logical router ports: %v", err)
		klog.Error(err)
		return err
	}

	return nil
}

// DeleteLogicalRouterPort delete logical router port from logical router
func (c *OVNNbClient) DeleteLogicalRouterPort(lrpName string) error {
	ops, err := c.DeleteLogicalRouterPortOp(lrpName)
	if err != nil {
		err := fmt.Errorf("generate operations for deleting logical router port %s: %v", lrpName, err)
		klog.Error(err)
		return err
	}

	if err = c.Transact("lrp-del", ops); err != nil {
		err := fmt.Errorf("delete logical router port %s", lrpName)
		klog.Error(err)
		return err
	}

	return nil
}

// GetLogicalRouterPort get logical router port by name
func (c *OVNNbClient) GetLogicalRouterPort(lrpName string, ignoreNotFound bool) (*ovnnb.LogicalRouterPort, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	lrp := &ovnnb.LogicalRouterPort{Name: lrpName}
	if err := c.Get(ctx, lrp); err != nil {
		if ignoreNotFound && err == client.ErrNotFound {
			return nil, nil
		}
		err = fmt.Errorf("get logical router port %s: %v", lrpName, err)
		klog.Error(err)
		return nil, err
	}

	return lrp, nil
}

// GetLogicalRouterPortByUUID get logical router port by UUID
func (c *OVNNbClient) GetLogicalRouterPortByUUID(uuid string) (*ovnnb.LogicalRouterPort, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	lrp := &ovnnb.LogicalRouterPort{UUID: uuid}
	if err := c.Get(ctx, lrp); err != nil {
		err := fmt.Errorf("get logical router port by UUID %s: %v", uuid, err)
		klog.Error(err)
		return nil, err
	}

	return lrp, nil
}

// ListLogicalRouterPorts list logical router ports
func (c *OVNNbClient) ListLogicalRouterPorts(externalIDs map[string]string, filter func(lrp *ovnnb.LogicalRouterPort) bool) ([]ovnnb.LogicalRouterPort, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	lrpList := make([]ovnnb.LogicalRouterPort, 0)

	if err := c.WhereCache(logicalRouterPortFilter(externalIDs, filter)).List(ctx, &lrpList); err != nil {
		err := fmt.Errorf("list logical router ports: %v", err)
		klog.Error(err)
		return nil, err
	}

	return lrpList, nil
}

func (c *OVNNbClient) LogicalRouterPortExists(lrpName string) (bool, error) {
	lrp, err := c.GetLogicalRouterPort(lrpName, true)
	if err != nil {
		err := fmt.Errorf("get logical router port %s: %v", lrpName, err)
		klog.Error(err)
		return false, err
	}
	return lrp != nil, err
}

// LogicalRouterPortUpdateGatewayChassisOp create operations add to or delete gateway chassis from logical router port
func (c *OVNNbClient) LogicalRouterPortUpdateGatewayChassisOp(lrpName string, uuids []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	if len(uuids) == 0 {
		return nil, nil
	}

	mutation := func(lrp *ovnnb.LogicalRouterPort) *model.Mutation {
		mutation := &model.Mutation{
			Field:   &lrp.GatewayChassis,
			Value:   uuids,
			Mutator: op,
		}

		return mutation
	}

	return c.LogicalRouterPortOp(lrpName, mutation)
}

// CreateLogicalRouterPortOp create operation which create logical router port
func (c *OVNNbClient) CreateLogicalRouterPortOp(lrp *ovnnb.LogicalRouterPort, lrName string) ([]ovsdb.Operation, error) {
	if lrp == nil {
		return nil, fmt.Errorf("logical_router_port is nil")
	}

	if lrp.ExternalIDs == nil {
		lrp.ExternalIDs = make(map[string]string)
	}

	// attach necessary info
	lrp.ExternalIDs[logicalRouterKey] = lrName
	lrp.ExternalIDs["vendor"] = util.CniTypeName

	/* create logical router port */
	lrpCreateOp, err := c.Create(lrp)
	if err != nil {
		err := fmt.Errorf("generate operations for creating logical router port %s: %v", lrp.Name, err)
		klog.Error(err)
		return nil, err
	}

	/* add logical router port to logical router*/
	lrpAddOp, err := c.LogicalRouterUpdatePortOp(lrName, lrp.UUID, ovsdb.MutateOperationInsert)
	if err != nil {
		err := fmt.Errorf("generate operations for adding logical router port %s to logical router %s: %v", lrp.Name, lrName, err)
		klog.Error(err)
		return nil, err
	}

	ops := make([]ovsdb.Operation, 0, len(lrpCreateOp)+len(lrpAddOp))
	ops = append(ops, lrpCreateOp...)
	ops = append(ops, lrpAddOp...)

	return ops, nil
}

// DeleteLogicalRouterPortOp create operation which delete logical router port
func (c *OVNNbClient) DeleteLogicalRouterPortOp(lrpName string) ([]ovsdb.Operation, error) {
	lrp, err := c.GetLogicalRouterPort(lrpName, true)
	if err != nil {
		err := fmt.Errorf("get logical router port %s when generate delete operations: %v", lrpName, err)
		klog.Error(err)
		return nil, err
	}

	// not found, skip
	if lrp == nil {
		return nil, nil
	}

	// remove logical router port from logical router
	lrName := lrp.ExternalIDs[logicalRouterKey]
	return c.LogicalRouterUpdatePortOp(lrName, lrp.UUID, ovsdb.MutateOperationDelete)
}

// LogicalRouterPortOp create operations about logical router port
func (c *OVNNbClient) LogicalRouterPortOp(lrpName string, mutationsFunc ...func(lrp *ovnnb.LogicalRouterPort) *model.Mutation) ([]ovsdb.Operation, error) {
	lrp, err := c.GetLogicalRouterPort(lrpName, false)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	if len(mutationsFunc) == 0 {
		return nil, nil
	}

	mutations := make([]model.Mutation, 0, len(mutationsFunc))

	for _, f := range mutationsFunc {
		mutation := f(lrp)

		if mutation != nil {
			mutations = append(mutations, *mutation)
		}
	}

	ops, err := c.ovsDbClient.Where(lrp).Mutate(lrp, mutations...)
	if err != nil {
		err := fmt.Errorf("generate operations for mutating logical router port %s: %v", lrpName, err)
		klog.Error(err)
		return nil, err
	}

	return ops, nil
}

// logicalRouterPortFilter filter logical router port which match the given externalIDs and external filter func
func logicalRouterPortFilter(externalIDs map[string]string, filter func(lrp *ovnnb.LogicalRouterPort) bool) func(lrp *ovnnb.LogicalRouterPort) bool {
	return func(lrp *ovnnb.LogicalRouterPort) bool {
		if len(lrp.ExternalIDs) < len(externalIDs) {
			return false
		}

		if len(lrp.ExternalIDs) != 0 {
			for k, v := range externalIDs {
				// if only key exist but not value in externalIDs, we should include this lrp,
				// it's equal to shell command `ovn-nbctl --columns=xx find logical_router_port external_ids:key!=\"\"`
				if len(v) == 0 {
					if len(lrp.ExternalIDs[k]) == 0 {
						return false
					}
				} else {
					if lrp.ExternalIDs[k] != v {
						return false
					}
				}
			}
		}

		// need meet custom filter
		if filter != nil {
			return filter(lrp)
		}

		return true
	}
}

func (c *OVNNbClient) AddLogicalRouterPort(lr, name, mac, networks string) error {
	router, err := c.GetLogicalRouter(lr, false)
	if err != nil {
		klog.Error(err)
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

	createOps, err := c.ovsDbClient.Create(lrp)
	if err != nil {
		klog.Error(err)
		return err
	}
	ops = append(ops, createOps...)

	mutationOps, err := c.ovsDbClient.
		Where(router).
		Mutate(router,
			model.Mutation{
				Field:   &router.Ports,
				Mutator: ovsdb.MutateOperationInsert,
				Value:   []string{lrp.UUID},
			},
		)
	if err != nil {
		klog.Error(err)
		return err
	}
	ops = append(ops, mutationOps...)
	klog.Infof("add vpc lrp %s, networks %s", name, networks)
	if err := c.Transact("lrp-add", ops); err != nil {
		return fmt.Errorf("failed to create logical router port %s: %v", name, err)
	}
	return nil
}
