package controller

import (
	"context"
	"fmt"
	"net"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	// external underlay vlan macvlan network attachment definition provider
	MACVLAN_NAD_PROVIDER = fmt.Sprintf("%s.%s", util.VpcExternalNet, ATTACHMENT_NS)
)

func (c *Controller) enqueueAddIptablesEip(obj interface{}) {

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.addIptablesEipQueue.Add(key)
}

func (c *Controller) enqueueUpdateIptablesEip(old, new interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}
	oldEip := old.(*kubeovnv1.IptablesEIP)
	newEip := new.(*kubeovnv1.IptablesEIP)
	if !newEip.DeletionTimestamp.IsZero() ||
		oldEip.Status.Redo != newEip.Status.Redo ||
		oldEip.Spec.QoSPolicy != newEip.Spec.QoSPolicy {
		c.updateIptablesEipQueue.Add(key)
	}
	c.updateSubnetStatusQueue.Add(util.VpcExternalNet)
}

func (c *Controller) enqueueDelIptablesEip(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.delIptablesEipQueue.Add(key)
	c.updateSubnetStatusQueue.Add(util.VpcExternalNet)
}

func (c *Controller) runAddIptablesEipWorker() {
	for c.processNextAddIptablesEipWorkItem() {
	}
}

func (c *Controller) runUpdateIptablesEipWorker() {
	for c.processNextUpdateIptablesEipWorkItem() {
	}
}

func (c *Controller) runResetIptablesEipWorker() {
	for c.processNextResetIptablesEipWorkItem() {
	}
}

func (c *Controller) runDelIptablesEipWorker() {
	for c.processNextDeleteIptablesEipWorkItem() {
	}
}

func (c *Controller) processNextAddIptablesEipWorkItem() bool {
	obj, shutdown := c.addIptablesEipQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addIptablesEipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addIptablesEipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddIptablesEip(key); err != nil {
			c.addIptablesEipQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addIptablesEipQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextResetIptablesEipWorkItem() bool {
	obj, shutdown := c.resetIptablesEipQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.resetIptablesEipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.resetIptablesEipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleResetIptablesEip(key); err != nil {
			c.resetIptablesEipQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.resetIptablesEipQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateIptablesEipWorkItem() bool {
	obj, shutdown := c.updateIptablesEipQueue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.updateIptablesEipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateIptablesEipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateIptablesEip(key); err != nil {
			c.updateIptablesEipQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateIptablesEipQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteIptablesEipWorkItem() bool {
	obj, shutdown := c.delIptablesEipQueue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.delIptablesEipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.delIptablesEipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected eip in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDelIptablesEip(key); err != nil {
			c.delIptablesEipQueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.delIptablesEipQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleAddIptablesEip(key string) error {
	if vpcNatEnabled != "true" {
		return fmt.Errorf("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.Lock(key)
	defer c.vpcNatGwKeyMutex.Unlock(key)

	cachedEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if cachedEip.Status.Ready && cachedEip.Status.IP != "" {
		// already ok
		return nil
	}
	klog.V(3).Infof("handle add eip %s", key)
	var v4ip, v6ip, mac, eipV4Cidr, v4Gw string
	portName := ovs.PodNameToPortName(cachedEip.Name, cachedEip.Namespace, MACVLAN_NAD_PROVIDER)
	if cachedEip.Spec.V4ip != "" {
		if v4ip, v6ip, mac, err = c.acquireStaticEip(cachedEip.Name, cachedEip.Namespace, portName, cachedEip.Spec.V4ip); err != nil {
			klog.Errorf("failed to acquire static eip, err: %v", err)
			return err
		}
	} else {
		// Random allocate
		if v4ip, v6ip, mac, err = c.acquireEip(cachedEip.Name, cachedEip.Namespace, portName); err != nil {
			klog.Errorf("failed to allocate eip, err: %v", err)
			return err
		}
	}
	if eipV4Cidr, err = c.getEipV4Cidr(v4ip); err != nil {
		klog.Errorf("failed to get eip cidr, err: %v", err)
		return err
	}
	if v4Gw, _, err = c.GetGwBySubnet(util.VpcExternalNet); err != nil {
		klog.Errorf("failed to get gw, err: %v", err)
		return err
	}
	if err = c.createEipInPod(cachedEip.Spec.NatGwDp, v4Gw, eipV4Cidr); err != nil {
		klog.Errorf("failed to create eip '%s' in pod, %v", key, err)
		return err
	}

	if cachedEip.Spec.QoSPolicy != "" {
		if err = c.addEipQoS(cachedEip, v4ip); err != nil {
			klog.Errorf("failed to add qos '%s' in pod, %v", key, err)
			return err
		}
	}
	if err = c.createOrUpdateCrdEip(key, v4ip, v6ip, mac, cachedEip.Spec.NatGwDp, cachedEip.Spec.QoSPolicy); err != nil {
		klog.Errorf("failed to update eip %s, %v", key, err)
		return err
	}
	return nil
}

func (c *Controller) handleResetIptablesEip(key string) error {
	eip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	klog.V(3).Infof("handle reset eip %s", key)
	var notUse bool
	switch eip.Status.Nat {
	case util.FipUsingEip:
		notUse = true
	case util.DnatUsingEip:
		// nat change eip not that fast
		dnats, err := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().List(context.Background(), metav1.ListOptions{
			LabelSelector: fields.OneTermEqualSelector(util.VpcNatGatewayNameLabel, key).String(),
		})
		if err != nil {
			klog.Errorf("failed to get dnats, %v", err)
			return err
		}
		notUse = true
		for _, item := range dnats.Items {
			if item.Annotations[util.VpcEipAnnotation] == key {
				notUse = false
				break
			}
		}
	case util.SnatUsingEip:
		// nat change eip not that fast
		snats, err := c.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().List(context.Background(), metav1.ListOptions{
			LabelSelector: fields.OneTermEqualSelector(util.VpcNatGatewayNameLabel, key).String(),
		})
		if err != nil {
			klog.Errorf("failed to get snats, %v", err)
			return err
		}
		notUse = true
		for _, item := range snats.Items {
			if item.Annotations[util.VpcEipAnnotation] == key {
				notUse = false
				break
			}
		}
	default:
		notUse = true
	}

	if notUse {
		if err := c.natLabelEip(key, ""); err != nil {
			klog.Errorf("failed to clean label for eip %s, %v", key, err)
			return err
		}
		if err := c.patchResetEipStatusNat(key, ""); err != nil {
			klog.Errorf("failed to clean status for eip %s, %v", key, err)
			return err
		}
	}
	return nil
}

func (c *Controller) handleUpdateIptablesEip(key string) error {
	c.vpcNatGwKeyMutex.Lock(key)
	defer c.vpcNatGwKeyMutex.Unlock(key)
	cachedEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	// should delete
	if !cachedEip.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("clean eip '%s' in pod", key)
		v4Cidr, err := c.getEipV4Cidr(cachedEip.Status.IP)
		if err != nil {
			klog.Errorf("failed to clean eip %s, %v", key, err)
			return err
		}
		if vpcNatEnabled == "true" {
			if err = c.deleteEipInPod(cachedEip.Spec.NatGwDp, v4Cidr); err != nil {
				klog.Errorf("failed to clean eip '%s' in pod, %v", key, err)
				return err
			}
		}
		if cachedEip.Status.QoSPolicy != "" {
			if err = c.delEipQoS(cachedEip, cachedEip.Status.IP); err != nil {
				klog.Errorf("failed to del qos '%s' in pod, %v", key, err)
				return err
			}
		}
		if err = c.handleDelIptablesEipFinalizer(key); err != nil {
			klog.Errorf("failed to handle del finalizer for eip %s, %v", key, err)
			return err
		}
		return nil
	}
	klog.V(3).Infof("handle update eip %s", key)
	// eip change ip
	if c.eipChangeIP(cachedEip) {
		err := fmt.Errorf("not support eip change ip, old ip '%s', new ip '%s'", cachedEip.Status.IP, cachedEip.Spec.V4ip)
		klog.Error(err)
		return err
	}
	// make sure vpc nat enabled
	if vpcNatEnabled != "true" {
		err := fmt.Errorf("iptables nat gw not enable")
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

		if err = c.qosLabelEIP(key, cachedEip.Spec.QoSPolicy); err != nil {
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
		eipV4Cidr, err := c.getEipV4Cidr(cachedEip.Status.IP)
		if err != nil {
			klog.Errorf("failed to get eip or v4Cidr, %v", err)
			return err
		}
		var v4Gw string
		if v4Gw, _, err = c.GetGwBySubnet(util.VpcExternalNet); err != nil {
			klog.Errorf("failed to get gw, %v", err)
			return err
		}
		if err = c.createEipInPod(cachedEip.Spec.NatGwDp, v4Gw, eipV4Cidr); err != nil {
			klog.Errorf("failed to create eip, %v", err)
			return err
		}

		if cachedEip.Spec.QoSPolicy != "" {
			if err = c.addEipQoS(cachedEip, cachedEip.Status.IP); err != nil {
				klog.Errorf("failed to add qos '%s' in pod, %v", key, err)
				return err
			}
		}

		if err = c.patchEipStatus(key, "", "", "", cachedEip.Spec.QoSPolicy, true); err != nil {
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

func (c *Controller) handleDelIptablesEip(key string) error {
	c.ipam.ReleaseAddressByPod(key)
	klog.V(3).Infof("deleted vpc nat eip %s", key)
	return nil
}

func (c *Controller) GetEip(eipName string) (*kubeovnv1.IptablesEIP, error) {
	cachedEip, err := c.iptablesEipsLister.Get(eipName)
	if err != nil {
		klog.Errorf("failed to get eip %s, %v", eipName, err)
		return nil, err
	}
	if cachedEip.Status.IP == "" || cachedEip.Spec.V4ip == "" {
		return nil, fmt.Errorf("eip '%s' is not ready, has no v4ip", eipName)
	}
	eip := cachedEip.DeepCopy()
	return eip, nil
}

func (c *Controller) createEipInPod(dp, gw, v4Cidr string) error {
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		return err
	}
	var addRules []string
	rule := fmt.Sprintf("%s,%s", v4Cidr, gw)
	addRules = append(addRules, rule)
	if err = c.execNatGwRules(gwPod, natGwEipAdd, addRules); err != nil {
		return err
	}
	return nil
}

func (c *Controller) deleteEipInPod(dp, v4Cidr string) error {
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	var delRules []string
	rule := v4Cidr
	delRules = append(delRules, rule)
	if err = c.execNatGwRules(gwPod, natGwEipDel, delRules); err != nil {
		return err
	}
	return nil
}

func (c *Controller) addOrUpdateEIPBandtithLimitRules(eip *kubeovnv1.IptablesEIP, v4ip string, rules kubeovnv1.QoSPolicyBandwidthLimitRules) error {
	var err error
	for _, rule := range rules {
		if err = c.addEipQoSInPod(eip.Spec.NatGwDp, v4ip, rule.Direction, rule.Priority, rule.RateMax, rule.BurstMax); err != nil {
			klog.Errorf("failed to set ingress eip '%s' qos in pod, %v", eip.Name, err)
			return err
		}
	}
	return nil
}

// add tc rule for eip in nat gw pod
func (c *Controller) addEipQoS(eip *kubeovnv1.IptablesEIP, v4ip string) error {
	var err error
	qosPolicy, err := c.config.KubeOvnClient.KubeovnV1().QoSPolicies().Get(context.Background(), eip.Spec.QoSPolicy, metav1.GetOptions{})
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
	if err != nil {
		klog.Errorf("get qos policy %s failed: %v", eip.Spec.QoSPolicy, err)
		return err
	}
	return c.addOrUpdateEIPBandtithLimitRules(eip, v4ip, qosPolicy.Status.BandwidthLimitRules)
}

func (c *Controller) delEIPBandtithLimitRules(eip *kubeovnv1.IptablesEIP, v4ip string, rules kubeovnv1.QoSPolicyBandwidthLimitRules) error {
	var err error
	for _, rule := range rules {
		// del qos
		if err = c.delEipQoSInPod(eip.Spec.NatGwDp, v4ip, rule.Direction); err != nil {
			klog.Errorf("failed to del egress eip '%s' qos in pod, %v", eip.Name, err)
			return err
		}
	}
	return nil
}

// del tc rule for eip in nat gw pod
func (c *Controller) delEipQoS(eip *kubeovnv1.IptablesEIP, v4ip string) error {
	var err error
	qosPolicy, err := c.config.KubeOvnClient.KubeovnV1().QoSPolicies().Get(context.Background(), eip.Status.QoSPolicy, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("get qos policy %s failed: %v", eip.Status.QoSPolicy, err)
		return err
	}

	return c.delEIPBandtithLimitRules(eip, v4ip, qosPolicy.Status.BandwidthLimitRules)
}

func (c *Controller) addEipQoSInPod(
	dp string, v4ip string, direction kubeovnv1.QoSPolicyRuleDirection, priority int, rate string,
	burst string) error {
	var operation string
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		return err
	}
	var addRules []string
	rule := fmt.Sprintf("%s,%d,%s,%s", v4ip, priority, rate, burst)
	addRules = append(addRules, rule)

	switch direction {
	case kubeovnv1.DirectionIngress:
		operation = natGwEipIngressQoSAdd
	case kubeovnv1.DirectionEgress:
		operation = natGwEipEgressQoSAdd
	}

	if err = c.execNatGwRules(gwPod, operation, addRules); err != nil {
		return err
	}
	return nil
}

func (c *Controller) delEipQoSInPod(dp string, v4ip string, direction kubeovnv1.QoSPolicyRuleDirection) error {
	var operation string
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		return err
	}
	var delRules []string
	delRules = append(delRules, v4ip)

	switch direction {
	case kubeovnv1.DirectionIngress:
		operation = natGwEipIngressQoSDel
	case kubeovnv1.DirectionEgress:
		operation = natGwEipEgressQoSDel
	}

	if err = c.execNatGwRules(gwPod, operation, delRules); err != nil {
		return err
	}
	return nil
}

func (c *Controller) acquireStaticEip(name, namespace, nicName, ip string) (string, string, string, error) {
	checkConflict := true
	var v4ip, v6ip, mac string
	var err error
	for _, ipStr := range strings.Split(ip, ",") {
		if net.ParseIP(ipStr) == nil {
			return "", "", "", fmt.Errorf("failed to parse eip ip %s", ipStr)
		}
	}

	if v4ip, v6ip, mac, err = c.ipam.GetStaticAddress(name, nicName, ip, mac, util.VpcExternalNet, checkConflict); err != nil {
		klog.Errorf("failed to get static ip %v, mac %v, subnet %v, err %v", ip, mac, util.VpcExternalNet, err)
		return "", "", "", err
	}
	return v4ip, v6ip, mac, nil
}

func (c *Controller) acquireEip(name, namespace, nicName string) (string, string, string, error) {
	var skippedAddrs []string
	for {
		ipv4, ipv6, mac, err := c.ipam.GetRandomAddress(name, nicName, "", util.VpcExternalNet, skippedAddrs, true)
		if err != nil {
			return "", "", "", err
		}

		ipv4OK, ipv6OK, err := c.validatePodIP(name, util.VpcExternalNet, ipv4, ipv6)
		if err != nil {
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
	if eip.Status.IP == "" || eip.Spec.V4ip == "" {
		// eip created but not ready
		return false
	}
	if eip.Status.IP != eip.Spec.V4ip {
		return true
	}
	return false
}

func (c *Controller) getEipV4Cidr(v4ip string) (string, error) {
	extSubnetMask, err := c.ipam.GetSubnetV4Mask(util.VpcExternalNet)
	if err != nil {
		klog.Errorf("failed to get eip '%s' mask from subnet %s, %v", v4ip, util.VpcExternalNet, err)
		return "", err
	}
	v4IpCidr := fmt.Sprintf("%s/%s", v4ip, extSubnetMask)
	return v4IpCidr, nil
}

func (c *Controller) GetGwBySubnet(name string) (string, string, error) {
	if subnet, ok := c.ipam.Subnets[name]; ok {
		return subnet.V4Gw, subnet.V6Gw, nil
	} else {
		return "", "", fmt.Errorf("failed to get subnet %s", name)
	}
}

func (c *Controller) createOrUpdateCrdEip(key, v4ip, v6ip, mac, natGwDp, qos string) error {
	cachedEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.V(3).Infof("create eip cr %s", key)
			_, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Create(context.Background(), &kubeovnv1.IptablesEIP{
				ObjectMeta: metav1.ObjectMeta{
					Name: key,
					Labels: map[string]string{
						util.SubnetNameLabel:        util.VpcExternalNet,
						util.VpcNatGatewayNameLabel: natGwDp,
					},
				},
				Spec: kubeovnv1.IptablesEipSpec{
					V4ip:       v4ip,
					V6ip:       v6ip,
					MacAddress: mac,
					NatGwDp:    natGwDp,
				},
			}, metav1.CreateOptions{})

			if err != nil {
				errMsg := fmt.Errorf("failed to create eip crd %s, %v", key, err)
				klog.Error(errMsg)
				return errMsg
			}
		} else {
			errMsg := fmt.Errorf("failed to get eip crd %s, %v", key, err)
			klog.Error(errMsg)
			return errMsg
		}
	} else {
		eip := cachedEip.DeepCopy()
		if v4ip != "" {
			klog.V(3).Infof("update eip cr %s", key)
			eip.Spec.V4ip = v4ip
			eip.Spec.V6ip = v6ip
			eip.Spec.NatGwDp = natGwDp
			eip.Spec.MacAddress = mac
			if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Update(context.Background(), eip, metav1.UpdateOptions{}); err != nil {
				errMsg := fmt.Errorf("failed to update eip crd %s, %v", key, err)
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
		var needUpdateLabel, needUpdateAnno bool
		var op string
		if len(eip.Labels) == 0 {
			op = "add"
			eip.Labels = map[string]string{
				util.SubnetNameLabel:        util.VpcExternalNet,
				util.VpcNatGatewayNameLabel: natGwDp,
			}
			needUpdateLabel = true
		} else if eip.Labels[util.SubnetNameLabel] != util.VpcExternalNet {
			op = "replace"
			eip.Labels[util.SubnetNameLabel] = util.VpcExternalNet
			eip.Labels[util.VpcNatGatewayNameLabel] = natGwDp
			needUpdateLabel = true
		}
		if eip.Spec.QoSPolicy != "" && eip.Labels[util.QoSLabel] != eip.Spec.QoSPolicy {
			eip.Labels[util.QoSLabel] = eip.Spec.QoSPolicy
		}
		if needUpdateLabel {
			if err := c.updateIptableLabels(eip.Name, op, "eip", eip.Labels); err != nil {
				return err
			}
		}
		if needUpdateAnno {
			if eip.Annotations == nil {
				eip.Annotations = make(map[string]string)
			}
			eip.Annotations[util.VpcNatAnnotation] = ""
			if err := c.updateIptableAnnotations(eip.Name, op, "eip", eip.Annotations); err != nil {
				return err
			}
		}
		if err = c.handleAddIptablesEipFinalizer(key); err != nil {
			klog.Errorf("failed to handle add finalizer for eip, %v", err)
			return err
		}
	}
	return nil
}

func (c *Controller) handleAddIptablesEipFinalizer(key string) error {
	cachedIptablesEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if cachedIptablesEip.DeletionTimestamp.IsZero() {
		if util.ContainsString(cachedIptablesEip.Finalizers, util.ControllerName) {
			return nil
		}
	}
	newIptablesEip := cachedIptablesEip.DeepCopy()
	controllerutil.AddFinalizer(newIptablesEip, util.ControllerName)
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
	return nil
}

func (c *Controller) handleDelIptablesEipFinalizer(key string) error {
	cachedIptablesEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if len(cachedIptablesEip.Finalizers) == 0 {
		return nil
	}
	newIptablesEip := cachedIptablesEip.DeepCopy()
	controllerutil.RemoveFinalizer(newIptablesEip, util.ControllerName)
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
	return nil
}

func (c *Controller) patchEipQoSStatus(key, qos string) error {
	var changed bool
	oriEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
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

func (c *Controller) patchEipStatus(key, v4ip, redo, nat, qos string, ready bool) error {
	oriEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
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
	if ready && nat != "" && eip.Status.Nat != nat {
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

func (c *Controller) patchEipNat(key, nat string) error {
	oriEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if oriEip.Status.Nat == nat {
		return nil
	}
	eip := oriEip.DeepCopy()
	eip.Status.Nat = nat
	bytes, err := eip.Status.Bytes()
	if err != nil {
		klog.Errorf("failed to marshal eip %s, %v", eip.Name, err)
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
	return nil
}

func (c *Controller) patchResetEipStatusNat(key, nat string) error {
	oriEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	eip := oriEip.DeepCopy()
	if eip.Status.Nat != nat {
		eip.Status.Nat = nat
		bytes, err := eip.Status.Bytes()
		if err != nil {
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Patch(context.Background(), key, types.MergePatchType,
			bytes, metav1.PatchOptions{}, "status"); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			klog.Errorf("failed to patch eip '%s' nat type, %v", eip.Name, err)
			return err
		}
	}
	return nil
}
func (c *Controller) natLabelEip(eipName, natName string) error {
	oriEip, err := c.iptablesEipsLister.Get(eipName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	eip := oriEip.DeepCopy()
	var needUpdateLabel, needUpdateAnno bool
	var op string
	if len(eip.Labels) == 0 {
		op = "add"
		needUpdateLabel = true
		eip.Labels = map[string]string{
			util.SubnetNameLabel:        util.VpcExternalNet,
			util.VpcNatGatewayNameLabel: eip.Spec.NatGwDp,
		}
	} else if eip.Labels[util.VpcNatGatewayNameLabel] != eip.Spec.NatGwDp {
		op = "replace"
		needUpdateLabel = true
		eip.Labels[util.SubnetNameLabel] = util.VpcExternalNet
		eip.Labels[util.VpcNatGatewayNameLabel] = eip.Spec.NatGwDp
	}
	if needUpdateLabel {
		if err := c.updateIptableLabels(eip.Name, op, "eip", eip.Labels); err != nil {
			return err
		}
	}

	if len(eip.Annotations) == 0 {
		op = "add"
		needUpdateAnno = true
		eip.Annotations = map[string]string{
			util.VpcNatAnnotation: natName,
		}
	} else if eip.Annotations[util.VpcNatAnnotation] != natName {
		op = "replace"
		needUpdateAnno = true
		eip.Annotations[util.VpcNatAnnotation] = natName
	}
	if needUpdateAnno {
		if err := c.updateIptableAnnotations(eip.Name, op, "eip", eip.Annotations); err != nil {
			return err
		}
	}
	return err
}

func (c *Controller) qosLabelEIP(eipName, qosName string) error {
	oriEip, err := c.iptablesEipsLister.Get(eipName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	eip := oriEip.DeepCopy()
	var needUpdateLabel bool
	var op string
	if len(eip.Labels) == 0 {
		op = "add"
		needUpdateLabel = true
		eip.Labels = map[string]string{
			util.QoSLabel: qosName,
		}
	} else if eip.Labels[util.QoSLabel] != qosName {
		op = "replace"
		needUpdateLabel = true
		eip.Labels[util.QoSLabel] = qosName
	}
	if needUpdateLabel {
		if err := c.updateIptableLabels(eip.Name, op, "eip", eip.Labels); err != nil {
			return err
		}
	}

	return err
}
