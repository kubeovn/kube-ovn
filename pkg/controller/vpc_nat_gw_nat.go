package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddIptablesFip(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.IptablesFIPRule)).String()
	klog.V(3).Infof("enqueue add iptables fip %s", key)
	c.addIptablesFipQueue.Add(key)
}

func (c *Controller) enqueueUpdateIptablesFip(oldObj, newObj any) {
	oldFip := oldObj.(*kubeovnv1.IptablesFIPRule)
	newFip := newObj.(*kubeovnv1.IptablesFIPRule)
	key := cache.MetaObjectToName(newFip).String()
	if !newFip.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("enqueue update to clean fip %s", key)
		c.updateIptablesFipQueue.Add(key)
		return
	}
	if newFip.Spec.EIP == "" || newFip.Spec.InternalIP == "" {
		klog.Errorf("skip enqueue fip %s: incomplete spec (eip=%q, internalIP=%q)", key, newFip.Spec.EIP, newFip.Spec.InternalIP)
		return
	}
	if oldFip.Status.V4ip != newFip.Status.V4ip ||
		oldFip.Spec.EIP != newFip.Spec.EIP ||
		oldFip.Status.Redo != newFip.Status.Redo ||
		oldFip.Spec.InternalIP != newFip.Spec.InternalIP {
		klog.V(3).Infof("enqueue update fip %s", key)
		c.updateIptablesFipQueue.Add(key)
		return
	}
}

func (c *Controller) enqueueDelIptablesFip(obj any) {
	var fip *kubeovnv1.IptablesFIPRule
	switch t := obj.(type) {
	case *kubeovnv1.IptablesFIPRule:
		fip = t
	case cache.DeletedFinalStateUnknown:
		f, ok := t.Obj.(*kubeovnv1.IptablesFIPRule)
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
	klog.V(3).Infof("enqueue delete iptables fip %s", key)
	c.delIptablesFipQueue.Add(key)
}

func (c *Controller) enqueueAddIptablesDnatRule(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.IptablesDnatRule)).String()
	klog.V(3).Infof("enqueue add iptables dnat %s", key)
	c.addIptablesDnatRuleQueue.Add(key)
}

func (c *Controller) enqueueUpdateIptablesDnatRule(oldObj, newObj any) {
	oldDnat := oldObj.(*kubeovnv1.IptablesDnatRule)
	newDnat := newObj.(*kubeovnv1.IptablesDnatRule)
	key := cache.MetaObjectToName(newDnat).String()
	if !newDnat.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("enqueue update to clean dnat %s", key)
		c.updateIptablesDnatRuleQueue.Add(key)
		return
	}
	if newDnat.Spec.EIP == "" || newDnat.Spec.ExternalPort == "" || newDnat.Spec.Protocol == "" ||
		newDnat.Spec.InternalIP == "" || newDnat.Spec.InternalPort == "" {
		klog.Errorf("skip enqueue dnat %s: incomplete spec (eip=%q, externalPort=%q, protocol=%q, internalIP=%q, internalPort=%q)",
			key, newDnat.Spec.EIP, newDnat.Spec.ExternalPort, newDnat.Spec.Protocol, newDnat.Spec.InternalIP, newDnat.Spec.InternalPort)
		return
	}
	if oldDnat.Status.V4ip != newDnat.Status.V4ip ||
		oldDnat.Spec.EIP != newDnat.Spec.EIP ||
		oldDnat.Status.Redo != newDnat.Status.Redo ||
		oldDnat.Spec.Protocol != newDnat.Spec.Protocol ||
		oldDnat.Spec.InternalIP != newDnat.Spec.InternalIP ||
		oldDnat.Spec.ExternalPort != newDnat.Spec.ExternalPort ||
		oldDnat.Spec.InternalPort != newDnat.Spec.InternalPort {
		klog.V(3).Infof("enqueue update dnat %s", key)
		c.updateIptablesDnatRuleQueue.Add(key)
		return
	}
}

func (c *Controller) enqueueDelIptablesDnatRule(obj any) {
	var dnat *kubeovnv1.IptablesDnatRule
	switch t := obj.(type) {
	case *kubeovnv1.IptablesDnatRule:
		dnat = t
	case cache.DeletedFinalStateUnknown:
		d, ok := t.Obj.(*kubeovnv1.IptablesDnatRule)
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
	klog.V(3).Infof("enqueue delete iptables dnat %s", key)
	c.delIptablesDnatRuleQueue.Add(key)
}

func (c *Controller) enqueueAddIptablesSnatRule(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.IptablesSnatRule)).String()
	klog.V(3).Infof("enqueue add iptables snat %s", key)
	c.addIptablesSnatRuleQueue.Add(key)
}

func (c *Controller) enqueueUpdateIptablesSnatRule(oldObj, newObj any) {
	oldSnat := oldObj.(*kubeovnv1.IptablesSnatRule)
	newSnat := newObj.(*kubeovnv1.IptablesSnatRule)
	key := cache.MetaObjectToName(newSnat).String()
	if !newSnat.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("enqueue update to clean snat %s", key)
		c.updateIptablesSnatRuleQueue.Add(key)
		return
	}
	if newSnat.Spec.EIP == "" || newSnat.Spec.InternalCIDR == "" {
		klog.Errorf("skip enqueue snat %s: incomplete spec (eip=%q, internalCIDR=%q)", key, newSnat.Spec.EIP, newSnat.Spec.InternalCIDR)
		return
	}
	if oldSnat.Status.V4ip != newSnat.Status.V4ip ||
		oldSnat.Spec.EIP != newSnat.Spec.EIP ||
		oldSnat.Status.Redo != newSnat.Status.Redo ||
		oldSnat.Spec.InternalCIDR != newSnat.Spec.InternalCIDR {
		klog.V(3).Infof("enqueue update snat %s", key)
		c.updateIptablesSnatRuleQueue.Add(key)
		return
	}
}

func (c *Controller) enqueueDelIptablesSnatRule(obj any) {
	var snat *kubeovnv1.IptablesSnatRule
	switch t := obj.(type) {
	case *kubeovnv1.IptablesSnatRule:
		snat = t
	case cache.DeletedFinalStateUnknown:
		s, ok := t.Obj.(*kubeovnv1.IptablesSnatRule)
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
	klog.V(3).Infof("enqueue delete iptables snat %s", key)
	c.delIptablesSnatRuleQueue.Add(key)
}

// handleAddIptablesFip creates a FIP rule from scratch.
//
// Responsibility:
//   - Bring the FIP from an empty Status to a fully consistent state across all 4 dimensions:
//     1. iptables rule in NAT GW Pod  2. FIP Status  3. FIP Labels  4. EIP Status
//   - On success, Status MUST be fully populated (V4ip, InternalIP, NatGwDp, Ready=true).
//     This is the contract that handleUpdateIptablesFip relies on: a complete Status means
//     the old values are reliable and can be used for spec-change detection and rule deletion.
//   - On failure at any step, returns error to retry. Partial state (e.g., iptables rule
//     created but Status not yet patched) may exist; the next retry uses current Spec
//     and must converge to the desired state.
//
// Error state: if this handler never completes (Status.V4ip stays empty), the resource is
// in an error state. The update handler MUST NOT attempt NAT operations in this state —
// it lacks reliable old values for safe rule replacement.
func (c *Controller) handleAddIptablesFip(key string) error {
	fip, err := c.iptablesFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	if vpcNatEnabled != "true" {
		return errors.New("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle add iptables fip %s", key)

	if fip.Status.Ready && fip.Status.V4ip != "" {
		// already ok
		return nil
	}
	klog.V(3).Infof("handle add fip %s", key)

	if err := c.validateFipRule(fip); err != nil {
		return err
	}

	eip, err := c.GetEip(fip.Spec.EIP)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}

	if err = c.fipTryUseEip(key, eip.Spec.V4ip); err != nil {
		err = fmt.Errorf("failed to create fip %s, %w", key, err)
		klog.Error(err)
		return err
	}

	// we add the finalizer **before** we run "createFipInPod". This is because if we
	// added the finalizer after, then it is possible that the FIP is deleted after
	// we run createFipInPod but before the finalizer is created, and
	// then we can be left with IPtables rules in the VPC Nat
	// Gateway pod which are unmanaged.
	if err = c.handleAddIptablesFipFinalizer(key); err != nil {
		klog.Errorf("failed to handle add finalizer for fip, %v", err)
		return err
	}

	if err = c.createFipInPod(eip.Spec.NatGwDp, eip.Status.IP, fip.Spec.InternalIP); err != nil {
		klog.Errorf("failed to create fip, %v", err)
		return err
	}
	if err = c.patchFipStatus(key, eip.Status.IP, eip.Spec.V6ip, eip.Spec.NatGwDp, "", true); err != nil {
		klog.Errorf("failed to patch status for fip %s, %v", key, err)
		return err
	}
	// label too long cause error
	if err = c.patchFipLabel(key, eip); err != nil {
		klog.Errorf("failed to update label for fip %s, %v", key, err)
		return err
	}
	if err = c.patchEipStatus(fip.Spec.EIP, "", "", "", true); err != nil {
		// refresh eip nats
		klog.Errorf("failed to patch fip use eip %s, %v", key, err)
		return err
	}
	// patchFipLabel updated the FIP's EipV4IpLabel via the API, but the informer cache
	// may not have synced yet when patchEipStatus called getIptablesEipNat above, causing
	// it to miss the FIP and leave EIP.Status.Nat stale. Schedule a delayed reset.
	c.resetIptablesEipQueue.AddAfter(fip.Spec.EIP, 3*time.Second)
	return nil
}

func (c *Controller) fipTryUseEip(fipName, eipV4IP string) error {
	// check if has another fip using this eip already
	selector := labels.SelectorFromSet(labels.Set{util.EipV4IpLabel: eipV4IP})
	usingFips, err := c.iptablesFipsLister.List(selector)
	if err != nil {
		klog.Errorf("failed to get fips, %v", err)
		return err
	}
	for _, uf := range usingFips {
		if uf.Name != fipName {
			err = fmt.Errorf("%s is using by the other fip %s", eipV4IP, uf.Name)
			klog.Error(err)
			return err
		}
	}
	return nil
}

// handleUpdateIptablesFip handles FIP deletion, spec changes, and redo.
//
// FIP involves 4 dimensions of data that must be kept consistent:
//
//	Dimension        Storage                Description
//	──────────────── ────────────────────── ──────────────────────────────────────────
//	1. iptables rule NAT GW Pod             v4ip <-> internalIP DNAT/SNAT pair
//	2. FIP Status    CR .status             V4ip, InternalIP, NatGwDp, Ready
//	3. FIP Label     CR .labels/.annotations EipV4IpLabel, VpcNatGatewayNameLabel, VpcEipAnnotation
//	4. EIP Status    EIP CR .status         Nat field records which NAT rules use this EIP
//
// Precondition for spec-change path:
//   - Status.V4ip != "" (Status is complete, populated by handleAddIptablesFip).
//     A complete Status means the old values are reliable and reflect what was actually
//     created in the Pod. This is the ONLY safe basis for deleting old iptables rules.
//   - If Status.V4ip == "", the resource is in an error state (handleAdd never completed).
//     The update handler MUST NOT attempt any NAT operations — it has no reliable old
//     values to delete and creating new rules could leave stale data.
//     It should log the error and return, leaving recovery to the add handler (or future updates that fix the spec).
//     IMPORTANT: If a change fails, we must wait for retry until success. Continuing to change spec
//     on top of a failed state (dirty status) can introduce more zombie rules that are hard to track.
//
// Paths:
//
//  1. Delete path (DeletionTimestamp set):
//     Uses Status (with Spec fallback for best-effort cleanup) to locate the old iptables rule.
//     Removes finalizer, then async-resets EIP to refresh dimension 4.
//
//  2. Spec change path (Status.V4ip != "" AND Status differs from Spec+EIP):
//     Old values come from Status; new values come from Spec + EIP CR.
//     Steps strictly ordered to maintain all 4 dimensions:
//     a. finalDeleteFipInPod  (clean dimension 1 with old values from Status)
//     b. createFipInPod  (create dimension 1 with new values from Spec+EIP)
//     c. patchFipStatus  (update dimension 2 to match new iptables rule)
//     d. patchFipLabel   (update dimension 3 to match new EIP)
//     e. patchEipStatus  (update dimension 4 on new EIP)
//     f. resetOldEip     (async clean dimension 4 on old EIP, if EIP changed)
//     If any step fails, returns error to retry. Iptables operations are idempotent.
//
//  3. Redo path (gateway pod restarted):
//     Re-creates the iptables rule using Status.V4ip (the known-good external IP)
//     plus Spec.InternalIP. Only touches dimension 1; dimensions 2-4 are already correct.
func (c *Controller) handleUpdateIptablesFip(key string) error {
	cachedFip, err := c.iptablesFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle update iptables fip %s", key)

	// should delete
	if !cachedFip.DeletionTimestamp.IsZero() {
		if vpcNatEnabled == "true" {
			if err = c.finalDeleteFipInPod(key, cachedFip); err != nil {
				return err
			}
		}
		if err = c.handleDelIptablesFipFinalizer(key); err != nil {
			klog.Errorf("failed to handle del finalizer for fip, %v", err)
			return err
		}
		//  reset eip
		c.resetIptablesEipQueue.AddAfter(cachedFip.Spec.EIP, 3*time.Second)
		return nil
	}
	klog.V(3).Infof("handle update fip %s", key)
	// add or update should make sure vpc nat enabled
	if vpcNatEnabled != "true" {
		return errors.New("iptables nat gw not enable")
	}

	if err := c.validateFipRule(cachedFip); err != nil {
		return err
	}

	eip, err := c.GetEip(cachedFip.Spec.EIP)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}

	if err = c.fipTryUseEip(key, eip.Spec.V4ip); err != nil {
		err = fmt.Errorf("failed to update fip %s, %w", key, err)
		klog.Error(err)
		return err
	}

	if eip.Spec.NatGwDp == "" {
		klog.Errorf("fip %s: eip %s has empty NatGwDp, skip binding", key, cachedFip.Spec.EIP)
		return nil
	}

	// Error state: Status incomplete means handleAdd never completed.
	// Do not attempt NAT operations — no reliable old values for safe rule replacement.
	// All spec-corresponding Status fields must be populated for the update handler to proceed.
	if cachedFip.Status.V4ip == "" || cachedFip.Status.NatGwDp == "" || cachedFip.Status.InternalIP == "" {
		klog.Errorf("fip %s has incomplete status (V4ip=%q, NatGwDp=%q, InternalIP=%q), skipping NAT operations; waiting for add handler to complete",
			key, cachedFip.Status.V4ip, cachedFip.Status.NatGwDp, cachedFip.Status.InternalIP)
		return nil
	}

	// spec change: compare Status (old, what's in Pod) vs Spec+EIP (new, desired)
	oldV4ip := cachedFip.Status.V4ip
	newV4ip := eip.Status.IP
	newInternalIP := cachedFip.Spec.InternalIP

	// Warn if we are modifying a resource that might be in a dirty state from a previous failed update.
	if !cachedFip.Status.Ready {
		// TODO: consider using a webhook to reject spec changes when the resource is not ready,
		// to prevent users from modifying the spec when the previous update has not yet been fully reconciled.
		klog.Warningf("fip %s is being updated while not Ready (previous update likely failed). This may lead to stale iptables rules.", key)
	}

	// Verify new parameters are valid before modifying any state.
	// eip.Status.IP can be empty if EIP itself is in error state.
	if newV4ip == "" || newInternalIP == "" {
		klog.Errorf("skipping fip %s update: incomplete new parameters (v4ip=%q, internalIP=%q)", key, newV4ip, newInternalIP)
		return nil
	}

	if oldV4ip != newV4ip || cachedFip.Status.InternalIP != newInternalIP {
		// Mark FIP as not ready before starting the update.
		// This ensures that if the controller crashes or the update fails midway,
		// the resource will be left in a non-ready state, indicating a potential inconsistency.
		if cachedFip.Status.Ready {
			klog.V(3).Infof("fip %s spec changed, marking as not ready before update", key)
			if err = c.patchFipStatus(key, oldV4ip, cachedFip.Status.V6ip, cachedFip.Status.NatGwDp, "", false); err != nil {
				klog.Errorf("failed to mark fip %s as not ready, %v", key, err)
				return err
			}
		}
		// delete old rule; finalDeleteFipInPod resolves (natGwDp, v4ip) from Status
		if err = c.finalDeleteFipInPod(key, cachedFip); err != nil {
			return err
		}
		if err = c.createFipInPod(eip.Spec.NatGwDp, newV4ip, newInternalIP); err != nil {
			klog.Errorf("failed to create fip %s, %v", key, err)
			return err
		}
		if err = c.patchFipStatus(key, newV4ip, eip.Spec.V6ip, eip.Spec.NatGwDp, "", true); err != nil {
			klog.Errorf("failed to patch status for fip %s, %v", key, err)
			return err
		}
		if err = c.patchFipLabel(key, eip); err != nil {
			klog.Errorf("failed to update label for fip %s, %v", key, err)
			return err
		}
		if err = c.patchEipStatus(cachedFip.Spec.EIP, "", "", "", true); err != nil {
			klog.Errorf("failed to patch fip use eip %s, %v", key, err)
			return err
		}
		// Reset old EIP after all 4 dimensions are updated.
		// This must NOT be in enqueue — async reset there could race with handler's
		// patchEipStatus, causing the old EIP's nat label to be cleared before the
		// new EIP is fully bound, leaving a window where the old EIP appears free.
		if oldEipName := cachedFip.Annotations[util.VpcEipAnnotation]; oldEipName != "" && oldEipName != cachedFip.Spec.EIP {
			c.resetIptablesEipQueue.AddAfter(oldEipName, 3*time.Second)
		}
		if err = c.handleAddIptablesFipFinalizer(key); err != nil {
			klog.Errorf("failed to handle add finalizer for fip %s, %v", key, err)
			return err
		}
		return nil
	}

	// redo
	if !cachedFip.Status.Ready &&
		cachedFip.Status.Redo != "" &&
		cachedFip.Status.V4ip != "" &&
		cachedFip.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("reapply fip '%s' in pod", key)
		gwPod, err := c.getNatGwPod(cachedFip.Status.NatGwDp, c.natGwNamespaceByName(cachedFip.Status.NatGwDp))
		if err != nil {
			klog.Error(err)
			return err
		}
		// If Pod started before the redo timestamp, it has not restarted since
		// the redo was marked — iptables rules are still intact, skip re-creation.
		fipRedo, _ := time.ParseInLocation("2006-01-02T15:04:05", cachedFip.Status.Redo, time.Local)
		if len(gwPod.Status.ContainerStatuses) == 0 || gwPod.Status.ContainerStatuses[0].State.Running == nil {
			return fmt.Errorf("fip %s: gateway pod container not running, will retry redo", key)
		}
		if gwPod.Status.ContainerStatuses[0].State.Running.StartedAt.Before(&metav1.Time{Time: fipRedo}) {
			klog.V(3).Infof("fip %s: pod started before redo mark, rules intact, skip", key)
			return nil
		}
		if err = c.createFipInPod(cachedFip.Status.NatGwDp, cachedFip.Status.V4ip, cachedFip.Status.InternalIP); err != nil {
			klog.Errorf("failed to create fip, %v", err)
			return err
		}
		if err = c.patchFipStatus(key, "", "", "", "", true); err != nil {
			klog.Errorf("failed to patch status for fip %s, %v", key, err)
			return err
		}
	}
	if err = c.handleAddIptablesFipFinalizer(key); err != nil {
		klog.Errorf("failed to handle add finalizer for fip %s, %v", key, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIptablesFip(key string) error {
	klog.V(3).Infof("deleted iptables fip %s", key)
	return nil
}

// handleAddIptablesDnatRule creates a DNAT rule from scratch.
//
// Responsibility:
//   - Bring the DNAT from an empty Status to a fully consistent state across all 4 dimensions:
//     1. iptables rule in NAT GW Pod  2. DNAT Status  3. DNAT Labels  4. EIP Status
//   - On success, Status MUST be fully populated (V4ip, Protocol, ExternalPort, InternalIP,
//     InternalPort, NatGwDp, Ready=true). This is the contract that handleUpdateIptablesDnatRule
//     relies on: a complete Status provides reliable old values for spec-change detection
//     and rule deletion.
//   - On failure at any step, returns error to retry. Partial state may exist; the next
//     retry uses current Spec and must converge to the desired state.
//
// Error state: if this handler never completes (Status.V4ip stays empty), the resource is
// in an error state. The update handler MUST NOT attempt NAT operations in this state.
func (c *Controller) handleAddIptablesDnatRule(key string) error {
	dnat, err := c.iptablesDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	if vpcNatEnabled != "true" {
		return errors.New("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle add iptables dnat rule %s", key)

	if dnat.Status.Ready && dnat.Status.V4ip != "" {
		// already ok
		return nil
	}
	klog.V(3).Infof("handle add iptables dnat %s", key)

	if err := c.validateDnatRule(dnat); err != nil {
		return err
	}

	eip, err := c.GetEip(dnat.Spec.EIP)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}
	if dup, err := c.isDnatDuplicated(eip.Spec.NatGwDp, dnat.Spec.EIP, dnat.Name, dnat.Spec.ExternalPort, dnat.Spec.Protocol); dup || err != nil {
		klog.Error(err)
		return err
	}
	// Add the finalizer **before** creating rules in Pod. If we added it after,
	// the DNAT could be deleted after createDnatInPod but before the finalizer,
	// leaving unmanaged iptables rules in the gateway pod.
	if err = c.handleAddIptablesDnatFinalizer(key); err != nil {
		klog.Errorf("failed to handle add finalizer for dnat, %v", err)
		return err
	}
	if err = c.createDnatInPod(eip.Spec.NatGwDp, dnat.Spec.Protocol,
		eip.Status.IP, dnat.Spec.InternalIP,
		dnat.Spec.ExternalPort, dnat.Spec.InternalPort); err != nil {
		klog.Errorf("failed to create dnat, %v", err)
		return err
	}
	if err = c.patchDnatStatus(key, eip.Status.IP, eip.Spec.V6ip, eip.Spec.NatGwDp, "", true); err != nil {
		klog.Errorf("failed to patch status for dnat %s, %v", key, err)
		return err
	}
	// label too long cause error
	if err = c.patchDnatLabel(key, eip); err != nil {
		klog.Errorf("failed to patch label for dnat %s, %v", key, err)
		return err
	}
	if err = c.patchEipStatus(dnat.Spec.EIP, "", "", "", true); err != nil {
		// refresh eip nats
		klog.Errorf("failed to patch dnat use eip %s, %v", key, err)
		return err
	}
	// patchDnatLabel updated the DNAT's EipV4IpLabel via the API, but the informer cache
	// may not have synced yet when patchEipStatus called getIptablesEipNat above, causing
	// it to miss the DNAT and leave EIP.Status.Nat stale. Schedule a delayed reset.
	c.resetIptablesEipQueue.AddAfter(dnat.Spec.EIP, 3*time.Second)
	return nil
}

// handleUpdateIptablesDnatRule handles DNAT rule deletion, spec changes, and redo.
//
// DNAT involves 4 dimensions of data consistency (see handleUpdateIptablesFip for general pattern):
//  1. iptables rule  - (protocol, v4ip, externalPort) -> (internalIP, internalPort)
//  2. DNAT Status    - V4ip, Protocol, InternalIP, ExternalPort, InternalPort, NatGwDp, Ready
//  3. DNAT Label     - EipV4IpLabel, VpcNatGatewayNameLabel, VpcDnatEPortLabel, VpcEipAnnotation
//  4. EIP Status     - Nat field
//
// Key difference from FIP: DNAT identity = (eip, externalPort, protocol).
// del_dnat in nat-gateway.sh matches by identity only (lenient deletion),
// so even if controller passes stale internalIP/internalPort, deletion still works correctly.
//
// Precondition for spec-change path:
//   - Status.V4ip != "" (Status is complete, populated by handleAddIptablesDnatRule).
//     Status provides reliable old values for detecting what changed and deleting old rules.
//   - If Status.V4ip == "", the resource is in an error state (handleAdd never completed).
//     The update handler MUST NOT attempt any NAT operations — it should log the error
//     and return, leaving recovery to the add handler.
//     IMPORTANT: If a change fails, we must wait for retry until success. Continuing to change spec
//     on top of a failed state (dirty status) can introduce more zombie rules that are hard to track.
func (c *Controller) handleUpdateIptablesDnatRule(key string) error {
	cachedDnat, err := c.iptablesDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle update iptables dnat %s", key)

	// should delete
	if !cachedDnat.DeletionTimestamp.IsZero() {
		if vpcNatEnabled == "true" {
			if err = c.finalDeleteDnatInPod(key, cachedDnat); err != nil {
				return err
			}
		}
		if err = c.handleDelIptablesDnatFinalizer(key); err != nil {
			klog.Errorf("failed to handle del finalizer for dnat %s, %v", key, err)
			return err
		}
		//  reset eip
		c.resetIptablesEipQueue.AddAfter(cachedDnat.Spec.EIP, 3*time.Second)
		return nil
	}
	klog.V(3).Infof("handle update dnat %s", key)

	if err := c.validateDnatRule(cachedDnat); err != nil {
		return err
	}

	eip, err := c.GetEip(cachedDnat.Spec.EIP)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}
	if dup, err := c.isDnatDuplicated(eip.Spec.NatGwDp, cachedDnat.Spec.EIP, cachedDnat.Name, cachedDnat.Spec.ExternalPort, cachedDnat.Spec.Protocol); dup || err != nil {
		klog.Errorf("failed to update dnat, %v", err)
		return err
	}
	// add or update should make sure vpc nat enabled
	if vpcNatEnabled != "true" {
		return errors.New("iptables nat gw not enable")
	}

	if eip.Spec.NatGwDp == "" {
		klog.Errorf("dnat %s: eip %s has empty NatGwDp, skip binding", key, cachedDnat.Spec.EIP)
		return nil
	}

	// Error state: Status incomplete means handleAdd never completed.
	// Do not attempt NAT operations — no reliable old values for safe rule replacement.
	// All spec-corresponding Status fields must be populated for the update handler to proceed.
	if cachedDnat.Status.V4ip == "" || cachedDnat.Status.NatGwDp == "" || cachedDnat.Status.Protocol == "" ||
		cachedDnat.Status.ExternalPort == "" || cachedDnat.Status.InternalIP == "" || cachedDnat.Status.InternalPort == "" {
		klog.Errorf("dnat %s has incomplete status (V4ip=%q, NatGwDp=%q, Protocol=%q, ExternalPort=%q, InternalIP=%q, InternalPort=%q), skipping NAT operations; waiting for add handler to complete",
			key, cachedDnat.Status.V4ip, cachedDnat.Status.NatGwDp, cachedDnat.Status.Protocol,
			cachedDnat.Status.ExternalPort, cachedDnat.Status.InternalIP, cachedDnat.Status.InternalPort)
		return nil
	}

	// spec change: compare Status (old, what's in Pod) vs Spec+EIP (new, desired)
	oldV4ip := cachedDnat.Status.V4ip
	oldProtocol := cachedDnat.Status.Protocol
	oldExternalPort := cachedDnat.Status.ExternalPort
	newV4ip := eip.Status.IP
	newProtocol := cachedDnat.Spec.Protocol
	newInternalIP := cachedDnat.Spec.InternalIP
	newExternalPort := cachedDnat.Spec.ExternalPort
	newInternalPort := cachedDnat.Spec.InternalPort

	// Warn if we are modifying a resource that might be in a dirty state from a previous failed update.
	if !cachedDnat.Status.Ready {
		// TODO: consider using a webhook to reject spec changes when the resource is not ready,
		// to prevent users from modifying the spec when the previous update has not yet been fully reconciled.
		klog.Warningf("dnat %s is being updated while not Ready (previous update likely failed). This may lead to stale iptables rules.", key)
	}

	// Verify new parameters are valid before modifying any state.
	// eip.Status.IP can be empty if EIP itself is in error state.
	if newV4ip == "" || newInternalIP == "" || newExternalPort == "" || newProtocol == "" || newInternalPort == "" {
		klog.Errorf("skipping dnat %s update: incomplete new parameters (v4ip=%q, internalIP=%q, externalPort=%q, protocol=%q, internalPort=%q)",
			key, newV4ip, newInternalIP, newExternalPort, newProtocol, newInternalPort)
		return nil
	}

	if oldV4ip != newV4ip || oldProtocol != newProtocol || oldExternalPort != newExternalPort ||
		cachedDnat.Status.InternalIP != newInternalIP || cachedDnat.Status.InternalPort != newInternalPort {
		// Mark DNAT as not ready before starting the update.
		// This ensures that if the controller crashes or the update fails midway,
		// the resource will be left in a non-ready state, indicating a potential inconsistency.
		if cachedDnat.Status.Ready {
			klog.V(3).Infof("dnat %s spec changed, marking as not ready before update", key)
			if err = c.patchDnatStatus(key, oldV4ip, cachedDnat.Status.V6ip, cachedDnat.Status.NatGwDp, "", false); err != nil {
				klog.Errorf("failed to mark dnat %s as not ready, %v", key, err)
				return err
			}
		}
		// delete old rule; finalDeleteDnatInPod resolves identity from Status
		if err = c.finalDeleteDnatInPod(key, cachedDnat); err != nil {
			return err
		}
		if err = c.createDnatInPod(eip.Spec.NatGwDp, newProtocol,
			newV4ip, newInternalIP,
			newExternalPort, newInternalPort); err != nil {
			klog.Errorf("failed to create dnat %s, %v", key, err)
			return err
		}
		if err = c.patchDnatStatus(key, newV4ip, eip.Spec.V6ip, eip.Spec.NatGwDp, "", true); err != nil {
			klog.Errorf("failed to patch status for dnat %s, %v", key, err)
			return err
		}
		if err = c.patchDnatLabel(key, eip); err != nil {
			klog.Errorf("failed to patch label for dnat %s, %v", key, err)
			return err
		}
		if err = c.patchEipStatus(cachedDnat.Spec.EIP, "", "", "", true); err != nil {
			klog.Errorf("failed to patch dnat use eip %s, %v", key, err)
			return err
		}
		// Reset old EIP after all 4 dimensions are updated.
		// This must NOT be in enqueue — async reset there could race with handler's
		// patchEipStatus, causing the old EIP's nat label to be cleared before the
		// new EIP is fully bound, leaving a window where the old EIP appears free.
		if oldEipName := cachedDnat.Annotations[util.VpcEipAnnotation]; oldEipName != "" && oldEipName != cachedDnat.Spec.EIP {
			c.resetIptablesEipQueue.AddAfter(oldEipName, 3*time.Second)
		}
		if err = c.handleAddIptablesDnatFinalizer(key); err != nil {
			klog.Errorf("failed to handle add finalizer for dnat %s, %v", key, err)
			return err
		}
		return nil
	}

	// redo
	if !cachedDnat.Status.Ready &&
		cachedDnat.Status.Redo != "" &&
		cachedDnat.Status.V4ip != "" &&
		cachedDnat.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("reapply dnat in pod for %s", key)
		gwPod, err := c.getNatGwPod(cachedDnat.Status.NatGwDp, c.natGwNamespaceByName(cachedDnat.Status.NatGwDp))
		if err != nil {
			klog.Error(err)
			return err
		}
		// If Pod started before the redo timestamp, it has not restarted since
		// the redo was marked — iptables rules are still intact, skip re-creation.
		dnatRedo, _ := time.ParseInLocation("2006-01-02T15:04:05", cachedDnat.Status.Redo, time.Local)
		if len(gwPod.Status.ContainerStatuses) == 0 || gwPod.Status.ContainerStatuses[0].State.Running == nil {
			return fmt.Errorf("dnat %s: gateway pod container not running, will retry redo", key)
		}
		if gwPod.Status.ContainerStatuses[0].State.Running.StartedAt.Before(&metav1.Time{Time: dnatRedo}) {
			klog.V(3).Infof("dnat %s: pod started before redo mark, rules intact, skip", key)
			return nil
		}
		if err = c.createDnatInPod(cachedDnat.Status.NatGwDp, cachedDnat.Status.Protocol,
			cachedDnat.Status.V4ip, cachedDnat.Status.InternalIP,
			cachedDnat.Status.ExternalPort, cachedDnat.Status.InternalPort); err != nil {
			klog.Errorf("failed to create dnat %s, %v", key, err)
			return err
		}
		if err = c.patchDnatStatus(key, "", "", "", "", true); err != nil {
			klog.Errorf("failed to patch status for dnat %s, %v", key, err)
			return err
		}
	}
	if err = c.handleAddIptablesDnatFinalizer(key); err != nil {
		klog.Errorf("failed to handle add finalizer for dnat %s, %v", key, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIptablesDnatRule(key string) error {
	klog.V(3).Infof("deleted iptables dnat %s", key)
	return nil
}

// handleAddIptablesSnatRule creates a SNAT rule from scratch.
//
// Responsibility:
//   - Bring the SNAT from an empty Status to a fully consistent state across all 4 dimensions:
//     1. iptables rule in NAT GW Pod  2. SNAT Status  3. SNAT Labels  4. EIP Status
//   - On success, Status MUST be fully populated (V4ip, InternalCIDR, NatGwDp, Ready=true).
//     This is the contract that handleUpdateIptablesSnatRule relies on: a complete Status
//     provides reliable old values for spec-change detection and rule deletion.
//   - On failure at any step, returns error to retry. Partial state may exist; the next
//     retry uses current Spec and must converge to the desired state.
//
// Error state: if this handler never completes (Status.V4ip stays empty), the resource is
// in an error state. The update handler MUST NOT attempt NAT operations in this state.
func (c *Controller) handleAddIptablesSnatRule(key string) error {
	snat, err := c.iptablesSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	if vpcNatEnabled != "true" {
		return errors.New("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle add iptables snat rule %s", key)

	if snat.Status.Ready && snat.Status.V4ip != "" {
		// already ok
		return nil
	}
	klog.V(3).Infof("handle add iptables snat %s", key)

	if err := c.validateSnatRule(snat); err != nil {
		return err
	}

	eip, err := c.GetEip(snat.Spec.EIP)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}
	// create snat
	v4Cidr, _ := util.SplitStringIP(snat.Spec.InternalCIDR)
	if v4Cidr == "" {
		// only support IPv4 snat
		err = fmt.Errorf("failed to get snat v4 internal cidr, original cidr is %s", snat.Spec.InternalCIDR)
		return err
	}
	// Add the finalizer **before** creating rules in Pod. If we added it after,
	// the SNAT could be deleted after createSnatInPod but before the finalizer,
	// leaving unmanaged iptables rules in the gateway pod.
	if err = c.handleAddIptablesSnatFinalizer(key); err != nil {
		klog.Errorf("failed to handle add finalizer for snat, %v", err)
		return err
	}
	if err = c.createSnatInPod(eip.Spec.NatGwDp, eip.Status.IP, v4Cidr); err != nil {
		klog.Errorf("failed to create snat, %v", err)
		return err
	}
	if err = c.patchSnatStatus(key, eip.Status.IP, eip.Spec.V6ip, eip.Spec.NatGwDp, "", true); err != nil {
		klog.Errorf("failed to update status for snat %s, %v", key, err)
		return err
	}
	if err = c.patchSnatLabel(key, eip); err != nil {
		klog.Errorf("failed to patch label for snat %s, %v", key, err)
		return err
	}
	if err = c.patchEipStatus(snat.Spec.EIP, "", "", "", true); err != nil {
		// refresh eip nats
		klog.Errorf("failed to patch snat use eip %s, %v", key, err)
		return err
	}
	// patchSnatLabel updated the SNAT's EipV4IpLabel via the API, but the informer cache
	// may not have synced yet when patchEipStatus called getIptablesEipNat above, causing
	// it to silently miss the SNAT and leave EIP.Status.Nat stale. Schedule a delayed reset
	// so that after the informer syncs the label, the EIP nat status is corrected.
	c.resetIptablesEipQueue.AddAfter(snat.Spec.EIP, 3*time.Second)
	return nil
}

// handleUpdateIptablesSnatRule handles SNAT rule deletion, spec changes, and redo.
//
// SNAT involves 4 dimensions of data consistency (see handleUpdateIptablesFip for general pattern):
//  1. iptables rule  - v4ip SNAT for internalCIDR
//  2. SNAT Status    - V4ip, InternalCIDR, NatGwDp, Ready
//  3. SNAT Label     - EipV4IpLabel, VpcNatGatewayNameLabel, VpcEipAnnotation
//  4. EIP Status     - Nat field
//
// Key difference from FIP/DNAT: SNAT is a 1:N model. One EIP can serve multiple CIDRs,
// and one CIDR can have multiple EIPs (for port exhaustion mitigation via --random-fully).
// SNAT identity = (eip, internalCIDR). del_snat in nat-gateway.sh matches by identity
// to locate the exact rule.
//
// Precondition for spec-change path:
//   - Status.V4ip != "" (Status is complete, populated by handleAddIptablesSnatRule).
//     Status provides reliable old values for detecting what changed and deleting old rules.
//   - If Status.V4ip == "", the resource is in an error state (handleAdd never completed).
//     The update handler MUST NOT attempt any NAT operations — it should log the error
//     and return, leaving recovery to the add handler.
//     IMPORTANT: If a change fails, we must wait for retry until success. Continuing to change spec
//     on top of a failed state (dirty status) can introduce more zombie rules that are hard to track.
func (c *Controller) handleUpdateIptablesSnatRule(key string) error {
	cachedSnat, err := c.iptablesSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle update iptables snat rule %s", key)

	// should delete
	if !cachedSnat.DeletionTimestamp.IsZero() {
		if vpcNatEnabled == "true" {
			if err = c.finalDeleteSnatInPod(key, cachedSnat); err != nil {
				return err
			}
		}
		if err = c.handleDelIptablesSnatFinalizer(key); err != nil {
			klog.Errorf("failed to handle del finalizer for snat %s, %v", key, err)
			return err
		}
		c.resetIptablesEipQueue.AddAfter(cachedSnat.Spec.EIP, 3*time.Second)
		return nil
	}
	klog.V(3).Infof("handle update snat %s", key)

	if err := c.validateSnatRule(cachedSnat); err != nil {
		return err
	}

	eip, err := c.GetEip(cachedSnat.Spec.EIP)
	if err != nil {
		klog.Errorf("failed to get eip, %v", err)
		return err
	}

	// add or update should make sure vpc nat enabled
	if vpcNatEnabled != "true" {
		return errors.New("iptables nat gw not enable")
	}

	if eip.Spec.NatGwDp == "" {
		klog.Errorf("snat %s: eip %s has empty NatGwDp, skip binding", key, cachedSnat.Spec.EIP)
		return nil
	}

	// Error state: Status incomplete means handleAdd never completed.
	// Do not attempt NAT operations — no reliable old values for safe rule replacement.
	// All spec-corresponding Status fields must be populated for the update handler to proceed.
	if cachedSnat.Status.V4ip == "" || cachedSnat.Status.NatGwDp == "" || cachedSnat.Status.InternalCIDR == "" {
		klog.Errorf("snat %s has incomplete status (V4ip=%q, NatGwDp=%q, InternalCIDR=%q), skipping NAT operations; waiting for add handler to complete",
			key, cachedSnat.Status.V4ip, cachedSnat.Status.NatGwDp, cachedSnat.Status.InternalCIDR)
		return nil
	}

	// spec change: compare Status (old, what's in Pod) vs Spec+EIP (new, desired)
	// SNAT identity = (v4ip, internalCIDR), all fields are identity — no non-identity fields.
	oldV4ip := cachedSnat.Status.V4ip
	oldV4Cidr, _ := util.SplitStringIP(cachedSnat.Status.InternalCIDR)
	newV4ip := eip.Status.IP
	newV4Cidr, _ := util.SplitStringIP(cachedSnat.Spec.InternalCIDR)

	// Warn if we are modifying a resource that might be in a dirty state from a previous failed update.
	if !cachedSnat.Status.Ready {
		// TODO: consider using a webhook to reject spec changes when the resource is not ready,
		// to prevent users from modifying the spec when the previous update has not yet been fully reconciled.
		klog.Warningf("snat %s is being updated while not Ready (previous update likely failed). This may lead to stale iptables rules.", key)
	}

	// Verify new parameters are valid before modifying any state.
	// eip.Status.IP can be empty if EIP itself is in error state;
	// SplitStringIP can return empty v4 part for IPv6-only CIDR.
	if newV4ip == "" || newV4Cidr == "" {
		klog.Errorf("skipping snat %s update: incomplete new parameters (v4ip=%q, v4Cidr=%q)", key, newV4ip, newV4Cidr)
		return nil
	}

	if oldV4ip != newV4ip || oldV4Cidr != newV4Cidr {
		// Mark SNAT as not ready before starting the update.
		// This ensures that if the controller crashes or the update fails midway,
		// the resource will be left in a non-ready state, indicating a potential inconsistency.
		if cachedSnat.Status.Ready {
			klog.V(3).Infof("snat %s spec changed, marking as not ready before update", key)
			if err = c.patchSnatStatus(key, oldV4ip, cachedSnat.Status.V6ip, cachedSnat.Status.NatGwDp, "", false); err != nil {
				klog.Errorf("failed to mark snat %s as not ready, %v", key, err)
				return err
			}
		}
		// delete old rule; finalDeleteSnatInPod resolves identity from Status
		if err = c.finalDeleteSnatInPod(key, cachedSnat); err != nil {
			return err
		}
		if err = c.createSnatInPod(eip.Spec.NatGwDp, newV4ip, newV4Cidr); err != nil {
			klog.Errorf("failed to create snat %s, %v", key, err)
			return err
		}
		if err = c.patchSnatStatus(key, newV4ip, eip.Spec.V6ip, eip.Spec.NatGwDp, "", true); err != nil {
			klog.Errorf("failed to patch status for snat %s, %v", key, err)
			return err
		}
		if err = c.patchSnatLabel(key, eip); err != nil {
			klog.Errorf("failed to patch label for snat %s, %v", key, err)
			return err
		}
		if err = c.patchEipStatus(cachedSnat.Spec.EIP, "", "", "", true); err != nil {
			klog.Errorf("failed to patch snat use eip %s, %v", key, err)
			return err
		}
		// Reset old EIP after all 4 dimensions are updated.
		// This must NOT be in enqueue — async reset there could race with handler's
		// patchEipStatus, causing the old EIP's nat label to be cleared before the
		// new EIP is fully bound, leaving a window where the old EIP appears free.
		if oldEipName := cachedSnat.Annotations[util.VpcEipAnnotation]; oldEipName != "" && oldEipName != cachedSnat.Spec.EIP {
			c.resetIptablesEipQueue.AddAfter(oldEipName, 3*time.Second)
		}
		if err = c.handleAddIptablesSnatFinalizer(key); err != nil {
			klog.Errorf("failed to handle add finalizer for snat %s, %v", key, err)
			return err
		}
		return nil
	}

	// redo
	if !cachedSnat.Status.Ready &&
		cachedSnat.Status.Redo != "" &&
		cachedSnat.Status.V4ip != "" &&
		cachedSnat.DeletionTimestamp.IsZero() {
		gwPod, err := c.getNatGwPod(cachedSnat.Status.NatGwDp, c.natGwNamespaceByName(cachedSnat.Status.NatGwDp))
		if err != nil {
			klog.Error(err)
			return err
		}
		// If Pod started before the redo timestamp, it has not restarted since
		// the redo was marked — iptables rules are still intact, skip re-creation.
		snatRedo, _ := time.ParseInLocation("2006-01-02T15:04:05", cachedSnat.Status.Redo, time.Local)
		if len(gwPod.Status.ContainerStatuses) == 0 || gwPod.Status.ContainerStatuses[0].State.Running == nil {
			return fmt.Errorf("snat %s: gateway pod container not running, will retry redo", key)
		}
		if gwPod.Status.ContainerStatuses[0].State.Running.StartedAt.Before(&metav1.Time{Time: snatRedo}) {
			klog.V(3).Infof("snat %s: pod started before redo mark, rules intact, skip", key)
			return nil
		}
		if err = c.createSnatInPod(cachedSnat.Status.NatGwDp, cachedSnat.Status.V4ip, cachedSnat.Status.InternalCIDR); err != nil {
			klog.Errorf("failed to create new snat, %v", err)
			return err
		}
		if err = c.patchSnatStatus(key, "", "", "", "", true); err != nil {
			klog.Errorf("failed to patch status for snat %s, %v", key, err)
			return err
		}
	}
	if err = c.handleAddIptablesSnatFinalizer(key); err != nil {
		klog.Errorf("failed to handle add finalizer for snat %s, %v", key, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIptablesSnatRule(key string) error {
	klog.V(3).Infof("deleted iptables snat %s", key)
	return nil
}

func (c *Controller) syncIptablesFipFinalizer(cl client.Client) error {
	// migrate depreciated finalizer to new finalizer
	rules := &kubeovnv1.IptablesFIPRuleList{}
	return migrateFinalizers(cl, rules, func(i int) (client.Object, client.Object) {
		if i < 0 || i >= len(rules.Items) {
			return nil, nil
		}
		return rules.Items[i].DeepCopy(), rules.Items[i].DeepCopy()
	})
}

func (c *Controller) handleAddIptablesFipFinalizer(key string) error {
	cachedIptablesFip, err := c.iptablesFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if !cachedIptablesFip.DeletionTimestamp.IsZero() || controllerutil.ContainsFinalizer(cachedIptablesFip, util.KubeOVNControllerFinalizer) {
		return nil
	}
	newIptablesFip := cachedIptablesFip.DeepCopy()
	controllerutil.RemoveFinalizer(newIptablesFip, util.DepreciatedFinalizerName)
	controllerutil.AddFinalizer(newIptablesFip, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedIptablesFip, newIptablesFip)
	if err != nil {
		klog.Errorf("failed to generate patch payload for iptables fip '%s', %v", cachedIptablesFip.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Patch(context.Background(), cachedIptablesFip.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for iptables fip '%s', %v", cachedIptablesFip.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIptablesFipFinalizer(key string) error {
	cachedIptablesFip, err := c.iptablesFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if len(cachedIptablesFip.GetFinalizers()) == 0 {
		return nil
	}
	newIptablesFip := cachedIptablesFip.DeepCopy()
	controllerutil.RemoveFinalizer(newIptablesFip, util.DepreciatedFinalizerName)
	controllerutil.RemoveFinalizer(newIptablesFip, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedIptablesFip, newIptablesFip)
	if err != nil {
		klog.Errorf("failed to generate patch payload for iptables fip '%s', %v", cachedIptablesFip.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Patch(context.Background(), cachedIptablesFip.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from iptables fip '%s', %v", cachedIptablesFip.Name, err)
		return err
	}
	return nil
}

func (c *Controller) syncIptablesDnatFinalizer(cl client.Client) error {
	// migrate depreciated finalizer to new finalizer
	rules := &kubeovnv1.IptablesDnatRuleList{}
	return migrateFinalizers(cl, rules, func(i int) (client.Object, client.Object) {
		if i < 0 || i >= len(rules.Items) {
			return nil, nil
		}
		return rules.Items[i].DeepCopy(), rules.Items[i].DeepCopy()
	})
}

func (c *Controller) handleAddIptablesDnatFinalizer(key string) error {
	cachedIptablesDnat, err := c.iptablesDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if !cachedIptablesDnat.DeletionTimestamp.IsZero() || controllerutil.ContainsFinalizer(cachedIptablesDnat, util.KubeOVNControllerFinalizer) {
		return nil
	}
	newIptablesDnat := cachedIptablesDnat.DeepCopy()
	controllerutil.RemoveFinalizer(newIptablesDnat, util.DepreciatedFinalizerName)
	controllerutil.AddFinalizer(newIptablesDnat, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedIptablesDnat, newIptablesDnat)
	if err != nil {
		klog.Errorf("failed to generate patch payload for iptables dnat '%s', %v", cachedIptablesDnat.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Patch(context.Background(), cachedIptablesDnat.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for iptables dnat '%s', %v", cachedIptablesDnat.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIptablesDnatFinalizer(key string) error {
	cachedIptablesDnat, err := c.iptablesDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if len(cachedIptablesDnat.GetFinalizers()) == 0 {
		return nil
	}
	newIptablesDnat := cachedIptablesDnat.DeepCopy()
	controllerutil.RemoveFinalizer(newIptablesDnat, util.DepreciatedFinalizerName)
	controllerutil.RemoveFinalizer(newIptablesDnat, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedIptablesDnat, newIptablesDnat)
	if err != nil {
		klog.Errorf("failed to generate patch payload for iptables dnat '%s', %v", cachedIptablesDnat.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Patch(context.Background(), cachedIptablesDnat.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from iptables dnat '%s', %v", cachedIptablesDnat.Name, err)
		return err
	}
	return nil
}

func (c *Controller) patchFipLabel(key string, eip *kubeovnv1.IptablesEIP) error {
	oriFip, err := c.iptablesFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	fip := oriFip.DeepCopy()
	var needUpdateLabel, needUpdateAnno bool
	var op string
	if len(fip.Labels) == 0 {
		op = "add"
		fip.Labels = map[string]string{
			util.VpcNatGatewayNameLabel: eip.Spec.NatGwDp,
			util.EipV4IpLabel:           eip.Spec.V4ip,
		}
		needUpdateLabel = true
	} else if fip.Labels[util.SubnetNameLabel] != eip.Spec.NatGwDp ||
		fip.Labels[util.EipV4IpLabel] != eip.Spec.V4ip {
		op = "replace"
		fip.Labels[util.VpcNatGatewayNameLabel] = eip.Spec.NatGwDp
		fip.Labels[util.EipV4IpLabel] = eip.Spec.V4ip
		needUpdateLabel = true
	}
	if needUpdateLabel {
		if err := c.updateIptableLabels(fip.Name, op, util.FipUsingEip, fip.Labels); err != nil {
			klog.Error(err)
			return err
		}
	}

	if len(fip.Annotations) == 0 {
		op = "add"
		needUpdateAnno = true
		fip.Annotations = map[string]string{
			util.VpcEipAnnotation: eip.Name,
		}
	} else if fip.Annotations[util.VpcEipAnnotation] != eip.Name {
		op = "replace"
		needUpdateAnno = true
		fip.Annotations[util.VpcEipAnnotation] = eip.Name
	}
	if needUpdateAnno {
		if err := c.updateIptableAnnotations(fip.Name, op, util.FipUsingEip, fip.Annotations); err != nil {
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *Controller) syncIptablesSnatFinalizer(cl client.Client) error {
	rules := &kubeovnv1.IptablesSnatRuleList{}
	return migrateFinalizers(cl, rules, func(i int) (client.Object, client.Object) {
		if i < 0 || i >= len(rules.Items) {
			return nil, nil
		}
		return rules.Items[i].DeepCopy(), rules.Items[i].DeepCopy()
	})
}

func (c *Controller) handleAddIptablesSnatFinalizer(key string) error {
	cachedIptablesSnat, err := c.iptablesSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if !cachedIptablesSnat.DeletionTimestamp.IsZero() || controllerutil.ContainsFinalizer(cachedIptablesSnat, util.KubeOVNControllerFinalizer) {
		return nil
	}
	newIptablesSnat := cachedIptablesSnat.DeepCopy()
	controllerutil.RemoveFinalizer(newIptablesSnat, util.DepreciatedFinalizerName)
	controllerutil.AddFinalizer(newIptablesSnat, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedIptablesSnat, newIptablesSnat)
	if err != nil {
		klog.Errorf("failed to generate patch payload for iptables snat '%s', %v", cachedIptablesSnat.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().Patch(context.Background(), cachedIptablesSnat.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for iptables snat '%s', %v", cachedIptablesSnat.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIptablesSnatFinalizer(key string) error {
	cachedIptablesSnat, err := c.iptablesSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if len(cachedIptablesSnat.GetFinalizers()) == 0 {
		return nil
	}
	newIptablesSnat := cachedIptablesSnat.DeepCopy()
	controllerutil.RemoveFinalizer(newIptablesSnat, util.DepreciatedFinalizerName)
	controllerutil.RemoveFinalizer(newIptablesSnat, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedIptablesSnat, newIptablesSnat)
	if err != nil {
		klog.Errorf("failed to generate patch payload for iptables snat '%s', %v", cachedIptablesSnat.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().Patch(context.Background(), cachedIptablesSnat.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from iptables snat '%s', %v", cachedIptablesSnat.Name, err)
		return err
	}
	return nil
}

func (c *Controller) patchFipStatus(key, v4ip, v6ip, natGwDp, redo string, ready bool) error {
	oriFip, err := c.iptablesFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	fip := oriFip.DeepCopy()
	var changed bool
	if fip.Status.Ready != ready {
		fip.Status.Ready = ready
		changed = true
	}
	if redo != "" && fip.Status.Redo != redo {
		fip.Status.Redo = redo
		changed = true
	}

	if ready && v4ip != "" && fip.Status.V4ip != v4ip {
		fip.Status.V4ip = v4ip
		fip.Status.V6ip = v6ip
		fip.Status.NatGwDp = natGwDp
		changed = true
	}
	if ready && fip.Spec.InternalIP != "" && fip.Status.InternalIP != fip.Spec.InternalIP {
		fip.Status.InternalIP = fip.Spec.InternalIP
		changed = true
	}

	if changed {
		bytes, err := fip.Status.Bytes()
		if err != nil {
			klog.Error(err)
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Patch(context.Background(), fip.Name,
			types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			klog.Errorf("failed to patch fip %s, %v", fip.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) redoFip(key, redo string, eipReady bool) error {
	fip, err := c.iptablesFipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get fip %s, %v", key, err)
		return err
	}
	if redo != "" && redo != fip.Status.Redo {
		if !eipReady {
			if err = c.patchEipLabel(fip.Spec.EIP); err != nil {
				err = fmt.Errorf("failed to patch eip %s, %w", fip.Spec.EIP, err)
				klog.Error(err)
				return err
			}
			if err = c.patchEipStatus(fip.Spec.EIP, "", redo, "", false); err != nil {
				err = fmt.Errorf("failed to patch eip %s, %w", fip.Spec.EIP, err)
				klog.Error(err)
				return err
			}
		}
		if err = c.patchFipStatus(key, "", "", "", redo, false); err != nil {
			err = fmt.Errorf("failed to patch fip %s, %w", fip.Name, err)
			klog.Error(err)
			return err
		}
	}
	return err
}

func (c *Controller) patchDnatLabel(key string, eip *kubeovnv1.IptablesEIP) error {
	oriDnat, err := c.iptablesDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	dnat := oriDnat.DeepCopy()
	var needUpdateLabel, needUpdateAnno bool
	var op string
	if len(dnat.Labels) == 0 {
		op = "add"
		dnat.Labels = map[string]string{
			util.VpcNatGatewayNameLabel: eip.Spec.NatGwDp,
			util.VpcDnatEPortLabel:      dnat.Spec.ExternalPort,
			util.EipV4IpLabel:           eip.Spec.V4ip,
		}
		needUpdateLabel = true
	} else if dnat.Labels[util.VpcNatGatewayNameLabel] != eip.Spec.NatGwDp ||
		dnat.Labels[util.VpcDnatEPortLabel] != dnat.Spec.ExternalPort ||
		dnat.Labels[util.EipV4IpLabel] != eip.Spec.V4ip {
		op = "replace"
		dnat.Labels[util.VpcNatGatewayNameLabel] = eip.Spec.NatGwDp
		dnat.Labels[util.VpcDnatEPortLabel] = dnat.Spec.ExternalPort
		dnat.Labels[util.EipV4IpLabel] = eip.Spec.V4ip
		needUpdateLabel = true
	}
	if needUpdateLabel {
		if err := c.updateIptableLabels(dnat.Name, op, util.DnatUsingEip, dnat.Labels); err != nil {
			klog.Error(err)
			return err
		}
	}

	if len(dnat.Annotations) == 0 {
		op = "add"
		needUpdateAnno = true
		dnat.Annotations = map[string]string{
			util.VpcEipAnnotation: eip.Name,
		}
	} else if dnat.Annotations[util.VpcEipAnnotation] != eip.Name {
		op = "replace"
		needUpdateAnno = true
		dnat.Annotations[util.VpcEipAnnotation] = eip.Name
	}
	if needUpdateAnno {
		if err := c.updateIptableAnnotations(dnat.Name, op, util.DnatUsingEip, dnat.Annotations); err != nil {
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchDnatStatus(key, v4ip, v6ip, natGwDp, redo string, ready bool) error {
	oriDnat, err := c.iptablesDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	var changed bool
	dnat := oriDnat.DeepCopy()
	if dnat.Status.Ready != ready {
		dnat.Status.Ready = ready
		changed = true
	}
	if redo != "" && dnat.Status.Redo != redo {
		dnat.Status.Redo = redo
		changed = true
	}
	if ready && v4ip != "" && dnat.Status.V4ip != v4ip {
		dnat.Status.V4ip = v4ip
		dnat.Status.V6ip = v6ip
		dnat.Status.NatGwDp = natGwDp
		changed = true
	}
	if ready && dnat.Spec.Protocol != "" && dnat.Status.Protocol != dnat.Spec.Protocol {
		dnat.Status.Protocol = dnat.Spec.Protocol
		changed = true
	}
	if ready && dnat.Spec.InternalIP != "" && dnat.Status.InternalIP != dnat.Spec.InternalIP {
		dnat.Status.InternalIP = dnat.Spec.InternalIP
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
		if _, err = c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Patch(context.Background(), dnat.Name,
			types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			klog.Errorf("failed to patch dnat %s, %v", dnat.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) redoDnat(key, redo string, eipReady bool) error {
	dnat, err := c.iptablesDnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get dnat %s, %v", key, err)
		return err
	}
	if redo != "" && redo != dnat.Status.Redo {
		if !eipReady {
			if err = c.patchEipStatus(dnat.Spec.EIP, "", redo, "", false); err != nil {
				err = fmt.Errorf("failed to patch eip %s, %w", dnat.Spec.EIP, err)
				klog.Error(err)
				return err
			}
		}
		if err = c.patchDnatStatus(key, "", "", "", redo, false); err != nil {
			err = fmt.Errorf("failed to patch dnat %s, %w", key, err)
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchSnatLabel(key string, eip *kubeovnv1.IptablesEIP) error {
	oriSnat, err := c.iptablesSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	snat := oriSnat.DeepCopy()
	var needUpdateLabel, needUpdateAnno bool
	var op string
	if len(snat.Labels) == 0 {
		op = "add"
		snat.Labels = map[string]string{
			util.VpcNatGatewayNameLabel: eip.Spec.NatGwDp,
			util.EipV4IpLabel:           eip.Spec.V4ip,
		}
		needUpdateLabel = true
	} else if snat.Labels[util.SubnetNameLabel] != eip.Spec.NatGwDp ||
		snat.Labels[util.EipV4IpLabel] != eip.Spec.V4ip {
		op = "replace"
		snat.Labels[util.VpcNatGatewayNameLabel] = eip.Spec.NatGwDp
		snat.Labels[util.EipV4IpLabel] = eip.Spec.V4ip
		needUpdateLabel = true
	}
	if needUpdateLabel {
		if err := c.updateIptableLabels(snat.Name, op, util.SnatUsingEip, snat.Labels); err != nil {
			klog.Error(err)
			return err
		}
	}

	if len(snat.Annotations) == 0 {
		op = "add"
		needUpdateAnno = true
		snat.Annotations = map[string]string{
			util.VpcEipAnnotation: eip.Name,
		}
	} else if snat.Annotations[util.VpcEipAnnotation] != eip.Name {
		op = "replace"
		needUpdateAnno = true
		snat.Annotations[util.VpcEipAnnotation] = eip.Name
	}
	if needUpdateAnno {
		if err := c.updateIptableAnnotations(snat.Name, op, util.SnatUsingEip, snat.Annotations); err != nil {
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchSnatStatus(key, v4ip, v6ip, natGwDp, redo string, ready bool) error {
	oriSnat, err := c.iptablesSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	snat := oriSnat.DeepCopy()
	var changed bool
	if snat.Status.Ready != ready {
		snat.Status.Ready = ready
		changed = true
	}
	if redo != "" && snat.Status.Redo != redo {
		snat.Status.Redo = redo
		changed = true
	}
	if ready && v4ip != "" && snat.Status.V4ip != v4ip {
		snat.Status.V4ip = v4ip
		snat.Status.V6ip = v6ip
		snat.Status.NatGwDp = natGwDp
		changed = true
	}
	if ready && snat.Spec.InternalCIDR != "" {
		v4CidrSpec, _ := util.SplitStringIP(snat.Spec.InternalCIDR)
		if v4CidrSpec != "" {
			v4Cidr, _ := util.SplitStringIP(snat.Status.InternalCIDR)
			if v4Cidr != v4CidrSpec {
				snat.Status.InternalCIDR = v4CidrSpec
				changed = true
			}
		}
	}

	if changed {
		bytes, err := snat.Status.Bytes()
		if err != nil {
			klog.Error(err)
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().Patch(context.Background(), snat.Name,
			types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			klog.Errorf("failed to patch snat %s, %v", snat.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) redoSnat(key, redo string, eipReady bool) error {
	snat, err := c.iptablesSnatRulesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get snat %s, %v", key, err)
		return err
	}
	if redo != "" && redo != snat.Status.Redo {
		if !eipReady {
			if err = c.patchEipStatus(snat.Spec.EIP, "", redo, "", false); err != nil {
				err = fmt.Errorf("failed to patch eip %s, %w", snat.Spec.EIP, err)
				klog.Error(err)
				return err
			}
		}
		if err = c.patchSnatStatus(key, "", "", "", redo, false); err != nil {
			err = fmt.Errorf("failed to patch snat %s, %w", key, err)
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *Controller) createFipInPod(dp, v4ip, internalIP string) error {
	gwPod, err := c.getNatGwPod(dp, c.natGwNamespaceByName(dp))
	if err != nil {
		klog.Error(err)
		return err
	}
	var addRules []string
	rule := fmt.Sprintf("%s,%s", v4ip, internalIP)
	addRules = append(addRules, rule)
	if err = c.execNatGwRules(gwPod, natGwSubnetFipAdd, addRules); err != nil {
		klog.Errorf("failed to create fip, err: %v", err)
		return err
	}
	return nil
}

// finalDeleteFipInPod resolves (natGwDp, v4ip) from the FIP CR's Status,
// with best-effort fallback to the EIP resource when Status is incomplete.
// Then delegates to deleteFipInPod to execute the actual shell deletion.
//
// Used by both the delete path (Status may be empty) and the spec-change path
// (Status guaranteed non-empty by the V4ip guard).
func (c *Controller) finalDeleteFipInPod(key string, cachedFip *kubeovnv1.IptablesFIPRule) error {
	klog.V(3).Infof("final delete fip '%s' in pod", key)
	var firstErr error
	statusV4ip := cachedFip.Status.V4ip
	statusNatGwDp := cachedFip.Status.NatGwDp
	if statusV4ip == "" {
		klog.Warningf("fip %s has empty Status.V4ip, fallback to eip %s", key, cachedFip.Spec.EIP)
		eip, err := c.GetEip(cachedFip.Spec.EIP)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				klog.Errorf("fip %s: eip %s not found, skip pod cleanup", key, cachedFip.Spec.EIP)
				return nil
			}
			klog.Errorf("failed to get eip %s for fip %s, %v", cachedFip.Spec.EIP, key, err)
			return err
		}
		statusV4ip = eip.Status.IP
		if statusNatGwDp == "" {
			statusNatGwDp = eip.Spec.NatGwDp
		}
	}
	if statusV4ip == "" || statusNatGwDp == "" {
		klog.Warningf("fip %s: skip status-based cleanup due to incomplete identity (v4ip=%q, natGwDp=%q)", key, statusV4ip, statusNatGwDp)
	} else if err := c.deleteFipInPod(statusNatGwDp, statusV4ip); err != nil {
		klog.Errorf("failed to delete fip %s, %v", key, err)
		firstErr = err
	}

	// Spec-change crash: Status has old IP (V4ip != "") but Ready=false means a spec
	// change crashed midway. Pod may have a new-IP rule while Status still points to the old IP.
	if !cachedFip.Status.Ready && cachedFip.Status.V4ip != "" {
		eip, err := c.GetEip(cachedFip.Spec.EIP)
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return err
			}
			klog.Warningf("fip %s not ready: eip %s not found, skip spec-based cleanup", key, cachedFip.Spec.EIP)
			return firstErr
		}
		specV4ip := eip.Status.IP
		specNatGwDp := eip.Spec.NatGwDp
		if specV4ip == "" || specNatGwDp == "" {
			klog.Warningf("fip %s not ready: skip spec-based cleanup due to incomplete spec identity (v4ip=%q, natGwDp=%q)", key, specV4ip, specNatGwDp)
			return firstErr
		}
		if specV4ip != statusV4ip || specNatGwDp != statusNatGwDp {
			if err = c.deleteFipInPod(specNatGwDp, specV4ip); err != nil {
				klog.Errorf("failed spec-based cleanup for fip %s, %v", key, err)
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	return firstErr
}

// finalDeleteDnatInPod resolves (natGwDp, protocol, v4ip, externalPort) from the DNAT CR's
// Status, with best-effort fallback to EIP/Spec when Status is incomplete.
// Then delegates to deleteDnatInPod to execute the actual shell deletion.
func (c *Controller) finalDeleteDnatInPod(key string, cachedDnat *kubeovnv1.IptablesDnatRule) error {
	klog.V(3).Infof("final delete dnat '%s' in pod", key)
	var firstErr error
	statusV4ip := cachedDnat.Status.V4ip
	statusNatGwDp := cachedDnat.Status.NatGwDp
	if statusV4ip == "" {
		klog.Warningf("dnat %s has empty Status.V4ip, fallback to eip %s", key, cachedDnat.Spec.EIP)
		eip, err := c.GetEip(cachedDnat.Spec.EIP)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				klog.Errorf("dnat %s: eip %s not found, skip pod cleanup", key, cachedDnat.Spec.EIP)
				return nil
			}
			return err
		}
		statusV4ip = eip.Status.IP
		if statusNatGwDp == "" {
			statusNatGwDp = eip.Spec.NatGwDp
		}
	}
	statusProtocol := cachedDnat.Status.Protocol
	if statusProtocol == "" {
		klog.Warningf("dnat %s has empty Status.Protocol, fallback to Spec", key)
		statusProtocol = cachedDnat.Spec.Protocol
	}
	if statusProtocol == "" {
		klog.Errorf("dnat %s has v4ip %s but protocol is empty in both Status and Spec, skip pod cleanup", key, statusV4ip)
		return nil
	}
	statusExternalPort := cachedDnat.Status.ExternalPort
	if statusExternalPort == "" {
		klog.Warningf("dnat %s has empty Status.ExternalPort, fallback to Spec", key)
		statusExternalPort = cachedDnat.Spec.ExternalPort
	}
	if statusExternalPort == "" {
		klog.Errorf("dnat %s has v4ip %s but externalPort is empty in both Status and Spec, skip pod cleanup", key, statusV4ip)
		return nil
	}
	if statusV4ip == "" || statusNatGwDp == "" {
		klog.Warningf("dnat %s: skip status-based cleanup due to incomplete identity (v4ip=%q, natGwDp=%q)", key, statusV4ip, statusNatGwDp)
	} else if err := c.deleteDnatInPod(statusNatGwDp, statusProtocol,
		statusV4ip, statusExternalPort); err != nil {
		klog.Errorf("failed to delete dnat %s, %v", key, err)
		firstErr = err
	}

	// Spec-change crash: Status has old IP (V4ip != "") but Ready=false means a spec
	// change crashed midway. Pod may have a new-IP rule while Status still points to the old IP.
	if !cachedDnat.Status.Ready && cachedDnat.Status.V4ip != "" {
		eip, err := c.GetEip(cachedDnat.Spec.EIP)
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return err
			}
			klog.Warningf("dnat %s not ready: eip %s not found, skip spec-based cleanup", key, cachedDnat.Spec.EIP)
			return firstErr
		}
		specV4ip := eip.Status.IP
		specNatGwDp := eip.Spec.NatGwDp
		specProtocol := cachedDnat.Spec.Protocol
		specExternalPort := cachedDnat.Spec.ExternalPort
		if specV4ip == "" || specNatGwDp == "" || specProtocol == "" || specExternalPort == "" {
			klog.Warningf("dnat %s not ready: skip spec-based cleanup due to incomplete spec identity (v4ip=%q, natGwDp=%q, protocol=%q, externalPort=%q)",
				key, specV4ip, specNatGwDp, specProtocol, specExternalPort)
			return firstErr
		}
		if specV4ip != statusV4ip || specNatGwDp != statusNatGwDp || specProtocol != statusProtocol || specExternalPort != statusExternalPort {
			if err = c.deleteDnatInPod(specNatGwDp, specProtocol, specV4ip, specExternalPort); err != nil {
				klog.Errorf("failed spec-based cleanup for dnat %s, %v", key, err)
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	return firstErr
}

// finalDeleteSnatInPod resolves (natGwDp, v4ip, v4Cidr) from the SNAT CR's Status,
// with best-effort fallback to EIP/Spec when Status is incomplete.
// Then delegates to deleteSnatInPod to execute the actual shell deletion.
func (c *Controller) finalDeleteSnatInPod(key string, cachedSnat *kubeovnv1.IptablesSnatRule) error {
	klog.V(3).Infof("final delete snat '%s' in pod", key)
	var firstErr error
	statusV4ip := cachedSnat.Status.V4ip
	statusNatGwDp := cachedSnat.Status.NatGwDp
	if statusV4ip == "" {
		klog.Warningf("snat %s has empty Status.V4ip, fallback to eip %s", key, cachedSnat.Spec.EIP)
		eip, err := c.GetEip(cachedSnat.Spec.EIP)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				klog.Errorf("snat %s: eip %s not found, skip pod cleanup", key, cachedSnat.Spec.EIP)
				return nil
			}
			return err
		}
		statusV4ip = eip.Status.IP
		if statusNatGwDp == "" {
			statusNatGwDp = eip.Spec.NatGwDp
		}
	}
	statusV4Cidr, _ := util.SplitStringIP(cachedSnat.Status.InternalCIDR)
	if statusV4Cidr == "" {
		klog.Warningf("snat %s has empty Status.InternalCIDR, fallback to Spec", key)
		statusV4Cidr, _ = util.SplitStringIP(cachedSnat.Spec.InternalCIDR)
	}
	if statusV4Cidr == "" {
		klog.Errorf("snat %s has v4ip %s but v4Cidr is empty in both Status and Spec, skip pod cleanup", key, statusV4ip)
		return nil
	}
	if statusV4ip == "" || statusNatGwDp == "" {
		klog.Warningf("snat %s: skip status-based cleanup due to incomplete identity (v4ip=%q, natGwDp=%q)", key, statusV4ip, statusNatGwDp)
	} else if err := c.deleteSnatInPod(statusNatGwDp, statusV4ip, statusV4Cidr); err != nil {
		klog.Errorf("failed to delete snat %s, %v", key, err)
		firstErr = err
	}

	// Spec-change crash: Status has old IP (V4ip != "") but Ready=false means a spec
	// change crashed midway. Pod may have a new-IP rule while Status still points to the old IP.
	if !cachedSnat.Status.Ready && cachedSnat.Status.V4ip != "" {
		eip, err := c.GetEip(cachedSnat.Spec.EIP)
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return err
			}
			klog.Warningf("snat %s not ready: eip %s not found, skip spec-based cleanup", key, cachedSnat.Spec.EIP)
			return firstErr
		}
		specV4ip := eip.Status.IP
		specNatGwDp := eip.Spec.NatGwDp
		specV4Cidr, _ := util.SplitStringIP(cachedSnat.Spec.InternalCIDR)
		if specV4ip == "" || specNatGwDp == "" || specV4Cidr == "" {
			klog.Warningf("snat %s not ready: skip spec-based cleanup due to incomplete spec identity (v4ip=%q, natGwDp=%q, v4Cidr=%q)",
				key, specV4ip, specNatGwDp, specV4Cidr)
			return firstErr
		}
		if specV4ip != statusV4ip || specNatGwDp != statusNatGwDp || specV4Cidr != statusV4Cidr {
			if err = c.deleteSnatInPod(specNatGwDp, specV4ip, specV4Cidr); err != nil {
				klog.Errorf("failed spec-based cleanup for snat %s, %v", key, err)
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	return firstErr
}

func (c *Controller) deleteFipInPod(dp, v4ip string) error {
	gwPod, err := c.getNatGwPod(dp, c.natGwNamespaceByName(dp))
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	// del_floating_ip matches by EIP only (FIP is 1:1, identity = EIP)
	if err = c.execNatGwRules(gwPod, natGwSubnetFipDel, []string{v4ip}); err != nil {
		klog.Errorf("failed to delete fip, err: %v", err)
		return err
	}
	return nil
}

func (c *Controller) createDnatInPod(dp, protocol, v4ip, internalIP, externalPort, internalPort string) error {
	gwPod, err := c.getNatGwPod(dp, c.natGwNamespaceByName(dp))
	if err != nil {
		klog.Errorf("failed to get nat gw pod, %v", err)
		return err
	}
	var addRules []string
	rule := fmt.Sprintf("%s,%s,%s,%s,%s", v4ip, externalPort, protocol, internalIP, internalPort)
	addRules = append(addRules, rule)

	if err = c.execNatGwRules(gwPod, natGwDnatAdd, addRules); err != nil {
		klog.Errorf("failed to create dnat, err: %v", err)
		return err
	}
	return nil
}

func (c *Controller) deleteDnatInPod(dp, protocol, v4ip, externalPort string) error {
	gwPod, err := c.getNatGwPod(dp, c.natGwNamespaceByName(dp))
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	// del_dnat matches by identity triplet (EIP, ExternalPort, Protocol) only
	rule := fmt.Sprintf("%s,%s,%s", v4ip, externalPort, protocol)
	if err = c.execNatGwRules(gwPod, natGwDnatDel, []string{rule}); err != nil {
		klog.Errorf("failed to delete dnat, err: %v", err)
		return err
	}
	return nil
}

func (c *Controller) createSnatInPod(dp, v4ip, internalCIDR string) error {
	gwPod, err := c.getNatGwPod(dp, c.natGwNamespaceByName(dp))
	if err != nil {
		klog.Errorf("failed to get nat gw pod, %v", err)
		return err
	}
	var rules []string
	rule := fmt.Sprintf("%s,%s", v4ip, internalCIDR)

	version, err := c.getIptablesVersion(gwPod)
	if err != nil {
		version = "1.0.0"
		klog.Warningf("failed to checking iptables version, assuming version at least %s: %v", version, err)
	}
	if util.CompareVersion(version, "1.6.2") >= 1 {
		rule = fmt.Sprintf("%s,%s", rule, "--random-fully")
	}

	rules = append(rules, rule)
	if err = c.execNatGwRules(gwPod, natGwSnatAdd, rules); err != nil {
		klog.Errorf("failed to exec nat gateway rule, err: %v", err)
		return err
	}
	return nil
}

func (c *Controller) deleteSnatInPod(dp, v4ip, internalCIDR string) error {
	gwPod, err := c.getNatGwPod(dp, c.natGwNamespaceByName(dp))
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	// del nat
	var delRules []string
	rule := fmt.Sprintf("%s,%s", v4ip, internalCIDR)
	delRules = append(delRules, rule)
	if err = c.execNatGwRules(gwPod, natGwSnatDel, delRules); err != nil {
		klog.Errorf("failed to delete snat, err: %v", err)
		return err
	}
	return nil
}

func (c *Controller) isDnatDuplicated(gwName, eipName, dnatName, externalPort, protocol string) (bool, error) {
	// Check if the tuple "eip:external port:protocol" is already used by another DNAT rule
	dnats, err := c.iptablesDnatRulesLister.List(labels.SelectorFromSet(labels.Set{
		util.VpcNatGatewayNameLabel: gwName,
		util.VpcDnatEPortLabel:      externalPort,
	}))
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return false, err
		}
	}
	if len(dnats) != 0 {
		for _, d := range dnats {
			if d.Name != dnatName && d.Spec.EIP == eipName && d.Spec.Protocol == protocol {
				err = fmt.Errorf("failed to create dnat %s, duplicate, same eip %s, same external port '%s', same protocol'%s' is using by dnat %s", dnatName, eipName, externalPort, protocol, d.Name)
				return true, err
			}
		}
	}
	return false, nil
}

func (c *Controller) updateIptableLabels(name, op, natType string, labels map[string]string) error {
	patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
	raw, _ := json.Marshal(labels)
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)

	if err := c.patchIptableInfo(name, natType, patchPayload); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to patch label for %s %s, %v", natType, name, err)
		return err
	}
	return nil
}

func (c *Controller) updateIptableAnnotations(name, op, natType string, anno map[string]string) error {
	patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/annotations", "value": %s }]`
	raw, _ := json.Marshal(anno)
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)

	if err := c.patchIptableInfo(name, natType, patchPayload); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to patch annotations for %s %s, %v", natType, name, err)
		return err
	}
	return nil
}

func (c *Controller) patchIptableInfo(name, natType, patchPayload string) error {
	var err error
	switch natType {
	case util.FipUsingEip:
		_, err = c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Patch(context.Background(), name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
	case util.SnatUsingEip:
		_, err = c.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().Patch(context.Background(), name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
	case util.DnatUsingEip:
		_, err = c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().Patch(context.Background(), name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
	case "eip":
		_, err = c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Patch(context.Background(), name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
	}
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	return nil
}

// validateDnatRule validates IptablesDnatRule fields to prevent malformed iptables commands.
func (c *Controller) validateDnatRule(dnat *kubeovnv1.IptablesDnatRule) error {
	var err error
	if dnat.Spec.EIP == "" {
		err = fmt.Errorf("%s: eip cannot be empty", dnat.Name)
		klog.Error(err)
		return err
	}
	if err = util.ValidatePort(dnat.Spec.ExternalPort); err != nil {
		err = fmt.Errorf("%s: invalid externalPort %q: %w", dnat.Name, dnat.Spec.ExternalPort, err)
		klog.Error(err)
		return err
	}
	if err = util.ValidatePort(dnat.Spec.InternalPort); err != nil {
		err = fmt.Errorf("%s: invalid internalPort %q: %w", dnat.Name, dnat.Spec.InternalPort, err)
		klog.Error(err)
		return err
	}
	if dnat.Spec.InternalIP == "" {
		err = fmt.Errorf("%s: internalIP cannot be empty", dnat.Name)
		klog.Error(err)
		return err
	}
	if !util.IsValidIP(dnat.Spec.InternalIP) {
		err = fmt.Errorf("%s: invalid internalIP %q", dnat.Name, dnat.Spec.InternalIP)
		klog.Error(err)
		return err
	}
	// iptables NAT only supports IPv4
	if util.CheckProtocol(dnat.Spec.InternalIP) != kubeovnv1.ProtocolIPv4 {
		err = fmt.Errorf("%s: internalIP %q must be IPv4, IPv6 is not supported", dnat.Name, dnat.Spec.InternalIP)
		klog.Error(err)
		return err
	}
	if err = util.ValidateProtocol(dnat.Spec.Protocol); err != nil {
		err = fmt.Errorf("%s: invalid protocol %q: %w", dnat.Name, dnat.Spec.Protocol, err)
		klog.Error(err)
		return err
	}
	return nil
}

// validateFipRule validates IptablesFIPRule fields to prevent malformed iptables commands.
func (c *Controller) validateFipRule(fip *kubeovnv1.IptablesFIPRule) error {
	var err error
	if fip.Spec.EIP == "" {
		err = fmt.Errorf("%s: eip cannot be empty", fip.Name)
		klog.Error(err)
		return err
	}
	if fip.Spec.InternalIP == "" {
		err = fmt.Errorf("%s: internalIP cannot be empty", fip.Name)
		klog.Error(err)
		return err
	}
	if !util.IsValidIP(fip.Spec.InternalIP) {
		err = fmt.Errorf("%s: invalid internalIP %q", fip.Name, fip.Spec.InternalIP)
		klog.Error(err)
		return err
	}
	// iptables NAT only supports IPv4
	if util.CheckProtocol(fip.Spec.InternalIP) != kubeovnv1.ProtocolIPv4 {
		err = fmt.Errorf("%s: internalIP %q must be IPv4, IPv6 is not supported", fip.Name, fip.Spec.InternalIP)
		klog.Error(err)
		return err
	}
	return nil
}

// validateSnatRule validates IptablesSnatRule fields to prevent malformed iptables commands.
func (c *Controller) validateSnatRule(snat *kubeovnv1.IptablesSnatRule) error {
	var err error
	if snat.Spec.EIP == "" {
		err = fmt.Errorf("%s: eip cannot be empty", snat.Name)
		klog.Error(err)
		return err
	}
	internalCIDR := snat.Spec.InternalCIDR
	if internalCIDR == "" {
		err = fmt.Errorf("%s: internalCIDR cannot be empty", snat.Name)
		klog.Error(err)
		return err
	}
	// iptables NAT only supports single IPv4 CIDR or IP, ip6tables is not used here
	if strings.Count(internalCIDR, "/") > 1 {
		err = fmt.Errorf("%s: internalCIDR %q contains multiple CIDRs, only single CIDR or IP is allowed", snat.Name, internalCIDR)
		klog.Error(err)
		return err
	}
	if strings.Contains(internalCIDR, "/") {
		if err = util.CheckCidrs(internalCIDR); err != nil {
			err = fmt.Errorf("%s: invalid internalCIDR %q: %w", snat.Name, internalCIDR, err)
			klog.Error(err)
			return err
		}
	} else {
		if !util.IsValidIP(internalCIDR) {
			err = fmt.Errorf("%s: invalid internalCIDR %q", snat.Name, internalCIDR)
			klog.Error(err)
			return err
		}
	}
	if util.CheckProtocol(internalCIDR) != kubeovnv1.ProtocolIPv4 {
		err = fmt.Errorf("%s: internalCIDR %q must be IPv4, IPv6 is not supported", snat.Name, internalCIDR)
		klog.Error(err)
		return err
	}
	return nil
}
