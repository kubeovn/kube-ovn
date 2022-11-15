package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kubeovn/kube-ovn/pkg/util"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (c *Controller) enqueueAddPodAnnotatedIptablesFip(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	p := obj.(*v1.Pod)
	if p.Spec.HostNetwork {
		return
	}
	if p.Annotations[util.FipEnableAnnotation] != "true" {
		return
	}
	if !isPodAlive(p) {
		isStateful, statefulSetName := isStatefulSetPod(p)
		isVmPod, vmName := isVmPod(p)
		if isStateful || (isVmPod && c.config.EnableKeepVmIP) {
			if isStateful && isStatefulSetPodToDel(c.config.KubeClient, p, statefulSetName) {
				c.delPodAnnotatedIptablesFipQueue.Add(obj)
				return
			}
			if isVmPod && c.isVmPodToDel(p, vmName) {
				c.delPodAnnotatedIptablesFipQueue.Add(obj)
				return
			}
		} else {
			c.delPodAnnotatedIptablesFipQueue.Add(obj)
			return
		}
		return
	}
	c.addPodAnnotatedIptablesFipQueue.Add(key)
}

func (c *Controller) enqueueUpdatePodAnnotatedIptablesFip(oldObj, newObj interface{}) {
	if !c.isLeader() {
		return
	}
	oldPod := oldObj.(*v1.Pod)
	newPod := newObj.(*v1.Pod)
	if oldPod.ResourceVersion == newPod.ResourceVersion {
		return
	}
	if oldPod.Spec.HostNetwork {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	if oldPod.Annotations[util.FipEnableAnnotation] != "true" && newPod.Annotations[util.FipEnableAnnotation] == "true" {
		// pod need fip after add fip annotation
		klog.V(3).Infof("enqueue add annotated iptables eip for pod %s/%s", newPod.Namespace, newPod.Name)
		c.addPodAnnotatedIptablesFipQueue.Add(key)
		return
	}
	if oldPod.Annotations[util.FipEnableAnnotation] == "true" && newPod.Annotations[util.FipEnableAnnotation] != "true" {
		klog.V(3).Infof("enqueue delete annotated iptables fip for pod %s/%s", newPod.Namespace, newPod.Name)
		c.delPodAnnotatedIptablesFipQueue.Add(newObj)
		return
	}
	if newPod.DeletionTimestamp != nil && len(newPod.Finalizers) == 0 {
		// avoid delete fip twice
		return
	}

	isStateful, _ := isStatefulSetPod(newPod)
	isVmPod, vmName := isVmPod(newPod)
	if !isPodAlive(newPod) && !isStateful && !isVmPod {
		c.delPodAnnotatedIptablesFipQueue.Add(newObj)
		return
	}
	if newPod.DeletionTimestamp != nil && isStateful {
		c.delPodAnnotatedIptablesFipQueue.Add(newObj)
		return
	}
	if c.config.EnableKeepVmIP && isVmPod && c.isVmPodToDel(newPod, vmName) {
		c.delPodAnnotatedIptablesFipQueue.Add(newObj)
		return
	}
}

func (c *Controller) enqueueDeletePodAnnotatedIptablesFip(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var err error
	if _, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	p := obj.(*v1.Pod)
	if p.Spec.HostNetwork {
		return
	}
	if p.Annotations[util.FipEnableAnnotation] != "true" {
		return
	}
	isStateful, statefulSetName := isStatefulSetPod(p)
	isVmPod, vmName := isVmPod(p)
	if isStateful {
		if isStatefulSetPodToDel(c.config.KubeClient, p, statefulSetName) {
			c.delPodAnnotatedIptablesFipQueue.Add(obj)
			return
		}
		if delete, err := appendCheckPodToDel(c, p, statefulSetName, "StatefulSet"); delete && err == nil {
			c.delPodAnnotatedIptablesFipQueue.Add(obj)
			return
		}
	} else if c.config.EnableKeepVmIP && isVmPod {
		if c.isVmPodToDel(p, vmName) {
			c.delPodAnnotatedIptablesFipQueue.Add(obj)
			return
		}
		if delete, err := appendCheckPodToDel(c, p, vmName, util.VmInstance); delete && err == nil {
			c.delPodAnnotatedIptablesFipQueue.Add(obj)
			return
		}
	} else {
		c.delPodAnnotatedIptablesFipQueue.Add(obj)
		return
	}
}

func (c *Controller) runAddPodAnnotatedIptablesFipWorker() {
	for c.processNextAddPodAnnotatedIptablesFipWorkItem() {
	}
}

func (c *Controller) runDelPodAnnotatedIptablesFipWorker() {
	for c.processNextDeletePodAnnotatedIptablesFipWorkItem() {
	}
}

func (c *Controller) processNextAddPodAnnotatedIptablesFipWorkItem() bool {
	obj, shutdown := c.addPodAnnotatedIptablesFipQueue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.addPodAnnotatedIptablesFipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addPodAnnotatedIptablesFipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddPodAnnotatedIptablesFip(key); err != nil {
			c.addPodAnnotatedIptablesFipQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addPodAnnotatedIptablesFipQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeletePodAnnotatedIptablesFipWorkItem() bool {
	obj, shutdown := c.delPodAnnotatedIptablesFipQueue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.delPodAnnotatedIptablesFipQueue.Done(obj)
		var pod *v1.Pod
		var ok bool
		if pod, ok = obj.(*v1.Pod); !ok {
			c.delPodAnnotatedIptablesFipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected pod in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDeletePodAnnotatedIptablesFip(pod); err != nil {
			c.delPodAnnotatedIptablesFipQueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing '%s': %s, requeuing", pod.Name, err.Error())
		}
		c.delPodAnnotatedIptablesFipQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleAddPodAnnotatedIptablesFip(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	cachedPod, err := c.podsLister.Pods(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if cachedPod.Annotations[util.FipEnableAnnotation] != "true" {
		// not enable fip
		return nil
	}
	if cachedPod.Annotations[util.FipNameAnnotation] != "" {
		// fip aleady ok
		return nil
	}
	fipName := PodNameToEipName(cachedPod.Name, cachedPod.Namespace)
	if v, ok := cachedPod.Annotations[util.EipNameAnnotation]; ok {
		v = strings.Trim(v, " ")
		if v != "" {
			fipName = v
		}
	}
	if err = c.handleAddPodAnnotatedIptablesFipFinalizer(cachedPod); err != nil {
		return err
	}
	if cachedPod.Annotations[util.AllocatedAnnotation] != "true" {
		err = fmt.Errorf("pod network not allocated, failed to create iptables fip %s", fipName)
		return err
	}
	if _, err = c.iptablesFipsLister.Get(fipName); err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
		klog.V(3).Infof("handle add pod annotated iptables fip %s", fipName)
		if err := c.createOrUpdateCrdFip(fipName, fipName, cachedPod.Annotations[util.IpAddressAnnotation]); err != nil {
			klog.Errorf("failed to create fip %s: %v", fipName, err)
			return err
		}
	}
	newPod := cachedPod.DeepCopy()
	newPod.Annotations[util.FipNameAnnotation] = fipName
	patch, err := util.GenerateStrategicMergePatchPayload(cachedPod, newPod)
	if err != nil {
		return err
	}
	if _, err := c.config.KubeClient.CoreV1().Pods(namespace).Patch(context.Background(), name,
		types.StrategicMergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("patch pod %s/%s annotation eip failed: %v", name, namespace, err)
		return err
	}
	return nil
}

func (c *Controller) handleDeletePodAnnotatedIptablesFip(pod *v1.Pod) error {
	var err error
	var keepFipCR bool
	klog.V(3).Infof("handle delete annotated iptables fip for pod %s/%s", pod.Namespace, pod.Name)
	if ok, sts := isStatefulSetPod(pod); ok {
		delete, err := appendCheckPodToDel(c, pod, sts, "StatefulSet")
		keepFipCR = !isStatefulSetPodToDel(c.config.KubeClient, pod, sts) && !delete && err == nil
	}
	if !keepFipCR {
		fipName := PodNameToEipName(pod.Name, pod.Namespace)
		if v, ok := pod.Annotations[util.EipNameAnnotation]; ok {
			v = strings.Trim(v, " ")
			if v != "" {
				fipName = v
			}
		}
		klog.V(3).Infof("delete pod annotated iptables fip cr %s", fipName)
		if err = c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().Delete(context.Background(), fipName, metav1.DeleteOptions{}); err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to delete iptables fip %s: %v", fipName, err)
				return err
			}
		}
	}
	if err = c.handleDelPodAnnotatedIptablesFipFinalizer(pod); err != nil {
		return err
	}
	return nil
}

func (c *Controller) handleAddPodAnnotatedIptablesFipFinalizer(pod *v1.Pod) error {
	if pod.DeletionTimestamp.IsZero() {
		if util.ContainsString(pod.Finalizers, util.FipFinalizer) {
			return nil
		}
		newPod := pod.DeepCopy()
		controllerutil.AddFinalizer(newPod, util.ControllerName)
		patch, err := util.GenerateMergePatchPayload(pod, newPod)
		if err != nil {
			klog.Errorf("failed to generate patch payload for pod '%s', %v", pod.Name, err)
			return err
		}
		if _, err := c.config.KubeClient.CoreV1().Pods(pod.Namespace).Patch(context.Background(), pod.Name,
			types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			klog.Errorf("failed to add iptables eip finalizer for pod %s: %v", pod.Name, err)
			return err
		}
		// wait local cache ready
		time.Sleep(2 * time.Second)
	}
	return nil
}

func (c *Controller) handleDelPodAnnotatedIptablesFipFinalizer(pod *v1.Pod) error {
	if len(pod.Finalizers) == 0 {
		return nil
	}
	newPod := pod.DeepCopy()
	controllerutil.RemoveFinalizer(newPod, util.ControllerName)
	patch, err := util.GenerateMergePatchPayload(pod, newPod)
	if err != nil {
		klog.Errorf("failed to generate patch payload for pod '%s', %v", pod.Name, err)
		return err
	}
	if _, err := c.config.KubeClient.CoreV1().Pods(pod.Namespace).Patch(context.Background(), pod.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add iptables eip finalizer for pod %s: %v", pod.Name, err)
		return err
	}
	return nil
}
