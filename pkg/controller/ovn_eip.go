package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

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

func (c *Controller) enqueueAddOvnEip(obj interface{}) {

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add ovn eip %s", key)
	c.addOvnEipQueue.Add(key)
}

func (c *Controller) enqueueUpdateOvnEip(old, new interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}
	oldovnEip := old.(*kubeovnv1.OvnEip)
	newovnEip := new.(*kubeovnv1.OvnEip)
	if newovnEip.DeletionTimestamp != nil {
		if len(newovnEip.Finalizers) == 0 {
			// avoid delete eip twice
			return
		} else {
			klog.V(3).Infof("enqueue del ovn eip %s", key)
			c.delOvnEipQueue.Add(key)
			return
		}
	}
	if oldovnEip.Spec.V4Ip != "" && oldovnEip.Spec.V4Ip != newovnEip.Spec.V4Ip ||
		oldovnEip.Spec.MacAddress != "" && oldovnEip.Spec.MacAddress != newovnEip.Spec.MacAddress {
		klog.Infof("not support change ip or mac for eip %s", key)
		c.resetOvnEipQueue.Add(key)
		return
	}
	klog.V(3).Infof("enqueue update ovn eip %s", key)
	c.updateOvnEipQueue.Add(key)
}

func (c *Controller) enqueueDelOvnEip(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue del ovn eip %s", key)
	c.delOvnEipQueue.Add(key)
}

func (c *Controller) runAddOvnEipWorker() {
	for c.processNextAddOvnEipWorkItem() {
	}
}

func (c *Controller) runUpdateOvnEipWorker() {
	for c.processNextUpdateOvnEipWorkItem() {
	}
}

func (c *Controller) runResetOvnEipWorker() {
	for c.processNextResetOvnEipWorkItem() {
	}
}

func (c *Controller) runDelOvnEipWorker() {
	for c.processNextDeleteOvnEipWorkItem() {
	}
}

func (c *Controller) processNextAddOvnEipWorkItem() bool {
	obj, shutdown := c.addOvnEipQueue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.addOvnEipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addOvnEipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddOvnEip(key); err != nil {
			c.addOvnEipQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addOvnEipQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateOvnEipWorkItem() bool {
	obj, shutdown := c.updateOvnEipQueue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.updateOvnEipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateOvnEipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateOvnEip(key); err != nil {
			c.updateOvnEipQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateOvnEipQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextResetOvnEipWorkItem() bool {
	obj, shutdown := c.resetOvnEipQueue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.resetOvnEipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.resetOvnEipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleResetOvnEip(key); err != nil {
			c.resetOvnEipQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.resetOvnEipQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteOvnEipWorkItem() bool {
	obj, shutdown := c.delOvnEipQueue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.delOvnEipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.delOvnEipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDelOvnEip(key); err != nil {
			c.delOvnEipQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.delOvnEipQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleAddOvnEip(key string) error {
	cachedEip, err := c.ovnEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if cachedEip.Status.MacAddress != "" {
		// already ok
		return nil
	}
	klog.V(3).Infof("handle add ovn eip %s", cachedEip.Name)
	var v4ip, v6ip, mac, subnetName string
	subnetName = cachedEip.Spec.ExternalSubnet
	if subnetName == "" {
		return fmt.Errorf("failed to create ovn eip '%s', subnet should be set", key)
	}
	subnet, err := c.subnetsLister.Get(subnetName)
	if err != nil {
		klog.Errorf("failed to get external subnet, %v", err)
		return err
	}
	portName := cachedEip.Name
	if cachedEip.Spec.V4Ip != "" {
		v4ip, v6ip, mac, err = c.acquireStaticIpAddress(subnet.Name, cachedEip.Name, portName, cachedEip.Spec.V4Ip)
	} else {
		// random allocate
		v4ip, v6ip, mac, err = c.acquireIpAddress(subnet.Name, cachedEip.Name, portName)
	}
	if err != nil {
		klog.Errorf("failed to acquire ip address, %v", err)
		return err
	}

	if cachedEip.Spec.Type == util.NodeExtGwUsingEip {
		mergedIp := util.GetStringIP(v4ip, v6ip)
		if err := c.ovnClient.CreateBareLogicalSwitchPort(subnet.Name, portName, mergedIp, mac); err != nil {
			klog.Error("failed to create lsp for ovn eip %s, %v", key, err)
			return err
		}

	}

	if err = c.createOrUpdateCrdOvnEip(key, subnet.Name, v4ip, v6ip, mac, cachedEip.Spec.Type); err != nil {
		klog.Errorf("failed to create or update ovn eip '%s', %v", cachedEip.Name, err)
		return err
	}
	if cachedEip.Spec.Type != util.NodeExtGwUsingEip {
		// node ext gw eip has a nic on node, so left node to make it ready
		if err = c.patchOvnEipStatus(key, true); err != nil {
			klog.Errorf("failed to patch ovn eip %s: %v", key, err)
			return err
		}
	}
	if err = c.subnetCountIp(subnet); err != nil {
		klog.Errorf("failed to count ovn eip '%s' in subnet, %v", cachedEip.Name, err)
		return err
	}

	return nil
}

func (c *Controller) handleUpdateOvnEip(key string) error {
	cachedEip, err := c.ovnEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !cachedEip.DeletionTimestamp.IsZero() {
		subnetName := cachedEip.Spec.ExternalSubnet
		if subnetName == "" {
			return fmt.Errorf("failed to update ovn eip '%s', subnet should be set", key)
		}
		subnet, err := c.subnetsLister.Get(subnetName)
		if err != nil {
			klog.Errorf("failed to get external subnet, %v", err)
			return err
		}
		if err = c.subnetCountIp(subnet); err != nil {
			klog.Errorf("failed to count ovn eip '%s' in subnet, %v", cachedEip.Name, err)
			return err
		}
		return nil
	}
	if cachedEip.Spec.Type != util.NodeExtGwUsingEip {
		// node ext gw eip has a nic on node, so left node to make it ready
		if err = c.patchOvnEipStatus(key, true); err != nil {
			klog.Errorf("failed to patch ovn eip %s: %v", key, err)
			return err
		}
	}
	return nil
}

func (c *Controller) handleResetOvnEip(key string) error {
	cachedEip, err := c.ovnEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	var notUse bool
	if notUse, err = c.isOvnEipNotUse(cachedEip); err != nil {
		klog.Errorf("failed to check whether ovn eip '%s' is still in use, %v", cachedEip.Name, err)
		return err
	}

	if notUse {
		if cachedEip.Status.MacAddress != "" && cachedEip.Status.MacAddress != cachedEip.Spec.MacAddress {
			// eip not support change ip, reset eip spec from its status
			if err = c.resetOvnEipSpec(key); err != nil {
				klog.Errorf("failed to reset ovn eip '%s', %v", cachedEip.Name, err)
				return err
			}
			return nil
		}
		if cachedEip.Spec.Type == "" {
			if err := c.patchOvnEipNat(key, ""); err != nil {
				klog.Errorf("failed to reset ovn eip '%s', %v", cachedEip.Name, err)
				return err
			}
		}
		if err = c.natLabelAndAnnoOvnEip(cachedEip.Name, "", ""); err != nil {
			klog.Errorf("failed to reset ovn eip %s, %v", cachedEip.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) handleDelOvnEip(key string) error {
	klog.V(3).Infof("handle del ovn eip %s", key)
	cachedEip, err := c.ovnEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if len(cachedEip.Finalizers) > 1 {
		err := errors.New("eip is referenced, it cannot be deleted directly")
		klog.Errorf("failed to delete eip %s, %v", key, err)
		return err
	}

	if cachedEip.Spec.Type == util.NodeExtGwUsingEip {
		if err := c.ovnClient.DeleteLogicalSwitchPort(cachedEip.Name); err != nil {
			klog.Errorf("failed to delete lsp %s, %v", cachedEip.Name, err)
			return err
		}
	}

	if cachedEip.Spec.Type == util.LrpUsingEip {
		if err := c.ovnClient.DeleteLogicalRouterPort(key); err != nil {
			klog.Errorf("failed to delete lrp %s, %v", key, err)
			return err
		}
	}

	c.ipam.ReleaseAddressByPod(key)
	return nil
}

func (c *Controller) createOrUpdateCrdOvnEip(key, subnet, v4ip, v6ip, mac, usage string) error {
	cachedEip, err := c.ovnEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err := c.config.KubeOvnClient.KubeovnV1().OvnEips().Create(context.Background(), &kubeovnv1.OvnEip{
				ObjectMeta: metav1.ObjectMeta{
					Name: key,
					Labels: map[string]string{
						util.SubnetNameLabel:  subnet,
						util.OvnEipUsageLabel: usage,
					},
				},
				Spec: kubeovnv1.OvnEipSpec{
					ExternalSubnet: subnet,
					V4Ip:           v4ip,
					V6Ip:           v6ip,
					MacAddress:     mac,
					Type:           usage,
				},
			}, metav1.CreateOptions{})
			if err != nil {
				err := fmt.Errorf("failed to create crd ovn eip '%s', %v", key, err)
				return err
			}
			// wait local cache ready
			time.Sleep(1 * time.Second)
		} else {
			return err
		}
	} else {
		ovnEip := cachedEip.DeepCopy()
		if ovnEip.Spec.V4Ip == "" && v4ip != "" ||
			ovnEip.Spec.V6Ip == "" && v6ip != "" {
			ovnEip.Spec.ExternalSubnet = subnet
			ovnEip.Spec.V4Ip = v4ip
			ovnEip.Spec.V6Ip = v6ip
			ovnEip.Spec.MacAddress = mac
			ovnEip.Spec.Type = usage
			if _, err := c.config.KubeOvnClient.KubeovnV1().OvnEips().Update(context.Background(), ovnEip, metav1.UpdateOptions{}); err != nil {
				errMsg := fmt.Errorf("failed to update ovn eip '%s', %v", key, err)
				klog.Error(errMsg)
				return errMsg
			}
		}

		if ovnEip.Status.MacAddress == "" {
			ovnEip.Status.V4Ip = v4ip
			ovnEip.Status.V6Ip = v6ip
			ovnEip.Status.MacAddress = mac
			ovnEip.Status.Type = usage
			bytes, err := ovnEip.Status.Bytes()
			if err != nil {
				klog.Error("failed to marshal ovn eip %s, %v", key, err)
				return err
			}
			if _, err = c.config.KubeOvnClient.KubeovnV1().OvnEips().Patch(context.Background(), key, types.MergePatchType,
				bytes, metav1.PatchOptions{}, "status"); err != nil {
				if k8serrors.IsNotFound(err) {
					return nil
				}
				klog.Errorf("failed to patch ovn eip %s, %v", ovnEip.Name, err)
				return err
			}
		}

		var needUpdateLabel bool
		var op string
		if len(ovnEip.Labels) == 0 {
			op = "add"
			ovnEip.Labels = map[string]string{
				util.SubnetNameLabel:  subnet,
				util.OvnEipUsageLabel: usage,
			}
			needUpdateLabel = true
		}
		if ovnEip.Labels[util.SubnetNameLabel] != subnet {
			op = "replace"
			ovnEip.Labels[util.SubnetNameLabel] = subnet
			needUpdateLabel = true
		}
		if needUpdateLabel {
			patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
			raw, _ := json.Marshal(ovnEip.Labels)
			patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
			if _, err := c.config.KubeOvnClient.KubeovnV1().OvnEips().Patch(context.Background(), ovnEip.Name, types.JSONPatchType,
				[]byte(patchPayload), metav1.PatchOptions{}); err != nil {
				klog.Errorf("failed to patch label for ovn eip '%s', %v", ovnEip.Name, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) patchLrpOvnEipEnableBfdLabel(key string, enableBfd bool) error {
	cachedEip, err := c.ovnEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		} else {
			err := fmt.Errorf("failed to get ovn eip %s, %v", key, err)
			klog.Error(err)
			return err
		}
	}
	ovnEip := cachedEip.DeepCopy()
	expectValue := strconv.FormatBool(enableBfd)
	if val, ok := ovnEip.Labels[util.OvnLrpEipEnableBfdLabel]; ok && (val == expectValue) {
		return nil
	}
	op := "replace"
	ovnEip.Labels[util.OvnLrpEipEnableBfdLabel] = expectValue
	patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
	raw, _ := json.Marshal(ovnEip.Labels)
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
	if _, err := c.config.KubeOvnClient.KubeovnV1().OvnEips().Patch(context.Background(), ovnEip.Name, types.JSONPatchType,
		[]byte(patchPayload), metav1.PatchOptions{}); err != nil {
		klog.Errorf("failed to patch label for ovn eip '%s', %v", ovnEip.Name, err)
		return err
	}
	return nil
}

func (c *Controller) patchOvnEipStatus(key string, ready bool) error {
	cachedOvnEip, err := c.ovnEipsLister.Get(key)
	if err != nil {
		klog.Errorf("failed to get cached ovn eip '%s', %v", key, err)
		return err
	}
	ovnEip := cachedOvnEip.DeepCopy()
	changed := false
	if ovnEip.Status.Ready != ready {
		ovnEip.Status.Ready = ready
		changed = true
	}
	if ovnEip.Status.MacAddress == "" {
		// not support change ip
		ovnEip.Status.V4Ip = cachedOvnEip.Spec.V4Ip
		ovnEip.Status.V6Ip = cachedOvnEip.Spec.V6Ip
		ovnEip.Status.MacAddress = cachedOvnEip.Spec.MacAddress
		changed = true
	}
	if ovnEip.Spec.Type != "" && ovnEip.Spec.Type != ovnEip.Status.Type {
		ovnEip.Status.Type = ovnEip.Spec.Type
		changed = true
	}
	if changed {
		bytes, err := ovnEip.Status.Bytes()
		if err != nil {
			klog.Errorf("failed to marshal ovn eip status '%s', %v", key, err)
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().OvnEips().Patch(context.Background(), ovnEip.Name,
			types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
			klog.Errorf("failed to patch status for ovn eip '%s', %v", key, err)
			return err
		}
	}
	return nil
}

func (c *Controller) resetOvnEipSpec(key string) error {
	cachedOvnEip, err := c.ovnEipsLister.Get(key)
	if err != nil {
		klog.Errorf("failed to get cached ovn eip '%s', %v", key, err)
		return err
	}
	ovnEip := cachedOvnEip.DeepCopy()
	changed := false
	if ovnEip.Status.MacAddress != "" {
		// not support change ip
		ovnEip.Spec.V4Ip = ovnEip.Status.V4Ip
		ovnEip.Spec.V6Ip = ovnEip.Status.V6Ip
		ovnEip.Spec.MacAddress = ovnEip.Status.MacAddress
		changed = true
	}
	if changed {
		klog.V(3).Infof("reset spec for eip %s", key)
		if _, err = c.config.KubeOvnClient.KubeovnV1().OvnEips().Update(context.Background(), ovnEip, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to update spec for ovn eip '%s', %v", key, err)
			return err
		}
	}
	return nil
}

func (c *Controller) natLabelAndAnnoOvnEip(eipName, natName, vpcName string) error {
	cachedEip, err := c.ovnEipsLister.Get(eipName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	eip := cachedEip.DeepCopy()
	var needUpdateLabel, needUpdateAnno bool
	var op string
	if len(eip.Labels) == 0 {
		op = "add"
		needUpdateLabel = true
		eip.Labels = map[string]string{
			util.SubnetNameLabel: cachedEip.Spec.ExternalSubnet,
			util.VpcNameLabel:    vpcName,
		}
	} else if eip.Labels[util.VpcNameLabel] != vpcName {
		op = "replace"
		needUpdateLabel = true
		eip.Labels[util.SubnetNameLabel] = cachedEip.Spec.ExternalSubnet
		eip.Labels[util.VpcNameLabel] = vpcName
	}
	if needUpdateLabel {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
		raw, _ := json.Marshal(eip.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().OvnEips().Patch(context.Background(), eip.Name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to patch label for ovn eip %s, %v", eip.Name, err)
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
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/annotations", "value": %s }]`
		raw, _ := json.Marshal(eip.Annotations)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().OvnEips().Patch(context.Background(), eip.Name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to patch annotation for ovn eip %s, %v", eip.Name, err)
			return err
		}
	}

	return err
}

func (c *Controller) handleAddOvnEipFinalizer(cachedEip *kubeovnv1.OvnEip, finalizer string) error {
	if cachedEip.DeletionTimestamp.IsZero() {
		if util.ContainsString(cachedEip.Finalizers, finalizer) {
			return nil
		}
	}
	newEip := cachedEip.DeepCopy()
	controllerutil.AddFinalizer(newEip, finalizer)
	patch, err := util.GenerateMergePatchPayload(cachedEip, newEip)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ovn eip '%s', %v", cachedEip.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().OvnEips().Patch(context.Background(), cachedEip.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for ovn eip '%s', %v", cachedEip.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelOvnEipFinalizer(cachedEip *kubeovnv1.OvnEip, finalizer string) error {
	if len(cachedEip.Finalizers) == 0 {
		return nil
	}

	var err error
	var notUse bool
	if notUse, err = c.isOvnEipNotUse(cachedEip); err != nil {
		klog.Errorf("failed to check whether ovn eip '%s' is still in use, %v", cachedEip.Name, err)
		return err
	}

	if !notUse {
		klog.V(3).Infof("ovn eip '%s' is still in use, finalizer will not be removed", cachedEip.Name)
		return nil
	}

	newEip := cachedEip.DeepCopy()
	controllerutil.RemoveFinalizer(newEip, finalizer)
	patch, err := util.GenerateMergePatchPayload(cachedEip, newEip)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ovn eip '%s', %v", cachedEip.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().OvnEips().Patch(context.Background(), cachedEip.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from ovn eip '%s', %v", cachedEip.Name, err)
		return err
	}
	return nil
}

func (c *Controller) patchOvnEipNat(key, nat string) error {
	cachedEip, err := c.ovnEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if cachedEip.Status.Type == nat {
		return nil
	}
	eip := cachedEip.DeepCopy()
	eip.Status.Type = nat
	bytes, err := eip.Status.Bytes()
	if err != nil {
		klog.Errorf("failed to marshal eip %s, %v", eip.Name, err)
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().OvnEips().Patch(context.Background(), key, types.MergePatchType,
		bytes, metav1.PatchOptions{}, "status"); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to patch ovn eip %s, %v", eip.Name, err)
		return err
	}
	return nil
}

func (c *Controller) isOvnEipNotUse(cachedEip *kubeovnv1.OvnEip) (bool, error) {
	switch cachedEip.Status.Type {
	case util.DnatUsingEip:
		// nat change eip not that fast
		dnats, err := c.config.KubeOvnClient.KubeovnV1().OvnDnatRules().List(context.Background(), metav1.ListOptions{})
		if err != nil {
			klog.Errorf("failed to get ovn dnat list, %v", err)
			return false, err
		}
		for _, item := range dnats.Items {
			if item.Annotations[util.VpcEipAnnotation] == cachedEip.Name {
				return false, nil
			}
		}
	case util.SnatUsingEip:
		// nat change eip not that fast
		snats, err := c.config.KubeOvnClient.KubeovnV1().OvnSnatRules().List(context.Background(), metav1.ListOptions{})
		if err != nil {
			klog.Errorf("failed to get ovn snat, %v", err)
			return false, err
		}
		for _, item := range snats.Items {
			if item.Annotations[util.VpcEipAnnotation] == cachedEip.Name {
				return false, nil
			}
		}
	case util.FipUsingEip:
	default:
		return true, nil
	}
	return true, nil
}
