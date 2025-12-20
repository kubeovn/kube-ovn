package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/kubeovn/felix/ipsets"
	"github.com/kubeovn/go-iptables/iptables"
	"github.com/scylladb/go-set/strset"
	"github.com/vishvananda/netlink"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	"k8s.io/utils/set"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	ServiceSet                 = "services"
	SubnetSet                  = "subnets"
	SubnetNatSet               = "subnets-nat"
	SubnetDistributedGwSet     = "subnets-distributed-gw"
	LocalPodSet                = "local-pod-ip-nat"
	OtherNodeSet               = "other-node"
	IPSetPrefix                = "ovn"
	NatOutGoingPolicySubnetSet = "subnets-nat-policy"
	NatOutGoingPolicyRuleSet   = "natpr-"
)

const (
	NAT                        = util.NAT
	MANGLE                     = util.Mangle
	Prerouting                 = util.Prerouting
	Postrouting                = util.Postrouting
	Output                     = util.Output
	OvnPrerouting              = util.OvnPrerouting
	OvnPostrouting             = util.OvnPostrouting
	OvnOutput                  = util.OvnOutput
	OvnMasquerade              = util.OvnMasquerade
	OvnNatOutGoingPolicy       = util.OvnNatOutGoingPolicy
	OvnNatOutGoingPolicySubnet = util.OvnNatOutGoingPolicySubnet
)

const (
	OnOutGoingNatMark     = "0x90001/0x90001"
	OnOutGoingForwardMark = "0x90002/0x90002"
	TProxyOutputMark      = util.TProxyOutputMark
	TProxyOutputMask      = util.TProxyOutputMask
	TProxyPreroutingMark  = util.TProxyPreroutingMark
	TProxyPreroutingMask  = util.TProxyPreroutingMask
)

var (
	tProxyOutputMarkMask     = fmt.Sprintf("%#x/%#x", TProxyOutputMark, TProxyOutputMask)
	tProxyPreRoutingMarkMask = fmt.Sprintf("%#x/%#x", TProxyPreroutingMark, TProxyPreroutingMask)
)

type policyRouteMeta struct {
	family   int
	source   string
	gateway  string
	tableID  uint32
	priority uint32
}

func (c *Controller) setIPSet() error {
	protocols := make([]string, 2)
	if c.protocol == kubeovnv1.ProtocolDual {
		protocols[0] = kubeovnv1.ProtocolIPv4
		protocols[1] = kubeovnv1.ProtocolIPv6
	} else {
		protocols[0] = c.protocol
	}

	for _, protocol := range protocols {
		if c.ipsets[protocol] == nil {
			continue
		}
		services := c.getServicesCIDR(protocol)
		subnets, _, err := c.getDefaultVpcSubnetsCIDR(protocol)
		if err != nil {
			klog.Errorf("get subnets failed, %+v", err)
			return err
		}
		subnetsNeedNat, err := c.getSubnetsNeedNAT(protocol)
		if err != nil {
			klog.Errorf("get need nat subnets failed, %+v", err)
			return err
		}
		subnetsDistributedGateway, err := c.getSubnetsDistributedGateway(protocol)
		if err != nil {
			klog.Errorf("failed to get subnets with centralized gateway: %v", err)
			return err
		}
		otherNode, err := c.getOtherNodes(protocol)
		if err != nil {
			klog.Errorf("failed to get node, %+v", err)
			return err
		}
		c.ipsets[protocol].AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   ServiceSet,
			Type:    ipsets.IPSetTypeHashNet,
		}, services)
		c.ipsets[protocol].AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   SubnetSet,
			Type:    ipsets.IPSetTypeHashNet,
		}, subnets)
		c.ipsets[protocol].AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   LocalPodSet,
			Type:    ipsets.IPSetTypeHashIP,
		}, nil)
		c.ipsets[protocol].AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   SubnetNatSet,
			Type:    ipsets.IPSetTypeHashNet,
		}, subnetsNeedNat)
		c.ipsets[protocol].AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   SubnetDistributedGwSet,
			Type:    ipsets.IPSetTypeHashNet,
		}, subnetsDistributedGateway)
		c.ipsets[protocol].AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   OtherNodeSet,
			Type:    ipsets.IPSetTypeHashNet,
		}, otherNode)
		c.reconcileNatOutGoingPolicyIPset(protocol)
		c.ipsets[protocol].ApplyUpdates()
	}
	return nil
}

func (c *Controller) gcIPSet() {
	protocols := make([]string, 2)
	if c.protocol == kubeovnv1.ProtocolDual {
		protocols[0] = kubeovnv1.ProtocolIPv4
		protocols[1] = kubeovnv1.ProtocolIPv6
	} else {
		protocols[0] = c.protocol
	}

	for _, protocol := range protocols {
		if c.ipsets[protocol] == nil {
			continue
		}
		c.ipsets[protocol].ApplyDeletions()
	}
}

func (c *Controller) addNatOutGoingPolicyRuleIPset(rule kubeovnv1.NatOutgoingPolicyRuleStatus, protocol string) {
	if rule.Match.SrcIPs != "" {
		ipsetName := getNatOutGoingPolicyRuleIPSetName(rule.RuleID, "src", "", false)
		c.ipsets[protocol].AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   ipsetName,
			Type:    ipsets.IPSetTypeHashNet,
		}, strings.Split(rule.Match.SrcIPs, ","))
	}

	if rule.Match.DstIPs != "" {
		ipsetName := getNatOutGoingPolicyRuleIPSetName(rule.RuleID, "dst", "", false)
		c.ipsets[protocol].AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   ipsetName,
			Type:    ipsets.IPSetTypeHashNet,
		}, strings.Split(rule.Match.DstIPs, ","))
	}
}

func (c *Controller) removeNatOutGoingPolicyRuleIPset(protocol string, natPolicyRuleIDs *strset.Set) {
	sets, err := c.k8sipsets.ListSets()
	if err != nil {
		klog.Errorf("failed to list ipsets: %v", err)
		return
	}
	for _, set := range sets {
		if isNatOutGoingPolicyRuleIPSet(set) {
			ruleID, _ := getNatOutGoingPolicyRuleIPSetItem(set)
			if !natPolicyRuleIDs.Has(ruleID) {
				c.ipsets[protocol].RemoveIPSet(formatIPsetUnPrefix(set))
			}
		}
	}
}

func (c *Controller) reconcileNatOutGoingPolicyIPset(protocol string) {
	subnets, err := c.getSubnetsNatOutGoingPolicy(protocol)
	if err != nil {
		klog.Errorf("failed to get subnets with NAT outgoing policy rule: %v", err)
		return
	}

	subnetCidrs := make([]string, 0, len(subnets))
	natPolicyRuleIDs := strset.New()
	for _, subnet := range subnets {
		cidrBlock, err := getCidrByProtocol(subnet.Spec.CIDRBlock, protocol)
		if err != nil {
			klog.Errorf("failed to get subnet %s CIDR block by protocol: %v", subnet.Name, err)
			continue
		}
		if cidrBlock != "" {
			subnetCidrs = append(subnetCidrs, cidrBlock)
		}
		for _, rule := range subnet.Status.NatOutgoingPolicyRules {
			if rule.RuleID == "" {
				klog.Errorf("unexpected empty ID for NAT outgoing rule %q of subnet %s", rule.NatOutgoingPolicyRule, subnet.Name)
				continue
			}
			natPolicyRuleIDs.Add(rule.RuleID)
			c.addNatOutGoingPolicyRuleIPset(rule, protocol)
		}
	}

	c.ipsets[protocol].AddOrReplaceIPSet(ipsets.IPSetMetadata{
		MaxSize: 1048576,
		SetID:   NatOutGoingPolicySubnetSet,
		Type:    ipsets.IPSetTypeHashNet,
	}, subnetCidrs)

	c.removeNatOutGoingPolicyRuleIPset(protocol, natPolicyRuleIDs)
}

func (c *Controller) setPolicyRouting() error {
	protocols := make([]string, 2)
	if c.protocol == kubeovnv1.ProtocolDual {
		protocols[0] = kubeovnv1.ProtocolIPv4
		protocols[1] = kubeovnv1.ProtocolIPv6
	} else {
		protocols[0] = c.protocol
	}

	for _, protocol := range protocols {
		if c.ipsets[protocol] == nil {
			continue
		}

		localPodIPs, err := c.getLocalPodIPsNeedPR(protocol)
		if err != nil {
			klog.Errorf("failed to get local pod ips failed: %+v", err)
			return err
		}
		subnetsNeedPR, err := c.getSubnetsNeedPR(protocol)
		if err != nil {
			klog.Errorf("failed to get subnets that need policy routing: %+v", err)
			return err
		}

		family, err := util.ProtocolToFamily(protocol)
		if err != nil {
			klog.Error(err)
			return err
		}

		for meta, ips := range localPodIPs {
			if err = c.addPolicyRouting(family, meta.gateway, meta.priority, meta.tableID, ips...); err != nil {
				klog.Errorf("failed to add policy routing for local pods: %+v", err)
				return err
			}
		}
		for meta, cidr := range subnetsNeedPR {
			if err = c.addPolicyRouting(family, meta.gateway, meta.priority, meta.tableID, cidr); err != nil {
				klog.Errorf("failed to add policy routing for subnet: %+v", err)
				return err
			}
		}
	}

	return nil
}

func (c *Controller) addPodPolicyRouting(podProtocol, externalEgressGateway string, priority, tableID uint32, ips []string) error {
	egw := strings.Split(externalEgressGateway, ",")
	prMetas := make([]policyRouteMeta, 0, 2)
	if len(egw) == 1 {
		family, _ := util.ProtocolToFamily(util.CheckProtocol(egw[0]))
		if family == netlink.FAMILY_V4 || podProtocol != kubeovnv1.ProtocolDual {
			prMetas = append(prMetas, policyRouteMeta{family: family, source: ips[0], gateway: egw[0]})
		} else {
			prMetas = append(prMetas, policyRouteMeta{family: family, source: ips[1], gateway: egw[0]})
		}
	} else {
		prMetas = append(prMetas, policyRouteMeta{family: netlink.FAMILY_V4, source: ips[0], gateway: egw[0]})
		prMetas = append(prMetas, policyRouteMeta{family: netlink.FAMILY_V6, source: ips[1], gateway: egw[1]})
	}

	for _, meta := range prMetas {
		if err := c.addPolicyRouting(meta.family, meta.gateway, priority, tableID, meta.source); err != nil {
			klog.Errorf("failed to add policy routing for pod: %+v", err)
			return err
		}
	}

	return nil
}

func (c *Controller) deletePodPolicyRouting(podProtocol, externalEgressGateway string, priority, tableID uint32, ips []string) error {
	egw := strings.Split(externalEgressGateway, ",")
	prMetas := make([]policyRouteMeta, 0, 2)
	if len(egw) == 1 {
		family, _ := util.ProtocolToFamily(util.CheckProtocol(egw[0]))
		if family == netlink.FAMILY_V4 || podProtocol != kubeovnv1.ProtocolDual {
			prMetas = append(prMetas, policyRouteMeta{family: family, source: ips[0], gateway: egw[0]})
		} else {
			prMetas = append(prMetas, policyRouteMeta{family: family, source: ips[1], gateway: egw[0]})
		}
	} else {
		prMetas = append(prMetas, policyRouteMeta{family: netlink.FAMILY_V4, source: ips[0], gateway: egw[0]})
		prMetas = append(prMetas, policyRouteMeta{family: netlink.FAMILY_V6, source: ips[1], gateway: egw[1]})
	}

	for _, meta := range prMetas {
		if err := c.deletePolicyRouting(meta.family, meta.gateway, priority, tableID, meta.source); err != nil {
			klog.Errorf("failed to delete policy routing for pod: %+v", err)
			return err
		}
	}

	return nil
}

func (c *Controller) addPolicyRouting(family int, gateway string, priority, tableID uint32, ips ...string) error {
	route := &netlink.Route{
		Protocol: netlink.RouteProtocol(syscall.RTPROT_STATIC),
		Gw:       net.ParseIP(gateway),
		Table:    int(tableID),
	}
	if err := netlink.RouteReplace(route); err != nil && !errors.Is(err, syscall.EEXIST) {
		err = fmt.Errorf("failed to replace route in table %d: %w", tableID, err)
		klog.Error(err)
		return err
	}

	maskBits := 32
	if family == netlink.FAMILY_V6 {
		maskBits = 128
	}

	rule := netlink.NewRule()
	rule.Family = family
	rule.Table = int(tableID)
	rule.Priority = int(priority)
	mask := net.CIDRMask(maskBits, maskBits)

	for _, ip := range ips {
		if strings.ContainsRune(ip, '/') {
			var err error
			if rule.Src, err = netlink.ParseIPNet(ip); err != nil {
				klog.Errorf("unexpected CIDR: %s", ip)
				err = fmt.Errorf("failed to add route in table %d: %w", tableID, err)
				klog.Error(err)
				return err
			}
		} else {
			rule.Src = &net.IPNet{IP: net.ParseIP(ip), Mask: mask}
		}

		if err := netlink.RuleAdd(rule); err != nil && !errors.Is(err, syscall.EEXIST) {
			err = fmt.Errorf("failed to add network rule: %w", err)
			klog.Error(err)
			return err
		}
	}

	return nil
}

func (c *Controller) deletePolicyRouting(family int, _ string, priority, tableID uint32, ips ...string) error {
	maskBits := 32
	if family == netlink.FAMILY_V6 {
		maskBits = 128
	}

	rule := netlink.NewRule()
	rule.Family = family
	rule.Table = int(tableID)
	rule.Priority = int(priority)
	mask := net.CIDRMask(maskBits, maskBits)

	for _, ip := range ips {
		if strings.ContainsRune(ip, '/') {
			var err error
			if rule.Src, err = netlink.ParseIPNet(ip); err != nil {
				klog.Errorf("unexpected CIDR: %s", ip)
				err = fmt.Errorf("failed to delete route in table %d: %w", tableID, err)
				klog.Error(err)
				return err
			}
		} else {
			rule.Src = &net.IPNet{IP: net.ParseIP(ip), Mask: mask}
		}

		if err := netlink.RuleDel(rule); err != nil && !errors.Is(err, syscall.ENOENT) {
			err = fmt.Errorf("failed to delete network rule: %w", err)
			klog.Error(err)
			return err
		}
	}

	// routes may be used by other Pods so delete rules only
	return nil
}

// findRulePositions locates all occurrences of a rule in the specified table and chain,
// searching from lowest to highest priority (bottom to top).
// Returns all matching position indices, or an empty slice if none found.
func findRulePositions(ipt *iptables.IPTables, rule util.IPTableRule) ([]int, error) {
	rules, err := ipt.List(rule.Table, rule.Chain)
	if err != nil {
		return nil, fmt.Errorf("failed to list iptables rules: %w", err)
	}

	positions := make([]int, 0)

	// Start from the end of the list (lowest priority) and go up to 1 (but skip index 0)
	for i := len(rules) - 1; i >= 1; i-- {
		ruleSpec := util.DoubleQuotedFields(rules[i])
		if len(ruleSpec) < 3 {
			continue
		}

		if slices.Equal(ruleSpec[2:], rule.Rule) {
			positions = append(positions, i)
		}
	}

	return positions, nil
}

// ensureNatPreroutingRulePosition ensures that the nat prerouting rule has a higher priority than the kube-proxy rule.
func ensureNatPreroutingRulePosition(ipt *iptables.IPTables, rule util.IPTableRule) error {
	kubeProxyRule := util.IPTableRule{
		Table: "nat",
		Chain: "PREROUTING",
		Rule:  []string{"-m", "comment", "--comment", "kubernetes service portals", "-j", "KUBE-SERVICES"},
	}
	kubeProxyPosList, err := findRulePositions(ipt, kubeProxyRule)
	if err != nil {
		return fmt.Errorf("failed to find kube-proxy rule position: %w", err)
	}

	insertPosition := 1
	if len(kubeProxyPosList) > 0 {
		insertPosition = kubeProxyPosList[len(kubeProxyPosList)-1]
	}

	// Check if the rule already exists at a higher priority than the kube-proxy rule
	existingNatPreroutingPosList, err := findRulePositions(ipt, rule)
	if err != nil {
		return fmt.Errorf("failed to find nat prerouting rule position: %w", err)
	}
	if len(existingNatPreroutingPosList) > 0 {
		pos := existingNatPreroutingPosList[len(existingNatPreroutingPosList)-1]
		if pos <= insertPosition {
			klog.V(5).Infof("nat prerouting rule already exists at position %d", pos)
			return nil
		}
	}

	klog.Infof("inserting nat prerouting rule %q at position %d", rule.Rule, insertPosition)
	return ipt.Insert(rule.Table, rule.Chain, insertPosition, rule.Rule...)
}

// ensureNatPreroutingRuleNoDuplicate ensures that there are no duplicate nat prerouting rules.
func ensureNatPreroutingRuleNoDuplicate(ipt *iptables.IPTables, rule util.IPTableRule) error {
	existingNatPreroutingPosList, err := findRulePositions(ipt, rule)
	if err != nil {
		return fmt.Errorf("failed to find nat prerouting rule position: %w", err)
	}
	if len(existingNatPreroutingPosList) == 0 {
		klog.Warningf("nat prerouting rule not found, skipping duplicate check")
		return nil
	}

	// Delete all but the top priority (highest) rule
	// NOTE: There's a race condition here as iptables rules could be modified by other processes
	// between our findRulePositions call and the Delete operations below. Since iptables lacks
	// a transaction mechanism, rule positions might change, potentially causing us to delete
	// incorrect rules. This is an accepted limitation of the current implementation.
	for _, pos := range existingNatPreroutingPosList[:len(existingNatPreroutingPosList)-1] {
		klog.Infof("deleting duplicate nat prerouting rule at position %d", pos)
		if err := ipt.Delete(rule.Table, rule.Chain, strconv.Itoa(pos)); err != nil {
			return fmt.Errorf("failed to delete duplicate nat prerouting rule: %w", err)
		}
	}

	return nil
}

// ensureNatPreroutingRule ensures that the nat prerouting rule is in the right position and no duplicate exists.
func ensureNatPreroutingRule(ipt *iptables.IPTables, rule util.IPTableRule) error {
	klog.V(3).Infof("ensure nat prerouting rule %q", rule.Rule)

	if err := ensureNatPreroutingRulePosition(ipt, rule); err != nil {
		return fmt.Errorf("failed to ensure nat prerouting rule position: %w", err)
	}

	if err := ensureNatPreroutingRuleNoDuplicate(ipt, rule); err != nil {
		return fmt.Errorf("failed to ensure nat prerouting rule no duplicate: %w", err)
	}

	return nil
}

func (c *Controller) createIptablesRule(ipt *iptables.IPTables, rule util.IPTableRule) error {
	exists, err := ipt.Exists(rule.Table, rule.Chain, rule.Rule...)
	if err != nil {
		klog.Errorf("failed to check iptables rule existence: %v", err)
		return err
	}

	s := strings.Join(rule.Rule, " ")
	if exists {
		if rule.Table == NAT && rule.Chain == Prerouting {
			return ensureNatPreroutingRule(ipt, rule)
		}
		return nil
	}

	klog.Infof("creating iptables rule in table %s chain %s at position %d: %q", rule.Table, rule.Chain, 1, s)
	if err = ipt.Insert(rule.Table, rule.Chain, 1, rule.Rule...); err != nil {
		klog.Errorf(`failed to insert iptables rule "%s": %v`, s, err)
		return err
	}

	return nil
}

func (c *Controller) updateIptablesChain(ipt *iptables.IPTables, table, chain, parent string, rules []util.IPTableRule) error {
	ok, err := ipt.ChainExists(table, chain)
	if err != nil {
		klog.Errorf("failed to check existence of iptables chain %s in table %s: %v", chain, table, err)
		return err
	}
	if !ok {
		if err = ipt.NewChain(table, chain); err != nil {
			klog.Errorf("failed to create iptables chain %s in table %s: %v", chain, table, err)
			return err
		}
		klog.Infof("created iptables chain %s in table %s", chain, table)
	}
	if parent != "" {
		comment := fmt.Sprintf("kube-ovn %s rules", strings.ToLower(parent))
		rule := util.IPTableRule{
			Table: table,
			Chain: parent,
			Rule:  []string{"-m", "comment", "--comment", comment, "-j", chain},
		}
		if err = c.createIptablesRule(ipt, rule); err != nil {
			klog.Errorf("failed to create iptables rule: %v", err)
			return err
		}
	}

	// list existing rules
	ruleList, err := ipt.List(table, chain)
	if err != nil {
		klog.Errorf("failed to list iptables rules in chain %s/%s: %v", table, chain, err)
		return err
	}

	// filter the heading default chain policy: -N OVN-POSTROUTING
	ruleList = ruleList[1:]

	// trim prefix: "-A OVN-POSTROUTING "
	prefixLen := 4 + len(chain)
	existingRules := make([][]string, 0, len(ruleList))
	for _, r := range ruleList {
		existingRules = append(existingRules, util.DoubleQuotedFields(r[prefixLen:]))
	}

	var added int
	for i, rule := range rules {
		if i-added < len(existingRules) && slices.Equal(existingRules[i-added], rule.Rule) {
			klog.V(5).Infof("iptables rule %v already exists", rule.Rule)
			continue
		}
		klog.Infof("creating iptables rule in table %s chain %s at position %d: %q", table, chain, i+1, strings.Join(rule.Rule, " "))
		if err = ipt.Insert(table, chain, i+1, rule.Rule...); err != nil {
			klog.Errorf(`failed to insert iptables rule %v: %v`, rule.Rule, err)
			return err
		}
		added++
	}
	for i := len(existingRules) - 1; i >= len(rules)-added; i-- {
		if err = ipt.Delete(table, chain, strconv.Itoa(i+added+1)); err != nil {
			klog.Errorf(`failed to delete iptables rule %v: %v`, existingRules[i], err)
			return err
		}
		klog.Infof("deleted iptables rule in table %s chain %s: %q", table, chain, strings.Join(existingRules[i], " "))
	}

	return nil
}

func (c *Controller) setIptables() error {
	klog.V(3).Infoln("start to set up iptables")
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node %s, %v", c.config.NodeName, err)
		return err
	}

	nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(*node)
	nodeIPs := map[string]string{
		kubeovnv1.ProtocolIPv4: nodeIPv4,
		kubeovnv1.ProtocolIPv6: nodeIPv6,
	}

	centralGwNatIPs, err := c.getEgressNatIPByNode(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get centralized subnets nat ips on node %s, %v", c.config.NodeName, err)
		return err
	}
	klog.V(3).Infof("centralized subnets nat ips %v", centralGwNatIPs)

	var (
		v4Rules = []util.IPTableRule{
			// mark packets from pod to service
			{Table: NAT, Chain: OvnPrerouting, Rule: strings.Fields(`-i ovn0 -m set --match-set ovn40subnets src -m set --match-set ovn40services dst -j MARK --set-xmark 0x4000/0x4000`)},
			// nat packets marked by kube-proxy or kube-ovn
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m mark --mark 0x4000/0x4000 -j ` + OvnMasquerade)},
			// nat service traffic
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m set --match-set ovn40subnets src -m set --match-set ovn40subnets dst -j ` + OvnMasquerade)},
			// do not nat node port service traffic with external traffic policy set to local
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m mark --mark 0x80000/0x80000 -m set --match-set ovn40subnets-distributed-gw dst -j RETURN`)},
			// nat node port service traffic with external traffic policy set to local for subnets with centralized gateway
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m mark --mark 0x80000/0x80000 -j ` + OvnMasquerade)},
			// do not nat reply packets in direct routing
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-p tcp -m tcp --tcp-flags SYN NONE -m conntrack --ctstate NEW -j RETURN`)},
			// do not nat route traffic
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m set ! --match-set ovn40subnets src -m set ! --match-set ovn40other-node src -m set --match-set ovn40subnets-nat dst -j RETURN`)},
			// nat outgoing policy rules
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields("-m set --match-set ovn40subnets-nat-policy src -m set ! --match-set ovn40subnets dst -j " + OvnNatOutGoingPolicy)},
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(fmt.Sprintf(`-m mark --mark %s -j %s`, OnOutGoingNatMark, OvnMasquerade))},
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(fmt.Sprintf(`-m mark --mark %s -j RETURN`, OnOutGoingForwardMark))},
			// default nat outgoing rules
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m set --match-set ovn40subnets-nat src -m set ! --match-set ovn40subnets dst -j ` + OvnMasquerade)},
			// clear mark
			{Table: NAT, Chain: OvnMasquerade, Rule: strings.Fields(`-j MARK --set-xmark 0x0/0xffffffff`)},
			// do masquerade
			{Table: NAT, Chain: OvnMasquerade, Rule: strings.Fields(`-j MASQUERADE`)},
			// Input Accept
			{Table: "filter", Chain: "INPUT", Rule: strings.Fields(`-m set --match-set ovn40subnets src -j ACCEPT`)},
			{Table: "filter", Chain: "INPUT", Rule: strings.Fields(`-m set --match-set ovn40subnets dst -j ACCEPT`)},
			{Table: "filter", Chain: "INPUT", Rule: strings.Fields(`-m set --match-set ovn40services src -j ACCEPT`)},
			{Table: "filter", Chain: "INPUT", Rule: strings.Fields(`-m set --match-set ovn40services dst -j ACCEPT`)},
			// Forward Accept
			{Table: "filter", Chain: "FORWARD", Rule: strings.Fields(`-m set --match-set ovn40subnets src -j ACCEPT`)},
			{Table: "filter", Chain: "FORWARD", Rule: strings.Fields(`-m set --match-set ovn40subnets dst -j ACCEPT`)},
			{Table: "filter", Chain: "FORWARD", Rule: strings.Fields(`-m set --match-set ovn40services src -j ACCEPT`)},
			{Table: "filter", Chain: "FORWARD", Rule: strings.Fields(`-m set --match-set ovn40services dst -j ACCEPT`)},
			// Output unmark to bypass kernel nat checksum issue https://github.com/flannel-io/flannel/issues/1279
			{Table: "filter", Chain: "OUTPUT", Rule: strings.Fields(`-p udp -m udp --dport 6081 -j MARK --set-xmark 0x0`)},
			{Table: "filter", Chain: "OUTPUT", Rule: strings.Fields(`-p udp -m udp --dport 4789 -j MARK --set-xmark 0x0`)},
			// Drop invalid rst
			{Table: MANGLE, Chain: OvnPostrouting, Rule: strings.Fields(`-p tcp -m set --match-set ovn40subnets src -m tcp --tcp-flags RST RST -m state --state INVALID -j DROP`)},
		}
		v6Rules = []util.IPTableRule{
			// mark packets from pod to service
			{Table: NAT, Chain: OvnPrerouting, Rule: strings.Fields(`-i ovn0 -m set --match-set ovn60subnets src -m set --match-set ovn60services dst -j MARK --set-xmark 0x4000/0x4000`)},
			// nat packets marked by kube-proxy or kube-ovn
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m mark --mark 0x4000/0x4000 -j ` + OvnMasquerade)},
			// nat service traffic
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m set --match-set ovn60subnets src -m set --match-set ovn60subnets dst -j ` + OvnMasquerade)},
			// do not nat node port service traffic with external traffic policy set to local
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m mark --mark 0x80000/0x80000 -m set --match-set ovn60subnets-distributed-gw dst -j RETURN`)},
			// nat node port service traffic with external traffic policy set to local for subnets with centralized gateway
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m mark --mark 0x80000/0x80000 -j ` + OvnMasquerade)},
			// do not nat reply packets in direct routing
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-p tcp -m tcp --tcp-flags SYN NONE -m conntrack --ctstate NEW -j RETURN`)},
			// do not nat route traffic
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m set ! --match-set ovn60subnets src -m set ! --match-set ovn60other-node src -m set --match-set ovn60subnets-nat dst -j RETURN`)},
			// nat outgoing policy rules
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields("-m set --match-set ovn60subnets-nat-policy src -m set ! --match-set ovn60subnets dst -j " + OvnNatOutGoingPolicy)},
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(fmt.Sprintf(`-m mark --mark %s -j %s`, OnOutGoingNatMark, OvnMasquerade))},
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(fmt.Sprintf(`-m mark --mark %s -j RETURN`, OnOutGoingForwardMark))},
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m set --match-set ovn60subnets-nat src -m set ! --match-set ovn60subnets dst -j ` + OvnMasquerade)},
			// clear mark
			{Table: NAT, Chain: OvnMasquerade, Rule: strings.Fields(`-j MARK --set-xmark 0x0/0xffffffff`)},
			// do masquerade
			{Table: NAT, Chain: OvnMasquerade, Rule: strings.Fields(`-j MASQUERADE`)},
			// Input Accept
			{Table: "filter", Chain: "INPUT", Rule: strings.Fields(`-m set --match-set ovn60subnets src -j ACCEPT`)},
			{Table: "filter", Chain: "INPUT", Rule: strings.Fields(`-m set --match-set ovn60subnets dst -j ACCEPT`)},
			{Table: "filter", Chain: "INPUT", Rule: strings.Fields(`-m set --match-set ovn60services src -j ACCEPT`)},
			{Table: "filter", Chain: "INPUT", Rule: strings.Fields(`-m set --match-set ovn60services dst -j ACCEPT`)},
			// Forward Accept
			{Table: "filter", Chain: "FORWARD", Rule: strings.Fields(`-m set --match-set ovn60subnets src -j ACCEPT`)},
			{Table: "filter", Chain: "FORWARD", Rule: strings.Fields(`-m set --match-set ovn60subnets dst -j ACCEPT`)},
			{Table: "filter", Chain: "FORWARD", Rule: strings.Fields(`-m set --match-set ovn60services src -j ACCEPT`)},
			{Table: "filter", Chain: "FORWARD", Rule: strings.Fields(`-m set --match-set ovn60services dst -j ACCEPT`)},
			// Output unmark to bypass kernel nat checksum issue https://github.com/flannel-io/flannel/issues/1279
			{Table: "filter", Chain: "OUTPUT", Rule: strings.Fields(`-p udp -m udp --dport 6081 -j MARK --set-xmark 0x0`)},
			{Table: "filter", Chain: "OUTPUT", Rule: strings.Fields(`-p udp -m udp --dport 4789 -j MARK --set-xmark 0x0`)},
			// Drop invalid rst
			{Table: MANGLE, Chain: OvnPostrouting, Rule: strings.Fields(`-p tcp -m set --match-set ovn60subnets src -m tcp --tcp-flags RST RST -m state --state INVALID -j DROP`)},
		}
	)
	protocols := make([]string, 0, 2)
	if c.protocol == kubeovnv1.ProtocolDual {
		protocols = append(protocols, kubeovnv1.ProtocolIPv4, kubeovnv1.ProtocolIPv6)
	} else {
		protocols = append(protocols, c.protocol)
	}

	for _, protocol := range protocols {
		ipt := c.iptables[protocol]
		if ipt == nil {
			continue
		}

		var kubeProxyIpsetProtocol, matchset, svcMatchset, nodeMatchSet string
		var obsoleteRules, iptablesRules []util.IPTableRule
		if protocol == kubeovnv1.ProtocolIPv4 {
			iptablesRules = v4Rules
			matchset, svcMatchset, nodeMatchSet = "ovn40subnets", "ovn40services", "ovn40"+OtherNodeSet
		} else {
			iptablesRules = v6Rules
			kubeProxyIpsetProtocol, matchset, svcMatchset, nodeMatchSet = "6-", "ovn60subnets", "ovn60services", "ovn60"+OtherNodeSet
		}

		ipset := fmt.Sprintf("KUBE-%sCLUSTER-IP", kubeProxyIpsetProtocol)
		ipsetExists, err := c.ipsetExists(ipset)
		if err != nil {
			klog.Errorf("failed to check existence of ipset %s: %v", ipset, err)
			return err
		}
		if ipsetExists {
			iptablesRules[0].Rule = strings.Fields(fmt.Sprintf(`-i ovn0 -m set --match-set %s src -m set --match-set %s dst,dst -j MARK --set-xmark 0x4000/0x4000`, matchset, ipset))
			rejectRule := strings.Fields(fmt.Sprintf(`-p tcp -m mark ! --mark 0x4000/0x4000 -m set --match-set %s dst -m conntrack --ctstate NEW -j REJECT`, svcMatchset))
			obsoleteRejectRule := strings.Fields(fmt.Sprintf(`-m mark ! --mark 0x4000/0x4000 -m set --match-set %s dst -m conntrack --ctstate NEW -j REJECT`, svcMatchset))
			iptablesRules = append(iptablesRules,
				util.IPTableRule{Table: "filter", Chain: "INPUT", Rule: rejectRule},
				util.IPTableRule{Table: "filter", Chain: "OUTPUT", Rule: rejectRule},
			)
			obsoleteRejectRules := []util.IPTableRule{
				{Table: "filter", Chain: "INPUT", Rule: obsoleteRejectRule},
				{Table: "filter", Chain: "OUTPUT", Rule: obsoleteRejectRule},
			}
			for _, rule := range obsoleteRejectRules {
				if err = deleteIptablesRule(ipt, rule); err != nil {
					klog.Errorf("failed to delete obsolete iptables rule %v: %v", rule, err)
					return err
				}
			}
		}

		if nodeIP := nodeIPs[protocol]; nodeIP != "" {
			obsoleteRules = []util.IPTableRule{
				{Table: NAT, Chain: Postrouting, Rule: strings.Fields(fmt.Sprintf(`! -s %s -m set --match-set %s dst -j MASQUERADE`, nodeIP, matchset))},
				{Table: NAT, Chain: Postrouting, Rule: strings.Fields(fmt.Sprintf(`! -s %s -m mark --mark 0x4000/0x4000 -j MASQUERADE`, nodeIP))},
				{Table: NAT, Chain: Postrouting, Rule: strings.Fields(fmt.Sprintf(`! -s %s -m set ! --match-set %s src -m set --match-set %s dst -j MASQUERADE`, nodeIP, matchset, matchset))},
			}

			rules := make([]util.IPTableRule, len(iptablesRules)+1)
			copy(rules, iptablesRules[:1])
			copy(rules[2:], iptablesRules[1:])
			rules[1] = util.IPTableRule{
				Table: NAT,
				Chain: OvnPostrouting,
				Rule:  strings.Fields(fmt.Sprintf(`-m set --match-set %s src -m set --match-set %s dst -m mark --mark 0x4000/0x4000 -j SNAT --to-source %s`, svcMatchset, matchset, nodeIP)),
			}
			iptablesRules = rules

			for _, p := range [...]string{"tcp", "udp"} {
				ipset := fmt.Sprintf("KUBE-%sNODE-PORT-LOCAL-%s", kubeProxyIpsetProtocol, strings.ToUpper(p))
				ipsetExists, err := c.ipsetExists(ipset)
				if err != nil {
					klog.Errorf("failed to check existence of ipset %s: %v", ipset, err)
					return err
				}
				if !ipsetExists {
					klog.V(5).Infof("ipset %s does not exist", ipset)
					continue
				}
				rule := fmt.Sprintf("-p %s -m addrtype --dst-type LOCAL -m set --match-set %s dst -j MARK --set-xmark 0x80000/0x80000", p, ipset)
				rule2 := fmt.Sprintf("-p %s -m set --match-set %s src -m set --match-set %s dst -j MARK --set-xmark 0x4000/0x4000", p, nodeMatchSet, ipset)
				obsoleteRules = append(obsoleteRules, util.IPTableRule{Table: NAT, Chain: Prerouting, Rule: strings.Fields(rule)})
				iptablesRules = append(iptablesRules,
					util.IPTableRule{Table: NAT, Chain: OvnPrerouting, Rule: strings.Fields(rule)},
					util.IPTableRule{Table: NAT, Chain: OvnPrerouting, Rule: strings.Fields(rule2)},
				)
			}
		}

		_, subnetCidrs, err := c.getDefaultVpcSubnetsCIDR(protocol)
		if err != nil {
			klog.Errorf("get subnets failed, %+v", err)
			return err
		}

		subnetNames := set.New[string]()
		for name, subnetCidr := range subnetCidrs {
			subnetNames.Insert(name)
			iptablesRules = append(iptablesRules,
				util.IPTableRule{Table: "filter", Chain: "FORWARD", Rule: strings.Fields(fmt.Sprintf(`-m comment --comment %s,%s -s %s`, util.OvnSubnetGatewayIptables, name, subnetCidr))},
				util.IPTableRule{Table: "filter", Chain: "FORWARD", Rule: strings.Fields(fmt.Sprintf(`-m comment --comment %s,%s -d %s`, util.OvnSubnetGatewayIptables, name, subnetCidr))},
			)
		}

		rules, err := ipt.List("filter", "FORWARD")
		if err != nil {
			klog.Errorf(`failed to list iptables rule table "filter" chain "FORWARD" with err %v `, err)
			return err
		}

		pattern := fmt.Sprintf(`-m comment --comment "%s,`, util.OvnSubnetGatewayIptables)
		for _, rule := range rules {
			if !strings.Contains(rule, pattern) {
				continue
			}
			fields := util.DoubleQuotedFields(rule)
			// -A FORWARD -d 10.16.0.0/16 -m comment --comment "ovn-subnet-gateway,ovn-default"
			if len(fields) != 8 || fields[6] != "--comment" {
				continue
			}
			commentFields := strings.Split(fields[7], ",")
			if len(commentFields) != 2 {
				continue
			}
			if subnetNames.Has(commentFields[1]) {
				continue
			}

			// use fields[2:] to skip prefix "-A FORWARD"
			if err = deleteIptablesRule(ipt, util.IPTableRule{Table: "filter", Chain: "FORWARD", Rule: fields[2:]}); err != nil {
				klog.Error(err)
				return err
			}
		}

		var natPreroutingRules, natPostroutingRules, ovnMasqueradeRules, manglePostroutingRules []util.IPTableRule
		for _, rule := range iptablesRules {
			if rule.Table == NAT {
				if c.k8siptables[protocol].HasRandomFully() &&
					(rule.Rule[len(rule.Rule)-1] == "MASQUERADE" || slices.Contains(rule.Rule, "SNAT")) {
					rule.Rule = append(rule.Rule, "--random-fully")
				}

				switch rule.Chain {
				case OvnPrerouting:
					natPreroutingRules = append(natPreroutingRules, rule)
					continue
				case OvnPostrouting:
					natPostroutingRules = append(natPostroutingRules, rule)
					continue
				case OvnMasquerade:
					ovnMasqueradeRules = append(ovnMasqueradeRules, rule)
					continue
				}
			} else if rule.Table == MANGLE {
				if rule.Chain == OvnPostrouting {
					manglePostroutingRules = append(manglePostroutingRules, rule)
					continue
				}
			}

			if err = c.createIptablesRule(ipt, rule); err != nil {
				klog.Errorf(`failed to create iptables rule "%s": %v`, strings.Join(rule.Rule, " "), err)
				return err
			}
		}

		var randomFully string
		if c.k8siptables[protocol].HasRandomFully() {
			randomFully = "--random-fully"
		}

		// add iptables rule for nat gw with designative ip in centralized subnet
		for cidr, ip := range centralGwNatIPs {
			if util.CheckProtocol(cidr) != protocol {
				continue
			}

			s := fmt.Sprintf("-s %s -m set ! --match-set %s dst -j SNAT --to-source %s %s", cidr, matchset, ip, randomFully)
			rule := util.IPTableRule{
				Table: NAT,
				Chain: OvnPostrouting,
				Rule:  util.DoubleQuotedFields(s),
			}
			// insert the rule before the one for nat outgoing
			n := len(natPostroutingRules)
			natPostroutingRules = append(natPostroutingRules[:n-1], rule, natPostroutingRules[n-1])
		}

		if err = c.reconcileNatOutgoingPolicyIptablesChain(protocol); err != nil {
			klog.Error(err)
			return err
		}

		if err = c.reconcileTProxyIPTableRules(protocol); err != nil {
			klog.Error(err)
			return err
		}

		if err = c.updateIptablesChain(ipt, NAT, OvnPrerouting, Prerouting, natPreroutingRules); err != nil {
			klog.Errorf("failed to update chain %s/%s: %v", NAT, OvnPrerouting, err)
			return err
		}
		if err = c.updateIptablesChain(ipt, NAT, OvnMasquerade, "", ovnMasqueradeRules); err != nil {
			klog.Errorf("failed to update chain %s/%s: %v", NAT, OvnMasquerade, err)
			return err
		}
		if err = c.updateIptablesChain(ipt, NAT, OvnPostrouting, Postrouting, natPostroutingRules); err != nil {
			klog.Errorf("failed to update chain %s/%s: %v", NAT, OvnPostrouting, err)
			return err
		}

		if err = c.updateIptablesChain(ipt, MANGLE, OvnPostrouting, Postrouting, manglePostroutingRules); err != nil {
			klog.Errorf("failed to update chain %s/%s: %v", MANGLE, OvnPostrouting, err)
			return err
		}

		if err = c.cleanObsoleteIptablesRules(protocol, obsoleteRules); err != nil {
			klog.Errorf("failed to clean legacy iptables rules: %v", err)
			return err
		}
	}
	return nil
}

func (c *Controller) reconcileTProxyIPTableRules(protocol string) error {
	if !c.config.EnableTProxy {
		return nil
	}

	ipt := c.iptables[protocol]
	tproxyPreRoutingRules := make([]util.IPTableRule, 0)
	tproxyOutputRules := make([]util.IPTableRule, 0)

	pods, err := c.getTProxyConditionPod(true)
	if err != nil {
		klog.Error(err)
		return err
	}

	for _, pod := range pods {
		var podIP string
		for _, ip := range pod.Status.PodIPs {
			if util.CheckProtocol(ip.IP) == protocol {
				podIP = ip.IP
				break
			}
		}

		if podIP == "" {
			continue
		}

		ports := getProbePorts(pod)
		if ports.Len() == 0 {
			continue
		}

		for _, probePort := range ports.SortedList() {
			hostIP := c.config.NodeIPv4
			prefixLen := 32
			if protocol == kubeovnv1.ProtocolIPv6 {
				prefixLen = 128
				hostIP = c.config.NodeIPv6
			}

			tproxyOutputRules = append(tproxyOutputRules, util.IPTableRule{Table: MANGLE, Chain: OvnOutput, Rule: strings.Fields(fmt.Sprintf(`-d %s/%d -p tcp -m tcp --dport %d -j MARK --set-xmark %s`, podIP, prefixLen, probePort, tProxyOutputMarkMask))})
			tproxyPreRoutingRules = append(tproxyPreRoutingRules, util.IPTableRule{Table: MANGLE, Chain: OvnPrerouting, Rule: strings.Fields(fmt.Sprintf(`-d %s/%d -p tcp -m tcp --dport %d -j TPROXY --on-port %d --on-ip %s --tproxy-mark %s`, podIP, prefixLen, probePort, util.TProxyListenPort, hostIP, tProxyPreRoutingMarkMask))})
		}
	}

	if err := c.updateIptablesChain(ipt, MANGLE, OvnPrerouting, Prerouting, tproxyPreRoutingRules); err != nil {
		klog.Errorf("failed to update chain %s with rules %v: %v", OvnPrerouting, tproxyPreRoutingRules, err)
		return err
	}

	if err := c.updateIptablesChain(ipt, MANGLE, OvnOutput, Output, tproxyOutputRules); err != nil {
		klog.Errorf("failed to update chain %s with rules %v: %v", OvnOutput, tproxyOutputRules, err)
		return err
	}
	return nil
}

func (c *Controller) cleanTProxyIPTableRules(protocol string) {
	ipt := c.iptables[protocol]
	if ipt == nil {
		return
	}
	for _, chain := range [2]string{OvnPrerouting, OvnOutput} {
		if err := ipt.ClearChain(MANGLE, chain); err != nil {
			klog.Errorf("failed to clear iptables chain %v in table %v, %+v", chain, MANGLE, err)
			return
		}
	}
}

func (c *Controller) reconcileNatOutgoingPolicyIptablesChain(protocol string) error {
	ipt := c.iptables[protocol]

	natPolicySubnetIptables, natPolicyRuleIptablesMap, gcNatPolicySubnetChains, err := c.generateNatOutgoingPolicyChainRules(protocol)
	if err != nil {
		klog.Errorf(`failed to get nat policy post routing rules with err %v `, err)
		return err
	}

	for chainName, natPolicyRuleIptableRules := range natPolicyRuleIptablesMap {
		if err = c.updateIptablesChain(ipt, NAT, chainName, "", natPolicyRuleIptableRules); err != nil {
			klog.Errorf("failed to update chain %s with rules %v: %v", chainName, natPolicyRuleIptableRules, err)
			return err
		}
	}

	if err = c.updateIptablesChain(ipt, NAT, OvnNatOutGoingPolicy, "", natPolicySubnetIptables); err != nil {
		klog.Errorf("failed to update chain %s: %v", OvnNatOutGoingPolicy, err)
		return err
	}

	for _, gcNatPolicySubnetChain := range gcNatPolicySubnetChains {
		if err = ipt.ClearAndDeleteChain(NAT, gcNatPolicySubnetChain); err != nil {
			klog.Errorf("failed to delete iptables chain %q in table %s: %v", gcNatPolicySubnetChain, NAT, err)
			return err
		}
		klog.Infof("deleted iptables chain %s in table %s", gcNatPolicySubnetChain, NAT)
	}
	return nil
}

func (c *Controller) generateNatOutgoingPolicyChainRules(protocol string) ([]util.IPTableRule, map[string][]util.IPTableRule, []string, error) {
	natPolicySubnetIptables := make([]util.IPTableRule, 0)
	natPolicyRuleIptablesMap := make(map[string][]util.IPTableRule)
	natPolicySubnetUIDs := strset.New()
	gcNatPolicySubnetChains := make([]string, 0)
	subnetNames := make([]string, 0)
	subnetMap := make(map[string]*kubeovnv1.Subnet)

	subnets, err := c.getSubnetsNatOutGoingPolicy(protocol)
	if err != nil {
		klog.Errorf("failed to get subnets with NAT outgoing policy rule: %v", err)
		return nil, nil, nil, err
	}

	for _, subnet := range subnets {
		subnetNames = append(subnetNames, subnet.Name)
		subnetMap[subnet.Name] = subnet
	}

	// To ensure the iptable rule order
	sort.Strings(subnetNames)

	getMatchProtocol := func(ips string) string {
		ip := strings.Split(ips, ",")[0]
		return util.CheckProtocol(ip)
	}

	for _, subnetName := range subnetNames {
		subnet := subnetMap[subnetName]
		var natPolicyRuleIptables []util.IPTableRule
		natPolicySubnetUIDs.Add(util.GetTruncatedUID(string(subnet.GetUID())))
		cidrBlock, err := getCidrByProtocol(subnet.Spec.CIDRBlock, protocol)
		if err != nil {
			klog.Errorf("failed to get subnet %s cidr block with protocol: %v", subnet.Name, err)
			continue
		}
		if cidrBlock == "" {
			continue
		}

		ovnNatPolicySubnetChainName := OvnNatOutGoingPolicySubnet + util.GetTruncatedUID(string(subnet.GetUID()))
		natPolicySubnetIptables = append(natPolicySubnetIptables, util.IPTableRule{Table: NAT, Chain: OvnNatOutGoingPolicy, Rule: strings.Fields(fmt.Sprintf(`-s %s -m comment --comment natPolicySubnet-%s -j %s`, cidrBlock, subnet.Name, ovnNatPolicySubnetChainName))})
		for _, rule := range subnet.Status.NatOutgoingPolicyRules {
			var markCode string
			switch rule.Action {
			case util.NatPolicyRuleActionNat:
				markCode = OnOutGoingNatMark
			case util.NatPolicyRuleActionForward:
				markCode = OnOutGoingForwardMark
			}

			if rule.RuleID == "" {
				continue
			}

			if rule.Match.SrcIPs != "" && getMatchProtocol(rule.Match.SrcIPs) != protocol {
				continue
			}

			if rule.Match.DstIPs != "" && getMatchProtocol(rule.Match.DstIPs) != protocol {
				continue
			}

			srcMatch := getNatOutGoingPolicyRuleIPSetName(rule.RuleID, "src", protocol, true)
			dstMatch := getNatOutGoingPolicyRuleIPSetName(rule.RuleID, "dst", protocol, true)

			var ovnNatoutGoingPolicyRule util.IPTableRule

			switch {
			case rule.Match.DstIPs != "" && rule.Match.SrcIPs != "":
				ovnNatoutGoingPolicyRule = util.IPTableRule{Table: NAT, Chain: ovnNatPolicySubnetChainName, Rule: strings.Fields(fmt.Sprintf(`-m set --match-set %s src -m set --match-set %s dst -j MARK --set-xmark %s`, srcMatch, dstMatch, markCode))}
			case rule.Match.SrcIPs != "":
				protocol = getMatchProtocol(rule.Match.SrcIPs)
				ovnNatoutGoingPolicyRule = util.IPTableRule{Table: NAT, Chain: ovnNatPolicySubnetChainName, Rule: strings.Fields(fmt.Sprintf(`-m set --match-set %s src -j MARK --set-xmark %s`, srcMatch, markCode))}
			case rule.Match.DstIPs != "":
				protocol = getMatchProtocol(rule.Match.DstIPs)
				ovnNatoutGoingPolicyRule = util.IPTableRule{Table: NAT, Chain: ovnNatPolicySubnetChainName, Rule: strings.Fields(fmt.Sprintf(`-m set --match-set %s dst -j MARK --set-xmark %s`, dstMatch, markCode))}
			default:
				continue
			}
			natPolicyRuleIptables = append(natPolicyRuleIptables, ovnNatoutGoingPolicyRule)
		}
		natPolicyRuleIptablesMap[ovnNatPolicySubnetChainName] = natPolicyRuleIptables
	}

	existNatChains, err := c.iptables[protocol].ListChains(NAT)
	if err != nil {
		klog.Errorf("list chains in table nat failed")
		return nil, nil, nil, err
	}

	for _, existNatChain := range existNatChains {
		if strings.HasPrefix(existNatChain, OvnNatOutGoingPolicySubnet) &&
			!natPolicySubnetUIDs.Has(getNatPolicySubnetChainUID(existNatChain)) {
			gcNatPolicySubnetChains = append(gcNatPolicySubnetChains, existNatChain)
		}
	}

	return natPolicySubnetIptables, natPolicyRuleIptablesMap, gcNatPolicySubnetChains, nil
}

func deleteIptablesRule(ipt *iptables.IPTables, rule util.IPTableRule) error {
	if rule.Pos != "" {
		klog.Infof("delete iptables rule by pos %s: %v", rule.Pos, rule)
		if err := ipt.Delete(rule.Table, rule.Chain, rule.Pos); err != nil {
			klog.Errorf("failed to delete iptables %s rule %q: %v", rule.Chain, strings.Join(rule.Rule, " "), err)
			return err
		}
		return nil
	}
	exists, err := ipt.Exists(rule.Table, rule.Chain, rule.Rule...)
	if err == nil && exists {
		klog.Infof("delete iptables rule: %v", rule)
		err = ipt.Delete(rule.Table, rule.Chain, rule.Rule...)
	}
	if err != nil {
		klog.Errorf("failed to delete iptables rule %q: %v", strings.Join(rule.Rule, " "), err)
		return err
	}
	return nil
}

func clearObsoleteIptablesChain(ipt *iptables.IPTables, table, chain, parent string) error {
	exists, err := ipt.ChainExists(table, chain)
	if err != nil {
		klog.Error(err)
		return err
	}
	if !exists {
		return nil
	}

	rule := fmt.Sprintf(`-m comment --comment "kube-ovn %s rules" -j %s`, strings.ToLower(parent), chain)
	if err = deleteIptablesRule(ipt, util.IPTableRule{Table: table, Chain: parent, Rule: util.DoubleQuotedFields(rule)}); err != nil {
		klog.Error(err)
		return err
	}
	if err = ipt.ClearAndDeleteChain(table, chain); err != nil {
		klog.Errorf("failed to delete iptables chain %q in table %s: %v", chain, table, err)
		return err
	}
	return nil
}

func (c *Controller) cleanObsoleteIptablesRules(protocol string, rules []util.IPTableRule) error {
	if c.iptablesObsolete == nil || c.iptablesObsolete[protocol] == nil {
		return nil
	}

	var (
		v4ObsoleteRules = []util.IPTableRule{
			{Table: NAT, Chain: Postrouting, Rule: strings.Fields(`-m mark --mark 0x40000/0x40000 -j MASQUERADE`)},
			{Table: "mangle", Chain: Prerouting, Rule: strings.Fields(`-i ovn0 -m set --match-set ovn40subnets src -m set --match-set ovn40services dst -j MARK --set-xmark 0x40000/0x40000`)},
			// legacy rules
			// nat packets marked by kube-proxy or kube-ovn
			{Table: NAT, Chain: Postrouting, Rule: strings.Fields(`-m mark --mark 0x4000/0x4000 -j MASQUERADE`)},
			// nat service traffic
			{Table: NAT, Chain: Postrouting, Rule: strings.Fields(`-m set --match-set ovn40subnets src -m set --match-set ovn40subnets dst -j MASQUERADE`)},
			// do not nat node port service traffic with external traffic policy set to local
			{Table: NAT, Chain: Postrouting, Rule: strings.Fields(`-m mark --mark 0x80000/0x80000 -m set --match-set ovn40subnets-distributed-gw dst -j RETURN`)},
			// nat node port service traffic with external traffic policy set to local for subnets with centralized gateway
			{Table: NAT, Chain: Postrouting, Rule: strings.Fields(`-m mark --mark 0x80000/0x80000 -j MASQUERADE`)},
			// do not nat reply packets in direct routing
			{Table: NAT, Chain: Postrouting, Rule: strings.Fields(`-p tcp --tcp-flags SYN NONE -m conntrack --ctstate NEW -j RETURN`)},
			// do not nat route traffic
			{Table: NAT, Chain: Postrouting, Rule: strings.Fields(`-m set ! --match-set ovn40subnets src -m set ! --match-set ovn40other-node src -m set --match-set ovn40subnets-nat dst -j RETURN`)},
			// nat outgoing
			{Table: NAT, Chain: Postrouting, Rule: strings.Fields(`-m set --match-set ovn40subnets-nat src -m set ! --match-set ovn40subnets dst -j MASQUERADE`)},
			// mark packets from pod to service
			{Table: "mangle", Chain: Prerouting, Rule: strings.Fields(`-i ovn0 -m set --match-set ovn40subnets src -m set --match-set ovn40services dst -j MARK --set-xmark 0x4000/0x4000`)},
			// Input Accept
			{Table: "filter", Chain: "INPUT", Rule: strings.Fields(`-m set --match-set ovn40subnets src -j ACCEPT`)},
			{Table: "filter", Chain: "INPUT", Rule: strings.Fields(`-m set --match-set ovn40subnets dst -j ACCEPT`)},
			{Table: "filter", Chain: "INPUT", Rule: strings.Fields(`-m set --match-set ovn40services src -j ACCEPT`)},
			{Table: "filter", Chain: "INPUT", Rule: strings.Fields(`-m set --match-set ovn40services dst -j ACCEPT`)},
			// Forward Accept
			{Table: "filter", Chain: "FORWARD", Rule: strings.Fields(`-m set --match-set ovn40subnets src -j ACCEPT`)},
			{Table: "filter", Chain: "FORWARD", Rule: strings.Fields(`-m set --match-set ovn40subnets dst -j ACCEPT`)},
			{Table: "filter", Chain: "FORWARD", Rule: strings.Fields(`-m set --match-set ovn40services src -j ACCEPT`)},
			{Table: "filter", Chain: "FORWARD", Rule: strings.Fields(`-m set --match-set ovn40services dst -j ACCEPT`)},
			// Output unmark to bypass kernel nat checksum issue https://github.com/flannel-io/flannel/issues/1279
			{Table: "filter", Chain: "OUTPUT", Rule: strings.Fields(`-p udp -m udp --dport 6081 -j MARK --set-xmark 0x0`)},
			{Table: "filter", Chain: "OUTPUT", Rule: strings.Fields(`-p udp -m udp --dport 4789 -j MARK --set-xmark 0x0`)},
		}
		v6ObsoleteRules = []util.IPTableRule{
			{Table: NAT, Chain: Postrouting, Rule: strings.Fields(`-m mark --mark 0x40000/0x40000 -j MASQUERADE`)},
			{Table: "mangle", Chain: Prerouting, Rule: strings.Fields(`-i ovn0 -m set --match-set ovn60subnets src -m set --match-set ovn60services dst -j MARK --set-xmark 0x40000/0x40000`)},
			// legacy rules
			// nat packets marked by kube-proxy or kube-ovn
			{Table: NAT, Chain: Postrouting, Rule: strings.Fields(`-m mark --mark 0x4000/0x4000 -j MASQUERADE`)},
			// nat service traffic
			{Table: NAT, Chain: Postrouting, Rule: strings.Fields(`-m set --match-set ovn60subnets src -m set --match-set ovn60subnets dst -j MASQUERADE`)},
			// do not nat node port service traffic with external traffic policy set to local
			{Table: NAT, Chain: Postrouting, Rule: strings.Fields(`-m mark --mark 0x80000/0x80000 -m set --match-set ovn60subnets-distributed-gw dst -j RETURN`)},
			// nat node port service traffic with external traffic policy set to local for subnets with centralized gateway
			{Table: NAT, Chain: Postrouting, Rule: strings.Fields(`-m mark --mark 0x80000/0x80000 -j MASQUERADE`)},
			// do not nat reply packets in direct routing
			{Table: NAT, Chain: Postrouting, Rule: strings.Fields(`-p tcp --tcp-flags SYN NONE -m conntrack --ctstate NEW -j RETURN`)},
			// do not nat route traffic
			{Table: NAT, Chain: Postrouting, Rule: strings.Fields(`-m set ! --match-set ovn60subnets src -m set ! --match-set ovn60other-node src -m set --match-set ovn60subnets-nat dst -j RETURN`)},
			// nat outgoing
			{Table: NAT, Chain: Postrouting, Rule: strings.Fields(`-m set --match-set ovn60subnets-nat src -m set ! --match-set ovn60subnets dst -j MASQUERADE`)},
			// mark packets from pod to service
			{Table: "mangle", Chain: Prerouting, Rule: strings.Fields(`-i ovn0 -m set --match-set ovn60subnets src -m set --match-set ovn60services dst -j MARK --set-xmark 0x4000/0x4000`)},
			// Input Accept
			{Table: "filter", Chain: "INPUT", Rule: strings.Fields(`-m set --match-set ovn60subnets src -j ACCEPT`)},
			{Table: "filter", Chain: "INPUT", Rule: strings.Fields(`-m set --match-set ovn60subnets dst -j ACCEPT`)},
			{Table: "filter", Chain: "INPUT", Rule: strings.Fields(`-m set --match-set ovn60services src -j ACCEPT`)},
			{Table: "filter", Chain: "INPUT", Rule: strings.Fields(`-m set --match-set ovn60services dst -j ACCEPT`)},
			// Forward Accept
			{Table: "filter", Chain: "FORWARD", Rule: strings.Fields(`-m set --match-set ovn60subnets src -j ACCEPT`)},
			{Table: "filter", Chain: "FORWARD", Rule: strings.Fields(`-m set --match-set ovn60subnets dst -j ACCEPT`)},
			{Table: "filter", Chain: "FORWARD", Rule: strings.Fields(`-m set --match-set ovn60services src -j ACCEPT`)},
			{Table: "filter", Chain: "FORWARD", Rule: strings.Fields(`-m set --match-set ovn60services dst -j ACCEPT`)},
			// Output unmark to bypass kernel nat checksum issue https://github.com/flannel-io/flannel/issues/1279
			{Table: "filter", Chain: "OUTPUT", Rule: strings.Fields(`-p udp -m udp --dport 6081 -j MARK --set-xmark 0x0`)},
			{Table: "filter", Chain: "OUTPUT", Rule: strings.Fields(`-p udp -m udp --dport 4789 -j MARK --set-xmark 0x0`)},
		}
	)

	var obsoleteRules []util.IPTableRule
	if protocol == kubeovnv1.ProtocolIPv4 {
		obsoleteRules = v4ObsoleteRules
	} else {
		obsoleteRules = v6ObsoleteRules
	}

	ipt := c.iptablesObsolete[protocol]
	for _, rule := range obsoleteRules {
		if err := deleteIptablesRule(ipt, rule); err != nil {
			klog.Error(err)
			return err
		}
	}
	for _, rule := range rules {
		if err := deleteIptablesRule(ipt, rule); err != nil {
			klog.Error(err)
			return err
		}
	}

	forwardRules, err := ipt.List("filter", "FORWARD")
	if err != nil {
		klog.Errorf(`failed to list legacy iptables rule in "FORWARD" chain "filter" table: %v`, err)
		return err
	}
	prefix := util.OvnSubnetGatewayIptables + ","
	for _, rule := range forwardRules {
		fields := util.DoubleQuotedFields(rule)
		for _, f := range fields {
			if strings.HasPrefix(f, prefix) {
				if err = ipt.Delete("filter", "FORWARD", fields...); err != nil {
					klog.Errorf("failed to delete legacy iptables rules %q: %v", rule, err)
				}
			}
		}
	}

	// delete unused iptables rule when nat gw with designative ip has been changed in centralized subnet
	if err = c.deleteObsoleteSnatRules(ipt, NAT, Postrouting); err != nil {
		klog.Errorf("failed to delete legacy iptables rule for SNAT: %v", err)
		return err
	}

	if err = clearObsoleteIptablesChain(ipt, NAT, OvnPrerouting, Prerouting); err != nil {
		klog.Error(err)
		return err
	}
	if err = clearObsoleteIptablesChain(ipt, NAT, OvnPostrouting, Postrouting); err != nil {
		klog.Error(err)
		return err
	}

	delete(c.iptablesObsolete, protocol)
	if len(c.iptablesObsolete) == 0 {
		c.iptablesObsolete = nil
	}
	return nil
}

func (c *Controller) setOvnSubnetGatewayMetric() {
	nodeName := os.Getenv(util.EnvNodeName)
	for proto, iptables := range c.iptables {
		rules, err := iptables.ListWithCounters("filter", "FORWARD")
		if err != nil {
			klog.Errorf("get proto %s iptables failed with err %v", proto, err)
			continue
		}

		for _, rule := range rules {
			if !strings.Contains(rule, util.OvnSubnetGatewayIptables) {
				continue
			}

			items := util.DoubleQuotedFields(rule)
			if len(items) != 11 {
				continue
			}

			comments := strings.Split(items[7], ",")
			if len(comments) != 2 || comments[0] != util.OvnSubnetGatewayIptables {
				continue
			}
			subnetName := comments[1]

			var direction string
			switch items[2] {
			case "-s":
				direction = "egress"
			case "-d":
				direction = "ingress"
			default:
				continue
			}

			cidr := items[3]
			proto := util.CheckProtocol(cidr)
			if proto == "" {
				klog.Errorf("failed to get protocol from cidr %q", cidr)
				continue
			}

			currentPackets, err := strconv.Atoi(items[9])
			if err != nil {
				klog.Errorf("failed to parse packets %q: %v", items[9], err)
				continue
			}
			currentPacketBytes, err := strconv.Atoi(items[10])
			if err != nil {
				klog.Errorf("failed to parse packet bytes %q: %v", items[10], err)
				continue
			}

			key := strings.Join([]string{subnetName, direction, proto}, "/")
			if c.gwCounters[key] == nil {
				c.gwCounters[key] = new(util.GwIPtableCounters)
			}
			lastPackets, lastPacketBytes := c.gwCounters[key].Packets, c.gwCounters[key].PacketBytes
			c.gwCounters[key].Packets, c.gwCounters[key].PacketBytes = currentPackets, currentPacketBytes

			if lastPackets == 0 && lastPacketBytes == 0 {
				// the gwCounters may just initialize don't cal the diff values,
				// it may loss packets to calculate during a metric period
				continue
			}
			if currentPackets < lastPackets || currentPacketBytes < lastPacketBytes {
				// if currentPacketBytes < lastPacketBytes, the reason is that iptables rule is reset ,
				// it may loss packets to calculate during a metric period
				continue
			}

			diffPackets := currentPackets - lastPackets
			diffPacketBytes := currentPacketBytes - lastPacketBytes
			klog.V(3).Infof(`nodeName %s key %s cidr %s direction %s proto %s has diffPackets %d diffPacketBytes %d currentPackets %d currentPacketBytes %d lastPackets %d lastPacketBytes %d`,
				nodeName, key, cidr, direction, proto, diffPackets, diffPacketBytes, currentPackets, currentPacketBytes, lastPackets, lastPacketBytes)
			metricOvnSubnetGatewayPackets.WithLabelValues(nodeName, key, cidr, direction, proto).Add(float64(diffPackets))
			metricOvnSubnetGatewayPacketBytes.WithLabelValues(nodeName, key, cidr, direction, proto).Add(float64(diffPacketBytes))
		}
	}
}

func (c *Controller) addEgressConfig(subnet *kubeovnv1.Subnet, ip string) error {
	if (subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) ||
		subnet.Spec.GatewayType != kubeovnv1.GWDistributedType ||
		subnet.Spec.Vpc != c.config.ClusterRouter {
		return nil
	}

	if !subnet.Spec.NatOutgoing && subnet.Spec.ExternalEgressGateway != "" {
		podIPs := strings.Split(ip, ",")
		protocol := util.CheckProtocol(ip)
		return c.addPodPolicyRouting(protocol, subnet.Spec.ExternalEgressGateway, subnet.Spec.PolicyRoutingPriority, subnet.Spec.PolicyRoutingTableID, podIPs)
	}

	return nil
}

func (c *Controller) removeEgressConfig(subnet, ip string) error {
	if subnet == "" || ip == "" {
		return nil
	}

	podSubnet, err := c.subnetsLister.Get(subnet)
	if k8serrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		klog.Errorf("failed to get subnet %s: %+v", subnet, err)
		return err
	}

	if (podSubnet.Spec.Vlan != "" && !podSubnet.Spec.LogicalGateway) ||
		podSubnet.Spec.GatewayType != kubeovnv1.GWDistributedType ||
		podSubnet.Spec.Vpc != c.config.ClusterRouter {
		return nil
	}

	if !podSubnet.Spec.NatOutgoing && podSubnet.Spec.ExternalEgressGateway != "" {
		podIPs := strings.Split(ip, ",")
		protocol := util.CheckProtocol(ip)
		return c.deletePodPolicyRouting(protocol, podSubnet.Spec.ExternalEgressGateway, podSubnet.Spec.PolicyRoutingPriority, podSubnet.Spec.PolicyRoutingTableID, podIPs)
	}

	return nil
}

func (c *Controller) setExGateway() error {
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node, %v", err)
		return err
	}
	var isUserspaceDP bool
	isUserspaceDP, err = ovs.IsUserspaceDataPath()
	if err != nil {
		klog.Error(err)
		return err
	}
	enable := node.Labels[util.ExGatewayLabel]
	externalBridge := util.ExternalBridgeName(c.config.ExternalGatewaySwitch)
	if enable == "true" {
		cm, err := c.config.KubeClient.CoreV1().ConfigMaps(c.config.ExternalGatewayConfigNS).Get(context.Background(), util.ExternalGatewayConfig, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get ovn-external-gw-config, %v", err)
			return err
		}

		linkName, exist := cm.Data["external-gw-nic"]
		if !exist || len(linkName) == 0 {
			err = errors.New("external-gw-nic not configured in ovn-external-gw-config")
			klog.Error(err)
			return err
		}

		if !isUserspaceDP {
			link, err := netlink.LinkByName(linkName)
			if err != nil {
				klog.Errorf("failed to get nic %s, %v", linkName, err)
				return err
			}
			if err := netlink.LinkSetUp(link); err != nil {
				klog.Errorf("failed to set gateway nic %s up, %v", linkName, err)
				return err
			}
		}

		externalBrReady := false
		// if external nic already attached into another bridge
		if existBr, err := ovs.Exec("port-to-br", linkName); err == nil {
			if existBr == externalBridge {
				externalBrReady = true
			} else {
				klog.Infof("external bridge should change from %s to %s, delete external bridge %s", existBr, externalBridge, existBr)
				if _, err := ovs.Exec(ovs.IfExists, "del-br", existBr); err != nil {
					err = fmt.Errorf("failed to del external br %s, %w", existBr, err)
					klog.Error(err)
					return err
				}
			}
		}

		if !externalBrReady {
			klog.Infof("create external bridge %s and add nic %s", externalBridge, linkName)
			if _, err := ovs.Exec(
				ovs.MayExist, "add-br", externalBridge, "--",
				ovs.MayExist, "add-port", externalBridge, linkName,
			); err != nil {
				err = fmt.Errorf("failed to enable external gateway, %w", err)
				klog.Error(err)
			}
		}
		if err = addOvnMapping("ovn-bridge-mappings", c.config.ExternalGatewaySwitch, externalBridge, true); err != nil {
			klog.Error(err)
			return err
		}
	} else {
		brExists, err := ovs.BridgeExists(externalBridge)
		if err != nil {
			return fmt.Errorf("failed to check OVS bridge existence: %w", err)
		}
		if !brExists {
			return nil
		}

		providerNetworks, err := c.providerNetworksLister.List(labels.Everything())
		if err != nil && !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to list provider networks: %v", err)
			return err
		}

		for _, pn := range providerNetworks {
			// if external nic already attached into another bridge
			if existBr, err := ovs.Exec("port-to-br", pn.Spec.DefaultInterface); err == nil {
				if existBr == externalBridge {
					// delete switch after related provider network not exist
					return nil
				}
			}
		}

		keepExternalSubnet := false
		externalSubnet, err := c.subnetsLister.Get(c.config.ExternalGatewaySwitch)
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to get subnet %s, %v", c.config.ExternalGatewaySwitch, err)
				return err
			}
		} else {
			if externalSubnet.Spec.Vlan != "" {
				keepExternalSubnet = true
			}
		}

		if !isUserspaceDP && !keepExternalSubnet {
			klog.Infof("delete external bridge %s", externalBridge)
			if _, err := ovs.Exec(
				ovs.IfExists, "del-br", externalBridge); err != nil {
				err = fmt.Errorf("failed to disable external gateway, %w", err)
				klog.Error(err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) getLocalPodIPsNeedPR(protocol string) (map[policyRouteMeta][]string, error) {
	allPods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list pods: %+v", err)
		return nil, err
	}

	nodeName := os.Getenv(util.EnvNodeName)
	localPodIPs := make(map[policyRouteMeta][]string)
	for _, pod := range allPods {
		if pod.Spec.HostNetwork ||
			!pod.DeletionTimestamp.IsZero() ||
			pod.Spec.NodeName != nodeName ||
			pod.Annotations[util.LogicalSwitchAnnotation] == "" ||
			pod.Annotations[util.IPAddressAnnotation] == "" {
			continue
		}

		subnet, err := c.subnetsLister.Get(pod.Annotations[util.LogicalSwitchAnnotation])
		if err != nil {
			klog.Errorf("failed to get subnet %s: %+v", pod.Annotations[util.LogicalSwitchAnnotation], err)
			continue
		}

		if subnet.Spec.ExternalEgressGateway == "" ||
			subnet.Spec.Vpc != c.config.ClusterRouter ||
			subnet.Spec.GatewayType != kubeovnv1.GWDistributedType {
			continue
		}
		if subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway {
			continue
		}

		ips := make([]string, 0, 2)
		if len(pod.Status.PodIPs) != 0 {
			if len(pod.Status.PodIPs) == 2 && protocol == kubeovnv1.ProtocolIPv6 {
				ips = append(ips, pod.Status.PodIPs[1].IP)
			} else if util.CheckProtocol(pod.Status.PodIP) == protocol {
				ips = append(ips, pod.Status.PodIP)
			}
		} else {
			ipv4, ipv6 := util.SplitStringIP(pod.Annotations[util.IPAddressAnnotation])
			if ipv4 != "" && protocol == kubeovnv1.ProtocolIPv4 {
				ips = append(ips, ipv4)
			}
			if ipv6 != "" && protocol == kubeovnv1.ProtocolIPv6 {
				ips = append(ips, ipv6)
			}
		}

		if len(ips) != 0 {
			meta := policyRouteMeta{
				priority: subnet.Spec.PolicyRoutingPriority,
				tableID:  subnet.Spec.PolicyRoutingTableID,
			}

			egw := strings.Split(subnet.Spec.ExternalEgressGateway, ",")
			if util.CheckProtocol(egw[0]) == protocol {
				meta.gateway = egw[0]
				if util.CheckProtocol(ips[0]) == protocol {
					localPodIPs[meta] = append(localPodIPs[meta], ips[0])
				} else {
					localPodIPs[meta] = append(localPodIPs[meta], ips[1])
				}
			} else if len(egw) == 2 && len(ips) == 2 {
				meta.gateway = egw[1]
				localPodIPs[meta] = append(localPodIPs[meta], ips[1])
			}
		}
	}

	return localPodIPs, nil
}

func (c *Controller) getSubnetsNeedPR(protocol string) (map[policyRouteMeta]string, error) {
	subnetsNeedPR := make(map[policyRouteMeta]string)
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
		return nil, err
	}

	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node %s: %v", c.config.NodeName, err)
		return nil, err
	}

	for _, subnet := range subnets {
		if !subnet.DeletionTimestamp.IsZero() ||
			subnet.Spec.ExternalEgressGateway == "" ||
			(subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) ||
			subnet.Spec.GatewayType != kubeovnv1.GWCentralizedType ||
			subnet.Spec.Vpc != c.config.ClusterRouter ||
			(subnet.Spec.Protocol != kubeovnv1.ProtocolDual && subnet.Spec.Protocol != protocol) {
			continue
		}

		isGatewayNode := util.GatewayContains(subnet.Spec.GatewayNode, c.config.NodeName) ||
			(subnet.Spec.GatewayNode == "" && util.MatchLabelSelectors(subnet.Spec.GatewayNodeSelectors, node.Labels))
		if !isGatewayNode {
			continue
		}

		meta := policyRouteMeta{
			priority: subnet.Spec.PolicyRoutingPriority,
			tableID:  subnet.Spec.PolicyRoutingTableID,
		}
		egw := strings.Split(subnet.Spec.ExternalEgressGateway, ",")
		if util.CheckProtocol(subnet.Spec.CIDRBlock) == kubeovnv1.ProtocolDual && protocol == kubeovnv1.ProtocolIPv6 {
			if len(egw) == 2 {
				meta.gateway = egw[1]
			} else if util.CheckProtocol(egw[0]) == protocol {
				meta.gateway = egw[0]
			}
		} else {
			meta.gateway = egw[0]
		}
		if meta.gateway != "" {
			cidrBlock, err := getCidrByProtocol(subnet.Spec.CIDRBlock, protocol)
			if err == nil && cidrBlock != "" {
				subnetsNeedPR[meta] = cidrBlock
			}
		}
	}

	return subnetsNeedPR, nil
}

func (c *Controller) deleteObsoleteSnatRules(ipt *iptables.IPTables, table, chain string) error {
	rules, err := ipt.List(table, chain)
	if err != nil {
		klog.Errorf("failed to list iptables rules in table %v chain %v, %+v", table, chain, err)
		return err
	}

	for _, rule := range rules {
		if !strings.Contains(rule, "--to-source") {
			continue
		}

		// "-A POSTROUTING -s 100.168.10.0/24 -m set ! --match-set ovn40subnets dst -j SNAT --to-source 172.17.0.3"
		rule := rule[4+len(chain):]
		spec := util.DoubleQuotedFields(rule)
		if err = ipt.Delete(table, chain, spec...); err != nil {
			klog.Errorf(`failed to delete iptables rule "%s": %v`, rule, err)
			return err
		}
	}

	return nil
}

func (c *Controller) ipsetExists(name string) (bool, error) {
	sets, err := c.k8sipsets.ListSets()
	if err != nil {
		return false, fmt.Errorf("failed to list ipset names: %w", err)
	}

	return slices.Contains(sets, name), nil
}

func getNatOutGoingPolicyRuleIPSetName(ruleID, srcOrDst, protocol string, hasPrefix bool) string {
	prefix := ""

	if hasPrefix {
		prefix = "ovn40"
		if protocol == kubeovnv1.ProtocolIPv6 {
			prefix = "ovn60"
		}
	}

	return prefix + NatOutGoingPolicyRuleSet + fmt.Sprintf("%s-%s", ruleID, srcOrDst)
}

func isNatOutGoingPolicyRuleIPSet(ipsetName string) bool {
	return strings.HasPrefix(ipsetName, "ovn40"+NatOutGoingPolicyRuleSet) ||
		strings.HasPrefix(ipsetName, "ovn60"+NatOutGoingPolicyRuleSet)
}

func getNatOutGoingPolicyRuleIPSetItem(ipsetName string) (string, string) {
	items := strings.Split(ipsetName[len("ovn40")+len(NatOutGoingPolicyRuleSet):], "-")
	ruleID := items[0]
	srcOrDst := items[1]
	return ruleID, srcOrDst
}

func getNatPolicySubnetChainUID(chainName string) string {
	return chainName[len(OvnNatOutGoingPolicySubnet):]
}

func formatIPsetUnPrefix(ipsetName string) string {
	return ipsetName[len("ovn40"):]
}
