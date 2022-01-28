package ovs

import (
	"context"
	"fmt"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/ovn-org/libovsdb/ovsdb"
)

func (c Client) ListLogicalRouterPolicy(router string, priority *int, match, nexthop string) ([]ovnnb.LogicalRouterPolicy, error) {
	var policyList []ovnnb.LogicalRouterPolicy
	if err := c.nbClient.WhereCache(func(policy *ovnnb.LogicalRouterPolicy) bool {
		if priority != nil && policy.Priority != *priority {
			return false
		}
		if match != "" && policy.Match != match {
			return false
		}
		// if nexthop != "" && *policy.Nexthop != nexthop {
		// 	return false
		// }
		return true
	}).List(context.TODO(), &policyList); err != nil {
		return nil, fmt.Errorf("failed to list logical router policy: %v", err)
	}
	return policyList, nil
}

func (c Client) GetLogicalRouterPolicy(router string, priority int, match string, ignoreNotFound bool) (*ovnnb.LogicalRouterPolicy, error) {
	policyList := make([]ovnnb.LogicalRouterPolicy, 0, 1)
	if err := c.nbClient.WhereCache(func(policy *ovnnb.LogicalRouterPolicy) bool {
		return policy.Priority == priority && policy.Match == match
	}).List(context.TODO(), &policyList); err != nil {
		return nil, fmt.Errorf("failed to list logical router policy: %v", err)
	}
	if len(policyList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("logical router policy with priority %d and match %s does not exist", priority, match)
	}
	if len(policyList) != 1 {
		return nil, fmt.Errorf("found multiple logical router policies with priority %d and match %s", priority, match)
	}

	return &policyList[0], nil
}

func (c Client) CreateLogicalRouterPolicy(router string, priority int, match string, action ovnnb.LogicalRouterPolicyAction, nextHop string) error {
	lrp, err := c.GetLogicalRouterPolicy(router, priority, match, true)
	if err != nil {
		return err
	}
	if lrp != nil {
		return nil
	}

	lrp = &ovnnb.LogicalRouterPolicy{
		Priority:    priority,
		Match:       match,
		Action:      action,
		ExternalIDs: map[string]string{"vendor": util.CniTypeName},
	}
	if nextHop != "" {
		lrp.Nexthop = &nextHop
	}
	ops, err := c.nbClient.Create(lrp)
	if err != nil {
		return fmt.Errorf("failed to generate create operations for logical router policy %v: %v", lrp, err)
	}
	if err = Transact(c.nbClient, "lr-policy-add", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to create logical router policy %v: %v", lrp, err)
	}

	return nil
}

func (c Client) DeleteLogicalRouterPolicy(router string, priority int, match string) error {
	policy, err := c.GetLogicalRouterPolicy(router, priority, match, true)
	if err != nil {
		return err
	}
	if policy == nil {
		return nil
	}

	ops, err := c.nbClient.Where(policy).Delete()
	if err != nil {
		return fmt.Errorf("failed to generate delete operations for logical router policy with priority %d and match %s in router %s: %v", priority, match, router, err)
	}
	if err = Transact(c.nbClient, "lr-policy-del", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to delete logical router policy with priority %d and match %s in router %s: %v", priority, match, router, err)
	}

	return nil
}

func (c Client) DeleteLogicalRouterPolicyByNexthop(router string, priority int, nexthop string) error {
	policies, err := c.ListLogicalRouterPolicy(router, &priority, "", nexthop)
	if err != nil {
		return err
	}

	var ops []ovsdb.Operation
	for _, policy := range policies {
		op, err := c.nbClient.Where(policy).Delete()
		if err != nil {
			return fmt.Errorf("failed to generate delete operations for logical router policy with priority %d and match %s in router %s: %v", policy.Priority, policy.Match, router, err)
		}
		ops = append(ops, op...)
	}

	if err = Transact(c.nbClient, "lr-policy-del", ops, c.OvnTimeout); err != nil {
		return fmt.Errorf("failed to delete logical router policy with nexthop %s in router %s: %v", nexthop, router, err)
	}

	return nil
}
