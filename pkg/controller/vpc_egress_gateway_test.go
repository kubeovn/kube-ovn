package controller

import (
	"testing"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
)

func TestVpcEgressGatewayContainerBFDDDefaultResources(t *testing.T) {
	container := genGatewayBFDDContainer("kube-ovn", "10.255.255.255", 100, 100, 5)

	require.Equal(t, "200m", container.Resources.Requests.Cpu().String())
	require.Equal(t, "200m", container.Resources.Limits.Cpu().String())
	require.Equal(t, "50Mi", container.Resources.Requests.Memory().String())
	require.Equal(t, "50Mi", container.Resources.Limits.Memory().String())
	ephemeralStorage := container.Resources.Limits[corev1.ResourceEphemeralStorage]
	require.Equal(t, "1Gi", ephemeralStorage.String())
}

func newVegWorkloadPod(name, node, podIP, attachment string) *corev1.Pod {
	annotations := map[string]string{}
	if attachment != "" {
		annotations[nadv1.NetworkStatusAnnot] = attachment
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			NodeName: node,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIPs: []corev1.PodIP{{
				IP: podIP,
			}},
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
}

func TestCollectVpcEgressGatewayWorkloadStatus(t *testing.T) {
	attachmentNetwork := "default/eth1"
	readyAttachment := `[{"name":"default/eth1","ips":["172.17.1.10"]}]`

	tests := []struct {
		name              string
		pods              []*corev1.Pod
		wantInternalIPs   []string
		wantExternalIPs   []string
		wantNodes         []string
		wantNotReadyCount int
	}{
		{
			name: "all workload pods have attachment network",
			pods: []*corev1.Pod{
				newVegWorkloadPod("veg-1", "node-1", "10.16.1.10", readyAttachment),
				newVegWorkloadPod("veg-2", "node-2", "10.16.1.11", `[{"name":"default/eth1","ips":["172.17.1.11"]}]`),
			},
			wantInternalIPs: []string{"10.16.1.10", "10.16.1.11"},
			wantExternalIPs: []string{"172.17.1.10", "172.17.1.11"},
			wantNodes:       []string{"node-1", "node-2"},
		},
		{
			name: "one workload pod misses attachment network",
			pods: []*corev1.Pod{
				newVegWorkloadPod("veg-1", "node-1", "10.16.1.10", readyAttachment),
				newVegWorkloadPod("veg-2", "node-2", "10.16.1.11", `[{"name":"kube-ovn","ips":["10.16.1.11"]}]`),
			},
			wantInternalIPs:   []string{"10.16.1.10"},
			wantExternalIPs:   []string{"172.17.1.10"},
			wantNodes:         []string{"node-1"},
			wantNotReadyCount: 2,
		},
		{
			name: "one workload pod has attachment network without ip",
			pods: []*corev1.Pod{
				newVegWorkloadPod("veg-1", "node-1", "10.16.1.10", readyAttachment),
				newVegWorkloadPod("veg-2", "node-2", "10.16.1.11", `[{"name":"default/eth1","ips":[]}]`),
			},
			wantInternalIPs:   []string{"10.16.1.10"},
			wantExternalIPs:   []string{"172.17.1.10"},
			wantNodes:         []string{"node-1"},
			wantNotReadyCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := &kubeovnv1.VpcEgressGateway{
				Spec: kubeovnv1.VpcEgressGatewaySpec{
					Replicas: 2,
				},
			}

			_, _, messages := collectVpcEgressGatewayWorkloadStatus(gw, tt.pods, attachmentNetwork)

			require.Equal(t, tt.wantInternalIPs, gw.Status.InternalIPs)
			require.Equal(t, tt.wantExternalIPs, gw.Status.ExternalIPs)
			require.Equal(t, tt.wantNodes, gw.Status.Workload.Nodes)
			require.Len(t, messages, tt.wantNotReadyCount)
		})
	}
}

func TestVegBFDSessionStatuses(t *testing.T) {
	up := ovnnb.BFDStatusUp
	oldTransition := metav1.Now()
	oldStatus := kubeovnv1.VpcEgressGatewayBFDStatus{
		Sessions: []kubeovnv1.VpcEgressGatewayBFDSession{{
			AddressFamily:      4,
			Node:               "node-a",
			Nexthop:            "10.0.0.2",
			SBStatus:           ovnsb.BFDStatusUp,
			LastTransitionTime: oldTransition,
		}},
	}

	sessions := vegBFDSessionStatuses(
		4,
		"bfd@test",
		map[string]string{"node-a": "10.0.0.2", "node-b": "10.0.0.3"},
		map[string]ovnnb.BFD{
			"10.0.0.2": {UUID: "nb-a", DstIP: "10.0.0.2", Status: &up},
		},
		map[string]ovnsb.BFD{
			"10.0.0.2": {UUID: "sb-a", DstIP: "10.0.0.2", Status: ovnsb.BFDStatusUp, ChassisName: "chassis-a"},
		},
		oldStatus,
	)

	require.Len(t, sessions, 2)
	require.True(t, vegBFDSessionUp(sessions[0]))
	require.Equal(t, oldTransition, sessions[0].LastTransitionTime)
	require.False(t, vegBFDSessionUp(sessions[1]))
	require.Equal(t, "NB BFD session is missing", sessions[1].Message)
}
