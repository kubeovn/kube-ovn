package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
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

func (c *Controller) enqueueUpdateOvnSnatRule(oldObj, newObj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	newSnat := newObj.(*kubeovnv1.OvnSnatRule)
	if !newSnat.DeletionTimestamp.IsZero() {
		if len(newSnat.Finalizers) == 0 {
			// avoid delete twice
			return
		}
		klog.Infof("enqueue del ovn snat %s", key)
		c.delOvnSnatRuleQueue.Add(key)
		return
	}
	oldSnat := oldObj.(*kubeovnv1.OvnSnatRule)
	if oldSnat.Spec.OvnEip != newSnat.Spec.OvnEip {
		// enqueue to reset eip to be clean
		c.resetOvnEipQueue.Add(oldSnat.Spec.OvnEip)
	}
	if oldSnat.Spec.OvnEip != newSnat.Spec.OvnEip ||
		oldSnat.Spec.VpcSubnet != newSnat.Spec.VpcSubnet ||
		oldSnat.Spec.IPName != newSnat.Spec.IPName {
		klog.Infof("enqueue update snat %s", key)
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
	klog.Infof("enqueue del ovn snat %s", key)
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
		klog.Error(err)
		return err
	}
	if cachedSnat.Status.Ready && cachedSnat.Status.V4IpCidr != "" {
		// already ok
		return nil
	}
	klog.Infof("handle add ovn snat %s", key)
	// check eip
	eipName := cachedSnat.Spec.OvnEip
	if eipName == "" {
		err := fmt.Errorf("failed to create ovn snat rule, should set eip")
		klog.Error(err)
		return err
	}
	cachedEip, err := c.GetOvnEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}
	if cachedEip.Spec.Type == util.OvnEipTypeLSP {
		// eip is using by ecmp nexthop lsp, nat can not use
		err = fmt.Errorf("ovn nat %s can not use type %s eip %s", key, util.OvnEipTypeLSP, eipName)
		klog.Error(err)
		return err
	}
	if cachedEip.Status.V4Ip == "" {
		err := fmt.Errorf("failed to create v4 snat %s, eip %s has no v4 ip", cachedSnat.Name, eipName)
		klog.Error(err)
		return err
	}
	var v4IpCidr, vpcName string
	if cachedSnat.Spec.Vpc != "" {
		vpcName = cachedSnat.Spec.Vpc
	}
	if cachedSnat.Spec.V4IpCidr != "" {
		v4IpCidr = cachedSnat.Spec.V4IpCidr
	}
	if v4IpCidr == "" && cachedSnat.Spec.VpcSubnet != "" {
		subnet, err := c.subnetsLister.Get(cachedSnat.Spec.VpcSubnet)
		if err != nil {
			klog.Errorf("failed to get vpc subnet %s, %v", cachedSnat.Spec.VpcSubnet, err)
			return err
		}
		vpcName = subnet.Spec.Vpc
		v4IpCidr = subnet.Spec.CIDRBlock
	}
	if v4IpCidr == "" && cachedSnat.Spec.IPName != "" {
		vpcPodIP, err := c.ipsLister.Get(cachedSnat.Spec.IPName)
		if err != nil {
			klog.Errorf("failed to get pod ip %s, %v", cachedSnat.Spec.IPName, err)
			return err
		}
		subnet, err := c.subnetsLister.Get(vpcPodIP.Spec.Subnet)
		if err != nil {
			klog.Errorf("failed to get vpc subnet %s, %v", vpcPodIP.Spec.Subnet, err)
			return err
		}
		vpcName = subnet.Spec.Vpc
		v4IpCidr = vpcPodIP.Spec.V4IPAddress
	}
	if v4IpCidr == "" {
		// only support IPv4 snat
		err = fmt.Errorf("failed to get v4 internal ip for snat %s", key)
		klog.Error(err)
		return err
	}
	if vpcName == "" {
		err := fmt.Errorf("failed to get vpc for snat %s", cachedSnat.Name)
		klog.Error(err)
		return err
	}

	// about conflicts: if multi vpc snat use the same eip, if only one gw node exist, it may should work
	if err = c.OVNNbClient.AddNat(vpcName, ovnnb.NATTypeSNAT, cachedEip.Spec.V4Ip, v4IpCidr, "", "", nil); err != nil {
		klog.Errorf("failed to create snat, %v", err)
		return err
	}
	if err := c.handleAddOvnSnatFinalizer(cachedSnat, util.KubeOVNControllerFinalizer); err != nil {
		klog.Errorf("failed to add finalizer for ovn snat %s, %v", cachedSnat.Name, err)
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
	if err = c.patchOvnEipStatus(eipName, true); err != nil {
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
		klog.Error(err)
		return err
	}
	// should delete
	if !cachedSnat.DeletionTimestamp.IsZero() {
		klog.Infof("ovn delete snat %s", key)
		if cachedSnat.Status.Vpc != "" && cachedSnat.Status.V4Eip != "" && cachedSnat.Status.V4IpCidr != "" {
			if err = c.OVNNbClient.DeleteNat(cachedSnat.Status.Vpc, ovnnb.NATTypeSNAT, cachedSnat.Status.V4Eip, cachedSnat.Status.V4IpCidr); err != nil {
				klog.Errorf("failed to delete snat, %v", err)
				return err
			}
		}
		c.resetOvnEipQueue.Add(cachedSnat.Spec.OvnEip)
		return nil
	}
	klog.Infof("handle update ovn snat %s", key)
	// check eip
	eipName := cachedSnat.Spec.OvnEip
	if eipName == "" {
		err := fmt.Errorf("failed to create ovn snat rule, should set eip")
		klog.Error(err)
		return err
	}
	cachedEip, err := c.GetOvnEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}
	if cachedEip.Spec.Type == util.OvnEipTypeLSP {
		// eip is using by ecmp nexthop lsp, nat can not use
		err = fmt.Errorf("ovn nat %s can not use type %s eip %s", key, util.OvnEipTypeLSP, eipName)
		klog.Error(err)
		return err
	}
	if cachedEip.Status.V4Ip == "" {
		err := fmt.Errorf("failed to create v4 snat %s, eip %s has no v4 ip", cachedSnat.Name, eipName)
		klog.Error(err)
		return err
	}
	var v4IpCidr, vpcName string
	if cachedSnat.Spec.Vpc != "" {
		vpcName = cachedSnat.Spec.Vpc
	}
	if cachedSnat.Spec.V4IpCidr != "" {
		v4IpCidr = cachedSnat.Spec.V4IpCidr
	}
	if v4IpCidr == "" && cachedSnat.Spec.VpcSubnet != "" {
		subnet, err := c.subnetsLister.Get(cachedSnat.Spec.VpcSubnet)
		if err != nil {
			klog.Errorf("failed to get vpc subnet %s, %v", cachedSnat.Spec.VpcSubnet, err)
			return err
		}
		vpcName = subnet.Spec.Vpc
		v4IpCidr = subnet.Spec.CIDRBlock
	}
	if v4IpCidr == "" && cachedSnat.Spec.IPName != "" {
		vpcPodIP, err := c.ipsLister.Get(cachedSnat.Spec.IPName)
		if err != nil {
			klog.Errorf("failed to get pod ip %s, %v", cachedSnat.Spec.IPName, err)
			return err
		}
		subnet, err := c.subnetsLister.Get(vpcPodIP.Spec.Subnet)
		if err != nil {
			klog.Errorf("failed to get vpc subnet %s, %v", vpcPodIP.Spec.Subnet, err)
			return err
		}
		vpcName = subnet.Spec.Vpc
		v4IpCidr = vpcPodIP.Spec.V4IPAddress
	}
	if v4IpCidr == "" {
		// only support IPv4 snat
		err = fmt.Errorf("failed to get v4 internal ip for snat %s", key)
		klog.Error(err)
		return err
	}
	if vpcName == "" {
		err := fmt.Errorf("failed to get vpc for snat %s", cachedSnat.Name)
		klog.Error(err)
		return err
	}
	if cachedEip.Spec.Type == util.OvnEipTypeLSP {
		// eip is using by ecmp nexthop lsp, nat can not use
		err = fmt.Errorf("ovn nat %s can not use type %s eip %s", key, util.OvnEipTypeLSP, eipName)
		klog.Error(err)
		return err
	}
	// snat change eip
	if c.ovnSnatChangeEip(cachedSnat, cachedEip) {
		klog.Infof("snat change ip, old ip %s, new ip %s", cachedEip.Status.V4Ip, cachedEip.Spec.V4Ip)
		if err = c.OVNNbClient.DeleteNat(vpcName, ovnnb.NATTypeSNAT, cachedEip.Status.V4Ip, v4IpCidr); err != nil {
			klog.Errorf("failed to delte snat, %v", err)
			return err
		}
		// ovn add snat with new eip
		if err = c.OVNNbClient.AddNat(vpcName, ovnnb.NATTypeSNAT, cachedEip.Spec.V4Ip, v4IpCidr, "", "", nil); err != nil {
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
	klog.Infof("handle delete ovn snat %s", key)
	cachedSnat, err := c.ovnSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	// ovn delete snat
	if cachedSnat.Status.Vpc != "" && cachedSnat.Status.V4Eip != "" && cachedSnat.Status.V4IpCidr != "" {
		if err = c.OVNNbClient.DeleteNat(cachedSnat.Status.Vpc, ovnnb.NATTypeSNAT,
			cachedSnat.Status.V4Eip, cachedSnat.Status.V4IpCidr); err != nil {
			klog.Errorf("failed to delete snat %s, %v", key, err)
			return err
		}
	}
	if err = c.handleDelOvnSnatFinalizer(cachedSnat, util.KubeOVNControllerFinalizer); err != nil {
		klog.Errorf("failed to remove finalizer for ovn snat %s, %v", cachedSnat.Name, err)
		return err
	}
	if cachedSnat.Spec.OvnEip != "" {
		c.resetOvnEipQueue.Add(cachedSnat.Spec.OvnEip)
	}
	return nil
}

func (c *Controller) patchOvnSnatStatus(key, vpc, v4Eip, v4IpCidr string, ready bool) error {
	oriSnat, err := c.ovnSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	snat := oriSnat.DeepCopy()
	needUpdateLabel := false
	var op string
	if len(snat.Labels) == 0 {
		op = "add"
		needUpdateLabel = true
		snat.Labels = map[string]string{
			util.EipV4IpLabel: v4Eip,
		}
	} else if snat.Labels[util.EipV4IpLabel] != v4Eip {
		op = "replace"
		needUpdateLabel = true
		snat.Labels[util.EipV4IpLabel] = v4Eip
	}
	if needUpdateLabel {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
		raw, _ := json.Marshal(snat.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().OvnSnatRules().Patch(context.Background(), snat.Name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to patch label for ovn snat %s, %v", snat.Name, err)
			return err
		}
	}
	var changed bool
	if snat.Status.Ready != ready {
		snat.Status.Ready = ready
		changed = true
	}
	if vpc != "" && snat.Status.Vpc != vpc {
		snat.Status.Vpc = vpc
		changed = true
	}
	if v4Eip != "" && snat.Status.V4Eip != v4Eip {
		snat.Status.V4Eip = v4Eip
		changed = true
	}
	if v4IpCidr != "" && snat.Status.V4IpCidr != v4IpCidr {
		snat.Status.V4IpCidr = v4IpCidr
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
		klog.Error(err)
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

func (c *Controller) syncOvnSnatFinalizer(cl client.Client) error {
	// migrate depreciated finalizer to new finalizer
	rules := &kubeovnv1.OvnSnatRuleList{}
	return updateFinalizers(cl, rules, func(i int) (client.Object, client.Object) {
		if i < 0 || i >= len(rules.Items) {
			return nil, nil
		}
		return rules.Items[i].DeepCopy(), rules.Items[i].DeepCopy()
	})
}

func (c *Controller) handleAddOvnSnatFinalizer(cachedSnat *kubeovnv1.OvnSnatRule, finalizer string) error {
	if cachedSnat.DeletionTimestamp.IsZero() {
		if slices.Contains(cachedSnat.Finalizers, finalizer) {
			return nil
		}
	}
	newSnat := cachedSnat.DeepCopy()
	controllerutil.AddFinalizer(newSnat, finalizer)
	patch, err := util.GenerateMergePatchPayload(cachedSnat, newSnat)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ovn snat '%s', %v", cachedSnat.Name, err)
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

func (c *Controller) handleDelOvnSnatFinalizer(cachedSnat *kubeovnv1.OvnSnatRule, finalizer string) error {
	if len(cachedSnat.Finalizers) == 0 {
		return nil
	}
	var err error
	newSnat := cachedSnat.DeepCopy()
	controllerutil.RemoveFinalizer(newSnat, finalizer)
	patch, err := util.GenerateMergePatchPayload(cachedSnat, newSnat)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ovn snat '%s', %v", cachedSnat.Name, err)
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
