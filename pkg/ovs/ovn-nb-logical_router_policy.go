package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"github.com/scylladb/go-set/strset"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
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
		klog.Error(err)
		return fmt.Errorf("get policy priority %d match %s in logical router %s: %w", priority, match, lrName, err)
	}

	// Same priority, same match, only retain the first policy
	duplicate := make([]string, 0, len(policyList))
	var policyFound *ovnnb.LogicalRouterPolicy
	for _, policy := range policyList {
		if policy.Action != action || (policy.Action == ovnnb.LogicalRouterPolicyActionReroute && !strset.New(nextHops...).IsEqual(strset.New(policy.Nexthops...))) {
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
			klog.Error(err)
			return err
		}
	}

	if policyFound == nil {
		klog.Infof("creating lr policy with priority = %d, match = %q, action = %q, nextHops = %q", priority, match, action, nextHops)
		policy := c.newLogicalRouterPolicy(priority, match, action, nextHops, bfdSessions, externalIDs)
		if err := c.CreateLogicalRouterPolicies(lrName, policy); err != nil {
			klog.Error(err)
			return fmt.Errorf("add policy to logical router %s: %w", lrName, err)
		}
	} else if !maps.Equal(policyFound.ExternalIDs, externalIDs) {
		policy := ptr.To(*policyFound)
		policy.ExternalIDs = externalIDs
		ops, err := c.Where(policy).Update(policy, &policy.ExternalIDs)
		if err != nil {
			err := fmt.Errorf("failed to generate operations for updating logical router policy: %w", err)
			klog.Error(err)
			return err
		}

		if err = c.Transact("lr-policy-update", ops); err != nil {
			err := fmt.Errorf("failed to update logical router policy: %w", err)
			klog.Error(err)
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
		if err := c.batchUpdatetLogicalRouterPolicies(needUpdatePolicy); err != nil {
			return err
		}
	}
	klog.Infof("take to %vms batch add logical router %s policy %d", time.Since(start).Milliseconds(), lrName, len(policies))
	return nil
}

// CreateLogicalRouterPolicies create several logical router policy once
func (c *OVNNbClient) CreateLogicalRouterPolicies(lrName string, policies ...*ovnnb.LogicalRouterPolicy) error {
	if len(policies) == 0 {
		return nil
	}

	models := make([]model.Model, 0, len(policies))
	policyUUIDs := make([]string, 0, len(policies))
	for _, policy := range policies {
		if policy != nil {
			models = append(models, model.Model(policy))
			policyUUIDs = append(policyUUIDs, policy.UUID)
		}
	}

	createPoliciesOp, err := c.ovsDbClient.Create(models...)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for creating policies: %w", err)
	}

	policyAddOp, err := c.LogicalRouterUpdatePolicyOp(lrName, policyUUIDs, ovsdb.MutateOperationInsert)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for adding policies to logical router %s: %w", lrName, err)
	}

	ops := make([]ovsdb.Operation, 0, len(createPoliciesOp)+len(policyAddOp))
	ops = append(ops, createPoliciesOp...)
	ops = append(ops, policyAddOp...)

	if err = c.Transact("lr-policies-add", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("add policies to %s: %w", lrName, err)
	}

	return nil
}

// DeleteLogicalRouterPolicy delete policy from logical router
func (c *OVNNbClient) DeleteLogicalRouterPolicy(lrName string, priority int, match string) error {
	policyList, err := c.GetLogicalRouterPolicy(lrName, priority, match, true)
	if err != nil {
		klog.Error(err)
		return err
	}

	for _, p := range policyList {
		if err := c.DeleteLogicalRouterPolicyByUUID(lrName, p.UUID); err != nil {
			klog.Error(err)
			return err
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
		klog.Error(err)
		return err
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
		klog.Error(err)
		return err
	}

	return nil
}

// DeleteLogicalRouterPolicy delete some policies from logical router once
func (c *OVNNbClient) DeleteLogicalRouterPolicies(lrName string, priority int, externalIDs map[string]string) error {
	// remove policies from logical router
	policies, err := c.ListLogicalRouterPolicies(lrName, priority, externalIDs, false)
	if err != nil {
		klog.Error(err)
		return err
	}
	if len(policies) == 0 {
		return nil
	}

	policiesUUIDs := make([]string, 0, len(policies))
	for _, policy := range policies {
		policiesUUIDs = append(policiesUUIDs, policy.UUID)
	}

	ops, err := c.LogicalRouterUpdatePolicyOp(lrName, policiesUUIDs, ovsdb.MutateOperationDelete)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for removing policy %v from logical router %s: %w", policiesUUIDs, lrName, err)
	}
	if err = c.Transact("lr-policies-del", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("delete logical router policy %v from logical router %s: %w", policiesUUIDs, lrName, err)
	}
	return nil
}

func (c *OVNNbClient) DeleteLogicalRouterPolicyByUUID(lrName, uuid string) error {
	// remove policy from logical router
	ops, err := c.LogicalRouterUpdatePolicyOp(lrName, []string{uuid}, ovsdb.MutateOperationDelete)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for removing policy '%s' from logical router %s: %w", uuid, lrName, err)
	}
	if err = c.Transact("lr-policy-del", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("delete logical router policy '%s' from logical router %s: %w", uuid, lrName, err)
	}
	return nil
}

// BatchDeleteLogicalRouterPolicyByUUID batch remove policy  from logical router
func (c *OVNNbClient) BatchDeleteLogicalRouterPolicyByUUID(lrName string, uuidList ...string) error {
	if len(uuidList) == 0 {
		return nil
	}
	start := time.Now()
	ops, err := c.LogicalRouterUpdatePolicyOp(lrName, uuidList, ovsdb.MutateOperationDelete)
	if err != nil {
		err := fmt.Errorf("generate operations for removing policies '%v' from logical router %s: %w", uuidList, lrName, err)
		klog.Error(err)
		return err
	}

	if err = c.Transact("lr-policy-del", ops); err != nil {
		err := fmt.Errorf("delete logical router policies '%v' from logical router %s: %w", uuidList, lrName, err)
		klog.Error(err)
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
		klog.Error(err)
		return err
	}
	for _, policy := range policyList {
		if err = c.DeleteLogicalRouterPolicyByUUID(lrName, policy.UUID); err != nil {
			klog.Error(err)
			return err
		}
	}
	return nil
}

// ClearLogicalRouterPolicy clear policy from logical router once
func (c *OVNNbClient) ClearLogicalRouterPolicy(lrName string) error {
	lr, err := c.GetLogicalRouter(lrName, false)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("get logical router %s: %w", lrName, err)
	}

	// clear logical router policy
	lr.Policies = nil
	ops, err := c.UpdateLogicalRouterOp(lr, &lr.Policies)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for clear logical router %s policy: %w", lrName, err)
	}
	if err = c.Transact("lr-policy-clear", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("clear logical router %s policy: %w", lrName, err)
	}

	return nil
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
		klog.Error(err)
		return nil, fmt.Errorf("get policy priority %d match %s in logical router %s: %w", priority, match, lrName, err)
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
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	policy := &ovnnb.LogicalRouterPolicy{UUID: uuid}
	if err := c.Get(ctx, policy); err != nil {
		klog.Error(err)
		return nil, err
	}

	return policy, nil
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
		if len(policy.ExternalIDs) < len(externalIDs) {
			return false
		}

		if len(policy.ExternalIDs) != 0 {
			for k, v := range externalIDs {
				// ignoreExtIDEmptyValue is used to the case below:
				// if only key exist but not value in externalIDs, we should include this lsp,
				// it's equal to shell command `ovn-nbctl --columns=xx find logical_router_policy external_ids:key!=\"\"`
				if len(v) == 0 && ignoreExtIDEmptyValue {
					if len(policy.ExternalIDs[k]) == 0 {
						return false
					}
				} else {
					if policy.ExternalIDs[k] != v {
						return false
					}
				}
			}
		}

		if priority >= 0 && priority != policy.Priority {
			return false
		}

		return true
	}
}

func (c *OVNNbClient) UpdateLogicalRouterPolicy(policy *ovnnb.LogicalRouterPolicy, fields ...interface{}) error {
	ops, err := c.ovsDbClient.Where(policy).Update(policy, fields...)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to generate update operations for logical router policy %s: %w", policy.UUID, err)
	}
	if err = c.Transact("lr-policy-update", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to update logical router policy %s: %w", policy.UUID, err)
	}
	return nil
}

func (c *OVNNbClient) DeleteRouterPolicy(lr *ovnnb.LogicalRouter, uuid string) error {
	ops, err := c.ovsDbClient.Where(lr).Mutate(lr, model.Mutation{
		Field:   &lr.Policies,
		Mutator: ovsdb.MutateOperationDelete,
		Value:   []string{uuid},
	})
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to generate delete operations for router %s: %w", uuid, err)
	}
	if err = c.Transact("lr-policy-delete", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to delete router policy %s: %w", uuid, err)
	}
	return nil
}

func (c *OVNNbClient) listLogicalRouterPoliciesByFilter(lrName string, filter func(policy *ovnnb.LogicalRouterPolicy) bool) ([]*ovnnb.LogicalRouterPolicy, error) {
	lr, err := c.GetLogicalRouter(lrName, false)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	uuidSet := set.New(lr.Policies...)
	predicate := func(policy *ovnnb.LogicalRouterPolicy) bool {
		if !uuidSet.Has(policy.UUID) {
			return false
		}
		return filter == nil || filter(policy)
	}

	policyList := make([]*ovnnb.LogicalRouterPolicy, 0, len(lr.Policies))
	if err = c.WhereCache(predicate).List(context.Background(), &policyList); err != nil {
		klog.Error(err)
		return nil, err
	}

	return policyList, nil
}

func (c *OVNNbClient) batchListLogicalRouterPoliciesByFilter(lrName string, policies ...*ovnnb.LogicalRouterPolicy) (map[*ovnnb.LogicalRouterPolicy][]*ovnnb.LogicalRouterPolicy, error) {
	start := time.Now()
	lr, err := c.GetLogicalRouter(lrName, false)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	lrPolicySet := set.New(lr.Policies...)

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	policyIndex := make([]model.Model, 0)
	for _, p := range policies {
		policyIndex = append(policyIndex, buildLogicalRouterPolicyIndex(p.Priority, p.Match))
	}

	var policyList []*ovnnb.LogicalRouterPolicy
	indexStart := time.Now()
	if err := c.ovsDbClient.Where(policyIndex...).List(ctx, &policyList); err != nil {
		klog.Error(err)
		return nil, err
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

	elapsed := float64((time.Since(start)) / time.Millisecond)
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

	for _, policyOld := range policyList {
		if policyOld.Action != policy.Action || (policyOld.Action == ovnnb.LogicalRouterPolicyActionReroute && !strset.New(policy.Nexthops...).IsEqual(strset.New(policyOld.Nexthops...))) {
			duplicate = append(duplicate, policyOld.UUID)
			continue
		}
		if policyFound != nil {
			duplicate = append(duplicate, policyFound.UUID)
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

func (c *OVNNbClient) batchUpdatetLogicalRouterPolicies(updateMap map[*ovnnb.LogicalRouterPolicy]*ovnnb.LogicalRouterPolicy) error {
	updateOps := make([]ovsdb.Operation, 0, len(updateMap))
	for policyNew, policyFound := range updateMap {
		policy := ptr.To(*policyFound)
		policy.ExternalIDs = policyNew.ExternalIDs
		ops, err := c.Where(policy).Update(policy, &policy.ExternalIDs)
		if err != nil {
			return fmt.Errorf("failed to generate operations for updating logical router policy: %w", err)
		}
		updateOps = append(updateOps, ops...)
	}
	if err := c.Transact("lr-policy-update", updateOps); err != nil {
		err := fmt.Errorf("failed to batch update logical router policy: %w", err)
		klog.Error(err)
		return err
	}
	return nil
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
