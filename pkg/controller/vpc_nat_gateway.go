package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
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
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	vpcNatEnabled   = "unknown"
	VpcNatCmVersion = ""
	natGwCreatedAT  = ""
)

const (
	natGwInit             = "init"
	natGwEipAdd           = "eip-add"
	natGwEipDel           = "eip-del"
	natGwDnatAdd          = "dnat-add"
	natGwDnatDel          = "dnat-del"
	natGwSnatAdd          = "snat-add"
	natGwSnatDel          = "snat-del"
	natGwEipIngressQoSAdd = "eip-ingress-qos-add"
	natGwEipIngressQoSDel = "eip-ingress-qos-del"
	QoSAdd                = "qos-add"
	QoSDel                = "qos-del"
	natGwEipEgressQoSAdd  = "eip-egress-qos-add"
	natGwEipEgressQoSDel  = "eip-egress-qos-del"
	natGwSubnetFipAdd     = "floating-ip-add"
	natGwSubnetFipDel     = "floating-ip-del"
	natGwSubnetRouteAdd   = "subnet-route-add"
	natGwSubnetRouteDel   = "subnet-route-del"

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
		return errors.New("iptables nat gw not enable")
	}

	if _, err := c.vpcsLister.Get(gw.Spec.Vpc); err != nil {
		err = fmt.Errorf("failed to get vpc '%s', err: %w", gw.Spec.Vpc, err)
		klog.Error(err)
		return err
	}
	if _, err := c.subnetsLister.Get(gw.Spec.Subnet); err != nil {
		err = fmt.Errorf("failed to get subnet '%s', err: %w", gw.Spec.Subnet, err)
		klog.Error(err)
		return err
	}

	// check or create statefulset
	needToCreate := false
	needToUpdate := false
	oldSts, err := c.config.KubeClient.AppsV1().StatefulSets(c.config.PodNamespace).
		Get(context.Background(), util.GenNatGwStsName(gw.Name), metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Error(err)
			return err
		}
		needToCreate, oldSts = true, nil
	}
	newSts, err := c.genNatGwStatefulSet(gw, oldSts)
	if err != nil {
		klog.Error(err)
		return err
	}
	if !needToCreate && isVpcNatGwChanged(gw) {
		needToUpdate = true
	}

	switch {
	case needToCreate:
		// if pod create successfully, will add initVpcNatGatewayQueue
		if _, err := c.config.KubeClient.AppsV1().StatefulSets(c.config.PodNamespace).
			Create(context.Background(), newSts, metav1.CreateOptions{}); err != nil {
			err := fmt.Errorf("failed to create statefulset '%s', err: %w", newSts.Name, err)
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
			err := fmt.Errorf("failed to update statefulset '%s', err: %w", newSts.Name, err)
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
				err := fmt.Errorf("failed to update nat gw %s: %w", gw.Name, err)
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
		return errors.New("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()
	klog.Infof("handle init vpc nat gateway %s", key)

	// subnet for vpc-nat-gw has been checked when create vpc-nat-gw

	oriPod, err := c.getNatGwPod(key)
	if err != nil {
		err := fmt.Errorf("failed to get nat gw %s pod: %w", gw.Name, err)
		klog.Error(err)
		return err
	}
	pod := oriPod.DeepCopy()

	if pod.Status.Phase != corev1.PodRunning {
		time.Sleep(10 * time.Second)
		err = fmt.Errorf("failed to init vpc nat gateway %s, pod is not ready", key)
		klog.Error(err)
		return err
	}

	if _, hasInit := pod.Annotations[util.VpcNatGatewayInitAnnotation]; hasInit {
		return nil
	}
	natGwCreatedAT = pod.CreationTimestamp.Format("2006-01-02T15:04:05")
	klog.V(3).Infof("nat gw pod '%s' inited at %s", key, natGwCreatedAT)
	if err = c.execNatGwRules(pod, natGwInit, nil); err != nil {
		err = fmt.Errorf("failed to init vpc nat gateway, %w", err)
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
		err := fmt.Errorf("failed to update nat gw %s: %w", gw.Name, err)
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
		err := fmt.Errorf("patch pod %s/%s failed %w", pod.Name, pod.Namespace, err)
		klog.Error(err)
		return err
	}
	return nil
}

func (c *Controller) handleUpdateVpcFloatingIP(natGwKey string) error {
	if vpcNatEnabled != "true" {
		return errors.New("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(natGwKey)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(natGwKey) }()
	klog.Infof("handle update vpc fip %s", natGwKey)

	// refresh exist fips
	if err := c.initCreateAt(natGwKey); err != nil {
		err = fmt.Errorf("failed to init nat gw pod '%s' create at, %w", natGwKey, err)
		klog.Error(err)
		return err
	}

	fips, err := c.iptablesFipsLister.List(labels.SelectorFromSet(labels.Set{util.VpcNatGatewayNameLabel: natGwKey}))
	if err != nil {
		err := fmt.Errorf("failed to get all fips, %w", err)
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
		return errors.New("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(natGwKey)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(natGwKey) }()
	klog.Infof("handle update vpc eip %s", natGwKey)

	// refresh exist fips
	if err := c.initCreateAt(natGwKey); err != nil {
		err = fmt.Errorf("failed to init nat gw pod '%s' create at, %w", natGwKey, err)
		klog.Error(err)
		return err
	}
	eips, err := c.iptablesEipsLister.List(labels.Everything())
	if err != nil {
		err = fmt.Errorf("failed to get eip list, %w", err)
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
		return errors.New("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(natGwKey)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(natGwKey) }()
	klog.Infof("handle update vpc snat %s", natGwKey)

	// refresh exist snats
	if err := c.initCreateAt(natGwKey); err != nil {
		err = fmt.Errorf("failed to init nat gw pod '%s' create at, %w", natGwKey, err)
		klog.Error(err)
		return err
	}
	snats, err := c.iptablesSnatRulesLister.List(labels.SelectorFromSet(labels.Set{util.VpcNatGatewayNameLabel: natGwKey}))
	if err != nil {
		err = fmt.Errorf("failed to get all snats, %w", err)
		klog.Error(err)
		return err
	}
	for _, snat := range snats {
		if snat.Status.Redo != natGwCreatedAT {
			klog.V(3).Infof("redo snat %s", snat.Name)
			if err = c.redoSnat(snat.Name, natGwCreatedAT, false); err != nil {
				err = fmt.Errorf("failed to update eip '%s' to re-apply, %w", snat.Spec.EIP, err)
				klog.Error(err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) handleUpdateVpcDnat(natGwKey string) error {
	if vpcNatEnabled != "true" {
		return errors.New("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(natGwKey)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(natGwKey) }()
	klog.Infof("handle update vpc dnat %s", natGwKey)

	// refresh exist dnats
	if err := c.initCreateAt(natGwKey); err != nil {
		err = fmt.Errorf("failed to init nat gw pod '%s' create at, %w", natGwKey, err)
		klog.Error(err)
		return err
	}

	dnats, err := c.iptablesDnatRulesLister.List(labels.SelectorFromSet(labels.Set{util.VpcNatGatewayNameLabel: natGwKey}))
	if err != nil {
		err = fmt.Errorf("failed to get all dnats, %w", err)
		klog.Error(err)
		return err
	}
	for _, dnat := range dnats {
		if dnat.Status.Redo != natGwCreatedAT {
			klog.V(3).Infof("redo dnat %s", dnat.Name)
			if err = c.redoDnat(dnat.Name, natGwCreatedAT, false); err != nil {
				err := fmt.Errorf("failed to update dnat '%s' to redo, %w", dnat.Name, err)
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
	klog.V(3).Info(cmd)
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
		return errors.New("iptables nat gw not enable")
	}

	c.vpcNatGwKeyMutex.LockKey(natGwKey)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(natGwKey) }()
	klog.Infof("handle update subnet route for nat gateway %s", natGwKey)

	cachedPod, err := c.getNatGwPod(natGwKey)
	if err != nil {
		err = fmt.Errorf("failed to get nat gw '%s' pod, %w", natGwKey, err)
		klog.Error(err)
		return err
	}
	pod := cachedPod.DeepCopy()

	v4InternalGw, _, err := c.GetGwBySubnet(gw.Spec.Subnet)
	if err != nil {
		err = fmt.Errorf("failed to get gw, err: %w", err)
		klog.Error(err)
		return err
	}
	vpc, err := c.vpcsLister.Get(gw.Spec.Vpc)
	if err != nil {
		err = fmt.Errorf("failed to get vpc, err: %w", err)
		klog.Error(err)
		return err
	}

	// update route table
	var newCIDRS, oldCIDRs, toBeDelCIDRs []string
	if len(vpc.Status.Subnets) > 0 {
		for _, s := range vpc.Status.Subnets {
			subnet, err := c.subnetsLister.Get(s)
			if err != nil {
				err = fmt.Errorf("failed to get subnet, err: %w", err)
				klog.Error(err)
				return err
			}
			if subnet.Spec.Vlan != "" && !subnet.Spec.U2OInterconnection {
				continue
			}
			if !isOvnSubnet(subnet) || !subnet.Status.IsValidated() {
				continue
			}
			if v4Cidr, _ := util.SplitStringIP(subnet.Spec.CIDRBlock); v4Cidr != "" {
				newCIDRS = append(newCIDRS, v4Cidr)
			}
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
				err = fmt.Errorf("failed to exec nat gateway rule, err: %w", err)
				klog.Error(err)
				return err
			}
		}
	}

	if len(toBeDelCIDRs) > 0 {
		for _, cidr := range toBeDelCIDRs {
			if err = c.execNatGwRules(pod, natGwSubnetRouteDel, []string{cidr}); err != nil {
				err = fmt.Errorf("failed to exec nat gateway rule, err: %w", err)
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
	patch, err := util.GenerateStrategicMergePatchPayload(cachedPod, pod)
	if err != nil {
		klog.Error(err)
		return err
	}
	if _, err := c.config.KubeClient.CoreV1().Pods(pod.Namespace).Patch(context.Background(), pod.Name,
		types.StrategicMergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		err = fmt.Errorf("patch pod %s/%s failed %w", pod.Name, pod.Namespace, err)
		klog.Error(err)
		return err
	}

	return nil
}

func (c *Controller) execNatGwRules(pod *corev1.Pod, operation string, rules []string) error {
	cmd := fmt.Sprintf("bash /kube-ovn/nat-gateway.sh %s %s", operation, strings.Join(rules, " "))
	klog.V(3).Info(cmd)
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

func (c *Controller) setNatGwInterface(annotations map[string]string, externalNetwork string, defaultSubnet *kubeovnv1.Subnet) error {
	if vpcNatAPINadName == "" {
		return errors.New("no NetworkAttachmentDefinition provided to access apiserver, check configmap ovn-vpc-nat-config and field 'apiNadName'")
	}

	nad := fmt.Sprintf("%s/%s, %s/%s", c.config.PodNamespace, externalNetwork, corev1.NamespaceDefault, vpcNatAPINadName)
	annotations[util.AttachmentNetworkAnnotation] = nad

	return setNatGwRoute(annotations, defaultSubnet.Spec.Gateway)
}

func setNatGwRoute(annotations map[string]string, subnetGw string) error {
	dst := os.Getenv("KUBERNETES_SERVICE_HOST")

	protocol := util.CheckProtocol(dst)
	if !strings.ContainsRune(dst, '/') {
		switch protocol {
		case kubeovnv1.ProtocolIPv4:
			dst = fmt.Sprintf("%s/32", dst)
		case kubeovnv1.ProtocolIPv6:
			dst = fmt.Sprintf("%s/128", dst)
		}
	}

	// Check the API NetworkAttachmentDefinition exists, otherwise we won't be able to attach
	// the BGP speaker to a network that has access to the K8S apiserver (and won't be able to detect EIPs)
	if vpcNatAPINadProvider == "" {
		return errors.New("no NetworkAttachmentDefinition provided to access apiserver, check configmap ovn-vpc-nat-config and field 'apiNadName'")
	}

	for _, gw := range strings.Split(subnetGw, ",") {
		if util.CheckProtocol(gw) == protocol {
			routes := []request.Route{{Destination: dst, Gateway: gw}}
			buf, err := json.Marshal(routes)
			if err != nil {
				return fmt.Errorf("failed to marshal routes %+v: %w", routes, err)
			}

			annotations[fmt.Sprintf(util.RoutesAnnotationTemplate, vpcNatAPINadProvider)] = string(buf)
			break
		}
	}

	return nil
}

func (c *Controller) genNatGwStatefulSet(gw *kubeovnv1.VpcNatGateway, oldSts *v1.StatefulSet) (*v1.StatefulSet, error) {
	annotations := make(map[string]string, 7)
	if oldSts != nil && len(oldSts.Annotations) != 0 {
		annotations = maps.Clone(oldSts.Annotations)
	}
	nadName := util.GetNatGwExternalNetwork(gw.Spec.ExternalSubnets)
	podAnnotations := map[string]string{
		util.VpcNatGatewayAnnotation:     gw.Name,
		util.AttachmentNetworkAnnotation: fmt.Sprintf("%s/%s", c.config.PodNamespace, nadName),
		util.LogicalSwitchAnnotation:     gw.Spec.Subnet,
		util.IPAddressAnnotation:         gw.Spec.LanIP,
	}

	if gw.Spec.BgpSpeaker.Enabled { // Add an interface that can reach the API server
		defaultSubnet, err := c.subnetsLister.Get(c.config.DefaultLogicalSwitch)
		if err != nil {
			return nil, fmt.Errorf("failed to get default subnet %s: %w", c.config.DefaultLogicalSwitch, err)
		}

		if err := c.setNatGwInterface(podAnnotations, nadName, defaultSubnet); err != nil {
			return nil, err
		}
	}

	for key, value := range podAnnotations {
		annotations[key] = value
	}

	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
		return nil, err
	}
	v4Gateway, v6Gateway, err := c.GetGwBySubnet(gw.Spec.Subnet)
	if err != nil {
		klog.Errorf("failed to get gateway ips for subnet %s: %v", gw.Spec.Subnet, err)
	}
	v4ClusterIPRange, v6ClusterIPRange := util.SplitStringIP(c.config.ServiceClusterIPRange)
	routes := make([]request.Route, 0, 2)
	if v4Gateway != "" && v4ClusterIPRange != "" {
		routes = append(routes, request.Route{Destination: v4ClusterIPRange, Gateway: v4Gateway})
	}
	if v6Gateway != "" && v6ClusterIPRange != "" {
		routes = append(routes, request.Route{Destination: v6ClusterIPRange, Gateway: v6Gateway})
	}
	for _, subnet := range subnets {
		if subnet.Spec.Vpc != gw.Spec.Vpc || subnet.Name == gw.Spec.Subnet ||
			!isOvnSubnet(subnet) || !subnet.Status.IsValidated() ||
			(subnet.Spec.Vlan != "" && !subnet.Spec.U2OInterconnection) {
			continue
		}
		cidrV4, cidrV6 := util.SplitStringIP(subnet.Spec.CIDRBlock)
		if cidrV4 != "" && v4Gateway != "" {
			routes = append(routes, request.Route{Destination: cidrV4, Gateway: v4Gateway})
		}
		if cidrV6 != "" && v6Gateway != "" {
			routes = append(routes, request.Route{Destination: cidrV6, Gateway: v6Gateway})
		}
	}

	if err = setPodRoutesAnnotation(annotations, util.OvnProvider, routes); err != nil {
		klog.Error(err)
		return nil, err
	}

	subnet, err := c.findSubnetByNetworkAttachmentDefinition(c.config.PodNamespace, nadName, subnets)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	routes = routes[0:0]
	v4Gateway, v6Gateway = util.SplitStringIP(subnet.Spec.Gateway)
	if v4Gateway != "" {
		routes = append(routes, request.Route{Destination: "0.0.0.0/0", Gateway: v4Gateway})
	}
	if v6Gateway != "" {
		routes = append(routes, request.Route{Destination: "::/0", Gateway: v6Gateway})
	}
	if err = setPodRoutesAnnotation(annotations, subnet.Spec.Provider, routes); err != nil {
		klog.Error(err)
		return nil, err
	}

	selectors := make(map[string]string, len(gw.Spec.Selector))
	for _, v := range gw.Spec.Selector {
		parts := strings.Split(strings.TrimSpace(v), ":")
		if len(parts) != 2 {
			continue
		}
		selectors[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	klog.V(3).Infof("prepare for vpc nat gateway pod, node selector: %v", selectors)

	name := util.GenNatGwStsName(gw.Name)
	labels := map[string]string{
		"app":                   name,
		util.VpcNatGatewayLabel: "true",
	}

	sts := &v1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: v1.StatefulSetSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: ptr.To(int64(0)),
					Containers: []corev1.Container{
						{
							Name:            "vpc-nat-gw",
							Image:           vpcNatImage,
							Command:         []string{"sleep", "infinity"},
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: &corev1.SecurityContext{
								Privileged:               ptr.To(true),
								AllowPrivilegeEscalation: ptr.To(true),
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

	// BGP speaker for GWs must be enabled globally and for this specific instance
	if gw.Spec.BgpSpeaker.Enabled {
		containers := sts.Spec.Template.Spec.Containers

		// We need a speaker image configured in the NAT GW ConfigMap
		if vpcNatGwBgpSpeakerImage == "" {
			return nil, fmt.Errorf("%s should have bgp speaker image field if bgp enabled", util.VpcNatConfig)
		}

		args := []string{
			"--nat-gw-mode", // Force to run in  NAT GW mode, we're not announcing Pod IPs or Services, only EIPs
		}

		speakerParams := gw.Spec.BgpSpeaker

		if speakerParams.RouterID != "" { // Override default auto-selected RouterID
			args = append(args, fmt.Sprintf("--router-id=%s", speakerParams.RouterID))
		}

		if speakerParams.Password != "" { // Password for TCP MD5 BGP
			args = append(args, fmt.Sprintf("--auth-password=%s", speakerParams.Password))
		}

		if speakerParams.EnableGracefulRestart { // Enable graceful restart
			args = append(args, "--graceful-restart")
		}

		if speakerParams.HoldTime != (metav1.Duration{}) { // Hold time
			args = append(args, fmt.Sprintf("--holdtime=%s", speakerParams.HoldTime.Duration.String()))
		}

		if speakerParams.ASN == 0 { // The ASN we use to speak
			return nil, errors.New("ASN not set, but must be non-zero value")
		}

		if speakerParams.RemoteASN == 0 { // The ASN we speak to
			return nil, errors.New("remote ASN not set, but must be non-zero value")
		}

		args = append(args, fmt.Sprintf("--cluster-as=%d", speakerParams.ASN))
		args = append(args, fmt.Sprintf("--neighbor-as=%d", speakerParams.RemoteASN))

		if len(speakerParams.Neighbors) == 0 {
			return nil, errors.New("no BGP neighbors specified")
		}

		var neighIPv4 []string
		var neighIPv6 []string
		for _, neighbor := range speakerParams.Neighbors {
			switch util.CheckProtocol(neighbor) {
			case kubeovnv1.ProtocolIPv4:
				neighIPv4 = append(neighIPv4, neighbor)
			case kubeovnv1.ProtocolIPv6:
				neighIPv6 = append(neighIPv6, neighbor)
			}
		}

		argNeighIPv4 := strings.Join(neighIPv4, ",")
		argNeighIPv6 := strings.Join(neighIPv6, ",")
		argNeighIPv4 = fmt.Sprintf("--neighbor-address=%s", argNeighIPv4)
		argNeighIPv6 = fmt.Sprintf("--neighbor-ipv6-address=%s", argNeighIPv6)

		if len(neighIPv4) > 0 {
			args = append(args, argNeighIPv4)
		}

		if len(neighIPv6) > 0 {
			args = append(args, argNeighIPv6)
		}

		// Extra args to start the speaker with, for example, logging levels...
		args = append(args, speakerParams.ExtraArgs...)

		sts.Spec.Template.Spec.ServiceAccountName = "vpc-nat-gw"
		speakerContainer := corev1.Container{
			Name:            "vpc-nat-gw-speaker",
			Image:           vpcNatGwBgpSpeakerImage,
			Command:         []string{"/kube-ovn/kube-ovn-speaker"},
			ImagePullPolicy: corev1.PullIfNotPresent,
			Env: []corev1.EnvVar{
				{
					Name:  util.GatewayNameEnv,
					Value: gw.Name,
				},
				{
					Name: "POD_IP",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "status.podIP",
						},
					},
				},
			},
			Args: args,
		}

		sts.Spec.Template.Spec.Containers = append(containers, speakerContainer)
	}

	return sts, nil
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
		return nil, errors.New("too many pod")
	case pods[0].Status.Phase != corev1.PodRunning:
		time.Sleep(5 * time.Second)
		return nil, errors.New("pod is not active now")
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
		errMsg := fmt.Errorf("failed to get vpc nat gw '%s', %w", key, err)
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
		err = fmt.Errorf("failed to exec nat gateway rule, err: %w", err)
		klog.Error(err)
		return err
	}
	return nil
}

func (c *Controller) initVpcNatGw() error {
	klog.Infof("init all vpc nat gateways")
	gws, err := c.vpcNatGatewayLister.List(labels.Everything())
	if err != nil {
		err = fmt.Errorf("failed to get vpc nat gw list, %w", err)
		klog.Error(err)
		return err
	}
	if len(gws) == 0 {
		return nil
	}

	if vpcNatEnabled != "true" {
		err := errors.New("iptables nat gw not enable")
		klog.Warning(err)
		return nil
	}

	for _, gw := range gws {
		pod, err := c.getNatGwPod(gw.Name)
		if err != nil {
			// the nat gw maybe deleted
			err := fmt.Errorf("failed to get nat gw %s pod: %w", gw.Name, err)
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
