package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddOvnEip(obj interface{}) {

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.Infof("enqueue add ovn eip %s", key)
	c.addOvnEipQueue.Add(key)
}

func (c *Controller) enqueueUpdateOvnEip(old, new interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}
	oldEip := old.(*kubeovnv1.OvnEip)
	newEip := new.(*kubeovnv1.OvnEip)
	if newEip.DeletionTimestamp != nil {
		if len(newEip.Finalizers) == 0 {
			// avoid delete eip twice
			return
		} else {
			klog.Infof("enqueue del ovn eip %s", key)
			c.delOvnEipQueue.Add(newEip)
			return
		}
	}
	if oldEip.Spec.V4Ip != "" && oldEip.Spec.V4Ip != newEip.Spec.V4Ip ||
		oldEip.Spec.MacAddress != "" && oldEip.Spec.MacAddress != newEip.Spec.MacAddress {
		klog.Infof("not support change ip or mac for eip %s", key)
		c.resetOvnEipQueue.Add(key)
		return
	}
	if !reflect.DeepEqual(oldEip.Spec.V4Ip, newEip.Spec.V4Ip) ||
		!reflect.DeepEqual(oldEip.Spec.V6Ip, newEip.Spec.V6Ip) {
		klog.Infof("enqueue update ovn eip %s", key)
		c.updateOvnEipQueue.Add(key)
	}
}

func (c *Controller) enqueueDelOvnEip(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.Infof("enqueue del ovn eip %s", key)
	eip := obj.(*kubeovnv1.OvnEip)
	c.delOvnEipQueue.Add(eip)
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
		var eip *kubeovnv1.OvnEip
		var ok bool
		if eip, ok = obj.(*kubeovnv1.OvnEip); !ok {
			c.delOvnEipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected ovn eip in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDelOvnEip(eip); err != nil {
			c.delOvnEipQueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing '%s': %s, requeuing", eip.Name, err.Error())
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
		klog.Error(err)
		return err
	}
	if cachedEip.Status.MacAddress != "" {
		// already ok
		return nil
	}
	klog.Infof("handle add ovn eip %s", cachedEip.Name)
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

	if cachedEip.Spec.Type == util.Lsp {
		mergedIp := util.GetStringIP(v4ip, v6ip)
		if err := c.ovnClient.CreateBareLogicalSwitchPort(subnet.Name, portName, mergedIp, mac); err != nil {
			klog.Error("failed to create lsp for ovn eip %s, %v", key, err)
			return err
		}
	}
	if cachedEip.Spec.Type == "" {
		// the eip only used by nat: fip, dnat, snat
		cachedEip.Spec.Type = util.NatUsingEip
	}
	if err = c.createOrUpdateCrdOvnEip(key, subnet.Name, v4ip, v6ip, mac, cachedEip.Spec.Type); err != nil {
		klog.Errorf("failed to create or update ovn eip '%s', %v", cachedEip.Name, err)
		return err
	}
	if cachedEip.Spec.Type != util.Lsp {
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
		klog.Error(err)
		return err
	}
	klog.V(3).Infof("handle update ovn eip %s", cachedEip.Name)
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
	if cachedEip.Spec.Type != util.Lsp {
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
		klog.Error(err)
		return err
	}
	if !cachedEip.DeletionTimestamp.IsZero() {
		return nil
	}
	klog.Infof("handle reset ovn eip %s", cachedEip.Name)
	if err := c.patchOvnEipStatus(key, true); err != nil {
		klog.Errorf("failed to reset nat for eip %s, %v", key, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelOvnEip(eip *kubeovnv1.OvnEip) error {
	klog.V(3).Infof("handle del ovn eip %s", eip.Name)
	if len(eip.Finalizers) > 1 {
		err := errors.New("eip is referenced, it cannot be deleted directly")
		klog.Errorf("failed to delete eip %s, %v", eip.Name, err)
		return err
	}

	if err := c.handleDelOvnEipFinalizer(eip, util.OvnEipFinalizer); err != nil {
		klog.Errorf("failed to handle remove ovn eip finalizer , %v", err)
		return err
	}

	if eip.Spec.Type == util.Lsp {
		if err := c.ovnClient.DeleteLogicalSwitchPort(eip.Name); err != nil {
			klog.Errorf("failed to delete lsp %s, %v", eip.Name, err)
			return err
		}
	}

	if eip.Spec.Type == util.Lrp {
		if err := c.ovnClient.DeleteLogicalRouterPort(eip.Name); err != nil {
			klog.Errorf("failed to delete lrp %s, %v", eip.Name, err)
			return err
		}
	}

	c.ipam.ReleaseAddressByPod(eip.Name)
	c.updateSubnetStatusQueue.Add(eip.Spec.ExternalSubnet)
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
						util.SubnetNameLabel: subnet,
						util.OvnEipTypeLabel: usage,
						util.EipV4IpLabel:    v4ip,
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
				klog.Error(err)
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
				util.SubnetNameLabel: subnet,
				util.OvnEipTypeLabel: usage,
				util.EipV4IpLabel:    v4ip,
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
	nat, err := c.getOvnEipNat(ovnEip.Spec.V4Ip)
	if err != nil {
		err := fmt.Errorf("failed to get ovn eip nat")
		klog.Error(err)
		return err
	}
	// nat record all kinds of nat rules using this eip
	klog.V(3).Infof("nat of ovn eip %s is %s", ovnEip.Name, nat)
	if ovnEip.Status.Nat != nat {
		ovnEip.Status.Nat = nat
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

func (c *Controller) natLabelAndAnnoOvnEip(eipName, natName, vpcName string) error {
	cachedEip, err := c.ovnEipsLister.Get(eipName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
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
	nat, err := c.getOvnEipNat(cachedEip.Spec.V4Ip)
	if err != nil {
		err := fmt.Errorf("failed to get ovn eip nat")
		klog.Error(err)
		return err
	}
	if nat != "" {
		err = fmt.Errorf("ovn eip '%s' is still in use, finalizer will not be removed", cachedEip.Name)
		klog.Error(err)
		return err
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

func (c *Controller) getOvnEipNat(eipV4IP string) (string, error) {
	nats := make([]string, 0, 3)
	selector := labels.SelectorFromSet(labels.Set{util.EipV4IpLabel: eipV4IP})
	dnats, err := c.ovnDnatRulesLister.List(selector)
	if err != nil {
		klog.Errorf("failed to get ovn dnats, %v", err)
		return "", err
	}
	if len(dnats) != 0 {
		nats = append(nats, util.DnatUsingEip)
	}
	fips, err := c.ovnFipsLister.List(selector)
	if err != nil {
		klog.Errorf("failed to get ovn fips, %v", err)
		return "", err
	}
	if len(fips) != 0 {
		nats = append(nats, util.FipUsingEip)
	}
	snats, err := c.ovnSnatRulesLister.List(selector)
	if err != nil {
		klog.Errorf("failed to get ovn snats, %v", err)
		return "", err
	}
	if len(snats) != 0 {
		nats = append(nats, util.SnatUsingEip)
	}
	nat := strings.Join(nats, ",")
	return nat, nil
}
