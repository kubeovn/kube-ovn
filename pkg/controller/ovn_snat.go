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

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.addOvnSnatRuleQueue.Add(key)
}

func (c *Controller) enqueueUpdateOvnSnatRule(old, new interface{}) {
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
	if cachedSnat.Status.Ready && cachedSnat.Status.V4IpCidr != "" {
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
	if cachedEip.Status.Type != "" && cachedEip.Status.Type != util.SnatUsingEip {
		err = fmt.Errorf("failed to create snat %s, eip '%s' is using by '%s'", key, eipName, cachedEip.Spec.Type)
		return err
	}
	var v4IpCidr, vpcName string
	if cachedSnat.Spec.VpcSubnet != "" {
		subnet, err := c.subnetsLister.Get(cachedSnat.Spec.VpcSubnet)
		if err != nil {
			klog.Errorf("failed to get vpc subnet %s, %v", cachedSnat.Spec.VpcSubnet, err)
			return err
		}
		vpcName = subnet.Spec.Vpc
		v4IpCidr = subnet.Spec.CIDRBlock
	}
	if cachedSnat.Spec.IpName != "" {
		vpcPodIp, err := c.ipsLister.Get(cachedSnat.Spec.IpName)
		if err != nil {
			klog.Errorf("failed to get pod ip %s, %v", cachedSnat.Spec.IpName, err)
			return err
		}
		subnet, err := c.subnetsLister.Get(vpcPodIp.Spec.Subnet)
		if err != nil {
			klog.Errorf("failed to get vpc subnet %s, %v", vpcPodIp.Spec.Subnet, err)
			return err
		}
		vpcName = subnet.Spec.Vpc
		v4IpCidr = vpcPodIp.Spec.V4IPAddress
	}

	if v4IpCidr == "" {
		// only support IPv4 snat
		err = fmt.Errorf("failed to get v4 internal ip for snat %s", key)
		return err
	}

	if err = c.patchOvnSnatStatus(key, vpcName, cachedEip.Spec.V4Ip, v4IpCidr, false); err != nil {
		klog.Errorf("failed to update status for snat %s, %v", key, err)
		return err
	}

	// create snat
	if err = c.handleAddOvnSnatRuleFinalizer(cachedSnat); err != nil {
		klog.Errorf("failed to add finalizer for ovn snat, %v", err)
		return err
	}
	if err = c.handleAddOvnEipFinalizer(cachedEip, util.OvnSnatUseEipFinalizer); err != nil {
		klog.Errorf("failed to add finalizer for ovn eip, %v", err)
		return err
	}
	// ovn add snat
	if err = c.ovnLegacyClient.AddSnatRule(vpcName, cachedEip.Spec.V4Ip, v4IpCidr); err != nil {
		klog.Errorf("failed to create snat, %v", err)
		return err
	}
	if err = c.natLabelAndAnnoOvnEip(eipName, cachedSnat.Name, vpcName); err != nil {
		klog.Errorf("failed to label snat '%s' in eip %s, %v", cachedSnat.Name, eipName, err)
		return err
	}
	if err = c.patchOvnSnatAnnotation(key, eipName); err != nil {
		klog.Errorf("failed to patch label for snat %s, %v", key, err)
		return err
	}
	if err = c.patchOvnSnatStatus(key, vpcName, cachedEip.Spec.V4Ip, v4IpCidr, true); err != nil {
		klog.Errorf("failed to update status for snat %s, %v", key, err)
		return err
	}
	if err = c.patchOvnEipNat(eipName, util.SnatUsingEip); err != nil {
		klog.Errorf("failed to patch status for eip %s, %v", key, err)
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
	klog.V(3).Infof("handle update ovn snat %s", key)
	eipName := cachedSnat.Spec.OvnEip
	if len(eipName) == 0 {
		klog.Errorf("failed to create snat rule, should set eip")
	}
	cachedEip, err := c.GetOvnEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}
	// should delete
	if !cachedSnat.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("ovn clean snat %s", key)
		// ovn delete snat
		if cachedSnat.Status.Vpc != "" && cachedSnat.Status.V4Eip != "" && cachedSnat.Status.V4IpCidr != "" {
			if err = c.ovnLegacyClient.DeleteSnatRule(cachedSnat.Status.Vpc, cachedSnat.Status.V4Eip, cachedSnat.Status.V4IpCidr); err != nil {
				klog.Errorf("failed to delete snat, %v", err)
				return err
			}
		}
		if err = c.handleDelOvnSnatRuleFinalizer(cachedSnat); err != nil {
			klog.Errorf("failed to handle finalizer for snat %s, %v", key, err)
			return err
		}
		//  reset eip
		c.resetOvnEipQueue.Add(cachedSnat.Spec.OvnEip)
		if err = c.handleDelOvnEipFinalizer(cachedEip, util.OvnSnatUseEipFinalizer); err != nil {
			klog.Errorf("failed to handle finalizer for eip %s, %v", key, err)
			return err
		}
		return nil
	}
	if cachedEip.Spec.Type != "" && cachedEip.Spec.Type != util.SnatUsingEip {
		// eip is in use by other
		err = fmt.Errorf("failed to create snat %s, eip '%s' is using by nat '%s'", key, eipName, cachedEip.Spec.Type)
		return err
	}
	var v4IpCidr, vpcName string
	if cachedSnat.Spec.VpcSubnet != "" {
		subnet, err := c.subnetsLister.Get(cachedSnat.Spec.VpcSubnet)
		if err != nil {
			klog.Errorf("failed to get vpc subnet %s, %v", cachedSnat.Spec.VpcSubnet, err)
			return err
		}
		vpcName = subnet.Spec.Vpc
		v4IpCidr = subnet.Spec.CIDRBlock
	}
	if cachedSnat.Spec.IpName != "" {
		vpcPodIp, err := c.ipsLister.Get(cachedSnat.Spec.IpName)
		if err != nil {
			klog.Errorf("failed to get pod ip %s, %v", cachedSnat.Spec.IpName, err)
			return err
		}
		subnet, err := c.subnetsLister.Get(vpcPodIp.Spec.Subnet)
		if err != nil {
			klog.Errorf("failed to get vpc subnet %s, %v", vpcPodIp.Spec.Subnet, err)
			return err
		}
		vpcName = subnet.Spec.Vpc
		v4IpCidr = vpcPodIp.Spec.V4IPAddress
	}

	if v4IpCidr == "" {
		// only support IPv4 snat
		err = fmt.Errorf("failed to get v4 internal ip for snat %s", key)
		return err
	}
	// snat change eip
	if c.ovnSnatChangeEip(cachedSnat, cachedEip) {
		klog.V(3).Infof("snat change ip, old ip %s, new ip %s", cachedEip.Status.V4Ip, cachedEip.Spec.V4Ip)
		if err = c.ovnLegacyClient.DeleteSnatRule(vpcName, cachedEip.Status.V4Ip, v4IpCidr); err != nil {
			klog.Errorf("failed to delte snat, %v", err)
			return err
		}
		// ovn add snat with new eip
		if err = c.ovnLegacyClient.AddSnatRule(vpcName, cachedEip.Spec.V4Ip, v4IpCidr); err != nil {
			klog.Errorf("failed to create snat, %v", err)
			return err
		}
		if err = c.natLabelAndAnnoOvnEip(eipName, cachedSnat.Name, vpcName); err != nil {
			klog.Errorf("failed to label snat '%s' in eip %s, %v", cachedSnat.Name, eipName, err)
			return err
		}
		if err = c.patchOvnSnatAnnotation(key, eipName); err != nil {
			klog.Errorf("failed to patch label for snat %s, %v", key, err)
			return err
		}
		if err = c.patchOvnSnatStatus(key, vpcName, cachedEip.Spec.V4Ip, v4IpCidr, true); err != nil {
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
		klog.Errorf("failed to generate patch for snat %s, %v", cachedSnat.Name, err)
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
		klog.Errorf("failed to generate patch for snat %s, %v", cachedSnat.Name, err)
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

func (c *Controller) patchOvnSnatStatus(key, vpc, v4Eip, v4IpCidr string, ready bool) error {
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
	if (v4Eip != "" && snat.Status.V4Eip != v4Eip) ||
		(v4IpCidr != "" && snat.Status.V4IpCidr != v4IpCidr) ||
		(vpc != "" && snat.Status.Vpc != vpc) {
		snat.Status.V4Eip = v4Eip
		snat.Status.V4IpCidr = v4IpCidr
		snat.Status.Vpc = vpc
		changed = true
	}
	if changed {
		bytes, err := snat.Status.Bytes()
		if err != nil {
			klog.Errorf("failed to marshal snat status, %v", err)
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

func (c *Controller) patchOvnSnatAnnotation(key, eipName string) error {
	oriFip, err := c.ovnSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	snat := oriFip.DeepCopy()
	var needUpdateAnno bool
	var op string
	if len(snat.Annotations) == 0 {
		op = "add"
		snat.Annotations = map[string]string{
			util.VpcEipAnnotation: eipName,
		}
		needUpdateAnno = true
	}
	if snat.Annotations[util.VpcEipAnnotation] != eipName {
		op = "replace"
		snat.Annotations[util.VpcEipAnnotation] = eipName
		needUpdateAnno = true
	}
	if needUpdateAnno {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/annotations", "value": %s }]`
		raw, _ := json.Marshal(snat.Annotations)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		_, err := c.config.KubeOvnClient.KubeovnV1().OvnSnatRules().Patch(context.Background(), snat.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
		if err != nil {
			klog.Errorf("failed to patch annotation for ovn snat %s, %v", snat.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) ovnSnatChangeEip(snat *kubeovnv1.OvnSnatRule, eip *kubeovnv1.OvnEip) bool {
	if snat.Status.V4Eip == "" || eip.Spec.V4Ip == "" {
		// eip created but not ready
		return false
	}
	if snat.Status.V4Eip != eip.Spec.V4Ip {
		return true
	}
	return false
}
