package controller

import (
	"context"
	"fmt"
	"strings"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

func (c *Controller) enqueueAddPodAnnotatedIptablesEip(obj interface{}) {

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
	eipName := PodNameToEipName(p.Name, p.Namespace)
	if v, ok := p.Annotations[util.EipNameAnnotation]; ok {
		v = strings.Trim(v, " ")
		if v != "" {
			eipName = v
		}
	}
	// delete eip if pod not alive
	if !isPodAlive(p) {
		isStateful, statefulSetName := isStatefulSetPod(p)
		isVmPod, vmName := isVmPod(p)
		if isStateful || (isVmPod && c.config.EnableKeepVmIP) {
			if isStateful && isStatefulSetDeleted(c.config.KubeClient, p, statefulSetName) {
				klog.V(3).Infof("enqueue delete pod annotated iptables eip %s", eipName)
				c.delPodAnnotatedIptablesEipQueue.Add(obj)
				return
			}
			if isVmPod && c.isVmPodToDel(p, vmName) {
				klog.V(3).Infof("enqueue delete pod annotated iptables eip %s", eipName)
				c.delPodAnnotatedIptablesEipQueue.Add(obj)
				return
			}
		} else {
			klog.V(3).Infof("enqueue delete pod annotated iptables eip %s", eipName)
			c.delPodAnnotatedIptablesEipQueue.Add(obj)
			return
		}
		return
	}
	klog.V(3).Infof("enqueue add pod annotated iptables eip %s", eipName)
	c.addPodAnnotatedIptablesEipQueue.Add(key)
}

func (c *Controller) enqueueUpdatePodAnnotatedIptablesEip(oldObj, newObj interface{}) {
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
		// pod need eip after add fip annotation
		klog.V(3).Infof("enqueue add annotated iptables eip for pod %s/%s", newPod.Namespace, newPod.Name)
		c.addPodAnnotatedIptablesEipQueue.Add(key)
		return
	}
	if oldPod.Annotations[util.FipEnableAnnotation] == "true" && newPod.Annotations[util.FipEnableAnnotation] != "true" {
		// pod not need eip after remove fip annotation
		klog.V(3).Infof("enqueue delete annotated iptables eip for pod %s/%s", newPod.Namespace, newPod.Name)
		c.delPodAnnotatedIptablesEipQueue.Add(newObj)
		return
	}
	if newPod.DeletionTimestamp != nil && len(newPod.Finalizers) == 0 {
		// avoid delete eip twice
		return
	}
	isStateful, _ := isStatefulSetPod(newPod)
	isVmPod, vmName := isVmPod(newPod)
	if newPod.DeletionTimestamp != nil && isStateful {
		c.delPodAnnotatedIptablesEipQueue.Add(newObj)
		return
	}
	if !isPodAlive(newPod) && !isStateful && !isVmPod {
		c.delPodAnnotatedIptablesEipQueue.Add(newObj)
		return
	}
	if c.config.EnableKeepVmIP && isVmPod && c.isVmPodToDel(newPod, vmName) {
		c.delPodAnnotatedIptablesEipQueue.Add(newObj)
		return
	}
}

func (c *Controller) enqueueDeletePodAnnotatedIptablesEip(obj interface{}) {
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
		if isStatefulSetDeleted(c.config.KubeClient, p, statefulSetName) {
			c.delPodAnnotatedIptablesEipQueue.Add(obj)
			return
		}
		if delete, err := appendCheckPodToDel(c, p, statefulSetName, "StatefulSet"); delete && err == nil {
			c.delPodAnnotatedIptablesEipQueue.Add(obj)
			return
		}
	} else if c.config.EnableKeepVmIP && isVmPod {
		if c.isVmPodToDel(p, vmName) {
			c.delPodAnnotatedIptablesEipQueue.Add(obj)
			return
		}
		if delete, err := appendCheckPodToDel(c, p, vmName, util.VmInstance); delete && err == nil {
			c.delPodAnnotatedIptablesEipQueue.Add(obj)
			return
		}
	} else {
		c.delPodAnnotatedIptablesEipQueue.Add(obj)
		return
	}
}

func (c *Controller) runAddPodAnnotatedIptablesEipWorker() {
	for c.processNextAddPodAnnotatedIptablesEipWorkItem() {
	}
}

func (c *Controller) runDelPodAnnotatedIptablesEipWorker() {
	for c.processNextDeletePodAnnotatedIptablesEipWorkItem() {
	}
}

func (c *Controller) processNextAddPodAnnotatedIptablesEipWorkItem() bool {
	obj, shutdown := c.addPodAnnotatedIptablesEipQueue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.addPodAnnotatedIptablesEipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addPodAnnotatedIptablesEipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddPodAnnotatedIptablesEip(key); err != nil {
			c.addPodAnnotatedIptablesEipQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addPodAnnotatedIptablesEipQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeletePodAnnotatedIptablesEipWorkItem() bool {
	obj, shutdown := c.delPodAnnotatedIptablesEipQueue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.delPodAnnotatedIptablesEipQueue.Done(obj)
		var pod *v1.Pod
		var ok bool
		if pod, ok = obj.(*v1.Pod); !ok {
			c.delPodAnnotatedIptablesEipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected pod in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDeletePodAnnotatedIptablesEip(pod); err != nil {
			c.delPodAnnotatedIptablesEipQueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing '%s': %s, requeuing", pod.Name, err.Error())
		}
		c.delPodAnnotatedIptablesEipQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleAddPodAnnotatedIptablesEip(key string) error {
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
	if cachedPod.Annotations[util.EipAnnotation] != "" {
		// eip aleady ok
		return nil
	}
	eipName := PodNameToEipName(cachedPod.Name, cachedPod.Namespace)
	if v, ok := cachedPod.Annotations[util.EipNameAnnotation]; ok {
		v = strings.Trim(v, " ")
		if v != "" {
			eipName = v
		}
	}
	newPod := cachedPod.DeepCopy()
	if newPod.Annotations[util.AllocatedAnnotation] != "true" {
		err = fmt.Errorf("pod network not allocated, failed to create iptables eip %s", eipName)
		return err
	}
	if _, err = c.iptablesEipsLister.Get(eipName); err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
		klog.V(3).Infof("handle add pod annotated iptables eip %s", eipName)
		subnet := newPod.Annotations[util.LogicalSwitchAnnotation]
		router := newPod.Annotations[util.LogicalRouterAnnotation]
		natGw, err := c.getNatGw(router, subnet)
		if err != nil {
			klog.Errorf("failed to get vpc nat gw eip: %v", eipName, err)
			return err
		}
		if err := c.createOrUpdateCrdEip(eipName, "", "", "", "", natGw); err != nil {
			klog.Errorf("failed to create eip %s: %v", eipName, err)
			return err
		}
	}
	// check eip if ready and update pod eip annotation
	var eip *kubeovnv1.IptablesEIP
	if eip, err = c.iptablesEipsLister.Get(eipName); err != nil {
		return err
	}
	// update pod eip annotation
	if eip.Status.IP != "" {
		newPod.Annotations[util.EipAnnotation] = eip.Status.IP
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
	} else {
		return fmt.Errorf("eip %s not ready", eipName)
	}
	return nil
}

func (c *Controller) handleDeletePodAnnotatedIptablesEip(pod *v1.Pod) error {
	var err error
	var keepEipCR bool
	klog.V(3).Infof("handle delete annotated iptables eip for pod %s/%s", pod.Namespace, pod.Name)

	// keep statefulset eip cr
	if isStateful, statefulSetName := isStatefulSetPod(pod); isStateful {
		if !isStatefulSetDeleted(c.config.KubeClient, pod, statefulSetName) {
			keepEipCR = true
		}
	}
	eipName := PodNameToEipName(pod.Name, pod.Namespace)
	if v, ok := pod.Annotations[util.EipNameAnnotation]; ok {
		// keep named eip cr
		v = strings.Trim(v, " ")
		if v != "" {
			keepEipCR = true
			eipName = v
		}
	}
	if keepEipCR {
		return nil
	}
	klog.V(3).Infof("delete pod annotated iptables eip cr %s", eipName)
	if err = c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Delete(context.Background(), eipName, metav1.DeleteOptions{}); err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to delete iptables eip %s: %v", eipName, err)
			return err
		}
	}
	return nil
}

func PodNameToEipName(name, namespace string) string {
	return fmt.Sprintf("%s.%s", name, namespace)
}

func isStatefulSetDeleted(c kubernetes.Interface, pod *v1.Pod, statefulSetName string) bool {
	// return true if statefulset is deleted or in deleting
	ss, err := c.AppsV1().StatefulSets(pod.Namespace).Get(context.Background(), statefulSetName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// statefulset is deleted
			return true
		} else {
			klog.Errorf("failed to get statefulset %v", err)
			return false
		}
	}
	// statefulset is deleting
	if ss.DeletionTimestamp != nil {
		return true
	}
	return false
}
