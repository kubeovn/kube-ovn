package daemon

import (
	"fmt"
	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/ovs"
	"github.com/alauda/kube-ovn/pkg/util"
	"github.com/projectcalico/felix/ipsets"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"
	"net"
	"os"
	"strings"
)

const (
	SubnetSet    = "subnets"
	SubnetNatSet = "subnets-nat"
	LocalPodSet  = "local-pod-ip-nat"
	IPSetPrefix  = "ovn"
)

var (
	v4Rules = []util.IPTableRule{
		// This rule makes sure we don't NAT traffic within overlay network
		{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(`-m set --match-set ovn40subnets src -m set --match-set ovn40subnets dst -j MASQUERADE`, " ")},
		// Prevent performing Masquerade on external traffic which arrives from a Node that owns the Pod/Subnet IP
		{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(`-m set ! --match-set ovn40subnets src -m set --match-set ovn40local-pod-ip-nat dst -j RETURN`, " ")},
		{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(`-m set ! --match-set ovn40subnets src -m set --match-set ovn40subnets-nat dst -j RETURN`, " ")},
		// NAT if pod/subnet to external address
		{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(`-m set --match-set ovn40local-pod-ip-nat src -m set ! --match-set ovn40subnets dst -j MASQUERADE`, " ")},
		{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(`-m set --match-set ovn40subnets-nat src -m set ! --match-set ovn40subnets dst -j MASQUERADE`, " ")},
		// Input Accept
		{Table: "filter", Chain: "FORWARD", Rule: strings.Split(`-m set --match-set ovn40subnets src -j ACCEPT`, " ")},
		{Table: "filter", Chain: "FORWARD", Rule: strings.Split(`-m set --match-set ovn40subnets dst -j ACCEPT`, " ")},
		// Forward Accept
		{Table: "filter", Chain: "INPUT", Rule: strings.Split(`-m set --match-set ovn40subnets src -j ACCEPT`, " ")},
		{Table: "filter", Chain: "INPUT", Rule: strings.Split(`-m set --match-set ovn40subnets dst -j ACCEPT`, " ")},
	}
	v6Rules = []util.IPTableRule{
		// This rule makes sure we don't NAT traffic within overlay network
		{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(`-m set --match-set ovn60subnets src -m set --match-set ovn60subnets dst -j MASQUERADE`, " ")},
		// Prevent performing Masquerade on external traffic which arrives from a Node that owns the Pod/Subnet IP
		{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(`-m set ! --match-set ovn40subnets src -m set --match-set ovn60local-pod-ip-nat dst -j RETURN`, " ")},
		{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(`-m set ! --match-set ovn40subnets src -m set --match-set ovn60subnets-nat dst -j RETURN`, " ")},
		// NAT if pod/subnet to external address
		{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(`-m set --match-set ovn60local-pod-ip-nat src -m set ! --match-set ovn60subnets dst -j MASQUERADE`, " ")},
		{Table: "nat", Chain: "POSTROUTING", Rule: strings.Split(`-m set --match-set ovn60subnets-nat src -m set ! --match-set ovn60subnets dst -j MASQUERADE`, " ")},
		// Input Accept
		{Table: "filter", Chain: "FORWARD", Rule: strings.Split(`-m set --match-set ovn60subnets src -j ACCEPT`, " ")},
		{Table: "filter", Chain: "FORWARD", Rule: strings.Split(`-m set --match-set ovn60subnets dst -j ACCEPT`, " ")},
		// Forward Accept
		{Table: "filter", Chain: "INPUT", Rule: strings.Split(`-m set --match-set ovn60subnets src -j ACCEPT`, " ")},
		{Table: "filter", Chain: "INPUT", Rule: strings.Split(`-m set --match-set ovn60subnets dst -j ACCEPT`, " ")},
	}
)

func (c *Controller) runGateway() {
	subnets, err := c.getSubnetsCIDR(c.protocol)
	if err != nil {
		klog.Errorf("get subnets failed, %+v", err)
		return
	}
	localPodIPs, err := c.getLocalPodIPsNeedNAT(c.protocol)
	if err != nil {
		klog.Errorf("get local pod ips failed, %+v", err)
		return
	}
	subnetsNeedNat, err := c.getSubnetsNeedNAT(c.protocol)
	if err != nil {
		klog.Errorf("get need nat subnets failed, %+v", err)
		return
	}
	c.ipset.AddOrReplaceIPSet(ipsets.IPSetMetadata{
		MaxSize: 1048576,
		SetID:   SubnetSet,
		Type:    ipsets.IPSetTypeHashNet,
	}, subnets)
	c.ipset.AddOrReplaceIPSet(ipsets.IPSetMetadata{
		MaxSize: 1048576,
		SetID:   LocalPodSet,
		Type:    ipsets.IPSetTypeHashIP,
	}, localPodIPs)
	c.ipset.AddOrReplaceIPSet(ipsets.IPSetMetadata{
		MaxSize: 1048576,
		SetID:   SubnetNatSet,
		Type:    ipsets.IPSetTypeHashNet,
	}, subnetsNeedNat)
	c.ipset.ApplyUpdates()

	var iptableRules []util.IPTableRule
	if c.protocol == kubeovnv1.ProtocolIPv4 {
		iptableRules = v4Rules
	} else {
		iptableRules = v6Rules
	}
	for _, iptRule := range iptableRules {
		exists, err := c.iptable.Exists(iptRule.Table, iptRule.Chain, iptRule.Rule...)
		if err != nil {
			klog.Errorf("check iptable rule exist failed, %+v", err)
			return
		}
		if !exists {
			klog.Info("iptables rules not exist, recreate iptables rules")
			if err := c.iptable.Insert(iptRule.Table, iptRule.Chain, 1, iptRule.Rule...); err != nil {
				klog.Errorf("insert iptable rule %v failed, %+v", iptRule.Rule, err)
				return
			}
		}
	}

	if err := c.setGatewayBandwidth(); err != nil {
		klog.Errorf("failed to set gw bandwidth, %v", err)
	}
	if err := c.setICGateway(); err != nil {
		klog.Errorf("failed to set ic gateway, %v", err)
	}

	c.appendMssRule()
}

func (c *Controller) setGatewayBandwidth() error {
	node, err := c.config.KubeClient.CoreV1().Nodes().Get(c.config.NodeName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get node, %v", err)
		return err
	}
	ingress, egress := node.Annotations[util.IngressRateAnnotation], node.Annotations[util.EgressRateAnnotation]
	ifaceId := fmt.Sprintf("node-%s", c.config.NodeName)
	return ovs.SetInterfaceBandwidth(ifaceId, egress, ingress)
}

func (c *Controller) setICGateway() error {
	node, err := c.config.KubeClient.CoreV1().Nodes().Get(c.config.NodeName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get node, %v", err)
		return err
	}
	enable := node.Labels[util.ICGatewayAnnotation]
	if enable == "true" {
		if _, err := ovs.Exec("set", "open_vswitch", ".", "external_ids:ovn-is-interconn=true"); err != nil {
			return fmt.Errorf("failed to enable ic gateway, %v", err)
		}
	} else {
		if _, err := ovs.Exec("set", "open_vswitch", ".", "external_ids:ovn-is-interconn=false"); err != nil {
			return fmt.Errorf("failed to disable ic gateway, %v", err)
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
			nsGWType == kubeovnv1.GWDistributedType &&
			pod.Spec.NodeName == hostname &&
			util.CheckProtocol(pod.Status.PodIP) == protocol {
			localPodIPs = append(localPodIPs, pod.Status.PodIP)
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
		if subnet.Spec.GatewayType == kubeovnv1.GWCentralizedType &&
			subnet.Status.ActivateGateway == c.config.NodeName &&
			subnet.Spec.Protocol == protocol &&
			subnet.Spec.NatOutgoing {
			subnetsNeedNat = append(subnetsNeedNat, subnet.Spec.CIDRBlock)
		}
	}
	return subnetsNeedNat, nil
}

func (c *Controller) getSubnetsCIDR(protocol string) ([]string, error) {
	var ret = []string{c.config.ServiceClusterIPRange}
	if c.config.NodeLocalDNSIP != "" && net.ParseIP(c.config.NodeLocalDNSIP) != nil {
		ret = append(ret, c.config.NodeLocalDNSIP)
	}
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Error("failed to list subnets")
		return nil, err
	}
	for _, subnet := range subnets {
		if subnet.Spec.Protocol == protocol {
			ret = append(ret, subnet.Spec.CIDRBlock)
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

		exists, err := c.iptable.Exists(MssMangleRule.Table, MssMangleRule.Chain, MssMangleRule.Rule...)
		if err != nil {
			klog.Errorf("check iptable rule %v failed, %+v", MssMangleRule.Rule, err)
			return
		}

		if !exists {
			klog.Info("iptables rules not exist, append iptables rules")
			if err := c.iptable.Append(MssMangleRule.Table, MssMangleRule.Chain, MssMangleRule.Rule...); err != nil {
				klog.Errorf("append iptable rule %v failed, %+v", MssMangleRule.Rule, err)
				return
			}
		}
	}
}
