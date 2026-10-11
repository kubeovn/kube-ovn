package controller

import (
	"errors"
	"maps"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
)

// Kubernetes defaults these only on Pods, not on DaemonSet templates. Apply
// their exact semantics to copies; do not ignore changed resources or ports.
func ipsecPodContainerDefaults(container corev1.Container, hostNetwork bool) corev1.Container {
	c := container.DeepCopy()
	if c.Resources.Limits != nil {
		c.Resources.Requests = maps.Clone(c.Resources.Requests)
		if c.Resources.Requests == nil {
			c.Resources.Requests = make(corev1.ResourceList)
		}
		for key, value := range c.Resources.Limits {
			if _, exists := c.Resources.Requests[key]; !exists {
				c.Resources.Requests[key] = value.DeepCopy()
			}
		}
	}
	if hostNetwork {
		for i := range c.Ports {
			if c.Ports[i].HostPort == 0 {
				c.Ports[i].HostPort = c.Ports[i].ContainerPort
			}
		}
	}
	defaultHTTP := func(action *corev1.HTTPGetAction) {
		if action != nil && action.Protocol == nil {
			action.Protocol = new(corev1.HTTPProtocolHTTP1)
		}
	}
	for _, probe := range []*corev1.Probe{c.StartupProbe, c.LivenessProbe, c.ReadinessProbe} {
		if probe != nil {
			defaultHTTP(probe.HTTPGet)
		}
	}
	if c.Lifecycle != nil {
		for _, handler := range []*corev1.LifecycleHandler{c.Lifecycle.PostStart, c.Lifecycle.PreStop} {
			if handler != nil {
				defaultHTTP(handler.HTTPGet)
			}
		}
	}
	return *c
}

func withoutIPsecTokenMounts(container corev1.Container, tokenVolume string) corev1.Container {
	container.VolumeMounts = slices.DeleteFunc(slices.Clone(container.VolumeMounts), func(mount corev1.VolumeMount) bool {
		return tokenVolume != "" && equality.Semantic.DeepEqual(mount, corev1.VolumeMount{Name: tokenVolume, MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true})
	})
	return container
}

func isIPsecAPITokenVolume(volume corev1.Volume) bool {
	if !strings.HasPrefix(volume.Name, "kube-api-access-") || volume.Projected == nil || len(volume.Projected.Sources) != 3 {
		return false
	}
	// Only the standard admission projection is exempt. A name prefix plus
	// one token source must not hide an extra Secret, path or writable source.
	token := volume.Projected.Sources[0].ServiceAccountToken
	if token == nil || token.ExpirationSeconds == nil || *token.ExpirationSeconds < 600 {
		return false
	}
	expected := corev1.Volume{Name: volume.Name, Projected: &corev1.ProjectedVolumeSource{
		DefaultMode: new(int32(0o644)), Sources: []corev1.VolumeProjection{
			{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{Path: "token", ExpirationSeconds: token.ExpirationSeconds}},
			{ConfigMap: &corev1.ConfigMapProjection{Name: "kube-root-ca.crt", Items: []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}}}},
			{DownwardAPI: &corev1.DownwardAPIProjection{Items: []corev1.DownwardAPIVolumeFile{{Path: "namespace", FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.namespace"}}}}},
		},
	}}
	return equality.Semantic.DeepEqual(volume, expected)
}

// Compare every container and private volume, not just the receipt producer.
// A matching init/container name must not authorize an injected sibling that
// can read the attestation key, or a changed hostPath behind the same mount.
// Ignore only the API's read-only projected service-account token injection.
func verifyIPsecPodTemplate(pod *corev1.Pod, ds *appsv1.DaemonSet) error {
	expected, actual := ds.Spec.Template.Spec, pod.Spec
	if len(actual.Containers) != len(expected.Containers) || len(actual.InitContainers) != len(expected.InitContainers) || len(actual.EphemeralContainers) != 0 ||
		actual.HostNetwork != expected.HostNetwork || actual.HostPID != expected.HostPID || actual.HostIPC != expected.HostIPC || !equality.Semantic.DeepEqual(actual.SecurityContext, expected.SecurityContext) || !equality.Semantic.DeepEqual(actual.RuntimeClassName, expected.RuntimeClassName) || !equality.Semantic.DeepEqual(actual.HostUsers, expected.HostUsers) {
		return errors.New("IPsec receipt Pod does not match the frozen execution context")
	}
	volumes := slices.Clone(actual.Volumes)
	tokenVolume := ""
	for i, volume := range volumes {
		if slices.ContainsFunc(expected.Volumes, func(v corev1.Volume) bool { return v.Name == volume.Name }) || !isIPsecAPITokenVolume(volume) {
			continue
		}
		tokenVolume = volume.Name
		volumes = slices.Delete(volumes, i, i+1)
		break
	}
	if !equality.Semantic.DeepEqual(volumes, expected.Volumes) {
		return errors.New("IPsec receipt Pod has a changed private or shared volume")
	}
	for i, container := range actual.Containers {
		if !equality.Semantic.DeepEqual(ipsecPodContainerDefaults(withoutIPsecTokenMounts(container, tokenVolume), actual.HostNetwork), ipsecPodContainerDefaults(expected.Containers[i], expected.HostNetwork)) {
			return errors.New("IPsec receipt Pod has a changed ordinary container")
		}
	}
	for i, container := range actual.InitContainers {
		if !equality.Semantic.DeepEqual(ipsecPodContainerDefaults(withoutIPsecTokenMounts(container, tokenVolume), actual.HostNetwork), ipsecPodContainerDefaults(expected.InitContainers[i], expected.HostNetwork)) {
			return errors.New("IPsec receipt Pod has a changed init container")
		}
	}
	return nil
}
