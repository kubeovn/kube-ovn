package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddVirtualIP(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.Infof("enqueue add vip %s", key)
	c.addVirtualIPQueue.Add(key)
}

func (c *Controller) enqueueUpdateVirtualIP(oldObj, newObj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	oldVip := oldObj.(*kubeovnv1.Vip)
	newVip := newObj.(*kubeovnv1.Vip)
	if !newVip.DeletionTimestamp.IsZero() ||
		oldVip.Spec.MacAddress != newVip.Spec.MacAddress ||
		oldVip.Spec.ParentMac != newVip.Spec.ParentMac ||
		oldVip.Spec.ParentV4ip != newVip.Spec.ParentV4ip ||
		oldVip.Spec.V4ip != newVip.Spec.V4ip ||
		oldVip.Spec.V6ip != newVip.Spec.V6ip {
		klog.Infof("enqueue update vip %s", key)
		c.updateVirtualIPQueue.Add(key)
	}
	if !reflect.DeepEqual(oldVip.Spec.Selector, newVip.Spec.Selector) {
		klog.Infof("enqueue update virtual parents for %s", key)
		c.updateVirtualParentsQueue.Add(key)
	}
}

func (c *Controller) enqueueDelVirtualIP(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.Infof("enqueue del vip %s", key)
	vip := obj.(*kubeovnv1.Vip)
	c.delVirtualIPQueue.Add(vip)
}

func (c *Controller) runAddVirtualIPWorker() {
	for c.processNextAddVirtualIPWorkItem() {
	}
}

func (c *Controller) runUpdateVirtualIPWorker() {
	for c.processNextUpdateVirtualIPWorkItem() {
	}
}

func (c *Controller) runUpdateVirtualParentsWorker() {
	for c.processNextUpdateVirtualParentsWorkItem() {
	}
}

func (c *Controller) runDelVirtualIPWorker() {
	for c.processNextDeleteVirtualIPWorkItem() {
	}
}

func (c *Controller) processNextAddVirtualIPWorkItem() bool {
	obj, shutdown := c.addVirtualIPQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addVirtualIPQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addVirtualIPQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddVirtualIP(key); err != nil {
			c.addVirtualIPQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addVirtualIPQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateVirtualIPWorkItem() bool {
	obj, shutdown := c.updateVirtualIPQueue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.updateVirtualIPQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateVirtualIPQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateVirtualIP(key); err != nil {
			c.updateVirtualIPQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateVirtualIPQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateVirtualParentsWorkItem() bool {
	obj, shutdown := c.updateVirtualParentsQueue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.updateVirtualParentsQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateVirtualParentsQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateVirtualParents(key); err != nil {
			c.updateVirtualParentsQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateVirtualParentsQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteVirtualIPWorkItem() bool {
	obj, shutdown := c.delVirtualIPQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.delVirtualIPQueue.Done(obj)
		var vip *kubeovnv1.Vip
		var ok bool
		if vip, ok = obj.(*kubeovnv1.Vip); !ok {
			c.delVirtualIPQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected vip in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDelVirtualIP(vip); err != nil {
			c.delVirtualIPQueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing '%s': %s, requeuing", vip.Name, err.Error())
		}
		c.delVirtualIPQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleAddVirtualIP(key string) error {
	cachedVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if cachedVip.Status.Mac != "" {
		// already ok
		return nil
	}
	klog.V(3).Infof("handle add vip %s", key)
	vip := cachedVip.DeepCopy()
	var sourceV4Ip, sourceV6Ip, v4ip, v6ip, mac, subnetName string
	subnetName = vip.Spec.Subnet
	if subnetName == "" {
		return fmt.Errorf("failed to create vip '%s', subnet should be set", key)
	}
	subnet, err := c.subnetsLister.Get(subnetName)
	if err != nil {
		klog.Errorf("failed to get subnet %s: %v", subnetName, err)
		return err
	}
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
	sourceV4Ip = vip.Spec.V4ip
	sourceV6Ip = vip.Spec.V6ip
	ipStr := util.GetStringIP(sourceV4Ip, sourceV6Ip)
	if ipStr != "" {
		v4ip, v6ip, mac, err = c.acquireStaticIPAddress(subnet.Name, vip.Name, portName, ipStr)
	} else {
		// Random allocate
		v4ip, v6ip, mac, err = c.acquireIPAddress(subnet.Name, vip.Name, portName)
	}

	if err != nil {
		klog.Error(err)
		return err
	}
	var parentV4ip, parentV6ip, parentMac string
	if vip.Spec.Type == util.SwitchLBRuleVip {
		// create a lsp use subnet gw mac, and set it option as arp_proxy
		lrpName := fmt.Sprintf("%s-%s", subnet.Spec.Vpc, subnet.Name)
		klog.Infof("get logical router port %s", lrpName)
		lrp, err := c.OVNNbClient.GetLogicalRouterPort(lrpName, false)
		if err != nil {
			klog.Errorf("failed to get lrp %s: %v", lrpName, err)
			return err
		}
		if lrp.MAC == "" {
			err = fmt.Errorf("logical router port %s should have mac", lrpName)
			klog.Error(err)
			return err
		}
		mac = lrp.MAC
		ipStr := util.GetStringIP(v4ip, v6ip)
		if err := c.OVNNbClient.CreateLogicalSwitchPort(subnet.Name, portName, ipStr, mac, vip.Name, vip.Spec.Namespace, false, "", "", false, nil, subnet.Spec.Vpc); err != nil {
			err = fmt.Errorf("failed to create lsp %s: %v", portName, err)
			klog.Error(err)
			return err
		}
		if err := c.OVNNbClient.SetLogicalSwitchPortArpProxy(portName, true); err != nil {
			err = fmt.Errorf("failed to enable lsp arp proxy for vip %s: %v", portName, err)
			klog.Error(err)
			return err
		}
	}
	if vip.Spec.ParentMac != "" {
		if vip.Spec.Type == util.SwitchLBRuleVip {
			err = fmt.Errorf("invalid usage of vip")
			klog.Error(err)
			return err
		}
		parentV4ip = vip.Spec.ParentV4ip
		parentV6ip = vip.Spec.ParentV6ip
		parentMac = vip.Spec.ParentMac
	}
	if err = c.createOrUpdateVipCR(key, vip.Spec.Namespace, subnet.Name, v4ip, v6ip, mac, parentV4ip, parentV6ip, parentMac); err != nil {
		klog.Errorf("failed to create or update vip '%s', %v", vip.Name, err)
		return err
	}
	if err = c.subnetCountIP(subnet); err != nil {
		klog.Errorf("failed to count vip '%s' in subnet, %v", vip.Name, err)
		return err
	}
	if err := c.handleUpdateVirtualParents(key); err != nil {
		err := fmt.Errorf("error syncing virtual parents for vip '%s': %s", key, err.Error())
		klog.Error(err)
		return err
	}
	return nil
}

func (c *Controller) handleUpdateVirtualIP(key string) error {
	cachedVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	vip := cachedVip.DeepCopy()
	// should delete
	if !vip.DeletionTimestamp.IsZero() {
		if err = c.handleDelVipFinalizer(key); err != nil {
			klog.Errorf("failed to handle vip finalizer %v", err)
			return err
		}
		return nil
	}
	// not support change ip
	if vip.Status.Mac != "" && vip.Status.Mac != vip.Spec.MacAddress ||
		vip.Status.V4ip != "" && vip.Status.V4ip != vip.Spec.V4ip ||
		vip.Status.V6ip != "" && vip.Status.V6ip != vip.Spec.V6ip {
		err = fmt.Errorf("not support change ip of vip")
		klog.Errorf("%v", err)
		return err
	}
	// should update
	if vip.Status.Mac == "" {
		// TODO:// add vip in its parent port aap list
		if err = c.createOrUpdateVipCR(key, vip.Spec.Namespace, vip.Spec.Subnet,
			vip.Spec.V4ip, vip.Spec.V6ip, vip.Spec.MacAddress,
			vip.Spec.ParentV4ip, vip.Spec.ParentV6ip, vip.Spec.MacAddress); err != nil {
			klog.Error(err)
			return err
		}
		ready := true
		if err = c.patchVipStatus(key, vip.Spec.V4ip, ready); err != nil {
			klog.Error(err)
			return err
		}
		if err = c.handleAddVipFinalizer(key); err != nil {
			klog.Errorf("failed to handle vip finalizer %v", err)
			return err
		}
	}
	return nil
}

func (c *Controller) handleDelVirtualIP(vip *kubeovnv1.Vip) error {
	klog.Infof("handle delete vip %s", vip.Name)
	// TODO:// clean vip in its parent port aap list
	if vip.Spec.Type == util.SwitchLBRuleVip {
		subnet, err := c.subnetsLister.Get(vip.Spec.Subnet)
		if err != nil {
			klog.Errorf("failed to get subnet %s: %v", vip.Spec.Subnet, err)
			return err
		}
		portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
		klog.Infof("delete vip arp proxy lsp %s", portName)
		if err := c.OVNNbClient.DeleteLogicalSwitchPort(portName); err != nil {
			err = fmt.Errorf("failed to delete lsp %s: %v", vip.Name, err)
			klog.Error(err)
			return err
		}
	}
	// delete virtual ports
	if err := c.OVNNbClient.DeleteLogicalSwitchPort(vip.Name); err != nil {
		klog.Errorf("delete virtual logical switch port %s from logical switch %s: %v", vip.Name, vip.Spec.Subnet, err)
		return err
	}
	c.ipam.ReleaseAddressByPod(vip.Name, vip.Spec.Subnet)
	c.updateSubnetStatusQueue.Add(vip.Spec.Subnet)
	return nil
}

func (c *Controller) handleUpdateVirtualParents(key string) error {
	cachedVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	// only pods in the same namespace as vip are allowed to use aap
	if (cachedVip.Status.V4ip == "" && cachedVip.Status.V6ip == "") || cachedVip.Spec.Namespace == "" {
		return nil
	}

	// add new virtual port if not exist
	ipStr := util.GetStringIP(cachedVip.Status.V4ip, cachedVip.Status.V6ip)
	if err = c.OVNNbClient.CreateVirtualLogicalSwitchPort(cachedVip.Name, cachedVip.Spec.Subnet, ipStr); err != nil {
		klog.Errorf("create virtual port with vip %s from logical switch %s: %v", cachedVip.Name, cachedVip.Spec.Subnet, err)
		return err
	}

	// update virtual parents
	if cachedVip.Spec.Type == util.SwitchLBRuleVip {
		// switch lb rule vip no need to have virtual parents
		return nil
	}

	// vip cloud use selector to select pods as its virtual parents
	selectors := make(map[string]string)
	for _, v := range cachedVip.Spec.Selector {
		parts := strings.Split(strings.TrimSpace(v), ":")
		if len(parts) != 2 {
			continue
		}
		selectors[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	sel, _ := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{MatchLabels: selectors})
	pods, err := c.podsLister.Pods(cachedVip.Spec.Namespace).List(sel)
	if err != nil {
		klog.Errorf("failed to list pods that meet selector requirements, %v", err)
		return err
	}

	var virtualParents []string
	for _, pod := range pods {
		if pod.Annotations == nil {
			// pod has no annotations
			continue
		}
		if aaps := strings.Split(pod.Annotations[util.AAPsAnnotation], ","); !slices.Contains(aaps, cachedVip.Name) {
			continue
		}
		podNets, err := c.getPodKubeovnNets(pod)
		if err != nil {
			klog.Errorf("failed to get pod nets %v", err)
		}
		for _, podNet := range podNets {
			if podNet.Subnet.Name == cachedVip.Spec.Subnet {
				portName := ovs.PodNameToPortName(pod.Name, pod.Namespace, podNet.ProviderName)
				virtualParents = append(virtualParents, portName)
				key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
				klog.Infof("enqueue update pod security for %s", key)
				c.updatePodSecurityQueue.Add(key)
				break
			}
		}
	}

	parents := strings.Join(virtualParents, ",")
	if err = c.OVNNbClient.SetVirtualLogicalSwitchPortVirtualParents(cachedVip.Name, parents); err != nil {
		klog.Errorf("set vip %s virtual parents %s: %v", cachedVip.Name, parents, err)
		return err
	}

	return nil
}

func (c *Controller) createOrUpdateVipCR(key, ns, subnet, v4ip, v6ip, mac, pV4ip, pV6ip, pmac string) error {
	vipCR, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			if _, err := c.config.KubeOvnClient.KubeovnV1().Vips().Create(context.Background(), &kubeovnv1.Vip{
				ObjectMeta: metav1.ObjectMeta{
					Name: key,
					Labels: map[string]string{
						util.SubnetNameLabel: subnet,
						util.IPReservedLabel: "",
					},
					Namespace: ns,
				},
				Spec: kubeovnv1.VipSpec{
					Namespace:  ns,
					Subnet:     subnet,
					V4ip:       v4ip,
					V6ip:       v6ip,
					MacAddress: mac,
					ParentV4ip: pV4ip,
					ParentV6ip: pV6ip,
					ParentMac:  pmac,
				},
			}, metav1.CreateOptions{}); err != nil {
				err := fmt.Errorf("failed to create crd vip '%s', %v", key, err)
				klog.Error(err)
				return err
			}
		} else {
			err := fmt.Errorf("failed to get crd vip '%s', %v", key, err)
			klog.Error(err)
			return err
		}
	} else {
		vip := vipCR.DeepCopy()
		if vip.Status.Mac == "" && mac != "" {
			// vip not support to update, just delete and create
			vip.Spec.Namespace = ns
			vip.Spec.V4ip = v4ip
			vip.Spec.V6ip = v6ip
			vip.Spec.MacAddress = mac
			vip.Spec.ParentV4ip = pV4ip
			vip.Spec.ParentV6ip = pV6ip
			vip.Spec.ParentMac = pmac

			vip.Status.Ready = true
			vip.Status.V4ip = v4ip
			vip.Status.V6ip = v6ip
			vip.Status.Mac = mac
			vip.Status.Pv4ip = pV4ip
			vip.Status.Pv6ip = pV6ip
			vip.Status.Pmac = pmac
			vip.Status.Type = vip.Spec.Type
			if _, err := c.config.KubeOvnClient.KubeovnV1().Vips().Update(context.Background(), vip, metav1.UpdateOptions{}); err != nil {
				err := fmt.Errorf("failed to update vip '%s', %v", key, err)
				klog.Error(err)
				return err
			}
		}
		var needUpdateLabel bool
		var op string
		if len(vip.Labels) == 0 {
			op = "add"
			vip.Labels = map[string]string{
				util.SubnetNameLabel: subnet,
				util.IPReservedLabel: "",
			}
			needUpdateLabel = true
		}
		if vip.Labels[util.SubnetNameLabel] != subnet {
			op = "replace"
			vip.Labels[util.SubnetNameLabel] = subnet
			needUpdateLabel = true
		}
		if needUpdateLabel {
			patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
			raw, _ := json.Marshal(vip.Labels)
			patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
			if _, err := c.config.KubeOvnClient.KubeovnV1().Vips().Patch(context.Background(), vip.Name, types.JSONPatchType,
				[]byte(patchPayload), metav1.PatchOptions{}); err != nil {
				klog.Errorf("failed to patch label for vip '%s', %v", vip.Name, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) patchVipStatus(key, v4ip string, ready bool) error {
	oriVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	vip := oriVip.DeepCopy()
	var changed bool
	if vip.Status.Ready != ready {
		vip.Status.Ready = ready
		changed = true
	}

	if ready && v4ip != "" && vip.Status.V4ip != v4ip {
		vip.Status.V4ip = v4ip
		changed = true
	}

	if changed {
		if _, err = c.config.KubeOvnClient.KubeovnV1().Vips().Update(context.Background(), vip, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to update status for vip '%s', %v", key, err)
			return err
		}
	}
	return nil
}

func (c *Controller) podReuseVip(key, portName string, keepVIP bool) error {
	// when pod use static vip, label vip reserved for pod
	oriVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	vip := oriVip.DeepCopy()
	var op string

	if vip.Labels[util.IPReservedLabel] != "" {
		if keepVIP && vip.Labels[util.IPReservedLabel] == portName {
			return nil
		}
		return fmt.Errorf("vip '%s' is in use by pod %s", vip.Name, vip.Labels[util.IPReservedLabel])
	}
	op = "replace"
	vip.Labels[util.IPReservedLabel] = portName
	patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
	raw, _ := json.Marshal(vip.Labels)
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
	if _, err = c.config.KubeOvnClient.KubeovnV1().Vips().Patch(context.Background(), vip.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
		klog.Errorf("failed to patch label for vip '%s', %v", vip.Name, err)
		return err
	}
	c.ipam.ReleaseAddressByPod(key, vip.Spec.Subnet)
	c.updateSubnetStatusQueue.Add(vip.Spec.Subnet)
	return nil
}

func (c *Controller) releaseVip(key string) error {
	// clean vip label when pod delete
	oriVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	vip := oriVip.DeepCopy()
	var needUpdateLabel bool
	var op string
	if vip.Labels[util.IPReservedLabel] == "" {
		return nil
	}
	op = "replace"
	vip.Labels[util.IPReservedLabel] = ""
	needUpdateLabel = true
	if needUpdateLabel {
		klog.V(3).Infof("clean reserved label from vip %s", key)
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
		raw, _ := json.Marshal(vip.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().Vips().Patch(context.Background(), vip.Name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to patch label for vip '%s', %v", vip.Name, err)
			return err
		}
		mac := &vip.Status.Mac
		if vip.Status.Mac == "" {
			mac = nil
		}
		if _, _, _, err = c.ipam.GetStaticAddress(key, vip.Name, vip.Status.V4ip, mac, vip.Spec.Subnet, false); err != nil {
			klog.Errorf("failed to recover IPAM from vip CR %s: %v", vip.Name, err)
		}
		c.updateSubnetStatusQueue.Add(vip.Spec.Subnet)
	}
	return nil
}

func (c *Controller) handleAddVipFinalizer(key string) error {
	cachedVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if cachedVip.DeletionTimestamp.IsZero() {
		if slices.Contains(cachedVip.Finalizers, util.KubeOVNControllerFinalizer) {
			return nil
		}
	}
	newVip := cachedVip.DeepCopy()
	controllerutil.AddFinalizer(newVip, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedVip, newVip)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ovn eip '%s', %v", cachedVip.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().Vips().Patch(context.Background(), cachedVip.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for vip '%s', %v", cachedVip.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelVipFinalizer(key string) error {
	cachedVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if len(cachedVip.Finalizers) == 0 {
		return nil
	}
	newVip := cachedVip.DeepCopy()
	controllerutil.RemoveFinalizer(newVip, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedVip, newVip)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ovn eip '%s', %v", cachedVip.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().Vips().Patch(context.Background(), cachedVip.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from vip '%s', %v", cachedVip.Name, err)
		return err
	}
	return nil
}

func (c *Controller) syncVipFinalizer(cl client.Client) error {
	// migrate depreciated finalizer to new finalizer
	vips := &kubeovnv1.VipList{}
	return updateFinalizers(cl, vips, func(i int) (client.Object, client.Object) {
		if i < 0 || i >= len(vips.Items) {
			return nil, nil
		}
		return vips.Items[i].DeepCopy(), vips.Items[i].DeepCopy()
	})
}
