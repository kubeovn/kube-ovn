package daemon

import (
	"context"
	"fmt"
	"net"
	"slices"
	"sort"
	"strings"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) runGateway() {
	if err := c.gatewayBackendManager.Reconcile(context.Background()); err != nil {
		klog.Errorf("协调网关 netfilter 后端失败: %v", err)
	}
	if err := c.setPolicyRouting(); err != nil {
		klog.Errorf("failed to set gw policy routing")
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

// getPodPrimaryNetworkProvider returns the kube-ovn network provider whose allocated
// IP addresses cover all the pod's primary network IPs, i.e. the network kubelet
// probes go through. It returns false if the pod's primary network is not managed
// by kube-ovn, e.g. when kube-ovn works as a secondary CNI.
func getPodPrimaryNetworkProvider(pod *v1.Pod) (string, bool) {
	podIPs := util.PodIPs(*pod)
	if len(podIPs) == 0 {
		return "", false
	}

	// check the default provider first so that the common case is deterministic
	if providerCoversIPs(pod, util.OvnProvider, podIPs) {
		return util.OvnProvider, true
	}

	suffix := fmt.Sprintf(util.IPAddressAnnotationTemplate, "")
	providers := make([]string, 0, 1)
	for key := range pod.Annotations {
		if provider, ok := strings.CutSuffix(key, suffix); ok && provider != "" && provider != util.OvnProvider {
			providers = append(providers, provider)
		}
	}
	slices.Sort(providers)
	for _, provider := range providers {
		if providerCoversIPs(pod, provider, podIPs) {
			return provider, true
		}
	}
	return "", false
}

// providerCoversIPs returns true if every IP in podIPs is allocated by the given
// provider according to the pod's ip_address annotation.
func providerCoversIPs(pod *v1.Pod, provider string, podIPs []string) bool {
	annotatedIPs := strings.Split(pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)], ",")
	allocated := make([]net.IP, 0, len(annotatedIPs))
	for _, ip := range annotatedIPs {
		if parsed := net.ParseIP(strings.TrimSpace(ip)); parsed != nil {
			allocated = append(allocated, parsed)
		}
	}
	if len(allocated) == 0 {
		return false
	}

	for _, podIP := range podIPs {
		parsed := net.ParseIP(podIP)
		if parsed == nil || !slices.ContainsFunc(allocated, parsed.Equal) {
			return false
		}
	}
	return true
}

func (c *Controller) getTProxyConditionPod(pods []*v1.Pod, needSort bool) ([]*v1.Pod, error) {
	var filteredPods []*v1.Pod
	for _, pod := range pods {
		provider, ok := getPodPrimaryNetworkProvider(pod)
		if !ok {
			// The pod's primary network is not managed by kube-ovn, e.g. kube-ovn works
			// as a secondary CNI. Kubelet probes go through the primary CNI in that case,
			// so tproxy must not intercept them.
			continue
		}

		subnetName, ok := pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider)]
		if !ok {
			continue
		}

		subnet, err := c.subnetsLister.Get(subnetName)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				klog.Warningf("subnet %q of pod %s/%s not found, skip", subnetName, pod.Namespace, pod.Name)
				continue
			}
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
