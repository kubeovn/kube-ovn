package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/internal"
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
	return c.patchSubnetStatusWithContext(context.Background(), subnet, reason, errStr)
}

func (c *Controller) patchSubnetStatusWithContext(ctx context.Context, subnet *kubeovnv1.Subnet, reason, errStr string) error {
	if err := c.checkSubnetRuntimeFence(ctx); err != nil {
		return err
	}
	if errStr != "" {
		subnet.Status.SetError(reason, errStr)
		if reason == "ValidateLogicalSwitchFailed" {
			subnet.Status.NotValidated(reason, errStr)
		} else {
			subnet.Status.Validated(reason, "")
		}
		subnet.Status.NotReady(reason, errStr)
		c.recordResourceEvent(subnet, v1.EventTypeWarning, reason, errStr)
	} else {
		subnet.Status.Validated(reason, "")
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
	if _, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(ctx, subnet.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
		klog.Errorf("failed to patch status for subnet %s, %v", subnet.Name, err)
		return err
	}
	if errStr == "" {
		c.recordResourceEvent(subnet, v1.EventTypeNormal, reason, fmt.Sprintf("Subnet %s status updated successfully", subnet.Name))
	}
	return nil
}

// patchSubnetGatewayMAC persists the subnet's gateway mac to status on its own, independent
// of the wider patchSubnetStatus flow, since it must be visible to the validating webhook
// (which only reads the Subnet CR) as soon as prepareOvnSubnet records it in ipam.
func (c *Controller) patchSubnetGatewayMAC(subnetName, gatewayMAC string) error {
	return c.patchSubnetGatewayMACWithContext(context.Background(), subnetName, gatewayMAC)
}

func (c *Controller) patchSubnetGatewayMACWithContext(ctx context.Context, subnetName, gatewayMAC string) error {
	if err := c.checkSubnetRuntimeFence(ctx); err != nil {
		return err
	}
	patch, err := json.Marshal(map[string]any{"status": map[string]any{"gatewayMAC": gatewayMAC}})
	if err != nil {
		klog.Error(err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(ctx, subnetName, types.MergePatchType, patch, metav1.PatchOptions{}, "status"); err != nil {
		klog.Errorf("failed to patch gateway mac status for subnet %s, %v", subnetName, err)
		return err
	}
	return nil
}

// recordSubnetGatewayMAC records subnet's router port mac in ipam and persists it to
// subnet.Status.GatewayMAC. A failed status patch is returned as an error so the subnet
// retries, since the webhook relies on the persisted value, not ipam, to see it.
func (c *Controller) recordSubnetGatewayMAC(subnet *kubeovnv1.Subnet, routerPortName string) error {
	return c.recordSubnetGatewayMACWithContext(context.Background(), subnet, routerPortName)
}

func (c *Controller) recordSubnetGatewayMACWithContext(ctx context.Context, subnet *kubeovnv1.Subnet, routerPortName string) error {
	if err := c.checkSubnetRuntimeFence(ctx); err != nil {
		return err
	}
	lrp, err := c.OVNNbClient.GetLogicalRouterPort(routerPortName, true)
	if err != nil || lrp == nil || lrp.MAC == "" {
		klog.V(3).Infof("router port %s not found or has no MAC, skipping gateway MAC record", routerPortName)
		return nil
	}
	if err := c.ipam.RecordGatewayMAC(subnet.Name, lrp.MAC); err != nil {
		klog.Warningf("failed to record gateway MAC %s for subnet %s: %v", lrp.MAC, subnet.Name, err)
	}
	if subnet.Status.GatewayMAC == lrp.MAC {
		return nil
	}
	if err := c.patchSubnetGatewayMACWithContext(ctx, subnet.Name, lrp.MAC); err != nil {
		klog.Errorf("failed to patch gateway MAC %s for subnet %s: %v", lrp.MAC, subnet.Name, err)
		return err
	}
	subnet.Status.GatewayMAC = lrp.MAC
	return nil
}

func (c *Controller) handleUpdateSubnetStatus(key string) error {
	c.subnetKeyMutex.LockKey(key)
	defer func() { _ = c.subnetKeyMutex.UnlockKey(key) }()

	cachedSubnet, err := c.subnetsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return c.recordSubnetKeyError(key, "GetSubnetFailed", err)
	}
	subnet := cachedSubnet.DeepCopy()

	ippools, err := c.ippoolLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list ippool: %v", err)
		return c.recordResourceError(subnet, "ListIPPoolsFailed", err)
	}
	for _, p := range ippools {
		if p.Spec.Subnet == subnet.Name {
			c.updateIPPoolStatusQueue.Add(p.Name)
		}
	}

	if subnet.Spec.CIDRBlock != "" {
		if _, err = c.calcSubnetStatusIP(subnet); err != nil {
			klog.Error(err)
			return c.recordResourceError(subnet, "CalculateStatusFailed", err)
		}

		if err := c.checkSubnetUsingIPs(subnet); err != nil {
			klog.Errorf("inconsistency detected in status of subnet %s : %v", subnet.Name, err)
			return c.recordResourceError(subnet, "CheckStatusConsistencyFailed", err)
		}
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

func (c *Controller) calculateUsingIPs(subnet *kubeovnv1.Subnet, podUsedIPs []*kubeovnv1.IP, noGWExcludeIPs []string) (internal.BigInt, error) {
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

	vips, err := c.virtualIpsLister.List(labels.SelectorFromSet(labels.Set{
		util.SubnetNameLabel: subnet.Name,
	}))
	if err != nil {
		return internal.BigInt{}, err
	}
	usingIPNums += len(vips)

	eips, err := c.iptablesEipsLister.List(
		labels.SelectorFromSet(labels.Set{util.SubnetNameLabel: subnet.Name}),
	)
	if err != nil {
		return internal.BigInt{}, err
	}
	usingIPNums += len(eips)

	ovnEips, err := c.ovnEipsLister.List(labels.SelectorFromSet(labels.Set{
		util.SubnetNameLabel: subnet.Name,
	}))
	if err != nil {
		return internal.BigInt{}, err
	}
	usingIPNums += len(ovnEips)

	return internal.NewBigInt(int64(usingIPNums)), nil
}

func (c *Controller) projectSubnetStatusIP(subnet *kubeovnv1.Subnet) (*kubeovnv1.Subnet, error) {
	if err := util.CheckCidrs(subnet.Spec.CIDRBlock); err != nil {
		return nil, err
	}

	// Look up the subnet's IPs via the bySubnet indexer instead of scanning the
	// whole IP store with a label selector, keeping this hot path O(matched).
	ipObjs, err := c.ipIndexer.ByIndex(IndexIPBySubnet, subnet.Name)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	podUsedIPs := make([]*kubeovnv1.IP, 0, len(ipObjs))
	for _, obj := range ipObjs {
		if ip, ok := obj.(*kubeovnv1.IP); ok {
			podUsedIPs = append(podUsedIPs, ip)
		}
	}

	noGWExcludeIPs := filterNonGatewayExcludeIPs(subnet)
	usingIPs, err := c.calculateUsingIPs(subnet, podUsedIPs, noGWExcludeIPs)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	v4UsingIPStr, v6UsingIPStr, v4AvailableIPStr, v6AvailableIPStr := c.ipam.GetSubnetIPRangeString(subnet.Name, subnet.Spec.ExcludeIps)
	return ProjectSubnetStatusIP(subnet, SubnetStatusIPProjectionInput{
		UsingIPs:           usingIPs,
		V4UsingIPRange:     v4UsingIPStr,
		V6UsingIPRange:     v6UsingIPStr,
		V4AvailableIPRange: v4AvailableIPStr,
		V6AvailableIPRange: v6AvailableIPStr,
	})
}

func (c *Controller) calcSubnetStatusIP(subnet *kubeovnv1.Subnet) (*kubeovnv1.Subnet, error) {
	projected, err := c.projectSubnetStatusIP(subnet)
	if err != nil {
		return nil, err
	}
	v4availableIPs := projected.Status.V4AvailableIPs
	v6availableIPs := projected.Status.V6AvailableIPs
	v4UsingIPs := projected.Status.V4UsingIPs
	v6UsingIPs := projected.Status.V6UsingIPs
	v4UsingIPStr := projected.Status.V4UsingIPRange
	v6UsingIPStr := projected.Status.V6UsingIPRange
	v4AvailableIPStr := projected.Status.V4AvailableIPRange
	v6AvailableIPStr := projected.Status.V6AvailableIPRange

	if subnet.Status.V4AvailableIPs.Equal(v4availableIPs) &&
		subnet.Status.V6AvailableIPs.Equal(v6availableIPs) &&
		subnet.Status.V4UsingIPs.Equal(v4UsingIPs) &&
		subnet.Status.V6UsingIPs.Equal(v6UsingIPs) &&
		subnet.Status.V4UsingIPRange == v4UsingIPStr &&
		subnet.Status.V6UsingIPRange == v6UsingIPStr &&
		subnet.Status.V4AvailableIPRange == v4AvailableIPStr &&
		subnet.Status.V6AvailableIPRange == v6AvailableIPStr {
		return subnet, nil
	}

	// Use a targeted patch with only IP-related fields to avoid overwriting
	// non-IP status fields (e.g., U2OInterconnectionVPC) set by other handlers.
	ipStatusPatch := struct {
		Status struct {
			V4AvailableIPs     internal.BigInt `json:"v4availableIPs"`
			V4AvailableIPRange string          `json:"v4availableIPrange"`
			V4UsingIPs         internal.BigInt `json:"v4usingIPs"`
			V4UsingIPRange     string          `json:"v4usingIPrange"`
			V6AvailableIPs     internal.BigInt `json:"v6availableIPs"`
			V6AvailableIPRange string          `json:"v6availableIPrange"`
			V6UsingIPs         internal.BigInt `json:"v6usingIPs"`
			V6UsingIPRange     string          `json:"v6usingIPrange"`
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
	return c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(context.Background(), subnet.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
}

// CalculateSubnetStatusIP exposes the legacy read-only status calculator to
// the opt-in controller-runtime projection adapter. It performs no API write.
func (c *Controller) CalculateSubnetStatusIP(subnet *kubeovnv1.Subnet) (*kubeovnv1.Subnet, error) {
	return c.projectSubnetStatusIP(subnet)
}

func (c *Controller) checkSubnetUsingIPs(subnet *kubeovnv1.Subnet) error {
	if !subnet.Status.V4UsingIPs.EqualInt64(0) && subnet.Status.V4UsingIPRange == "" {
		err := fmt.Errorf("subnet %s has %s v4 ip in use, while the v4 using ip range is empty", subnet.Name, subnet.Status.V4UsingIPs)
		klog.Error(err)
		return err
	}
	if !subnet.Status.V6UsingIPs.EqualInt64(0) && subnet.Status.V6UsingIPRange == "" {
		err := fmt.Errorf("subnet %s has %s v6 ip in use, while the v6 using ip range is empty", subnet.Name, subnet.Status.V6UsingIPs)
		klog.Error(err)
		return err
	}
	return nil
}
