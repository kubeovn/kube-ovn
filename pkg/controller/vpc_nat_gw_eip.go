package controller

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddIptablesEip(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.IptablesEIP)).String()
	klog.Infof("enqueue add iptables eip %s", key)
	c.addIptablesEipQueue.Add(key)
}

func (c *Controller) enqueueUpdateIptablesEip(oldObj, newObj any) {
	oldEip := oldObj.(*kubeovnv1.IptablesEIP)
	newEip := newObj.(*kubeovnv1.IptablesEIP)
	if !newEip.DeletionTimestamp.IsZero() ||
		oldEip.Status.Redo != newEip.Status.Redo ||
		oldEip.Spec.QoSPolicy != newEip.Spec.QoSPolicy {
		key := cache.MetaObjectToName(newEip).String()
		klog.Infof("enqueue update iptables eip %s", key)
		c.updateIptablesEipQueue.Add(key)
	}
}

func (c *Controller) enqueueDelIptablesEip(obj any) {
	var eip *kubeovnv1.IptablesEIP
	switch t := obj.(type) {
	case *kubeovnv1.IptablesEIP:
		eip = t
	case cache.DeletedFinalStateUnknown:
		e, ok := t.Obj.(*kubeovnv1.IptablesEIP)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		eip = e
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	key := cache.MetaObjectToName(eip).String()
	klog.Infof("enqueue del iptables eip %s", key)
	c.delIptablesEipQueue.Add(eip)
}

func (c *Controller) handleAddIptablesEip(key string) error {
	cachedEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	if vpcNatEnabled != "true" {
		return errors.New("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle add iptables eip %s", key)

	if cachedEip.Status.Ready && cachedEip.Status.IP != "" {
		// already ok
		return nil
	}

	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
		return err
	}

	nadName := util.GetExternalNetwork(cachedEip.Spec.ExternalSubnet)
	subnet, err := c.findSubnetByNetworkAttachmentDefinition(c.config.PodNamespace, nadName, subnets)
	if err != nil {
		klog.Error(err)
		return err
	}

	// v6 ip address can not use upper case
	if util.ContainsUppercase(cachedEip.Spec.V6ip) {
		err := fmt.Errorf("eip %s v6 ip address %s can not contain upper case", cachedEip.Name, cachedEip.Spec.V6ip)
		klog.Error(err)
		return err
	}

	// make sure vpc nat gw pod is ready before eip allocation
	if _, err := c.getNatGwPod(cachedEip.Spec.NatGwDp); err != nil {
		klog.Error(err)
		return err
	}

	var v4ip, v6ip, mac string
	portName := ovs.PodNameToPortName(cachedEip.Name, cachedEip.Namespace, subnet.Spec.Provider)
	if cachedEip.Spec.V4ip != "" {
		if v4ip, v6ip, mac, err = c.acquireStaticEip(cachedEip.Name, cachedEip.Namespace, portName, cachedEip.Spec.V4ip, subnet.Name); err != nil {
			klog.Errorf("failed to acquire static eip, err: %v", err)
			return err
		}
	} else {
		// Random allocate
		if v4ip, v6ip, mac, err = c.acquireEip(cachedEip.Name, cachedEip.Namespace, portName, subnet.Name); err != nil {
			klog.Errorf("failed to allocate eip, err: %v", err)
			return err
		}
	}
	eipV4Cidr, _ := util.SplitStringIP(subnet.Spec.CIDRBlock)
	if v4ip == "" || eipV4Cidr == "" {
		err = fmt.Errorf("subnet %s does not support ipv4", subnet.Name)
		klog.Error(err)
		return err
	}
	addrV4, err := util.GetIPAddrWithMask(v4ip, eipV4Cidr)
	if err != nil {
		err = fmt.Errorf("failed to get eip %s with mask by cidr %s: %w", v4ip, eipV4Cidr, err)
		klog.Error(err)
		return err
	}

	if err = c.createEipInPod(cachedEip.Spec.NatGwDp, addrV4); err != nil {
		klog.Errorf("failed to create eip '%s' in pod, %v", key, err)
		return err
	}

	if cachedEip.Spec.QoSPolicy != "" {
		if err = c.addEipQoS(cachedEip, v4ip); err != nil {
			klog.Errorf("failed to add qos '%s' in pod, %v", key, err)
			return err
		}
	}
	if err = c.createOrUpdateEipCR(key, v4ip, v6ip, mac, cachedEip.Spec.NatGwDp, cachedEip.Spec.QoSPolicy, subnet.Name); err != nil {
		klog.Errorf("failed to update eip %s, %v", key, err)
		return err
	}

	// Trigger subnet status update after all operations complete
	// At this point: IPAM allocated, IptablesEIP CR created with labels+status+finalizer
	c.updateSubnetStatusQueue.Add(subnet.Name)
	return nil
}

func (c *Controller) handleResetIptablesEip(key string) error {
	if _, err := c.iptablesEipsLister.Get(key); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	klog.Infof("handle reset eip %s", key)
	if err := c.patchEipLabel(key); err != nil {
		klog.Errorf("failed to patch label for eip %s, %v", key, err)
		return err
	}
	if err := c.patchEipStatus(key, "", "", "", true); err != nil {
		klog.Errorf("failed to reset nat for eip %s, %v", key, err)
		return err
	}
	return nil
}

func (c *Controller) handleUpdateIptablesEip(key string) error {
	cachedEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle update iptables eip %s", key)

	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
		return err
	}

	nadName := util.GetExternalNetwork(cachedEip.Spec.ExternalSubnet)
	subnet, err := c.findSubnetByNetworkAttachmentDefinition(c.config.PodNamespace, nadName, subnets)
	if err != nil {
		klog.Error(err)
		return err
	}

	v4Cidr, _ := util.SplitStringIP(subnet.Spec.CIDRBlock)
	if v4Cidr == "" {
		err = fmt.Errorf("subnet %s does not support ipv4", subnet.Name)
		klog.Error(err)
		return err
	}

	if !cachedEip.DeletionTimestamp.IsZero() {
		klog.Infof("clean eip %q in pod", key)

		// Check if EIP is still being used by any NAT rules (FIP/DNAT/SNAT)
		// Only remove finalizer when no NAT rules are using it
		// Note: We query NAT rules directly instead of relying on cachedEip.Status.Nat
		// to avoid cache staleness issues
		nat, err := c.getIptablesEipNat(cachedEip.Spec.V4ip)
		if err != nil {
			klog.Errorf("failed to get eip %s nat rules, %v", key, err)
			return err
		}
		if nat != "" {
			klog.Infof("eip %s is still being used by NAT rules: %s, waiting for them to be deleted", key, nat)
			return nil
		}

		if vpcNatEnabled == "true" {
			v4ipCidr, err := util.GetIPAddrWithMask(cachedEip.Status.IP, v4Cidr)
			if err != nil {
				err = fmt.Errorf("failed to get eip %s with mask by cidr %s: %w", cachedEip.Status.IP, v4Cidr, err)
				klog.Error(err)
				return err
			}
			if err = c.deleteEipInPod(cachedEip.Spec.NatGwDp, v4ipCidr); err != nil {
				klog.Errorf("failed to clean eip '%s' in pod, %v", key, err)
				return err
			}
		}
		// Save qosPolicy before deleting, we need to trigger QoS Policy reconcile after EIP is deleted
		qosPolicyName := cachedEip.Status.QoSPolicy
		if qosPolicyName != "" {
			if err = c.delEipQoS(cachedEip, cachedEip.Status.IP); err != nil {
				klog.Errorf("failed to del qos '%s' in pod, %v", key, err)
				return err
			}
		}
		// Release IP from IPAM before removing finalizer
		c.ipam.ReleaseAddressByPod(key, cachedEip.Spec.ExternalSubnet)

		// Now remove finalizer, which will trigger subnet status update
		if err = c.handleDelIptablesEipFinalizer(key); err != nil {
			klog.Errorf("failed to handle del finalizer for eip %s, %v", key, err)
			return err
		}

		// Trigger QoS Policy reconcile after EIP is deleted
		// This allows the QoS Policy to remove its finalizer if no other EIPs are using it
		if qosPolicyName != "" {
			c.updateQoSPolicyQueue.Add(qosPolicyName)
		}

		return nil
	}
	klog.Infof("handle update eip %s", key)
	// v6 ip address can not use upper case
	if util.ContainsUppercase(cachedEip.Spec.V6ip) {
		err := fmt.Errorf("eip %s v6 ip address %s can not contain upper case", cachedEip.Name, cachedEip.Spec.V6ip)
		klog.Error(err)
		return err
	}
	// eip change ip
	if c.eipChangeIP(cachedEip) {
		err := fmt.Errorf("not support eip change ip, old ip '%s', new ip '%s'", cachedEip.Status.IP, cachedEip.Spec.V4ip)
		klog.Error(err)
		return err
	}
	// make sure vpc nat enabled
	if vpcNatEnabled != "true" {
		err := errors.New("iptables nat gw not enable")
		klog.Error(err)
		return err
	}

	// update qos
	if cachedEip.Status.QoSPolicy != cachedEip.Spec.QoSPolicy {
		if cachedEip.Status.QoSPolicy != "" {
			if err = c.delEipQoS(cachedEip, cachedEip.Status.IP); err != nil {
				klog.Errorf("failed to del qos '%s' in pod, %v", key, err)
				return err
			}
		}
		if cachedEip.Spec.QoSPolicy != "" {
			if err = c.addEipQoS(cachedEip, cachedEip.Status.IP); err != nil {
				klog.Errorf("failed to add qos '%s' in pod, %v", key, err)
				return err
			}
		}

		if err = c.patchEipLabel(key); err != nil {
			klog.Errorf("failed to label qos in eip, %v", err)
			return err
		}

		if err = c.patchEipQoSStatus(key, cachedEip.Spec.QoSPolicy); err != nil {
			klog.Errorf("failed to patch status for eip %s, %v", key, err)
			return err
		}
	}

	// redo
	if !cachedEip.Status.Ready &&
		cachedEip.Status.Redo != "" &&
		cachedEip.Status.IP != "" &&
		cachedEip.DeletionTimestamp.IsZero() {
		gwPod, err := c.getNatGwPod(cachedEip.Spec.NatGwDp)
		if err != nil {
			klog.Error(err)
			return err
		}
		// compare gw pod started time with eip redo time. if redo time before gw pod started. redo again
		eipRedo, _ := time.ParseInLocation("2006-01-02T15:04:05", cachedEip.Status.Redo, time.Local)
		if cachedEip.Status.Ready && cachedEip.Status.IP != "" && gwPod.Status.ContainerStatuses[0].State.Running.StartedAt.Before(&metav1.Time{Time: eipRedo}) {
			// already ok
			klog.V(3).Infof("eip %s already ok", key)
			return nil
		}
		addrV4, err := util.GetIPAddrWithMask(cachedEip.Status.IP, v4Cidr)
		if err != nil {
			err = fmt.Errorf("failed to get eip %s with mask by cidr %s: %w", cachedEip.Status.IP, v4Cidr, err)
			klog.Error(err)
			return err
		}
		if err = c.createEipInPod(cachedEip.Spec.NatGwDp, addrV4); err != nil {
			klog.Errorf("failed to create eip, %v", err)
			return err
		}

		if cachedEip.Spec.QoSPolicy != "" {
			if err = c.addEipQoS(cachedEip, cachedEip.Status.IP); err != nil {
				klog.Errorf("failed to add qos '%s' in pod, %v", key, err)
				return err
			}
		}

		if err = c.patchEipStatus(key, "", "", cachedEip.Spec.QoSPolicy, true); err != nil {
			klog.Errorf("failed to patch status for eip %s, %v", key, err)
			return err
		}
	}
	if err = c.handleAddIptablesEipFinalizer(key); err != nil {
		klog.Errorf("failed to handle add finalizer for eip, %v", err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIptablesEip(eip *kubeovnv1.IptablesEIP) error {
	klog.Infof("handle delete iptables eip %s", eip.Name)

	// For IptablesEIPs deleted without finalizer (race condition or direct deletion),
	// we need to ensure subnet status is updated as a safety net.
	externalNetwork := util.GetExternalNetwork(eip.Spec.ExternalSubnet)
	if externalNetwork != "" {
		c.updateSubnetStatusQueue.Add(externalNetwork)
	}

	return nil
}

func (c *Controller) GetEip(eipName string) (*kubeovnv1.IptablesEIP, error) {
	cachedEip, err := c.iptablesEipsLister.Get(eipName)
	if err != nil {
		klog.Errorf("failed to get eip %s, %v", eipName, err)
		return nil, err
	}
	if cachedEip.Status.IP == "" {
		return nil, fmt.Errorf("eip '%s' is not ready, has no v4ip", eipName)
	}
	eip := cachedEip.DeepCopy()
	return eip, nil
}

func (c *Controller) createEipInPod(dp, addrV4 string) error {
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		klog.Error(err)
		return err
	}
	return c.execNatGwRules(gwPod, natGwEipAdd, []string{addrV4})
}

func (c *Controller) deleteEipInPod(dp, v4Cidr string) error {
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	var delRules []string
	rule := v4Cidr
	delRules = append(delRules, rule)
	if err = c.execNatGwRules(gwPod, natGwEipDel, delRules); err != nil {
		klog.Error(err)
		return err
	}
	return nil
}

func (c *Controller) addOrUpdateEIPBandwidthLimitRules(eip *kubeovnv1.IptablesEIP, v4ip string, rules kubeovnv1.QoSPolicyBandwidthLimitRules) error {
	var err error
	for _, rule := range rules {
		if err = c.addEipQoSInPod(eip.Spec.NatGwDp, v4ip, rule.Direction, rule.Priority, rule.RateMax, rule.BurstMax); err != nil {
			klog.Errorf("failed to set %s eip '%s' qos in pod, %v", rule.Direction, eip.Name, err)
			return err
		}
	}
	return nil
}

// add tc rule for eip in nat gw pod
func (c *Controller) addEipQoS(eip *kubeovnv1.IptablesEIP, v4ip string) error {
	var err error
	qosPolicy, err := c.qosPoliciesLister.Get(eip.Spec.QoSPolicy)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get qos policy %s, %v", eip.Spec.QoSPolicy, err)
		return err
	}
	if !qosPolicy.Status.Shared {
		eips, err := c.iptablesEipsLister.List(
			labels.SelectorFromSet(labels.Set{util.QoSLabel: qosPolicy.Name}))
		if err != nil {
			klog.Errorf("failed to get eip list, %v", err)
			return err
		}
		if len(eips) != 0 {
			if eips[0].Name != eip.Name {
				err := fmt.Errorf("not support unshared qos policy %s to related to multiple eip", eip.Spec.QoSPolicy)
				klog.Error(err)
				return err
			}
		}
	}
	return c.addOrUpdateEIPBandwidthLimitRules(eip, v4ip, qosPolicy.Status.BandwidthLimitRules)
}

func (c *Controller) delEIPBandwidthLimitRules(eip *kubeovnv1.IptablesEIP, v4ip string, rules kubeovnv1.QoSPolicyBandwidthLimitRules) error {
	var err error
	for _, rule := range rules {
		if err = c.delEipQoSInPod(eip.Spec.NatGwDp, v4ip, rule.Direction); err != nil {
			klog.Errorf("failed to del %s eip '%s' qos in pod, %v", rule.Direction, eip.Name, err)
			return err
		}
	}
	return nil
}

// del tc rule for eip in nat gw pod
func (c *Controller) delEipQoS(eip *kubeovnv1.IptablesEIP, v4ip string) error {
	var err error
	qosPolicy, err := c.qosPoliciesLister.Get(eip.Status.QoSPolicy)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get qos policy %s, %v", eip.Status.QoSPolicy, err)
		return err
	}

	return c.delEIPBandwidthLimitRules(eip, v4ip, qosPolicy.Status.BandwidthLimitRules)
}

func (c *Controller) addEipQoSInPod(
	dp, v4ip string, direction kubeovnv1.QoSPolicyRuleDirection, priority int, rate string,
	burst string,
) error {
	if v4ip == "" {
		klog.Infof("v4ip is empty for nat gateway %s, skipping QoS rule addition", dp)
		return nil
	}
	var operation string
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		klog.Error(err)
		return err
	}
	var addRules []string
	rule := fmt.Sprintf("%s,%d,%s,%s", v4ip, priority, rate, burst)
	addRules = append(addRules, rule)

	switch direction {
	case kubeovnv1.QoSDirectionIngress:
		operation = natGwEipIngressQoSAdd
	case kubeovnv1.QoSDirectionEgress:
		operation = natGwEipEgressQoSAdd
	}

	return c.execNatGwRules(gwPod, operation, addRules)
}

func (c *Controller) delEipQoSInPod(dp, v4ip string, direction kubeovnv1.QoSPolicyRuleDirection) error {
	var operation string
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		klog.Error(err)
		return err
	}
	var delRules []string
	delRules = append(delRules, v4ip)

	switch direction {
	case kubeovnv1.QoSDirectionIngress:
		operation = natGwEipIngressQoSDel
	case kubeovnv1.QoSDirectionEgress:
		operation = natGwEipEgressQoSDel
	}

	return c.execNatGwRules(gwPod, operation, delRules)
}

func (c *Controller) acquireStaticEip(name, _, nicName, ip, externalSubnet string) (string, string, string, error) {
	checkConflict := true
	var v4ip, v6ip, mac string
	var err error
	for ipStr := range strings.SplitSeq(ip, ",") {
		if net.ParseIP(ipStr) == nil {
			return "", "", "", fmt.Errorf("failed to parse eip ip %s", ipStr)
		}
	}

	if v4ip, v6ip, mac, err = c.ipam.GetStaticAddress(name, nicName, ip, nil, externalSubnet, checkConflict); err != nil {
		klog.Errorf("failed to get static ip %v, mac %v, subnet %v, err %v", ip, mac, externalSubnet, err)
		return "", "", "", err
	}
	return v4ip, v6ip, mac, nil
}

func (c *Controller) acquireEip(name, _, nicName, externalSubnet string) (string, string, string, error) {
	var skippedAddrs []string
	for {
		ipv4, ipv6, mac, err := c.ipam.GetRandomAddress(name, nicName, nil, externalSubnet, "", skippedAddrs, true)
		if err != nil {
			klog.Error(err)
			return "", "", "", err
		}

		ipv4OK, ipv6OK, err := c.validatePodIP(name, externalSubnet, ipv4, ipv6)
		if err != nil {
			klog.Error(err)
			return "", "", "", err
		}
		if ipv4OK && ipv6OK {
			return ipv4, ipv6, mac, nil
		}
		if !ipv4OK {
			skippedAddrs = append(skippedAddrs, ipv4)
		}
		if !ipv6OK {
			skippedAddrs = append(skippedAddrs, ipv6)
		}
	}
}

func (c *Controller) eipChangeIP(eip *kubeovnv1.IptablesEIP) bool {
	if eip.Status.IP == "" {
		// eip created but not ready
		return false
	}
	if eip.Status.IP != eip.Spec.V4ip {
		return true
	}
	return false
}

func (c *Controller) GetGwBySubnet(name string) (string, string, error) {
	subnet, err := c.subnetsLister.Get(name)
	if err != nil {
		err = fmt.Errorf("faile to get subnet %q: %w", name, err)
		klog.Error(err)
		return "", "", err
	}
	v4, v6 := util.SplitStringIP(subnet.Spec.Gateway)
	return v4, v6, nil
}

func (c *Controller) createOrUpdateEipCR(key, v4ip, v6ip, mac, natGwDp, qos, externalNet string) error {
	needCreate := false
	cachedEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			needCreate = true
		} else {
			klog.Errorf("failed to get eip %s, %v", key, err)
			return err
		}
	}
	if needCreate {
		klog.V(3).Infof("create eip cr %s", key)
		// Create CR with finalizer, labels and status all at once
		_, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Create(context.Background(), &kubeovnv1.IptablesEIP{
			ObjectMeta: metav1.ObjectMeta{
				Name:       key,
				Finalizers: []string{util.KubeOVNControllerFinalizer},
				Labels: map[string]string{
					util.SubnetNameLabel:        externalNet,
					util.EipV4IpLabel:           v4ip,
					util.VpcNatGatewayNameLabel: natGwDp,
				},
			},
			Spec: kubeovnv1.IptablesEIPSpec{
				V4ip:           v4ip,
				V6ip:           v6ip,
				MacAddress:     mac,
				NatGwDp:        natGwDp,
				QoSPolicy:      qos,
				ExternalSubnet: externalNet,
			},
			Status: kubeovnv1.IptablesEIPStatus{
				IP:        v4ip,
				Ready:     true,
				QoSPolicy: qos,
				Nat:       "",
				Redo:      "",
			},
		}, metav1.CreateOptions{})
		if err != nil {
			errMsg := fmt.Errorf("failed to create eip crd %s, %w", key, err)
			klog.Error(errMsg)
			return errMsg
		}
	} else {
		eip := cachedEip.DeepCopy()

		// Ensure labels are set correctly before any update
		if eip.Labels == nil {
			eip.Labels = make(map[string]string)
		}
		eip.Labels[util.SubnetNameLabel] = externalNet
		eip.Labels[util.VpcNatGatewayNameLabel] = natGwDp
		eip.Labels[util.EipV4IpLabel] = v4ip
		if eip.Spec.QoSPolicy != "" {
			eip.Labels[util.QoSLabel] = eip.Spec.QoSPolicy
		}

		if v4ip != "" {
			klog.V(3).Infof("update eip cr %s", key)
			eip.Spec.V4ip = v4ip
			eip.Spec.V6ip = v6ip
			eip.Spec.NatGwDp = natGwDp
			eip.Spec.MacAddress = mac
			eip.Spec.ExternalSubnet = externalNet
			// Update with labels and spec in one call
			if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Update(context.Background(), eip, metav1.UpdateOptions{}); err != nil {
				errMsg := fmt.Errorf("failed to update eip crd %s, %w", key, err)
				klog.Error(errMsg)
				return errMsg
			}
			if eip.Status.IP == "" {
				// eip is ip holder, not support change ip
				eip.Status.IP = v4ip
				// TODO:// ipv6
			}
			eip.Status.Ready = true
			eip.Status.QoSPolicy = qos
			bytes, err := eip.Status.Bytes()
			if err != nil {
				klog.Error(err)
				return err
			}
			if _, err = c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Patch(context.Background(), key, types.MergePatchType,
				bytes, metav1.PatchOptions{}, "status"); err != nil {
				if k8serrors.IsNotFound(err) {
					return nil
				}
				klog.Errorf("failed to patch eip %s, %v", eip.Name, err)
				return err
			}
		}

		if err = c.handleAddIptablesEipFinalizer(key); err != nil {
			klog.Errorf("failed to handle add or update finalizer for eip, %v", err)
			return err
		}
	}
	// Trigger subnet status update after all operations complete
	c.updateSubnetStatusQueue.AddAfter(externalNet, 300*time.Millisecond)
	return nil
}

func (c *Controller) syncIptablesEipFinalizer(cl client.Client) error {
	// migrate depreciated finalizer to new finalizer
	eips := &kubeovnv1.IptablesEIPList{}
	return migrateFinalizers(cl, eips, func(i int) (client.Object, client.Object) {
		if i < 0 || i >= len(eips.Items) {
			return nil, nil
		}
		return eips.Items[i].DeepCopy(), eips.Items[i].DeepCopy()
	})
}

func (c *Controller) handleAddIptablesEipFinalizer(key string) error {
	cachedIptablesEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if !cachedIptablesEip.DeletionTimestamp.IsZero() {
		return nil
	}
	newIptablesEip := cachedIptablesEip.DeepCopy()
	controllerutil.RemoveFinalizer(newIptablesEip, util.DepreciatedFinalizerName)
	controllerutil.AddFinalizer(newIptablesEip, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedIptablesEip, newIptablesEip)
	if err != nil {
		klog.Errorf("failed to generate patch payload for iptables eip '%s', %v", cachedIptablesEip.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Patch(context.Background(), cachedIptablesEip.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for iptables eip '%s', %v", cachedIptablesEip.Name, err)
		return err
	}

	// Trigger subnet status update after finalizer is processed as a fallback
	// This handles cases where finalizer was not added during creation
	// AddFinalizer is idempotent, so this is safe even if finalizer already exists
	externalNetwork := util.GetExternalNetwork(cachedIptablesEip.Spec.ExternalSubnet)
	c.updateSubnetStatusQueue.Add(externalNetwork)
	return nil
}

func (c *Controller) handleDelIptablesEipFinalizer(key string) error {
	cachedIptablesEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if len(cachedIptablesEip.GetFinalizers()) == 0 {
		return nil
	}
	newIptablesEip := cachedIptablesEip.DeepCopy()
	controllerutil.RemoveFinalizer(newIptablesEip, util.DepreciatedFinalizerName)
	controllerutil.RemoveFinalizer(newIptablesEip, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedIptablesEip, newIptablesEip)
	if err != nil {
		klog.Errorf("failed to generate patch payload for iptables eip '%s', %v", cachedIptablesEip.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Patch(context.Background(), cachedIptablesEip.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from iptables eip '%s', %v", cachedIptablesEip.Name, err)
		return err
	}

	// Trigger subnet status update after finalizer is removed
	// This ensures subnet status reflects the IP release
	// Add delay to ensure API server completes the finalizer removal
	externalNetwork := util.GetExternalNetwork(cachedIptablesEip.Spec.ExternalSubnet)
	c.updateSubnetStatusQueue.AddAfter(externalNetwork, 300*time.Millisecond)
	return nil
}

func (c *Controller) patchEipQoSStatus(key, qos string) error {
	var changed bool
	oriEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	eip := oriEip.DeepCopy()

	// update status.qosPolicy
	if eip.Status.QoSPolicy != qos {
		eip.Status.QoSPolicy = qos
		changed = true
	}

	if changed {
		bytes, err := eip.Status.Bytes()
		if err != nil {
			klog.Error(err)
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Patch(context.Background(), key, types.MergePatchType,
			bytes, metav1.PatchOptions{}, "status"); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			klog.Errorf("failed to patch eip %s, %v", eip.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) getIptablesEipNat(eipV4IP string) (string, error) {
	nats := make([]string, 0, 3)
	selector := labels.SelectorFromSet(labels.Set{util.EipV4IpLabel: eipV4IP})
	dnats, err := c.iptablesDnatRulesLister.List(selector)
	if err != nil {
		klog.Errorf("failed to get dnats, %v", err)
		return "", err
	}
	if len(dnats) != 0 {
		nats = append(nats, util.DnatUsingEip)
	}
	fips, err := c.iptablesFipsLister.List(selector)
	if err != nil {
		klog.Errorf("failed to get fips, %v", err)
		return "", err
	}
	if len(fips) != 0 {
		nats = append(nats, util.FipUsingEip)
	}
	snats, err := c.iptablesSnatRulesLister.List(selector)
	if err != nil {
		klog.Errorf("failed to get snats, %v", err)
		return "", err
	}
	if len(snats) != 0 {
		nats = append(nats, util.SnatUsingEip)
	}
	nat := strings.Join(nats, ",")
	return nat, nil
}

func (c *Controller) patchEipStatus(key, v4ip, redo, qos string, ready bool) error {
	oriEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	eip := oriEip.DeepCopy()
	var changed bool
	if eip.Status.Ready != ready {
		eip.Status.Ready = ready
		changed = true
	}

	if redo != "" && eip.Status.Redo != redo {
		eip.Status.Redo = redo
		changed = true
	}

	if ready && v4ip != "" && eip.Status.IP != v4ip {
		eip.Status.IP = v4ip
		changed = true
	}

	nat, err := c.getIptablesEipNat(oriEip.Spec.V4ip)
	if err != nil {
		err := errors.New("failed to get eip nat")
		klog.Error(err)
		return err
	}
	// nat record all kinds of nat rules using this eip
	klog.V(3).Infof("nat of eip %s is %s", eip.Name, nat)
	if eip.Status.Nat != nat {
		eip.Status.Nat = nat
		changed = true
	}

	if qos != "" && eip.Status.QoSPolicy != qos {
		eip.Status.QoSPolicy = qos
		changed = true
	}

	if changed {
		bytes, err := eip.Status.Bytes()
		if err != nil {
			klog.Error(err)
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Patch(context.Background(), key, types.MergePatchType,
			bytes, metav1.PatchOptions{}, "status"); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			klog.Errorf("failed to patch eip %s, %v", eip.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchEipLabel(eipName string) error {
	oriEip, err := c.iptablesEipsLister.Get(eipName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	externalNetwork := util.GetExternalNetwork(oriEip.Spec.ExternalSubnet)

	eip := oriEip.DeepCopy()
	var needUpdateLabel bool
	var op string
	if len(eip.Labels) == 0 {
		op = "add"
		needUpdateLabel = true
		eip.Labels = map[string]string{
			util.SubnetNameLabel:        externalNetwork,
			util.VpcNatGatewayNameLabel: eip.Spec.NatGwDp,
			util.QoSLabel:               eip.Spec.QoSPolicy,
			util.EipV4IpLabel:           eip.Spec.V4ip,
		}
	} else if eip.Labels[util.VpcNatGatewayNameLabel] != eip.Spec.NatGwDp || eip.Labels[util.QoSLabel] != eip.Spec.QoSPolicy {
		op = "replace"
		needUpdateLabel = true
		eip.Labels[util.SubnetNameLabel] = externalNetwork
		eip.Labels[util.VpcNatGatewayNameLabel] = eip.Spec.NatGwDp
		eip.Labels[util.QoSLabel] = eip.Spec.QoSPolicy
		eip.Labels[util.EipV4IpLabel] = eip.Spec.V4ip
	}
	if needUpdateLabel {
		if err := c.updateIptableLabels(eip.Name, op, "eip", eip.Labels); err != nil {
			klog.Errorf("failed to update label of eip %s, %v", eip.Name, err)
			return err
		}
	}
	return err
}
