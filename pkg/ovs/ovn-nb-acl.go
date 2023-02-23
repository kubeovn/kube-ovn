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

// CreateIngressACL creates an ingress ACL
func (c *ovnClient) CreateIngressAcl(pgName, asIngressName, asExceptName, protocol string, npp []netv1.NetworkPolicyPort) error {
	acls := make([]*ovnnb.ACL, 0)

	/* default drop acl */
	AllIpMatch := NewAndAclMatch(
		NewAclMatch("outport", "==", "@"+pgName, ""),
		NewAclMatch("ip", "", "", ""),
	)
	options := func(acl *ovnnb.ACL) {
		acl.Name = &pgName
		acl.Log = true
		acl.Severity = &ovnnb.ACLSeverityWarning
	}

	defaultDropAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionToLport, util.IngressDefaultDrop, AllIpMatch.String(), ovnnb.ACLActionDrop, options)
	if err != nil {
		return fmt.Errorf("new default drop ingress acl for port group %s: %v", pgName, err)
	}

	acls = append(acls, defaultDropAcl)

	/* allow acl */
	matches := newNetworkPolicyAclMatch(pgName, asIngressName, asExceptName, protocol, ovnnb.ACLDirectionToLport, npp)
	for _, m := range matches {
		allowAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, m, ovnnb.ACLActionAllowRelated)
		if err != nil {
			return fmt.Errorf("new allow ingress acl for port group %s: %v", pgName, err)
		}

		acls = append(acls, allowAcl)
	}

	if err := c.CreateAcls(pgName, portGroupKey, acls...); err != nil {
		return fmt.Errorf("add ingress acls to port group %s: %v", pgName, err)
	}

	return nil
}

// CreateIngressACL creates an egress ACL
func (c *ovnClient) CreateEgressAcl(pgName, asEgressName, asExceptName, protocol string, npp []netv1.NetworkPolicyPort) error {
	acls := make([]*ovnnb.ACL, 0)

	/* default drop acl */
	AllIpMatch := NewAndAclMatch(
		NewAclMatch("inport", "==", "@"+pgName, ""),
		NewAclMatch("ip", "", "", ""),
	)
	options := func(acl *ovnnb.ACL) {
		acl.Name = &pgName
		acl.Log = true
		acl.Severity = &ovnnb.ACLSeverityWarning
	}

	defaultDropAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionFromLport, util.EgressDefaultDrop, AllIpMatch.String(), ovnnb.ACLActionDrop, options)
	if err != nil {
		return fmt.Errorf("new default drop egress acl for port group %s: %v", pgName, err)
	}

	acls = append(acls, defaultDropAcl)

	/* allow acl */
	matches := newNetworkPolicyAclMatch(pgName, asEgressName, asExceptName, protocol, ovnnb.ACLDirectionFromLport, npp)
	for _, m := range matches {
		allowAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, m, ovnnb.ACLActionAllowRelated)
		if err != nil {
			return fmt.Errorf("new allow egress acl for port group %s: %v", pgName, err)
		}

		acls = append(acls, allowAcl)
	}

	if err := c.CreateAcls(pgName, portGroupKey, acls...); err != nil {
		return fmt.Errorf("add egress acls to port group %s: %v", pgName, err)
	}

	return nil
}

// CreateGatewayACL create allow acl for subnet gateway
func (c *ovnClient) CreateGatewayAcl(pgName, gateway string) error {
	acls := make([]*ovnnb.ACL, 0)

	for _, gw := range strings.Split(gateway, ",") {
		protocol := util.CheckProtocol(gw)
		ipSuffix := "ip4"
		if protocol == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
		}

		allowIngressAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, fmt.Sprintf("%s.src == %s", ipSuffix, gw), ovnnb.ACLActionAllowRelated)
		if err != nil {
			return fmt.Errorf("new allow ingress acl for port group %s: %v", pgName, err)
		}

		allowEgressAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, fmt.Sprintf("%s.dst == %s", ipSuffix, gw), ovnnb.ACLActionAllowRelated)
		if err != nil {
			return fmt.Errorf("new allow egress acl for port group %s: %v", pgName, err)
		}

		acls = append(acls, allowIngressAcl, allowEgressAcl)
	}

	if err := c.CreateAcls(pgName, portGroupKey, acls...); err != nil {
		return fmt.Errorf("add gateway acls to port group %s: %v", pgName, err)
	}

	return nil
}

// CreateGatewayACL create allow acl for node join ip
func (c *ovnClient) CreateNodeAcl(pgName, nodeIp string) error {
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
			return fmt.Errorf("new allow ingress acl for port group %s: %v", pgName, err)
		}

		allowEgressAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionFromLport, util.NodeAllowPriority, fmt.Sprintf("%s.dst == %s && %s.src == $%s", ipSuffix, ip, ipSuffix, pgAs), ovnnb.ACLActionAllowRelated)
		if err != nil {
			return fmt.Errorf("new allow egress acl for port group %s: %v", pgName, err)
		}

		acls = append(acls, allowIngressAcl, allowEgressAcl)
	}

	if err := c.CreateAcls(pgName, portGroupKey, acls...); err != nil {
		return fmt.Errorf("add node acls to port group %s: %v", pgName, err)
	}

	return nil
}

func (c *ovnClient) CreateSgDenyAllAcl(sgName string) error {
	pgName := GetSgPortGroupName(sgName)

	ingressAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionToLport, util.SecurityGroupDropPriority, fmt.Sprintf("outport == @%s && ip", pgName), ovnnb.ACLActionDrop)
	if err != nil {
		return fmt.Errorf("new deny all ingress acl for security group %s: %v", sgName, err)
	}

	egressAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionFromLport, util.SecurityGroupDropPriority, fmt.Sprintf("inport == @%s && ip", pgName), ovnnb.ACLActionDrop)
	if err != nil {
		return fmt.Errorf("new deny all egress acl for security group %s: %v", sgName, err)
	}

	if err := c.CreateAcls(pgName, portGroupKey, ingressAcl, egressAcl); err != nil {
		return fmt.Errorf("add deny all acl to port group %s: %v", pgName, err)
	}

	return nil
}

func (c *ovnClient) UpdateSgAcl(sg *kubeovnv1.SecurityGroup, direction string) error {
	pgName := GetSgPortGroupName(sg.Name)

	// clear acl
	if err := c.DeleteAcls(pgName, portGroupKey, direction); err != nil {
		return fmt.Errorf("delete direction '%s' acls from port group %s: %v", direction, pgName, err)
	}

	acls := make([]*ovnnb.ACL, 0, 2)

	// ingress rule
	srcOrDst, portDirection, sgRules := "src", "outport", sg.Spec.IngressRules
	if direction == ovnnb.ACLDirectionFromLport { // egress rule
		srcOrDst = "dst"
		portDirection = "inport"
		sgRules = sg.Spec.EgressRules
	}

	/* create port_group associated acl */
	if sg.Spec.AllowSameGroupTraffic {
		asName := GetSgV4AssociatedName(sg.Name)
		for _, ipSuffix := range []string{"ip4", "ip6"} {
			if ipSuffix == "ip6" {
				asName = GetSgV6AssociatedName(sg.Name)
			}

			match := NewAndAclMatch(
				NewAclMatch(portDirection, "==", "@"+pgName, ""),
				NewAclMatch(ipSuffix, "", "", ""),
				NewAclMatch(ipSuffix+"."+srcOrDst, "==", "$"+asName, ""),
			)
			acl, err := c.newAcl(pgName, direction, util.SecurityGroupAllowPriority, match.String(), ovnnb.ACLActionAllowRelated)
			if err != nil {
				return fmt.Errorf("new allow acl for security group %s: %v", sg.Name, err)
			}

			acls = append(acls, acl)
		}
	}

	/* create rule acl */
	for _, rule := range sgRules {
		acl, err := c.newSgRuleACL(sg.Name, direction, rule)
		if err != nil {
			return fmt.Errorf("new rule acl for security group %s: %v", sg.Name, err)
		}
		acls = append(acls, acl)
	}

	if err := c.CreateAcls(pgName, portGroupKey, acls...); err != nil {
		return fmt.Errorf("add acl to port group %s: %v", pgName, err)
	}

	return nil
}

func (c *ovnClient) UpdateLogicalSwitchAcl(lsName string, subnetAcls []kubeovnv1.Acl) error {
	if err := c.DeleteAcls(lsName, logicalSwitchKey, ""); err != nil {
		return fmt.Errorf("clear logical switch %s acls: %v", lsName, err)
	}

	if len(subnetAcls) == 0 {
		return nil
	}
	acls := make([]*ovnnb.ACL, 0, len(subnetAcls))

	/* recreate logical switch acl */
	for _, subnetAcl := range subnetAcls {
		acl, err := c.newAcl(lsName, subnetAcl.Direction, strconv.Itoa(subnetAcl.Priority), subnetAcl.Match, subnetAcl.Action)
		if err != nil {
			return fmt.Errorf("new acl for logical switch %s: %v", lsName, err)
		}
		acls = append(acls, acl)
	}

	if err := c.CreateAcls(lsName, logicalSwitchKey, acls...); err != nil {
		return fmt.Errorf("add acls to logical switch %s: %v", lsName, err)
	}

	return nil
}

// UpdateAcl update acl
func (c *ovnClient) UpdateAcl(acl *ovnnb.ACL, fields ...interface{}) error {
	if acl == nil {
		return fmt.Errorf("address_set is nil")
	}

	op, err := c.Where(acl).Update(acl, fields...)
	if err != nil {
		return fmt.Errorf("generate operations for updating acl with 'direction %s priority %d match %s': %v", acl.Direction, acl.Priority, acl.Match, err)
	}

	if err = c.Transact("acl-update", op); err != nil {
		return fmt.Errorf("update acl with 'direction %s priority %d match %s': %v", acl.Direction, acl.Priority, acl.Match, err)
	}

	return nil
}

// SetLogicalSwitchPrivate will drop all ingress traffic except allow subnets, same subnet and node subnet
func (c *ovnClient) SetLogicalSwitchPrivate(lsName, cidrBlock string, allowSubnets []string) error {
	// clear acls
	if err := c.DeleteAcls(lsName, logicalSwitchKey, ""); err != nil {
		return fmt.Errorf("clear logical switch %s acls: %v", lsName, err)
	}

	acls := make([]*ovnnb.ACL, 0)

	/* default drop acl */
	AllIpMatch := NewAclMatch("ip", "", "", "")

	options := func(acl *ovnnb.ACL) {
		acl.Name = &lsName
		acl.Log = true
		acl.Severity = &ovnnb.ACLSeverityWarning
	}

	defaultDropAcl, err := c.newAcl(lsName, ovnnb.ACLDirectionToLport, util.DefaultDropPriority, AllIpMatch.String(), ovnnb.ACLActionDrop, options)
	if err != nil {
		return fmt.Errorf("new default drop ingress acl for logical switch %s: %v", lsName, err)
	}

	acls = append(acls, defaultDropAcl)

	nodeSubnetAclFunc := func(protocol, ipSuffix string) error {
		for _, nodeCidr := range strings.Split(c.NodeSwitchCIDR, ",") {
			// skip different address family
			if protocol != util.CheckProtocol(nodeCidr) {
				continue
			}

			match := NewAndAclMatch(
				NewAclMatch(ipSuffix+".src", "==", nodeCidr, ""),
			)

			acl, err := c.newAcl(lsName, ovnnb.ACLDirectionToLport, util.NodeAllowPriority, match.String(), ovnnb.ACLActionAllowRelated)
			if err != nil {
				return fmt.Errorf("new node subnet ingress acl for logical switch %s: %v", lsName, err)
			}

			acls = append(acls, acl)
		}

		return nil
	}

	allowSubnetAclFunc := func(protocol, ipSuffix, cidr string) error {
		for _, allowSubnet := range allowSubnets {
			subnet := strings.TrimSpace(allowSubnet)
			// skip empty subnet
			if len(subnet) == 0 {
				continue
			}

			// skip different address family
			if util.CheckProtocol(subnet) != protocol {
				continue
			}

			match := NewOrAclMatch(
				NewAndAclMatch(
					NewAclMatch(ipSuffix+".src", "==", cidr, ""),
					NewAclMatch(ipSuffix+".dst", "==", subnet, ""),
				),
				NewAndAclMatch(
					NewAclMatch(ipSuffix+".src", "==", subnet, ""),
					NewAclMatch(ipSuffix+".dst", "==", cidr, ""),
				),
			)

			acl, err := c.newAcl(lsName, ovnnb.ACLDirectionToLport, util.SubnetAllowPriority, match.String(), ovnnb.ACLActionAllowRelated)
			if err != nil {
				return fmt.Errorf("new allow subnet ingress acl for logical switch %s: %v", lsName, err)
			}

			acls = append(acls, acl)
		}
		return nil
	}

	for _, cidr := range strings.Split(cidrBlock, ",") {
		protocol := util.CheckProtocol(cidr)

		ipSuffix := "ip4"
		if protocol == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
		}

		/* same subnet acl */
		sameSubnetMatch := NewAndAclMatch(
			NewAclMatch(ipSuffix+".src", "==", cidr, ""),
			NewAclMatch(ipSuffix+".dst", "==", cidr, ""),
		)

		sameSubnetAcl, err := c.newAcl(lsName, ovnnb.ACLDirectionToLport, util.SubnetAllowPriority, sameSubnetMatch.String(), ovnnb.ACLActionAllowRelated)
		if err != nil {
			return fmt.Errorf("new same subnet ingress acl for logical switch %s: %v", lsName, err)
		}

		acls = append(acls, sameSubnetAcl)

		// node subnet acl
		if err := nodeSubnetAclFunc(protocol, ipSuffix); err != nil {
			return err
		}

		// allow subnet acl
		if err := allowSubnetAclFunc(protocol, ipSuffix, cidr); err != nil {
			return err
		}
	}

	if err := c.CreateAcls(lsName, logicalSwitchKey, acls...); err != nil {
		return fmt.Errorf("add ingress acls to logical switch %s: %v", lsName, err)
	}

	return nil
}

func (c *ovnClient) SetAclLog(pgName string, logEnable, isIngress bool) error {
	direction := ovnnb.ACLDirectionToLport
	portDirection := "outport"
	if !isIngress {
		direction = ovnnb.ACLDirectionFromLport
		portDirection = "inport"
	}

	// match all traffic to or from pgName
	AllIpMatch := NewAndAclMatch(
		NewAclMatch(portDirection, "==", "@"+pgName, ""),
		NewAclMatch("ip", "", "", ""),
	)

	acl, err := c.GetAcl(pgName, direction, util.IngressDefaultDrop, AllIpMatch.String(), false)
	if err != nil {
		return err
	}

	acl.Log = logEnable

	err = c.UpdateAcl(acl, &acl.Log)
	if err != nil {
		return fmt.Errorf("update acl: %v", err)
	}

	return nil
}

// CreateAcls create several acl once
// parentType is 'ls' or 'pg'
func (c *ovnClient) CreateAcls(parentName, parentType string, acls ...*ovnnb.ACL) error {
	if parentType != portGroupKey && parentType != logicalSwitchKey {
		return fmt.Errorf("acl parent type must be '%s' or '%s'", portGroupKey, logicalSwitchKey)
	}

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

	var aclAddOp []ovsdb.Operation
	if parentType == portGroupKey { // acl attach to port group
		aclAddOp, err = c.portGroupUpdateAclOp(parentName, aclUUIDs, ovsdb.MutateOperationInsert)
		if err != nil {
			return fmt.Errorf("generate operations for adding acls to port group %s: %v", parentName, err)
		}
	} else { // acl attach to logical switch
		aclAddOp, err = c.logicalSwitchUpdateAclOp(parentName, aclUUIDs, ovsdb.MutateOperationInsert)
		if err != nil {
			return fmt.Errorf("generate operations for adding acls to logical switch %s: %v", parentName, err)
		}
	}

	ops := make([]ovsdb.Operation, 0, len(createAclsOp)+len(aclAddOp))
	ops = append(ops, createAclsOp...)
	ops = append(ops, aclAddOp...)

	if err = c.Transact("acls-add", ops); err != nil {
		return fmt.Errorf("add acls to type %s %s: %v", parentType, parentName, err)
	}

	return nil
}

func (c *ovnClient) CreateBareAcl(parentName, direction, priority, match, action string) error {
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

// DeleteAcls delete several acl once,
// delete to-lport and from-lport direction acl when direction is empty, otherwise one-way
// parentType is 'ls' or 'pg'
func (c *ovnClient) DeleteAcls(parentName, parentType string, direction string) error {
	externalIDs := map[string]string{aclParentKey: parentName}

	/* delete acls from port group or logical switch */
	acls, err := c.ListAcls(direction, externalIDs)
	if err != nil {
		return fmt.Errorf("list type %s %s acls: %v", parentType, parentName, err)
	}

	aclUUIDs := make([]string, 0, len(acls))
	for _, acl := range acls {
		aclUUIDs = append(aclUUIDs, acl.UUID)
	}

	var removeAclOp []ovsdb.Operation
	if parentType == portGroupKey { // remove acl from port group
		removeAclOp, err = c.portGroupUpdateAclOp(parentName, aclUUIDs, ovsdb.MutateOperationDelete)
		if err != nil {
			return fmt.Errorf("generate operations for deleting acls from port group %s: %v", parentName, err)
		}
	} else { // remove acl from logical switch
		removeAclOp, err = c.logicalSwitchUpdateAclOp(parentName, aclUUIDs, ovsdb.MutateOperationDelete)
		if err != nil {
			return fmt.Errorf("generate operations for deleting acls from logical switch %s: %v", parentName, err)
		}
	}

	// delete acls
	delAclsOp, err := c.WhereCache(aclFilter(direction, externalIDs)).Delete()
	if err != nil {
		return fmt.Errorf("generate operation for deleting acls: %v", err)
	}

	ops := make([]ovsdb.Operation, 0, len(removeAclOp)+len(delAclsOp))
	ops = append(ops, removeAclOp...)
	ops = append(ops, delAclsOp...)

	if err = c.Transact("acls-del", ops); err != nil {
		return fmt.Errorf("del acls from type %s %s: %v", parentType, parentName, err)
	}

	return nil
}

// GetAcl get acl by direction, priority and match,
// be consistent with ovn-nbctl which direction, priority and match determine one acl in port group or logical switch
func (c *ovnClient) GetAcl(parent, direction, priority, match string, ignoreNotFound bool) (*ovnnb.ACL, error) {
	// this is necessary because may exist same direction, priority and match acl in different port group or logical switch
	if len(parent) == 0 {
		return nil, fmt.Errorf("the parent name is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	intPriority, _ := strconv.Atoi(priority)

	aclList := make([]ovnnb.ACL, 0)
	if err := c.ovnNbClient.WhereCache(func(acl *ovnnb.ACL) bool {
		return len(acl.ExternalIDs) != 0 && acl.ExternalIDs[aclParentKey] == parent && acl.Direction == direction && acl.Priority == intPriority && acl.Match == match
	}).List(ctx, &aclList); err != nil {
		return nil, fmt.Errorf("get acl with 'parent %s direction %s priority %s match %s': %v", parent, direction, priority, match, err)
	}

	// not found
	if len(aclList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("not found acl with 'parent %s direction %s priority %s match %s'", parent, direction, priority, match)
	}

	if len(aclList) > 1 {
		return nil, fmt.Errorf("more than one acl with same 'parent %s direction %s priority %s match %s'", parent, direction, priority, match)
	}

	return &aclList[0], nil
}

// ListAcls list acls which match the given externalIDs,
// result should include all to-lport and from-lport acls when direction is empty,
// result should include all acls when externalIDs is empty,
// result should include all acls which externalIDs[key] is not empty when externalIDs[key] is ""
// TODO: maybe add other filter conditions(priority or match)
func (c *ovnClient) ListAcls(direction string, externalIDs map[string]string) ([]ovnnb.ACL, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	aclList := make([]ovnnb.ACL, 0)

	if err := c.WhereCache(aclFilter(direction, externalIDs)).List(ctx, &aclList); err != nil {
		return nil, fmt.Errorf("list acls: %v", err)
	}

	return aclList, nil
}

func (c *ovnClient) AclExists(parent, direction, priority, match string) (bool, error) {
	acl, err := c.GetAcl(parent, direction, priority, match, true)
	return acl != nil, err
}

// newAcl return acl with basic information
func (c *ovnClient) newAcl(parent, direction, priority, match, action string, options ...func(acl *ovnnb.ACL)) (*ovnnb.ACL, error) {
	if len(parent) == 0 {
		return nil, fmt.Errorf("the parent name is required")
	}

	if len(direction) == 0 || len(priority) == 0 || len(match) == 0 || len(action) == 0 {
		return nil, fmt.Errorf("acl 'direction %s' and 'priority %s' and 'match %s' and 'action %s' is required", direction, priority, match, action)
	}

	exists, err := c.AclExists(parent, direction, priority, match)
	if err != nil {
		return nil, fmt.Errorf("get parent %s acl: %v", parent, err)
	}

	// found, ignore
	if exists {
		return nil, nil
	}

	intPriority, _ := strconv.Atoi(priority)

	acl := &ovnnb.ACL{
		UUID:      ovsclient.NamedUUID(),
		Action:    action,
		Direction: direction,
		Match:     match,
		Priority:  intPriority,
		ExternalIDs: map[string]string{
			aclParentKey: parent,
		},
	}

	for _, option := range options {
		option(acl)
	}

	return acl, nil
}

// createSgRuleACL create security group rule acl
func (c *ovnClient) newSgRuleACL(sgName string, direction string, rule *kubeovnv1.SgRule) (*ovnnb.ACL, error) {
	ipSuffix := "ip4"
	if rule.IPVersion == "ipv6" {
		ipSuffix = "ip6"
	}

	pgName := GetSgPortGroupName(sgName)

	// ingress rule
	srcOrDst, portDirection := "src", "outport"
	if direction == ovnnb.ACLDirectionFromLport { // egress rule
		srcOrDst = "dst"
		portDirection = "inport"
	}

	ipKey := ipSuffix + "." + srcOrDst

	/* match all traffic to or from pgName */
	AllIpMatch := NewAndAclMatch(
		NewAclMatch(portDirection, "==", "@"+pgName, ""),
		NewAclMatch(ipSuffix, "", "", ""),
	)

	/* allow allowed ip traffic */
	// type address
	allowedIpMatch := NewAndAclMatch(
		AllIpMatch,
		NewAclMatch(ipKey, "==", rule.RemoteAddress, ""),
	)

	// type securityGroup
	if rule.RemoteType == kubeovnv1.SgRemoteTypeSg {
		allowedIpMatch = NewAndAclMatch(
			AllIpMatch,
			NewAclMatch(ipKey, "==", "$"+rule.RemoteSecurityGroup, ""),
		)
	}

	/* allow layer 4 traffic */
	// allow all layer 4 traffic
	match := allowedIpMatch

	switch rule.Protocol {
	case kubeovnv1.ProtocolICMP:
		match = NewAndAclMatch(
			allowedIpMatch,
			NewAclMatch("icmp4", "", "", ""),
		)
		if ipSuffix == "ip6" {
			match = NewAndAclMatch(
				allowedIpMatch,
				NewAclMatch("icmp6", "", "", ""),
			)
		}
	case kubeovnv1.ProtocolTCP, kubeovnv1.ProtocolUDP:
		match = NewAndAclMatch(
			allowedIpMatch,
			NewAclMatch(string(rule.Protocol)+".dst", "<=", strconv.Itoa(rule.PortRangeMin), strconv.Itoa(rule.PortRangeMax)),
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
	srcOrDst, portDirection := "src", "outport"
	if direction == ovnnb.ACLDirectionFromLport { // egress rule
		srcOrDst = "dst"
		portDirection = "inport"
	}

	ipKey := ipSuffix + "." + srcOrDst

	// match all traffic to or from pgName
	AllIpMatch := NewAndAclMatch(
		NewAclMatch(portDirection, "==", "@"+pgName, ""),
		NewAclMatch("ip", "", "", ""),
	)

	allowedIpMatch := NewAndAclMatch(
		AllIpMatch,
		NewAclMatch(ipKey, "==", "$"+asAllowName, ""),
		NewAclMatch(ipKey, "!=", "$"+asExceptName, ""),
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
			allLayer4Match := NewAndAclMatch(
				allowedIpMatch,
				NewAclMatch(protocol, "", "", ""),
			)

			matches = append(matches, allLayer4Match.String())
			continue
		}

		// allow one tcp or udp port traffic
		if port.EndPort == nil {
			tcpKey := protocol + ".dst"
			oneTcpMatch := NewAndAclMatch(
				allowedIpMatch,
				NewAclMatch(tcpKey, "==", fmt.Sprintf("%d", port.Port.IntVal), ""),
			)

			matches = append(matches, oneTcpMatch.String())
			continue
		}

		// allow several tcp or udp port traffic
		tcpKey := protocol + ".dst"
		severalTcpMatch := NewAndAclMatch(
			allowedIpMatch,
			NewAclMatch(tcpKey, "<=", fmt.Sprintf("%d", port.Port.IntVal), fmt.Sprintf("%d", *port.EndPort)),
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
