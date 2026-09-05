package ovn_ic_controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type icGatewayChassisProvider interface {
	ReconcileGatewayChassises(lrpName string, chassises []string) error
}

type icLogicalPatchPortProvider interface {
	CreateLogicalPatchPort(lsName, lrName, lspName, lrpName, ip, mac string, chassises ...string) error
}

func (c *Controller) reconcileICGatewayChassises(lrpName string, chassises []string) error {
	if provider, ok := c.OVNNbTables.(icGatewayChassisProvider); ok {
		return provider.ReconcileGatewayChassises(lrpName, chassises)
	}
	if c.OVNNbTables != nil {
		return errors.New("OVN NB table provider does not support gateway chassis reconciliation")
	}
	if c.OVNNbClient == nil {
		return errors.New("OVN NB client is nil")
	}
	return c.OVNNbClient.ReconcileGatewayChassises(lrpName, chassises)
}

func (c *Controller) createICLogicalPatchPort(lsName, lrName, lspName, lrpName, ip, mac string, chassises ...string) error {
	if provider, ok := c.OVNNbTables.(icLogicalPatchPortProvider); ok {
		return provider.CreateLogicalPatchPort(lsName, lrName, lspName, lrpName, ip, mac, chassises...)
	}
	if c.OVNNbTables != nil {
		return errors.New("OVN NB table provider does not support logical patch port creation")
	}
	if c.OVNNbClient == nil {
		return errors.New("OVN NB client is nil")
	}
	return c.OVNNbClient.CreateLogicalPatchPort(lsName, lrName, lspName, lrpName, ip, mac, chassises...)
}

func (c *Controller) setICAutoRouteTable(enable bool, blacklist []string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.SetICAutoRoute(enable, blacklist)
	}
	global, err := c.getNBGlobalTable()
	if err != nil {
		return err
	}
	options := maps.Clone(global.Options)
	if options == nil {
		options = make(map[string]string, 3)
	}
	if enable {
		options["ic-route-adv"] = "true"
		options["ic-route-learn"] = "true"
		options["ic-route-blacklist"] = strings.Join(blacklist, ",")
	} else {
		delete(options, "ic-route-adv")
		delete(options, "ic-route-learn")
		delete(options, "ic-route-blacklist")
	}
	if maps.Equal(options, global.Options) {
		return nil
	}
	global.Options = options
	return c.OVNNbTables.Table(&ovnnb.NBGlobal{}).Update(
		context.Background(), "ic-nb-global-update", global, global, &global.Options,
	)
}

func (c *Controller) setICAzNameTable(name string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.SetAzName(name)
	}
	global, err := c.getNBGlobalTable()
	if err != nil {
		return err
	}
	if global.Name == name {
		return nil
	}
	global.Name = name
	return c.OVNNbTables.Table(&ovnnb.NBGlobal{}).Update(
		context.Background(), "ic-nb-global-update", global, global, &global.Name,
	)
}

func (c *Controller) getNBGlobalTable() (*ovnnb.NBGlobal, error) {
	var rows []ovnnb.NBGlobal
	if err := c.OVNNbTables.Table(&ovnnb.NBGlobal{}).List(context.Background(), &rows); err != nil {
		return nil, fmt.Errorf("list NB_Global: %w", err)
	}
	switch len(rows) {
	case 0:
		return nil, errors.New("NB_Global row not found")
	case 1:
		return &rows[0], nil
	default:
		return nil, errors.New("more than one NB_Global row")
	}
}

func (c *Controller) listICLogicalSwitchPorts(filter func(*ovnnb.LogicalSwitchPort) bool) ([]ovnnb.LogicalSwitchPort, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ListLogicalSwitchPorts(false, nil, filter)
	}
	var rows []ovnnb.LogicalSwitchPort
	err := c.OVNNbTables.Table(&ovnnb.LogicalSwitchPort{}).Filter(
		context.Background(), filter, &rows,
	)
	return rows, err
}

func (c *Controller) logicalSwitchPortExistsTable(name string) (bool, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.LogicalSwitchPortExists(name)
	}
	rows, err := c.listICLogicalSwitchPorts(func(row *ovnnb.LogicalSwitchPort) bool {
		return row.Name == name
	})
	return len(rows) != 0, err
}

func (c *Controller) listICLogicalRouterPorts(filter func(*ovnnb.LogicalRouterPort) bool) ([]ovnnb.LogicalRouterPort, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ListLogicalRouterPorts(nil, filter)
	}
	var rows []ovnnb.LogicalRouterPort
	err := c.OVNNbTables.Table(&ovnnb.LogicalRouterPort{}).Filter(
		context.Background(), filter, &rows,
	)
	return rows, err
}

func (c *Controller) deleteICLogicalSwitchPorts(filter func(*ovnnb.LogicalSwitchPort) bool) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteLogicalSwitchPorts(nil, filter)
	}
	ports, err := c.listICLogicalSwitchPorts(filter)
	if err != nil {
		return fmt.Errorf("list logical switch ports: %w", err)
	}
	if len(ports) == 0 {
		return nil
	}
	var switches []ovnnb.LogicalSwitch
	if err := c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).List(context.Background(), &switches); err != nil {
		return fmt.Errorf("list logical switches: %w", err)
	}
	portIDs := make(map[string]struct{}, len(ports))
	for _, port := range ports {
		portIDs[port.UUID] = struct{}{}
	}
	parents := make(map[string][]string, len(ports))
	for _, port := range ports {
		for _, logicalSwitch := range switches {
			if slices.Contains(logicalSwitch.Ports, port.UUID) {
				parents[port.UUID] = append(parents[port.UUID], logicalSwitch.Name)
			}
		}
		if len(parents[port.UUID]) != 1 {
			return fmt.Errorf("expected one logical switch for logical switch port %s, found %d", port.UUID, len(parents[port.UUID]))
		}
	}
	var operations []ovsdb.Operation
	for i := range switches {
		ids := make([]string, 0)
		for _, uuid := range switches[i].Ports {
			if _, ok := portIDs[uuid]; ok && slices.Contains(parents[uuid], switches[i].Name) {
				ids = append(ids, uuid)
			}
		}
		if len(ids) == 0 {
			continue
		}
		ops, opErr := c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).MutateOps(&switches[i], model.Mutation{
			Field: &switches[i].Ports, Value: ids, Mutator: ovsdb.MutateOperationDelete,
		})
		if opErr != nil {
			return opErr
		}
		operations = append(operations, ops...)
	}
	if len(operations) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Transact(context.Background(), "ic-lsps-del", operations...)
}

func (c *Controller) deleteICLogicalRouterPorts(filter func(*ovnnb.LogicalRouterPort) bool) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteLogicalRouterPorts(nil, filter)
	}
	ports, err := c.listICLogicalRouterPorts(filter)
	if err != nil {
		return fmt.Errorf("list logical router ports: %w", err)
	}
	if len(ports) == 0 {
		return nil
	}
	var routers []ovnnb.LogicalRouter
	if err := c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).List(context.Background(), &routers); err != nil {
		return fmt.Errorf("list logical routers: %w", err)
	}
	portIDs := make(map[string]struct{}, len(ports))
	for _, port := range ports {
		portIDs[port.UUID] = struct{}{}
	}
	parents := make(map[string][]string, len(ports))
	for _, port := range ports {
		for _, logicalRouter := range routers {
			if slices.Contains(logicalRouter.Ports, port.UUID) {
				parents[port.UUID] = append(parents[port.UUID], logicalRouter.Name)
			}
		}
		if len(parents[port.UUID]) != 1 {
			return fmt.Errorf("expected one logical router for logical router port %s, found %d", port.UUID, len(parents[port.UUID]))
		}
	}
	var operations []ovsdb.Operation
	for i := range routers {
		ids := make([]string, 0)
		for _, uuid := range routers[i].Ports {
			if _, ok := portIDs[uuid]; ok && slices.Contains(parents[uuid], routers[i].Name) {
				ids = append(ids, uuid)
			}
		}
		if len(ids) == 0 {
			continue
		}
		ops, opErr := c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).MutateOps(&routers[i], model.Mutation{
			Field: &routers[i].Ports, Value: ids, Mutator: ovsdb.MutateOperationDelete,
		})
		if opErr != nil {
			return opErr
		}
		operations = append(operations, ops...)
	}
	if len(operations) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Transact(context.Background(), "ic-lrps-del", operations...)
}

func (c *Controller) deleteICLogicalSwitch(name string) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteLogicalSwitch(name)
	}
	var rows []ovnnb.LogicalSwitch
	if err := c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Filter(context.Background(), func(row *ovnnb.LogicalSwitch) bool {
		return row.Name == name
	}, &rows); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	if len(rows) > 1 {
		return fmt.Errorf("more than one logical switch with name %q", name)
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalSwitch{}).Delete(context.Background(), "ic-ls-del", &rows[0])
}

func (c *Controller) getICChassisByHost(hostname string) (*ovnsb.Chassis, error) {
	if c.OVNSbTables == nil {
		return c.OVNSbClient.GetChassisByHost(hostname)
	}
	var rows []ovnsb.Chassis
	if err := c.OVNSbTables.Table(&ovnsb.Chassis{}).Filter(context.Background(), func(row *ovnsb.Chassis) bool {
		return row.Hostname == hostname
	}, &rows); err != nil {
		return nil, err
	}
	switch len(rows) {
	case 0:
		return nil, fmt.Errorf("no chassis for host %q", hostname)
	case 1:
		return &rows[0], nil
	default:
		return nil, errors.New("one host maps to multiple chassis")
	}
}

func (c *Controller) listICLogicalRouters() ([]ovnnb.LogicalRouter, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ListLogicalRouter(false, nil)
	}
	var rows []ovnnb.LogicalRouter
	err := c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).List(context.Background(), &rows)
	return rows, err
}

func (c *Controller) getICLogicalRouter(name string) (*ovnnb.LogicalRouter, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.GetLogicalRouter(name, false)
	}
	rows, err := c.listICLogicalRouters()
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i].Name == name {
			return &rows[i], nil
		}
	}
	return nil, fmt.Errorf("logical router %q not found", name)
}

func (c *Controller) listICRoutes(lr *ovnnb.LogicalRouter, filter func(*ovnnb.LogicalRouterStaticRoute) bool) ([]*ovnnb.LogicalRouterStaticRoute, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ListLogicalRouterStaticRoutes(lr.Name, nil, nil, "", nil)
	}
	allowed := make(map[string]struct{}, len(lr.StaticRoutes))
	for _, uuid := range lr.StaticRoutes {
		allowed[uuid] = struct{}{}
	}
	var rows []*ovnnb.LogicalRouterStaticRoute
	err := c.OVNNbTables.Table(&ovnnb.LogicalRouterStaticRoute{}).Filter(context.Background(), func(row *ovnnb.LogicalRouterStaticRoute) bool {
		_, ok := allowed[row.UUID]
		return ok && (filter == nil || filter(row))
	}, &rows)
	return rows, err
}

func (c *Controller) deleteICRoutes(lr *ovnnb.LogicalRouter, routes []*ovnnb.LogicalRouterStaticRoute) error {
	if len(routes) == 0 {
		return nil
	}
	uuids := make([]string, 0, len(routes))
	for _, route := range routes {
		if route != nil {
			uuids = append(uuids, route.UUID)
		}
	}
	if len(uuids) == 0 {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Mutate(
		context.Background(), "ic-routes-del", lr,
		model.Mutation{Field: &lr.StaticRoutes, Value: slices.Clip(uuids), Mutator: ovsdb.MutateOperationDelete},
	)
}

func (c *Controller) listICPolicies(lr *ovnnb.LogicalRouter, filter func(*ovnnb.LogicalRouterPolicy) bool) ([]*ovnnb.LogicalRouterPolicy, error) {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.ListLogicalRouterPolicies(lr.Name, -1, nil, false)
	}
	allowed := make(map[string]struct{}, len(lr.Policies))
	for _, uuid := range lr.Policies {
		allowed[uuid] = struct{}{}
	}
	var rows []*ovnnb.LogicalRouterPolicy
	err := c.OVNNbTables.Table(&ovnnb.LogicalRouterPolicy{}).Filter(context.Background(), func(row *ovnnb.LogicalRouterPolicy) bool {
		_, ok := allowed[row.UUID]
		return ok && (filter == nil || filter(row))
	}, &rows)
	return rows, err
}

func (c *Controller) addICPolicy(lr *ovnnb.LogicalRouter, policy *ovnnb.LogicalRouterPolicy) error {
	if c.OVNNbTables == nil {
		return c.OVNNbClient.AddLogicalRouterPolicy(lr.Name, policy.Priority, policy.Match, policy.Action, policy.Nexthops, policy.BFDSessions, policy.ExternalIDs)
	}
	policy.UUID = ovsclient.NamedUUID()
	createOps, err := c.OVNNbTables.Table(&ovnnb.LogicalRouterPolicy{}).CreateOps(policy)
	if err != nil {
		return err
	}
	parentOps, err := c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).MutateOps(lr, model.Mutation{
		Field: &lr.Policies, Value: []string{policy.UUID}, Mutator: ovsdb.MutateOperationInsert,
	})
	if err != nil {
		return err
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouterPolicy{}).Transact(context.Background(), "ic-policy-add", append(createOps, parentOps...)...)
}

func (c *Controller) deleteICPolicyUUID(lr *ovnnb.LogicalRouter, uuid string) error {
	if uuid == "" {
		return nil
	}
	if c.OVNNbTables == nil {
		return c.OVNNbClient.DeleteLogicalRouterPolicyByUUID(lr.Name, uuid)
	}
	if !slices.Contains(lr.Policies, uuid) {
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Mutate(
		context.Background(), "ic-policy-del", lr,
		model.Mutation{Field: &lr.Policies, Value: []string{uuid}, Mutator: ovsdb.MutateOperationDelete},
	)
}

func (c *Controller) deleteICPolicies(lr *ovnnb.LogicalRouter, priority int, key, value string) error {
	policies, err := c.listICPolicies(lr, func(policy *ovnnb.LogicalRouterPolicy) bool {
		return policy.Priority == priority && policy.ExternalIDs[key] == value
	})
	if err != nil {
		return err
	}
	if len(policies) == 0 {
		return nil
	}
	uuids := make([]string, 0, len(policies))
	for _, policy := range policies {
		uuids = append(uuids, policy.UUID)
	}
	if c.OVNNbTables == nil {
		for _, uuid := range uuids {
			if err := c.deleteICPolicyUUID(lr, uuid); err != nil {
				return err
			}
		}
		return nil
	}
	return c.OVNNbTables.Table(&ovnnb.LogicalRouter{}).Mutate(
		context.Background(), "ic-policies-del", lr,
		model.Mutation{Field: &lr.Policies, Value: slices.Clip(uuids), Mutator: ovsdb.MutateOperationDelete},
	)
}

func (c *Controller) listICRemoteLogicalSwitchPortAddresses() ([]ovnnb.LogicalSwitchPort, error) {
	return c.listICLogicalSwitchPorts(func(row *ovnnb.LogicalSwitchPort) bool {
		return row.Type == "remote" && row.ExternalIDs["vendor"] == util.CniTypeName
	})
}
