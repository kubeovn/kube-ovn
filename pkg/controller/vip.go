package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddVirtualIP(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.Vip)).String()
	klog.Infof("enqueue add vip %s", key)
	c.addVirtualIPQueue.Add(key)
}

func (c *Controller) enqueueUpdateVirtualIP(oldObj, newObj any) {
	oldVip := oldObj.(*kubeovnv1.Vip)
	newVip := newObj.(*kubeovnv1.Vip)
	key := cache.MetaObjectToName(newVip).String()
	if !newVip.DeletionTimestamp.IsZero() ||
		oldVip.Spec.MacAddress != newVip.Spec.MacAddress ||
		oldVip.Spec.V4ip != newVip.Spec.V4ip ||
		oldVip.Spec.V6ip != newVip.Spec.V6ip {
		klog.Infof("enqueue update vip %s", key)
		c.updateVirtualIPQueue.Add(key)
	}
	if !slices.Equal(oldVip.Spec.Selector, newVip.Spec.Selector) {
		klog.Infof("enqueue update virtual parents for %s", key)
		c.updateVirtualParentsQueue.Add(key)
	}
}

func (c *Controller) enqueueDelVirtualIP(obj any) {
	var vip *kubeovnv1.Vip
	switch t := obj.(type) {
	case *kubeovnv1.Vip:
		vip = t
	case cache.DeletedFinalStateUnknown:
		v, ok := t.Obj.(*kubeovnv1.Vip)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		vip = v
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	key := cache.MetaObjectToName(vip).String()
	klog.Infof("enqueue del vip %s", key)
	c.delVirtualIPQueue.Add(vip.DeepCopy())
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
	// v6 ip address can not use upper case
	if util.ContainsUppercase(vip.Spec.V6ip) {
		err := fmt.Errorf("vip %s v6 ip address %s can not contain upper case", vip.Name, vip.Spec.V6ip)
		klog.Error(err)
		return err
	}
	var macPointer *string
	ipStr := util.GetStringIP(sourceV4Ip, sourceV6Ip)
	if ipStr != "" || vip.Spec.MacAddress != "" {
		if vip.Spec.MacAddress != "" {
			macPointer = &vip.Spec.MacAddress
		}
		v4ip, v6ip, mac, err = c.acquireStaticIPAddress(subnet.Name, vip.Name, portName, ipStr, macPointer)
	} else {
		// Random allocate
		v4ip, v6ip, mac, err = c.acquireIPAddress(subnet.Name, vip.Name, portName)
	}
	if err != nil {
		klog.Error(err)
		return err
	}
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
			err = fmt.Errorf("failed to create lsp %s: %w", portName, err)
			klog.Error(err)
			return err
		}
		if err := c.OVNNbClient.SetLogicalSwitchPortArpProxy(portName, true); err != nil {
			err = fmt.Errorf("failed to enable lsp arp proxy for vip %s: %w", portName, err)
			klog.Error(err)
			return err
		}
	}

	if vip.Spec.Type == util.KubeHostVMVip {
		// k8s host network pod vm use vip for its nic ip
		klog.Infof("create lsp for host network pod vm nic ip %s", vip.Name)
		ipStr := util.GetStringIP(v4ip, v6ip)
		if err := c.OVNNbClient.CreateLogicalSwitchPort(subnet.Name, portName, ipStr, mac, vip.Name, vip.Spec.Namespace, false, "", "", false, nil, subnet.Spec.Vpc); err != nil {
			err = fmt.Errorf("failed to create lsp %s: %w", portName, err)
			klog.Error(err)
			return err
		}
	}
	if err = c.createOrUpdateVipCR(key, vip.Spec.Namespace, subnet.Name, v4ip, v6ip, mac); err != nil {
		klog.Errorf("failed to create or update vip '%s', %v", vip.Name, err)
		return err
	}
	if vip.Spec.Type == util.KubeHostVMVip {
		// vm use the vip as its real ip
		klog.Infof("created host network pod vm ip %s", key)
		return nil
	}
	if err := c.handleUpdateVirtualParents(key); err != nil {
		err := fmt.Errorf("error syncing virtual parents for vip '%s': %s", key, err.Error())
		klog.Error(err)
		return err
	}

	// Trigger subnet status update after all operations complete
	// At this point: IPAM allocated, VIP CR created with labels+status+finalizer
	c.updateSubnetStatusQueue.Add(subnetName)
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
		klog.Infof("handle deleting vip %s", vip.Name)
		// Clean up resources before removing finalizer
		if vip.Spec.Type != "" {
			subnet, err := c.subnetsLister.Get(vip.Spec.Subnet)
			if err != nil {
				klog.Errorf("failed to get subnet %s: %v", vip.Spec.Subnet, err)
				return err
			}
			portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
			klog.Infof("delete vip lsp %s", portName)
			if err := c.OVNNbClient.DeleteLogicalSwitchPort(portName); err != nil {
				err = fmt.Errorf("failed to delete lsp %s: %w", vip.Name, err)
				klog.Error(err)
				return err
			}
		}
		// delete virtual ports
		if err := c.OVNNbClient.DeleteLogicalSwitchPort(vip.Name); err != nil {
			klog.Errorf("delete virtual logical switch port %s from logical switch %s: %v", vip.Name, vip.Spec.Subnet, err)
			return err
		}
		// Release IP from IPAM before removing finalizer
		c.ipam.ReleaseAddressByPod(vip.Name, vip.Spec.Subnet)

		// Now remove finalizer, which will trigger subnet status update
		if err = c.handleDelVipFinalizer(key); err != nil {
			klog.Errorf("failed to handle vip finalizer %v", err)
			return err
		}
		return nil
	}
	// v6 ip address can not use upper case
	if util.ContainsUppercase(vip.Spec.V6ip) {
		err := fmt.Errorf("vip %s v6 ip address %s can not contain upper case", vip.Name, vip.Spec.V6ip)
		klog.Error(err)
		return err
	}
	// not support change
	if vip.Status.Mac != "" && vip.Status.Mac != vip.Spec.MacAddress {
		err = errors.New("not support change mac of vip")
		klog.Errorf("%v", err)
		return err
	}
	if vip.Status.V4ip != "" && vip.Status.V4ip != vip.Spec.V4ip {
		err = errors.New("not support change v4 ip of vip")
		klog.Errorf("%v", err)
		return err
	}
	if vip.Status.V6ip != "" && vip.Status.V6ip != vip.Spec.V6ip {
		err = errors.New("not support change v6 ip of vip")
		klog.Errorf("%v", err)
		return err
	}
	// should update
	if vip.Status.Mac == "" {
		if err = c.createOrUpdateVipCR(key, vip.Spec.Namespace, vip.Spec.Subnet,
			vip.Spec.V4ip, vip.Spec.V6ip, vip.Spec.MacAddress); err != nil {
			klog.Error(err)
			return err
		}
	}
	// Always ensure finalizer is added regardless of Status
	if err = c.handleAddOrUpdateVipFinalizer(key); err != nil {
		klog.Errorf("failed to handle vip finalizer %v", err)
		return err
	}
	return nil
}

func (c *Controller) handleDelVirtualIP(vip *kubeovnv1.Vip) error {
	// Cleanup is now handled in handleUpdateVirtualIP before finalizer removal
	// This function is kept for compatibility with the delete queue
	klog.V(3).Infof("vip %s cleanup already done in update handler", vip.Name)

	// For VIPs deleted without finalizer (race condition or direct deletion),
	// we need to ensure subnet status is updated as a safety net.
	if vip.Spec.Subnet != "" {
		c.updateSubnetStatusQueue.Add(vip.Spec.Subnet)
	}

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
	if cachedVip.Spec.Type == util.KubeHostVMVip {
		// vm use the vip as its real ip
		klog.Infof("created host network pod vm ip %s", key)
		return nil
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
	matchLabels := make(map[string]string)
	for _, v := range cachedVip.Spec.Selector {
		parts := strings.Split(strings.TrimSpace(v), ":")
		if len(parts) != 2 {
			continue
		}
		matchLabels[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{MatchLabels: matchLabels})
	if err != nil {
		klog.Errorf("failed to convert label selector %v: %v", matchLabels, err)
		return err
	}
	pods, err := c.podsLister.Pods(cachedVip.Spec.Namespace).List(selector)
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
		podName := c.getNameByPod(pod)
		podNets, err := c.getPodKubeovnNets(pod)
		if err != nil {
			klog.Errorf("failed to get pod nets %v", err)
		}
		for _, podNet := range podNets {
			// Skip non-OVN subnets that don't create OVN logical switch ports
			if !isOvnSubnet(podNet.Subnet) {
				continue
			}

			if podNet.Subnet.Name == cachedVip.Spec.Subnet {
				portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
				virtualParents = append(virtualParents, portName)
				key := cache.MetaObjectToName(pod).String()
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

func (c *Controller) createOrUpdateVipCR(key, ns, subnet, v4ip, v6ip, mac string) error {
	vipCR, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Create CR with finalizer, labels and status all at once
			if _, err := c.config.KubeOvnClient.KubeovnV1().Vips().Create(context.Background(), &kubeovnv1.Vip{
				ObjectMeta: metav1.ObjectMeta{
					Name:       key,
					Namespace:  ns,
					Finalizers: []string{util.KubeOVNControllerFinalizer},
					Labels: map[string]string{
						util.SubnetNameLabel: subnet,
						util.IPReservedLabel: "",
					},
				},
				Spec: kubeovnv1.VipSpec{
					Namespace:  ns,
					Subnet:     subnet,
					V4ip:       v4ip,
					V6ip:       v6ip,
					MacAddress: mac,
				},
				Status: kubeovnv1.VipStatus{
					V4ip: v4ip,
					V6ip: v6ip,
					Mac:  mac,
				},
			}, metav1.CreateOptions{}); err != nil {
				err := fmt.Errorf("failed to create crd vip '%s', %w", key, err)
				klog.Error(err)
				return err
			}
		} else {
			err := fmt.Errorf("failed to get crd vip '%s', %w", key, err)
			klog.Error(err)
			return err
		}
	} else {
		vip := vipCR.DeepCopy()

		// Ensure labels are set correctly
		if vip.Labels == nil {
			vip.Labels = make(map[string]string)
		}
		vip.Labels[util.SubnetNameLabel] = subnet
		vip.Labels[util.IPReservedLabel] = ""

		if vip.Status.Mac == "" && mac != "" ||
			vip.Status.V4ip == "" && v4ip != "" ||
			vip.Status.V6ip == "" && v6ip != "" {
			// vip spec mac or ip not support to update
			// only set once during creation
			vip.Spec.Namespace = ns
			vip.Spec.V4ip = v4ip
			vip.Spec.V6ip = v6ip
			vip.Spec.MacAddress = mac

			vip.Status.V4ip = v4ip
			vip.Status.V6ip = v6ip
			vip.Status.Mac = mac
			vip.Status.Type = vip.Spec.Type
			// TODO:// Ready = true as subnet.Status.Ready
			// Update with labels, spec and status in one call
			if _, err := c.config.KubeOvnClient.KubeovnV1().Vips().Update(context.Background(), vip, metav1.UpdateOptions{}); err != nil {
				err := fmt.Errorf("failed to update vip '%s', %w", key, err)
				klog.Error(err)
				return err
			}
		}
	}
	// Trigger subnet status update after CR creation or update
	time.Sleep(300 * time.Millisecond)
	c.updateSubnetStatusQueue.Add(subnet)
	return nil
}

func (c *Controller) podReuseVip(vipName, portName string, keepVIP bool) error {
	// when pod use static vip, label vip reserved for pod
	oriVip, err := c.virtualIpsLister.Get(vipName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	vip := oriVip.DeepCopy()
	if vip.Labels == nil {
		vip.Labels = map[string]string{}
	}
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
	c.ipam.ReleaseAddressByPod(vipName, vip.Spec.Subnet)
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
	}
	return nil
}

func (c *Controller) handleAddOrUpdateVipFinalizer(key string) error {
	cachedVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if !cachedVip.DeletionTimestamp.IsZero() {
		return nil
	}
	newVip := cachedVip.DeepCopy()
	controllerutil.RemoveFinalizer(newVip, util.DepreciatedFinalizerName)
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

	// Trigger subnet status update after finalizer is processed as a fallback
	// This handles cases where finalizer was not added during creation
	// AddFinalizer is idempotent, so this is safe even if finalizer already exists
	c.updateSubnetStatusQueue.Add(cachedVip.Spec.Subnet)
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
	if len(cachedVip.GetFinalizers()) == 0 {
		return nil
	}
	newVip := cachedVip.DeepCopy()
	controllerutil.RemoveFinalizer(newVip, util.DepreciatedFinalizerName)
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

	// Trigger subnet status update after finalizer is removed
	// This ensures subnet status reflects the IP release
	// Add delay to ensure API server completes the finalizer removal
	time.Sleep(300 * time.Millisecond)
	c.updateSubnetStatusQueue.Add(cachedVip.Spec.Subnet)
	return nil
}

func (c *Controller) syncVipFinalizer(cl client.Client) error {
	// migrate depreciated finalizer to new finalizer
	vips := &kubeovnv1.VipList{}
	return migrateFinalizers(cl, vips, func(i int) (client.Object, client.Object) {
		if i < 0 || i >= len(vips.Items) {
			return nil, nil
		}
		return vips.Items[i].DeepCopy(), vips.Items[i].DeepCopy()
	})
}
