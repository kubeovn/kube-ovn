package ovs

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/scylladb/go-set/strset"
	"k8s.io/klog/v2"
	"k8s.io/utils/set"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

// AddLogicalRouterPolicy add a policy route to logical router
func (c *OVNNbClient) AddLogicalRouterPolicy(lrName string, priority int, match, action string, nextHops, bfdSessions []string, externalIDs map[string]string) error {
	fnFilter := func(policy *ovnnb.LogicalRouterPolicy) bool {
		return policy.Priority == priority && policy.Match == match
	}
	policyList, err := c.listLogicalRouterPoliciesByFilter(lrName, fnFilter)
	if err != nil {
		return logWrap(err, wrapErr("get policy priority %d match %s in logical router %s: %w", priority, match, lrName))
	}

	// Same priority, same match, only retain the first policy
	duplicate := make([]string, 0, len(policyList))
	var policyFound *ovnnb.LogicalRouterPolicy
	// nextHopSet is only consulted for reroute policies; skip the allocation otherwise
	var nextHopSet *strset.Set
	if action == ovnnb.LogicalRouterPolicyActionReroute {
		nextHopSet = strset.New(nextHops...)
	}
	for _, policy := range policyList {
		if policy.Action != action || (policy.Action == ovnnb.LogicalRouterPolicyActionReroute && !nextHopSet.IsEqual(strset.New(policy.Nexthops...))) {
			duplicate = append(duplicate, policy.UUID)
			continue
		}
		if policyFound != nil {
			duplicate = append(duplicate, policy.UUID)
		} else {
			policyFound = policy
		}
	}
	for _, uuid := range duplicate {
		klog.Infof("deleting lr policy by uuid %s", uuid)
		if err = c.DeleteLogicalRouterPolicyByUUID(lrName, uuid); err != nil {
			return logErr(err)
		}
	}

	if policyFound == nil {
		klog.Infof("creating lr policy with priority = %d, match = %q, action = %q, nextHops = %q", priority, match, action, nextHops)
		policy := c.newLogicalRouterPolicy(priority, match, action, nextHops, bfdSessions, externalIDs)
		if err := c.CreateLogicalRouterPolicies(lrName, policy); err != nil {
			return logWrap(err, wrapErr("add policy to logical router %s: %w", lrName))
		}
	} else if !maps.Equal(policyFound.ExternalIDs, externalIDs) {
		policy := new(*policyFound)
		policy.ExternalIDs = externalIDs
		ops, err := c.Database.Table(&ovnnb.LogicalRouterPolicy{}).UpdateOps(policy, policy, &policy.ExternalIDs)
		if err := c.transactGenerated("lr-policy-update", ops, err,
			wrapErr("failed to generate operations for updating logical router policy: %w"),
			wrapErr("failed to update logical router policy: %w"),
		); err != nil {
			return err
		}
	}

	return nil
}

// BatchAddLogicalRouterPolicy  batch add a policy route to logical router
func (c *OVNNbClient) BatchAddLogicalRouterPolicy(lrName string, policies ...*ovnnb.LogicalRouterPolicy) error {
	if len(policies) == 0 {
		return nil
	}

	start := time.Now()
	var (
		needDelete       []string
		needCreatePolicy []*ovnnb.LogicalRouterPolicy
		needUpdatePolicy = make(map[*ovnnb.LogicalRouterPolicy]*ovnnb.LogicalRouterPolicy)
	)
	policyListMap, err := c.batchListLogicalRouterPoliciesByFilter(lrName, policies...)
	if err != nil {
		return fmt.Errorf("batch list logical router %s policies %d: %w", lrName, len(policies), err)
	}

	for policy, policyList := range policyListMap {
		if len(policyList) == 0 {
			needCreatePolicy = append(needCreatePolicy, policy)
			continue
		}
		duplicate, policyFound := c.matchLogicalRouterPolicies(policy, policyList)
		if policyFound == nil {
			needCreatePolicy = append(needCreatePolicy, policy)
		} else if !maps.Equal(policyFound.ExternalIDs, policy.ExternalIDs) {
			needUpdatePolicy[policy] = policyFound
		}
		if len(duplicate) > 0 {
			needDelete = append(needDelete, duplicate...)
		}
	}
	klog.Infof("take to %vms batch add logical router %s list policy del %d create %d update %d", time.Since(start).Milliseconds(), lrName, len(needDelete), len(needCreatePolicy), len(needUpdatePolicy))
	if len(needDelete) > 0 {
		if err := c.BatchDeleteLogicalRouterPolicyByUUID(lrName, needDelete...); err != nil {
			return err
		}
	}
	if len(needCreatePolicy) > 0 {
		if err := c.batchCreateLogicalRouterPolicies(lrName, needCreatePolicy); err != nil {
			return err
		}
	}
	if len(needUpdatePolicy) > 0 {
		if err := c.batchUpdateLogicalRouterPolicies(needUpdatePolicy); err != nil {
			return err
		}
	}
	klog.Infof("take to %vms batch add logical router %s policy %d", time.Since(start).Milliseconds(), lrName, len(policies))
	return nil
}

// CreateLogicalRouterPolicies create several logical router policy once
func (c *OVNNbClient) CreateLogicalRouterPolicies(lrName string, policies ...*ovnnb.LogicalRouterPolicy) error {
	models, uuids := modelsAndUUIDs(policies, func(policy *ovnnb.LogicalRouterPolicy) string { return policy.UUID })
	ops, err := createAndAttachOps(c, &ovnnb.LogicalRouterPolicy{}, models, func(ids []string) ([]ovsdb.Operation, error) {
		return c.LogicalRouterUpdatePolicyOp(lrName, ids, ovsdb.MutateOperationInsert)
	}, uuids)
	return c.transactGenerated("lr-policies-add", ops, err,
		wrapErr("generate operations for adding policies to logical router %s: %w", lrName),
		wrapErr("add policies to %s: %w", lrName),
	)
}

// DeleteLogicalRouterPolicy delete policy from logical router
func (c *OVNNbClient) DeleteLogicalRouterPolicy(lrName string, priority int, match string) error {
	policyList, err := c.GetLogicalRouterPolicy(lrName, priority, match, true)
	if err != nil {
		return logErr(err)
	}

	for _, p := range policyList {
		if err := c.DeleteLogicalRouterPolicyByUUID(lrName, p.UUID); err != nil {
			return logErr(err)
		}
	}

	return nil
}

// DeleteLogicalRouterPolicy delete policy from logical router
func (c *OVNNbClient) BatchDeleteLogicalRouterPolicy(lrName string, logicalRouteRolicies []*ovnnb.LogicalRouterPolicy) error {
	if len(logicalRouteRolicies) == 0 {
		return nil
	}

	policyListMap, err := c.batchListLogicalRouterPoliciesByFilter(lrName, logicalRouteRolicies...)
	if err != nil {
		return logErr(err)
	}

	uuidList := make([]string, 0)
	for _, policyList := range policyListMap {
		if len(policyList) == 0 {
			continue
		}
		for _, p := range policyList {
			uuidList = append(uuidList, p.UUID)
		}
	}

	// not found,skip
	if len(uuidList) == 0 {
		return nil
	}

	if err := c.BatchDeleteLogicalRouterPolicyByUUID(lrName, uuidList...); err != nil {
		return logErr(err)
	}

	return nil
}

// DeleteLogicalRouterPolicy delete some policies from logical router once
func (c *OVNNbClient) DeleteLogicalRouterPolicies(lrName string, priority int, externalIDs map[string]string) error {
	policies, err := c.ListLogicalRouterPolicies(lrName, priority, externalIDs, false)
	if err != nil {
		return logErr(err)
	}
	uuids := rowUUIDs(policies, func(policy *ovnnb.LogicalRouterPolicy) string { return policy.UUID })
	return c.detachRouterUUIDs(lrName, uuids, "lr-policies-del", fmt.Sprintf("policy %v", uuids), c.LogicalRouterUpdatePolicyOp)
}

func (c *OVNNbClient) DeleteLogicalRouterPolicyByUUID(lrName, uuid string) error {
	return c.detachRouterUUIDs(lrName, []string{uuid}, "lr-policy-del", fmt.Sprintf("policy '%s'", uuid), c.LogicalRouterUpdatePolicyOp)
}

func (c *OVNNbClient) BatchDeleteLogicalRouterPolicyByUUID(lrName string, uuidList ...string) error {
	if len(uuidList) == 0 {
		return nil
	}
	start := time.Now()
	uuidList = set.New(uuidList...).UnsortedList()
	if err := c.detachRouterUUIDs(lrName, uuidList, "lr-policy-del", fmt.Sprintf("policies '%v'", uuidList), c.LogicalRouterUpdatePolicyOp); err != nil {
		return err
	}
	klog.V(3).Infof("take to %vms batch delete logical router policies %s uuid %v", time.Since(start).Milliseconds(), lrName, uuidList)
	return nil
}

func (c *OVNNbClient) DeleteLogicalRouterPolicyByNexthop(lrName string, priority int, nexthop string) error {
	policyList, err := c.listLogicalRouterPoliciesByFilter(lrName, func(route *ovnnb.LogicalRouterPolicy) bool {
		if route.Priority != priority {
			return false
		}
		return (route.Nexthop != nil && *route.Nexthop == nexthop) || slices.Contains(route.Nexthops, nexthop)
	})
	if err != nil {
		return logErr(err)
	}
	for _, policy := range policyList {
		if err = c.DeleteLogicalRouterPolicyByUUID(lrName, policy.UUID); err != nil {
			return logErr(err)
		}
	}
	return nil
}

// ClearLogicalRouterPolicy clear policy from logical router once
func (c *OVNNbClient) ClearLogicalRouterPolicy(lrName string) error {
	lr, err := c.GetLogicalRouter(lrName, false)
	if err != nil {
		return logWrap(err, wrapErr("get logical router %s: %w", lrName))
	}

	// clear logical router policy
	lr.Policies = nil
	ops, err := c.UpdateLogicalRouterOp(lr, &lr.Policies)
	return c.transactGenerated("lr-policy-clear", ops, err,
		wrapErr("generate operations for clearing logical router %s policy: %w", lrName),
		wrapErr("clear logical router %s policy: %w", lrName),
	)
}

// GetLogicalRouterPolicy get logical router policy by priority and match,
// be consistent with ovn-nbctl which priority and match determine one policy in logical router
func (c *OVNNbClient) GetLogicalRouterPolicy(lrName string, priority int, match string, ignoreNotFound bool) ([]*ovnnb.LogicalRouterPolicy, error) {
	// this is necessary because may exist same priority and match policy in different logical router
	if len(lrName) == 0 {
		return nil, errors.New("the logical router name is required")
	}

	fnFilter := func(policy *ovnnb.LogicalRouterPolicy) bool {
		return policy.Priority == priority && policy.Match == match
	}
	policyList, err := c.listLogicalRouterPoliciesByFilter(lrName, fnFilter)
	if err != nil {
		return nil, logWrap(err, wrapErr("get policy priority %d match %s in logical router %s: %w", priority, match, lrName))
	}

	// not found
	if len(policyList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("not found policy priority %d match %s in logical router %s", priority, match, lrName)
	}

	return policyList, nil
}

// GetLogicalRouterPolicyByUUID get logical router policy by UUID
func (c *OVNNbClient) GetLogicalRouterPolicyByUUID(uuid string) (*ovnnb.LogicalRouterPolicy, error) {
	return getIndexedLogged(c.Database, &ovnnb.LogicalRouterPolicy{UUID: uuid})
}

// GetLogicalRouterPoliciesByExtID get logical router policy route by external ID
func (c *OVNNbClient) GetLogicalRouterPoliciesByExtID(lrName, key, value string) ([]*ovnnb.LogicalRouterPolicy, error) {
	fnFilter := func(policy *ovnnb.LogicalRouterPolicy) bool {
		if len(policy.ExternalIDs) != 0 {
			if _, ok := policy.ExternalIDs[key]; ok {
				return policy.ExternalIDs[key] == value
			}
		}
		return false
	}
	return c.listLogicalRouterPoliciesByFilter(lrName, fnFilter)
}

// ListLogicalRouterPolicies list route policy which match the given externalIDs
func (c *OVNNbClient) ListLogicalRouterPolicies(lrName string, priority int, externalIDs map[string]string, ignoreExtIDEmptyValue bool) ([]*ovnnb.LogicalRouterPolicy, error) {
	return c.listLogicalRouterPoliciesByFilter(lrName, policyFilter(priority, externalIDs, ignoreExtIDEmptyValue))
}

// newLogicalRouterPolicy return logical router policy with basic information
func (c *OVNNbClient) newLogicalRouterPolicy(priority int, match, action string, nextHops, bfdSessions []string, externalIDs map[string]string) *ovnnb.LogicalRouterPolicy {
	return &ovnnb.LogicalRouterPolicy{
		UUID:        ovsclient.NamedUUID(),
		Priority:    priority,
		Match:       match,
		Action:      action,
		Nexthops:    nextHops,
		BFDSessions: bfdSessions,
		ExternalIDs: externalIDs,
	}
}

// policyFilter filter policies which match the given externalIDs
func policyFilter(priority int, externalIDs map[string]string, ignoreExtIDEmptyValue bool) func(policy *ovnnb.LogicalRouterPolicy) bool {
	return func(policy *ovnnb.LogicalRouterPolicy) bool {
		if !matchExternalIDsMode(policy.ExternalIDs, externalIDs, ignoreExtIDEmptyValue) {
			return false
		}
		return priority < 0 || priority == policy.Priority
	}
}

func (c *OVNNbClient) UpdateLogicalRouterPolicy(policy *ovnnb.LogicalRouterPolicy, fields ...any) error {
	return c.updateModelLogged("lr-policy-update", policy, func(err error) error {
		return fmt.Errorf("failed to update logical router policy %s: %w", policy.UUID, err)
	}, fields...)
}

func (c *OVNNbClient) DeleteRouterPolicy(lr *ovnnb.LogicalRouter, uuid string) error {
	ops, err := c.Database.Table(&ovnnb.LogicalRouter{}).MutateOps(lr, model.Mutation{
		Field:   &lr.Policies,
		Mutator: ovsdb.MutateOperationDelete,
		Value:   []string{uuid},
	})
	return c.transactGenerated("lr-policy-delete", ops, err,
		wrapErr("failed to generate delete operations for router %s: %w", uuid),
		wrapErr("failed to delete router policy %s: %w", uuid),
	)
}

func (c *OVNNbClient) listLogicalRouterPoliciesByFilter(lrName string, filter func(policy *ovnnb.LogicalRouterPolicy) bool) ([]*ovnnb.LogicalRouterPolicy, error) {
	return c.listRouterChildren(lrName, func(lr *ovnnb.LogicalRouter) []string { return lr.Policies }, &ovnnb.LogicalRouterPolicy{}, func(policy *ovnnb.LogicalRouterPolicy) string { return policy.UUID }, filter)
}

func (c *OVNNbClient) batchListLogicalRouterPoliciesByFilter(lrName string, policies ...*ovnnb.LogicalRouterPolicy) (map[*ovnnb.LogicalRouterPolicy][]*ovnnb.LogicalRouterPolicy, error) {
	start := time.Now()
	lr, err := c.GetLogicalRouter(lrName, false)
	if err != nil {
		return nil, logErr(err)
	}
	lrPolicySet := set.New(lr.Policies...)

	policyIndex := make([]model.Model, 0, len(policies))
	for _, p := range policies {
		policyIndex = append(policyIndex, buildLogicalRouterPolicyIndex(p.Priority, p.Match))
	}

	indexStart := time.Now()
	policyList, err := listWhere[ovnnb.LogicalRouterPolicy](c.Database, &ovnnb.LogicalRouterPolicy{}, policyIndex...)
	if err != nil {
		return nil, logErr(err)
	}
	klog.Infof("take to %v batch list logical router policy %s incoming policies len %v query policies len %v by client index", time.Since(indexStart), lrName, len(policies), len(policyList))

	policySet := make(map[string]*ovnnb.LogicalRouterPolicy)
	for _, policy := range policies {
		key := createPolicyKey(policy.Priority, policy.Match)
		policySet[key] = policy
	}

	policyMapByUUID := make(map[string][]*ovnnb.LogicalRouterPolicy)
	for _, policy := range policyList {
		if lrPolicySet.Has(policy.UUID) {
			key := createPolicyKey(policy.Priority, policy.Match)
			policyMapByUUID[key] = append(policyMapByUUID[key], policy)
		}
	}

	policyListMap := make(map[*ovnnb.LogicalRouterPolicy][]*ovnnb.LogicalRouterPolicy)
	for policyKey, policy := range policySet {
		if matchingPolicies, found := policyMapByUUID[policyKey]; found {
			policyListMap[policy] = append(policyListMap[policy], matchingPolicies...)
		} else {
			policyListMap[policy] = []*ovnnb.LogicalRouterPolicy{}
		}
	}

	elapsed := float64(time.Since(start) / time.Millisecond)
	if elapsed > 500 {
		klog.Infof("take to %vms batch list logical router policy %s policies %d query result policies %d nb policies len %d", elapsed, lrName, len(policies), len(policyList), len(policyListMap))
	}
	return policyListMap, nil
}

func (c *OVNNbClient) matchLogicalRouterPolicies(policy *ovnnb.LogicalRouterPolicy, policyList []*ovnnb.LogicalRouterPolicy) ([]string, *ovnnb.LogicalRouterPolicy) {
	var (
		duplicate   []string
		policyFound *ovnnb.LogicalRouterPolicy
	)

	// nextHopSet is only consulted for reroute policies; skip the allocation otherwise
	var nextHopSet *strset.Set
	if policy.Action == ovnnb.LogicalRouterPolicyActionReroute {
		nextHopSet = strset.New(policy.Nexthops...)
	}
	for _, policyOld := range policyList {
		if policyOld.Action != policy.Action || (policyOld.Action == ovnnb.LogicalRouterPolicyActionReroute && !nextHopSet.IsEqual(strset.New(policyOld.Nexthops...))) {
			duplicate = append(duplicate, policyOld.UUID)
			continue
		}
		if policyFound != nil {
			duplicate = append(duplicate, policyOld.UUID)
		} else {
			policyFound = policyOld
		}
	}

	return duplicate, policyFound
}

func (c *OVNNbClient) batchCreateLogicalRouterPolicies(lrName string, policies []*ovnnb.LogicalRouterPolicy) error {
	routerPolicies := make([]*ovnnb.LogicalRouterPolicy, 0, len(policies))
	for _, policy := range policies {
		routerPolicies = append(routerPolicies, c.newLogicalRouterPolicy(policy.Priority, policy.Match, policy.Action, policy.Nexthops, policy.BFDSessions, policy.ExternalIDs))
	}
	if err := c.CreateLogicalRouterPolicies(lrName, routerPolicies...); err != nil {
		return fmt.Errorf("failed to batch create policies for router %s: %w", lrName, err)
	}
	return nil
}

func (c *OVNNbClient) batchUpdateLogicalRouterPolicies(updateMap map[*ovnnb.LogicalRouterPolicy]*ovnnb.LogicalRouterPolicy) error {
	updateOps := make([]ovsdb.Operation, 0, len(updateMap))
	for policyNew, policyFound := range updateMap {
		policy := new(*policyFound)
		policy.ExternalIDs = policyNew.ExternalIDs
		ops, err := c.Database.Table(&ovnnb.LogicalRouterPolicy{}).UpdateOps(policy, policy, &policy.ExternalIDs)
		if err != nil {
			return fmt.Errorf("failed to generate operations for updating logical router policy: %w", err)
		}
		updateOps = append(updateOps, ops...)
	}
	return c.transactGenerated("lr-policy-update", updateOps, nil, nil,
		wrapErr("failed to batch update logical router policy: %w"),
	)
}

func createPolicyKey(priority int, match string) string {
	return fmt.Sprintf("%s-%d", match, priority)
}

func buildLogicalRouterPolicyIndex(priority int, match string) *ovnnb.LogicalRouterPolicy {
	policy := &ovnnb.LogicalRouterPolicy{}
	if match != "" {
		policy.Match = match
	}
	if priority >= 0 {
		policy.Priority = priority
	}
	return policy
}
