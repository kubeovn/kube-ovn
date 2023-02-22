package ovs

import (
	"context"
	"fmt"

	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

// AddLogicalRouterPolicy add a policy route to logical router
func (c OvnClient) AddLogicalRouterPolicy(lrName string, priority int, match, action string, nextHops []string, externalIDs map[string]string) error {
	policy, err := c.newLogicalRouterPolicy(lrName, priority, match, action, nextHops, externalIDs)
	if err != nil {
		return fmt.Errorf("new policy for logical router %s: %v", lrName, err)
	}

	if err := c.CreateLogicalRouterPolicys(lrName, policy); err != nil {
		return fmt.Errorf("add policy to logical router %s: %v", lrName, err)
	}

	return nil
}

// CreateLogicalRouterPolicys create several logical router policy once
func (c OvnClient) CreateLogicalRouterPolicys(lrName string, policies ...*ovnnb.LogicalRouterPolicy) error {
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

	createPoliciesOp, err := c.ovnNbClient.Create(models...)
	if err != nil {
		return fmt.Errorf("generate operations for creating policies: %v", err)
	}

	policyAddOp, err := c.LogicalRouterUpdatePolicyOp(lrName, policyUUIDs, ovsdb.MutateOperationInsert)
	if err != nil {
		return fmt.Errorf("generate operations for adding policies to logical router %s: %v", lrName, err)
	}

	ops := make([]ovsdb.Operation, 0, len(createPoliciesOp)+len(policyAddOp))
	ops = append(ops, createPoliciesOp...)
	ops = append(ops, policyAddOp...)

	if err = c.Transact("lr-policies-add", ops); err != nil {
		return fmt.Errorf("add policies to %s: %v", lrName, err)
	}

	return nil
}

func (c OvnClient) CreateBareLogicalRouterPolicy(lrName string, priority int, match, action string, nextHops []string) error {
	policy, err := c.newLogicalRouterPolicy(lrName, priority, match, action, nextHops, nil)
	if err != nil {
		return fmt.Errorf("new logical router policy: %v", err)
	}

	op, err := c.ovnNbClient.Create(policy)
	if err != nil {
		return fmt.Errorf("generate operations for creating logical router policy: %v", err)
	}

	if err = c.Transact("lr-policy-create", op); err != nil {
		return fmt.Errorf("create logical router policy: %v", err)
	}

	return nil
}

// DeleteLogicalRouterPolicy delete policy from logical router
func (c OvnClient) DeleteLogicalRouterPolicy(lrName string, priority int, match string) error {
	policy, err := c.GetLogicalRouterPolicy(lrName, priority, match, true)
	if err != nil {
		return err
	}

	// not found, skip
	if policy == nil {
		return nil
	}

	// remove policy from logical router
	policyRemoveOp, err := c.LogicalRouterUpdatePolicyOp(lrName, []string{policy.UUID}, ovsdb.MutateOperationDelete)
	if err != nil {
		return fmt.Errorf("generate operations for removing policy 'priority %d match %s' from logical router %s: %v", priority, match, lrName, err)
	}

	// delete logical router policy
	policyDelOp, err := c.Where(policy).Delete()
	if err != nil {
		return fmt.Errorf("generate operations for deleting logical router policy 'priority %d match %s': %v", priority, match, err)
	}

	ops := make([]ovsdb.Operation, 0, len(policyRemoveOp)+len(policyDelOp))
	ops = append(ops, policyRemoveOp...)
	ops = append(ops, policyDelOp...)

	if err = c.Transact("lr-policy-del", ops); err != nil {
		return fmt.Errorf("delete logical router policy 'priority %d match %s': %v", priority, match, err)
	}

	return nil
}

// ClearLogicalRouterPolicy clear policy from logical router once
func (c OvnClient) ClearLogicalRouterPolicy(lrName string) error {
	lr, err := c.GetLogicalRouter(lrName, false)
	if err != nil {
		return fmt.Errorf("get logical router %s: %v", lrName, err)
	}

	// clear logical router policy
	lr.Policies = nil
	policyClearOp, err := c.UpdateLogicalRouterOp(lr, &lr.Policies)
	if err != nil {
		return fmt.Errorf("generate operations for clear logical router %s policy: %v", lrName, err)
	}

	// delete logical router policy
	policyDelOp, err := c.WhereCache(func(policy *ovnnb.LogicalRouterPolicy) bool {
		return len(policy.ExternalIDs) != 0 && policy.ExternalIDs[logicalRouterKey] == lrName
	}).Delete()
	if err != nil {
		return fmt.Errorf("generate operations for deleting logical router %s policy: %v", lrName, err)
	}

	ops := make([]ovsdb.Operation, 0, len(policyClearOp)+len(policyDelOp))
	ops = append(ops, policyClearOp...)
	ops = append(ops, policyDelOp...)

	if err = c.Transact("lr-policies-del", ops); err != nil {
		return fmt.Errorf("delete logical router %s policy: %v", lrName, err)
	}

	return nil
}

// GetLogicalRouterPolicy get logical router policy by priority and match,
// be consistent with ovn-nbctl which priority and match determine one policy in logical router
func (c OvnClient) GetLogicalRouterPolicy(lrName string, priority int, match string, ignoreNotFound bool) (*ovnnb.LogicalRouterPolicy, error) {
	// this is necessary because may exist same priority and match policy in different logical router
	if len(lrName) == 0 {
		return nil, fmt.Errorf("the logical router name is required")
	}

	policyList := make([]ovnnb.LogicalRouterPolicy, 0)
	if err := c.ovnNbClient.WhereCache(func(policy *ovnnb.LogicalRouterPolicy) bool {
		return len(policy.ExternalIDs) != 0 && policy.ExternalIDs[logicalRouterKey] == lrName && policy.Priority == priority && policy.Match == match
	}).List(context.TODO(), &policyList); err != nil {
		return nil, fmt.Errorf("get policy priority %d match %s in logical router %s: %v", priority, match, lrName, err)
	}

	// not found
	if len(policyList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("not found policy priority %d match %s in logical router %s", priority, match, lrName)
	}

	if len(policyList) > 1 {
		return nil, fmt.Errorf("more than one policy with same priority %d match %s in logical router %s", priority, match, lrName)
	}

	return &policyList[0], nil
}

func (c OvnClient) LogicalRouterPolicyExists(lrName string, priority int, match string) (bool, error) {
	acl, err := c.GetLogicalRouterPolicy(lrName, priority, match, true)
	return acl != nil, err
}

// newLogicalRouterPolicy return logical router policy with basic information
func (c OvnClient) newLogicalRouterPolicy(lrName string, priority int, match, action string, nextHops []string, externalIDs map[string]string) (*ovnnb.LogicalRouterPolicy, error) {
	if len(lrName) == 0 {
		return nil, fmt.Errorf("the logical router name is required")
	}

	if priority == 0 || len(match) == 0 || len(action) == 0 {
		return nil, fmt.Errorf("logical router policy 'priority %d' or 'match %s' or 'action %s' is required", priority, match, action)
	}

	exists, err := c.LogicalRouterPolicyExists(lrName, priority, match)
	if err != nil {
		return nil, fmt.Errorf("get logical router policy: %v", err)
	}

	// found, ignore
	if exists {
		return nil, nil
	}

	policy := &ovnnb.LogicalRouterPolicy{
		UUID:     ovsclient.UUID(),
		Priority: priority,
		Match:    match,
		Action:   action,
		Nexthops: nextHops,
		ExternalIDs: map[string]string{
			logicalRouterKey: lrName,
		},
	}

	for k, v := range externalIDs {
		policy.ExternalIDs[k] = v
	}

	return policy, nil
}

/*
----------------------------------------------------------------------------------------------
TODO: wait to be deleted
*/

func (c OvnClient) AddRouterPolicy(lr *ovnnb.LogicalRouter, match string, action ovnnb.LogicalRouterPolicyAction,
	opts map[string]string, extIDs map[string]string, priority int) error {
	lrPolicy := &ovnnb.LogicalRouterPolicy{
		Action:      action,
		Match:       match,
		Options:     opts,
		Priority:    priority,
		ExternalIDs: extIDs,
		UUID:        ovsclient.NamedUUID(),
		Nexthop:     nil,
		Nexthops:    nil,
	}

	waitOp := ConstructWaitForUniqueOperation("Logical_Router_Policy", "match", match)
	ops := []ovsdb.Operation{waitOp}

	createOps, err := c.ovnNbClient.Create(lrPolicy)
	if err != nil {
		return err
	}
	ops = append(ops, createOps...)
	mutationOps, err := c.ovnNbClient.Where(lr).Mutate(lr, model.Mutation{
		Field:   &lr.Policies,
		Mutator: ovsdb.MutateOperationInsert,
		Value:   []string{lrPolicy.UUID},
	})
	if err != nil {
		return fmt.Errorf("failed to generate create operations for router policy %s: %v", match, err)
	}
	ops = append(ops, mutationOps...)

	if err = c.Transact("lr-policy-add", ops); err != nil {
		return fmt.Errorf("failed to create route policy %s: %v", match, err)
	}
	return nil
}

func (c OvnClient) DeleteRouterPolicy(lr *ovnnb.LogicalRouter, uuid string) error {
	ops, err := c.ovnNbClient.Where(lr).Mutate(lr, model.Mutation{
		Field:   &lr.Policies,
		Mutator: ovsdb.MutateOperationDelete,
		Value:   []string{uuid},
	})
	if err != nil {
		return fmt.Errorf("failed to generate delete operations for router %s: %v", uuid, err)
	}

	lrPolicy := &ovnnb.LogicalRouterPolicy{
		UUID: uuid,
	}
	deleteOps, err := c.ovnNbClient.Where(lrPolicy).Delete()
	if err != nil {
		return fmt.Errorf("failed to generate delete operations for router policy %s: %v", uuid, err)
	}
	ops = append(ops, deleteOps...)

	if err = c.Transact("lr-policy-delete", ops); err != nil {
		return fmt.Errorf("failed to delete route policy %s: %v", uuid, err)
	}
	return nil
}
