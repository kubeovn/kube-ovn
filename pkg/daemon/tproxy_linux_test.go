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
	require.Equal(t, []int32{8081}, getTProxyRulePorts(pod, podIP).SortedList())

	customVPCPodTCPProbeIPPort.Store(util.JoinHostPort(podIP, 8080), tcpProbeResult{podUID: pod.UID, reachable: true})
	require.Equal(t, []int32{8080, 8081}, getTProxyRulePorts(pod, podIP).SortedList())

	pod.UID = "uid-recreated"
	require.Empty(t, getTProxyRulePorts(pod, podIP))
}
