package daemon

import (
	"fmt"
	"sort"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
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
	c.gcIPSet()
}

func (c *Controller) setGatewayBandwidth() error {
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node, %v", err)
		return err
	}
	ingress, egress := node.Annotations[util.IngressRateAnnotation], node.Annotations[util.EgressRateAnnotation]
	ingressBurst, egressBurst := node.Annotations[util.IngressBurstAnnotation], node.Annotations[util.EgressBurstAnnotation]
	ifaceID := util.NodeLspName(c.config.NodeName)
	if ingress == "" && egress == "" {
		if htbQos, _ := ovs.IsHtbQos(ifaceID); !htbQos {
			return nil
		}
	}
	return ovs.SetInterfaceBandwidth("", "", ifaceID, egress, ingress, egressBurst, ingressBurst)
}

func (c *Controller) setICGateway() error {
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node, %v", err)
		return err
	}
	enable := node.Labels[util.ICGatewayLabel]
	if enable == "true" {
		icEnabled, err := ovs.Exec(ovs.IfExists, "get", "open", ".", "external_ids:ovn-is-interconn")
		if err != nil {
			return fmt.Errorf("failed to get if ic enabled, %w", err)
		}
		if strings.Trim(icEnabled, "\"") != "true" {
			if _, err := ovs.Exec("set", "open", ".", "external_ids:ovn-is-interconn=true"); err != nil {
				return fmt.Errorf("failed to enable ic gateway, %w", err)
			}
		}
	} else {
		if _, err := ovs.Exec("set", "open", ".", "external_ids:ovn-is-interconn=false"); err != nil {
			return fmt.Errorf("failed to disable ic gateway, %w", err)
		}
	}
	return nil
}

func (c *Controller) isSubnetNeedNat(subnet *kubeovnv1.Subnet, protocol string) bool {
	if subnet.DeletionTimestamp.IsZero() &&
		subnet.Spec.NatOutgoing &&
		(subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway) &&
		subnet.Spec.Vpc == c.config.ClusterRouter &&
		subnet.Spec.CIDRBlock != "" &&
		(subnet.Spec.Protocol == kubeovnv1.ProtocolDual || subnet.Spec.Protocol == protocol) {
		return true
	}
	return false
}

func (c *Controller) getSubnetsNeedNAT(subnets []*kubeovnv1.Subnet, protocol string) []string {
	var subnetsNeedNat []string
	for _, subnet := range subnets {
		if !c.isSubnetNeedNat(subnet, protocol) {
			continue
		}
		cidrBlock, err := getCidrByProtocol(subnet.Spec.CIDRBlock, protocol)
		if err != nil {
			klog.Errorf("failed to get subnet %s CIDR block by protocol: %v", subnet.Name, err)
			continue
		}
		if cidrBlock != "" {
			subnetsNeedNat = append(subnetsNeedNat, cidrBlock)
		}
	}
	return subnetsNeedNat
}

func (c *Controller) getSubnetsNatOutGoingPolicy(subnets []*kubeovnv1.Subnet, protocol string) []*kubeovnv1.Subnet {
	var subnetsWithNatPolicy []*kubeovnv1.Subnet
	for _, subnet := range subnets {
		if c.isSubnetNeedNat(subnet, protocol) && len(subnet.Status.NatOutgoingPolicyRules) != 0 {
			subnetsWithNatPolicy = append(subnetsWithNatPolicy, subnet)
		}
	}
	return subnetsWithNatPolicy
}

func (c *Controller) getSubnetsDistributedGateway(subnets []*kubeovnv1.Subnet, protocol string) []string {
	var result []string
	for _, subnet := range subnets {
		if subnet.DeletionTimestamp.IsZero() &&
			(subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway) &&
			subnet.Spec.Vpc == c.config.ClusterRouter &&
			subnet.Spec.CIDRBlock != "" &&
			subnet.Spec.GatewayType == kubeovnv1.GWDistributedType &&
			(subnet.Spec.Protocol == kubeovnv1.ProtocolDual || subnet.Spec.Protocol == protocol) {
			cidrBlock, err := getCidrByProtocol(subnet.Spec.CIDRBlock, protocol)
			if err != nil {
				klog.Errorf("failed to get subnet %s CIDR block by protocol: %v", subnet.Name, err)
				continue
			}
			if cidrBlock != "" {
				result = append(result, cidrBlock)
			}
		}
	}
	return result
}

func (c *Controller) getServicesCIDR(protocol string) []string {
	if protocol == kubeovnv1.ProtocolIPv6 {
		return c.serviceCIDRStore.V6CIDRs()
	}
	return c.serviceCIDRStore.V4CIDRs()
}

func (c *Controller) getDefaultVpcSubnetsCIDR(subnets []*kubeovnv1.Subnet, protocol string) ([]string, map[string]string) {
	ret := make([]string, 0, len(subnets)+1)
	subnetMap := make(map[string]string, len(subnets)+1)

	for _, subnet := range subnets {
		if subnet.Spec.Vpc == c.config.ClusterRouter && (subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway) && subnet.Spec.CIDRBlock != "" {
			cidrBlock, err := getCidrByProtocol(subnet.Spec.CIDRBlock, protocol)
			if err != nil {
				klog.Errorf("failed to get subnet %s CIDR block by protocol: %v", subnet.Name, err)
				continue
			}
			if cidrBlock != "" {
				ret = append(ret, cidrBlock)
				subnetMap[subnet.Name] = cidrBlock
			}
		}
	}
	return ret, subnetMap
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

func getCidrByProtocol(cidr, protocol string) (string, error) {
	if err := util.CheckCidrs(cidr); err != nil {
		return "", err
	}

	for cidr := range strings.SplitSeq(cidr, ",") {
		if util.CheckProtocol(cidr) == protocol {
			return cidr, nil
		}
	}

	return "", nil
}

func (c *Controller) getEgressNatIPByNode(subnets []*kubeovnv1.Subnet, nodeName string) map[string]string {
	subnetsNatIP := make(map[string]string)
	for _, subnet := range subnets {
		if !subnet.Spec.NatOutgoing ||
			(subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) ||
			subnet.Spec.GatewayType != kubeovnv1.GWCentralizedType ||
			!util.GatewayContains(subnet.Spec.GatewayNode, nodeName) ||
			subnet.Spec.Vpc != c.config.ClusterRouter {
			continue
		}

		for cidr := range strings.SplitSeq(subnet.Spec.CIDRBlock, ",") {
			// check format like 'kube-ovn-worker:172.18.0.2, kube-ovn-control-plane:172.18.0.3'
			for gw := range strings.SplitSeq(subnet.Spec.GatewayNode, ",") {
				if strings.Contains(gw, ":") && util.GatewayContains(gw, nodeName) && util.CheckProtocol(cidr) == util.CheckProtocol(strings.Split(gw, ":")[1]) {
					if subnet.Spec.EnableEcmp {
						subnetsNatIP[cidr] = strings.TrimSpace(strings.Split(gw, ":")[1])
					} else if subnet.Status.ActivateGateway == nodeName {
						subnetsNatIP[cidr] = strings.TrimSpace(strings.Split(gw, ":")[1])
					}
					break
				}
			}
		}
	}
	return subnetsNatIP
}

func (c *Controller) getTProxyConditionPod(pods []*v1.Pod, needSort bool) ([]*v1.Pod, error) {
	var filteredPods []*v1.Pod
	for _, pod := range pods {
		subnetName, ok := pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, util.OvnProvider)]
		if !ok {
			continue
		}

		subnet, err := c.subnetsLister.Get(subnetName)
		if err != nil {
			return nil, fmt.Errorf("failed to get subnet %q: %w", subnetName, err)
		}

		if subnet.Spec.Vpc == c.config.ClusterRouter {
			continue
		}

		filteredPods = append(filteredPods, pod)
	}

	if needSort {
		sort.Slice(filteredPods, func(i, j int) bool {
			return filteredPods[i].Namespace+"/"+filteredPods[i].Name < filteredPods[j].Namespace+"/"+filteredPods[j].Name
		})
	}

	return filteredPods, nil
}
