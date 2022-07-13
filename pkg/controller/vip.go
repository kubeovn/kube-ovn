package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

func (c *Controller) enqueueAddVirtualIp(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.addVirtualIpQueue.Add(key)
}

func (c *Controller) enqueueUpdateVirtualIp(old, new interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}
	oldVip := old.(*kubeovnv1.Vip)
	newVip := new.(*kubeovnv1.Vip)
	if !newVip.DeletionTimestamp.IsZero() ||
		oldVip.Spec.MacAddress != newVip.Spec.MacAddress ||
		oldVip.Spec.ParentMac != newVip.Spec.ParentMac ||
		oldVip.Spec.ParentV4ip != newVip.Spec.ParentV4ip ||
		oldVip.Spec.V4ip != newVip.Spec.V4ip {
		c.updateVirtualIpQueue.Add(key)
	}
}

func (c *Controller) enqueueDelVirtualIp(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.delVirtualIpQueue.Add(key)
	vipObj := obj.(*kubeovnv1.Vip)
	c.updateSubnetStatusQueue.Add(vipObj.Spec.Subnet)
}

func (c *Controller) runAddVirtualIpWorker() {
	for c.processNextAddVirtualIpWorkItem() {
	}
}

func (c *Controller) runUpdateVirtualIpWorker() {
	for c.processNextUpdateVirtualIpWorkItem() {
	}
}

func (c *Controller) runDelVirtualIpWorker() {
	for c.processNextDeleteVirtualIpWorkItem() {
	}
}

func (c *Controller) processNextAddVirtualIpWorkItem() bool {
	obj, shutdown := c.addVirtualIpQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addVirtualIpQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addVirtualIpQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddVirtualIp(key); err != nil {
			c.addVirtualIpQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addVirtualIpQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateVirtualIpWorkItem() bool {
	obj, shutdown := c.updateVirtualIpQueue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.updateVirtualIpQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateVirtualIpQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateVirtualIp(key); err != nil {
			c.updateVirtualIpQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateVirtualIpQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteVirtualIpWorkItem() bool {
	obj, shutdown := c.delVirtualIpQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.delVirtualIpQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.delVirtualIpQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDelVirtualIp(key); err != nil {
			c.delVirtualIpQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.delVirtualIpQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleAddVirtualIp(key string) error {
	cachedVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if cachedVip.Status.Mac != "" {
		// already ok
		return nil
	}
	vip := cachedVip.DeepCopy()
	klog.V(3).Infof("handle add vip %s", vip.Name)
	var sourceV4Ip, v4ip, v6ip, mac, subnetName, parentV4ip, parentV6ip, parentMac string
	subnetName = vip.Spec.Subnet
	if subnetName == "" {
		return fmt.Errorf("failed to create vip '%s', subnet should be set", key)
	}
	subnet, err := c.subnetsLister.Get(subnetName)
	if err != nil {
		return err
	}
	portName := ovs.PodNameToPortName(vip.Name, vip.Namespace, subnet.Spec.Provider)
	sourceV4Ip = vip.Spec.V4ip
	if sourceV4Ip != "" {
		v4ip, v6ip, mac, err = c.acquireStaticVirtualAddress(subnet.Name, vip.Name, portName, sourceV4Ip)
	} else {
		// Random allocate
		v4ip, v6ip, mac, err = c.acquireVirtualAddress(subnet.Name, vip.Name, portName)
	}
	if err != nil {
		return err
	}
	if vip.Spec.ParentMac != "" {
		parentV4ip = vip.Spec.ParentV4ip
		parentV6ip = vip.Spec.ParentV6ip
		parentMac = vip.Spec.ParentMac
	}
	if err = c.createOrUpdateCrdVip(key, vip.Namespace, subnet.Name, v4ip, v6ip, mac, parentV4ip, parentV6ip, parentMac); err != nil {
		klog.Errorf("failed to create or update vip '%s', %v", vip.Name, err)
		return err
	}
	_, err = c.handleVipFinalizer(vip)
	if err != nil {
		klog.Errorf("failed to handle vip finalizer, %v", err)
		return err
	}
	if err = c.subnetCountVip(subnet); err != nil {
		klog.Errorf("failed to count vip '%s' in subnet, %v", vip.Name, err)
		return err

	}
	return nil
}

func (c *Controller) handleUpdateVirtualIp(key string) error {
	cachedVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	vip := cachedVip.DeepCopy()
	// should delete
	if !vip.DeletionTimestamp.IsZero() {
		// TODO:// clean vip in its parent port aap list
		// klog.V(3).Infof("todo:// remove this vip '%s' from its parent port aap list", key)
		v4ip := ""
		ready := false
		if err = c.patchVipStatus(key, v4ip, ready); err != nil {
			return err
		}
		vip.Status.Ready = false
		_, err = c.handleVipFinalizer(vip)
		if err != nil {
			klog.Errorf("failed to handle vip finalizer %v", err)
			return err
		}
		return nil
	}
	if vip.Status.Mac == "" ||
		vip.Status.Mac != vip.Spec.MacAddress ||
		vip.Status.Pmac != vip.Spec.ParentMac {
		// TODO:// add vip in its parent port aap list
		// klog.V(3).Infof("todo:// add this vip '%s' into its parent port aap list", key)

		if err = c.createOrUpdateCrdVip(key, vip.Namespace, vip.Spec.Subnet,
			vip.Spec.V6ip, vip.Spec.V6ip, vip.Spec.MacAddress,
			vip.Spec.ParentV4ip, vip.Spec.ParentV6ip, vip.Spec.MacAddress); err != nil {
			return err
		}
		ready := true
		if err = c.patchVipStatus(key, vip.Spec.V4ip, ready); err != nil {
			return err
		}
	}
	vip.Status.Ready = true
	_, err = c.handleVipFinalizer(vip)
	if err != nil {
		klog.Errorf("failed to handle vip finalizer %v", err)
		return err
	}
	return nil
}

func (c *Controller) handleDelVirtualIp(key string) error {
	klog.V(3).Infof("release vip %s", key)
	c.ipam.ReleaseAddressByPod(key)
	return nil
}

func (c *Controller) acquireStaticVirtualAddress(subnetName, name, nicName, ip string) (string, string, string, error) {
	checkConflict := true
	var v4ip, v6ip, mac string
	var err error
	for _, ipStr := range strings.Split(ip, ",") {
		if net.ParseIP(ipStr) == nil {
			return "", "", "", fmt.Errorf("failed to parse vip ip %s", ipStr)
		}
	}

	if v4ip, v6ip, mac, err = c.ipam.GetStaticAddress(name, nicName, ip, mac, subnetName, checkConflict); err != nil {
		klog.Errorf("failed to get static virtual ip '%s', mac '%s', subnet '%s', %v", ip, mac, subnetName, err)
		return "", "", "", err
	}
	return v4ip, v6ip, mac, nil
}

func (c *Controller) acquireVirtualAddress(subnetName, name, nicName string) (string, string, string, error) {
	var skippedAddrs []string
	var v4ip, v6ip, mac string
	checkConflict := false
	var err error
	for {
		v4ip, v6ip, mac, err = c.ipam.GetRandomAddress(name, nicName, mac, subnetName, skippedAddrs, checkConflict)
		if err != nil {
			return "", "", "", err
		}

		ipv4OK, ipv6OK, err := c.validatePodIP(name, subnetName, v4ip, v6ip)
		if err != nil {
			return "", "", "", err
		}

		if ipv4OK && ipv6OK {
			return v4ip, v6ip, mac, nil
		}

		if !ipv4OK {
			skippedAddrs = append(skippedAddrs, v4ip)
		}
		if !ipv6OK {
			skippedAddrs = append(skippedAddrs, v6ip)
		}
	}
}

func (c *Controller) subnetCountVip(subnet *kubeovnv1.Subnet) error {
	var err error
	if util.CheckProtocol(subnet.Spec.CIDRBlock) == kubeovnv1.ProtocolDual {
		err = calcDualSubnetStatusIP(subnet, c)
	} else {
		err = calcSubnetStatusIP(subnet, c)
	}
	return err
}

func (c *Controller) createOrUpdateCrdVip(key, ns, subnet, v4ip, v6ip, mac, pV4ip, pV6ip, pmac string) error {
	vipCr, err := c.config.KubeOvnClient.KubeovnV1().Vips().Get(context.Background(), key, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err := c.config.KubeOvnClient.KubeovnV1().Vips().Create(context.Background(), &kubeovnv1.Vip{
				ObjectMeta: metav1.ObjectMeta{
					Name: key,
					Labels: map[string]string{
						util.SubnetNameLabel: subnet,
						util.IpReservedLabel: "",
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
				Status: kubeovnv1.VipStatus{
					Ready: true,
					V4ip:  v4ip,
					V6ip:  v6ip,
					Mac:   mac,
					Pv4ip: pV4ip,
					Pv6ip: pV6ip,
					Pmac:  pmac,
				},
			}, metav1.CreateOptions{})

			if err != nil {
				errMsg := fmt.Errorf("failed to create crd vip '%s', %v", key, err)
				klog.Error(errMsg)
				return errMsg
			}
		} else {
			errMsg := fmt.Errorf("failed to get crd vip '%s', %v", key, err)
			klog.Error(errMsg)
			return errMsg
		}
	} else {
		vip := vipCr.DeepCopy()
		if vip.Spec.MacAddress == "" && mac != "" {
			// vip not support to update, just delete and create
			vip.ObjectMeta.Namespace = ns
			vip.Spec.Namespace = ns
			vip.Spec.Subnet = subnet
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

			_, err := c.config.KubeOvnClient.KubeovnV1().Vips().Update(context.Background(), vip, metav1.UpdateOptions{})
			if err != nil {
				errMsg := fmt.Errorf("failed to update vip '%s', %v", key, err)
				klog.Error(errMsg)
				return errMsg
			}
		}
		var needUpdateLabel bool
		var op string
		if len(vip.Labels) == 0 {
			op = "add"
			vip.Labels = map[string]string{
				util.SubnetNameLabel: subnet,
				util.IpReservedLabel: "",
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
			_, err := c.config.KubeOvnClient.KubeovnV1().Vips().Patch(context.Background(), vip.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
			if err != nil {
				klog.Errorf("failed to patch label for vip '%s', %v", vip.Name, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) handleVipFinalizer(vip *kubeovnv1.Vip) (bool, error) {
	if vip.DeletionTimestamp.IsZero() && !util.ContainsString(vip.Finalizers, util.ControllerName) {
		if len(vip.Finalizers) != 0 {
			return false, nil
		}
		vip.Finalizers = append(vip.Finalizers, util.ControllerName)
		raw, _ := json.Marshal(vip.Finalizers)
		patchPayloadTemplate := `[{ "op": "add", "path": "/metadata/finalizers", "value": %s }]`
		patchPayload := fmt.Sprintf(patchPayloadTemplate, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().Vips().Patch(context.Background(), vip.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			if k8serrors.IsNotFound(err) {
				return true, nil
			}
			klog.Errorf("failed to add finalizer for vip '%s', %v", vip.Name, err)
			return false, err
		}
		// wait local cache ready
		time.Sleep(2 * time.Second)
		return false, nil
	}

	if !vip.DeletionTimestamp.IsZero() && !vip.Status.Ready {
		if len(vip.Finalizers) == 0 {
			return true, nil
		}
		vip.Finalizers = util.RemoveString(vip.Finalizers, util.ControllerName)
		raw, _ := json.Marshal(vip.Finalizers)
		patchPayloadTemplate := `[{ "op": "remove", "path": "/metadata/finalizers", "value": %s }]`
		patchPayload := fmt.Sprintf(patchPayloadTemplate, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().Vips().Patch(context.Background(), vip.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			if k8serrors.IsNotFound(err) {
				return true, nil
			}
			klog.Errorf("failed to remove finalizer from vip '%s', %v", vip.Name, err)
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (c *Controller) patchVipStatus(key, v4ip string, ready bool) error {
	oriVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Errorf("failed to get cached vip '%s', %v", key, err)
			return nil
		}
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

func (c *Controller) podReuseVip(key, portName string, isStsPod bool) error {
	// when pod use static vip, label vip reserved for pod
	oriVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Errorf("failed to get cached vip '%s', %v", key, err)
			return nil
		}
		return err
	}
	vip := oriVip.DeepCopy()
	var op string

	if vip.Labels[util.IpReservedLabel] != "" {
		if isStsPod && vip.Labels[util.IpReservedLabel] == portName {
			return nil
		} else {
			return fmt.Errorf("vip '%s' is in use by pod %s", vip.Name, vip.Labels[util.IpReservedLabel])
		}
	}
	op = "replace"
	vip.Labels[util.IpReservedLabel] = portName
	patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
	raw, _ := json.Marshal(vip.Labels)
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
	if _, err = c.config.KubeOvnClient.KubeovnV1().Vips().Patch(context.Background(), vip.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
		klog.Errorf("failed to patch label for vip '%s', %v", vip.Name, err)
		return err
	}
	c.ipam.ReleaseAddressByPod(key)
	c.updateSubnetStatusQueue.Add(vip.Spec.Subnet)
	return nil
}

func (c *Controller) releaseVip(key string) error {
	// clean vip label when pod delete
	oriVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Errorf("failed to get cached vip '%s', %v", key, err)
			return nil
		}
		return err
	}
	vip := oriVip.DeepCopy()
	var needUpdateLabel bool
	var op string
	if vip.Labels[util.IpReservedLabel] == "" {
		return nil
	} else {
		op = "replace"
		vip.Labels[util.IpReservedLabel] = ""
		needUpdateLabel = true
	}
	if needUpdateLabel {
		klog.V(3).Infof("clean reserved label from vip %s", key)
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
		raw, _ := json.Marshal(vip.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		_, err := c.config.KubeOvnClient.KubeovnV1().Vips().Patch(context.Background(), vip.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
		if err != nil {
			klog.Errorf("failed to patch label for vip '%s', %v", vip.Name, err)
			return err
		}
		if _, _, _, err = c.ipam.GetStaticAddress(key, vip.Name, vip.Spec.V4ip, vip.Spec.MacAddress, vip.Spec.Subnet, false); err != nil {
			klog.Errorf("failed to recover IPAM from VIP CR %s: %v", vip.Name, err)
		}
		c.updateSubnetStatusQueue.Add(vip.Spec.Subnet)
	}
	return nil
}
