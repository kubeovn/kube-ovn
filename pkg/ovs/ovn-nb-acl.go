package ovs

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// UpdateIngressAclOps return operation that creates an ingress ACL
func (c *ovnClient) UpdateIngressAclOps(pgName, asIngressName, asExceptName, protocol string, npp []netv1.NetworkPolicyPort, logEnable bool, namedPortMap map[string]*util.NamedPortInfo) ([]ovsdb.Operation, error) {
	acls := make([]*ovnnb.ACL, 0)

	ipSuffix := "ip4"
	if protocol == kubeovnv1.ProtocolIPv6 {
		ipSuffix = "ip6"
	}

	/* default drop acl */
	allIpMatch := NewAndAclMatch(
		NewAclMatch("outport", "==", "@"+pgName, ""),
		NewAclMatch(ipSuffix, "", "", ""),
	)
	options := func(acl *ovnnb.ACL) {
		if logEnable {
			acl.Log = true
			acl.Severity = &ovnnb.ACLSeverityWarning
		}
	}

	defaultDropAcl, err := c.newAclWithoutCheck(pgName, ovnnb.ACLDirectionToLport, util.IngressDefaultDrop, allIpMatch.String(), ovnnb.ACLActionDrop, options)
	if err != nil {
		return nil, fmt.Errorf("new default drop ingress acl for port group %s: %v", pgName, err)
	}

	acls = append(acls, defaultDropAcl)

	/* allow acl */
	matches := newNetworkPolicyAclMatch(pgName, asIngressName, asExceptName, protocol, ovnnb.ACLDirectionToLport, npp, namedPortMap)
	for _, m := range matches {
		allowAcl, err := c.newAclWithoutCheck(pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, m, ovnnb.ACLActionAllowRelated)
		if err != nil {
			return nil, fmt.Errorf("new allow ingress acl for port group %s: %v", pgName, err)
		}

		acls = append(acls, allowAcl)
	}

	ops, err := c.CreateAclsOps(pgName, portGroupKey, acls...)
	if err != nil {
		return nil, err
	}

	return ops, nil
}

// UpdateEgressAclOps return operation that creates an egress ACL
func (c *ovnClient) UpdateEgressAclOps(pgName, asEgressName, asExceptName, protocol string, npp []netv1.NetworkPolicyPort, logEnable bool, namedPortMap map[string]*util.NamedPortInfo) ([]ovsdb.Operation, error) {
	acls := make([]*ovnnb.ACL, 0)

	ipSuffix := "ip4"
	if protocol == kubeovnv1.ProtocolIPv6 {
		ipSuffix = "ip6"
	}

	/* default drop acl */
	allIpMatch := NewAndAclMatch(
		NewAclMatch("inport", "==", "@"+pgName, ""),
		NewAclMatch(ipSuffix, "", "", ""),
	)
	options := func(acl *ovnnb.ACL) {
		if logEnable {
			acl.Log = true
			acl.Severity = &ovnnb.ACLSeverityWarning
		}

		if acl.Options == nil {
			acl.Options = make(map[string]string)
		}
		acl.Options["apply-after-lb"] = "true"
	}

	defaultDropAcl, err := c.newAclWithoutCheck(pgName, ovnnb.ACLDirectionFromLport, util.EgressDefaultDrop, allIpMatch.String(), ovnnb.ACLActionDrop, options)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("new default drop egress acl for port group %s: %v", pgName, err)
	}

	acls = append(acls, defaultDropAcl)

	/* allow acl */
	matches := newNetworkPolicyAclMatch(pgName, asEgressName, asExceptName, protocol, ovnnb.ACLDirectionFromLport, npp, namedPortMap)
	for _, m := range matches {
		allowAcl, err := c.newAclWithoutCheck(pgName, ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, m, ovnnb.ACLActionAllowRelated, func(acl *ovnnb.ACL) {
			if acl.Options == nil {
				acl.Options = make(map[string]string)
			}
			acl.Options["apply-after-lb"] = "true"
		})
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("new allow egress acl for port group %s: %v", pgName, err)
		}

		acls = append(acls, allowAcl)
	}

	ops, err := c.CreateAclsOps(pgName, portGroupKey, acls...)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	return ops, nil
}

// CreateGatewayACL create allow acl for subnet gateway
func (c *ovnClient) CreateGatewayAcl(lsName, pgName, gateway string) error {
	acls := make([]*ovnnb.ACL, 0)

	var parentName, parentType string
	if len(pgName) != 0 {
		parentName, parentType = pgName, portGroupKey
	} else if len(lsName) != 0 {
		parentName, parentType = lsName, logicalSwitchKey
	} else {
		return fmt.Errorf("one of port group name and logical switch name must be specified")
	}

	for _, gw := range strings.Split(gateway, ",") {
		protocol := util.CheckProtocol(gw)
		ipSuffix := "ip4"
		if protocol == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
		}

		allowIngressAcl, err := c.newAcl(parentName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, fmt.Sprintf("%s.src == %s", ipSuffix, gw), ovnnb.ACLActionAllowStateless)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("new allow ingress acl for %s: %v", parentName, err)
		}

		options := func(acl *ovnnb.ACL) {
			if acl.Options == nil {
				acl.Options = make(map[string]string)
			}
			acl.Options["apply-after-lb"] = "true"
		}

		allowEgressAcl, err := c.newAcl(parentName, ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, fmt.Sprintf("%s.dst == %s", ipSuffix, gw), ovnnb.ACLActionAllowStateless, options)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("new allow egress acl for %s: %v", parentName, err)
		}

		acls = append(acls, allowIngressAcl, allowEgressAcl)

		if ipSuffix == "ip6" {
			ndAcl, err := c.newAcl(parentName, ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, "nd || nd_ra || nd_rs", ovnnb.ACLActionAllowStateless, options)
			if err != nil {
				klog.Error(err)
				return fmt.Errorf("new nd acl for %s: %v", parentName, err)
			}

			acls = append(acls, ndAcl)
		}

	}

	if err := c.CreateAcls(parentName, parentType, acls...); err != nil {
		return fmt.Errorf("add gateway acls to %s: %v", pgName, err)
	}

	return nil
}

// CreateNodeAcl create allow acl for node join ip
func (c *ovnClient) CreateNodeAcl(pgName, nodeIpStr, joinIpStr string) error {
	acls := make([]*ovnnb.ACL, 0)
	nodeIPs := strings.Split(nodeIpStr, ",")
	for _, nodeIP := range nodeIPs {
		protocol := util.CheckProtocol(nodeIP)
		ipSuffix := "ip4"
		if protocol == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
		}
		pgAs := fmt.Sprintf("%s_%s", pgName, ipSuffix)

		allowIngressAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionToLport, util.NodeAllowPriority, fmt.Sprintf("%s.src == %s && %s.dst == $%s", ipSuffix, nodeIP, ipSuffix, pgAs), ovnnb.ACLActionAllowStateless)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("new allow ingress acl for port group %s: %v", pgName, err)
		}

		options := func(acl *ovnnb.ACL) {
			if acl.Options == nil {
				acl.Options = make(map[string]string)
			}
			acl.Options["apply-after-lb"] = "true"
		}

		allowEgressAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionFromLport, util.NodeAllowPriority, fmt.Sprintf("%s.dst == %s && %s.src == $%s", ipSuffix, nodeIP, ipSuffix, pgAs), ovnnb.ACLActionAllowStateless, options)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("new allow egress acl for port group %s: %v", pgName, err)
		}

		acls = append(acls, allowIngressAcl, allowEgressAcl)
	}

	for _, joinIp := range strings.Split(joinIpStr, ",") {
		if util.ContainsString(nodeIPs, joinIp) {
			continue
		}

		protocol := util.CheckProtocol(joinIp)
		ipSuffix := "ip4"
		if protocol == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
		}

		pgAs := fmt.Sprintf("%s_%s", pgName, ipSuffix)

		if err := c.DeleteAcl(pgName, portGroupKey, ovnnb.ACLDirectionToLport, util.NodeAllowPriority, fmt.Sprintf("%s.src == %s && %s.dst == $%s", ipSuffix, joinIp, ipSuffix, pgAs)); err != nil {
			klog.Errorf("delete ingress acl from port group %s: %v", pgName, err)
			return err
		}

		if err := c.DeleteAcl(pgName, portGroupKey, ovnnb.ACLDirectionFromLport, util.NodeAllowPriority, fmt.Sprintf("%s.dst == %s && %s.src == $%s", ipSuffix, joinIp, ipSuffix, pgAs)); err != nil {
			klog.Errorf("delete egress acl from port group %s: %v", pgName, err)
			return err
		}
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
		klog.Error(err)
		return fmt.Errorf("new deny all ingress acl for security group %s: %v", sgName, err)
	}

	egressAcl, err := c.newAcl(pgName, ovnnb.ACLDirectionFromLport, util.SecurityGroupDropPriority, fmt.Sprintf("inport == @%s && ip", pgName), ovnnb.ACLActionDrop)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("new deny all egress acl for security group %s: %v", sgName, err)
	}

	if err := c.CreateAcls(pgName, portGroupKey, ingressAcl, egressAcl); err != nil {
		return fmt.Errorf("add deny all acl to port group %s: %v", pgName, err)
	}

	return nil
}

func (c *ovnClient) CreateSgBaseACL(sgName string, direction string) error {
	pgName := GetSgPortGroupName(sgName)

	// ingress rule
	portDirection := "outport"
	dhcpv4UdpSrc, dhcpv4UdpDst := "67", "68"
	dhcpv6UdpSrc, dhcpv6UdpDst := "547", "546"
	if direction == ovnnb.ACLDirectionFromLport { // egress rule
		portDirection = "inport"
		dhcpv4UdpSrc, dhcpv4UdpDst = dhcpv4UdpDst, dhcpv4UdpSrc
		dhcpv6UdpSrc, dhcpv6UdpDst = dhcpv6UdpDst, dhcpv6UdpSrc
	}

	acls := make([]*ovnnb.ACL, 0)

	newAcl := func(match string) {
		acl, err := c.newAcl(pgName, ovnnb.ACLDirectionToLport, util.SecurityGroupBasePriority, match, ovnnb.ACLActionAllowRelated)
		if err != nil {
			klog.Error(err)
			klog.Errorf("new base ingress acl for security group %s: %v", sgName, err)
			return
		}
		acls = append(acls, acl)
	}

	// allow arp
	AllArpMatch := NewAndAclMatch(
		NewAclMatch(portDirection, "==", "@"+pgName, ""),
		NewAclMatch("arp", "", "", ""),
	)
	newAcl(AllArpMatch.String())

	// icmpv6
	icmpv6Match := NewAndAclMatch(
		NewAclMatch(portDirection, "==", "@"+pgName, ""),
		NewAclMatch("icmp6.type", "==", "{130, 134, 135, 136}", ""),
		NewAclMatch("icmp6.code", "==", "0", ""),
		NewAclMatch("ip.ttl", "==", "255", ""),
	)
	newAcl(icmpv6Match.String())

	// dhcpv4 offer
	dhcpv4Match := NewAndAclMatch(
		NewAclMatch(portDirection, "==", "@"+pgName, ""),
		NewAclMatch("udp.src", "==", dhcpv4UdpSrc, ""),
		NewAclMatch("udp.dst", "==", dhcpv4UdpDst, ""),
		NewAclMatch("ip4", "", "", ""),
	)
	newAcl(dhcpv4Match.String())

	// dhcpv6 offer
	dhcpv6Match := NewAndAclMatch(
		NewAclMatch(portDirection, "==", "@"+pgName, ""),
		NewAclMatch("udp.src", "==", dhcpv6UdpSrc, ""),
		NewAclMatch("udp.dst", "==", dhcpv6UdpDst, ""),
		NewAclMatch("ip6", "", "", ""),
	)

	newAcl(dhcpv6Match.String())

	if err := c.CreateAcls(pgName, portGroupKey, acls...); err != nil {
		return fmt.Errorf("add ingress acls to port group %s: %v", pgName, err)
	}
	return nil
}

func (c *ovnClient) UpdateSgAcl(sg *kubeovnv1.SecurityGroup, direction string) error {
	pgName := GetSgPortGroupName(sg.Name)

	// clear acl
	if err := c.DeleteAcls(pgName, portGroupKey, direction, nil); err != nil {
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
				klog.Error(err)
				return fmt.Errorf("new allow acl for security group %s: %v", sg.Name, err)
			}

			acls = append(acls, acl)
		}
	}

	/* create rule acl */
	for _, rule := range sgRules {
		acl, err := c.newSgRuleACL(sg.Name, direction, rule)
		if err != nil {
			klog.Error(err)
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
	if err := c.DeleteAcls(lsName, logicalSwitchKey, "", map[string]string{"subnet": lsName}); err != nil {
		return fmt.Errorf("delete subnet acls from %s: %v", lsName, err)
	}

	if len(subnetAcls) == 0 {
		return nil
	}
	acls := make([]*ovnnb.ACL, 0, len(subnetAcls))

	options := func(acl *ovnnb.ACL) {
		if acl.ExternalIDs == nil {
			acl.ExternalIDs = make(map[string]string)
		}
		acl.ExternalIDs["subnet"] = lsName
	}

	/* recreate logical switch acl */
	for _, subnetAcl := range subnetAcls {
		acl, err := c.newAcl(lsName, subnetAcl.Direction, strconv.Itoa(subnetAcl.Priority), subnetAcl.Match, subnetAcl.Action, options)
		if err != nil {
			klog.Error(err)
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
		klog.Error(err)
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
	if err := c.DeleteAcls(lsName, logicalSwitchKey, "", nil); err != nil {
		return fmt.Errorf("clear logical switch %s acls: %v", lsName, err)
	}

	acls := make([]*ovnnb.ACL, 0)

	/* default drop acl */
	allIpMatch := NewAclMatch("ip", "", "", "")

	options := func(acl *ovnnb.ACL) {
		acl.Name = &lsName
		acl.Log = true
		acl.Severity = &ovnnb.ACLSeverityWarning
	}

	defaultDropAcl, err := c.newAcl(lsName, ovnnb.ACLDirectionToLport, util.DefaultDropPriority, allIpMatch.String(), ovnnb.ACLActionDrop, options)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("new default drop ingress acl for logical switch %s: %v", lsName, err)
	}

	acls = append(acls, defaultDropAcl)

	nodeSubnetAclFunc := func(protocol, ipSuffix string) error {
		for _, nodeCidr := range strings.Split(c.NodeSwitchCIDR, ",") {
			// skip different address family
			if protocol != util.CheckProtocol(nodeCidr) {
				continue
			}

			match := NewAclMatch(ipSuffix+".src", "==", nodeCidr, "")

			acl, err := c.newAcl(lsName, ovnnb.ACLDirectionToLport, util.NodeAllowPriority, match.String(), ovnnb.ACLActionAllowRelated)
			if err != nil {
				klog.Error(err)
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
				klog.Error(err)
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
			klog.Error(err)
			return fmt.Errorf("new same subnet ingress acl for logical switch %s: %v", lsName, err)
		}

		acls = append(acls, sameSubnetAcl)

		// node subnet acl
		if err := nodeSubnetAclFunc(protocol, ipSuffix); err != nil {
			klog.Error(err)
			return err
		}

		// allow subnet acl
		if err := allowSubnetAclFunc(protocol, ipSuffix, cidr); err != nil {
			klog.Error(err)
			return err
		}
	}

	if err := c.CreateAcls(lsName, logicalSwitchKey, acls...); err != nil {
		klog.Error(err)
		return fmt.Errorf("add ingress acls to logical switch %s: %v", lsName, err)
	}

	return nil
}

func (c *ovnClient) SetAclLog(pgName, protocol string, logEnable, isIngress bool) error {
	direction := ovnnb.ACLDirectionToLport
	portDirection := "outport"
	if !isIngress {
		direction = ovnnb.ACLDirectionFromLport
		portDirection = "inport"
	}

	ipSuffix := "ip4"
	if protocol == kubeovnv1.ProtocolIPv6 {
		ipSuffix = "ip6"
	}

	// match all traffic to or from pgName
	allIpMatch := NewAndAclMatch(
		NewAclMatch(portDirection, "==", "@"+pgName, ""),
		NewAclMatch(ipSuffix, "", "", ""),
	)

	acl, err := c.GetAcl(pgName, direction, util.IngressDefaultDrop, allIpMatch.String(), true)
	if err != nil {
		klog.Error(err)
		return err
	}

	if acl == nil {
		return nil //skip if acl not found
	}

	acl.Log = logEnable

	err = c.UpdateAcl(acl, &acl.Log)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("update acl: %v", err)
	}

	return nil
}

// CreateAcls create several acl once
// parentType is 'ls' or 'pg'
func (c *ovnClient) CreateAcls(parentName, parentType string, acls ...*ovnnb.ACL) error {
	ops, err := c.CreateAclsOps(parentName, parentType, acls...)
	if err != nil {
		klog.Error(err)
		return err
	}

	if err = c.Transact("acls-add", ops); err != nil {
		return fmt.Errorf("add acls to type %s %s: %v", parentType, parentName, err)
	}

	return nil
}

func (c *ovnClient) CreateBareAcl(parentName, direction, priority, match, action string) error {
	acl, err := c.newAcl(parentName, direction, priority, match, action)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("new acl direction %s priority %s match %s action %s: %v", direction, priority, match, action, err)
	}

	op, err := c.ovnNbClient.Create(acl)
	if err != nil {
		klog.Error(err)
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
func (c *ovnClient) DeleteAcls(parentName, parentType string, direction string, externalIDs map[string]string) error {
	ops, err := c.DeleteAclsOps(parentName, parentType, direction, externalIDs)
	if err != nil {
		klog.Error(err)
		return err
	}

	if err = c.Transact("acls-del", ops); err != nil {
		return fmt.Errorf("del acls from type %s %s: %v", parentType, parentName, err)
	}

	return nil
}

func (c *ovnClient) DeleteAcl(parentName, parentType, direction, priority, match string) error {
	acl, err := c.GetAcl(parentName, direction, priority, match, true)
	if err != nil {
		klog.Error(err)
		return err
	}

	if acl == nil {
		return nil // skip if acl not exist
	}

	// the acls column has a strong reference to the ACL table, so there is no need to delete the ACL
	var removeAclOp []ovsdb.Operation
	if parentType == portGroupKey { // remove acl from port group
		removeAclOp, err = c.portGroupUpdateAclOp(parentName, []string{acl.UUID}, ovsdb.MutateOperationDelete)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("generate operations for deleting acl from port group %s: %v", parentName, err)
		}
	} else { // remove acl from logical switch
		removeAclOp, err = c.logicalSwitchUpdateAclOp(parentName, []string{acl.UUID}, ovsdb.MutateOperationDelete)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("generate operations for deleting acl from logical switch %s: %v", parentName, err)
		}
	}

	if err = c.Transact("acls-del", removeAclOp); err != nil {
		klog.Error(err)
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
		klog.Error(err)
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
		klog.Error(err)
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

// newAclWithoutCheck return acl with basic information without check acl exists,
// this would cause duplicated acl, so don't use this function to create acl normally,
// but maybe used for updating network policy acl
func (c *ovnClient) newAclWithoutCheck(parent, direction, priority, match, action string, options ...func(acl *ovnnb.ACL)) (*ovnnb.ACL, error) {
	if len(parent) == 0 {
		return nil, fmt.Errorf("the parent name is required")
	}

	if len(direction) == 0 || len(priority) == 0 || len(match) == 0 || len(action) == 0 {
		return nil, fmt.Errorf("acl 'direction %s' and 'priority %s' and 'match %s' and 'action %s' is required", direction, priority, match, action)
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
	allIpMatch := NewAndAclMatch(
		NewAclMatch(portDirection, "==", "@"+pgName, ""),
		NewAclMatch(ipSuffix, "", "", ""),
	)

	/* allow allowed ip traffic */
	// type address
	allowedIpMatch := NewAndAclMatch(
		allIpMatch,
		NewAclMatch(ipKey, "==", rule.RemoteAddress, ""),
	)

	// type securityGroup
	remotePgName := GetSgV4AssociatedName(rule.RemoteSecurityGroup)
	if rule.IPVersion == "ipv6" {
		remotePgName = GetSgV6AssociatedName(rule.RemoteSecurityGroup)
	}
	if rule.RemoteType == kubeovnv1.SgRemoteTypeSg {
		allowedIpMatch = NewAndAclMatch(
			allIpMatch,
			NewAclMatch(ipKey, "==", "$"+remotePgName, ""),
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
		klog.Error(err)
		return nil, fmt.Errorf("new security group acl for port group %s: %v", pgName, err)
	}

	return acl, nil
}

func newNetworkPolicyAclMatch(pgName, asAllowName, asExceptName, protocol, direction string, npp []netv1.NetworkPolicyPort, namedPortMap map[string]*util.NamedPortInfo) []string {
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
	allIpMatch := NewAndAclMatch(
		NewAclMatch(portDirection, "==", "@"+pgName, ""),
		NewAclMatch("ip", "", "", ""),
	)

	allowedIpMatch := NewAndAclMatch(
		allIpMatch,
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

			var portId int32 = 0

			if port.Port.Type == intstr.Int {
				portId = port.Port.IntVal
			} else {
				if namedPortMap != nil {
					_, ok := namedPortMap[port.Port.StrVal]
					if !ok {
						// for cyclonus network policy test case 'should allow ingress access on one named port'
						// this case expect all-deny if no named port defined
						klog.Errorf("no named port with name %s found ", port.Port.StrVal)
					} else {
						portId = namedPortMap[port.Port.StrVal].PortId
					}
				}
			}

			oneTcpMatch := NewAndAclMatch(
				allowedIpMatch,
				NewAclMatch(tcpKey, "==", fmt.Sprintf("%d", portId), ""),
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

// CreateAcls return operations which create several acl once
// parentType is 'ls' or 'pg'
func (c *ovnClient) CreateAclsOps(parentName, parentType string, acls ...*ovnnb.ACL) ([]ovsdb.Operation, error) {
	if parentType != portGroupKey && parentType != logicalSwitchKey {
		return nil, fmt.Errorf("acl parent type must be '%s' or '%s'", portGroupKey, logicalSwitchKey)
	}

	if len(acls) == 0 {
		return nil, nil
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
		klog.Error(err)
		return nil, fmt.Errorf("generate operations for creating acls: %v", err)
	}

	var aclAddOp []ovsdb.Operation
	if parentType == portGroupKey { // acl attach to port group
		aclAddOp, err = c.portGroupUpdateAclOp(parentName, aclUUIDs, ovsdb.MutateOperationInsert)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("generate operations for adding acls to port group %s: %v", parentName, err)
		}
	} else { // acl attach to logical switch
		aclAddOp, err = c.logicalSwitchUpdateAclOp(parentName, aclUUIDs, ovsdb.MutateOperationInsert)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("generate operations for adding acls to logical switch %s: %v", parentName, err)
		}
	}

	ops := make([]ovsdb.Operation, 0, len(createAclsOp)+len(aclAddOp))
	ops = append(ops, createAclsOp...)
	ops = append(ops, aclAddOp...)

	return ops, nil
}

// DeleteAcls return operation which delete several acl once,
// delete to-lport and from-lport direction acl when direction is empty, otherwise one-way
// parentType is 'ls' or 'pg'
func (c *ovnClient) DeleteAclsOps(parentName, parentType string, direction string, externalIDs map[string]string) ([]ovsdb.Operation, error) {
	if externalIDs == nil {
		externalIDs = make(map[string]string)
	}

	externalIDs[aclParentKey] = parentName

	/* delete acls from port group or logical switch */
	acls, err := c.ListAcls(direction, externalIDs)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("list type %s %s acls: %v", parentType, parentName, err)
	}

	aclUUIDs := make([]string, 0, len(acls))
	for _, acl := range acls {
		aclUUIDs = append(aclUUIDs, acl.UUID)
	}

	// the acls column has a strong reference to the ACL table, so there is no need to delete the ACL
	var removeAclOp []ovsdb.Operation
	if parentType == portGroupKey { // remove acl from port group
		removeAclOp, err = c.portGroupUpdateAclOp(parentName, aclUUIDs, ovsdb.MutateOperationDelete)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("generate operations for deleting acls from port group %s: %v", parentName, err)
		}
	} else { // remove acl from logical switch
		removeAclOp, err = c.logicalSwitchUpdateAclOp(parentName, aclUUIDs, ovsdb.MutateOperationDelete)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("generate operations for deleting acls from logical switch %s: %v", parentName, err)
		}
	}

	return removeAclOp, nil
}
