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
		Name: "probe", Namespace: "default", UID: "uid-probe",
		Spec: corev1.PodSpec{Containers: []corev1.Container{
			{
				Ports: []corev1.ContainerPort{{Name: "tcp-port", ContainerPort: 8080}},
				ReadinessProbe: &corev1.Probe{
					TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString("tcp-port")},
				},
				LivenessProbe: &corev1.Probe{
					HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromInt32(8081)},
				},
			},
			{
				ReadinessProbe: &corev1.Probe{
					GRPC: &corev1.GRPCAction{Port: 8082},
				},
				LivenessProbe: &corev1.Probe{
					TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(8083)},
				},
			},
			{
				ReadinessProbe: &corev1.Probe{
					TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(8081)},
				},
				LivenessProbe: &corev1.Probe{
					HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromInt32(8084)},
				},
			},
		}},
	}
	const podIP = "10.30.0.2"
	for _, port := range []int32{8080, 8081, 8083} {
		key := util.JoinHostPort(podIP, port)
		t.Cleanup(func() { customVPCPodTCPProbeIPPort.Delete(key) })
	}

	allPorts, tcpPorts := getProbePorts(pod)
	require.Equal(t, []int32{8080, 8081, 8082, 8083, 8084}, allPorts.SortedList())
	require.Equal(t, []int32{8080, 8081, 8083}, tcpPorts.SortedList())
	require.Equal(t, []int32{8082, 8084}, getTProxyRulePorts(pod, podIP).SortedList())

	customVPCPodTCPProbeIPPort.Store(util.JoinHostPort(podIP, 8080), tcpProbeResult{podUID: pod.UID, reachable: true})
	customVPCPodTCPProbeIPPort.Store(util.JoinHostPort(podIP, 8081), tcpProbeResult{podUID: pod.UID, reachable: true})
	customVPCPodTCPProbeIPPort.Store(util.JoinHostPort(podIP, 8083), tcpProbeResult{podUID: pod.UID})
	require.Equal(t, []int32{8080, 8081, 8082, 8084}, getTProxyRulePorts(pod, podIP).SortedList())

	customVPCPodTCPProbeIPPort.Store(util.JoinHostPort(podIP, 8080), tcpProbeResult{podUID: pod.UID})
	customVPCPodTCPProbeIPPort.Store(util.JoinHostPort(podIP, 8081), tcpProbeResult{podUID: pod.UID})
	customVPCPodTCPProbeIPPort.Store(util.JoinHostPort(podIP, 8083), tcpProbeResult{podUID: pod.UID, reachable: true})
	require.Equal(t, []int32{8082, 8083, 8084}, getTProxyRulePorts(pod, podIP).SortedList())
	require.Equal(t, []int32{8082, 8084}, getTProxyRulePorts(pod, "10.30.0.3").SortedList())

	oldUID := pod.UID
	pod.UID = "uid-recreated"
	require.Equal(t, []int32{8082, 8084}, getTProxyRulePorts(pod, podIP).SortedList())
	pod.UID = oldUID
}
