package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"
	"syscall"

	"github.com/alauda/felix/ipsets"
	"github.com/kubeovn/go-iptables/iptables"
	"github.com/vishvananda/netlink"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	k8sipset "k8s.io/kubernetes/pkg/util/ipset"
	k8sexec "k8s.io/utils/exec"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	ServiceSet             = "services"
	SubnetSet              = "subnets"
	SubnetNatSet           = "subnets-nat"
	SubnetDistributedGwSet = "subnets-distributed-gw"
	LocalPodSet            = "local-pod-ip-nat"
	OtherNodeSet           = "other-node"
	IPSetPrefix            = "ovn"
)

const (
	NAT            = "nat"
	Prerouting     = "PREROUTING"
	Postrouting    = "POSTROUTING"
	OvnPrerouting  = "OVN-PREROUTING"
	OvnPostrouting = "OVN-POSTROUTING"
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
		c.ipsets[protocol].ApplyUpdates()
	}
	return nil
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
		Protocol: netlink.RouteProtocol(family),
		Gw:       net.ParseIP(gateway),
		Table:    int(tableID),
	}
	if err := netlink.RouteReplace(route); err != nil && !errors.Is(err, syscall.EEXIST) {
		err = fmt.Errorf("failed to replace route in table %d: %+v", tableID, err)
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
				err = fmt.Errorf("failed to add route in table %d: %+v", tableID, err)
				klog.Error(err)
				return err
			}
		} else {
			rule.Src = &net.IPNet{IP: net.ParseIP(ip), Mask: mask}
		}

		if err := netlink.RuleAdd(rule); err != nil && !errors.Is(err, syscall.EEXIST) {
			err = fmt.Errorf("failed to add network rule: %+v", err)
			klog.Error(err)
			return err
		}
	}

	return nil
}

func (c *Controller) deletePolicyRouting(family int, gateway string, priority, tableID uint32, ips ...string) error {
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
				err = fmt.Errorf("failed to delete route in table %d: %+v", tableID, err)
				klog.Error(err)
				return err
			}
		} else {
			rule.Src = &net.IPNet{IP: net.ParseIP(ip), Mask: mask}
		}

		if err := netlink.RuleDel(rule); err != nil && !errors.Is(err, syscall.ENOENT) {
			err = fmt.Errorf("failed to delete network rule: %+v", err)
			klog.Error(err)
			return err
		}
	}

	// routes may be used by other Pods so delete rules only
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
		klog.V(3).Infof(`iptables rule %q already exists`, s)
		return nil
	}

	klog.Infof(`creating iptables rules: "%s"`, s)
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
		if i-added < len(existingRules) && reflect.DeepEqual(existingRules[i-added], rule.Rule) {
			klog.V(5).Infof("iptables rule %v already exists", rule.Rule)
			continue
		}
		if err = ipt.Insert(table, chain, i+1, rule.Rule...); err != nil {
			klog.Errorf(`failed to insert iptables rule %v: %v`, rule.Rule, err)
			return err
		}
		klog.Infof(`created iptables rule %v`, rule.Rule)
		added++
	}
	for i := len(existingRules) - 1; i >= len(rules)-added; i-- {
		if err = ipt.Delete(table, chain, strconv.Itoa(i+added+1)); err != nil {
			klog.Errorf(`failed to delete iptables rule %v: %v`, existingRules[i], err)
			return err
		}
		klog.Infof("deleted iptables rule %v", existingRules[i])
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

	centralGwNatIPs, err := c.getEgressNatIpByNode(c.config.NodeName)
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
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m mark --mark 0x4000/0x4000 -j MASQUERADE`)},
			// nat service traffic
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m set --match-set ovn40subnets src -m set --match-set ovn40subnets dst -j MASQUERADE`)},
			// do not nat node port service traffic with external traffic policy set to local
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m mark --mark 0x80000/0x80000 -m set --match-set ovn40subnets-distributed-gw dst -j RETURN`)},
			// nat node port service traffic with external traffic policy set to local for subnets with centralized gateway
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m mark --mark 0x80000/0x80000 -j MASQUERADE`)},
			// do not nat reply packets in direct routing
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-p tcp -m tcp --tcp-flags SYN NONE -m conntrack --ctstate NEW -j RETURN`)},
			// do not nat route traffic
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m set ! --match-set ovn40subnets src -m set ! --match-set ovn40other-node src -m set --match-set ovn40subnets-nat dst -j RETURN`)},
			// nat outgoing
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m set --match-set ovn40subnets-nat src -m set ! --match-set ovn40subnets dst -j MASQUERADE`)},
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
		}
		v6Rules = []util.IPTableRule{
			// mark packets from pod to service
			{Table: NAT, Chain: OvnPrerouting, Rule: strings.Fields(`-i ovn0 -m set --match-set ovn60subnets src -m set --match-set ovn60services dst -j MARK --set-xmark 0x4000/0x4000`)},
			// nat packets marked by kube-proxy or kube-ovn
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m mark --mark 0x4000/0x4000 -j MASQUERADE`)},
			// nat service traffic
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m set --match-set ovn60subnets src -m set --match-set ovn60subnets dst -j MASQUERADE`)},
			// do not nat node port service traffic with external traffic policy set to local
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m mark --mark 0x80000/0x80000 -m set --match-set ovn60subnets-distributed-gw dst -j RETURN`)},
			// nat node port service traffic with external traffic policy set to local for subnets with centralized gateway
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m mark --mark 0x80000/0x80000 -j MASQUERADE`)},
			// do not nat reply packets in direct routing
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-p tcp -m tcp --tcp-flags SYN NONE -m conntrack --ctstate NEW -j RETURN`)},
			// do not nat route traffic
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m set ! --match-set ovn60subnets src -m set ! --match-set ovn60other-node src -m set --match-set ovn60subnets-nat dst -j RETURN`)},
			// nat outgoing
			{Table: NAT, Chain: OvnPostrouting, Rule: strings.Fields(`-m set --match-set ovn60subnets-nat src -m set ! --match-set ovn60subnets dst -j MASQUERADE`)},
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
		}
	)
	protocols := make([]string, 2)
	if c.protocol == kubeovnv1.ProtocolDual {
		protocols[0] = kubeovnv1.ProtocolIPv4
		protocols[1] = kubeovnv1.ProtocolIPv6
	} else {
		protocols[0] = c.protocol
	}

	for _, protocol := range protocols {
		ipt := c.iptables[protocol]
		if ipt == nil {
			continue
		}

		var kubeProxyIpsetProtocol, matchset, svcMatchset string
		var obsoleteRules, iptablesRules []util.IPTableRule
		if protocol == kubeovnv1.ProtocolIPv4 {
			iptablesRules = v4Rules
			matchset, svcMatchset = "ovn40subnets", "ovn40services"
		} else {
			iptablesRules = v6Rules
			kubeProxyIpsetProtocol, matchset, svcMatchset = "6-", "ovn60subnets", "ovn60services"
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
				ipsetExists, err := ipsetExists(ipset)
				if err != nil {
					klog.Error("failed to check existence of ipset %s: %v", ipset, err)
					return err
				}
				if !ipsetExists {
					klog.V(5).Infof("ipset %s does not exist", ipset)
					continue
				}
				rule := fmt.Sprintf("-p %s -m addrtype --dst-type LOCAL -m set --match-set %s dst -j MARK --set-xmark 0x80000/0x80000", p, ipset)
				obsoleteRules = append(obsoleteRules, util.IPTableRule{Table: NAT, Chain: Prerouting, Rule: strings.Fields(rule)})
				iptablesRules = append(iptablesRules, util.IPTableRule{Table: NAT, Chain: OvnPrerouting, Rule: strings.Fields(rule)})
			}
		}

		_, subnetCidrs, err := c.getDefaultVpcSubnetsCIDR(protocol)
		if err != nil {
			klog.Errorf("get subnets failed, %+v", err)
			return err
		}

		for name, subnetCidr := range subnetCidrs {
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

		for _, rule := range rules {
			if !strings.Contains(rule, util.OvnSubnetGatewayIptables) {
				continue
			}

			var inUse bool
			for name := range subnetCidrs {
				if util.ContainsString(util.DoubleQuotedFields(rule), fmt.Sprintf("%s,%s", util.OvnSubnetGatewayIptables, name)) {
					inUse = true
					break
				}
			}

			if !inUse {
				// rule[11:] skip "-A FORWARD "
				if err = deleteIptablesRule(ipt, util.IPTableRule{Table: "filter", Chain: "FORWARD", Rule: util.DoubleQuotedFields(rule[11:])}); err != nil {
					klog.Error(err)
					return err
				}
			}
		}

		var natPreroutingRules, natPostroutingRules []util.IPTableRule
		for _, rule := range iptablesRules {
			if rule.Table == NAT {
				switch rule.Chain {
				case OvnPrerouting:
					natPreroutingRules = append(natPreroutingRules, rule)
					continue
				case OvnPostrouting:
					if util.ContainsString(rule.Rule, "MASQUERADE") && c.k8siptables[protocol].HasRandomFully() {
						// https://github.com/kubeovn/kube-ovn/issues/2641
						// Work around Linux kernel bug that sometimes causes multiple flows to
						// get mapped to the same IP:PORT and consequently some suffer packet
						// drops.
						rule.Rule = append(rule.Rule, "--random-fully")
					}
					natPostroutingRules = append(natPostroutingRules, rule)
					continue
				}
			}

			if err = c.createIptablesRule(ipt, rule); err != nil {
				klog.Errorf(`failed to create iptables rule "%s": %v`, strings.Join(rule.Rule, " "), err)
				return err
			}
		}

		// add iptables rule for nat gw with designative ip in centralized subnet
		for cidr, ip := range centralGwNatIPs {
			if util.CheckProtocol(cidr) != protocol {
				continue
			}

			s := fmt.Sprintf("-s %s -m set ! --match-set %s dst -j SNAT --to-source %s", cidr, matchset, ip)
			rule := util.IPTableRule{
				Table: NAT,
				Chain: OvnPostrouting,
				Rule:  util.DoubleQuotedFields(s),
			}
			// insert the rule before the one for nat outgoing
			n := len(natPostroutingRules)
			natPostroutingRules = append(natPostroutingRules[:n-1], rule, natPostroutingRules[n-1])
		}

		if err = c.updateIptablesChain(ipt, NAT, OvnPrerouting, Prerouting, natPreroutingRules); err != nil {
			klog.Errorf("failed to update chain %s/%s: %v", NAT, OvnPrerouting)
			return err
		}
		if err = c.updateIptablesChain(ipt, NAT, OvnPostrouting, Postrouting, natPostroutingRules); err != nil {
			klog.Errorf("failed to update chain %s/%s: %v", NAT, OvnPostrouting)
			return err
		}

		if err = c.cleanObsoleteIptablesRules(protocol, obsoleteRules); err != nil {
			klog.Errorf("failed to clean legacy iptables rules: %v", err)
			return err
		}
	}
	return nil
}

func deleteIptablesRule(ipt *iptables.IPTables, rule util.IPTableRule) error {
	if err := ipt.DeleteIfExists(rule.Table, rule.Chain, rule.Rule...); err != nil {
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
		return err
	}
	if err = clearObsoleteIptablesChain(ipt, NAT, OvnPostrouting, Postrouting); err != nil {
		return err
	}

	delete(c.iptablesObsolete, protocol)
	if len(c.iptablesObsolete) == 0 {
		c.iptablesObsolete = nil
	}
	return nil
}

func (c *Controller) setOvnSubnetGatewayMetric() {
	hostname := os.Getenv(util.HostnameEnv)
	for proto, iptables := range c.iptables {
		rules, err := iptables.ListWithCounters("filter", "FORWARD")
		if err != nil {
			klog.Errorf("get proto %s iptables failed with err %v ", proto, err)
			continue
		}

		for _, rule := range rules {
			items := util.DoubleQuotedFields(rule)
			cidr := ""
			direction := ""
			subnetName := ""
			var currentPackets, currentPacketBytes int
			if len(items) <= 10 {
				continue
			}
			for _, item := range items {
				if strings.Contains(item, util.OvnSubnetGatewayIptables) {
					cidr = items[3]
					if items[2] == "-s" {
						direction = "egress"
					} else if items[2] == "-d" {
						direction = "ingress"
					} else {
						break
					}

					comments := strings.Split(items[7], ",")
					if len(comments) != 2 {
						break
					}
					subnetName = comments[1][:len(comments[1])-1]
					currentPackets, err = strconv.Atoi(items[9])
					if err != nil {
						break
					}
					currentPacketBytes, err = strconv.Atoi(items[10])
					if err != nil {
						break
					}
				}
			}

			proto := util.CheckProtocol(cidr)

			if cidr == "" || direction == "" || subnetName == "" && proto != "" {
				continue
			}

			lastPacketBytes := 0
			lastPackets := 0
			diffPacketBytes := 0
			diffPackets := 0

			key := strings.Join([]string{subnetName, direction, proto}, "/")
			if ret, ok := c.gwCounters[key]; ok {
				lastPackets = ret.Packets
				lastPacketBytes = ret.PacketBytes
			} else {
				c.gwCounters[key] = &util.GwIPtableCounters{
					Packets:     lastPackets,
					PacketBytes: lastPacketBytes,
				}
			}

			if lastPacketBytes == 0 && lastPackets == 0 {
				// the gwCounters may just initialize don't cal the diff values,
				// it may loss packets to calculate during a metric period
				c.gwCounters[key].Packets = currentPackets
				c.gwCounters[key].PacketBytes = currentPacketBytes
				continue
			}

			if currentPackets >= lastPackets && currentPacketBytes >= lastPacketBytes {
				diffPacketBytes = currentPacketBytes - lastPacketBytes
				diffPackets = currentPackets - lastPackets
			} else {
				// if currentPacketBytes < lastPacketBytes, the reason is that iptables rule is reset ,
				// it may loss packets to calculate during a metric period
				c.gwCounters[key].Packets = currentPackets
				c.gwCounters[key].PacketBytes = currentPacketBytes
				continue
			}

			c.gwCounters[key].Packets = currentPackets
			c.gwCounters[key].PacketBytes = currentPacketBytes

			klog.V(3).Infof(`hostname %s key %s cidr %s direction %s proto %s has diffPackets %d diffPacketBytes %d currentPackets %d currentPacketBytes %d lastPackets %d lastPacketBytes %d`,
				hostname, key, cidr, direction, proto, diffPackets, diffPacketBytes, currentPackets, currentPacketBytes, lastPackets, lastPacketBytes)
			if diffPackets > 0 {
				metricOvnSubnetGatewayPackets.WithLabelValues(hostname, key, cidr, direction, proto).Add(float64(diffPackets))
			}
			if diffPacketBytes > 0 {
				metricOvnSubnetGatewayPacketBytes.WithLabelValues(hostname, key, cidr, direction, proto).Add(float64(diffPacketBytes))
			}
		}
	}
}

func (c *Controller) addEgressConfig(subnet *kubeovnv1.Subnet, ip string) error {
	if (subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) ||
		subnet.Spec.GatewayType != kubeovnv1.GWDistributedType ||
		subnet.Spec.Vpc != util.DefaultVpc {
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
		podSubnet.Spec.Vpc != util.DefaultVpc {
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
	enable := node.Labels[util.ExGatewayLabel]
	externalBride := fmt.Sprintf("br-%s", c.config.ExternalGatewaySwitch)
	if enable == "true" {
		cm, err := c.config.KubeClient.CoreV1().ConfigMaps(c.config.ExternalGatewayConfigNS).Get(context.Background(), util.ExternalGatewayConfig, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get ovn-external-gw-config, %v", err)
			return err
		}
		// enable external-gw-config without 'external-gw-nic' configured
		// to reuse existing physical network from arg 'external-gateway-net'
		linkName, exist := cm.Data["external-gw-nic"]
		if !exist || len(linkName) == 0 {
			return nil
		}
		link, err := netlink.LinkByName(cm.Data["external-gw-nic"])
		if err != nil {
			klog.Errorf("failed to get nic %s, %v", cm.Data["external-gw-nic"], err)
			return err
		}
		if err := netlink.LinkSetUp(link); err != nil {
			klog.Errorf("failed to set gateway nic %s up, %v", cm.Data["external-gw-nic"], err)
			return err
		}
		externalBrReady := false
		// if external nic already attached into another bridge
		if existBr, err := ovs.Exec("port-to-br", cm.Data["external-gw-nic"]); err == nil {
			if existBr == externalBride {
				externalBrReady = true
			} else {
				klog.Infof("external bridge should change from %s to %s, delete external bridge %s", existBr, externalBride, existBr)
				if _, err := ovs.Exec(ovs.IfExists, "del-br", existBr); err != nil {
					err = fmt.Errorf("failed to del external br %s, %v", existBr, err)
					klog.Error(err)
					return err
				}
			}
		}

		if !externalBrReady {
			if _, err := ovs.Exec(
				ovs.MayExist, "add-br", externalBride, "--",
				ovs.MayExist, "add-port", externalBride, cm.Data["external-gw-nic"],
			); err != nil {
				err = fmt.Errorf("failed to enable external gateway, %v", err)
				klog.Error(err)
			}
		}
		output, err := ovs.Exec(ovs.IfExists, "get", "open", ".", "external-ids:ovn-bridge-mappings")
		if err != nil {
			err = fmt.Errorf("failed to get external-ids, %v", err)
			klog.Error(err)
			return err
		}
		bridgeMappings := fmt.Sprintf("external:%s", externalBride)
		if output != "" && !util.IsStringIn(bridgeMappings, strings.Split(output, ",")) {
			bridgeMappings = fmt.Sprintf("%s,%s", output, bridgeMappings)
		}

		output, err = ovs.Exec("set", "open", ".", fmt.Sprintf("external-ids:ovn-bridge-mappings=%s", bridgeMappings))
		if err != nil {
			err = fmt.Errorf("failed to set bridge-mappings, %v: %q", err, output)
			klog.Error(err)
			return err
		}
	} else {
		brExists, err := ovs.BridgeExists(externalBride)
		if err != nil {
			return fmt.Errorf("failed to check OVS bridge existence: %v", err)
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
				if existBr == externalBride {
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

		if !keepExternalSubnet {
			klog.Infof("delete external bridge %s", externalBride)
			if _, err := ovs.Exec(
				ovs.IfExists, "del-br", externalBride); err != nil {
				err = fmt.Errorf("failed to disable external gateway, %v", err)
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

	nodeName := os.Getenv(util.HostnameEnv)
	localPodIPs := make(map[policyRouteMeta][]string)
	for _, pod := range allPods {
		if pod.Spec.HostNetwork ||
			pod.DeletionTimestamp != nil ||
			pod.Spec.NodeName != nodeName ||
			pod.Annotations[util.LogicalSwitchAnnotation] == "" ||
			pod.Annotations[util.IpAddressAnnotation] == "" {
			continue
		}

		subnet, err := c.subnetsLister.Get(pod.Annotations[util.LogicalSwitchAnnotation])
		if err != nil {
			klog.Errorf("failed to get subnet %s: %+v", pod.Annotations[util.LogicalSwitchAnnotation], err)
			continue
		}

		if subnet.Spec.ExternalEgressGateway == "" ||
			subnet.Spec.Vpc != util.DefaultVpc ||
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
			ipv4, ipv6 := util.SplitStringIP(pod.Annotations[util.IpAddressAnnotation])
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

	for _, subnet := range subnets {
		if subnet.DeletionTimestamp == nil &&
			subnet.Spec.ExternalEgressGateway != "" &&
			(subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway) &&
			subnet.Spec.GatewayType == kubeovnv1.GWCentralizedType &&
			util.GatewayContains(subnet.Spec.GatewayNode, c.config.NodeName) &&
			subnet.Spec.Vpc == util.DefaultVpc &&
			(subnet.Spec.Protocol == kubeovnv1.ProtocolDual || subnet.Spec.Protocol == protocol) {
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
				cidrBlock := getCidrByProtocol(subnet.Spec.CIDRBlock, protocol)
				subnetsNeedPR[meta] = cidrBlock
			}
		}
	}

	return subnetsNeedPR, nil
}

// Generally, the MTU of the interface is set to 1400. But in special cases, a special pod (docker indocker) will introduce the docker0 interface to the pod. The MTU of docker0 is 1500.
// The network application in pod will calculate the TCP MSS according to the MTU of docker0, and then initiate communication with others. After the other party sends a response, the kernel protocol stack of Linux host will send ICMP unreachable message to the other party, indicating that IP fragmentation is needed, which is not supported by the other party, resulting in communication failure.
func (c *Controller) appendMssRule() {
	if c.config.Iface != "" && c.config.MSS > 0 {
		iface, err := findInterface(c.config.Iface)
		if err != nil {
			klog.Errorf("failed to findInterface, %v", err)
			return
		}
		rule := fmt.Sprintf("-p tcp --tcp-flags SYN,RST SYN -o %s -j TCPMSS --set-mss %d", iface.Name, c.config.MSS)
		MssMangleRule := util.IPTableRule{
			Table: "mangle",
			Chain: Postrouting,
			Rule:  strings.Fields(rule),
		}

		switch c.protocol {
		case kubeovnv1.ProtocolIPv4:
			c.updateMssRuleByProtocol(c.protocol, MssMangleRule)
		case kubeovnv1.ProtocolIPv6:
			c.updateMssRuleByProtocol(c.protocol, MssMangleRule)
		case kubeovnv1.ProtocolDual:
			c.updateMssRuleByProtocol(kubeovnv1.ProtocolIPv4, MssMangleRule)
			c.updateMssRuleByProtocol(kubeovnv1.ProtocolIPv6, MssMangleRule)
		}
	}
}

func (c *Controller) updateMssRuleByProtocol(protocol string, MssMangleRule util.IPTableRule) {
	exists, err := c.iptables[protocol].Exists(MssMangleRule.Table, MssMangleRule.Chain, MssMangleRule.Rule...)
	if err != nil {
		klog.Errorf("check iptables rule %v failed, %+v", MssMangleRule.Rule, err)
		return
	}

	if !exists {
		klog.Infof("iptables rules %s not exist, append iptables rules", strings.Join(MssMangleRule.Rule, " "))
		if err := c.iptables[protocol].Append(MssMangleRule.Table, MssMangleRule.Chain, MssMangleRule.Rule...); err != nil {
			klog.Errorf("append iptables rule %v failed, %+v", MssMangleRule.Rule, err)
			return
		}
	}
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

func ipsetExists(name string) (bool, error) {
	sets, err := k8sipset.New(k8sexec.New()).ListSets()
	if err != nil {
		return false, fmt.Errorf("failed to list ipset names: %v", err)
	}

	return util.ContainsString(sets, name), nil
}
