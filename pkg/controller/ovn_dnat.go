package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddOvnDnatRule(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.OvnDnatRule)).String()
	klog.Infof("enqueue add ovn dnat %s", key)
	c.addOvnDnatRuleQueue.Add(key)
}

func (c *Controller) enqueueUpdateOvnDnatRule(oldObj, newObj any) {
	newDnat := newObj.(*kubeovnv1.OvnDnatRule)
	key := cache.MetaObjectToName(newDnat).String()
	if !newDnat.DeletionTimestamp.IsZero() {
		if len(newDnat.GetFinalizers()) == 0 {
			// avoid delete twice
			return
		}
		// DNAT with finalizer should be handled in updateOvnDnatRuleQueue
		klog.Infof("enqueue update (deleting) ovn dnat %s", key)
		c.updateOvnDnatRuleQueue.Add(key)
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

func (c *Controller) enqueueDelOvnDnatRule(obj any) {
	var dnat *kubeovnv1.OvnDnatRule
	switch t := obj.(type) {
	case *kubeovnv1.OvnDnatRule:
		dnat = t
	case cache.DeletedFinalStateUnknown:
		d, ok := t.Obj.(*kubeovnv1.OvnDnatRule)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		dnat = d
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	key := cache.MetaObjectToName(dnat).String()
	klog.Infof("enqueue delete ovn dnat %s", key)
	c.delOvnDnatRuleQueue.Add(key)
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
				err = fmt.Errorf("failed to create dnat %s, duplicate, same eip %s, same external port '%s' is used by dnat %s", dnatName, eipName, externalPort, d.Name)
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
		// eip is used by ecmp nexthop lsp, nat can not use
		err = fmt.Errorf("ovn nat %s can not use type %s eip %s", key, util.OvnEipTypeLSP, eipName)
		klog.Error(err)
		return err
	}

	var v4Eip, v6Eip, internalV4Ip, internalV6Ip, subnetName, vpcName, ipName string
	var isSwitchLBVip bool
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
			isSwitchLBVip = (internalVip.Spec.Type == util.SwitchLBRuleVip)
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

	// For switch_lb_vip DNATs we always re-run to sync backends.
	// For regular DNATs, early exit if already established.
	if !isSwitchLBVip && cachedDnat.Status.Ready && cachedDnat.Status.V4Ip != "" {
		return nil
	}

	klog.Infof("handle add dnat %s", key)

	if isSwitchLBVip {
		// For switch_lb_vip: create router+switch LBs so external traffic reaches
		// pod backends directly, bypassing the switch LB skip that occurs for
		// router→switch traffic (ls_in_pre_lb priority=110).
		if internalV4Ip != "" && v4Eip != "" {
			if err = c.addDnatVipLBRules(cachedDnat.Name, vpcName, subnetName, v4Eip, internalV4Ip, externalPort, internalPort, protocol); err != nil {
				klog.Errorf("failed to add dnat vip lb rules for %s, %v", cachedDnat.Name, err)
				return err
			}
		}
		// store subnet name so deletion can clean up switch LBs without VIP
		if err = c.patchOvnDnatSubnetAnnotation(key, subnetName); err != nil {
			klog.Errorf("failed to patch subnet annotation for dnat %s, %v", key, err)
			return err
		}
	} else {
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
	if cachedDnat.Status.Vpc == "" {
		err := fmt.Errorf("failed to delete dnat %s, no vpc", cachedDnat.Name)
		klog.Error(err)
		return err
	}
	if err = c.cleanOvnDnatOvnResources(cachedDnat); err != nil {
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

	// Handle deletion first (for DNATs with finalizers)
	if !cachedDnat.DeletionTimestamp.IsZero() {
		klog.Infof("handle deleting ovn dnat %s", key)
		if cachedDnat.Status.Vpc == "" {
			// Already cleaned, just remove finalizer
			if err = c.handleDelOvnDnatFinalizer(cachedDnat); err != nil {
				klog.Errorf("failed to remove finalizer for ovn dnat %s, %v", cachedDnat.Name, err)
				return err
			}
			return nil
		}

		if err = c.cleanOvnDnatOvnResources(cachedDnat); err != nil {
			return err
		}

		// Remove finalizer
		if err = c.handleDelOvnDnatFinalizer(cachedDnat); err != nil {
			klog.Errorf("failed to remove finalizer for ovn dnat %s, %v", cachedDnat.Name, err)
			return err
		}

		// Reset eip
		if cachedDnat.Spec.OvnEip != "" {
			c.resetOvnEipQueue.Add(cachedDnat.Spec.OvnEip)
		}
		return nil
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
		// eip is used by ecmp nexthop lsp, nat can not use
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
		dnat.Status.V6Eip = v6Eip
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

	if err = c.OVNNbClient.CreateLoadBalancer(dnatName, protocol); err != nil {
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

// cleanOvnDnatOvnResources removes OVN resources (LBs) created for the DNAT rule.
func (c *Controller) cleanOvnDnatOvnResources(cachedDnat *kubeovnv1.OvnDnatRule) error {
	// Determine if this was a switch_lb_vip DNAT by checking the VIP type.
	// If the VIP is already gone, fall back to the subnet annotation stored at creation.
	isSwitchLBVip := false
	if cachedDnat.Spec.IPType == util.Vip && cachedDnat.Spec.IPName != "" {
		if vip, err := c.virtualIpsLister.Get(cachedDnat.Spec.IPName); err == nil {
			isSwitchLBVip = (vip.Spec.Type == util.SwitchLBRuleVip)
		}
	}
	if !isSwitchLBVip && cachedDnat.Annotations[util.DnatInternalSubnetAnnotation] != "" {
		isSwitchLBVip = true
	}

	if isSwitchLBVip {
		subnetName := cachedDnat.Annotations[util.DnatInternalSubnetAnnotation]
		if err := c.delDnatVipLBRules(cachedDnat.Name, cachedDnat.Status.Vpc, subnetName); err != nil {
			klog.Errorf("failed to delete dnat vip lb rules for %s, %v", cachedDnat.Name, err)
			return err
		}
		return nil
	}

	// Regular DNAT: delete router LBs
	if cachedDnat.Status.V4Eip != "" && cachedDnat.Status.ExternalPort != "" {
		if err := c.DelDnatRule(cachedDnat.Status.Vpc, cachedDnat.Name,
			cachedDnat.Status.V4Eip, cachedDnat.Status.ExternalPort); err != nil {
			klog.Errorf("failed to delete v4 dnat %s, %v", cachedDnat.Name, err)
			return err
		}
	}
	if cachedDnat.Status.V6Eip != "" && cachedDnat.Status.ExternalPort != "" {
		if err := c.DelDnatRule(cachedDnat.Status.Vpc, cachedDnat.Name,
			cachedDnat.Status.V6Eip, cachedDnat.Status.ExternalPort); err != nil {
			klog.Errorf("failed to delete v6 dnat %s, %v", cachedDnat.Name, err)
			return err
		}
	}
	return nil
}

// addDnatVipLBRules creates a router-level LB (for external traffic) and a switch-level LB
// with hairpin_snat_ip (for same-subnet hairpin) for a DNAT rule targeting a switch_lb_vip VIP.
// The switch LB is needed because OVN's ls_in_pre_lb (priority 110) bypasses switch LBs for
// router→switch traffic, making chained LB resolution impossible.
func (c *Controller) addDnatVipLBRules(dnatName, vpcName, subnetName, v4Eip, vipIP, externalPort, internalPort, protocol string) error {
	allSlrs, err := c.switchLBRuleLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list SwitchLBRules: %w", err)
	}
	var slr *kubeovnv1.SwitchLBRule
	for _, s := range allSlrs {
		if s.Spec.Vip == vipIP {
			slr = s
			break
		}
	}
	if slr == nil {
		return fmt.Errorf("no SwitchLBRule found for VIP %s", vipIP)
	}

	intPortNum64, err := strconv.ParseInt(internalPort, 10, 32)
	if err != nil {
		return fmt.Errorf("invalid internal port %s: %w", internalPort, err)
	}
	intPortNum := int32(intPortNum64)

	// Find the matching port in SwitchLBRule (servicePort == internalPort, protocol matches).
	var targetPort int32
	found := false
	proto := strings.ToLower(protocol)
	for _, port := range slr.Spec.Ports {
		portProto := strings.ToLower(port.Protocol)
		if portProto == "" {
			portProto = "tcp"
		}
		if port.Port == intPortNum && portProto == proto {
			targetPort = port.TargetPort
			if targetPort == 0 {
				targetPort = port.Port
			}
			found = true
			break
		}
	}
	if !found {
		// Retry without protocol matching for SLRs that don't specify protocol.
		for _, port := range slr.Spec.Ports {
			if port.Port == intPortNum {
				targetPort = port.TargetPort
				if targetPort == 0 {
					targetPort = port.Port
				}
				found = true
				break
			}
		}
	}
	if !found {
		return fmt.Errorf("no port %s/%s in SwitchLBRule %s for VIP %s", internalPort, protocol, slr.Name, vipIP)
	}

	svcName := generateSvcName(slr.Name)
	eps, err := c.config.KubeClient.CoreV1().Endpoints(slr.Spec.Namespace).Get(context.Background(), svcName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("endpoints %s/%s not found", slr.Spec.Namespace, svcName)
		}
		return fmt.Errorf("get endpoints %s/%s: %w", slr.Spec.Namespace, svcName, err)
	}

	var backends []string
	for _, subset := range eps.Subsets {
		for _, addr := range subset.Addresses {
			backends = append(backends, net.JoinHostPort(addr.IP, strconv.Itoa(int(targetPort))))
		}
	}
	if len(backends) == 0 {
		return fmt.Errorf("no backends for dnat %s", dnatName)
	}

	eipEndpoint := net.JoinHostPort(v4Eip, externalPort)

	// Router LB: external/node → EIP:externalPort → pod backends directly,
	// bypassing the VIP (whose switch LB would be skipped for router→switch traffic).
	routerLBName := dnatName
	if err = c.OVNNbClient.CreateLoadBalancer(routerLBName, proto); err != nil {
		return fmt.Errorf("create router LB %s: %w", routerLBName, err)
	}
	if existingLB, lbErr := c.OVNNbClient.GetLoadBalancer(routerLBName, true); lbErr == nil && existingLB != nil {
		for staleVip := range existingLB.Vips {
			if staleVip != eipEndpoint {
				_ = c.OVNNbClient.LoadBalancerDeleteVip(routerLBName, staleVip, true)
			}
		}
	}
	if err = c.OVNNbClient.LoadBalancerAddVip(routerLBName, eipEndpoint, backends...); err != nil {
		return fmt.Errorf("add vip to router LB %s: %w", routerLBName, err)
	}
	if err = c.OVNNbClient.LogicalRouterUpdateLoadBalancers(vpcName, ovsdb.MutateOperationInsert, routerLBName); err != nil {
		return fmt.Errorf("attach router LB to vpc: %w", err)
	}

	// Switch LB with hairpin_snat_ip: same-subnet pod → EIP:externalPort → pod backends.
	switchLBName := dnatName + "-sw"
	if err = c.OVNNbClient.CreateLoadBalancer(switchLBName, proto); err != nil {
		return fmt.Errorf("create switch LB %s: %w", switchLBName, err)
	}
	if existingLB, lbErr := c.OVNNbClient.GetLoadBalancer(switchLBName, true); lbErr == nil && existingLB != nil {
		for staleVip := range existingLB.Vips {
			if staleVip != eipEndpoint {
				_ = c.OVNNbClient.LoadBalancerDeleteVip(switchLBName, staleVip, true)
			}
		}
	}
	if err = c.OVNNbClient.LoadBalancerAddVip(switchLBName, eipEndpoint, backends...); err != nil {
		return fmt.Errorf("add vip to switch LB %s: %w", switchLBName, err)
	}
	if err = c.OVNNbClient.SetLoadBalancerHairpinSnatIP(switchLBName, v4Eip); err != nil {
		return fmt.Errorf("set hairpin_snat_ip on switch LB %s: %w", switchLBName, err)
	}
	if err = c.OVNNbClient.LogicalSwitchUpdateLoadBalancers(subnetName, ovsdb.MutateOperationInsert, switchLBName); err != nil {
		return fmt.Errorf("attach switch LB to subnet: %w", err)
	}
	return nil
}

// delDnatVipLBRules removes the router LB and switch LB created by addDnatVipLBRules.
func (c *Controller) delDnatVipLBRules(dnatName, vpcName, subnetName string) error {
	routerLBName := dnatName
	switchLBName := dnatName + "-sw"

	if vpcName != "" {
		if err := c.OVNNbClient.LogicalRouterUpdateLoadBalancers(vpcName, ovsdb.MutateOperationDelete, routerLBName); err != nil {
			klog.Warningf("failed to remove router LB %s from vpc %s: %v", routerLBName, vpcName, err)
		}
	}
	if subnetName != "" {
		if err := c.OVNNbClient.LogicalSwitchUpdateLoadBalancers(subnetName, ovsdb.MutateOperationDelete, switchLBName); err != nil {
			klog.Warningf("failed to remove switch LB %s from subnet %s: %v", switchLBName, subnetName, err)
		}
	}
	rln, sln := routerLBName, switchLBName
	if err := c.OVNNbClient.DeleteLoadBalancers(func(lb *ovnnb.LoadBalancer) bool { return lb.Name == rln }); err != nil {
		klog.Warningf("failed to delete router LB %s: %v", routerLBName, err)
	}
	if err := c.OVNNbClient.DeleteLoadBalancers(func(lb *ovnnb.LoadBalancer) bool { return lb.Name == sln }); err != nil {
		klog.Warningf("failed to delete switch LB %s: %v", switchLBName, err)
	}
	return nil
}

// enqueueDnatsForVipIP re-queues all OvnDnatRules that target a switch_lb_vip VIP with
// the given IP address, so their LB backends get refreshed when endpoints change.
func (c *Controller) enqueueDnatsForVipIP(vipIP string) {
	vips, err := c.virtualIpsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vips for ip %s: %v", vipIP, err)
		return
	}
	for _, vip := range vips {
		if vip.Spec.Type != util.SwitchLBRuleVip {
			continue
		}
		if vip.Status.V4ip != vipIP && vip.Spec.V4ip != vipIP &&
			vip.Status.V6ip != vipIP && vip.Spec.V6ip != vipIP {
			continue
		}
		dnats, err := c.ovnDnatRulesLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list ovn dnat rules for vip %s: %v", vip.Name, err)
			return
		}
		for _, dnat := range dnats {
			if dnat.Spec.IPType == util.Vip && dnat.Spec.IPName == vip.Name {
				key := cache.MetaObjectToName(dnat).String()
				klog.Infof("enqueue dnat %s for vip %s endpoint change", key, vip.Name)
				c.addOvnDnatRuleQueue.Add(key)
			}
		}
	}
}

// patchOvnDnatSubnetAnnotation stores the internal subnet name on the DNAT so
// deletion can clean up switch LBs even if the VIP resource is already gone.
func (c *Controller) patchOvnDnatSubnetAnnotation(key, subnetName string) error {
	oriDnat, err := c.ovnDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if oriDnat.Annotations[util.DnatInternalSubnetAnnotation] == subnetName {
		return nil
	}
	dnat := oriDnat.DeepCopy()
	if dnat.Annotations == nil {
		dnat.Annotations = make(map[string]string)
	}
	dnat.Annotations[util.DnatInternalSubnetAnnotation] = subnetName
	patch, err := util.GenerateMergePatchPayload(oriDnat, dnat)
	if err != nil {
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().OvnDnatRules().Patch(context.Background(), oriDnat.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

func (c *Controller) syncOvnDnatFinalizer(cl client.Client) error {
	// migrate deprecated finalizer to new finalizer
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

	controllerutil.RemoveFinalizer(newDnat, util.DeprecatedFinalizerName)
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
	controllerutil.RemoveFinalizer(newDnat, util.DeprecatedFinalizerName)
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

	// Trigger associated EIP to recheck if it can be deleted now
	if cachedDnat.Spec.OvnEip != "" {
		klog.Infof("triggering eip %s update after dnat %s deletion", cachedDnat.Spec.OvnEip, cachedDnat.Name)
		c.updateOvnEipQueue.Add(cachedDnat.Spec.OvnEip)
	}

	return nil
}
