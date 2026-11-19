package ovs

import (
	"fmt"
	"maps"
	"slices"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const LoadBalancerIngressGuardExternalID = "kube-ovn.io/lb-ingress-guard"

func (c *OVNNbClient) resourceLoadBalancerIngressGuardOps(plan LoadBalancerReconciliation) ([]ovsdb.Operation, error) {
	owner := maps.Clone(plan.OwnerExternalIDs)
	owner[LoadBalancerIngressGuardExternalID] = "true"
	existing, err := c.ListAcls("", owner)
	if err != nil {
		return nil, err
	}
	if len(existing) == 0 && len(plan.IngressGuards) == 0 {
		return nil, nil
	}
	switches, err := c.ListLogicalSwitch(false, nil)
	if err != nil {
		return nil, err
	}
	managed := make(map[string]struct{}, len(existing))
	for _, acl := range existing {
		managed[acl.UUID] = struct{}{}
	}
	var ops []ovsdb.Operation
	retained := make(map[string]struct{})
	for _, ls := range switches {
		uuids := slices.DeleteFunc(slices.Clone(ls.ACLs), func(uuid string) bool { _, ok := managed[uuid]; return ok })
		for _, guard := range plan.IngressGuards {
			if guard.LogicalSwitch != ls.Name {
				continue
			}
			acl := &ovnnb.ACL{
				UUID: ovsclient.NamedUUID(), Direction: ovnnb.ACLDirectionFromLport,
				Priority: 32700, Match: guard.Match, Action: ovnnb.ACLActionDrop,
				Options: map[string]string{"apply-after-lb": "false"}, ExternalIDs: maps.Clone(owner),
			}
			acl.ExternalIDs["vendor"] = util.CniTypeName
			acl.ExternalIDs[aclParentKey] = ls.Name
			index := slices.IndexFunc(existing, func(row ovnnb.ACL) bool {
				return slices.Contains(ls.ACLs, row.UUID) && row.Direction == acl.Direction &&
					row.Priority == acl.Priority && row.Match == acl.Match && row.Action == acl.Action &&
					row.Tier == acl.Tier && maps.Equal(row.Options, acl.Options)
			})
			if index >= 0 {
				acl = &existing[index]
				retained[acl.UUID] = struct{}{}
				guards, err := c.Where(acl).Wait(ovsdb.WaitConditionEqual, new(0), acl,
					&acl.Direction, &acl.Priority, &acl.Match, &acl.Action, &acl.Tier, &acl.Options, &acl.ExternalIDs)
				if err != nil {
					return nil, err
				}
				ops = append(ops, guards...)
			} else {
				createOps, err := c.Create(acl)
				if err != nil {
					return nil, err
				}
				ops = append(ops, createOps...)
			}
			uuids = append(uuids, acl.UUID)
		}
		attachmentOps, err := c.resourceLoadBalancerParentOps(&ls, &ls.ACLs, uuids)
		if err != nil {
			return nil, fmt.Errorf("attach load balancer ingress guards to %s: %w", ls.Name, err)
		}
		ops = append(ops, attachmentOps...)
	}
	for _, acl := range existing {
		if _, ok := retained[acl.UUID]; ok {
			continue
		}
		deleteOps, err := c.resourceLoadBalancerDeleteOps(&acl, ovnnb.ACLTable, acl.UUID, owner)
		if err != nil {
			return nil, err
		}
		ops = append(ops, deleteOps...)
	}
	return ops, nil
}
