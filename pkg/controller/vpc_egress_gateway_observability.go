package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilversion "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/utils/ptr"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/pkg/vegoobserver"
)

const (
	vpcEgressObserverContainerName           = "observability"
	vpcEgressObserverConfigVolume            = "observability-config"
	vpcEgressObserverPodInfoVolume           = "observability-podinfo"
	vpcEgressObserverConfigPath              = "/etc/kube-ovn-observer/config.json"
	vpcEgressObserverNetworkStatusPath       = "/etc/podinfo/network-status"
	vpcEgressObserverPort              int32 = 10666
)

var serviceMonitorGVR = schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "servicemonitors"}

type vpcEgressObserverState struct {
	enabled    bool
	configName string
}

func observabilityEnabled(config *kubeovnv1.VpcEgressGatewayObservability) bool {
	return config != nil && (config.InterfaceMetrics.Enabled || config.Conntrack.Metrics.Enabled || config.Conntrack.Log.Enabled)
}

func observabilityMetricsEnabled(config *kubeovnv1.VpcEgressGatewayObservability) bool {
	return config != nil && (config.InterfaceMetrics.Enabled || config.Conntrack.Metrics.Enabled)
}

func (c *Controller) supportsRestartableInitContainers() (bool, error) {
	c.restartableInitContainersOnce.Do(func() {
		serverVersion, err := c.config.KubeClient.Discovery().ServerVersion()
		if err != nil {
			c.restartableInitContainersErr = fmt.Errorf("discover Kubernetes version: %w", err)
			return
		}
		version, err := utilversion.ParseSemantic(serverVersion.GitVersion)
		if err != nil {
			c.restartableInitContainersErr = fmt.Errorf("parse Kubernetes version %q: %w", serverVersion.GitVersion, err)
			return
		}
		c.restartableInitContainers = version.AtLeast(utilversion.MustParseSemantic("1.29.0"))
	})
	return c.restartableInitContainers, c.restartableInitContainersErr
}

func (c *Controller) reconcileVpcEgressGatewayObservability(gw *kubeovnv1.VpcEgressGateway, externalNetwork string, labels map[string]string) vpcEgressObserverState {
	resourceName := vpcEgressObserverResourceName(gw)
	if !observabilityEnabled(gw.Spec.Observability) {
		c.deleteVpcEgressGatewayObservabilityResources(gw, resourceName)
		gw.Status.Conditions.RemoveCondition(kubeovnv1.ObservabilityConfigured)
		gw.Status.Conditions.RemoveCondition(kubeovnv1.ServiceMonitorReady)
		return vpcEgressObserverState{}
	}

	supported, err := c.supportsRestartableInitContainers()
	if err != nil || !supported {
		reason, message := "UnsupportedKubernetesVersion", "restartable init containers require Kubernetes 1.29 or later"
		if err != nil {
			reason, message = "KubernetesVersionUnknown", err.Error()
		}
		c.setVpcEgressGatewayObservabilityCondition(gw, kubeovnv1.ObservabilityConfigured, corev1.ConditionFalse, reason, message)
		if observabilityMetricsEnabled(gw.Spec.Observability) {
			c.setVpcEgressGatewayObservabilityCondition(gw, kubeovnv1.ServiceMonitorReady, corev1.ConditionFalse, reason, message)
		} else {
			gw.Status.Conditions.RemoveCondition(kubeovnv1.ServiceMonitorReady)
		}
		c.deleteVpcEgressGatewayObservabilityResources(gw, resourceName)
		return vpcEgressObserverState{}
	}
	if !observabilityMetricsEnabled(gw.Spec.Observability) {
		gw.Status.Conditions.RemoveCondition(kubeovnv1.ServiceMonitorReady)
	}

	observerConfig := vegoobserver.Config{Namespace: gw.Namespace, Name: gw.Name, ExternalNetwork: externalNetwork, Observability: *gw.Spec.Observability.DeepCopy()}
	data, err := json.Marshal(observerConfig)
	if err != nil {
		c.setVpcEgressGatewayObservabilityCondition(gw, kubeovnv1.ObservabilityConfigured, corev1.ConditionFalse, "InvalidConfiguration", err.Error())
		if observabilityMetricsEnabled(gw.Spec.Observability) {
			c.setVpcEgressGatewayObservabilityCondition(gw, kubeovnv1.ServiceMonitorReady, corev1.ConditionFalse, "InvalidConfiguration", err.Error())
		}
		return vpcEgressObserverState{}
	}
	if err := c.reconcileVpcEgressObserverConfigMap(gw, resourceName, labels, data); err != nil {
		c.setVpcEgressGatewayObservabilityCondition(gw, kubeovnv1.ObservabilityConfigured, corev1.ConditionFalse, "ConfigMapReconcileFailed", err.Error())
		if observabilityMetricsEnabled(gw.Spec.Observability) {
			c.setVpcEgressGatewayObservabilityCondition(gw, kubeovnv1.ServiceMonitorReady, corev1.ConditionFalse, "ConfigMapReconcileFailed", err.Error())
		}
		c.deleteOwnedService(gw, resourceName)
		c.deleteOwnedServiceMonitor(gw, resourceName)
		return vpcEgressObserverState{}
	}
	c.setVpcEgressGatewayObservabilityCondition(gw, kubeovnv1.ObservabilityConfigured, corev1.ConditionTrue, "Configured", "")

	if observabilityMetricsEnabled(gw.Spec.Observability) {
		if err := c.reconcileVpcEgressObserverService(gw, resourceName, labels); err != nil {
			c.setVpcEgressGatewayObservabilityCondition(gw, kubeovnv1.ObservabilityConfigured, corev1.ConditionFalse, "ServiceReconcileFailed", err.Error())
			c.setVpcEgressGatewayObservabilityCondition(gw, kubeovnv1.ServiceMonitorReady, corev1.ConditionFalse, "ServiceReconcileFailed", err.Error())
			c.deleteOwnedConfigMap(gw, resourceName)
			c.deleteOwnedServiceMonitor(gw, resourceName)
			return vpcEgressObserverState{}
		}
		if err := c.reconcileVpcEgressObserverServiceMonitor(gw, resourceName, labels); err != nil {
			reason := "ServiceMonitorReconcileFailed"
			if k8serrors.IsNotFound(err) {
				reason = "ServiceMonitorCRDNotInstalled"
			}
			c.setVpcEgressGatewayObservabilityCondition(gw, kubeovnv1.ServiceMonitorReady, corev1.ConditionFalse, reason, err.Error())
			c.addOrUpdateVpcEgressGatewayQueue.AddAfter(fmt.Sprintf("%s/%s", gw.Namespace, gw.Name), 30*time.Second)
		} else {
			c.setVpcEgressGatewayObservabilityCondition(gw, kubeovnv1.ServiceMonitorReady, corev1.ConditionTrue, "Ready", "")
		}
	} else {
		c.deleteOwnedService(gw, resourceName)
		c.deleteOwnedServiceMonitor(gw, resourceName)
		gw.Status.Conditions.RemoveCondition(kubeovnv1.ServiceMonitorReady)
	}
	return vpcEgressObserverState{enabled: true, configName: resourceName}
}

func (c *Controller) setVpcEgressGatewayObservabilityCondition(gw *kubeovnv1.VpcEgressGateway, conditionType kubeovnv1.ConditionType, status corev1.ConditionStatus, reason, message string) {
	previous := gw.Status.Conditions.GetCondition(conditionType)
	gw.Status.Conditions.SetCondition(conditionType, status, reason, message, gw.Generation)
	if status == corev1.ConditionFalse && (previous == nil || previous.Status != status || previous.Reason != reason || previous.Message != message) {
		c.recordVpcEgressGatewayEvent(gw, corev1.EventTypeWarning, reason, message)
	}
}

func vpcEgressObserverResourceName(gw *kubeovnv1.VpcEgressGateway) string {
	return util.NormalizeLabelValue("vpc-egress-" + strings.ReplaceAll(gw.Spec.Prefix+gw.Name+"-observability", ".", "-"))
}

func (c *Controller) reconcileVpcEgressObserverConfigMap(gw *kubeovnv1.VpcEgressGateway, name string, labels map[string]string, data []byte) error {
	configMaps := c.config.KubeClient.CoreV1().ConfigMaps(gw.Namespace)
	desired := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: gw.Namespace, Labels: maps.Clone(labels)}, Data: map[string]string{"config.json": string(data)}}
	if err := util.SetOwnerReference(gw, desired); err != nil {
		return err
	}
	current, err := configMaps.Get(context.Background(), name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = configMaps.Create(context.Background(), desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if !metav1.IsControlledBy(current, gw) {
		return fmt.Errorf("config map %s/%s is not controlled by this VpcEgressGateway", gw.Namespace, name)
	}
	desired.ResourceVersion = current.ResourceVersion
	_, err = configMaps.Update(context.Background(), desired, metav1.UpdateOptions{})
	return err
}

func (c *Controller) reconcileVpcEgressObserverService(gw *kubeovnv1.VpcEgressGateway, name string, labels map[string]string) error {
	services := c.config.KubeClient.CoreV1().Services(gw.Namespace)
	desired := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: gw.Namespace, Labels: maps.Clone(labels)}, Spec: corev1.ServiceSpec{
		ClusterIP: corev1.ClusterIPNone, PublishNotReadyAddresses: true, Selector: maps.Clone(labels),
		Ports: []corev1.ServicePort{{Name: "metrics", Port: vpcEgressObserverPort, TargetPort: intstrFromInt32(vpcEgressObserverPort)}},
	}}
	if err := util.SetOwnerReference(gw, desired); err != nil {
		return err
	}
	current, err := services.Get(context.Background(), name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = services.Create(context.Background(), desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if !metav1.IsControlledBy(current, gw) {
		return fmt.Errorf("service %s/%s is not controlled by this VpcEgressGateway", gw.Namespace, name)
	}
	desired.ResourceVersion = current.ResourceVersion
	desired.Spec.ClusterIPs = current.Spec.ClusterIPs
	desired.Spec.IPFamilies = current.Spec.IPFamilies
	desired.Spec.IPFamilyPolicy = current.Spec.IPFamilyPolicy
	_, err = services.Update(context.Background(), desired, metav1.UpdateOptions{})
	return err
}

func (c *Controller) reconcileVpcEgressObserverServiceMonitor(gw *kubeovnv1.VpcEgressGateway, name string, selector map[string]string) error {
	if c.config.DynamicClient == nil {
		return errors.New("dynamic client is not configured")
	}
	labels := maps.Clone(gw.Spec.Observability.ServiceMonitor.Labels)
	if labels == nil {
		labels = map[string]string{}
	}
	maps.Copy(labels, selector)
	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "monitoring.coreos.com/v1", "kind": "ServiceMonitor",
		"metadata": map[string]any{"name": name, "namespace": gw.Namespace, "labels": stringMapAny(labels), "annotations": stringMapAny(gw.Spec.Observability.ServiceMonitor.Annotations)},
		"spec":     map[string]any{"selector": map[string]any{"matchLabels": stringMapAny(selector)}, "endpoints": []any{map[string]any{"port": "metrics", "path": "/metrics"}}},
	}}
	if err := util.SetOwnerReference(gw, object); err != nil {
		return err
	}
	client := c.config.DynamicClient.Resource(serviceMonitorGVR).Namespace(gw.Namespace)
	current, err := client.Get(context.Background(), name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.Create(context.Background(), object, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if !metav1.IsControlledBy(current, gw) {
		return fmt.Errorf("service monitor %s/%s is not controlled by this VpcEgressGateway", gw.Namespace, name)
	}
	object.SetResourceVersion(current.GetResourceVersion())
	_, err = client.Update(context.Background(), object, metav1.UpdateOptions{})
	return err
}

func (c *Controller) deleteVpcEgressGatewayObservabilityResources(gw *kubeovnv1.VpcEgressGateway, name string) {
	c.deleteOwnedConfigMap(gw, name)
	c.deleteOwnedService(gw, name)
	c.deleteOwnedServiceMonitor(gw, name)
}

func (c *Controller) deleteOwnedConfigMap(gw *kubeovnv1.VpcEgressGateway, name string) {
	client := c.config.KubeClient.CoreV1().ConfigMaps(gw.Namespace)
	object, err := client.Get(context.Background(), name, metav1.GetOptions{})
	if err == nil && metav1.IsControlledBy(object, gw) {
		_ = client.Delete(context.Background(), name, metav1.DeleteOptions{})
	}
}

func (c *Controller) deleteOwnedService(gw *kubeovnv1.VpcEgressGateway, name string) {
	client := c.config.KubeClient.CoreV1().Services(gw.Namespace)
	object, err := client.Get(context.Background(), name, metav1.GetOptions{})
	if err == nil && metav1.IsControlledBy(object, gw) {
		_ = client.Delete(context.Background(), name, metav1.DeleteOptions{})
	}
}

func (c *Controller) deleteOwnedServiceMonitor(gw *kubeovnv1.VpcEgressGateway, name string) {
	if c.config.DynamicClient == nil {
		return
	}
	client := c.config.DynamicClient.Resource(serviceMonitorGVR).Namespace(gw.Namespace)
	object, err := client.Get(context.Background(), name, metav1.GetOptions{})
	if err == nil && metav1.IsControlledBy(object, gw) {
		_ = client.Delete(context.Background(), name, metav1.DeleteOptions{})
	}
}

func addVpcEgressGatewayObserver(podSpec *corev1.PodSpec, image string, config *kubeovnv1.VpcEgressGatewayObservability, state vpcEgressObserverState) {
	if !state.enabled {
		return
	}
	resources := config.Resources
	if reflect.DeepEqual(resources, corev1.ResourceRequirements{}) {
		resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("20m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m"), corev1.ResourceMemory: resource.MustParse("256Mi")},
		}
	}
	launcher := fmt.Sprintf("if [ -x /kube-ovn/vpc-egress-gateway-observer ]; then exec /kube-ovn/vpc-egress-gateway-observer --config %s --network-status %s; fi; exec sleep infinity", vpcEgressObserverConfigPath, vpcEgressObserverNetworkStatusPath)
	podSpec.InitContainers = append(podSpec.InitContainers, corev1.Container{
		Name: vpcEgressObserverContainerName, Image: image, ImagePullPolicy: corev1.PullIfNotPresent,
		Command: []string{"/bin/sh", "-ec", launcher}, RestartPolicy: ptr.To(corev1.ContainerRestartPolicyAlways), Resources: resources,
		Env: []corev1.EnvVar{
			{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}},
			{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"}}},
		},
		Ports:         []corev1.ContainerPort{{Name: "metrics", ContainerPort: vpcEgressObserverPort}},
		LivenessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstrFromInt32(vpcEgressObserverPort)}}, PeriodSeconds: 10, FailureThreshold: 3},
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot: new(true), RunAsUser: ptr.To[int64](65534), RunAsGroup: ptr.To[int64](65534),
			AllowPrivilegeEscalation: new(false), ReadOnlyRootFilesystem: new(true),
			Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}, Add: []corev1.Capability{"NET_ADMIN"}},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: vpcEgressObserverConfigVolume, MountPath: "/etc/kube-ovn-observer", ReadOnly: true},
			{Name: vpcEgressObserverPodInfoVolume, MountPath: "/etc/podinfo", ReadOnly: true},
		},
	})
	podSpec.Volumes = append(
		podSpec.Volumes,
		corev1.Volume{Name: vpcEgressObserverConfigVolume, VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: state.configName}}}},
		corev1.Volume{Name: vpcEgressObserverPodInfoVolume, VolumeSource: corev1.VolumeSource{DownwardAPI: &corev1.DownwardAPIVolumeSource{Items: []corev1.DownwardAPIVolumeFile{{Path: "network-status", FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.annotations['k8s.v1.cni.cncf.io/network-status']"}}}}}},
	)
}

func intstrFromInt32(value int32) intstr.IntOrString {
	return intstr.FromInt32(value)
}

func stringMapAny(values map[string]string) map[string]any {
	if values == nil {
		return nil
	}
	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
