package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	vpcNatEnabled   = "unknown"
	VpcNatCmVersion = ""
	natGwCreatedAT  = ""
)

const (
	natGwInit              = "init"
	natGwEipAdd            = "eip-add"
	natGwEipDel            = "eip-del"
	natGwDnatAdd           = "dnat-add"
	natGwDnatDel           = "dnat-del"
	natGwSnatAdd           = "snat-add"
	natGwSnatDel           = "snat-del"
	natGwEipIngressQoSAdd  = "eip-ingress-qos-add"
	natGwEipIngressQoSDel  = "eip-ingress-qos-del"
	QoSAdd                 = "qos-add"
	QoSDel                 = "qos-del"
	natGwEipEgressQoSAdd   = "eip-egress-qos-add"
	natGwEipEgressQoSDel   = "eip-egress-qos-del"
	natGwSubnetFipAdd      = "floating-ip-add"
	natGwSubnetFipDel      = "floating-ip-del"
	natGwSubnetRouteAdd    = "subnet-route-add"
	natGwSubnetRouteDel    = "subnet-route-del"
	natGwExtSubnetRouteAdd = "ext-subnet-route-add"

	getIptablesVersion = "get-iptables-version"
)

func (c *Controller) resyncVpcNatGwConfig() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcNatGatewayConfig)
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to get ovn-vpc-nat-gw-config, %v", err)
		return
	}

	if k8serrors.IsNotFound(err) || cm.Data["enable-vpc-nat-gw"] == "false" {
		if vpcNatEnabled == "false" {
			return
		}
		klog.Info("start to clean up vpc nat gateway")
		if err := c.cleanUpVpcNatGw(); err != nil {
			klog.Errorf("failed to clean up vpc nat gateway, %v", err)
			return
		}
		vpcNatEnabled = "false"
		VpcNatCmVersion = ""
		klog.Info("finish clean up vpc nat gateway")
		return
	}
	if vpcNatEnabled == "true" && VpcNatCmVersion == cm.ResourceVersion {
		return
	}
	gws, err := c.vpcNatGatewayLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to get vpc nat gateway, %v", err)
		return
	}
	if err = c.resyncVpcNatImage(); err != nil {
		klog.Errorf("failed to resync vpc nat config, err: %v", err)
		return
	}
	vpcNatEnabled = "true"
	VpcNatCmVersion = cm.ResourceVersion
	for _, gw := range gws {
		c.addOrUpdateVpcNatGatewayQueue.Add(gw.Name)
	}
	klog.Info("finish establishing vpc-nat-gateway")
}

func (c *Controller) enqueueAddVpcNatGw(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add vpc-nat-gw %s", key)
	c.addOrUpdateVpcNatGatewayQueue.Add(key)
}

func (c *Controller) enqueueUpdateVpcNatGw(_, newObj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue update vpc-nat-gw %s", key)
	c.addOrUpdateVpcNatGatewayQueue.Add(key)
}

func (c *Controller) enqueueDeleteVpcNatGw(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue del vpc-nat-gw %s", key)
	c.delVpcNatGatewayQueue.Add(key)
}

func (c *Controller) runAddOrUpdateVpcNatGwWorker() {
	for c.processNextWorkItem("addOrUpdateVpcNatGateway", c.addOrUpdateVpcNatGatewayQueue, c.handleAddOrUpdateVpcNatGw) {
	}
}

func (c *Controller) runInitVpcNatGwWorker() {
	for c.processNextWorkItem("initVpcNatGateway", c.initVpcNatGatewayQueue, c.handleInitVpcNatGw) {
	}
}

func (c *Controller) runDelVpcNatGwWorker() {
	for c.processNextWorkItem("delVpcNatGateway", c.delVpcNatGatewayQueue, c.handleDelVpcNatGw) {
	}
}

func (c *Controller) runUpdateVpcFloatingIPWorker() {
	for c.processNextWorkItem("updateVpcFloatingIp", c.updateVpcFloatingIPQueue, c.handleUpdateVpcFloatingIP) {
	}
}

func (c *Controller) runUpdateVpcEipWorker() {
	for c.processNextWorkItem("UpdateVpcEip", c.updateVpcEipQueue, c.handleUpdateVpcEip) {
	}
}

func (c *Controller) runUpdateVpcDnatWorker() {
	for c.processNextWorkItem("updateVpcDnat", c.updateVpcDnatQueue, c.handleUpdateVpcDnat) {
	}
}

func (c *Controller) runUpdateVpcSnatWorker() {
	for c.processNextWorkItem("updateVpcSnat", c.updateVpcSnatQueue, c.handleUpdateVpcSnat) {
	}
}

func (c *Controller) runUpdateVpcSubnetWorker() {
	for c.processNextWorkItem("updateVpcSubnet", c.updateVpcSubnetQueue, c.handleUpdateNatGwSubnetRoute) {
	}
}

func (c *Controller) processNextWorkItem(processName string, queue workqueue.RateLimitingInterface, handler func(key string) error) bool {
	obj, shutdown := queue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer queue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			queue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := handler(key); err != nil {
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		queue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("process: %s. err: %v", processName, err))
		queue.AddRateLimited(obj)
		return true
	}
	return true
}

func (c *Controller) handleDelVpcNatGw(key string) error {
	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()

	name := util.GenNatGwStsName(key)
	klog.Infof("delete vpc nat gw %s", name)
	if err := c.config.KubeClient.AppsV1().StatefulSets(c.config.PodNamespace).Delete(context.Background(),
		name, metav1.DeleteOptions{}); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	return nil
}

func isVpcNatGwChanged(gw *kubeovnv1.VpcNatGateway) bool {
	if !reflect.DeepEqual(gw.Spec.ExternalSubnets, gw.Status.ExternalSubnets) {
		gw.Status.ExternalSubnets = gw.Spec.ExternalSubnets
		return true
	}
	if !reflect.DeepEqual(gw.Spec.Selector, gw.Status.Selector) {
		gw.Status.Selector = gw.Spec.Selector
		return true
	}
	if !reflect.DeepEqual(gw.Spec.Tolerations, gw.Status.Tolerations) {
		gw.Status.Tolerations = gw.Spec.Tolerations
		return true
	}
	if !reflect.DeepEqual(gw.Spec.Affinity, gw.Status.Affinity) {
		gw.Status.Affinity = gw.Spec.Affinity
		return true
	}
	return false
}

func (c *Controller) handleAddOrUpdateVpcNatGw(key string) error {
	gw, err := c.vpcNatGatewayLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	// create nat gw statefulset
	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle add/update vpc nat gateway %s", key)

	if vpcNatEnabled != "true" {
		return fmt.Errorf("iptables nat gw not enable")
	}

	if _, err := c.vpcsLister.Get(gw.Spec.Vpc); err != nil {
		err = fmt.Errorf("failed to get vpc '%s', err: %v", gw.Spec.Vpc, err)
		klog.Error(err)
		return err
	}
	if _, err := c.subnetsLister.Get(gw.Spec.Subnet); err != nil {
		err = fmt.Errorf("failed to get subnet '%s', err: %v", gw.Spec.Subnet, err)
		klog.Error(err)
		return err
	}

	// check or create statefulset
	needToCreate := false
	needToUpdate := false
	oldSts, err := c.config.KubeClient.AppsV1().StatefulSets(c.config.PodNamespace).
		Get(context.Background(), util.GenNatGwStsName(gw.Name), metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			needToCreate = true
		} else {
			klog.Error(err)
			return err
		}
	}
	newSts := c.genNatGwStatefulSet(gw, oldSts.DeepCopy())
	if !needToCreate && isVpcNatGwChanged(gw) {
		needToUpdate = true
	}

	switch {
	case needToCreate:
		// if pod create successfully, will add initVpcNatGatewayQueue
		if _, err := c.config.KubeClient.AppsV1().StatefulSets(c.config.PodNamespace).
			Create(context.Background(), newSts, metav1.CreateOptions{}); err != nil {
			err := fmt.Errorf("failed to create statefulset '%s', err: %v", newSts.Name, err)
			klog.Error(err)
			return err
		}
		if err = c.patchNatGwStatus(key); err != nil {
			klog.Errorf("failed to patch nat gw sts status for nat gw %s, %v", key, err)
			return err
		}
		return nil
	case needToUpdate:
		if _, err := c.config.KubeClient.AppsV1().StatefulSets(c.config.PodNamespace).
			Update(context.Background(), newSts, metav1.UpdateOptions{}); err != nil {
			err := fmt.Errorf("failed to update statefulset '%s', err: %v", newSts.Name, err)
			klog.Error(err)
			return err
		}
		if err = c.patchNatGwStatus(key); err != nil {
			klog.Errorf("failed to patch nat gw sts status for nat gw %s, %v", key, err)
			return err
		}
	default:
		// check if need to change qos
		if gw.Spec.QoSPolicy != gw.Status.QoSPolicy {
			if gw.Status.QoSPolicy != "" {
				if err = c.execNatGwQoS(gw, gw.Status.QoSPolicy, QoSDel); err != nil {
					klog.Errorf("failed to add qos for nat gw %s, %v", key, err)
					return err
				}
			}
			if gw.Spec.QoSPolicy != "" {
				if err = c.execNatGwQoS(gw, gw.Spec.QoSPolicy, QoSAdd); err != nil {
					klog.Errorf("failed to del qos for nat gw %s, %v", key, err)
					return err
				}
			}
			if err := c.updateCrdNatGwLabels(key, gw.Spec.QoSPolicy); err != nil {
				err := fmt.Errorf("failed to update nat gw %s: %v", gw.Name, err)
				klog.Error(err)
				return err
			}
			// if update qos success, will update nat gw status
			if err = c.patchNatGwQoSStatus(key, gw.Spec.QoSPolicy); err != nil {
				klog.Errorf("failed to patch nat gw qos status for nat gw %s, %v", key, err)
				return err
			}
		}
	}

	return nil
}

func (c *Controller) handleInitVpcNatGw(key string) error {
	gw, err := c.vpcNatGatewayLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	if vpcNatEnabled != "true" {
		return fmt.Errorf("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle init vpc nat gateway %s", key)

	// subnet for vpc-nat-gw has been checked when create vpc-nat-gw

	oriPod, err := c.getNatGwPod(key)
	if err != nil {
		err := fmt.Errorf("failed to get nat gw %s pod: %v", gw.Name, err)
		klog.Error(err)
		return err
	}
	pod := oriPod.DeepCopy()

	if pod.Status.Phase != corev1.PodRunning {
		time.Sleep(10 * time.Second)
		return fmt.Errorf("failed to init vpc nat gateway, pod is not ready")
	}

	if _, hasInit := pod.Annotations[util.VpcNatGatewayInitAnnotation]; hasInit {
		return nil
	}
	natGwCreatedAT = pod.CreationTimestamp.Format("2006-01-02T15:04:05")
	klog.V(3).Infof("nat gw pod '%s' inited at %s", key, natGwCreatedAT)
	if err = c.execNatGwRules(pod, natGwInit, []string{fmt.Sprintf("%s,%s", c.config.ServiceClusterIPRange, pod.Annotations[util.GatewayAnnotation])}); err != nil {
		err = fmt.Errorf("failed to init vpc nat gateway, %v", err)
		klog.Error(err)
		return err
	}

	if gw.Spec.QoSPolicy != "" {
		if err = c.execNatGwQoS(gw, gw.Spec.QoSPolicy, QoSAdd); err != nil {
			klog.Errorf("failed to add qos for nat gw %s, %v", key, err)
			return err
		}
	}
	// if update qos success, will update nat gw status
	if gw.Spec.QoSPolicy != gw.Status.QoSPolicy {
		if err = c.patchNatGwQoSStatus(key, gw.Spec.QoSPolicy); err != nil {
			klog.Errorf("failed to patch status for nat gw %s, %v", key, err)
			return err
		}
	}

	if err := c.updateCrdNatGwLabels(gw.Name, gw.Spec.QoSPolicy); err != nil {
		err := fmt.Errorf("failed to update nat gw %s: %v", gw.Name, err)
		klog.Error(err)
		return err
	}

	c.updateVpcFloatingIPQueue.Add(key)
	c.updateVpcDnatQueue.Add(key)
	c.updateVpcSnatQueue.Add(key)
	c.updateVpcSubnetQueue.Add(key)
	c.updateVpcEipQueue.Add(key)
	pod.Annotations[util.VpcNatGatewayInitAnnotation] = "true"
	patch, err := util.GenerateStrategicMergePatchPayload(oriPod, pod)
	if err != nil {
		klog.Error(err)
		return err
	}
	if _, err := c.config.KubeClient.CoreV1().Pods(pod.Namespace).Patch(context.Background(), pod.Name,
		types.StrategicMergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		err := fmt.Errorf("patch pod %s/%s failed %v", pod.Name, pod.Namespace, err)
		klog.Error(err)
		return err
	}
	return nil
}

func (c *Controller) handleUpdateVpcFloatingIP(natGwKey string) error {
	if vpcNatEnabled != "true" {
		return fmt.Errorf("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(natGwKey)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(natGwKey) }()
	klog.Infof("handle update vpc fip %s", natGwKey)

	// refresh exist fips
	if err := c.initCreateAt(natGwKey); err != nil {
		err = fmt.Errorf("failed to init nat gw pod '%s' create at, %v", natGwKey, err)
		klog.Error(err)
		return err
	}

	fips, err := c.iptablesFipsLister.List(labels.SelectorFromSet(labels.Set{util.VpcNatGatewayNameLabel: natGwKey}))
	if err != nil {
		err := fmt.Errorf("failed to get all fips, %v", err)
		klog.Error(err)
		return err
	}

	for _, fip := range fips {
		if fip.Status.Redo != natGwCreatedAT {
			klog.V(3).Infof("redo fip %s", fip.Name)
			if err = c.redoFip(fip.Name, natGwCreatedAT, false); err != nil {
				klog.Errorf("failed to update eip '%s' to re-apply, %v", fip.Spec.EIP, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) handleUpdateVpcEip(natGwKey string) error {
	if vpcNatEnabled != "true" {
		return fmt.Errorf("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(natGwKey)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(natGwKey) }()
	klog.Infof("handle update vpc eip %s", natGwKey)

	// refresh exist fips
	if err := c.initCreateAt(natGwKey); err != nil {
		err = fmt.Errorf("failed to init nat gw pod '%s' create at, %v", natGwKey, err)
		klog.Error(err)
		return err
	}
	eips, err := c.iptablesEipsLister.List(labels.Everything())
	if err != nil {
		err = fmt.Errorf("failed to get eip list, %v", err)
		klog.Error(err)
		return err
	}
	for _, eip := range eips {
		if eip.Spec.NatGwDp == natGwKey && eip.Status.Redo != natGwCreatedAT {
			klog.V(3).Infof("redo eip %s", eip.Name)
			if err = c.patchEipStatus(eip.Name, "", natGwCreatedAT, "", false); err != nil {
				klog.Errorf("failed to update eip '%s' to re-apply, %v", eip.Name, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) handleUpdateVpcSnat(natGwKey string) error {
	if vpcNatEnabled != "true" {
		return fmt.Errorf("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(natGwKey)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(natGwKey) }()
	klog.Infof("handle update vpc snat %s", natGwKey)

	// refresh exist snats
	if err := c.initCreateAt(natGwKey); err != nil {
		err = fmt.Errorf("failed to init nat gw pod '%s' create at, %v", natGwKey, err)
		klog.Error(err)
		return err
	}
	snats, err := c.iptablesSnatRulesLister.List(labels.SelectorFromSet(labels.Set{util.VpcNatGatewayNameLabel: natGwKey}))
	if err != nil {
		err = fmt.Errorf("failed to get all snats, %v", err)
		klog.Error(err)
		return err
	}
	for _, snat := range snats {
		if snat.Status.Redo != natGwCreatedAT {
			klog.V(3).Infof("redo snat %s", snat.Name)
			if err = c.redoSnat(snat.Name, natGwCreatedAT, false); err != nil {
				err = fmt.Errorf("failed to update eip '%s' to re-apply, %v", snat.Spec.EIP, err)
				klog.Error(err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) handleUpdateVpcDnat(natGwKey string) error {
	if vpcNatEnabled != "true" {
		return fmt.Errorf("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(natGwKey)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(natGwKey) }()
	klog.Infof("handle update vpc dnat %s", natGwKey)

	// refresh exist dnats
	if err := c.initCreateAt(natGwKey); err != nil {
		err = fmt.Errorf("failed to init nat gw pod '%s' create at, %v", natGwKey, err)
		klog.Error(err)
		return err
	}

	dnats, err := c.iptablesDnatRulesLister.List(labels.SelectorFromSet(labels.Set{util.VpcNatGatewayNameLabel: natGwKey}))
	if err != nil {
		err = fmt.Errorf("failed to get all dnats, %v", err)
		klog.Error(err)
		return err
	}
	for _, dnat := range dnats {
		if dnat.Status.Redo != natGwCreatedAT {
			klog.V(3).Infof("redo dnat %s", dnat.Name)
			if err = c.redoDnat(dnat.Name, natGwCreatedAT, false); err != nil {
				err := fmt.Errorf("failed to update dnat '%s' to redo, %v", dnat.Name, err)
				klog.Error(err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) getIptablesVersion(pod *corev1.Pod) (version string, err error) {
	operation := getIptablesVersion
	cmd := fmt.Sprintf("bash /kube-ovn/nat-gateway.sh %s", operation)
	klog.V(3).Infof(cmd)
	stdOutput, errOutput, err := util.ExecuteCommandInContainer(c.config.KubeClient, c.config.KubeRestConfig, pod.Namespace, pod.Name, "vpc-nat-gw", []string{"/bin/bash", "-c", cmd}...)
	if err != nil {
		if len(errOutput) > 0 {
			klog.Errorf("failed to ExecuteCommandInContainer, errOutput: %v", errOutput)
		}
		if len(stdOutput) > 0 {
			klog.V(3).Infof("failed to ExecuteCommandInContainer, stdOutput: %v", stdOutput)
		}
		klog.Error(err)
		return "", err
	}

	if len(stdOutput) > 0 {
		klog.V(3).Infof("ExecuteCommandInContainer stdOutput: %v", stdOutput)
	}

	if len(errOutput) > 0 {
		klog.Errorf("failed to ExecuteCommandInContainer errOutput: %v", errOutput)
		return "", err
	}

	versionMatcher := regexp.MustCompile(`v([0-9]+(\.[0-9]+)+)`)
	match := versionMatcher.FindStringSubmatch(stdOutput)
	if match == nil {
		return "", fmt.Errorf("no iptables version found in string: %s", stdOutput)
	}
	return match[1], nil
}

func (c *Controller) handleUpdateNatGwSubnetRoute(natGwKey string) error {
	gw, err := c.vpcNatGatewayLister.Get(natGwKey)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	if vpcNatEnabled != "true" {
		return fmt.Errorf("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(natGwKey)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(natGwKey) }()
	klog.Infof("handle update subnet route for nat gateway %s", natGwKey)

	oriPod, err := c.getNatGwPod(natGwKey)
	if err != nil {
		err = fmt.Errorf("failed to get nat gw '%s' pod, %v", natGwKey, err)
		klog.Error(err)
		return err
	}
	pod := oriPod.DeepCopy()
	var extRules []string
	var v4ExternalGw, v4InternalGw, v4ExternalCidr string
	externalNetwork := util.GetNatGwExternalNetwork(gw.Spec.ExternalSubnets)
	if subnet, ok := c.ipam.Subnets[externalNetwork]; ok {
		v4ExternalGw = subnet.V4Gw
		v4ExternalCidr = subnet.V4CIDR.String()
	} else {
		return fmt.Errorf("failed to get external subnet %s", externalNetwork)
	}
	extRules = append(extRules, fmt.Sprintf("%s,%s", v4ExternalCidr, v4ExternalGw))
	if err = c.execNatGwRules(pod, natGwExtSubnetRouteAdd, extRules); err != nil {
		err = fmt.Errorf("failed to exec nat gateway rule, err: %v", err)
		klog.Error(err)
		return err
	}

	if v4InternalGw, _, err = c.GetGwBySubnet(gw.Spec.Subnet); err != nil {
		err = fmt.Errorf("failed to get gw, err: %v", err)
		klog.Error(err)
		return err
	}
	vpc, err := c.vpcsLister.Get(gw.Spec.Vpc)
	if err != nil {
		err = fmt.Errorf("failed to get vpc, err: %v", err)
		klog.Error(err)
		return err
	}

	// update route table
	var newCIDRS, oldCIDRs, toBeDelCIDRs []string
	if len(vpc.Status.Subnets) > 0 {
		for _, s := range vpc.Status.Subnets {
			subnet, ok := c.ipam.Subnets[s]
			if !ok {
				err = fmt.Errorf("failed to get subnet, err: %v", err)
				klog.Error(err)
				return err
			}
			newCIDRS = append(newCIDRS, subnet.V4CIDR.String())
		}
	}
	if cidrs, ok := pod.Annotations[util.VpcCIDRsAnnotation]; ok {
		if err = json.Unmarshal([]byte(cidrs), &oldCIDRs); err != nil {
			klog.Error(err)
			return err
		}
	}
	for _, old := range oldCIDRs {
		if !slices.Contains(newCIDRS, old) {
			toBeDelCIDRs = append(toBeDelCIDRs, old)
		}
	}

	if len(newCIDRS) > 0 {
		var rules []string
		for _, cidr := range newCIDRS {
			if !util.CIDRContainIP(cidr, v4InternalGw) {
				rules = append(rules, fmt.Sprintf("%s,%s", cidr, v4InternalGw))
			}
		}
		if len(rules) > 0 {
			if err = c.execNatGwRules(pod, natGwSubnetRouteAdd, rules); err != nil {
				err = fmt.Errorf("failed to exec nat gateway rule, err: %v", err)
				klog.Error(err)
				return err
			}
		}
	}

	if len(toBeDelCIDRs) > 0 {
		for _, cidr := range toBeDelCIDRs {
			if err = c.execNatGwRules(pod, natGwSubnetRouteDel, []string{cidr}); err != nil {
				err = fmt.Errorf("failed to exec nat gateway rule, err: %v", err)
				klog.Error(err)
				return err
			}
		}
	}

	cidrBytes, err := json.Marshal(newCIDRS)
	if err != nil {
		klog.Errorf("marshal eip annotation failed %v", err)
		return err
	}
	pod.Annotations[util.VpcCIDRsAnnotation] = string(cidrBytes)
	patch, err := util.GenerateStrategicMergePatchPayload(oriPod, pod)
	if err != nil {
		klog.Error(err)
		return err
	}
	if _, err := c.config.KubeClient.CoreV1().Pods(pod.Namespace).Patch(context.Background(), pod.Name,
		types.StrategicMergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		err = fmt.Errorf("patch pod %s/%s failed %v", pod.Name, pod.Namespace, err)
		klog.Error(err)
		return err
	}

	return nil
}

func (c *Controller) execNatGwRules(pod *corev1.Pod, operation string, rules []string) error {
	cmd := fmt.Sprintf("bash /kube-ovn/nat-gateway.sh %s %s", operation, strings.Join(rules, " "))
	klog.V(3).Infof(cmd)
	stdOutput, errOutput, err := util.ExecuteCommandInContainer(c.config.KubeClient, c.config.KubeRestConfig, pod.Namespace, pod.Name, "vpc-nat-gw", []string{"/bin/bash", "-c", cmd}...)
	if err != nil {
		if len(errOutput) > 0 {
			klog.Errorf("failed to ExecuteCommandInContainer, errOutput: %v", errOutput)
		}
		if len(stdOutput) > 0 {
			klog.V(3).Infof("failed to ExecuteCommandInContainer, stdOutput: %v", stdOutput)
		}
		klog.Error(err)
		return err
	}

	if len(stdOutput) > 0 {
		klog.V(3).Infof("ExecuteCommandInContainer stdOutput: %v", stdOutput)
	}

	if len(errOutput) > 0 {
		klog.Errorf("failed to ExecuteCommandInContainer errOutput: %v", errOutput)
		return errors.New(errOutput)
	}
	return nil
}

func (c *Controller) genNatGwStatefulSet(gw *kubeovnv1.VpcNatGateway, oldSts *v1.StatefulSet) (newSts *v1.StatefulSet) {
	replicas := int32(1)
	name := util.GenNatGwStsName(gw.Name)
	allowPrivilegeEscalation := true
	privileged := true
	labels := map[string]string{
		"app":                   name,
		util.VpcNatGatewayLabel: "true",
	}
	newPodAnnotations := map[string]string{}
	if oldSts != nil && len(oldSts.Annotations) != 0 {
		newPodAnnotations = oldSts.Annotations
	}
	externalNetwork := util.GetNatGwExternalNetwork(gw.Spec.ExternalSubnets)
	podAnnotations := map[string]string{
		util.VpcNatGatewayAnnotation:     gw.Name,
		util.AttachmentNetworkAnnotation: fmt.Sprintf("%s/%s", c.config.PodNamespace, externalNetwork),
		util.LogicalSwitchAnnotation:     gw.Spec.Subnet,
		util.IPAddressAnnotation:         gw.Spec.LanIP,
	}
	for key, value := range podAnnotations {
		newPodAnnotations[key] = value
	}

	selectors := make(map[string]string)
	for _, v := range gw.Spec.Selector {
		parts := strings.Split(strings.TrimSpace(v), ":")
		if len(parts) != 2 {
			continue
		}
		selectors[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	klog.V(3).Infof("prepare for vpc nat gateway pod, node selector: %v", selectors)
	v4SubnetGw, _, _ := c.GetGwBySubnet(gw.Spec.Subnet)
	newSts = &v1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: v1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: newPodAnnotations,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "vpc-nat-gw",
							Image:           vpcNatImage,
							Command:         []string{"bash"},
							Args:            []string{"-c", "while true; do sleep 10000; done"},
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: &corev1.SecurityContext{
								Privileged:               &privileged,
								AllowPrivilegeEscalation: &allowPrivilegeEscalation,
							},
						},
					},
					InitContainers: []corev1.Container{
						{
							Name:            "vpc-nat-gw-init",
							Image:           vpcNatImage,
							Command:         []string{"bash"},
							Args:            []string{"-c", fmt.Sprintf("bash /kube-ovn/nat-gateway.sh init %s,%s", c.config.ServiceClusterIPRange, v4SubnetGw)},
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: &corev1.SecurityContext{
								Privileged:               &privileged,
								AllowPrivilegeEscalation: &allowPrivilegeEscalation,
							},
						},
					},
					NodeSelector: selectors,
					Tolerations:  gw.Spec.Tolerations,
					Affinity:     &gw.Spec.Affinity,
				},
			},
			UpdateStrategy: v1.StatefulSetUpdateStrategy{
				Type: v1.RollingUpdateStatefulSetStrategyType,
			},
		},
	}
	return
}

func (c *Controller) cleanUpVpcNatGw() error {
	gws, err := c.vpcNatGatewayLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to get vpc nat gateway, %v", err)
		return err
	}
	for _, gw := range gws {
		c.delVpcNatGatewayQueue.Add(gw.Name)
	}
	return nil
}

func (c *Controller) getNatGwPod(name string) (*corev1.Pod, error) {
	sel, _ := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{"app": util.GenNatGwStsName(name), util.VpcNatGatewayLabel: "true"},
	})

	pods, err := c.podsLister.Pods(c.config.PodNamespace).List(sel)

	switch {
	case err != nil:
		klog.Error(err)
		return nil, err
	case len(pods) == 0:
		return nil, k8serrors.NewNotFound(v1.Resource("pod"), name)
	case len(pods) != 1:
		time.Sleep(5 * time.Second)
		return nil, fmt.Errorf("too many pod")
	case pods[0].Status.Phase != "Running":
		time.Sleep(5 * time.Second)
		return nil, fmt.Errorf("pod is not active now")
	}

	return pods[0], nil
}

func (c *Controller) initCreateAt(key string) (err error) {
	if natGwCreatedAT != "" {
		return nil
	}
	pod, err := c.getNatGwPod(key)
	if err != nil {
		klog.Error(err)
		return err
	}
	natGwCreatedAT = pod.CreationTimestamp.Format("2006-01-02T15:04:05")
	return nil
}

func (c *Controller) updateCrdNatGwLabels(key, qos string) error {
	gw, err := c.vpcNatGatewayLister.Get(key)
	if err != nil {
		errMsg := fmt.Errorf("failed to get vpc nat gw '%s', %v", key, err)
		klog.Error(errMsg)
		return errMsg
	}
	var needUpdateLabel bool
	var op string
	// vpc nat gw label may lost
	if len(gw.Labels) == 0 {
		op = "add"
		gw.Labels = map[string]string{
			util.SubnetNameLabel: gw.Spec.Subnet,
			util.VpcNameLabel:    gw.Spec.Vpc,
			util.QoSLabel:        qos,
		}
		needUpdateLabel = true
	} else {
		if gw.Labels[util.SubnetNameLabel] != gw.Spec.Subnet {
			op = "replace"
			gw.Labels[util.SubnetNameLabel] = gw.Spec.Subnet
			needUpdateLabel = true
		}
		if gw.Labels[util.VpcNameLabel] != gw.Spec.Vpc {
			op = "replace"
			gw.Labels[util.VpcNameLabel] = gw.Spec.Vpc
			needUpdateLabel = true
		}
		if gw.Labels[util.QoSLabel] != qos {
			op = "replace"
			gw.Labels[util.QoSLabel] = qos
			needUpdateLabel = true
		}
	}
	if needUpdateLabel {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
		raw, _ := json.Marshal(gw.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().VpcNatGateways().Patch(context.Background(), gw.Name, types.JSONPatchType,
			[]byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to patch vpc nat gw %s: %v", gw.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchNatGwQoSStatus(key, qos string) error {
	// add qos label to vpc nat gw
	var changed bool
	oriGw, err := c.vpcNatGatewayLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get vpc nat gw %s, %v", key, err)
		return err
	}
	gw := oriGw.DeepCopy()

	// update status.qosPolicy
	if gw.Status.QoSPolicy != qos {
		gw.Status.QoSPolicy = qos
		changed = true
	}

	if changed {
		bytes, err := gw.Status.Bytes()
		if err != nil {
			klog.Errorf("failed to marshal vpc nat gw %s status, %v", gw.Name, err)
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().VpcNatGateways().Patch(context.Background(), gw.Name, types.MergePatchType,
			bytes, metav1.PatchOptions{}, "status"); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			klog.Errorf("failed to patch gw %s, %v", gw.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchNatGwStatus(key string) error {
	var changed bool
	oriGw, err := c.vpcNatGatewayLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get vpc nat gw %s, %v", key, err)
		return err
	}
	gw := oriGw.DeepCopy()

	if !reflect.DeepEqual(gw.Spec.ExternalSubnets, gw.Status.ExternalSubnets) {
		gw.Status.ExternalSubnets = gw.Spec.ExternalSubnets
		changed = true
	}
	if !reflect.DeepEqual(gw.Spec.Selector, gw.Status.Selector) {
		gw.Status.Selector = gw.Spec.Selector
		changed = true
	}
	if !reflect.DeepEqual(gw.Spec.Tolerations, gw.Status.Tolerations) {
		gw.Status.Tolerations = gw.Spec.Tolerations
		changed = true
	}
	if !reflect.DeepEqual(gw.Spec.Affinity, gw.Status.Affinity) {
		gw.Status.Affinity = gw.Spec.Affinity
		changed = true
	}

	if changed {
		bytes, err := gw.Status.Bytes()
		if err != nil {
			klog.Error(err)
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().VpcNatGateways().Patch(context.Background(), gw.Name, types.MergePatchType,
			bytes, metav1.PatchOptions{}, "status"); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			klog.Errorf("failed to patch gw %s, %v", gw.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) execNatGwQoS(gw *kubeovnv1.VpcNatGateway, qos, operation string) error {
	qosPolicy, err := c.qosPoliciesLister.Get(qos)
	if err != nil {
		klog.Errorf("get qos policy %s failed: %v", qos, err)
		return err
	}
	if !qosPolicy.Status.Shared {
		err := fmt.Errorf("not support unshared qos policy %s to related to gw", qos)
		klog.Error(err)
		return err
	}
	if qosPolicy.Status.BindingType != kubeovnv1.QoSBindingTypeNatGw {
		err := fmt.Errorf("not support qos policy %s binding type %s to related to gw", qos, qosPolicy.Status.BindingType)
		klog.Error(err)
		return err
	}
	return c.execNatGwBandtithLimitRules(gw, qosPolicy.Status.BandwidthLimitRules, operation)
}

func (c *Controller) execNatGwBandtithLimitRules(gw *kubeovnv1.VpcNatGateway, rules kubeovnv1.QoSPolicyBandwidthLimitRules, operation string) error {
	var err error
	for _, rule := range rules {
		if err = c.execNatGwQoSInPod(gw.Name, rule, operation); err != nil {
			klog.Errorf("failed to %s ingress gw '%s' qos in pod, %v", operation, gw.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) execNatGwQoSInPod(
	dp string, r *kubeovnv1.QoSPolicyBandwidthLimitRule, operation string,
) error {
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		klog.Errorf("failed to get nat gw pod, %v", err)
		return err
	}
	var addRules []string
	var classifierType, matchDirection, cidr string
	switch {
	case r.MatchType == "ip":
		classifierType = "u32"
		// matchValue: dst xxx.xxx.xxx.xxx/32
		splitStr := strings.Split(r.MatchValue, " ")
		if len(splitStr) != 2 {
			err := fmt.Errorf("matchValue %s format error", r.MatchValue)
			klog.Error(err)
			return err
		}
		matchDirection = splitStr[0]
		cidr = splitStr[1]
	case r.MatchType == "":
		classifierType = "matchall"
	default:
		err := fmt.Errorf("MatchType %s format error", r.MatchType)
		klog.Error(err)
		return err
	}
	rule := fmt.Sprintf("%s,%s,%d,%s,%s,%s,%s,%s,%s",
		r.Direction, r.Interface, r.Priority,
		classifierType, r.MatchType, matchDirection,
		cidr, r.RateMax, r.BurstMax)
	addRules = append(addRules, rule)

	if err = c.execNatGwRules(gwPod, operation, addRules); err != nil {
		err = fmt.Errorf("failed to exec nat gateway rule, err: %v", err)
		klog.Error(err)
		return err
	}
	return nil
}

func (c *Controller) initVpcNatGw() error {
	klog.Infof("init all vpc nat gateways")
	gws, err := c.vpcNatGatewayLister.List(labels.Everything())
	if err != nil {
		err = fmt.Errorf("failed to get vpc nat gw list, %v", err)
		klog.Error(err)
		return err
	}
	if len(gws) == 0 {
		return nil
	}

	if vpcNatEnabled != "true" {
		err := fmt.Errorf("iptables nat gw not enable")
		klog.Warning(err)
		return nil
	}

	for _, gw := range gws {
		pod, err := c.getNatGwPod(gw.Name)
		if err != nil {
			// the nat gw maybe deleted
			err := fmt.Errorf("failed to get nat gw %s pod: %v", gw.Name, err)
			klog.Error(err)
			continue
		}
		if vpcGwName, isVpcNatGw := pod.Annotations[util.VpcNatGatewayAnnotation]; isVpcNatGw {
			if _, hasInit := pod.Annotations[util.VpcNatGatewayInitAnnotation]; hasInit {
				return nil
			}
			c.initVpcNatGatewayQueue.Add(vpcGwName)
		}
	}
	return nil
}
