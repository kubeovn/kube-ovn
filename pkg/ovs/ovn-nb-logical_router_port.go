package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
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
		err := fmt.Errorf("create peer router port %s for logical router%s: %w", localRouterPort, localRouter, err)
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

func (c *OVNNbClient) UpdateLogicalRouterPortNetworks(lrpName string, networks []string) error {
	lrp, err := c.GetLogicalRouterPort(lrpName, false)
	if err != nil {
		klog.Error(err)
		return err
	}
	if slices.Equal(networks, lrp.Networks) {
		return nil
	}

	lrp.Networks = networks
	return c.UpdateLogicalRouterPort(lrp, &lrp.Networks)
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

	newOptions := maps.Clone(lrp.Options)
	for k, v := range options {
		if len(v) == 0 {
			delete(newOptions, k)
		} else {
			if len(newOptions) == 0 {
				newOptions = make(map[string]string)
			}
			newOptions[k] = v
		}
	}
	if maps.Equal(newOptions, lrp.Options) {
		return nil
	}

	lrp.Options = newOptions
	return c.UpdateLogicalRouterPort(lrp, &lrp.Options)
}

func (c *OVNNbClient) SetLogicalRouterPortHAChassisGroup(lrpName, haChassisGroupName string) error {
	lrp, err := c.GetLogicalRouterPort(lrpName, false)
	if err != nil {
		klog.Error(err)
		return err
	}
	group, err := c.GetHAChassisGroup(haChassisGroupName, false)
	if err != nil {
		klog.Error(err)
		return err
	}

	lrp.HaChassisGroup = &group.UUID
	return c.UpdateLogicalRouterPort(lrp, &lrp.HaChassisGroup)
}

// UpdateLogicalRouterPort update logical router port
func (c *OVNNbClient) UpdateLogicalRouterPort(lrp *ovnnb.LogicalRouterPort, fields ...any) error {
	if lrp == nil {
		return errors.New("logical_router_port is nil")
	}

	op, err := c.Where(lrp).Update(lrp, fields...)
	if err != nil {
		err := fmt.Errorf("generate operations for updating logical router port %s: %w", lrp.Name, err)
		klog.Error(err)
		return err
	}

	if err = c.Transact("lrp-update", op); err != nil {
		err := fmt.Errorf("update logical router port %s: %w", lrp.Name, err)
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
		ExternalIDs: map[string]string{
			"vendor": util.CniTypeName,
		},
	}

	op, err := c.CreateLogicalRouterPortOp(lrp, lrName)
	if err != nil {
		err := fmt.Errorf("generate operations for creating logical router port %s: %w", lrp.Name, err)
		klog.Error(err)
		return err
	}

	if err = c.Transact("lrp-add", op); err != nil {
		err := fmt.Errorf("create logical router port %s: %w", lrp.Name, err)
		klog.Error(err)
		return err
	}

	return nil
}

// DeleteLogicalRouterPort delete logical router port from logical router
func (c *OVNNbClient) DeleteLogicalRouterPorts(externalIDs map[string]string, filter func(lrp *ovnnb.LogicalRouterPort) bool) error {
	lrpList, err := c.ListLogicalRouterPorts(externalIDs, filter)
	if err != nil {
		err := fmt.Errorf("list logical router ports: %w", err)
		klog.Error(err)
		return err
	}

	ops := make([]ovsdb.Operation, 0, len(lrpList))
	for _, lrp := range lrpList {
		op, err := c.DeleteLogicalRouterPortOp(lrp.Name)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("generate operations for deleting logical router port %s: %w", lrp.Name, err)
		}
		ops = append(ops, op...)
	}

	if err := c.Transact("lrps-del", ops); err != nil {
		err := fmt.Errorf("del logical router ports: %w", err)
		klog.Error(err)
		return err
	}

	return nil
}

// DeleteLogicalRouterPort delete logical router port from logical router
func (c *OVNNbClient) DeleteLogicalRouterPort(lrpName string) error {
	ops, err := c.DeleteLogicalRouterPortOp(lrpName)
	if err != nil {
		err := fmt.Errorf("generate operations for deleting logical router port %s: %w", lrpName, err)
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
		if ignoreNotFound && errors.Is(err, client.ErrNotFound) {
			return nil, nil
		}
		err = fmt.Errorf("get logical router port %s: %w", lrpName, err)
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
		err := fmt.Errorf("get logical router port by UUID %s: %w", uuid, err)
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
		err := fmt.Errorf("list logical router ports: %w", err)
		klog.Error(err)
		return nil, err
	}

	return lrpList, nil
}

func (c *OVNNbClient) LogicalRouterPortExists(lrpName string) (bool, error) {
	lrp, err := c.GetLogicalRouterPort(lrpName, true)
	if err != nil {
		err := fmt.Errorf("get logical router port %s: %w", lrpName, err)
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
		return nil, errors.New("logical_router_port is nil")
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
		err := fmt.Errorf("generate operations for creating logical router port %s: %w", lrp.Name, err)
		klog.Error(err)
		return nil, err
	}

	/* add logical router port to logical router*/
	lrpAddOp, err := c.LogicalRouterUpdatePortOp(lrName, lrp.UUID, ovsdb.MutateOperationInsert)
	if err != nil {
		err := fmt.Errorf("generate operations for adding logical router port %s to logical router %s: %w", lrp.Name, lrName, err)
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
		err := fmt.Errorf("get logical router port %s when generate delete operations: %w", lrpName, err)
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
		if mutation := f(lrp); mutation != nil {
			mutations = append(mutations, *mutation)
		}
	}

	ops, err := c.ovsDbClient.Where(lrp).Mutate(lrp, mutations...)
	if err != nil {
		err := fmt.Errorf("generate operations for mutating logical router port %s: %w", lrpName, err)
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
