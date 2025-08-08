package controller

import (
	"context"
	"fmt"
	"slices"

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

type updateVerGobgpConfigObject struct {
	key    string
	oldVer *kubeovnv1.GobgpConfig
	newVer *kubeovnv1.GobgpConfig
}

func (c *Controller) enqueueAddGobgpConfig(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.GobgpConfig)).String()
	klog.V(3).Infof("enqueue add gobgp-configuration %s", key)
	c.addGobgpConfigQueue.Add(key)
}

func (c *Controller) enqueueUpdateGobgpConfig(oldObj, newObj any) {
	key := cache.MetaObjectToName(newObj.(*kubeovnv1.GobgpConfig)).String()
	klog.V(3).Infof("enqueue update gobgp-configuration %s", key)

	if oldObj == nil || newObj == nil {
		klog.Warningf("enqueue update gobgp-configuration %s, but old object is nil", key)
		return
	}

	oldGobgpConfig := oldObj.(*kubeovnv1.GobgpConfig)
	newGobgpConfig := newObj.(*kubeovnv1.GobgpConfig)
	updateConfigVer := &updateVerGobgpConfigObject{
		key:    key,
		oldVer: oldGobgpConfig,
		newVer: newGobgpConfig,
	}

	c.updateGobgpConfigQueue.Add(updateConfigVer)
}

func (c *Controller) enqueueDeleteGobgpConfig(obj any) {
	var gobgpConfig *kubeovnv1.GobgpConfig
	switch t := obj.(type) {
	case *kubeovnv1.GobgpConfig:
		gobgpConfig = t
	case cache.DeletedFinalStateUnknown:
		if v, ok := t.Obj.(*kubeovnv1.GobgpConfig); ok {
			gobgpConfig = v
		}
	}
	if gobgpConfig == nil {
		klog.Warning("enqueueDeleteGobgpConfig: object is not GobgpConfig")
		return
	}
	key := cache.MetaObjectToName(obj.(*kubeovnv1.GobgpConfig)).String()
	klog.V(3).Infof("enqueue delete gobgp-config %s", key)
	c.deleteGobgpConfigQueue.Add(key)
}

func (c *Controller) handleAddGobgpConfig(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.gobgpConfigKeyMutex.LockKey(key)
	defer func() { _ = c.gobgpConfigKeyMutex.UnlockKey(key) }()

	cachedGobgpConfig, err := c.gobgpConfigLister.GobgpConfigs(ns).Get(name)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Error(err)
			return err
		}
		return nil
	}

	if !cachedGobgpConfig.DeletionTimestamp.IsZero() {
		c.deleteGobgpConfigQueue.Add(key)
		return nil
	}

	klog.V(3).Infof("debug gobgp-config %s", cachedGobgpConfig.Name)

	if _, err := c.initGobgpConfigStatus(cachedGobgpConfig); err != nil {
		klog.Error(err)
		return err
	}

	klog.Infof("reconciling gobgp-configuration %s", key)
	gobgpConfig := cachedGobgpConfig.DeepCopy()

	if controllerutil.AddFinalizer(gobgpConfig, util.KubeOVNControllerFinalizer) {
		updatedGobgpConfig, err := c.config.KubeOvnClient.KubeovnV1().GobgpConfigs(gobgpConfig.Namespace).
			Update(context.Background(),
				gobgpConfig, metav1.UpdateOptions{})
		if err != nil {
			err = fmt.Errorf("failed to add finalizer for gobgp-configuration %s/%s: %w", gobgpConfig.Namespace, gobgpConfig.Name, err)
			klog.Error(err)
			return err
		}
		gobgpConfig = updatedGobgpConfig
	}

	pods, err := c.validateGobgpConfig(gobgpConfig)
	if err != nil || pods == nil {
		klog.Error(err)
		return err
	}

	for _, pod := range pods {
		if len(pod.Status.PodIPs) == 0 {
			continue
		}
		klog.Infof("handle adding gobgp-config %s", key)
		// if err = c.addOrDeleteGobgpConfigRule("add", key, pod, gobgpConfig); err != nil {
		// 	klog.Error(err)
		// 	return err
		// }
	}

	gobgpConfig.Status.Conditions.SetReady("ReconcileSuccess", gobgpConfig.Generation)
	if _, err = c.updateGobgpConfigStatus(gobgpConfig); err != nil {
		return err
	}
	klog.Infof("finished reconciling gobgp-config %s", key)

	return nil
}

func (c *Controller) handleUpdateGobgpConfig(updatedObj *updateVerGobgpConfigObject) error {
	key := updatedObj.key

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.gobgpConfigKeyMutex.LockKey(key)
	defer func() { _ = c.gobgpConfigKeyMutex.UnlockKey(key) }()

	cachedGobgpConfig, err := c.gobgpConfigLister.GobgpConfigs(ns).Get(name)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Error(err)
			return err
		}
		return nil
	}

	if !cachedGobgpConfig.DeletionTimestamp.IsZero() {
		c.deleteGobgpConfigQueue.Add(key)
		return nil
	}

	klog.Infof("reconciling gobgp-configs %s", key)
	gobgpConfig := cachedGobgpConfig.DeepCopy()

	pods, err := c.validateGobgpConfig(gobgpConfig)
	if err != nil || pods == nil {
		klog.Error(err)
		return err
	}

	// for _, pod := range pods {
	// 	if len(pod.Status.PodIPs) == 0 {
	// 		continue
	// 	}
	// 	klog.Infof("handle adding gobgp-configs %s", key)
	// 	if err = c.updateGobgpConfigRule(key, pod, updatedObj.oldVer, updatedObj.newVer); err != nil {
	// 		klog.Error(err)
	// 		return err
	// 	}
	// }

	gobgpConfig.Status.Conditions.SetReady("ReconcileSuccess", gobgpConfig.Generation)
	if _, err = c.updateGobgpConfigStatus(gobgpConfig); err != nil {
		return err
	}
	klog.Infof("finished reconciling gobgp-configs %s", key)

	return nil
}

func (c *Controller) handleDelGobgpConfig(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.gobgpConfigKeyMutex.LockKey(key)
	defer func() { _ = c.gobgpConfigKeyMutex.UnlockKey(key) }()

	cachedGobgpConfig, err := c.gobgpConfigLister.GobgpConfigs(ns).Get(name)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Error(err)
			return err
		}
		return nil
	}

	klog.Infof("reconciling gobgp-configs %s", key)
	gobgpConfig := cachedGobgpConfig.DeepCopy()

	pods, err := c.validateGobgpConfig(gobgpConfig)
	if err != nil || pods == nil {
		klog.Error(err)
		return err
	}

	for _, pod := range pods {
		if len(pod.Status.PodIPs) == 0 {
			continue
		}
		klog.Infof("handle deleting gobgp-configs %s", key)
		// if err = c.addOrDeleteGobgpConfigRule("del", key, pod, gobgpConfig); err != nil {
		// 	klog.Error(err)
		// 	return err
		// }
	}

	gobgpConfig = cachedGobgpConfig.DeepCopy()
	if controllerutil.RemoveFinalizer(gobgpConfig, util.KubeOVNControllerFinalizer) {
		if _, err = c.config.KubeOvnClient.KubeovnV1().GobgpConfigs(gobgpConfig.Namespace).
			Update(context.Background(), gobgpConfig, metav1.UpdateOptions{}); err != nil {
			err = fmt.Errorf("failed to remove finalizer from gobgp-configs %s: %w", key, err)
			klog.Error(err)
		}
	}

	gobgpConfig.Status.Conditions.SetReady("ReconcileSuccess", gobgpConfig.Generation)
	if _, err = c.updateGobgpConfigStatus(gobgpConfig); err != nil {
		return err
	}
	klog.Infof("finished reconciling gobgp-config %s", key)

	return nil
}

// func (c *Controller) updateGobgpConfigRule(key string, pod *corev1.Pod, oldGobgpConfig, newGobgpConfig *kubeovnv1.GobgpConfig) error {
// 	if pod.Name == "" {
// 		err := fmt.Errorf("failed to get pod name %s", pod.Name)
// 		klog.Error(err)
// 		return err
// 	}
// 	// klog.Infof("update gobgp-config %s %s %s", key, oldGobgpConfig, newGobgpConfig)

// 	// var oldSubnetArray []string
// 	// var newSubnetArray []string

// 	// for _, neighbor := range oldGobgpConfig.Spec.Neighbors {
// 	// 	toAdvertisePrefixes := neighbor.ToAdvertise.Allowed.Prefixes
// 	// 	toReceivePrefixes := neighbor.ToReceive.Allowed.Prefixes

// 	// 	klog.Infof("cleaning gobgp-config %s for subnet %s", key, prefix)
// 	// }

// 	// for _, prefix := range newGobgpConfig.Spec.ToAdvertise.Allowed.Prefixes {
// 	// 	subnet, err := c.subnetsLister.Get(prefix)
// 	// 	if err != nil {
// 	// 		err = fmt.Errorf("failed to get subnet %s: %w", prefix, err)
// 	// 		klog.Error(err)
// 	// 		return err
// 	// 	}
// 	// 	if subnet.Spec.CIDRBlock != "" {
// 	// 		newSubnetArray = append(newSubnetArray, subnet.Spec.CIDRBlock)
// 	// 	}
// 	// 	klog.Infof("cleaning gobgp-config %s for subnet %s", key, subnet.Name)
// 	// }

// 	// if err := c.execUpdateBgpRoute(pod, oldSubnetArray, newSubnetArray); err != nil {
// 	// 	klog.Error(err)
// 	// 	return err
// 	// }

// 	return nil
// }

// func (c *Controller) addOrDeleteGobgpConfigRule(op, key string, pod *corev1.Pod, gobgpConfig *kubeovnv1.GobgpConfig) error {
// 	neighbors := gobgpConfig.Spec.Neighbors
// 	klog.Infof("add delete gobgp-config %s", key)

// 	subnetCidrArray := []string{}
// 	for _, neighbor := range neighbors {
// 		// address := neighbor.Address
// 		toAdvertise := neighbor.ToAdvertise.Allowed
// 		toReceive := neighbor.ToReceive.Allowed
// 		if toAdvertise.Mode == "all" {
// 			// toAdvertisePrefixes := toAdvertise.Prefixes
// 			// toReceivePrefixes := toReceive.Prefixes
// 			// Exec gobgpConfig command to advertise prefixes
// 		} else {
// 			// toAdvertisePrefixes := toAdvertise.Prefixes
// 			// toReceivePrefixes := toReceive.Prefixes
// 			// Exec gobgpConfig command to advertise prefixes
// 		}

// 		if toReceive.Mode == "all" {
// 			// toAdvertisePrefixes := toAdvertise.Prefixes
// 			// toReceivePrefixes := toReceive.Prefixes
// 			// Exec gobgpConfig command to advertise prefixes
// 		} else {
// 			// toAdvertisePrefixes := toAdvertise.Prefixes
// 			// toReceivePrefixes := toReceive.Prefixes
// 			// Exec gobgpConfig command to advertise prefixes
// 		}
// 	}

// 	if op == "add" {
// 		if err := c.execUpdateBgpRoute(pod, nil, subnetCidrArray); err != nil {
// 			klog.Error(err)
// 			return err
// 		}
// 	} else {
// 		if err := c.execUpdateBgpRoute(pod, subnetCidrArray, nil); err != nil {
// 			klog.Error(err)
// 			return err
// 		}
// 	}

// 	return nil
// }

func (c *Controller) validateGobgpConfig(gobgpConfig *kubeovnv1.GobgpConfig) ([]*corev1.Pod, error) {
	ber, err := c.bgpEdgeRouterLister.BgpEdgeRouters(gobgpConfig.Spec.BgpEdgeRouterInfo.Namespace).Get(gobgpConfig.Spec.BgpEdgeRouterInfo.Name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, fmt.Errorf("bgp edge router %s not found: %w", gobgpConfig.Spec.BgpEdgeRouterInfo.Name, err)
		}
	}
	berNeighbors := ber.Spec.BGP.Neighbors
	gobgpNeighbors := gobgpConfig.Spec.Neighbors
	neighborFlag := false
	for _, gNeighbor := range gobgpNeighbors {
		if containsNeighbor(berNeighbors, gNeighbor.Address) {
			neighborFlag = true
			break
		}
	}

	if !neighborFlag {
		err = fmt.Errorf("no matching neighbor found in BgpEdgeRouter %s for GobgpConfig %s", gobgpConfig.Spec.BgpEdgeRouterInfo.Name, gobgpConfig.Name)
		klog.Error(err)
		return nil, err
	}

	deploy, err := c.berDeploymentsLister.Deployments(gobgpConfig.Spec.BgpEdgeRouterInfo.Namespace).Get(gobgpConfig.Spec.BgpEdgeRouterInfo.Name)
	if err != nil {
		gobgpConfig.Status.Ready = false
		msg := fmt.Sprintf("Waiting for %s %s to be ready", deploy.Kind, deploy.Name)
		gobgpConfig.Status.Conditions.SetCondition(kubeovnv1.Validated, corev1.ConditionFalse, "BgpEdgeRouterDeployNotFound", msg, gobgpConfig.Generation)
		_, _ = c.updateGobgpConfigStatus(gobgpConfig)
		klog.Error(err)
		return nil, err
	}

	ready := util.DeploymentIsReady(deploy)
	if !ready {
		gobgpConfig.Status.Ready = false
		msg := fmt.Sprintf("Waiting for %s %s to be ready", deploy.Kind, deploy.Name)
		gobgpConfig.Status.Conditions.SetCondition(kubeovnv1.Validated, corev1.ConditionFalse, "BgpEdgeRouterNotEnabled", msg, gobgpConfig.Generation)
		_, _ = c.updateGobgpConfigStatus(gobgpConfig)
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
		gobgpConfig.Status.Ready = true
	}

	return pods, nil
}

func (c *Controller) initGobgpConfigStatus(gobgpConfig *kubeovnv1.GobgpConfig) (*kubeovnv1.GobgpConfig, error) {
	var err error
	gobgpConfig, err = c.updateGobgpConfigStatus(gobgpConfig)
	return gobgpConfig, err
}

func (c *Controller) updateGobgpConfigStatus(gobgpConfig *kubeovnv1.GobgpConfig) (*kubeovnv1.GobgpConfig, error) {
	if len(gobgpConfig.Status.Conditions) == 0 {
		gobgpConfig.Status.Conditions.SetCondition(kubeovnv1.Init, corev1.ConditionUnknown, "Processing", "", gobgpConfig.Generation)
	}

	updatedGobgpConfig, err := c.config.KubeOvnClient.KubeovnV1().GobgpConfigs(gobgpConfig.Namespace).
		UpdateStatus(context.Background(), gobgpConfig, metav1.UpdateOptions{})
	if err != nil {
		err = fmt.Errorf("failed to update status of gobgp-config %s/%s: %w", gobgpConfig.Namespace, gobgpConfig.Name, err)
		klog.Error(err)
		return nil, err
	}

	return updatedGobgpConfig, nil
}

func containsNeighbor(neighbors []string, address string) bool {
	return slices.Contains(neighbors, address)
}
