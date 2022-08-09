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
		return fmt.Errorf("new ingress default drop acl for port group %s: %v", pgName, err)
	}

	acls = append(acls, defaultDropAcl)

	/* allow acl */
	matches := newNetworkPolicyAclMatch(pgName, asIngressName, asExceptName, kubeovnv1.ProtocolIPv4, ovnnb.ACLDirectionToLport, npp)
	for _, m := range matches {
		allowAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, m, ovnnb.ACLActionAllowRelated)
		if err != nil {
			return fmt.Errorf("new ingress allow acl for port group %s: %v", pgName, err)
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
		return fmt.Errorf("new egress default drop acl for port group %s: %v", pgName, err)
	}

	acls = append(acls, defaultDropAcl)

	/* allow acl */
	matches := newNetworkPolicyAclMatch(pgName, asEgressName, asExceptName, kubeovnv1.ProtocolIPv4, ovnnb.ACLDirectionFromLport, npp)
	for _, m := range matches {
		allowAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, m, ovnnb.ACLActionAllowRelated)
		if err != nil {
			return fmt.Errorf("new egress allow acl for port group %s: %v", pgName, err)
		}

		acls = append(acls, allowAcl)
	}

	if err := c.CreateAcls(pgName, acls...); err != nil {
		return fmt.Errorf("add egress acls to port group %s: %v", pgName, err)
	}

	return nil
}

// CreateGatewayACL create allow acl for subnet gateway
func (c OvnClient) CreateGatewayAcl(pgName, gateway string) error {
	acls := make([]*ovnnb.ACL, 0)

	for _, gw := range strings.Split(gateway, ",") {
		protocol := util.CheckProtocol(gw)
		ipSuffix := "ip4"
		if protocol == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
		}

		allowIngressAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, fmt.Sprintf("%s.src == %s", ipSuffix, gw), ovnnb.ACLActionAllowRelated)
		if err != nil {
			return fmt.Errorf("new ingress allow acl for port group %s: %v", pgName, err)
		}

		allowEgressAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, fmt.Sprintf("%s.dst == %s", ipSuffix, gw), ovnnb.ACLActionAllowRelated)
		if err != nil {
			return fmt.Errorf("new egress allow acl for port group %s: %v", pgName, err)
		}

		acls = append(acls, allowIngressAcl, allowEgressAcl)
	}

	if err := c.CreateAcls(pgName, acls...); err != nil {
		return fmt.Errorf("add gateway acls to port group %s: %v", pgName, err)
	}

	return nil
}

// CreateGatewayACL create allow acl for node join ip
func (c OvnClient) CreateNodeAcl(pgName, nodeIp string) error {
	acls := make([]*ovnnb.ACL, 0)
	for _, ip := range strings.Split(nodeIp, ",") {
		protocol := util.CheckProtocol(ip)
		ipSuffix := "ip4"
		if protocol == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
		}
		pgAs := fmt.Sprintf("%s_%s", pgName, ipSuffix)

		allowIngressAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionToLport, util.NodeAllowPriority, fmt.Sprintf("%s.src == %s && %s.dst == $%s", ipSuffix, ip, ipSuffix, pgAs), ovnnb.ACLActionAllowRelated)
		if err != nil {
			return fmt.Errorf("new ingress allow acl for port group %s: %v", pgName, err)
		}

		allowEgressAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionFromLport, util.NodeAllowPriority, fmt.Sprintf("%s.dst == %s && %s.src == $%s", ipSuffix, ip, ipSuffix, pgAs), ovnnb.ACLActionAllowRelated)
		if err != nil {
			return fmt.Errorf("new egress allow acl for port group %s: %v", pgName, err)
		}

		acls = append(acls, allowIngressAcl, allowEgressAcl)
	}

	if err := c.CreateAcls(pgName, acls...); err != nil {
		return fmt.Errorf("add node acls to port group %s: %v", pgName, err)
	}

	return nil
}

func (c OvnClient) CreateSgDenyAllAcl(sgName string) error {
	pgName := GetSgPortGroupName(sgName)

	ingressAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionToLport, util.SecurityGroupDropPriority, fmt.Sprintf("outport == @%s && ip", pgName), ovnnb.ACLActionDrop)
	if err != nil {
		return fmt.Errorf("new deny all ingress acl for port group %s: %v", pgName, err)
	}

	egressAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionFromLport, util.SecurityGroupDropPriority, fmt.Sprintf("inport == @%s && ip", pgName), ovnnb.ACLActionDrop)
	if err != nil {
		return fmt.Errorf("new deny all security group egress acl for port group %s: %v", pgName, err)
	}

	if err := c.CreateAcls(pgName, ingressAcl, egressAcl); err != nil {
		return fmt.Errorf("add deny all acl to port group %s: %v", pgName, err)
	}

	return nil
}

func (c OvnClient) UpdateSgACL(sg *kubeovnv1.SecurityGroup, direction string) error {
	pgName := GetSgPortGroupName(sg.Name)
	// clear one-way direction acl
	if err := c.DeletePortGroupAcls(pgName, direction); err != nil {
		return err
	}

	// clear address_set
	if err := c.DeleteAddressSets(map[string]string{"sg": sg.Name}); err != nil {
		return err
	}

	acls := make([]*ovnnb.ACL, 0, 2)

	// ingress rule
	srcOrDst, portDriection, sgRules := "src", "outport", sg.Spec.IngressRules
	if direction == ovnnb.ACLDirectionFromLport { // egress rule
		srcOrDst = "dst"
		portDriection = "inport"
		sgRules = sg.Spec.EgressRules
	}

	/* create port_group associated acl */
	if sg.Spec.AllowSameGroupTraffic {
		asName := GetSgV4AssociatedName(sg.Name)
		for _, ipSuffix := range []string{"ip4", "ip6"} {
			if ipSuffix == "ip6" {
				asName = GetSgV6AssociatedName(sg.Name)
			}

			match := NewAndAclMatchRule(
				NewAclRuleKv(portDriection, "==", "@"+pgName, ""),
				NewAclRuleKv(ipSuffix, "", "", ""),
				NewAclRuleKv(ipSuffix+"."+srcOrDst, "==", "$"+asName, ""),
			)
			acl, err := c.newAcl(pgName, direction, util.SecurityGroupAllowPriority, match.String(), ovnnb.ACLActionAllowRelated)
			if err != nil {
				return fmt.Errorf("new %s allow acl for port group %s: %v", ipSuffix, pgName, err)
			}

			acls = append(acls, acl)
		}
	}

	/* recreate rule ACL */
	for _, rule := range sgRules {
		acl, err := c.newSgRuleACL(sg.Name, direction, rule)
		if err != nil {
			return fmt.Errorf("create security group %s rule acl: %v", sg.Name, err)
		}
		acls = append(acls, acl)
	}

	if err := c.CreateAcls(pgName, acls...); err != nil {
		return fmt.Errorf("add acl to port group %s: %v", pgName, err)
	}

	return nil
}

// CreateAcls generate operation which create several acl once
func (c OvnClient) CreateAcls(parentName string, acls ...*ovnnb.ACL) error {
	if len(acls) == 0 {
		return nil
	}

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

// DeletePortGroupAcl delete acls from port group,
// delete to-lport and from-lport direction acl when direction is empty, otherwise one-way
func (c OvnClient) DeletePortGroupAcls(pgName, direction string) error {
	_, err := c.GetPortGroup(pgName, false)
	if err != nil {
		return fmt.Errorf("get port group %s", pgName)
	}

	externalIDs := map[string]string{aclParentKey: pgName}

	/* delete acls from port group */
	acls, err := c.ListAcls(direction, externalIDs)
	if err != nil {
		return fmt.Errorf("list port groups %s acls: %v", pgName, err)
	}

	aclUUIDs := make([]string, 0, len(acls))
	for _, acl := range acls {
		aclUUIDs = append(aclUUIDs, acl.UUID)
	}

	removeAclsOp, err := c.portGroupUpdateAclOp(pgName, aclUUIDs, ovsdb.MutateOperationDelete)
	if err != nil {
		return fmt.Errorf("generate operation for deleting port group %s direction '%s' acls: %v", pgName, direction, err)
	}

	// delete acls
	delAclsOp, err := c.WhereCache(aclFilter(direction, externalIDs)).Delete()
	if err != nil {
		return fmt.Errorf("generate operation for deleting acls: %v", err)
	}

	ops := make([]ovsdb.Operation, 0, len(removeAclsOp)+len(delAclsOp))
	ops = append(ops, removeAclsOp...)
	ops = append(ops, delAclsOp...)

	if err = c.Transact("acls-del", ops); err != nil {
		return fmt.Errorf("del acls from %s: %v", pgName, err)
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

// ListAcls list acls which match the given externalIDs,
// result should include all to-lport and from-lport acls when direction is empty,
// result should include all acls when externalIDs is empty,
// result should include all acls which externalIDs[key] is not empty when externalIDs[key] is ""
// TODO: maybe add other filter conditions(priority or match)
func (c OvnClient) ListAcls(direction string, externalIDs map[string]string) ([]ovnnb.ACL, error) {
	aclList := make([]ovnnb.ACL, 0)

	if err := c.WhereCache(aclFilter(direction, externalIDs)).List(context.TODO(), &aclList); err != nil {
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
		return nil, fmt.Errorf("get acl 'direction %s' 'priority %s' 'match %s': %v", direction, priority, match, err)
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

// createSgRuleACL create security group rule acl
func (c OvnClient) newSgRuleACL(sgName string, direction string, rule *kubeovnv1.SgRule) (*ovnnb.ACL, error) {
	ipSuffix := "ip4"
	if rule.IPVersion == "ipv6" {
		ipSuffix = "ip6"
	}

	pgName := GetSgPortGroupName(sgName)

	// ingress rule
	srcOrDst, portDriection := "src", "outport"
	if direction == ovnnb.ACLDirectionFromLport { // egress rule
		srcOrDst = "dst"
		portDriection = "inport"
	}

	ipKey := ipSuffix + "." + srcOrDst

	/* match all traffic to or from pgName */
	AllIpMatch := NewAndAclMatchRule(
		NewAclRuleKv(portDriection, "==", "@"+pgName, ""),
		NewAclRuleKv(ipSuffix, "", "", ""),
	)

	/* allow allowed ip traffic */
	// type address
	allowedIpMatch := NewAndAclMatchRule(
		AllIpMatch,
		NewAclRuleKv(ipKey, "==", rule.RemoteAddress, ""),
	)

	// type securityGroup
	if rule.RemoteType == kubeovnv1.SgRemoteTypeSg {
		allowedIpMatch = NewAndAclMatchRule(
			AllIpMatch,
			NewAclRuleKv(ipKey, "==", "$"+rule.RemoteSecurityGroup, ""),
		)
	}

	/* allow layer 4 traffic */
	// allow all layer 4 traffic
	match := allowedIpMatch

	switch rule.Protocol {
	case kubeovnv1.ProtocolICMP:
		match = NewAndAclMatchRule(
			allowedIpMatch,
			NewAclRuleKv("icmp4", "", "", ""),
		)
		if ipSuffix == "ip6" {
			match = NewAndAclMatchRule(
				allowedIpMatch,
				NewAclRuleKv("icmp6", "", "", ""),
			)
		}
	case kubeovnv1.ProtocolTCP, kubeovnv1.ProtocolUDP:
		match = NewAndAclMatchRule(
			allowedIpMatch,
			NewAclRuleKv(string(rule.Protocol)+".dst", "<=", strconv.Itoa(rule.PortRangeMin), strconv.Itoa(rule.PortRangeMax)),
		)
	}

	action := ovnnb.ACLActionDrop
	if rule.Policy == kubeovnv1.PolicyAllow {
		action = ovnnb.ACLActionAllowRelated
	}

	highestPriority, _ := strconv.Atoi(util.SecurityGroupHighestPriority)

	acl, err := c.newAcl(pgName, direction, strconv.Itoa(highestPriority-rule.Priority), match.String(), action)
	if err != nil {
		return nil, fmt.Errorf("new security group acl for port group %s: %v", pgName, err)
	}

	return acl, nil
}

func newNetworkPolicyAclMatch(pgName, asAllowName, asExceptName, protocol, direction string, npp []netv1.NetworkPolicyPort) []string {
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

// aclFilter filter acls which match the given externalIDs,
// result should include all to-lport and from-lport acls when direction is empty,
// result should include all acls when externalIDs is empty,
// result should include all acls which externalIDs[key] is not empty when externalIDs[key] is ""
// TODO: maybe add other filter conditions(priority or match)
func aclFilter(direction string, externalIDs map[string]string) func(acl *ovnnb.ACL) bool {
	return func(acl *ovnnb.ACL) bool {
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
	}
}
