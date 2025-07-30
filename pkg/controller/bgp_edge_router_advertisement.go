package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddBgpEdgeRouterAdvertisement(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.BgpEdgeRouterAdvertisement)).String()
	klog.V(3).Infof("enqueue add bgp-edge-router-advertisement %s", key)
	c.addBgpEdgeRouterAdvertisementQueue.Add(key)
}

func (c *Controller) enqueueUpdateBgpEdgeRouterAdvertisement(_, newObj any) {
	key := cache.MetaObjectToName(newObj.(*kubeovnv1.BgpEdgeRouterAdvertisement)).String()
	klog.V(3).Infof("enqueue update bgp-edge-router-advertisement %s", key)
	c.updateBgpEdgeRouterAdvertisementQueue.Add(key)
}

func (c *Controller) enqueueDeleteBgpEdgeRouterAdvertisement(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.BgpEdgeRouterAdvertisement)).String()
	klog.V(3).Infof("enqueue delete bgp-edge-router-advertisement %s", key)
	c.delBgpEdgeRouterAdvertisementQueue.Add(key)
}

func (c *Controller) handleAddBgpEdgeRouterAdvertisement(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.bgpEdgeRouterAdvertisementKeyMutex.LockKey(key)
	defer func() { _ = c.bgpEdgeRouterAdvertisementKeyMutex.UnlockKey(key) }()

	cachedAdvertisement, err := c.bgpEdgeRouterAdvertisementLister.BgpEdgeRouterAdvertisements(ns).Get(name)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Error(err)
			return err
		}
		return nil
	}

	if !cachedAdvertisement.DeletionTimestamp.IsZero() {
		c.delBgpEdgeRouterAdvertisementQueue.Add(key)
		return nil
	}

	if _, err := c.initBgpEdgeRouterAdvertisementStatus(cachedAdvertisement); err != nil {
		klog.Error(err)
		return err
	}

	klog.Infof("reconciling bgp-edge-router-advertisement %s", key)
	advertisement := cachedAdvertisement.DeepCopy()

	if controllerutil.AddFinalizer(advertisement, util.KubeOVNControllerFinalizer) {
		updatedAdvertisement, err := c.config.KubeOvnClient.KubeovnV1().BgpEdgeRouterAdvertisements(advertisement.Namespace).
			Update(context.Background(), advertisement, metav1.UpdateOptions{})
		if err != nil {
			err = fmt.Errorf("failed to add finalizer for bgp-edge-router %s/%s: %w", advertisement.Namespace, advertisement.Name, err)
			klog.Error(err)
			return err
		}
		advertisement = updatedAdvertisement
	}

	if pods, err := c.validateBgpEdgeRouterAdvertisement(advertisement); err != nil || pods == nil {
		klog.Error(err)
		return err
	} else {
		for _, pod := range pods {
			if len(pod.Status.PodIPs) == 0 {
				continue
			}
			klog.Infof("handle deleting bgp-edge-router-advertisement %s", key)
			if err = c.addBgpEdgeRouterAdvertisementRule(key, pod.Name, advertisement.Spec.Subnet); err != nil {
				klog.Error(err)
				return err
			}
		}
	}

	klog.Infof("finished reconciling bgp-edge-router-advertisement %s", key)

	return nil
}

func (c *Controller) handleUpdateBgpEdgeRouterAdvertisement(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.bgpEdgeRouterAdvertisementKeyMutex.LockKey(key)
	defer func() { _ = c.bgpEdgeRouterAdvertisementKeyMutex.UnlockKey(key) }()

	cachedAdvertisement, err := c.bgpEdgeRouterAdvertisementLister.BgpEdgeRouterAdvertisements(ns).Get(name)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Error(err)
			return err
		}
		return nil
	}

	if !cachedAdvertisement.DeletionTimestamp.IsZero() {
		c.delBgpEdgeRouterAdvertisementQueue.Add(key)
		return nil
	}

	klog.Infof("reconciling bgp-edge-router-advertisement %s", key)
	advertisement := cachedAdvertisement.DeepCopy()

	if controllerutil.AddFinalizer(advertisement, util.KubeOVNControllerFinalizer) {
		updatedAdvertisement, err := c.config.KubeOvnClient.KubeovnV1().BgpEdgeRouterAdvertisements(advertisement.Namespace).
			Update(context.Background(), advertisement, metav1.UpdateOptions{})
		if err != nil {
			err = fmt.Errorf("failed to add finalizer for bgp-edge-router %s/%s: %w", advertisement.Namespace, advertisement.Name, err)
			klog.Error(err)
			return err
		}
		advertisement = updatedAdvertisement
	}

	// reconcile the bgp edge router workload and get the route sources for later OVN resources reconciliation
	deploy, err := c.berDeploymentsLister.Deployments(advertisement.Namespace).Get(advertisement.Spec.BgpEdgeRouter)
	if err != nil {
		klog.Error(err)
		return err
	}

	ready := util.DeploymentIsReady(deploy)
	if !ready {
		advertisement.Status.Ready = false
		msg := fmt.Sprintf("Waiting for %s %s to be ready", deploy.Kind, deploy.Name)
		// advertisement.Status.Conditions.SetCondition(kubeovnv1.Ready, corev1.ConditionFalse, "Processing", msg, advertisement.Generation)
		advertisement.Status.Conditions.SetCondition(kubeovnv1.Validated, corev1.ConditionFalse, "BgpEdgeRouterNotEnabled", msg, advertisement.Generation)
		_, _ = c.updatebgpEdgeRouterAdvertisementStatus(advertisement)
		readyErr := fmt.Sprintf("Kind %s, Deployment %s is not ready", deploy.Kind, deploy.Name)
		klog.Error(readyErr)
		return fmt.Errorf("%s", readyErr)
	}
	// get the pods of the deployment to collect the pod IPs
	podSelector, err := metav1.LabelSelectorAsSelector(deploy.Spec.Selector)
	if err != nil {
		err = fmt.Errorf("failed to get pod selector of deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
		klog.Error(err)
		return err
	}

	pods, err := c.podsLister.Pods(deploy.Namespace).List(podSelector)
	if err != nil {
		err = fmt.Errorf("failed to list pods of deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
		klog.Error(err)
		return err
	}

	for _, pod := range pods {
		if len(pod.Status.PodIPs) == 0 {
			continue
		}
	}

	klog.Infof("finished reconciling bgp-edge-router-advertisement %s", key)

	return nil
}

func (c *Controller) handleDelBgpEdgeRouterAdvertisement(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.bgpEdgeRouterKeyMutex.LockKey(key)
	defer func() { _ = c.bgpEdgeRouterKeyMutex.UnlockKey(key) }()

	cachedAdvertisement, err := c.bgpEdgeRouterAdvertisementLister.BgpEdgeRouterAdvertisements(ns).Get(name)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Error(err)
			return err
		}
		return nil
	}

	if !cachedAdvertisement.DeletionTimestamp.IsZero() {
		c.delBgpEdgeRouterAdvertisementQueue.Add(key)
		return nil
	}

	klog.Infof("reconciling bgp-edge-router-advertisement %s", key)
	advertisement := cachedAdvertisement.DeepCopy()

	if pods, err := c.validateBgpEdgeRouterAdvertisement(advertisement); err != nil || pods == nil {
		klog.Error(err)
		return err
	} else {
		for _, pod := range pods {
			if len(pod.Status.PodIPs) == 0 {
				continue
			}
			klog.Infof("handle deleting bgp-edge-router-advertisement %s", key)
			if err = c.cleanBgpEdgeRouterAdvertisementRule(key, pod.Name, advertisement.Spec.Subnet); err != nil {
				klog.Error(err)
				return err
			}
		}
	}

	advertisement = cachedAdvertisement.DeepCopy()
	if controllerutil.RemoveFinalizer(advertisement, util.KubeOVNControllerFinalizer) {
		if _, err = c.config.KubeOvnClient.KubeovnV1().BgpEdgeRouterAdvertisements(advertisement.Namespace).
			Update(context.Background(), advertisement, metav1.UpdateOptions{}); err != nil {
			err = fmt.Errorf("failed to remove finalizer from bgp-edge-router-advertisement %s: %w", key, err)
			klog.Error(err)
		}
	}
	return nil
}

func (c *Controller) cleanBgpEdgeRouterAdvertisementRule(key, podName string, subnetNames []string) error {

	if podName == "" {
		err := fmt.Errorf("failed to get pod name %s", podName)
		klog.Error(err)
		return err
	}
	for _, subnetName := range subnetNames {
		if subnet, err := c.subnetsLister.Get(subnetName); err != nil {
			err = fmt.Errorf("failed to get subnet %s: %w", subnetName, err)
			klog.Error(err)
			return err
		} else {
			if subnet.Spec.CIDRBlock != "" {
				//delete bgp advertised ipblock
			}
			klog.Infof("cleaning bgp-edge-router-advertisement %s for subnet %s", key, subnet.Name)
		}

	}

	return nil
}

func (c *Controller) addBgpEdgeRouterAdvertisementRule(key, podName string, subnetNames []string) error {

	if podName == "" {
		err := fmt.Errorf("failed to get pod name %s", podName)
		klog.Error(err)
		return err
	}
	for _, subnetName := range subnetNames {
		if subnet, err := c.subnetsLister.Get(subnetName); err != nil {
			err = fmt.Errorf("failed to get subnet %s: %w", subnetName, err)
			klog.Error(err)
			return err
		} else {
			if subnet.Spec.CIDRBlock != "" {
				//delete bgp advertised ipblock
			}
			klog.Infof("cleaning bgp-edge-router-advertisement %s for subnet %s", key, subnet.Name)
		}

	}

	return nil
}

func (c *Controller) validateBgpEdgeRouterAdvertisement(advertisement *kubeovnv1.BgpEdgeRouterAdvertisement) ([]*corev1.Pod, error) {

	deploy, err := c.berDeploymentsLister.Deployments(advertisement.Namespace).Get(advertisement.Spec.BgpEdgeRouter)
	if err != nil {
		advertisement.Status.Ready = false
		msg := fmt.Sprintf("Waiting for %s %s to be ready", deploy.Kind, deploy.Name)
		advertisement.Status.Conditions.SetCondition(kubeovnv1.Validated, corev1.ConditionFalse, "BgpEdgeRouterDeployNotFound", msg, advertisement.Generation)
		_, _ = c.updatebgpEdgeRouterAdvertisementStatus(advertisement)
		klog.Error(err)
		return nil, err
	}

	ready := util.DeploymentIsReady(deploy)
	if !ready {
		advertisement.Status.Ready = false
		msg := fmt.Sprintf("Waiting for %s %s to be ready", deploy.Kind, deploy.Name)
		advertisement.Status.Conditions.SetCondition(kubeovnv1.Validated, corev1.ConditionFalse, "BgpEdgeRouterNotEnabled", msg, advertisement.Generation)
		_, _ = c.updatebgpEdgeRouterAdvertisementStatus(advertisement)
		readyErr := fmt.Sprintf("Kind %s, Deployment %s is not ready", deploy.Kind, deploy.Name)
		klog.Error(readyErr)
		return nil, fmt.Errorf("%s", readyErr)
	}
	// get the pods of the deployment to collect the pod IPs
	podSelector, err := metav1.LabelSelectorAsSelector(deploy.Spec.Selector)
	if err != nil {
		err = fmt.Errorf("failed to get pod selector of deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
		klog.Error(err)
		return nil, err
	}

	pods, err := c.podsLister.Pods(deploy.Namespace).List(podSelector)
	if err != nil {
		err = fmt.Errorf("failed to list pods of deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
		klog.Error(err)
		return nil, err
	}

	if ready {
		advertisement.Status.Ready = true
		advertisement.Status.Conditions.SetReady("ReconcileSuccess", advertisement.Generation)
		if _, err = c.updatebgpEdgeRouterAdvertisementStatus(advertisement); err != nil {
			return pods, err
		}
	}

	return pods, nil
}

func (c *Controller) initBgpEdgeRouterAdvertisementStatus(advertisement *kubeovnv1.BgpEdgeRouterAdvertisement) (*kubeovnv1.BgpEdgeRouterAdvertisement, error) {
	var err error
	advertisement, err = c.updatebgpEdgeRouterAdvertisementStatus(advertisement)
	return advertisement, err
}

func (c *Controller) updatebgpEdgeRouterAdvertisementStatus(advertisement *kubeovnv1.BgpEdgeRouterAdvertisement) (*kubeovnv1.BgpEdgeRouterAdvertisement, error) {
	if len(advertisement.Status.Conditions) == 0 {
		advertisement.Status.Conditions.SetCondition(kubeovnv1.Init, corev1.ConditionUnknown, "Processing", "", advertisement.Generation)
	}

	updateAdvertisement, err := c.config.KubeOvnClient.KubeovnV1().BgpEdgeRouterAdvertisements(advertisement.Namespace).
		UpdateStatus(context.Background(), advertisement, metav1.UpdateOptions{})
	if err != nil {
		err = fmt.Errorf("failed to update status of bgp-edge-router %s/%s: %w", advertisement.Namespace, advertisement.Name, err)
		klog.Error(err)
		return nil, err
	}

	return updateAdvertisement, nil
}
