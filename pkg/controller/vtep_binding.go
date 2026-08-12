package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const vtepBindingChassisRetryDelay = 5 * time.Second

var errVtepBindingPending = errors.New("vtep binding waiting for SB chassis binding")

func (c *Controller) enqueueAddOrUpdateVtepBinding(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.VtepBinding)).String()
	klog.V(3).Infof("enqueue add/update vtep binding %s", key)
	c.addOrUpdateVtepBindingQueue.Add(key)
}

func (c *Controller) enqueueUpdateVtepBinding(oldObj, newObj any) {
	oldBinding := oldObj.(*kubeovnv1.VtepBinding)
	newBinding := newObj.(*kubeovnv1.VtepBinding)
	if !newBinding.DeletionTimestamp.IsZero() {
		key := cache.MetaObjectToName(newBinding).String()
		klog.V(3).Infof("enqueue delete vtep binding %s due to deletion timestamp", key)
		c.addOrUpdateVtepBindingQueue.Add(key)
		return
	}
	if !reflect.DeepEqual(oldBinding.Spec, newBinding.Spec) {
		key := cache.MetaObjectToName(newBinding).String()
		klog.V(3).Infof("enqueue update vtep binding %s", key)
		c.addOrUpdateVtepBindingQueue.Add(key)
	}
}

func (c *Controller) enqueueDeleteVtepBinding(obj any) {
	var binding *kubeovnv1.VtepBinding
	switch t := obj.(type) {
	case *kubeovnv1.VtepBinding:
		binding = t
	case cache.DeletedFinalStateUnknown:
		b, ok := t.Obj.(*kubeovnv1.VtepBinding)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		binding = b
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	key := cache.MetaObjectToName(binding).String()
	klog.V(3).Infof("enqueue delete vtep binding %s", key)
	c.deleteVtepBindingQueue.Add(binding)
}

func (c *Controller) handleAddOrUpdateVtepBinding(key string) error {
	c.vtepBindingKeyMutex.LockKey(key)
	defer func() { _ = c.vtepBindingKeyMutex.UnlockKey(key) }()

	cachedBinding, err := c.vtepBindingsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	binding := cachedBinding.DeepCopy()
	if err = c.handleAddVtepBindingFinalizer(binding); err != nil {
		klog.Errorf("failed to add finalizer for vtep binding %s: %v", binding.Name, err)
		return err
	}

	if !binding.DeletionTimestamp.IsZero() {
		return c.cleanupVtepBinding(binding)
	}

	klog.Infof("handle add/update vtep binding %s", binding.Name)
	binding.Status.EnsureStandardConditions()

	if err = c.reconcileVtepBinding(binding); err != nil {
		if errors.Is(err, errVtepBindingPending) {
			binding.Status.NotReady("WaitingForChassis", err.Error())
			if patchErr := c.patchVtepBindingStatus(binding); patchErr != nil {
				klog.Error(patchErr)
				return patchErr
			}
			c.addOrUpdateVtepBindingQueue.AddAfter(key, vtepBindingChassisRetryDelay)
			return nil
		}
		klog.Errorf("failed to reconcile vtep binding %s: %v", binding.Name, err)
		if patchErr := c.patchVtepBindingStatusCondition(binding, "ReconcileFailed", err.Error()); patchErr != nil {
			klog.Error(patchErr)
		}
		return err
	}

	if err = c.patchVtepBindingStatusCondition(binding, "VTEPAttachmentReady", ""); err != nil {
		klog.Error(err)
		return err
	}
	return nil
}

func (c *Controller) handleDeleteVtepBinding(binding *kubeovnv1.VtepBinding) error {
	c.vtepBindingKeyMutex.LockKey(binding.Name)
	defer func() { _ = c.vtepBindingKeyMutex.UnlockKey(binding.Name) }()

	klog.Infof("handle delete vtep binding %s", binding.Name)
	return c.cleanupVtepBinding(binding)
}

func (c *Controller) cleanupVtepBinding(binding *kubeovnv1.VtepBinding) error {
	vtepLogicalSwitch := binding.VtepLogicalSwitchName()
	if c.VtepClient != nil {
		if err := c.VtepClient.RemoveVtepBinding(
			binding.Spec.PhysicalSwitch,
			binding.Spec.PhysicalPort,
			vtepLogicalSwitch,
			binding.Name,
			binding.Spec.VlanID,
		); err != nil {
			klog.Errorf("failed to remove VTEP DB binding for %s: %v", binding.Name, err)
			return err
		}
	}

	lspName := ovs.GetVtepLogicalSwitchPortName(binding.Name)
	if err := c.OVNNbClient.DeleteLogicalSwitchPort(lspName); err != nil {
		klog.Errorf("failed to delete vtep logical switch port %s for binding %s: %v", lspName, binding.Name, err)
		return err
	}
	if err := c.handleDelVtepBindingFinalizer(binding); err != nil {
		klog.Errorf("failed to remove finalizer for vtep binding %s: %v", binding.Name, err)
		return err
	}
	return nil
}

func (c *Controller) reconcileVtepBinding(binding *kubeovnv1.VtepBinding) error {
	subnet, err := c.subnetsLister.Get(binding.Spec.Subnet)
	if err != nil {
		return fmt.Errorf("failed to get subnet %s: %w", binding.Spec.Subnet, err)
	}

	vtepLogicalSwitch := binding.VtepLogicalSwitchName()
	if err = c.validateVtepBindingConflict(binding, vtepLogicalSwitch); err != nil {
		return err
	}

	exist, err := c.OVNNbClient.LogicalSwitchExists(subnet.Name)
	if err != nil {
		return fmt.Errorf("failed to check logical switch %s: %w", subnet.Name, err)
	}
	if !exist {
		return fmt.Errorf("logical switch %s for subnet %s does not exist", subnet.Name, subnet.Name)
	}

	lspName := ovs.GetVtepLogicalSwitchPortName(binding.Name)
	externalIDs := map[string]string{
		ovs.VtepBindingKey:     binding.Name,
		"vtep-physical-switch": binding.Spec.PhysicalSwitch,
		"vtep-logical-switch":  vtepLogicalSwitch,
		"physical-port":        binding.Spec.PhysicalPort,
		"vlan-id":              strconv.Itoa(binding.Spec.VlanID),
	}

	if err = c.OVNNbClient.CreateVtepLogicalSwitchPort(
		subnet.Name,
		lspName,
		binding.Spec.PhysicalSwitch,
		vtepLogicalSwitch,
		externalIDs,
	); err != nil {
		return fmt.Errorf("failed to create vtep logical switch port %s: %w", lspName, err)
	}

	if c.VtepClient != nil {
		if err = c.VtepClient.EnsureVtepBinding(
			binding.Spec.PhysicalSwitch,
			binding.Spec.PhysicalPort,
			vtepLogicalSwitch,
			binding.Name,
			binding.Spec.VlanID,
		); err != nil {
			return fmt.Errorf("failed to ensure VTEP DB binding: %w", err)
		}
	}

	binding.Status.LogicalSwitch = subnet.Name
	binding.Status.LogicalSwitchPort = lspName
	binding.Status.VtepLogicalSwitch = vtepLogicalSwitch

	pb, err := c.OVNSbClient.GetPortBindingByLogicalPort(lspName, true)
	if err != nil {
		return fmt.Errorf("failed to get SB port binding for %s: %w", lspName, err)
	}
	if !ovs.IsPortBindingChassisBound(pb) {
		binding.Status.Chassis = ""
		return fmt.Errorf("%w: logical port %s has no chassis", errVtepBindingPending, lspName)
	}

	chassisName, err := c.OVNSbClient.GetChassisNameByUUID(*pb.Chassis)
	if err != nil {
		return err
	}
	binding.Status.Chassis = chassisName
	return nil
}

func (c *Controller) validateVtepBindingConflict(binding *kubeovnv1.VtepBinding, vtepLogicalSwitch string) error {
	bindings, err := c.vtepBindingsLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list vtep bindings: %w", err)
	}
	for _, other := range bindings {
		if other.Name == binding.Name || !other.DeletionTimestamp.IsZero() {
			continue
		}
		if other.Spec.PhysicalSwitch == binding.Spec.PhysicalSwitch &&
			other.VtepLogicalSwitchName() == vtepLogicalSwitch {
			return fmt.Errorf("vtep binding %s conflicts with %s: physicalSwitch %q and vtepLogicalSwitch %q already in use",
				binding.Name, other.Name, binding.Spec.PhysicalSwitch, vtepLogicalSwitch)
		}
	}
	return nil
}

func (c *Controller) patchVtepBindingStatusCondition(binding *kubeovnv1.VtepBinding, reason, errMsg string) error {
	if errMsg != "" {
		binding.Status.SetError(reason, errMsg)
		binding.Status.NotReady(reason, errMsg)
		c.recorder.Eventf(binding, corev1.EventTypeWarning, reason, "%s", errMsg)
	} else {
		binding.Status.ReadyCondition(reason, "")
		c.recorder.Eventf(binding, corev1.EventTypeNormal, reason, "")
	}
	return c.patchVtepBindingStatus(binding)
}

func (c *Controller) patchVtepBindingStatus(binding *kubeovnv1.VtepBinding) error {
	bytes, err := binding.Status.Bytes()
	if err != nil {
		klog.Errorf("failed to generate json representation for status of vtep binding %s: %v", binding.Name, err)
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().VtepBindings().Patch(context.Background(), binding.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
		klog.Errorf("failed to patch status of vtep binding %s: %v", binding.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleAddVtepBindingFinalizer(binding *kubeovnv1.VtepBinding) error {
	if !binding.DeletionTimestamp.IsZero() {
		return nil
	}
	if controllerutil.ContainsFinalizer(binding, util.KubeOVNControllerFinalizer) {
		return nil
	}

	newBinding := binding.DeepCopy()
	controllerutil.AddFinalizer(newBinding, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(binding, newBinding)
	if err != nil {
		klog.Errorf("failed to generate patch payload for vtep binding %s: %v", binding.Name, err)
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().VtepBindings().Patch(context.Background(), binding.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for vtep binding %s: %v", binding.Name, err)
		return err
	}
	// refresh local copy so subsequent status patches see the finalizer
	binding.Finalizers = newBinding.Finalizers
	return nil
}

func (c *Controller) handleDelVtepBindingFinalizer(binding *kubeovnv1.VtepBinding) error {
	if binding == nil || len(binding.GetFinalizers()) == 0 {
		return nil
	}

	newBinding := binding.DeepCopy()
	controllerutil.RemoveFinalizer(newBinding, util.DeprecatedFinalizerName)
	controllerutil.RemoveFinalizer(newBinding, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(binding, newBinding)
	if err != nil {
		klog.Errorf("failed to generate patch payload for vtep binding %s: %v", binding.Name, err)
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().VtepBindings().Patch(context.Background(), binding.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from vtep binding %s: %v", binding.Name, err)
		return err
	}
	return nil
}
