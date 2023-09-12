package daemon

import (
	"fmt"
	"os/exec"
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
	c.appendMssRule()
}

func (c *Controller) setGatewayBandwidth() error {
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node, %v", err)
		return err
	}
	ingress, egress := node.Annotations[util.IngressRateAnnotation], node.Annotations[util.EgressRateAnnotation]
	ifaceID := fmt.Sprintf("node-%s", c.config.NodeName)
	if ingress == "" && egress == "" {
		if htbQos, _ := ovs.IsHtbQos(ifaceID); !htbQos {
			return nil
		}
	}
	return ovs.SetInterfaceBandwidth("", "", ifaceID, egress, ingress)
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

func (c *Controller) isSubnetNeedNat(subnet *kubeovnv1.Subnet, protocol string) bool {
	if subnet.DeletionTimestamp == nil &&
		subnet.Spec.NatOutgoing &&
		(subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway) &&
		subnet.Spec.Vpc == c.config.ClusterRouter &&
		subnet.Spec.CIDRBlock != "" &&
		(subnet.Spec.Protocol == kubeovnv1.ProtocolDual || subnet.Spec.Protocol == protocol) {
		return true
	}
	return false
}

func (c *Controller) getSubnetsNeedNAT(protocol string) ([]string, error) {
	var subnetsNeedNat []string
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("list subnets failed, %v", err)
		return nil, err
	}

	for _, subnet := range subnets {
		if c.isSubnetNeedNat(subnet, protocol) {
			cidrBlock := getCidrByProtocol(subnet.Spec.CIDRBlock, protocol)
			subnetsNeedNat = append(subnetsNeedNat, cidrBlock)
		}
	}
	return subnetsNeedNat, nil
}

func (c *Controller) getSubnetsNatOutGoingPolicy(protocol string) ([]*kubeovnv1.Subnet, error) {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("list subnets failed, %v", err)
		return nil, err
	}

	var subnetsWithNatPolicy []*kubeovnv1.Subnet
	for _, subnet := range subnets {
		if c.isSubnetNeedNat(subnet, protocol) && len(subnet.Status.NatOutgoingPolicyRules) != 0 {
			subnetsWithNatPolicy = append(subnetsWithNatPolicy, subnet)
		}
	}
	return subnetsWithNatPolicy, nil
}

func (c *Controller) getSubnetsDistributedGateway(protocol string) ([]string, error) {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
		return nil, err
	}

	var result []string
	for _, subnet := range subnets {
		if subnet.DeletionTimestamp == nil &&
			(subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway) &&
			subnet.Spec.Vpc == c.config.ClusterRouter &&
			subnet.Spec.CIDRBlock != "" &&
			subnet.Spec.GatewayType == kubeovnv1.GWDistributedType &&
			(subnet.Spec.Protocol == kubeovnv1.ProtocolDual || subnet.Spec.Protocol == protocol) {
			cidrBlock := getCidrByProtocol(subnet.Spec.CIDRBlock, protocol)
			result = append(result, cidrBlock)
		}
	}
	return result, nil
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

func (c *Controller) getDefaultVpcSubnetsCIDR(protocol string) ([]string, map[string]string, error) {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Error("failed to list subnets")
		return nil, nil, err
	}

	ret := make([]string, 0, len(subnets)+1)
	subnetMap := make(map[string]string, len(subnets)+1)

	for _, subnet := range subnets {
		if subnet.Spec.Vpc == c.config.ClusterRouter && (subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway) && subnet.Spec.CIDRBlock != "" {
			cidrBlock := getCidrByProtocol(subnet.Spec.CIDRBlock, protocol)
			ret = append(ret, cidrBlock)
			subnetMap[subnet.Name] = cidrBlock
		}
	}
	return ret, subnetMap, nil
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

func (c *Controller) getEgressNatIPByNode(nodeName string) (map[string]string, error) {
	subnetsNatIP := make(map[string]string)
	subnetList, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return subnetsNatIP, err
	}

	for _, subnet := range subnetList {
		if !subnet.Spec.NatOutgoing ||
			(subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) ||
			subnet.Spec.GatewayType != kubeovnv1.GWCentralizedType ||
			!util.GatewayContains(subnet.Spec.GatewayNode, nodeName) ||
			subnet.Spec.Vpc != c.config.ClusterRouter {
			continue
		}

		// only check format like 'kube-ovn-worker:172.18.0.2, kube-ovn-control-plane:172.18.0.3'
		for _, cidr := range strings.Split(subnet.Spec.CIDRBlock, ",") {
			for _, gw := range strings.Split(subnet.Spec.GatewayNode, ",") {
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
	return subnetsNatIP, nil
}

func (c *Controller) getTProxyConditionPod(needSort bool) ([]*v1.Pod, error) {
	var filteredPods []*v1.Pod
	pods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("list pods failed, %v", err)
		return nil, err
	}

	for _, pod := range pods {
		if pod.Spec.NodeName != c.config.NodeName {
			continue
		}

		subnetName, ok := pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, util.OvnProvider)]
		if !ok {
			continue
		}

		subnet, err := c.subnetsLister.Get(subnetName)
		if err != nil {
			err = fmt.Errorf("failed to get subnet '%s', err: %v", subnetName, err)
			return nil, err
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
