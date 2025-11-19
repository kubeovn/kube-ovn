package controller

import (
	"context"
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
	key := cache.MetaObjectToName(obj.(*kubeovnv1.IPPool)).String()
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
		c.deleteIPPoolQueue.Add(newIPPool.DeepCopy())
		return
	}
	if !slices.Equal(oldIPPool.Spec.Namespaces, newIPPool.Spec.Namespaces) ||
		!slices.Equal(oldIPPool.Spec.IPs, newIPPool.Spec.IPs) ||
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
		return err
	}
	klog.Infof("handle add/update ippool %s", cachedIPPool.Name)

	ippool := cachedIPPool.DeepCopy()
	if err = c.handleAddIPPoolFinalizer(ippool); err != nil {
		klog.Errorf("failed to add finalizer for ippool %s: %v", ippool.Name, err)
		return err
	}
	if !ippool.DeletionTimestamp.IsZero() {
		klog.Infof("ippool %s is being deleted, skip add/update handling", ippool.Name)
		return nil
	}
	ippool.Status.EnsureStandardConditions()
	if err = c.reconcileIPPoolAddressSet(ippool); err != nil {
		klog.Errorf("failed to reconcile address set for ippool %s: %v", ippool.Name, err)
		if patchErr := c.patchIPPoolStatusCondition(ippool, "ReconcileAddressSetFailed", err.Error()); patchErr != nil {
			klog.Error(patchErr)
		}
		return err
	}
	if err = c.ipam.AddOrUpdateIPPool(ippool.Spec.Subnet, ippool.Name, ippool.Spec.IPs); err != nil {
		klog.Errorf("failed to add/update ippool %s with IPs %v in subnet %s: %v", ippool.Name, ippool.Spec.IPs, ippool.Spec.Subnet, err)
		if patchErr := c.patchIPPoolStatusCondition(ippool, "UpdateIPAMFailed", err.Error()); patchErr != nil {
			klog.Error(patchErr)
		}
		return err
	}

	c.updateIPPoolStatistics(ippool)

	if err = c.patchIPPoolStatusCondition(ippool, "UpdateIPAMSucceeded", ""); err != nil {
		klog.Error(err)
		return err
	}

	for _, ns := range ippool.Spec.Namespaces {
		c.addNamespaceQueue.Add(ns)
	}

	return nil
}

func (c *Controller) handleDeleteIPPool(ippool *kubeovnv1.IPPool) error {
	c.ippoolKeyMutex.LockKey(ippool.Name)
	defer func() { _ = c.ippoolKeyMutex.UnlockKey(ippool.Name) }()

	klog.Infof("handle delete ippool %s", ippool.Name)
	c.ipam.RemoveIPPool(ippool.Spec.Subnet, ippool.Name)
	if err := c.OVNNbClient.DeleteAddressSet(util.IPPoolAddressSetName(ippool.Name)); err != nil {
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

	if err := c.handleDelIPPoolFinalizer(ippool); err != nil {
		klog.Errorf("failed to remove finalizer for ippool %s: %v", ippool.Name, err)
		return err
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
		return err
	}

	ippool := cachedIPPool.DeepCopy()
	c.updateIPPoolStatistics(ippool)
	if reflect.DeepEqual(ippool.Status, cachedIPPool.Status) {
		return nil
	}

	return c.patchIPPoolStatus(ippool)
}

func (c Controller) patchIPPoolStatusCondition(ippool *kubeovnv1.IPPool, reason, errMsg string) error {
	if errMsg != "" {
		ippool.Status.SetError(reason, errMsg)
		ippool.Status.NotReady(reason, errMsg)
		c.recorder.Eventf(ippool, corev1.EventTypeWarning, reason, errMsg)
	} else {
		ippool.Status.Ready(reason, "")
	}

	return c.patchIPPoolStatus(ippool)
}

func (c Controller) patchIPPoolStatus(ippool *kubeovnv1.IPPool) error {
	bytes, err := ippool.Status.Bytes()
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
	if ippool == nil || !ippool.DeletionTimestamp.IsZero() {
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
	controllerutil.RemoveFinalizer(newIPPool, util.DepreciatedFinalizerName)
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

func (c *Controller) reconcileIPPoolAddressSet(ippool *kubeovnv1.IPPool) error {
	asName := util.IPPoolAddressSetName(ippool.Name)

	if !ippool.Spec.EnableAddressSet {
		if err := c.OVNNbClient.DeleteAddressSet(asName); err != nil {
			err = fmt.Errorf("failed to delete address set %s: %w", asName, err)
			klog.Error(err)
			return err
		}
		return nil
	}

	addresses, err := util.ExpandIPPoolAddressesForOVN(ippool.Spec.IPs)
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
