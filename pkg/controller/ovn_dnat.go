package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net"

	"github.com/ovn-org/libovsdb/ovsdb"
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

func (c *Controller) enqueueAddOvnDnatRule(obj interface{}) {
	var (
		key string
		err error
	)

	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.Infof("enqueue add ovn dnat %s", key)
	c.addOvnDnatRuleQueue.Add(key)
}

func (c *Controller) enqueueUpdateOvnDnatRule(oldObj, newObj interface{}) {
	var (
		key string
		err error
	)

	if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	newDnat := newObj.(*kubeovnv1.OvnDnatRule)
	if !newDnat.DeletionTimestamp.IsZero() {
		if len(newDnat.GetFinalizers()) == 0 {
			// avoid delete twice
			return
		}
		klog.Infof("enqueue del ovn dnat %s", key)
		c.delOvnDnatRuleQueue.Add(key)
		return
	}
	oldDnat := oldObj.(*kubeovnv1.OvnDnatRule)
	if oldDnat.Spec.OvnEip != newDnat.Spec.OvnEip {
		c.resetOvnEipQueue.Add(oldDnat.Spec.OvnEip)
	}

	if oldDnat.Spec.OvnEip != newDnat.Spec.OvnEip ||
		oldDnat.Spec.Protocol != newDnat.Spec.Protocol ||
		oldDnat.Spec.IPName != newDnat.Spec.IPName ||
		oldDnat.Spec.InternalPort != newDnat.Spec.InternalPort ||
		oldDnat.Spec.ExternalPort != newDnat.Spec.ExternalPort {
		klog.Infof("enqueue update dnat %s", key)
		c.updateOvnDnatRuleQueue.Add(key)
	}
}

func (c *Controller) enqueueDelOvnDnatRule(obj interface{}) {
	var (
		key string
		err error
	)

	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.Infof("enqueue delete ovn dnat %s", key)
	c.delOvnDnatRuleQueue.Add(key)
}

func (c *Controller) runAddOvnDnatRuleWorker() {
	for c.processNextAddOvnDnatRuleWorkItem() {
	}
}

func (c *Controller) runUpdateOvnDnatRuleWorker() {
	for c.processNextUpdateOvnDnatRuleWorkItem() {
	}
}

func (c *Controller) runDelOvnDnatRuleWorker() {
	for c.processNextDeleteOvnDnatRuleWorkItem() {
	}
}

func (c *Controller) processNextAddOvnDnatRuleWorkItem() bool {
	obj, shutdown := c.addOvnDnatRuleQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addOvnDnatRuleQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addOvnDnatRuleQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddOvnDnatRule(key); err != nil {
			c.addOvnDnatRuleQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addOvnDnatRuleQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateOvnDnatRuleWorkItem() bool {
	obj, shutdown := c.updateOvnDnatRuleQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateOvnDnatRuleQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateOvnDnatRuleQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateOvnDnatRule(key); err != nil {
			c.updateOvnDnatRuleQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateOvnDnatRuleQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteOvnDnatRuleWorkItem() bool {
	obj, shutdown := c.delOvnDnatRuleQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.delOvnDnatRuleQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.delOvnDnatRuleQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDelOvnDnatRule(key); err != nil {
			c.delOvnDnatRuleQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.delOvnDnatRuleQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) isOvnDnatDuplicated(eipName, dnatName, externalPort string) error {
	// check if eip:external port already used
	dnats, err := c.ovnDnatRulesLister.List(labels.SelectorFromSet(labels.Set{
		util.VpcDnatEPortLabel: externalPort,
	}))
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Error(err)
			return err
		}
	}
	if len(dnats) != 0 {
		for _, d := range dnats {
			if d.Name != dnatName && d.Spec.OvnEip == eipName {
				err = fmt.Errorf("failed to create dnat %s, duplicate, same eip %s, same external port '%s' is using by dnat %s", dnatName, eipName, externalPort, d.Name)
				klog.Error(err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) handleAddOvnDnatRule(key string) error {
	cachedDnat, err := c.ovnDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	if cachedDnat.Status.Ready && cachedDnat.Status.V4Ip != "" {
		// already ok
		return nil
	}
	klog.Infof("handle add dnat %s", key)
	// check eip
	eipName := cachedDnat.Spec.OvnEip
	if eipName == "" {
		err := fmt.Errorf("failed to create dnat %s, should set eip", cachedDnat.Name)
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

	var v4Eip, v6Eip, internalV4Ip, internalV6Ip, subnetName, vpcName, ipName string
	v4Eip = cachedEip.Status.V4Ip
	v6Eip = cachedEip.Status.V6Ip
	if v4Eip == "" && v6Eip == "" {
		err := fmt.Errorf("failed to dnat %s, eip %s has no ip", cachedDnat.Name, eipName)
		klog.Error(err)
		return err
	}
	vpcName = cachedDnat.Spec.Vpc
	internalV4Ip = cachedDnat.Spec.V4Ip
	internalV6Ip = cachedDnat.Spec.V6Ip
	ipName = cachedDnat.Spec.IPName
	if ipName != "" {
		if cachedDnat.Spec.IPType == util.Vip {
			internalVip, err := c.virtualIpsLister.Get(cachedDnat.Spec.IPName)
			if err != nil {
				klog.Errorf("failed to get vip %s, %v", cachedDnat.Spec.IPName, err)
				return err
			}
			internalV4Ip = internalVip.Status.V4ip
			internalV6Ip = internalVip.Status.V6ip
			subnetName = internalVip.Spec.Subnet
		} else {
			internalIP, err := c.ipsLister.Get(cachedDnat.Spec.IPName)
			if err != nil {
				klog.Errorf("failed to get ip %s, %v", cachedDnat.Spec.IPName, err)
				return err
			}
			internalV4Ip = internalIP.Spec.V4IPAddress
			internalV6Ip = internalIP.Spec.V6IPAddress
			subnetName = internalIP.Spec.Subnet
		}
		subnet, err := c.subnetsLister.Get(subnetName)
		if err != nil {
			klog.Errorf("failed to get vpc subnet %s, %v", subnetName, err)
			return err
		}
		vpcName = subnet.Spec.Vpc
	}
	if internalV4Ip == "" && internalV6Ip == "" {
		err := fmt.Errorf("failed to create dnat %s, no internal ip", cachedDnat.Name)
		klog.Error(err)
		return err
	}
	if v4Eip == "" && v6Eip == "" {
		err := fmt.Errorf("failed to create dnat %s, no eip", cachedDnat.Name)
		klog.Error(err)
		return err
	}
	if vpcName == "" {
		err := fmt.Errorf("failed to create dnat %s, no vpc", cachedDnat.Name)
		klog.Error(err)
		return err
	}

	var externalPort, internalPort, protocol string
	externalPort = cachedDnat.Spec.ExternalPort
	internalPort = cachedDnat.Spec.InternalPort
	protocol = cachedDnat.Spec.Protocol
	if externalPort == "" {
		err := fmt.Errorf("failed to create dnat %s, no external port", cachedDnat.Name)
		klog.Error(err)
		return err
	}
	if err := c.isOvnDnatDuplicated(eipName, key, cachedDnat.Spec.ExternalPort); err != nil {
		klog.Errorf("failed to create dnat %s, %v", cachedDnat.Name, err)
		return err
	}
	if internalPort == "" {
		err := fmt.Errorf("failed to create dnat %s, no internal port", cachedDnat.Name)
		klog.Error(err)
		return err
	}
	if protocol == "" {
		err := fmt.Errorf("failed to create dnat %s, no protocol", cachedDnat.Name)
		klog.Error(err)
		return err
	}

	if internalV4Ip != "" && v4Eip != "" {
		if err = c.AddDnatRule(vpcName, cachedDnat.Name, v4Eip, internalV4Ip, externalPort, internalPort, protocol); err != nil {
			klog.Errorf("failed to create v4 dnat, %v", err)
			return err
		}
	}
	if internalV6Ip != "" && v6Eip != "" {
		if err = c.AddDnatRule(vpcName, cachedDnat.Name, v6Eip, internalV6Ip, externalPort, internalPort, protocol); err != nil {
			klog.Errorf("failed to create v6 dnat, %v", err)
			return err
		}
	}
	if err := c.handleAddOvnDnatFinalizer(cachedDnat); err != nil {
		klog.Errorf("failed to add finalizer for ovn dnat %s, %v", cachedDnat.Name, err)
		return err
	}
	// patch dnat eip relationship
	if err = c.natLabelAndAnnoOvnEip(eipName, cachedDnat.Name, vpcName); err != nil {
		klog.Errorf("failed to label dnat '%s' in eip %s, %v", cachedDnat.Name, eipName, err)
		return err
	}
	if err = c.patchOvnDnatAnnotations(key, eipName); err != nil {
		klog.Errorf("failed to update annotations for dnat %s, %v", key, err)
		return err
	}
	if err = c.patchOvnDnatStatus(key, vpcName, v4Eip, v6Eip, internalV4Ip, internalV6Ip, true); err != nil {
		klog.Errorf("failed to patch status for dnat %s, %v", key, err)
		return err
	}
	if err = c.patchOvnEipStatus(eipName, true); err != nil {
		klog.Errorf("failed to patch status for eip %s, %v", key, err)
		return err
	}

	return nil
}

func (c *Controller) handleDelOvnDnatRule(key string) error {
	klog.Infof("handle delete ovn dnat %s", key)
	cachedDnat, err := c.ovnDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if cachedDnat.Status.Vpc != "" {
		if cachedDnat.Status.V4Eip != "" && cachedDnat.Status.ExternalPort != "" {
			if err = c.DelDnatRule(cachedDnat.Status.Vpc, cachedDnat.Name,
				cachedDnat.Status.V4Eip, cachedDnat.Status.ExternalPort); err != nil {
				klog.Errorf("failed to delete v4 dnat %s, %v", key, err)
				return err
			}
		}
		if cachedDnat.Status.V6Eip != "" && cachedDnat.Status.ExternalPort != "" {
			if err = c.DelDnatRule(cachedDnat.Status.Vpc, cachedDnat.Name,
				cachedDnat.Status.V6Eip, cachedDnat.Status.ExternalPort); err != nil {
				klog.Errorf("failed to delete v6 dnat %s, %v", key, err)
				return err
			}
		}
	} else {
		err := fmt.Errorf("failed to delete dnat %s, no vpc", cachedDnat.Name)
		klog.Error(err)
		return err
	}
	if err = c.handleDelOvnDnatFinalizer(cachedDnat); err != nil {
		klog.Errorf("failed to remove finalizer for ovn dnat %s, %v", cachedDnat.Name, err)
		return err
	}
	if cachedDnat.Spec.OvnEip != "" {
		c.resetOvnEipQueue.Add(cachedDnat.Spec.OvnEip)
	}
	return nil
}

func (c *Controller) handleUpdateOvnDnatRule(key string) error {
	cachedDnat, err := c.ovnDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if !cachedDnat.Status.Ready {
		// create dnat only in add process, just check to error out here
		klog.Infof("wait ovn dnat %s to be ready only in the handle add process", cachedDnat.Name)
		return nil
	}
	klog.Infof("handle update dnat %s", key)
	// check eip
	eipName := cachedDnat.Spec.OvnEip
	if eipName == "" {
		err := fmt.Errorf("failed to create dnat %s, should set eip", cachedDnat.Name)
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
	var v4Eip, v6Eip, internalV4Ip, internalV6Ip, subnetName, vpcName, ipName string
	v4Eip = cachedEip.Status.V4Ip
	v6Eip = cachedEip.Status.V6Ip
	if v4Eip == "" && v6Eip == "" {
		err := fmt.Errorf("failed to update dnat %s, eip %s has no ip", cachedDnat.Name, eipName)
		klog.Error(err)
		return err
	}
	vpcName = cachedDnat.Spec.Vpc
	internalV4Ip = cachedDnat.Spec.V4Ip
	internalV6Ip = cachedDnat.Spec.V6Ip
	ipName = cachedDnat.Spec.IPName
	if ipName != "" {
		if cachedDnat.Spec.IPType == util.Vip {
			internalVip, err := c.virtualIpsLister.Get(cachedDnat.Spec.IPName)
			if err != nil {
				klog.Errorf("failed to get vip %s, %v", cachedDnat.Spec.IPName, err)
				return err
			}
			internalV4Ip = internalVip.Status.V4ip
			internalV6Ip = internalVip.Status.V6ip
			subnetName = internalVip.Spec.Subnet
		} else {
			internalIP, err := c.ipsLister.Get(cachedDnat.Spec.IPName)
			if err != nil {
				klog.Errorf("failed to get ip %s, %v", cachedDnat.Spec.IPName, err)
				return err
			}
			internalV4Ip = internalIP.Spec.V4IPAddress
			internalV6Ip = internalIP.Spec.V6IPAddress
			subnetName = internalIP.Spec.Subnet
		}
		subnet, err := c.subnetsLister.Get(subnetName)
		if err != nil {
			klog.Errorf("failed to get vpc subnet %s, %v", subnetName, err)
			return err
		}
		vpcName = subnet.Spec.Vpc
	}
	// not support chang
	if cachedDnat.Spec.ExternalPort != cachedDnat.Status.ExternalPort {
		err := fmt.Errorf("not support change external port for dnat %s", cachedDnat.Name)
		klog.Error(err)
		return err
	}
	if cachedDnat.Spec.InternalPort != cachedDnat.Status.InternalPort {
		err := fmt.Errorf("not support change internal port for dnat %s", cachedDnat.Name)
		klog.Error(err)
		return err
	}
	if cachedDnat.Spec.Protocol != cachedDnat.Status.Protocol {
		err := fmt.Errorf("not support change protocol for dnat %s", cachedDnat.Name)
		klog.Error(err)
		return err
	}
	if v4Eip != cachedDnat.Status.V4Eip || v6Eip != cachedDnat.Status.V6Eip {
		err := fmt.Errorf("not support change eip for dnat %s", cachedDnat.Name)
		klog.Error(err)
		return err
	}
	if internalV4Ip != cachedDnat.Status.V4Ip || internalV6Ip != cachedDnat.Status.V6Ip {
		err := fmt.Errorf("not support change internal ip for dnat %s", cachedDnat.Name)
		klog.Error(err)
		return err
	}
	if vpcName != cachedDnat.Status.Vpc {
		err := fmt.Errorf("not support change vpc for dnat %s", cachedDnat.Name)
		klog.Error(err)
		return err
	}
	if cachedDnat.Spec.InternalPort != cachedDnat.Status.InternalPort {
		err := fmt.Errorf("not support change internal port for dnat %s", cachedDnat.Name)
		klog.Error(err)
		return err
	}
	if cachedDnat.Spec.ExternalPort != cachedDnat.Status.ExternalPort {
		err := fmt.Errorf("not support change external port for dnat %s", cachedDnat.Name)
		klog.Error(err)
		return err
	}
	if err := c.isOvnDnatDuplicated(eipName, key, cachedDnat.Spec.ExternalPort); err != nil {
		klog.Errorf("failed to update dnat %s, %v", cachedDnat.Name, err)
		return err
	}
	return nil
}

func (c *Controller) patchOvnDnatAnnotations(key, eipName string) error {
	var (
		oriDnat, dnat *kubeovnv1.OvnDnatRule
		err           error
	)

	if oriDnat, err = c.ovnDnatRulesLister.Get(key); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	dnat = oriDnat.DeepCopy()

	var (
		needUpdateAnno bool
		op             string
	)

	if len(dnat.Annotations) == 0 {
		op = "add"
		dnat.Annotations = map[string]string{
			util.VpcEipAnnotation: eipName,
		}
		needUpdateAnno = true
	}
	if dnat.Annotations[util.VpcEipAnnotation] != eipName {
		op = "replace"
		dnat.Annotations[util.VpcEipAnnotation] = eipName
		needUpdateAnno = true
	}
	if needUpdateAnno {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/annotations", "value": %s }]`
		raw, _ := json.Marshal(dnat.Annotations)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		_, err := c.config.KubeOvnClient.KubeovnV1().OvnDnatRules().Patch(context.Background(), dnat.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
		if err != nil {
			klog.Errorf("failed to patch annotation for ovn dnat %s, %v", dnat.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchOvnDnatStatus(key, vpcName, v4Eip, v6Eip, internalV4Ip, internalV6Ip string, ready bool) error {
	var (
		oriDnat, dnat *kubeovnv1.OvnDnatRule
		err           error
	)
	if oriDnat, err = c.ovnDnatRulesLister.Get(key); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	dnat = oriDnat.DeepCopy()
	var (
		needUpdateLabel = false
		changed         bool
		op              string
	)

	if len(dnat.Labels) == 0 {
		op = "add"
		needUpdateLabel = true
		dnat.Labels = map[string]string{
			util.EipV4IpLabel: v4Eip,
			util.EipV6IpLabel: v6Eip,
		}
	} else if dnat.Labels[util.EipV4IpLabel] != v4Eip {
		op = "replace"
		needUpdateLabel = true
		dnat.Labels[util.EipV4IpLabel] = v4Eip
	}
	if needUpdateLabel {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
		raw, _ := json.Marshal(dnat.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().OvnDnatRules().Patch(context.Background(), dnat.Name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to patch label for ovn dnat %s, %v", dnat.Name, err)
			return err
		}
	}

	if dnat.Status.Ready != ready {
		dnat.Status.Ready = ready
		changed = true
	}
	if vpcName != "" && dnat.Status.Vpc != vpcName {
		dnat.Status.Vpc = vpcName
		changed = true
	}
	if v4Eip != "" && dnat.Status.V4Eip != v4Eip {
		dnat.Status.V4Eip = v4Eip
		changed = true
	}
	if v6Eip != "" && dnat.Status.V6Eip != v6Eip {
		dnat.Status.V4Eip = v6Eip
		changed = true
	}
	if internalV4Ip != "" && dnat.Status.V4Ip != internalV4Ip {
		dnat.Status.V4Ip = internalV4Ip
		changed = true
	}
	if internalV6Ip != "" && dnat.Status.V6Ip != internalV6Ip {
		dnat.Status.V6Ip = internalV6Ip
		changed = true
	}
	if ready && dnat.Spec.Protocol != "" && dnat.Status.Protocol != dnat.Spec.Protocol {
		dnat.Status.Protocol = dnat.Spec.Protocol
		changed = true
	}
	if ready && dnat.Spec.IPName != "" && dnat.Spec.IPName != dnat.Status.IPName {
		dnat.Status.IPName = dnat.Spec.IPName
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
		if _, err = c.config.KubeOvnClient.KubeovnV1().OvnDnatRules().Patch(context.Background(), dnat.Name,
			types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
			klog.Errorf("failed to patch dnat %s, %v", dnat.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) AddDnatRule(vpcName, dnatName, externalIP, internalIP, externalPort, internalPort, protocol string) error {
	var (
		externalEndpoint = net.JoinHostPort(externalIP, externalPort)
		internalEndpoint = net.JoinHostPort(internalIP, internalPort)
		err              error
	)

	if err = c.OVNNbClient.CreateLoadBalancer(dnatName, protocol, ""); err != nil {
		klog.Errorf("create loadBalancer %s: %v", dnatName, err)
		return err
	}

	if err = c.OVNNbClient.LoadBalancerAddVip(dnatName, externalEndpoint, internalEndpoint); err != nil {
		klog.Errorf("add vip %s with backends %s to LB %s: %v", externalEndpoint, internalEndpoint, dnatName, err)
		return err
	}

	if err = c.OVNNbClient.LogicalRouterUpdateLoadBalancers(vpcName, ovsdb.MutateOperationInsert, dnatName); err != nil {
		klog.Errorf("add lb %s to vpc %s: %v", dnatName, vpcName, err)
		return err
	}
	return nil
}

func (c *Controller) DelDnatRule(vpcName, dnatName, externalIP, externalPort string) error {
	var (
		ignoreHealthCheck = true
		externalEndpoint  string
		err               error
	)
	externalEndpoint = net.JoinHostPort(externalIP, externalPort)
	if err = c.OVNNbClient.LoadBalancerDeleteVip(dnatName, externalEndpoint, ignoreHealthCheck); err != nil {
		klog.Errorf("delete loadBalancer vips %s: %v", externalEndpoint, err)
		return err
	}

	if err = c.OVNNbClient.LogicalRouterUpdateLoadBalancers(vpcName, ovsdb.MutateOperationDelete, dnatName); err != nil {
		klog.Errorf("failed to remove lb %s from vpc %s: %v", dnatName, vpcName, err)
		return err
	}

	return nil
}

func (c *Controller) syncOvnDnatFinalizer(cl client.Client) error {
	// migrate depreciated finalizer to new finalizer
	rules := &kubeovnv1.OvnDnatRuleList{}
	return migrateFinalizers(cl, rules, func(i int) (client.Object, client.Object) {
		if i < 0 || i >= len(rules.Items) {
			return nil, nil
		}
		return rules.Items[i].DeepCopy(), rules.Items[i].DeepCopy()
	})
}

func (c *Controller) handleAddOvnDnatFinalizer(cachedDnat *kubeovnv1.OvnDnatRule) error {
	if !cachedDnat.DeletionTimestamp.IsZero() || len(cachedDnat.GetFinalizers()) != 0 {
		return nil
	}

	var (
		newDnat = cachedDnat.DeepCopy()
		patch   []byte
		err     error
	)

	controllerutil.AddFinalizer(newDnat, util.KubeOVNControllerFinalizer)
	if patch, err = util.GenerateMergePatchPayload(cachedDnat, newDnat); err != nil {
		klog.Errorf("failed to generate patch payload for ovn dnat '%s', %v", cachedDnat.Name, err)
		return err
	}

	if _, err = c.config.KubeOvnClient.KubeovnV1().OvnDnatRules().Patch(context.Background(), cachedDnat.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for ovn dnat '%s', %v", cachedDnat.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelOvnDnatFinalizer(cachedDnat *kubeovnv1.OvnDnatRule) error {
	if len(cachedDnat.GetFinalizers()) == 0 {
		return nil
	}
	newDnat := cachedDnat.DeepCopy()
	controllerutil.RemoveFinalizer(newDnat, util.DepreciatedFinalizerName)
	controllerutil.RemoveFinalizer(newDnat, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedDnat, newDnat)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ovn dnat '%s', %v", cachedDnat.Name, err)
		return err
	}

	if _, err = c.config.KubeOvnClient.KubeovnV1().OvnDnatRules().Patch(context.Background(), cachedDnat.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from ovn dnat '%s', %v", cachedDnat.Name, err)
		return err
	}
	return nil
}
