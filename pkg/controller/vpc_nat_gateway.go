package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
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
	vpcNatImage     = ""
	vpcNatEnabled   = "unknown"
	VpcNatCmVersion = ""
	createAt        = ""
)

const (
	natGwInit              = "init"
	natGwEipAdd            = "eip-add"
	natGwEipDel            = "eip-del"
	natGwDnatAdd           = "dnat-add"
	natGwDnatDel           = "dnat-del"
	natGwSnatAdd           = "snat-add"
	natGwSnatDel           = "snat-del"
	natGwSubnetFipAdd      = "floating-ip-add"
	natGwSubnetFipDel      = "floating-ip-del"
	natGwSubnetRouteAdd    = "subnet-route-add"
	natGwSubnetRouteDel    = "subnet-route-del"
	natGwExtSubnetRouteAdd = "ext-subnet-route-add"
	natGwExtSubnetRouteDel = "ext-subnet-route-del"
)

func genNatGwStsName(name string) string {
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
		vpcNatEnabled = "false"
		VpcNatCmVersion = ""
		klog.Info("finish clean up vpc nat gateway")
		return
	} else {
		if vpcNatEnabled == "true" && VpcNatCmVersion == cm.ResourceVersion {
			return
		}

		klog.Info("start establish vpc-nat-gateway")
		if err = c.checkVpcExternalNet(); err != nil {
			klog.Errorf("failed to check vpc external net, %v", err)
			return
		}

		gws, err := c.vpcNatGatewayLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to get vpc nat gateway, %v", err)
			return
		}
		vpcNatImage = cm.Data["image"]
		vpcNatEnabled = "true"
		VpcNatCmVersion = cm.ResourceVersion
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

func (c *Controller) runUpdateVpcFloatingIpWorker() {
	for c.processNextWorkItem("updateVpcFloatingIp", c.updateVpcFloatingIpQueue, c.handleUpdateVpcFloatingIp) {
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
	c.vpcNatGwKeyMutex.Lock(key)
	defer c.vpcNatGwKeyMutex.Unlock(key)
	name := genNatGwStsName(key)
	klog.Infof("delete vpc nat gw %s", name)
	err := c.config.KubeClient.AppsV1().StatefulSets(c.config.PodNamespace).Delete(context.Background(), name, metav1.DeleteOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

func (c *Controller) handleAddOrUpdateVpcNatGw(key string) error {
	// create nat gw statefulset
	c.vpcNatGwKeyMutex.Lock(key)
	defer c.vpcNatGwKeyMutex.Unlock(key)
	if vpcNatEnabled != "true" {
		// wait and check again
		time.Sleep(10 * time.Second)
		if vpcNatEnabled != "true" {
			return fmt.Errorf("failed to addOrUpdateVpcNatGw, vpcNatEnabled='%s'", vpcNatEnabled)
		}
	}
	gw, err := c.vpcNatGatewayLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if _, err := c.vpcsLister.Get(gw.Spec.Vpc); err != nil {
		klog.Errorf("failed to get vpc '%s', err: %v", gw.Spec.Vpc, err)
		return err
	}
	if _, err := c.subnetsLister.Get(gw.Spec.Subnet); err != nil {
		klog.Errorf("failed to get subnet '%s', err: %v", gw.Spec.Subnet, err)
		return err
	}

	// check or create statefulset
	needToCreate := false
	oldSts, err := c.config.KubeClient.AppsV1().StatefulSets(c.config.PodNamespace).
		Get(context.Background(), genNatGwStsName(gw.Name), metav1.GetOptions{})

	if err != nil {
		if k8serrors.IsNotFound(err) {
			needToCreate = true
		} else {
			return err
		}
	}

	newSts := c.genNatGwStatefulSet(gw, oldSts.DeepCopy())

	if needToCreate {
		_, err := c.config.KubeClient.AppsV1().StatefulSets(c.config.PodNamespace).
			Create(context.Background(), newSts, metav1.CreateOptions{})

		if err != nil {
			klog.Errorf("failed to create statefulset '%s', err: %v", newSts.Name, err)
			return err
		}
		return nil
	} else {
		_, err := c.config.KubeClient.AppsV1().StatefulSets(c.config.PodNamespace).
			Update(context.Background(), newSts, metav1.UpdateOptions{})

		if err != nil {
			klog.Errorf("failed to update statefulset '%s', err: %v", newSts.Name, err)
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
	// sync all nat crd
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
	c.updateVpcFloatingIpQueue.Add(key)
	c.updateVpcDnatQueue.Add(key)
	c.updateVpcSnatQueue.Add(key)
	c.updateVpcSubnetQueue.Add(key)
	c.updateVpcEipQueue.Add(key)
	return nil
}

func (c *Controller) handleInitVpcNatGw(key string) error {
	if vpcNatEnabled != "true" {
		time.Sleep(10 * time.Second)
		if vpcNatEnabled != "true" {
			return fmt.Errorf("failed init vpc nat gateway, vpcNatEnabled='%s'", vpcNatEnabled)
		}
	}
	c.vpcNatGwKeyMutex.Lock(key)
	defer c.vpcNatGwKeyMutex.Unlock(key)
	gw, err := c.vpcNatGatewayLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	subnet, err := c.subnetsLister.Get(gw.Spec.Subnet)
	if err != nil {
		klog.Errorf("failed to get subnet '%s', %v", gw.Spec.Subnet, err)
		return fmt.Errorf("failed to initialize vpc nat gateway '%s', %v", key, err)
	}

	oriPod, err := c.getNatGwPod(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	pod := oriPod.DeepCopy()

	if pod.Status.Phase != corev1.PodRunning {
		time.Sleep(10 * 1000)
		return fmt.Errorf("failed to init vpc nat gateway, pod is not ready")
	}

	if _, hasInit := pod.Annotations[util.VpcNatGatewayInitAnnotation]; hasInit {
		return nil
	}
	createAt = pod.CreationTimestamp.Format("2006-01-02T15:04:05")
	klog.V(3).Infof("nat gw pod '%s' inited at %s", key, createAt)
	if err = c.execNatGwRules(pod, natGwInit, []string{subnet.Spec.CIDRBlock}); err != nil {
		klog.Errorf("failed to init vpc nat gateway, %v", err)
		return err
	}
	pod.Annotations[util.VpcNatGatewayInitAnnotation] = "true"
	patch, err := util.GenerateStrategicMergePatchPayload(oriPod, pod)
	if err != nil {
		return err
	}
	if _, err := c.config.KubeClient.CoreV1().Pods(pod.Namespace).Patch(context.Background(), pod.Name,
		types.StrategicMergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		klog.Errorf("patch pod %s/%s failed %v", pod.Name, pod.Namespace, err)
		return err
	}
	return c.syncVpcNatGwRules(key)
}

func (c *Controller) handleUpdateVpcFloatingIp(natGwKey string) error {
	if vpcNatEnabled != "true" {
		time.Sleep(10 * time.Second)
		if vpcNatEnabled != "true" {
			return fmt.Errorf("failed to update vpc floatingIp, vpcNatEnabled='%s'", vpcNatEnabled)
		}
	}
	c.vpcNatGwKeyMutex.Lock(natGwKey)
	defer c.vpcNatGwKeyMutex.Unlock(natGwKey)
	// refresh exist fips
	if err := c.initCreateAt(natGwKey); err != nil {
		klog.Errorf("failed to init nat gw pod '%s' create at, %v", natGwKey, err)
	}

	fips, err := c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().List(context.Background(), metav1.ListOptions{
		LabelSelector: fields.OneTermEqualSelector(util.VpcNatGatewayNameLabel, natGwKey).String(),
	})

	if err != nil {
		klog.Errorf("failed to get all fips, %v", err)
		return err
	}

	for _, fip := range fips.Items {
		if fip.Status.Redo != createAt {
			klog.V(3).Infof("redo fip %s", fip.Name)
			if err = c.redoFip(fip.Name, createAt, false); err != nil {
				klog.Errorf("failed to update eip '%s' to make sure applied, %v", fip.Spec.EIP, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) handleUpdateVpcEip(natGwKey string) error {
	if vpcNatEnabled != "true" {
		time.Sleep(10 * time.Second)
		if vpcNatEnabled != "true" {
			return fmt.Errorf("failed to update vpc eip, vpcNatEnabled='%s'", vpcNatEnabled)
		}
	}
	c.vpcNatGwKeyMutex.Lock(natGwKey)
	defer c.vpcNatGwKeyMutex.Unlock(natGwKey)
	// refresh exist fips
	if err := c.initCreateAt(natGwKey); err != nil {
		klog.Errorf("failed to init nat gw pod '%s' create at, %v", natGwKey, err)
	}
	eips, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().List(context.Background(), metav1.ListOptions{
		LabelSelector: fields.OneTermEqualSelector(util.VpcNatLabel, "").String(),
	})
	if err != nil {
		klog.Errorf("failed to get not used eips, %v", err)
		return err
	}
	for _, eip := range eips.Items {
		if eip.Spec.NatGwDp == natGwKey && eip.Status.Redo != createAt {
			klog.V(3).Infof("redo eip %s", eip.Name)
			if err = c.patchEipStatus(eip.Name, "", createAt, "", false); err != nil {
				klog.Errorf("failed to update eip '%s' to make sure applied, %v", eip.Name, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) handleUpdateVpcSnat(natGwKey string) error {
	if vpcNatEnabled != "true" {
		time.Sleep(10 * time.Second)
		if vpcNatEnabled != "true" {
			return fmt.Errorf("failed to update vpc snat, vpcNatEnabled='%s'", vpcNatEnabled)
		}
	}
	c.vpcNatGwKeyMutex.Lock(natGwKey)
	defer c.vpcNatGwKeyMutex.Unlock(natGwKey)
	// refresh exist snats
	if err := c.initCreateAt(natGwKey); err != nil {
		klog.Errorf("failed to init nat gw pod '%s' create at, %v", natGwKey, err)
	}
	snats, err := c.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().List(context.Background(), metav1.ListOptions{
		LabelSelector: fields.OneTermEqualSelector(util.VpcNatGatewayNameLabel, natGwKey).String(),
	})
	if err != nil {
		klog.Errorf("failed to get all snats, %v", err)
		return err
	}
	for _, snat := range snats.Items {
		if snat.Status.Redo != createAt {
			klog.V(3).Infof("redo snat %s", snat.Name)
			if err = c.redoSnat(snat.Name, createAt, false); err != nil {
				klog.Errorf("failed to update eip '%s' to make sure applied, %v", snat.Spec.EIP, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) handleUpdateVpcDnat(natGwKey string) error {
	if vpcNatEnabled != "true" {
		time.Sleep(10 * time.Second)
		if vpcNatEnabled != "true" {
			return fmt.Errorf("failed update vpc dnat, vpcNatEnabled='%s'", vpcNatEnabled)
		}
	}
	c.vpcNatGwKeyMutex.Lock(natGwKey)
	defer c.vpcNatGwKeyMutex.Unlock(natGwKey)
	// refresh exist dnats
	if err := c.initCreateAt(natGwKey); err != nil {
		klog.Errorf("failed to init nat gw pod '%s' create at, %v", natGwKey, err)
	}

	dnats, err := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().List(context.Background(), metav1.ListOptions{
		LabelSelector: fields.OneTermEqualSelector(util.VpcNatGatewayNameLabel, natGwKey).String(),
	})
	if err != nil {
		klog.Errorf("failed to get all dnats, %v", err)
		return err
	}
	for _, dnat := range dnats.Items {
		if dnat.Status.Redo != createAt {
			klog.V(3).Infof("redo dnat %s", dnat.Name)
			if err = c.redoDnat(dnat.Name, createAt, false); err != nil {
				klog.Errorf("failed to update dnat '%s' to make sure applied, %v", dnat.Name, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) handleUpdateNatGwSubnetRoute(natGwKey string) error {
	if vpcNatEnabled != "true" {
		time.Sleep(10 * time.Second)
		if vpcNatEnabled != "true" {
			return fmt.Errorf("failed to update subnet route, vpcNatEnabled='%s'", vpcNatEnabled)
		}
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

	oriPod, err := c.getNatGwPod(natGwKey)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	pod := oriPod.DeepCopy()
	extSubnet, err := c.subnetsLister.Get(util.VpcExternalNet)
	if err != nil {
		klog.Errorf("failed to get ovn-vpc-external-network subnet, err: %v", err)
		return err
	}
	var extRules []string
	if extSubnet.Spec.CIDRBlock != "" && extSubnet.Spec.Gateway != "" {
		extRules = append(extRules, fmt.Sprintf("%s,%s", extSubnet.Spec.CIDRBlock, extSubnet.Spec.Gateway))
		if err = c.execNatGwRules(pod, natGwExtSubnetRouteAdd, extRules); err != nil {
			klog.Errorf("failed to exec nat gateway rule, err: %v", err)
			return err
		}
	} else {
		err = fmt.Errorf("failed to get external subnet cidr and gw")
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
			if !util.CIDRContainIP(cidr, gwSubnet.Spec.Gateway) {
				rules = append(rules, fmt.Sprintf("%s,%s", cidr, gwSubnet.Spec.Gateway))
			}
		}
		if len(rules) > 0 {
			if err = c.execNatGwRules(pod, natGwSubnetRouteAdd, rules); err != nil {
				klog.Errorf("failed to exec nat gateway rule, err: %v", err)
				return err
			}
		}
	}

	if len(toBeDelCIDRs) > 0 {
		for _, cidr := range toBeDelCIDRs {
			if err = c.execNatGwRules(pod, natGwSubnetRouteDel, []string{cidr}); err != nil {
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
	patch, err := util.GenerateStrategicMergePatchPayload(oriPod, pod)
	if err != nil {
		return err
	}
	if _, err := c.config.KubeClient.CoreV1().Pods(pod.Namespace).Patch(context.Background(), pod.Name,
		types.StrategicMergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		klog.Errorf("patch pod %s/%s failed %v", pod.Name, pod.Namespace, err)
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
	name := genNatGwStsName(gw.Name)
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

	podAnnotations := map[string]string{
		util.VpcNatGatewayAnnotation:     gw.Name,
		util.AttachmentNetworkAnnotation: fmt.Sprintf("%s/%s", c.config.PodNamespace, util.VpcExternalNet),
		util.LogicalSwitchAnnotation:     gw.Spec.Subnet,
		util.IpAddressAnnotation:         gw.Spec.LanIp,
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

	newSts = &v1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
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
					NodeSelector: selectors,
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
		MatchLabels: map[string]string{"app": genNatGwStsName(name), util.VpcNatGatewayLabel: "true"},
	})

	pods, err := c.podsLister.Pods(c.config.PodNamespace).List(sel)
	if err != nil {
		return nil, err
	} else if len(pods) == 0 {
		time.Sleep(2 * time.Second)
		return nil, fmt.Errorf("pod '%s' not exist", name)
	} else if len(pods) != 1 {
		time.Sleep(5 * time.Second)
		return nil, fmt.Errorf("too many pod")
	} else if pods[0].Status.Phase != "Running" {
		time.Sleep(5 * time.Second)
		return nil, fmt.Errorf("pod is not active now")
	}

	return pods[0], nil
}

func (c *Controller) checkVpcExternalNet() (err error) {
	networkClient := c.config.AttachNetClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(c.config.PodNamespace)
	_, err = networkClient.Get(context.Background(), util.VpcExternalNet, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Errorf("vpc external multus net '%s' should be exist already before ovn-vpc-nat-gw-config applied", util.VpcExternalNet)
		}
	}
	return err
}

func (c *Controller) initCreateAt(key string) (err error) {
	if createAt != "" {
		return nil
	}
	pod, err := c.getNatGwPod(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	createAt = pod.CreationTimestamp.Format("2006-01-02T15:04:05")
	return nil
}
