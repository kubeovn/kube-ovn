package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

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
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddOvnFip(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.Infof("enqueue add ovn fip %s", key)
	c.addOvnFipQueue.Add(key)
}

func (c *Controller) enqueueUpdateOvnFip(oldObj, newObj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	newFip := newObj.(*kubeovnv1.OvnFip)
	if !newFip.DeletionTimestamp.IsZero() {
		if len(newFip.Finalizers) == 0 {
			// avoid delete twice
			return
		}
		klog.Infof("enqueue del ovn fip %s", key)
		c.delOvnFipQueue.Add(key)
		return
	}
	oldFip := oldObj.(*kubeovnv1.OvnFip)
	if oldFip.Spec.OvnEip != newFip.Spec.OvnEip {
		// enqueue to reset eip to be clean
		klog.Infof("enqueue reset old ovn eip %s", oldFip.Spec.OvnEip)
		c.resetOvnEipQueue.Add(oldFip.Spec.OvnEip)
	}
	if oldFip.Spec.IPName != newFip.Spec.IPName ||
		oldFip.Spec.IPType != newFip.Spec.IPType {
		klog.Infof("enqueue update fip %s", key)
		c.updateOvnFipQueue.Add(key)
		return
	}
}

func (c *Controller) enqueueDelOvnFip(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.Infof("enqueue del ovn fip %s", key)
	c.delOvnFipQueue.Add(key)
}

func (c *Controller) runAddOvnFipWorker() {
	for c.processNextAddOvnFipWorkItem() {
	}
}

func (c *Controller) runUpdateOvnFipWorker() {
	for c.processNextUpdateOvnFipWorkItem() {
	}
}

func (c *Controller) runDelOvnFipWorker() {
	for c.processNextDeleteOvnFipWorkItem() {
	}
}

func (c *Controller) processNextAddOvnFipWorkItem() bool {
	obj, shutdown := c.addOvnFipQueue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.addOvnFipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addOvnFipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddOvnFip(key); err != nil {
			c.addOvnFipQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addOvnFipQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateOvnFipWorkItem() bool {
	obj, shutdown := c.updateOvnFipQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateOvnFipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateOvnFipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateOvnFip(key); err != nil {
			c.updateOvnFipQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateOvnFipQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteOvnFipWorkItem() bool {
	obj, shutdown := c.delOvnFipQueue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.delOvnFipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.delOvnFipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected fip in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDelOvnFip(key); err != nil {
			c.delOvnFipQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.delOvnFipQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) isOvnFipDuplicated(fipName, eipV4IP string) error {
	// check if has another fip using this eip already
	selector := labels.SelectorFromSet(labels.Set{util.EipV4IpLabel: eipV4IP})
	usingFips, err := c.ovnFipsLister.List(selector)
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

func (c *Controller) handleAddOvnFip(key string) error {
	cachedFip, err := c.ovnFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if cachedFip.Status.Ready && cachedFip.Status.V4Ip != "" {
		// already ok
		return nil
	}
	klog.Infof("handle add fip %s", key)
	// check eip
	eipName := cachedFip.Spec.OvnEip
	if eipName == "" {
		err := fmt.Errorf("failed to create fip rule, should set eip")
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
	var v4Eip, v6Eip, v4IP, v6IP string
	v4Eip = cachedEip.Status.V4Ip
	v6Eip = cachedEip.Status.V6Ip
	if err = c.isOvnFipDuplicated(key, cachedEip.Spec.V4Ip); err != nil {
		err = fmt.Errorf("failed to add fip %s, %v", key, err)
		klog.Error(err)
		return err
	}

	var mac, subnetName, vpcName, ipName string
	vpcName = cachedFip.Spec.Vpc
	v4IP = cachedFip.Spec.V4Ip
	v6IP = cachedFip.Spec.V6Ip
	ipName = cachedFip.Spec.IPName
	if ipName != "" {
		if cachedFip.Spec.IPType == util.Vip {
			internalVip, err := c.virtualIpsLister.Get(ipName)
			if err != nil {
				klog.Errorf("failed to get vip %s, %v", ipName, err)
				return err
			}
			v4IP = internalVip.Status.V4ip
			v6IP = internalVip.Status.V6ip
			subnetName = internalVip.Spec.Subnet
			// though vip lsp has its mac, vip always use its parent lsp nic mac
			// and vip could float to different parent lsp nic
			// all vip its parent lsp acl should allow the vip ip
		} else {
			internalIP, err := c.ipsLister.Get(ipName)
			if err != nil {
				klog.Errorf("failed to get ip %s, %v", ipName, err)
				return err
			}
			v4IP = internalIP.Spec.V4IPAddress
			v6IP = internalIP.Spec.V6IPAddress
			subnetName = internalIP.Spec.Subnet
			mac = internalIP.Spec.MacAddress
			// mac is necessary while using distributed router fip, fip use lsp its mac
			// centralized router fip not need lsp mac, fip use lrp mac
		}
		subnet, err := c.subnetsLister.Get(subnetName)
		if err != nil {
			klog.Errorf("failed to get vpc subnet %s, %v", subnetName, err)
			return err
		}
		vpcName = subnet.Spec.Vpc
		if err = c.isOvnFipDuplicated(key, cachedEip.Spec.V4Ip); err != nil {
			err = fmt.Errorf("failed to update fip %s, %v", key, err)
			klog.Error(err)
			return err
		}
	}
	if vpcName == "" {
		err := fmt.Errorf("failed to create fip %s, no vpc", cachedFip.Name)
		klog.Error(err)
		return err
	}
	if v4IP == "" && v6IP == "" {
		err := fmt.Errorf("failed to create fip %s, no internal ip", cachedFip.Name)
		klog.Error(err)
		return err
	}
	if v4Eip == "" && v6Eip == "" {
		err := fmt.Errorf("failed to create fip %s, no external ip", cachedFip.Name)
		klog.Error(err)
		return err
	}
	// ovn add fip
	options := map[string]string{"staleless": strconv.FormatBool(c.ExternalGatewayType == kubeovnv1.GWDistributedType)}
	if v4IP != "" && v4Eip != "" {
		if err = c.OVNNbClient.AddNat(vpcName, ovnnb.NATTypeDNATAndSNAT, v4Eip, v4IP, mac, cachedFip.Spec.IPName, options); err != nil {
			klog.Errorf("failed to create v4 fip, %v", err)
			return err
		}
	}
	if v6Eip == "" && v6IP != "" {
		if err = c.OVNNbClient.AddNat(vpcName, ovnnb.NATTypeDNATAndSNAT, v6Eip, v6IP, mac, cachedFip.Spec.IPName, options); err != nil {
			klog.Errorf("failed to create v6 fip, %v", err)
			return err
		}
	}

	if err = c.handleAddOvnFipFinalizer(cachedFip, util.KubeOVNControllerFinalizer); err != nil {
		klog.Errorf("failed to add finalizer for ovn fip, %v", err)
		return err
	}

	// patch fip eip relationship
	if err = c.natLabelAndAnnoOvnEip(eipName, cachedFip.Name, vpcName); err != nil {
		klog.Errorf("failed to label fip '%s' in eip %s, %v", cachedFip.Name, eipName, err)
		return err
	}
	if err = c.patchOvnFipAnnotations(key, eipName); err != nil {
		klog.Errorf("failed to update label for fip %s, %v", key, err)
		return err
	}
	if err = c.patchOvnFipStatus(key, vpcName, v4Eip, v4IP, true); err != nil {
		klog.Errorf("failed to patch status for fip %s, %v", key, err)
		return err
	}
	if err = c.patchOvnEipStatus(eipName, true); err != nil {
		klog.Errorf("failed to patch status for eip %s, %v", key, err)
		return err
	}
	return nil
}

func (c *Controller) handleUpdateOvnFip(key string) error {
	cachedFip, err := c.ovnFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if !cachedFip.Status.Ready {
		// create fip only in add process, just check to error out here
		klog.Infof("wait ovn fip %s to be ready only in the handle add process", cachedFip.Name)
		return nil
	}
	klog.Infof("handle update fip %s", key)
	// check eip
	eipName := cachedFip.Spec.OvnEip
	if eipName == "" {
		err := fmt.Errorf("failed to create fip rule, should set eip")
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
	var v4Eip, v6Eip, v4IP, v6IP string
	v4Eip = cachedEip.Status.V4Ip
	v6Eip = cachedEip.Status.V6Ip
	if err = c.isOvnFipDuplicated(key, cachedEip.Spec.V4Ip); err != nil {
		err = fmt.Errorf("failed to add fip %s, %v", key, err)
		klog.Error(err)
		return err
	}

	var subnetName, vpcName, ipName string
	vpcName = cachedFip.Spec.Vpc
	v4IP = cachedFip.Spec.V4Ip
	v6IP = cachedFip.Spec.V6Ip
	ipName = cachedFip.Spec.IPName
	if ipName != "" {
		if cachedFip.Spec.IPType == util.Vip {
			internalVip, err := c.virtualIpsLister.Get(ipName)
			if err != nil {
				klog.Errorf("failed to get vip %s, %v", ipName, err)
				return err
			}
			v4IP = internalVip.Status.V4ip
			v6IP = internalVip.Status.V6ip
			subnetName = internalVip.Spec.Subnet
			// vip lsp has its mac, but vip always use its parent lsp nic mac
			// vip could float to different parent lsp nic
			// all vip its parent lsp acl should allow the vip ip
		} else {
			internalIP, err := c.ipsLister.Get(ipName)
			if err != nil {
				klog.Errorf("failed to get ip %s, %v", ipName, err)
				return err
			}
			v4IP = internalIP.Spec.V4IPAddress
			v6IP = internalIP.Spec.V6IPAddress
			subnetName = internalIP.Spec.Subnet
		}
		subnet, err := c.subnetsLister.Get(subnetName)
		if err != nil {
			klog.Errorf("failed to get vpc subnet %s, %v", subnetName, err)
			return err
		}
		vpcName = subnet.Spec.Vpc
		if err = c.isOvnFipDuplicated(key, cachedEip.Spec.V4Ip); err != nil {
			err = fmt.Errorf("failed to update fip %s, %v", key, err)
			klog.Error(err)
			return err
		}
	}
	if vpcName == "" {
		err := fmt.Errorf("failed to update ovn fip %s, no vpc", cachedFip.Name)
		klog.Error(err)
		return err
	}
	if vpcName != cachedFip.Status.Vpc {
		err := fmt.Errorf("not support change vpc for ovn fip %s", cachedEip.Name)
		klog.Error(err)
		return err
	}
	if v4IP != cachedFip.Status.V4Ip {
		err := fmt.Errorf("not support change v4 ip for ovn fip %s", cachedEip.Name)
		klog.Error(err)
		return err
	}
	if v6IP != cachedFip.Status.V6Ip {
		err := fmt.Errorf("not support change v6 ip for ovn fip %s", cachedEip.Name)
		klog.Error(err)
		return err
	}
	if v4Eip != cachedFip.Status.V4Eip {
		err := fmt.Errorf("not support change eip for ovn fip %s", cachedEip.Name)
		klog.Error(err)
		return err
	}
	if v6Eip != cachedFip.Status.V6Eip {
		err := fmt.Errorf("not support change eip for ovn fip %s", cachedEip.Name)
		klog.Error(err)
		return err
	}
	return nil
}

func (c *Controller) handleDelOvnFip(key string) error {
	klog.Infof("handle del ovn fip %s", key)
	cachedFip, err := c.ovnFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if cachedFip.Status.Vpc == "" {
		return nil
	}
	// ovn delete fip nat
	if cachedFip.Status.V4Eip != "" && cachedFip.Status.V4Ip != "" {
		if err = c.OVNNbClient.DeleteNat(cachedFip.Status.Vpc, ovnnb.NATTypeDNATAndSNAT, cachedFip.Status.V4Eip, cachedFip.Status.V4Ip); err != nil {
			klog.Errorf("failed to delete v4 fip %s, %v", key, err)
			return err
		}
	}
	if cachedFip.Status.V6Eip != "" && cachedFip.Status.V6Ip != "" {
		if err = c.OVNNbClient.DeleteNat(cachedFip.Status.Vpc, ovnnb.NATTypeDNATAndSNAT, cachedFip.Status.V6Eip, cachedFip.Status.V6Ip); err != nil {
			klog.Errorf("failed to delete v6 fip %s, %v", key, err)
			return err
		}
	}
	if err = c.handleDelOvnFipFinalizer(cachedFip, util.KubeOVNControllerFinalizer); err != nil {
		klog.Errorf("failed to remove finalizer for ovn fip %s, %v", cachedFip.Name, err)
		return err
	}
	//  reset eip
	if cachedFip.Spec.OvnEip != "" {
		c.resetOvnEipQueue.Add(cachedFip.Spec.OvnEip)
	}
	return nil
}

func (c *Controller) patchOvnFipAnnotations(key, eipName string) error {
	oriFip, err := c.ovnFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	fip := oriFip.DeepCopy()
	var needUpdateAnno bool
	var op string
	if len(fip.Annotations) == 0 {
		op = "add"
		fip.Annotations = map[string]string{
			util.VpcEipAnnotation: eipName,
		}
		needUpdateAnno = true
	}
	if fip.Annotations[util.VpcEipAnnotation] != eipName {
		op = "replace"
		fip.Annotations[util.VpcEipAnnotation] = eipName
		needUpdateAnno = true
	}
	if needUpdateAnno {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/annotations", "value": %s }]`
		raw, _ := json.Marshal(fip.Annotations)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().OvnFips().Patch(context.Background(), fip.Name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to patch annotation for ovn fip %s, %v", fip.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchOvnFipStatus(key, vpcName, v4Eip, podIP string, ready bool) error {
	oriFip, err := c.ovnFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	fip := oriFip.DeepCopy()
	needUpdateLabel := false
	var op string
	if len(fip.Labels) == 0 {
		op = "add"
		needUpdateLabel = true
		fip.Labels = map[string]string{
			util.EipV4IpLabel: v4Eip,
		}
	} else if fip.Labels[util.EipV4IpLabel] != v4Eip {
		op = "replace"
		needUpdateLabel = true
		fip.Labels[util.EipV4IpLabel] = v4Eip
	}
	if needUpdateLabel {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
		raw, _ := json.Marshal(fip.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().OvnFips().Patch(context.Background(), fip.Name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to patch label for ovn fip %s, %v", fip.Name, err)
			return err
		}
	}
	var changed bool
	if fip.Status.Ready != ready {
		fip.Status.Ready = ready
		changed = true
	}
	if vpcName != "" && fip.Status.Vpc != vpcName {
		fip.Status.Vpc = vpcName
		changed = true
	}
	if v4Eip != "" && fip.Status.V4Eip != v4Eip {
		fip.Status.V4Eip = v4Eip
		changed = true
	}
	if podIP != "" && fip.Status.V4Ip != podIP {
		fip.Status.V4Ip = podIP
		changed = true
	}
	if changed {
		bytes, err := fip.Status.Bytes()
		if err != nil {
			klog.Error(err)
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().OvnFips().Patch(context.Background(), fip.Name,
			types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
			klog.Errorf("failed to patch fip %s, %v", fip.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) GetOvnEip(eipName string) (*kubeovnv1.OvnEip, error) {
	cachedEip, err := c.ovnEipsLister.Get(eipName)
	if err != nil {
		klog.Errorf("failed to get eip %s, %v", eipName, err)
		return nil, err
	}
	if cachedEip.Status.V4Ip == "" && cachedEip.Status.V6Ip == "" {
		err := fmt.Errorf("eip '%s' is not ready, has no ip", eipName)
		klog.Error(err)
		return nil, err
	}
	return cachedEip, nil
}

func (c *Controller) syncOvnFipFinalizer(cl client.Client) error {
	// migrate depreciated finalizer to new finalizer
	fips := &kubeovnv1.OvnFipList{}
	return updateFinalizers(cl, fips, func(i int) (client.Object, client.Object) {
		if i < 0 || i >= len(fips.Items) {
			return nil, nil
		}
		return fips.Items[i].DeepCopy(), fips.Items[i].DeepCopy()
	})
}

func (c *Controller) handleAddOvnFipFinalizer(cachedFip *kubeovnv1.OvnFip, finalizer string) error {
	if cachedFip.DeletionTimestamp.IsZero() {
		if slices.Contains(cachedFip.Finalizers, finalizer) {
			return nil
		}
	}
	newFip := cachedFip.DeepCopy()
	controllerutil.AddFinalizer(newFip, finalizer)
	patch, err := util.GenerateMergePatchPayload(cachedFip, newFip)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ovn fip '%s', %v", cachedFip.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().OvnFips().Patch(context.Background(), cachedFip.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for ovn fip '%s', %v", cachedFip.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelOvnFipFinalizer(cachedFip *kubeovnv1.OvnFip, finalizer string) error {
	if len(cachedFip.Finalizers) == 0 {
		return nil
	}
	var err error
	newFip := cachedFip.DeepCopy()
	controllerutil.RemoveFinalizer(newFip, finalizer)
	patch, err := util.GenerateMergePatchPayload(cachedFip, newFip)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ovn fip '%s', %v", cachedFip.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().OvnFips().Patch(context.Background(), cachedFip.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from ovn fip '%s', %v", cachedFip.Name, err)
		return err
	}
	return nil
}
