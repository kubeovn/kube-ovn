package daemon

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/knftables"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestBuildNFTServiceSnapshot(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			ClusterIPs:            []string{"10.96.0.10", "fd00:10:96::10"},
			ExternalIPs:           []string{"192.0.2.10"},
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyLocal,
			Ports: []corev1.ServicePort{{
				Protocol: corev1.ProtocolSCTP,
				Port:     90,
				NodePort: 30090,
			}},
		},
		Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{{IP: "198.51.100.10"}},
		}},
	}

	snapshot, err := buildNFTGatewaySnapshot(nftSnapshotInput{
		Protocol: kubeovnv1.ProtocolDual,
		Services: []*corev1.Service{service},
	})
	require.NoError(t, err)

	v4 := nftFamilySnapshotForTest(t, snapshot, knftables.IPv4Family)
	require.Contains(t, v4.ClusterIPPorts, nftAddressPort{Address: "10.96.0.10", Protocol: "sctp", Port: 90})
	require.Contains(t, v4.ServiceVIPPorts, nftAddressPort{Address: "192.0.2.10", Protocol: "sctp", Port: 90})
	require.Contains(t, v4.ServiceVIPPorts, nftAddressPort{Address: "198.51.100.10", Protocol: "sctp", Port: 90})
	require.Contains(t, v4.LocalNodePorts, nftProtocolPort{Protocol: "sctp", Port: 30090})

	v6 := nftFamilySnapshotForTest(t, snapshot, knftables.IPv6Family)
	require.Contains(t, v6.ClusterIPPorts, nftAddressPort{Address: "fd00:10:96::10", Protocol: "sctp", Port: 90})
	require.Contains(t, v6.LocalNodePorts, nftProtocolPort{Protocol: "sctp", Port: 30090})
}

func TestBuildNFTGatewaySnapshot(t *testing.T) {
	distributed := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "distributed", UID: types.UID("uid-distributed")},
		Spec: kubeovnv1.SubnetSpec{
			Vpc:         util.DefaultVpc,
			Protocol:    kubeovnv1.ProtocolDual,
			CIDRBlock:   "10.16.0.0/24,fd00:10:16::/120",
			GatewayType: kubeovnv1.GWDistributedType,
			NatOutgoing: true,
		},
		Status: kubeovnv1.SubnetStatus{NatOutgoingPolicyRules: []kubeovnv1.NatOutgoingPolicyRuleStatus{{
			RuleID: "rule-1",
			NatOutgoingPolicyRule: kubeovnv1.NatOutgoingPolicyRule{
				Match:  kubeovnv1.NatOutGoingPolicyMatch{SrcIPs: "10.16.0.10,fd00:10:16::10", DstIPs: "0.0.0.0/0,::/0"},
				Action: "nat",
			},
		}}},
	}
	centralized := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "centralized", UID: types.UID("uid-centralized")},
		Spec: kubeovnv1.SubnetSpec{
			Vpc:         util.DefaultVpc,
			Protocol:    kubeovnv1.ProtocolIPv4,
			CIDRBlock:   "10.17.0.0/24",
			GatewayType: kubeovnv1.GWCentralizedType,
			GatewayNode: "node-a:192.168.1.10",
			NatOutgoing: true,
		},
		Status: kubeovnv1.SubnetStatus{ActivateGateway: "node-a"},
	}
	nodes := []*corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
			Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "192.168.1.10"},
				{Type: corev1.NodeInternalIP, Address: "fd00::10"},
			}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "node-b"},
			Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "192.168.1.11"},
				{Type: corev1.NodeInternalIP, Address: "fd00::11"},
			}},
		},
	}
	tproxyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "probe", Namespace: "default"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			LivenessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(8080)},
			}},
		}}},
		Status: corev1.PodStatus{PodIPs: []corev1.PodIP{{IP: "10.30.0.2"}, {IP: "fd00:30::2"}}},
	}

	snapshot, err := buildNFTGatewaySnapshot(nftSnapshotInput{
		Protocol:       kubeovnv1.ProtocolDual,
		ClusterRouter:  util.DefaultVpc,
		NodeName:       "node-a",
		Subnets:        []*kubeovnv1.Subnet{centralized, distributed, distributed.DeepCopy()},
		Nodes:          nodes,
		TProxyPods:     []*corev1.Pod{tproxyPod},
		LocalAddresses: []string{"127.0.0.1", "192.168.1.10", "::1", "fd00::10"},
	})
	require.NoError(t, err)

	v4 := nftFamilySnapshotForTest(t, snapshot, knftables.IPv4Family)
	require.Equal(t, "192.168.1.10", v4.NodeInternalIP)
	require.Equal(t, []string{"192.168.1.10"}, v4.NodeIPs)
	require.Equal(t, []string{"192.168.1.11"}, v4.OtherNodeIPs)
	require.Equal(t, []string{"10.16.0.0/24", "10.17.0.0/24"}, v4.Subnets)
	require.Equal(t, []string{"10.16.0.0/24", "10.17.0.0/24"}, v4.NATSubnets)
	require.Equal(t, []string{"10.16.0.0/24"}, v4.DistributedGWSubnets)
	require.Contains(t, v4.NATPolicies, nftNATPolicy{
		SubnetCIDR: "10.16.0.0/24",
		RuleID:     "rule-1",
		SrcIPs:     []string{"10.16.0.10"},
		DstIPs:     []string{"0.0.0.0/0"},
		Action:     "nat",
	})
	require.Contains(t, v4.CentralizedSNATs, nftCentralizedSNAT{CIDR: "10.17.0.0/24", IP: "192.168.1.10"})
	require.Contains(t, v4.TProxyTargets, nftTProxyTarget{Address: "10.30.0.2", Port: 8080})
	require.Len(t, v4.SubnetCounters, 2)

	v6 := nftFamilySnapshotForTest(t, snapshot, knftables.IPv6Family)
	require.Equal(t, "fd00::10", v6.NodeInternalIP)
	require.Equal(t, []string{"fd00::10"}, v6.NodeIPs)
	require.Equal(t, []string{"fd00::11"}, v6.OtherNodeIPs)
	require.Equal(t, []string{"fd00:10:16::/120"}, v6.Subnets)
	require.Contains(t, v6.TProxyTargets, nftTProxyTarget{Address: "fd00:30::2", Port: 8080})
}

func TestBuildNFTGatewaySnapshotPreservesNATPolicyOrder(t *testing.T) {
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "ordered", UID: types.UID("uid-ordered")},
		Spec: kubeovnv1.SubnetSpec{
			Vpc:         util.DefaultVpc,
			CIDRBlock:   "10.18.0.0/24",
			GatewayType: kubeovnv1.GWDistributedType,
			NatOutgoing: true,
		},
		Status: kubeovnv1.SubnetStatus{NatOutgoingPolicyRules: []kubeovnv1.NatOutgoingPolicyRuleStatus{
			{
				RuleID: "z-rule",
				NatOutgoingPolicyRule: kubeovnv1.NatOutgoingPolicyRule{
					Match:  kubeovnv1.NatOutGoingPolicyMatch{DstIPs: "192.0.2.0/24"},
					Action: "forward",
				},
			},
			{
				RuleID: "a-rule",
				NatOutgoingPolicyRule: kubeovnv1.NatOutgoingPolicyRule{
					Match:  kubeovnv1.NatOutGoingPolicyMatch{DstIPs: "0.0.0.0/0"},
					Action: "nat",
				},
			},
		}},
	}

	snapshot, err := buildNFTGatewaySnapshot(nftSnapshotInput{
		Protocol:      kubeovnv1.ProtocolIPv4,
		ClusterRouter: util.DefaultVpc,
		Subnets:       []*kubeovnv1.Subnet{subnet},
	})
	require.NoError(t, err)

	v4 := nftFamilySnapshotForTest(t, snapshot, knftables.IPv4Family)
	require.Equal(t, []string{"z-rule", "a-rule"}, []string{v4.NATPolicies[0].RuleID, v4.NATPolicies[1].RuleID})
}

func TestBuildNFTGatewaySnapshotSkipsEmptyNATPolicyMatch(t *testing.T) {
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "empty-match", UID: types.UID("uid-empty-match")},
		Spec: kubeovnv1.SubnetSpec{
			Vpc:         util.DefaultVpc,
			CIDRBlock:   "10.19.0.0/24",
			GatewayType: kubeovnv1.GWDistributedType,
			NatOutgoing: true,
		},
		Status: kubeovnv1.SubnetStatus{NatOutgoingPolicyRules: []kubeovnv1.NatOutgoingPolicyRuleStatus{{
			RuleID: "empty",
			NatOutgoingPolicyRule: kubeovnv1.NatOutgoingPolicyRule{
				Action: "nat",
			},
		}}},
	}

	snapshot, err := buildNFTGatewaySnapshot(nftSnapshotInput{
		Protocol:      kubeovnv1.ProtocolIPv4,
		ClusterRouter: util.DefaultVpc,
		Subnets:       []*kubeovnv1.Subnet{subnet},
	})
	require.NoError(t, err)

	v4 := nftFamilySnapshotForTest(t, snapshot, knftables.IPv4Family)
	require.Empty(t, v4.NATPolicies)
}

func TestBuildNFTGatewaySnapshotCompactsOverlappingNATPolicyAddresses(t *testing.T) {
	subnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "overlap", UID: types.UID("uid-overlap")},
		Spec: kubeovnv1.SubnetSpec{
			Vpc:         util.DefaultVpc,
			CIDRBlock:   "10.18.0.0/24",
			GatewayType: kubeovnv1.GWDistributedType,
			NatOutgoing: true,
		},
		Status: kubeovnv1.SubnetStatus{NatOutgoingPolicyRules: []kubeovnv1.NatOutgoingPolicyRuleStatus{{
			RuleID: "rule-overlap",
			NatOutgoingPolicyRule: kubeovnv1.NatOutgoingPolicyRule{
				Match: kubeovnv1.NatOutGoingPolicyMatch{
					SrcIPs: "10.0.0.0/8,10.1.0.0/16,10.1.2.3",
				},
				Action: util.NatPolicyRuleActionNat,
			},
		}}},
	}

	snapshot, err := buildNFTGatewaySnapshot(nftSnapshotInput{
		Protocol:      kubeovnv1.ProtocolIPv4,
		ClusterRouter: util.DefaultVpc,
		Subnets:       []*kubeovnv1.Subnet{subnet},
	})
	require.NoError(t, err)
	v4 := nftFamilySnapshotForTest(t, snapshot, knftables.IPv4Family)
	require.Equal(t, []string{"10.0.0.0/8"}, v4.NATPolicies[0].SrcIPs)
}

func nftFamilySnapshotForTest(t *testing.T, snapshot gatewayNFTSnapshot, family knftables.Family) nftFamilySnapshot {
	t.Helper()
	for _, item := range snapshot.Families {
		if item.Family == family {
			return item
		}
	}
	require.FailNow(t, "nftables family not found", string(family))
	return nftFamilySnapshot{}
}
