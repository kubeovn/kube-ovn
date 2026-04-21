package controller

import (
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

var (
	// Default resource requirements for gateway containers
	gwSleepResourceCPU         = resource.MustParse("10m")
	gwSleepResourceMemory      = resource.MustParse("10Mi")
	gwBFDDResourceCPU          = resource.MustParse("50m")
	gwBFDDResourceMemory       = resource.MustParse("50Mi")
	gwResourceEphemeralStorage = resource.MustParse("1Gi")
)

// genGatewayBFDDContainer creates a BFD daemon container for VPC gateways (both Egress and NAT).
// The container runs OpenBFDD to establish BFD sessions with the VPC's BFD port for health monitoring.
//
// Parameters:
//   - image: Container image to use
//   - bfdIP: IP address(es) of the BFD peer (VPC BFD port), comma-separated for dual-stack
//   - minTX: BFD minimum transmit interval in milliseconds
//   - minRX: BFD minimum receive interval in milliseconds
//   - multiplier: BFD detection multiplier
//
// Returns a container specification ready to be added to a pod template.
func genGatewayBFDDContainer(image, bfdIP string, minTX, minRX, multiplier int32) corev1.Container {
	return corev1.Container{
		Name:            "bfdd",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"bash", "/kube-ovn/start-bfdd.sh"},
		Env: []corev1.EnvVar{
			{
				Name: "POD_IPS",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "status.podIPs",
					},
				},
			},
			{
				Name:  "BFD_PEER_IPS",
				Value: bfdIP,
			},
			{
				Name:  "BFD_MIN_TX",
				Value: strconv.Itoa(int(minTX)),
			},
			{
				Name:  "BFD_MIN_RX",
				Value: strconv.Itoa(int(minRX)),
			},
			{
				Name:  "BFD_MULTI",
				Value: strconv.Itoa(int(multiplier)),
			},
		},
		// Wait for the BFD process to be running and initialize the BFD configuration
		StartupProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"bash", "/kube-ovn/bfdd-prestart.sh"},
				},
			},
			InitialDelaySeconds: 1,
			FailureThreshold:    1,
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"bfdd-control", "status"},
				},
			},
			InitialDelaySeconds: 1,
			PeriodSeconds:       5,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"bfdd-control", "status"},
				},
			},
			InitialDelaySeconds: 3,
			PeriodSeconds:       3,
			FailureThreshold:    1,
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    gwBFDDResourceCPU,
				corev1.ResourceMemory: gwBFDDResourceMemory,
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:              gwBFDDResourceCPU,
				corev1.ResourceMemory:           gwBFDDResourceMemory,
				corev1.ResourceEphemeralStorage: gwResourceEphemeralStorage,
			},
		},
		SecurityContext: &corev1.SecurityContext{
			Privileged: new(false),
			RunAsUser:  ptr.To[int64](65534),
			Capabilities: &corev1.Capabilities{
				Add:  []corev1.Capability{"NET_ADMIN", "NET_BIND_SERVICE", "NET_RAW"},
				Drop: []corev1.Capability{"ALL"},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "usr-local-sbin",
				MountPath: "/usr/local/sbin",
			},
		},
	}
}

// genGatewaySleepContainer creates a minimal sleep container for gateways.
// This container runs indefinitely and is used as the main container when the gateway
// only needs to run BFD or other sidecar containers.
func genGatewaySleepContainer(image string) corev1.Container {
	return corev1.Container{
		Name:            "gateway",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"sleep", "infinity"},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    gwSleepResourceCPU,
				corev1.ResourceMemory: gwSleepResourceMemory,
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:              gwSleepResourceCPU,
				corev1.ResourceMemory:           gwSleepResourceMemory,
				corev1.ResourceEphemeralStorage: gwResourceEphemeralStorage,
			},
		},
		SecurityContext: &corev1.SecurityContext{
			Privileged: new(false),
			RunAsUser:  ptr.To[int64](65534),
			Capabilities: &corev1.Capabilities{
				Add:  []corev1.Capability{"NET_ADMIN", "NET_RAW"},
				Drop: []corev1.Capability{"ALL"},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "usr-local-sbin",
				MountPath: "/usr/local/sbin",
			},
		},
	}
}

// GatewayBFDConfig represents common BFD configuration shared by VPC gateways.
// This interface allows both VpcEgressGateway and VpcNatGateway to use shared BFD logic.
type GatewayBFDConfig interface {
	IsEnabled() bool
	GetMinRX() int32
	GetMinTX() int32
	GetMultiplier() int32
}

// genGatewayPodAntiAffinity creates pod anti-affinity rules to ensure gateway instances
// run on different nodes. This is essential for HA deployments.
func genGatewayPodAntiAffinity(labels map[string]string) *corev1.Affinity {
	return &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: labels,
				},
				TopologyKey: corev1.LabelHostname,
			}},
		},
	}
}

// genGatewayDeploymentStrategy creates the standard rolling update strategy for gateway deployments.
// MaxUnavailable=1 ensures only one instance is updated at a time.
// MaxSurge=0 ensures no extra instances are created during updates.
func genGatewayDeploymentStrategy() appsv1.DeploymentStrategy {
	return appsv1.DeploymentStrategy{
		Type: appsv1.RollingUpdateDeploymentStrategyType,
		RollingUpdate: &appsv1.RollingUpdateDeployment{
			MaxUnavailable: new(intstr.FromInt(1)),
			MaxSurge:       new(intstr.FromInt(0)),
		},
	}
}

// mergeGatewayAffinity merges multiple affinity configurations into one.
// Later affinities take precedence over earlier ones.
func mergeGatewayAffinity(affinities ...*corev1.Affinity) *corev1.Affinity {
	result := &corev1.Affinity{}
	for _, aff := range affinities {
		if aff == nil {
			continue
		}
		if aff.NodeAffinity != nil {
			result.NodeAffinity = aff.NodeAffinity
		}
		if aff.PodAffinity != nil {
			result.PodAffinity = aff.PodAffinity
		}
		if aff.PodAntiAffinity != nil {
			result.PodAntiAffinity = aff.PodAntiAffinity
		}
	}
	return result
}
