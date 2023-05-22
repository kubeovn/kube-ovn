package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	VpcSslVpnGatewayEnabled   = "unknown"
	VpcSslVpnGatewayCmVersion = ""
	VpcSslVpnGatewayCreatedAt = ""
	vpcSslVpnGatewayInit      = "init"
)

const (
	SslVpnGatewayInit = "init"
)

func genSslVpnGatewayStsName(name string) string {
	return fmt.Sprintf("ssl-vpn-gw-%s", name)
}

func (c *Controller) enqueueAddVpcSslVpnGateway(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add vpc-ssl-vpn-gw %s", key)
	c.addOrUpdateVpcSslVpnGatewayQueue.Add(key)
}

func (c *Controller) enqueueUpdateVpcSslVpnGateway(old, new interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue update vpc-ssl-vpn-gw %s", key)
	c.addOrUpdateVpcSslVpnGatewayQueue.Add(key)
}

func (c *Controller) enqueueDeleteVpcSslVpnGateway(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue del vpc-ssl-vpn-gw %s", key)
	c.delVpcSslVpnGatewayQueue.Add(key)
}

func (c *Controller) runInitVpcSslVpnGatewayWorker() {
	for c.processNextWorkItem("initVpcSslVpnGateway", c.initVpcSslVpnGatewayQueue, c.handleInitVpcSslVpnGateway) {
	}
}

func (c *Controller) runAddOrUpdateVpcSslVpnGatewayWorker() {
	for c.processNextWorkItem("addOrUpdateVpcSslVpnGateway", c.addOrUpdateVpcSslVpnGatewayQueue, c.handleAddOrUpdateVpcSslVpnGateway) {
	}
}

func (c *Controller) runDelVpcSslVpnGatewayWorker() {
	for c.processNextWorkItem("delVpcSslVpnGateway", c.delVpcSslVpnGatewayQueue, c.handleDelVpcSslVpnGateway) {
	}
}

func (c *Controller) resyncVpcSslVpnGatewayConfig() {
	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.VpcSslVpnGatewayConfig)
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to get configmap %s, %v", util.VpcSslVpnGatewayConfig, err)
		return
	}

	if k8serrors.IsNotFound(err) || cm.Data["enable-vpc-ssl-vpn-gw"] == "false" {
		if VpcSslVpnGatewayEnabled == "false" {
			return
		}
		klog.Info("start to clean up ssl vpn gw")
		if err := c.cleanUpVpcSslVpnGateway(); err != nil {
			klog.Errorf("failed to clean up ssl vpn gw, %v", err)
			return
		}
		VpcSslVpnGatewayEnabled = "false"
		VpcSslVpnGatewayCmVersion = ""
		klog.Info("finish clean up ssl vpn gw")
		return
	} else {
		if VpcSslVpnGatewayEnabled == "true" && VpcSslVpnGatewayCmVersion == cm.ResourceVersion {
			return
		}
		gws, err := c.vpcSslVpnGatewayLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to get ssl vpn gw, %v", err)
			return
		}
		if err = c.resyncVpcNatImage(); err != nil {
			klog.Errorf("failed to resync vpc nat image config, err: %v", err)
			return
		}
		VpcSslVpnGatewayEnabled = "true"
		VpcSslVpnGatewayCmVersion = cm.ResourceVersion
		for _, gw := range gws {
			c.addOrUpdateVpcSslVpnGatewayQueue.Add(gw.Name)
		}
		klog.Info("finish establishing ssl vpn gateway")
		return
	}
}

func (c *Controller) handleDelVpcSslVpnGateway(key string) error {
	c.vpcSslVpnGatewayKeyMutex.LockKey(key)
	defer func() { _ = c.vpcSslVpnGatewayKeyMutex.UnlockKey(key) }()
	name := genSslVpnGatewayStsName(key)
	klog.Infof("delete ssl vpn gw %s", name)
	if err := c.config.KubeClient.AppsV1().StatefulSets(c.config.PodNamespace).Delete(context.Background(),
		name, metav1.DeleteOptions{}); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

func isVpcSslVpnGatewayChanged(gw *kubeovnv1.VpcSslVpnGateway) bool {
	if gw.Status.Subnet == "" && gw.Spec.Subnet != "" {
		// subnet not support change
		gw.Status.Subnet = gw.Spec.Subnet
		return true
	}
	if gw.Status.Ip != gw.Spec.Ip {
		gw.Status.Ip = gw.Spec.Ip
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

func (c *Controller) handleAddOrUpdateVpcSslVpnGateway(key string) error {
	// create ssl vpn gw statefulset
	c.vpcSslVpnGatewayKeyMutex.LockKey(key)
	defer func() { _ = c.vpcSslVpnGatewayKeyMutex.UnlockKey(key) }()
	klog.Infof("handle add/update ssl vpn gw %s", key)

	if VpcSslVpnGatewayEnabled != "true" {
		return fmt.Errorf("ssl vpn gw not enable")
	}
	gw, err := c.vpcSslVpnGatewayLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if gw.Spec.ConfigMap == "" {
		err := fmt.Errorf("vpc ssl vpn gw configmap is required")
		klog.Error(err)
		return err
	}
	if _, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(gw.Spec.ConfigMap); err != nil {
		err = fmt.Errorf("failed to get configmap '%s/%s', err: %v", c.config.PodNamespace, gw.Spec.ConfigMap, err)
		klog.Error(err)
		return err
	}
	subnet, err := c.subnetsLister.Get(gw.Spec.Subnet)
	if err != nil {
		err = fmt.Errorf("failed to get subnet '%s', err: %v", gw.Spec.Subnet, err)
		klog.Error(err)
		return err
	}

	if _, err := c.vpcsLister.Get(subnet.Spec.Vpc); err != nil {
		err = fmt.Errorf("failed to get vpc '%s', err: %v", subnet.Spec.Vpc, err)
		klog.Error(err)
		return err
	}
	// check or create statefulset
	needToCreate := false
	needToUpdate := false
	oldSts, err := c.config.KubeClient.AppsV1().StatefulSets(c.config.PodNamespace).
		Get(context.Background(), genSslVpnGatewayStsName(gw.Name), metav1.GetOptions{})

	if err != nil {
		if k8serrors.IsNotFound(err) {
			needToCreate = true
		} else {
			return err
		}
	}
	if err := c.resyncVpcNatImage(); err != nil {
		klog.Errorf("failed to sync vpc ssl vpn image, err: %v", err)
		return nil
	}
	newSts := c.genSslVpnGatewayStatefulSet(gw, oldSts.DeepCopy())
	if !needToCreate && isVpcSslVpnGatewayChanged(gw) {
		needToUpdate = true
	}

	if needToCreate {
		if _, err := c.config.KubeClient.AppsV1().StatefulSets(c.config.PodNamespace).
			Create(context.Background(), newSts, metav1.CreateOptions{}); err != nil {
			err := fmt.Errorf("failed to create statefulset '%s', err: %v", newSts.Name, err)
			klog.Error(err)
			return err
		}
		if err = c.patchSslVpnGatewayStatus(key); err != nil {
			klog.Errorf("failed to patch ssl vpn gw sts status for ssl vpn gw %s, %v", key, err)
			return err
		}
		return nil
	}
	if needToUpdate {
		if _, err := c.config.KubeClient.AppsV1().StatefulSets(c.config.PodNamespace).
			Update(context.Background(), newSts, metav1.UpdateOptions{}); err != nil {
			err := fmt.Errorf("failed to update statefulset '%s', err: %v", newSts.Name, err)
			klog.Error(err)
			return err
		}
		if err = c.patchSslVpnGatewayStatus(key); err != nil {
			klog.Errorf("failed to patch ssl vpn gw sts status for ssl vpn gw %s, %v", key, err)
			return err
		}
	}
	return nil
}

func (c *Controller) handleInitVpcSslVpnGateway(key string) error {
	if VpcSslVpnGatewayEnabled != "true" {
		return fmt.Errorf("ssl vpn gw not enable")
	}

	c.vpcSslVpnGatewayKeyMutex.LockKey(key)
	defer func() { _ = c.vpcSslVpnGatewayKeyMutex.UnlockKey(key) }()

	klog.Infof("handle init ssl vpn gw %s", key)
	gw, err := c.vpcSslVpnGatewayLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	// subnet for vpc-ssl-vpn-gw has been checked when create vpc-ssl-vpn-gw
	oriPod, err := c.getSslVpnGatewayPod(key)
	if err != nil {
		err := fmt.Errorf("failed to get ssl vpn gw %s pod: %v", gw.Name, err)
		klog.Error(err)
		return err
	}
	pod := oriPod.DeepCopy()
	if pod.Status.Phase != corev1.PodRunning {
		time.Sleep(10 * time.Second)
		return fmt.Errorf("failed to init ssl vpn gw, pod is not ready")
	}

	if _, hasInit := pod.Annotations[util.VpcSslVpnGatewayInitAnnotation]; hasInit {
		return nil
	}
	VpcSslVpnGatewayCreatedAt = pod.CreationTimestamp.Format("2006-01-02T15:04:05")
	klog.V(3).Infof("ssl vpn gw pod '%s' inited at %s", key, VpcSslVpnGatewayCreatedAt)
	if err = c.execInSslVpnGateway(pod, SslVpnGatewayInit, []string{fmt.Sprintf("%s,%s", c.config.ServiceClusterIPRange, pod.Annotations[util.GatewayAnnotation])}); err != nil {
		err = fmt.Errorf("failed to init ssl vpn gw, %v", err)
		klog.Error(err)
		return err
	}

	if err := c.updateCrdSslVpnGatewayLabels(gw.Name); err != nil {
		err := fmt.Errorf("failed to update ssl vpn gw %s: %v", gw.Name, err)
		klog.Error(err)
		return err
	}

	c.updateVpcEipQueue.Add(key)
	pod.Annotations[util.VpcSslVpnGatewayInitAnnotation] = "true"
	patch, err := util.GenerateStrategicMergePatchPayload(oriPod, pod)
	if err != nil {
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

func (c *Controller) execInSslVpnGateway(pod *corev1.Pod, operation string, rules []string) error {
	// cmd := fmt.Sprintf("bash /kube-ovn/vpc-ssl-vpn-gateway.sh %s %s", operation, strings.Join(rules, " "))
	cmd := fmt.Sprintf("bash ls -l")
	klog.V(3).Infof(cmd)
	stdOutput, errOutput, err := util.ExecuteCommandInContainer(c.config.KubeClient, c.config.KubeRestConfig, pod.Namespace, pod.Name, "vpc-ssl-vpn-gw", []string{"/bin/bash", "-c", cmd}...)

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

func (c *Controller) genSslVpnGatewayStatefulSet(gw *kubeovnv1.VpcSslVpnGateway, oldSts *v1.StatefulSet) (newSts *v1.StatefulSet) {
	replicas := int32(1)
	name := genSslVpnGatewayStsName(gw.Name)
	allowPrivilegeEscalation := true
	privileged := true
	labels := map[string]string{
		"app":                      name,
		util.VpcSslVpnGatewayLabel: "true",
	}
	newPodAnnotations := map[string]string{}
	if oldSts != nil && len(oldSts.Annotations) != 0 {
		newPodAnnotations = oldSts.Annotations
	}
	podAnnotations := map[string]string{
		util.VpcSslVpnGatewayAnnotation: gw.Name,
		util.LogicalSwitchAnnotation:    gw.Spec.Subnet,
		util.IpAddressAnnotation:        gw.Spec.Ip,
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

	klog.V(3).Infof("prepare for ssl vpn gw pod, node selector: %v", selectors)
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
							Name:            "vpc-ssl-vpn-gw",
							Image:           sslVpnImage,
							Command:         []string{"bash"},
							Args:            []string{"-c", "sleep infinity"},
							EnvFrom:         []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: gw.Spec.ConfigMap}}}},
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

func (c *Controller) cleanUpVpcSslVpnGateway() error {
	gws, err := c.vpcSslVpnGatewayLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to get ssl vpn gw, %v", err)
		return err
	}
	for _, gw := range gws {
		c.delVpcSslVpnGatewayQueue.Add(gw.Name)
	}
	return nil
}

func (c *Controller) getSslVpnGatewayPod(name string) (*corev1.Pod, error) {
	sel, _ := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{"app": genSslVpnGatewayStsName(name), util.VpcSslVpnGatewayLabel: "true"},
	})

	pods, err := c.podsLister.Pods(c.config.PodNamespace).List(sel)
	if err != nil {
		return nil, err
	} else if len(pods) == 0 {
		return nil, k8serrors.NewNotFound(v1.Resource("pod"), name)
	} else if len(pods) != 1 {
		time.Sleep(5 * time.Second)
		return nil, fmt.Errorf("too many pod")
	} else if pods[0].Status.Phase != "Running" {
		time.Sleep(5 * time.Second)
		return nil, fmt.Errorf("pod is not active now")
	}

	return pods[0], nil
}

func (c *Controller) initSslVpnGatewayCreateAt(key string) (err error) {
	if VpcSslVpnGatewayCreatedAt != "" {
		return nil
	}
	pod, err := c.getSslVpnGatewayPod(key)
	if err != nil {
		return err
	}
	VpcSslVpnGatewayCreatedAt = pod.CreationTimestamp.Format("2006-01-02T15:04:05")
	return nil
}

func (c *Controller) updateCrdSslVpnGatewayLabels(key string) error {
	gw, err := c.vpcSslVpnGatewayLister.Get(key)
	if err != nil {
		errMsg := fmt.Errorf("failed to get ssl vpn gw '%s', %v", key, err)
		klog.Error(errMsg)
		return errMsg
	}
	subnet, err := c.subnetsLister.Get(gw.Spec.Subnet)
	if err != nil {
		err = fmt.Errorf("failed to get subnet '%s', err: %v", gw.Spec.Subnet, err)
		klog.Error(err)
		return err
	}
	var needUpdateLabel bool
	var op string
	// ssl vpn gw label may lost
	if len(gw.Labels) == 0 {
		op = "add"
		gw.Labels = map[string]string{
			util.SubnetNameLabel: gw.Spec.Subnet,
			util.VpcNameLabel:    subnet.Spec.Vpc,
		}
		needUpdateLabel = true
	} else {
		if gw.Labels[util.SubnetNameLabel] != gw.Spec.Subnet {
			op = "replace"
			gw.Labels[util.SubnetNameLabel] = gw.Spec.Subnet
			needUpdateLabel = true
		}
		if gw.Labels[util.VpcNameLabel] != subnet.Spec.Vpc {
			op = "replace"
			gw.Labels[util.VpcNameLabel] = subnet.Spec.Vpc
			needUpdateLabel = true
		}
	}
	if needUpdateLabel {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
		raw, _ := json.Marshal(gw.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().VpcSslVpnGateways().Patch(context.Background(), gw.Name, types.JSONPatchType,
			[]byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to patch ssl vpn gw %s: %v", gw.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) getSslVpnGateway(vpc, subnet string) (string, error) {
	selectors := labels.Set{util.VpcNameLabel: vpc, util.SubnetNameLabel: subnet}.AsSelector()
	gws, err := c.vpcSslVpnGatewayLister.List(selectors)
	if err != nil {
		return "", err
	}
	if len(gws) == 1 {
		return gws[0].Name, nil
	}
	if len(gws) == 0 {
		return "", fmt.Errorf("no ssl vpn gw found by selector %v", selectors)
	}
	return "", fmt.Errorf("too many ssl vpn gw")
}

func (c *Controller) patchSslVpnGatewayStatus(key string) error {
	var changed bool
	oriGw, err := c.vpcSslVpnGatewayLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get ssl vpn gw %s, %v", key, err)
		return err
	}
	gw := oriGw.DeepCopy()
	if gw.Status.Subnet == "" && gw.Status.Subnet != gw.Spec.Subnet {
		// subnet not support update
		gw.Status.Subnet = gw.Spec.Subnet
		changed = true
	}
	if gw.Status.Ip != gw.Spec.Ip {
		gw.Status.Ip = gw.Spec.Ip
		changed = true
	}
	if gw.Status.ConfigMap != gw.Spec.ConfigMap {
		gw.Status.ConfigMap = gw.Spec.ConfigMap
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
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().VpcSslVpnGateways().Patch(context.Background(), gw.Name, types.MergePatchType,
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
