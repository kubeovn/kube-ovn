package daemon

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestGetTProxyRulePorts(t *testing.T) {
	pod := &corev1.Pod{
		UID: "uid-probe",
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Ports: []corev1.ContainerPort{{Name: "startup-port", ContainerPort: 8085}},
			StartupProbe: &corev1.Probe{
				HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromString("startup-port")},
			},
			ReadinessProbe: &corev1.Probe{
				TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(8080)},
			},
			LivenessProbe: &corev1.Probe{
				HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromInt32(8081)},
			},
		}}},
	}
	const podIP = "10.30.0.2"
	t.Cleanup(func() {
		customVPCPodIPToNs.Delete(podIP)
		customVPCPodTCPProbeIPPort.Delete(util.JoinHostPort(podIP, 8080))
	})

	customVPCPodIPToNs.Store(podIP, podNetworkNamespace{podUID: "uid-old", namespace: "old-ns"})
	require.Empty(t, getTProxyRulePorts(pod, podIP))

	customVPCPodIPToNs.Store(podIP, podNetworkNamespace{podUID: pod.UID, namespace: "pod-ns"})
	require.Equal(t, []int32{8081, 8085}, getTProxyRulePorts(pod, podIP).SortedList())

	customVPCPodTCPProbeIPPort.Store(util.JoinHostPort(podIP, 8080), tcpProbeResult{podUID: pod.UID, reachable: true})
	require.Equal(t, []int32{8080, 8081, 8085}, getTProxyRulePorts(pod, podIP).SortedList())

	pod.UID = "uid-recreated"
	require.Empty(t, getTProxyRulePorts(pod, podIP))
}

func TestGetTProxyRulePortsForNativeSidecar(t *testing.T) {
	const podIP = "10.30.0.42"
	sidecarTCP := util.JoinHostPort(podIP, 9101)
	t.Cleanup(func() { customVPCPodTCPProbeIPPort.Delete(sidecarTCP) })
	pod := &corev1.Pod{
		Name: "native-sidecar", Namespace: "default", UID: "uid-native-sidecar",
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					ReadinessProbe: &corev1.Probe{
						HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromInt32(8080)},
					},
				},
			},
			InitContainers: []corev1.Container{
				{
					Name:  "setup",
					Ports: []corev1.ContainerPort{{Name: "setup-port", ContainerPort: 9000}},
				},
				{
					Name: "proxy", RestartPolicy: new(corev1.ContainerRestartPolicyAlways),
					Ports: []corev1.ContainerPort{{Name: "health", ContainerPort: 9100}},
					StartupProbe: &corev1.Probe{
						HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromString("health")},
					},
					LivenessProbe: &corev1.Probe{
						TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(9101)},
					},
					ReadinessProbe: &corev1.Probe{
						GRPC: &corev1.GRPCAction{Port: 9102},
					},
				},
			},
		},
	}

	require.Equal(t, []int32{8080, 9100, 9102}, getTProxyRulePorts(pod, podIP).SortedList())
	_, tcpPorts := getProbePorts(pod)
	require.Equal(t, []int32{9101}, tcpPorts.SortedList())

	customVPCPodTCPProbeIPPort.Store(sidecarTCP, tcpProbeResult{podUID: pod.UID, reachable: true})
	require.Equal(t, []int32{8080, 9100, 9101, 9102}, getTProxyRulePorts(pod, podIP).SortedList())
	require.Equal(t, []int32{8080, 9100, 9102}, getTProxyRulePorts(pod, "fd00::42").SortedList())
}
