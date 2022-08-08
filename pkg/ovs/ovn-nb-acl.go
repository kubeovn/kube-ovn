package ovs

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	netv1 "k8s.io/api/networking/v1"

	"github.com/ovn-org/libovsdb/model"
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
func (c OvnClient) CreateIngressAcl(pgName, asIngressName, asExceptName, protocol string, npp []netv1.NetworkPolicyPort) error {
	acls := make([]*ovnnb.ACL, 0)

	/* default drop acl */
	AllIpMatch := NewAndAclMatchRule(
		NewAclRuleKv("outport", "==", "@"+pgName, ""),
		NewAclRuleKv("ip", "", "", ""),
	)
	options := func(acl *ovnnb.ACL) {
		acl.Name = &pgName
		acl.Log = true
		acl.Severity = &ovnnb.ACLSeverityWarning
	}

	defaultDropAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionToLport, util.IngressDefaultDrop, AllIpMatch.String(), ovnnb.ACLActionDrop, options)
	if err != nil {
		return fmt.Errorf("new ingress default drop acl direction %s priority %s match %s action %s: %v", ovnnb.ACLDirectionToLport, util.IngressDefaultDrop, AllIpMatch, ovnnb.ACLActionDrop, err)
	}

	acls = append(acls, defaultDropAcl)

	/* allow acl */
	matches := newAllowAclMatch(pgName, asIngressName, asExceptName, kubeovnv1.ProtocolIPv4, ovnnb.ACLDirectionToLport, npp)
	for _, m := range matches {
		allowAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, m, ovnnb.ACLActionAllowRelated)
		if err != nil {
			return fmt.Errorf("new ingress allow acl direction %s priority %s match %s action %s: %v", ovnnb.ACLDirectionToLport, util.IngressAllowPriority, m, ovnnb.ACLActionAllowRelated, err)
		}

		acls = append(acls, allowAcl)
	}

	if err := c.CreateAcls(pgName, acls...); err != nil {
		return fmt.Errorf("add ingress acls to port group %s: %v", pgName, err)
	}

	return nil
}

// CreateIngressACL creates an egress ACL
func (c OvnClient) CreateEgressAcl(pgName, asEgressName, asExceptName, protocol string, npp []netv1.NetworkPolicyPort) error {
	acls := make([]*ovnnb.ACL, 0)

	/* default drop acl */
	AllIpMatch := NewAndAclMatchRule(
		NewAclRuleKv("inport", "==", "@"+pgName, ""),
		NewAclRuleKv("ip", "", "", ""),
	)
	options := func(acl *ovnnb.ACL) {
		acl.Name = &pgName
		acl.Log = true
		acl.Severity = &ovnnb.ACLSeverityWarning
	}

	defaultDropAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionFromLport, util.EgressDefaultDrop, AllIpMatch.String(), ovnnb.ACLActionDrop, options)
	if err != nil {
		return fmt.Errorf("new egress default drop acl direction %s priority %s match %s action %s: %v", ovnnb.ACLDirectionFromLport, util.EgressDefaultDrop, AllIpMatch, ovnnb.ACLActionDrop, err)
	}

	acls = append(acls, defaultDropAcl)

	/* allow acl */
	matches := newAllowAclMatch(pgName, asEgressName, asExceptName, kubeovnv1.ProtocolIPv4, ovnnb.ACLDirectionFromLport, npp)
	for _, m := range matches {
		allowAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, m, ovnnb.ACLActionAllowRelated)
		if err != nil {
			return fmt.Errorf("new egress allow acl direction %s priority %s match %s action %s: %v", ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, m, ovnnb.ACLActionAllowRelated, err)
		}

		acls = append(acls, allowAcl)
	}

	if err := c.CreateAcls(pgName, acls...); err != nil {
		return fmt.Errorf("add egress acls to port group %s: %v", pgName, err)
	}

	return nil
}

// CreateAcls generate operation which create several acl once
func (c OvnClient) CreateAcls(parentName string, acls ...*ovnnb.ACL) error {
	models := make([]model.Model, 0, len(acls))
	aclUUIDs := make([]string, 0, len(acls))
	for _, acl := range acls {
		if acl != nil {
			models = append(models, model.Model(acl))
			aclUUIDs = append(aclUUIDs, acl.UUID)
		}
	}

	createAclsOp, err := c.ovnNbClient.Create(models...)
	if err != nil {
		return fmt.Errorf("generate operations for creating acls: %v", err)
	}

	// acl attach to port group
	aclAddOp, err := c.portGroupUpdateAclOp(parentName, aclUUIDs, ovsdb.MutateOperationInsert)
	if err != nil {
		return fmt.Errorf("generate operations for adding acls to %s: %v", parentName, err)
	}

	ops := make([]ovsdb.Operation, 0, len(createAclsOp)+len(aclAddOp))
	ops = append(ops, createAclsOp...)
	ops = append(ops, aclAddOp...)

	if err = c.Transact("acls-add", ops); err != nil {
		return fmt.Errorf("add acls to %s: %v", parentName, err)
	}

	return nil
}

func (c OvnClient) CreateBareAcl(parentName, direction, priority, match, action string) error {
	acl, err := c.newAcl(parentName, direction, priority, match, action)
	if err != nil {
		return fmt.Errorf("new acl direction %s priority %s match %s action %s: %v", direction, priority, match, action, err)
	}

	op, err := c.ovnNbClient.Create(acl)
	if err != nil {
		return fmt.Errorf("generate operations for creating acl direction %s priority %s match %s action %s: %v", direction, priority, match, action, err)
	}

	if err = c.Transact("acl-create", op); err != nil {
		return fmt.Errorf("create acl direction %s priority %s match %s action %s: %v", direction, priority, match, action, err)
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

// ListAcls list acks which match the given externalIDs,
// result should include all to-lport and from-lport acls when direction is empty,
// result should include all acls when externalIDs is empty,
// result should include all acls which externalIDs[key] is not empty when externalIDs[key] is ""
// TODO: maybe add other filter conditions(priority or match)
func (c OvnClient) ListAcls(direction string, externalIDs map[string]string) ([]ovnnb.ACL, error) {
	aclList := make([]ovnnb.ACL, 0)

	if err := c.WhereCache(func(acl *ovnnb.ACL) bool {
		if len(acl.ExternalIDs) < len(externalIDs) {
			return false
		}

		if len(acl.ExternalIDs) != 0 {
			for k, v := range externalIDs {
				// if only key exist but not value in externalIDs, we should include this lsp,
				// it's equal to shell command `ovn-nbctl --columns=xx find acl external_ids:key!=\"\"`
				if len(v) == 0 {
					if len(acl.ExternalIDs[k]) == 0 {
						return false
					}
				} else {
					if acl.ExternalIDs[k] != v {
						return false
					}
				}
			}
		}

		if len(direction) != 0 && acl.Direction != direction {
			return false
		}

		return true
	}).List(context.TODO(), &aclList); err != nil {
		return nil, fmt.Errorf("list acls: %v", err)
	}

	return aclList, nil
}

func (c OvnClient) AclExists(direction, priority, match string) (bool, error) {
	acl, err := c.GetAcl(direction, priority, match, true)
	return acl != nil, err
}

// newAcl return acl with basic information
func (c OvnClient) newAcl(parentName, direction, priority, match, action string, options ...func(acl *ovnnb.ACL)) (*ovnnb.ACL, error) {
	if len(direction) == 0 || len(priority) == 0 || len(match) == 0 || len(action) == 0 {
		return nil, fmt.Errorf("acl 'direction %s' or 'priority %s' or 'match %s' or 'action %s' is empty", direction, priority, match, action)
	}

	exists, err := c.AclExists(direction, priority, match)
	if err != nil {
		return nil, err
	}

	// found, ingore
	if exists {
		return nil, nil
	}

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

	return acl, nil
}

func newAllowAclMatch(pgName, asAllowName, asExceptName, protocol, direction string, npp []netv1.NetworkPolicyPort) []string {
	ipSuffix := "ip4"
	if protocol == kubeovnv1.ProtocolIPv6 {
		ipSuffix = "ip6"
	}

	// ingress rule
	srcOrDst, portDriection := "src", "outport"
	if direction == ovnnb.ACLDirectionFromLport { // egress rule
		srcOrDst = "dst"
		portDriection = "inport"
	}

	ipKey := ipSuffix + "." + srcOrDst

	// match all traffic to or from pgName
	AllIpMatch := NewAndAclMatchRule(
		NewAclRuleKv(portDriection, "==", "@"+pgName, ""),
		NewAclRuleKv("ip", "", "", ""),
	)

	allowedIpMatch := NewAndAclMatchRule(
		AllIpMatch,
		NewAclRuleKv(ipKey, "==", "$"+asAllowName, ""),
		NewAclRuleKv(ipKey, "!=", "$"+asExceptName, ""),
	)

	matches := make([]string, 0)

	// allow allowed ip traffic but except
	if len(npp) == 0 {
		return []string{allowedIpMatch.String()}
	}

	for _, port := range npp {
		protocol := strings.ToLower(string(*port.Protocol))

		// allow all tcp or udp traffic
		if port.Port == nil {
			allLayer4Match := NewAndAclMatchRule(
				allowedIpMatch,
				NewAclRuleKv(protocol, "", "", ""),
			)

			matches = append(matches, allLayer4Match.String())
			continue
		}

		// allow one tcp or udp port traffic
		if port.EndPort == nil {
			tcpKey := protocol + ".dst"
			oneTcpMatch := NewAndAclMatchRule(
				allowedIpMatch,
				NewAclRuleKv(tcpKey, "==", fmt.Sprintf("%d", port.Port.IntVal), ""),
			)

			matches = append(matches, oneTcpMatch.String())
			continue
		}

		// allow several tcp or udp port traffic
		tcpKey := protocol + ".dst"
		severalTcpMatch := NewAndAclMatchRule(
			allowedIpMatch,
			NewAclRuleKv(tcpKey, "<=", fmt.Sprintf("%d", port.Port.IntVal), fmt.Sprintf("%d", *port.EndPort)),
		)
		matches = append(matches, severalTcpMatch.String())
	}

	return matches
}
