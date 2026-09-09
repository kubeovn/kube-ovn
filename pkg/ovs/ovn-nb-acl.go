package ovs

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"
	v1alpha2 "sigs.k8s.io/network-policy-api/apis/v1alpha2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs/nbops"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
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
	name = limitedACLName(name)
	acl.Name = new(name)
}

func limitedACLName(name string) string {
	if len(name) > 63 {
		// ACL name length limit is 63
		return name[:60] + "..."
	}
	return name
}

func setACLApplyAfterLB(acl *ovnnb.ACL) {
	if acl.Options == nil {
		acl.Options = make(map[string]string)
	}
	acl.Options["apply-after-lb"] = "true"
}

func aclPortDirection(direction string) string {
	if direction == ovnnb.ACLDirectionFromLport {
		return "inport"
	}
	return "outport"
}

func aclSrcOrDst(direction string) string {
	if direction == ovnnb.ACLDirectionFromLport {
		return "dst"
	}
	return "src"
}

func aclSgEndpoints(direction string) (local, remote string) {
	if direction == ovnnb.ACLDirectionFromLport {
		return "src", "dst"
	}
	return "dst", "src"
}

func aclPGAndMatch(pgName, portDirection string, extras ...ACLMatch) ACLMatch {
	parts := make([]ACLMatch, 0, 1+len(extras))
	parts = append(parts, NewACLMatch(portDirection, "==", "@"+pgName, ""))
	parts = append(parts, extras...)
	return NewAndACLMatch(parts...)
}

func aclPGIPMatch(pgName, portDirection string) ACLMatch {
	return aclPGAndMatch(pgName, portDirection, NewACLMatch("ip", "", "", ""))
}

func aclPGDHCPMatch(pgName, portDirection, udpSrc, udpDst, ipFamily string) ACLMatch {
	return aclPGAndMatch(pgName, portDirection,
		NewACLMatch("udp.src", "==", udpSrc, ""),
		NewACLMatch("udp.dst", "==", udpDst, ""),
		NewACLMatch(ipFamily, "", "", ""),
	)
}

func sameSubnetACLMatch(ipSuffix, cidr string) ACLMatch {
	return NewAndACLMatch(
		NewACLMatch(ipSuffix+".src", "==", cidr, ""),
		NewACLMatch(ipSuffix+".dst", "==", cidr, ""),
	)
}

func networkPolicyPortMatches(allowedIPMatch ACLMatch, pgName, direction string, npp []netv1.NetworkPolicyPort, namedPortMap map[string]*util.NamedPortInfo) []string {
	if len(npp) == 0 {
		return []string{allowedIPMatch.String()}
	}
	matches := make([]string, 0, len(npp))
	for _, port := range npp {
		protocol := strings.ToLower(string(*port.Protocol))
		if port.Port == nil {
			matches = append(matches, NewAndACLMatch(allowedIPMatch, NewACLMatch(protocol, "", "", "")).String())
			continue
		}
		tcpKey := protocol + ".dst"
		if port.EndPort == nil {
			var portID int32
			if port.Port.Type == intstr.Int {
				portID = port.Port.IntVal
			} else {
				if namedPortMap == nil {
					continue
				}
				info, ok := namedPortMap[port.Port.StrVal]
				if !ok {
					klog.Errorf("no named port with name %s found in pg %s (%s)", port.Port.StrVal, pgName, direction)
					continue
				}
				portID = info.PortID
			}
			matches = append(matches, NewAndACLMatch(allowedIPMatch, NewACLMatch(tcpKey, "==", strconv.Itoa(int(portID)), "")).String())
			continue
		}
		if port.Port.Type == intstr.String {
			klog.Errorf("named port %s with endPort is not supported in pg %s (%s), skipping", port.Port.StrVal, pgName, direction)
			continue
		}
		matches = append(matches, NewAndACLMatch(allowedIPMatch, NewACLMatch(tcpKey, "<=", strconv.Itoa(int(port.Port.IntVal)), strconv.Itoa(int(*port.EndPort)))).String())
	}
	return matches
}

func aclIPSuffix(protocol string) string {
	if protocol == kubeovnv1.ProtocolIPv6 || protocol == "ipv6" {
		return "ip6"
	}
	return "ip4"
}

// UpdateDefaultBlockACLOps returns operations to update/create the default block ACL
func (c *OVNNbClient) UpdateDefaultBlockACLOps(npName, pgName, direction string, loggingEnabled, lax bool, logRate int) ([]ovsdb.Operation, error) {
	portDirection := aclPortDirection(direction)
	priority := util.IngressDefaultDrop
	meterName := fmt.Sprintf("%s_%s_meter", pgName, direction)

	if direction == ovnnb.ACLDirectionFromLport {
		priority = util.EgressDefaultDrop
	}

	var match ACLMatch

	if lax {
		// This is the "lax" enforcement mode, we block only TCP/UDP/SCTP
		match = aclPGAndMatch(pgName, portDirection, NewACLMatch("(tcp || udp || sctp)", "", "", ""))
	} else {
		// This is the "standard" enforcement mode, we block everything IP related (IPv4/IPv6/ICMPv4/ICMPv6/...)
		match = aclPGIPMatch(pgName, portDirection)
	}

	options := func(acl *ovnnb.ACL) {
		setNetworkPolicyACLName(acl, npName)
		if loggingEnabled {
			acl.Log = true
			acl.Severity = ptr.To(ovnnb.ACLSeverityWarning)
			if loggingEnabled && logRate > 0 {
				acl.Meter = new(meterName)
			}
		}

		if direction == ovnnb.ACLDirectionFromLport {
			setACLApplyAfterLB(acl)
		}
	}

	if loggingEnabled && logRate > 0 {
		if err := c.CreateOrUpdateMeter(meterName, ovnnb.MeterUnitPktps, logRate, 1); err != nil {
			klog.Errorf("failed to create meter %s: %v", meterName, err)
			return nil, fmt.Errorf("create meter %s: %w", meterName, err)
		}
	} else {
		if err := c.DeleteMeter(meterName); err != nil {
			klog.Errorf("failed to delete meter %s: %v", meterName, err)
		}
	}

	defaultDropACL, err := c.newACLWithoutCheck(pgName, direction, priority, match.String(), ovnnb.ACLActionDrop, util.NetpolACLTier, options)
	if err != nil {
		return nil, logWrap(err, wrapErr("failed to create drop acl for port group %s: %w", pgName))
	}

	ops, err := c.CreateAclsOps(pgName, portGroupKey, defaultDropACL)
	if err != nil {
		return nil, logWrap(err, wrapErr("failed to create default drop acl ops for port group %s: %w", pgName))
	}

	return ops, nil
}

// UpdateDefaultBlockExceptionsACLOps updates the exceptions to the default block ACLs of a NetworkPolicy to allow DHCPv4/DHCPv6.
func (c *OVNNbClient) UpdateDefaultBlockExceptionsACLOps(npName, pgName, npNamespace, direction string) ([]ovsdb.Operation, error) {
	portDirection := aclPortDirection(direction)
	priority := util.IngressAllowPriority
	dhcpv4UdpSrc, dhcpv4UdpDst := "67", "68"
	dhcpv6UdpSrc, dhcpv6UdpDst := "547", "546"

	if direction == ovnnb.ACLDirectionFromLport { // Egress rule
		priority = util.EgressAllowPriority
		dhcpv4UdpSrc, dhcpv4UdpDst = dhcpv4UdpDst, dhcpv4UdpSrc
		dhcpv6UdpSrc, dhcpv6UdpDst = dhcpv6UdpDst, dhcpv6UdpSrc
	}

	acls := make([]*ovnnb.ACL, 0)

	newACL := func(match string) {
		options := func(acl *ovnnb.ACL) {
			setACLName(acl, npName)
			if direction == ovnnb.ACLDirectionFromLport {
				setACLApplyAfterLB(acl)
			}
		}

		acl, err := c.newACLWithoutCheck(pgName, direction, priority, match, ovnnb.ACLActionAllowRelated, util.NetpolACLTier, options)
		if err != nil {
			klog.Error(err)
			klog.Errorf("failed to create new block exceptions acl for network policy %s/%s: %v", npNamespace, npName, err)
			return
		}
		acls = append(acls, acl)
	}

	newACL(aclPGDHCPMatch(pgName, portDirection, dhcpv6UdpSrc, dhcpv6UdpDst, "ip6").String())
	newACL(aclPGDHCPMatch(pgName, portDirection, dhcpv4UdpSrc, dhcpv4UdpDst, "ip4").String())

	ops, err := c.CreateAclsOps(pgName, portGroupKey, acls...)
	if err != nil {
		return nil, logWrap(err, wrapErr("failed to create block exceptions acl for port group %s: %w", pgName))
	}
	return ops, nil
}

func (c *OVNNbClient) ensureNPACLMeter(pgName, direction, kind string, logEnable bool, logRate int) (string, error) {
	meterName := fmt.Sprintf("%s_%s_meter", pgName, direction)
	if logEnable && logRate > 0 {
		if err := c.CreateOrUpdateMeter(meterName, ovnnb.MeterUnitPktps, logRate, 1); err != nil {
			return "", fmt.Errorf("create %s meter %s: %w", kind, meterName, err)
		}
		return meterName, nil
	}
	if err := c.DeleteMeter(meterName); err != nil {
		klog.Errorf("failed to delete %s meter %s: %v", kind, meterName, err)
	}
	return meterName, nil
}

func (c *OVNNbClient) createACLOpsFromMatches(pgName, direction, priority string, action ovnnb.ACLAction, tier int, matches []string, options func(*ovnnb.ACL), wrap func(error) error) ([]ovsdb.Operation, error) {
	acls := make([]*ovnnb.ACL, 0, len(matches))
	for _, m := range matches {
		acl, err := c.newACLWithoutCheck(pgName, direction, priority, m, action, tier, options)
		if err != nil {
			klog.Error(err)
			return nil, wrap(err)
		}
		acls = append(acls, acl)
	}
	return c.CreateAclsOps(pgName, portGroupKey, acls...)
}

func (c *OVNNbClient) createNPAllowACLOps(pgName, aclName, direction, priority, kind string, matches []string, logEnable bool, logACLActions []ovnnb.ACLAction, logRate int, meterName string, applyAfterLB bool) ([]ovsdb.Operation, error) {
	return c.createACLOpsFromMatches(pgName, direction, priority, ovnnb.ACLActionAllowRelated, util.NetpolACLTier, matches, func(acl *ovnnb.ACL) {
		setNetworkPolicyACLName(acl, aclName)
		if applyAfterLB {
			setACLApplyAfterLB(acl)
		}
		if logEnable && slices.Contains(logACLActions, ovnnb.ACLActionAllow) {
			acl.Log = true
			if logRate > 0 {
				acl.Meter = new(meterName)
			}
		}
	}, func(err error) error {
		return fmt.Errorf("new allow %s acl for port group %s: %w", kind, pgName, err)
	})
}

func (c *OVNNbClient) updateNetworkPolicyAllowACLOps(pgName, asName, asExceptName, protocol, aclName, direction, priority, kind string, npp []netv1.NetworkPolicyPort, logEnable bool, logACLActions []ovnnb.ACLAction, logRate int, namedPortMap map[string]*util.NamedPortInfo, applyAfterLB bool) ([]ovsdb.Operation, error) {
	meterName, err := c.ensureNPACLMeter(pgName, direction, kind, logEnable, logRate)
	if err != nil {
		return nil, err
	}
	matches := newNetworkPolicyACLMatch(pgName, asName, asExceptName, protocol, direction, npp, namedPortMap)
	return c.createNPAllowACLOps(pgName, aclName, direction, priority, kind, matches, logEnable, logACLActions, logRate, meterName, applyAfterLB)
}

// UpdateIngressACLOps return operation that creates an ingress ACL
func (c *OVNNbClient) UpdateIngressACLOps(pgName, asIngressName, asExceptName, protocol, aclName string, npp []netv1.NetworkPolicyPort, logEnable bool, logACLActions []ovnnb.ACLAction, logRate int, namedPortMap map[string]*util.NamedPortInfo) ([]ovsdb.Operation, error) {
	return c.updateNetworkPolicyAllowACLOps(pgName, asIngressName, asExceptName, protocol, aclName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, "ingress", npp, logEnable, logACLActions, logRate, namedPortMap, false)
}

func (c *OVNNbClient) UpdateEgressACLOps(pgName, asEgressName, asExceptName, protocol, aclName string, npp []netv1.NetworkPolicyPort, logEnable bool, logACLActions []ovnnb.ACLAction, logRate int, namedPortMap map[string]*util.NamedPortInfo) ([]ovsdb.Operation, error) {
	return c.updateNetworkPolicyAllowACLOps(pgName, asEgressName, asExceptName, protocol, aclName, ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, "egress", npp, logEnable, logACLActions, logRate, namedPortMap, true)
}

func (c *OVNNbClient) CreateGatewayACL(lsName, pgName string) error {
	var parentName, parentType string
	switch {
	case len(pgName) != 0:
		parentName, parentType = pgName, portGroupKey
	case len(lsName) != 0:
		parentName, parentType = lsName, LogicalSwitchKey
	default:
		return errors.New("one of port group name and logical switch name must be specified")
	}

	icmpv6EgressACL, err := c.newACLLogged(parentName, ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, "icmp6", ovnnb.ACLActionAllowStateless, util.NetpolACLTier, wrapErr("new icmpv6 egress acl for %s: %w", parentName), setACLApplyAfterLB)
	if err != nil {
		return err
	}
	icmpv6IngressACL, err := c.newACLLogged(parentName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, "icmp6", ovnnb.ACLActionAllowStateless, util.NetpolACLTier, wrapErr("new icmpv6 ingress acl for %s: %w", parentName))
	if err != nil {
		return err
	}
	if err := c.CreateAcls(parentName, parentType, icmpv6EgressACL, icmpv6IngressACL); err != nil {
		return logWrap(err, wrapErr("add gateway acls to %s: %w", parentName))
	}
	return nil
}

// CreateNodeACL create allow acl for node join ip
func nodeJoinACLMatch(ipSuffix, ip, pgAs string, ingress bool) string {
	if ingress {
		return fmt.Sprintf("%s.src == %s && %s.dst == $%s", ipSuffix, ip, ipSuffix, pgAs)
	}
	return fmt.Sprintf("%s.dst == %s && %s.src == $%s", ipSuffix, ip, ipSuffix, pgAs)
}

func (c *OVNNbClient) CreateNodeACL(pgName, nodeIPStr, joinIPStr string) error {
	acls := make([]*ovnnb.ACL, 0)
	nodeIPs := strings.Split(nodeIPStr, ",")
	for _, nodeIP := range nodeIPs {
		protocol := util.CheckProtocol(nodeIP)
		ipSuffix := aclIPSuffix(protocol)
		pgAs := fmt.Sprintf("%s_%s", pgName, ipSuffix)

		allowIngressACL, err := c.newACLLogged(pgName, ovnnb.ACLDirectionToLport, util.NodeAllowPriority, nodeJoinACLMatch(ipSuffix, nodeIP, pgAs, true), ovnnb.ACLActionAllowRelated, util.NetpolACLTier, wrapErr("new allow ingress acl for port group %s: %w", pgName))
		if err != nil {
			return err
		}
		allowEgressACL, err := c.newACLLogged(pgName, ovnnb.ACLDirectionFromLport, util.NodeAllowPriority, nodeJoinACLMatch(ipSuffix, nodeIP, pgAs, false), ovnnb.ACLActionAllowRelated, util.NetpolACLTier, wrapErr("new allow egress acl for port group %s: %w", pgName), setACLApplyAfterLB)
		if err != nil {
			return err
		}
		acls = append(acls, allowIngressACL, allowEgressACL)
	}

	for joinIP := range strings.SplitSeq(joinIPStr, ",") {
		if slices.Contains(nodeIPs, joinIP) {
			continue
		}
		protocol := util.CheckProtocol(joinIP)
		ipSuffix := aclIPSuffix(protocol)
		pgAs := fmt.Sprintf("%s_%s", pgName, ipSuffix)
		if err := c.deleteNodeJoinACL(pgName, ipSuffix, joinIP, pgAs, true); err != nil {
			return err
		}
		if err := c.deleteNodeJoinACL(pgName, ipSuffix, joinIP, pgAs, false); err != nil {
			return err
		}
	}

	if err := c.CreateAcls(pgName, portGroupKey, acls...); err != nil {
		return fmt.Errorf("add node acls to port group %s: %w", pgName, err)
	}

	return nil
}

func (c *OVNNbClient) newACLLogged(parent, direction, priority, match, action string, tier int, wrap func(error) error, options ...func(*ovnnb.ACL)) (*ovnnb.ACL, error) {
	acl, err := c.newACL(parent, direction, priority, match, action, tier, options...)
	if err != nil {
		klog.Error(err)
		if wrap != nil {
			return nil, wrap(err)
		}
		return nil, err
	}
	return acl, nil
}

func (c *OVNNbClient) deleteNodeJoinACL(pgName, ipSuffix, joinIP, pgAs string, ingress bool) error {
	direction, kind := ovnnb.ACLDirectionFromLport, "egress"
	if ingress {
		direction, kind = ovnnb.ACLDirectionToLport, "ingress"
	}
	if err := c.DeleteACL(pgName, portGroupKey, direction, util.NodeAllowPriority, nodeJoinACLMatch(ipSuffix, joinIP, pgAs, ingress), util.NetpolACLTier); err != nil {
		klog.Errorf("delete %s acl from port group %s: %v", kind, pgName, err)
		return err
	}
	return nil
}

func (c *OVNNbClient) appendSgACL(acls []*ovnnb.ACL, pgName, direction, priority, match, action string, tier int, errFmt, sgName string) ([]*ovnnb.ACL, error) {
	acl, err := c.newACLLogged(pgName, direction, priority, match, action, tier, wrapErr(errFmt, sgName))
	if err != nil {
		return nil, err
	}
	return append(acls, acl), nil
}

func (c *OVNNbClient) CreateSgDenyAllACL(sgName string) error {
	pgName := GetSgPortGroupName(sgName)

	acls := make([]*ovnnb.ACL, 0)

	// Add default deny rules for all the tiers. This is to ensure that if a packet
	// is moved between tiers during acl evaluation, it is always dropped if no explicit
	// allow or drop rule is not hit.
	for tier := util.SecurityGroupAPITierMinimum; tier <= util.SecurityGroupAPITierMaximum; tier++ {
		ovnTier := util.ConvertSGTierToOvnTier(tier)
		var err error
		acls, err = c.appendSgACL(acls, pgName, ovnnb.ACLDirectionToLport, util.SecurityGroupDropPriority, fmt.Sprintf("outport == @%s && ip", pgName), ovnnb.ACLActionDrop, ovnTier, "new deny all ingress acl for security group %s: %w", sgName)
		if err != nil {
			return err
		}
		acls, err = c.appendSgACL(acls, pgName, ovnnb.ACLDirectionFromLport, util.SecurityGroupDropPriority, fmt.Sprintf("inport == @%s && ip", pgName), ovnnb.ACLActionDrop, ovnTier, "new deny all egress acl for security group %s: %w", sgName)
		if err != nil {
			return err
		}
	}

	err := c.CreateAcls(pgName, portGroupKey, acls...)
	if err != nil {
		return logWrap(err, wrapErr("add deny all acl to port group %s: %w", pgName))
	}

	return nil
}

// CreateSgACL create allow acl for security group
func (c *OVNNbClient) CreateSgBaseACL(sgName, direction string) error {
	pgName := GetSgPortGroupName(sgName)

	// ingress rule
	portDirection := aclPortDirection(direction)
	dhcpv4UdpSrc, dhcpv4UdpDst := "67", "68"
	dhcpv6UdpSrc, dhcpv6UdpDst := "547", "546"
	icmpv6Type := "{130, 134, 135, 136}"
	// 130 group membership query
	// 133 router solicitation
	// 134 router advertisement
	// 135 neighbor solicitation
	// 136 neighbor advertisement

	if direction == ovnnb.ACLDirectionFromLport { // egress rule
		dhcpv4UdpSrc, dhcpv4UdpDst = dhcpv4UdpDst, dhcpv4UdpSrc
		dhcpv6UdpSrc, dhcpv6UdpDst = dhcpv6UdpDst, dhcpv6UdpSrc
		icmpv6Type = "{130, 133, 135, 136}"
	}

	acls := make([]*ovnnb.ACL, 0)

	newACL := func(match string) {
		// Add baserules for all the tiers. This is to ensure that if a packet
		// is moved between tiers during acl evaluation, the protocol packets are always allowed.
		for tier := util.SecurityGroupAPITierMinimum; tier <= util.SecurityGroupAPITierMaximum; tier++ {
			acl, err := c.newACL(pgName, direction, util.SecurityGroupBasePriority, match, ovnnb.ACLActionAllowRelated, util.ConvertSGTierToOvnTier(tier))
			if err != nil {
				klog.Error(err)
				klog.Errorf("failed to create new base ingress acl for security group %s: %v", sgName, err)
				return
			}
			acls = append(acls, acl)
		}
	}

	newACL(aclPGAndMatch(pgName, portDirection, NewACLMatch("arp", "", "", "")).String())
	newACL(aclPGAndMatch(pgName, portDirection,
		NewACLMatch("icmp6.type", "==", icmpv6Type, ""),
		NewACLMatch("icmp6.code", "==", "0", ""),
		NewACLMatch("ip.ttl", "==", "255", ""),
	).String())
	newACL(aclPGDHCPMatch(pgName, portDirection, dhcpv4UdpSrc, dhcpv4UdpDst, "ip4").String())
	newACL(aclPGDHCPMatch(pgName, portDirection, dhcpv6UdpSrc, dhcpv6UdpDst, "ip6").String())
	newACL(aclPGAndMatch(pgName, portDirection, NewACLMatch("ip.proto", "==", "112", "")).String())

	if err := c.CreateAcls(pgName, portGroupKey, acls...); err != nil {
		return logWrap(err, wrapErr("add ingress acls to port group %s: %w", pgName))
	}
	return nil
}

func (c *OVNNbClient) UpdateSgACL(sg *kubeovnv1.SecurityGroup, direction string) error {
	pgName := GetSgPortGroupName(sg.Name)

	// clear acl
	if err := c.DeleteAcls(pgName, portGroupKey, direction, nil); err != nil {
		return logWrap(err, wrapErr("delete direction '%s' acls from port group %s: %w", direction, pgName))
	}

	acls := make([]*ovnnb.ACL, 0, 2)

	// ingress rule
	srcOrDst, portDirection := aclSrcOrDst(direction), aclPortDirection(direction)
	sgRules := sg.Spec.IngressRules
	if direction == ovnnb.ACLDirectionFromLport { // egress rule
		sgRules = sg.Spec.EgressRules
	}

	/* create port_group associated acl */
	if sg.Spec.AllowSameGroupTraffic {
		asName := GetSgV4AssociatedName(sg.Name)
		for _, ipSuffix := range []string{"ip4", "ip6"} {
			if ipSuffix == "ip6" {
				asName = GetSgV6AssociatedName(sg.Name)
			}

			match := aclPGAndMatch(pgName, portDirection,
				NewACLMatch(ipSuffix, "", "", ""),
				NewACLMatch(ipSuffix+"."+srcOrDst, "==", "$"+asName, ""),
			)
			acl, err := c.newACLLogged(pgName, direction, util.SecurityGroupAllowPriority, match.String(), ovnnb.ACLActionAllowRelated, util.ConvertSGTierToOvnTier(sg.Spec.Tier), wrapErr("new allow acl for security group %s: %w", sg.Name))
			if err != nil {
				return err
			}
			acls = append(acls, acl)
		}
	}

	/* create rule acl */
	for _, rule := range sgRules {
		acl, err := c.newSgRuleACL(sg.Name, direction, rule, util.ConvertSGTierToOvnTier(sg.Spec.Tier))
		if err != nil {
			return logWrap(err, wrapErr("new rule acl for security group %s: %w", sg.Name))
		}
		acls = append(acls, acl)
	}

	if err := c.CreateAcls(pgName, portGroupKey, acls...); err != nil {
		return logWrap(err, wrapErr("add acl to port group %s: %w", pgName))
	}

	return nil
}

func (c *OVNNbClient) UpdateLogicalSwitchACL(lsName, cidrBlock string, subnetAcls []kubeovnv1.ACL, allowEWTraffic bool) error {
	if len(subnetAcls) == 0 {
		if err := c.DeleteAcls(lsName, LogicalSwitchKey, "", map[string]string{"subnet": lsName}); err != nil {
			return logWrap(err, wrapErr("delete subnet acls from %s: %w", lsName))
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

			ipSuffix := aclIPSuffix(protocol)

			sameSubnetMatch := sameSubnetACLMatch(ipSuffix, cidr)
			for _, direction := range []string{ovnnb.ACLDirectionToLport, ovnnb.ACLDirectionFromLport} {
				kind := "ingress"
				if direction == ovnnb.ACLDirectionFromLport {
					kind = "egress"
				}
				acl, err := c.newACLLogged(lsName, direction, util.AllowEWTrafficPriority, sameSubnetMatch.String(), ovnnb.ACLActionAllow, util.NetpolACLTier, wrapErr("new same subnet %s acl for logical switch %s: %w", kind, lsName), options)
				if err != nil {
					return err
				}
				acls = append(acls, acl)
			}
		}
	}

	/* recreate logical switch acl */
	for _, subnetACL := range subnetAcls {
		acl, err := c.newACLLogged(lsName, subnetACL.Direction, strconv.Itoa(subnetACL.Priority), subnetACL.Match, subnetACL.Action, util.NetpolACLTier, wrapErr("new acl for logical switch %s: %w", lsName), options)
		if err != nil {
			return err
		}
		acls = append(acls, acl)
	}

	delOps, err := c.DeleteAclsOps(lsName, LogicalSwitchKey, "", map[string]string{"subnet": lsName})
	if err != nil {
		return logErr(err)
	}

	addOps, err := c.CreateAclsOps(lsName, LogicalSwitchKey, acls...)
	if err != nil {
		return logErr(err)
	}

	return c.transactGenerated("acls-update", append(delOps, addOps...), nil, nil,
		wrapErr("update acls for logical switch %s: %w", lsName),
	)
}

// UpdateACL update acl
func (c *OVNNbClient) UpdateACL(acl *ovnnb.ACL, fields ...any) error {
	if acl == nil {
		return errors.New("address_set is nil")
	}

	return c.updateModelLogged("acl-update", acl, func(err error) error {
		return fmt.Errorf("update acl with 'direction %s priority %d match %s': %w", acl.Direction, acl.Priority, acl.Match, err)
	}, fields...)
}

// SetLogicalSwitchPrivate will drop all ingress traffic except allow subnets, same subnet and node subnet
func (c *OVNNbClient) SetLogicalSwitchPrivate(lsName, cidrBlock, nodeSwitchCIDR string, allowSubnets []string) error {
	// clear acls
	if err := c.DeleteAcls(lsName, LogicalSwitchKey, "", nil); err != nil {
		return logWrap(err, wrapErr("clear logical switch %s acls: %w", lsName))
	}

	acls := make([]*ovnnb.ACL, 0)

	/* default drop acl */
	allIPMatch := NewACLMatch("ip", "", "", "")

	options := func(acl *ovnnb.ACL) {
		setACLName(acl, lsName)
		acl.Log = true
		acl.Severity = ptr.To(ovnnb.ACLSeverityWarning)
	}

	defaultDropACL, err := c.newACLLogged(lsName, ovnnb.ACLDirectionToLport, util.DefaultDropPriority, allIPMatch.String(), ovnnb.ACLActionDrop, util.NetpolACLTier, wrapErr("new default drop ingress acl for logical switch %s: %w", lsName), options)
	if err != nil {
		return err
	}

	acls = append(acls, defaultDropACL)

	nodeSubnetACLFunc := func(protocol, ipSuffix string) error {
		for nodeCidr := range strings.SplitSeq(nodeSwitchCIDR, ",") {
			// skip different address family
			if protocol != util.CheckProtocol(nodeCidr) {
				continue
			}

			match := NewACLMatch(ipSuffix+".src", "==", nodeCidr, "")

			acl, err := c.newACLLogged(lsName, ovnnb.ACLDirectionToLport, util.NodeAllowPriority, match.String(), ovnnb.ACLActionAllowRelated, util.NetpolACLTier, wrapErr("new node subnet ingress acl for logical switch %s: %w", lsName))
			if err != nil {
				return err
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

			acl, err := c.newACLLogged(lsName, ovnnb.ACLDirectionToLport, util.SubnetAllowPriority, match.String(), ovnnb.ACLActionAllowRelated, util.NetpolACLTier, wrapErr("new allow subnet ingress acl for logical switch %s: %w", lsName))
			if err != nil {
				return err
			}

			acls = append(acls, acl)
		}
		return nil
	}

	for cidr := range strings.SplitSeq(cidrBlock, ",") {
		protocol := util.CheckProtocol(cidr)

		ipSuffix := aclIPSuffix(protocol)

		sameSubnetMatch := sameSubnetACLMatch(ipSuffix, cidr)

		sameSubnetACL, err := c.newACLLogged(lsName, ovnnb.ACLDirectionToLport, util.SubnetAllowPriority, sameSubnetMatch.String(), ovnnb.ACLActionAllowRelated, util.NetpolACLTier, wrapErr("new same subnet ingress acl for logical switch %s: %w", lsName))
		if err != nil {
			return err
		}

		acls = append(acls, sameSubnetACL)

		// node subnet acl
		if err := nodeSubnetACLFunc(protocol, ipSuffix); err != nil {
			return logErr(err)
		}

		// allow subnet acl
		if err := allowSubnetACLFunc(protocol, ipSuffix, cidr); err != nil {
			return logErr(err)
		}
	}

	if err := c.CreateAcls(lsName, LogicalSwitchKey, acls...); err != nil {
		return logWrap(err, wrapErr("add ingress acls to logical switch %s: %w", lsName))
	}

	return nil
}

// SetLogicalSwitchRouted installs an allow-list / default-deny ACL policy so
// pods may only ARP/ND the gateway and send/receive IP frames via the logical
// router port MAC. Same-subnet traffic is hairpinned through the OVN logical
// router. When private is true, ingress via the router is further limited to
// the subnet CIDR (hairpin), node join CIDR, and allowSubnets.
//
// router is the logical router name used to derive the router LSP on the
// switch; from-lport allows that use eth.src == LRP MAC are constrained to
// that inport so pods cannot spoof the router MAC (port security is off by
// default).
func (c *OVNNbClient) SetLogicalSwitchRouted(lsName, router, cidrBlock, gateway, gatewayMAC, nodeSwitchCIDR string, allowSubnets []string, private bool) error {
	if lsName == "" {
		return errors.New("logical switch name is required")
	}
	if router == "" {
		return fmt.Errorf("router is required for routed subnet %s", lsName)
	}
	if gatewayMAC == "" {
		return fmt.Errorf("gateway MAC is required for routed subnet %s", lsName)
	}

	if err := c.DeleteAcls(lsName, LogicalSwitchKey, "", nil); err != nil {
		return logWrap(err, wrapErr("clear logical switch %s acls: %w", lsName))
	}

	acls, err := c.buildRoutedLogicalSwitchACLs(lsName, router, cidrBlock, gateway, gatewayMAC, nodeSwitchCIDR, allowSubnets, private)
	if err != nil {
		return err
	}
	if err := c.CreateAcls(lsName, LogicalSwitchKey, acls...); err != nil {
		return logWrap(err, wrapErr("add routed acls to logical switch %s: %w", lsName))
	}
	return nil
}

func (c *OVNNbClient) buildRoutedLogicalSwitchACLs(lsName, router, cidrBlock, gateway, gatewayMAC, nodeSwitchCIDR string, allowSubnets []string, private bool) ([]*ovnnb.ACL, error) {
	options := func(acl *ovnnb.ACL) { setACLName(acl, lsName) }
	acls := make([]*ovnnb.ACL, 0)

	discoveryACLs, err := c.buildRoutedGatewayDiscoveryACLs(lsName, gateway, options)
	if err != nil {
		return nil, err
	}
	acls = append(acls, discoveryACLs...)

	ipACLs, err := c.buildRoutedIPAllowACLs(lsName, router, cidrBlock, gatewayMAC, nodeSwitchCIDR, allowSubnets, private, options)
	if err != nil {
		return nil, err
	}
	acls = append(acls, ipACLs...)

	denyACLs, err := c.buildRoutedDefaultDenyACLs(lsName, options)
	if err != nil {
		return nil, err
	}
	acls = append(acls, denyACLs...)
	return acls, nil
}

func (c *OVNNbClient) appendACL(acls []*ovnnb.ACL, parent, direction, priority, match, action string, wrap func(error) error, options func(*ovnnb.ACL)) ([]*ovnnb.ACL, error) {
	var opts []func(*ovnnb.ACL)
	if options != nil {
		opts = []func(*ovnnb.ACL){options}
	}
	acl, err := c.newACL(parent, direction, priority, match, action, util.NetpolACLTier, opts...)
	if err != nil {
		if wrap != nil {
			return nil, wrap(err)
		}
		return nil, err
	}
	return append(acls, acl), nil
}

func (c *OVNNbClient) appendRoutedACL(acls []*ovnnb.ACL, lsName, direction, match, action, errFmt string, options func(*ovnnb.ACL)) ([]*ovnnb.ACL, error) {
	return c.appendACL(acls, lsName, direction, util.RoutedAllowPriority, match, action, wrapErr(errFmt, lsName), options)
}

func (c *OVNNbClient) appendRoutedDiscoveryPair(acls []*ovnnb.ACL, lsName, kind, egressMatch, ingressMatch string, options func(*ovnnb.ACL)) ([]*ovnnb.ACL, error) {
	var err error
	acls, err = c.appendRoutedACL(acls, lsName, ovnnb.ACLDirectionFromLport, egressMatch, ovnnb.ACLActionAllow, "new routed "+kind+" allow acl for logical switch %s: %w", options)
	if err != nil {
		return nil, err
	}
	return c.appendRoutedACL(acls, lsName, ovnnb.ACLDirectionToLport, ingressMatch, ovnnb.ACLActionAllow, "new routed "+kind+" ingress acl for logical switch %s: %w", options)
}

func (c *OVNNbClient) buildRoutedGatewayDiscoveryACLs(lsName, gateway string, options func(*ovnnb.ACL)) ([]*ovnnb.ACL, error) {
	acls := make([]*ovnnb.ACL, 0)
	for _, gw := range util.SplitTrimmed(gateway, ",") {
		var err error
		switch util.CheckProtocol(gw) {
		case kubeovnv1.ProtocolIPv4:
			acls, err = c.appendRoutedDiscoveryPair(acls, lsName, "ARP",
				NewAndACLMatch(NewACLMatch("arp", "", "", ""), NewACLMatch("arp.tpa", "==", gw, "")).String(),
				NewAndACLMatch(NewACLMatch("arp", "", "", ""), NewACLMatch("arp.spa", "==", gw, "")).String(),
				options)
		case kubeovnv1.ProtocolIPv6:
			acls, err = c.appendRoutedDiscoveryPair(acls, lsName, "ND",
				NewAndACLMatch(NewACLMatch("nd_ns", "", "", ""), NewACLMatch("nd.target", "==", gw, "")).String(),
				NewAndACLMatch(NewACLMatch("nd_na", "", "", ""), NewACLMatch("ip6.src", "==", gw, "")).String(),
				options)
		}
		if err != nil {
			return nil, err
		}
	}
	return acls, nil
}

func (c *OVNNbClient) buildRoutedIPAllowACLs(lsName, router, cidrBlock, gatewayMAC, nodeSwitchCIDR string, allowSubnets []string, private bool, options func(*ovnnb.ACL)) ([]*ovnnb.ACL, error) {
	acls := make([]*ovnnb.ACL, 0)
	routerLSP := LogicalSwitchPortName(router, lsName)

	// Pod -> router: eth.dst == LRP on both ACL directions. from-lport is
	// evaluated on the pod inport; to-lport is evaluated when delivering to
	// the router LSP. Direct L2 pod->pod has eth.dst != LRP and is denied.
	toRouter := NewAndACLMatch(NewACLMatch("ip", "", "", ""), NewACLMatch("eth.dst", "==", gatewayMAC, ""))
	for _, direction := range []string{ovnnb.ACLDirectionFromLport, ovnnb.ACLDirectionToLport} {
		var err error
		acls, err = c.appendRoutedACL(acls, lsName, direction, toRouter.String(), ovnnb.ACLActionAllowRelated, "new routed to-router "+direction+" acl for logical switch %s: %w", options)
		if err != nil {
			return nil, err
		}
	}

	// Router -> switch: allow eth.src == LRP only when the packet actually
	// enters from the router LSP. A bare from-lport eth.src==LRP allow would
	// let pods spoof the router MAC (port security is disabled by default).
	fromRouter := NewAndACLMatch(
		NewACLMatch("ip", "", "", ""),
		NewACLMatch("inport", "==", fmt.Sprintf(`"%s"`, routerLSP), ""),
		NewACLMatch("eth.src", "==", gatewayMAC, ""),
	)
	acls, err := c.appendRoutedACL(acls, lsName, ovnnb.ACLDirectionFromLport, fromRouter.String(), ovnnb.ACLActionAllowRelated, "new routed from-router acl for logical switch %s: %w", options)
	if err != nil {
		return nil, err
	}

	if !private {
		// Router -> pods: delivery to pod ports has eth.src == LRP.
		fromGW := NewAndACLMatch(NewACLMatch("ip", "", "", ""), NewACLMatch("eth.src", "==", gatewayMAC, ""))
		return c.appendRoutedACL(acls, lsName, ovnnb.ACLDirectionToLport, fromGW.String(), ovnnb.ACLActionAllowRelated, "new routed ingress-from-gateway acl for logical switch %s: %w", options)
	}

	privateIngress, err := c.buildRoutedPrivateIngressACLs(lsName, cidrBlock, gatewayMAC, nodeSwitchCIDR, allowSubnets, options)
	if err != nil {
		return nil, err
	}
	return append(acls, privateIngress...), nil
}

func routedFromGatewayMatch(gatewayMAC, ipSuffix, src string) ACLMatch {
	return NewAndACLMatch(
		NewACLMatch("ip", "", "", ""),
		NewACLMatch("eth.src", "==", gatewayMAC, ""),
		NewACLMatch(ipSuffix+".src", "==", src, ""),
	)
}

func (c *OVNNbClient) appendRoutedPrivateSrcACL(acls []*ovnnb.ACL, lsName, gatewayMAC, ipSuffix, src, errFmt string, options func(*ovnnb.ACL)) ([]*ovnnb.ACL, error) {
	return c.appendRoutedACL(acls, lsName, ovnnb.ACLDirectionToLport, routedFromGatewayMatch(gatewayMAC, ipSuffix, src).String(), ovnnb.ACLActionAllowRelated, errFmt, options)
}

func (c *OVNNbClient) buildRoutedPrivateIngressACLs(lsName, cidrBlock, gatewayMAC, nodeSwitchCIDR string, allowSubnets []string, options func(*ovnnb.ACL)) ([]*ovnnb.ACL, error) {
	acls := make([]*ovnnb.ACL, 0)
	for cidr := range strings.SplitSeq(cidrBlock, ",") {
		if cidr == "" {
			continue
		}
		protocol := util.CheckProtocol(cidr)
		ipSuffix := aclIPSuffix(protocol)

		var err error
		acls, err = c.appendRoutedPrivateSrcACL(acls, lsName, gatewayMAC, ipSuffix, cidr, "new routed private hairpin acl for logical switch %s: %w", options)
		if err != nil {
			return nil, err
		}

		for nodeCidr := range strings.SplitSeq(nodeSwitchCIDR, ",") {
			if protocol != util.CheckProtocol(nodeCidr) {
				continue
			}
			acls, err = c.appendRoutedPrivateSrcACL(acls, lsName, gatewayMAC, ipSuffix, nodeCidr, "new routed private node acl for logical switch %s: %w", options)
			if err != nil {
				return nil, err
			}
		}

		for _, allowSubnet := range allowSubnets {
			subnet := strings.TrimSpace(allowSubnet)
			if subnet == "" || util.CheckProtocol(subnet) != protocol {
				continue
			}
			acls, err = c.appendRoutedPrivateSrcACL(acls, lsName, gatewayMAC, ipSuffix, subnet, "new routed private allow-subnet acl for logical switch %s: %w", options)
			if err != nil {
				return nil, err
			}
		}
	}
	return acls, nil
}

func (c *OVNNbClient) buildRoutedDefaultDenyACLs(lsName string, options func(*ovnnb.ACL)) ([]*ovnnb.ACL, error) {
	dropOptions := func(acl *ovnnb.ACL) {
		options(acl)
		acl.Log = true
		acl.Severity = ptr.To(ovnnb.ACLSeverityWarning)
	}

	acls := make([]*ovnnb.ACL, 0, 8)
	for _, direction := range []string{ovnnb.ACLDirectionFromLport, ovnnb.ACLDirectionToLport} {
		for _, match := range []string{"ip", "arp", "nd_ns", "nd_na"} {
			var err error
			acls, err = c.appendACL(acls, lsName, direction, util.RoutedDefaultDropPriority, match, ovnnb.ACLActionDrop, wrapErr("new routed default-deny %s acl (%s) for logical switch %s: %w", match, direction, lsName), dropOptions)
			if err != nil {
				return nil, err
			}
		}
	}
	return acls, nil
}

func (c *OVNNbClient) SetNetPolACLLog(pgName string, logEnable, isIngress bool) error {
	direction := namedACLDirection(isIngress)
	portDirection := aclPortDirection(direction)

	// match all traffic to or from pgName
	allIPMatch := aclPGIPMatch(pgName, portDirection)

	acl, err := c.GetACL(pgName, direction, util.IngressDefaultDrop, allIPMatch.String(), util.NetpolACLTier, true)
	if err != nil {
		return logErr(err)
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
		return logWrap(err, wrapErr("update acl: %w"))
	}

	return nil
}

// CreateAcls create several acl once
// parentType is 'ls' or 'pg'
func (c *OVNNbClient) CreateAcls(parentName, parentType string, acls ...*ovnnb.ACL) error {
	ops, err := c.CreateAclsOps(parentName, parentType, acls...)
	return c.transactGenerated("acls-add", ops, err, nil,
		wrapErr("add acls to type %s %s: %w", parentType, parentName),
	)
}

// EnsureACLParent makes exactly one logical switch or port group the owner of an ACL.
func (c *OVNNbClient) EnsureACLParent(parentName, parentType, aclUUID string) error {
	ctx, cancel := timeoutCtx(c.Database)
	defer cancel()
	return nbops.NewACLs(c.Database, c.Database).EnsureParent(ctx, parentName, parentType, aclUUID)
}

func (c *OVNNbClient) CreateBareACL(parentName, direction, priority, match, action string) error {
	acl, err := c.newACLLogged(parentName, direction, priority, match, action, util.NetpolACLTier, wrapErr("new acl direction %s priority %s match %s action %s: %w", direction, priority, match, action))
	if err != nil {
		return err
	}

	op, err := c.Database.Table(&ovnnb.ACL{}).CreateOps(acl)
	return c.transactGenerated("acl-create", op, err,
		wrapErr("generate operations for creating acl direction %s priority %s match %s action %s: %w", direction, priority, match, action),
		wrapErr("create acl direction %s priority %s match %s action %s: %w", direction, priority, match, action),
	)
}

// DeleteAcls delete several acl once,
// delete to-lport and from-lport direction acl when direction is empty, otherwise one-way
// parentType is 'ls' or 'pg'
func (c *OVNNbClient) parentUpdateACLOp(parentName, parentType string, uuids []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	if parentType == portGroupKey {
		return c.portGroupUpdateACLOp(parentName, uuids, op)
	}
	return c.logicalSwitchUpdateACLOp(parentName, uuids, op)
}

func (c *OVNNbClient) DeleteAcls(parentName, parentType, direction string, externalIDs map[string]string) error {
	ops, err := c.DeleteAclsOps(parentName, parentType, direction, externalIDs)
	return c.transactGenerated("acls-del", ops, err, nil,
		wrapErr("del acls from type %s %s: %w", parentType, parentName),
	)
}

func (c *OVNNbClient) DeleteACL(parentName, parentType, direction, priority, match string, tier int) error {
	acl, err := c.GetACL(parentName, direction, priority, match, tier, true)
	if err != nil {
		return logErr(err)
	}

	if acl == nil {
		return nil // skip if acl not exist
	}

	removeACLOp, err := c.parentUpdateACLOp(parentName, parentType, []string{acl.UUID}, ovsdb.MutateOperationDelete)
	if err != nil {
		klog.Error(err)
		kind := "logical switch"
		if parentType == portGroupKey {
			kind = "port group"
		}
		return fmt.Errorf("generate operations for deleting acl from %s %s: %w", kind, parentName, err)
	}

	return c.transactGenerated("acls-del", removeACLOp, nil, nil,
		wrapErr("del acls from type %s %s: %w", parentType, parentName),
	)
}

// GetACL get acl by direction, priority and match,
// be consistent with ovn-nbctl which direction, priority and match determine one acl in port group or logical switch
func (c *OVNNbClient) GetACL(parent, direction, priority, match string, tier int, ignoreNotFound bool) (*ovnnb.ACL, error) {
	if len(parent) == 0 {
		return nil, errors.New("the port group name or logical switch name is required")
	}

	intPriority, _ := strconv.Atoi(priority)
	aclList, err := filterLogged(c.Database, &ovnnb.ACL{}, func(acl *ovnnb.ACL) bool {
		return len(acl.ExternalIDs) != 0 && acl.ExternalIDs[aclParentKey] == parent && acl.Direction == direction && acl.Priority == intPriority && acl.Match == match && tier == acl.Tier
	}, func(err error) error {
		return NewACLError(ACLErrorDatabase, fmt.Sprintf("get acl with 'parent %s direction %s priority %s match %s tier %d': %v", parent, direction, priority, match, tier, err))
	})
	if err != nil {
		return nil, err
	}
	return compat.Unique(aclList, ignoreNotFound,
		NewACLError(ACLErrorNotFound, fmt.Sprintf("not found acl with 'parent %s direction %s priority %s match %s tier %d '", parent, direction, priority, match, tier)),
		NewACLError(ACLErrorDuplicated, fmt.Sprintf("more than one acl with same 'parent %s direction %s priority %s match %s tier %d '", parent, direction, priority, match, tier)),
	)
}

func (c *OVNNbClient) ListAcls(direction string, externalIDs map[string]string) ([]ovnnb.ACL, error) {
	return filterLogged(c.Database, &ovnnb.ACL{}, aclFilter(direction, externalIDs), func(err error) error {
		return fmt.Errorf("list acls: %w", err)
	})
}

func (c *OVNNbClient) ACLExists(parent, direction, priority, match string, tier int) (bool, error) {
	acl, err := c.GetACL(parent, direction, priority, match, tier, true)
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
func validateACLArgs(parent, direction, priority, match, action string) error {
	if len(parent) == 0 {
		return errors.New("the port group name or logical switch name is required")
	}
	if len(direction) == 0 || len(priority) == 0 || len(match) == 0 || len(action) == 0 {
		return fmt.Errorf("acl 'direction %s' and 'priority %s' and 'match %s' and 'action %s' is required", direction, priority, match, action)
	}
	return nil
}

func newACLRecord(parent, direction, priority, match, action string, tier int, options ...func(acl *ovnnb.ACL)) *ovnnb.ACL {
	intPriority, _ := strconv.Atoi(priority)
	acl := &ovnnb.ACL{
		UUID:        ovsclient.NamedUUID(),
		Action:      action,
		Direction:   direction,
		Match:       match,
		Priority:    intPriority,
		ExternalIDs: clonedVendorIDs(map[string]string{aclParentKey: parent}),
		Tier:        tier,
	}
	for _, option := range options {
		option(acl)
	}
	return acl
}

func (c *OVNNbClient) newACL(parent, direction, priority, match, action string, tier int, options ...func(acl *ovnnb.ACL)) (*ovnnb.ACL, error) {
	if err := validateACLArgs(parent, direction, priority, match, action); err != nil {
		return nil, err
	}

	exists, err := c.ACLExists(parent, direction, priority, match, tier)
	if err != nil {
		return nil, logWrap(err, wrapErr("get parent %s acl: %w", parent))
	}
	if exists {
		return nil, nil
	}
	return newACLRecord(parent, direction, priority, match, action, tier, options...), nil
}

// newACLWithoutCheck return acl with basic information without check acl exists,
// this would cause duplicated acl, so don't use this function to create acl normally,
// but maybe used for updating network policy acl
func (c *OVNNbClient) newACLWithoutCheck(parent, direction, priority, match, action string, tier int, options ...func(acl *ovnnb.ACL)) (*ovnnb.ACL, error) {
	if err := validateACLArgs(parent, direction, priority, match, action); err != nil {
		return nil, err
	}
	return newACLRecord(parent, direction, priority, match, action, tier, options...), nil
}

// createSgRuleACL create security group rule acl
func sgRuleACLPriority(rule kubeovnv1.SecurityGroupRule) string {
	highestPriority, _ := strconv.Atoi(util.SecurityGroupHighestPriority)
	return strconv.Itoa(highestPriority - rule.Priority)
}

func sgRuleACLMatch(sgName, direction string, rule kubeovnv1.SecurityGroupRule) (string, ACLMatch) {
	ipSuffix := aclIPSuffix(rule.IPVersion)
	pgName := GetSgPortGroupName(sgName)
	portDirection := aclPortDirection(direction)
	localSrcOrDst, remoteSrcOrDst := aclSgEndpoints(direction)
	remoteIPKey := ipSuffix + "." + remoteSrcOrDst
	localIPKey := ipSuffix + "." + localSrcOrDst

	allIPMatch := aclPGAndMatch(pgName, portDirection, NewACLMatch(ipSuffix, "", "", ""))
	allowedIPMatch := NewAndACLMatch(
		allIPMatch,
		NewACLMatch(remoteIPKey, "==", rule.RemoteAddress, ""),
	)
	remotePgName := GetSgV4AssociatedName(rule.RemoteSecurityGroup)
	if rule.IPVersion == "ipv6" {
		remotePgName = GetSgV6AssociatedName(rule.RemoteSecurityGroup)
	}
	if rule.RemoteType == kubeovnv1.SgRemoteTypeSg {
		allowedIPMatch = NewAndACLMatch(
			allIPMatch,
			NewACLMatch(remoteIPKey, "==", "$"+remotePgName, ""),
		)
	}
	if rule.LocalAddress != "" {
		allowedIPMatch = NewAndACLMatch(
			allowedIPMatch,
			NewACLMatch(localIPKey, "==", rule.LocalAddress, ""),
		)
	}

	match := allowedIPMatch
	switch rule.Protocol {
	case kubeovnv1.SgProtocolICMP:
		icmp := "icmp4"
		if ipSuffix == "ip6" {
			icmp = "icmp6"
		}
		match = NewAndACLMatch(allowedIPMatch, NewACLMatch(icmp, "", "", ""))
	case kubeovnv1.SgProtocolTCP, kubeovnv1.SgProtocolUDP:
		match = NewAndACLMatch(
			allowedIPMatch,
			NewACLMatch(string(rule.Protocol)+".dst", "<=", strconv.Itoa(rule.PortRangeMin), strconv.Itoa(rule.PortRangeMax)),
		)
		if rule.LocalAddress != "" {
			match = NewAndACLMatch(
				match,
				NewACLMatch(string(rule.Protocol)+".src", "<=", strconv.Itoa(rule.SourcePortRangeMin), strconv.Itoa(rule.SourcePortRangeMax)),
			)
		}
	}
	return pgName, match
}

func (c *OVNNbClient) newSgRuleACL(sgName, direction string, rule kubeovnv1.SecurityGroupRule, tier int) (*ovnnb.ACL, error) {
	pgName, match := sgRuleACLMatch(sgName, direction, rule)

	var action string
	switch rule.Policy {
	case kubeovnv1.SgPolicyAllow:
		action = ovnnb.ACLActionAllowRelated
	case kubeovnv1.SgPolicyPass:
		action = ovnnb.ACLActionPass
	default:
		action = ovnnb.ACLActionDrop
	}

	acl, err := c.newACLLogged(pgName, direction, sgRuleACLPriority(rule), match.String(), action, tier, wrapErr("new security group acl for port group %s: %w", pgName))
	if err != nil {
		return nil, err
	}
	return acl, nil
}

func newNetworkPolicyACLMatch(pgName, asAllowName, asExceptName, protocol, direction string, npp []netv1.NetworkPolicyPort, namedPortMap map[string]*util.NamedPortInfo) []string {
	ipSuffix := aclIPSuffix(protocol)

	// ingress rule
	srcOrDst, portDirection := aclSrcOrDst(direction), aclPortDirection(direction)

	ipKey := ipSuffix + "." + srcOrDst

	// match all traffic to or from pgName
	allIPMatch := aclPGIPMatch(pgName, portDirection)

	allowedIPMatch := NewAndACLMatch(
		allIPMatch,
		NewACLMatch(ipKey, "==", "$"+asAllowName, ""),
		NewACLMatch(ipKey, "!=", "$"+asExceptName, ""),
	)
	return networkPolicyPortMatches(allowedIPMatch, pgName, direction, npp, namedPortMap)
}

// newIPBlockACLMatch builds ACL match strings for ipBlock peers with per-CIDR scoped except.
// Unlike newNetworkPolicyACLMatch which uses shared address sets, this function inlines the
// CIDR and except values directly in the match expression, ensuring that except entries from
// one ipBlock do not affect other peers in the same NetworkPolicy rule.
func newIPBlockACLMatch(pgName, protocol, direction string, ipBlocks []netv1.IPBlock, npp []netv1.NetworkPolicyPort, namedPortMap map[string]*util.NamedPortInfo) []string {
	ipSuffix := aclIPSuffix(protocol)

	srcOrDst, portDirection := aclSrcOrDst(direction), aclPortDirection(direction)

	ipKey := ipSuffix + "." + srcOrDst

	// Build per-ipBlock match with scoped except
	var perBlockMatches []ACLMatch
	for i := range ipBlocks {
		block := ipBlocks[i]
		if util.CheckProtocol(block.CIDR) != protocol {
			continue
		}

		cidrMatch := NewACLMatch(ipKey, "==", block.CIDR, "")

		var filteredExcepts []string
		for _, e := range block.Except {
			if util.CheckProtocol(e) != protocol {
				continue
			}
			contained, err := util.CIDRContainsCIDR(block.CIDR, e)
			if err != nil {
				klog.Warningf("error checking containment for IPBlock except CIDR %s in main CIDR %s, skipping: %v", e, block.CIDR, err)
				continue
			}
			if !contained {
				klog.Warningf("IPBlock except CIDR %s is not contained in main CIDR %s, skipping", e, block.CIDR)
				continue
			}
			filteredExcepts = append(filteredExcepts, e)
		}

		if len(filteredExcepts) == 0 {
			perBlockMatches = append(perBlockMatches, cidrMatch)
		} else {
			exceptMatch := NewACLMatch(ipKey, "!=", "{"+strings.Join(filteredExcepts, ", ")+"}", "")
			perBlockMatches = append(perBlockMatches, NewAndACLMatch(cidrMatch, exceptMatch))
		}
	}

	if len(perBlockMatches) == 0 {
		return nil
	}

	// Combine all ipBlock matches with OR
	ipBlockL3Match := perBlockMatches[0]
	if len(perBlockMatches) > 1 {
		ipBlockL3Match = NewOrACLMatch(perBlockMatches...)
	}

	allIPMatch := aclPGIPMatch(pgName, portDirection)

	allowedIPMatch := NewAndACLMatch(allIPMatch, NewGroupACLMatch(ipBlockL3Match))
	return networkPolicyPortMatches(allowedIPMatch, pgName, direction, npp, namedPortMap)
}

// UpdateIngressIPBlockACLOps returns operations that create ingress ACLs for ipBlock peers
func (c *OVNNbClient) updateIPBlockACLOps(pgName, protocol, aclName, direction, priority, kind string, ipBlocks []netv1.IPBlock, npp []netv1.NetworkPolicyPort, logEnable bool, logACLActions []ovnnb.ACLAction, logRate int, namedPortMap map[string]*util.NamedPortInfo, applyAfterLB bool) ([]ovsdb.Operation, error) {
	matches := newIPBlockACLMatch(pgName, protocol, direction, ipBlocks, npp, namedPortMap)
	if len(matches) == 0 {
		return nil, nil
	}
	meterName := fmt.Sprintf("%s_%s_meter", pgName, direction)
	return c.createNPAllowACLOps(pgName, aclName, direction, priority, kind, matches, logEnable, logACLActions, logRate, meterName, applyAfterLB)
}

func (c *OVNNbClient) UpdateIngressIPBlockACLOps(pgName, protocol, aclName string, ipBlocks []netv1.IPBlock, npp []netv1.NetworkPolicyPort, logEnable bool, logACLActions []ovnnb.ACLAction, logRate int, namedPortMap map[string]*util.NamedPortInfo) ([]ovsdb.Operation, error) {
	return c.updateIPBlockACLOps(pgName, protocol, aclName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, "ipBlock ingress", ipBlocks, npp, logEnable, logACLActions, logRate, namedPortMap, false)
}

func (c *OVNNbClient) UpdateEgressIPBlockACLOps(pgName, protocol, aclName string, ipBlocks []netv1.IPBlock, npp []netv1.NetworkPolicyPort, logEnable bool, logACLActions []ovnnb.ACLAction, logRate int, namedPortMap map[string]*util.NamedPortInfo) ([]ovsdb.Operation, error) {
	return c.updateIPBlockACLOps(pgName, protocol, aclName, ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, "ipBlock egress", ipBlocks, npp, logEnable, logACLActions, logRate, namedPortMap, true)
}

func aclFilter(direction string, externalIDs map[string]string) func(acl *ovnnb.ACL) bool {
	return func(acl *ovnnb.ACL) bool {
		if !matchExternalIDs(acl.ExternalIDs, externalIDs) {
			return false
		}
		return len(direction) == 0 || acl.Direction == direction
	}
}

func (c *OVNNbClient) CreateAclsOps(parentName, parentType string, acls ...*ovnnb.ACL) ([]ovsdb.Operation, error) {
	if parentType != portGroupKey && parentType != LogicalSwitchKey {
		return nil, fmt.Errorf("acl parent type must be '%s' or '%s'", portGroupKey, LogicalSwitchKey)
	}

	if len(acls) == 0 {
		return nil, nil
	}

	models, aclUUIDs := modelsAndUUIDs(acls, func(acl *ovnnb.ACL) string { return acl.UUID })
	ops, err := createAndAttachOps(c, &ovnnb.ACL{}, models, func(uuids []string) ([]ovsdb.Operation, error) {
		return c.parentUpdateACLOp(parentName, parentType, uuids, ovsdb.MutateOperationInsert)
	}, aclUUIDs)
	if err != nil {
		return nil, logWrap(err, wrapErr("generate operations for creating acls: %w"))
	}
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
		return nil, logWrap(err, wrapErr("list type %s %s acls: %w", parentType, parentName))
	}

	aclUUIDs := make([]string, 0, len(acls))
	for _, acl := range acls {
		aclUUIDs = append(aclUUIDs, acl.UUID)
	}

	removeACLOp, err := c.parentUpdateACLOp(parentName, parentType, aclUUIDs, ovsdb.MutateOperationDelete)
	if err != nil {
		klog.Error(err)
		kind := "logical switch"
		if parentType == portGroupKey {
			kind = "port group"
		}
		return nil, fmt.Errorf("generate operations for deleting acls from %s %s: %w", kind, parentName, err)
	}
	return removeACLOp, nil
}

// sgRuleNoACL check if security group rule has acl in a tier
func (c *OVNNbClient) sgRuleNoACL(sgName, direction string, rule kubeovnv1.SecurityGroupRule, tier int) (bool, error) {
	pgName, match := sgRuleACLMatch(sgName, direction, rule)
	exists, err := c.ACLExists(pgName, direction, sgRuleACLPriority(rule), match.String(), tier)
	if err != nil {
		return false, logFmt("failed to check acl rule for security group %s: %w", sgName, err)
	}
	return !exists, nil
}

// SGLostACL check if security group lost an acl
func (c *OVNNbClient) sgRulesLostACL(sg *kubeovnv1.SecurityGroup, rules []kubeovnv1.SecurityGroupRule, direction, kind string) (bool, error) {
	for _, rule := range rules {
		no, err := c.sgRuleNoACL(sg.Name, direction, rule, util.ConvertSGTierToOvnTier(sg.Spec.Tier))
		if err != nil {
			klog.Error(err)
			return false, err
		}
		if no {
			klog.Infof("security group %s lost %s rule: %v", sg.Name, kind, rule)
			return true, nil
		}
	}
	return false, nil
}

func (c *OVNNbClient) SGLostACL(sg *kubeovnv1.SecurityGroup) (bool, error) {
	lost, err := c.sgRulesLostACL(sg, sg.Spec.IngressRules, ovnnb.ACLDirectionToLport, "ingress")
	if err != nil || lost {
		return lost, err
	}
	return c.sgRulesLostACL(sg, sg.Spec.EgressRules, ovnnb.ACLDirectionFromLport, "egress")
}

type namedACLPort struct {
	protocol   string
	equalPort  *int
	rangeStart *int
	rangeEnd   *int
}

func namedACLDirection(isIngress bool) ovnnb.ACLDirection {
	if isIngress {
		return ovnnb.ACLDirectionToLport
	}
	return ovnnb.ACLDirectionFromLport
}

func namedACLOptions(aclName, pgName string, aclAction ovnnb.ACLAction, logACLActions []ovnnb.ACLAction) func(*ovnnb.ACL) {
	return func(acl *ovnnb.ACL) {
		setACLName(acl, aclName)
		if acl.ExternalIDs == nil {
			acl.ExternalIDs = make(map[string]string)
		}
		acl.ExternalIDs[aclParentKey] = pgName
		setACLApplyAfterLB(acl)
		if slices.Contains(logACLActions, aclAction) {
			acl.Log = true
			if aclAction == ovnnb.ACLActionDrop {
				acl.Severity = ptr.To(ovnnb.ACLSeverityWarning)
			}
		}
	}
}

func (c *OVNNbClient) createNamedRuleACLOps(pgName, aclName string, priority int, aclAction ovnnb.ACLAction, logACLActions []ovnnb.ACLAction, tier int, isIngress bool, matches []string) ([]ovsdb.Operation, error) {
	return c.createACLOpsFromMatches(pgName, namedACLDirection(isIngress), strconv.Itoa(priority), aclAction, tier, matches, namedACLOptions(aclName, pgName, aclAction, logACLActions), func(err error) error {
		return fmt.Errorf("new acl for port group %s: %w", pgName, err)
	})
}

func newNamedACLMatch(pgName, asName, protocol, direction, warnKind string, ports []namedACLPort) []string {
	ipSuffix := aclIPSuffix(protocol)
	srcOrDst, portDirection := aclSrcOrDst(direction), aclPortDirection(direction)
	selectIPMatch := NewAndACLMatch(
		aclPGIPMatch(pgName, portDirection),
		NewACLMatch(ipSuffix+"."+srcOrDst, "==", "$"+asName, ""),
	)
	if len(ports) == 0 {
		return []string{selectIPMatch.String()}
	}
	matches := make([]string, 0, len(ports))
	for _, port := range ports {
		switch {
		case port.equalPort != nil:
			matches = append(matches, NewAndACLMatch(selectIPMatch, NewACLMatch(port.protocol+".dst", "==", strconv.Itoa(*port.equalPort), "")).String())
		case port.rangeStart != nil && port.rangeEnd != nil:
			matches = append(matches, NewAndACLMatch(selectIPMatch, NewACLMatch(port.protocol+".dst", "<=", strconv.Itoa(*port.rangeStart), strconv.Itoa(*port.rangeEnd))).String())
		default:
			klog.Errorf("failed to check port for %s ingress rule, pg %s, as %s", warnKind, pgName, asName)
		}
	}
	return matches
}

// UpdateAnpRuleACLOps return operation that creates an ingress/egress ACL
func (c *OVNNbClient) UpdateAnpRuleACLOps(pgName, asName, protocol, aclName string, priority int, aclAction ovnnb.ACLAction, logACLActions []ovnnb.ACLAction, rulePorts []v1alpha1.AdminNetworkPolicyPort, isIngress, isBanp bool) ([]ovsdb.Operation, error) {
	tier := util.AnpACLTier
	if isBanp {
		tier = util.BanpACLTier
	}
	matches := newAnpACLMatch(pgName, asName, protocol, namedACLDirection(isIngress), rulePorts)
	return c.createNamedRuleACLOps(pgName, aclName, priority, aclAction, logACLActions, tier, isIngress, matches)
}

func (c *OVNNbClient) UpdateCnpRuleACLOps(pgName, asName, protocol, aclName string, priority int, aclAction ovnnb.ACLAction, logACLActions []ovnnb.ACLAction, rulePorts []v1alpha2.ClusterNetworkPolicyPort, isIngress bool, tier int) ([]ovsdb.Operation, error) {
	matches := newCnpACLMatch(pgName, asName, protocol, namedACLDirection(isIngress), rulePorts)
	return c.createNamedRuleACLOps(pgName, aclName, priority, aclAction, logACLActions, tier, isIngress, matches)
}

func namedACLEqualPort(protocol string, port int) namedACLPort {
	value := port
	return namedACLPort{protocol: strings.ToLower(protocol), equalPort: &value}
}

func namedACLRangePort(protocol string, start, end int) namedACLPort {
	s, e := start, end
	return namedACLPort{protocol: strings.ToLower(protocol), rangeStart: &s, rangeEnd: &e}
}

func namedACLPorts[T any](rulePorts []T, convert func(T) namedACLPort) []namedACLPort {
	ports := make([]namedACLPort, len(rulePorts))
	for i, port := range rulePorts {
		ports[i] = convert(port)
	}
	return ports
}

func anpPorts(rulePorts []v1alpha1.AdminNetworkPolicyPort) []namedACLPort {
	return namedACLPorts(rulePorts, func(port v1alpha1.AdminNetworkPolicyPort) namedACLPort {
		switch {
		case port.PortNumber != nil:
			return namedACLEqualPort(string(port.PortNumber.Protocol), int(port.PortNumber.Port))
		case port.PortRange != nil:
			return namedACLRangePort(string(port.PortRange.Protocol), int(port.PortRange.Start), int(port.PortRange.End))
		default:
			return namedACLPort{}
		}
	})
}

func cnpPorts(rulePorts []v1alpha2.ClusterNetworkPolicyPort) []namedACLPort {
	return namedACLPorts(rulePorts, func(port v1alpha2.ClusterNetworkPolicyPort) namedACLPort {
		switch {
		case port.PortNumber != nil:
			return namedACLEqualPort(string(port.PortNumber.Protocol), int(port.PortNumber.Port))
		case port.PortRange != nil:
			return namedACLRangePort(string(port.PortRange.Protocol), int(port.PortRange.Start), int(port.PortRange.End))
		default:
			return namedACLPort{}
		}
	})
}

func newAnpACLMatch(pgName, asName, protocol, direction string, rulePorts []v1alpha1.AdminNetworkPolicyPort) []string {
	return newNamedACLMatch(pgName, asName, protocol, direction, "anp", anpPorts(rulePorts))
}

func newCnpACLMatch(pgName, asName, protocol, direction string, rulePorts []v1alpha2.ClusterNetworkPolicyPort) []string {
	return newNamedACLMatch(pgName, asName, protocol, direction, "cnp", cnpPorts(rulePorts))
}

func (c *OVNNbClient) MigrateACLTier() error {
	aclList, err := filterTimeout(c.Database, &ovnnb.ACL{}, func(acl *ovnnb.ACL) bool { return acl.Tier == 0 })
	if err != nil {
		return logFmt("failed to list acls with tier 0: %w", err)
	}

	ops := make([]ovsdb.Operation, 0, len(aclList))
	for _, acl := range aclList {
		acl.Tier = util.NetpolACLTier
		op, err := c.Database.Table(&ovnnb.ACL{}).UpdateOps(&acl, &acl, &acl.Tier)
		if err != nil {
			return logWrap(err, wrapErr("failed to generate operations for updating acl %s tier: %w", acl.UUID))
		}
		ops = append(ops, op...)
	}
	if err := transactOps(c, "acl-migrate-tier", ops); err != nil {
		return logWrap(err, wrapErr("failed to migrate acl tier: %w"))
	}
	return nil
}

func appendParentACLDeletes[T any](c *OVNNbClient, prototype model.Model, pred func(*T) bool, nameOf func(*T) string, update func(string, []string, ovsdb.Mutator) ([]ovsdb.Operation, error), aclUUID string, ops []ovsdb.Operation) []ovsdb.Operation {
	rows, err := filterTimeout(c.Database, prototype, pred)
	if err != nil {
		return ops
	}
	for i := range rows {
		op, err := update(nameOf(&rows[i]), []string{aclUUID}, ovsdb.MutateOperationDelete)
		if err == nil {
			ops = append(ops, op...)
		}
	}
	return ops
}

func (c *OVNNbClient) CleanNoParentKeyAcls() error {
	aclList, err := filterTimeout(c.Database, &ovnnb.ACL{}, func(acl *ovnnb.ACL) bool {
		// Only clean ACLs that belong to kube-ovn (vendor=kube-ovn) but are missing the parent key.
		if !hasVendor(acl.ExternalIDs) {
			return false
		}
		_, hasParent := acl.ExternalIDs[aclParentKey]
		return !hasParent
	})
	if err != nil {
		return logFmt("failed to list kube-ovn acls without parent: %w", err)
	}

	ops := make([]ovsdb.Operation, 0, len(aclList))
	for _, acl := range aclList {
		ops = appendParentACLDeletes(c, &ovnnb.PortGroup{}, func(pg *ovnnb.PortGroup) bool {
			return slices.Contains(pg.ACLs, acl.UUID)
		}, func(pg *ovnnb.PortGroup) string { return pg.Name }, c.portGroupUpdateACLOp, acl.UUID, ops)
		ops = appendParentACLDeletes(c, &ovnnb.LogicalSwitch{}, func(ls *ovnnb.LogicalSwitch) bool {
			return slices.Contains(ls.ACLs, acl.UUID)
		}, func(ls *ovnnb.LogicalSwitch) string { return ls.Name }, c.logicalSwitchUpdateACLOp, acl.UUID, ops)
		delOp, err := c.Database.Table(&ovnnb.ACL{}).DeleteOps(&acl)
		if err == nil {
			ops = append(ops, delOp...)
		}
	}
	if len(ops) == 0 {
		return nil
	}

	return c.transactGenerated("acl-clean-no-parent", ops, nil, nil,
		wrapErr("failed to clean kube-ovn acls without parent: %w"),
	)
}
