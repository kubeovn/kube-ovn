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

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
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

func (c *Controller) enqueueAddVpcNatGw(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.VpcNatGateway)).String()
	klog.V(3).Infof("enqueue add vpc-nat-gw %s", key)
	c.addOrUpdateVpcNatGatewayQueue.Add(key)
}

func (c *Controller) enqueueUpdateVpcNatGw(_, newObj any) {
	key := cache.MetaObjectToName(newObj.(*kubeovnv1.VpcNatGateway)).String()
	klog.V(3).Infof("enqueue update vpc-nat-gw %s", key)
	c.addOrUpdateVpcNatGatewayQueue.Add(key)
}

func (c *Controller) enqueueDeleteVpcNatGw(obj any) {
	var gw *kubeovnv1.VpcNatGateway
	switch t := obj.(type) {
	case *kubeovnv1.VpcNatGateway:
		gw = t
	case cache.DeletedFinalStateUnknown:
		g, ok := t.Obj.(*kubeovnv1.VpcNatGateway)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		gw = g
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	key := cache.MetaObjectToName(gw).String()
	klog.V(3).Infof("enqueue del vpc-nat-gw %s", key)
	c.delVpcNatGatewayQueue.Add(key)
}

func (c *Controller) handleDelVpcNatGw(key string) error {
	c.vpcNatGwKeyMutex.LockKey(key)
	defer func() { _ = c.vpcNatGwKeyMutex.UnlockKey(key) }()

	name := util.GenNatGwName(key)
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
	if !slices.Equal(gw.Spec.ExternalSubnets, gw.Status.ExternalSubnets) {
		return true
	}
	if !slices.Equal(gw.Spec.Selector, gw.Status.Selector) {
		return true
	}
	if !reflect.DeepEqual(gw.Spec.Tolerations, gw.Status.Tolerations) {
		return true
	}
	if !reflect.DeepEqual(gw.Spec.Affinity, gw.Status.Affinity) {
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

	var natGwPodContainerRestartCount int32
	pod, err := c.getNatGwPod(key)
	if err == nil {
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if containerStatus.Name == "vpc-nat-gw" {
				natGwPodContainerRestartCount = containerStatus.RestartCount
				break
			}
		}
	}

	// check or create statefulset
	needToCreate := false
	needToUpdate := false
	oldSts, err := c.config.KubeClient.AppsV1().StatefulSets(c.config.PodNamespace).
		Get(context.Background(), util.GenNatGwName(gw.Name), metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Error(err)
			return err
		}
		needToCreate, oldSts = true, nil
	}
	newSts, err := c.genNatGwStatefulSet(gw, oldSts, natGwPodContainerRestartCount)
	if err != nil {
		klog.Error(err)
		return err
	}
	if !needToCreate && (isVpcNatGwChanged(gw) || natGwPodContainerRestartCount > 0) {
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

	pod, err := c.getNatGwPod(key)
	if err != nil {
		err := fmt.Errorf("failed to get nat gw %s pod: %w", gw.Name, err)
		klog.Error(err)
		return err
	}

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
		// Check if this is a transient initialization error (e.g., first attempt before iptables chains are created)
		// The init script may fail on first run but succeed on retry after chains are established
		klog.Warningf("vpc nat gateway %s init attempt failed (will retry): %v", key, err)
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

	patch := util.KVPatch{util.VpcNatGatewayInitAnnotation: "true"}
	if err = util.PatchAnnotations(c.config.KubeClient.CoreV1().Pods(pod.Namespace), pod.Name, patch); err != nil {
		err := fmt.Errorf("failed to patch pod %s/%s: %w", pod.Namespace, pod.Name, err)
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
	cmd := "bash /kube-ovn/nat-gateway.sh " + operation
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

	pod, err := c.getNatGwPod(natGwKey)
	if err != nil {
		err = fmt.Errorf("failed to get nat gw '%s' pod, %w", natGwKey, err)
		klog.Error(err)
		return err
	}

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

	patch := util.KVPatch{util.VpcCIDRsAnnotation: string(cidrBytes)}
	if err = util.PatchAnnotations(c.config.KubeClient.CoreV1().Pods(pod.Namespace), pod.Name, patch); err != nil {
		err = fmt.Errorf("failed to patch pod %s/%s: %w", pod.Namespace, pod.Name, err)
		klog.Error(err)
		return err
	}

	return nil
}

func (c *Controller) execNatGwRules(pod *corev1.Pod, operation string, rules []string) error {
	cmd := fmt.Sprintf("bash /kube-ovn/nat-gateway.sh %s %s", operation, strings.Join(rules, " "))
	klog.V(3).Infof("executing NAT gateway command: %s", cmd)
	stdOutput, errOutput, err := util.ExecuteCommandInContainer(c.config.KubeClient, c.config.KubeRestConfig, pod.Namespace, pod.Name, "vpc-nat-gw", []string{"/bin/bash", "-c", cmd}...)
	if err != nil {
		if len(errOutput) > 0 {
			klog.Errorf("NAT gateway command failed - stderr: %v", errOutput)
		}
		if len(stdOutput) > 0 {
			klog.Infof("NAT gateway command failed - stdout: %v", stdOutput)
		}
		klog.Errorf("NAT gateway command execution error: %v", err)
		return err
	}

	if len(stdOutput) > 0 {
		klog.V(3).Infof("NAT gateway command succeeded - stdout: %v", stdOutput)
	}

	if len(errOutput) > 0 {
		klog.Errorf("failed to ExecuteCommandInContainer errOutput: %v", errOutput)
		return errors.New(errOutput)
	}
	return nil
}

// setNatGwAPIAccess adds an interface with API access to the NAT gateway and attaches the standard externalNetwork to the gateway.
// This interface is backed by a NetworkAttachmentDefinition (NAD) with a provider corresponding
// to one that is configured on a subnet part of the default VPC (the K8S apiserver runs in the default VPC)
func (c *Controller) setNatGwAPIAccess(annotations map[string]string) error {
	// Check the NetworkAttachmentDefinition provider exists, must be user-configured
	if vpcNatAPINadProvider == "" {
		return errors.New("no NetworkAttachmentDefinition provided to access apiserver, check configmap ovn-vpc-nat-config and field 'apiNadProvider'")
	}

	// Subdivide provider so we can infer the name of the NetworkAttachmentDefinition
	providerSplit := strings.Split(vpcNatAPINadProvider, ".")
	if len(providerSplit) != 3 || providerSplit[2] != util.OvnProvider {
		return fmt.Errorf("name of the provider must have syntax 'name.namespace.ovn', got %s", vpcNatAPINadProvider)
	}

	// Extract the name of the provider and its namespace
	name, namespace := providerSplit[0], providerSplit[1]

	// Craft the name of the NAD for the externalNetwork and the apiNetwork
	networkAttachments := []string{fmt.Sprintf("%s/%s", namespace, name)}
	if externalNetworkAttachment, ok := annotations[nadv1.NetworkAttachmentAnnot]; ok {
		networkAttachments = append([]string{externalNetworkAttachment}, networkAttachments...)
	}

	// Attach the NADs to the Pod by adding them to the special annotation
	annotations[nadv1.NetworkAttachmentAnnot] = strings.Join(networkAttachments, ",")

	// Set the network route to the API, so we can reach it
	return c.setNatGwAPIRoute(annotations, namespace, name)
}

// setNatGwAPIRoute adds routes to a pod to reach the K8S API server
func (c *Controller) setNatGwAPIRoute(annotations map[string]string, nadNamespace, nadName string) error {
	dst := os.Getenv("KUBERNETES_SERVICE_HOST")

	protocol := util.CheckProtocol(dst)
	if !strings.ContainsRune(dst, '/') {
		switch protocol {
		case kubeovnv1.ProtocolIPv4:
			dst += "/32"
		case kubeovnv1.ProtocolIPv6:
			dst += "/128"
		}
	}

	// Retrieve every subnet on the cluster
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list subnets: %w", err)
	}

	// Retrieve the subnet connected to the NAD, this subnet should be in the VPC of the API
	apiSubnet, err := c.findSubnetByNetworkAttachmentDefinition(nadNamespace, nadName, subnets)
	if err != nil {
		return fmt.Errorf("failed to find api subnet using the nad %s/%s: %w", nadNamespace, nadName, err)
	}

	// Craft the route to reach the API from the subnet we've just retrieved
	for gw := range strings.SplitSeq(apiSubnet.Spec.Gateway, ",") {
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

func (c *Controller) genNatGwStatefulSet(gw *kubeovnv1.VpcNatGateway, oldSts *v1.StatefulSet, natGwPodContainerRestartCount int32) (*v1.StatefulSet, error) {
	annotations := make(map[string]string, 7)
	if oldSts != nil && len(oldSts.Annotations) != 0 {
		annotations = maps.Clone(oldSts.Annotations)
	}

	externalNadNamespace, externalNadName := c.getExternalSubnetNad(gw)
	podAnnotations := util.GenNatGwPodAnnotations(gw, externalNadNamespace, externalNadName)

	// Restart logic to fix #5072
	if oldSts != nil && len(oldSts.Spec.Template.Annotations) != 0 {
		if _, ok := oldSts.Spec.Template.Annotations[util.VpcNatGatewayContainerRestartAnnotation]; !ok && natGwPodContainerRestartCount > 0 {
			podAnnotations[util.VpcNatGatewayContainerRestartAnnotation] = ""
		}
	}
	klog.V(3).Infof("%s podAnnotations:%v", gw.Name, podAnnotations)

	// Add an interface that can reach the API server, we need access to it to probe Kube-OVN resources
	if gw.Spec.BgpSpeaker.Enabled {
		if err := c.setNatGwAPIAccess(podAnnotations); err != nil {
			klog.Errorf("couldn't add an API interface to the NAT gateway: %v", err)
			return nil, err
		}
	}

	maps.Copy(annotations, podAnnotations)

	// Retrieve all subnets in existence
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
		return nil, err
	}

	// Retrieve the gateways of the subnet sitting behind the NAT gateway
	v4Gateway, v6Gateway, err := c.GetGwBySubnet(gw.Spec.Subnet)
	if err != nil {
		klog.Errorf("failed to get gateway ips for subnet %s: %v", gw.Spec.Subnet, err)
		return nil, err
	}

	// Add routes to join the services (is this still needed?)
	v4ClusterIPRange, v6ClusterIPRange := util.SplitStringIP(c.config.ServiceClusterIPRange)
	routes := make([]request.Route, 0, 2)
	if v4Gateway != "" && v4ClusterIPRange != "" {
		routes = append(routes, request.Route{Destination: v4ClusterIPRange, Gateway: v4Gateway})
	}
	if v6Gateway != "" && v6ClusterIPRange != "" {
		routes = append(routes, request.Route{Destination: v6ClusterIPRange, Gateway: v6Gateway})
	}

	// Add gateway to join every subnet in the same VPC? (is this still needed?)
	// Are we trying to give the NAT gateway access to every subnet in the VPC?
	// I suspect this is to solve a problem where a static route is inserted to redirect all the traffic
	// from a VPC into the NAT GW. When that happens, the GW has no return path to the other subnets.
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

	// Use this function is nat gw set route?
	if err = setPodRoutesAnnotation(annotations, util.OvnProvider, routes); err != nil {
		klog.Error(err)
		return nil, err
	}

	// Set the default routes to the external network
	subnet, err := c.subnetsLister.Get(util.GetNatGwExternalNetwork(gw.Spec.ExternalSubnets))
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
	// TODO:// check NAD if has ipam to disable ipam
	if !gw.Spec.NoDefaultEIP {
		if err = setPodRoutesAnnotation(annotations, subnet.Spec.Provider, routes); err != nil {
			klog.Error(err)
			return nil, err
		}
	} else {
		// NAT gateway uses no-IPAM mode in network attachment definition when NoDefaultEIP is enabled
		// This allows macvlan/other CNI plugins to work without IP allocation from Kube-OVN
		klog.Infof("skipping IP allocation for NAT gateway %s (NoDefaultEIP enabled)", gw.Name)
		annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, subnet.Spec.Provider)] = "true"
	}

	selectors := util.GenNatGwSelectors(gw.Spec.Selector)
	klog.V(3).Infof("prepare for vpc nat gateway pod, node selector: %v", selectors)

	labels := util.GenNatGwLabels(gw.Name)

	sts := &v1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:   util.GenNatGwName(gw.Name),
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
							Name:    "vpc-nat-gw",
							Image:   vpcNatImage,
							Command: []string{"sleep", "infinity"},
							Lifecycle: &corev1.Lifecycle{
								PostStart: &corev1.LifecycleHandler{
									Exec: &corev1.ExecAction{
										Command: []string{"sh", "-c", "sysctl -w net.ipv4.ip_forward=1"},
									},
								},
							},
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env: []corev1.EnvVar{
								{
									Name:  "GATEWAY_V4",
									Value: v4Gateway,
								},
								{
									Name:  "GATEWAY_V6",
									Value: v6Gateway,
								},
							},
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

	// BGP speaker is enabled on this instance, add a BGP speaker to the statefulset
	if gw.Spec.BgpSpeaker.Enabled {
		// We need to connect to the K8S API to make the BGP speaker work, this implies a ServiceAccount
		sts.Spec.Template.Spec.ServiceAccountName = "vpc-nat-gw"
		sts.Spec.Template.Spec.AutomountServiceAccountToken = ptr.To(true)

		// Craft a BGP speaker container to add to our statefulset
		bgpSpeakerContainer, err := util.GenNatGwBgpSpeakerContainer(gw.Spec.BgpSpeaker, vpcNatGwBgpSpeakerImage, gw.Name)
		if err != nil {
			klog.Errorf("failed to create a BGP speaker container for gateway %s: %v", gw.Name, err)
			return nil, err
		}

		// Add our container to the list of containers in the statefulset
		sts.Spec.Template.Spec.Containers = append(sts.Spec.Template.Spec.Containers, *bgpSpeakerContainer)
	}

	return sts, nil
}

// getExternalSubnetNad returns the namespace and name of the NetworkAttachmentDefinition associated with
// an external network attached to a NAT gateway
func (c *Controller) getExternalSubnetNad(gw *kubeovnv1.VpcNatGateway) (string, string) {
	externalNadNamespace := c.config.PodNamespace
	externalNadName := util.GetNatGwExternalNetwork(gw.Spec.ExternalSubnets)
	if externalSubnet, err := c.subnetsLister.Get(externalNadName); err == nil {
		if name, namespace, ok := util.GetNadBySubnetProvider(externalSubnet.Spec.Provider); ok {
			externalNadName = name
			externalNadNamespace = namespace
		}
	}

	return externalNadNamespace, externalNadName
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
		MatchLabels: map[string]string{"app": util.GenNatGwName(name), util.VpcNatGatewayLabel: "true"},
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

	if !slices.Equal(gw.Spec.ExternalSubnets, gw.Status.ExternalSubnets) {
		gw.Status.ExternalSubnets = gw.Spec.ExternalSubnets
		changed = true
	}
	if !slices.Equal(gw.Spec.Selector, gw.Status.Selector) {
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
		if err = c.execNatGwQoSInPod(gw.Name, &rule, operation); err != nil {
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
	switch r.MatchType {
	case "ip":
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
	case "":
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
