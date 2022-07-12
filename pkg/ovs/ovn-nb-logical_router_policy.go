package ovs

import (
	"fmt"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
)

func (c OvnClient) AddRouterPolicy(lr *ovnnb.LogicalRouter, matchfield string, action ovnnb.LogicalRouterPolicyAction,
	opts map[string]string, extIDs map[string]string, priority int) error {
	lrPolicy := &ovnnb.LogicalRouterPolicy{
		Action:      action,
		Match:       matchfield,
		Options:     opts,
		Priority:    priority,
		ExternalIDs: extIDs,
		UUID:        ovsclient.NamedUUID(),
		Nexthop:     nil,
		Nexthops:    nil,
	}

	var ops []ovsdb.Operation

	waitOp := ConstructWaitForUniqueOperation("Logical_Router_Policy", "match", matchfield)
	ops = append(ops, waitOp)

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
		return fmt.Errorf("failed to generate create operations for router policy %s: %v", matchfield, err)
	}
	ops = append(ops, mutationOps...)

	if err = Transact(c.ovnNbClient, "lr-policy-add", ops, c.ovnNbClient.Timeout); err != nil {
		return fmt.Errorf("failed to create route policy %s: %v", matchfield, err)
	}
	return nil
}

func (c OvnClient) DeleteRouterPolicy(lr *ovnnb.LogicalRouter, uuid string) error {

	var ops []ovsdb.Operation

	delOps, err := c.ovnNbClient.Where(lr).Mutate(lr, model.Mutation{
		Field:   &lr.Policies,
		Mutator: ovsdb.MutateOperationDelete,
		Value:   []string{uuid},
	})
	if err != nil {
		return fmt.Errorf("failed to generate delete operations for router %s: %v", uuid, err)
	}
	ops = append(ops, delOps...)

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
