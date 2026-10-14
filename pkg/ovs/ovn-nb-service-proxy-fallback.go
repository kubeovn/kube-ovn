package ovs

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const serviceProxyFallbackACLKey = "isServiceProxyFallbackACL"

// ReconcileServiceProxyFallbackACL atomically replaces the switch's owned
// post-LB fallback ACLs while preserving unrelated ACLs and unchanged rows.
func (c *OVNNbClient) ReconcileServiceProxyFallbackACL(lsName string, matches []string) error {
	if lsName == "" {
		return errors.New("logical switch name is required")
	}
	desired := make(map[string]bool, len(matches))
	for _, match := range matches {
		if match == "" {
			return errors.New("service proxy fallback ACL match is required")
		}
		desired[match] = false
	}
	ls, err := c.GetLogicalSwitch(lsName, len(matches) == 0)
	if err != nil || ls == nil {
		return err
	}
	externalIDs := map[string]string{
		aclParentKey: lsName, "vendor": util.CniTypeName, serviceProxyFallbackACLKey: "true",
	}
	acls, err := c.ListAcls("", externalIDs)
	if err != nil {
		return fmt.Errorf("list service proxy fallback ACLs for switch %s: %w", lsName, err)
	}
	options := map[string]string{"apply-after-lb": "true"}
	stale := make([]string, 0, len(acls))
	for _, acl := range acls {
		kept, wanted := desired[acl.Match]
		if wanted && !kept && slices.Contains(ls.ACLs, acl.UUID) &&
			acl.Direction == ovnnb.ACLDirectionFromLport && acl.Tier == 0 &&
			acl.Priority == util.ServiceProxyFallbackACLPriority && acl.Action == ovnnb.ACLActionDrop &&
			maps.Equal(acl.Options, options) && maps.Equal(acl.ExternalIDs, externalIDs) {
			desired[acl.Match] = true
		} else {
			stale = append(stale, acl.UUID)
		}
	}
	create := make([]*ovnnb.ACL, 0, len(desired))
	for match, kept := range desired {
		if !kept {
			create = append(create, &ovnnb.ACL{
				UUID: ovsclient.NamedUUID(), Direction: ovnnb.ACLDirectionFromLport,
				Priority: util.ServiceProxyFallbackACLPriority, Match: match, Action: ovnnb.ACLActionDrop,
				Options: options, ExternalIDs: externalIDs,
			})
		}
	}
	ops, err := c.CreateAclsOps(lsName, LogicalSwitchKey, create...)
	if err != nil {
		return fmt.Errorf("create service proxy fallback ACL operations for switch %s: %w", lsName, err)
	}
	deleteOps, err := c.logicalSwitchUpdateACLOp(lsName, stale, ovsdb.MutateOperationDelete)
	if err != nil {
		return fmt.Errorf("delete service proxy fallback ACL operations for switch %s: %w", lsName, err)
	}
	if err := c.Transact("service-proxy-fallback-acl-reconcile", append(ops, deleteOps...)); err != nil {
		return fmt.Errorf("reconcile service proxy fallback ACLs for switch %s: %w", lsName, err)
	}
	return nil
}

// DeleteLogicalSwitchPolicyACLs clears subnet policy ACLs while retaining the
// independently managed Service proxy fallback protection. DeleteAcls still
// removes every ACL when the logical switch itself is being removed.
func (c *OVNNbClient) DeleteLogicalSwitchPolicyACLs(lsName string) error {
	if lsName == "" {
		return errors.New("logical switch name is required")
	}
	acls, err := c.ListAcls("", map[string]string{aclParentKey: lsName})
	if err != nil {
		return fmt.Errorf("list policy ACLs for switch %s: %w", lsName, err)
	}
	stale := make([]string, 0, len(acls))
	for _, acl := range acls {
		if acl.ExternalIDs[serviceProxyFallbackACLKey] != "true" || acl.ExternalIDs["vendor"] != util.CniTypeName {
			stale = append(stale, acl.UUID)
		}
	}
	ops, err := c.logicalSwitchUpdateACLOp(lsName, stale, ovsdb.MutateOperationDelete)
	if err != nil {
		return fmt.Errorf("delete policy ACL operations for switch %s: %w", lsName, err)
	}
	if err := c.Transact("logical-switch-policy-acls-delete", ops); err != nil {
		return fmt.Errorf("delete policy ACLs for switch %s: %w", lsName, err)
	}
	return nil
}
