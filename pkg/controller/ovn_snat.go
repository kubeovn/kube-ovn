package controller

import (
	"context"
	"encoding/json"
	"fmt"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (c *Controller) enqueueAddOvnSnatRule(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.addOvnSnatRuleQueue.Add(key)
}

func (c *Controller) enqueueUpdateOvnSnatRule(old, new interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}
	oldSnat := old.(*kubeovnv1.OvnSnatRule)
	newSnat := new.(*kubeovnv1.OvnSnatRule)
	if !newSnat.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("enqueue reset old ovn eip %s", oldSnat.Spec.OvnEip)
		c.updateOvnSnatRuleQueue.Add(key)
		return
	}
	if oldSnat.Spec.OvnEip != newSnat.Spec.OvnEip {
		// enqueue to reset eip to be clean
		c.resetOvnEipQueue.Add(oldSnat.Spec.OvnEip)
	}
	if oldSnat.Spec.OvnEip != newSnat.Spec.OvnEip {
		klog.V(3).Infof("enqueue update snat %s", key)
		c.updateOvnSnatRuleQueue.Add(key)
		return
	}
}

func (c *Controller) enqueueDelOvnSnatRule(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue del ovn snat %s", key)
	c.delOvnSnatRuleQueue.Add(key)
}

func (c *Controller) runAddOvnSnatRuleWorker() {
	for c.processNextAddOvnSnatRuleWorkItem() {
	}
}

func (c *Controller) runUpdateOvnSnatRuleWorker() {
	for c.processNextUpdateOvnSnatRuleWorkItem() {
	}
}

func (c *Controller) runDelOvnSnatRuleWorker() {
	for c.processNextDeleteOvnSnatRuleWorkItem() {
	}
}

func (c *Controller) processNextAddOvnSnatRuleWorkItem() bool {
	obj, shutdown := c.addOvnSnatRuleQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addOvnSnatRuleQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addOvnSnatRuleQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddOvnSnatRule(key); err != nil {
			c.addOvnSnatRuleQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addOvnSnatRuleQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateOvnSnatRuleWorkItem() bool {
	obj, shutdown := c.updateOvnSnatRuleQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateOvnSnatRuleQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateOvnSnatRuleQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateOvnSnatRule(key); err != nil {
			c.updateOvnSnatRuleQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateOvnSnatRuleQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteOvnSnatRuleWorkItem() bool {
	obj, shutdown := c.delOvnSnatRuleQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.delOvnSnatRuleQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.delOvnSnatRuleQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected snat in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDelOvnSnatRule(key); err != nil {
			c.delOvnSnatRuleQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.delOvnSnatRuleQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleAddOvnSnatRule(key string) error {
	cachedSnat, err := c.ovnSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if cachedSnat.Status.Ready && cachedSnat.Status.V4ip != "" {
		// already ok
		return nil
	}
	klog.V(3).Infof("handle add ovn snat %s", key)
	eipName := cachedSnat.Spec.OvnEip
	if len(eipName) == 0 {
		klog.Errorf("failed to create snat rule, should set eip")
	}
	cachedEip, err := c.GetOvnEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}
	if cachedEip.Spec.Type != "" && cachedEip.Spec.Type != util.SnatUsingEip {
		err = fmt.Errorf("failed to create snat %s, eip '%s' is using by '%s'", key, eipName, cachedEip.Spec.Type)
		return err
	}
	v4Cidr, _ := util.SplitStringIP(cachedSnat.Spec.InternalCIDR)
	if v4Cidr == "" {
		// only support IPv4 snat
		err = fmt.Errorf("failed to get snat v4 internal cidr, original cidr is %s", cachedSnat.Spec.InternalCIDR)
		return err
	}
	vpcName, err := c.getVpcBySubnet(cachedEip.Spec.ExternalSubnet)
	if err != nil {
		klog.Errorf("failed to get vpc, %v", err)
		return err
	}
	// create snat
	if err = c.handleAddOvnSnatRuleFinalizer(cachedSnat); err != nil {
		klog.Errorf("failed to add finalizer for ovn snat, %v", err)
		return err
	}
	if err = c.handleAddOvnEipFinalizer(cachedEip); err != nil {
		klog.Errorf("failed to add finalizer for ovn eip, %v", err)
		return err
	}
	// ovn add snat
	if err = c.ovnLegacyClient.AddSnatRule(vpcName, cachedEip.Spec.V4ip, cachedSnat.Spec.InternalCIDR); err != nil {
		klog.Errorf("failed to create snat, %v", err)
		return err
	}
	if err = c.natLabelOvnEip(eipName, cachedSnat.Name, vpcName); err != nil {
		klog.Errorf("failed to label snat '%s' in eip %s, %v", cachedSnat.Name, eipName, err)
		return err
	}
	if err = c.patchOvnSnatLabel(key, eipName); err != nil {
		klog.Errorf("failed to patch label for snat %s, %v", key, err)
		return err
	}
	if err = c.patchOvnEipStatus(eipName); err != nil {
		klog.Errorf("failed to patch status for eip %s, %v", key, err)
		return err
	}
	if err = c.patchOvnSnatStatus(key, cachedEip.Spec.V4ip, cachedEip.Spec.V6ip, true); err != nil {
		klog.Errorf("failed to update status for snat %s, %v", key, err)
		return err
	}
	return nil
}

func (c *Controller) handleUpdateOvnSnatRule(key string) error {
	cachedSnat, err := c.ovnSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	klog.V(3).Infof("handle add ovn snat %s", key)
	eipName := cachedSnat.Spec.OvnEip
	if len(eipName) == 0 {
		klog.Errorf("failed to create snat rule, should set eip")
	}
	cachedEip, err := c.GetOvnEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}
	if cachedEip.Spec.Type != "" && cachedEip.Spec.Type != util.SnatUsingEip {
		// eip is in use by other
		err = fmt.Errorf("failed to create snat %s, eip '%s' is using by nat '%s'", key, eipName, cachedEip.Spec.Type)
		return err
	}
	v4Cidr, _ := util.SplitStringIP(cachedSnat.Spec.InternalCIDR)
	if v4Cidr == "" {
		// only support IPv4 snat
		err = fmt.Errorf("failed to get snat v4 internal cidr, original cidr is %s", cachedSnat.Spec.InternalCIDR)
		return err
	}
	vpcName, err := c.getVpcBySubnet(cachedEip.Spec.ExternalSubnet)
	if err != nil {
		klog.Errorf("failed to get vpc, %v", err)
		return err
	}
	// should delete
	if !cachedSnat.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("ovn clean snat %s", key)
		// ovn delete snat
		if err = c.ovnLegacyClient.DeleteSnatRule(vpcName, cachedEip.Spec.V4ip, cachedSnat.Spec.InternalCIDR); err != nil {
			klog.Errorf("failed to delte snat, %v", err)
			return err
		}
		//  reset eip
		c.resetOvnEipQueue.Add(cachedSnat.Spec.OvnEip)
		if err = c.handleDelOvnSnatRuleFinalizer(cachedSnat); err != nil {
			klog.Errorf("failed to handle finalizer for snat %s, %v", key, err)
			return err
		}
		if err = c.handleDelOvnEipFinalizer(cachedEip); err != nil {
			klog.Errorf("failed to handle finalizer for eip %s, %v", key, err)
			return err
		}
		return nil
	}
	// snat change eip
	if c.ovnSnatChangeEip(cachedSnat, cachedEip) {
		klog.V(3).Infof("snat change ip, old ip %s, new ip %s", cachedEip.Status.V4ip, cachedEip.Spec.V4ip)
		if err = c.ovnLegacyClient.DeleteSnatRule(vpcName, cachedEip.Spec.V4ip, cachedSnat.Spec.InternalCIDR); err != nil {
			klog.Errorf("failed to delte snat, %v", err)
			return err
		}
		// ovn add snat
		if err = c.ovnLegacyClient.AddSnatRule(vpcName, cachedEip.Spec.V4ip, cachedSnat.Spec.InternalCIDR); err != nil {
			klog.Errorf("failed to create snat, %v", err)
			return err
		}
		if err = c.natLabelOvnEip(eipName, cachedSnat.Name, vpcName); err != nil {
			klog.Errorf("failed to label snat '%s' in eip %s, %v", cachedSnat.Name, eipName, err)
			return err
		}
		if err = c.patchOvnSnatLabel(key, eipName); err != nil {
			klog.Errorf("failed to patch label for snat %s, %v", key, err)
			return err
		}
		if err = c.patchOvnEipStatus(eipName); err != nil {
			klog.Errorf("failed to patch status for eip %s, %v", key, err)
			return err
		}
		if err = c.patchOvnSnatStatus(key, cachedEip.Spec.V4ip, cachedEip.Spec.V6ip, true); err != nil {
			klog.Errorf("failed to update status for snat %s, %v", key, err)
			return err
		}
		return nil
	}
	return nil
}

func (c *Controller) handleDelOvnSnatRule(key string) error {
	klog.V(3).Infof("deleted ovn snat %s", key)
	return nil
}

func (c *Controller) handleAddOvnSnatRuleFinalizer(cachedSnat *kubeovnv1.OvnSnatRule) error {
	if cachedSnat.DeletionTimestamp.IsZero() {
		if util.ContainsString(cachedSnat.Finalizers, util.ControllerName) {
			return nil
		}
	}
	newSnat := cachedSnat.DeepCopy()
	controllerutil.AddFinalizer(newSnat, util.ControllerName)
	patch, err := util.GenerateMergePatchPayload(cachedSnat, newSnat)
	if err != nil {
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().OvnSnatRules().Patch(context.Background(), cachedSnat.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for ovn snat '%s', %v", cachedSnat.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelOvnSnatRuleFinalizer(cachedSnat *kubeovnv1.OvnSnatRule) error {
	if len(cachedSnat.Finalizers) == 0 {
		return nil
	}
	newSnat := cachedSnat.DeepCopy()
	controllerutil.RemoveFinalizer(newSnat, util.ControllerName)
	patch, err := util.GenerateMergePatchPayload(cachedSnat, newSnat)
	if err != nil {
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().OvnSnatRules().Patch(context.Background(), cachedSnat.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from ovn snat '%s', %v", cachedSnat.Name, err)
		return err
	}
	return nil
}

func (c *Controller) patchOvnSnatStatus(key, v4ip, v6ip string, ready bool) error {
	oriSnat, err := c.ovnSnatRulesLister.Get(key)
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
	if ready && v4ip != "" && snat.Status.V4ip != v4ip {
		snat.Status.V4ip = v4ip
		snat.Status.V6ip = v6ip
		changed = true
	}
	if changed {
		bytes, err := snat.Status.Bytes()
		if err != nil {
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().OvnSnatRules().Patch(context.Background(), snat.Name,
			types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
			klog.Errorf("failed to patch snat %s, %v", snat.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchOvnSnatLabel(key, eipName string) error {
	oriFip, err := c.ovnSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	snat := oriFip.DeepCopy()
	var needUpdateLabel bool
	var op string
	if len(snat.Labels) == 0 {
		op = "add"
		snat.Labels = map[string]string{
			util.VpcEipLabel: eipName,
		}
		needUpdateLabel = true
	}
	if snat.Labels[util.VpcEipLabel] != eipName {
		op = "replace"
		snat.Labels[util.VpcEipLabel] = eipName
		needUpdateLabel = true
	}
	if needUpdateLabel {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
		raw, _ := json.Marshal(snat.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		_, err := c.config.KubeOvnClient.KubeovnV1().OvnSnatRules().Patch(context.Background(), snat.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
		if err != nil {
			klog.Errorf("failed to patch label for ovn snat %s, %v", snat.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) ovnSnatChangeEip(snat *kubeovnv1.OvnSnatRule, eip *kubeovnv1.OvnEip) bool {
	if snat.Status.V4ip == "" || eip.Spec.V4ip == "" {
		// eip created but not ready
		return false
	}
	if snat.Status.V4ip != eip.Spec.V4ip {
		return true
	}
	return false
}
