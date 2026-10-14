package controller

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func newServiceProxyFallbackController(t *testing.T) (*Controller, *kubeovnv1.Subnet) {
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
	require.NoError(t, ctrl.addPolicyRouteForDistributedSubnet(subnet, node.Name, ip.Spec.V4IPAddress, ip.Spec.V6IPAddress))
	return ctrl, subnet
}

func serviceProxyFallbackPolicies(t *testing.T, ctrl *Controller, subnet *kubeovnv1.Subnet) []*ovnnb.LogicalRouterPolicy {
	t.Helper()
	policies, err := ctrl.OVNNbClient.ListLogicalRouterPolicies(ctrl.config.ClusterRouter, -1,
		buildPolicyRouteExternalIDs(subnet.Name, map[string]string{"isServiceProxyFallbackDropPolicy": "true"}), true)
	require.NoError(t, err)
	return policies
}

func TestServiceProxyFallbackDisablingLoadBalancer(t *testing.T) {
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprintf("global=%t", global), func(t *testing.T) {
			ctrl, subnet := newServiceProxyFallbackController(t)
			require.Len(t, serviceProxyFallbackPolicies(t, ctrl, subnet), 2)
			if global {
				ctrl.config.EnableOvnLB = false
			} else {
				subnet.Spec.EnableLb = new(false)
			}
			require.NoError(t, ctrl.addPolicyRouteForDistributedSubnet(subnet, "worker-1", "100.64.0.2", "fd00:100:64::2"))
			require.Empty(t, serviceProxyFallbackPolicies(t, ctrl, subnet))
			routes, err := ctrl.OVNNbClient.ListLogicalRouterPolicies(ctrl.config.ClusterRouter,
				util.GatewayRouterPolicyPriority, buildPolicyRouteExternalIDs(subnet.Name, nil), true)
			require.NoError(t, err)
			require.Len(t, routes, 2, "node proxy reroutes must remain usable when OVN LB is disabled")
		})
	}
}

func TestServiceProxyFallbackPreservesLocalDNSPriority(t *testing.T) {
	ctrl, subnet := newServiceProxyFallbackController(t)
	for _, policy := range serviceProxyFallbackPolicies(t, ctrl, subnet) {
		// Seed the priority used before the NodeLocalDNS ordering fix.
		require.NoError(t, ctrl.OVNNbClient.AddLogicalRouterPolicy(ctrl.config.ClusterRouter, 30050,
			policy.Match, ovnnb.LogicalRouterPolicyActionDrop, nil, nil, policy.ExternalIDs))
	}
	for _, dns := range []struct {
		af      int
		ip      string
		nextHop string
	}{
		{4, "10.96.0.10", "100.64.0.2"},
		{6, "fd00:10:96::a", "fd00:100:64::2"},
	} {
		require.NoError(t, ctrl.addPolicyRouteForLocalDNSCacheOnNode([]string{dns.ip},
			util.NodeLspName("worker-1"), dns.nextHop, "worker-1", dns.af))
	}
	require.NoError(t, ctrl.reconcileServiceProxyFallbackDropPolicies(subnet, "worker-1"))
	policies := serviceProxyFallbackPolicies(t, ctrl, subnet)
	require.Len(t, policies, 2, "reconciliation must replace the old priority without leaving duplicate matches")
	dnsPolicies, err := ctrl.OVNNbClient.ListLogicalRouterPolicies(ctrl.config.ClusterRouter, -1,
		map[string]string{"isLocalDnsCache": "true", "node": "worker-1"}, true)
	require.NoError(t, err)
	require.Len(t, dnsPolicies, 2)
	for _, policy := range policies {
		require.Equal(t, ovnnb.LogicalRouterPolicyActionDrop, policy.Action)
		require.Greater(t, policy.Priority, util.GatewayRouterPolicyPriority)
		for _, dnsPolicy := range dnsPolicies {
			require.Less(t, policy.Priority, dnsPolicy.Priority, "NodeLocalDNS inside ServiceCIDR must bypass the drop")
		}
	}
}

func TestServiceProxyFallbackOverlayToUnderlay(t *testing.T) {
	ctrl, subnet := newServiceProxyFallbackController(t)
	// Cleanup must cover all former nodes, including nodes no longer in the informer.
	require.NoError(t, ctrl.addPolicyRouteForDistributedSubnet(subnet, "removed-node", "100.64.0.3", "fd00:100:64::3"))
	otherSubnet := subnet.DeepCopy()
	otherSubnet.Name = "other-subnet"
	require.NoError(t, ctrl.addPolicyRouteForDistributedSubnet(otherSubnet, "worker-1", "100.64.0.2", "fd00:100:64::2"))
	require.Len(t, serviceProxyFallbackPolicies(t, ctrl, subnet), 4)

	subnet.Spec.Vlan = "vlan-1"
	subnet.Spec.EnableLb = new(false)
	subnet.Spec.U2OInterconnection = true
	subnet.Status.U2OInterconnectionIP = "10.16.0.2,fd00:10:16::2"
	require.NoError(t, ctrl.reconcileOvnDefaultVpcRoute(subnet))
	require.Empty(t, serviceProxyFallbackPolicies(t, ctrl, subnet), "old overlay drops must not shadow U2O proxy routes")
	require.Len(t, serviceProxyFallbackPolicies(t, ctrl, otherSubnet), 2, "cleanup must preserve other subnet policies")
	routes, err := ctrl.OVNNbClient.ListLogicalRouterPolicies(ctrl.config.ClusterRouter, -1,
		buildPolicyRouteExternalIDs(subnet.Name, map[string]string{"isU2ONoLBRoutePolicy": "true"}), true)
	require.NoError(t, err)
	require.Len(t, routes, 2)
	for _, route := range routes {
		require.Equal(t, ovnnb.LogicalRouterPolicyActionReroute, route.Action)
		require.Equal(t, util.U2OSubnetPolicyPriority, route.Priority)
	}
}

func TestServiceProxyFallbackCentralizedRetry(t *testing.T) {
	ctrl, subnet := newServiceProxyFallbackController(t)
	require.Len(t, serviceProxyFallbackPolicies(t, ctrl, subnet), 2)
	subnet.Spec.GatewayType = kubeovnv1.GWCentralizedType
	subnet.Spec.GatewayNode = "worker-1"
	subnet.Spec.EnableLb = new(false)
	// Simulate a restart after the active gateway was persisted but before the
	// old distributed policies were removed.
	subnet.Status.ActivateGateway = "worker-1"
	require.NoError(t, ctrl.reconcileOvnDefaultVpcRoute(subnet))
	require.Empty(t, serviceProxyFallbackPolicies(t, ctrl, subnet))
	for _, cidr := range []struct {
		family string
		block  string
	}{
		{"ip4", "10.16.0.0/16"},
		{"ip6", "fd00:10:16::/64"},
	} {
		routes, err := ctrl.OVNNbClient.GetLogicalRouterPolicy(ctrl.config.ClusterRouter,
			util.GatewayRouterPolicyPriority, fmt.Sprintf("%s.src == %s", cidr.family, cidr.block), false)
		require.NoError(t, err)
		require.Len(t, routes, 1)
		require.Equal(t, ovnnb.LogicalRouterPolicyActionReroute, routes[0].Action)
	}
}
