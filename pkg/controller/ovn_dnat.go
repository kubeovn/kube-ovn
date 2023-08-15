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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddOvnDnatRule(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.Infof("enqueue add ovn dnat %s", key)
	c.addOvnDnatRuleQueue.Add(key)
}

func (c *Controller) enqueueUpdateOvnDnatRule(old, new interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}
	newDnat := new.(*kubeovnv1.OvnDnatRule)
	if !newDnat.DeletionTimestamp.IsZero() {
		klog.Infof("enqueue del ovn dnat %s", key)
		c.delOvnDnatRuleQueue.Add(key)
		return
	}
	oldDnat := old.(*kubeovnv1.OvnDnatRule)
	if oldDnat.Spec.OvnEip != newDnat.Spec.OvnEip {
		c.resetOvnEipQueue.Add(oldDnat.Spec.OvnEip)
	}

	if oldDnat.Spec.OvnEip != newDnat.Spec.OvnEip ||
		oldDnat.Spec.Protocol != newDnat.Spec.Protocol ||
		oldDnat.Spec.IpName != newDnat.Spec.IpName ||
		oldDnat.Spec.InternalPort != newDnat.Spec.InternalPort ||
		oldDnat.Spec.ExternalPort != newDnat.Spec.ExternalPort {
		klog.Infof("enqueue update dnat %s", key)
		c.updateOvnDnatRuleQueue.Add(key)
	}
}

func (c *Controller) enqueueDelOvnDnatRule(obj interface{}) {
	var key string
	var err error
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

func (c *Controller) isOvnDnatDuplicated(eipName, dnatName, externalPort string) (bool, error) {
	// check if eip:external port already used
	dnats, err := c.ovnDnatRulesLister.List(labels.SelectorFromSet(labels.Set{
		util.VpcDnatEPortLabel: externalPort,
	}))
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Error(err)
			return false, err
		}
	}
	if len(dnats) != 0 {
		for _, d := range dnats {
			if d.Name != dnatName && d.Spec.OvnEip == eipName {
				err = fmt.Errorf("failed to create dnat %s, duplicate, same eip %s, same external port '%s' is using by dnat %s", dnatName, eipName, externalPort, d.Name)
				return true, err
			}
		}
	}
	return false, nil
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
	var internalV4Ip, mac, subnetName string
	if cachedDnat.Spec.IpType == util.Vip {
		internalVip, err := c.virtualIpsLister.Get(cachedDnat.Spec.IpName)
		if err != nil {
			klog.Errorf("failed to get vip %s, %v", cachedDnat.Spec.IpName, err)
			return err
		}
		internalV4Ip = internalVip.Status.V4ip
		mac = internalVip.Status.Mac
		subnetName = internalVip.Spec.Subnet
	} else {
		internalIp, err := c.ipsLister.Get(cachedDnat.Spec.IpName)
		if err != nil {
			klog.Errorf("failed to get ip %s, %v", cachedDnat.Spec.IpName, err)
			return err
		}
		internalV4Ip = internalIp.Spec.V4IPAddress
		mac = internalIp.Spec.MacAddress
		subnetName = internalIp.Spec.Subnet
	}

	eipName := cachedDnat.Spec.OvnEip
	if len(eipName) == 0 {
		err := fmt.Errorf("failed to create dnat %s, should set eip", cachedDnat.Name)
		klog.Error(err)
		return err
	}

	cachedEip, err := c.GetOvnEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}

	if cachedEip.Spec.Type == util.Lsp {
		// eip is using by ecmp nexthop lsp, nat can not use
		err = fmt.Errorf("ovn nat %s can not use type %s eip %s", key, util.Lsp, eipName)
		klog.Error(err)
		return err
	}

	if _, err := c.isOvnDnatDuplicated(eipName, key, cachedDnat.Spec.ExternalPort); err != nil {
		klog.Error("failed to create dnat %s, %v", cachedDnat.Name, err)
		return err
	}

	subnet, err := c.subnetsLister.Get(subnetName)
	if err != nil {
		klog.Errorf("failed to get vpc subnet %s, %v", subnetName, err)
		return err
	}

	if cachedEip.Status.V4Ip == "" || internalV4Ip == "" {
		err := fmt.Errorf("failed to create v4 dnat %s", cachedDnat.Name)
		klog.Error(err)
		return err
	}

	vpcName := subnet.Spec.Vpc
	if err = c.patchOvnDnatStatus(key, vpcName, cachedEip.Status.V4Ip,
		internalV4Ip, mac, false); err != nil {
		klog.Errorf("failed to patch status for dnat %s, %v", key, err)
		return err
	}

	if err = c.handleAddOvnEipFinalizer(cachedEip, util.ControllerName); err != nil {
		klog.Errorf("failed to add finalizer for ovn eip, %v", err)
		return err
	}

	if err = c.AddDnatRule(vpcName, cachedDnat.Name, cachedEip.Status.V4Ip, internalV4Ip,
		cachedDnat.Spec.ExternalPort, cachedDnat.Spec.InternalPort, cachedDnat.Spec.Protocol); err != nil {
		klog.Errorf("failed to create v4 dnat, %v", err)
		return err
	}

	if err := c.handleAddOvnDnatFinalizer(cachedDnat, util.ControllerName); err != nil {
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

	if err = c.patchOvnDnatStatus(key, vpcName, cachedEip.Status.V4Ip,
		internalV4Ip, mac, true); err != nil {
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
	if cachedDnat.Status.Vpc != "" && cachedDnat.Status.V4Eip != "" && cachedDnat.Status.ExternalPort != "" {
		if err = c.DelDnatRule(cachedDnat.Status.Vpc, cachedDnat.Name,
			cachedDnat.Status.V4Eip, cachedDnat.Status.ExternalPort); err != nil {
			klog.Errorf("failed to delete dnat %s, %v", key, err)
			return err
		}
	}
	if err = c.handleDelOvnDnatFinalizer(cachedDnat, util.ControllerName); err != nil {
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

	klog.Infof("handle update dnat %s", key)
	var internalV4Ip, mac, subnetName string
	if cachedDnat.Spec.IpType == util.Vip {
		internalVip, err := c.virtualIpsLister.Get(cachedDnat.Spec.IpName)
		if err != nil {
			klog.Errorf("failed to get vip %s, %v", cachedDnat.Spec.IpName, err)
			return err
		}
		internalV4Ip = internalVip.Status.V4ip
		mac = internalVip.Status.Mac
		subnetName = internalVip.Spec.Subnet
	} else {
		internalIp, err := c.ipsLister.Get(cachedDnat.Spec.IpName)
		if err != nil {
			klog.Errorf("failed to get ip %s, %v", cachedDnat.Spec.IpName, err)
			return err
		}
		internalV4Ip = internalIp.Spec.V4IPAddress
		mac = internalIp.Spec.MacAddress
		subnetName = internalIp.Spec.Subnet
	}

	eipName := cachedDnat.Spec.OvnEip
	if len(eipName) == 0 {
		err := fmt.Errorf("failed to create dnat %s, should set eip", cachedDnat.Name)
		klog.Error(err)
		return err
	}

	cachedEip, err := c.GetOvnEip(eipName)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}

	if cachedEip.Status.Type != "" && cachedEip.Status.Type != util.NatUsingEip {
		err = fmt.Errorf("ovn eip %s type is not %s, can not use", cachedEip.Name, util.NatUsingEip)
		return err
	}

	if _, err := c.isOvnDnatDuplicated(eipName, key, cachedDnat.Spec.ExternalPort); err != nil {
		klog.Error("failed to update dnat %s, %v", cachedDnat.Name, err)
		return err
	}

	subnet, err := c.subnetsLister.Get(subnetName)
	if err != nil {
		klog.Errorf("failed to get vpc subnet %s, %v", subnetName, err)
		return err
	}
	vpcName := subnet.Spec.Vpc

	if cachedEip.Status.V4Ip == "" || internalV4Ip == "" {
		err := fmt.Errorf("failed to create v4 dnat %s", cachedDnat.Name)
		klog.Error(err)
		return err
	}

	if cachedEip.Spec.Type == util.Lsp {
		// eip is using by ecmp nexthop lsp, nat can not use
		err = fmt.Errorf("ovn nat %s can not use type %s eip %s", key, util.Lsp, eipName)
		klog.Error(err)
		return err
	}

	dnat := cachedDnat.DeepCopy()
	if dnat.Status.Ready {
		klog.Infof("dnat change ip, old ip '%s', new ip %s", dnat.Status.V4Ip, cachedEip.Status.V4Ip)
		if err = c.DelDnatRule(dnat.Status.Vpc, dnat.Name, dnat.Status.V4Eip, dnat.Status.ExternalPort); err != nil {
			klog.Errorf("failed to delete dnat, %v", err)
			return err
		}

		if err = c.AddDnatRule(vpcName, dnat.Name, cachedEip.Status.V4Ip, internalV4Ip,
			dnat.Spec.ExternalPort, dnat.Spec.InternalPort, dnat.Spec.Protocol); err != nil {
			klog.Errorf("failed to create dnat, %v", err)
			return err
		}

		if err = c.natLabelAndAnnoOvnEip(eipName, dnat.Name, vpcName); err != nil {
			klog.Errorf("failed to label dnat '%s' in eip %s, %v", dnat.Name, eipName, err)
			return err
		}

		if err = c.patchOvnDnatAnnotations(key, eipName); err != nil {
			klog.Errorf("failed to update annotations for dnat %s, %v", key, err)
			return err
		}

		if err = c.patchOvnDnatStatus(key, vpcName, cachedEip.Status.V4Ip, internalV4Ip, mac, true); err != nil {
			klog.Errorf("failed to patch status for dnat '%s', %v", key, err)
			return err
		}
		return nil
	}
	return nil
}

func (c *Controller) patchOvnDnatAnnotations(key, eipName string) error {
	oriDnat, err := c.ovnDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	dnat := oriDnat.DeepCopy()
	var needUpdateAnno bool
	var op string
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

func (c *Controller) patchOvnDnatStatus(key, vpcName, v4Eip, podIp, podMac string, ready bool) error {
	oriDnat, err := c.ovnDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	dnat := oriDnat.DeepCopy()
	needUpdateLabel := false
	var op string
	if len(dnat.Labels) == 0 {
		op = "add"
		needUpdateLabel = true
		dnat.Labels = map[string]string{
			util.EipV4IpLabel: v4Eip,
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

	var changed bool
	if dnat.Status.Ready != ready {
		dnat.Status.Ready = ready
		changed = true
	}

	if (v4Eip != "" && dnat.Status.V4Eip != v4Eip) ||
		(vpcName != "" && dnat.Status.Vpc != vpcName) ||
		(podIp != "" && dnat.Status.V4Ip != podIp) ||
		(podMac != "" && dnat.Status.MacAddress != podMac) {
		dnat.Status.Vpc = vpcName
		dnat.Status.V4Eip = v4Eip
		dnat.Status.V4Ip = podIp
		dnat.Status.MacAddress = podMac
		changed = true
	}

	if ready && dnat.Spec.Protocol != "" && dnat.Status.Protocol != dnat.Spec.Protocol {
		dnat.Status.Protocol = dnat.Spec.Protocol
		changed = true
	}

	if ready && dnat.Spec.IpName != "" && dnat.Spec.IpName != dnat.Status.IpName {
		dnat.Status.IpName = dnat.Spec.IpName
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

func (c *Controller) AddDnatRule(vpcName, dnatName, externalIp, internalIp, externalPort, internalPort, protocol string) error {
	externalEndpoint := net.JoinHostPort(externalIp, externalPort)
	internalEndpoint := net.JoinHostPort(internalIp, internalPort)

	if err := c.ovnClient.CreateLoadBalancer(dnatName, protocol, ""); err != nil {
		klog.Errorf("create loadBalancer %s: %v", dnatName, err)
		return err
	}

	if err := c.ovnClient.LoadBalancerAddVip(dnatName, externalEndpoint, internalEndpoint); err != nil {
		klog.Errorf("add vip %s with backends %s to LB %s: %v", externalEndpoint, internalEndpoint, dnatName, err)
		return err
	}

	if err := c.ovnClient.LogicalRouterUpdateLoadBalancers(vpcName, ovsdb.MutateOperationInsert, dnatName); err != nil {
		klog.Errorf("add lb %s to vpc %s: %v", dnatName, vpcName, err)
		return err
	}
	return nil
}

func (c *Controller) DelDnatRule(vpcName, dnatName, externalIp, externalPort string) error {
	externalEndpoint := net.JoinHostPort(externalIp, externalPort)

	if err := c.ovnClient.LoadBalancerDeleteVip(dnatName, externalEndpoint); err != nil {
		klog.Errorf("delete loadBalancer vips %s: %v", externalEndpoint, err)
		return err
	}

	if err := c.ovnClient.LogicalRouterUpdateLoadBalancers(vpcName, ovsdb.MutateOperationDelete, dnatName); err != nil {
		klog.Errorf("failed to remove lb %s from vpc %s: %v", dnatName, vpcName, err)
		return err
	}

	return nil
}

func (c *Controller) handleAddOvnDnatFinalizer(cachedDnat *kubeovnv1.OvnDnatRule, finalizer string) error {
	if cachedDnat.DeletionTimestamp.IsZero() {
		if util.ContainsString(cachedDnat.Finalizers, finalizer) {
			return nil
		}
	}
	newDnat := cachedDnat.DeepCopy()
	controllerutil.AddFinalizer(newDnat, finalizer)
	patch, err := util.GenerateMergePatchPayload(cachedDnat, newDnat)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ovn dnat '%s', %v", cachedDnat.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().OvnDnatRules().Patch(context.Background(), cachedDnat.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for ovn dnat '%s', %v", cachedDnat.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelOvnDnatFinalizer(cachedDnat *kubeovnv1.OvnDnatRule, finalizer string) error {
	if len(cachedDnat.Finalizers) == 0 {
		return nil
	}
	var err error
	newDnat := cachedDnat.DeepCopy()
	controllerutil.RemoveFinalizer(newDnat, finalizer)
	patch, err := util.GenerateMergePatchPayload(cachedDnat, newDnat)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ovn dnat '%s', %v", cachedDnat.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().OvnDnatRules().Patch(context.Background(), cachedDnat.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from ovn dnat '%s', %v", cachedDnat.Name, err)
		return err
	}
	return nil
}
