package daemon

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func newPodForPolicyRouting(name, namespace, subnetName, podIP string, podIPs []v1.PodIP) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				util.LogicalSwitchAnnotation: subnetName,
			},
		},
		Status: v1.PodStatus{
			PodIP:  podIP,
			PodIPs: podIPs,
		},
	}
}

func TestGetPolicyRouting(t *testing.T) {
	t.Parallel()

	const (
		clusterRouter = "ovn-cluster"
		nodeName      = "test-node"
		subnetName    = "test-subnet"
		tableID       = 100
		priority      = 200
	)

	tests := []struct {
		name          string
		subnet        *kubeovnv1.Subnet
		pods          []*v1.Pod
		expectedRules int
		expectedRtns  int
		validateRules func(t *testing.T, rules []netlink.Rule)
	}{
		{
			name:          "nil subnet returns nil",
			subnet:        nil,
			expectedRules: 0,
			expectedRtns:  0,
		},
		{
			name: "subnet without ExternalEgressGateway returns nil",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: subnetName},
				Spec: kubeovnv1.SubnetSpec{
					Vpc:         clusterRouter,
					GatewayType: kubeovnv1.GWDistributedType,
				},
			},
			expectedRules: 0,
			expectedRtns:  0,
		},
		{
			name: "subnet with different VPC returns nil",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: subnetName},
				Spec: kubeovnv1.SubnetSpec{
					Vpc:                   "other-vpc",
					ExternalEgressGateway: "10.0.0.1",
					GatewayType:           kubeovnv1.GWDistributedType,
					PolicyRoutingTableID:  tableID,
					PolicyRoutingPriority: priority,
				},
			},
			expectedRules: 0,
			expectedRtns:  0,
		},
		{
			name: "distributed: single-stack IPv4 EGW + IPv4 Pod",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: subnetName},
				Spec: kubeovnv1.SubnetSpec{
					Vpc:                   clusterRouter,
					CIDRBlock:             "10.16.0.0/24",
					ExternalEgressGateway: "10.0.0.1",
					GatewayType:           kubeovnv1.GWDistributedType,
					PolicyRoutingTableID:  tableID,
					PolicyRoutingPriority: priority,
				},
			},
			pods: []*v1.Pod{
				newPodForPolicyRouting("pod1", "default", subnetName, "10.16.0.5",
					[]v1.PodIP{{IP: "10.16.0.5"}}),
			},
			expectedRules: 1,
			expectedRtns:  1,
			validateRules: func(t *testing.T, rules []netlink.Rule) {
				require.Equal(t, unix.AF_INET, rules[0].Family)
				require.Equal(t, net.ParseIP("10.16.0.5"), rules[0].Src.IP)
				ones, _ := rules[0].Src.Mask.Size()
				require.Equal(t, 32, ones)
			},
		},
		{
			name: "distributed: dual-stack EGW + dual-stack Pod",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: subnetName},
				Spec: kubeovnv1.SubnetSpec{
					Vpc:                   clusterRouter,
					CIDRBlock:             "10.16.0.0/24,fd00::/120",
					ExternalEgressGateway: "10.0.0.1,fd00::1",
					GatewayType:           kubeovnv1.GWDistributedType,
					PolicyRoutingTableID:  tableID,
					PolicyRoutingPriority: priority,
				},
			},
			pods: []*v1.Pod{
				newPodForPolicyRouting("pod1", "default", subnetName, "10.16.0.5",
					[]v1.PodIP{{IP: "10.16.0.5"}, {IP: "fd00::5"}}),
			},
			expectedRules: 2,
			expectedRtns:  2,
			validateRules: func(t *testing.T, rules []netlink.Rule) {
				require.Equal(t, unix.AF_INET, rules[0].Family)
				require.Equal(t, net.ParseIP("10.16.0.5"), rules[0].Src.IP)
				ones, _ := rules[0].Src.Mask.Size()
				require.Equal(t, 32, ones)

				require.Equal(t, unix.AF_INET6, rules[1].Family)
				require.Equal(t, net.ParseIP("fd00::5"), rules[1].Src.IP)
				ones, _ = rules[1].Src.Mask.Size()
				require.Equal(t, 128, ones)
			},
		},
		{
			name: "distributed: dual-stack EGW + IPv4-only Pod should skip IPv6 rule",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: subnetName},
				Spec: kubeovnv1.SubnetSpec{
					Vpc:                   clusterRouter,
					CIDRBlock:             "10.16.0.0/24,fd00::/120",
					ExternalEgressGateway: "10.0.0.1,fd00::1",
					GatewayType:           kubeovnv1.GWDistributedType,
					PolicyRoutingTableID:  tableID,
					PolicyRoutingPriority: priority,
				},
			},
			pods: []*v1.Pod{
				newPodForPolicyRouting("pod1", "default", subnetName, "10.16.0.5",
					[]v1.PodIP{{IP: "10.16.0.5"}}),
			},
			expectedRules: 1,
			expectedRtns:  2,
			validateRules: func(t *testing.T, rules []netlink.Rule) {
				require.Equal(t, unix.AF_INET, rules[0].Family)
				require.Equal(t, net.ParseIP("10.16.0.5"), rules[0].Src.IP)
			},
		},
		{
			name: "distributed: dual-stack EGW + IPv6-only Pod should skip IPv4 rule",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: subnetName},
				Spec: kubeovnv1.SubnetSpec{
					Vpc:                   clusterRouter,
					CIDRBlock:             "10.16.0.0/24,fd00::/120",
					ExternalEgressGateway: "10.0.0.1,fd00::1",
					GatewayType:           kubeovnv1.GWDistributedType,
					PolicyRoutingTableID:  tableID,
					PolicyRoutingPriority: priority,
				},
			},
			pods: []*v1.Pod{
				newPodForPolicyRouting("pod1", "default", subnetName, "fd00::5",
					[]v1.PodIP{{IP: "fd00::5"}}),
			},
			expectedRules: 1,
			expectedRtns:  2,
			validateRules: func(t *testing.T, rules []netlink.Rule) {
				require.Equal(t, unix.AF_INET6, rules[0].Family)
				require.Equal(t, net.ParseIP("fd00::5"), rules[0].Src.IP)
				ones, _ := rules[0].Src.Mask.Size()
				require.Equal(t, 128, ones)
			},
		},
		{
			name: "distributed: multiple pods with mixed stacks",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: subnetName},
				Spec: kubeovnv1.SubnetSpec{
					Vpc:                   clusterRouter,
					CIDRBlock:             "10.16.0.0/24,fd00::/120",
					ExternalEgressGateway: "10.0.0.1,fd00::1",
					GatewayType:           kubeovnv1.GWDistributedType,
					PolicyRoutingTableID:  tableID,
					PolicyRoutingPriority: priority,
				},
			},
			pods: []*v1.Pod{
				// dual-stack pod: should generate 2 rules
				newPodForPolicyRouting("pod1", "default", subnetName, "10.16.0.5",
					[]v1.PodIP{{IP: "10.16.0.5"}, {IP: "fd00::5"}}),
				// IPv4-only pod: should generate 1 rule
				newPodForPolicyRouting("pod2", "default", subnetName, "10.16.0.6",
					[]v1.PodIP{{IP: "10.16.0.6"}}),
				// pod in different subnet: should be skipped
				newPodForPolicyRouting("pod3", "default", "other-subnet", "10.16.0.7",
					[]v1.PodIP{{IP: "10.16.0.7"}}),
				// pod without IP: should be skipped
				newPodForPolicyRouting("pod4", "default", subnetName, "",
					nil),
			},
			expectedRules: 3, // 2 from pod1 + 1 from pod2
			expectedRtns:  2,
			validateRules: func(t *testing.T, rules []netlink.Rule) {
				for _, r := range rules {
					require.NotNil(t, r.Src, "rule.Src must not be nil")
					require.NotNil(t, r.Src.IP, "rule.Src.IP must not be nil")
				}
			},
		},
		{
			name: "centralized: dual-stack EGW + dual-stack CIDR",
			subnet: &kubeovnv1.Subnet{
				ObjectMeta: metav1.ObjectMeta{Name: subnetName},
				Spec: kubeovnv1.SubnetSpec{
					Vpc:                   clusterRouter,
					CIDRBlock:             "10.16.0.0/24,fd00::/120",
					ExternalEgressGateway: "10.0.0.1,fd00::1",
					GatewayType:           kubeovnv1.GWCentralizedType,
					GatewayNode:           nodeName,
					PolicyRoutingTableID:  tableID,
					PolicyRoutingPriority: priority,
				},
			},
			expectedRules: 2,
			expectedRtns:  2,
			validateRules: func(t *testing.T, rules []netlink.Rule) {
				require.Equal(t, unix.AF_INET, rules[0].Family)
				require.NotNil(t, rules[0].Src)
				require.Equal(t, "10.16.0.0/24", rules[0].Src.String())

				require.Equal(t, unix.AF_INET6, rules[1].Family)
				require.NotNil(t, rules[1].Src)
				require.Equal(t, "fd00::/120", rules[1].Src.String())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Build pod indexer
			podIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
			for _, pod := range tt.pods {
				require.NoError(t, podIndexer.Add(pod))
			}

			// Build node indexer for centralized gateway tests
			nodeIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			require.NoError(t, nodeIndexer.Add(&v1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: nodeName},
			}))

			c := &Controller{
				podsLister:  listerv1.NewPodLister(podIndexer),
				nodesLister: listerv1.NewNodeLister(nodeIndexer),
				config: &Configuration{
					ClusterRouter: clusterRouter,
					NodeName:      nodeName,
				},
			}

			rules, routes, err := c.getPolicyRouting(tt.subnet)
			require.NoError(t, err)
			require.Len(t, rules, tt.expectedRules)
			require.Len(t, routes, tt.expectedRtns)

			// Validate all rules have non-nil Src.IP
			for i, r := range rules {
				require.NotNil(t, r.Src, "rule[%d].Src must not be nil", i)
				require.NotNil(t, r.Src.IP, "rule[%d].Src.IP must not be nil", i)
			}

			if tt.validateRules != nil {
				tt.validateRules(t, rules)
			}
		})
	}
}
