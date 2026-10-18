package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"

	corev1 "k8s.io/api/core/v1"
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

func (c *Controller) enqueueAddIPPool(obj any) {
	ippool := obj.(*kubeovnv1.IPPool)
	// An informer (re)list delivers an ippool that is already terminating only via the
	// add handler, so route it to the delete queue like the update handler does;
	// otherwise the finalizer is never removed after a controller restart.
	if !ippool.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("enqueue delete ippool %s due to deletion timestamp", cache.MetaObjectToName(ippool).String())
		c.deleteIPPoolQueue.Add(ippool)
		return
	}
	key := cache.MetaObjectToName(ippool).String()
	klog.V(3).Infof("enqueue add ippool %s", key)
	c.addOrUpdateIPPoolQueue.Add(key)
}

func (c *Controller) enqueueDeleteIPPool(obj any) {
	var ippool *kubeovnv1.IPPool
	switch t := obj.(type) {
	case *kubeovnv1.IPPool:
		ippool = t
	case cache.DeletedFinalStateUnknown:
		i, ok := t.Obj.(*kubeovnv1.IPPool)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		ippool = i
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	klog.V(3).Infof("enqueue delete ippool %s", cache.MetaObjectToName(ippool).String())
	c.deleteIPPoolQueue.Add(ippool)
}

func (c *Controller) enqueueUpdateIPPool(oldObj, newObj any) {
	oldIPPool := oldObj.(*kubeovnv1.IPPool)
	newIPPool := newObj.(*kubeovnv1.IPPool)
	if !newIPPool.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("enqueue delete ippool %s due to deletion timestamp", cache.MetaObjectToName(newIPPool).String())
		c.deleteIPPoolQueue.Add(newIPPool)
		return
	}
	if !slices.Equal(oldIPPool.Spec.Namespaces, newIPPool.Spec.Namespaces) ||
		!slices.Equal(oldIPPool.Spec.IPs, newIPPool.Spec.IPs) ||
		oldIPPool.Spec.IPCount != newIPPool.Spec.IPCount ||
		oldIPPool.Spec.EnableAddressSet != newIPPool.Spec.EnableAddressSet {
		key := cache.MetaObjectToName(newIPPool).String()
		klog.V(3).Infof("enqueue update ippool %s", key)
		c.addOrUpdateIPPoolQueue.Add(key)
	}
}

func (c *Controller) handleAddOrUpdateIPPool(key string) error {
	c.ippoolKeyMutex.LockKey(key)
	defer func() { _ = c.ippoolKeyMutex.UnlockKey(key) }()

	cachedIPPool, err := c.ippoolLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return c.recordIPPoolKeyError(key, "GetIPPoolFailed", err)
	}
	klog.Infof("handle add/update ippool %s", cachedIPPool.Name)

	ippool := cachedIPPool.DeepCopy()
	if err = c.handleAddIPPoolFinalizer(ippool); err != nil {
		klog.Errorf("failed to add finalizer for ippool %s: %v", ippool.Name, err)
		return c.recordResourceError(ippool, "UpdateFinalizerFailed", err)
	}
	if !ippool.DeletionTimestamp.IsZero() {
		klog.Infof("ippool %s is being deleted, skip add/update handling", ippool.Name)
		return nil
	}
	ippool.Status.EnsureStandardConditions()
	// select the membership without exposing it to pod allocation: it becomes visible
	// only at the activation step below, once the other prerequisites hold
	ips, subnet, err := c.selectIPPoolIPs(ippool)
	if err != nil {
		klog.Errorf("failed to select IPs for ippool %s in subnet %s: %v", ippool.Name, ippool.Spec.Subnet, err)
		if patchErr := c.patchIPPoolStatusCondition(ippool, "SelectIPsFailed", err.Error()); patchErr != nil {
			klog.Error(patchErr)
			return errors.Join(err, c.recordResourceError(ippool, "UpdateStatusFailed", patchErr))
		}
		return err
	}
	// persist the selection first: after a controller restart the pool is restored from
	// status.ips, which then already describes the membership waiting to be activated
	switch {
	case ippool.Spec.IPCount > 0:
		if err = c.patchIPPoolIPs(ippool, ips); err != nil {
			klog.Error(err)
			return c.recordResourceError(ippool, "UpdateStatusFailed", err)
		}
	case len(ippool.Status.IPs) > 0:
		// the pool left count mode: clear the recorded addresses, they describe the
		// former selection and must not be reused when the pool re-enters count mode
		if err = c.patchIPPoolIPs(ippool, nil); err != nil {
			klog.Error(err)
			return c.recordResourceError(ippool, "UpdateStatusFailed", err)
		}
	}
	// the address set must match the new membership before it becomes allocatable, so
	// that no consumer can get a pool address the data plane does not match yet
	if err = c.reconcileIPPoolAddressSet(ippool, ips); err != nil {
		klog.Errorf("failed to reconcile address set for ippool %s: %v", ippool.Name, err)
		if patchErr := c.patchIPPoolStatusCondition(ippool, "ReconcileAddressSetFailed", err.Error()); patchErr != nil {
			klog.Error(patchErr)
			return errors.Join(err, c.recordResourceError(ippool, "UpdateStatusFailed", patchErr))
		}
		return err
	}
	// activate the membership in IPAM only now that persistence and the data plane both
	// describe exactly this selection; the activation verifies that it is still the
	// pickable one and abandons the reconcile without committing otherwise, since a
	// membership consumers could observe must equal the published one
	if err = c.activateIPPoolIPs(ippool, subnet, ips); err != nil {
		klog.Errorf("failed to activate IPs %v for ippool %s in subnet %s: %v", ips, ippool.Name, ippool.Spec.Subnet, err)
		if patchErr := c.patchIPPoolStatusCondition(ippool, "ActivateIPsFailed", err.Error()); patchErr != nil {
			klog.Error(patchErr)
			return errors.Join(err, c.recordResourceError(ippool, "UpdateStatusFailed", patchErr))
		}
		return err
	}

	c.updateIPPoolStatistics(ippool)
	if err = c.patchIPPoolStatusCondition(ippool, "UpdateIPAMSucceeded", ""); err != nil {
		klog.Error(err)
		return c.recordResourceError(ippool, "UpdateStatusFailed", err)
	}

	for _, ns := range ippool.Spec.Namespaces {
		c.addNamespaceQueue.Add(ns)
	}

	return nil
}

// ippoolIPs returns the addresses the pool currently consists of: the ones picked from
// the subnet and recorded in status.ips when spec.ipCount is used, spec.ips otherwise.
func ippoolIPs(ippool *kubeovnv1.IPPool) []string {
	if ippool.Spec.IPCount > 0 {
		return ippool.Status.IPs
	}
	return ippool.Spec.IPs
}

// maxIPPoolIPCount bounds spec.ipCount: an ippool is meant to carve a small set of
// addresses out of a subnet, larger needs are served by a dedicated subnet or by listing
// ranges in spec.ips. The CRD rejects larger values; this check also covers pools created
// before that validation existed.
const maxIPPoolIPCount = 32

// selectIPPoolIPs picks the addresses of a pool using spec.ipCount, without
// committing them to IPAM: they become allocatable only when activateIPPoolIPs
// commits the same selection after the data plane and persistence prerequisites
// succeeded. Addresses recorded in status.ips are retained while they stay usable and
// unclaimed by others, addresses the pool already holds in IPAM - e.g. committed by a
// previous reconcile a stale informer cache does not know about - are retained next,
// and missing ones are taken from the subnet's free addresses. The selection stays
// stable across reconciles and restarts: growing appends newly picked addresses,
// shrinking drops the most recently added ones first. For a pool listing spec.ips it
// simply returns that listing, and subnet is nil.
func (c *Controller) selectIPPoolIPs(ippool *kubeovnv1.IPPool) ([]string, *kubeovnv1.Subnet, error) {
	if ippool.Spec.IPCount <= 0 {
		return ippool.Spec.IPs, nil, nil
	}
	if ippool.Spec.IPCount > maxIPPoolIPCount {
		return nil, nil, fmt.Errorf("ippool %s requests %d addresses per IP family, more than the maximum of %d",
			ippool.Name, ippool.Spec.IPCount, maxIPPoolIPCount)
	}

	subnet, err := c.subnetsLister.Get(ippool.Spec.Subnet)
	if err != nil {
		klog.Errorf("failed to get subnet %s: %v", ippool.Spec.Subnet, err)
		return nil, nil, err
	}

	wantV4, wantV6 := ipPoolFamilyCounts(ippool.Spec.IPCount, subnet)

	// offer the recorded addresses that still belong to the subnet as keep candidates;
	// IPAM revalidates them against its own state and replaces stale ones itself
	kept := make([]string, 0, len(ippool.Status.IPs))
	for _, ip := range ippool.Status.IPs {
		if util.CIDRContainIP(subnet.Spec.CIDRBlock, ip) {
			kept = append(kept, ip)
		}
	}
	v4, v6, err := c.ipam.SelectIPPoolIPs(subnet.Name, ippool.Name, kept, wantV4, wantV6)
	if err != nil {
		klog.Error(err)
		return nil, nil, err
	}
	return append(v4, v6...), subnet, nil
}

// ipPoolFamilyCounts returns the per-family address counts a pool using spec.ipCount
// requests on the given subnet.
func ipPoolFamilyCounts(count int32, subnet *kubeovnv1.Subnet) (v4, v6 int) {
	v4, v6 = int(count), int(count)
	// subnet.Spec.Protocol may still be empty right after the subnet is created
	switch util.CheckProtocol(subnet.Spec.CIDRBlock) {
	case kubeovnv1.ProtocolIPv4:
		v6 = 0
	case kubeovnv1.ProtocolIPv6:
		v4 = 0
	}
	return v4, v6
}

// activateIPPoolIPs commits the membership a pool published - in status.ips for
// count-based pools, in the address set for both kinds - making its addresses
// allocatable. For a count-based pool the commit is verified against the current IPAM
// state: if part of the selection was allocated elsewhere in the meantime it fails
// without committing anything, and the retried reconcile selects and publishes again.
func (c *Controller) activateIPPoolIPs(ippool *kubeovnv1.IPPool, subnet *kubeovnv1.Subnet, ips []string) error {
	if ippool.Spec.IPCount <= 0 {
		return c.ipam.AddOrUpdateIPPool(ippool.Spec.Subnet, ippool.Name, ips)
	}
	v4Count, v6Count := ipPoolFamilyCounts(ippool.Spec.IPCount, subnet)
	return c.ipam.ActivateIPPoolIPs(subnet.Name, ippool.Name, ips, v4Count, v6Count)
}

func (c *Controller) handleDeleteIPPool(ippool *kubeovnv1.IPPool) (err error) {
	c.ippoolKeyMutex.LockKey(ippool.Name)
	defer func() { _ = c.ippoolKeyMutex.UnlockKey(ippool.Name) }()
	defer func() {
		if err != nil {
			_ = c.recordResourceError(ippool, "DeleteFailed", fmt.Errorf("failed to delete ippool %s: %w", ippool.Name, err))
		}
	}()
	hadFinalizer := controllerutil.ContainsFinalizer(ippool, util.DeprecatedFinalizerName) ||
		controllerutil.ContainsFinalizer(ippool, util.KubeOVNControllerFinalizer)

	klog.Infof("handle delete ippool %s", ippool.Name)
	c.ipam.RemoveIPPool(ippool.Spec.Subnet, ippool.Name)
	if err = c.OVNNbClient.DeleteAddressSet(util.IPPoolAddressSetName(ippool.Name)); err != nil {
		klog.Errorf("failed to delete address set for ippool %s: %v", ippool.Name, err)
		return err
	}

	namespaces, err := c.namespacesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list namespaces: %v", err)
		return err
	}

	for _, ns := range namespaces {
		if ns.Annotations[util.IPPoolAnnotation] == ippool.Name {
			c.enqueueAddNamespace(ns)
		}
	}

	if err = c.handleDelIPPoolFinalizer(ippool); err != nil {
		klog.Errorf("failed to remove finalizer for ippool %s: %v", ippool.Name, err)
		return err
	}

	if hadFinalizer {
		c.recordResourceEvent(ippool, corev1.EventTypeNormal, "DeleteSuccess", fmt.Sprintf("IPPool %s deleted successfully", ippool.Name))
	}

	return nil
}

func (c *Controller) handleUpdateIPPoolStatus(key string) error {
	c.ippoolKeyMutex.LockKey(key)
	defer func() { _ = c.ippoolKeyMutex.UnlockKey(key) }()

	cachedIPPool, err := c.ippoolLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return c.recordIPPoolKeyError(key, "GetIPPoolFailed", err)
	}

	ippool := cachedIPPool.DeepCopy()
	c.updateIPPoolStatistics(ippool)
	if reflect.DeepEqual(ippool.Status, cachedIPPool.Status) {
		return nil
	}

	if err := c.patchIPPoolStatus(ippool); err != nil {
		return c.recordResourceError(ippool, "UpdateStatusFailed", err)
	}
	return nil
}

func (c *Controller) patchIPPoolStatusCondition(ippool *kubeovnv1.IPPool, reason, errMsg string) error {
	if errMsg != "" {
		ippool.Status.SetError(reason, errMsg)
		ippool.Status.NotReady(reason, errMsg)
		c.recordResourceEvent(ippool, corev1.EventTypeWarning, reason, errMsg)
		return c.patchIPPoolStatus(ippool)
	}

	ippool.Status.Ready(reason, "")
	if err := c.patchIPPoolStatus(ippool); err != nil {
		return err
	}
	// to observe ippool status change to normal
	c.recordResourceEvent(ippool, corev1.EventTypeNormal, reason, fmt.Sprintf("IPPool %s reconciled successfully", ippool.Name))
	return nil
}

// patchIPPoolIPs records the addresses picked for a pool using spec.ipCount. It is the
// only writer of status.ips: patchIPPoolStatus deliberately leaves the field out, so that
// a statistics update built from a stale informer cache cannot roll the addresses back.
func (c *Controller) patchIPPoolIPs(ippool *kubeovnv1.IPPool, ips []string) error {
	if slices.Equal(ippool.Status.IPs, ips) {
		return nil
	}
	bytes, err := json.Marshal(map[string]any{"status": map[string]any{"ips": ips}})
	if err != nil {
		klog.Errorf("failed to generate json representation for IPs of ippool %s: %v", ippool.Name, err)
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().IPPools().Patch(context.Background(), ippool.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
		klog.Errorf("failed to patch IPs of ippool %s: %v", ippool.Name, err)
		return err
	}
	ippool.Status.IPs = ips
	return nil
}

func (c *Controller) patchIPPoolStatus(ippool *kubeovnv1.IPPool) error {
	status := ippool.Status
	status.IPs = nil // owned by patchIPPoolIPs, see the comment there
	bytes, err := status.Bytes()
	if err != nil {
		klog.Errorf("failed to generate json representation for status of ippool %s: %v", ippool.Name, err)
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().IPPools().Patch(context.Background(), ippool.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
		klog.Errorf("failed to patch status of ippool %s: %v", ippool.Name, err)
		return err
	}

	return nil
}

func (c *Controller) syncIPPoolFinalizer(cl client.Client) error {
	ippools := &kubeovnv1.IPPoolList{}
	return migrateFinalizers(cl, ippools, func(i int) (client.Object, client.Object) {
		if i < 0 || i >= len(ippools.Items) {
			return nil, nil
		}
		return ippools.Items[i].DeepCopy(), ippools.Items[i].DeepCopy()
	})
}

func (c *Controller) handleAddIPPoolFinalizer(ippool *kubeovnv1.IPPool) error {
	if !ippool.DeletionTimestamp.IsZero() {
		return nil
	}
	if controllerutil.ContainsFinalizer(ippool, util.KubeOVNControllerFinalizer) {
		return nil
	}

	newIPPool := ippool.DeepCopy()
	controllerutil.AddFinalizer(newIPPool, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(ippool, newIPPool)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ippool %s: %v", ippool.Name, err)
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().IPPools().Patch(context.Background(), ippool.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for ippool %s: %v", ippool.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIPPoolFinalizer(ippool *kubeovnv1.IPPool) error {
	if ippool == nil || len(ippool.GetFinalizers()) == 0 {
		return nil
	}

	newIPPool := ippool.DeepCopy()
	controllerutil.RemoveFinalizer(newIPPool, util.DeprecatedFinalizerName)
	controllerutil.RemoveFinalizer(newIPPool, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(ippool, newIPPool)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ippool %s: %v", ippool.Name, err)
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().IPPools().Patch(context.Background(), ippool.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from ippool %s: %v", ippool.Name, err)
		return err
	}
	return nil
}

func (c *Controller) updateIPPoolStatistics(ippool *kubeovnv1.IPPool) {
	v4a, v4u, v6a, v6u, v4as, v4us, v6as, v6us := c.ipam.IPPoolStatistics(ippool.Spec.Subnet, ippool.Name)
	ippool.Status.V4AvailableIPs = v4a
	ippool.Status.V4UsingIPs = v4u
	ippool.Status.V6AvailableIPs = v6a
	ippool.Status.V6UsingIPs = v6u
	ippool.Status.V4AvailableIPRange = v4as
	ippool.Status.V4UsingIPRange = v4us
	ippool.Status.V6AvailableIPRange = v6as
	ippool.Status.V6UsingIPRange = v6us
}

func (c *Controller) reconcileIPPoolAddressSet(ippool *kubeovnv1.IPPool, ips []string) error {
	asName := util.IPPoolAddressSetName(ippool.Name)

	if !ippool.Spec.EnableAddressSet {
		if err := c.OVNNbClient.DeleteAddressSet(asName); err != nil {
			err = fmt.Errorf("failed to delete address set %s: %w", asName, err)
			klog.Error(err)
			return err
		}
		return nil
	}

	addresses, err := util.ExpandIPPoolAddressesForOVN(ips)
	if err != nil {
		err = fmt.Errorf("failed to build address set entries for ippool %s: %w", ippool.Name, err)
		klog.Error(err)
		return err
	}

	if err := c.OVNNbClient.CreateAddressSet(asName, map[string]string{ippoolKey: ippool.Name}); err != nil {
		err = fmt.Errorf("failed to create address set for ippool %s: %w", ippool.Name, err)
		klog.Error(err)
		return err
	}

	if err := c.OVNNbClient.AddressSetUpdateAddress(asName, addresses...); err != nil {
		err = fmt.Errorf("failed to update address set for ippool %s: %w", ippool.Name, err)
		klog.Error(err)
		return err
	}

	return nil
}
