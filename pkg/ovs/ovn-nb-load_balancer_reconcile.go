package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"reflect"
	"slices"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

// LoadBalancerDesiredState contains the complete forwarding and attachment
// state of one resource-owned LB. Unrelated options and external IDs survive.
type LoadBalancerDesiredState struct {
	Config          LoadBalancerConfig
	VIPs            map[string]string
	IPPortMappings  map[string]string
	HealthChecks    map[string]map[string]string
	LogicalSwitches []string
	LogicalRouters  []string
}

// LoadBalancerReconciliation replaces all LBs belonging to one resource.
// OwnerExternalIDs must identify that resource, independently of LB protocol
// or traffic class. Omitted owned LBs are deleted in the same transaction.
type LoadBalancerReconciliation struct {
	OwnerExternalIDs         map[string]string
	LoadBalancers            []LoadBalancerDesiredState
	VIPDeletions             []TemplateVIPDeletion
	TemplateVariables        []ChassisTemplateVariableReconciliation
	TemplateVariablePrefixes []string
	DeleteEmptyLoadBalancers []string
}

type loadBalancerReconcileRow struct {
	original *ovnnb.LoadBalancer
	desired  *ovnnb.LoadBalancer
	deleted  bool
}

// ReconcileLoadBalancers atomically applies a resource's LB additions, updates,
// deletions, VIP migrations, attachments and chassis variables. No writes occur
// while planning. A stale snapshot or concurrent creation fails the whole
// transaction so the caller can reconcile again from a fresh cache.
func (c *OVNNbClient) ReconcileLoadBalancers(reconciliation LoadBalancerReconciliation) error {
	ops, err := c.resourceLoadBalancerOps(reconciliation)
	if err != nil {
		return err
	}
	if err := c.Transact("resource-lb-reconcile", ops); err != nil {
		return fmt.Errorf("reconcile resource load balancers: %w", err)
	}
	return nil
}

func (c *OVNNbClient) resourceLoadBalancerOps(reconciliation LoadBalancerReconciliation) ([]ovsdb.Operation, error) {
	if len(reconciliation.OwnerExternalIDs) == 0 {
		return nil, errors.New("load balancer owner external IDs are required")
	}
	rows, err := c.resourceLoadBalancerRows(reconciliation)
	if err != nil {
		return nil, err
	}
	var ops []ovsdb.Operation
	for _, desired := range reconciliation.LoadBalancers {
		row := rows[desired.Config.Name]
		healthOps, err := c.resourceLoadBalancerHealthCheckOps(row, desired.HealthChecks)
		if err != nil {
			return nil, err
		}
		ops = append(ops, healthOps...)
	}
	for _, name := range slices.Sorted(maps.Keys(rows)) {
		rowOps, err := c.resourceLoadBalancerRowOps(rows[name])
		if err != nil {
			return nil, err
		}
		ops = append(ops, rowOps...)
	}
	attachmentOps, err := c.resourceLoadBalancerAttachmentOps(rows, reconciliation.LoadBalancers)
	if err != nil {
		return nil, err
	}
	ops = append(ops, attachmentOps...)
	variableOps, err := c.resourceLoadBalancerVariableOps(reconciliation)
	if err != nil {
		return nil, err
	}
	ops = append(ops, variableOps...)
	if !slices.ContainsFunc(ops, func(op ovsdb.Operation) bool { return op.Op != ovsdb.OperationWait }) {
		return nil, nil
	}
	return ops, nil
}

func (c *OVNNbClient) resourceLoadBalancerRows(reconciliation LoadBalancerReconciliation) (map[string]*loadBalancerReconcileRow, error) {
	owned, err := c.ListLoadBalancers(func(lb *ovnnb.LoadBalancer) bool {
		for key, value := range reconciliation.OwnerExternalIDs {
			if lb.ExternalIDs[key] != value {
				return false
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	rows := make(map[string]*loadBalancerReconcileRow)
	for _, lb := range owned {
		if rows[lb.Name] != nil {
			return nil, fmt.Errorf("more than one owned load balancer named %q", lb.Name)
		}
		rows[lb.Name] = &loadBalancerReconcileRow{original: &lb, deleted: true}
	}
	seen := make(map[string]struct{}, len(reconciliation.LoadBalancers))
	for _, desired := range reconciliation.LoadBalancers {
		name := desired.Config.Name
		if name == "" {
			return nil, errors.New("load balancer name is required")
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("duplicate desired load balancer %q", name)
		}
		for key, value := range reconciliation.OwnerExternalIDs {
			if desired.Config.ExternalIDs[key] != value {
				return nil, fmt.Errorf("desired load balancer %s does not belong to the resource", name)
			}
		}
		seen[name] = struct{}{}
		row, err := c.resourceLoadBalancerRow(rows, name)
		if err != nil {
			return nil, err
		}
		row.deleted = false
		applyLoadBalancerDesiredState(row.desired, desired)
	}
	for _, deletion := range reconciliation.VIPDeletions {
		if deletion.LoadBalancer == "" {
			continue
		}
		if _, ok := seen[deletion.LoadBalancer]; ok {
			// The complete desired VIP map supersedes old representations.
			continue
		}
		row, err := c.resourceLoadBalancerRow(rows, deletion.LoadBalancer)
		if err != nil {
			return nil, err
		}
		if row.original == nil {
			delete(rows, deletion.LoadBalancer)
			continue
		}
		if row.deleted {
			continue
		}
		delete(row.desired.Vips, deletion.VIP)
		delete(row.desired.ExternalIDs, localExternalVIPKeyPrefix+deletion.VIP)
	}
	if err := c.resourceLoadBalancerEmptyRows(rows, seen, reconciliation.DeleteEmptyLoadBalancers); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if !row.deleted && row.original != nil {
			c.pruneResourceLoadBalancerMappings(row)
			if err := c.pruneResourceLoadBalancerHealthChecks(row); err != nil {
				return nil, err
			}
		}
	}
	return rows, nil
}

func (c *OVNNbClient) resourceLoadBalancerEmptyRows(rows map[string]*loadBalancerReconcileRow, seen map[string]struct{}, names []string) error {
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		row, err := c.resourceLoadBalancerRow(rows, name)
		if err != nil {
			return err
		}
		if row.original == nil {
			delete(rows, name)
		} else if len(row.desired.Vips) == 0 {
			row.deleted = true
		}
	}
	return nil
}

func (c *OVNNbClient) resourceLoadBalancerRow(rows map[string]*loadBalancerReconcileRow, name string) (*loadBalancerReconcileRow, error) {
	row := rows[name]
	if row == nil {
		lb, err := c.GetLoadBalancer(name, true)
		if err != nil {
			return nil, err
		}
		row = &loadBalancerReconcileRow{original: lb}
		rows[name] = row
	}
	if row.desired == nil {
		if row.original == nil {
			row.desired = &ovnnb.LoadBalancer{UUID: ovsclient.NamedUUID(), Name: name}
		} else {
			row.desired = cloneLoadBalancer(row.original)
		}
	}
	return row, nil
}

func cloneLoadBalancer(lb *ovnnb.LoadBalancer) *ovnnb.LoadBalancer {
	return &ovnnb.LoadBalancer{
		UUID: lb.UUID, Name: lb.Name, Protocol: lb.Protocol,
		SelectionFields: slices.Clone(lb.SelectionFields), HealthCheck: slices.Clone(lb.HealthCheck),
		ExternalIDs: maps.Clone(lb.ExternalIDs), Options: maps.Clone(lb.Options),
		Vips: maps.Clone(lb.Vips), IPPortMappings: maps.Clone(lb.IPPortMappings),
	}
}

func applyLoadBalancerDesiredState(lb *ovnnb.LoadBalancer, desired LoadBalancerDesiredState) {
	lb.Protocol = new(desired.Config.Protocol)
	lb.SelectionFields = slices.Clone(desired.Config.SelectionFields)
	if lb.ExternalIDs == nil {
		lb.ExternalIDs = make(map[string]string)
	}
	maps.Copy(lb.ExternalIDs, desired.Config.ExternalIDs)
	maps.DeleteFunc(lb.ExternalIDs, func(key, _ string) bool { return strings.HasPrefix(key, localExternalVIPKeyPrefix) })
	if lb.Options == nil {
		lb.Options = make(map[string]string)
	}
	maps.Copy(lb.Options, desired.Config.Options)
	for _, option := range desired.Config.DeleteOptions {
		delete(lb.Options, option)
	}
	lb.Vips = maps.Clone(desired.VIPs)
	lb.IPPortMappings = maps.Clone(desired.IPPortMappings)
}

func (c *OVNNbClient) pruneResourceLoadBalancerMappings(row *loadBalancerReconcileRow) {
	removedIPs := make(map[string]bool)
	for vip := range row.original.Vips {
		if _, ok := row.desired.Vips[vip]; ok {
			continue
		}
		for backend := range strings.SplitSeq(row.original.Vips[vip], ",") {
			if ip, _, err := net.SplitHostPort(backend); err == nil {
				removedIPs[ip] = true
			}
		}
	}
	maps.DeleteFunc(row.desired.IPPortMappings, func(key, _ string) bool {
		ip := strings.Trim(key, "[]")
		return removedIPs[ip] && !c.isBackendIPStillUsed(row.desired, "", ip)
	})
}

func (c *OVNNbClient) resourceLoadBalancerHealthCheckOps(row *loadBalancerReconcileRow, desired map[string]map[string]string) ([]ovsdb.Operation, error) {
	existing := make(map[string]string)
	if row.original != nil {
		for _, uuid := range row.original.HealthCheck {
			hc := &ovnnb.LoadBalancerHealthCheck{UUID: uuid}
			if err := c.getResourceLoadBalancerHealthCheck(hc); err != nil {
				return nil, err
			}
			existing[hc.Vip] = uuid
		}
	}
	row.desired.HealthCheck = nil
	var ops []ovsdb.Operation
	for _, vip := range slices.Sorted(maps.Keys(desired)) {
		if uuid := existing[vip]; uuid != "" {
			row.desired.HealthCheck = append(row.desired.HealthCheck, uuid)
			continue
		}
		hc := &ovnnb.LoadBalancerHealthCheck{
			UUID: ovsclient.NamedUUID(), Vip: vip, ExternalIDs: maps.Clone(desired[vip]),
			Options: map[string]string{"timeout": "20", "interval": "5", "success_count": "3", "failure_count": "3"},
		}
		createOps, err := c.Create(hc)
		if err != nil {
			return nil, err
		}
		ops = append(ops, createOps...)
		row.desired.HealthCheck = append(row.desired.HealthCheck, hc.UUID)
	}
	return ops, nil
}

func (c *OVNNbClient) pruneResourceLoadBalancerHealthChecks(row *loadBalancerReconcileRow) error {
	var uuids []string
	for _, uuid := range row.desired.HealthCheck {
		hc := &ovnnb.LoadBalancerHealthCheck{UUID: uuid}
		if err := c.getResourceLoadBalancerHealthCheck(hc); err != nil {
			return err
		}
		if _, ok := row.desired.Vips[hc.Vip]; ok {
			uuids = append(uuids, uuid)
		}
	}
	row.desired.HealthCheck = uuids
	return nil
}

func (c *OVNNbClient) resourceLoadBalancerRowOps(row *loadBalancerReconcileRow) ([]ovsdb.Operation, error) {
	if row.original == nil {
		createOps, err := c.Create(row.desired)
		if err != nil {
			return nil, err
		}
		return append([]ovsdb.Operation{ConstructWaitForNameNotExistsOperation(row.desired.Name, ovnnb.LoadBalancerTable)}, createOps...), nil
	}
	old := row.original
	fields := []any{&old.Protocol, &old.SelectionFields, &old.Options, &old.ExternalIDs, &old.Vips, &old.IPPortMappings, &old.HealthCheck}
	guards, err := c.Where(old).Wait(ovsdb.WaitConditionEqual, new(0), old, fields...)
	if err != nil {
		return nil, err
	}
	if !row.deleted && loadBalancerStateEqual(old, row.desired) {
		return guards, nil
	}
	if row.deleted {
		ops, err := c.Where(old).Delete()
		return append(guards, ops...), err
	}
	lb := row.desired
	ops, err := c.Where(old).Update(lb, &lb.Protocol, &lb.SelectionFields, &lb.Options, &lb.ExternalIDs, &lb.Vips, &lb.IPPortMappings, &lb.HealthCheck)
	return append(guards, ops...), err
}

func loadBalancerStateEqual(a, b *ovnnb.LoadBalancer) bool {
	return reflect.DeepEqual(a.Protocol, b.Protocol) &&
		slices.Equal(a.SelectionFields, b.SelectionFields) &&
		slices.Equal(slices.Sorted(slices.Values(a.HealthCheck)), slices.Sorted(slices.Values(b.HealthCheck))) &&
		maps.Equal(a.Options, b.Options) && maps.Equal(a.ExternalIDs, b.ExternalIDs) &&
		maps.Equal(a.Vips, b.Vips) && maps.Equal(a.IPPortMappings, b.IPPortMappings)
}

func (c *OVNNbClient) resourceLoadBalancerVariableOps(reconciliation LoadBalancerReconciliation) ([]ovsdb.Operation, error) {
	if len(reconciliation.TemplateVariablePrefixes) == 0 && len(reconciliation.TemplateVariables) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	var existing []ovnnb.ChassisTemplateVar
	if err := c.WhereCache(func(*ovnnb.ChassisTemplateVar) bool { return true }).List(ctx, &existing); err != nil {
		return nil, err
	}
	current := make(map[string]*ovnnb.ChassisTemplateVar, len(existing))
	desired := make(map[string]map[string]string, len(existing))
	for _, row := range existing {
		if current[row.Chassis] != nil {
			return nil, fmt.Errorf("more than one template variable row for chassis %s", row.Chassis)
		}
		current[row.Chassis] = &row
		desired[row.Chassis] = maps.Clone(row.Variables)
		maps.DeleteFunc(desired[row.Chassis], func(key, _ string) bool {
			return slices.ContainsFunc(reconciliation.TemplateVariablePrefixes, func(prefix string) bool { return strings.HasPrefix(key, prefix) })
		})
	}
	for _, variables := range reconciliation.TemplateVariables {
		if desired[variables.Chassis] == nil {
			desired[variables.Chassis] = make(map[string]string)
		}
		maps.DeleteFunc(desired[variables.Chassis], func(key, _ string) bool { return strings.HasPrefix(key, variables.Prefix) })
		maps.Copy(desired[variables.Chassis], variables.Variables)
	}
	var ops []ovsdb.Operation
	for _, chassis := range slices.Sorted(maps.Keys(desired)) {
		row := current[chassis]
		if row == nil {
			if len(desired[chassis]) == 0 {
				continue
			}
			newRow := &ovnnb.ChassisTemplateVar{UUID: ovsclient.NamedUUID(), Chassis: chassis, Variables: desired[chassis]}
			createOps, err := c.Create(newRow)
			if err != nil {
				return nil, err
			}
			ops = append(ops, ConstructWaitForUniqueOperation(ovnnb.ChassisTemplateVarTable, "chassis", chassis))
			ops = append(ops, createOps...)
			continue
		}
		if maps.Equal(row.Variables, desired[chassis]) {
			continue
		}
		guards, err := c.Where(row).Wait(ovsdb.WaitConditionEqual, new(0), row, &row.Variables)
		if err != nil {
			return nil, err
		}
		updated := *row
		updated.Variables = desired[chassis]
		updateOps, err := c.Where(row).Update(&updated, &updated.Variables)
		if err != nil {
			return nil, err
		}
		ops = append(ops, guards...)
		ops = append(ops, updateOps...)
	}
	return ops, nil
}

func (c *OVNNbClient) resourceLoadBalancerAttachmentOps(rows map[string]*loadBalancerReconcileRow, desired []LoadBalancerDesiredState) ([]ovsdb.Operation, error) {
	managed := make(map[string]struct{})
	for _, row := range rows {
		if row.original != nil && (row.deleted || slices.ContainsFunc(desired, func(lb LoadBalancerDesiredState) bool { return lb.Config.Name == row.original.Name })) {
			managed[row.original.UUID] = struct{}{}
		}
	}
	switches, err := c.ListLogicalSwitch(false, nil)
	if err != nil {
		return nil, err
	}
	routers, err := c.ListLogicalRouter(false, nil)
	if err != nil {
		return nil, err
	}
	var ops []ovsdb.Operation
	for _, ls := range switches {
		uuids := resourceLoadBalancerAttachments(ls.Name, ls.LoadBalancer, managed, rows, desired, false)
		attachmentOps, err := c.resourceLoadBalancerParentOps(&ls, &ls.LoadBalancer, uuids)
		if err != nil {
			return nil, err
		}
		ops = append(ops, attachmentOps...)
	}
	for _, lr := range routers {
		uuids := resourceLoadBalancerAttachments(lr.Name, lr.LoadBalancer, managed, rows, desired, true)
		attachmentOps, err := c.resourceLoadBalancerParentOps(&lr, &lr.LoadBalancer, uuids)
		if err != nil {
			return nil, err
		}
		ops = append(ops, attachmentOps...)
	}
	return ops, nil
}

func resourceLoadBalancerAttachments(name string, current []string, managed map[string]struct{}, rows map[string]*loadBalancerReconcileRow, desired []LoadBalancerDesiredState, router bool) []string {
	uuids := slices.DeleteFunc(slices.Clone(current), func(uuid string) bool { _, ok := managed[uuid]; return ok })
	for _, lb := range desired {
		parents := lb.LogicalSwitches
		if router {
			parents = lb.LogicalRouters
		}
		if slices.Contains(parents, name) {
			uuids = append(uuids, rows[lb.Config.Name].desired.UUID)
		}
	}
	return uuids
}

func (c *OVNNbClient) resourceLoadBalancerParentOps(parent model.Model, field *[]string, desired []string) ([]ovsdb.Operation, error) {
	deletes := slices.DeleteFunc(slices.Clone(*field), func(uuid string) bool { return slices.Contains(desired, uuid) })
	inserts := slices.DeleteFunc(slices.Clone(desired), func(uuid string) bool { return slices.Contains(*field, uuid) })
	var mutations []model.Mutation
	if len(deletes) != 0 {
		mutations = append(mutations, model.Mutation{Field: field, Mutator: ovsdb.MutateOperationDelete, Value: deletes})
	}
	if len(inserts) != 0 {
		mutations = append(mutations, model.Mutation{Field: field, Mutator: ovsdb.MutateOperationInsert, Value: inserts})
	}
	if len(mutations) == 0 {
		return nil, nil
	}
	return c.Where(parent).Mutate(parent, mutations...)
}

func (c *OVNNbClient) getResourceLoadBalancerHealthCheck(hc *ovnnb.LoadBalancerHealthCheck) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	return c.Get(ctx, hc)
}
