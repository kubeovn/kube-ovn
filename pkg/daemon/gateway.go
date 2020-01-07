package daemon

import (
	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/util"
	"github.com/projectcalico/felix/ipsets"
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
	podNatV4Rule = util.IPTableRule{
		Table: "nat",
		Chain: "POSTROUTING",
		Rule:  strings.Split("-m set --match-set ovn40local-pod-ip-nat src -m set ! --match-set ovn40subnets dst -j MASQUERADE", " "),
	}
	subnetNatV4Rule = util.IPTableRule{
		Table: "nat",
		Chain: "POSTROUTING",
		Rule:  strings.Split("-m set --match-set ovn40subnets-nat src -m set ! --match-set ovn40subnets dst -j MASQUERADE", " "),
	}
	podNatV6Rule = util.IPTableRule{
		Table: "nat",
		Chain: "POSTROUTING",
		Rule:  strings.Split("-m set --match-set ovn60local-pod-ip-nat src -m set ! --match-set ovn60subnets dst -j MASQUERADE", " "),
	}
	subnetNatV6Rule = util.IPTableRule{
		Table: "nat",
		Chain: "POSTROUTING",
		Rule:  strings.Split("-m set --match-set ovn60subnets-nat src -m set ! --match-set ovn60subnets dst -j MASQUERADE", " "),
	}
	forwardAcceptRule1 = util.IPTableRule{
		Table: "filter",
		Chain: "FORWARD",
		Rule:  strings.Split("-i ovn0 -j ACCEPT", " "),
	}
	forwardAcceptRule2 = util.IPTableRule{
		Table: "filter",
		Chain: "FORWARD",
		Rule:  strings.Split(`-o ovn0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT`, " "),
	}
)

func (c *Controller) runGateway() {
	klog.Info("reconcile gateway")
	protocol := c.protocol
	subnets, err := c.getSubnetsCIDR(protocol)
	if err != nil {
		klog.Errorf("get subnets failed, %+v", err)
		return
	}
	localPodIPs, err := c.getLocalPodIPsNeedNAT(protocol)
	if err != nil {
		klog.Errorf("get local pod ips failed, %+v", err)
		return
	}
	subnetsNeedNat, err := c.getSubnetsNeedNAT(protocol)
	if err != nil {
		klog.Errorf("get need nat subnets failed, %+v", err)
		return
	}
	if protocol == kubeovnv1.ProtocolIPv4 {
		c.ipSetsV4Mgr.AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   SubnetSet,
			Type:    ipsets.IPSetTypeHashNet,
		}, subnets)
		c.ipSetsV4Mgr.AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   LocalPodSet,
			Type:    ipsets.IPSetTypeHashIP,
		}, localPodIPs)
		c.ipSetsV4Mgr.AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   SubnetNatSet,
			Type:    ipsets.IPSetTypeHashNet,
		}, subnetsNeedNat)
		c.ipSetsV4Mgr.ApplyUpdates()
		for _, iptRule := range []util.IPTableRule{forwardAcceptRule1, forwardAcceptRule2, podNatV4Rule, subnetNatV4Rule} {
			exists, err := c.iptablesV4Mgr.Exists(iptRule.Table, iptRule.Chain, iptRule.Rule...)
			if err != nil {
				klog.Errorf("check iptable rule exist failed, %+v", err)
			}
			if !exists {
				klog.Info("iptables rules not exist, recreate iptables rules")
				err := c.iptablesV4Mgr.Insert(iptRule.Table, iptRule.Chain, 1, iptRule.Rule...)
				if err != nil {
					klog.Errorf("insert iptable rule exist failed, %+v", err)
				}
			}
		}
	} else {
		c.ipSetsV6Mgr.AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   SubnetSet,
			Type:    ipsets.IPSetTypeHashNet,
		}, subnets)
		c.ipSetsV6Mgr.AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   LocalPodSet,
			Type:    ipsets.IPSetTypeHashIP,
		}, localPodIPs)
		c.ipSetsV6Mgr.AddOrReplaceIPSet(ipsets.IPSetMetadata{
			MaxSize: 1048576,
			SetID:   SubnetNatSet,
			Type:    ipsets.IPSetTypeHashNet,
		}, subnetsNeedNat)
		c.ipSetsV6Mgr.ApplyUpdates()
		for _, iptRule := range []util.IPTableRule{forwardAcceptRule1, forwardAcceptRule2, podNatV6Rule, subnetNatV6Rule} {
			exists, err := c.iptablesV6Mgr.Exists(iptRule.Table, iptRule.Chain, iptRule.Rule...)
			if err != nil {
				klog.Errorf("check iptable rule exist failed, %+v", err)
			}
			if !exists {
				klog.Info("iptables rules not exist, recreate iptables rules")
				err := c.iptablesV6Mgr.Insert(iptRule.Table, iptRule.Chain, 1, iptRule.Rule...)
				if err != nil {
					klog.Errorf("insert iptable rule exist failed, %+v", err)
				}
			}
		}
	}
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
		if pod.Spec.HostNetwork == true || pod.Status.PodIP == "" {
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
