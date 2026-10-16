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
	ips, err := c.resolveIPPoolIPs(ippool)
	if err != nil {
		klog.Errorf("failed to resolve IPs for ippool %s: %v", ippool.Name, err)
		if patchErr := c.patchIPPoolStatusCondition(ippool, "ResolveIPsFailed", err.Error()); patchErr != nil {
			klog.Error(patchErr)
			return errors.Join(err, c.recordResourceError(ippool, "UpdateStatusFailed", patchErr))
		}
		return err
	}
	if err = c.reconcileIPPoolAddressSet(ippool, ips); err != nil {
		klog.Errorf("failed to reconcile address set for ippool %s: %v", ippool.Name, err)
		if patchErr := c.patchIPPoolStatusCondition(ippool, "ReconcileAddressSetFailed", err.Error()); patchErr != nil {
			klog.Error(patchErr)
			return errors.Join(err, c.recordResourceError(ippool, "UpdateStatusFailed", patchErr))
		}
		return err
	}
	if err = c.ipam.AddOrUpdateIPPool(ippool.Spec.Subnet, ippool.Name, ips); err != nil {
		klog.Errorf("failed to add/update ippool %s with IPs %v in subnet %s: %v", ippool.Name, ips, ippool.Spec.Subnet, err)
		if patchErr := c.patchIPPoolStatusCondition(ippool, "UpdateIPAMFailed", err.Error()); patchErr != nil {
			klog.Error(patchErr)
			return errors.Join(err, c.recordResourceError(ippool, "UpdateStatusFailed", patchErr))
		}
		return err
	}

	c.updateIPPoolStatistics(ippool)
	if ippool.Spec.IPCount > 0 {
		// record the picked addresses only after IPAM accepted them, so that a rejected
		// selection (e.g. conflicting with another pool) is recomputed on the next retry.
		// a failure here leaves the pool usable - IPAM already holds the addresses - so it
		// is reported as an error to requeue instead of marking the pool not ready
		if err = c.patchIPPoolIPs(ippool, ips); err != nil {
			klog.Error(err)
			return c.recordResourceError(ippool, "UpdateStatusFailed", err)
		}
	}

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

// resolveIPPoolIPs returns the addresses the pool should consist of. With spec.ipCount
// set, addresses are picked from the subnet's free addresses and recorded in status.ips
// by the caller, once IPAM accepted them, so that they stay stable across reconciles and
// restarts: growing appends newly picked addresses, shrinking drops the most recently
// added ones first.
func (c *Controller) resolveIPPoolIPs(ippool *kubeovnv1.IPPool) ([]string, error) {
	if ippool.Spec.IPCount <= 0 {
		return ippool.Spec.IPs, nil
	}
	if ippool.Spec.IPCount > maxIPPoolIPCount {
		return nil, fmt.Errorf("ippool %s requests %d addresses per IP family, more than the maximum of %d",
			ippool.Name, ippool.Spec.IPCount, maxIPPoolIPCount)
	}

	subnet, err := c.subnetsLister.Get(ippool.Spec.Subnet)
	if err != nil {
		klog.Errorf("failed to get subnet %s: %v", ippool.Spec.Subnet, err)
		return nil, err
	}

	wantV4, wantV6 := int(ippool.Spec.IPCount), int(ippool.Spec.IPCount)
	// subnet.Spec.Protocol may still be empty right after the subnet is created
	switch util.CheckProtocol(subnet.Spec.CIDRBlock) {
	case kubeovnv1.ProtocolIPv4:
		wantV6 = 0
	case kubeovnv1.ProtocolIPv6:
		wantV4 = 0
	}

	// drop addresses that no longer belong to the subnet, e.g. after a CIDR change
	kept := make([]string, 0, len(ippool.Status.IPs))
	for _, ip := range ippool.Status.IPs {
		if util.CIDRContainIP(subnet.Spec.CIDRBlock, ip) {
			kept = append(kept, ip)
		}
	}
	v4, v6 := util.SplitIpsByProtocol(kept)
	v4, v6 = trimIPs(v4, wantV4), trimIPs(v6, wantV6)

	newV4, newV6, err := c.ipam.GetFreeIPs(ippool.Spec.Subnet, wantV4-len(v4), wantV6-len(v6))
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	v4, v6 = append(v4, newV4...), append(v6, newV6...)
	if len(v4) < wantV4 || len(v6) < wantV6 {
		return nil, fmt.Errorf("subnet %s does not have enough free addresses for ippool %s: got %d/%d IPv4 and %d/%d IPv6 addresses",
			subnet.Name, ippool.Name, len(v4), wantV4, len(v6), wantV6)
	}

	ips := append(v4, v6...)
	return ips, nil
}

func trimIPs(ips []string, count int) []string {
	if len(ips) > count {
		return ips[:count]
	}
	return ips
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
