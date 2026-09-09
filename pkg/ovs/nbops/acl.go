package nbops

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

const (
	aclParentPortGroup     = "pg"
	aclParentLogicalSwitch = "ls"
)

// ACLs provides resource-level parent ownership operations for ACL rows.
// Parent lookup uses LogicalSwitch.ACLs and PortGroup.ACLs membership, not
// external_ids["parent"].
type ACLs struct {
	acl        table
	switches   table
	portGroups table
	executor   compat.Executor
}

// NewACLs creates a typed facade over an NB table provider.
func NewACLs(provider compat.TableProvider, executor compat.Executor) *ACLs {
	if provider == nil {
		return &ACLs{executor: executor}
	}
	return &ACLs{
		acl:        provider.Table(&ovnnb.ACL{}),
		switches:   provider.Table(&ovnnb.LogicalSwitch{}),
		portGroups: provider.Table(&ovnnb.PortGroup{}),
		executor:   executor,
	}
}

// EnsureParent makes exactly one logical switch or port group the owner of an
// ACL. Any stale parent references are removed before the desired parent is
// attached in one transaction. parentType must be "pg" or "ls".
func (a *ACLs) EnsureParent(ctx context.Context, parentName, parentType, aclUUID string) error {
	if a == nil || a.acl == nil || a.switches == nil || a.portGroups == nil || a.executor == nil {
		return errors.New("acl facade is nil")
	}
	if parentName == "" || aclUUID == "" {
		return errors.New("acl parent name and uuid are required")
	}
	if parentType != aclParentPortGroup && parentType != aclParentLogicalSwitch {
		return errors.New("acl parent type must be 'pg' or 'ls'")
	}

	acl := &ovnnb.ACL{UUID: aclUUID}
	if err := a.acl.Get(ctx, acl); err != nil {
		return fmt.Errorf("get acl %s: %w", aclUUID, err)
	}

	var targetSwitch *ovnnb.LogicalSwitch
	var targetGroup *ovnnb.PortGroup
	if parentType == aclParentLogicalSwitch {
		target, err := getNamed(ctx, a.switches, parentName, "logical switch", func(row *ovnnb.LogicalSwitch) string { return row.Name })
		if err != nil {
			return err
		}
		targetSwitch = target
	} else {
		target, err := getNamed(ctx, a.portGroups, parentName, "port group", func(row *ovnnb.PortGroup) string { return row.Name })
		if err != nil {
			return err
		}
		targetGroup = target
	}

	lsParents, err := listMatching(ctx, a.switches, func(row *ovnnb.LogicalSwitch) bool {
		return slices.Contains(row.ACLs, aclUUID)
	})
	if err != nil {
		return fmt.Errorf("find switch parents for acl %s: %w", aclUUID, err)
	}
	pgParents, err := listMatching(ctx, a.portGroups, func(row *ovnnb.PortGroup) bool {
		return slices.Contains(row.ACLs, aclUUID)
	})
	if err != nil {
		return fmt.Errorf("find port group parents for acl %s: %w", aclUUID, err)
	}

	plan := compat.NewTxPlan("acl-parent")
	hasTarget := false
	for i := range lsParents {
		parent := &lsParents[i]
		if parentType == aclParentLogicalSwitch && parent.Name == parentName {
			hasTarget = true
			continue
		}
		operations, err := a.switches.MutateOps(parent, model.Mutation{
			Field: &parent.ACLs, Value: []string{aclUUID}, Mutator: ovsdb.MutateOperationDelete,
		})
		if err != nil {
			return fmt.Errorf("detach acl %s from %s: %w", aclUUID, parent.Name, err)
		}
		plan.Add(operations...)
	}
	for i := range pgParents {
		parent := &pgParents[i]
		if parentType == aclParentPortGroup && parent.Name == parentName {
			hasTarget = true
			continue
		}
		operations, err := a.portGroups.MutateOps(parent, model.Mutation{
			Field: &parent.ACLs, Value: []string{aclUUID}, Mutator: ovsdb.MutateOperationDelete,
		})
		if err != nil {
			return fmt.Errorf("detach acl %s from %s: %w", aclUUID, parent.Name, err)
		}
		plan.Add(operations...)
	}
	if !hasTarget {
		if parentType == aclParentLogicalSwitch {
			operations, err := a.switches.MutateOps(targetSwitch, model.Mutation{
				Field: &targetSwitch.ACLs, Value: []string{aclUUID}, Mutator: ovsdb.MutateOperationInsert,
			})
			if err != nil {
				return fmt.Errorf("attach acl %s to %s: %w", aclUUID, parentName, err)
			}
			plan.Add(operations...)
		} else {
			operations, err := a.portGroups.MutateOps(targetGroup, model.Mutation{
				Field: &targetGroup.ACLs, Value: []string{aclUUID}, Mutator: ovsdb.MutateOperationInsert,
			})
			if err != nil {
				return fmt.Errorf("attach acl %s to %s: %w", aclUUID, parentName, err)
			}
			plan.Add(operations...)
		}
	}
	return a.executor.Execute(ctx, plan)
}
