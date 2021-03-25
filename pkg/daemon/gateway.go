package daemon

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/alauda/felix/ipsets"
	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/ovs"
	"github.com/alauda/kube-ovn/pkg/util"
	"github.com/vishvananda/netlink"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"
)

const (
	SubnetSet    = "subnets"
	SubnetNatSet = "subnets-nat"
	LocalPodSet  = "local-pod-ip-nat"
	OtherNodeSet = "other-node"
	IPSetPrefix  = "ovn"
)

func (c *Controller) runGateway() {
	if err := c.setIPSet(); err != nil {
		klog.Errorf("failed to set gw ipsets")
	}
	if err := c.setIptables(); err != nil {
		klog.Errorf("failed to set gw iptables")
	}

	if err := c.setGatewayBandwidth(); err != nil {
		klog.Errorf("failed to set gw bandwidth, %v", err)
	}
	if err := c.setICGateway(); err != nil {
		klog.Errorf("failed to set ic gateway, %v", err)
	}
	if err := c.setExGateway(); err != nil {
		klog.Errorf("failed to set ex gateway, %v", err)
	}

	c.appendMssRule()
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
		if c.ipset[protocol] == nil {
			continue
		}
		subnets, err := c.getSubnetsCIDR(protocol)
		if err != nil {
			klog.Errorf("get subnets failed, %+v", err)
			return err
		}
		localPodIPs, err := c.getLocalPodIPsNeedNAT(protocol)
		if err != nil {
			klog.Errorf("get local pod ips failed, %+v", err)
			return err
		}
		subnetsNeedNat, err := c.getSubnetsNeedNAT(protocol)
		if err != nil {
			klog.Errorf("get need nat subnets failed, %+v", err)
			return err
		}
		otherNode, err := c.getOtherNodes(protocol)
		if err != nil {
			klog.Errorf("failed to get node, %+v", err)
			return err
		}
		c.ipset[protocol].AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   SubnetSet,
			Type:    ipsets.IPSetTypeHashNet,
		}, subnets)
		c.ipset[protocol].AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   LocalPodSet,
			Type:    ipsets.IPSetTypeHashIP,
		}, localPodIPs)
		c.ipset[protocol].AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   SubnetNatSet,
			Type:    ipsets.IPSetTypeHashNet,
		}, subnetsNeedNat)
		c.ipset[protocol].AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   OtherNodeSet,
			Type:    ipsets.IPSetTypeHashNet,
		}, otherNode)
		c.ipset[protocol].ApplyUpdates()
	}
	return nil
}

func (c *Controller) setIptables() error {
	klog.Infoln("start to set up iptables")
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node %s, %v", c.config.NodeName, err)
		return err
	}

	hostIP := util.GetNodeInternalIP(node)

	var (
		v4Rules = []util.IPTableRule{
			// Prevent performing Masquerade on external traffic which arrives from a Node that owns the Pod/Subnet IP
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(`-m set ! --match-set ovn40subnets src -m set ! --match-set ovn40other-node src -m set --match-set ovn40local-pod-ip-nat dst -j RETURN`, " ")},
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(`-m set ! --match-set ovn40subnets src -m set ! --match-set ovn40other-node src -m set --match-set ovn40subnets-nat dst -j RETURN`, " ")},
			// NAT if pod/subnet to external address
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(`-m set --match-set ovn40local-pod-ip-nat src -m set ! --match-set ovn40subnets dst -j MASQUERADE`, " ")},
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(`-m set --match-set ovn40subnets-nat src -m set ! --match-set ovn40subnets dst -j MASQUERADE`, " ")},
			// masq traffic from hostport/nodeport
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(fmt.Sprintf(`-o ovn0 ! -s %s -j MASQUERADE`, hostIP), " ")},
			// Input Accept
			{Table: "filter", Chain: "FORWARD", Rule: strings.Split(`-m set --match-set ovn40subnets src -j ACCEPT`, " ")},
			{Table: "filter", Chain: "FORWARD", Rule: strings.Split(`-m set --match-set ovn40subnets dst -j ACCEPT`, " ")},
			// Forward Accept
			{Table: "filter", Chain: "INPUT", Rule: strings.Split(`-m set --match-set ovn40subnets src -j ACCEPT`, " ")},
			{Table: "filter", Chain: "INPUT", Rule: strings.Split(`-m set --match-set ovn40subnets dst -j ACCEPT`, " ")},
		}
		v6Rules = []util.IPTableRule{
			// Prevent performing Masquerade on external traffic which arrives from a Node that owns the Pod/Subnet IP
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(`-m set ! --match-set ovn60subnets src -m set ! --match-set ovn60other-node src -m set --match-set ovn60local-pod-ip-nat dst -j RETURN`, " ")},
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(`-m set ! --match-set ovn60subnets src -m set ! --match-set ovn60other-node src -m set --match-set ovn60subnets-nat dst -j RETURN`, " ")},
			// NAT if pod/subnet to external address
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(`-m set --match-set ovn60local-pod-ip-nat src -m set ! --match-set ovn60subnets dst -j MASQUERADE`, " ")},
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(`-m set --match-set ovn60subnets-nat src -m set ! --match-set ovn60subnets dst -j MASQUERADE`, " ")},
			// masq traffic from hostport/nodeport
			{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(fmt.Sprintf(`-o ovn0 ! -s %s -j MASQUERADE`, hostIP), " ")},
			// Input Accept
			{Table: "filter", Chain: "FORWARD", Rule: strings.Split(`-m set --match-set ovn60subnets src -j ACCEPT`, " ")},
			{Table: "filter", Chain: "FORWARD", Rule: strings.Split(`-m set --match-set ovn60subnets dst -j ACCEPT`, " ")},
			// Forward Accept
			{Table: "filter", Chain: "INPUT", Rule: strings.Split(`-m set --match-set ovn60subnets src -j ACCEPT`, " ")},
			{Table: "filter", Chain: "INPUT", Rule: strings.Split(`-m set --match-set ovn60subnets dst -j ACCEPT`, " ")},
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
		if c.iptable[protocol] == nil {
			continue
		}
		iptableRules := make([]util.IPTableRule, len(v4Rules))
		if protocol == kubeovnv1.ProtocolIPv4 {
			iptableRules = v4Rules
		} else {
			iptableRules = v6Rules
		}
		iptableRules[0], iptableRules[1], iptableRules[3], iptableRules[4] =
			iptableRules[4], iptableRules[3], iptableRules[1], iptableRules[0]
		for _, iptRule := range iptableRules {
			if strings.Contains(strings.Join(iptRule.Rule, " "), "ovn0") && protocol != util.CheckProtocol(hostIP) {
				klog.V(3).Infof("ignore check iptable rule, protocol %v, hostIP %v", protocol, hostIP)
				continue
			}

			exists, err := c.iptable[protocol].Exists(iptRule.Table, iptRule.Chain, iptRule.Rule...)
			if err != nil {
				klog.Errorf("check iptable rule exist failed, %+v", err)
				return err
			}
			if !exists {
				klog.Infof("iptables rules %s not exist, recreate iptables rules", strings.Join(iptRule.Rule, " "))
				if err := c.iptable[protocol].Insert(iptRule.Table, iptRule.Chain, 1, iptRule.Rule...); err != nil {
					klog.Errorf("insert iptable rule %s failed, %+v", strings.Join(iptRule.Rule, " "), err)
					return err
				}
			}
		}
	}
	return nil
}

func (c *Controller) setGatewayBandwidth() error {
	node, err := c.config.KubeClient.CoreV1().Nodes().Get(context.Background(), c.config.NodeName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get node, %v", err)
		return err
	}
	ingress, egress := node.Annotations[util.IngressRateAnnotation], node.Annotations[util.EgressRateAnnotation]
	ifaceId := fmt.Sprintf("node-%s", c.config.NodeName)
	return ovs.SetInterfaceBandwidth(ifaceId, egress, ingress)
}

func (c *Controller) setICGateway() error {
	node, err := c.config.KubeClient.CoreV1().Nodes().Get(context.Background(), c.config.NodeName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get node, %v", err)
		return err
	}
	enable := node.Labels[util.ICGatewayLabel]
	if enable == "true" {
		icEnabled, err := ovs.Exec(ovs.IfExists, "get", "open", ".", "external_ids:ovn-is-interconn")
		if err != nil {
			return fmt.Errorf("failed to get if ic enabled, %v", err)
		}
		if strings.Trim(icEnabled, "\"") != "true" {
			if _, err := ovs.Exec("set", "open", ".", "external_ids:ovn-is-interconn=true"); err != nil {
				return fmt.Errorf("failed to enable ic gateway, %v", err)
			}
			output, err := exec.Command("/usr/share/ovn/scripts/ovn-ctl", "restart_controller").CombinedOutput()
			if err != nil {
				return fmt.Errorf("failed to restart ovn-controller, %v, %q", err, output)
			}
		}
	} else {
		if _, err := ovs.Exec("set", "open", ".", "external_ids:ovn-is-interconn=false"); err != nil {
			return fmt.Errorf("failed to disable ic gateway, %v", err)
		}
	}
	return nil
}

func (c *Controller) setExGateway() error {
	node, err := c.config.KubeClient.CoreV1().Nodes().Get(context.Background(), c.config.NodeName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get node, %v", err)
		return err
	}
	enable := node.Labels[util.ExGatewayLabel]
	if enable == "true" {
		cm, err := c.config.KubeClient.CoreV1().ConfigMaps("kube-system").Get(context.Background(), util.ExternalGatewayConfig, metav1.GetOptions{})
		if err != nil && !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to get ovn-external-gw-config, %v", err)
			return err
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
			return fmt.Errorf("failed to set bridg-mappings, %v: %q", err, output)
		}
	} else {
		if _, err := ovs.Exec(
			ovs.IfExists, "del-br", "br-external"); err != nil {
			return fmt.Errorf("failed to disable external gateway, %v", err)
		}
	}
	return nil
}

func (c *Controller) getLocalPodIPsNeedNAT(protocol string) ([]string, error) {
	var localPodIPs []string
	hostname := os.Getenv("KUBE_NODE_NAME")
	allPods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("list pods failed, %+v", err)
		return nil, err
	}
	for _, pod := range allPods {
		if pod.Spec.HostNetwork == true ||
			pod.Status.PodIP == "" ||
			pod.Annotations[util.LogicalSwitchAnnotation] == "" {
			continue
		}
		subnet, err := c.subnetsLister.Get(pod.Annotations[util.LogicalSwitchAnnotation])
		if err != nil {
			klog.Errorf("get subnet %s failed, %+v", pod.Annotations[util.LogicalSwitchAnnotation], err)
			continue
		}

		nsGWType := subnet.Spec.GatewayType
		nsGWNat := subnet.Spec.NatOutgoing
		if nsGWNat &&
			subnet.Spec.Vpc == util.DefaultVpc &&
			nsGWType == kubeovnv1.GWDistributedType &&
			pod.Spec.NodeName == hostname {
			if len(pod.Status.PodIPs) == 2 && protocol == kubeovnv1.ProtocolIPv6 {
				localPodIPs = append(localPodIPs, pod.Status.PodIPs[1].IP)
			} else if util.CheckProtocol(pod.Status.PodIP) == protocol {
				localPodIPs = append(localPodIPs, pod.Status.PodIP)
			}
		}
	}

	klog.V(3).Infof("local pod ips %v", localPodIPs)
	return localPodIPs, nil
}

func (c *Controller) getSubnetsNeedNAT(protocol string) ([]string, error) {
	var subnetsNeedNat []string
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("list subnets failed, %v", err)
		return nil, err
	}

	for _, subnet := range subnets {
		if subnet.Spec.Vpc == util.DefaultVpc &&
			subnet.Spec.GatewayType == kubeovnv1.GWCentralizedType &&
			subnet.Status.ActivateGateway == c.config.NodeName &&
			(subnet.Spec.Protocol == kubeovnv1.ProtocolDual || subnet.Spec.Protocol == protocol) &&
			subnet.Spec.NatOutgoing {
			cidrBlock := getCidrByProtocol(subnet.Spec.CIDRBlock, protocol)
			subnetsNeedNat = append(subnetsNeedNat, cidrBlock)
		}
	}
	return subnetsNeedNat, nil
}

func (c *Controller) getSubnetsCIDR(protocol string) ([]string, error) {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Error("failed to list subnets")
		return nil, err
	}

	ret := make([]string, 0, len(subnets)+3)
	if c.config.NodeLocalDNSIP != "" && net.ParseIP(c.config.NodeLocalDNSIP) != nil && util.CheckProtocol(c.config.NodeLocalDNSIP) == protocol {
		ret = append(ret, c.config.NodeLocalDNSIP)
	}
	for _, sip := range strings.Split(c.config.ServiceClusterIPRange, ",") {
		if util.CheckProtocol(sip) == protocol {
			ret = append(ret, sip)
		}
	}
	for _, subnet := range subnets {
		if subnet.Spec.Vpc == util.DefaultVpc {
			cidrBlock := getCidrByProtocol(subnet.Spec.CIDRBlock, protocol)
			ret = append(ret, cidrBlock)
		}
	}
	return ret, nil
}

func (c *Controller) getOtherNodes(protocol string) ([]string, error) {
	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Error("failed to list nodes")
		return nil, err
	}
	ret := make([]string, 0, len(nodes)-1)
	for _, node := range nodes {
		if node.Name == c.config.NodeName {
			continue
		}
		for _, addr := range node.Status.Addresses {
			if addr.Type == v1.NodeInternalIP {
				if util.CheckProtocol(addr.Address) == protocol {
					ret = append(ret, addr.Address)
				}
			}
		}
	}
	return ret, nil
}

//Generally, the MTU of the interface is set to 1400. But in special cases, a special pod (docker indocker) will introduce the docker0 interface to the pod. The MTU of docker0 is 1500.
//The network application in pod will calculate the TCP MSS according to the MTU of docker0, and then initiate communication with others. After the other party sends a response, the kernel protocol stack of Linux host will send ICMP unreachable message to the other party, indicating that IP fragmentation is needed, which is not supported by the other party, resulting in communication failure.
func (c *Controller) appendMssRule() {
	if c.config.Iface != "" && c.config.MSS > 0 {
		rule := fmt.Sprintf("-p tcp --tcp-flags SYN,RST SYN -o %s -j TCPMSS --set-mss %d", c.config.Iface, c.config.MSS)
		MssMangleRule := util.IPTableRule{
			Table: "mangle",
			Chain: "POSTROUTING",
			Rule:  strings.Split(rule, " "),
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
	exists, err := c.iptable[protocol].Exists(MssMangleRule.Table, MssMangleRule.Chain, MssMangleRule.Rule...)
	if err != nil {
		klog.Errorf("check iptable rule %v failed, %+v", MssMangleRule.Rule, err)
		return
	}

	if !exists {
		klog.Infof("iptables rules %s not exist, append iptables rules", strings.Join(MssMangleRule.Rule, " "))
		if err := c.iptable[protocol].Append(MssMangleRule.Table, MssMangleRule.Chain, MssMangleRule.Rule...); err != nil {
			klog.Errorf("append iptable rule %v failed, %+v", MssMangleRule.Rule, err)
			return
		}
	}
}

func getCidrByProtocol(cidr, protocol string) string {
	var cidrStr string
	if util.CheckProtocol(cidr) == kubeovnv1.ProtocolDual {
		cidrBlocks := strings.Split(cidr, ",")
		if protocol == kubeovnv1.ProtocolIPv4 {
			cidrStr = cidrBlocks[0]
		} else if protocol == kubeovnv1.ProtocolIPv6 {
			cidrStr = cidrBlocks[1]
		}
	} else {
		cidrStr = cidr
	}
	return cidrStr
}
