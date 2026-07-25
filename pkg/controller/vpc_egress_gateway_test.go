package controller

import (
	"testing"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/set"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
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

func TestFlattenVpcEgressGatewayNexthops(t *testing.T) {
	nextHops := flattenVpcEgressGatewayNexthops(map[string]set.Set[string]{
		"node-1": set.New("10.16.1.10", "10.16.1.11"),
		"node-2": set.New("10.16.2.10"),
	})
	require.Equal(t, set.New("10.16.1.10", "10.16.1.11", "10.16.2.10"), nextHops)
}

func TestUpdateVpcEgressGatewayPolicyNexthops(t *testing.T) {
	policy := &ovnnb.LogicalRouterPolicy{
		Nexthops:    []string{"10.16.1.10", "10.16.1.11"},
		BFDSessions: []string{"bfd-1", "bfd-2"},
	}

	changed := updateVpcEgressGatewayPolicyNexthops(policy, set.New("10.16.1.10"), set.New("bfd-1"))
	require.True(t, changed)
	require.Equal(t, set.New("10.16.1.10"), set.New(policy.Nexthops...))
	require.Equal(t, set.New("bfd-1"), set.New(policy.BFDSessions...))

	changed = updateVpcEgressGatewayPolicyNexthops(policy, set.New("10.16.1.10", "10.16.1.11"), set.New[string]())
	require.True(t, changed)
	require.Equal(t, set.New("10.16.1.10", "10.16.1.11"), set.New(policy.Nexthops...))
	require.Empty(t, policy.BFDSessions)

	require.False(t, updateVpcEgressGatewayPolicyNexthops(
		policy,
		set.New("10.16.1.11", "10.16.1.10"),
		set.New[string](),
	))
}

func TestLocalGatewayPolicyBFDSessions(t *testing.T) {
	bfdMap := map[string]string{
		"10.16.1.10": "bfd-1",
		"10.16.1.11": "bfd-2",
		"10.16.2.10": "bfd-3",
	}
	require.Equal(t,
		set.New("bfd-1", "bfd-2"),
		localGatewayPolicyBFDSessions(bfdMap, set.New("10.16.1.10", "10.16.1.11")),
	)
	require.Empty(t, localGatewayPolicyBFDSessions(nil, set.New("10.16.1.10")))
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
	sameNodePod1 := newVegWorkloadPod("veg-1", "node-1", "10.16.1.10", readyAttachment)
	sameNodePod1.Status.PodIPs = append(sameNodePod1.Status.PodIPs, corev1.PodIP{IP: "fd00:10::10"})
	sameNodePod2 := newVegWorkloadPod("veg-2", "node-1", "10.16.1.11", `[{"name":"default/eth1","ips":["172.17.1.11"]}]`)
	sameNodePod2.Status.PodIPs = append(sameNodePod2.Status.PodIPs, corev1.PodIP{IP: "fd00:10::11"})

	tests := []struct {
		name                string
		pods                []*corev1.Pod
		wantInternalIPs     []string
		wantExternalIPs     []string
		wantNodes           []string
		wantNodeNexthopIPv4 map[string]set.Set[string]
		wantNodeNexthopIPv6 map[string]set.Set[string]
		wantNotReadyCount   int
	}{
		{
			name: "all workload pods have attachment network",
			pods: []*corev1.Pod{
				newVegWorkloadPod("veg-1", "node-1", "10.16.1.10", readyAttachment),
				newVegWorkloadPod("veg-2", "node-2", "10.16.1.11", `[{"name":"default/eth1","ips":["172.17.1.11"]}]`),
			},
			wantInternalIPs:     []string{"10.16.1.10", "10.16.1.11"},
			wantExternalIPs:     []string{"172.17.1.10", "172.17.1.11"},
			wantNodes:           []string{"node-1", "node-2"},
			wantNodeNexthopIPv4: map[string]set.Set[string]{"node-1": set.New("10.16.1.10"), "node-2": set.New("10.16.1.11")},
			wantNodeNexthopIPv6: map[string]set.Set[string]{},
		},
		{
			name:                "workload pods on the same node",
			pods:                []*corev1.Pod{sameNodePod1, sameNodePod2},
			wantInternalIPs:     []string{"10.16.1.10,fd00:10::10", "10.16.1.11,fd00:10::11"},
			wantExternalIPs:     []string{"172.17.1.10", "172.17.1.11"},
			wantNodes:           []string{"node-1"},
			wantNodeNexthopIPv4: map[string]set.Set[string]{"node-1": set.New("10.16.1.10", "10.16.1.11")},
			wantNodeNexthopIPv6: map[string]set.Set[string]{"node-1": set.New("fd00:10::10", "fd00:10::11")},
		},
		{
			name: "one workload pod misses attachment network",
			pods: []*corev1.Pod{
				newVegWorkloadPod("veg-1", "node-1", "10.16.1.10", readyAttachment),
				newVegWorkloadPod("veg-2", "node-2", "10.16.1.11", `[{"name":"kube-ovn","ips":["10.16.1.11"]}]`),
			},
			wantInternalIPs:     []string{"10.16.1.10"},
			wantExternalIPs:     []string{"172.17.1.10"},
			wantNodes:           []string{"node-1"},
			wantNodeNexthopIPv4: map[string]set.Set[string]{"node-1": set.New("10.16.1.10")},
			wantNodeNexthopIPv6: map[string]set.Set[string]{},
			wantNotReadyCount:   2,
		},
		{
			name: "one workload pod has attachment network without ip",
			pods: []*corev1.Pod{
				newVegWorkloadPod("veg-1", "node-1", "10.16.1.10", readyAttachment),
				newVegWorkloadPod("veg-2", "node-2", "10.16.1.11", `[{"name":"default/eth1","ips":[]}]`),
			},
			wantInternalIPs:     []string{"10.16.1.10"},
			wantExternalIPs:     []string{"172.17.1.10"},
			wantNodes:           []string{"node-1"},
			wantNodeNexthopIPv4: map[string]set.Set[string]{"node-1": set.New("10.16.1.10")},
			wantNodeNexthopIPv6: map[string]set.Set[string]{},
			wantNotReadyCount:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := &kubeovnv1.VpcEgressGateway{
				Spec: kubeovnv1.VpcEgressGatewaySpec{
					Replicas: 2,
				},
			}

			nodeNexthopIPv4, nodeNexthopIPv6, messages := collectVpcEgressGatewayWorkloadStatus(gw, tt.pods, attachmentNetwork)

			require.Equal(t, tt.wantInternalIPs, gw.Status.InternalIPs)
			require.Equal(t, tt.wantExternalIPs, gw.Status.ExternalIPs)
			require.Equal(t, tt.wantNodes, gw.Status.Workload.Nodes)
			require.Equal(t, tt.wantNodeNexthopIPv4, nodeNexthopIPv4)
			require.Equal(t, tt.wantNodeNexthopIPv6, nodeNexthopIPv6)
			require.Len(t, messages, tt.wantNotReadyCount)
		})
	}
}
