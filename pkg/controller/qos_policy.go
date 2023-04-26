package controller

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (c *Controller) enqueueAddQoSPolicy(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add qos policy %s", key)
	c.addQoSPolicyQueue.Add(key)
}

func compareQoSPolicyBandwithLimitRules(old, new kubeovnv1.QoSPolicyBandwidthLimitRules) bool {
	if len(old) != len(new) {
		return false
	}

	sort.Slice(new, func(i, j int) bool {
		return new[i].Name < new[j].Name
	})
	return reflect.DeepEqual(old, new)
}

func (c *Controller) enqueueUpdateQoSPolicy(old, new interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}
	oldQos := old.(*kubeovnv1.QoSPolicy)
	newQos := new.(*kubeovnv1.QoSPolicy)
	if !newQos.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("enqueue update to clean qos %s", key)
		c.updateQoSPolicyQueue.Add(key)
		return
	}
	if oldQos.Status.Shared != newQos.Spec.Shared ||
		oldQos.Status.BindingType != newQos.Spec.BindingType ||
		!compareQoSPolicyBandwithLimitRules(oldQos.Status.BandwidthLimitRules,
			newQos.Spec.BandwidthLimitRules) {
		klog.V(3).Infof("enqueue update qos %s", key)
		c.updateQoSPolicyQueue.Add(key)
		return
	}
}

func (c *Controller) enqueueDelQoSPolicy(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.delQoSPolicyQueue.Add(key)
}

func (c *Controller) runAddQoSPolicyWorker() {
	for c.processNextAddQoSPolicyWorkItem() {
	}
}

func (c *Controller) runUpdateQoSPolicyWorker() {
	for c.processNextUpdateQoSPolicyWorkItem() {
	}
}

func (c *Controller) runDelQoSPolicyWorker() {
	for c.processNextDeleteQoSPolicyWorkItem() {
	}
}

func (c *Controller) processNextAddQoSPolicyWorkItem() bool {
	obj, shutdown := c.addQoSPolicyQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addQoSPolicyQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addQoSPolicyQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddQoSPolicy(key); err != nil {
			c.addQoSPolicyQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addQoSPolicyQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateQoSPolicyWorkItem() bool {
	obj, shutdown := c.updateQoSPolicyQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateQoSPolicyQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateQoSPolicyQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateQoSPolicy(key); err != nil {
			c.updateQoSPolicyQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateQoSPolicyQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteQoSPolicyWorkItem() bool {
	obj, shutdown := c.delQoSPolicyQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.delQoSPolicyQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.delQoSPolicyQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected qos in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDelQoSPolicy(key); err != nil {
			c.delQoSPolicyQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.delQoSPolicyQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleAddQoSPolicy(key string) error {

	c.vpcNatGwKeyMutex.Lock(key)
	defer c.vpcNatGwKeyMutex.Unlock(key)

	cachedQoS, err := c.qosPoliciesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	sortedNewRules := cachedQoS.Spec.BandwidthLimitRules
	sort.Slice(sortedNewRules, func(i, j int) bool {
		return sortedNewRules[i].Name < sortedNewRules[j].Name
	})

	if reflect.DeepEqual(cachedQoS.Status.BandwidthLimitRules,
		sortedNewRules) &&
		cachedQoS.Status.Shared == cachedQoS.Spec.Shared &&
		cachedQoS.Status.BindingType == cachedQoS.Spec.BindingType {
		// already ok
		return nil
	}
	klog.V(3).Infof("handle add qos %s", key)

	if err := c.validateQosPolicy(cachedQoS); err != nil {
		klog.Errorf("failed to validate qos %s, %v", key, err)
		return err
	}

	if err = c.patchQoSStatus(key, cachedQoS.Spec.Shared, cachedQoS.Spec.BindingType, sortedNewRules); err != nil {
		klog.Errorf("failed to patch status for qos %s, %v", key, err)
		return err
	}

	return nil
}

func (c *Controller) patchQoSStatus(
	key string, shared bool, qosType kubeovnv1.QoSPolicyBindingType, bandwithRules kubeovnv1.QoSPolicyBandwidthLimitRules) error {
	oriQoS, err := c.qosPoliciesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	qos := oriQoS.DeepCopy()
	qos.Status.Shared = shared
	qos.Status.BindingType = qosType
	qos.Status.BandwidthLimitRules = bandwithRules
	bytes, err := qos.Status.Bytes()
	if err != nil {
		return err
	}
	if _, err = c.config.KubeOvnClient.KubeovnV1().QoSPolicies().Patch(context.Background(), qos.Name,
		types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to patch qos %s, %v", qos.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelQoSPoliciesFinalizer(key string) error {
	cachedQoSPolicies, err := c.qosPoliciesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if len(cachedQoSPolicies.Finalizers) == 0 {
		return nil
	}
	newQoSPolicies := cachedQoSPolicies.DeepCopy()
	controllerutil.RemoveFinalizer(newQoSPolicies, util.ControllerName)
	patch, err := util.GenerateMergePatchPayload(cachedQoSPolicies, newQoSPolicies)
	if err != nil {
		klog.Errorf("failed to generate patch payload for qos '%s', %v", cachedQoSPolicies.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().QoSPolicies().Patch(context.Background(), cachedQoSPolicies.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from qos '%s', %v", cachedQoSPolicies.Name, err)
		return err
	}
	return nil
}

func diffQoSPolicyBandwithLimitRules(oldList, newList kubeovnv1.QoSPolicyBandwidthLimitRules) (added, deleted, updated kubeovnv1.QoSPolicyBandwidthLimitRules) {
	added = kubeovnv1.QoSPolicyBandwidthLimitRules{}
	deleted = kubeovnv1.QoSPolicyBandwidthLimitRules{}
	updated = kubeovnv1.QoSPolicyBandwidthLimitRules{}

	// Create a map of old rules indexed by name for efficient lookup
	oldMap := make(map[string]*kubeovnv1.QoSPolicyBandwidthLimitRule)
	for _, s := range oldList {
		oldMap[s.Name] = s
	}

	// Loop through new rules and compare with old rules
	for _, s := range newList {
		if old, ok := oldMap[s.Name]; !ok {
			// add the rule
			added = append(added, s)
		} else if !reflect.DeepEqual(old, s) {
			// updated the rule
			updated = append(updated, s)
		}
		// keep the rule not changed
		delete(oldMap, s.Name)
	}

	// Remaining rules in oldMap are deleted
	for _, s := range oldMap {
		deleted = append(deleted, s)
	}

	return added, deleted, updated
}

func (c *Controller) reconcileEIPBandtithLimitRules(
	eip *kubeovnv1.IptablesEIP,
	added kubeovnv1.QoSPolicyBandwidthLimitRules,
	deleted kubeovnv1.QoSPolicyBandwidthLimitRules,
	updated kubeovnv1.QoSPolicyBandwidthLimitRules) error {
	var err error
	// in this case, we must delete rules first, then add or update rules
	if len(deleted) > 0 {
		if err = c.delEIPBandtithLimitRules(eip, eip.Status.IP, deleted); err != nil {
			klog.Errorf("failed to delete eip %s bandwidth limit rules, %v", eip.Name, err)
			return err
		}
	}
	if len(added) > 0 {
		if err = c.addOrUpdateEIPBandtithLimitRules(eip, eip.Status.IP, added); err != nil {
			klog.Errorf("failed to add eip %s bandwidth limit rules, %v", eip.Name, err)
			return err
		}
	}
	if len(updated) > 0 {
		if err = c.addOrUpdateEIPBandtithLimitRules(eip, eip.Status.IP, updated); err != nil {
			klog.Errorf("failed to update eip %s bandwidth limit rules, %v", eip.Name, err)
			return err
		}
	}

	return nil
}

func (c *Controller) validateQosPolicy(qosPolicy *kubeovnv1.QoSPolicy) error {
	return nil
}

func (c *Controller) handleUpdateQoSPolicy(key string) error {
	c.vpcNatGwKeyMutex.Lock(key)
	defer c.vpcNatGwKeyMutex.Unlock(key)

	cachedQos, err := c.qosPoliciesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	// should delete
	if !cachedQos.DeletionTimestamp.IsZero() {
		eips, err := c.iptablesEipsLister.List(
			labels.SelectorFromSet(labels.Set{util.QoSLabel: key}))
		if err != nil {
			klog.Errorf("failed to get eip list, %v", err)
			return err
		}
		if len(eips) != 0 {
			err = fmt.Errorf("qos policy %s is being used", key)
			klog.Error(err)
			return err
		}
		if err = c.handleDelQoSPoliciesFinalizer(key); err != nil {
			klog.Errorf("failed to handle del finalizer for qos %s, %v", key, err)
			return err
		}
		return nil
	}
	if err = c.handleAddQoSPolicyFinalizer(key); err != nil {
		klog.Errorf("failed to handle add finalizer for qos, %v", err)
		return err
	}

	if cachedQos.Status.Shared != cachedQos.Spec.Shared ||
		cachedQos.Status.BindingType != cachedQos.Spec.BindingType {
		err := fmt.Errorf("not support qos %s change shared", key)
		klog.Error(err)
		return err
	}

	if err := c.validateQosPolicy(cachedQos); err != nil {
		klog.Errorf("failed to validate qos %s, %v", key, err)
		return err
	}

	added, deleted, updated := diffQoSPolicyBandwithLimitRules(cachedQos.Status.BandwidthLimitRules, cachedQos.Spec.BandwidthLimitRules)
	bandwithRulesChanged := len(added) > 0 || len(deleted) > 0 || len(updated) > 0

	if bandwithRulesChanged {
		klog.V(3).Infof(
			"bandwidth limit rules is changed for qos %s, added: %s, deleted: %s, updated: %s",
			key, added.Strings(), deleted.Strings(), updated.Strings())
		if cachedQos.Status.Shared {
			err := fmt.Errorf("not support shared qos %s change rule ", key)
			klog.Error(err)
			return err
		} else {
			if cachedQos.Status.BindingType == kubeovnv1.QoSBindingTypeEIP {
				// filter to eip
				eips, err := c.iptablesEipsLister.List(
					labels.SelectorFromSet(labels.Set{util.QoSLabel: key}))
				if err != nil {
					klog.Errorf("failed to get eip list, %v", err)
					return err
				}
				if len(eips) == 0 {
					// not thing to do
				} else if len(eips) == 1 {
					eip := eips[0]
					if err = c.reconcileEIPBandtithLimitRules(eip, added, deleted, updated); err != nil {
						klog.Errorf("failed to reconcile eip %s bandwidth limit rules, %v", eip.Name, err)
						return err
					}
				} else {
					err := fmt.Errorf("not support qos %s change rule, related eip more than one", key)
					klog.Error(err)
					return err
				}
			}

			sortedNewRules := cachedQos.Spec.BandwidthLimitRules
			sort.Slice(sortedNewRules, func(i, j int) bool {
				return sortedNewRules[i].Name < sortedNewRules[j].Name
			})

			// .Status.Shared and .Status.BindingType are not supported to change
			if err = c.patchQoSStatus(key, cachedQos.Status.Shared, cachedQos.Status.BindingType, sortedNewRules); err != nil {
				klog.Errorf("failed to patch status for qos %s, %v", key, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) handleDelQoSPolicy(key string) error {
	klog.V(3).Infof("deleted qos policy %s", key)
	return nil
}

func (c *Controller) handleAddQoSPolicyFinalizer(key string) error {
	cachedQoSPolicy, err := c.qosPoliciesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if cachedQoSPolicy.DeletionTimestamp.IsZero() {
		if util.ContainsString(cachedQoSPolicy.Finalizers, util.ControllerName) {
			return nil
		}
	}
	newQoSPolicy := cachedQoSPolicy.DeepCopy()
	controllerutil.AddFinalizer(newQoSPolicy, util.ControllerName)
	patch, err := util.GenerateMergePatchPayload(cachedQoSPolicy, newQoSPolicy)
	if err != nil {
		klog.Errorf("failed to generate patch payload for qos '%s', %v", cachedQoSPolicy.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().QoSPolicies().Patch(context.Background(), cachedQoSPolicy.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for qos '%s', %v", cachedQoSPolicy.Name, err)
		return err
	}
	return nil
}
