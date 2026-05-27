package controller

import (
	"strings"
	"testing"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/set"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestAppendVpcEgressGatewayPodStatusUsesUniqueWorkloadNodes(t *testing.T) {
	gw := &kubeovnv1.VpcEgressGateway{}
	nodeNexthopIPv4 := map[string]string{}
	nodeNexthopIPv6 := map[string]string{}
	workloadNodes := set.New[string]()

	pods := []*corev1.Pod{
		makeVpcEgressGatewayPod("pod-1", "node-a", []string{"10.16.0.10"}, []string{"172.17.0.10"}),
		makeVpcEgressGatewayPod("pod-2", "node-a", []string{"10.16.0.11"}, []string{"172.17.0.11"}),
		makeVpcEgressGatewayPod("pod-3", "node-b", []string{"10.16.0.12"}, []string{"172.17.0.12"}),
		makeVpcEgressGatewayPodWithLegacyPodIP("pod-4", "node-b", "10.16.0.13", []string{"172.17.0.13"}),
	}
	for _, pod := range pods {
		require.NoError(t, appendVpcEgressGatewayPodStatus(gw, pod, "default/macvlan", nodeNexthopIPv4, nodeNexthopIPv6, workloadNodes))
	}

	require.Equal(t, []string{"node-a", "node-b"}, workloadNodes.SortedList())
	require.Equal(t, []string{"10.16.0.10", "10.16.0.11", "10.16.0.12", "10.16.0.13"}, gw.Status.InternalIPs)
	require.Equal(t, []string{"172.17.0.10", "172.17.0.11", "172.17.0.12", "172.17.0.13"}, gw.Status.ExternalIPs)
	require.Equal(t, map[string]string{
		"node-a": "10.16.0.11",
		"node-b": "10.16.0.13",
	}, nodeNexthopIPv4)
}

func makeVpcEgressGatewayPod(name, node string, podIPs, attachmentIPs []string) *corev1.Pod {
	statusIPs := make([]corev1.PodIP, 0, len(podIPs))
	for _, ip := range podIPs {
		statusIPs = append(statusIPs, corev1.PodIP{IP: ip})
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
			Annotations: map[string]string{
				nadv1.NetworkStatusAnnot: `[{"name": "default/macvlan", "ips": [` + quoteJSONStrings(attachmentIPs) + `]}]`,
			},
		},
		Spec: corev1.PodSpec{
			NodeName: node,
		},
		Status: corev1.PodStatus{
			PodIPs: statusIPs,
		},
	}
}

func makeVpcEgressGatewayPodWithLegacyPodIP(name, node, podIP string, attachmentIPs []string) *corev1.Pod {
	pod := makeVpcEgressGatewayPod(name, node, nil, attachmentIPs)
	pod.Status.PodIP = podIP
	return pod
}

func quoteJSONStrings(values []string) string {
	var builder strings.Builder
	for i, value := range values {
		if i != 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(`"` + value + `"`)
	}
	return builder.String()
}
