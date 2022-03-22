package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

func (c *Controller) enqueueAddIptablesFip(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.Infof("enqueue add iptables fip [%s]", key)
	c.addIptablesFipQueue.Add(key)
}

func (c *Controller) enqueueUpdateIptablesFip(old, new interface{}) {
	if !c.isLeader() {
		return
	}

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}
	oldFip := old.(*kubeovnv1.IptablesFIPRule)
	newFip := new.(*kubeovnv1.IptablesFIPRule)
	if !newFip.DeletionTimestamp.IsZero() {
		klog.Infof("enqueue update to clean fip [%s]", key)
		c.updateIptablesFipQueue.Add(key)
		return
	}
	if oldFip.Spec.EIP != newFip.Spec.EIP {
		// to notify old eip to remove nat lable
		c.resetIptablesEipQueue.Add(oldFip.Spec.EIP)
	}
	if oldFip.Status.V4ip != newFip.Status.V4ip ||
		oldFip.Spec.EIP != newFip.Spec.EIP ||
		oldFip.Status.Redo != newFip.Status.Redo {
		klog.Infof("enqueue update fip [%s]", key)
		c.updateIptablesFipQueue.Add(key)
		return
	}
}

func (c *Controller) enqueueDelIptablesFip(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.delIptablesFipQueue.Add(key)
}

func (c *Controller) enqueueAddIptablesDnatRule(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.Infof("enqueue add iptables dnat [%s]", key)
	c.addIptablesDnatRuleQueue.Add(key)
}

func (c *Controller) enqueueUpdateIptablesDnatRule(old, new interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}
	oldDnat := old.(*kubeovnv1.IptablesDnatRule)
	newDnat := new.(*kubeovnv1.IptablesDnatRule)
	if !newDnat.DeletionTimestamp.IsZero() {
		klog.Infof("enqueue update to clean dnat [%s]", key)
		c.updateIptablesDnatRuleQueue.Add(key)
		return
	}

	if oldDnat.Spec.EIP != newDnat.Spec.EIP {
		// to notify old eip to remove nat lable
		c.resetIptablesEipQueue.Add(oldDnat.Spec.EIP)
	}

	if oldDnat.Status.V4ip != newDnat.Status.V4ip ||
		oldDnat.Spec.EIP != newDnat.Spec.EIP ||
		oldDnat.Status.Redo != newDnat.Status.Redo {
		klog.Infof("enqueue update dnat [%s]", key)
		c.updateIptablesDnatRuleQueue.Add(key)
		return
	}
}

func (c *Controller) enqueueDelIptablesDnatRule(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.delIptablesDnatRuleQueue.Add(key)
}

func (c *Controller) enqueueAddIptablesSnatRule(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.addIptablesSnatRuleQueue.Add(key)
}

func (c *Controller) enqueueUpdateIptablesSnatRule(old, new interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}
	oldSnat := old.(*kubeovnv1.IptablesSnatRule)
	newSnat := new.(*kubeovnv1.IptablesSnatRule)
	if !newSnat.DeletionTimestamp.IsZero() {
		klog.Infof("enqueue update to clean snat [%s]", key)
		c.updateIptablesSnatRuleQueue.Add(key)
		return
	}
	if oldSnat.Spec.EIP != newSnat.Spec.EIP {
		// to notify old eip to remove nat lable
		c.resetIptablesEipQueue.Add(oldSnat.Spec.EIP)
	}
	if oldSnat.Status.V4ip != newSnat.Status.V4ip ||
		oldSnat.Spec.EIP != newSnat.Spec.EIP ||
		oldSnat.Status.Redo != newSnat.Status.Redo {
		klog.Infof("enqueue update snat [%s]", key)
		c.updateIptablesSnatRuleQueue.Add(key)
		return
	}
}

func (c *Controller) enqueueDelIptablesSnatRule(obj interface{}) {
	if !c.isLeader() {
		return
	}
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
		time.Sleep(10 * time.Second)
		if vpcNatEnabled != "true" {
			return fmt.Errorf("failed to add vpc fip rule, vpcNatEnabled='%s'", vpcNatEnabled)
		}
	}
	c.vpcNatGwKeyMutex.Lock(key)
	defer c.vpcNatGwKeyMutex.Unlock(key)

	cachedFip, err := c.iptablesFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	fip := cachedFip.DeepCopy()
	klog.Infof("handle add fip [%s]", key)
	// get eip
	eipName := fip.Spec.EIP
	if len(eipName) == 0 {
		klog.Errorf("failed to create fip rule: no eip ")
	}
	eip, err := c.GetEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		time.Sleep(2 * time.Second)
		return err
	}
	if eip.Status.Nat != "" && eip.Status.Nat != "fip" {
		// eip is in use by other nat
		err = fmt.Errorf("failed to create fip [%s], eip [%s] is used by other nat [%s]", key, eipName, eip.Status.Nat)
		time.Sleep(2 * time.Second)
		return err
	}

	if eip.Status.Nat == "fip" &&
		eip.Labels[util.VpcNatLabel] != "" &&
		eip.Labels[util.VpcNatLabel] != fip.Name {
		err = fmt.Errorf("failed to create fip [%s], eip [%s] is used by other fip [%s]", key, eipName, eip.Labels[util.VpcNatLabel])
		time.Sleep(2 * time.Second)
		return err
	}

	// create fip nat
	err = c.createFipInPod(eip.Spec.NatGwDp, eip.Spec.V4ip, fip.Spec.InternalIp)
	if err != nil {
		klog.Errorf("failed to create fip, %v", err)
		time.Sleep(2 * time.Second)
		return err
	}
	if err = c.natLabelEip(eipName, fip.Name); err != nil {
		klog.Errorf("failed to label fip in eip, %v", err)
		return err
	}
	if err = c.patchFipLabel(key, eip); err != nil {
		klog.Errorf("failed to update label for fip [%s], %v", key, err)
		time.Sleep(2 * time.Second)
		return err
	}
	if err = c.patchEipStatus(eipName, "", "", "fip", true); err != nil {
		klog.Errorf("failed to update eip [%s] status, %v", key, err)
		time.Sleep(2 * time.Second)
		return err
	}
	if err = c.patchFipStatus(key, eip.Spec.V4ip, eip.Spec.V6ip, eip.Spec.NatGwDp, "", true); err != nil {
		klog.Errorf("failed to update status for fip [%s], %v", key, err)
		time.Sleep(2 * time.Second)
		return err
	}
	return nil
}

func (c *Controller) handleUpdateIptablesFip(key string) error {
	if vpcNatEnabled != "true" {
		time.Sleep(10 * time.Second)
		if vpcNatEnabled != "true" {
			return fmt.Errorf("failed to del vpc fip rule, vpcNatEnabled='%s'", vpcNatEnabled)
		}
	}
	c.vpcNatGwKeyMutex.Lock(key)
	defer c.vpcNatGwKeyMutex.Unlock(key)

	cachedFip, err := c.iptablesFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	fip := cachedFip.DeepCopy()
	// should delete
	if !fip.DeletionTimestamp.IsZero() {
		klog.Infof("clean fip in pod for [%s]", key)
		if err = c.deleteFipInPod(fip.Status.NatGwDp, fip.Status.V4ip, fip.Spec.InternalIp); err != nil {
			klog.Errorf("failed to delete fip, %v", err)
			return err
		}
		if _, err = c.handleIptablesFipFinalizer(fip, true); err != nil {
			klog.Errorf("failed to handle eip finalizer %v", err)
			return err
		}
		//  reset eip
		c.resetIptablesEipQueue.Add(fip.Spec.EIP)
		return nil
	}
	eipName := cachedFip.Spec.EIP
	if len(eipName) == 0 {
		klog.Errorf("failed to update fip rule: no eip ")
	}
	eip, err := c.GetEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		time.Sleep(2 * time.Second)
		return err
	}
	if eip.Status.Nat != "" && eip.Status.Nat != "fip" {
		// eip is in use by other nat
		err = fmt.Errorf("failed to update fip [%s], eip [%s] is used by [%s]", key, eipName, eip.Status.Nat)
		time.Sleep(2 * time.Second)
		return err
	}
	if eip.Status.Nat == "fip" &&
		eip.Labels[util.VpcNatLabel] != "" &&
		eip.Labels[util.VpcNatLabel] != fip.Name {
		err = fmt.Errorf("failed to update fip [%s], eip [%s] is used by other fip [%s]", key, eipName, eip.Labels[util.VpcNatLabel])
		time.Sleep(2 * time.Second)
		return err
	}
	// fip change eip
	if c.fipChangeEip(fip, eip) {
		klog.Infof("fip change ip, old ip [%s], new ip [%s]", fip.Status.V4ip, eip.Spec.V4ip)
		if err = c.deleteFipInPod(fip.Status.NatGwDp, fip.Status.V4ip, fip.Spec.InternalIp); err != nil {
			klog.Errorf("failed to delete old fip, %v", err)
			return err
		}
		if err = c.createFipInPod(eip.Spec.NatGwDp, eip.Spec.V4ip, fip.Spec.InternalIp); err != nil {
			klog.Errorf("failed to create new fip, %v", err)
			time.Sleep(2 * time.Second)
			return err
		}
		if err = c.natLabelEip(eipName, fip.Name); err != nil {
			klog.Errorf("failed to label fip in eip, %v", err)
			return err
		}
		if err = c.patchFipLabel(key, eip); err != nil {
			klog.Errorf("failed to update label for fip [%s], %v", key, err)
			time.Sleep(2 * time.Second)
			return err
		}
		if err = c.patchFipStatus(key, eip.Spec.V4ip, eip.Spec.V6ip, eip.Spec.NatGwDp, "", true); err != nil {
			klog.Errorf("failed to update fip [%s] status, %v", key, err)
			time.Sleep(2 * time.Second)
			return err
		}
		if err = c.patchEipStatus(eipName, "", "", "fip", true); err != nil {
			klog.Errorf("failed to update eip [%s] status, %v", key, err)
			time.Sleep(2 * time.Second)
			return err
		}
		return nil
	}
	// redo
	if fip.Status.Redo != "" &&
		fip.Status.V4ip != "" &&
		fip.DeletionTimestamp.IsZero() {
		klog.Infof("reapply fip in pod for [%s]", key)
		if err = c.createFipInPod(eip.Spec.NatGwDp, fip.Status.V4ip, fip.Spec.InternalIp); err != nil {
			klog.Errorf("failed to create fip, %v", err)
			time.Sleep(2 * time.Second)
			return err
		}
		if err = c.patchFipStatus(key, "", "", "", "", true); err != nil {
			klog.Errorf("failed to update fip [%s] status, %v", key, err)
			time.Sleep(2 * time.Second)
			return err
		}
	}
	if _, err = c.handleIptablesFipFinalizer(fip, false); err != nil {
		klog.Errorf("failed to handle eip finalizer %v", err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIptablesFip(key string) error {
	klog.Infof("deleted iptables fip [%s]", key)
	return nil
}

func (c *Controller) handleAddIptablesDnatRule(key string) error {
	if vpcNatEnabled != "true" {
		time.Sleep(10 * time.Second)
		if vpcNatEnabled != "true" {
			return fmt.Errorf("failed to add vpc dnat rule, vpcNatEnabled='%s'", vpcNatEnabled)
		}
	}
	c.vpcNatGwKeyMutex.Lock(key)
	defer c.vpcNatGwKeyMutex.Unlock(key)

	cachedDnat, err := c.iptablesDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if cachedDnat.Status.V4ip != "" {
		// already ok
		return nil
	}
	dnat := cachedDnat.DeepCopy()
	klog.Infof("handle add iptables dnat [%s]", key)
	eipName := cachedDnat.Spec.EIP
	if len(eipName) == 0 {
		klog.Errorf("failed to create dnat rule: no eip ")
	}

	eip, err := c.GetEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		time.Sleep(2 * time.Second)
		return err
	}
	if eip.Status.Nat != "" && eip.Status.Nat != "dnat" {
		// eip is in use by other nat
		err = fmt.Errorf("failed to update dnat [%s], eip [%s] is used by [%s]", key, eipName, eip.Status.Nat)
		time.Sleep(2 * time.Second)
		return err
	}
	// create nat
	err = c.createDnatInPod(eip.Spec.NatGwDp, dnat.Spec.Protocol,
		eip.Spec.V4ip, dnat.Spec.InternalIp,
		dnat.Spec.ExternalPort, dnat.Spec.InternalPort,
	)
	if err != nil {
		// not retry too often
		klog.Errorf("failed to create dnat, %v", err)
		time.Sleep(2 * time.Second)
		return err
	}
	if err = c.natLabelEip(eipName, dnat.Name); err != nil {
		klog.Errorf("failed to label dnat in eip, %v", err)
		return err
	}
	if err = c.patchDnatLabel(key, eip); err != nil {
		klog.Errorf("failed to update label for dnat [%s], %v", key, err)
		time.Sleep(2 * time.Second)
		return err
	}
	if err = c.patchEipStatus(eipName, "", "", "dnat", true); err != nil {
		klog.Errorf("failed to update eip [%s] status, %v", key, err)
		time.Sleep(2 * time.Second)
		return err
	}
	if err = c.patchDnatStatus(key, eip.Spec.V4ip, eip.Spec.V6ip, eip.Spec.NatGwDp, "", true); err != nil {
		klog.Errorf("failed to update status for dnat [%s], %v", key, err)
		time.Sleep(2 * time.Second)
		return err
	}
	return nil
}

func (c *Controller) handleUpdateIptablesDnatRule(key string) error {
	if vpcNatEnabled != "true" {
		time.Sleep(10 * time.Second)
		if vpcNatEnabled != "true" {
			return fmt.Errorf("failed to del vpc dnat rule, vpcNatEnabled='%s'", vpcNatEnabled)
		}
	}
	c.vpcNatGwKeyMutex.Lock(key)
	defer c.vpcNatGwKeyMutex.Unlock(key)

	cachedDnat, err := c.iptablesDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	dnat := cachedDnat.DeepCopy()
	// should delete
	if !dnat.DeletionTimestamp.IsZero() {
		klog.Infof("clean eip in pod for [%s]", key)
		if err = c.deleteDnatInPod(dnat.Status.NatGwDp, dnat.Spec.Protocol,
			dnat.Status.V4ip, dnat.Spec.InternalIp,
			dnat.Spec.ExternalPort, dnat.Spec.InternalPort,
		); err != nil {
			klog.Errorf("failed to delete dnat, %v", err)
			return err
		}
		if _, err = c.handleIptablesDnatRuleFinalizer(dnat, true); err != nil {
			klog.Errorf("failed to handle dnat finalizer %v", err)
			return err
		}
		//  reset eip
		c.resetIptablesEipQueue.Add(dnat.Spec.EIP)
		return nil
	}
	eipName := cachedDnat.Spec.EIP
	if len(eipName) == 0 {
		klog.Errorf("failed to update fip rule: no eip ")
	}
	eip, err := c.GetEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		time.Sleep(2 * time.Second)
		return err
	}
	if eip.Status.Nat != "" && eip.Status.Nat != "dnat" {
		// eip is in use by other nat
		err = fmt.Errorf("failed to update dnat [%s], eip [%s] is used by [%s]", key, eipName, eip.Status.Nat)
		time.Sleep(2 * time.Second)
		return err
	}
	if c.dnatChangeEip(dnat, eip) {
		klog.Infof("dnat change ip, old ip [%s], new ip [%s]", dnat.Status.V4ip, eip.Spec.V4ip)
		if err = c.deleteDnatInPod(dnat.Status.NatGwDp, dnat.Spec.Protocol,
			dnat.Status.V4ip, dnat.Spec.InternalIp,
			dnat.Spec.ExternalPort, dnat.Spec.InternalPort); err != nil {
			klog.Errorf("failed to delete old dnat, %v", err)
			return err
		}
		if err = c.createDnatInPod(eip.Spec.NatGwDp, dnat.Spec.Protocol,
			eip.Spec.V4ip, dnat.Spec.InternalIp,
			dnat.Spec.ExternalPort, dnat.Spec.InternalPort); err != nil {
			klog.Errorf("failed to create new dnat [%s], %v", key, err)
			time.Sleep(2 * time.Second)
			return err
		}
		if err = c.natLabelEip(eipName, dnat.Name); err != nil {
			klog.Errorf("failed to label dnat in eip, %v", err)
			return err
		}
		if err = c.patchDnatLabel(key, eip); err != nil {
			klog.Errorf("failed to update label for dnat [%s], %v", key, err)
			time.Sleep(2 * time.Second)
			return err
		}
		if err = c.patchDnatStatus(key, eip.Spec.V4ip, eip.Spec.V6ip, eip.Spec.NatGwDp, "", true); err != nil {
			klog.Errorf("failed to update dnat [%s] status, %v", key, err)
			time.Sleep(2 * time.Second)
			return err
		}
		if err = c.patchEipStatus(eipName, "", "", "dnat", true); err != nil {
			klog.Errorf("failed to update eip [%s] status, %v", key, err)
			time.Sleep(2 * time.Second)
			return err
		}
		return nil
	}
	// redo
	if dnat.Status.Redo != "" &&
		dnat.Status.V4ip != "" &&
		dnat.DeletionTimestamp.IsZero() {
		klog.Infof("reapply dnat in pod for [%s]", key)
		if err = c.createDnatInPod(eip.Spec.NatGwDp, dnat.Spec.Protocol,
			dnat.Status.V4ip, dnat.Spec.InternalIp,
			dnat.Spec.ExternalPort, dnat.Spec.InternalPort); err != nil {
			klog.Errorf("failed to create dnat [%s], %v", key, err)
			time.Sleep(2 * time.Second)
			return err
		}
		if err = c.patchDnatStatus(key, "", "", "", "", true); err != nil {
			klog.Errorf("failed to update dnat [%s] status, %v", key, err)
			time.Sleep(2 * time.Second)
			return err
		}
	}
	if _, err = c.handleIptablesDnatRuleFinalizer(dnat, false); err != nil {
		klog.Errorf("failed to handle dnat finalizer %v", err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIptablesDnatRule(key string) error {
	klog.Infof("deleted iptables dnat [%s]", key)
	return nil
}

func (c *Controller) handleAddIptablesSnatRule(key string) error {
	if vpcNatEnabled != "true" {
		time.Sleep(10 * time.Second)
		if vpcNatEnabled != "true" {
			return fmt.Errorf("failed to add vpc snat rule, vpcNatEnabled='%s'", vpcNatEnabled)
		}
	}
	c.vpcNatGwKeyMutex.Lock(key)
	defer c.vpcNatGwKeyMutex.Unlock(key)

	cachedSnat, err := c.iptablesSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if cachedSnat.Status.V4ip != "" {
		// already ok
		return nil
	}
	snat := cachedSnat.DeepCopy()
	klog.Infof("handle add iptables snat [%s]", key)
	eipName := cachedSnat.Spec.EIP
	if len(eipName) == 0 {
		klog.Errorf("failed to create snat rule: no eip ")
	}

	eip, err := c.GetEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		time.Sleep(2 * time.Second)
		return err
	}
	if eip.Status.Nat != "" && eip.Status.Nat != "snat" {
		// eip is in use by other nat
		err = fmt.Errorf("failed to create snat [%s], eip [%s] is used by [%s]", key, eipName, eip.Status.Nat)
		time.Sleep(2 * time.Second)
		return err
	}
	// create snat
	err = c.createSnatInPod(eip.Spec.NatGwDp, eip.Spec.V4ip, snat.Spec.InternalCIDR)
	if err != nil {
		// not retry too often
		klog.Errorf("failed to create snat, %v", err)
		time.Sleep(2 * time.Second)
		return err
	}
	if err = c.natLabelEip(eipName, snat.Name); err != nil {
		klog.Errorf("failed to label snat in eip, %v", err)
		return err
	}
	if err = c.patchSnatLabel(key, eip); err != nil {
		klog.Errorf("failed to update label for snat [%s], %v", key, err)
		time.Sleep(2 * time.Second)
		return err
	}
	if err = c.patchEipStatus(eipName, "", "", "snat", true); err != nil {
		klog.Errorf("failed to update eip [%s] status, %v", key, err)
		time.Sleep(2 * time.Second)
		return err
	}
	if err = c.patchSnatStatus(key, eip.Spec.V4ip, eip.Spec.V6ip, eip.Spec.NatGwDp, "", true); err != nil {
		klog.Errorf("failed to update status for snat [%s], %v", key, err)
		time.Sleep(2 * time.Second)
		return err
	}
	return nil
}

func (c *Controller) handleUpdateIptablesSnatRule(key string) error {
	if vpcNatEnabled != "true" {
		time.Sleep(10 * time.Second)
		if vpcNatEnabled != "true" {
			return fmt.Errorf("failed to del vpc snat rule, vpcNatEnabled='%s'", vpcNatEnabled)
		}
	}
	c.vpcNatGwKeyMutex.Lock(key)
	defer c.vpcNatGwKeyMutex.Unlock(key)

	cachedSnat, err := c.iptablesSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	snat := cachedSnat.DeepCopy()
	// should delete
	if !snat.DeletionTimestamp.IsZero() {
		klog.Infof("clean eip in pod for [%s]", key)
		if err = c.deleteSnatInPod(snat.Status.NatGwDp, snat.Status.V4ip, snat.Spec.InternalCIDR); err != nil {
			klog.Errorf("failed to delete snat, %v", err)
			return err
		}
		if _, err = c.handleIptablesSnatRuleFinalizer(snat, true); err != nil {
			klog.Errorf("failed to handle snat finalizer %v", err)
			return err
		}
		//  reset eip
		c.resetIptablesEipQueue.Add(snat.Spec.EIP)
		return nil
	}
	eipName := cachedSnat.Spec.EIP
	if len(eipName) == 0 {
		klog.Errorf("failed to update fip rule: no eip ")
	}
	eip, err := c.GetEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		time.Sleep(2 * time.Second)
		return err
	}
	if eip.Status.Nat != "" && eip.Status.Nat != "snat" {
		// eip is in use by other nat
		err = fmt.Errorf("failed to update snat [%s], eip [%s] is used by [%s]", key, eipName, eip.Status.Nat)
		time.Sleep(2 * time.Second)
		return err
	}
	// snat change eip
	if c.snatChangeEip(snat, eip) {
		klog.Infof("snat change ip, old ip [%s], new ip [%s]", snat.Status.V4ip, eip.Spec.V4ip)
		if err = c.deleteSnatInPod(snat.Status.NatGwDp, snat.Status.V4ip, snat.Spec.InternalCIDR); err != nil {
			klog.Errorf("failed to delete old snat, %v", err)
			return err
		}
		if err = c.createSnatInPod(snat.Status.NatGwDp, eip.Spec.V4ip, snat.Spec.InternalCIDR); err != nil {
			klog.Errorf("failed to create new snat, %v", err)
			time.Sleep(2 * time.Second)
			return err
		}
		if err = c.natLabelEip(eipName, snat.Name); err != nil {
			klog.Errorf("failed to label snat in eip, %v", err)
			return err
		}
		if err = c.patchSnatLabel(key, eip); err != nil {
			klog.Errorf("failed to update label for snat [%s], %v", key, err)
			time.Sleep(2 * time.Second)
			return err
		}
		if err = c.patchSnatStatus(key, eip.Spec.V4ip, eip.Spec.V6ip, eip.Spec.NatGwDp, "", true); err != nil {
			klog.Errorf("failed to update snat [%s] status, %v", key, err)
			time.Sleep(2 * time.Second)
			return err
		}
		if err = c.patchEipStatus(eipName, "", "", "snat", true); err != nil {
			klog.Errorf("failed to update eip [%s] status, %v", key, err)
			time.Sleep(2 * time.Second)
			return err
		}
		return nil
	}
	// redo
	if snat.Status.Redo != "" &&
		snat.Status.V4ip != "" &&
		snat.DeletionTimestamp.IsZero() {
		if err = c.createSnatInPod(snat.Status.NatGwDp, snat.Status.V4ip, snat.Spec.InternalCIDR); err != nil {
			klog.Errorf("failed to create new snat, %v", err)
			time.Sleep(2 * time.Second)
			return err
		}
		if err = c.patchSnatStatus(key, "", "", "", "", true); err != nil {
			klog.Errorf("failed to update snat [%s] status, %v", key, err)
			time.Sleep(2 * time.Second)
			return err
		}
	}
	if _, err = c.handleIptablesSnatRuleFinalizer(snat, false); err != nil {
		klog.Errorf("failed to handle snat finalizer %v", err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIptablesSnatRule(key string) error {
	klog.Infof("deleted iptables snat [%s]", key)
	return nil
}

func (c *Controller) handleIptablesFipFinalizer(fip *kubeovnv1.IptablesFIPRule, justDelete bool) (bool, error) {
	if !fip.DeletionTimestamp.IsZero() && justDelete {
		if len(fip.Finalizers) == 0 {
			return true, nil
		}
		fip.Finalizers = util.RemoveString(fip.Finalizers, util.ControllerName)
		raw, _ := json.Marshal(fip.Finalizers)
		patchPayloadTemplate := `[{ "op": "remove", "path": "/metadata/finalizers", "value": %s }]`
		patchPayload := fmt.Sprintf(patchPayloadTemplate, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Patch(context.Background(), fip.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to remove finalizer from fip %s, %v", fip.Name, err)
			time.Sleep(2 * time.Second)
			return false, err
		}
		return true, nil
	}

	if fip.DeletionTimestamp.IsZero() && !util.ContainsString(fip.Finalizers, util.ControllerName) {
		if len(fip.Finalizers) != 0 {
			return false, nil
		}
		fip.Finalizers = append(fip.Finalizers, util.ControllerName)
		raw, _ := json.Marshal(fip.Finalizers)
		patchPayloadTemplate := `[{ "op": "add", "path": "/metadata/finalizers", "value": %s }]`
		patchPayload := fmt.Sprintf(patchPayloadTemplate, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Patch(context.Background(), fip.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to add finalizer to fip %s, %v", fip.Name, err)
			time.Sleep(2 * time.Second)
			return false, err
		}
		// wait local cache ready
		time.Sleep(2 * time.Second)
		return false, nil
	}

	if !fip.DeletionTimestamp.IsZero() && !fip.Status.Ready {
		if len(fip.Finalizers) == 0 {
			return true, nil
		}
		fip.Finalizers = util.RemoveString(fip.Finalizers, util.ControllerName)
		raw, _ := json.Marshal(fip.Finalizers)
		patchPayloadTemplate := `[{ "op": "remove", "path": "/metadata/finalizers", "value": %s }]`
		patchPayload := fmt.Sprintf(patchPayloadTemplate, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Patch(context.Background(), fip.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to remove finalizer from fip %s, %v", fip.Name, err)
			time.Sleep(2 * time.Second)
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (c *Controller) handleIptablesDnatRuleFinalizer(dnat *kubeovnv1.IptablesDnatRule, justDelete bool) (bool, error) {
	if !dnat.DeletionTimestamp.IsZero() && justDelete {
		if len(dnat.Finalizers) == 0 {
			return true, nil
		}
		dnat.Finalizers = util.RemoveString(dnat.Finalizers, util.ControllerName)
		raw, _ := json.Marshal(dnat.Finalizers)
		patchPayloadTemplate := `[{ "op": "remove", "path": "/metadata/finalizers", "value": %s }]`
		patchPayload := fmt.Sprintf(patchPayloadTemplate, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Patch(context.Background(), dnat.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to remove finalizer from dnat %s, %v", dnat.Name, err)
			time.Sleep(2 * time.Second)
			return false, err
		}
		return true, nil
	}

	if dnat.DeletionTimestamp.IsZero() && !util.ContainsString(dnat.Finalizers, util.ControllerName) {
		if len(dnat.Finalizers) != 0 {
			return false, nil
		}
		dnat.Finalizers = append(dnat.Finalizers, util.ControllerName)
		raw, _ := json.Marshal(dnat.Finalizers)
		patchPayloadTemplate := `[{ "op": "add", "path": "/metadata/finalizers", "value": %s }]`
		patchPayload := fmt.Sprintf(patchPayloadTemplate, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Patch(context.Background(), dnat.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to add finalizer to dnat %s, %v", dnat.Name, err)
			time.Sleep(2 * time.Second)
			return false, err
		}
		time.Sleep(2 * time.Second)
		// wait local cache ready
		return false, nil
	}

	if !dnat.DeletionTimestamp.IsZero() && !dnat.Status.Ready {
		if len(dnat.Finalizers) == 0 {
			return true, nil
		}
		dnat.Finalizers = util.RemoveString(dnat.Finalizers, util.ControllerName)
		raw, _ := json.Marshal(dnat.Finalizers)
		patchPayloadTemplate := `[{ "op": "remove", "path": "/metadata/finalizers", "value": %s }]`
		patchPayload := fmt.Sprintf(patchPayloadTemplate, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Patch(context.Background(), dnat.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to remove finalizer from dnat %s, %v", dnat.Name, err)
			time.Sleep(2 * time.Second)
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (c *Controller) patchFipLabel(key string, eip *kubeovnv1.IptablesEIP) error {
	oriFip, err := c.iptablesFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	fip := oriFip.DeepCopy()
	var needUpdateLabel bool
	var op string
	if len(fip.Labels) == 0 {
		op = "add"
		fip.Labels = map[string]string{
			util.VpcNatGatewayNameLabel: eip.Spec.NatGwDp,
			util.VpcEipLabel:            eip.Name,
		}
		needUpdateLabel = true
	} else if fip.Labels[util.SubnetNameLabel] != eip.Spec.NatGwDp {
		op = "replace"
		fip.Labels[util.VpcNatGatewayNameLabel] = eip.Spec.NatGwDp
		fip.Labels[util.VpcEipLabel] = eip.Name
		needUpdateLabel = true
	}
	if fip.Labels[util.VpcEipLabel] != eip.Name {
		op = "replace"
		fip.Labels[util.VpcEipLabel] = eip.Name
		needUpdateLabel = true
	}
	if needUpdateLabel {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
		raw, _ := json.Marshal(fip.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		_, err := c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Patch(context.Background(), fip.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
		if err != nil {
			klog.Errorf("failed to patch fip label %s: %v", fip.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) handleIptablesSnatRuleFinalizer(snat *kubeovnv1.IptablesSnatRule, justDelete bool) (bool, error) {
	if !snat.DeletionTimestamp.IsZero() && justDelete {
		if len(snat.Finalizers) == 0 {
			return true, nil
		}
		snat.Finalizers = util.RemoveString(snat.Finalizers, util.ControllerName)
		raw, _ := json.Marshal(snat.Finalizers)
		patchPayloadTemplate := `[{ "op": "remove", "path": "/metadata/finalizers", "value": %s }]`
		patchPayload := fmt.Sprintf(patchPayloadTemplate, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().Patch(context.Background(), snat.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to add finalizer to snat %s, %v", snat.Name, err)
			time.Sleep(2 * time.Second)
			return false, err
		}
		return true, nil
	}

	if snat.DeletionTimestamp.IsZero() && !util.ContainsString(snat.Finalizers, util.ControllerName) {
		if len(snat.Finalizers) != 0 {
			return false, nil
		}
		snat.Finalizers = append(snat.Finalizers, util.ControllerName)
		raw, _ := json.Marshal(snat.Finalizers)
		patchPayloadTemplate := `[{ "op": "add", "path": "/metadata/finalizers", "value": %s }]`
		patchPayload := fmt.Sprintf(patchPayloadTemplate, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().Patch(context.Background(), snat.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to add finalizer to snat %s, %v", snat.Name, err)
			time.Sleep(2 * time.Second)
			return false, err
		}
		// wait local cache ready
		time.Sleep(2 * time.Second)
		return false, nil
	}

	if !snat.DeletionTimestamp.IsZero() && !snat.Status.Ready {
		if len(snat.Finalizers) == 0 {
			return true, nil
		}
		snat.Finalizers = util.RemoveString(snat.Finalizers, util.ControllerName)
		raw, _ := json.Marshal(snat.Finalizers)
		patchPayloadTemplate := `[{ "op": "remove", "path": "/metadata/finalizers", "value": %s }]`
		patchPayload := fmt.Sprintf(patchPayloadTemplate, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().Patch(context.Background(), snat.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to add finalizer to snat %s, %v", snat.Name, err)
			time.Sleep(2 * time.Second)
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (c *Controller) patchFipStatus(key, v4ip, v6ip, natGwDp, redo string, ready bool) error {
	oriFip, err := c.iptablesFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
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

	bytes, err := fip.Status.Bytes()
	if err != nil {
		return err
	}
	if changed {
		_, err = c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Patch(context.Background(), fip.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
		return err
	}
	return nil
}

func (c *Controller) redoFip(key, redo string, eipReady bool) error {
	oriFip, err := c.iptablesFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	fip := oriFip.DeepCopy()
	if redo != "" && redo != fip.Status.Redo {
		if !eipReady {
			if err = c.patchEipStatus(fip.Spec.EIP, "", redo, "", false); err != nil {
				return err
			}
		}
		if err = c.patchFipStatus(key, "", "", "", redo, false); err != nil {
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
		return err
	}
	dnat := oriDnat.DeepCopy()
	var needUpdateLabel bool
	var op string
	if len(dnat.Labels) == 0 {
		op = "add"
		dnat.Labels = map[string]string{
			util.VpcNatGatewayNameLabel: eip.Spec.NatGwDp,
			util.VpcEipLabel:            eip.Name,
		}
		needUpdateLabel = true
	} else if dnat.Labels[util.SubnetNameLabel] != eip.Spec.NatGwDp {
		op = "replace"
		dnat.Labels[util.VpcNatGatewayNameLabel] = eip.Spec.NatGwDp
		dnat.Labels[util.VpcEipLabel] = eip.Name
		needUpdateLabel = true
	}

	if dnat.Labels[util.VpcEipLabel] != eip.Name {
		op = "replace"
		dnat.Labels[util.VpcEipLabel] = eip.Name
		needUpdateLabel = true
	}
	if needUpdateLabel {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
		raw, _ := json.Marshal(dnat.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		_, err := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Patch(context.Background(), dnat.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
		if err != nil {
			klog.Errorf("failed to patch dnat label %s: %v", dnat.Name, err)
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

	bytes, err := dnat.Status.Bytes()
	if err != nil {
		return err
	}
	if changed {
		_, err = c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Patch(context.Background(), dnat.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
		return err
	}
	return nil
}

func (c *Controller) redoDnat(key, redo string, eipReady bool) error {
	oriDnat, err := c.iptablesDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	dnat := oriDnat.DeepCopy()
	if redo != "" && redo != dnat.Status.Redo {
		if !eipReady {
			if err = c.patchEipStatus(dnat.Spec.EIP, "", redo, "", false); err != nil {
				return err
			}
		}
		if err = c.patchDnatStatus(key, "", "", "", redo, false); err != nil {
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
		return err
	}
	snat := oriSnat.DeepCopy()
	var needUpdateLabel bool
	var op string
	if len(snat.Labels) == 0 {
		op = "add"
		snat.Labels = map[string]string{
			util.VpcNatGatewayNameLabel: eip.Spec.NatGwDp,
			util.VpcEipLabel:            eip.Name,
		}
		needUpdateLabel = true
	} else if snat.Labels[util.SubnetNameLabel] != eip.Spec.NatGwDp {
		op = "replace"
		snat.Labels[util.VpcNatGatewayNameLabel] = eip.Spec.NatGwDp
		snat.Labels[util.VpcEipLabel] = eip.Name
		needUpdateLabel = true
	}

	if snat.Labels[util.VpcEipLabel] != eip.Name {
		op = "replace"
		snat.Labels[util.VpcEipLabel] = eip.Name
		needUpdateLabel = true
	}

	if needUpdateLabel {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
		raw, _ := json.Marshal(snat.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		_, err := c.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().Patch(context.Background(), snat.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
		if err != nil {
			klog.Errorf("failed to patch snat label %s: %v", snat.Name, err)
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

	bytes, err := snat.Status.Bytes()
	if err != nil {
		return err
	}
	if changed {
		_, err = c.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().Patch(context.Background(), snat.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
		return err
	}
	return nil
}

func (c *Controller) redoSnat(key, redo string, eipReady bool) error {
	oriSnat, err := c.iptablesSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	snat := oriSnat.DeepCopy()
	if redo != "" && redo != snat.Status.Redo {
		if !eipReady {
			if err = c.patchEipStatus(snat.Spec.EIP, "", redo, "", false); err != nil {
				return err
			}
		}
		if err = c.patchSnatStatus(key, "", "", "", redo, false); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) createFipInPod(dp, v4ip, internalIP string) error {
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	var addRules []string
	rule := fmt.Sprintf("%s,%s", v4ip, internalIP)
	addRules = append(addRules, rule)
	if err = c.execNatGwRules(gwPod, NAT_GW_FLOATING_IP_ADD, addRules); err != nil {
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
		return err
	}
	// del nat
	var delRules []string
	rule := fmt.Sprintf("%s,%s", v4ip, internalIP)
	delRules = append(delRules, rule)
	if err = c.execNatGwRules(gwPod, NAT_GW_FLOATING_IP_DEL, delRules); err != nil {
		return err
	}
	return nil
}

func (c *Controller) createDnatInPod(dp, protocol, v4ip, internalIp, externalPort, internalPort string) error {
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		klog.Errorf("failed to get nat gw pod, %v", err)
		return err
	}
	var addRules []string
	rule := fmt.Sprintf("%s,%s,%s,%s,%s", v4ip, externalPort, protocol, internalIp, internalPort)
	addRules = append(addRules, rule)

	if err = c.execNatGwRules(gwPod, NAT_GW_DNAT_ADD, addRules); err != nil {
		return err
	}
	return nil
}

func (c *Controller) deleteDnatInPod(dp, protocol, v4ip, internalIp, externalPort, internalPort string) error {
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		klog.Errorf("failed to get nat gw pod, %v", err)
		return err
	}
	// del nat
	var delRules []string
	rule := fmt.Sprintf("%s,%s,%s,%s,%s", v4ip, externalPort, protocol, internalIp, internalPort)
	delRules = append(delRules, rule)
	if err = c.execNatGwRules(gwPod, NAT_GW_DNAT_DEL, delRules); err != nil {
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
	rules = append(rules, rule)
	if err = c.execNatGwRules(gwPod, NAT_GW_SNAT_ADD, rules); err != nil {
		klog.Errorf("failed to exec nat gateway rule, err: %v", err)
		return err
	}
	return nil
}

func (c *Controller) deleteSnatInPod(dp, v4ip, internalCIDR string) error {
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		klog.Errorf("failed to get nat gw pod, %v", err)
		return err
	}
	// del nat
	var delRules []string
	rule := fmt.Sprintf("%s,%s", v4ip, internalCIDR)
	delRules = append(delRules, rule)
	if err = c.execNatGwRules(gwPod, NAT_GW_SNAT_DEL, delRules); err != nil {
		return err
	}
	return nil
}

func (c *Controller) fipChangeEip(fip *kubeovnv1.IptablesFIPRule, eip *kubeovnv1.IptablesEIP) bool {
	if fip.Status.V4ip == "" || eip.Spec.V4ip == "" {
		// eip created but not ready
		return false
	}
	if fip.Status.V4ip != eip.Spec.V4ip {
		return true
	}
	return false
}

func (c *Controller) dnatChangeEip(dnat *kubeovnv1.IptablesDnatRule, eip *kubeovnv1.IptablesEIP) bool {
	if dnat.Status.V4ip == "" || eip.Spec.V4ip == "" {
		// eip created but not ready
		return false
	}
	if dnat.Status.V4ip != eip.Spec.V4ip {
		return true
	}
	return false
}

func (c *Controller) snatChangeEip(snat *kubeovnv1.IptablesSnatRule, eip *kubeovnv1.IptablesEIP) bool {
	if snat.Status.V4ip == "" || eip.Spec.V4ip == "" {
		// eip created but not ready
		return false
	}
	if snat.Status.V4ip != eip.Spec.V4ip {
		return true
	}
	return false
}
