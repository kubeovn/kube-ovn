package controller

import (
	"context"
	"encoding/json"
	"errors"
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

func (c *Controller) enqueueAddOvnFip(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.OvnFip)).String()
	klog.Infof("enqueue add ovn fip %s", key)
	c.addOvnFipQueue.Add(key)
}

func (c *Controller) enqueueUpdateOvnFip(oldObj, newObj any) {
	newFip := newObj.(*kubeovnv1.OvnFip)
	key := cache.MetaObjectToName(newFip).String()
	if !newFip.DeletionTimestamp.IsZero() {
		if len(newFip.GetFinalizers()) == 0 {
			// avoid delete twice
			return
		}
		// FIP with finalizer should be handled in updateOvnFipQueue
		klog.Infof("enqueue update (deleting) ovn fip %s", key)
		c.updateOvnFipQueue.Add(key)
		return
	}
	oldFip := oldObj.(*kubeovnv1.OvnFip)
	if oldFip.Spec.OvnEip != newFip.Spec.OvnEip {
		// enqueue to reset eip to be clean
		klog.Infof("enqueue reset old ovn eip %s", oldFip.Spec.OvnEip)
		c.resetOvnEipQueue.Add(oldFip.Spec.OvnEip)
	}
	if oldFip.Spec.IPName != newFip.Spec.IPName ||
		oldFip.Spec.IPType != newFip.Spec.IPType {
		klog.Infof("enqueue update fip %s", key)
		c.updateOvnFipQueue.Add(key)
		return
	}
}

func (c *Controller) enqueueDelOvnFip(obj any) {
	var fip *kubeovnv1.OvnFip
	switch t := obj.(type) {
	case *kubeovnv1.OvnFip:
		fip = t
	case cache.DeletedFinalStateUnknown:
		f, ok := t.Obj.(*kubeovnv1.OvnFip)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		fip = f
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	key := cache.MetaObjectToName(fip).String()
	klog.Infof("enqueue del ovn fip %s", key)
	c.delOvnFipQueue.Add(key)
}

func (c *Controller) isOvnFipDuplicated(fipName, eipV4IP string) error {
	// check if has another fip using this eip already
	selector := labels.SelectorFromSet(labels.Set{util.EipV4IpLabel: eipV4IP})
	usingFips, err := c.ovnFipsLister.List(selector)
	if err != nil {
		klog.Errorf("failed to get fips, %v", err)
		return err
	}
	for _, uf := range usingFips {
		if uf.Name != fipName {
			err = fmt.Errorf("%s is used by the other fip %s", eipV4IP, uf.Name)
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *Controller) handleAddOvnFip(key string) error {
	cachedFip, err := c.ovnFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	// If the FIP has a deletion timestamp (e.g. after controller restart), delegate to
	// handleUpdateOvnFip which handles the deletion path.
	if !cachedFip.DeletionTimestamp.IsZero() {
		return c.handleUpdateOvnFip(key)
	}
	// check eip
	eipName := cachedFip.Spec.OvnEip
	if eipName == "" {
		err := errors.New("failed to create fip rule, should set eip")
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
	var v4Eip, v6Eip, v4IP, v6IP string
	v4Eip = cachedEip.Status.V4Ip
	v6Eip = cachedEip.Status.V6Ip
	if err = c.isOvnFipDuplicated(key, cachedEip.Spec.V4Ip); err != nil {
		err = fmt.Errorf("failed to add fip %s, %w", key, err)
		klog.Error(err)
		return err
	}

	var mac, subnetName, vpcName, ipName string
	var isSwitchLBVip bool
	vpcName = cachedFip.Spec.Vpc
	v4IP = cachedFip.Spec.V4Ip
	v6IP = cachedFip.Spec.V6Ip
	ipName = cachedFip.Spec.IPName
	if ipName != "" {
		if cachedFip.Spec.IPType == util.Vip {
			internalVip, err := c.virtualIpsLister.Get(ipName)
			if err != nil {
				klog.Errorf("failed to get vip %s, %v", ipName, err)
				return err
			}
			v4IP = internalVip.Status.V4ip
			v6IP = internalVip.Status.V6ip
			subnetName = internalVip.Spec.Subnet
			isSwitchLBVip = (internalVip.Spec.Type == util.SwitchLBRuleVip)
			// though vip lsp has its mac, vip always use its parent lsp nic mac
			// and vip could float to different parent lsp nic
			// all vip its parent lsp acl should allow the vip ip
		} else {
			internalIP, err := c.ipsLister.Get(ipName)
			if err != nil {
				klog.Errorf("failed to get ip %s, %v", ipName, err)
				return err
			}
			v4IP = internalIP.Spec.V4IPAddress
			v6IP = internalIP.Spec.V6IPAddress
			subnetName = internalIP.Spec.Subnet
			mac = internalIP.Spec.MacAddress
			// mac is necessary while using distributed router fip, fip use lsp its mac
			// centralized router fip not need lsp mac, fip use lrp mac
		}
		subnet, err := c.subnetsLister.Get(subnetName)
		if err != nil {
			klog.Errorf("failed to get vpc subnet %s, %v", subnetName, err)
			return err
		}
		vpcName = subnet.Spec.Vpc
		if err = c.isOvnFipDuplicated(key, cachedEip.Spec.V4Ip); err != nil {
			err = fmt.Errorf("failed to update fip %s, %w", key, err)
			klog.Error(err)
			return err
		}
	}

	// For switch_lb_vip FIPs we always re-run to sync backends.
	// For regular FIPs, early exit if already established.
	if !isSwitchLBVip && cachedFip.Status.Ready && cachedFip.Status.V4Ip != "" {
		return nil
	}

	klog.Infof("handle add fip %s", key)

	if vpcName == "" {
		err := fmt.Errorf("failed to create fip %s, no vpc", cachedFip.Name)
		klog.Error(err)
		return err
	}
	if v4IP == "" && v6IP == "" {
		err := fmt.Errorf("failed to create fip %s, no internal ip", cachedFip.Name)
		klog.Error(err)
		return err
	}
	if v4Eip == "" && v6Eip == "" {
		err := fmt.Errorf("failed to create fip %s, no external ip", cachedFip.Name)
		klog.Error(err)
		return err
	}
	if isSwitchLBVip {
		// For switch_lb_vip: create router+switch LBs so external/node traffic
		// can reach the VIP backends even though switch LB is bypassed for
		// router→switch traffic (ls_in_pre_lb priority=110).
		if v4IP != "" && v4Eip != "" {
			if err = c.addFipVipLBRules(key, vpcName, subnetName, v4Eip, v4IP); err != nil {
				klog.Errorf("failed to add fip vip lb rules for %s, %v", key, err)
				return err
			}
		}
		// store subnet name so deletion can clean up switch LBs without VIP
		if err = c.patchOvnFipSubnetAnnotation(key, subnetName); err != nil {
			klog.Errorf("failed to patch subnet annotation for fip %s, %v", key, err)
			return err
		}
	} else {
		// ovn add fip via dnat_and_snat NAT rule
		var stateless bool
		if cachedFip.Spec.Type != "" {
			stateless = (cachedFip.Spec.Type == kubeovnv1.GWDistributedType)
		} else {
			stateless = (c.ExternalGatewayType == kubeovnv1.GWDistributedType)
		}
		options := map[string]string{"stateless": strconv.FormatBool(stateless)}

		// support v4:v4
		if v4IP != "" && v4Eip != "" {
			if err = c.OVNNbClient.AddNat(vpcName, ovnnb.NATTypeDNATAndSNAT, v4Eip, v4IP, mac, cachedFip.Spec.IPName, options); err != nil {
				klog.Errorf("failed to create v4:v4 fip, %v", err)
				return err
			}
		}

		// support v6:v6
		if v6IP != "" && v6Eip != "" {
			if err = c.OVNNbClient.AddNat(vpcName, ovnnb.NATTypeDNATAndSNAT, v6Eip, v6IP, mac, cachedFip.Spec.IPName, options); err != nil {
				klog.Errorf("failed to create v6:v6 fip, %v", err)
				return err
			}
		}

		// support v4:v6
		if v4IP != "" && v6IP == "" && v4Eip == "" && v6Eip != "" {
			if err = c.OVNNbClient.AddNat(vpcName, ovnnb.NATTypeDNATAndSNAT, v6Eip, v4IP, mac, cachedFip.Spec.IPName, options); err != nil {
				klog.Errorf("failed to create v4:v6 fip, %v", err)
				return err
			}
		}

		// support v6:v4
		if v6IP != "" && v4IP == "" && v6Eip == "" && v4Eip != "" {
			if err = c.OVNNbClient.AddNat(vpcName, ovnnb.NATTypeDNATAndSNAT, v4Eip, v6IP, mac, cachedFip.Spec.IPName, options); err != nil {
				klog.Errorf("failed to create v6:v4 fip, %v", err)
				return err
			}
		}
	}

	if err = c.handleAddOvnFipFinalizer(cachedFip); err != nil {
		klog.Errorf("failed to add finalizer for ovn fip, %v", err)
		return err
	}

	// patch fip eip relationship
	if err = c.natLabelAndAnnoOvnEip(eipName, cachedFip.Name, vpcName); err != nil {
		klog.Errorf("failed to label fip '%s' in eip %s, %v", cachedFip.Name, eipName, err)
		return err
	}
	if err = c.patchOvnFipAnnotations(key, eipName); err != nil {
		klog.Errorf("failed to update label for fip %s, %v", key, err)
		return err
	}
	if err = c.patchOvnFipStatus(key, vpcName, v4Eip, v4IP, true); err != nil {
		klog.Errorf("failed to patch status for fip %s, %v", key, err)
		return err
	}
	if err = c.patchOvnEipStatus(eipName, true); err != nil {
		klog.Errorf("failed to patch status for eip %s, %v", key, err)
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
		klog.Error(err)
		return err
	}

	// Handle deletion first (for FIPs with finalizers)
	if !cachedFip.DeletionTimestamp.IsZero() {
		klog.Infof("handle deleting ovn fip %s", key)
		if cachedFip.Status.Vpc == "" {
			// Already cleaned, just remove finalizer
			if err = c.handleDelOvnFipFinalizer(cachedFip); err != nil {
				klog.Errorf("failed to remove finalizer for ovn fip %s, %v", cachedFip.Name, err)
				return err
			}
			return nil
		}

		if err = c.cleanOvnFipOvnResources(cachedFip); err != nil {
			return err
		}

		// Remove finalizer
		if err = c.handleDelOvnFipFinalizer(cachedFip); err != nil {
			klog.Errorf("failed to remove finalizer for ovn fip %s, %v", cachedFip.Name, err)
			return err
		}

		// Reset eip
		if cachedFip.Spec.OvnEip != "" {
			c.resetOvnEipQueue.Add(cachedFip.Spec.OvnEip)
		}
		return nil
	}

	if !cachedFip.Status.Ready {
		// create fip only in add process, just check to error out here
		klog.Infof("wait ovn fip %s to be ready only in the handle add process", cachedFip.Name)
		return nil
	}
	klog.Infof("handle update fip %s", key)
	// check eip
	eipName := cachedFip.Spec.OvnEip
	if eipName == "" {
		err := errors.New("failed to create fip rule, should set eip")
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
	var v4Eip, v6Eip, v4IP, v6IP string
	v4Eip = cachedEip.Status.V4Ip
	v6Eip = cachedEip.Status.V6Ip
	if err = c.isOvnFipDuplicated(key, cachedEip.Spec.V4Ip); err != nil {
		err = fmt.Errorf("failed to add fip %s, %w", key, err)
		klog.Error(err)
		return err
	}

	var subnetName, vpcName, ipName string
	vpcName = cachedFip.Spec.Vpc
	v4IP = cachedFip.Spec.V4Ip
	v6IP = cachedFip.Spec.V6Ip
	ipName = cachedFip.Spec.IPName
	if ipName != "" {
		if cachedFip.Spec.IPType == util.Vip {
			internalVip, err := c.virtualIpsLister.Get(ipName)
			if err != nil {
				klog.Errorf("failed to get vip %s, %v", ipName, err)
				return err
			}
			v4IP = internalVip.Status.V4ip
			v6IP = internalVip.Status.V6ip
			subnetName = internalVip.Spec.Subnet
			// vip lsp has its mac, but vip always use its parent lsp nic mac
			// vip could float to different parent lsp nic
			// all vip its parent lsp acl should allow the vip ip
		} else {
			internalIP, err := c.ipsLister.Get(ipName)
			if err != nil {
				klog.Errorf("failed to get ip %s, %v", ipName, err)
				return err
			}
			v4IP = internalIP.Spec.V4IPAddress
			v6IP = internalIP.Spec.V6IPAddress
			subnetName = internalIP.Spec.Subnet
		}
		subnet, err := c.subnetsLister.Get(subnetName)
		if err != nil {
			klog.Errorf("failed to get vpc subnet %s, %v", subnetName, err)
			return err
		}
		vpcName = subnet.Spec.Vpc
		if err = c.isOvnFipDuplicated(key, cachedEip.Spec.V4Ip); err != nil {
			err = fmt.Errorf("failed to update fip %s, %w", key, err)
			klog.Error(err)
			return err
		}
	}
	if vpcName == "" {
		err := fmt.Errorf("failed to update ovn fip %s, no vpc", cachedFip.Name)
		klog.Error(err)
		return err
	}
	if vpcName != cachedFip.Status.Vpc {
		err := fmt.Errorf("not support change vpc for ovn fip %s", cachedEip.Name)
		klog.Error(err)
		return err
	}
	if v4IP != cachedFip.Status.V4Ip {
		err := fmt.Errorf("not support change v4 ip for ovn fip %s", cachedEip.Name)
		klog.Error(err)
		return err
	}
	if v6IP != cachedFip.Status.V6Ip {
		err := fmt.Errorf("not support change v6 ip for ovn fip %s", cachedEip.Name)
		klog.Error(err)
		return err
	}
	if v4Eip != cachedFip.Status.V4Eip {
		err := fmt.Errorf("not support change eip for ovn fip %s", cachedEip.Name)
		klog.Error(err)
		return err
	}
	if v6Eip != cachedFip.Status.V6Eip {
		err := fmt.Errorf("not support change eip for ovn fip %s", cachedEip.Name)
		klog.Error(err)
		return err
	}
	return nil
}

func (c *Controller) handleDelOvnFip(key string) error {
	klog.Infof("handle del ovn fip %s", key)
	cachedFip, err := c.ovnFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if cachedFip.Status.Vpc == "" {
		return nil
	}
	if err = c.cleanOvnFipOvnResources(cachedFip); err != nil {
		return err
	}
	if err = c.handleDelOvnFipFinalizer(cachedFip); err != nil {
		klog.Errorf("failed to remove finalizer for ovn fip %s, %v", cachedFip.Name, err)
		return err
	}
	//  reset eip
	if cachedFip.Spec.OvnEip != "" {
		c.resetOvnEipQueue.Add(cachedFip.Spec.OvnEip)
	}
	return nil
}

// cleanOvnFipOvnResources removes OVN resources (NAT rules or LBs) created for the FIP.
func (c *Controller) cleanOvnFipOvnResources(cachedFip *kubeovnv1.OvnFip) error {
	// Determine if this was a switch_lb_vip FIP by checking the VIP type.
	// If the VIP is already gone, fall back to the subnet annotation we stored at creation:
	// its presence signals this was a switch_lb_vip FIP.
	isSwitchLBVip := false
	if cachedFip.Spec.IPType == util.Vip && cachedFip.Spec.IPName != "" {
		if vip, err := c.virtualIpsLister.Get(cachedFip.Spec.IPName); err == nil {
			isSwitchLBVip = (vip.Spec.Type == util.SwitchLBRuleVip)
		}
	}
	if !isSwitchLBVip && cachedFip.Annotations[util.FipInternalSubnetAnnotation] != "" {
		isSwitchLBVip = true
	}

	if isSwitchLBVip {
		subnetName := cachedFip.Annotations[util.FipInternalSubnetAnnotation]
		if err := c.delFipVipLBRules(cachedFip.Name, cachedFip.Status.Vpc, subnetName, cachedFip.Status.V4Eip, cachedFip.Status.V4Ip); err != nil {
			klog.Errorf("failed to delete fip vip lb rules for %s, %v", cachedFip.Name, err)
			return err
		}
		return nil
	}

	// Regular FIP: delete dnat_and_snat NAT rules
	if cachedFip.Status.V4Eip != "" && cachedFip.Status.V4Ip != "" {
		if err := c.OVNNbClient.DeleteNat(cachedFip.Status.Vpc, ovnnb.NATTypeDNATAndSNAT, cachedFip.Status.V4Eip, cachedFip.Status.V4Ip); err != nil {
			klog.Errorf("failed to delete v4 fip %s, %v", cachedFip.Name, err)
			return err
		}
	}
	if cachedFip.Status.V6Eip != "" && cachedFip.Status.V6Ip != "" {
		if err := c.OVNNbClient.DeleteNat(cachedFip.Status.Vpc, ovnnb.NATTypeDNATAndSNAT, cachedFip.Status.V6Eip, cachedFip.Status.V6Ip); err != nil {
			klog.Errorf("failed to delete v6 fip %s, %v", cachedFip.Name, err)
			return err
		}
	}
	return nil
}

// addFipVipLBRules creates a router-level LB (for external/node→EIP traffic) and a
// switch-level LB with hairpin_snat_ip (for same-subnet pod→EIP traffic) for a FIP
// that targets a switch_lb_vip VIP.  The switch LB is needed because OVN's
// ls_in_pre_lb (priority 110) bypasses switch LBs for router→switch traffic, so
// the existing switch LB for the VIP would never match external-originated packets.
func (c *Controller) addFipVipLBRules(fipName, vpcName, subnetName, v4Eip, vipIP string) error {
	// Find the SwitchLBRule that owns this VIP IP.
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

	// Get the backing endpoints.
	svcName := generateSvcName(slr.Name)
	eps, err := c.config.KubeClient.CoreV1().Endpoints(slr.Spec.Namespace).Get(context.Background(), svcName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Warningf("endpoints %s/%s not found for fip %s, will retry", slr.Spec.Namespace, svcName, fipName)
			return fmt.Errorf("endpoints %s/%s not found", slr.Spec.Namespace, svcName)
		}
		return fmt.Errorf("get endpoints %s/%s: %w", slr.Spec.Namespace, svcName, err)
	}

	for _, port := range slr.Spec.Ports {
		proto := strings.ToLower(port.Protocol)
		if proto == "" {
			proto = "tcp"
		}
		targetPort := port.TargetPort
		if targetPort == 0 {
			targetPort = port.Port
		}

		var backends []string
		for _, subset := range eps.Subsets {
			for _, addr := range subset.Addresses {
				backends = append(backends, net.JoinHostPort(addr.IP, strconv.Itoa(int(targetPort))))
			}
		}
		if len(backends) == 0 {
			klog.V(3).Infof("no backends yet for fip %s port %d/%s, will retry later", fipName, port.Port, proto)
			return fmt.Errorf("no backends for fip %s port %d/%s", fipName, port.Port, proto)
		}

		eipVip := net.JoinHostPort(v4Eip, strconv.Itoa(int(port.Port)))

		// Router LB: handles external/node → EIP:port → pod backends
		routerLBName := fmt.Sprintf("fip-%s-%s", fipName, proto)
		if err = c.OVNNbClient.CreateLoadBalancer(routerLBName, proto); err != nil {
			return fmt.Errorf("create router LB %s: %w", routerLBName, err)
		}
		// Remove any stale VIPs (from a previous EIP IP) before adding the current one.
		if existingLB, lbErr := c.OVNNbClient.GetLoadBalancer(routerLBName, true); lbErr == nil && existingLB != nil {
			for staleVip := range existingLB.Vips {
				if staleVip != eipVip {
					_ = c.OVNNbClient.LoadBalancerDeleteVip(routerLBName, staleVip, true)
				}
			}
		}
		if err = c.OVNNbClient.LoadBalancerAddVip(routerLBName, eipVip, backends...); err != nil {
			return fmt.Errorf("add vip to router LB %s: %w", routerLBName, err)
		}
		if err = c.OVNNbClient.LogicalRouterUpdateLoadBalancers(vpcName, ovsdb.MutateOperationInsert, routerLBName); err != nil {
			return fmt.Errorf("attach router LB %s to vpc %s: %w", routerLBName, vpcName, err)
		}

		// Switch LB: handles same-subnet pod → EIP:port (hairpin) with SNAT to EIP
		switchLBName := fmt.Sprintf("fip-%s-%s-sw", fipName, proto)
		if err = c.OVNNbClient.CreateLoadBalancer(switchLBName, proto); err != nil {
			return fmt.Errorf("create switch LB %s: %w", switchLBName, err)
		}
		// Remove any stale VIPs (from a previous EIP IP) before adding the current one.
		if existingLB, lbErr := c.OVNNbClient.GetLoadBalancer(switchLBName, true); lbErr == nil && existingLB != nil {
			for staleVip := range existingLB.Vips {
				if staleVip != eipVip {
					_ = c.OVNNbClient.LoadBalancerDeleteVip(switchLBName, staleVip, true)
				}
			}
		}
		if err = c.OVNNbClient.LoadBalancerAddVip(switchLBName, eipVip, backends...); err != nil {
			return fmt.Errorf("add vip to switch LB %s: %w", switchLBName, err)
		}
		if err = c.OVNNbClient.SetLoadBalancerHairpinSnatIP(switchLBName, v4Eip); err != nil {
			return fmt.Errorf("set hairpin_snat_ip on switch LB %s: %w", switchLBName, err)
		}
		if err = c.OVNNbClient.LogicalSwitchUpdateLoadBalancers(subnetName, ovsdb.MutateOperationInsert, switchLBName); err != nil {
			return fmt.Errorf("attach switch LB %s to subnet %s: %w", switchLBName, subnetName, err)
		}
	}

	// Add a dnat_and_snat NAT rule on the VPC router for ARP proxy purposes.
	// The router LB fires first in OVN's pipeline (priority 120 vs NAT priority 100),
	// so this rule does NOT DNAT actual traffic — but it causes the router to respond
	// to ARP requests for v4Eip, which is required for nodes on the external subnet
	// to be able to send packets to the EIP.
	if v4Eip != "" {
		if err = c.OVNNbClient.AddNat(vpcName, ovnnb.NATTypeDNATAndSNAT, v4Eip, vipIP, "", "", nil); err != nil {
			return fmt.Errorf("add arp-proxy NAT rule for fip %s: %w", fipName, err)
		}
	}
	return nil
}

// delFipVipLBRules removes the router LBs, switch LBs, and ARP-proxy NAT rule
// created by addFipVipLBRules.
func (c *Controller) delFipVipLBRules(fipName, vpcName, subnetName, v4Eip, vipIP string) error {
	for _, proto := range []string{"tcp", "udp", "sctp"} {
		routerLBName := fmt.Sprintf("fip-%s-%s", fipName, proto)
		switchLBName := fmt.Sprintf("fip-%s-%s-sw", fipName, proto)

		// Detach from router (ignores non-existent LBs)
		if vpcName != "" {
			if err := c.OVNNbClient.LogicalRouterUpdateLoadBalancers(vpcName, ovsdb.MutateOperationDelete, routerLBName); err != nil {
				klog.Warningf("failed to remove router LB %s from vpc %s: %v", routerLBName, vpcName, err)
			}
		}
		// Detach from switch (ignores non-existent LBs)
		if subnetName != "" {
			if err := c.OVNNbClient.LogicalSwitchUpdateLoadBalancers(subnetName, ovsdb.MutateOperationDelete, switchLBName); err != nil {
				klog.Warningf("failed to remove switch LB %s from subnet %s: %v", switchLBName, subnetName, err)
			}
		}
		// Delete LB objects (no-op if not found)
		rln, sln := routerLBName, switchLBName
		if err := c.OVNNbClient.DeleteLoadBalancers(func(lb *ovnnb.LoadBalancer) bool { return lb.Name == rln }); err != nil {
			klog.Warningf("failed to delete router LB %s: %v", routerLBName, err)
		}
		if err := c.OVNNbClient.DeleteLoadBalancers(func(lb *ovnnb.LoadBalancer) bool { return lb.Name == sln }); err != nil {
			klog.Warningf("failed to delete switch LB %s: %v", switchLBName, err)
		}
	}
	// Remove the ARP-proxy NAT rule (warn if not found, do not fail)
	if vpcName != "" && v4Eip != "" && vipIP != "" {
		if err := c.OVNNbClient.DeleteNat(vpcName, ovnnb.NATTypeDNATAndSNAT, v4Eip, vipIP); err != nil {
			klog.Warningf("failed to delete arp-proxy NAT rule for fip %s: %v", fipName, err)
		}
	}
	return nil
}

// enqueueFipsForVipIP re-queues all OvnFips that target a switch_lb_vip VIP with
// the given IP address, so their LB backends get refreshed when the SwitchLBRule
// endpoints change.
func (c *Controller) enqueueFipsForVipIP(vipIP string) {
	// Find VIP resources with this IP to get their names.
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
		// Find FIPs referencing this VIP by name.
		fips, err := c.ovnFipsLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list ovn fips for vip %s: %v", vip.Name, err)
			return
		}
		for _, fip := range fips {
			if fip.Spec.IPType == util.Vip && fip.Spec.IPName == vip.Name {
				key := cache.MetaObjectToName(fip).String()
				klog.Infof("enqueue fip %s for vip %s endpoint change", key, vip.Name)
				c.addOvnFipQueue.Add(key)
			}
		}
	}
}

// patchOvnFipSubnetAnnotation stores the internal subnet name on the FIP so
// deletion can clean up switch LBs even if the VIP resource is already gone.
func (c *Controller) patchOvnFipSubnetAnnotation(key, subnetName string) error {
	oriFip, err := c.ovnFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if oriFip.Annotations[util.FipInternalSubnetAnnotation] == subnetName {
		return nil
	}
	fip := oriFip.DeepCopy()
	if fip.Annotations == nil {
		fip.Annotations = make(map[string]string)
	}
	fip.Annotations[util.FipInternalSubnetAnnotation] = subnetName
	patch, err := util.GenerateMergePatchPayload(oriFip, fip)
	if err != nil {
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().OvnFips().Patch(context.Background(), oriFip.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

func (c *Controller) patchOvnFipAnnotations(key, eipName string) error {
	oriFip, err := c.ovnFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	fip := oriFip.DeepCopy()
	var needUpdateAnno bool
	var op string
	if len(fip.Annotations) == 0 {
		op = "add"
		fip.Annotations = map[string]string{
			util.VpcEipAnnotation: eipName,
		}
		needUpdateAnno = true
	}
	if fip.Annotations[util.VpcEipAnnotation] != eipName {
		op = "replace"
		fip.Annotations[util.VpcEipAnnotation] = eipName
		needUpdateAnno = true
	}
	if needUpdateAnno {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/annotations", "value": %s }]`
		raw, _ := json.Marshal(fip.Annotations)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().OvnFips().Patch(context.Background(), fip.Name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to patch annotation for ovn fip %s, %v", fip.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchOvnFipStatus(key, vpcName, v4Eip, podIP string, ready bool) error {
	oriFip, err := c.ovnFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	fip := oriFip.DeepCopy()
	needUpdateLabel := false
	var op string
	if len(fip.Labels) == 0 {
		op = "add"
		needUpdateLabel = true
		fip.Labels = map[string]string{
			util.EipV4IpLabel: v4Eip,
		}
	} else if fip.Labels[util.EipV4IpLabel] != v4Eip {
		op = "replace"
		needUpdateLabel = true
		fip.Labels[util.EipV4IpLabel] = v4Eip
	}
	if needUpdateLabel {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
		raw, _ := json.Marshal(fip.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().OvnFips().Patch(context.Background(), fip.Name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to patch label for ovn fip %s, %v", fip.Name, err)
			return err
		}
	}
	var changed bool
	if fip.Status.Ready != ready {
		fip.Status.Ready = ready
		changed = true
	}
	if vpcName != "" && fip.Status.Vpc != vpcName {
		fip.Status.Vpc = vpcName
		changed = true
	}
	if v4Eip != "" && fip.Status.V4Eip != v4Eip {
		fip.Status.V4Eip = v4Eip
		changed = true
	}
	if podIP != "" && fip.Status.V4Ip != podIP {
		fip.Status.V4Ip = podIP
		changed = true
	}
	if changed {
		bytes, err := fip.Status.Bytes()
		if err != nil {
			klog.Error(err)
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

func (c *Controller) GetOvnEip(eipName string) (*kubeovnv1.OvnEip, error) {
	cachedEip, err := c.ovnEipsLister.Get(eipName)
	if err != nil {
		klog.Errorf("failed to get eip %s, %v", eipName, err)
		return nil, err
	}
	if cachedEip.Status.V4Ip == "" && cachedEip.Status.V6Ip == "" {
		err := fmt.Errorf("eip '%s' is not ready, has no ip", eipName)
		klog.Error(err)
		return nil, err
	}
	return cachedEip, nil
}

func (c *Controller) syncOvnFipFinalizer(cl client.Client) error {
	// migrate deprecated finalizer to new finalizer
	fips := &kubeovnv1.OvnFipList{}
	return migrateFinalizers(cl, fips, func(i int) (client.Object, client.Object) {
		if i < 0 || i >= len(fips.Items) {
			return nil, nil
		}
		return fips.Items[i].DeepCopy(), fips.Items[i].DeepCopy()
	})
}

func (c *Controller) handleAddOvnFipFinalizer(cachedFip *kubeovnv1.OvnFip) error {
	if !cachedFip.DeletionTimestamp.IsZero() || len(cachedFip.GetFinalizers()) != 0 {
		return nil
	}
	newFip := cachedFip.DeepCopy()
	controllerutil.RemoveFinalizer(newFip, util.DeprecatedFinalizerName)
	controllerutil.AddFinalizer(newFip, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedFip, newFip)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ovn fip '%s', %v", cachedFip.Name, err)
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
	if len(cachedFip.GetFinalizers()) == 0 {
		return nil
	}
	var err error
	newFip := cachedFip.DeepCopy()
	controllerutil.RemoveFinalizer(newFip, util.DeprecatedFinalizerName)
	controllerutil.RemoveFinalizer(newFip, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedFip, newFip)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ovn fip '%s', %v", cachedFip.Name, err)
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

	// Trigger associated EIP to recheck if it can be deleted now
	if cachedFip.Spec.OvnEip != "" {
		klog.Infof("triggering eip %s update after fip %s deletion", cachedFip.Spec.OvnEip, cachedFip.Name)
		c.updateOvnEipQueue.Add(cachedFip.Spec.OvnEip)
	}

	return nil
}
