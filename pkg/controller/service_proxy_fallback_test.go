package controller

import (
	"fmt"
	"strings"
	"testing"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func newServiceProxyFallbackController(t *testing.T) (*fakeController, *kubeovnv1.Subnet) {
	t.Helper()
	subnet := &kubeovnv1.Subnet{Name: "ovn-default", Spec: kubeovnv1.SubnetSpec{
		Vpc: util.DefaultVpc, CIDRBlock: "10.16.0.0/16,fd00:10:16::/64",
		Gateway: "10.16.0.1,fd00:10:16::1", GatewayType: kubeovnv1.GWDistributedType,
		EnableLb: new(true),
	}}
	node := &corev1.Node{
		Name: "worker-1", Annotations: map[string]string{util.IPAddressAnnotation: "100.64.0.2,fd00:100:64::2"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
	}
	ip := &kubeovnv1.IP{Name: util.NodeLspName(node.Name), Spec: kubeovnv1.IPSpec{
		NodeName: node.Name, V4IPAddress: "100.64.0.2", V6IPAddress: "fd00:100:64::2",
	}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{subnet}, Nodes: []*corev1.Node{node}, IPs: []*kubeovnv1.IP{ip},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.EnableOvnLB = true
	ctrl.serviceCIDRStore = util.NewServiceCIDRStore("10.96.0.0/12,fd00:10:96::/108")
	ctrl.OVNNbClient = newInMemoryOVNNbClient(t)
	require.NoError(t, ctrl.OVNNbClient.CreateLogicalRouter(ctrl.config.ClusterRouter))
	require.NoError(t, ctrl.OVNNbClient.CreateBareLogicalSwitch(subnet.Name))
	return fc, subnet
}

func serviceProxyFallbackPolicies(t *testing.T, ctrl *Controller, subnet *kubeovnv1.Subnet, owner string) []*ovnnb.LogicalRouterPolicy {
	t.Helper()
	policies, err := ctrl.OVNNbClient.ListLogicalRouterPolicies(ctrl.config.ClusterRouter, -1,
		buildPolicyRouteExternalIDs(subnet.Name, map[string]string{owner: "true"}), true)
	require.NoError(t, err)
	return policies
}

func TestServiceProxyFallbackDropScope(t *testing.T) {
	for _, mode := range []string{"distributed", "centralized", "logical-gateway", "inherited-lb", "physical-underlay", "u2o", "custom-vpc", "join", "non-ovn", "global-lb-disabled", "subnet-lb-disabled"} {
		t.Run(mode, func(t *testing.T) {
			fc, subnet := newServiceProxyFallbackController(t)
			ctrl := fc.fakeController
			want := 2
			switch mode {
			case "centralized":
				subnet.Spec.GatewayType = kubeovnv1.GWCentralizedType
			case "logical-gateway":
				subnet.Spec.Vlan = "vlan-1"
				subnet.Spec.LogicalGateway = true
			case "inherited-lb":
				subnet.Spec.EnableLb = nil
			case "physical-underlay", "u2o":
				subnet.Spec.Vlan = "vlan-1"
				subnet.Spec.U2OInterconnection = mode == "u2o"
				want = 0
			case "custom-vpc":
				subnet.Spec.Vpc = "custom"
				want = 0
			case "join":
				subnet.Name = ctrl.config.NodeSwitch
				want = 0
			case "non-ovn":
				subnet.Spec.Provider = "net.ns"
				want = 0
			case "global-lb-disabled":
				// An explicit subnet opt-in cannot override the global disable.
				ctrl.config.EnableOvnLB = false
				want = 0
			case "subnet-lb-disabled":
				subnet.Spec.EnableLb = new(false)
				want = 0
			}
			require.NoError(t, ctrl.reconcileServiceProxyFallbackDropPolicy(subnet))
			policies := serviceProxyFallbackPolicies(t, ctrl, subnet, "isServiceProxyFallbackDropPolicy")
			require.Len(t, policies, want)
			var matches []string
			for _, policy := range policies {
				matches = append(matches, policy.Match)
				require.Equal(t, ovnnb.LogicalRouterPolicyActionDrop, policy.Action)
				require.Empty(t, policy.Nexthops)
				require.Greater(t, policy.Priority, util.GatewayRouterPolicyPriority)
				require.Less(t, policy.Priority, util.NodeRouterPolicyPriority)
			}
			if want != 0 {
				require.ElementsMatch(t, []string{
					`inport == "ovn-cluster-ovn-default" && ip4.dst == 10.96.0.0/12`,
					`inport == "ovn-cluster-ovn-default" && ip6.dst == fd00:10:96::/108`,
				}, matches)
			}
			require.NoError(t, ctrl.reconcileServiceProxyFallbackDropPolicy(subnet))
			require.ElementsMatch(t, policies, serviceProxyFallbackPolicies(t, ctrl, subnet, "isServiceProxyFallbackDropPolicy"))
		})
	}
}

func TestServiceProxyFallbackDropLifecycle(t *testing.T) {
	for _, transition := range []string{"global-lb-disabled", "subnet-lb-disabled", "physical-underlay", "custom-vpc", "service-cidr", "ipv4-only", "dns", "delete"} {
		t.Run(transition, func(t *testing.T) {
			fc, subnet := newServiceProxyFallbackController(t)
			ctrl := fc.fakeController
			require.NoError(t, ctrl.reconcileServiceProxyFallbackDropPolicy(subnet))
			require.Len(t, serviceProxyFallbackPolicies(t, ctrl, subnet, "isServiceProxyFallbackDropPolicy"), 2)
			other := subnet.DeepCopy()
			other.Name = "other"
			require.NoError(t, ctrl.reconcileServiceProxyFallbackDropPolicy(other))
			foreignMatch := "ip4.dst == 203.0.113.0/24"
			require.NoError(t, ctrl.OVNNbClient.AddLogicalRouterPolicy(ctrl.config.ClusterRouter, 1000,
				foreignMatch, ovnnb.LogicalRouterPolicyActionDrop, nil, nil, buildPolicyRouteExternalIDs(subnet.Name, nil)))
			want := 0
			switch transition {
			case "global-lb-disabled":
				ctrl.config.EnableOvnLB = false
			case "subnet-lb-disabled":
				subnet.Spec.EnableLb = new(false)
			case "physical-underlay":
				subnet.Spec.Vlan = "vlan-1"
			case "custom-vpc":
				subnet.Spec.Vpc = "custom"
			case "service-cidr":
				ctrl.serviceCIDRStore = util.NewServiceCIDRStore("10.112.0.0/12")
				want = 1
			case "ipv4-only":
				subnet.Spec.CIDRBlock = "10.16.0.0/16"
				want = 1
			case "dns":
				ctrl.config.NodeLocalDNSIPs = []string{"10.96.0.10", "fd00:10:96::a", "169.254.20.10"}
				want = 2
			}
			if transition == "delete" {
				require.NoError(t, ctrl.deletePolicyRouteByGatewayType(subnet, subnet.Spec.GatewayType, true))
			} else {
				require.NoError(t, ctrl.reconcileServiceProxyFallbackDropPolicy(subnet))
			}
			policies := serviceProxyFallbackPolicies(t, ctrl, subnet, "isServiceProxyFallbackDropPolicy")
			require.Len(t, policies, want)
			switch transition {
			case "service-cidr":
				require.Equal(t, `inport == "ovn-cluster-ovn-default" && ip4.dst == 10.112.0.0/12`, policies[0].Match)
			case "ipv4-only":
				require.Equal(t, `inport == "ovn-cluster-ovn-default" && ip4.dst == 10.96.0.0/12`, policies[0].Match)
			case "dns":
				var matches []string
				for _, policy := range policies {
					matches = append(matches, policy.Match)
				}
				require.ElementsMatch(t, []string{
					`inport == "ovn-cluster-ovn-default" && ip4.dst == 10.96.0.0/12 && ip4.dst != 10.96.0.10`,
					`inport == "ovn-cluster-ovn-default" && ip6.dst == fd00:10:96::/108 && ip6.dst != fd00:10:96::a`,
				}, matches)
			}
			require.Len(t, serviceProxyFallbackPolicies(t, ctrl, other, "isServiceProxyFallbackDropPolicy"), 2)
			foreign, err := ctrl.OVNNbClient.GetLogicalRouterPolicy(ctrl.config.ClusterRouter, 1000, foreignMatch, false)
			require.NoError(t, err)
			require.Len(t, foreign, 1)
		})
	}
}

func TestServiceProxyFallbackCentralizedLBTransition(t *testing.T) {
	fc, subnet := newServiceProxyFallbackController(t)
	ctrl := fc.fakeController
	subnet.Spec.GatewayType = kubeovnv1.GWCentralizedType
	subnet.Spec.GatewayNode = "worker-1"
	subnet.Status.ActivateGateway = "worker-1"
	for _, enabled := range []bool{false, true, false} {
		ctrl.config.EnableOvnLB = enabled
		require.NoError(t, ctrl.reconcileOvnDefaultVpcRoute(subnet))
		drops := serviceProxyFallbackPolicies(t, ctrl, subnet, "isServiceProxyFallbackDropPolicy")
		reroutes := serviceProxyFallbackPolicies(t, ctrl, subnet, "isServiceProxyFallbackReroutePolicy")
		if enabled {
			require.Len(t, drops, 2)
			require.Empty(t, reroutes)
		} else {
			require.Empty(t, drops)
			require.Len(t, reroutes, 2)
		}
	}
}

func TestServiceProxyFallbackCentralizedUsesPodNode(t *testing.T) {
	fc, subnet := newServiceProxyFallbackController(t)
	ctrl := fc.fakeController
	subnet.Spec.GatewayType = kubeovnv1.GWCentralizedType
	subnet.Spec.GatewayNode = "worker-1"
	subnet.Status.ActivateGateway = "worker-1"
	subnet.Spec.EnableLb = new(false)
	worker2 := &corev1.Node{Name: "worker-2", Annotations: map[string]string{util.IPAddressAnnotation: "100.64.0.3,fd00:100:64::3"}}
	indexer := fc.fakeInformers.nodeInformer.Informer().GetIndexer()
	require.NoError(t, indexer.Add(worker2))
	require.NoError(t, ctrl.reconcileOvnDefaultVpcRoute(subnet))
	policies := serviceProxyFallbackPolicies(t, ctrl, subnet, "isServiceProxyFallbackReroutePolicy")
	require.Len(t, policies, 4)
	for _, policy := range policies {
		node := policy.ExternalIDs["node"]
		v4, v6 := "100.64.0.2", "fd00:100:64::2"
		if node == "worker-2" {
			v4, v6 = "100.64.0.3", "fd00:100:64::3"
		}
		if policy.ExternalIDs["serviceCIDR"] == "10.96.0.0/12" {
			require.Equal(t, fmt.Sprintf("ip4.src == $node.%s_ip4 && ip4.src == 10.16.0.0/16 && ip4.dst == 10.96.0.0/12", strings.ReplaceAll(node, "-", ".")), policy.Match)
			require.Equal(t, []string{v4}, policy.Nexthops)
		} else {
			require.Equal(t, []string{v6}, policy.Nexthops)
		}
		require.Equal(t, ovnnb.LogicalRouterPolicyActionReroute, policy.Action)
		require.Greater(t, policy.Priority, util.GatewayRouterPolicyPriority)
		require.Less(t, policy.Priority, util.NodeRouterPolicyPriority)
	}
	gatewayRoutes, err := ctrl.OVNNbClient.GetLogicalRouterPolicy(ctrl.config.ClusterRouter,
		util.GatewayRouterPolicyPriority, "ip4.src == 10.16.0.0/16", false)
	require.NoError(t, err)
	require.Equal(t, []string{"100.64.0.2"}, gatewayRoutes[0].Nexthops)

	// A join address change and loss of one family must update/remove existing rows.
	worker2 = worker2.DeepCopy()
	worker2.Annotations[util.IPAddressAnnotation] = "100.64.0.4"
	require.NoError(t, indexer.Update(worker2))
	require.NoError(t, ctrl.reconcileServiceProxyFallbackReroutePolicy(subnet))
	policies = serviceProxyFallbackPolicies(t, ctrl, subnet, "isServiceProxyFallbackReroutePolicy")
	require.Len(t, policies, 3)
	for _, policy := range policies {
		if policy.ExternalIDs["node"] == worker2.Name {
			require.Equal(t, []string{"100.64.0.4"}, policy.Nexthops)
		}
	}
	require.NoError(t, indexer.Delete(worker2))
	require.NoError(t, ctrl.reconcileServiceProxyFallbackReroutePolicy(subnet))
	require.Len(t, serviceProxyFallbackPolicies(t, ctrl, subnet, "isServiceProxyFallbackReroutePolicy"), 2)
}

func TestServiceProxyFallbackRerouteLifecycle(t *testing.T) {
	for _, transition := range []string{"lb-enabled", "distributed", "physical-underlay", "service-cidr", "subnet-cidr", "delete"} {
		t.Run(transition, func(t *testing.T) {
			fc, subnet := newServiceProxyFallbackController(t)
			ctrl := fc.fakeController
			subnet.Spec.GatewayType = kubeovnv1.GWCentralizedType
			ctrl.config.EnableOvnLB = false
			require.NoError(t, ctrl.reconcileServiceProxyFallbackReroutePolicy(subnet))
			require.Len(t, serviceProxyFallbackPolicies(t, ctrl, subnet, "isServiceProxyFallbackReroutePolicy"), 2)
			other := subnet.DeepCopy()
			other.Name = "other"
			other.Spec.CIDRBlock = "10.17.0.0/16,fd00:10:17::/64"
			require.NoError(t, ctrl.reconcileServiceProxyFallbackReroutePolicy(other))
			want := 0
			switch transition {
			case "lb-enabled":
				ctrl.config.EnableOvnLB = true
			case "distributed":
				subnet.Spec.GatewayType = kubeovnv1.GWDistributedType
			case "physical-underlay":
				subnet.Spec.Vlan = "vlan1"
			case "service-cidr":
				ctrl.serviceCIDRStore = util.NewServiceCIDRStore("10.112.0.0/12")
				want = 1
			case "subnet-cidr":
				subnet.Spec.CIDRBlock = "10.18.0.0/16"
				want = 1
			}
			if transition == "delete" {
				require.NoError(t, ctrl.deletePolicyRouteByGatewayType(subnet, subnet.Spec.GatewayType, true))
			} else {
				require.NoError(t, ctrl.reconcileServiceProxyFallbackReroutePolicy(subnet))
			}
			policies := serviceProxyFallbackPolicies(t, ctrl, subnet, "isServiceProxyFallbackReroutePolicy")
			require.Len(t, policies, want)
			switch transition {
			case "subnet-cidr":
				require.Contains(t, policies[0].Match, "ip4.src == 10.18.0.0/16")
			case "service-cidr":
				require.Contains(t, policies[0].Match, "ip4.dst == 10.112.0.0/12")
			}
			require.Len(t, serviceProxyFallbackPolicies(t, ctrl, other, "isServiceProxyFallbackReroutePolicy"), 2)
		})
	}
}

func TestServiceProxyFallbackRemovesLegacyRouterDrops(t *testing.T) {
	fc, subnet := newServiceProxyFallbackController(t)
	ctrl := fc.fakeController
	for _, priority := range []int{29900, 30050} {
		require.NoError(t, ctrl.OVNNbClient.AddLogicalRouterPolicy(ctrl.config.ClusterRouter, priority,
			"ip4.src == $old.node_ip4 && ip4.dst == 10.96.0.0/12", ovnnb.LogicalRouterPolicyActionDrop, nil, nil,
			buildPolicyRouteExternalIDs(subnet.Name, map[string]string{"isServiceProxyFallbackDropPolicy": "true", "node": "deleted-node"})))
	}
	require.NoError(t, ctrl.reconcileOvnDefaultVpcRoute(subnet))
	policies := serviceProxyFallbackPolicies(t, ctrl, subnet, "isServiceProxyFallbackDropPolicy")
	require.Len(t, policies, 2)
	for _, policy := range policies {
		require.Equal(t, util.ServiceProxyFallbackDropPriority, policy.Priority)
		require.NotContains(t, policy.Match, "ip4.src")
		require.Contains(t, policy.Match, `inport == "ovn-cluster-ovn-default"`)
	}
}

func TestServiceProxyFallbackNodeEvents(t *testing.T) {
	for _, event := range []string{"add", "update", "delete"} {
		t.Run(event, func(t *testing.T) {
			fc, subnet := newServiceProxyFallbackController(t)
			subnet.Spec.GatewayType = kubeovnv1.GWCentralizedType
			require.NoError(t, fc.fakeInformers.subnetInformer.Informer().GetIndexer().Update(subnet))
			ctrl := fc.fakeController
			ctrl.config.EnableOvnLB = false
			queue := newTypedRateLimitingQueue[string]("service-proxy-node-test", nil)
			t.Cleanup(queue.ShutDown)
			ctrl.addOrUpdateSubnetQueue = queue
			ctrl.deletingNodeObjMap = xsync.NewMap[string, *corev1.Node]()
			ctrl.addNodeQueue = newTypedRateLimitingQueue[string]("fallback-node-add", nil)
			ctrl.updateNodeQueue = newTypedRateLimitingQueue[string]("fallback-node-update", nil)
			ctrl.deleteNodeQueue = newTypedRateLimitingQueue[string]("fallback-node-delete", nil)
			t.Cleanup(ctrl.addNodeQueue.ShutDown)
			t.Cleanup(ctrl.updateNodeQueue.ShutDown)
			t.Cleanup(ctrl.deleteNodeQueue.ShutDown)
			node := &corev1.Node{Name: "worker-2", Annotations: map[string]string{util.IPAddressAnnotation: "100.64.0.3"}}
			switch event {
			case "add":
				ctrl.enqueueAddNode(node)
			case "update":
				oldNode := node.DeepCopy()
				oldNode.Annotations[util.IPAddressAnnotation] = "100.64.0.4"
				ctrl.enqueueUpdateNode(oldNode, node)
			case "delete":
				ctrl.enqueueDeleteNode(node)
			}
			require.Equal(t, 1, queue.Len())
			key, _ := queue.Get()
			require.Equal(t, subnet.Name, key)
			queue.Done(key)
		})
	}
}
