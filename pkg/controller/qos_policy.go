package controller

import (
	"context"
	"fmt"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
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

	if oldQos.Status.BandwidthLimitRule != newQos.Spec.BandwidthLimitRule {
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

	if cachedQoS.Spec.BandwidthLimitRule == cachedQoS.Status.BandwidthLimitRule {
		// already ok
		return nil
	}
	klog.V(3).Infof("handle add qos %s", key)

	if err = c.patchQoSStatus(key, &cachedQoS.Spec.BandwidthLimitRule); err != nil {
		klog.Errorf("failed to patch status for qos %s, %v", key, err)
		return err
	}

	return nil
}

func (c *Controller) patchQoSStatus(key string, bandwithRule *kubeovnv1.QoSPolicyBandwidthLimitRule) error {
	oriQoS, err := c.qosPoliciesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	qos := oriQoS.DeepCopy()
	qos.Status.BandwidthLimitRule = *bandwithRule
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
	if cachedQos.Spec.BandwidthLimitRule != cachedQos.Status.BandwidthLimitRule {
		err := fmt.Errorf("not support qos %s change rule ", key)
		klog.Error(err)
		return err
	}
	// should delete
	if !cachedQos.DeletionTimestamp.IsZero() {

		eips, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().List(context.Background(),
			metav1.ListOptions{LabelSelector: fields.OneTermEqualSelector(util.QoSLabel, key).String()})
		if err != nil {
			klog.Errorf("failed to get eip list, %v", err)
			return err
		}
		if len(eips.Items) != 0 {
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
