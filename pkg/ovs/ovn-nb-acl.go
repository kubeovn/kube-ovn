package ovs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"k8s.io/utils/set"

	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type ACLErrorType int

const (
	ACLErrorNotFound ACLErrorType = iota
	ACLErrorDuplicated
	ACLErrorDatabase
)

type ACLError struct {
	Type ACLErrorType
	Msg  string
}

func (e *ACLError) Error() string {
	return e.Msg
}

func NewACLError(errType ACLErrorType, msg string) *ACLError {
	return &ACLError{Type: errType, Msg: msg}
}

func setACLName(acl *ovnnb.ACL, name string) {
	if len(name) > 63 {
		// ACL name length limit is 63
		name = name[:60] + "..."
	}
	acl.Name = ptr.To(name)
}

// UpdateIngressACLOps return operation that creates an ingress ACL
func (c *OVNNbClient) UpdateIngressACLOps(netpol, pgName, asIngressName, asExceptName, protocol, aclName string, npp []netv1.NetworkPolicyPort, logEnable bool, logACLActions []ovnnb.ACLAction, namedPortMap map[string]*util.NamedPortInfo) ([]ovsdb.Operation, error) {
	acls := make([]*ovnnb.ACL, 0)

	if strings.HasSuffix(asIngressName, ".0") || strings.HasSuffix(asIngressName, ".all") {
		// create the default drop rule for only once
		// both IPv4 and IPv6 traffic should be forbade in dual-stack situation
		allIPMatch := NewAndACLMatch(
			NewACLMatch("outport", "==", "@"+pgName, ""),
			NewACLMatch("ip", "", "", ""),
		)
		options := func(acl *ovnnb.ACL) {
			setACLName(acl, netpol)
			if logEnable {
				acl.Log = true
				acl.Severity = ptr.To(ovnnb.ACLSeverityWarning)
			}
		}

		defaultDropACL, err := c.newACLWithoutCheck(pgName, ovnnb.ACLDirectionToLport, util.IngressDefaultDrop, allIPMatch.String(), ovnnb.ACLActionDrop, util.NetpolACLTier, options)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("new default drop ingress acl for port group %s: %w", pgName, err)
		}

		acls = append(acls, defaultDropACL)
	}

	/* allow acl */
	matches := newNetworkPolicyACLMatch(pgName, asIngressName, asExceptName, protocol, ovnnb.ACLDirectionToLport, npp, namedPortMap)
	for _, m := range matches {
		options := func(acl *ovnnb.ACL) {
			setACLName(acl, aclName)
			if logEnable && slices.Contains(logACLActions, ovnnb.ACLActionAllow) {
				acl.Log = true
			}
		}

		allowACL, err := c.newACLWithoutCheck(pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, m, ovnnb.ACLActionAllowRelated, util.NetpolACLTier, options)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("new allow ingress acl for port group %s: %w", pgName, err)
		}

		acls = append(acls, allowACL)
	}

	ops, err := c.CreateAclsOps(pgName, portGroupKey, acls...)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to create ingress acl for port group %s: %w", pgName, err)
	}

	return ops, nil
}

// UpdateEgressACLOps return operation that creates an egress ACL
func (c *OVNNbClient) UpdateEgressACLOps(netpol, pgName, asEgressName, asExceptName, protocol, aclName string, npp []netv1.NetworkPolicyPort, logEnable bool, logACLActions []ovnnb.ACLAction, namedPortMap map[string]*util.NamedPortInfo) ([]ovsdb.Operation, error) {
	acls := make([]*ovnnb.ACL, 0)

	if strings.HasSuffix(asEgressName, ".0") || strings.HasSuffix(asEgressName, ".all") {
		// create the default drop rule for only once
		// both IPv4 and IPv6 traffic should be forbade in dual-stack situation
		allIPMatch := NewAndACLMatch(
			NewACLMatch("inport", "==", "@"+pgName, ""),
			NewACLMatch("ip", "", "", ""),
		)
		options := func(acl *ovnnb.ACL) {
			setACLName(acl, netpol)
			if logEnable {
				acl.Log = true
				acl.Severity = ptr.To(ovnnb.ACLSeverityWarning)
			}

			if acl.Options == nil {
				acl.Options = make(map[string]string)
			}
			acl.Options["apply-after-lb"] = "true"
		}

		defaultDropACL, err := c.newACLWithoutCheck(pgName, ovnnb.ACLDirectionFromLport, util.EgressDefaultDrop, allIPMatch.String(), ovnnb.ACLActionDrop, util.NetpolACLTier, options)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("new default drop egress acl for port group %s: %w", pgName, err)
		}

		acls = append(acls, defaultDropACL)
	}

	/* allow acl */
	matches := newNetworkPolicyACLMatch(pgName, asEgressName, asExceptName, protocol, ovnnb.ACLDirectionFromLport, npp, namedPortMap)
	for _, m := range matches {
		allowACL, err := c.newACLWithoutCheck(pgName, ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, m, ovnnb.ACLActionAllowRelated, util.NetpolACLTier, func(acl *ovnnb.ACL) {
			setACLName(acl, aclName)
			if acl.Options == nil {
				acl.Options = make(map[string]string)
			}
			acl.Options["apply-after-lb"] = "true"
			if logEnable && slices.Contains(logACLActions, ovnnb.ACLActionAllow) {
				acl.Log = true
			}
		})
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("new allow egress acl for port group %s: %w", pgName, err)
		}

		acls = append(acls, allowACL)
	}

	ops, err := c.CreateAclsOps(pgName, portGroupKey, acls...)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	return ops, nil
}

// CreateGatewayACL create allow acl for subnet gateway
func (c *OVNNbClient) CreateGatewayACL(lsName, pgName, gateway, u2oInterconnectionIP string) error {
	acls := make([]*ovnnb.ACL, 0)

	var parentName, parentType string
	switch {
	case len(pgName) != 0:
		parentName, parentType = pgName, portGroupKey
	case len(lsName) != 0:
		parentName, parentType = lsName, LogicalSwitchKey
	default:
		return errors.New("one of port group name and logical switch name must be specified")
	}

	gateways := set.New(strings.Split(gateway, ",")...)
	if u2oInterconnectionIP != "" {
		gateways = gateways.Insert(strings.Split(u2oInterconnectionIP, ",")...)
	}

	options := func(acl *ovnnb.ACL) {
		if acl.Options == nil {
			acl.Options = make(map[string]string)
		}
		acl.Options["apply-after-lb"] = "true"
	}
	v6Exists := false
	for gw := range gateways {
		protocol := util.CheckProtocol(gw)
		ipSuffix := "ip4"
		if protocol == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
			v6Exists = true
		}

		allowIngressACL, err := c.newACL(parentName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, fmt.Sprintf("%s.src == %s", ipSuffix, gw), ovnnb.ACLActionAllowStateless, util.NetpolACLTier)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("new allow ingress acl for %s: %w", parentName, err)
		}

		allowEgressACL, err := c.newACL(parentName, ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, fmt.Sprintf("%s.dst == %s", ipSuffix, gw), ovnnb.ACLActionAllowStateless, util.NetpolACLTier, options)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("new allow egress acl for %s: %w", parentName, err)
		}

		acls = append(acls, allowIngressACL, allowEgressACL)
	}

	if v6Exists {
		ndACL, err := c.newACL(parentName, ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, "nd || nd_ra || nd_rs", ovnnb.ACLActionAllowStateless, util.NetpolACLTier, options)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("new nd acl for %s: %w", parentName, err)
		}

		acls = append(acls, ndACL)
	}

	if err := c.CreateAcls(parentName, parentType, acls...); err != nil {
		klog.Error(err)
		return fmt.Errorf("add gateway acls to %s: %w", pgName, err)
	}

	return nil
}

// CreateNodeACL create allow acl for node join ip
func (c *OVNNbClient) CreateNodeACL(pgName, nodeIPStr, joinIPStr string) error {
	acls := make([]*ovnnb.ACL, 0)
	nodeIPs := strings.Split(nodeIPStr, ",")
	for _, nodeIP := range nodeIPs {
		protocol := util.CheckProtocol(nodeIP)
		ipSuffix := "ip4"
		if protocol == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
		}
		pgAs := fmt.Sprintf("%s_%s", pgName, ipSuffix)

		allowIngressACL, err := c.newACL(pgName, ovnnb.ACLDirectionToLport, util.NodeAllowPriority, fmt.Sprintf("%s.src == %s && %s.dst == $%s", ipSuffix, nodeIP, ipSuffix, pgAs), ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("new allow ingress acl for port group %s: %w", pgName, err)
		}

		options := func(acl *ovnnb.ACL) {
			if acl.Options == nil {
				acl.Options = make(map[string]string)
			}
			acl.Options["apply-after-lb"] = "true"
		}

		allowEgressACL, err := c.newACL(pgName, ovnnb.ACLDirectionFromLport, util.NodeAllowPriority, fmt.Sprintf("%s.dst == %s && %s.src == $%s", ipSuffix, nodeIP, ipSuffix, pgAs), ovnnb.ACLActionAllowRelated, util.NetpolACLTier, options)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("new allow egress acl for port group %s: %w", pgName, err)
		}

		acls = append(acls, allowIngressACL, allowEgressACL)
	}

	for joinIP := range strings.SplitSeq(joinIPStr, ",") {
		if slices.Contains(nodeIPs, joinIP) {
			continue
		}

		protocol := util.CheckProtocol(joinIP)
		ipSuffix := "ip4"
		if protocol == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
		}

		pgAs := fmt.Sprintf("%s_%s", pgName, ipSuffix)

		if err := c.DeleteACL(pgName, portGroupKey, ovnnb.ACLDirectionToLport, util.NodeAllowPriority, fmt.Sprintf("%s.src == %s && %s.dst == $%s", ipSuffix, joinIP, ipSuffix, pgAs)); err != nil {
			klog.Errorf("delete ingress acl from port group %s: %v", pgName, err)
			return err
		}

		if err := c.DeleteACL(pgName, portGroupKey, ovnnb.ACLDirectionFromLport, util.NodeAllowPriority, fmt.Sprintf("%s.dst == %s && %s.src == $%s", ipSuffix, joinIP, ipSuffix, pgAs)); err != nil {
			klog.Errorf("delete egress acl from port group %s: %v", pgName, err)
			return err
		}
	}

	if err := c.CreateAcls(pgName, portGroupKey, acls...); err != nil {
		return fmt.Errorf("add node acls to port group %s: %w", pgName, err)
	}

	return nil
}

func (c *OVNNbClient) CreateSgDenyAllACL(sgName string) error {
	pgName := GetSgPortGroupName(sgName)

	ingressACL, err := c.newACL(pgName, ovnnb.ACLDirectionToLport, util.SecurityGroupDropPriority, fmt.Sprintf("outport == @%s && ip", pgName), ovnnb.ACLActionDrop, util.NetpolACLTier)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("new deny all ingress acl for security group %s: %w", sgName, err)
	}

	egressACL, err := c.newACL(pgName, ovnnb.ACLDirectionFromLport, util.SecurityGroupDropPriority, fmt.Sprintf("inport == @%s && ip", pgName), ovnnb.ACLActionDrop, util.NetpolACLTier)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("new deny all egress acl for security group %s: %w", sgName, err)
	}

	err = c.CreateAcls(pgName, portGroupKey, ingressACL, egressACL)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("add deny all acl to port group %s: %w", pgName, err)
	}

	return nil
}

// CreateSgACL create allow acl for security group
func (c *OVNNbClient) CreateSgBaseACL(sgName, direction string) error {
	pgName := GetSgPortGroupName(sgName)

	// ingress rule
	portDirection := "outport"
	dhcpv4UdpSrc, dhcpv4UdpDst := "67", "68"
	dhcpv6UdpSrc, dhcpv6UdpDst := "547", "546"
	icmpv6Type := "{130, 134, 135, 136}"
	// 130 group membership query
	// 133 router solicitation
	// 134 router advertisement
	// 135 neighbor solicitation
	// 136 neighbor advertisement

	if direction == ovnnb.ACLDirectionFromLport { // egress rule
		portDirection = "inport"
		dhcpv4UdpSrc, dhcpv4UdpDst = dhcpv4UdpDst, dhcpv4UdpSrc
		dhcpv6UdpSrc, dhcpv6UdpDst = dhcpv6UdpDst, dhcpv6UdpSrc
		icmpv6Type = "{130, 133, 135, 136}"
	}

	acls := make([]*ovnnb.ACL, 0)

	newACL := func(match string) {
		acl, err := c.newACL(pgName, direction, util.SecurityGroupBasePriority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		if err != nil {
			klog.Error(err)
			klog.Errorf("new base ingress acl for security group %s: %v", sgName, err)
			return
		}
		acls = append(acls, acl)
	}

	// allow arp
	allArpMatch := NewAndACLMatch(
		NewACLMatch(portDirection, "==", "@"+pgName, ""),
		NewACLMatch("arp", "", "", ""),
	)
	newACL(allArpMatch.String())

	// icmpv6
	icmpv6Match := NewAndACLMatch(
		NewACLMatch(portDirection, "==", "@"+pgName, ""),
		NewACLMatch("icmp6.type", "==", icmpv6Type, ""),
		NewACLMatch("icmp6.code", "==", "0", ""),
		NewACLMatch("ip.ttl", "==", "255", ""),
	)
	newACL(icmpv6Match.String())

	// dhcpv4 offer
	dhcpv4Match := NewAndACLMatch(
		NewACLMatch(portDirection, "==", "@"+pgName, ""),
		NewACLMatch("udp.src", "==", dhcpv4UdpSrc, ""),
		NewACLMatch("udp.dst", "==", dhcpv4UdpDst, ""),
		NewACLMatch("ip4", "", "", ""),
	)
	newACL(dhcpv4Match.String())

	// dhcpv6 offer
	dhcpv6Match := NewAndACLMatch(
		NewACLMatch(portDirection, "==", "@"+pgName, ""),
		NewACLMatch("udp.src", "==", dhcpv6UdpSrc, ""),
		NewACLMatch("udp.dst", "==", dhcpv6UdpDst, ""),
		NewACLMatch("ip6", "", "", ""),
	)
	newACL(dhcpv6Match.String())

	// vrrp
	vrrpMatch := NewAndACLMatch(
		NewACLMatch(portDirection, "==", "@"+pgName, ""),
		NewACLMatch("ip.proto", "==", "112", ""),
	)
	newACL(vrrpMatch.String())

	if err := c.CreateAcls(pgName, portGroupKey, acls...); err != nil {
		klog.Error(err)
		return fmt.Errorf("add ingress acls to port group %s: %w", pgName, err)
	}
	return nil
}

func (c *OVNNbClient) UpdateSgACL(sg *kubeovnv1.SecurityGroup, direction string) error {
	pgName := GetSgPortGroupName(sg.Name)

	// clear acl
	if err := c.DeleteAcls(pgName, portGroupKey, direction, nil); err != nil {
		klog.Error(err)
		return fmt.Errorf("delete direction '%s' acls from port group %s: %w", direction, pgName, err)
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

			match := NewAndACLMatch(
				NewACLMatch(portDirection, "==", "@"+pgName, ""),
				NewACLMatch(ipSuffix, "", "", ""),
				NewACLMatch(ipSuffix+"."+srcOrDst, "==", "$"+asName, ""),
			)
			acl, err := c.newACL(pgName, direction, util.SecurityGroupAllowPriority, match.String(), ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
			if err != nil {
				klog.Error(err)
				return fmt.Errorf("new allow acl for security group %s: %w", sg.Name, err)
			}

			acls = append(acls, acl)
		}
	}

	/* create rule acl */
	for _, rule := range sgRules {
		acl, err := c.newSgRuleACL(sg.Name, direction, rule)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("new rule acl for security group %s: %w", sg.Name, err)
		}
		acls = append(acls, acl)
	}

	if err := c.CreateAcls(pgName, portGroupKey, acls...); err != nil {
		klog.Error(err)
		return fmt.Errorf("add acl to port group %s: %w", pgName, err)
	}

	return nil
}

func (c *OVNNbClient) UpdateLogicalSwitchACL(lsName, cidrBlock string, subnetAcls []kubeovnv1.ACL, allowEWTraffic bool) error {
	if len(subnetAcls) == 0 {
		if err := c.DeleteAcls(lsName, LogicalSwitchKey, "", map[string]string{"subnet": lsName}); err != nil {
			klog.Error(err)
			return fmt.Errorf("delete subnet acls from %s: %w", lsName, err)
		}
		return nil
	}

	acls := make([]*ovnnb.ACL, 0)

	options := func(acl *ovnnb.ACL) {
		if acl.ExternalIDs == nil {
			acl.ExternalIDs = make(map[string]string)
		}
		acl.ExternalIDs["subnet"] = lsName
	}

	if allowEWTraffic {
		for cidr := range strings.SplitSeq(cidrBlock, ",") {
			protocol := util.CheckProtocol(cidr)

			ipSuffix := "ip4"
			if protocol == kubeovnv1.ProtocolIPv6 {
				ipSuffix = "ip6"
			}

			/* same subnet acl */
			sameSubnetMatch := NewAndACLMatch(
				NewACLMatch(ipSuffix+".src", "==", cidr, ""),
				NewACLMatch(ipSuffix+".dst", "==", cidr, ""),
			)

			ingressSameSubnetACL, err := c.newACL(lsName, ovnnb.ACLDirectionToLport, util.AllowEWTrafficPriority, sameSubnetMatch.String(), ovnnb.ACLActionAllow, util.NetpolACLTier, options)
			if err != nil {
				klog.Error(err)
				return fmt.Errorf("new same subnet ingress acl for logical switch %s: %w", lsName, err)
			}
			acls = append(acls, ingressSameSubnetACL)

			egressSameSubnetACL, err := c.newACL(lsName, ovnnb.ACLDirectionFromLport, util.AllowEWTrafficPriority, sameSubnetMatch.String(), ovnnb.ACLActionAllow, util.NetpolACLTier, options)
			if err != nil {
				klog.Error(err)
				return fmt.Errorf("new same subnet egress acl for logical switch %s: %w", lsName, err)
			}
			acls = append(acls, egressSameSubnetACL)
		}
	}

	/* recreate logical switch acl */
	for _, subnetACL := range subnetAcls {
		acl, err := c.newACL(lsName, subnetACL.Direction, strconv.Itoa(subnetACL.Priority), subnetACL.Match, subnetACL.Action, util.NetpolACLTier, options)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("new acl for logical switch %s: %w", lsName, err)
		}
		acls = append(acls, acl)
	}

	delOps, err := c.DeleteAclsOps(lsName, LogicalSwitchKey, "", map[string]string{"subnet": lsName})
	if err != nil {
		klog.Error(err)
		return err
	}

	addOps, err := c.CreateAclsOps(lsName, LogicalSwitchKey, acls...)
	if err != nil {
		klog.Error(err)
		return err
	}

	if err := c.Transact("acls-update", append(delOps, addOps...)); err != nil {
		klog.Error(err)
		return fmt.Errorf("update acls for logical switch %s: %w", lsName, err)
	}

	return nil
}

// UpdateACL update acl
func (c *OVNNbClient) UpdateACL(acl *ovnnb.ACL, fields ...any) error {
	if acl == nil {
		return errors.New("address_set is nil")
	}

	op, err := c.Where(acl).Update(acl, fields...)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for updating acl with 'direction %s priority %d match %s': %w", acl.Direction, acl.Priority, acl.Match, err)
	}

	if err = c.Transact("acl-update", op); err != nil {
		klog.Error(err)
		return fmt.Errorf("update acl with 'direction %s priority %d match %s': %w", acl.Direction, acl.Priority, acl.Match, err)
	}

	return nil
}

// SetLogicalSwitchPrivate will drop all ingress traffic except allow subnets, same subnet and node subnet
func (c *OVNNbClient) SetLogicalSwitchPrivate(lsName, cidrBlock, nodeSwitchCIDR string, allowSubnets []string) error {
	// clear acls
	if err := c.DeleteAcls(lsName, LogicalSwitchKey, "", nil); err != nil {
		klog.Error(err)
		return fmt.Errorf("clear logical switch %s acls: %w", lsName, err)
	}

	acls := make([]*ovnnb.ACL, 0)

	/* default drop acl */
	allIPMatch := NewACLMatch("ip", "", "", "")

	options := func(acl *ovnnb.ACL) {
		setACLName(acl, lsName)
		acl.Log = true
		acl.Severity = ptr.To(ovnnb.ACLSeverityWarning)
	}

	defaultDropACL, err := c.newACL(lsName, ovnnb.ACLDirectionToLport, util.DefaultDropPriority, allIPMatch.String(), ovnnb.ACLActionDrop, util.NetpolACLTier, options)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("new default drop ingress acl for logical switch %s: %w", lsName, err)
	}

	acls = append(acls, defaultDropACL)

	nodeSubnetACLFunc := func(protocol, ipSuffix string) error {
		for nodeCidr := range strings.SplitSeq(nodeSwitchCIDR, ",") {
			// skip different address family
			if protocol != util.CheckProtocol(nodeCidr) {
				continue
			}

			match := NewACLMatch(ipSuffix+".src", "==", nodeCidr, "")

			acl, err := c.newACL(lsName, ovnnb.ACLDirectionToLport, util.NodeAllowPriority, match.String(), ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
			if err != nil {
				klog.Error(err)
				return fmt.Errorf("new node subnet ingress acl for logical switch %s: %w", lsName, err)
			}

			acls = append(acls, acl)
		}

		return nil
	}

	allowSubnetACLFunc := func(protocol, ipSuffix, cidr string) error {
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

			match := NewOrACLMatch(
				NewAndACLMatch(
					NewACLMatch(ipSuffix+".src", "==", cidr, ""),
					NewACLMatch(ipSuffix+".dst", "==", subnet, ""),
				),
				NewAndACLMatch(
					NewACLMatch(ipSuffix+".src", "==", subnet, ""),
					NewACLMatch(ipSuffix+".dst", "==", cidr, ""),
				),
			)

			acl, err := c.newACL(lsName, ovnnb.ACLDirectionToLport, util.SubnetCrossAllowPriority, match.String(), ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
			if err != nil {
				klog.Error(err)
				return fmt.Errorf("new allow subnet ingress acl for logical switch %s: %w", lsName, err)
			}

			acls = append(acls, acl)
		}
		return nil
	}

	for cidr := range strings.SplitSeq(cidrBlock, ",") {
		protocol := util.CheckProtocol(cidr)

		ipSuffix := "ip4"
		if protocol == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
		}

		/* same subnet acl */
		sameSubnetMatch := NewAndACLMatch(
			NewACLMatch(ipSuffix+".src", "==", cidr, ""),
			NewACLMatch(ipSuffix+".dst", "==", cidr, ""),
		)

		sameSubnetACL, err := c.newACL(lsName, ovnnb.ACLDirectionToLport, util.SubnetAllowPriority, sameSubnetMatch.String(), ovnnb.ACLActionAllowRelated, util.NetpolACLTier)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("new same subnet ingress acl for logical switch %s: %w", lsName, err)
		}

		acls = append(acls, sameSubnetACL)

		// node subnet acl
		if err := nodeSubnetACLFunc(protocol, ipSuffix); err != nil {
			klog.Error(err)
			return err
		}

		// allow subnet acl
		if err := allowSubnetACLFunc(protocol, ipSuffix, cidr); err != nil {
			klog.Error(err)
			return err
		}
	}

	if err := c.CreateAcls(lsName, LogicalSwitchKey, acls...); err != nil {
		klog.Error(err)
		return fmt.Errorf("add ingress acls to logical switch %s: %w", lsName, err)
	}

	return nil
}

func (c *OVNNbClient) SetACLLog(pgName string, logEnable, isIngress bool) error {
	direction := ovnnb.ACLDirectionToLport
	portDirection := "outport"
	if !isIngress {
		direction = ovnnb.ACLDirectionFromLport
		portDirection = "inport"
	}

	// match all traffic to or from pgName
	allIPMatch := NewAndACLMatch(
		NewACLMatch(portDirection, "==", "@"+pgName, ""),
		NewACLMatch("ip", "", "", ""),
	)

	acl, err := c.GetACL(pgName, direction, util.IngressDefaultDrop, allIPMatch.String(), true)
	if err != nil {
		klog.Error(err)
		return err
	}

	if acl == nil {
		return nil // skip if acl not found
	}

	if acl.Log == logEnable {
		return nil
	}
	acl.Log = logEnable

	err = c.UpdateACL(acl, &acl.Log)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("update acl: %w", err)
	}

	return nil
}

// CreateAcls create several acl once
// parentType is 'ls' or 'pg'
func (c *OVNNbClient) CreateAcls(parentName, parentType string, acls ...*ovnnb.ACL) error {
	ops, err := c.CreateAclsOps(parentName, parentType, acls...)
	if err != nil {
		klog.Error(err)
		return err
	}

	if err = c.Transact("acls-add", ops); err != nil {
		return fmt.Errorf("add acls to type %s %s: %w", parentType, parentName, err)
	}

	return nil
}

func (c *OVNNbClient) CreateBareACL(parentName, direction, priority, match, action string) error {
	acl, err := c.newACL(parentName, direction, priority, match, action, util.NetpolACLTier)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("new acl direction %s priority %s match %s action %s: %w", direction, priority, match, action, err)
	}

	op, err := c.Create(acl)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for creating acl direction %s priority %s match %s action %s: %w", direction, priority, match, action, err)
	}

	if err = c.Transact("acl-create", op); err != nil {
		klog.Error(err)
		return fmt.Errorf("create acl direction %s priority %s match %s action %s: %w", direction, priority, match, action, err)
	}

	return nil
}

// DeleteAcls delete several acl once,
// delete to-lport and from-lport direction acl when direction is empty, otherwise one-way
// parentType is 'ls' or 'pg'
func (c *OVNNbClient) DeleteAcls(parentName, parentType, direction string, externalIDs map[string]string) error {
	ops, err := c.DeleteAclsOps(parentName, parentType, direction, externalIDs)
	if err != nil {
		klog.Error(err)
		return err
	}

	if err = c.Transact("acls-del", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("del acls from type %s %s: %w", parentType, parentName, err)
	}

	return nil
}

func (c *OVNNbClient) DeleteACL(parentName, parentType, direction, priority, match string) error {
	acl, err := c.GetACL(parentName, direction, priority, match, true)
	if err != nil {
		klog.Error(err)
		return err
	}

	if acl == nil {
		return nil // skip if acl not exist
	}

	// the acls column has a strong reference to the ACL table, so there is no need to delete the ACL
	var removeACLOp []ovsdb.Operation
	if parentType == portGroupKey { // remove acl from port group
		removeACLOp, err = c.portGroupUpdateACLOp(parentName, []string{acl.UUID}, ovsdb.MutateOperationDelete)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("generate operations for deleting acl from port group %s: %w", parentName, err)
		}
	} else { // remove acl from logical switch
		removeACLOp, err = c.logicalSwitchUpdateACLOp(parentName, []string{acl.UUID}, ovsdb.MutateOperationDelete)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("generate operations for deleting acl from logical switch %s: %w", parentName, err)
		}
	}

	if err = c.Transact("acls-del", removeACLOp); err != nil {
		klog.Error(err)
		return fmt.Errorf("del acls from type %s %s: %w", parentType, parentName, err)
	}

	return nil
}

// GetACL get acl by direction, priority and match,
// be consistent with ovn-nbctl which direction, priority and match determine one acl in port group or logical switch
func (c *OVNNbClient) GetACL(parent, direction, priority, match string, ignoreNotFound bool) (*ovnnb.ACL, error) {
	// this is necessary because may exist same direction, priority and match acl in different port group or logical switch
	if len(parent) == 0 {
		return nil, errors.New("the port group name or logical switch name is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	intPriority, _ := strconv.Atoi(priority)

	aclList := make([]ovnnb.ACL, 0)
	if err := c.ovsDbClient.WhereCache(func(acl *ovnnb.ACL) bool {
		return len(acl.ExternalIDs) != 0 && acl.ExternalIDs[aclParentKey] == parent && acl.Direction == direction && acl.Priority == intPriority && acl.Match == match
	}).List(ctx, &aclList); err != nil {
		klog.Error(err)
		return nil, NewACLError(ACLErrorDatabase, fmt.Sprintf("get acl with 'parent %s direction %s priority %s match %s': %v", parent, direction, priority, match, err))
	}

	// not found
	if len(aclList) == 0 {
		if ignoreNotFound {
			return nil, nil
		}
		return nil, NewACLError(ACLErrorNotFound, fmt.Sprintf("not found acl with 'parent %s direction %s priority %s match %s'", parent, direction, priority, match))
	}

	if len(aclList) > 1 {
		return nil, NewACLError(ACLErrorDuplicated, fmt.Sprintf("more than one acl with same 'parent %s direction %s priority %s match %s'", parent, direction, priority, match))
	}

	// #nosec G602
	return &aclList[0], nil
}

// ListAcls list acls which match the given externalIDs,
// result should include all to-lport and from-lport acls when direction is empty,
// result should include all acls when externalIDs is empty,
// result should include all acls which externalIDs[key] is not empty when externalIDs[key] is ""
// TODO: maybe add other filter conditions(priority or match)
func (c *OVNNbClient) ListAcls(direction string, externalIDs map[string]string) ([]ovnnb.ACL, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	aclList := make([]ovnnb.ACL, 0)

	if err := c.WhereCache(aclFilter(direction, externalIDs)).List(ctx, &aclList); err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("list acls: %w", err)
	}

	return aclList, nil
}

func (c *OVNNbClient) ACLExists(parent, direction, priority, match string) (bool, error) {
	acl, err := c.GetACL(parent, direction, priority, match, true)
	if err != nil {
		var aclErr *ACLError
		if errors.As(err, &aclErr) && aclErr.Type == ACLErrorDuplicated {
			return true, nil
		}
		return false, err
	}
	return acl != nil, nil
}

// newACL return acl with basic information
func (c *OVNNbClient) newACL(parent, direction, priority, match, action string, tier int, options ...func(acl *ovnnb.ACL)) (*ovnnb.ACL, error) {
	if len(parent) == 0 {
		return nil, errors.New("the port group name or logical switch name is required")
	}

	if len(direction) == 0 || len(priority) == 0 || len(match) == 0 || len(action) == 0 {
		return nil, fmt.Errorf("acl 'direction %s' and 'priority %s' and 'match %s' and 'action %s' is required", direction, priority, match, action)
	}

	exists, err := c.ACLExists(parent, direction, priority, match)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("get parent %s acl: %w", parent, err)
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
		Tier: tier,
	}

	for _, option := range options {
		option(acl)
	}

	return acl, nil
}

// newACLWithoutCheck return acl with basic information without check acl exists,
// this would cause duplicated acl, so don't use this function to create acl normally,
// but maybe used for updating network policy acl
func (c *OVNNbClient) newACLWithoutCheck(parent, direction, priority, match, action string, tier int, options ...func(acl *ovnnb.ACL)) (*ovnnb.ACL, error) {
	if len(parent) == 0 {
		return nil, errors.New("the port group name or logical switch name is required")
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
		Tier: tier,
	}

	for _, option := range options {
		option(acl)
	}

	return acl, nil
}

// createSgRuleACL create security group rule acl
func (c *OVNNbClient) newSgRuleACL(sgName, direction string, rule kubeovnv1.SecurityGroupRule) (*ovnnb.ACL, error) {
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
	allIPMatch := NewAndACLMatch(
		NewACLMatch(portDirection, "==", "@"+pgName, ""),
		NewACLMatch(ipSuffix, "", "", ""),
	)

	/* allow allowed ip traffic */
	// type address
	allowedIPMatch := NewAndACLMatch(
		allIPMatch,
		NewACLMatch(ipKey, "==", rule.RemoteAddress, ""),
	)

	// type securityGroup
	remotePgName := GetSgV4AssociatedName(rule.RemoteSecurityGroup)
	if rule.IPVersion == "ipv6" {
		remotePgName = GetSgV6AssociatedName(rule.RemoteSecurityGroup)
	}
	if rule.RemoteType == kubeovnv1.SgRemoteTypeSg {
		allowedIPMatch = NewAndACLMatch(
			allIPMatch,
			NewACLMatch(ipKey, "==", "$"+remotePgName, ""),
		)
	}

	/* allow layer 4 traffic */
	// allow all layer 4 traffic
	match := allowedIPMatch

	switch rule.Protocol {
	case kubeovnv1.SgProtocolICMP:
		match = NewAndACLMatch(
			allowedIPMatch,
			NewACLMatch("icmp4", "", "", ""),
		)
		if ipSuffix == "ip6" {
			match = NewAndACLMatch(
				allowedIPMatch,
				NewACLMatch("icmp6", "", "", ""),
			)
		}
	case kubeovnv1.SgProtocolTCP, kubeovnv1.SgProtocolUDP:
		match = NewAndACLMatch(
			allowedIPMatch,
			NewACLMatch(string(rule.Protocol)+".dst", "<=", strconv.Itoa(rule.PortRangeMin), strconv.Itoa(rule.PortRangeMax)),
		)
	}

	action := ovnnb.ACLActionDrop
	if rule.Policy == kubeovnv1.SgPolicyAllow {
		action = ovnnb.ACLActionAllowRelated
	}

	highestPriority, _ := strconv.Atoi(util.SecurityGroupHighestPriority)

	acl, err := c.newACL(pgName, direction, strconv.Itoa(highestPriority-rule.Priority), match.String(), action, util.NetpolACLTier)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("new security group acl for port group %s: %w", pgName, err)
	}

	return acl, nil
}

func newNetworkPolicyACLMatch(pgName, asAllowName, asExceptName, protocol, direction string, npp []netv1.NetworkPolicyPort, namedPortMap map[string]*util.NamedPortInfo) []string {
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
	allIPMatch := NewAndACLMatch(
		NewACLMatch(portDirection, "==", "@"+pgName, ""),
		NewACLMatch("ip", "", "", ""),
	)

	allowedIPMatch := NewAndACLMatch(
		allIPMatch,
		NewACLMatch(ipKey, "==", "$"+asAllowName, ""),
		NewACLMatch(ipKey, "!=", "$"+asExceptName, ""),
	)

	matches := make([]string, 0)

	// allow allowed ip traffic but except
	if len(npp) == 0 {
		return []string{allowedIPMatch.String()}
	}

	for _, port := range npp {
		protocol := strings.ToLower(string(*port.Protocol))

		// allow all tcp or udp traffic
		if port.Port == nil {
			allLayer4Match := NewAndACLMatch(
				allowedIPMatch,
				NewACLMatch(protocol, "", "", ""),
			)

			matches = append(matches, allLayer4Match.String())
			continue
		}

		// allow one tcp or udp port traffic
		if port.EndPort == nil {
			tcpKey := protocol + ".dst"

			var portID int32
			if port.Port.Type == intstr.Int {
				portID = port.Port.IntVal
			} else if namedPortMap != nil {
				_, ok := namedPortMap[port.Port.StrVal]
				if !ok {
					// for cyclonus network policy test case 'should allow ingress access on one named port'
					// this case expect all-deny if no named port defined
					klog.Errorf("no named port with name %s found", port.Port.StrVal)
				} else {
					portID = namedPortMap[port.Port.StrVal].PortID
				}
			}

			oneTCPMatch := NewAndACLMatch(
				allowedIPMatch,
				NewACLMatch(tcpKey, "==", strconv.Itoa(int(portID)), ""),
			)

			matches = append(matches, oneTCPMatch.String())

			continue
		}

		// allow several tcp or udp port traffic
		tcpKey := protocol + ".dst"
		severalTCPMatch := NewAndACLMatch(
			allowedIPMatch,
			NewACLMatch(tcpKey, "<=", strconv.Itoa(int(port.Port.IntVal)), strconv.Itoa(int(*port.EndPort))),
		)
		matches = append(matches, severalTCPMatch.String())
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
func (c *OVNNbClient) CreateAclsOps(parentName, parentType string, acls ...*ovnnb.ACL) ([]ovsdb.Operation, error) {
	if parentType != portGroupKey && parentType != LogicalSwitchKey {
		return nil, fmt.Errorf("acl parent type must be '%s' or '%s'", portGroupKey, LogicalSwitchKey)
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

	createAclsOp, err := c.Create(models...)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("generate operations for creating acls: %w", err)
	}

	var aclAddOp []ovsdb.Operation
	if parentType == portGroupKey { // acl attach to port group
		aclAddOp, err = c.portGroupUpdateACLOp(parentName, aclUUIDs, ovsdb.MutateOperationInsert)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("generate operations for adding acls to port group %s: %w", parentName, err)
		}
	} else { // acl attach to logical switch
		aclAddOp, err = c.logicalSwitchUpdateACLOp(parentName, aclUUIDs, ovsdb.MutateOperationInsert)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("generate operations for adding acls to logical switch %s: %w", parentName, err)
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
func (c *OVNNbClient) DeleteAclsOps(parentName, parentType, direction string, externalIDs map[string]string) ([]ovsdb.Operation, error) {
	if parentName == "" {
		return nil, errors.New("the port group name or logical switch name is required")
	}

	if externalIDs == nil {
		externalIDs = make(map[string]string)
	}

	externalIDs[aclParentKey] = parentName

	/* delete acls from port group or logical switch */
	acls, err := c.ListAcls(direction, externalIDs)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("list type %s %s acls: %w", parentType, parentName, err)
	}

	aclUUIDs := make([]string, 0, len(acls))
	for _, acl := range acls {
		aclUUIDs = append(aclUUIDs, acl.UUID)
	}

	// the acls column has a strong reference to the ACL table, so there is no need to delete the ACL
	var removeACLOp []ovsdb.Operation
	if parentType == portGroupKey { // remove acl from port group
		removeACLOp, err = c.portGroupUpdateACLOp(parentName, aclUUIDs, ovsdb.MutateOperationDelete)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("generate operations for deleting acls from port group %s: %w", parentName, err)
		}
	} else { // remove acl from logical switch
		removeACLOp, err = c.logicalSwitchUpdateACLOp(parentName, aclUUIDs, ovsdb.MutateOperationDelete)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("generate operations for deleting acls from logical switch %s: %w", parentName, err)
		}
	}

	return removeACLOp, nil
}

// sgRuleNoACL check if security group rule has acl
func (c *OVNNbClient) sgRuleNoACL(sgName, direction string, rule kubeovnv1.SecurityGroupRule) (bool, error) {
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
	allIPMatch := NewAndACLMatch(
		NewACLMatch(portDirection, "==", "@"+pgName, ""),
		NewACLMatch(ipSuffix, "", "", ""),
	)

	/* allow allowed ip traffic */
	// type address
	allowedIPMatch := NewAndACLMatch(
		allIPMatch,
		NewACLMatch(ipKey, "==", rule.RemoteAddress, ""),
	)

	// type securityGroup
	remotePgName := GetSgV4AssociatedName(rule.RemoteSecurityGroup)
	if rule.IPVersion == "ipv6" {
		remotePgName = GetSgV6AssociatedName(rule.RemoteSecurityGroup)
	}
	if rule.RemoteType == kubeovnv1.SgRemoteTypeSg {
		allowedIPMatch = NewAndACLMatch(
			allIPMatch,
			NewACLMatch(ipKey, "==", "$"+remotePgName, ""),
		)
	}

	/* allow layer 4 traffic */
	// allow all layer 4 traffic
	match := allowedIPMatch

	switch rule.Protocol {
	case kubeovnv1.SgProtocolICMP:
		match = NewAndACLMatch(
			allowedIPMatch,
			NewACLMatch("icmp4", "", "", ""),
		)
		if ipSuffix == "ip6" {
			match = NewAndACLMatch(
				allowedIPMatch,
				NewACLMatch("icmp6", "", "", ""),
			)
		}
	case kubeovnv1.SgProtocolTCP, kubeovnv1.SgProtocolUDP:
		match = NewAndACLMatch(
			allowedIPMatch,
			NewACLMatch(string(rule.Protocol)+".dst", "<=", strconv.Itoa(rule.PortRangeMin), strconv.Itoa(rule.PortRangeMax)),
		)
	}

	securityGroupHighestPriority, _ := strconv.Atoi(util.SecurityGroupHighestPriority)
	priority := securityGroupHighestPriority - rule.Priority
	exists, err := c.ACLExists(pgName, direction, strconv.Itoa(priority), match.String())
	if err != nil {
		err = fmt.Errorf("failed to check acl rule for security group %s: %w", sgName, err)
		klog.Error(err)
		return false, err
	}

	// sg rule no acl, need to sync
	if !exists {
		return true, nil
	}
	return false, nil
}

// SGLostACL check if security group lost an acl
func (c *OVNNbClient) SGLostACL(sg *kubeovnv1.SecurityGroup) (bool, error) {
	ingressRules := sg.Spec.IngressRules
	for _, rule := range ingressRules {
		no, err := c.sgRuleNoACL(sg.Name, ovnnb.ACLDirectionToLport, rule)
		if err != nil {
			klog.Error(err)
			return false, err
		}
		if no {
			klog.Infof("security group %s lost ingress rule: %v", sg.Name, rule)
			return true, nil
		}
	}
	egressRules := sg.Spec.EgressRules
	for _, rule := range egressRules {
		no, err := c.sgRuleNoACL(sg.Name, ovnnb.ACLDirectionFromLport, rule)
		if err != nil {
			klog.Error(err)
			return false, err
		}
		if no {
			klog.Infof("security group %s lost egress rule: %v", sg.Name, rule)
			return true, nil
		}
	}
	return false, nil
}

// UpdateAnpRuleACLOps return operation that creates an ingress/egress ACL
func (c *OVNNbClient) UpdateAnpRuleACLOps(pgName, asName, protocol, aclName string, priority int, aclAction ovnnb.ACLAction, logACLActions []ovnnb.ACLAction, rulePorts []v1alpha1.AdminNetworkPolicyPort, isIngress, isBanp bool) ([]ovsdb.Operation, error) {
	acls := make([]*ovnnb.ACL, 0, 10)

	options := func(acl *ovnnb.ACL) {
		setACLName(acl, aclName)

		if acl.ExternalIDs == nil {
			acl.ExternalIDs = make(map[string]string)
		}
		acl.ExternalIDs[aclParentKey] = pgName

		if acl.Options == nil {
			acl.Options = make(map[string]string)
		}
		acl.Options["apply-after-lb"] = "true"

		if slices.Contains(logACLActions, aclAction) {
			acl.Log = true
			if aclAction == ovnnb.ACLActionDrop {
				acl.Severity = ptr.To(ovnnb.ACLSeverityWarning)
			}
		}
	}

	var direction ovnnb.ACLDirection
	if isIngress {
		direction = ovnnb.ACLDirectionToLport
	} else {
		direction = ovnnb.ACLDirectionFromLport
	}

	var tier int
	if isBanp {
		tier = util.BanpACLTier
	} else {
		tier = util.AnpACLTier
	}

	matches := newAnpACLMatch(pgName, asName, protocol, direction, rulePorts)
	for _, m := range matches {
		strPriority := strconv.Itoa(priority)
		setACL, err := c.newACLWithoutCheck(pgName, direction, strPriority, m, aclAction, tier, options)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("new ingress acl for port group %s: %w", pgName, err)
		}

		acls = append(acls, setACL)
	}

	ops, err := c.CreateAclsOps(pgName, portGroupKey, acls...)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	return ops, nil
}

func newAnpACLMatch(pgName, asName, protocol, direction string, rulePorts []v1alpha1.AdminNetworkPolicyPort) []string {
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
	allIPMatch := NewAndACLMatch(
		NewACLMatch(portDirection, "==", "@"+pgName, ""),
		NewACLMatch("ip", "", "", ""),
	)

	selectIPMatch := NewAndACLMatch(
		allIPMatch,
		NewACLMatch(ipKey, "==", "$"+asName, ""),
	)
	if len(rulePorts) == 0 {
		return []string{selectIPMatch.String()}
	}

	matches := make([]string, 0, 10)
	for _, port := range rulePorts {
		// Exactly one field must be set.
		// Do not support NamedPort now
		switch {
		case port.PortNumber != nil:
			protocol := strings.ToLower(string(port.PortNumber.Protocol))
			protocolKey := protocol + ".dst"

			oneMatch := NewAndACLMatch(
				selectIPMatch,
				NewACLMatch(protocolKey, "==", strconv.Itoa(int(port.PortNumber.Port)), ""),
			)
			matches = append(matches, oneMatch.String())
		case port.PortRange != nil:
			protocol := strings.ToLower(string(port.PortRange.Protocol))
			protocolKey := protocol + ".dst"

			severalMatch := NewAndACLMatch(
				selectIPMatch,
				NewACLMatch(protocolKey, "<=", strconv.Itoa(int(port.PortRange.Start)), strconv.Itoa(int(port.PortRange.End))),
			)
			matches = append(matches, severalMatch.String())
		default:
			klog.Errorf("failed to check port for anp ingress rule, pg %s, as %s", pgName, asName)
		}
	}
	return matches
}

func (c *OVNNbClient) MigrateACLTier() error {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var aclList []ovnnb.ACL
	if err := c.ovsDbClient.WhereCache(func(acl *ovnnb.ACL) bool { return acl.Tier == 0 }).List(ctx, &aclList); err != nil {
		err = fmt.Errorf("failed to list acls with tier 0: %w", err)
		klog.Error(err)
		return err
	}

	ops := make([]ovsdb.Operation, 0, len(aclList))
	for _, acl := range aclList {
		acl.Tier = util.NetpolACLTier
		op, err := c.Where(&acl).Update(&acl, &acl.Tier)
		if err != nil {
			klog.Error(err)
			return fmt.Errorf("failed to generate operations for updating acl %s tier: %w", acl.UUID, err)
		}
		ops = append(ops, op...)
	}
	if len(ops) == 0 {
		return nil
	}

	if err := c.Transact("acl-migrate-tier", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to migrate acl tier: %w", err)
	}

	return nil
}

func (c *OVNNbClient) CleanNoParentKeyAcls() error {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var aclList []ovnnb.ACL
	if err := c.ovsDbClient.WhereCache(func(acl *ovnnb.ACL) bool {
		_, ok := acl.ExternalIDs[aclParentKey]
		return !ok
	}).List(ctx, &aclList); err != nil {
		err = fmt.Errorf("failed to list acls without parent: %w", err)
		klog.Error(err)
		return err
	}

	ops := make([]ovsdb.Operation, 0, len(aclList))
	for _, acl := range aclList {
		var portGroups []ovnnb.PortGroup
		if err := c.ovsDbClient.WhereCache(func(pg *ovnnb.PortGroup) bool {
			return slices.Contains(pg.ACLs, acl.UUID)
		}).List(ctx, &portGroups); err == nil {
			for _, pg := range portGroups {
				op, err := c.portGroupUpdateACLOp(pg.Name, []string{acl.UUID}, ovsdb.MutateOperationDelete)
				if err == nil {
					ops = append(ops, op...)
				}
			}
		}
		var logicalSwitches []ovnnb.LogicalSwitch
		if err := c.ovsDbClient.WhereCache(func(ls *ovnnb.LogicalSwitch) bool {
			return slices.Contains(ls.ACLs, acl.UUID)
		}).List(ctx, &logicalSwitches); err == nil {
			for _, ls := range logicalSwitches {
				op, err := c.logicalSwitchUpdateACLOp(ls.Name, []string{acl.UUID}, ovsdb.MutateOperationDelete)
				if err == nil {
					ops = append(ops, op...)
				}
			}
		}
		delOp, err := c.Where(&acl).Delete()
		if err == nil {
			ops = append(ops, delOp...)
		}
	}
	if len(ops) == 0 {
		return nil
	}

	if err := c.Transact("acl-clean-no-parent", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to clean acls without parent: %w", err)
	}

	return nil
}
