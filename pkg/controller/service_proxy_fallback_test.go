package controller

import (
	"fmt"
	"testing"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
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
			require.Equal(t, fmt.Sprintf(`inport == "ovn-cluster-ovn-default" && ip4.src == $%s_ip4 && ip4.src == 10.16.0.0/16 && ip4.dst == 10.96.0.0/12`, serviceProxyFallbackPortGroupName(subnet.Name, node)), policy.Match)
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

func TestServiceProxyFallbackSecondaryNetworkSurvivesNodePGRefresh(t *testing.T) {
	const provider = "secondary.default.ovn"
	primary := &kubeovnv1.Subnet{Name: "custom-primary", Spec: kubeovnv1.SubnetSpec{Vpc: "custom", CIDRBlock: "192.168.0.0/24"}}
	secondary := &kubeovnv1.Subnet{Name: "secondary", Spec: kubeovnv1.SubnetSpec{Vpc: util.DefaultVpc, Provider: provider, CIDRBlock: "10.16.0.0/16", GatewayType: kubeovnv1.GWCentralizedType, EnableLb: new(false)}}
	node := &corev1.Node{Name: "worker-1", Annotations: map[string]string{util.IPAddressAnnotation: "100.64.0.2", util.PortNameAnnotation: "node-worker-1"}}
	pod := &corev1.Pod{Name: "client", Namespace: "default", Spec: corev1.PodSpec{NodeName: node.Name}, Status: corev1.PodStatus{Phase: corev1.PodRunning}, Annotations: map[string]string{
		nadv1.NetworkAttachmentAnnot: `[{"name":"secondary"}]`,
		util.LogicalRouterAnnotation: "custom", util.LogicalSwitchAnnotation: primary.Name,
		util.AllocatedAnnotation: "true", util.IPAddressAnnotation: "192.168.0.2",
		fmt.Sprintf(util.AllocatedAnnotationTemplate, provider):     "true",
		fmt.Sprintf(util.IPAddressAnnotationTemplate, provider):     "10.16.0.2",
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider): secondary.Name,
	}}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{primary, secondary}, Pods: []*corev1.Pod{pod}, Nodes: []*corev1.Node{node},
		NetworkAttachments: []*nadv1.NetworkAttachmentDefinition{{Name: "secondary", Namespace: "default", Spec: nadv1.NetworkAttachmentDefinitionSpec{Config: `{"cniVersion":"0.3.1","name":"secondary","type":"kube-ovn"}`}}},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.EnableOvnLB = false
	ctrl.config.EnableNP = false
	nets, err := ctrl.getPodKubeovnNets(pod)
	require.NoError(t, err)
	require.Len(t, nets, 2)
	require.True(t, ctrl.subnetUsesNodeServiceProxy(secondary))
	ctrl.serviceCIDRStore = util.NewServiceCIDRStore("10.96.0.0/12")
	ctrl.OVNNbClient = newInMemoryOVNNbClient(t)
	require.NoError(t, ctrl.OVNNbClient.CreateLogicalRouter(ctrl.config.ClusterRouter))
	require.NoError(t, ctrl.OVNNbClient.CreateBareLogicalSwitch(secondary.Name))
	require.NoError(t, ctrl.OVNNbClient.CreateBareLogicalSwitch(primary.Name))
	require.NoError(t, ctrl.OVNNbClient.CreateBareLogicalSwitchPort(primary.Name, ovs.PodNameToPortName(pod.Name, pod.Namespace, util.OvnProvider), "192.168.0.2", "00:00:00:00:00:02"))
	port := ovs.PodNameToPortName(pod.Name, pod.Namespace, provider)
	require.NoError(t, ctrl.OVNNbClient.CreateBareLogicalSwitchPort(secondary.Name, port, "10.16.0.2", "00:00:00:00:16:02"))
	require.NoError(t, ctrl.OVNNbClient.CreatePortGroup("node.worker.1", map[string]string{"node": node.Name, networkPolicyKey: "node/worker-1"}))
	require.NoError(t, ctrl.OVNNbClient.PortGroupAddPorts("node.worker.1", port))
	require.NoError(t, ctrl.reconcileServiceProxyFallbackReroutePolicy(secondary))
	require.Len(t, serviceProxyFallbackPolicies(t, ctrl, secondary, "isServiceProxyFallbackReroutePolicy"), 1)
	before, err := ctrl.OVNNbClient.GetPortGroup("node.worker.1", false)
	require.NoError(t, err)
	require.Len(t, before.Ports, 1)
	require.NoError(t, ctrl.checkAndUpdateNodePortGroup())
	after, err := ctrl.OVNNbClient.GetPortGroup("node.worker.1", false)
	require.NoError(t, err)
	require.Equal(t, before.Ports, after.Ports, "retain the eligible secondary port without adding the custom VPC primary port")
}

func TestServiceProxyFallbackOverlappingVLANOwnership(t *testing.T) {
	fc, a := newServiceProxyFallbackController(t)
	ctrl := fc.fakeController
	ctrl.config.EnableOvnLB = false
	a.Spec.GatewayType = kubeovnv1.GWCentralizedType
	a.Spec.Vlan = "vlan-a"
	a.Spec.LogicalGateway = true
	b := a.DeepCopy()
	b.Name = "other-vlan"
	b.Spec.Vlan = "vlan-b"
	require.NoError(t, util.ValidateCidrConflict(*b, []kubeovnv1.Subnet{*a}))
	require.NoError(t, ctrl.reconcileServiceProxyFallbackReroutePolicy(a))
	require.Len(t, serviceProxyFallbackPolicies(t, ctrl, a, "isServiceProxyFallbackReroutePolicy"), 2)
	require.NoError(t, ctrl.reconcileServiceProxyFallbackReroutePolicy(b))
	require.Len(t, serviceProxyFallbackPolicies(t, ctrl, a, "isServiceProxyFallbackReroutePolicy"), 2, "reconciling another valid subnet must not steal this subnet's fallback rows")
	bPolicies := serviceProxyFallbackPolicies(t, ctrl, b, "isServiceProxyFallbackReroutePolicy")
	require.Len(t, bPolicies, 2)
	for _, policy := range bPolicies {
		require.Contains(t, policy.Match, `inport == "ovn-cluster-other-vlan"`)
	}
	require.NoError(t, ctrl.reconcileServiceProxyFallbackReroutePolicy(a))
	require.ElementsMatch(t, bPolicies, serviceProxyFallbackPolicies(t, ctrl, b, "isServiceProxyFallbackReroutePolicy"))
	require.NoError(t, ctrl.deleteServiceProxyFallbackPolicies(a, "isServiceProxyFallbackReroutePolicy"))
	require.Empty(t, serviceProxyFallbackPolicies(t, ctrl, a, "isServiceProxyFallbackReroutePolicy"))
	require.ElementsMatch(t, bPolicies, serviceProxyFallbackPolicies(t, ctrl, b, "isServiceProxyFallbackReroutePolicy"))
}
