package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/alauda/felix/ipsets"
	"github.com/vishvananda/netlink"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

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
		subnets, err := c.getDefaultVpcSubnetsCIDR(protocol)
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
	if err := netlink.RouteAdd(route); err != nil && !errors.Is(err, syscall.EEXIST) {
		err = fmt.Errorf("failed to add route in table %d: %+v", tableID, err)
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

	centralGwNatips, err := c.getEgressNatIpByNode(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get centralized subnets nat ips on node %s, %v", c.config.NodeName, err)
		return err
	}
	klog.V(3).Infof("centralized subnets nat ips %v", centralGwNatips)

	var (
		v4AbandonedRules = []util.IPTableRule{
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Fields(`-m mark --mark 0x40000/0x40000 -j MASQUERADE`)},
			{Table: "mangle", Chain: "PREROUTING", Rule: strings.Fields(`-i ovn0 -m set --match-set ovn40subnets src -m set --match-set ovn40services dst -j MARK --set-xmark 0x40000/0x40000`)},
		}
		v6AbandonedRules = []util.IPTableRule{
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Fields(`-m mark --mark 0x40000/0x40000 -j MASQUERADE`)},
			{Table: "mangle", Chain: "PREROUTING", Rule: strings.Fields(`-i ovn0 -m set --match-set ovn60subnets src -m set --match-set ovn60services dst -j MARK --set-xmark 0x40000/0x40000`)},
		}

		v4Rules = []util.IPTableRule{
			// nat packets marked by kube-proxy or kube-ovn
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Fields(`-m mark --mark 0x4000/0x4000 -j MASQUERADE`)},
			// do not nat node port service traffic with external traffic policy set to local
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Fields(`-m mark --mark 0x80000/0x80000 -m set --match-set ovn40subnets-distributed-gw dst -j RETURN`)},
			// nat node port service traffic with external traffic policy set to local for subnets with centralized gateway
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Fields(`-m mark --mark 0x80000/0x80000 -j MASQUERADE`)},
			// do not nat reply packets in direct routing
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Fields(`-p tcp --tcp-flags SYN NONE -m conntrack --ctstate NEW -j RETURN`)},
			// do not nat route traffic
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Fields(`-m set ! --match-set ovn40subnets src -m set ! --match-set ovn40other-node src -m set --match-set ovn40subnets-nat dst -j RETURN`)},
			// nat outgoing
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Fields(`-m set --match-set ovn40subnets-nat src -m set ! --match-set ovn40subnets dst -j MASQUERADE`)},
			// mark packets from pod to service
			{Table: "mangle", Chain: "PREROUTING", Rule: strings.Fields(`-i ovn0 -m set --match-set ovn40subnets src -m set --match-set ovn40services dst -j MARK --set-xmark 0x4000/0x4000`)},
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
			// nat packets marked by kube-proxy or kube-ovn
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Fields(`-m mark --mark 0x4000/0x4000 -j MASQUERADE`)},
			// do not nat node port service traffic with external traffic policy set to local
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Fields(`-m mark --mark 0x80000/0x80000 -m set --match-set ovn60subnets-distributed-gw dst -j RETURN`)},
			// nat node port service traffic with external traffic policy set to local for subnets with centralized gateway
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Fields(`-m mark --mark 0x80000/0x80000 -j MASQUERADE`)},
			// do not nat reply packets in direct routing
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Fields(`-p tcp --tcp-flags SYN NONE -m conntrack --ctstate NEW -j RETURN`)},
			// do not nat route traffic
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Fields(`-m set ! --match-set ovn60subnets src -m set ! --match-set ovn60other-node src -m set --match-set ovn60subnets-nat dst -j RETURN`)},
			// nat outgoing
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Fields(`-m set --match-set ovn60subnets-nat src -m set ! --match-set ovn60subnets dst -j MASQUERADE`)},
			// mark packets from pod to service
			{Table: "mangle", Chain: "PREROUTING", Rule: strings.Fields(`-i ovn0 -m set --match-set ovn60subnets src -m set --match-set ovn60services dst -j MARK --set-xmark 0x4000/0x4000`)},
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
		if c.iptables[protocol] == nil {
			continue
		}
		// delete unused iptables rule when nat gw with designative ip has been changed in centralized subnet
		if err = c.deleteUnusedIptablesRule(protocol, "nat", "POSTROUTING", centralGwNatips); err != nil {
			klog.Errorf("failed to delete iptables rule on node %s, maybe can delete manually, %v", c.config.NodeName, err)
			return err
		}

		var kubeProxyIpsetProtocol, matchset string
		var abandonedRules, iptablesRules []util.IPTableRule
		if protocol == kubeovnv1.ProtocolIPv4 {
			iptablesRules, abandonedRules = v4Rules, v4AbandonedRules
			matchset = "ovn40subnets"
		} else {
			iptablesRules, abandonedRules = v6Rules, v6AbandonedRules
			kubeProxyIpsetProtocol, matchset = "6-", "ovn60subnets"
		}

		kubeProxyIpsets := map[string]string{
			"tcp": fmt.Sprintf("KUBE-%sNODE-PORT-LOCAL-TCP", kubeProxyIpsetProtocol),
			"udp": fmt.Sprintf("KUBE-%sNODE-PORT-LOCAL-UDP", kubeProxyIpsetProtocol),
		}

		if nodeIP := nodeIPs[protocol]; nodeIP != "" {
			abandonedRules = append(abandonedRules,
				util.IPTableRule{Table: "nat", Chain: "POSTROUTING", Rule: strings.Fields(fmt.Sprintf(`! -s %s -m set --match-set %s dst -j MASQUERADE`, nodeIP, matchset))},
				util.IPTableRule{Table: "nat", Chain: "POSTROUTING", Rule: strings.Fields(fmt.Sprintf(`! -s %s -m mark --mark 0x4000/0x4000 -j MASQUERADE`, nodeIP))},
				util.IPTableRule{Table: "nat", Chain: "POSTROUTING", Rule: strings.Fields(fmt.Sprintf(`! -s %s -m set ! --match-set %s src -m set --match-set %s dst -j MASQUERADE`, nodeIP, matchset, matchset))},
			)

			chainExists, err := c.iptables[protocol].ChainExists("nat", "KUBE-NODE-PORT")
			if err != nil {
				klog.Errorf("failed to check existence of chain KUBE-NODE-PORT in nat table: %v", err)
				return err
			}
			if chainExists {
				nodePortRules := make([]util.IPTableRule, 0, len(kubeProxyIpsets))
				for protocol, ipset := range kubeProxyIpsets {
					ipsetExists, err := ipsetExists(ipset)
					if err != nil {
						klog.Error("failed to check existence of ipset %s: %v", ipset, err)
						return err
					}
					if !ipsetExists {
						klog.Warningf("ipset %s does not exist", ipset)
						continue
					}
					nodePortRules = append(nodePortRules, util.IPTableRule{Table: "nat", Chain: "KUBE-NODE-PORT", Rule: strings.Fields(fmt.Sprintf("-p %s -m set --match-set %s dst -j MARK --set-xmark 0x80000/0x80000", protocol, ipset))})
				}
				iptablesRules = append(nodePortRules, iptablesRules...)
			}
		}

		// delete abandoned iptables rules
		for _, iptRule := range abandonedRules {
			exists, err := c.iptables[protocol].Exists(iptRule.Table, iptRule.Chain, iptRule.Rule...)
			if err != nil {
				klog.Errorf("failed to check existence of iptables rule: %v", err)
				return err
			}
			if exists {
				klog.Infof("deleting abandoned iptables rule: %s", strings.Join(iptRule.Rule, " "))
				if err := c.iptables[protocol].Delete(iptRule.Table, iptRule.Chain, iptRule.Rule...); err != nil {
					klog.Errorf("failed to delete iptables rule %s: %v", strings.Join(iptRule.Rule, " "), err)
					return err
				}
			}
		}

		// add iptables rule for nat gw with designative ip in centralized subnet
		for cidr, natip := range centralGwNatips {
			if util.CheckProtocol(cidr) != protocol {
				continue
			}

			ruleval := fmt.Sprintf("-s %v -m set ! --match-set %s dst -j SNAT --to-source %v", cidr, matchset, natip)
			rule := util.IPTableRule{
				Table: "nat",
				Chain: "POSTROUTING",
				Rule:  strings.Fields(ruleval),
			}
			iptablesRules = append(iptablesRules, rule)
		}

		// reverse rules in nat table
		var idx int
		for idx = range iptablesRules {
			if iptablesRules[idx].Table != "nat" {
				break
			}
		}
		for i := 0; i < idx/2; i++ {
			iptablesRules[i], iptablesRules[idx-1-i] = iptablesRules[idx-1-i], iptablesRules[i]
		}

		for _, iptRule := range iptablesRules {
			exists, err := c.iptables[protocol].Exists(iptRule.Table, iptRule.Chain, iptRule.Rule...)
			if err != nil {
				klog.Errorf("check iptables rule exist failed, %+v", err)
				return err
			}
			if !exists {
				klog.Infof("iptables rules %s not exist, recreate iptables rules", strings.Join(iptRule.Rule, " "))
				if err := c.iptables[protocol].Insert(iptRule.Table, iptRule.Chain, 1, iptRule.Rule...); err != nil {
					klog.Errorf("insert iptables rule %s failed, %+v", strings.Join(iptRule.Rule, " "), err)
					return err
				}
			}
			klog.V(3).Infof("iptables rules %v, exists %v", strings.Join(iptRule.Rule, " "), exists)
		}
	}
	return nil
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
	if enable == "true" {
		cm, err := c.config.KubeClient.CoreV1().ConfigMaps(c.config.ExternalGatewayConfigNS).Get(context.Background(), util.ExternalGatewayConfig, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get ovn-external-gw-config, %v", err)
			return err
		}
		// enable external-gw-config without 'external-gw-nic' configured
		// to reuse existing physical network from arg 'external-gateway-net'
		linkname, exist := cm.Data["external-gw-nic"]
		if !exist || len(linkname) == 0 {
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
		if _, err := ovs.Exec(
			ovs.MayExist, "add-br", "br-external", "--",
			ovs.MayExist, "add-port", "br-external", cm.Data["external-gw-nic"],
		); err != nil {
			return fmt.Errorf("failed to enable external gateway, %v", err)
		}

		output, err := ovs.Exec(ovs.IfExists, "get", "open", ".", "external-ids:ovn-bridge-mappings")
		if err != nil {
			return fmt.Errorf("failed to get external-ids, %v", err)
		}
		bridgeMappings := "external:br-external"
		if output != "" && !util.IsStringIn(bridgeMappings, strings.Split(output, ",")) {
			bridgeMappings = fmt.Sprintf("%s,%s", output, bridgeMappings)
		}

		output, err = ovs.Exec("set", "open", ".", fmt.Sprintf("external-ids:ovn-bridge-mappings=%s", bridgeMappings))
		if err != nil {
			return fmt.Errorf("failed to set bridge-mappings, %v: %q", err, output)
		}
	} else {
		if _, err := ovs.Exec(
			ovs.IfExists, "del-br", "br-external"); err != nil {
			return fmt.Errorf("failed to disable external gateway, %v", err)
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

//Generally, the MTU of the interface is set to 1400. But in special cases, a special pod (docker indocker) will introduce the docker0 interface to the pod. The MTU of docker0 is 1500.
//The network application in pod will calculate the TCP MSS according to the MTU of docker0, and then initiate communication with others. After the other party sends a response, the kernel protocol stack of Linux host will send ICMP unreachable message to the other party, indicating that IP fragmentation is needed, which is not supported by the other party, resulting in communication failure.
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
			Chain: "POSTROUTING",
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

func (c *Controller) deleteUnusedIptablesRule(protocol, table, chain string, subnetsNatIps map[string]string) error {
	rules, err := c.iptables[protocol].List(table, chain)
	if err != nil {
		klog.Errorf("failed to list iptables rules in table %v chain %v, %+v", table, chain, err)
		return err
	}

	for _, rule := range rules {
		if !strings.Contains(rule, "--to-source") {
			continue
		}
		// "-A POSTROUTING -s 100.168.10.0/24 -m set ! --match-set ovn40subnets dst -j SNAT --to-source 172.17.0.3"
		rule = strings.TrimPrefix(rule, "-A POSTROUTING ")
		ruleval := strings.Fields(rule)
		dstNatIp := ruleval[len(ruleval)-1]

		found := false
		for cidr, natip := range subnetsNatIps {
			if util.CheckProtocol(cidr) != protocol {
				continue
			}

			if dstNatIp == natip {
				found = true
				break
			}
		}

		if !found {
			num, err := getIptablesRuleNum(table, chain, rule, dstNatIp)
			if err != nil {
				klog.Errorf("failed to get iptables rule num when delete rule %v, please check manually", rule)
				continue
			}

			klog.Infof("iptables rule %v %v %s, num %v should be deleted because nat gw has been changed", table, chain, rule, num)
			if err := c.iptables[protocol].Delete(table, chain, num); err != nil {
				klog.Errorf("delete iptables rule %s failed, %+v", rule, err)
				return err
			}
		}
	}
	return nil
}

func getIptablesRuleNum(table, chain, rule, dstNatIp string) (string, error) {
	var num string
	var err error

	cmdstr := fmt.Sprintf("iptables -t %v -L %v --line-numbers", table, chain)
	cmd := exec.Command("sh", "-c", cmdstr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return num, fmt.Errorf("failed to get iptables rule num: %v", err)
	}

	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, dstNatIp) {
			num = strings.Fields(line)[0]
			klog.Infof("get iptables rule %v num %v", rule, num)
			break
		}
	}
	return num, nil
}

func ipsetExists(name string) (bool, error) {
	result, err := netlink.IpsetListAll()
	if err != nil {
		return false, fmt.Errorf("failed to list ipsets: %v", err)
	}

	for _, ipset := range result {
		if ipset.SetName == name {
			return true, nil
		}
	}
	return false, nil
}
