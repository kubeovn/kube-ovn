package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
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
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddIptablesFip(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add iptables fip %s", key)
	c.addIptablesFipQueue.Add(key)
}

func (c *Controller) enqueueUpdateIptablesFip(oldObj, newObj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	oldFip := oldObj.(*kubeovnv1.IptablesFIPRule)
	newFip := newObj.(*kubeovnv1.IptablesFIPRule)
	if !newFip.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("enqueue update to clean fip %s", key)
		c.updateIptablesFipQueue.Add(key)
		return
	}
	if oldFip.Spec.EIP != newFip.Spec.EIP {
		// to notify old eip to remove nat label
		c.resetIptablesEipQueue.Add(oldFip.Spec.EIP)
	}
	if oldFip.Status.V4ip != newFip.Status.V4ip ||
		oldFip.Spec.EIP != newFip.Spec.EIP ||
		oldFip.Status.Redo != newFip.Status.Redo ||
		oldFip.Spec.InternalIP != newFip.Spec.InternalIP {
		klog.V(3).Infof("enqueue update fip %s", key)
		c.updateIptablesFipQueue.Add(key)
		return
	}
}

func (c *Controller) enqueueDelIptablesFip(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.delIptablesFipQueue.Add(key)
}

func (c *Controller) enqueueAddIptablesDnatRule(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add iptables dnat %s", key)
	c.addIptablesDnatRuleQueue.Add(key)
}

func (c *Controller) enqueueUpdateIptablesDnatRule(oldObj, newObj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	oldDnat := oldObj.(*kubeovnv1.IptablesDnatRule)
	newDnat := newObj.(*kubeovnv1.IptablesDnatRule)
	if !newDnat.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("enqueue update to clean dnat %s", key)
		c.updateIptablesDnatRuleQueue.Add(key)
		return
	}

	if oldDnat.Spec.EIP != newDnat.Spec.EIP {
		// to notify old eip to remove nat table
		c.resetIptablesEipQueue.Add(oldDnat.Spec.EIP)
	}

	if oldDnat.Status.V4ip != newDnat.Status.V4ip ||
		oldDnat.Spec.EIP != newDnat.Spec.EIP ||
		oldDnat.Status.Redo != newDnat.Status.Redo ||
		oldDnat.Spec.Protocol != newDnat.Spec.Protocol ||
		oldDnat.Spec.InternalIP != newDnat.Spec.InternalIP ||
		oldDnat.Spec.InternalPort != newDnat.Spec.InternalPort ||
		oldDnat.Spec.ExternalPort != newDnat.Spec.ExternalPort {
		klog.V(3).Infof("enqueue update dnat %s", key)
		c.updateIptablesDnatRuleQueue.Add(key)
		return
	}
}

func (c *Controller) enqueueDelIptablesDnatRule(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.delIptablesDnatRuleQueue.Add(key)
}

func (c *Controller) enqueueAddIptablesSnatRule(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.addIptablesSnatRuleQueue.Add(key)
}

func (c *Controller) enqueueUpdateIptablesSnatRule(oldObj, newObj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	oldSnat := oldObj.(*kubeovnv1.IptablesSnatRule)
	newSnat := newObj.(*kubeovnv1.IptablesSnatRule)
	if !newSnat.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("enqueue update to clean snat %s", key)
		c.updateIptablesSnatRuleQueue.Add(key)
		return
	}
	if oldSnat.Spec.EIP != newSnat.Spec.EIP {
		// to notify old eip to remove nat label
		c.resetIptablesEipQueue.Add(oldSnat.Spec.EIP)
	}
	if oldSnat.Status.V4ip != newSnat.Status.V4ip ||
		oldSnat.Spec.EIP != newSnat.Spec.EIP ||
		oldSnat.Status.Redo != newSnat.Status.Redo ||
		oldSnat.Spec.InternalCIDR != newSnat.Spec.InternalCIDR {
		klog.V(3).Infof("enqueue update snat %s", key)
		c.updateIptablesSnatRuleQueue.Add(key)
		return
	}
}

func (c *Controller) enqueueDelIptablesSnatRule(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.delIptablesSnatRuleQueue.Add(key)
}

func (c *Controller) runAddIptablesFipWorker() {
	for c.processNextAddIptablesFipWorkItem() {
	}
}

func (c *Controller) runUpdateIptablesFipWorker() {
	for c.processNextUpdateIptablesFipWorkItem() {
	}
}

func (c *Controller) runDelIptablesFipWorker() {
	for c.processNextDeleteIptablesFipWorkItem() {
	}
}

func (c *Controller) runAddIptablesDnatRuleWorker() {
	for c.processNextAddIptablesDnatRuleWorkItem() {
	}
}

func (c *Controller) runUpdateIptablesDnatRuleWorker() {
	for c.processNextUpdateIptablesDnatRuleWorkItem() {
	}
}

func (c *Controller) runDelIptablesDnatRuleWorker() {
	for c.processNextDeleteIptablesDnatRuleWorkItem() {
	}
}

func (c *Controller) runAddIptablesSnatRuleWorker() {
	for c.processNextAddIptablesSnatRuleWorkItem() {
	}
}

func (c *Controller) runUpdateIptablesSnatRuleWorker() {
	for c.processNextUpdateIptablesSnatRuleWorkItem() {
	}
}

func (c *Controller) runDelIptablesSnatRuleWorker() {
	for c.processNextDeleteIptablesSnatRuleWorkItem() {
	}
}

func (c *Controller) processNextAddIptablesFipWorkItem() bool {
	obj, shutdown := c.addIptablesFipQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addIptablesFipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addIptablesFipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddIptablesFip(key); err != nil {
			c.addIptablesFipQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addIptablesFipQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateIptablesFipWorkItem() bool {
	obj, shutdown := c.updateIptablesFipQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateIptablesFipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateIptablesFipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateIptablesFip(key); err != nil {
			c.updateIptablesFipQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateIptablesFipQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteIptablesFipWorkItem() bool {
	obj, shutdown := c.delIptablesFipQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.delIptablesFipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.delIptablesFipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected fip in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDelIptablesFip(key); err != nil {
			c.delIptablesFipQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.delIptablesFipQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextAddIptablesDnatRuleWorkItem() bool {
	obj, shutdown := c.addIptablesDnatRuleQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addIptablesDnatRuleQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addIptablesDnatRuleQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddIptablesDnatRule(key); err != nil {
			c.addIptablesDnatRuleQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addIptablesDnatRuleQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateIptablesDnatRuleWorkItem() bool {
	obj, shutdown := c.updateIptablesDnatRuleQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateIptablesDnatRuleQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateIptablesDnatRuleQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateIptablesDnatRule(key); err != nil {
			c.updateIptablesDnatRuleQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateIptablesDnatRuleQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteIptablesDnatRuleWorkItem() bool {
	obj, shutdown := c.delIptablesDnatRuleQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.delIptablesDnatRuleQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.delIptablesDnatRuleQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected dnat in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDelIptablesDnatRule(key); err != nil {
			c.delIptablesDnatRuleQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.delIptablesDnatRuleQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextAddIptablesSnatRuleWorkItem() bool {
	obj, shutdown := c.addIptablesSnatRuleQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addIptablesSnatRuleQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addIptablesSnatRuleQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddIptablesSnatRule(key); err != nil {
			c.addIptablesSnatRuleQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addIptablesSnatRuleQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateIptablesSnatRuleWorkItem() bool {
	obj, shutdown := c.updateIptablesSnatRuleQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateIptablesSnatRuleQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateIptablesSnatRuleQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateIptablesSnatRule(key); err != nil {
			c.updateIptablesSnatRuleQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateIptablesSnatRuleQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteIptablesSnatRuleWorkItem() bool {
	obj, shutdown := c.delIptablesSnatRuleQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.delIptablesSnatRuleQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.delIptablesSnatRuleQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected snat in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDelIptablesSnatRule(key); err != nil {
			c.delIptablesSnatRuleQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.delIptablesSnatRuleQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleAddIptablesFip(key string) error {
	if vpcNatEnabled != "true" {
		return fmt.Errorf("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle add iptables fip %s", key)

	fip, err := c.iptablesFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if fip.Status.Ready && fip.Status.V4ip != "" {
		// already ok
		return nil
	}
	klog.V(3).Infof("handle add fip %s", key)
	// get eip
	eipName := fip.Spec.EIP
	if eipName == "" {
		return fmt.Errorf("failed to create fip rule, should set eip")
	}
	eip, err := c.GetEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}

	if err = c.fipTryUseEip(key, eip.Spec.V4ip); err != nil {
		err = fmt.Errorf("failed to create fip %s, %v", key, err)
		klog.Error(err)
		return err
	}

	// create fip nat
	if err = c.createFipInPod(eip.Spec.NatGwDp, eip.Status.IP, fip.Spec.InternalIP); err != nil {
		klog.Errorf("failed to create fip, %v", err)
		return err
	}
	if err = c.patchFipStatus(key, eip.Status.IP, eip.Spec.V6ip, eip.Spec.NatGwDp, "", true); err != nil {
		klog.Errorf("failed to patch status for fip %s, %v", key, err)
		return err
	}
	// label too long cause error
	if err = c.patchFipLabel(key, eip); err != nil {
		klog.Errorf("failed to update label for fip %s, %v", key, err)
		return err
	}
	if err = c.handleAddIptablesFipFinalizer(key); err != nil {
		klog.Errorf("failed to handle add finalizer for fip, %v", err)
		return err
	}
	if err = c.patchEipStatus(eipName, "", "", "", true); err != nil {
		// refresh eip nats
		klog.Errorf("failed to patch fip use eip %s, %v", key, err)
		return err
	}
	return nil
}

func (c *Controller) fipTryUseEip(fipName, eipV4IP string) error {
	// check if has another fip using this eip already
	selector := labels.SelectorFromSet(labels.Set{util.EipV4IpLabel: eipV4IP})
	usingFips, err := c.iptablesFipsLister.List(selector)
	if err != nil {
		klog.Errorf("failed to get fips, %v", err)
		return err
	}
	for _, uf := range usingFips {
		if uf.Name != fipName {
			err = fmt.Errorf("%s is using by the other fip %s", eipV4IP, uf.Name)
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *Controller) handleUpdateIptablesFip(key string) error {
	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle update iptables fip %s", key)

	cachedFip, err := c.iptablesFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	// should delete
	if !cachedFip.DeletionTimestamp.IsZero() {
		if vpcNatEnabled == "true" {
			klog.V(3).Infof("clean fip '%s' in pod", key)
			if err = c.deleteFipInPod(cachedFip.Status.NatGwDp, cachedFip.Status.V4ip, cachedFip.Status.InternalIP); err != nil {
				klog.Errorf("failed to delete fip %s, %v", key, err)
				return err
			}
		}
		if err = c.handleDelIptablesFipFinalizer(key); err != nil {
			klog.Errorf("failed to handle del finalizer for fip, %v", err)
			return err
		}
		//  reset eip
		c.resetIptablesEipQueue.AddAfter(cachedFip.Spec.EIP, 3*time.Second)
		return nil
	}
	klog.V(3).Infof("handle update fip %s", key)
	// add or update should make sure vpc nat enabled
	if vpcNatEnabled != "true" {
		return fmt.Errorf("iptables nat gw not enable")
	}
	eipName := cachedFip.Spec.EIP
	if eipName == "" {
		return fmt.Errorf("failed to update fip rule, should set eip")
	}
	eip, err := c.GetEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}

	if err = c.fipTryUseEip(key, eip.Spec.V4ip); err != nil {
		err = fmt.Errorf("failed to update fip %s, %v", key, err)
		klog.Error(err)
		return err
	}

	klog.V(3).Infof("fip change ip, old ip '%s', new ip %s", cachedFip.Status.V4ip, eip.Status.IP)
	if err = c.deleteFipInPod(cachedFip.Status.NatGwDp, cachedFip.Status.V4ip, cachedFip.Status.InternalIP); err != nil {
		klog.Errorf("failed to delete old fip, %v", err)
		return err
	}
	if err = c.createFipInPod(eip.Spec.NatGwDp, eip.Status.IP, cachedFip.Spec.InternalIP); err != nil {
		klog.Errorf("failed to create new fip, %v", err)
		return err
	}
	if err = c.patchFipStatus(key, eip.Status.IP, eip.Spec.V6ip, eip.Spec.NatGwDp, "", true); err != nil {
		klog.Errorf("failed to patch status for fip '%s', %v", key, err)
		return err
	}
	// fip change eip
	if c.fipChangeEip(cachedFip, eip) {
		// label too long cause error
		if err = c.patchFipLabel(key, eip); err != nil {
			klog.Errorf("failed to update label for fip %s, %v", key, err)
			return err
		}
		if err = c.patchEipStatus(eipName, "", "", "", true); err != nil {
			// refresh eip nats
			klog.Errorf("failed to patch fip use eip %s, %v", key, err)
			return err
		}
		return nil
	}
	// redo
	if !cachedFip.Status.Ready &&
		cachedFip.Status.Redo != "" &&
		cachedFip.Status.V4ip != "" &&
		cachedFip.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("reapply fip '%s' in pod", key)
		gwPod, err := c.getNatGwPod(eip.Spec.NatGwDp)
		if err != nil {
			klog.Error(err)
			return err
		}
		// compare gw pod started time with fip redo time. if fip redo time before gw pod started. should redo again
		fipRedo, _ := time.ParseInLocation("2006-01-02T15:04:05", cachedFip.Status.Redo, time.Local)
		if cachedFip.Status.Ready && cachedFip.Status.V4ip != "" && gwPod.Status.ContainerStatuses[0].State.Running.StartedAt.Before(&metav1.Time{Time: fipRedo}) {
			// already ok
			klog.V(3).Infof("fip %s already ok", key)
			return nil
		}
		if err = c.createFipInPod(eip.Spec.NatGwDp, cachedFip.Status.V4ip, cachedFip.Spec.InternalIP); err != nil {
			klog.Errorf("failed to create fip, %v", err)
			return err
		}
		if err = c.patchFipStatus(key, "", "", "", "", true); err != nil {
			klog.Errorf("failed to patch status for fip %s, %v", key, err)
			return err
		}
	}
	if err = c.handleAddIptablesFipFinalizer(key); err != nil {
		klog.Errorf("failed to handle add finalizer for fip %s, %v", key, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIptablesFip(key string) error {
	klog.V(3).Infof("deleted iptables fip %s", key)
	return nil
}

func (c *Controller) handleAddIptablesDnatRule(key string) error {
	if vpcNatEnabled != "true" {
		return fmt.Errorf("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle add iptables dnat rule %s", key)

	dnat, err := c.iptablesDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if dnat.Status.Ready && dnat.Status.V4ip != "" {
		// already ok
		return nil
	}
	klog.V(3).Infof("handle add iptables dnat %s", key)
	eipName := dnat.Spec.EIP
	if eipName == "" {
		return fmt.Errorf("failed to create dnat rule, should set eip")
	}

	eip, err := c.GetEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}
	if dup, err := c.isDnatDuplicated(eip.Spec.NatGwDp, eipName, dnat.Name, dnat.Spec.ExternalPort); dup || err != nil {
		klog.Error(err)
		return err
	}
	// create nat
	if err = c.createDnatInPod(eip.Spec.NatGwDp, dnat.Spec.Protocol,
		eip.Status.IP, dnat.Spec.InternalIP,
		dnat.Spec.ExternalPort, dnat.Spec.InternalPort); err != nil {
		klog.Errorf("failed to create dnat, %v", err)
		return err
	}
	if err = c.patchDnatStatus(key, eip.Status.IP, eip.Spec.V6ip, eip.Spec.NatGwDp, "", true); err != nil {
		klog.Errorf("failed to patch status for dnat %s, %v", key, err)
		return err
	}
	// label too long cause error
	if err = c.patchDnatLabel(key, eip); err != nil {
		klog.Errorf("failed to patch label for dnat %s, %v", key, err)
		return err
	}
	if err = c.handleAddIptablesDnatFinalizer(key); err != nil {
		klog.Errorf("failed to handle add finalizer for dnat, %v", err)
		return err
	}
	if err = c.patchEipStatus(eipName, "", "", "", true); err != nil {
		// refresh eip nats
		klog.Errorf("failed to patch dnat use eip %s, %v", key, err)
		return err
	}
	return nil
}

func (c *Controller) handleUpdateIptablesDnatRule(key string) error {
	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle update iptables fip %s", key)

	cachedDnat, err := c.iptablesDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	// should delete
	if !cachedDnat.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("clean dnat '%s' in pod", key)
		if vpcNatEnabled == "true" {
			if err = c.deleteDnatInPod(cachedDnat.Status.NatGwDp, cachedDnat.Status.Protocol,
				cachedDnat.Status.V4ip, cachedDnat.Status.InternalIP,
				cachedDnat.Status.ExternalPort, cachedDnat.Status.InternalPort); err != nil {
				klog.Errorf("failed to delete dnat, %v", err)
				return err
			}
		}
		if err = c.handleDelIptablesDnatFinalizer(key); err != nil {
			klog.Errorf("failed to handle add finalizer for dnat %s, %v", key, err)
			return err
		}
		//  reset eip
		c.resetIptablesEipQueue.AddAfter(cachedDnat.Spec.EIP, 3*time.Second)
		return nil
	}
	klog.V(3).Infof("handle update dnat %s", key)
	eipName := cachedDnat.Spec.EIP
	if eipName == "" {
		return fmt.Errorf("failed to update fip rule, should set eip")
	}
	eip, err := c.GetEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}
	if dup, err := c.isDnatDuplicated(cachedDnat.Status.NatGwDp, eipName, cachedDnat.Name, cachedDnat.Spec.ExternalPort); dup || err != nil {
		klog.Errorf("failed to update dnat, %v", err)
		return err
	}
	// add or update should make sure vpc nat enabled
	if vpcNatEnabled != "true" {
		return fmt.Errorf("iptables nat gw not enable")
	}

	if err = c.deleteDnatInPod(cachedDnat.Status.NatGwDp, cachedDnat.Status.Protocol,
		cachedDnat.Status.V4ip, cachedDnat.Status.InternalIP,
		cachedDnat.Status.ExternalPort, cachedDnat.Status.InternalPort); err != nil {
		klog.Errorf("failed to delete old dnat, %v", err)
		return err
	}
	if err = c.createDnatInPod(eip.Spec.NatGwDp, cachedDnat.Spec.Protocol,
		eip.Status.IP, cachedDnat.Spec.InternalIP,
		cachedDnat.Spec.ExternalPort, cachedDnat.Spec.InternalPort); err != nil {
		klog.Errorf("failed to create new dnat %s, %v", key, err)
		return err
	}
	if err = c.patchDnatStatus(key, eip.Status.IP, eip.Spec.V6ip, eip.Spec.NatGwDp, "", true); err != nil {
		klog.Errorf("failed to patch status for dnat %s , %v", key, err)
		return err
	}
	// dnat change eip
	if c.dnatChangeEip(cachedDnat, eip) {
		klog.V(3).Infof("dnat change ip, old ip '%s', new ip %s", cachedDnat.Status.V4ip, eip.Status.IP)
		// label too long cause error
		if err = c.patchDnatLabel(key, eip); err != nil {
			klog.Errorf("failed to patch label for dnat %s, %v", key, err)
			return err
		}
		if err = c.patchEipStatus(eipName, "", "", "", true); err != nil {
			// refresh eip nats
			klog.Errorf("failed to patch dnat use eip %s, %v", key, err)
			return err
		}
	}
	// redo
	if !cachedDnat.Status.Ready &&
		cachedDnat.Status.Redo != "" &&
		cachedDnat.Status.V4ip != "" &&
		cachedDnat.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("reapply dnat in pod for %s", key)
		gwPod, err := c.getNatGwPod(eip.Spec.NatGwDp)
		if err != nil {
			klog.Error(err)
			return err
		}
		// compare gw pod started time with dnat redo time. if redo time before gw pod started. redo again
		dnatRedo, _ := time.ParseInLocation("2006-01-02T15:04:05", cachedDnat.Status.Redo, time.Local)
		if cachedDnat.Status.Ready && cachedDnat.Status.V4ip != "" && gwPod.Status.ContainerStatuses[0].State.Running.StartedAt.Before(&metav1.Time{Time: dnatRedo}) {
			// already ok
			klog.V(3).Infof("dnat %s already ok", key)
			return nil
		}
		if err = c.createDnatInPod(eip.Spec.NatGwDp, cachedDnat.Spec.Protocol,
			cachedDnat.Status.V4ip, cachedDnat.Spec.InternalIP,
			cachedDnat.Spec.ExternalPort, cachedDnat.Spec.InternalPort); err != nil {
			klog.Errorf("failed to create dnat %s, %v", key, err)
			return err
		}
		if err = c.patchDnatStatus(key, "", "", "", "", true); err != nil {
			klog.Errorf("failed to patch status for dnat %s, %v", key, err)
			return err
		}
	}
	if err = c.handleAddIptablesDnatFinalizer(key); err != nil {
		klog.Errorf("failed to handle add finalizer for dnat %s, %v", key, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIptablesDnatRule(key string) error {
	klog.V(3).Infof("deleted iptables dnat %s", key)
	return nil
}

func (c *Controller) handleAddIptablesSnatRule(key string) error {
	if vpcNatEnabled != "true" {
		return fmt.Errorf("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle add iptables snat rule %s", key)

	snat, err := c.iptablesSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if snat.Status.Ready && snat.Status.V4ip != "" {
		// already ok
		return nil
	}
	klog.V(3).Infof("handle add iptables snat %s", key)
	eipName := snat.Spec.EIP
	if eipName == "" {
		return fmt.Errorf("failed to create snat rule, should set eip")
	}

	eip, err := c.GetEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}
	// create snat
	v4Cidr, _ := util.SplitStringIP(snat.Spec.InternalCIDR)
	if v4Cidr == "" {
		// only support IPv4 snat
		err = fmt.Errorf("failed to get snat v4 internal cidr, original cidr is %s", snat.Spec.InternalCIDR)
		return err
	}
	if err = c.createSnatInPod(eip.Spec.NatGwDp, eip.Status.IP, v4Cidr); err != nil {
		klog.Errorf("failed to create snat, %v", err)
		return err
	}
	if err = c.patchSnatStatus(key, eip.Status.IP, eip.Spec.V6ip, eip.Spec.NatGwDp, "", true); err != nil {
		klog.Errorf("failed to update status for snat %s, %v", key, err)
		return err
	}
	if err = c.patchSnatLabel(key, eip); err != nil {
		klog.Errorf("failed to patch label for snat %s, %v", key, err)
		return err
	}
	if err = c.handleAddIptablesSnatFinalizer(key); err != nil {
		klog.Errorf("failed to handle add finalizer for snat, %v", err)
		return err
	}
	if err = c.patchEipStatus(eipName, "", "", "", true); err != nil {
		// refresh eip nats
		klog.Errorf("failed to patch snat use eip %s, %v", key, err)
		return err
	}
	return nil
}

func (c *Controller) handleUpdateIptablesSnatRule(key string) error {
	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle update iptables snat rule %s", key)

	cachedSnat, err := c.iptablesSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	v4Cidr, _ := util.SplitStringIP(cachedSnat.Status.InternalCIDR)
	if v4Cidr == "" {
		err = fmt.Errorf("failed to get snat v4 internal cidr, original cidr is %s", cachedSnat.Status.InternalCIDR)
		return err
	}
	v4CidrSpec, _ := util.SplitStringIP(cachedSnat.Spec.InternalCIDR)
	if v4CidrSpec == "" {
		err = fmt.Errorf("failed to get snat v4 internal cidr, original cidr is %s", cachedSnat.Spec.InternalCIDR)
		return err
	}
	// should delete
	if !cachedSnat.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("clean snat '%s' in pod", key)
		if vpcNatEnabled == "true" {
			if err = c.deleteSnatInPod(cachedSnat.Status.NatGwDp, cachedSnat.Status.V4ip, v4Cidr); err != nil {
				klog.Errorf("failed to delete snat, %v", err)
				return err
			}
		}
		if err = c.handleDelIptablesSnatFinalizer(key); err != nil {
			klog.Errorf("failed to handle del finalizer for snat %s, %v", key, err)
			return err
		}
		//  reset eip
		c.resetIptablesEipQueue.AddAfter(cachedSnat.Spec.EIP, 3*time.Second)
		return nil
	}
	klog.V(3).Infof("handle update snat %s", key)
	eipName := cachedSnat.Spec.EIP
	if eipName == "" {
		return fmt.Errorf("failed to update fip rule, should set eip")
	}
	eip, err := c.GetEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}

	// add or update should make sure vpc nat enabled
	if vpcNatEnabled != "true" {
		return fmt.Errorf("iptables nat gw not enable")
	}

	klog.V(3).Infof("snat change ip, old ip %s, new ip %s", cachedSnat.Status.V4ip, eip.Status.IP)
	if err = c.deleteSnatInPod(cachedSnat.Status.NatGwDp, cachedSnat.Status.V4ip, v4Cidr); err != nil {
		klog.Errorf("failed to delete old snat, %v", err)
		return err
	}
	if err = c.createSnatInPod(cachedSnat.Status.NatGwDp, eip.Status.IP, v4CidrSpec); err != nil {
		klog.Errorf("failed to create new snat, %v", err)
		return err
	}
	if err = c.patchSnatStatus(key, eip.Status.IP, eip.Spec.V6ip, eip.Spec.NatGwDp, "", true); err != nil {
		klog.Errorf("failed to patch status for snat %s, %v", key, err)
		return err
	}
	// snat change eip
	if c.snatChangeEip(cachedSnat, eip) {
		if err = c.patchSnatLabel(key, eip); err != nil {
			klog.Errorf("failed to patch label for snat %s, %v", key, err)
			return err
		}
		if err = c.patchEipStatus(eipName, "", "", "", true); err != nil {
			// refresh eip nats
			klog.Errorf("failed to patch fip use eip %s, %v", key, err)
			return err
		}
	}
	// redo
	if !cachedSnat.Status.Ready &&
		cachedSnat.Status.Redo != "" &&
		cachedSnat.Status.V4ip != "" &&
		cachedSnat.DeletionTimestamp.IsZero() {
		gwPod, err := c.getNatGwPod(eip.Spec.NatGwDp)
		if err != nil {
			klog.Error(err)
			return err
		}
		// compare gw pod started time with snat redo time. if redo time before gw pod started. redo again
		snatRedo, _ := time.ParseInLocation("2006-01-02T15:04:05", cachedSnat.Status.Redo, time.Local)
		if cachedSnat.Status.Ready && cachedSnat.Status.V4ip != "" && gwPod.Status.ContainerStatuses[0].State.Running.StartedAt.Before(&metav1.Time{Time: snatRedo}) {
			// already ok
			klog.V(3).Infof("snat %s already ok", key)
			return nil
		}
		if err = c.createSnatInPod(cachedSnat.Status.NatGwDp, cachedSnat.Status.V4ip, v4CidrSpec); err != nil {
			klog.Errorf("failed to create new snat, %v", err)
			return err
		}
		if err = c.patchSnatStatus(key, "", "", "", "", true); err != nil {
			klog.Errorf("failed to patch status for snat %s, %v", key, err)
			return err
		}
	}
	if err = c.handleAddIptablesSnatFinalizer(key); err != nil {
		klog.Errorf("failed to handle add finalizer for snat %s, %v", key, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIptablesSnatRule(key string) error {
	klog.V(3).Infof("deleted iptables snat %s", key)
	return nil
}

func (c *Controller) syncIptablesFipFinalizer(cl client.Client) error {
	// migrate depreciated finalizer to new finalizer
	rules := &kubeovnv1.IptablesFIPRuleList{}
	return updateFinalizers(cl, rules, func(i int) (client.Object, client.Object) {
		if i < 0 || i >= len(rules.Items) {
			return nil, nil
		}
		return rules.Items[i].DeepCopy(), rules.Items[i].DeepCopy()
	})
}

func (c *Controller) handleAddIptablesFipFinalizer(key string) error {
	cachedIptablesFip, err := c.iptablesFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if cachedIptablesFip.DeletionTimestamp.IsZero() {
		if slices.Contains(cachedIptablesFip.Finalizers, util.KubeOVNControllerFinalizer) {
			return nil
		}
	}
	newIptablesFip := cachedIptablesFip.DeepCopy()
	controllerutil.AddFinalizer(newIptablesFip, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedIptablesFip, newIptablesFip)
	if err != nil {
		klog.Errorf("failed to generate patch payload for iptables fip '%s', %v", cachedIptablesFip.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Patch(context.Background(), cachedIptablesFip.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for iptables fip '%s', %v", cachedIptablesFip.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIptablesFipFinalizer(key string) error {
	cachedIptablesFip, err := c.iptablesFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if len(cachedIptablesFip.Finalizers) == 0 {
		return nil
	}
	newIptablesFip := cachedIptablesFip.DeepCopy()
	controllerutil.RemoveFinalizer(newIptablesFip, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedIptablesFip, newIptablesFip)
	if err != nil {
		klog.Errorf("failed to generate patch payload for iptables fip '%s', %v", cachedIptablesFip.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Patch(context.Background(), cachedIptablesFip.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from iptables fip '%s', %v", cachedIptablesFip.Name, err)
		return err
	}
	return nil
}

func (c *Controller) syncIptablesDnatFinalizer(cl client.Client) error {
	// migrate depreciated finalizer to new finalizer
	rules := &kubeovnv1.IptablesDnatRuleList{}
	return updateFinalizers(cl, rules, func(i int) (client.Object, client.Object) {
		if i < 0 || i >= len(rules.Items) {
			return nil, nil
		}
		return rules.Items[i].DeepCopy(), rules.Items[i].DeepCopy()
	})
}

func (c *Controller) handleAddIptablesDnatFinalizer(key string) error {
	cachedIptablesDnat, err := c.iptablesDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if cachedIptablesDnat.DeletionTimestamp.IsZero() {
		if slices.Contains(cachedIptablesDnat.Finalizers, util.KubeOVNControllerFinalizer) {
			return nil
		}
	}
	newIptablesDnat := cachedIptablesDnat.DeepCopy()
	controllerutil.AddFinalizer(newIptablesDnat, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedIptablesDnat, newIptablesDnat)
	if err != nil {
		klog.Errorf("failed to generate patch payload for iptables dnat '%s', %v", cachedIptablesDnat.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Patch(context.Background(), cachedIptablesDnat.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for iptables dnat '%s', %v", cachedIptablesDnat.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIptablesDnatFinalizer(key string) error {
	cachedIptablesDnat, err := c.iptablesDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if len(cachedIptablesDnat.Finalizers) == 0 {
		return nil
	}
	newIptablesDnat := cachedIptablesDnat.DeepCopy()
	controllerutil.RemoveFinalizer(newIptablesDnat, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedIptablesDnat, newIptablesDnat)
	if err != nil {
		klog.Errorf("failed to generate patch payload for iptables dnat '%s', %v", cachedIptablesDnat.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Patch(context.Background(), cachedIptablesDnat.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from iptables dnat '%s', %v", cachedIptablesDnat.Name, err)
		return err
	}
	return nil
}

func (c *Controller) patchFipLabel(key string, eip *kubeovnv1.IptablesEIP) error {
	oriFip, err := c.iptablesFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	fip := oriFip.DeepCopy()
	var needUpdateLabel, needUpdateAnno bool
	var op string
	if len(fip.Labels) == 0 {
		op = "add"
		fip.Labels = map[string]string{
			util.VpcNatGatewayNameLabel: eip.Spec.NatGwDp,
			util.EipV4IpLabel:           eip.Spec.V4ip,
		}
		needUpdateLabel = true
	} else if fip.Labels[util.SubnetNameLabel] != eip.Spec.NatGwDp ||
		fip.Labels[util.EipV4IpLabel] != eip.Spec.V4ip {
		op = "replace"
		fip.Labels[util.VpcNatGatewayNameLabel] = eip.Spec.NatGwDp
		fip.Labels[util.EipV4IpLabel] = eip.Spec.V4ip
		needUpdateLabel = true
	}
	if needUpdateLabel {
		if err := c.updateIptableLabels(fip.Name, op, util.FipUsingEip, fip.Labels); err != nil {
			klog.Error(err)
			return err
		}
	}

	if len(fip.Annotations) == 0 {
		op = "add"
		needUpdateAnno = true
		fip.Annotations = map[string]string{
			util.VpcEipAnnotation: eip.Name,
		}
	} else if fip.Annotations[util.VpcEipAnnotation] != eip.Name {
		op = "replace"
		needUpdateAnno = true
		fip.Annotations[util.VpcEipAnnotation] = eip.Name
	}
	if needUpdateAnno {
		if err := c.updateIptableAnnotations(fip.Name, op, util.FipUsingEip, fip.Annotations); err != nil {
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *Controller) syncIptablesSnatFinalizer(cl client.Client) error {
	rules := &kubeovnv1.IptablesSnatRuleList{}
	return updateFinalizers(cl, rules, func(i int) (client.Object, client.Object) {
		if i < 0 || i >= len(rules.Items) {
			return nil, nil
		}
		return rules.Items[i].DeepCopy(), rules.Items[i].DeepCopy()
	})
}

func (c *Controller) handleAddIptablesSnatFinalizer(key string) error {
	cachedIptablesSnat, err := c.iptablesSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if cachedIptablesSnat.DeletionTimestamp.IsZero() {
		if slices.Contains(cachedIptablesSnat.Finalizers, util.KubeOVNControllerFinalizer) {
			return nil
		}
	}
	newIptablesSnat := cachedIptablesSnat.DeepCopy()
	controllerutil.AddFinalizer(newIptablesSnat, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedIptablesSnat, newIptablesSnat)
	if err != nil {
		klog.Errorf("failed to generate patch payload for iptables snat '%s', %v", cachedIptablesSnat.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().Patch(context.Background(), cachedIptablesSnat.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for iptables snat '%s', %v", cachedIptablesSnat.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIptablesSnatFinalizer(key string) error {
	cachedIptablesSnat, err := c.iptablesSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if len(cachedIptablesSnat.Finalizers) == 0 {
		return nil
	}
	newIptablesSnat := cachedIptablesSnat.DeepCopy()
	controllerutil.RemoveFinalizer(newIptablesSnat, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedIptablesSnat, newIptablesSnat)
	if err != nil {
		klog.Errorf("failed to generate patch payload for iptables snat '%s', %v", cachedIptablesSnat.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().Patch(context.Background(), cachedIptablesSnat.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from iptables snat '%s', %v", cachedIptablesSnat.Name, err)
		return err
	}
	return nil
}

func (c *Controller) patchFipStatus(key, v4ip, v6ip, natGwDp, redo string, ready bool) error {
	oriFip, err := c.iptablesFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	fip := oriFip.DeepCopy()
	var changed bool
	if fip.Status.Ready != ready {
		fip.Status.Ready = ready
		changed = true
	}
	if redo != "" && fip.Status.Redo != redo {
		fip.Status.Redo = redo
		changed = true
	}

	if ready && v4ip != "" && fip.Status.V4ip != v4ip {
		fip.Status.V4ip = v4ip
		fip.Status.V6ip = v6ip
		fip.Status.NatGwDp = natGwDp
		changed = true
	}
	if ready && fip.Spec.InternalIP != "" && fip.Status.InternalIP != fip.Spec.InternalIP {
		fip.Status.InternalIP = fip.Spec.InternalIP
		changed = true
	}

	if changed {
		bytes, err := fip.Status.Bytes()
		if err != nil {
			klog.Error(err)
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Patch(context.Background(), fip.Name,
			types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			klog.Errorf("failed to patch fip %s, %v", fip.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) redoFip(key, redo string, eipReady bool) error {
	fip, err := c.iptablesFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get fip %s, %v", key, err)
		return err
	}
	if redo != "" && redo != fip.Status.Redo {
		if !eipReady {
			if err = c.patchEipLabel(fip.Spec.EIP); err != nil {
				err = fmt.Errorf("failed to patch eip %s, %v", fip.Spec.EIP, err)
				klog.Error(err)
				return err
			}
			if err = c.patchEipStatus(fip.Spec.EIP, "", redo, "", false); err != nil {
				err = fmt.Errorf("failed to patch eip %s, %v", fip.Spec.EIP, err)
				klog.Error(err)
				return err
			}
		}
		if err = c.patchFipStatus(key, "", "", "", redo, false); err != nil {
			err = fmt.Errorf("failed to patch fip %s, %v", fip.Name, err)
			klog.Error(err)
			return err
		}
	}
	return err
}

func (c *Controller) patchDnatLabel(key string, eip *kubeovnv1.IptablesEIP) error {
	oriDnat, err := c.iptablesDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	dnat := oriDnat.DeepCopy()
	var needUpdateLabel, needUpdateAnno bool
	var op string
	if len(dnat.Labels) == 0 {
		op = "add"
		dnat.Labels = map[string]string{
			util.VpcNatGatewayNameLabel: eip.Spec.NatGwDp,
			util.VpcDnatEPortLabel:      dnat.Spec.ExternalPort,
			util.EipV4IpLabel:           eip.Spec.V4ip,
		}
		needUpdateLabel = true
	} else if dnat.Labels[util.SubnetNameLabel] != eip.Spec.NatGwDp ||
		dnat.Labels[util.EipV4IpLabel] != eip.Spec.V4ip {
		op = "replace"
		dnat.Labels[util.VpcNatGatewayNameLabel] = eip.Spec.NatGwDp
		dnat.Labels[util.VpcDnatEPortLabel] = dnat.Spec.ExternalPort
		dnat.Labels[util.EipV4IpLabel] = eip.Spec.V4ip
		needUpdateLabel = true
	}
	if needUpdateLabel {
		if err := c.updateIptableLabels(dnat.Name, op, util.DnatUsingEip, dnat.Labels); err != nil {
			klog.Error(err)
			return err
		}
	}

	if len(dnat.Annotations) == 0 {
		op = "add"
		needUpdateAnno = true
		dnat.Annotations = map[string]string{
			util.VpcEipAnnotation: eip.Name,
		}
	} else if dnat.Annotations[util.VpcEipAnnotation] != eip.Name {
		op = "replace"
		needUpdateAnno = true
		dnat.Annotations[util.VpcEipAnnotation] = eip.Name
	}
	if needUpdateAnno {
		if err := c.updateIptableAnnotations(dnat.Name, op, util.DnatUsingEip, dnat.Annotations); err != nil {
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchDnatStatus(key, v4ip, v6ip, natGwDp, redo string, ready bool) error {
	oriDnat, err := c.iptablesDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	var changed bool
	dnat := oriDnat.DeepCopy()
	if dnat.Status.Ready != ready {
		dnat.Status.Ready = ready
		changed = true
	}
	if redo != "" && dnat.Status.Redo != redo {
		dnat.Status.Redo = redo
		changed = true
	}
	if ready && v4ip != "" && dnat.Status.V4ip != v4ip {
		dnat.Status.V4ip = v4ip
		dnat.Status.V6ip = v6ip
		dnat.Status.NatGwDp = natGwDp
		changed = true
	}
	if ready && dnat.Spec.Protocol != "" && dnat.Status.Protocol != dnat.Spec.Protocol {
		dnat.Status.Protocol = dnat.Spec.Protocol
		changed = true
	}
	if ready && dnat.Spec.InternalIP != "" && dnat.Status.InternalIP != dnat.Spec.InternalIP {
		dnat.Status.InternalIP = dnat.Spec.InternalIP
		changed = true
	}
	if ready && dnat.Spec.InternalPort != "" && dnat.Status.InternalPort != dnat.Spec.InternalPort {
		dnat.Status.InternalPort = dnat.Spec.InternalPort
		changed = true
	}
	if ready && dnat.Spec.ExternalPort != "" && dnat.Status.ExternalPort != dnat.Spec.ExternalPort {
		dnat.Status.ExternalPort = dnat.Spec.ExternalPort
		changed = true
	}

	if changed {
		bytes, err := dnat.Status.Bytes()
		if err != nil {
			klog.Error(err)
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Patch(context.Background(), dnat.Name,
			types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			klog.Errorf("failed to patch dnat %s, %v", dnat.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) redoDnat(key, redo string, eipReady bool) error {
	dnat, err := c.iptablesDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get dnat %s, %v", key, err)
		return err
	}
	if redo != "" && redo != dnat.Status.Redo {
		if !eipReady {
			if err = c.patchEipStatus(dnat.Spec.EIP, "", redo, "", false); err != nil {
				err = fmt.Errorf("failed to patch eip %s, %v", dnat.Spec.EIP, err)
				klog.Error(err)
				return err
			}
		}
		if err = c.patchDnatStatus(key, "", "", "", redo, false); err != nil {
			err = fmt.Errorf("failed to patch dnat %s, %v", key, err)
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchSnatLabel(key string, eip *kubeovnv1.IptablesEIP) error {
	oriSnat, err := c.iptablesSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	snat := oriSnat.DeepCopy()
	var needUpdateLabel, needUpdateAnno bool
	var op string
	if len(snat.Labels) == 0 {
		op = "add"
		snat.Labels = map[string]string{
			util.VpcNatGatewayNameLabel: eip.Spec.NatGwDp,
			util.EipV4IpLabel:           eip.Spec.V4ip,
		}
		needUpdateLabel = true
	} else if snat.Labels[util.SubnetNameLabel] != eip.Spec.NatGwDp ||
		snat.Labels[util.EipV4IpLabel] != eip.Spec.V4ip {
		op = "replace"
		snat.Labels[util.VpcNatGatewayNameLabel] = eip.Spec.NatGwDp
		snat.Labels[util.EipV4IpLabel] = eip.Spec.V4ip
		needUpdateLabel = true
	}
	if needUpdateLabel {
		if err := c.updateIptableLabels(snat.Name, op, util.SnatUsingEip, snat.Labels); err != nil {
			klog.Error(err)
			return err
		}
	}

	if len(snat.Annotations) == 0 {
		op = "add"
		needUpdateAnno = true
		snat.Annotations = map[string]string{
			util.VpcEipAnnotation: eip.Name,
		}
	} else if snat.Annotations[util.VpcEipAnnotation] != eip.Name {
		op = "replace"
		needUpdateAnno = true
		snat.Annotations[util.VpcEipAnnotation] = eip.Name
	}
	if needUpdateAnno {
		if err := c.updateIptableAnnotations(snat.Name, op, util.SnatUsingEip, snat.Annotations); err != nil {
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchSnatStatus(key, v4ip, v6ip, natGwDp, redo string, ready bool) error {
	oriSnat, err := c.iptablesSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	snat := oriSnat.DeepCopy()
	var changed bool
	if snat.Status.Ready != ready {
		snat.Status.Ready = ready
		changed = true
	}
	if redo != "" && snat.Status.Redo != redo {
		snat.Status.Redo = redo
		changed = true
	}
	if ready && v4ip != "" && snat.Status.V4ip != v4ip {
		snat.Status.V4ip = v4ip
		snat.Status.V6ip = v6ip
		snat.Status.NatGwDp = natGwDp
		changed = true
	}
	if ready && snat.Spec.InternalCIDR != "" {
		v4CidrSpec, _ := util.SplitStringIP(snat.Spec.InternalCIDR)
		if v4CidrSpec != "" {
			v4Cidr, _ := util.SplitStringIP(snat.Status.InternalCIDR)
			if v4Cidr != v4CidrSpec {
				snat.Status.InternalCIDR = v4CidrSpec
				changed = true
			}
		}
	}

	if changed {
		bytes, err := snat.Status.Bytes()
		if err != nil {
			klog.Error(err)
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().Patch(context.Background(), snat.Name,
			types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			klog.Errorf("failed to patch snat %s, %v", snat.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) redoSnat(key, redo string, eipReady bool) error {
	snat, err := c.iptablesSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get snat %s, %v", key, err)
		return err
	}
	if redo != "" && redo != snat.Status.Redo {
		if !eipReady {
			if err = c.patchEipStatus(snat.Spec.EIP, "", redo, "", false); err != nil {
				err = fmt.Errorf("failed to patch eip %s, %v", snat.Spec.EIP, err)
				klog.Error(err)
				return err
			}
		}
		if err = c.patchSnatStatus(key, "", "", "", redo, false); err != nil {
			err = fmt.Errorf("failed to patch snat %s, %v", key, err)
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *Controller) createFipInPod(dp, v4ip, internalIP string) error {
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		klog.Error(err)
		return err
	}
	var addRules []string
	rule := fmt.Sprintf("%s,%s", v4ip, internalIP)
	addRules = append(addRules, rule)
	if err = c.execNatGwRules(gwPod, natGwSubnetFipAdd, addRules); err != nil {
		klog.Errorf("failed to create fip, err: %v", err)
		return err
	}
	return nil
}

func (c *Controller) deleteFipInPod(dp, v4ip, internalIP string) error {
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	// del nat
	var delRules []string
	rule := fmt.Sprintf("%s,%s", v4ip, internalIP)
	delRules = append(delRules, rule)
	if err = c.execNatGwRules(gwPod, natGwSubnetFipDel, delRules); err != nil {
		klog.Errorf("failed to delete fip, err: %v", err)
		return err
	}
	return nil
}

func (c *Controller) createDnatInPod(dp, protocol, v4ip, internalIP, externalPort, internalPort string) error {
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		klog.Errorf("failed to get nat gw pod, %v", err)
		return err
	}
	var addRules []string
	rule := fmt.Sprintf("%s,%s,%s,%s,%s", v4ip, externalPort, protocol, internalIP, internalPort)
	addRules = append(addRules, rule)

	if err = c.execNatGwRules(gwPod, natGwDnatAdd, addRules); err != nil {
		klog.Errorf("failed to create dnat, err: %v", err)
		return err
	}
	return nil
}

func (c *Controller) deleteDnatInPod(dp, protocol, v4ip, internalIP, externalPort, internalPort string) error {
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	// del nat
	var delRules []string
	rule := fmt.Sprintf("%s,%s,%s,%s,%s", v4ip, externalPort, protocol, internalIP, internalPort)
	delRules = append(delRules, rule)
	if err = c.execNatGwRules(gwPod, natGwDnatDel, delRules); err != nil {
		klog.Errorf("failed to delete dnat, err: %v", err)
		return err
	}
	return nil
}

func (c *Controller) createSnatInPod(dp, v4ip, internalCIDR string) error {
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		klog.Errorf("failed to get nat gw pod, %v", err)
		return err
	}
	var rules []string
	rule := fmt.Sprintf("%s,%s", v4ip, internalCIDR)

	version, err := c.getIptablesVersion(gwPod)
	if err != nil {
		version = "1.0.0"
		klog.Warningf("failed to checking iptables version, assuming version at least %s: %v", version, err)
	}
	if util.CompareVersion(version, "1.6.2") >= 1 {
		rule = fmt.Sprintf("%s,%s", rule, "--random-fully")
	}

	rules = append(rules, rule)
	if err = c.execNatGwRules(gwPod, natGwSnatAdd, rules); err != nil {
		klog.Errorf("failed to exec nat gateway rule, err: %v", err)
		return err
	}
	return nil
}

func (c *Controller) deleteSnatInPod(dp, v4ip, internalCIDR string) error {
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	// del nat
	var delRules []string
	rule := fmt.Sprintf("%s,%s", v4ip, internalCIDR)
	delRules = append(delRules, rule)
	if err = c.execNatGwRules(gwPod, natGwSnatDel, delRules); err != nil {
		klog.Errorf("failed to delete snat, err: %v", err)
		return err
	}
	return nil
}

func (c *Controller) fipChangeEip(fip *kubeovnv1.IptablesFIPRule, eip *kubeovnv1.IptablesEIP) bool {
	if fip.Status.V4ip == "" || eip.Status.IP == "" {
		// eip created but not ready
		return false
	}
	if fip.Status.V4ip != eip.Status.IP {
		return true
	}
	return false
}

func (c *Controller) dnatChangeEip(dnat *kubeovnv1.IptablesDnatRule, eip *kubeovnv1.IptablesEIP) bool {
	if dnat.Status.V4ip == "" || eip.Status.IP == "" {
		// eip created but not ready
		return false
	}
	if dnat.Status.V4ip != eip.Status.IP {
		return true
	}
	return false
}

func (c *Controller) snatChangeEip(snat *kubeovnv1.IptablesSnatRule, eip *kubeovnv1.IptablesEIP) bool {
	if snat.Status.V4ip == "" || eip.Status.IP == "" {
		// eip created but not ready
		return false
	}
	if snat.Status.V4ip != eip.Status.IP {
		return true
	}
	return false
}

func (c *Controller) isDnatDuplicated(gwName, eipName, dnatName, externalPort string) (bool, error) {
	// check if eip:external port already used
	dnats, err := c.iptablesDnatRulesLister.List(labels.SelectorFromSet(labels.Set{
		util.VpcNatGatewayNameLabel: gwName,
		util.VpcDnatEPortLabel:      externalPort,
	}))
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return false, err
		}
	}
	if len(dnats) != 0 {
		for _, d := range dnats {
			if d.Name != dnatName && d.Spec.EIP == eipName {
				err = fmt.Errorf("failed to create dnat %s, duplicate, same eip %s, same external port '%s' is using by dnat %s", dnatName, eipName, externalPort, d.Name)
				return true, err
			}
		}
	}
	return false, nil
}

func (c *Controller) updateIptableLabels(name, op, natType string, labels map[string]string) error {
	patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
	raw, _ := json.Marshal(labels)
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)

	if err := c.patchIptableInfo(name, natType, patchPayload); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to patch label for %s %s, %v", natType, name, err)
		return err
	}
	return nil
}

func (c *Controller) updateIptableAnnotations(name, op, natType string, anno map[string]string) error {
	patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/annotations", "value": %s }]`
	raw, _ := json.Marshal(anno)
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)

	if err := c.patchIptableInfo(name, natType, patchPayload); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to patch annotations for %s %s, %v", natType, name, err)
		return err
	}
	return nil
}

func (c *Controller) patchIptableInfo(name, natType, patchPayload string) error {
	var err error
	switch natType {
	case util.FipUsingEip:
		_, err = c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Patch(context.Background(), name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
	case util.SnatUsingEip:
		_, err = c.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().Patch(context.Background(), name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
	case util.DnatUsingEip:
		_, err = c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Patch(context.Background(), name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
	case "eip":
		_, err = c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Patch(context.Background(), name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
	}
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	return nil
}
