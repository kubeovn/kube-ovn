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

func (c *Controller) enqueueAddOvnFip(obj interface{}) {

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add ovn fip %s", key)
	c.addOvnFipQueue.Add(key)
}

func (c *Controller) enqueueUpdateOvnFip(old, new interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}
	oldFip := old.(*kubeovnv1.OvnFip)
	newFip := new.(*kubeovnv1.OvnFip)
	if !newFip.DeletionTimestamp.IsZero() {
		if len(newFip.Finalizers) == 0 {
			// avoid delete fip twice
			return
		} else {
			klog.V(3).Infof("enqueue del ovn fip %s", key)
			c.delOvnFipQueue.Add(key)
			return
		}
	}
	if oldFip.Spec.OvnEip != newFip.Spec.OvnEip {
		// enqueue to reset eip to be clean
		klog.V(3).Infof("enqueue reset old ovn eip %s", oldFip.Spec.OvnEip)
		c.resetOvnEipQueue.Add(oldFip.Spec.OvnEip)
	}
	if oldFip.Spec.OvnEip != newFip.Spec.OvnEip {
		klog.V(3).Infof("enqueue update fip %s", key)
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
	klog.V(3).Infof("enqueue del ovn fip %s", key)
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

func (c *Controller) handleAddOvnFip(key string) error {
	cachedFip, err := c.ovnFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if cachedFip.Status.Ready && cachedFip.Status.V4Ip != "" {
		// already ok
		return nil
	}
	klog.V(3).Infof("handle add fip %s", key)
	var internalV4Ip, mac, subnetName string
	if cachedFip.Spec.IpType == util.NatUsingVip {
		internalVip, err := c.virtualIpsLister.Get(cachedFip.Spec.IpName)
		if err != nil {
			klog.Errorf("failed to get vip %s, %v", cachedFip.Spec.IpName, err)
			return err
		}
		internalV4Ip = internalVip.Status.V4ip
		mac = internalVip.Status.Mac
		subnetName = internalVip.Spec.Subnet
	} else {
		internalIp, err := c.ipsLister.Get(cachedFip.Spec.IpName)
		if err != nil {
			klog.Errorf("failed to get ip %s, %v", cachedFip.Spec.IpName, err)
			return err
		}
		internalV4Ip = internalIp.Spec.V4IPAddress
		mac = internalIp.Spec.MacAddress
		subnetName = internalIp.Spec.Subnet
	}

	// get eip
	eipName := cachedFip.Spec.OvnEip
	if len(eipName) == 0 {
		klog.Errorf("failed to create fip rule, should set eip")
	}
	cachedEip, err := c.GetOvnEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}
	subnet, err := c.subnetsLister.Get(subnetName)
	if err != nil {
		klog.Errorf("failed to get vpc subnet %s, %v", subnetName, err)
		return err
	}
	if cachedEip.Status.V4Ip == "" || internalV4Ip == "" {
		err := fmt.Errorf("failed to create v4 fip %s", cachedFip.Name)
		klog.Error(err)
		return err
	}
	vpcName := subnet.Spec.Vpc
	if cachedEip.Status.Type != "" && cachedEip.Status.Type != util.FipUsingEip {
		err = fmt.Errorf("failed to create ovn fip %s, eip '%s' is using by %s", key, eipName, cachedEip.Spec.Type)
		return err
	}
	if cachedEip.Status.Type == util.FipUsingEip &&
		cachedEip.Annotations[util.VpcNatAnnotation] != "" &&
		cachedEip.Annotations[util.VpcNatAnnotation] != cachedFip.Name {
		err = fmt.Errorf("failed to create fip %s, eip '%s' is using by other fip %s", key, eipName, cachedEip.Annotations[util.VpcNatAnnotation])
		return err
	}
	if err = c.patchOvnFipStatus(key, vpcName, cachedEip.Status.V4Ip,
		internalV4Ip, mac, false); err != nil {
		klog.Errorf("failed to patch status for fip %s, %v", key, err)
		return err
	}
	if err = c.handleAddOvnEipFinalizer(cachedEip, util.OvnFipUseEipFinalizer); err != nil {
		klog.Errorf("failed to add finalizer for ovn eip, %v", err)
		return err
	}
	if err = c.handleAddOvnFipFinalizer(cachedFip); err != nil {
		klog.Errorf("failed to handle finalizer for ovn fip, %v", err)
		return err
	}
	// ovn add fip
	if err = c.ovnLegacyClient.AddFipRule(vpcName, cachedEip.Status.V4Ip,
		internalV4Ip, mac, cachedFip.Spec.IpName); err != nil {
		klog.Errorf("failed to create v4 fip, %v", err)
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
	if err = c.patchOvnFipStatus(key, vpcName, cachedEip.Status.V4Ip,
		internalV4Ip, mac, true); err != nil {
		klog.Errorf("failed to patch status for fip %s, %v", key, err)
		return err
	}
	if err = c.patchOvnEipNat(eipName, util.FipUsingEip); err != nil {
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
		return err
	}
	klog.V(3).Infof("handle update fip %s", key)
	var internalV4Ip, mac, subnetName string
	if cachedFip.Spec.IpType == util.NatUsingVip {
		internalVip, err := c.virtualIpsLister.Get(cachedFip.Spec.IpName)
		if err != nil {
			klog.Errorf("failed to get vip %s, %v", cachedFip.Spec.IpName, err)
			return err
		}
		internalV4Ip = internalVip.Status.V4ip
		mac = internalVip.Status.Mac
		subnetName = internalVip.Spec.Subnet
	} else {
		internalIp, err := c.ipsLister.Get(cachedFip.Spec.IpName)
		if err != nil {
			klog.Errorf("failed to get ip %s, %v", cachedFip.Spec.IpName, err)
			return err
		}
		internalV4Ip = internalIp.Spec.V4IPAddress
		mac = internalIp.Spec.MacAddress
		subnetName = internalIp.Spec.Subnet
	}
	// get eip
	eipName := cachedFip.Spec.OvnEip
	if len(eipName) == 0 {
		klog.Errorf("failed to create fip rule, should set eip")
	}
	cachedEip, err := c.GetOvnEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}
	subnet, err := c.subnetsLister.Get(subnetName)
	if err != nil {
		klog.Errorf("failed to get vpc subnet %s, %v", subnetName, err)
		return err
	}
	if cachedEip.Status.V4Ip == "" || internalV4Ip == "" {
		err := fmt.Errorf("failed to create v4 fip %s", cachedFip.Name)
		klog.Error(err)
		return err
	}
	vpcName := subnet.Spec.Vpc
	if err = c.ovnLegacyClient.AddFipRule(vpcName, cachedEip.Status.V4Ip,
		internalV4Ip, mac, cachedFip.Spec.IpName); err != nil {
		klog.Errorf("failed to create v4 fip, %v", err)
		return err
	}
	if cachedEip.Spec.Type != "" && cachedEip.Spec.Type != util.FipUsingEip {
		// eip is in use by other nat
		err = fmt.Errorf("failed to update fip %s, eip '%s' is using by %s", key, eipName, cachedEip.Spec.Type)
		return err
	}
	if cachedEip.Spec.Type == util.FipUsingEip &&
		cachedEip.Annotations[util.VpcNatAnnotation] != "" &&
		cachedEip.Annotations[util.VpcNatAnnotation] != cachedFip.Name {
		err = fmt.Errorf("failed to update fip %s, eip '%s' is using by other fip %s", key, eipName, cachedEip.Annotations[util.VpcNatAnnotation])
		return err
	}
	fip := cachedFip.DeepCopy()
	// fip change eip
	if c.ovnFipChangeEip(fip, cachedEip) {
		klog.V(3).Infof("fip change ip, old ip '%s', new ip %s", fip.Status.V4Ip, cachedEip.Status.V4Ip)
		if err = c.ovnLegacyClient.DeleteFipRule(vpcName, fip.Status.V4Ip, internalV4Ip); err != nil {
			klog.Errorf("failed to create fip, %v", err)
			return err
		}
		// ovn add fip
		if err = c.ovnLegacyClient.AddFipRule(vpcName, cachedEip.Status.V4Ip,
			internalV4Ip, mac, cachedFip.Spec.IpName); err != nil {
			klog.Errorf("failed to create fip, %v", err)
			return err
		}
		if err = c.natLabelAndAnnoOvnEip(eipName, fip.Name, vpcName); err != nil {
			klog.Errorf("failed to label fip '%s' in eip %s, %v", fip.Name, eipName, err)
			return err
		}
		if err = c.patchOvnFipAnnotations(key, eipName); err != nil {
			klog.Errorf("failed to update label for fip %s, %v", key, err)
			return err
		}
		if err = c.patchOvnFipStatus(key, vpcName, cachedEip.Status.V4Ip,
			internalV4Ip, mac, true); err != nil {
			klog.Errorf("failed to patch status for fip '%s', %v", key, err)
			return err
		}
		return nil
	}
	return nil
}

func (c *Controller) handleDelOvnFip(key string) error {
	klog.V(3).Infof("handle del ovn fip %s", key)
	cachedFip, err := c.ovnFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	eipName := cachedFip.Spec.OvnEip
	if len(eipName) == 0 {
		klog.Errorf("failed to delete ovn fip, should set eip")
	}
	cachedEip, err := c.GetOvnEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}
	// ovn delete fip
	if cachedFip.Status.Vpc != "" && cachedFip.Status.V4Eip != "" && cachedFip.Status.V4Ip != "" {
		if err = c.ovnLegacyClient.DeleteFipRule(cachedFip.Status.Vpc, cachedFip.Status.V4Eip, cachedFip.Status.V4Ip); err != nil {
			klog.Errorf("failed to delete fip, %v", err)
			return err
		}
	}
	if err = c.handleDelOvnFipFinalizer(cachedFip); err != nil {
		klog.Errorf("failed to handle remove finalizer from ovn fip, %v", err)
		return err
	}
	//  reset eip
	c.resetOvnEipQueue.Add(cachedFip.Spec.OvnEip)
	if err = c.handleDelOvnEipFinalizer(cachedEip, util.OvnFipUseEipFinalizer); err != nil {
		klog.Errorf("failed to handle remove finalizer from ovn eip, %v", err)
		return err
	}
	return nil
}

func (c *Controller) handleAddOvnFipFinalizer(cachedFip *kubeovnv1.OvnFip) error {
	if cachedFip.DeletionTimestamp.IsZero() {
		if util.ContainsString(cachedFip.Finalizers, util.ControllerName) {
			return nil
		}
	}
	newFip := cachedFip.DeepCopy()
	controllerutil.AddFinalizer(newFip, util.ControllerName)
	patch, err := util.GenerateMergePatchPayload(cachedFip, newFip)
	if err != nil {
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

func (c *Controller) handleDelOvnFipFinalizer(cachedFip *kubeovnv1.OvnFip) error {
	if len(cachedFip.Finalizers) == 0 {
		return nil
	}
	newFip := cachedFip.DeepCopy()
	controllerutil.RemoveFinalizer(newFip, util.ControllerName)
	patch, err := util.GenerateMergePatchPayload(cachedFip, newFip)
	if err != nil {
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

func (c *Controller) patchOvnFipAnnotations(key, eipName string) error {
	oriFip, err := c.ovnFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
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
		_, err := c.config.KubeOvnClient.KubeovnV1().OvnFips().Patch(context.Background(), fip.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
		if err != nil {
			klog.Errorf("failed to patch annotation for ovn fip %s, %v", fip.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchOvnFipStatus(key, vpcName, v4Eip, podIp, podMac string, ready bool) error {
	oriFip, err := c.ovnFipsLister.Get(key)
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
	if (v4Eip != "" && fip.Status.V4Eip != v4Eip) ||
		(vpcName != "" && fip.Status.Vpc != vpcName) ||
		(podIp != "" && fip.Status.V4Ip != podIp) ||
		(podMac != "" && fip.Status.MacAddress != podMac) {
		fip.Status.Vpc = vpcName
		fip.Status.V4Eip = v4Eip
		fip.Status.V4Ip = podIp
		fip.Status.MacAddress = podMac
		changed = true
	}
	if changed {
		bytes, err := fip.Status.Bytes()
		if err != nil {
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

func (c *Controller) ovnFipChangeEip(fip *kubeovnv1.OvnFip, eip *kubeovnv1.OvnEip) bool {
	if fip.Status.V4Ip == "" || eip.Status.V4Ip == "" {
		// eip created but not ready
		return false
	}
	if fip.Status.V4Ip != eip.Status.V4Ip {
		return true
	}
	return false
}

func (c *Controller) GetOvnEip(eipName string) (*kubeovnv1.OvnEip, error) {
	cachedEip, err := c.ovnEipsLister.Get(eipName)
	if err != nil {
		klog.Errorf("failed to get eip %s, %v", eipName, err)
		return nil, err
	}
	if cachedEip.Status.V4Ip == "" {
		return nil, fmt.Errorf("eip '%s' is not ready, has no v4ip", eipName)
	}
	return cachedEip, nil
}
