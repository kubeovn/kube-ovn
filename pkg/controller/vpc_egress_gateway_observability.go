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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilversion "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/pkg/vegobserver"
)

const (
	vpcEgressObserverContainerName           = "observability"
	vpcEgressObserverConfigVolume            = "observability-config"
	vpcEgressObserverPodInfoVolume           = "observability-podinfo"
	vpcEgressObserverConfigPath              = "/etc/kube-ovn-observer/config.json"
	vpcEgressObserverNetworkStatusPath       = "/etc/podinfo/network-status"
	vpcEgressObserverPort              int32 = 10666
)

type restartableInitContainerCapability uint8

const (
	restartableInitContainerCapabilityUnknown restartableInitContainerCapability = iota
	restartableInitContainerCapabilityUnsupported
	restartableInitContainerCapabilitySupported
)

type restartableInitContainerSupport struct {
	capability restartableInitContainerCapability
	reason     string
	message    string
}

func (s restartableInitContainerSupport) supported() bool {
	return s.capability == restartableInitContainerCapabilitySupported
}

func (s restartableInitContainerSupport) definitive() bool {
	return s.capability != restartableInitContainerCapabilityUnknown
}

type vpcEgressObserverState struct {
	enabled            bool
	configName         string
	preservedContainer *corev1.Container
	preservedVolumes   []corev1.Volume
}

func observabilityEnabled(config *kubeovnv1.VpcEgressGatewayObservability) bool {
	return config != nil && (config.InterfaceMetrics.Enabled || config.Conntrack.Metrics.Enabled || config.Conntrack.Log.Enabled)
}

func (c *Controller) supportsRestartableInitContainers() restartableInitContainerSupport {
	c.restartableInitContainersMu.Lock()
	defer c.restartableInitContainersMu.Unlock()
	if c.restartableInitContainerSupport.definitive() {
		return c.restartableInitContainerSupport
	}

	serverVersion, err := c.config.KubeClient.Discovery().ServerVersion()
	if err != nil {
		return restartableInitContainerSupport{reason: "KubernetesVersionUnknown", message: fmt.Errorf("discover Kubernetes version: %w", err).Error()}
	}
	version, err := utilversion.ParseSemantic(serverVersion.GitVersion)
	if err != nil {
		return restartableInitContainerSupport{reason: "KubernetesVersionUnknown", message: fmt.Errorf("parse Kubernetes version %q: %w", serverVersion.GitVersion, err).Error()}
	}
	if !version.AtLeast(utilversion.MustParseSemantic("1.29.0")) {
		c.restartableInitContainerSupport = restartableInitContainerSupport{
			capability: restartableInitContainerCapabilityUnsupported,
			reason:     "UnsupportedKubernetesVersion",
			message:    "restartable init containers require Kubernetes 1.29 or later",
		}
		return c.restartableInitContainerSupport
	}

	namespace := c.config.PodNamespace
	if namespace == "" {
		namespace = metav1.NamespaceDefault
	}
	image := c.config.Image
	if image == "" {
		image = "kubeovn/sidecar-capability-probe"
	}
	labels := map[string]string{"app": "kube-ovn-sidecar-capability-probe"}
	probe := &appsv1.Deployment{
		GenerateName: "kube-ovn-sidecar-capability-probe-", Namespace: namespace,
		Spec: appsv1.DeploymentSpec{
			Replicas: new(int32(0)), Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				Labels: labels,
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{Name: "sidecar", Image: image, RestartPolicy: new(corev1.ContainerRestartPolicyAlways)}},
					Containers:     []corev1.Container{{Name: "main", Image: image}},
				},
			},
		},
	}
	created, err := c.config.KubeClient.AppsV1().Deployments(namespace).Create(context.Background(), probe, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	if err != nil {
		return restartableInitContainerSupport{reason: "SidecarContainersCapabilityUnknown", message: fmt.Errorf("probe restartable init container support: %w", err).Error()}
	}
	if len(created.Spec.Template.Spec.InitContainers) != 1 || created.Spec.Template.Spec.InitContainers[0].RestartPolicy == nil || *created.Spec.Template.Spec.InitContainers[0].RestartPolicy != corev1.ContainerRestartPolicyAlways {
		c.restartableInitContainerSupport = restartableInitContainerSupport{
			capability: restartableInitContainerCapabilityUnsupported,
			reason:     "SidecarContainersDisabled",
			message:    "the Kubernetes API server dropped the restartable init container policy; enable the SidecarContainers feature gate",
		}
		return c.restartableInitContainerSupport
	}
	c.restartableInitContainerSupport = restartableInitContainerSupport{capability: restartableInitContainerCapabilitySupported}
	return c.restartableInitContainerSupport
}

func (c *Controller) reconcileVpcEgressGatewayObservability(gw *kubeovnv1.VpcEgressGateway, externalNetwork string, labels map[string]string) vpcEgressObserverState {
	resourceName := vpcEgressObserverResourceName(gw)
	gw.Status.Conditions.RemoveCondition(kubeovnv1.ServiceMonitorReady)
	if !observabilityEnabled(gw.Spec.Observability) {
		c.deleteOwnedVpcEgressObserverConfigMap(gw, resourceName)
		gw.Status.Conditions.RemoveCondition(kubeovnv1.ObservabilityConfigured)
		return vpcEgressObserverState{}
	}
	preservedState := c.currentVpcEgressObserverState(gw, resourceName)

	support := c.supportsRestartableInitContainers()
	if !support.supported() {
		c.setVpcEgressGatewayObservabilityCondition(gw, kubeovnv1.ObservabilityConfigured, corev1.ConditionFalse, support.reason, support.message)
		if !support.definitive() {
			c.addOrUpdateVpcEgressGatewayQueue.AddAfter(fmt.Sprintf("%s/%s", gw.Namespace, gw.Name), 30*time.Second)
			return preservedState
		}
		c.deleteOwnedVpcEgressObserverConfigMap(gw, resourceName)
		return vpcEgressObserverState{}
	}

	observerConfig := vegobserver.Config{Namespace: gw.Namespace, Name: gw.Name, ExternalNetwork: externalNetwork, Observability: *gw.Spec.Observability.DeepCopy()}
	data, err := json.Marshal(observerConfig)
	if err != nil {
		c.setVpcEgressGatewayObservabilityCondition(gw, kubeovnv1.ObservabilityConfigured, corev1.ConditionFalse, "InvalidConfiguration", err.Error())
		return preservedState
	}
	if err := c.reconcileVpcEgressObserverConfigMap(gw, resourceName, labels, data); err != nil {
		c.setVpcEgressGatewayObservabilityCondition(gw, kubeovnv1.ObservabilityConfigured, corev1.ConditionFalse, "ConfigMapReconcileFailed", err.Error())
		return preservedState
	}
	c.setVpcEgressGatewayObservabilityCondition(gw, kubeovnv1.ObservabilityConfigured, corev1.ConditionTrue, "Configured", "")
	return vpcEgressObserverState{enabled: true, configName: resourceName}
}

func (c *Controller) currentVpcEgressObserverState(gw *kubeovnv1.VpcEgressGateway, resourceName string) vpcEgressObserverState {
	if c.deploymentsLister == nil {
		return vpcEgressObserverState{}
	}
	deployment, err := c.deploymentsLister.Deployments(gw.Namespace).Get(gw.Spec.Prefix + gw.Name)
	if k8serrors.IsNotFound(err) {
		return vpcEgressObserverState{}
	}
	if err != nil {
		klog.ErrorS(err, "Failed to get the current VpcEgressGateway Deployment while preserving observability", "namespace", gw.Namespace, "name", gw.Name)
		return vpcEgressObserverState{}
	}

	state := vpcEgressObserverState{configName: resourceName}
	for i := range deployment.Spec.Template.Spec.InitContainers {
		container := &deployment.Spec.Template.Spec.InitContainers[i]
		if container.Name == vpcEgressObserverContainerName {
			state.enabled = true
			state.preservedContainer = container.DeepCopy()
			break
		}
	}
	if !state.enabled {
		return vpcEgressObserverState{}
	}
	for i := range deployment.Spec.Template.Spec.Volumes {
		volume := &deployment.Spec.Template.Spec.Volumes[i]
		if volume.Name != vpcEgressObserverConfigVolume && volume.Name != vpcEgressObserverPodInfoVolume {
			continue
		}
		state.preservedVolumes = append(state.preservedVolumes, *volume.DeepCopy())
		if volume.Name == vpcEgressObserverConfigVolume && volume.ConfigMap != nil {
			state.configName = volume.ConfigMap.Name
		}
	}
	return state
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

func setVpcEgressGatewayControllerReference(gw *kubeovnv1.VpcEgressGateway, object metav1.Object) error {
	if err := util.SetControllerReference(gw, object); err != nil {
		return err
	}
	ownerReferences := object.GetOwnerReferences()
	for i := range ownerReferences {
		if ownerReferences[i].UID == gw.UID && ownerReferences[i].Controller != nil && *ownerReferences[i].Controller {
			ownerReferences[i].BlockOwnerDeletion = nil
			object.SetOwnerReferences(ownerReferences)
			return nil
		}
	}
	return errors.New("vpc egress gateway controller reference was not set")
}

func (c *Controller) reconcileVpcEgressObserverConfigMap(gw *kubeovnv1.VpcEgressGateway, name string, labels map[string]string, data []byte) error {
	configMaps := c.config.KubeClient.CoreV1().ConfigMaps(gw.Namespace)
	desired := &corev1.ConfigMap{Name: name, Namespace: gw.Namespace, Labels: maps.Clone(labels), Data: map[string]string{"config.json": string(data)}}
	if err := setVpcEgressGatewayControllerReference(gw, desired); err != nil {
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

func (c *Controller) deleteOwnedVpcEgressObserverConfigMap(gw *kubeovnv1.VpcEgressGateway, name string) {
	client := c.config.KubeClient.CoreV1().ConfigMaps(gw.Namespace)
	configMap, err := client.Get(context.Background(), name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return
	}
	if err != nil {
		klog.ErrorS(err, "Failed to get VpcEgressGateway observability ConfigMap", "namespace", gw.Namespace, "name", name)
		return
	}
	if metav1.IsControlledBy(configMap, gw) {
		opts := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &configMap.UID}}
		if err := client.Delete(context.Background(), name, opts); err != nil && !k8serrors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to delete VpcEgressGateway observability ConfigMap", "namespace", gw.Namespace, "name", name)
		}
	}
}

func addVpcEgressGatewayObserver(podSpec *corev1.PodSpec, image string, config *kubeovnv1.VpcEgressGatewayObservability, state vpcEgressObserverState) {
	if !state.enabled {
		return
	}
	if state.preservedContainer != nil {
		podSpec.InitContainers = append(podSpec.InitContainers, *state.preservedContainer.DeepCopy())
		for i := range state.preservedVolumes {
			podSpec.Volumes = append(podSpec.Volumes, *state.preservedVolumes[i].DeepCopy())
		}
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
	healthCheck := "if [ ! -x /kube-ovn/vpc-egress-gateway-observer ]; then exit 0; fi; exec /kube-ovn/vpc-egress-gateway-observer --health-check"
	livenessProbe := &corev1.Probe{Exec: &corev1.ExecAction{Command: []string{"/bin/sh", "-ec", healthCheck}}, PeriodSeconds: 10, FailureThreshold: 3}
	podSpec.InitContainers = append(podSpec.InitContainers, corev1.Container{
		Name: vpcEgressObserverContainerName, Image: image, ImagePullPolicy: corev1.PullIfNotPresent,
		Command: []string{"/bin/sh", "-ec", launcher}, RestartPolicy: new(corev1.ContainerRestartPolicyAlways), Resources: resources,
		Env: []corev1.EnvVar{
			{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}},
			{Name: "NODE_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"}}},
		},
		Ports:         []corev1.ContainerPort{{Name: "metrics", ContainerPort: vpcEgressObserverPort}},
		LivenessProbe: livenessProbe,
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot: new(true), RunAsUser: new(int64(65534)), RunAsGroup: new(int64(65534)),
			AllowPrivilegeEscalation: new(true), ReadOnlyRootFilesystem: new(true),
			Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}, Add: []corev1.Capability{"NET_ADMIN"}},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: vpcEgressObserverConfigVolume, MountPath: "/etc/kube-ovn-observer", ReadOnly: true},
			{Name: vpcEgressObserverPodInfoVolume, MountPath: "/etc/podinfo", ReadOnly: true},
		},
	})
	podSpec.Volumes = append(
		podSpec.Volumes,
		corev1.Volume{Name: vpcEgressObserverConfigVolume, ConfigMap: &corev1.ConfigMapVolumeSource{Name: state.configName}},
		corev1.Volume{Name: vpcEgressObserverPodInfoVolume, DownwardAPI: &corev1.DownwardAPIVolumeSource{Items: []corev1.DownwardAPIVolumeFile{{Path: "network-status", FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.annotations['k8s.v1.cni.cncf.io/network-status']"}}}}},
	)
}
