package daemon

import (
	"fmt"
	"net"
	"os"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) runGateway() {
	if err := c.setIPSet(); err != nil {
		klog.Errorf("failed to set gw ipsets")
	}
	if err := c.setPolicyRouting(); err != nil {
		klog.Errorf("failed to set gw policy routing")
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

func (c *Controller) getLocalPodIPsNeedNAT(protocol string) ([]string, error) {
	var localPodIPs []string
	nodeName := os.Getenv(util.HostnameEnv)
	allPods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("list pods failed, %+v", err)
		return nil, err
	}
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
			klog.Errorf("get subnet %s failed, %+v", pod.Annotations[util.LogicalSwitchAnnotation], err)
			continue
		}

		if !subnet.Spec.NatOutgoing ||
			subnet.Spec.Vpc != util.DefaultVpc ||
			subnet.Spec.GatewayType != kubeovnv1.GWDistributedType {
			continue
		}
		if subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway {
			continue
		}

		if len(pod.Status.PodIPs) != 0 {
			if len(pod.Status.PodIPs) == 2 && protocol == kubeovnv1.ProtocolIPv6 {
				localPodIPs = append(localPodIPs, pod.Status.PodIPs[1].IP)
			} else if util.CheckProtocol(pod.Status.PodIP) == protocol {
				localPodIPs = append(localPodIPs, pod.Status.PodIP)
			}
		} else {
			ipv4, ipv6 := util.SplitStringIP(pod.Annotations[util.IpAddressAnnotation])
			if ipv4 != "" && protocol == kubeovnv1.ProtocolIPv4 {
				localPodIPs = append(localPodIPs, ipv4)
			}
			if ipv6 != "" && protocol == kubeovnv1.ProtocolIPv6 {
				localPodIPs = append(localPodIPs, ipv6)
			}
		}
		attachIps, err := c.getAttachmentLocalPodIPsNeedNAT(pod, nodeName, protocol)
		if len(attachIps) != 0 && err == nil {
			localPodIPs = append(localPodIPs, attachIps...)
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
		if subnet.DeletionTimestamp == nil &&
			subnet.Spec.NatOutgoing &&
			(subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway) &&
			subnet.Spec.GatewayType == kubeovnv1.GWCentralizedType &&
			util.GatewayContains(subnet.Spec.GatewayNode, c.config.NodeName) &&
			subnet.Spec.Vpc == util.DefaultVpc &&
			(subnet.Spec.Protocol == kubeovnv1.ProtocolDual || subnet.Spec.Protocol == protocol) {
			cidrBlock := getCidrByProtocol(subnet.Spec.CIDRBlock, protocol)
			subnetsNeedNat = append(subnetsNeedNat, cidrBlock)
		}
	}
	return subnetsNeedNat, nil
}

func (c *Controller) getServicesCIDR(protocol string) []string {
	ret := make([]string, 0)
	for _, cidr := range strings.Split(c.config.ServiceClusterIPRange, ",") {
		if util.CheckProtocol(cidr) == protocol {
			ret = append(ret, cidr)
		}
	}
	return ret
}

func (c *Controller) getDefaultVpcSubnetsCIDR(protocol string) ([]string, error) {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Error("failed to list subnets")
		return nil, err
	}

	ret := make([]string, 0, len(subnets)+1)
	if c.config.NodeLocalDnsIP != "" && net.ParseIP(c.config.NodeLocalDnsIP) != nil && util.CheckProtocol(c.config.NodeLocalDnsIP) == protocol {
		ret = append(ret, c.config.NodeLocalDnsIP)
	}
	for _, subnet := range subnets {
		if subnet.Spec.Vpc == util.DefaultVpc && (subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway) {
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

func (c *Controller) getEgressNatIpByNode(nodeName string) (map[string]string, error) {
	var subnetsNatIp = make(map[string]string)
	subnetList, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return subnetsNatIp, err
	}

	for _, subnet := range subnetList {
		if !subnet.Spec.NatOutgoing ||
			(subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) ||
			subnet.Spec.GatewayType != kubeovnv1.GWCentralizedType ||
			!util.GatewayContains(subnet.Spec.GatewayNode, nodeName) ||
			subnet.Spec.Vpc != util.DefaultVpc {
			continue
		}

		// only check format like 'kube-ovn-worker:172.18.0.2, kube-ovn-control-plane:172.18.0.3'
		for _, cidr := range strings.Split(subnet.Spec.CIDRBlock, ",") {
			for _, gw := range strings.Split(subnet.Spec.GatewayNode, ",") {
				if strings.Contains(gw, ":") && util.GatewayContains(gw, nodeName) && util.CheckProtocol(cidr) == util.CheckProtocol(strings.Split(gw, ":")[1]) {
					subnetsNatIp[cidr] = strings.TrimSpace(strings.Split(gw, ":")[1])
					break
				}
			}
		}
	}
	return subnetsNatIp, nil
}

func (c *Controller) getAttachmentLocalPodIPsNeedNAT(pod *v1.Pod, nodeName, protocol string) ([]string, error) {
	var attachPodIPs []string

	attachNets, err := util.ParsePodNetworkAnnotation(pod.Annotations[util.AttachmentNetworkAnnotation], pod.Namespace)
	if err != nil {
		klog.Errorf("failed to parse attach net for pod '%s', %v", pod.Name, err)
		return attachPodIPs, err
	}
	for _, multiNet := range attachNets {
		provider := fmt.Sprintf("%s.%s.ovn", multiNet.Name, multiNet.Namespace)
		if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, provider)] == "true" {
			subnet, err := c.subnetsLister.Get(pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider)])
			if err != nil {
				klog.Errorf("get subnet %s failed, %+v", pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider)], err)
				continue
			}

			if subnet.Spec.NatOutgoing &&
				subnet.Spec.Vpc == util.DefaultVpc &&
				subnet.Spec.GatewayType == kubeovnv1.GWDistributedType &&
				pod.Spec.NodeName == nodeName {
				ipv4, ipv6 := util.SplitStringIP(pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, provider)])
				if ipv4 != "" && protocol == kubeovnv1.ProtocolIPv4 {
					attachPodIPs = append(attachPodIPs, ipv4)
				}
				if ipv6 != "" && protocol == kubeovnv1.ProtocolIPv6 {
					attachPodIPs = append(attachPodIPs, ipv6)
				}
			}
		}
	}
	return attachPodIPs, nil
}
