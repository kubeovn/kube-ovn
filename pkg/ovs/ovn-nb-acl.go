package ovs

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	netv1 "k8s.io/api/networking/v1"

	"github.com/ovn-org/libovsdb/ovsdb"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	aclParentKey = "acl-parent"
)

// CreateIngressACL creates an ingress ACL
func (c OvnClient) CreateIngressACL(pgName, asIngressName, asExceptName, protocol string, npp []netv1.NetworkPolicyPort) error {
	aclUUIDs := make([]string, 0)

	/* default drop acl */
	match := fmt.Sprintf("outport==@%s && ip", pgName)
	options := func(acl *ovnnb.ACL) {
		acl.Name = &pgName
		acl.Log = true
		acl.Severity = &ovnnb.ACLSeverityWarning
	}

	defaultDropAclOp, uuid, err := c.CreateAclOp(pgName, ovnnb.ACLDirectionToLport, util.IngressDefaultDrop, match, ovnnb.ACLActionAllowRelated, options)
	if err != nil {
		return fmt.Errorf("generate operations for creating acl: %v", err)
	}

	aclUUIDs = append(aclUUIDs, uuid)

	/* allow acl */
	matches := newIngressAllowACLMatch(pgName, asIngressName, asExceptName, kubeovnv1.ProtocolIPv4, npp)

	allowAclOps := make([]ovsdb.Operation, 0, len(matches))
	for _, m := range matches {
		allowAclOp, uuid, err := c.CreateAclOp(pgName, ovnnb.ACLDirectionToLport, util.IngressDefaultDrop, m, ovnnb.ACLActionAllowRelated)
		if err != nil {
			return fmt.Errorf("generate operations for creating acl: %v", err)
		}
		aclUUIDs = append(aclUUIDs, uuid)
		allowAclOps = append(allowAclOps, allowAclOp...)
	}

	// acl attach to port group
	aclAddOp, err := c.portGroupUpdateAclOp(pgName, aclUUIDs, ovsdb.MutateOperationInsert)
	if err != nil {
		return fmt.Errorf("generate operations for port group %s adding acl %v: %v", pgName, aclUUIDs, err)
	}

	ops := make([]ovsdb.Operation, 0, len(defaultDropAclOp)+len(allowAclOps)+len(aclAddOp))
	ops = append(ops, defaultDropAclOp...)
	ops = append(ops, allowAclOps...)
	ops = append(ops, aclAddOp...)

	if err = c.Transact("acl-add", ops); err != nil {
		return fmt.Errorf("add acls to port group %s: %v", pgName, err)
	}
	return nil
}

func (c OvnClient) CreateBareACL(direction, priority, match, action string) error {
	op, _, err := c.CreateAclOp("", direction, priority, match, action)
	if err != nil {
		return fmt.Errorf("generate operations for create acl: %v", err)
	}

	if err = c.Transact("acl-create", op); err != nil {
		return fmt.Errorf("add acl direction %s priority %s match %s action %s: %v", direction, priority, match, action, err)
	}

	return nil
}

// GetAcl get acl by direction, priority and match,
// be consistent with ovn-nbctl which direction, priority and match determine one acl rule
func (c OvnClient) GetAcl(direction, priority, match string, ignoreNotFound bool) (*ovnnb.ACL, error) {
	intPriority, _ := strconv.Atoi(priority)

	aclList := make([]ovnnb.ACL, 0)
	if err := c.ovnNbClient.WhereCache(func(acl *ovnnb.ACL) bool {
		return acl.Direction == direction && acl.Priority == intPriority && acl.Match == match
	}).List(context.TODO(), &aclList); err != nil {
		return nil, fmt.Errorf("get acl direction %s priority %s match %s: %v", direction, priority, match, err)
	}

	// not found
	if len(aclList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("not found acl direction %s priority %s match %s", direction, priority, match)
	}

	if len(aclList) > 1 {
		return nil, fmt.Errorf("more than one acl with same direction %s priority %s match %s", direction, priority, match)
	}

	return &aclList[0], nil
}

func (c OvnClient) AclExists(direction, priority, match string) (bool, error) {
	acl, err := c.GetAcl(direction, priority, match, true)
	return acl != nil, err
}

// CreateAclOp generate operation which create an ACL
func (c OvnClient) CreateAclOp(parentName, direction, priority, match, action string, options ...func(acl *ovnnb.ACL)) ([]ovsdb.Operation, string, error) {
	if len(direction) == 0 || len(priority) == 0 || len(match) == 0 || len(action) == 0 {
		return nil, "", fmt.Errorf("acl direction or priority or match or action is empty")
	}

	exists, err := c.AclExists(direction, priority, match)
	if err != nil {
		return nil, "", err
	}

	// found, ingore
	if exists {
		return nil, "", nil
	}

	acl := newAcl(parentName, direction, priority, match, action, options...)

	op, err := c.ovnNbClient.Create(acl)
	if err != nil {
		return nil, "", fmt.Errorf("generate operations for creating acl direction %s priority %d match %s action %s: %v", acl.Direction, acl.Priority, acl.Match, acl.Action, err)
	}

	return op, acl.UUID, nil
}

// newAcl return acl with basic information
func newAcl(parentName, direction, priority, match, action string, options ...func(acl *ovnnb.ACL)) *ovnnb.ACL {
	intPriority, _ := strconv.Atoi(priority)

	acl := &ovnnb.ACL{
		UUID:      ovsclient.UUID(),
		Action:    action,
		Direction: direction,
		Match:     match,
		Priority:  intPriority,
		ExternalIDs: map[string]string{
			aclParentKey: parentName,
		},
	}

	for _, option := range options {
		option(acl)
	}

	return acl
}

func newIngressAllowACLMatch(pgName, asIngressName, asExceptName, protocol string, npp []netv1.NetworkPolicyPort) []string {
	/* allow acl */
	ipSuffix := "ip4"
	if protocol == kubeovnv1.ProtocolIPv6 {
		ipSuffix = "ip6"
	}

	matches := make([]string, 0)

	if len(npp) == 0 {
		match := fmt.Sprintf("%s.src == $%s && %s.src != $%s && outport == @%s && ip", ipSuffix, asIngressName, ipSuffix, asExceptName, pgName)
		matches = append(matches, match)
		return matches
	}

	for _, port := range npp {
		protocol := strings.ToLower(string(*port.Protocol))
		if port.Port == nil {
			match := fmt.Sprintf("%s.src == $%s && %s.src != $%s && %s && outport == @%s && ip", ipSuffix, asIngressName, ipSuffix, asExceptName, protocol, pgName)
			matches = append(matches, match)
			continue
		}

		if port.EndPort == nil {
			match := fmt.Sprintf("%s.src == $%s && %s.src != $%s && %s.dst == %d && outport == @%s && ip", ipSuffix, asIngressName, ipSuffix, asExceptName, protocol, port.Port.IntVal, pgName)
			matches = append(matches, match)
			continue
		}

		match := fmt.Sprintf("%s.src == $%s && %s.src != $%s && %d <= %s.dst <= %d && outport == @%s && ip", ipSuffix, asIngressName, ipSuffix, asExceptName, port.Port.IntVal, protocol, *port.EndPort, pgName)
		matches = append(matches, match)
	}

	return matches
}
