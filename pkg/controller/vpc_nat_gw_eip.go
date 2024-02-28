package controller

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddIptablesEip(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.Infof("enqueue add iptables eip %s", key)
	c.addIptablesEipQueue.Add(key)
}

func (c *Controller) enqueueUpdateIptablesEip(oldObj, newObj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	oldEip := oldObj.(*kubeovnv1.IptablesEIP)
	newEip := newObj.(*kubeovnv1.IptablesEIP)
	if !newEip.DeletionTimestamp.IsZero() ||
		oldEip.Status.Redo != newEip.Status.Redo ||
		oldEip.Spec.QoSPolicy != newEip.Spec.QoSPolicy {
		klog.Infof("enqueue update iptables eip %s", key)
		c.updateIptablesEipQueue.Add(key)
	}
	externalNetwork := util.GetExternalNetwork(newEip.Spec.ExternalSubnet)
	c.updateSubnetStatusQueue.Add(externalNetwork)
}

func (c *Controller) enqueueDelIptablesEip(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	eip := obj.(*kubeovnv1.IptablesEIP)
	klog.Infof("enqueue del iptables eip %s", key)
	c.delIptablesEipQueue.Add(key)
	externalNetwork := util.GetExternalNetwork(eip.Spec.ExternalSubnet)
	c.updateSubnetStatusQueue.Add(externalNetwork)
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
	cachedEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	if vpcNatEnabled != "true" {
		return fmt.Errorf("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle add iptables eip %s", key)

	if cachedEip.Status.Ready && cachedEip.Status.IP != "" {
		// already ok
		return nil
	}
	var v4ip, v6ip, mac, eipV4Cidr, v4Gw string
	externalNetwork := util.GetExternalNetwork(cachedEip.Spec.ExternalSubnet)
	externalProvider := fmt.Sprintf("%s.%s", externalNetwork, attachmentNs)

	portName := ovs.PodNameToPortName(cachedEip.Name, cachedEip.Namespace, externalProvider)
	if cachedEip.Spec.V4ip != "" {
		if v4ip, v6ip, mac, err = c.acquireStaticEip(cachedEip.Name, cachedEip.Namespace, portName, cachedEip.Spec.V4ip, externalNetwork); err != nil {
			klog.Errorf("failed to acquire static eip, err: %v", err)
			return err
		}
	} else {
		// Random allocate
		if v4ip, v6ip, mac, err = c.acquireEip(cachedEip.Name, cachedEip.Namespace, portName, externalNetwork); err != nil {
			klog.Errorf("failed to allocate eip, err: %v", err)
			return err
		}
	}
	if eipV4Cidr, err = c.getEipV4Cidr(v4ip, externalNetwork); err != nil {
		klog.Errorf("failed to get eip cidr, err: %v", err)
		return err
	}
	if v4Gw, _, err = c.GetGwBySubnet(externalNetwork); err != nil {
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
	if err = c.createOrUpdateEipCR(key, v4ip, v6ip, mac, cachedEip.Spec.NatGwDp, cachedEip.Spec.QoSPolicy, externalNetwork); err != nil {
		klog.Errorf("failed to update eip %s, %v", key, err)
		return err
	}
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

	externalNetwork := util.GetExternalNetwork(cachedEip.Spec.ExternalSubnet)
	// should delete
	if !cachedEip.DeletionTimestamp.IsZero() {
		klog.Infof("clean eip %q in pod", key)
		v4Cidr, err := c.getEipV4Cidr(cachedEip.Status.IP, externalNetwork)
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
		c.ipam.ReleaseAddressByPod(key, cachedEip.Spec.ExternalSubnet)
		return nil
	}
	klog.Infof("handle update eip %s", key)
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
		eipV4Cidr, err := c.getEipV4Cidr(cachedEip.Status.IP, externalNetwork)
		if err != nil {
			klog.Errorf("failed to get eip or v4Cidr, %v", err)
			return err
		}
		var v4Gw string
		if v4Gw, _, err = c.GetGwBySubnet(externalNetwork); err != nil {
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

func (c *Controller) handleDelIptablesEip(key string) error {
	klog.Infof("handle delete iptables eip %s", key)
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

func (c *Controller) createEipInPod(dp, gw, v4Cidr string) error {
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		klog.Error(err)
		return err
	}
	var addRules []string
	rule := fmt.Sprintf("%s,%s", v4Cidr, gw)
	addRules = append(addRules, rule)
	return c.execNatGwRules(gwPod, natGwEipAdd, addRules)
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
	qosPolicy, err := c.qosPoliciesLister.Get(eip.Status.QoSPolicy)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get qos policy %s, %v", eip.Status.QoSPolicy, err)
		return err
	}

	return c.delEIPBandtithLimitRules(eip, v4ip, qosPolicy.Status.BandwidthLimitRules)
}

func (c *Controller) addEipQoSInPod(
	dp, v4ip string, direction kubeovnv1.QoSPolicyRuleDirection, priority int, rate string,
	burst string,
) error {
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
	case kubeovnv1.DirectionIngress:
		operation = natGwEipIngressQoSAdd
	case kubeovnv1.DirectionEgress:
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
	case kubeovnv1.DirectionIngress:
		operation = natGwEipIngressQoSDel
	case kubeovnv1.DirectionEgress:
		operation = natGwEipEgressQoSDel
	}

	return c.execNatGwRules(gwPod, operation, delRules)
}

func (c *Controller) acquireStaticEip(name, _, nicName, ip, externalSubnet string) (string, string, string, error) {
	checkConflict := true
	var v4ip, v6ip, mac string
	var err error
	for _, ipStr := range strings.Split(ip, ",") {
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

func (c *Controller) getEipV4Cidr(v4ip, externalSubnet string) (string, error) {
	extSubnetMask, err := c.ipam.GetSubnetV4Mask(externalSubnet)
	if err != nil {
		klog.Errorf("failed to get eip '%s' mask from subnet %s, %v", v4ip, externalSubnet, err)
		return "", err
	}
	v4IpCidr := fmt.Sprintf("%s/%s", v4ip, extSubnetMask)
	return v4IpCidr, nil
}

func (c *Controller) GetGwBySubnet(name string) (string, string, error) {
	if subnet, ok := c.ipam.Subnets[name]; ok {
		return subnet.V4Gw, subnet.V6Gw, nil
	}
	return "", "", fmt.Errorf("failed to get subnet %s", name)
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
	externalNetwork := util.GetExternalNetwork(cachedEip.Spec.ExternalSubnet)
	if needCreate {
		klog.V(3).Infof("create eip cr %s", key)
		_, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Create(context.Background(), &kubeovnv1.IptablesEIP{
			ObjectMeta: metav1.ObjectMeta{
				Name: key,
				Labels: map[string]string{
					util.SubnetNameLabel:        externalNet,
					util.EipV4IpLabel:           v4ip,
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
		var needUpdateLabel bool
		var op string
		if len(eip.Labels) == 0 {
			op = "add"
			eip.Labels = map[string]string{
				util.SubnetNameLabel:        externalNetwork,
				util.VpcNatGatewayNameLabel: natGwDp,
				util.EipV4IpLabel:           v4ip,
			}
			needUpdateLabel = true
		} else if eip.Labels[util.SubnetNameLabel] != externalNetwork {
			op = "replace"
			eip.Labels[util.SubnetNameLabel] = externalNetwork
			eip.Labels[util.VpcNatGatewayNameLabel] = natGwDp
			needUpdateLabel = true
		}
		if eip.Spec.QoSPolicy != "" && eip.Labels[util.QoSLabel] != eip.Spec.QoSPolicy {
			eip.Labels[util.QoSLabel] = eip.Spec.QoSPolicy
			needUpdateLabel = true
		}
		if needUpdateLabel {
			if err := c.updateIptableLabels(eip.Name, op, "eip", eip.Labels); err != nil {
				klog.Errorf("failed to update eip %s labels, %v", eip.Name, err)
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

func (c *Controller) syncIptablesEipFinalizer(cl client.Client) error {
	// migrate depreciated finalizer to new finalizer
	eips := &kubeovnv1.IptablesEIPList{}
	return updateFinalizers(cl, eips, func(i int) (client.Object, client.Object) {
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
	if cachedIptablesEip.DeletionTimestamp.IsZero() {
		if slices.Contains(cachedIptablesEip.Finalizers, util.KubeOVNControllerFinalizer) {
			return nil
		}
	}
	newIptablesEip := cachedIptablesEip.DeepCopy()
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
	if len(cachedIptablesEip.Finalizers) == 0 {
		return nil
	}
	newIptablesEip := cachedIptablesEip.DeepCopy()
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
		err := fmt.Errorf("failed to get eip nat")
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
