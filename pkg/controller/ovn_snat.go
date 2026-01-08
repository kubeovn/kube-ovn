package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddOvnSnatRule(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.OvnSnatRule)).String()
	klog.Infof("enqueue add ovn snat %s", key)
	c.addOvnSnatRuleQueue.Add(key)
}

func (c *Controller) enqueueUpdateOvnSnatRule(oldObj, newObj any) {
	newSnat := newObj.(*kubeovnv1.OvnSnatRule)
	key := cache.MetaObjectToName(newSnat).String()
	if !newSnat.DeletionTimestamp.IsZero() {
		if len(newSnat.GetFinalizers()) == 0 {
			// avoid delete twice
			return
		}
		// SNAT with finalizer should be handled in updateOvnSnatRuleQueue
		klog.Infof("enqueue update (deleting) ovn snat %s", key)
		c.updateOvnSnatRuleQueue.Add(key)
		return
	}
	oldSnat := oldObj.(*kubeovnv1.OvnSnatRule)
	if oldSnat.Spec.OvnEip != newSnat.Spec.OvnEip {
		// enqueue to reset eip to be clean
		c.resetOvnEipQueue.Add(oldSnat.Spec.OvnEip)
	}
	if oldSnat.Spec.OvnEip != newSnat.Spec.OvnEip ||
		oldSnat.Spec.VpcSubnet != newSnat.Spec.VpcSubnet ||
		oldSnat.Spec.IPName != newSnat.Spec.IPName {
		klog.Infof("enqueue update snat %s", key)
		c.updateOvnSnatRuleQueue.Add(key)
		return
	}
}

func (c *Controller) enqueueDelOvnSnatRule(obj any) {
	var snat *kubeovnv1.OvnSnatRule
	switch t := obj.(type) {
	case *kubeovnv1.OvnSnatRule:
		snat = t
	case cache.DeletedFinalStateUnknown:
		s, ok := t.Obj.(*kubeovnv1.OvnSnatRule)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		snat = s
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	key := cache.MetaObjectToName(snat).String()
	klog.Infof("enqueue del ovn snat %s", key)
	c.delOvnSnatRuleQueue.Add(key)
}

func (c *Controller) handleAddOvnSnatRule(key string) error {
	cachedSnat, err := c.ovnSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if cachedSnat.Status.Ready {
		// already ok
		return nil
	}
	klog.Infof("handle add ovn snat %s", key)
	// check eip
	eipName := cachedSnat.Spec.OvnEip
	if eipName == "" {
		err := errors.New("failed to create ovn snat rule, should set eip")
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
	var v4Eip, v6Eip, v4IpCidr, v6IpCidr, vpcName, subnetName, ipName string
	v4Eip = cachedEip.Status.V4Ip
	v6Eip = cachedEip.Status.V6Ip
	v4IpCidr = cachedSnat.Spec.V4IpCidr
	v6IpCidr = cachedSnat.Spec.V6IpCidr
	vpcName = cachedSnat.Spec.Vpc
	subnetName = cachedSnat.Spec.VpcSubnet
	if v4IpCidr == "" && subnetName != "" {
		subnet, err := c.subnetsLister.Get(subnetName)
		if err != nil {
			klog.Errorf("failed to get vpc subnet %s, %v", subnetName, err)
			return err
		}
		vpcName = subnet.Spec.Vpc
		v4IpCidr, v6IpCidr = util.SplitStringIP(subnet.Spec.CIDRBlock)
	}
	ipName = cachedSnat.Spec.IPName
	if v4IpCidr == "" && ipName != "" {
		vpcPodIP, err := c.ipsLister.Get(ipName)
		if err != nil {
			klog.Errorf("failed to get pod ip %s, %v", ipName, err)
			return err
		}
		subnet, err := c.subnetsLister.Get(vpcPodIP.Spec.Subnet)
		if err != nil {
			klog.Errorf("failed to get vpc subnet %s, %v", vpcPodIP.Spec.Subnet, err)
			return err
		}
		vpcName = subnet.Spec.Vpc
		v4IpCidr = vpcPodIP.Spec.V4IPAddress
		v6IpCidr = vpcPodIP.Spec.V6IPAddress
	}
	if vpcName == "" {
		err := fmt.Errorf("failed to get vpc for snat %s", cachedSnat.Name)
		klog.Error(err)
		return err
	}
	if v4IpCidr == "" && v6IpCidr == "" {
		err = fmt.Errorf("failed to get subnet or ip cidr for snat %s", key)
		klog.Error(err)
		return err
	}
	if v4Eip == "" && v6Eip == "" {
		err := fmt.Errorf("failed to create snat %s, no external ip", cachedSnat.Name)
		klog.Error(err)
		return err
	}
	// about conflicts: if multi vpc snat use the same eip, if only one gw node exist, it may should work
	if v4IpCidr != "" && v4Eip != "" {
		if err = c.OVNNbClient.AddNat(vpcName, ovnnb.NATTypeSNAT, v4Eip, v4IpCidr, "", "", nil); err != nil {
			klog.Errorf("failed to create v4 snat, %v", err)
			return err
		}
	}
	if v6IpCidr != "" && v6Eip != "" {
		if err = c.OVNNbClient.AddNat(vpcName, ovnnb.NATTypeSNAT, v6Eip, v6IpCidr, "", "", nil); err != nil {
			klog.Errorf("failed to create v6 snat, %v", err)
			return err
		}
	}
	if err := c.handleAddOvnSnatFinalizer(cachedSnat); err != nil {
		klog.Errorf("failed to add finalizer for ovn snat %s, %v", cachedSnat.Name, err)
		return err
	}
	if err = c.natLabelAndAnnoOvnEip(eipName, cachedSnat.Name, vpcName); err != nil {
		klog.Errorf("failed to label snat '%s' in eip %s, %v", cachedSnat.Name, eipName, err)
		return err
	}
	if err = c.patchOvnSnatAnnotation(key, eipName); err != nil {
		klog.Errorf("failed to patch label for snat %s, %v", key, err)
		return err
	}
	if err = c.patchOvnSnatStatus(key, vpcName, v4Eip, v6Eip, v4IpCidr, v6IpCidr, true); err != nil {
		klog.Errorf("failed to update status for snat %s, %v", key, err)
		return err
	}
	if err = c.patchOvnEipStatus(eipName, true); err != nil {
		klog.Errorf("failed to patch status for eip %s, %v", key, err)
		return err
	}
	return nil
}

func (c *Controller) handleUpdateOvnSnatRule(key string) error {
	cachedSnat, err := c.ovnSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	// Handle deletion first (for SNATs with finalizers)
	if !cachedSnat.DeletionTimestamp.IsZero() {
		klog.Infof("handle deleting ovn snat %s", key)
		if cachedSnat.Status.Vpc == "" {
			// Already cleaned, just remove finalizer
			if err = c.handleDelOvnSnatFinalizer(cachedSnat); err != nil {
				klog.Errorf("failed to remove finalizer for ovn snat %s, %v", cachedSnat.Name, err)
				return err
			}
			return nil
		}

		// ovn delete snat
		if cachedSnat.Status.V4Eip != "" && cachedSnat.Status.V4IpCidr != "" {
			if err = c.OVNNbClient.DeleteNat(cachedSnat.Status.Vpc, ovnnb.NATTypeSNAT, cachedSnat.Status.V4Eip, cachedSnat.Status.V4IpCidr); err != nil {
				klog.Errorf("failed to delete v4 snat %s, %v", key, err)
				return err
			}
		}
		if cachedSnat.Status.V6Eip != "" && cachedSnat.Status.V6IpCidr != "" {
			if err = c.OVNNbClient.DeleteNat(cachedSnat.Status.Vpc, ovnnb.NATTypeSNAT, cachedSnat.Status.V6Eip, cachedSnat.Status.V6IpCidr); err != nil {
				klog.Errorf("failed to delete v6 snat %s, %v", key, err)
				return err
			}
		}

		// Remove finalizer
		if err = c.handleDelOvnSnatFinalizer(cachedSnat); err != nil {
			klog.Errorf("failed to remove finalizer for ovn snat %s, %v", cachedSnat.Name, err)
			return err
		}

		// Reset eip
		if cachedSnat.Spec.OvnEip != "" {
			c.resetOvnEipQueue.Add(cachedSnat.Spec.OvnEip)
		}
		return nil
	}

	if !cachedSnat.Status.Ready {
		klog.Infof("wait ovn snat %s to be ready only in the handle add process", cachedSnat.Name)
		return nil
	}
	klog.Infof("handle update ovn snat %s", key)
	// check eip
	eipName := cachedSnat.Spec.OvnEip
	if eipName == "" {
		err := errors.New("failed to create ovn snat rule, should set eip")
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
	var v4Eip, v6Eip, v4IpCidr, v6IpCidr, vpcName, subnetName, ipName string
	v4Eip = cachedEip.Status.V4Ip
	v6Eip = cachedEip.Status.V6Ip
	v4IpCidr = cachedSnat.Spec.V4IpCidr
	v6IpCidr = cachedSnat.Spec.V6IpCidr
	vpcName = cachedSnat.Spec.Vpc
	subnetName = cachedSnat.Spec.VpcSubnet
	if v4IpCidr == "" && subnetName != "" {
		subnet, err := c.subnetsLister.Get(subnetName)
		if err != nil {
			klog.Errorf("failed to get vpc subnet %s, %v", subnetName, err)
			return err
		}
		vpcName = subnet.Spec.Vpc
		v4IpCidr, v6IpCidr = util.SplitStringIP(subnet.Spec.CIDRBlock)
	}
	ipName = cachedSnat.Spec.IPName
	if v4IpCidr == "" && ipName != "" {
		vpcPodIP, err := c.ipsLister.Get(ipName)
		if err != nil {
			klog.Errorf("failed to get pod ip %s, %v", ipName, err)
			return err
		}
		subnet, err := c.subnetsLister.Get(vpcPodIP.Spec.Subnet)
		if err != nil {
			klog.Errorf("failed to get vpc subnet %s, %v", vpcPodIP.Spec.Subnet, err)
			return err
		}
		vpcName = subnet.Spec.Vpc
		v4IpCidr = vpcPodIP.Spec.V4IPAddress
		v6IpCidr = vpcPodIP.Spec.V6IPAddress
	}
	if vpcName == "" {
		err := fmt.Errorf("failed to get vpc for snat %s", cachedSnat.Name)
		klog.Error(err)
		return err
	}
	if vpcName != cachedSnat.Status.Vpc {
		err := fmt.Errorf("failed to update snat %s, vpc changed", key)
		klog.Error(err)
		return err
	}
	if v4IpCidr == "" && v6IpCidr == "" {
		err = fmt.Errorf("failed to get subnet or ip cidr for snat %s", key)
		klog.Error(err)
		return err
	}
	if v4Eip == "" && v6Eip == "" {
		err := fmt.Errorf("failed to update snat %s, no external ip", cachedSnat.Name)
		klog.Error(err)
		return err
	}
	if v4Eip != cachedSnat.Status.V4Eip {
		err := fmt.Errorf("failed to update snat %s, v4 eip changed", key)
		klog.Error(err)
		return err
	}
	if v6Eip != cachedSnat.Status.V6Eip {
		err := fmt.Errorf("failed to update snat %s, v6 eip changed", key)
		klog.Error(err)
		return err
	}
	if v4IpCidr != cachedSnat.Status.V4IpCidr {
		err := fmt.Errorf("failed to update snat %s, v4 ip cidr changed", key)
		klog.Error(err)
		return err
	}
	if v6IpCidr != cachedSnat.Status.V6IpCidr {
		err := fmt.Errorf("failed to update snat %s, v6 ip cidr changed", key)
		klog.Error(err)
		return err
	}
	return nil
}

func (c *Controller) handleDelOvnSnatRule(key string) error {
	klog.Infof("handle delete ovn snat %s", key)
	cachedSnat, err := c.ovnSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	// ovn delete snat
	if cachedSnat.Status.Vpc != "" && cachedSnat.Status.V4Eip != "" && cachedSnat.Status.V4IpCidr != "" {
		if err = c.OVNNbClient.DeleteNat(cachedSnat.Status.Vpc, ovnnb.NATTypeSNAT,
			cachedSnat.Status.V4Eip, cachedSnat.Status.V4IpCidr); err != nil {
			klog.Errorf("failed to delete v4 snat %s, %v", key, err)
			return err
		}
	}
	if cachedSnat.Status.Vpc != "" && cachedSnat.Status.V6Eip != "" && cachedSnat.Status.V6IpCidr != "" {
		if err = c.OVNNbClient.DeleteNat(cachedSnat.Status.Vpc, ovnnb.NATTypeSNAT, cachedSnat.Status.V6Eip, cachedSnat.Status.V6IpCidr); err != nil {
			klog.Errorf("failed to delete v6 snat, %v", err)
			return err
		}
	}
	c.resetOvnEipQueue.Add(cachedSnat.Spec.OvnEip)
	if err = c.handleDelOvnSnatFinalizer(cachedSnat); err != nil {
		klog.Errorf("failed to remove finalizer for ovn snat %s, %v", cachedSnat.Name, err)
		return err
	}
	if cachedSnat.Spec.OvnEip != "" {
		c.resetOvnEipQueue.Add(cachedSnat.Spec.OvnEip)
	}
	return nil
}

func (c *Controller) patchOvnSnatStatus(key, vpc, v4Eip, v6Eip, v4IpCidr, v6IpCidr string, ready bool) error {
	oriSnat, err := c.ovnSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	snat := oriSnat.DeepCopy()
	needUpdateLabel := false
	var op string
	if len(snat.Labels) == 0 {
		op = "add"
		needUpdateLabel = true
		snat.Labels = map[string]string{
			util.EipV4IpLabel: v4Eip,
			util.EipV6IpLabel: v6Eip,
		}
	} else if snat.Labels[util.EipV4IpLabel] != v4Eip {
		op = "replace"
		needUpdateLabel = true
		snat.Labels[util.EipV4IpLabel] = v4Eip
		snat.Labels[util.EipV6IpLabel] = v4Eip
	}
	if needUpdateLabel {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
		raw, _ := json.Marshal(snat.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().OvnSnatRules().Patch(context.Background(), snat.Name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to patch label for ovn snat %s, %v", snat.Name, err)
			return err
		}
	}
	var changed bool
	if snat.Status.Ready != ready {
		snat.Status.Ready = ready
		changed = true
	}
	if vpc != "" && snat.Status.Vpc != vpc {
		snat.Status.Vpc = vpc
		changed = true
	}
	if v4Eip != "" && snat.Status.V4Eip != v4Eip {
		snat.Status.V4Eip = v4Eip
		changed = true
	}
	if v6Eip != "" && snat.Status.V6Eip != v6Eip {
		snat.Status.V6Eip = v6Eip
		changed = true
	}
	if v4IpCidr != "" && snat.Status.V4IpCidr != v4IpCidr {
		snat.Status.V4IpCidr = v4IpCidr
		changed = true
	}
	if v6IpCidr != "" && snat.Status.V6IpCidr != v6IpCidr {
		snat.Status.V6IpCidr = v6IpCidr
		changed = true
	}
	if changed {
		bytes, err := snat.Status.Bytes()
		if err != nil {
			klog.Errorf("failed to marshal snat status, %v", err)
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().OvnSnatRules().Patch(context.Background(), snat.Name,
			types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
			klog.Errorf("failed to patch snat %s, %v", snat.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchOvnSnatAnnotation(key, eipName string) error {
	oriFip, err := c.ovnSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	snat := oriFip.DeepCopy()
	var needUpdateAnno bool
	var op string
	if len(snat.Annotations) == 0 {
		op = "add"
		snat.Annotations = map[string]string{
			util.VpcEipAnnotation: eipName,
		}
		needUpdateAnno = true
	}
	if snat.Annotations[util.VpcEipAnnotation] != eipName {
		op = "replace"
		snat.Annotations[util.VpcEipAnnotation] = eipName
		needUpdateAnno = true
	}
	if needUpdateAnno {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/annotations", "value": %s }]`
		raw, _ := json.Marshal(snat.Annotations)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		_, err := c.config.KubeOvnClient.KubeovnV1().OvnSnatRules().Patch(context.Background(), snat.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
		if err != nil {
			klog.Errorf("failed to patch annotation for ovn snat %s, %v", snat.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) syncOvnSnatFinalizer(cl client.Client) error {
	// migrate depreciated finalizer to new finalizer
	rules := &kubeovnv1.OvnSnatRuleList{}
	return migrateFinalizers(cl, rules, func(i int) (client.Object, client.Object) {
		if i < 0 || i >= len(rules.Items) {
			return nil, nil
		}
		return rules.Items[i].DeepCopy(), rules.Items[i].DeepCopy()
	})
}

func (c *Controller) handleAddOvnSnatFinalizer(cachedSnat *kubeovnv1.OvnSnatRule) error {
	if !cachedSnat.DeletionTimestamp.IsZero() || len(cachedSnat.GetFinalizers()) != 0 {
		return nil
	}
	newSnat := cachedSnat.DeepCopy()
	controllerutil.RemoveFinalizer(newSnat, util.DepreciatedFinalizerName)
	controllerutil.AddFinalizer(newSnat, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedSnat, newSnat)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ovn snat '%s', %v", cachedSnat.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().OvnSnatRules().Patch(context.Background(), cachedSnat.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for ovn snat '%s', %v", cachedSnat.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelOvnSnatFinalizer(cachedSnat *kubeovnv1.OvnSnatRule) error {
	if len(cachedSnat.GetFinalizers()) == 0 {
		return nil
	}
	newSnat := cachedSnat.DeepCopy()
	controllerutil.RemoveFinalizer(newSnat, util.DepreciatedFinalizerName)
	controllerutil.RemoveFinalizer(newSnat, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedSnat, newSnat)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ovn snat '%s', %v", cachedSnat.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().OvnSnatRules().Patch(context.Background(), cachedSnat.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from ovn snat '%s', %v", cachedSnat.Name, err)
		return err
	}

	// Trigger associated EIP to recheck if it can be deleted now
	if cachedSnat.Spec.OvnEip != "" {
		klog.Infof("triggering eip %s update after snat %s deletion", cachedSnat.Spec.OvnEip, cachedSnat.Name)
		c.updateOvnEipQueue.Add(cachedSnat.Spec.OvnEip)
	}

	return nil
}
