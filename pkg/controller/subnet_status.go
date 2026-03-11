package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) updateNatOutgoingPolicyRulesStatus(subnet *kubeovnv1.Subnet) error {
	if subnet.Spec.NatOutgoing {
		subnet.Status.NatOutgoingPolicyRules = make([]kubeovnv1.NatOutgoingPolicyRuleStatus, len(subnet.Spec.NatOutgoingPolicyRules))
		for index, rule := range subnet.Spec.NatOutgoingPolicyRules {
			jsonRule, err := json.Marshal(rule)
			if err != nil {
				klog.Error(err)
				return err
			}
			priority := strconv.Itoa(index)
			var retBytes []byte
			retBytes = append(retBytes, []byte(subnet.Name)...)
			retBytes = append(retBytes, []byte(priority)...)
			retBytes = append(retBytes, jsonRule...)
			result := util.Sha256Hash(retBytes)

			subnet.Status.NatOutgoingPolicyRules[index].RuleID = result[:util.NatPolicyRuleIDLength]
			subnet.Status.NatOutgoingPolicyRules[index].Match = rule.Match
			subnet.Status.NatOutgoingPolicyRules[index].Action = rule.Action
		}
	} else {
		subnet.Status.NatOutgoingPolicyRules = []kubeovnv1.NatOutgoingPolicyRuleStatus{}
	}

	return nil
}

func (c *Controller) patchSubnetStatus(subnet *kubeovnv1.Subnet, reason, errStr string) error {
	if errStr != "" {
		subnet.Status.SetError(reason, errStr)
		if reason == "ValidateLogicalSwitchFailed" {
			subnet.Status.NotValidated(reason, errStr)
		} else {
			subnet.Status.Validated(reason, "")
		}
		subnet.Status.NotReady(reason, errStr)
		c.recorder.Eventf(subnet, v1.EventTypeWarning, reason, errStr)
	} else {
		subnet.Status.Validated(reason, "")
		c.recorder.Eventf(subnet, v1.EventTypeNormal, reason, errStr)
		if reason == "SetPrivateLogicalSwitchSuccess" ||
			reason == "ResetLogicalSwitchAclSuccess" ||
			reason == "ReconcileCentralizedGatewaySuccess" ||
			reason == "SetNonOvnSubnetSuccess" {
			subnet.Status.Ready(reason, "")
		}
	}

	bytes, err := subnet.Status.Bytes()
	if err != nil {
		klog.Error(err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(context.Background(), subnet.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
		klog.Errorf("failed to patch status for subnet %s, %v", subnet.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleUpdateSubnetStatus(key string) error {
	c.subnetKeyMutex.LockKey(key)
	defer func() { _ = c.subnetKeyMutex.UnlockKey(key) }()

	cachedSubnet, err := c.subnetsLister.Get(key)
	subnet := cachedSubnet.DeepCopy()
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	ippools, err := c.ippoolLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list ippool: %v", err)
		return err
	}
	for _, p := range ippools {
		if p.Spec.Subnet == subnet.Name {
			c.updateIPPoolStatusQueue.Add(p.Name)
		}
	}

	if _, err = c.calcSubnetStatusIP(subnet); err != nil {
		klog.Error(err)
		return err
	}

	if err := c.checkSubnetUsingIPs(subnet); err != nil {
		klog.Errorf("inconsistency detected in status of subnet %s : %v", subnet.Name, err)
		return err
	}
	return nil
}

func filterNonGatewayExcludeIPs(subnet *kubeovnv1.Subnet) []string {
	noGWExcludeIPs := []string{}
	v4gw, v6gw := util.SplitStringIP(subnet.Spec.Gateway)
	for _, excludeIP := range subnet.Spec.ExcludeIps {
		if v4gw == excludeIP || v6gw == excludeIP {
			continue
		}
		noGWExcludeIPs = append(noGWExcludeIPs, excludeIP)
	}
	return noGWExcludeIPs
}

func (c *Controller) calculateUsingIPs(subnet *kubeovnv1.Subnet, podUsedIPs []*kubeovnv1.IP, noGWExcludeIPs []string) (float64, error) {
	usingIPNums := len(podUsedIPs)

	if len(noGWExcludeIPs) > 0 {
		for _, podUsedIP := range podUsedIPs {
			for _, excludeIP := range noGWExcludeIPs {
				if util.ContainsIPs(excludeIP, podUsedIP.Spec.V4IPAddress) || util.ContainsIPs(excludeIP, podUsedIP.Spec.V6IPAddress) {
					usingIPNums--
					break
				}
			}
		}
	}

	usingIPs := float64(usingIPNums)

	vips, err := c.virtualIpsLister.List(labels.SelectorFromSet(labels.Set{
		util.SubnetNameLabel: subnet.Name,
		util.IPReservedLabel: "",
	}))
	if err != nil {
		return 0, err
	}
	usingIPs += float64(len(vips))

	eips, err := c.iptablesEipsLister.List(
		labels.SelectorFromSet(labels.Set{util.SubnetNameLabel: subnet.Name}))
	if err != nil {
		return 0, err
	}
	usingIPs += float64(len(eips))

	ovnEips, err := c.ovnEipsLister.List(labels.SelectorFromSet(labels.Set{
		util.SubnetNameLabel: subnet.Name,
	}))
	if err != nil {
		return 0, err
	}
	usingIPs += float64(len(ovnEips))

	return usingIPs, nil
}

func (c *Controller) calcSubnetStatusIP(subnet *kubeovnv1.Subnet) (*kubeovnv1.Subnet, error) {
	if err := util.CheckCidrs(subnet.Spec.CIDRBlock); err != nil {
		return nil, err
	}

	podUsedIPs, err := c.ipsLister.List(labels.SelectorFromSet(labels.Set{subnet.Name: ""}))
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	noGWExcludeIPs := filterNonGatewayExcludeIPs(subnet)
	usingIPs, err := c.calculateUsingIPs(subnet, podUsedIPs, noGWExcludeIPs)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	var v4availableIPs, v6availableIPs float64
	v4UsingIPStr, v6UsingIPStr, v4AvailableIPStr, v6AvailableIPStr := c.ipam.GetSubnetIPRangeString(subnet.Name, subnet.Spec.ExcludeIps)

	switch subnet.Spec.Protocol {
	case kubeovnv1.ProtocolDual:
		v4ExcludeIPs, v6ExcludeIPs := util.SplitIpsByProtocol(subnet.Spec.ExcludeIps)
		cidrBlocks := strings.Split(subnet.Spec.CIDRBlock, ",")
		v4toSubIPs := util.ExpandExcludeIPs(v4ExcludeIPs, cidrBlocks[0])
		v6toSubIPs := util.ExpandExcludeIPs(v6ExcludeIPs, cidrBlocks[1])
		_, v4CIDR, _ := net.ParseCIDR(cidrBlocks[0])
		_, v6CIDR, _ := net.ParseCIDR(cidrBlocks[1])
		v4availableIPs = util.AddressCount(v4CIDR) - util.CountIPNums(v4toSubIPs) - usingIPs
		v6availableIPs = util.AddressCount(v6CIDR) - util.CountIPNums(v6toSubIPs) - usingIPs
	case kubeovnv1.ProtocolIPv4:
		_, cidr, _ := net.ParseCIDR(subnet.Spec.CIDRBlock)
		toSubIPs := util.ExpandExcludeIPs(subnet.Spec.ExcludeIps, subnet.Spec.CIDRBlock)
		v4availableIPs = util.AddressCount(cidr) - util.CountIPNums(toSubIPs) - usingIPs
	case kubeovnv1.ProtocolIPv6:
		_, cidr, _ := net.ParseCIDR(subnet.Spec.CIDRBlock)
		toSubIPs := util.ExpandExcludeIPs(subnet.Spec.ExcludeIps, subnet.Spec.CIDRBlock)
		v6availableIPs = util.AddressCount(cidr) - util.CountIPNums(toSubIPs) - usingIPs
	}

	v4availableIPs = max(v4availableIPs, 0)
	v6availableIPs = max(v6availableIPs, 0)

	v4UsingIPs, v6UsingIPs := usingIPs, usingIPs
	switch subnet.Spec.Protocol {
	case kubeovnv1.ProtocolIPv4:
		v6UsingIPs = 0
	case kubeovnv1.ProtocolIPv6:
		v4UsingIPs = 0
	}

	if subnet.Status.V4AvailableIPs == v4availableIPs &&
		subnet.Status.V6AvailableIPs == v6availableIPs &&
		subnet.Status.V4UsingIPs == v4UsingIPs &&
		subnet.Status.V6UsingIPs == v6UsingIPs &&
		subnet.Status.V4UsingIPRange == v4UsingIPStr &&
		subnet.Status.V6UsingIPRange == v6UsingIPStr &&
		subnet.Status.V4AvailableIPRange == v4AvailableIPStr &&
		subnet.Status.V6AvailableIPRange == v6AvailableIPStr {
		return subnet, nil
	}

	subnet.Status.V4AvailableIPs = v4availableIPs
	subnet.Status.V6AvailableIPs = v6availableIPs
	subnet.Status.V4UsingIPs = v4UsingIPs
	subnet.Status.V6UsingIPs = v6UsingIPs
	subnet.Status.V4UsingIPRange = v4UsingIPStr
	subnet.Status.V6UsingIPRange = v6UsingIPStr
	subnet.Status.V4AvailableIPRange = v4AvailableIPStr
	subnet.Status.V6AvailableIPRange = v6AvailableIPStr

	// Use a targeted patch with only IP-related fields to avoid overwriting
	// non-IP status fields (e.g., U2OInterconnectionVPC) set by other handlers.
	ipStatusPatch := struct {
		Status struct {
			V4AvailableIPs     float64 `json:"v4availableIPs"`
			V4AvailableIPRange string  `json:"v4availableIPrange"`
			V4UsingIPs         float64 `json:"v4usingIPs"`
			V4UsingIPRange     string  `json:"v4usingIPrange"`
			V6AvailableIPs     float64 `json:"v6availableIPs"`
			V6AvailableIPRange string  `json:"v6availableIPrange"`
			V6UsingIPs         float64 `json:"v6usingIPs"`
			V6UsingIPRange     string  `json:"v6usingIPrange"`
		} `json:"status"`
	}{}
	ipStatusPatch.Status.V4AvailableIPs = v4availableIPs
	ipStatusPatch.Status.V4AvailableIPRange = v4AvailableIPStr
	ipStatusPatch.Status.V4UsingIPs = v4UsingIPs
	ipStatusPatch.Status.V4UsingIPRange = v4UsingIPStr
	ipStatusPatch.Status.V6AvailableIPs = v6availableIPs
	ipStatusPatch.Status.V6AvailableIPRange = v6AvailableIPStr
	ipStatusPatch.Status.V6UsingIPs = v6UsingIPs
	ipStatusPatch.Status.V6UsingIPRange = v6UsingIPStr
	bytes, err := json.Marshal(ipStatusPatch)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	newSubnet, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(context.Background(), subnet.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
	return newSubnet, err
}

func (c *Controller) checkSubnetUsingIPs(subnet *kubeovnv1.Subnet) error {
	if subnet.Status.V4UsingIPs != 0 && subnet.Status.V4UsingIPRange == "" {
		err := fmt.Errorf("subnet %s has %.0f v4 ip in use, while the v4 using ip range is empty", subnet.Name, subnet.Status.V4UsingIPs)
		klog.Error(err)
		return err
	}
	if subnet.Status.V6UsingIPs != 0 && subnet.Status.V6UsingIPRange == "" {
		err := fmt.Errorf("subnet %s has %.0f v6 ip in use, while the v6 using ip range is empty", subnet.Name, subnet.Status.V6UsingIPs)
		klog.Error(err)
		return err
	}
	return nil
}
