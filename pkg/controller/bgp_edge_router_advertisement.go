package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/set"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type updateVerObject struct {
	key    string
	oldVer *kubeovnv1.BgpEdgeRouterAdvertisement
	newVer *kubeovnv1.BgpEdgeRouterAdvertisement
}

func (c *Controller) enqueueAddBgpEdgeRouterAdvertisement(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.BgpEdgeRouterAdvertisement)).String()
	klog.V(3).Infof("enqueue add bgp-edge-router-advertisement %s", key)
	c.addBgpEdgeRouterAdvertisementQueue.Add(key)
}

func (c *Controller) enqueueUpdateBgpEdgeRouterAdvertisement(oldObj, newObj any) {
	key := cache.MetaObjectToName(newObj.(*kubeovnv1.BgpEdgeRouterAdvertisement)).String()
	klog.V(3).Infof("enqueue update bgp-edge-router-advertisement %s", key)

	if oldObj == nil || newObj == nil {
		klog.Warningf("enqueue update bgp-edge-router-advertisement %s, but old object is nil", key)
		return
	}

	oldRouter := oldObj.(*kubeovnv1.BgpEdgeRouterAdvertisement)
	newRouter := newObj.(*kubeovnv1.BgpEdgeRouterAdvertisement)
	updateVer := &updateVerObject{
		key:    key,
		oldVer: oldRouter,
		newVer: newRouter,
	}

	if !newRouter.DeletionTimestamp.IsZero() {
		c.deleteBgpEdgeRouterAdvertisementQueue.Add(key)
		return
	}

	if !reflect.DeepEqual(oldRouter.Spec, newRouter.Spec) {
		klog.Infof("enqueue update bgp-edge-router-advertisement %s", key)
		c.updateBgpEdgeRouterAdvertisementQueue.Add(updateVer)
	}
}

func (c *Controller) enqueueDeleteBgpEdgeRouterAdvertisement(obj any) {
	var berAd *kubeovnv1.BgpEdgeRouterAdvertisement
	switch t := obj.(type) {
	case *kubeovnv1.BgpEdgeRouterAdvertisement:
		berAd = t
	case cache.DeletedFinalStateUnknown:
		if v, ok := t.Obj.(*kubeovnv1.BgpEdgeRouterAdvertisement); ok {
			berAd = v
		}
	}
	if berAd == nil {
		klog.Warning("enqueueDeleteBgpEdgeRouterAdvertisement: object is not BgpEdgeRouterAdvertisement")
		return
	}
	key := cache.MetaObjectToName(obj.(*kubeovnv1.BgpEdgeRouterAdvertisement)).String()
	klog.V(3).Infof("enqueue delete bgp-edge-router-advertisement %s", key)
	c.deleteBgpEdgeRouterAdvertisementQueue.Add(key)
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
		c.deleteBgpEdgeRouterAdvertisementQueue.Add(key)
		return nil
	}
	klog.V(3).Infof("debug bgp-edge-router-advertisement %s", cachedAdvertisement.Name)

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

	pods, err := c.validateBgpEdgeRouterAdvertisement(advertisement)
	if err != nil || pods == nil {
		klog.Error(err)
		return err
	}

	for _, pod := range pods {
		if len(pod.Status.PodIPs) == 0 {
			continue
		}
		klog.Infof("handle adding bgp-edge-router-advertisement %s", key)
		if err = c.addOrDeleteBgpEdgeRouterAdvertisementRule("add", key, pod, advertisement.Spec.Subnet); err != nil {
			klog.Error(err)
			return err
		}
	}

	advertisement.Status.Conditions.SetReady("ReconcileSuccess", advertisement.Generation)
	if _, err = c.updatebgpEdgeRouterAdvertisementStatus(advertisement); err != nil {
		return err
	}

	// update ber address_set
	if err := c.updateAddressSetForBer(ns, advertisement, "add"); err != nil {
		klog.Error(err)
		return err
	}

	klog.Infof("finished reconciling bgp-edge-router-advertisement %s", key)

	return nil
}

func (c *Controller) updateAddressSetForBer(ns string, advertisement *kubeovnv1.BgpEdgeRouterAdvertisement, op string) error {
	// modify ber address_set
	berName := advertisement.Spec.BgpEdgeRouter
	cachedRouter, err := c.bgpEdgeRouterLister.BgpEdgeRouters(ns).Get(berName)
	if err != nil {
		klog.Error(err)
		return err
	}
	router := cachedRouter.DeepCopy()
	// collect egress policies
	ipv4ForwardSrc, ipv6ForwardSrc := set.New[string](), set.New[string]()
	ipv4SNATSrc, ipv6SNATSrc := set.New[string](), set.New[string]()
	for _, policy := range router.Spec.Policies {
		ipv4, ipv6 := util.SplitIpsByProtocol(policy.IPBlocks)
		if policy.SNAT {
			ipv4SNATSrc.Insert(ipv4...)
			ipv6SNATSrc.Insert(ipv6...)
		} else {
			ipv4ForwardSrc.Insert(ipv4...)
			ipv6ForwardSrc.Insert(ipv6...)
		}
		for _, subnetName := range policy.Subnets {
			subnet, err := c.subnetsLister.Get(subnetName)
			if err != nil {
				klog.Error(err)
				return err
			}
			if subnet.Status.IsNotValidated() {
				err = fmt.Errorf("subnet %s is not validated", subnet.Name)
				klog.Error(err)
				return err
			}
			// TODO: check subnet's vpc and vlan
			ipv4, ipv6 := util.SplitStringIP(subnet.Spec.CIDRBlock)
			if policy.SNAT {
				ipv4SNATSrc.Insert(ipv4)
				ipv6SNATSrc.Insert(ipv6)
			} else {
				ipv4ForwardSrc.Insert(ipv4)
				ipv6ForwardSrc.Insert(ipv6)
			}
		}
	}

	// collect advertisement subnets
	if op == "add" {
		advCidrBlocks, err := c.getSubnetCidrBlock(advertisement)
		if err != nil {
			klog.Error(err)
			return err
		}
		for _, advCidrBlock := range advCidrBlocks {
			ipv4adv, ipv6adv := util.SplitStringIP(advCidrBlock)
			ipv4ForwardSrc.Insert(ipv4adv)
			ipv6ForwardSrc.Insert(ipv6adv)
		}
	}

	// calculate internal route destinations and forward source CIDR blocks
	intRouteDstIPv4, intRouteDstIPv6 := ipv4ForwardSrc.Union(ipv4SNATSrc), ipv6ForwardSrc.Union(ipv6SNATSrc)
	intRouteDstIPv4.Delete("")
	intRouteDstIPv6.Delete("")
	ipv4ForwardSrc.Delete("")
	ipv6ForwardSrc.Delete("")

	klog.Infof("setting address set for bgp edge router : %s, intRouteDstIPv4 %v, intRouteDstIPv6 %v", berName, intRouteDstIPv4, intRouteDstIPv6)
	berKey := cache.MetaObjectToName(router).String()
	klog.Infof("debug bgp-edge-router %s", berKey)
	if intRouteDstIPv4.Len() > 0 {
		asName := berAddressSetName(berKey, 4)
		klog.Infof("address set name: %s", asName)
		if err = c.OVNNbClient.AddressSetUpdateAddress(asName, intRouteDstIPv4.SortedList()...); err != nil {
			klog.Error(err)
			err = fmt.Errorf("failed to create or update address set %s: %w", asName, err)
			klog.Error(err)
			return err
		}
	}
	if intRouteDstIPv6.Len() > 0 {
		asName := berAddressSetName(berKey, 6)
		if err = c.OVNNbClient.AddressSetUpdateAddress(asName, intRouteDstIPv6.SortedList()...); err != nil {
			klog.Error(err)
			err = fmt.Errorf("failed to create or update address set %s: %w", asName, err)
			klog.Error(err)
			return err
		}
	}
	return nil
}

func (c *Controller) handleUpdateBgpEdgeRouterAdvertisement(updatedObj *updateVerObject) error {
	key := updatedObj.key

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
		c.deleteBgpEdgeRouterAdvertisementQueue.Add(key)
		return nil
	}

	klog.Infof("reconciling bgp-edge-router-advertisement %s", key)
	advertisement := cachedAdvertisement.DeepCopy()

	pods, err := c.validateBgpEdgeRouterAdvertisement(advertisement)
	if err != nil || pods == nil {
		klog.Error(err)
		return err
	}

	for _, pod := range pods {
		if len(pod.Status.PodIPs) == 0 {
			continue
		}
		klog.Infof("handle adding bgp-edge-router-advertisement %s", key)
		if err = c.updateBgpEdgeRouterAdvertisementRule(key, pod, updatedObj.oldVer, updatedObj.newVer); err != nil {
			klog.Error(err)
			return err
		}
	}

	// update ber address_set
	if err := c.updateAddressSetForBer(ns, advertisement, "add"); err != nil {
		klog.Error(err)
		return err
	}

	advertisement.Status.Conditions.SetReady("ReconcileSuccess", advertisement.Generation)
	if _, err = c.updatebgpEdgeRouterAdvertisementStatus(advertisement); err != nil {
		return err
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

	klog.Infof("reconciling bgp-edge-router-advertisement %s", key)
	advertisement := cachedAdvertisement.DeepCopy()

	pods, err := c.validateBgpEdgeRouterAdvertisement(advertisement)
	if err != nil || pods == nil {
		klog.Error(err)
		return err
	}

	for _, pod := range pods {
		if len(pod.Status.PodIPs) == 0 {
			continue
		}
		klog.Infof("handle deleting bgp-edge-router-advertisement %s", key)
		if err = c.addOrDeleteBgpEdgeRouterAdvertisementRule("del", key, pod, advertisement.Spec.Subnet); err != nil {
			klog.Error(err)
			return err
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

	// update ber address_set
	if err := c.updateAddressSetForBer(ns, advertisement, "del"); err != nil {
		klog.Error(err)
		return err
	}

	// advertisement.Status.Conditions.SetReady("ReconcileSuccess", advertisement.Generation)
	// if _, err = c.updatebgpEdgeRouterAdvertisementStatus(advertisement); err != nil {
	// 	return err
	// }

	klog.Infof("finished reconciling bgp-edge-router-advertisement %s", key)

	return nil
}

func (c *Controller) updateBgpEdgeRouterAdvertisementRule(key string, pod *corev1.Pod, oldBerAd, newBerAd *kubeovnv1.BgpEdgeRouterAdvertisement) error {
	if pod.Name == "" {
		err := fmt.Errorf("failed to get pod name %s", pod.Name)
		klog.Error(err)
		return err
	}
	var oldSubnetArray []string
	var newSubnetArray []string

	for _, subnetName := range oldBerAd.Spec.Subnet {
		var subnet *kubeovnv1.Subnet
		var err error
		if subnet, err = c.subnetsLister.Get(subnetName); err != nil {
			err = fmt.Errorf("failed to get subnet %s: %w", subnetName, err)
			klog.Error(err)
			return err
		}
		if subnet.Spec.CIDRBlock != "" {
			oldSubnetArray = append(oldSubnetArray, subnet.Spec.CIDRBlock)
		}
		klog.Infof("cleaning bgp-edge-router-advertisement %s for subnet %s", key, subnet.Name)
	}

	for _, subnetName := range newBerAd.Spec.Subnet {
		subnet, err := c.subnetsLister.Get(subnetName)
		if err != nil {
			err = fmt.Errorf("failed to get subnet %s: %w", subnetName, err)
			klog.Error(err)
			return err
		}
		if subnet.Spec.CIDRBlock != "" {
			newSubnetArray = append(newSubnetArray, subnet.Spec.CIDRBlock)
		}
		klog.Infof("cleaning bgp-edge-router-advertisement %s for subnet %s", key, subnet.Name)
	}

	if err := c.execUpdateBgpRoute(pod, oldSubnetArray, newSubnetArray); err != nil {
		klog.Error(err)
		return err
	}

	return nil
}

func (c *Controller) addOrDeleteBgpEdgeRouterAdvertisementRule(op, key string, pod *corev1.Pod, subnetNames []string) error {
	if pod.Name == "" {
		err := fmt.Errorf("failed to get pod name %s", pod.Name)
		klog.Error(err)
		return err
	}
	SubnetCidrArray := []string{}
	for _, subnetName := range subnetNames {
		subnet, err := c.subnetsLister.Get(subnetName)
		if err != nil {
			err = fmt.Errorf("failed to get subnet %s: %w", subnetName, err)
			klog.Error(err)
			return err
		}
		if subnet.Spec.CIDRBlock != "" {
			SubnetCidrArray = append(SubnetCidrArray, subnet.Spec.CIDRBlock)
		}
		klog.Infof("cleaning bgp-edge-router-advertisement %s for subnet %s", key, subnet.Name)
	}

	if op == "add" {
		if err := c.execUpdateBgpRoute(pod, nil, SubnetCidrArray); err != nil {
			klog.Error(err)
			return err
		}
	} else {
		if err := c.execUpdateBgpRoute(pod, SubnetCidrArray, nil); err != nil {
			klog.Error(err)
			return err
		}
	}

	return nil
}

func (c *Controller) resyncBgpRules() {
	klog.Info("resync bgp edge router")
	// resync all bgp edge routers
	bgpEdgeRouterAds, err := c.bgpEdgeRouterAdvertisementLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list bgp edge routers: %v", err)
		return
	}

	for _, bgpEdgeRouterAd := range bgpEdgeRouterAds {
		// Check router.Spec.BGP.AdvertisedRoutes same with pods bgp advertised routes
		if err := c.syncAdvertisedRoutes(bgpEdgeRouterAd); err != nil {
			klog.Errorf("failed to sync advertised routes for bgp edge router %s: %v", bgpEdgeRouterAd.Name, err)
			continue
		}
		klog.Infof("resync bgp edge router %s", bgpEdgeRouterAd.Name)
	}
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

func (c *Controller) execUpdateBgpRoute(pod *corev1.Pod, oldCidrs, newCidrs []string) error {
	// add_announced_route
	cmdArs := []string{}
	if len(oldCidrs) > 0 {
		cmdArs = append(cmdArs, "del_announced_route="+strings.Join(oldCidrs, ","))
	}
	if len(newCidrs) > 0 {
		cmdArs = append(cmdArs, "add_announced_route="+strings.Join(newCidrs, ","))
	}
	cmdArs = append(cmdArs, "list_announced_route")
	cmd := fmt.Sprintf("bash /kube-ovn/update-bgp-route.sh %s", strings.Join(cmdArs, " "))

	klog.Infof("exec command : %s", cmd)
	stdOutput, errOutput, err := util.ExecuteCommandInContainer(c.config.KubeClient, c.config.KubeRestConfig, pod.Namespace, pod.Name, "bgp-router-speaker", []string{"/bin/bash", "-c", cmd}...)
	if err != nil {
		if len(errOutput) > 0 {
			klog.Errorf("failed to ExecuteCommandInContainer, errOutput: %v", errOutput)
		}
		if len(stdOutput) > 0 {
			klog.Infof("failed to ExecuteCommandInContainer, stdOutput: %v", stdOutput)
		}
		klog.Error(err)
		return err
	}

	if len(stdOutput) > 0 {
		klog.Infof("ExecuteCommandInContainer stdOutput: %v", stdOutput)
	}

	if len(errOutput) > 0 {
		klog.Errorf("failed to ExecuteCommandInContainer errOutput: %v", errOutput)
		return errors.New(errOutput)
	}

	// list the current rule and check if the routes are updated

	return nil
}

func (c *Controller) syncAdvertisedRoutes(advertisement *kubeovnv1.BgpEdgeRouterAdvertisement) error {
	key := cache.MetaObjectToName(advertisement).String()

	c.bgpEdgeRouterAdvertisementKeyMutex.LockKey(key)
	defer func() { _ = c.bgpEdgeRouterAdvertisementKeyMutex.UnlockKey(key) }()

	if !advertisement.DeletionTimestamp.IsZero() {
		c.deleteBgpEdgeRouterAdvertisementQueue.Add(key)
		return nil
	}
	klog.Infof("reconciling bgp-edge-router %s", key)
	// Deep copy because we might mutate Status below.
	cachedAdvertisement := advertisement.DeepCopy()

	pods, err := c.validateBgpEdgeRouterAdvertisement(cachedAdvertisement)
	if err != nil || pods == nil {
		klog.Error(err)
		return err
	}
	cidrBlock, err := c.getSubnetCidrBlock(cachedAdvertisement)
	if err != nil {
		klog.Error(err)
		return err
	}
	for _, pod := range pods {
		if len(pod.Status.PodIPs) == 0 {
			continue
		}
		podCidr, err := c.execGetBgpRoute(pod)
		if err != nil {
			return err
		}
		klog.Infof("current router advertised routes: %v", cidrBlock)
		klog.Infof("router pod %s/%s advertised routes: %v", pod.Namespace, pod.Name, podCidr)
		routesDiff := !slicesEqual(podCidr, cidrBlock)
		if routesDiff {
			if err := c.execUpdateBgpRoute(pod, podCidr, cidrBlock); err != nil {
				return err
			}
			klog.Infof("synced advertised routes for bgp-router-speaker %s pod %s/%s", key, pod.Namespace, pod.Name)
		}
	}

	klog.Infof("finished sync bgp-edge-router %s advertised routes", key)
	return nil
}

func (c *Controller) execGetBgpRoute(routerPod *corev1.Pod) ([]string, error) {
	cmd := "bash /kube-ovn/update-bgp-route.sh list_announced_route"
	klog.Infof("exec command : %s", cmd)
	stdOutput, errOutput, err := util.ExecuteCommandInContainer(c.config.KubeClient, c.config.KubeRestConfig, routerPod.Namespace, routerPod.Name, "bgp-router-speaker", []string{"/bin/bash", "-c", cmd}...)
	if err != nil {
		if len(errOutput) > 0 {
			klog.Errorf("failed to ExecuteCommandInContainer, errOutput: %v", errOutput)
		}
		klog.Error(err)
		return nil, err
	}

	if len(stdOutput) > 0 {
		klog.Infof("ExecuteCommandInContainer stdOutput: %v", stdOutput)
	}
	if len(errOutput) > 0 {
		klog.Errorf("failed to ExecuteCommandInContainer errOutput: %v", errOutput)
		return nil, errors.New(errOutput)
	}

	// Parse the output to extract announced routes
	announcedRoutes, err := c.parseBgpAnnouncedRoutes(stdOutput)
	if err != nil {
		klog.Errorf("failed to parse BGP announced routes: %v", err)
		return nil, err
	}

	return announcedRoutes, nil
}

func (c *Controller) getSubnetCidrBlock(advertisement *kubeovnv1.BgpEdgeRouterAdvertisement) ([]string, error) {
	var cirdBlock []string
	for _, subnetName := range advertisement.Spec.Subnet {
		var subnet *kubeovnv1.Subnet
		var err error
		subnet, err = c.subnetsLister.Get(subnetName)
		if err != nil {
			err = fmt.Errorf("failed to get subnet %s: %w", subnetName, err)
			klog.Error(err)
			return nil, err
		}
		if subnet.Spec.CIDRBlock != "" {
			cirdBlock = append(cirdBlock, subnet.Spec.CIDRBlock)
		}
	}
	return cirdBlock, nil
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	// Create copies and sort them
	aCopy := make([]string, len(a))
	bCopy := make([]string, len(b))
	copy(aCopy, a)
	copy(bCopy, b)

	sort.Strings(aCopy)
	sort.Strings(bCopy)

	return slices.Equal(aCopy, bCopy)
}

func (c *Controller) parseBgpAnnouncedRoutes(output string) ([]string, error) {
	var routes []string

	// Look for the specific section with next-hop routes
	lines := strings.Split(output, "\n")
	inTargetSection := false
	foundRoutesSection := false

	// Regex to match route lines starting with "*>" followed by CIDR
	routeRegex := regexp.MustCompile(`^\*>\s+(\d+\.\d+\.\d+\.\d+/\d+)`)

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Start parsing when we find the target section with any IP address
		if strings.Contains(line, "--- Routes with Next-Hop") && strings.Contains(line, "---") {
			inTargetSection = true
			continue
		}

		// Look for the IPv4 routes subsection
		if inTargetSection && strings.Contains(line, "IPv4 routes with next-hop") {
			foundRoutesSection = true
			continue
		}

		// Stop parsing if we hit another section starting with "---" or "==="
		if inTargetSection && foundRoutesSection && (strings.HasPrefix(line, "---") || strings.HasPrefix(line, "===")) {
			break
		}

		// Skip header lines (Network, Next Hop, AS_PATH, etc.)
		if inTargetSection && (strings.Contains(line, "Network") && strings.Contains(line, "Next Hop")) {
			continue
		}

		// Parse route lines in the target section that start with "*>"
		if inTargetSection && foundRoutesSection && routeRegex.MatchString(line) {
			matches := routeRegex.FindStringSubmatch(line)
			if len(matches) > 1 {
				routes = append(routes, matches[1])
			}
		}
	}

	if len(routes) == 0 {
		return nil, errors.New("no announced routes found in BGP output")
	}

	return routes, nil
}
