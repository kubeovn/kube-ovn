package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/cnf/structhash"
	netattachdef "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	vpcNatImage                     = ""
	vpcNatEnabled                   = "unknown"
	lastVpcNatCM  map[string]string = nil
)

const (
	NAT_GW_INIT             = "init"
	NAT_GW_FLOATING_IP_SYNC = "floating-ip-sync"
	NAT_GW_EIP_ADD          = "eip-add"
	NAT_GW_EIP_DEL          = "eip-del"
	NAT_GW_SNAT_SYNC        = "snat-sync"
	NAT_GW_DNAT_SYNC        = "dnat-sync"
	NAT_GW_SUBNET_ROUTE_ADD = "subnet-route-add"
	NAT_GW_SUBNET_ROUTE_DEL = "subnet-route-del"
)

func genNatGwDpName(name string) string {
	return fmt.Sprintf("vpc-nat-gw-%s", name)
}

func (c *Controller) resyncVpcNatGwConfig() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcNatGatewayConfig)
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to get ovn-vpc-nat-gw-config, %v", err)
		return
	}

	if k8serrors.IsNotFound(err) || cm.Data["enable-vpc-nat-gw"] == "false" || cm.Data["image"] == "" {
		if vpcNatEnabled == "false" {
			return
		}
		klog.Info("start to clean up vpc nat gateway")
		if err := c.cleanUpVpcNatGw(); err != nil {
			klog.Errorf("failed to clean up vpc nat gateway, %v", err)
			return
		}
		if err = c.gcVpcExternalNetwork(); err != nil {
			klog.Errorf("failed to gc vpc external network, %v", err)
			return
		}

		vpcNatEnabled = "false"
		lastVpcNatCM = nil
		klog.Info("finish clean up vpc nat gateway")
		return
	} else {
		if vpcNatEnabled == "true" && lastVpcNatCM != nil && reflect.DeepEqual(cm.Data, lastVpcNatCM) {
			return
		}

		if err = c.applyVpcExternalNetwork(cm.Data["nic"]); err != nil {
			klog.Errorf("failed to apply vpc external network, %v", err)
			return
		}

		gws, err := c.vpcNatGatewayLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to get vpc nat gateway, %v", err)
			return
		}
		vpcNatImage = cm.Data["image"]
		vpcNatEnabled = "true"
		lastVpcNatCM = cm.Data
		for _, gw := range gws {
			c.addOrUpdateVpcNatGatewayQueue.Add(gw.Name)
		}
		klog.Info("finish establishing vpc-nat-gateway")
		return
	}
}

func (c *Controller) enqueueAddVpcNatGw(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add vpc-nat-gw %s", key)
	c.addOrUpdateVpcNatGatewayQueue.Add(key)
}

func (c *Controller) enqueueUpdateVpcNatGw(old, new interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.addOrUpdateVpcNatGatewayQueue.Add(key)
}

func (c *Controller) enqueueDeleteVpcNatGw(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
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

func (c *Controller) runUpdateVpcEipWorker() {
	for c.processNextWorkItem("updateVpcEip", c.updateVpcEipQueue, c.handleUpdateVpcEips) {
	}
}

func (c *Controller) runUpdateVpcFloatingIpWorker() {
	for c.processNextWorkItem("updateVpcFloatingIp", c.updateVpcFloatingIpQueue, c.handleUpdateVpcFloatingIp) {
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
	c.vpcNatGwKeyMutex.Lock(key)
	defer c.vpcNatGwKeyMutex.Unlock(key)
	gw, err := c.vpcNatGatewayLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).Delete(context.Background(), genNatGwDpName(gw.Name), metav1.DeleteOptions{})
}

func (c *Controller) handleAddOrUpdateVpcNatGw(key string) error {
	c.vpcNatGwKeyMutex.Lock(key)
	defer c.vpcNatGwKeyMutex.Unlock(key)
	if vpcNatEnabled != "true" {
		return fmt.Errorf("failed to addOrUpdateVpcNatGw, vpcNatEnabled='%s'", vpcNatEnabled)
	}

	gw, err := c.vpcNatGatewayLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if _, err := c.vpcsLister.Get(gw.Spec.Vpc); err != nil {
		klog.Errorf("failed to get vpc %s, err: %v", gw.Spec.Vpc, err)
		return err
	}
	if _, err := c.subnetsLister.Get(gw.Spec.Subnet); err != nil {
		klog.Errorf("failed to get subnet %s, err: %v", gw.Spec.Subnet, err)
		return err
	}

	// check or create deployment
	needToCreate := false
	_, err = c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).
		Get(context.Background(), genNatGwDpName(gw.Name), metav1.GetOptions{})

	if err != nil {
		if k8serrors.IsNotFound(err) {
			needToCreate = true
		} else {
			return err
		}
	}

	newDp := c.genNatGwDeployment(gw)

	if needToCreate {
		_, err := c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).
			Create(context.Background(), newDp, metav1.CreateOptions{})

		if err != nil {
			klog.Errorf("failed to create deployment %s, err: %v", newDp.Name, err)
			return err
		}
		return nil
	} else {
		_, err := c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).
			Update(context.Background(), newDp, metav1.UpdateOptions{})

		if err != nil {
			klog.Errorf("failed to update deployment %s, err: %v", newDp.Name, err)
			return err
		}
	}

	pod, err := c.getNatGwPod(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if _, ok := pod.Annotations[util.VpcNatGatewayInitAnnotation]; ok {
		return c.syncVpcNatGwRules(key)
	}
	return nil
}

func (c *Controller) syncVpcNatGwRules(key string) error {
	pod, err := c.getNatGwPod(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if _, hasInit := pod.Annotations[util.VpcNatGatewayInitAnnotation]; !hasInit {
		c.initVpcNatGatewayQueue.Add(key)
		return nil
	}
	c.updateVpcEipQueue.Add(key)
	c.updateVpcFloatingIpQueue.Add(key)
	c.updateVpcDnatQueue.Add(key)
	c.updateVpcSnatQueue.Add(key)
	c.updateVpcSnatQueue.Add(key)
	c.updateVpcSubnetQueue.Add(key)
	return nil
}

func (c *Controller) handleInitVpcNatGw(key string) error {
	if vpcNatEnabled != "true" {
		return fmt.Errorf("failed init vpc nat gateway, vpcNatEnabled='%s'", vpcNatEnabled)
	}
	c.vpcNatGwKeyMutex.Lock(key)
	defer c.vpcNatGwKeyMutex.Unlock(key)
	_, err := c.vpcNatGatewayLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	pod, err := c.getNatGwPod(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if pod.Status.Phase != corev1.PodRunning {
		time.Sleep(5 * 1000)
		return fmt.Errorf("failed to init vpc nat gateway, pod is not ready.")
	}

	if _, hasInit := pod.Annotations[util.VpcNatGatewayInitAnnotation]; hasInit {
		return nil
	}
	if err = c.execNatGwRules(pod, NAT_GW_INIT, nil); err != nil {
		klog.Errorf("failed to init vpc nat gateway, err: %v", err)
		return err
	}
	pod.Annotations[util.VpcNatGatewayInitAnnotation] = "true"
	if _, err := c.config.KubeClient.CoreV1().Pods(pod.Namespace).Patch(context.Background(), pod.Name, types.JSONPatchType, generatePatchPayload(pod.Annotations, "replace"), metav1.PatchOptions{}, ""); err != nil {
		klog.Errorf("patch pod %s/%s failed %v", pod.Name, pod.Namespace, err)
		return err
	}
	return c.syncVpcNatGwRules(key)
}

func (c *Controller) handleUpdateVpcEips(natGwKey string) error {
	if vpcNatEnabled != "true" {
		return fmt.Errorf("failed to update vpc eips, vpcNatEnabled='%s'", vpcNatEnabled)
	}
	c.vpcNatGwKeyMutex.Lock(natGwKey)
	defer c.vpcNatGwKeyMutex.Unlock(natGwKey)
	gw, err := c.vpcNatGatewayLister.Get(natGwKey)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	pod, err := c.getNatGwPod(natGwKey)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	var toBeDelEips, oldEips []*kubeovnv1.Eip
	if eipAnnotation, ok := pod.Annotations[util.VpcEipsAnnotation]; ok {
		if err := json.Unmarshal([]byte(eipAnnotation), &oldEips); err != nil {
			klog.Errorf("%v", gw.Spec.Eips)
			return err
		}
	}

	for _, oldEip := range oldEips {
		toBeDel := true
		for _, newEip := range gw.Spec.Eips {
			if oldEip.EipCIDR == newEip.EipCIDR {
				toBeDel = false
				break
			}
		}
		if toBeDel {
			toBeDelEips = append(toBeDelEips, oldEip)
		}
	}

	if len(toBeDelEips) > 0 {
		var delRules []string
		for _, rule := range toBeDelEips {
			delRules = append(delRules, rule.EipCIDR)
		}
		if err = c.execNatGwRules(pod, NAT_GW_EIP_DEL, delRules); err != nil {
			klog.Errorf("failed to exec nat gateway rule, err: %v", err)
			return err
		}
	}

	if len(gw.Spec.Eips) > 0 {
		var addRules []string
		for _, rule := range gw.Spec.Eips {
			addRules = append(addRules, fmt.Sprintf("%s,%s", rule.EipCIDR, rule.Gateway))
		}
		if err = c.execNatGwRules(pod, NAT_GW_EIP_ADD, addRules); err != nil {
			return err
		}
	}

	eipBytes, err := json.Marshal(gw.Spec.Eips)
	if err != nil {
		klog.Errorf("marshal eip annotation failed %v", err)
		return err
	}
	pod.Annotations[util.VpcEipsAnnotation] = string(eipBytes)
	if _, err := c.config.KubeClient.CoreV1().Pods(pod.Namespace).Patch(context.Background(), pod.Name, types.JSONPatchType, generatePatchPayload(pod.Annotations, "replace"), metav1.PatchOptions{}, ""); err != nil {
		klog.Errorf("patch pod %s/%s failed %v", pod.Name, pod.Namespace, err)
		return err
	}
	return nil
}

func (c *Controller) handleUpdateVpcFloatingIp(natGwKey string) error {
	if vpcNatEnabled != "true" {
		return fmt.Errorf("failed to update vpc floatingIp, vpcNatEnabled='%s'", vpcNatEnabled)
	}
	c.vpcNatGwKeyMutex.Lock(natGwKey)
	defer c.vpcNatGwKeyMutex.Unlock(natGwKey)
	gw, err := c.vpcNatGatewayLister.Get(natGwKey)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	pod, err := c.getNatGwPod(natGwKey)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	// check md5
	newMd5 := fmt.Sprintf("%x", structhash.Md5(gw.Spec.FloatingIpRules, 1))
	oldMd5 := pod.Annotations[util.VpcFloatingIpMd5Annotation]
	if newMd5 == oldMd5 {
		return nil
	}

	// update rules
	var rules []string
	for _, rule := range gw.Spec.FloatingIpRules {
		rules = append(rules, fmt.Sprintf("%s,%s", rule.Eip, rule.InternalIp))
	}
	if err = c.execNatGwRules(pod, NAT_GW_FLOATING_IP_SYNC, rules); err != nil {
		klog.Errorf("failed to exec nat gateway rule, err: %v", err)
		return err
	}

	// update annotation
	pod.Annotations[util.VpcFloatingIpMd5Annotation] = newMd5
	if _, err := c.config.KubeClient.CoreV1().Pods(pod.Namespace).Patch(context.Background(), pod.Name, types.JSONPatchType, generatePatchPayload(pod.Annotations, "replace"), metav1.PatchOptions{}, ""); err != nil {
		klog.Errorf("patch pod %s/%s failed %v", pod.Name, pod.Namespace, err)
		return err
	}

	return nil
}

func (c *Controller) handleUpdateVpcSnat(natGwKey string) error {
	if vpcNatEnabled != "true" {
		return fmt.Errorf("failed to update vpc snat, vpcNatEnabled='%s'", vpcNatEnabled)
	}
	c.vpcNatGwKeyMutex.Lock(natGwKey)
	defer c.vpcNatGwKeyMutex.Unlock(natGwKey)
	gw, err := c.vpcNatGatewayLister.Get(natGwKey)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	pod, err := c.getNatGwPod(natGwKey)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	// check md5
	newMd5 := fmt.Sprintf("%x", structhash.Md5(gw.Spec.SnatRules, 1))
	oldMd5 := pod.Annotations[util.VpcSnatMd5Annotation]
	if newMd5 == oldMd5 {
		return nil
	}

	// update rules
	var rules []string
	for _, rule := range gw.Spec.SnatRules {
		rules = append(rules, fmt.Sprintf("%s,%s", rule.Eip, rule.InternalCIDR))
	}
	if err = c.execNatGwRules(pod, NAT_GW_SNAT_SYNC, rules); err != nil {
		klog.Errorf("failed to exec nat gateway rule, err: %v", err)
		return err
	}

	// update annotation
	pod.Annotations[util.VpcSnatMd5Annotation] = newMd5
	if _, err := c.config.KubeClient.CoreV1().Pods(pod.Namespace).Patch(context.Background(), pod.Name, types.JSONPatchType, generatePatchPayload(pod.Annotations, "replace"), metav1.PatchOptions{}, ""); err != nil {
		klog.Errorf("patch pod %s/%s failed %v", pod.Name, pod.Namespace, err)
		return err
	}
	return nil
}

func (c *Controller) handleUpdateVpcDnat(natGwKey string) error {
	if vpcNatEnabled != "true" {
		return fmt.Errorf("failed update vpc dnat, vpcNatEnabled='%s'", vpcNatEnabled)
	}
	c.vpcNatGwKeyMutex.Lock(natGwKey)
	defer c.vpcNatGwKeyMutex.Unlock(natGwKey)
	gw, err := c.vpcNatGatewayLister.Get(natGwKey)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	pod, err := c.getNatGwPod(natGwKey)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	// check md5
	newMd5 := fmt.Sprintf("%x", structhash.Md5(gw.Spec.DnatRules, 1))
	oldMd5 := pod.Annotations[util.VpcDnatMd5Annotation]
	if newMd5 == oldMd5 {
		return nil
	}

	// update rules
	var rules []string
	for _, rule := range gw.Spec.DnatRules {
		rules = append(rules, fmt.Sprintf("%s,%s,%s,%s,%s", rule.Eip, rule.ExternalPort, rule.Protocol, rule.InternalIp, rule.InternalPort))
	}
	if err = c.execNatGwRules(pod, NAT_GW_DNAT_SYNC, rules); err != nil {
		klog.Errorf("failed to exec nat gateway rule, err: %v", err)
		return err
	}

	// update annotation
	pod.Annotations[util.VpcDnatMd5Annotation] = newMd5
	if _, err := c.config.KubeClient.CoreV1().Pods(pod.Namespace).Patch(context.Background(), pod.Name, types.JSONPatchType, generatePatchPayload(pod.Annotations, "replace"), metav1.PatchOptions{}, ""); err != nil {
		klog.Errorf("patch pod %s/%s failed %v", pod.Name, pod.Namespace, err)
		return err
	}
	return nil
}

func (c *Controller) handleUpdateNatGwSubnetRoute(natGwKey string) error {
	if vpcNatEnabled != "true" {
		return fmt.Errorf("failed to update subnet route, vpcNatEnabled='%s'", vpcNatEnabled)
	}
	c.vpcNatGwKeyMutex.Lock(natGwKey)
	defer c.vpcNatGwKeyMutex.Unlock(natGwKey)
	gw, err := c.vpcNatGatewayLister.Get(natGwKey)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	pod, err := c.getNatGwPod(natGwKey)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	gwSubnet, err := c.subnetsLister.Get(gw.Spec.Subnet)
	if err != nil {
		klog.Errorf("failed to get subnet, err: %v", err)
		return err
	}
	vpc, err := c.vpcsLister.Get(gw.Spec.Vpc)
	if err != nil {
		klog.Errorf("failed to get vpc, err: %v", err)
		return err
	}

	// update route table
	var newCIDRS, oldCIDRs, toBeDelCIDRs []string
	if len(vpc.Status.Subnets) > 0 {
		for _, s := range vpc.Status.Subnets {
			subnet, err := c.subnetsLister.Get(s)
			if err != nil {
				klog.Errorf("failed to get subnet, err: %v", err)
				return err
			}
			newCIDRS = append(newCIDRS, subnet.Spec.CIDRBlock)
		}
	}
	if cidrs, ok := pod.Annotations[util.VpcCIDRsAnnotation]; ok {
		if err = json.Unmarshal([]byte(cidrs), &oldCIDRs); err != nil {
			return err
		}
	}
	for _, old := range oldCIDRs {
		if !util.ContainsString(newCIDRS, old) {
			toBeDelCIDRs = append(toBeDelCIDRs, old)
		}
	}

	if len(newCIDRS) > 0 {
		var rules []string
		for _, cidr := range newCIDRS {
			rules = append(rules, fmt.Sprintf("%s,%s", cidr, gwSubnet.Spec.Gateway))
		}
		if err = c.execNatGwRules(pod, NAT_GW_SUBNET_ROUTE_ADD, rules); err != nil {
			klog.Errorf("failed to exec nat gateway rule, err: %v", err)
			return err
		}
	}

	if len(toBeDelCIDRs) > 0 {
		for _, cidr := range toBeDelCIDRs {
			if err = c.execNatGwRules(pod, NAT_GW_SUBNET_ROUTE_DEL, []string{cidr}); err != nil {
				klog.Errorf("failed to exec nat gateway rule, err: %v", err)
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
	if _, err := c.config.KubeClient.CoreV1().Pods(pod.Namespace).Patch(context.Background(), pod.Name, types.JSONPatchType, generatePatchPayload(pod.Annotations, "replace"), metav1.PatchOptions{}, ""); err != nil {
		klog.Errorf("patch pod %s/%s failed %v", pod.Name, pod.Namespace, err)
		return err
	}

	return nil
}

func (c *Controller) execNatGwRules(pod *corev1.Pod, operation string, rules []string) error {
	stdOutput, errOutput, err := util.ExecuteCommandInContainer(c.config.KubeClient, c.config.KubeRestConfig, pod.Namespace, pod.Name, "vpc-nat-gw",
		[]string{"/bin/bash", "-c", fmt.Sprintf("bash /kube-ovn/nat-gateway.sh %s %s", operation, strings.Join(rules, " "))}...)

	if err != nil {
		if len(errOutput) > 0 {
			klog.Errorf("failed to ExecuteCommandInContainer, errOutput: %v", errOutput)
		}
		if len(stdOutput) > 0 {
			klog.Infof("failed to ExecuteCommandInContainer, stdOutput: %v", stdOutput)
		}
		return err
	}

	if len(stdOutput) > 0 {
		klog.Infof("ExecuteCommandInContainer stdOutput: %v", stdOutput)
	}

	if len(errOutput) > 0 {
		klog.Errorf("failed to ExecuteCommandInContainer errOutput: %v", errOutput)
		return errors.New(errOutput)
	}
	return nil
}

func (c *Controller) genNatGwDeployment(gw *kubeovnv1.VpcNatGateway) (dp *v1.Deployment) {
	replicas := int32(1)
	name := genNatGwDpName(gw.Name)
	allowPrivilegeEscalation := true
	privileged := true
	labels := map[string]string{
		"app":                   name,
		util.VpcNatGatewayLabel: "true",
	}

	podAnnotations := map[string]string{
		util.VpcNatGatewayAnnotation:     gw.Name,
		util.AttachmentNetworkAnnotation: fmt.Sprintf("%s/%s", c.config.PodNamespace, util.VpcExternalNet),
		util.LogicalSwitchAnnotation:     gw.Spec.Subnet,
		util.IpAddressAnnotation:         gw.Spec.LanIp,
	}

	dp = &v1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: podAnnotations,
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
				},
			},
			Strategy: v1.DeploymentStrategy{
				Type: v1.RecreateDeploymentStrategyType,
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
		MatchLabels: map[string]string{"app": genNatGwDpName(name), util.VpcNatGatewayLabel: "true"},
	})

	pods, err := c.podsLister.Pods(c.config.PodNamespace).List(sel)
	if err != nil {
		return nil, err
	} else if len(pods) != 1 {
		return nil, fmt.Errorf("too many pod.")
	} else if pods[0].Status.Phase != "Running" {
		return nil, fmt.Errorf("pod is not active now")
	}

	return pods[0], nil
}

func (c *Controller) gcVpcExternalNetwork() (err error) {
	networkClient := c.config.AttachNetClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(c.config.PodNamespace)
	_, err = networkClient.Get(context.Background(), util.VpcExternalNet, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		if k8serrors.IsForbidden(err) {
			klog.Warningf("failed to get net-attach-def, %v", err)
			return nil
		}
		return err
	}
	err = networkClient.Delete(context.Background(), util.VpcExternalNet, metav1.DeleteOptions{})
	if k8serrors.IsForbidden(err) {
		klog.Warningf("failed to delete net-attach-def, %v", err)
		return nil
	}
	return
}

func (c *Controller) applyVpcExternalNetwork(nic string) (err error) {
	cfgTmpl := "{\"cniVersion\": \"0.3.0\",\"type\": \"macvlan\",\"master\": \"%NIC%\",\"mode\": \"bridge\"}"
	netCfg := strings.ReplaceAll(cfgTmpl, "%NIC%", nic)

	networkClient := c.config.AttachNetClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(c.config.PodNamespace)
	network, err := networkClient.Get(context.Background(), util.VpcExternalNet, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			network := &netattachdef.NetworkAttachmentDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Name:      util.VpcExternalNet,
					Namespace: c.config.PodNamespace,
				},
				Spec: netattachdef.NetworkAttachmentDefinitionSpec{Config: netCfg},
			}
			_, err = networkClient.Create(context.Background(), network, metav1.CreateOptions{})
		} else {
			return err
		}
	} else {
		network.Spec = netattachdef.NetworkAttachmentDefinitionSpec{Config: netCfg}
		_, err = networkClient.Update(context.Background(), network, metav1.UpdateOptions{})
	}
	return err
}
