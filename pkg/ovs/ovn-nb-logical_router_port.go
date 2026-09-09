package ovs

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

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
		return logErr(err)
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
	return c.transactGenerated("lrp-add", ops, err, nil,
		wrapErr("create peer router port %s for logical router%s: %w", localRouterPort, localRouter),
	)
}

func (c *OVNNbClient) requireLogicalRouterPort(lrpName string) (*ovnnb.LogicalRouterPort, error) {
	return logRet(c.GetLogicalRouterPort(lrpName, false))
}

func (c *OVNNbClient) UpdateLogicalRouterPortRA(lrpName, ipv6RAConfigsStr string, enableIPv6RA bool) error {
	lrp, err := c.requireLogicalRouterPort(lrpName)
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

func (c *OVNNbClient) UpdateLogicalRouterPortNetworks(lrpName string, networks []string) error {
	lrp, err := c.requireLogicalRouterPort(lrpName)
	if err != nil {
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

	lrp, err := c.requireLogicalRouterPort(lrpName)
	if err != nil {
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
	lrp, err := c.requireLogicalRouterPort(lrpName)
	if err != nil {
		return err
	}
	group, err := c.GetHAChassisGroup(haChassisGroupName, false)
	if err != nil {
		return logErr(err)
	}

	lrp.HaChassisGroup = &group.UUID
	return c.UpdateLogicalRouterPort(lrp, &lrp.HaChassisGroup)
}

// UpdateLogicalRouterPort update logical router port
func (c *OVNNbClient) UpdateLogicalRouterPort(lrp *ovnnb.LogicalRouterPort, fields ...any) error {
	if lrp == nil {
		return errors.New("logical_router_port is nil")
	}

	return c.updateModelLogged("lrp-update", lrp, func(err error) error {
		return fmt.Errorf("update logical router port %s: %w", lrp.Name, err)
	}, fields...)
}

func (c *OVNNbClient) createLRPIfAbsent(lrName string, lrp *ovnnb.LogicalRouterPort) error {
	exists, err := c.LogicalRouterPortExists(lrp.Name)
	if err != nil {
		return logErr(err)
	}
	if exists {
		return c.EnsureLogicalRouterPortParent(lrp.Name, lrName)
	}

	op, err := c.CreateLogicalRouterPortOp(lrp, lrName)
	if err != nil {
		return logFmt("generate operations for creating logical router port %s: %w", lrp.Name, err)
	}
	if err := transactOps(c, "lrp-add", op); err != nil {
		return logFmt("create logical router port %s: %w", lrp.Name, err)
	}
	return nil
}

// CreateLogicalRouterPort create logical router port with basic configuration
func (c *OVNNbClient) CreateLogicalRouterPort(lrName, lrpName, mac string, networks []string) error {
	if mac == "" {
		mac = util.GenerateMac()
	}
	return c.createLRPIfAbsent(lrName, &ovnnb.LogicalRouterPort{
		UUID:     ovsclient.NamedUUID(),
		Name:     lrpName,
		MAC:      mac,
		Networks: networks,
		ExternalIDs: map[string]string{
			"vendor": util.CniTypeName,
		},
	})
}

// DeleteLogicalRouterPort delete logical router port from logical router
func (c *OVNNbClient) DeleteLogicalRouterPorts(externalIDs map[string]string, filter func(lrp *ovnnb.LogicalRouterPort) bool) error {
	lrpList, err := c.ListLogicalRouterPorts(externalIDs, filter)
	if err != nil {
		return logFmt("list logical router ports: %w", err)
	}

	ops := make([]ovsdb.Operation, 0, len(lrpList))
	for _, lrp := range lrpList {
		op, err := c.DeleteLogicalRouterPortOp(lrp.Name)
		if err != nil {
			return logWrap(err, wrapErr("generate operations for deleting logical router port %s: %w", lrp.Name))
		}
		ops = append(ops, op...)
	}

	return c.transactGenerated("lrps-del", ops, nil, nil, wrapErr("del logical router ports: %w"))
}

// DeleteLogicalRouterPort delete logical router port from logical router
func (c *OVNNbClient) DeleteLogicalRouterPort(lrpName string) error {
	ops, err := c.DeleteLogicalRouterPortOp(lrpName)
	return c.transactGenerated("lrp-del", ops, err,
		wrapErr("generate operations for deleting logical router port %s: %w", lrpName),
		func(error) error { return fmt.Errorf("delete logical router port %s", lrpName) },
	)
}

// GetLogicalRouterPort get logical router port by name
func (c *OVNNbClient) GetLogicalRouterPort(lrpName string, ignoreNotFound bool) (*ovnnb.LogicalRouterPort, error) {
	return getIndexedWrap(c.Database, &ovnnb.LogicalRouterPort{Name: lrpName}, ignoreNotFound, func(err error) error {
		return fmt.Errorf("get logical router port %s: %w", lrpName, err)
	})
}

// GetLogicalRouterPortByUUID get logical router port by UUID
func (c *OVNNbClient) GetLogicalRouterPortByUUID(uuid string) (*ovnnb.LogicalRouterPort, error) {
	return getIndexedWrap(c.Database, &ovnnb.LogicalRouterPort{UUID: uuid}, false, func(err error) error {
		return fmt.Errorf("get logical router port by UUID %s: %w", uuid, err)
	})
}

// ListLogicalRouterPorts list logical router ports
func (c *OVNNbClient) ListLogicalRouterPorts(externalIDs map[string]string, filter func(lrp *ovnnb.LogicalRouterPort) bool) ([]ovnnb.LogicalRouterPort, error) {
	lrpList, err := filterTimeout(c.Database, &ovnnb.LogicalRouterPort{}, logicalRouterPortFilter(externalIDs, filter))
	if err != nil {
		return nil, logFmt("list logical router ports: %w", err)
	}
	return lrpList, nil
}

func (c *OVNNbClient) LogicalRouterPortExists(lrpName string) (bool, error) {
	lrp, err := c.GetLogicalRouterPort(lrpName, true)
	if err != nil {
		return false, logFmt("get logical router port %s: %w", lrpName, err)
	}
	return lrp != nil, err
}

// LogicalRouterPortUpdateGatewayChassisOp create operations add to or delete gateway chassis from logical router port
func (c *OVNNbClient) LogicalRouterPortUpdateGatewayChassisOp(lrpName string, uuids []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	return c.logicalRouterPortTable().mutateUUIDs(lrpName, func(lrp *ovnnb.LogicalRouterPort) *[]string { return &lrp.GatewayChassis }, uuids, op)
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

	models, uuids := modelsAndUUIDs([]*ovnnb.LogicalRouterPort{lrp}, func(port *ovnnb.LogicalRouterPort) string { return port.UUID })
	ops, err := createAndAttachOps(c, &ovnnb.LogicalRouterPort{}, models, func(ids []string) ([]ovsdb.Operation, error) {
		return c.LogicalRouterUpdatePortOp(lrName, ids[0], ovsdb.MutateOperationInsert)
	}, uuids)
	if err != nil {
		return nil, logFmt("generate operations for creating logical router port %s: %w", lrp.Name, err)
	}
	return ops, nil
}

// DeleteLogicalRouterPortOp create operation which delete logical router port
func (c *OVNNbClient) DeleteLogicalRouterPortOp(lrpName string) ([]ovsdb.Operation, error) {
	lrp, err := c.GetLogicalRouterPort(lrpName, true)
	if err != nil {
		return nil, logFmt("get logical router port %s when generate delete operations: %w", lrpName, err)
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
		return nil, logErr(err)
	}

	ops, err := c.logicalRouterPortTable().mutate(lrp, mutationsFunc...)
	if err != nil {
		return nil, logFmt("generate operations for mutating logical router port %s: %w", lrpName, err)
	}

	return ops, nil
}

func (c *OVNNbClient) logicalRouterPortTable() namedTable[ovnnb.LogicalRouterPort] {
	table := newNamedTable(c.Database, &ovnnb.LogicalRouterPort{}, "logical router port",
		func(lrp *ovnnb.LogicalRouterPort) string { return lrp.Name },
		func(lrp *ovnnb.LogicalRouterPort) map[string]string { return lrp.ExternalIDs })
	table.newByName = func(name string) *ovnnb.LogicalRouterPort { return &ovnnb.LogicalRouterPort{Name: name} }
	return table
}

// logicalRouterPortFilter filter logical router port which match the given externalIDs and external filter func
func logicalRouterPortFilter(externalIDs map[string]string, filter func(lrp *ovnnb.LogicalRouterPort) bool) func(lrp *ovnnb.LogicalRouterPort) bool {
	return func(lrp *ovnnb.LogicalRouterPort) bool {
		if !matchExternalIDs(lrp.ExternalIDs, externalIDs) {
			return false
		}
		return filter == nil || filter(lrp)
	}
}
