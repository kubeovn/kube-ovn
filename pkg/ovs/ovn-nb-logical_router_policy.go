package ovs

import (
	"fmt"

	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

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

	if err = Transact(c.ovnNbClient, "lr-policy-add", ops, c.ovnNbClient.Timeout); err != nil {
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

	if err = Transact(c.ovnNbClient, "lr-policy-delete", ops, c.ovnNbClient.Timeout); err != nil {
		return fmt.Errorf("failed to delete route policy %s: %v", uuid, err)
	}
	return nil
}
