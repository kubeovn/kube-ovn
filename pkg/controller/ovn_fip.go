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
	if !c.isLeader() {
		return
	}
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
	if !c.isLeader() {
		return
	}
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
	if !c.isLeader() {
		return
	}
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
	if cachedFip.Status.Ready && cachedFip.Status.V4ip != "" {
		// already ok
		return nil
	}
	klog.V(3).Infof("handle add fip %s", key)
	vpcPodIp, err := c.ipsLister.Get(cachedFip.Spec.IpName)
	if err != nil {
		return err
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
	vpcName, err := c.getVpcBySubnet(cachedEip.Spec.ExternalSubnet)
	if err != nil {
		klog.Errorf("failed to get vpc, %v", err)
		return err
	}
	if cachedEip.Spec.Type != "" && cachedEip.Spec.Type != util.FipUsingEip {
		err = fmt.Errorf("failed to create ovn fip %s, eip '%s' is using by %s", key, eipName, cachedEip.Spec.Type)
		return err
	}
	if cachedEip.Spec.Type == util.FipUsingEip &&
		cachedEip.Labels[util.VpcNatLabel] != "" &&
		cachedEip.Labels[util.VpcNatLabel] != cachedFip.Name {
		err = fmt.Errorf("failed to create fip %s, eip '%s' is using by other fip %s", key, eipName, cachedEip.Labels[util.VpcNatLabel])
		return err
	}
	if err = c.handleAddOvnEipFinalizer(cachedEip); err != nil {
		klog.Errorf("failed to add finalizer for ovn eip, %v", err)
		return err
	}
	if err = c.handleAddOvnFipFinalizer(cachedFip); err != nil {
		klog.Errorf("failed to handle finalizer for ovn fip, %v", err)
		return err
	}
	// ovn add fip
	if err = c.ovnLegacyClient.AddFipRule(vpcName, cachedEip.Spec.V4ip, vpcPodIp.Spec.V4IPAddress, vpcPodIp.Spec.MacAddress, vpcPodIp.Name); err != nil {
		klog.Errorf("failed to create fip, %v", err)
		return err
	}
	// patch fip eip relationship
	if err = c.natLabelOvnEip(eipName, cachedFip.Name, vpcName); err != nil {
		klog.Errorf("failed to label fip '%s' in eip %s, %v", cachedFip.Name, eipName, err)
		return err
	}
	if err = c.patchOvnFipLabel(key, eipName); err != nil {
		klog.Errorf("failed to update label for fip %s, %v", key, err)
		return err
	}
	if err = c.patchOvnEipStatus(eipName); err != nil {
		klog.Errorf("failed to patch status for eip %s, %v", key, err)
		return err
	}
	if err = c.patchOvnFipStatus(key, cachedEip.Spec.V4ip, cachedEip.Spec.V6ip, true); err != nil {
		klog.Errorf("failed to patch status for fip %s, %v", key, err)
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
	klog.V(3).Infof("handle add fip %s", key)
	vpcPodIp, err := c.ipsLister.Get(cachedFip.Spec.IpName)
	if err != nil {
		return err
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
	vpcName, err := c.getVpcBySubnet(cachedEip.Spec.ExternalSubnet)
	if err != nil {
		klog.Errorf("failed to get vpc, %v", err)
		return err
	}
	if cachedEip.Spec.Type != "" && cachedEip.Spec.Type != util.FipUsingEip {
		// eip is in use by other nat
		err = fmt.Errorf("failed to update fip %s, eip '%s' is using by %s", key, eipName, cachedEip.Spec.Type)
		return err
	}
	if cachedEip.Spec.Type == util.FipUsingEip &&
		cachedEip.Labels[util.VpcNatLabel] != "" &&
		cachedEip.Labels[util.VpcNatLabel] != cachedFip.Name {
		err = fmt.Errorf("failed to update fip %s, eip '%s' is using by other fip %s", key, eipName, cachedEip.Labels[util.VpcNatLabel])
		return err
	}
	fip := cachedFip.DeepCopy()
	// fip change eip
	if c.ovnFipChangeEip(fip, cachedEip) {
		klog.V(3).Infof("fip change ip, old ip '%s', new ip %s", fip.Status.V4ip, cachedEip.Spec.V4ip)
		if err = c.ovnLegacyClient.DeleteFipRule(vpcName, fip.Status.V4ip, vpcPodIp.Spec.V4IPAddress); err != nil {
			klog.Errorf("failed to create fip, %v", err)
			return err
		}
		// ovn add fip
		if err = c.ovnLegacyClient.AddFipRule(vpcName, cachedEip.Spec.V4ip, vpcPodIp.Spec.V4IPAddress, vpcPodIp.Spec.MacAddress, vpcPodIp.Name); err != nil {
			klog.Errorf("failed to create fip, %v", err)
			return err
		}
		if err = c.natLabelOvnEip(eipName, fip.Name, vpcName); err != nil {
			klog.Errorf("failed to label fip '%s' in eip %s, %v", fip.Name, eipName, err)
			return err
		}
		if err = c.patchOvnFipLabel(key, eipName); err != nil {
			klog.Errorf("failed to update label for fip %s, %v", key, err)
			return err
		}
		if err = c.patchOvnFipStatus(key, cachedEip.Spec.V4ip, cachedEip.Spec.V6ip, true); err != nil {
			klog.Errorf("failed to patch status for fip '%s', %v", key, err)
			return err
		}
		if err = c.patchOvnEipStatus(eipName); err != nil {
			klog.Errorf("failed to patch status for eip %s, %v", key, err)
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
	vpcName, err := c.getVpcBySubnet(cachedEip.Spec.ExternalSubnet)
	if err != nil {
		klog.Errorf("failed to get vpc, %v", err)
		return err
	}
	vpcPodIp, err := c.ipsLister.Get(cachedFip.Spec.IpName)
	if err != nil {
		return err
	}
	// ovn delete fip
	if err = c.ovnLegacyClient.DeleteFipRule(vpcName, cachedEip.Spec.V4ip, vpcPodIp.Spec.V4IPAddress); err != nil {
		klog.Errorf("failed to create fip, %v", err)
		return err
	}
	if err = c.handleDelOvnFipFinalizer(cachedFip); err != nil {
		klog.Errorf("failed to handle remove finalizer from ovn fip, %v", err)
		return err
	}
	//  reset eip
	c.resetOvnEipQueue.Add(cachedFip.Spec.OvnEip)
	if err = c.handleDelOvnEipFinalizer(cachedEip); err != nil {
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

func (c *Controller) patchOvnFipLabel(key, eipName string) error {
	oriFip, err := c.ovnFipsLister.Get(key)
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
			util.VpcEipLabel: eipName,
		}
		needUpdateLabel = true
	}
	if fip.Labels[util.VpcEipLabel] != eipName {
		op = "replace"
		fip.Labels[util.VpcEipLabel] = eipName
		needUpdateLabel = true
	}
	if needUpdateLabel {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
		raw, _ := json.Marshal(fip.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		_, err := c.config.KubeOvnClient.KubeovnV1().OvnFips().Patch(context.Background(), fip.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
		if err != nil {
			klog.Errorf("failed to patch label for ovn fip %s, %v", fip.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchOvnFipStatus(key, v4ip, v6ip string, ready bool) error {
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
	if ready && v4ip != "" && fip.Status.V4ip != v4ip {
		fip.Status.V4ip = v4ip
		fip.Status.V6ip = v6ip
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
	if fip.Status.V4ip == "" || eip.Spec.V4ip == "" {
		// eip created but not ready
		return false
	}
	if fip.Status.V4ip != eip.Spec.V4ip {
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
	if cachedEip.Spec.V4ip == "" {
		return nil, fmt.Errorf("eip '%s' is not ready, has no v4ip", eipName)
	}
	return cachedEip, nil
}
